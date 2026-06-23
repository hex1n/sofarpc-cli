// Package projectconfig owns the on-disk .sofarpc project configuration
// format shared by CLI setup and MCP project initialization.
package projectconfig

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/hex1n/sofarpc-cli/internal/core/invocationprops"
)

type Kind string

const (
	KindLocal  Kind = "local"
	KindShared Kind = "shared"
)

const LocalGitignoreEntry = ".sofarpc/config.local.json"

type Config struct {
	DirectURL            string                       `json:"directUrl,omitempty"`
	RegistryAddress      string                       `json:"registryAddress,omitempty"`
	RegistryProtocol     string                       `json:"registryProtocol,omitempty"`
	Protocol             string                       `json:"protocol,omitempty"`
	Serialization        string                       `json:"serialization,omitempty"`
	UniqueID             string                       `json:"uniqueId,omitempty"`
	TimeoutMS            int                          `json:"timeoutMs,omitempty"`
	ConnectTimeoutMS     int                          `json:"connectTimeoutMs,omitempty"`
	AllowedServices      []string                     `json:"allowedServices,omitempty"`
	InvocationProperties invocationprops.Declarations `json:"invocationProperties,omitempty"`
	// DefaultProfile names the Target Profile to use when a call selects
	// none. A local declaration wins over a shared one (see ADR 0003).
	DefaultProfile string `json:"defaultProfile,omitempty"`
	// Profiles holds named Target Profiles. The top-level fields above act
	// as Base Target Settings that a selected profile overlays.
	Profiles map[string]ProfileConfig `json:"profiles,omitempty"`
}

// ProfileConfig is one named Target Profile inside a project config file. It
// mirrors the target/wire/invocation-context fields of Config but never
// carries allowedServices: the service allowlist stays a project-wide
// guardrail independent of the active profile (see ADR 0003). Because the
// parser uses DisallowUnknownFields, an allowedServices key inside a profile
// is rejected automatically.
type ProfileConfig struct {
	DirectURL            string                       `json:"directUrl,omitempty"`
	RegistryAddress      string                       `json:"registryAddress,omitempty"`
	RegistryProtocol     string                       `json:"registryProtocol,omitempty"`
	Protocol             string                       `json:"protocol,omitempty"`
	Serialization        string                       `json:"serialization,omitempty"`
	UniqueID             string                       `json:"uniqueId,omitempty"`
	TimeoutMS            int                          `json:"timeoutMs,omitempty"`
	ConnectTimeoutMS     int                          `json:"connectTimeoutMs,omitempty"`
	InvocationProperties invocationprops.Declarations `json:"invocationProperties,omitempty"`
}

type ReadResult struct {
	Path               string
	Kind               Kind
	Exists             bool
	Config             Config
	AllowedServicesSet bool
}

type fileConfig struct {
	DirectURL            string                       `json:"directUrl,omitempty"`
	RegistryAddress      string                       `json:"registryAddress,omitempty"`
	RegistryProtocol     string                       `json:"registryProtocol,omitempty"`
	Protocol             string                       `json:"protocol,omitempty"`
	Serialization        string                       `json:"serialization,omitempty"`
	UniqueID             string                       `json:"uniqueId,omitempty"`
	TimeoutMS            int                          `json:"timeoutMs,omitempty"`
	ConnectTimeoutMS     int                          `json:"connectTimeoutMs,omitempty"`
	AllowedServices      *[]string                    `json:"allowedServices,omitempty"`
	InvocationProperties invocationprops.Declarations `json:"invocationProperties,omitempty"`
	DefaultProfile       string                       `json:"defaultProfile,omitempty"`
	Profiles             map[string]ProfileConfig     `json:"profiles,omitempty"`
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

func Read(projectRoot string, kind Kind) (ReadResult, error) {
	result := ReadResult{
		Path: ConfigPath(projectRoot, kind),
		Kind: kind,
	}
	body, err := os.ReadFile(result.Path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return result, nil
		}
		return result, err
	}
	result.Exists = true

	cfg, allowedServicesSet, err := parse(body)
	if err != nil {
		return result, err
	}
	result.Config = cfg
	result.AllowedServicesSet = allowedServicesSet
	return result, nil
}

