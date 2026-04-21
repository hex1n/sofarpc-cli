package sourcecontract

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/hex1n/sofarpc-cli/internal/facadesemantic"
)

func TestLoad_ResolvesFacadeAndDTOs(t *testing.T) {
	root := t.TempDir()
	writeJava(t, root, "src/main/java/com/foo/Svc.java", `
package com.foo;

import com.foo.model.OperationResult;
import com.foo.model.request.DailyHoldingsQueryRequest;
import com.foo.model.response.DailyHoldingResponse;
import java.util.List;

public interface Svc {
    OperationResult<DailyHoldingResponse> queryPortfolioAvailableCash(DailyHoldingsQueryRequest request);
    OperationResult<List<DailyHoldingResponse>> queryAll(List<Long> mpCodes);
}
`)
	writeJava(t, root, "src/main/java/com/foo/model/request/BaseRequest.java", `
package com.foo.model.request;

public class BaseRequest {
    private String traceId;
}
`)
	writeJava(t, root, "src/main/java/com/foo/model/request/DailyHoldingsQueryRequest.java", `
package com.foo.model.request;

import java.io.Serializable;
import java.util.List;

public class DailyHoldingsQueryRequest extends BaseRequest implements Serializable {
    private static final long serialVersionUID = 1L;
    private final String tradeDate;
    private final List<Long> mpCodeList;
    private final Long mpCode;

    public static class Builder {
        private String tradeDate;
        public Builder tradeDate(String tradeDate) { return this; }
    }
}
`)
	writeJava(t, root, "src/main/java/com/foo/model/OperationResult.java", `
package com.foo.model;

public class OperationResult<T> {
    private boolean success;
    private T data;
}
`)
	writeJava(t, root, "src/main/java/com/foo/model/response/DailyHoldingInfo.java", `
package com.foo.model.response;

import java.math.BigDecimal;

public class DailyHoldingInfo {
    private Long mpCode;
    private String fundCode;
    private BigDecimal holdingQuantity;
}
`)
	writeJava(t, root, "src/main/java/com/foo/model/response/DailyHoldingResponse.java", `
package com.foo.model.response;

import java.util.List;

public class DailyHoldingResponse {
    private List<DailyHoldingInfo> dailyHoldingInfos;
}
`)

	store, err := Load(root)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if store == nil {
		t.Fatal("Load returned nil store")
	}
	if store.Size() != 6 {
		t.Fatalf("Size: got %d want 6", store.Size())
	}
	if len(store.cache) != 0 {
		t.Fatalf("cache should start empty, got %d entries", len(store.cache))
	}

	svc, ok := store.Class("com.foo.Svc")
	if !ok {
		t.Fatal("Svc not found")
	}
	if len(store.cache) == 0 {
		t.Fatal("cache should populate on first Class lookup")
	}
	if svc.Kind != facadesemantic.KindInterface {
		t.Fatalf("Svc.Kind: got %q", svc.Kind)
	}
	if len(svc.Methods) != 2 {
		t.Fatalf("Svc.Methods: got %d want 2", len(svc.Methods))
	}
	if got := svc.Methods[0].ParamTypes; !reflect.DeepEqual(got, []string{"com.foo.model.request.DailyHoldingsQueryRequest"}) {
		t.Fatalf("Svc.Methods[0].ParamTypes: got %v", got)
	}
	if got := svc.Methods[0].ReturnType; got != "com.foo.model.OperationResult<com.foo.model.response.DailyHoldingResponse>" {
		t.Fatalf("Svc.Methods[0].ReturnType: got %q", got)
	}
	if got := svc.Methods[1].ParamTypes; !reflect.DeepEqual(got, []string{"java.util.List<java.lang.Long>"}) {
		t.Fatalf("Svc.Methods[1].ParamTypes: got %v", got)
	}

	req, ok := store.Class("com.foo.model.request.DailyHoldingsQueryRequest")
	if !ok {
		t.Fatal("DailyHoldingsQueryRequest not found")
	}
	if req.Superclass != "com.foo.model.request.BaseRequest" {
		t.Fatalf("Superclass: got %q", req.Superclass)
	}
	if !reflect.DeepEqual(req.Interfaces, []string{"java.io.Serializable"}) {
		t.Fatalf("Interfaces: got %v", req.Interfaces)
	}
	if names := fieldNames(req.Fields); !reflect.DeepEqual(names, []string{"tradeDate", "mpCodeList", "mpCode"}) {
		t.Fatalf("field names: got %v", names)
	}
	if got := req.Fields[1].JavaType; got != "java.util.List<java.lang.Long>" {
		t.Fatalf("mpCodeList type: got %q", got)
	}
	if got := req.Fields[2].JavaType; got != "java.lang.Long" {
		t.Fatalf("mpCode type: got %q", got)
	}

	resp, ok := store.Class("com.foo.model.response.DailyHoldingResponse")
	if !ok {
		t.Fatal("DailyHoldingResponse not found")
	}
	if len(resp.Fields) != 1 || resp.Fields[0].JavaType != "java.util.List<com.foo.model.response.DailyHoldingInfo>" {
		t.Fatalf("response fields: %+v", resp.Fields)
	}
}

