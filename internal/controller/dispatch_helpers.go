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

// Package controller — dispatch_helpers.go consolidates the three planner
// dispatch helpers that all three up-stack reconcilers (Milestone, Phase,
// Plan) share (Phase 3 D-A1 / D-A2 / D-A4). The helpers exist to keep the
// reconciler bodies from drifting in lockstep — each reconciler is ~80-150
// LOC of NEW code instead of ~230 LOC because the shared bits live here.
//
//   - ResolveProvider walks the Project.Spec.Subagent precedence chain
//     per D-C2: levels.{level}.{model,params} → Project default →
//     Helm-chart default.
//
//   - BuildPlannerEnvelope mirrors task_controller.go buildEnvelopeIn for
//     planner-level dispatch — sets Role="planner", Level=<level>,
//     populates Provider via ResolveProvider, and marshals to []byte.
//
//   - MaterializeChildCRDs server-side-creates child CRDs from
//     EnvelopeOut.ChildCRDs. Enforces the Kind allowlist
//     (T-308 mitigation): only {Milestone, Phase, Plan, Task, Wave} pass.
//     Each child gets a controller-style owner ref pointing at the parent.
//     AlreadyExists is treated as idempotent success (mirrors Phase 2
//     SUB-03 / Pitfall F).
package controller

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	tideprojectv1alpha3 "github.com/jsquirrelz/tide/api/v1alpha3"
	"github.com/jsquirrelz/tide/internal/budget"
	"github.com/jsquirrelz/tide/internal/dispatch"
	"github.com/jsquirrelz/tide/internal/dispatch/podjob"
	"github.com/jsquirrelz/tide/internal/reporter"
	pkgdispatch "github.com/jsquirrelz/tide/pkg/dispatch"
	pkggit "github.com/jsquirrelz/tide/pkg/git"
)

const (
	// credproxyEndpoint is the in-pod HTTPS address of the tide-credproxy
	// native sidecar (Plan 05). Every dispatch (planner and executor) passes
	// this to the subagent Job as the envelope's ProxyEndpoint so the
	// subagent routes provider calls through the sidecar for token
	// validation (D-C1).
	credproxyEndpoint = "https://127.0.0.1:8443"

	// defaultPlannerIterations is applied when the operator has not set
	// Caps.Iterations on a planner dispatch (the Caps type doesn't carry
	// per-Kind iteration defaults; only the wall-clock floor differs by Kind
	// via podjob.DefaultCaps).
	defaultPlannerIterations = 20
)

