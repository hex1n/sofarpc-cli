// Package mcp wires the sofarpc tool surface into the MCP SDK. Each
// handler lives in its own file (open.go, describe.go, ...); this file
// owns construction, the shared Options, and the input-only types that
// describe the tool schemas. See docs/architecture.md §3.
package mcp

import (
	"strings"

	"github.com/hex1n/sofarpc-cli/internal/core/contract"
	"github.com/hex1n/sofarpc-cli/internal/core/invocationprops"
	"github.com/hex1n/sofarpc-cli/internal/core/target"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	serverName           = "sofarpc-mcp"
	defaultServerVersion = "0.0.0-dev"
)

// Options carries the ambient state the handlers need. The entrypoint
// (cmd/sofarpc-mcp) fills this from SOFARPC_* env — the server itself
// does no I/O at construction.
//
// ServerVersion, when set, is surfaced through the MCP implementation
// metadata. Release builds should pass the same value printed by the
// CLI version command.
//
// ContractLoadError, when non-nil, signals that the entrypoint tried to
// materialize a contract store but failed. Handlers surface it in
// sofarpc_open / sofarpc_doctor so agents see the reason without
// having to scrape the server's stderr.
//
// ContractLoader, when non-nil, is called lazily on the first tool path
// that asks for the contract store. A store (and any error) it returns
// replaces any synchronously-supplied Contract / ContractLoadError, so
// large Java trees do not delay MCP server startup or tool registration.
// When nil, the sync Contract fields are used as-is.
type Options struct {
	TargetSources     target.Sources
	ServerVersion     string
	Sessions          *SessionStore
	Contract          contract.Store
	ContractLoadError error
	ContractLoader    func() (contract.Store, error)
	// ProjectContractLoader loads a contract store for the resolved
	// projectRoot of a tool call or session. When set, project/session-scoped
	// calls use it instead of the process-global ContractLoader.
	ProjectContractLoader func(projectRoot string) (contract.Store, error)
}

// New returns an MCP server with the sofarpc tools registered.
func New(opts Options) *sdkmcp.Server {
	if opts.Sessions == nil {
		opts.Sessions = NewSessionStore()
	}
	holder := newContractHolder(opts.Contract, loadErrorMessage(opts.ContractLoadError), opts.ContractLoader)
	holder.SetDefaultRoot(opts.TargetSources.ProjectRoot)
	holder.SetProjectLoader(opts.ProjectContractLoader)
	server := sdkmcp.NewServer(&sdkmcp.Implementation{
		Name:    serverName,
		Version: normalizeServerVersion(opts.ServerVersion),
	}, nil)
	registerPrompts(server)
	registerResources(server, opts, holder)
	registerInitProject(server, opts, holder)
	registerOpen(server, opts, holder)
	registerDescribe(server, opts, holder)
	registerTarget(server, opts)
	registerInvoke(server, opts, holder)
	registerReplay(server, opts)
	registerDoctor(server, opts, holder)
	return server
}

func normalizeServerVersion(version string) string {
	version = strings.TrimSpace(version)
	if version == "" {
		return defaultServerVersion
	}
	return version
}

// loadErrorMessage keeps the contract-banner surface free of raw error
// values — callers want a short string they can surface to agents
// verbatim. An empty return means "no load error", which open/doctor
// treat as the healthy state.
func loadErrorMessage(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// --- sofarpc_open (see open.go) --------------------------------------------

// OpenInput is the input shape for sofarpc_open. All fields are optional;
// Cwd defaults to the process CWD, Project falls back to Cwd.
type OpenInput struct {
	Cwd     string `json:"cwd,omitempty"`
	Project string `json:"project,omitempty"`
	// Profile selects the Active Target Profile for this session; sessionId
	// calls inherit it. Empty falls back to the project's defaultProfile.
	Profile string `json:"profile,omitempty"`
}

// --- sofarpc_describe (see describe.go) ------------------------------------

// DescribeInput is the input shape for sofarpc_describe. Types is the
// paramType list the agent may supply to disambiguate overloads.
type DescribeInput struct {
	Cwd       string   `json:"cwd,omitempty"`
	Project   string   `json:"project,omitempty"`
	SessionID string   `json:"sessionId,omitempty"`
	Service   string   `json:"service,omitempty"`
	Method    string   `json:"method,omitempty"`
	Types     []string `json:"types,omitempty"`
}

// --- sofarpc_invoke (see invoke.go) ----------------------------------------

// InvokeInput is the input shape for sofarpc_invoke. Args is any because
// runtime decoding preserves legacy input forms, but the public MCP schema
// advertises a JSON array argument vector. Invalid shapes are rejected as
// input.args-invalid. Version and TargetAppName are optional transport hints
// for direct invoke paths.
type InvokeInput struct {
	Cwd                  string                       `json:"cwd,omitempty"`
	Project              string                       `json:"project,omitempty"`
	Service              string                       `json:"service,omitempty"`
	Method               string                       `json:"method,omitempty"`
	Types                []string                     `json:"types,omitempty"`
	Args                 any                          `json:"args,omitempty"`
	Version              string                       `json:"version,omitempty"`
	TargetAppName        string                       `json:"targetAppName,omitempty"`
	InvocationProperties invocationprops.Declarations `json:"invocationProperties,omitempty"`
	Profile              string                       `json:"profile,omitempty"`
	DirectURL            string                       `json:"directUrl,omitempty"`
	RegistryAddress      string                       `json:"registryAddress,omitempty"`
	RegistryProtocol     string                       `json:"registryProtocol,omitempty"`
	TimeoutMS            int                          `json:"timeoutMs,omitempty"`
	DryRun               bool                         `json:"dryRun,omitempty"`
	Trusted              bool                         `json:"trusted,omitempty"`
	ContractMode         string                       `json:"contractMode,omitempty"`
	// SessionID, when set, tags the resulting plan onto the session so
	// sofarpc_replay can replay it without re-sending the payload.
	SessionID string `json:"sessionId,omitempty"`
}

// --- sofarpc_replay (see replay.go) ----------------------------------------

// ReplayInput is the input shape for sofarpc_replay. SessionID can either
// select the captured session plan or, when Payload is present, provide the
// project/safety context for that literal plan. DryRun mirrors sofarpc_invoke.
type ReplayInput struct {
	SessionID string `json:"sessionId,omitempty"`
	Cwd       string `json:"cwd,omitempty"`
	Project   string `json:"project,omitempty"`
	Payload   any    `json:"payload,omitempty"`
	DryRun    bool   `json:"dryRun,omitempty"`
}

// --- sofarpc_doctor (see doctor.go) ----------------------------------------

// DoctorInput is the input shape for sofarpc_doctor. Service is optional:
// when set, doctor biases target resolution toward a per-service uniqueId.
// Profile selects the Target Profile to diagnose; empty inherits the session's
// Active Target Profile (when called by sessionId) and then defaultProfile.
type DoctorInput struct {
	Cwd                  string                       `json:"cwd,omitempty"`
	Project              string                       `json:"project,omitempty"`
	SessionID            string                       `json:"sessionId,omitempty"`
	Service              string                       `json:"service,omitempty"`
	Profile              string                       `json:"profile,omitempty"`
	InvocationProperties invocationprops.Declarations `json:"invocationProperties,omitempty"`
}
