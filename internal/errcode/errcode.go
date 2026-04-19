// Package errcode defines the structured error taxonomy surfaced to agents.
//
// Every error returned to an agent carries a stable Code plus an optional
// Hint that names the tool the agent should call next and the arguments it
// should pass. Agents recover without reading prose.
package errcode

// Code is a stable identifier for an error class. Agents branch on Code;
// humans read the accompanying message. Values are dotted, lower-kebab per
// segment, and MUST NOT be renamed once released.
type Code string

const (
	// Input / configuration errors.
	TargetMissing        Code = "target.missing"
	TargetUnreachable    Code = "target.unreachable"
	ServiceMissing       Code = "input.service-missing"
	MethodMissing        Code = "input.method-missing"
	ArgsInvalid          Code = "input.args-invalid"
	MethodAmbiguous      Code = "contract.method-ambiguous"
	MethodNotFound       Code = "contract.method-not-found"
	ContractUnresolvable Code = "contract.unresolvable"

	// Workspace / project errors.
	FacadeNotConfigured Code = "workspace.facade-not-configured"
	IndexStale          Code = "workspace.index-stale"
	// IndexerFailed is surfaced when the indexer subprocess itself
	// failed (jar path wrong, Spoon crashed, timeout, …). Distinct from
	// IndexStale, which means the existing index is out of date and a
	// retry-with-refresh is likely to fix it; IndexerFailed means the
	// indexer cannot produce a fresh index at all, so the fix is
	// configuration-side rather than agent-retryable.
	IndexerFailed Code = "workspace.indexer-failed"

	// Runtime / daemon errors.
	DaemonUnavailable  Code = "runtime.daemon-unavailable"
	WorkerError        Code = "runtime.worker-error"
	DeserializeFailed  Code = "runtime.deserialize-failed"
	InvocationTimeout  Code = "runtime.timeout"
	InvocationRejected Code = "runtime.rejected"
	// InvocationUncertain is surfaced when the worker disconnected after
	// the client had already written the request but before a response
	// arrived. The outcome is unknowable from the client side, so the
	// agent must decide whether to retry — safe for idempotent calls,
	// unsafe otherwise. Distinct from DaemonUnavailable (worker never
	// reached) and WorkerError (worker answered with a failure).
	InvocationUncertain Code = "runtime.invocation-uncertain"
)

// Hint points the agent at its next action. NextTool names a tool the agent
// can already see; NextArgs is the argument object to pass into it. Both are
// optional — a hint may carry only NextArgs to pre-fill a form, or only
// NextTool to suggest a diagnostic.
type Hint struct {
	NextTool string         `json:"nextTool,omitempty"`
	NextArgs map[string]any `json:"nextArgs,omitempty"`
	Reason   string         `json:"reason,omitempty"`
}

// Error is the canonical structured error. Code + Message are always set;
// Phase groups errors by pipeline stage (resolve / contract / invoke);
// Hint carries the agent-facing recovery suggestion.
type Error struct {
	Code    Code   `json:"code"`
	Message string `json:"message"`
	Phase   string `json:"phase,omitempty"`
	Hint    *Hint  `json:"hint,omitempty"`
}

func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	if e.Message != "" {
		return e.Message
	}
	return string(e.Code)
}

// New builds an Error without a hint.
func New(code Code, phase, message string) *Error {
	return &Error{Code: code, Phase: phase, Message: message}
}

// WithHint attaches a hint and returns the same error for chaining.
func (e *Error) WithHint(nextTool string, nextArgs map[string]any, reason string) *Error {
	if e == nil {
		return nil
	}
	e.Hint = &Hint{NextTool: nextTool, NextArgs: nextArgs, Reason: reason}
	return e
}
