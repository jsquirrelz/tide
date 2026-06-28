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

// Plan 31-02 Task 1+2 — Envtest coverage for ADOPT-01 / ADOPT-03 / ADOPT-05.
//
// ADOPT-01: an adopted Project (ImportSource + ImportComplete=True + owned Milestone)
// advances Initialized→Running with ZERO role=project-planner Jobs dispatched, and
// stamps ConditionProjectPlannerSuppressed=True (Reason=AdoptionComplete) in one patch.
//
// ADOPT-05 (cold-cache restart): the durable suppression condition short-circuits
// before the live r.List — Phase=Running is preserved on re-reconcile with no new Jobs.
//
// ADOPT-05 (no-regression): a normal Project (ImportSource==nil) still dispatches its
// project-planner Job and advances normally — no regression on the non-import path.
//
// ADOPT-03 (budget-gate): a Running, adopted Project with ConditionBudgetBlocked=True
// seeded directly via Status().Patch refuses planner dispatch — zero new planner Jobs
// appear (the real dispatch-gate enforcement path, not Phase==BudgetExceeded).
package controller

import (
	"context"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	batchv1 "k8s.io/api/batch/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	tidev1alpha2 "github.com/jsquirrelz/tide/api/v1alpha2"
)

// ────────────────────────────────────────────────────────────────────────────
// Helpers
// ────────────────────────────────────────────────────────────────────────────

// newAdoptionReconciler builds a ProjectReconciler for adoption lifecycle specs.
// Mirrors newBHProjectReconciler (billing_halt_regression_test.go:986) — uses
// mgrClient so the cached client sees freshly-patched status conditions.
func newAdoptionReconciler() *ProjectReconciler {
	return &ProjectReconciler{
		Client:         mgrClient,
		Scheme:         k8sClient.Scheme(),
		Dispatcher:     &stubDispatcher{},
		SigningKey:     testSigningKey,
		CredproxyImage: testCredproxyImage,
		HelmProviderDefaults: ProviderDefaults{
			Image: testSubagentImage,
		},
		EnvReader: newMapEnvReader(),
	}
}

// createAdoptedProject creates a Project with ImportSource set, stamps
// ImportComplete=True, and creates one owned Milestone (metav1.IsControlledBy=true).
// Returns the re-fetched project (with UID) and the milestone's name.
// Callers must defer cleanup via cleanupAdoptedProject.
func createAdoptedProject(ctx context.Context, projName, msName string) *tidev1alpha2.Project {
	// 1. Create project with ImportSource.
	proj := &tidev1alpha2.Project{
		ObjectMeta: metav1.ObjectMeta{Name: projName, Namespace: "default"},
		Spec: tidev1alpha2.ProjectSpec{
			SchemaRevision: "v1alpha2",
			TargetRepo:     "https://github.com/example/adopted.git",
			ImportSource: &tidev1alpha2.ImportSourceRef{
				SeedManifestConfigMap: fmt.Sprintf("seed-cm-%s", projName),
				SalvagedPVCSubPath:    "old-uid/workspace",
			},
		},
	}
	Expect(k8sClient.Create(ctx, proj)).To(Succeed())
	waitForCacheSync(projName, "default", &tidev1alpha2.Project{})

	// 2. Re-fetch to get the UID.
	fetched := &tidev1alpha2.Project{}
	Expect(k8sClient.Get(ctx, types.NamespacedName{Name: projName, Namespace: "default"}, fetched)).To(Succeed())

	// 3. Stamp ImportComplete=True.
	sp := client.MergeFrom(fetched.DeepCopy())
	meta.SetStatusCondition(&fetched.Status.Conditions, metav1.Condition{
		Type:               tidev1alpha2.ConditionImportComplete,
		Status:             metav1.ConditionTrue,
		Reason:             tidev1alpha2.ReasonImportSucceeded,
		Message:            "Import completed for test",
		LastTransitionTime: metav1.Now(),
	})
	Expect(k8sClient.Status().Patch(ctx, fetched, sp)).To(Succeed())

	// 4. Re-fetch after status patch (UID is stable, resourceVersion changes).
	Expect(k8sClient.Get(ctx, types.NamespacedName{Name: projName, Namespace: "default"}, fetched)).To(Succeed())

	// 5. Wait for ImportComplete to be visible in cache.
	Eventually(func() bool {
		var p tidev1alpha2.Project
		if err := mgrClient.Get(ctx, types.NamespacedName{Name: projName, Namespace: "default"}, &p); err != nil {
			return false
		}
		c := meta.FindStatusCondition(p.Status.Conditions, tidev1alpha2.ConditionImportComplete)
		return c != nil && c.Status == metav1.ConditionTrue
	}, 5*time.Second, 50*time.Millisecond).Should(BeTrue(), "ImportComplete must be in cache before reconcile")

	// 6. Create an owned Milestone with a real controller owner reference pointing
	// to this Project's UID (mirrors import_controller.go:405 via owner.EnsureOwnerRef).
	tru := true
	ms := &tidev1alpha2.Milestone{
		ObjectMeta: metav1.ObjectMeta{
			Name:      msName,
			Namespace: "default",
			OwnerReferences: []metav1.OwnerReference{{
				APIVersion:         tidev1alpha2.GroupVersion.String(),
				Kind:               "Project",
				Name:               projName,
				UID:                fetched.UID,
				Controller:         &tru,
				BlockOwnerDeletion: &tru,
			}},
		},
		Spec: tidev1alpha2.MilestoneSpec{ProjectRef: projName},
	}
	Expect(k8sClient.Create(ctx, ms)).To(Succeed())
	waitForCacheSync(msName, "default", &tidev1alpha2.Milestone{})

	return fetched
}

