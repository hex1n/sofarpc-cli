package cli

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hex1n/sofarpc-cli/internal/facadekit"
)

func TestSplitFacadeProjectArg(t *testing.T) {
	project, rest, err := splitFacadeProjectArg([]string{
		"--project", "C:/work/demo",
		"--filter", "Deal",
		"--save",
	})
	if err != nil {
		t.Fatalf("splitFacadeProjectArg() error = %v", err)
	}
	if project != "C:/work/demo" {
		t.Fatalf("expected project override, got %q", project)
	}
	if got := strings.Join(rest, " "); got != "--filter Deal --save" {
		t.Fatalf("unexpected passthrough args: %q", got)
	}
}

func TestSplitFacadeProjectArgRejectsMissingValue(t *testing.T) {
	if _, _, err := splitFacadeProjectArg([]string{"--project"}); err == nil {
		t.Fatal("expected missing project value to be rejected")
	}
}

func TestInspectFacadeStatePrefersExistingConfigLayout(t *testing.T) {
	root := t.TempDir()
	stateDir := filepath.Join(root, ".sofarpc")
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(stateDir, "config.json"), []byte("{}\n"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	state := facadekit.InspectState(root)
	if state.Layout.Label() != "primary (.sofarpc)" {
		t.Fatalf("expected primary layout, got %q", state.Layout.Label())
	}
	if !strings.Contains(state.ConfigPath, filepath.Join(".sofarpc", "config.json")) {
		t.Fatalf("unexpected config path %q", state.ConfigPath)
	}
}

func TestResolveFacadeProjectRootWalksUpFromNestedDir(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "pom.xml"), []byte("<project><modelVersion>4.0.0</modelVersion></project>"), 0o644); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	nested := filepath.Join(root, "svc", "impl")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatalf("MkdirAll(nested) error = %v", err)
	}
	app := &App{Cwd: nested}
	got, err := app.resolveFacadeProjectRoot("")
	if err != nil {
		t.Fatalf("resolveFacadeProjectRoot() error = %v", err)
	}
	if got != root {
		t.Fatalf("expected walk-up root %q, got %q", root, got)
	}
}

func TestPrintFacadeSchemaText(t *testing.T) {
	var stdout bytes.Buffer
	schema := facadekit.MethodSchemaEnvelope{
		Service: "com.example.UserFacade",
		File:    "src/main/java/com/example/UserFacade.java",
		Method: facadekit.MethodSchemaResult{
			Name:                  "getUser",
			ReturnType:            "com.example.UserVO",
			ParamTypes:            []string{"com.example.UserRequest"},
			ParamsFieldInfo:       []facadekit.ParameterSchema{{Name: "request", Type: "com.example.UserRequest", RequiredHint: "required", Fields: []facadekit.FieldSchema{{Name: "tenantId", Type: "java.lang.String", Comment: "tenant id"}}}},
			ResponseWarning:       "response wrapper exposes Optional/helper getters; prefer raw mode when stub jars are complete, generic mode may lose nested DTO types",
			ResponseWarningReason: "Optional getter found on return type",
		},
	}

	if err := printFacadeSchema(&stdout, schema); err != nil {
		t.Fatalf("printFacadeSchema() error = %v", err)
	}

	out := stdout.String()
	for _, want := range []string{
		"service: com.example.UserFacade",
		"file:    src/main/java/com/example/UserFacade.java",
		"method:  getUser",
		"return:  com.example.UserVO",
		"warning: response wrapper",
		"- request: com.example.UserRequest",
		"  - tenantId: java.lang.String # tenant id",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("expected output to contain %q, got:\n%s", want, out)
		}
	}
}

func TestRunFacadeStatusPrintsResolvedProjectState(t *testing.T) {
	root := t.TempDir()
	stateDir := filepath.Join(root, ".sofarpc")
	if err := os.MkdirAll(filepath.Join(stateDir, "index"), 0o755); err != nil {
		t.Fatalf("MkdirAll(index) error = %v", err)
	}
	if err := os.MkdirAll(filepath.Join(stateDir, "replays"), 0o755); err != nil {
		t.Fatalf("MkdirAll(replays) error = %v", err)
	}
	body, err := json.Marshal(map[string]any{
		"sofarpcBin":     "C:/Users/demo/bin/sofarpc.exe",
		"defaultContext": "test-direct",
		"manifestPath":   "sofarpc.manifest.json",
		"facadeModules":  []map[string]string{{"name": "demo", "sourceRoot": "svc/src/main/java"}},
	})
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(stateDir, "config.json"), append(body, '\n'), 0o644); err != nil {
		t.Fatalf("WriteFile(config) error = %v", err)
	}

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd() error = %v", err)
	}
	moduleRoot, err := filepath.Abs(filepath.Join(cwd, "..", ".."))
	if err != nil {
		t.Fatalf("Abs(module root) error = %v", err)
	}
	t.Setenv("SOFARPC_HOME", moduleRoot)

	var stdout bytes.Buffer
	app := &App{
		Stdout: &stdout,
		Stderr: io.Discard,
		Cwd:    root,
	}
	if err := app.runFacadeStatus(nil); err != nil {
		t.Fatalf("runFacadeStatus() error = %v", err)
	}

	out := stdout.String()
	for _, want := range []string{
		"state layout:   primary (.sofarpc)",
		"config path:    " + filepath.Join(root, ".sofarpc", "config.json"),
		"index dir:      " + filepath.Join(root, ".sofarpc", "index"),
		"replay dir:     " + filepath.Join(root, ".sofarpc", "replays"),
		"sofarpcBin:     C:/Users/demo/bin/sofarpc.exe",
		"defaultContext: test-direct",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("expected output to contain %q, got:\n%s", want, out)
		}
	}
}
