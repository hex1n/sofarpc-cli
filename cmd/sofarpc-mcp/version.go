package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"runtime/debug"
	"strings"
)

var (
	// These values are overridden by release builds via -ldflags, for example:
	// -X main.version=v0.1.0 -X main.commit=$(git rev-parse --short HEAD)
	// -X main.date=$(date -u +%Y-%m-%dT%H:%M:%SZ)
	version = "dev"
	commit  = "unknown"
	date    = "unknown"
)

type versionInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	Commit  string `json:"commit"`
	Date    string `json:"date"`
}

func buildVersion() string {
	trimmed := strings.TrimSpace(version)
	if trimmed != "" && trimmed != "dev" {
		return trimmed
	}
	if info, ok := debug.ReadBuildInfo(); ok {
		mainVersion := strings.TrimSpace(info.Main.Version)
		if mainVersion != "" && mainVersion != "(devel)" {
			return mainVersion
		}
	}
	return "dev"
}

func currentVersionInfo() versionInfo {
	return versionInfo{
		Name:    "sofarpc-mcp",
		Version: buildVersion(),
		Commit:  normalizeBuildField(commit),
		Date:    normalizeBuildField(date),
	}
}

func normalizeBuildField(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "unknown"
	}
	return raw
}

func runVersion(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("version", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	jsonOutput := fs.Bool("json", false, "print version information as JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}
	info := currentVersionInfo()
	if *jsonOutput {
		body, err := json.MarshalIndent(info, "", "  ")
		if err != nil {
			return err
		}
		_, err = fmt.Fprintln(w, string(body))
		return err
	}
	_, err := fmt.Fprintf(w, "%s %s commit=%s date=%s\n", info.Name, info.Version, info.Commit, info.Date)
	return err
}
