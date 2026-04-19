package worker

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sync"
	"time"
)

// spawner is what the pool calls to materialise a Process from a Spec.
// Defaults to package-level Spawn; tests swap it for a fake that does
// not require a real subprocess.
type spawner func(context.Context, Spec) (*Process, error)

// Pool keeps at most one worker Process per Profile. Workers spawn
// lazily on the first Get; concurrent Gets on the same profile share
// one spawn (no double-starts). Pool is safe for concurrent use.
//
// Self-heal: Get non-blockingly checks the cached Process's Exited
// channel before returning it. A dead worker is evicted and respawned
// in the same call (bounded to one respawn per Get). Evict lets
// Client.Invoke reactively recover when Send observes ErrConnClosed.
//
// Bounded resource use: two dimensions run on-Get — no background
// goroutine, so there is no Stop() lifecycle beyond Close.
//
//   - Idle TTL: slots whose lastUsed is older than ttl are evicted and
//     their Process is Stopped asynchronously. A Get on a key whose
//     slot just expired spawns a fresh worker.
//   - Capacity: when len(slots) would exceed cap, the LRU slot is
//     evicted to make room. In-flight spawns are never evicted — their
//     outcome hasn't been observed yet.
//
// Zero for either dimension disables it (matches SessionStore).
type Pool struct {
	base  Spec
	spawn spawner
	clock func() time.Time
	ttl   time.Duration
	cap   int

	mu    sync.Mutex
	slots map[string]*slot
}

// slot tracks one in-flight or completed spawn keyed by Profile.Key().
// done is closed exactly once after proc/err are set; lastUsed is the
// GC anchor, bumped by Get on every successful return.
type slot struct {
	done     chan struct{}
	proc     *Process
	err      error
	lastUsed time.Time
}

// defaultPoolTTL drops workers idle for more than 30 min. JVMs are
// expensive to restart and class metadata caches take time to warm, so
// the ceiling is generous — we only want to reclaim memory when a
// long-running server genuinely stops using a profile.
const defaultPoolTTL = 30 * time.Minute

// defaultPoolCap caps concurrent workers. In typical single-project use
// the pool carries one worker (one profile); 8 leaves headroom for
// servers that drive multiple projects or runtime jars before LRU bites.
const defaultPoolCap = 8

// NewPool returns a pool with default TTL (30m) and capacity (8).
func NewPool(base Spec) *Pool {
	return NewPoolWithLimits(base, defaultPoolTTL, defaultPoolCap)
}

// NewPoolWithLimits returns a pool with explicit idle TTL and capacity.
// Zero for either dimension disables it. Use NewPool in production —
// this exists for tests and for callers that need to tune the bounds.
func NewPoolWithLimits(base Spec, ttl time.Duration, capacity int) *Pool {
	return &Pool{
		base:  base,
		spawn: Spawn,
		clock: time.Now,
		ttl:   ttl,
		cap:   capacity,
		slots: map[string]*slot{},
	}
}

// withSpawner swaps the spawn function. Test-only helper — production
// code never touches it.
func (p *Pool) withSpawner(fn spawner) *Pool {
	p.spawn = fn
	return p
}

// withClock swaps the clock for tests that need deterministic TTL /
// LRU behaviour. Production callers leave the default (time.Now).
func (p *Pool) withClock(fn func() time.Time) *Pool {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.clock = fn
	return p
}

// SetSpawnerForTesting lets callers in other packages (notably
// internal/mcp tests) inject a fake spawn function without exposing the
// unexported spawner type. Do not call from production code.
func (p *Pool) SetSpawnerForTesting(fn func(context.Context, Spec) (*Process, error)) {
	p.spawn = spawner(fn)
}

