package runtime

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/hex1n/sofarpc-cli/internal/model"
)

func TestClasspathContentKeyEmpty(t *testing.T) {
	got, err := classpathContentKey(nil)
	if err != nil {
		t.Fatalf("classpathContentKey(nil) error = %v", err)
	}
	if got == "" {
		t.Fatal("expected a non-empty key for empty stub list")
	}
}

func TestClasspathContentKeyOrderIndependent(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "a.jar")
	second := filepath.Join(dir, "b.jar")
	if err := os.WriteFile(first, []byte("alpha"), 0o644); err != nil {
		t.Fatalf("write a.jar: %v", err)
	}
	if err := os.WriteFile(second, []byte("beta"), 0o644); err != nil {
		t.Fatalf("write b.jar: %v", err)
	}
	forward, err := classpathContentKey([]string{first, second})
	if err != nil {
		t.Fatalf("forward: %v", err)
	}
	reverse, err := classpathContentKey([]string{second, first})
	if err != nil {
		t.Fatalf("reverse: %v", err)
	}
	if forward != reverse {
		t.Fatalf("expected stable key regardless of input order, forward=%s reverse=%s", forward, reverse)
	}
}

func TestClasspathContentKeyAllowsMissingStubs(t *testing.T) {
	key, err := classpathContentKeyWithPolicy([]string{"/tmp/no-such-stub.jar"}, true)
	if err != nil {
		t.Fatalf("expected missing stub path to be accepted in fallback mode: %v", err)
	}
	if key == "" {
		t.Fatal("expected a non-empty key")
	}
}

func TestClasspathContentKeyRequiresExistingStubs(t *testing.T) {
	_, err := classpathContentKeyWithPolicy([]string{"/tmp/no-such-stub.jar"}, false)
	if err == nil {
		t.Fatal("expected missing stub path to fail in strict mode")
	}
}

func TestClasspathContentKeyChangesOnContentChange(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.jar")
	if err := os.WriteFile(path, []byte("alpha"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	first, err := classpathContentKey([]string{path})
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	if err := os.WriteFile(path, []byte("different"), 0o644); err != nil {
		t.Fatalf("rewrite: %v", err)
	}
	second, err := classpathContentKey([]string{path})
	if err != nil {
		t.Fatalf("second: %v", err)
	}
	if first == second {
		t.Fatal("expected key to change when stub content changes")
	}
}

func TestReadSchemaCacheMissing(t *testing.T) {
	_, ok, err := readSchemaCache(filepath.Join(t.TempDir(), "no-such.json"))
	if err != nil {
		t.Fatalf("expected nil error on missing cache, got %v", err)
	}
	if ok {
		t.Fatal("expected ok=false for missing cache")
	}
}

func TestReadSchemaCacheValid(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "schema.json")
	sample := model.ServiceSchema{
		Service: "com.example.UserService",
		Methods: []model.MethodSchema{{Name: "getUser", ParamTypes: []string{"java.lang.Long"}, ReturnType: "com.example.User"}},
	}
	body, err := json.Marshal(sample)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	got, ok, err := readSchemaCache(path)
	if err != nil {
		t.Fatalf("readSchemaCache error = %v", err)
	}
	if !ok {
		t.Fatal("expected ok=true")
	}
	if got.Service != sample.Service || len(got.Methods) != 1 || got.Methods[0].Name != "getUser" {
		t.Fatalf("unexpected schema: %+v", got)
	}
}

func TestReadSchemaCacheCorrupt(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "schema.json")
	if err := os.WriteFile(path, []byte("not json"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	_, _, err := readSchemaCache(path)
	if err == nil {
		t.Fatal("expected error on corrupt cache")
	}
}

func TestBuildClasspathOrderAndSeparator(t *testing.T) {
	got := buildClasspath("/tmp/worker.jar", []string{"/tmp/a.jar", "/tmp/b.jar"})
	sep := string(os.PathListSeparator)
	want := "/tmp/worker.jar" + sep + "/tmp/a.jar" + sep + "/tmp/b.jar"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}
