package session

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	sessionadapter "github.com/hex1n/sofarpc-cli/internal/adapters/session"
	"github.com/hex1n/sofarpc-cli/internal/config"
	"github.com/hex1n/sofarpc-cli/internal/contract"
	"github.com/hex1n/sofarpc-cli/internal/facadeconfig"
	"github.com/hex1n/sofarpc-cli/internal/model"
	"github.com/hex1n/sofarpc-cli/internal/runtime"
	"github.com/hex1n/sofarpc-cli/internal/targetmodel"
)

func TestOpenBuildsWorkspaceSessionFromResolvedProject(t *testing.T) {
	store := sessionadapter.NewMemoryStore()
	root := t.TempDir()
	service := Deps{
		Store: store,
		Clock: func() time.Time {
			return time.Date(2026, 4, 18, 12, 0, 0, 0, time.UTC)
		},
		NewID: func() string { return "ws_demo" },
		ResolveProjectRoot: func(cwd, project string) (string, bool, error) {
			if cwd != root || project != "" {
				t.Fatalf("unexpected resolve args cwd=%q project=%q", cwd, project)
			}
			return root, true, nil
		},
		ResolveManifestPath: func(cwd, explicit string) string {
			return filepath.Join(cwd, "sofarpc.manifest.json")
		},
		LoadManifest: func(path string) (targetmodel.Manifest, bool, error) {
			if path != filepath.Join(root, "sofarpc.manifest.json") {
				t.Fatalf("unexpected manifest path %q", path)
			}
			return targetmodel.Manifest{
				DefaultContext: "prod",
				SofaRPCVersion: "5.7.6",
			}, true, nil
		},
		LoadContextStore: func(paths config.Paths) (targetmodel.ContextStore, error) {
			return targetmodel.ContextStore{
				Active: "local",
				Contexts: map[string]targetmodel.Context{
					"local": {Name: "local"},
				},
			}, nil
		},
		InspectFacadeState: func(projectRoot string) facadeconfig.StatePaths {
			return facadeconfig.StatePaths{
				ProjectRoot: projectRoot,
				StateDir:    filepath.Join(projectRoot, ".sofarpc"),
				IndexDir:    filepath.Join(projectRoot, ".sofarpc", "index"),
			}
		},
		LoadFacadeConfig: func(projectRoot string, optional bool) (facadeconfig.Config, error) {
			return facadeconfig.Config{
				FacadeModules: []facadeconfig.FacadeModule{{}},
			}, nil
		},
		FileExists: func(path string) bool {
			return strings.HasSuffix(path, filepath.Join(".sofarpc", "index", "_index.json"))
		},
		Runtime:  noopRuntime{},
		Metadata: noopMetadata{},
	}

	session, err := service.Open(context.Background(), OpenRequest{
		Cwd:   root,
		Paths: config.Paths{},
	})
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	if session.ID != "ws_demo" {
		t.Fatalf("session.ID = %q", session.ID)
	}
	if session.ProjectRoot != root {
		t.Fatalf("session.ProjectRoot = %q, want %q", session.ProjectRoot, root)
	}
	if !session.ManifestLoaded {
		t.Fatal("expected manifest to be loaded")
	}
	if session.ActiveContext != "prod" {
		t.Fatalf("session.ActiveContext = %q, want %q", session.ActiveContext, "prod")
	}
	if !session.Capabilities.Runtime || !session.Capabilities.Metadata {
		t.Fatalf("expected runtime/metadata capabilities to be true: %+v", session.Capabilities)
	}
	if !session.Capabilities.FacadeConfig || !session.Capabilities.FacadeIndex || !session.Capabilities.LocalContract {
		t.Fatalf("expected facade capabilities to be true: %+v", session.Capabilities)
	}
	if got := session.CreatedAt; got != "2026-04-18T12:00:00Z" {
		t.Fatalf("session.CreatedAt = %q", got)
	}
	stored, ok, err := store.Get(context.Background(), session.ID)
	if err != nil {
		t.Fatalf("store.Get() error = %v", err)
	}
	if !ok {
		t.Fatal("expected stored session")
	}
	if stored.ProjectRoot != root {
		t.Fatalf("stored.ProjectRoot = %q, want %q", stored.ProjectRoot, root)
	}
}

