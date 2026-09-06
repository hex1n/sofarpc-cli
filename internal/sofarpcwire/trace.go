package sofarpcwire

import (
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// SOFARPC carries the caller's tracing context in
// SofaRequest.requestProps["rpc_trace_context"]. A provider running
// SOFATracer adopts sofaTraceId from that map as its server span's trace
// id (RpcSofaTracer.serverReceived), so an id generated here is the id
// that shows up in the provider's digest and business logs. Without it
// the provider mints its own trace id, which the caller never learns.
//
// The Java bolt client puts this map in the hessian-encoded request
// content, not in the BOLT header, so encoding it alongside
// rpc_req_baggage is the faithful shape.
const (
	TraceContextKey = "rpc_trace_context"

	traceIDKey        = "sofaTraceId"
	traceRPCIDKey     = "sofaRpcId"
	traceSamplingKey  = "samp"
	traceCallerAppKey = "sofaCallerApp"
	traceCallerIPKey  = "sofaCallerIp"

	// TraceCallerApp is the caller identity providers log as the remote
	// app, so agent-originated calls are recognizable in their logs.
	TraceCallerApp = "sofarpc-cli"

	// traceRootRPCID is SOFATracer's root span id. Provider spans hang
	// off it as 0.1, 0.2, and so on.
	traceRootRPCID = "0"

	// traceUnknownIPHex mirrors TraceIdGenerator's fallback when the
	// local address cannot be determined.
	traceUnknownIPHex = "ffffffff"
)

// TraceContext is the caller half of a SOFATracer trace. The zero value
// carries no trace and is encoded as nothing.
type TraceContext struct {
	TraceID   string
	RPCID     string
	CallerApp string
	CallerIP  string
	Sampled   bool
}

// NewTraceContext mints a root trace context in SOFATracer's own
// TraceIdGenerator format: 8 hex chars of the local IPv4, epoch
// milliseconds, a 1000-9000 sequence, and the pid. Keeping the format
// matters because log platforms parse origin and time out of the id.
func NewTraceContext() TraceContext {
	ip, ipHex := localIPv4()
	return TraceContext{
		TraceID:   fmt.Sprintf("%s%d%d%d", ipHex, time.Now().UnixMilli(), nextTraceSequence(), os.Getpid()),
		RPCID:     traceRootRPCID,
		CallerApp: TraceCallerApp,
		CallerIP:  ip,
		Sampled:   true,
	}
}

// props renders the wire map SOFATracer reads on the provider side.
// Keys come from TracerCompatibleConstants.
func (t TraceContext) props() map[string]interface{} {
	rpcID := strings.TrimSpace(t.RPCID)
	if rpcID == "" {
		rpcID = traceRootRPCID
	}
	// String values only: the provider casts this map to
	// Map<String, String> without checking.
	props := map[string]interface{}{
		traceIDKey:       strings.TrimSpace(t.TraceID),
		traceRPCIDKey:    rpcID,
		traceSamplingKey: strconv.FormatBool(t.Sampled),
	}
	if app := strings.TrimSpace(t.CallerApp); app != "" {
		props[traceCallerAppKey] = app
	}
	if ip := strings.TrimSpace(t.CallerIP); ip != "" {
		props[traceCallerIPKey] = ip
	}
	return props
}

// traceSequence mirrors TraceIdGenerator's 1000-9000 rolling counter.
var traceSequence atomic.Int32

func init() {
	traceSequence.Store(1000)
}

func nextTraceSequence() int32 {
	for {
		current := traceSequence.Load()
		next := current + 1
		if current > 9000 {
			next = 1000
		}
		if traceSequence.CompareAndSwap(current, next) {
			return next
		}
	}
}

var (
	localIPOnce sync.Once
	localIPAddr string
	localIPHex  = traceUnknownIPHex
)

// localIPv4 resolves the first global-unicast IPv4 address once per
// process. It reads interface addresses directly instead of resolving
// the hostname, so a slow or broken DNS setup cannot stall an invoke.
func localIPv4() (string, string) {
	localIPOnce.Do(func() {
		addrs, err := net.InterfaceAddrs()
		if err != nil {
			return
		}
		for _, addr := range addrs {
			ipNet, ok := addr.(*net.IPNet)
			if !ok {
				continue
			}
			ip := ipNet.IP.To4()
			if ip == nil || !ip.IsGlobalUnicast() {
				continue
			}
			localIPAddr = ip.String()
			localIPHex = fmt.Sprintf("%02x%02x%02x%02x", ip[0], ip[1], ip[2], ip[3])
			return
		}
	})
	return localIPAddr, localIPHex
}
