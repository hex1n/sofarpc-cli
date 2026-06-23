package target

import (
	"os"
	"path/filepath"
	"testing"
)

// writeProfileFixture lays down a project whose shared config defines team
// profiles (test, staging) and whose local config adds a personal "local"
// profile plus a local override of "test". It mirrors the ADR 0003 example.
func writeProfileFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, ".sofarpc")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	shared := `{
  "protocol": "bolt",
  "serialization": "hessian2",
  "timeoutMs": 3000,
  "allowedServices": ["com.foo.UserFacade"],
  "defaultProfile": "test",
  "profiles": {
    "test": {
      "registryAddress": "zookeeper://zk-test.example.com:2181",
      "registryProtocol": "zookeeper"
    },
    "staging": {
      "registryAddress": "zookeeper://zk-staging.example.com:2181"
    }
  }
}`
	local := `{
  "defaultProfile": "local",
  "profiles": {
    "local": { "directUrl": "bolt://127.0.0.1:12200" },
    "test": { "uniqueId": "mylocal" }
  }
}`
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(shared), 0o644); err != nil {
		t.Fatalf("write shared: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.local.json"), []byte(local), 0o644); err != nil {
		t.Fatalf("write local: %v", err)
	}
	return root
}

func winnerLayer(report Report, field string) (string, bool) {
	for _, tr := range report.Trace {
		if tr.Field == field {
			return tr.Winner.Layer, true
		}
	}
	return "", false
}

func TestResolve_ProfileOverlaysBaseAndLocalOverlaysShared(t *testing.T) {
	root := writeProfileFixture(t)
	sources := ProjectSources(root, Config{})

	if got := sources.DefaultProfile; got != "local" {
		t.Fatalf("DefaultProfile: got %q want %q (local declaration must win)", got, "local")
	}

	report := Resolve(Input{Profile: "test", Explain: true}, sources)

	if report.ProfileError != "" {
		t.Fatalf("unexpected profile error: %s", report.ProfileError)
	}
	if report.ActiveProfile != "test" {
		t.Fatalf("ActiveProfile: got %q want %q", report.ActiveProfile, "test")
	}
	if want := []string{"local", "staging", "test"}; !equalStrings(report.AvailableProfiles, want) {
		t.Fatalf("AvailableProfiles: got %v want %v", report.AvailableProfiles, want)
	}

	// Endpoint comes from the shared profile layer (registry mode).
	if report.Target.Mode != ModeRegistry {
		t.Fatalf("Mode: got %q want %q", report.Target.Mode, ModeRegistry)
	}
	if report.Target.RegistryAddress != "zookeeper://zk-test.example.com:2181" {
		t.Fatalf("RegistryAddress: got %q", report.Target.RegistryAddress)
	}
	if layer, _ := winnerLayer(report, "registryAddress"); layer != "project:profiles[test]" {
		t.Fatalf("registryAddress winner layer: got %q want %q", layer, "project:profiles[test]")
	}

	// A base field (protocol) is inherited from the shared base, not a profile.
	if report.Target.Protocol != "bolt" {
		t.Fatalf("Protocol: got %q want bolt", report.Target.Protocol)
	}
	if layer, _ := winnerLayer(report, "protocol"); layer != "project" {
		t.Fatalf("protocol winner layer: got %q want %q", layer, "project")
	}

	// The local profile overlays the shared profile for the same key.
	if report.Target.UniqueID != "mylocal" {
		t.Fatalf("UniqueID: got %q want mylocal", report.Target.UniqueID)
	}
	if layer, _ := winnerLayer(report, "uniqueId"); layer != "project-local:profiles[test]" {
		t.Fatalf("uniqueId winner layer: got %q want %q", layer, "project-local:profiles[test]")
	}
}

func TestResolve_DefaultProfileUsedWhenNoneSelected(t *testing.T) {
	root := writeProfileFixture(t)
	sources := ProjectSources(root, Config{})

	report := Resolve(Input{}, sources) // no per-call profile -> defaultProfile "local"

	if report.ActiveProfile != "local" {
		t.Fatalf("ActiveProfile: got %q want local", report.ActiveProfile)
	}
	if report.Target.Mode != ModeDirect {
		t.Fatalf("Mode: got %q want %q", report.Target.Mode, ModeDirect)
	}
	if report.Target.DirectURL != "bolt://127.0.0.1:12200" {
		t.Fatalf("DirectURL: got %q", report.Target.DirectURL)
	}
}

func TestResolve_UnknownProfileIsHardError(t *testing.T) {
	root := writeProfileFixture(t)
	sources := ProjectSources(root, Config{})

	report := Resolve(Input{Profile: "prod"}, sources)

	if report.ProfileError == "" {
		t.Fatalf("expected ProfileError for undefined profile")
	}
	if report.Target.Mode != "" {
		t.Fatalf("undefined profile must not resolve a target, got mode %q", report.Target.Mode)
	}
	// Error must enumerate available profiles so the fix is obvious.
	for _, name := range []string{"local", "staging", "test"} {
		if !contains(report.ProfileError, name) {
			t.Fatalf("ProfileError %q should list available profile %q", report.ProfileError, name)
		}
	}
}

func TestResolve_PerCallProfileBeatsDefault(t *testing.T) {
	root := writeProfileFixture(t)
	sources := ProjectSources(root, Config{})

	report := Resolve(Input{Profile: "staging"}, sources)

	if report.ActiveProfile != "staging" {
		t.Fatalf("ActiveProfile: got %q want staging", report.ActiveProfile)
	}
	if report.Target.RegistryAddress != "zookeeper://zk-staging.example.com:2181" {
		t.Fatalf("RegistryAddress: got %q", report.Target.RegistryAddress)
	}
}

func equalStrings(a, b []string) bool {
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

func contains(haystack, needle string) bool {
	return len(needle) == 0 || (len(haystack) >= len(needle) && indexOf(haystack, needle) >= 0)
}

func indexOf(haystack, needle string) int {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return i
		}
	}
	return -1
}