// spawnReporterIfNeeded idempotently spawns the tide-reporter reader Job for a
// planner-level Job completion (Option C / T-09-13: AlreadyExists on Create is
// success; a pre-existing reporter Job means this completion was already
// observed). Returns isFirstCompletion=true when this is the initial
// observation of the planner Job's terminal state: either the reporter Job was
// newly spawned, or no ReporterImage is configured at all (stub/test path).
// When ReporterImage is set but the parent Project is unresolved, no spawn
// happens and isFirstCompletion stays false (mirrors the prior inline blocks
// in milestone_controller.go / phase_controller.go).
//
// pvcName is the caller's configured shared-PVC name (r.sharedPVCName()) so
// the reporter mounts the same claim the planner Job wrote out.json to;
// BuildReporterJob falls back to defaultSharedPVCName when empty.
//
// skipMessageSpans (Phase 45/ADAPT-01) is the caller's fresh vendor
// capability lookup result (pkgdispatch.SelfInstruments on the level's
// resolved ProviderSpec.Vendor) — threaded straight into
// ReporterOptions.SkipMessageSpans (D-04).
func spawnReporterIfNeeded(
	ctx context.Context,
	c client.Client,
	scheme *runtime.Scheme,
	parent metav1.Object,
	project *tideprojectv1alpha3.Project,
	parentKind string,
	reporterImage string,
	pvcName string,
	traceParent string,
	otlpEndpoint string,
	skipMessageSpans bool,
) (bool, error) {
	logger := logf.FromContext(ctx)
	if reporterImage == "" {
		logger.V(1).Info("skipping reporter Job spawn: ReporterImage not configured",
			"parentKind", parentKind, "parent", parent.GetName())
		return true, nil
	}
	if project == nil {
		return false, nil
	}
	reporterJobName := fmt.Sprintf("tide-reporter-%s", parent.GetUID())
	var existing batchv1.Job
	if gErr := c.Get(ctx, types.NamespacedName{Name: reporterJobName, Namespace: parent.GetNamespace()}, &existing); gErr != nil {
		if !apierrors.IsNotFound(gErr) {
			return false, fmt.Errorf("get reporter job %s: %w", reporterJobName, gErr)
		}
		// Not found — spawn it (first completion observation).
		reporterJob := BuildReporterJob(parent, project, pvcName, string(parent.GetUID()), parentKind,
			ReporterOptions{
				ReporterImage:    reporterImage,
				TraceParent:      traceParent,
				OTLPEndpoint:     otlpEndpoint,
				SkipMessageSpans: skipMessageSpans,
			}, scheme)
		if cErr := c.Create(ctx, reporterJob); cErr != nil {
			if !apierrors.IsAlreadyExists(cErr) {
				return false, fmt.Errorf("create reporter job %s: %w", reporterJobName, cErr)
			}
		} else {
			logger.Info("spawned reporter Job", "job", reporterJobName,
				"parentKind", parentKind, "parent", parent.GetName())
		}
		return true, nil
	}
	logger.V(1).Info("reporter Job already exists; skipping spawn (T-09-13)", "job", reporterJobName)
	return false, nil
}

// ProviderDefaults carries the Helm-chart-supplied defaults used as the
// last fallback in ResolveProvider's precedence chain (D-C2).
//
// Image is the default subagent image ref (vendor selection). Models maps
// level name ("milestone"|"phase"|"plan"|"task") to the per-level default
// model identifier. Both are filled at Manager startup from Helm values.
type ProviderDefaults struct {
	// Image is the default subagent image ref. Empty string means
	// "no Helm default" — caller is responsible for surfacing this
	// at Job creation time (a missing image is a config error).
	Image string

	// Models maps level→model. Missing key means "no Helm default for
	// that level".
	Models map[string]string

	// AgentName is the chart-tier committer/author name (D-03 middle tier),
	// populated at Manager startup from the TIDE_AGENT_NAME env var. Empty
	// string means "no Helm default" — resolveAgentIdentity falls through to
	// the pkg/git compiled default. The manager must NOT collapse an unset
	// chart value into the compiled default; defaulting happens once, at
	// resolve time.
	AgentName string

	// AgentEmail is the chart-tier committer/author email (D-03 middle tier),
	// populated from TIDE_AGENT_EMAIL. Same "empty = no Helm default"
	// convention as AgentName; resolves independently of it.
	AgentEmail string
}

