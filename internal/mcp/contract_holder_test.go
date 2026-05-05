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
