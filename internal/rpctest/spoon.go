package rpctest

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const (
	skillName      = "call-rpc"
	indexerRelDir  = "indexer-java"
	indexerJarName = "call-rpc-spoon-indexer.jar"
)

func LoadSemanticRegistry(projectRoot string, sourceRoots, markers []string) (Registry, error) {
	canonicalProjectRoot := canonicalPath(projectRoot)
	canonicalSourceRoots := make([]string, 0, len(sourceRoots))
	for _, sourceRoot := range sourceRoots {
		canonicalSourceRoots = append(canonicalSourceRoots, canonicalPath(sourceRoot))
	}
	jarPath, err := ensureIndexerJar()
	if err != nil {
		return nil, err
	}
	javaBin, err := exec.LookPath("java")
	if err != nil {
		return nil, fmt.Errorf("missing dependency: java is required to run the Spoon indexer")
	}
	args := []string{"-jar", jarPath, "--project-root", canonicalProjectRoot}
	for _, sourceRoot := range canonicalSourceRoots {
		args = append(args, "--source-root", sourceRoot)
	}
	for _, marker := range markers {
		args = append(args, "--required-marker", marker)
	}
	cmd := exec.Command(javaBin, args...)
	cmd.Dir = canonicalProjectRoot
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		detail := strings.TrimSpace(stderr.String())
		if detail == "" {
			detail = strings.TrimSpace(stdout.String())
		}
		if detail != "" {
			return nil, fmt.Errorf("Spoon semantic analysis failed: %w: %s", err, detail)
		}
		return nil, fmt.Errorf("Spoon semantic analysis failed: %w", err)
	}

	var payload SemanticIndex
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		return nil, fmt.Errorf("invalid Spoon indexer output: %w", err)
	}
	registry := make(Registry, len(payload.Classes))
	for _, classInfo := range payload.Classes {
		registry[classInfo.FQN] = classInfo
	}
	return registry, nil
}

func canonicalPath(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		return filepath.Clean(path)
	}
	if real, err := filepath.EvalSymlinks(abs); err == nil {
		return real
	}
	return abs
}

func ensureIndexerJar() (string, error) {
	indexerDir, err := indexerModuleDir()
	if err != nil {
		return "", err
	}
	jarPath := filepath.Join(indexerDir, "target", indexerJarName)
	stale, err := jarIsStale(indexerDir, jarPath)
	if err != nil {
		return "", err
	}
	if !stale {
		return jarPath, nil
	}
	mvnBin, err := exec.LookPath("mvn")
	if err != nil {
		return "", fmt.Errorf("missing dependency: mvn is required to build the Spoon indexer")
	}
	cmd := exec.Command(mvnBin, "-q", "-DskipTests", "package")
	cmd.Dir = indexerDir
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		detail := strings.TrimSpace(stderr.String())
		if detail == "" {
			detail = strings.TrimSpace(stdout.String())
		}
		if detail != "" {
			return "", fmt.Errorf("failed to build Spoon indexer: %w: %s", err, detail)
		}
		return "", fmt.Errorf("failed to build Spoon indexer: %w", err)
	}
	if _, err := os.Stat(jarPath); err != nil {
		return "", fmt.Errorf("expected built jar missing: %s", jarPath)
	}
	return jarPath, nil
}

func jarIsStale(indexerDir, jarPath string) (bool, error) {
	jarInfo, err := os.Stat(jarPath)
	if err != nil {
		if os.IsNotExist(err) {
			return true, nil
		}
		return false, err
	}
	jarModTime := jarInfo.ModTime()
	stale := false
	err = filepath.WalkDir(indexerDir, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			if d.Name() == "target" {
				return filepath.SkipDir
			}
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if info.ModTime().After(jarModTime) {
			stale = true
		}
		return nil
	})
	if err != nil {
		return false, err
	}
	return stale, nil
}

func indexerModuleDir() (string, error) {
	skillsDir, err := bundledSkillsDir()
	if err != nil {
		return "", err
	}
	indexerDir := filepath.Join(skillsDir, skillName, indexerRelDir)
	info, err := os.Stat(indexerDir)
	if err != nil || !info.IsDir() {
		return "", fmt.Errorf("Spoon indexer module not found under %s", indexerDir)
	}
	return indexerDir, nil
}

func bundledSkillsDir() (string, error) {
	if home := strings.TrimSpace(os.Getenv("SOFARPC_HOME")); home != "" {
		cand := filepath.Join(home, "skills")
		if info, err := os.Stat(cand); err == nil && info.IsDir() {
			return cand, nil
		}
	}
	exe, err := os.Executable()
	if err == nil {
		exe, _ = filepath.EvalSymlinks(exe)
		installRoot := filepath.Dir(filepath.Dir(exe))
		cand := filepath.Join(installRoot, "skills")
		if info, err := os.Stat(cand); err == nil && info.IsDir() {
			return cand, nil
		}
	}
	return "", fmt.Errorf("cannot locate bundled skills/ directory — set SOFARPC_HOME to the CLI install root")
}
