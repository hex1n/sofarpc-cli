package indexer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// --- TestMain self-exec stub --------------------------------------------
//
// FAKE_INDEXER_MODE turns this test binary into a fake spoon-indexer when
// re-executed by Run. Each mode exercises a different code path (happy /
// incremental / fail / slow).

const envIndexerMode = "FAKE_INDEXER_MODE"

func TestMain(m *testing.M) {
	mode := os.Getenv(envIndexerMode)
	if mode == "" {
		os.Exit(m.Run())
	}
	runFakeIndexer(mode)
}

func runFakeIndexer(mode string) {
	output, sinceMS := parseFakeArgs(os.Args)
	switch mode {
	case "happy":
		requireOutput(output)
		if err := writeFakeIndex(output, 2); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
		os.Exit(0)
	case "empty":
		requireOutput(output)
		if err := writeFakeIndex(output, 0); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
		os.Exit(0)
	case "incremental":
		requireOutput(output)
		if sinceMS == "" {
			fmt.Fprintln(os.Stderr, "fake indexer: --since missing in incremental mode")
			os.Exit(3)
		}
		if err := writeFakeIndex(output, 1); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
		os.Exit(0)
	case "fail":
		fmt.Fprintln(os.Stderr, "fake indexer: intentional failure")
		os.Exit(2)
	case "slow":
		time.Sleep(30 * time.Second)
		os.Exit(0)
	default:
		fmt.Fprintf(os.Stderr, "unknown FAKE_INDEXER_MODE=%s\n", mode)
		os.Exit(2)
	}
}

func requireOutput(output string) {
	if output == "" {
		fmt.Fprintln(os.Stderr, "fake indexer: missing --output")
		os.Exit(2)
	}
}

func parseFakeArgs(argv []string) (output string, sinceMS string) {
	for i, a := range argv {
		if i+1 >= len(argv) {
			break
		}
		switch a {
		case "--output":
			output = argv[i+1]
		case "--since":
			sinceMS = argv[i+1]
		}
	}
	return
}

func writeFakeIndex(dir string, classCount int) error {
	if err := os.MkdirAll(filepath.Join(dir, "shards"), 0o755); err != nil {
		return err
	}
	meta := Meta{Version: 1, Generated: time.Now().UnixMilli(), Classes: map[string]string{}}
	for i := 0; i < classCount; i++ {
		fqn := fmt.Sprintf("com.fake.Svc%d", i)
		rel := fmt.Sprintf("shards/svc%d.json", i)
		shard, err := json.Marshal(map[string]any{"fqn": fqn, "kind": "interface"})
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(dir, rel), shard, 0o644); err != nil {
			return err
		}
		meta.Classes[fqn] = rel
	}
	body, err := json.Marshal(meta)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, MetaFilename), body, 0o644)
}

func fakeIndexerSpec(t *testing.T, mode string) Spec {
	t.Helper()
	root := t.TempDir()
	src := filepath.Join(root, "src/main/java")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatalf("mkdir src: %v", err)
	}
	return Spec{
		Java:        os.Args[0],
		Jar:         "ignored",
		ProjectRoot: root,
		Sources:     []string{src},
		ExtraArgs:   []string{"-test.run=^$"},
		Env:         []string{envIndexerMode + "=" + mode},
		Timeout:     5 * time.Second,
	}
}

// --- End-to-end subprocess tests ----------------------------------------

func TestRun_HappyPathWritesIndexAndReportsClasses(t *testing.T) {
	if testing.Short() {
		t.Skip("self-exec spawn test")
	}
	spec := fakeIndexerSpec(t, "happy")
	summary, err := Run(context.Background(), spec)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if summary.Classes != 2 {
		t.Fatalf("classes: got %d want 2", summary.Classes)
	}
	if summary.Incremental {
		t.Fatal("full scan should report Incremental=false")
	}
	if summary.Duration <= 0 {
		t.Fatalf("duration should be positive, got %s", summary.Duration)
	}
	if !strings.HasSuffix(filepath.ToSlash(summary.Output), "/"+DirName) {
		t.Fatalf("output path %q should end with %q", summary.Output, DirName)
	}
}

