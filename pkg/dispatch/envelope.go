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

// APIVersionV1Alpha1 is the envelope contract version. The group
// "dispatch.tideproject.k8s" is the dispatch-contract's OWN subdomain group,
// deliberately decoupled from the CRD group "tideproject.k8s" (D-08) — a CRD
// version crank (each schema-revision bump) cannot collide with or
// accidentally bump the subagent-image envelope contract. The version
// component stays fixed; this is a pure group decoupling, not a stability
// claim (see pkg/dispatch/doc.go for the kubeadm precedent this follows).
// Consumers MUST reject envelopes whose apiVersion field does not match this
// constant via [ValidateAPIVersionKind].
const APIVersionV1Alpha1 = "dispatch.tideproject.k8s/v1alpha1"

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

	// Role is the planner/executor/verifier selector: "planner", "executor",
	// or "verifier".
	Role string `json:"role"`

	// Level is the hierarchy level this task operates at:
	// "milestone" | "phase" | "plan" | "task".
	Level string `json:"level"`

	// Prompt is the full prompt body the subagent should act on.
	// For executor dispatches this field is empty — the executor reads the
	// prompt in-pod from PromptPath off its own namespace PVC (defect #10b).
	// For planner dispatches (role="planner") this field carries the resolved
	// outcome prompt directly (the outcome prompt is not a PVC artifact).
	Prompt string `json:"prompt"`

	// PromptPath is the workspace-relative path of the executor prompt artifact
	// on the per-Project namespace PVC (e.g.
	// "envelopes/<plannerUID>/children/task-01.json"). Non-empty only for
	// executor dispatches (role="executor"). The in-pod anthropic runner reads
	// the .spec.prompt field from this artifact and renders it as {{.Prompt}}
	// in the compiled-in prompt template. The Manager MUST NOT read this path
	// cross-namespace; the read happens in-pod so it uses the executor's own
	// namespace PVC (defect #10b). Traversal-defended in-pod: absolute paths
	// and "../" escapes are rejected before the read.
	PromptPath string `json:"promptPath,omitempty"`

	// Branch is the per-run git branch (project.Status.Git.BranchName, format
	// "tide/run-<project>-<unix>") the executor checks out its isolated worktree
	// onto. Non-empty only for executor dispatches (role="executor"); planner
	// dispatches short-circuit worktree creation (harness.EnsureWorktree). The
	// in-pod executor reads this from in.json — the single source of truth for
	// the worktree branch (09-09: replaced the never-written branch.txt).
	Branch string `json:"branch,omitempty"`

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

	// LoopRunID is the outer Task-loop run identity, stable across all
	// repair attempts of one Task (D-01, EXEC-01): the Execution loop's
	// `loop.parent_run_id`. Derived deterministically as the owning Task's
	// UID — never minted or persisted. Stamped by the controller at dispatch
	// time; the executor echoes it verbatim onto EnvelopeOut.
	LoopRunID string `json:"loopRunID,omitempty"`

	// AttemptID is this individual execution attempt's identity (D-01,
	// EXEC-01): the Execution loop's `loop.run_id`, matching the per-attempt
	// Job-name tuple `podjob.JobName(taskUID, attempt) ->
	// "tide-task-{taskUID}-{attempt}"` (formatted here as
	// "{taskUID}-{attempt}", without the "tide-task-" prefix). Stamped by the
	// controller at dispatch time; the executor echoes it verbatim onto
	// EnvelopeOut. It never mints its own.
	AttemptID string `json:"attemptID,omitempty"`

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
	//
	// Provider.Params carries MODEL parameters only (temperature, thinking_budget,
	// …). It is NOT a general metadata bag — the anthropic runner hard-rejects
	// any key outside its allow-list. Dispatch metadata the planner needs to
	// build child-CRD refs lives in [EnvelopeIn.Dispatch], not here.
	Provider ProviderSpec `json:"provider"`

	// Dispatch carries orchestrator-side dispatch metadata the planner subagent
	// reads to build child-CRD references (e.g. parentName → projectRef /
	// milestoneRef / phaseRef / planRef) without querying the K8s API. It is
	// distinct from Provider.Params, which is model-parameters-only: overloading
	// Provider.Params with dispatch metadata tripped the anthropic runner's
	// strict param allow-list. Pointer + omitempty so executor-level and
	// real-Claude dispatches that don't consume it don't serialize a
	// "dispatch: null" placeholder. Real Claude-backed subagent images may
	// ignore this struct entirely — the real planner authors from the outcome
	// prompt, not canned parent refs.
	Dispatch *DispatchMeta `json:"dispatch,omitempty"`

	// Dev carries test-fixture-only metadata injected by integration tests.
	// Real Claude-backed subagent images MUST ignore this struct entirely
	// (RESEARCH.md Pitfall 9 / D-F1). The field is omitted from JSON when nil
	// so production envelopes are not polluted with "dev: null".
	Dev *Dev `json:"dev,omitempty"`

	// Verify carries verify-dispatch-specific input data. Populated when
	// Role=="verifier" (a verify dispatch, D-03) OR when Role=="executor"
	// carries a TASK-02 repair attempt's staged EvidencePacketPath (Phase 51
	// Plan 07, controller.buildEnvelopeIn) — GateCommand/Commands/
	// RequiredArtifacts/EvaluatorRef stay empty on the latter; only
	// EvidencePacketPath is set. Omitted from JSON when nil, mirroring
	// Dispatch/Dev.
	Verify *VerifyContext `json:"verify,omitempty"`

	// SharedContext is the wave-scoped shared context string emitted by the
	// parent planner and stamped byte-identically onto all wave siblings by
	// the controller at child dispatch time (Phase 20 CACHE-02/D-05). Grows
	// the stable prefix toward the provider's cacheable minimum (≥1,024 tokens
	// for Sonnet/Opus; 4,096 for Haiku — see PROJECT.md provider floor table).
	//
	// Planner templates reference {{.SharedContext}} in the D-07 reserved slot.
	// Executor dispatches (role="executor") never populate this field (CACHE-02
	// lock) — the executor template does not reference it.
	SharedContext string `json:"sharedContext,omitempty"`

	// RepairFindings carries a prior plan-check verdict's findings, condensed
	// to severity/confidence/summary, when this dispatch is a Phase 52 D-04
	// re-plan attempt (Role=="planner", Level=="plan"). Nil/empty on every
	// other dispatch — which today is every dispatch, since the plan-check
	// loop that populates this field lands in a later plan; plan_planner.tmpl
	// renders its {{if .RepairFindings}} block as nothing until then,
	// byte-identical to its pre-D-04 output. Omitted from JSON when empty,
	// mirroring SharedContext/Dispatch/Dev.
	RepairFindings []RepairFinding `json:"repairFindings,omitempty"`
}

