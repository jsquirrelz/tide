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

// level_verify.go — the ONE shared, level-parameterized level-verify state
// machine for Phase, Milestone, and Project (Phase 52 D-07). Mirrors
// depgraph.go's factor-not-fork precedent: rather than three near-identical
// forks of the Task loop's dispatch/consume machinery
// (task_controller.go:2131-2991), a single dispatch/consume/terminal-routing
// unit is parameterized by levelVerifyTarget and called identically by
// PhaseReconciler, MilestoneReconciler, and ProjectReconciler (52-10 wires
// the three call sites; this file stays free of any per-controller fork).
//
// D-07: for a level with a resolved verification contract, the verifier
// dispatches at the exact moment the level would otherwise stamp Succeeded
// (the level_status.go patch{Level}Succeeded seam) — semantic verification
// of the observable outcome, running alongside the existing mechanical
// merge-ancestry completeness gate. maxIterations:0 is encoded entirely by
// ResolveLoopPolicy's clamp (dispatch_helpers.go) — this file has no repair
// branch and no level-specific if-statements; every non-APPROVED terminal
// routes through the ONE exhaustVerifyLoop (level_status.go) D-08 branch
// point, same as Task's migrated haltVerify.
package controller

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"time"

	"go.opentelemetry.io/otel/trace"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	tideprojectv1alpha3 "github.com/jsquirrelz/tide/api/v1alpha3"
	"github.com/jsquirrelz/tide/internal/budget"
	"github.com/jsquirrelz/tide/internal/credproxy"
	"github.com/jsquirrelz/tide/internal/dispatch/podjob"
	"github.com/jsquirrelz/tide/internal/owner"
	"github.com/jsquirrelz/tide/internal/subagent/common"
	pkgdispatch "github.com/jsquirrelz/tide/pkg/dispatch"
)

// levelVerifyTarget carries the level-specific accessors maybeRunLevelVerify
// needs — one instance per call, built by each of the three reconcilers from
// their own CRD's Status fields (mirrors the plan-check target shape 52-07
// builds for Plan). No per-controller fork lives in THIS file; every field
// here is a pointer/accessor into the caller's own object.
type levelVerifyTarget struct {
	// Obj is the level's own CRD object (Phase, Milestone, or Project).
	Obj client.Object

	// Conditions is a pointer to Obj.Status.Conditions — patchLevelStatus's
	// shared leaf mutates through this pointer.
	Conditions *[]metav1.Condition

	// PhasePtr is a pointer to Obj.Status.Phase — the same fieldPtr shape
	// patchLevelStatus already accepts.
	PhasePtr *string

	// LoopStatus is a pointer to Obj.Status.LoopStatus (Phase 52 D-07's
	// embedding site on PhaseStatus/MilestoneStatus/ProjectStatus). Never
	// nil in production (the embedding is a value, not a pointer, on every
	// caller's Status struct) — nil-checked defensively for unit tests.
	LoopStatus *tideprojectv1alpha3.LoopStatus

	// Level is the dispatch level string: "phase"|"milestone"|"project".
	Level string

	// Goal is the level-appropriate observable-outcome goal text rendered
	// into phase_verifier.tmpl/milestone_verifier.tmpl/project_verifier.tmpl's
	// {{.LevelGoal}} placeholder (D-09) — the caller supplies the phase
	// brief / MILESTONE.md / outcome-prompt text, this file never resolves
	// artifact content itself.
	Goal string

	// ParentSpanID is the SAME parent span ID the level's own AGENT span was
	// given (mirrors emitEvaluatorSpanForVerifier's Task-level resolution) —
	// makes the EVALUATOR span a sibling of the AGENT span, not its child.
	// Zero value when no parent span is available (e.g. Project, the trace
	// root) — synthesizeEvaluatorSpan degrades gracefully per its own doc.
	ParentSpanID trace.SpanID
}

