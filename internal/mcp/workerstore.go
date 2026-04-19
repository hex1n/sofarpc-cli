package mcp

import (
	"context"
	"sync"

	"github.com/hex1n/sofarpc-cli/internal/core/contract"
	"github.com/hex1n/sofarpc-cli/internal/facadesemantic"
	"github.com/hex1n/sofarpc-cli/internal/worker"
)

// describeClient is the minimum Describe surface workerStore needs.
// *worker.Client satisfies it in production; tests inject fakes without
// standing up a pool or fake TCP process.
type describeClient interface {
	Describe(ctx context.Context, service string) (facadesemantic.Class, bool, error)
}

// workerStore is a contract.Store that lazily fetches classes from a
// running worker via reflection (worker.Client.Describe). It exists so
// `sofarpc_describe` can resolve methods + render skeletons for
// codebases that don't have a local Spoon index — only a facade jar
// loaded into the worker's classpath.
//
// Describe walks nested user types during skeleton rendering, which
// triggers a Class() call per type. Each call hits the worker over
// local loopback TCP (<1ms), but a per-store cache keeps repeated walks
// from paying even that cost.
//
// The cache stores both hits and misses so a missing class doesn't
// round-trip on every retry. Cache entries live for the lifetime of the
// store; a facade-jar swap requires recreating the store (which the MCP
// server does on open).
type workerStore struct {
	client describeClient
	ctx    context.Context

	mu      sync.RWMutex
	classes map[string]facadesemantic.Class
	misses  map[string]struct{}
}

// NewWorkerStore wires a Client + ambient context into a contract.Store.
// The context should be the long-lived server context so describe calls
// inherit cancellation but aren't bounded by per-tool deadlines.
//
// Exported so cmd/sofarpc-mcp can assemble it at startup without
// importing internal fixtures.
func NewWorkerStore(ctx context.Context, client *worker.Client) contract.Store {
	if client == nil {
		return nil
	}
	return newWorkerStoreWithClient(ctx, client)
}

func newWorkerStoreWithClient(ctx context.Context, client describeClient) *workerStore {
	if ctx == nil {
		ctx = context.Background()
	}
	return &workerStore{
		client:  client,
		ctx:     ctx,
		classes: map[string]facadesemantic.Class{},
		misses:  map[string]struct{}{},
	}
}

// Class implements contract.Store. Errors from the worker surface as
// ok=false so callers (BuildSkeleton, ResolveMethod) treat them the
// same as any other unknown type. The describe handler's own code path
// still produces a structured error for direct ResolveMethod failures
// because that call goes through the store's Class() → contract layer,
// which rewraps misses into contract.unresolvable.
//
// Note: a nil workerStore is a usable zero value — it just misses on
// everything. This keeps the `facade == nil` check in BuildPlan and
// describe aligned with "no metadata available".
func (s *workerStore) Class(fqn string) (facadesemantic.Class, bool) {
	if s == nil || s.client == nil || fqn == "" {
		return facadesemantic.Class{}, false
	}

	s.mu.RLock()
	if cls, ok := s.classes[fqn]; ok {
		s.mu.RUnlock()
		return cls, true
	}
	if _, miss := s.misses[fqn]; miss {
		s.mu.RUnlock()
		return facadesemantic.Class{}, false
	}
	s.mu.RUnlock()

	cls, ok, err := s.client.Describe(s.ctx, fqn)
	s.mu.Lock()
	defer s.mu.Unlock()
	if err != nil || !ok {
		s.misses[fqn] = struct{}{}
		return facadesemantic.Class{}, false
	}
	s.classes[fqn] = cls
	return cls, true
}

// Compile-time guard that workerStore satisfies contract.Store.
var _ contract.Store = (*workerStore)(nil)
