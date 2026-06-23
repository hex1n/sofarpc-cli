// Package projectbootstrap owns the shared .sofarpc project-initialization
// workflow used by both CLI project setup and MCP init_project.
package projectbootstrap

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/hex1n/sofarpc-cli/internal/core/projectconfig"
)

var (
	ErrNoConfigFields         = errors.New("no project config fields resolved")
	ErrAllowedServicesMissing = errors.New("allowedServices is required")
	ErrProfileNameRequired    = errors.New("profile name is required")
	ErrProfileNoFields        = errors.New("profile has no target or invocation fields")
)

type ExistingConfigError struct {
	Path string
}

func (e ExistingConfigError) Error() string {
	return fmt.Sprintf("%s already exists", e.Path)
}

// ExistingProfileError signals that a same-named profile already exists in the
// target file and force was not set. Adding a new profile is never destructive,
// so only an overwrite trips this.
type ExistingProfileError struct {
	Path string
	Name string
}

func (e ExistingProfileError) Error() string {
	return fmt.Sprintf("profile %q already exists in %s", e.Name, e.Path)
}

// ProfileNotDefinedError signals that `profile use` (default selection) named a
// profile defined in neither config file. It never falls through to base.
type ProfileNotDefinedError struct {
	Name      string
	Available []string
}

func (e ProfileNotDefinedError) Error() string {
	if len(e.Available) == 0 {
		return fmt.Sprintf("profile %q is not defined; no profiles are defined", e.Name)
	}
	return fmt.Sprintf("profile %q is not defined; available profiles: %s", e.Name, strings.Join(e.Available, ", "))
}

type Input struct {
	ProjectRoot            string
	Kind                   projectconfig.Kind
	Config                 projectconfig.Config
	Force                  bool
	DryRun                 bool
	RequireConfigFields    bool
	RequireAllowedServices bool
}

type Result struct {
	ProjectRoot string
	Kind        projectconfig.Kind
	Config      projectconfig.Config
	ConfigPath  string
	ConfigBody  []byte
	DryRun      bool
	Existing    bool
	Wrote       bool
	Overwrote   bool
	Gitignore   *GitignoreResult
}

type GitignoreResult struct {
	Path        string
	Entry       string
	Changed     bool
	WouldChange bool
}

func Run(in Input) (Result, error) {
	cfg := projectconfig.Normalize(in.Config)
	result := Result{
		ProjectRoot: strings.TrimSpace(in.ProjectRoot),
		Kind:        in.Kind,
		Config:      cfg,
		DryRun:      in.DryRun,
	}
	result.ConfigPath = projectconfig.ConfigPath(result.ProjectRoot, in.Kind)

	exists, err := projectconfig.Existing(result.ConfigPath)
	if err != nil {
		return result, err
	}
	result.Existing = exists

	if in.RequireConfigFields && !HasConfigFields(cfg) {
		return result, ErrNoConfigFields
	}
	if in.RequireAllowedServices && len(cfg.AllowedServices) == 0 {
		return result, ErrAllowedServicesMissing
	}

	body, err := projectconfig.Marshal(cfg)
	if err != nil {
		return result, err
	}
	result.ConfigBody = body

	if exists && !in.Force {
		return result, ExistingConfigError{Path: result.ConfigPath}
	}

	if in.Kind == projectconfig.KindLocal {
		gitignore, err := prepareLocalGitignore(result.ProjectRoot, in.DryRun)
		if err != nil {
			return result, err
		}
		result.Gitignore = gitignore
	}

	if in.DryRun {
		return result, nil
	}

	written, err := projectconfig.Write(result.ProjectRoot, in.Kind, cfg, in.Force)
	if err != nil {
		return result, err
	}
	result.Wrote = true
	result.ConfigPath = written.Path
	result.ConfigBody = written.Body
	result.Overwrote = written.Overwrote
	return result, nil
}

func HasConfigFields(cfg projectconfig.Config) bool {
	return strings.TrimSpace(cfg.DirectURL) != "" ||
		strings.TrimSpace(cfg.RegistryAddress) != "" ||
		strings.TrimSpace(cfg.RegistryProtocol) != "" ||
		strings.TrimSpace(cfg.Protocol) != "" ||
		strings.TrimSpace(cfg.Serialization) != "" ||
		strings.TrimSpace(cfg.UniqueID) != "" ||
		cfg.TimeoutMS > 0 ||
		cfg.ConnectTimeoutMS > 0 ||
		len(cfg.AllowedServices) > 0 ||
		len(cfg.InvocationProperties) > 0
}

// ProfileInput describes a merge-into-existing write of a single named Target
// Profile. Force is required only to overwrite a pre-existing same-named
// profile; adding a new profile (even to an existing file) is non-destructive.
type ProfileInput struct {
	ProjectRoot string
	Kind        projectconfig.Kind
	Name        string
	Profile     projectconfig.ProfileConfig
	SetDefault  bool
	Force       bool
	DryRun      bool
}

type ProfileResult struct {
	ProjectRoot    string
	Kind           projectconfig.Kind
	Name           string
	Config         projectconfig.Config
	ConfigPath     string
	ConfigBody     []byte
	DryRun         bool
	FileExisted    bool
	ProfileExisted bool
	Wrote          bool
	SetDefault     bool
	Gitignore      *GitignoreResult
}