// PlannerReconcilerDeps carries the dispatch-related dependencies shared by
// the four planner-tier reconcilers (Milestone/Phase/Plan/Project). Mirrors
// TaskReconcilerDeps (task_controller.go:90-119) for the up-stack
// reconcilers — plan 41-06 consolidation.
//
// Fields are populated at Manager wiring time (cmd/manager/main.go) and never
// mutated thereafter — copying a small struct at construction is cheaper than
// indirection at every dispatch (RESEARCH.md §P3.2 §Known pitfalls).
//
// Pool fields (PlannerPool, ExecutorPool), WatchNamespace, Recorder, and
// SharedPVCName stay as direct fields on each reconciler because they're
// conceptually separate from "what to dispatch with" — they're concurrency
// limiters and per-reconciler config, not dispatch-tier deps.
//
// Project is included here, not just Milestone/Phase/Plan, per RESEARCH
// Pitfall 2: leaving it out would repeat exactly the "forgotten Dispatcher
// field" bug class (cascade-8) this carrier exists to prevent.
type PlannerReconcilerDeps struct {
	Dispatcher dispatch.Dispatcher

	// EnvReader reads the EnvelopeOut from the per-Project PVC after the
	// planner Job completes.
	EnvReader podjob.EnvelopeReader

	// SigningKey is the HMAC signing key used to mint per-dispatch tokens
	// for the credproxy sidecar.
	SigningKey []byte

	// CredproxyImage is the image ref for the tide-credproxy sidecar.
	CredproxyImage string

	// TidePushImage is the image ref for the tide-push container used by
	// the W-2 boundary push trigger.
	TidePushImage string

	// ReporterImage is the image ref for the tide-reporter reader Job. When
	// empty, spawning the reader Job is skipped.
	ReporterImage string

	// HelmProviderDefaults carry Helm-chart provider/model defaults.
	HelmProviderDefaults ProviderDefaults

	// PricingOverridesJSON is the validated D-02 override JSON forwarded
	// opaquely to planner Jobs as TIDE_PRICING_OVERRIDES_JSON.
	PricingOverridesJSON string

	// OTLPEndpoint is the manager's own OTEL_EXPORTER_OTLP_ENDPOINT, forwarded
	// into reporter Job env so the reporter's TracerProvider resolves the same
	// collector (Phase 44 TRACE-03/D-06); empty = tracing dark, reporter env
	// omitted.
	OTLPEndpoint string
}

// levelOverrideKey maps a dispatch level (the 5-valued identity string
// carried in EnvelopeIn.Level, the tideproject.k8s/level Job label, and the
// subagent template-selection switch) to the Subagent.Levels override slot it
// resolves against (D-02 semantic rename, folded todo
// 2026-07-03-project-level-subagent-override-slot.md). Each Levels.X key now
// means "level X is planned by this model" (the reading operators already
// had) rather than "the model the X-named CR's OWN dispatch uses":
//
//	project   (authors MILESTONE.md) -> Levels.Milestone
//	milestone (authors phase briefs) -> Levels.Phase
//	phase     (authors PLAN.md)      -> Levels.Plan
//	plan      (authors the task DAG) -> Levels.Plan (D-11 collapse: same slot
//	                                     as "phase" -- both are "planning the
//	                                     plan's content")
//	task      (task execution)       -> Levels.Task (unchanged -- was never
//	                                     off-by-one)
//
// This is an override-key remap only -- dispatch identity (the level string
// itself) is untouched; every call site keeps passing its existing literal.
// Any level not in this table (none exist in production) resolves to itself.
func levelOverrideKey(level string) string {
	switch level {
	case "project":
		return "milestone"
	case "milestone":
		return "phase"
	case "phase":
		return "plan"
	case "plan":
		return "plan"
	case "task":
		return "task"
	default:
		return level
	}
}

// ResolveProvider walks Project.Spec.Subagent precedence chain for the given
// dispatch level (D-C2), first mapping it to its Levels.<overrideKey> slot via
// levelOverrideKey (D-02). Returns a ProviderSpec with Vendor pinned to
// "anthropic" in v1.0 (per-vendor selection deferred -- CONTEXT.md
// "Deferred Ideas"). Model and Params resolve via:
//
//	project.Spec.Subagent.Levels.<overrideKey>.Model ->
//	project.Spec.Subagent.Model ->
//	helmDefaults.Models[<overrideKey>] ->
//	"" (caller surfaces missing-config error)
//
// Params merge: level Params copied first, then Project-level Params
// inserted only for keys NOT already set at the level -- i.e., level
// wins on key conflict.
func ResolveProvider(project *tideprojectv1alpha3.Project, level string, helmDefaults ProviderDefaults) pkgdispatch.ProviderSpec {
	key := levelOverrideKey(level)

	// Helper to read per-level overrides.
	var levelCfg *tideprojectv1alpha3.LevelConfig
	if project != nil {
		switch key {
		case "milestone":
			levelCfg = project.Spec.Subagent.Levels.Milestone
		case "phase":
			levelCfg = project.Spec.Subagent.Levels.Phase
		case "plan":
			levelCfg = project.Spec.Subagent.Levels.Plan
		case "task":
			levelCfg = project.Spec.Subagent.Levels.Task
		}
	}

	// Resolve Model.
	var model string
	switch {
	case levelCfg != nil && levelCfg.Model != "":
		model = levelCfg.Model
	case project != nil && project.Spec.Subagent.Model != "":
		model = project.Spec.Subagent.Model
	default:
		if helmDefaults.Models != nil {
			model = helmDefaults.Models[key]
		}
	}

	// Merge Params — level overrides Project defaults on key conflict.
	var params map[string]string
	if levelCfg != nil && len(levelCfg.Params) > 0 {
		params = make(map[string]string, len(levelCfg.Params))
		maps.Copy(params, levelCfg.Params)
	}
	// (Project-level Params are not currently exposed on SubagentConfig
	// — LevelConfig.Params is the per-level extension; if a future
	// schema bump adds a top-level Subagent.Params, merge here with
	// "level wins on conflict" semantics.)

	return pkgdispatch.ProviderSpec{
		Vendor: "anthropic",
		Model:  model,
		Params: params,
	}
}

