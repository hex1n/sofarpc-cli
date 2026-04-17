package projectscan

type FacadeModule struct {
	Name            string `json:"name"`
	SourceRoot      string `json:"sourceRoot"`
	MavenModulePath string `json:"mavenModulePath"`
	JarGlob         string `json:"jarGlob"`
	DepsDir         string `json:"depsDir"`
}

type ProjectLayout struct {
	Root          string
	BuildTool     string
	FacadeModules []FacadeModule
}

type ArtifactSet struct {
	PrimaryJars    []string
	DependencyJars []string
}

type ServiceMatch struct {
	Module    FacadeModule
	MatchKind string
	MatchPath string
}
