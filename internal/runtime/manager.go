package runtime

import (
	"path/filepath"

	"github.com/hex1n/sofarpc-cli/internal/config"
)

const (
	defaultRuntimeVersion = "5.7.6"
	defaultTimeoutMS      = 3000
	defaultConnectMS      = 1000
	mainClass             = "com.hex1n.sofarpc.worker.WorkerMain"
)

type Manager struct {
	Paths config.Paths
	Cwd   string
}

type Spec struct {
	SofaRPCVersion string
	JavaBin        string
	JavaMajor      string
	RuntimeJar     string
	RuntimeDigest  string
	StubPaths      []string
	ClasspathHash  string
	DaemonKey      string
	MetadataFile   string
	StdoutLog      string
	StderrLog      string
}

func NewManager(paths config.Paths, cwd string) *Manager {
	return &Manager{Paths: paths, Cwd: cwd}
}

func (m *Manager) DaemonDir() string {
	return filepath.Join(m.Paths.CacheDir, "daemons")
}

func (m *Manager) RuntimeDir() string {
	return filepath.Join(m.Paths.CacheDir, "runtimes")
}

func (m *Manager) SchemaDir() string {
	return filepath.Join(m.Paths.CacheDir, "schemas")
}

func (m *Manager) ResolveSpec(javaBin, runtimeJar, version string, stubPaths []string) (Spec, error) {
	if javaBin == "" {
		javaBin = "java"
	}
	if version == "" {
		version = defaultRuntimeVersion
	}
	if runtimeJar == "" {
		resolved, err := m.EnsureRuntimeAvailable(version)
		if err != nil {
			return Spec{}, err
		}
		runtimeJar = resolved
	}
	runtimeJar, err := filepath.Abs(runtimeJar)
	if err != nil {
		return Spec{}, err
	}
	javaMajor, err := detectJavaMajor(javaBin)
	if err != nil {
		return Spec{}, err
	}
	digest, err := fileDigest(runtimeJar)
	if err != nil {
		return Spec{}, err
	}
	normalized, err := normalizePaths(stubPaths)
	if err != nil {
		return Spec{}, err
	}
	classpathHash := hashStrings(normalized)
	key := hashStrings([]string{version, digest, classpathHash, javaMajor})
	daemonDir := m.DaemonDir()
	return Spec{
		SofaRPCVersion: version,
		JavaBin:        javaBin,
		JavaMajor:      javaMajor,
		RuntimeJar:     runtimeJar,
		RuntimeDigest:  digest,
		StubPaths:      normalized,
		ClasspathHash:  classpathHash,
		DaemonKey:      key,
		MetadataFile:   filepath.Join(daemonDir, key+".json"),
		StdoutLog:      filepath.Join(daemonDir, key+".stdout.log"),
		StderrLog:      filepath.Join(daemonDir, key+".stderr.log"),
	}, nil
}
