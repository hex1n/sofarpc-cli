package mcp

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/hex1n/sofarpc-cli/internal/core/invoke"
	"github.com/hex1n/sofarpc-cli/internal/core/target"
	"github.com/hex1n/sofarpc-cli/internal/errcode"
)

const (
	defaultMaxConcurrentInvokes          = 16
	defaultMaxConcurrentInvokesPerTarget = 8
	defaultInvokeQueueTimeout            = time.Second
)

// InvokeLimiterConfig controls admission for real invoke/replay work. A zero
// limit disables that dimension; New() uses NewInvokeLimiterFromEnv, whose
// defaults keep a bounded queue instead of allowing unbounded fan-out.
type InvokeLimiterConfig struct {
	GlobalLimit    int
	PerTargetLimit int
	QueueTimeout   time.Duration
}

// InvokeLimiterDiagnostics is surfaced in tool diagnostics and doctor output
// so agents can distinguish a busy runtime from transport or serialization
// failures.
type InvokeLimiterDiagnostics struct {
	Enabled           bool   `json:"enabled"`
	GlobalLimit       int    `json:"globalLimit,omitempty"`
	GlobalInUse       int    `json:"globalInUse"`
	PerTargetLimit    int    `json:"perTargetLimit,omitempty"`
	Target            string `json:"target,omitempty"`
	TargetInUse       int    `json:"targetInUse,omitempty"`
	QueueTimeoutMS    int64  `json:"queueTimeoutMs"`
	WaitingForRelease bool   `json:"waitingForRelease,omitempty"`
}

// InvokeLimiter is a workload-aware bulkhead shared by real invoke and replay.
// It deliberately limits application work before the SOFARPC runtime path;
// dry-run planning does not acquire a slot.
type InvokeLimiter struct {
	cfg InvokeLimiterConfig

	mu          sync.Mutex
	globalInUse int
	perTarget   map[string]int
	notify      chan struct{}
}

func NewInvokeLimiterFromEnv() *InvokeLimiter {
	cfg := InvokeLimiterConfig{
		GlobalLimit:    envInt(invoke.EnvMaxConcurrentInvokes, defaultMaxConcurrentInvokes),
		PerTargetLimit: envInt(invoke.EnvMaxConcurrentInvokesPerTarget, defaultMaxConcurrentInvokesPerTarget),
		QueueTimeout:   time.Duration(envInt(invoke.EnvInvokeQueueTimeoutMS, int(defaultInvokeQueueTimeout/time.Millisecond))) * time.Millisecond,
	}
	return NewInvokeLimiter(cfg)
}

func NewInvokeLimiter(cfg InvokeLimiterConfig) *InvokeLimiter {
	if cfg.GlobalLimit < 0 {
		cfg.GlobalLimit = 0
	}
	if cfg.PerTargetLimit < 0 {
		cfg.PerTargetLimit = 0
	}
	if cfg.QueueTimeout < 0 {
		cfg.QueueTimeout = 0
	}
	return &InvokeLimiter{
		cfg:       cfg,
		perTarget: map[string]int{},
		notify:    make(chan struct{}),
	}
}

