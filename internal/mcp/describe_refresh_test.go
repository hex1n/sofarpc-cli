package mcp

import (
	"context"
	"errors"
	"testing"

	"github.com/hex1n/sofarpc-cli/internal/core/contract"
	"github.com/hex1n/sofarpc-cli/internal/errcode"
	"github.com/hex1n/sofarpc-cli/internal/facadesemantic"
)

func TestDescribe_RefreshPopulatesEmptyFacade(t *testing.T) {
	// Start with no facade; refresh=true must swap in the reindexer's
	// store and then serve the describe call against it.
	fresh := contract.NewInMemoryStore(
		facadesemantic.Class{
			FQN: "com.foo.Svc", Kind: facadesemantic.KindInterface,
			Methods: []facadesemantic.Method{
				{Name: "doThing", ParamTypes: []string{"java.lang.String"}, ReturnType: "java.lang.String"},
			},
		},
	)
	var calls int
	reindexer := ReindexerFunc(func(ctx context.Context) (contract.Store, error) {
		calls++
		return fresh, nil
	})

	out := callDescribe(t, Options{Reindexer: reindexer}, map[string]any{
		"service": "com.foo.Svc",
		"method":  "doThing",
		"refresh": true,
	})
	if out.Error != nil {
		t.Fatalf("unexpected error: %+v", out.Error)
	}
	if calls != 1 {
		t.Fatalf("expected exactly one reindex call, got %d", calls)
	}
	if !out.Diagnostics.Refreshed {
		t.Fatal("diagnostics.refreshed should be true on refresh=true path")
	}
	if len(out.Overloads) != 1 {
		t.Fatalf("overloads: got %d want 1", len(out.Overloads))
	}
}

func TestDescribe_RefreshFailureSurfacesIndexStale(t *testing.T) {
	reindexer := ReindexerFunc(func(ctx context.Context) (contract.Store, error) {
		return nil, errors.New("spoon jar missing")
	})
	out := callDescribe(t, Options{Reindexer: reindexer}, map[string]any{
		"service": "com.foo.Svc",
		"method":  "doThing",
		"refresh": true,
	})
	if out.Error == nil {
		t.Fatal("error should be populated when reindex fails")
	}
	if out.Error.Code != errcode.IndexStale {
		t.Fatalf("code: got %q want %q", out.Error.Code, errcode.IndexStale)
	}
	if out.Error.Hint == nil || out.Error.Hint.NextTool != "sofarpc_doctor" {
		t.Fatalf("hint should route to sofarpc_doctor on reindex failure, got %+v", out.Error.Hint)
	}
}

func TestDescribe_RefreshWithoutReindexerIsFacadeNotConfigured(t *testing.T) {
	// refresh=true with no reindexer wired is a configuration gap, not a
	// stale index — the agent can't self-heal by retrying. We assert the
	// taxonomy routes to doctor instead.
	out := callDescribe(t, Options{}, map[string]any{
		"service": "com.foo.Svc",
		"method":  "doThing",
		"refresh": true,
	})
	if out.Error == nil {
		t.Fatal("error should be populated when refresh is requested without a reindexer")
	}
	if out.Error.Code != errcode.FacadeNotConfigured {
		t.Fatalf("code: got %q want %q", out.Error.Code, errcode.FacadeNotConfigured)
	}
	if out.Error.Hint == nil || out.Error.Hint.NextTool != "sofarpc_doctor" {
		t.Fatalf("hint should route to sofarpc_doctor, got %+v", out.Error.Hint)
	}
}

func TestDescribe_FacadeNotConfiguredHintsSelfHealWhenReindexerPresent(t *testing.T) {
	// No initial facade, reindexer wired, no refresh on this call — we
	// expect the error to hint the agent back at describe with refresh=true.
	reindexer := ReindexerFunc(func(ctx context.Context) (contract.Store, error) {
		t.Fatal("reindexer should not run when refresh is false")
		return nil, nil
	})
	out := callDescribe(t, Options{Reindexer: reindexer}, map[string]any{
		"service": "com.foo.Svc",
		"method":  "doThing",
	})
	if out.Error == nil || out.Error.Code != errcode.FacadeNotConfigured {
		t.Fatalf("expected FacadeNotConfigured, got %+v", out.Error)
	}
	if out.Error.Hint == nil || out.Error.Hint.NextTool != "sofarpc_describe" {
		t.Fatalf("hint should point back at describe when reindexer is available: %+v", out.Error.Hint)
	}
	if refresh, _ := out.Error.Hint.NextArgs["refresh"].(bool); !refresh {
		t.Fatalf("self-heal hint should carry refresh=true, got %+v", out.Error.Hint.NextArgs)
	}
}

func TestDescribe_RefreshSwapIsVisibleToNextCall(t *testing.T) {
	// Same server, two describe calls: the first with refresh=true
	// populates the shared holder; the second (no refresh) must see the
	// store without asking the reindexer to run again.
	fresh := contract.NewInMemoryStore(
		facadesemantic.Class{
			FQN: "com.foo.Svc", Kind: facadesemantic.KindInterface,
			Methods: []facadesemantic.Method{
				{Name: "doThing", ParamTypes: []string{"java.lang.String"}, ReturnType: "java.lang.String"},
			},
		},
	)
	var calls int
	reindexer := ReindexerFunc(func(ctx context.Context) (contract.Store, error) {
		calls++
		return fresh, nil
	})

	server := New(Options{Reindexer: reindexer})
	ctx := context.Background()
	client := connect(t, ctx, server)
	defer client.Close()

	first := invokeDescribeTool(t, client, map[string]any{
		"service": "com.foo.Svc",
		"method":  "doThing",
		"refresh": true,
	})
	if first.Error != nil {
		t.Fatalf("first call should succeed, got %+v", first.Error)
	}
	second := invokeDescribeTool(t, client, map[string]any{
		"service": "com.foo.Svc",
		"method":  "doThing",
	})
	if second.Error != nil {
		t.Fatalf("second call should see the swapped facade, got %+v", second.Error)
	}
	if second.Diagnostics.Refreshed {
		t.Fatal("second call did not pass refresh=true; diagnostics.refreshed must be false")
	}
	if calls != 1 {
		t.Fatalf("reindexer should run exactly once across two calls, got %d", calls)
	}
}
