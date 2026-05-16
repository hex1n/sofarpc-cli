package sourcecontract

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/hex1n/sofarpc-cli/internal/core/contract"
	"github.com/hex1n/sofarpc-cli/internal/javamodel"
)

func TestLoad_ResolvesFacadeAndDTOs(t *testing.T) {
	root := t.TempDir()
	writeJava(t, root, "src/main/java/com/foo/Svc.java", `
package com.foo;

import com.foo.model.Result;
import com.foo.model.request.ExampleRequest;
import com.foo.model.response.ExampleResponse;
import java.util.List;

public interface Svc {
    Result<ExampleResponse> query(ExampleRequest request);
    Result<List<ExampleResponse>> queryAll(List<Long> ids);
}
`)
	writeJava(t, root, "src/main/java/com/foo/model/request/BaseRequest.java", `
package com.foo.model.request;

public class BaseRequest {
    private String traceId;
}
`)
	writeJava(t, root, "src/main/java/com/foo/model/request/ExampleRequest.java", `
package com.foo.model.request;

import java.io.Serializable;
import java.util.List;

public class ExampleRequest extends BaseRequest implements Serializable {
    private static final long serialVersionUID = 1L;
    private final String date;
    private final List<Long> idList;
    private final Long id;

    public static class Builder {
        private String date;
        public Builder date(String date) { return this; }
    }
}
`)
	writeJava(t, root, "src/main/java/com/foo/model/Result.java", `
package com.foo.model;

public class Result<T> {
    private boolean success;
    private T data;
}
`)
	writeJava(t, root, "src/main/java/com/foo/model/response/ExampleItem.java", `
package com.foo.model.response;

import java.math.BigDecimal;

public class ExampleItem {
    private Long id;
    private String code;
    private BigDecimal quantity;
}
`)
	writeJava(t, root, "src/main/java/com/foo/model/response/ExampleResponse.java", `
package com.foo.model.response;

import java.util.List;

public class ExampleResponse {
    private List<ExampleItem> items;
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
	if svc.Kind != javamodel.KindInterface {
		t.Fatalf("Svc.Kind: got %q", svc.Kind)
	}
	if len(svc.Methods) != 2 {
		t.Fatalf("Svc.Methods: got %d want 2", len(svc.Methods))
	}
	if got := svc.Methods[0].ParamTypes; !reflect.DeepEqual(got, []string{"com.foo.model.request.ExampleRequest"}) {
		t.Fatalf("Svc.Methods[0].ParamTypes: got %v", got)
	}
	if got := svc.Methods[0].ReturnType; got != "com.foo.model.Result<com.foo.model.response.ExampleResponse>" {
		t.Fatalf("Svc.Methods[0].ReturnType: got %q", got)
	}
	if got := svc.Methods[1].ParamTypes; !reflect.DeepEqual(got, []string{"java.util.List<java.lang.Long>"}) {
		t.Fatalf("Svc.Methods[1].ParamTypes: got %v", got)
	}

	req, ok := store.Class("com.foo.model.request.ExampleRequest")
	if !ok {
		t.Fatal("ExampleRequest not found")
	}
	if req.Superclass != "com.foo.model.request.BaseRequest" {
		t.Fatalf("Superclass: got %q", req.Superclass)
	}
	if !reflect.DeepEqual(req.Interfaces, []string{"java.io.Serializable"}) {
		t.Fatalf("Interfaces: got %v", req.Interfaces)
	}
	if names := fieldNames(req.Fields); !reflect.DeepEqual(names, []string{"date", "idList", "id"}) {
		t.Fatalf("field names: got %v", names)
	}
	if got := req.Fields[1].JavaType; got != "java.util.List<java.lang.Long>" {
		t.Fatalf("idList type: got %q", got)
	}
	if got := req.Fields[2].JavaType; got != "java.lang.Long" {
		t.Fatalf("id type: got %q", got)
	}

	resp, ok := store.Class("com.foo.model.response.ExampleResponse")
	if !ok {
		t.Fatal("ExampleResponse not found")
	}
	if len(resp.Fields) != 1 || resp.Fields[0].JavaType != "java.util.List<com.foo.model.response.ExampleItem>" {
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
	if status.Kind != javamodel.KindEnum {
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

func TestLoad_AnnotationGroupsDoNotSplitFieldSegments(t *testing.T) {
	root := t.TempDir()
	writeJava(t, root, "src/main/java/com/foo/AnnotatedGroups.java", `
package com.foo;

import java.util.List;

public class AnnotatedGroups {
    @NotEmpty(message = "ids required", groups = {Query.class})
    private final List<Long> ids;

    @NotNull(message = "id required", groups = {Query.class})
    private final Long id;

    public interface Query {
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
	cls, ok := store.Class("com.foo.AnnotatedGroups")
	if !ok {
		t.Fatal("AnnotatedGroups not found")
	}
	if names := fieldNames(cls.Fields); !reflect.DeepEqual(names, []string{"ids", "id"}) {
		t.Fatalf("field names: got %v", names)
	}
	if got := cls.Fields[0].JavaType; got != "java.util.List<java.lang.Long>" {
		t.Fatalf("ids type: got %q", got)
	}
	if got := cls.Fields[1].JavaType; got != "java.lang.Long" {
		t.Fatalf("id type: got %q", got)
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

func TestLoad_ResolvesJDK8GenericBounds(t *testing.T) {
	root := t.TempDir()
	writeJava(t, root, "src/main/java/com/foo/Base.java", `
package com.foo;
public class Base {
    private String id;
}
`)
	writeJava(t, root, "src/main/java/com/foo/Holder.java", `
package com.foo;
public class Holder<T extends Base> {
    private T value;
    public T echo(T input) { return input; }
}
`)

	store, err := Load(root)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	holder, ok := store.Class("com.foo.Holder")
	if !ok {
		t.Fatal("Holder not found")
	}
	if got := holder.Fields[0].JavaType; got != "com.foo.Base" {
		t.Fatalf("generic bound field type: got %q", got)
	}
	if got := holder.Methods[0].ReturnType; got != "com.foo.Base" {
		t.Fatalf("generic bound return type: got %q", got)
	}
	if got := holder.Methods[0].ParamTypes; !reflect.DeepEqual(got, []string{"com.foo.Base"}) {
		t.Fatalf("generic bound param types: got %v", got)
	}
}

func TestLoad_ResolvesSelfReferentialGenericBoundsConservatively(t *testing.T) {
	root := t.TempDir()
	writeJava(t, root, "src/main/java/com/foo/Box.java", `
package com.foo;

public class Box<T extends Comparable<T>> {
    private T value;
}
`)

	store, err := Load(root)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	box, ok := store.Class("com.foo.Box")
	if !ok {
		t.Fatal("Box not found")
	}
	if got := box.Fields[0].JavaType; got != "java.lang.Comparable<java.lang.Object>" {
		t.Fatalf("self-referential bound field type: got %q", got)
	}
}

func TestLoad_SubstitutesGenericInterfaceArgumentsForInheritedFacadeMethods(t *testing.T) {
	root := t.TempDir()
	writeJava(t, root, "src/main/java/com/foo/BaseFacade.java", `
package com.foo;

public interface BaseFacade<T> {
    Result<T> query(T request);
}
`)
	writeJava(t, root, "src/main/java/com/foo/UserFacade.java", `
package com.foo;

public interface UserFacade extends BaseFacade<UserRequest> {
}
`)
	writeJava(t, root, "src/main/java/com/foo/Result.java", `
package com.foo;

public class Result<T> {
    private T data;
}
`)
	writeJava(t, root, "src/main/java/com/foo/UserRequest.java", `
package com.foo;

public class UserRequest {
    private Long id;
}
`)

	store, err := Load(root)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	res, err := contract.ResolveMethod(store, "com.foo.UserFacade", "query", []string{"com.foo.UserRequest"})
	if err != nil {
		t.Fatalf("ResolveMethod: %v", err)
	}
	if got := res.Method.ParamTypes; !reflect.DeepEqual(got, []string{"com.foo.UserRequest"}) {
		t.Fatalf("param types: got %v", got)
	}
	if got := res.Method.ReturnType; got != "com.foo.Result<com.foo.UserRequest>" {
		t.Fatalf("return type: got %q", got)
	}

	skeleton := contract.BuildSkeleton(res.Method.ParamTypes, store)
	if !strings.Contains(string(skeleton[0]), `"@type":"com.foo.UserRequest"`) {
		t.Fatalf("skeleton should target concrete request type: %s", skeleton[0])
	}
	if !strings.Contains(string(skeleton[0]), `"id":"0"`) {
		t.Fatalf("skeleton should include UserRequest fields: %s", skeleton[0])
	}
}

func TestLoad_ResolvesMethodLevelGenericBoundsWithAnnotatedParams(t *testing.T) {
	root := t.TempDir()
	writeJava(t, root, "src/main/java/com/foo/GenericFacade.java", `
package com.foo;

public interface GenericFacade {
    <T extends BaseRequest>
    Result<T> query(
        @Valid
        @NotNull(message = "request required")
        T request
    );
}
`)
	writeJava(t, root, "src/main/java/com/foo/BaseRequest.java", `
package com.foo;

public class BaseRequest {
    private Long id;
}
`)
	writeJava(t, root, "src/main/java/com/foo/Result.java", `
package com.foo;

public class Result<T> {
    private T data;
}
`)

	store, err := Load(root)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	cls, ok := store.Class("com.foo.GenericFacade")
	if !ok {
		t.Fatal("GenericFacade not found")
	}
	if len(cls.Methods) != 1 {
		t.Fatalf("methods: %+v", cls.Methods)
	}
	method := cls.Methods[0]
	if got := method.ParamTypes; !reflect.DeepEqual(got, []string{"com.foo.BaseRequest"}) {
		t.Fatalf("param types: got %v", got)
	}
	if got := method.ParamTypeTemplates; !reflect.DeepEqual(got, []string{"T"}) {
		t.Fatalf("param templates: got %v", got)
	}
	if got := method.ReturnType; got != "com.foo.Result<com.foo.BaseRequest>" {
		t.Fatalf("return type: got %q", got)
	}
	if got := method.ReturnTypeTemplate; got != "com.foo.Result<T>" {
		t.Fatalf("return template: got %q", got)
	}

	res, err := contract.ResolveMethod(store, "com.foo.GenericFacade", "query", nil)
	if err != nil {
		t.Fatalf("ResolveMethod: %v", err)
	}
	skeleton := contract.BuildSkeleton(res.Method.ParamTypes, store)
	if !strings.Contains(string(skeleton[0]), `"@type":"com.foo.BaseRequest"`) {
		t.Fatalf("skeleton should target generic bound: %s", skeleton[0])
	}
	if !strings.Contains(string(skeleton[0]), `"id":"0"`) {
		t.Fatalf("skeleton should include BaseRequest fields: %s", skeleton[0])
	}
}

func TestLoad_ResolvesCommonImplicitJavaLangTypes(t *testing.T) {
	root := t.TempDir()
	writeJava(t, root, "src/main/java/com/foo/CommonLang.java", `
package com.foo;

public class CommonLang<T extends CharSequence> implements Runnable, Cloneable, AutoCloseable {
    private StringBuilder builder;
    private T value;
    private Thread thread;
    private StackTraceElement frame;

    public void run() {}
    public void close() {}
}
`)

	store, err := Load(root)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	cls, ok := store.Class("com.foo.CommonLang")
	if !ok {
		t.Fatal("CommonLang not found")
	}
	if got := cls.Interfaces; !reflect.DeepEqual(got, []string{"java.lang.Runnable", "java.lang.Cloneable", "java.lang.AutoCloseable"}) {
		t.Fatalf("interfaces: got %v", got)
	}
	wantFields := []javamodel.Field{
		{Name: "builder", JavaType: "java.lang.StringBuilder"},
		{Name: "value", JavaType: "java.lang.CharSequence", TypeTemplate: "T"},
		{Name: "thread", JavaType: "java.lang.Thread"},
		{Name: "frame", JavaType: "java.lang.StackTraceElement"},
	}
	if !reflect.DeepEqual(cls.Fields, wantFields) {
		t.Fatalf("fields:\n got: %+v\nwant: %+v", cls.Fields, wantFields)
	}
}

func TestLoad_ResolvesCurrentPackageTypesFromProjectIndex(t *testing.T) {
	root := t.TempDir()
	writeJava(t, root, "src/main/java/com/foo/Request.java", `
package com.foo;
public class Request {}
`)
	writeJava(t, root, "src/main/java/com/foo/Svc.java", `
package com.foo;
public class Svc {
    private Request request;
}
`)

	store, err := Load(root)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	cls, ok := store.Class("com.foo.Svc")
	if !ok {
		t.Fatal("Svc not found")
	}
	if got := cls.Fields[0].JavaType; got != "com.foo.Request" {
		t.Fatalf("current-package field type: got %q", got)
	}
}

func TestLoad_ResolvesWildcardImportsAgainstKnownSymbols(t *testing.T) {
	root := t.TempDir()
	writeJava(t, root, "src/main/java/com/other/Other.java", `
package com.other;
public class Other {}
`)
	writeJava(t, root, "src/main/java/com/foo/UsesWildcards.java", `
package com.foo;

import com.other.*;
import java.util.*;

public class UsesWildcards {
    private List<String> names;
    private Map.Entry<String, String> entry;
    private Other other;
}
`)

	store, err := Load(root)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	cls, ok := store.Class("com.foo.UsesWildcards")
	if !ok {
		t.Fatal("UsesWildcards not found")
	}
	wantFields := []javamodel.Field{
		{Name: "names", JavaType: "java.util.List<java.lang.String>"},
		{Name: "entry", JavaType: "java.util.Map.Entry<java.lang.String, java.lang.String>"},
		{Name: "other", JavaType: "com.other.Other"},
	}
	if !reflect.DeepEqual(cls.Fields, wantFields) {
		t.Fatalf("fields:\n got: %+v\nwant: %+v", cls.Fields, wantFields)
	}
}

func TestLoad_LeavesUnknownWildcardTypesUnqualified(t *testing.T) {
	root := t.TempDir()
	writeJava(t, root, "src/main/java/com/foo/UsesMissingWildcard.java", `
package com.foo;

import com.external.*;

public class UsesMissingWildcard {
    private Missing value;
}
`)

	store, err := Load(root)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	cls, ok := store.Class("com.foo.UsesMissingWildcard")
	if !ok {
		t.Fatal("UsesMissingWildcard not found")
	}
	if got := cls.Fields[0].JavaType; got != "Missing" {
		t.Fatalf("unknown wildcard field type: got %q", got)
	}
	diag := store.Diagnostics()
	if diag.ResolutionIssueCount != 1 {
		t.Fatalf("resolution issue count: got %d want 1 (%+v)", diag.ResolutionIssueCount, diag.ResolutionIssues)
	}
	if issues := diag.ResolutionIssues["com.foo.UsesMissingWildcard"]; len(issues) != 1 || !strings.Contains(issues[0], "Missing: unresolved type") {
		t.Fatalf("resolution issues: %+v", diag.ResolutionIssues)
	}
}

func TestLoad_LeavesAmbiguousWildcardTypesUnqualified(t *testing.T) {
	root := t.TempDir()
	writeJava(t, root, "src/main/java/com/foo/UsesAmbiguousWildcard.java", `
package com.foo;

import java.sql.*;
import java.util.*;

public class UsesAmbiguousWildcard {
    private Date value;
}
`)

	store, err := Load(root)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	cls, ok := store.Class("com.foo.UsesAmbiguousWildcard")
	if !ok {
		t.Fatal("UsesAmbiguousWildcard not found")
	}
	if got := cls.Fields[0].JavaType; got != "Date" {
		t.Fatalf("ambiguous wildcard field type: got %q", got)
	}
	diag := store.Diagnostics()
	if diag.ResolutionIssueCount != 1 {
		t.Fatalf("resolution issue count: got %d want 1 (%+v)", diag.ResolutionIssueCount, diag.ResolutionIssues)
	}
	issues := diag.ResolutionIssues["com.foo.UsesAmbiguousWildcard"]
	if len(issues) != 1 || !strings.Contains(issues[0], "Date: ambiguous on-demand import") {
		t.Fatalf("resolution issues: %+v", diag.ResolutionIssues)
	}
	if !strings.Contains(issues[0], "java.sql.Date") || !strings.Contains(issues[0], "java.util.Date") {
		t.Fatalf("ambiguous candidates missing: %v", issues)
	}
}

func TestLoad_PrefersCurrentPackageTypesOverImplicitJavaLang(t *testing.T) {
	root := t.TempDir()
	writeJava(t, root, "src/main/java/com/foo/Thread.java", `
package com.foo;
public class Thread {}
`)
	writeJava(t, root, "src/main/java/com/foo/UsesThread.java", `
package com.foo;
public class UsesThread {
    private Thread value;
}
`)

	store, err := Load(root)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	cls, ok := store.Class("com.foo.UsesThread")
	if !ok {
		t.Fatal("UsesThread not found")
	}
	if got := cls.Fields[0].JavaType; got != "com.foo.Thread" {
		t.Fatalf("current-package shadow type: got %q", got)
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

func TestLoad_DiagnosticsReportsDuplicatesSkippedDirsAndHints(t *testing.T) {
	root := t.TempDir()
	writeJava(t, root, "module-a/src/main/java/com/foo/Dupe.java", `
package com.foo;
public class Dupe {}
`)
	writeJava(t, root, "module-b/src/main/java/com/foo/Dupe.java", `
package com.foo;
public class Dupe {}
`)
	writeJava(t, root, "module-b/src/main/java/com/foo/LombokDto.java", `
package com.foo;
@Data
public class LombokDto {
    private String name;
}
`)
	writeJava(t, root, "module-b/target/generated-sources/com/foo/Generated.java", `
package com.foo;
public class Generated {}
`)
	writeJava(t, root, "module-b/build/tmp/com/foo/Ignored.java", `
package com.foo;
public class Ignored {}
`)

	store, err := Load(root)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	diag := store.Diagnostics()
	if len(diag.DuplicateClasses["com.foo.Dupe"]) != 2 {
		t.Fatalf("duplicate diagnostics: %+v", diag.DuplicateClasses)
	}
	if diag.SkippedDirs["target"] == 0 || diag.SkippedDirs["build"] == 0 {
		t.Fatalf("skipped dirs: %+v", diag.SkippedDirs)
	}
	if diag.ModuleRoots != 2 {
		t.Fatalf("moduleRoots: got %d want 2", diag.ModuleRoots)
	}
	if !containsHint(diag.Hints, "duplicate Java FQNs") || !containsHint(diag.Hints, "lombok annotations") {
		t.Fatalf("hints: %+v", diag.Hints)
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

func fieldNames(fields []javamodel.Field) []string {
	out := make([]string, 0, len(fields))
	for _, field := range fields {
		out = append(out, field.Name)
	}
	return out
}

func containsHint(hints []string, needle string) bool {
	for _, hint := range hints {
		if strings.Contains(hint, needle) {
			return true
		}
	}
	return false
}
