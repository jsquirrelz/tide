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

// approve.go — `tide approve` cobra command + testable seam. Writes the
// canonical approve annotation (tideproject.k8s/approve-<level>=true) on the
// AwaitingApproval level discovered from Project.Status, or
// tideproject.k8s/approve-wave-<N>=true on the named Plan when --wave is
// passed. All writes use client.Patch + client.MergeFrom for one-shot
// annotation semantics that mirror the reconciler-side reads in plan 04-04.

package main

import (
	"context"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/validation"
	"sigs.k8s.io/controller-runtime/pkg/client"

	tidev1alpha1 "github.com/jsquirrelz/tide/api/v1alpha1"
	"github.com/jsquirrelz/tide/internal/gates"
)

// approveWaveRE validates the --wave flag SHAPE — non-empty plan name +
// slash + non-negative integer. WR-07 fix: the loose character class
// `[a-z0-9.-]+` accepted invalid plan names like `..-..` that the apiserver
// would then reject on Get with a confusing apiserver-side error. After
// matching this regex, the plan-name component is additionally validated
// against k8s.io/apimachinery/pkg/util/validation.IsDNS1123Label so the
// CLI produces a friendly local error instead of a 422 from the apiserver.
var approveWaveRE = regexp.MustCompile(`^[^/]+/\d+$`)

// approveRun is the testable seam. Resolves the target object (Project for
// level discovery; Plan for --wave) and writes the canonical approve
// annotation via client.MergeFrom + client.Patch.
//
// The out parameter is reserved for future stderr-style operator feedback
// (e.g., "approved milestone ms-alpha on project my-project"). Currently the
// seam exits silently on success; callers in production should add a single
// confirmation line at the cobra adapter layer.
func approveRun(ctx context.Context, c client.Client, ns, projectName, waveFlag string, out io.Writer) error {
	// Branch A: --wave plan/N — write approve-wave-N on the named Plan.
	if waveFlag != "" {
		return approveWave(ctx, c, ns, projectName, waveFlag)
	}
	// Branch B: discover the AwaitingApproval level on the Project's child
	// chain and write approve-<level>=true on that child.
	return approveLevel(ctx, c, ns, projectName)
}

// approveWave validates the --wave flag, fetches the Plan, and writes
// approve-wave-<N>=true. Project existence is verified first so a missing
// Project surfaces the same friendly error as the level path.
func approveWave(ctx context.Context, c client.Client, ns, projectName, waveFlag string) error {
	if !approveWaveRE.MatchString(waveFlag) {
		return fmt.Errorf("--wave must be <plan-name>/<integer>; got %q", waveFlag)
	}
	parts := strings.SplitN(waveFlag, "/", 2)
	planName := parts[0]
	// WR-07 fix: validate plan-name as DNS-1123 BEFORE issuing the apiserver
	// Get so invalid names produce a friendly local error rather than an
	// apiserver-side 422 / "name is invalid" string.
	if errs := validation.IsDNS1123Label(planName); len(errs) > 0 {
		return fmt.Errorf("--wave: plan name %q is not a valid DNS-1123 label: %s", planName, strings.Join(errs, "; "))
	}
	waveN, err := strconv.Atoi(parts[1])
	if err != nil {
		return fmt.Errorf("--wave: parse wave integer: %w", err)
	}

	// Verify the Project exists so the operator gets a clear NotFound rather
	// than a "Plan not found" error when the plan name is correct but the
	// project name is wrong.
	var proj tidev1alpha1.Project
	if err := c.Get(ctx, types.NamespacedName{Namespace: ns, Name: projectName}, &proj); err != nil {
		if apierrors.IsNotFound(err) {
			return fmt.Errorf("tide: project %q not found in namespace %q", projectName, ns)
		}
		return fmt.Errorf("get project: %w", err)
	}

	var plan tidev1alpha1.Plan
	if err := c.Get(ctx, types.NamespacedName{Namespace: ns, Name: planName}, &plan); err != nil {
		if apierrors.IsNotFound(err) {
			return fmt.Errorf("tide: plan %q not found in namespace %q", planName, ns)
		}
		return fmt.Errorf("get plan: %w", err)
	}

	patch := client.MergeFrom(plan.DeepCopy())
	anno := plan.GetAnnotations()
	if anno == nil {
		anno = map[string]string{}
	}
	anno[gates.AnnotationApproveWavePrefix+strconv.Itoa(waveN)] = "true"
	plan.SetAnnotations(anno)
	if err := c.Patch(ctx, &plan, patch); err != nil {
		return fmt.Errorf("patch plan %s: %w", planName, err)
	}
	return nil
}