func TestRun_IncrementalPassesSinceArg(t *testing.T) {
	if testing.Short() {
		t.Skip("self-exec spawn test")
	}
	spec := fakeIndexerSpec(t, "incremental")
	spec.Since = time.Unix(1700000000, 0)
	summary, err := Run(context.Background(), spec)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !summary.Incremental {
		t.Fatal("Incremental should be true when Since is set")
	}
	if summary.Classes != 1 {
		t.Fatalf("classes: got %d want 1", summary.Classes)
	}
}

func TestRun_IncrementalOmittedWhenSinceZero(t *testing.T) {
	if testing.Short() {
		t.Skip("self-exec spawn test")
	}
	// The incremental fake exits 3 when --since is missing — if Run
	// leaks --since=0 on a zero time, we'd hit the happy path instead.
	spec := fakeIndexerSpec(t, "incremental")
	_, err := Run(context.Background(), spec)
	if err == nil {
		t.Fatal("expected failure when Since is zero (no --since sent)")
	}
}

func TestRun_FailingProcessSurfacesError(t *testing.T) {
	if testing.Short() {
		t.Skip("self-exec spawn test")
	}
	spec := fakeIndexerSpec(t, "fail")
	_, err := Run(context.Background(), spec)
	if err == nil {
		t.Fatal("expected run error")
	}
	if !strings.HasPrefix(err.Error(), "indexer: run:") {
		t.Fatalf("expected 'indexer: run:' prefix, got %v", err)
	}
}

func TestRun_TimeoutAbortsSlowProcess(t *testing.T) {
	if testing.Short() {
		t.Skip("self-exec spawn test")
	}
	spec := fakeIndexerSpec(t, "slow")
	spec.Timeout = 200 * time.Millisecond
	start := time.Now()
	_, err := Run(context.Background(), spec)
	elapsed := time.Since(start)
	if err == nil {
		t.Fatal("expected timeout error")
	}
	if !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("expected 'timed out' error, got %v", err)
	}
	if elapsed > 3*time.Second {
		t.Fatalf("timeout should abort promptly, elapsed=%s", elapsed)
	}
}

func TestRun_ContextCancelShortCircuits(t *testing.T) {
	if testing.Short() {
		t.Skip("self-exec spawn test")
	}
	spec := fakeIndexerSpec(t, "slow")
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()
	_, err := Run(ctx, spec)
	if err == nil {
		t.Fatal("expected cancellation error")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
}

func TestRun_EmptyIndexLoadsAsZero(t *testing.T) {
	if testing.Short() {
		t.Skip("self-exec spawn test")
	}
	spec := fakeIndexerSpec(t, "empty")
	summary, err := Run(context.Background(), spec)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if summary.Classes != 0 {
		t.Fatalf("classes: got %d want 0", summary.Classes)
	}
}

func TestRun_ProducedIndexIsLoadable(t *testing.T) {
	if testing.Short() {
		t.Skip("self-exec spawn test")
	}
	spec := fakeIndexerSpec(t, "happy")
	if _, err := Run(context.Background(), spec); err != nil {
		t.Fatalf("run: %v", err)
	}
	idx, err := Load(spec.ProjectRoot)
	if err != nil {
		t.Fatalf("load produced index: %v", err)
	}
	services := idx.Services()
	if len(services) != 2 {
		t.Fatalf("services: got %d entries want 2", len(services))
	}
}

// --- Pure validation (no subprocess) ------------------------------------

func TestRun_RejectsIncompleteSpec(t *testing.T) {
	cases := []struct {
		name string
		spec Spec
		want string
	}{
		{"missing jar", Spec{ProjectRoot: "/x", Sources: []string{"/s"}}, "Jar is required"},
		{"missing project", Spec{Jar: "j", Sources: []string{"/s"}}, "ProjectRoot is required"},
		{"missing sources", Spec{Jar: "j", ProjectRoot: "/x"}, "Source is required"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Run(context.Background(), tc.spec)
			if err == nil {
				t.Fatal("expected validation error")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error %q should mention %q", err.Error(), tc.want)
			}
		})
	}
}
