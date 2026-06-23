package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/hex1n/sofarpc-cli/internal/core/projectbootstrap"
	"github.com/hex1n/sofarpc-cli/internal/core/projectconfig"
)

func runProjectSetup(opts setupOptions) error {
	if err := rejectUserOnlyFlags(opts); err != nil {
		return err
	}
	if opts.local == opts.shared {
		return fmt.Errorf("project setup requires exactly one of --local or --shared")
	}
	projectRoot, err := resolveProjectSetupRoot(opts.projectRoot)
	if err != nil {
		return err
	}
	kind := projectconfig.KindShared
	if opts.local {
		kind = projectconfig.KindLocal
	}
	if name := strings.TrimSpace(opts.profile); name != "" {
		return runProjectProfileSetup(opts, projectRoot, kind, name)
	}
	if opts.setDefault {
		return fmt.Errorf("--set-default requires --profile; to set the default for an existing profile use: sofarpc-mcp profile use <name>")
	}
	cfg, err := buildProjectTargetConfig(opts)
	if err != nil {
		return err
	}
	result, err := projectbootstrap.Run(projectbootstrap.Input{
		ProjectRoot:            projectRoot,
		Kind:                   kind,
		Config:                 cfg,
		Force:                  opts.force,
		DryRun:                 opts.dryRun,
		RequireConfigFields:    true,
		RequireAllowedServices: true,
	})
	if err != nil {
		return projectSetupBootstrapError(err)
	}

	if opts.dryRun {
		fmt.Printf("[dry-run] project %s:\n%s", result.ConfigPath, result.ConfigBody)
		if result.Gitignore != nil {
			if result.Gitignore.WouldChange {
				fmt.Printf("[dry-run] project %s append:\n%s\n", result.Gitignore.Path, result.Gitignore.Entry)
			} else {
				fmt.Printf("[dry-run] project %s already contains %s\n", result.Gitignore.Path, result.Gitignore.Entry)
			}
		}
		return nil
	}
	if result.Gitignore != nil && result.Gitignore.Changed {
		fmt.Printf("project: ensured %s ignores %s\n", result.Gitignore.Path, result.Gitignore.Entry)
	}
	fmt.Printf("project: wrote %s\n", result.ConfigPath)
	return nil
}

func runProjectProfileSetup(opts setupOptions, projectRoot string, kind projectconfig.Kind, name string) error {
	profile, err := buildProjectProfileConfig(opts)
	if err != nil {
		return err
	}
	result, err := projectbootstrap.WriteProfile(projectbootstrap.ProfileInput{
		ProjectRoot: projectRoot,
		Kind:        kind,
		Name:        name,
		Profile:     profile,
		SetDefault:  opts.setDefault,
		Force:       opts.force,
		DryRun:      opts.dryRun,
	})
	if err != nil {
		return projectProfileSetupError(err)
	}
	if opts.dryRun {
		fmt.Printf("[dry-run] project %s profile %q:\n%s", result.ConfigPath, name, result.ConfigBody)
		if result.Gitignore != nil {
			if result.Gitignore.WouldChange {
				fmt.Printf("[dry-run] project %s append:\n%s\n", result.Gitignore.Path, result.Gitignore.Entry)
			} else {
				fmt.Printf("[dry-run] project %s already contains %s\n", result.Gitignore.Path, result.Gitignore.Entry)
			}
		}
		return nil
	}
	if result.Gitignore != nil && result.Gitignore.Changed {
		fmt.Printf("project: ensured %s ignores %s\n", result.Gitignore.Path, result.Gitignore.Entry)
	}
	verb := "wrote"
	if result.ProfileExisted {
		verb = "overwrote"
	}
	fmt.Printf("project: %s profile %q in %s\n", verb, name, result.ConfigPath)
	if result.SetDefault {
		fmt.Printf("project: set defaultProfile=%q\n", name)
	}
	return nil
}

func projectProfileSetupError(err error) error {
	switch {
	case errors.Is(err, projectbootstrap.ErrProfileNoFields):
		return fmt.Errorf("profile setup needs at least one target config flag (e.g. --direct-url)")
	case errors.Is(err, projectbootstrap.ErrProfileNameRequired):
		return fmt.Errorf("profile setup requires a non-empty --profile name")
	default:
		var existing projectbootstrap.ExistingProfileError
		if errors.As(err, &existing) {
			return fmt.Errorf("profile %q already exists in %s; pass --force to overwrite it", existing.Name, existing.Path)
		}
		return err
	}
}

