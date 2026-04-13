package cli

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/hex1n/sofarpc-cli/internal/config"
	"github.com/hex1n/sofarpc-cli/internal/model"
)

func (a *App) runRuntime(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("runtime subcommand required: list, show, install, source")
	}
	switch args[0] {
	case "list":
		return a.runRuntimeList()
	case "show":
		return a.runRuntimeShow(args[1:])
	case "install":
		return a.runRuntimeInstall(args[1:])
	case "source":
		return a.runRuntimeSource(args[1:])
	default:
		return fmt.Errorf("unknown runtime subcommand %q", args[0])
	}
}

func (a *App) runRuntimeList() error {
	runtimes, err := a.Runtime.ListRuntimes()
	if err != nil {
		return err
	}
	return printJSON(a.Stdout, map[string]any{
		"runtimeDir": a.Runtime.RuntimeDir(),
		"runtimes":   runtimes,
	})
}

func (a *App) runRuntimeShow(args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("runtime show requires exactly one version")
	}
	record, err := a.Runtime.GetRuntime(args[0])
	if err != nil {
		return err
	}
	return printJSON(a.Stdout, record)
}

func (a *App) runRuntimeInstall(args []string) error {
	flags := failFlagSet("runtime install")
	version := defaultSofaRPCVersion
	jarPath := ""
	sourceName := ""
	flags.StringVar(&version, "version", version, "SOFARPC runtime version")
	flags.StringVar(&sourceName, "source", "", "named local runtime source")
	flags.StringVar(&jarPath, "jar", "", "path to the worker runtime jar; when omitted, install from a local bundled candidate")
	if err := flags.Parse(args); err != nil {
		return err
	}
	record, err := a.Runtime.InstallRuntimeFrom(version, sourceName, jarPath)
	if err != nil {
		return err
	}
	return printJSON(a.Stdout, record)
}

func (a *App) runRuntimeSource(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("runtime source subcommand required: list, show, set, validate, use, delete")
	}
	switch args[0] {
	case "list":
		return a.runRuntimeSourceList(args[1:])
	case "show":
		return a.runRuntimeSourceShow(args[1:])
	case "set":
		return a.runRuntimeSourceSet(args[1:])
	case "validate":
		return a.runRuntimeSourceValidate(args[1:])
	case "use":
		return a.runRuntimeSourceUse(args[1:])
	case "delete":
		return a.runRuntimeSourceDelete(args[1:])
	default:
		return fmt.Errorf("unknown runtime source subcommand %q", args[0])
	}
}

func (a *App) runRuntimeSourceList(args []string) error {
	flags := failFlagSet("runtime source list")
	version := ""
	flags.StringVar(&version, "version", "", "optional SOFARPC runtime version to validate all sources against")
	if err := flags.Parse(args); err != nil {
		return err
	}
	store, err := config.LoadRuntimeSourceStore(a.Paths)
	if err != nil {
		return err
	}
	report := model.RuntimeSourceListReport{
		Active:  store.Active,
		Version: version,
		Sources: store.Sources,
	}
	if strings.TrimSpace(version) != "" {
		names := make([]string, 0, len(store.Sources))
		for name := range store.Sources {
			names = append(names, name)
		}
		sort.Strings(names)
		report.Validations = make([]model.RuntimeSourceValidation, 0, len(names))
		for _, name := range names {
			validation, err := a.Runtime.ValidateRuntimeSource(version, name)
			if err != nil {
				validation = model.RuntimeSourceValidation{
					Name:    name,
					Version: version,
					OK:      false,
					Error:   err.Error(),
				}
			}
			report.Validations = append(report.Validations, validation)
		}
	}
	return printJSON(a.Stdout, report)
}

func (a *App) runRuntimeSourceShow(args []string) error {
	store, err := config.LoadRuntimeSourceStore(a.Paths)
	if err != nil {
		return err
	}
	name := store.Active
	if len(args) == 1 {
		name = args[0]
	}
	if strings.TrimSpace(name) == "" {
		return fmt.Errorf("runtime source show requires a source name when no active source is set")
	}
	source, ok := store.Sources[name]
	if !ok {
		return fmt.Errorf("runtime source %q does not exist", name)
	}
	return printJSON(a.Stdout, source)
}

