package worker

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// fakeProcess constructs a Process whose Conn is wired to an in-memory
// pipe served by a local goroutine. We skip exec.Cmd entirely —
// process_test.go covers the real subprocess path.
func fakeProcess(t *testing.T, profile Profile) (*Process, func()) {
	t.Helper()
	serverSide, clientSide := net.Pipe()
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer serverSide.Close()
		reader := bufio.NewReader(serverSide)
		writer := bufio.NewWriter(serverSide)
		for {
			line, err := reader.ReadBytes('\n')
			if len(line) > 0 {
				var req Request
				if json.Unmarshal(line, &req) == nil {
					resp, _ := json.Marshal(Response{RequestID: req.RequestID, Ok: true})
					writer.Write(resp)
					writer.WriteByte('\n')
					writer.Flush()
				}
			}
			if err != nil {
				return
			}
		}
	}()

	// exited starts open so Pool.Get's liveness check sees the fake as
	// alive. StopGrace is tiny so the bounded waits in Stop return fast
	// for tests that rely on pool.Close to tear down workers.
	exited := make(chan struct{})
	proc := &Process{
		spec:   Spec{Profile: profile, Jar: "fake", StopGrace: 10 * time.Millisecond},
		cmd:    nil, // tested path in Stop tolerates nil cmd
		conn:   NewConn(clientSide),
		ready:  ReadyMessage{Ready: true, Port: 1, PID: 2},
		exited: exited,
	}
	return proc, func() {
		// Close exited before Stop so Stop's grace-period waits return
		// immediately. Guarded so double-close from tests that already
		// simulated a crash is a no-op.
		select {
		case <-exited:
		default:
			close(exited)
		}
		proc.Stop(context.Background())
		<-done
	}
}

func newFakeSpawner(t *testing.T) (*atomic.Int32, spawner) {
	var count atomic.Int32
	return &count, func(ctx context.Context, spec Spec) (*Process, error) {
		count.Add(1)
		proc, _ := fakeProcess(t, spec.Profile)
		return proc, nil
	}
}

func fullProfile(tag string) Profile {
	return Profile{SOFARPCVersion: "5.12.0", RuntimeJarDigest: tag, JavaMajor: 17}
}

func TestPool_GetReusesSameWorker(t *testing.T) {
	count, sp := newFakeSpawner(t)
	pool := NewPool(Spec{Jar: "fake.jar"}).withSpawner(sp)
	defer pool.Close(context.Background())

	ctx := context.Background()
	a, err := pool.Get(ctx, fullProfile("x"))
	if err != nil {
		t.Fatalf("get 1: %v", err)
	}
	b, err := pool.Get(ctx, fullProfile("x"))
	if err != nil {
		t.Fatalf("get 2: %v", err)
	}
	if a != b {
		t.Fatal("same profile must return the same Process")
	}
	if got := count.Load(); got != 1 {
		t.Fatalf("spawn count: got %d want 1", got)
	}
}

func TestPool_ConcurrentGetsShareOneSpawn(t *testing.T) {
	count, sp := newFakeSpawner(t)
	// Slow the spawner so the race window is obvious.
	slow := func(ctx context.Context, spec Spec) (*Process, error) {
		time.Sleep(20 * time.Millisecond)
		return sp(ctx, spec)
	}
	pool := NewPool(Spec{Jar: "fake.jar"}).withSpawner(slow)
	defer pool.Close(context.Background())

	const n = 16
	results := make([]*Process, n)
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			p, err := pool.Get(context.Background(), fullProfile("x"))
			if err != nil {
				t.Errorf("get %d: %v", i, err)
				return
			}
			results[i] = p
		}(i)
	}
	wg.Wait()

	first := results[0]
	for i, p := range results {
		if p != first {
			t.Fatalf("result %d differed — pool double-spawned", i)
		}
	}
	if got := count.Load(); got != 1 {
		t.Fatalf("spawn count: got %d want 1 (concurrent Gets must dedupe)", got)
	}
}

func TestPool_DifferentProfilesSpawnIndependently(t *testing.T) {
	count, sp := newFakeSpawner(t)
	pool := NewPool(Spec{Jar: "fake.jar"}).withSpawner(sp)
	defer pool.Close(context.Background())

	a, err := pool.Get(context.Background(), fullProfile("a"))
	if err != nil {
		t.Fatalf("get a: %v", err)
	}
	b, err := pool.Get(context.Background(), fullProfile("b"))
	if err != nil {
		t.Fatalf("get b: %v", err)
	}
	if a == b {
		t.Fatal("different profiles should give different workers")
	}
	if got := count.Load(); got != 2 {
		t.Fatalf("spawn count: got %d want 2", got)
	}
	if pool.Size() != 2 {
		t.Fatalf("pool Size: got %d want 2", pool.Size())
	}
}