// NewFakeProcessForTesting wraps a pre-connected net.Conn in a Process
// suitable for the pool's fake spawner in cross-package tests. It
// bypasses exec entirely — Stop is a no-op on the underlying cmd.
//
// exited is left open so the pool's liveness check treats the fake as
// alive. StopGrace is tiny so Stop's bounded waits return quickly when
// Pool.Close tears the process down without ever closing exited.
func NewFakeProcessForTesting(spec Spec, conn net.Conn) *Process {
	if spec.StopGrace == 0 {
		spec.StopGrace = 10 * time.Millisecond
	}
	return &Process{
		spec:   spec,
		conn:   NewConn(conn),
		exited: make(chan struct{}),
	}
}

// Get returns the Process for profile, spawning one if none exists yet
// or if the cached one has already exited. Concurrent calls for the
// same profile share one spawn; different profiles run in parallel.
// On spawn failure the slot is removed so the next Get retries.
//
// Self-heal: the liveness check is non-blocking, so healthy workers
// pay nothing; a dead cached worker is evicted and a fresh spawn runs
// within the same Get. We cap the respawn loop at one retry so a JVM
// that dies immediately after start (e.g. bad classpath) fails fast
// instead of spinning.
func (p *Pool) Get(ctx context.Context, profile Profile) (*Process, error) {
	if profile.Empty() {
		return nil, errors.New("worker: pool Get requires a complete Profile")
	}
	key := profile.Key()

	for attempt := 0; attempt < 2; attempt++ {
		p.mu.Lock()
		now := p.clock()
		s, existing := p.slots[key]
		if !existing {
			// Sweep + cap only when creating — hits on an existing
			// slot pay nothing beyond the lastUsed bump below.
			p.sweepExpiredLocked(now)
			p.enforceCapLocked()
			s = &slot{done: make(chan struct{}), lastUsed: now}
			p.slots[key] = s
			go p.runSpawn(profile, s)
		}
		p.mu.Unlock()

		select {
		case <-s.done:
			if s.err != nil {
				p.deleteSlotIfSame(key, s)
				return nil, s.err
			}
			select {
			case <-s.proc.Exited():
				// Cached worker has died. Evict and loop around to
				// spawn a replacement.
				p.deleteSlotIfSame(key, s)
				continue
			default:
			}
			p.mu.Lock()
			if p.slots[key] == s {
				s.lastUsed = p.clock()
			}
			p.mu.Unlock()
			return s.proc, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return nil, errors.New("worker: respawned worker exited immediately")
}

// Evict removes the slot for profile and best-effort stops the held
// process in the background. Safe to call for an unknown profile. The
// next Get will spawn a fresh worker.
//
// Client.Invoke calls this when Send returns ErrConnClosed (pre-send,
// before retrying) or ErrConnLost (mid-flight, before surfacing
// InvocationUncertain) — the agent thus sees self-healing without
// needing to know about pool internals.
func (p *Pool) Evict(profile Profile) {
	if profile.Empty() {
		return
	}
	key := profile.Key()
	p.mu.Lock()
	s, ok := p.slots[key]
	if ok {
		delete(p.slots, key)
	}
	p.mu.Unlock()
	if !ok {
		return
	}
	// Stop asynchronously — the caller (Invoke retry) shouldn't wait
	// on a dying JVM's grace period.
	go func() {
		<-s.done
		if s.proc != nil {
			_ = s.proc.Stop(context.Background())
		}
	}()
}

func (p *Pool) deleteSlotIfSame(key string, s *slot) {
	p.mu.Lock()
	if p.slots[key] == s {
		delete(p.slots, key)
	}
	p.mu.Unlock()
}

// sweepExpiredLocked drops slots whose lastUsed is older than now-ttl,
// stopping their processes asynchronously. In-flight spawns (done still
// open) are skipped — we only evict slots whose spawn has settled.
// O(n) but n is bounded by cap, and this only runs on fresh Gets.
func (p *Pool) sweepExpiredLocked(now time.Time) {
	if p.ttl <= 0 {
		return
	}
	cutoff := now.Add(-p.ttl)
	for key, s := range p.slots {
		if !slotIdle(s) {
			continue
		}
		if s.lastUsed.Before(cutoff) {
			p.stopEvictedLocked(key, s)
		}
	}
}

// enforceCapLocked evicts LRU slots until there is room for one more.
// In-flight spawns are never the eviction target — picking one would
// orphan a goroutine that's about to call s.proc.Stop on a process the
// caller is still waiting on. With cap ≫ 1 (8 default) and in-flight
// spawns rare, there is always a settled slot to sacrifice first.
func (p *Pool) enforceCapLocked() {
	if p.cap <= 0 {
		return
	}
	for len(p.slots) >= p.cap {
		var lruKey string
		var lruSlot *slot
		for key, s := range p.slots {
			if !slotIdle(s) {
				continue
			}
			if lruSlot == nil || s.lastUsed.Before(lruSlot.lastUsed) {
				lruKey = key
				lruSlot = s
			}
		}
		if lruSlot == nil {
			// All remaining slots are in-flight spawns — tolerate the
			// temporary overshoot rather than killing a live spawn.
			return
		}
		p.stopEvictedLocked(lruKey, lruSlot)
	}
}

// stopEvictedLocked removes a settled slot from the map and stops its
// process asynchronously. The Stop is fire-and-forget: Get must not pay
// a JVM's grace period just because another slot aged out.
func (p *Pool) stopEvictedLocked(key string, s *slot) {
	delete(p.slots, key)
	if s.proc != nil {
		go s.proc.Stop(context.Background())
	}
}

// slotIdle reports whether a slot's spawn has finished, so it is safe
// to evict. An in-flight spawn's done channel is still open.
func slotIdle(s *slot) bool {
	select {
	case <-s.done:
		return true
	default:
		return false
	}
}

// Close stops every worker in parallel and empties the pool. Safe to
// call on an already-closed pool (no-op). New Gets after Close will
// spawn fresh workers.
func (p *Pool) Close(ctx context.Context) error {
	p.mu.Lock()
	slots := make([]*slot, 0, len(p.slots))
	for _, s := range p.slots {
		slots = append(slots, s)
	}
	p.slots = map[string]*slot{}
	p.mu.Unlock()

	var wg sync.WaitGroup
	errs := make(chan error, len(slots))
	for _, s := range slots {
		wg.Add(1)
		go func(s *slot) {
			defer wg.Done()
			// Wait for the in-flight spawn (if any) to settle before
			// trying to stop. If it failed, there's nothing to stop.
			select {
			case <-s.done:
			case <-ctx.Done():
				errs <- ctx.Err()
				return
			}
			if s.proc == nil {
				return
			}
			if err := s.proc.Stop(ctx); err != nil {
				errs <- err
			}
		}(s)
	}
	wg.Wait()
	close(errs)
	var first error
	for err := range errs {
		if first == nil {
			first = err
		}
	}
	return first
}

// Size returns how many slots the pool currently tracks. Useful for
// diagnostics (sofarpc_doctor) and tests.
func (p *Pool) Size() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.slots)
}

// Cap returns the configured capacity. Zero means unbounded. doctor
// surfaces this alongside Size so agents can see how close the pool is
// to LRU pressure — same contract as SessionStore.Cap.
func (p *Pool) Cap() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.cap
}

func (p *Pool) runSpawn(profile Profile, s *slot) {
	defer close(s.done)
	spec := p.base
	spec.Profile = profile
	if spec.Jar == "" {
		s.err = fmt.Errorf("worker: pool base spec has no Jar; configure SOFARPC_RUNTIME_JAR")
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), spawnTimeout(spec))
	defer cancel()
	proc, err := p.spawn(ctx, spec)
	if err != nil {
		s.err = err
		return
	}
	s.proc = proc
}

// spawnTimeout is the upper bound for one spawn. We reuse ReadyTimeout
// plus a small padding so a slow JVM has room before we give up.
func spawnTimeout(spec Spec) time.Duration {
	base := spec.ReadyTimeout
	if base == 0 {
		base = 30 * time.Second
	}
	return base + 5*time.Second
}
