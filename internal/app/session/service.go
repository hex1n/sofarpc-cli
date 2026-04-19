package session

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/hex1n/sofarpc-cli/internal/config"
	"github.com/hex1n/sofarpc-cli/internal/facadeconfig"
	"github.com/hex1n/sofarpc-cli/internal/model"
	"github.com/hex1n/sofarpc-cli/internal/ports"
	"github.com/hex1n/sofarpc-cli/internal/targetmodel"
)

type Deps struct {
	Store               ports.SessionStore
	Clock               func() time.Time
	NewID               func() string
	ResolveProjectRoot  func(cwd, project string) (string, bool, error)
	ResolveManifestPath func(cwd, explicit string) string
	LoadManifest        func(string) (targetmodel.Manifest, bool, error)
	LoadContextStore    func(config.Paths) (targetmodel.ContextStore, error)
	InspectFacadeState  func(string) facadeconfig.StatePaths
	LoadFacadeConfig    func(string, bool) (facadeconfig.Config, error)
	FileExists          func(string) bool
	Runtime             ports.RuntimeService
	Metadata            ports.MetadataService
}

type OpenRequest struct {
	Cwd          string
	Paths        config.Paths
	Project      string
	ManifestPath string
	ContextName  string
}

type RecordTargetRequest struct {
	ID           string
	Service      string
	ContextName  string
	Target       targetmodel.TargetConfig
	Reachability model.ProbeResult
}

type RecordPlanRequest struct {
	ID               string
	Service          string
	Method           string
	Request          model.InvocationRequest
	Spec             model.WorkspaceRuntimePlanSpec
	Runtime          model.RuntimeSnapshot
	WrappedSingleArg bool
}

type RecordDescribeRequest struct {
	ID          string
	Service     string
	Method      string
	Overloads   []model.WorkspaceMethodOverload
	Selected    *model.WorkspaceMethodOverload
	Diagnostics model.DiagnosticInfo
}

