package metadata

import (
	"encoding/json"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/hex1n/sofarpc-cli/internal/contract"
	"github.com/hex1n/sofarpc-cli/internal/model"
)

func TestDaemonResolveSchemaCachesInMemory(t *testing.T) {
	origProject := describeServiceFromProjectFn
	origArtifacts := describeServiceFromArtifactsFn
	origFingerprint := contractFingerprintFn
	origTTL := cacheTTL
	t.Cleanup(func() {
		describeServiceFromProjectFn = origProject
		describeServiceFromArtifactsFn = origArtifacts
		contractFingerprintFn = origFingerprint
		cacheTTL = origTTL
	})

	calls := 0
	describeServiceFromProjectFn = func(projectRoot, service string) (model.ServiceSchema, error) {
		calls++
		return model.ServiceSchema{
			Service: service,
			Methods: []model.MethodSchema{{Name: "importAsset"}},
		}, nil
	}
	describeServiceFromArtifactsFn = func(projectRoot, service string) (model.ServiceSchema, error) {
		return model.ServiceSchema{}, errors.New("artifact miss")
	}
	contractFingerprintFn = func(projectRoot, service string) (string, error) {
		return "source:fingerprint-a", nil
	}
	cacheTTL = time.Minute

	now := time.Unix(100, 0)
	d := newDaemon()
	d.now = func() time.Time { return now }

	req := resolveRequest{Action: actionSchema, ProjectRoot: "C:/repo", Service: "com.example.OrderFacade"}
	first := d.handle(req)
	second := d.handle(req)

	if !first.OK || first.CacheHit {
		t.Fatalf("first = %+v", first)
	}
	if !second.OK || !second.CacheHit {
		t.Fatalf("second = %+v", second)
	}
	if calls != 1 {
		t.Fatalf("calls = %d, want 1", calls)
	}
}

func TestDaemonResolveMethodCachesByArityAndPreferredTypes(t *testing.T) {
	origProject := resolveMethodFromProjectFn
	origArtifacts := resolveMethodFromArtifactsFn
	origFingerprint := contractFingerprintFn
	origTTL := cacheTTL
	t.Cleanup(func() {
		resolveMethodFromProjectFn = origProject
		resolveMethodFromArtifactsFn = origArtifacts
		contractFingerprintFn = origFingerprint
		cacheTTL = origTTL
	})

	calls := 0
	resolveMethodFromProjectFn = func(projectRoot, service, method string, preferredParamTypes []string, rawArgs json.RawMessage) (contract.ProjectMethod, error) {
		calls++
		return contract.ProjectMethod{
			Schema: model.MethodSchema{
				Name:                method,
				ParamTypes:          []string{"com.example.OrderRequest"},
				ParamTypeSignatures: []string{"com.example.OrderRequest"},
			},
		}, nil
	}
	resolveMethodFromArtifactsFn = func(projectRoot, service, method string, preferredParamTypes []string, rawArgs json.RawMessage) (contract.ProjectMethod, error) {
		return contract.ProjectMethod{}, errors.New("artifact miss")
	}
	contractFingerprintFn = func(projectRoot, service string) (string, error) {
		return "source:fingerprint-a", nil
	}
	cacheTTL = time.Minute

	d := newDaemon()
	req := resolveRequest{
		Action:              actionMethod,
		ProjectRoot:         "C:/repo",
		Service:             "com.example.OrderFacade",
		Method:              "importAsset",
		PreferredParamTypes: []string{"com.example.OrderRequest"},
		RawArgs:             json.RawMessage(`[{"id":1}]`),
	}
	first := d.handle(req)
	second := d.handle(req)

	if !first.OK || first.CacheHit {
		t.Fatalf("first = %+v", first)
	}
	if !second.OK || !second.CacheHit {
		t.Fatalf("second = %+v", second)
	}
	if calls != 1 {
		t.Fatalf("calls = %d, want 1", calls)
	}
}

