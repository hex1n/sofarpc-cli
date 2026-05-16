package main

import (
	_ "embed"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// embeddedSkill is the sofarpc-invoke agent-facing playbook, baked into
// the binary at compile time so a `go install` user who never clones
// the repo can still install the skill via `sofarpc-mcp setup`. The
// canonical source lives at cmd/sofarpc-mcp/skill/SKILL.md; the
// repo-level .claude/skills/ entry is a symlink to that same file.
//
//go:embed skill/SKILL.md
var embeddedSkill string

// runSetup owns installation-time configuration. User scope registers
// this binary with Claude Code and Codex; project scope writes target
// defaults into .sofarpc/config*.json. User registration is minimal on
// purpose: `command` points at the binary, and SOFARPC_* flags seed env
// defaults while the server keeps resolving everything else at runtime.
//
// User registration is idempotent: running setup again with different
// flags replaces only the sofarpc entry and merges existing sofarpc env
// keys unless --replace-env is requested.
func runSetup(args []string) error {
	flags := flag.NewFlagSet("setup", flag.ContinueOnError)
	var opts setupOptions
	flags.StringVar(&opts.scope, "scope", "user", "setup scope: user or project")
	flags.BoolVar(&opts.claude, "claude-code", false, "user scope: register only in Claude Code (~/.claude.json)")
	flags.BoolVar(&opts.codex, "codex", false, "user scope: register only in Codex (~/.codex/config.toml)")
	flags.StringVar(&opts.command, "command", "", "user scope: absolute sofarpc-mcp command path to register")
	flags.StringVar(&opts.projectRoot, "project-root", "", "user scope: optional SOFARPC_PROJECT_ROOT; project scope: target project root")
	flags.StringVar(&opts.directURL, "direct-url", "", "optional direct target URL")
	flags.StringVar(&opts.registryAddr, "registry-address", "", "optional registry target address")
	flags.StringVar(&opts.registryProtocol, "registry-protocol", "", "optional registry protocol")
	flags.StringVar(&opts.protocol, "protocol", "", "optional wire protocol")
	flags.StringVar(&opts.serialization, "serialization", "", "optional wire serialization")
	flags.StringVar(&opts.uniqueID, "unique-id", "", "optional SOFA service uniqueId")
	flags.StringVar(&opts.timeoutMS, "timeout-ms", "", "optional request timeout in milliseconds")
	flags.StringVar(&opts.connectTimeoutMS, "connect-timeout-ms", "", "optional connect timeout in milliseconds")
	flags.BoolVar(&opts.allowInvoke, "allow-invoke", false, "user scope: set SOFARPC_ALLOW_INVOKE")
	flags.StringVar(&opts.allowedServices, "allowed-services", "", "user scope: comma-separated SOFARPC_ALLOWED_SERVICES")
	flags.StringVar(&opts.allowedTargetHosts, "allowed-target-hosts", "", "user scope: comma-separated SOFARPC_ALLOWED_TARGET_HOSTS")
	flags.BoolVar(&opts.allowTargetOverride, "allow-target-override", false, "user scope: set SOFARPC_ALLOW_TARGET_OVERRIDE")
	flags.StringVar(&opts.argsFileRoot, "args-file-root", "", "user scope: SOFARPC_ARGS_FILE_ROOT")
	flags.StringVar(&opts.argsFileMaxBytes, "args-file-max-bytes", "", "user scope: SOFARPC_ARGS_FILE_MAX_BYTES")
	flags.StringVar(&opts.sessionPlanMaxBytes, "session-plan-max-bytes", "", "user scope: SOFARPC_SESSION_PLAN_MAX_BYTES")
	flags.StringVar(&opts.maxResponseBytes, "max-response-bytes", "", "user scope: SOFARPC_MAX_RESPONSE_BYTES")
	flags.BoolVar(&opts.installSkill, "install-skill", true, "user scope: also install the sofarpc-invoke skill under ~/.claude/skills and ~/.codex/skills")
	flags.BoolVar(&opts.replaceEnv, "replace-env", false, "user scope: replace the sofarpc env block instead of merging existing keys")
	flags.BoolVar(&opts.local, "local", false, "project scope: write .sofarpc/config.local.json")
	flags.BoolVar(&opts.shared, "shared", false, "project scope: write .sofarpc/config.json")
	flags.BoolVar(&opts.force, "force", false, "project scope: overwrite an existing project config file")
	flags.BoolVar(&opts.dryRun, "dry-run", false, "print the would-be changes without writing")
	flags.Usage = func() {
		fmt.Fprintf(flags.Output(), "Usage: sofarpc-mcp setup [flags]\n\nRegisters this binary at user scope or writes project-level target config.\n\nFlags:\n")
		flags.PrintDefaults()
	}
	if err := flags.Parse(args); err != nil {
		return err
	}
	opts.set = visitedFlags(flags)

	switch opts.scope {
	case "user":
		return runUserSetup(opts)
	case "project":
		return runProjectSetup(opts)
	default:
		return fmt.Errorf("invalid --scope %q: expected user or project", opts.scope)
	}
}

type setupOptions struct {
	scope               string
	claude              bool
	codex               bool
	command             string
	projectRoot         string
	directURL           string
	registryAddr        string
	registryProtocol    string
	protocol            string
	serialization       string
	uniqueID            string
	timeoutMS           string
	connectTimeoutMS    string
	allowInvoke         bool
	allowedServices     string
	allowedTargetHosts  string
	allowTargetOverride bool
	argsFileRoot        string
	argsFileMaxBytes    string
	sessionPlanMaxBytes string
	maxResponseBytes    string
	installSkill        bool
	replaceEnv          bool
	local               bool
	shared              bool
	force               bool
	dryRun              bool
	set                 map[string]bool
}

func visitedFlags(flags *flag.FlagSet) map[string]bool {
	out := map[string]bool{}
	flags.Visit(func(f *flag.Flag) {
		out[f.Name] = true
	})
	return out
}

func runUserSetup(opts setupOptions) error {
	if err := rejectProjectOnlyFlags(opts); err != nil {
		return err
	}
	binary, err := resolveSetupCommand(opts.command)
	if err != nil {
		return fmt.Errorf("resolve binary: %w", err)
	}

	envDefaults, err := buildSetupEnv(opts)
	if err != nil {
		return err
	}
	bothClients := !opts.claude && !opts.codex

	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("user home: %w", err)
	}

	if opts.claude || bothClients {
		path := filepath.Join(home, ".claude.json")
		if err := setupClaudeAt(path, binary, envDefaults, opts.replaceEnv, opts.dryRun); err != nil {
			return fmt.Errorf("claude-code: %w", err)
		}
		if opts.installSkill {
			dir := filepath.Join(home, ".claude", "skills", "sofarpc-invoke")
			if err := installSkillAt(dir, opts.dryRun); err != nil {
				return fmt.Errorf("claude-code skill: %w", err)
			}
		}
	}
	if opts.codex || bothClients {
		path := filepath.Join(home, ".codex", "config.toml")
		if err := setupCodexAt(path, binary, envDefaults, opts.replaceEnv, opts.dryRun); err != nil {
			return fmt.Errorf("codex: %w", err)
		}
		if opts.installSkill {
			dir := filepath.Join(home, ".codex", "skills", "sofarpc-invoke")
			if err := installSkillAt(dir, opts.dryRun); err != nil {
				return fmt.Errorf("codex skill: %w", err)
			}
		}
	}
	return nil
}