func (d Deps) Open(ctx context.Context, req OpenRequest) (model.WorkspaceSession, error) {
	if d.Store == nil {
		return model.WorkspaceSession{}, fmt.Errorf("session store is required")
	}
	projectRoot, resolved, err := d.ResolveProjectRoot(req.Cwd, req.Project)
	if err != nil {
		return model.WorkspaceSession{}, err
	}
	notes := make([]string, 0, 4)
	if !resolved {
		projectRoot, err = filepath.Abs(req.Cwd)
		if err != nil {
			return model.WorkspaceSession{}, err
		}
		notes = append(notes, "project root fell back to current working directory")
	}
	manifestPath := d.ResolveManifestPath(projectRoot, req.ManifestPath)
	manifest, manifestLoaded, err := d.LoadManifest(manifestPath)
	if err != nil {
		return model.WorkspaceSession{}, err
	}
	if !manifestLoaded {
		notes = append(notes, fmt.Sprintf("manifest not found at %s", manifestPath))
	}
	contextStore, err := d.LoadContextStore(req.Paths)
	if err != nil {
		return model.WorkspaceSession{}, err
	}
	activeContext := firstNonEmpty(req.ContextName, manifest.DefaultContext, contextStore.Active)
	if activeContext == "" {
		notes = append(notes, "no active context resolved")
	}
	state := d.InspectFacadeState(projectRoot)
	cfg, cfgErr := d.LoadFacadeConfig(projectRoot, true)
	facadeConfigured := cfgErr == nil && len(cfg.FacadeModules) > 0
	if configPrintableError(cfgErr) {
		notes = append(notes, fmt.Sprintf("facade config unavailable: %v", cfgErr))
	}
	facadeIndexPath := filepath.Join(state.IndexDir, "_index.json")
	now := d.now().UTC().Format(time.RFC3339)
	session := model.WorkspaceSession{
		ID:               d.newID(),
		ProjectRoot:      projectRoot,
		ManifestPath:     manifestPath,
		ManifestLoaded:   manifestLoaded,
		ActiveContext:    activeContext,
		DefaultContext:   manifest.DefaultContext,
		SofaRPCVersion:   manifest.SofaRPCVersion,
		FacadeConfigured: facadeConfigured,
		FacadeIndexPath:  facadeIndexPath,
		Capabilities: model.WorkspaceCapabilities{
			Manifest:      manifestLoaded,
			ContextStore:  len(contextStore.Contexts) > 0,
			LocalContract: facadeConfigured,
			FacadeConfig:  facadeConfigured,
			FacadeIndex:   d.fileExists(facadeIndexPath),
			Runtime:       d.Runtime != nil,
			Metadata:      d.Metadata != nil,
		},
		Notes:     notes,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := d.Store.Save(ctx, session); err != nil {
		return model.WorkspaceSession{}, err
	}
	return session, nil
}

func (d Deps) Get(ctx context.Context, id string) (model.WorkspaceSession, bool, error) {
	if d.Store == nil {
		return model.WorkspaceSession{}, false, fmt.Errorf("session store is required")
	}
	return d.Store.Get(ctx, id)
}

func (d Deps) List(ctx context.Context) ([]model.WorkspaceSession, error) {
	if d.Store == nil {
		return nil, fmt.Errorf("session store is required")
	}
	return d.Store.List(ctx)
}

func (d Deps) Close(ctx context.Context, id string) error {
	if d.Store == nil {
		return fmt.Errorf("session store is required")
	}
	return d.Store.Delete(ctx, id)
}

func (d Deps) RecordTarget(ctx context.Context, req RecordTargetRequest) (model.WorkspaceSession, error) {
	return d.update(ctx, req.ID, func(session *model.WorkspaceSession, now string) {
		session.LastTarget = &model.WorkspaceResolvedTarget{
			Service:      req.Service,
			ContextName:  req.ContextName,
			Target:       req.Target,
			Reachability: req.Reachability,
			ResolvedAt:   now,
		}
	})
}

func (d Deps) RecordPlan(ctx context.Context, req RecordPlanRequest) (model.WorkspaceSession, error) {
	return d.update(ctx, req.ID, func(session *model.WorkspaceSession, now string) {
		session.LastPlan = &model.WorkspaceInvocationPlan{
			Service:          req.Service,
			Method:           req.Method,
			Request:          cloneInvocationRequest(req.Request),
			Spec:             cloneRuntimePlanSpec(req.Spec),
			Runtime:          cloneRuntimeSnapshot(req.Runtime),
			WrappedSingleArg: req.WrappedSingleArg,
			PlannedAt:        now,
		}
	})
}

func (d Deps) RecordDescribe(ctx context.Context, req RecordDescribeRequest) (model.WorkspaceSession, error) {
	return d.update(ctx, req.ID, func(session *model.WorkspaceSession, now string) {
		session.LastDescribe = &model.WorkspaceMethodDescription{
			Service:     req.Service,
			Method:      req.Method,
			Overloads:   cloneMethodOverloads(req.Overloads),
			Selected:    cloneSelectedOverload(req.Selected),
			Diagnostics: cloneDiagnosticInfo(req.Diagnostics),
			DescribedAt: now,
		}
	})
}

func (d Deps) now() time.Time {
	if d.Clock != nil {
		return d.Clock()
	}
	return time.Now()
}

func (d Deps) newID() string {
	if d.NewID != nil {
		return d.NewID()
	}
	return fmt.Sprintf("ws_%d", d.now().UnixNano())
}

func (d Deps) fileExists(path string) bool {
	if d.FileExists != nil {
		return d.FileExists(path)
	}
	_, err := os.Stat(path)
	return err == nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func configPrintableError(err error) bool {
	return err != nil && !os.IsNotExist(err) && !strings.Contains(err.Error(), "no config found")
}

func (d Deps) update(ctx context.Context, id string, mutate func(*model.WorkspaceSession, string)) (model.WorkspaceSession, error) {
	if d.Store == nil {
		return model.WorkspaceSession{}, fmt.Errorf("session store is required")
	}
	session, ok, err := d.Store.Get(ctx, id)
	if err != nil {
		return model.WorkspaceSession{}, err
	}
	if !ok {
		return model.WorkspaceSession{}, fmt.Errorf("workspace session %q not found", id)
	}
	now := d.now().UTC().Format(time.RFC3339)
	mutate(&session, now)
	session.UpdatedAt = now
	if err := d.Store.Save(ctx, session); err != nil {
		return model.WorkspaceSession{}, err
	}
	return session, nil
}

func cloneInvocationRequest(request model.InvocationRequest) model.InvocationRequest {
	request.ParamTypes = append([]string{}, request.ParamTypes...)
	request.ParamTypeSignatures = append([]string{}, request.ParamTypeSignatures...)
	request.Args = append([]byte{}, request.Args...)
	return request
}

func cloneRuntimeSnapshot(snapshot model.RuntimeSnapshot) model.RuntimeSnapshot {
	snapshot.ContractNotes = append([]string{}, snapshot.ContractNotes...)
	return snapshot
}

func cloneRuntimePlanSpec(spec model.WorkspaceRuntimePlanSpec) model.WorkspaceRuntimePlanSpec {
	spec.StubPaths = append([]string{}, spec.StubPaths...)
	return spec
}

func cloneDiagnosticInfo(info model.DiagnosticInfo) model.DiagnosticInfo {
	info.ContractNotes = append([]string{}, info.ContractNotes...)
	return info
}

func cloneMethodOverloads(overloads []model.WorkspaceMethodOverload) []model.WorkspaceMethodOverload {
	cloned := make([]model.WorkspaceMethodOverload, 0, len(overloads))
	for _, overload := range overloads {
		item := overload
		item.ParamTypes = append([]string{}, item.ParamTypes...)
		item.ParamTypeSignatures = append([]string{}, item.ParamTypeSignatures...)
		cloned = append(cloned, item)
	}
	return cloned
}

func cloneSelectedOverload(selected *model.WorkspaceMethodOverload) *model.WorkspaceMethodOverload {
	if selected == nil {
		return nil
	}
	item := *selected
	item.ParamTypes = append([]string{}, item.ParamTypes...)
	item.ParamTypeSignatures = append([]string{}, item.ParamTypeSignatures...)
	return &item
}
