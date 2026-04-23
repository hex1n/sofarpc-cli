package mcp

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/hex1n/sofarpc-cli/internal/core/invoke"
	"github.com/hex1n/sofarpc-cli/internal/core/target"
)

const (
	// defaultSessionTTL drops sessions idle for longer than a day. Sessions
	// are advisory (a process restart is always a clean reset), so the
	// ceiling can be generous — we just don't want MCP servers that stay
	// up for weeks to accumulate dead entries.
	defaultSessionTTL = 24 * time.Hour

	// defaultSessionCap caps concurrent sessions. Typical agent usage keeps
	// one or two open workspaces; 256 leaves a wide runway before LRU bites.
	defaultSessionCap = 256

	// defaultSessionPlanMaxBytes caps how much captured invoke plan data a
	// session may retain for sessionId replay. It does not truncate or
	// redact the plan returned to the caller; it only bounds in-memory
	// session retention.
	defaultSessionPlanMaxBytes = int64(1 << 20)
	envSessionPlanMaxBytes     = "SOFARPC_SESSION_PLAN_MAX_BYTES"
)

// PlanCaptureResult reports whether an invoke plan was attached to a session
// for sessionId replay. Failed capture is advisory: invoke/dry-run can still
// return the full plan to the caller, and payload replay remains available.
type PlanCaptureResult struct {
	Captured  bool   `json:"captured"`
	Reason    string `json:"reason,omitempty"`
	PlanBytes int64  `json:"planBytes,omitempty"`
	MaxBytes  int64  `json:"maxBytes,omitempty"`
}

// Session is a per-workspace snapshot the agent can refer back to by ID
// in subsequent calls, so it does not have to re-specify project/context
// or rebuild the invocation plan. Sessions live only in memory — they
// are recreated on every process start.
type Session struct {
	ID          string        `json:"id"`
	ProjectRoot string        `json:"projectRoot"`
	Target      target.Config `json:"target,omitempty"`
	CreatedAt   time.Time     `json:"createdAt"`
	// LastPlan is the most recent plan produced by sofarpc_invoke for
	// this session. sofarpc_replay reads it when called with sessionId.
	LastPlan *invoke.Plan `json:"lastPlan,omitempty"`

	// lastUsed is the GC anchor: Get / UpdatePlan bump it so active
	// sessions don't expire. Unexported + json:"-" so it stays an
	// implementation detail.
	lastUsed time.Time
}

// SessionStore is a bounded in-memory registry keyed by session ID.
//
// GC runs on-write (Create / UpdatePlan) — no background goroutine, so
// there is no Stop() lifecycle to manage. Three dimensions bound memory:
//
//   - Idle TTL: sessions whose lastUsed is older than ttl are dropped
//     on the next write. Bumped by Get and UpdatePlan so active
//     sessions survive.
//   - Capacity: when len reaches cap, the LRU entry is evicted to make
//     room. A zero ttl or cap disables that dimension (unbounded growth
//     in tests only).
//   - Captured plan bytes: sessionId replay keeps only plans whose JSON
//     representation is at or below maxPlanBytes. A zero max disables
//     the captured-plan byte bound.
//
// Safe for concurrent use.
type SessionStore struct {
	ttl          time.Duration
	cap          int
	maxPlanBytes int64
	clock        func() time.Time
	newID        func() string

	mu       sync.Mutex
	sessions map[string]Session
}

// NewSessionStore returns a store with default TTL (24h), capacity (256),
// and captured-plan byte limit. SOFARPC_SESSION_PLAN_MAX_BYTES overrides the
// captured-plan byte limit; set it to 0 to disable that bound.
func NewSessionStore() *SessionStore {
	return NewSessionStoreWithLimits(defaultSessionTTL, defaultSessionCap)
}

// NewSessionStoreWithLimits returns a store with explicit idle TTL and
// capacity. A zero value for either disables that dimension of GC.
// Use NewSessionStore in production — this exists for tests and for
// callers that need to tune the bounds.
func NewSessionStoreWithLimits(ttl time.Duration, capacity int) *SessionStore {
	return &SessionStore{
		ttl:          ttl,
		cap:          capacity,
		maxPlanBytes: sessionPlanMaxBytesFromEnv(),
		clock:        time.Now,
		newID:        randomSessionID,
		sessions:     map[string]Session{},
	}
}

// WithIDFunc sets the ID generator (tests use this to get deterministic IDs).
func (s *SessionStore) WithIDFunc(fn func() string) *SessionStore {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.newID = fn
	return s
}

// WithClock swaps the clock for tests that need deterministic TTL
// behaviour. Production callers should leave the default (time.Now).
func (s *SessionStore) WithClock(fn func() time.Time) *SessionStore {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.clock = fn
	return s
}

// WithMaxPlanBytes swaps the captured-plan byte limit. A zero value disables
// the byte bound. Production callers should prefer SOFARPC_SESSION_PLAN_MAX_BYTES.
func (s *SessionStore) WithMaxPlanBytes(maxBytes int64) *SessionStore {
	s.mu.Lock()
	defer s.mu.Unlock()
	if maxBytes < 0 {
		maxBytes = defaultSessionPlanMaxBytes
	}
	s.maxPlanBytes = maxBytes
	return s
}

