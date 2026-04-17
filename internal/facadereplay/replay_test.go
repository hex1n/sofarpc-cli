package facadereplay

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hex1n/sofarpc-cli/internal/facadeconfig"
)

func TestReplayCallsDryRunPrintsCommands(t *testing.T) {
	root := filepath.Join(t.TempDir(), "proj")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("MkdirAll root: %v", err)
	}
	cfg := facadeconfig.DefaultConfig()
	cfg.DefaultContext = "from-config"
	cfg.FacadeModules = []facadeconfig.FacadeModule{{Name: "svc-facade", SourceRoot: "svc-facade/src/main/java"}}
	if err := facadeconfig.SaveJSON(facadeconfig.ConfigPath(root), cfg); err != nil {
		t.Fatalf("SaveJSON config: %v", err)
	}
	replayDir := facadeconfig.EffectiveReplayDir(root)
	if err := os.MkdirAll(replayDir, 0o755); err != nil {
		t.Fatalf("MkdirAll replays: %v", err)
	}
	payload := map[string]any{
		"service": "com.example.UserFacade",
		"method":  "getUser",
		"calls": []map[string]any{
			{
				"name":        "happy",
				"context":     "from-replay",
				"payloadMode": "generic",
				"params": []map[string]any{
					{"id": 1},
				},
			},
		},
	}
	if err := facadeconfig.SaveJSON(filepath.Join(replayDir, "UserFacade_getUser.json"), payload); err != nil {
		t.Fatalf("SaveJSON replay: %v", err)
	}

	var stdout, stderr bytes.Buffer
	err := ReplayCalls(root, ReplayOptions{
		Filter:          "UserFacade",
		ContextOverride: "from-override",
		DryRun:          true,
	}, &stdout, &stderr)
	if err != nil {
		t.Fatalf("ReplayCalls error = %v", err)
	}

	out := stdout.String()
	for _, want := range []string{
		"▶ UserFacade.getUser [happy]",
		"-context from-override",
		"-payload-mode generic",
		"-full-response",
		"DRY",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("expected output to contain %q, got:\n%s", want, out)
		}
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %s", stderr.String())
	}
	if tempFiles, err := filepath.Glob(filepath.Join(root, ".rpc-run-*.json")); err != nil {
		t.Fatalf("Glob temp files: %v", err)
	} else if len(tempFiles) != 0 {
		t.Fatalf("temporary files not cleaned up: %v", tempFiles)
	}
}

func TestReplayCallsSavesResultsAndReturnsSilentErrorOnRPCFail(t *testing.T) {
	root := filepath.Join(t.TempDir(), "proj")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("MkdirAll root: %v", err)
	}
	cfg := facadeconfig.DefaultConfig()
	cfg.DefaultContext = "test-direct"
	cfg.SofaRPCBin = "fake-sofarpc"
	cfg.FacadeModules = []facadeconfig.FacadeModule{{Name: "svc-facade", SourceRoot: "svc-facade/src/main/java"}}
	if err := facadeconfig.SaveJSON(facadeconfig.ConfigPath(root), cfg); err != nil {
		t.Fatalf("SaveJSON config: %v", err)
	}
	replayDir := facadeconfig.EffectiveReplayDir(root)
	if err := os.MkdirAll(replayDir, 0o755); err != nil {
		t.Fatalf("MkdirAll replays: %v", err)
	}
	if err := facadeconfig.SaveJSON(filepath.Join(replayDir, "UserFacade_getUser.json"), map[string]any{
		"service": "com.example.UserFacade",
		"method":  "getUser",
		"calls": []map[string]any{
			{
				"name":   "rpc_fail",
				"params": []map[string]any{{"id": 1}},
			},
		},
	}); err != nil {
		t.Fatalf("SaveJSON replay: %v", err)
	}

	originalRunner := ReplayCommandRunner
	defer func() { ReplayCommandRunner = originalRunner }()

	var seenBin, seenCwd, tempPath string
	var seenArgs []string
	ReplayCommandRunner = func(bin string, args []string, cwd string) (int, string, string, error) {
		seenBin = bin
		seenArgs = append([]string{}, args...)
		seenCwd = cwd
		for i := 0; i < len(args)-1; i++ {
			if args[i] == "-data" {
				tempPath = strings.TrimPrefix(args[i+1], "@")
				break
			}
		}
		body, err := os.ReadFile(tempPath)
		if err != nil {
			t.Fatalf("ReadFile temp payload: %v", err)
		}
		var params []map[string]any
		if err := json.Unmarshal(body, &params); err != nil {
			t.Fatalf("Unmarshal temp payload: %v", err)
		}
		if len(params) != 1 || params[0]["id"] != float64(1) {
			t.Fatalf("params = %#v", params)
		}
		return 1, "", "transport failed", nil
	}

	var stdout, stderr bytes.Buffer
	err := ReplayCalls(root, ReplayOptions{Save: true}, &stdout, &stderr)
	if err == nil {
		t.Fatal("ReplayCalls error = nil, want silent rpc-fail exit")
	}
	silent, ok := err.(interface{ Silent() bool })
	if !ok || !silent.Silent() {
		t.Fatalf("expected silent exit error, got %T: %v", err, err)
	}
	if seenBin != "fake-sofarpc" {
		t.Fatalf("runner bin = %q", seenBin)
	}
	if seenCwd != root {
		t.Fatalf("runner cwd = %q", seenCwd)
	}
	if !strings.Contains(strings.Join(seenArgs, " "), "call -context test-direct") {
		t.Fatalf("runner args = %v", seenArgs)
	}
	if tempPath == "" {
		t.Fatal("temp payload path not captured")
	}
	if _, statErr := os.Stat(tempPath); !os.IsNotExist(statErr) {
		t.Fatalf("temp payload still exists: %v", statErr)
	}
	runFile := filepath.Join(replayDir, "_runs", "UserFacade_getUser__rpc_fail.json")
	body, err := os.ReadFile(runFile)
	if err != nil {
		t.Fatalf("ReadFile saved run: %v", err)
	}
	var saved map[string]any
	if err := json.Unmarshal(body, &saved); err != nil {
		t.Fatalf("Unmarshal saved run: %v", err)
	}
	if saved["status"] != "RPC_FAIL" {
		t.Fatalf("saved status = %#v", saved["status"])
	}
	if !strings.Contains(stdout.String(), "RPC_FAIL") {
		t.Fatalf("stdout = %s", stdout.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %s", stderr.String())
	}
}
