package mcp

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/hex1n/sofarpc-cli/internal/core/projectconfig"
	"github.com/hex1n/sofarpc-cli/internal/core/target"
	"github.com/hex1n/sofarpc-cli/internal/core/workspace"
	"github.com/hex1n/sofarpc-cli/internal/errcode"
	"github.com/hex1n/sofarpc-cli/internal/javamodel"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

type InitProjectInput struct {
	Cwd                 string   `json:"cwd,omitempty"`
	Project             string   `json:"project,omitempty"`
	SessionID           string   `json:"sessionId,omitempty"`
	ConfigFile          string   `json:"config,omitempty"`
	Force               bool     `json:"force,omitempty"`
	DryRun              bool     `json:"dryRun,omitempty"`
	Services            []string `json:"services,omitempty"`
	AllowAllServices    bool     `json:"allowAllServices,omitempty"`
	ServiceNameSuffixes []string `json:"serviceNameSuffixes,omitempty"`
	DirectURL           string   `json:"directUrl,omitempty"`
	RegistryAddress     string   `json:"registryAddress,omitempty"`
	RegistryProtocol    string   `json:"registryProtocol,omitempty"`
	Protocol            string   `json:"protocol,omitempty"`
	Serialization       string   `json:"serialization,omitempty"`
	UniqueID            string   `json:"uniqueId,omitempty"`
	TimeoutMS           int      `json:"timeoutMs,omitempty"`
	ConnectTimeoutMS    int      `json:"connectTimeoutMs,omitempty"`
}

type InitProjectOutput struct {
	Ok                bool                            `json:"ok"`
	ProjectRoot       string                          `json:"projectRoot,omitempty"`
	ConfigFile        string                          `json:"configFile,omitempty"`
	ConfigPath        string                          `json:"configPath,omitempty"`
	DryRun            bool                            `json:"dryRun,omitempty"`
	Wrote             bool                            `json:"wrote,omitempty"`
	Existing          bool                            `json:"existing,omitempty"`
	Overwrote         bool                            `json:"overwrote,omitempty"`
	ProjectConfig     projectconfig.Config            `json:"projectConfig,omitempty"`
	ProjectResolution *workspace.JavaProjectDiscovery `json:"projectResolution,omitempty"`
	Discovery         InitProjectDiscovery            `json:"discovery,omitempty"`
	Gitignore         *InitProjectGitignore           `json:"gitignore,omitempty"`
	NextSteps         []string                        `json:"nextSteps,omitempty"`
	Error             *errcode.Error                  `json:"error,omitempty"`
}

type InitProjectDiscovery struct {
	Source            string         `json:"source,omitempty"`
	SelectedServices  []string       `json:"selectedServices,omitempty"`
	CandidateServices []string       `json:"candidateServices,omitempty"`
	Suffixes          []string       `json:"suffixes,omitempty"`
	Contract          ContractBanner `json:"contract,omitempty"`
}

type InitProjectGitignore struct {
	Path        string `json:"path,omitempty"`
	Entry       string `json:"entry,omitempty"`
	Changed     bool   `json:"changed,omitempty"`
	WouldChange bool   `json:"wouldChange,omitempty"`
}

type indexedClassStore interface {
	IndexedClasses() []string
}

