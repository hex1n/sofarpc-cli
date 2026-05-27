package workspace

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const (
	DiscoveryConfidenceHigh   = "high"
	DiscoveryConfidenceMedium = "medium"
	DiscoveryConfidenceLow    = "low"
)

type JavaProjectDiscovery struct {
	Root          string                 `json:"root,omitempty"`
	Source        string                 `json:"source,omitempty"`
	Confidence    string                 `json:"confidence,omitempty"`
	Reason        string                 `json:"reason,omitempty"`
	Markers       []string               `json:"markers,omitempty"`
	ScanTruncated bool                   `json:"scanTruncated,omitempty"`
	VisitedCount  int                    `json:"visitedCount,omitempty"`
	Candidates    []JavaProjectCandidate `json:"candidates,omitempty"`
}

type JavaProjectCandidate struct {
	Root          string   `json:"root,omitempty"`
	Confidence    string   `json:"confidence,omitempty"`
	Reason        string   `json:"reason,omitempty"`
	Markers       []string `json:"markers,omitempty"`
	ScanTruncated bool     `json:"scanTruncated,omitempty"`
	VisitedCount  int      `json:"visitedCount,omitempty"`
}

// DiscoverJavaProject inspects cwd and its ancestors for Java project markers.
// It returns a writable project root only when exactly one high-confidence
// candidate exists; ambiguous or weak evidence is surfaced as candidates.
func DiscoverJavaProject(cwd string) (JavaProjectDiscovery, error) {
	start, err := discoverStart(cwd)
	if err != nil {
		return JavaProjectDiscovery{}, err
	}
	gitRoot := findGitRoot(start)
	candidates, err := discoverCandidates(start, gitRoot)
	if err != nil {
		return JavaProjectDiscovery{}, err
	}
	out := JavaProjectDiscovery{
		Source:     "auto-discovery",
		Confidence: DiscoveryConfidenceLow,
		Candidates: candidates,
	}
	if len(candidates) == 0 {
		out.Reason = "no Java project markers found"
		return out, nil
	}
	var high []JavaProjectCandidate
	for _, candidate := range candidates {
		if candidate.Confidence == DiscoveryConfidenceHigh {
			high = append(high, candidate)
		}
	}
	switch len(high) {
	case 1:
		out.Root = high[0].Root
		out.Confidence = DiscoveryConfidenceHigh
		out.Reason = high[0].Reason
		out.Markers = high[0].Markers
		out.ScanTruncated = high[0].ScanTruncated
		out.VisitedCount = high[0].VisitedCount
	case 0:
		out.Reason = "no high-confidence Java project candidate found"
	default:
		out.Reason = "multiple high-confidence Java project candidates found"
	}
	return out, nil
}

func discoverStart(cwd string) (string, error) {
	candidate := strings.TrimSpace(cwd)
	if candidate == "" {
		wd, err := os.Getwd()
		if err != nil {
			return "", fmt.Errorf("getwd: %w", err)
		}
		candidate = wd
	}
	abs, err := filepath.Abs(candidate)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return "", fmt.Errorf("stat discovery cwd %q: %w", candidate, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("discovery cwd %q is not a directory", candidate)
	}
	return filepath.Clean(abs), nil
}