// levelVerifierRenderData is the render-data contract for
// phase_verifier.tmpl/milestone_verifier.tmpl/project_verifier.tmpl (Phase
// 52 D-09): embeds EnvelopeIn for the templates' existing
// {{.TaskUID}}/{{.Verify...}} references, plus the pinned {{.LevelGoal}}
// field. Mirrors 52-03's test-only levelVerifierFixture — this is the real
// dispatch-time type the plan's Task 1 promised ("Plans 52-07/52-08/52-09
// supply these exact structs").
type levelVerifierRenderData struct {
	pkgdispatch.EnvelopeIn
	LevelGoal string
}

// levelVerifyDecisionKind is the outcome of levelVerifyDecision's pure guard
// evaluation (Task 2 — extracted so the guard is directly unit-testable
// without a fake client.Client/context, per the plan's explicit "structure
// the pure guard as a small testable function" instruction).
type levelVerifyDecisionKind int

const (
	// levelVerifyInactive: no resolved-and-Locked contract for this level —
	// the off-switch; caller proceeds to patch{Level}Succeeded unchanged.
	levelVerifyInactive levelVerifyDecisionKind = iota
	// levelVerifyConverged: LoopStatus.ExitReason is already set — the
	// post-approval convergence guard (T-52-25); caller proceeds to
	// patch{Level}Succeeded unchanged.
	levelVerifyConverged
	// levelVerifyNeedsDispatch: active, not concluded, not yet
	// Phase==Verifying — patch the transition and dispatch a fresh verifier
	// Job.
	levelVerifyNeedsDispatch
	// levelVerifyAlreadyVerifying: active, not concluded, already
	// Phase==Verifying — Get the deterministic Job and branch on its state.
	levelVerifyAlreadyVerifying
)

// levelVerifyDecision is the pure guard maybeRunLevelVerify evaluates before
// any I/O: given the resolved VerificationSpec (ResolveVerificationSpec is
// itself pure — no ctx/client params), the level's current LoopStatus, and
// its current phase string, decides which of the four mutually-exclusive
// branches applies. nil-safe on loopStatus.
func levelVerifyDecision(spec tideprojectv1alpha3.VerificationSpec, loopStatus *tideprojectv1alpha3.LoopStatus, phase string) levelVerifyDecisionKind {
	if spec.GateCommand == "" || spec.Phase != verificationPhaseLocked {
		return levelVerifyInactive
	}
	if loopStatus != nil && loopStatus.ExitReason != "" {
		return levelVerifyConverged
	}
	if phase != tideprojectv1alpha3.LevelPhaseVerifying {
		return levelVerifyNeedsDispatch
	}
	return levelVerifyAlreadyVerifying
}

// maybeRunLevelVerify is the single entry point the pre-Succeeded seams call
// (phase_controller.go/milestone_controller.go/project_controller.go, wired
// in 52-10) immediately before they would otherwise call
// patch{Level}Succeeded. Returns handled=false in exactly two cases — no
// resolved contract (the stage is off) and a loop that already concluded
// (the ExitReason convergence guard, T-52-25) — both of which mean the
// caller should proceed to patch{Level}Succeeded exactly as it does today.
// Any other outcome returns handled=true: this file is driving the level's
// state this reconcile and the caller must NOT also stamp Succeeded.
func maybeRunLevelVerify(
	ctx context.Context,
	c client.Client,
	scheme *runtime.Scheme,
	deps PlannerReconcilerDeps,
	project *tideprojectv1alpha3.Project,
	target levelVerifyTarget,
) (handled bool, res ctrl.Result, err error) {
	spec := ResolveVerificationSpec(project, nil, nil, target.Level)
	phase := ""
	if target.PhasePtr != nil {
		phase = *target.PhasePtr
	}

	switch levelVerifyDecision(spec, target.LoopStatus, phase) {
	case levelVerifyInactive, levelVerifyConverged:
		return false, ctrl.Result{}, nil

	case levelVerifyNeedsDispatch:
		if _, pErr := patchLevelStatus(ctx, c, target.Obj, target.Conditions, target.PhasePtr,
			tideprojectv1alpha3.LevelPhaseVerifying, false, ctrl.Result{},
			metav1.Condition{
				Type:    tideprojectv1alpha3.ConditionReconciling,
				Status:  metav1.ConditionTrue,
				Reason:  "LevelVerifierDispatched",
				Message: "All children succeeded; dispatching an independent verifier against the locked verification contract",
			},
		); pErr != nil {
			return true, ctrl.Result{}, pErr
		}
		result, dErr := dispatchLevelVerifier(ctx, c, scheme, deps, project, target, spec)
		return true, result, dErr

	default: // levelVerifyAlreadyVerifying
		jobName := podjob.VerifierJobName(target.Level, string(target.Obj.GetUID()), 1)
		var job batchv1.Job
		if gErr := c.Get(ctx, client.ObjectKey{Namespace: target.Obj.GetNamespace(), Name: jobName}, &job); gErr != nil {
			if !apierrors.IsNotFound(gErr) {
				return true, ctrl.Result{}, gErr
			}
			// NotFound is a legitimate "cap-hit deferred the dispatch" state
			// (mirrors checkVerifyingState's identical NotFound-retry
			// posture) — retry dispatchLevelVerifier; VerifierJobName is
			// deterministic and AlreadyExists on Create is treated as
			// success (SUB-03).
			result, dErr := dispatchLevelVerifier(ctx, c, scheme, deps, project, target, spec)
			return true, result, dErr
		}
		if isJobTerminal(&job) {
			return handleLevelVerifierCompletion(ctx, c, deps, project, target, &job)
		}
		// Still running: nothing to do this reconcile.
		return true, ctrl.Result{}, nil
	}
}

