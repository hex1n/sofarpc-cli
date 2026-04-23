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
	TargetInvalid        Code = "target.invalid"
	TargetUnreachable    Code = "target.unreachable"
	TargetConnectFailed  Code = "target.connect-failed"
	ServiceMissing       Code = "input.service-missing"
	MethodMissing        Code = "input.method-missing"
	ArgsInvalid          Code = "input.args-invalid"
	MethodAmbiguous      Code = "contract.method-ambiguous"
	MethodNotFound       Code = "contract.method-not-found"
	ContractUnresolvable Code = "contract.unresolvable"

	// Workspace / project errors.
	FacadeNotConfigured Code = "workspace.facade-not-configured"

	// Replay / captured plan errors.
	PlanVersionUnsupported Code = "replay.plan-version-unsupported"

	// Runtime / invoke errors.
	SerializeFailed    Code = "runtime.serialize-failed"
	DeserializeFailed  Code = "runtime.deserialize-failed"
	InvocationTimeout  Code = "runtime.timeout"
	ProtocolFailed     Code = "runtime.protocol-failed"
	InvocationRejected Code = "runtime.rejected"
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