func rejectProjectOnlyFlags(opts setupOptions) error {
	for _, name := range []string{"local", "shared", "force"} {
		if opts.set[name] {
			return fmt.Errorf("--%s is only valid with --scope=project", name)
		}
	}
	return nil
}

// resolveBinaryPath returns the absolute path the client configs should
// point at. We prefer the symlink-resolved location so a later `go
// install` that refreshes the link still works; if symlink resolution
// fails (Windows, restricted FS) fall back to whatever Executable()
// reports.
func resolveBinaryPath() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		return resolved, nil
	}
	return exe, nil
}

func resolveSetupCommand(command string) (string, error) {
	if command = strings.TrimSpace(command); command != "" {
		if !filepath.IsAbs(command) {
			return "", fmt.Errorf("--command must be an absolute path, got %q", command)
		}
		info, err := os.Stat(command)
		if err != nil {
			return "", fmt.Errorf("--command %q: %w", command, err)
		}
		if info.IsDir() {
			return "", fmt.Errorf("--command must point to a file, got directory %q", command)
		}
		if resolved, err := filepath.EvalSymlinks(command); err == nil {
			return resolved, nil
		}
		return command, nil
	}
	binary, err := resolveBinaryPath()
	if err != nil {
		return "", err
	}
	if isLikelyGoRunBinary(binary) {
		return "", fmt.Errorf("refusing to register temporary go run binary %q; build/install sofarpc-mcp first or pass --command /abs/path/to/sofarpc-mcp", binary)
	}
	return binary, nil
}

