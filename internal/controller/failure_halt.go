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

// failure_halt.go — FailureHalt condition helpers for DISP-02 (Phase 25).
//
// D-02b: When TaskReconciler observes a task execution failure under a
// conservative FailureProfile, it calls setFailureHaltIfNeeded to stamp
// FailureHalt=True on the Project. All four EXECUTION dispatch gates call
// checkFailureHalt before dispatching; if halted they park with a 30s requeue.
//
// Conservative halt is cleared by `tide resume --retry-failed` (same verb that
// resets Failed Task phases). CR-02 (Phase 25 review): the clear is NOT
// self-fencing — a Failed Task can reconcile between the clear and its own phase
// reset (or an unrelated straggler reconciles after the clear) and re-stamp the
// halt, re-freezing the project. setFailureHaltIfNeeded therefore mirrors
// setBillingHaltIfNeeded's resume time-fence: `tide resume --retry-failed`
// stamps AnnotationFailureResumedAt when it clears the halt, and this helper
// refuses to re-stamp for a task whose completion timestamp predates that fence.
//
// NOTE: checkFailureHalt is added to the four EXECUTION dispatch sites
// (task/plan/phase/milestone controllers). It is NOT added to the
// project_controller.go planner dispatch site — conservative failure halt is
// execution-only (D-03); gating planning would wrongly freeze authoring of
// already-approved scopes.
package controller

import (
	"context"
	"time"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	tideprojectv1alpha3 "github.com/jsquirrelz/tide/api/v1alpha3"
)

// checkFailureHalt returns true if the Project has a FailureHalt=True condition,
// indicating that a task failed under conservative FailureProfile and all new
// dispatch should be parked until the operator runs `tide resume --retry-failed`.
//
// Nil-safe: a nil project returns false.
func checkFailureHalt(project *tideprojectv1alpha3.Project) bool {
	if project == nil {
		return false
	}
	return meta.IsStatusConditionTrue(project.Status.Conditions, tideprojectv1alpha3.ConditionFailureHalt)
}

// setFailureHaltIfNeeded stamps FailureHalt=True on project when the Project's
// FailureProfile is conservative and FailureHalt is not already set. Idempotent:
// a second call when halt is already True is a no-op (avoids patch churn on
// concurrent wave failures).
//
// taskCompletedAt is the failing Task's Status.CompletedAt (passed by each call
// site; zero when not yet set). CR-02 resume time-fence: when the project carries
// AnnotationFailureResumedAt and taskCompletedAt is non-zero and predates that
// resume timestamp, the failure is a pre-resume straggler and the halt is NOT
// re-stamped — mirroring setBillingHaltIfNeeded's jobStart<resumedAt guard.
// A zero taskCompletedAt or unparseable annotation falls through to stamping
// (fail-closed toward halting, matching the billing path's conservatism).
//
// Called from TaskReconciler.handleJobCompletion on task execution failure only
// (not on planning Job failures — FailureHalt is an execution-layer signal).
// Nil project is a safe no-op.
func setFailureHaltIfNeeded(ctx context.Context, c client.Client, project *tideprojectv1alpha3.Project, taskCompletedAt time.Time) error {
	if project == nil {
		return nil
	}
	if project.Spec.FailureProfile != tideprojectv1alpha3.FailureProfileConservative {
		return nil // strict profile (or unset default): no-op
	}
	// Already halted: idempotent no-op.
	if meta.IsStatusConditionTrue(project.Status.Conditions, tideprojectv1alpha3.ConditionFailureHalt) {
		return nil
	}
	// CR-02 resume time-fence: refuse to re-stamp a halt for a failure that
	// predates the operator's `tide resume --retry-failed`. Mirrors
	// setBillingHaltIfNeeded (billing_halt.go). Fail-closed on zero timestamp or
	// unparseable annotation → fall through and stamp.
	if !taskCompletedAt.IsZero() {
		if resumeVal, ok := project.Annotations[tideprojectv1alpha3.AnnotationFailureResumedAt]; ok {
			if resumedAt, err := time.Parse(time.RFC3339, resumeVal); err == nil {
				if taskCompletedAt.Before(resumedAt) {
					return nil // stale pre-resume straggler; no-op
				}
			}
			// unparseable annotation → fall through (fail-closed: stamp halt)
		}
	}
	patch := client.MergeFrom(project.DeepCopy())
	meta.SetStatusCondition(&project.Status.Conditions, metav1.Condition{
		Type:   tideprojectv1alpha3.ConditionFailureHalt,
		Status: metav1.ConditionTrue,
		Reason: tideprojectv1alpha3.ReasonTaskFailedHalt,
		Message: "A task failed under conservative FailureProfile. New dispatch halted project-wide. " +
			"Run `tide resume --retry-failed` after addressing the failure.",
		LastTransitionTime: metav1.Now(),
	})
	return c.Status().Patch(ctx, project, patch)
}
