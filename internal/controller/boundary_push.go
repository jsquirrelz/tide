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
	"errors"
	"fmt"

	batchv1 "k8s.io/api/batch/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	tideprojectv1alpha3 "github.com/jsquirrelz/tide/api/v1alpha3"
	tidemetrics "github.com/jsquirrelz/tide/internal/metrics"
	"github.com/jsquirrelz/tide/internal/owner"
)

// errGitWriterBusy is returned by triggerBoundaryPush when the D-02
// single-flight gate finds another git-writer Job (wave-integration or
// boundary-push) already in flight for this Project. Call sites translate
// this into a short requeue (5s) rather than propagating it as a reconcile
// error — a busy gate is normal serialization, not a failure (Pitfall 7).
var errGitWriterBusy = errors.New("git-writer busy: another run-branch writer Job is in flight")

// triggerBoundaryPush is the shared implementation invoked by every up-
// stack reconciler's per-receiver maybeTriggerBoundaryPush. It dispatches
// a `tide-push-<project-uid>` Job carrying the level's D-B2 commit
// message AFTER the gate-policy seam has approved the transition.
//
// Plan 04-06 Task 2 (W-2). Co-located per D-W2: the same seam that
// consults gate policy (D-G2) is the seam that triggers push.
//
// Contract:
//
//  1. Skip silently if Project lacks git config (RepoURL empty) — mirrors
//     ProjectReconciler.reconcilePhase3Lifecycle:385 guard.
//  2. Compute the deterministic name `tide-push-<project.UID>` (D-B5
//     serialization). Multiple concurrent boundary detections (e.g., a
//     Phase and a Milestone in the same tick) collapse to one push Job per
//     Project; K8s API AlreadyExists is the synchronization point.
//  3. If the Job exists, return nil — push is already mid-flight or has
//     completed.
//  4. Otherwise build buildPushJob with the level's D-B2 commit message
//     and Create it; AlreadyExists is tolerated.
//
// Owner ref: the Job is owned by the Project (not the level CRD). This
// matches the Project-boundary push (project_controller.go:393) and keeps
// cascade-cleanup semantics consistent — deleting the Project deletes
// any in-flight push Job regardless of which level triggered it.
//
// The Job's name carries Project.UID, so even a buildPushJob constructed
// at Phase boundary lives in the Project's logical scope.
//
// Phase 34 D-02/D-03/D-07: the cumulative Succeeded-branch set is computed
// HERE (via a live List), not passed in by the caller — the plan-level
// caller fires pre-Tasks and every other level never collected branches at
// all, so the only correct place to compute the set is inside the shared
// trigger. D-02: before Create, a live List checks for any OTHER in-flight
// git-writer Job (wave-integration or boundary-push); if one is active,
// return errGitWriterBusy so the caller requeues instead of racing a second
// writer onto the run branch.
//
// pvcName is the caller's configured shared-PVC name (r.sharedPVCName()) so
// the push Job mounts the same claim the dispatch Jobs write to; empty falls
// back to defaultSharedPVCName.
func triggerBoundaryPush(
	ctx context.Context,
	c client.Client,
	scheme *runtime.Scheme,
	parent client.Object,
	project *tideprojectv1alpha3.Project,
	level string,
	tidePushImage string,
	pvcName string,
	helmDefaults ProviderDefaults,
) error {
	logger := logf.FromContext(ctx)

	if project == nil {
		// No project resolution → no push.
		return nil
	}
	if project.Spec.Git == nil || project.Spec.Git.RepoURL == "" {
		// No git config → no push.
		return nil
	}
	if tidePushImage == "" {
		// No push image configured on the reconciler (test fixtures or
		// dev clusters without the Helm chart): cannot dispatch a Job
		// with an empty container image — K8s API rejects it as Invalid.
		// Skip rather than fail the reconcile loop.
		//
		// CR-02 fix: promoted to Info (was V(1)) so silent disablement is
		// operator-visible at default log verbosity. In production this
		// signals a chart/env misconfiguration (TIDE_PUSH_IMAGE env unset)
		// and warrants attention even though it's not fatal.
		logger.Info("skipping boundary push: TidePushImage not configured", "level", level, "project", project.Name)
		return nil
	}

	// D-09: a merge-conflict park is a human-recovery halt — a level trigger
	// must not recreate the doomed push after the failed Job's TTL GC. The
	// miss-reason variant stays dispatchable (its cap arm bounds retries and
	// a later success auto-clears the condition); `tide resume` clears the
	// conflict park after a replan.
	if cond := meta.FindStatusCondition(project.Status.Conditions, tideprojectv1alpha3.ConditionIntegrationIncomplete); cond != nil &&
		cond.Status == metav1.ConditionTrue && cond.Reason == tideprojectv1alpha3.ReasonMergeConflict {
		logger.Info("skipping boundary push: parked on a merge conflict awaiting `tide resume`",
			"level", level, "project", project.Name)
		return nil
	}

	pushJobName := fmt.Sprintf("tide-push-%s", project.UID)

	// Check existence first — idempotent dispatch (D-B5).
	var existing batchv1.Job
	getErr := c.Get(ctx, types.NamespacedName{Name: pushJobName, Namespace: project.Namespace}, &existing)
	if getErr == nil {
		return nil // already created — boundary push already in flight
	}
	if !apierrors.IsNotFound(getErr) {
		return fmt.Errorf("get push job %s: %w", pushJobName, getErr)
	}

	// D-02 single-flight gate: exclude pushJobName itself (it does not exist
	// yet per the Get above, so this is belt-and-braces against a
	// concurrent create) and check for any OTHER in-flight git-writer Job.
	inFlight, gwErr := gitWriterInFlightCount(ctx, c, project.Namespace, project.Name, pushJobName)
	if gwErr != nil {
		return fmt.Errorf("check git-writer in-flight count: %w", gwErr)
	}
	if inFlight > 0 {
		return errGitWriterBusy
	}

	msg, err := buildCommitMessage(level, parent.GetName())
	if err != nil {
		return fmt.Errorf("build commit message for %s boundary: %w", level, err)
	}

	if pvcName == "" {
		pvcName = defaultSharedPVCName
	}

	// D-03/D-07: cumulative Succeeded-branch set, recomputed via a live List
	// every time — whichever level's trigger wins the deterministic
	// tide-push-<project.UID> create race integrates identically.
	branches, bErr := succeededTaskBranches(ctx, c, project.Namespace, project.Name)
	if bErr != nil {
		return fmt.Errorf("compute cumulative succeeded-task branches: %w", bErr)
	}

	// SIGN-01 / D-03: resolve the agent identity once per dispatch and stamp it
	// into the push Job env (covers integrate merge commits + boundary commit).
	agentName, agentEmail := resolveAgentIdentity(project, helmDefaults)

	// R-05 (37-06): every push — boundary or artifact-triggered — carries the full
	// cumulative planner-artifact map so a single writer class stages all completed
	// levels. Best-effort: a boundary push MUST still land even if the map can't be
	// computed, so a collection error degrades to an un-staged (but committed) push.
	stageEnvs, seErr := collectStageEnvelopes(ctx, c, project)
	if seErr != nil {
		logger.Error(seErr, "collectStageEnvelopes for boundary push failed (non-fatal); pushing without artifact staging", "level", level, "project", project.Name)
		stageEnvs = nil
	}

	pushOpts := PushOptions{
		TidePushImage:         tidePushImage,
		Branch:                project.Status.Git.BranchName,
		LastPushedSHA:         project.Status.Git.LastPushedSHA,
		CommitMessage:         msg,
		LeaksConfigMap:        project.Spec.Git.LeaksConfigRef,
		IntegrateTaskBranches: branches,
		StageEnvelopes:        stageEnvs,
		AgentName:             agentName,
		AgentEmail:            agentEmail,
	}
	pushJob := buildPushJob(project, pvcName, pushOpts, scheme)
	if cErr := c.Create(ctx, pushJob); cErr != nil {
		if !apierrors.IsAlreadyExists(cErr) {
			return fmt.Errorf("create push job: %w", cErr)
		}
		// AlreadyExists: idempotent success — the D-B5 serialization race.
	}

	logger.Info("triggered boundary push", "level", level, "parent", parent.GetName(), "project", project.Name, "job", pushJobName, "message", msg, "integrateTaskBranches", len(branches))
	tidemetrics.PushJobsTotal.WithLabelValues(project.Name, "dispatched").Inc()
	return nil
}

