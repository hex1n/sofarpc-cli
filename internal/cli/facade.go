package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/hex1n/sofarpc-cli/internal/facadekit"
)

var loadFacadeServiceSummary = facadekit.LoadServiceSummary

// runFacade dispatches `sofarpc facade <sub>`.
// It drives facade support workflows from the Go CLI, while delegating only
// Java source semantics to the Spoon indexer.
//
// Subcommands:
//
//	init      alias for `sofarpc skills init`
//	discover  writes <project>/.sofarpc/config.json
//	index     refreshes facade index/
//	services  lists available facade services in the current project
//	schema    prints generated DTO schema for a facade method
//	replay    replays saved calls under replays/
//	status    prints resolved tools dir + project state paths
func (a *App) runFacade(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("facade subcommand required: init, discover, index, services, schema, replay, status")
	}
	switch normalizeFacadeSubcommand(args[0]) {
	case "init":
		return a.runSkillsInit(args[1:])
	case "discover":
		return a.runFacadeDiscover(args[1:])
	case "index":
		return a.runFacadeIndex(args[1:])
	case "services":
		return a.runFacadeServices(args[1:])
	case "schema":
		return a.runFacadeSchema(args[1:])
	case "replay":
		return a.runFacadeReplay(args[1:])
	case "status":
		return a.runFacadeStatus(args[1:])
	default:
		return fmt.Errorf("unknown facade subcommand %q", args[0])
	}
}

func normalizeFacadeSubcommand(command string) string {
	return strings.ToLower(strings.TrimSpace(command))
}

func (a *App) runFacadeStatus(args []string) error {
	project, passthrough, err := splitFacadeProjectArg(args)
	if err != nil {
		return err
	}
	if len(passthrough) > 0 {
		return fmt.Errorf("unknown facade status args: %s", strings.Join(passthrough, " "))
	}
	skillDir, err := facadeSkillDir()
	projectRoot, errProject := a.resolveFacadeProjectRoot(project)
	if errProject != nil {
		return errProject
	}
	state := facadekit.InspectState(projectRoot)
	cfg, cfgErr := facadekit.LoadConfig(projectRoot, true)

	fmt.Fprintf(a.Stdout, "skill dir:      %s\n", fmtPathOrErr(skillDir, err))
	fmt.Fprintf(a.Stdout, "project root:   %s\n", fmtPathOrErr(projectRoot, errProject))
	fmt.Fprintf(a.Stdout, "state layout:   %s\n", state.Layout.Label())
	fmt.Fprintf(a.Stdout, "state dir:      %s\n", state.StateDir)
	fmt.Fprintf(a.Stdout, "config path:    %s\n", formatPathStatus(state.ConfigPath))
	fmt.Fprintf(a.Stdout, "index dir:      %s\n", formatPathStatus(state.IndexDir))
	fmt.Fprintf(a.Stdout, "replay dir:     %s\n", formatPathStatus(state.ReplayDir))
	if cfgErr == nil {
		fmt.Fprintf(a.Stdout, "sofarpcBin:     %s\n", emptyFallback(cfg.SofaRPCBin, "(not set)"))
		fmt.Fprintf(a.Stdout, "defaultContext: %s\n", emptyFallback(cfg.DefaultContext, "(not set)"))
		fmt.Fprintf(a.Stdout, "manifestPath:   %s\n", emptyFallback(cfg.ManifestPath, "(not set)"))
	} else if !os.IsNotExist(cfgErr) && !strings.Contains(cfgErr.Error(), "no config found") {
		fmt.Fprintf(a.Stdout, "config parse:   (unavailable: %v)\n", cfgErr)
	}
	return nil
}

func splitFacadeProjectArg(args []string) (string, []string, error) {
	var project string
	rest := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			rest = append(rest, args[i:]...)
			break
		}
		if arg == "--project" {
			if i+1 >= len(args) {
				return "", nil, fmt.Errorf("--project requires a value")
			}
			if project != "" {
				return "", nil, fmt.Errorf("--project specified more than once")
			}
			project = args[i+1]
			i++
			continue
		}
		if strings.HasPrefix(arg, "--project=") {
			if project != "" {
				return "", nil, fmt.Errorf("--project specified more than once")
			}
			project = strings.TrimSpace(strings.TrimPrefix(arg, "--project="))
			if project == "" {
				return "", nil, fmt.Errorf("--project requires a value")
			}
			continue
		}
		rest = append(rest, arg)
	}
	return project, rest, nil
}

func (a *App) resolveFacadeProjectRoot(project string) (string, error) {
	root := strings.TrimSpace(project)
	if root != "" {
		return facadekit.ValidateProjectDir(root)
	}
	return facadekit.ResolveProjectRoot(a.Cwd, a.Stderr)
}

func formatPathStatus(path string) string {
	_, err := os.Stat(path)
	if err == nil {
		return path
	}
	if os.IsNotExist(err) {
		return path + " (missing)"
	}
	return fmt.Sprintf("%s (unavailable: %v)", path, err)
}

