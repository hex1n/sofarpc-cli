// Package invoke owns the generic-invoke core path: it builds Plans from
// resolved target + contract + args, and it executes them through the
// pure-Go direct transport. The planning half remains independently
// testable, so dryRun never needs a runtime.
package invoke

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/hex1n/sofarpc-cli/internal/core/contract"
	"github.com/hex1n/sofarpc-cli/internal/core/target"
	"github.com/hex1n/sofarpc-cli/internal/errcode"
	"github.com/hex1n/sofarpc-cli/internal/javamodel"
)

const PlanSchemaVersion = "sofarpc.invoke.plan/v1"

// Input is what sofarpc_invoke passes in. Target fields are merged into
// target.Sources via target.Resolve — BuildPlan does that internally.
type Input struct {
	Service       string
	Method        string
	ParamTypes    []string
	Args          any
	Version       string
	TargetAppName string
	Target        target.Input
}

// Plan is the wire-ready payload plus diagnostics. When dryRun is
// requested, sofarpc_invoke returns this verbatim; otherwise it hands
// the wire fields to the direct transport.
type Plan struct {
	SchemaVersion  string             `json:"schemaVersion"`
	Service        string             `json:"service"`
	Method         string             `json:"method"`
	ParamTypes     []string           `json:"paramTypes"`
	ReturnType     string             `json:"returnType,omitempty"`
	Args           []any              `json:"args"`
	Version        string             `json:"version,omitempty"`
	TargetAppName  string             `json:"targetAppName,omitempty"`
	Target         target.Config      `json:"target"`
	Overloads      []javamodel.Method `json:"overloads,omitempty"`
	Selected       int                `json:"selected"`
	ContractSource string             `json:"contractSource,omitempty"`
	TargetLayers   []target.Layer     `json:"targetLayers,omitempty"`
	ArgSource      string             `json:"argSource,omitempty"`
}

// ValidatePlanSchema rejects payload/session plans produced by incompatible
// future or pre-versioned plan shapes. Replay callers should run this before
// dry-run output or execution so unknown payloads are never treated as valid.
func ValidatePlanSchema(plan Plan, phase string) error {
	if strings.TrimSpace(plan.SchemaVersion) == PlanSchemaVersion {
		return nil
	}
	if strings.TrimSpace(plan.SchemaVersion) == "" {
		return errcode.New(errcode.PlanVersionUnsupported, phase,
			fmt.Sprintf("plan schemaVersion is missing; expected %q", PlanSchemaVersion)).
			WithHint("sofarpc_invoke", map[string]any{"dryRun": true},
				"produce a fresh replayable plan with invoke dryRun")
	}
	return errcode.New(errcode.PlanVersionUnsupported, phase,
		fmt.Sprintf("unsupported plan schemaVersion %q; expected %q", plan.SchemaVersion, PlanSchemaVersion)).
		WithHint("sofarpc_invoke", map[string]any{"dryRun": true},
			"produce a fresh replayable plan with invoke dryRun")
}

// BuildPlan resolves target + contract + args and returns a Plan.
// It never performs I/O — callers have already materialised target.Sources
// and plugged a contract.Store.
//
// Two modes:
//   - facade-store: facade != nil, standard path with overload
//     disambiguation and skeleton rendering.
//   - trusted: facade == nil, the agent supplies a complete
//     service/method/paramTypes/args tuple and we hand it straight to
//     the direct transport. The agent is expected to know the wire
//     shape from elsewhere (IDL, prior describe output, etc.).
//
// Failure modes (all *errcode.Error):
//   - target.missing: no layer supplied a target mode.
//   - workspace.facade-not-configured: trusted mode is missing paramTypes
//     or args — without a facade we cannot synthesize either.
//   - contract.*: propagated from contract.ResolveMethod (facade mode only).
//   - input.args-invalid: args provided with the wrong arity.
func BuildPlan(in Input, facade contract.Store, sources target.Sources) (Plan, error) {
	report := target.Resolve(in.Target, sources)
	if report.Target.Mode == "" {
		return Plan{}, errcode.New(errcode.TargetMissing, "invoke",
			"no target resolved; call sofarpc_target for the layer breakdown").
			WithHint("sofarpc_target", map[string]any{"explain": true},
				"inspect config layers to see which field is missing")
	}
	if facade == nil {
		return buildTrustedPlan(in, report)
	}

	resolved, err := contract.ResolveMethod(facade, in.Service, in.Method, in.ParamTypes)
	if err != nil {
		return Plan{}, err
	}

	args, argSource, err := resolveArgs(in.Service, in.Method, in.Args, resolved.Method.ParamTypes, facade)
	if err != nil {
		return Plan{}, err
	}

	return Plan{
		SchemaVersion:  PlanSchemaVersion,
		Service:        in.Service,
		Method:         in.Method,
		ParamTypes:     resolved.Method.ParamTypes,
		ReturnType:     resolved.Method.ReturnType,
		Args:           args,
		Version:        strings.TrimSpace(in.Version),
		TargetAppName:  strings.TrimSpace(in.TargetAppName),
		Target:         report.Target,
		Overloads:      resolved.Overloads,
		Selected:       resolved.Selected,
		ContractSource: "facade-store",
		TargetLayers:   report.Layers,
		ArgSource:      argSource,
	}, nil
}

