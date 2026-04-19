package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/hex1n/sofarpc-cli/internal/core/target"
)

func TestAtoiOrZero(t *testing.T) {
	cases := []struct {
		in   string
		want int
	}{
		{"", 0},
		{"0", 0},
		{"42", 42},
		{"-7", -7},
		{"not-a-number", 0},
		{"12x", 0},
	}
	for _, tc := range cases {
		if got := atoiOrZero(tc.in); got != tc.want {
			t.Errorf("atoiOrZero(%q) = %d, want %d", tc.in, got, tc.want)
		}
	}
}

func TestEnvConfig_DirectURLImpliesModeDirect(t *testing.T) {
	clearTargetEnv(t)
	t.Setenv("SOFARPC_DIRECT_URL", "bolt://host:12200")
	t.Setenv("SOFARPC_TIMEOUT_MS", "2500")

	cfg := envConfig()
	if cfg.Mode != target.ModeDirect {
		t.Fatalf("mode: got %q want %q", cfg.Mode, target.ModeDirect)
	}
	if cfg.DirectURL != "bolt://host:12200" {
		t.Fatalf("directUrl: got %q", cfg.DirectURL)
	}
	if cfg.TimeoutMS != 2500 {
		t.Fatalf("timeoutMs: got %d want 2500", cfg.TimeoutMS)
	}
}

func TestEnvConfig_RegistryWithoutDirectURLImpliesModeRegistry(t *testing.T) {
	clearTargetEnv(t)
	t.Setenv("SOFARPC_REGISTRY_ADDRESS", "zookeeper://host:2181")

	cfg := envConfig()
	if cfg.Mode != target.ModeRegistry {
		t.Fatalf("mode: got %q want %q", cfg.Mode, target.ModeRegistry)
	}
}

func TestEnvConfig_EmptyEnvYieldsEmptyMode(t *testing.T) {
	clearTargetEnv(t)
	cfg := envConfig()
	if cfg.Mode != "" {
		t.Fatalf("mode should be empty without env, got %q", cfg.Mode)
	}
}

func TestEnvConfig_DirectUrlWinsOverRegistry(t *testing.T) {
	clearTargetEnv(t)
	// Both set — direct wins, matching target.Resolve precedence.
	t.Setenv("SOFARPC_DIRECT_URL", "bolt://host:1")
	t.Setenv("SOFARPC_REGISTRY_ADDRESS", "zk://host:2")

	cfg := envConfig()
	if cfg.Mode != target.ModeDirect {
		t.Fatalf("mode: got %q want direct", cfg.Mode)
	}
}

func TestProjectRootFromEnv_PrefersExplicitOverCWD(t *testing.T) {
	t.Setenv("SOFARPC_PROJECT_ROOT", "/custom/root")
	if got := projectRootFromEnv(); got != "/custom/root" {
		t.Fatalf("got %q want /custom/root", got)
	}
}

func TestProjectRootFromEnv_FallsBackToCWD(t *testing.T) {
	t.Setenv("SOFARPC_PROJECT_ROOT", "")
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if got := projectRootFromEnv(); got != wd {
		t.Fatalf("got %q want %q", got, wd)
	}
}

