// sofarpc-mcp serves the sofarpc-cli tools over MCP stdio. The single
// entrypoint is intentional: agents load one server and call the sofarpc tools. See
// docs/architecture.md for the design.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/hex1n/sofarpc-cli/internal/core/contract"
	"github.com/hex1n/sofarpc-cli/internal/core/target"
	mcpserver "github.com/hex1n/sofarpc-cli/internal/mcp"
	"github.com/hex1n/sofarpc-cli/internal/sourcecontract"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

func main() {
	// "setup" handles user-level MCP registration and project-level
	// .sofarpc target config. "version" / --version are side-effect-free
	// inspection paths. Everything else falls through to the MCP stdio
	// server — that is the binary's job.
	if len(os.Args) >= 2 {
		switch os.Args[1] {
		case "setup":
			if err := runSetup(os.Args[2:]); err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
			return
		case "version", "--version", "-v":
			if err := runVersion(os.Stdout, os.Args[2:]); err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
			return
		}
	}
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
		TargetSources: target.ProjectSources(projectRoot, envConfig()),
		ServerVersion: buildVersion(),
		// Guard against the typed-nil-in-interface pitfall: when
		// loadContractStore returns a nil *sourcecontract.Store, wrap it
		// as an untyped nil contract.Store so holder readers see "no
		// contract attached" rather than a non-nil interface that panics
		// on first use.
		ContractLoader: func() (contract.Store, error) {
			store, err := loadContractStore(projectRoot)
			if store == nil {
				return nil, err
			}
			return store, err
		},
		ProjectContractLoader: func(projectRoot string) (contract.Store, error) {
			store, err := loadContractStore(projectRoot)
			if store == nil {
				return nil, err
			}
			return store, err
		},
	})
	return server.Run(ctx, &sdkmcp.StdioTransport{})
}

// projectRootFromEnv picks the directory the server should anchor to.
// Project-specific SOFARPC_* env such as SOFARPC_PROJECT_ROOT are ignored:
// project roots now come from per-call project/cwd/session inputs or the
// process CWD, and target config lives under .sofarpc/config*.json.
func projectRootFromEnv() string {
	wd, err := os.Getwd()
	if err != nil {
		return ""
	}
	return wd
}

// envConfig intentionally does not read project-specific target env.
// User-level setup may still seed global guardrail env in the MCP host, but
// target/root/service policy belongs in project .sofarpc/config*.json.
func envConfig() target.Config {
	return target.Config{}
}

// loadContractStore attempts to materialize a source-contract store. The
// returned error is also passed to the MCP server so sofarpc_open and
// sofarpc_doctor can surface it to agents — stderr on its own is
// unreliable because many MCP hosts swallow the subprocess's stderr.
func loadContractStore(projectRoot string) (*sourcecontract.Store, error) {
	store, err := sourcecontract.Load(projectRoot)
	if err != nil {
		fmt.Fprintf(os.Stderr, "load source contract: %v\n", err)
		return nil, err
	}
	return store, nil
}
