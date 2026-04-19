package shared

import "github.com/hex1n/sofarpc-cli/internal/model"

type InvocationInputs struct {
	ManifestPath     string
	ContextName      string
	Service          string
	Method           string
	TypesCSV         string
	ArgsJSON         string
	PayloadMode      string
	DirectURL        string
	RegistryAddress  string
	RegistryProtocol string
	Protocol         string
	Serialization    string
	UniqueID         string
	TimeoutMS        int
	ConnectTimeoutMS int
	StubPathCSV      string
	SofaRPCVersion   string
	JavaBin          string
	RuntimeJar       string
	RefreshContract  bool
}

type ResolvedInvocation struct {
	Request              model.InvocationRequest
	ManifestPath         string
	ManifestFound        bool
	ActiveContext        string
	SofaRPCVersion       string
	SofaRPCVersionSource string
	JavaBin              string
	RuntimeJar           string
	StubPaths            []string
}

type LocalSchemaResolution struct {
	Schema   model.ServiceSchema
	Source   string
	CacheHit bool
	Notes    []string
}

type ServiceSchemaResolution struct {
	Schema   model.ServiceSchema
	Source   string
	CacheHit bool
	Notes    []string
}
