package target

import (
	"net"
	"os"
	"path/filepath"
	"testing"
)

func TestResolve_InputOverridesEnvAndDefaults(t *testing.T) {
	report := Resolve(Input{
		DirectURL: "bolt://input-host:12200",
		Explain:   true,
	}, Sources{
		Env: Config{
			DirectURL:     "bolt://env-host:12200",
			Serialization: "fastjson2",
		},
	})

	if report.Target.Mode != ModeDirect {
		t.Fatalf("mode: got %q want %q", report.Target.Mode, ModeDirect)
	}
	if report.Target.DirectURL != "bolt://input-host:12200" {
		t.Fatalf("directUrl: got %q", report.Target.DirectURL)
	}
	if report.Target.Serialization != "fastjson2" {
		t.Fatalf("serialization: got %q want fastjson2", report.Target.Serialization)
	}
	if report.Target.Protocol != defaultProtocol {
		t.Fatalf("protocol: got %q want %q", report.Target.Protocol, defaultProtocol)
	}
	if len(report.Layers) == 0 {
		t.Fatal("layers should be populated")
	}
	if len(report.Trace) == 0 {
		t.Fatal("trace should be populated when explain=true")
	}
	if len(report.Explain) == 0 {
		t.Fatal("explain should be populated when explain=true")
	}
}

func TestResolve_ProjectLocalOverridesProjectAndEnv(t *testing.T) {
	report := Resolve(Input{Explain: true}, Sources{
		Env: Config{
			DirectURL:     "bolt://env-host:12200",
			Serialization: "fastjson2",
		},
		Project: Config{
			DirectURL: "bolt://project-host:12200",
			TimeoutMS: 4000,
		},
		ProjectLocal: Config{
			DirectURL: "bolt://local-host:12200",
		},
	})

	if report.Target.Mode != ModeDirect {
		t.Fatalf("mode: got %q want direct", report.Target.Mode)
	}
	if report.Target.DirectURL != "bolt://local-host:12200" {
		t.Fatalf("directUrl: got %q", report.Target.DirectURL)
	}
	if report.Target.TimeoutMS != 4000 {
		t.Fatalf("timeoutMs should come from shared project config, got %d", report.Target.TimeoutMS)
	}
	if report.Target.Serialization != "fastjson2" {
		t.Fatalf("serialization should fall through to env, got %q", report.Target.Serialization)
	}
	if len(report.Trace) == 0 {
		t.Fatal("trace should be populated when explain=true")
	}
}

func TestResolve_ProjectRegistrySuppressesEnvDirect(t *testing.T) {
	report := Resolve(Input{}, Sources{
		Env: Config{DirectURL: "bolt://env-host:12200"},
		Project: Config{
			RegistryAddress:  "zookeeper://zk:2181",
			RegistryProtocol: "zookeeper",
		},
	})

	if report.Target.Mode != ModeRegistry {
		t.Fatalf("mode: got %q want registry", report.Target.Mode)
	}
	if report.Target.RegistryAddress != "zookeeper://zk:2181" {
		t.Fatalf("registryAddress: got %q", report.Target.RegistryAddress)
	}
	if report.Target.DirectURL != "" {
		t.Fatalf("directUrl should be suppressed, got %q", report.Target.DirectURL)
	}
	if report.Target.RegistryProtocol != "zookeeper" {
		t.Fatalf("registryProtocol: got %q", report.Target.RegistryProtocol)
	}
}

func TestResolve_ProjectLocalDirectSuppressesProjectRegistry(t *testing.T) {
	report := Resolve(Input{}, Sources{
		Project:      Config{RegistryAddress: "zookeeper://zk:2181", RegistryProtocol: "zookeeper"},
		ProjectLocal: Config{DirectURL: "bolt://local-host:12200"},
	})

	if report.Target.Mode != ModeDirect {
		t.Fatalf("mode: got %q want direct", report.Target.Mode)
	}
	if report.Target.DirectURL != "bolt://local-host:12200" {
		t.Fatalf("directUrl: got %q", report.Target.DirectURL)
	}
	if report.Target.RegistryAddress != "" || report.Target.RegistryProtocol != "" {
		t.Fatalf("registry fields should be suppressed, got %+v", report.Target)
	}
}

func TestResolve_InputRegistrySuppressesEnvDirect(t *testing.T) {
	report := Resolve(Input{
		RegistryAddress:  "zookeeper://input-zk:2181",
		RegistryProtocol: "zookeeper",
	}, Sources{
		Env: Config{DirectURL: "bolt://env-host:12200"},
	})

	if report.Target.Mode != ModeRegistry {
		t.Fatalf("mode: got %q want registry", report.Target.Mode)
	}
	if report.Target.RegistryAddress != "zookeeper://input-zk:2181" {
		t.Fatalf("registryAddress: got %q", report.Target.RegistryAddress)
	}
	if report.Target.DirectURL != "" {
		t.Fatalf("directUrl should be suppressed, got %q", report.Target.DirectURL)
	}
}

func TestResolve_InputDirectSuppressesProjectRegistry(t *testing.T) {
	report := Resolve(Input{
		DirectURL: "bolt://input-host:12200",
	}, Sources{
		Project: Config{RegistryAddress: "zookeeper://zk:2181", RegistryProtocol: "zookeeper"},
	})

	if report.Target.Mode != ModeDirect {
		t.Fatalf("mode: got %q want direct", report.Target.Mode)
	}
	if report.Target.DirectURL != "bolt://input-host:12200" {
		t.Fatalf("directUrl: got %q", report.Target.DirectURL)
	}
	if report.Target.RegistryAddress != "" || report.Target.RegistryProtocol != "" {
		t.Fatalf("registry fields should be suppressed, got %+v", report.Target)
	}
}

