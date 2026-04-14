package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

const (
	callRPCSkillName = "call-rpc"
)

// runSkills dispatches `sofarpc skills <sub>`.
func (a *App) runSkills(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("skills subcommand required: install, init, where, list")
	}
	switch args[0] {
	case "install":
		return a.runSkillsInstall(args[1:])
	case "init":
		return a.runSkillsInit(args[1:])
	case "where":
		return a.runSkillsWhere(args[1:])
	case "list":
		return a.runSkillsList(args[1:])
	default:
		return fmt.Errorf("unknown skills subcommand %q", args[0])
	}
}

func (a *App) runSkillsInstall(args []string) error {
	flags := failFlagSet("skills install")
	var (
		force    bool
		dryRun   bool
		name     string
		skipShim bool
		shimDir  string
		target   string
	)
	flags.BoolVar(&force, "force", false, "remove destination dir first if present")
	flags.BoolVar(&dryRun, "dry-run", false, "print what would be copied, do nothing")
	flags.BoolVar(&skipShim, "skip-shim", false, "skip writing a user-level sofarpc shim")
	flags.StringVar(&shimDir, "shim-dir", "", "override shim destination (default: ~/.local/bin on POSIX, %USERPROFILE%\\bin on Windows)")
	flags.StringVar(&name, "name", callRPCSkillName, "skill to install (default: call-rpc)")
	flags.StringVar(&target, "target", "claude", "install target: claude | codex | both (codex drops the skill under ~/.agents/skills/<name>/)")
	if err := flags.Parse(args); err != nil {
		return err
	}
	name = strings.TrimSpace(name)
	if name == "" {
		name = callRPCSkillName
	}

	src, err := skillsSourceDir()
	if err != nil {
		return err
	}
	srcSkill := filepath.Join(src, name)
	if _, err := os.Stat(srcSkill); err != nil {
		return fmt.Errorf("source skill %q not found at %s: %w", name, srcSkill, err)
	}
	installRoot := filepath.Dir(src) // src is <root>/skills -> <root>

	targets, err := resolveInstallTargets(target, name)
	if err != nil {
		return err
	}

	fmt.Fprintf(a.Stdout, "source: %s\n", srcSkill)
	for _, t := range targets {
		fmt.Fprintf(a.Stdout, "target: %s (%s)\n", t.dest, t.label)
	}

	for _, t := range targets {
		if _, err := os.Stat(t.dest); err == nil {
			if !force {
				return fmt.Errorf("destination already exists — pass --force to replace: %s", t.dest)
			}
			if dryRun {
				fmt.Fprintf(a.Stdout, "[dry-run] would remove %s\n", t.dest)
			} else {
				if err := os.RemoveAll(t.dest); err != nil {
					return fmt.Errorf("remove existing dest %s: %w", t.dest, err)
				}
			}
		}
	}

	if dryRun {
		for _, t := range targets {
			fmt.Fprintf(a.Stdout, "[dry-run] would copy %s -> %s\n", srcSkill, t.dest)
		}
		return nil
	}

	for _, t := range targets {
		if err := os.MkdirAll(filepath.Dir(t.dest), 0o755); err != nil {
			return err
		}
		copied, err := copyTree(srcSkill, t.dest)
		if err != nil {
			return err
		}
		fmt.Fprintf(a.Stdout, "installed %d files -> %s\n", copied, t.dest)
	}
	if !skipShim {
		if err := a.writeUserShim(installRoot, shimDir); err != nil {
			fmt.Fprintf(a.Stdout, "warning: failed to write user shim: %v\n", err)
		}
	}
	fmt.Fprintf(a.Stdout, "next: sofarpc facade init   (run from your project root)\n")
	return nil
}

type installTarget struct {
	label string
	dest  string
}

func resolveInstallTargets(target, name string) ([]installTarget, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	claude := installTarget{label: "claude", dest: filepath.Join(home, ".claude", "skills", name)}
	codex := installTarget{label: "codex", dest: filepath.Join(home, ".agents", "skills", name)}
	switch strings.ToLower(strings.TrimSpace(target)) {
	case "", "claude":
		return []installTarget{claude}, nil
	case "codex":
		return []installTarget{codex}, nil
	case "both":
		return []installTarget{claude, codex}, nil
	default:
		return nil, fmt.Errorf("unknown --target %q (want claude|codex|both)", target)
	}
}

