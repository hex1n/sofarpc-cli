package mcp

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/hex1n/sofarpc-cli/internal/core/contract"
	"github.com/hex1n/sofarpc-cli/internal/core/projectbootstrap"
	"github.com/hex1n/sofarpc-cli/internal/core/projectconfig"
	"github.com/hex1n/sofarpc-cli/internal/core/target"
	"github.com/hex1n/sofarpc-cli/internal/core/workspace"
	"github.com/hex1n/sofarpc-cli/internal/errcode"
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

func registerInitProject(server *sdkmcp.Server, opts Options, holder *contractHolder) {
	sources := opts.TargetSources
	sessions := opts.Sessions
	sdkmcp.AddTool(server, &sdkmcp.Tool{
		Name:        "sofarpc_init_project",
		Title:       "Initialize SOFARPC Project",
		Description: "Initialize a Java project's .sofarpc config. It can discover facade services from source contracts, write allowedServices, and optionally persist an explicit direct or registry target.",
		Annotations: localWriteAnnotations("Initialize SOFARPC Project"),
		InputSchema: initProjectInputSchema(),
	}, func(ctx context.Context, req *sdkmcp.CallToolRequest, in InitProjectInput) (*sdkmcp.CallToolResult, InitProjectOutput, error) {
		notifyToolProgress(ctx, req, 0, 4, "resolving project scope")
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
		notifyToolProgress(ctx, req, 1, 4, "building project config")
		toolCtx := attachContractContext(scope, holder)
		cfg, discovery, err := buildInitProjectConfig(in, toolCtx.Contract)
		if err != nil {
			out := InitProjectOutput{ProjectRoot: scope.ProjectRoot, ProjectResolution: projectResolution, ConfigFile: string(kind), Error: errcode.New(errcode.ArgsInvalid, "init-project", err.Error())}
			return initProjectResult(out), out, nil
		}
		out := InitProjectOutput{
			ProjectRoot:       scope.ProjectRoot,
			ConfigFile:        string(kind),
			ConfigPath:        projectconfig.ConfigPath(scope.ProjectRoot, kind),
			DryRun:            in.DryRun,
			ProjectConfig:     cfg,
			ProjectResolution: projectResolution,
			Discovery:         discovery,
			NextSteps:         initProjectNextSteps(cfg, kind),
		}
		notifyToolProgress(ctx, req, 2, 4, "validating project bootstrap")
		notifyToolProgress(ctx, req, 3, 4, "applying project bootstrap")
		bootstrapResult, err := projectbootstrap.Run(projectbootstrap.Input{
			ProjectRoot:            scope.ProjectRoot,
			Kind:                   kind,
			Config:                 cfg,
			Force:                  in.Force,
			DryRun:                 in.DryRun,
			RequireConfigFields:    true,
			RequireAllowedServices: true,
		})
		applyInitProjectBootstrapResult(&out, bootstrapResult)
		if err != nil {
			out.Error = initProjectBootstrapError(err, scope.ProjectRoot)
			return initProjectResult(out), out, nil
		}
		out.Ok = true
		notifyToolProgress(ctx, req, 4, 4, "project initialization complete")
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
	scope, err = resolveWorkspaceScope(base, in.Cwd, in.Project)
	if err != nil {
		return toolScope{}, nil, err
	}
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
		discovery = initProjectDiscoveryFromServices(snapshot, contract.DiscoverServiceInterfaces(snapshot.store, contract.ServiceDiscoveryOptions{
			Suffixes:      in.ServiceNameSuffixes,
			IndexedSource: "sourcecontract",
			StoreSource:   "contract-store",
		}))
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

func initProjectDiscoveryFromServices(snapshot contractSnapshot, services contract.ServiceDiscovery) InitProjectDiscovery {
	return InitProjectDiscovery{
		Source:            services.Source,
		SelectedServices:  services.SelectedServices,
		CandidateServices: services.CandidateServices,
		Suffixes:          services.Suffixes,
		Contract:          buildContractBanner(snapshot.store, snapshot.loadError, snapshot.root),
	}
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

func applyInitProjectBootstrapResult(out *InitProjectOutput, result projectbootstrap.Result) {
	if strings.TrimSpace(result.ConfigPath) != "" {
		out.ConfigPath = result.ConfigPath
	}
	out.Existing = result.Existing
	out.Wrote = result.Wrote
	out.Overwrote = result.Overwrote
	if result.Gitignore != nil {
		out.Gitignore = &InitProjectGitignore{
			Path:        result.Gitignore.Path,
			Entry:       result.Gitignore.Entry,
			Changed:     result.Gitignore.Changed,
			WouldChange: result.Gitignore.WouldChange,
		}
	}
}

func initProjectBootstrapError(err error, projectRoot string) *errcode.Error {
	switch {
	case errors.Is(err, projectbootstrap.ErrNoConfigFields):
		return errcode.New(errcode.ArgsInvalid, "init-project",
			"no project config fields resolved; pass a target, explicit services, or use serviceNameSuffixes that match facade interfaces").
			WithHint("sofarpc_init_project", map[string]any{"project": projectRoot, "dryRun": true},
				"preview project initialization after providing target or service discovery inputs")
	case errors.Is(err, projectbootstrap.ErrAllowedServicesMissing):
		return errcode.New(errcode.ArgsInvalid, "init-project",
			"allowedServices is required for project initialization; pass services, adjust serviceNameSuffixes, or set allowAllServices=true intentionally").
			WithHint("sofarpc_init_project", map[string]any{"project": projectRoot, "dryRun": true, "serviceNameSuffixes": []string{"Facade"}},
				"preview service allowlist discovery before writing project config")
	default:
		var existing projectbootstrap.ExistingConfigError
		if errors.As(err, &existing) {
			return errcode.New(errcode.ArgsInvalid, "init-project",
				fmt.Sprintf("%s already exists; pass force=true to overwrite", existing.Path)).
				WithHint("sofarpc_target", map[string]any{"project": projectRoot, "explain": true},
					"inspect existing project config before overwriting it")
		}
		return errcode.New(errcode.ArgsInvalid, "init-project", err.Error())
	}
}

func initProjectResult(out InitProjectOutput) *sdkmcp.CallToolResult {
	return toolResult(out, summarizeInitProject(out), out.Error != nil)
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