// BuildPlannerEnvelope constructs and marshals an EnvelopeIn for a
// planner-level dispatch. Mirrors task_controller.go:buildEnvelopeIn
// (Phase 2) but at the planner level: sets Role="planner",
// Level=<level>, populates Provider via ResolveProvider, and reuses the
// Caps / ProxyEndpoint / SignedToken parameters supplied by the caller.
//
// parent is the up-stack CRD whose UID stamps EnvelopeIn.TaskUID (the
// field is named TaskUID for backward-compat with Phase 2's envelope
// schema, but it carries the parent's UID at the planner level — the
// semantic is "the dispatch this envelope drives" regardless of level).
//
// prompt is the level-appropriate prompt body the dispatching reconciler
// supplies; it is assigned verbatim to EnvelopeIn.Prompt so the real
// subagent's template render has the actual outcome to author against
// (defect #4: prior to this the field was never set and the real Claude
// planner saw an empty prompt). token and prompt are DISTINCT params —
// token is the credproxy HMAC, prompt is the authoring instruction. The
// project planner passes Project.Spec.OutcomePrompt; deeper planners pass
// the same outcome (the parent artifact context — MILESTONE.md, the phase
// brief, PLAN.md — lives on the PVC and the template instructs reading it,
// and ParentName flows through EnvelopeIn.Dispatch).
// outcomePromptOf returns project.Spec.OutcomePrompt, nil-safe: the deeper
// reconcilers resolve the owning Project by walking the parent chain
// (resolveProject / resolveProjectForPlan), which returns nil on a not-yet-
// visible chain. A nil project yields "" — the same empty-prompt shape the
// real subagent already guards (EMPTY_PROMPT warning) rather than a panic.
func outcomePromptOf(project *tideprojectv1alpha3.Project) string {
	if project == nil {
		return ""
	}
	return project.Spec.OutcomePrompt
}

// BuildPlannerEnvelope constructs and marshals an EnvelopeIn for a
// planner-level dispatch. The sharedContext parameter (Phase 20 D-07) is
// stamped byte-identically into EnvelopeIn.SharedContext for all wave siblings
// dispatched with the same parent-curated blob (CACHE-02 uniform population).
// Callers pass the parent CRD's Spec.SharedContext; test fixtures pass "".
func BuildPlannerEnvelope(level string, parent metav1.Object, project *tideprojectv1alpha3.Project, attempt int, token, prompt string, caps pkgdispatch.Caps, proxyEndpoint string, helmDefaults ProviderDefaults, sharedContext string) (pkgdispatch.EnvelopeIn, []byte, error) {
	envIn := pkgdispatch.EnvelopeIn{
		APIVersion:    pkgdispatch.APIVersionV1Alpha1,
		Kind:          pkgdispatch.KindTaskEnvelopeIn,
		TaskUID:       string(parent.GetUID()),
		Role:          "planner",
		Level:         level,
		Prompt:        prompt,
		Caps:          caps,
		ProxyEndpoint: proxyEndpoint,
		SignedToken:   token,
		Provider:      ResolveProvider(project, level, helmDefaults),
		SharedContext: sharedContext, // D-07: uniform stamp; empty for executor path (CACHE-02 lock)
	}

	// Stamp parentName into the dedicated Dispatch metadata field so the stub
	// planner can populate the child *Ref field (e.g. milestoneRef, phaseRef)
	// without querying the K8s API — parent.GetName() is the authoritative
	// source (T-07-03-03: parentName is metadata, not a secret). This is kept
	// out of Provider.Params (model-parameters-only) so the anthropic runner's
	// strict param allow-list is not tripped by dispatch metadata.
	envIn.Dispatch = &pkgdispatch.DispatchMeta{ParentName: parent.GetName()}

	data, err := json.Marshal(envIn)
	if err != nil {
		return pkgdispatch.EnvelopeIn{}, nil, fmt.Errorf("marshal planner envelope: %w", err)
	}
	return envIn, data, nil
}

