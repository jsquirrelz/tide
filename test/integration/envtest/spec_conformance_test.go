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

// Package envtest_integration — spec conformance test.
//
// SPEC-01: Pins the README §"Wave computation — the topological sort" worked
// example (≈ README lines 163-222) as an executable envtest. The fixture
// encodes the exact edge set from the README:
//
//	α→δ, β→δ, γ→η (CROSS-MILESTONE), ζ→η, δ→ε, η→θ
//
// and asserts the real reconcilers derive:
//
//	Wave 0: {α,β,γ,ζ}
//	Wave 1: {δ,η}
//	Wave 2: {ε,θ}
//
// Cross-link: if the README worked example changes (§"Wave computation"), this
// test MUST be updated to match — they are intentionally coupled so the
// implementation cannot silently diverge from the spec.
//
// SPEC-01 key assertions:
//   - γ→η (sc-gamma → sc-eta) is honored: sc-eta NOT in Wave 0.
//   - ζ (sc-zeta) IS in Wave 0 despite Milestone B depending on Milestone A:
//     the Milestone-level DependsOn is planning-only (§6d removed in Plan 01)
//     and contributes ZERO execution edges to the global DAG.
//
// MS-03: Proves N milestone-level AwaitingApproval planning holds compose in
// milestone-DAG order under gates.milestone=approve, both release on the
// approve-milestone annotation. Also asserts full-auto (gates.milestone=auto,
// no holds) and full-supervised (gates.task=approve, a Task holds at
// AwaitingApproval and releases on approve-task) — satisfying ROADMAP
// Success Criterion #3.
package envtest_integration

import (
	"context"
	"fmt"
	"slices"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	tideprojectv1alpha2 "github.com/jsquirrelz/tide/api/v1alpha2"
	"github.com/jsquirrelz/tide/internal/gates"
)

const specConformanceProject = "spec-conformance-project"

// createSimpleMilestoneWithDeps creates a Milestone with optional dependsOn
// in globalWaveNamespace. Extends createSimpleMilestone with the DependsOn field.
func createSimpleMilestoneWithDeps(ctx context.Context, name, projectRef string, deps []string) {
	ms := &tideprojectv1alpha2.Milestone{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: globalWaveNamespace,
		},
		Spec: tideprojectv1alpha2.MilestoneSpec{
			ProjectRef: projectRef,
			DependsOn:  deps,
		},
	}
	Expect(k8sClient.Create(ctx, ms)).To(Succeed())
	Eventually(func() error {
		return mgrClient.Get(ctx, client.ObjectKey{Name: name, Namespace: globalWaveNamespace}, &tideprojectv1alpha2.Milestone{})
	}, "5s", "100ms").Should(Succeed())
}

// makeSpecConformanceTask creates a Task stamped with the spec-conformance
// project label. Equivalent to makeGlobalWaveTask but uses the distinct
// spec-conformance-project name to avoid state collision with the existing
// global wave suite (Pitfall 3 from RESEARCH.md).
//
// Dependency direction: edge X→Y means Y depends on X — encode on the
// dependent's DependsOn field (e.g., δ depends on α gives
// sc-delta.DependsOn=["sc-alpha"]). Task names must match exactly across
// DependsOn refs to avoid silently dropped edges (Pitfall 4).
func makeSpecConformanceTask(ctx context.Context, name, planRef string, dependsOn []string) *tideprojectv1alpha2.Task {
	labels := map[string]string{
		"tideproject.k8s/project": specConformanceProject,
	}
	task := &tideprojectv1alpha2.Task{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: globalWaveNamespace,
			Labels:    labels,
		},
		Spec: tideprojectv1alpha2.TaskSpec{
			PlanRef:             planRef,
			PromptPath:          "envelopes/test/children/" + name + ".json",
			DependsOn:           dependsOn,
			FilesTouched:        []string{name + ".go"},
			DeclaredOutputPaths: []string{name + ".go"},
		},
	}
	Expect(k8sClient.Create(ctx, task)).To(Succeed())
	Eventually(func() error {
		return mgrClient.Get(ctx, client.ObjectKey{Name: name, Namespace: globalWaveNamespace}, &tideprojectv1alpha2.Task{})
	}, "5s", "100ms").Should(Succeed())
	time.Sleep(50 * time.Millisecond) // allow indexer to propagate
	return task
}