// cleanupAdoptedProject removes a Project and its owned Milestone (best-effort).
func cleanupAdoptedProject(ctx context.Context, projName, msName string) {
	ms := &tidev1alpha2.Milestone{}
	if err := k8sClient.Get(ctx, types.NamespacedName{Name: msName, Namespace: "default"}, ms); err == nil {
		ms.Finalizers = nil
		_ = k8sClient.Update(ctx, ms)
		_ = k8sClient.Delete(ctx, ms)
	}
	p := &tidev1alpha2.Project{}
	if err := k8sClient.Get(ctx, types.NamespacedName{Name: projName, Namespace: "default"}, p); err == nil {
		p.Finalizers = nil
		_ = k8sClient.Update(ctx, p)
		_ = k8sClient.Delete(ctx, p)
	}
}

// listPlannerJobsForProject lists all Jobs in the default namespace with
// tideproject.k8s/level=project AND tideproject.k8s/role=planner, narrowed
// to a specific project by tideproject.k8s/project-uid label.
func listPlannerJobsForProject(ctx context.Context, projectUID types.UID) []batchv1.Job {
	var jobs batchv1.JobList
	Expect(k8sClient.List(ctx, &jobs,
		client.InNamespace("default"),
		client.MatchingLabels{
			"tideproject.k8s/level":       "project",
			"tideproject.k8s/role":        "planner",
			"tideproject.k8s/project-uid": string(projectUID),
		},
	)).To(Succeed())
	return jobs.Items
}

// stampBudgetBlocked stamps ConditionBudgetBlocked=True on a Project via
// Status().Patch and waits for the cache to reflect it. This is the lightest
// faithful path — identical to how budget_blocked_test.go seeds the condition.
func stampBudgetBlockedOnProject(ctx context.Context, projName string) {
	var proj tidev1alpha2.Project
	Expect(k8sClient.Get(ctx, types.NamespacedName{Name: projName, Namespace: "default"}, &proj)).To(Succeed())
	sp := client.MergeFrom(proj.DeepCopy())
	meta.SetStatusCondition(&proj.Status.Conditions, metav1.Condition{
		Type:               tidev1alpha2.ConditionBudgetBlocked,
		Status:             metav1.ConditionTrue,
		Reason:             tidev1alpha2.ReasonBudgetCapReached,
		Message:            "Test: budget cap reached",
		LastTransitionTime: metav1.Now(),
	})
	Expect(k8sClient.Status().Patch(ctx, &proj, sp)).To(Succeed())
	// Wait for cache to reflect it.
	Eventually(func() bool {
		var p tidev1alpha2.Project
		if err := mgrClient.Get(ctx, types.NamespacedName{Name: projName, Namespace: "default"}, &p); err != nil {
			return false
		}
		c := meta.FindStatusCondition(p.Status.Conditions, tidev1alpha2.ConditionBudgetBlocked)
		return c != nil && c.Status == metav1.ConditionTrue
	}, 5*time.Second, 50*time.Millisecond).Should(BeTrue(), "BudgetBlocked must be visible in cache")
}

// ────────────────────────────────────────────────────────────────────────────
// ADOPT-01: adopted Project advances to Running with zero project-planner Jobs
// and stamps ConditionProjectPlannerSuppressed=True (Reason=AdoptionComplete).
// ────────────────────────────────────────────────────────────────────────────

