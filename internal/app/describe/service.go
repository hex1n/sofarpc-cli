package describe

import (
	"context"

	"github.com/hex1n/sofarpc-cli/internal/app/shared"
	"github.com/hex1n/sofarpc-cli/internal/model"
	"github.com/hex1n/sofarpc-cli/internal/runtime"
	"github.com/hex1n/sofarpc-cli/internal/targetmodel"
)

type Deps struct {
	ResolveLocalServiceSchemaDetailed func(context.Context, string, string, bool) (shared.LocalSchemaResolution, error)
	LoadManifest                      func(string) (targetmodel.Manifest, bool, error)
	ResolveStubPaths                  func(cwd, manifestPath string, manifestPaths []string, rawCSV, service string) ([]string, error)
	ResolveSofaRPCVersion             func(flagValue, manifestValue string) (string, string)
	ResolveSpec                       func(javaBin, runtimeJar, version string, stubPaths []string) (runtime.Spec, error)
	DescribeServiceLegacyFallback     func(context.Context, runtime.Spec, string, runtime.DescribeOptions) (model.ServiceSchema, error)
	ContractSourceLabel               func(string) string
	WorkerClasspathMode               func([]string) string
}

type Request struct {
	Cwd            string
	ManifestPath   string
	StubPathCSV    string
	SofaRPCVersion string
	JavaBin        string
	RuntimeJar     string
	Service        string
	Refresh        bool
}

type Result struct {
	Schema      *model.ServiceSchema
	Diagnostics model.DiagnosticInfo
}

func (d Deps) Execute(ctx context.Context, req Request) (Result, error) {
	localResolution, localErr := d.ResolveLocalServiceSchemaDetailed(ctx, req.ManifestPath, req.Service, req.Refresh)
	if localErr == nil {
		return Result{
			Schema: &localResolution.Schema,
			Diagnostics: model.DiagnosticInfo{
				ContractSource:   d.ContractSourceLabel(localResolution.Source),
				ContractCacheHit: localResolution.CacheHit,
				ContractNotes:    localResolution.Notes,
			},
		}, nil
	}

	manifest, _, err := d.LoadManifest(req.ManifestPath)
	if err != nil {
		return Result{
			Diagnostics: model.DiagnosticInfo{
				ContractNotes: localResolution.Notes,
			},
		}, err
	}
	stubPaths, err := d.ResolveStubPaths(req.Cwd, req.ManifestPath, manifest.StubPaths, req.StubPathCSV, req.Service)
	if err != nil {
		return Result{
			Diagnostics: model.DiagnosticInfo{
				ContractNotes: localResolution.Notes,
			},
		}, err
	}
	version, _ := d.ResolveSofaRPCVersion(req.SofaRPCVersion, manifest.SofaRPCVersion)
	javaBin := req.JavaBin
	if javaBin == "" {
		javaBin = "java"
	}
	spec, err := d.ResolveSpec(javaBin, req.RuntimeJar, version, stubPaths)
	if err != nil {
		return Result{
			Diagnostics: model.DiagnosticInfo{
				ContractNotes:   localResolution.Notes,
				WorkerClasspath: d.WorkerClasspathMode(stubPaths),
			},
		}, err
	}
	schema, err := d.DescribeServiceLegacyFallback(ctx, spec, req.Service, runtime.DescribeOptions{Refresh: req.Refresh})
	diagnostics := model.DiagnosticInfo{
		ContractSource:  "legacy-worker-describe",
		ContractNotes:   localResolution.Notes,
		WorkerClasspath: d.WorkerClasspathMode(stubPaths),
		RuntimeJar:      spec.RuntimeJar,
		RuntimeVersion:  spec.SofaRPCVersion,
		JavaBin:         spec.JavaBin,
		JavaMajor:       spec.JavaMajor,
		DaemonKey:       spec.DaemonKey,
	}
	if err != nil {
		return Result{Diagnostics: diagnostics}, err
	}
	return Result{
		Schema:      &schema,
		Diagnostics: diagnostics,
	}, nil
}
