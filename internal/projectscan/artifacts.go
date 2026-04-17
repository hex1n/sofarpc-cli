package projectscan

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func DiscoverArtifacts(projectRoot string, module FacadeModule) (ArtifactSet, error) {
	primary, err := discoverPrimaryJars(projectRoot, module)
	if err != nil {
		return ArtifactSet{}, err
	}
	deps, err := discoverDependencyJars(projectRoot, module)
	if err != nil {
		return ArtifactSet{}, err
	}
	return ArtifactSet{
		PrimaryJars:    primary,
		DependencyJars: deps,
	}, nil
}

func discoverPrimaryJars(projectRoot string, module FacadeModule) ([]string, error) {
	patterns := []string{}
	if strings.TrimSpace(module.JarGlob) != "" {
		patterns = append(patterns, resolvePattern(projectRoot, module.JarGlob))
	}
	if strings.TrimSpace(module.MavenModulePath) != "" && strings.TrimSpace(module.Name) != "" {
		moduleRoot := resolvePath(projectRoot, module.MavenModulePath)
		patterns = append(patterns,
			filepath.Join(moduleRoot, "target", module.Name+"-*.jar"),
			filepath.Join(moduleRoot, "build", "libs", "*.jar"),
		)
	}
	var found []string
	for _, pattern := range patterns {
		matches, err := filepath.Glob(pattern)
		if err != nil {
			return nil, err
		}
		for _, match := range matches {
			if isJarFile(match) {
				found = append(found, match)
			}
		}
	}
	return sortAndDedupe(found), nil
}

func discoverDependencyJars(projectRoot string, module FacadeModule) ([]string, error) {
	dirs := []string{}
	if strings.TrimSpace(module.DepsDir) != "" {
		dirs = append(dirs, resolvePath(projectRoot, module.DepsDir))
	}
	if strings.TrimSpace(module.MavenModulePath) != "" {
		moduleRoot := resolvePath(projectRoot, module.MavenModulePath)
		dirs = append(dirs,
			filepath.Join(moduleRoot, "target", "facade-deps"),
			filepath.Join(moduleRoot, "target", "dependency"),
		)
	}
	var found []string
	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, err
		}
		for _, entry := range entries {
			fullPath := filepath.Join(dir, entry.Name())
			if entry.IsDir() || !isJarFile(fullPath) {
				continue
			}
			found = append(found, fullPath)
		}
	}
	return sortAndDedupe(found), nil
}

func sortAndDedupe(paths []string) []string {
	if len(paths) == 0 {
		return nil
	}
	seen := map[string]struct{}{}
	unique := make([]string, 0, len(paths))
	for _, path := range paths {
		if strings.TrimSpace(path) == "" {
			continue
		}
		abs, err := filepath.Abs(path)
		if err != nil {
			abs = filepath.Clean(path)
		}
		if _, ok := seen[abs]; ok {
			continue
		}
		seen[abs] = struct{}{}
		unique = append(unique, abs)
	}
	sort.Strings(unique)
	return unique
}

func resolvePattern(projectRoot, pattern string) string {
	if filepath.IsAbs(pattern) {
		return filepath.Clean(pattern)
	}
	return filepath.Join(projectRoot, filepath.FromSlash(pattern))
}

func resolvePath(projectRoot, path string) string {
	if filepath.IsAbs(path) {
		return filepath.Clean(path)
	}
	return filepath.Join(projectRoot, filepath.FromSlash(path))
}

func isJarFile(path string) bool {
	if strings.TrimSpace(path) == "" {
		return false
	}
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return false
	}
	return strings.EqualFold(filepath.Ext(path), ".jar")
}
