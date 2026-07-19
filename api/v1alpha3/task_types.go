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

package v1alpha3

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Caps declares resource caps applied to the subagent pod executing a Task (Phase 2+).
//
// Design note: api/v1alpha3.Caps and pkg/dispatch.Caps are intentionally two
// separate types that serve different layers — this struct is CEL-validated at
// the CRD admission boundary, while pkg/dispatch.Caps is the Go-only public API
// used by the dispatcher. Plan 09's TaskReconciler.buildEnvelopeIn translates
// one to the other at dispatch time, keeping the CRD schema and the dispatch
// interface decoupled.
type Caps struct {
	// WallClockSeconds is the maximum wall-clock time for the subagent execution.
	// +kubebuilder:validation:Minimum=1
	// +optional
	WallClockSeconds int32 `json:"wallClockSeconds,omitempty"`

	// Iterations caps the number of agentic iterations (tool-call loops).
	// +kubebuilder:validation:Minimum=1
	// +optional
	Iterations int32 `json:"iterations,omitempty"`

	// InputTokens caps the number of input (prompt) tokens consumed per dispatch.
	// +kubebuilder:validation:Minimum=0
	// +optional
	InputTokens int64 `json:"inputTokens,omitempty"`

	// OutputTokens caps the number of output (completion) tokens produced per dispatch.
	// +kubebuilder:validation:Minimum=0
	// +optional
	OutputTokens int64 `json:"outputTokens,omitempty"`
}

// TaskDev carries developer/test-only overrides for the stub subagent (Phase 2+).
// This field mirrors the pkg/dispatch.Dev contract: keep rigidly scoped to
// dev/test namespaces — a future CEL rule can enforce the restriction (see
// Pitfall 9 / T-02-03-03).
type TaskDev struct {
	// TestMode overrides the stub subagent's exit behaviour (used by integration tests).
	// The wait-for-signal mode (Phase 3 D-D3) pins the stub at Running until the
	// orchestrator touches /workspace/envelopes/{task-uid}/release — required by
	// the chaos-resume Layer B spec to observe mid-wave leader handoff. See
	// cmd/stub-subagent/main.go:dispatchWaitForSignal for the polling contract.
	// +kubebuilder:validation:Enum=success;fail-exit-1;hang;exceed-output-paths;wait-for-signal
	// +optional
	TestMode string `json:"testMode,omitempty"`
}

// VerificationSpec is the planner-authored, immutable-once-locked
// verification contract for a Task (Phase 51 TASK-01/D-01). Mirrors the
// Gates/Caps standalone-type precedent — NOT inline TaskSpec fields — so the
// identical VerificationSpec shape generalizes to Plan.Spec/Project.Spec with
// a Task > Plan > Project precedence in Phase 52 (this phase is Task-scoped
// only; Plan/Project fields are OUT OF SCOPE here).
//
// Design note (RESEARCH Pitfall 2 / Open Question 1): the governing Phase/
// Version fields live HERE on spec — not on TaskStatus — because a CEL
// x-kubernetes-validations transition rule reading oldSelf can only see the
// prior state of its own spec subtree; a spec-level rule cannot reference
// status. Only the observed LockedSHA (the commit a dispatched attempt
// actually ran against) lives on TaskStatus.
//
// +kubebuilder:validation:XValidation:rule="oldSelf.phase != 'Locked' || self == oldSelf || self.phase == 'Superseded'",message="verification is immutable once Locked; supersede to a new version to change it"
type VerificationSpec struct {
	// Phase is the lifecycle state governing immutability: Draft is freely
	// editable, Locked is immutable except for a Locked->Superseded
	// transition (which mints a new version to carry further edits),
	// Superseded is a terminal historical record. Transition rules are
	// skipped on CREATE (no oldSelf) — a Draft-at-create is unconstrained.
	// +kubebuilder:validation:Enum=Draft;Locked;Superseded
	// +optional
	Phase string `json:"phase,omitempty"`

	// Version is the monotonic version number, incremented each time a
	// Locked contract is superseded by a new Draft.
	// +kubebuilder:validation:Minimum=1
	// +optional
	Version int32 `json:"version,omitempty"`

	// GateCommand is the single canonical primary pass-criterion command —
	// resolves onto pkg/dispatch.VerifyContext.GateCommand at verifier
	// dispatch time (Plan 06). This is planner-authored data EXECUTED FOR
	// REAL downstream, NOT decorative/status text: the independent verifier
	// runs it and parses the exit code (TASK-04); a non-zero exit dominates
	// any LLM judge's APPROVED verdict.
	// +optional
	GateCommand string `json:"gateCommand,omitempty"`

	// Commands lists additional acceptance-signal commands beyond
	// GateCommand. Plan 06 resolves this list onto the transported
	// VerifyContext.Commands list; the Plan 02 verifier executes EACH
	// command out-of-band (exit codes parsed, any non-zero dominates any
	// LLM APPROVED — TASK-04). Not decorative.
	// +optional
	Commands []string `json:"commands,omitempty"`

	// RequiredArtifacts lists workspace-relative paths the verifier
	// confirms exist.
	// +optional
	RequiredArtifacts []string `json:"requiredArtifacts,omitempty"`

	// Evaluator names the evaluator config this verification contract
	// resolves against (same-namespace name ref, plain string — not
	// corev1.LocalObjectReference).
	// +optional
	Evaluator string `json:"evaluator,omitempty"`

	// MaxIterations bounds the Task loop's repeat policy: the maximum
	// number of evaluator-driven fresh attempts the loop may dispatch
	// before halting (D-07/TASK-05).
	// +kubebuilder:validation:Minimum=0
	// +optional
	MaxIterations int32 `json:"maxIterations,omitempty"`

	// OnExhaustion declares the bounded exit path once MaxIterations is
	// reached without an APPROVED evaluation. In Phase 51 BOTH enum values
	// resolve IDENTICALLY to ConditionVerifyHalt + await `tide resume` —
	// per-value differentiation (a future LoopPolicy.EscalationPolicy) is
	// Phase 52 scope. Declared-but-uniform-now, never unenforced: both
	// values halt dispatch today.
	// +kubebuilder:validation:Enum=escalate;requireApproval
	// +optional
	OnExhaustion string `json:"onExhaustion,omitempty"`
}

