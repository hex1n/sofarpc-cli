package config

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/hex1n/sofarpc-cli/internal/model"
)

const appName = "sofarpc-cli"

type Paths struct {
	ConfigDir           string
	CacheDir            string
	ContextsFile        string
	RuntimeSourcesFile  string
	ContextTemplateFile string
}

func ResolvePaths() (Paths, error) {
	configRoot, err := os.UserConfigDir()
	if err != nil {
		return Paths{}, err
	}
	cacheRoot, err := os.UserCacheDir()
	if err != nil {
		return Paths{}, err
	}
	configDir := filepath.Join(configRoot, appName)
	return Paths{
		ConfigDir:           configDir,
		CacheDir:            filepath.Join(cacheRoot, appName),
		ContextsFile:        filepath.Join(configDir, "contexts.json"),
		RuntimeSourcesFile:  filepath.Join(configDir, "runtime-sources.json"),
		ContextTemplateFile: filepath.Join(configDir, "contexts.template.json"),
	}, nil
}

func (p Paths) Ensure() error {
	if err := os.MkdirAll(p.ConfigDir, 0o755); err != nil {
		return err
	}
	return os.MkdirAll(p.CacheDir, 0o755)
}

func LoadContextStore(paths Paths) (model.ContextStore, error) {
	store := model.ContextStore{Contexts: map[string]model.Context{}}
	body, err := os.ReadFile(paths.ContextsFile)
	if errors.Is(err, fs.ErrNotExist) {
		return store, nil
	}
	if err != nil {
		return store, err
	}
	if len(body) == 0 {
		return store, nil
	}
	if err := json.Unmarshal(body, &store); err != nil {
		return store, err
	}
	if store.Contexts == nil {
		store.Contexts = map[string]model.Context{}
	}
	return store, nil
}

func SaveContextStore(paths Paths, store model.ContextStore) error {
	if store.Contexts == nil {
		store.Contexts = map[string]model.Context{}
	}
	return writeJSON(paths.ContextsFile, store)
}

func defaultContextTemplate() map[string]any {
	return map[string]any{
		"_comment": `Copy this file to contexts.json and fill project-specific values.
projectRoot can be set for per-project auto matching. Leave it empty for global fallback contexts.
After editing, run: cp <path>/contexts.template.json <path>/contexts.json`,
		"active": "",
		"contexts": map[string]any{
			"local-dev": map[string]any{
				"_comment":         "Example for local direct mode",
				"name":             "local-dev",
				"projectRoot":      "/absolute/path/to/project-a",
				"mode":             "direct",
				"directUrl":        "bolt://127.0.0.1:12200",
				"protocol":         "bolt",
				"serialization":    "hessian2",
				"timeoutMs":        3000,
				"connectTimeoutMs": 1000,
			},
			"remote-registry": map[string]any{
				"_comment":         "Example for registry mode",
				"name":             "remote-registry",
				"mode":             "registry",
				"registryAddress":  "zookeeper://127.0.0.1:2181",
				"registryProtocol": "zookeeper",
				"protocol":         "bolt",
				"serialization":    "hessian2",
				"timeoutMs":        3000,
				"connectTimeoutMs": 1000,
			},
		},
	}
}

func EnsureContextTemplate(paths Paths) error {
	if _, err := os.Stat(paths.ContextTemplateFile); err == nil {
		return nil
	} else if !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	body := defaultContextTemplate()
	return writeJSON(paths.ContextTemplateFile, body)
}

func LoadRuntimeSourceStore(paths Paths) (model.RuntimeSourceStore, error) {
	store := model.RuntimeSourceStore{Sources: map[string]model.RuntimeSource{}}
	body, err := os.ReadFile(paths.RuntimeSourcesFile)
	if errors.Is(err, fs.ErrNotExist) {
		return store, nil
	}
	if err != nil {
		return store, err
	}
	if len(body) == 0 {
		return store, nil
	}
	if err := json.Unmarshal(body, &store); err != nil {
		return store, err
	}
	if store.Sources == nil {
		store.Sources = map[string]model.RuntimeSource{}
	}
	return store, nil
}

func SaveRuntimeSourceStore(paths Paths, store model.RuntimeSourceStore) error {
	if store.Sources == nil {
		store.Sources = map[string]model.RuntimeSource{}
	}
	return writeJSON(paths.RuntimeSourcesFile, store)
}

func LoadManifest(path string) (model.Manifest, bool, error) {
	var manifest model.Manifest
	if path == "" {
		return manifest, false, nil
	}
	body, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return manifest, false, nil
	}
	if err != nil {
		return manifest, false, err
	}
	if len(body) == 0 {
		return manifest, true, nil
	}
	if err := json.Unmarshal(body, &manifest); err != nil {
		return manifest, true, err
	}
	return manifest, true, nil
}

func SaveManifest(path string, manifest model.Manifest) error {
	return writeJSON(path, manifest)
}

func writeJSON(path string, value any) error {
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
