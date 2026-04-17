package runtime

import (
	"encoding/json"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/hex1n/sofarpc-cli/internal/config"
	"github.com/hex1n/sofarpc-cli/internal/model"
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
		"/tmp/jackson-databind-2.11.2.jar",
		"/tmp/user-api.jar",
	})
	if len(warnings) != 1 {
		t.Fatalf("expected one warning, got %d", len(warnings))
	}
	if strings.Contains(warnings[0], "guava-32.1.2.jar") || strings.Contains(warnings[0], "jackson-databind-2.11.2.jar") {
		t.Fatalf("expected summarized warning without concrete jar names, got %q", warnings[0])
	}
	if !strings.Contains(warnings[0], "2 across guava, jackson") {
		t.Fatalf("expected summarized warning families/count, got %q", warnings[0])
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

func TestRetireProfileDaemonsStopsMatchingProfile(t *testing.T) {
	manager := testManager(t)
	spec := Spec{
		SofaRPCVersion: "5.7.6",
		RuntimeDigest:  "runtime-digest",
		JavaMajor:      "17",
		DaemonProfile:  daemonProfile("5.7.6", "runtime-digest", "17"),
		DaemonKey:      "current-key",
	}

	match1 := model.DaemonMetadata{
		RuntimeVersion: "5.7.6",
		RuntimeDigest:  "runtime-digest",
		JavaMajor:      "17",
		DaemonProfile:  daemonProfile("5.7.6", "runtime-digest", "17"),
	}
	match2 := model.DaemonMetadata{
		RuntimeVersion: "5.7.6",
		RuntimeDigest:  "runtime-digest",
		JavaMajor:      "17",
		DaemonProfile:  daemonProfile("5.7.6", "runtime-digest", "17"),
	}
	legacy := model.DaemonMetadata{
		RuntimeVersion: "5.7.6",
	}
	keep := model.DaemonMetadata{
		RuntimeVersion: "5.7.7",
		RuntimeDigest:  "other-digest",
		JavaMajor:      "17",
		DaemonProfile:  daemonProfile("5.7.7", "other-digest", "17"),
	}

	matchCmd1 := spawnLoopbackDaemonProcess(t, manager, "match-1", &match1)
	matchCmd2 := spawnLoopbackDaemonProcess(t, manager, "match-2", &match2)
	legacyCmd := spawnLoopbackDaemonProcess(t, manager, "legacy", &legacy)
	spawnLoopbackDaemonProcess(t, manager, "keep", &keep)

	actions, err := manager.retireProfileDaemons(spec)
	if err != nil {
		t.Fatalf("retireProfileDaemons() error = %v", err)
	}
	if len(actions) != 3 {
		t.Fatalf("expected 3 retired daemons, got %d", len(actions))
	}

	assertMissing(t, filepath.Join(manager.DaemonDir(), "match-1.json"))
	assertMissing(t, filepath.Join(manager.DaemonDir(), "match-2.json"))
	assertMissing(t, filepath.Join(manager.DaemonDir(), "legacy.json"))
	assertExists(t, filepath.Join(manager.DaemonDir(), "keep.json"))
	waitForReachable(t, keep.Host, keep.Port)

	waitForProcessExit(t, matchCmd1)
	waitForProcessExit(t, matchCmd2)
	waitForProcessExit(t, legacyCmd)
}

func waitForProcessExit(t *testing.T, cmd *exec.Cmd) {
	t.Helper()
	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		_ = cmd.Process.Kill()
		t.Fatalf("expected process %d to exit after stop", cmd.Process.Pid)
	}
}

func spawnLoopbackDaemonProcess(t *testing.T, manager *Manager, key string, metadata *model.DaemonMetadata) *exec.Cmd {
	t.Helper()
	if metadata == nil {
		metadata = &model.DaemonMetadata{}
	}
	addressFile := filepath.Join(t.TempDir(), key+".txt")
	cmd := exec.Command(os.Args[0], "-test.run=TestHelperLoopbackDaemonProcess", "--", addressFile)
	cmd.Env = append(os.Environ(), "GO_WANT_HELPER_DAEMON=1")
	if err := cmd.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	t.Cleanup(func() {
		if cmd.ProcessState == nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
	})
	address := waitForAddressFile(t, addressFile)
	metadata.Host, metadata.Port = splitListenerAddress(t, address)
	metadata.PID = cmd.Process.Pid
	if metadata.StartedAt == "" {
		metadata.StartedAt = "2026-01-01T00:00:00Z"
	}
	writeDaemonMetadata(t, manager, key, *metadata)
	return cmd
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
	bundledJar := filepath.Join(manager.Cwd, "runtime-worker-java", "target", "sofarpc-worker-5.7.6.jar")
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
	bundledJar := filepath.Join(manager.Cwd, "runtime-worker-java", "target", "sofarpc-worker-5.7.6.jar")
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
	bundledJar := filepath.Join(sourceDir, "5.7.6", "sofarpc-worker-5.7.6.jar")
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
	bundledJar := filepath.Join(sourceDir, "sofarpc-worker-5.7.6.jar")
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