// Create records a new session and returns it with an ID and timestamp
// populated. The caller-supplied session's ID, CreatedAt, and lastUsed
// are ignored. GC runs before the insert so a long-idle server doesn't
// grow without bound.
func (s *SessionStore) Create(session Session) Session {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.clock().UTC()
	s.sweepExpiredLocked(now)
	s.enforceCapLocked()
	session.ID = s.newID()
	session.CreatedAt = now
	session.lastUsed = now
	s.sessions[session.ID] = session
	return session
}

// Get returns a copy of the stored session and bumps its LRU timestamp
// so that subsequent capacity pressure doesn't evict actively-used IDs.
// ok=false means no such ID (either never existed or already expired).
func (s *SessionStore) Get(id string) (Session, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	session, ok := s.sessions[id]
	if !ok {
		return Session{}, false
	}
	session.lastUsed = s.clock().UTC()
	s.sessions[id] = session
	return session, true
}

// CapturePlan attaches the most recent invoke plan to a session so that
// sofarpc_replay can replay it by sessionId. Oversized plans are not stored,
// but the session is still treated as active and the caller can replay by
// passing the returned dry-run plan as payload.
func (s *SessionStore) CapturePlan(id string, plan invoke.Plan) PlanCaptureResult {
	if id == "" {
		return PlanCaptureResult{Captured: false, Reason: "empty-session-id"}
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	session, ok := s.sessions[id]
	if !ok {
		return PlanCaptureResult{Captured: false, Reason: "session-not-found"}
	}

	planBytes, err := planSizeBytes(plan)
	if err != nil {
		session.lastUsed = s.clock().UTC()
		s.sessions[id] = session
		return PlanCaptureResult{Captured: false, Reason: "plan-size-unavailable"}
	}
	if s.maxPlanBytes > 0 && planBytes > s.maxPlanBytes {
		session.LastPlan = nil
		session.lastUsed = s.clock().UTC()
		s.sessions[id] = session
		return PlanCaptureResult{
			Captured:  false,
			Reason:    "plan-too-large",
			PlanBytes: planBytes,
			MaxBytes:  s.maxPlanBytes,
		}
	}

	clone := plan
	session.LastPlan = &clone
	session.lastUsed = s.clock().UTC()
	s.sessions[id] = session
	return PlanCaptureResult{Captured: true, Reason: "captured", PlanBytes: planBytes, MaxBytes: s.maxPlanBytes}
}

// UpdatePlan attaches the most recent invoke plan to a session so that
// sofarpc_replay can replay it by sessionId. It is a no-op (returning
// false) when the session does not exist or the plan exceeds the captured-plan
// byte limit — callers treat replay capture as advisory and should not fail
// the invoke on this. Also bumps the LRU timestamp since a plan write attempt
// is a clear "session is alive" signal.
func (s *SessionStore) UpdatePlan(id string, plan invoke.Plan) bool {
	return s.CapturePlan(id, plan).Captured
}

// Size returns the number of live sessions. Useful for diagnostics and
// for tests that need to observe GC behaviour.
func (s *SessionStore) Size() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.sessions)
}

// Cap returns the configured capacity. Zero means unbounded.
// sofarpc_doctor surfaces this alongside Size so agents can see how
// close they are to LRU pressure.
func (s *SessionStore) Cap() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cap
}

// MaxPlanBytes returns the configured captured-plan byte limit. Zero means
// unbounded.
func (s *SessionStore) MaxPlanBytes() int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.maxPlanBytes
}

// sweepExpiredLocked drops every session whose lastUsed is older than
// now-ttl. O(n) but n is bounded by cap, and this runs only on Create.
func (s *SessionStore) sweepExpiredLocked(now time.Time) {
	if s.ttl <= 0 {
		return
	}
	cutoff := now.Add(-s.ttl)
	for id, session := range s.sessions {
		if session.lastUsed.Before(cutoff) {
			delete(s.sessions, id)
		}
	}
}

// enforceCapLocked drops LRU entries until there is room for one more.
// O(k*n) with k = number evicted; in steady state k is 0 or 1, so we
// don't bother maintaining a heap.
func (s *SessionStore) enforceCapLocked() {
	if s.cap <= 0 {
		return
	}
	for len(s.sessions) >= s.cap {
		var lruID string
		var lruTime time.Time
		for id, sess := range s.sessions {
			if lruID == "" || sess.lastUsed.Before(lruTime) {
				lruID = id
				lruTime = sess.lastUsed
			}
		}
		if lruID == "" {
			return
		}
		delete(s.sessions, lruID)
	}
}

func randomSessionID() string {
	var buf [8]byte
	if _, err := rand.Read(buf[:]); err != nil {
		// rand.Read only fails on catastrophic platform errors. Fall back
		// to a timestamp so we never hand out an empty ID.
		return "ws_" + hex.EncodeToString([]byte(time.Now().UTC().Format("20060102150405.000000000")))
	}
	return "ws_" + hex.EncodeToString(buf[:])
}

func sessionPlanMaxBytesFromEnv() int64 {
	raw := strings.TrimSpace(os.Getenv(envSessionPlanMaxBytes))
	if raw == "" {
		return defaultSessionPlanMaxBytes
	}
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || value < 0 {
		return defaultSessionPlanMaxBytes
	}
	return value
}

func planSizeBytes(plan invoke.Plan) (int64, error) {
	body, err := json.Marshal(plan)
	if err != nil {
		return 0, fmt.Errorf("marshal plan: %w", err)
	}
	return int64(len(body)), nil
}
