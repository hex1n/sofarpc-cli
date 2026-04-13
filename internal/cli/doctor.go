package cli

import (
	"context"
	"encoding/json"

	"github.com/hex1n/sofarpc-cli/internal/model"
	"github.com/hex1n/sofarpc-cli/internal/runtime"
)

func (a *App) runDoctor(args []string) error {
	flags := failFlagSet("doctor")
	var input invocationInputs
	flags.StringVar(&input.ManifestPath, "manifest", "", "manifest file path")
	flags.StringVar(&input.ContextName, "context", "", "context name")
	flags.StringVar(&input.DirectURL, "direct-url", "", "direct bolt target")
	flags.StringVar(&input.RegistryAddress, "registry-address", "", "registry address")
	flags.StringVar(&input.RegistryProtocol, "registry-protocol", "", "registry protocol")
	flags.StringVar(&input.Protocol, "protocol", "", "SOFARPC protocol")
	flags.StringVar(&input.Serialization, "serialization", "", "serialization")
	flags.StringVar(&input.UniqueID, "unique-id", "", "service uniqueId")
	flags.IntVar(&input.TimeoutMS, "timeout-ms", 0, "invoke timeout in milliseconds")
	flags.IntVar(&input.ConnectTimeoutMS, "connect-timeout-ms", 0, "connect timeout in milliseconds")
	flags.StringVar(&input.StubPathCSV, "stub-path", "", "comma-separated stub paths")
	flags.StringVar(&input.SofaRPCVersion, "sofa-rpc-version", "", "runtime SOFARPC version")
	flags.StringVar(&input.JavaBin, "java-bin", "", "java executable")
	flags.StringVar(&input.RuntimeJar, "runtime-jar", "", "worker runtime jar")
	flags.StringVar(&input.Service, "service", "doctor.ProbeService", "optional service marker")
	flags.StringVar(&input.Method, "method", "doctor", "optional method marker")
	if err := flags.Parse(args); err != nil {
		return err
	}
	input.ArgsJSON = "[]"
	input.PayloadMode = model.PayloadRaw
	resolved, err := a.resolveInvocation(input)
	if err != nil {
		return err
	}
	spec, err := a.Runtime.ResolveSpec(resolved.JavaBin, resolved.RuntimeJar, resolved.SofaRPCVersion, resolved.StubPaths)
	report := model.DoctorReport{
		ManifestPath:   resolved.ManifestPath,
		ManifestLoaded: resolved.ManifestFound,
		ActiveContext:  resolved.ActiveContext,
		Target:         resolved.Request.Target,
		StubWarnings:   runtime.ScanStubWarnings(resolved.StubPaths),
		Reachability:   runtime.ProbeTarget(resolved.Request.Target),
	}
	if err == nil {
		report.Runtime = model.RuntimeSnapshot{
			SofaRPCVersion: spec.SofaRPCVersion,
			RuntimeJar:     spec.RuntimeJar,
			JavaBin:        spec.JavaBin,
			JavaMajor:      spec.JavaMajor,
			DaemonKey:      spec.DaemonKey,
		}
		metadata, ensureErr := a.Runtime.EnsureDaemon(context.Background(), spec)
		if ensureErr != nil {
			report.Daemon = model.DaemonSnapshot{Ready: false, Error: ensureErr.Error()}
		} else {
			report.Daemon = model.DaemonSnapshot{Ready: true, Metadata: &metadata}
			probeRequest := model.InvocationRequest{
				RequestID:   randomID(),
				Service:     "doctor.ProbeService",
				Method:      "doctor",
				Args:        json.RawMessage("[]"),
				PayloadMode: model.PayloadRaw,
				Target:      resolved.Request.Target,
			}
			probeResponse, invokeErr := a.Runtime.Invoke(context.Background(), metadata, probeRequest)
			report.InvokeProbe = summarizeInvokeProbe(probeResponse, invokeErr)
		}
	} else {
		report.Runtime = model.RuntimeSnapshot{SofaRPCVersion: resolved.SofaRPCVersion}
		report.Daemon = model.DaemonSnapshot{Ready: false, Error: err.Error()}
	}
	return printJSON(a.Stdout, report)
}

func summarizeInvokeProbe(response model.InvocationResponse, invokeErr error) *model.InvokeProbe {
	probe := &model.InvokeProbe{Attempted: true}
	if invokeErr != nil {
		probe.TransportError = invokeErr.Error()
		return probe
	}
	if response.OK {
		probe.Reachable = true
		return probe
	}
	probe.Error = response.Error
	if response.Error == nil {
		return probe
	}
	switch response.Error.Code {
	case "PROVIDER_UNREACHABLE", "TARGET_TIMEOUT", "INTERNAL_ERROR":
		probe.Reachable = false
	default:
		probe.Reachable = true
	}
	return probe
}