// TaskSpec carries the executor envelope per D-F1 (retired), D-F2.
// In v1alpha3 the plan-local D-F1 restriction on DependsOn stays retired
// (retired in the prior schema revision); Tasks may declare dependencies on
// any node (Task/Plan/Phase/Milestone) in the Project.
type TaskSpec struct {
	// PlanRef is the name of the owning Plan (same namespace). Used for
	// ownership and Task listing; NOT a dep constraint.
	// +kubebuilder:validation:MinLength=1
	PlanRef string `json:"planRef"`

	// DependsOn lists the names of Tasks (or Plan/Phase/Milestone scope nodes)
	// in any Plan/Phase/Milestone of this Project that must complete before this
	// Task may dispatch. D-F1 (plan-local restriction) is retired — entries may
	// target Tasks in any Plan, any Phase, or any Milestone within this Project.
	// Coarse scope refs (naming a Plan/Phase/Milestone rather than a Task) are
	// fan-out expanded by the Phase 24 assembler (DEPS-02). Resolved in-memory
	// at assembly time (D-05); authored coarse dependsOn is the only persisted
	// truth.
	// +optional
	// +kubebuilder:validation:items:MinLength=1
	// +kubebuilder:validation:XValidation:rule="!self.exists(d, d == '')",message="dependsOn entries must not be empty strings"
	DependsOn []string `json:"dependsOn,omitempty"`

	// FilesTouched declares the workspace paths this Task intends to write (D-F2).
	// +kubebuilder:validation:MinItems=1
	FilesTouched []string `json:"filesTouched"`

	// PromptPath is the PVC-relative path (under the per-Project workspace root)
	// to the durable children/task-NN.json artifact this Task was materialized
	// from. The executor instruction lives at .spec.prompt inside that file; the
	// controller reads it FRESH from the PVC on every dispatch (buildEnvelopeIn →
	// EnvelopeIn.Prompt). Editing the children file and re-dispatching re-applies
	// the edit with nothing to clobber — the file is the source of truth, not a
	// cached CRD body. The materializer always sets this; a Task without it is
	// undispatchable, so it is required at the API boundary (MinLength=1).
	//
	// Path is workspace-relative (e.g. "envelopes/<plannerUID>/children/task-01.json")
	// so it resolves under both the controller PVC mount and the executor pod mount.
	// +kubebuilder:validation:MinLength=1
	PromptPath string `json:"promptPath"`

	// DeclaredOutputPaths declares the output artifact paths the subagent must produce
	// (Phase 2+). Plan 06's harness output validator enforces against this set (HARN-05).
	// +kubebuilder:validation:MinItems=1
	DeclaredOutputPaths []string `json:"declaredOutputPaths"`

	// Gates declares per-level gate policy for this Task (Phase 25 DISP-03).
	// When Gates.Task == "approve", the task controller parks this Task at
	// AwaitingApproval after its global indegree reaches 0 — implementing the
	// task-level approve gate that composes with global dispatch readiness.
	// Default (zero-value) is "auto" (no hold). Mirrors the Gates pattern on
	// ProjectSpec; the Task-level field takes precedence over the Project-level
	// Project.Spec.Gates.Task when evaluating task gate policy.
	// +optional
	Gates Gates `json:"gates,omitempty"`

	// Caps optionally restricts subagent resource usage (Phase 2+).
	// +optional
	Caps *Caps `json:"caps,omitempty"`

	// Verification declares the planner-authored, immutable-once-locked
	// pass-criteria contract for this Task (Phase 51 TASK-01). Mirrors the
	// Gates precedence-doc pattern: Task-scoped only in this phase; the
	// identical VerificationSpec shape generalizes to Plan.Spec/
	// Project.Spec with a Task > Plan > Project precedence in Phase 52.
	// +optional
	Verification VerificationSpec `json:"verification,omitempty"`

	// Dev carries dev/test-only overrides for the stub subagent (Phase 2+).
	// Zero-value embed (not pointer) — field presence is governed by the TestMode
	// enum constraint, mirroring the Gates pattern in shared_types.go.
	// +optional
	Dev TaskDev `json:"dev,omitempty"`

	// SharedContext is the wave-scoped shared context string stamped by the
	// orchestrator at Task creation time (Phase 20 D-05). Populated from the
	// parent planner's EnvelopeOut.SharedContext; byte-identical across all
	// Tasks in the same wave. The dispatcher reads this at Task dispatch time
	// and places it in EnvelopeIn.SharedContext (D-07).
	// Empty for Tasks authored before Phase 20 or where the parent planner
	// emitted no SharedContext; omitempty keeps older CRD objects small.
	// +optional
	SharedContext string `json:"sharedContext,omitempty"`
}

