package mcp

import (
	"encoding/binary"
	"io"
	"net"
	"strings"
	"sync"
	"testing"

	"github.com/hex1n/sofarpc-cli/internal/core/contract"
	"github.com/hex1n/sofarpc-cli/internal/core/target"
	"github.com/hex1n/sofarpc-cli/internal/javamodel"
	"github.com/hex1n/sofarpc-cli/internal/sofarpcwire"
)

// captureDirectServer mirrors fakeDirectServer but also retains the raw
// request body so tests can assert what actually went on the wire.
func captureDirectServer(t *testing.T, content []byte) (string, func() []byte, func()) {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	var mu sync.Mutex
	var request []byte
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer listener.Close()

		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()

		fixed := make([]byte, 22)
		if _, err := io.ReadFull(conn, fixed); err != nil {
			return
		}
		classLen := binary.BigEndian.Uint16(fixed[14:16])
		headerLen := binary.BigEndian.Uint16(fixed[16:18])
		contentLen := binary.BigEndian.Uint32(fixed[18:22])
		body := make([]byte, int(classLen)+int(headerLen)+int(contentLen))
		if _, err := io.ReadFull(conn, body); err != nil {
			return
		}
		mu.Lock()
		request = body
		mu.Unlock()
		_ = writeBoltResponse(conn, binary.BigEndian.Uint32(fixed[5:9]), content)
	}()

	captured := func() []byte {
		mu.Lock()
		defer mu.Unlock()
		return request
	}
	return "bolt://" + listener.Addr().String(), captured, func() {
		_ = listener.Close()
		<-done
	}
}

// Old archives freeze dot-form nested names in ParamTypes too, and those
// become SofaRequest.methodArgSigs verbatim — replay must rescue the
// signatures alongside the @type values.
func TestReplay_RewritesDotNestedParamSigsBeforeExecution(t *testing.T) {
	t.Setenv(envAllowInvoke, "true")
	t.Setenv(envAllowedServices, "")

	store := contract.NewInMemoryStore(
		javamodel.Class{
			FQN:        "com.example.UpsertReq.CustomTimeWindow",
			BinaryName: "com.example.UpsertReq$CustomTimeWindow",
			Kind:       javamodel.KindClass,
			Fields: []javamodel.Field{
				{Name: "beginDate", JavaType: "java.lang.String"},
			},
		},
	)

	plan := samplePlan()
	plan.ParamTypes = []string{"com.example.UpsertReq.CustomTimeWindow"}
	plan.Args = []any{map[string]any{
		"@type":     "com.example.UpsertReq.CustomTimeWindow",
		"beginDate": "20260701",
	}}

	appResponse := sofarpcwire.NormalizeArgs([]any{
		map[string]any{"@type": "com.example.demo.Result", "success": true},
	})[0]
	responseBytes, err := sofarpcwire.BuildSuccessResponse(appResponse)
	if err != nil {
		t.Fatalf("BuildSuccessResponse: %v", err)
	}
	directURL, captured, stop := captureDirectServer(t, responseBytes)
	defer stop()
	plan.Target.DirectURL = directURL

	out := callReplay(t, Options{
		Contract: store,
		TargetSources: target.Sources{
			Env: target.Config{DirectURL: directURL},
			ProjectPolicy: target.PolicyConfig{
				AllowedServices: []string{"com.foo.Svc"},
			},
		},
	}, map[string]any{
		"payload": plan,
	})
	if !out.Ok {
		t.Fatalf("replay failed: error=%+v diagnostics=%+v", out.Error, out.Diagnostics)
	}

	wire := string(captured())
	binary := "com.example.UpsertReq$CustomTimeWindow"
	if got := strings.Count(wire, binary); got < 2 {
		t.Errorf("wire should carry the binary name in both methodArgSigs and @type, found %d occurrence(s)", got)
	}
	if strings.Contains(wire, "UpsertReq.CustomTimeWindow") {
		t.Errorf("dot-form nested type name still on the wire")
	}
	if got := out.Plan.ParamTypes[0]; got != "com.example.UpsertReq.CustomTimeWindow" {
		t.Errorf("reported plan should keep the captured paramTypes spelling, got %v", got)
	}
}

// A plan captured before the nested-type-name fix carries dot-form @type
// values frozen in its Args. Real replay must rewrite them to JVM binary
// names when the contract store knows the class — while the reported Plan
// keeps the captured spelling untouched.
func TestReplay_RewritesDotNestedTypeNamesBeforeExecution(t *testing.T) {
	t.Setenv(envAllowInvoke, "true")
	t.Setenv(envAllowedServices, "")

	store := contract.NewInMemoryStore(
		javamodel.Class{
			FQN:  "com.example.UpsertReq",
			Kind: javamodel.KindClass,
			Fields: []javamodel.Field{
				{Name: "customTimeWindows", JavaType: "java.util.List<com.example.UpsertReq.CustomTimeWindow>"},
			},
		},
		javamodel.Class{
			FQN:        "com.example.UpsertReq.CustomTimeWindow",
			BinaryName: "com.example.UpsertReq$CustomTimeWindow",
			Kind:       javamodel.KindClass,
			Fields: []javamodel.Field{
				{Name: "beginDate", JavaType: "java.lang.String"},
			},
		},
	)

	plan := samplePlan()
	plan.ParamTypes = []string{"com.example.UpsertReq"}
	plan.Args = []any{map[string]any{
		"@type": "com.example.UpsertReq",
		"customTimeWindows": []any{map[string]any{
			"@type":     "com.example.UpsertReq.CustomTimeWindow",
			"beginDate": "20260701",
		}},
	}}

	appResponse := sofarpcwire.NormalizeArgs([]any{
		map[string]any{
			"@type":   "com.example.demo.Result",
			"success": true,
		},
	})[0]
	responseBytes, err := sofarpcwire.BuildSuccessResponse(appResponse)
	if err != nil {
		t.Fatalf("BuildSuccessResponse: %v", err)
	}
	directURL, captured, stop := captureDirectServer(t, responseBytes)
	defer stop()
	plan.Target.DirectURL = directURL

	out := callReplay(t, Options{
		Contract: store,
		TargetSources: target.Sources{
			Env: target.Config{DirectURL: directURL},
			ProjectPolicy: target.PolicyConfig{
				AllowedServices: []string{"com.foo.Svc"},
			},
		},
	}, map[string]any{
		"payload": plan,
	})
	if !out.Ok {
		t.Fatalf("replay failed: error=%+v diagnostics=%+v", out.Error, out.Diagnostics)
	}

	wire := string(captured())
	if wire == "" {
		t.Fatal("fake server captured no request bytes")
	}
	if !strings.Contains(wire, "com.example.UpsertReq$CustomTimeWindow") {
		t.Errorf("wire bytes missing binary nested type name: old archives are not rescued")
	}
	if strings.Contains(wire, "UpsertReq.CustomTimeWindow") {
		t.Errorf("dot-form nested type name still on the wire")
	}

	planArg, ok := out.Plan.Args[0].(map[string]any)
	if !ok {
		t.Fatalf("reported plan arg type = %T", out.Plan.Args[0])
	}
	windows, ok := planArg["customTimeWindows"].([]any)
	if !ok || len(windows) == 0 {
		t.Fatalf("reported plan customTimeWindows missing: %v", planArg)
	}
	window := windows[0].(map[string]any)
	if got := window["@type"]; got != "com.example.UpsertReq.CustomTimeWindow" {
		t.Errorf("reported plan should keep the captured @type spelling, got %v", got)
	}
}
