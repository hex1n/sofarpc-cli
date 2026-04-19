package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"testing"

	"github.com/hex1n/sofarpc-cli/internal/core/contract"
	"github.com/hex1n/sofarpc-cli/internal/core/target"
	"github.com/hex1n/sofarpc-cli/internal/errcode"
	"github.com/hex1n/sofarpc-cli/internal/facadesemantic"
	"github.com/hex1n/sofarpc-cli/internal/worker"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestInvoke_DryRunReturnsPlan(t *testing.T) {
	store := contract.NewInMemoryStore(
		facadesemantic.Class{
			FQN:  "com.foo.Svc",
			Kind: facadesemantic.KindInterface,
			Methods: []facadesemantic.Method{
				{Name: "doThing", ParamTypes: []string{"java.lang.String"}, ReturnType: "java.lang.String"},
			},
		},
	)
	out := callInvoke(t, Options{Facade: store}, map[string]any{
		"service":   "com.foo.Svc",
		"method":    "doThing",
		"directUrl": "bolt://host:12200",
		"dryRun":    true,
	})
	if !out.Ok {
		t.Fatalf("dry-run should succeed; got error=%+v", out.Error)
	}
	if out.Plan == nil {
		t.Fatal("plan should be populated")
	}
	if out.Plan.Target.Mode != target.ModeDirect {
		t.Fatalf("plan.target.mode: got %q", out.Plan.Target.Mode)
	}
	if out.Plan.ArgSource != "skeleton" {
		t.Fatalf("argSource: got %q", out.Plan.ArgSource)
	}
}

func TestInvoke_NonDryRunReturnsDaemonUnavailable(t *testing.T) {
	store := contract.NewInMemoryStore(
		facadesemantic.Class{
			FQN:  "com.foo.Svc",
			Kind: facadesemantic.KindInterface,
			Methods: []facadesemantic.Method{
				{Name: "doThing", ParamTypes: []string{"java.lang.String"}, ReturnType: "java.lang.String"},
			},
		},
	)
	out := callInvoke(t, Options{Facade: store}, map[string]any{
		"service":   "com.foo.Svc",
		"method":    "doThing",
		"directUrl": "bolt://host:12200",
	})
	if out.Error == nil || out.Error.Code != errcode.DaemonUnavailable {
		t.Fatalf("expected DaemonUnavailable, got %+v", out.Error)
	}
	// Even with the worker missing, the plan should still be attached so
	// agents can inspect what *would* have been sent.
	if out.Plan == nil {
		t.Fatal("plan should still be attached on DaemonUnavailable")
	}
}

func TestInvoke_FacadeNilSurfacesErrcode(t *testing.T) {
	out := callInvoke(t, Options{}, map[string]any{
		"service":   "com.foo.Svc",
		"method":    "doThing",
		"directUrl": "bolt://host:12200",
	})
	if out.Error == nil || out.Error.Code != errcode.FacadeNotConfigured {
		t.Fatalf("expected FacadeNotConfigured, got %+v", out.Error)
	}
}

func TestInvoke_TargetMissingSurfacesErrcode(t *testing.T) {
	store := contract.NewInMemoryStore(
		facadesemantic.Class{
			FQN: "com.foo.Svc", Kind: facadesemantic.KindInterface,
			Methods: []facadesemantic.Method{{Name: "doThing"}},
		},
	)
	out := callInvoke(t, Options{Facade: store}, map[string]any{
		"service": "com.foo.Svc",
		"method":  "doThing",
	})
	if out.Error == nil || out.Error.Code != errcode.TargetMissing {
		t.Fatalf("expected TargetMissing, got %+v", out.Error)
	}
}

func TestInvoke_UserArgsPassThrough(t *testing.T) {
	store := contract.NewInMemoryStore(
		facadesemantic.Class{
			FQN: "com.foo.Svc", Kind: facadesemantic.KindInterface,
			Methods: []facadesemantic.Method{
				{Name: "doThing", ParamTypes: []string{"java.lang.String"}},
			},
		},
	)
	out := callInvoke(t, Options{Facade: store}, map[string]any{
		"service":   "com.foo.Svc",
		"method":    "doThing",
		"directUrl": "bolt://h:1",
		"args":      []any{"hello"},
		"dryRun":    true,
	})
	if !out.Ok {
		t.Fatalf("dry-run should succeed; got error=%+v", out.Error)
	}
	if out.Plan.ArgSource != "user" {
		t.Fatalf("argSource: got %q want user", out.Plan.ArgSource)
	}
	if out.Plan.Args[0] != "hello" {
		t.Fatalf("user arg should pass through, got %v", out.Plan.Args[0])
	}
}

