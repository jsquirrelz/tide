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

// Package v1alpha2 contains TIDE's v1alpha2 CRD types.
//
// shared_types.go declares the status-condition vocabulary used uniformly
// across all six Kinds (Project, Milestone, Phase, Plan, Task, Wave) per
// CONTEXT.md "Claude's Discretion". Reconcilers call meta.SetStatusCondition
// with these constants.
package v1alpha2

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
	// does not exist in the namespace. Surfaced (condition False + Warning Event)
	// before requeuing so the stall is observable rather than silent.
	ReasonParentRefNotFound = "ParentRefNotFound"
)

// Phase 11 condition + reason vocabulary — per-wave integration failure.
const (
	// ReasonWaveIntegrationFailed — a per-wave integration Job (BackoffLimit
	// exhausted) failed before wave k+1 could be dispatched. Plan is marked
	// terminal Failed; subsequent reconcile cycles see Phase=="Failed" and
	// exit early without requeueing.
	ReasonWaveIntegrationFailed = "WaveIntegrationFailed"
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

// Phase 23 condition + reason vocabulary — v1alpha2 schema migration (SCHEMA-03).
// ReasonRequiresReinstall is consumed by the Plan-03 project reconciler old-object
// guard: any Project whose Spec.SchemaRevision != "v1alpha2" (i.e. it was authored
// under v1alpha1 and slipped into etcd before the CRD upgrade) is rejected with
// this reason and reconcile.TerminalError (no requeue). The operator must delete
// the old Project CR and re-apply it under the v1alpha2 shape.
//
// ReasonGlobalCycleDetected is consumed by the Phase-23 global cycle gate in the
// Project reconciler: when pkg/dag.ComputeWaves returns a CycleError over the
// assembled task-level dep graph, the Project's CycleDetected condition is set with
// this reason and the involved node names in the Message. Unlike RequiresReinstall,
// this is NOT a TerminalError — a subsequent plan edit may remove the cycle, so the
// reconciler requeues on Project changes.
const (
	// ReasonRequiresReinstall — Project was created under v1alpha1 schema;
	// the controller fail-closes with this reason and reconcile.TerminalError.
	// Reinstall: kubectl delete project <name> && kubectl apply -f <project.yaml>
	// (with a v1alpha2-compliant manifest including SchemaRevision: v1alpha2).
	ReasonRequiresReinstall = "RequiresReinstall"

	// ReasonGlobalCycleDetected — the assembled task-level dep graph for this
	// Project contains a cycle across plan/phase/milestone boundaries. Surfaced
	// as a CycleDetected status condition naming the involved nodes. Phase-24
	// fan-out of coarse scope deps may expose additional cycles; the Phase-23
	// gate catches task-level cycles only (conservative by construction).
	ReasonGlobalCycleDetected = "GlobalCycleDetected"
)
