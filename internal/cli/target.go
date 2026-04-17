package cli

import (
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"strings"

	"github.com/hex1n/sofarpc-cli/internal/config"
	"github.com/hex1n/sofarpc-cli/internal/facadekit"
	"github.com/hex1n/sofarpc-cli/internal/model"
	"github.com/hex1n/sofarpc-cli/internal/runtime"
)

type targetReport struct {
	ProjectRoot    string             `json:"projectRoot,omitempty"`
	ManifestPath   string             `json:"manifestPath,omitempty"`
	ManifestLoaded bool               `json:"manifestLoaded"`
	ActiveContext  string             `json:"activeContext,omitempty"`
	Service        string             `json:"service,omitempty"`
	Target         model.TargetConfig `json:"target"`
	Reachability   model.ProbeResult  `json:"reachability"`
	Candidates     []targetCandidate  `json:"candidates,omitempty"`
	Layers         []targetLayer      `json:"layers,omitempty"`
	Explain        []string           `json:"explain,omitempty"`
}

type targetCandidate struct {
	Name     string             `json:"name,omitempty"`
	Roles    []string           `json:"roles,omitempty"`
	Selected bool               `json:"selected,omitempty"`
	Target   model.TargetConfig `json:"target"`
}

type targetLayer struct {
	Name          string             `json:"name,omitempty"`
	Kind          string             `json:"kind,omitempty"`
	AppliedFields []string           `json:"appliedFields,omitempty"`
	Target        model.TargetConfig `json:"target"`
}

type contextDecision struct {
	SelectedName    string
	SelectedContext model.Context
	SelectedReason  string
	Candidates      []targetCandidate
	ProjectMatches  []string
}

func (a *App) runTarget(args []string) error {
	if len(args) == 0 || strings.HasPrefix(args[0], "-") {
		return a.runTargetShow(args)
	}
	switch args[0] {
	case "show":
		return a.runTargetShow(args[1:])
	default:
		return fmt.Errorf("unknown target subcommand %q", args[0])
	}
}

func (a *App) runTargetShow(args []string) error {
	flags := failFlagSet("target show")
	var (
		input   invocationInputs
		project string
		asJSON  bool
		showAll bool
		explain bool
	)
	flags.StringVar(&project, "project", "", "project root used to resolve manifest and project-scoped context")
	flags.StringVar(&input.ManifestPath, "manifest", "", "manifest file path")
	flags.StringVar(&input.ContextName, "context", "", "context name")
	flags.StringVar(&input.Service, "service", "", "optional service name (for manifest uniqueId resolution)")
	flags.StringVar(&input.DirectURL, "direct-url", "", "direct bolt target")
	flags.StringVar(&input.RegistryAddress, "registry-address", "", "registry address")
	flags.StringVar(&input.RegistryProtocol, "registry-protocol", "", "registry protocol")
	flags.StringVar(&input.Protocol, "protocol", "", "SOFARPC protocol")
	flags.StringVar(&input.Serialization, "serialization", "", "serialization")
	flags.StringVar(&input.UniqueID, "unique-id", "", "service uniqueId")
	flags.IntVar(&input.TimeoutMS, "timeout-ms", 0, "invoke timeout in milliseconds")
	flags.IntVar(&input.ConnectTimeoutMS, "connect-timeout-ms", 0, "connect timeout in milliseconds")
	flags.BoolVar(&asJSON, "json", false, "print JSON output")
	flags.BoolVar(&showAll, "all", false, "show all relevant target/context candidates")
	flags.BoolVar(&explain, "explain", false, "show how the final target was selected")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if len(flags.Args()) > 0 {
		return fmt.Errorf("unknown target show args: %s", strings.Join(flags.Args(), " "))
	}
	report, err := a.resolveTargetReport(project, input, showAll, explain)
	if err != nil {
		return err
	}
	if asJSON {
		return printJSON(a.Stdout, report)
	}
	return printTargetReport(a.Stdout, report)
}

