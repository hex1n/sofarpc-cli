package mcp

import "testing"

func TestDoctor_SessionsReportsPlanCaptureLimit(t *testing.T) {
	store := NewSessionStoreWithLimits(0, 32).WithMaxPlanBytes(2048)
	out := callDoctor(t, Options{Sessions: store}, nil)
	check := findCheck(t, out, "sessions")
	if !check.Ok {
		t.Fatalf("sessions check should be informational (Ok=true), got %+v", check)
	}
	if check.Data == nil {
		t.Fatal("sessions check should include data")
	}
	if got := check.Data["maxPlanBytes"]; got != float64(2048) {
		t.Fatalf("maxPlanBytes data = %#v, want 2048", got)
	}
}

func TestDoctor_SessionsReportsUnboundedPlanCapture(t *testing.T) {
	store := NewSessionStoreWithLimits(0, 32).WithMaxPlanBytes(0)
	out := callDoctor(t, Options{Sessions: store}, nil)
	check := findCheck(t, out, "sessions")
	if !check.Ok {
		t.Fatalf("sessions check should be informational (Ok=true), got %+v", check)
	}
	if got := check.Data["maxPlanBytes"]; got != float64(0) {
		t.Fatalf("maxPlanBytes data = %#v, want 0", got)
	}
}
