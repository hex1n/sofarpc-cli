package mcp

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/hex1n/sofarpc-cli/internal/core/contract"
	"github.com/hex1n/sofarpc-cli/internal/core/invocationprops"
	"github.com/hex1n/sofarpc-cli/internal/core/invoke"
	"github.com/hex1n/sofarpc-cli/internal/core/target"
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

func registerDoctor(server *sdkmcp.Server, opts Options, holder *contractHolder) {
	sources := opts.TargetSources
	sessions := opts.Sessions
	sdkmcp.AddTool(server, &sdkmcp.Tool{
		Name:        "sofarpc_doctor",
		Title:       "Diagnose SOFARPC Workspace",
		Description: "Run end-to-end self-diagnosis: target resolution, reachability, workspace state, and session readiness.",
		Annotations: networkReadOnlyAnnotations("Diagnose SOFARPC Workspace"),
	}, func(ctx context.Context, req *sdkmcp.CallToolRequest, in DoctorInput) (*sdkmcp.CallToolResult, DoctorOutput, error) {
		notifyToolProgress(ctx, req, 0, 3, "resolving diagnostic context")
		toolCtx, err := resolveToolContext(sources, sessions, holder, in.SessionID, in.Cwd, in.Project)
		if err != nil {
			out := DoctorOutput{
				Checks: []DoctorCheck{{
					Name:   "scope",
					Ok:     false,
					Detail: err.Error(),
					NextStep: &DoctorAction{
						Tool: "sofarpc_open",
					},
				}},
			}
			out.Ok = false
			out.Summary = summarizeDoctor(out.Checks)
			return toolResult(out, out.Summary, true), out, nil
		}
		// Effective profile: per-call wins over the session's profile; mutating
		// the local input lets every check see the same selection. An empty
		// value still lets target resolution fall back to defaultProfile.
		in.Profile = effectiveProfile(in.Profile, toolCtx.SessionProfile)

		notifyToolProgress(ctx, req, 1, 3, "running diagnostic checks")
		checks := make([]DoctorCheck, 6)
		var wg sync.WaitGroup
		wg.Add(6)
		go func() { defer wg.Done(); checks[0] = checkTarget(in, toolCtx.Sources) }()
		go func() {
			defer wg.Done()
			checks[1] = checkContract(toolCtx.Contract, toolCtx.ProjectRoot, holder.ProjectCacheDiagnostics())
		}()
		go func() { defer wg.Done(); checks[2] = checkSessions(sessions) }()
		go func() { defer wg.Done(); checks[3] = checkInvokePolicy(in, toolCtx.Sources) }()
		go func() { defer wg.Done(); checks[4] = checkInvocationProperties(in, toolCtx.Sources) }()
		go func() { defer wg.Done(); checks[5] = checkProfile(in, toolCtx.Sources) }()
		wg.Wait()
		out := DoctorOutput{Checks: checks}
		out.Ok = allOk(out.Checks)
		out.Summary = summarizeDoctor(out.Checks)

		result := toolResult(out, out.Summary, false)
		if !out.Ok {
			result.IsError = true
		}
		notifyToolProgress(ctx, req, 3, 3, "diagnostics complete")
		return result, out, nil
	})
}

// checkProfile reports the resolved Target Profile selection. It fails when a
// named profile is defined in neither config file (the same hard error invoke
// and open raise), so an agent sees the typo before any target check confuses
// it with a missing endpoint.
func checkProfile(in DoctorInput, sources target.Sources) DoctorCheck {
	report := target.Resolve(target.Input{Service: in.Service, Profile: in.Profile}, sources)
	data := map[string]any{}
	if len(report.AvailableProfiles) > 0 {
		data["availableProfiles"] = report.AvailableProfiles
	}
	if report.ActiveProfile != "" {
		data["activeProfile"] = report.ActiveProfile
	}
	if dp := strings.TrimSpace(sources.DefaultProfile); dp != "" {
		data["defaultProfile"] = dp
	}
	if report.ProfileError != "" {
		return DoctorCheck{
			Name:     "profile",
			Ok:       false,
			Detail:   report.ProfileError,
			Data:     data,
			NextStep: &DoctorAction{Tool: "sofarpc_target", Args: map[string]any{"explain": true}},
		}
	}
	if report.ActiveProfile == "" {
		detail := "no target profile selected; base target settings apply"
		if len(report.AvailableProfiles) > 0 {
			detail += " (available: " + strings.Join(report.AvailableProfiles, ", ") + ")"
		}
		return DoctorCheck{Name: "profile", Ok: true, Detail: detail, Data: data}
	}
	return DoctorCheck{
		Name:   "profile",
		Ok:     true,
		Detail: fmt.Sprintf("active profile %q", report.ActiveProfile),
		Data:   data,
	}
}

