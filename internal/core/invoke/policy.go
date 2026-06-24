package invoke

import (
	"fmt"
	"net"
	"strings"

	"github.com/hex1n/sofarpc-cli/internal/core/target"
	"github.com/hex1n/sofarpc-cli/internal/errcode"
)

const (
	EnvAllowInvoke                   = "SOFARPC_ALLOW_INVOKE"
	EnvAllowedServices               = "SOFARPC_ALLOWED_SERVICES"
	EnvAllowTargetOverride           = "SOFARPC_ALLOW_TARGET_OVERRIDE"
	EnvAllowedTargetHosts            = "SOFARPC_ALLOWED_TARGET_HOSTS"
	EnvMaxConcurrentInvokes          = "SOFARPC_MAX_CONCURRENT_INVOKES"
	EnvMaxConcurrentInvokesPerTarget = "SOFARPC_MAX_CONCURRENT_INVOKES_PER_TARGET"
	EnvInvokeQueueTimeoutMS          = "SOFARPC_INVOKE_QUEUE_TIMEOUT_MS"
)

// ExecutionPolicy is the core invoke guardrail. It keeps real-invoke
// enablement, service allowlisting, and target override checks behind one
// interface so invoke and replay enforce the same rules.
type ExecutionPolicy struct {
	AllowInvoke               bool
	AllowedServices           []string
	AllowedServicesConfigured bool
	AllowedServicesSource     string
	AllowTargetOverride       bool
	AllowedTargetHosts        []string
	Sources                   target.Sources
}

type ExecutionPolicyDiagnostics struct {
	AllowInvoke               bool     `json:"allowInvoke"`
	AllowedServices           []string `json:"allowedServices,omitempty"`
	AllowedServicesConfigured bool     `json:"allowedServicesConfigured"`
	AllowedServicesSource     string   `json:"allowedServicesSource,omitempty"`
	AllowTargetOverride       bool     `json:"allowTargetOverride"`
	AllowedTargetHosts        []string `json:"allowedTargetHosts,omitempty"`
}

func (p ExecutionPolicy) Diagnostics() ExecutionPolicyDiagnostics {
	return ExecutionPolicyDiagnostics{
		AllowInvoke:               p.AllowInvoke,
		AllowedServices:           append([]string(nil), p.AllowedServices...),
		AllowedServicesConfigured: p.serviceAllowlistConfigured(),
		AllowedServicesSource:     strings.TrimSpace(p.AllowedServicesSource),
		AllowTargetOverride:       p.AllowTargetOverride,
		AllowedTargetHosts:        append([]string(nil), p.AllowedTargetHosts...),
	}
}

func (p ExecutionPolicy) Validate(plan Plan, phase string) error {
	if err := p.ValidateProjectConfig(phase); err != nil {
		return err
	}
	if err := p.ValidateRealInvoke(plan.Service, phase); err != nil {
		return err
	}
	return p.ValidateTarget(plan, phase)
}

func (p ExecutionPolicy) ValidateRealInvoke(service string, phase string) error {
	phase = normalizePolicyPhase(phase)
	if !p.AllowInvoke {
		return errcode.New(errcode.InvocationRejected, phase,
			"real invoke is disabled; set "+EnvAllowInvoke+"=true to enable non-dry-run calls").
			WithHint("sofarpc_invoke", map[string]any{"dryRun": true}, "inspect the plan safely first")
	}
	if !p.serviceAllowlistConfigured() {
		return errcode.New(errcode.InvocationRejected, phase,
			"project allowedServices is required for real invoke; configure .sofarpc/config.local.json or .sofarpc/config.json").
			WithHint("sofarpc_init_project", map[string]any{"dryRun": true},
				"initialize project allowedServices before enabling real invoke")
	}
	if !p.ServiceAllowed(service) {
		source := strings.TrimSpace(p.AllowedServicesSource)
		if source == "" {
			source = "configured service allowlist"
		}
		return errcode.New(errcode.InvocationRejected, phase,
			fmt.Sprintf("service %q is not allowed by %s", service, source)).
			WithHint("sofarpc_doctor", nil, "inspect invoke safety configuration")
	}
	return nil
}

func (p ExecutionPolicy) ServiceAllowed(service string) bool {
	if !p.serviceAllowlistConfigured() {
		return false
	}
	for _, allowed := range p.AllowedServices {
		if allowed == "*" || allowed == service {
			return true
		}
	}
	return false
}

func (p ExecutionPolicy) serviceAllowlistConfigured() bool {
	return p.AllowedServicesConfigured || len(p.AllowedServices) > 0
}