func TestOpenFallsBackToCwdWhenProjectIsNotResolved(t *testing.T) {
	store := sessionadapter.NewMemoryStore()
	cwd := t.TempDir()
	service := Deps{
		Store: store,
		NewID: func() string { return "ws_fallback" },
		ResolveProjectRoot: func(cwdValue, project string) (string, bool, error) {
			return "", false, nil
		},
		ResolveManifestPath: func(base, explicit string) string {
			return filepath.Join(base, "sofarpc.manifest.json")
		},
		LoadManifest: func(path string) (targetmodel.Manifest, bool, error) {
			return targetmodel.Manifest{}, false, nil
		},
		LoadContextStore: func(paths config.Paths) (targetmodel.ContextStore, error) {
			return targetmodel.ContextStore{}, nil
		},
		InspectFacadeState: func(projectRoot string) facadeconfig.StatePaths {
			return facadeconfig.StatePaths{
				ProjectRoot: projectRoot,
				IndexDir:    filepath.Join(projectRoot, ".sofarpc", "index"),
			}
		},
		LoadFacadeConfig: func(projectRoot string, optional bool) (facadeconfig.Config, error) {
			return facadeconfig.Config{}, os.ErrNotExist
		},
		FileExists: func(path string) bool { return false },
	}

	session, err := service.Open(context.Background(), OpenRequest{
		Cwd: cwd,
	})
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	if session.ProjectRoot != cwd {
		t.Fatalf("session.ProjectRoot = %q, want %q", session.ProjectRoot, cwd)
	}
	if session.ManifestLoaded {
		t.Fatal("expected manifest to be absent")
	}
	if session.Capabilities.Runtime || session.Capabilities.Metadata || session.Capabilities.FacadeConfig {
		t.Fatalf("expected capabilities to remain false: %+v", session.Capabilities)
	}
	if len(session.Notes) < 2 {
		t.Fatalf("expected fallback notes, got %+v", session.Notes)
	}
	if session.Notes[0] != "project root fell back to current working directory" {
		t.Fatalf("unexpected first note %q", session.Notes[0])
	}
}

func TestListGetAndCloseDelegateToStore(t *testing.T) {
	store := sessionadapter.NewMemoryStore()
	service := Deps{Store: store}
	base := model.WorkspaceSession{ID: "ws_1", UpdatedAt: "2026-04-18T12:00:00Z"}
	if err := store.Save(context.Background(), base); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	got, ok, err := service.Get(context.Background(), "ws_1")
	if err != nil || !ok {
		t.Fatalf("Get() = (%+v, %t, %v)", got, ok, err)
	}
	if got.ID != "ws_1" {
		t.Fatalf("Get().ID = %q", got.ID)
	}
	list, err := service.List(context.Background())
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(list) != 1 || list[0].ID != "ws_1" {
		t.Fatalf("List() = %+v", list)
	}
	if err := service.Close(context.Background(), "ws_1"); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	_, ok, err = service.Get(context.Background(), "ws_1")
	if err != nil {
		t.Fatalf("Get(after close) error = %v", err)
	}
	if ok {
		t.Fatal("expected session to be removed")
	}
}

func TestRecordTargetAndPlanUpdateSessionState(t *testing.T) {
	store := sessionadapter.NewMemoryStore()
	service := Deps{
		Store: store,
		Clock: func() time.Time {
			return time.Date(2026, 4, 18, 12, 30, 0, 0, time.UTC)
		},
	}
	base := model.WorkspaceSession{
		ID:        "ws_1",
		CreatedAt: "2026-04-18T12:00:00Z",
		UpdatedAt: "2026-04-18T12:00:00Z",
	}
	if err := store.Save(context.Background(), base); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	targetSession, err := service.RecordTarget(context.Background(), RecordTargetRequest{
		ID:          "ws_1",
		Service:     "com.example.UserFacade",
		ContextName: "dev",
		Target: model.TargetConfig{
			Mode:      model.ModeDirect,
			DirectURL: "bolt://127.0.0.1:12200",
			UniqueID:  "user-facade",
		},
		Reachability: model.ProbeResult{
			Reachable: true,
		},
	})
	if err != nil {
		t.Fatalf("RecordTarget() error = %v", err)
	}
	if targetSession.LastTarget == nil {
		t.Fatal("expected last target snapshot")
	}
	if targetSession.LastTarget.Service != "com.example.UserFacade" {
		t.Fatalf("LastTarget.Service = %q", targetSession.LastTarget.Service)
	}
	if targetSession.LastTarget.ResolvedAt != "2026-04-18T12:30:00Z" {
		t.Fatalf("LastTarget.ResolvedAt = %q", targetSession.LastTarget.ResolvedAt)
	}

	planSession, err := service.RecordPlan(context.Background(), RecordPlanRequest{
		ID:      "ws_1",
		Service: "com.example.UserFacade",
		Method:  "getUser",
		Request: model.InvocationRequest{
			Service:     "com.example.UserFacade",
			Method:      "getUser",
			ParamTypes:  []string{"java.lang.Long"},
			Args:        json.RawMessage(`[123]`),
			PayloadMode: model.PayloadRaw,
		},
		Runtime: model.RuntimeSnapshot{
			ContractSource: "project-source",
			ContractNotes:  []string{"metadata-daemon"},
			SofaRPCVersion: "5.7.6",
		},
	})
	if err != nil {
		t.Fatalf("RecordPlan() error = %v", err)
	}
	if planSession.LastPlan == nil {
		t.Fatal("expected last plan snapshot")
	}
	if planSession.LastPlan.Method != "getUser" {
		t.Fatalf("LastPlan.Method = %q", planSession.LastPlan.Method)
	}
	if planSession.LastPlan.PlannedAt != "2026-04-18T12:30:00Z" {
		t.Fatalf("LastPlan.PlannedAt = %q", planSession.LastPlan.PlannedAt)
	}
	if planSession.UpdatedAt != "2026-04-18T12:30:00Z" {
		t.Fatalf("UpdatedAt = %q", planSession.UpdatedAt)
	}

	stored, ok, err := store.Get(context.Background(), "ws_1")
	if err != nil || !ok {
		t.Fatalf("store.Get() = (%+v, %t, %v)", stored, ok, err)
	}
	if stored.LastTarget == nil || stored.LastPlan == nil {
		t.Fatalf("stored session missing snapshots: %+v", stored)
	}
}

