/*
Copyright 2026 TIDE Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

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

	// Provider selects the LLM vendor + model + per-vendor tuning knobs the
	// subagent image should use to satisfy this dispatch (D-C3). Value type
	// (not pointer): every dispatch declares a provider. The dispatching
	// reconciler resolves Project.Spec.subagent.levels.{level}.{vendor,model,
	// params} → Project-level defaults → Helm-chart defaults and stamps the
	// resolved triple here. The provider implementation in
	// internal/subagent/{vendor}/ rejects an envelope whose Provider.Vendor
	// does not match its compiled-in sentinel — fail-fast defense against
	// image-tag-vs-envelope drift.
	Provider ProviderSpec `json:"provider"`

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

	// ChildCRDs is the slice of typed child-CRD declarations a planner
	// subagent emits for the orchestrator to materialize server-side (D-A1).
	// MilestoneReconciler reads Phase entries here; PhaseReconciler reads
	// Plan entries; PlanReconciler reads Task + Wave entries; TaskReconciler
	// (executor level) emits no ChildCRDs. Omitted from JSON when empty so
	// executor-level EnvelopeOut documents stay small. Consumers MUST
	// validate ChildCRDSpec.Kind against an allowlist before server-side
	// create — threat T-308 mitigation lives at the consumer site (plan
	// 03-08), not in this type.
	ChildCRDs []ChildCRDSpec `json:"childCRDs,omitempty"`

	// Git carries git-side output a push Job or a harness produces, in
	// particular the HeadSHA the per-Project run branch was advanced to at
	// this dispatch's level boundary. Pointer + omitempty so dispatches that
	// don't touch git (e.g. planner Jobs whose output is consumed only via
	// child-CRD materialization) don't serialize a "git: null" placeholder.
	Git *GitOutput `json:"git,omitempty"`
}

// GitOutput carries git-side output fields a dispatch produces, populated by
// the push Job at level-boundary push success and by executor harnesses
// committing per-Task worktree changes (D-B4 / D-B6). HeadSHA is the SHA the
// per-run branch was advanced to; ProjectReconciler patches
// Project.Status.git.lastPushedSHA from this value to enable the next push's
// --force-with-lease check (Pitfall 13 mitigation).
type GitOutput struct {
	// HeadSHA is the 40-character hex SHA the per-run branch points to after
	// this dispatch's level-boundary push completed. Required when Git is
	// non-nil.
	HeadSHA string `json:"headSHA"`
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

	// CacheReadTokens is the count of input tokens served from the provider's
	// prompt cache (D-C5). The Anthropic stream-json
	// usage.cache_read_input_tokens field maps directly here. Surfacing this
	// separately lets Project.Status.budget (Phase 2 D-D2) show the cached
	// vs uncached input mix instead of hiding cache hits inside InputTokens.
	CacheReadTokens int64 `json:"cacheReadTokens"`

	// CacheCreationTokens is the count of input tokens spent populating the
	// provider's prompt cache during this dispatch (D-C5). The Anthropic
	// stream-json usage.cache_creation_input_tokens field maps directly here.
	// Cache creation is billed at a higher rate than cache reads; separating
	// the two prevents budget rollups from understating real spend on
	// cache-warmup turns.
	CacheCreationTokens int64 `json:"cacheCreationTokens"`
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
