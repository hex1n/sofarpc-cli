package target

import (
	"fmt"
	"sort"
	"strings"

	"github.com/hex1n/sofarpc-cli/internal/core/invocationprops"
	"github.com/hex1n/sofarpc-cli/internal/core/projectconfig"
)

const (
	ModeDirect   = "direct"
	ModeRegistry = "registry"
)

const (
	defaultProtocol         = "bolt"
	defaultSerialization    = "hessian2"
	defaultTimeoutMS        = 3000
	defaultConnectTimeoutMS = 1000
)

// Input is the per-call target override surface exposed to MCP tools.
// Empty fields fall through to the ambient Sources and then defaults.
type Input struct {
	Service          string
	DirectURL        string
	RegistryAddress  string
	RegistryProtocol string
	Protocol         string
	Serialization    string
	UniqueID         string
	TimeoutMS        int
	ConnectTimeoutMS int
	Explain          bool
	// Profile names the Target Profile to select. Empty means fall back to
	// the project's DefaultProfile, then to base-only resolution.
	Profile string
}

// Config is the resolved wire target handed to invoke/replay/worker.
type Config struct {
	Mode             string `json:"mode,omitempty"`
	DirectURL        string `json:"directUrl,omitempty"`
	RegistryAddress  string `json:"registryAddress,omitempty"`
	RegistryProtocol string `json:"registryProtocol,omitempty"`
	Protocol         string `json:"protocol,omitempty"`
	Serialization    string `json:"serialization,omitempty"`
	UniqueID         string `json:"uniqueId,omitempty"`
	TimeoutMS        int    `json:"timeoutMs,omitempty"`
	ConnectTimeoutMS int    `json:"connectTimeoutMs,omitempty"`
}

// Sources is the ambient, already-materialised config surface available
// to resolution. ProjectLocal and Project are loaded from
// .sofarpc/config.local.json and .sofarpc/config.json respectively.
type Sources struct {
	Env                              Config
	Project                          Config
	ProjectLocal                     Config
	ProjectPolicy                    PolicyConfig
	ProjectLocalPolicy               PolicyConfig
	ProjectInvocationProperties      invocationprops.Declarations
	ProjectLocalInvocationProperties invocationprops.Declarations
	ProjectRoot                      string
	ConfigErrors                     []ConfigError
	// ProjectProfiles / ProjectLocalProfiles hold the named Target Profile
	// target configs from the shared and local config files. DefaultProfile
	// is the resolved fallback profile (local declaration wins over shared).
	ProjectProfiles      map[string]Config
	ProjectLocalProfiles map[string]Config
	DefaultProfile       string
	// ProjectProfileInvocationProperties / ProjectLocalProfileInvocationProperties
	// hold each Target Profile's invocation-property declarations, keyed by
	// profile name, from the shared and local config files.
	ProjectProfileInvocationProperties      map[string]invocationprops.Declarations
	ProjectLocalProfileInvocationProperties map[string]invocationprops.Declarations
}

// PolicyConfig contains project-scoped execution policy. It is parsed from
// the same .sofarpc/config*.json files as target config, but kept separate
// from Config so resolved invoke targets do not leak policy-only fields.
type PolicyConfig struct {
	AllowedServices    []string `json:"allowedServices,omitempty"`
	AllowedServicesSet bool     `json:"allowedServicesSet,omitempty"`
}

type ServiceAllowlist struct {
	Configured bool
	Services   []string
	Source     string
}

// ConfigError records a project config file that existed but could not be
// parsed. Missing config files are not errors.
type ConfigError struct {
	Path  string `json:"path"`
	Error string `json:"error"`
}

// Layer records which output fields a resolution layer contributed.
type Layer struct {
	Name          string   `json:"name"`
	AppliedFields []string `json:"appliedFields,omitempty"`
}

// TraceValue is one field contribution rendered as a stable string so
// agents can surface it without guessing Go/JSON scalar types.
type TraceValue struct {
	Layer string `json:"layer"`
	Value string `json:"value,omitempty"`
}

// FieldTrace records which layer won for a field and which lower
// priority layers were shadowed.
type FieldTrace struct {
	Field    string       `json:"field"`
	Winner   TraceValue   `json:"winner"`
	Shadowed []TraceValue `json:"shadowed,omitempty"`
}