// MaterializeChildCRDs delegates to reporter.MaterializeChildCRDs.
// The materialization machinery lives in internal/reporter so cmd/tide-reporter
// can import it without a back-edge to internal/controller (plan 09-04).
func MaterializeChildCRDs(ctx context.Context, c client.Client, scheme *runtime.Scheme, parent metav1.Object, children []pkgdispatch.ChildCRDSpec) error {
	return reporter.MaterializeChildCRDs(ctx, c, scheme, parent, children)
}

// resolveImage walks Project.Spec.Subagent precedence chain for the given
// dispatch level, first mapping it to its Levels.<overrideKey> slot via
// levelOverrideKey (D-02), and returns the resolved subagent container image
// reference.
//
//	Levels.<overrideKey>.Image → Spec.Subagent.Image → helmDefaults.Image → ""
//
// An empty return means no image was configured; callers must surface this
// as a config error rather than dispatching a Job with an empty image field.
// Post-D-02, every dispatch level maps to a real Levels.<overrideKey> slot —
// the "project" level's prior silent fall-through (the CRD had no
// Levels.Project) is dead: it now resolves via the Levels.Milestone slot.
func resolveImage(project *tideprojectv1alpha3.Project, level string, helmDefaults ProviderDefaults) string {
	key := levelOverrideKey(level)
	var levelCfg *tideprojectv1alpha3.LevelConfig
	if project != nil {
		switch key {
		case "milestone":
			levelCfg = project.Spec.Subagent.Levels.Milestone
		case "phase":
			levelCfg = project.Spec.Subagent.Levels.Phase
		case "plan":
			levelCfg = project.Spec.Subagent.Levels.Plan
		case "task":
			levelCfg = project.Spec.Subagent.Levels.Task
		}
	}
	switch {
	case levelCfg != nil && levelCfg.Image != "":
		return levelCfg.Image
	case project != nil && project.Spec.Subagent.Image != "":
		return project.Spec.Subagent.Image
	default:
		return helmDefaults.Image
	}
}

// resolveAgentIdentity walks the D-03 precedence chain for the committer/author
// identity TIDE stamps at all three commit sites (SIGN-01), returning the
// resolved (name, email) pair. Each field resolves independently:
//
//	project.Spec.Git.AgentName  → helmDefaults.AgentName  → pkggit.DefaultAgentName
//	project.Spec.Git.AgentEmail → helmDefaults.AgentEmail → pkggit.DefaultAgentEmail
//
// Resolution is pure — it never reads the environment (the manager's job is to
// carry the chart tier into helmDefaults; see cmd/manager/env.go). Both
// project and project.Spec.Git are nil-checked (Spec.Git is *GitConfig,
// Pitfall 7), so a nil project or an absent GitConfig resolves cleanly to the
// chart tier or the compiled default. Non-empty is the override signal at every
// tier, matching resolveImage.
func resolveAgentIdentity(project *tideprojectv1alpha3.Project, helmDefaults ProviderDefaults) (name, email string) {
	name = pkggit.DefaultAgentName
	email = pkggit.DefaultAgentEmail

	if helmDefaults.AgentName != "" {
		name = helmDefaults.AgentName
	}
	if helmDefaults.AgentEmail != "" {
		email = helmDefaults.AgentEmail
	}

	if project != nil && project.Spec.Git != nil {
		if project.Spec.Git.AgentName != "" {
			name = project.Spec.Git.AgentName
		}
		if project.Spec.Git.AgentEmail != "" {
			email = project.Spec.Git.AgentEmail
		}
	}

	return name, email
}