// buildTrustedPlan is the facade-less path. The agent must supply
// service/method plus complete paramTypes + args — we cannot synthesize
// a skeleton or disambiguate overloads without an index. The error
// shapes deliberately mirror the facade-mode errors so MCP callers
// branch on the same codes regardless of which mode ran.
func buildTrustedPlan(in Input, report target.Report) (Plan, error) {
	if strings.TrimSpace(in.Service) == "" {
		return Plan{}, errcode.New(errcode.ServiceMissing, "invoke",
			"service is required").
			WithHint("sofarpc_open", nil,
				"open a workspace or pass service on the invoke call")
	}
	if strings.TrimSpace(in.Method) == "" {
		return Plan{}, errcode.New(errcode.MethodMissing, "invoke",
			"method is required").
			WithHint("sofarpc_describe",
				map[string]any{"service": in.Service},
				"describe the service to see its methods")
	}
	if len(in.ParamTypes) == 0 {
		return Plan{}, errcode.New(errcode.FacadeNotConfigured, "invoke",
			"contract information is not attached; pass paramTypes on the invoke call to proceed in trusted mode").
			WithHint("sofarpc_doctor", nil,
				"doctor reports whether this workspace can describe methods or must use trusted-mode invoke")
	}
	if in.Args == nil {
		return Plan{}, errcode.New(errcode.FacadeNotConfigured, "invoke",
			"contract information is not attached; pass args on the invoke call — trusted mode cannot synthesize a skeleton").
			WithHint("sofarpc_doctor", nil,
				"doctor reports whether this workspace can describe methods or must use trusted-mode invoke")
	}
	args, err := coerceArgsVector(in.Args, len(in.ParamTypes))
	if err != nil {
		return Plan{}, errcode.New(errcode.ArgsInvalid, "invoke",
			fmt.Sprintf("trusted mode expects %d arg(s): %v", len(in.ParamTypes), err)).
			WithHint("sofarpc_describe",
				describeHintArgs(in.Service, in.Method),
				"align args shape with paramTypes")
	}
	if len(args) != len(in.ParamTypes) {
		return Plan{}, errcode.New(errcode.ArgsInvalid, "invoke",
			fmt.Sprintf("arity mismatch: got %d args, paramTypes has %d", len(args), len(in.ParamTypes))).
			WithHint("sofarpc_describe",
				describeHintArgs(in.Service, in.Method),
				"align args length with paramTypes")
	}
	return Plan{
		SchemaVersion:  PlanSchemaVersion,
		Service:        in.Service,
		Method:         in.Method,
		ParamTypes:     in.ParamTypes,
		Args:           args,
		Version:        strings.TrimSpace(in.Version),
		TargetAppName:  strings.TrimSpace(in.TargetAppName),
		Target:         report.Target,
		ContractSource: "trusted",
		TargetLayers:   report.Layers,
		ArgSource:      "user",
	}, nil
}

// resolveArgs chooses the arg vector that will go on the wire.
//
//   - user args == nil          → render skeleton from paramTypes.
//   - len(user) == len(types)   → pass through verbatim (user retains @type duty).
//   - len(user) != len(types)   → input.args-invalid with an explicit message.
//
// argSource is "user" or "skeleton" so the MCP output can say which path
// was taken without re-deriving it.
//
// service/method are threaded through only to pre-fill the hint's
// NextArgs on arity errors — passing empty strings would give the agent
// a hint it can't follow, so an empty value is dropped from NextArgs
// rather than emitted.
func resolveArgs(service, method string, userArgs any, paramTypes []string, facade contract.Store) ([]any, string, error) {
	if userArgs == nil {
		skeleton := contract.BuildSkeleton(paramTypes, facade)
		return decodeSkeleton(skeleton), "skeleton", nil
	}
	vector, err := coerceArgsVector(userArgs, len(paramTypes))
	if err != nil {
		return nil, "", errcode.New(errcode.ArgsInvalid, "invoke",
			fmt.Sprintf("method takes %d arg(s): %v", len(paramTypes), err)).
			WithHint("sofarpc_describe",
				describeHintArgs(service, method),
				"describe the method to see its paramTypes")
	}
	if len(vector) != len(paramTypes) {
		return nil, "", errcode.New(errcode.ArgsInvalid, "invoke",
			fmt.Sprintf("arity mismatch: got %d args, method takes %d", len(vector), len(paramTypes))).
			WithHint("sofarpc_describe",
				describeHintArgs(service, method),
				"describe the method to see its paramTypes")
	}
	normalized, err := contract.NormalizeArgs(paramTypes, vector, facade)
	if err != nil {
		return nil, "", errcode.New(errcode.ArgsInvalid, "invoke",
			fmt.Sprintf("normalize args: %v", err)).
			WithHint("sofarpc_describe",
				describeHintArgs(service, method),
				"describe the method to inspect field types or pass a canonical payload")
	}
	return normalized, "user", nil
}

func coerceArgsVector(raw any, arity int) ([]any, error) {
	if raw == nil {
		return nil, nil
	}
	if values, ok := raw.([]any); ok {
		return values, nil
	}
	if arity == 1 {
		return []any{raw}, nil
	}
	return nil, fmt.Errorf("pass args as a JSON array for multi-arg methods, got %T", raw)
}

// describeHintArgs builds the NextArgs payload for a describe hint. We
// only include fields that are non-empty — a hint the agent can actually
// run — and return nil when there is nothing useful to pre-fill.
func describeHintArgs(service, method string) map[string]any {
	if service == "" && method == "" {
		return nil
	}
	args := map[string]any{}
	if service != "" {
		args["service"] = service
	}
	if method != "" {
		args["method"] = method
	}
	return args
}

// decodeSkeleton converts []json.RawMessage into []any so the MCP
// SDK's schema inference doesn't mistake the bytes for a byte array.
func decodeSkeleton(raw []json.RawMessage) []any {
	out := make([]any, len(raw))
	for i, r := range raw {
		var v any
		if err := json.Unmarshal(r, &v); err != nil {
			v = string(r)
		}
		out[i] = v
	}
	return out
}