func Marshal(cfg Config) ([]byte, error) {
	prepared, err := prepareForMarshal(cfg)
	if err != nil {
		return nil, err
	}
	body, err := json.MarshalIndent(prepared, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(body, '\n'), nil
}

// MarshalMerged is Marshal but, when allowedServicesSet is true, preserves an
// explicitly-empty allowedServices ([] meaning block-all) instead of letting
// omitempty drop it. Read-modify-write merges pass the AllowedServicesSet flag
// from the prior Read so a profile edit never silently widens the allowlist.
func MarshalMerged(cfg Config, allowedServicesSet bool) ([]byte, error) {
	prepared, err := prepareForMarshal(cfg)
	if err != nil {
		return nil, err
	}
	if !allowedServicesSet || len(prepared.AllowedServices) > 0 {
		body, err := json.MarshalIndent(prepared, "", "  ")
		if err != nil {
			return nil, err
		}
		return append(body, '\n'), nil
	}
	body, err := json.MarshalIndent(configToFile(prepared, true), "", "  ")
	if err != nil {
		return nil, err
	}
	return append(body, '\n'), nil
}

// prepareForMarshal validates and normalizes cfg for serialization. It is the
// shared front half of Marshal / MarshalMerged.
func prepareForMarshal(cfg Config) (Config, error) {
	if err := validateProfileKeys(cfg.Profiles); err != nil {
		return Config{}, err
	}
	cfg = Normalize(cfg)
	props, err := invocationprops.NormalizeInput(cfg.InvocationProperties)
	if err != nil {
		return Config{}, err
	}
	cfg.InvocationProperties = props
	if cfg.Profiles, err = normalizeProfileInvocationProps(cfg.Profiles); err != nil {
		return Config{}, err
	}
	if err := Validate(cfg); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// configToFile maps a Config back to the on-disk fileConfig shape. The only
// field that needs special handling is allowedServices: fileConfig uses a
// pointer so an explicitly-empty allowlist (allowedServicesSet, len 0) round
// trips as `[]` rather than being dropped by omitempty.
func configToFile(cfg Config, allowedServicesSet bool) fileConfig {
	fc := fileConfig{
		DirectURL:            cfg.DirectURL,
		RegistryAddress:      cfg.RegistryAddress,
		RegistryProtocol:     cfg.RegistryProtocol,
		Protocol:             cfg.Protocol,
		Serialization:        cfg.Serialization,
		UniqueID:             cfg.UniqueID,
		TimeoutMS:            cfg.TimeoutMS,
		ConnectTimeoutMS:     cfg.ConnectTimeoutMS,
		InvocationProperties: cfg.InvocationProperties,
		DefaultProfile:       cfg.DefaultProfile,
		Profiles:             cfg.Profiles,
	}
	if allowedServicesSet || len(cfg.AllowedServices) > 0 {
		// Non-nil empty slice so an explicit block-all allowlist marshals as
		// `[]` rather than `null`.
		services := append([]string{}, cfg.AllowedServices...)
		fc.AllowedServices = &services
	}
	return fc
}

// SetProfile returns a copy of cfg with profiles[name] set to profile, leaving
// every other field (including other profiles) untouched. It is a pure
// transform; callers own read/write and force semantics.
func SetProfile(cfg Config, name string, profile ProfileConfig) Config {
	name = strings.TrimSpace(name)
	profiles := make(map[string]ProfileConfig, len(cfg.Profiles)+1)
	for k, v := range cfg.Profiles {
		profiles[k] = v
	}
	profiles[name] = profile
	cfg.Profiles = profiles
	return cfg
}

// SetDefaultProfile returns a copy of cfg with DefaultProfile set to name.
func SetDefaultProfile(cfg Config, name string) Config {
	cfg.DefaultProfile = strings.TrimSpace(name)
	return cfg
}

// HasProfile reports whether cfg already defines a profile named name.
func HasProfile(cfg Config, name string) bool {
	_, ok := cfg.Profiles[strings.TrimSpace(name)]
	return ok
}

// ProfileHasFields reports whether p carries at least one target/wire field or
// invocation property — an empty profile is a no-op and callers should reject it.
func ProfileHasFields(p ProfileConfig) bool {
	return strings.TrimSpace(p.DirectURL) != "" ||
		strings.TrimSpace(p.RegistryAddress) != "" ||
		strings.TrimSpace(p.RegistryProtocol) != "" ||
		strings.TrimSpace(p.Protocol) != "" ||
		strings.TrimSpace(p.Serialization) != "" ||
		strings.TrimSpace(p.UniqueID) != "" ||
		p.TimeoutMS > 0 ||
		p.ConnectTimeoutMS > 0 ||
		len(p.InvocationProperties) > 0
}

func Validate(cfg Config) error {
	if strings.TrimSpace(cfg.DirectURL) != "" && strings.TrimSpace(cfg.RegistryAddress) != "" {
		return fmt.Errorf("directUrl and registryAddress are mutually exclusive")
	}
	if _, err := invocationprops.NormalizeInput(cfg.InvocationProperties); err != nil {
		return err
	}
	if err := validateProfileKeys(cfg.Profiles); err != nil {
		return err
	}
	return validateProfileContents(cfg.Profiles)
}

func Normalize(cfg Config) Config {
	cfg.DirectURL = strings.TrimSpace(cfg.DirectURL)
	cfg.RegistryAddress = strings.TrimSpace(cfg.RegistryAddress)
	cfg.RegistryProtocol = strings.TrimSpace(cfg.RegistryProtocol)
	cfg.Protocol = strings.TrimSpace(cfg.Protocol)
	cfg.Serialization = strings.TrimSpace(cfg.Serialization)
	cfg.UniqueID = strings.TrimSpace(cfg.UniqueID)
	cfg.DefaultProfile = strings.TrimSpace(cfg.DefaultProfile)
	cfg.AllowedServices = normalizeStringList(cfg.AllowedServices)
	cfg.Profiles = normalizeProfiles(cfg.Profiles)
	return cfg
}

// validateProfileKeys rejects empty or trim-colliding profile names. It runs
// on the raw (pre-normalized) map so a "test " vs "test" collision is caught
// before normalizeProfiles would silently collapse it.
func validateProfileKeys(profiles map[string]ProfileConfig) error {
	if len(profiles) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(profiles))
	for rawName := range profiles {
		name := strings.TrimSpace(rawName)
		if name == "" {
			return fmt.Errorf("profile name must not be empty")
		}
		if _, dup := seen[name]; dup {
			return fmt.Errorf("profile name %q is duplicated after trimming", name)
		}
		seen[name] = struct{}{}
	}
	return nil
}

func validateProfileContents(profiles map[string]ProfileConfig) error {
	for rawName, p := range profiles {
		name := strings.TrimSpace(rawName)
		if strings.TrimSpace(p.DirectURL) != "" && strings.TrimSpace(p.RegistryAddress) != "" {
			return fmt.Errorf("profile %q: directUrl and registryAddress are mutually exclusive", name)
		}
		if _, err := invocationprops.NormalizeInput(p.InvocationProperties); err != nil {
			return fmt.Errorf("profile %q: %w", name, err)
		}
	}
	return nil
}

func normalizeProfiles(in map[string]ProfileConfig) map[string]ProfileConfig {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]ProfileConfig, len(in))
	for name, p := range in {
		out[strings.TrimSpace(name)] = normalizeProfile(p)
	}
	return out
}

