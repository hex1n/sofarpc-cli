// Package worker is the Go-side driver for the long-running Java invoke
// worker described in docs/architecture.md §7. It owns: profile keying,
// the line-delimited JSON wire protocol, process supervision, and the
// per-profile pool the MCP layer calls into.
//
// The package is split so each piece can be tested in isolation:
//
//	wire.go       request / response / error shapes (this file)
//	profile.go    daemon-key derivation (deterministic hashes)
//	conn.go       single TCP connection — line-delimited JSON
//	process.go    spawn + ready-handshake + lifecycle
//	pool.go       per-profile pool with lazy spawn
//	client.go     the surface the MCP handlers consume
//
// The worker JVM jar itself lives outside this repository (see
// runtime-worker-java/), but the Go side is testable end-to-end against
// a fake TCP server, so this package can ship before the jar does.
package worker

import "github.com/hex1n/sofarpc-cli/internal/core/target"

// Request is what the Go side writes to the worker's TCP socket as one
// JSON line. RequestID lets the worker correlate responses; the Go
// client guarantees uniqueness per connection.
//
// See architecture §7.2.
type Request struct {
	RequestID   string         `json:"requestId"`
	Action      string         `json:"action"`
	Service     string         `json:"service,omitempty"`
	Method      string         `json:"method,omitempty"`
	ParamTypes  []string       `json:"paramTypes,omitempty"`
	Args        []any          `json:"args,omitempty"`
	Classloader *ClassloaderID `json:"classloader,omitempty"`
	Target      *target.Config `json:"target,omitempty"`
}

// ClassloaderID names the per-request URLClassLoader the worker should
// use. ID is the deterministic hash of sorted StubJars; the worker
// caches the constructed loader by ID with a TTL (architecture §7.1).
type ClassloaderID struct {
	ID       string   `json:"id"`
	StubJars []string `json:"stubJars,omitempty"`
}

// Response is the JSON line the worker writes back. Exactly one of
// Result and Error is meaningful; Ok=false implies Error is set.
type Response struct {
	RequestID   string         `json:"requestId"`
	Ok          bool           `json:"ok"`
	Result      any            `json:"result,omitempty"`
	Error       *WireError     `json:"error,omitempty"`
	Diagnostics map[string]any `json:"diagnostics,omitempty"`
}

// WireError is the JSON shape the worker uses for failures. We mirror
// errcode.Error's wire layout so the MCP layer can lift these into an
// errcode.Error without remapping every field.
type WireError struct {
	Code    string         `json:"code"`
	Message string         `json:"message"`
	Phase   string         `json:"phase,omitempty"`
	Hint    map[string]any `json:"hint,omitempty"`
}

// ReadyMessage is the one-shot handshake the worker prints on stdout
// before flipping into TCP-only line-JSON mode. The Go side parses it
// with json.Unmarshal on the first non-empty stdout line.
//
// See architecture §7.4.
type ReadyMessage struct {
	Ready bool   `json:"ready"`
	Port  int    `json:"port"`
	PID   int    `json:"pid"`
	Note  string `json:"note,omitempty"`
}

// Common action verbs the worker recognises.
const (
	ActionInvoke   = "invoke"
	ActionShutdown = "shutdown"
	ActionPing     = "ping"
	// ActionDescribe asks the worker to reflect on a class loaded from
	// the facade classpath and return its facadesemantic.Class shape.
	// Request carries `service` (the FQN to describe); Response.Result
	// is the JSON-marshalled facadesemantic.Class. Missing classes
	// surface as WireError with code "contract.unresolvable" so the
	// Go-side Store adapter can translate to ok=false.
	//
	// This lets sofarpc_describe work without a local Spoon index —
	// the worker reflects on the same classes it would otherwise invoke,
	// so describe and invoke see exactly the same wire shape.
	ActionDescribe = "describe"
)
