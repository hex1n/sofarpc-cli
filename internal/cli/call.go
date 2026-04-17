package cli

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/hex1n/sofarpc-cli/internal/model"
	"github.com/hex1n/sofarpc-cli/internal/runtime"
)

func (a *App) runCall(args []string) error {
	flags := failFlagSet("call")
	var input invocationInputs
	var fullResponse bool
	flags.StringVar(&input.ManifestPath, "manifest", "", "manifest file path")
	flags.StringVar(&input.ContextName, "context", "", "context name")
	flags.StringVar(&input.Service, "service", "", "service name")
	flags.StringVar(&input.Method, "method", "", "method name")
	flags.StringVar(&input.TypesCSV, "types", "", "comma-separated parameter type names")
	setArgs := func(value string) error { input.ArgsJSON = value; return nil }
	flags.Func("args", "JSON array of arguments; supports @file or - for stdin", setArgs)
	flags.Func("data", "alias for --args (curl-style)", setArgs)
	flags.Func("d", "alias for --args (curl-style short form)", setArgs)
	flags.StringVar(&input.PayloadMode, "payload-mode", "", "payload mode: raw, generic, schema")
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
	flags.BoolVar(&input.RefreshContract, "refresh-contract", false, "bypass local contract cache and re-resolve source/jar metadata")
	flags.BoolVar(&fullResponse, "full-response", false, "print full structured response")
	if err := flags.Parse(args); err != nil {
		return err
	}
	positionals := flags.Args()
	if input.Service == "" && len(positionals) > 0 {
		service, method, err := parseServiceMethod(positionals[0])
		if err != nil {
			return err
		}
		input.Service = service
		input.Method = method
	}
	if input.ArgsJSON == "" && len(positionals) > 1 {
		input.ArgsJSON = positionals[1]
	}
	resolvedArgs, err := loadArgsInput(input.ArgsJSON, a.Cwd, a.Stdin)
	if err != nil {
		return err
	}
	input.ArgsJSON = resolvedArgs
	resolved, err := a.resolveInvocation(input)
	if err != nil {
		return err
	}
	ctx := context.Background()
	contractSource, contractCacheHit, err := a.applyProjectMethodContract(ctx, &resolved, input.RefreshContract)
	if err != nil {
		return err
	}
	spec, err := a.Runtime.ResolveSpec(resolved.JavaBin, resolved.RuntimeJar, resolved.SofaRPCVersion, resolved.StubPaths)
	if err != nil {
		return err
	}
	if contractSource == "" && (len(resolved.Request.ParamTypes) == 0 || resolved.Request.PayloadMode == model.PayloadSchema) {
		schema, describeErr := a.resolveServiceSchema(ctx, resolved.ManifestPath, spec, resolved.Request.Service, runtime.DescribeOptions{NoCache: input.RefreshContract})
		if describeErr != nil {
			return fmt.Errorf("infer paramTypes via describe: %w", describeErr)
		}
		methodSchema, err := pickMethodSchema(schema, resolved.Request.Method, resolved.Request.Args, resolved.Request.ParamTypes)
		if err != nil {
			return err
		}
		if len(resolved.Request.ParamTypes) == 0 {
			resolved.Request.ParamTypes = methodSchema.ParamTypes
		}
		if resolved.Request.PayloadMode == model.PayloadSchema {
			resolved.Request.ParamTypeSignatures = methodSchema.ParamTypeSignatures
		}
	}
	if resolved.Request.PayloadMode == model.PayloadSchema && len(resolved.Request.ParamTypeSignatures) == 0 {
		resolved.Request.ParamTypeSignatures = append([]string{}, resolved.Request.ParamTypes...)
	}
	if contractSource == "" {
		if wrapped, ok := maybeWrapSingleArg(resolved.Request.Args, len(resolved.Request.ParamTypes)); ok {
			resolved.Request.Args = wrapped
		}
	}
	resolved.Request.RequestID = randomID()
	metadata, err := a.Runtime.EnsureDaemon(ctx, spec)
	if err != nil {
		return err
	}
	response, err := a.Runtime.Invoke(ctx, metadata, resolved.Request)
	if err != nil {
		return err
	}
	response.Diagnostics.RuntimeJar = spec.RuntimeJar
	response.Diagnostics.RuntimeVersion = spec.SofaRPCVersion
	response.Diagnostics.JavaBin = spec.JavaBin
	response.Diagnostics.JavaMajor = spec.JavaMajor
	response.Diagnostics.DaemonKey = spec.DaemonKey
	response.Diagnostics.ContractSource = contractSourceLabel(contractSource)
	response.Diagnostics.ContractCacheHit = contractCacheHit
	response.Diagnostics.WorkerClasspath = workerClasspathMode(resolved.StubPaths)
	if !response.OK {
		if err := printJSON(a.Stderr, response); err != nil {
			return err
		}
		message := "rpc invocation failed"
		if response.Error != nil && response.Error.Code != "" {
			message = response.Error.Code
		}
		return &exitError{message: message, silent: true}
	}
	if fullResponse {
		return printJSON(a.Stdout, response)
	}
	if len(response.Result) == 0 {
		return printJSON(a.Stdout, map[string]any{"ok": true})
	}
	var pretty any
	if err := json.Unmarshal(response.Result, &pretty); err != nil {
		return fmt.Errorf("worker returned invalid result payload: %w", err)
	}
	return printJSON(a.Stdout, pretty)
}

