package worker

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"sync"
	"sync/atomic"
)

// Conn wraps one TCP connection to a worker JVM and owns the
// line-delimited JSON RPC: write a Request as one JSON line, read
// Responses as JSON lines. RequestID correlates them so multiple
// callers can have requests in flight at once.
//
// Lifetime: NewConn → Send… → Close. After Close, every in-flight Send
// returns ErrConnClosed; every new Send returns ErrConnClosed too.
type Conn struct {
	conn   net.Conn
	writer *bufio.Writer
	writeM sync.Mutex
	nextID atomic.Uint64

	pendingM sync.Mutex
	pending  map[string]chan Response
	closed   chan struct{}
	closeOnce sync.Once
	readErr  atomic.Value // error
}

// ErrConnClosed is returned from Send after the conn has been closed
// (locally via Close, or remotely by the worker dropping the socket).
var ErrConnClosed = errors.New("worker: connection closed")

// NewConn takes ownership of c and starts the reader goroutine. The
// caller MUST eventually call Conn.Close to release the goroutine.
func NewConn(c net.Conn) *Conn {
	conn := &Conn{
		conn:    c,
		writer:  bufio.NewWriter(c),
		pending: map[string]chan Response{},
		closed:  make(chan struct{}),
	}
	go conn.readLoop()
	return conn
}

// Send writes req, then blocks until either a matching Response arrives,
// ctx is cancelled, or the conn closes. RequestID is overwritten with a
// per-conn unique value so callers do not have to manage it.
func (c *Conn) Send(ctx context.Context, req Request) (Response, error) {
	id := strconv.FormatUint(c.nextID.Add(1), 10)
	req.RequestID = id

	waiter := make(chan Response, 1)
	c.pendingM.Lock()
	if c.isClosedLocked() {
		c.pendingM.Unlock()
		return Response{}, ErrConnClosed
	}
	c.pending[id] = waiter
	c.pendingM.Unlock()

	if err := c.writeRequest(req); err != nil {
		c.dropPending(id)
		return Response{}, err
	}

	select {
	case resp := <-waiter:
		return resp, nil
	case <-ctx.Done():
		c.dropPending(id)
		return Response{}, ctx.Err()
	case <-c.closed:
		c.dropPending(id)
		if err, _ := c.readErr.Load().(error); err != nil && !errors.Is(err, io.EOF) {
			return Response{}, fmt.Errorf("worker: %w", err)
		}
		return Response{}, ErrConnClosed
	}
}

// Close shuts the connection. Pending Sends unblock with ErrConnClosed.
// Close is idempotent and safe to call from any goroutine.
func (c *Conn) Close() error {
	var err error
	c.closeOnce.Do(func() {
		err = c.conn.Close()
		close(c.closed)
	})
	return err
}

func (c *Conn) writeRequest(req Request) error {
	body, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("worker: marshal request: %w", err)
	}
	c.writeM.Lock()
	defer c.writeM.Unlock()
	if _, err := c.writer.Write(body); err != nil {
		return fmt.Errorf("worker: write: %w", err)
	}
	if err := c.writer.WriteByte('\n'); err != nil {
		return fmt.Errorf("worker: write: %w", err)
	}
	return c.writer.Flush()
}

func (c *Conn) readLoop() {
	defer c.Close()
	reader := bufio.NewReader(c.conn)
	for {
		line, err := reader.ReadBytes('\n')
		if len(line) > 0 {
			var resp Response
			if jerr := json.Unmarshal(line, &resp); jerr != nil {
				// Malformed line: stash the error so Send sees it on close.
				c.readErr.Store(fmt.Errorf("decode response: %w", jerr))
				return
			}
			c.deliver(resp)
		}
		if err != nil {
			if !errors.Is(err, io.EOF) {
				c.readErr.Store(err)
			}
			return
		}
	}
}

func (c *Conn) deliver(resp Response) {
	c.pendingM.Lock()
	waiter, ok := c.pending[resp.RequestID]
	if ok {
		delete(c.pending, resp.RequestID)
	}
	c.pendingM.Unlock()
	if ok {
		waiter <- resp
	}
	// Unmatched responses are dropped silently — they imply the caller
	// already gave up (ctx done) or the worker is misbehaving. Either
	// way, we don't have a place to surface them.
}

func (c *Conn) dropPending(id string) {
	c.pendingM.Lock()
	delete(c.pending, id)
	c.pendingM.Unlock()
}

func (c *Conn) isClosedLocked() bool {
	select {
	case <-c.closed:
		return true
	default:
		return false
	}
}
