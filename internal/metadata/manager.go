package metadata

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/hex1n/sofarpc-cli/internal/config"
	"github.com/hex1n/sofarpc-cli/internal/contract"
	"github.com/hex1n/sofarpc-cli/internal/model"
)

type Manager struct {
	Paths config.Paths
	Cwd   string
}

func NewManager(paths config.Paths, cwd string) *Manager {
	return &Manager{Paths: paths, Cwd: cwd}
}

func (m *Manager) DaemonDir() string {
	return filepath.Join(m.Paths.CacheDir, "metadata")
}

func (m *Manager) metadataFile() string {
	return filepath.Join(m.DaemonDir(), "daemon.json")
}

func (m *Manager) ResolveServiceSchema(ctx context.Context, projectRoot, service string, refresh bool) (model.ServiceSchema, string, bool, error) {
	meta, err := m.ensureDaemon(ctx)
	if err != nil {
		return model.ServiceSchema{}, "", false, err
	}
	resp, err := m.request(ctx, meta, resolveRequest{
		Action:      actionSchema,
		ProjectRoot: projectRoot,
		Service:     service,
		Refresh:     refresh,
	})
	if err != nil {
		return model.ServiceSchema{}, "", false, err
	}
	if !resp.OK || resp.Schema == nil {
		return model.ServiceSchema{}, "", false, responseError(resp)
	}
	return *resp.Schema, resp.Source, resp.CacheHit, nil
}

func (m *Manager) ResolveMethod(ctx context.Context, projectRoot, service, method string, preferredParamTypes []string, rawArgs json.RawMessage, refresh bool) (contract.ProjectMethod, string, bool, error) {
	meta, err := m.ensureDaemon(ctx)
	if err != nil {
		return contract.ProjectMethod{}, "", false, err
	}
	resp, err := m.request(ctx, meta, resolveRequest{
		Action:              actionMethod,
		ProjectRoot:         projectRoot,
		Service:             service,
		Method:              method,
		PreferredParamTypes: append([]string{}, preferredParamTypes...),
		RawArgs:             rawArgs,
		Refresh:             refresh,
	})
	if err != nil {
		return contract.ProjectMethod{}, "", false, err
	}
	if !resp.OK || resp.Method == nil {
		return contract.ProjectMethod{}, "", false, responseError(resp)
	}
	return *resp.Method, resp.Source, resp.CacheHit, nil
}

func responseError(resp resolveResponse) error {
	if strings.TrimSpace(resp.Error) != "" {
		return errors.New(resp.Error)
	}
	return errors.New("metadata daemon request failed")
}

func (m *Manager) ensureDaemon(ctx context.Context) (daemonMetadata, error) {
	if err := os.MkdirAll(m.DaemonDir(), 0o755); err != nil {
		return daemonMetadata{}, err
	}
	path := m.metadataFile()
	executable, digest, ttlValue, err := m.currentDaemonIdentity()
	if err != nil {
		return daemonMetadata{}, err
	}

	if meta, ok := loadMetadata(path); ok {
		if metadataMatches(meta, executable, digest, ttlValue) && daemonReachable(meta) {
			return meta, nil
		}
		if live, ok := m.fetchLiveMetadata(ctx, meta); ok {
			if metadataMatches(live, executable, digest, ttlValue) {
				if err := writeMetadataFile(path, live); err == nil {
					return live, nil
				}
				return live, nil
			}
			_, _ = m.request(ctx, meta, resolveRequest{Action: actionShutdown})
		}
		_ = os.Remove(path)
	}

	address, err := chooseAddress()
	if err != nil {
		return daemonMetadata{}, err
	}
	if err := m.startDaemon(address, path); err != nil {
		return daemonMetadata{}, err
	}

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return daemonMetadata{}, ctx.Err()
		default:
		}
		if meta, ok := loadMetadata(path); ok && metadataMatches(meta, executable, digest, ttlValue) && daemonReachable(meta) {
			return meta, nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return daemonMetadata{}, fmt.Errorf("metadata daemon did not become ready")
}

func (m *Manager) startDaemon(address, metadataFile string) error {
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	cmd := exec.Command(executable, "metadata", "serve", "--listen", address, "--metadata-file", metadataFile)
	cmd.Stdout = nil
	cmd.Stderr = nil
	cmd.Env = os.Environ()
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}

func (m *Manager) currentDaemonIdentity() (string, string, string, error) {
	executable, err := os.Executable()
	if err != nil {
		return "", "", "", err
	}
	executable, err = filepath.Abs(executable)
	if err != nil {
		return "", "", "", err
	}
	digest, err := executableDigest(executable)
	if err != nil {
		return "", "", "", err
	}
	return executable, digest, resolveCacheTTL().String(), nil
}

func (m *Manager) request(ctx context.Context, meta daemonMetadata, req resolveRequest) (resolveResponse, error) {
	address := net.JoinHostPort(meta.Host, strconv.Itoa(meta.Port))
	conn, err := (&net.Dialer{Timeout: 2 * time.Second}).DialContext(ctx, "tcp", address)
	if err != nil {
		return resolveResponse{}, err
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(10 * time.Second))
	if err := json.NewEncoder(conn).Encode(req); err != nil {
		return resolveResponse{}, err
	}
	var resp resolveResponse
	if err := json.NewDecoder(conn).Decode(&resp); err != nil {
		return resolveResponse{}, err
	}
	return resp, nil
}

func metadataMatches(meta daemonMetadata, executable, digest, ttlValue string) bool {
	return meta.ProtocolVersion == protocolVersion &&
		strings.EqualFold(filepath.Clean(meta.Executable), filepath.Clean(executable)) &&
		meta.ExecutableDigest == digest &&
		meta.CacheTTL == ttlValue
}

func loadMetadata(path string) (daemonMetadata, bool) {
	var meta daemonMetadata
	body, err := os.ReadFile(path)
	if err != nil {
		return meta, false
	}
	if err := json.Unmarshal(body, &meta); err != nil {
		return meta, false
	}
	return meta, true
}

func daemonReachable(meta daemonMetadata) bool {
	if meta.Host == "" || meta.Port == 0 {
		return false
	}
	conn, err := net.DialTimeout("tcp", net.JoinHostPort(meta.Host, strconv.Itoa(meta.Port)), 500*time.Millisecond)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
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

func (m *Manager) fetchLiveMetadata(ctx context.Context, meta daemonMetadata) (daemonMetadata, bool) {
	if !daemonReachable(meta) {
		return daemonMetadata{}, false
	}
	resp, err := m.request(ctx, meta, resolveRequest{Action: actionInfo})
	if err != nil || !resp.OK || resp.Metadata == nil {
		return daemonMetadata{}, false
	}
	return *resp.Metadata, true
}