// approveLevel walks the child CRDs of the Project looking for one in
// Status.Phase=AwaitingApproval. Order: Milestone → Phase → Plan → Task. The
// first matching child receives the approve-<level>=true annotation.
//
// Per the plan: discovers level from child CRDs (not from Project.Status
// conditions directly, since the AwaitingApproval state lives on the child
// itself per plan 04-05's patchMilestoneAwaitingApproval / etc.).
func approveLevel(ctx context.Context, c client.Client, ns, projectName string) error {
	var proj tidev1alpha1.Project
	if err := c.Get(ctx, types.NamespacedName{Namespace: ns, Name: projectName}, &proj); err != nil {
		if apierrors.IsNotFound(err) {
			return fmt.Errorf("tide: project %q not found in namespace %q", projectName, ns)
		}
		return fmt.Errorf("get project: %w", err)
	}

	// Iterate child kinds in dependency-order. The first child matching
	// AwaitingApproval is the one the operator is gating on.
	if obj, level, err := findAwaitingMilestone(ctx, c, ns, projectName); err != nil {
		return err
	} else if obj != nil {
		return patchApproveLevel(ctx, c, obj, level)
	}
	if obj, level, err := findAwaitingPhase(ctx, c, ns, projectName); err != nil {
		return err
	} else if obj != nil {
		return patchApproveLevel(ctx, c, obj, level)
	}
	if obj, level, err := findAwaitingPlan(ctx, c, ns, projectName); err != nil {
		return err
	} else if obj != nil {
		return patchApproveLevel(ctx, c, obj, level)
	}
	if obj, level, err := findAwaitingTask(ctx, c, ns, projectName); err != nil {
		return err
	} else if obj != nil {
		return patchApproveLevel(ctx, c, obj, level)
	}
	return fmt.Errorf("tide: no level awaiting approval on project %s", projectName)
}

// findAwaitingMilestone lists Milestones in the namespace, filtered to the
// Project by the canonical tideproject.k8s/project label (per
// internal/controller/plan_controller.go vocabulary). Returns the first
// Milestone whose Status.Phase is "AwaitingApproval".
func findAwaitingMilestone(ctx context.Context, c client.Client, ns, projectName string) (client.Object, string, error) {
	var list tidev1alpha1.MilestoneList
	if err := c.List(ctx, &list, client.InNamespace(ns)); err != nil {
		return nil, "", fmt.Errorf("list milestones: %w", err)
	}
	for i := range list.Items {
		m := &list.Items[i]
		if m.Labels["tideproject.k8s/project"] != projectName {
			continue
		}
		if m.Status.Phase == "AwaitingApproval" {
			return m, "milestone", nil
		}
	}
	return nil, "", nil
}

func findAwaitingPhase(ctx context.Context, c client.Client, ns, projectName string) (client.Object, string, error) {
	var list tidev1alpha1.PhaseList
	if err := c.List(ctx, &list, client.InNamespace(ns)); err != nil {
		return nil, "", fmt.Errorf("list phases: %w", err)
	}
	for i := range list.Items {
		p := &list.Items[i]
		if p.Labels["tideproject.k8s/project"] != projectName {
			continue
		}
		if p.Status.Phase == "AwaitingApproval" {
			return p, "phase", nil
		}
	}
	return nil, "", nil
}

func findAwaitingPlan(ctx context.Context, c client.Client, ns, projectName string) (client.Object, string, error) {
	var list tidev1alpha1.PlanList
	if err := c.List(ctx, &list, client.InNamespace(ns)); err != nil {
		return nil, "", fmt.Errorf("list plans: %w", err)
	}
	for i := range list.Items {
		p := &list.Items[i]
		if p.Labels["tideproject.k8s/project"] != projectName {
			continue
		}
		if p.Status.Phase == "AwaitingApproval" {
			return p, "plan", nil
		}
	}
	return nil, "", nil
}

func findAwaitingTask(ctx context.Context, c client.Client, ns, projectName string) (client.Object, string, error) {
	var list tidev1alpha1.TaskList
	if err := c.List(ctx, &list, client.InNamespace(ns)); err != nil {
		return nil, "", fmt.Errorf("list tasks: %w", err)
	}
	for i := range list.Items {
		t := &list.Items[i]
		if t.Labels["tideproject.k8s/project"] != projectName {
			continue
		}
		if t.Status.Phase == "AwaitingApproval" {
			return t, "task", nil
		}
	}
	return nil, "", nil
}

// patchApproveLevel writes approve-<level>=true on obj via client.MergeFrom +
// client.Patch. Mirrors the reconciler's annotation-write pattern in plan
// 04-05 (e.g. milestone_controller.go:289-296) — DeepCopy original, mutate
// annotations on the live obj, Patch with MergeFrom(original).
func patchApproveLevel(ctx context.Context, c client.Client, obj client.Object, level string) error {
	original := obj.DeepCopyObject().(client.Object)
	patch := client.MergeFrom(original)

	anno := obj.GetAnnotations()
	if anno == nil {
		anno = map[string]string{}
	}
	anno[gates.AnnotationApprovePrefix+level] = "true"
	obj.SetAnnotations(anno)

	if err := c.Patch(ctx, obj, patch); err != nil {
		return fmt.Errorf("patch %s/%s: %w", level, obj.GetName(), err)
	}
	return nil
}

// newApproveCmd is the cobra command constructor for `tide approve`.
func newApproveCmd() *cobra.Command {
	var waveFlag string
	c := &cobra.Command{
		Use:   "approve <project>",
		Short: "Approve the current AwaitingApproval level or a specific wave",
		Long: "tide approve writes the canonical approve annotation " +
			"(tideproject.k8s/approve-<level>=true) on the Project's current " +
			"AwaitingApproval level. With --wave <plan>/<N>, writes " +
			"tideproject.k8s/approve-wave-<N>=true on the named Plan instead.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := K8sClient()
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
			return approveRun(cmd.Context(), c, ns, args[0], waveFlag, cmd.OutOrStdout())
		},
	}
	c.Flags().StringVar(&waveFlag, "wave", "", "Approve a specific wave: <plan-name>/<integer>")
	return c
}