func TestDaemonResolveMethodExpiresEntries(t *testing.T) {
	origProject := resolveMethodFromProjectFn
	origArtifacts := resolveMethodFromArtifactsFn
	origFingerprint := contractFingerprintFn
	origTTL := cacheTTL
	t.Cleanup(func() {
		resolveMethodFromProjectFn = origProject
		resolveMethodFromArtifactsFn = origArtifacts
		contractFingerprintFn = origFingerprint
		cacheTTL = origTTL
	})

	calls := 0
	resolveMethodFromProjectFn = func(projectRoot, service, method string, preferredParamTypes []string, rawArgs json.RawMessage) (contract.ProjectMethod, error) {
		calls++
		return contract.ProjectMethod{
			Schema: model.MethodSchema{Name: method},
		}, nil
	}
	resolveMethodFromArtifactsFn = func(projectRoot, service, method string, preferredParamTypes []string, rawArgs json.RawMessage) (contract.ProjectMethod, error) {
		return contract.ProjectMethod{}, errors.New("artifact miss")
	}
	contractFingerprintFn = func(projectRoot, service string) (string, error) {
		return "source:fingerprint-a", nil
	}
	cacheTTL = time.Second

	now := time.Unix(200, 0)
	d := newDaemon()
	d.now = func() time.Time { return now }

	req := resolveRequest{
		Action:      actionMethod,
		ProjectRoot: "C:/repo",
		Service:     "com.example.OrderFacade",
		Method:      "importAsset",
		RawArgs:     json.RawMessage(`[{}]`),
	}

	if resp := d.handle(req); !resp.OK || resp.CacheHit {
		t.Fatalf("first = %+v", resp)
	}
	now = now.Add(2 * time.Second)
	if resp := d.handle(req); !resp.OK || resp.CacheHit {
		t.Fatalf("second after ttl = %+v", resp)
	}
	if calls != 2 {
		t.Fatalf("calls = %d, want 2", calls)
	}
}

func TestDaemonCacheHitExtendsExpiry(t *testing.T) {
	origProject := describeServiceFromProjectFn
	origArtifacts := describeServiceFromArtifactsFn
	origFingerprint := contractFingerprintFn
	origTTL := cacheTTL
	t.Cleanup(func() {
		describeServiceFromProjectFn = origProject
		describeServiceFromArtifactsFn = origArtifacts
		contractFingerprintFn = origFingerprint
		cacheTTL = origTTL
	})

	calls := 0
	describeServiceFromProjectFn = func(projectRoot, service string) (model.ServiceSchema, error) {
		calls++
		return model.ServiceSchema{
			Service: service,
			Methods: []model.MethodSchema{{Name: "importAsset"}},
		}, nil
	}
	describeServiceFromArtifactsFn = func(projectRoot, service string) (model.ServiceSchema, error) {
		return model.ServiceSchema{}, errors.New("artifact miss")
	}
	contractFingerprintFn = func(projectRoot, service string) (string, error) {
		return "source:fingerprint-a", nil
	}
	cacheTTL = 10 * time.Second

	now := time.Unix(300, 0)
	d := newDaemon()
	d.now = func() time.Time { return now }

	req := resolveRequest{Action: actionSchema, ProjectRoot: "C:/repo", Service: "com.example.OrderFacade"}
	if resp := d.handle(req); !resp.OK || resp.CacheHit {
		t.Fatalf("first = %+v", resp)
	}
	now = now.Add(9 * time.Second)
	if resp := d.handle(req); !resp.OK || !resp.CacheHit {
		t.Fatalf("second = %+v", resp)
	}
	now = now.Add(9 * time.Second)
	if resp := d.handle(req); !resp.OK || !resp.CacheHit {
		t.Fatalf("third = %+v", resp)
	}
	if calls != 1 {
		t.Fatalf("calls = %d, want 1", calls)
	}
}

func TestDaemonRefreshBypassesCache(t *testing.T) {
	origProject := resolveMethodFromProjectFn
	origArtifacts := resolveMethodFromArtifactsFn
	origFingerprint := contractFingerprintFn
	origTTL := cacheTTL
	t.Cleanup(func() {
		resolveMethodFromProjectFn = origProject
		resolveMethodFromArtifactsFn = origArtifacts
		contractFingerprintFn = origFingerprint
		cacheTTL = origTTL
	})

	calls := 0
	resolveMethodFromProjectFn = func(projectRoot, service, method string, preferredParamTypes []string, rawArgs json.RawMessage) (contract.ProjectMethod, error) {
		calls++
		return contract.ProjectMethod{
			Schema: model.MethodSchema{Name: method},
		}, nil
	}
	resolveMethodFromArtifactsFn = func(projectRoot, service, method string, preferredParamTypes []string, rawArgs json.RawMessage) (contract.ProjectMethod, error) {
		return contract.ProjectMethod{}, errors.New("artifact miss")
	}
	contractFingerprintFn = func(projectRoot, service string) (string, error) {
		return "source:fingerprint-a", nil
	}
	cacheTTL = time.Minute

	d := newDaemon()
	req := resolveRequest{
		Action:      actionMethod,
		ProjectRoot: "C:/repo",
		Service:     "com.example.OrderFacade",
		Method:      "importAsset",
		RawArgs:     json.RawMessage(`[{}]`),
	}
	if resp := d.handle(req); !resp.OK || resp.CacheHit {
		t.Fatalf("first = %+v", resp)
	}
	req.Refresh = true
	if resp := d.handle(req); !resp.OK || resp.CacheHit {
		t.Fatalf("refresh = %+v", resp)
	}
	if calls != 2 {
		t.Fatalf("calls = %d, want 2", calls)
	}
}

