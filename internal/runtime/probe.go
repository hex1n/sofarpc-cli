package runtime

import (
	"fmt"
	"net"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/hex1n/sofarpc-cli/internal/model"
)

func ProbeTarget(target model.TargetConfig) model.ProbeResult {
	endpoint := configuredTarget(target)
	if endpoint == "" {
		return model.ProbeResult{Reachable: false, Message: "no direct or registry target configured"}
	}
	address, err := dialAddress(target)
	if err != nil {
		return model.ProbeResult{Reachable: false, Target: endpoint, Message: err.Error()}
	}
	timeout := time.Duration(max(target.ConnectTimeoutMS, defaultConnectMS)) * time.Millisecond
	conn, err := net.DialTimeout("tcp", address, timeout)
	if err != nil {
		return model.ProbeResult{Reachable: false, Target: endpoint, Message: err.Error()}
	}
	_ = conn.Close()
	return model.ProbeResult{Reachable: true, Target: endpoint, Message: "tcp probe succeeded"}
}

func ScanStubWarnings(stubPaths []string) []string {
	patterns := []*regexp.Regexp{
		regexp.MustCompile(`(?i)guava-`),
		regexp.MustCompile(`(?i)logback-`),
		regexp.MustCompile(`(?i)slf4j-`),
		regexp.MustCompile(`(?i)jackson-`),
		regexp.MustCompile(`(?i)spring-boot`),
	}
	var warnings []string
	for _, path := range stubPaths {
		base := filepath.Base(path)
		for _, pattern := range patterns {
			if pattern.MatchString(base) {
				warnings = append(warnings, fmt.Sprintf("high-risk classpath entry: %s", base))
				break
			}
		}
	}
	return warnings
}

func configuredTarget(target model.TargetConfig) string {
	switch target.Mode {
	case model.ModeDirect:
		return target.DirectURL
	case model.ModeRegistry:
		return target.RegistryProtocol + "://" + target.RegistryAddress
	default:
		return ""
	}
}

func dialAddress(target model.TargetConfig) (string, error) {
	raw := configuredTarget(target)
	parsed, err := url.Parse(raw)
	if err == nil && parsed.Host != "" {
		return parsed.Host, nil
	}
	if strings.Contains(raw, "://") {
		raw = strings.SplitN(raw, "://", 2)[1]
	}
	if strings.TrimSpace(raw) == "" {
		return "", fmt.Errorf("target address is empty")
	}
	return raw, nil
}
