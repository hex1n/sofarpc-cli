package sourcecontract

import (
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/hex1n/sofarpc-cli/internal/javamodel"
)

// scanProjectIndex walks projectRoot and records a lightweight
// FQN→path mapping. It intentionally does not parse fields or methods —
// that work is deferred to parseJavaFileClasses so Load stays cheap on
// large trees.
func scanProjectIndex(projectRoot string) (map[string]string, int, scanStats, error) {
	index := map[string]string{}
	files := 0
	stats := scanStats{
		indexFailures:    map[string]string{},
		duplicateClasses: map[string][]string{},
		skippedDirs:      map[string]int{},
		moduleRoots:      map[string]struct{}{},
		hintSet:          map[string]struct{}{},
	}
	err := filepath.WalkDir(projectRoot, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		name := d.Name()
		if d.IsDir() {
			if path != projectRoot && shouldSkipDir(name) {
				stats.skippedDirs[name]++
				return filepath.SkipDir
			}
			return nil
		}
		if shouldSkipFile(path, name) {
			return nil
		}
		files++
		stats.noteSourcePath(projectRoot, path)
		info, ok := parseJavaIndexFile(path)
		if info.hasLombok {
			stats.addHint("lombok annotations detected; generated fields/methods are not materialized by sourcecontract")
		}
		fqn := info.fqn
		if !ok || fqn == "" {
			stats.indexFailureCount++
			if len(stats.indexFailures) < 8 {
				stats.indexFailures[path] = "could not identify top-level class/interface/enum"
			}
			return nil
		}
		if first, exists := index[fqn]; exists {
			stats.recordDuplicate(fqn, first, path)
		} else {
			index[fqn] = path
		}
		return nil
	})
	if err != nil {
		return nil, 0, scanStats{}, err
	}
	if stats.generatedSourceFiles > 0 {
		stats.addHint("generated Java sources detected; sourcecontract indexes them as best-effort source inputs")
	}
	if len(stats.moduleRoots) > 1 {
		stats.addHint("multiple Java source roots detected; duplicate FQNs may depend on module ordering")
	}
	return index, files, stats, nil
}

type scanStats struct {
	indexFailures        map[string]string
	indexFailureCount    int
	duplicateClasses     map[string][]string
	skippedDirs          map[string]int
	generatedSourceFiles int
	moduleRoots          map[string]struct{}
	hintSet              map[string]struct{}
}

func (s *scanStats) recordDuplicate(fqn, firstPath, duplicatePath string) {
	if s.duplicateClasses == nil {
		s.duplicateClasses = map[string][]string{}
	}
	paths := s.duplicateClasses[fqn]
	if len(paths) == 0 {
		paths = append(paths, firstPath)
	}
	paths = append(paths, duplicatePath)
	s.duplicateClasses[fqn] = paths
	s.addHint("duplicate Java FQNs detected; sourcecontract keeps the first indexed file")
}

func (s *scanStats) noteSourcePath(projectRoot, path string) {
	normalized := filepath.ToSlash(path)
	if strings.Contains(normalized, "/generated-sources/") || strings.Contains(normalized, "/generated/") {
		s.generatedSourceFiles++
	}
	if root := javaSourceRoot(projectRoot, path); root != "" {
		s.moduleRoots[root] = struct{}{}
	}
}

func (s *scanStats) addHint(hint string) {
	hint = strings.TrimSpace(hint)
	if hint == "" {
		return
	}
	if s.hintSet == nil {
		s.hintSet = map[string]struct{}{}
	}
	s.hintSet[hint] = struct{}{}
}

func (s scanStats) hints() []string {
	out := make([]string, 0, len(s.hintSet))
	for hint := range s.hintSet {
		out = append(out, hint)
	}
	sort.Strings(out)
	return out
}

func javaSourceRoot(projectRoot, path string) string {
	rel, err := filepath.Rel(projectRoot, path)
	if err != nil {
		return ""
	}
	parts := strings.Split(filepath.ToSlash(rel), "/src/main/java/")
	if len(parts) != 2 {
		return ""
	}
	if parts[0] == "" {
		return projectRoot
	}
	return filepath.Join(projectRoot, filepath.FromSlash(parts[0]))
}

type javaIndexInfo struct {
	fqn       string
	hasLombok bool
}

func parseJavaIndexFile(path string) (javaIndexInfo, bool) {
	body, err := os.ReadFile(path)
	if err != nil {
		return javaIndexInfo{}, false
	}
	hasLombok := containsLombokAnnotation(string(body))
	clean := sanitizeJava(string(body))
	pkg := parsePackage(clean)
	decl, ok := parseTopLevelType(clean)
	if !ok || decl.simpleName == "" {
		return javaIndexInfo{hasLombok: hasLombok}, false
	}
	if pkg == "" {
		return javaIndexInfo{fqn: decl.simpleName, hasLombok: hasLombok}, true
	}
	return javaIndexInfo{fqn: pkg + "." + decl.simpleName, hasLombok: hasLombok}, true
}

func parseJavaFileClasses(path string, projectSymbols symbolTable) ([]javamodel.Class, []typeResolutionIssue, bool) {
	body, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, false
	}
	clean := sanitizeJava(string(body))
	pkg := parsePackage(clean)
	imports := parseImports(clean)
	decl, ok := parseTopLevelDecl(clean)
	if !ok || decl.simpleName == "" {
		return nil, nil, false
	}
	classes, issues := materializeClasses(path, pkg, imports, decl, projectSymbols)
	return classes, issues, true
}

func containsLombokAnnotation(src string) bool {
	for _, marker := range []string{
		"@Data", "@Getter", "@Setter", "@Value", "@Builder",
		"@SuperBuilder", "@AllArgsConstructor", "@NoArgsConstructor",
	} {
		if strings.Contains(src, marker) {
			return true
		}
	}
	return false
}

func shouldSkipDir(name string) bool {
	if strings.HasPrefix(name, ".") {
		return true
	}
	switch name {
	case "target", "build", "out", "bin", "dist", "node_modules", "vendor":
		return true
	default:
		return false
	}
}

func shouldSkipFile(path, name string) bool {
	if filepath.Ext(name) != ".java" {
		return true
	}
	if name == "package-info.java" || name == "module-info.java" {
		return true
	}
	lower := strings.ToLower(filepath.ToSlash(path))
	if strings.Contains(lower, "/src/test/") || strings.HasSuffix(name, "Test.java") {
		return true
	}
	return false
}