// profileNotDefinedCheck converts a profile-resolution error into a failing
// check under the caller's name, so target / invoke-policy / invocation-property
// checks defer to the dedicated profile check instead of emitting a misleading
// "no target mode" message. It returns nil when the profile resolved cleanly.
func profileNotDefinedCheck(name string, report target.Report) *DoctorCheck {
	if report.ProfileError == "" {
		return nil
	}
	return &DoctorCheck{
		Name:     name,
		Ok:       false,
		Detail:   report.ProfileError,
		Data:     map[string]any{"availableProfiles": report.AvailableProfiles},
		NextStep: &DoctorAction{Tool: "sofarpc_target", Args: map[string]any{"explain": true}},
	}
}

func checkInvocationProperties(in DoctorInput, sources target.Sources) DoctorCheck {
	if c := profileNotDefinedCheck("invocation-properties", target.Resolve(target.Input{Profile: in.Profile}, sources)); c != nil {
		return *c
	}
	props, err := invocationprops.Merge(target.InvocationPropertySources(in.InvocationProperties, in.Profile, sources)...)
	data := map[string]any{}
	if strings.TrimSpace(sources.ProjectRoot) != "" {
		data["targetRoot"] = sources.ProjectRoot
	}
	if err != nil {
		return DoctorCheck{
			Name:   "invocation-properties",
			Ok:     false,
			Detail: err.Error(),
			Data:   data,
			NextStep: &DoctorAction{
				Tool: "sofarpc_doctor",
			},
		}
	}
	data["properties"] = invocationPropertySummary(props)
	data["wireCarrier"] = "SofaRequest.requestProps[\"rpc_req_baggage\"]"
	data["servicePrerequisite"] = "target SOFARPC runtime must have invoke.baggage.enable enabled for RpcInvokeContext.getRequestBaggage(...)"
	statuses, err := invocationprops.CheckEnv(props, os.LookupEnv)
	if err != nil {
		return DoctorCheck{
			Name:   "invocation-properties",
			Ok:     false,
			Detail: err.Error(),
			Data:   data,
			NextStep: &DoctorAction{
				Tool: "sofarpc_doctor",
			},
		}
	}
	if len(statuses) > 0 {
		data["env"] = statuses
	}
	missing := missingInvocationPropertyEnv(statuses)
	if len(missing) > 0 {
		return DoctorCheck{
			Name:   "invocation-properties",
			Ok:     false,
			Detail: "env references are missing or empty: " + strings.Join(missing, ", "),
			Data:   data,
			NextStep: &DoctorAction{
				Tool: "sofarpc_doctor",
			},
		}
	}
	if len(props) == 0 {
		return DoctorCheck{
			Name:   "invocation-properties",
			Ok:     true,
			Detail: "no invocation properties configured",
			Data:   data,
		}
	}
	detail := fmt.Sprintf("%d invocation property key(s) configured for SOFARPC request baggage", len(props))
	if len(statuses) > 0 {
		detail += fmt.Sprintf("; %d env reference(s) resolvable", len(statuses))
	}
	detail += "; target service must have invoke.baggage.enable enabled"
	return DoctorCheck{
		Name:   "invocation-properties",
		Ok:     true,
		Detail: detail,
		Data:   data,
	}
}

func invocationPropertySummary(props invocationprops.Declarations) map[string]any {
	summary := map[string]any{
		"count": len(props),
	}
	var literalKeys []string
	var envKeys []string
	for _, key := range invocationprops.SortedKeys(props) {
		decl := props[key]
		switch {
		case decl.Value != nil:
			literalKeys = append(literalKeys, key)
		case strings.TrimSpace(decl.Env) != "":
			envKeys = append(envKeys, key)
		}
	}
	if len(literalKeys) > 0 {
		summary["literalKeys"] = literalKeys
	}
	if len(envKeys) > 0 {
		summary["envKeys"] = envKeys
	}
	return summary
}

func missingInvocationPropertyEnv(statuses []invocationprops.EnvStatus) []string {
	var missing []string
	for _, status := range statuses {
		if !status.Present || status.Empty {
			missing = append(missing, status.Key+"="+status.Env)
		}
	}
	return missing
}

