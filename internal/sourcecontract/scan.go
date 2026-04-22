package sourcecontract

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/hex1n/sofarpc-cli/internal/javamodel"
)

// scanProjectIndex walks projectRoot and records a lightweight
// FQN→path mapping. It intentionally does not parse fields or methods —
// that work is deferred to parseJavaFileClasses so Load stays cheap on
// large trees.
func scanProjectIndex(projectRoot string) (map[string]string, int, map[string]string, error) {
	index := map[string]string{}
	files := 0
	failures := map[string]string{}
	err := filepath.WalkDir(projectRoot, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		name := d.Name()
		if d.IsDir() {
			if shouldSkipDir(name) {
				return filepath.SkipDir
			}
			return nil
		}
		if shouldSkipFile(path, name) {
			return nil
		}
		files++
		fqn, ok := parseJavaIndexFile(path)
		if !ok || fqn == "" {
			if len(failures) < 8 {
				failures[path] = "could not identify top-level class/interface/enum"
			}
			return nil
		}
		if _, exists := index[fqn]; !exists {
			index[fqn] = path
		}
		return nil
	})
	if err != nil {
		return nil, 0, nil, err
	}
	return index, files, failures, nil
}

func parseJavaIndexFile(path string) (string, bool) {
	body, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}
	clean := sanitizeJava(string(body))
	pkg := parsePackage(clean)
	decl, ok := parseTopLevelType(clean)
	if !ok || decl.simpleName == "" {
		return "", false
	}
	if pkg == "" {
		return decl.simpleName, true
	}
	return pkg + "." + decl.simpleName, true
}

func parseJavaFileClasses(path string) ([]javamodel.Class, bool) {
	body, err := os.ReadFile(path)
	if err != nil {
		return nil, false
	}
	clean := sanitizeJava(string(body))
	pkg := parsePackage(clean)
	imports := parseImports(clean)
	decl, ok := parseTopLevelDecl(clean)
	if !ok || decl.simpleName == "" {
		return nil, false
	}
	return materializeClasses(path, pkg, imports, decl), true
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
