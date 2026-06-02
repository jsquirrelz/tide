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

// informer_bridge.go — translates controller-runtime watch events on all
// 6 TIDE CRDs into hub.Publish calls keyed by the owning Project name.
//
// Plan 04-11 / DASH-03 wires the bridge once at boot via main.go:
//
//   mgr.Add(manager.RunnableFunc(func(ctx context.Context) error {
//       return api.BridgeInformerToHub(ctx, mgr.GetCache(), mgr.GetClient(), hub, log)
//   }))
//
// The manager starts the cache (which starts the informers) before
// running our Runnable; AddEventHandler is safe-to-call at any point and
// receives events for both initial-list AND ongoing watches.
//
// Project-key resolution chain (no OwnerRefs are stamped today —
// internal/controller/* uses Spec.{Project,Milestone,Phase,Plan}Ref):
//
//   Project   → self
//   Milestone → .Spec.ProjectRef
//   Phase     → .Spec.MilestoneRef → Milestone → .Spec.ProjectRef
//   Plan      → .Spec.PhaseRef → Phase → ...
//   Task      → .Spec.PlanRef → Plan → ...
//   Wave      → .Spec.PlanRef → Plan → ...
//
// Failures to resolve the parent project are logged at V(1) and the event
// is silently dropped — the dashboard's correctness budget allows missed
// events on a misconfigured CRD (the next event will refresh).

package api

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/go-logr/logr"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	toolscache "k8s.io/client-go/tools/cache"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"

	tidev1alpha1 "github.com/jsquirrelz/tide/api/v1alpha1"
	"github.com/jsquirrelz/tide/cmd/dashboard/hub"
)

// BridgeInformerToHub registers ResourceEventHandlers on the informers
// for all 6 TIDE CRD kinds. Each handler resolves the owning Project name
// for the event's object, serializes a minimal JSON projection, and calls
// hub.Publish.
//
// The function returns once all 6 informers have a handler attached;
// the bridge does not own a goroutine of its own — controller-runtime's
// informer reconciler drives the handler callbacks.
//
// `cli` is a reader-only client used for owner-chain resolution
// (Milestone → Project, Phase → Milestone, etc.). It MUST be backed by
// the same cache as `c` to ensure consistent reads during event delivery.
//
//nolint:logcheck // both ctx and logger are intentional here: ctx drives informer lifecycle, logger is the named bridge logger
func BridgeInformerToHub(ctx context.Context, c cache.Cache, cli client.Reader, h *hub.Hub, log logr.Logger) error {
	type kindWiring struct {
		obj        client.Object
		typePrefix string // "project", "milestone", "phase", "plan", "task", "wave"
	}
	wirings := []kindWiring{
		{&tidev1alpha1.Project{}, "project"},
		{&tidev1alpha1.Milestone{}, "milestone"},
		{&tidev1alpha1.Phase{}, "phase"},
		{&tidev1alpha1.Plan{}, "plan"},
		{&tidev1alpha1.Task{}, "task"},
		{&tidev1alpha1.Wave{}, "wave"},
	}

	for _, w := range wirings {
		inf, err := c.GetInformer(ctx, w.obj)
		if err != nil {
			return fmt.Errorf("get informer for %T: %w", w.obj, err)
		}
		handler := newKindHandler(ctx, cli, h, log, w.typePrefix)
		if _, err := inf.AddEventHandler(handler); err != nil {
			return fmt.Errorf("add event handler for %T: %w", w.obj, err)
		}
	}
	return nil
}

// newKindHandler returns a toolscache.ResourceEventHandler that closes
// over the project-key resolver + the hub + the per-kind event-type
// prefix. The prefix becomes the SSE `event:` field: "<prefix>.create",
// "<prefix>.update", "<prefix>.delete".
//
//nolint:logcheck // both ctx and logger are intentional: ctx scopes owner-chain reads, logger is the named bridge logger
func newKindHandler(ctx context.Context, cli client.Reader, h *hub.Hub, log logr.Logger, typePrefix string) toolscache.ResourceEventHandler {
	publish := func(verb string, obj any) {
		co, ok := obj.(client.Object)
		if !ok {
			// DeletedFinalStateUnknown or non-object — try the tombstone
			// shape from client-go before giving up.
			if tomb, ok := obj.(toolscache.DeletedFinalStateUnknown); ok {
				if inner, ok := tomb.Obj.(client.Object); ok {
					co = inner
				}
			}
		}
		if co == nil {
			log.V(1).Info("informer event missing client.Object",
				"verb", verb, "kind", typePrefix)
			return
		}

		projectKey, err := resolveProjectKey(ctx, cli, co)
		if err != nil || projectKey == "" {
			log.V(1).Info("could not resolve project key; dropping event",
				"verb", verb, "kind", typePrefix, "name", co.GetName(), "err", err)
			return
		}

		payload := minimalProjection(co)
		buf, err := json.Marshal(payload)
		if err != nil {
			log.V(1).Info("failed to marshal projection",
				"verb", verb, "kind", typePrefix, "err", err)
			return
		}
		h.Publish(projectKey, hub.Event{
			Type: typePrefix + "." + verb,
			JSON: json.RawMessage(buf),
		})
	}

	return toolscache.ResourceEventHandlerFuncs{
		AddFunc: func(obj any) {
			publish("create", obj)
		},
		UpdateFunc: func(_, newObj any) {
			publish("update", newObj)
		},
		DeleteFunc: func(obj any) {
			publish("delete", obj)
		},
	}
}

