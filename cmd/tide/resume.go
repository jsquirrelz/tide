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

// resume.go — `tide resume` cobra command + testable seam.
//
// Base mode (no flags): clears the tideproject.k8s/reject annotation via
// gates.ConsumeReject + client.Patch, allowing the reconcilers to re-enter
// the normal advance path. Mirrors the reconciler's annotation-consume
// pattern (e.g. milestone_controller.go:291-296).
//
// --retry-failed mode: additionally resets Status.Phase on every Failed
// level (Milestone/Phase/Plan/Task) of the project via the status subresource
// and stamps a ResumedByUser condition. This is the sanctioned CLI replacement
// for the run-1 kubectl recovery recipe:
//
//	kubectl patch <kind> <name> --subresource=status --type=merge \
//	    -p '{"status":{"phase":"","conditions":[]}}'
//
// The flag is deliberate friction (D-06) — legitimately dead work is not
// accidentally resurrected. Running levels are never touched (Pitfall 3 —
// no double-dispatch). Phase 13's HALT-01 will point its recovery story at
// this verb.

package main

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/spf13/cobra"
	batchv1 "k8s.io/api/batch/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	tidev1alpha3 "github.com/jsquirrelz/tide/api/v1alpha3"
	"github.com/jsquirrelz/tide/internal/gates"
	"github.com/jsquirrelz/tide/internal/owner"
)

