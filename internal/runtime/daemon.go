package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/hex1n/sofarpc-cli/internal/model"
)

func (m *Manager) ListDaemons() ([]model.DaemonRecord, error) {
	files, err := filepath.Glob(filepath.Join(m.DaemonDir(), "*.json"))
	if err != nil {
		return nil, err
	}
	records := make([]model.DaemonRecord, 0, len(files))
	for _, file := range files {
		records = append(records, m.daemonRecordFromFile(file))
	}
	sort.Slice(records, func(i, j int) bool {
		left := daemonSortValue(records[i])
		right := daemonSortValue(records[j])
		if left == right {
			return records[i].Key < records[j].Key
		}
		return left > right
	})
	return records, nil
}

func (m *Manager) GetDaemon(key string) (model.DaemonRecord, error) {
	path := m.metadataFileForKey(key)
	if _, err := os.Stat(path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return model.DaemonRecord{}, fmt.Errorf("daemon %q does not exist", key)
		}
		return model.DaemonRecord{}, err
	}
	record := m.daemonRecordFromFile(path)
	if record.Key == "" {
		return model.DaemonRecord{}, fmt.Errorf("daemon %q does not exist", key)
	}
	return record, nil
}

func (m *Manager) StopDaemon(key string) (model.DaemonAction, error) {
	record, err := m.GetDaemon(key)
	if err != nil {
		return model.DaemonAction{}, err
	}
	action := model.DaemonAction{Daemon: record}
	if record.Ready {
		signaled, stopErr := stopLoopbackDaemon(record)
		if stopErr != nil {
			return model.DaemonAction{}, stopErr
		}
		action.SignaledProcess = signaled
	}
	removed, err := removeIfExists(record.MetadataFile)
	if err != nil {
		return model.DaemonAction{}, err
	}
	action.RemovedMetadata = removed
	action.Daemon.Ready = false
	action.Daemon.Stale = true
	return action, nil
}

func (m *Manager) PruneDaemons() ([]model.DaemonAction, error) {
	records, err := m.ListDaemons()
	if err != nil {
		return nil, err
	}
	actions := make([]model.DaemonAction, 0, len(records))
	for _, record := range records {
		if record.Ready {
			continue
		}
		action := model.DaemonAction{Daemon: record}
		if action.RemovedMetadata, err = removeIfExists(record.MetadataFile); err != nil {
			return nil, err
		}
		if action.RemovedStdoutLog, err = removeIfExists(record.StdoutLog); err != nil {
			return nil, err
		}
		if action.RemovedStderrLog, err = removeIfExists(record.StderrLog); err != nil {
			return nil, err
		}
		actions = append(actions, action)
	}
	return actions, nil
}

func (m *Manager) EnsureDaemon(ctx context.Context, spec Spec) (model.DaemonMetadata, error) {
	if err := os.MkdirAll(filepath.Dir(spec.MetadataFile), 0o755); err != nil {
		return model.DaemonMetadata{}, err
	}
	if metadata, ok := m.loadMetadata(spec.MetadataFile); ok && daemonReachable(metadata) {
		return metadata, nil
	}
	if err := os.Remove(spec.MetadataFile); err != nil && !errors.Is(err, os.ErrNotExist) {
		return model.DaemonMetadata{}, err
	}
	address, err := chooseAddress()
	if err != nil {
		return model.DaemonMetadata{}, err
	}
	if err := m.startDaemon(spec, address); err != nil {
		return model.DaemonMetadata{}, err
	}
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return model.DaemonMetadata{}, ctx.Err()
		default:
		}
		if metadata, ok := m.loadMetadata(spec.MetadataFile); ok && daemonReachable(metadata) {
			return metadata, nil
		}
		time.Sleep(200 * time.Millisecond)
	}
	return model.DaemonMetadata{}, fmt.Errorf("worker daemon did not become ready: %s", spec.StderrLog)
}

