package target

import (
	"fmt"
	"net"
	"net/url"
	"strings"
)

const (
	DefaultBoltPort     = "12200"
	DefaultRegistryPort = "2181"
)

// ParseDirectDialAddress canonicalizes a direct SOFARPC target into host:port.
// It accepts bolt://host[:port], host[:port], and bracketed IPv6 literals.
func ParseDirectDialAddress(raw string) (string, error) {
	return parseDialAddress(raw, DefaultBoltPort, "directUrl")
}

// ParseRegistryDialAddress canonicalizes a registry target into host:port.
// It accepts scheme://host[:port], host[:port], and bracketed IPv6 literals.
func ParseRegistryDialAddress(raw string) (string, error) {
	return parseDialAddress(raw, DefaultRegistryPort, "registryAddress")
}

func parseDialAddress(raw, defaultPort, field string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", fmt.Errorf("%s is empty", field)
	}

	host := raw
	if strings.Contains(raw, "://") {
		parsed, err := url.Parse(raw)
		if err != nil {
			return "", fmt.Errorf("parse %s: %w", field, err)
		}
		if parsed.Host == "" {
			return "", fmt.Errorf("%s has no host: %q", field, raw)
		}
		host = parsed.Host
	}

	return ensurePort(host, defaultPort), nil
}

func ensurePort(host, defaultPort string) string {
	host = strings.TrimSpace(host)
	if host == "" {
		return host
	}
	if strings.HasPrefix(host, "[") {
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
