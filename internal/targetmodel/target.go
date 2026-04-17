package targetmodel

const (
	ModeDirect   = "direct"
	ModeRegistry = "registry"
)

type Manifest struct {
	SchemaVersion  string                   `json:"schemaVersion,omitempty"`
	SofaRPCVersion string                   `json:"sofaRpcVersion,omitempty"`
	DefaultContext string                   `json:"defaultContext,omitempty"`
	DefaultTarget  TargetConfig             `json:"defaultTarget,omitempty"`
	StubPaths      []string                 `json:"stubPaths,omitempty"`
	Services       map[string]ServiceConfig `json:"services,omitempty"`
}

type ServiceConfig struct {
	UniqueID string                  `json:"uniqueId,omitempty"`
	Methods  map[string]MethodConfig `json:"methods,omitempty"`
}

type MethodConfig struct {
	ParamTypes  []string `json:"paramTypes,omitempty"`
	PayloadMode string   `json:"payloadMode,omitempty"`
}

type ContextStore struct {
	Active   string             `json:"active,omitempty"`
	Contexts map[string]Context `json:"contexts,omitempty"`
}

type Context struct {
	Name             string `json:"name,omitempty"`
	Mode             string `json:"mode,omitempty"`
	DirectURL        string `json:"directUrl,omitempty"`
	RegistryAddress  string `json:"registryAddress,omitempty"`
	RegistryProtocol string `json:"registryProtocol,omitempty"`
	Protocol         string `json:"protocol,omitempty"`
	Serialization    string `json:"serialization,omitempty"`
	UniqueID         string `json:"uniqueId,omitempty"`
	TimeoutMS        int    `json:"timeoutMs,omitempty"`
	ConnectTimeoutMS int    `json:"connectTimeoutMs,omitempty"`
	ProjectRoot      string `json:"projectRoot,omitempty"`
}

type TargetConfig struct {
	Mode             string `json:"mode,omitempty"`
	DirectURL        string `json:"directUrl,omitempty"`
	RegistryAddress  string `json:"registryAddress,omitempty"`
	RegistryProtocol string `json:"registryProtocol,omitempty"`
	Protocol         string `json:"protocol,omitempty"`
	Serialization    string `json:"serialization,omitempty"`
	UniqueID         string `json:"uniqueId,omitempty"`
	TimeoutMS        int    `json:"timeoutMs,omitempty"`
	ConnectTimeoutMS int    `json:"connectTimeoutMs,omitempty"`
}
