package runtime

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/hex1n/sofarpc-cli/internal/model"
)

type installSource struct {
	JarPath string
	Source  string
}

func (m *Manager) ListRuntimes() ([]model.RuntimeRecord, error) {
	entries, err := os.ReadDir(m.RuntimeDir())
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	records := make([]model.RuntimeRecord, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		record, ok, err := m.loadRuntimeRecord(entry.Name())
		if err != nil {
			return nil, err
		}
		if ok {
			records = append(records, record)
		}
	}
	sort.Slice(records, func(i, j int) bool {
		return records[i].Version < records[j].Version
	})
	return records, nil
}

func (m *Manager) GetRuntime(version string) (model.RuntimeRecord, error) {
	record, ok, err := m.loadRuntimeRecord(version)
	if err != nil {
		return model.RuntimeRecord{}, err
	}
	if !ok {
		return model.RuntimeRecord{}, fmt.Errorf("runtime %q is not installed", version)
	}
	return record, nil
}

func (m *Manager) InstallRuntime(version, sourceJar string) (model.RuntimeRecord, error) {
	return m.InstallRuntimeFrom(version, "", sourceJar)
}

func (m *Manager) EnsureRuntimeAvailable(version string) (string, error) {
	if strings.TrimSpace(version) == "" {
		version = defaultRuntimeVersion
	}
	installed := m.installedRuntimeJar(version)
	if _, err := os.Stat(installed); err == nil {
		return installed, nil
	}
	record, err := m.InstallRuntimeFrom(version, "", "")
	if err != nil {
		return "", fmt.Errorf("runtime %q is not available: %w", version, err)
	}
	return record.Path, nil
}

func (m *Manager) InstallRuntimeFrom(version, sourceName, sourceJar string) (model.RuntimeRecord, error) {
	if strings.TrimSpace(version) == "" {
		return model.RuntimeRecord{}, fmt.Errorf("runtime version is required")
	}
	resolved, err := m.resolveInstallSource(version, sourceName, sourceJar)
	if err != nil {
		return model.RuntimeRecord{}, err
	}
	targetJar := m.installedRuntimeJar(version)
	if err := os.MkdirAll(filepath.Dir(targetJar), 0o755); err != nil {
		return model.RuntimeRecord{}, err
	}
	if !samePath(resolved.JarPath, targetJar) {
		if err := copyFile(resolved.JarPath, targetJar); err != nil {
			return model.RuntimeRecord{}, err
		}
	}
	record, err := m.buildRuntimeRecord(version, targetJar, resolved.Source, time.Now().UTC().Format(time.RFC3339))
	if err != nil {
		return model.RuntimeRecord{}, err
	}
	if err := writeJSONFile(m.runtimeMetadataFile(version), record); err != nil {
		return model.RuntimeRecord{}, err
	}
	return record, nil
}

func (m *Manager) loadRuntimeRecord(version string) (model.RuntimeRecord, bool, error) {
	metadataFile := m.runtimeMetadataFile(version)
	body, err := os.ReadFile(metadataFile)
	if err == nil {
		var record model.RuntimeRecord
		if err := json.Unmarshal(body, &record); err != nil {
			return model.RuntimeRecord{}, false, err
		}
		if record.Path == "" {
			record.Path = m.installedRuntimeJar(version)
		}
		if record.MetadataFile == "" {
			record.MetadataFile = metadataFile
		}
		if record.Source == "" {
			record.Source = "local-cache"
		}
		return record, true, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return model.RuntimeRecord{}, false, err
	}
	targetJar := m.installedRuntimeJar(version)
	if _, err := os.Stat(targetJar); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return model.RuntimeRecord{}, false, nil
		}
		return model.RuntimeRecord{}, false, err
	}
	record, err := m.buildRuntimeRecord(version, targetJar, "local-cache", "")
	if err != nil {
		return model.RuntimeRecord{}, false, err
	}
	return record, true, nil
}

func (m *Manager) buildRuntimeRecord(version, jarPath, source, installedAt string) (model.RuntimeRecord, error) {
	digest, err := fileDigest(jarPath)
	if err != nil {
		return model.RuntimeRecord{}, err
	}
	return model.RuntimeRecord{
		Version:      version,
		Path:         jarPath,
		Source:       source,
		Digest:       digest,
		InstalledAt:  installedAt,
		MetadataFile: m.runtimeMetadataFile(version),
	}, nil
}

func (m *Manager) runtimeVersionDir(version string) string {
	return filepath.Join(m.RuntimeDir(), version)
}

func (m *Manager) installedRuntimeJar(version string) string {
	return filepath.Join(m.runtimeVersionDir(version), "sofarpc-worker-"+version+".jar")
}

func (m *Manager) runtimeMetadataFile(version string) string {
	return filepath.Join(m.runtimeVersionDir(version), "runtime.json")
}

func (m *Manager) bundledRuntimeJarCandidates(version string) []string {
	return runtimeJarCandidatesForBase(m.Cwd, version)
}

func runtimeJarCandidatesForBase(basePath, version string) []string {
	jarName := "sofarpc-worker-" + version + ".jar"
	return []string{
		filepath.Join(basePath, jarName),
		filepath.Join(basePath, version, jarName),
		filepath.Join(basePath, "runtime-worker-java", "target", jarName),
	}
}
