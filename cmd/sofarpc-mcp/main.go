// sofarpc-mcp serves the sofarpc-cli tools over MCP stdio. The single
// entrypoint is intentional: agents load one server, call six tools. See
// docs/architecture.md for the design.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"github.com/hex1n/sofarpc-cli/internal/core/contract"
	"github.com/hex1n/sofarpc-cli/internal/core/target"
	"github.com/hex1n/sofarpc-cli/internal/indexer"
	mcpserver "github.com/hex1n/sofarpc-cli/internal/mcp"
	"github.com/hex1n/sofarpc-cli/internal/worker"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

// shutdownGrace bounds how long Close waits for pooled workers to stop
// on SIGINT/SIGTERM. Longer than worker.Spec.StopGrace (2s) so normal
// shutdowns finish, short enough that a stuck JVM doesn't wedge the
// orchestrator waiting for our exit.
const shutdownGrace = 5 * time.Second

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
	workerClient := loadWorker()
	server := mcpserver.New(mcpserver.Options{
		TargetSources: target.Sources{Env: envConfig(), ProjectRoot: projectRoot},
		Facade:        loadFacade(projectRoot),
		Worker:        workerClient,
		Reindexer:     loadReindexer(projectRoot),
	})
	defer func() {
		// Use a fresh, bounded context — ctx is already cancelled by the
		// time we reach this defer, and Close on a cancelled context
		// returns instantly without stopping any JVMs.
		closeCtx, cancel := context.WithTimeout(context.Background(), shutdownGrace)
		defer cancel()
		_ = workerClient.Close(closeCtx)
	}()
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

// loadFacade tries to load the on-disk index at startup. A missing
// index is expected (indexer hasn't run yet) and degrades gracefully
// to nil; describe will surface errcode.FacadeNotConfigured.
func loadFacade(projectRoot string) contract.Store {
	if projectRoot == "" {
		return nil
	}
	idx, err := indexer.Load(projectRoot)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		fmt.Fprintf(os.Stderr, "warning: could not load facade index: %v\n", err)
		return nil
	}
	return idx
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

// loadReindexer wires sofarpc_describe refresh=true to the Spoon
// indexer subprocess. Requires SOFARPC_INDEXER_JAR plus a project root
// with at least one source path (SOFARPC_INDEXER_SOURCES, colon-separated,
// defaults to <projectRoot>/src/main/java). A missing jar returns nil
// — the MCP layer maps nil+refresh=true to errcode.FacadeNotConfigured.
func loadReindexer(projectRoot string) mcpserver.Reindexer {
	jar := os.Getenv("SOFARPC_INDEXER_JAR")
	if jar == "" || projectRoot == "" {
		return nil
	}
	sources := indexerSourcesFromEnv(projectRoot)
	if len(sources) == 0 {
		fmt.Fprintln(os.Stderr, "warning: SOFARPC_INDEXER_JAR set but no source roots resolved; reindexer disabled")
		return nil
	}
	spec := indexer.Spec{
		Java:        indexerJavaFromEnv(),
		Jar:         jar,
		ProjectRoot: projectRoot,
		Sources:     sources,
		Timeout:     5 * time.Minute,
	}
	return mcpserver.ReindexerFunc(func(ctx context.Context) (contract.Store, error) {
		if _, err := indexer.Run(ctx, spec); err != nil {
			return nil, err
		}
		return indexer.Load(projectRoot)
	})
}

// indexerJavaFromEnv resolves which JDK the indexer subprocess should
// run on. The indexer and worker are decoupled by design: recent Spoon
// releases need JDK 11+ to run, but the worker must match the target
// service's JDK (often 8 for SOFA deployments). SOFARPC_INDEXER_JAVA
// wins; SOFARPC_JAVA is the fallback so existing single-JDK setups keep
// working unchanged.
func indexerJavaFromEnv() string {
	if v := os.Getenv("SOFARPC_INDEXER_JAVA"); v != "" {
		return v
	}
	return os.Getenv("SOFARPC_JAVA")
}

// indexerSourcesFromEnv resolves the source roots to pass to the indexer.
// Explicit SOFARPC_INDEXER_SOURCES wins (OS path-list separator, same
// shape as $PATH — ':' on unix, ';' on windows); otherwise we fall
// back to <projectRoot>/src/main/java iff it exists — the Maven /
// Gradle convention that covers >95% of SOFA users.
func indexerSourcesFromEnv(projectRoot string) []string {
	if raw := os.Getenv("SOFARPC_INDEXER_SOURCES"); raw != "" {
		var out []string
		for _, s := range filepath.SplitList(raw) {
			if s != "" {
				out = append(out, s)
			}
		}
		return out
	}
	fallback := filepath.Join(projectRoot, "src", "main", "java")
	if info, err := os.Stat(fallback); err == nil && info.IsDir() {
		return []string{fallback}
	}
	return nil
}

// loadWorker assembles a worker.Client from env. Missing jar/digest/java
// means the worker stays disabled; the MCP layer handles nil Client by
// returning DaemonUnavailable. We warn on stderr rather than failing so
// dryRun / describe / target flows still work.
func loadWorker() *worker.Client {
	jar := os.Getenv("SOFARPC_RUNTIME_JAR")
	if jar == "" {
		return nil
	}
	digest := os.Getenv("SOFARPC_RUNTIME_JAR_DIGEST")
	if digest == "" {
		fmt.Fprintln(os.Stderr, "warning: SOFARPC_RUNTIME_JAR is set but SOFARPC_RUNTIME_JAR_DIGEST is not; worker disabled")
		return nil
	}
	version := os.Getenv("SOFARPC_VERSION")
	if version == "" {
		version = "unknown"
	}
	javaMajor := atoiOrZero(os.Getenv("SOFARPC_JAVA_MAJOR"))
	if javaMajor == 0 {
		javaMajor = 17
	}
	profile := worker.Profile{
		SOFARPCVersion:   version,
		RuntimeJarDigest: digest,
		JavaMajor:        javaMajor,
	}
	if profile.Empty() {
		fmt.Fprintln(os.Stderr, "warning: worker profile incomplete; worker disabled")
		return nil
	}
	spec := worker.Spec{
		Java:         os.Getenv("SOFARPC_JAVA"),
		Jar:          jar,
		ReadyTimeout: 30 * time.Second,
		StopGrace:    2 * time.Second,
	}
	return &worker.Client{Pool: worker.NewPool(spec), Profile: profile}
}