func emptyFallback(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func (a *App) runFacadeDiscover(args []string) error {
	flags := failFlagSet("facade discover")
	var (
		project string
		write   bool
	)
	flags.StringVar(&project, "project", "", "project root (default: current working directory)")
	flags.BoolVar(&write, "write", false, "write config.json instead of printing a dry-run")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if len(flags.Args()) > 0 {
		return fmt.Errorf("unknown facade discover args: %s", strings.Join(flags.Args(), " "))
	}
	projectRoot, err := a.resolveFacadeProjectRoot(project)
	if err != nil {
		return err
	}
	return facadekit.DetectConfig(projectRoot, write, a.Stdout, a.Stderr)
}

func (a *App) runFacadeSchema(args []string) error {
	flags := failFlagSet("facade schema")
	var (
		project string
		types   string
		asJSON  bool
	)
	flags.StringVar(&project, "project", "", "project root (default: current working directory)")
	flags.StringVar(&types, "types", "", "comma-separated param types to disambiguate overloads")
	flags.BoolVar(&asJSON, "json", false, "print JSON output")
	if err := flags.Parse(args); err != nil {
		return err
	}
	rest := flags.Args()
	if len(rest) != 1 {
		return fmt.Errorf("usage: sofarpc facade schema [--project <path>] [--types <csv>] [--json] <service.method>")
	}
	service, method, err := parseServiceMethod(rest[0])
	if err != nil {
		return err
	}
	projectRoot, err := a.resolveFacadeProjectRoot(project)
	if err != nil {
		return err
	}
	cfg, err := facadekit.LoadConfig(projectRoot, false)
	if err != nil {
		return err
	}
	sourceRoots := facadekit.IterSourceRoots(cfg, projectRoot)
	if len(sourceRoots) == 0 {
		return fmt.Errorf("config has no facade source roots")
	}
	registry, err := facadekit.LoadSemanticRegistry(projectRoot, sourceRoots, cfg.RequiredMarkers)
	if err != nil {
		return err
	}
	schema, err := facadekit.BuildMethodSchema(registry, service, method, parseCSV(types), cfg.RequiredMarkers)
	if err != nil {
		return err
	}
	_ = asJSON
	if asJSON {
		return printJSON(a.Stdout, schema)
	}
	return printFacadeSchema(a.Stdout, schema)
}

func (a *App) runFacadeServices(args []string) error {
	flags := failFlagSet("facade services")
	var (
		project string
		filter  string
		asJSON  bool
	)
	flags.StringVar(&project, "project", "", "project root (default: current working directory)")
	flags.StringVar(&filter, "filter", "", "substring match against service or method")
	flags.BoolVar(&asJSON, "json", false, "print JSON output")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if len(flags.Args()) > 0 {
		return fmt.Errorf("unknown facade services args: %s", strings.Join(flags.Args(), " "))
	}
	projectRoot, err := a.resolveFacadeProjectRoot(project)
	if err != nil {
		return err
	}
	summary, err := loadFacadeServiceSummary(projectRoot)
	if err != nil {
		return err
	}
	summary = filterFacadeServiceSummary(summary, filter)
	if asJSON {
		return printJSON(a.Stdout, summary)
	}
	return printFacadeServices(a.Stdout, projectRoot, summary, filter)
}

func printFacadeSchema(out io.Writer, schema facadekit.MethodSchemaEnvelope) error {
	method := schema.Method
	if _, err := fmt.Fprintf(out, "service: %s\n", schema.Service); err != nil {
		return err
	}
	if strings.TrimSpace(schema.File) != "" {
		if _, err := fmt.Fprintf(out, "file:    %s\n", schema.File); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintf(out, "method:  %s\n", method.Name); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(out, "return:  %s\n", method.ReturnType); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(out, "params:  %d\n", len(method.ParamsFieldInfo)); err != nil {
		return err
	}
	if method.ResponseWarning != "" {
		if _, err := fmt.Fprintf(out, "warning: %s\n", method.ResponseWarning); err != nil {
			return err
		}
		if strings.TrimSpace(method.ResponseWarningReason) != "" {
			if _, err := fmt.Fprintf(out, "reason:  %s\n", method.ResponseWarningReason); err != nil {
				return err
			}
		}
	}

	for _, param := range method.ParamsFieldInfo {
		label := "optional"
		if strings.TrimSpace(param.RequiredHint) != "" {
			label = "required: " + param.RequiredHint
		}
		if _, err := fmt.Fprintf(out, "- %s: %s (%s)\n", param.Name, param.Type, label); err != nil {
			return err
		}
		if len(param.Fields) == 0 {
			continue
		}
		for _, field := range param.Fields {
			comment := strings.TrimSpace(field.Comment)
			if comment == "" {
				comment = ""
			} else {
				comment = " # " + comment
			}
			if _, err := fmt.Fprintf(
				out,
				"  - %s: %s%s\n",
				field.Name,
				field.Type,
				comment,
			); err != nil {
				return err
			}
		}
	}
	return nil
}

func filterFacadeServiceSummary(summary facadekit.IndexSummary, filter string) facadekit.IndexSummary {
	needle := strings.TrimSpace(strings.ToLower(filter))
	if needle == "" {
		return summary
	}
	filtered := make([]facadekit.IndexSummaryService, 0, len(summary.Services))
	for _, service := range summary.Services {
		if strings.Contains(strings.ToLower(service.Service), needle) {
			filtered = append(filtered, service)
			continue
		}
		for _, method := range service.Methods {
			if strings.Contains(strings.ToLower(method), needle) {
				filtered = append(filtered, service)
				break
			}
		}
	}
	summary.Services = filtered
	return summary
}

func printFacadeServices(out io.Writer, projectRoot string, summary facadekit.IndexSummary, filter string) error {
	if _, err := fmt.Fprintf(out, "project root: %s\n", projectRoot); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(out, "services:     %d\n", len(summary.Services)); err != nil {
		return err
	}
	if strings.TrimSpace(filter) != "" {
		if _, err := fmt.Fprintf(out, "filter:       %s\n", filter); err != nil {
			return err
		}
	}
	if len(summary.Services) == 0 {
		_, err := fmt.Fprintln(out, "\n(no facade services matched)")
		return err
	}
	for _, service := range summary.Services {
		if _, err := fmt.Fprintf(out, "\n- %s (%d methods)\n", service.Service, len(service.Methods)); err != nil {
			return err
		}
		if strings.TrimSpace(service.File) != "" {
			if _, err := fmt.Fprintf(out, "  file:    %s\n", service.File); err != nil {
				return err
			}
		}
		if _, err := fmt.Fprintf(out, "  methods: %s\n", strings.Join(service.Methods, ", ")); err != nil {
			return err
		}
	}
	return nil
}

func (a *App) runFacadeIndex(args []string) error {
	project, passthrough, err := splitFacadeProjectArg(args)
	if err != nil {
		return err
	}
	if len(passthrough) > 0 {
		return fmt.Errorf("unknown facade index args: %s", strings.Join(passthrough, " "))
	}
	projectRoot, err := a.resolveFacadeProjectRoot(project)
	if err != nil {
		return err
	}
	cfg, err := facadekit.LoadConfig(projectRoot, false)
	if err != nil {
		return err
	}
	return facadekit.RefreshIndex(projectRoot, cfg, a.Stdout, a.Stderr)
}

func (a *App) runFacadeReplay(args []string) error {
	flags := failFlagSet("facade replay")
	var (
		project      string
		filter       string
		onlyNamesCSV string
		contextName  string
		sofarpcBin   string
		dryRun       bool
		save         bool
	)
	flags.StringVar(&project, "project", "", "project root (default: current working directory)")
	flags.StringVar(&filter, "filter", "", "substring match against service or method")
	flags.StringVar(&onlyNamesCSV, "only-names", "", "comma-separated case names to include")
	flags.StringVar(&contextName, "context", "", "override sofarpc context for every case")
	flags.StringVar(&sofarpcBin, "sofarpc", "", "override sofarpc binary (else from config.json)")
	flags.BoolVar(&dryRun, "dry-run", false, "print commands, do not execute")
	flags.BoolVar(&save, "save", false, "save per-call results under replays/_runs/")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if len(flags.Args()) > 0 {
		return fmt.Errorf("unknown facade replay args: %s", strings.Join(flags.Args(), " "))
	}
	projectRoot, err := a.resolveFacadeProjectRoot(project)
	if err != nil {
		return err
	}
	return facadekit.ReplayCalls(projectRoot, facadekit.ReplayOptions{
		Filter:          filter,
		OnlyNames:       parseCSV(onlyNamesCSV),
		ContextOverride: contextName,
		DryRun:          dryRun,
		Save:            save,
		SofaRPCBin:      sofarpcBin,
	}, a.Stdout, a.Stderr)
}

// facadeSkillDir returns the installed call-rpc skill directory. Resolution order:
//  1. ~/.claude/skills/call-rpc/     (current Claude install)
//  2. ~/.agents/skills/call-rpc/     (current Codex install)
//  3. <cli-install-root>/skills/call-rpc/ (bundled source fallback)
func facadeSkillDir() (string, error) {
	home, err := os.UserHomeDir()
	if err == nil {
		names := bundledSkillNameCandidates(callRPCSkillName)
		candidates := make([]string, 0, len(names)*2)
		for _, base := range []string{
			filepath.Join(home, ".claude", "skills"),
			filepath.Join(home, ".agents", "skills"),
		} {
			for _, name := range names {
				candidates = append(candidates, filepath.Join(base, name))
			}
		}
		for _, cand := range candidates {
			if info, err := os.Stat(cand); err == nil && info.IsDir() {
				return cand, nil
			}
		}
	}
	src, err := skillsSourceDir()
	if err != nil {
		return "", fmt.Errorf("cannot locate call-rpc skill: %w", err)
	}
	cand := filepath.Join(src, callRPCSkillName)
	if info, err := os.Stat(cand); err == nil && info.IsDir() {
		return cand, nil
	}
	return "", fmt.Errorf("call-rpc skill not found under %s (run `sofarpc skills install`)", src)
}
