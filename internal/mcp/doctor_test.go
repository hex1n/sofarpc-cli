package mcp

import (
	"context"
	"encoding/json"
	"net"
	"strings"
	"testing"

	"github.com/hex1n/sofarpc-cli/internal/core/contract"
	"github.com/hex1n/sofarpc-cli/internal/core/target"
	"github.com/hex1n/sofarpc-cli/internal/worker"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestDoctor_UnresolvedTargetFails(t *testing.T) {
	out := callDoctor(t, Options{}, nil)
	if out.Ok {
		t.Fatal("doctor.Ok should be false when no target is configured")
	}
	target := findCheck(t, out, "target")
	if target.Ok {
		t.Fatal("target check should fail without env/input")
	}
	if target.NextStep == nil || target.NextStep.Tool != "sofarpc_target" {
		t.Fatalf("target check should point at sofarpc_target, got %+v", target.NextStep)
	}
}

func TestDoctor_ReachableTargetPasses(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer listener.Close()
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			_ = conn.Close()
		}
	}()

	opts := Options{
		TargetSources: target.Sources{
			Env: target.Config{
				DirectURL:        "bolt://" + listener.Addr().String(),
				ConnectTimeoutMS: 500,
			},
		},
	}
	out := callDoctor(t, opts, nil)

	targetCheck := findCheck(t, out, "target")
	if !targetCheck.Ok {
		t.Fatalf("target check should pass, got %+v", targetCheck)
	}
	// indexer and worker are placeholders, so the top-level Ok must still be false.
	if out.Ok {
		t.Fatal("overall Ok should be false while indexer/worker are not wired")
	}
}

func TestDoctor_IndexerAndWorkerAreReportedUnwired(t *testing.T) {
	out := callDoctor(t, Options{}, nil)
	indexer := findCheck(t, out, "indexer")
	if indexer.Ok {
		t.Fatal("indexer should fail when no facade store is attached")
	}
	// With no reindexer wired there's no actionable tool for the agent —
	// we suppress the nextStep to avoid pointing back at doctor and rely
	// on the detail line to carry the human remedy.
	if indexer.NextStep != nil {
		t.Fatalf("indexer failure without reindexer should omit nextStep, got %+v", indexer.NextStep)
	}
	workerCheck := findCheck(t, out, "worker")
	if workerCheck.Ok {
		t.Fatal("worker should be reported as not-configured when no client is attached")
	}
	if !strings.Contains(workerCheck.Detail, "not configured") {
		t.Fatalf("worker detail should mention 'not configured', got %q", workerCheck.Detail)
	}
	if workerCheck.NextStep != nil {
		t.Fatalf("worker failure should not self-loop via nextStep, got %+v", workerCheck.NextStep)
	}
}

func TestDoctor_IndexerFailureWithReindexerHintsRefresh(t *testing.T) {
	reindexer := ReindexerFunc(func(ctx context.Context) (contract.Store, error) {
		return nil, nil
	})
	out := callDoctor(t, Options{Reindexer: reindexer}, nil)
	indexer := findCheck(t, out, "indexer")
	if indexer.Ok {
		t.Fatal("indexer should still fail when no facade store is attached")
	}
	if indexer.NextStep == nil {
		t.Fatal("indexer failure with reindexer should carry a nextStep")
	}
	if indexer.NextStep.Tool != "sofarpc_describe" {
		t.Fatalf("nextStep should route to sofarpc_describe, got %q", indexer.NextStep.Tool)
	}
	if refresh, _ := indexer.NextStep.Args["refresh"].(bool); !refresh {
		t.Fatalf("nextStep args should carry refresh=true, got %+v", indexer.NextStep.Args)
	}
}

