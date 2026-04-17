package facadeconfig

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const EnvProjectRoot = "SOFARPC_PROJECT_ROOT"

var rootMarkers = []string{".sofarpc", "pom.xml", ".git"}

type Layout string

const (
	LayoutPrimary Layout = "primary"
)

type StatePaths struct {
	ProjectRoot string
	Layout      Layout
	StateDir    string
	ConfigPath  string
	IndexDir    string
	ReplayDir   string
}

func (l Layout) Label() string {
	switch l {
	case LayoutPrimary:
		return "primary (.sofarpc)"
	default:
		return string(l)
	}
}

func ResolveProjectRoot(start string, stderr io.Writer) (string, error) {
	env := strings.TrimSpace(os.Getenv(EnvProjectRoot))
	if env != "" {
		if root, err := ValidateProjectDir(env); err == nil {
			return root, nil
		} else if stderr != nil {
			fmt.Fprintf(stderr, "[facade] %s=%s does not exist; falling back\n", EnvProjectRoot, env)
		}
	}
	if strings.TrimSpace(start) == "" {
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
	if found := walkUpRoot(absStart); found != "" {
		return found, nil
	}
	return ValidateProjectDir(absStart)
}

func ValidateProjectDir(root string) (string, error) {
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

func StateDir(projectRoot string) string {
	return filepath.Join(projectRoot, ".sofarpc")
}

func ConfigPath(projectRoot string) string {
	return filepath.Join(StateDir(projectRoot), "config.json")
}

func EffectiveConfigPath(projectRoot string) (string, Layout) {
	return ConfigPath(projectRoot), LayoutPrimary
}

func EffectiveStateDir(projectRoot string) string {
	configPath, _ := EffectiveConfigPath(projectRoot)
	return filepath.Dir(configPath)
}

func EffectiveIndexDir(projectRoot string) string {
	return filepath.Join(EffectiveStateDir(projectRoot), "index")
}

func EffectiveReplayDir(projectRoot string) string {
	return filepath.Join(EffectiveStateDir(projectRoot), "replays")
}

func InspectState(projectRoot string) StatePaths {
	configPath, layout := EffectiveConfigPath(projectRoot)
	stateDir := filepath.Dir(configPath)
	return StatePaths{
		ProjectRoot: projectRoot,
		Layout:      layout,
		StateDir:    stateDir,
		ConfigPath:  configPath,
		IndexDir:    filepath.Join(stateDir, "index"),
		ReplayDir:   filepath.Join(stateDir, "replays"),
	}
}

func walkUpRoot(start string) string {
	cur := filepath.Clean(start)
	for {
		for _, marker := range rootMarkers {
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
