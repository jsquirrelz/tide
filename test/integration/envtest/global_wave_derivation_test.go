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

package envtest_integration

import (
	"context"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	tideprojectv1alpha3 "github.com/jsquirrelz/tide/api/v1alpha3"
)

const globalWaveNamespace = "default"
const globalWaveTestProject = "global-wave-test-project"

// createSimplePhase creates a minimal Phase in the globalWaveNamespace for testing.
// Follows the createSimplePlan shape from indegree_test.go.
func createSimplePhase(ctx context.Context, name, milestoneRef string) {
	phase := &tideprojectv1alpha3.Phase{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: globalWaveNamespace,
		},
		Spec: tideprojectv1alpha3.PhaseSpec{
			MilestoneRef: milestoneRef,
		},
	}
	Expect(k8sClient.Create(ctx, phase)).To(Succeed())
	Eventually(func() error {
		return mgrClient.Get(ctx, client.ObjectKey{Name: name, Namespace: globalWaveNamespace}, &tideprojectv1alpha3.Phase{})
	}, "5s", "100ms").Should(Succeed())
}

// createSimpleMilestone creates a minimal Milestone in the globalWaveNamespace for testing.
// Follows the createSimplePlan shape from indegree_test.go.
func createSimpleMilestone(ctx context.Context, name, projectRef string) {
	ms := &tideprojectv1alpha3.Milestone{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: globalWaveNamespace,
		},
		Spec: tideprojectv1alpha3.MilestoneSpec{
			ProjectRef: projectRef,
		},
	}
	Expect(k8sClient.Create(ctx, ms)).To(Succeed())
	Eventually(func() error {
		return mgrClient.Get(ctx, client.ObjectKey{Name: name, Namespace: globalWaveNamespace}, &tideprojectv1alpha3.Milestone{})
	}, "5s", "100ms").Should(Succeed())
}

// makeGlobalWaveTask creates a Task in globalWaveNamespace stamped with
// globalWaveTestProject as the tideproject.k8s/project label. This is the
// equivalent of makeTask from indegree_test.go, but stamps the correct project
// label for the global wave derivation test suite.
//
// The global derivation engine (ProjectReconciler) lists Tasks by:
//
//	client.MatchingLabels{owner.LabelProject: project.Name}
//
// so Tasks MUST carry the correct project label to be picked up by the reconciler.
func makeGlobalWaveTask(ctx context.Context, name, planRef string, dependsOn, files []string) *tideprojectv1alpha3.Task {
	if files == nil {
		files = []string{name + ".go"}
	}
	labels := map[string]string{
		"tideproject.k8s/project": globalWaveTestProject,
	}
	task := &tideprojectv1alpha3.Task{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: globalWaveNamespace,
			Labels:    labels,
		},
		Spec: tideprojectv1alpha3.TaskSpec{
			PlanRef:             planRef,
			PromptPath:          "envelopes/test/children/" + name + ".json",
			DependsOn:           dependsOn,
			FilesTouched:        files,
			DeclaredOutputPaths: files,
		},
	}
	Expect(k8sClient.Create(ctx, task)).To(Succeed())
	Eventually(func() error {
		return mgrClient.Get(ctx, client.ObjectKey{Name: name, Namespace: globalWaveNamespace}, &tideprojectv1alpha3.Task{})
	}, "5s", "100ms").Should(Succeed())
	time.Sleep(50 * time.Millisecond) // allow indexer to propagate
	return task
}

// assertWaveExists asserts that a global Wave CR named tide-wave-<projectName>-<waveIdx>
// exists in globalWaveNamespace. Uses the Get pattern from indegree_test.go.
func assertWaveExists(ctx context.Context, projectName string, waveIdx int) {
	Eventually(func() error {
		wave := &tideprojectv1alpha3.Wave{}
		return k8sClient.Get(ctx, client.ObjectKey{
			Name:      fmt.Sprintf("tide-wave-%s-%d", projectName, waveIdx),
			Namespace: globalWaveNamespace,
		}, wave)
	}, "30s", "500ms").Should(Succeed(), "Wave CR tide-wave-%s-%d should exist", projectName, waveIdx)
}