func TestDaemonResolveMethodCarriesFallbackNotes(t *testing.T) {
	origProject := resolveMethodFromProjectFn
	origArtifacts := resolveMethodFromArtifactsFn
	t.Cleanup(func() {
		resolveMethodFromProjectFn = origProject
		resolveMethodFromArtifactsFn = origArtifacts
	})

	resolveMethodFromProjectFn = func(projectRoot, service, method string, preferredParamTypes []string, rawArgs json.RawMessage) (contract.ProjectMethod, error) {
		return contract.ProjectMethod{}, errors.New("source miss")
	}
	resolveMethodFromArtifactsFn = func(projectRoot, service, method string, preferredParamTypes []string, rawArgs json.RawMessage) (contract.ProjectMethod, error) {
		return contract.ProjectMethod{
			Schema: model.MethodSchema{Name: method},
		}, nil
	}

	d := newDaemon()
	resp := d.handle(resolveRequest{
		Action:      actionMethod,
		ProjectRoot: "C:/repo",
		Service:     "com.example.OrderFacade",
		Method:      "importAsset",
		RawArgs:     json.RawMessage(`[{}]`),
	})
	if !resp.OK {
		t.Fatalf("resp = %+v", resp)
	}
	if len(resp.Notes) != 1 || resp.Notes[0] != "project-source: source miss" {
		t.Fatalf("Notes = %v", resp.Notes)
	}
}

func TestDaemonFingerprintChangeInvalidatesCache(t *testing.T) {
	origProject := describeServiceFromProjectFn
	origArtifacts := describeServiceFromArtifactsFn
	origFingerprint := contractFingerprintFn
	origTTL := cacheTTL
	t.Cleanup(func() {
		describeServiceFromProjectFn = origProject
		describeServiceFromArtifactsFn = origArtifacts
		contractFingerprintFn = origFingerprint
		cacheTTL = origTTL
	})

	calls := 0
	describeServiceFromProjectFn = func(projectRoot, service string) (model.ServiceSchema, error) {
		calls++
		return model.ServiceSchema{
			Service: service,
			Methods: []model.MethodSchema{{Name: "importAsset"}},
		}, nil
	}
	describeServiceFromArtifactsFn = func(projectRoot, service string) (model.ServiceSchema, error) {
		return model.ServiceSchema{}, errors.New("artifact miss")
	}
	fingerprint := "source:fingerprint-a"
	contractFingerprintFn = func(projectRoot, service string) (string, error) {
		return fingerprint, nil
	}
	cacheTTL = time.Minute

	d := newDaemon()
	req := resolveRequest{Action: actionSchema, ProjectRoot: "C:/repo", Service: "com.example.OrderFacade"}
	if resp := d.handle(req); !resp.OK || resp.CacheHit {
		t.Fatalf("first = %+v", resp)
	}
	if resp := d.handle(req); !resp.OK || !resp.CacheHit {
		t.Fatalf("second = %+v", resp)
	}
	fingerprint = "source:fingerprint-b"
	if resp := d.handle(req); !resp.OK || resp.CacheHit {
		t.Fatalf("third after fingerprint change = %+v", resp)
	}
	if calls != 2 {
		t.Fatalf("calls = %d, want 2", calls)
	}
}

func TestResolveCacheTTLFromEnv(t *testing.T) {
	origTTL := cacheTTL
	t.Cleanup(func() {
		cacheTTL = origTTL
		_ = os.Unsetenv(cacheTTLEnv)
	})

	cacheTTL = 45 * time.Minute
	if ttl := resolveCacheTTL(); ttl != 45*time.Minute {
		t.Fatalf("resolveCacheTTL() = %s, want fallback 45m", ttl)
	}

	if err := os.Setenv(cacheTTLEnv, "2h15m"); err != nil {
		t.Fatalf("Setenv: %v", err)
	}
	if ttl := resolveCacheTTL(); ttl != 2*time.Hour+15*time.Minute {
		t.Fatalf("resolveCacheTTL(env) = %s", ttl)
	}

	if err := os.Setenv(cacheTTLEnv, "bad"); err != nil {
		t.Fatalf("Setenv invalid: %v", err)
	}
	if ttl := resolveCacheTTL(); ttl != 45*time.Minute {
		t.Fatalf("resolveCacheTTL(invalid) = %s, want fallback 45m", ttl)
	}
}