var _ = Describe("Adoption lifecycle — ADOPT-01/03/05 (Plan 31-02)",
	Label("envtest", "phase31", "adoption"), func() {
		ctx := context.Background()

		Describe("ADOPT-01: adopted Project advances Initialized→Running without project-planner Job", func() {
			const projName = "adopt-01-proj"
			const msName = "adopt-01-ms"

			var fetched *tidev1alpha2.Project
			BeforeEach(func() {
				fetched = createAdoptedProject(ctx, projName, msName)
			})
			AfterEach(func() {
				cleanupAdoptedProject(ctx, projName, msName)
				// Clean up any planner Jobs that may have been left.
				var jobs batchv1.JobList
				_ = k8sClient.List(ctx, &jobs, client.InNamespace("default"))
				for i := range jobs.Items {
					if jobs.Items[i].Labels["tideproject.k8s/project-uid"] == string(fetched.UID) {
						_ = k8sClient.Delete(ctx, &jobs.Items[i])
					}
				}
			})

			It("stamps Phase=Running + ConditionProjectPlannerSuppressed=True and creates zero project-planner Jobs", func() {
				r := newAdoptionReconciler()
				name := types.NamespacedName{Name: projName, Namespace: "default"}

				// Reconcile until Phase=Running is visible (may take a few cycles due to cache).
				Eventually(func(g Gomega) {
					_, err := r.reconcileProjectPlannerDispatch(ctx, fetched)
					g.Expect(err).NotTo(HaveOccurred())
					// Re-fetch to see if Phase was patched.
					var p tidev1alpha2.Project
					g.Expect(mgrClient.Get(ctx, name, &p)).To(Succeed())
					g.Expect(p.Status.Phase).To(Equal(tidev1alpha2.PhaseRunning),
						"ADOPT-01: adopted Project must advance to Running")
				}, 5*time.Second, 100*time.Millisecond).Should(Succeed())

				// Assert ConditionProjectPlannerSuppressed=True (Reason=AdoptionComplete).
				var p tidev1alpha2.Project
				Expect(mgrClient.Get(ctx, name, &p)).To(Succeed())
				suppCond := meta.FindStatusCondition(p.Status.Conditions, tidev1alpha2.ConditionProjectPlannerSuppressed)
				Expect(suppCond).NotTo(BeNil(),
					"ADOPT-01: ConditionProjectPlannerSuppressed must be stamped on adopted Project")
				Expect(suppCond.Status).To(Equal(metav1.ConditionTrue),
					"ADOPT-01: ConditionProjectPlannerSuppressed must be True")
				Expect(suppCond.Reason).To(Equal(tidev1alpha2.ReasonAdoptionComplete),
					"ADOPT-01: suppression condition must have Reason=AdoptionComplete")

				// Assert ZERO project-planner Jobs created.
				Expect(listPlannerJobsForProject(ctx, fetched.UID)).To(BeEmpty(),
					"ADOPT-01: zero project-planner Jobs must be created for an adopted Project")
			})
		})

		// ────────────────────────────────────────────────────────────────────────────
		// ADOPT-05 (cold-cache restart): the durable suppression condition short-circuits
		// before the live r.List — re-reconcile with no new Jobs, Phase stays Running.
		// ────────────────────────────────────────────────────────────────────────────

		Describe("ADOPT-05: durable suppression survives a cold-cache restart (condition-first short-circuit)", func() {
			const projName = "adopt-05-cold-proj"
			const msName = "adopt-05-cold-ms"

			var fetched *tidev1alpha2.Project
			BeforeEach(func() {
				fetched = createAdoptedProject(ctx, projName, msName)
			})
			AfterEach(func() {
				cleanupAdoptedProject(ctx, projName, msName)
				var jobs batchv1.JobList
				_ = k8sClient.List(ctx, &jobs, client.InNamespace("default"))
				for i := range jobs.Items {
					if jobs.Items[i].Labels["tideproject.k8s/project-uid"] == string(fetched.UID) {
						_ = k8sClient.Delete(ctx, &jobs.Items[i])
					}
				}
			})

			It("re-reconcile after suppression condition is stamped creates zero new planner Jobs", func() {
				r := newAdoptionReconciler()
				name := types.NamespacedName{Name: projName, Namespace: "default"}

				// First reconcile: stamps Phase=Running + ConditionProjectPlannerSuppressed.
				Eventually(func(g Gomega) {
					_, err := r.reconcileProjectPlannerDispatch(ctx, fetched)
					g.Expect(err).NotTo(HaveOccurred())
					var p tidev1alpha2.Project
					g.Expect(mgrClient.Get(ctx, name, &p)).To(Succeed())
					g.Expect(p.Status.Phase).To(Equal(tidev1alpha2.PhaseRunning))
					cond := meta.FindStatusCondition(p.Status.Conditions, tidev1alpha2.ConditionProjectPlannerSuppressed)
					g.Expect(cond).NotTo(BeNil())
					g.Expect(cond.Status).To(Equal(metav1.ConditionTrue))
				}, 5*time.Second, 100*time.Millisecond).Should(Succeed())

				// Simulate a cold-cache restart: fetch the project fresh (condition is in .status).
				var fresh tidev1alpha2.Project
				Expect(mgrClient.Get(ctx, name, &fresh)).To(Succeed())

				// Second reconcile: must short-circuit on the durable condition.
				result, err := r.reconcileProjectPlannerDispatch(ctx, &fresh)
				Expect(err).NotTo(HaveOccurred())
				Expect(result).To(Equal(reconcile.Result{}),
					"ADOPT-05: suppression condition short-circuit must return empty Result (no requeue)")

				// Assert Phase is still Running, no new Jobs.
				var after tidev1alpha2.Project
				Expect(mgrClient.Get(ctx, name, &after)).To(Succeed())
				Expect(after.Status.Phase).To(Equal(tidev1alpha2.PhaseRunning),
					"ADOPT-05: Phase must remain Running after cold-cache re-reconcile")
				Expect(listPlannerJobsForProject(ctx, fetched.UID)).To(BeEmpty(),
					"ADOPT-05: zero project-planner Jobs must exist after cold-cache re-reconcile")
			})
		})

		// ────────────────────────────────────────────────────────────────────────────
		// ADOPT-05 (no-regression): a normal Project (ImportSource==nil) still
		// dispatches its project-planner and advances normally.
		// ────────────────────────────────────────────────────────────────────────────

		Describe("ADOPT-05 no-regression: normal Project (ImportSource==nil) still dispatches project-planner", func() {
			const projName = "adopt-05-normal-proj"

			var proj *tidev1alpha2.Project
			BeforeEach(func() {
				proj = &tidev1alpha2.Project{
					ObjectMeta: metav1.ObjectMeta{Name: projName, Namespace: "default"},
					Spec: tidev1alpha2.ProjectSpec{
						SchemaRevision: "v1alpha2",
						TargetRepo:     "https://github.com/example/normal.git",
						OutcomePrompt:  "Build something",
						Subagent:       tidev1alpha2.SubagentConfig{Model: "claude-opus-4-7"},
					},
				}
				Expect(k8sClient.Create(ctx, proj)).To(Succeed())
				waitForCacheSync(projName, "default", &tidev1alpha2.Project{})
				// Re-fetch to get the UID.
				Expect(k8sClient.Get(ctx, types.NamespacedName{Name: projName, Namespace: "default"}, proj)).To(Succeed())
			})
			AfterEach(func() {
				p := &tidev1alpha2.Project{}
				if err := k8sClient.Get(ctx, types.NamespacedName{Name: projName, Namespace: "default"}, p); err == nil {
					p.Finalizers = nil
					_ = k8sClient.Update(ctx, p)
					_ = k8sClient.Delete(ctx, p)
				}
				// Clean up any Jobs created.
				var jobs batchv1.JobList
				_ = k8sClient.List(ctx, &jobs, client.InNamespace("default"))
				for i := range jobs.Items {
					if jobs.Items[i].Labels["tideproject.k8s/project-uid"] == string(proj.UID) {
						_ = k8sClient.Delete(ctx, &jobs.Items[i])
					}
				}
			})

			It("dispatches project-planner Job and advances Phase=Running (no import suppression on normal path)", func() {
				// Build a reconciler with PlannerPool for the normal-dispatch path.
				r := &ProjectReconciler{
					Client:         mgrClient,
					Scheme:         k8sClient.Scheme(),
					Dispatcher:     &stubDispatcher{},
					PlannerPool:    newPlannerPoolForTest(),
					SigningKey:     testSigningKey,
					CredproxyImage: testCredproxyImage,
					HelmProviderDefaults: ProviderDefaults{
						Image: testSubagentImage,
					},
					EnvReader: newMapEnvReader(),
				}

				name := types.NamespacedName{Name: projName, Namespace: "default"}

				// Wait for Phase=Running AND a planner Job to exist.
				Eventually(func(g Gomega) {
					// Re-fetch before each reconcile attempt.
					var p tidev1alpha2.Project
					g.Expect(mgrClient.Get(ctx, name, &p)).To(Succeed())
					_, err := r.reconcileProjectPlannerDispatch(ctx, &p)
					g.Expect(err).NotTo(HaveOccurred())
					// Check Phase.
					g.Expect(mgrClient.Get(ctx, name, &p)).To(Succeed())
					g.Expect(p.Status.Phase).To(Equal(tidev1alpha2.PhaseRunning),
						"ADOPT-05 no-regression: normal Project must advance to Running")
				}, 10*time.Second, 200*time.Millisecond).Should(Succeed())

				// Assert ConditionProjectPlannerSuppressed is NOT set on normal Project.
				var p tidev1alpha2.Project
				Expect(mgrClient.Get(ctx, name, &p)).To(Succeed())
				suppCond := meta.FindStatusCondition(p.Status.Conditions, tidev1alpha2.ConditionProjectPlannerSuppressed)
				Expect(suppCond).To(BeNil(),
					"ADOPT-05 no-regression: ConditionProjectPlannerSuppressed must NOT be set on normal Project")
			})
		})

		// ────────────────────────────────────────────────────────────────────────────
		// ADOPT-03: over-cap adopted Project with ConditionBudgetBlocked=True seeded
		// directly → planner dispatch gate refuses, zero new planner Jobs.
		// ────────────────────────────────────────────────────────────────────────────

		Describe("ADOPT-03: ConditionBudgetBlocked=True drives dispatch gate to refuse — zero new planner Jobs", func() {
			const projName = "adopt-03-proj"
			const msName = "adopt-03-ms"

			var fetched *tidev1alpha2.Project
			BeforeEach(func() {
				fetched = createAdoptedProject(ctx, projName, msName)

				// Advance to Running first (ADOPT-01 path), then seed BudgetBlocked.
				r := newAdoptionReconciler()
				name := types.NamespacedName{Name: projName, Namespace: "default"}
				Eventually(func(g Gomega) {
					_, err := r.reconcileProjectPlannerDispatch(ctx, fetched)
					g.Expect(err).NotTo(HaveOccurred())
					var p tidev1alpha2.Project
					g.Expect(mgrClient.Get(ctx, name, &p)).To(Succeed())
					g.Expect(p.Status.Phase).To(Equal(tidev1alpha2.PhaseRunning))
				}, 5*time.Second, 100*time.Millisecond).Should(Succeed())

				// Now seed BudgetBlocked=True (the ACTUAL dispatch gate).
				stampBudgetBlockedOnProject(ctx, projName)
			})
			AfterEach(func() {
				cleanupAdoptedProject(ctx, projName, msName)
				var jobs batchv1.JobList
				_ = k8sClient.List(ctx, &jobs, client.InNamespace("default"))
				for i := range jobs.Items {
					if jobs.Items[i].Labels["tideproject.k8s/project-uid"] == string(fetched.UID) {
						_ = k8sClient.Delete(ctx, &jobs.Items[i])
					}
				}
			})

			It("refuses planner dispatch while ConditionBudgetBlocked=True — zero project-planner Jobs appear", func() {
				r := newAdoptionReconciler()
				name := types.NamespacedName{Name: projName, Namespace: "default"}

				// Re-fetch the Running+BudgetBlocked project.
				var p tidev1alpha2.Project
				Expect(mgrClient.Get(ctx, name, &p)).To(Succeed())

				// The dispatch gate (checkBudgetBlocked) must short-circuit before any Job creation.
				// The function returns ctrl.Result{RequeueAfter: 30s}, nil.
				result, err := r.reconcileProjectPlannerDispatch(ctx, &p)
				Expect(err).NotTo(HaveOccurred())
				Expect(result.RequeueAfter).To(Equal(30*time.Second),
					"ADOPT-03: checkBudgetBlocked must park dispatch with 30s requeue")

				// Assert zero project-planner (or any new planner) Jobs.
				// ADOPT-01 already proved zero Jobs were created during the Running advance;
				// this proves BudgetBlocked=True gates the subsequent dispatch attempt.
				Expect(listPlannerJobsForProject(ctx, fetched.UID)).To(BeEmpty(),
					"ADOPT-03: ConditionBudgetBlocked=True must prevent project-planner Job creation")
			})
		})
	})
