package runtime

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/hex1n/sofa-rpcctl/greenfield/internal/config"
	"github.com/hex1n/sofa-rpcctl/greenfield/internal/model"
)

const (
	defaultRuntimeVersion = "5.7.6"
	defaultTimeoutMS      = 3000
	defaultConnectMS      = 1000
	mainClass             = "com.hex1n.rpcctl.greenfield.worker.WorkerMain"
)

type Manager struct {
	Paths config.Paths
	Cwd   string
}

type Spec struct {
	SofaRPCVersion string
	JavaBin        string
	JavaMajor      string
	RuntimeJar     string
	RuntimeDigest  string
	StubPaths      []string
	ClasspathHash  string
	DaemonKey      string
	MetadataFile   string
	StdoutLog      string
	StderrLog      string
}

type installSource struct {
	JarPath string
	Source  string
	Cleanup func() error
}

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

func NewManager(paths config.Paths, cwd string) *Manager {
	return &Manager{Paths: paths, Cwd: cwd}
}

func (m *Manager) DaemonDir() string {
	return filepath.Join(m.Paths.CacheDir, "daemons")
}

func (m *Manager) RuntimeDir() string {
	return filepath.Join(m.Paths.CacheDir, "runtimes")
}

func (m *Manager) ResolveSpec(javaBin, runtimeJar, version string, stubPaths []string) (Spec, error) {
	if javaBin == "" {
		javaBin = "java"
	}
	if version == "" {
		version = defaultRuntimeVersion
	}
	if runtimeJar == "" {
		runtimeJar = m.defaultRuntimeJar(version)
	}
	runtimeJar, err := filepath.Abs(runtimeJar)
	if err != nil {
		return Spec{}, err
	}
	javaMajor, err := detectJavaMajor(javaBin)
	if err != nil {
		return Spec{}, err
	}
	digest, err := fileDigest(runtimeJar)
	if err != nil {
		return Spec{}, err
	}
	normalized, err := normalizePaths(stubPaths)
	if err != nil {
		return Spec{}, err
	}
	classpathHash := hashStrings(normalized)
	key := hashStrings([]string{version, digest, classpathHash, javaMajor})
	daemonDir := m.DaemonDir()
	return Spec{
		SofaRPCVersion: version,
		JavaBin:        javaBin,
		JavaMajor:      javaMajor,
		RuntimeJar:     runtimeJar,
		RuntimeDigest:  digest,
		StubPaths:      normalized,
		ClasspathHash:  classpathHash,
		DaemonKey:      key,
		MetadataFile:   filepath.Join(daemonDir, key+".json"),
		StdoutLog:      filepath.Join(daemonDir, key+".stdout.log"),
		StderrLog:      filepath.Join(daemonDir, key+".stderr.log"),
	}, nil
}

