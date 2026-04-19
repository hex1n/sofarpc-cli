package ports

import (
	"context"

	"github.com/hex1n/sofarpc-cli/internal/model"
	"github.com/hex1n/sofarpc-cli/internal/runtime"
)

type RuntimeService interface {
	ResolveSpec(javaBin, runtimeJar, version string, stubPaths []string) (runtime.Spec, error)
	EnsureDaemon(context.Context, runtime.Spec) (model.DaemonMetadata, error)
	Invoke(context.Context, model.DaemonMetadata, model.InvocationRequest) (model.InvocationResponse, error)
	DescribeServiceLegacyFallback(context.Context, runtime.Spec, string, runtime.DescribeOptions) (model.ServiceSchema, error)
}
