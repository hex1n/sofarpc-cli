//go:build spike

// spike-invoke is a throwaway developer harness for exercising the
// direct-transport invoke path against a known SOFARPC service. It is
// not part of the shipping CLI surface — `go build ./...` excludes this
// binary unless the `spike` build tag is supplied explicitly.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	coreinvoke "github.com/hex1n/sofarpc-cli/internal/core/invoke"
	"github.com/hex1n/sofarpc-cli/internal/core/target"
	"github.com/hex1n/sofarpc-cli/internal/errcode"
	"github.com/hex1n/sofarpc-cli/internal/sofarpcwire"
)

const (
	defaultAddr    = "10.74.194.40:12200"
	defaultService = "com.thfund.salesfundmp.facade.sales.holdings.SalesDailyHoldingsFacade"
	defaultMethod  = "queryPortfolioAvailableCash"
	defaultType    = "com.thfund.salesfundmp.facade.model.request.DailyHoldingsQueryRequest"
	defaultMPCode  = int64(434153733362950144)
)

type output struct {
	Addr          string         `json:"addr"`
	Service       string         `json:"service"`
	Method        string         `json:"method"`
	ParamTypes    []string       `json:"paramTypes"`
	Version       string         `json:"version,omitempty"`
	TargetAppName string         `json:"targetAppName,omitempty"`
	Diagnostics   map[string]any `json:"diagnostics,omitempty"`
	Result        any            `json:"result,omitempty"`
	ResultType    string         `json:"resultType,omitempty"`
	Success       *bool          `json:"success,omitempty"`
	Message       string         `json:"message,omitempty"`
	ErrorCode     string         `json:"errorCode,omitempty"`
	Error         string         `json:"error,omitempty"`
}

func main() {
	addr := flag.String("addr", defaultAddr, "target host:port")
	service := flag.String("service", defaultService, "facade service interface")
	method := flag.String("method", defaultMethod, "facade method")
	types := flag.String("types", defaultType, "comma-separated parameter types")
	argFile := flag.String("arg-file", "", "JSON file containing an array of args")
	timeout := flag.Duration("timeout", 10*time.Second, "request timeout")
	version := flag.String("version", sofarpcwire.DefaultVersion, "SOFARPC service version")
	uniqueID := flag.String("unique-id", "", "SOFARPC unique id")
	targetApp := flag.String("target-app", "", "optional target app name")
	flag.Parse()

	args, err := loadArgs(*argFile)
	if err != nil {
		exitErr(err)
	}

	paramTypes := splitCSV(*types)
	serviceName := strings.TrimSpace(*service)
	methodName := strings.TrimSpace(*method)
	versionValue := strings.TrimSpace(*version)
	targetAppName := strings.TrimSpace(*targetApp)

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()

	out := output{
		Addr:          *addr,
		Service:       serviceName,
		Method:        methodName,
		ParamTypes:    paramTypes,
		Version:       versionValue,
		TargetAppName: targetAppName,
	}

	plan := coreinvoke.Plan{
		Service:       serviceName,
		Method:        methodName,
		ParamTypes:    paramTypes,
		Args:          args,
		Version:       versionValue,
		TargetAppName: targetAppName,
		Target: target.Config{
			Mode:          target.ModeDirect,
			DirectURL:     directURL(*addr),
			Protocol:      "bolt",
			Serialization: "hessian2",
			UniqueID:      strings.TrimSpace(*uniqueID),
			TimeoutMS:     int((*timeout) / time.Millisecond),
		},
	}

	outcome, execErr := coreinvoke.Execute(ctx, plan, "spike-invoke")
	out.Diagnostics = outcome.Diagnostics
	if execErr != nil {
		if ecerr, ok := execErr.(*errcode.Error); ok {
			out.ErrorCode = string(ecerr.Code)
			out.Error = ecerr.Message
		} else {
			out.Error = execErr.Error()
		}
		printOutput(out)
		os.Exit(1)
	}

	out.Result = outcome.Result
	out.ResultType, out.Success, out.Message = extractResultSummary(outcome.Result, "")
	printOutput(out)
}

func loadArgs(path string) ([]any, error) {
	if strings.TrimSpace(path) == "" {
		return []any{
			map[string]any{
				"@type":      defaultType,
				"tradeDate":  "20260414",
				"mpCode":     defaultMPCode,
				"mpCodeList": []any{defaultMPCode},
			},
		}, nil
	}
	return sofarpcwire.LoadArgsFile(path)
}

func splitCSV(raw string) []string {
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func extractResultSummary(appResponse any, fallback string) (string, *bool, string) {
	responseMap, ok := appResponse.(map[string]any)
	if !ok {
		return "", nil, fallback
	}

	resultType, _ := responseMap["type"].(string)
	fields, _ := responseMap["fields"].(map[string]any)
	if fields == nil {
		return resultType, nil, fallback
	}

	var successPtr *bool
	if success, ok := fields["success"].(bool); ok {
		successPtr = &success
	}
	if message, ok := fields["message"].(string); ok {
		return resultType, successPtr, message
	}
	return resultType, successPtr, fallback
}

func directURL(addr string) string {
	addr = strings.TrimSpace(addr)
	if strings.Contains(addr, "://") {
		return addr
	}
	return "bolt://" + addr
}

func printOutput(out output) {
	body, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		fmt.Fprintln(os.Stderr, strings.TrimSpace(err.Error()))
		os.Exit(1)
	}
	fmt.Println(string(body))
}

func exitErr(err error) {
	printOutput(output{Error: strings.TrimSpace(err.Error())})
	os.Exit(1)
}