// WriteProfile reads the project config file of the given kind, sets
// profiles[Name]=Profile (optionally DefaultProfile=Name), and rewrites the
// file preserving every other field — the base target settings, other
// profiles, and an explicit allowedServices. An unparseable existing file is an
// error rather than being clobbered.
func WriteProfile(in ProfileInput) (ProfileResult, error) {
	name := strings.TrimSpace(in.Name)
	result := ProfileResult{
		ProjectRoot: strings.TrimSpace(in.ProjectRoot),
		Kind:        in.Kind,
		Name:        name,
		DryRun:      in.DryRun,
		SetDefault:  in.SetDefault,
	}
	result.ConfigPath = projectconfig.ConfigPath(result.ProjectRoot, in.Kind)
	if name == "" {
		return result, ErrProfileNameRequired
	}
	if !projectconfig.ProfileHasFields(in.Profile) {
		return result, ErrProfileNoFields
	}

	existing, err := projectconfig.Read(result.ProjectRoot, in.Kind)
	if err != nil {
		return result, err
	}
	result.FileExisted = existing.Exists
	result.ProfileExisted = projectconfig.HasProfile(existing.Config, name)
	if result.ProfileExisted && !in.Force {
		return result, ExistingProfileError{Path: result.ConfigPath, Name: name}
	}

	cfg := projectconfig.SetProfile(existing.Config, name, in.Profile)
	if in.SetDefault {
		cfg = projectconfig.SetDefaultProfile(cfg, name)
	}
	result.Config = cfg
	body, err := projectconfig.MarshalMerged(cfg, existing.AllowedServicesSet)
	if err != nil {
		return result, err
	}
	result.ConfigBody = body

	if in.Kind == projectconfig.KindLocal {
		gitignore, err := prepareLocalGitignore(result.ProjectRoot, in.DryRun)
		if err != nil {
			return result, err
		}
		result.Gitignore = gitignore
	}
	if in.DryRun {
		return result, nil
	}

	written, err := projectconfig.WriteMerged(result.ProjectRoot, in.Kind, cfg, existing.AllowedServicesSet)
	if err != nil {
		return result, err
	}
	result.Wrote = true
	result.ConfigPath = written.Path
	result.ConfigBody = written.Body
	return result, nil
}

// UseProfileInput describes the `profile use` operation: set the persisted
// DefaultProfile to an already-defined profile. The default always lands in
// config.local.json (a personal selection), and the named profile must be
// defined in either file or it errors rather than persisting an undefined
// default (which would only resurface as a hard error at resolve time).
type UseProfileInput struct {
	ProjectRoot string
	Name        string
	DryRun      bool
}

type UseProfileResult struct {
	ProjectRoot string
	Name        string
	Config      projectconfig.Config
	ConfigPath  string
	ConfigBody  []byte
	DryRun      bool
	Wrote       bool
	Available   []string
	Gitignore   *GitignoreResult
}

// UseProfile writes DefaultProfile=Name into config.local.json after verifying
// Name is defined across the shared and local files.
func UseProfile(in UseProfileInput) (UseProfileResult, error) {
	name := strings.TrimSpace(in.Name)
	result := UseProfileResult{
		ProjectRoot: strings.TrimSpace(in.ProjectRoot),
		Name:        name,
		DryRun:      in.DryRun,
	}
	result.ConfigPath = projectconfig.ConfigPath(result.ProjectRoot, projectconfig.KindLocal)
	if name == "" {
		return result, ErrProfileNameRequired
	}

	available, defined, err := availableProfiles(result.ProjectRoot)
	if err != nil {
		return result, err
	}
	result.Available = available
	if _, ok := defined[name]; !ok {
		return result, ProfileNotDefinedError{Name: name, Available: available}
	}

	local, err := projectconfig.Read(result.ProjectRoot, projectconfig.KindLocal)
	if err != nil {
		return result, err
	}
	cfg := projectconfig.SetDefaultProfile(local.Config, name)
	result.Config = cfg
	body, err := projectconfig.MarshalMerged(cfg, local.AllowedServicesSet)
	if err != nil {
		return result, err
	}
	result.ConfigBody = body

	gitignore, err := prepareLocalGitignore(result.ProjectRoot, in.DryRun)
	if err != nil {
		return result, err
	}
	result.Gitignore = gitignore
	if in.DryRun {
		return result, nil
	}

	written, err := projectconfig.WriteMerged(result.ProjectRoot, projectconfig.KindLocal, cfg, local.AllowedServicesSet)
	if err != nil {
		return result, err
	}
	result.Wrote = true
	result.ConfigPath = written.Path
	result.ConfigBody = written.Body
	return result, nil
}

// availableProfiles returns the sorted union of profile names defined across
// the shared and local config files, plus a set for membership tests.
func availableProfiles(projectRoot string) ([]string, map[string]struct{}, error) {
	defined := map[string]struct{}{}
	for _, kind := range []projectconfig.Kind{projectconfig.KindShared, projectconfig.KindLocal} {
		r, err := projectconfig.Read(projectRoot, kind)
		if err != nil {
			return nil, nil, err
		}
		for name := range r.Config.Profiles {
			defined[name] = struct{}{}
		}
	}
	out := make([]string, 0, len(defined))
	for name := range defined {
		out = append(out, name)
	}
	sort.Strings(out)
	return out, defined, nil
}

func prepareLocalGitignore(projectRoot string, dryRun bool) (*GitignoreResult, error) {
	if dryRun {
		status, err := projectconfig.LocalConfigIgnoreStatus(projectRoot)
		if err != nil {
			return nil, err
		}
		return &GitignoreResult{
			Path:        status.Path,
			Entry:       status.Entry,
			WouldChange: status.Changed,
		}, nil
	}
	status, err := projectconfig.EnsureLocalConfigIgnored(projectRoot)
	if err != nil {
		return nil, err
	}
	return &GitignoreResult{
		Path:    status.Path,
		Entry:   status.Entry,
		Changed: status.Changed,
	}, nil
}
