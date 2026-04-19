package indexer

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/hex1n/sofarpc-cli/internal/facadesemantic"
)

func TestLoad_MissingIndexErrors(t *testing.T) {
	_, err := Load(t.TempDir())
	if err == nil {
		t.Fatal("Load should fail when the manifest is missing")
	}
	if !os.IsNotExist(unwrapPathError(err)) {
		t.Fatalf("error should be os.ErrNotExist-rooted: %v", err)
	}
}

func TestLoad_MalformedManifestErrors(t *testing.T) {
	root := t.TempDir()
	writeMeta(t, root, []byte("{not json"))
	if _, err := Load(root); err == nil {
		t.Fatal("Load should fail on malformed manifest")
	}
}

func TestClass_ReturnsShardAndCaches(t *testing.T) {
	root := t.TempDir()
	writeShard(t, root, "shards/svc.json", facadesemantic.Class{
		FQN:  "com.foo.Svc",
		Kind: facadesemantic.KindInterface,
		Methods: []facadesemantic.Method{
			{Name: "doThing", ParamTypes: []string{"java.lang.String"}, ReturnType: "java.lang.String"},
		},
	})
	writeMeta(t, root, mustJSON(Meta{
		Version: 1,
		Classes: map[string]string{"com.foo.Svc": "shards/svc.json"},
	}))

	idx, err := Load(root)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	cls, ok := idx.Class("com.foo.Svc")
	if !ok {
		t.Fatal("expected Svc to be found")
	}
	if cls.Kind != facadesemantic.KindInterface {
		t.Fatalf("kind: got %q", cls.Kind)
	}

	// Second call must be served from cache — delete the shard to prove it.
	if err := os.Remove(filepath.Join(root, DirName, "shards", "svc.json")); err != nil {
		t.Fatalf("remove shard: %v", err)
	}
	cached, ok := idx.Class("com.foo.Svc")
	if !ok {
		t.Fatal("second lookup should hit cache")
	}
	if cached.FQN != "com.foo.Svc" {
		t.Fatal("cached class should match original")
	}
}

func TestClass_MissingFqnReturnsOkFalse(t *testing.T) {
	root := t.TempDir()
	writeMeta(t, root, mustJSON(Meta{Version: 1, Classes: map[string]string{}}))
	idx, err := Load(root)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if _, ok := idx.Class("com.foo.Nope"); ok {
		t.Fatal("unknown class should return ok=false")
	}
}

func TestClass_StaleShardIsPersistentMiss(t *testing.T) {
	root := t.TempDir()
	writeMeta(t, root, mustJSON(Meta{
		Version: 1,
		Classes: map[string]string{"com.foo.Svc": "shards/gone.json"},
	}))
	idx, err := Load(root)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	// Shard referenced but not on disk — first call misses.
	if _, ok := idx.Class("com.foo.Svc"); ok {
		t.Fatal("first lookup should miss when shard is absent")
	}
	// Second call must not retry the filesystem — we verify this by
	// writing the shard now and confirming the miss persists.
	writeShard(t, root, "shards/gone.json", facadesemantic.Class{FQN: "com.foo.Svc"})
	if _, ok := idx.Class("com.foo.Svc"); ok {
		t.Fatal("second lookup should still miss (persistent miss cache)")
	}
}

func TestServices_FiltersToInterfaces(t *testing.T) {
	root := t.TempDir()
	writeShard(t, root, "shards/svc.json", facadesemantic.Class{FQN: "com.foo.Svc", Kind: facadesemantic.KindInterface})
	writeShard(t, root, "shards/dto.json", facadesemantic.Class{FQN: "com.foo.Dto", Kind: facadesemantic.KindClass})
	writeMeta(t, root, mustJSON(Meta{
		Version: 1,
		Classes: map[string]string{
			"com.foo.Svc": "shards/svc.json",
			"com.foo.Dto": "shards/dto.json",
		},
	}))
	idx, err := Load(root)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	services := idx.Services()
	if len(services) != 1 || services[0] != "com.foo.Svc" {
		t.Fatalf("services: got %v want [com.foo.Svc]", services)
	}
	if idx.Size() != 2 {
		t.Fatalf("size: got %d want 2", idx.Size())
	}
}

func TestNilIndex_SafeToCall(t *testing.T) {
	var idx *Index
	if _, ok := idx.Class("x"); ok {
		t.Fatal("nil index should miss")
	}
	if idx.Size() != 0 {
		t.Fatal("nil index size should be 0")
	}
	if idx.Services() != nil {
		t.Fatal("nil index services should be nil")
	}
}

func writeMeta(t *testing.T, root string, body []byte) {
	t.Helper()
	dir := filepath.Join(root, DirName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, MetaFilename), body, 0o644); err != nil {
		t.Fatalf("write meta: %v", err)
	}
}

func writeShard(t *testing.T, root, rel string, cls facadesemantic.Class) {
	t.Helper()
	path := filepath.Join(root, DirName, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir shard: %v", err)
	}
	body, err := json.Marshal(cls)
	if err != nil {
		t.Fatalf("marshal shard: %v", err)
	}
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatalf("write shard: %v", err)
	}
}

func mustJSON(v any) []byte {
	body, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return body
}

// unwrapPathError peels wrapping errors to expose an underlying *os.PathError
// so os.IsNotExist works even through fmt.Errorf with %w.
func unwrapPathError(err error) error {
	for err != nil {
		if _, ok := err.(*os.PathError); ok {
			return err
		}
		u, ok := err.(interface{ Unwrap() error })
		if !ok {
			return err
		}
		err = u.Unwrap()
	}
	return err
}