// triggerWaveIntegrationJob dispatches a per-wave integration Job for the
// given plan, project, and wave index. The Job runs tide-push --mode=push with
// --integrate-task-branches set (no --artifact-paths) to merge wave k's task
// branches into the run branch before wave k+1 executors are dispatched (D-02).
//
// Job name is deterministic: "tide-push-wave-<plan.UID>-<waveIndex>" so
// idempotency relies on AlreadyExists at the K8s API level.
//
// Returns apierrors.IsAlreadyExists-tolerant: AlreadyExists is treated as
// success (idempotent dispatch).
//
// pvcName follows the triggerBoundaryPush contract: the caller's configured
// shared-PVC name, empty falling back to defaultSharedPVCName.
func triggerWaveIntegrationJob(
	ctx context.Context,
	c client.Client,
	scheme *runtime.Scheme,
	plan *tideprojectv1alpha3.Plan,
	project *tideprojectv1alpha3.Project,
	waveIndex int,
	branches []string,
	tidePushImage string,
	pvcName string,
	helmDefaults ProviderDefaults,
) error {
	if project == nil || project.Spec.Git == nil || project.Spec.Git.RepoURL == "" {
		return nil
	}
	if tidePushImage == "" {
		return nil
	}

	if pvcName == "" {
		pvcName = defaultSharedPVCName
	}
	jobName := fmt.Sprintf("tide-push-wave-%s-%d", plan.UID, waveIndex)
	commitMsg := fmt.Sprintf("tide: integrate wave %d", waveIndex)

	// SIGN-01 / D-03: resolve identity once and stamp into the wave-integration
	// push Job env so the in-pod merge commits carry the configured author.
	agentName, agentEmail := resolveAgentIdentity(project, helmDefaults)

	pushOpts := PushOptions{
		TidePushImage:         tidePushImage,
		Branch:                project.Status.Git.BranchName,
		LastPushedSHA:         project.Status.Git.LastPushedSHA,
		CommitMessage:         commitMsg,
		IntegrateTaskBranches: branches,
		// IntegrationOnly: merge+verify into the LOCAL run branch, no
		// commit/push — the remote push belongs to boundary pushes, which
		// carry the same cumulative branch set with IntegrationOnly false.
		IntegrationOnly: true,
		LeaksConfigMap:  project.Spec.Git.LeaksConfigRef,
		AgentName:       agentName,
		AgentEmail:      agentEmail,
	}

	job := buildPushJob(project, pvcName, pushOpts, scheme)
	// Override the deterministic name: wave integration jobs are named
	// tide-push-wave-<plan.UID>-<waveIndex>, distinct from the plan-boundary
	// tide-push-<project.UID> job.
	job.Name = jobName
	// Owner ref on Plan (not Project) so cleanup happens on Plan deletion.
	_ = owner.EnsureOwnerRef(job, plan, scheme)

	if cErr := c.Create(ctx, job); cErr != nil {
		if !apierrors.IsAlreadyExists(cErr) {
			return fmt.Errorf("create wave integration job %s: %w", jobName, cErr)
		}
		// AlreadyExists — idempotent success.
	}
	return nil
}

