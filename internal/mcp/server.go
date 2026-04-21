// Package mcp wires the sofarpc tool surface into the MCP SDK. Each
// handler lives in its own file (open.go, describe.go, ...); this file
// owns construction, the shared Options, and the input-only types that
// describe the tool schemas. See docs/architecture.md §3.
package mcp

import (
	"github.com/hex1n/sofarpc-cli/internal/core/contract"
	"github.com/hex1n/sofarpc-cli/internal/core/target"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	serverName    = "sofarpc-mcp"
	serverVersion = "0.0.0-dev"
)

// Options carries the ambient state the handlers need. The entrypoint
// (cmd/sofarpc-mcp) fills this from SOFARPC_* env — the server itself
// does no I/O at construction.
type Options struct {
	TargetSources target.Sources
	Sessions      *SessionStore
	Facade        contract.Store
}

// New returns an MCP server with the six sofarpc tools registered.
func New(opts Options) *sdkmcp.Server {
	if opts.Sessions == nil {
		opts.Sessions = NewSessionStore()
	}
	holder := newFacadeHolder(opts.Facade)
	server := sdkmcp.NewServer(&sdkmcp.Implementation{
		Name:    serverName,
		Version: serverVersion,
	}, nil)
	registerOpen(server, opts, holder)
	registerDescribe(server, opts, holder)
	registerTarget(server, opts)
	registerInvoke(server, opts, holder)
	registerReplay(server, opts)
	registerDoctor(server, opts, holder)
	return server
}

// --- sofarpc_open (see open.go) --------------------------------------------

// OpenInput is the input shape for sofarpc_open. All fields are optional;
// Cwd defaults to the process CWD, Project falls back to Cwd.
type OpenInput struct {
	Cwd     string `json:"cwd,omitempty"`
	Project string `json:"project,omitempty"`
}

// --- sofarpc_describe (see describe.go) ------------------------------------

// DescribeInput is the input shape for sofarpc_describe. Types is the
// paramType list the agent may supply to disambiguate overloads.
type DescribeInput struct {
	Service string   `json:"service,omitempty"`
	Method  string   `json:"method,omitempty"`
	Types   []string `json:"types,omitempty"`
}

// --- sofarpc_invoke (see invoke.go) ----------------------------------------

// InvokeInput is the input shape for sofarpc_invoke. Args is any so the
// agent can send a JSON array inline, or an "@<path>" string pointing at
// a file that contains a JSON array of the same shape. Anything else is
// rejected as input.args-invalid. Stdin ("-") is not accepted — the MCP
// server's stdin carries the transport, not user data. Version and
// TargetAppName are optional transport hints for direct invoke paths.
type InvokeInput struct {
	Service          string   `json:"service,omitempty"`
	Method           string   `json:"method,omitempty"`
	Types            []string `json:"types,omitempty"`
	Args             any      `json:"args,omitempty"`
	Version          string   `json:"version,omitempty"`
	TargetAppName    string   `json:"targetAppName,omitempty"`
	DirectURL        string   `json:"directUrl,omitempty"`
	RegistryAddress  string   `json:"registryAddress,omitempty"`
	RegistryProtocol string   `json:"registryProtocol,omitempty"`
	TimeoutMS        int      `json:"timeoutMs,omitempty"`
	DryRun           bool     `json:"dryRun,omitempty"`
	// SessionID, when set, tags the resulting plan onto the session so
	// sofarpc_replay can replay it without re-sending the payload.
	SessionID string `json:"sessionId,omitempty"`
}

// --- sofarpc_replay (see replay.go) ----------------------------------------

// ReplayInput is the input shape for sofarpc_replay. Exactly one of
// SessionID and Payload should be set — the handler errors otherwise.
// DryRun mirrors sofarpc_invoke.
type ReplayInput struct {
	SessionID string `json:"sessionId,omitempty"`
	Payload   any    `json:"payload,omitempty"`
	DryRun    bool   `json:"dryRun,omitempty"`
}

// --- sofarpc_doctor (see doctor.go) ----------------------------------------

// DoctorInput is the input shape for sofarpc_doctor. Service is optional:
// when set, doctor biases target resolution toward a per-service uniqueId.
type DoctorInput struct {
	Service string `json:"service,omitempty"`
}
