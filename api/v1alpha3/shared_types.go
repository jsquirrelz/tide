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

// Package v1alpha3 contains TIDE's v1alpha3 CRD types.
//
// shared_types.go declares the status-condition vocabulary used uniformly
// across all six Kinds (Project, Milestone, Phase, Plan, Task, Wave) per
// CONTEXT.md "Claude's Discretion". Reconcilers call meta.SetStatusCondition
// with these constants.
package v1alpha3

const (
	// ConditionPending — initial state, awaiting first reconcile.
	ConditionPending = "Pending"
	// ConditionReady — object reached its terminal "OK" state.
	ConditionReady = "Ready"
	// ConditionReconciling — actively being processed by a reconciler.
	ConditionReconciling = "Reconciling"
	// ConditionFailed — terminal failure; surface via Status.Conditions message.
	ConditionFailed = "Failed"

	// Common Reasons used with meta.SetStatusCondition.
	ReasonInitialized            = "Initialized"
	ReasonAwaitingDispatch       = "AwaitingDispatch"
	ReasonFinalizerTimedOut      = "FinalizerTimedOut"
	ReasonSubagentDispatchFailed = "SubagentDispatchFailed"
)

// Phase 2 condition and reason constants — dispatch, validation, budget, and
// rate-limiting vocabulary used by the innermost reconcilers and the Plan
// admission webhook (Plans 07, 09, 10, 11).
const (
	// ConditionValidated — Plan admission webhook has accepted the Plan's DAG
	// and file-touch declarations (Plan 11 sets; Plan 09 reads).
	ConditionValidated = "Validated"
	// ConditionBudgetExceeded — Project absolute cost cap has been hit
	// (Plan 10 sets; TaskReconciler halts on this condition).
	ConditionBudgetExceeded = "BudgetExceeded"
	// ConditionRunning — task/wave/plan is actively executing.
	ConditionRunning = "Running"
	// ConditionSucceeded — task/wave/plan reached terminal success.
	ConditionSucceeded = "Succeeded"

	// ReasonCycleDetected — Plan DAG contains a dependency cycle; set on
	// Plan.Status.ValidationState=CycleDetected by the admission webhook.
	ReasonCycleDetected = "CycleDetected"
	// ReasonFileTouchMismatch — Task declares file touches not matching
	// PlanAdmission.FileTouchMode expectations.
	ReasonFileTouchMismatch = "FileTouchMismatch"
	// ReasonCapHit — a Task or Project cap (tokens, iterations, wall-clock) was
	// reached; TaskReconciler marks the Task failed with this reason.
	ReasonCapHit = "CapHit"
	// ReasonRateLimitHit — the provider rate-limiter was saturated; dispatcher
	// retries with backoff, eventually failing with this reason.
	ReasonRateLimitHit = "RateLimitHit"
	// ReasonBypassApplied — a gate bypass was explicitly applied by an operator;
	// used in Conditions.Message to surface the override.
	ReasonBypassApplied = "BypassApplied"
)

