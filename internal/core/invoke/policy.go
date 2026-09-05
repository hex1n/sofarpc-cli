package invoke

import (
	"fmt"
	"net"
	"os"
	"strconv"
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

// EnvFlag reports whether the named guardrail environment variable is set to
// a true value, using strict strconv.ParseBool semantics. Every execution
// entrypoint (MCP tools, the call CLI) must parse opt-ins through this single
// helper so an env value can never enable one path that another refuses.
func EnvFlag(name string) bool {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return false
	}
	value, err := strconv.ParseBool(raw)
	return err == nil && value
}

// EnvList returns the comma-separated values of the named environment
// variable with whitespace trimmed and empty items dropped.
func EnvList(name string) []string {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return nil
	}
	var out []string
	for _, item := range strings.Split(raw, ",") {
		if value := strings.TrimSpace(item); value != "" {
			out = append(out, value)
		}
	}
	return out
}

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

// ValidateRealInvoke is the real-invoke gate in the order invoke applies it:
// the policy must be enabled, then the service must be allowed.
func (p ExecutionPolicy) ValidateRealInvoke(service string, phase string) error {
	if err := p.ValidateRealInvokeEnabled(phase); err != nil {
		return err
	}
	return p.ValidateServiceAllowed(service, phase)
}

// ValidateRealInvokeEnabled rejects when real invoke is disabled or no
// project allowlist is configured; it looks at no service.
func (p ExecutionPolicy) ValidateRealInvokeEnabled(phase string) error {
	phase = normalizePolicyPhase(phase)
	if !p.AllowInvoke {
		return errcode.New(errcode.InvocationRejected, phase,
			"real invoke is disabled; set "+EnvAllowInvoke+"=true to enable non-dry-run calls").
			WithHint("sofarpc_invoke", map[string]any{"dryRun": true}, "inspect the plan safely first")
	}
	if !p.serviceAllowlistConfigured() {
		return errcode.New(errcode.InvocationRejected, phase,
			"project allowedServices is required for real invoke; configure .sofarpc/config.local.json or .sofarpc/config.json").
			WithHint("sofarpc_init_project", InitProjectHintArgs(p.Sources),
				"initialize project allowedServices before enabling real invoke")
	}
	return nil
}

// ValidateServiceAllowed rejects a service outside the configured allowlist.
func (p ExecutionPolicy) ValidateServiceAllowed(service string, phase string) error {
	phase = normalizePolicyPhase(phase)
	if !p.ServiceAllowed(service) {
		source := strings.TrimSpace(p.AllowedServicesSource)
		if source == "" {
			source = "configured service allowlist"
		}
		return errcode.New(errcode.InvocationRejected, phase,
			fmt.Sprintf("service %q is not allowed by %s", service, source)).
			WithHint("sofarpc_doctor", map[string]any{"service": service},
				"doctor reports the configured allowedServices this service was checked against")
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
				WithHint("sofarpc_target", TargetHintArgs(p.Sources),
					"inspect the resolved target before enabling real invoke")
		default:
			same, err := SameDirectTarget(cfg.DirectURL, ambientCfg.DirectURL)
			if err != nil {
				return errcode.New(errcode.TargetInvalid, phase, err.Error()).
					WithHint("sofarpc_target", TargetHintArgs(p.Sources),
						"inspect the resolved direct target address")
			}
			if !same {
				return errcode.New(errcode.InvocationRejected, phase,
					fmt.Sprintf("direct target %q does not match the resolved ambient target; set %s=true to allow per-call target overrides", cfg.DirectURL, EnvAllowTargetOverride)).
					WithHint("sofarpc_target", TargetHintArgs(p.Sources),
						"inspect the resolved target layers")
			}
		}
	}

	allowed, host, err := p.DirectTargetHostAllowed(cfg.DirectURL)
	if err != nil {
		return errcode.New(errcode.TargetInvalid, phase, err.Error()).
			WithHint("sofarpc_target", TargetHintArgs(p.Sources),
				"inspect the resolved direct target address")
	}
	if !allowed {
		return errcode.New(errcode.InvocationRejected, phase,
			fmt.Sprintf("direct target host %q is not allowed by %s", host, EnvAllowedTargetHosts)).
			WithHint("sofarpc_target", TargetHintArgs(p.Sources),
				"inspect the resolved direct target host")
	}
	return nil
}

// ValidateTargetResolved rejects when no config layer produced a target to
// execute against. It is the single owner of the "nothing resolved" rule:
// BuildPlan and doctor both route here so agents read one message.
func ValidateTargetResolved(cfg target.Config, phase string) error {
	if strings.TrimSpace(target.Normalize(cfg).Mode) != "" {
		return nil
	}
	return errcode.New(errcode.TargetMissing, normalizePolicyPhase(phase),
		"no target resolved; call sofarpc_target for the layer breakdown").
		WithHint("sofarpc_target", map[string]any{"explain": true},
			"inspect config layers to see which field is missing")
}

// ValidateTargetTransport rejects a resolved target the pure-Go transport
// cannot execute. It is the single owner of the direct+bolt+hessian2 rule:
// Execute applies it on the wire path, doctor previews it.
func ValidateTargetTransport(cfg target.Config, phase string) error {
	if target.SupportsDirectBolt(cfg) {
		return nil
	}
	return unsupportedTargetError(normalizePolicyPhase(phase), Plan{Target: cfg})
}

func (p ExecutionPolicy) ValidateProjectConfig(phase string) error {
	phase = normalizePolicyPhase(phase)
	if len(p.Sources.ConfigErrors) == 0 {
		return nil
	}
	return errcode.New(errcode.InvocationRejected, phase,
		"project target config has errors: "+target.FormatConfigErrors(p.Sources.ConfigErrors)).
		WithHint("sofarpc_target", TargetHintArgs(p.Sources),
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

// TargetHintArgs builds the sofarpc_target arguments that send an agent to
// the resolved layer breakdown, scoped to the project when one is known.
func TargetHintArgs(sources target.Sources) map[string]any {
	return projectScopedHintArgs(sources, map[string]any{"explain": true})
}

// InitProjectHintArgs builds the sofarpc_init_project arguments that let an
// agent inspect project policy without writing to it.
func InitProjectHintArgs(sources target.Sources) map[string]any {
	return projectScopedHintArgs(sources, map[string]any{"dryRun": true})
}

func projectScopedHintArgs(sources target.Sources, args map[string]any) map[string]any {
	if root := strings.TrimSpace(sources.ProjectRoot); root != "" {
		args["project"] = root
	}
	return args
}
