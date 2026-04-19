package worker

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"testing"
	"time"
)

// --- TestMain self-exec stub --------------------------------------------
//
// Setting FAKE_WORKER_MODE turns this test binary into a fake worker JVM
// when re-executed by Spawn. Each mode exercises a different code path
// (happy / never-ready / shutdown-honored / shutdown-ignored).

const envMode = "FAKE_WORKER_MODE"

func TestMain(m *testing.M) {
	mode := os.Getenv(envMode)
	if mode == "" {
		os.Exit(m.Run())
	}
	runFakeWorker(mode)
}

func runFakeWorker(mode string) {
	switch mode {
	case "ready":
		serveFakeWorker(true)
	case "noisy-ready":
		// Print log noise before the JSON line — Spawn must skip it.
		fmt.Println("INFO  Bootstrapping fake worker JVM…")
		fmt.Println("WARN  this line should be ignored by awaitReady")
		serveFakeWorker(true)
	case "never-ready":
		// Just hang without printing anything. time.Sleep (not select{})
		// so the Go runtime doesn't flag the process as deadlocked.
		time.Sleep(time.Hour)
	case "shutdown-ignored":
		// Print ready, then never honour the shutdown action — Stop
		// must escalate to SIGTERM/SIGKILL.
		serveFakeWorker(false)
	default:
		fmt.Fprintf(os.Stderr, "unknown FAKE_WORKER_MODE=%s\n", mode)
		os.Exit(2)
	}
}

func serveFakeWorker(honorShutdown bool) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		fmt.Fprintf(os.Stderr, "fake worker listen: %v\n", err)
		os.Exit(2)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	body, _ := json.Marshal(ReadyMessage{Ready: true, Port: port, PID: os.Getpid()})
	fmt.Println(string(body))
	// Important: flush by closing stdout once the handshake is sent —
	// the architecture says the worker flips to TCP mode here.
	os.Stdout.Close()

	conn, err := listener.Accept()
	if err != nil {
		os.Exit(0)
	}
	defer conn.Close()
	reader := bufio.NewReader(conn)
	writer := bufio.NewWriter(conn)
	for {
		line, err := reader.ReadBytes('\n')
		if len(line) > 0 {
			var req Request
			if json.Unmarshal(line, &req) == nil {
				switch req.Action {
				case ActionShutdown:
					if honorShutdown {
						resp, _ := json.Marshal(Response{RequestID: req.RequestID, Ok: true})
						writer.Write(resp)
						writer.WriteByte('\n')
						writer.Flush()
						return
					}
					// Otherwise: ignore, force the test to escalate.
				case ActionPing:
					resp, _ := json.Marshal(Response{RequestID: req.RequestID, Ok: true, Result: "pong"})
					writer.Write(resp)
					writer.WriteByte('\n')
					writer.Flush()
				}
			}
		}
		if err != nil {
			return
		}
	}
}

// fakeWorkerSpec returns a Spec that re-execs this test binary in the
// given fake-worker mode. We piggyback on the `java -jar X` arg shape
// by setting Java=os.Args[0] and Jar="-test.run=TestNoop" — the args
// are nonsense but exec.Cmd doesn't care about jar semantics, only about
// the binary on the other end.
func fakeWorkerSpec(t *testing.T, mode string) Spec {
	t.Helper()
	return Spec{
		Profile:      Profile{SOFARPCVersion: "5.12.0", RuntimeJarDigest: "test", JavaMajor: 17},
		Java:         os.Args[0],
		Jar:          "ignored", // becomes `-jar ignored`, the fake binary disregards
		ExtraArgs:    []string{"-test.run=^$"},
		Env:          []string{envMode + "=" + mode},
		ReadyTimeout: 5 * time.Second,
		StopGrace:    500 * time.Millisecond,
	}
}

// --- awaitReady (pure parsing) ------------------------------------------

func TestAwaitReady_FirstJSONLineWins(t *testing.T) {
	body, _ := json.Marshal(ReadyMessage{Ready: true, Port: 12345, PID: 99})
	r := bytes.NewBufferString(string(body) + "\n")
	got, err := awaitReady(context.Background(), r, 100*time.Millisecond)
	if err != nil {
		t.Fatalf("awaitReady: %v", err)
	}
	if got.Port != 12345 || got.PID != 99 || !got.Ready {
		t.Fatalf("unexpected message: %+v", got)
	}
}

func TestAwaitReady_SkipsNonJSONNoise(t *testing.T) {
	body, _ := json.Marshal(ReadyMessage{Ready: true, Port: 8081, PID: 1})
	r := bytes.NewBufferString("INFO booting…\nWARN slow disk\n" + string(body) + "\n")
	got, err := awaitReady(context.Background(), r, 100*time.Millisecond)
	if err != nil {
		t.Fatalf("awaitReady: %v", err)
	}
	if got.Port != 8081 {
		t.Fatalf("got port=%d want 8081", got.Port)
	}
}