// dispatchLevelVerifier creates the independent, read-only verifier Job for
// a level's resolved verification contract. Mirrors dispatchVerifier's exact
// shape (task_controller.go:2152-2277): cap-before-reserve ordering (ESC-04/
// D-10/Pitfall 6 — no slot/reservation leak on cap-hit), a deterministic
// VerifierJobName so a retry (checkVerifyingState-equivalent above) is
// idempotent, and AlreadyExists-as-success on Create (SUB-03). attempt is
// FIXED at 1 — maxIterations:0 levels never mint a second quality attempt;
// Job backoff handles pod-level retries, not this dispatch layer.
func dispatchLevelVerifier(
	ctx context.Context,
	c client.Client,
	scheme *runtime.Scheme,
	deps PlannerReconcilerDeps,
	project *tideprojectv1alpha3.Project,
	target levelVerifyTarget,
	spec tideprojectv1alpha3.VerificationSpec,
) (ctrl.Result, error) {
	logger := logf.FromContext(ctx)
	const attempt = 1
	uid := string(target.Obj.GetUID())
	ns := target.Obj.GetNamespace()
	verifierJobName := podjob.VerifierJobName(target.Level, uid, attempt)

	// LO-01 (mirrors dispatchVerifier): no verifier image configured — leave
	// the level benignly parked in Verifying rather than build a Job with an
	// empty image ref.
	if deps.VerifierImage == "" {
		logger.Info("verifier image not configured (TIDE_VERIFIER_IMAGE empty); leaving level parked in Verifying without dispatching a verifier Job",
			"level", target.Level, "name", target.Obj.GetName())
		return ctrl.Result{}, nil
	}

	// ESC-04/D-10: cap-before-acquire (Pitfall 6), self-excluding the
	// deterministic Job name so a re-reconcile of an already-dispatched
	// verifier never counts itself.
	inFlight, cErr := verifierInFlightCount(ctx, c, ns, project.Name, verifierJobName)
	if cErr != nil {
		return ctrl.Result{}, fmt.Errorf("level verifier in-flight count: %w", cErr)
	}
	if inFlight >= defaultVerifierConcurrencyCap {
		logger.V(1).Info("level verifier dispatch deferred: concurrency cap reached",
			"inFlight", inFlight, "cap", defaultVerifierConcurrencyCap, "level", target.Level, "name", target.Obj.GetName())
		return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
	}

	reserved := false
	if deps.ReserveEstimateCents > 0 {
		deps.Reservations.Reserve(uid, deps.ReserveEstimateCents)
		reserved = true
	}
	releaseOnError := func() {
		if reserved {
			deps.Reservations.Release(uid)
			reserved = false
		}
	}

	verifierCaps := podjob.DefaultCaps(nil, podjob.JobKindVerifier)
	wallClock := verifierCaps.WallClockSeconds
	token, sErr := credproxy.Sign(deps.SigningKey, uid,
		time.Duration(wallClock+podjob.DefaultWallClockGraceSeconds)*time.Second)
	if sErr != nil {
		releaseOnError()
		return ctrl.Result{}, fmt.Errorf("mint level verifier signed token: %w", sErr)
	}

	// D-07 point 4 / Pitfall 2: the run branch (project.Status.Git.BranchName)
	// populates BOTH EnvelopeIn.Branch and BuildOptions.WorktreeBranch — the
	// worktree-checkout init container (52-05) checks out this same tip, and
	// the verifier's rendered prompt/envelope carries the identical value the
	// executor's own EnvelopeIn.Branch already carries at Task level.
	runBranch := project.Status.Git.BranchName

	_, envInJSON, bErr := buildLevelVerifierEnvelopeIn(deps, project, target, spec, token, runBranch)
	if bErr != nil {
		releaseOnError()
		return ctrl.Result{}, bErr
	}

	var secretUID string
	if project.Spec.ProviderSecretRef != "" {
		var secret corev1.Secret
		if gErr := c.Get(ctx, client.ObjectKey{Namespace: ns, Name: project.Spec.ProviderSecretRef}, &secret); gErr == nil {
			secretUID = string(secret.UID)
		}
	}
	agentName, agentEmail := resolveAgentIdentity(project, deps.HelmProviderDefaults)
	job := podjob.BuildJobSpec(podjob.BuildOptions{
		Kind:           podjob.JobKindVerifier,
		ParentObj:      target.Obj,
		Level:          target.Level,
		Project:        project,
		Attempt:        attempt,
		SignedToken:    token,
		EnvelopeInJSON: envInJSON,
		SubagentImage:  deps.VerifierImage,
		AgentName:      agentName,
		AgentEmail:     agentEmail,
		CredproxyImage: deps.CredproxyImage,
		SecretUID:      secretUID,
		PVCName:        defaultSharedPVCName,
		ProjectUID:     string(project.UID),
		ReadOnly:       true,
		GateCommand:    spec.GateCommand,
		// Phase 52 D-07/RESEARCH "The missing worktree": Phase/Milestone/
		// Project never dispatch an executor at their own UID, so (unlike
		// Task) there is no inherited worktree to read — the SECOND init
		// container (52-05's buildWorktreeCheckoutContainer) provisions a
		// fresh, read-only, detached checkout at the run branch's tip.
		WorktreeCheckoutImage: deps.TidePushImage,
		WorktreeBranch:        runBranch,
		EstimatedCostCents:    deps.ReserveEstimateCents,
	})
	// BuildJobSpec's JobKindVerifier case stamps role=verifier + level +
	// task-uid but not the project label (mirrors dispatchVerifier's own
	// post-build label stamp, task_controller.go:2244-2257) —
	// verifierInFlightCount's project-scoped List needs it.
	if job.Labels == nil {
		job.Labels = map[string]string{}
	}
	job.Labels[owner.LabelProject] = project.Name
	if job.Spec.Template.Labels == nil {
		job.Spec.Template.Labels = map[string]string{}
	}
	job.Spec.Template.Labels[owner.LabelProject] = project.Name

	if oErr := owner.EnsureOwnerRef(job, target.Obj, scheme); oErr != nil {
		releaseOnError()
		return ctrl.Result{}, fmt.Errorf("ensure owner ref on level verifier job: %w", oErr)
	}
	if createErr := c.Create(ctx, job); createErr != nil {
		if !apierrors.IsAlreadyExists(createErr) {
			releaseOnError()
			return ctrl.Result{}, fmt.Errorf("create level verifier job: %w", createErr)
		}
		// AlreadyExists: idempotent success (watch-lag race, or a retry
		// after a prior cap-hit deferred dispatch — Pitfall F / SUB-03).
		logger.Info("level verifier job already exists; treating as successful dispatch", "job", job.Name)
	}

	logger.Info("dispatched level verifier", "level", target.Level, "name", target.Obj.GetName(),
		"job", job.Name, "gateCommand", spec.GateCommand)
	return ctrl.Result{}, nil
}