func (p ExecutionPolicy) ValidateTarget(plan Plan, phase string) error {
	phase = normalizePolicyPhase(phase)
	if err := p.ValidateProjectConfig(phase); err != nil {
		return err
	}

	cfg := target.Normalize(plan.Target)
	if cfg.Mode != target.ModeDirect || cfg.DirectURL == "" {
		return nil
	}

	ambientCfg := target.Normalize(target.Resolve(target.Input{}, p.Sources).Target)
	if !p.AllowTargetOverride {
		switch {
		case ambientCfg.DirectURL == "":
			return errcode.New(errcode.InvocationRejected, phase,
				fmt.Sprintf("direct target %q is not allowed; configure .sofarpc/config.local.json, .sofarpc/config.json, or set %s=true", cfg.DirectURL, EnvAllowTargetOverride)).
				WithHint("sofarpc_target", policyTargetHintArgs(p.Sources),
					"inspect the resolved target before enabling real invoke")
		default:
			same, err := SameDirectTarget(cfg.DirectURL, ambientCfg.DirectURL)
			if err != nil {
				return errcode.New(errcode.TargetInvalid, phase, err.Error()).
					WithHint("sofarpc_target", policyTargetHintArgs(p.Sources),
						"inspect the resolved direct target address")
			}
			if !same {
				return errcode.New(errcode.InvocationRejected, phase,
					fmt.Sprintf("direct target %q does not match the resolved ambient target; set %s=true to allow per-call target overrides", cfg.DirectURL, EnvAllowTargetOverride)).
					WithHint("sofarpc_target", policyTargetHintArgs(p.Sources),
						"inspect the resolved target layers")
			}
		}
	}

	allowed, host, err := p.DirectTargetHostAllowed(cfg.DirectURL)
	if err != nil {
		return errcode.New(errcode.TargetInvalid, phase, err.Error()).
			WithHint("sofarpc_target", policyTargetHintArgs(p.Sources),
				"inspect the resolved direct target address")
	}
	if !allowed {
		return errcode.New(errcode.InvocationRejected, phase,
			fmt.Sprintf("direct target host %q is not allowed by %s", host, EnvAllowedTargetHosts)).
			WithHint("sofarpc_doctor", nil, "inspect invoke safety configuration")
	}
	return nil
}

func (p ExecutionPolicy) ValidateProjectConfig(phase string) error {
	phase = normalizePolicyPhase(phase)
	if len(p.Sources.ConfigErrors) == 0 {
		return nil
	}
	return errcode.New(errcode.InvocationRejected, phase,
		"project target config has errors: "+formatPolicyConfigErrors(p.Sources.ConfigErrors)).
		WithHint("sofarpc_target", policyTargetHintArgs(p.Sources),
			"inspect project config errors and resolved target layers")
}

func SameDirectTarget(left, right string) (bool, error) {
	leftDial, err := target.ParseDirectDialAddress(left)
	if err != nil {
		return false, err
	}
	rightDial, err := target.ParseDirectDialAddress(right)
	if err != nil {
		return false, err
	}
	return strings.EqualFold(leftDial, rightDial), nil
}

func (p ExecutionPolicy) DirectTargetHostAllowed(directURL string) (bool, string, error) {
	if len(p.AllowedTargetHosts) == 0 {
		return true, "", nil
	}
	dialTarget, err := target.ParseDirectDialAddress(directURL)
	if err != nil {
		return false, "", err
	}
	host, port, err := net.SplitHostPort(dialTarget)
	if err != nil {
		return false, "", fmt.Errorf("parse direct target host: %w", err)
	}
	normalizedDial := net.JoinHostPort(host, port)
	for _, allowed := range p.AllowedTargetHosts {
		switch {
		case allowed == "*",
			strings.EqualFold(allowed, host),
			strings.EqualFold(allowed, dialTarget),
			strings.EqualFold(allowed, normalizedDial):
			return true, host, nil
		}
	}
	return false, host, nil
}

func normalizePolicyPhase(phase string) string {
	if strings.TrimSpace(phase) == "" {
		return "invoke"
	}
	return phase
}

func policyTargetHintArgs(sources target.Sources) map[string]any {
	args := map[string]any{"explain": true}
	if strings.TrimSpace(sources.ProjectRoot) != "" {
		args["project"] = sources.ProjectRoot
	}
	return args
}

func formatPolicyConfigErrors(errors []target.ConfigError) string {
	parts := make([]string, 0, len(errors))
	for _, item := range errors {
		path := strings.TrimSpace(item.Path)
		msg := strings.TrimSpace(item.Error)
		switch {
		case path != "" && msg != "":
			parts = append(parts, path+": "+msg)
		case path != "":
			parts = append(parts, path)
		case msg != "":
			parts = append(parts, msg)
		}
	}
	return strings.Join(parts, "; ")
}