func TestAwaitReady_EOFBeforeReady(t *testing.T) {
	r := bytes.NewBufferString("only log lines, no json\n")
	_, err := awaitReady(context.Background(), r, 100*time.Millisecond)
	if !errors.Is(err, io.EOF) {
		t.Fatalf("expected io.EOF, got %v", err)
	}
}

func TestAwaitReady_TimesOutWhenStdoutHangs(t *testing.T) {
	r, _ := io.Pipe() // never written to
	_, err := awaitReady(context.Background(), r, 50*time.Millisecond)
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("expected timeout error, got %v", err)
	}
}

func TestAwaitReady_RejectsZeroPort(t *testing.T) {
	body, _ := json.Marshal(ReadyMessage{Ready: true, Port: 0})
	r := bytes.NewBufferString(string(body) + "\n")
	_, err := awaitReady(context.Background(), r, 100*time.Millisecond)
	if err == nil || !strings.Contains(err.Error(), "invalid port") {
		t.Fatalf("expected invalid-port error, got %v", err)
	}
}

func TestAwaitReady_CancelUnblocks(t *testing.T) {
	r, _ := io.Pipe()
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()
	_, err := awaitReady(ctx, r, time.Second)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
}

// --- Spawn / Stop end-to-end against the self-exec fake worker ----------

func TestSpawn_HappyPath(t *testing.T) {
	if testing.Short() {
		t.Skip("self-exec spawn test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	proc, err := Spawn(ctx, fakeWorkerSpec(t, "ready"))
	if err != nil {
		t.Fatalf("spawn: %v", err)
	}
	defer proc.Stop(context.Background())

	if proc.Ready().Port == 0 {
		t.Fatal("ready message should carry a port")
	}
	if proc.Ready().PID == 0 {
		t.Fatal("ready message should carry a pid")
	}

	resp, err := proc.Conn().Send(ctx, Request{Action: ActionPing})
	if err != nil {
		t.Fatalf("ping: %v", err)
	}
	if !resp.Ok || resp.Result != "pong" {
		t.Fatalf("unexpected ping response: %+v", resp)
	}
}

func TestSpawn_NoisyStdoutStillResolves(t *testing.T) {
	if testing.Short() {
		t.Skip("self-exec spawn test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	proc, err := Spawn(ctx, fakeWorkerSpec(t, "noisy-ready"))
	if err != nil {
		t.Fatalf("spawn: %v", err)
	}
	defer proc.Stop(context.Background())
	if proc.Ready().Port == 0 {
		t.Fatal("ready message should carry a port even after noise")
	}
}

func TestSpawn_NeverReadyTimesOut(t *testing.T) {
	if testing.Short() {
		t.Skip("self-exec spawn test")
	}
	spec := fakeWorkerSpec(t, "never-ready")
	spec.ReadyTimeout = 200 * time.Millisecond
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := Spawn(ctx, spec)
	if err == nil {
		t.Fatal("expected timeout error")
	}
	if !strings.Contains(err.Error(), "ready handshake") {
		t.Fatalf("expected ready handshake error, got %v", err)
	}
}

func TestStop_EscalatesWhenShutdownIgnored(t *testing.T) {
	if testing.Short() {
		t.Skip("self-exec spawn test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	proc, err := Spawn(ctx, fakeWorkerSpec(t, "shutdown-ignored"))
	if err != nil {
		t.Fatalf("spawn: %v", err)
	}

	stopped := make(chan error, 1)
	go func() { stopped <- proc.Stop(context.Background()) }()

	select {
	case err := <-stopped:
		if err != nil {
			t.Fatalf("stop returned err: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Stop did not return; escalation likely broken")
	}

	// And the underlying process is really gone. ProcessState != nil
	// means cmd.Wait reaped it; Exited() may be false when we had to
	// escalate to SIGTERM/SIGKILL, which is still a legitimate exit.
	if proc.cmd.ProcessState == nil {
		t.Fatal("process should have been reaped after Stop")
	}
}

func TestSpawn_RejectsEmptyProfile(t *testing.T) {
	_, err := Spawn(context.Background(), Spec{Jar: "x"})
	if err == nil {
		t.Fatal("expected error for empty profile")
	}
}

func TestSpawn_RejectsMissingJar(t *testing.T) {
	_, err := Spawn(context.Background(), Spec{
		Profile: Profile{SOFARPCVersion: "5", RuntimeJarDigest: "x", JavaMajor: 17},
	})
	if err == nil {
		t.Fatal("expected error when jar is missing")
	}
}

