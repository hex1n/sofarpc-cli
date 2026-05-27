// Package projectconfig owns the on-disk .sofarpc project configuration
// format shared by CLI setup and MCP project initialization.
package projectconfig

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

type Kind string

const (
	KindLocal  Kind = "local"
	KindShared Kind = "shared"
)

const LocalGitignoreEntry = ".sofarpc/config.local.json"

type Config struct {
	DirectURL        string   `json:"directUrl,omitempty"`
	RegistryAddress  string   `json:"registryAddress,omitempty"`
	RegistryProtocol string   `json:"registryProtocol,omitempty"`
	Protocol         string   `json:"protocol,omitempty"`
	Serialization    string   `json:"serialization,omitempty"`
	UniqueID         string   `json:"uniqueId,omitempty"`
	TimeoutMS        int      `json:"timeoutMs,omitempty"`
	ConnectTimeoutMS int      `json:"connectTimeoutMs,omitempty"`
	AllowedServices  []string `json:"allowedServices,omitempty"`
}

type WriteResult struct {
	Path      string
	Body      []byte
	Overwrote bool
}

type GitignoreResult struct {
	Path    string
	Entry   string
	Changed bool
}

func ParseKind(raw string) (Kind, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", string(KindLocal):
		return KindLocal, nil
	case string(KindShared):
		return KindShared, nil
	default:
		return "", fmt.Errorf("invalid config %q: expected local or shared", raw)
	}
}

func ConfigPath(projectRoot string, kind Kind) string {
	name := "config.local.json"
	if kind == KindShared {
		name = "config.json"
	}
	return filepath.Join(projectRoot, ".sofarpc", name)
}

func Marshal(cfg Config) ([]byte, error) {
	if err := Validate(cfg); err != nil {
		return nil, err
	}
	body, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(body, '\n'), nil
}

func Validate(cfg Config) error {
	if strings.TrimSpace(cfg.DirectURL) != "" && strings.TrimSpace(cfg.RegistryAddress) != "" {
		return fmt.Errorf("directUrl and registryAddress are mutually exclusive")
	}
	return nil
}

func Write(projectRoot string, kind Kind, cfg Config, force bool) (WriteResult, error) {
	body, err := Marshal(cfg)
	if err != nil {
		return WriteResult{}, err
	}
	path := ConfigPath(projectRoot, kind)
	exists, err := fileExists(path)
	if err != nil {
		return WriteResult{}, err
	}
	if exists && !force {
		return WriteResult{}, fmt.Errorf("%s already exists; pass force=true to overwrite", path)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return WriteResult{}, err
	}
	if err := atomicWrite(path, body); err != nil {
		return WriteResult{}, err
	}
	return WriteResult{Path: path, Body: body, Overwrote: exists}, nil
}

func Existing(path string) (bool, error) {
	return fileExists(path)
}

func EnsureLocalConfigIgnored(projectRoot string) (GitignoreResult, error) {
	path := filepath.Join(projectRoot, ".gitignore")
	existing, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return GitignoreResult{}, err
	}
	body, changed := AppendGitignoreEntry(string(existing), LocalGitignoreEntry)
	if !changed {
		return GitignoreResult{Path: path, Entry: LocalGitignoreEntry}, nil
	}
	if err := atomicWrite(path, []byte(body)); err != nil {
		return GitignoreResult{}, err
	}
	return GitignoreResult{Path: path, Entry: LocalGitignoreEntry, Changed: true}, nil
}

func LocalConfigIgnoreStatus(projectRoot string) (GitignoreResult, error) {
	path := filepath.Join(projectRoot, ".gitignore")
	existing, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return GitignoreResult{}, err
	}
	_, changed := AppendGitignoreEntry(string(existing), LocalGitignoreEntry)
	return GitignoreResult{Path: path, Entry: LocalGitignoreEntry, Changed: changed}, nil
}

func AppendGitignoreEntry(body, entry string) (string, bool) {
	for _, line := range strings.Split(body, "\n") {
		if strings.TrimSpace(line) == entry {
			return body, false
		}
	}
	body = strings.TrimRight(body, "\n")
	if body == "" {
		return entry + "\n", true
	}
	return body + "\n" + entry + "\n", true
}

func fileExists(path string) (bool, error) {
	if _, err := os.Stat(path); err == nil {
		return true, nil
	} else if !errors.Is(err, fs.ErrNotExist) {
		return false, err
	}
	return false, nil
}

func atomicWrite(path string, body []byte) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".sofarpc-mcp-*")
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
