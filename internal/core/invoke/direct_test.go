package invoke

import (
	"context"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"testing"

	"github.com/hex1n/sofarpc-cli/internal/boltclient"
	"github.com/hex1n/sofarpc-cli/internal/core/target"
	"github.com/hex1n/sofarpc-cli/internal/errcode"
	"github.com/hex1n/sofarpc-cli/internal/sofarpcwire"
)

func TestExecuteDirectIfPossible_UnsupportedTargetFallsThrough(t *testing.T) {
	exec, err := ExecuteDirectIfPossible(context.Background(), Plan{
		Service:    "com.foo.Svc",
		Method:     "doThing",
		ParamTypes: []string{"java.lang.String"},
		Args:       []any{"hello"},
		Target: target.Config{
			Mode:            target.ModeRegistry,
			RegistryAddress: "zookeeper://h:1",
		},
	}, "invoke")
	if err != nil {
		t.Fatalf("ExecuteDirectIfPossible: %v", err)
	}
	if exec.Handled {
		t.Fatal("registry target should fall through to caller")
	}
}

func TestExecuteDirectIfPossible_RoundTrip(t *testing.T) {
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

	exec, err := ExecuteDirectIfPossible(context.Background(), Plan{
		Service:       "com.example.demo.ExampleFacade",
		Method:        "query",
		ParamTypes:    []string{"com.example.demo.ExampleRequest"},
		Version:       "2.0",
		TargetAppName: "demo-app",
		Args: []any{
			map[string]any{
				"@type": "com.example.demo.ExampleRequest",
				"id":    int64(1001),
				"items": []any{int64(1001)},
			},
		},
		Target: target.Config{
			Mode:      target.ModeDirect,
			DirectURL: directURL,
		},
	}, "invoke")
	if err != nil {
		t.Fatalf("ExecuteDirectIfPossible: %v", err)
	}
	if !exec.Handled {
		t.Fatal("direct target should be handled")
	}
	if transport, _ := exec.Diagnostics["transport"].(string); transport != DirectTransportName {
		t.Fatalf("transport: got %q want %q", transport, DirectTransportName)
	}
	if dialTarget, _ := exec.Diagnostics["dialTarget"].(string); dialTarget == "" {
		t.Fatalf("expected dialTarget diagnostic")
	}
	if got, _ := exec.Diagnostics["targetServiceUniqueName"].(string); got != "com.example.demo.ExampleFacade:2.0" {
		t.Fatalf("targetServiceUniqueName: got %q", got)
	}
	if got, _ := exec.Diagnostics["requestClass"].(string); got != sofarpcwire.RequestClass {
		t.Fatalf("requestClass: got %q want %q", got, sofarpcwire.RequestClass)
	}
	result, ok := exec.Result.(map[string]any)
	if !ok {
		t.Fatalf("result type = %T", exec.Result)
	}
	if got := result["type"]; got != "com.example.demo.Result" {
		t.Fatalf("result.type: got %#v", got)
	}
}

func TestExecuteDirectIfPossible_InvalidTargetReturnsErrcode(t *testing.T) {
	_, err := ExecuteDirectIfPossible(context.Background(), Plan{
		Service:    "com.foo.Svc",
		Method:     "doThing",
		ParamTypes: []string{"java.lang.String"},
		Args:       []any{"hello"},
		Target: target.Config{
			Mode:      target.ModeDirect,
			DirectURL: "bolt://",
		},
	}, "invoke")
	if err == nil {
		t.Fatal("expected error")
	}
	ecerr, ok := err.(*errcode.Error)
	if !ok {
		t.Fatalf("error type = %T", err)
	}
	if ecerr.Code != errcode.TargetInvalid {
		t.Fatalf("code: got %q want %q", ecerr.Code, errcode.TargetInvalid)
	}
}

func TestExecuteRejectsInvalidPlanBeforeTransport(t *testing.T) {
	_, err := Execute(context.Background(), Plan{
		Service:    "com.foo.Svc",
		Method:     "doThing",
		ParamTypes: []string{"java.lang.String"},
		Args:       []any{"hello"},
		Target: target.Config{
			Mode:      target.ModeDirect,
			DirectURL: "bolt://127.0.0.1:1",
		},
	}, "invoke")
	if err == nil {
		t.Fatal("expected error")
	}
	ecerr, ok := err.(*errcode.Error)
	if !ok {
		t.Fatalf("error type = %T", err)
	}
	if ecerr.Code != errcode.PlanVersionUnsupported {
		t.Fatalf("code: got %q want %q", ecerr.Code, errcode.PlanVersionUnsupported)
	}
}

func TestMaxResponseBytesFromEnv(t *testing.T) {
	t.Setenv(envMaxResponseBytes, "4096")
	if got := maxResponseBytesFromEnv(); got != 4096 {
		t.Fatalf("maxResponseBytesFromEnv = %d, want 4096", got)
	}

	t.Setenv(envMaxResponseBytes, "invalid")
	if got := maxResponseBytesFromEnv(); got != boltclient.DefaultMaxResponseBytes {
		t.Fatalf("invalid env fallback = %d, want %d", got, boltclient.DefaultMaxResponseBytes)
	}
}

func TestClassifyDirectInvokeError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
		want errcode.Code
	}{
		{
			name: "timeout",
			err:  errors.New("context deadline exceeded"),
			want: errcode.InvocationTimeout,
		},
		{
			name: "connect failed",
			err:  errors.New("dial tcp 127.0.0.1:12200: connect: connection refused"),
			want: errcode.TargetConnectFailed,
		},
		{
			name: "serialize failed",
			err:  errors.New("hessian encode java.math.BigDecimal: invalid value"),
			want: errcode.SerializeFailed,
		},
		{
			name: "protocol failed",
			err:  errors.New("unexpected bolt command code"),
			want: errcode.ProtocolFailed,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := classifyDirectInvokeError("invoke", "bolt://127.0.0.1:12200", tt.err)
			if got.Code != tt.want {
				t.Fatalf("code = %s, want %s", got.Code, tt.want)
			}
			if got.Hint == nil || got.Hint.NextTool == "" {
				t.Fatalf("expected recovery hint, got %#v", got.Hint)
			}
		})
	}
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