func TestIndexerSourcesFromEnv_ExplicitWins(t *testing.T) {
	root := t.TempDir()
	// Fabricate a src/main/java so we can prove the env overrides it.
	mavenDir := filepath.Join(root, "src", "main", "java")
	if err := os.MkdirAll(mavenDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	sep := string(os.PathListSeparator)
	t.Setenv("SOFARPC_INDEXER_SOURCES", "/abs/a"+sep+"/abs/b")

	got := indexerSourcesFromEnv(root)
	want := []string{"/abs/a", "/abs/b"}
	if !equalStringSlices(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
}

func TestIndexerSourcesFromEnv_EmptyEntriesAreDropped(t *testing.T) {
	sep := string(os.PathListSeparator)
	t.Setenv("SOFARPC_INDEXER_SOURCES", sep+"/abs/a"+sep+sep+"/abs/b"+sep)
	got := indexerSourcesFromEnv("")
	want := []string{"/abs/a", "/abs/b"}
	if !equalStringSlices(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
}

func TestIndexerSourcesFromEnv_FallbackMavenLayout(t *testing.T) {
	t.Setenv("SOFARPC_INDEXER_SOURCES", "")
	root := t.TempDir()
	mavenDir := filepath.Join(root, "src", "main", "java")
	if err := os.MkdirAll(mavenDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	got := indexerSourcesFromEnv(root)
	if len(got) != 1 || got[0] != mavenDir {
		t.Fatalf("got %v want [%s]", got, mavenDir)
	}
}

func TestIndexerSourcesFromEnv_FallbackSkippedWhenMissing(t *testing.T) {
	t.Setenv("SOFARPC_INDEXER_SOURCES", "")
	// TempDir exists but has no src/main/java subtree.
	if got := indexerSourcesFromEnv(t.TempDir()); got != nil {
		t.Fatalf("expected nil, got %v", got)
	}
}

func TestLoadReindexer_NilWithoutJar(t *testing.T) {
	t.Setenv("SOFARPC_INDEXER_JAR", "")
	if r := loadReindexer(t.TempDir()); r != nil {
		t.Fatalf("expected nil reindexer without jar, got %T", r)
	}
}

func TestLoadReindexer_NilWithoutProjectRoot(t *testing.T) {
	t.Setenv("SOFARPC_INDEXER_JAR", "/abs/spoon.jar")
	if r := loadReindexer(""); r != nil {
		t.Fatalf("expected nil reindexer without project root, got %T", r)
	}
}

func TestLoadReindexer_NilWhenNoSourcesResolvable(t *testing.T) {
	t.Setenv("SOFARPC_INDEXER_JAR", "/abs/spoon.jar")
	t.Setenv("SOFARPC_INDEXER_SOURCES", "")
	// Empty TempDir with no src/main/java → fallback returns nil →
	// loadReindexer logs a warning and returns nil.
	if r := loadReindexer(t.TempDir()); r != nil {
		t.Fatalf("expected nil reindexer when no sources, got %T", r)
	}
}

func TestLoadReindexer_NonNilWhenJarAndSourcesPresent(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "src", "main", "java"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	t.Setenv("SOFARPC_INDEXER_JAR", "/abs/spoon.jar")
	t.Setenv("SOFARPC_INDEXER_SOURCES", "")
	if r := loadReindexer(root); r == nil {
		t.Fatal("expected non-nil reindexer when jar + fallback sources are available")
	}
}

func TestLoadWorker_NilWithoutJar(t *testing.T) {
	t.Setenv("SOFARPC_RUNTIME_JAR", "")
	if c := loadWorker(); c != nil {
		t.Fatalf("expected nil client without jar, got %T", c)
	}
}

func TestLoadWorker_NilWhenDigestMissing(t *testing.T) {
	t.Setenv("SOFARPC_RUNTIME_JAR", "/abs/runtime.jar")
	t.Setenv("SOFARPC_RUNTIME_JAR_DIGEST", "")
	if c := loadWorker(); c != nil {
		t.Fatalf("expected nil client without digest, got %T", c)
	}
}

func TestLoadWorker_BuildsClientWhenFullyConfigured(t *testing.T) {
	t.Setenv("SOFARPC_RUNTIME_JAR", "/abs/runtime.jar")
	t.Setenv("SOFARPC_RUNTIME_JAR_DIGEST", "sha256:deadbeef")
	t.Setenv("SOFARPC_VERSION", "5.12.0")
	t.Setenv("SOFARPC_JAVA_MAJOR", "17")
	client := loadWorker()
	if client == nil {
		t.Fatal("expected a client when all env is set")
	}
	if client.Profile.SOFARPCVersion != "5.12.0" {
		t.Fatalf("profile version: got %q", client.Profile.SOFARPCVersion)
	}
	if client.Profile.RuntimeJarDigest != "sha256:deadbeef" {
		t.Fatalf("profile digest: got %q", client.Profile.RuntimeJarDigest)
	}
	if client.Profile.JavaMajor != 17 {
		t.Fatalf("java major: got %d", client.Profile.JavaMajor)
	}
	if client.Pool == nil {
		t.Fatal("pool should be initialised")
	}
}

func TestLoadWorker_DefaultsJavaMajorWhenUnset(t *testing.T) {
	t.Setenv("SOFARPC_RUNTIME_JAR", "/abs/runtime.jar")
	t.Setenv("SOFARPC_RUNTIME_JAR_DIGEST", "sha256:x")
	t.Setenv("SOFARPC_JAVA_MAJOR", "")
	t.Setenv("SOFARPC_VERSION", "")
	client := loadWorker()
	if client == nil {
		t.Fatal("expected a client with defaulted fields")
	}
	if client.Profile.JavaMajor != 17 {
		t.Fatalf("java major default: got %d want 17", client.Profile.JavaMajor)
	}
	if client.Profile.SOFARPCVersion != "unknown" {
		t.Fatalf("version default: got %q want unknown", client.Profile.SOFARPCVersion)
	}
}

// --- helpers ------------------------------------------------------------

// clearTargetEnv resets every SOFARPC_* target variable, so tests that
// assert derived Mode aren't contaminated by the developer's shell or
// by sibling tests that left a var set.
func clearTargetEnv(t *testing.T) {
	t.Helper()
	for _, key := range []string{
		"SOFARPC_DIRECT_URL",
		"SOFARPC_REGISTRY_ADDRESS",
		"SOFARPC_REGISTRY_PROTOCOL",
		"SOFARPC_PROTOCOL",
		"SOFARPC_SERIALIZATION",
		"SOFARPC_UNIQUE_ID",
		"SOFARPC_TIMEOUT_MS",
		"SOFARPC_CONNECT_TIMEOUT_MS",
	} {
		t.Setenv(key, "")
	}
}

func equalStringSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
