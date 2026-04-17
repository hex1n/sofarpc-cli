package model

import (
	"encoding/json"

	"github.com/hex1n/sofarpc-cli/internal/targetmodel"
)

const (
	ModeDirect   = targetmodel.ModeDirect
	ModeRegistry = targetmodel.ModeRegistry

	PayloadRaw     = "raw"
	PayloadGeneric = "generic"
	PayloadSchema  = "schema"
)

type Manifest = targetmodel.Manifest
type ServiceConfig = targetmodel.ServiceConfig
type MethodConfig = targetmodel.MethodConfig
type ContextStore = targetmodel.ContextStore
type Context = targetmodel.Context
type TargetConfig = targetmodel.TargetConfig

type InvocationRequest struct {
	RequestID           string          `json:"requestId"`
	Action              string          `json:"action,omitempty"`
	Service             string          `json:"service"`
	Method              string          `json:"method"`
	ParamTypes          []string        `json:"paramTypes,omitempty"`
	ParamTypeSignatures []string        `json:"paramTypeSignatures,omitempty"`
	Args                json.RawMessage `json:"args"`
	PayloadMode         string          `json:"payloadMode"`
	Refresh             bool            `json:"refresh,omitempty"`
	Target              TargetConfig    `json:"target"`
}

type InvocationResponse struct {
	RequestID   string          `json:"requestId,omitempty"`
	OK          bool            `json:"ok"`
	Result      json.RawMessage `json:"result,omitempty"`
	Error       *RuntimeError   `json:"error,omitempty"`
	Diagnostics DiagnosticInfo  `json:"diagnostics,omitempty"`
}

type RuntimeError struct {
	Code             string `json:"code,omitempty"`
	Message          string `json:"message,omitempty"`
	Phase            string `json:"phase,omitempty"`
	TargetMode       string `json:"targetMode,omitempty"`
	ConfiguredTarget string `json:"configuredTarget,omitempty"`
	ResolvedTarget   string `json:"resolvedTarget,omitempty"`
	InvokeStyle      string `json:"invokeStyle,omitempty"`
	PayloadMode      string `json:"payloadMode,omitempty"`
	Retriable        bool   `json:"retriable,omitempty"`
	Hint             string `json:"hint,omitempty"`
}

type DiagnosticInfo struct {
	Phase            string   `json:"phase,omitempty"`
	TargetMode       string   `json:"targetMode,omitempty"`
	ConfiguredTarget string   `json:"configuredTarget,omitempty"`
	ResolvedTarget   string   `json:"resolvedTarget,omitempty"`
	InvokeStyle      string   `json:"invokeStyle,omitempty"`
	PayloadMode      string   `json:"payloadMode,omitempty"`
	ContractSource   string   `json:"contractSource,omitempty"`
	ContractCacheHit bool     `json:"contractCacheHit,omitempty"`
	ContractNotes    []string `json:"contractNotes,omitempty"`
	WorkerClasspath  string   `json:"workerClasspath,omitempty"`
	RuntimeVersion   string   `json:"runtimeVersion,omitempty"`
	RuntimeJar       string   `json:"runtimeJar,omitempty"`
	JavaBin          string   `json:"javaBin,omitempty"`
	JavaMajor        string   `json:"javaMajor,omitempty"`
	DaemonKey        string   `json:"daemonKey,omitempty"`
}

type DaemonMetadata struct {
	PID             int    `json:"pid"`
	Host            string `json:"host"`
	Port            int    `json:"port"`
	StartedAt       string `json:"startedAt"`
	RuntimeDigest   string `json:"runtimeDigest,omitempty"`
	RuntimeVersion  string `json:"runtimeVersion,omitempty"`
	JavaMajor       string `json:"javaMajor,omitempty"`
	ProtocolVersion string `json:"protocolVersion,omitempty"`
	DaemonProfile   string `json:"daemonProfile,omitempty"`
}

type DaemonRecord struct {
	Key          string          `json:"key"`
	Ready        bool            `json:"ready"`
	Stale        bool            `json:"stale,omitempty"`
	Metadata     *DaemonMetadata `json:"metadata,omitempty"`
	MetadataFile string          `json:"metadataFile,omitempty"`
	StdoutLog    string          `json:"stdoutLog,omitempty"`
	StderrLog    string          `json:"stderrLog,omitempty"`
	Error        string          `json:"error,omitempty"`
}