// Phase 3 condition vocabulary additions — up-stack reconcilers + push Job
// signaling (Plans 03-04, 03-05, 03-06, 03-07, 03-08). All three are set/
// cleared by Phase 3 reconcilers; the existing Pending/Ready/Reconciling/
// Failed/Validated/Running/Succeeded vocabulary remains the cross-Kind base.
const (
	// ConditionCloned — Project's per-run branch has been cloned + worktrees
	// initialized by the clone Job (Phase 3 D-B6 / ProjectReconciler extension).
	ConditionCloned = "Cloned"

	// ConditionAuthoringPlanner — a planner Job (Milestone/Phase/Plan) has been
	// dispatched and is still running. Cleared when the Job reaches a terminal
	// state and the child CRDs (Phase 3 D-A1 envelope childCRDs) have been
	// materialized. Reflects the "planning fans out wide" phase of dispatch.
	ConditionAuthoringPlanner = "AuthoringPlanner"

	// ConditionPushLeaseFailed — push Job rejected by --force-with-lease
	// (Phase 3 D-B6). Cleared by either a successful subsequent push or the
	// tideproject.k8s/bypass-push-lease=true annotation.
	ConditionPushLeaseFailed = "PushLeaseFailed"

	// ConditionPushLeakBlocked — push Job blocked by gitleaks (exit code 10,
	// envelope.reason=leak-detected). Phase 4 W-1 follow-up — distinct from
	// ConditionPushLeaseFailed so the operator-visible reason is unambiguous
	// (a secret was detected; no bypass annotation exists for this — the
	// only path forward is removing the secret from the staged artifacts and
	// re-running the boundary). Set by the ProjectReconciler push-result
	// envelope handler (plan 04-06 task 1).
	ConditionPushLeakBlocked = "PushLeakBlocked"

	// ConditionCloneFailed — the clone Job reached terminal-Failed (Failed>0,
	// Succeeded==0) after exhausting its BackoffLimit (Phase 27 WR-03). The
	// ProjectReconciler deletes the failed Job to fast-path a re-dispatch
	// instead of stalling on the clone Job's 300s TTL; this condition surfaces
	// the failure+recovery so an operator can see the clone stall window.
	ConditionCloneFailed = "CloneFailed"

	// ReasonBaseRefUnresolvable — the ConditionCloneFailed Reason for a clone
	// Job that terminal-failed with envelope reason "baseref-unresolvable"
	// (Phase 35 BASE-02, D-06/D-07). Unlike the existing "CloneJobFailed" reason
	// — which keeps its delete-and-re-dispatch meaning for transient/other clone
	// failures — this reason HALTS re-dispatch for the current generation
	// (classify-don't-retry): the condition is stamped with ObservedGeneration =
	// project.Generation and the dispatch guard skips clone dispatch while the
	// condition is True for that generation. An operator edit to spec.git.baseRef
	// bumps metadata.generation and releases the halt (recovery = one kubectl edit).
	ReasonBaseRefUnresolvable = "BaseRefUnresolvable"
)

// Debug defect #13b — boundary-push observability + bounded auto-retry.
//
// ConditionBoundaryPushed is a NON-TERMINAL condition on the Project that
// surfaces whether the already-integrated run branch has landed on the remote.
// Complete is NEVER gated on this condition: the Project reaches Complete on the
// control-plane succession roll-up (all Milestones Succeeded) independent of the
// boundary push, and this condition tracks the push outcome separately so a
// failed/never-landed push is observable without blocking succession.
const (
	// ConditionBoundaryPushed — True when the run branch is confirmed pushed
	// (boundary push Job Complete); False while a push attempt is in flight /
	// pending retry (Reason=Pushing) or once the bounded retry budget is
	// exhausted (Reason=PushFailed).
	ConditionBoundaryPushed = "BoundaryPushed"

	// ReasonPushed — the run branch is confirmed on the remote.
	ReasonPushed = "Pushed"
	// ReasonPushing — a boundary push attempt is in flight or pending retry.
	ReasonPushing = "Pushing"
	// ReasonPushFailed — the bounded boundary-push retry budget is exhausted;
	// the controller stops dispatching push Jobs and emits a Warning Event.
	ReasonPushFailed = "PushFailed"
)

// Phase 4 condition + reason vocabulary additions — gate-policy seam at every
// level boundary (plans 04-04, 04-05, 04-06). All four up-stack reconcilers
// (Milestone/Phase/Plan/Task) set ConditionWaveOrLevelPaused with one of the
// four Reasons below when the gate-policy hook trips; the annotation-driven
// approve/reject path (D-G3 / D-G4) flips the Reason in place rather than
// adding new condition types.
const (
	// ConditionWaveOrLevelPaused — set when a reconciler observes a gate-policy
	// value of "approve" or "pause" at a level boundary, OR when the Plan-level
	// Spec.Gates.PauseBetweenWaves check trips between consecutive waves
	// (D-G2). Cleared by the matching approve / resume annotation (D-G3 / D-G4).
	ConditionWaveOrLevelPaused = "WaveOrLevelPaused"

	// ReasonAwaitingApproval — gate=approve at this boundary and no
	// tideproject.k8s/approve-* annotation has been observed yet (D-G2).
	ReasonAwaitingApproval = "AwaitingApproval"
	// ReasonPausedAtBoundary — gate=pause OR PauseBetweenWaves=true halts the
	// dispatch without polling for an approval annotation; requires explicit
	// `tide resume` (D-G2).
	ReasonPausedAtBoundary = "PausedAtBoundary"
	// ReasonRejectedByUser — `tide reject` set the tideproject.k8s/reject
	// annotation; reconciler halts dispatch and leaves resources in place
	// for human inspection (D-G4).
	ReasonRejectedByUser = "RejectedByUser"
	// ReasonResumedByUser — `tide resume` cleared the reject annotation and
	// reconciliation has re-entered the normal advance path (D-G4).
	ReasonResumedByUser = "ResumedByUser"
	// ReasonApprovedByUser — operator ran `tide approve`; the level's
	// AwaitingApproval park is lifted (Status=ConditionFalse indicates pause
	// cleared). Mirrors ReasonResumedByUser; no new Status.Phase enum — the
	// level returns to Running and Succeeded fires only via ChildCount-gated
	// succession after all children complete. Phase 12 D-04.
	ReasonApprovedByUser = "ApprovedByUser"
)

