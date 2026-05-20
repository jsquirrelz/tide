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

	batchv1 "k8s.io/api/batch/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	tideprojectv1alpha1 "github.com/jsquirrelz/tide/api/v1alpha1"
	tidemetrics "github.com/jsquirrelz/tide/internal/metrics"
)

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
func triggerBoundaryPush(
	ctx context.Context,
	c client.Client,
	scheme *runtime.Scheme,
	parent client.Object,
	project *tideprojectv1alpha1.Project,
	level string,
	tidePushImage string,
	sharedPVCName string,
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

	msg, err := buildCommitMessage(level, parent.GetName())
	if err != nil {
		return fmt.Errorf("build commit message for %s boundary: %w", level, err)
	}

	pvcName := sharedPVCName
	if pvcName == "" {
		pvcName = defaultSharedPVCName
	}

	pushOpts := PushOptions{
		TidePushImage:  tidePushImage,
		Branch:         project.Status.Git.BranchName,
		LastPushedSHA:  project.Status.Git.LastPushedSHA,
		CommitMessage:  msg,
		LeaksConfigMap: project.Spec.Git.LeaksConfigRef,
	}
	pushJob := buildPushJob(project, pvcName, pushOpts, scheme)
	if cErr := c.Create(ctx, pushJob); cErr != nil {
		if !apierrors.IsAlreadyExists(cErr) {
			return fmt.Errorf("create push job: %w", cErr)
		}
		// AlreadyExists: idempotent success — the D-B5 serialization race.
	}

	logger.Info("triggered boundary push", "level", level, "parent", parent.GetName(), "project", project.Name, "job", pushJobName, "message", msg)
	tidemetrics.PushJobsTotal.WithLabelValues(project.Name, "dispatched").Inc()
	return nil
}

// maybeTriggerBoundaryPush is the MilestoneReconciler entry point. Invoked
// from handleJobCompletion AFTER the gate-policy seam passes (so a paused
// or rejected level NEVER pushes) and BEFORE patchMilestoneSucceeded
// (so the operator-visible Status.Phase=Succeeded transition happens
// after the push trigger).
func (r *MilestoneReconciler) maybeTriggerBoundaryPush(ctx context.Context, parent client.Object, project *tideprojectv1alpha1.Project) error {
	return triggerBoundaryPush(ctx, r.Client, r.Scheme, parent, project, "milestone", r.TidePushImage, "")
}

// maybeTriggerBoundaryPush is the PhaseReconciler entry point.
func (r *PhaseReconciler) maybeTriggerBoundaryPush(ctx context.Context, parent client.Object, project *tideprojectv1alpha1.Project) error {
	return triggerBoundaryPush(ctx, r.Client, r.Scheme, parent, project, "phase", r.TidePushImage, "")
}

// maybeTriggerBoundaryPush is the PlanReconciler entry point. Plan boundary
// commit messages carry the only D-B2 shape with `+ executed` suffix
// because Tasks have already executed by the time the Plan boundary
// fires.
func (r *PlanReconciler) maybeTriggerBoundaryPush(ctx context.Context, parent client.Object, project *tideprojectv1alpha1.Project) error {
	return triggerBoundaryPush(ctx, r.Client, r.Scheme, parent, project, "plan", r.TidePushImage, "")
}