// resolveProjectKey returns the name of the Project that owns `obj`. The
// resolution chain follows Spec.{Project,Milestone,Phase,Plan}Ref because
// the controllers currently don't stamp OwnerReferences — see
// internal/controller/*_controller.go and the project_v1alpha1.AddToScheme
// list. Returns an empty string + nil error if the chain breaks (e.g., a
// dangling reference to a deleted parent); callers treat this as
// "drop the event".
func resolveProjectKey(ctx context.Context, cli client.Reader, obj client.Object) (string, error) {
	switch v := obj.(type) {
	case *tidev1alpha1.Project:
		return v.GetName(), nil

	case *tidev1alpha1.Milestone:
		return v.Spec.ProjectRef, nil

	case *tidev1alpha1.Phase:
		ms, err := getMilestone(ctx, cli, v.GetNamespace(), v.Spec.MilestoneRef)
		if err != nil {
			return "", err
		}
		if ms == nil {
			return "", nil
		}
		return ms.Spec.ProjectRef, nil

	case *tidev1alpha1.Plan:
		ph, err := getPhase(ctx, cli, v.GetNamespace(), v.Spec.PhaseRef)
		if err != nil {
			return "", err
		}
		if ph == nil {
			return "", nil
		}
		return resolveProjectKey(ctx, cli, ph)

	case *tidev1alpha1.Task:
		pl, err := getPlan(ctx, cli, v.GetNamespace(), v.Spec.PlanRef)
		if err != nil {
			return "", err
		}
		if pl == nil {
			return "", nil
		}
		return resolveProjectKey(ctx, cli, pl)

	case *tidev1alpha1.Wave:
		pl, err := getPlan(ctx, cli, v.GetNamespace(), v.Spec.PlanRef)
		if err != nil {
			return "", err
		}
		if pl == nil {
			return "", nil
		}
		return resolveProjectKey(ctx, cli, pl)

	default:
		return "", fmt.Errorf("unsupported kind %T for resolveProjectKey", obj)
	}
}

func getMilestone(ctx context.Context, cli client.Reader, ns, name string) (*tidev1alpha1.Milestone, error) {
	if name == "" {
		return nil, nil
	}
	var m tidev1alpha1.Milestone
	if err := cli.Get(ctx, types.NamespacedName{Namespace: ns, Name: name}, &m); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	return &m, nil
}

func getPhase(ctx context.Context, cli client.Reader, ns, name string) (*tidev1alpha1.Phase, error) {
	if name == "" {
		return nil, nil
	}
	var p tidev1alpha1.Phase
	if err := cli.Get(ctx, types.NamespacedName{Namespace: ns, Name: name}, &p); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	return &p, nil
}

func getPlan(ctx context.Context, cli client.Reader, ns, name string) (*tidev1alpha1.Plan, error) {
	if name == "" {
		return nil, nil
	}
	var p tidev1alpha1.Plan
	if err := cli.Get(ctx, types.NamespacedName{Namespace: ns, Name: name}, &p); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	return &p, nil
}

// minimalProjection is the JSON shape published on every event. We
// deliberately do NOT serialize the full CRD — managedFields,
// resourceVersion churn, and the heavy Spec/Status payloads would bloat
// the SSE wire by ~10×. The dashboard treats events as "ping that
// something changed for object X"; it re-fetches the detail via the
// existing GET endpoints when it needs full state.
//
// Shape: { name, namespace, kind, phase, resourceVersion }
//   - name + namespace identify the object uniquely
//   - kind lets the frontend route the update to the right node-type
//   - phase is the single most-asked-for status field (used in pill render)
//   - resourceVersion lets the frontend de-dupe rapid update bursts
func minimalProjection(obj client.Object) map[string]string {
	p := map[string]string{
		"name":            obj.GetName(),
		"namespace":       obj.GetNamespace(),
		"resourceVersion": obj.GetResourceVersion(),
	}
	switch v := obj.(type) {
	case *tidev1alpha1.Project:
		p["kind"] = "Project"
		p["phase"] = v.Status.Phase
	case *tidev1alpha1.Milestone:
		p["kind"] = "Milestone"
		p["phase"] = v.Status.Phase
		p["projectRef"] = v.Spec.ProjectRef
	case *tidev1alpha1.Phase:
		p["kind"] = "Phase"
		p["phase"] = v.Status.Phase
		p["milestoneRef"] = v.Spec.MilestoneRef
	case *tidev1alpha1.Plan:
		p["kind"] = "Plan"
		p["phase"] = v.Status.Phase
		p["phaseRef"] = v.Spec.PhaseRef
	case *tidev1alpha1.Task:
		p["kind"] = "Task"
		p["phase"] = v.Status.Phase
		p["planRef"] = v.Spec.PlanRef
	case *tidev1alpha1.Wave:
		p["kind"] = "Wave"
		p["phase"] = v.Status.Phase
		p["planRef"] = v.Spec.PlanRef
	}
	return p
}