func TestInvoke_ArgsWrongTypeIsErrcode(t *testing.T) {
	store := contract.NewInMemoryStore(
		facadesemantic.Class{
			FQN: "com.foo.Svc", Kind: facadesemantic.KindInterface,
			Methods: []facadesemantic.Method{
				{Name: "doThing", ParamTypes: []string{"java.lang.String"}},
			},
		},
	)
	out := callInvoke(t, Options{Facade: store}, map[string]any{
		"service":   "com.foo.Svc",
		"method":    "doThing",
		"directUrl": "bolt://h:1",
		"args":      "not an array",
	})
	if out.Error == nil || out.Error.Code != errcode.ArgsInvalid {
		t.Fatalf("expected ArgsInvalid, got %+v", out.Error)
	}
	// The hint must preserve service/method so the agent can follow it
	// verbatim — an empty NextArgs would force it to remember the failed
	// call's inputs, defeating the "follow this" contract.
	if out.Error.Hint == nil || out.Error.Hint.NextTool != "sofarpc_describe" {
		t.Fatalf("hint should route to sofarpc_describe, got %+v", out.Error.Hint)
	}
	if svc, _ := out.Error.Hint.NextArgs["service"].(string); svc != "com.foo.Svc" {
		t.Fatalf("hint.NextArgs.service: got %q want com.foo.Svc", svc)
	}
	if m, _ := out.Error.Hint.NextArgs["method"].(string); m != "doThing" {
		t.Fatalf("hint.NextArgs.method: got %q want doThing", m)
	}
}

func TestInvoke_ArgsAtFileLoadsJSONArray(t *testing.T) {
	store := contract.NewInMemoryStore(
		facadesemantic.Class{
			FQN: "com.foo.Svc", Kind: facadesemantic.KindInterface,
			Methods: []facadesemantic.Method{
				{Name: "doThing", ParamTypes: []string{"java.lang.String"}},
			},
		},
	)
	dir := t.TempDir()
	path := filepath.Join(dir, "args.json")
	if err := os.WriteFile(path, []byte(`["from-file"]`), 0o644); err != nil {
		t.Fatalf("write args file: %v", err)
	}

	out := callInvoke(t, Options{Facade: store}, map[string]any{
		"service":   "com.foo.Svc",
		"method":    "doThing",
		"directUrl": "bolt://h:1",
		"args":      "@" + path,
		"dryRun":    true,
	})
	if !out.Ok {
		t.Fatalf("dry-run should succeed; got error=%+v", out.Error)
	}
	if out.Plan.ArgSource != "user" {
		t.Fatalf("argSource: got %q want user", out.Plan.ArgSource)
	}
	if got := out.Plan.Args[0]; got != "from-file" {
		t.Fatalf("args[0]: got %v want %q", got, "from-file")
	}
}

func TestInvoke_ArgsAtFileMissingIsErrcode(t *testing.T) {
	store := contract.NewInMemoryStore(
		facadesemantic.Class{
			FQN: "com.foo.Svc", Kind: facadesemantic.KindInterface,
			Methods: []facadesemantic.Method{
				{Name: "doThing", ParamTypes: []string{"java.lang.String"}},
			},
		},
	)
	out := callInvoke(t, Options{Facade: store}, map[string]any{
		"service":   "com.foo.Svc",
		"method":    "doThing",
		"directUrl": "bolt://h:1",
		"args":      "@/definitely/does/not/exist.json",
	})
	if out.Error == nil || out.Error.Code != errcode.ArgsInvalid {
		t.Fatalf("expected ArgsInvalid, got %+v", out.Error)
	}
}

func TestInvoke_ArgsAtFileNonArrayIsErrcode(t *testing.T) {
	store := contract.NewInMemoryStore(
		facadesemantic.Class{
			FQN: "com.foo.Svc", Kind: facadesemantic.KindInterface,
			Methods: []facadesemantic.Method{
				{Name: "doThing", ParamTypes: []string{"java.lang.String"}},
			},
		},
	)
	dir := t.TempDir()
	path := filepath.Join(dir, "args.json")
	if err := os.WriteFile(path, []byte(`{"not":"an array"}`), 0o644); err != nil {
		t.Fatalf("write args file: %v", err)
	}

	out := callInvoke(t, Options{Facade: store}, map[string]any{
		"service":   "com.foo.Svc",
		"method":    "doThing",
		"directUrl": "bolt://h:1",
		"args":      "@" + path,
	})
	if out.Error == nil || out.Error.Code != errcode.ArgsInvalid {
		t.Fatalf("expected ArgsInvalid, got %+v", out.Error)
	}
}

func TestInvoke_ArgsEmptyAtIsErrcode(t *testing.T) {
	store := contract.NewInMemoryStore(
		facadesemantic.Class{
			FQN: "com.foo.Svc", Kind: facadesemantic.KindInterface,
			Methods: []facadesemantic.Method{
				{Name: "doThing", ParamTypes: []string{"java.lang.String"}},
			},
		},
	)
	out := callInvoke(t, Options{Facade: store}, map[string]any{
		"service":   "com.foo.Svc",
		"method":    "doThing",
		"directUrl": "bolt://h:1",
		"args":      "@",
	})
	if out.Error == nil || out.Error.Code != errcode.ArgsInvalid {
		t.Fatalf("expected ArgsInvalid, got %+v", out.Error)
	}
}

