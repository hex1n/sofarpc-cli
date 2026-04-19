package facade

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/hex1n/sofarpc-cli/internal/facadeconfig"
	"github.com/hex1n/sofarpc-cli/internal/facadeindex"
	"github.com/hex1n/sofarpc-cli/internal/facadereplay"
	"github.com/hex1n/sofarpc-cli/internal/facadeschema"
	"github.com/hex1n/sofarpc-cli/internal/facadesemantic"
)

type Deps struct {
	ResolveProjectRoot   func(cwd, project string) (string, error)
	FindSkillDir         func() (string, error)
	InspectState         func(string) facadeconfig.StatePaths
	LoadConfig           func(string, bool) (facadeconfig.Config, error)
	LoadServiceSummary   func(string) (facadeindex.IndexSummary, error)
	IterSourceRoots      func(facadeconfig.Config, string) []string
	LoadSemanticRegistry func(string, []string, []string) (facadesemantic.Registry, error)
	BuildMethodSchema    func(facadesemantic.Registry, string, string, []string, []string) (facadeschema.MethodSchemaEnvelope, error)
	DetectConfig         func(string, bool, io.Writer, io.Writer) error
	RefreshIndex         func(string, facadeconfig.Config, io.Writer, io.Writer) error
	ReplayCalls          func(string, facadereplay.ReplayOptions, io.Writer, io.Writer) error
}

type StatusRequest struct {
	Cwd     string
	Project string
}

type StatusResult struct {
	SkillDir      string
	SkillDirError error
	ProjectRoot   string
	State         facadeconfig.StatePaths
	Config        *facadeconfig.Config
	ConfigError   error
}

type ServicesRequest struct {
	Cwd     string
	Project string
	Filter  string
}

type ServicesResult struct {
	ProjectRoot string
	Summary     facadeindex.IndexSummary
}

type SchemaRequest struct {
	Cwd        string
	Project    string
	Service    string
	Method     string
	ParamTypes []string
}

type SchemaResult struct {
	ProjectRoot string
	Schema      facadeschema.MethodSchemaEnvelope
}

type DiscoverRequest struct {
	Cwd     string
	Project string
	Write   bool
	Stdout  io.Writer
	Stderr  io.Writer
}

type DiscoverResult struct {
	ProjectRoot string
}

type IndexRequest struct {
	Cwd     string
	Project string
	Stdout  io.Writer
	Stderr  io.Writer
}

type IndexResult struct {
	ProjectRoot string
}

type ReplayRequest struct {
	Cwd         string
	Project     string
	Filter      string
	OnlyNames   []string
	ContextName string
	SofaRPCBin  string
	DryRun      bool
	Save        bool
	Stdout      io.Writer
	Stderr      io.Writer
}

type ReplayResult struct {
	ProjectRoot string
}

func (d Deps) Status(req StatusRequest) (StatusResult, error) {
	projectRoot, err := d.ResolveProjectRoot(req.Cwd, req.Project)
	if err != nil {
		return StatusResult{}, err
	}
	skillDir, skillErr := d.FindSkillDir()
	state := d.InspectState(projectRoot)
	cfg, cfgErr := d.LoadConfig(projectRoot, true)
	result := StatusResult{
		SkillDir:      skillDir,
		SkillDirError: skillErr,
		ProjectRoot:   projectRoot,
		State:         state,
		ConfigError:   cfgErr,
	}
	if cfgErr == nil {
		result.Config = &cfg
	}
	return result, nil
}

func (d Deps) Services(req ServicesRequest) (ServicesResult, error) {
	projectRoot, err := d.ResolveProjectRoot(req.Cwd, req.Project)
	if err != nil {
		return ServicesResult{}, err
	}
	summary, err := d.LoadServiceSummary(projectRoot)
	if err != nil {
		return ServicesResult{}, err
	}
	return ServicesResult{
		ProjectRoot: projectRoot,
		Summary:     filterServiceSummary(summary, req.Filter),
	}, nil
}

func (d Deps) Schema(req SchemaRequest) (SchemaResult, error) {
	projectRoot, err := d.ResolveProjectRoot(req.Cwd, req.Project)
	if err != nil {
		return SchemaResult{}, err
	}
	cfg, err := d.LoadConfig(projectRoot, false)
	if err != nil {
		return SchemaResult{}, err
	}
	sourceRoots := d.IterSourceRoots(cfg, projectRoot)
	if len(sourceRoots) == 0 {
		return SchemaResult{}, fmt.Errorf("config has no facade source roots")
	}
	registry, err := d.LoadSemanticRegistry(projectRoot, sourceRoots, cfg.RequiredMarkers)
	if err != nil {
		return SchemaResult{}, err
	}
	schema, err := d.BuildMethodSchema(registry, req.Service, req.Method, req.ParamTypes, cfg.RequiredMarkers)
	if err != nil {
		return SchemaResult{}, err
	}
	return SchemaResult{
		ProjectRoot: projectRoot,
		Schema:      schema,
	}, nil
}

func (d Deps) Discover(req DiscoverRequest) (DiscoverResult, error) {
	projectRoot, err := d.ResolveProjectRoot(req.Cwd, req.Project)
	if err != nil {
		return DiscoverResult{}, err
	}
	if err := d.DetectConfig(projectRoot, req.Write, req.Stdout, req.Stderr); err != nil {
		return DiscoverResult{}, err
	}
	return DiscoverResult{ProjectRoot: projectRoot}, nil
}

func (d Deps) Index(req IndexRequest) (IndexResult, error) {
	projectRoot, err := d.ResolveProjectRoot(req.Cwd, req.Project)
	if err != nil {
		return IndexResult{}, err
	}
	cfg, err := d.LoadConfig(projectRoot, false)
	if err != nil {
		return IndexResult{}, err
	}
	if err := d.RefreshIndex(projectRoot, cfg, req.Stdout, req.Stderr); err != nil {
		return IndexResult{}, err
	}
	return IndexResult{ProjectRoot: projectRoot}, nil
}

func (d Deps) Replay(req ReplayRequest) (ReplayResult, error) {
	projectRoot, err := d.ResolveProjectRoot(req.Cwd, req.Project)
	if err != nil {
		return ReplayResult{}, err
	}
	if err := d.ReplayCalls(projectRoot, facadereplay.ReplayOptions{
		Filter:          req.Filter,
		OnlyNames:       req.OnlyNames,
		ContextOverride: req.ContextName,
		DryRun:          req.DryRun,
		Save:            req.Save,
		SofaRPCBin:      req.SofaRPCBin,
	}, req.Stdout, req.Stderr); err != nil {
		return ReplayResult{}, err
	}
	return ReplayResult{ProjectRoot: projectRoot}, nil
}

func filterServiceSummary(summary facadeindex.IndexSummary, filter string) facadeindex.IndexSummary {
	needle := strings.TrimSpace(strings.ToLower(filter))
	if needle == "" {
		return summary
	}
	filtered := make([]facadeindex.IndexSummaryService, 0, len(summary.Services))
	for _, service := range summary.Services {
		if strings.Contains(strings.ToLower(service.Service), needle) {
			filtered = append(filtered, service)
			continue
		}
		for _, method := range service.Methods {
			if strings.Contains(strings.ToLower(method), needle) {
				filtered = append(filtered, service)
				break
			}
		}
	}
	summary.Services = filtered
	return summary
}

func ConfigPrintableError(err error) bool {
	return err != nil && !os.IsNotExist(err) && !strings.Contains(err.Error(), "no config found")
}
