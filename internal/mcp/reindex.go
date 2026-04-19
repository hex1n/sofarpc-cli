package mcp

import (
	"context"

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
// Errors should be stable enough to surface via errcode.IndexStale with
// a hint back to sofarpc_doctor — the MCP handler wraps them.
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
