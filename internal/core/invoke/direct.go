package invoke

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"strings"
	"time"

	"github.com/hex1n/sofarpc-cli/internal/boltclient"
	"github.com/hex1n/sofarpc-cli/internal/core/target"
	"github.com/hex1n/sofarpc-cli/internal/errcode"
	"github.com/hex1n/sofarpc-cli/internal/sofarpcwire"
)

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
	if !supportsDirectTransport(plan.Target) {
		return DirectExecution{}, nil
	}

	addr, err := directDialAddr(plan.Target.DirectURL)
	if err != nil {
		return DirectExecution{Handled: true}, targetUnreachableError(phase,
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
		RequestID: uint32(time.Now().UnixNano()),
	})
	if err != nil {
		return DirectExecution{Handled: true}, targetUnreachableError(phase,
			fmt.Sprintf("invoke direct target %s: %v", plan.Target.DirectURL, err))
	}

	diagnostics := map[string]any{
		"transport":               DirectTransportName,
		"target":                  plan.Target.DirectURL,
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

func supportsDirectTransport(cfg target.Config) bool {
	cfg = normalizeTargetConfig(cfg)
	if cfg.Mode != target.ModeDirect || cfg.DirectURL == "" {
		return false
	}
	if cfg.Protocol != "" && cfg.Protocol != "bolt" {
		return false
	}
	if cfg.Serialization != "" && cfg.Serialization != "hessian2" {
		return false
	}
	return true
}

func normalizeTargetConfig(cfg target.Config) target.Config {
	switch {
	case cfg.DirectURL != "":
		cfg.Mode = target.ModeDirect
	case cfg.RegistryAddress != "":
		cfg.Mode = target.ModeRegistry
	}
	cfg.Protocol = strings.TrimSpace(cfg.Protocol)
	cfg.Serialization = strings.TrimSpace(cfg.Serialization)
	cfg.DirectURL = strings.TrimSpace(cfg.DirectURL)
	cfg.RegistryAddress = strings.TrimSpace(cfg.RegistryAddress)
	return cfg
}

func directDialAddr(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", fmt.Errorf("empty target")
	}
	if !strings.Contains(raw, "://") {
		if _, _, err := net.SplitHostPort(raw); err != nil {
			return "", err
		}
		return raw, nil
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return "", err
	}
	if parsed.Host == "" {
		return "", fmt.Errorf("missing host:port")
	}
	if _, _, err := net.SplitHostPort(parsed.Host); err != nil {
		return "", err
	}
	return parsed.Host, nil
}

func targetUnreachableError(phase, message string) *errcode.Error {
	return errcode.New(errcode.TargetUnreachable, phase, message).
		WithHint("sofarpc_target", map[string]any{"explain": true},
			"inspect the resolved direct target and reachability")
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