func isLikelyGoRunBinary(path string) bool {
	clean := filepath.Clean(path)
	sep := string(os.PathSeparator)
	return strings.Contains(clean, sep+"go-build")
}

func buildSetupEnv(opts setupOptions) (map[string]string, error) {
	out := map[string]string{}
	if err := addProjectRootEnv(out, opts.projectRoot, opts.set); err != nil {
		return nil, err
	}
	addStringEnv(out, "direct-url", "SOFARPC_DIRECT_URL", opts.directURL, opts.set)
	addStringEnv(out, "registry-address", "SOFARPC_REGISTRY_ADDRESS", opts.registryAddr, opts.set)
	addStringEnv(out, "registry-protocol", "SOFARPC_REGISTRY_PROTOCOL", opts.registryProtocol, opts.set)
	addStringEnv(out, "protocol", "SOFARPC_PROTOCOL", opts.protocol, opts.set)
	addStringEnv(out, "serialization", "SOFARPC_SERIALIZATION", opts.serialization, opts.set)
	addStringEnv(out, "unique-id", "SOFARPC_UNIQUE_ID", opts.uniqueID, opts.set)
	if err := addNumericEnv(out, "timeout-ms", "SOFARPC_TIMEOUT_MS", opts.timeoutMS, opts.set); err != nil {
		return nil, err
	}
	if err := addNumericEnv(out, "connect-timeout-ms", "SOFARPC_CONNECT_TIMEOUT_MS", opts.connectTimeoutMS, opts.set); err != nil {
		return nil, err
	}
	if opts.set["allow-invoke"] {
		out["SOFARPC_ALLOW_INVOKE"] = strconv.FormatBool(opts.allowInvoke)
	}
	addStringEnv(out, "allowed-services", "SOFARPC_ALLOWED_SERVICES", opts.allowedServices, opts.set)
	addStringEnv(out, "allowed-target-hosts", "SOFARPC_ALLOWED_TARGET_HOSTS", opts.allowedTargetHosts, opts.set)
	if opts.set["allow-target-override"] {
		out["SOFARPC_ALLOW_TARGET_OVERRIDE"] = strconv.FormatBool(opts.allowTargetOverride)
	}
	addStringEnv(out, "args-file-root", "SOFARPC_ARGS_FILE_ROOT", opts.argsFileRoot, opts.set)
	if err := addNumericEnv(out, "args-file-max-bytes", "SOFARPC_ARGS_FILE_MAX_BYTES", opts.argsFileMaxBytes, opts.set); err != nil {
		return nil, err
	}
	if err := addNumericEnv(out, "session-plan-max-bytes", "SOFARPC_SESSION_PLAN_MAX_BYTES", opts.sessionPlanMaxBytes, opts.set); err != nil {
		return nil, err
	}
	if err := addNumericEnv(out, "max-response-bytes", "SOFARPC_MAX_RESPONSE_BYTES", opts.maxResponseBytes, opts.set); err != nil {
		return nil, err
	}
	return out, nil
}

func addProjectRootEnv(out map[string]string, raw string, set map[string]bool) error {
	if !set["project-root"] {
		return nil
	}
	root := strings.TrimSpace(raw)
	if root == "" {
		return nil
	}
	if !filepath.IsAbs(root) {
		abs, err := filepath.Abs(root)
		if err != nil {
			return fmt.Errorf("--project-root: %w", err)
		}
		root = abs
	}
	out["SOFARPC_PROJECT_ROOT"] = root
	return nil
}

