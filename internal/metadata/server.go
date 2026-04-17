package metadata

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/hex1n/sofarpc-cli/internal/contract"
	"github.com/hex1n/sofarpc-cli/internal/model"
)

var (
	describeServiceFromProjectFn   = contract.DescribeServiceFromProject
	describeServiceFromArtifactsFn = contract.DescribeServiceFromArtifacts
	resolveMethodFromProjectFn     = contract.ResolveMethodFromProject
	resolveMethodFromArtifactsFn   = contract.ResolveMethodFromArtifacts
	cacheTTL                       = 30 * time.Minute
)

const cacheTTLEnv = "SOFARPC_METADATA_CACHE_TTL"

type schemaCacheEntry struct {
	Schema    model.ServiceSchema
	Source    string
	ExpiresAt time.Time
}

type methodCacheEntry struct {
	Method    contract.ProjectMethod
	Source    string
	ExpiresAt time.Time
}

type daemon struct {
	now         func() time.Time
	ttl         time.Duration
	meta        daemonMetadata
	listener    net.Listener
	mu          sync.Mutex
	stopOnce    sync.Once
	stopped     bool
	schemaCache map[string]schemaCacheEntry
	methodCache map[string]methodCacheEntry
}

func newDaemon() *daemon {
	return newDaemonWithTTL(cacheTTL)
}

func newDaemonWithTTL(ttl time.Duration) *daemon {
	if ttl <= 0 {
		ttl = cacheTTL
	}
	return &daemon{
		now:         time.Now,
		ttl:         ttl,
		schemaCache: map[string]schemaCacheEntry{},
		methodCache: map[string]methodCacheEntry{},
	}
}

func Serve(listenAddress, metadataFile string) error {
	if strings.TrimSpace(listenAddress) == "" {
		return errors.New("listen address is required")
	}
	if strings.TrimSpace(metadataFile) == "" {
		return errors.New("metadata file is required")
	}
	listener, err := net.Listen("tcp", listenAddress)
	if err != nil {
		return err
	}
	defer listener.Close()

	ttl := resolveCacheTTL()
	meta, err := daemonMetadataForListener(listener, ttl)
	if err != nil {
		return err
	}
	if err := writeMetadataFile(metadataFile, meta); err != nil {
		return err
	}

	server := newDaemonWithTTL(ttl)
	server.meta = meta
	server.listener = listener
	for {
		conn, err := listener.Accept()
		if err != nil {
			if server.isStopped() {
				return nil
			}
			if ne, ok := err.(net.Error); ok && ne.Temporary() {
				time.Sleep(100 * time.Millisecond)
				continue
			}
			return err
		}
		go server.serveConn(conn)
	}
}

func daemonMetadataForListener(listener net.Listener, ttl time.Duration) (daemonMetadata, error) {
	host, port, err := net.SplitHostPort(listener.Addr().String())
	if err != nil {
		return daemonMetadata{}, err
	}
	portNum, err := strconv.Atoi(port)
	if err != nil {
		return daemonMetadata{}, err
	}
	executable, err := os.Executable()
	if err != nil {
		return daemonMetadata{}, err
	}
	executable, err = filepath.Abs(executable)
	if err != nil {
		return daemonMetadata{}, err
	}
	digest, err := executableDigest(executable)
	if err != nil {
		return daemonMetadata{}, err
	}
	return daemonMetadata{
		PID:              os.Getpid(),
		Host:             host,
		Port:             portNum,
		StartedAt:        time.Now().Format(time.RFC3339Nano),
		ProtocolVersion:  protocolVersion,
		Executable:       executable,
		ExecutableDigest: digest,
		CacheTTL:         ttl.String(),
	}, nil
}

func writeMetadataFile(path string, meta daemonMetadata) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	body, err := json.Marshal(meta)
	if err != nil {
		return err
	}
	return os.WriteFile(path, body, 0o644)
}

