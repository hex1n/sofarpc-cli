package mcp

import (
	"sync"

	"github.com/hex1n/sofarpc-cli/internal/core/contract"
)

// contractHolder owns the current contract store behind a mutex so the MCP
// handlers can share one stable snapshot. Readers take a snapshot via Get;
// writers replace the inner store via Set.
//
// A nil holder and a holder whose inner store is nil both mean "no
// contract configured" — handlers decide by checking the store returned
// from Get against nil, preserving the pre-refactor semantic.
type contractHolder struct {
	mu    sync.RWMutex
	store contract.Store
}

func newContractHolder(store contract.Store) *contractHolder {
	return &contractHolder{store: store}
}

func (h *contractHolder) Get() contract.Store {
	if h == nil {
		return nil
	}
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.store
}

func (h *contractHolder) Set(store contract.Store) {
	if h == nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.store = store
}
