package mcp

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hex1n/sofarpc-cli/internal/core/contract"
)

func TestDedupReindexer_ConcurrentCallsShareOneFlight(t *testing.T) {
	var calls atomic.Int32
	release := make(chan struct{})
	inner := ReindexerFunc(func(ctx context.Context) (contract.Store, error) {
		calls.Add(1)
		// Block until the test releases; every concurrent caller must
		// pile up behind the leader in this window.
		<-release
		return contract.NewInMemoryStore(), nil
	})
	d := newDedupReindexer(inner)

	const n = 8
	var wg sync.WaitGroup
	stores := make([]contract.Store, n)
	errs := make([]error, n)
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			stores[i], errs[i] = d.Reindex(context.Background())
		}(i)
	}
	// Give callers time to queue on the shared flight before the leader
	// completes the inner call.
	time.Sleep(50 * time.Millisecond)
	close(release)
	wg.Wait()

	if got := calls.Load(); got != 1 {
		t.Fatalf("inner should run exactly once, got %d", got)
	}
	first := stores[0]
	for i, s := range stores {
		if errs[i] != nil {
			t.Fatalf("follower %d err: %v", i, errs[i])
		}
		if s != first {
			t.Fatalf("follower %d saw a different store than the leader", i)
		}
	}
}

func TestDedupReindexer_SequentialCallsRestart(t *testing.T) {
	var calls atomic.Int32
	inner := ReindexerFunc(func(ctx context.Context) (contract.Store, error) {
		calls.Add(1)
		return contract.NewInMemoryStore(), nil
	})
	d := newDedupReindexer(inner)

	for i := 0; i < 3; i++ {
		if _, err := d.Reindex(context.Background()); err != nil {
			t.Fatalf("call %d: %v", i, err)
		}
	}
	if got := calls.Load(); got != 3 {
		t.Fatalf("inner should run once per sequential call, got %d", got)
	}
}

func TestDedupReindexer_ErrorShared_NotCached(t *testing.T) {
	var calls atomic.Int32
	release := make(chan struct{})
	sentinel := errors.New("indexer blew up")
	inner := ReindexerFunc(func(ctx context.Context) (contract.Store, error) {
		calls.Add(1)
		<-release
		return nil, sentinel
	})
	d := newDedupReindexer(inner)

	// First burst: N concurrent callers all see the same error.
	const n = 4
	var wg sync.WaitGroup
	errs := make([]error, n)
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			_, errs[i] = d.Reindex(context.Background())
		}(i)
	}
	time.Sleep(25 * time.Millisecond)
	close(release)
	wg.Wait()

	for i, err := range errs {
		if !errors.Is(err, sentinel) {
			t.Fatalf("call %d: expected sentinel error, got %v", i, err)
		}
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("first burst should share one inner run, got %d", got)
	}

	// Second call (sequential) must re-run inner — error isn't cached.
	release2 := make(chan struct{})
	inner2 := ReindexerFunc(func(ctx context.Context) (contract.Store, error) {
		calls.Add(1)
		<-release2
		return contract.NewInMemoryStore(), nil
	})
	d.inner = inner2 // swap for a success path
	var s contract.Store
	done := make(chan error, 1)
	go func() {
		var err error
		s, err = d.Reindex(context.Background())
		done <- err
	}()
	close(release2)
	if err := <-done; err != nil {
		t.Fatalf("post-failure call should retry fresh, got %v", err)
	}
	if s == nil {
		t.Fatal("post-failure call should see the swapped inner's store")
	}
	if got := calls.Load(); got != 2 {
		t.Fatalf("expected 2 total inner calls, got %d", got)
	}
}

func TestDedupReindexer_FollowerRespectsContextCancel(t *testing.T) {
	release := make(chan struct{})
	inner := ReindexerFunc(func(ctx context.Context) (contract.Store, error) {
		<-release
		return contract.NewInMemoryStore(), nil
	})
	d := newDedupReindexer(inner)

	// Kick off the leader in a long-running ctx.
	leaderErr := make(chan error, 1)
	go func() {
		_, err := d.Reindex(context.Background())
		leaderErr <- err
	}()
	// Give the leader time to take the flight.
	time.Sleep(25 * time.Millisecond)

	// Follower with a soon-to-expire ctx must return before the leader.
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	_, err := d.Reindex(ctx)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("follower should surface ctx err, got %v", err)
	}

	// Release the leader so the test goroutine finishes cleanly.
	close(release)
	if err := <-leaderErr; err != nil {
		t.Fatalf("leader should still complete after follower gives up: %v", err)
	}
}

func TestDedupReindexer_NilInnerIsNoop(t *testing.T) {
	d := newDedupReindexer(nil)
	store, err := d.Reindex(context.Background())
	if err != nil || store != nil {
		t.Fatalf("nil inner should yield (nil, nil), got (%v, %v)", store, err)
	}
}
