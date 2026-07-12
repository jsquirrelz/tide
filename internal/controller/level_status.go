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

// Package controller — level_status.go holds the leaf status-mutation
// primitives shared by the Milestone/Phase/Plan/Task reconcilers (Phase 41
// item 10 / REFAC-10): the MergeFrom → set field → SetStatusCondition →
// Status().Patch body repeated across the 15 patch{Level}{Outcome} functions,
// the approve-annotation consume-and-resume two-step repeated at six
// planner-tier gate hooks, and the namespace-List+ownerRef-count body
// repeated by the four countChild* helpers.
//
// This is explicitly NOT a generic level-reconciler (Phase 41 seed's own
// boundary) — reconcilePlannerDispatch/handleJobCompletion bodies stay
// intact in each *_controller.go file; only their leaf status writes extract
// here, mirroring the "one shared body + thin typed entry points" idiom
// already established by spawnReporterIfNeeded (dispatch_helpers.go) and
// triggerBoundaryPush (boundary_push.go).
package controller

import (
	"context"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	tideprojectv1alpha3 "github.com/jsquirrelz/tide/api/v1alpha3"
	"github.com/jsquirrelz/tide/internal/gates"
)

// patchLevelStatus is the shared "MergeFrom → optional field set →
// SetStatusCondition(s) → Status().Patch" leaf backing all 15
// patch{Milestone,Phase,Plan,Task}{Succeeded,Failed,Rejected,
// AwaitingApproval,FileTouchMismatch} wrappers.
//
// fieldPtr/newValue let one leaf cover both shapes seen across the 15
// originals: most set Status.Phase (pass &obj.Status.Phase); some (Rejected)
// mutate no field at all (pass fieldPtr=nil or newValue=""); one
// (patchPlanFileTouchMismatch) mutates a different string field
// (Status.ValidationState) — fieldPtr is generic so that call site passes
// &plan.Status.ValidationState instead.
//
// optimisticLock selects client.MergeFromWithOptimisticLock{} instead of a
// plain client.MergeFrom — required by the three AwaitingApproval park
// wrappers (Milestone/Phase/Plan) so a stale-snapshot re-park 409s instead of
// blind-merging over a concurrent approve's Running+ApprovedByUser write
// (see consumeApproveAndResume's doc and the milestone AwaitingApproval
// wrapper for the full race description).
//
// cond/extra are applied via meta.SetStatusCondition in order, each stamped
// with LastTransitionTime at call time — covers both the single-condition
// originals and the Succeeded wrappers' two-condition (Succeeded +
// WaveOrLevelPaused=False) shape.
func patchLevelStatus(
	ctx context.Context,
	c client.Client,
	obj client.Object,
	conditions *[]metav1.Condition,
	fieldPtr *string,
	newValue string,
	optimisticLock bool,
	result ctrl.Result,
	cond metav1.Condition,
	extra ...metav1.Condition,
) (ctrl.Result, error) {
	base, ok := obj.DeepCopyObject().(client.Object)
	if !ok {
		return ctrl.Result{}, nil
	}
	var patch client.Patch
	if optimisticLock {
		patch = client.MergeFromWithOptions(base, client.MergeFromWithOptimisticLock{})
	} else {
		patch = client.MergeFrom(base)
	}
	if fieldPtr != nil && newValue != "" {
		*fieldPtr = newValue
	}
	cond.LastTransitionTime = metav1.Now()
	meta.SetStatusCondition(conditions, cond)
	for _, ec := range extra {
		ec.LastTransitionTime = metav1.Now()
		meta.SetStatusCondition(conditions, ec)
	}
	if err := c.Status().Patch(ctx, obj, patch); err != nil {
		return ctrl.Result{}, err
	}
	return result, nil
}

// consumeApproveAndResume performs the approve-annotation consume-and-resume
// two-step shared by the six planner-tier gate hooks (Milestone/Phase/Plan,
// each at their AwaitingApproval early-return AND their handleJobCompletion
// gate-policy hook): (a) gates.ConsumeApprove + SetAnnotations + a plain
// MergeFrom annotation Patch FIRST — T-04-G2: the one-shot annotation must be
// consumed before any status write, so a crash between the two steps cannot
// double-fire approval — then (b) a status patch (via patchLevelStatus)
// setting the level's phase field to LevelPhaseRunning and
// ConditionWaveOrLevelPaused=False/ReasonApprovedByUser with the caller's
// resumeMessage.
//
// D-04 invariant: this helper NEVER advances a level to Succeeded — it always
// returns to Running. Succession past children is exclusively the
// Running-branch/handleJobCompletion's ChildCount-gated job; callers must not
// use this helper as a shortcut past that gate.
//
// Returns (ctrl.Result{Requeue: true}, nil) on success, mirroring today's
// "requeue immediately — the Running branch owns ChildCount-gated succession"
// contract. Callers at the AwaitingApproval early-return can propagate the
// result directly; callers already inside handleJobCompletion that fall
// through to ChildCount-gated succession should check only the error and
// ignore the requeue result (see call sites).
func consumeApproveAndResume(
	ctx context.Context,
	c client.Client,
	obj client.Object,
	conditions *[]metav1.Condition,
	fieldPtr *string,
	level string,
	resumeMessage string,
) (ctrl.Result, error) {
	newAnno := gates.ConsumeApprove(obj, level)
	annoBase, ok := obj.DeepCopyObject().(client.Object)
	if !ok {
		return ctrl.Result{}, nil
	}
	annoPatch := client.MergeFrom(annoBase)
	obj.SetAnnotations(newAnno)
	if err := c.Patch(ctx, obj, annoPatch); err != nil {
		return ctrl.Result{}, err
	}
	return patchLevelStatus(ctx, c, obj, conditions, fieldPtr, tideprojectv1alpha3.LevelPhaseRunning, false, ctrl.Result{Requeue: true},
		metav1.Condition{
			Type:    tideprojectv1alpha3.ConditionWaveOrLevelPaused,
			Status:  metav1.ConditionFalse,
			Reason:  tideprojectv1alpha3.ReasonApprovedByUser,
			Message: resumeMessage,
		},
	)
}

// countChildren returns the number of objects in list (after a
// namespace-scoped List) whose Controller-true ownerRef UID matches ownerUID.
// Backs the four countChild{Phases,Plans,Tasks,Milestones} wrappers (plan
// 09-08). Unified on the controller-ownerRef+UID predicate: the prior
// per-Kind loops (countChildPhases/Plans/Tasks) matched on ref.Kind+ref.UID
// without checking the Controller flag, while countChildMilestones used
// metav1.IsControlledBy (Controller=true + UID only, no Kind check). These
// are behaviorally equivalent in this codebase — every child CRD's owner ref
// is stamped via internal/owner.EnsureOwnerRef → controllerutil.
// SetControllerReference, which always sets Controller=true with the correct
// Kind — so matching on Controller+UID (dropping the redundant Kind check)
// changes no observed behavior while unifying the four bodies into one.
func countChildren(ctx context.Context, c client.Client, namespace string, ownerUID types.UID, list client.ObjectList) int {
	if err := c.List(ctx, list, client.InNamespace(namespace)); err != nil {
		return 0
	}
	items, err := meta.ExtractList(list)
	if err != nil {
		return 0
	}
	count := 0
	for _, item := range items {
		obj, ok := item.(metav1.Object)
		if !ok {
			continue
		}
		for _, ref := range obj.GetOwnerReferences() {
			if ref.UID == ownerUID && ref.Controller != nil && *ref.Controller {
				count++
				break
			}
		}
	}
	return count
}
