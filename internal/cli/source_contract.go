package cli

import (
	"context"
	"encoding/json"

	"github.com/hex1n/sofarpc-cli/internal/contract"
	"github.com/hex1n/sofarpc-cli/internal/model"
)

var (
	resolveProjectMethodContract  = contract.ResolveMethodFromProject
	resolveArtifactMethodContract = contract.ResolveMethodFromArtifacts
	compileProjectMethodArgs      = contract.CompileProjectMethodArgs
)

func (a *App) applyProjectMethodContract(ctx context.Context, resolved *resolvedInvocation, refresh bool) (string, bool, error) {
	projectRoot := projectAwareRoot(a.Cwd, resolved.ManifestPath)
	if a.Metadata != nil {
		methodContract, source, cacheHit, err := a.Metadata.ResolveMethod(
			ctx,
			projectRoot,
			resolved.Request.Service,
			resolved.Request.Method,
			resolved.Request.ParamTypes,
			resolved.Request.Args,
			refresh,
		)
		if err == nil {
			return applyResolvedMethodContract(resolved, methodContract, source, cacheHit)
		}
	}
	methodContract, err := resolveProjectMethodContract(
		projectRoot,
		resolved.Request.Service,
		resolved.Request.Method,
		resolved.Request.ParamTypes,
		resolved.Request.Args,
	)
	if err == nil {
		return applyResolvedMethodContract(resolved, methodContract, "project-source", false)
	}
	methodContract, err = resolveArtifactMethodContract(
		projectRoot,
		resolved.Request.Service,
		resolved.Request.Method,
		resolved.Request.ParamTypes,
		resolved.Request.Args,
	)
	if err == nil {
		return applyResolvedMethodContract(resolved, methodContract, "jar-javap", false)
	}
	return "", false, nil
}

func applyResolvedMethodContract(resolved *resolvedInvocation, methodContract contract.ProjectMethod, source string, cacheHit bool) (string, bool, error) {
	resolved.Request.ParamTypes = append([]string{}, methodContract.Schema.ParamTypes...)
	resolved.Request.ParamTypeSignatures = append([]string{}, methodContract.Schema.ParamTypeSignatures...)
	if wrapped, ok := maybeWrapSingleArg(resolved.Request.Args, len(resolved.Request.ParamTypes)); ok {
		resolved.Request.Args = wrapped
	}
	compiled, err := compileProjectMethodArgs(resolved.Request.Args, methodContract)
	if err != nil {
		return "", false, err
	}
	resolved.Request.Args = json.RawMessage(compiled)
	resolved.Request.PayloadMode = model.PayloadGeneric
	resolved.StubPaths = nil
	return source, cacheHit, nil
}