// checkTarget runs the same resolver+probe chain as sofarpc_target and
// collapses the result into two checks: resolution and reachability.
func checkTarget(in DoctorInput, sources target.Sources) DoctorCheck {
	report := target.Resolve(target.Input{Service: in.Service, Profile: in.Profile}, sources)
	if c := profileNotDefinedCheck("target", report); c != nil {
		return *c
	}
	data := map[string]any{}
	if strings.TrimSpace(sources.ProjectRoot) != "" {
		data["targetRoot"] = sources.ProjectRoot
	}
	if len(report.ConfigErrors) > 0 {
		data["configErrors"] = report.ConfigErrors
		return DoctorCheck{
			Name:   "target",
			Ok:     false,
			Detail: "project target config could not be loaded",
			Data:   data,
			NextStep: &DoctorAction{
				Tool: "sofarpc_target",
				Args: targetHintArgs(sources),
			},
		}
	}
	if report.Target.Mode == "" {
		return DoctorCheck{
			Name:   "target",
			Ok:     false,
			Detail: "no layer supplied a target mode (direct or registry)",
			NextStep: &DoctorAction{
				Tool: "sofarpc_target",
				Args: targetHintArgs(sources),
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
				Args: targetHintArgs(sources),
			},
		}
	}
	return DoctorCheck{
		Name:   "target",
		Ok:     true,
		Detail: fmt.Sprintf("mode=%s target=%s reachable", report.Target.Mode, probe.Target),
		Data:   data,
	}
}

func checkInvokePolicy(in DoctorInput, sources target.Sources) DoctorCheck {
	policy := executionPolicyFromEnv(sources)
	policyDiagnostics := policy.Diagnostics()
	report := target.Resolve(target.Input{Service: in.Service, Profile: in.Profile}, sources)
	if c := profileNotDefinedCheck("invoke-policy", report); c != nil {
		return *c
	}
	data := map[string]any{
		"policy":             policyDiagnostics,
		"supportsDirectBolt": target.SupportsDirectBolt(report.Target),
	}
	if strings.TrimSpace(sources.ProjectRoot) != "" {
		data["targetRoot"] = sources.ProjectRoot
	}
	if report.Target.Mode != "" {
		data["target"] = report.Target
	}

	if !policy.AllowInvoke {
		return DoctorCheck{
			Name:   "invoke-policy",
			Ok:     false,
			Detail: invoke.EnvAllowInvoke + " is not enabled; real invoke and replay are blocked",
			Data:   data,
			NextStep: &DoctorAction{
				Tool: "sofarpc_invoke",
				Args: map[string]any{"dryRun": true},
			},
		}
	}
	if len(report.ConfigErrors) > 0 {
		return DoctorCheck{
			Name:   "invoke-policy",
			Ok:     false,
			Detail: "project target config errors block real invoke",
			Data:   data,
			NextStep: &DoctorAction{
				Tool: "sofarpc_target",
				Args: targetHintArgs(sources),
			},
		}
	}
	if !policyDiagnostics.AllowedServicesConfigured {
		return DoctorCheck{
			Name:   "invoke-policy",
			Ok:     false,
			Detail: "project allowedServices is required for real invoke",
			Data:   data,
			NextStep: &DoctorAction{
				Tool: "sofarpc_init_project",
				Args: initProjectPolicyHintArgs(sources),
			},
		}
	}
	if strings.TrimSpace(in.Service) != "" && !policy.ServiceAllowed(in.Service) {
		source := strings.TrimSpace(policy.AllowedServicesSource)
		if source == "" {
			source = "configured service allowlist"
		}
		return DoctorCheck{
			Name:   "invoke-policy",
			Ok:     false,
			Detail: fmt.Sprintf("service %q is not allowed by %s", in.Service, source),
			Data:   data,
			NextStep: &DoctorAction{
				Tool: "sofarpc_doctor",
			},
		}
	}
	if report.Target.Mode == "" {
		return DoctorCheck{
			Name:   "invoke-policy",
			Ok:     false,
			Detail: "real invoke policy is enabled, but no executable target is resolved",
			Data:   data,
			NextStep: &DoctorAction{
				Tool: "sofarpc_target",
				Args: map[string]any{"explain": true},
			},
		}
	}
	if !target.SupportsDirectBolt(report.Target) {
		return DoctorCheck{
			Name:   "invoke-policy",
			Ok:     false,
			Detail: fmt.Sprintf("pure-Go invoke supports only direct+bolt+hessian2; got mode=%s protocol=%s serialization=%s", report.Target.Mode, report.Target.Protocol, report.Target.Serialization),
			Data:   data,
			NextStep: &DoctorAction{
				Tool: "sofarpc_target",
				Args: targetHintArgs(sources),
			},
		}
	}
	if err := policy.ValidateTarget(invoke.Plan{Target: report.Target}, "doctor"); err != nil {
		return DoctorCheck{
			Name:   "invoke-policy",
			Ok:     false,
			Detail: err.Error(),
			Data:   data,
			NextStep: &DoctorAction{
				Tool: "sofarpc_target",
				Args: targetHintArgs(sources),
			},
		}
	}
	detail := "real invoke policy allows the resolved direct target"
	if len(policy.AllowedServices) > 0 && strings.TrimSpace(in.Service) == "" {
		detail += "; pass service to doctor to verify service allowlisting"
	}
	return DoctorCheck{
		Name:   "invoke-policy",
		Ok:     true,
		Detail: detail,
		Data:   data,
	}
}