// TaskStatus defines the observed state of Task.
// Stays small per PERSIST-02 / Pitfall 4.
type TaskStatus struct {
	// +optional
	Phase string `json:"phase,omitempty"`

	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// +optional
	Attempt int `json:"attempt,omitempty"`

	// +optional
	ExitCode *int `json:"exitCode,omitempty"`

	// StartedAt is the wall-clock time the reconciler dispatched the Job for this
	// Task. Used by handleJobCompletion to anchor the output-path validation time
	// window (Warning #5 / HARN-05).
	// +optional
	StartedAt *metav1.Time `json:"startedAt,omitempty"`

	// +optional
	CompletedAt *metav1.Time `json:"completedAt,omitempty"`

	// TaskSpanEmittedUID is the UID of the executor Job whose completion has
	// already had its dispatch span synthesized. Mirrors {Level}SpanEmittedUID
	// on the four planner-level Status structs (Phase 43 TRACE-01).
	// +optional
	TaskSpanEmittedUID string `json:"taskSpanEmittedUID,omitempty"`

	// TaskTraceSpanID is the OTel SpanID of this Task's own synthesized span,
	// persisted for Phase 46's dashboard deep-link (OBS-04). Never store the
	// TraceID — always re-derivable from Project.UID via otelai.TraceIDFromUID.
	// +optional
	TaskTraceSpanID string `json:"taskTraceSpanID,omitempty"`

	// TaskTraceReporterSpawnedUID is the UID of the completed executor Job whose
	// trace-only reporter Job has been spawned for this Task attempt — the
	// durable gate closing the CR-01 window where the name-only spawn gate in
	// spawnTaskTraceReporterIfNeeded re-opens after the reporter Job's 300s
	// TTL-GC and a sustained-reconcile parent re-Creates a duplicate reporter
	// with recomputed options (Phase 47 gap-closure; mirrors TaskSpanEmittedUID's
	// per-attempt keying). The value is the completed Job's UID — Task always
	// observes a non-nil completedJob at this call site, so there is no
	// name-fallback branch here (unlike the four planner-level markers).
	// +optional
	TaskTraceReporterSpawnedUID string `json:"taskTraceReporterSpawnedUID,omitempty"`

	// LoopStatus carries the Task loop's current-iteration summary + exit
	// reason only (Phase 51 D-07/LOOP-03 — no accumulating iteration
	// history; see TestLoopStatus_NoForbiddenFields). Re-derivable across a
	// restart from Attempt above + the completed-set, never a cache of
	// iteration history.
	// +optional
	LoopStatus LoopStatus `json:"loopStatus,omitempty"`

	// LockedSHA is the commit SHA of spec.verification at the moment it was
	// last transitioned to Locked — a runtime OBSERVATION recorded at
	// dispatch, not the governing enum (which lives on
	// spec.verification.phase/version per RESEARCH Pitfall 2 / Open
	// Question 1). `git show <lockedSHA>` reproduces exactly the contract a
	// dispatched attempt ran against (TASK-01 repudiation guard).
	// +optional
	LockedSHA string `json:"lockedSHA,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:storageversion
// +kubebuilder:resource:scope=Namespaced
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=".status.phase"
// +kubebuilder:printcolumn:name="Status",type=string,JSONPath=".status.conditions[?(@.type=='Ready')].status"
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=".metadata.creationTimestamp"
// +kubebuilder:validation:XValidation:rule="!has(self.spec.dependsOn) || !(self.metadata.name in self.spec.dependsOn)",message="a task cannot depend on itself"

// Task is the Schema for the tasks API
type Task struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// spec defines the desired state of Task
	// +required
	Spec TaskSpec `json:"spec"`

	// status defines the observed state of Task
	// +optional
	Status TaskStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// TaskList contains a list of Task
type TaskList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []Task `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Task{}, &TaskList{})
}