func TestInvoke_WorkerRoundTripSetsOkAndResult(t *testing.T) {
	store := contract.NewInMemoryStore(
		facadesemantic.Class{
			FQN: "com.foo.Svc", Kind: facadesemantic.KindInterface,
			Methods: []facadesemantic.Method{
				{Name: "doThing", ParamTypes: []string{"java.lang.String"}, ReturnType: "java.lang.String"},
			},
		},
	)
	client, stop := fakeWorkerClient(t, func(req worker.Request) worker.Response {
		return worker.Response{
			Ok:          true,
			Result:      "hello:" + req.Method,
			Diagnostics: map[string]any{"latencyMs": float64(7)},
		}
	})
	defer stop()

	out := callInvoke(t, Options{Facade: store, Worker: client}, map[string]any{
		"service":   "com.foo.Svc",
		"method":    "doThing",
		"directUrl": "bolt://h:1",
		"args":      []any{"hello"},
	})
	if !out.Ok {
		t.Fatalf("expected Ok=true, got error=%+v", out.Error)
	}
	if out.Result != "hello:doThing" {
		t.Fatalf("result mismatch: got %v", out.Result)
	}
	if out.Diagnostics["latencyMs"] != float64(7) {
		t.Fatalf("diagnostics not forwarded: %+v", out.Diagnostics)
	}
}

func TestInvoke_WorkerWireErrorSurfacesCode(t *testing.T) {
	store := contract.NewInMemoryStore(
		facadesemantic.Class{
			FQN: "com.foo.Svc", Kind: facadesemantic.KindInterface,
			Methods: []facadesemantic.Method{
				{Name: "doThing", ParamTypes: []string{"java.lang.String"}, ReturnType: "java.lang.String"},
			},
		},
	)
	client, stop := fakeWorkerClient(t, func(req worker.Request) worker.Response {
		return worker.Response{
			Ok: false,
			Error: &worker.WireError{
				Code:    string(errcode.WorkerError),
				Message: "boom",
			},
		}
	})
	defer stop()

	out := callInvoke(t, Options{Facade: store, Worker: client}, map[string]any{
		"service":   "com.foo.Svc",
		"method":    "doThing",
		"directUrl": "bolt://h:1",
	})
	if out.Ok {
		t.Fatal("Ok should be false on wire error")
	}
	if out.Error == nil || out.Error.Code != errcode.WorkerError {
		t.Fatalf("expected WorkerError, got %+v", out.Error)
	}
	if out.Plan == nil {
		t.Fatal("plan should still be attached on worker error")
	}
}

// fakeWorkerClient wires a worker.Client to an in-process fake that
// responds to every request by calling handler. The handler owns
// RequestID echoing — tests shouldn't need to worry about correlation.
func fakeWorkerClient(t *testing.T, handler func(worker.Request) worker.Response) (*worker.Client, func()) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	serverDone := make(chan struct{})
	go func() {
		defer close(serverDone)
		defer listener.Close()
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		reader := bufio.NewReader(conn)
		writer := bufio.NewWriter(conn)
		for {
			line, err := reader.ReadBytes('\n')
			if len(line) > 0 {
				var req worker.Request
				if json.Unmarshal(line, &req) == nil {
					resp := handler(req)
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
	}()

	// Build a pool whose spawner dials the in-process listener instead
	// of exec'ing a real JVM.
	spec := worker.Spec{Jar: "fake.jar"}
	profile := worker.Profile{SOFARPCVersion: "5.12.0", RuntimeJarDigest: "test", JavaMajor: 17}
	pool := worker.NewPool(spec)
	pool.SetSpawnerForTesting(func(ctx context.Context, s worker.Spec) (*worker.Process, error) {
		tcp, err := net.Dial("tcp", listener.Addr().String())
		if err != nil {
			return nil, err
		}
		return worker.NewFakeProcessForTesting(s, tcp), nil
	})
	client := &worker.Client{Pool: pool, Profile: profile}

	return client, func() {
		client.Close(context.Background())
		listener.Close()
		<-serverDone
	}
}

func callInvoke(t *testing.T, opts Options, args map[string]any) InvokeOutput {
	t.Helper()
	server := New(opts)
	ctx := context.Background()
	client := connect(t, ctx, server)
	defer client.Close()

	result, err := client.CallTool(ctx, &sdkmcp.CallToolParams{
		Name:      "sofarpc_invoke",
		Arguments: args,
	})
	if err != nil {
		t.Fatalf("call invoke: %v", err)
	}
	body, err := json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatalf("marshal structured: %v", err)
	}
	var out InvokeOutput
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("unmarshal structured: %v", err)
	}
	return out
}