// Phase 04.1 P1.4 condition + reason vocabulary — parent-resolution failure
// surfaces when a Task or Plan cannot find its owning Project via the
// `tideproject.k8s/project` label fast-path OR via an owner-ref chain walk
// (Task→Plan→Phase→Milestone→Project, bounded depth 5). Distinct from
// ConditionFailed because the resource is not terminally failed — it's
// awaiting either a label stamp from PlanReconciler (Task case) or an
// owner-ref addition (Plan case). Caller requeues after 30s and tries again.
const (
	// ConditionParentUnresolved is set on a Task or Plan when its parent
	// Project cannot be resolved by label or owner-chain walk; also set on a
	// Phase or Milestone (defect #17) when its direct parent-ref resolves to
	// NotFound.
	ConditionParentUnresolved = "ParentUnresolved"

	// ReasonNoProjectLabel — no tideproject.k8s/project label on the resource.
	ReasonNoProjectLabel = "NoProjectLabel"

	// ReasonNoOwnerRef — no Project owner ref in the (bounded) owner-ref chain.
	ReasonNoOwnerRef = "NoOwnerRef"

	// ReasonParentRefNotFound — the resource's direct parent-ref (defect #17:
	// Phase.spec.milestoneRef / Milestone.spec.projectRef) names a parent that
	// does not exist in the namespace. Surfaced (condition True — D-04, Phase
	// 41: True == parent unresolved — + Warning Event) before requeuing so the
	// stall is observable rather than silent.
	ReasonParentRefNotFound = "ParentRefNotFound"

	// ReasonParentResolved — set with Status=False once a previously-missing
	// parent-ref resolves successfully (D-04, Phase 41: the clear-on-resolve
	// counterpart to ReasonParentRefNotFound).
	ReasonParentResolved = "ParentResolved"
)

// Phase 11 condition + reason vocabulary — per-wave integration failure.
const (
	// ReasonWaveIntegrationFailed — a per-wave integration Job (BackoffLimit
	// exhausted) failed before wave k+1 could be dispatched. Plan is marked
	// terminal Failed; subsequent reconcile cycles see Phase=="Failed" and
	// exit early without requeueing.
	ReasonWaveIntegrationFailed = "WaveIntegrationFailed"
)

// Phase 34 condition + reason vocabulary — run-integrity integration-miss
// gate (INTEG-01..05). ConditionIntegrationIncomplete lives on the Project
// (D-11 — beside ConditionBoundaryPushed, since it's a push-gate outcome and
// the Project is what operators watch) and is set sticky (True) once the
// bounded boundary-push retry budget is exhausted on a completeness miss
// (D-08) OR immediately on a same-wave merge conflict surfacing at the
// project-boundary push (D-09). ReasonMergeConflict is also used on the
// PLAN's own Failed condition when a wave-integration merge conflict fails
// the Plan (D-10 — conflicting parallel tasks were not actually independent).
const (
	// ConditionIntegrationIncomplete — sticky push-gate-outcome condition on
	// the Project. True means the run branch is missing at least one
	// Succeeded task's worktree branch (a completeness miss, ReasonIntegrationIncomplete)
	// or hit a genuine merge conflict integrating a task branch
	// (ReasonMergeConflict). Cleared automatically the next time a verify+push
	// succeeds, or explicitly via `tide resume` (D-13).
	ConditionIntegrationIncomplete = "IntegrationIncomplete"

	// ReasonIntegrationIncomplete — the in-Job verify gate (`git merge-base
	// --is-ancestor` per expected branch, D-06) found at least one Succeeded
	// task's branch missing from the run branch after the bounded retry
	// budget (maxBoundaryPushAttempts) was exhausted (D-08). The condition
	// message names each missing task + branch (truncated, D-12).
	ReasonIntegrationIncomplete = "IntegrationIncomplete"

	// ReasonMergeConflict — a genuine git merge conflict (not a transient
	// infra failure) was hit integrating a task branch into the run branch.
	// Distinguished from ReasonIntegrationIncomplete because it is a content
	// problem requiring a human (D-09) — retries are NOT burned on a
	// conflict; the push/wave-integration Job parks/fails immediately.
	ReasonMergeConflict = "MergeConflict"
)

