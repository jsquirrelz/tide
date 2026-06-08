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

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	tideprojectv1alpha1 "github.com/jsquirrelz/tide/api/v1alpha1"
	"github.com/jsquirrelz/tide/internal/reporter"
	pkgdispatch "github.com/jsquirrelz/tide/pkg/dispatch"
)

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

// childKindAllowlist is a package-local alias for the allowlist that now lives in
// internal/reporter. Call sites within internal/controller reference it via this
// delegator so callers need not be updated in this plan.
var childKindAllowlist = reporter.ChildKindAllowlist

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

func BuildPlannerEnvelope(level string, parent metav1.Object, project *tideprojectv1alpha1.Project, attempt int, token, prompt string, caps pkgdispatch.Caps, proxyEndpoint string, helmDefaults ProviderDefaults) (pkgdispatch.EnvelopeIn, []byte, error) {
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

// childrenAlreadyMaterialized delegates to reporter.ChildrenAlreadyMaterialized.
// The guard logic lives in internal/reporter so cmd/tide-reporter can import it;
// the controller callers remain unchanged (plan 09-04).
func childrenAlreadyMaterialized(ctx context.Context, c client.Client, parent metav1.Object) (bool, error) {
	return reporter.ChildrenAlreadyMaterialized(ctx, c, parent)
}

// MaterializeChildCRDs delegates to reporter.MaterializeChildCRDs.
// The materialization machinery lives in internal/reporter so cmd/tide-reporter
// can import it without a back-edge to internal/controller (plan 09-04).
func MaterializeChildCRDs(ctx context.Context, c client.Client, scheme *runtime.Scheme, parent metav1.Object, children []pkgdispatch.ChildCRDSpec) error {
	return reporter.MaterializeChildCRDs(ctx, c, scheme, parent, children)
}