func (d *daemon) serveConn(conn net.Conn) {
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(15 * time.Second))
	var req resolveRequest
	if err := json.NewDecoder(conn).Decode(&req); err != nil {
		_ = json.NewEncoder(conn).Encode(resolveResponse{OK: false, Error: err.Error()})
		return
	}
	resp := d.handle(req)
	_ = json.NewEncoder(conn).Encode(resp)
	if req.Action == actionShutdown && resp.OK {
		d.shutdown()
	}
}

func (d *daemon) handle(req resolveRequest) resolveResponse {
	switch req.Action {
	case actionSchema:
		return d.resolveSchema(req)
	case actionMethod:
		return d.resolveMethod(req)
	case actionInfo:
		meta := d.meta
		return resolveResponse{OK: true, Metadata: &meta}
	case actionShutdown:
		meta := d.meta
		return resolveResponse{OK: true, Metadata: &meta}
	default:
		return resolveResponse{OK: false, Error: fmt.Sprintf("unknown action %q", req.Action)}
	}
}

func (d *daemon) resolveSchema(req resolveRequest) resolveResponse {
	key, cacheable := d.schemaCacheKey(req)
	if cacheable && !req.Refresh {
		if cached, ok := d.getSchema(key); ok {
			schema := cached.Schema
			return resolveResponse{OK: true, Source: cached.Source, CacheHit: true, Schema: &schema}
		}
	}
	schema, source, notes, err := resolveSchemaUncached(req.ProjectRoot, req.Service)
	if err != nil {
		return resolveResponse{OK: false, Error: err.Error(), Notes: notes}
	}
	if cacheable {
		d.putSchema(key, schemaCacheEntry{
			Schema:    schema,
			Source:    source,
			ExpiresAt: d.nextExpiry(),
		})
	}
	return resolveResponse{OK: true, Source: source, Notes: notes, Schema: &schema}
}

func (d *daemon) resolveMethod(req resolveRequest) resolveResponse {
	key, cacheable := d.methodCacheKey(req)
	if cacheable && !req.Refresh {
		if cached, ok := d.getMethod(key); ok {
			method := cached.Method
			return resolveResponse{OK: true, Source: cached.Source, CacheHit: true, Method: &method}
		}
	}
	method, source, notes, err := resolveMethodUncached(req.ProjectRoot, req.Service, req.Method, req.PreferredParamTypes, req.RawArgs)
	if err != nil {
		return resolveResponse{OK: false, Error: err.Error(), Notes: notes}
	}
	if cacheable {
		d.putMethod(key, methodCacheEntry{
			Method:    method,
			Source:    source,
			ExpiresAt: d.nextExpiry(),
		})
	}
	return resolveResponse{OK: true, Source: source, Notes: notes, Method: &method}
}

func resolveSchemaUncached(projectRoot, service string) (model.ServiceSchema, string, []string, error) {
	notes := []string{}
	if schema, err := describeServiceFromProjectFn(projectRoot, service); err == nil {
		return schema, "project-source", notes, nil
	} else {
		notes = append(notes, contractAttemptNote("project-source", err))
	}
	if schema, err := describeServiceFromArtifactsFn(projectRoot, service); err == nil {
		return schema, "jar-javap", notes, nil
	} else {
		notes = append(notes, contractAttemptNote("jar-javap", err))
	}
	return model.ServiceSchema{}, "", notes, fmt.Errorf("service schema %s not found", service)
}

func resolveMethodUncached(projectRoot, service, method string, preferredParamTypes []string, rawArgs json.RawMessage) (contract.ProjectMethod, string, []string, error) {
	notes := []string{}
	if resolved, err := resolveMethodFromProjectFn(projectRoot, service, method, preferredParamTypes, rawArgs); err == nil {
		return resolved, "project-source", notes, nil
	} else {
		notes = append(notes, contractAttemptNote("project-source", err))
	}
	if resolved, err := resolveMethodFromArtifactsFn(projectRoot, service, method, preferredParamTypes, rawArgs); err == nil {
		return resolved, "jar-javap", notes, nil
	} else {
		notes = append(notes, contractAttemptNote("jar-javap", err))
	}
	return contract.ProjectMethod{}, "", notes, fmt.Errorf("method contract %s.%s not found", service, method)
}

