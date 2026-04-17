package cli

import "testing"

func TestWorkerClasspathMode(t *testing.T) {
	if got := workerClasspathMode(nil); got != "runtime-only" {
		t.Fatalf("workerClasspathMode(nil) = %q", got)
	}
	if got := workerClasspathMode([]string{"a.jar"}); got != "runtime+stubs" {
		t.Fatalf("workerClasspathMode(stubs) = %q", got)
	}
}

func TestContractSourceLabel(t *testing.T) {
	if got := contractSourceLabel("project-source"); got != "project-source" {
		t.Fatalf("contractSourceLabel(project-source) = %q", got)
	}
	if got := contractSourceLabel(""); got != "" {
		t.Fatalf("contractSourceLabel(empty) = %q", got)
	}
}
