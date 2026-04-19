package worker

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hex1n/sofarpc-cli/internal/errcode"
)

// fakeWireErrorProcess returns a Process whose worker always responds
// with Ok=false + a WireError that carries a hint. Used to prove the
// client lifts the wire error into an *errcode.Error.
func fakeWireErrorProcess(t *testing.T) (*Process, func()) {
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
					resp := Response{
						RequestID: req.RequestID,
						Ok:        false,
						Error: &WireError{
							Code:    "runtime.deserialize-failed",
							Message: "bad json",
							Phase:   "worker",
							Hint: map[string]any{
								"nextTool": "sofarpc_describe",
								"nextArgs": map[string]any{"service": "x"},
								"reason":   "inspect method shape",
							},
						},
					}
					body, _ := json.Marshal(resp)
					writer.Write(body)
					writer.WriteByte('\n')
					writer.Flush()
				}
			}
			if err != nil {
				return
			}
		}
	}()

	exited := make(chan struct{})
	proc := &Process{
		spec:   Spec{Jar: "fake", StopGrace: 10 * time.Millisecond},
		conn:   NewConn(clientSide),
		exited: exited,
	}
	return proc, func() {
		select {
		case <-exited:
		default:
			close(exited)
		}
		proc.Stop(context.Background())
		<-done
	}
}

func TestClient_NilIsDaemonUnavailable(t *testing.T) {
	var c *Client
	_, err := c.Invoke(context.Background(), Request{Action: ActionInvoke})
	assertErrcode(t, err, errcode.DaemonUnavailable)
}

func TestClient_EmptyProfileIsDaemonUnavailable(t *testing.T) {
	c := &Client{Pool: NewPool(Spec{Jar: "fake.jar"})}
	_, err := c.Invoke(context.Background(), Request{Action: ActionInvoke})
	assertErrcode(t, err, errcode.DaemonUnavailable)
}

func TestClient_SpawnFailureIsDaemonUnavailable(t *testing.T) {
	pool := NewPool(Spec{Jar: "fake.jar"}).withSpawner(func(context.Context, Spec) (*Process, error) {
		return nil, errors.New("exec: no such file")
	})
	c := &Client{Pool: pool, Profile: fullProfile("x")}
	_, err := c.Invoke(context.Background(), Request{Action: ActionInvoke})
	assertErrcode(t, err, errcode.DaemonUnavailable)
}

func TestClient_RoundTripSuccess(t *testing.T) {
	sp := func(context.Context, Spec) (*Process, error) {
		proc, _ := fakeProcess(t, fullProfile("x"))
		return proc, nil
	}
	pool := NewPool(Spec{Jar: "fake.jar"}).withSpawner(sp)
	c := &Client{Pool: pool, Profile: fullProfile("x")}
	defer c.Close(context.Background())

	resp, err := c.Invoke(context.Background(), Request{
		Action: ActionInvoke, Service: "s", Method: "m",
	})
	if err != nil {
		t.Fatalf("invoke: %v", err)
	}
	if !resp.Ok {
		t.Fatalf("expected Ok=true, got %+v", resp)
	}
}

func TestClient_WireErrorLifted(t *testing.T) {
	sp := func(context.Context, Spec) (*Process, error) {
		proc, _ := fakeWireErrorProcess(t)
		return proc, nil
	}
	pool := NewPool(Spec{Jar: "fake.jar"}).withSpawner(sp)
	c := &Client{Pool: pool, Profile: fullProfile("x")}
	defer c.Close(context.Background())

	_, err := c.Invoke(context.Background(), Request{Action: ActionInvoke})
	assertErrcode(t, err, errcode.Code("runtime.deserialize-failed"))
	var ecerr *errcode.Error
	if !errors.As(err, &ecerr) {
		t.Fatal("error should unwrap as *errcode.Error")
	}
	if ecerr.Message != "bad json" {
		t.Fatalf("message: got %q want bad json", ecerr.Message)
	}
	if ecerr.Hint == nil || ecerr.Hint.NextTool != "sofarpc_describe" {
		t.Fatalf("hint not lifted: %+v", ecerr.Hint)
	}
}

// fakeDyingProcess returns a Process whose server side hangs up after
// reading exactly one request, simulating a worker that crashed mid-
// call. The client's Send observes ErrConnClosed, which triggers the
// Invoke retry path.
func fakeDyingProcess(t *testing.T, profile Profile) *Process {
	t.Helper()
	serverSide, clientSide := net.Pipe()
	go func() {
		defer serverSide.Close()
		reader := bufio.NewReader(serverSide)
		_, _ = reader.ReadBytes('\n')
	}()
	return &Process{
		spec:   Spec{Profile: profile, Jar: "fake", StopGrace: 10 * time.Millisecond},
		conn:   NewConn(clientSide),
		exited: make(chan struct{}),
	}
}

func TestClient_RetriesAfterConnClosed(t *testing.T) {
	var counter atomic.Int32
	sp := func(context.Context, Spec) (*Process, error) {
		n := counter.Add(1)
		if n == 1 {
			return fakeDyingProcess(t, fullProfile("x")), nil
		}
		proc, _ := fakeProcess(t, fullProfile("x"))
		return proc, nil
	}
	pool := NewPool(Spec{Jar: "fake.jar"}).withSpawner(sp)
	c := &Client{Pool: pool, Profile: fullProfile("x")}
	defer c.Close(context.Background())

	resp, err := c.Invoke(context.Background(), Request{Action: ActionInvoke})
	if err != nil {
		t.Fatalf("invoke should self-heal, got error: %v", err)
	}
	if !resp.Ok {
		t.Fatalf("expected Ok=true after retry, got %+v", resp)
	}
	if got := counter.Load(); got != 2 {
		t.Fatalf("spawn count: got %d want 2 (original + respawn)", got)
	}
}

func TestClient_RetryExhaustedSurfacesDaemonUnavailable(t *testing.T) {
	var counter atomic.Int32
	sp := func(context.Context, Spec) (*Process, error) {
		counter.Add(1)
		// Both spawns return dying workers; the retry should give up
		// and surface DaemonUnavailable rather than loop forever.
		return fakeDyingProcess(t, fullProfile("x")), nil
	}
	pool := NewPool(Spec{Jar: "fake.jar"}).withSpawner(sp)
	c := &Client{Pool: pool, Profile: fullProfile("x")}
	defer c.Close(context.Background())

	_, err := c.Invoke(context.Background(), Request{Action: ActionInvoke})
	assertErrcode(t, err, errcode.DaemonUnavailable)
	if got := counter.Load(); got != 2 {
		t.Fatalf("spawn count: got %d want 2 (exactly one retry)", got)
	}
}

func TestClient_CloseIsNilSafe(t *testing.T) {
	var c *Client
	if err := c.Close(context.Background()); err != nil {
		t.Fatalf("Close on nil client should be a no-op, got %v", err)
	}
}

// --- helpers --------------------------------------------------------

func assertErrcode(t *testing.T, err error, want errcode.Code) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected error %s, got nil", want)
	}
	var ecerr *errcode.Error
	if !errors.As(err, &ecerr) {
		t.Fatalf("expected *errcode.Error, got %T: %v", err, err)
	}
	if ecerr.Code != want {
		t.Fatalf("code: got %q want %q", ecerr.Code, want)
	}
}