func TestDoctor_WiredWorkerThatPingsReportsReady(t *testing.T) {
	var sawPing bool
	client, stop := fakeWorkerClient(t, func(req worker.Request) worker.Response {
		if req.Action == worker.ActionPing {
			sawPing = true
			return worker.Response{Ok: true, Result: "pong"}
		}
		// shutdown (from pool close) and anything else — just ack.
		return worker.Response{Ok: true}
	})
	defer stop()

	out := callDoctor(t, Options{Worker: client}, nil)
	workerCheck := findCheck(t, out, "worker")
	if !workerCheck.Ok {
		t.Fatalf("worker check should pass, got %+v", workerCheck)
	}
	if workerCheck.Detail != "worker ready" {
		t.Fatalf("expected 'worker ready', got %q", workerCheck.Detail)
	}
	if !sawPing {
		t.Fatal("doctor should have sent a ping request")
	}
}

func TestDoctor_WiredWorkerThatErrorsSurfacesFailure(t *testing.T) {
	client, stop := fakeWorkerClient(t, func(req worker.Request) worker.Response {
		return worker.Response{
			Ok: false,
			Error: &worker.WireError{
				Code:    "runtime.worker-error",
				Message: "stuck in GC",
			},
		}
	})
	defer stop()

	out := callDoctor(t, Options{Worker: client}, nil)
	workerCheck := findCheck(t, out, "worker")
	if workerCheck.Ok {
		t.Fatal("worker check should fail when ping returns an error")
	}
	if !strings.Contains(workerCheck.Detail, "stuck in GC") {
		t.Fatalf("detail should surface wire error message, got %q", workerCheck.Detail)
	}
}

func TestDoctor_SummaryListsEachCheck(t *testing.T) {
	out := callDoctor(t, Options{}, nil)
	for _, name := range []string{"target", "indexer", "worker", "sessions"} {
		if !strings.Contains(out.Summary, name+"=") {
			t.Fatalf("summary %q missing %s entry", out.Summary, name)
		}
	}
}

func TestDoctor_SessionsReportsSizeAndCap(t *testing.T) {
	// Explicit store so we can seed it and know the expected load.
	store := NewSessionStoreWithLimits(0, 32)
	store.Create(Session{ProjectRoot: "/a"})
	store.Create(Session{ProjectRoot: "/b"})

	out := callDoctor(t, Options{Sessions: store}, nil)
	check := findCheck(t, out, "sessions")
	if !check.Ok {
		t.Fatalf("sessions check should be informational (Ok=true), got %+v", check)
	}
	if !strings.Contains(check.Detail, "2/32") {
		t.Fatalf("detail should carry size/cap (2/32), got %q", check.Detail)
	}
}

func TestDoctor_SessionsUnboundedCapReportsSoftly(t *testing.T) {
	store := NewSessionStoreWithLimits(0, 0)
	out := callDoctor(t, Options{Sessions: store}, nil)
	check := findCheck(t, out, "sessions")
	if !check.Ok {
		t.Fatalf("sessions check should still be Ok=true when cap is 0, got %+v", check)
	}
	if !strings.Contains(check.Detail, "unbounded") {
		t.Fatalf("detail should mention 'unbounded' when cap=0, got %q", check.Detail)
	}
}

func callDoctor(t *testing.T, opts Options, args map[string]any) DoctorOutput {
	t.Helper()
	server := New(opts)
	ctx := context.Background()
	client := connect(t, ctx, server)
	defer client.Close()

	if args == nil {
		args = map[string]any{}
	}
	result, err := client.CallTool(ctx, &sdkmcp.CallToolParams{
		Name:      "sofarpc_doctor",
		Arguments: args,
	})
	if err != nil {
		t.Fatalf("call doctor: %v", err)
	}
	body, err := json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatalf("marshal structured: %v", err)
	}
	var out DoctorOutput
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("unmarshal structured: %v", err)
	}
	return out
}

func findCheck(t *testing.T, out DoctorOutput, name string) DoctorCheck {
	t.Helper()
	for _, c := range out.Checks {
		if c.Name == name {
			return c
		}
	}
	t.Fatalf("check %q not found in %+v", name, out.Checks)
	return DoctorCheck{}
}