// Phase 33 condition + reason vocabulary — planner failure semantics (D4).
const (
	// ReasonPlannerFailed — phase or milestone planner exited nonzero with zero
	// children, preventing false-leaf succession. Level is marked terminal Failed;
	// run `tide resume --retry-failed` to reset and re-dispatch.
	ReasonPlannerFailed = "PlannerFailed"
)

// Phase 13 condition + reason vocabulary — provider billing halt (HALT-01).
const (
	// ConditionBillingHalt — provider returned a credit-exhaustion 400;
	// new dispatch is halted project-wide until the operator refills credits
	// and runs `tide resume`. Set by reconciler billing classifier on Project;
	// read by all five dispatch gates; cleared by tide resume. Phase 13 HALT-01.
	ConditionBillingHalt = "BillingHalt"

	// ReasonCreditBalanceTooLow — Anthropic API returned HTTP 400 with
	// "credit balance" in the error body. Set on Project by the reconciler
	// billing classifier.
	ReasonCreditBalanceTooLow = "CreditBalanceTooLow"

	// AnnotationBillingResumedAt — RFC3339 timestamp stamped by `tide resume`
	// when clearing the BillingHalt condition. Consumed by the reconciler
	// backstop (setBillingHaltIfNeeded) to ignore billing evidence from Jobs
	// created before the resume timestamp.
	AnnotationBillingResumedAt = "tideproject.k8s/billing-resumed-at"
)

// Phase 38 condition + reason vocabulary — unknown-model pricing fallback (COST-02).
const (
	// ConditionPricingFallbackActive — informational condition: a dispatch was
	// priced at the conservative (most-expensive) fallback tier because its
	// model was absent from the pricing table. Unlike BillingHalt it gates
	// NOTHING — dispatch continues; the condition exists so the fallback
	// survives pod GC and shows on Prometheus-less installs (Phase 38 COST-02
	// / D-02). Sticky for the run's lifetime (no clearer in v1.0.7).
	ConditionPricingFallbackActive = "PricingFallbackActive"

	// ReasonUnknownModelPriced — the dispatch's model missed the effective
	// price table even after date-suffix normalization; tokens were billed at
	// the conservative tier. Set on Project by setPricingFallbackIfNeeded.
	ReasonUnknownModelPriced = "UnknownModelPriced"
)

// Phase 14 condition + reason vocabulary — operator budget cap blocked (BUDGET-02).
const (
	// ConditionBudgetBlocked — operator's budget cap has been reached; new dispatch
	// is halted project-wide until the cap is raised (Spec.Budget.AbsoluteCapCents)
	// or the bypass annotation is applied. Set and cleared by the TaskReconciler
	// dispatch gate via setBudgetBlockedIfNeeded. Phase 14 BUDGET-02.
	ConditionBudgetBlocked = "BudgetBlocked"

	// ReasonBudgetCapReached — the project's absolute cost cap (or rolling cap) was
	// exceeded; set by the TaskReconciler dispatch gate via setBudgetBlockedIfNeeded.
	ReasonBudgetCapReached = "BudgetCapReached"

	// ReasonBudgetCapCleared — the project's cost cap is no longer exceeded (e.g.
	// the operator raised Spec.Budget.AbsoluteCapCents); the BudgetBlocked condition
	// is flipped to False so dispatch can resume. Phase 14 BUDGET-02.
	ReasonBudgetCapCleared = "BudgetCapCleared"
)

// Phase 25 condition + reason vocabulary — task failure halt (DISP-02 conservative).
const (
	// ConditionFailureHalt — a task failed under conservative FailureProfile;
	// new dispatch is halted project-wide until the operator runs
	// `tide resume --retry-failed`. Set by TaskReconciler.handleJobCompletion;
	// read by all four execution dispatch gates; cleared by tide resume. Phase 25 DISP-02.
	ConditionFailureHalt = "FailureHalt"

	// ReasonTaskFailedHalt — a member task failed and the Project's
	// FailureProfile is conservative; halt is set project-wide.
	ReasonTaskFailedHalt = "TaskFailedHalt"

	// AnnotationFailureResumedAt — RFC3339 timestamp stamped by
	// `tide resume --retry-failed` when clearing the FailureHalt condition.
	// Mirrors AnnotationBillingResumedAt. Optional: only needed if the
	// reconciler gates re-stamping FailureHalt against this timestamp.
	AnnotationFailureResumedAt = "tideproject.k8s/failure-resumed-at"
)

