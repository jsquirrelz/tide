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

// waves.go — UI-SPEC C5: computeRunningWaves derives the all-running-waves
// aggregate for a project via label-selector queries over Tasks.
//
// Payload contract is locked by 15-UI-SPEC §C3/C5:
//
//   - A wave is "running" iff >= 1 member Task has phase in {Running, Dispatching}.
//   - Waves are sorted by plan name asc, then wave index asc (numeric).
//   - Tasks within a wave are sorted by name asc.
//   - Zero running waves serializes as {"waves":[]} — never null.
//
// Reads use the controller-runtime informer cache (client.Reader) — all
// queries are in-memory; no apiserver round-trip per event. Re-derivation
// on every Task change is O(tasks-in-namespace) and is the accepted T-15-17
// disposition for v1 (documented in the T-15-17 row of the threat model;
// debounce is deferred to v1.x if profiling demands it).
//
// Label vocabulary mirrors cmd/tide/inspect_wave_run.go:37-40 — the same
// constants (labelProject, labelWaveIndex) are used so the CLI's grouping
// semantics and the dashboard's aggregate share a single source of truth.
package api

import (
	"context"
	"fmt"
	"sort"
	"strconv"

	"sigs.k8s.io/controller-runtime/pkg/client"

	tidev1alpha3 "github.com/jsquirrelz/tide/api/v1alpha3"
)

// Label vocabulary — canonical keys stamped by internal/controller/*.
// Also defined in cmd/tide/inspect_wave_run.go; kept here to avoid an
// import cycle (cmd/tide is main; cmd/dashboard/api is a library package).
const (
	labelProject   = "tideproject.k8s/project"
	labelWaveIndex = "tideproject.k8s/wave-index"
)

// RunningWaveTask is the per-task JSON shape in a wave card.
// Field names match the UI-SPEC C3 wire contract exactly.
type RunningWaveTask struct {
	Name   string `json:"name"`
	Status string `json:"status"`
}

// RunningWave is the per-wave JSON shape in a waves.snapshot payload.
// Field names match the UI-SPEC C3 wire contract exactly.
type RunningWave struct {
	PlanName  string            `json:"planName"`
	WaveIndex int               `json:"waveIndex"`
	Tasks     []RunningWaveTask `json:"tasks"`
}

// WavesSnapshot is the envelope type for a waves.snapshot SSE event.
// Waves is always a non-nil slice so it serializes as [] rather than null
// when there are no running waves (UI-SPEC C3: "zero running waves
// serializes as waves: [] never null").
type WavesSnapshot struct {
	Waves []RunningWave `json:"waves"`
}

// waveKey is the grouping key used during aggregate derivation.
type waveKey struct {
	planName  string
	waveIndex int
}

// computeRunningWaves derives the running-waves aggregate for projectName in
// the given namespace. It lists Tasks via the informer-cache-backed reader
// (cheap, in-process), groups by (planRef, wave-index), and retains only
// waves where at least one member Task is in the "Running" or "Dispatching"
// phase. All member tasks of an included wave are returned (running count vs
// total is the client's render concern per UI-SPEC C3).
//
// The returned WavesSnapshot.Waves is always a non-nil slice.
func computeRunningWaves(ctx context.Context, cli client.Reader, ns, projectName string) (WavesSnapshot, error) {
	var taskList tidev1alpha3.TaskList
	if err := cli.List(ctx, &taskList,
		client.InNamespace(ns),
		client.MatchingLabels{labelProject: projectName},
	); err != nil {
		return WavesSnapshot{Waves: []RunningWave{}},
			fmt.Errorf("list tasks for waves aggregate: %w", err)
	}

	// Group tasks by (planRef, wave-index).
	// Tasks missing the wave-index label (not yet stamped by the reconciler)
	// are skipped — they will appear on the next reconcile cycle.
	groups := make(map[waveKey][]tidev1alpha3.Task)
	for i := range taskList.Items {
		tk := &taskList.Items[i]
		waveStr, ok := tk.Labels[labelWaveIndex]
		if !ok {
			continue
		}
		waveIdx, err := strconv.Atoi(waveStr)
		if err != nil {
			// Malformed label — skip rather than error; the next
			// reconcile will re-stamp or fix it.
			continue
		}
		key := waveKey{planName: tk.Spec.PlanRef, waveIndex: waveIdx}
		groups[key] = append(groups[key], *tk)
	}

	// Collect waves where >= 1 member task is Running or Dispatching.
	waves := make([]RunningWave, 0, len(groups))
	for key, tasks := range groups {
		if !anyRunning(tasks) {
			continue
		}
		// Sort tasks by name asc within the wave.
		sort.Slice(tasks, func(i, j int) bool {
			return tasks[i].Name < tasks[j].Name
		})
		runningTasks := make([]RunningWaveTask, 0, len(tasks))
		for _, tk := range tasks {
			phase := tk.Status.Phase
			if phase == "" {
				phase = "Pending"
			}
			runningTasks = append(runningTasks, RunningWaveTask{
				Name:   tk.Name,
				Status: phase,
			})
		}
		waves = append(waves, RunningWave{
			PlanName:  key.planName,
			WaveIndex: key.waveIndex,
			Tasks:     runningTasks,
		})
	}

	// Deterministic ordering: plan name asc, then wave index asc (numeric).
	sort.Slice(waves, func(i, j int) bool {
		if waves[i].PlanName != waves[j].PlanName {
			return waves[i].PlanName < waves[j].PlanName
		}
		return waves[i].WaveIndex < waves[j].WaveIndex
	})

	// Ensure the slice is non-nil so it serializes as [] not null.
	if waves == nil {
		waves = []RunningWave{}
	}

	return WavesSnapshot{Waves: waves}, nil
}

// anyRunning reports whether any task in the slice has phase Running or
// Dispatching — the "is this wave running" predicate per UI-SPEC C3.
func anyRunning(tasks []tidev1alpha3.Task) bool {
	for i := range tasks {
		if isRunningPhase(tasks[i].Status.Phase) {
			return true
		}
	}
	return false
}

// isRunningPhase reports whether the given phase is one of the phases that
// make a wave "running" per UI-SPEC C3 semantics.
func isRunningPhase(phase string) bool {
	return phase == "Running" || phase == "Dispatching"
}
