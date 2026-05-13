package dispatch

import "time"

// APIVersionV1Alpha1 is the envelope contract version shipped in TIDE v1alpha1.
// Consumers MUST reject envelopes whose apiVersion field does not match this
// constant via [ValidateAPIVersionKind].
const APIVersionV1Alpha1 = "tideproject.k8s/v1alpha1"

// KindTaskEnvelopeIn is the "kind" discriminator for input envelopes written
// by the orchestrator and consumed by subagent images.
const KindTaskEnvelopeIn = "TaskEnvelopeIn"

// KindTaskEnvelopeOut is the "kind" discriminator for output envelopes written
// by the in-pod harness and consumed by the controller on Job completion.
const KindTaskEnvelopeOut = "TaskEnvelopeOut"

// EnvelopeIn is the self-contained input document written by the orchestrator
// to /workspace/envelopes/{task-uid}/in.json on the per-Project PVC. The
// subagent pod mounts the PVC read-only; everything the agent needs to run is
// in this document (D-A4). Out-of-tree subagent image authors decode this type
// via [ValidateAPIVersionKind] + json.Unmarshal.
type EnvelopeIn struct {
	// APIVersion is pinned to [APIVersionV1Alpha1]. Harness MUST reject any
	// other value via [ValidateAPIVersionKind] before consuming any field.
	APIVersion string `json:"apiVersion"`

	// Kind is [KindTaskEnvelopeIn]. Harness MUST reject any other value.
	Kind string `json:"kind"`

	// TaskUID is the Kubernetes UID of the owning Task CRD object. Carried
	// through to EnvelopeOut so the controller correlates result envelopes
	// without scanning the PVC directory tree.
	TaskUID string `json:"taskUID"`

	// Role is the planner/executor selector: "planner" or "executor".
	Role string `json:"role"`

	// Level is the hierarchy level this task operates at:
	// "milestone" | "phase" | "plan" | "task".
	Level string `json:"level"`

	// Prompt is the full prompt body the subagent should act on.
	Prompt string `json:"prompt"`

	// FilesTouched is the set of repo-relative paths this task is expected to
	// read or write. Used by the harness for output-path validation (HARN-05)
	// and by the Plan admission webhook for file-touch consistency checks
	// (PLAN-02).
	FilesTouched []string `json:"filesTouched"`

	// DependsOn is the list of sibling Task names this task declares as
	// predecessors in the execution DAG. Omitted when empty (D-A4).
	DependsOn []string `json:"dependsOn,omitempty"`

	// DeclaredOutputPaths is the exhaustive list of PVC-relative paths the
	// harness is permitted to write. Any write outside this set is rejected by
	// the harness (HARN-05).
	DeclaredOutputPaths []string `json:"declaredOutputPaths"`

	// Caps carries per-task resource limits enforced by the in-pod harness
	// (HARN-02). The orchestrator derives these from Project.Spec.budget.
	Caps Caps `json:"caps"`

	// ProxyEndpoint is the localhost HTTPS endpoint for the signed-token
	// credential proxy sidecar (D-C1). Subagent sets ANTHROPIC_BASE_URL to
	// this value; the sidecar holds the real API key.
	ProxyEndpoint string `json:"proxyEndpoint"`

	// SignedToken is the HMAC-SHA256 token minted by the controller at
	// Job-create time. The credential proxy validates this token before
	// forwarding API requests (D-C3).
	SignedToken string `json:"signedToken"`

	// Dev carries test-fixture-only metadata injected by integration tests.
	// Real Claude-backed subagent images MUST ignore this struct entirely
	// (RESEARCH.md Pitfall 9 / D-F1). The field is omitted from JSON when nil
	// so production envelopes are not polluted with "dev: null".
	Dev *Dev `json:"dev,omitempty"`
}

