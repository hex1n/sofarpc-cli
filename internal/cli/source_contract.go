package cli

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/hex1n/sofarpc-cli/internal/contract"
	"github.com/hex1n/sofarpc-cli/internal/model"
)

var (
	resolveProjectMethodContract  = contract.ResolveMethodFromProject
	resolveArtifactMethodContract = contract.ResolveMethodFromArtifacts
	compileProjectMethodArgs      = contract.CompileProjectMethodArgs
)

func (a *App) applyProjectMethodContract(ctx context.Context, resolved *resolvedInvocation, refresh bool) (string, bool, []string, error) {
	projectRoot := projectAwareRoot(a.Cwd, resolved.ManifestPath)
	notes := []string{}
	if a.Metadata != nil {
		methodContract, source, cacheHit, metadataNotes, err := a.Metadata.ResolveMethod(
			ctx,
			projectRoot,
			resolved.Request.Service,
			resolved.Request.Method,
			resolved.Request.ParamTypes,
			resolved.Request.Args,
			refresh,
		)
		notes = appendContractNotes(notes, metadataNotes...)
		if err == nil {
			return applyResolvedMethodContract(resolved, methodContract, source, cacheHit, notes)
		}
		if len(metadataNotes) == 0 {
			notes = appendContractNotes(notes, contractFailureNote("metadata-daemon", err))
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
		return applyResolvedMethodContract(resolved, methodContract, "project-source", false, notes)
	}
	notes = appendContractNotes(notes, contractFailureNote("project-source", err))
	methodContract, err = resolveArtifactMethodContract(
		projectRoot,
		resolved.Request.Service,
		resolved.Request.Method,
		resolved.Request.ParamTypes,
		resolved.Request.Args,
	)
	if err == nil {
		return applyResolvedMethodContract(resolved, methodContract, "jar-javap", false, notes)
	}
	notes = appendContractNotes(notes, contractFailureNote("jar-javap", err))
	return "", false, notes, nil
}

func applyResolvedMethodContract(resolved *resolvedInvocation, methodContract contract.ProjectMethod, source string, cacheHit bool, notes []string) (string, bool, []string, error) {
	resolved.Request.ParamTypes = append([]string{}, methodContract.Schema.ParamTypes...)
	resolved.Request.ParamTypeSignatures = append([]string{}, methodContract.Schema.ParamTypeSignatures...)
	if wrapped, ok := maybeWrapSingleArg(resolved.Request.Args, len(resolved.Request.ParamTypes)); ok {
		resolved.Request.Args = wrapped
	}
	compiled, err := compileProjectMethodArgs(resolved.Request.Args, methodContract)
	if err != nil {
		return "", false, notes, err
	}
	resolved.Request.Args = json.RawMessage(compiled)
	resolved.Request.PayloadMode = model.PayloadGeneric
	resolved.StubPaths = nil
	return source, cacheHit, notes, nil
}

func contractFailureNote(stage string, err error) string {
	if err == nil {
		return stage
	}
	text := strings.TrimSpace(err.Error())
	if idx := strings.IndexByte(text, '\n'); idx >= 0 {
		text = text[:idx]
	}
	if len(text) > 180 {
		text = text[:177] + "..."
	}
	return stage + ": " + text
}

func appendContractNotes(existing []string, more ...string) []string {
	seen := map[string]struct{}{}
	for _, item := range existing {
		seen[item] = struct{}{}
	}
	for _, item := range more {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		existing = append(existing, item)
	}
	return existing
}
