package invoke

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	"github.com/hex1n/sofarpc-cli/internal/boltclient"
	"github.com/hex1n/sofarpc-cli/internal/core/target"
	"github.com/hex1n/sofarpc-cli/internal/errcode"
	"github.com/hex1n/sofarpc-cli/internal/sofarpcwire"
)

// requestIDCounter serializes BOLT request IDs. Seeded from the process
// start time so IDs don't restart at 1 on each run (useful for log
// correlation) and are monotonic across concurrent invokes.
var requestIDCounter atomic.Uint32

func init() {
	requestIDCounter.Store(uint32(time.Now().UnixNano()))
}

func nextRequestID() uint32 {
	for {
		id := requestIDCounter.Add(1)
		if id != 0 {
			return id
		}
	}
}

const DirectTransportName = "direct-bolt"

type DirectExecution struct {
	Handled     bool
	Result      any
	Diagnostics map[string]any
}

// ExecuteDirectIfPossible runs a plan through the pure-Go direct
// transport when the target is direct+bolt+hessian2. Unsupported targets
// return Handled=false so callers can fall back to the worker path.
func ExecuteDirectIfPossible(ctx context.Context, plan Plan, phase string) (DirectExecution, error) {
	if !target.SupportsDirectBolt(plan.Target) {
		return DirectExecution{}, nil
	}

	addr, err := target.ParseDirectDialAddress(plan.Target.DirectURL)
	if err != nil {
		return DirectExecution{Handled: true}, targetInvalidError(phase,
			fmt.Sprintf("invalid directUrl %q: %v", plan.Target.DirectURL, err))
	}

	timeout := time.Duration(plan.Target.TimeoutMS) * time.Millisecond
	if timeout <= 0 {
		timeout = 3 * time.Second
	}

	result, err := sofarpcwire.InvokeDirect(ctx, sofarpcwire.RequestSpec{
		Service:       plan.Service,
		Method:        plan.Method,
		ParamTypes:    plan.ParamTypes,
		Args:          plan.Args,
		Version:       plan.Version,
		UniqueID:      plan.Target.UniqueID,
		TargetAppName: plan.TargetAppName,
	}, sofarpcwire.DirectInvokeOptions{
		Addr:      addr,
		Codec:     boltclient.CodecHessian2,
		Timeout:   timeout,
		RequestID: nextRequestID(),
	})
	if err != nil {
		return DirectExecution{Handled: true}, classifyDirectInvokeError(phase, plan.Target.DirectURL, err)
	}

	diagnostics := map[string]any{
		"transport":               DirectTransportName,
		"target":                  plan.Target.DirectURL,
		"dialTarget":              addr,
		"requestId":               result.RequestID,
		"requestCodec":            boltclient.CodecHessian2,
		"requestClass":            result.Request.Class,
		"targetServiceUniqueName": result.Request.TargetServiceUniqueName,
		"responseStatus":          result.Response.ResponseStatus,
		"responseClass":           result.Response.ResponseClass,
		"responseCodec":           result.Response.Codec,
		"responseContentLength":   len(result.Response.Content),
	}
	if len(result.Response.Header) > 0 {
		diagnostics["responseHeader"] = cloneStringMap(result.Response.Header)
	}
	if result.Decoded != nil && len(result.Decoded.ResponseProps) > 0 {
		diagnostics["responseProps"] = cloneStringMap(result.Decoded.ResponseProps)
	}

	if result.DecodeErr != nil {
		return DirectExecution{Handled: true, Diagnostics: diagnostics},
			errcode.New(errcode.DeserializeFailed, phase,
				fmt.Sprintf("decode SOFARPC response: %v", result.DecodeErr))
	}
	if result.Decoded == nil {
		return DirectExecution{Handled: true, Diagnostics: diagnostics},
			errcode.New(errcode.DeserializeFailed, phase,
				"direct target returned no decodable payload")
	}
	if result.Decoded.IsError {
		msg := strings.TrimSpace(result.Decoded.ErrorMsg)
		if msg == "" {
			msg = "remote response flagged isError=true"
		}
		return DirectExecution{Handled: true, Diagnostics: diagnostics},
			errcode.New(errcode.InvocationRejected, phase, msg)
	}
	if msg := strings.TrimSpace(result.Decoded.ErrorMsg); msg != "" {
		return DirectExecution{Handled: true, Diagnostics: diagnostics},
			errcode.New(errcode.InvocationRejected, phase, msg)
	}

	return DirectExecution{
		Handled:     true,
		Result:      sofarpcwire.FormatValue(result.Decoded.AppResponse),
		Diagnostics: diagnostics,
	}, nil
}

func targetInvalidError(phase, message string) *errcode.Error {
	return errcode.New(errcode.TargetInvalid, phase, message).
		WithHint("sofarpc_target", map[string]any{"explain": true},
			"inspect the resolved direct target address")
}

func classifyDirectInvokeError(phase, directURL string, err error) *errcode.Error {
	msg := strings.ToLower(err.Error())
	human := fmt.Sprintf("invoke direct target %s: %v", directURL, err)

	switch {
	case strings.Contains(msg, "context deadline exceeded"),
		strings.Contains(msg, "timeout"),
		strings.Contains(msg, "i/o timeout"):
		return errcode.New(errcode.InvocationTimeout, phase, human).
			WithHint("sofarpc_doctor", nil,
				"inspect timeoutMs, connectTimeoutMs, and target reachability")
	case strings.Contains(msg, "dial"),
		strings.Contains(msg, "connection refused"),
		strings.Contains(msg, "no such host"),
		strings.Contains(msg, "network is unreachable"),
		strings.Contains(msg, "cannot assign requested address"):
		return errcode.New(errcode.TargetConnectFailed, phase, human).
			WithHint("sofarpc_target", map[string]any{"explain": true},
				"inspect the resolved direct target and reachability")
	case strings.Contains(msg, "hessian"),
		strings.Contains(msg, "encode"),
		strings.Contains(msg, "serialize"),
		strings.Contains(msg, "marshal"):
		return errcode.New(errcode.SerializeFailed, phase, human).
			WithHint("sofarpc_describe", nil,
				"inspect paramTypes and argument shape")
	default:
		return errcode.New(errcode.ProtocolFailed, phase, human).
			WithHint("sofarpc_doctor", nil,
				"run structured diagnostics for the direct invoke path")
	}
}

func cloneStringMap(input map[string]string) map[string]string {
	if len(input) == 0 {
		return nil
	}
	out := make(map[string]string, len(input))
	for key, value := range input {
		out[key] = value
	}
	return out
}