func (a *App) resolveTargetReport(project string, input invocationInputs, showAll, explain bool) (targetReport, error) {
	projectRoot, projectResolved, err := resolveTargetProjectRoot(a.Cwd, project)
	if err != nil {
		return targetReport{}, err
	}
	manifestBase := a.Cwd
	if projectResolved {
		manifestBase = projectRoot
	}
	manifestPath := resolveManifestPath(manifestBase, input.ManifestPath)
	manifest, manifestFound, err := config.LoadManifest(manifestPath)
	if err != nil {
		return targetReport{}, err
	}
	store, err := config.LoadContextStore(a.Paths)
	if err != nil {
		return targetReport{}, err
	}
	decision, err := resolveContextDecision(store, input.ContextName, manifest.DefaultContext, projectRoot)
	if err != nil {
		return targetReport{}, err
	}
	serviceConfig := manifest.Services[input.Service]
	manifestTarget := manifest.DefaultTarget
	defaults := defaultsTarget()
	target := model.TargetConfig{
		Mode:             firstNonEmpty(inputMode(input), decision.SelectedContext.Mode, manifestTarget.Mode),
		DirectURL:        firstNonEmpty(input.DirectURL, decision.SelectedContext.DirectURL, manifestTarget.DirectURL),
		RegistryAddress:  firstNonEmpty(input.RegistryAddress, decision.SelectedContext.RegistryAddress, manifestTarget.RegistryAddress),
		RegistryProtocol: firstNonEmpty(input.RegistryProtocol, decision.SelectedContext.RegistryProtocol, manifestTarget.RegistryProtocol),
		Protocol:         firstNonEmpty(input.Protocol, decision.SelectedContext.Protocol, manifestTarget.Protocol, defaults.Protocol),
		Serialization:    firstNonEmpty(input.Serialization, decision.SelectedContext.Serialization, manifestTarget.Serialization, defaults.Serialization),
		UniqueID:         firstNonEmpty(input.UniqueID, serviceConfig.UniqueID, decision.SelectedContext.UniqueID, manifestTarget.UniqueID),
		TimeoutMS:        firstPositive(input.TimeoutMS, decision.SelectedContext.TimeoutMS, manifestTarget.TimeoutMS, defaults.TimeoutMS),
		ConnectTimeoutMS: firstPositive(input.ConnectTimeoutMS, decision.SelectedContext.ConnectTimeoutMS, manifestTarget.ConnectTimeoutMS, defaults.ConnectTimeoutMS),
	}
	if target.Mode == "" {
		switch {
		case target.DirectURL != "":
			target.Mode = model.ModeDirect
		case target.RegistryAddress != "":
			target.Mode = model.ModeRegistry
		}
	}
	if target.Mode == "" {
		return targetReport{}, fmt.Errorf("either a direct target or registry target is required")
	}
	report := targetReport{
		ProjectRoot:    projectRoot,
		ManifestPath:   manifestPath,
		ManifestLoaded: manifestFound,
		ActiveContext:  decision.SelectedName,
		Service:        input.Service,
		Target:         target,
		Reachability:   runtime.ProbeTarget(target),
	}
	if showAll {
		report.Candidates = decision.Candidates
		report.Layers = buildTargetLayers(input, decision, serviceConfig, manifestTarget, defaults, target)
	}
	if explain {
		report.Explain = buildTargetExplanation(input, manifest, decision, target)
	}
	return report, nil
}

