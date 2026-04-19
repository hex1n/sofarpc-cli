// Package invoke composes a generic-invoke Plan from the resolved target
// + the resolved contract + the agent's args. It does NOT send the
// payload — the worker driver (internal/worker, not yet built) does
// that. Separating plan from execution lets sofarpc_invoke support a
// cheap dryRun mode and lets tests cover plan correctness without a JVM.
package invoke

import (
	"encoding/json"
	"fmt"

	"github.com/hex1n/sofarpc-cli/internal/core/contract"
	"github.com/hex1n/sofarpc-cli/internal/core/target"
	"github.com/hex1n/sofarpc-cli/internal/errcode"
	"github.com/hex1n/sofarpc-cli/internal/facadesemantic"
)

// Input is what sofarpc_invoke passes in. Target fields are merged into
// target.Sources via target.Resolve — BuildPlan does that internally.
type Input struct {
	Service    string
	Method     string
	ParamTypes []string
	Args       []any
	Target     target.Input
}

// Plan is the wire-ready payload plus diagnostics. When dryRun is
// requested, sofarpc_invoke returns this verbatim; otherwise it hands
// the wire fields to the worker.
type Plan struct {
	Service        string                  `json:"service"`
	Method         string                  `json:"method"`
	ParamTypes     []string                `json:"paramTypes"`
	ReturnType     string                  `json:"returnType,omitempty"`
	Args           []any                   `json:"args"`
	Target         target.Config           `json:"target"`
	Overloads      []facadesemantic.Method `json:"overloads,omitempty"`
	Selected       int                     `json:"selected"`
	ContractSource string                  `json:"contractSource,omitempty"`
	TargetLayers   []target.Layer          `json:"targetLayers,omitempty"`
	ArgSource      string                  `json:"argSource,omitempty"`
}

// BuildPlan resolves target + contract + args and returns a Plan.
// It never performs I/O — callers have already materialised target.Sources
// and plugged a contract.Store.
//
// Failure modes (all *errcode.Error):
//   - target.missing: no layer supplied a target mode.
//   - workspace.facade-not-configured: facade is nil.
//   - contract.*: propagated from contract.ResolveMethod.
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
		return Plan{}, errcode.New(errcode.FacadeNotConfigured, "invoke",
			"facade index is not configured").
			WithHint("sofarpc_doctor", nil, "run doctor to inspect facade state")
	}

	resolved, err := contract.ResolveMethod(facade, in.Service, in.Method, in.ParamTypes)
	if err != nil {
		return Plan{}, err
	}

	args, argSource, err := resolveArgs(in.Args, resolved.Method.ParamTypes, facade)
	if err != nil {
		return Plan{}, err
	}

	return Plan{
		Service:        in.Service,
		Method:         in.Method,
		ParamTypes:     resolved.Method.ParamTypes,
		ReturnType:     resolved.Method.ReturnType,
		Args:           args,
		Target:         report.Target,
		Overloads:      resolved.Overloads,
		Selected:       resolved.Selected,
		ContractSource: "facade-store",
		TargetLayers:   report.Layers,
		ArgSource:      argSource,
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
func resolveArgs(userArgs []any, paramTypes []string, facade contract.Store) ([]any, string, error) {
	if userArgs == nil {
		skeleton := contract.BuildSkeleton(paramTypes, facade)
		return decodeSkeleton(skeleton), "skeleton", nil
	}
	if len(userArgs) != len(paramTypes) {
		return nil, "", errcode.New(errcode.ArgsInvalid, "invoke",
			fmt.Sprintf("arity mismatch: got %d args, method takes %d", len(userArgs), len(paramTypes))).
			WithHint("sofarpc_describe",
				map[string]any{"service": "", "method": ""},
				"describe the method to see its paramTypes")
	}
	return userArgs, "user", nil
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