// resumeRun is the testable seam. Fetches the Project, calls
// gates.ConsumeReject to get a new annotation map with the reject key
// removed, then Patches via MergeFrom.
//
// When retryFailed is true, also walks all four level kinds (Milestone, Phase,
// Plan, Task) labelled with the project name and resets any Failed items via
// the status subresource, stamping a ResumedByUser condition. Only
// Status.Phase=="Failed" items are touched — Running/Succeeded/empty levels
// are never modified (Pitfall 3).
//
// out receives one line per reset level; if retryFailed is true and no Failed
// levels were found, prints "tide: no Failed levels found". out may be nil
// when the caller does not need feedback (e.g. tests checking annotation-only
// behaviour).
func resumeRun(ctx context.Context, c client.Client, ns, projectName string, retryFailed bool, out io.Writer) error {
	var proj tidev1alpha3.Project
	if err := c.Get(ctx, types.NamespacedName{Namespace: ns, Name: projectName}, &proj); err != nil {
		if apierrors.IsNotFound(err) {
			return fmt.Errorf("tide: project %q not found in namespace %q", projectName, ns)
		}
		return fmt.Errorf("get project: %w", err)
	}

	patch := client.MergeFrom(proj.DeepCopy())
	newAnnotations := gates.ConsumeReject(&proj)

	// Phase 34 D-13: if the Project shows boundary-push retry state (a sticky
	// IntegrationIncomplete condition, or a non-zero attempts tally even
	// without the sticky condition yet), stamp the reset-boundary-push
	// annotation so the controller resets Attempts/LastError and clears the
	// condition on its next reconcile (consumeResetBoundaryPushAnnotation).
	imCond := meta.FindStatusCondition(proj.Status.Conditions, tidev1alpha3.ConditionIntegrationIncomplete)
	needsBoundaryPushReset := (imCond != nil && imCond.Status == metav1.ConditionTrue) ||
		proj.Status.BoundaryPush.Attempts > 0
	if needsBoundaryPushReset {
		if newAnnotations == nil {
			newAnnotations = make(map[string]string)
		}
		newAnnotations[gates.AnnotationResetBoundaryPush] = "true"
	}
	proj.SetAnnotations(newAnnotations)
	if err := c.Patch(ctx, &proj, patch); err != nil {
		return fmt.Errorf("patch project: %w", err)
	}
	if needsBoundaryPushReset && out != nil {
		fmt.Fprintln(out, "tide: reset boundary-push retry state (tideproject.k8s/reset-boundary-push); "+
			"the controller will clear Attempts/LastError and any sticky IntegrationIncomplete condition on its next reconcile")
	}

	// Phase 13 D-06: clear BillingHalt unconditionally (operator chose recovery
	// by invoking resume; no auto-probe of provider balance). Re-fetch so the
	// status patch uses a fresh resourceVersion after the annotation patch above.
	// Skip the status patch when no BillingHalt condition exists — avoids a
	// spurious no-op patch on clients that don't register Project as a status
	// subresource (e.g. legacy fake-client tests).
	if err := c.Get(ctx, types.NamespacedName{Namespace: ns, Name: projectName}, &proj); err != nil {
		return fmt.Errorf("re-get project for BillingHalt clear: %w", err)
	}
	haltCond := meta.FindStatusCondition(proj.Status.Conditions, tidev1alpha3.ConditionBillingHalt)
	if haltCond != nil {
		hadBillingHalt := haltCond.Status == metav1.ConditionTrue
		patch2 := client.MergeFrom(proj.DeepCopy())
		meta.RemoveStatusCondition(&proj.Status.Conditions, tidev1alpha3.ConditionBillingHalt)
		if err := c.Status().Patch(ctx, &proj, patch2); err != nil {
			return fmt.Errorf("patch status (clear BillingHalt): %w", err)
		}
		if hadBillingHalt {
			// WR-03 (Plan 13-05): stamp billing-resumed-at so the reconciler backstop
			// can distinguish pre-resume stragglers from fresh post-resume failures.
			// Annotations are NOT status — separate metadata patch from the condition
			// removal above (different subresource, different resourceVersion window).
			if err := c.Get(ctx, types.NamespacedName{Namespace: ns, Name: projectName}, &proj); err != nil {
				return fmt.Errorf("re-get project for billing-resumed-at stamp: %w", err)
			}
			metaPatch := client.MergeFrom(proj.DeepCopy())
			ann := proj.GetAnnotations()
			if ann == nil {
				ann = make(map[string]string)
			}
			ann[tidev1alpha3.AnnotationBillingResumedAt] = time.Now().UTC().Format(time.RFC3339)
			proj.SetAnnotations(ann)
			if err := c.Patch(ctx, &proj, metaPatch); err != nil {
				return fmt.Errorf("patch metadata (billing-resumed-at stamp): %w", err)
			}
			if out != nil {
				fmt.Fprintln(out, "tide: cleared BillingHalt (billing recovery); "+
					"pre-resume in-flight sessions can no longer re-trip the halt")
			}
		}
	}

	// Phase 51 ESC-03 (HI-01): VerifyHalt recovery is UNCONDITIONAL on plain
	// `tide resume` — a DISTINCT halt class from the conservative FailureHalt
	// that --retry-failed clears (setVerifyHaltIfNeeded's own operator message
	// points at plain `tide resume`). No-op when no VerifyHalt condition exists.
	if err := clearVerifyHaltIfPresent(ctx, c, ns, projectName, out); err != nil {
		return err
	}

	if !retryFailed {
		return nil
	}

	// Phase 25 CR-01: reset the Failed levels FIRST, then clear FailureHalt LAST.
	// Ordering matters: while any Task still has Status.Phase=="Failed", the
	// TaskReconciler terminal short-circuit (task_controller.go gateChecks) and
	// handleJobCompletion re-stamp ConditionFailureHalt via setFailureHaltIfNeeded.
	// Clearing the halt before resetting the tasks opened a re-stamp race that
	// re-froze the project after a "successful" resume (the run-1 failure mode this
	// verb replaces). Resetting first means no task can re-stamp once it leaves
	// "Failed"; clearing last + the AnnotationFailureResumedAt fence (read by
	// setFailureHaltIfNeeded) closes the residual informer-lag window.
	if err := retryFailedLevels(ctx, c, ns, projectName, out); err != nil {
		return err
	}

	// Phase 25 D-04 / CR-02: clear FailureHalt (conservative halt recovery) and stamp
	// AnnotationFailureResumedAt so the reconciler backstop refuses to re-stamp a halt
	// for any pre-resume straggler failure (mirrors the BillingHalt resume fence above).
	// Re-fetch for a fresh resourceVersion after the Task phase-reset status patches.
	if err := c.Get(ctx, types.NamespacedName{Namespace: ns, Name: projectName}, &proj); err != nil {
		return fmt.Errorf("re-get project for FailureHalt clear: %w", err)
	}
	fhCond := meta.FindStatusCondition(proj.Status.Conditions, tidev1alpha3.ConditionFailureHalt)
	if fhCond != nil && fhCond.Status == metav1.ConditionTrue {
		patch3 := client.MergeFrom(proj.DeepCopy())
		meta.RemoveStatusCondition(&proj.Status.Conditions, tidev1alpha3.ConditionFailureHalt)
		if err := c.Status().Patch(ctx, &proj, patch3); err != nil {
			return fmt.Errorf("patch status (clear FailureHalt): %w", err)
		}

		// CR-02 fence stamp: AnnotationFailureResumedAt is metadata (not status) —
		// separate metadata patch from the condition removal above. Re-fetch first
		// for a fresh resourceVersion after the status patch.
		if err := c.Get(ctx, types.NamespacedName{Namespace: ns, Name: projectName}, &proj); err != nil {
			return fmt.Errorf("re-get project for failure-resumed-at stamp: %w", err)
		}
		metaPatch := client.MergeFrom(proj.DeepCopy())
		ann := proj.GetAnnotations()
		if ann == nil {
			ann = make(map[string]string)
		}
		ann[tidev1alpha3.AnnotationFailureResumedAt] = time.Now().UTC().Format(time.RFC3339)
		proj.SetAnnotations(ann)
		if err := c.Patch(ctx, &proj, metaPatch); err != nil {
			return fmt.Errorf("patch metadata (failure-resumed-at stamp): %w", err)
		}

		if out != nil {
			fmt.Fprintln(out, "tide: cleared FailureHalt; pre-resume Failed-task stragglers can no longer re-trip the halt")
		}
	}

	return nil
}