func printTargetReport(out io.Writer, report targetReport) error {
	if _, err := fmt.Fprintf(out, "project root:      %s\n", emptyFallback(report.ProjectRoot, "(not resolved)")); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(out, "manifest path:     %s\n", emptyFallback(report.ManifestPath, "(not resolved)")); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(out, "manifest loaded:   %t\n", report.ManifestLoaded); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(out, "active context:    %s\n", emptyFallback(report.ActiveContext, "(none)")); err != nil {
		return err
	}
	if strings.TrimSpace(report.Service) != "" {
		if _, err := fmt.Fprintf(out, "service:           %s\n", report.Service); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintf(out, "mode:              %s\n", report.Target.Mode); err != nil {
		return err
	}
	switch report.Target.Mode {
	case model.ModeDirect:
		if _, err := fmt.Fprintf(out, "direct url:        %s\n", emptyFallback(report.Target.DirectURL, "(not set)")); err != nil {
			return err
		}
	case model.ModeRegistry:
		if _, err := fmt.Fprintf(out, "registry:          %s://%s\n", emptyFallback(report.Target.RegistryProtocol, "(not set)"), emptyFallback(report.Target.RegistryAddress, "(not set)")); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintf(out, "protocol:          %s\n", emptyFallback(report.Target.Protocol, "(not set)")); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(out, "serialization:     %s\n", emptyFallback(report.Target.Serialization, "(not set)")); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(out, "uniqueId:          %s\n", emptyFallback(report.Target.UniqueID, "(not set)")); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(out, "timeout ms:        %d\n", report.Target.TimeoutMS); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(out, "connect timeout:   %d\n", report.Target.ConnectTimeoutMS); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(out, "reachable:         %t\n", report.Reachability.Reachable); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(out, "probe target:      %s\n", emptyFallback(report.Reachability.Target, "(none)")); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(out, "probe message:     %s\n", emptyFallback(report.Reachability.Message, "(none)")); err != nil {
		return err
	}
	if len(report.Candidates) > 0 {
		if _, err := fmt.Fprintln(out, "\ncandidates:"); err != nil {
			return err
		}
		for _, candidate := range report.Candidates {
			marker := " "
			if candidate.Selected {
				marker = "*"
			}
			if _, err := fmt.Fprintf(out, "%s %s [%s]\n", marker, candidate.Name, strings.Join(candidate.Roles, ", ")); err != nil {
				return err
			}
			if candidate.Target.Mode == model.ModeDirect {
				if _, err := fmt.Fprintf(out, "  direct url: %s\n", emptyFallback(candidate.Target.DirectURL, "(not set)")); err != nil {
					return err
				}
			} else if candidate.Target.Mode == model.ModeRegistry {
				if _, err := fmt.Fprintf(out, "  registry:   %s://%s\n", emptyFallback(candidate.Target.RegistryProtocol, "(not set)"), emptyFallback(candidate.Target.RegistryAddress, "(not set)")); err != nil {
					return err
				}
			}
		}
	}
	if len(report.Layers) > 0 {
		if _, err := fmt.Fprintln(out, "\nlayers:"); err != nil {
			return err
		}
		for _, layer := range report.Layers {
			if _, err := fmt.Fprintf(out, "- %s [%s]\n", layer.Name, layer.Kind); err != nil {
				return err
			}
			if len(layer.AppliedFields) > 0 {
				if _, err := fmt.Fprintf(out, "  applied fields: %s\n", strings.Join(layer.AppliedFields, ", ")); err != nil {
					return err
				}
			}
			if hasVisibleTargetFields(layer.Target) {
				if err := printIndentedTarget(out, "  ", layer.Target); err != nil {
					return err
				}
			}
		}
	}
	if len(report.Explain) > 0 {
		if _, err := fmt.Fprintln(out, "\nselection:"); err != nil {
			return err
		}
		for _, line := range report.Explain {
			if _, err := fmt.Fprintf(out, "- %s\n", line); err != nil {
				return err
			}
		}
	}
	return nil
}

func resolveTargetProjectRoot(cwd, project string) (string, bool, error) {
	root := strings.TrimSpace(project)
	if root != "" {
		validated, err := facadekit.ValidateProjectDir(root)
		return validated, true, err
	}
	if abs, err := facadekit.ResolveProjectRoot(cwd, nil); err == nil {
		return abs, true, nil
	}
	if abs, err := facadekit.ValidateProjectDir(cwd); err == nil {
		return abs, true, nil
	}
	return "", false, nil
}

func resolveContextDecision(store model.ContextStore, explicitContextName, manifestContextName, projectRoot string) (contextDecision, error) {
	decision := contextDecision{}
	candidateMap := map[string]*targetCandidate{}
	addCandidate := func(name, role string, ctx model.Context) {
		entry, ok := candidateMap[name]
		if !ok {
			target := model.TargetConfig{
				Mode:             ctx.Mode,
				DirectURL:        ctx.DirectURL,
				RegistryAddress:  ctx.RegistryAddress,
				RegistryProtocol: ctx.RegistryProtocol,
				Protocol:         ctx.Protocol,
				Serialization:    ctx.Serialization,
				UniqueID:         ctx.UniqueID,
				TimeoutMS:        ctx.TimeoutMS,
				ConnectTimeoutMS: ctx.ConnectTimeoutMS,
			}
			entry = &targetCandidate{Name: name, Target: target}
			candidateMap[name] = entry
		}
		for _, existing := range entry.Roles {
			if existing == role {
				return
			}
		}
		entry.Roles = append(entry.Roles, role)
	}

	if explicitContextName != "" {
		ctx, ok := store.Contexts[explicitContextName]
		if !ok {
			return contextDecision{}, fmt.Errorf("context %q does not exist", explicitContextName)
		}
		addCandidate(explicitContextName, "explicit-context", ctx)
		decision.SelectedName = explicitContextName
		decision.SelectedContext = ctx
		decision.SelectedReason = "selected because --context overrides manifest, project, and active contexts"
	}

	if manifestContextName != "" {
		ctx, ok := store.Contexts[manifestContextName]
		if ok {
			addCandidate(manifestContextName, "manifest-default-context", ctx)
			if decision.SelectedName == "" {
				decision.SelectedName = manifestContextName
				decision.SelectedContext = ctx
				decision.SelectedReason = "selected because manifest.defaultContext overrides project-scoped and active contexts"
			}
		}
	}

	if strings.TrimSpace(projectRoot) != "" {
		projectMatches := collectProjectContextMatches(store.Contexts, projectRoot)
		for _, match := range projectMatches {
			addCandidate(match.Name, "project-context", match.Context)
			decision.ProjectMatches = append(decision.ProjectMatches, match.Name)
		}
		if decision.SelectedName == "" && len(projectMatches) > 0 {
			decision.SelectedName = projectMatches[0].Name
			decision.SelectedContext = projectMatches[0].Context
			decision.SelectedReason = projectSelectionReason(projectMatches, projectRoot)
		}
	}

	if store.Active != "" {
		ctx, ok := store.Contexts[store.Active]
		if ok {
			addCandidate(store.Active, "active-context", ctx)
			if decision.SelectedName == "" {
				decision.SelectedName = store.Active
				decision.SelectedContext = ctx
				decision.SelectedReason = "selected because it is the active context fallback"
			}
		}
	}

	names := make([]string, 0, len(candidateMap))
	for name := range candidateMap {
		names = append(names, name)
	}
	sort.Strings(names)
	decision.Candidates = make([]targetCandidate, 0, len(names))
	for _, name := range names {
		candidate := *candidateMap[name]
		sort.Strings(candidate.Roles)
		candidate.Selected = name == decision.SelectedName && name != ""
		decision.Candidates = append(decision.Candidates, candidate)
	}
	return decision, nil
}

type namedContext struct {
	Name    string
	Context model.Context
	Weight  int
}

func collectProjectContextMatches(contexts map[string]model.Context, projectRoot string) []namedContext {
	projectRoot, err := filepath.Abs(projectRoot)
	if err != nil {
		projectRoot = filepath.Clean(projectRoot)
	}
	matches := make([]namedContext, 0)
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
		matches = append(matches, namedContext{
			Name:    name,
			Context: contextValue,
			Weight:  strings.Count(rawRoot, string(filepath.Separator)),
		})
	}
	sort.Slice(matches, func(i, j int) bool {
		if matches[i].Weight == matches[j].Weight {
			return matches[i].Name < matches[j].Name
		}
		return matches[i].Weight > matches[j].Weight
	})
	return matches
}

func projectSelectionReason(matches []namedContext, projectRoot string) string {
	if len(matches) == 0 {
		return ""
	}
	if len(matches) == 1 {
		return fmt.Sprintf("selected project-scoped context %q because it matches %s", matches[0].Name, projectRoot)
	}
	if matches[0].Weight == matches[1].Weight {
		return fmt.Sprintf("selected project-scoped context %q because multiple matches had the same specificity and %q wins by name", matches[0].Name, matches[0].Name)
	}
	return fmt.Sprintf("selected project-scoped context %q because it is the most specific projectRoot match for %s", matches[0].Name, projectRoot)
}

func buildTargetExplanation(input invocationInputs, manifest model.Manifest, decision contextDecision, target model.TargetConfig) []string {
	lines := make([]string, 0, 4)
	if decision.SelectedName != "" {
		lines = append(lines, decision.SelectedReason)
	} else {
		lines = append(lines, "no context was selected; resolution used manifest.defaultTarget plus defaults")
	}
	if manifest.DefaultTarget.Mode != "" || manifest.DefaultTarget.DirectURL != "" || manifest.DefaultTarget.RegistryAddress != "" {
		lines = append(lines, "manifest.defaultTarget remains the fallback layer for unset context fields")
	} else {
		lines = append(lines, "manifest.defaultTarget is not configured")
	}
	if overrides := collectTargetOverrides(input); len(overrides) > 0 {
		lines = append(lines, fmt.Sprintf("explicit flag overrides applied: %s", strings.Join(overrides, ", ")))
	} else {
		lines = append(lines, "explicit flag overrides applied: none")
	}
	lines = append(lines, fmt.Sprintf("final target resolved to %s", describeResolvedTarget(target)))
	return lines
}

func collectTargetOverrides(input invocationInputs) []string {
	out := make([]string, 0, 8)
	if strings.TrimSpace(input.DirectURL) != "" {
		out = append(out, "--direct-url")
	}
	if strings.TrimSpace(input.RegistryAddress) != "" {
		out = append(out, "--registry-address")
	}
	if strings.TrimSpace(input.RegistryProtocol) != "" {
		out = append(out, "--registry-protocol")
	}
	if strings.TrimSpace(input.Protocol) != "" {
		out = append(out, "--protocol")
	}
	if strings.TrimSpace(input.Serialization) != "" {
		out = append(out, "--serialization")
	}
	if strings.TrimSpace(input.UniqueID) != "" {
		out = append(out, "--unique-id")
	}
	if input.TimeoutMS > 0 {
		out = append(out, "--timeout-ms")
	}
	if input.ConnectTimeoutMS > 0 {
		out = append(out, "--connect-timeout-ms")
	}
	return out
}

func describeResolvedTarget(target model.TargetConfig) string {
	switch target.Mode {
	case model.ModeDirect:
		return fmt.Sprintf("direct target %s", emptyFallback(target.DirectURL, "(not set)"))
	case model.ModeRegistry:
		return fmt.Sprintf("registry target %s://%s", emptyFallback(target.RegistryProtocol, "(not set)"), emptyFallback(target.RegistryAddress, "(not set)"))
	default:
		return "an unresolved target"
	}
}

func buildTargetLayers(input invocationInputs, decision contextDecision, serviceConfig model.ServiceConfig, manifestTarget, defaults, final model.TargetConfig) []targetLayer {
	layers := make([]targetLayer, 0, 6)
	if explicit := targetConfigFromInput(input); hasVisibleTargetFields(explicit) {
		layers = append(layers, targetLayer{
			Name:          "explicit-flags",
			Kind:          "override",
			AppliedFields: targetFieldNames(explicit),
			Target:        explicit,
		})
	}
	if strings.TrimSpace(serviceConfig.UniqueID) != "" {
		serviceLayer := model.TargetConfig{UniqueID: serviceConfig.UniqueID}
		layers = append(layers, targetLayer{
			Name:          "manifest.service",
			Kind:          "service-config",
			AppliedFields: targetFieldNames(serviceLayer),
			Target:        serviceLayer,
		})
	}
	if decision.SelectedName != "" {
		contextLayer := targetConfigFromContext(decision.SelectedContext)
		layers = append(layers, targetLayer{
			Name:          "selected-context:" + decision.SelectedName,
			Kind:          "context",
			AppliedFields: targetFieldNames(contextLayer),
			Target:        contextLayer,
		})
	}
	if hasVisibleTargetFields(manifestTarget) {
		layers = append(layers, targetLayer{
			Name:          "manifest.defaultTarget",
			Kind:          "manifest-target",
			AppliedFields: targetFieldNames(manifestTarget),
			Target:        manifestTarget,
		})
	}
	layers = append(layers, targetLayer{
		Name:          "built-in-defaults",
		Kind:          "defaults",
		AppliedFields: targetFieldNames(defaults),
		Target:        defaults,
	})
	layers = append(layers, targetLayer{
		Name:          "final-target",
		Kind:          "resolved",
		AppliedFields: targetFieldNames(final),
		Target:        final,
	})
	return layers
}

func targetConfigFromInput(input invocationInputs) model.TargetConfig {
	return model.TargetConfig{
		Mode:             inputMode(input),
		DirectURL:        input.DirectURL,
		RegistryAddress:  input.RegistryAddress,
		RegistryProtocol: input.RegistryProtocol,
		Protocol:         input.Protocol,
		Serialization:    input.Serialization,
		UniqueID:         input.UniqueID,
		TimeoutMS:        input.TimeoutMS,
		ConnectTimeoutMS: input.ConnectTimeoutMS,
	}
}

func targetConfigFromContext(ctx model.Context) model.TargetConfig {
	return model.TargetConfig{
		Mode:             ctx.Mode,
		DirectURL:        ctx.DirectURL,
		RegistryAddress:  ctx.RegistryAddress,
		RegistryProtocol: ctx.RegistryProtocol,
		Protocol:         ctx.Protocol,
		Serialization:    ctx.Serialization,
		UniqueID:         ctx.UniqueID,
		TimeoutMS:        ctx.TimeoutMS,
		ConnectTimeoutMS: ctx.ConnectTimeoutMS,
	}
}

func hasVisibleTargetFields(target model.TargetConfig) bool {
	return len(targetFieldNames(target)) > 0
}

func targetFieldNames(target model.TargetConfig) []string {
	fields := make([]string, 0, 8)
	if strings.TrimSpace(target.Mode) != "" {
		fields = append(fields, "mode")
	}
	if strings.TrimSpace(target.DirectURL) != "" {
		fields = append(fields, "directUrl")
	}
	if strings.TrimSpace(target.RegistryAddress) != "" {
		fields = append(fields, "registryAddress")
	}
	if strings.TrimSpace(target.RegistryProtocol) != "" {
		fields = append(fields, "registryProtocol")
	}
	if strings.TrimSpace(target.Protocol) != "" {
		fields = append(fields, "protocol")
	}
	if strings.TrimSpace(target.Serialization) != "" {
		fields = append(fields, "serialization")
	}
	if strings.TrimSpace(target.UniqueID) != "" {
		fields = append(fields, "uniqueId")
	}
	if target.TimeoutMS > 0 {
		fields = append(fields, "timeoutMs")
	}
	if target.ConnectTimeoutMS > 0 {
		fields = append(fields, "connectTimeoutMs")
	}
	return fields
}

func printIndentedTarget(out io.Writer, indent string, target model.TargetConfig) error {
	if strings.TrimSpace(target.Mode) != "" {
		if _, err := fmt.Fprintf(out, "%smode: %s\n", indent, target.Mode); err != nil {
			return err
		}
	}
	if strings.TrimSpace(target.DirectURL) != "" {
		if _, err := fmt.Fprintf(out, "%sdirect url: %s\n", indent, target.DirectURL); err != nil {
			return err
		}
	}
	if strings.TrimSpace(target.RegistryAddress) != "" || strings.TrimSpace(target.RegistryProtocol) != "" {
		if _, err := fmt.Fprintf(out, "%sregistry: %s://%s\n", indent, emptyFallback(target.RegistryProtocol, "(not set)"), emptyFallback(target.RegistryAddress, "(not set)")); err != nil {
			return err
		}
	}
	if strings.TrimSpace(target.Protocol) != "" {
		if _, err := fmt.Fprintf(out, "%sprotocol: %s\n", indent, target.Protocol); err != nil {
			return err
		}
	}
	if strings.TrimSpace(target.Serialization) != "" {
		if _, err := fmt.Fprintf(out, "%sserialization: %s\n", indent, target.Serialization); err != nil {
			return err
		}
	}
	if strings.TrimSpace(target.UniqueID) != "" {
		if _, err := fmt.Fprintf(out, "%suniqueId: %s\n", indent, target.UniqueID); err != nil {
			return err
		}
	}
	if target.TimeoutMS > 0 {
		if _, err := fmt.Fprintf(out, "%stimeout ms: %d\n", indent, target.TimeoutMS); err != nil {
			return err
		}
	}
	if target.ConnectTimeoutMS > 0 {
		if _, err := fmt.Fprintf(out, "%sconnect timeout: %d\n", indent, target.ConnectTimeoutMS); err != nil {
			return err
		}
	}
	return nil
}
