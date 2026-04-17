package projectscan

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

var (
	facadeSuffixPattern = regexp.MustCompile(`(?i)(?:-|_)?(facade|api|client)(?:s)?$`)
	artifactPattern     = regexp.MustCompile(`(?i)<artifactId>\s*([^<\s]+?)\s*</artifactId>`)
	skipDirs            = map[string]struct{}{
		"target": {}, "build": {}, "node_modules": {}, ".git": {}, ".idea": {}, "dist": {}, "out": {},
	}
)

func DiscoverProject(startDir string) (ProjectLayout, error) {
	root, err := resolveProjectRoot(startDir)
	if err != nil {
		return ProjectLayout{}, err
	}
	return ProjectLayout{
		Root:          root,
		BuildTool:     detectBuildTool(root),
		FacadeModules: DetectFacadeModules(root),
	}, nil
}

func DetectFacadeModules(projectRoot string) []FacadeModule {
	cleanRoot := filepath.Clean(projectRoot)
	var found []FacadeModule
	_ = filepath.WalkDir(cleanRoot, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if _, skip := skipDirs[d.Name()]; skip {
				return filepath.SkipDir
			}
			if strings.HasPrefix(d.Name(), ".") && path != cleanRoot {
				return filepath.SkipDir
			}
			return nil
		}
		if d.Name() != "pom.xml" {
			return nil
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		artifact := FirstArtifactID(string(body))
		if artifact == "" || !facadeSuffixPattern.MatchString(artifact) {
			return nil
		}
		moduleDir := filepath.Dir(path)
		sourceRoot := filepath.Join(moduleDir, "src", "main", "java")
		if info, err := os.Stat(sourceRoot); err != nil || !info.IsDir() {
			return nil
		}
		moduleRel := relativeSlash(cleanRoot, moduleDir)
		found = append(found, FacadeModule{
			Name:            artifact,
			SourceRoot:      relativeSlash(cleanRoot, sourceRoot),
			MavenModulePath: moduleRel,
			JarGlob:         filepath.ToSlash(filepath.Join(moduleRel, "target", artifact+"-*.jar")),
			DepsDir:         filepath.ToSlash(filepath.Join(moduleRel, "target", "facade-deps")),
		})
		return nil
	})
	sort.Slice(found, func(i, j int) bool {
		if found[i].Name == found[j].Name {
			return found[i].MavenModulePath < found[j].MavenModulePath
		}
		return found[i].Name < found[j].Name
	})
	unique := make([]FacadeModule, 0, len(found))
	seen := map[string]struct{}{}
	for _, module := range found {
		key := module.Name + "\x00" + module.MavenModulePath
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		unique = append(unique, module)
	}
	return unique
}

func FirstArtifactID(pomText string) string {
	searchFrom := 0
	if idx := strings.Index(strings.ToLower(pomText), "</parent>"); idx >= 0 {
		searchFrom = idx + len("</parent>")
	}
	if match := artifactPattern.FindStringSubmatch(pomText[searchFrom:]); len(match) >= 2 {
		return match[1]
	}
	if match := artifactPattern.FindStringSubmatch(pomText); len(match) >= 2 {
		return match[1]
	}
	return ""
}

func resolveProjectRoot(startDir string) (string, error) {
	if strings.TrimSpace(startDir) == "" {
		wd, err := os.Getwd()
		if err != nil {
			return "", err
		}
		startDir = wd
	}
	absStart, err := filepath.Abs(startDir)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(absStart)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		absStart = filepath.Dir(absStart)
	}
	var nearestPom string
	for dir := absStart; ; dir = filepath.Dir(dir) {
		if nearestPom == "" {
			if _, err := os.Stat(filepath.Join(dir, "pom.xml")); err == nil {
				nearestPom = dir
			}
		}
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
	}
	if nearestPom != "" {
		return nearestPom, nil
	}
	return "", fmt.Errorf("[projectscan] unable to locate project root from %s", absStart)
}

func detectBuildTool(projectRoot string) string {
	if _, err := os.Stat(filepath.Join(projectRoot, "pom.xml")); err == nil {
		return "maven"
	}
	for _, name := range []string{"settings.gradle", "settings.gradle.kts", "build.gradle", "build.gradle.kts"} {
		if _, err := os.Stat(filepath.Join(projectRoot, name)); err == nil {
			return "gradle"
		}
	}
	return ""
}

func relativeSlash(projectRoot, path string) string {
	rel, err := filepath.Rel(projectRoot, path)
	if err != nil {
		return filepath.ToSlash(path)
	}
	return filepath.ToSlash(rel)
}
