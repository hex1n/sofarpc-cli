// sofarpc-mcp serves the sofarpc-cli tools over MCP stdio. The single
// entrypoint is intentional: agents load one server, call six tools. See
// docs/architecture.md for the design.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"syscall"

	"github.com/hex1n/sofarpc-cli/internal/core/target"
	mcpserver "github.com/hex1n/sofarpc-cli/internal/mcp"
	"github.com/hex1n/sofarpc-cli/internal/sourcecontract"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	projectRoot := projectRootFromEnv()
	server := mcpserver.New(mcpserver.Options{
		TargetSources: target.Sources{Env: envConfig(), ProjectRoot: projectRoot},
		Facade:        loadFacade(projectRoot),
	})
	return server.Run(ctx, &sdkmcp.StdioTransport{})
}

// projectRootFromEnv picks the directory the server should anchor to.
// SOFARPC_PROJECT_ROOT wins; otherwise we use the process CWD. Empty
// string is acceptable — handlers that need a project root resolve it
// per-call via the workspace package.
func projectRootFromEnv() string {
	if v := os.Getenv("SOFARPC_PROJECT_ROOT"); v != "" {
		return v
	}
	wd, err := os.Getwd()
	if err != nil {
		return ""
	}
	return wd
}

// envConfig reads the SOFARPC_* environment into a target.Config. Only
// fields that are set contribute to resolution; everything else stays
// empty so the defaults layer can fill it.
func envConfig() target.Config {
	cfg := target.Config{
		DirectURL:        os.Getenv("SOFARPC_DIRECT_URL"),
		RegistryAddress:  os.Getenv("SOFARPC_REGISTRY_ADDRESS"),
		RegistryProtocol: os.Getenv("SOFARPC_REGISTRY_PROTOCOL"),
		Protocol:         os.Getenv("SOFARPC_PROTOCOL"),
		Serialization:    os.Getenv("SOFARPC_SERIALIZATION"),
		UniqueID:         os.Getenv("SOFARPC_UNIQUE_ID"),
		TimeoutMS:        atoiOrZero(os.Getenv("SOFARPC_TIMEOUT_MS")),
		ConnectTimeoutMS: atoiOrZero(os.Getenv("SOFARPC_CONNECT_TIMEOUT_MS")),
	}
	if cfg.DirectURL != "" {
		cfg.Mode = target.ModeDirect
	} else if cfg.RegistryAddress != "" {
		cfg.Mode = target.ModeRegistry
	}
	return cfg
}

func atoiOrZero(raw string) int {
	if raw == "" {
		return 0
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return 0
	}
	return n
}

func loadFacade(projectRoot string) *sourcecontract.Store {
	store, err := sourcecontract.Load(projectRoot)
	if err != nil {
		fmt.Fprintf(os.Stderr, "load source contract: %v\n", err)
		return nil
	}
	return store
}
