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
	h := newContractHolder(nil, "initial failure")
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