func addStringEnv(out map[string]string, flagName, envName, raw string, set map[string]bool) {
	if !set[flagName] {
		return
	}
	if v := strings.TrimSpace(raw); v != "" {
		out[envName] = v
	}
}

func addNumericEnv(out map[string]string, flagName, envName, raw string, set map[string]bool) error {
	if !set[flagName] {
		return nil
	}
	v := strings.TrimSpace(raw)
	if v == "" {
		return fmt.Errorf("--%s requires a numeric value", flagName)
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 0 {
		return fmt.Errorf("--%s must be a non-negative integer, got %q", flagName, raw)
	}
	out[envName] = strconv.Itoa(n)
	return nil
}

// --- Claude Code ---------------------------------------------------------

// setupClaudeAt upserts the sofarpc entry in a Claude Code config file.
// The file is round-tripped through map[string]any so every unrelated
// top-level key (conversations, settings, other mcpServers) is retained.
// Key order is not preserved — `encoding/json` does not guarantee it —
// but the data is intact, which is what matters for a machine-owned
// config file.
func setupClaudeAt(path, binary string, envDefaults map[string]string, replaceEnv, dryRun bool) error {
	existing, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}

	var doc map[string]any
	if len(existing) > 0 {
		if err := json.Unmarshal(existing, &doc); err != nil {
			return fmt.Errorf("parse %s: %w", path, err)
		}
	}
	if doc == nil {
		doc = map[string]any{}
	}

	servers, _ := doc["mcpServers"].(map[string]any)
	if servers == nil {
		servers = map[string]any{}
	}
	env := map[string]string{}
	if !replaceEnv {
		env = readClaudeEnv(servers["sofarpc"])
	}
	for k, v := range envDefaults {
		env[k] = v
	}
	entry := map[string]any{"command": binary}
	if len(env) > 0 {
		entry["env"] = stringMapToAny(env)
	}
	servers["sofarpc"] = entry
	doc["mcpServers"] = servers

	body, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	body = append(body, '\n')

	if dryRun {
		fmt.Printf("[dry-run] claude-code %s:\n%s", path, body)
		return nil
	}
	if err := atomicWrite(path, body); err != nil {
		return err
	}
	fmt.Printf("claude-code: registered sofarpc → %s\n", binary)
	return nil
}

func readClaudeEnv(entry any) map[string]string {
	out := map[string]string{}
	server, ok := entry.(map[string]any)
	if !ok {
		return out
	}
	env, ok := server["env"].(map[string]any)
	if !ok {
		return out
	}
	for k, raw := range env {
		if v, ok := raw.(string); ok {
			out[k] = v
		}
	}
	return out
}

// --- Codex ---------------------------------------------------------------

// codexHeaderRe matches a TOML section header on its own line. Inline
// tables and array-of-tables (`[[...]]`) deliberately do not match —
// Codex's config.toml uses plain tables for mcp_servers and we want to
// leave any other shape untouched.
var codexHeaderRe = regexp.MustCompile(`(?m)^\s*\[([^\[\]]+)\]\s*$`)

func setupCodexAt(path, binary string, envDefaults map[string]string, replaceEnv, dryRun bool) error {
	existing, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}

	merged := upsertCodexTOML(string(existing), binary, envDefaults, replaceEnv)

	if dryRun {
		fmt.Printf("[dry-run] codex %s:\n%s", path, merged)
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if err := atomicWrite(path, []byte(merged)); err != nil {
		return err
	}
	fmt.Printf("codex: registered sofarpc → %s\n", binary)
	return nil
}

// upsertCodexTOML rewrites a Codex config.toml so the sofarpc server
// entry reflects the supplied binary / env. Any block under
// [mcp_servers.sofarpc] or its sub-tables (currently just .env) is
// removed; every other block — including unrelated servers — is kept
// verbatim. The rebuilt sofarpc block is appended at the end, which is
// semantically equivalent to replacement in TOML.
func upsertCodexTOML(existing, binary string, envDefaults map[string]string, replaceEnv bool) string {
	env := map[string]string{}
	if !replaceEnv {
		env = readCodexSofaEnv(existing)
	}
	for k, v := range envDefaults {
		env[k] = v
	}
	kept := stripCodexSofaBlocks(existing)
	kept = strings.TrimRight(kept, "\n")
	block := renderCodexSofaBlock(binary, env)
	if kept == "" {
		return block
	}
	return kept + "\n\n" + block
}

