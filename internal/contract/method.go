package contract

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/hex1n/sofarpc-cli/internal/facadeconfig"
	"github.com/hex1n/sofarpc-cli/internal/facadesemantic"
	"github.com/hex1n/sofarpc-cli/internal/model"
	"github.com/hex1n/sofarpc-cli/internal/projectscan"
)

type ProjectMethod struct {
	ProjectRoot string
	Registry    facadesemantic.Registry
	ServiceInfo facadesemantic.SemanticClassInfo
	MethodInfo  facadesemantic.SemanticMethodInfo
	Schema      model.MethodSchema
}

func ResolveMethodFromProject(projectRoot, service, method string, preferredParamTypes []string, rawArgs json.RawMessage) (ProjectMethod, error) {
	layout, cfg, registry, err := loadProjectRegistry(projectRoot, service)
	if err != nil {
		return ProjectMethod{}, err
	}
	_ = cfg
	serviceInfo, ok := registry[service]
	if !ok {
		return ProjectMethod{}, fmt.Errorf("service %s not found in semantic registry", service)
	}
	if serviceInfo.Kind != "interface" {
		return ProjectMethod{}, fmt.Errorf("service %s is not an interface", service)
	}
	methodInfo, err := selectSemanticMethod(serviceInfo, registry, method, preferredParamTypes, rawArgs)
	if err != nil {
		return ProjectMethod{}, err
	}
	return ProjectMethod{
		ProjectRoot: layout.Root,
		Registry:    registry,
		ServiceInfo: serviceInfo,
		MethodInfo:  methodInfo,
		Schema:      convertMethodSchema(serviceInfo, methodInfo, registry),
	}, nil
}

func loadProjectRegistry(projectRoot, service string) (projectLayout, facadeconfig.Config, facadesemantic.Registry, error) {
	layout, err := discoverProjectLayout(projectRoot)
	if err != nil {
		return projectLayout{}, facadeconfig.Config{}, nil, err
	}
	cfg, err := loadProjectConfigFn(layout.Root, true)
	if err != nil {
		cfg = facadeconfig.DefaultConfig()
	}
	modules := layout.Modules
	if match, err := matchServiceFn(layout.Root, service, layout.Modules); err == nil {
		modules = []projectscan.FacadeModule{match.Module}
	}
	sourceRoots := sourceRootsForModules(layout.Root, modules)
	if len(sourceRoots) == 0 {
		return projectLayout{}, facadeconfig.Config{}, nil, fmt.Errorf("no facade source roots discovered for %s", service)
	}
	registry, err := loadSemanticRegistryFn(layout.Root, sourceRoots, cfg.RequiredMarkers)
	if err != nil {
		return projectLayout{}, facadeconfig.Config{}, nil, err
	}
	return layout, cfg, registry, nil
}

type projectLayout struct {
	Root    string
	Modules []projectscan.FacadeModule
}

func discoverProjectLayout(projectRoot string) (projectLayout, error) {
	projectRoot = strings.TrimSpace(projectRoot)
	if projectRoot == "" {
		return projectLayout{}, fmt.Errorf("project root is required")
	}
	layout, err := discoverProjectFn(projectRoot)
	if err != nil {
		return projectLayout{}, err
	}
	if len(layout.FacadeModules) == 0 {
		return projectLayout{}, fmt.Errorf("no facade modules discovered under %s", layout.Root)
	}
	modules := make([]projectscan.FacadeModule, 0, len(layout.FacadeModules))
	for _, module := range layout.FacadeModules {
		modules = append(modules, module)
	}
	return projectLayout{Root: layout.Root, Modules: modules}, nil
}

func selectSemanticMethod(serviceInfo facadesemantic.SemanticClassInfo, registry facadesemantic.Registry, method string, preferredParamTypes []string, rawArgs json.RawMessage) (facadesemantic.SemanticMethodInfo, error) {
	var matches []facadesemantic.SemanticMethodInfo
	for _, candidate := range serviceInfo.Methods {
		if candidate.Name == method {
			matches = append(matches, candidate)
		}
	}
	if len(matches) == 0 {
		return facadesemantic.SemanticMethodInfo{}, fmt.Errorf("method %s not found on %s", method, serviceInfo.FQN)
	}
	if len(preferredParamTypes) > 0 {
		var narrowed []facadesemantic.SemanticMethodInfo
		for _, candidate := range matches {
			if sameParamTypes(candidateParamTypes(serviceInfo, candidate, registry), preferredParamTypes) {
				narrowed = append(narrowed, candidate)
			}
		}
		if len(narrowed) == 1 {
			return narrowed[0], nil
		}
		if len(narrowed) > 1 {
			matches = narrowed
		}
	}
	if hint := argsArityHint(rawArgs); hint >= 0 {
		var narrowed []facadesemantic.SemanticMethodInfo
		for _, candidate := range matches {
			if len(candidate.Parameters) == hint {
				narrowed = append(narrowed, candidate)
			}
		}
		if len(narrowed) == 1 {
			return narrowed[0], nil
		}
		if len(narrowed) > 1 {
			matches = narrowed
		}
	}
	if len(matches) == 1 {
		return matches[0], nil
	}
	options := make([]string, 0, len(matches))
	for _, candidate := range matches {
		options = append(options, "["+strings.Join(candidateParamTypes(serviceInfo, candidate, registry), ",")+"]")
	}
	sort.Strings(options)
	return facadesemantic.SemanticMethodInfo{}, fmt.Errorf("method %s.%s is overloaded; pass --types to disambiguate: %s",
		serviceInfo.FQN, method, strings.Join(options, " | "))
}

func candidateParamTypes(owner facadesemantic.SemanticClassInfo, method facadesemantic.SemanticMethodInfo, registry facadesemantic.Registry) []string {
	out := make([]string, 0, len(method.Parameters))
	for _, parameter := range method.Parameters {
		out = append(out, rawTypeName(parameter.Type, owner, registry))
	}
	return out
}

func sameParamTypes(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}

func argsArityHint(raw json.RawMessage) int {
	if len(raw) == 0 {
		return -1
	}
	if !isJSONArray(raw) {
		return 1
	}
	var items []json.RawMessage
	if err := json.Unmarshal(raw, &items); err != nil {
		return -1
	}
	return len(items)
}

func isJSONArray(raw []byte) bool {
	for _, b := range raw {
		switch b {
		case ' ', '\t', '\n', '\r':
			continue
		case '[':
			return true
		default:
			return false
		}
	}
	return false
}
