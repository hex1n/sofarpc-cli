package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type projectTargetConfig struct {
	DirectURL        string `json:"directUrl,omitempty"`
	RegistryAddress  string `json:"registryAddress,omitempty"`
	RegistryProtocol string `json:"registryProtocol,omitempty"`
	Protocol         string `json:"protocol,omitempty"`
	Serialization    string `json:"serialization,omitempty"`
	UniqueID         string `json:"uniqueId,omitempty"`
	TimeoutMS        int    `json:"timeoutMs,omitempty"`
	ConnectTimeoutMS int    `json:"connectTimeoutMs,omitempty"`
}

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
	name := "config.json"
	if opts.local {
		name = "config.local.json"
	}
	path := filepath.Join(projectRoot, ".sofarpc", name)
	if _, err := os.Stat(path); err == nil && !opts.force {
		return fmt.Errorf("%s already exists; pass --force to overwrite", path)
	} else if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}

	body, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	body = append(body, '\n')

	if opts.dryRun {
		fmt.Printf("[dry-run] project %s:\n%s", path, body)
		if opts.local {
			return ensureLocalConfigIgnored(projectRoot, true)
		}
		return nil
	}
	if opts.local {
		if err := ensureLocalConfigIgnored(projectRoot, false); err != nil {
			return err
		}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if err := atomicWrite(path, body); err != nil {
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
		"allowed-services",
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

func buildProjectTargetConfig(opts setupOptions) (projectTargetConfig, error) {
	if opts.set["direct-url"] && strings.TrimSpace(opts.directURL) != "" &&
		opts.set["registry-address"] && strings.TrimSpace(opts.registryAddr) != "" {
		return projectTargetConfig{}, fmt.Errorf("--direct-url and --registry-address are mutually exclusive")
	}
	var cfg projectTargetConfig
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
			return projectTargetConfig{}, err
		}
		cfg.TimeoutMS = v
		fields++
	}
	if opts.set["connect-timeout-ms"] {
		v, err := positiveProjectInt("connect-timeout-ms", opts.connectTimeoutMS)
		if err != nil {
			return projectTargetConfig{}, err
		}
		cfg.ConnectTimeoutMS = v
		fields++
	}
	if fields == 0 {
		return projectTargetConfig{}, fmt.Errorf("project setup needs at least one target config flag")
	}
	return cfg, nil
}

func projectStringFlag(opts setupOptions, name, value string) string {
	if !opts.set[name] {
		return ""
	}
	return strings.TrimSpace(value)
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

const localConfigGitignoreEntry = ".sofarpc/config.local.json"

func ensureLocalConfigIgnored(projectRoot string, dryRun bool) error {
	path := filepath.Join(projectRoot, ".gitignore")
	existing, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	if containsGitignoreEntry(string(existing), localConfigGitignoreEntry) {
		if dryRun {
			fmt.Printf("[dry-run] project %s already contains %s\n", path, localConfigGitignoreEntry)
		}
		return nil
	}
	body := appendGitignoreEntry(string(existing), localConfigGitignoreEntry)
	if dryRun {
		fmt.Printf("[dry-run] project %s append:\n%s\n", path, localConfigGitignoreEntry)
		return nil
	}
	if err := atomicWrite(path, []byte(body)); err != nil {
		return err
	}
	fmt.Printf("project: ensured %s ignores %s\n", path, localConfigGitignoreEntry)
	return nil
}

func containsGitignoreEntry(body, entry string) bool {
	for _, line := range strings.Split(body, "\n") {
		if strings.TrimSpace(line) == entry {
			return true
		}
	}
	return false
}

func appendGitignoreEntry(body, entry string) string {
	body = strings.TrimRight(body, "\n")
	if body == "" {
		return entry + "\n"
	}
	return body + "\n" + entry + "\n"
}
