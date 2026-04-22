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

// runSetup registers this sofarpc-mcp binary with Claude Code and Codex.
// The shape of a registration is minimal on purpose: `command` points
// at the binary, and the caller-supplied flags seed optional SOFARPC_*
// env defaults. Everything else (PROJECT_ROOT falling back to CWD,
// protocol/serialization defaults) is already resolved server-side, so
// a zero-argument setup still yields a working server.
//
// Registration is idempotent: running setup again with different flags
// replaces the sofarpc entry in place. Unrelated entries — other MCP
// servers, unrelated top-level keys in ~/.claude.json — are preserved.
func runSetup(args []string) error {
	flags := flag.NewFlagSet("setup", flag.ContinueOnError)
	var (
		claude       = flags.Bool("claude-code", false, "register only in Claude Code (~/.claude.json)")
		codex        = flags.Bool("codex", false, "register only in Codex (~/.codex/config.toml)")
		projectRoot  = flags.String("project-root", "", "optional default SOFARPC_PROJECT_ROOT")
		directURL    = flags.String("direct-url", "", "optional default SOFARPC_DIRECT_URL")
		registryAddr = flags.String("registry-address", "", "optional default SOFARPC_REGISTRY_ADDRESS")
		installSkill = flags.Bool("install-skill", true, "also install the sofarpc-invoke skill under ~/.claude/skills and ~/.codex/skills")
		dryRun       = flags.Bool("dry-run", false, "print the would-be changes without writing")
	)
	flags.Usage = func() {
		fmt.Fprintf(flags.Output(), "Usage: sofarpc-mcp setup [flags]\n\nRegisters this binary as an MCP server in Claude Code and Codex.\n\nFlags:\n")
		flags.PrintDefaults()
	}
	if err := flags.Parse(args); err != nil {
		return err
	}

	binary, err := resolveBinaryPath()
	if err != nil {
		return fmt.Errorf("resolve binary: %w", err)
	}

	envDefaults := buildSetupEnv(*projectRoot, *directURL, *registryAddr)
	bothClients := !*claude && !*codex

	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("user home: %w", err)
	}

	if *claude || bothClients {
		path := filepath.Join(home, ".claude.json")
		if err := setupClaudeAt(path, binary, envDefaults, *dryRun); err != nil {
			return fmt.Errorf("claude-code: %w", err)
		}
		if *installSkill {
			dir := filepath.Join(home, ".claude", "skills", "sofarpc-invoke")
			if err := installSkillAt(dir, *dryRun); err != nil {
				return fmt.Errorf("claude-code skill: %w", err)
			}
		}
	}
	if *codex || bothClients {
		path := filepath.Join(home, ".codex", "config.toml")
		if err := setupCodexAt(path, binary, envDefaults, *dryRun); err != nil {
			return fmt.Errorf("codex: %w", err)
		}
		if *installSkill {
			dir := filepath.Join(home, ".codex", "skills", "sofarpc-invoke")
			if err := installSkillAt(dir, *dryRun); err != nil {
				return fmt.Errorf("codex skill: %w", err)
			}
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

func buildSetupEnv(projectRoot, directURL, registry string) map[string]string {
	out := map[string]string{}
	if v := strings.TrimSpace(projectRoot); v != "" {
		out["SOFARPC_PROJECT_ROOT"] = v
	}
	if v := strings.TrimSpace(directURL); v != "" {
		out["SOFARPC_DIRECT_URL"] = v
	}
	if v := strings.TrimSpace(registry); v != "" {
		out["SOFARPC_REGISTRY_ADDRESS"] = v
	}
	return out
}

// --- Claude Code ---------------------------------------------------------

// setupClaudeAt upserts the sofarpc entry in a Claude Code config file.
// The file is round-tripped through map[string]any so every unrelated
// top-level key (conversations, settings, other mcpServers) is retained.
// Key order is not preserved — `encoding/json` does not guarantee it —
// but the data is intact, which is what matters for a machine-owned
// config file.
func setupClaudeAt(path, binary string, envDefaults map[string]string, dryRun bool) error {
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
	entry := map[string]any{"command": binary}
	if len(envDefaults) > 0 {
		entry["env"] = stringMapToAny(envDefaults)
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

// --- Codex ---------------------------------------------------------------

// codexHeaderRe matches a TOML section header on its own line. Inline
// tables and array-of-tables (`[[...]]`) deliberately do not match —
// Codex's config.toml uses plain tables for mcp_servers and we want to
// leave any other shape untouched.
var codexHeaderRe = regexp.MustCompile(`(?m)^\s*\[([^\[\]]+)\]\s*$`)

func setupCodexAt(path, binary string, envDefaults map[string]string, dryRun bool) error {
	existing, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}

	merged := upsertCodexTOML(string(existing), binary, envDefaults)

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
func upsertCodexTOML(existing, binary string, envDefaults map[string]string) string {
	kept := stripCodexSofaBlocks(existing)
	kept = strings.TrimRight(kept, "\n")
	block := renderCodexSofaBlock(binary, envDefaults)
	if kept == "" {
		return block
	}
	return kept + "\n\n" + block
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
