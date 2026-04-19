package mcp

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/hex1n/sofarpc-cli/internal/core/contract"
	"github.com/hex1n/sofarpc-cli/internal/core/target"
	"github.com/hex1n/sofarpc-cli/internal/worker"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

// DoctorOutput is the structured payload for sofarpc_doctor. Each Check
// is self-describing so agents can iterate and fix one problem at a time.
// Ok at the top is the AND of every check.Ok.
type DoctorOutput struct {
	Ok      bool          `json:"ok"`
	Summary string        `json:"summary"`
	Checks  []DoctorCheck `json:"checks"`
}

// DoctorCheck is one diagnostic line. NextStep is omitted when the check
// passed; when it fails, the agent should prefer this over guessing.
type DoctorCheck struct {
	Name     string        `json:"name"`
	Ok       bool          `json:"ok"`
	Detail   string        `json:"detail,omitempty"`
	NextStep *DoctorAction `json:"nextStep,omitempty"`
}

// DoctorAction is a minimal nextStep payload — kept separate from
// errcode.Hint because doctor is advisory, not an error response.
type DoctorAction struct {
	Tool string         `json:"tool"`
	Args map[string]any `json:"args,omitempty"`
}

func registerDoctor(server *sdkmcp.Server, opts Options, holder *facadeHolder) {
	sources := opts.TargetSources
	client := opts.Worker
	sessions := opts.Sessions
	canReindex := opts.Reindexer != nil
	sdkmcp.AddTool(server, &sdkmcp.Tool{
		Name:        "sofarpc_doctor",
		Description: "Run end-to-end self-diagnosis: config resolution, indexer status, worker pool health, target reachability. The catch-all fallback when other tools return an unresolved error.",
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, in DoctorInput) (*sdkmcp.CallToolResult, DoctorOutput, error) {
		// Run the four checks in parallel. Worst-case serial latency is
		// probe-timeout (1s default) + ping-timeout (2s) + fast checks —
		// which stacks on top of the agent's request budget. The checks
		// are independent (no shared mutable state), so fanning out
		// collapses that to the slowest single check.
		checks := make([]DoctorCheck, 4)
		var wg sync.WaitGroup
		wg.Add(4)
		go func() { defer wg.Done(); checks[0] = checkTarget(in, sources) }()
		go func() { defer wg.Done(); checks[1] = checkIndexer(holder.Get(), canReindex) }()
		go func() { defer wg.Done(); checks[2] = checkWorker(ctx, client) }()
		go func() { defer wg.Done(); checks[3] = checkSessions(sessions) }()
		wg.Wait()
		out := DoctorOutput{Checks: checks}
		out.Ok = allOk(out.Checks)
		out.Summary = summarizeDoctor(out.Checks)

		result := &sdkmcp.CallToolResult{
			Content: []sdkmcp.Content{
				&sdkmcp.TextContent{Text: out.Summary},
			},
		}
		if !out.Ok {
			result.IsError = true
		}
		return result, out, nil
	})
}

// checkTarget runs the same resolver+probe chain as sofarpc_target and
// collapses the result into two checks: resolution and reachability.
func checkTarget(in DoctorInput, sources target.Sources) DoctorCheck {
	report := target.Resolve(target.Input{Service: in.Service}, sources)
	if report.Target.Mode == "" {
		return DoctorCheck{
			Name:   "target",
			Ok:     false,
			Detail: "no layer supplied a target mode (direct or registry)",
			NextStep: &DoctorAction{
				Tool: "sofarpc_target",
				Args: map[string]any{"explain": true},
			},
		}
	}
	probe := target.Probe(report.Target)
	if !probe.Reachable {
		return DoctorCheck{
			Name:   "target",
			Ok:     false,
			Detail: fmt.Sprintf("mode=%s target=%s unreachable: %s", report.Target.Mode, probe.Target, probe.Message),
			NextStep: &DoctorAction{
				Tool: "sofarpc_target",
				Args: map[string]any{"explain": true},
			},
		}
	}
	return DoctorCheck{
		Name:   "target",
		Ok:     true,
		Detail: fmt.Sprintf("mode=%s target=%s reachable", report.Target.Mode, probe.Target),
	}
}