type DaemonAction struct {
	Daemon           DaemonRecord `json:"daemon"`
	SignaledProcess  bool         `json:"signaledProcess,omitempty"`
	RemovedMetadata  bool         `json:"removedMetadata,omitempty"`
	RemovedStdoutLog bool         `json:"removedStdoutLog,omitempty"`
	RemovedStderrLog bool         `json:"removedStderrLog,omitempty"`
}

type RuntimeRecord struct {
	Version      string `json:"version"`
	Path         string `json:"path"`
	Source       string `json:"source,omitempty"`
	Digest       string `json:"digest,omitempty"`
	InstalledAt  string `json:"installedAt,omitempty"`
	MetadataFile string `json:"metadataFile,omitempty"`
}

type RuntimeSourceStore struct {
	Active  string                   `json:"active,omitempty"`
	Sources map[string]RuntimeSource `json:"sources,omitempty"`
}

type RuntimeSource struct {
	Name string `json:"name,omitempty"`
	Kind string `json:"kind,omitempty"`
	Path string `json:"path,omitempty"`
}

type RuntimeSourceListReport struct {
	Active  string                   `json:"active,omitempty"`
	Sources map[string]RuntimeSource `json:"sources,omitempty"`
}

type DoctorReport struct {
	ManifestPath   string          `json:"manifestPath,omitempty"`
	ManifestLoaded bool            `json:"manifestLoaded"`
	ActiveContext  string          `json:"activeContext,omitempty"`
	Target         TargetConfig    `json:"target"`
	Runtime        RuntimeSnapshot `json:"runtime"`
	StubWarnings   []string        `json:"stubWarnings,omitempty"`
	Reachability   ProbeResult     `json:"reachability"`
	Daemon         DaemonSnapshot  `json:"daemon"`
	InvokeProbe    *InvokeProbe    `json:"invokeProbe,omitempty"`
}

type DescribeReport struct {
	Schema      *ServiceSchema `json:"schema,omitempty"`
	Error       string         `json:"error,omitempty"`
	Diagnostics DiagnosticInfo `json:"diagnostics,omitempty"`
}

type RuntimeSnapshot struct {
	SofaRPCVersion       string   `json:"sofaRpcVersion,omitempty"`
	SofaRPCVersionSource string   `json:"sofaRpcVersionSource,omitempty"`
	ContractSource       string   `json:"contractSource,omitempty"`
	ContractCacheHit     bool     `json:"contractCacheHit,omitempty"`
	ContractNotes        []string `json:"contractNotes,omitempty"`
	WorkerClasspath      string   `json:"workerClasspath,omitempty"`
	RuntimeJar           string   `json:"runtimeJar,omitempty"`
	JavaBin              string   `json:"javaBin,omitempty"`
	JavaMajor            string   `json:"javaMajor,omitempty"`
	DaemonKey            string   `json:"daemonKey,omitempty"`
}

type ProbeResult struct {
	Reachable bool   `json:"reachable"`
	Target    string `json:"target,omitempty"`
	Message   string `json:"message,omitempty"`
}

type DaemonSnapshot struct {
	Ready    bool            `json:"ready"`
	Error    string          `json:"error,omitempty"`
	Metadata *DaemonMetadata `json:"metadata,omitempty"`
}

type InvokeProbe struct {
	Attempted      bool          `json:"attempted"`
	Reachable      bool          `json:"reachable"`
	TransportError string        `json:"transportError,omitempty"`
	Error          *RuntimeError `json:"error,omitempty"`
}

type ServiceSchema struct {
	Service string         `json:"service"`
	Methods []MethodSchema `json:"methods"`
}

type MethodSchema struct {
	Name                string   `json:"name"`
	ParamTypes          []string `json:"paramTypes"`
	ParamTypeSignatures []string `json:"paramTypeSignatures,omitempty"`
	ReturnType          string   `json:"returnType,omitempty"`
}