func randomID() string {
	var buffer [8]byte
	_, _ = rand.Read(buffer[:])
	return hex.EncodeToString(buffer[:])
}

func pickMethodTypes(schema model.ServiceSchema, method string, rawArgs json.RawMessage) ([]string, error) {
	match, err := pickMethodSchema(schema, method, rawArgs, nil)
	if err != nil {
		return nil, err
	}
	return match.ParamTypes, nil
}

func pickMethodSchema(schema model.ServiceSchema, method string, rawArgs json.RawMessage, preferredParamTypes []string) (model.MethodSchema, error) {
	var matches []model.MethodSchema
	for _, candidate := range schema.Methods {
		if candidate.Name == method {
			matches = append(matches, candidate)
		}
	}
	if len(matches) == 0 {
		return model.MethodSchema{}, fmt.Errorf("method %s not found on %s", method, schema.Service)
	}
	if len(matches) == 1 {
		return matches[0], nil
	}
	if len(preferredParamTypes) > 0 {
		var narrowed []model.MethodSchema
		for _, candidate := range matches {
			if sameParamTypes(candidate.ParamTypes, preferredParamTypes) {
				narrowed = append(narrowed, candidate)
			}
		}
		if len(narrowed) == 1 {
			return narrowed[0], nil
		}
		if len(narrowed) > 1 {
			matches = narrowed
		}
	}
	hint := argsArityHint(rawArgs)
	if hint >= 0 {
		var narrowed []model.MethodSchema
		for _, candidate := range matches {
			if len(candidate.ParamTypes) == hint {
				narrowed = append(narrowed, candidate)
			}
		}
		if len(narrowed) == 1 {
			return narrowed[0], nil
		}
	}
	options := make([]string, 0, len(matches))
	for _, candidate := range matches {
		options = append(options, "["+strings.Join(candidate.ParamTypes, ",")+"]")
	}
	return model.MethodSchema{}, fmt.Errorf("method %s.%s is overloaded; pass --types to disambiguate: %s",
		schema.Service, method, strings.Join(options, " | "))
}

func sameParamTypes(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}

func maybeWrapSingleArg(raw json.RawMessage, arity int) (json.RawMessage, bool) {
	if arity != 1 {
		return nil, false
	}
	if len(raw) == 0 {
		return nil, false
	}
	if isJSONArray(raw) {
		return nil, false
	}
	wrapped := make([]byte, 0, len(raw)+2)
	wrapped = append(wrapped, '[')
	wrapped = append(wrapped, raw...)
	wrapped = append(wrapped, ']')
	return wrapped, true
}

func isJSONArray(raw []byte) bool {
	for _, b := range raw {
		switch b {
		case ' ', '\t', '\n', '\r':
			continue
		case '[':
			return true
		default:
			return false
		}
	}
	return false
}

func argsArityHint(raw json.RawMessage) int {
	if len(raw) == 0 {
		return -1
	}
	if !isJSONArray(raw) {
		return 1
	}
	var items []json.RawMessage
	if err := json.Unmarshal(raw, &items); err != nil {
		return -1
	}
	return len(items)
}

func loadArgsInput(value, cwd string, stdin io.Reader) (string, error) {
	if value == "" {
		return "", nil
	}
	if value == "-" {
		if stdin == nil {
			return "", fmt.Errorf("--args - requires stdin")
		}
		data, err := io.ReadAll(stdin)
		if err != nil {
			return "", fmt.Errorf("read --args from stdin: %w", err)
		}
		return strings.TrimSpace(string(data)), nil
	}
	if strings.HasPrefix(value, "@") {
		path := value[1:]
		if path == "" {
			return "", fmt.Errorf("--args @ requires a file path")
		}
		if !filepath.IsAbs(path) {
			path = filepath.Join(cwd, path)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return "", fmt.Errorf("read --args from %s: %w", path, err)
		}
		return strings.TrimSpace(string(data)), nil
	}
	return value, nil
}
