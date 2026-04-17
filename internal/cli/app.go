package cli

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/hex1n/sofarpc-cli/internal/config"
	"github.com/hex1n/sofarpc-cli/internal/metadata"
	"github.com/hex1n/sofarpc-cli/internal/model"
	"github.com/hex1n/sofarpc-cli/internal/runtime"
)

const defaultSofaRPCVersion = "5.7.6"

type App struct {
	Stdin    io.Reader
	Stdout   io.Writer
	Stderr   io.Writer
	Cwd      string
	Paths    config.Paths
	Runtime  *runtime.Manager
	Metadata *metadata.Manager
}

type exitError struct {
	message string
	silent  bool
}

func (e *exitError) Error() string {
	return e.message
}

func (e *exitError) Silent() bool {
	return e.silent
}

func New(stdin io.Reader, stdout, stderr io.Writer, cwd string) (*App, error) {
	paths, err := config.ResolvePaths()
	if err != nil {
		return nil, err
	}
	if err := paths.Ensure(); err != nil {
		return nil, err
	}
	if err := config.EnsureContextTemplate(paths); err != nil {
		return nil, err
	}
	return &App{
		Stdin:    stdin,
		Stdout:   stdout,
		Stderr:   stderr,
		Cwd:      cwd,
		Paths:    paths,
		Runtime:  runtime.NewManager(paths, cwd),
		Metadata: metadata.NewManager(paths, cwd),
	}, nil
}

func (a *App) Run(args []string) error {
	if len(args) == 0 {
		a.printUsage()
		return nil
	}
	switch args[0] {
	case "call":
		return a.runCall(args[1:])
	case "describe":
		return a.runDescribe(args[1:])
	case "doctor":
		return a.runDoctor(args[1:])
	case "target":
		return a.runTarget(args[1:])
	case "daemon":
		return a.runDaemon(args[1:])
	case "runtime":
		return a.runRuntime(args[1:])
	case "metadata":
		return a.runMetadata(args[1:])
	case "context":
		return a.runContext(args[1:])
	case "manifest":
		return a.runManifest(args[1:])
	case "skills":
		return a.runSkills(args[1:])
	case "facade":
		return a.runFacade(args[1:])
	case "help":
		a.printUsage()
		return nil
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func (a *App) printUsage() {
	fmt.Fprintln(a.Stdout, strings.TrimSpace(`
sofarpc — SOFARPC CLI

Commands:
  call      invoke a SOFARPC service through the Java runtime daemon
  describe  print a service method schema from project source or local artifacts
  doctor    show resolved config, runtime, target reachability, and daemon state
  target    show the currently resolved direct/registry target
  daemon    inspect and manage local Java runtime daemons
  runtime   install and inspect locally cached Java worker runtimes
  context   manage reusable target contexts
  manifest  initialize or generate a project manifest
  skills    install or inspect bundled agent skills (call-rpc, ...)
	  facade   bootstrap + drive facade invocation helpers (init/discover/index/services/schema/replay/status)
`))
}

func printJSON(out io.Writer, value any) error {
	body, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(out, "%s\n", body)
	return err
}

func parseCSV(value string) []string {
	if value == "" {
		return nil
	}
	parts := strings.Split(value, ",")
	items := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			items = append(items, part)
		}
	}
	return items
}

func resolveManifestPath(cwd, explicit string) string {
	if explicit != "" {
		if filepath.IsAbs(explicit) {
			return explicit
		}
		return filepath.Join(cwd, explicit)
	}
	return filepath.Join(cwd, "sofarpc.manifest.json")
}

func failFlagSet(name string) *flag.FlagSet {
	set := flag.NewFlagSet(name, flag.ContinueOnError)
	set.SetOutput(io.Discard)
	return set
}

func requireValue(name, value string) error {
	if strings.TrimSpace(value) == "" {
		return fmt.Errorf("%s is required", name)
	}
	return nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func firstPositive(values ...int) int {
	for _, value := range values {
		if value > 0 {
			return value
		}
	}
	return 0
}

func parseServiceMethod(token string) (string, string, error) {
	if strings.Contains(token, "/") {
		return "", "", fmt.Errorf("positional form must be %q, got %q", "service.method", token)
	}
	idx := strings.LastIndex(token, ".")
	if idx <= 0 || idx == len(token)-1 {
		return "", "", fmt.Errorf("positional form must be %q, got %q", "service.method", token)
	}
	return token[:idx], token[idx+1:], nil
}

func defaultsTarget() model.TargetConfig {
	return model.TargetConfig{
		Protocol:         "bolt",
		Serialization:    "hessian2",
		TimeoutMS:        3000,
		ConnectTimeoutMS: 1000,
	}
}
