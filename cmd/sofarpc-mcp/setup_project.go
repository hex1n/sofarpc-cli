package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

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
	cfg, err := buildProjectTargetConfig(opts)
	if err != nil {
		return err
	}
	kind := projectconfig.KindShared
	if opts.local {
		kind = projectconfig.KindLocal
	}
	path := projectconfig.ConfigPath(projectRoot, kind)
	exists, err := projectconfig.Existing(path)
	if err != nil {
		return err
	}
	if exists && !opts.force {
		return fmt.Errorf("%s already exists; pass --force to overwrite", path)
	}

	body, err := projectconfig.Marshal(cfg)
	if err != nil {
		return err
	}

	if opts.dryRun {
		fmt.Printf("[dry-run] project %s:\n%s", path, body)
		if opts.local {
			status, err := projectconfig.LocalConfigIgnoreStatus(projectRoot)
			if err != nil {
				return err
			}
			if status.Changed {
				fmt.Printf("[dry-run] project %s append:\n%s\n", status.Path, status.Entry)
			} else {
				fmt.Printf("[dry-run] project %s already contains %s\n", status.Path, status.Entry)
			}
		}
		return nil
	}
	if opts.local {
		status, err := projectconfig.EnsureLocalConfigIgnored(projectRoot)
		if err != nil {
			return err
		}
		if status.Changed {
			fmt.Printf("project: ensured %s ignores %s\n", status.Path, status.Entry)
		}
	}
	if _, err := projectconfig.Write(projectRoot, kind, cfg, opts.force); err != nil {
		return err
	}
	fmt.Printf("project: wrote %s\n", path)
	return nil
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
	fields := 0
	if v := projectStringFlag(opts, "direct-url", opts.directURL); v != "" {
		cfg.DirectURL = v
		fields++
	}
	if v := projectStringFlag(opts, "registry-address", opts.registryAddr); v != "" {
		cfg.RegistryAddress = v
		fields++
	}
	if v := projectStringFlag(opts, "registry-protocol", opts.registryProtocol); v != "" {
		cfg.RegistryProtocol = v
		fields++
	}
	if v := projectStringFlag(opts, "protocol", opts.protocol); v != "" {
		cfg.Protocol = v
		fields++
	}
	if v := projectStringFlag(opts, "serialization", opts.serialization); v != "" {
		cfg.Serialization = v
		fields++
	}
	if v := projectStringFlag(opts, "unique-id", opts.uniqueID); v != "" {
		cfg.UniqueID = v
		fields++
	}
	if opts.set["timeout-ms"] {
		v, err := positiveProjectInt("timeout-ms", opts.timeoutMS)
		if err != nil {
			return projectconfig.Config{}, err
		}
		cfg.TimeoutMS = v
		fields++
	}
	if opts.set["connect-timeout-ms"] {
		v, err := positiveProjectInt("connect-timeout-ms", opts.connectTimeoutMS)
		if err != nil {
			return projectconfig.Config{}, err
		}
		cfg.ConnectTimeoutMS = v
		fields++
	}
	if opts.set["allowed-services"] {
		cfg.AllowedServices = csvProjectFlag(opts.allowedServices)
		if len(cfg.AllowedServices) > 0 {
			fields++
		}
	}
	if fields == 0 {
		return projectconfig.Config{}, fmt.Errorf("project setup needs at least one target config flag")
	}
	if len(cfg.AllowedServices) == 0 {
		return projectconfig.Config{}, fmt.Errorf("project setup requires --allowed-services; use --allowed-services=* intentionally to allow every service")
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
