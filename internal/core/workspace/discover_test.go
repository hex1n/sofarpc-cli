package workspace

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// canonicalTempDir resolves t.TempDir() through symlinks so expected roots
// compare equal with discovery output (/var vs /private/var on macOS).
func canonicalTempDir(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("EvalSymlinks: %v", err)
	}
	return dir
}

func TestDiscoverJavaProject_FindsUniqueHighConfidenceRoot(t *testing.T) {
	root := canonicalTempDir(t)
	mkdir(t, filepath.Join(root, ".git"))
	writeFile(t, filepath.Join(root, "pom.xml"), "<project/>")
	writeFile(t, filepath.Join(root, "src", "main", "java", "com", "foo", "UserFacade.java"), "package com.foo; public interface UserFacade {}")
	start := filepath.Join(root, "src", "main", "java", "com", "foo")

	got, err := DiscoverJavaProject(start)
	if err != nil {
		t.Fatalf("DiscoverJavaProject: %v", err)
	}
	if got.Root != root || got.Confidence != DiscoveryConfidenceHigh {
		t.Fatalf("discovery = %+v, want root %s high", got, root)
	}
	if !contains(got.Markers, "pom.xml") || !contains(got.Markers, "src/main/java") || !contains(got.Markers, ".git") {
		t.Fatalf("markers missing expected evidence: %+v", got.Markers)
	}
}

func TestDiscoverJavaProject_ExistingSofaConfigWins(t *testing.T) {
	root := canonicalTempDir(t)
	writeFile(t, filepath.Join(root, ".sofarpc", "config.local.json"), "{}")

	got, err := DiscoverJavaProject(root)
	if err != nil {
		t.Fatalf("DiscoverJavaProject: %v", err)
	}
	if got.Root != root || got.Confidence != DiscoveryConfidenceHigh {
		t.Fatalf("discovery = %+v, want existing config root", got)
	}
	if !contains(got.Markers, ".sofarpc/config") {
		t.Fatalf("markers missing .sofarpc/config: %+v", got.Markers)
	}
}

func TestDiscoverJavaProject_RejectsMultipleHighConfidenceCandidates(t *testing.T) {
	root := canonicalTempDir(t)
	mkdir(t, filepath.Join(root, ".git"))
	writeFile(t, filepath.Join(root, "pom.xml"), "<project/>")
	writeFile(t, filepath.Join(root, "module-a", "pom.xml"), "<project/>")
	writeFile(t, filepath.Join(root, "module-a", "src", "main", "java", "com", "foo", "UserFacade.java"), "package com.foo; public interface UserFacade {}")

	got, err := DiscoverJavaProject(filepath.Join(root, "module-a"))
	if err != nil {
		t.Fatalf("DiscoverJavaProject: %v", err)
	}
	if got.Root != "" || got.Confidence != DiscoveryConfidenceLow {
		t.Fatalf("ambiguous discovery should not choose a root: %+v", got)
	}
	if !strings.Contains(got.Reason, "multiple high-confidence") {
		t.Fatalf("reason should explain ambiguity: %+v", got)
	}
	if len(got.Candidates) < 2 {
		t.Fatalf("expected candidates for agent follow-up: %+v", got.Candidates)
	}
}

func TestDiscoverJavaProject_FindsLargeMultiModuleRootWithoutFullTreeScan(t *testing.T) {
	root := canonicalTempDir(t)
	mkdir(t, filepath.Join(root, ".git"))
	writeFile(t, filepath.Join(root, "pom.xml"), "<project/>")
	for i := 0; i < 2105; i++ {
		writeFile(t, filepath.Join(root, "aaa-noise", fmt.Sprintf("file-%04d.txt", i)), "x")
	}
	writeFile(t, filepath.Join(root, "zzz-module", "src", "main", "java", "com", "foo", "UserFacade.java"), "package com.foo; public interface UserFacade {}")

	got, err := DiscoverJavaProject(root)
	if err != nil {
		t.Fatalf("DiscoverJavaProject: %v", err)
	}
	if got.Root != root || got.Confidence != DiscoveryConfidenceHigh {
		t.Fatalf("discovery = %+v, want large multi-module root high", got)
	}
	if !contains(got.Markers, "nested-src/main/java") {
		t.Fatalf("markers should include nested source evidence: %+v", got.Markers)
	}
}

