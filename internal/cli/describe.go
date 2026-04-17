package cli

import (
	"context"
	"fmt"

	"github.com/hex1n/sofarpc-cli/internal/config"
	"github.com/hex1n/sofarpc-cli/internal/model"
	"github.com/hex1n/sofarpc-cli/internal/runtime"
)

func (a *App) runDescribe(args []string) error {
	flags := failFlagSet("describe")
	var (
		manifestPath   string
		stubPathCSV    string
		sofaRPCVersion string
		javaBin        string
		runtimeJar     string
		refresh        bool
		fullResponse   bool
	)
	flags.StringVar(&manifestPath, "manifest", "", "manifest file path")
	flags.StringVar(&stubPathCSV, "stub-path", "", "manual fallback stub paths (debug only when local resolution misses)")
	flags.StringVar(&sofaRPCVersion, "sofa-rpc-version", "", "runtime SOFARPC version")
	flags.StringVar(&javaBin, "java-bin", "", "java executable")
	flags.StringVar(&runtimeJar, "runtime-jar", "", "worker runtime jar")
	flags.BoolVar(&refresh, "refresh", false, "bypass schema cache and re-resolve local or legacy fallback schema")
	flags.BoolVar(&fullResponse, "full-response", false, "print schema plus local resolution diagnostics")
	if err := flags.Parse(args); err != nil {
		return err
	}
	positionals := flags.Args()
	if len(positionals) != 1 {
		return fmt.Errorf("describe requires exactly one service FQCN")
	}
	service := positionals[0]

	resolvedManifest := resolveManifestPath(a.Cwd, manifestPath)
	localResolution, localErr := a.resolveLocalServiceSchemaDetailed(context.Background(), resolvedManifest, service, refresh)
	if localErr == nil {
		if fullResponse {
			return printJSON(a.Stdout, model.DescribeReport{
				Schema: &localResolution.Schema,
				Diagnostics: model.DiagnosticInfo{
					ContractSource:   contractSourceLabel(localResolution.Source),
					ContractCacheHit: localResolution.CacheHit,
					ContractNotes:    localResolution.Notes,
				},
			})
		}
		return printJSON(a.Stdout, localResolution.Schema)
	}
	manifest, _, err := config.LoadManifest(resolvedManifest)
	if err != nil {
		if fullResponse {
			return a.writeDescribeFailure(err, localResolution.Notes, nil, nil, "")
		}
		return err
	}
	stubPaths, err := resolveStubPaths(a.Cwd, resolvedManifest, manifest.StubPaths, stubPathCSV, service)
	if err != nil {
		if fullResponse {
			return a.writeDescribeFailure(err, localResolution.Notes, nil, nil, "")
		}
		return err
	}
	version, _ := resolveSofaRPCVersion(sofaRPCVersion, manifest.SofaRPCVersion)
	if javaBin == "" {
		javaBin = "java"
	}
	spec, err := a.Runtime.ResolveSpec(javaBin, runtimeJar, version, stubPaths)
	if err != nil {
		if fullResponse {
			return a.writeDescribeFailure(err, localResolution.Notes, stubPaths, nil, "")
		}
		return err
	}
	schema, err := a.Runtime.DescribeServiceLegacyFallback(context.Background(), spec, service, runtime.DescribeOptions{Refresh: refresh})
	if err != nil {
		if fullResponse {
			return a.writeDescribeFailure(err, localResolution.Notes, stubPaths, &spec, "legacy-worker-describe")
		}
		return err
	}
	if fullResponse {
		return printJSON(a.Stdout, model.DescribeReport{
			Schema: &schema,
			Diagnostics: model.DiagnosticInfo{
				ContractSource:  "legacy-worker-describe",
				ContractNotes:   localResolution.Notes,
				WorkerClasspath: workerClasspathMode(stubPaths),
				RuntimeJar:      spec.RuntimeJar,
				RuntimeVersion:  spec.SofaRPCVersion,
				JavaBin:         spec.JavaBin,
				JavaMajor:       spec.JavaMajor,
				DaemonKey:       spec.DaemonKey,
			},
		})
	}
	return printJSON(a.Stdout, schema)
}

func (a *App) writeDescribeFailure(err error, contractNotes []string, stubPaths []string, spec *runtime.Spec, contractSource string) error {
	report := model.DescribeReport{
		Error: err.Error(),
		Diagnostics: model.DiagnosticInfo{
			ContractSource:  contractSource,
			ContractNotes:   contractNotes,
			WorkerClasspath: workerClasspathMode(stubPaths),
		},
	}
	if spec != nil {
		report.Diagnostics.RuntimeJar = spec.RuntimeJar
		report.Diagnostics.RuntimeVersion = spec.SofaRPCVersion
		report.Diagnostics.JavaBin = spec.JavaBin
		report.Diagnostics.JavaMajor = spec.JavaMajor
		report.Diagnostics.DaemonKey = spec.DaemonKey
	}
	if printErr := printJSON(a.Stderr, report); printErr != nil {
		return printErr
	}
	return &exitError{message: "describe failed", silent: true}
}
