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
