package mcp

import (
	"fmt"
	"testing"
	"time"

	"github.com/hex1n/sofarpc-cli/internal/core/invoke"
)

// fakeClock returns a clock hook whose time advances via the returned
// setter, so TTL and LRU logic can be exercised without real sleeps.
func fakeClock(start time.Time) (func() time.Time, func(time.Duration)) {
	now := start
	return func() time.Time { return now }, func(d time.Duration) { now = now.Add(d) }
}

// seqIDs returns deterministic "s0", "s1", … ids so test assertions can
// refer to sessions by creation order without reading randoms.
func seqIDs() func() string {
	var n int
	return func() string {
		id := fmt.Sprintf("s%d", n)
		n++
		return id
	}
}

func TestSessionStore_Create_PopulatesMetadata(t *testing.T) {
	clock, _ := fakeClock(time.Date(2026, 4, 1, 12, 0, 0, 0, time.UTC))
	store := NewSessionStore().WithClock(clock).WithIDFunc(seqIDs())

	got := store.Create(Session{ProjectRoot: "/tmp/proj"})
	if got.ID != "s0" {
		t.Fatalf("id: got %q want s0", got.ID)
	}
	if got.ProjectRoot != "/tmp/proj" {
		t.Fatalf("projectRoot: got %q", got.ProjectRoot)
	}
	if !got.CreatedAt.Equal(clock()) {
		t.Fatalf("createdAt: got %v want %v", got.CreatedAt, clock())
	}
	if store.Size() != 1 {
		t.Fatalf("Size: got %d want 1", store.Size())
	}
}

func TestSessionStore_TTL_ExpiresIdleSessionsOnNextCreate(t *testing.T) {
	clock, advance := fakeClock(time.Date(2026, 4, 1, 12, 0, 0, 0, time.UTC))
	store := NewSessionStoreWithLimits(time.Hour, 0).WithClock(clock).WithIDFunc(seqIDs())

	old := store.Create(Session{ProjectRoot: "/a"})
	advance(2 * time.Hour) // idle past TTL
	fresh := store.Create(Session{ProjectRoot: "/b"})

	if _, ok := store.Get(old.ID); ok {
		t.Fatal("old session should have been swept on Create")
	}
	if _, ok := store.Get(fresh.ID); !ok {
		t.Fatal("fresh session should survive its own Create")
	}
	if store.Size() != 1 {
		t.Fatalf("Size: got %d want 1", store.Size())
	}
}

func TestSessionStore_Get_BumpsLRUKeepsSessionAlive(t *testing.T) {
	clock, advance := fakeClock(time.Date(2026, 4, 1, 12, 0, 0, 0, time.UTC))
	store := NewSessionStoreWithLimits(time.Hour, 0).WithClock(clock).WithIDFunc(seqIDs())

	stale := store.Create(Session{ProjectRoot: "/a"})
	advance(45 * time.Minute)
	// Activity before the TTL bites — should reset the idle timer.
	if _, ok := store.Get(stale.ID); !ok {
		t.Fatal("Get should see the session before expiry")
	}
	advance(45 * time.Minute) // total 90m since Create, only 45m since Get
	_ = store.Create(Session{ProjectRoot: "/b"})

	if _, ok := store.Get(stale.ID); !ok {
		t.Fatal("Get should have bumped lastUsed and kept the session alive")
	}
}

func TestSessionStore_UpdatePlan_BumpsLRU(t *testing.T) {
	clock, advance := fakeClock(time.Date(2026, 4, 1, 12, 0, 0, 0, time.UTC))
	store := NewSessionStoreWithLimits(time.Hour, 0).WithClock(clock).WithIDFunc(seqIDs())

	s := store.Create(Session{ProjectRoot: "/a"})
	advance(45 * time.Minute)
	if !store.UpdatePlan(s.ID, invoke.Plan{Service: "x", Method: "y"}) {
		t.Fatal("UpdatePlan should succeed on known session")
	}
	advance(45 * time.Minute) // 90m since Create, 45m since UpdatePlan
	_ = store.Create(Session{ProjectRoot: "/b"})

	if _, ok := store.Get(s.ID); !ok {
		t.Fatal("UpdatePlan should have bumped lastUsed and kept the session alive")
	}
}

func TestSessionStore_Capacity_EvictsLRUEntry(t *testing.T) {
	clock, advance := fakeClock(time.Date(2026, 4, 1, 12, 0, 0, 0, time.UTC))
	store := NewSessionStoreWithLimits(0, 2).WithClock(clock).WithIDFunc(seqIDs())

	a := store.Create(Session{ProjectRoot: "/a"})
	advance(time.Minute)
	b := store.Create(Session{ProjectRoot: "/b"})
	advance(time.Minute)
	// Touch a so b becomes the LRU entry.
	if _, ok := store.Get(a.ID); !ok {
		t.Fatal("Get on a should succeed")
	}
	advance(time.Minute)
	c := store.Create(Session{ProjectRoot: "/c"})

	if _, ok := store.Get(b.ID); ok {
		t.Fatal("b should have been evicted as the LRU entry")
	}
	if _, ok := store.Get(a.ID); !ok {
		t.Fatal("a should still be present (touched before cap pressure)")
	}
	if _, ok := store.Get(c.ID); !ok {
		t.Fatal("c should be present (just created)")
	}
	if store.Size() != 2 {
		t.Fatalf("Size: got %d want 2", store.Size())
	}
}

func TestSessionStore_Capacity_ZeroDisablesBound(t *testing.T) {
	clock, advance := fakeClock(time.Date(2026, 4, 1, 12, 0, 0, 0, time.UTC))
	store := NewSessionStoreWithLimits(0, 0).WithClock(clock).WithIDFunc(seqIDs())

	const n = 10
	for i := 0; i < n; i++ {
		store.Create(Session{ProjectRoot: "/x"})
		advance(time.Second)
	}
	if store.Size() != n {
		t.Fatalf("Size: got %d want %d (cap=0 must not evict)", store.Size(), n)
	}
}

func TestSessionStore_UpdatePlan_UnknownIDIsNoop(t *testing.T) {
	store := NewSessionStore()
	if store.UpdatePlan("does-not-exist", invoke.Plan{Service: "x"}) {
		t.Fatal("UpdatePlan should return false for unknown ids")
	}
	if store.UpdatePlan("", invoke.Plan{Service: "x"}) {
		t.Fatal("UpdatePlan should return false for empty id")
	}
}