// RepairFinding is a single finding rendered into a re-plan planner prompt's
// D-04 findings block (plan_planner.tmpl). Its shape is the compact,
// human-readable one-liner condensed FOR the fresh planner attempt —
// deliberately NOT [Finding] itself, whose Dimension/Evidence/SuggestedFix
// fields are the verifier's own wire format, not a planner-prompt summary.
type RepairFinding struct {
	// Severity is the originating finding's severity (e.g. "blocker",
	// "advisory"). Free string, mirroring [Finding.Severity].
	Severity string `json:"severity,omitempty"`

	// Confidence is the originating finding's confidence (e.g. "high",
	// "medium", "low"). Free string, mirroring [Finding.Confidence].
	Confidence string `json:"confidence,omitempty"`

	// Summary is a one-line, human-readable statement of the finding — the
	// compact evidence a fresh planner attempt reads instead of the prior
	// attempt's full context (D-04's plan-level analog of the Task loop's
	// evidence packet).
	Summary string `json:"summary,omitempty"`
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
	// Reason stays free-text diagnostic detail; it is complementary to
	// TerminalReason below, never a rename of it (D-02).
	Reason string `json:"reason"`

	// TerminalReason is the machine-checkable exit classification (D-02,
	// EXEC-02) — see [TerminalReason] for the closed 5-value enum and the
	// fail-closed "never a silent default" contract. Deliberately carries NO
	// omitempty: an unset reason MUST be visible as "" on the wire so a
	// silent-default bug is observable, never hidden by JSON omission.
	TerminalReason TerminalReason `json:"terminalReason"`

	// LoopRunID mirrors EnvelopeIn.LoopRunID, echoed verbatim by the executor
	// (D-01, EXEC-01) — the outer Task-loop run identity, stable across
	// repair attempts of this Task.
	LoopRunID string `json:"loopRunID,omitempty"`

	// AttemptID mirrors EnvelopeIn.AttemptID, echoed verbatim by the executor
	// (D-01, EXEC-01) — this individual execution attempt's identity.
	AttemptID string `json:"attemptID,omitempty"`

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

	// ChildCount is the number of child CRDs the planner authored. When
	// reading the tiny termination-message status (PodStatusEnvelopeReader),
	// this field is populated from the TerminationStub.ChildCount JSON key so
	// planner controllers can perform race-free succession gating without also
	// carrying the full ChildCRDs payloads (plan 09-08 Defect B). Zero for
	// executor-level dispatches and genuine leaf planners. When reading a full
	// out.json via FilesystemEnvelopeReader the field falls back to
	// len(ChildCRDs) in the planner controllers (non-Option-C path).
	ChildCount int `json:"childCount,omitempty"`

	// SharedContext is the curated wave-scoped shared context string the
	// parent planner emits for the orchestrator to stamp byte-identically onto
	// each sibling child's EnvelopeIn.SharedContext at dispatch time (D-05).
	// Empty for executor-level dispatches and genuine leaf planners (no
	// children to annotate). omitempty keeps executor out.json documents small.
	SharedContext string `json:"sharedContext,omitempty"`

	// Verdict carries the verifier's gate_decision. Populated only by
	// verifier dispatches (Phase 51) — schema/plumbing only this phase.
	// Pointer + omitempty so non-verify dispatches don't serialize a
	// "verdict: null" placeholder.
	Verdict *GateDecision `json:"verdict,omitempty"`

	// RunEvidence is the Phase-50 run-evidence contract (D-03, EXEC-03) — see
	// [RunEvidence] for the field-by-field mapping to the canonical
	// evals/README.md contract list. Pointer + omitempty so dispatches that
	// don't populate it don't serialize a "runEvidence: null" placeholder.
	RunEvidence *RunEvidence `json:"runEvidence,omitempty"`
}

