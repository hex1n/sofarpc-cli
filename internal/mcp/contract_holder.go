package mcp

import (
	"sync"

	"github.com/hex1n/sofarpc-cli/internal/core/contract"
)

// contractHolder owns the current contract store and its load error
// behind a mutex so the MCP handlers can share one stable snapshot.
// Readers take a snapshot via Get / LoadError; when a loader is present,
// the first reader loads the store exactly once.
//
// A nil holder and a holder whose inner store is nil both mean "no
// contract configured" — handlers decide by checking the store returned
// from Get against nil, preserving the pre-refactor semantic.
type contractHolder struct {
	mu        sync.RWMutex
	once      sync.Once
	store     contract.Store
	loadError string
	loader    func() (contract.Store, error)
}

func newContractHolder(store contract.Store, loadError string, loader func() (contract.Store, error)) *contractHolder {
	return &contractHolder{store: store, loadError: loadError, loader: loader}
}

func (h *contractHolder) Get() contract.Store {
	if h == nil {
		return nil
	}
	h.ensureLoaded()
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.store
}

func (h *contractHolder) LoadError() string {
	if h == nil {
		return ""
	}
	h.ensureLoaded()
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.loadError
}

// Set atomically swaps both the store and its load-error message so
// readers never see a partially-updated state (e.g. a newly-loaded
// store paired with a stale error from the previous attempt).
func (h *contractHolder) Set(store contract.Store, loadError string) {
	if h == nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.store = store
	h.loadError = loadError
}

func (h *contractHolder) ensureLoaded() {
	if h == nil || h.loader == nil {
		return
	}
	h.once.Do(func() {
		store, err := h.loader()
		h.Set(store, loadErrorMessage(err))
	})
}
