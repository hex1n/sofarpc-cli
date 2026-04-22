package mcp

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"io"
	"net"
	"os"
	"path/filepath"
	"testing"

	"github.com/hex1n/sofarpc-cli/internal/boltclient"
	"github.com/hex1n/sofarpc-cli/internal/core/contract"
	coreinvoke "github.com/hex1n/sofarpc-cli/internal/core/invoke"
	"github.com/hex1n/sofarpc-cli/internal/core/target"
	"github.com/hex1n/sofarpc-cli/internal/errcode"
	"github.com/hex1n/sofarpc-cli/internal/javamodel"
	"github.com/hex1n/sofarpc-cli/internal/sofarpcwire"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestInvoke_DryRunReturnsPlan(t *testing.T) {
	store := contract.NewInMemoryStore(
		javamodel.Class{
			FQN:  "com.foo.Svc",
			Kind: javamodel.KindInterface,
			Methods: []javamodel.Method{
				{Name: "doThing", ParamTypes: []string{"java.lang.String"}, ReturnType: "java.lang.String"},
			},
		},
	)
	out := callInvoke(t, Options{Contract: store}, map[string]any{
		"service":       "com.foo.Svc",
		"method":        "doThing",
		"version":       "2.0",
		"targetAppName": "demo-app",
		"directUrl":     "bolt://host:12200",
		"dryRun":        true,
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
	if out.Plan.Version != "2.0" {
		t.Fatalf("version: got %q want 2.0", out.Plan.Version)
	}
	if out.Plan.TargetAppName != "demo-app" {
		t.Fatalf("targetAppName: got %q want demo-app", out.Plan.TargetAppName)
	}
}

func TestInvoke_UnsupportedTargetSurfacesInvocationRejected(t *testing.T) {
	store := contract.NewInMemoryStore(
		javamodel.Class{
			FQN:  "com.foo.Svc",
			Kind: javamodel.KindInterface,
			Methods: []javamodel.Method{
				{Name: "doThing", ParamTypes: []string{"java.lang.String"}, ReturnType: "java.lang.String"},
			},
		},
	)
	out := callInvoke(t, Options{Contract: store}, map[string]any{
		"service":         "com.foo.Svc",
		"method":          "doThing",
		"registryAddress": "zookeeper://host:2181",
	})
	if out.Error == nil || out.Error.Code != errcode.InvocationRejected {
		t.Fatalf("expected InvocationRejected, got %+v", out.Error)
	}
	if out.Plan == nil {
		t.Fatal("plan should still be attached on InvocationRejected")
	}
}

func TestInvoke_DirectTransportRoundTripSetsOkAndResult(t *testing.T) {
	store := contract.NewInMemoryStore(
		javamodel.Class{
			FQN:  "com.example.demo.ExampleFacade",
			Kind: javamodel.KindInterface,
			Methods: []javamodel.Method{
				{
					Name:       "query",
					ParamTypes: []string{"com.example.demo.ExampleRequest"},
					ReturnType: "com.example.demo.Result",
				},
			},
		},
	)
	appResponse := sofarpcwire.NormalizeArgs([]any{
		map[string]any{
			"@type":   "com.example.demo.Result",
			"success": true,
			"message": "ok",
		},
	})[0]
	responseBytes, err := sofarpcwire.BuildSuccessResponse(appResponse)
	if err != nil {
		t.Fatalf("BuildSuccessResponse: %v", err)
	}
	directURL, stop := fakeDirectServer(t, responseBytes)
	defer stop()

	out := callInvoke(t, Options{Contract: store}, map[string]any{
		"service":       "com.example.demo.ExampleFacade",
		"method":        "query",
		"version":       "2.0",
		"targetAppName": "demo-app",
		"directUrl":     directURL,
		"args": []any{
			map[string]any{
				"@type": "com.example.demo.ExampleRequest",
				"id":    float64(1001),
				"items": []any{float64(1001)},
			},
		},
	})
	if !out.Ok {
		t.Fatalf("expected Ok=true, got error=%+v diagnostics=%+v", out.Error, out.Diagnostics)
	}
	if transport, _ := out.Diagnostics["transport"].(string); transport != coreinvoke.DirectTransportName {
		t.Fatalf("transport: got %q want %q", transport, coreinvoke.DirectTransportName)
	}
	if got, _ := out.Diagnostics["targetServiceUniqueName"].(string); got != "com.example.demo.ExampleFacade:2.0" {
		t.Fatalf("targetServiceUniqueName: got %q", got)
	}
	result, ok := out.Result.(map[string]any)
	if !ok {
		t.Fatalf("result type = %T", out.Result)
	}
	if got := result["type"]; got != "com.example.demo.Result" {
		t.Fatalf("result.type: got %#v", got)
	}
	fields, ok := result["fields"].(map[string]any)
	if !ok {
		t.Fatalf("result.fields type = %T", result["fields"])
	}
	if got, ok := fields["success"].(bool); !ok || !got {
		t.Fatalf("result.fields.success = %#v", fields["success"])
	}
}

func TestInvoke_FacadeNilWithoutParamTypesSurfacesErrcode(t *testing.T) {
	out := callInvoke(t, Options{}, map[string]any{
		"service":   "com.foo.Svc",
		"method":    "doThing",
		"directUrl": "bolt://host:12200",
	})
	if out.Error == nil || out.Error.Code != errcode.FacadeNotConfigured {
		t.Fatalf("expected FacadeNotConfigured, got %+v", out.Error)
	}
}

// Trusted mode: no contract guidance, but the agent supplies a complete
// service/method/paramTypes/args tuple. Plan should build cleanly with
// contractSource=trusted.
func TestInvoke_FacadeNilWithTrustedArgsDryRunSucceeds(t *testing.T) {
	out := callInvoke(t, Options{}, map[string]any{
		"service":   "com.foo.Svc",
		"method":    "doThing",
		"directUrl": "bolt://host:12200",
		"types":     []any{"java.lang.String"},
		"args":      []any{"hello"},
		"dryRun":    true,
	})
	if !out.Ok {
		t.Fatalf("trusted dry-run should succeed; got error=%+v", out.Error)
	}
	if out.Plan == nil {
		t.Fatal("plan should be populated")
	}
	if out.Plan.ContractSource != "trusted" {
		t.Fatalf("contractSource: got %q want trusted", out.Plan.ContractSource)
	}
	if out.Plan.ArgSource != "user" {
		t.Fatalf("argSource: got %q want user", out.Plan.ArgSource)
	}
	if out.Plan.Args[0] != "hello" {
		t.Fatalf("user arg should pass through, got %v", out.Plan.Args[0])
	}
}

func TestInvoke_TargetMissingSurfacesErrcode(t *testing.T) {
	store := contract.NewInMemoryStore(
		javamodel.Class{
			FQN: "com.foo.Svc", Kind: javamodel.KindInterface,
			Methods: []javamodel.Method{{Name: "doThing"}},
		},
	)
	out := callInvoke(t, Options{Contract: store}, map[string]any{
		"service": "com.foo.Svc",
		"method":  "doThing",
	})
	if out.Error == nil || out.Error.Code != errcode.TargetMissing {
		t.Fatalf("expected TargetMissing, got %+v", out.Error)
	}
}

func TestInvoke_UserArgsPassThrough(t *testing.T) {
	store := contract.NewInMemoryStore(
		javamodel.Class{
			FQN: "com.foo.Svc", Kind: javamodel.KindInterface,
			Methods: []javamodel.Method{
				{Name: "doThing", ParamTypes: []string{"java.lang.String"}},
			},
		},
	)
	out := callInvoke(t, Options{Contract: store}, map[string]any{
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

func TestInvoke_DryRunNormalizesFacadeBackedArgs(t *testing.T) {
	store := contract.NewInMemoryStore(
		javamodel.Class{
			FQN:  "com.foo.Svc",
			Kind: javamodel.KindInterface,
			Methods: []javamodel.Method{
				{Name: "doThing", ParamTypes: []string{"com.foo.Req"}},
			},
		},
		javamodel.Class{
			FQN:  "com.foo.Req",
			Kind: javamodel.KindClass,
			Fields: []javamodel.Field{
				{Name: "amount", JavaType: "java.math.BigDecimal"},
			},
		},
	)

	out := callInvoke(t, Options{Contract: store}, map[string]any{
		"service":   "com.foo.Svc",
		"method":    "doThing",
		"directUrl": "bolt://h:1",
		"args": []any{
			map[string]any{"amount": 1000.5},
		},
		"dryRun": true,
	})
	if !out.Ok {
		t.Fatalf("dry-run should succeed; got error=%+v", out.Error)
	}
	arg, ok := out.Plan.Args[0].(map[string]any)
	if !ok {
		t.Fatalf("arg type: %T", out.Plan.Args[0])
	}
	if got := arg["@type"]; got != "com.foo.Req" {
		t.Fatalf("@type: got %#v", got)
	}
	amount, ok := arg["amount"].(map[string]any)
	if !ok {
		t.Fatalf("amount type: %T", arg["amount"])
	}
	if amount["@type"] != "java.math.BigDecimal" || amount["value"] != "1000.5" {
		t.Fatalf("amount: %#v", amount)
	}
}

func TestInvoke_ArgsWrongTypeIsErrcode(t *testing.T) {
	store := contract.NewInMemoryStore(
		javamodel.Class{
			FQN: "com.foo.Svc", Kind: javamodel.KindInterface,
			Methods: []javamodel.Method{
				{Name: "doThing", ParamTypes: []string{"java.lang.String"}},
			},
		},
	)
	out := callInvoke(t, Options{Contract: store}, map[string]any{
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
		javamodel.Class{
			FQN: "com.foo.Svc", Kind: javamodel.KindInterface,
			Methods: []javamodel.Method{
				{Name: "doThing", ParamTypes: []string{"java.lang.String"}},
			},
		},
	)
	dir := t.TempDir()
	path := filepath.Join(dir, "args.json")
	if err := os.WriteFile(path, []byte(`["from-file"]`), 0o644); err != nil {
		t.Fatalf("write args file: %v", err)
	}

	out := callInvoke(t, Options{Contract: store}, map[string]any{
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
		javamodel.Class{
			FQN: "com.foo.Svc", Kind: javamodel.KindInterface,
			Methods: []javamodel.Method{
				{Name: "doThing", ParamTypes: []string{"java.lang.String"}},
			},
		},
	)
	out := callInvoke(t, Options{Contract: store}, map[string]any{
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
		javamodel.Class{
			FQN: "com.foo.Svc", Kind: javamodel.KindInterface,
			Methods: []javamodel.Method{
				{Name: "doThing", ParamTypes: []string{"java.lang.String"}},
			},
		},
	)
	dir := t.TempDir()
	path := filepath.Join(dir, "args.json")
	if err := os.WriteFile(path, []byte(`{"not":"an array"}`), 0o644); err != nil {
		t.Fatalf("write args file: %v", err)
	}

	out := callInvoke(t, Options{Contract: store}, map[string]any{
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
		javamodel.Class{
			FQN: "com.foo.Svc", Kind: javamodel.KindInterface,
			Methods: []javamodel.Method{
				{Name: "doThing", ParamTypes: []string{"java.lang.String"}},
			},
		},
	)
	out := callInvoke(t, Options{Contract: store}, map[string]any{
		"service":   "com.foo.Svc",
		"method":    "doThing",
		"directUrl": "bolt://h:1",
		"args":      "@",
	})
	if out.Error == nil || out.Error.Code != errcode.ArgsInvalid {
		t.Fatalf("expected ArgsInvalid, got %+v", out.Error)
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

func fakeDirectServer(t *testing.T, content []byte) (string, func()) {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer listener.Close()

		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()

		requestID, err := readBoltRequestID(conn)
		if err != nil {
			return
		}
		_ = writeBoltResponse(conn, requestID, content)
	}()

	return "bolt://" + listener.Addr().String(), func() {
		_ = listener.Close()
		<-done
	}
}

func readBoltRequestID(r io.Reader) (uint32, error) {
	fixed := make([]byte, 22)
	if _, err := io.ReadFull(r, fixed); err != nil {
		return 0, err
	}
	classLen := binary.BigEndian.Uint16(fixed[14:16])
	headerLen := binary.BigEndian.Uint16(fixed[16:18])
	contentLen := binary.BigEndian.Uint32(fixed[18:22])
	body := make([]byte, int(classLen)+int(headerLen)+int(contentLen))
	if _, err := io.ReadFull(r, body); err != nil {
		return 0, err
	}
	return binary.BigEndian.Uint32(fixed[5:9]), nil
}

func writeBoltResponse(w io.Writer, requestID uint32, content []byte) error {
	classBytes := []byte(sofarpcwire.ResponseClass)
	fixed := make([]byte, 20)
	fixed[0] = boltclient.ProtocolCodeV1
	fixed[1] = boltclient.ResponseType
	binary.BigEndian.PutUint16(fixed[2:4], boltclient.CmdCodeRPCResponse)
	fixed[4] = boltclient.CmdVersion
	binary.BigEndian.PutUint32(fixed[5:9], requestID)
	fixed[9] = boltclient.CodecHessian2
	binary.BigEndian.PutUint16(fixed[10:12], 0)
	binary.BigEndian.PutUint16(fixed[12:14], uint16(len(classBytes)))
	binary.BigEndian.PutUint16(fixed[14:16], 0)
	binary.BigEndian.PutUint32(fixed[16:20], uint32(len(content)))

	if _, err := w.Write(fixed); err != nil {
		return err
	}
	if _, err := w.Write(classBytes); err != nil {
		return err
	}
	_, err := w.Write(content)
	return err
}
