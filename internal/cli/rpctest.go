package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

var rpcTestRootMarkers = []string{".sofarpc", ".claude", ".agents", "pom.xml", ".git"}

// runRPCTest dispatches `sofarpc rpc-test <sub>`. It fronts the bundled
// call-rpc Python tools so agents (and humans) can invoke them without
// hunting down a SKILL path or worrying about PYTHONPATH.
//
// Subcommands:
//
//	init            alias for `sofarpc skills init`
//	detect-config   writes <project>/.sofarpc/config.json
//	build-index     refreshes facade index/
//	run-cases       replays saved cases under cases/
//	where           prints resolved tools dir + project state paths
func (a *App) runRPCTest(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("rpc-test subcommand required: init, detect-config, build-index, run-cases, where")
	}
	switch args[0] {
	case "init":
		return a.runSkillsInit(args[1:])
	case "detect-config":
		return a.runRPCTestScript("detect_config.py", args[1:])
	case "build-index":
		return a.runRPCTestScript("build_index.py", args[1:])
	case "run-cases":
		return a.runRPCTestScript("run_cases.py", args[1:])
	case "where":
		return a.runRPCTestWhere(args[1:])
	default:
		return fmt.Errorf("unknown rpc-test subcommand %q", args[0])
	}
}

// runRPCTestScript resolves the bundled Python script and runs it in the
// selected project root. `--project` is handled by the Go wrapper; remaining
// args are passed through verbatim, so flags like --filter / --save / --context
// flow to the underlying tool.
func (a *App) runRPCTestScript(script string, args []string) error {
	project, passthrough, err := splitRPCTestProjectArg(args)
	if err != nil {
		return err
	}
	toolsDir, err := rpcTestToolsDir()
	if err != nil {
		return err
	}
	scriptPath := filepath.Join(toolsDir, script)
	if _, err := os.Stat(scriptPath); err != nil {
		return fmt.Errorf("%s not found at %s", script, scriptPath)
	}
	python, err := findPython()
	if err != nil {
		return err
	}
	projectRoot, err := a.resolveRPCTestProjectRoot(project)
	if err != nil {
		return err
	}
	return a.runPython(python, scriptPath, passthrough, projectRoot)
}

func (a *App) runRPCTestWhere(args []string) error {
	project, passthrough, err := splitRPCTestProjectArg(args)
	if err != nil {
		return err
	}
	if len(passthrough) > 0 {
		return fmt.Errorf("unknown rpc-test where args: %s", strings.Join(passthrough, " "))
	}
	toolsDir, err := rpcTestToolsDir()
	projectRoot, errProject := a.resolveRPCTestProjectRoot(project)
	if errProject != nil {
		return errProject
	}
	state := inspectRPCTestState(projectRoot)
	cfg, cfgErr := loadRPCTestConfigSummary(state.ConfigPath)

	fmt.Fprintf(a.Stdout, "tools dir:      %s\n", fmtPathOrErr(toolsDir, err))
	if python, err := findPython(); err == nil {
		fmt.Fprintf(a.Stdout, "python:         %s\n", python)
	} else {
		fmt.Fprintf(a.Stdout, "python:         (not found) %v\n", err)
	}
	fmt.Fprintf(a.Stdout, "project root:   %s\n", fmtPathOrErr(projectRoot, errProject))
	fmt.Fprintf(a.Stdout, "state layout:   %s\n", state.LayoutLabel)
	fmt.Fprintf(a.Stdout, "state dir:      %s\n", state.StateDir)
	fmt.Fprintf(a.Stdout, "config path:    %s\n", state.ConfigPathStatus)
	fmt.Fprintf(a.Stdout, "index dir:      %s\n", state.IndexDirStatus)
	fmt.Fprintf(a.Stdout, "cases dir:      %s\n", state.CasesDirStatus)
	if cfgErr == nil {
		fmt.Fprintf(a.Stdout, "sofarpcBin:     %s\n", emptyFallback(cfg.SofaRPCBin, "(not set)"))
		fmt.Fprintf(a.Stdout, "defaultContext: %s\n", emptyFallback(cfg.DefaultContext, "(not set)"))
		fmt.Fprintf(a.Stdout, "manifestPath:   %s\n", emptyFallback(cfg.ManifestPath, "(not set)"))
	} else if !os.IsNotExist(cfgErr) {
		fmt.Fprintf(a.Stdout, "config parse:   (unavailable: %v)\n", cfgErr)
	}
	return nil
}

type rpcTestStateInfo struct {
	LayoutLabel      string
	StateDir         string
	ConfigPath       string
	ConfigPathStatus string
	IndexDirStatus   string
	CasesDirStatus   string
}

type rpcTestConfigSummary struct {
	SofaRPCBin     string `json:"sofarpcBin"`
	DefaultContext string `json:"defaultContext"`
	ManifestPath   string `json:"manifestPath"`
}