// buildLevelVerifierEnvelopeIn constructs and marshals the EnvelopeIn for a
// level-verify dispatch. Mirrors buildVerifierEnvelopeIn (task_controller.go:
// 2295-2362): Role="verifier", Provider.Vendor="langgraph" (the verifier is
// a logically independent process, TASK-04), Verify.Commands is the resolved
// ordered union [GateCommand] ++ spec.Commands. Unlike Task's variant, Branch
// is populated (D-07 point 4 — the Task verifier never needed it; the
// level-verify worktree-checkout container does) and the prompt renders
// against levelVerifierRenderData's {{.LevelGoal}}, not the bare envelope.
func buildLevelVerifierEnvelopeIn(
	deps PlannerReconcilerDeps,
	project *tideprojectv1alpha3.Project,
	target levelVerifyTarget,
	spec tideprojectv1alpha3.VerificationSpec,
	token, runBranch string,
) (pkgdispatch.EnvelopeIn, []byte, error) {
	var commands []string
	if spec.GateCommand != "" {
		commands = append(commands, spec.GateCommand)
	}
	commands = append(commands, spec.Commands...)

	uid := string(target.Obj.GetUID())
	envIn := pkgdispatch.EnvelopeIn{
		APIVersion: pkgdispatch.APIVersionV1Alpha1,
		Kind:       pkgdispatch.KindTaskEnvelopeIn,
		TaskUID:    uid,
		Role:       "verifier",
		Level:      target.Level,
		Branch:     runBranch,
		// D-01: derived from the target UID + fixed attempt tuple, never
		// minted — same shape as the Task verifier's own stamp.
		LoopRunID: uid,
		AttemptID: fmt.Sprintf("%s-%d", uid, 1),
		Provider: pkgdispatch.ProviderSpec{
			Vendor: "langgraph",
			Model:  ResolveProvider(project, target.Level, deps.HelmProviderDefaults).Model,
		},
		ProxyEndpoint: credproxyEndpoint,
		SignedToken:   token,
		Verify: &pkgdispatch.VerifyContext{
			GateCommand:       spec.GateCommand,
			Commands:          commands,
			RequiredArtifacts: spec.RequiredArtifacts,
			EvaluatorRef:      spec.Evaluator,
		},
	}

	tmpl, tErr := common.LoadPromptTemplate("verifier", target.Level)
	if tErr != nil {
		return pkgdispatch.EnvelopeIn{}, nil, fmt.Errorf("load level verifier prompt template: %w", tErr)
	}
	var promptBuf bytes.Buffer
	if xErr := tmpl.Execute(&promptBuf, levelVerifierRenderData{EnvelopeIn: envIn, LevelGoal: target.Goal}); xErr != nil {
		return pkgdispatch.EnvelopeIn{}, nil, fmt.Errorf("render level verifier prompt template: %w", xErr)
	}
	envIn.Prompt = promptBuf.String()

	data, mErr := json.Marshal(envIn)
	if mErr != nil {
		return pkgdispatch.EnvelopeIn{}, nil, fmt.Errorf("marshal level verifier envelope: %w", mErr)
	}
	return envIn, data, nil
}

