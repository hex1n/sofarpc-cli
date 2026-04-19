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

// fakeMidFlightDyingProcess returns a Process whose server side hangs
// up after reading exactly one request. The client successfully
// flushes the request, then observes the close while waiting for a
// response — that's the ErrConnLost / InvocationUncertain path, not a
// safe-to-retry pre-send close.
func fakeMidFlightDyingProcess(t *testing.T, profile Profile) *Process {
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

// fakePreSendDeadProcess returns a Process whose Conn is already
// closed on arrival. Send's pre-write gate catches this and returns
// ErrConnClosed before the request hits the wire — safe to retry, and
// the behaviour the client's self-heal path is designed for.
func fakePreSendDeadProcess(t *testing.T, profile Profile) *Process {
	t.Helper()
	serverSide, clientSide := net.Pipe()
	serverSide.Close()
	conn := NewConn(clientSide)
	conn.Close()
	return &Process{
		spec:   Spec{Profile: profile, Jar: "fake", StopGrace: 10 * time.Millisecond},
		conn:   conn,
		exited: make(chan struct{}),
	}
}

func TestClient_RetriesOnPreSendClose(t *testing.T) {
	var counter atomic.Int32
	sp := func(context.Context, Spec) (*Process, error) {
		n := counter.Add(1)
		if n == 1 {
			return fakePreSendDeadProcess(t, fullProfile("x")), nil
		}
		proc, _ := fakeProcess(t, fullProfile("x"))
		return proc, nil
	}
	pool := NewPool(Spec{Jar: "fake.jar"}).withSpawner(sp)
	c := &Client{Pool: pool, Profile: fullProfile("x")}
	defer c.Close(context.Background())

	resp, err := c.Invoke(context.Background(), Request{Action: ActionInvoke})
	if err != nil {
		t.Fatalf("invoke should self-heal on a pre-send close, got: %v", err)
	}
	if !resp.Ok {
		t.Fatalf("expected Ok=true after retry, got %+v", resp)
	}
	if got := counter.Load(); got != 2 {
		t.Fatalf("spawn count: got %d want 2 (original + respawn)", got)
	}
}

// The mid-flight case MUST NOT retry: the worker may have processed
// the request before it died, and a silent replay would double-apply
// a non-idempotent action. The client evicts the dead slot and
// surfaces InvocationUncertain so the agent can decide.
func TestClient_MidFlightLossIsUncertainNoRetry(t *testing.T) {
	var counter atomic.Int32
	sp := func(context.Context, Spec) (*Process, error) {
		counter.Add(1)
		return fakeMidFlightDyingProcess(t, fullProfile("x")), nil
	}
	pool := NewPool(Spec{Jar: "fake.jar"}).withSpawner(sp)
	c := &Client{Pool: pool, Profile: fullProfile("x")}
	defer c.Close(context.Background())

	_, err := c.Invoke(context.Background(), Request{Action: ActionInvoke})
	assertErrcode(t, err, errcode.InvocationUncertain)
	if got := counter.Load(); got != 1 {
		t.Fatalf("spawn count: got %d want 1 (no retry on mid-flight loss)", got)
	}
}

func TestClient_RetryExhaustedSurfacesDaemonUnavailable(t *testing.T) {
	var counter atomic.Int32
	sp := func(context.Context, Spec) (*Process, error) {
		counter.Add(1)
		// Both spawns return pre-send-dead workers, so the retry path
		// fires but the second attempt also fails. The client must
		// give up with DaemonUnavailable rather than loop forever.
		return fakePreSendDeadProcess(t, fullProfile("x")), nil
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

// fakeDescribeProcess answers ActionDescribe by synthesising a Class
// payload keyed on the requested service. Any other action or an
// unknown service surfaces a WireError so the client's decode path
// gets exercised.
func fakeDescribeProcess(t *testing.T, known map[string]map[string]any) (*Process, func()) {
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
					var resp Response
					resp.RequestID = req.RequestID
					if req.Action != ActionDescribe {
						resp.Ok = false
						resp.Error = &WireError{Code: "runtime.worker-error", Message: "unexpected action"}
					} else if cls, ok := known[req.Service]; ok {
						resp.Ok = true
						resp.Result = cls
					} else {
						resp.Ok = false
						resp.Error = &WireError{
							Code:    string(errcode.ContractUnresolvable),
							Message: "class not on facade classpath: " + req.Service,
						}
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

func TestClient_DescribeDecodesClass(t *testing.T) {
	known := map[string]map[string]any{
		"com.foo.Svc": {
			"fqn":        "com.foo.Svc",
			"simpleName": "Svc",
			"kind":       "interface",
			"methods": []map[string]any{
				{"name": "doThing", "paramTypes": []string{"java.lang.String"}, "returnType": "java.lang.Long"},
			},
		},
	}
	sp := func(context.Context, Spec) (*Process, error) {
		proc, _ := fakeDescribeProcess(t, known)
		return proc, nil
	}
	pool := NewPool(Spec{Jar: "fake.jar"}).withSpawner(sp)
	c := &Client{Pool: pool, Profile: fullProfile("x")}
	defer c.Close(context.Background())

	cls, ok, err := c.Describe(context.Background(), "com.foo.Svc")
	if err != nil {
		t.Fatalf("describe: %v", err)
	}
	if !ok {
		t.Fatal("expected ok=true")
	}
	if cls.FQN != "com.foo.Svc" {
		t.Fatalf("fqn: got %q", cls.FQN)
	}
	if len(cls.Methods) != 1 || cls.Methods[0].Name != "doThing" {
		t.Fatalf("methods: got %+v", cls.Methods)
	}
	if cls.Methods[0].ReturnType != "java.lang.Long" {
		t.Fatalf("returnType: got %q", cls.Methods[0].ReturnType)
	}
}

func TestClient_DescribeUnresolvableIsMiss(t *testing.T) {
	sp := func(context.Context, Spec) (*Process, error) {
		proc, _ := fakeDescribeProcess(t, nil)
		return proc, nil
	}
	pool := NewPool(Spec{Jar: "fake.jar"}).withSpawner(sp)
	c := &Client{Pool: pool, Profile: fullProfile("x")}
	defer c.Close(context.Background())

	_, ok, err := c.Describe(context.Background(), "com.unknown.Type")
	if err != nil {
		t.Fatalf("unresolvable should be a silent miss, got err=%v", err)
	}
	if ok {
		t.Fatal("ok should be false for unresolvable")
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