// plannerInFlightCount returns the count of non-terminal planner Jobs currently
// visible in the informer cache. Used by the D3 concurrency cap gate before
// PlannerPool.Acquire at each of the four planner dispatch sites.
//
// An empty watchNamespace counts across all namespaces (cluster-scoped install
// posture — mirrors pool.PreCharge and project_controller.go:949). When
// watchNamespace is non-empty, the list is restricted to that namespace so the
// namespace-scoped informer cache is not asked for cross-namespace entries.
//
// Returns (0, err) on List failure; callers treat this as a transient error and
// return ctrl.Result{}, fmt.Errorf("planner in-flight count: %w", err).
func plannerInFlightCount(ctx context.Context, c client.Client, watchNamespace string) (int, error) {
	var jobs batchv1.JobList
	opts := []client.ListOption{
		client.MatchingLabels{"tideproject.k8s/role": "planner"},
	}
	if watchNamespace != "" {
		opts = append(opts, client.InNamespace(watchNamespace))
	}
	if err := c.List(ctx, &jobs, opts...); err != nil {
		return 0, err
	}
	n := 0
	for i := range jobs.Items {
		// A Job being deleted (DeletionTimestamp set) is on its way out — its pod
		// is terminating — so it must not hold a cap slot. A stalled-GC Job would
		// otherwise linger non-terminal and throttle dispatch across all namespaces
		// (global cap). Skip it; count only live, non-terminal planner Jobs.
		if jobs.Items[i].DeletionTimestamp != nil {
			continue
		}
		if !isJobTerminal(&jobs.Items[i]) {
			n++
		}
	}
	return n, nil
}

// checkParentApproval implements the D-02 descent hold — children materialize
// but dispatch waits for parental approval (tidal lock pending).
//
// Returns (true, nil) when the direct parent is parked at AwaitingApproval,
// signalling that the child reconciler must hold Job dispatch with a 5s requeue.
// Returns (false, nil) when parentName is empty (root level — no parent to check)
// or when the parent is not found (client.IgnoreNotFound — NotFound is transient
// informer lag; callers continue dispatch and the next reconcile re-checks).
// Non-NotFound Get errors propagate to the standard requeue-on-error path.
//
// The parentKind switch covers "Milestone", "Phase", and "Plan" — the three
// parent kinds that can park at AwaitingApproval. Unknown kinds return (false, nil)
// (permissive default: unknown parent kind should not block dispatch).
//
// Design note: held children stay at Status.Phase="" — this helper writes NO
// status so tide approve's findAwaiting* cannot target a held child instead of
// the parked parent (Pitfall 5 from 12-RESEARCH.md).
func checkParentApproval(ctx context.Context, c client.Client, ns, parentName, parentKind string) (bool, error) {
	if parentName == "" {
		return false, nil
	}
	switch parentKind {
	case "Milestone":
		var ms tideprojectv1alpha3.Milestone
		if err := c.Get(ctx, client.ObjectKey{Namespace: ns, Name: parentName}, &ms); err != nil {
			return false, client.IgnoreNotFound(err)
		}
		return ms.Status.Phase == tideprojectv1alpha3.LevelPhaseAwaitingApproval, nil
	case "Phase":
		var ph tideprojectv1alpha3.Phase
		if err := c.Get(ctx, client.ObjectKey{Namespace: ns, Name: parentName}, &ph); err != nil {
			return false, client.IgnoreNotFound(err)
		}
		return ph.Status.Phase == tideprojectv1alpha3.LevelPhaseAwaitingApproval, nil
	case "Plan":
		var plan tideprojectv1alpha3.Plan
		if err := c.Get(ctx, client.ObjectKey{Namespace: ns, Name: parentName}, &plan); err != nil {
			return false, client.IgnoreNotFound(err)
		}
		return plan.Status.Phase == tideprojectv1alpha3.LevelPhaseAwaitingApproval, nil
	}
	return false, nil
}

