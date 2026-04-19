package mcp

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/hex1n/sofarpc-cli/internal/core/contract"
	"github.com/hex1n/sofarpc-cli/internal/facadesemantic"
)

// fakeDescribeClient records every Describe call so tests can assert
// on cache behavior. Known classes return ok=true with their supplied
// shape; anything else is treated as a miss (mirroring how the real
// worker reports contract.unresolvable for classes absent from the
// facade classpath).
type fakeDescribeClient struct {
	known map[string]facadesemantic.Class
	calls atomic.Int32
}

func (f *fakeDescribeClient) Describe(_ context.Context, service string) (facadesemantic.Class, bool, error) {
	f.calls.Add(1)
	cls, ok := f.known[service]
	return cls, ok, nil
}

func TestWorkerStore_ClassCachesHit(t *testing.T) {
	svc := facadesemantic.Class{
		FQN:  "com.foo.Svc",
		Kind: facadesemantic.KindInterface,
		Methods: []facadesemantic.Method{
			{Name: "doThing", ParamTypes: []string{"java.lang.String"}},
		},
	}
	fake := &fakeDescribeClient{known: map[string]facadesemantic.Class{svc.FQN: svc}}
	store := newWorkerStoreWithClient(context.Background(), fake)

	cls, ok := store.Class(svc.FQN)
	if !ok {
		t.Fatal("first lookup should hit")
	}
	if cls.FQN != svc.FQN {
		t.Fatalf("fqn: got %q", cls.FQN)
	}
	if got := fake.calls.Load(); got != 1 {
		t.Fatalf("RPC count after first lookup: got %d want 1", got)
	}

	if _, ok := store.Class(svc.FQN); !ok {
		t.Fatal("second lookup should hit the cache")
	}
	if got := fake.calls.Load(); got != 1 {
		t.Fatalf("RPC count after cached lookup: got %d want 1", got)
	}
}

func TestWorkerStore_ClassCachesMiss(t *testing.T) {
	fake := &fakeDescribeClient{known: map[string]facadesemantic.Class{}}
	store := newWorkerStoreWithClient(context.Background(), fake)

	if _, ok := store.Class("com.unknown.X"); ok {
		t.Fatal("unknown should miss")
	}
	if got := fake.calls.Load(); got != 1 {
		t.Fatalf("RPC count: got %d want 1", got)
	}
	if _, ok := store.Class("com.unknown.X"); ok {
		t.Fatal("unknown miss should stick across lookups")
	}
	if got := fake.calls.Load(); got != 1 {
		t.Fatalf("negative cache: RPC fired again, got %d want 1", got)
	}
}

func TestWorkerStore_NilIsHarmless(t *testing.T) {
	var s *workerStore
	if _, ok := s.Class("anything"); ok {
		t.Fatal("nil store should miss silently")
	}
}

func TestWorkerStore_NewWorkerStoreReturnsNilForNilClient(t *testing.T) {
	if s := NewWorkerStore(context.Background(), nil); s != nil {
		t.Fatalf("NewWorkerStore(nil) should be nil, got %T", s)
	}
}

// Compile-time guard that the tests exercise the concrete store via
// the interface, so the adapter really does satisfy contract.Store.
var _ contract.Store = (*workerStore)(nil)
