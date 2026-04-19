package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/hex1n/sofarpc-cli/internal/config"
	"github.com/hex1n/sofarpc-cli/internal/facadeconfig"
	"github.com/hex1n/sofarpc-cli/internal/model"
	"github.com/hex1n/sofarpc-cli/internal/projectscan"
	"github.com/hex1n/sofarpc-cli/internal/targetmodel"
)

func (a *App) resolveInvocation(input invocationInputs) (resolvedInvocation, error) {
	manifestPath := resolveManifestPath(a.Cwd, input.ManifestPath)
	manifest, manifestFound, err := config.LoadManifest(manifestPath)
	if err != nil {
		return resolvedInvocation{}, err
	}
	store, err := config.LoadContextStore(a.Paths)
	if err != nil {
		return resolvedInvocation{}, err
	}
	activeName, activeContext := resolveActiveContext(store, input.ContextName, manifest.DefaultContext, a.Cwd, manifestPath)
	if activeName != "" && activeContext.Name == "" {
		return resolvedInvocation{}, fmt.Errorf("context %q does not exist", activeName)
	}
	serviceName := input.Service
	serviceConfig := manifest.Services[serviceName]
	manifestTarget := manifest.DefaultTarget
	defaults := defaultsTarget()
	target := targetmodel.TargetConfig{
		Mode:             firstNonEmpty(inputMode(input), activeContext.Mode, manifestTarget.Mode),
		DirectURL:        firstNonEmpty(input.DirectURL, activeContext.DirectURL, manifestTarget.DirectURL),
		RegistryAddress:  firstNonEmpty(input.RegistryAddress, activeContext.RegistryAddress, manifestTarget.RegistryAddress),
		RegistryProtocol: firstNonEmpty(input.RegistryProtocol, activeContext.RegistryProtocol, manifestTarget.RegistryProtocol),
		Protocol:         firstNonEmpty(input.Protocol, activeContext.Protocol, manifestTarget.Protocol, defaults.Protocol),
		Serialization:    firstNonEmpty(input.Serialization, activeContext.Serialization, manifestTarget.Serialization, defaults.Serialization),
		UniqueID:         firstNonEmpty(input.UniqueID, serviceConfig.UniqueID, activeContext.UniqueID, manifestTarget.UniqueID),
		TimeoutMS:        firstPositive(input.TimeoutMS, activeContext.TimeoutMS, manifestTarget.TimeoutMS, defaults.TimeoutMS),
		ConnectTimeoutMS: firstPositive(input.ConnectTimeoutMS, activeContext.ConnectTimeoutMS, manifestTarget.ConnectTimeoutMS, defaults.ConnectTimeoutMS),
	}
	if target.Mode == "" {
		switch {
		case target.DirectURL != "":
			target.Mode = targetmodel.ModeDirect
		case target.RegistryAddress != "":
			target.Mode = targetmodel.ModeRegistry
		}
	}
	methodName := input.Method
	methodConfig := serviceConfig.Methods[methodName]
	paramTypes := parseCSV(input.TypesCSV)
	if len(paramTypes) == 0 {
		paramTypes = methodConfig.ParamTypes
	}
	payloadMode := firstNonEmpty(input.PayloadMode, methodConfig.PayloadMode, model.PayloadRaw)
	argsJSON := input.ArgsJSON
	if argsJSON == "" {
		argsJSON = "[]"
	}
	if !json.Valid([]byte(argsJSON)) {
		return resolvedInvocation{}, fmt.Errorf("--args must be valid JSON")
	}
	stubPaths, err := resolveStubPaths(a.Cwd, manifestPath, manifest.StubPaths, input.StubPathCSV, serviceName)
	if err != nil {
		return resolvedInvocation{}, err
	}
	if err := requireValue("--service", serviceName); err != nil {
		return resolvedInvocation{}, err
	}
	if err := requireValue("--method", methodName); err != nil {
		return resolvedInvocation{}, err
	}
	if target.Mode == "" {
		return resolvedInvocation{}, fmt.Errorf("either a direct target or registry target is required")
	}
	request := model.InvocationRequest{
		Service:     serviceName,
		Method:      methodName,
		ParamTypes:  paramTypes,
		Args:        json.RawMessage(argsJSON),
		PayloadMode: payloadMode,
		Target:      target,
	}
	sofaRPCVersion, sofaRPCVersionSource := resolveSofaRPCVersion(input.SofaRPCVersion, manifest.SofaRPCVersion)
	return resolvedInvocation{
		Request:              request,
		ManifestPath:         manifestPath,
		ManifestFound:        manifestFound,
		ActiveContext:        activeName,
		SofaRPCVersion:       sofaRPCVersion,
		SofaRPCVersionSource: sofaRPCVersionSource,
		JavaBin:              firstNonEmpty(input.JavaBin, "java"),
		RuntimeJar:           input.RuntimeJar,
		StubPaths:            stubPaths,
	}, nil
}

