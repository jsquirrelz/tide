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

// execution_dag.go — GET /api/v1/projects/{name}/execution-dag.
//
// Surfaces the global execution DAG for a Project: all Tasks across all
// Milestones with their waveIndex, status, and dependsOn. Used by the
// GlobalExecutionDAGView dashboard component.
//
// DASH-05 zero-mutation contract: this handler is HTTP GET only.
package api

import (
	"fmt"
	"net/http"
	"sort"

	"github.com/go-chi/chi/v5"
	"github.com/go-logr/logr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	tidev1alpha3 "github.com/jsquirrelz/tide/api/v1alpha3"
	"github.com/jsquirrelz/tide/internal/owner"
)

// ExecutionDAGHandler serves GET /api/v1/projects/{name}/execution-dag.
// Per D-D2 the Client is the dashboard's read-only controller-runtime
// client (informer-cache-backed); no Create/Update/Patch/Delete calls
// anywhere in this handler.
type ExecutionDAGHandler struct {
	Client client.Client
	Log    logr.Logger
}

// projectExecutionDAGResponse is the JSON shape for GET /api/v1/projects/{name}/execution-dag.
// Reuses planTaskCard (same field names the frontend's ExecutionTaskData expects).
type projectExecutionDAGResponse struct {
	ProjectName string         `json:"projectName"`
	Tasks       []planTaskCard `json:"tasks"`
}

// Get implements GET /api/v1/projects/{name}/execution-dag[?namespace=foo].
// Returns the project-scoped global execution DAG: all Tasks across all
// Milestones with their waveIndex (from Wave.Status.TaskRefs), status phase,
// attempt count, and dependsOn edges. Tasks sorted by (waveIndex ASC, name ASC).
// 500 with JSON body on apiserver errors.
func (h *ExecutionDAGHandler) Get(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	name := chi.URLParam(r, "name")
	namespace := r.URL.Query().Get("namespace")
	if namespace == "" {
		namespace = "default"
	}

	// List ALL Tasks for this project via the project label.
	// The label is owner.LabelProject, stamped by internal/owner on every
	// Task when the orchestrator creates it.
	var tasks tidev1alpha3.TaskList
	if err := h.Client.List(ctx, &tasks,
		client.InNamespace(namespace),
		client.MatchingLabels{owner.LabelProject: name},
	); err != nil {
		h.Log.Error(err, "list tasks failed", "project", name, "namespace", namespace)
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("failed to list tasks: %s", err.Error()))
		return
	}

	// Waves — build waveByTask map from Wave CRs (same pattern as plans.go:121-127).
	// Wave.Status.TaskRefs is the source of truth for waveIndex assignment per Task.
	var waves tidev1alpha3.WaveList
	if err := h.Client.List(ctx, &waves,
		client.InNamespace(namespace),
		client.MatchingLabels{owner.LabelProject: name},
	); err != nil {
		// Non-fatal: return tasks with waveIndex=0 fallback rather than erroring out.
		// The SSE refresh path will re-fetch when waves become available.
		h.Log.Error(err, "list waves failed", "project", name, "namespace", namespace)
	}
	waveByTask := make(map[string]int, len(tasks.Items))
	for i := range waves.Items {
		wv := &waves.Items[i]
		for _, tref := range wv.Status.TaskRefs {
			waveByTask[tref] = wv.Spec.WaveIndex
		}
	}

	// Build cards. Reuse planTaskCard (same JSON shape the frontend expects).
	cards := make([]planTaskCard, 0, len(tasks.Items))
	for i := range tasks.Items {
		tk := &tasks.Items[i]
		phase := tk.Status.Phase
		if phase == "" {
			phase = tidev1alpha3.LevelPhasePending
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

	writeJSON(w, http.StatusOK, projectExecutionDAGResponse{
		ProjectName: name,
		Tasks:       cards,
	})
}