// retryFailedLevels walks all four level kinds (Milestone → Phase → Plan →
// Task) in the namespace, resetting any item whose Status.Phase=="Failed" via
// the status subresource. Prints one line per reset to out.
func retryFailedLevels(ctx context.Context, c client.Client, ns, projectName string, out io.Writer) error {
	resetCount := 0
	resumedByUser := metav1.Condition{
		Type:               tidev1alpha3.ConditionWaveOrLevelPaused,
		Status:             metav1.ConditionFalse,
		Reason:             tidev1alpha3.ReasonResumedByUser,
		Message:            "Level reset by tide resume --retry-failed; reconciler will re-dispatch",
		LastTransitionTime: metav1.Now(),
	}

	// Milestone
	var msList tidev1alpha3.MilestoneList
	if err := c.List(ctx, &msList,
		client.InNamespace(ns),
		client.MatchingLabels{owner.LabelProject: projectName},
	); err != nil {
		return fmt.Errorf("list milestones: %w", err)
	}
	for i := range msList.Items {
		item := &msList.Items[i]
		if item.Status.Phase != tidev1alpha3.LevelPhaseFailed {
			continue
		}
		orig := item.DeepCopy()
		patch := client.MergeFrom(orig)
		item.Status.Phase = ""
		item.Status.Conditions = nil
		meta.SetStatusCondition(&item.Status.Conditions, resumedByUser)
		if err := c.Status().Patch(ctx, item, patch); err != nil {
			return fmt.Errorf("reset milestone %s: %w", item.Name, err)
		}
		printReset(out, "Milestone", item.Name)
		resetCount++
	}

	// Phase
	var phList tidev1alpha3.PhaseList
	if err := c.List(ctx, &phList,
		client.InNamespace(ns),
		client.MatchingLabels{owner.LabelProject: projectName},
	); err != nil {
		return fmt.Errorf("list phases: %w", err)
	}
	for i := range phList.Items {
		item := &phList.Items[i]
		if item.Status.Phase != tidev1alpha3.LevelPhaseFailed {
			continue
		}
		orig := item.DeepCopy()
		patch := client.MergeFrom(orig)
		item.Status.Phase = ""
		item.Status.Conditions = nil
		meta.SetStatusCondition(&item.Status.Conditions, resumedByUser)
		if err := c.Status().Patch(ctx, item, patch); err != nil {
			return fmt.Errorf("reset phase %s: %w", item.Name, err)
		}
		printReset(out, "Phase", item.Name)
		resetCount++
	}

	// Plan
	var plList tidev1alpha3.PlanList
	if err := c.List(ctx, &plList,
		client.InNamespace(ns),
		client.MatchingLabels{owner.LabelProject: projectName},
	); err != nil {
		return fmt.Errorf("list plans: %w", err)
	}
	for i := range plList.Items {
		item := &plList.Items[i]
		if item.Status.Phase != tidev1alpha3.LevelPhaseFailed {
			continue
		}
		orig := item.DeepCopy()
		patch := client.MergeFrom(orig)
		item.Status.Phase = ""
		item.Status.Conditions = nil
		// Phase 34: a wave-integration failure parks retry state on the Plan
		// (Wave/Attempts/LastError). Without zeroing it, a plan that failed
		// at the Attempts cap re-fails terminally after ONE fresh attempt —
		// the same-wave reset arm in handleWaveIntegrationFailure only zeroes
		// the counter when the blocking wave CHANGES.
		item.Status.WaveIntegration = tidev1alpha3.WaveIntegrationStatus{}
		meta.SetStatusCondition(&item.Status.Conditions, resumedByUser)
		if err := c.Status().Patch(ctx, item, patch); err != nil {
			return fmt.Errorf("reset plan %s: %w", item.Name, err)
		}
		// Delete any stale terminal wave-integration Job for this Plan: its
		// deterministic name (tide-push-wave-<plan.UID>-<N>) survives up to
		// TTLSecondsAfterFinished, and the resumed Plan's next reconcile
		// would re-read the stale failure envelope and instantly re-fail.
		if err := deleteStaleWaveJobs(ctx, c, ns, string(item.UID), out); err != nil {
			return fmt.Errorf("delete stale wave-integration jobs for plan %s: %w", item.Name, err)
		}
		printReset(out, "Plan", item.Name)
		resetCount++
	}

	// Task
	var tkList tidev1alpha3.TaskList
	if err := c.List(ctx, &tkList,
		client.InNamespace(ns),
		client.MatchingLabels{owner.LabelProject: projectName},
	); err != nil {
		return fmt.Errorf("list tasks: %w", err)
	}
	for i := range tkList.Items {
		item := &tkList.Items[i]
		if item.Status.Phase != tidev1alpha3.LevelPhaseFailed {
			continue
		}
		orig := item.DeepCopy()
		patch := client.MergeFrom(orig)
		item.Status.Phase = ""
		item.Status.Conditions = nil
		meta.SetStatusCondition(&item.Status.Conditions, resumedByUser)
		if err := c.Status().Patch(ctx, item, patch); err != nil {
			return fmt.Errorf("reset task %s: %w", item.Name, err)
		}
		printReset(out, "Task", item.Name)
		resetCount++
	}

	if resetCount == 0 {
		if out != nil {
			fmt.Fprintln(out, "tide: no Failed levels found")
		}
	}
	return nil
}

