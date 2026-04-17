package facadekit

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"

	"github.com/hex1n/sofarpc-cli/internal/projectscan"
)

type Config struct {
	FacadeModules     []FacadeModule `json:"facadeModules"`
	MvnCommand        string         `json:"mvnCommand"`
	SofaRPCBin        string         `json:"sofarpcBin"`
	InterfaceSuffixes []string       `json:"interfaceSuffixes"`
	RequiredMarkers   []string       `json:"requiredMarkers"`
	DefaultContext    string         `json:"defaultContext"`
	ManifestPath      string         `json:"manifestPath"`
}

type FacadeModule = projectscan.FacadeModule

func DefaultConfig() Config {
	mvnCommand := "./mvnw"
	if runtime.GOOS == "windows" {
		mvnCommand = "./mvnw.cmd"
	}
	return Config{
		FacadeModules:     []FacadeModule{},
		MvnCommand:        mvnCommand,
		SofaRPCBin:        "sofarpc",
		InterfaceSuffixes: []string{"Facade", "Api"},
		RequiredMarkers:   []string{"必传", "必填", "required"},
		DefaultContext:    "",
		ManifestPath:      "sofarpc.manifest.json",
	}
}

func LoadConfig(projectRoot string, optional bool) (Config, error) {
	cfg := DefaultConfig()
	configPath, _ := EffectiveConfigPath(projectRoot)
	body, err := os.ReadFile(configPath)
	if errors.Is(err, fs.ErrNotExist) {
		if optional {
			return cfg, nil
		}
		return Config{}, fmt.Errorf(
			"[facade] no config found at %s.\n  Run `sofarpc facade discover --write` to generate one at %s.\n  Project root used: %s",
			ConfigPath(projectRoot),
			ConfigPath(projectRoot),
			projectRoot,
		)
	}
	if err != nil {
		return Config{}, err
	}
	if len(body) != 0 {
		if err := json.Unmarshal(body, &cfg); err != nil {
			return Config{}, err
		}
	}
	if cfg.FacadeModules == nil {
		cfg.FacadeModules = []FacadeModule{}
	}
	if !optional && len(cfg.FacadeModules) == 0 {
		return Config{}, fmt.Errorf("[facade] config has no facadeModules entry (at %s)", configPath)
	}
	return cfg, nil
}

func LoadConfigOrDiscover(projectRoot string) (Config, error) {
	cfg, err := LoadConfig(projectRoot, true)
	if err != nil {
		return Config{}, err
	}
	if len(cfg.FacadeModules) == 0 {
		cfg.FacadeModules = projectscan.DetectFacadeModules(projectRoot)
	}
	if len(cfg.FacadeModules) == 0 {
		return Config{}, fmt.Errorf("[facade] no facade modules found under %s", projectRoot)
	}
	return cfg, nil
}

func ResolveRepoPath(pathFromRepo, projectRoot string) string {
	if filepath.IsAbs(pathFromRepo) {
		return filepath.Clean(pathFromRepo)
	}
	return filepath.Clean(filepath.Join(projectRoot, pathFromRepo))
}

func IterSourceRoots(cfg Config, projectRoot string) []string {
	roots := make([]string, 0, len(cfg.FacadeModules))
	for _, module := range cfg.FacadeModules {
		if module.SourceRoot == "" {
			continue
		}
		roots = append(roots, ResolveRepoPath(module.SourceRoot, projectRoot))
	}
	return roots
}

func SaveJSON(path string, value any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	body, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	body = append(body, '\n')
	return os.WriteFile(path, body, 0o644)
}