func TestLoad_ParsesEnumConstants(t *testing.T) {
	root := t.TempDir()
	writeJava(t, root, "src/main/java/com/foo/Status.java", `
package com.foo;

public enum Status {
    OPEN,
    CLOSED;
}
`)

	store, err := Load(root)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if store == nil {
		t.Fatal("Load returned nil store")
	}
	status, ok := store.Class("com.foo.Status")
	if !ok {
		t.Fatal("Status not found")
	}
	if status.Kind != facadesemantic.KindEnum {
		t.Fatalf("Kind: got %q", status.Kind)
	}
	if !reflect.DeepEqual(status.EnumConstants, []string{"OPEN", "CLOSED"}) {
		t.Fatalf("EnumConstants: got %v", status.EnumConstants)
	}
}

func TestLoad_SkipsHiddenAndTestTrees(t *testing.T) {
	root := t.TempDir()
	writeJava(t, root, ".claude/worktrees/tmp/src/main/java/com/foo/Hidden.java", `
package com.foo;
public class Hidden {}
`)
	writeJava(t, root, "src/test/java/com/foo/TestOnly.java", `
package com.foo;
public class TestOnly {}
`)
	writeJava(t, root, "src/main/java/com/foo/Visible.java", `
package com.foo;
public class Visible {}
`)

	store, err := Load(root)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if store == nil {
		t.Fatal("Load returned nil store")
	}
	if store.Size() != 1 {
		t.Fatalf("Size: got %d want 1", store.Size())
	}
	if _, ok := store.Class("com.foo.Visible"); !ok {
		t.Fatal("Visible not found")
	}
}

func TestLoad_AnnotatedFieldDoesNotPanicMethodParser(t *testing.T) {
	root := t.TempDir()
	writeJava(t, root, "src/main/java/com/foo/Annotated.java", `
package com.foo;

public class Annotated {
    @JsonProperty("demo")
    private String name;
}
`)

	store, err := Load(root)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if store == nil {
		t.Fatal("Load returned nil store")
	}
	cls, ok := store.Class("com.foo.Annotated")
	if !ok {
		t.Fatal("Annotated not found")
	}
	if len(cls.Fields) != 1 || cls.Fields[0].Name != "name" {
		t.Fatalf("fields: %+v", cls.Fields)
	}
}

func TestLoad_BuildsIndexLazily(t *testing.T) {
	root := t.TempDir()
	writeJava(t, root, "src/main/java/com/foo/A.java", `
package com.foo;
public class A {
    private String name;
}
`)
	writeJava(t, root, "src/main/java/com/foo/B.java", `
package com.foo;
public class B {
    private Long id;
}
`)

	store, err := Load(root)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if store == nil {
		t.Fatal("Load returned nil store")
	}
	if got := len(store.index); got != 2 {
		t.Fatalf("index size: got %d want 2", got)
	}
	if got := len(store.cache); got != 0 {
		t.Fatalf("cache size: got %d want 0", got)
	}

	if _, ok := store.Class("com.foo.A"); !ok {
		t.Fatal("A not found")
	}
	if got := len(store.cache); got != 1 {
		t.Fatalf("cache size after one lookup: got %d want 1", got)
	}
	if _, ok := store.cache["com.foo.B"]; ok {
		t.Fatal("B should not be parsed before it is requested")
	}
}

