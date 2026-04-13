package cli

import (
	"errors"
	"testing"

	"github.com/hex1n/sofa-rpcctl/greenfield/internal/model"
)

func TestSummarizeInvokeProbeTransportError(t *testing.T) {
	probe := summarizeInvokeProbe(model.InvocationResponse{}, errors.New("dial tcp timeout"))
	if !probe.Attempted || probe.Reachable {
		t.Fatalf("expected failed transport probe, got %+v", probe)
	}
	if probe.TransportError == "" {
		t.Fatalf("expected transport error to be populated, got %+v", probe)
	}
}

func TestSummarizeInvokeProbeTreatsProviderNotFoundAsReachable(t *testing.T) {
	probe := summarizeInvokeProbe(model.InvocationResponse{
		OK: false,
		Error: &model.RuntimeError{
			Code: "PROVIDER_NOT_FOUND",
		},
	}, nil)
	if !probe.Reachable {
		t.Fatalf("expected provider-not-found probe to confirm rpc path, got %+v", probe)
	}
}

func TestSummarizeInvokeProbeTreatsProviderUnreachableAsNotReachable(t *testing.T) {
	probe := summarizeInvokeProbe(model.InvocationResponse{
		OK: false,
		Error: &model.RuntimeError{
			Code: "PROVIDER_UNREACHABLE",
		},
	}, nil)
	if probe.Reachable {
		t.Fatalf("expected provider-unreachable probe to fail reachability, got %+v", probe)
	}
}