// maybeTriggerBoundaryPush is the MilestoneReconciler entry point. Invoked
// from handleJobCompletion AFTER the gate-policy seam passes (so a paused
// or rejected level NEVER pushes) and BEFORE patchMilestoneSucceeded
// (so the operator-visible Status.Phase=Succeeded transition happens
// after the push trigger).
func (r *MilestoneReconciler) maybeTriggerBoundaryPush(ctx context.Context, parent client.Object, project *tideprojectv1alpha3.Project) error {
	return triggerBoundaryPush(ctx, r.Client, r.Scheme, parent, project, "milestone", r.Deps.TidePushImage, r.sharedPVCName(), r.Deps.HelmProviderDefaults)
}

// maybeTriggerBoundaryPush is the PhaseReconciler entry point.
func (r *PhaseReconciler) maybeTriggerBoundaryPush(ctx context.Context, parent client.Object, project *tideprojectv1alpha3.Project) error {
	return triggerBoundaryPush(ctx, r.Client, r.Scheme, parent, project, "phase", r.Deps.TidePushImage, r.sharedPVCName(), r.Deps.HelmProviderDefaults)
}

// maybeTriggerBoundaryPush is the PlanReconciler entry point. Plan boundary
// commit messages carry the only D-B2 shape with `+ executed` suffix
// because Tasks have already executed by the time the Plan boundary fires.
//
// Phase 34 D-03: the taskItems parameter is gone — the cumulative
// Succeeded-branch set is now computed inside the shared triggerBoundaryPush
// via a live List, not collected by the caller. The only live call site
// (plan_controller.go, at planner-Job completion) fires BEFORE Tasks exist
// anyway (CR-03 note), so the old per-caller collection here was always
// dead code in practice.
func (r *PlanReconciler) maybeTriggerBoundaryPush(ctx context.Context, parent client.Object, project *tideprojectv1alpha3.Project) error {
	return triggerBoundaryPush(ctx, r.Client, r.Scheme, parent, project, "plan", r.Deps.TidePushImage, r.sharedPVCName(), r.Deps.HelmProviderDefaults)
}
