//go:build e2e

package e2e_test

import (
	"context"
	"encoding/binary"
	"io"
	"net"
	"testing"

	"github.com/hex1n/sofarpc-cli/internal/boltclient"
	coreinvoke "github.com/hex1n/sofarpc-cli/internal/core/invoke"
	"github.com/hex1n/sofarpc-cli/internal/core/target"
	"github.com/hex1n/sofarpc-cli/internal/sofarpcwire"
)

func TestDirectBoltHessian2Smoke(t *testing.T) {
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
	directURL, stop := fakeBoltServer(t, responseBytes)
	defer stop()

	outcome, err := coreinvoke.Execute(context.Background(), coreinvoke.Plan{
		SchemaVersion: coreinvoke.PlanSchemaVersion,
		Service:       "com.example.demo.ExampleFacade",
		Method:        "query",
		ParamTypes:    []string{"com.example.demo.ExampleRequest"},
		Version:       "2.0",
		TargetAppName: "demo-app",
		Args: []any{
			map[string]any{
				"@type": "com.example.demo.ExampleRequest",
				"id":    int64(1001),
			},
		},
		Target: target.Config{
			Mode:      target.ModeDirect,
			DirectURL: directURL,
		},
	}, "e2e")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if transport, _ := outcome.Diagnostics["transport"].(string); transport != coreinvoke.DirectTransportName {
		t.Fatalf("transport: got %q want %q", transport, coreinvoke.DirectTransportName)
	}
	if targetService, _ := outcome.Diagnostics["targetServiceUniqueName"].(string); targetService != "com.example.demo.ExampleFacade:2.0" {
		t.Fatalf("targetServiceUniqueName: got %q", targetService)
	}
	result, ok := outcome.Result.(map[string]any)
	if !ok {
		t.Fatalf("result type = %T", outcome.Result)
	}
	if got := result["type"]; got != "com.example.demo.Result" {
		t.Fatalf("result.type: got %#v", got)
	}
}

func fakeBoltServer(t *testing.T, content []byte) (string, func()) {
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
