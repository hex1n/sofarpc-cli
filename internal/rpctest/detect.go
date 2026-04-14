package rpctest

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
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

func DetectConfig(projectRoot string, write bool, stdout, stderr io.Writer) error {
	detected := DefaultConfig()
	detected.MvnCommand = detectMavenCommand(projectRoot)
	detected.SofaRPCBin = detectSofaRPCBin()
	detected.FacadeModules = detectFacadeModules(projectRoot)

	existing, existingRaw, _, err := loadExistingConfigForDetect(projectRoot, stderr)
	if err != nil {
		return err
	}
	final := mergeDetectedConfig(existing, detected)
	finalDoc, err := mergeDetectedConfigDocument(existingRaw, final)
	if err != nil {
		return err
	}

	if err := printDetectedConfig(stdout, finalDoc); err != nil {
		return err
	}
	if len(final.FacadeModules) == 0 && stderr != nil {
		fmt.Fprintln(stderr, "\n[detect] no facade modules found. Expected a maven module whose")
		fmt.Fprintln(stderr, "  artifactId ends in 'facade' / 'api' / 'client' with src/main/java.")
		fmt.Fprintln(stderr, "  Edit the generated config manually if your project uses different naming.")
	}
	if !write {
		fmt.Fprintf(stdout, "\n[detect] dry-run only; pass --write to save %s\n", ConfigPath(projectRoot))
		return nil
	}
	if err := SaveJSON(ConfigPath(projectRoot), finalDoc); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "\n[detect] wrote %s\n", ConfigPath(projectRoot))
	return nil
}

func detectMavenCommand(projectRoot string) string {
	if runtimeWindows() {
		if _, err := os.Stat(filepath.Join(projectRoot, "mvnw.cmd")); err == nil {
			return "./mvnw.cmd"
		}
	}
	if _, err := os.Stat(filepath.Join(projectRoot, "mvnw")); err == nil {
		return "./mvnw"
	}
	return "mvn"
}

func detectSofaRPCBin() string {
	for _, candidate := range []string{"sofarpc", "sofarpc.exe"} {
		if _, err := exec.LookPath(candidate); err == nil {
			return "sofarpc"
		}
	}
	exeName := "sofarpc"
	if runtimeWindows() {
		exeName = "sofarpc.exe"
	}
	var common []string
	if shimDir := strings.TrimSpace(os.Getenv("SOFARPC_SHIM_DIR")); shimDir != "" {
		common = append(common, filepath.Join(shimDir, exeName))
	}
	home, err := os.UserHomeDir()
	if err == nil {
		if runtimeWindows() {
			common = append(common, filepath.Join(home, "bin", exeName))
		} else {
			common = append(common, filepath.Join(home, ".local", "bin", exeName))
		}
		common = append(common, filepath.Join(home, ".sofarpc", "bin", exeName))
	}
	for _, candidate := range common {
		if _, err := os.Stat(candidate); err == nil {
			return filepath.ToSlash(candidate)
		}
	}
	return "sofarpc"
}