// writeUserShim installs a sofarpc entry-point in a user-scope bin dir.
//   - Windows: copies sofarpc.exe directly. Using a .cmd wrapper breaks
//     Python subprocess.run (CreateProcess can't launch .cmd without a
//     shell interpreter), so a real .exe is the only portable option.
//   - POSIX: writes a tiny sh wrapper with shebang — execve follows the
//     shebang, so subprocess.run and bare PATH lookups both work.
//
// shimDir overrides the default location. `SOFARPC_SHIM_DIR` env var is
// also honored.
func (a *App) writeUserShim(installRoot, shimDir string) error {
	if shimDir == "" {
		shimDir = strings.TrimSpace(os.Getenv("SOFARPC_SHIM_DIR"))
	}
	if shimDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return err
		}
		if runtime.GOOS == "windows" {
			shimDir = filepath.Join(home, "bin")
		} else {
			shimDir = filepath.Join(home, ".local", "bin")
		}
	}
	if err := os.MkdirAll(shimDir, 0o755); err != nil {
		return err
	}

	var shimPath string
	if runtime.GOOS == "windows" {
		target := filepath.Join(installRoot, "bin", "sofarpc.exe")
		if _, statErr := os.Stat(target); statErr != nil {
			return fmt.Errorf("sofarpc.exe not found at %s", target)
		}
		shimPath = filepath.Join(shimDir, "sofarpc.exe")
		if err := copyFile(target, shimPath); err != nil {
			return err
		}
	} else {
		target := filepath.Join(installRoot, "bin", "sofarpc")
		if _, statErr := os.Stat(target); statErr != nil {
			return fmt.Errorf("sofarpc binary not found at %s", target)
		}
		shimPath = filepath.Join(shimDir, "sofarpc")
		body := "#!/bin/sh\nexec \"" + target + "\" \"$@\"\n"
		if err := os.WriteFile(shimPath, []byte(body), 0o755); err != nil {
			return err
		}
	}
	fmt.Fprintf(a.Stdout, "wrote shim: %s\n", shimPath)
	if !isOnPATH(shimDir) {
		fmt.Fprintf(a.Stdout, "note: %s is not on your PATH.\n", shimDir)
		if runtime.GOOS == "windows" {
			fmt.Fprintf(a.Stdout, "  add via System Properties → Environment Variables, or run once:\n")
			fmt.Fprintf(a.Stdout, "    setx PATH \"%%PATH%%;%s\"\n", shimDir)
			fmt.Fprintf(a.Stdout, "  (open a new shell afterwards)\n")
		} else {
			fmt.Fprintf(a.Stdout, "  append this to your shell profile:\n")
			fmt.Fprintf(a.Stdout, "    export PATH=\"%s:$PATH\"\n", shimDir)
		}
	}
	return nil
}

func isOnPATH(dir string) bool {
	pathEnv := os.Getenv("PATH")
	if pathEnv == "" {
		return false
	}
	cleanTarget := filepath.Clean(dir)
	for _, entry := range filepath.SplitList(pathEnv) {
		if entry == "" {
			continue
		}
		clean := filepath.Clean(entry)
		if runtime.GOOS == "windows" {
			if strings.EqualFold(clean, cleanTarget) {
				return true
			}
		} else if clean == cleanTarget {
			return true
		}
	}
	return false
}

