package runtime

import (
	"fmt"
	"net"
	"net/url"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/hex1n/sofarpc-cli/internal/model"
	"github.com/hex1n/sofarpc-cli/internal/targetmodel"
)

func ProbeTarget(target targetmodel.TargetConfig) model.ProbeResult {
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
	patterns := map[string]*regexp.Regexp{
		"guava":       regexp.MustCompile(`(?i)guava-`),
		"logback":     regexp.MustCompile(`(?i)logback-`),
		"slf4j":       regexp.MustCompile(`(?i)slf4j-`),
		"jackson":     regexp.MustCompile(`(?i)jackson-`),
		"spring-boot": regexp.MustCompile(`(?i)spring-boot`),
	}
	total := 0
	families := map[string]struct{}{}
	for _, path := range stubPaths {
		base := filepath.Base(path)
		for family, pattern := range patterns {
			if pattern.MatchString(base) {
				total++
				families[family] = struct{}{}
				break
			}
		}
	}
	if total == 0 {
		return nil
	}
	names := make([]string, 0, len(families))
	for family := range families {
		names = append(names, family)
	}
	sort.Strings(names)
	return []string{
		fmt.Sprintf("high-risk classpath entries detected (%d across %s)", total, strings.Join(names, ", ")),
	}
}

func configuredTarget(target targetmodel.TargetConfig) string {
	switch target.Mode {
	case targetmodel.ModeDirect:
		return target.DirectURL
	case targetmodel.ModeRegistry:
		return target.RegistryProtocol + "://" + target.RegistryAddress
	default:
		return ""
	}
}

func dialAddress(target targetmodel.TargetConfig) (string, error) {
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
