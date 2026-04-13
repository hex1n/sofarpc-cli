package runtime

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/hex1n/sofarpc-cli/internal/config"
	"github.com/hex1n/sofarpc-cli/internal/model"
)

func (m *Manager) resolveInstallSource(version, sourceName, explicitJar string) (installSource, error) {
	if strings.TrimSpace(explicitJar) != "" {
		jarPath, err := filepath.Abs(explicitJar)
		if err != nil {
			return installSource{}, err
		}
		if _, err := os.Stat(jarPath); err != nil {
			return installSource{}, err
		}
		return installSource{JarPath: jarPath, Source: "user-jar"}, nil
	}
	if strings.TrimSpace(sourceName) != "" {
		return m.resolveNamedRuntimeSource(version, sourceName)
	}
	if resolved, ok, err := m.resolveActiveRuntimeSource(version); err != nil {
		return installSource{}, err
	} else if ok {
		return resolved, nil
	}
	for _, candidate := range m.bundledRuntimeJarCandidates(version) {
		if _, err := os.Stat(candidate); err == nil {
			jarPath, err := filepath.Abs(candidate)
			if err != nil {
				return installSource{}, err
			}
			return installSource{JarPath: jarPath, Source: "workspace-bundled"}, nil
		}
	}
	return installSource{}, fmt.Errorf("runtime %q has no local bundled candidate; pass --jar or configure a runtime source", version)
}

func (m *Manager) resolveActiveRuntimeSource(version string) (installSource, bool, error) {
	store, err := config.LoadRuntimeSourceStore(m.Paths)
	if err != nil {
		return installSource{}, false, err
	}
	if strings.TrimSpace(store.Active) == "" {
		return installSource{}, false, nil
	}
	source, ok := store.Sources[store.Active]
	if !ok {
		return installSource{}, false, fmt.Errorf("active runtime source %q does not exist", store.Active)
	}
	resolved, err := m.resolveSourceRecord(version, store.Active, source, true)
	if err != nil {
		return installSource{}, false, err
	}
	return resolved, true, nil
}

func (m *Manager) resolveNamedRuntimeSource(version, sourceName string) (installSource, error) {
	store, err := config.LoadRuntimeSourceStore(m.Paths)
	if err != nil {
		return installSource{}, err
	}
	source, ok := store.Sources[sourceName]
	if !ok {
		return installSource{}, fmt.Errorf("runtime source %q does not exist", sourceName)
	}
	return m.resolveSourceRecord(version, sourceName, source, false)
}

func (m *Manager) resolveSourceRecord(version, sourceName string, source model.RuntimeSource, active bool) (installSource, error) {
	if source.Name == "" {
		source.Name = sourceName
	}
	switch source.Kind {
	case "file":
		jarPath, err := filepath.Abs(source.Path)
		if err != nil {
			return installSource{}, err
		}
		if _, err := os.Stat(jarPath); err != nil {
			return installSource{}, err
		}
		return installSource{JarPath: jarPath, Source: "source:" + source.Name}, nil
	case "directory":
		basePath, err := filepath.Abs(source.Path)
		if err != nil {
			return installSource{}, err
		}
		for _, candidate := range runtimeJarCandidatesForBase(basePath, version) {
			if _, err := os.Stat(candidate); err == nil {
				return installSource{JarPath: candidate, Source: "source:" + source.Name}, nil
			}
		}
		if active {
			return installSource{}, fmt.Errorf("active runtime source %q does not provide runtime %q", source.Name, version)
		}
		return installSource{}, fmt.Errorf("runtime source %q does not provide runtime %q", source.Name, version)
	default:
		return installSource{}, fmt.Errorf("runtime source %q uses unsupported kind %q", source.Name, source.Kind)
	}
}