func TestResolve_EndpointTraceShowsLowerLayerEndpointsAsShadowed(t *testing.T) {
	report := Resolve(Input{Explain: true}, Sources{
		Env:     Config{DirectURL: "bolt://env-host:12200"},
		Project: Config{RegistryAddress: "zookeeper://zk:2181"},
	})

	var endpointTrace FieldTrace
	for _, trace := range report.Trace {
		if trace.Field == "registryAddress" {
			endpointTrace = trace
			break
		}
	}
	if endpointTrace.Winner.Layer != "project" {
		t.Fatalf("endpoint winner: %+v", endpointTrace)
	}
	if len(endpointTrace.Shadowed) != 1 || endpointTrace.Shadowed[0].Value != "directUrl=bolt://env-host:12200" {
		t.Fatalf("shadowed endpoint values: %+v", endpointTrace.Shadowed)
	}
}

func TestResolve_RegistryWinsWhenNoDirectURL(t *testing.T) {
	report := Resolve(Input{}, Sources{
		Env: Config{RegistryAddress: "zookeeper://127.0.0.1:2181"},
	})

	if report.Target.Mode != ModeRegistry {
		t.Fatalf("mode: got %q want %q", report.Target.Mode, ModeRegistry)
	}
	if report.Target.DirectURL != "" {
		t.Fatalf("directUrl should be cleared in registry mode, got %q", report.Target.DirectURL)
	}
}

func TestProjectSources_LoadsSharedAndLocalConfig(t *testing.T) {
	root := t.TempDir()
	writeProjectTargetConfig(t, root, "config.json", `{
  "directUrl": "bolt://project-host:12200",
  "timeoutMs": 4000
}`)
	writeProjectTargetConfig(t, root, "config.local.json", `{
  "directUrl": "bolt://local-host:12200",
  "connectTimeoutMs": 250
}`)

	sources := ProjectSources(root, Config{Serialization: "fastjson2"})
	report := Resolve(Input{}, sources)

	if report.Target.DirectURL != "bolt://local-host:12200" {
		t.Fatalf("directUrl: got %q", report.Target.DirectURL)
	}
	if report.Target.TimeoutMS != 4000 {
		t.Fatalf("timeoutMs: got %d", report.Target.TimeoutMS)
	}
	if report.Target.ConnectTimeoutMS != 250 {
		t.Fatalf("connectTimeoutMs: got %d", report.Target.ConnectTimeoutMS)
	}
	if report.Target.Serialization != "fastjson2" {
		t.Fatalf("serialization: got %q", report.Target.Serialization)
	}
}

func TestProjectSources_ReportsInvalidConfig(t *testing.T) {
	root := t.TempDir()
	writeProjectTargetConfig(t, root, "config.json", `{"directUrl":"bolt://project-host:12200", "unknown": true}`)

	sources := ProjectSources(root, Config{})
	if len(sources.ConfigErrors) != 1 {
		t.Fatalf("config errors: got %+v", sources.ConfigErrors)
	}
	report := Resolve(Input{}, sources)
	if len(report.ConfigErrors) != 1 {
		t.Fatalf("report config errors: got %+v", report.ConfigErrors)
	}
}

func TestProjectSources_RejectsDirectAndRegistryInSameFile(t *testing.T) {
	root := t.TempDir()
	writeProjectTargetConfig(t, root, "config.json", `{
  "directUrl": "bolt://project-host:12200",
  "registryAddress": "zookeeper://zk:2181"
}`)

	sources := ProjectSources(root, Config{})
	if len(sources.ConfigErrors) != 1 {
		t.Fatalf("config errors: got %+v", sources.ConfigErrors)
	}
	if sources.Project.DirectURL != "" || sources.Project.RegistryAddress != "" {
		t.Fatalf("invalid config should not be applied: %+v", sources.Project)
	}
}

func TestProjectSources_RejectsModeField(t *testing.T) {
	root := t.TempDir()
	writeProjectTargetConfig(t, root, "config.json", `{
  "mode": "registry",
  "directUrl": "bolt://project-host:12200"
}`)

	sources := ProjectSources(root, Config{})
	if len(sources.ConfigErrors) != 1 {
		t.Fatalf("config errors: got %+v", sources.ConfigErrors)
	}
	if sources.Project.DirectURL != "" {
		t.Fatalf("invalid config should not be applied: %+v", sources.Project)
	}
}

func TestProbe_DirectReachable(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer listener.Close()

	probe := Probe(Config{
		DirectURL:        "bolt://" + listener.Addr().String(),
		ConnectTimeoutMS: 200,
	})
	if !probe.Reachable {
		t.Fatalf("probe should be reachable, got %+v", probe)
	}
	if probe.Target != listener.Addr().String() {
		t.Fatalf("target: got %q want %q", probe.Target, listener.Addr().String())
	}
}

func TestProbe_UnresolvedTargetFails(t *testing.T) {
	probe := Probe(Config{})
	if probe.Reachable {
		t.Fatalf("probe should fail, got %+v", probe)
	}
	if probe.Message == "" {
		t.Fatal("probe should explain the failure")
	}
}

func writeProjectTargetConfig(t *testing.T, root, name, body string) {
	t.Helper()
	path := filepath.Join(root, ".sofarpc", name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll(%s): %v", path, err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("WriteFile(%s): %v", path, err)
	}
}