func (a *App) runRuntimeSourceSet(args []string) error {
	flags := failFlagSet("runtime source set")
	kind := ""
	pathValue := ""
	sha256URL := ""
	flags.StringVar(&kind, "kind", "", "source kind: file, directory, url-template, or manifest-url")
	flags.StringVar(&pathValue, "path", "", "source path, URL template, or manifest URL")
	flags.StringVar(&sha256URL, "sha256-url", "", "optional SHA-256 checksum URL template for url-template sources")
	if err := flags.Parse(args); err != nil {
		return err
	}
	positionals := flags.Args()
	if len(positionals) != 1 {
		return fmt.Errorf("runtime source set requires exactly one source name")
	}
	if kind != "file" && kind != "directory" && kind != "url-template" && kind != "manifest-url" {
		return fmt.Errorf("runtime source set requires --kind file, directory, url-template, or manifest-url")
	}
	if err := requireValue("--path", pathValue); err != nil {
		return err
	}
	if kind != "url-template" && strings.TrimSpace(sha256URL) != "" {
		return fmt.Errorf("--sha256-url is only supported for --kind url-template")
	}
	if kind == "file" || kind == "directory" {
		if !filepath.IsAbs(pathValue) {
			pathValue = filepath.Join(a.Cwd, pathValue)
		}
		pathValue = filepath.Clean(pathValue)
	}
	store, err := config.LoadRuntimeSourceStore(a.Paths)
	if err != nil {
		return err
	}
	name := positionals[0]
	store.Sources[name] = model.RuntimeSource{
		Name:      name,
		Kind:      kind,
		Path:      pathValue,
		SHA256URL: strings.TrimSpace(sha256URL),
	}
	if store.Active == "" {
		store.Active = name
	}
	if err := config.SaveRuntimeSourceStore(a.Paths, store); err != nil {
		return err
	}
	return printJSON(a.Stdout, store.Sources[name])
}

func (a *App) runRuntimeSourceValidate(args []string) error {
	flags := failFlagSet("runtime source validate")
	version := defaultSofaRPCVersion
	flags.StringVar(&version, "version", version, "SOFARPC runtime version")
	if err := flags.Parse(args); err != nil {
		return err
	}
	positionals := flags.Args()
	if len(positionals) > 1 {
		return fmt.Errorf("runtime source validate accepts at most one source name")
	}
	sourceName := ""
	if len(positionals) == 1 {
		sourceName = positionals[0]
	} else {
		store, err := config.LoadRuntimeSourceStore(a.Paths)
		if err != nil {
			return err
		}
		sourceName = store.Active
		if strings.TrimSpace(sourceName) == "" {
			return fmt.Errorf("runtime source validate requires a source name when no active source is set")
		}
	}
	validation, err := a.Runtime.ValidateRuntimeSource(version, sourceName)
	if err != nil {
		return err
	}
	if err := printJSON(a.Stdout, validation); err != nil {
		return err
	}
	if validation.OK {
		return nil
	}
	return &exitError{silent: true}
}

func (a *App) runRuntimeSourceUse(args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("runtime source use requires exactly one source name")
	}
	store, err := config.LoadRuntimeSourceStore(a.Paths)
	if err != nil {
		return err
	}
	if _, ok := store.Sources[args[0]]; !ok {
		return fmt.Errorf("runtime source %q does not exist", args[0])
	}
	store.Active = args[0]
	return config.SaveRuntimeSourceStore(a.Paths, store)
}

func (a *App) runRuntimeSourceDelete(args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("runtime source delete requires exactly one source name")
	}
	store, err := config.LoadRuntimeSourceStore(a.Paths)
	if err != nil {
		return err
	}
	delete(store.Sources, args[0])
	if store.Active == args[0] {
		store.Active = ""
	}
	return config.SaveRuntimeSourceStore(a.Paths, store)
}
