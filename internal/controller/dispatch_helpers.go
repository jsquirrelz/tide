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

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	tideprojectv1alpha1 "github.com/jsquirrelz/tide/api/v1alpha1"
	"github.com/jsquirrelz/tide/internal/owner"
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
	// "no Helm default" — caller is responsible for surfacing this
	// at Job creation time (a missing image is a config error).
	Image string

	// Models maps level→model. Missing key means "no Helm default for
	// that level".
	Models map[string]string
}

// childKindAllowlist is the T-308 mitigation gate: only these Kinds may
// pass through MaterializeChildCRDs. Anything else returns an error
// before any K8s API call is made. The set matches the five TIDE CRD
// Kinds; non-TIDE Kinds (Pod, ConfigMap, Job, etc.) MUST never reach
// server-side-create from a planner pod's emitted ChildCRDs envelope
// (subagent pod has zero K8s verbs per Phase 2 D-A4 — the envelope is
// the only channel from the subagent process into the cluster's typed
// CRD graph).
var childKindAllowlist = map[string]bool{
	"Milestone": true,
	"Phase":     true,
	"Plan":      true,
	"Task":      true,
	"Wave":      true,
}

// ResolveProvider walks Project.Spec.Subagent precedence chain for the
// given level (D-C2). Returns a ProviderSpec with Vendor pinned to
// "anthropic" in v1.0 (per-vendor selection deferred — CONTEXT.md
// "Deferred Ideas"). Model and Params resolve via:
//
//	project.Spec.Subagent.Levels.<level>.Model →
//	project.Spec.Subagent.Model →
//	helmDefaults.Models[<level>] →
//	"" (caller surfaces missing-config error)
//
// Params merge: level Params copied first, then Project-level Params
// inserted only for keys NOT already set at the level — i.e., level
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

	// Merge Params — level overrides Project defaults on key conflict.
	var params map[string]string
	if levelCfg != nil && len(levelCfg.Params) > 0 {
		params = make(map[string]string, len(levelCfg.Params))
		for k, v := range levelCfg.Params {
			params[k] = v
		}
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
func BuildPlannerEnvelope(level string, parent metav1.Object, project *tideprojectv1alpha1.Project, attempt int, token string, caps pkgdispatch.Caps, proxyEndpoint string, helmDefaults ProviderDefaults) (pkgdispatch.EnvelopeIn, []byte, error) {
	envIn := pkgdispatch.EnvelopeIn{
		APIVersion:    pkgdispatch.APIVersionV1Alpha1,
		Kind:          pkgdispatch.KindTaskEnvelopeIn,
		TaskUID:       string(parent.GetUID()),
		Role:          "planner",
		Level:         level,
		Caps:          caps,
		ProxyEndpoint: proxyEndpoint,
		SignedToken:   token,
		Provider:      ResolveProvider(project, level, helmDefaults),
	}

	// Inject parentName into Provider.Params so the stub (and real planner
	// subagents) can populate the child *Ref field (e.g. milestoneRef, phaseRef)
	// without querying the K8s API — parent.GetName() is the authoritative
	// source (T-07-03-03: parentName is metadata, not a secret).
	if envIn.Provider.Params == nil {
		envIn.Provider.Params = make(map[string]string)
	}
	envIn.Provider.Params["parentName"] = parent.GetName()

	data, err := json.Marshal(envIn)
	if err != nil {
		return pkgdispatch.EnvelopeIn{}, nil, fmt.Errorf("marshal planner envelope: %w", err)
	}
	return envIn, data, nil
}

// MaterializeChildCRDs server-side-creates child CRDs from
// EnvelopeOut.ChildCRDs.
//
// Each child is allocated to its concrete *tideprojectv1alpha1 pointer
// based on Kind (only the allowlist-approved Kinds advance to creation —
// T-308 mitigation). The child's Spec is decoded from child.Spec.Raw via
// json.Unmarshal directly into the typed Spec field. ObjectMeta.Name is
// child.Name; Namespace is the parent's namespace. OwnerRef is set via
// internal/owner.EnsureOwnerRef (which enforces same-namespace per
// Pitfall 23 and sets Controller=true / BlockOwnerDeletion=true).
//
// AlreadyExists on Create is treated as idempotent success (mirrors
// Phase 2 task_controller.go:397-403 SUB-03 / Pitfall F watch-lag race
// handling). Any other error short-circuits the loop and returns —
// callers should patch their parent's Status.Phase=Failed.
func MaterializeChildCRDs(ctx context.Context, c client.Client, scheme *runtime.Scheme, parent metav1.Object, children []pkgdispatch.ChildCRDSpec) error {
	// Pre-flight: enforce Kind allowlist BEFORE any K8s API call.
	// Any rejected Kind aborts the whole batch (planner contract
	// violation — the envelope is poisoned; refuse to materialize any
	// of it).
	for _, child := range children {
		if !childKindAllowlist[child.Kind] {
			return fmt.Errorf("MaterializeChildCRDs: kind %q not in allowlist (allowed: Milestone, Phase, Plan, Task, Wave); refusing to create — T-308 mitigation", child.Kind)
		}
	}

	for _, child := range children {
		var obj client.Object
		switch child.Kind {
		case "Milestone":
			ms := &tideprojectv1alpha1.Milestone{}
			if err := json.Unmarshal(child.Spec.Raw, &ms.Spec); err != nil {
				return fmt.Errorf("MaterializeChildCRDs: unmarshal Milestone %q spec: %w", child.Name, err)
			}
			obj = ms
		case "Phase":
			ph := &tideprojectv1alpha1.Phase{}
			if err := json.Unmarshal(child.Spec.Raw, &ph.Spec); err != nil {
				return fmt.Errorf("MaterializeChildCRDs: unmarshal Phase %q spec: %w", child.Name, err)
			}
			obj = ph
		case "Plan":
			pl := &tideprojectv1alpha1.Plan{}
			if err := json.Unmarshal(child.Spec.Raw, &pl.Spec); err != nil {
				return fmt.Errorf("MaterializeChildCRDs: unmarshal Plan %q spec: %w", child.Name, err)
			}
			obj = pl
		case "Task":
			tk := &tideprojectv1alpha1.Task{}
			if err := json.Unmarshal(child.Spec.Raw, &tk.Spec); err != nil {
				return fmt.Errorf("MaterializeChildCRDs: unmarshal Task %q spec: %w", child.Name, err)
			}
			obj = tk
		case "Wave":
			wv := &tideprojectv1alpha1.Wave{}
			if err := json.Unmarshal(child.Spec.Raw, &wv.Spec); err != nil {
				return fmt.Errorf("MaterializeChildCRDs: unmarshal Wave %q spec: %w", child.Name, err)
			}
			obj = wv
		default:
			// Unreachable — allowlist was checked above. Defensive.
			return fmt.Errorf("MaterializeChildCRDs: kind %q unreachable after allowlist", child.Kind)
		}

		obj.SetName(child.Name)
		obj.SetNamespace(parent.GetNamespace())

		if err := owner.EnsureOwnerRef(obj, parent, scheme); err != nil {
			return fmt.Errorf("MaterializeChildCRDs: ensure owner ref on %s/%s: %w", child.Kind, child.Name, err)
		}

		if err := c.Create(ctx, obj); err != nil {
			if !apierrors.IsAlreadyExists(err) {
				return fmt.Errorf("MaterializeChildCRDs: create %s/%s: %w", child.Kind, child.Name, err)
			}
			// AlreadyExists: idempotent success (SUB-03 / Pitfall F).
		}
	}
	return nil
}
