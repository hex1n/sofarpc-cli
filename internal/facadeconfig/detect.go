package facadeconfig

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"

	"github.com/hex1n/sofarpc-cli/internal/projectscan"
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
	return projectscan.DetectFacadeModules(projectRoot)
}

func FirstArtifactID(pomText string) string {
	return projectscan.FirstArtifactID(pomText)
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

func runtimeWindows() bool {
	return os.PathSeparator == '\\'
}
