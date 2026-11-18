package invoke

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/hex1n/sofarpc-cli/internal/boltclient"
	"github.com/hex1n/sofarpc-cli/internal/core/invocationprops"
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

const (
	DirectTransportName = "direct-bolt"
	envMaxResponseBytes = "SOFARPC_MAX_RESPONSE_BYTES"
)

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

	requestBaggage, err := invocationprops.Resolve(plan.InvocationProperties, os.LookupEnv)
	if err != nil {
		return DirectExecution{Handled: true}, invocationPropertiesInvalidError(phase, err)
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
	connectTimeout := time.Duration(plan.Target.ConnectTimeoutMS) * time.Millisecond
	if connectTimeout <= 0 {
		connectTimeout = time.Second
	}
	maxResponseBytes := maxResponseBytesFromEnv()
	requestID := nextRequestID()
	// A fresh trace per execution, not per plan: plans are replayable
	// payloads, so baking an identity into one would make every replay
	// land on the same trace id in provider logs.
	trace := sofarpcwire.NewTraceContext()

	// These describe the attempt rather than its outcome, so they are
	// built before the wire call: a timeout is exactly when the caller
	// needs traceId to find the request in the provider's logs.
	diagnostics := map[string]any{
		"transport":        DirectTransportName,
		"target":           plan.Target.DirectURL,
		"dialTarget":       addr,
		"requestId":        requestID,
		"requestCodec":     boltclient.CodecHessian2,
		"traceId":          trace.TraceID,
		"maxResponseBytes": maxResponseBytes,
	}

	result, err := sofarpcwire.InvokeDirect(ctx, sofarpcwire.RequestSpec{
		Service:        plan.Service,
		Method:         plan.Method,
		ParamTypes:     plan.ParamTypes,
		Args:           plan.Args,
		Version:        plan.Version,
		UniqueID:       plan.Target.UniqueID,
		TargetAppName:  plan.TargetAppName,
		RequestBaggage: requestBaggage,
		Trace:          trace,
	}, sofarpcwire.DirectInvokeOptions{
		Addr:             addr,
		Codec:            boltclient.CodecHessian2,
		Timeout:          timeout,
		ConnectTimeout:   connectTimeout,
		RequestID:        requestID,
		MaxResponseBytes: maxResponseBytes,
	})
	if err != nil {
		return DirectExecution{Handled: true, Diagnostics: diagnostics},
			classifyDirectInvokeError(phase, plan.Target.DirectURL, err)
	}

	diagnostics["requestClass"] = result.Request.Class
	diagnostics["targetServiceUniqueName"] = result.Request.TargetServiceUniqueName
	diagnostics["responseStatus"] = result.Response.ResponseStatus
	diagnostics["responseClass"] = result.Response.ResponseClass
	diagnostics["responseCodec"] = result.Response.Codec
	diagnostics["responseContentLength"] = len(result.Response.Content)
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

	formatted, err := sofarpcwire.FormatValueSafe(result.Decoded.AppResponse)
	if err != nil {
		return DirectExecution{Handled: true, Diagnostics: diagnostics},
			errcode.New(errcode.DeserializeFailed, phase,
				fmt.Sprintf("format SOFARPC response: %v", err))
	}

	return DirectExecution{
		Handled:     true,
		Result:      formatted,
		Diagnostics: diagnostics,
	}, nil
}

func maxResponseBytesFromEnv() int64 {
	raw := strings.TrimSpace(os.Getenv(envMaxResponseBytes))
	if raw == "" {
		return boltclient.DefaultMaxResponseBytes
	}
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || value <= 0 {
		return boltclient.DefaultMaxResponseBytes
	}
	return value
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
	case errors.Is(err, context.Canceled):
		return errcode.New(errcode.InvocationCanceled, phase, human)
	case errors.Is(err, context.DeadlineExceeded),
		strings.Contains(msg, "context deadline exceeded"),
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
