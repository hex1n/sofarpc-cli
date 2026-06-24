package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"sync"
	"testing"

	"github.com/hex1n/sofarpc-cli/internal/errcode"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestRawToolPanicGuardReturnsStructuredError(t *testing.T) {
	server := sdkmcp.NewServer(&sdkmcp.Implementation{Name: "panic-test", Version: "0.0.0"}, nil)
	addRawTool(server, &sdkmcp.Tool{
		Name:         "panic_raw",
		InputSchema:  objectSchema(nil),
		OutputSchema: invokeOutputSchema(),
	}, "invoke", func(ecerr *errcode.Error) *sdkmcp.CallToolResult {
		return invokeToolResult(InvokeOutput{Error: ecerr}, errorText("invoke failed", ecerr), true)
	}, func(context.Context, *sdkmcp.CallToolRequest) (*sdkmcp.CallToolResult, error) {
		panic("raw boom")
	})

	ctx := context.Background()
	client := connect(t, ctx, server)
	defer client.Close()

	result, err := client.CallTool(ctx, &sdkmcp.CallToolParams{Name: "panic_raw"})
	if err != nil {
		t.Fatalf("call raw panic tool: %v", err)
	}
	if !result.IsError {
		t.Fatal("panic result should be a tool error")
	}
	var out InvokeOutput
	decodeStructuredContent(t, result, &out)
	if out.Error == nil || out.Error.Code != errcode.InternalFailure || out.Error.Phase != "invoke" {
		t.Fatalf("panic error = %+v", out.Error)
	}
}

func TestTypedToolPanicGuardReturnsStructuredError(t *testing.T) {
	server := sdkmcp.NewServer(&sdkmcp.Implementation{Name: "panic-test", Version: "0.0.0"}, nil)
	addTypedTool(server, &sdkmcp.Tool{
		Name: "panic_typed",
	}, "describe", func(ecerr *errcode.Error) DescribeOutput {
		return DescribeOutput{Error: ecerr}
	}, func(context.Context, *sdkmcp.CallToolRequest, struct{}) (*sdkmcp.CallToolResult, DescribeOutput, error) {
		panic("typed boom")
	})

	ctx := context.Background()
	client := connect(t, ctx, server)
	defer client.Close()

	result, err := client.CallTool(ctx, &sdkmcp.CallToolParams{Name: "panic_typed"})
	if err != nil {
		t.Fatalf("call typed panic tool: %v", err)
	}
	if !result.IsError {
		t.Fatal("panic result should be a tool error")
	}
	var out DescribeOutput
	decodeStructuredContent(t, result, &out)
	if out.Error == nil || out.Error.Code != errcode.InternalFailure || out.Error.Phase != "describe" {
		t.Fatalf("panic error = %+v", out.Error)
	}
}

func TestRunDoctorCheckRecoversPanic(t *testing.T) {
	checks := make([]DoctorCheck, 1)
	var wg sync.WaitGroup
	wg.Add(1)
	runDoctorCheck(&wg, checks, 0, "target", func() DoctorCheck {
		panic("doctor boom")
	})
	wg.Wait()

	if checks[0].Ok {
		t.Fatalf("doctor panic check should fail: %+v", checks[0])
	}
	if checks[0].Name != "target" || checks[0].Data["code"] != errcode.InternalFailure {
		t.Fatalf("doctor panic check = %+v", checks[0])
	}
}

func decodeStructuredContent(t *testing.T, result *sdkmcp.CallToolResult, out any) {
	t.Helper()
	body, err := json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatalf("marshal structured content: %v", err)
	}
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber()
	if err := dec.Decode(out); err != nil {
		t.Fatalf("decode structured content: %v", err)
	}
}
