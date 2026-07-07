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
	"encoding/json"
	"sort"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	tideprojectv1alpha2 "github.com/jsquirrelz/tide/api/v1alpha2"
	pkggit "github.com/jsquirrelz/tide/pkg/git"
)

// Phase 34 (D-02/D-03/D-07) shared git-writer helpers. Every Job that merges
// into or pushes the run branch — wave-integration Jobs and boundary-push
// Jobs at all four levels — is labeled with these two keys so a live
// client.List can find in-flight writers before dispatching another
// (gitWriterInFlightCount) and so the cumulative Succeeded-branch set can be
// recomputed from the cluster on every trigger (succeededTaskBranches).

const (
	// gitWriterRoleLabelKey / gitWriterRoleLabelValue mark every Job that
	// merges into or pushes the run branch (wave-integration + boundary
	// push, all four levels). Mirrors the `tideproject.k8s/role: planner`
	// label consumed by plannerInFlightCount (dispatch_helpers.go).
	gitWriterRoleLabelKey   = "tideproject.k8s/role"
	gitWriterRoleLabelValue = "git-writer"

	// gitWriterProjectLabelKey scopes the git-writer List to a single
	// Project — matches the existing tideproject.k8s/project label
	// convention used project-wide (fixtures_test.go, cmd/tide/resume.go).
	gitWriterProjectLabelKey = "tideproject.k8s/project"
)

// succeededTaskBranches returns the sorted, deterministic list of worktree
// branch names (tide/wt-<uid>) for every Task CR in namespace ns labeled
// tideproject.k8s/project=projectName whose Status.Phase is "Succeeded".
// Empty (nil) slice when no Task has Succeeded yet.
//
// This is the D-03/D-07 cumulative Succeeded-branch set: computed via a live
// List at dispatch time inside the shared trigger, never cached in .status
// (the completeness verdict is always recomputed from git downstream —
// STATE.md binding constraint). sort.Strings gives deterministic Job args
// across retries so a re-dispatch after a transient failure is byte-for-byte
// the same --integrate-task-branches value.
func succeededTaskBranches(ctx context.Context, c client.Client, ns, projectName string) ([]string, error) {
	var tasks tideprojectv1alpha2.TaskList
	if err := c.List(ctx, &tasks,
		client.InNamespace(ns),
		client.MatchingLabels{gitWriterProjectLabelKey: projectName},
	); err != nil {
		return nil, err
	}
	var branches []string
	for i := range tasks.Items {
		if tasks.Items[i].Status.Phase == "Succeeded" {
			branches = append(branches, pkggit.TaskBranchName(string(tasks.Items[i].UID)))
		}
	}
	sort.Strings(branches)
	return branches, nil
}

// gitWriterInFlightCount returns the count of non-terminal git-writer Jobs
// (role=git-writer, project=projectName) in namespace ns, EXCLUDING the Job
// named excludeJobName. The exclusion parameter is Pitfall 7's self-exclusion
// guard: a state machine that dispatches/observes a deterministic Job name
// must not count that same Job as "another" writer in flight, or the D-02
// gate deadlocks on itself (the boundary-push retry loop blocking on the
// very push Job it manages).
//
// Mirrors plannerInFlightCount's shape (dispatch_helpers.go): skip Jobs with
// a DeletionTimestamp (on their way out, must not hold a slot), count only
// non-terminal Jobs via isJobTerminal.
func gitWriterInFlightCount(ctx context.Context, c client.Client, ns, projectName, excludeJobName string) (int, error) {
	var jobs batchv1.JobList
	if err := c.List(ctx, &jobs,
		client.InNamespace(ns),
		client.MatchingLabels{
			gitWriterRoleLabelKey:    gitWriterRoleLabelValue,
			gitWriterProjectLabelKey: projectName,
		},
	); err != nil {
		return 0, err
	}
	n := 0
	for i := range jobs.Items {
		if jobs.Items[i].Name == excludeJobName {
			continue
		}
		if jobs.Items[i].DeletionTimestamp != nil {
			continue
		}
		if !isJobTerminal(&jobs.Items[i]) {
			n++
		}
	}
	return n, nil
}

// readJobPushEnvelope returns the parsed pushResultEnvelope from the FIRST
// pod labeled job-name=jobName that has a terminated container carrying a
// non-empty termination message; (zero, false) otherwise.
//
// Termination-log only — NEVER the shared PVC envelope path (Pitfall 2: the
// PVC copy is keyed by project UID and collides across every push-mode Job
// for the same Project — a failing wave Job would overwrite the boundary
// push's envelope and vice versa). The termination log is per-pod and
// API-server-mediated, so it is collision-free by construction.
//
// Package-level (not a ProjectReconciler method) so PlanReconciler can use it
// to classify wave-integration Job failures (D-09/D-10) without an import
// cycle or method-promotion trick.
func readJobPushEnvelope(ctx context.Context, c client.Client, namespace, jobName string) (pushResultEnvelope, bool) {
	var pods corev1.PodList
	if err := c.List(ctx, &pods,
		client.InNamespace(namespace),
		client.MatchingLabels{"job-name": jobName},
	); err != nil {
		return pushResultEnvelope{}, false
	}
	if len(pods.Items) == 0 {
		return pushResultEnvelope{}, false
	}
	pod := &pods.Items[0]
	if len(pod.Status.ContainerStatuses) == 0 {
		return pushResultEnvelope{}, false
	}
	term := pod.Status.ContainerStatuses[0].State.Terminated
	if term == nil || term.Message == "" {
		return pushResultEnvelope{}, false
	}
	var env pushResultEnvelope
	if err := json.Unmarshal([]byte(term.Message), &env); err != nil {
		return pushResultEnvelope{}, false
	}
	return env, true
}
