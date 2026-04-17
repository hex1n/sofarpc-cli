package cli

import (
	"context"
	"fmt"

	"github.com/hex1n/sofarpc-cli/internal/config"
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
	)
	flags.StringVar(&manifestPath, "manifest", "", "manifest file path")
	flags.StringVar(&stubPathCSV, "stub-path", "", "comma-separated stub paths")
	flags.StringVar(&sofaRPCVersion, "sofa-rpc-version", "", "runtime SOFARPC version")
	flags.StringVar(&javaBin, "java-bin", "", "java executable")
	flags.StringVar(&runtimeJar, "runtime-jar", "", "worker runtime jar")
	flags.BoolVar(&refresh, "refresh", false, "bypass schema cache and re-run worker")
	if err := flags.Parse(args); err != nil {
		return err
	}
	positionals := flags.Args()
	if len(positionals) != 1 {
		return fmt.Errorf("describe requires exactly one service FQCN")
	}
	service := positionals[0]

	resolvedManifest := resolveManifestPath(a.Cwd, manifestPath)
	if schema, err := a.resolveLocalServiceSchema(context.Background(), resolvedManifest, service, refresh); err == nil {
		return printJSON(a.Stdout, schema)
	}
	manifest, _, err := config.LoadManifest(resolvedManifest)
	if err != nil {
		return err
	}
	stubPaths, err := resolveStubPaths(a.Cwd, resolvedManifest, manifest.StubPaths, stubPathCSV, service)
	if err != nil {
		return err
	}
	version, _ := resolveSofaRPCVersion(sofaRPCVersion, manifest.SofaRPCVersion)
	if javaBin == "" {
		javaBin = "java"
	}
	spec, err := a.Runtime.ResolveSpec(javaBin, runtimeJar, version, stubPaths)
	if err != nil {
		return err
	}
	schema, err := a.Runtime.DescribeService(context.Background(), spec, service, runtime.DescribeOptions{Refresh: refresh})
	if err != nil {
		return err
	}
	return printJSON(a.Stdout, schema)
}
