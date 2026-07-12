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

// waves_test.go — unit tests for computeRunningWaves (UI-SPEC C5 semantics).
// Plan 15-06 Task 1: covers grouping, running-wave filter, deterministic sort,
// empty-slice guarantee, and cross-project/missing-label exclusion.
package api

import (
	"context"
	"encoding/json"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrlfake "sigs.k8s.io/controller-runtime/pkg/client/fake"

	tidev1alpha3 "github.com/jsquirrelz/tide/api/v1alpha3"
)

// makeTask is a minimal Task factory for wave test fixtures.
func makeTask(name, projectName, planRef, waveIdx, phase string) *tidev1alpha3.Task {
	labels := map[string]string{
		labelProject:   projectName,
		labelWaveIndex: waveIdx,
	}
	if waveIdx == "" {
		labels = map[string]string{
			labelProject: projectName,
		}
	}
	t := &tidev1alpha3.Task{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "default",
			Labels:    labels,
		},
		Spec: tidev1alpha3.TaskSpec{
			PlanRef:             planRef,
			FilesTouched:        []string{"a.go"},
			DeclaredOutputPaths: []string{"/workspace/a"},
		},
		Status: tidev1alpha3.TaskStatus{
			Phase: phase,
		},
	}
	return t
}

// newWaveTestScheme returns a scheme with v1alpha3 types registered.
func newWaveTestScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	s := runtime.NewScheme()
	if err := tidev1alpha3.AddToScheme(s); err != nil {
		t.Fatalf("AddToScheme: %v", err)
	}
	return s
}

// TestRunningWavesGroupByPlanAndWaveIndex verifies behavior #1:
// Tasks across two plans with wave-index labels group into waves keyed by
// (planName, waveIndex); only waves with >= 1 task in {Running, Dispatching}
// are included (terminal waves excluded by construction).
func TestRunningWavesGroupByPlanAndWaveIndex(t *testing.T) {
	tasks := []runtime.Object{
		// plan-alpha wave 0: 1 Running task → included
		makeTask("alpha-w0-t1", "proj1", "plan-alpha", "0", "Running"),
		makeTask("alpha-w0-t2", "proj1", "plan-alpha", "0", "Pending"),
		// plan-alpha wave 1: all Succeeded → excluded
		makeTask("alpha-w1-t1", "proj1", "plan-alpha", "1", "Succeeded"),
		// plan-beta wave 0: 1 Dispatching task → included
		makeTask("beta-w0-t1", "proj1", "plan-beta", "0", "Dispatching"),
	}

	scheme := newWaveTestScheme(t)
	c := ctrlfake.NewClientBuilder().WithScheme(scheme).WithRuntimeObjects(tasks...).Build()

	snap, err := computeRunningWaves(context.Background(), c, "default", "proj1")
	if err != nil {
		t.Fatalf("computeRunningWaves: %v", err)
	}

	if len(snap.Waves) != 2 {
		t.Fatalf("len(Waves) = %d, want 2; waves=%+v", len(snap.Waves), snap.Waves)
	}

	// Verify each wave is present (exact order tested separately in sort test).
	waveSet := map[string]bool{}
	for _, w := range snap.Waves {
		waveSet[w.PlanName+"/"+string(rune('0'+w.WaveIndex))] = true
	}
	if !waveSet["plan-alpha/0"] {
		t.Error("expected wave plan-alpha/0 in result")
	}
	if !waveSet["plan-beta/0"] {
		t.Error("expected wave plan-beta/0 in result")
	}
	// Ensure the all-Succeeded wave is excluded.
	for _, w := range snap.Waves {
		if w.PlanName == "plan-alpha" && w.WaveIndex == 1 {
			t.Error("wave plan-alpha/1 (all Succeeded) should be excluded from running waves")
		}
	}
}