// Phase 51 condition + reason vocabulary — Task loop verification halt
// (ESC-02/ESC-03). Third generation of the BillingHalt → FailureHalt →
// VerifyHalt halt-condition mirror.
//
// What SETS it — setVerifyHaltIfNeeded (verify_halt.go) stamps
// ConditionVerifyHalt=True when a Task's verification loop exhausts
// LoopPolicy.MaxIterations without an APPROVED evaluator verdict.
//
// What READS it — checkVerifyHalt is wired into BOTH dispatch chains: the
// planner-tier checkDispatchHolds AND the Task-tier gateChecks (D-09) —
// because a BLOCKED verify means the artifact tree the next dispatch would
// build on is suspect, at every level.
//
// What CLEARS it — `tide resume` stamps AnnotationVerifyResumedAt when
// clearing the condition; mirrors AnnotationFailureResumedAt's CR-02
// resume time-fence so a straggler reconcile predating the resume cannot
// re-freeze the project.
//
// VerifyHalt is a DISTINCT halt class — never a reinterpretation of Failed
// wave semantics. A VerifyHalt leaves phase/wave-siblings/conservative-
// profile propagation untouched (ESC-03).
const (
	// ConditionVerifyHalt — the Task loop's verification exhausted
	// LoopPolicy.MaxIterations without an APPROVED evaluator verdict; new
	// dispatch is halted project-wide until the operator runs `tide resume`.
	ConditionVerifyHalt = "VerifyHalt"

	// ReasonVerifyExhausted — the Task loop reached MaxIterations without
	// an APPROVED evaluation; set on Project by setVerifyHaltIfNeeded.
	ReasonVerifyExhausted = "VerifyExhausted"

	// AnnotationVerifyResumedAt — RFC3339 timestamp stamped by `tide resume`
	// when clearing the VerifyHalt condition. Mirrors
	// AnnotationFailureResumedAt; consumed by setVerifyHaltIfNeeded's
	// resume time-fence to ignore verify-exhaustion evidence predating the
	// resume timestamp.
	AnnotationVerifyResumedAt = "tideproject.k8s/verify-resumed-at"
)

// Phase 28 condition + reason vocabulary — envelope import (IMPORT-01..05).
const (
	// ConditionImportComplete — the one-shot UID-rewrite import Job has
	// completed successfully; planner-dispatch holds clear at all 5 sites.
	// Set by ImportController on Project.Status.Conditions.
	ConditionImportComplete = "ImportComplete"

	// ReasonImportSucceeded — tide-import Job exited 0; all envelopes copied
	// and schema-converted to new-UID paths.
	ReasonImportSucceeded = "ImportSucceeded"

	// ReasonImportFailed — tide-import Job exited non-zero, or envelope
	// validation failed (ChildCount mismatch, Kind not allowlisted).
	// Operator must investigate and optionally apply AnnotationRetryImport.
	ReasonImportFailed = "ImportFailed"

	// ReasonCyclicPlanDetected — dag.ComputeWaves found a cycle in the
	// imported seed's dependency graph (Plan/Phase/Milestone dependsOn, run
	// BEFORE any client.Create). Distinct reason so the operator can tell a
	// cyclic plan apart from a generic import failure. Set by ImportController.
	ReasonCyclicPlanDetected = "CyclicPlanDetected"

	// AnnotationRetryImport — applied by operator to trigger an import retry
	// after ImportFailed; consumed by ImportController to reset import state.
	// Mirrors AnnotationBillingResumedAt pattern.
	AnnotationRetryImport = "tideproject.k8s/retry-import"
)

