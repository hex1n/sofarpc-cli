package mcp

import (
	"testing"

	"github.com/hex1n/sofarpc-cli/internal/core/contract"
	"github.com/hex1n/sofarpc-cli/internal/javamodel"
)

func TestContractHolder_NilReceiverIsSafe(t *testing.T) {
	var h *contractHolder
	if got := h.Get(); got != nil {
		t.Fatalf("Get on nil holder: %#v", got)
	}
	if got := h.LoadError(); got != "" {
		t.Fatalf("LoadError on nil holder: %q", got)
	}
	// Set on nil must not panic.
	h.Set(contract.NewInMemoryStore(), "ignored")
}

func TestContractHolder_SetAtomicallyReplacesStoreAndError(t *testing.T) {
	// Start with a load error and no store — the shape the entrypoint
	// hands the holder when the sync path failed.
	h := newContractHolder(nil, "initial failure", nil)
	if h.Get() != nil {
		t.Fatalf("Get before Set: want nil")
	}
	if h.LoadError() != "initial failure" {
		t.Fatalf("LoadError before Set: got %q", h.LoadError())
	}

	// A later async loader succeeded; Set must clear the stale error.
	store := contract.NewInMemoryStore(javamodel.Class{FQN: "com.foo.X"})
	h.Set(store, "")
	if h.Get() == nil {
		t.Fatal("Get after Set: got nil store")
	}
	if h.LoadError() != "" {
		t.Fatalf("LoadError after Set: got %q want empty", h.LoadError())
	}
}

func TestContractHolder_LazyLoaderRunsOnce(t *testing.T) {
	calls := 0
	store := contract.NewInMemoryStore(javamodel.Class{FQN: "com.foo.Lazy"})
	h := newContractHolder(nil, "", func() (contract.Store, error) {
		calls++
		return store, nil
	})
	if calls != 0 {
		t.Fatalf("loader should not run at construction, calls=%d", calls)
	}
	if got := h.Get(); got == nil {
		t.Fatal("Get should return loaded store")
	}
	if calls != 1 {
		t.Fatalf("loader calls after first Get: got %d want 1", calls)
	}
	if h.LoadError() != "" {
		t.Fatalf("LoadError after successful load: got %q", h.LoadError())
	}
	if calls != 1 {
		t.Fatalf("loader should not rerun, calls=%d", calls)
	}
	if _, ok := h.Get().Class("com.foo.Lazy"); !ok {
		t.Fatal("loaded store should contain com.foo.Lazy")
	}
}

func TestContractHolder_ForProjectCachesPerProjectRoot(t *testing.T) {
	calls := map[string]int{}
	projectA := t.TempDir()
	projectB := t.TempDir()
	h := newContractHolder(nil, "", nil)
	h.SetProjectLoader(func(projectRoot string) (contract.Store, error) {
		calls[projectRoot]++
		return contract.NewInMemoryStore(javamodel.Class{FQN: "com.foo.Project"}), nil
	})

	firstA := h.ForProject(projectA)
	secondA := h.ForProject(projectA)
	firstB := h.ForProject(projectB)

	if firstA.root != canonicalProjectRoot(projectA) || secondA.root != canonicalProjectRoot(projectA) || firstB.root != canonicalProjectRoot(projectB) {
		t.Fatalf("unexpected roots: firstA=%q secondA=%q firstB=%q", firstA.root, secondA.root, firstB.root)
	}
	if calls[canonicalProjectRoot(projectA)] != 1 || calls[canonicalProjectRoot(projectB)] != 1 {
		t.Fatalf("project loader calls = %+v, want one call per project", calls)
	}
	if firstA.store == firstB.store {
		t.Fatal("different projects should not share a contract store")
	}
}

func TestContractHolder_ForProjectCacheEvictsOldestProject(t *testing.T) {
	calls := map[string]int{}
	roots := make([]string, defaultProjectContractCacheMax+1)
	for i := range roots {
		roots[i] = t.TempDir()
	}
	h := newContractHolder(nil, "", nil)
	h.SetProjectLoader(func(projectRoot string) (contract.Store, error) {
		calls[projectRoot]++
		return contract.NewInMemoryStore(javamodel.Class{FQN: "com.foo.Project"}), nil
	})

	for _, root := range roots {
		h.ForProject(root)
	}
	h.ForProject(roots[0])

	if calls[canonicalProjectRoot(roots[0])] != 2 {
		t.Fatalf("oldest project should be evicted and reloaded, calls=%+v", calls)
	}
	if got := len(h.projects); got > defaultProjectContractCacheMax {
		t.Fatalf("project cache size: got %d want <= %d", got, defaultProjectContractCacheMax)
	}
}

func TestContractHolder_ForProjectFallsBackToDefaultStoreWithoutProjectLoader(t *testing.T) {
	defaultRoot := t.TempDir()
	store := contract.NewInMemoryStore(javamodel.Class{FQN: "com.foo.Default"})
	h := newContractHolder(store, "", nil)
	h.SetDefaultRoot(defaultRoot)

	snapshot := h.ForProject("other-root")

	if snapshot.store != store {
		t.Fatal("ForProject should return the default store when no project loader is configured")
	}
	if snapshot.root != canonicalProjectRoot(defaultRoot) {
		t.Fatalf("root: got %q want %q", snapshot.root, canonicalProjectRoot(defaultRoot))
	}
}