func registerInitProject(server *sdkmcp.Server, opts Options, holder *contractHolder) {
	sources := opts.TargetSources
	sessions := opts.Sessions
	sdkmcp.AddTool(server, &sdkmcp.Tool{
		Name:        "sofarpc_init_project",
		Description: "Initialize a Java project's .sofarpc config. It can discover facade services from source contracts, write allowedServices, and optionally persist an explicit direct or registry target.",
		InputSchema: initProjectInputSchema(),
	}, func(_ context.Context, _ *sdkmcp.CallToolRequest, in InitProjectInput) (*sdkmcp.CallToolResult, InitProjectOutput, error) {
		scope, projectResolution, err := resolveInitProjectScope(sources, sessions, in)
		if err != nil {
			out := InitProjectOutput{ProjectResolution: projectResolution, Error: errcode.New(errcode.ArgsInvalid, "init-project", err.Error())}
			return initProjectResult(out), out, nil
		}
		if strings.TrimSpace(scope.ProjectRoot) == "" {
			out := InitProjectOutput{
				DryRun:            in.DryRun,
				ProjectResolution: projectResolution,
				NextSteps:         initProjectDiscoveryNextSteps(projectResolution),
			}
			if in.DryRun {
				out.Ok = true
				return initProjectResult(out), out, nil
			}
			out.Error = errcode.New(errcode.ArgsInvalid, "init-project",
				"project scope must be explicit before writing; pass project, cwd, or sessionId").
				WithHint("sofarpc_init_project", initProjectDiscoveryHint(projectResolution),
					"retry with an explicit project root after inspecting projectResolution")
			return initProjectResult(out), out, nil
		}
		kind, err := projectconfig.ParseKind(in.ConfigFile)
		if err != nil {
			out := InitProjectOutput{ProjectRoot: scope.ProjectRoot, ProjectResolution: projectResolution, Error: errcode.New(errcode.ArgsInvalid, "init-project", err.Error())}
			return initProjectResult(out), out, nil
		}
		cfg, discovery, err := buildInitProjectConfig(in, holder.ForProject(scope.ProjectRoot))
		if err != nil {
			out := InitProjectOutput{ProjectRoot: scope.ProjectRoot, ProjectResolution: projectResolution, ConfigFile: string(kind), Error: errcode.New(errcode.ArgsInvalid, "init-project", err.Error())}
			return initProjectResult(out), out, nil
		}
		path := projectconfig.ConfigPath(scope.ProjectRoot, kind)
		exists, err := projectconfig.Existing(path)
		if err != nil {
			out := InitProjectOutput{ProjectRoot: scope.ProjectRoot, ProjectResolution: projectResolution, ConfigFile: string(kind), ConfigPath: path, Error: errcode.New(errcode.ArgsInvalid, "init-project", err.Error())}
			return initProjectResult(out), out, nil
		}
		out := InitProjectOutput{
			ProjectRoot:       scope.ProjectRoot,
			ConfigFile:        string(kind),
			ConfigPath:        path,
			DryRun:            in.DryRun,
			Existing:          exists,
			ProjectConfig:     cfg,
			ProjectResolution: projectResolution,
			Discovery:         discovery,
			NextSteps:         initProjectNextSteps(cfg, kind),
		}
		if !hasProjectConfigFields(cfg) {
			out.Error = errcode.New(errcode.ArgsInvalid, "init-project",
				"no project config fields resolved; pass a target, explicit services, or use serviceNameSuffixes that match facade interfaces").
				WithHint("sofarpc_init_project", map[string]any{"project": scope.ProjectRoot, "dryRun": true},
					"preview project initialization after providing target or service discovery inputs")
			return initProjectResult(out), out, nil
		}
		if len(cfg.AllowedServices) == 0 {
			out.Error = errcode.New(errcode.ArgsInvalid, "init-project",
				"allowedServices is required for project initialization; pass services, adjust serviceNameSuffixes, or set allowAllServices=true intentionally").
				WithHint("sofarpc_init_project", map[string]any{"project": scope.ProjectRoot, "dryRun": true, "serviceNameSuffixes": []string{"Facade"}},
					"preview service allowlist discovery before writing project config")
			return initProjectResult(out), out, nil
		}
		if exists && !in.Force {
			out.Error = errcode.New(errcode.ArgsInvalid, "init-project",
				fmt.Sprintf("%s already exists; pass force=true to overwrite", path)).
				WithHint("sofarpc_target", map[string]any{"project": scope.ProjectRoot, "explain": true},
					"inspect existing project config before overwriting it")
			return initProjectResult(out), out, nil
		}
		if kind == projectconfig.KindLocal {
			status, err := initProjectGitignore(scope.ProjectRoot, in.DryRun)
			if err != nil {
				out.Error = errcode.New(errcode.ArgsInvalid, "init-project", err.Error())
				return initProjectResult(out), out, nil
			}
			out.Gitignore = status
		}
		if in.DryRun {
			out.Ok = true
			return initProjectResult(out), out, nil
		}
		written, err := projectconfig.Write(scope.ProjectRoot, kind, cfg, in.Force)
		if err != nil {
			out.Error = errcode.New(errcode.ArgsInvalid, "init-project", err.Error())
			return initProjectResult(out), out, nil
		}
		out.Ok = true
		out.Wrote = true
		out.ConfigPath = written.Path
		out.Overwrote = written.Overwrote
		return initProjectResult(out), out, nil
	})
}

func resolveInitProjectScope(base target.Sources, sessions *SessionStore, in InitProjectInput) (toolScope, *workspace.JavaProjectDiscovery, error) {
	if strings.TrimSpace(in.SessionID) == "" && strings.TrimSpace(in.Cwd) == "" && strings.TrimSpace(in.Project) == "" {
		discovery, err := workspace.DiscoverJavaProject("")
		if err != nil {
			return toolScope{}, nil, fmt.Errorf("auto-discover Java project: %w", err)
		}
		return toolScope{}, &discovery, nil
	}
	scope, err := resolveToolScope(base, sessions, in.SessionID, in.Cwd, in.Project)
	if err != nil {
		return toolScope{}, nil, err
	}
	resolution := initProjectExplicitResolution(scope)
	if strings.TrimSpace(scope.ProjectRoot) != "" {
		return scope, resolution, nil
	}
	ws, err := workspace.Resolve(workspace.Input{Cwd: in.Cwd, Project: in.Project})
	if err != nil {
		return toolScope{}, nil, fmt.Errorf("resolve workspace: %w", err)
	}
	scope = toolScope{ProjectRoot: ws.ProjectRoot, Sources: ws.Sources(base.Env), Source: "project"}
	return scope, initProjectExplicitResolution(scope), nil
}

