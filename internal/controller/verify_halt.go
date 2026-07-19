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

// verify_halt.go — VerifyHalt condition helpers for ESC-02/ESC-03 (Phase 51).
//
// D-09: when TaskReconciler's verification loop exhausts LoopPolicy.MaxIterations
// without an APPROVED evaluator verdict, it calls setVerifyHaltIfNeeded to stamp
// VerifyHalt=True on the Project. checkVerifyHalt gates BOTH the planner tier
// (checkDispatchHolds) and the task tier (gateChecks) — a BLOCKED verify means
// the artifact tree the next dispatch would build on is suspect, at every level.
//
// VerifyHalt is cleared by `tide resume`, mirroring FailureHalt's `tide resume
// --retry-failed` clear. CR-02 (Phase 25 review, carried forward): the clear is
// NOT self-fencing — a Task can reconcile between the clear and its own reset
// (or an unrelated straggler reconciles after the clear) and re-stamp the halt,
// re-freezing the project. setVerifyHaltIfNeeded therefore mirrors
// setFailureHaltIfNeeded's resume time-fence: `tide resume` stamps
// AnnotationVerifyResumedAt when it clears the halt, and this helper refuses to
// re-stamp for a task whose completion timestamp predates that fence.
//
// NOTE (reverses failure_halt.go's D-03 note): checkVerifyHalt IS added to the
// project_controller.go planner dispatch site, in addition to the four
// EXECUTION dispatch sites — a BLOCKED verify means the artifact tree is
// suspect at every level, so gating planning too is correct here (unlike
// conservative FailureHalt, which is execution-only).
package controller

import (
	"context"
	"time"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	tideprojectv1alpha3 "github.com/jsquirrelz/tide/api/v1alpha3"
)

// checkVerifyHalt returns true if the Project has a VerifyHalt=True condition,
// indicating a Task's verification loop exhausted LoopPolicy.MaxIterations
// without an APPROVED evaluator verdict and all new dispatch should be parked
// until the operator runs `tide resume`.
//
// Nil-safe: a nil project returns false.
func checkVerifyHalt(project *tideprojectv1alpha3.Project) bool {
	if project == nil {
		return false
	}
	return meta.IsStatusConditionTrue(project.Status.Conditions, tideprojectv1alpha3.ConditionVerifyHalt)
}

// setVerifyHaltIfNeeded stamps VerifyHalt=True on project when VerifyHalt is
// not already set. Idempotent: a second call when halt is already True is a
// no-op (avoids patch churn on concurrent wave exhaustions).
//
// Unlike setFailureHaltIfNeeded, there is no FailureProfile gate here — the
// caller (TaskReconciler's verify-exhaustion branch, Plan 07) only invokes
// this helper once the Task loop has genuinely exhausted MaxIterations
// without an APPROVED verdict; the exhaustion trigger lives at the call site,
// not inside this helper.
//
// taskCompletedAt is the exhausted Task's Status.CompletedAt (passed by the
// call site; zero when not yet set). CR-02 resume time-fence: when the
// project carries AnnotationVerifyResumedAt and taskCompletedAt is non-zero
// and predates that resume timestamp, the exhaustion is a pre-resume
// straggler and the halt is NOT re-stamped — mirroring
// setFailureHaltIfNeeded's taskCompletedAt<resumedAt guard. A zero
// taskCompletedAt or unparseable annotation falls through to stamping
// (fail-closed toward halting, matching the failure/billing paths' conservatism).
//
// Nil project is a safe no-op.
func setVerifyHaltIfNeeded(ctx context.Context, c client.Client, project *tideprojectv1alpha3.Project, taskCompletedAt time.Time) error {
	if project == nil {
		return nil
	}
	// Already halted: idempotent no-op.
	if meta.IsStatusConditionTrue(project.Status.Conditions, tideprojectv1alpha3.ConditionVerifyHalt) {
		return nil
	}
	// CR-02 resume time-fence: refuse to re-stamp a halt for an exhaustion that
	// predates the operator's `tide resume`. Mirrors setFailureHaltIfNeeded
	// (failure_halt.go). Fail-closed on zero timestamp or unparseable
	// annotation → fall through and stamp.
	if !taskCompletedAt.IsZero() {
		if resumeVal, ok := project.Annotations[tideprojectv1alpha3.AnnotationVerifyResumedAt]; ok {
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
		Type:   tideprojectv1alpha3.ConditionVerifyHalt,
		Status: metav1.ConditionTrue,
		Reason: tideprojectv1alpha3.ReasonVerifyExhausted,
		Message: "A task's verification loop exhausted MaxIterations without an APPROVED " +
			"evaluator verdict. New dispatch halted project-wide. Run `tide resume` after " +
			"reviewing the verification findings.",
		LastTransitionTime: metav1.Now(),
	})
	return c.Status().Patch(ctx, project, patch)
}