// TestRunningWavesDeterministicSort verifies behavior #2:
// waves are sorted by plan name asc then wave index asc (numeric, not
// lexicographic); tasks within a wave are sorted by name asc.
func TestRunningWavesDeterministicSort(t *testing.T) {
	tasks := []runtime.Object{
		// plan-z wave 10: should appear AFTER plan-z wave 2 (numeric sort)
		makeTask("z-w10-t1", "proj1", "plan-z", "10", "Running"),
		// plan-z wave 2
		makeTask("z-w2-t2", "proj1", "plan-z", "2", "Running"),
		makeTask("z-w2-t1", "proj1", "plan-z", "2", "Dispatching"),
		// plan-a wave 0: should appear before plan-z
		makeTask("a-w0-t1", "proj1", "plan-a", "0", "Running"),
	}

	scheme := newWaveTestScheme(t)
	c := ctrlfake.NewClientBuilder().WithScheme(scheme).WithRuntimeObjects(tasks...).Build()

	snap, err := computeRunningWaves(context.Background(), c, "default", "proj1")
	if err != nil {
		t.Fatalf("computeRunningWaves: %v", err)
	}

	if len(snap.Waves) != 3 {
		t.Fatalf("len(Waves) = %d, want 3", len(snap.Waves))
	}

	// Verify order: plan-a/0, plan-z/2, plan-z/10 (numeric wave sort)
	if snap.Waves[0].PlanName != "plan-a" || snap.Waves[0].WaveIndex != 0 {
		t.Errorf("Waves[0] = {%s, %d}, want {plan-a, 0}", snap.Waves[0].PlanName, snap.Waves[0].WaveIndex)
	}
	if snap.Waves[1].PlanName != "plan-z" || snap.Waves[1].WaveIndex != 2 {
		t.Errorf("Waves[1] = {%s, %d}, want {plan-z, 2}", snap.Waves[1].PlanName, snap.Waves[1].WaveIndex)
	}
	if snap.Waves[2].PlanName != "plan-z" || snap.Waves[2].WaveIndex != 10 {
		t.Errorf("Waves[2] = {%s, %d}, want {plan-z, 10}", snap.Waves[2].PlanName, snap.Waves[2].WaveIndex)
	}

	// Verify task sort within plan-z/2: t1 before t2
	wave2 := snap.Waves[1]
	if len(wave2.Tasks) != 2 {
		t.Fatalf("wave plan-z/2: len(Tasks)=%d, want 2", len(wave2.Tasks))
	}
	if wave2.Tasks[0].Name != "z-w2-t1" {
		t.Errorf("wave2.Tasks[0].Name=%q, want z-w2-t1", wave2.Tasks[0].Name)
	}
	if wave2.Tasks[1].Name != "z-w2-t2" {
		t.Errorf("wave2.Tasks[1].Name=%q, want z-w2-t2", wave2.Tasks[1].Name)
	}

	// Key invariant: numeric sort — wave 2 before wave 10 (would fail lexicographic "10" < "2")
	if snap.Waves[1].WaveIndex >= snap.Waves[2].WaveIndex {
		t.Errorf("expected wave index 2 before 10; got %d, %d",
			snap.Waves[1].WaveIndex, snap.Waves[2].WaveIndex)
	}
}

// TestRunningWavesEmptyMarshal verifies behavior #3:
// zero running waves marshals to {"waves":[]} — never null.
func TestRunningWavesEmptyMarshal(t *testing.T) {
	// No tasks in the store.
	scheme := newWaveTestScheme(t)
	c := ctrlfake.NewClientBuilder().WithScheme(scheme).Build()

	snap, err := computeRunningWaves(context.Background(), c, "default", "proj1")
	if err != nil {
		t.Fatalf("computeRunningWaves: %v", err)
	}

	if snap.Waves == nil {
		t.Fatal("snap.Waves is nil; want non-nil empty slice ([] not null)")
	}
	if len(snap.Waves) != 0 {
		t.Fatalf("len(snap.Waves) = %d, want 0", len(snap.Waves))
	}

	// Marshal and verify JSON shape is [] not null.
	b, err := json.Marshal(snap)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	s := string(b)
	if !containsStr(s, `"waves":[]`) {
		t.Errorf("expected waves:[] in JSON, got: %s", s)
	}
}

// TestRunningWavesExcludesOtherProjectsAndMissingLabels verifies behavior #4:
// Tasks missing the wave-index label are skipped without error; Tasks from
// other projects (different tideproject.k8s/project label) are excluded.
func TestRunningWavesExcludesOtherProjectsAndMissingLabels(t *testing.T) {
	tasks := []runtime.Object{
		// Our project, valid wave label, running — should appear
		makeTask("target-t1", "proj1", "plan-x", "0", "Running"),
		// Different project — should be excluded
		makeTask("other-t1", "proj2", "plan-x", "0", "Running"),
		// Our project but missing wave-index label — should be skipped
		makeTask("no-label-t1", "proj1", "plan-x", "", "Running"),
	}

	scheme := newWaveTestScheme(t)
	c := ctrlfake.NewClientBuilder().WithScheme(scheme).WithRuntimeObjects(tasks...).Build()

	snap, err := computeRunningWaves(context.Background(), c, "default", "proj1")
	if err != nil {
		t.Fatalf("computeRunningWaves: %v", err)
	}

	if len(snap.Waves) != 1 {
		t.Fatalf("len(Waves) = %d, want 1; waves=%+v", len(snap.Waves), snap.Waves)
	}
	if snap.Waves[0].PlanName != "plan-x" || snap.Waves[0].WaveIndex != 0 {
		t.Errorf("unexpected wave: %+v", snap.Waves[0])
	}
	// Only the target task should be present.
	if len(snap.Waves[0].Tasks) != 1 || snap.Waves[0].Tasks[0].Name != "target-t1" {
		t.Errorf("unexpected tasks: %+v", snap.Waves[0].Tasks)
	}
}

// containsStr is a simple string substring check for wave test assertions.
// (package-level contains() is declared in tasks_test.go — use a distinct name.)
func containsStr(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 || func() bool {
		for i := 0; i <= len(s)-len(sub); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
		return false
	}())
}
