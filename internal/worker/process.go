package worker

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"strconv"
	"sync"
	"syscall"
	"time"
)

// Spec is everything Spawn needs to launch one worker JVM. Profile is
// keyed for the pool; the jar / java fields actually drive exec.
type Spec struct {
	// Profile keys the worker in the pool. Required (Spawn refuses
	// Profile.Empty()).
	Profile Profile

	// Java is the absolute path to the `java` binary. Empty defaults to
	// "java" on PATH — fine for tests, intentional miss for production
	// (the entrypoint should set this from SOFARPC_JAVA).
	Java string

	// Jar is the absolute path to the runtime worker jar. Required.
	Jar string

	// ExtraArgs are appended after `-jar <Jar>`. Useful for tuning the
	// worker (e.g. `--port 0` for ephemeral ports, sysprops, …).
	ExtraArgs []string

	// Env entries to merge onto the child env (KEY=VALUE format).
	Env []string

	// ReadyTimeout caps how long Spawn waits for the worker's ready
	// line on stdout. Zero defaults to 30s.
	ReadyTimeout time.Duration

	// StopGrace is how long Stop waits for a clean exit after sending
	// the shutdown action. Zero defaults to 2s, matching architecture
	// §7.4.
	StopGrace time.Duration
}

// Process is one running worker JVM plus its connected Conn. The pool
// owns the lifetime; nobody else should call Stop directly.
type Process struct {
	spec    Spec
	cmd     *exec.Cmd
	conn    *Conn
	ready   ReadyMessage
	startAt time.Time

	stopOnce sync.Once
	stopErr  error
	exited   chan struct{}
}

// Conn returns the live worker connection. It's safe to call before Stop;
// after Stop returns it may already be closed.
func (p *Process) Conn() *Conn { return p.conn }

// Exited returns a channel that closes when the underlying worker
// process has exited. Callers can non-blockingly check liveness:
//
//	select { case <-p.Exited(): /* dead */ default: /* alive */ }
//
// The pool uses this to self-heal after a crashed JVM without waiting
// for the next Send to surface an ErrConnClosed.
func (p *Process) Exited() <-chan struct{} { return p.exited }

// Ready returns the handshake message the worker emitted on stdout.
func (p *Process) Ready() ReadyMessage { return p.ready }

// Profile returns the profile this worker was spawned for.
func (p *Process) Profile() Profile { return p.spec.Profile }

// Spawn starts the worker JVM, parses its ready handshake, dials the
// reported port, and returns a Process whose Conn is ready to Send.
//
// On any failure the JVM is killed before Spawn returns, so callers don't
// have to clean up partial state.
func Spawn(ctx context.Context, spec Spec) (*Process, error) {
	if spec.Profile.Empty() {
		return nil, errors.New("worker: spawn requires a complete Profile")
	}
	if spec.Jar == "" {
		return nil, errors.New("worker: spawn requires Jar")
	}
	java := spec.Java
	if java == "" {
		java = "java"
	}
	timeout := spec.ReadyTimeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}

	args := append([]string{"-jar", spec.Jar}, spec.ExtraArgs...)
	cmd := exec.CommandContext(ctx, java, args...)
	if len(spec.Env) > 0 {
		cmd.Env = append(os.Environ(), spec.Env...)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("worker: stdout pipe: %w", err)
	}
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("worker: start: %w", err)
	}

	exited := make(chan struct{})
	go func() {
		_ = cmd.Wait()
		close(exited)
	}()

	ready, err := awaitReady(ctx, stdout, timeout)
	if err != nil {
		killAndDrain(cmd, exited)
		return nil, fmt.Errorf("worker: ready handshake: %w", err)
	}

	addr := net.JoinHostPort("127.0.0.1", strconv.Itoa(ready.Port))
	tcp, err := dialReady(ctx, addr)
	if err != nil {
		killAndDrain(cmd, exited)
		return nil, fmt.Errorf("worker: dial %s: %w", addr, err)
	}

	return &Process{
		spec:    spec,
		cmd:     cmd,
		conn:    NewConn(tcp),
		ready:   ready,
		startAt: time.Now(),
		exited:  exited,
	}, nil
}