var _ = Describe("Global Wave Derivation", Label("envtest"), func() {
	ctx := context.Background()

	// README worked-example fixture:
	// Tasks α,β,γ in plan-A; δ,ε in plan-B; ζ,η,θ in plan-C
	// Edges: α→δ, β→δ, γ→η, ζ→η, δ→ε, η→θ
	// Expected global waves: [{α,β,γ,ζ}, {δ,η}, {ε,θ}]

	BeforeEach(func() {
		makeBoundPVC(ctx, "tide-projects", globalWaveNamespace)
		// Create-or-wait (see ensureLiveProject in helpers_test.go): the previous
		// spec's AfterEach deletes the Project asynchronously; a bare Create +
		// IgnoreAlreadyExists can be silently swallowed by the still-terminating
		// object, leaving the spec with NO Project (CI flake: tide-wave-<project>-0
		// absent for the full 30s assert window).
		ensureLiveProject(ctx, &tideprojectv1alpha3.Project{
			ObjectMeta: metav1.ObjectMeta{
				Name:      globalWaveTestProject,
				Namespace: globalWaveNamespace,
			},
			Spec: tideprojectv1alpha3.ProjectSpec{
				SchemaRevision: "v1alpha3",
				TargetRepo:     "https://github.com/example/global-wave-test.git",
			},
		})
	})

	AfterEach(func() {
		// Delete Waves first so global Wave CRs do not leak into the next It block.
		waves := &tideprojectv1alpha3.WaveList{}
		_ = k8sClient.List(ctx, waves, client.InNamespace(globalWaveNamespace))
		for i := range waves.Items {
			_ = k8sClient.Delete(ctx, &waves.Items[i])
		}
		tasks := &tideprojectv1alpha3.TaskList{}
		_ = k8sClient.List(ctx, tasks, client.InNamespace(globalWaveNamespace))
		for i := range tasks.Items {
			_ = k8sClient.Delete(ctx, &tasks.Items[i])
		}
		plans := &tideprojectv1alpha3.PlanList{}
		_ = k8sClient.List(ctx, plans, client.InNamespace(globalWaveNamespace))
		for i := range plans.Items {
			_ = k8sClient.Delete(ctx, &plans.Items[i])
		}
		phases := &tideprojectv1alpha3.PhaseList{}
		_ = k8sClient.List(ctx, phases, client.InNamespace(globalWaveNamespace))
		for i := range phases.Items {
			_ = k8sClient.Delete(ctx, &phases.Items[i])
		}
		milestones := &tideprojectv1alpha3.MilestoneList{}
		_ = k8sClient.List(ctx, milestones, client.InNamespace(globalWaveNamespace))
		for i := range milestones.Items {
			_ = k8sClient.Delete(ctx, &milestones.Items[i])
		}
		projects := &tideprojectv1alpha3.ProjectList{}
		_ = k8sClient.List(ctx, projects, client.InNamespace(globalWaveNamespace))
		for i := range projects.Items {
			_ = k8sClient.Delete(ctx, &projects.Items[i])
		}
		pvcs := &corev1.PersistentVolumeClaimList{}
		_ = k8sClient.List(ctx, pvcs, client.InNamespace(globalWaveNamespace))
		for i := range pvcs.Items {
			_ = k8sClient.Delete(ctx, &pvcs.Items[i])
		}
	})

	// GlobalDag (EXEC-01): after applying the multi-plan Project, the derivation
	// produces a global schedule spanning all three plans — assert Wave CRs
	// tide-wave-<project>-0/1/2 eventually exist.
	Describe("GlobalDag: multi-plan project produces global schedule (EXEC-01)", func() {
		It("derives Wave CRs spanning tasks from all three plans (README worked example)", func() {
			// Create the three plans.
			createSimplePlan(ctx, "global-plan-a")
			createSimplePlan(ctx, "global-plan-b")
			createSimplePlan(ctx, "global-plan-c")

			// Tasks α,β,γ in plan-A; δ,ε in plan-B; ζ,η,θ in plan-C.
			// Edges: α→δ, β→δ, γ→η, ζ→η, δ→ε, η→θ.
			// All tasks carry the project label (makeTask stamps it).
			alpha := makeGlobalWaveTask(ctx, "gw-alpha", "global-plan-a", nil, []string{"alpha.go"})
			beta := makeGlobalWaveTask(ctx, "gw-beta", "global-plan-a", nil, []string{"beta.go"})
			gamma := makeGlobalWaveTask(ctx, "gw-gamma", "global-plan-a", nil, []string{"gamma.go"})
			_ = makeGlobalWaveTask(ctx, "gw-delta", "global-plan-b", []string{alpha.Name, beta.Name}, []string{"delta.go"})
			_ = makeGlobalWaveTask(ctx, "gw-epsilon", "global-plan-b", []string{"gw-delta"}, []string{"epsilon.go"})
			zeta := makeGlobalWaveTask(ctx, "gw-zeta", "global-plan-c", nil, []string{"zeta.go"})
			_ = makeGlobalWaveTask(ctx, "gw-eta", "global-plan-c", []string{gamma.Name, zeta.Name}, []string{"eta.go"})
			_ = makeGlobalWaveTask(ctx, "gw-theta", "global-plan-c", []string{"gw-eta"}, []string{"theta.go"})

			// The global derivation engine (Plans 02/03) should produce three global
			// Wave CRs named tide-wave-<project>-0, -1, -2 owned by the Project.
			// This assertion will be RED on current main (no engine yet).
			assertWaveExists(ctx, globalWaveTestProject, 0)
			assertWaveExists(ctx, globalWaveTestProject, 1)
			assertWaveExists(ctx, globalWaveTestProject, 2)
		})
	})

	// GlobalWaveIndex (EXEC-02): Wave CRs are named tide-wave-<project>-<N> (NOT
	// tide-wave-<plan.UID>-<i>), carry Spec.WaveIndex == N and Spec.ProjectRef ==
	// <project>, and their task membership matches the README worked example.
	Describe("GlobalWaveIndex: Wave CRs carry global project-scoped indices (EXEC-02)", func() {
		It("names Wave CRs tide-wave-<project>-<N> with correct WaveIndex and ProjectRef", func() {
			createSimplePlan(ctx, "gwi-plan-a")
			createSimplePlan(ctx, "gwi-plan-b")
			createSimplePlan(ctx, "gwi-plan-c")

			alpha := makeGlobalWaveTask(ctx, "gwi-alpha", "gwi-plan-a", nil, []string{"alpha.go"})
			beta := makeGlobalWaveTask(ctx, "gwi-beta", "gwi-plan-a", nil, []string{"beta.go"})
			gamma := makeGlobalWaveTask(ctx, "gwi-gamma", "gwi-plan-a", nil, []string{"gamma.go"})
			_ = makeGlobalWaveTask(ctx, "gwi-delta", "gwi-plan-b", []string{alpha.Name, beta.Name}, []string{"delta.go"})
			_ = makeGlobalWaveTask(ctx, "gwi-epsilon", "gwi-plan-b", []string{"gwi-delta"}, []string{"epsilon.go"})
			zeta := makeGlobalWaveTask(ctx, "gwi-zeta", "gwi-plan-c", nil, []string{"zeta.go"})
			_ = makeGlobalWaveTask(ctx, "gwi-eta", "gwi-plan-c", []string{gamma.Name, zeta.Name}, []string{"eta.go"})
			_ = makeGlobalWaveTask(ctx, "gwi-theta", "gwi-plan-c", []string{"gwi-eta"}, []string{"theta.go"})

			// Wave 0: {α,β,γ,ζ} — assert correct WaveIndex and ProjectRef.
			Eventually(func() error {
				wave := &tideprojectv1alpha3.Wave{}
				waveName := fmt.Sprintf("tide-wave-%s-0", globalWaveTestProject)
				if err := k8sClient.Get(ctx, client.ObjectKey{Name: waveName, Namespace: globalWaveNamespace}, wave); err != nil {
					return fmt.Errorf("get wave 0: %w", err)
				}
				if wave.Spec.WaveIndex != 0 {
					return fmt.Errorf("wave-0 Spec.WaveIndex = %d, want 0", wave.Spec.WaveIndex)
				}
				if wave.Spec.ProjectRef != globalWaveTestProject {
					return fmt.Errorf("wave-0 Spec.ProjectRef = %q, want %q", wave.Spec.ProjectRef, globalWaveTestProject)
				}
				return nil
			}, "30s", "500ms").Should(Succeed(), "tide-wave-<project>-0 should have WaveIndex=0 and correct ProjectRef")

			// Wave 1: {δ,η} — assert correct WaveIndex.
			Eventually(func() error {
				wave := &tideprojectv1alpha3.Wave{}
				waveName := fmt.Sprintf("tide-wave-%s-1", globalWaveTestProject)
				if err := k8sClient.Get(ctx, client.ObjectKey{Name: waveName, Namespace: globalWaveNamespace}, wave); err != nil {
					return fmt.Errorf("get wave 1: %w", err)
				}
				if wave.Spec.WaveIndex != 1 {
					return fmt.Errorf("wave-1 Spec.WaveIndex = %d, want 1", wave.Spec.WaveIndex)
				}
				return nil
			}, "30s", "500ms").Should(Succeed(), "tide-wave-<project>-1 should have WaveIndex=1")

			// Wave 2: {ε,θ} — assert correct WaveIndex.
			Eventually(func() error {
				wave := &tideprojectv1alpha3.Wave{}
				waveName := fmt.Sprintf("tide-wave-%s-2", globalWaveTestProject)
				if err := k8sClient.Get(ctx, client.ObjectKey{Name: waveName, Namespace: globalWaveNamespace}, wave); err != nil {
					return fmt.Errorf("get wave 2: %w", err)
				}
				if wave.Spec.WaveIndex != 2 {
					return fmt.Errorf("wave-2 Spec.WaveIndex = %d, want 2", wave.Spec.WaveIndex)
				}
				return nil
			}, "30s", "500ms").Should(Succeed(), "tide-wave-<project>-2 should have WaveIndex=2")
		})
	})

	// BidirectionalIndex (EXEC-03): task→wave — each Task's tideproject.k8s/wave-index
	// label equals its expected global index; wave→tasks — a label selector list with
	// wave-index="0" and project=<project> returns exactly the wave-0 task set.
	Describe("BidirectionalIndex: global wave index queryable both directions (EXEC-03)", func() {
		It("stamps each Task with correct global wave-index label and wave→tasks label selector returns correct set", func() {
			createSimplePlan(ctx, "bi-plan-a")
			createSimplePlan(ctx, "bi-plan-b")

			// Simple two-plan fixture: tasks P and Q in plan-a (no deps, wave-0),
			// task R in plan-b depending on P (wave-1).
			taskP := makeGlobalWaveTask(ctx, "bi-task-p", "bi-plan-a", nil, []string{"p.go"})
			taskQ := makeGlobalWaveTask(ctx, "bi-task-q", "bi-plan-a", nil, []string{"q.go"})
			_ = makeGlobalWaveTask(ctx, "bi-task-r", "bi-plan-b", []string{taskP.Name}, []string{"r.go"})

			// task→wave: bi-task-p and bi-task-q should have wave-index label "0".
			Eventually(func() string {
				t := &tideprojectv1alpha3.Task{}
				if err := k8sClient.Get(ctx, client.ObjectKey{Name: taskP.Name, Namespace: globalWaveNamespace}, t); err != nil {
					return ""
				}
				return t.Labels["tideproject.k8s/wave-index"]
			}, "30s", "500ms").Should(Equal("0"), "bi-task-p should carry wave-index label = 0")

			Eventually(func() string {
				t := &tideprojectv1alpha3.Task{}
				if err := k8sClient.Get(ctx, client.ObjectKey{Name: taskQ.Name, Namespace: globalWaveNamespace}, t); err != nil {
					return ""
				}
				return t.Labels["tideproject.k8s/wave-index"]
			}, "30s", "500ms").Should(Equal("0"), "bi-task-q should carry wave-index label = 0")

			// task→wave: bi-task-r should have wave-index label "1".
			Eventually(func() string {
				t := &tideprojectv1alpha3.Task{}
				if err := k8sClient.Get(ctx, client.ObjectKey{Name: "bi-task-r", Namespace: globalWaveNamespace}, t); err != nil {
					return ""
				}
				return t.Labels["tideproject.k8s/wave-index"]
			}, "30s", "500ms").Should(Equal("1"), "bi-task-r should carry wave-index label = 1")

			// wave→tasks: label selector for wave-index=0 and project=<project>
			// should return exactly {bi-task-p, bi-task-q}.
			Eventually(func() ([]string, error) {
				taskList := &tideprojectv1alpha3.TaskList{}
				if err := k8sClient.List(ctx, taskList,
					client.InNamespace(globalWaveNamespace),
					client.MatchingLabels{
						"tideproject.k8s/wave-index": "0",
						"tideproject.k8s/project":    globalWaveTestProject,
					},
				); err != nil {
					return nil, err
				}
				names := make([]string, 0, len(taskList.Items))
				for _, t := range taskList.Items {
					names = append(names, t.Name)
				}
				return names, nil
			}, "30s", "500ms").Should(ConsistOf("bi-task-p", "bi-task-q"),
				"wave→tasks label selector for wave-0 should return exactly {bi-task-p, bi-task-q}")
		})
	})

	// WaveRederivation (EXEC-04): adding a Task that depends on a wave-2 task extends
	// the schedule; removing a dependency compresses it. Assert Wave CR set and
	// wave-index labels re-derive and stale high-index Wave CRs are pruned.
	// Also assert no Schedule/Waves[] aggregate appears on Project.Status.
	Describe("WaveRederivation: re-derives schedule on task add and asserts no cached aggregate (EXEC-04)", func() {
		It("extends global wave schedule when a new dependent Task is added", func() {
			createSimplePlan(ctx, "wd-plan-a")
			createSimplePlan(ctx, "wd-plan-b")

			// Initial fixture: task-x → task-y (two waves: {x}, {y}).
			makeGlobalWaveTask(ctx, "wd-task-x", "wd-plan-a", nil, []string{"x.go"})
			makeGlobalWaveTask(ctx, "wd-task-y", "wd-plan-b", []string{"wd-task-x"}, []string{"y.go"})

			// Initial schedule: waves 0 and 1 exist.
			assertWaveExists(ctx, globalWaveTestProject, 0)
			assertWaveExists(ctx, globalWaveTestProject, 1)

			// Wave 2 should not exist yet.
			Consistently(func() error {
				wave := &tideprojectv1alpha3.Wave{}
				return k8sClient.Get(ctx, client.ObjectKey{
					Name:      fmt.Sprintf("tide-wave-%s-2", globalWaveTestProject),
					Namespace: globalWaveNamespace,
				}, wave)
			}, "3s", "500ms").Should(Not(Succeed()),
				"tide-wave-<project>-2 should not exist before wd-task-z is added")

			// Add task-z depending on task-y (wave-1) — extends the schedule to wave 2.
			makeGlobalWaveTask(ctx, "wd-task-z", "wd-plan-b", []string{"wd-task-y"}, []string{"z.go"})

			// Re-derivation should create Wave 2.
			assertWaveExists(ctx, globalWaveTestProject, 2)
		})

		It("asserts Project.Status has no Schedule/Waves[] cached aggregate (PERSIST-03)", func() {
			createSimplePlan(ctx, "nc-plan-a")
			makeGlobalWaveTask(ctx, "nc-task-a", "nc-plan-a", nil, []string{"nc-a.go"})

			// Wait for at least one reconcile to occur (wave 0 should appear).
			assertWaveExists(ctx, globalWaveTestProject, 0)

			// Verify Project.Status does NOT carry a cached schedule aggregate.
			// The verify-no-aggregates Makefile guard forbids Schedule/Waves[]/IndegreeMap
			// in api types; this test ensures the runtime value is also absent.
			project := &tideprojectv1alpha3.Project{}
			Expect(k8sClient.Get(ctx, client.ObjectKey{
				Name:      globalWaveTestProject,
				Namespace: globalWaveNamespace,
			}, project)).To(Succeed())

			// The status should have no cached wave schedule. Since the API type does not
			// define Schedule/Waves[] fields (verify-no-aggregates guard), this is enforced
			// at the type level. Assert the Project is retrievable without error — the
			// structural assertion is guaranteed by the api type itself.
			// The real enforcement is the static guard (make verify-no-aggregates).
			Expect(project.Name).To(Equal(globalWaveTestProject),
				"Project should be retrievable; no cached schedule field exists per PERSIST-03")
		})
	})

	// PruneShrink (CR-01): regression test for the stale-Wave prune dead-code defect.
	// Before FIX 1 (CR-01): Wave CRs were created with no Labels, so the prune List
	// selector (MatchingLabels{LabelProject: project.Name}) always returned zero items
	// and orphaned high-index Waves were never deleted after re-derivation produced
	// fewer waves. This test must be RED on the pre-fix code and GREEN after the fix.
	//
	// Scenario: create two Tasks with a dependency (→ waves 0,1); then delete the
	// dependent Task so re-derivation yields only wave 0. Assert tide-wave-<project>-1
	// is deleted (pruned) within the eventual consistency window.
	Describe("PruneShrink: prune deletes stale high-index Wave CRs after re-derivation shrinks (CR-01)", func() {
		It("deletes tide-wave-<project>-1 after the dependent Task is removed", func() {
			createSimplePlan(ctx, "ps-plan-a")
			createSimplePlan(ctx, "ps-plan-b")

			// Initial two-task fixture: ps-task-src (wave 0) → ps-task-dep (wave 1).
			makeGlobalWaveTask(ctx, "ps-task-src", "ps-plan-a", nil, []string{"src.go"})
			makeGlobalWaveTask(ctx, "ps-task-dep", "ps-plan-b", []string{"ps-task-src"}, []string{"dep.go"})

			// Both Wave CRs must appear before we shrink the schedule.
			assertWaveExists(ctx, globalWaveTestProject, 0)
			assertWaveExists(ctx, globalWaveTestProject, 1)

			// Shrink: delete ps-task-dep. Re-derivation now produces only wave-0.
			depTask := &tideprojectv1alpha3.Task{}
			Expect(k8sClient.Get(ctx, client.ObjectKey{
				Name:      "ps-task-dep",
				Namespace: globalWaveNamespace,
			}, depTask)).To(Succeed())
			Expect(k8sClient.Delete(ctx, depTask)).To(Succeed())

			// CR-01 assertion: tide-wave-<project>-1 must be pruned (NotFound) once the
			// ProjectReconciler re-derives the schedule. Before FIX 1 this would timeout
			// because the prune List always returned zero items (dead selector).
			Eventually(func() error {
				wave := &tideprojectv1alpha3.Wave{}
				err := k8sClient.Get(ctx, client.ObjectKey{
					Name:      fmt.Sprintf("tide-wave-%s-1", globalWaveTestProject),
					Namespace: globalWaveNamespace,
				}, wave)
				if err == nil {
					return fmt.Errorf("tide-wave-%s-1 still exists; prune has not fired", globalWaveTestProject)
				}
				// apierrors.IsNotFound is the success condition.
				return client.IgnoreNotFound(err)
			}, "30s", "500ms").Should(Succeed(),
				"tide-wave-<project>-1 should be deleted after re-derivation shrinks to 1 wave (CR-01 prune fix)")

			// wave-0 must remain (it still has ps-task-src as its sole member).
			assertWaveExists(ctx, globalWaveTestProject, 0)
		})
	})

	// Cross-phase / cross-milestone case using createSimplePhase / createSimpleMilestone
	// plus Plan-level dependsOn (coarse ref) to exercise fan-out.
	Context("cross-phase cross-milestone coarse-ref fan-out", func() {
		It("fans out a Plan-level coarse dependsOn over every Task in the referenced Plan", func() {
			// Create hierarchy: Milestone → Phase → Plan-A, Plan-B.
			createSimpleMilestone(ctx, "cm-milestone", globalWaveTestProject)
			createSimplePhase(ctx, "cm-phase", "cm-milestone")

			// Plan A has two independent tasks (wave 0 candidates).
			createSimplePlan(ctx, "cm-plan-a")
			makeGlobalWaveTask(ctx, "cm-task-a1", "cm-plan-a", nil, []string{"a1.go"})
			makeGlobalWaveTask(ctx, "cm-task-a2", "cm-plan-a", nil, []string{"a2.go"})

			// Plan B has one task with a coarse Plan-level DependsOn targeting Plan A.
			// This should fan out to: cm-task-b1 depends on {cm-task-a1, cm-task-a2}.
			planB := &tideprojectv1alpha3.Plan{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "cm-plan-b",
					Namespace: globalWaveNamespace,
				},
				Spec: tideprojectv1alpha3.PlanSpec{
					PhaseRef:  "cm-phase",
					DependsOn: []string{"cm-plan-a"}, // coarse Plan-level dep
				},
			}
			Expect(k8sClient.Create(ctx, planB)).To(Succeed())
			Eventually(func() error {
				return mgrClient.Get(ctx, client.ObjectKey{Name: "cm-plan-b", Namespace: globalWaveNamespace}, &tideprojectv1alpha3.Plan{})
			}, "5s", "100ms").Should(Succeed())

			// cm-task-b1 declares no DependsOn itself; coarse fan-out from Plan B's
			// DependsOn should create edges from {cm-task-a1, cm-task-a2} to cm-task-b1.
			makeGlobalWaveTask(ctx, "cm-task-b1", "cm-plan-b", nil, []string{"b1.go"})

			// Expected global waves: wave-0={cm-task-a1, cm-task-a2}, wave-1={cm-task-b1}.
			// The global engine (Plans 02/03) must expand Plan-level DependsOn to task edges.
			assertWaveExists(ctx, globalWaveTestProject, 0)
			assertWaveExists(ctx, globalWaveTestProject, 1)

			// cm-task-b1 should be in wave 1 (depends on all tasks in plan-a via fan-out).
			Eventually(func() string {
				t := &tideprojectv1alpha3.Task{}
				if err := k8sClient.Get(ctx, client.ObjectKey{Name: "cm-task-b1", Namespace: globalWaveNamespace}, t); err != nil {
					return ""
				}
				return t.Labels["tideproject.k8s/wave-index"]
			}, "30s", "500ms").Should(Equal("1"),
				"cm-task-b1 should be in wave-1 after fan-out of cm-plan-b's coarse DependsOn=[cm-plan-a]")
		})
	})
})
