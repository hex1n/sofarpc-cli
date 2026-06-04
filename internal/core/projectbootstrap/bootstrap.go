// Package projectbootstrap owns the shared .sofarpc project-initialization
// workflow used by both CLI project setup and MCP init_project.
package projectbootstrap

import (
	"errors"
	"fmt"
	"strings"

	"github.com/hex1n/sofarpc-cli/internal/core/projectconfig"
)

var (
	ErrNoConfigFields         = errors.New("no project config fields resolved")
	ErrAllowedServicesMissing = errors.New("allowedServices is required")
)

type ExistingConfigError struct {
	Path string
}

func (e ExistingConfigError) Error() string {
	return fmt.Sprintf("%s already exists", e.Path)
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
