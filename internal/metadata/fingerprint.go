package metadata

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/hex1n/sofarpc-cli/internal/projectscan"
)

var contractFingerprintFn = contractFingerprint

func contractFingerprint(projectRoot, service string) (string, error) {
	projectRoot = strings.TrimSpace(projectRoot)
	service = strings.TrimSpace(service)
	if projectRoot == "" || service == "" {
		return "", fmt.Errorf("project root and service are required")
	}

	layout, err := projectscan.DiscoverProject(projectRoot)
	if err != nil {
		return "", err
	}
	if len(layout.FacadeModules) == 0 {
		return "", fmt.Errorf("no facade modules discovered under %s", layout.Root)
	}

	modules := layout.FacadeModules
	if match, err := projectscan.MatchService(layout.Root, service, layout.FacadeModules); err == nil {
		modules = []projectscan.FacadeModule{match.Module}
	}

	var parts []string
	roots := sourceRootsForModules(layout.Root, modules)
	if len(roots) > 0 {
		digest, err := fingerprintSourceRoots(roots)
		if err != nil {
			return "", err
		}
		parts = append(parts, "source", digest)
	}
	artifacts, err := artifactPaths(layout.Root, modules)
	if err != nil {
		return "", err
	}
	if len(artifacts) > 0 {
		digest, err := fingerprintFiles(artifacts)
		if err != nil {
			return "", err
		}
		parts = append(parts, "artifacts", digest)
	}
	if len(parts) == 0 {
		return "", fmt.Errorf("no source roots or artifacts available for %s", service)
	}
	return "combined:" + fingerprintStrings(parts), nil
}

func sourceRootsForModules(projectRoot string, modules []projectscan.FacadeModule) []string {
	seen := map[string]struct{}{}
	roots := make([]string, 0, len(modules))
	for _, module := range modules {
		if strings.TrimSpace(module.SourceRoot) == "" {
			continue
		}
		root := resolveRepoPath(module.SourceRoot, projectRoot)
		if _, ok := seen[root]; ok {
			continue
		}
		seen[root] = struct{}{}
		roots = append(roots, root)
	}
	sort.Strings(roots)
	return roots
}

func artifactPaths(projectRoot string, modules []projectscan.FacadeModule) ([]string, error) {
	seen := map[string]struct{}{}
	paths := make([]string, 0, len(modules))
	for _, module := range modules {
		artifacts, err := projectscan.DiscoverArtifacts(projectRoot, module)
		if err != nil {
			return nil, err
		}
		for _, path := range append(artifacts.PrimaryJars, artifacts.DependencyJars...) {
			clean := filepath.Clean(path)
			if _, ok := seen[clean]; ok {
				continue
			}
			seen[clean] = struct{}{}
			paths = append(paths, clean)
		}
	}
	sort.Strings(paths)
	return paths, nil
}

func fingerprintSourceRoots(roots []string) (string, error) {
	files := make([]string, 0)
	for _, root := range roots {
		err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				return nil
			}
			if !strings.EqualFold(filepath.Ext(path), ".java") {
				return nil
			}
			files = append(files, filepath.Clean(path))
			return nil
		})
		if err != nil {
			return "", err
		}
	}
	sort.Strings(files)
	return fingerprintFiles(files)
}

func fingerprintFiles(paths []string) (string, error) {
	hash := sha256.New()
	for _, path := range paths {
		info, err := os.Stat(path)
		if err != nil {
			return "", err
		}
		line := fmt.Sprintf("%s|%d|%d\n", filepath.Clean(path), info.Size(), info.ModTime().UnixNano())
		if _, err := hash.Write([]byte(line)); err != nil {
			return "", err
		}
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func fingerprintStrings(parts []string) string {
	hash := sha256.New()
	for _, part := range parts {
		_, _ = hash.Write([]byte(part))
		_, _ = hash.Write([]byte{'\n'})
	}
	return hex.EncodeToString(hash.Sum(nil))
}

func resolveRepoPath(pathFromRepo, projectRoot string) string {
	if filepath.IsAbs(pathFromRepo) {
		return filepath.Clean(pathFromRepo)
	}
	return filepath.Clean(filepath.Join(projectRoot, filepath.FromSlash(pathFromRepo)))
}
