package invoke

import (
	"context"
	"encoding/json"

	"github.com/hex1n/sofarpc-cli/internal/app/shared"
	"github.com/hex1n/sofarpc-cli/internal/model"
	"github.com/hex1n/sofarpc-cli/internal/runtime"
)

type Deps struct {
	ResolveInvocation            func(shared.InvocationInputs) (shared.ResolvedInvocation, error)
	ApplyProjectMethodContract   func(context.Context, *shared.ResolvedInvocation, bool) (string, bool, []string, error)
	ResolveSpec                  func(javaBin, runtimeJar, version string, stubPaths []string) (runtime.Spec, error)
	ResolveServiceSchemaDetailed func(context.Context, string, runtime.Spec, string, runtime.DescribeOptions) (shared.ServiceSchemaResolution, error)
	PickMethodSchema             func(model.ServiceSchema, string, json.RawMessage, []string) (model.MethodSchema, error)
	MaybeWrapSingleArg           func(json.RawMessage, int) (json.RawMessage, bool)
	EnsureDaemon                 func(context.Context, runtime.Spec) (model.DaemonMetadata, error)
	Invoke                       func(context.Context, model.DaemonMetadata, model.InvocationRequest) (model.InvocationResponse, error)
	RandomID                     func() string
	ContractSourceLabel          func(string) string
	WorkerClasspathMode          func([]string) string
}

type Request struct {
	Input shared.InvocationInputs
}

type Result struct {
	Response model.InvocationResponse
	Pretty   any
	OKOnly   bool
}

func (d Deps) Execute(ctx context.Context, req Request) (Result, error) {
	plan, err := d.Plan(ctx, req)
	if err != nil {
		return Result{}, err
	}
	return d.ExecutePlan(ctx, plan)
}

func (d Deps) ExecutePlan(ctx context.Context, plan Plan) (Result, error) {
	metadata, err := d.EnsureDaemon(ctx, plan.Spec)
	if err != nil {
		return Result{}, err
	}
	response, err := d.Invoke(ctx, metadata, plan.Request)
	if err != nil {
		return Result{}, err
	}
	response.Diagnostics.RuntimeJar = plan.Spec.RuntimeJar
	response.Diagnostics.RuntimeVersion = plan.Spec.SofaRPCVersion
	response.Diagnostics.JavaBin = plan.Spec.JavaBin
	response.Diagnostics.JavaMajor = plan.Spec.JavaMajor
	response.Diagnostics.DaemonKey = plan.Spec.DaemonKey
	response.Diagnostics.ContractSource = plan.ContractSource
	response.Diagnostics.ContractCacheHit = plan.ContractCacheHit
	response.Diagnostics.ContractNotes = append([]string{}, plan.ContractNotes...)
	response.Diagnostics.WorkerClasspath = plan.WorkerClasspath
	result := Result{Response: response}
	if !response.OK {
		return result, nil
	}
	if len(response.Result) == 0 {
		result.OKOnly = true
		return result, nil
	}
	if err := json.Unmarshal(response.Result, &result.Pretty); err != nil {
		return Result{}, err
	}
	return result, nil
}
