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
	indexerRelDir  = "spoon-indexer-java"
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
		return nil, commandFailureError("Spoon semantic analysis failed", cmd, stdout.Bytes(), stderr.Bytes(), err)
	}

	var payload SemanticIndex
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		return nil, fmt.Errorf("invalid Spoon indexer output from %q: %w", commandString(cmd), err)
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
		return "", commandFailureError("failed to build Spoon indexer", cmd, stdout.Bytes(), stderr.Bytes(), err)
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
	paths := []string{}
	if home := strings.TrimSpace(os.Getenv("SOFARPC_HOME")); home != "" {
		paths = append(paths, filepath.Join(home, indexerRelDir))
		paths = append(paths, filepath.Join(home, "skills", "call-rpc", indexerRelDir))
	}
	exe, err := os.Executable()
	if err == nil {
		exe, _ = filepath.EvalSymlinks(exe)
		installRoot := filepath.Dir(filepath.Dir(exe))
		paths = append(paths, filepath.Join(installRoot, indexerRelDir))
		paths = append(paths, filepath.Join(installRoot, "skills", "call-rpc", indexerRelDir))
	}
	paths = append(paths, filepath.Join(".", indexerRelDir))
	for _, indexerDir := range paths {
		if info, err := os.Stat(indexerDir); err == nil && info.IsDir() {
			return indexerDir, nil
		}
	}
	return "", fmt.Errorf("Spoon indexer module not found (tried: %s)", strings.Join(paths, ", "))
}

func commandString(cmd *exec.Cmd) string {
	parts := append([]string{cmd.Path}, cmd.Args[1:]...)
	return strings.Join(parts, " ")
}

func commandFailureError(prefix string, cmd *exec.Cmd, stdout, stderr []byte, err error) error {
	command := commandString(cmd)
	detail := strings.TrimSpace(string(stderr))
	if detail == "" {
		detail = strings.TrimSpace(string(stdout))
	}
	if exitErr, ok := err.(*exec.ExitError); ok {
		if detail != "" {
			return fmt.Errorf("%s: command %q (cwd=%s, exit=%d): %w: %s", prefix, command, cmd.Dir, exitErr.ExitCode(), err, detail)
		}
		return fmt.Errorf("%s: command %q (cwd=%s, exit=%d): %w", prefix, command, cmd.Dir, exitErr.ExitCode(), err)
	}
	if detail != "" {
		return fmt.Errorf("%s: command %q (cwd=%s): %w: %s", prefix, command, cmd.Dir, err, detail)
	}
	return fmt.Errorf("%s: command %q (cwd=%s): %w", prefix, command, cmd.Dir, err)
}