// handleLevelVerifierCompletion consumes a terminal level-verify Job's
// EnvelopeOut.Verdict. Fail-closed by construction (mirrors
// handleVerifierCompletion, task_controller.go:2945-2991): an unreadable
// envelope or a nil Verdict routes to exhaustLevelVerify (never Succeeded).
// ClassifyVerdict drives the decision: APPROVED (no deterministic
// gate-command dominance, D-06) -> handled=false so the caller falls through
// to patch{Level}Succeeded THIS reconcile; every other outcome (REPAIRABLE,
// BLOCKED, an APPROVED verdict a red gate-command Finding dominates) ->
// exhaustLevelVerify — D-07: no separate quality-iteration branch exists at
// these levels, the resolver's MaxIterations=0 clamp is what makes
// REPAIRABLE behave like an immediate escalation here.
func handleLevelVerifierCompletion(
	ctx context.Context,
	c client.Client,
	deps PlannerReconcilerDeps,
	project *tideprojectv1alpha3.Project,
	target levelVerifyTarget,
	verifierJob *batchv1.Job,
) (handled bool, res ctrl.Result, err error) {
	uid := string(target.Obj.GetUID())
	out, rErr := readVerifierEnvelope(ctx, deps.EnvReader, string(project.UID), uid)
	if rErr != nil {
		synthOut := synthesizeNoLevelVerifyEnvelopeOut(target.Obj, verifierJob)
		emitLevelEvaluatorSpan(ctx, target, project, deps.HelmProviderDefaults, verifierJob, synthOut, false)
		result, hErr := exhaustLevelVerify(ctx, c, deps, project, target, synthOut, rErr.Error(), "VerifierEnvelopeUnreadable", tideprojectv1alpha3.ExitEscalated)
		return true, result, hErr
	}
	if out.Verdict == nil {
		emitLevelEvaluatorSpan(ctx, target, project, deps.HelmProviderDefaults, verifierJob, out, true)
		result, hErr := exhaustLevelVerify(ctx, c, deps, project, target, out, "verifier envelope carried no verdict (fail-closed BLOCKED)", "VerifierVerdictMissing", tideprojectv1alpha3.ExitEscalated)
		return true, result, hErr
	}

	// OBS-03/D-11: the EVALUATOR sibling span, emitted before the terminal
	// status patches below (span-loss-averse ordering, mirrors
	// emitEvaluatorSpanForVerifier's own call-site ordering).
	emitLevelEvaluatorSpan(ctx, target, project, deps.HelmProviderDefaults, verifierJob, out, true)

	// D-04 (mirrors handleVerifierCompletion): re-derive the classification
	// through the canonical fail-closed ClassifyVerdict function rather than
	// trusting out.Verdict.Verdict's raw decoded string directly.
	raw, mErr := json.Marshal(out.Verdict)
	if mErr != nil {
		result, hErr := exhaustLevelVerify(ctx, c, deps, project, target, out, mErr.Error(), "VerifierVerdictMarshalFailed", tideprojectv1alpha3.ExitEscalated)
		return true, result, hErr
	}

	switch pkgdispatch.ClassifyVerdict(raw) {
	case pkgdispatch.VerdictApproved:
		if hasDeterministicFailure(out.Verdict) {
			// D-06 defence-in-depth: a red gate-command Finding dominates
			// even a top-level APPROVED verdict, controller-side.
			result, hErr := exhaustLevelVerify(ctx, c, deps, project, target, out, out.Verdict.Summary, "VerifyBlocked", tideprojectv1alpha3.ExitEscalated)
			return true, result, hErr
		}
		base, ok := target.Obj.DeepCopyObject().(client.Object)
		if !ok {
			return true, ctrl.Result{}, nil
		}
		applyLevelLoopStatus(target.LoopStatus, out, tideprojectv1alpha3.ExitApproved)
		if pErr := c.Status().Patch(ctx, target.Obj, client.MergeFrom(base)); pErr != nil {
			return true, ctrl.Result{}, pErr
		}
		settleLevelVerifierSpend(ctx, c, deps, project, uid, out)
		// handled=false: the caller's own pre-Succeeded seam proceeds to
		// patch{Level}Succeeded in THIS same reconcile — the ExitReason just
		// persisted above makes any LATER re-entry a no-op via the
		// convergence guard in maybeRunLevelVerify.
		return false, ctrl.Result{}, nil
	case pkgdispatch.VerdictRepairable:
		result, hErr := exhaustLevelVerify(ctx, c, deps, project, target, out, out.Verdict.Summary, "VerifyRepairable", tideprojectv1alpha3.ExitEscalated)
		return true, result, hErr
	default: // pkgdispatch.VerdictBlocked, and ClassifyVerdict's own fail-closed default.
		result, hErr := exhaustLevelVerify(ctx, c, deps, project, target, out, out.Verdict.Summary, "VerifyBlocked", tideprojectv1alpha3.ExitEscalated)
		return true, result, hErr
	}
}

