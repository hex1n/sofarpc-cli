package runtime

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/hex1n/sofa-rpcctl/greenfield/internal/config"
	"github.com/hex1n/sofa-rpcctl/greenfield/internal/model"
)

type runtimeSourceManifest struct {
	SchemaVersion string                                `json:"schemaVersion,omitempty"`
	Versions      map[string]runtimeSourceManifestEntry `json:"versions,omitempty"`
}

type runtimeSourceManifestEntry struct {
	URL       string `json:"url,omitempty"`
	SHA256    string `json:"sha256,omitempty"`
	SHA256URL string `json:"sha256Url,omitempty"`
}

var sha256Pattern = regexp.MustCompile(`(?i)\b[a-f0-9]{64}\b`)

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

func (m *Manager) ValidateRuntimeSource(version, sourceName string) (model.RuntimeSourceValidation, error) {
	store, err := config.LoadRuntimeSourceStore(m.Paths)
	if err != nil {
		return model.RuntimeSourceValidation{}, err
	}
	source, ok := store.Sources[sourceName]
	if !ok {
		return model.RuntimeSourceValidation{}, fmt.Errorf("runtime source %q does not exist", sourceName)
	}
	if source.Name == "" {
		source.Name = sourceName
	}
	validation := model.RuntimeSourceValidation{
		Name:    source.Name,
		Kind:    source.Kind,
		Version: version,
		Active:  sourceName == store.Active,
	}
	switch source.Kind {
	case "file":
		return m.validateFileRuntimeSource(source, validation), nil
	case "directory":
		return m.validateDirectoryRuntimeSource(version, source, validation), nil
	case "url-template":
		return m.validateURLTemplateRuntimeSource(version, source, validation), nil
	case "manifest-url":
		return m.validateManifestRuntimeSource(version, source, validation), nil
	default:
		validation.Error = fmt.Sprintf("runtime source %q uses unsupported kind %q", source.Name, source.Kind)
		return validation, nil
	}
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
	case "url-template":
		downloadedJar, err := m.downloadRuntimeSource(version, source)
		if err != nil {
			return installSource{}, err
		}
		return installSource{
			JarPath: downloadedJar,
			Source:  "source:" + source.Name,
			Cleanup: func() error {
				return os.Remove(downloadedJar)
			},
		}, nil
	case "manifest-url":
		downloadedJar, err := m.downloadRuntimeSourceFromManifest(version, source)
		if err != nil {
			return installSource{}, err
		}
		return installSource{
			JarPath: downloadedJar,
			Source:  "source:" + source.Name,
			Cleanup: func() error {
				return os.Remove(downloadedJar)
			},
		}, nil
	default:
		return installSource{}, fmt.Errorf("runtime source %q uses unsupported kind %q", source.Name, source.Kind)
	}
}

func (m *Manager) downloadRuntimeSource(version string, source model.RuntimeSource) (string, error) {
	runtimeURL, err := expandRuntimeSourceTemplate(source.Path, version)
	if err != nil {
		return "", fmt.Errorf("runtime source %q: %w", source.Name, err)
	}
	return m.downloadRuntimeArtifact(version, source.Name, runtimeURL, "", source.SHA256URL)
}

func (m *Manager) downloadRuntimeSourceFromManifest(version string, source model.RuntimeSource) (string, error) {
	manifestURL, err := expandRuntimeSourceTemplate(source.Path, version)
	if err != nil {
		return "", fmt.Errorf("runtime source %q: %w", source.Name, err)
	}
	manifest, err := fetchRuntimeSourceManifest(manifestURL)
	if err != nil {
		return "", fmt.Errorf("runtime source %q: %w", source.Name, err)
	}
	if manifest.SchemaVersion != "" && manifest.SchemaVersion != "v1alpha1" {
		return "", fmt.Errorf("runtime source %q manifest uses unsupported schemaVersion %q", source.Name, manifest.SchemaVersion)
	}
	entry, ok := manifest.Versions[version]
	if !ok {
		return "", fmt.Errorf("runtime source %q manifest does not define version %q", source.Name, version)
	}
	runtimeURL, err := expandRuntimeSourceTemplate(entry.URL, version)
	if err != nil {
		return "", fmt.Errorf("runtime source %q manifest entry for version %q: %w", source.Name, version, err)
	}
	return m.downloadRuntimeArtifact(version, source.Name, runtimeURL, entry.SHA256, entry.SHA256URL)
}