// Report is the full diagnostic payload produced by Resolve.
type Report struct {
	Target       Config        `json:"target"`
	Service      string        `json:"service,omitempty"`
	Layers       []Layer       `json:"layers,omitempty"`
	ConfigErrors []ConfigError `json:"configErrors,omitempty"`
	Explain      []string      `json:"explain,omitempty"`
	Trace        []FieldTrace  `json:"trace,omitempty"`
	// ActiveProfile is the selected Target Profile (per-call input, else
	// DefaultProfile). AvailableProfiles lists every profile defined across
	// both config files. ProfileError is set, and no target is resolved,
	// when ActiveProfile names a profile defined in neither file.
	ActiveProfile     string   `json:"activeProfile,omitempty"`
	AvailableProfiles []string `json:"availableProfiles,omitempty"`
	ProfileError      string   `json:"profileError,omitempty"`
}

type fieldSpec struct {
	name   string
	get    func(Config) string
	getInt func(Config) int
	set    func(*Config, string)
	setInt func(*Config, int)
}

type layerConfig struct {
	name string
	cfg  Config
}

var configFieldSpecs = []fieldSpec{
	{
		name: "registryProtocol",
		get:  func(c Config) string { return c.RegistryProtocol },
		set:  func(c *Config, v string) { c.RegistryProtocol = v },
	},
	{
		name: "protocol",
		get:  func(c Config) string { return c.Protocol },
		set:  func(c *Config, v string) { c.Protocol = v },
	},
	{
		name: "serialization",
		get:  func(c Config) string { return c.Serialization },
		set:  func(c *Config, v string) { c.Serialization = v },
	},
	{
		name: "uniqueId",
		get:  func(c Config) string { return c.UniqueID },
		set:  func(c *Config, v string) { c.UniqueID = v },
	},
	{
		name:   "timeoutMs",
		getInt: func(c Config) int { return c.TimeoutMS },
		setInt: func(c *Config, v int) { c.TimeoutMS = v },
	},
	{
		name:   "connectTimeoutMs",
		getInt: func(c Config) int { return c.ConnectTimeoutMS },
		setInt: func(c *Config, v int) { c.ConnectTimeoutMS = v },
	},
}

// Resolve merges the target layers in descending priority. When a Target
// Profile is active, its layers sit above the base layers so a profile-specific
// value beats a base value:
//
//	input
//	  > project-local:profiles[P] > project:profiles[P]
//	  > project-local (base) > project (base)
//	  > mcp-env > built-in defaults
//
// A profile that is named but defined in neither config file is a hard error:
// Resolve returns a Report with ProfileError set and no resolved target rather
// than silently falling through to the base layers.
func Resolve(input Input, sources Sources) Report {
	report := Report{
		Service:           strings.TrimSpace(input.Service),
		ConfigErrors:      append([]ConfigError(nil), sources.ConfigErrors...),
		AvailableProfiles: availableProfiles(sources),
	}

	activeProfile := resolveActiveProfile(input.Profile, sources)
	report.ActiveProfile = activeProfile
	if activeProfile != "" && !profileDefined(sources, activeProfile) {
		report.ProfileError = fmt.Sprintf("profile %q is not defined; available profiles: %s",
			activeProfile, formatProfileList(report.AvailableProfiles))
		return report
	}

	layers := buildLayerStack(input, sources, activeProfile)
	layerFields := make(map[string][]string, len(layers))

	endpoint, endpointTraces, endpointFields := resolveEndpoint(layers)
	report.Target.Mode = endpoint.Mode
	report.Target.DirectURL = endpoint.DirectURL
	report.Target.RegistryAddress = endpoint.RegistryAddress
	if input.Explain {
		report.Trace = append(report.Trace, endpointTraces...)
	}
	for layerName, fields := range endpointFields {
		layerFields[layerName] = append(layerFields[layerName], fields...)
	}

	for _, spec := range configFieldSpecs {
		winner, trace, ok := resolveField(spec, layers)
		if !ok {
			continue
		}
		layerFields[trace.Winner.Layer] = append(layerFields[trace.Winner.Layer], spec.name)
		if spec.set != nil {
			spec.set(&report.Target, winner)
		} else if spec.setInt != nil {
			spec.setInt(&report.Target, parseTraceInt(winner))
		}
		if input.Explain {
			report.Trace = append(report.Trace, trace)
		}
	}

	report.Target = normalizeResolvedTarget(report.Target)
	if report.Target.Mode != ModeRegistry {
		report.Target.RegistryProtocol = ""
	}
	report.Layers = buildLayers(layers, layerFields)
	if input.Explain {
		report.Explain = buildExplain(report.Trace)
	}
	return report
}

