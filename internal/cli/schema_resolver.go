package cli

import (
	"context"
	"errors"

	"github.com/hex1n/sofarpc-cli/internal/contract"
	"github.com/hex1n/sofarpc-cli/internal/model"
	"github.com/hex1n/sofarpc-cli/internal/runtime"
)

var describeServiceFromProject = contract.DescribeServiceFromProject
var describeServiceFromArtifacts = contract.DescribeServiceFromArtifacts
var describeServiceLegacyFallback = func(ctx context.Context, manager *runtime.Manager, spec runtime.Spec, service string, opts runtime.DescribeOptions) (model.ServiceSchema, error) {
	return manager.DescribeServiceLegacyFallback(ctx, spec, service, opts)
}
var errLocalSchemaUnavailable = errors.New("local schema unavailable")

type localSchemaResolution struct {
	Schema   model.ServiceSchema
	Source   string
	CacheHit bool
	Notes    []string
}

type serviceSchemaResolution struct {
	Schema   model.ServiceSchema
	Source   string
	CacheHit bool
	Notes    []string
}

func (a *App) resolveLocalServiceSchema(ctx context.Context, manifestPath, service string, refresh bool) (model.ServiceSchema, error) {
	resolution, err := a.resolveLocalServiceSchemaDetailed(ctx, manifestPath, service, refresh)
	if err != nil {
		return model.ServiceSchema{}, err
	}
	return resolution.Schema, nil
}

func (a *App) resolveLocalServiceSchemaDetailed(ctx context.Context, manifestPath, service string, refresh bool) (localSchemaResolution, error) {
	projectRoot := projectAwareRoot(a.Cwd, manifestPath)
	notes := []string{}
	if a.Metadata != nil {
		if schema, source, cacheHit, metadataNotes, err := a.Metadata.ResolveServiceSchema(ctx, projectRoot, service, refresh); err == nil {
			return localSchemaResolution{
				Schema:   schema,
				Source:   source,
				CacheHit: cacheHit,
				Notes:    appendContractNotes(notes, metadataNotes...),
			}, nil
		} else {
			if len(metadataNotes) > 0 {
				notes = appendContractNotes(notes, metadataNotes...)
			} else {
				notes = appendContractNotes(notes, contractFailureNote("metadata-daemon", err))
			}
		}
	}
	if schema, err := describeServiceFromProject(projectRoot, service); err == nil {
		return localSchemaResolution{
			Schema: schema,
			Source: "project-source",
			Notes:  notes,
		}, nil
	} else {
		notes = appendContractNotes(notes, contractFailureNote("project-source", err))
	}
	if schema, err := describeServiceFromArtifacts(projectRoot, service); err == nil {
		return localSchemaResolution{
			Schema: schema,
			Source: "jar-javap",
			Notes:  notes,
		}, nil
	} else {
		notes = appendContractNotes(notes, contractFailureNote("jar-javap", err))
	}
	return localSchemaResolution{Notes: notes}, errLocalSchemaUnavailable
}

func (a *App) resolveServiceSchemaDetailed(ctx context.Context, manifestPath string, spec runtime.Spec, service string, opts runtime.DescribeOptions) (serviceSchemaResolution, error) {
	if resolution, err := a.resolveLocalServiceSchemaDetailed(ctx, manifestPath, service, opts.Refresh || opts.NoCache); err == nil {
		return serviceSchemaResolution{
			Schema:   resolution.Schema,
			Source:   resolution.Source,
			CacheHit: resolution.CacheHit,
			Notes:    resolution.Notes,
		}, nil
	}
	schema, err := describeServiceLegacyFallback(ctx, a.Runtime, spec, service, opts)
	if err != nil {
		return serviceSchemaResolution{}, err
	}
	return serviceSchemaResolution{
		Schema: schema,
		Source: "legacy-worker-describe",
	}, nil
}

func (a *App) resolveServiceSchema(ctx context.Context, manifestPath string, spec runtime.Spec, service string, opts runtime.DescribeOptions) (model.ServiceSchema, error) {
	resolution, err := a.resolveServiceSchemaDetailed(ctx, manifestPath, spec, service, opts)
	if err == nil {
		return resolution.Schema, nil
	}
	return model.ServiceSchema{}, err
}
