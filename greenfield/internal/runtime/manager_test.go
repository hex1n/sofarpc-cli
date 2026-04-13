package runtime

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/hex1n/sofa-rpcctl/greenfield/internal/config"
	"github.com/hex1n/sofa-rpcctl/greenfield/internal/model"
)

func TestHashStringsChangesWithOrder(t *testing.T) {
	left := hashStrings([]string{"a", "b"})
	right := hashStrings([]string{"b", "a"})
	if left == right {
		t.Fatal("expected classpath hash to preserve order")
	}
}

func TestScanStubWarnings(t *testing.T) {
	warnings := ScanStubWarnings([]string{
		"/tmp/guava-32.1.2.jar",
		"/tmp/user-api.jar",
	})
	if len(warnings) != 1 {
		t.Fatalf("expected one warning, got %d", len(warnings))
	}
}

func TestListDaemonsMarksReadyAndStale(t *testing.T) {
	manager := testManager(t)

	readyListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen() error = %v", err)
	}
	defer readyListener.Close()
	readyHost, readyPort := splitListenerAddress(t, readyListener.Addr().String())
	writeDaemonMetadata(t, manager, "ready", model.DaemonMetadata{
		PID:             111,
		Host:            readyHost,
		Port:            readyPort,
		StartedAt:       "2026-04-13T08:00:00Z",
		RuntimeVersion:  "5.7.6",
		ProtocolVersion: "v1",
	})

	staleListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen() error = %v", err)
	}
	staleHost, stalePort := splitListenerAddress(t, staleListener.Addr().String())
	_ = staleListener.Close()
	writeDaemonMetadata(t, manager, "stale", model.DaemonMetadata{
		PID:             222,
		Host:            staleHost,
		Port:            stalePort,
		StartedAt:       "2026-04-13T07:59:00Z",
		RuntimeVersion:  "5.7.6",
		ProtocolVersion: "v1",
	})

	records, err := manager.ListDaemons()
	if err != nil {
		t.Fatalf("ListDaemons() error = %v", err)
	}
	if len(records) != 2 {
		t.Fatalf("expected 2 daemon records, got %d", len(records))
	}
	got := map[string]model.DaemonRecord{}
	for _, record := range records {
		got[record.Key] = record
	}
	if !got["ready"].Ready || got["ready"].Stale {
		t.Fatalf("expected ready daemon to be reachable, got %+v", got["ready"])
	}
	if got["stale"].Ready || !got["stale"].Stale {
		t.Fatalf("expected stale daemon to be unreachable, got %+v", got["stale"])
	}
}

func TestPruneDaemonsRemovesOnlyStaleEntries(t *testing.T) {
	manager := testManager(t)

	readyListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen() error = %v", err)
	}
	defer readyListener.Close()
	readyHost, readyPort := splitListenerAddress(t, readyListener.Addr().String())
	writeDaemonMetadata(t, manager, "ready", model.DaemonMetadata{
		PID:             333,
		Host:            readyHost,
		Port:            readyPort,
		StartedAt:       "2026-04-13T08:00:00Z",
		RuntimeVersion:  "5.7.6",
		ProtocolVersion: "v1",
	})

	staleListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen() error = %v", err)
	}
	staleHost, stalePort := splitListenerAddress(t, staleListener.Addr().String())
	_ = staleListener.Close()
	writeDaemonMetadata(t, manager, "stale", model.DaemonMetadata{
		PID:             444,
		Host:            staleHost,
		Port:            stalePort,
		StartedAt:       "2026-04-13T07:59:00Z",
		RuntimeVersion:  "5.7.6",
		ProtocolVersion: "v1",
	})
	writeTextFile(t, filepath.Join(manager.DaemonDir(), "stale.stdout.log"), "stdout")
	writeTextFile(t, filepath.Join(manager.DaemonDir(), "stale.stderr.log"), "stderr")

	actions, err := manager.PruneDaemons()
	if err != nil {
		t.Fatalf("PruneDaemons() error = %v", err)
	}
	if len(actions) != 1 {
		t.Fatalf("expected 1 pruned daemon, got %d", len(actions))
	}
	if actions[0].Daemon.Key != "stale" {
		t.Fatalf("expected stale daemon to be pruned, got %+v", actions[0])
	}
	assertMissing(t, filepath.Join(manager.DaemonDir(), "stale.json"))
	assertMissing(t, filepath.Join(manager.DaemonDir(), "stale.stdout.log"))
	assertMissing(t, filepath.Join(manager.DaemonDir(), "stale.stderr.log"))
	assertExists(t, filepath.Join(manager.DaemonDir(), "ready.json"))
}