func resolveSofaRPCVersion(flagValue, manifestValue string) (string, string) {
	if strings.TrimSpace(flagValue) != "" {
		return flagValue, "flag"
	}
	if strings.TrimSpace(manifestValue) != "" {
		return manifestValue, "manifest"
	}
	return defaultSofaRPCVersion, "default"
}

func inputMode(input invocationInputs) string {
	switch {
	case input.DirectURL != "":
		return targetmodel.ModeDirect
	case input.RegistryAddress != "":
		return targetmodel.ModeRegistry
	default:
		return ""
	}
}

func resolveActiveContext(store targetmodel.ContextStore, explicitContextName, manifestContextName, cwd, manifestPath string) (string, targetmodel.Context) {
	contextName := firstNonEmpty(explicitContextName, manifestContextName)
	if contextName != "" {
		ctx, ok := store.Contexts[contextName]
		if ok {
			return contextName, ctx
		}
		return contextName, targetmodel.Context{}
	}
	projectMatchedName, projectMatchedContext := resolveProjectContext(store.Contexts, projectAwareRoot(cwd, manifestPath))
	if projectMatchedName != "" {
		return projectMatchedName, projectMatchedContext
	}
	if store.Active != "" {
		activeContext, ok := store.Contexts[store.Active]
		if !ok {
			return store.Active, targetmodel.Context{}
		}
		return store.Active, activeContext
	}
	return "", targetmodel.Context{}
}

func resolveStubPaths(cwd, manifestPath string, manifestPaths []string, rawCSV, service string) ([]string, error) {
	var candidates []string
	switch {
	case strings.TrimSpace(rawCSV) != "":
		candidates = parseCSV(rawCSV)
	case len(manifestPaths) > 0:
		candidates = manifestPaths
	}
	if len(candidates) == 0 {
		autoCandidates, err := autoResolveStubPaths(cwd, manifestPath, service)
		if err != nil {
			return nil, nil
		}
		return autoCandidates, nil
	}
	baseDir := cwd
	if manifestPath != "" {
		baseDir = filepath.Dir(manifestPath)
	}
	normalized := make([]string, 0, len(candidates))
	for _, item := range candidates {
		path := item
		if !filepath.IsAbs(path) {
			path = filepath.Join(baseDir, path)
		}
		absolute, err := filepath.Abs(path)
		if err != nil {
			return nil, err
		}
		normalized = append(normalized, filepath.Clean(absolute))
	}
	return normalized, nil
}

func projectAwareRoot(cwd, manifestPath string) string {
	if strings.TrimSpace(manifestPath) != "" {
		manifestDir := filepath.Dir(manifestPath)
		if manifestDir != "" {
			if abs, err := filepath.Abs(manifestDir); err == nil {
				return abs
			}
		}
	}
	if abs, err := filepath.Abs(cwd); err == nil {
		return filepath.Clean(abs)
	}
	return filepath.Clean(cwd)
}