func initProjectExplicitResolution(scope toolScope) *workspace.JavaProjectDiscovery {
	root := strings.TrimSpace(scope.ProjectRoot)
	if root == "" {
		return nil
	}
	source := strings.TrimSpace(scope.Source)
	if source == "" {
		source = "input"
	}
	return &workspace.JavaProjectDiscovery{
		Root:       root,
		Source:     source,
		Confidence: "explicit",
		Reason:     "project root supplied by " + source,
	}
}

func initProjectDiscoveryNextSteps(discovery *workspace.JavaProjectDiscovery) []string {
	if discovery == nil {
		return []string{"Call sofarpc_init_project again with project or cwd set to the Java project root."}
	}
	var out []string
	if len(discovery.Candidates) == 0 {
		out = append(out, "Call sofarpc_init_project again with project or cwd set to the Java project root.")
		return out
	}
	for _, candidate := range discovery.Candidates {
		if strings.TrimSpace(candidate.Root) == "" {
			continue
		}
		out = append(out, fmt.Sprintf("Call sofarpc_init_project again with project=%s after confirming this is the intended Java project.", candidate.Root))
	}
	if len(out) == 0 {
		out = append(out, "Call sofarpc_init_project again with project or cwd set to the Java project root.")
	}
	return out
}

func initProjectDiscoveryHint(discovery *workspace.JavaProjectDiscovery) map[string]any {
	args := map[string]any{"dryRun": true}
	if discovery == nil {
		return args
	}
	for _, candidate := range discovery.Candidates {
		if strings.TrimSpace(candidate.Root) != "" {
			args["project"] = candidate.Root
			return args
		}
	}
	return args
}

func buildInitProjectConfig(in InitProjectInput, snapshot contractSnapshot) (projectconfig.Config, InitProjectDiscovery, error) {
	if in.TimeoutMS < 0 {
		return projectconfig.Config{}, InitProjectDiscovery{}, fmt.Errorf("timeoutMs must be non-negative")
	}
	if in.ConnectTimeoutMS < 0 {
		return projectconfig.Config{}, InitProjectDiscovery{}, fmt.Errorf("connectTimeoutMs must be non-negative")
	}

	services := normalizeUniqueStrings(in.Services)
	discovery := InitProjectDiscovery{
		SelectedServices: services,
		Contract:         buildContractBanner(snapshot.store, snapshot.loadError, snapshot.root),
	}
	switch {
	case in.AllowAllServices:
		if len(services) > 0 && !(len(services) == 1 && services[0] == "*") {
			return projectconfig.Config{}, discovery, fmt.Errorf("allowAllServices=true conflicts with explicit services")
		}
		services = []string{"*"}
		discovery.SelectedServices = services
		discovery.Source = "input"
	case len(services) > 0:
		discovery.Source = "input"
	default:
		discovery = discoverInitProjectServices(snapshot, initProjectSuffixes(in.ServiceNameSuffixes))
		services = discovery.SelectedServices
	}

	cfg := projectconfig.Config{
		DirectURL:        strings.TrimSpace(in.DirectURL),
		RegistryAddress:  strings.TrimSpace(in.RegistryAddress),
		RegistryProtocol: strings.TrimSpace(in.RegistryProtocol),
		Protocol:         strings.TrimSpace(in.Protocol),
		Serialization:    strings.TrimSpace(in.Serialization),
		UniqueID:         strings.TrimSpace(in.UniqueID),
		TimeoutMS:        in.TimeoutMS,
		ConnectTimeoutMS: in.ConnectTimeoutMS,
		AllowedServices:  services,
	}
	if err := projectconfig.Validate(cfg); err != nil {
		return projectconfig.Config{}, discovery, err
	}
	return cfg, discovery, nil
}

func discoverInitProjectServices(snapshot contractSnapshot, suffixes []string) InitProjectDiscovery {
	out := InitProjectDiscovery{Suffixes: suffixes}
	store := snapshot.store
	if store == nil {
		out.Contract = buildContractBanner(snapshot.store, snapshot.loadError, snapshot.root)
		return out
	}
	indexed, ok := store.(indexedClassStore)
	if !ok {
		out.Source = "contract-store"
		out.Contract = buildContractBanner(snapshot.store, snapshot.loadError, snapshot.root)
		return out
	}
	out.Source = "sourcecontract"
	var candidates []string
	var selected []string
	for _, fqn := range indexed.IndexedClasses() {
		if !initProjectFQNMatchesSuffix(fqn, suffixes) {
			continue
		}
		cls, ok := store.Class(fqn)
		if !ok || cls.Kind != javamodel.KindInterface || len(cls.Methods) == 0 {
			continue
		}
		candidates = append(candidates, cls.FQN)
		if initProjectServiceMatches(cls, suffixes) {
			selected = append(selected, cls.FQN)
		}
	}
	out.CandidateServices = normalizeUniqueStrings(candidates)
	out.SelectedServices = normalizeUniqueStrings(selected)
	out.Contract = buildContractBanner(snapshot.store, snapshot.loadError, snapshot.root)
	return out
}

