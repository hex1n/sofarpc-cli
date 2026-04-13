package cli

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/hex1n/sofarpc-cli/internal/config"
	"github.com/hex1n/sofarpc-cli/internal/model"
)

type invocationInputs struct {
	ManifestPath     string
	ContextName      string
	Service          string
	Method           string
	TypesCSV         string
	ArgsJSON         string
	PayloadMode      string
	DirectURL        string
	RegistryAddress  string
	RegistryProtocol string
	Protocol         string
	Serialization    string
	UniqueID         string
	TimeoutMS        int
	ConnectTimeoutMS int
	StubPathCSV      string
	SofaRPCVersion   string
	JavaBin          string
	RuntimeJar       string
}

type resolvedInvocation struct {
	Request              model.InvocationRequest
	ManifestPath         string
	ManifestFound        bool
	ActiveContext        string
	SofaRPCVersion       string
	SofaRPCVersionSource string
	JavaBin              string
	RuntimeJar           string
	StubPaths            []string
}

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
	activeName := firstNonEmpty(input.ContextName, manifest.DefaultContext, store.Active)
	activeContext := model.Context{}
	if activeName != "" {
		contextValue, ok := store.Contexts[activeName]
		if !ok {
			return resolvedInvocation{}, fmt.Errorf("context %q does not exist", activeName)
		}
		activeContext = contextValue
	}
	manifestTarget := manifest.DefaultTarget
	defaults := defaultsTarget()
	target := model.TargetConfig{
		Mode:             firstNonEmpty(inputMode(input), activeContext.Mode, manifestTarget.Mode),
		DirectURL:        firstNonEmpty(input.DirectURL, activeContext.DirectURL, manifestTarget.DirectURL),
		RegistryAddress:  firstNonEmpty(input.RegistryAddress, activeContext.RegistryAddress, manifestTarget.RegistryAddress),
		RegistryProtocol: firstNonEmpty(input.RegistryProtocol, activeContext.RegistryProtocol, manifestTarget.RegistryProtocol),
		Protocol:         firstNonEmpty(input.Protocol, activeContext.Protocol, manifestTarget.Protocol, defaults.Protocol),
		Serialization:    firstNonEmpty(input.Serialization, activeContext.Serialization, manifestTarget.Serialization, defaults.Serialization),
		UniqueID:         firstNonEmpty(input.UniqueID, activeContext.UniqueID, manifestTarget.UniqueID),
		TimeoutMS:        firstPositive(input.TimeoutMS, activeContext.TimeoutMS, manifestTarget.TimeoutMS, defaults.TimeoutMS),
		ConnectTimeoutMS: firstPositive(input.ConnectTimeoutMS, activeContext.ConnectTimeoutMS, manifestTarget.ConnectTimeoutMS, defaults.ConnectTimeoutMS),
	}
	if target.Mode == "" {
		switch {
		case target.DirectURL != "":
			target.Mode = model.ModeDirect
		case target.RegistryAddress != "":
			target.Mode = model.ModeRegistry
		}
	}
	serviceName := input.Service
	methodName := input.Method
	serviceConfig := manifest.Services[serviceName]
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
	stubPaths, err := resolveStubPaths(a.Cwd, manifestPath, manifest.StubPaths, input.StubPathCSV)
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
		return model.ModeDirect
	case input.RegistryAddress != "":
		return model.ModeRegistry
	default:
		return ""
	}
}

func resolveStubPaths(cwd, manifestPath string, manifestPaths []string, rawCSV string) ([]string, error) {
	var candidates []string
	switch {
	case strings.TrimSpace(rawCSV) != "":
		candidates = parseCSV(rawCSV)
	case len(manifestPaths) > 0:
		candidates = manifestPaths
	}
	if len(candidates) == 0 {
		return nil, nil
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