func checkContract(snapshot contractSnapshot, targetRoot string, cache map[string]any) DoctorCheck {
	store := snapshot.store
	loadErr := snapshot.loadError
	contractRoot := snapshot.root
	data := contractRootData(targetRoot, contractRoot)
	if len(cache) > 0 {
		data["cache"] = cache
	}
	if rootsMismatch(targetRoot, contractRoot) {
		return DoctorCheck{
			Name:   "contract",
			Ok:     false,
			Detail: fmt.Sprintf("target root %s does not match contract root %s", targetRoot, contractRoot),
			Data:   data,
			NextStep: &DoctorAction{
				Tool: "sofarpc_open",
				Args: openHintArgs(targetRoot),
			},
		}
	}
	if store == nil {
		if loadErr != "" {
			data["loadError"] = loadErr
			return DoctorCheck{
				Name:   "contract",
				Ok:     false,
				Detail: "contract store failed to load: " + loadErr + "; trusted-mode invoke still works",
				Data:   data,
				NextStep: &DoctorAction{
					Tool: "sofarpc_open",
					Args: openHintArgs(targetRoot),
				},
			}
		}
		return DoctorCheck{
			Name:   "contract",
			Ok:     true,
			Detail: "no contract information attached; describe is unavailable, trusted-mode invoke still works",
			Data:   data,
		}
	}
	banner, ok := store.(interface{ Size() int })
	if !ok {
		return DoctorCheck{
			Name:   "contract",
			Ok:     true,
			Detail: "contract information attached",
			Data:   data,
		}
	}
	size := banner.Size()
	diagProvider, hasDiagnostics := store.(contract.DiagnosticStore)
	if hasDiagnostics {
		diag := diagProvider.Diagnostics()
		data["indexedClasses"] = diag.IndexedClasses
		data["indexedFiles"] = diag.IndexedFiles
		data["parsedClasses"] = diag.ParsedClasses
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
			Data:   data,
		}
	}
	return DoctorCheck{
		Name:   "contract",
		Ok:     true,
		Detail: fmt.Sprintf("contract information attached (%d class(es))", size),
		Data:   data,
	}
}

func contractRootData(targetRoot, contractRoot string) map[string]any {
	data := map[string]any{}
	if strings.TrimSpace(targetRoot) != "" {
		data["targetRoot"] = targetRoot
	}
	if strings.TrimSpace(contractRoot) != "" {
		data["contractRoot"] = contractRoot
	}
	return data
}

func rootsMismatch(targetRoot, contractRoot string) bool {
	targetRoot = strings.TrimSpace(targetRoot)
	contractRoot = strings.TrimSpace(contractRoot)
	if targetRoot == "" || contractRoot == "" {
		return false
	}
	return !sameProjectRoot(targetRoot, contractRoot)
}

func openHintArgs(projectRoot string) map[string]any {
	if strings.TrimSpace(projectRoot) == "" {
		return nil
	}
	return map[string]any{"project": projectRoot}
}

// checkSessions reports the session store's current load relative to its
// capacity and captured-plan byte limit. It is purely informational — Ok is
// always true — so adding it never downgrades the overall doctor verdict.
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
	maxPlanBytes := store.MaxPlanBytes()
	data := map[string]any{
		"size":         size,
		"capacity":     capacity,
		"maxPlanBytes": maxPlanBytes,
	}
	planLimit := fmt.Sprintf("session plan max=%d bytes", maxPlanBytes)
	if maxPlanBytes <= 0 {
		planLimit = "session plan capture unbounded"
	}
	if capacity <= 0 {
		return DoctorCheck{
			Name:   "sessions",
			Ok:     true,
			Detail: fmt.Sprintf("%d session(s); capacity unbounded; %s", size, planLimit),
			Data:   data,
		}
	}
	return DoctorCheck{
		Name:   "sessions",
		Ok:     true,
		Detail: fmt.Sprintf("%d/%d session(s); LRU evicts on overflow; %s", size, capacity, planLimit),
		Data:   data,
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
