package target

import (
	"fmt"
	"net"
	"net/url"
	"strings"
	"time"
)

// ProbeResult reports whether the resolved target answered a TCP dial.
type ProbeResult struct {
	Reachable bool   `json:"reachable"`
	Target    string `json:"target,omitempty"`
	Message   string `json:"message,omitempty"`
}

// Probe dials the target's host:port with the configured connect timeout.
// It does NOT speak SOFARPC — it only verifies that something is
// listening. A reachable=false with a specific message is how we surface
// problems to the agent (address parse errors, DNS, connect refused).
//
// A default is applied when ConnectTimeoutMS is 0 so the probe never
// hangs on zero-valued configs.
func Probe(cfg Config) ProbeResult {
	cfg = inferProbeMode(cfg)
	dialTarget, err := dialTarget(cfg)
	if err != nil {
		return ProbeResult{Message: err.Error()}
	}
	timeout := time.Duration(cfg.ConnectTimeoutMS) * time.Millisecond
	if timeout <= 0 {
		timeout = time.Second
	}
	conn, err := net.DialTimeout("tcp", dialTarget, timeout)
	if err != nil {
		return ProbeResult{Target: dialTarget, Message: err.Error()}
	}
	_ = conn.Close()
	return ProbeResult{Reachable: true, Target: dialTarget}
}

// inferProbeMode fills in Mode when the caller supplied only an address.
// A pre-set Mode is preserved verbatim so the "unknown mode" and explicit
// empty-address paths still surface their specific errors.
func inferProbeMode(cfg Config) Config {
	if cfg.Mode != "" {
		return cfg
	}
	switch {
	case strings.TrimSpace(cfg.DirectURL) != "":
		cfg.Mode = ModeDirect
	case strings.TrimSpace(cfg.RegistryAddress) != "":
		cfg.Mode = ModeRegistry
	}
	return cfg
}

func dialTarget(cfg Config) (string, error) {
	switch cfg.Mode {
	case ModeDirect:
		return parseBoltAddress(cfg.DirectURL)
	case ModeRegistry:
		return parseRegistryAddress(cfg.RegistryAddress)
	case "":
		return "", fmt.Errorf("target mode not resolved")
	default:
		return "", fmt.Errorf("unknown target mode %q", cfg.Mode)
	}
}

// parseBoltAddress accepts `bolt://host:port`, `host:port`, `host` and
// returns `host:port` (defaulting the port to 12200 — SOFARPC's well-known
// default — when missing).
func parseBoltAddress(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", fmt.Errorf("directUrl is empty")
	}
	if strings.Contains(raw, "://") {
		parsed, err := url.Parse(raw)
		if err != nil {
			return "", fmt.Errorf("parse directUrl: %w", err)
		}
		if parsed.Host == "" {
			return "", fmt.Errorf("directUrl has no host: %q", raw)
		}
		return ensurePort(parsed.Host, "12200"), nil
	}
	return ensurePort(raw, "12200"), nil
}

func parseRegistryAddress(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", fmt.Errorf("registryAddress is empty")
	}
	if strings.Contains(raw, "://") {
		parsed, err := url.Parse(raw)
		if err != nil {
			return "", fmt.Errorf("parse registryAddress: %w", err)
		}
		if parsed.Host == "" {
			return "", fmt.Errorf("registryAddress has no host: %q", raw)
		}
		return ensurePort(parsed.Host, "2181"), nil
	}
	return ensurePort(raw, "2181"), nil
}

// ensurePort attaches defaultPort when host has no explicit port. It
// handles IPv6 literals via net.SplitHostPort heuristics.
func ensurePort(host, defaultPort string) string {
	if host == "" {
		return host
	}
	if strings.HasPrefix(host, "[") {
		// IPv6 literal — trust the caller, but add port if missing.
		if strings.Contains(host, "]:") {
			return host
		}
		return host + ":" + defaultPort
	}
	if _, _, err := net.SplitHostPort(host); err == nil {
		return host
	}
	return host + ":" + defaultPort
}
