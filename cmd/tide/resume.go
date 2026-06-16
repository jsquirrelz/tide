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
	"time"

	"github.com/spf13/cobra"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	tidev1alpha1 "github.com/jsquirrelz/tide/api/v1alpha2"
	"github.com/jsquirrelz/tide/internal/gates"
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
	var proj tidev1alpha1.Project
	if err := c.Get(ctx, types.NamespacedName{Namespace: ns, Name: projectName}, &proj); err != nil {
		if apierrors.IsNotFound(err) {
			return fmt.Errorf("tide: project %q not found in namespace %q", projectName, ns)
		}
		return fmt.Errorf("get project: %w", err)
	}

	patch := client.MergeFrom(proj.DeepCopy())
	proj.SetAnnotations(gates.ConsumeReject(&proj))
	if err := c.Patch(ctx, &proj, patch); err != nil {
		return fmt.Errorf("patch project: %w", err)
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
	haltCond := meta.FindStatusCondition(proj.Status.Conditions, tidev1alpha1.ConditionBillingHalt)
	if haltCond != nil {
		hadBillingHalt := haltCond.Status == metav1.ConditionTrue
		patch2 := client.MergeFrom(proj.DeepCopy())
		meta.RemoveStatusCondition(&proj.Status.Conditions, tidev1alpha1.ConditionBillingHalt)
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
			ann[tidev1alpha1.AnnotationBillingResumedAt] = time.Now().UTC().Format(time.RFC3339)
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

	if !retryFailed {
		return nil
	}

	return retryFailedLevels(ctx, c, ns, projectName, out)
}

// retryFailedLevels walks all four level kinds (Milestone → Phase → Plan →
// Task) in the namespace, resetting any item whose Status.Phase=="Failed" via
// the status subresource. Prints one line per reset to out.
func retryFailedLevels(ctx context.Context, c client.Client, ns, projectName string, out io.Writer) error {
	resetCount := 0
	resumedByUser := metav1.Condition{
		Type:               tidev1alpha1.ConditionWaveOrLevelPaused,
		Status:             metav1.ConditionFalse,
		Reason:             tidev1alpha1.ReasonResumedByUser,
		Message:            "Level reset by tide resume --retry-failed; reconciler will re-dispatch",
		LastTransitionTime: metav1.Now(),
	}

	// Milestone
	var msList tidev1alpha1.MilestoneList
	if err := c.List(ctx, &msList,
		client.InNamespace(ns),
		client.MatchingLabels{"tideproject.k8s/project": projectName},
	); err != nil {
		return fmt.Errorf("list milestones: %w", err)
	}
	for i := range msList.Items {
		item := &msList.Items[i]
		if item.Status.Phase != "Failed" {
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
	var phList tidev1alpha1.PhaseList
	if err := c.List(ctx, &phList,
		client.InNamespace(ns),
		client.MatchingLabels{"tideproject.k8s/project": projectName},
	); err != nil {
		return fmt.Errorf("list phases: %w", err)
	}
	for i := range phList.Items {
		item := &phList.Items[i]
		if item.Status.Phase != "Failed" {
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
	var plList tidev1alpha1.PlanList
	if err := c.List(ctx, &plList,
		client.InNamespace(ns),
		client.MatchingLabels{"tideproject.k8s/project": projectName},
	); err != nil {
		return fmt.Errorf("list plans: %w", err)
	}
	for i := range plList.Items {
		item := &plList.Items[i]
		if item.Status.Phase != "Failed" {
			continue
		}
		orig := item.DeepCopy()
		patch := client.MergeFrom(orig)
		item.Status.Phase = ""
		item.Status.Conditions = nil
		meta.SetStatusCondition(&item.Status.Conditions, resumedByUser)
		if err := c.Status().Patch(ctx, item, patch); err != nil {
			return fmt.Errorf("reset plan %s: %w", item.Name, err)
		}
		printReset(out, "Plan", item.Name)
		resetCount++
	}

	// Task
	var tkList tidev1alpha1.TaskList
	if err := c.List(ctx, &tkList,
		client.InNamespace(ns),
		client.MatchingLabels{"tideproject.k8s/project": projectName},
	); err != nil {
		return fmt.Errorf("list tasks: %w", err)
	}
	for i := range tkList.Items {
		item := &tkList.Items[i]
		if item.Status.Phase != "Failed" {
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

// printReset writes one line of per-level feedback to out (nil-safe).
func printReset(out io.Writer, kind, name string) {
	if out == nil {
		return
	}
	fmt.Fprintf(out, "tide: reset %s/%s for re-dispatch\n", kind, name)
}

// newResumeCmd constructs the cobra command for `tide resume`.
func newResumeCmd() *cobra.Command {
	var retryFailed bool
	c := &cobra.Command{
		Use:   "resume <project>",
		Short: "Clear a tideproject.k8s/reject annotation on a Project",
		Long: "tide resume clears the tideproject.k8s/reject annotation via " +
			"gates.ConsumeReject + client.Patch. The reconcilers re-enter the " +
			"normal advance path.\n\n" +
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