// checkIndexer reports the facade index state. A nil store means the
// indexer hasn't run yet (or the project has no index). A populated
// store reports the service count. When canReindex is true, failures
// hint at sofarpc_describe refresh=true so an agent can self-heal;
// otherwise they explain the env the human needs to set.
func checkIndexer(facade contract.Store, canReindex bool) DoctorCheck {
	if facade == nil {
		return DoctorCheck{
			Name:     "indexer",
			Ok:       false,
			Detail:   "facade index not loaded; run the Spoon indexer over your project",
			NextStep: reindexHint(canReindex),
		}
	}
	banner, ok := facade.(interface{ Size() int })
	if !ok {
		return DoctorCheck{
			Name:   "indexer",
			Ok:     true,
			Detail: "facade store attached (in-memory)",
		}
	}
	size := banner.Size()
	if size == 0 {
		return DoctorCheck{
			Name:     "indexer",
			Ok:       false,
			Detail:   "facade index is empty — indexer produced no classes",
			NextStep: reindexHint(canReindex),
		}
	}
	return DoctorCheck{
		Name:   "indexer",
		Ok:     true,
		Detail: fmt.Sprintf("facade index loaded (%d classes)", size),
	}
}

// reindexHint picks the right nextStep for a missing / empty index.
// If a Reindexer is wired, the agent can self-heal via sofarpc_describe
// refresh=true; otherwise nothing the agent knows can fix it, so we
// omit the step to avoid a doctor→doctor loop and let the detail line
// carry the human instruction.
func reindexHint(canReindex bool) *DoctorAction {
	if !canReindex {
		return nil
	}
	return &DoctorAction{
		Tool: "sofarpc_describe",
		Args: map[string]any{"refresh": true},
	}
}

// checkWorker pings the worker pool. A nil client means the
// SOFARPC_RUNTIME_JAR / _DIGEST env pair wasn't set; otherwise we send
// a short-timeout Ping so a misconfigured or crashed worker surfaces
// here instead of during the next invoke.
//
// Worker failures never carry a nextStep: doctor is already the final
// fallback the agent would be pointed at, and nothing else the agent
// can call fixes a missing jar or a crashed JVM. The detail line
// instead carries the env the human needs to set.
func checkWorker(ctx context.Context, client *worker.Client) DoctorCheck {
	if client == nil {
		return DoctorCheck{
			Name:   "worker",
			Ok:     false,
			Detail: "worker not configured; set SOFARPC_RUNTIME_JAR and SOFARPC_RUNTIME_JAR_DIGEST",
		}
	}
	pingCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	resp, err := client.Invoke(pingCtx, worker.Request{Action: worker.ActionPing})
	if err != nil {
		return DoctorCheck{
			Name:   "worker",
			Ok:     false,
			Detail: "worker unreachable: " + err.Error(),
		}
	}
	if !resp.Ok {
		return DoctorCheck{
			Name:   "worker",
			Ok:     false,
			Detail: "worker ping returned Ok=false with no error payload",
		}
	}
	return DoctorCheck{
		Name:   "worker",
		Ok:     true,
		Detail: workerReadyDetail(client),
	}
}

// workerReadyDetail composes the Ok-path detail. Surfacing pool
// size/cap mirrors checkSessions so a multi-profile server can see
// when LRU is about to start evicting JVMs. Zero cap means unbounded —
// rendered as a lone size, matching the sessions format.
func workerReadyDetail(client *worker.Client) string {
	if client == nil || client.Pool == nil {
		return "worker ready"
	}
	size := client.Pool.Size()
	capacity := client.Pool.Cap()
	if capacity <= 0 {
		return fmt.Sprintf("worker ready; %d worker(s); capacity unbounded", size)
	}
	return fmt.Sprintf("worker ready; %d/%d worker(s); LRU evicts on overflow", size, capacity)
}

// checkSessions reports the session store's current load relative to its
// capacity. It is purely informational — Ok is always true — so adding
// it never downgrades the overall doctor verdict. Agents that expose a
// dashboard can read size/cap and surface a warning near full; the
// on-write LRU keeps the store correct even without external action.
func checkSessions(store *SessionStore) DoctorCheck {
	if store == nil {
		return DoctorCheck{
			Name:   "sessions",
			Ok:     true,
			Detail: "no session store attached",
		}
	}
	size := store.Size()
	capacity := store.Cap()
	if capacity <= 0 {
		return DoctorCheck{
			Name:   "sessions",
			Ok:     true,
			Detail: fmt.Sprintf("%d session(s); capacity unbounded", size),
		}
	}
	return DoctorCheck{
		Name:   "sessions",
		Ok:     true,
		Detail: fmt.Sprintf("%d/%d session(s); LRU evicts on overflow", size, capacity),
	}
}

func allOk(checks []DoctorCheck) bool {
	for _, c := range checks {
		if !c.Ok {
			return false
		}
	}
	return true
}

func summarizeDoctor(checks []DoctorCheck) string {
	parts := make([]string, 0, len(checks))
	for _, c := range checks {
		state := "ok"
		if !c.Ok {
			state = "fail"
		}
		parts = append(parts, c.Name+"="+state)
	}
	return strings.Join(parts, " ")
}