// exhaustLevelVerify handles every terminal, non-APPROVED exit of a
// level-verify loop — an unreadable envelope, a missing Verdict, a
// classified BLOCKED/REPAIRABLE verdict, or an APPROVED verdict a
// deterministic gate-command Finding dominates. Mirrors haltVerify
// (task_controller.go:2605-2659) exactly: the terminal patch delegates to
// exhaustVerifyLoop (level_status.go) — Phase 52 D-08's ONE branch point —
// which differentiates requireApproval (park at AwaitingApproval) from
// escalate (VerifyHalted + project-wide ConditionVerifyHalt). A SECOND,
// focused patch then stamps the caller-specific conditionReason + LoopStatus
// — exhaustVerifyLoop's own patch base is captured via DeepCopyObject()
// BEFORE this function's mutations, so those mutations must land in a
// separate patch or they would be silently dropped (see exhaustVerifyLoop's
// own doc comment).
func exhaustLevelVerify(
	ctx context.Context,
	c client.Client,
	deps PlannerReconcilerDeps,
	project *tideprojectv1alpha3.Project,
	target levelVerifyTarget,
	out pkgdispatch.EnvelopeOut,
	message, conditionReason string,
	exitReason tideprojectv1alpha3.ExitReason,
) (ctrl.Result, error) {
	var completedAt time.Time
	if !out.CompletedAt.IsZero() {
		completedAt = out.CompletedAt
	}

	policy := ResolveLoopPolicy(project, nil, nil, target.Level)
	result, err := exhaustVerifyLoop(ctx, c, project, target.Obj, target.Conditions, target.PhasePtr, target.Level, policy, completedAt, message)
	if err != nil {
		return ctrl.Result{}, err
	}

	base, ok := target.Obj.DeepCopyObject().(client.Object)
	if !ok {
		return result, nil
	}
	// Skipped on the requireApproval leg, mirroring haltVerify: exhaustVerifyLoop
	// already parked the level with its own WaveOrLevelPaused/ReasonVerifyExhausted
	// condition, and stamping ConditionFailed=True on a merely-parked (not
	// failed) level would contradict that state.
	if policy.EscalationPolicy != tideprojectv1alpha3.EscalationRequireApproval {
		meta.SetStatusCondition(target.Conditions, metav1.Condition{
			Type:               tideprojectv1alpha3.ConditionFailed,
			Status:             metav1.ConditionTrue,
			Reason:             conditionReason,
			Message:            message,
			LastTransitionTime: metav1.Now(),
		})
	}
	applyLevelLoopStatus(target.LoopStatus, out, exitReason)
	if pErr := c.Status().Patch(ctx, target.Obj, client.MergeFrom(base)); pErr != nil {
		return ctrl.Result{}, pErr
	}

	settleLevelVerifierSpend(ctx, c, deps, project, string(target.Obj.GetUID()), out)
	return result, nil
}