func TestPool_GetRejectsEmptyProfile(t *testing.T) {
	pool := NewPool(Spec{Jar: "fake.jar"})
	_, err := pool.Get(context.Background(), Profile{})
	if err == nil {
		t.Fatal("empty profile should fail")
	}
}

func TestPool_GetFailsWhenBaseSpecHasNoJar(t *testing.T) {
	pool := NewPool(Spec{}).withSpawner(func(context.Context, Spec) (*Process, error) {
		t.Fatal("spawner should not run when base spec is invalid")
		return nil, nil
	})
	_, err := pool.Get(context.Background(), fullProfile("x"))
	if err == nil {
		t.Fatal("missing Jar should fail Get")
	}
}

func TestPool_SpawnFailureIsNotCached(t *testing.T) {
	var attempts atomic.Int32
	sp := func(context.Context, Spec) (*Process, error) {
		n := attempts.Add(1)
		if n == 1 {
			return nil, errors.New("first attempt fails")
		}
		proc, _ := fakeProcess(t, Profile{})
		return proc, nil
	}
	pool := NewPool(Spec{Jar: "fake.jar"}).withSpawner(sp)
	defer pool.Close(context.Background())

	if _, err := pool.Get(context.Background(), fullProfile("x")); err == nil {
		t.Fatal("first Get should fail")
	}
	if _, err := pool.Get(context.Background(), fullProfile("x")); err != nil {
		t.Fatalf("second Get should succeed after failure cleared: %v", err)
	}
	if got := attempts.Load(); got != 2 {
		t.Fatalf("expected 2 spawn attempts, got %d", got)
	}
}

func TestPool_GetRespectsContext(t *testing.T) {
	block := make(chan struct{})
	sp := func(ctx context.Context, spec Spec) (*Process, error) {
		<-block
		return nil, errors.New("never")
	}
	pool := NewPool(Spec{Jar: "fake.jar"}).withSpawner(sp)
	defer func() { close(block); pool.Close(context.Background()) }()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	_, err := pool.Get(ctx, fullProfile("x"))
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected DeadlineExceeded, got %v", err)
	}
}

func TestPool_GetReplacesExitedWorker(t *testing.T) {
	count, sp := newFakeSpawner(t)
	pool := NewPool(Spec{Jar: "fake.jar"}).withSpawner(sp)
	defer pool.Close(context.Background())

	a, err := pool.Get(context.Background(), fullProfile("x"))
	if err != nil {
		t.Fatalf("get 1: %v", err)
	}
	// Simulate the worker crashing: close the exit channel so the
	// pool's next liveness check evicts the cached slot.
	close(a.exited)

	b, err := pool.Get(context.Background(), fullProfile("x"))
	if err != nil {
		t.Fatalf("get 2: %v", err)
	}
	if a == b {
		t.Fatal("Get should respawn after the cached worker exits")
	}
	if got := count.Load(); got != 2 {
		t.Fatalf("spawn count: got %d want 2", got)
	}
}

func TestPool_EvictRemovesSlot(t *testing.T) {
	count, sp := newFakeSpawner(t)
	pool := NewPool(Spec{Jar: "fake.jar"}).withSpawner(sp)
	defer pool.Close(context.Background())

	if _, err := pool.Get(context.Background(), fullProfile("x")); err != nil {
		t.Fatalf("get: %v", err)
	}
	if pool.Size() != 1 {
		t.Fatalf("Size after Get: got %d want 1", pool.Size())
	}
	pool.Evict(fullProfile("x"))
	if pool.Size() != 0 {
		t.Fatalf("Size after Evict: got %d want 0", pool.Size())
	}
	if _, err := pool.Get(context.Background(), fullProfile("x")); err != nil {
		t.Fatalf("get after evict: %v", err)
	}
	if got := count.Load(); got != 2 {
		t.Fatalf("spawn count: got %d want 2 (original + post-evict respawn)", got)
	}
}

func TestPool_EvictUnknownProfileIsNoop(t *testing.T) {
	_, sp := newFakeSpawner(t)
	pool := NewPool(Spec{Jar: "fake.jar"}).withSpawner(sp)
	defer pool.Close(context.Background())

	// Never spawned — Evict must be a silent no-op.
	pool.Evict(fullProfile("nobody"))
	if pool.Size() != 0 {
		t.Fatalf("Size: got %d want 0", pool.Size())
	}
}

func TestPool_CloseStopsAllWorkers(t *testing.T) {
	_, sp := newFakeSpawner(t)
	pool := NewPool(Spec{Jar: "fake.jar"}).withSpawner(sp)

	for _, tag := range []string{"a", "b", "c"} {
		if _, err := pool.Get(context.Background(), fullProfile(tag)); err != nil {
			t.Fatalf("get %s: %v", tag, err)
		}
	}
	if pool.Size() != 3 {
		t.Fatalf("Size before close: got %d want 3", pool.Size())
	}
	if err := pool.Close(context.Background()); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if pool.Size() != 0 {
		t.Fatalf("Size after close: got %d want 0", pool.Size())
	}
}