func splitRPCTestProjectArg(args []string) (string, []string, error) {
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

func (a *App) resolveRPCTestProjectRoot(project string) (string, error) {
	root := strings.TrimSpace(project)
	if root != "" {
		return validateRPCTestProjectDir(root)
	}
	start := strings.TrimSpace(a.Cwd)
	if start == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return "", err
		}
		start = cwd
	}
	absStart, err := filepath.Abs(start)
	if err != nil {
		return "", err
	}
	if found := walkUpRPCTestRoot(absStart); found != "" {
		return found, nil
	}
	return validateRPCTestProjectDir(absStart)
}

func validateRPCTestProjectDir(root string) (string, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", fmt.Errorf("%s is not a directory", abs)
	}
	return abs, nil
}

func walkUpRPCTestRoot(start string) string {
	cur := filepath.Clean(start)
	for {
		for _, marker := range rpcTestRootMarkers {
			if _, err := os.Stat(filepath.Join(cur, marker)); err == nil {
				return cur
			}
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return ""
		}
		cur = parent
	}
}

func inspectRPCTestState(projectRoot string) rpcTestStateInfo {
	type candidate struct {
		name      string
		label     string
		stateDir  string
		configRel string
	}
	candidates := []candidate{
		{
			name:      "primary",
			label:     "primary (.sofarpc)",
			stateDir:  filepath.Join(projectRoot, ".sofarpc"),
			configRel: filepath.Join(projectRoot, ".sofarpc", "config.json"),
		},
		{
			name:      "claude",
			label:     "legacy claude (.claude/rpc-test)",
			stateDir:  filepath.Join(projectRoot, ".claude", "rpc-test"),
			configRel: filepath.Join(projectRoot, ".claude", "rpc-test", "config.json"),
		},
		{
			name:      "legacy",
			label:     "legacy skill (.claude/skills/rpc-test)",
			stateDir:  filepath.Join(projectRoot, ".claude", "skills", "rpc-test"),
			configRel: filepath.Join(projectRoot, ".claude", "skills", "rpc-test", "config.json"),
		},
	}

	selected := candidates[0]
	for _, cand := range candidates {
		if _, err := os.Stat(cand.configRel); err == nil {
			selected = cand
			break
		}
	}
	return rpcTestStateInfo{
		LayoutLabel:      selected.label,
		StateDir:         selected.stateDir,
		ConfigPath:       selected.configRel,
		ConfigPathStatus: formatPathStatus(selected.configRel),
		IndexDirStatus:   formatPathStatus(filepath.Join(selected.stateDir, "index")),
		CasesDirStatus:   formatPathStatus(filepath.Join(selected.stateDir, "cases")),
	}
}

func loadRPCTestConfigSummary(path string) (rpcTestConfigSummary, error) {
	var summary rpcTestConfigSummary
	body, err := os.ReadFile(path)
	if err != nil {
		return summary, err
	}
	if err := json.Unmarshal(body, &summary); err != nil {
		return summary, err
	}
	return summary, nil
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

// rpcTestToolsDir returns the tools directory to use. Resolution order:
//  1. ~/.claude/skills/call-rpc/tools/          (current Claude install)
//  2. ~/.claude/skills/call-facade/tools/       (deprecated Claude alias)
//  3. ~/.claude/skills/invoke-facade/tools/     (deprecated Claude alias)
//  4. ~/.claude/skills/rpc-test/tools/          (legacy Claude alias)
//  5. ~/.agents/skills/call-rpc/tools/          (current Codex install)
//  6. ~/.agents/skills/call-facade/tools/       (deprecated Codex alias)
//  7. ~/.agents/skills/invoke-facade/tools/     (deprecated Codex alias)
//  8. ~/.agents/skills/rpc-test/tools/          (legacy Codex alias)
//  9. <cli-install-root>/skills/call-rpc/tools/ (bundled source fallback)
func rpcTestToolsDir() (string, error) {
	home, err := os.UserHomeDir()
	if err == nil {
		names := bundledSkillNameCandidates(rpcTestSkillAlias)
		candidates := make([]string, 0, len(names)*2)
		for _, base := range []string{
			filepath.Join(home, ".claude", "skills"),
			filepath.Join(home, ".agents", "skills"),
		} {
			for _, name := range names {
				candidates = append(candidates, filepath.Join(base, name, "tools"))
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
		return "", fmt.Errorf("cannot locate call-rpc tools: %w", err)
	}
	cand := filepath.Join(src, callRPCSkillName, "tools")
	if info, err := os.Stat(cand); err == nil && info.IsDir() {
		return cand, nil
	}
	return "", fmt.Errorf("call-rpc tools not found under %s (run `sofarpc skills install`)", src)
}
