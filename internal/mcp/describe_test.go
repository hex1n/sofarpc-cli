package mcp

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/hex1n/sofarpc-cli/internal/core/contract"
	"github.com/hex1n/sofarpc-cli/internal/errcode"
	"github.com/hex1n/sofarpc-cli/internal/facadesemantic"
	"github.com/hex1n/sofarpc-cli/internal/sourcecontract"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestDescribe_FacadeNotConfigured(t *testing.T) {
	out := callDescribe(t, Options{}, map[string]any{
		"service": "com.foo.Svc",
		"method":  "doThing",
	})
	if out.Error == nil {
		t.Fatal("error should be populated when facade is nil")
	}
	if out.Error.Code != errcode.FacadeNotConfigured {
		t.Fatalf("code: got %q want %q", out.Error.Code, errcode.FacadeNotConfigured)
	}
	if out.Error.Hint == nil || out.Error.Hint.NextTool != "sofarpc_doctor" {
		t.Fatalf("hint should point at sofarpc_doctor: %+v", out.Error.Hint)
	}
}

func TestDescribe_ReturnsOverloadsAndSkeleton(t *testing.T) {
	store := contract.NewInMemoryStore(
		facadesemantic.Class{
			FQN:  "com.foo.Svc",
			Kind: facadesemantic.KindInterface,
			Methods: []facadesemantic.Method{
				{Name: "doThing", ParamTypes: []string{"com.foo.Req"}, ReturnType: "com.foo.Resp"},
			},
		},
		facadesemantic.Class{
			FQN:  "com.foo.Req",
			Kind: facadesemantic.KindClass,
			Fields: []facadesemantic.Field{
				{Name: "id", JavaType: "java.lang.Long"},
			},
		},
	)
	opts := Options{Facade: store}
	out := callDescribe(t, opts, map[string]any{
		"service": "com.foo.Svc",
		"method":  "doThing",
	})
	if out.Error != nil {
		t.Fatalf("unexpected error: %+v", out.Error)
	}
	if len(out.Overloads) != 1 {
		t.Fatalf("overloads: got %d want 1", len(out.Overloads))
	}
	if out.Selected != 0 {
		t.Fatalf("selected: got %d want 0", out.Selected)
	}
	if len(out.Skeleton) != 1 {
		t.Fatalf("skeleton: got %d entries", len(out.Skeleton))
	}
	obj, ok := out.Skeleton[0].(map[string]any)
	if !ok {
		t.Fatalf("skeleton[0] should be an object, got %T: %v", out.Skeleton[0], out.Skeleton[0])
	}
	if obj["@type"] != "com.foo.Req" {
		t.Fatalf("skeleton @type wrong: %v", obj)
	}
	if out.Diagnostics.ContractSource != "contract-store" {
		t.Fatalf("contractSource: got %q want contract-store", out.Diagnostics.ContractSource)
	}
	if !out.Diagnostics.Contract.Attached {
		t.Fatal("contract.attached should be true")
	}
}

func TestDescribe_IncludesSourceDiagnostics(t *testing.T) {
	root := t.TempDir()
	writeDescribeJava(t, root, "src/main/java/com/foo/Svc.java", `
package com.foo;

public interface Svc {
    Resp doThing(Req request);
}
`)
	writeDescribeJava(t, root, "src/main/java/com/foo/Req.java", `
package com.foo;

public class Req {
    private String id;
}
`)
	writeDescribeJava(t, root, "src/main/java/com/foo/Resp.java", `
package com.foo;

public class Resp {
    private String name;
}
`)

	store, err := sourcecontract.Load(root)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if store == nil {
		t.Fatal("Load returned nil store")
	}

	out := callDescribe(t, Options{Facade: store}, map[string]any{
		"service": "com.foo.Svc",
		"method":  "doThing",
	})
	if out.Error != nil {
		t.Fatalf("unexpected error: %+v", out.Error)
	}
	if out.Diagnostics.ContractSource != "sourcecontract" {
		t.Fatalf("contractSource: got %q want sourcecontract", out.Diagnostics.ContractSource)
	}
	if !out.Diagnostics.Contract.Attached {
		t.Fatal("contract.attached should be true")
	}
	if out.Diagnostics.Contract.IndexedClasses != 3 {
		t.Fatalf("indexedClasses: got %d want 3", out.Diagnostics.Contract.IndexedClasses)
	}
	if out.Diagnostics.Contract.ParsedClasses < 2 {
		t.Fatalf("parsedClasses should reflect on-demand parsing, got %d", out.Diagnostics.Contract.ParsedClasses)
	}
}

func TestDescribe_AmbiguousReturnsErrcode(t *testing.T) {
	store := contract.NewInMemoryStore(facadesemantic.Class{
		FQN:  "com.foo.Svc",
		Kind: facadesemantic.KindInterface,
		Methods: []facadesemantic.Method{
			{Name: "doThing", ParamTypes: []string{"java.lang.String"}},
			{Name: "doThing", ParamTypes: []string{"java.lang.Integer"}},
		},
	})
	out := callDescribe(t, Options{Facade: store}, map[string]any{
		"service": "com.foo.Svc",
		"method":  "doThing",
	})
	if out.Error == nil || out.Error.Code != errcode.MethodAmbiguous {
		t.Fatalf("expected MethodAmbiguous, got %+v", out.Error)
	}
}

func TestDescribe_MissingServiceIsErrcode(t *testing.T) {
	store := contract.NewInMemoryStore()
	out := callDescribe(t, Options{Facade: store}, map[string]any{
		"method": "doThing",
	})
	if out.Error == nil || out.Error.Code != errcode.ServiceMissing {
		t.Fatalf("expected ServiceMissing, got %+v", out.Error)
	}
}

func callDescribe(t *testing.T, opts Options, args map[string]any) DescribeOutput {
	t.Helper()
	server := New(opts)
	ctx := context.Background()
	client := connect(t, ctx, server)
	defer client.Close()
	return invokeDescribeTool(t, client, args)
}

// invokeDescribeTool issues one sofarpc_describe call on an already-open
// client session. Tests that want to exercise multiple calls against
// the same server (e.g. refresh-then-reuse) share a client instead of
// rebuilding a new server on every call.
func invokeDescribeTool(t *testing.T, client *sdkmcp.ClientSession, args map[string]any) DescribeOutput {
	t.Helper()
	ctx := context.Background()
	result, err := client.CallTool(ctx, &sdkmcp.CallToolParams{
		Name:      "sofarpc_describe",
		Arguments: args,
	})
	if err != nil {
		t.Fatalf("call describe: %v", err)
	}
	body, err := json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatalf("marshal structured: %v", err)
	}
	var out DescribeOutput
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("unmarshal structured: %v", err)
	}
	return out
}

func writeDescribeJava(t *testing.T, root, relative, body string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll(%s): %v", path, err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("WriteFile(%s): %v", path, err)
	}
}
