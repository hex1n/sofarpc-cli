package projectscan

import (
	"archive/zip"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func MatchService(projectRoot, serviceFQCN string, modules []FacadeModule) (ServiceMatch, error) {
	serviceFQCN = strings.TrimSpace(serviceFQCN)
	if serviceFQCN == "" {
		return ServiceMatch{}, fmt.Errorf("[projectscan] service name is required")
	}
	var sourceMatches []ServiceMatch
	for _, module := range modules {
		if path, ok := matchServiceInSource(projectRoot, module, serviceFQCN); ok {
			sourceMatches = append(sourceMatches, ServiceMatch{
				Module:    module,
				MatchKind: "source",
				MatchPath: path,
			})
		}
	}
	if len(sourceMatches) > 1 {
		return ServiceMatch{}, ambiguousMatchError(serviceFQCN, sourceMatches)
	}
	if len(sourceMatches) == 1 {
		return sourceMatches[0], nil
	}

	var jarMatches []ServiceMatch
	for _, module := range modules {
		artifacts, err := DiscoverArtifacts(projectRoot, module)
		if err != nil {
			return ServiceMatch{}, err
		}
		for _, jarPath := range artifacts.PrimaryJars {
			ok, err := matchServiceInPrimaryJar(jarPath, serviceFQCN)
			if err != nil {
				return ServiceMatch{}, err
			}
			if ok {
				jarMatches = append(jarMatches, ServiceMatch{
					Module:    module,
					MatchKind: "primary-jar",
					MatchPath: jarPath,
				})
			}
		}
	}
	if len(jarMatches) > 1 {
		return ServiceMatch{}, ambiguousMatchError(serviceFQCN, jarMatches)
	}
	if len(jarMatches) == 1 {
		return jarMatches[0], nil
	}
	return ServiceMatch{}, fmt.Errorf("[projectscan] service %s not found in discovered modules", serviceFQCN)
}

func matchServiceInSource(projectRoot string, module FacadeModule, serviceFQCN string) (string, bool) {
	sourceRoot := resolvePath(projectRoot, module.SourceRoot)
	sourcePath := filepath.Join(sourceRoot, filepath.FromSlash(strings.ReplaceAll(serviceFQCN, ".", "/")+".java"))
	info, err := os.Stat(sourcePath)
	if err != nil || info.IsDir() {
		return "", false
	}
	abs, err := filepath.Abs(sourcePath)
	if err != nil {
		return filepath.Clean(sourcePath), true
	}
	return abs, true
}

func matchServiceInPrimaryJar(jarPath, serviceFQCN string) (bool, error) {
	reader, err := zip.OpenReader(jarPath)
	if err != nil {
		return false, err
	}
	defer reader.Close()

	target := classEntryForService(serviceFQCN)
	for _, file := range reader.File {
		if file.Name == target {
			return true, nil
		}
	}
	return false, nil
}

func classEntryForService(serviceFQCN string) string {
	return strings.ReplaceAll(serviceFQCN, ".", "/") + ".class"
}

func ambiguousMatchError(serviceFQCN string, matches []ServiceMatch) error {
	paths := make([]string, 0, len(matches))
	for _, match := range matches {
		paths = append(paths, match.MatchPath)
	}
	sort.Strings(paths)
	return fmt.Errorf("[projectscan] service %s matched multiple modules: %s", serviceFQCN, strings.Join(paths, ", "))
}
