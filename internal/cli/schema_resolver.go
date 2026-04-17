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
var errLocalSchemaUnavailable = errors.New("local schema unavailable")

func (a *App) resolveLocalServiceSchema(ctx context.Context, manifestPath, service string, refresh bool) (model.ServiceSchema, error) {
	projectRoot := projectAwareRoot(a.Cwd, manifestPath)
	if a.Metadata != nil {
		if schema, _, _, err := a.Metadata.ResolveServiceSchema(ctx, projectRoot, service, refresh); err == nil {
			return schema, nil
		}
	}
	if schema, err := describeServiceFromProject(projectRoot, service); err == nil {
		return schema, nil
	}
	if schema, err := describeServiceFromArtifacts(projectRoot, service); err == nil {
		return schema, nil
	}
	return model.ServiceSchema{}, errLocalSchemaUnavailable
}

func (a *App) resolveServiceSchema(ctx context.Context, manifestPath string, spec runtime.Spec, service string, opts runtime.DescribeOptions) (model.ServiceSchema, error) {
	if schema, err := a.resolveLocalServiceSchema(ctx, manifestPath, service, opts.Refresh || opts.NoCache); err == nil {
		return schema, nil
	}
	return a.Runtime.DescribeService(ctx, spec, service, opts)
}
