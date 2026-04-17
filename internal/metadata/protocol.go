package metadata

import (
	"encoding/json"

	"github.com/hex1n/sofarpc-cli/internal/contract"
	"github.com/hex1n/sofarpc-cli/internal/model"
)

const (
	protocolVersion = "1"
	actionSchema    = "schema"
	actionMethod    = "method"
	actionInfo      = "info"
	actionShutdown  = "shutdown"
)

type daemonMetadata struct {
	PID              int    `json:"pid"`
	Host             string `json:"host"`
	Port             int    `json:"port"`
	StartedAt        string `json:"startedAt"`
	ProtocolVersion  string `json:"protocolVersion,omitempty"`
	Executable       string `json:"executable,omitempty"`
	ExecutableDigest string `json:"executableDigest,omitempty"`
	CacheTTL         string `json:"cacheTtl,omitempty"`
}

type resolveRequest struct {
	Action              string          `json:"action"`
	ProjectRoot         string          `json:"projectRoot"`
	Service             string          `json:"service"`
	Method              string          `json:"method,omitempty"`
	PreferredParamTypes []string        `json:"preferredParamTypes,omitempty"`
	RawArgs             json.RawMessage `json:"rawArgs,omitempty"`
	Refresh             bool            `json:"refresh,omitempty"`
}

type resolveResponse struct {
	OK       bool                    `json:"ok"`
	Error    string                  `json:"error,omitempty"`
	Source   string                  `json:"source,omitempty"`
	CacheHit bool                    `json:"cacheHit,omitempty"`
	Notes    []string                `json:"notes,omitempty"`
	Schema   *model.ServiceSchema    `json:"schema,omitempty"`
	Method   *contract.ProjectMethod `json:"method,omitempty"`
	Metadata *daemonMetadata         `json:"metadata,omitempty"`
}