func (m *Manager) ListDaemons() ([]model.DaemonRecord, error) {
	files, err := filepath.Glob(filepath.Join(m.DaemonDir(), "*.json"))
	if err != nil {
		return nil, err
	}
	records := make([]model.DaemonRecord, 0, len(files))
	for _, file := range files {
		records = append(records, m.daemonRecordFromFile(file))
	}
	sort.Slice(records, func(i, j int) bool {
		left := daemonSortValue(records[i])
		right := daemonSortValue(records[j])
		if left == right {
			return records[i].Key < records[j].Key
		}
		return left > right
	})
	return records, nil
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

func (m *Manager) InstallRuntimeFrom(version, sourceName, sourceJar string) (model.RuntimeRecord, error) {
	if strings.TrimSpace(version) == "" {
		return model.RuntimeRecord{}, fmt.Errorf("runtime version is required")
	}
	resolved, err := m.resolveInstallSource(version, sourceName, sourceJar)
	if err != nil {
		return model.RuntimeRecord{}, err
	}
	if resolved.Cleanup != nil {
		defer func() {
			_ = resolved.Cleanup()
		}()
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

func (m *Manager) GetDaemon(key string) (model.DaemonRecord, error) {
	path := m.metadataFileForKey(key)
	if _, err := os.Stat(path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return model.DaemonRecord{}, fmt.Errorf("daemon %q does not exist", key)
		}
		return model.DaemonRecord{}, err
	}
	record := m.daemonRecordFromFile(path)
	if record.Key == "" {
		return model.DaemonRecord{}, fmt.Errorf("daemon %q does not exist", key)
	}
	return record, nil
}

func (m *Manager) StopDaemon(key string) (model.DaemonAction, error) {
	record, err := m.GetDaemon(key)
	if err != nil {
		return model.DaemonAction{}, err
	}
	action := model.DaemonAction{Daemon: record}
	if record.Ready {
		signaled, stopErr := stopLoopbackDaemon(record)
		if stopErr != nil {
			return model.DaemonAction{}, stopErr
		}
		action.SignaledProcess = signaled
	}
	removed, err := removeIfExists(record.MetadataFile)
	if err != nil {
		return model.DaemonAction{}, err
	}
	action.RemovedMetadata = removed
	action.Daemon.Ready = false
	action.Daemon.Stale = true
	return action, nil
}

func (m *Manager) PruneDaemons() ([]model.DaemonAction, error) {
	records, err := m.ListDaemons()
	if err != nil {
		return nil, err
	}
	actions := make([]model.DaemonAction, 0, len(records))
	for _, record := range records {
		if record.Ready {
			continue
		}
		action := model.DaemonAction{Daemon: record}
		if action.RemovedMetadata, err = removeIfExists(record.MetadataFile); err != nil {
			return nil, err
		}
		if action.RemovedStdoutLog, err = removeIfExists(record.StdoutLog); err != nil {
			return nil, err
		}
		if action.RemovedStderrLog, err = removeIfExists(record.StderrLog); err != nil {
			return nil, err
		}
		actions = append(actions, action)
	}
	return actions, nil
}

func (m *Manager) EnsureDaemon(ctx context.Context, spec Spec) (model.DaemonMetadata, error) {
	if err := os.MkdirAll(filepath.Dir(spec.MetadataFile), 0o755); err != nil {
		return model.DaemonMetadata{}, err
	}
	if metadata, ok := m.loadMetadata(spec.MetadataFile); ok && daemonReachable(metadata) {
		return metadata, nil
	}
	if err := os.Remove(spec.MetadataFile); err != nil && !errors.Is(err, os.ErrNotExist) {
		return model.DaemonMetadata{}, err
	}
	address, err := chooseAddress()
	if err != nil {
		return model.DaemonMetadata{}, err
	}
	if err := m.startDaemon(spec, address); err != nil {
		return model.DaemonMetadata{}, err
	}
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return model.DaemonMetadata{}, ctx.Err()
		default:
		}
		if metadata, ok := m.loadMetadata(spec.MetadataFile); ok && daemonReachable(metadata) {
			return metadata, nil
		}
		time.Sleep(200 * time.Millisecond)
	}
	return model.DaemonMetadata{}, fmt.Errorf("worker daemon did not become ready: %s", spec.StderrLog)
}

func (m *Manager) Invoke(ctx context.Context, metadata model.DaemonMetadata, request model.InvocationRequest) (model.InvocationResponse, error) {
	address := net.JoinHostPort(metadata.Host, strconv.Itoa(metadata.Port))
	conn, err := net.DialTimeout("tcp", address, 2*time.Second)
	if err != nil {
		return model.InvocationResponse{}, err
	}
	defer conn.Close()
	timeout := max(request.Target.TimeoutMS, defaultTimeoutMS) + 5000
	_ = conn.SetDeadline(time.Now().Add(time.Duration(timeout) * time.Millisecond))
	select {
	case <-ctx.Done():
		return model.InvocationResponse{}, ctx.Err()
	default:
	}
	if err := json.NewEncoder(conn).Encode(request); err != nil {
		return model.InvocationResponse{}, err
	}
	var response model.InvocationResponse
	if err := json.NewDecoder(conn).Decode(&response); err != nil {
		return model.InvocationResponse{}, err
	}
	return response, nil
}

func ProbeTarget(target model.TargetConfig) model.ProbeResult {
	endpoint := configuredTarget(target)
	if endpoint == "" {
		return model.ProbeResult{Reachable: false, Message: "no direct or registry target configured"}
	}
	address, err := dialAddress(target)
	if err != nil {
		return model.ProbeResult{Reachable: false, Target: endpoint, Message: err.Error()}
	}
	timeout := time.Duration(max(target.ConnectTimeoutMS, defaultConnectMS)) * time.Millisecond
	conn, err := net.DialTimeout("tcp", address, timeout)
	if err != nil {
		return model.ProbeResult{Reachable: false, Target: endpoint, Message: err.Error()}
	}
	_ = conn.Close()
	return model.ProbeResult{Reachable: true, Target: endpoint, Message: "tcp probe succeeded"}
}