func TestStopDaemonKillsReachableLoopbackProcess(t *testing.T) {
	manager := testManager(t)
	addressFile := filepath.Join(t.TempDir(), "addr.txt")
	cmd := exec.Command(os.Args[0], "-test.run=TestHelperLoopbackDaemonProcess", "--", addressFile)
	cmd.Env = append(os.Environ(), "GO_WANT_HELPER_DAEMON=1")
	if err := cmd.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	defer func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
	}()

	address := waitForAddressFile(t, addressFile)
	host, port := splitListenerAddress(t, address)
	waitForReachable(t, host, port)
	writeDaemonMetadata(t, manager, "live", model.DaemonMetadata{
		PID:             cmd.Process.Pid,
		Host:            host,
		Port:            port,
		StartedAt:       "2026-04-13T08:01:00Z",
		RuntimeVersion:  "5.7.6",
		ProtocolVersion: "v1",
	})

	action, err := manager.StopDaemon("live")
	if err != nil {
		t.Fatalf("StopDaemon() error = %v", err)
	}
	if !action.SignaledProcess || !action.RemovedMetadata {
		t.Fatalf("expected daemon stop to signal process and remove metadata, got %+v", action)
	}
	assertMissing(t, filepath.Join(manager.DaemonDir(), "live.json"))

	waitDone := make(chan error, 1)
	go func() {
		waitDone <- cmd.Wait()
	}()
	select {
	case <-time.After(3 * time.Second):
		t.Fatal("expected helper daemon process to exit after stop")
	case <-waitDone:
	}
}

func TestInstallRuntimeCopiesJarAndWritesMetadata(t *testing.T) {
	manager := testManager(t)
	sourceJar := filepath.Join(t.TempDir(), "worker.jar")
	writeTextFile(t, sourceJar, "jar-bits")

	record, err := manager.InstallRuntime("5.7.6", sourceJar)
	if err != nil {
		t.Fatalf("InstallRuntime() error = %v", err)
	}
	assertExists(t, record.Path)
	assertExists(t, record.MetadataFile)
	if record.Version != "5.7.6" {
		t.Fatalf("expected version 5.7.6, got %+v", record)
	}
	if record.Digest == "" {
		t.Fatalf("expected runtime digest to be populated, got %+v", record)
	}
	if record.Source != "user-jar" {
		t.Fatalf("expected explicit install source to be user-jar, got %+v", record)
	}
}