func contractAttemptNote(stage string, err error) string {
	if err == nil {
		return stage
	}
	text := strings.TrimSpace(err.Error())
	if idx := strings.IndexByte(text, '\n'); idx >= 0 {
		text = text[:idx]
	}
	if len(text) > 180 {
		text = text[:177] + "..."
	}
	return stage + ": " + text
}

func resolveCacheTTL() time.Duration {
	raw := strings.TrimSpace(os.Getenv(cacheTTLEnv))
	if raw == "" {
		return cacheTTL
	}
	ttl, err := time.ParseDuration(raw)
	if err != nil || ttl <= 0 {
		return cacheTTL
	}
	return ttl
}

func (d *daemon) getSchema(key string) (schemaCacheEntry, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	entry, ok := d.schemaCache[key]
	if !ok {
		return schemaCacheEntry{}, false
	}
	if d.now().After(entry.ExpiresAt) {
		delete(d.schemaCache, key)
		return schemaCacheEntry{}, false
	}
	entry.ExpiresAt = d.nextExpiry()
	d.schemaCache[key] = entry
	return entry, true
}

func (d *daemon) putSchema(key string, entry schemaCacheEntry) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.schemaCache[key] = entry
}

func (d *daemon) getMethod(key string) (methodCacheEntry, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	entry, ok := d.methodCache[key]
	if !ok {
		return methodCacheEntry{}, false
	}
	if d.now().After(entry.ExpiresAt) {
		delete(d.methodCache, key)
		return methodCacheEntry{}, false
	}
	entry.ExpiresAt = d.nextExpiry()
	d.methodCache[key] = entry
	return entry, true
}

func (d *daemon) nextExpiry() time.Time {
	return d.now().Add(d.ttl)
}

func (d *daemon) shutdown() {
	d.stopOnce.Do(func() {
		d.mu.Lock()
		d.stopped = true
		d.mu.Unlock()
		if d.listener != nil {
			_ = d.listener.Close()
		}
	})
}

func (d *daemon) isStopped() bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.stopped
}

func (d *daemon) putMethod(key string, entry methodCacheEntry) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.methodCache[key] = entry
}

func schemaCacheKey(projectRoot, service string) string {
	return strings.Join([]string{
		filepath.Clean(strings.TrimSpace(projectRoot)),
		strings.TrimSpace(service),
	}, "|")
}

func methodCacheKey(projectRoot, service, method string, preferredParamTypes []string, arity int) string {
	return strings.Join([]string{
		filepath.Clean(strings.TrimSpace(projectRoot)),
		strings.TrimSpace(service),
		strings.TrimSpace(method),
		strings.Join(preferredParamTypes, ","),
		strconv.Itoa(arity),
	}, "|")
}

func (d *daemon) schemaCacheKey(req resolveRequest) (string, bool) {
	fingerprint, err := contractFingerprintFn(req.ProjectRoot, req.Service)
	if err != nil || fingerprint == "" {
		return "", false
	}
	return strings.Join([]string{
		schemaCacheKey(req.ProjectRoot, req.Service),
		fingerprint,
	}, "|"), true
}

func (d *daemon) methodCacheKey(req resolveRequest) (string, bool) {
	fingerprint, err := contractFingerprintFn(req.ProjectRoot, req.Service)
	if err != nil || fingerprint == "" {
		return "", false
	}
	return strings.Join([]string{
		methodCacheKey(req.ProjectRoot, req.Service, req.Method, req.PreferredParamTypes, argsArityHint(req.RawArgs)),
		fingerprint,
	}, "|"), true
}

func argsArityHint(raw json.RawMessage) int {
	if len(raw) == 0 {
		return -1
	}
	if !isJSONArray(raw) {
		return 1
	}
	var items []json.RawMessage
	if err := json.Unmarshal(raw, &items); err != nil {
		return -1
	}
	return len(items)
}

func executableDigest(path string) (string, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	hash := sha256.Sum256(body)
	return hex.EncodeToString(hash[:]), nil
}

func isJSONArray(raw []byte) bool {
	for _, b := range raw {
		switch b {
		case ' ', '\t', '\n', '\r':
			continue
		case '[':
			return true
		default:
			return false
		}
	}
	return false
}
