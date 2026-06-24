package mcp

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/hex1n/sofarpc-cli/internal/core/invoke"
	"github.com/hex1n/sofarpc-cli/internal/core/target"
	"github.com/hex1n/sofarpc-cli/internal/errcode"
)

func TestInvokeLimiterFromEnvUsesRaisedDefaults(t *testing.T) {
	t.Setenv(invoke.EnvMaxConcurrentInvokes, "")
	t.Setenv(invoke.EnvMaxConcurrentInvokesPerTarget, "")
	t.Setenv(invoke.EnvInvokeQueueTimeoutMS, "")

	limiter := NewInvokeLimiterFromEnv()
	diag := limiter.Diagnostics()
	if diag.GlobalLimit != 16 || diag.PerTargetLimit != 8 || diag.QueueTimeoutMS != 1000 {
		t.Fatalf("default limiter diagnostics: %+v", diag)
	}
}
func TestInvokeLimiterRejectsWhenGlobalLimitBusy(t *testing.T) {
	limiter := NewInvokeLimiter(InvokeLimiterConfig{GlobalLimit: 1, PerTargetLimit: 1, QueueTimeout: time.Millisecond})
	plan := samplePlan()

	release, diag, err := limiter.Acquire(context.Background(), "invoke", plan)
	if err != nil {
		t.Fatalf("first acquire: %v", err)
	}
	defer release()
	if diag.GlobalInUse != 1 || diag.TargetInUse != 1 {
		t.Fatalf("diagnostics after acquire: %+v", diag)
	}

	_, busyDiag, err := limiter.Acquire(context.Background(), "invoke", plan)
	if err == nil {
		t.Fatal("expected busy error")
	}
	var ecerr *errcode.Error
	if !errors.As(err, &ecerr) {
		t.Fatalf("error type = %T", err)
	}
	if ecerr.Code != errcode.InvocationBusy {
		t.Fatalf("code = %s, want %s", ecerr.Code, errcode.InvocationBusy)
	}
	if ecerr.Phase != "invoke" {
		t.Fatalf("phase = %q, want invoke", ecerr.Phase)
	}
	if !busyDiag.WaitingForRelease || busyDiag.GlobalInUse != 1 || busyDiag.TargetInUse != 1 {
		t.Fatalf("busy diagnostics: %+v", busyDiag)
	}
}

func TestInvokeLimiterPerTargetIsolation(t *testing.T) {
	limiter := NewInvokeLimiter(InvokeLimiterConfig{GlobalLimit: 3, PerTargetLimit: 1, QueueTimeout: time.Millisecond})
	planA := samplePlan()
	planB := samplePlan()
	planB.Target.DirectURL = "bolt://other.example:12200"

	releaseA, _, err := limiter.Acquire(context.Background(), "invoke", planA)
	if err != nil {
		t.Fatalf("acquire A: %v", err)
	}
	defer releaseA()
	releaseB, diagB, err := limiter.Acquire(context.Background(), "invoke", planB)
	if err != nil {
		t.Fatalf("different target should acquire: %v", err)
	}
	defer releaseB()
	if diagB.GlobalInUse != 2 || diagB.TargetInUse != 1 {
		t.Fatalf("diagnostics for B: %+v", diagB)
	}

	_, busyDiag, err := limiter.Acquire(context.Background(), "replay", planA)
	if err == nil {
		t.Fatal("expected same target to be busy")
	}
	var ecerr *errcode.Error
	if !errors.As(err, &ecerr) || ecerr.Code != errcode.InvocationBusy || ecerr.Phase != "replay" {
		t.Fatalf("busy error = %#v", err)
	}
	if busyDiag.Target != "h:1" || busyDiag.TargetInUse != 1 {
		t.Fatalf("busy diagnostics should point at target A: %+v", busyDiag)
	}
}

func TestExecutePlanWithPolicyReturnsRuntimeBusy(t *testing.T) {
	t.Setenv(envAllowInvoke, "true")
	t.Setenv(envAllowedTargetHosts, "")
	plan := samplePlan()
	limiter := NewInvokeLimiter(InvokeLimiterConfig{GlobalLimit: 1, PerTargetLimit: 1, QueueTimeout: time.Millisecond})
	release, _, err := limiter.Acquire(context.Background(), "invoke", plan)
	if err != nil {
		t.Fatalf("pre-acquire: %v", err)
	}
	defer release()

	result := executePlanWithPolicy(context.Background(), plan, "replay", target.Sources{
		Env: target.Config{DirectURL: plan.Target.DirectURL},
		ProjectPolicy: target.PolicyConfig{
			AllowedServices: []string{plan.Service},
		},
	}, nil, limiter)
	if result.Err == nil {
		t.Fatal("expected busy error")
	}
	var ecerr *errcode.Error
	if !errors.As(result.Err, &ecerr) || ecerr.Code != errcode.InvocationBusy {
		t.Fatalf("error = %#v", result.Err)
	}
	if !result.Rejected {
		t.Fatal("busy admission failure should be marked rejected")
	}
	if result.Outcome.Diagnostics == nil || result.Outcome.Diagnostics["invokeConcurrency"] == nil {
		t.Fatalf("missing limiter diagnostics: %+v", result.Outcome.Diagnostics)
	}
}

func TestDoctorReportsInvokeConcurrencyLimiter(t *testing.T) {
	limiter := NewInvokeLimiter(InvokeLimiterConfig{GlobalLimit: 3, PerTargetLimit: 2, QueueTimeout: 25 * time.Millisecond})
	out := callDoctor(t, Options{InvokeLimiter: limiter}, nil)
	check := findCheck(t, out, "invoke-concurrency")
	if !check.Ok {
		t.Fatalf("invoke-concurrency check should be informational: %+v", check)
	}
	if !strings.Contains(check.Detail, "global=3") || !strings.Contains(check.Detail, "queueTimeoutMs=25") {
		t.Fatalf("detail should include limits, got %q", check.Detail)
	}
	limiterData, ok := check.Data["limiter"].(map[string]any)
	if !ok {
		t.Fatalf("limiter data type = %T", check.Data["limiter"])
	}
	if limiterData["globalLimit"] != float64(3) || limiterData["perTargetLimit"] != float64(2) {
		t.Fatalf("limiter data: %+v", limiterData)
	}
}
