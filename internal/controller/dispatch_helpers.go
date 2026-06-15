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

// Package controller â dispatch_helpers.go consolidates the three planner
// dispatch helpers that all three up-stack reconcilers (Milestone, Phase,
// Plan) share (Phase 3 D-A1 / D-A2 / D-A4). The helpers exist to keep the
// reconciler bodies from drifting in lockstep â each reconciler is ~80-150
// LOC of NEW code instead of ~230 LOC because the shared bits live here.
//
//   - ResolveProvider walks the Project.Spec.Subagent precedence chain
//     per D-C2: levels.{level}.{model,params} â Project default â
//     Helm-chart default.
//
//   - BuildPlannerEnvelope mirrors task_controller.go buildEnvelopeIn for
//     planner-level dispatch â sets Role="planner", Level=<level>,
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

	batchv1 "k8s.io/api/batch/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	tideprojectv1alpha1 "github.com/jsquirrelz/tide/api/v1alpha1"
	"github.com/jsquirrelz/tide/internal/reporter"
	pkgdispatch "github.com/jsquirrelz/tide/pkg/dispatch"
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
func spawnReporterIfNeeded(
	ctx context.Context,
	c client.Client,
	scheme *runtime.Scheme,
	parent metav1.Object,
	project *tideprojectv1alpha1.Project,
	parentKind string,
	reporterImage string,
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
		reporterJob := BuildReporterJob(parent, project, defaultSharedPVCName, string(parent.GetUID()), parentKind,
			ReporterOptions{ReporterImage: reporterImage}, scheme)
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
	// "no Helm default" â caller is responsible for surfacing this
	// at Job creation time (a missing image is a config error).
	Image string

	// Models maps levelâmodel. Missing key means "no Helm default for
	// that level".
	Models map[string]string
}

// ResolveProvider walks Project.Spec.Subagent precedence chain for the
// given level (D-C2). Returns a ProviderSpec with Vendor pinned to
// "anthropic" in v1.0 (per-vendor selection deferred â CONTEXT.md
// "Deferred Ideas"). Model and Params resolve via:
//
//	project.Spec.Subagent.Levels.<level>.Model â
//	project.Spec.Subagent.Model â
//	helmDefaults.Models[<level>] â
//	"" (caller surfaces missing-config error)
//
// Params merge: level Params copied first, then Project-level Params
// inserted only for keys NOT already set at the level â i.e., level
// wins on key conflict.
func ResolveProvider(project *tideprojectv1alpha1.Project, level string, helmDefaults ProviderDefaults) pkgdispatch.ProviderSpec {
	// Helper to read per-level overrides.
	var levelCfg *tideprojectv1alpha1.LevelConfig
	if project != nil {
		switch level {
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
			model = helmDefaults.Models[level]
		}
	}

	// Merge Params â level overrides Project defaults on key conflict.
	var params map[string]string
	if levelCfg != nil && len(levelCfg.Params) > 0 {
		params = make(map[string]string, len(levelCfg.Params))
		maps.Copy(params, levelCfg.Params)
	}
	// (Project-level Params are not currently exposed on SubagentConfig
	// â LevelConfig.Params is the per-level extension; if a future
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
// schema, but it carries the parent's UID at the planner level â the
// semantic is "the dispatch this envelope drives" regardless of level).
//
// prompt is the level-appropriate prompt body the dispatching reconciler
// supplies; it is assigned verbatim to EnvelopeIn.Prompt so the real
// subagent's template render has the actual outcome to author against
// (defect #4: prior to this the field was never set and the real Claude
// planner saw an empty prompt). token and prompt are DISTINCT params â
// token is the credproxy HMAC, prompt is the authoring instruction. The
// project planner passes Project.Spec.OutcomePrompt; deeper planners pass
// the same outcome (the parent artifact context â MILESTONE.md, the phase
// brief, PLAN.md â lives on the PVC and the template instructs reading it,
// and ParentName flows through EnvelopeIn.Dispatch).
// outcomePromptOf returns project.Spec.OutcomePrompt, nil-safe: the deeper
// reconcilers resolve the owning Project by walking the parent chain
// (resolveProject / resolveProjectForPlan), which returns nil on a not-yet-
// visible chain. A nil project yields "" — the same empty-prompt shape the
// real subagent already guards (EMPTY_PROMPT warning) rather than a panic.
func outcomePromptOf(project *tideprojectv1alpha1.Project) string {
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
func BuildPlannerEnvelope(level string, parent metav1.Object, project *tideprojectv1alpha1.Project, attempt int, token, prompt string, caps pkgdispatch.Caps, proxyEndpoint string, helmDefaults ProviderDefaults, sharedContext string) (pkgdispatch.EnvelopeIn, []byte, error) {
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
	// without querying the K8s API â parent.GetName() is the authoritative
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
// level, returning the resolved subagent container image reference.
//
//	Levels.<level>.Image → Spec.Subagent.Image → helmDefaults.Image → ""
//
// An empty return means no image was configured; callers must surface this
// as a config error rather than dispatching a Job with an empty image field.
// Level "project" has no entry in the switch (the CRD has no Levels.Project);
// resolution falls straight to Spec.Subagent.Image, then helmDefaults.Image.
func resolveImage(project *tideprojectv1alpha1.Project, level string, helmDefaults ProviderDefaults) string {
	var levelCfg *tideprojectv1alpha1.LevelConfig
	if project != nil {
		switch level {
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
		var ms tideprojectv1alpha1.Milestone
		if err := c.Get(ctx, client.ObjectKey{Namespace: ns, Name: parentName}, &ms); err != nil {
			return false, client.IgnoreNotFound(err)
		}
		return ms.Status.Phase == "AwaitingApproval", nil
	case "Phase":
		var ph tideprojectv1alpha1.Phase
		if err := c.Get(ctx, client.ObjectKey{Namespace: ns, Name: parentName}, &ph); err != nil {
			return false, client.IgnoreNotFound(err)
		}
		return ph.Status.Phase == "AwaitingApproval", nil
	case "Plan":
		var plan tideprojectv1alpha1.Plan
		if err := c.Get(ctx, client.ObjectKey{Namespace: ns, Name: parentName}, &plan); err != nil {
			return false, client.IgnoreNotFound(err)
		}
		return plan.Status.Phase == "AwaitingApproval", nil
	}
	return false, nil
}
