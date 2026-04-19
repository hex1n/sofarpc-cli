package doctor

import (
	"context"
	"encoding/json"

	"github.com/hex1n/sofarpc-cli/internal/app/shared"
	"github.com/hex1n/sofarpc-cli/internal/model"
	"github.com/hex1n/sofarpc-cli/internal/runtime"
)

type Deps struct {
	ResolveInvocation       func(shared.InvocationInputs) (shared.ResolvedInvocation, error)
	PrepareDoctorInvocation func(context.Context, *shared.ResolvedInvocation, shared.InvocationInputs) (string, bool, []string, error)
	ResolveSpec             func(javaBin, runtimeJar, version string, stubPaths []string) (runtime.Spec, error)
	ScanStubWarnings        func([]string) []string
	ProbeTarget             func(model.TargetConfig) model.ProbeResult
	EnsureDaemon            func(context.Context, runtime.Spec) (model.DaemonMetadata, error)
	Invoke                  func(context.Context, model.DaemonMetadata, model.InvocationRequest) (model.InvocationResponse, error)
	SummarizeInvokeProbe    func(model.InvocationResponse, error) *model.InvokeProbe
	RandomID                func() string
	ContractSourceLabel     func(string) string
	WorkerClasspathMode     func([]string) string
}

func (d Deps) Execute(ctx context.Context, input shared.InvocationInputs) (model.DoctorReport, error) {
	resolved, err := d.ResolveInvocation(input)
	if err != nil {
		return model.DoctorReport{}, err
	}
	contractSource, contractCacheHit, contractNotes, err := d.PrepareDoctorInvocation(ctx, &resolved, input)
	if err != nil {
		return model.DoctorReport{}, err
	}
	spec, resolveErr := d.ResolveSpec(resolved.JavaBin, resolved.RuntimeJar, resolved.SofaRPCVersion, resolved.StubPaths)
	report := model.DoctorReport{
		ManifestPath:   resolved.ManifestPath,
		ManifestLoaded: resolved.ManifestFound,
		ActiveContext:  resolved.ActiveContext,
		Target:         resolved.Request.Target,
		StubWarnings:   d.ScanStubWarnings(resolved.StubPaths),
		Reachability:   d.ProbeTarget(resolved.Request.Target),
	}
	if resolveErr == nil {
		report.Runtime = model.RuntimeSnapshot{
			SofaRPCVersion:       spec.SofaRPCVersion,
			SofaRPCVersionSource: resolved.SofaRPCVersionSource,
			ContractSource:       d.ContractSourceLabel(contractSource),
			ContractCacheHit:     contractCacheHit,
			ContractNotes:        contractNotes,
			WorkerClasspath:      d.WorkerClasspathMode(resolved.StubPaths),
			RuntimeJar:           spec.RuntimeJar,
			JavaBin:              spec.JavaBin,
			JavaMajor:            spec.JavaMajor,
			DaemonKey:            spec.DaemonKey,
		}
		metadata, ensureErr := d.EnsureDaemon(ctx, spec)
		if ensureErr != nil {
			report.Daemon = model.DaemonSnapshot{Ready: false, Error: ensureErr.Error()}
		} else {
			report.Daemon = model.DaemonSnapshot{Ready: true, Metadata: &metadata}
			probeRequest := model.InvocationRequest{
				RequestID:   d.RandomID(),
				Service:     input.Service,
				Method:      input.Method,
				Args:        json.RawMessage("[]"),
				PayloadMode: model.PayloadRaw,
				Target:      resolved.Request.Target,
			}
			probeResponse, invokeErr := d.Invoke(ctx, metadata, probeRequest)
			report.InvokeProbe = d.SummarizeInvokeProbe(probeResponse, invokeErr)
		}
		return report, nil
	}
	report.Runtime = model.RuntimeSnapshot{
		SofaRPCVersion:       resolved.SofaRPCVersion,
		SofaRPCVersionSource: resolved.SofaRPCVersionSource,
		ContractSource:       d.ContractSourceLabel(contractSource),
		ContractCacheHit:     contractCacheHit,
		ContractNotes:        contractNotes,
		WorkerClasspath:      d.WorkerClasspathMode(resolved.StubPaths),
	}
	report.Daemon = model.DaemonSnapshot{Ready: false, Error: resolveErr.Error()}
	return report, nil
}