func ScanStubWarnings(stubPaths []string) []string {
	patterns := []*regexp.Regexp{
		regexp.MustCompile(`(?i)guava-`),
		regexp.MustCompile(`(?i)logback-`),
		regexp.MustCompile(`(?i)slf4j-`),
		regexp.MustCompile(`(?i)jackson-`),
		regexp.MustCompile(`(?i)spring-boot`),
	}
	var warnings []string
	for _, path := range stubPaths {
		base := filepath.Base(path)
		for _, pattern := range patterns {
			if pattern.MatchString(base) {
				warnings = append(warnings, fmt.Sprintf("high-risk classpath entry: %s", base))
				break
			}
		}
	}
	return warnings
}

func normalizePaths(paths []string) ([]string, error) {
	var normalized []string
	for _, path := range paths {
		if strings.TrimSpace(path) == "" {
			continue
		}
		absolute, err := filepath.Abs(path)
		if err != nil {
			return nil, err
		}
		normalized = append(normalized, filepath.Clean(absolute))
	}
	return normalized, nil
}

func detectJavaMajor(javaBin string) (string, error) {
	output, err := exec.Command(javaBin, "-version").CombinedOutput()
	if err != nil {
		return "", err
	}
	text := string(output)
	start := strings.IndexByte(text, '"')
	if start < 0 {
		return "", fmt.Errorf("unable to parse java version output")
	}
	end := strings.IndexByte(text[start+1:], '"')
	if end < 0 {
		return "", fmt.Errorf("unable to parse java version output")
	}
	version := text[start+1 : start+1+end]
	if strings.HasPrefix(version, "1.") {
		parts := strings.Split(version, ".")
		if len(parts) > 1 {
			return parts[1], nil
		}
	}
	return strings.Split(version, ".")[0], nil
}

func fileDigest(path string) (string, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	hash := sha256.Sum256(body)
	return hex.EncodeToString(hash[:]), nil
}

func hashStrings(values []string) string {
	hash := sha256.New()
	for _, value := range values {
		_, _ = hash.Write([]byte(value))
		_, _ = hash.Write([]byte{'\n'})
	}
	return hex.EncodeToString(hash.Sum(nil))
}

func (m *Manager) loadMetadata(path string) (model.DaemonMetadata, bool) {
	var metadata model.DaemonMetadata
	body, err := os.ReadFile(path)
	if err != nil {
		return metadata, false
	}
	if err := json.Unmarshal(body, &metadata); err != nil {
		return metadata, false
	}
	return metadata, true
}

func (m *Manager) metadataFileForKey(key string) string {
	return filepath.Join(m.DaemonDir(), key+".json")
}

func (m *Manager) runtimeVersionDir(version string) string {
	return filepath.Join(m.RuntimeDir(), version)
}

func (m *Manager) installedRuntimeJar(version string) string {
	return filepath.Join(m.runtimeVersionDir(version), "rpc-runtime-worker-sofa-"+version+".jar")
}

func (m *Manager) runtimeMetadataFile(version string) string {
	return filepath.Join(m.runtimeVersionDir(version), "runtime.json")
}

func (m *Manager) daemonRecordFromFile(path string) model.DaemonRecord {
	key := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	record := model.DaemonRecord{
		Key:          key,
		MetadataFile: path,
		StdoutLog:    filepath.Join(m.DaemonDir(), key+".stdout.log"),
		StderrLog:    filepath.Join(m.DaemonDir(), key+".stderr.log"),
	}
	metadata, ok := m.loadMetadata(path)
	if !ok {
		if _, err := os.Stat(path); err == nil {
			record.Error = "metadata is unreadable"
		}
		record.Stale = true
		return record
	}
	record.Metadata = &metadata
	record.Ready = daemonReachable(metadata)
	record.Stale = !record.Ready
	return record
}

