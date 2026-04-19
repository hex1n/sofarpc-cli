package cli

import (
	"context"
	"fmt"

	appdescribe "github.com/hex1n/sofarpc-cli/internal/app/describe"
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
	result, err := a.newDescribeResolver().Execute(context.Background(), appdescribe.Request{
		Cwd:            a.Cwd,
		ManifestPath:   resolvedManifest,
		StubPathCSV:    stubPathCSV,
		SofaRPCVersion: sofaRPCVersion,
		JavaBin:        javaBin,
		RuntimeJar:     runtimeJar,
		Service:        service,
		Refresh:        refresh,
	})
	if err != nil {
		if fullResponse {
			return writeDescribeFailureReport(a.Stderr, err, result.Diagnostics)
		}
		return err
	}
	schema := result.Schema
	if fullResponse {
		return printJSON(a.Stdout, buildSuccessDescribeReport(*schema, result.Diagnostics))
	}
	return printJSON(a.Stdout, schema)
}
