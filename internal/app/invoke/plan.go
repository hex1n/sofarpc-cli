package invoke

import (
	"context"

	"github.com/hex1n/sofarpc-cli/internal/model"
	"github.com/hex1n/sofarpc-cli/internal/runtime"
)

type Plan struct {
	Request          model.InvocationRequest
	Spec             runtime.Spec
	ContractSource   string
	ContractCacheHit bool
	ContractNotes    []string
	WorkerClasspath  string
	Method           *model.MethodSchema
	WrappedSingleArg bool
}

func (d Deps) Plan(ctx context.Context, req Request) (Plan, error) {
	resolved, err := d.ResolveInvocation(req.Input)
	if err != nil {
		return Plan{}, err
	}
	contractSource, contractCacheHit, contractNotes, err := d.ApplyProjectMethodContract(ctx, &resolved, req.Input.RefreshContract)
	if err != nil {
		return Plan{}, err
	}
	spec, err := d.ResolveSpec(resolved.JavaBin, resolved.RuntimeJar, resolved.SofaRPCVersion, resolved.StubPaths)
	if err != nil {
		return Plan{}, err
	}

	var selectedMethod *model.MethodSchema
	if contractSource == "" && (len(resolved.Request.ParamTypes) == 0 || resolved.Request.PayloadMode == model.PayloadSchema) {
		schemaResolution, describeErr := d.ResolveServiceSchemaDetailed(
			ctx,
			resolved.ManifestPath,
			spec,
			resolved.Request.Service,
			runtime.DescribeOptions{NoCache: req.Input.RefreshContract},
		)
		if describeErr != nil {
			return Plan{}, describeErr
		}
		methodSchema, err := d.PickMethodSchema(
			schemaResolution.Schema,
			resolved.Request.Method,
			resolved.Request.Args,
			resolved.Request.ParamTypes,
		)
		if err != nil {
			return Plan{}, err
		}
		selected := methodSchema
		selectedMethod = &selected
		if len(resolved.Request.ParamTypes) == 0 {
			resolved.Request.ParamTypes = methodSchema.ParamTypes
		}
		if resolved.Request.PayloadMode == model.PayloadSchema {
			resolved.Request.ParamTypeSignatures = methodSchema.ParamTypeSignatures
		}
		if contractSource == "" && schemaResolution.Source != "" {
			contractSource = schemaResolution.Source
		}
		if !contractCacheHit && schemaResolution.CacheHit {
			contractCacheHit = true
		}
		if len(contractNotes) == 0 && len(schemaResolution.Notes) > 0 {
			contractNotes = append([]string{}, schemaResolution.Notes...)
		}
	}
	if resolved.Request.PayloadMode == model.PayloadSchema && len(resolved.Request.ParamTypeSignatures) == 0 {
		resolved.Request.ParamTypeSignatures = append([]string{}, resolved.Request.ParamTypes...)
	}

	wrappedSingleArg := false
	if contractSource == "" {
		if wrapped, ok := d.MaybeWrapSingleArg(resolved.Request.Args, len(resolved.Request.ParamTypes)); ok {
			resolved.Request.Args = wrapped
			wrappedSingleArg = true
		}
	}

	resolved.Request.RequestID = d.RandomID()
	return Plan{
		Request:          resolved.Request,
		Spec:             spec,
		ContractSource:   d.ContractSourceLabel(contractSource),
		ContractCacheHit: contractCacheHit,
		ContractNotes:    append([]string{}, contractNotes...),
		WorkerClasspath:  d.WorkerClasspathMode(resolved.StubPaths),
		Method:           selectedMethod,
		WrappedSingleArg: wrappedSingleArg,
	}, nil
}