func (m *Manager) Invoke(ctx context.Context, metadata model.DaemonMetadata, request model.InvocationRequest) (model.InvocationResponse, error) {
	address := net.JoinHostPort(metadata.Host, strconv.Itoa(metadata.Port))
	conn, err := net.DialTimeout("tcp", address, 2*time.Second)
	if err != nil {
		return model.InvocationResponse{}, err
	}
	defer conn.Close()
	timeout := max(request.Target.TimeoutMS, defaultTimeoutMS) + 5000
	_ = conn.SetDeadline(time.Now().Add(time.Duration(timeout) * time.Millisecond))
	select {
	case <-ctx.Done():
		return model.InvocationResponse{}, ctx.Err()
	default:
	}
	if err := json.NewEncoder(conn).Encode(request); err != nil {
		return model.InvocationResponse{}, err
	}
	var response model.InvocationResponse
	if err := json.NewDecoder(conn).Decode(&response); err != nil {
		return model.InvocationResponse{}, err
	}
	return response, nil
}

func (m *Manager) loadMetadata(path string) (model.DaemonMetadata, bool) {
	var metadata model.DaemonMetadata
	body, err := os.ReadFile(path)
	if err != nil {
		return metadata, false
	}
	if err := json.Unmarshal(body, &metadata); err != nil {
		return metadata, false
	}
	return metadata, true
}

func (m *Manager) metadataFileForKey(key string) string {
	return filepath.Join(m.DaemonDir(), key+".json")
}

func (m *Manager) daemonRecordFromFile(path string) model.DaemonRecord {
	key := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	record := model.DaemonRecord{
		Key:          key,
		MetadataFile: path,
		StdoutLog:    filepath.Join(m.DaemonDir(), key+".stdout.log"),
		StderrLog:    filepath.Join(m.DaemonDir(), key+".stderr.log"),
	}
	metadata, ok := m.loadMetadata(path)
	if !ok {
		if _, err := os.Stat(path); err == nil {
			record.Error = "metadata is unreadable"
		}
		record.Stale = true
		return record
	}
	record.Metadata = &metadata
	record.Ready = daemonReachable(metadata)
	record.Stale = !record.Ready
	return record
}

func daemonReachable(metadata model.DaemonMetadata) bool {
	if metadata.Host == "" || metadata.Port == 0 {
		return false
	}
	conn, err := net.DialTimeout("tcp", net.JoinHostPort(metadata.Host, strconv.Itoa(metadata.Port)), 500*time.Millisecond)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

func daemonSortValue(record model.DaemonRecord) string {
	if record.Metadata != nil {
		return record.Metadata.StartedAt
	}
	return ""
}

func stopLoopbackDaemon(record model.DaemonRecord) (bool, error) {
	if record.Metadata == nil {
		return false, nil
	}
	if record.Metadata.PID <= 0 {
		return false, fmt.Errorf("daemon %q is missing a valid pid", record.Key)
	}
	if !isLoopbackHost(record.Metadata.Host) {
		return false, fmt.Errorf("refusing to stop non-loopback daemon %q", record.Key)
	}
	process, err := os.FindProcess(record.Metadata.PID)
	if err != nil {
		return false, err
	}
	defer process.Release()
	if err := process.Kill(); err != nil {
		if !strings.Contains(strings.ToLower(err.Error()), "finished") &&
			!strings.Contains(strings.ToLower(err.Error()), "no process") {
			return false, err
		}
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if record.Metadata == nil || !daemonReachable(*record.Metadata) {
			return true, nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return true, nil
}

func isLoopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func chooseAddress() (string, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", err
	}
	address := listener.Addr().String()
	if err := listener.Close(); err != nil {
		return "", err
	}
	return address, nil
}

func (m *Manager) startDaemon(spec Spec, address string) error {
	stdoutFile, err := os.Create(spec.StdoutLog)
	if err != nil {
		return err
	}
	defer stdoutFile.Close()
	stderrFile, err := os.Create(spec.StderrLog)
	if err != nil {
		return err
	}
	defer stderrFile.Close()
	classpathParts := append([]string{spec.RuntimeJar}, spec.StubPaths...)
	command := exec.Command(spec.JavaBin,
		"-cp",
		strings.Join(classpathParts, string(os.PathListSeparator)),
		mainClass,
		"serve",
		"--listen",
		address,
		"--metadata-file",
		spec.MetadataFile,
	)
	command.Stdout = stdoutFile
	command.Stderr = stderrFile
	command.Env = os.Environ()
	if err := command.Start(); err != nil {
		return err
	}
	return command.Process.Release()
}