func TestRecordDescribeUpdatesSessionState(t *testing.T) {
	store := sessionadapter.NewMemoryStore()
	service := Deps{
		Store: store,
		Clock: func() time.Time {
			return time.Date(2026, 4, 18, 13, 0, 0, 0, time.UTC)
		},
	}
	base := model.WorkspaceSession{
		ID:        "ws_desc",
		CreatedAt: "2026-04-18T12:00:00Z",
		UpdatedAt: "2026-04-18T12:00:00Z",
	}
	if err := store.Save(context.Background(), base); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	session, err := service.RecordDescribe(context.Background(), RecordDescribeRequest{
		ID:      "ws_desc",
		Service: "com.example.UserFacade",
		Method:  "getUser",
		Overloads: []model.WorkspaceMethodOverload{
			{
				ParamTypes: []string{"java.lang.Long"},
				ReturnType: "com.example.UserDTO",
			},
		},
		Selected: &model.WorkspaceMethodOverload{
			ParamTypes: []string{"java.lang.Long"},
			ReturnType: "com.example.UserDTO",
		},
		Diagnostics: model.DiagnosticInfo{
			ContractSource: "project-source",
			ContractNotes:  []string{"metadata-daemon"},
		},
	})
	if err != nil {
		t.Fatalf("RecordDescribe() error = %v", err)
	}
	if session.LastDescribe == nil {
		t.Fatal("expected last describe snapshot")
	}
	if session.LastDescribe.Method != "getUser" {
		t.Fatalf("LastDescribe.Method = %q", session.LastDescribe.Method)
	}
	if session.LastDescribe.DescribedAt != "2026-04-18T13:00:00Z" {
		t.Fatalf("LastDescribe.DescribedAt = %q", session.LastDescribe.DescribedAt)
	}
	if session.LastDescribe.Selected == nil || len(session.LastDescribe.Overloads) != 1 {
		t.Fatalf("unexpected describe snapshot: %+v", session.LastDescribe)
	}
}

type noopRuntime struct{}

func (noopRuntime) ResolveSpec(javaBin, runtimeJar, version string, stubPaths []string) (runtime.Spec, error) {
	return runtime.Spec{}, nil
}

func (noopRuntime) EnsureDaemon(context.Context, runtime.Spec) (model.DaemonMetadata, error) {
	return model.DaemonMetadata{}, nil
}

func (noopRuntime) Invoke(context.Context, model.DaemonMetadata, model.InvocationRequest) (model.InvocationResponse, error) {
	return model.InvocationResponse{}, nil
}

func (noopRuntime) DescribeServiceLegacyFallback(context.Context, runtime.Spec, string, runtime.DescribeOptions) (model.ServiceSchema, error) {
	return model.ServiceSchema{}, nil
}

type noopMetadata struct{}

func (noopMetadata) ResolveServiceSchema(context.Context, string, string, bool) (model.ServiceSchema, string, bool, []string, error) {
	return model.ServiceSchema{}, "", false, nil, nil
}

func (noopMetadata) ResolveMethod(context.Context, string, string, string, []string, json.RawMessage, bool) (contract.ProjectMethod, string, bool, []string, error) {
	return contract.ProjectMethod{}, "", false, nil, nil
}
