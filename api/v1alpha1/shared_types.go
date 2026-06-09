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

// Package v1alpha1 contains TIDE's v1alpha1 CRD types.
//
// shared_types.go declares the status-condition vocabulary used uniformly
// across all six Kinds (Project, Milestone, Phase, Plan, Task, Wave) per
// CONTEXT.md "Claude's Discretion". Reconcilers call meta.SetStatusCondition
// with these constants.
package v1alpha1

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
)

// Phase 04.1 P1.4 condition + reason vocabulary — parent-resolution failure
// surfaces when a Task or Plan cannot find its owning Project via the
// `tideproject.k8s/project` label fast-path OR via an owner-ref chain walk
// (Task→Plan→Phase→Milestone→Project, bounded depth 5). Distinct from
// ConditionFailed because the resource is not terminally failed — it's
// awaiting either a label stamp from PlanReconciler (Task case) or an
// owner-ref addition (Plan case). Caller requeues after 30s and tries again.
//
// Closes the silent mis-routing bug class in multi-Project namespaces where
// the prior fallback `projectList.Items[0]` would adopt whichever Project
// sorted first.
const (
	// ConditionParentUnresolved is set on a Task or Plan when its parent
	// Project cannot be resolved by label or owner-chain walk.
	ConditionParentUnresolved = "ParentUnresolved"

	// ReasonNoProjectLabel — no tideproject.k8s/project label on the resource.
	ReasonNoProjectLabel = "NoProjectLabel"

	// ReasonNoOwnerRef — no Project owner ref in the (bounded) owner-ref chain.
	ReasonNoOwnerRef = "NoOwnerRef"
)

// Phase 11 condition + reason vocabulary — per-wave integration failure.
// A wave integration Job (BackoffLimit exhausted) failed before wave k+1 could
// be dispatched; the Plan is marked terminal Failed so dependents never dispatch
// and the reconciler does not livelock.
const (
	// ReasonWaveIntegrationFailed — a per-wave integration Job (BackoffLimit
	// exhausted) failed before wave k+1 could be dispatched. Plan is marked
	// terminal Failed; subsequent reconcile cycles see Phase=="Failed" and
	// exit early without requeueing.
	ReasonWaveIntegrationFailed = "WaveIntegrationFailed"
)