// clearVerifyHaltIfPresent recovers a project-wide VerifyHalt (Phase 51
// ESC-03/HI-01): a no-op unless ConditionVerifyHalt=True. Mirrors the Phase 25
// CR-01 ordering — reset the VerifyHalted Tasks FIRST, then clear the
// project-wide condition LAST and stamp AnnotationVerifyResumedAt, so a
// pre-resume straggler verifier completion cannot re-stamp the halt after the
// clear (setVerifyHaltIfNeeded reads that fence). Extracted from resumeRun to
// keep its cyclomatic complexity in check.
func clearVerifyHaltIfPresent(ctx context.Context, c client.Client, ns, projectName string, out io.Writer) error {
	var proj tidev1alpha3.Project
	if err := c.Get(ctx, types.NamespacedName{Namespace: ns, Name: projectName}, &proj); err != nil {
		return fmt.Errorf("re-get project for VerifyHalt clear: %w", err)
	}
	vhCond := meta.FindStatusCondition(proj.Status.Conditions, tidev1alpha3.ConditionVerifyHalt)
	if vhCond == nil || vhCond.Status != metav1.ConditionTrue {
		return nil
	}
	if err := resetVerifyHaltedTasks(ctx, c, ns, projectName, out); err != nil {
		return err
	}
	// Re-fetch for a fresh resourceVersion after the Task phase-reset patches.
	if err := c.Get(ctx, types.NamespacedName{Namespace: ns, Name: projectName}, &proj); err != nil {
		return fmt.Errorf("re-get project for VerifyHalt clear: %w", err)
	}
	patchVH := client.MergeFrom(proj.DeepCopy())
	meta.RemoveStatusCondition(&proj.Status.Conditions, tidev1alpha3.ConditionVerifyHalt)
	if err := c.Status().Patch(ctx, &proj, patchVH); err != nil {
		return fmt.Errorf("patch status (clear VerifyHalt): %w", err)
	}
	// CR-02 fence stamp: AnnotationVerifyResumedAt is metadata (not status) —
	// separate metadata patch from the condition removal above. Re-fetch first
	// for a fresh resourceVersion after the status patch.
	if err := c.Get(ctx, types.NamespacedName{Namespace: ns, Name: projectName}, &proj); err != nil {
		return fmt.Errorf("re-get project for verify-resumed-at stamp: %w", err)
	}
	metaPatch := client.MergeFrom(proj.DeepCopy())
	ann := proj.GetAnnotations()
	if ann == nil {
		ann = make(map[string]string)
	}
	ann[tidev1alpha3.AnnotationVerifyResumedAt] = time.Now().UTC().Format(time.RFC3339)
	proj.SetAnnotations(ann)
	if err := c.Patch(ctx, &proj, metaPatch); err != nil {
		return fmt.Errorf("patch metadata (verify-resumed-at stamp): %w", err)
	}
	if out != nil {
		fmt.Fprintln(out, "tide: cleared VerifyHalt; pre-resume verifier stragglers can no longer re-trip the halt")
	}
	return nil
}

