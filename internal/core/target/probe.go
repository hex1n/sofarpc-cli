package target

import (
	"fmt"
	"net"
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
		return ParseDirectDialAddress(cfg.DirectURL)
	case ModeRegistry:
		return ParseRegistryDialAddress(cfg.RegistryAddress)
	case "":
		return "", fmt.Errorf("target mode not resolved")
	default:
		return "", fmt.Errorf("unknown target mode %q", cfg.Mode)
	}
}