// IsEnvelopeComplete reports whether env represents a fully-completed dispatch.
// A complete envelope has ExitCode==0 AND len(ChildCRDs)==ChildCount.
//
// The equality is strict by design:
//   - A legitimate executor-level envelope has ChildCount==0 and no ChildCRDs
//     (0==0, passes) — the leaf/executor case.
//   - A planner envelope that authored children must have ChildCount stamped to
//     len(ChildCRDs). A ChildCRDs slice with ChildCount==0 is MALFORMED for
//     import and is rejected — the count is the validation invariant that
//     guards against planner output truncation (WR-02).
//
// This is the single source of truth shared by the tide-import Job and the
// export tooling (RESUME-PARTIAL-01). Both callers must call this function;
// no caller should re-implement the check inline.
func IsEnvelopeComplete(env EnvelopeOut) bool {
	if env.ExitCode != 0 {
		return false
	}
	if len(env.ChildCRDs) != env.ChildCount {
		return false
	}
	return true
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

	// CacheSavingsCents is the realized savings in US cents from cache reads
	// during this task's execution. Computed by the provider (provider firewall
	// D-C1, Phase 21 OBSV-02): savings = CacheReadTokens × (inputRate −
	// cacheReadRate) / 1_000_000 with truncation division. Zero when
	// CacheReadTokens is zero — omitted from JSON in the common zero case.
	CacheSavingsCents int64 `json:"cacheSavingsCents,omitempty"`

	// PricingFallbackModel carries the unmatched model ID when this dispatch's
	// model missed the effective price table even after normalization and was
	// billed at the conservative (most-expensive) tier. Set by the provider
	// (provider firewall — Phase 38 COST-02 / D-02); the controller rolls it up
	// into a PricingFallbackActive Project condition and a Prometheus counter.
	// Empty in the common priced case — omitted from JSON so pre-Phase-38
	// envelopes stay byte-compatible.
	PricingFallbackModel string `json:"pricingFallbackModel,omitempty"`

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

// DispatchMeta carries orchestrator-side dispatch metadata the planner subagent
// reads to build child-CRD references without querying the K8s API. It is kept
// separate from [ProviderSpec.Params] (model-parameters-only) so the anthropic
// runner's strict param allow-list is not tripped by dispatch metadata. Real
// Claude-backed subagent images may ignore this struct — the real planner
// authors child CRDs from the outcome prompt, not canned parent refs.
type DispatchMeta struct {
	// ParentName is the up-stack CRD's metadata.name. The stub planner stamps
	// it into the child CRD's parent-spec ref (projectRef / milestoneRef /
	// phaseRef / planRef) so the controller can wire ownership without the
	// subagent holding a K8s client (T-07-03-03: parentName is metadata, not a
	// secret).
	ParentName string `json:"parentName,omitempty"`
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

// VerifyContext carries the minimal data a verifier dispatch needs (D-03).
// The pre-2026-07-18 three-Stage framing is dropped under the loop reframe —
// grow this per consumer, never a speculative superset.
type VerifyContext struct {
	// GateCommand is the single canonical PRIMARY pass-criterion command —
	// always Commands[0] below when Commands is non-empty (Plan 06 populates
	// both in lockstep from the LOCKED spec.verification, never
	// independently). Exit code parsed, never self-reported. Field name
	// carried forward from research/ARCHITECTURE.md:146 to avoid Phase 51
	// template churn.
	GateCommand string `json:"gateCommand,omitempty"`

	// Commands is the resolved, ordered pass-criteria list the verifier
	// executes out-of-band (Phase 51 Plan 06, D-01): the union
	// [GateCommand] ++ the planner-authored spec.verification.commands, so
	// GateCommand is guaranteed to run first and every additional authored
	// command also transports and executes — no authored command left
	// unexecuted. The Plan 02 verifier entrypoint iterates this full list
	// (env.verify.commands), not GateCommand alone. Omitted from JSON when
	// empty (non-verify dispatches, or a verify dispatch with no resolved
	// commands).
	Commands []string `json:"commands,omitempty"`

	// RequiredArtifacts lists workspace-relative paths the verifier confirms
	// exist.
	RequiredArtifacts []string `json:"requiredArtifacts,omitempty"`

	// EvaluatorRef names the evaluator config this dispatch resolves against
	// (same-namespace name ref, plain string — not corev1.LocalObjectReference).
	EvaluatorRef string `json:"evaluatorRef,omitempty"`

	// EvidencePacketPath is the PVC-relative path to the compact evidence
	// packet a repair attempt receives (original spec + evidence, never the
	// prior agent's full context — TASK-02). Empty on a fresh, non-repair
	// attempt.
	EvidencePacketPath string `json:"evidencePacketPath,omitempty"`
}

// TerminationStub is the tiny cross-namespace status carrier written to the
// dispatch Job's 4 KB termination message (Pod.status.containerStatuses[].
// state.terminated.message). It carries ONLY the fields the Manager needs to
// record Task completion without reading the PVC: ExitCode, Reason, Usage,
// the HeadSHA the run-branch was advanced to (if any), and ChildCount — the
// count of child CRDs the planner authored. ChildCRDs payloads and the verbose
// Result are intentionally excluded — they stay on the namespace-local PVC in
// out.json, which the in-namespace reporter Job reads (plan 09-05).
//
// ChildCount enables race-free succession gating (plan 09-08 Defect B): the
// controller compares observed owned children against this expected count and
// requeues until observed >= expected before declaring the level Succeeded.
//
// By construction this type MUST stay < 4 KB when marshalled; the test
// TestNewTerminationStub_StaysSmall enforces this invariant.
type TerminationStub struct {
	// ExitCode mirrors EnvelopeOut.ExitCode: 0 = success, non-zero = failure.
	ExitCode int `json:"exitCode"`

	// Reason mirrors EnvelopeOut.Reason: structured failure code (e.g.
	// "forced-failure", "cap-hit") when ExitCode != 0.
	Reason string `json:"reason"`

	// Usage mirrors EnvelopeOut.Usage: the token and cost tally for this
	// dispatch, rolled up by the Manager into Project.Status.budget.
	Usage Usage `json:"usage"`

	// HeadSHA is the git HeadSHA from EnvelopeOut.Git.HeadSHA, flattened here
	// to avoid a nested pointer. Empty when the dispatch did not produce a git
	// push (executor-level, non-push Jobs). The Manager copies this into
	// Project.Status.git.lastPushedSHA for the --force-with-lease fence.
	HeadSHA string `json:"headSHA,omitempty"`

	// ChildCount is the number of child CRDs the planner authored
	// (len(EnvelopeOut.ChildCRDs)). Used by the four planner-level controllers
	// as the "expected" child count for race-free succession gating: a level
	// cannot Succeed until observed owned children >= ChildCount (plan 09-08).
	// Zero for executor-level dispatches and genuine leaf planners.
	ChildCount int `json:"childCount"`

	// GateDecision is the verdict enum string only (EVAL-05 / D-05a) — NEVER
	// a free-text summary. Empty on any non-verify dispatch.
	GateDecision string `json:"gateDecision,omitempty"`

	// FindingsCount is len(EnvelopeOut.Verdict.Findings). Zero on any
	// non-verify dispatch.
	FindingsCount int `json:"findingsCount,omitempty"`

	// HighSeverityCount is the count of findings whose Severity equals the
	// high-severity token (currently "blocker" — see [Finding.Severity]).
	// Zero on any non-verify dispatch.
	HighSeverityCount int `json:"highSeverityCount,omitempty"`

	// TerminalReason mirrors EnvelopeOut.TerminalReason as a plain string
	// (D-02) — the tiny termination-message carrier for the machine-checkable
	// exit classification. Empty only for pre-Phase-50 envelopes; a live
	// executor always sets EnvelopeOut.TerminalReason, so this is normally
	// non-empty.
	TerminalReason string `json:"terminalReason,omitempty"`

	// ChangedFileCount mirrors EnvelopeOut.RunEvidence.ChangedFileTotal
	// (D-03) — the bounded count-only summary of the changed-file manifest.
	// The full ChangedFiles array stays on the namespace-local PVC out.json;
	// never copied here (T-50-01).
	ChangedFileCount int `json:"changedFileCount,omitempty"`
}

// highSeverityFindingToken is the Finding.Severity value NewTerminationStub
// counts as "high severity" for TerminationStub.HighSeverityCount. A small
// package const (rather than a hardcoded literal at the call site) so
// Phase 51's rubric work can retune it in one place.
const highSeverityFindingToken = "blocker"

// NewTerminationStub builds a TerminationStub from a full EnvelopeOut by
// copying the tiny-status subset: ExitCode, Reason, Usage, Git.HeadSHA,
// ChildCount (= len(out.ChildCRDs)), and — when out.Verdict is non-nil — the
// verdict enum string plus two bounded findings counts. ChildCRD payloads,
// Result, Artifacts, and the full findings array are deliberately excluded.
// A nil out.Git is safe and produces an empty HeadSHA. A nil or empty
// ChildCRDs slice produces ChildCount == 0. A nil out.Verdict is safe and
// produces an empty GateDecision + zero counts.
func NewTerminationStub(out EnvelopeOut) TerminationStub {
	headSHA := ""
	if out.Git != nil {
		headSHA = out.Git.HeadSHA
	}
	stub := TerminationStub{
		ExitCode:   out.ExitCode,
		Reason:     out.Reason,
		Usage:      out.Usage,
		HeadSHA:    headSHA,
		ChildCount: len(out.ChildCRDs),
	}
	if out.Verdict != nil {
		stub.GateDecision = string(out.Verdict.Verdict)
		stub.FindingsCount = len(out.Verdict.Findings)
		for _, f := range out.Verdict.Findings {
			if f.Severity == highSeverityFindingToken {
				stub.HighSeverityCount++
			}
		}
	}
	stub.TerminalReason = string(out.TerminalReason)
	if out.RunEvidence != nil {
		stub.ChangedFileCount = out.RunEvidence.ChangedFileTotal
	}
	return stub
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