// Stop asks the worker to shut down cleanly, waits StopGrace for the
// process to exit, then SIGTERMs (and finally SIGKILLs) it. Idempotent.
func (p *Process) Stop(ctx context.Context) error {
	p.stopOnce.Do(func() {
		grace := p.spec.StopGrace
		if grace == 0 {
			grace = 2 * time.Second
		}

		// Best-effort cooperative shutdown — ignore errors; the process
		// may already have died, in which case we still want to reap.
		shutdownCtx, cancel := context.WithTimeout(ctx, grace/2)
		_, _ = p.conn.Send(shutdownCtx, Request{Action: ActionShutdown})
		cancel()
		_ = p.conn.Close()

		select {
		case <-p.exited:
			return
		case <-time.After(grace):
		}

		// Escalate: SIGTERM → wait grace → SIGKILL.
		if p.cmd != nil && p.cmd.Process != nil {
			_ = p.cmd.Process.Signal(syscall.SIGTERM)
		}
		select {
		case <-p.exited:
			return
		case <-time.After(grace):
		}
		if p.cmd != nil && p.cmd.Process != nil {
			_ = p.cmd.Process.Kill()
		}
		// Bound the final wait so a worker whose exit channel is never
		// closed (fakes in tests, or a zombie process) cannot hang Stop
		// indefinitely. SIGKILL above should be near-instant in practice.
		select {
		case <-p.exited:
		case <-time.After(grace):
		}
	})
	return p.stopErr
}

// awaitReady reads stdout one line at a time, looking for the worker's
// ready handshake. Non-JSON lines are tolerated (the JVM may print log
// lines before flipping to JSON mode); the first line that parses as a
// ReadyMessage with Ready=true wins. Times out per timeout.
//
// The reader argument is consumed; callers should not read from it
// afterwards (the worker switches to TCP mode anyway).
func awaitReady(ctx context.Context, r io.Reader, timeout time.Duration) (ReadyMessage, error) {
	type result struct {
		ready ReadyMessage
		err   error
	}
	ch := make(chan result, 1)
	go func() {
		scanner := bufio.NewScanner(r)
		scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		for scanner.Scan() {
			line := scanner.Bytes()
			if len(line) == 0 || line[0] != '{' {
				continue
			}
			var msg ReadyMessage
			if err := json.Unmarshal(line, &msg); err != nil {
				continue
			}
			if !msg.Ready {
				continue
			}
			if msg.Port <= 0 {
				ch <- result{err: fmt.Errorf("ready message has invalid port %d", msg.Port)}
				return
			}
			ch <- result{ready: msg}
			return
		}
		if err := scanner.Err(); err != nil {
			ch <- result{err: fmt.Errorf("read stdout: %w", err)}
			return
		}
		ch <- result{err: io.EOF}
	}()

	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case res := <-ch:
		return res.ready, res.err
	case <-timer.C:
		return ReadyMessage{}, fmt.Errorf("timed out after %s", timeout)
	case <-ctx.Done():
		return ReadyMessage{}, ctx.Err()
	}
}

// dialReady retries the TCP dial briefly so callers don't race the
// worker's listen syscall. The worker prints "ready" once the listener
// is bound, but on slow CI we still occasionally see one ECONNREFUSED.
func dialReady(ctx context.Context, addr string) (net.Conn, error) {
	deadline := time.Now().Add(2 * time.Second)
	var lastErr error
	for {
		dialer := net.Dialer{Timeout: 500 * time.Millisecond}
		conn, err := dialer.DialContext(ctx, "tcp", addr)
		if err == nil {
			return conn, nil
		}
		lastErr = err
		if time.Now().After(deadline) || ctx.Err() != nil {
			break
		}
		select {
		case <-time.After(50 * time.Millisecond):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return nil, lastErr
}

func killAndDrain(cmd *exec.Cmd, exited <-chan struct{}) {
	if cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
	<-exited
}
