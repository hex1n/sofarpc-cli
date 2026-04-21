package target

import (
	"net"
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