func TestDiscoverJavaProject_GitOnlyIsNotCandidate(t *testing.T) {
	root := canonicalTempDir(t)
	mkdir(t, filepath.Join(root, ".git"))

	got, err := DiscoverJavaProject(root)
	if err != nil {
		t.Fatalf("DiscoverJavaProject: %v", err)
	}
	if got.Root != "" || got.Confidence != DiscoveryConfidenceLow {
		t.Fatalf("git-only directory should not be selected: %+v", got)
	}
	for _, candidate := range got.Candidates {
		if candidate.Root == root {
			t.Fatalf("git-only directory should not be a candidate: %+v", got)
		}
	}
}

func TestDiscoverJavaProject_ResolvesSymlinkedStart(t *testing.T) {
	base := canonicalTempDir(t)
	root := filepath.Join(base, "real")
	mkdir(t, filepath.Join(root, ".git"))
	writeFile(t, filepath.Join(root, "pom.xml"), "<project/>")
	writeFile(t, filepath.Join(root, "src", "main", "java", "com", "foo", "UserFacade.java"), "package com.foo; public interface UserFacade {}")
	link := filepath.Join(base, "link")
	if err := os.Symlink(root, link); err != nil {
		t.Skipf("symlink unsupported on this platform: %v", err)
	}

	got, err := DiscoverJavaProject(link)
	if err != nil {
		t.Fatalf("DiscoverJavaProject: %v", err)
	}
	if got.Root != root || got.Confidence != DiscoveryConfidenceHigh {
		t.Fatalf("discovery = %+v, want canonical root %s high", got, root)
	}
}

func TestDiscoverJavaProject_StopsAtGitRoot(t *testing.T) {
	outer := canonicalTempDir(t)
	writeFile(t, filepath.Join(outer, "pom.xml"), "<project/>")
	writeFile(t, filepath.Join(outer, "src", "main", "java", "com", "foo", "Outer.java"), "package com.foo; public interface Outer {}")
	inner := filepath.Join(outer, "repo")
	mkdir(t, filepath.Join(inner, ".git"))
	writeFile(t, filepath.Join(inner, "pom.xml"), "<project/>")
	writeFile(t, filepath.Join(inner, "src", "main", "java", "com", "foo", "UserFacade.java"), "package com.foo; public interface UserFacade {}")

	got, err := DiscoverJavaProject(inner)
	if err != nil {
		t.Fatalf("DiscoverJavaProject: %v", err)
	}
	if got.Root != inner || got.Confidence != DiscoveryConfidenceHigh {
		t.Fatalf("discovery = %+v, want git root %s high", got, inner)
	}
	for _, candidate := range got.Candidates {
		if candidate.Root == outer {
			t.Fatalf("walk should stop at git root, not evaluate %s: %+v", outer, got.Candidates)
		}
	}
}

func TestDiscoverJavaProject_SkipsMarkerlessAncestorsOutsideGit(t *testing.T) {
	base := canonicalTempDir(t)
	start := filepath.Join(base, "app")
	writeFile(t, filepath.Join(start, "pom.xml"), "<project/>")
	writeFile(t, filepath.Join(base, "other", "src", "main", "java", "com", "foo", "Noise.java"), "package com.foo; public interface Noise {}")

	got, err := DiscoverJavaProject(start)
	if err != nil {
		t.Fatalf("DiscoverJavaProject: %v", err)
	}
	for _, candidate := range got.Candidates {
		if candidate.Root == base {
			t.Fatalf("marker-less ancestor should not be content-scanned into a candidate: %+v", got.Candidates)
		}
	}
	if len(got.Candidates) != 1 || got.Candidates[0].Root != start {
		t.Fatalf("candidates = %+v, want only start dir %s", got.Candidates, start)
	}
}

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll(%s): %v", path, err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("WriteFile(%s): %v", path, err)
	}
}

func mkdir(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatalf("MkdirAll(%s): %v", path, err)
	}
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
