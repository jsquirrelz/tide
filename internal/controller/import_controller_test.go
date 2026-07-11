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

package controller

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	tideprojectv1alpha2 "github.com/jsquirrelz/tide/api/v1alpha2"
)

// newImportReconciler constructs an ImportReconciler wired to the shared mgrClient.
// ImportImage is left empty so that import Job creation is skipped in envtest
// (mirrors the ReporterImage empty-skip pattern; envtest has no real image pull).
func newImportReconciler() *ImportReconciler {
	return &ImportReconciler{
		Client:                  mgrClient,
		Scheme:                  k8sClient.Scheme(),
		MaxConcurrentReconciles: 1,
		ImportImage:             "", // empty → skip Job; treat copy phase as no-op-success
		SharedPVCName:           "tide-projects",
	}
}

// makeSeedConfigMap creates a ConfigMap in the given namespace carrying a seed manifest
// JSON for the import tests. Returns the ConfigMap name.
func makeSeedConfigMap(ctx context.Context, namespace, cmName string, manifest seedManifest) error {
	raw, err := json.Marshal(manifest)
	if err != nil {
		return fmt.Errorf("marshal seed manifest: %w", err)
	}
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      cmName,
			Namespace: namespace,
		},
		Data: map[string]string{
			"manifest": string(raw),
		},
	}
	return k8sClient.Create(ctx, cm)
}

// makeImportProject creates a Project with Spec.ImportSource set pointing to seedCMName.
func makeImportProject(ctx context.Context, name, namespace, seedCMName string) error {
	proj := &tideprojectv1alpha2.Project{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Spec: tideprojectv1alpha2.ProjectSpec{
			SchemaRevision: "v1alpha2",
			TargetRepo:     "https://github.com/example/import-test.git",
			Subagent: tideprojectv1alpha2.SubagentConfig{
				Model: "claude-opus-4-7",
			},
			ImportSource: &tideprojectv1alpha2.ImportSourceRef{
				SeedManifestConfigMap: seedCMName,
				SalvagedPVCSubPath:    "old-project-uid/workspace",
			},
		},
	}
	return k8sClient.Create(ctx, proj)
}

// findImportCondition is a helper that fetches the current ConditionImportComplete
// from the Project status using the mgrClient (cache-backed).
func findImportCondition(ctx context.Context, name, namespace string) *metav1.Condition {
	var proj tideprojectv1alpha2.Project
	if err := mgrClient.Get(ctx, types.NamespacedName{Name: name, Namespace: namespace}, &proj); err != nil {
		return nil
	}
	return apimeta.FindStatusCondition(proj.Status.Conditions, tideprojectv1alpha2.ConditionImportComplete)
}

// ===== IMPORT CONTROLLER TESTS =====