// runSkillsInit bootstraps the call-rpc skill for the current (or explicit)
// project in one shot:
// ensure the user-level skill is installed, run detect_config --write, preflight
// facade jars, then build the initial index. Everything is idempotent — re-running
// just refreshes config and index.
func (a *App) runSkillsInit(args []string) error {
	flags := failFlagSet("skills init")
	var (
		project    string
		name       string
		skipIndex  bool
		skipDetect bool
	)
	flags.StringVar(&project, "project", "", "project root (default: current working directory)")
	flags.StringVar(&name, "name", callRPCSkillName, "skill name (default: call-rpc)")
	flags.BoolVar(&skipIndex, "skip-index", false, "skip the initial build_index run")
	flags.BoolVar(&skipDetect, "skip-detect-config", false, "skip detect_config (use existing config.json as-is)")
	if err := flags.Parse(args); err != nil {
		return err
	}
	name = strings.TrimSpace(name)
	if name == "" {
		name = callRPCSkillName
	}

	if project == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return err
		}
		project = cwd
	}
	abs, err := filepath.Abs(project)
	if err != nil {
		return err
	}
	project = abs
	fmt.Fprintf(a.Stdout, "project: %s\n", project)

	dstSkill, foundLabel, err := locateInstalledSkill(name)
	if err != nil {
		fmt.Fprintf(a.Stdout, "skill not yet installed — installing (target=claude)...\n")
		if err := a.runSkillsInstall([]string{"--name", name}); err != nil {
			return fmt.Errorf("skills install failed: %w", err)
		}
		dstSkill, foundLabel, err = locateInstalledSkill(name)
		if err != nil {
			return fmt.Errorf("post-install: skill still not locatable: %w", err)
		}
	}
	fmt.Fprintf(a.Stdout, "skill present: %s (%s)\n", dstSkill, foundLabel)

	if skipDetect {
		fmt.Fprintln(a.Stdout, "\n[1/2] detect_config — skipped (--skip-detect-config)")
	} else {
		fmt.Fprintln(a.Stdout, "\n[1/2] detect_config --write")
		if err := a.runRPCTestDetectConfig([]string{"--project", project, "--write"}); err != nil {
			return fmt.Errorf("detect_config: %w", err)
		}
	}

	preflightFacadeArtifacts(a.Stdout, project)

	if skipIndex {
		fmt.Fprintln(a.Stdout, "\n[2/2] build_index — skipped (--skip-index)")
		fmt.Fprintln(a.Stdout, "done.")
		return nil
	}

	fmt.Fprintln(a.Stdout, "\n[2/2] build_index")
	if err := a.runRPCTestBuildIndex([]string{"--project", project}); err != nil {
		return fmt.Errorf("build_index: %w", err)
	}
	fmt.Fprintln(a.Stdout, "\ndone.")
	return nil
}

