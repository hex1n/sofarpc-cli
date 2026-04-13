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
	spec, err := a.Runtime.ResolveSpec(resolved.JavaBin, resolved.RuntimeJar, resolved.SofaRPCVersion, resolved.StubPaths)
	if err != nil {
		return err
	}
	resolved.Request.RequestID = randomID()
	metadata, err := a.Runtime.EnsureDaemon(context.Background(), spec)
	if err != nil {
		return err
	}
	response, err := a.Runtime.Invoke(context.Background(), metadata, resolved.Request)
	if err != nil {
		return err
	}
	response.Diagnostics.RuntimeJar = spec.RuntimeJar
	response.Diagnostics.RuntimeVersion = spec.SofaRPCVersion
	response.Diagnostics.JavaBin = spec.JavaBin
	response.Diagnostics.JavaMajor = spec.JavaMajor
	response.Diagnostics.DaemonKey = spec.DaemonKey
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