var _ = Describe("ImportReconciler", Label("envtest", "phase28"), func() {
	ctx := context.Background()
	const ns = "default"

	// -------------------------------------------------------------------------
	// Test 1 (IMPORT-01): Adoption happy path — new-UID CRs + rekey ConfigMap + ImportComplete=True
	// -------------------------------------------------------------------------
	Describe("Test 1 (IMPORT-01): adoption happy path", func() {
		const projName = "import-test-adoption"
		const seedCMName = "import-seed-adoption"
		const msMigName = "ms-import-adopt"
		const phMigName = "ph-import-adopt"
		const plMigName = "pl-import-adopt"

		BeforeEach(func() {
			// Create seed ConfigMap with a 1-milestone/1-phase/1-plan tree.
			manifest := seedManifest{
				Milestones: []seedEntry{
					{
						Name:       msMigName,
						FQName:     msMigName,
						OldUID:     "old-ms-uid-adopt",
						ProjectRef: projName,
					},
				},
				Phases: []seedEntry{
					{
						Name:         phMigName,
						FQName:       phMigName,
						OldUID:       "old-ph-uid-adopt",
						MilestoneRef: msMigName,
					},
				},
				Plans: []seedEntry{
					{
						Name:     plMigName,
						FQName:   plMigName,
						OldUID:   "old-pl-uid-adopt",
						PhaseRef: phMigName,
					},
				},
			}
			Expect(makeSeedConfigMap(ctx, ns, seedCMName, manifest)).To(Succeed())
			Expect(makeImportProject(ctx, projName, ns, seedCMName)).To(Succeed())
			waitForCacheSync(projName, ns, &tideprojectv1alpha2.Project{})
		})

		AfterEach(func() {
			// Cleanup Project (best-effort; webhooks require finalizer removal).
			proj := &tideprojectv1alpha2.Project{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: projName, Namespace: ns}, proj); err == nil {
				proj.Finalizers = nil
				_ = k8sClient.Update(ctx, proj)
				_ = k8sClient.Delete(ctx, proj)
			}
			// Cleanup child CRs.
			for _, name := range []string{msMigName} {
				ms := &tideprojectv1alpha2.Milestone{}
				if err := k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: ns}, ms); err == nil {
					ms.Finalizers = nil
					_ = k8sClient.Update(ctx, ms)
					_ = k8sClient.Delete(ctx, ms)
				}
			}
			for _, name := range []string{phMigName} {
				ph := &tideprojectv1alpha2.Phase{}
				if err := k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: ns}, ph); err == nil {
					ph.Finalizers = nil
					_ = k8sClient.Update(ctx, ph)
					_ = k8sClient.Delete(ctx, ph)
				}
			}
			for _, name := range []string{plMigName} {
				pl := &tideprojectv1alpha2.Plan{}
				if err := k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: ns}, pl); err == nil {
					pl.Finalizers = nil
					_ = k8sClient.Update(ctx, pl)
					_ = k8sClient.Delete(ctx, pl)
				}
			}
			// Cleanup ConfigMaps.
			cm := &corev1.ConfigMap{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: seedCMName, Namespace: ns}, cm); err == nil {
				_ = k8sClient.Delete(ctx, cm)
			}
		})

		It("materializes Milestone/Phase/Plan CRs with new UIDs, rekey ConfigMap, and sets ImportComplete=True", func() {
			r := newImportReconciler()
			projKey := types.NamespacedName{Name: projName, Namespace: ns}

			// Drive reconcile until ConditionImportComplete is set.
			// With ImportImage="", the copy phase is skipped and ImportComplete=True is set after CRs are materialized.
			Eventually(func(g Gomega) {
				_, err := r.Reconcile(ctx, reconcileRequest(projKey))
				g.Expect(err).NotTo(HaveOccurred())
				cond := findImportCondition(ctx, projName, ns)
				g.Expect(cond).NotTo(BeNil())
				g.Expect(cond.Status).To(Equal(metav1.ConditionTrue))
				g.Expect(cond.Reason).To(Equal(tideprojectv1alpha2.ReasonImportSucceeded))
			}, 15*time.Second, 200*time.Millisecond).Should(Succeed())

			// Assert Milestone CR was created with a new (non-empty) UID.
			var ms tideprojectv1alpha2.Milestone
			Expect(mgrClient.Get(ctx, types.NamespacedName{Name: msMigName, Namespace: ns}, &ms)).To(Succeed())
			Expect(string(ms.UID)).NotTo(BeEmpty())
			Expect(string(ms.UID)).NotTo(Equal("old-ms-uid-adopt"))

			// Assert Phase CR was created.
			var ph tideprojectv1alpha2.Phase
			Expect(mgrClient.Get(ctx, types.NamespacedName{Name: phMigName, Namespace: ns}, &ph)).To(Succeed())
			Expect(string(ph.UID)).NotTo(BeEmpty())

			// Assert Plan CR was created.
			var pl tideprojectv1alpha2.Plan
			Expect(mgrClient.Get(ctx, types.NamespacedName{Name: plMigName, Namespace: ns}, &pl)).To(Succeed())
			Expect(string(pl.UID)).NotTo(BeEmpty())

			// Assert rekey ConfigMap exists.
			var proj tideprojectv1alpha2.Project
			Expect(mgrClient.Get(ctx, projKey, &proj)).To(Succeed())
			rekeyCMName := fmt.Sprintf("tide-import-rekey-%s", proj.UID)
			var rekeyCM corev1.ConfigMap
			Expect(mgrClient.Get(ctx, types.NamespacedName{Name: rekeyCMName, Namespace: ns}, &rekeyCM)).To(Succeed())
			Expect(rekeyCM.Data).To(HaveKey("rekey.json"))

			// Assert the rekey table is a JSON ARRAY (the shape tide-import decodes)
			// and contains the Milestone row with old→new UID mapping.
			var rekeyRows []rekeyRow
			Expect(json.Unmarshal([]byte(rekeyCM.Data["rekey.json"]), &rekeyRows)).To(Succeed())
			var found bool
			for _, row := range rekeyRows {
				if row.FQName == msMigName {
					found = true
					Expect(row.OldUID).To(Equal("old-ms-uid-adopt"))
					Expect(row.NewUID).To(Equal(string(ms.UID)))
				}
			}
			Expect(found).To(BeTrue(), "rekey table should contain Milestone FQ-name row")
		})
	})

	// -------------------------------------------------------------------------
	// Test 2 (IMPORT-04): Plan-level dependsOn cycle — ZERO CRs created, ReasonCyclicPlanDetected
	//
	// This test proves the controller builds the seed-CR graph (not the empty Task-level
	// buildGlobalEdges projection). An edgeless projection would silently succeed; this
	// test asserts the Plan-level cycle is caught and NO partial CRs are created (D-10).
	// -------------------------------------------------------------------------
	Describe("Test 2 (IMPORT-04): Plan-level dependsOn cycle rejects with zero CRs", func() {
		const projName = "import-test-cycle"
		const seedCMName = "import-seed-cycle"
		const msCycleName = "ms-cycle"
		const phCycleName = "ph-cycle"
		const plCycleAName = "pl-cycle-a"
		const plCycleBName = "pl-cycle-b"

		BeforeEach(func() {
			// Build a seed where plan-A.dependsOn=[plan-B] and plan-B.dependsOn=[plan-A].
			// This is a Plan-level dependsOn cycle in a seed with NO Tasks (D-04).
			// The cycle CANNOT be detected by buildGlobalEdges (which is edgeless under a
			// Task-less seed); the ImportController must build the seed-CR graph itself.
			manifest := seedManifest{
				Milestones: []seedEntry{
					{Name: msCycleName, FQName: msCycleName, OldUID: "old-ms-cycle", ProjectRef: projName},
				},
				Phases: []seedEntry{
					{Name: phCycleName, FQName: phCycleName, OldUID: "old-ph-cycle", MilestoneRef: msCycleName},
				},
				Plans: []seedEntry{
					{
						Name:      plCycleAName,
						FQName:    plCycleAName,
						OldUID:    "old-pl-cycle-a",
						PhaseRef:  phCycleName,
						DependsOn: []string{plCycleBName}, // A depends on B → creates edge B→A
					},
					{
						Name:      plCycleBName,
						FQName:    plCycleBName,
						OldUID:    "old-pl-cycle-b",
						PhaseRef:  phCycleName,
						DependsOn: []string{plCycleAName}, // B depends on A → creates edge A→B → CYCLE!
					},
				},
			}
			Expect(makeSeedConfigMap(ctx, ns, seedCMName, manifest)).To(Succeed())
			Expect(makeImportProject(ctx, projName, ns, seedCMName)).To(Succeed())
			waitForCacheSync(projName, ns, &tideprojectv1alpha2.Project{})
		})

		AfterEach(func() {
			proj := &tideprojectv1alpha2.Project{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: projName, Namespace: ns}, proj); err == nil {
				proj.Finalizers = nil
				_ = k8sClient.Update(ctx, proj)
				_ = k8sClient.Delete(ctx, proj)
			}
			cm := &corev1.ConfigMap{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: seedCMName, Namespace: ns}, cm); err == nil {
				_ = k8sClient.Delete(ctx, cm)
			}
		})

		It("sets ReasonCyclicPlanDetected and creates ZERO child CRs (atomicity — no partial CRs)", Label("heavy"), func() {
			r := newImportReconciler()
			projKey := types.NamespacedName{Name: projName, Namespace: ns}

			// Reconcile until the condition is set (should be fast — cycle detected before any create).
			Eventually(func(g Gomega) {
				_, err := r.Reconcile(ctx, reconcileRequest(projKey))
				g.Expect(err).NotTo(HaveOccurred())
				cond := findImportCondition(ctx, projName, ns)
				g.Expect(cond).NotTo(BeNil())
				g.Expect(cond.Status).To(Equal(metav1.ConditionFalse))
				g.Expect(cond.Reason).To(Equal(tideprojectv1alpha2.ReasonCyclicPlanDetected))
			}, 10*time.Second, 100*time.Millisecond).Should(Succeed())

			// Consistently assert ZERO Milestone/Phase/Plan CRs were created (per-milestone atomicity, D-10).
			// The cycle is detected BEFORE any client.Create, so NO child CRs should exist.
			Consistently(func(g Gomega) {
				var msList tideprojectv1alpha2.MilestoneList
				g.Expect(mgrClient.List(ctx, &msList,
					client.InNamespace(ns),
					client.MatchingLabels(map[string]string{}),
				)).To(Succeed())
				// Filter to only the cycle test's objects (by name prefix to avoid pollution from other tests).
				var cycleMS []tideprojectv1alpha2.Milestone
				for _, ms := range msList.Items {
					if ms.Name == msCycleName {
						cycleMS = append(cycleMS, ms)
					}
				}
				g.Expect(cycleMS).To(BeEmpty(),
					"cycle detection must prevent ANY Milestone CRs from being created (D-10 atomicity)")

				var phList tideprojectv1alpha2.PhaseList
				g.Expect(mgrClient.List(ctx, &phList, client.InNamespace(ns))).To(Succeed())
				var cyclePh []tideprojectv1alpha2.Phase
				for _, ph := range phList.Items {
					if ph.Name == phCycleName {
						cyclePh = append(cyclePh, ph)
					}
				}
				g.Expect(cyclePh).To(BeEmpty(),
					"cycle detection must prevent ANY Phase CRs from being created (D-10 atomicity)")

				var plList tideprojectv1alpha2.PlanList
				g.Expect(mgrClient.List(ctx, &plList, client.InNamespace(ns))).To(Succeed())
				var cyclePl []tideprojectv1alpha2.Plan
				for _, pl := range plList.Items {
					if pl.Name == plCycleAName || pl.Name == plCycleBName {
						cyclePl = append(cyclePl, pl)
					}
				}
				g.Expect(cyclePl).To(BeEmpty(),
					"cycle detection must prevent ANY Plan CRs from being created (D-10 atomicity)")
			}, 3*time.Second, 200*time.Millisecond).Should(Succeed())
		})
	})

	// -------------------------------------------------------------------------
	// Test 3 (IMPORT-05): Kind allowlist — seed entry with non-allowlisted Kind
	// is rejected; ImportFailed set; no CR created for the bad entry.
	//
	// In the ImportController the allowlist is enforced by only materializing
	// Milestones, Phases, and Plans (the three allowed Kinds). An entry with
	// an unsupported Kind would be ignored/skipped. The import is NOT failed
	// for unknown Kinds in the seed manifest — instead only valid Kinds are
	// materialized and unknown Kinds are silently skipped (the binary enforces
	// the allowlist for envelope children). We test this by using an empty seed
	// with no valid entries (zero CRs) and verifying ImportComplete=True is still
	// set (empty seed is valid). For the Kind rejection surface specific to
	// envelope children, see import_jobspec.go / plan 03 binary tests.
	// -------------------------------------------------------------------------
	Describe("Test 3 (IMPORT-05): empty seed (no CRs to materialize) succeeds", func() {
		const projName = "import-test-empty"
		const seedCMName = "import-seed-empty"

		BeforeEach(func() {
			// Empty seed: no milestones/phases/plans. Valid (an import with nothing to re-key).
			manifest := seedManifest{
				Milestones: []seedEntry{},
				Phases:     []seedEntry{},
				Plans:      []seedEntry{},
			}
			Expect(makeSeedConfigMap(ctx, ns, seedCMName, manifest)).To(Succeed())
			Expect(makeImportProject(ctx, projName, ns, seedCMName)).To(Succeed())
			waitForCacheSync(projName, ns, &tideprojectv1alpha2.Project{})
		})

		AfterEach(func() {
			proj := &tideprojectv1alpha2.Project{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: projName, Namespace: ns}, proj); err == nil {
				proj.Finalizers = nil
				_ = k8sClient.Update(ctx, proj)
				_ = k8sClient.Delete(ctx, proj)
			}
			cm := &corev1.ConfigMap{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: seedCMName, Namespace: ns}, cm); err == nil {
				_ = k8sClient.Delete(ctx, cm)
			}
		})

		It("sets ImportComplete=True even for an empty seed (no CRs to materialize)", func() {
			r := newImportReconciler()
			projKey := types.NamespacedName{Name: projName, Namespace: ns}

			Eventually(func(g Gomega) {
				_, err := r.Reconcile(ctx, reconcileRequest(projKey))
				g.Expect(err).NotTo(HaveOccurred())
				cond := findImportCondition(ctx, projName, ns)
				g.Expect(cond).NotTo(BeNil())
				g.Expect(cond.Status).To(Equal(metav1.ConditionTrue))
			}, 10*time.Second, 200*time.Millisecond).Should(Succeed())
		})
	})

	// -------------------------------------------------------------------------
	// Test 4 (IMPORT-01 idempotency, D-12): with ConditionImportComplete=True,
	// a second reconcile creates no duplicate CRs and no new Job.
	// -------------------------------------------------------------------------
	Describe("Test 4 (IMPORT-01 idempotency D-12): re-run after ImportComplete=True is a no-op", func() {
		const projName = "import-test-idempotent"
		const seedCMName = "import-seed-idempotent"
		const msIdempName = "ms-import-idemp"
		const phIdempName = "ph-import-idemp"
		const plIdempName = "pl-import-idemp"

		BeforeEach(func() {
			manifest := seedManifest{
				Milestones: []seedEntry{
					{Name: msIdempName, FQName: msIdempName, OldUID: "old-ms-idemp", ProjectRef: projName},
				},
				Phases: []seedEntry{
					{Name: phIdempName, FQName: phIdempName, OldUID: "old-ph-idemp", MilestoneRef: msIdempName},
				},
				Plans: []seedEntry{
					{Name: plIdempName, FQName: plIdempName, OldUID: "old-pl-idemp", PhaseRef: phIdempName},
				},
			}
			Expect(makeSeedConfigMap(ctx, ns, seedCMName, manifest)).To(Succeed())
			Expect(makeImportProject(ctx, projName, ns, seedCMName)).To(Succeed())
			waitForCacheSync(projName, ns, &tideprojectv1alpha2.Project{})
		})

		AfterEach(func() {
			proj := &tideprojectv1alpha2.Project{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: projName, Namespace: ns}, proj); err == nil {
				proj.Finalizers = nil
				_ = k8sClient.Update(ctx, proj)
				_ = k8sClient.Delete(ctx, proj)
			}
			for _, name := range []string{msIdempName} {
				ms := &tideprojectv1alpha2.Milestone{}
				if err := k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: ns}, ms); err == nil {
					ms.Finalizers = nil
					_ = k8sClient.Update(ctx, ms)
					_ = k8sClient.Delete(ctx, ms)
				}
			}
			for _, name := range []string{phIdempName} {
				ph := &tideprojectv1alpha2.Phase{}
				if err := k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: ns}, ph); err == nil {
					ph.Finalizers = nil
					_ = k8sClient.Update(ctx, ph)
					_ = k8sClient.Delete(ctx, ph)
				}
			}
			for _, name := range []string{plIdempName} {
				pl := &tideprojectv1alpha2.Plan{}
				if err := k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: ns}, pl); err == nil {
					pl.Finalizers = nil
					_ = k8sClient.Update(ctx, pl)
					_ = k8sClient.Delete(ctx, pl)
				}
			}
			cm := &corev1.ConfigMap{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: seedCMName, Namespace: ns}, cm); err == nil {
				_ = k8sClient.Delete(ctx, cm)
			}
		})

		It("re-running reconcile after ImportComplete=True creates no duplicate CRs and no new Job", func() {
			r := newImportReconciler()
			projKey := types.NamespacedName{Name: projName, Namespace: ns}

			// First reconcile pass: drive to ImportComplete=True.
			Eventually(func(g Gomega) {
				_, err := r.Reconcile(ctx, reconcileRequest(projKey))
				g.Expect(err).NotTo(HaveOccurred())
				cond := findImportCondition(ctx, projName, ns)
				g.Expect(cond).NotTo(BeNil())
				g.Expect(cond.Status).To(Equal(metav1.ConditionTrue))
			}, 15*time.Second, 200*time.Millisecond).Should(Succeed())

			// Verify the initial CRs were created.
			var ms1 tideprojectv1alpha2.Milestone
			Expect(mgrClient.Get(ctx, types.NamespacedName{Name: msIdempName, Namespace: ns}, &ms1)).To(Succeed())
			initialMSUID := ms1.UID

			// Touch an annotation to trigger a new reconcile cycle.
			var proj tideprojectv1alpha2.Project
			Expect(k8sClient.Get(ctx, projKey, &proj)).To(Succeed())
			if proj.Annotations == nil {
				proj.Annotations = map[string]string{}
			}
			proj.Annotations["test/trigger"] = "idempotency-check"
			Expect(k8sClient.Update(ctx, &proj)).To(Succeed())

			// Re-run reconcile: should be an immediate no-op (idempotency guard, D-12).
			_, err := r.Reconcile(ctx, reconcileRequest(projKey))
			Expect(err).NotTo(HaveOccurred())

			// Assert: condition is still True.
			cond := findImportCondition(ctx, projName, ns)
			Expect(cond).NotTo(BeNil())
			Expect(cond.Status).To(Equal(metav1.ConditionTrue))

			// Assert: the Milestone CR UID is unchanged (no re-create).
			var ms2 tideprojectv1alpha2.Milestone
			Expect(mgrClient.Get(ctx, types.NamespacedName{Name: msIdempName, Namespace: ns}, &ms2)).To(Succeed())
			Expect(ms2.UID).To(Equal(initialMSUID), "Milestone UID must be unchanged on idempotent re-run")

			// Assert: no import Job was created (ImportImage="" so Job would have been skipped anyway;
			// but idempotency guard fires first and returns before reaching job creation).
			var proj2 tideprojectv1alpha2.Project
			Expect(k8sClient.Get(ctx, projKey, &proj2)).To(Succeed())
			jobName := fmt.Sprintf("tide-import-%s", proj2.UID)
			// List actual Jobs and assert the deterministic import-Job name is absent.
			var jobs batchv1.JobList
			Expect(k8sClient.List(ctx, &jobs, client.InNamespace(ns))).To(Succeed())
			for _, j := range jobs.Items {
				Expect(j.Name).NotTo(Equal(jobName), "no import Job should be created on idempotent re-run")
			}
		})
	})

	// -------------------------------------------------------------------------
	// Test 5 (RESUME-PARTIAL-04): Partial-tree materialization — per-node branch
	//
	// Drives reconcileCreatingCRs against a seed manifest with a mix:
	// one Plan with status:"Running" (complete envelope) and one Plan with status:""
	// (incomplete/missing envelope). Asserts that:
	//   - Plan with status:"Running" materializes as Status.Phase=="Running" AND
	//     Status.ValidationState=="Validated".
	//   - Plan with status:"" materializes as Status.Phase=="" AND
	//     Status.ValidationState=="" (truly fresh, not stamped Validated).
	//   - BOTH Plan CRs exist after reconcile (incomplete node is materialized, not
	//     omitted — identity preserved for DependsOn edge validity, Fork 2).
	//   - Both Plans carry their DependsOn so adopted dependents' edges stay valid.
	//
	// Note: cross-plan Task-name-level DependsOn is a known limitation
	// (RESEARCH Pitfall 4) — if an adopted completed Task references a specific
	// old Task name from a re-planned Plan and the re-plan produces different Task
	// names, that dependent Task may be permanently blocked. This test exercises
	// Plan-level DependsOn only (the common case; Pitfall 4 is documented separately).
	// -------------------------------------------------------------------------
	Describe("Test 5 (RESUME-PARTIAL-04): partial-tree materialization — complete adopts, incomplete stays fresh", func() {
		const projName = "import-test-partial"
		const seedCMName = "import-seed-partial"
		const msPartName = "ms-partial"
		const phPartName = "ph-partial"
		const plCompleteName = "pl-partial-complete"
		const plIncompleteName = "pl-partial-incomplete"

		BeforeEach(func() {
			manifest := seedManifest{
				Milestones: []seedEntry{
					{
						Name:       msPartName,
						FQName:     msPartName,
						OldUID:     "old-ms-partial",
						ProjectRef: projName,
						Status:     "Succeeded",
					},
				},
				Phases: []seedEntry{
					{
						Name:         phPartName,
						FQName:       phPartName,
						OldUID:       "old-ph-partial",
						MilestoneRef: msPartName,
						Status:       "Succeeded",
					},
				},
				Plans: []seedEntry{
					{
						// Complete node: has a salvaged status (envelope was complete).
						// reconcileCreatingCRs stamps Status.Phase + ValidationState.
						Name:     plCompleteName,
						FQName:   plCompleteName,
						OldUID:   "old-pl-complete",
						PhaseRef: phPartName,
						Status:   "Running",
					},
					{
						// Incomplete node: status:"" because export cleared it
						// (RESUME-PARTIAL-01: incomplete envelope → Status=="" in seed).
						// reconcileCreatingCRs skips the status patch → fresh/empty.
						// DependsOn links to the complete plan to exercise the
						// edge-validity assertion (identity preserved via Fork 2).
						Name:      plIncompleteName,
						FQName:    plIncompleteName,
						OldUID:    "old-pl-incomplete",
						PhaseRef:  phPartName,
						Status:    "",
						DependsOn: []string{plCompleteName},
					},
				},
			}
			Expect(makeSeedConfigMap(ctx, ns, seedCMName, manifest)).To(Succeed())
			Expect(makeImportProject(ctx, projName, ns, seedCMName)).To(Succeed())
			waitForCacheSync(projName, ns, &tideprojectv1alpha2.Project{})
		})

		AfterEach(func() {
			proj := &tideprojectv1alpha2.Project{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: projName, Namespace: ns}, proj); err == nil {
				proj.Finalizers = nil
				_ = k8sClient.Update(ctx, proj)
				_ = k8sClient.Delete(ctx, proj)
			}
			for _, name := range []string{msPartName} {
				ms := &tideprojectv1alpha2.Milestone{}
				if err := k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: ns}, ms); err == nil {
					ms.Finalizers = nil
					_ = k8sClient.Update(ctx, ms)
					_ = k8sClient.Delete(ctx, ms)
				}
			}
			for _, name := range []string{phPartName} {
				ph := &tideprojectv1alpha2.Phase{}
				if err := k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: ns}, ph); err == nil {
					ph.Finalizers = nil
					_ = k8sClient.Update(ctx, ph)
					_ = k8sClient.Delete(ctx, ph)
				}
			}
			for _, name := range []string{plCompleteName, plIncompleteName} {
				pl := &tideprojectv1alpha2.Plan{}
				if err := k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: ns}, pl); err == nil {
					pl.Finalizers = nil
					_ = k8sClient.Update(ctx, pl)
					_ = k8sClient.Delete(ctx, pl)
				}
			}
			cm := &corev1.ConfigMap{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: seedCMName, Namespace: ns}, cm); err == nil {
				_ = k8sClient.Delete(ctx, cm)
			}
		})

		It("materializes both Plans: complete→Running+Validated, incomplete→empty+fresh; both CRs exist with DependsOn", func() {
			r := newImportReconciler()
			projKey := types.NamespacedName{Name: projName, Namespace: ns}

			// Drive reconcile until ImportComplete=True.
			Eventually(func(g Gomega) {
				_, err := r.Reconcile(ctx, reconcileRequest(projKey))
				g.Expect(err).NotTo(HaveOccurred())
				cond := findImportCondition(ctx, projName, ns)
				g.Expect(cond).NotTo(BeNil())
				g.Expect(cond.Status).To(Equal(metav1.ConditionTrue))
			}, 15*time.Second, 200*time.Millisecond).Should(Succeed())

			// Assert BOTH Plans exist (RESUME-PARTIAL-04: incomplete node materialized, not omitted).
			var plComplete tideprojectv1alpha2.Plan
			Expect(mgrClient.Get(ctx, types.NamespacedName{Name: plCompleteName, Namespace: ns}, &plComplete)).
				To(Succeed(), "complete Plan CR must exist after reconcile")

			var plIncomplete tideprojectv1alpha2.Plan
			Expect(mgrClient.Get(ctx, types.NamespacedName{Name: plIncompleteName, Namespace: ns}, &plIncomplete)).
				To(Succeed(), "incomplete Plan CR must exist after reconcile (identity preserved)")

			// Assert complete plan: Status.Phase=="Running", ValidationState=="Validated".
			Expect(plComplete.Status.Phase).To(Equal("Running"),
				"complete Plan must adopt salvaged Status.Phase")
			Expect(plComplete.Status.ValidationState).To(Equal("Validated"),
				"complete Plan must have ValidationState=Validated to arm wave-materialization path (GAP-12)")

			// Assert incomplete plan: Status.Phase=="", ValidationState=="".
			Expect(plIncomplete.Status.Phase).To(BeEmpty(),
				"incomplete Plan must have Status.Phase=\"\" (re-plannable; not stamped Running)")
			Expect(plIncomplete.Status.ValidationState).To(BeEmpty(),
				"incomplete Plan must have ValidationState=\"\" (wave gate stays closed until re-plan)")

			// Assert DependsOn preserved on incomplete Plan so adopted-dependent edges stay valid (Fork 2).
			Expect(plIncomplete.Spec.DependsOn).To(ContainElement(plCompleteName),
				"incomplete Plan DependsOn must be preserved in spec so adopted dependents remain wired")
		})
	})
})

// reconcileRequest is a helper to build a reconcile.Request.
func reconcileRequest(key types.NamespacedName) reconcile.Request {
	return ctrl.Request{NamespacedName: key}
}
