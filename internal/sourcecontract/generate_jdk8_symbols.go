//go:build ignore

package main

import (
	"archive/zip"
	"bytes"
	"fmt"
	"go/format"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
)

var publicTopLevelDecl = regexp.MustCompile(`\bpublic\s+(?:(?:abstract|final|strictfp)\s+)*(?:@interface|class|interface|enum)\s+([A-Za-z_$][A-Za-z0-9_$]*)\b`)

func main() {
	srcZip, err := findJDK8SourceZip()
	if err != nil {
		fatal(err)
	}
	symbols, err := readSymbols(srcZip)
	if err != nil {
		fatal(err)
	}
	if len(symbols) == 0 {
		fatal(fmt.Errorf("no java.* symbols found in %s", srcZip))
	}
	out, err := render(symbols)
	if err != nil {
		fatal(err)
	}
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		fatal(fmt.Errorf("cannot locate generator source directory"))
	}
	target := filepath.Join(filepath.Dir(thisFile), "jdk8_platform_symbols_generated.go")
	if err := os.WriteFile(target, out, 0o644); err != nil {
		fatal(err)
	}
}

func fatal(err error) {
	fmt.Fprintf(os.Stderr, "generate_jdk8_symbols: %v\n", err)
	os.Exit(1)
}

func findJDK8SourceZip() (string, error) {
	var roots []string
	for _, key := range []string{"JDK8_HOME", "JAVA_HOME"} {
		if value := strings.TrimSpace(os.Getenv(key)); value != "" {
			roots = append(roots, value)
		}
	}
	if javaHome := currentJavaHome(); javaHome != "" {
		roots = append(roots, javaHome)
	}
	seen := map[string]struct{}{}
	for _, root := range roots {
		for _, candidate := range sourceZipCandidates(root) {
			candidate = filepath.Clean(candidate)
			if _, ok := seen[candidate]; ok {
				continue
			}
			seen[candidate] = struct{}{}
			if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
				return candidate, nil
			}
		}
	}
	return "", fmt.Errorf("JDK 8 src.zip not found; set JDK8_HOME or JAVA_HOME to a JDK 8 installation")
}

func currentJavaHome() string {
	out, err := exec.Command("java", "-XshowSettings:properties", "-version").CombinedOutput()
	if err != nil && len(out) == 0 {
		return ""
	}
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "java.home =") {
			continue
		}
		return strings.TrimSpace(strings.TrimPrefix(line, "java.home ="))
	}
	return ""
}

func sourceZipCandidates(root string) []string {
	root = filepath.Clean(root)
	candidates := []string{
		filepath.Join(root, "src.zip"),
		filepath.Join(root, "..", "src.zip"),
		filepath.Join(root, "jre", "src.zip"),
		filepath.Join(root, "..", "jre", "src.zip"),
		filepath.Join(root, "Contents", "Home", "src.zip"),
		filepath.Join(root, "zulu-8.jdk", "Contents", "Home", "src.zip"),
	}
	return candidates
}

func readSymbols(srcZip string) (map[string][]string, error) {
	reader, err := zip.OpenReader(srcZip)
	if err != nil {
		return nil, err
	}
	defer reader.Close()

	symbolSet := map[string]map[string]struct{}{}
	for _, file := range reader.File {
		name := filepath.ToSlash(file.Name)
		if !strings.HasPrefix(name, "java/") || !strings.HasSuffix(name, ".java") {
			continue
		}
		base := filepath.Base(name)
		if base == "package-info.java" || base == "module-info.java" {
			continue
		}
		pkg := strings.ReplaceAll(filepath.Dir(name), "/", ".")
		simple := strings.TrimSuffix(base, ".java")
		ok, err := containsPublicTopLevel(file, simple)
		if err != nil {
			return nil, err
		}
		if !ok {
			continue
		}
		if symbolSet[pkg] == nil {
			symbolSet[pkg] = map[string]struct{}{}
		}
		symbolSet[pkg][simple] = struct{}{}
	}

	out := make(map[string][]string, len(symbolSet))
	for pkg, set := range symbolSet {
		names := make([]string, 0, len(set))
		for name := range set {
			names = append(names, name)
		}
		sort.Strings(names)
		out[pkg] = names
	}
	return out, nil
}

func containsPublicTopLevel(file *zip.File, simple string) (bool, error) {
	rc, err := file.Open()
	if err != nil {
		return false, err
	}
	defer rc.Close()
	body, err := io.ReadAll(rc)
	if err != nil {
		return false, err
	}
	clean := stripComments(body)
	matches := publicTopLevelDecl.FindAllSubmatch(clean, -1)
	for _, match := range matches {
		if len(match) == 2 && string(match[1]) == simple {
			return true, nil
		}
	}
	return false, nil
}

func stripComments(input []byte) []byte {
	out := make([]byte, len(input))
	copy(out, input)
	const (
		normal = iota
		lineComment
		blockComment
		singleQuote
		doubleQuote
	)
	state := normal
	for i := 0; i < len(out); i++ {
		switch state {
		case normal:
			if i+1 < len(out) && out[i] == '/' && out[i+1] == '/' {
				out[i], out[i+1] = ' ', ' '
				state = lineComment
				i++
				continue
			}
			if i+1 < len(out) && out[i] == '/' && out[i+1] == '*' {
				out[i], out[i+1] = ' ', ' '
				state = blockComment
				i++
				continue
			}
			if out[i] == '\'' {
				out[i] = ' '
				state = singleQuote
				continue
			}
			if out[i] == '"' {
				out[i] = ' '
				state = doubleQuote
				continue
			}
		case lineComment:
			if out[i] == '\n' || out[i] == '\r' {
				state = normal
			} else {
				out[i] = ' '
			}
		case blockComment:
			if i+1 < len(out) && out[i] == '*' && out[i+1] == '/' {
				out[i], out[i+1] = ' ', ' '
				state = normal
				i++
			} else if out[i] != '\n' && out[i] != '\r' {
				out[i] = ' '
			}
		case singleQuote:
			if out[i] == '\\' && i+1 < len(out) {
				out[i], out[i+1] = ' ', ' '
				i++
				continue
			}
			if out[i] == '\'' {
				state = normal
			}
			if out[i] != '\n' && out[i] != '\r' {
				out[i] = ' '
			}
		case doubleQuote:
			if out[i] == '\\' && i+1 < len(out) {
				out[i], out[i+1] = ' ', ' '
				i++
				continue
			}
			if out[i] == '"' {
				state = normal
			}
			if out[i] != '\n' && out[i] != '\r' {
				out[i] = ' '
			}
		}
	}
	return out
}

func render(symbols map[string][]string) ([]byte, error) {
	var packages []string
	for pkg := range symbols {
		packages = append(packages, pkg)
	}
	sort.Strings(packages)

	var buf bytes.Buffer
	buf.WriteString("// Code generated by go generate ./internal/sourcecontract; DO NOT EDIT.\n\n")
	buf.WriteString("package sourcecontract\n\n")
	buf.WriteString("var jdk8PlatformSymbols = map[string]map[string]string{\n")
	for _, pkg := range packages {
		buf.WriteString(fmt.Sprintf("\t%q: {\n", pkg))
		for _, simple := range symbols[pkg] {
			buf.WriteString(fmt.Sprintf("\t\t%q: %q,\n", simple, pkg+"."+simple))
		}
		buf.WriteString("\t},\n")
	}
	buf.WriteString("}\n")

	formatted, err := format.Source(buf.Bytes())
	if err != nil {
		return nil, err
	}
	return formatted, nil
}
