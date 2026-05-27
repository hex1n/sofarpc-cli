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
	mu            sync.RWMutex
	once          sync.Once
	store         contract.Store
	loadError     string
	loader        func() (contract.Store, error)
	defaultRoot   string
	projectLoader func(string) (contract.Store, error)
	projects      map[string]*projectContractEntry
}

type contractSnapshot struct {
	store     contract.Store
	loadError string
	root      string
}

type projectContractEntry struct {
	once      sync.Once
	root      string
	store     contract.Store
	loadError string
	loader    func(string) (contract.Store, error)
}

func newContractHolder(store contract.Store, loadError string, loader func() (contract.Store, error)) *contractHolder {
	return &contractHolder{store: store, loadError: loadError, loader: loader, projects: map[string]*projectContractEntry{}}
}

func (h *contractHolder) Get() contract.Store {
	return h.Default().store
}

func (h *contractHolder) LoadError() string {
	return h.Default().loadError
}

func (h *contractHolder) Default() contractSnapshot {
	if h == nil {
		return contractSnapshot{}
	}
	h.ensureLoaded()
	h.mu.RLock()
	defer h.mu.RUnlock()
	return contractSnapshot{store: h.store, loadError: h.loadError, root: h.defaultRoot}
}

func (h *contractHolder) ForProject(projectRoot string) contractSnapshot {
	if h == nil {
		return contractSnapshot{}
	}
	projectRoot = canonicalProjectRoot(projectRoot)
	h.mu.RLock()
	loader := h.projectLoader
	h.mu.RUnlock()
	if projectRoot == "" || loader == nil {
		return h.Default()
	}
	h.mu.Lock()
	entry := h.projects[projectRoot]
	if entry == nil {
		entry = &projectContractEntry{root: projectRoot, loader: loader}
		h.projects[projectRoot] = entry
	}
	h.mu.Unlock()

	entry.once.Do(func() {
		store, err := entry.loader(projectRoot)
		entry.store = store
		entry.loadError = loadErrorMessage(err)
	})
	return contractSnapshot{store: entry.store, loadError: entry.loadError, root: entry.root}
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

func (h *contractHolder) SetDefaultRoot(projectRoot string) {
	if h == nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.defaultRoot = canonicalProjectRoot(projectRoot)
}

func (h *contractHolder) SetProjectLoader(loader func(string) (contract.Store, error)) {
	if h == nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.projectLoader = loader
	h.projects = map[string]*projectContractEntry{}
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