func buildProjectProfileConfig(opts setupOptions) (projectconfig.ProfileConfig, error) {
	if opts.set["allowed-services"] {
		return projectconfig.ProfileConfig{}, fmt.Errorf("--allowed-services is not valid with --profile; the service allowlist stays a base, project-wide setting")
	}
	cfg, err := buildProjectTargetConfig(opts)
	if err != nil {
		return projectconfig.ProfileConfig{}, err
	}
	return projectconfig.ProfileConfig{
		DirectURL:        cfg.DirectURL,
		RegistryAddress:  cfg.RegistryAddress,
		RegistryProtocol: cfg.RegistryProtocol,
		Protocol:         cfg.Protocol,
		Serialization:    cfg.Serialization,
		UniqueID:         cfg.UniqueID,
		TimeoutMS:        cfg.TimeoutMS,
		ConnectTimeoutMS: cfg.ConnectTimeoutMS,
	}, nil
}

func projectSetupBootstrapError(err error) error {
	switch {
	case errors.Is(err, projectbootstrap.ErrNoConfigFields):
		return fmt.Errorf("project setup needs at least one target config flag")
	case errors.Is(err, projectbootstrap.ErrAllowedServicesMissing):
		return fmt.Errorf("project setup requires --allowed-services; use --allowed-services=* intentionally to allow every service")
	default:
		var existing projectbootstrap.ExistingConfigError
		if errors.As(err, &existing) {
			return fmt.Errorf("%s already exists; pass --force to overwrite", existing.Path)
		}
		return err
	}
}

func rejectUserOnlyFlags(opts setupOptions) error {
	for _, name := range []string{
		"claude-code",
		"codex",
		"command",
		"replace-env",
		"allow-invoke",
		"allowed-target-hosts",
		"allow-target-override",
		"args-file-root",
		"args-file-max-bytes",
		"session-plan-max-bytes",
		"max-response-bytes",
	} {
		if opts.set[name] {
			return fmt.Errorf("--%s is only valid with --scope=user", name)
		}
	}
	if opts.set["install-skill"] {
		return fmt.Errorf("--install-skill is only valid with --scope=user")
	}
	return nil
}

func resolveProjectSetupRoot(raw string) (string, error) {
	root := strings.TrimSpace(raw)
	if root == "" {
		wd, err := os.Getwd()
		if err != nil {
			return "", err
		}
		root = wd
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", fmt.Errorf("project root is not a directory: %s", abs)
	}
	return abs, nil
}

func buildProjectTargetConfig(opts setupOptions) (projectconfig.Config, error) {
	if opts.set["direct-url"] && strings.TrimSpace(opts.directURL) != "" &&
		opts.set["registry-address"] && strings.TrimSpace(opts.registryAddr) != "" {
		return projectconfig.Config{}, fmt.Errorf("--direct-url and --registry-address are mutually exclusive")
	}
	var cfg projectconfig.Config
	if v := projectStringFlag(opts, "direct-url", opts.directURL); v != "" {
		cfg.DirectURL = v
	}
	if v := projectStringFlag(opts, "registry-address", opts.registryAddr); v != "" {
		cfg.RegistryAddress = v
	}
	if v := projectStringFlag(opts, "registry-protocol", opts.registryProtocol); v != "" {
		cfg.RegistryProtocol = v
	}
	if v := projectStringFlag(opts, "protocol", opts.protocol); v != "" {
		cfg.Protocol = v
	}
	if v := projectStringFlag(opts, "serialization", opts.serialization); v != "" {
		cfg.Serialization = v
	}
	if v := projectStringFlag(opts, "unique-id", opts.uniqueID); v != "" {
		cfg.UniqueID = v
	}
	if opts.set["timeout-ms"] {
		v, err := positiveProjectInt("timeout-ms", opts.timeoutMS)
		if err != nil {
			return projectconfig.Config{}, err
		}
		cfg.TimeoutMS = v
	}
	if opts.set["connect-timeout-ms"] {
		v, err := positiveProjectInt("connect-timeout-ms", opts.connectTimeoutMS)
		if err != nil {
			return projectconfig.Config{}, err
		}
		cfg.ConnectTimeoutMS = v
	}
	if opts.set["allowed-services"] {
		cfg.AllowedServices = csvProjectFlag(opts.allowedServices)
	}
	return cfg, nil
}

func projectStringFlag(opts setupOptions, name, value string) string {
	if !opts.set[name] {
		return ""
	}
	return strings.TrimSpace(value)
}

func csvProjectFlag(value string) []string {
	var out []string
	for _, item := range strings.Split(value, ",") {
		item = strings.TrimSpace(item)
		if item != "" {
			out = append(out, item)
		}
	}
	return out
}

func positiveProjectInt(name, raw string) (int, error) {
	v := strings.TrimSpace(raw)
	if v == "" {
		return 0, fmt.Errorf("--%s requires a numeric value", name)
	}
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 {
		return 0, fmt.Errorf("--%s must be a positive integer, got %q", name, raw)
	}
	return n, nil
}
