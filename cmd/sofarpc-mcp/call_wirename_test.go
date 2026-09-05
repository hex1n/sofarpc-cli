package main

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/hex1n/sofarpc-cli/internal/boltclient"
	"github.com/hex1n/sofarpc-cli/internal/core/invoke"
	"github.com/hex1n/sofarpc-cli/internal/core/projectconfig"
	"github.com/hex1n/sofarpc-cli/internal/sofarpcwire"
)

// captureBoltServer accepts one BOLT request, retains its raw body so the
// test can assert what actually went on the wire, and answers with content.
func captureBoltServer(t *testing.T, content []byte) (string, func() []byte, func()) {
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

		classBytes := []byte(sofarpcwire.ResponseClass)
		out := make([]byte, 20)
		out[0] = boltclient.ProtocolCodeV1
		out[1] = boltclient.ResponseType
		binary.BigEndian.PutUint16(out[2:4], boltclient.CmdCodeRPCResponse)
		out[4] = boltclient.CmdVersion
		binary.BigEndian.PutUint32(out[5:9], binary.BigEndian.Uint32(fixed[5:9]))
		out[9] = boltclient.CodecHessian2
		binary.BigEndian.PutUint16(out[10:12], 0)
		binary.BigEndian.PutUint16(out[12:14], uint16(len(classBytes)))
		binary.BigEndian.PutUint16(out[14:16], 0)
		binary.BigEndian.PutUint32(out[16:20], uint32(len(content)))
		_, _ = conn.Write(out)
		_, _ = conn.Write(classBytes)
		_, _ = conn.Write(content)
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

func writeCallJavaSource(t *testing.T, root, relative, body string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
}

// `call -plan` replays a captured plan. Plans captured before nested type
// names were fixed carry source-canonical spellings that Hessian2 cannot
// resolve, so the CLI must rescue them before execution exactly like
// sofarpc_replay does — while leaving the caller's archive file untouched.
func TestRunCall_PlanFileRescuesDotNestedTypeNames(t *testing.T) {
	t.Setenv(invoke.EnvAllowInvoke, "true")

	root := t.TempDir()
	writeCallJavaSource(t, root, "src/main/java/com/foo/UpsertReq.java", `
package com.foo;
import java.util.List;
public class UpsertReq {
    private List<CustomWindow> windows;

    public static class CustomWindow {
        private String beginDate;
    }
}
`)
	writeCallJavaSource(t, root, "src/main/java/com/foo/UserFacade.java", `
package com.foo;
public interface UserFacade {
    String upsert(UpsertReq request);
}
`)

	appResponse := sofarpcwire.NormalizeArgs([]any{
		map[string]any{"@type": "com.foo.Result", "success": true},
	})[0]
	responseBytes, err := sofarpcwire.BuildSuccessResponse(appResponse)
	if err != nil {
		t.Fatalf("BuildSuccessResponse: %v", err)
	}
	directURL, captured, stop := captureBoltServer(t, responseBytes)
	defer stop()
	seedProjectConfig(t, root, projectconfig.KindLocal,
		`{"directUrl":"`+directURL+`","allowedServices":["com.foo.UserFacade"]}`)

	// A pre-fix archive: nested @type frozen in source-canonical dot form.
	plan := invoke.Plan{
		SchemaVersion: invoke.PlanSchemaVersion,
		Service:       "com.foo.UserFacade",
		Method:        "upsert",
		ParamTypes:    []string{"com.foo.UpsertReq"},
		Args: []any{map[string]any{
			"@type": "com.foo.UpsertReq",
			"windows": []any{map[string]any{
				"@type":     "com.foo.UpsertReq.CustomWindow",
				"beginDate": "20260701",
			}},
		}},
		ArgSource: "user",
	}
	plan.Target.Mode = "direct"
	plan.Target.DirectURL = directURL

	planBytes, err := json.MarshalIndent(plan, "", "  ")
	if err != nil {
		t.Fatalf("marshal plan: %v", err)
	}
	planPath := filepath.Join(root, "archive.json")
	if err := os.WriteFile(planPath, planBytes, 0o644); err != nil {
		t.Fatalf("write plan: %v", err)
	}

	var buf bytes.Buffer
	if err := runCall(&buf, []string{"-project", root, "-plan", planPath}); err != nil {
		t.Fatalf("replay failed: %v\noutput: %s", err, buf.String())
	}

	wire := string(captured())
	if wire == "" {
		t.Fatal("fake server captured no request bytes")
	}
	if !strings.Contains(wire, "com.foo.UpsertReq$CustomWindow") {
		t.Errorf("wire bytes missing binary nested type name: pre-fix archives are not rescued")
	}
	if strings.Contains(wire, "UpsertReq.CustomWindow") {
		t.Errorf("dot-form nested type name still on the wire")
	}

	// The caller's archive must not be rewritten in place.
	after, err := os.ReadFile(planPath)
	if err != nil {
		t.Fatalf("re-read plan: %v", err)
	}
	if !strings.Contains(string(after), "com.foo.UpsertReq.CustomWindow") {
		t.Errorf("archive file was mutated in place; it must keep the captured spelling")
	}

	// The reported plan mirrors sofarpc_replay: captured spelling preserved.
	out := decodeCallOutput(t, buf.Bytes())
	if out.Plan == nil {
		t.Fatal("replay output should carry the plan")
	}
	arg := out.Plan.Args[0].(map[string]any)
	windows := arg["windows"].([]any)
	window := windows[0].(map[string]any)
	if got := window["@type"]; got != "com.foo.UpsertReq.CustomWindow" {
		t.Errorf("reported plan should keep the captured @type, got %v", got)
	}
}

// A plan whose classes the store cannot resolve must replay exactly as
// captured: the rescue may never turn a working replay into a failure.
func TestRunCall_PlanFileWithoutContractStoreReplaysVerbatim(t *testing.T) {
	t.Setenv(invoke.EnvAllowInvoke, "true")

	root := t.TempDir() // no Java sources: no contract store
	appResponse := sofarpcwire.NormalizeArgs([]any{
		map[string]any{"@type": "com.foo.Result", "success": true},
	})[0]
	responseBytes, err := sofarpcwire.BuildSuccessResponse(appResponse)
	if err != nil {
		t.Fatalf("BuildSuccessResponse: %v", err)
	}
	directURL, captured, stop := captureBoltServer(t, responseBytes)
	defer stop()
	seedProjectConfig(t, root, projectconfig.KindLocal,
		`{"directUrl":"`+directURL+`","allowedServices":["com.foo.UserFacade"]}`)

	plan := invoke.Plan{
		SchemaVersion: invoke.PlanSchemaVersion,
		Service:       "com.foo.UserFacade",
		Method:        "upsert",
		ParamTypes:    []string{"com.foo.Unknown"},
		Args: []any{map[string]any{
			"@type": "com.foo.Unknown.Inner",
			"v":     "x",
		}},
		ArgSource: "user",
	}
	plan.Target.Mode = "direct"
	plan.Target.DirectURL = directURL

	planBytes, _ := json.MarshalIndent(plan, "", "  ")
	planPath := filepath.Join(root, "archive.json")
	if err := os.WriteFile(planPath, planBytes, 0o644); err != nil {
		t.Fatalf("write plan: %v", err)
	}

	var buf bytes.Buffer
	if err := runCall(&buf, []string{"-project", root, "-plan", planPath}); err != nil {
		t.Fatalf("replay should still succeed without a store: %v\noutput: %s", err, buf.String())
	}
	wire := string(captured())
	if !strings.Contains(wire, "com.foo.Unknown.Inner") {
		t.Errorf("unresolvable type name must reach the wire unchanged")
	}
}