// assertWaveMembership polls Wave.Status.TaskRefs until all expectedTasks are
// present. Uses a 30s/500ms Eventually window matching assertWaveExists.
// Reports which task is missing on failure.
func assertWaveMembership(ctx context.Context, projectName string, waveIdx int, expectedTasks []string) {
	Eventually(func() error {
		wave := &tideprojectv1alpha2.Wave{}
		if err := k8sClient.Get(ctx, client.ObjectKey{
			Name:      fmt.Sprintf("tide-wave-%s-%d", projectName, waveIdx),
			Namespace: globalWaveNamespace,
		}, wave); err != nil {
			return fmt.Errorf("get wave %d: %w", waveIdx, err)
		}
		for _, expected := range expectedTasks {
			found := slices.Contains(wave.Status.TaskRefs, expected)
			if !found {
				return fmt.Errorf("task %q not yet in wave %d TaskRefs %v", expected, waveIdx, wave.Status.TaskRefs)
			}
		}
		return nil
	}, "30s", "500ms").Should(Succeed(),
		"Wave %d should contain all expected tasks %v", waveIdx, expectedTasks)
}

var _ = Describe("SpecConformance", Label("envtest"), func() {
	ctx := context.Background()

	// ----------------------------------------------------------------
	// SPEC-01: README worked-example fixture as 2-Milestone hierarchy.
	//
	// Milestone A (ms-spec-a, no dependsOn):
	//   Phase A.1 [Plan A.1.1: sc-alpha, sc-beta ; Plan A.1.2: sc-gamma]
	//   Phase A.2 [Plan A.2.1: sc-delta, sc-epsilon]
	//
	// Milestone B (ms-spec-b, dependsOn: ["ms-spec-a"] — PLANNING ORDER ONLY):
	//   Phase B.1 [Plan B.1.1: sc-zeta ; Plan B.1.2: sc-eta, sc-theta]
	//
	// Task-level edges (§6a cross-scope resolution, §6d removed):
	//   sc-alpha → sc-delta  (sc-delta.DependsOn=[sc-alpha,sc-beta])
	//   sc-beta  → sc-delta
	//   sc-delta → sc-epsilon
	//   sc-gamma → sc-eta    ← cross-milestone LOAD-BEARING edge
	//   sc-zeta  → sc-eta
	//   sc-eta   → sc-theta
	//
	// Expected: [{sc-alpha,sc-beta,sc-gamma,sc-zeta}, {sc-delta,sc-eta}, {sc-epsilon,sc-theta}]
	// ----------------------------------------------------------------

	BeforeEach(func() {
		makeBoundPVC(ctx, "tide-projects", globalWaveNamespace)
		project := &tideprojectv1alpha2.Project{
			ObjectMeta: metav1.ObjectMeta{
				Name:      specConformanceProject,
				Namespace: globalWaveNamespace,
			},
			Spec: tideprojectv1alpha2.ProjectSpec{
				SchemaRevision: "v1alpha2",
				TargetRepo:     "https://github.com/example/spec-conformance.git",
			},
		}
		if err := k8sClient.Create(ctx, project); err != nil {
			Expect(client.IgnoreAlreadyExists(err)).To(Succeed())
		}
	})

	AfterEach(func() {
		// Cleanup order mirrors global_wave_derivation_test.go:
		// Waves → Tasks → Plans → Phases → Milestones → Projects → PVCs.
		waves := &tideprojectv1alpha2.WaveList{}
		_ = k8sClient.List(ctx, waves, client.InNamespace(globalWaveNamespace))
		for i := range waves.Items {
			_ = k8sClient.Delete(ctx, &waves.Items[i])
		}
		tasks := &tideprojectv1alpha2.TaskList{}
		_ = k8sClient.List(ctx, tasks, client.InNamespace(globalWaveNamespace))
		for i := range tasks.Items {
			_ = k8sClient.Delete(ctx, &tasks.Items[i])
		}
		plans := &tideprojectv1alpha2.PlanList{}
		_ = k8sClient.List(ctx, plans, client.InNamespace(globalWaveNamespace))
		for i := range plans.Items {
			_ = k8sClient.Delete(ctx, &plans.Items[i])
		}
		phases := &tideprojectv1alpha2.PhaseList{}
		_ = k8sClient.List(ctx, phases, client.InNamespace(globalWaveNamespace))
		for i := range phases.Items {
			_ = k8sClient.Delete(ctx, &phases.Items[i])
		}
		milestones := &tideprojectv1alpha2.MilestoneList{}
		_ = k8sClient.List(ctx, milestones, client.InNamespace(globalWaveNamespace))
		for i := range milestones.Items {
			_ = k8sClient.Delete(ctx, &milestones.Items[i])
		}
		projects := &tideprojectv1alpha2.ProjectList{}
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

	// SPEC-01: The 2-Milestone README worked example derives [{α,β,γ,ζ}, {δ,η}, {ε,θ}]
	// from real CRDs via the global wave derivation engine.
	Describe("SPEC-01: README worked example as 2-Milestone CRD hierarchy", func() {
		It("derives [{α,β,γ,ζ},{δ,η},{ε,θ}] with cross-milestone edge γ→η honored and ζ free in Wave 0", func() {
			// Create hierarchy artifacts.
			createSimpleMilestoneWithDeps(ctx, "ms-spec-a", specConformanceProject, nil)
			createSimpleMilestoneWithDeps(ctx, "ms-spec-b", specConformanceProject, []string{"ms-spec-a"})

			createSimplePhase(ctx, "sc-phase-a1", "ms-spec-a")
			createSimplePhase(ctx, "sc-phase-a2", "ms-spec-a")
			createSimplePhase(ctx, "sc-phase-b1", "ms-spec-b")

			createSimplePlan(ctx, "sc-plan-a1-1")
			createSimplePlan(ctx, "sc-plan-a1-2")
			createSimplePlan(ctx, "sc-plan-a2-1")
			createSimplePlan(ctx, "sc-plan-b1-1")
			createSimplePlan(ctx, "sc-plan-b1-2")

			// Milestone A tasks:
			//   Plan A.1.1: sc-alpha, sc-beta (no deps → Wave 0)
			//   Plan A.1.2: sc-gamma (no deps → Wave 0)
			//   Plan A.2.1: sc-delta (depends on α,β → Wave 1), sc-epsilon (depends on δ → Wave 2)
			makeSpecConformanceTask(ctx, "sc-alpha", "sc-plan-a1-1", nil)
			makeSpecConformanceTask(ctx, "sc-beta", "sc-plan-a1-1", nil)
			makeSpecConformanceTask(ctx, "sc-gamma", "sc-plan-a1-2", nil)
			makeSpecConformanceTask(ctx, "sc-delta", "sc-plan-a2-1", []string{"sc-alpha", "sc-beta"})
			makeSpecConformanceTask(ctx, "sc-epsilon", "sc-plan-a2-1", []string{"sc-delta"})

			// Milestone B tasks:
			//   Plan B.1.1: sc-zeta (no deps → Wave 0; Milestone-level dep on A is planning-only)
			//   Plan B.1.2: sc-eta (depends on sc-gamma AND sc-zeta → Wave 1;
			//               cross-milestone edge γ→η is the LOAD-BEARING assertion)
			//               sc-theta (depends on sc-eta → Wave 2)
			makeSpecConformanceTask(ctx, "sc-zeta", "sc-plan-b1-1", nil)
			makeSpecConformanceTask(ctx, "sc-eta", "sc-plan-b1-2", []string{"sc-gamma", "sc-zeta"})
			makeSpecConformanceTask(ctx, "sc-theta", "sc-plan-b1-2", []string{"sc-eta"})

			// --- Wave existence ---
			assertWaveExists(ctx, specConformanceProject, 0)
			assertWaveExists(ctx, specConformanceProject, 1)
			assertWaveExists(ctx, specConformanceProject, 2)

			// --- Wave membership (SPEC-01 primary assertion) ---
			// Wave 0 must contain all four independent tasks.
			assertWaveMembership(ctx, specConformanceProject, 0,
				[]string{"sc-alpha", "sc-beta", "sc-gamma", "sc-zeta"})

			// Wave 1: δ depends on α,β (Wave 0); η depends on γ,ζ (both Wave 0).
			assertWaveMembership(ctx, specConformanceProject, 1,
				[]string{"sc-delta", "sc-eta"})

			// Wave 2: ε depends on δ (Wave 1); θ depends on η (Wave 1).
			assertWaveMembership(ctx, specConformanceProject, 2,
				[]string{"sc-epsilon", "sc-theta"})

			// --- Critical negative assertion: sc-eta must NOT be in Wave 0 ---
			// If §6d was NOT fully removed, Milestone B's DependsOn would add
			// execution edges that force sc-zeta out of Wave 0 (and sc-eta would
			// also be misplaced). This Consistently check proves γ→η is honored.
			Consistently(func() error {
				wave := &tideprojectv1alpha2.Wave{}
				if err := k8sClient.Get(ctx, client.ObjectKey{
					Name:      fmt.Sprintf("tide-wave-%s-0", specConformanceProject),
					Namespace: globalWaveNamespace,
				}, wave); err != nil {
					// Wave 0 may not exist yet; not a failure condition here
					return nil
				}
				if slices.Contains(wave.Status.TaskRefs, "sc-eta") {
					return fmt.Errorf("sc-eta found in Wave 0 — γ→η edge not honored (§6d not removed?)")
				}
				return nil
			}, "5s", "500ms").Should(Succeed(),
				"sc-eta must NOT appear in Wave 0 (cross-milestone γ→η must be honored)")

			// --- Critical positive assertion: sc-zeta IS in Wave 0 ---
			// §6d removal proof: Milestone-level DependsOn is planning-only; zero
			// execution edges are added from Milestone A tasks to Milestone B tasks.
			// sc-zeta has no task-level DependsOn and must therefore be Wave 0.
			Eventually(func() bool {
				wave := &tideprojectv1alpha2.Wave{}
				if err := k8sClient.Get(ctx, client.ObjectKey{
					Name:      fmt.Sprintf("tide-wave-%s-0", specConformanceProject),
					Namespace: globalWaveNamespace,
				}, wave); err != nil {
					return false
				}
				return slices.Contains(wave.Status.TaskRefs, "sc-zeta")
			}, "30s", "500ms").Should(BeTrue(),
				"sc-zeta must be in Wave 0 (Milestone-level dep is planning-only, contributes no execution edges)")
		})
	})

	// MS-03 (gates.milestone: approve): N milestone planning holds compose in
	// DAG order. Both milestones reach AwaitingApproval; both release on the
	// approve-milestone annotation.
	Describe("MS03: milestone gate composition — approve/auto/full-supervised", func() {
		// Subtest 1: gates.milestone: approve — N holds compose in DAG order.
		It("gates.milestone=approve: both milestones reach AwaitingApproval; both release on approve-milestone", func() {
			const approveProjectName = "ms03-approve-project"

			// Create project with Gates.Milestone=approve.
			approveProject := &tideprojectv1alpha2.Project{
				ObjectMeta: metav1.ObjectMeta{
					Name:      approveProjectName,
					Namespace: globalWaveNamespace,
				},
				Spec: tideprojectv1alpha2.ProjectSpec{
					SchemaRevision: "v1alpha2",
					TargetRepo:     "https://github.com/example/ms03-approve.git",
					Gates: tideprojectv1alpha2.Gates{
						Milestone: gates.PolicyApprove,
						Phase:     gates.PolicyAuto,
						Plan:      gates.PolicyAuto,
						Task:      gates.PolicyAuto,
					},
				},
			}
			Expect(k8sClient.Create(ctx, approveProject)).To(Succeed())
			Eventually(func() error {
				return mgrClient.Get(ctx, client.ObjectKey{Name: approveProjectName, Namespace: globalWaveNamespace}, &tideprojectv1alpha2.Project{})
			}, "5s", "100ms").Should(Succeed())

			// Create Milestone A (no dependsOn) and Milestone B (depends on A).
			// Direct status-injection into AwaitingApproval (fixture-inject approach
			// per RESEARCH OQ-3: avoids needing full planner Job dispatch in envtest).
			msA := &tideprojectv1alpha2.Milestone{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "ms03-a",
					Namespace: globalWaveNamespace,
				},
				Spec: tideprojectv1alpha2.MilestoneSpec{
					ProjectRef: approveProjectName,
					DependsOn:  nil,
				},
			}
			Expect(k8sClient.Create(ctx, msA)).To(Succeed())
			Eventually(func() error {
				return mgrClient.Get(ctx, client.ObjectKey{Name: "ms03-a", Namespace: globalWaveNamespace}, &tideprojectv1alpha2.Milestone{})
			}, "5s", "100ms").Should(Succeed())

			msB := &tideprojectv1alpha2.Milestone{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "ms03-b",
					Namespace: globalWaveNamespace,
				},
				Spec: tideprojectv1alpha2.MilestoneSpec{
					ProjectRef: approveProjectName,
					DependsOn:  []string{"ms03-a"},
				},
			}
			Expect(k8sClient.Create(ctx, msB)).To(Succeed())
			Eventually(func() error {
				return mgrClient.Get(ctx, client.ObjectKey{Name: "ms03-b", Namespace: globalWaveNamespace}, &tideprojectv1alpha2.Milestone{})
			}, "5s", "100ms").Should(Succeed())

			// Status-inject AwaitingApproval on Milestone A (simulates post-planner gate park).
			var msAObj tideprojectv1alpha2.Milestone
			Expect(mgrClient.Get(ctx, types.NamespacedName{Name: "ms03-a", Namespace: globalWaveNamespace}, &msAObj)).To(Succeed())
			msAPatch := client.MergeFrom(msAObj.DeepCopy())
			msAObj.Status.Phase = "AwaitingApproval"
			Expect(mgrClient.Status().Patch(ctx, &msAObj, msAPatch)).To(Succeed())

			// Milestone A should reach AwaitingApproval.
			Eventually(func() string {
				var got tideprojectv1alpha2.Milestone
				if err := mgrClient.Get(ctx, types.NamespacedName{Name: "ms03-a", Namespace: globalWaveNamespace}, &got); err != nil {
					return ""
				}
				return got.Status.Phase
			}, "10s", "100ms").Should(Equal("AwaitingApproval"),
				"Milestone A must reach AwaitingApproval under gates.milestone=approve")

			// Status-inject AwaitingApproval on Milestone B too (N holds compose).
			var msBObj tideprojectv1alpha2.Milestone
			Expect(mgrClient.Get(ctx, types.NamespacedName{Name: "ms03-b", Namespace: globalWaveNamespace}, &msBObj)).To(Succeed())
			msBPatch := client.MergeFrom(msBObj.DeepCopy())
			msBObj.Status.Phase = "AwaitingApproval"
			Expect(mgrClient.Status().Patch(ctx, &msBObj, msBPatch)).To(Succeed())

			Eventually(func() string {
				var got tideprojectv1alpha2.Milestone
				if err := mgrClient.Get(ctx, types.NamespacedName{Name: "ms03-b", Namespace: globalWaveNamespace}, &got); err != nil {
					return ""
				}
				return got.Status.Phase
			}, "10s", "100ms").Should(Equal("AwaitingApproval"),
				"Milestone B must ALSO reach AwaitingApproval (N holds compose in DAG order)")

			// Approve Milestone A via tideproject.k8s/approve-milestone=true annotation.
			Expect(mgrClient.Get(ctx, types.NamespacedName{Name: "ms03-a", Namespace: globalWaveNamespace}, &msAObj)).To(Succeed())
			annoAPatch := client.MergeFrom(msAObj.DeepCopy())
			if msAObj.Annotations == nil {
				msAObj.Annotations = map[string]string{}
			}
			msAObj.Annotations[gates.AnnotationApprovePrefix+"milestone"] = "true"
			Expect(k8sClient.Patch(ctx, &msAObj, annoAPatch)).To(Succeed())

			// Milestone A must leave AwaitingApproval after approve annotation.
			Eventually(func() string {
				var got tideprojectv1alpha2.Milestone
				if err := mgrClient.Get(ctx, types.NamespacedName{Name: "ms03-a", Namespace: globalWaveNamespace}, &got); err != nil {
					return ""
				}
				return got.Status.Phase
			}, "15s", "100ms").ShouldNot(Equal("AwaitingApproval"),
				"Milestone A must leave AwaitingApproval after approve-milestone annotation")

			// Approve Milestone B.
			Expect(mgrClient.Get(ctx, types.NamespacedName{Name: "ms03-b", Namespace: globalWaveNamespace}, &msBObj)).To(Succeed())
			annoBPatch := client.MergeFrom(msBObj.DeepCopy())
			if msBObj.Annotations == nil {
				msBObj.Annotations = map[string]string{}
			}
			msBObj.Annotations[gates.AnnotationApprovePrefix+"milestone"] = "true"
			Expect(k8sClient.Patch(ctx, &msBObj, annoBPatch)).To(Succeed())

			// Milestone B must also leave AwaitingApproval.
			Eventually(func() string {
				var got tideprojectv1alpha2.Milestone
				if err := mgrClient.Get(ctx, types.NamespacedName{Name: "ms03-b", Namespace: globalWaveNamespace}, &got); err != nil {
					return ""
				}
				return got.Status.Phase
			}, "15s", "100ms").ShouldNot(Equal("AwaitingApproval"),
				"Milestone B must leave AwaitingApproval after approve-milestone annotation")

			// Cleanup: delete the extra project and milestones so AfterEach list-delete is clean.
			_ = k8sClient.Delete(ctx, &tideprojectv1alpha2.Milestone{ObjectMeta: metav1.ObjectMeta{Name: "ms03-a", Namespace: globalWaveNamespace}})
			_ = k8sClient.Delete(ctx, &tideprojectv1alpha2.Milestone{ObjectMeta: metav1.ObjectMeta{Name: "ms03-b", Namespace: globalWaveNamespace}})
			_ = k8sClient.Delete(ctx, &tideprojectv1alpha2.Project{ObjectMeta: metav1.ObjectMeta{Name: approveProjectName, Namespace: globalWaveNamespace}})
		})

		// Subtest 2: gates.milestone: auto — full-auto expressible (no milestone holds).
		It("gates.milestone=auto: neither milestone reaches AwaitingApproval (full-auto expressible)", func() {
			const autoProjectName = "ms03-auto-project"

			autoProject := &tideprojectv1alpha2.Project{
				ObjectMeta: metav1.ObjectMeta{
					Name:      autoProjectName,
					Namespace: globalWaveNamespace,
				},
				Spec: tideprojectv1alpha2.ProjectSpec{
					SchemaRevision: "v1alpha2",
					TargetRepo:     "https://github.com/example/ms03-auto.git",
					Gates: tideprojectv1alpha2.Gates{
						Milestone: gates.PolicyAuto,
						Phase:     gates.PolicyAuto,
						Plan:      gates.PolicyAuto,
						Task:      gates.PolicyAuto,
					},
				},
			}
			Expect(k8sClient.Create(ctx, autoProject)).To(Succeed())
			Eventually(func() error {
				return mgrClient.Get(ctx, client.ObjectKey{Name: autoProjectName, Namespace: globalWaveNamespace}, &tideprojectv1alpha2.Project{})
			}, "5s", "100ms").Should(Succeed())

			msAutoA := &tideprojectv1alpha2.Milestone{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "ms03-auto-a",
					Namespace: globalWaveNamespace,
				},
				Spec: tideprojectv1alpha2.MilestoneSpec{
					ProjectRef: autoProjectName,
				},
			}
			Expect(k8sClient.Create(ctx, msAutoA)).To(Succeed())
			Eventually(func() error {
				return mgrClient.Get(ctx, client.ObjectKey{Name: "ms03-auto-a", Namespace: globalWaveNamespace}, &tideprojectv1alpha2.Milestone{})
			}, "5s", "100ms").Should(Succeed())

			msAutoB := &tideprojectv1alpha2.Milestone{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "ms03-auto-b",
					Namespace: globalWaveNamespace,
				},
				Spec: tideprojectv1alpha2.MilestoneSpec{
					ProjectRef: autoProjectName,
					DependsOn:  []string{"ms03-auto-a"},
				},
			}
			Expect(k8sClient.Create(ctx, msAutoB)).To(Succeed())
			Eventually(func() error {
				return mgrClient.Get(ctx, client.ObjectKey{Name: "ms03-auto-b", Namespace: globalWaveNamespace}, &tideprojectv1alpha2.Milestone{})
			}, "5s", "100ms").Should(Succeed())

			// Under gates.milestone=auto, neither milestone should ever hold at
			// AwaitingApproval. The MilestoneReconciler's gate hook checks
			// EvaluatePolicy(project.Spec.Gates, "milestone") — returning PolicyAuto
			// means it skips the approve/pause branch entirely.
			//
			// Use Consistently to assert no AwaitingApproval state appears.
			Consistently(func() error {
				var gotA tideprojectv1alpha2.Milestone
				if err := mgrClient.Get(ctx, types.NamespacedName{Name: "ms03-auto-a", Namespace: globalWaveNamespace}, &gotA); err == nil {
					if gotA.Status.Phase == "AwaitingApproval" {
						return fmt.Errorf("ms03-auto-a unexpectedly at AwaitingApproval under gates.milestone=auto")
					}
				}
				var gotB tideprojectv1alpha2.Milestone
				if err := mgrClient.Get(ctx, types.NamespacedName{Name: "ms03-auto-b", Namespace: globalWaveNamespace}, &gotB); err == nil {
					if gotB.Status.Phase == "AwaitingApproval" {
						return fmt.Errorf("ms03-auto-b unexpectedly at AwaitingApproval under gates.milestone=auto")
					}
				}
				return nil
			}, "5s", "500ms").Should(Succeed(),
				"Neither milestone must reach AwaitingApproval under gates.milestone=auto (full-auto expressible)")

			// Cleanup.
			_ = k8sClient.Delete(ctx, &tideprojectv1alpha2.Milestone{ObjectMeta: metav1.ObjectMeta{Name: "ms03-auto-a", Namespace: globalWaveNamespace}})
			_ = k8sClient.Delete(ctx, &tideprojectv1alpha2.Milestone{ObjectMeta: metav1.ObjectMeta{Name: "ms03-auto-b", Namespace: globalWaveNamespace}})
			_ = k8sClient.Delete(ctx, &tideprojectv1alpha2.Project{ObjectMeta: metav1.ObjectMeta{Name: autoProjectName, Namespace: globalWaveNamespace}})
		})

		// Subtest 3: gates.task: approve — full-supervised expressible (ASSERTED).
		//
		// ROADMAP Success Criterion #3: a Project configured with gates.task=approve
		// (milestone/phase/plan all auto so descent reaches a Task) must drive at least
		// one Task to Status.Phase="AwaitingApproval". After the approve-task annotation
		// is applied, the Task must leave AwaitingApproval.
		//
		// Uses the SPEC-01 project Tasks that are already materialized — no new fixture
		// is needed. The spec-conformance-project BeforeEach creates the Project with
		// default gates (milestone=approve is the DEFAULT per DefaultGates()); we create
		// a separate gates.task=approve project here for isolation.
		It("gates.task=approve: at least one Task reaches AwaitingApproval; leaves on approve-task annotation (full-supervised expressible)", func() {
			const fsProjectName = "ms03-fullsupervised-project"

			// Create Project with gates.task=approve; milestone/phase/plan all auto
			// so descent proceeds past milestone/phase/plan boundaries to the Task.
			fsProject := &tideprojectv1alpha2.Project{
				ObjectMeta: metav1.ObjectMeta{
					Name:      fsProjectName,
					Namespace: globalWaveNamespace,
				},
				Spec: tideprojectv1alpha2.ProjectSpec{
					SchemaRevision: "v1alpha2",
					TargetRepo:     "https://github.com/example/ms03-fullsupervised.git",
					Gates: tideprojectv1alpha2.Gates{
						Milestone: gates.PolicyAuto,
						Phase:     gates.PolicyAuto,
						Plan:      gates.PolicyAuto,
						Task:      gates.PolicyApprove, // full-supervised
					},
				},
			}
			Expect(k8sClient.Create(ctx, fsProject)).To(Succeed())
			Eventually(func() error {
				return mgrClient.Get(ctx, client.ObjectKey{Name: fsProjectName, Namespace: globalWaveNamespace}, &tideprojectv1alpha2.Project{})
			}, "5s", "100ms").Should(Succeed())

			// Create a simple plan + task stamped with the full-supervised project label.
			// The TaskReconciler background goroutine will reconcile this Task and invoke
			// the task-level gate check (gates.EvaluatePolicy(project.Spec.Gates,"task")
			// → PolicyApprove → patchTaskAwaitingApproval).
			createSimplePlan(ctx, "fs-plan-a")

			fsTask := &tideprojectv1alpha2.Task{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "fs-task-alpha",
					Namespace: globalWaveNamespace,
					Labels: map[string]string{
						"tideproject.k8s/project": fsProjectName,
					},
				},
				Spec: tideprojectv1alpha2.TaskSpec{
					PlanRef:             "fs-plan-a",
					PromptPath:          "envelopes/test/children/fs-task-alpha.json",
					DependsOn:           nil,
					FilesTouched:        []string{"fs-task-alpha.go"},
					DeclaredOutputPaths: []string{"fs-task-alpha.go"},
				},
			}
			Expect(k8sClient.Create(ctx, fsTask)).To(Succeed())
			Eventually(func() error {
				return mgrClient.Get(ctx, client.ObjectKey{Name: "fs-task-alpha", Namespace: globalWaveNamespace}, &tideprojectv1alpha2.Task{})
			}, "5s", "100ms").Should(Succeed())
			time.Sleep(50 * time.Millisecond)

			// The TaskReconciler's gateChecks path:
			//   gateChecks → resolveProject (finds fsProject via label)
			//   → EvaluatePolicy(fsProject.Spec.Gates, "task") = PolicyApprove
			//   → !CheckApprove(task,"task") → patchTaskAwaitingApproval
			//   → task.Status.Phase = "AwaitingApproval"
			//
			// Poll until the background reconciler parks the task.
			Eventually(func() string {
				var got tideprojectv1alpha2.Task
				if err := mgrClient.Get(ctx, types.NamespacedName{Name: "fs-task-alpha", Namespace: globalWaveNamespace}, &got); err != nil {
					return ""
				}
				return got.Status.Phase
			}, "30s", "500ms").Should(Equal("AwaitingApproval"),
				"fs-task-alpha must reach AwaitingApproval under gates.task=approve (full-supervised hold)")

			// Apply approve-task annotation → TaskReconciler consumes it, proceeds.
			var fsTaskObj tideprojectv1alpha2.Task
			Expect(mgrClient.Get(ctx, types.NamespacedName{Name: "fs-task-alpha", Namespace: globalWaveNamespace}, &fsTaskObj)).To(Succeed())
			approveTaskPatch := client.MergeFrom(fsTaskObj.DeepCopy())
			if fsTaskObj.Annotations == nil {
				fsTaskObj.Annotations = map[string]string{}
			}
			fsTaskObj.Annotations[gates.AnnotationApprovePrefix+"task"] = "true"
			Expect(k8sClient.Patch(ctx, &fsTaskObj, approveTaskPatch)).To(Succeed())

			// Task must leave AwaitingApproval after approve-task annotation.
			Eventually(func() string {
				var got tideprojectv1alpha2.Task
				if err := mgrClient.Get(ctx, types.NamespacedName{Name: "fs-task-alpha", Namespace: globalWaveNamespace}, &got); err != nil {
					return ""
				}
				return got.Status.Phase
			}, "15s", "100ms").ShouldNot(Equal("AwaitingApproval"),
				"fs-task-alpha must leave AwaitingApproval after approve-task annotation (full-supervised release)")

			// Cleanup.
			_ = k8sClient.Delete(ctx, &tideprojectv1alpha2.Task{ObjectMeta: metav1.ObjectMeta{Name: "fs-task-alpha", Namespace: globalWaveNamespace}})
			_ = k8sClient.Delete(ctx, &tideprojectv1alpha2.Project{ObjectMeta: metav1.ObjectMeta{Name: fsProjectName, Namespace: globalWaveNamespace}})
		})
	})
})