// resetVerifyHaltedTasks resets any Task whose Status.Phase=="VerifyHalted"
// (Phase 51 ESC-03/HI-01) to the empty phase via the status subresource so
// the TaskReconciler re-dispatches a fresh executor attempt once
// ConditionVerifyHalt clears. VerifyHalted is a DISTINCT halt class from
// Failed — retryFailedLevels never touches these (they are not
// LevelPhaseFailed), so plain `tide resume` owns their recovery. Only
// Status.Phase=="VerifyHalted" items are touched; Running/Succeeded/Failed/
// empty tasks are never modified. Prints one line per reset to out.
func resetVerifyHaltedTasks(ctx context.Context, c client.Client, ns, projectName string, out io.Writer) error {
	var tkList tidev1alpha3.TaskList
	if err := c.List(ctx, &tkList,
		client.InNamespace(ns),
		client.MatchingLabels{owner.LabelProject: projectName},
	); err != nil {
		return fmt.Errorf("list tasks for verify-halt reset: %w", err)
	}
	resumedByUser := metav1.Condition{
		Type:               tidev1alpha3.ConditionWaveOrLevelPaused,
		Status:             metav1.ConditionFalse,
		Reason:             tidev1alpha3.ReasonResumedByUser,
		Message:            "VerifyHalted task reset by tide resume; reconciler will re-dispatch",
		LastTransitionTime: metav1.Now(),
	}
	for i := range tkList.Items {
		item := &tkList.Items[i]
		if item.Status.Phase != tidev1alpha3.LevelPhaseVerifyHalted {
			continue
		}
		orig := item.DeepCopy()
		patch := client.MergeFrom(orig)
		item.Status.Phase = ""
		item.Status.Conditions = nil
		meta.SetStatusCondition(&item.Status.Conditions, resumedByUser)
		if err := c.Status().Patch(ctx, item, patch); err != nil {
			return fmt.Errorf("reset verify-halted task %s: %w", item.Name, err)
		}
		printReset(out, "Task", item.Name)
	}
	return nil
}

