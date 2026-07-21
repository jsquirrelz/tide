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
	case tideprojectv1alpha3.LevelPhaseAwaitingApproval, tideprojectv1alpha3.LevelPhaseSucceeded, tideprojectv1alpha3.PhaseComplete:
		return true
	default:
		return false
	}
}

// taskFindingsStageable reports whether a Task's verifier findings are safe to
// stage onto the run branch (Finding 10 / 53-03; consumed by both this file's
// collectStageEnvelopes loop and plan 53-10's Task verdict-final push
// trigger, which MUST apply the identical eligibility check — this is the
// shared, named predicate the plan requires).
//
// Deliberately does NOT reuse plannerMaterialized: that predicate's phase
// vocabulary (AwaitingApproval/Succeeded/Complete keyed off PLANNER
// completion) doesn't fit a Task's verify-loop phase vocabulary, and reusing
// it would admit a pre-verify Succeeded Task (Task's "Running exit 0, no
// verification contract" path — OQ2) with no verdict at all.
//
// Verdict-final AND evaluation-recorded (supersedes RESEARCH Assumption A4's
// unguarded VerifyHalted arm): a Task qualifies only when BOTH hold —
//
//  1. Status.Phase is one of the three verdict-final phases: VerifyHalted
//     (the escalate-leg halt), Succeeded (an APPROVED verdict consumed via
//     markVerifiedSucceeded), or AwaitingApproval (the ESC-02 requireApproval
//     park — the operator needs the findings to decide `tide approve`,
//     mirroring plannerMaterialized already admitting AwaitingApproval for
//     the other four kinds). Verifying/Running (mid-loop) are excluded —
//     never stage mid-iteration.
//  2. Status.LoopStatus.LastEvaluation != nil. applyLoopStatus
//     (task_controller.go:2505-2530) sets LastEvaluation if-and-only-if
//     out.Verdict != nil — i.e. the verifier's own envelope was read AND
//     carried a parsed verdict. haltVerify calls applyLoopStatus
//     unconditionally on every halt leg (VerifierEnvelopeUnreadable,
//     VerifierVerdictMissing, VerifyBlocked, ...), so a VerifyHalted Task
//     whose verifier crashed before producing a verdict (an unreadable
//     envelope) still has a nil LastEvaluation and is excluded by guard (2)
//     alone. This guard is REQUIRED on every arm, VerifyHalted included: a
//     PVC envelope dir can exist (the executor's own out.json created it)
//     with no verdict ever recorded, and tide-push hard-fails the ENTIRE
//     cumulative push if any staged entry's dir lacks findings.json
//     (cmd/tide-push/main.go:1242-1252) — poisoning every other level's
//     artifacts in that push.
//
// uid->srcDir verified: entries key by the Task's own UID (t.UID below), and
// tide-push resolves srcDir as filepath.Join(cfg.Workspace, "envelopes",
// es.UID) (cmd/tide-push/main.go's stageEnvelopeArtifacts). The verifier Job
// dispatched for a Task (dispatchVerifier, task_controller.go:2164) is built
// with podjob.BuildOptions.Task = the same Task, and BuildJobSpec's
// JobKindVerifier branch derives its envelope dir from opts.Task.UID
// (internal/dispatch/podjob/jobspec.go) — so the Task's own UID is exactly
// the directory the verifier writes its envelope into. The mapping is
// correct as-is; no ENTRY-construction fix was needed.
//
// LastEvaluation-as-presence-proxy — SURFACED CONTRADICTION (never loosen the
// predicate to compensate): as of this plan, nothing in the verifier's write
// path (cmd/tide-langgraph-verifier/verifier/__main__.py:211-225) ever writes
// a findings.json file — it writes only out.json (write_envelope_out) and the
// termination-log TerminationStub (write_termination_stub). A recorded
// LastEvaluation therefore does NOT yet imply findings.json landed on the PVC;
// it implies only that out.Verdict was non-nil when applyLoopStatus ran. This
// guard is still the TIGHTEST predicate expressible from Task.Status alone
// (the controller never mounts the PVC and cannot stat findings.json
// directly) — the fix is a verifier-side findings.json writer, out of this
// plan's declared file scope (files_modified: artifact_push.go/_test.go,
// dashboard artifacts.go/_test.go only). Until that writer lands, a
// tide-push carrying a task-kind entry will hard-fail on the missing
// findings.json regardless of how this predicate is shaped — flagged for
// whichever plan wires the verifier-side writer.
func taskFindingsStageable(t *tideprojectv1alpha3.Task) bool {
	if t.Status.LoopStatus.LastEvaluation == nil {
		return false
	}
	switch t.Status.Phase {
	case tideprojectv1alpha3.LevelPhaseVerifyHalted, tideprojectv1alpha3.LevelPhaseSucceeded, tideprojectv1alpha3.LevelPhaseAwaitingApproval:
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
// Project's Milestone/Phase/Plan/Task children are namespace-scoped (one
// Project per namespace), listed with client.InNamespace. Only
// planner-materialized levels (see plannerMaterialized) are included so the
// resulting push never stages an envelope that lacks a *.md; Tasks use the
// distinct taskFindingsStageable predicate (verdict-final + evaluation-
// recorded, Finding 10 / 53-03) so a task-kind entry never stages without a
// parsed verdict.
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

	var taskList tideprojectv1alpha3.TaskList
	if err := c.List(ctx, &taskList, inNS); err != nil {
		return nil, fmt.Errorf("list tasks: %w", err)
	}
	for i := range taskList.Items {
		t := &taskList.Items[i]
		if taskFindingsStageable(t) {
			entries = append(entries, entry{"task", t.Name, string(t.UID)})
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
		out = append(out, stageEntry(e.kind, e.name, e.uid))
	}
	return out, nil
}

// stageEntry formats a single <uid>:<kind>/<name> staging-map entry (37-06
// layout contract). Shared by collectStageEnvelopes' List-derived entries and
// triggerArtifactPush's ensure-entry union (plan 53-10) so the two paths
// never diverge on entry shape.
func stageEntry(kind, name, uid string) string {
	return fmt.Sprintf("%s:%s/%s", uid, kind, name)
}

// ensureTaskEntries unions each ensure Task's own <uid>:task/<name> staging
// entry into envelopes, deduped against what's already present (plan 53-10).
// A Task's verdict-final status patch and the informer-cache-backed List
// collectStageEnvelopes just ran are not ordered with respect to each other —
// the just-patched Task that triggered THIS push may not yet be visible to
// that List. Without this union, a stale snapshot could push a cumulative map
// missing the exact Task the caller is trying to surface, and — because the
// push Job is single-flight — the frozen VerifyHalted project would never
// retry until some LATER level's boundary push happened to re-collect it.
// Callers that pass no ensure Tasks (the four existing planner-tier call
// sites) get back envelopes unchanged.
func ensureTaskEntries(envelopes []string, ensure []*tideprojectv1alpha3.Task) []string {
	if len(ensure) == 0 {
		return envelopes
	}
	present := make(map[string]struct{}, len(envelopes))
	for _, e := range envelopes {
		present[e] = struct{}{}
	}
	for _, t := range ensure {
		if t == nil {
			continue
		}
		entry := stageEntry("task", t.Name, string(t.UID))
		if _, ok := present[entry]; ok {
			continue
		}
		envelopes = append(envelopes, entry)
		present[entry] = struct{}{}
	}
	return envelopes
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
//
// pvcName follows the triggerBoundaryPush contract: the caller's configured
// shared-PVC name (r.sharedPVCName()), empty falling back to
// defaultSharedPVCName.
//
// ensure (plan 53-10) is an optional, variadic set of Tasks whose own
// <uid>:task/<name> entry MUST ride this push even if collectStageEnvelopes'
// own List has not yet observed their status patch (see ensureTaskEntries).
// The four existing planner-tier callers pass nothing and are behaviorally
// unchanged; the Task verdict-final trigger (task_controller.go) passes the
// just-patched Task.
func triggerArtifactPush(
	ctx context.Context,
	c client.Client,
	scheme *runtime.Scheme,
	project *tideprojectv1alpha3.Project,
	level string,
	tidePushImage string,
	pvcName string,
	helmDefaults ProviderDefaults,
	ensure ...*tideprojectv1alpha3.Task,
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
	envelopes = ensureTaskEntries(envelopes, ensure)
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
	if pvcName == "" {
		pvcName = defaultSharedPVCName
	}
	pushJob := buildPushJob(project, pvcName, pushOpts, scheme)
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
