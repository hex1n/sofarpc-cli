package worker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/hex1n/sofarpc-cli/internal/errcode"
	"github.com/hex1n/sofarpc-cli/internal/facadesemantic"
)

// Client is the surface the MCP invoke handler calls. It couples a
// Pool with the Profile this process was configured for, so callers
// don't have to thread Profile through every request.
//
// A nil Client is a valid sentinel: every method returns
// errcode.DaemonUnavailable so the MCP layer can detect "worker not
// wired" without a stub branch in every handler.
type Client struct {
	Pool    *Pool
	Profile Profile
}

// Invoke routes req through the pooled worker for c.Profile and returns
// the worker's Response. WireError is lifted into an *errcode.Error so
// the MCP layer can surface a consistent error shape.
//
// nil-Client / empty Profile / pool errors all collapse into
// errcode.DaemonUnavailable so the MCP handler doesn't need to tell
// "worker not configured" apart from "worker unreachable".
//
// Self-heal: when Send returns ErrConnClosed (the request never hit
// the wire because the pooled conn was already dead), we evict the
// dead slot and retry once with a fresh spawn. A persistently broken
// worker fails on the second attempt and surfaces DaemonUnavailable.
//
// We deliberately do NOT retry on ErrConnLost (conn dropped after the
// request was flushed). The worker may have processed the request
// before it died, so replaying would double-apply a non-idempotent
// call. Instead we evict the dead slot and surface
// InvocationUncertain so the agent can decide.
func (c *Client) Invoke(ctx context.Context, req Request) (Response, error) {
	if c == nil || c.Pool == nil {
		return Response{}, daemonUnavailable("worker client not configured; set SOFARPC_RUNTIME_JAR")
	}
	if c.Profile.Empty() {
		return Response{}, daemonUnavailable("worker profile is incomplete; check SOFARPC_JAVA/SOFARPC_RUNTIME_JAR")
	}

	for attempt := 0; attempt < 2; attempt++ {
		proc, err := c.Pool.Get(ctx, c.Profile)
		if err != nil {
			return Response{}, daemonUnavailable("spawn worker: " + err.Error())
		}
		resp, err := proc.Conn().Send(ctx, req)
		if err == nil {
			if !resp.Ok && resp.Error != nil {
				return resp, liftWireError(resp.Error)
			}
			return resp, nil
		}
		if errors.Is(err, ErrConnClosed) {
			if attempt == 0 {
				c.Pool.Evict(c.Profile)
				continue
			}
			return Response{}, daemonUnavailable("worker connection closed before request was sent")
		}
		if errors.Is(err, ErrConnLost) {
			c.Pool.Evict(c.Profile)
			return Response{}, errcode.New(errcode.InvocationUncertain, "invoke",
				"worker disconnected after the request was sent; outcome is unknown").
				WithHint("sofarpc_doctor", nil,
					"check worker logs; rerun only if the action is idempotent")
		}
		return Response{}, errcode.New(errcode.WorkerError, "invoke",
			"worker request failed: "+err.Error()).
			WithHint("sofarpc_doctor", nil, "check worker health")
	}
	// Loop exits only by return; this is defensive.
	return Response{}, daemonUnavailable("worker connection closed after respawn")
}

