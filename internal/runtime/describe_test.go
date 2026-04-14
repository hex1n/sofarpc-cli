package runtime

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/hex1n/sofarpc-cli/internal/model"
)

func TestClasspathContentKeyEmpty(t *testing.T) {
	got, err := classpathContentKey(nil)
	if err != nil {
		t.Fatalf("classpathContentKey(nil) error = %v", err)
	}
	if got == "" {
		t.Fatal("expected a non-empty key for empty stub list")
	}
}

func TestClasspathContentKeyOrderIndependent(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "a.jar")
	second := filepath.Join(dir, "b.jar")
	if err := os.WriteFile(first, []byte("alpha"), 0o644); err != nil {
		t.Fatalf("write a.jar: %v", err)
	}
	if err := os.WriteFile(second, []byte("beta"), 0o644); err != nil {
		t.Fatalf("write b.jar: %v", err)
	}
	forward, err := classpathContentKey([]string{first, second})
	if err != nil {
		t.Fatalf("forward: %v", err)
	}
	reverse, err := classpathContentKey([]string{second, first})
	if err != nil {
		t.Fatalf("reverse: %v", err)
	}
	if forward != reverse {
		t.Fatalf("expected stable key regardless of input order, forward=%s reverse=%s", forward, reverse)
	}
}

func TestClasspathContentKeyAllowsMissingStubs(t *testing.T) {
	key, err := classpathContentKeyWithPolicy([]string{"/tmp/no-such-stub.jar"}, true)
	if err != nil {
		t.Fatalf("expected missing stub path to be accepted in fallback mode: %v", err)
	}
	if key == "" {
		t.Fatal("expected a non-empty key")
	}
}

func TestClasspathContentKeyRequiresExistingStubs(t *testing.T) {
	_, err := classpathContentKeyWithPolicy([]string{"/tmp/no-such-stub.jar"}, false)
	if err == nil {
		t.Fatal("expected missing stub path to fail in strict mode")
	}
}

func TestClasspathContentKeyChangesOnContentChange(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.jar")
	if err := os.WriteFile(path, []byte("alpha"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	first, err := classpathContentKey([]string{path})
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	if err := os.WriteFile(path, []byte("different"), 0o644); err != nil {
		t.Fatalf("rewrite: %v", err)
	}
	second, err := classpathContentKey([]string{path})
	if err != nil {
		t.Fatalf("second: %v", err)
	}
	if first == second {
		t.Fatal("expected key to change when stub content changes")
	}
}

func TestDescribeServiceUsesDaemonPathByDefault(t *testing.T) {
	manager := testManager(t)
	oldDescribe := describeViaDaemonRequest
	oldDescribeWorker := describeWorker
	defer func() {
		describeViaDaemonRequest = oldDescribe
		describeWorker = oldDescribeWorker
	}()
	spec := Spec{
		RuntimeJar: "/tmp/runtime.jar",
		JavaBin:    "java",
	}
	describeCallCount := 0
	requestedService := ""
	requestedRefresh := false
	describeViaDaemonRequest = func(_ context.Context, _ *Manager, _ Spec, service string, opts DescribeOptions) (model.ServiceSchema, error) {
		describeCallCount++
		requestedService = service
		requestedRefresh = opts.Refresh || opts.NoCache
		return model.ServiceSchema{
			Service: service,
			Methods: []model.MethodSchema{{Name: "foo"}},
		}, nil
	}
	describeWorker = func(_ *Manager, _ context.Context, _ Spec, service string) (model.ServiceSchema, error) {
		t.Fatalf("describe worker fallback should not run when daemon describe succeeds")
		return model.ServiceSchema{}, errors.New("should not run")
	}

	schema, err := manager.DescribeService(context.Background(), spec, "com.example.Service", DescribeOptions{Refresh: true})
	if err != nil {
		t.Fatalf("DescribeService() error = %v", err)
	}
	if schema.Service != "com.example.Service" {
		t.Fatalf("unexpected schema: %+v", schema)
	}
	if describeCallCount != 1 {
		t.Fatalf("expected daemon request once, got %d", describeCallCount)
	}
	if requestedService != "com.example.Service" {
		t.Fatalf("expected requested service com.example.Service, got %q", requestedService)
	}
	if !requestedRefresh {
		t.Fatalf("expected refresh/force flag to be forwarded")
	}
}

func TestDescribeServiceFallsBackToWorkerWhenDaemonPathFails(t *testing.T) {
	manager := testManager(t)
	oldDescribe := describeViaDaemonRequest
	oldDescribeWorker := describeWorker
	defer func() {
		describeViaDaemonRequest = oldDescribe
		describeWorker = oldDescribeWorker
	}()
	var fallbackCalled bool
	describeViaDaemonRequest = func(_ context.Context, _ *Manager, _ Spec, _ string, _ DescribeOptions) (model.ServiceSchema, error) {
		return model.ServiceSchema{}, errors.New("daemon unavailable")
	}
	describeWorker = func(_ *Manager, _ context.Context, _ Spec, service string) (model.ServiceSchema, error) {
		fallbackCalled = true
		return model.ServiceSchema{
			Service: service,
			Methods: []model.MethodSchema{{Name: "fallback"}},
		}, nil
	}
	spec := Spec{
		RuntimeJar: "/tmp/runtime.jar",
		JavaBin:    "java",
	}

	schema, err := manager.DescribeService(context.Background(), spec, "com.example.Service", DescribeOptions{})
	if err != nil {
		t.Fatalf("DescribeService() error = %v", err)
	}
	if !fallbackCalled {
		t.Fatal("expected fallback worker path to be used when daemon request fails")
	}
	if got := schema.Methods[0].Name; got != "fallback" {
		t.Fatalf("expected fallback result, got %q", got)
	}
}

func TestDescribeServiceRefreshesWithNoCache(t *testing.T) {
	manager := testManager(t)
	oldDescribe := describeViaDaemonRequest
	defer func() {
		describeViaDaemonRequest = oldDescribe
	}()

	describeCalls := 0
	describeViaDaemonRequest = func(_ context.Context, _ *Manager, _ Spec, _ string, opts DescribeOptions) (model.ServiceSchema, error) {
		describeCalls++
		if !opts.NoCache {
			t.Fatalf("expected NoCache flag to be forwarded")
		}
		return model.ServiceSchema{
			Service: "com.example.Service",
		}, nil
	}
	spec := Spec{
		RuntimeJar: "/tmp/runtime.jar",
		JavaBin:    "java",
	}
	_, err := manager.DescribeService(context.Background(), spec, "com.example.Service", DescribeOptions{NoCache: true})
	if err != nil {
		t.Fatalf("DescribeService() error = %v", err)
	}
	if describeCalls != 1 {
		t.Fatalf("expected one daemon request, got %d", describeCalls)
	}
}

func TestBuildClasspathOrderAndSeparator(t *testing.T) {
	got := buildClasspath("/tmp/worker.jar", []string{"/tmp/a.jar", "/tmp/b.jar"})
	sep := string(os.PathListSeparator)
	want := "/tmp/worker.jar" + sep + "/tmp/a.jar" + sep + "/tmp/b.jar"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestBuildClasspathAllowsEmptyRuntimeJar(t *testing.T) {
	got := buildClasspath("", []string{"/tmp/a.jar"})
	if got != "/tmp/a.jar" {
		t.Fatalf("buildClasspath() = %q, want %q", got, "/tmp/a.jar")
	}
}