func TestLoad_ResolvesInnerClassesLazily(t *testing.T) {
	root := t.TempDir()
	writeJava(t, root, "src/main/java/com/foo/Outer.java", `
package com.foo;

public class Outer {
    private Inner current;
    private Inner.Deep deep;

    public static class Inner {
        private Deep nested;

        public static class Deep {
            private String name;
        }
    }
}
`)

	store, err := Load(root)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if store == nil {
		t.Fatal("Load returned nil store")
	}
	if got := len(store.index); got != 1 {
		t.Fatalf("index size: got %d want 1", got)
	}
	if got := len(store.cache); got != 0 {
		t.Fatalf("cache size: got %d want 0", got)
	}

	deep, ok := store.Class("com.foo.Outer.Inner.Deep")
	if !ok {
		t.Fatal("Deep not found")
	}
	if deep.FQN != "com.foo.Outer.Inner.Deep" {
		t.Fatalf("Deep.FQN: got %q", deep.FQN)
	}
	if got := len(store.cache); got != 3 {
		t.Fatalf("cache size after inner lookup: got %d want 3", got)
	}

	outer, ok := store.Class("com.foo.Outer")
	if !ok {
		t.Fatal("Outer not found")
	}
	if got := outer.Fields[0].JavaType; got != "com.foo.Outer.Inner" {
		t.Fatalf("current type: got %q", got)
	}
	if got := outer.Fields[1].JavaType; got != "com.foo.Outer.Inner.Deep" {
		t.Fatalf("deep type: got %q", got)
	}

	inner, ok := store.Class("com.foo.Outer.Inner")
	if !ok {
		t.Fatal("Inner not found")
	}
	if got := inner.Fields[0].JavaType; got != "com.foo.Outer.Inner.Deep" {
		t.Fatalf("nested type: got %q", got)
	}
}

func TestLoad_DiagnosticsReportsIndexAndParseState(t *testing.T) {
	root := t.TempDir()
	writeJava(t, root, "src/main/java/com/foo/A.java", `
package com.foo;
public class A {
    private String name;
}
`)

	store, err := Load(root)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if store == nil {
		t.Fatal("Load returned nil store")
	}

	diag := store.Diagnostics()
	if diag.IndexedClasses != 1 || diag.IndexedFiles != 1 || diag.ParsedClasses != 0 {
		t.Fatalf("diagnostics before parse: %+v", diag)
	}

	if _, ok := store.Class("com.foo.A"); !ok {
		t.Fatal("A not found")
	}
	diag = store.Diagnostics()
	if diag.ParsedClasses != 1 {
		t.Fatalf("parsedClasses after lookup: %+v", diag)
	}
	if len(diag.ParseFailures) != 0 {
		t.Fatalf("unexpected parse failures: %+v", diag.ParseFailures)
	}
}

func TestLoad_DiagnosticsCaptureParseFailure(t *testing.T) {
	root := t.TempDir()
	writeJava(t, root, "src/main/java/com/foo/A.java", `
package com.foo;
public class A {
    private String name;
}
`)

	store, err := Load(root)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if store == nil {
		t.Fatal("Load returned nil store")
	}

	badPath := filepath.Join(root, "src/main/java/com/foo/Bad.java")
	if err := os.WriteFile(badPath, []byte("not java at all"), 0o644); err != nil {
		t.Fatalf("WriteFile(%s): %v", badPath, err)
	}
	store.index["com.foo.Bad"] = badPath

	if _, ok := store.Class("com.foo.Bad"); ok {
		t.Fatal("expected lookup to fail")
	}
	diag := store.Diagnostics()
	if len(diag.ParseFailures) != 1 {
		t.Fatalf("parseFailures: %+v", diag.ParseFailures)
	}
	if _, ok := diag.ParseFailures["com.foo.Bad"]; !ok {
		t.Fatalf("missing com.foo.Bad failure: %+v", diag.ParseFailures)
	}
}

func writeJava(t *testing.T, root, relative, body string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll(%s): %v", path, err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("WriteFile(%s): %v", path, err)
	}
}

func fieldNames(fields []facadesemantic.Field) []string {
	out := make([]string, 0, len(fields))
	for _, field := range fields {
		out = append(out, field.Name)
	}
	return out
}