// applyLevelLoopStatus stamps LoopStatus.Iteration/ParentRunID/ExitReason/
// LastEvaluation from a terminal level-verify EnvelopeOut. Mirrors
// applyLoopStatus (task_controller.go:2493-2518) with ONE difference:
// Iteration is fixed at 1 (never task.Status.Attempt) — D-07 levels dispatch
// exactly one verify attempt ever, never a repair-driven counter. Nil-safe:
// a nil LoopStatus (defensive; never nil in production, see levelVerifyTarget
// doc) is a no-op.
func applyLevelLoopStatus(ls *tideprojectv1alpha3.LoopStatus, out pkgdispatch.EnvelopeOut, exitReason tideprojectv1alpha3.ExitReason) {
	if ls == nil {
		return
	}
	ls.Iteration = 1
	if out.LoopRunID != "" {
		ls.ParentRunID = out.LoopRunID
	}
	ls.ExitReason = exitReason
	if out.Verdict == nil {
		return
	}
	var highSeverity int32
	for _, f := range out.Verdict.Findings {
		if f.Severity == gateCommandFindingSeverity {
			highSeverity++
		}
	}
	summary := tideprojectv1alpha3.EvaluationSummary{
		Decision:          string(out.Verdict.Verdict),
		FindingsCount:     int32(len(out.Verdict.Findings)),
		HighSeverityCount: highSeverity,
	}
	if !out.CompletedAt.IsZero() {
		ct := metav1.NewTime(out.CompletedAt)
		summary.CompletedAt = &ct
	}
	ls.LastEvaluation = &summary
}