// checkDispatchHolds centralizes the planner-tier project-scoped dispatch-holds
// gate chain shared by MilestoneReconciler, PhaseReconciler, and PlanReconciler
// (item 7, Phase 41 D-07 — the seed's "finish an extraction the codebase
// already started"). The order — Billing(30s) → Failure(30s) → Budget(30s) →
// Import(5s) — and the requeue literals are load-bearing: this is the
// enforcement point that stops planner Job dispatch (and therefore LLM spend)
// on BillingHalt / FailureHalt / BudgetBlocked / import-pending. A swap in
// order or interval is a spend-gate bypass or an over-holding regression, not
// a cosmetic change (T-41-05a/b).
//
// TaskReconciler intentionally does NOT call this helper — its chain checks
// Import SECOND (immediately after parent-approval, before Billing/Failure/
// Budget) and adds a task-only reservation-headroom hold with no planner-tier
// counterpart. Normalizing Task onto this order would be a behavior change in
// a non-breaking phase; see the divergence comment at task_controller.go's
// gate chain and .planning/todos/pending/2026-07-12-task-dispatch-gate-order-divergence.md.
//
// nil-safe: a nil project is not a hold (matches checkBillingHalt /
// checkFailureHalt / checkBudgetBlocked's own nil-safe wrappers).
func checkDispatchHolds(ctx context.Context, project *tideprojectv1alpha3.Project, level, objName string) (held bool, result ctrl.Result) {
	if project == nil {
		return false, ctrl.Result{}
	}

	// Phase 13 HALT-01 / D-05: third dispatch-entry hold (after CheckRejected +
	// parent-approval); park, never fail; cleared by tide resume.
	// Position: BEFORE pool acquire and BEFORE Job creation (Pitfall 2).
	if checkBillingHalt(project) {
		logf.FromContext(ctx).V(1).Info("dispatch held: project billing halt",
			"level", level, "name", objName, "project", project.Name)
		return true, ctrl.Result{RequeueAfter: 30 * time.Second}
	}
	// Phase 25 DISP-02 / D-02b: conservative failure halt hold.
	// Execution-only (not planner) — gates dispatch when ConditionFailureHalt=True.
	// Park (never fail); cleared by `tide resume --retry-failed`.
	if checkFailureHalt(project) {
		logf.FromContext(ctx).V(1).Info("dispatch held: project failure halt (conservative profile)",
			"level", level, "name", objName, "project", project.Name)
		return true, ctrl.Result{RequeueAfter: 30 * time.Second}
	}
	// Phase 14 BUDGET-02 / D-04: BudgetBlocked hold (operator cap) — separate from
	// BillingHalt (provider billing); both may be true simultaneously.
	if checkBudgetBlocked(project) && !budget.IsBypassed(project, time.Now()) {
		logf.FromContext(ctx).V(1).Info("dispatch held: project budget blocked",
			"level", level, "name", objName, "project", project.Name)
		return true, ctrl.Result{RequeueAfter: 30 * time.Second}
	}
	// Phase 28 IMPORT-01: park planner dispatch until import completes.
	// Position: BEFORE pool acquire (Pitfall 2 — parking after acquire leaks a slot).
	if project.Spec.ImportSource != nil {
		c := meta.FindStatusCondition(project.Status.Conditions, tideprojectv1alpha3.ConditionImportComplete)
		if c == nil || c.Status != metav1.ConditionTrue {
			logf.FromContext(ctx).V(1).Info("import pending; holding planner dispatch",
				"level", level, "name", objName, "project", project.Name)
			return true, ctrl.Result{RequeueAfter: 5 * time.Second}
		}
	}

	return false, ctrl.Result{}
}