func (l *InvokeLimiter) Acquire(ctx context.Context, phase string, plan invoke.Plan) (func(), InvokeLimiterDiagnostics, error) {
	if l == nil {
		return func() {}, InvokeLimiterDiagnostics{}, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	targetKey := invokeLimitTargetKey(plan)
	if !l.enabled() {
		return func() {}, l.DiagnosticsForTarget(targetKey), nil
	}

	waitCtx := ctx
	var cancel context.CancelFunc
	if l.cfg.QueueTimeout > 0 {
		waitCtx, cancel = context.WithTimeout(ctx, l.cfg.QueueTimeout)
		defer cancel()
	}

	for {
		l.mu.Lock()
		if l.canAcquireLocked(targetKey) {
			l.globalInUse++
			if targetKey != "" {
				l.perTarget[targetKey]++
			}
			diag := l.diagnosticsLocked(targetKey, false)
			release := l.releaseFunc(targetKey)
			l.mu.Unlock()
			return release, diag, nil
		}
		diag := l.diagnosticsLocked(targetKey, true)
		notify := l.notify
		l.mu.Unlock()

		select {
		case <-waitCtx.Done():
			return nil, diag, l.busyError(phase, targetKey, waitCtx.Err())
		case <-notify:
		}
	}
}

func (l *InvokeLimiter) Diagnostics() InvokeLimiterDiagnostics {
	if l == nil {
		return InvokeLimiterDiagnostics{}
	}
	return l.DiagnosticsForTarget("")
}

func (l *InvokeLimiter) DiagnosticsForPlan(plan invoke.Plan) InvokeLimiterDiagnostics {
	if l == nil {
		return InvokeLimiterDiagnostics{}
	}
	return l.DiagnosticsForTarget(invokeLimitTargetKey(plan))
}

func (l *InvokeLimiter) DiagnosticsForTarget(targetKey string) InvokeLimiterDiagnostics {
	if l == nil {
		return InvokeLimiterDiagnostics{}
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.diagnosticsLocked(targetKey, false)
}

func (l *InvokeLimiter) enabled() bool {
	return l != nil && (l.cfg.GlobalLimit > 0 || l.cfg.PerTargetLimit > 0)
}

func (l *InvokeLimiter) canAcquireLocked(targetKey string) bool {
	if l.cfg.GlobalLimit > 0 && l.globalInUse >= l.cfg.GlobalLimit {
		return false
	}
	if targetKey != "" && l.cfg.PerTargetLimit > 0 && l.perTarget[targetKey] >= l.cfg.PerTargetLimit {
		return false
	}
	return true
}

func (l *InvokeLimiter) diagnosticsLocked(targetKey string, waiting bool) InvokeLimiterDiagnostics {
	diag := InvokeLimiterDiagnostics{
		Enabled:           l.enabled(),
		GlobalLimit:       l.cfg.GlobalLimit,
		GlobalInUse:       l.globalInUse,
		PerTargetLimit:    l.cfg.PerTargetLimit,
		Target:            targetKey,
		QueueTimeoutMS:    int64(l.cfg.QueueTimeout / time.Millisecond),
		WaitingForRelease: waiting,
	}
	if targetKey != "" {
		diag.TargetInUse = l.perTarget[targetKey]
	}
	return diag
}

func (l *InvokeLimiter) releaseFunc(targetKey string) func() {
	var once sync.Once
	return func() {
		once.Do(func() {
			l.mu.Lock()
			defer l.mu.Unlock()
			if l.globalInUse > 0 {
				l.globalInUse--
			}
			if targetKey != "" {
				if current := l.perTarget[targetKey]; current <= 1 {
					delete(l.perTarget, targetKey)
				} else {
					l.perTarget[targetKey] = current - 1
				}
			}
			old := l.notify
			l.notify = make(chan struct{})
			close(old)
		})
	}
}

func (l *InvokeLimiter) busyError(phase, targetKey string, cause error) *errcode.Error {
	phase = strings.TrimSpace(phase)
	if phase == "" {
		phase = "invoke"
	}
	message := fmt.Sprintf("%s concurrency limit reached", phase)
	if targetKey != "" {
		message += ": target=" + targetKey
	}
	if cause != nil {
		message += ": " + cause.Error()
	}
	return errcode.New(errcode.InvocationBusy, phase, message).
		WithHint("sofarpc_doctor", nil, "doctor reports current invoke concurrency limits and queue timeout")
}

func invokeLimitTargetKey(plan invoke.Plan) string {
	cfg := target.Normalize(plan.Target)
	if cfg.DirectURL != "" {
		if dial, err := target.ParseDirectDialAddress(cfg.DirectURL); err == nil {
			return strings.ToLower(dial)
		}
		return strings.ToLower(strings.TrimSpace(cfg.DirectURL))
	}
	if cfg.RegistryAddress != "" {
		return "registry:" + strings.ToLower(strings.TrimSpace(cfg.RegistryAddress))
	}
	if cfg.Mode != "" {
		return "mode:" + strings.ToLower(strings.TrimSpace(cfg.Mode))
	}
	return "service:" + strings.ToLower(strings.TrimSpace(plan.Service))
}

func envInt(name string, fallback int) int {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return fallback
	}
	return value
}