func discoverCandidates(start, gitRoot string) ([]JavaProjectCandidate, error) {
	var out []JavaProjectCandidate
	for dir := start; ; dir = filepath.Dir(dir) {
		candidate, ok, err := evaluateJavaProjectCandidate(dir, gitRoot)
		if err != nil {
			return nil, err
		}
		if ok {
			out = append(out, candidate)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
	}
	return out, nil
}

func evaluateJavaProjectCandidate(root, gitRoot string) (JavaProjectCandidate, bool, error) {
	markers := map[string]struct{}{}
	addMarker := func(marker string) {
		if strings.TrimSpace(marker) != "" {
			markers[marker] = struct{}{}
		}
	}
	if hasSofaProjectConfig(root) {
		addMarker(".sofarpc/config")
	}
	for _, marker := range buildMarkers(root) {
		addMarker(marker)
	}
	if dirExists(filepath.Join(root, "src", "main", "java")) {
		addMarker("src/main/java")
	}
	if hasNestedJavaSourceRoot(root) {
		addMarker("nested-src/main/java")
	}
	hasSource, visitedCount, scanTruncated := hasJavaSource(root)
	if hasSource {
		addMarker("java-source")
	}
	if sameCleanPath(root, gitRoot) {
		addMarker(".git")
	} else if gitRoot != "" && pathWithin(root, gitRoot) {
		addMarker("git-ancestor")
	}
	markerList := sortedMarkers(markers)
	if !hasAnyProjectMarker(markers) {
		return JavaProjectCandidate{}, false, nil
	}
	confidence, reason := candidateConfidence(markers)
	return JavaProjectCandidate{
		Root:          root,
		Confidence:    confidence,
		Reason:        reason,
		Markers:       markerList,
		ScanTruncated: scanTruncated,
		VisitedCount:  visitedCount,
	}, true, nil
}

func hasSofaProjectConfig(root string) bool {
	return fileExists(filepath.Join(root, ".sofarpc", "config.local.json")) ||
		fileExists(filepath.Join(root, ".sofarpc", "config.json"))
}

func buildMarkers(root string) []string {
	var out []string
	for _, name := range []string{
		"pom.xml",
		"build.gradle",
		"build.gradle.kts",
		"settings.gradle",
		"settings.gradle.kts",
	} {
		if fileExists(filepath.Join(root, name)) {
			out = append(out, name)
		}
	}
	return out
}

func hasNestedJavaSourceRoot(root string) bool {
	for _, pattern := range []string{
		filepath.Join(root, "*", "src", "main", "java"),
		filepath.Join(root, "*", "*", "src", "main", "java"),
	} {
		matches, err := filepath.Glob(pattern)
		if err != nil {
			continue
		}
		for _, match := range matches {
			if dirExists(match) {
				return true
			}
		}
	}
	return false
}

func hasJavaSource(root string) (bool, int, bool) {
	found := false
	visited := 0
	truncated := false
	const maxVisited = 2000
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || found {
			return filepath.SkipDir
		}
		if path != root && d.IsDir() && shouldSkipDiscoveryDir(d.Name()) {
			return filepath.SkipDir
		}
		visited++
		if visited > maxVisited {
			truncated = true
			return filepath.SkipAll
		}
		if !d.IsDir() && strings.EqualFold(filepath.Ext(path), ".java") {
			found = true
			return filepath.SkipAll
		}
		return nil
	})
	return found, visited, truncated
}

func shouldSkipDiscoveryDir(name string) bool {
	switch name {
	case ".git", ".gradle", ".idea", ".mvn", "build", "target", "out", "node_modules":
		return true
	default:
		return strings.HasPrefix(name, ".")
	}
}

func findGitRoot(start string) string {
	for dir := start; ; dir = filepath.Dir(dir) {
		if fileExists(filepath.Join(dir, ".git")) || dirExists(filepath.Join(dir, ".git")) {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
	}
}

func candidateConfidence(markers map[string]struct{}) (string, string) {
	if hasMarker(markers, ".sofarpc/config") {
		return DiscoveryConfidenceHigh, "existing .sofarpc project config found"
	}
	hasBuild := hasAnyMarker(markers, "pom.xml", "build.gradle", "build.gradle.kts", "settings.gradle", "settings.gradle.kts")
	hasSource := hasAnyMarker(markers, "src/main/java", "nested-src/main/java", "java-source")
	inGit := hasAnyMarker(markers, ".git", "git-ancestor")
	switch {
	case hasBuild && hasSource && inGit:
		return DiscoveryConfidenceHigh, "Java build and source markers found inside a git worktree"
	case hasBuild && hasSource:
		return DiscoveryConfidenceMedium, "Java build and source markers found"
	case hasBuild && inGit:
		return DiscoveryConfidenceMedium, "Java build marker found inside a git worktree"
	case hasSource && inGit:
		return DiscoveryConfidenceMedium, "Java source marker found inside a git worktree"
	default:
		return DiscoveryConfidenceLow, "only weak Java project markers found"
	}
}

func hasAnyProjectMarker(markers map[string]struct{}) bool {
	return hasMarker(markers, ".sofarpc/config") ||
		hasAnyMarker(markers, "pom.xml", "build.gradle", "build.gradle.kts", "settings.gradle", "settings.gradle.kts", "src/main/java", "nested-src/main/java", "java-source")
}

func hasAnyMarker(markers map[string]struct{}, names ...string) bool {
	for _, name := range names {
		if hasMarker(markers, name) {
			return true
		}
	}
	return false
}

func hasMarker(markers map[string]struct{}, name string) bool {
	_, ok := markers[name]
	return ok
}

func sortedMarkers(markers map[string]struct{}) []string {
	out := make([]string, 0, len(markers))
	for marker := range markers {
		out = append(out, marker)
	}
	sort.Strings(out)
	return out
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func sameCleanPath(left, right string) bool {
	left = filepath.Clean(strings.TrimSpace(left))
	right = filepath.Clean(strings.TrimSpace(right))
	return left != "" && right != "" && strings.EqualFold(left, right)
}

func pathWithin(path, root string) bool {
	path = filepath.Clean(path)
	root = filepath.Clean(root)
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator)) && !filepath.IsAbs(rel))
}
