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
