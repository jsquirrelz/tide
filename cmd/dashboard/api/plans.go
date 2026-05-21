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

// plans.go — GET /api/v1/plans/{name} (plan 04-17).
//
// Surfaces the rich Plan + child Task DAG shape that the SSE projection
// (informer_bridge.go) cannot carry. The dashboard's right-pane
// ExecutionDAGView calls fetchPlan(name) on plan-name change and on every
// SSE refresh-trigger (kind ∈ {Plan, Task, Wave} whose planRef matches)
// and rebuilds ExecutionPlanData from this payload.
//
// DASH-05 zero-mutation contract: this handler is HTTP GET. The router
// walks the route tree at test time and fails the build on any non-GET
// registration.
package api

import (
	"fmt"
	"net/http"
	"sort"

	"github.com/go-chi/chi/v5"
	"github.com/go-logr/logr"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"

	tidev1alpha1 "github.com/jsquirrelz/tide/api/v1alpha1"
)

// PlansHandler serves GET /api/v1/plans/{name}. Per D-D2 the Client is the
// dashboard's read-only controller-runtime client (informer-cache-backed);
// no Create/Update/Patch/Delete calls anywhere in this handler.
type PlansHandler struct {
	Client client.Client
	Log    logr.Logger
}

// planTaskCard is the JSON shape for one entry inside planDetail.Tasks.
// Mirrors the frontend ExecutionTaskData shape (ExecutionDAGView.tsx)
// minus the StatusValue coercion the React layer applies to `phase`.
type planTaskCard struct {
	Name       string   `json:"name"`
	Phase      string   `json:"phase"`
	WaveIndex  int      `json:"waveIndex"`
	Attempt    int      `json:"attempt"`
	DependsOn  []string `json:"dependsOn"`
}

// planDetail is the JSON shape returned by Get. The frontend's
// useTasks() hook maps PlanDetail → ExecutionPlanData by coercing
// `phase` to the StatusValue union and folding `activeDispatchWave`
// from the pointer to `undefined` on nil.
type planDetail struct {
	Name               string         `json:"name"`
	Namespace          string         `json:"namespace"`
	Phase              string         `json:"phase"`
	PhaseRef           string         `json:"phaseRef"`
	Tasks              []planTaskCard `json:"tasks"`
	ActiveDispatchWave *int           `json:"activeDispatchWave"`
}

// Get implements GET /api/v1/plans/{name}[?namespace=foo]. Returns the Plan
// + the materialized Task DAG (one card per Task with name/phase/
// waveIndex/attempt/dependsOn). Tasks sorted by (waveIndex ASC, name ASC)
// for deterministic output. 404 with JSON body when the Plan doesn't
// exist; 500 with JSON body on apiserver errors.
func (h *PlansHandler) Get(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	name := chi.URLParam(r, "name")
	namespace := r.URL.Query().Get("namespace")
	if namespace == "" {
		namespace = "default"
	}

	var pl tidev1alpha1.Plan
	if err := h.Client.Get(ctx, client.ObjectKey{Namespace: namespace, Name: name}, &pl); err != nil {
		if apierrors.IsNotFound(err) {
			writeError(w, http.StatusNotFound, fmt.Sprintf("plan %s not found", name))
			return
		}
		h.Log.Error(err, "get plan failed", "name", name, "namespace", namespace)
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("failed to get plan: %s", err.Error()))
		return
	}

	// Tasks — filtered by Spec.PlanRef == pl.Name. On error, the response
	// still returns 200 with the Plan metadata and an empty task list (the
	// partial-result contract; the SSE refresh path will re-fetch).
	var tasks tidev1alpha1.TaskList
	if err := h.Client.List(ctx, &tasks, client.InNamespace(namespace)); err != nil {
		h.Log.Error(err, "list tasks failed", "plan", name)
	}

	// Waves — filtered by Spec.PlanRef == pl.Name. Each Wave's Status.TaskRefs
	// is the source of truth for waveIndex assignment per Task.
	var waves tidev1alpha1.WaveList
	if err := h.Client.List(ctx, &waves, client.InNamespace(namespace)); err != nil {
		h.Log.Error(err, "list waves failed", "plan", name)
	}

	// Build taskName → waveIndex map from the Wave CRDs filtered to this Plan.
	// Tasks not present in any wave's TaskRefs fall through to waveIndex=0.
	waveByTask := make(map[string]int, len(tasks.Items))
	for i := range waves.Items {
		wv := &waves.Items[i]
		if wv.Spec.PlanRef != name {
			continue
		}
		for _, tref := range wv.Status.TaskRefs {
			waveByTask[tref] = wv.Spec.WaveIndex
		}
	}

	cards := make([]planTaskCard, 0, len(tasks.Items))
	for i := range tasks.Items {
		tk := &tasks.Items[i]
		if tk.Spec.PlanRef != name {
			continue
		}
		phase := tk.Status.Phase
		if phase == "" {
			phase = "Pending"
		}
		deps := tk.Spec.DependsOn
		if deps == nil {
			deps = []string{}
		}
		cards = append(cards, planTaskCard{
			Name:      tk.Name,
			Phase:     phase,
			WaveIndex: waveByTask[tk.Name],
			Attempt:   tk.Status.Attempt,
			DependsOn: deps,
		})
	}

	// Sort: (waveIndex ASC, name ASC) — deterministic output for both the
	// dashboard render and downstream test assertions.
	sort.Slice(cards, func(i, j int) bool {
		if cards[i].WaveIndex != cards[j].WaveIndex {
			return cards[i].WaveIndex < cards[j].WaveIndex
		}
		return cards[i].Name < cards[j].Name
	})

	// ActiveDispatchWave: lowest waveIndex among tasks whose phase is
	// "Dispatching" or "Running". nil pointer when none are mid-flight.
	var activeDispatchWave *int
	for _, c := range cards {
		if c.Phase == "Dispatching" || c.Phase == "Running" {
			if activeDispatchWave == nil || c.WaveIndex < *activeDispatchWave {
				w := c.WaveIndex
				activeDispatchWave = &w
			}
		}
	}

	writeJSON(w, http.StatusOK, planDetail{
		Name:               pl.Name,
		Namespace:          pl.Namespace,
		Phase:              pl.Status.Phase,
		PhaseRef:           pl.Spec.PhaseRef,
		Tasks:              cards,
		ActiveDispatchWave: activeDispatchWave,
	})
}
