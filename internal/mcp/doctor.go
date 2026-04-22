package mcp

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/hex1n/sofarpc-cli/internal/core/contract"
	"github.com/hex1n/sofarpc-cli/internal/core/target"
	"github.com/hex1n/sofarpc-cli/internal/sourcecontract"
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
	Name     string         `json:"name"`
	Ok       bool           `json:"ok"`
	Detail   string         `json:"detail,omitempty"`
	Data     map[string]any `json:"data,omitempty"`
	NextStep *DoctorAction  `json:"nextStep,omitempty"`
}

// DoctorAction is a minimal nextStep payload — kept separate from
// errcode.Hint because doctor is advisory, not an error response.
type DoctorAction struct {
	Tool string         `json:"tool"`
	Args map[string]any `json:"args,omitempty"`
}

func registerDoctor(server *sdkmcp.Server, opts Options, holder *contractHolder, loadErr string) {
	sources := opts.TargetSources
	sessions := opts.Sessions
	sdkmcp.AddTool(server, &sdkmcp.Tool{
		Name:        "sofarpc_doctor",
		Description: "Run end-to-end self-diagnosis: target resolution, reachability, workspace state, and session readiness.",
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, in DoctorInput) (*sdkmcp.CallToolResult, DoctorOutput, error) {
		checks := make([]DoctorCheck, 3)
		var wg sync.WaitGroup
		wg.Add(3)
		go func() { defer wg.Done(); checks[0] = checkTarget(in, sources) }()
		go func() { defer wg.Done(); checks[1] = checkContract(holder.Get(), loadErr) }()
		go func() { defer wg.Done(); checks[2] = checkSessions(sessions) }()
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

func checkContract(store contract.Store, loadErr string) DoctorCheck {
	if store == nil {
		if loadErr != "" {
			return DoctorCheck{
				Name:   "contract",
				Ok:     false,
				Detail: "contract store failed to load: " + loadErr + "; trusted-mode invoke still works",
				Data:   map[string]any{"loadError": loadErr},
				NextStep: &DoctorAction{
					Tool: "sofarpc_open",
				},
			}
		}
		return DoctorCheck{
			Name:   "contract",
			Ok:     true,
			Detail: "no contract information attached; describe is unavailable, trusted-mode invoke still works",
		}
	}
	banner, ok := store.(interface{ Size() int })
	if !ok {
		return DoctorCheck{
			Name:   "contract",
			Ok:     true,
			Detail: "contract information attached",
		}
	}
	size := banner.Size()
	diagProvider, hasDiagnostics := store.(interface {
		Diagnostics() sourcecontract.Diagnostics
	})
	if hasDiagnostics {
		diag := diagProvider.Diagnostics()
		data := map[string]any{
			"indexedClasses": diag.IndexedClasses,
			"indexedFiles":   diag.IndexedFiles,
			"parsedClasses":  diag.ParsedClasses,
		}
		if len(diag.IndexFailures) > 0 {
			data["indexFailures"] = diag.IndexFailures
		}
		if len(diag.ParseFailures) > 0 {
			data["parseFailures"] = diag.ParseFailures
		}
		detail := fmt.Sprintf("contract information attached (%d indexed class(es), %d parsed on demand)", diag.IndexedClasses, diag.ParsedClasses)
		if size == 0 {
			detail = "contract information attached but empty; describe may not return overloads"
		}
		if len(diag.ParseFailures) > 0 {
			detail += fmt.Sprintf("; %d parse failure(s) recorded", len(diag.ParseFailures))
		}
		return DoctorCheck{
			Name:   "contract",
			Ok:     true,
			Detail: detail,
			Data:   data,
		}
	}
	if size == 0 {
		return DoctorCheck{
			Name:   "contract",
			Ok:     true,
			Detail: "contract information attached but empty; describe may not return overloads",
		}
	}
	return DoctorCheck{
		Name:   "contract",
		Ok:     true,
		Detail: fmt.Sprintf("contract information attached (%d class(es))", size),
	}
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
