package worker

import (
	"context"
	"errors"

	"github.com/hex1n/sofarpc-cli/internal/errcode"
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
// Self-heal: when Send returns ErrConnClosed (the worker JVM died mid-
// request or between requests), we evict the dead slot from the pool
// and retry once with a fresh spawn. A persistently broken worker
// fails on the second attempt and surfaces DaemonUnavailable.
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
			return Response{}, daemonUnavailable("worker connection closed mid-request")
		}
		return Response{}, errcode.New(errcode.WorkerError, "invoke",
			"worker request failed: "+err.Error()).
			WithHint("sofarpc_doctor", nil, "check worker health")
	}
	// Loop exits only by return; this is defensive.
	return Response{}, daemonUnavailable("worker connection closed after respawn")
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