// preflightFacadeArtifacts reads the project's primary config.json and warns about
// facade modules whose jar or depsDir is missing.
// Non-fatal: build_index tolerates missing jars (it uses sourceRoot), but
// invoke-time will need them.
func preflightFacadeArtifacts(stdout io.Writer, project string) {
	candidates := []string{
		filepath.Join(project, ".sofarpc", "config.json"),
	}
	var raw []byte
	var err error
	for _, cand := range candidates {
		raw, err = os.ReadFile(cand)
		if err == nil {
			break
		}
	}
	if err != nil {
		return
	}
	var parsed struct {
		FacadeModules []struct {
			Name            string `json:"name"`
			JarGlob         string `json:"jarGlob"`
			DepsDir         string `json:"depsDir"`
			MavenModulePath string `json:"mavenModulePath"`
		} `json:"facadeModules"`
		MvnCommand string `json:"mvnCommand"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return
	}
	type missingEntry struct {
		name   string
		module string
	}
	var missing []missingEntry
	for _, m := range parsed.FacadeModules {
		if m.JarGlob != "" {
			matches, _ := filepath.Glob(filepath.Join(project, m.JarGlob))
			if len(matches) == 0 {
				missing = append(missing, missingEntry{m.Name, m.MavenModulePath})
				continue
			}
		}
		if m.DepsDir != "" {
			depsAbs := filepath.Join(project, m.DepsDir)
			entries, statErr := os.ReadDir(depsAbs)
			if statErr != nil || !containsJar(entries) {
				missing = append(missing, missingEntry{m.Name, m.MavenModulePath})
			}
		}
	}
	if len(missing) == 0 {
		return
	}
	mvn := parsed.MvnCommand
	if strings.TrimSpace(mvn) == "" {
		mvn = "mvn"
	}
	fmt.Fprintln(stdout, "\npreflight: facade artifacts missing — invokes will fail until you build:")
	for _, m := range missing {
		module := m.module
		if module == "" {
			module = m.name
		}
		fmt.Fprintf(stdout, "  # %s\n", m.name)
		fmt.Fprintf(stdout, "  %s -pl %s -am install -DskipTests\n", mvn, module)
		fmt.Fprintf(stdout, "  %s -pl %s dependency:copy-dependencies -DincludeScope=runtime -DoutputDirectory=target/facade-deps\n", mvn, module)
	}
}

func containsJar(entries []fs.DirEntry) bool {
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(strings.ToLower(e.Name()), ".jar") {
			return true
		}
	}
	return false
}

func (a *App) runSkillsWhere(args []string) error {
	_ = args
	src, srcErr := skillsSourceDir()
	fmt.Fprintf(a.Stdout, "source (bundled with CLI): %s\n", fmtPathOrErr(src, srcErr))
	home, homeErr := os.UserHomeDir()
	if homeErr != nil {
		fmt.Fprintf(a.Stdout, "user home unavailable: %v\n", homeErr)
		return nil
	}
	claudeRoot := filepath.Join(home, ".claude", "skills")
	codexRoot := filepath.Join(home, ".agents", "skills")
	fmt.Fprintf(a.Stdout, "install target (claude): %s%s\n", claudeRoot, markPresent(claudeRoot))
	fmt.Fprintf(a.Stdout, "install target (codex):  %s%s\n", codexRoot, markPresent(codexRoot))
	return nil
}

// locateInstalledSkill returns the directory + label of the installed skill.
func locateInstalledSkill(name string) (string, string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", "", err
	}
	names := bundledSkillNameCandidates(name)
	candidates := make([]struct{ label, path string }, 0, len(names)*2)
	for _, root := range []struct {
		label string
		base  string
	}{
		{"claude", filepath.Join(home, ".claude", "skills")},
		{"codex", filepath.Join(home, ".agents", "skills")},
	} {
		for _, skillName := range names {
			candidates = append(candidates, struct{ label, path string }{
				label: root.label,
				path:  filepath.Join(root.base, skillName),
			})
		}
	}
	for _, c := range candidates {
		if info, err := os.Stat(c.path); err == nil && info.IsDir() {
			return c.path, c.label, nil
		}
	}
	return "", "", fmt.Errorf("skill %q not installed under ~/.claude/skills or ~/.agents/skills", name)
}

func markPresent(dir string) string {
	if info, err := os.Stat(dir); err == nil && info.IsDir() {
		return "  (present)"
	}
	return ""
}

func (a *App) runSkillsList(args []string) error {
	_ = args
	src, err := skillsSourceDir()
	if err != nil {
		return err
	}
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if e.IsDir() && shouldListBundledSkillDir(e.Name()) {
			fmt.Fprintln(a.Stdout, e.Name())
		}
	}
	return nil
}

func canonicalBundledSkillName(name string) (string, bool) {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return callRPCSkillName, false
	}
	return trimmed, false
}

func bundledSkillNameCandidates(name string) []string {
	canonical, _ := canonicalBundledSkillName(name)
	return []string{canonical}
}

func shouldListBundledSkillDir(name string) bool {
	return !strings.HasPrefix(name, ".")
}

// skillsSourceDir returns the `<install_root>/skills` directory that ships
// with the CLI. Resolution order:
//  1. env SOFARPC_HOME/skills
//  2. <dir-of-executable>/../skills
//  3. walk up from cwd (source checkout)
//  4. error
func skillsSourceDir() (string, error) {
	if home := strings.TrimSpace(os.Getenv("SOFARPC_HOME")); home != "" {
		cand := filepath.Join(home, "skills")
		if info, err := os.Stat(cand); err == nil && info.IsDir() {
			return cand, nil
		}
	}
	exe, err := os.Executable()
	if err == nil {
		exe, _ = filepath.EvalSymlinks(exe)
		installRoot := filepath.Dir(filepath.Dir(exe)) // <root>/bin/sofarpc(.exe) -> <root>
		cand := filepath.Join(installRoot, "skills")
		if info, err := os.Stat(cand); err == nil && info.IsDir() {
			return cand, nil
		}
	}
	if cwd, err := os.Getwd(); err == nil {
		cur := filepath.Clean(cwd)
		for {
			cand := filepath.Join(cur, "skills")
			if info, err := os.Stat(cand); err == nil && info.IsDir() {
				return cand, nil
			}
			parent := filepath.Dir(cur)
			if parent == cur {
				break
			}
			cur = parent
		}
	}
	return "", fmt.Errorf("cannot locate bundled skills/ directory — set SOFARPC_HOME to the CLI install root")
}

func copyTree(src, dst string) (int, error) {
	count := 0
	err := filepath.WalkDir(src, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, relErr := filepath.Rel(src, path)
		if relErr != nil {
			return relErr
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		if err := copyFile(path, target); err != nil {
			return err
		}
		count++
		return nil
	})
	return count, err
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}

func fmtPathOrErr(p string, err error) string {
	if err != nil {
		return fmt.Sprintf("(unavailable: %v)", err)
	}
	return p
}