func (m *Manager) downloadRuntimeArtifact(version, sourceName, runtimeURL, expectedSHA256, checksumTemplate string) (string, error) {
	request, err := http.NewRequest(http.MethodGet, runtimeURL, nil)
	if err != nil {
		return "", err
	}
	client := &http.Client{Timeout: 30 * time.Second}
	response, err := client.Do(request)
	if err != nil {
		return "", fmt.Errorf("download runtime %q for source %q from %q: %w", version, sourceName, runtimeURL, err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download runtime %q for source %q from %q: unexpected status %s", version, sourceName, runtimeURL, response.Status)
	}
	versionDir := m.runtimeVersionDir(version)
	if err := os.MkdirAll(versionDir, 0o755); err != nil {
		return "", err
	}
	tempFile, err := os.CreateTemp(versionDir, "download-*.jar")
	if err != nil {
		return "", err
	}
	if _, err := io.Copy(tempFile, response.Body); err != nil {
		_ = os.Remove(tempFile.Name())
		_ = tempFile.Close()
		return "", err
	}
	if err := tempFile.Close(); err != nil {
		_ = os.Remove(tempFile.Name())
		return "", err
	}
	if err := m.validateRuntimeArtifactSHA256(version, sourceName, tempFile.Name(), expectedSHA256, checksumTemplate); err != nil {
		_ = os.Remove(tempFile.Name())
		return "", err
	}
	return tempFile.Name(), nil
}

func (m *Manager) validateRuntimeArtifactSHA256(version, sourceName, jarPath, expectedSHA256, checksumTemplate string) error {
	if strings.TrimSpace(expectedSHA256) == "" && strings.TrimSpace(checksumTemplate) == "" {
		return nil
	}
	expected := strings.TrimSpace(expectedSHA256)
	if expected == "" {
		checksumURL, err := expandRuntimeSourceTemplate(checksumTemplate, version)
		if err != nil {
			return fmt.Errorf("runtime source %q checksum: %w", sourceName, err)
		}
		resolved, err := fetchRuntimeSourceChecksum(version, checksumURL)
		if err != nil {
			return fmt.Errorf("runtime source %q checksum: %w", sourceName, err)
		}
		expected = resolved
	}
	expected, err := parseSHA256Digest(expected)
	if err != nil {
		return fmt.Errorf("runtime source %q checksum: %w", sourceName, err)
	}
	actual, err := fileDigest(jarPath)
	if err != nil {
		return err
	}
	if !strings.EqualFold(expected, actual) {
		return fmt.Errorf("runtime source %q checksum mismatch for version %q: expected %s, got %s", sourceName, version, expected, actual)
	}
	return nil
}

func fetchRuntimeSourceManifest(manifestURL string) (runtimeSourceManifest, error) {
	request, err := http.NewRequest(http.MethodGet, manifestURL, nil)
	if err != nil {
		return runtimeSourceManifest{}, err
	}
	client := &http.Client{Timeout: 15 * time.Second}
	response, err := client.Do(request)
	if err != nil {
		return runtimeSourceManifest{}, fmt.Errorf("download manifest from %q: %w", manifestURL, err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return runtimeSourceManifest{}, fmt.Errorf("download manifest from %q: unexpected status %s", manifestURL, response.Status)
	}
	var manifest runtimeSourceManifest
	if err := json.NewDecoder(response.Body).Decode(&manifest); err != nil {
		return runtimeSourceManifest{}, fmt.Errorf("decode manifest from %q: %w", manifestURL, err)
	}
	if manifest.Versions == nil {
		manifest.Versions = map[string]runtimeSourceManifestEntry{}
	}
	return manifest, nil
}

func fetchRuntimeSourceChecksum(version, checksumURL string) (string, error) {
	request, err := http.NewRequest(http.MethodGet, checksumURL, nil)
	if err != nil {
		return "", err
	}
	client := &http.Client{Timeout: 15 * time.Second}
	response, err := client.Do(request)
	if err != nil {
		return "", fmt.Errorf("download checksum for runtime %q from %q: %w", version, checksumURL, err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download checksum for runtime %q from %q: unexpected status %s", version, checksumURL, response.Status)
	}
	body, err := io.ReadAll(response.Body)
	if err != nil {
		return "", err
	}
	return parseSHA256Digest(string(body))
}

func expandRuntimeSourceTemplate(template, version string) (string, error) {
	rawURL := strings.TrimSpace(template)
	if rawURL == "" {
		return "", fmt.Errorf("URL template is empty")
	}
	rawURL = strings.ReplaceAll(rawURL, "{version}", url.PathEscape(version))
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return "", fmt.Errorf("invalid URL template: %w", err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", fmt.Errorf("unsupported url-template scheme %q", parsed.Scheme)
	}
	return parsed.String(), nil
}

func parseSHA256Digest(text string) (string, error) {
	digest := sha256Pattern.FindString(text)
	if digest == "" {
		return "", fmt.Errorf("response does not contain a SHA-256 digest")
	}
	return strings.ToLower(digest), nil
}

func (m *Manager) validateFileRuntimeSource(source model.RuntimeSource, validation model.RuntimeSourceValidation) model.RuntimeSourceValidation {
	jarPath, err := filepath.Abs(source.Path)
	if err != nil {
		validation.Error = err.Error()
		return validation
	}
	validation.ResolvedPath = jarPath
	validation.VersionDefined = true
	if _, err := os.Stat(jarPath); err != nil {
		validation.Error = err.Error()
		return validation
	}
	validation.ArtifactReachable = true
	validation.OK = true
	return validation
}

func (m *Manager) validateDirectoryRuntimeSource(version string, source model.RuntimeSource, validation model.RuntimeSourceValidation) model.RuntimeSourceValidation {
	basePath, err := filepath.Abs(source.Path)
	if err != nil {
		validation.Error = err.Error()
		return validation
	}
	for _, candidate := range runtimeJarCandidatesForBase(basePath, version) {
		if _, err := os.Stat(candidate); err == nil {
			validation.VersionDefined = true
			validation.ArtifactReachable = true
			validation.ResolvedPath = candidate
			validation.OK = true
			return validation
		}
	}
	validation.Error = fmt.Sprintf("runtime source %q does not provide runtime %q", source.Name, version)
	return validation
}

func (m *Manager) validateURLTemplateRuntimeSource(version string, source model.RuntimeSource, validation model.RuntimeSourceValidation) model.RuntimeSourceValidation {
	runtimeURL, err := expandRuntimeSourceTemplate(source.Path, version)
	if err != nil {
		validation.Error = err.Error()
		return validation
	}
	validation.VersionDefined = true
	validation.ResolvedURL = runtimeURL
	validation.ArtifactReachable, err = probeHTTPURL(runtimeURL)
	if err != nil {
		validation.Error = err.Error()
		return validation
	}
	if strings.TrimSpace(source.SHA256URL) != "" {
		checksumURL, err := expandRuntimeSourceTemplate(source.SHA256URL, version)
		if err != nil {
			validation.Error = err.Error()
			return validation
		}
		validation.ChecksumMode = "sha256-url"
		validation.ChecksumURL = checksumURL
		validation.ChecksumAvailable, err = probeHTTPURL(checksumURL)
		if err != nil {
			validation.Error = err.Error()
			return validation
		}
	}
	validation.OK = validation.ArtifactReachable && (validation.ChecksumMode == "" || validation.ChecksumAvailable)
	if !validation.OK && validation.Error == "" {
		validation.Error = fmt.Sprintf("runtime source %q is not reachable for version %q", source.Name, version)
	}
	return validation
}

func (m *Manager) validateManifestRuntimeSource(version string, source model.RuntimeSource, validation model.RuntimeSourceValidation) model.RuntimeSourceValidation {
	manifestURL, err := expandRuntimeSourceTemplate(source.Path, version)
	if err != nil {
		validation.Error = err.Error()
		return validation
	}
	validation.ManifestURL = manifestURL
	manifest, err := fetchRuntimeSourceManifest(manifestURL)
	if err != nil {
		validation.Error = err.Error()
		return validation
	}
	if manifest.SchemaVersion != "" && manifest.SchemaVersion != "v1alpha1" {
		validation.Error = fmt.Sprintf("runtime source %q manifest uses unsupported schemaVersion %q", source.Name, manifest.SchemaVersion)
		return validation
	}
	entry, ok := manifest.Versions[version]
	if !ok {
		validation.Error = fmt.Sprintf("runtime source %q manifest does not define version %q", source.Name, version)
		return validation
	}
	validation.VersionDefined = true
	runtimeURL, err := expandRuntimeSourceTemplate(entry.URL, version)
	if err != nil {
		validation.Error = err.Error()
		return validation
	}
	validation.ResolvedURL = runtimeURL
	validation.ArtifactReachable, err = probeHTTPURL(runtimeURL)
	if err != nil {
		validation.Error = err.Error()
		return validation
	}
	if strings.TrimSpace(entry.SHA256) != "" {
		validation.ChecksumMode = "inline-sha256"
		validation.ChecksumAvailable = true
		if _, err := parseSHA256Digest(entry.SHA256); err != nil {
			validation.ChecksumAvailable = false
			validation.Error = err.Error()
			return validation
		}
	} else if strings.TrimSpace(entry.SHA256URL) != "" {
		checksumURL, err := expandRuntimeSourceTemplate(entry.SHA256URL, version)
		if err != nil {
			validation.Error = err.Error()
			return validation
		}
		validation.ChecksumMode = "sha256-url"
		validation.ChecksumURL = checksumURL
		validation.ChecksumAvailable, err = probeHTTPURL(checksumURL)
		if err != nil {
			validation.Error = err.Error()
			return validation
		}
	}
	validation.OK = validation.ArtifactReachable && (validation.ChecksumMode == "" || validation.ChecksumAvailable)
	if !validation.OK && validation.Error == "" {
		validation.Error = fmt.Sprintf("runtime source %q is not reachable for version %q", source.Name, version)
	}
	return validation
}

func probeHTTPURL(rawURL string) (bool, error) {
	request, err := http.NewRequest(http.MethodHead, rawURL, nil)
	if err != nil {
		return false, err
	}
	client := &http.Client{Timeout: 15 * time.Second}
	response, err := client.Do(request)
	if err != nil {
		return false, err
	}
	defer response.Body.Close()
	if response.StatusCode >= 200 && response.StatusCode < 400 {
		return true, nil
	}
	if response.StatusCode == http.StatusMethodNotAllowed || response.StatusCode == http.StatusNotImplemented {
		return probeHTTPURLWithGet(rawURL)
	}
	return false, fmt.Errorf("unexpected status %s", response.Status)
}

func probeHTTPURLWithGet(rawURL string) (bool, error) {
	request, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		return false, err
	}
	client := &http.Client{Timeout: 15 * time.Second}
	response, err := client.Do(request)
	if err != nil {
		return false, err
	}
	defer response.Body.Close()
	if response.StatusCode >= 200 && response.StatusCode < 400 {
		return true, nil
	}
	return false, fmt.Errorf("unexpected status %s", response.Status)
}
