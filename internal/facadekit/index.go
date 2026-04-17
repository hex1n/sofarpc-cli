package facadekit

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type ServiceIndexFile struct {
	Service string               `json:"service"`
	File    string               `json:"file,omitempty"`
	Methods []MethodSchemaResult `json:"methods"`
}

type IndexSummary struct {
	SourceRoots       []string              `json:"sourceRoots"`
	InterfaceSuffixes []string              `json:"interfaceSuffixes"`
	Services          []IndexSummaryService `json:"services"`
}

type IndexSummaryService struct {
	Service string   `json:"service"`
	File    string   `json:"file,omitempty"`
	Methods []string `json:"methods"`
}

func RefreshIndex(projectRoot string, cfg Config, stdout, stderr io.Writer) error {
	sourceRoots := IterSourceRoots(cfg, projectRoot)
	if len(sourceRoots) == 0 {
		return fmt.Errorf("no facade source roots in config")
	}
	for _, sourceRoot := range sourceRoots {
		if _, err := os.Stat(sourceRoot); err != nil && os.IsNotExist(err) {
			if stderr != nil {
				fmt.Fprintf(stderr, "[index] WARNING source root does not exist: %s\n", sourceRoot)
			}
		}
	}
	registry, err := LoadSemanticRegistry(projectRoot, sourceRoots, cfg.RequiredMarkers)
	if err != nil {
		return err
	}
	indexDir := EffectiveIndexDir(projectRoot)
	return WriteIndexFiles(indexDir, projectRoot, cfg, registry, stdout)
}

func WriteIndexFiles(indexDir, projectRoot string, cfg Config, registry Registry, stdout io.Writer) error {
	parentDir := filepath.Dir(indexDir)
	if err := os.MkdirAll(parentDir, 0o755); err != nil {
		return err
	}
	tmpIndexDir, err := os.MkdirTemp(parentDir, ".sofarpc-index-")
	if err != nil {
		return err
	}
	tmpSummary, err := buildIndexFiles(tmpIndexDir, projectRoot, cfg, registry, stdout)
	if err != nil {
		_ = os.RemoveAll(tmpIndexDir)
		return err
	}
	if err := switchIndexDir(tmpIndexDir, indexDir); err != nil {
		_ = os.RemoveAll(tmpIndexDir)
		return err
	}
	if stdout != nil {
		fmt.Fprintf(stdout, "\n[index] wrote %d services to %s\n", len(tmpSummary.Services), displayPath(projectRoot, indexDir))
	}
	return nil
}

func switchIndexDir(next, current string) error {
	backupDir := filepath.Join(filepath.Dir(current), fmt.Sprintf(".sofarpc-index-old-%d", time.Now().UnixNano()))
	currentExists := false
	if _, err := os.Stat(current); err == nil {
		if err := os.Rename(current, backupDir); err != nil {
			return err
		}
		currentExists = true
	} else if err != nil && !os.IsNotExist(err) {
		return err
	}

	if err := os.Rename(next, current); err != nil {
		if currentExists {
			_ = os.Rename(backupDir, current)
		}
		return err
	}

	if currentExists {
		_ = os.RemoveAll(backupDir)
	}
	return nil
}

func buildIndexFiles(indexDir, projectRoot string, cfg Config, registry Registry, stdout io.Writer) (IndexSummary, error) {
	if err := os.MkdirAll(indexDir, 0o755); err != nil {
		return IndexSummary{}, err
	}

	summary := IndexSummary{
		SourceRoots:       make([]string, 0, len(cfg.FacadeModules)),
		InterfaceSuffixes: append([]string{}, cfg.InterfaceSuffixes...),
		Services:          []IndexSummaryService{},
	}
	for _, sourceRoot := range IterSourceRoots(cfg, projectRoot) {
		rel, err := filepath.Rel(projectRoot, sourceRoot)
		if err != nil {
			rel = sourceRoot
		}
		summary.SourceRoots = append(summary.SourceRoots, filepath.ToSlash(rel))
	}

	var serviceNames []string
	for fqn := range registry {
		serviceNames = append(serviceNames, fqn)
	}
	sort.Strings(serviceNames)
	for _, fqn := range serviceNames {
		classInfo := registry[fqn]
		if !IsFacadeInterface(classInfo, cfg.InterfaceSuffixes) {
			continue
		}
		payload := ServiceIndexFile{
			Service: classInfo.FQN,
			File:    classInfo.File,
			Methods: make([]MethodSchemaResult, 0, len(classInfo.Methods)),
		}
		methodNames := make([]string, 0, len(classInfo.Methods))
		for _, method := range classInfo.Methods {
			payload.Methods = append(payload.Methods, buildMethodSchemaResult(registry, classInfo, method, cfg.RequiredMarkers))
			methodNames = append(methodNames, method.Name)
		}
		if err := SaveJSON(filepath.Join(indexDir, classInfo.FQN+".json"), payload); err != nil {
			return IndexSummary{}, err
		}
		summary.Services = append(summary.Services, IndexSummaryService{
			Service: classInfo.FQN,
			File:    classInfo.File,
			Methods: methodNames,
		})
		if stdout != nil {
			fmt.Fprintf(stdout, "  + %s  (%d methods)\n", classInfo.FQN, len(payload.Methods))
		}
	}

	if err := SaveJSON(filepath.Join(indexDir, "_index.json"), summary); err != nil {
		return IndexSummary{}, err
	}
	return summary, nil
}

func IsFacadeInterface(classInfo SemanticClassInfo, suffixes []string) bool {
	if classInfo.Kind != "interface" {
		return false
	}
	for _, suffix := range suffixes {
		if strings.HasSuffix(classInfo.SimpleName, suffix) {
			return true
		}
	}
	return false
}

func displayPath(projectRoot, path string) string {
	rel, err := filepath.Rel(projectRoot, path)
	if err != nil {
		return path
	}
	return filepath.ToSlash(rel)
}
