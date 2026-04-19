package workspace

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/hex1n/sofarpc-cli/internal/core/target"
)

func TestResolve_UsesCwdWhenProjectEmpty(t *testing.T) {
	dir := t.TempDir()
	ws, err := Resolve(Input{Cwd: dir})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if ws.ProjectRoot != dir {
		t.Fatalf("projectRoot: got %q want %q", ws.ProjectRoot, dir)
	}
}

func TestResolve_RelativeProjectJoinsCwd(t *testing.T) {
	root := t.TempDir()
	sub := filepath.Join(root, "svc")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	ws, err := Resolve(Input{Cwd: root, Project: "svc"})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if ws.ProjectRoot != sub {
		t.Fatalf("projectRoot: got %q want %q", ws.ProjectRoot, sub)
	}
}

func TestResolve_ProjectRootMustExist(t *testing.T) {
	_, err := Resolve(Input{Project: "/definitely/does/not/exist"})
	if err == nil {
		t.Fatal("resolve should fail for a non-existent project root")
	}
}

func TestResolve_ProjectRootMustBeDirectory(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "not-a-dir")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	_, err := Resolve(Input{Project: file})
	if err == nil {
		t.Fatal("resolve should fail when project root is a regular file")
	}
}

// Both Cwd and Project empty exercises the os.Getwd fallback inside
// resolveRoot. The result must match the process CWD after symlink
// resolution that filepath.Abs performs.
func TestResolve_EmptyInputFallsBackToCWD(t *testing.T) {
	ws, err := Resolve(Input{})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	wantAbs, err := filepath.Abs(wd)
	if err != nil {
		t.Fatalf("abs: %v", err)
	}
	// Match both paths through EvalSymlinks so /var vs /private/var on
	// macOS doesn't make the comparison flaky.
	got, err := filepath.EvalSymlinks(ws.ProjectRoot)
	if err != nil {
		t.Fatalf("evalSymlinks: %v", err)
	}
	want, err := filepath.EvalSymlinks(wantAbs)
	if err != nil {
		t.Fatalf("evalSymlinks: %v", err)
	}
	if got != want {
		t.Fatalf("projectRoot: got %q want %q", got, want)
	}
}

// A relative Project with no Cwd must be joined to the process CWD, not
// treated as rooted.
func TestResolve_RelativeProjectWithoutCwdUsesProcessCWD(t *testing.T) {
	root := t.TempDir()
	sub := filepath.Join(root, "svc")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	prev, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(prev) })
	if err := os.Chdir(root); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	ws, err := Resolve(Input{Project: "svc"})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	// On macOS, /var is a symlink to /private/var — os.Getwd canonicalises
	// to the real path, so normalise the expected path the same way before
	// comparing.
	wantAbs, err := filepath.EvalSymlinks(sub)
	if err != nil {
		t.Fatalf("evalSymlinks: %v", err)
	}
	if ws.ProjectRoot != wantAbs {
		t.Fatalf("projectRoot: got %q want %q", ws.ProjectRoot, wantAbs)
	}
}

func TestSources_PropagatesEnvAndProjectRoot(t *testing.T) {
	ws := Workspace{ProjectRoot: "/tmp/proj"}
	src := ws.Sources(target.Config{Serialization: "fastjson2"})
	if src.Env.Serialization != "fastjson2" {
		t.Fatal("env serialization should propagate")
	}
	if src.ProjectRoot != "/tmp/proj" {
		t.Fatalf("projectRoot: got %q", src.ProjectRoot)
	}
}
