package model

type WorkspaceCapabilities struct {
	Manifest      bool `json:"manifest"`
	ContextStore  bool `json:"contextStore"`
	LocalContract bool `json:"localContract"`
	FacadeConfig  bool `json:"facadeConfig"`
	FacadeIndex   bool `json:"facadeIndex"`
	Runtime       bool `json:"runtime"`
	Metadata      bool `json:"metadata"`
}

type WorkspaceResolvedTarget struct {
	Service      string       `json:"service,omitempty"`
	ContextName  string       `json:"contextName,omitempty"`
	Target       TargetConfig `json:"target"`
	Reachability ProbeResult  `json:"reachability"`
	ResolvedAt   string       `json:"resolvedAt,omitempty"`
}

type WorkspaceInvocationPlan struct {
	Service          string                   `json:"service,omitempty"`
	Method           string                   `json:"method,omitempty"`
	Request          InvocationRequest        `json:"request"`
	Spec             WorkspaceRuntimePlanSpec `json:"spec"`
	Runtime          RuntimeSnapshot          `json:"runtime"`
	WrappedSingleArg bool                     `json:"wrappedSingleArg,omitempty"`
	PlannedAt        string                   `json:"plannedAt,omitempty"`
}

type WorkspaceRuntimePlanSpec struct {
	SofaRPCVersion string   `json:"sofaRpcVersion,omitempty"`
	JavaBin        string   `json:"javaBin,omitempty"`
	JavaMajor      string   `json:"javaMajor,omitempty"`
	RuntimeJar     string   `json:"runtimeJar,omitempty"`
	RuntimeDigest  string   `json:"runtimeDigest,omitempty"`
	DaemonProfile  string   `json:"daemonProfile,omitempty"`
	StubPaths      []string `json:"stubPaths,omitempty"`
	ClasspathHash  string   `json:"classpathHash,omitempty"`
	DaemonKey      string   `json:"daemonKey,omitempty"`
	MetadataFile   string   `json:"metadataFile,omitempty"`
	StdoutLog      string   `json:"stdoutLog,omitempty"`
	StderrLog      string   `json:"stderrLog,omitempty"`
}

type WorkspaceMethodOverload struct {
	ParamTypes          []string `json:"paramTypes,omitempty"`
	ParamTypeSignatures []string `json:"paramTypeSignatures,omitempty"`
	ReturnType          string   `json:"returnType,omitempty"`
}

type WorkspaceMethodDescription struct {
	Service     string                    `json:"service,omitempty"`
	Method      string                    `json:"method,omitempty"`
	Overloads   []WorkspaceMethodOverload `json:"overloads,omitempty"`
	Selected    *WorkspaceMethodOverload  `json:"selected,omitempty"`
	Diagnostics DiagnosticInfo            `json:"diagnostics,omitempty"`
	DescribedAt string                    `json:"describedAt,omitempty"`
}

type WorkspaceSession struct {
	ID               string                      `json:"id"`
	ProjectRoot      string                      `json:"projectRoot,omitempty"`
	ManifestPath     string                      `json:"manifestPath,omitempty"`
	ManifestLoaded   bool                        `json:"manifestLoaded"`
	ActiveContext    string                      `json:"activeContext,omitempty"`
	DefaultContext   string                      `json:"defaultContext,omitempty"`
	SofaRPCVersion   string                      `json:"sofaRpcVersion,omitempty"`
	FacadeConfigured bool                        `json:"facadeConfigured,omitempty"`
	FacadeIndexPath  string                      `json:"facadeIndexPath,omitempty"`
	Capabilities     WorkspaceCapabilities       `json:"capabilities"`
	LastTarget       *WorkspaceResolvedTarget    `json:"lastTarget,omitempty"`
	LastDescribe     *WorkspaceMethodDescription `json:"lastDescribe,omitempty"`
	LastPlan         *WorkspaceInvocationPlan    `json:"lastPlan,omitempty"`
	Notes            []string                    `json:"notes,omitempty"`
	CreatedAt        string                      `json:"createdAt,omitempty"`
	UpdatedAt        string                      `json:"updatedAt,omitempty"`
}