// settleLevelVerifierSpend rolls the level verifier's real token spend into
// Project.Status.budget and settles the BudgetCents reservation
// dispatchLevelVerifier made at dispatch time. Mirrors settleVerifierSpend
// (task_controller.go:2553-2562) — called exactly once per level-verify
// completion regardless of verdict outcome, since the verifier ran and spent
// real tokens either way.
func settleLevelVerifierSpend(ctx context.Context, c client.Client, deps PlannerReconcilerDeps, project *tideprojectv1alpha3.Project, uid string, out pkgdispatch.EnvelopeOut) {
	logger := logf.FromContext(ctx)
	if err := budget.RollUpUsage(ctx, c, project, out.Usage); err != nil {
		logger.Error(err, "failed to roll up level verifier budget usage", "uid", uid)
	}
	if fbErr := setPricingFallbackIfNeeded(ctx, c, project, out.Usage.PricingFallbackModel); fbErr != nil {
		logger.Error(fbErr, "setPricingFallbackIfNeeded failed (non-fatal)", "uid", uid)
	}
	deps.Reservations.Settle(uid)
}

// emitLevelEvaluatorSpan resolves the evaluator version off RunEvidence (when
// present) and emits the OBS-03/D-11 EVALUATOR sibling span via the existing,
// already level-parameterized synthesizeEvaluatorSpan (span_emission.go:326)
// — mirrors emitEvaluatorSpanForVerifier's shape exactly, generalized off
// target.Level/target.ParentSpanID instead of Task's hardcoded "task" +
// PlanRef lookup.
func emitLevelEvaluatorSpan(
	ctx context.Context,
	target levelVerifyTarget,
	project *tideprojectv1alpha3.Project,
	helmDefaults ProviderDefaults,
	verifierJob *batchv1.Job,
	out pkgdispatch.EnvelopeOut,
	envReadOK bool,
) {
	evaluatorVersion := ""
	if out.RunEvidence != nil && len(out.RunEvidence.EvaluatorVersions) > 0 {
		evaluatorVersion = out.RunEvidence.EvaluatorVersions[0]
	}
	synthesizeEvaluatorSpan(ctx, target.Level, target.Obj.GetName(), project, helmDefaults, verifierJob, out, envReadOK, evaluatorVersion, target.ParentSpanID)
}

// synthesizeNoLevelVerifyEnvelopeOut mirrors synthesizeNoEnvelopeOut
// (task_controller.go:1313-1336), generalized off a client.Object's UID
// instead of *Task, with the level-verify attempt fixed at 1.
func synthesizeNoLevelVerifyEnvelopeOut(obj client.Object, completedJob *batchv1.Job) pkgdispatch.EnvelopeOut {
	uid := string(obj.GetUID())
	out := pkgdispatch.EnvelopeOut{
		APIVersion:  pkgdispatch.APIVersionV1Alpha1,
		Kind:        pkgdispatch.KindTaskEnvelopeOut,
		TaskUID:     uid,
		LoopRunID:   uid,
		AttemptID:   fmt.Sprintf("%s-%d", uid, 1),
		CompletedAt: time.Now().UTC(),
	}
	if completedJob == nil {
		return out
	}
	for _, cond := range completedJob.Status.Conditions {
		if cond.Type == batchv1.JobFailed && cond.Status == corev1.ConditionTrue {
			if cond.Reason == "DeadlineExceeded" {
				out.TerminalReason = pkgdispatch.TerminalReasonCapExceeded
				out.Reason = "wall-clock cap exceeded (ActiveDeadlineSeconds): pod was SIGKILLed before it could write out.json"
			}
			break
		}
	}
	return out
}
