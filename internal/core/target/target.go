package target

import (
	"fmt"
	"sort"
	"strings"
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
// to resolution. Today the only non-input layer is the MCP env.
type Sources struct {
	Env         Config
	ProjectRoot string
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
	Target  Config       `json:"target"`
	Service string       `json:"service,omitempty"`
	Layers  []Layer      `json:"layers,omitempty"`
	Explain []string     `json:"explain,omitempty"`
	Trace   []FieldTrace `json:"trace,omitempty"`
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
		name: "directUrl",
		get:  func(c Config) string { return c.DirectURL },
		set:  func(c *Config, v string) { c.DirectURL = v },
	},
	{
		name: "registryAddress",
		get:  func(c Config) string { return c.RegistryAddress },
		set:  func(c *Config, v string) { c.RegistryAddress = v },
	},
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

// Resolve merges the target layers in descending priority:
// input > MCP env > built-in defaults.
func Resolve(input Input, sources Sources) Report {
	layers := []layerConfig{
		{name: "input", cfg: configFromInput(input)},
		{name: "mcp-env", cfg: normalizeConfig(sources.Env)},
		{name: "defaults", cfg: defaultConfig()},
	}

	report := Report{Service: strings.TrimSpace(input.Service)}
	layerFields := make(map[string][]string, len(layers))

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
	report.Layers = buildLayers(layers, layerFields)
	if input.Explain {
		report.Explain = buildExplain(report.Trace)
	}
	return report
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