func daemonReachable(metadata model.DaemonMetadata) bool {
	if metadata.Host == "" || metadata.Port == 0 {
		return false
	}
	conn, err := net.DialTimeout("tcp", net.JoinHostPort(metadata.Host, strconv.Itoa(metadata.Port)), 500*time.Millisecond)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

func daemonSortValue(record model.DaemonRecord) string {
	if record.Metadata != nil {
		return record.Metadata.StartedAt
	}
	return ""
}

func stopLoopbackDaemon(record model.DaemonRecord) (bool, error) {
	if record.Metadata == nil {
		return false, nil
	}
	if record.Metadata.PID <= 0 {
		return false, fmt.Errorf("daemon %q is missing a valid pid", record.Key)
	}
	if !isLoopbackHost(record.Metadata.Host) {
		return false, fmt.Errorf("refusing to stop non-loopback daemon %q", record.Key)
	}
	process, err := os.FindProcess(record.Metadata.PID)
	if err != nil {
		return false, err
	}
	defer process.Release()
	if err := process.Kill(); err != nil {
		if !strings.Contains(strings.ToLower(err.Error()), "finished") &&
			!strings.Contains(strings.ToLower(err.Error()), "no process") {
			return false, err
		}
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if record.Metadata == nil || !daemonReachable(*record.Metadata) {
			return true, nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return true, nil
}

func isLoopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func removeIfExists(path string) (bool, error) {
	if strings.TrimSpace(path) == "" {
		return false, nil
	}
	if err := os.Remove(path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

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

func chooseAddress() (string, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", err
	}
	address := listener.Addr().String()
	if err := listener.Close(); err != nil {
		return "", err
	}
	return address, nil
}

func (m *Manager) startDaemon(spec Spec, address string) error {
	stdoutFile, err := os.Create(spec.StdoutLog)
	if err != nil {
		return err
	}
	defer stdoutFile.Close()
	stderrFile, err := os.Create(spec.StderrLog)
	if err != nil {
		return err
	}
	defer stderrFile.Close()
	classpathParts := append([]string{spec.RuntimeJar}, spec.StubPaths...)
	command := exec.Command(spec.JavaBin,
		"-cp",
		strings.Join(classpathParts, string(os.PathListSeparator)),
		mainClass,
		"serve",
		"--listen",
		address,
		"--metadata-file",
		spec.MetadataFile,
	)
	command.Stdout = stdoutFile
	command.Stderr = stderrFile
	command.Env = os.Environ()
	if err := command.Start(); err != nil {
		return err
	}
	return command.Process.Release()
}

func configuredTarget(target model.TargetConfig) string {
	switch target.Mode {
	case model.ModeDirect:
		return target.DirectURL
	case model.ModeRegistry:
		return target.RegistryProtocol + "://" + target.RegistryAddress
	default:
		return ""
	}
}

func dialAddress(target model.TargetConfig) (string, error) {
	raw := configuredTarget(target)
	parsed, err := url.Parse(raw)
	if err == nil && parsed.Host != "" {
		return parsed.Host, nil
	}
	if strings.Contains(raw, "://") {
		raw = strings.SplitN(raw, "://", 2)[1]
	}
	if strings.TrimSpace(raw) == "" {
		return "", fmt.Errorf("target address is empty")
	}
	return raw, nil
}

func max(left, right int) int {
	if left > right {
		return left
	}
	return right
}

func (m *Manager) defaultRuntimeJar(version string) string {
	installed := m.installedRuntimeJar(version)
	if _, err := os.Stat(installed); err == nil {
		return installed
	}
	for _, candidate := range m.bundledRuntimeJarCandidates(version) {
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	candidates := m.bundledRuntimeJarCandidates(version)
	return candidates[0]
}

func (m *Manager) bundledRuntimeJarCandidates(version string) []string {
	return runtimeJarCandidatesForBase(m.Cwd, version)
}

func runtimeJarCandidatesForBase(basePath, version string) []string {
	jarName := "rpc-runtime-worker-sofa-" + version + ".jar"
	return []string{
		filepath.Join(basePath, jarName),
		filepath.Join(basePath, version, jarName),
		filepath.Join(basePath, "runtime-worker-java", "target", jarName),
		filepath.Join(basePath, "greenfield", "runtime-worker-java", "target", jarName),
	}
}

func samePath(left, right string) bool {
	return strings.EqualFold(filepath.Clean(left), filepath.Clean(right))
}

func copyFile(source, target string) error {
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	output, err := os.Create(target)
	if err != nil {
		return err
	}
	defer output.Close()
	if _, err := io.Copy(output, input); err != nil {
		return err
	}
	return output.Close()
}

func writeJSONFile(path string, value any) error {
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