func detectFacadeModules(projectRoot string) []FacadeModule {
	var found []FacadeModule
	_ = filepath.WalkDir(projectRoot, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if _, skip := skipDirs[d.Name()]; skip {
				return filepath.SkipDir
			}
			if strings.HasPrefix(d.Name(), ".") && path != projectRoot {
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
		artifact := firstArtifactID(string(body))
		if artifact == "" || !facadeSuffixPattern.MatchString(artifact) {
			return nil
		}
		moduleDir := filepath.Dir(path)
		sourceRoot := filepath.Join(moduleDir, "src", "main", "java")
		if info, err := os.Stat(sourceRoot); err != nil || !info.IsDir() {
			return nil
		}
		found = append(found, FacadeModule{
			Name:            artifact,
			SourceRoot:      relPath(projectRoot, sourceRoot),
			MavenModulePath: relPath(projectRoot, moduleDir),
			JarGlob:         filepath.ToSlash(filepath.Join(relPath(projectRoot, moduleDir), "target", artifact+"-*.jar")),
			DepsDir:         filepath.ToSlash(filepath.Join(relPath(projectRoot, moduleDir), "target", "facade-deps")),
		})
		return nil
	})
	sort.Slice(found, func(i, j int) bool {
		if found[i].Name == found[j].Name {
			return found[i].MavenModulePath < found[j].MavenModulePath
		}
		return found[i].Name < found[j].Name
	})
	var unique []FacadeModule
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

func firstArtifactID(pomText string) string {
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

func loadExistingConfigForDetect(projectRoot string, stderr io.Writer) (Config, map[string]any, string, error) {
	body, err := os.ReadFile(ConfigPath(projectRoot))
	if os.IsNotExist(err) {
		return Config{}, nil, "", nil
	}
	if err != nil {
		return Config{}, nil, "", err
	}
	var raw map[string]any
	if err := json.Unmarshal(body, &raw); err != nil {
		if stderr != nil {
			fmt.Fprintf(stderr, "[detect] %s is not valid JSON (%v); ignoring\n", ConfigPath(projectRoot), err)
		}
		return Config{}, nil, "", nil
	}
	clean := cleanConfigMap(raw)
	cfg, err := configFromMap(clean)
	if err != nil {
		return Config{}, nil, "", err
	}
	return cfg, clean, ConfigPath(projectRoot), nil
}

func mergeDetectedConfig(existing, detected Config) Config {
	if len(existing.FacadeModules) == 0 {
		existing.FacadeModules = detected.FacadeModules
	}
	if strings.TrimSpace(existing.MvnCommand) == "" {
		existing.MvnCommand = detected.MvnCommand
	}
	if strings.TrimSpace(existing.SofaRPCBin) == "" {
		existing.SofaRPCBin = detected.SofaRPCBin
	}
	if len(existing.InterfaceSuffixes) == 0 {
		existing.InterfaceSuffixes = detected.InterfaceSuffixes
	}
	if len(existing.RequiredMarkers) == 0 {
		existing.RequiredMarkers = detected.RequiredMarkers
	}
	if strings.TrimSpace(existing.DefaultContext) == "" {
		existing.DefaultContext = detected.DefaultContext
	}
	if strings.TrimSpace(existing.ManifestPath) == "" {
		existing.ManifestPath = detected.ManifestPath
	}
	return existing
}

func mergeDetectedConfigDocument(existing map[string]any, merged Config) (map[string]any, error) {
	detected, err := configToMap(merged)
	if err != nil {
		return nil, err
	}
	if len(existing) == 0 {
		return detected, nil
	}
	final := make(map[string]any, len(existing)+len(detected))
	for key, value := range existing {
		final[key] = value
	}
	for key, value := range detected {
		if isBlankConfigValue(final[key]) {
			final[key] = value
		}
	}
	return final, nil
}

func printDetectedConfig(stdout io.Writer, cfg any) error {
	body, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(stdout, "%s\n", body)
	return err
}

func cleanConfigMap(raw map[string]any) map[string]any {
	clean := make(map[string]any, len(raw))
	for key, value := range raw {
		if strings.HasPrefix(key, "_") || strings.HasPrefix(key, "$") {
			continue
		}
		clean[key] = value
	}
	return clean
}

func configFromMap(raw map[string]any) (Config, error) {
	if len(raw) == 0 {
		return Config{}, nil
	}
	body, err := json.Marshal(raw)
	if err != nil {
		return Config{}, err
	}
	var cfg Config
	if err := json.Unmarshal(body, &cfg); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func configToMap(cfg Config) (map[string]any, error) {
	body, err := json.Marshal(cfg)
	if err != nil {
		return nil, err
	}
	var raw map[string]any
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, err
	}
	return raw, nil
}

func isBlankConfigValue(value any) bool {
	if value == nil {
		return true
	}
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed) == ""
	}
	rv := reflect.ValueOf(value)
	switch rv.Kind() {
	case reflect.Slice, reflect.Array, reflect.Map:
		return rv.Len() == 0
	}
	return false
}

func relPath(projectRoot, path string) string {
	rel, err := filepath.Rel(projectRoot, path)
	if err != nil {
		return filepath.ToSlash(path)
	}
	return filepath.ToSlash(rel)
}

func runtimeWindows() bool {
	return os.PathSeparator == '\\'
}