func normalizeProfile(p ProfileConfig) ProfileConfig {
	p.DirectURL = strings.TrimSpace(p.DirectURL)
	p.RegistryAddress = strings.TrimSpace(p.RegistryAddress)
	p.RegistryProtocol = strings.TrimSpace(p.RegistryProtocol)
	p.Protocol = strings.TrimSpace(p.Protocol)
	p.Serialization = strings.TrimSpace(p.Serialization)
	p.UniqueID = strings.TrimSpace(p.UniqueID)
	return p
}

func normalizeProfileInvocationProps(profiles map[string]ProfileConfig) (map[string]ProfileConfig, error) {
	if len(profiles) == 0 {
		return profiles, nil
	}
	out := make(map[string]ProfileConfig, len(profiles))
	for name, p := range profiles {
		props, err := invocationprops.NormalizeInput(p.InvocationProperties)
		if err != nil {
			return nil, fmt.Errorf("profile %q: %w", strings.TrimSpace(name), err)
		}
		p.InvocationProperties = props
		out[name] = p
	}
	return out, nil
}

func parse(body []byte) (Config, bool, error) {
	var raw fileConfig
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&raw); err != nil {
		return Config{}, false, err
	}
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		if err == nil {
			err = fmt.Errorf("contains multiple JSON values")
		}
		return Config{}, false, err
	}

	if err := validateProfileKeys(raw.Profiles); err != nil {
		return Config{}, false, err
	}
	cfg := configFromFile(raw)
	props, err := invocationprops.NormalizeInput(cfg.InvocationProperties)
	if err != nil {
		return Config{}, false, err
	}
	cfg.InvocationProperties = props
	if cfg.Profiles, err = normalizeProfileInvocationProps(cfg.Profiles); err != nil {
		return Config{}, false, err
	}
	if err := Validate(cfg); err != nil {
		return Config{}, false, err
	}
	return cfg, raw.AllowedServices != nil, nil
}

func configFromFile(raw fileConfig) Config {
	cfg := Config{
		DirectURL:            strings.TrimSpace(raw.DirectURL),
		RegistryAddress:      strings.TrimSpace(raw.RegistryAddress),
		RegistryProtocol:     strings.TrimSpace(raw.RegistryProtocol),
		Protocol:             strings.TrimSpace(raw.Protocol),
		Serialization:        strings.TrimSpace(raw.Serialization),
		UniqueID:             strings.TrimSpace(raw.UniqueID),
		TimeoutMS:            raw.TimeoutMS,
		ConnectTimeoutMS:     raw.ConnectTimeoutMS,
		InvocationProperties: raw.InvocationProperties,
		DefaultProfile:       strings.TrimSpace(raw.DefaultProfile),
		Profiles:             raw.Profiles,
	}
	if raw.AllowedServices != nil {
		cfg.AllowedServices = normalizeStringList(*raw.AllowedServices)
	}
	return Normalize(cfg)
}

func normalizeStringList(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			out = append(out, value)
		}
	}
	return out
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

// WriteMerged writes cfg to the kind's path unconditionally — it is the
// caller's responsibility to have merged non-destructively (read-modify-write).
// Unlike Write there is no force gate, because a merge intentionally rewrites
// the whole file from already-merged content. allowedServicesSet preserves an
// explicitly-empty allowlist (see MarshalMerged). Overwrote reports whether a
// file was already present.
func WriteMerged(projectRoot string, kind Kind, cfg Config, allowedServicesSet bool) (WriteResult, error) {
	body, err := MarshalMerged(cfg, allowedServicesSet)
	if err != nil {
		return WriteResult{}, err
	}
	path := ConfigPath(projectRoot, kind)
	exists, err := fileExists(path)
	if err != nil {
		return WriteResult{}, err
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