func TestPool_CapReportsConfiguredLimit(t *testing.T) {
	cases := []struct {
		name string
		cap  int
	}{
		{"default cap", defaultPoolCap},
		{"explicit cap", 3},
		{"unbounded", 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var pool *Pool
			if tc.name == "default cap" {
				pool = NewPool(Spec{Jar: "fake.jar"})
			} else {
				pool = NewPoolWithLimits(Spec{Jar: "fake.jar"}, 0, tc.cap)
			}
			if got := pool.Cap(); got != tc.cap {
				t.Fatalf("Cap: got %d want %d", got, tc.cap)
			}
		})
	}
}

func TestPool_TTLEvictsIdleWorkers(t *testing.T) {
	count, sp := newFakeSpawner(t)
	ttl := 10 * time.Minute
	pool := NewPoolWithLimits(Spec{Jar: "fake.jar"}, ttl, 0).withSpawner(sp)
	defer pool.Close(context.Background())

	// Drive the clock by hand — a real wall clock would make this test
	// flaky or slow.
	now := time.Unix(0, 0)
	pool.withClock(func() time.Time { return now })

	if _, err := pool.Get(context.Background(), fullProfile("x")); err != nil {
		t.Fatalf("get 1: %v", err)
	}
	if pool.Size() != 1 {
		t.Fatalf("Size after first Get: got %d want 1", pool.Size())
	}

	// Jump past TTL, then Get a different profile — the sweep runs on
	// the new-slot path and should reclaim the idle "x" slot.
	now = now.Add(ttl + time.Second)
	if _, err := pool.Get(context.Background(), fullProfile("y")); err != nil {
		t.Fatalf("get 2: %v", err)
	}

	// "x" expired and was evicted; only "y" should remain.
	if pool.Size() != 1 {
		t.Fatalf("Size after TTL sweep: got %d want 1", pool.Size())
	}
	// Get(x) again → fresh spawn, so total spawn count is 3.
	if _, err := pool.Get(context.Background(), fullProfile("x")); err != nil {
		t.Fatalf("get 3: %v", err)
	}
	if got := count.Load(); got != 3 {
		t.Fatalf("spawn count: got %d want 3 (x, y, x-respawned)", got)
	}
}

func TestPool_TTLZeroDisablesSweep(t *testing.T) {
	_, sp := newFakeSpawner(t)
	// ttl=0 means "idle sweep disabled"; cap=0 means "cap disabled".
	pool := NewPoolWithLimits(Spec{Jar: "fake.jar"}, 0, 0).withSpawner(sp)
	defer pool.Close(context.Background())

	now := time.Unix(0, 0)
	pool.withClock(func() time.Time { return now })

	if _, err := pool.Get(context.Background(), fullProfile("x")); err != nil {
		t.Fatalf("get 1: %v", err)
	}
	// Advance way past any reasonable TTL.
	now = now.Add(48 * time.Hour)
	if _, err := pool.Get(context.Background(), fullProfile("y")); err != nil {
		t.Fatalf("get 2: %v", err)
	}
	if pool.Size() != 2 {
		t.Fatalf("ttl=0 should not evict; got Size %d want 2", pool.Size())
	}
}

func TestPool_CapEvictsLRU(t *testing.T) {
	count, sp := newFakeSpawner(t)
	// cap=2 so the third Get must evict the LRU.
	pool := NewPoolWithLimits(Spec{Jar: "fake.jar"}, 0, 2).withSpawner(sp)
	defer pool.Close(context.Background())

	now := time.Unix(0, 0)
	tick := func() time.Time { now = now.Add(time.Second); return now }
	pool.withClock(tick)

	for _, tag := range []string{"a", "b"} {
		if _, err := pool.Get(context.Background(), fullProfile(tag)); err != nil {
			t.Fatalf("get %s: %v", tag, err)
		}
	}
	// Touch "b" so "a" becomes the LRU victim.
	if _, err := pool.Get(context.Background(), fullProfile("b")); err != nil {
		t.Fatalf("touch b: %v", err)
	}
	if _, err := pool.Get(context.Background(), fullProfile("c")); err != nil {
		t.Fatalf("get c: %v", err)
	}

	if pool.Size() != 2 {
		t.Fatalf("cap=2 Size after third profile: got %d want 2", pool.Size())
	}
	// "a" was the LRU, so Get(a) respawns → 4 total spawns (a, b, c, a').
	if _, err := pool.Get(context.Background(), fullProfile("a")); err != nil {
		t.Fatalf("get a again: %v", err)
	}
	if got := count.Load(); got != 4 {
		t.Fatalf("spawn count: got %d want 4", got)
	}
}

// sanity import guard — net.Pipe is the kernel of fakeProcess
var _ = io.EOF
var _ = (net.Conn)(nil)
