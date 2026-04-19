package mcp

import (
	"context"
	"sync"

	"github.com/hex1n/sofarpc-cli/internal/core/contract"
)

// Reindexer regenerates the facade index on demand and returns a freshly
// loaded Store. Handlers call it when the agent passes refresh=true to
// sofarpc_describe (architecture §6). Implementations are expected to:
//
//  1. Run the Spoon indexer over the project's source roots.
//  2. Reload the resulting on-disk manifest.
//  3. Return the new Store so the holder can atomically swap it in.
//
// Errors are wrapped by the describe handler into errcode.IndexerFailed
// with a hint back to sofarpc_doctor — they denote "indexer subprocess
// could not produce a fresh index," not "index is stale."
type Reindexer interface {
	Reindex(ctx context.Context) (contract.Store, error)
}

// ReindexerFunc adapts a function to the Reindexer interface, matching
// http.HandlerFunc's convention. Useful for tests that want to inject a
// canned response without a full struct.
type ReindexerFunc func(ctx context.Context) (contract.Store, error)

// Reindex implements Reindexer.
func (f ReindexerFunc) Reindex(ctx context.Context) (contract.Store, error) {
	return f(ctx)
}

// dedupReindexer wraps another Reindexer so that concurrent Reindex
// calls collapse into one underlying run. Followers wait on the leader
// and share its result; the flight is discarded as soon as the leader
// returns, so subsequent calls start fresh (no result caching).
//
// The problem this solves: two agents issuing sofarpc_describe
// refresh=true at the same time would otherwise spawn two Spoon
// subprocesses and race to call holder.Set. Wrapping with this type
// means the MCP server runs Spoon at most once per concurrency window.
type dedupReindexer struct {
	inner Reindexer

	mu     sync.Mutex
	flight *reindexFlight
}

// reindexFlight is the in-flight-or-recently-completed slot shared
// between the leader and its followers. done closes once the leader
// writes store / err.
type reindexFlight struct {
	done  chan struct{}
	store contract.Store
	err   error
}

// newDedupReindexer returns a wrapper. If inner is nil, the wrapper
// behaves like a no-op reindexer (Reindex returns nil, nil) — callers
// that need to reject "no reindexer configured" should check for nil
// upstream rather than relying on this wrapper.
func newDedupReindexer(inner Reindexer) *dedupReindexer {
	return &dedupReindexer{inner: inner}
}

// Reindex deduplicates concurrent calls. The first caller runs the
// inner Reindex; others wait for it and receive the same store/err
// pair. A caller whose ctx is cancelled while waiting returns
// ctx.Err() without affecting the in-flight run.
func (d *dedupReindexer) Reindex(ctx context.Context) (contract.Store, error) {
	if d == nil || d.inner == nil {
		return nil, nil
	}
	d.mu.Lock()
	flight := d.flight
	leader := flight == nil
	if leader {
		flight = &reindexFlight{done: make(chan struct{})}
		d.flight = flight
	}
	d.mu.Unlock()

	if leader {
		store, err := d.inner.Reindex(ctx)
		flight.store = store
		flight.err = err
		d.mu.Lock()
		d.flight = nil
		d.mu.Unlock()
		close(flight.done)
		return store, err
	}

	select {
	case <-flight.done:
		return flight.store, flight.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}