func initProjectFQNMatchesSuffix(fqn string, suffixes []string) bool {
	name := strings.TrimSpace(fqn)
	if dot := strings.LastIndexByte(name, '.'); dot >= 0 {
		name = name[dot+1:]
	}
	for _, suffix := range suffixes {
		if suffix == "*" || strings.HasSuffix(name, suffix) {
			return true
		}
	}
	return false
}

func initProjectServiceMatches(cls javamodel.Class, suffixes []string) bool {
	name := strings.TrimSpace(cls.SimpleName)
	if name == "" {
		name = cls.FQN
		if dot := strings.LastIndexByte(name, '.'); dot >= 0 {
			name = name[dot+1:]
		}
	}
	for _, suffix := range suffixes {
		if suffix == "*" || strings.HasSuffix(name, suffix) {
			return true
		}
	}
	return false
}

func initProjectSuffixes(raw []string) []string {
	out := normalizeUniqueStrings(raw)
	if len(out) == 0 {
		return []string{"Facade"}
	}
	return out
}

func hasProjectConfigFields(cfg projectconfig.Config) bool {
	return strings.TrimSpace(cfg.DirectURL) != "" ||
		strings.TrimSpace(cfg.RegistryAddress) != "" ||
		strings.TrimSpace(cfg.RegistryProtocol) != "" ||
		strings.TrimSpace(cfg.Protocol) != "" ||
		strings.TrimSpace(cfg.Serialization) != "" ||
		strings.TrimSpace(cfg.UniqueID) != "" ||
		cfg.TimeoutMS > 0 ||
		cfg.ConnectTimeoutMS > 0 ||
		len(cfg.AllowedServices) > 0
}

func initProjectGitignore(projectRoot string, dryRun bool) (*InitProjectGitignore, error) {
	if dryRun {
		status, err := projectconfig.LocalConfigIgnoreStatus(projectRoot)
		if err != nil {
			return nil, err
		}
		return &InitProjectGitignore{
			Path:        status.Path,
			Entry:       status.Entry,
			WouldChange: status.Changed,
		}, nil
	}
	status, err := projectconfig.EnsureLocalConfigIgnored(projectRoot)
	if err != nil {
		return nil, err
	}
	return &InitProjectGitignore{
		Path:    status.Path,
		Entry:   status.Entry,
		Changed: status.Changed,
	}, nil
}

func initProjectNextSteps(cfg projectconfig.Config, kind projectconfig.Kind) []string {
	var out []string
	if strings.TrimSpace(cfg.DirectURL) == "" && strings.TrimSpace(cfg.RegistryAddress) == "" {
		out = append(out, "Add directUrl or registryAddress before real invoke; allowedServices-only config is still useful for safety policy.")
	}
	if len(cfg.AllowedServices) == 0 {
		out = append(out, "Review candidateServices and pass services or serviceNameSuffixes to set allowedServices.")
	}
	out = append(out, "Call sofarpc_open with this project root and reuse the returned sessionId.")
	if kind == projectconfig.KindShared {
		out = append(out, "Commit .sofarpc/config.json only if its target and allowedServices are valid for the team.")
	}
	return out
}

func initProjectResult(out InitProjectOutput) *sdkmcp.CallToolResult {
	return &sdkmcp.CallToolResult{
		IsError: out.Error != nil,
		Content: []sdkmcp.Content{
			&sdkmcp.TextContent{Text: summarizeInitProject(out)},
		},
	}
}

func summarizeInitProject(out InitProjectOutput) string {
	if out.Error != nil {
		return fmt.Sprintf("init_project failed: %s: %s", out.Error.Code, out.Error.Message)
	}
	if strings.TrimSpace(out.ConfigPath) == "" && out.ProjectResolution != nil {
		return fmt.Sprintf("init_project discovery confidence=%s candidates=%d", out.ProjectResolution.Confidence, len(out.ProjectResolution.Candidates))
	}
	action := "prepared"
	if out.DryRun {
		action = "would write"
	} else if out.Wrote {
		action = "wrote"
	}
	return fmt.Sprintf("%s %s project=%s services=%d", action, out.ConfigPath, out.ProjectRoot, len(out.ProjectConfig.AllowedServices))
}

func normalizeUniqueStrings(values []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}
