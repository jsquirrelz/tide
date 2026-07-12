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
//
// D-07 guard (Plan 12-02, narrowed in Plan 17-03 / DEBT-03 — Option A):
// approveLevel discovers the AwaitingApproval target FIRST via the
// findAwaiting* chain, then refuses only if THAT specific target is itself
// Failed. An unrelated Failed sibling elsewhere on the same project does NOT
// block approval of a healthy AwaitingApproval level, honoring the
// strict-failure-profile contract (siblings are independent; only dependents
// halt). When no AwaitingApproval level exists and a Failed level is present,
// the "retry-failed" hint is surfaced as an actionable fallback.
//
// --wave path (Option A): a --wave approve targets a specific Plan/wave and
// is NOT subject to the level-path guard; both paths are now coherent under
// Option A.

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
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/validation"
	"sigs.k8s.io/controller-runtime/pkg/client"

	tidev1alpha3 "github.com/jsquirrelz/tide/api/v1alpha3"
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
//
//nolint:unparam // out is a documented future seam (success-confirmation line); kept on the signature intentionally
func approveRun(ctx context.Context, c client.Client, ns, projectName, waveFlag string, out io.Writer) error {
	// Branch A: --wave plan/N — write approve-wave-N on the named Plan.
	// Option A (DEBT-03): --wave targets a specific Plan/wave, not a
	// project-wide gate; it is not subject to the level-path failed-level
	// guard. This is intentional — the operator is approving a precise wave
	// on a named Plan, which is a different authorization surface from
	// approving the next AwaitingApproval level in the hierarchy.
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
	var proj tidev1alpha3.Project
	if err := c.Get(ctx, types.NamespacedName{Namespace: ns, Name: projectName}, &proj); err != nil {
		if apierrors.IsNotFound(err) {
			return fmt.Errorf("tide: project %q not found in namespace %q", projectName, ns)
		}
		return fmt.Errorf("get project: %w", err)
	}

	var plan tidev1alpha3.Plan
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
// D-07 guard (Option A, DEBT-03): the guard is narrowed to the approval target.
// The AwaitingApproval target is discovered FIRST; refusal fires only if THAT
// specific target has Status.Phase=="Failed". Unrelated Failed siblings do NOT
// block approval of a healthy AwaitingApproval level — honoring the
// strict-failure-profile contract (T-17-08: siblings are independent).
//
// When no AwaitingApproval level exists at all, findFailedLevel is consulted
// as a UX fallback: if a Failed level is present, the operator sees the
// actionable "retry-failed" hint instead of a bare "no level awaiting" message.
// This preserves D-07's "approval never doubles as a spend-retry" intent for
// the case where the operator has no valid target to approve (T-17-07).
//
// Per the plan: discovers level from child CRDs (not from Project.Status
// conditions directly, since the AwaitingApproval state lives on the child
// itself per plan 04-05's patchMilestoneAwaitingApproval / etc.).
func approveLevel(ctx context.Context, c client.Client, ns, projectName string) error {
	var proj tidev1alpha3.Project
	if err := c.Get(ctx, types.NamespacedName{Namespace: ns, Name: projectName}, &proj); err != nil {
		if apierrors.IsNotFound(err) {
			return fmt.Errorf("tide: project %q not found in namespace %q", projectName, ns)
		}
		return fmt.Errorf("get project: %w", err)
	}

	// Option A (DEBT-03): discover the AwaitingApproval target FIRST in
	// dependency-order. Milestone → Phase → Plan → Task. The first matching
	// child is the one the operator is gating on.
	if obj, level, err := findAwaitingMilestone(ctx, c, ns, projectName); err != nil {
		return err
	} else if obj != nil {
		return approveLevelTarget(ctx, c, obj, level, projectName)
	}
	if obj, level, err := findAwaitingPhase(ctx, c, ns, projectName); err != nil {
		return err
	} else if obj != nil {
		return approveLevelTarget(ctx, c, obj, level, projectName)
	}
	if obj, level, err := findAwaitingPlan(ctx, c, ns, projectName); err != nil {
		return err
	} else if obj != nil {
		return approveLevelTarget(ctx, c, obj, level, projectName)
	}
	if obj, level, err := findAwaitingTask(ctx, c, ns, projectName); err != nil {
		return err
	} else if obj != nil {
		return approveLevelTarget(ctx, c, obj, level, projectName)
	}

	// No AwaitingApproval level found. Check for Failed levels as a UX hint:
	// if a Failed level exists, guide the operator to `tide resume --retry-failed`
	// rather than surfacing a confusing "no level awaiting approval" message.
	if obj, kind, err := findFailedLevel(ctx, c, ns, projectName); err != nil {
		return err
	} else if obj != nil {
		detail := buildFailureDetail(obj)
		return fmt.Errorf(
			"tide: level %q (%s) has failed%s; approval never retries failed work — "+
				"use 'tide resume %s --retry-failed' to recover",
			obj.GetName(), kind, detail, projectName,
		)
	}

	return fmt.Errorf("tide: no level awaiting approval on project %s", projectName)
}

// approveLevelTarget applies the D-07 targeted check to the discovered
// AwaitingApproval object and, if it passes, writes the approve annotation.
//
// D-07 (Option A, DEBT-03): approval is refused only if the TARGET itself
// has Status.Phase=="Failed" — not because of any unrelated sibling failure.
// Reuses buildFailureDetail for the actionable "retry-failed" error message
// (T-17-07: prevents approval from doubling as a spend-retry for the target).
func approveLevelTarget(ctx context.Context, c client.Client, obj client.Object, level, projectName string) error {
	// Belt-and-suspenders: refuse if the discovered AwaitingApproval target
	// is itself in a Failed phase (e.g., the level transitioned to Failed
	// after the list but before the approval check — or the controller sets
	// both AwaitingApproval status and a Failed condition simultaneously).
	var targetPhase string
	switch v := obj.(type) {
	case *tidev1alpha3.Milestone:
		targetPhase = v.Status.Phase
	case *tidev1alpha3.Phase:
		targetPhase = v.Status.Phase
	case *tidev1alpha3.Plan:
		targetPhase = v.Status.Phase
	case *tidev1alpha3.Task:
		targetPhase = v.Status.Phase
	}
	if targetPhase == "Failed" {
		detail := buildFailureDetail(obj)
		return fmt.Errorf(
			"tide: level %q (%s) has failed%s; approval never retries failed work — "+
				"use 'tide resume %s --retry-failed' to recover",
			obj.GetName(), level, detail, projectName,
		)
	}
	return patchApproveLevel(ctx, c, obj, level)
}

// findFailedLevel scans all four level kinds (Milestone → Phase → Plan →
// Task) for the first item with Status.Phase=="Failed" belonging to the
// project. Returns the object and its kind string ("milestone", "phase",
// "plan", "task"), or (nil, "", nil) when none is found.
func findFailedLevel(ctx context.Context, c client.Client, ns, projectName string) (client.Object, string, error) {
	var msList tidev1alpha3.MilestoneList
	if err := c.List(ctx, &msList, client.InNamespace(ns)); err != nil {
		return nil, "", fmt.Errorf("list milestones: %w", err)
	}
	for i := range msList.Items {
		m := &msList.Items[i]
		if m.Labels["tideproject.k8s/project"] != projectName {
			continue
		}
		if m.Status.Phase == "Failed" {
			return m, "milestone", nil
		}
	}

	var phList tidev1alpha3.PhaseList
	if err := c.List(ctx, &phList, client.InNamespace(ns)); err != nil {
		return nil, "", fmt.Errorf("list phases: %w", err)
	}
	for i := range phList.Items {
		p := &phList.Items[i]
		if p.Labels["tideproject.k8s/project"] != projectName {
			continue
		}
		if p.Status.Phase == "Failed" {
			return p, "phase", nil
		}
	}

	var plList tidev1alpha3.PlanList
	if err := c.List(ctx, &plList, client.InNamespace(ns)); err != nil {
		return nil, "", fmt.Errorf("list plans: %w", err)
	}
	for i := range plList.Items {
		p := &plList.Items[i]
		if p.Labels["tideproject.k8s/project"] != projectName {
			continue
		}
		if p.Status.Phase == "Failed" {
			return p, "plan", nil
		}
	}

	var tkList tidev1alpha3.TaskList
	if err := c.List(ctx, &tkList, client.InNamespace(ns)); err != nil {
		return nil, "", fmt.Errorf("list tasks: %w", err)
	}
	for i := range tkList.Items {
		t := &tkList.Items[i]
		if t.Labels["tideproject.k8s/project"] != projectName {
			continue
		}
		if t.Status.Phase == "Failed" {
			return t, "task", nil
		}
	}

	return nil, "", nil
}

// buildFailureDetail extracts a human-readable failure detail fragment from
// the level's conditions. Looks for ConditionWaveOrLevelPaused first; falls
// back to the first condition in the slice. Returns " (reason: <R>: <M>)"
// when a condition with Reason and Message is found, or "" otherwise.
func buildFailureDetail(obj client.Object) string {
	var reason, message string
	switch v := obj.(type) {
	case *tidev1alpha3.Milestone:
		c := meta.FindStatusCondition(v.Status.Conditions, tidev1alpha3.ConditionWaveOrLevelPaused)
		if c == nil && len(v.Status.Conditions) > 0 {
			c = &v.Status.Conditions[0]
		}
		if c != nil {
			reason, message = c.Reason, c.Message
		}
	case *tidev1alpha3.Phase:
		c := meta.FindStatusCondition(v.Status.Conditions, tidev1alpha3.ConditionWaveOrLevelPaused)
		if c == nil && len(v.Status.Conditions) > 0 {
			c = &v.Status.Conditions[0]
		}
		if c != nil {
			reason, message = c.Reason, c.Message
		}
	case *tidev1alpha3.Plan:
		c := meta.FindStatusCondition(v.Status.Conditions, tidev1alpha3.ConditionWaveOrLevelPaused)
		if c == nil && len(v.Status.Conditions) > 0 {
			c = &v.Status.Conditions[0]
		}
		if c != nil {
			reason, message = c.Reason, c.Message
		}
	case *tidev1alpha3.Task:
		c := meta.FindStatusCondition(v.Status.Conditions, tidev1alpha3.ConditionWaveOrLevelPaused)
		if c == nil && len(v.Status.Conditions) > 0 {
			c = &v.Status.Conditions[0]
		}
		if c != nil {
			reason, message = c.Reason, c.Message
		}
	}
	if reason == "" && message == "" {
		return ""
	}
	return fmt.Sprintf(" (reason: %s: %s)", reason, message)
}

// findAwaitingMilestone lists Milestones in the namespace, filtered to the
// Project by the canonical tideproject.k8s/project label (per
// internal/controller/plan_controller.go vocabulary). Returns the first
// Milestone whose Status.Phase is "AwaitingApproval".
func findAwaitingMilestone(
	ctx context.Context, c client.Client, ns, projectName string,
) (client.Object, string, error) {
	var list tidev1alpha3.MilestoneList
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
	var list tidev1alpha3.PhaseList
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
	var list tidev1alpha3.PlanList
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
	var list tidev1alpha3.TaskList
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