// Describe asks the worker to reflect on `service` (a fully-qualified
// class name) and return its facadesemantic.Class. The worker must have
// been launched with a facade classpath that resolves `service`.
//
// Returns (Class, true, nil) on success.
// Returns (_, false, nil) when the worker reports contract.unresolvable
// (class not on the facade classpath) — this matches contract.Store's
// "ok=false for unknown types" semantics so the adapter can hand the
// result straight through.
// Any other failure collapses to an *errcode.Error the caller can surface.
//
// Retry / eviction behavior mirrors Invoke: transient ErrConnClosed
// respawns once; ErrConnLost surfaces InvocationUncertain because a
// describe isn't strictly idempotent from the worker's perspective (it
// may have triggered a classloader load with side effects).
func (c *Client) Describe(ctx context.Context, service string) (facadesemantic.Class, bool, error) {
	var zero facadesemantic.Class
	if c == nil || c.Pool == nil {
		return zero, false, daemonUnavailable("worker client not configured; set SOFARPC_RUNTIME_JAR")
	}
	if c.Profile.Empty() {
		return zero, false, daemonUnavailable("worker profile is incomplete; check SOFARPC_JAVA/SOFARPC_RUNTIME_JAR")
	}

	req := Request{Action: ActionDescribe, Service: service}
	for attempt := 0; attempt < 2; attempt++ {
		proc, err := c.Pool.Get(ctx, c.Profile)
		if err != nil {
			return zero, false, daemonUnavailable("spawn worker: " + err.Error())
		}
		resp, err := proc.Conn().Send(ctx, req)
		if err == nil {
			if !resp.Ok && resp.Error != nil {
				// Translate "class not on facade classpath" into ok=false
				// so the Store adapter looks like any other Store on a miss.
				if resp.Error.Code == string(errcode.ContractUnresolvable) {
					return zero, false, nil
				}
				return zero, false, liftWireError(resp.Error)
			}
			cls, err := decodeDescribeResult(resp.Result)
			if err != nil {
				return zero, false, errcode.New(errcode.WorkerError, "describe",
					"decode describe result: "+err.Error()).
					WithHint("sofarpc_doctor", nil, "worker returned a malformed describe payload")
			}
			return cls, true, nil
		}
		if errors.Is(err, ErrConnClosed) {
			if attempt == 0 {
				c.Pool.Evict(c.Profile)
				continue
			}
			return zero, false, daemonUnavailable("worker connection closed before request was sent")
		}
		if errors.Is(err, ErrConnLost) {
			c.Pool.Evict(c.Profile)
			return zero, false, errcode.New(errcode.InvocationUncertain, "describe",
				"worker disconnected after the request was sent; outcome is unknown").
				WithHint("sofarpc_doctor", nil,
					"check worker logs; retry describe once the worker is back")
		}
		return zero, false, errcode.New(errcode.WorkerError, "describe",
			"worker request failed: "+err.Error()).
			WithHint("sofarpc_doctor", nil, "check worker health")
	}
	return zero, false, daemonUnavailable("worker connection closed after respawn")
}

// decodeDescribeResult round-trips through JSON because the worker
// returns `result` as a decoded map[string]any but our Class type has
// nested slices/structs. A round-trip is cheaper than reflecting into
// the target shape by hand.
func decodeDescribeResult(raw any) (facadesemantic.Class, error) {
	var cls facadesemantic.Class
	if raw == nil {
		return cls, fmt.Errorf("describe response has no result")
	}
	body, err := json.Marshal(raw)
	if err != nil {
		return cls, err
	}
	if err := json.Unmarshal(body, &cls); err != nil {
		return cls, err
	}
	if cls.FQN == "" {
		return cls, fmt.Errorf("describe result missing fqn")
	}
	return cls, nil
}

// Close releases pool resources. Safe to call on a nil Client.
func (c *Client) Close(ctx context.Context) error {
	if c == nil || c.Pool == nil {
		return nil
	}
	return c.Pool.Close(ctx)
}

func daemonUnavailable(msg string) *errcode.Error {
	return errcode.New(errcode.DaemonUnavailable, "invoke", msg).
		WithHint("sofarpc_doctor", nil, "worker subsystem is not ready")
}

// liftWireError maps the worker's on-wire error into an errcode.Error.
// Unknown codes pass through as-is — the client-side taxonomy is a
// superset of the worker's, so new worker codes surface cleanly.
func liftWireError(w *WireError) error {
	hint := ""
	if w.Hint != nil {
		if v, ok := w.Hint["reason"].(string); ok {
			hint = v
		}
	}
	phase := w.Phase
	if phase == "" {
		phase = "worker"
	}
	ec := errcode.New(errcode.Code(w.Code), phase, w.Message)
	if nextTool, _ := asString(w.Hint, "nextTool"); nextTool != "" {
		nextArgs, _ := w.Hint["nextArgs"].(map[string]any)
		ec = ec.WithHint(nextTool, nextArgs, hint)
	}
	return ec
}

func asString(m map[string]any, key string) (string, bool) {
	if m == nil {
		return "", false
	}
	v, ok := m[key].(string)
	return v, ok
}