func TestListRuntimesFindsInstalledJar(t *testing.T) {
	manager := testManager(t)
	targetJar := manager.installedRuntimeJar("5.7.6")
	if err := os.MkdirAll(filepath.Dir(targetJar), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	writeTextFile(t, targetJar, "jar-bits")

	records, err := manager.ListRuntimes()
	if err != nil {
		t.Fatalf("ListRuntimes() error = %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("expected 1 installed runtime, got %d", len(records))
	}
	if records[0].Version != "5.7.6" || records[0].Path != targetJar {
		t.Fatalf("unexpected runtime record %+v", records[0])
	}
}

func TestGetRuntimeReturnsInstalledRecord(t *testing.T) {
	manager := testManager(t)
	sourceJar := filepath.Join(t.TempDir(), "worker.jar")
	writeTextFile(t, sourceJar, "jar-bits")
	if _, err := manager.InstallRuntime("5.7.6", sourceJar); err != nil {
		t.Fatalf("InstallRuntime() error = %v", err)
	}

	record, err := manager.GetRuntime("5.7.6")
	if err != nil {
		t.Fatalf("GetRuntime() error = %v", err)
	}
	if record.Version != "5.7.6" || record.Path == "" {
		t.Fatalf("unexpected runtime record %+v", record)
	}
}

func TestEnsureRuntimeAvailableReturnsInstalledJar(t *testing.T) {
	manager := testManager(t)
	targetJar := manager.installedRuntimeJar("5.7.6")
	if err := os.MkdirAll(filepath.Dir(targetJar), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	writeTextFile(t, targetJar, "jar-bits")

	got, err := manager.EnsureRuntimeAvailable("5.7.6")
	if err != nil {
		t.Fatalf("EnsureRuntimeAvailable() error = %v", err)
	}
	if got != targetJar {
		t.Fatalf("expected installed runtime jar %q, got %q", targetJar, got)
	}
}

func TestEnsureRuntimeAvailableAutoInstallsFromBundled(t *testing.T) {
	manager := testManager(t)
	bundledJar := filepath.Join(manager.Cwd, "runtime-worker-java", "target", "rpc-runtime-worker-sofa-5.7.6.jar")
	if err := os.MkdirAll(filepath.Dir(bundledJar), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	writeTextFile(t, bundledJar, "bundled-jar")

	got, err := manager.EnsureRuntimeAvailable("5.7.6")
	if err != nil {
		t.Fatalf("EnsureRuntimeAvailable() error = %v", err)
	}
	if got != manager.installedRuntimeJar("5.7.6") {
		t.Fatalf("expected cached runtime path, got %q", got)
	}
	assertExists(t, got)
}

func TestEnsureRuntimeAvailableErrorsWhenNothingAvailable(t *testing.T) {
	manager := testManager(t)

	if _, err := manager.EnsureRuntimeAvailable("5.7.6"); err == nil {
		t.Fatal("expected error when runtime is missing and no source is configured")
	}
}

func TestInstallRuntimeUsesBundledCandidateWhenJarOmitted(t *testing.T) {
	manager := testManager(t)
	bundledJar := filepath.Join(manager.Cwd, "runtime-worker-java", "target", "rpc-runtime-worker-sofa-5.7.6.jar")
	if err := os.MkdirAll(filepath.Dir(bundledJar), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	writeTextFile(t, bundledJar, "bundled-jar")

	record, err := manager.InstallRuntime("5.7.6", "")
	if err != nil {
		t.Fatalf("InstallRuntime() error = %v", err)
	}
	if record.Source != "workspace-bundled" {
		t.Fatalf("expected bundled install source, got %+v", record)
	}
	if record.Path != manager.installedRuntimeJar("5.7.6") {
		t.Fatalf("expected cached runtime path, got %+v", record)
	}
}

func TestInstallRuntimeUsesActiveNamedSourceWhenConfigured(t *testing.T) {
	manager := testManager(t)
	sourceDir := filepath.Join(t.TempDir(), "source")
	bundledJar := filepath.Join(sourceDir, "5.7.6", "rpc-runtime-worker-sofa-5.7.6.jar")
	if err := os.MkdirAll(filepath.Dir(bundledJar), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	writeTextFile(t, bundledJar, "source-jar")
	if err := config.SaveRuntimeSourceStore(manager.Paths, model.RuntimeSourceStore{
		Active: "local",
		Sources: map[string]model.RuntimeSource{
			"local": {
				Name: "local",
				Kind: "directory",
				Path: sourceDir,
			},
		},
	}); err != nil {
		t.Fatalf("SaveRuntimeSourceStore() error = %v", err)
	}

	record, err := manager.InstallRuntime("5.7.6", "")
	if err != nil {
		t.Fatalf("InstallRuntime() error = %v", err)
	}
	if record.Source != "source:local" {
		t.Fatalf("expected active source install, got %+v", record)
	}
}

func TestInstallRuntimeUsesNamedSourceOverride(t *testing.T) {
	manager := testManager(t)
	sourceDir := filepath.Join(t.TempDir(), "source")
	bundledJar := filepath.Join(sourceDir, "rpc-runtime-worker-sofa-5.7.6.jar")
	if err := os.MkdirAll(filepath.Dir(bundledJar), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	writeTextFile(t, bundledJar, "source-jar")
	if err := config.SaveRuntimeSourceStore(manager.Paths, model.RuntimeSourceStore{
		Sources: map[string]model.RuntimeSource{
			"override": {
				Name: "override",
				Kind: "directory",
				Path: sourceDir,
			},
		},
	}); err != nil {
		t.Fatalf("SaveRuntimeSourceStore() error = %v", err)
	}

	record, err := manager.InstallRuntimeFrom("5.7.6", "override", "")
	if err != nil {
		t.Fatalf("InstallRuntimeFrom() error = %v", err)
	}
	if record.Source != "source:override" {
		t.Fatalf("expected named source install, got %+v", record)
	}
}

func TestInstallRuntimeDownloadsFromURLTemplateSource(t *testing.T) {
	manager := testManager(t)
	jarBody := []byte("remote-jar")
	hash := sha256.Sum256(jarBody)
	digest := hex.EncodeToString(hash[:])
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/runtime/5.7.6/rpc-runtime-worker-sofa-5.7.6.jar":
			_, _ = w.Write(jarBody)
		case "/runtime/5.7.6/rpc-runtime-worker-sofa-5.7.6.jar.sha256":
			_, _ = w.Write([]byte(digest + "  rpc-runtime-worker-sofa-5.7.6.jar\n"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	if err := config.SaveRuntimeSourceStore(manager.Paths, model.RuntimeSourceStore{
		Sources: map[string]model.RuntimeSource{
			"remote": {
				Name:      "remote",
				Kind:      "url-template",
				Path:      server.URL + "/runtime/{version}/rpc-runtime-worker-sofa-{version}.jar",
				SHA256URL: server.URL + "/runtime/{version}/rpc-runtime-worker-sofa-{version}.jar.sha256",
			},
		},
	}); err != nil {
		t.Fatalf("SaveRuntimeSourceStore() error = %v", err)
	}

	record, err := manager.InstallRuntimeFrom("5.7.6", "remote", "")
	if err != nil {
		t.Fatalf("InstallRuntimeFrom() error = %v", err)
	}
	if record.Source != "source:remote" {
		t.Fatalf("expected url-template install source, got %+v", record)
	}
	body, err := os.ReadFile(record.Path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if string(body) != "remote-jar" {
		t.Fatalf("expected downloaded runtime contents, got %q", string(body))
	}
	leftovers, err := filepath.Glob(filepath.Join(manager.runtimeVersionDir("5.7.6"), "download-*.jar"))
	if err != nil {
		t.Fatalf("Glob() error = %v", err)
	}
	if len(leftovers) != 0 {
		t.Fatalf("expected temporary downloads to be cleaned up, got %v", leftovers)
	}
}

func TestInstallRuntimeFailsOnSHA256Mismatch(t *testing.T) {
	manager := testManager(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/runtime/5.7.6/rpc-runtime-worker-sofa-5.7.6.jar":
			_, _ = w.Write([]byte("remote-jar"))
		case "/runtime/5.7.6/rpc-runtime-worker-sofa-5.7.6.jar.sha256":
			_, _ = w.Write([]byte(strings.Repeat("0", 64)))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	if err := config.SaveRuntimeSourceStore(manager.Paths, model.RuntimeSourceStore{
		Sources: map[string]model.RuntimeSource{
			"remote": {
				Name:      "remote",
				Kind:      "url-template",
				Path:      server.URL + "/runtime/{version}/rpc-runtime-worker-sofa-{version}.jar",
				SHA256URL: server.URL + "/runtime/{version}/rpc-runtime-worker-sofa-{version}.jar.sha256",
			},
		},
	}); err != nil {
		t.Fatalf("SaveRuntimeSourceStore() error = %v", err)
	}

	_, err := manager.InstallRuntimeFrom("5.7.6", "remote", "")
	if err == nil {
		t.Fatal("expected checksum mismatch to fail installation")
	}
	if !strings.Contains(err.Error(), "checksum mismatch") {
		t.Fatalf("expected checksum mismatch error, got %v", err)
	}
	leftovers, globErr := filepath.Glob(filepath.Join(manager.runtimeVersionDir("5.7.6"), "download-*.jar"))
	if globErr != nil {
		t.Fatalf("Glob() error = %v", globErr)
	}
	if len(leftovers) != 0 {
		t.Fatalf("expected failed downloads to be cleaned up, got %v", leftovers)
	}
}

func TestInstallRuntimeDownloadsFromManifestURLSource(t *testing.T) {
	manager := testManager(t)
	jarBody := []byte("manifest-jar")
	hash := sha256.Sum256(jarBody)
	digest := hex.EncodeToString(hash[:])
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/catalog/runtime.json":
			_, _ = w.Write([]byte(`{
  "schemaVersion": "v1alpha1",
  "versions": {
    "5.7.6": {
      "url": "` + server.URL + `/artifacts/rpc-runtime-worker-sofa-{version}.jar",
      "sha256": "` + digest + `"
    }
  }
}`))
		case "/artifacts/rpc-runtime-worker-sofa-5.7.6.jar":
			_, _ = w.Write(jarBody)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	if err := config.SaveRuntimeSourceStore(manager.Paths, model.RuntimeSourceStore{
		Sources: map[string]model.RuntimeSource{
			"catalog": {
				Name: "catalog",
				Kind: "manifest-url",
				Path: server.URL + "/catalog/runtime.json",
			},
		},
	}); err != nil {
		t.Fatalf("SaveRuntimeSourceStore() error = %v", err)
	}

	record, err := manager.InstallRuntimeFrom("5.7.6", "catalog", "")
	if err != nil {
		t.Fatalf("InstallRuntimeFrom() error = %v", err)
	}
	if record.Source != "source:catalog" {
		t.Fatalf("expected manifest-url install source, got %+v", record)
	}
	body, err := os.ReadFile(record.Path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if string(body) != "manifest-jar" {
		t.Fatalf("expected downloaded runtime contents, got %q", string(body))
	}
}

func TestInstallRuntimeFailsWhenManifestURLSourceMissingVersion(t *testing.T) {
	manager := testManager(t)
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/catalog/runtime.json":
			_, _ = w.Write([]byte(`{
  "schemaVersion": "v1alpha1",
  "versions": {
    "5.4.0": {
      "url": "` + server.URL + `/artifacts/rpc-runtime-worker-sofa-5.4.0.jar"
    }
  }
}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	if err := config.SaveRuntimeSourceStore(manager.Paths, model.RuntimeSourceStore{
		Sources: map[string]model.RuntimeSource{
			"catalog": {
				Name: "catalog",
				Kind: "manifest-url",
				Path: server.URL + "/catalog/runtime.json",
			},
		},
	}); err != nil {
		t.Fatalf("SaveRuntimeSourceStore() error = %v", err)
	}

	_, err := manager.InstallRuntimeFrom("5.7.6", "catalog", "")
	if err == nil {
		t.Fatal("expected missing manifest version to fail installation")
	}
	if !strings.Contains(err.Error(), `does not define version "5.7.6"`) {
		t.Fatalf("expected missing version error, got %v", err)
	}
}

func TestInstallRuntimeFailsWhenManifestURLChecksumMismatch(t *testing.T) {
	manager := testManager(t)
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/catalog/runtime.json":
			_, _ = w.Write([]byte(`{
  "schemaVersion": "v1alpha1",
  "versions": {
    "5.7.6": {
      "url": "` + server.URL + `/artifacts/rpc-runtime-worker-sofa-5.7.6.jar",
      "sha256": "` + strings.Repeat("0", 64) + `"
    }
  }
}`))
		case "/artifacts/rpc-runtime-worker-sofa-5.7.6.jar":
			_, _ = w.Write([]byte("manifest-jar"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	if err := config.SaveRuntimeSourceStore(manager.Paths, model.RuntimeSourceStore{
		Sources: map[string]model.RuntimeSource{
			"catalog": {
				Name: "catalog",
				Kind: "manifest-url",
				Path: server.URL + "/catalog/runtime.json",
			},
		},
	}); err != nil {
		t.Fatalf("SaveRuntimeSourceStore() error = %v", err)
	}

	_, err := manager.InstallRuntimeFrom("5.7.6", "catalog", "")
	if err == nil {
		t.Fatal("expected manifest checksum mismatch to fail installation")
	}
	if !strings.Contains(err.Error(), "checksum mismatch") {
		t.Fatalf("expected checksum mismatch error, got %v", err)
	}
}

func TestValidateRuntimeSourceReportsURLTemplateReachability(t *testing.T) {
	manager := testManager(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/runtime/5.7.6/rpc-runtime-worker-sofa-5.7.6.jar":
			w.WriteHeader(http.StatusOK)
		case "/runtime/5.7.6/rpc-runtime-worker-sofa-5.7.6.jar.sha256":
			w.WriteHeader(http.StatusOK)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	if err := config.SaveRuntimeSourceStore(manager.Paths, model.RuntimeSourceStore{
		Active: "remote",
		Sources: map[string]model.RuntimeSource{
			"remote": {
				Name:      "remote",
				Kind:      "url-template",
				Path:      server.URL + "/runtime/{version}/rpc-runtime-worker-sofa-{version}.jar",
				SHA256URL: server.URL + "/runtime/{version}/rpc-runtime-worker-sofa-{version}.jar.sha256",
			},
		},
	}); err != nil {
		t.Fatalf("SaveRuntimeSourceStore() error = %v", err)
	}

	validation, err := manager.ValidateRuntimeSource("5.7.6", "remote")
	if err != nil {
		t.Fatalf("ValidateRuntimeSource() error = %v", err)
	}
	if !validation.OK || !validation.Active || !validation.VersionDefined || !validation.ArtifactReachable || !validation.ChecksumAvailable {
		t.Fatalf("expected successful validation, got %+v", validation)
	}
	if validation.ChecksumMode != "sha256-url" {
		t.Fatalf("expected sha256-url mode, got %+v", validation)
	}
}

func TestValidateRuntimeSourceReportsManifestVersionMissing(t *testing.T) {
	manager := testManager(t)
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/catalog/runtime.json":
			_, _ = w.Write([]byte(`{
  "schemaVersion": "v1alpha1",
  "versions": {
    "5.4.0": {
      "url": "` + server.URL + `/artifacts/rpc-runtime-worker-sofa-5.4.0.jar"
    }
  }
}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	if err := config.SaveRuntimeSourceStore(manager.Paths, model.RuntimeSourceStore{
		Sources: map[string]model.RuntimeSource{
			"catalog": {
				Name: "catalog",
				Kind: "manifest-url",
				Path: server.URL + "/catalog/runtime.json",
			},
		},
	}); err != nil {
		t.Fatalf("SaveRuntimeSourceStore() error = %v", err)
	}

	validation, err := manager.ValidateRuntimeSource("5.7.6", "catalog")
	if err != nil {
		t.Fatalf("ValidateRuntimeSource() error = %v", err)
	}
	if validation.OK || validation.VersionDefined {
		t.Fatalf("expected missing version validation failure, got %+v", validation)
	}
	if !strings.Contains(validation.Error, `does not define version "5.7.6"`) {
		t.Fatalf("expected missing version error, got %+v", validation)
	}
}

func TestValidateRuntimeSourceReportsManifestInlineChecksum(t *testing.T) {
	manager := testManager(t)
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/catalog/runtime.json":
			_, _ = w.Write([]byte(`{
  "schemaVersion": "v1alpha1",
  "versions": {
    "5.7.6": {
      "url": "` + server.URL + `/artifacts/rpc-runtime-worker-sofa-5.7.6.jar",
      "sha256": "` + strings.Repeat("a", 64) + `"
    }
  }
}`))
		case "/artifacts/rpc-runtime-worker-sofa-5.7.6.jar":
			w.WriteHeader(http.StatusOK)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	if err := config.SaveRuntimeSourceStore(manager.Paths, model.RuntimeSourceStore{
		Sources: map[string]model.RuntimeSource{
			"catalog": {
				Name: "catalog",
				Kind: "manifest-url",
				Path: server.URL + "/catalog/runtime.json",
			},
		},
	}); err != nil {
		t.Fatalf("SaveRuntimeSourceStore() error = %v", err)
	}

	validation, err := manager.ValidateRuntimeSource("5.7.6", "catalog")
	if err != nil {
		t.Fatalf("ValidateRuntimeSource() error = %v", err)
	}
	if !validation.OK || validation.ChecksumMode != "inline-sha256" || !validation.ChecksumAvailable {
		t.Fatalf("expected inline checksum validation success, got %+v", validation)
	}
}

func TestHelperLoopbackDaemonProcess(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_DAEMON") != "1" {
		return
	}
	addressFile := os.Args[len(os.Args)-1]
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		os.Exit(2)
	}
	defer listener.Close()
	if err := os.WriteFile(addressFile, []byte(listener.Addr().String()), 0o644); err != nil {
		os.Exit(3)
	}
	for {
		conn, err := listener.Accept()
		if err != nil {
			os.Exit(0)
		}
		_ = conn.Close()
	}
}

func testManager(t *testing.T) *Manager {
	t.Helper()
	configDir := t.TempDir()
	cacheDir := t.TempDir()
	return NewManager(config.Paths{
		ConfigDir:          configDir,
		CacheDir:           cacheDir,
		ContextsFile:       filepath.Join(configDir, "contexts.json"),
		RuntimeSourcesFile: filepath.Join(configDir, "runtime-sources.json"),
	}, t.TempDir())
}

func writeDaemonMetadata(t *testing.T, manager *Manager, key string, metadata model.DaemonMetadata) {
	t.Helper()
	if err := os.MkdirAll(manager.DaemonDir(), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	body, err := json.Marshal(metadata)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(manager.DaemonDir(), key+".json"), body, 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
}

func writeTextFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
}

func waitForAddressFile(t *testing.T, path string) string {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		body, err := os.ReadFile(path)
		if err == nil && strings.TrimSpace(string(body)) != "" {
			return strings.TrimSpace(string(body))
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("address file %q was not written in time", path)
	return ""
}

func splitListenerAddress(t *testing.T, address string) (string, int) {
	t.Helper()
	host, portText, err := net.SplitHostPort(address)
	if err != nil {
		t.Fatalf("SplitHostPort() error = %v", err)
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		t.Fatalf("Atoi() error = %v", err)
	}
	return host, port
}

func waitForReachable(t *testing.T, host string, port int) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", net.JoinHostPort(host, strconv.Itoa(port)), 100*time.Millisecond)
		if err == nil {
			_ = conn.Close()
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("address %s:%d was not reachable in time", host, port)
}

func assertExists(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected %q to exist: %v", path, err)
	}
}

func assertMissing(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("expected %q to be removed, err=%v", path, err)
	}
}