// buildLayerStack assembles the descending-priority layer list, inserting the
// active profile's layers above the base layers when a profile is selected.
func buildLayerStack(input Input, sources Sources, activeProfile string) []layerConfig {
	layers := make([]layerConfig, 0, 6)
	layers = append(layers, layerConfig{name: "input", cfg: configFromInput(input)})
	if activeProfile != "" {
		layers = append(layers,
			layerConfig{name: profileLayerName("project-local", activeProfile), cfg: normalizeConfig(sources.ProjectLocalProfiles[activeProfile])},
			layerConfig{name: profileLayerName("project", activeProfile), cfg: normalizeConfig(sources.ProjectProfiles[activeProfile])},
		)
	}
	layers = append(layers,
		layerConfig{name: "project-local", cfg: normalizeConfig(sources.ProjectLocal)},
		layerConfig{name: "project", cfg: normalizeConfig(sources.Project)},
		layerConfig{name: "mcp-env", cfg: normalizeConfig(sources.Env)},
		layerConfig{name: "defaults", cfg: defaultConfig()},
	)
	return layers
}

func profileLayerName(tier, profile string) string {
	return tier + ":profiles[" + profile + "]"
}

// InvocationPropertySources returns the invocation-property declaration sources
// in descending precedence, inserting the active Target Profile's declarations
// above the base declarations so a profile value beats a base value:
//
//	input
//	  > project-local:profiles[P] > project:profiles[P]
//	  > project-local (base) > project (base)
//
// profile is the already-resolved per-call/session selection; an empty profile
// falls back to sources.DefaultProfile. input carries the caller's per-call
// declarations and always ranks highest. This is the single definition of the
// invocation-property layer order, shared by plan building and doctor.
func InvocationPropertySources(input invocationprops.Declarations, profile string, sources Sources) []invocationprops.Source {
	profile = resolveActiveProfile(profile, sources)
	srcs := []invocationprops.Source{
		{Name: "input", Declarations: input},
	}
	if profile != "" {
		srcs = append(srcs,
			invocationprops.Source{Name: profileLayerName("project-local", profile), Declarations: sources.ProjectLocalProfileInvocationProperties[profile]},
			invocationprops.Source{Name: profileLayerName("project", profile), Declarations: sources.ProjectProfileInvocationProperties[profile]},
		)
	}
	srcs = append(srcs,
		invocationprops.Source{Name: "project-local", Declarations: sources.ProjectLocalInvocationProperties},
		invocationprops.Source{Name: "project", Declarations: sources.ProjectInvocationProperties},
	)
	return srcs
}