func readCodexSofaEnv(text string) map[string]string {
	out := map[string]string{}
	lines := strings.Split(text, "\n")
	inEnv := false
	for _, line := range lines {
		if match := codexHeaderRe.FindStringSubmatch(line); match != nil {
			name := strings.TrimSpace(match[1])
			inEnv = name == "mcp_servers.sofarpc.env"
			continue
		}
		if !inEnv {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		if key == "" || strings.HasPrefix(key, "#") {
			continue
		}
		raw := strings.TrimSpace(value)
		if strings.HasPrefix(raw, "#") || raw == "" {
			continue
		}
		if v, ok := parseSimpleTOMLString(raw); ok {
			out[key] = v
		}
	}
	return out
}

func parseSimpleTOMLString(raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	switch {
	case strings.HasPrefix(raw, "\""):
		end := strings.LastIndex(raw, "\"")
		if end <= 0 {
			return "", false
		}
		v, err := strconv.Unquote(raw[:end+1])
		return v, err == nil
	case strings.HasPrefix(raw, "'"):
		end := strings.LastIndex(raw, "'")
		if end <= 0 {
			return "", false
		}
		return raw[1:end], true
	default:
		return "", false
	}
}

func stripCodexSofaBlocks(text string) string {
	if text == "" {
		return ""
	}
	lines := strings.Split(text, "\n")
	out := make([]string, 0, len(lines))
	skip := false
	for _, line := range lines {
		if match := codexHeaderRe.FindStringSubmatch(line); match != nil {
			name := strings.TrimSpace(match[1])
			if name == "mcp_servers.sofarpc" || strings.HasPrefix(name, "mcp_servers.sofarpc.") {
				skip = true
				continue
			}
			skip = false
		}
		if skip {
			continue
		}
		out = append(out, line)
	}
	return strings.Join(out, "\n")
}

func renderCodexSofaBlock(binary string, envDefaults map[string]string) string {
	var b strings.Builder
	b.WriteString("[mcp_servers.sofarpc]\n")
	fmt.Fprintf(&b, "command = %q\n", binary)
	if len(envDefaults) > 0 {
		b.WriteString("\n[mcp_servers.sofarpc.env]\n")
		keys := make([]string, 0, len(envDefaults))
		for k := range envDefaults {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			fmt.Fprintf(&b, "%s = %q\n", k, envDefaults[k])
		}
	}
	return b.String()
}

// --- shared --------------------------------------------------------------

// atomicWrite writes body to path via a sibling temp file + rename.
// A crash mid-write leaves the original config intact; there is no
// window where readers see a half-written file.
func atomicWrite(path string, body []byte) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".sofarpc-mcp-setup-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	committed := false
	defer func() {
		if !committed {
			_ = os.Remove(tmpPath)
		}
	}()
	if _, err := tmp.Write(body); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return err
	}
	committed = true
	return nil
}

func stringMapToAny(m map[string]string) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

// installSkillAt writes the embedded SKILL.md into dir/SKILL.md via an
// atomic temp+rename. Re-running replaces the file, so fixes to the
// skill ship with a new binary + re-run of `setup`. The target
// directory is created on demand; if MkdirAll fails we surface the
// error rather than fall back, because a failure here usually means
// the home dir layout is unusual and the user should see it.
func installSkillAt(dir string, dryRun bool) error {
	path := filepath.Join(dir, "SKILL.md")
	body := []byte(embeddedSkill)

	if dryRun {
		fmt.Printf("[dry-run] skill %s (%d bytes)\n", path, len(body))
		return nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	if err := atomicWrite(path, body); err != nil {
		return err
	}
	fmt.Printf("skill: installed sofarpc-invoke → %s\n", path)
	return nil
}
