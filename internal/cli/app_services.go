package cli

import (
	"context"
	"io"

	appdescribe "github.com/hex1n/sofarpc-cli/internal/app/describe"
	appdoctor "github.com/hex1n/sofarpc-cli/internal/app/doctor"
	appfacade "github.com/hex1n/sofarpc-cli/internal/app/facade"
	appinvoke "github.com/hex1n/sofarpc-cli/internal/app/invoke"
	appsession "github.com/hex1n/sofarpc-cli/internal/app/session"
	apptarget "github.com/hex1n/sofarpc-cli/internal/app/target"
	"github.com/hex1n/sofarpc-cli/internal/config"
	"github.com/hex1n/sofarpc-cli/internal/facadeconfig"
	"github.com/hex1n/sofarpc-cli/internal/facadeindex"
	"github.com/hex1n/sofarpc-cli/internal/facadereplay"
	"github.com/hex1n/sofarpc-cli/internal/facadeschema"
	"github.com/hex1n/sofarpc-cli/internal/facadesemantic"
	"github.com/hex1n/sofarpc-cli/internal/model"
	"github.com/hex1n/sofarpc-cli/internal/runtime"
)

func (a *App) newInvokeExecutor() appinvoke.Deps {
	return appinvoke.Deps{
		ResolveInvocation:            a.resolveInvocation,
		ApplyProjectMethodContract:   a.applyProjectMethodContract,
		ResolveSpec:                  a.Runtime.ResolveSpec,
		ResolveServiceSchemaDetailed: a.resolveServiceSchemaDetailed,
		PickMethodSchema:             pickMethodSchema,
		MaybeWrapSingleArg:           maybeWrapSingleArg,
		EnsureDaemon:                 a.Runtime.EnsureDaemon,
		Invoke:                       a.Runtime.Invoke,
		RandomID:                     randomID,
		ContractSourceLabel:          contractSourceLabel,
		WorkerClasspathMode:          workerClasspathMode,
	}
}

func (a *App) newDescribeResolver() appdescribe.Deps {
	return appdescribe.Deps{
		ResolveLocalServiceSchemaDetailed: a.resolveLocalServiceSchemaDetailed,
		LoadManifest:                      config.LoadManifest,
		ResolveStubPaths:                  resolveStubPaths,
		ResolveSofaRPCVersion:             resolveSofaRPCVersion,
		ResolveSpec:                       a.Runtime.ResolveSpec,
		DescribeServiceLegacyFallback: func(ctx context.Context, spec runtime.Spec, service string, opts runtime.DescribeOptions) (model.ServiceSchema, error) {
			return a.Runtime.DescribeServiceLegacyFallback(ctx, spec, service, opts)
		},
		ContractSourceLabel: contractSourceLabel,
		WorkerClasspathMode: workerClasspathMode,
	}
}

func (a *App) newDoctorService() appdoctor.Deps {
	return appdoctor.Deps{
		ResolveInvocation:       a.resolveInvocation,
		PrepareDoctorInvocation: a.prepareDoctorInvocation,
		ResolveSpec:             a.Runtime.ResolveSpec,
		ScanStubWarnings:        runtime.ScanStubWarnings,
		ProbeTarget:             runtime.ProbeTarget,
		EnsureDaemon:            a.Runtime.EnsureDaemon,
		Invoke:                  a.Runtime.Invoke,
		SummarizeInvokeProbe:    summarizeInvokeProbe,
		RandomID:                randomID,
		ContractSourceLabel:     contractSourceLabel,
		WorkerClasspathMode:     workerClasspathMode,
	}
}

func (a *App) newTargetService() apptarget.Deps {
	return apptarget.Deps{
		LoadManifest:             config.LoadManifest,
		LoadContextStore:         config.LoadContextStore,
		ResolveTargetProjectRoot: resolveTargetProjectRoot,
		ProbeTarget:              runtime.ProbeTarget,
	}
}

func (a *App) newSessionService() appsession.Deps {
	return appsession.Deps{
		Store:               a.Sessions,
		NewID:               randomID,
		ResolveProjectRoot:  resolveTargetProjectRoot,
		ResolveManifestPath: resolveManifestPath,
		LoadManifest:        config.LoadManifest,
		LoadContextStore:    config.LoadContextStore,
		InspectFacadeState:  facadeconfig.InspectState,
		LoadFacadeConfig:    facadeconfig.LoadConfig,
		Runtime:             a.Runtime,
		Metadata:            a.Metadata,
	}
}

func (a *App) newFacadeService() appfacade.Deps {
	return appfacade.Deps{
		ResolveProjectRoot: func(cwd, project string) (string, error) {
			return resolveFacadeProjectRoot(cwd, a.Stderr, project)
		},
		FindSkillDir:         facadeSkillDir,
		InspectState:         facadeconfig.InspectState,
		LoadConfig:           facadeconfig.LoadConfig,
		LoadServiceSummary:   loadFacadeServiceSummary,
		IterSourceRoots:      facadeconfig.IterSourceRoots,
		LoadSemanticRegistry: facadesemantic.LoadSemanticRegistry,
		BuildMethodSchema: func(registry facadesemantic.Registry, service, method string, preferredParamTypes []string, markers []string) (facadeschema.MethodSchemaEnvelope, error) {
			return facadeschema.BuildMethodSchema(registry, service, method, preferredParamTypes, markers)
		},
		DetectConfig: facadeconfig.DetectConfig,
		RefreshIndex: facadeindex.RefreshIndex,
		ReplayCalls:  facadereplay.ReplayCalls,
	}
}

func writeDescribeFailureReport(stderr io.Writer, err error, diagnostics model.DiagnosticInfo) error {
	report := model.DescribeReport{
		Error:       err.Error(),
		Diagnostics: diagnostics,
	}
	if printErr := printJSON(stderr, report); printErr != nil {
		return printErr
	}
	return &exitError{message: "describe failed", silent: true}
}

func buildSuccessDescribeReport(schema model.ServiceSchema, diagnostics model.DiagnosticInfo) model.DescribeReport {
	return model.DescribeReport{
		Schema:      &schema,
		Diagnostics: diagnostics,
	}
}