// availableProfiles returns the sorted union of profile names defined across
// the shared and local config files.
func availableProfiles(sources Sources) []string {
	if len(sources.ProjectProfiles) == 0 && len(sources.ProjectLocalProfiles) == 0 {
		return nil
	}
	set := make(map[string]struct{}, len(sources.ProjectProfiles)+len(sources.ProjectLocalProfiles))
	for name := range sources.ProjectProfiles {
		set[name] = struct{}{}
	}
	for name := range sources.ProjectLocalProfiles {
		set[name] = struct{}{}
	}
	out := make([]string, 0, len(set))
	for name := range set {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// resolveActiveProfile applies the per-call/session selection, falling back to
// the project's DefaultProfile when empty. It is the single fallback rule
// shared by Resolve and InvocationPropertySources so the two never drift.
func resolveActiveProfile(profile string, sources Sources) string {
	if p := strings.TrimSpace(profile); p != "" {
		return p
	}
	return strings.TrimSpace(sources.DefaultProfile)
}

func profileDefined(sources Sources, name string) bool {
	if _, ok := sources.ProjectProfiles[name]; ok {
		return true
	}
	_, ok := sources.ProjectLocalProfiles[name]
	return ok
}

func formatProfileList(names []string) string {
	if len(names) == 0 {
		return "(none defined)"
	}
	return strings.Join(names, ", ")
}

// ProjectSources builds a Sources value for projectRoot by reading the optional
// project-level target config files. Config load errors are carried in the
// returned Sources so callers can surface them in target/open/doctor output
// without making missing files fatal.
func ProjectSources(projectRoot string, env Config) Sources {
	projectRoot = strings.TrimSpace(projectRoot)
	src := Sources{
		Env:         env,
		ProjectRoot: projectRoot,
	}
	if projectRoot == "" {
		return src
	}
	applyProjectConfig(&src, projectconfig.KindShared)
	applyProjectConfig(&src, projectconfig.KindLocal)
	return src
}

func applyProjectConfig(src *Sources, kind projectconfig.Kind) {
	loaded, err := projectconfig.Read(src.ProjectRoot, kind)
	if err != nil {
		src.ConfigErrors = append(src.ConfigErrors, ConfigError{Path: loaded.Path, Error: err.Error()})
		return
	}
	if !loaded.Exists {
		return
	}
	switch kind {
	case projectconfig.KindShared:
		src.Project = configFromProjectConfig(loaded.Config)
		src.ProjectPolicy = policyFromProjectConfig(loaded)
		src.ProjectInvocationProperties = loaded.Config.InvocationProperties
		src.ProjectProfiles = profilesFromProjectConfig(loaded.Config.Profiles)
		src.ProjectProfileInvocationProperties = profileInvocationProperties(loaded.Config.Profiles)
		if dp := strings.TrimSpace(loaded.Config.DefaultProfile); dp != "" && src.DefaultProfile == "" {
			src.DefaultProfile = dp
		}
	case projectconfig.KindLocal:
		src.ProjectLocal = configFromProjectConfig(loaded.Config)
		src.ProjectLocalPolicy = policyFromProjectConfig(loaded)
		src.ProjectLocalInvocationProperties = loaded.Config.InvocationProperties
		src.ProjectLocalProfiles = profilesFromProjectConfig(loaded.Config.Profiles)
		src.ProjectLocalProfileInvocationProperties = profileInvocationProperties(loaded.Config.Profiles)
		// Local declaration wins over shared.
		if dp := strings.TrimSpace(loaded.Config.DefaultProfile); dp != "" {
			src.DefaultProfile = dp
		}
	}
}

func profileInvocationProperties(profiles map[string]projectconfig.ProfileConfig) map[string]invocationprops.Declarations {
	if len(profiles) == 0 {
		return nil
	}
	out := make(map[string]invocationprops.Declarations, len(profiles))
	for name, p := range profiles {
		if len(p.InvocationProperties) > 0 {
			out[name] = p.InvocationProperties
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func profilesFromProjectConfig(profiles map[string]projectconfig.ProfileConfig) map[string]Config {
	if len(profiles) == 0 {
		return nil
	}
	out := make(map[string]Config, len(profiles))
	for name, p := range profiles {
		out[name] = normalizeConfig(Config{
			DirectURL:        p.DirectURL,
			RegistryAddress:  p.RegistryAddress,
			RegistryProtocol: p.RegistryProtocol,
			Protocol:         p.Protocol,
			Serialization:    p.Serialization,
			UniqueID:         p.UniqueID,
			TimeoutMS:        p.TimeoutMS,
			ConnectTimeoutMS: p.ConnectTimeoutMS,
		})
	}
	return out
}

// AllowedServices returns the effective project-scoped service allowlist.
// Local config wins over shared config. A nil return means the project did
// not set a service allowlist.
func AllowedServices(sources Sources) []string {
	allowlist := ServiceAllowlistForSources(sources)
	if !allowlist.Configured {
		return nil
	}
	return append([]string(nil), allowlist.Services...)
}

// ServiceAllowlistForSources returns the effective service allowlist plus
// whether it was explicitly configured. An explicitly empty local allowlist
// still wins over a shared allowlist and blocks every service.
func ServiceAllowlistForSources(sources Sources) ServiceAllowlist {
	if policyHasAllowedServices(sources.ProjectLocalPolicy) {
		return ServiceAllowlist{
			Configured: true,
			Services:   append([]string(nil), sources.ProjectLocalPolicy.AllowedServices...),
			Source:     "project allowedServices",
		}
	}
	if policyHasAllowedServices(sources.ProjectPolicy) {
		return ServiceAllowlist{
			Configured: true,
			Services:   append([]string(nil), sources.ProjectPolicy.AllowedServices...),
			Source:     "project allowedServices",
		}
	}
	return ServiceAllowlist{}
}

func policyHasAllowedServices(policy PolicyConfig) bool {
	return policy.AllowedServicesSet || len(policy.AllowedServices) > 0
}

func resolveEndpoint(layers []layerConfig) (Config, []FieldTrace, map[string][]string) {
	var winner layerConfig
	var endpoint Config
	for _, layer := range layers {
		cfg := normalizeConfig(layer.cfg)
		switch {
		case cfg.DirectURL != "":
			winner = layerConfig{name: layer.name, cfg: cfg}
			endpoint = Config{Mode: ModeDirect, DirectURL: cfg.DirectURL}
		case cfg.RegistryAddress != "":
			winner = layerConfig{name: layer.name, cfg: cfg}
			endpoint = Config{Mode: ModeRegistry, RegistryAddress: cfg.RegistryAddress}
		}
		if endpoint.Mode != "" {
			break
		}
	}
	if endpoint.Mode == "" {
		return Config{}, nil, nil
	}

	fields := map[string][]string{winner.name: endpointFieldNames(endpoint.Mode)}
	traces := endpointFieldTraces(winner, endpoint, layers)
	return endpoint, traces, fields
}

func endpointFieldNames(mode string) []string {
	switch mode {
	case ModeDirect:
		return []string{"directUrl"}
	case ModeRegistry:
		return []string{"registryAddress"}
	default:
		return nil
	}
}

func endpointFieldTraces(winner layerConfig, endpoint Config, layers []layerConfig) []FieldTrace {
	var traces []FieldTrace
	switch endpoint.Mode {
	case ModeDirect:
		traces = append(traces, FieldTrace{
			Field:    "directUrl",
			Winner:   TraceValue{Layer: winner.name, Value: endpoint.DirectURL},
			Shadowed: shadowedEndpointValues(winner.name, layers),
		})
	case ModeRegistry:
		traces = append(traces, FieldTrace{
			Field:    "registryAddress",
			Winner:   TraceValue{Layer: winner.name, Value: endpoint.RegistryAddress},
			Shadowed: shadowedEndpointValues(winner.name, layers),
		})
	}
	return traces
}

func shadowedEndpointValues(winnerName string, layers []layerConfig) []TraceValue {
	var out []TraceValue
	seenWinner := false
	for _, layer := range layers {
		if layer.name == winnerName {
			seenWinner = true
			continue
		}
		if !seenWinner {
			continue
		}
		cfg := normalizeConfig(layer.cfg)
		if cfg.DirectURL != "" {
			out = append(out, TraceValue{Layer: layer.name, Value: "directUrl=" + cfg.DirectURL})
		}
		if cfg.RegistryAddress != "" {
			out = append(out, TraceValue{Layer: layer.name, Value: "registryAddress=" + cfg.RegistryAddress})
		}
	}
	return out
}

func configFromProjectConfig(cfg projectconfig.Config) Config {
	return normalizeConfig(Config{
		DirectURL:        cfg.DirectURL,
		RegistryAddress:  cfg.RegistryAddress,
		RegistryProtocol: cfg.RegistryProtocol,
		Protocol:         cfg.Protocol,
		Serialization:    cfg.Serialization,
		UniqueID:         cfg.UniqueID,
		TimeoutMS:        cfg.TimeoutMS,
		ConnectTimeoutMS: cfg.ConnectTimeoutMS,
	})
}

func policyFromProjectConfig(loaded projectconfig.ReadResult) PolicyConfig {
	if !loaded.AllowedServicesSet {
		return PolicyConfig{}
	}
	return PolicyConfig{
		AllowedServices:    append([]string(nil), loaded.Config.AllowedServices...),
		AllowedServicesSet: true,
	}
}

func configFromInput(in Input) Config {
	return normalizeConfig(Config{
		DirectURL:        in.DirectURL,
		RegistryAddress:  in.RegistryAddress,
		RegistryProtocol: in.RegistryProtocol,
		Protocol:         in.Protocol,
		Serialization:    in.Serialization,
		UniqueID:         in.UniqueID,
		TimeoutMS:        in.TimeoutMS,
		ConnectTimeoutMS: in.ConnectTimeoutMS,
	})
}

func defaultConfig() Config {
	return Config{
		Protocol:         defaultProtocol,
		Serialization:    defaultSerialization,
		TimeoutMS:        defaultTimeoutMS,
		ConnectTimeoutMS: defaultConnectTimeoutMS,
	}
}

func normalizeConfig(cfg Config) Config {
	cfg.Mode = strings.TrimSpace(cfg.Mode)
	cfg.DirectURL = strings.TrimSpace(cfg.DirectURL)
	cfg.RegistryAddress = strings.TrimSpace(cfg.RegistryAddress)
	cfg.RegistryProtocol = strings.TrimSpace(cfg.RegistryProtocol)
	cfg.Protocol = strings.TrimSpace(cfg.Protocol)
	cfg.Serialization = strings.TrimSpace(cfg.Serialization)
	cfg.UniqueID = strings.TrimSpace(cfg.UniqueID)
	return cfg
}

// Normalize trims whitespace on target fields and infers Mode from
// DirectURL / RegistryAddress. It is the canonical pre-step for code
// paths that receive a Config from outside the Resolve pipeline
// (Probe, direct-transport support checks).
func Normalize(cfg Config) Config {
	return normalizeResolvedTarget(cfg)
}

// SupportsDirectBolt reports whether cfg picks out the single concrete
// invoke shape the pure-Go runtime can execute today. Empty Protocol /
// Serialization are treated as "accept defaults"; callers that want to
// surface a precise reason to the user should read cfg.Protocol /
// cfg.Serialization directly after calling this.
func SupportsDirectBolt(cfg Config) bool {
	cfg = Normalize(cfg)
	if cfg.Mode != ModeDirect || cfg.DirectURL == "" {
		return false
	}
	if cfg.Protocol != "" && cfg.Protocol != defaultProtocol {
		return false
	}
	if cfg.Serialization != "" && cfg.Serialization != defaultSerialization {
		return false
	}
	return true
}

func normalizeResolvedTarget(cfg Config) Config {
	cfg = normalizeConfig(cfg)
	switch {
	case cfg.DirectURL != "":
		cfg.Mode = ModeDirect
		cfg.RegistryAddress = ""
		cfg.RegistryProtocol = ""
	case cfg.RegistryAddress != "":
		cfg.Mode = ModeRegistry
		cfg.DirectURL = ""
	default:
		cfg.Mode = ""
	}
	return cfg
}

func resolveField(spec fieldSpec, layers []layerConfig) (string, FieldTrace, bool) {
	trace := FieldTrace{Field: spec.name}
	for i, layer := range layers {
		if value, ok := readField(spec, layer.cfg); ok {
			if trace.Winner.Layer == "" {
				trace.Winner = TraceValue{Layer: layer.name, Value: value}
				for _, lower := range layers[i+1:] {
					if shadowed, shadowedOK := readField(spec, lower.cfg); shadowedOK {
						trace.Shadowed = append(trace.Shadowed, TraceValue{
							Layer: lower.name,
							Value: shadowed,
						})
					}
				}
				return value, trace, true
			}
		}
	}
	return "", FieldTrace{}, false
}

func readField(spec fieldSpec, cfg Config) (string, bool) {
	if spec.get != nil {
		v := spec.get(cfg)
		if strings.TrimSpace(v) == "" {
			return "", false
		}
		return v, true
	}
	if spec.getInt != nil {
		v := spec.getInt(cfg)
		if v == 0 {
			return "", false
		}
		return fmt.Sprintf("%d", v), true
	}
	return "", false
}

func buildLayers(layerOrder []layerConfig, applied map[string][]string) []Layer {
	out := make([]Layer, 0, len(layerOrder))
	for _, layer := range layerOrder {
		fields := applied[layer.name]
		if len(fields) == 0 {
			continue
		}
		sort.Strings(fields)
		out = append(out, Layer{Name: layer.name, AppliedFields: fields})
	}
	return out
}

func buildExplain(traces []FieldTrace) []string {
	out := make([]string, 0, len(traces))
	for _, trace := range traces {
		line := fmt.Sprintf("%s from %s (%s)", trace.Field, trace.Winner.Layer, trace.Winner.Value)
		if len(trace.Shadowed) > 0 {
			parts := make([]string, 0, len(trace.Shadowed))
			for _, shadowed := range trace.Shadowed {
				parts = append(parts, fmt.Sprintf("%s=%s", shadowed.Layer, shadowed.Value))
			}
			line += "; shadowed " + strings.Join(parts, ", ")
		}
		out = append(out, line)
	}
	sort.Strings(out)
	return out
}

func parseTraceInt(raw string) int {
	var value int
	_, _ = fmt.Sscanf(raw, "%d", &value)
	return value
}
