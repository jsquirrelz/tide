/*
Copyright 2026 TIDE Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

package controller

import (
	"context"
	"fmt"
	"sort"

	batchv1 "k8s.io/api/batch/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	tideprojectv1alpha3 "github.com/jsquirrelz/tide/api/v1alpha3"
	tidemetrics "github.com/jsquirrelz/tide/internal/metrics"
)

// plannerMaterialized reports whether a level's planner has completed and its
// planning *.md envelope is GUARANTEED present on the shared PVC.
//
// Conservative by design (37-06). tide-push fails the ENTIRE cumulative push
// loud when any staged envelope lacks a *.md (37-02 D-03: "missing dir / no
// *.md -> reason artifact-stage-failed, nonzero exit, nothing pushed"). So
// over-including a still-planning level would poison EVERY other level's
// artifacts in that push. Under-inclusion, by contrast, self-heals — the next
// push carries the level once its status settles.
//
// The landed level enum reuses "Running" for BOTH planner-executing AND
// children-dispatching (see milestone/phase/plan controllers, which set
// Status.Phase="Running" at planner dispatch and again after approve). "Running"
// therefore cannot prove *.md presence and is deliberately excluded. Only the
// strictly-post-planner-completion states qualify:
//
//   - milestone / phase / plan: "AwaitingApproval" (parked at gate, planner done)
//     or "Succeeded".
//   - project: "Complete" (the project has no approve gate — D-02 auto-proceed —
//     so it is additionally admitted by the child-Milestone signal in
//     collectStageEnvelopes for early staging).
//
// NOTE (divergence from 37-06-PLAN): the plan's action text assumed a distinct
// "Planning-class" phase to exclude; the landed enum has no such phase and reuses
// "Running". The CONTRACT the plan cares about (cumulative map of planner-completed
// levels, before approve gates, single writer class) is what this predicate holds.
func plannerMaterialized(phase string) bool {
	switch phase {
	case "AwaitingApproval", "Succeeded", tideprojectv1alpha3.PhaseComplete:
		return true
	default:
		return false
	}
}

// collectStageEnvelopes builds the cumulative, deterministically-ordered
// <uid>:<kind>/<name> staging map of every planner-completed level in the
// Project's namespace (37-06 / DASH-02). Each entry maps a level's PVC-side
// envelope dir (keyed by UID) to its human-readable run-branch destination
// prefix <kind>/<name>, matching the layout contract plan 37-02 stages into
// .tide/planning/<kind>/<name>/.
//
// Listing idiom mirrors assembleProjectDepGraph (project_controller.go): the
// Project's Milestone/Phase/Plan children are namespace-scoped (one Project per
// namespace), listed with client.InNamespace. Only planner-materialized levels
// (see plannerMaterialized) are included so the resulting push never stages an
// envelope that lacks a *.md.
//
// The Project itself is included when its own planner has completed — proven by
// any materialized child Milestone (the reporter creates Milestones from the
// project planner's out.json, so their existence implies MILESTONE.md is on the
// PVC) or a Complete Project.
//
// Ordering is by (kind, name) so byte-identical restages are no-ops through the
// 37-02 clean-tree skip.
func collectStageEnvelopes(ctx context.Context, c client.Client, project *tideprojectv1alpha3.Project) ([]string, error) {
	if project == nil {
		return nil, nil
	}

	type entry struct{ kind, name, uid string }
	var entries []entry

	inNS := client.InNamespace(project.Namespace)

	var msList tideprojectv1alpha3.MilestoneList
	if err := c.List(ctx, &msList, inNS); err != nil {
		return nil, fmt.Errorf("list milestones: %w", err)
	}
	for i := range msList.Items {
		m := &msList.Items[i]
		if plannerMaterialized(m.Status.Phase) {
			entries = append(entries, entry{"milestone", m.Name, string(m.UID)})
		}
	}

	var phList tideprojectv1alpha3.PhaseList
	if err := c.List(ctx, &phList, inNS); err != nil {
		return nil, fmt.Errorf("list phases: %w", err)
	}
	for i := range phList.Items {
		p := &phList.Items[i]
		if plannerMaterialized(p.Status.Phase) {
			entries = append(entries, entry{"phase", p.Name, string(p.UID)})
		}
	}

	var planList tideprojectv1alpha3.PlanList
	if err := c.List(ctx, &planList, inNS); err != nil {
		return nil, fmt.Errorf("list plans: %w", err)
	}
	for i := range planList.Items {
		p := &planList.Items[i]
		if plannerMaterialized(p.Status.Phase) {
			entries = append(entries, entry{"plan", p.Name, string(p.UID)})
		}
	}

	// Project itself: its MILESTONE.md envelope exists once the project planner
	// completed. Any materialized child Milestone proves that (reporter output), and
	// a Complete project always qualifies. The project has no approve gate (D-02),
	// so early inclusion is a pure fidelity win — never a correctness risk.
	if project.Status.Phase == tideprojectv1alpha3.PhaseComplete || len(msList.Items) > 0 {
		entries = append(entries, entry{"project", project.Name, string(project.UID)})
	}

	sort.Slice(entries, func(i, j int) bool {
		if entries[i].kind != entries[j].kind {
			return entries[i].kind < entries[j].kind
		}
		return entries[i].name < entries[j].name
	})

	out := make([]string, 0, len(entries))
	for _, e := range entries {
		out = append(out, fmt.Sprintf("%s:%s/%s", e.uid, e.kind, e.name))
	}
	return out, nil
}

// buildArtifactStageMessage returns the commit message for an artifact-stage
// push, identifying it as a planner-artifact stage and the triggering level.
// Distinct from the four fixed D-B2 boundary shapes (buildCommitMessage) so
// operators can tell artifact-stage commits from boundary commits in the
// run-branch log.
func buildArtifactStageMessage(level string) string {
	return fmt.Sprintf("tide: stage planning artifacts (%s)", level)
}

// triggerArtifactPush dispatches the deterministic tide-push Job carrying the
// cumulative planner-artifact staging map (37-06 / DASH-02).
//
// It mirrors triggerBoundaryPush's guard chain and single-flight semantics
// EXACTLY — same deterministic Job name `tide-push-<project.UID>`, so the Phase 34
// single-flight gate serializes boundary and artifact pushes into ONE writer
// class (R-05). No new push mechanism, no second force-with-lease anchor path.
//
// Contract:
//   - nil / git-less project → silent skip (nil).
//   - empty tidePushImage → Info-logged skip (nil) — cannot create a Job with an
//     empty image.
//   - empty Status.Git.BranchName → skip (Open Q4): no run branch to push onto
//     yet; the parked-arm requeue (Task 2) retries until EnsureRunBranch stamps it.
//   - empty cumulative map → skip: nothing planner-completed yet.
//   - deterministic push Job already exists → no-op nil (single-flight; the
//     cumulative map self-heals on the next push).
func triggerArtifactPush(
	ctx context.Context,
	c client.Client,
	scheme *runtime.Scheme,
	project *tideprojectv1alpha3.Project,
	level string,
	tidePushImage string,
	helmDefaults ProviderDefaults,
) error {
	logger := logf.FromContext(ctx)

	if project == nil {
		return nil
	}
	if project.Spec.Git == nil || project.Spec.Git.RepoURL == "" {
		return nil
	}
	if tidePushImage == "" {
		logger.Info("skipping artifact push: TidePushImage not configured", "level", level, "project", project.Name)
		return nil
	}
	if project.Status.Git.BranchName == "" {
		// Open Q4: no run branch yet → nothing to push onto. The parked-arm requeue
		// (Task 2) keeps retrying until EnsureRunBranch stamps Status.Git.BranchName.
		logger.Info("skipping artifact push: run branch not yet provisioned", "level", level, "project", project.Name)
		return nil
	}

	envelopes, err := collectStageEnvelopes(ctx, c, project)
	if err != nil {
		return fmt.Errorf("collect stage envelopes for %s artifact push: %w", level, err)
	}
	if len(envelopes) == 0 {
		// Nothing planner-completed yet — no artifacts to stage.
		return nil
	}

	pushJobName := fmt.Sprintf("tide-push-%s", project.UID)
	var existing batchv1.Job
	getErr := c.Get(ctx, types.NamespacedName{Name: pushJobName, Namespace: project.Namespace}, &existing)
	if getErr == nil {
		// Single-flight: a push is already in flight. The cumulative map self-heals
		// on the next push — nothing is lost.
		return nil
	}
	if !apierrors.IsNotFound(getErr) {
		return fmt.Errorf("get push job %s: %w", pushJobName, getErr)
	}

	agentName, agentEmail := resolveAgentIdentity(project, helmDefaults)
	pushOpts := PushOptions{
		TidePushImage:  tidePushImage,
		Branch:         project.Status.Git.BranchName,
		LastPushedSHA:  project.Status.Git.LastPushedSHA,
		CommitMessage:  buildArtifactStageMessage(level),
		LeaksConfigMap: project.Spec.Git.LeaksConfigRef,
		StageEnvelopes: envelopes,
		AgentName:      agentName,
		AgentEmail:     agentEmail,
	}
	pushJob := buildPushJob(project, defaultSharedPVCName, pushOpts, scheme)
	if cErr := c.Create(ctx, pushJob); cErr != nil {
		if !apierrors.IsAlreadyExists(cErr) {
			return fmt.Errorf("create artifact push job: %w", cErr)
		}
		// AlreadyExists: idempotent success (single-flight race).
	}

	logger.Info("triggered artifact push", "level", level, "project", project.Name, "job", pushJobName, "envelopes", len(envelopes))
	tidemetrics.PushJobsTotal.WithLabelValues(project.Name, "artifact-stage").Inc()
	return nil
}