// EnvelopeOut is the result document written by the in-pod harness to
// /workspace/envelopes/{task-uid}/out.json. The controller reads this file
// after the K8s Job terminates to record Task completion status and budget
// tally (D-A2).
type EnvelopeOut struct {
	// APIVersion is pinned to [APIVersionV1Alpha1].
	APIVersion string `json:"apiVersion"`

	// Kind is [KindTaskEnvelopeOut].
	Kind string `json:"kind"`

	// TaskUID mirrors EnvelopeIn.TaskUID so the controller can correlate
	// result envelopes without scanning directory names.
	TaskUID string `json:"taskUID"`

	// ExitCode is the subagent process exit code: 0 = success, non-zero = failure.
	ExitCode int `json:"exitCode"`

	// Result is a human-readable one-line summary of the task outcome. Written
	// by the harness on success or by the stub's canned response on testMode.
	Result string `json:"result"`

	// Reason carries a structured failure code when ExitCode != 0, e.g.
	// "forced-failure", "cap-hit", "output-path-violation", "token-expired".
	Reason string `json:"reason"`

	// Usage is the token and cost tally for this task, rolled up by the
	// controller into Project.Status.budget at Task completion (D-D2).
	Usage Usage `json:"usage"`

	// Artifacts is the list of PVC-relative paths the harness confirmed were
	// written during this task's execution. Used for audit and for the
	// declared-vs-actual output-path comparison (HARN-05).
	Artifacts []string `json:"artifacts"`

	// CompletedAt is the wall-clock time the harness wrote this envelope.
	CompletedAt time.Time `json:"completedAt"`
}

// Caps carries per-task resource limits (HARN-02). The orchestrator derives
// these from Project.Spec.budget and injects them into every EnvelopeIn so
// the in-pod harness can enforce them without querying the K8s API.
type Caps struct {
	// WallClockSeconds is the maximum wall-clock seconds the subagent is
	// permitted to run before the harness sends SIGTERM.
	WallClockSeconds int `json:"wallClockSeconds"`

	// Iterations is the maximum number of agent loop iterations before the
	// harness sends SIGTERM.
	Iterations int `json:"iterations"`

	// InputTokens is the maximum cumulative input tokens across all LLM calls.
	InputTokens int64 `json:"inputTokens"`

	// OutputTokens is the maximum cumulative output tokens across all LLM
	// calls in this task.
	OutputTokens int64 `json:"outputTokens"`
}

// Usage carries the token and cost tally reported by the harness in
// EnvelopeOut. Rolled up by the controller into Project.Status.budget at Task
// completion (D-D2).
type Usage struct {
	// InputTokens is the total input tokens consumed across all LLM calls
	// during this task's execution.
	InputTokens int64 `json:"inputTokens"`

	// OutputTokens is the total output tokens produced across all LLM calls
	// during this task's execution.
	OutputTokens int64 `json:"outputTokens"`

	// EstimatedCostCents is the estimated US-cent cost for this task, rounded
	// up to the nearest cent. Used for Project.Status.budget.costSpentCents
	// rollup.
	EstimatedCostCents int64 `json:"estimatedCostCents"`

	// Iterations is the actual number of agent loop iterations completed.
	Iterations int `json:"iterations"`
}

// Dev carries test-fixture-only metadata for the stub-subagent image. Real
// Claude-backed subagent images MUST ignore this struct entirely (D-F1 /
// RESEARCH.md Pitfall 9). It is intentionally kept in a sub-struct so the
// compiler makes omission explicit (pointer + omitempty on EnvelopeIn.Dev) and
// grep for "Dev" in production code is unambiguous.
type Dev struct {
	// TestMode is the stub behavior selector. Valid values:
	//   "success"              — writes canned result.json + declared artifacts; exit 0.
	//   "fail-exit-1"          — writes structured failure result; exit 1.
	//   "hang"                 — sleeps past caps.wallClockSeconds (HARN-02 test).
	//   "exceed-output-paths"  — writes outside declaredOutputPaths (HARN-05 test).
	// An empty string is treated as "success" by the stub.
	TestMode string `json:"testMode,omitempty"`
}

// ValidateAPIVersionKind checks that apiVersion equals [APIVersionV1Alpha1]
// and that kind equals expectedKind. It is the first call an envelope consumer
// must make before accessing any other field (D-A3).
//
// Returns [*UnknownAPIVersionError] if apiVersion is unrecognized.
// Returns [*UnknownKindError] if kind is unrecognized.
// Returns nil on success.
func ValidateAPIVersionKind(apiVersion, kind, expectedKind string) error {
	if apiVersion != APIVersionV1Alpha1 {
		return &UnknownAPIVersionError{APIVersion: apiVersion}
	}
	if kind != expectedKind {
		return &UnknownKindError{Kind: kind}
	}
	return nil
}
