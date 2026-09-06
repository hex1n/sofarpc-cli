package sofarpcwire

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"net"
	"os"
	"regexp"
	"testing"
)

func TestNewTraceContext_UsesSOFATracerIDFormat(t *testing.T) {
	t.Parallel()

	trace := NewTraceContext()

	// TraceIdGenerator: 8 hex chars of the local IPv4, epoch millis, a
	// 1000-9000 sequence, then the pid. Log platforms parse origin and
	// time back out of this, so the shape is part of the contract.
	pattern := regexp.MustCompile(`^[0-9a-f]{8}\d{13}\d{4}` + fmt.Sprint(os.Getpid()) + `$`)
	if !pattern.MatchString(trace.TraceID) {
		t.Fatalf("traceId %q does not match SOFATracer id format", trace.TraceID)
	}
	if trace.RPCID != traceRootRPCID {
		t.Fatalf("rpcId: got %q want %q", trace.RPCID, traceRootRPCID)
	}
	if !trace.Sampled {
		t.Fatal("a debug-tool invoke should be sampled so the provider logs it")
	}
	if trace.CallerApp != TraceCallerApp {
		t.Fatalf("callerApp: got %q want %q", trace.CallerApp, TraceCallerApp)
	}
}

func TestNewTraceContext_EncodesCallerIPAsIDPrefix(t *testing.T) {
	t.Parallel()

	trace := NewTraceContext()
	if trace.CallerIP == "" {
		t.Skip("no global-unicast IPv4 on this host")
	}

	// Log platforms read the origin host out of the id prefix, so decode
	// it back rather than recomputing it the way the generator does.
	octets := make([]byte, 4)
	if _, err := hex.Decode(octets, []byte(trace.TraceID[:8])); err != nil {
		t.Fatalf("traceId prefix %q is not hex: %v", trace.TraceID[:8], err)
	}
	if got := net.IPv4(octets[0], octets[1], octets[2], octets[3]).String(); got != trace.CallerIP {
		t.Fatalf("traceId prefix decodes to %s, want callerIp %s", got, trace.CallerIP)
	}
}

func TestNewTraceContext_IsUniquePerCall(t *testing.T) {
	t.Parallel()

	seen := map[string]struct{}{}
	for i := 0; i < 100; i++ {
		id := NewTraceContext().TraceID
		if _, ok := seen[id]; ok {
			t.Fatalf("duplicate traceId %q after %d calls", id, i)
		}
		seen[id] = struct{}{}
	}
}

func TestTraceContextProps_UsesTracerCompatibleKeys(t *testing.T) {
	t.Parallel()

	props := TraceContext{TraceID: "abc", CallerApp: "cli", CallerIP: "10.0.0.1"}.props()

	want := map[string]string{
		traceIDKey:        "abc",
		traceRPCIDKey:     traceRootRPCID,
		traceSamplingKey:  "false",
		traceCallerAppKey: "cli",
		traceCallerIPKey:  "10.0.0.1",
	}
	for key, value := range want {
		got, ok := props[key]
		if !ok {
			t.Fatalf("props missing %q", key)
		}
		// The provider casts this map to Map<String, String> without
		// checking, so a non-string value would blow up server-side.
		str, isString := got.(string)
		if !isString {
			t.Fatalf("props[%q] = %T, want string", key, got)
		}
		if str != value {
			t.Fatalf("props[%q] = %q, want %q", key, str, value)
		}
	}
}

func TestTraceContextProps_OmitsUnknownCallerIdentity(t *testing.T) {
	t.Parallel()

	props := TraceContext{TraceID: "abc"}.props()

	for _, key := range []string{traceCallerAppKey, traceCallerIPKey} {
		if _, ok := props[key]; ok {
			t.Fatalf("props should omit empty %q", key)
		}
	}
}

func TestBuildGenericRequest_EncodesTraceContext(t *testing.T) {
	t.Parallel()

	trace := NewTraceContext()
	req, err := BuildGenericRequest(RequestSpec{
		Service:    "com.example.Facade",
		Method:     "doThing",
		ParamTypes: []string{"java.lang.String"},
		Args:       []any{"hello"},
		Trace:      trace,
	})
	if err != nil {
		t.Fatalf("BuildGenericRequest() error = %v", err)
	}
	for _, needle := range [][]byte{
		[]byte(TraceContextKey),
		[]byte(traceIDKey),
		[]byte(trace.TraceID),
		[]byte(traceRPCIDKey),
		[]byte(traceCallerAppKey),
		[]byte(TraceCallerApp),
	} {
		if !bytes.Contains(req.Content, needle) {
			t.Fatalf("content missing %q", needle)
		}
	}
}

func TestBuildGenericRequest_OmitsEmptyTraceContext(t *testing.T) {
	t.Parallel()

	req, err := BuildGenericRequest(RequestSpec{
		Service:    "com.example.Facade",
		Method:     "doThing",
		ParamTypes: []string{"java.lang.String"},
		Args:       []any{"hello"},
	})
	if err != nil {
		t.Fatalf("BuildGenericRequest() error = %v", err)
	}
	if bytes.Contains(req.Content, []byte(TraceContextKey)) {
		t.Fatalf("content should carry no %q when the caller sends no trace", TraceContextKey)
	}
}

// Not parallel: it drives the shared counter to its wrap point, which
// would otherwise let a parallel id-uniqueness check see a reset.
func TestNextTraceSequence_RollsWithinTracerRange(t *testing.T) {
	// Mirrors TraceIdGenerator's counter: 1000..9001, then back to 1000.
	traceSequence.Store(9000)
	if got := nextTraceSequence(); got != 9001 {
		t.Fatalf("sequence after 9000: got %d want 9001", got)
	}
	if got := nextTraceSequence(); got != 1000 {
		t.Fatalf("sequence after 9001: got %d want 1000", got)
	}
}
