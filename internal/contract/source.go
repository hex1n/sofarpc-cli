package contract

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/hex1n/sofarpc-cli/internal/facadekit"
	"github.com/hex1n/sofarpc-cli/internal/model"
	"github.com/hex1n/sofarpc-cli/internal/projectscan"
)

var (
	discoverProjectFn      = projectscan.DiscoverProject
	discoverArtifactsFn    = projectscan.DiscoverArtifacts
	matchServiceFn         = projectscan.MatchService
	loadProjectConfigFn    = facadekit.LoadConfig
	loadSemanticRegistryFn = facadekit.LoadSemanticRegistry
)

func DescribeServiceFromProject(projectRoot, service string) (model.ServiceSchema, error) {
	projectRoot = strings.TrimSpace(projectRoot)
	service = strings.TrimSpace(service)
	if projectRoot == "" {
		return model.ServiceSchema{}, fmt.Errorf("project root is required")
	}
	if service == "" {
		return model.ServiceSchema{}, fmt.Errorf("service is required")
	}

	layout, err := discoverProjectFn(projectRoot)
	if err != nil {
		return model.ServiceSchema{}, err
	}
	if len(layout.FacadeModules) == 0 {
		return model.ServiceSchema{}, fmt.Errorf("no facade modules discovered under %s", layout.Root)
	}

	modules := layout.FacadeModules
	if match, err := matchServiceFn(layout.Root, service, layout.FacadeModules); err == nil {
		modules = []projectscan.FacadeModule{match.Module}
	}
	sourceRoots := sourceRootsForModules(layout.Root, modules)
	if len(sourceRoots) == 0 {
		return model.ServiceSchema{}, fmt.Errorf("no facade source roots discovered for %s", service)
	}

	cfg, err := loadProjectConfigFn(layout.Root, true)
	if err != nil {
		cfg = facadekit.DefaultConfig()
	}
	registry, err := loadSemanticRegistryFn(layout.Root, sourceRoots, cfg.RequiredMarkers)
	if err != nil {
		return model.ServiceSchema{}, err
	}
	return BuildServiceSchema(registry, service)
}

func BuildServiceSchema(registry facadekit.Registry, service string) (model.ServiceSchema, error) {
	service = strings.TrimSpace(service)
	serviceInfo, ok := registry[service]
	if !ok {
		return model.ServiceSchema{}, fmt.Errorf("service %s not found in semantic registry", service)
	}
	if serviceInfo.Kind != "interface" {
		return model.ServiceSchema{}, fmt.Errorf("service %s is not an interface", service)
	}

	methods := make([]model.MethodSchema, 0, len(serviceInfo.Methods))
	for _, method := range serviceInfo.Methods {
		methods = append(methods, convertMethodSchema(serviceInfo, method, registry))
	}
	sort.Slice(methods, func(i, j int) bool {
		if methods[i].Name == methods[j].Name {
			return strings.Join(methods[i].ParamTypeSignatures, ",") < strings.Join(methods[j].ParamTypeSignatures, ",")
		}
		return methods[i].Name < methods[j].Name
	})

	return model.ServiceSchema{
		Service: serviceInfo.FQN,
		Methods: methods,
	}, nil
}

func sourceRootsForModules(projectRoot string, modules []projectscan.FacadeModule) []string {
	seen := map[string]struct{}{}
	roots := make([]string, 0, len(modules))
	for _, module := range modules {
		if strings.TrimSpace(module.SourceRoot) == "" {
			continue
		}
		root := facadekit.ResolveRepoPath(module.SourceRoot, projectRoot)
		root = filepath.Clean(root)
		if _, ok := seen[root]; ok {
			continue
		}
		seen[root] = struct{}{}
		roots = append(roots, root)
	}
	sort.Strings(roots)
	return roots
}

func convertMethodSchema(owner facadekit.SemanticClassInfo, method facadekit.SemanticMethodInfo, registry facadekit.Registry) model.MethodSchema {
	paramTypes := make([]string, 0, len(method.Parameters))
	paramTypeSignatures := make([]string, 0, len(method.Parameters))
	for _, parameter := range method.Parameters {
		signature := strings.TrimSpace(parameter.Type)
		paramTypeSignatures = append(paramTypeSignatures, signature)
		paramTypes = append(paramTypes, rawTypeName(signature, owner, registry))
	}
	return model.MethodSchema{
		Name:                method.Name,
		ParamTypes:          paramTypes,
		ParamTypeSignatures: paramTypeSignatures,
		ReturnType:          rawTypeName(method.ReturnType, owner, registry),
	}
}

func rawTypeName(typeStr string, owner facadekit.SemanticClassInfo, registry facadekit.Registry) string {
	text := stripWildcard(strings.TrimSpace(typeStr))
	if text == "" {
		return ""
	}
	arraySuffix := ""
	for strings.HasSuffix(text, "[]") {
		arraySuffix += "[]"
		text = strings.TrimSpace(strings.TrimSuffix(text, "[]"))
	}
	head := strings.TrimSpace(strings.Split(text, "<")[0])
	if resolved := resolveTypeName(head, owner, registry); resolved != "" {
		return resolved + arraySuffix
	}
	return head + arraySuffix
}

func resolveTypeName(typeName string, owner facadekit.SemanticClassInfo, registry facadekit.Registry) string {
	typeName = stripWildcard(strings.TrimSpace(typeName))
	if typeName == "" {
		return ""
	}
	if isPrimitiveType(typeName) || strings.Contains(typeName, ".") {
		return typeName
	}
	if owner.Imports != nil {
		if candidate, ok := owner.Imports[typeName]; ok {
			return candidate
		}
	}
	if owner.SamePkgPrefix != "" {
		candidate := owner.SamePkgPrefix + "." + typeName
		if _, ok := registry[candidate]; ok {
			return candidate
		}
	}
	var suffixMatches []string
	for fqn := range registry {
		if strings.HasSuffix(fqn, "."+typeName) {
			suffixMatches = append(suffixMatches, fqn)
		}
	}
	if len(suffixMatches) == 1 {
		return suffixMatches[0]
	}
	return typeName
}

func stripWildcard(typeStr string) string {
	text := strings.TrimSpace(typeStr)
	for _, prefix := range []string{"? extends ", "? super "} {
		if strings.HasPrefix(text, prefix) {
			return strings.TrimSpace(strings.TrimPrefix(text, prefix))
		}
	}
	if text == "?" {
		return "java.lang.Object"
	}
	return text
}

func isPrimitiveType(typeName string) bool {
	switch typeName {
	case "byte", "short", "int", "long", "float", "double", "boolean", "char", "void":
		return true
	default:
		return false
	}
}