// Phase 31 condition + reason vocabulary — project-planner adoption suppression (ADOPT-01 / D-01).
const (
	// ConditionProjectPlannerSuppressed — durable .status condition that permanently
	// suppresses project-planner re-dispatch on an adopted/imported Project once the
	// import tree is confirmed present (ConditionImportComplete=True and all child
	// Milestones exist). Survives manager restart because it lives in .status and is
	// self-documenting in `kubectl describe`. Read by reconcileProjectPlannerDispatch
	// as a short-circuit before the live List of owned Milestones, preventing a
	// cache-miss re-dispatch on cold restart (P-D2a pitfall). Phase 31 ADOPT-01 / D-01.
	ConditionProjectPlannerSuppressed = "ProjectPlannerSuppressed"

	// ReasonAdoptionComplete — the project-planner suppression was stamped because
	// the Project was imported (ConditionImportComplete=True) and its adoption tree
	// was confirmed present; the project-planner will not be re-dispatched.
	ReasonAdoptionComplete = "AdoptionComplete"
)

// FailureProfileType is the failure-propagation policy for this Project.
// +kubebuilder:validation:Enum=strict;conservative
type FailureProfileType string

const (
	// FailureProfileStrict (default): non-dependent tasks in later waves
	// continue dispatching when an earlier task fails. The indegree model
	// enforces this automatically — only dependents are blocked.
	FailureProfileStrict FailureProfileType = "strict"

	// FailureProfileConservative: first task execution failure halts all
	// new dispatch project-wide (ConditionFailureHalt) until the operator
	// runs `tide resume --retry-failed`. In-flight Jobs complete naturally.
	FailureProfileConservative FailureProfileType = "conservative"
)

// Phase 23 condition + reason vocabulary — schema migration guard (SCHEMA-03),
// generalized in Phase 40 (D-04) to the current expectedSchemaRevision constant.
// ReasonRequiresReinstall is consumed by the Project reconciler old-object
// guard: any Project whose Spec.SchemaRevision != "v1alpha3" (i.e. it was
// authored under a prior schema revision and slipped into etcd before the CRD
// upgrade) is rejected with this reason and reconcile.TerminalError (no
// requeue). The operator must delete the old Project CR and re-apply it under
// the v1alpha3 shape.
//
// ReasonGlobalCycleDetected is consumed by the Phase-23 global cycle gate in the
// Project reconciler: when pkg/dag.ComputeWaves returns a CycleError over the
// assembled task-level dep graph, the Project's CycleDetected condition is set with
// this reason and the involved node names in the Message. Unlike RequiresReinstall,
// this is NOT a TerminalError — a subsequent plan edit may remove the cycle, so the
// reconciler requeues on Project changes.
const (
	// ReasonRequiresReinstall — Project was created under a prior schema
	// revision; the controller fail-closes with this reason and
	// reconcile.TerminalError. Reinstall: kubectl delete project <name> &&
	// kubectl apply -f <project.yaml> (with a v1alpha3-compliant manifest
	// including SchemaRevision: v1alpha3).
	ReasonRequiresReinstall = "RequiresReinstall"

	// ReasonGlobalCycleDetected — the assembled task-level dep graph for this
	// Project contains a cycle across plan/phase/milestone boundaries. Surfaced
	// as a CycleDetected status condition naming the involved nodes. Phase-24
	// fan-out of coarse scope deps may expose additional cycles; the Phase-23
	// gate catches task-level cycles only (conservative by construction).
	ReasonGlobalCycleDetected = "GlobalCycleDetected"
)

// LevelPhase constants for Status.Phase on the five level kinds (Milestone,
// Phase, Plan, Task, Wave). Distinct from Project's own Phase* vocabulary
// (project_types.go), which has a different value set entirely — the two
// namespaces are kept separate in this package rather than collapsed, so a
// per-kind duplicate block is not used. Field type stays string (no
// +kubebuilder:validation:Enum) — this is a non-breaking cleanup (Phase 41
// D-03): it closes the "typo compiles silently" failure class without
// changing the CRD schema.
const (
	// LevelPhasePending is the initial phase before any reconcile has run.
	LevelPhasePending = "Pending"
	// LevelPhaseRunning is set when dispatch is actively proceeding.
	LevelPhaseRunning = "Running"
	// LevelPhaseSucceeded is the terminal success phase.
	LevelPhaseSucceeded = "Succeeded"
	// LevelPhaseFailed is the terminal failure phase.
	LevelPhaseFailed = "Failed"
	// LevelPhaseAwaitingApproval is set when a gate-policy hook has parked
	// dispatch at this level pending an operator `tide approve`.
	LevelPhaseAwaitingApproval = "AwaitingApproval"
	// LevelPhaseZeroMembers is Wave-only: set by the wave aggregator when a
	// Wave's TaskRefs is empty (no member Tasks were ever assigned to it).
	LevelPhaseZeroMembers = "ZeroMembers"
)
