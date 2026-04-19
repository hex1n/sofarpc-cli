package worker

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"net"
	"sync"
	"testing"
	"time"
)

// fakeWorker is a minimal stand-in for the Java worker JVM. It accepts
// one TCP connection, reads line-delimited Requests, and lets a test
// supply a handler that produces Responses. The handler may delay or
// drop requests to exercise concurrency / cancellation paths.
type fakeWorker struct {
	t        *testing.T
	listener net.Listener
	handler  func(Request) (Response, bool)
	wg       sync.WaitGroup

	connM sync.Mutex
	conn  net.Conn
}

func startFakeWorker(t *testing.T, handler func(Request) (Response, bool)) *fakeWorker {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	fw := &fakeWorker{t: t, listener: listener, handler: handler}
	fw.wg.Add(1)
	go fw.serve()
	return fw
}

func (fw *fakeWorker) serve() {
	defer fw.wg.Done()
	conn, err := fw.listener.Accept()
	if err != nil {
		return
	}
	fw.connM.Lock()
	fw.conn = conn
	fw.connM.Unlock()
	defer conn.Close()
	reader := bufio.NewReader(conn)
	writer := bufio.NewWriter(conn)
	for {
		line, err := reader.ReadBytes('\n')
		if len(line) > 0 {
			var req Request
			if err := json.Unmarshal(line, &req); err != nil {
				fw.t.Errorf("fake worker decode: %v", err)
				return
			}
			resp, send := fw.handler(req)
			if send {
				resp.RequestID = req.RequestID
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
}

func (fw *fakeWorker) addr() string {
	return fw.listener.Addr().String()
}

// stop closes both the listener (no new connections) and the accepted
// connection (unblocks the serve goroutine reading from the socket).
func (fw *fakeWorker) stop() {
	fw.listener.Close()
	fw.connM.Lock()
	if fw.conn != nil {
		fw.conn.Close()
	}
	fw.connM.Unlock()
	fw.wg.Wait()
}

func dial(t *testing.T, addr string) *Conn {
	t.Helper()
	c, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	return NewConn(c)
}

func TestConn_RoundTrip(t *testing.T) {
	fw := startFakeWorker(t, func(req Request) (Response, bool) {
		return Response{Ok: true, Result: req.Service + "." + req.Method}, true
	})
	defer fw.stop()

	c := dial(t, fw.addr())
	defer c.Close()

	resp, err := c.Send(context.Background(), Request{
		Action: ActionInvoke, Service: "com.foo.Svc", Method: "doThing",
	})
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	if !resp.Ok || resp.Result != "com.foo.Svc.doThing" {
		t.Fatalf("unexpected response: %+v", resp)
	}
	if resp.RequestID == "" {
		t.Fatal("response should echo a non-empty requestId")
	}
}

func TestConn_ConcurrentRequestsAreDemuxed(t *testing.T) {
	// Worker delays each response by 50ms and answers in arrival order.
	// Two callers fire in parallel; both must get their own responses.
	fw := startFakeWorker(t, func(req Request) (Response, bool) {
		time.Sleep(20 * time.Millisecond)
		return Response{Ok: true, Result: req.Method}, true
	})
	defer fw.stop()

	c := dial(t, fw.addr())
	defer c.Close()

	const n = 8
	results := make([]Response, n)
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			method := "m" + string(rune('A'+i))
			resp, err := c.Send(context.Background(), Request{Action: ActionInvoke, Method: method})
			if err != nil {
				t.Errorf("send %d: %v", i, err)
				return
			}
			results[i] = resp
		}(i)
	}
	wg.Wait()

	for i, r := range results {
		want := "m" + string(rune('A'+i))
		if r.Result != want {
			t.Fatalf("response %d: got %v want %s — demux likely wrong", i, r.Result, want)
		}
	}
}

func TestConn_ContextCancelUnblocksSend(t *testing.T) {
	// Worker accepts requests but never responds; ctx cancel must wake Send.
	fw := startFakeWorker(t, func(Request) (Response, bool) { return Response{}, false })
	defer fw.stop()

	c := dial(t, fw.addr())
	defer c.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err := c.Send(ctx, Request{Action: ActionInvoke})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected DeadlineExceeded, got %v", err)
	}
}

func TestConn_RemoteCloseUnblocksSend(t *testing.T) {
	fw := startFakeWorker(t, func(Request) (Response, bool) { return Response{}, false })

	c := dial(t, fw.addr())
	defer c.Close()

	done := make(chan error, 1)
	go func() {
		_, err := c.Send(context.Background(), Request{Action: ActionInvoke})
		done <- err
	}()

	// Give Send a moment to register, then drop the worker.
	time.Sleep(20 * time.Millisecond)
	fw.stop()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected error after remote close, got nil")
		}
	case <-time.After(time.Second):
		t.Fatal("Send did not unblock after remote close")
	}
}

func TestConn_SendAfterCloseFails(t *testing.T) {
	fw := startFakeWorker(t, func(Request) (Response, bool) { return Response{Ok: true}, true })
	defer fw.stop()

	c := dial(t, fw.addr())
	c.Close()

	_, err := c.Send(context.Background(), Request{Action: ActionInvoke})
	if !errors.Is(err, ErrConnClosed) {
		t.Fatalf("expected ErrConnClosed, got %v", err)
	}
}