// printReset writes one line of per-level feedback to out (nil-safe).
func printReset(out io.Writer, kind, name string) {
	if out == nil {
		return
	}
	fmt.Fprintf(out, "tide: reset %s/%s for re-dispatch\n", kind, name)
}

// deleteStaleWaveJobs removes terminal wave-integration Jobs named
// tide-push-wave-<planUID>-<N> (Phase 34). Their deterministic names live up
// to TTLSecondsAfterFinished past failure, and a resumed Plan's reconcile
// Gets the name first — a lingering failed Job re-fails the Plan from its
// stale envelope before a fresh attempt can dispatch. Background propagation
// mirrors the controller's own retry-path deletion.
func deleteStaleWaveJobs(ctx context.Context, c client.Client, ns, planUID string, out io.Writer) error {
	var jobs batchv1.JobList
	if err := c.List(ctx, &jobs, client.InNamespace(ns)); err != nil {
		return fmt.Errorf("list jobs: %w", err)
	}
	prefix := fmt.Sprintf("tide-push-wave-%s-", planUID)
	policy := metav1.DeletePropagationBackground
	for i := range jobs.Items {
		job := &jobs.Items[i]
		if !strings.HasPrefix(job.Name, prefix) {
			continue
		}
		err := c.Delete(ctx, job, &client.DeleteOptions{PropagationPolicy: &policy})
		if err != nil && !apierrors.IsNotFound(err) {
			return fmt.Errorf("delete job %s: %w", job.Name, err)
		}
		if out != nil {
			fmt.Fprintf(out, "tide: deleted stale wave-integration job %s\n", job.Name)
		}
	}
	return nil
}

// newResumeCmd constructs the cobra command for `tide resume`.
func newResumeCmd() *cobra.Command {
	var retryFailed bool
	c := &cobra.Command{
		Use:   "resume <project>",
		Short: "Clear a tideproject.k8s/reject annotation on a Project",
		Long: "tide resume clears the tideproject.k8s/reject annotation via " +
			"gates.ConsumeReject + client.Patch. The reconcilers re-enter the " +
			"normal advance path. It also unconditionally resets any boundary-push " +
			"bounded-retry state (Phase 34 D-13): a Project with a sticky " +
			"IntegrationIncomplete condition or a non-zero BoundaryPush.Attempts " +
			"tally is stamped with tideproject.k8s/reset-boundary-push=true, which " +
			"the controller consumes to zero Attempts/LastError and clear the " +
			"condition. It also clears a project-wide VerifyHalt (Phase 51 " +
			"ESC-03) — resetting any VerifyHalted Task for re-dispatch and " +
			"stamping a verify-resumed-at fence — a DISTINCT halt class from the " +
			"conservative FailureHalt that --retry-failed clears.\n\n" +
			"With --retry-failed, also resets Status.Phase on every Failed level " +
			"(Milestone/Phase/Plan/Task) of the project via the status subresource " +
			"and stamps a ResumedByUser condition. This is the sanctioned replacement " +
			"for the manual kubectl status-patch recipe from run-1 finding 9a. " +
			"Running levels are never touched.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cl, err := K8sClient()
			if err != nil {
				return err
			}
			ns, err := resolveNamespace()
			if err != nil {
				return err
			}
			if ns == "" {
				ns = "default"
			}
			return resumeRun(cmd.Context(), cl, ns, args[0], retryFailed, cmd.OutOrStdout())
		},
	}
	c.Flags().BoolVar(&retryFailed, "retry-failed", false,
		"Also reset Status.Phase on Failed levels so reconcilers re-dispatch them")
	return c
}