func resolveProjectContext(contexts map[string]targetmodel.Context, projectRoot string) (string, targetmodel.Context) {
	projectRoot, err := filepath.Abs(projectRoot)
	if err != nil {
		projectRoot = filepath.Clean(projectRoot)
	}
	bestName := ""
	bestContext := targetmodel.Context{}
	bestWeight := -1
	for name, contextValue := range contexts {
		if strings.TrimSpace(contextValue.ProjectRoot) == "" {
			continue
		}
		rawRoot, err := filepath.Abs(contextValue.ProjectRoot)
		if err != nil {
			continue
		}
		rawRoot = filepath.Clean(rawRoot)
		if !isUnderPath(projectRoot, rawRoot) {
			continue
		}
		weight := strings.Count(rawRoot, string(filepath.Separator))
		if weight > bestWeight || (weight == bestWeight && name < bestName) {
			bestWeight = weight
			bestName = name
			bestContext = contextValue
		}
	}
	return bestName, bestContext
}

func isUnderPath(path, root string) bool {
	relative, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	return relative == "." || (!strings.HasPrefix(relative, "..") && relative != "")
}

func autoResolveStubPaths(cwd, manifestPath, service string) ([]string, error) {
	projectStart := resolveProjectRootForAutoStub(cwd, manifestPath)
	layout, err := projectscan.DiscoverProject(projectStart)
	if err == nil {
		paths := discoverStubPathsFromModules(layout.Root, narrowedModules(layout.Root, service, layout.FacadeModules))
		if len(paths) > 0 {
			return normalizeStubPaths(paths)
		}
	}
	cfg, err := facadeconfig.LoadConfig(projectStart, true)
	if err != nil {
		return nil, nil
	}
	paths := discoverStubPathsFromConfig(projectStart, cfg)
	return normalizeStubPaths(paths)
}

func resolveProjectRootForAutoStub(cwd, manifestPath string) string {
	base := filepath.Dir(manifestPath)
	if manifestPath == "" || base == "." || base == "" || base == string(filepath.Separator) {
		return cwd
	}
	return base
}

func discoverStubPathsFromConfig(projectRoot string, cfg facadeconfig.Config) []string {
	return discoverStubPathsFromModules(projectRoot, cfg.FacadeModules)
}

func discoverStubPathsFromModules(projectRoot string, modules []projectscan.FacadeModule) []string {
	seen := map[string]struct{}{}
	for _, module := range modules {
		artifacts, err := projectscan.DiscoverArtifacts(projectRoot, module)
		if err != nil {
			continue
		}
		for _, path := range append(artifacts.PrimaryJars, artifacts.DependencyJars...) {
			seen[filepath.Clean(path)] = struct{}{}
		}
	}
	out := make([]string, 0, len(seen))
	for item := range seen {
		out = append(out, item)
	}
	return out
}

func narrowedModules(projectRoot, service string, modules []projectscan.FacadeModule) []projectscan.FacadeModule {
	if len(modules) == 0 || strings.TrimSpace(service) == "" {
		return modules
	}
	match, err := projectscan.MatchService(projectRoot, service, modules)
	if err != nil {
		return modules
	}
	return []projectscan.FacadeModule{match.Module}
}

func normalizeStubPaths(paths []string) ([]string, error) {
	if len(paths) == 0 {
		return nil, nil
	}
	normalized := make([]string, 0, len(paths))
	seen := map[string]struct{}{}
	for _, item := range paths {
		clean := filepath.Clean(item)
		if _, err := os.Stat(clean); err != nil {
			continue
		}
		if _, exists := seen[clean]; exists {
			continue
		}
		seen[clean] = struct{}{}
		normalized = append(normalized, clean)
	}
	sort.Strings(normalized)
	return normalized, nil
}
