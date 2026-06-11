/*
Copyright 2026 TIDE Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

// Phase 9 plan 09-05 — SC-1 envtest integration test for reporter_materialize.
//
// Validates the reporter's materialize path against a real envtest apiserver:
//   - reporter.MaterializeChildCRDs creates child CRs in the parent's namespace
//   - child CRs have a controller ownerRef pointing at the parent (same-namespace)
//   - child CRs have the spec-parent-ref set (e.g. Phase.spec.milestoneRef)
//   - idempotent re-run does not create duplicate children
//
// This is the envtest cross-ns-create integration level; unit-level coverage
// lives in internal/reporter/materialize_test.go (plan 09-04).
package envtest_integration

import (
	"context"
	"encoding/json"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	tideprojectv1alpha1 "github.com/jsquirrelz/tide/api/v1alpha1"
	"github.com/jsquirrelz/tide/internal/reporter"
	pkgdispatch "github.com/jsquirrelz/tide/pkg/dispatch"
)

// reporterNS is the namespace for reporter materialize tests.
const reporterNS = "default"

// rmPhaseSpec encodes a PhaseSpec as a runtime.RawExtension for use in ChildCRDSpec.
func rmPhaseSpec(milestoneRef string) runtime.RawExtension {
	raw, err := json.Marshal(tideprojectv1alpha1.PhaseSpec{MilestoneRef: milestoneRef})
	ExpectWithOffset(1, err).NotTo(HaveOccurred(), "marshal PhaseSpec")
	return runtime.RawExtension{Raw: raw}
}

var _ = Describe("Phase 9 — reporter materialize (envtest)", Label("envtest", "phase9", "reporter-materialize"), func() {
	ctx := context.Background()

	// Clean up all Milestones and Phases in reporterNS after each spec to prevent
	// name collisions across the Describe suite. (The suite-level manager may
	// reconcile these objects; Gomega Eventually in AfterEach is safe.)
	AfterEach(func() {
		phases := &tideprojectv1alpha1.PhaseList{}
		_ = k8sClient.List(ctx, phases, client.InNamespace(reporterNS))
		for i := range phases.Items {
			_ = k8sClient.Delete(ctx, &phases.Items[i])
		}
		milestones := &tideprojectv1alpha1.MilestoneList{}
		_ = k8sClient.List(ctx, milestones, client.InNamespace(reporterNS))
		for i := range milestones.Items {
			_ = k8sClient.Delete(ctx, &milestones.Items[i])
		}
	})

	// TC-1: reporter creates child CR in the parent's namespace with controller
	// ownerRef and spec-parent-ref set.
	Describe("TC-1: create child CR with ownerRef + specRef", func() {
		It("creates a Phase with Milestone ownerRef and milestoneRef set", func() {
			milestoneName := "rm-milestone-tc1"
			milestone := &tideprojectv1alpha1.Milestone{
				ObjectMeta: metav1.ObjectMeta{
					Name:      milestoneName,
					Namespace: reporterNS,
				},
				Spec: tideprojectv1alpha1.MilestoneSpec{ProjectRef: "rm-project-tc1"},
			}
			Expect(k8sClient.Create(ctx, milestone)).To(Succeed())

			// Fetch with live UID (envtest sets UID on create).
			Expect(k8sClient.Get(ctx, client.ObjectKey{Namespace: reporterNS, Name: milestoneName}, milestone)).To(Succeed())
			Expect(milestone.UID).NotTo(BeEmpty(), "envtest must set UID")

			children := []pkgdispatch.ChildCRDSpec{
				{Kind: "Phase", Name: "rm-phase-tc1", Spec: rmPhaseSpec(milestoneName)},
			}

			// Use the envtest k8sClient (not the manager's cached mgrClient) so the
			// create is direct and not subject to the manager's watch-triggered rate-limit.
			Expect(reporter.MaterializeChildCRDs(ctx, k8sClient, mgrClient.Scheme(), milestone, children)).To(Succeed())

			// Assert: Phase exists in the parent's namespace.
			var phase tideprojectv1alpha1.Phase
			Expect(k8sClient.Get(ctx, client.ObjectKey{Namespace: reporterNS, Name: "rm-phase-tc1"}, &phase)).To(Succeed())

			// Assert: Phase namespace = parent namespace (same-namespace ownerRef).
			Expect(phase.Namespace).To(Equal(reporterNS))

			// Assert: controller ownerRef pointing at the Milestone.
			refs := phase.GetOwnerReferences()
			Expect(refs).NotTo(BeEmpty(), "Phase must have ownerRef")
			var foundRef bool
			for _, r := range refs {
				if r.Kind == "Milestone" && r.UID == milestone.UID {
					Expect(r.Controller).NotTo(BeNil(), "controller bool must be set")
					Expect(*r.Controller).To(BeTrue(), "ownerRef.Controller must be true")
					foundRef = true
				}
			}
			Expect(foundRef).To(BeTrue(), "Phase must have a controller ownerRef with UID=%s", milestone.UID)

			// Assert: spec-parent-ref set (Phase.spec.milestoneRef = milestoneName).
			Expect(phase.Spec.MilestoneRef).To(Equal(milestoneName), "spec.milestoneRef must match parent name")
		})
	})

	// TC-2: idempotent re-run — second call to MaterializeChildCRDs does not
	// create a duplicate child (ChildrenAlreadyMaterialized short-circuits).
	Describe("TC-2: idempotent re-run", func() {
		It("does not create a duplicate Phase on second materialize", func() {
			milestoneName := "rm-milestone-tc2"
			milestone := &tideprojectv1alpha1.Milestone{
				ObjectMeta: metav1.ObjectMeta{
					Name:      milestoneName,
					Namespace: reporterNS,
				},
				Spec: tideprojectv1alpha1.MilestoneSpec{ProjectRef: "rm-project-tc2"},
			}
			Expect(k8sClient.Create(ctx, milestone)).To(Succeed())
			Expect(k8sClient.Get(ctx, client.ObjectKey{Namespace: reporterNS, Name: milestoneName}, milestone)).To(Succeed())

			children := []pkgdispatch.ChildCRDSpec{
				{Kind: "Phase", Name: "rm-phase-tc2", Spec: rmPhaseSpec(milestoneName)},
			}

			// First materialize — creates the child.
			Expect(reporter.MaterializeChildCRDs(ctx, k8sClient, mgrClient.Scheme(), milestone, children)).To(Succeed())

			// Verify child exists before second call.
			var phase tideprojectv1alpha1.Phase
			Expect(k8sClient.Get(ctx, client.ObjectKey{Namespace: reporterNS, Name: "rm-phase-tc2"}, &phase)).To(Succeed())
			firstUID := phase.UID

			// Second materialize — must not error (idempotent).
			already, err := reporter.ChildrenAlreadyMaterialized(ctx, k8sClient, milestone)
			Expect(err).NotTo(HaveOccurred())
			Expect(already).To(BeTrue(), "ChildrenAlreadyMaterialized must return true after first materialize")

			// Invoking MaterializeChildCRDs again also succeeds (AlreadyExists = idempotent).
			Expect(reporter.MaterializeChildCRDs(ctx, k8sClient, mgrClient.Scheme(), milestone, children)).To(Succeed())

			// The child UID is unchanged (no replacement).
			var phaseAfter tideprojectv1alpha1.Phase
			Expect(k8sClient.Get(ctx, client.ObjectKey{Namespace: reporterNS, Name: "rm-phase-tc2"}, &phaseAfter)).To(Succeed())
			Expect(phaseAfter.UID).To(Equal(firstUID), "Phase UID must not change on idempotent re-materialize")

			// Only one Phase with this milestoneRef in the namespace.
			// Use a manual filter — .spec.milestoneRef is not registered as a field
			// indexer on k8sClient (only on mgrClient via SetupWithManager). A List +
			// manual filter is reliable across both the envtest and fake client.
			var allPhases tideprojectv1alpha1.PhaseList
			Expect(k8sClient.List(ctx, &allPhases, client.InNamespace(reporterNS))).To(Succeed())
			count := 0
			for _, ph := range allPhases.Items {
				if ph.Spec.MilestoneRef == milestoneName {
					count++
				}
			}
			Expect(count).To(Equal(1), "should be exactly 1 Phase with milestoneRef=%s after two materializes", milestoneName)
		})
	})

	// TC-3: fixture compatibility — both stub-authored and real-authored EnvelopeOut
	// shapes populate ChildCRDs identically (RESEARCH §Stub Compatibility). Using
	// both forms in one test ensures the reporter's materialize path handles either.
	Describe("TC-3: stub-authored and real-authored fixture shapes", func() {
		It("materializes children from a fixture EnvelopeOut as written by stub or real subagent", func() {
			milestoneName := "rm-milestone-tc3"
			milestone := &tideprojectv1alpha1.Milestone{
				ObjectMeta: metav1.ObjectMeta{
					Name:      milestoneName,
					Namespace: reporterNS,
				},
				Spec: tideprojectv1alpha1.MilestoneSpec{ProjectRef: "rm-project-tc3"},
			}
			Expect(k8sClient.Create(ctx, milestone)).To(Succeed())
			Expect(k8sClient.Get(ctx, client.ObjectKey{Namespace: reporterNS, Name: milestoneName}, milestone)).To(Succeed())

			// Fixture out.json that mirrors what both stub and real subagent write.
			// Both serialize ChildCRDs as a JSON array of ChildCRDSpec objects with Kind/Name/Spec.
			fixtureJSON := `{
				"apiVersion": "tideproject.k8s/v1alpha1",
				"kind": "TaskEnvelopeOut",
				"taskUID": "task-uid-tc3",
				"exitCode": 0,
				"childCRDs": [
					{"kind": "Phase", "name": "rm-phase-tc3a", "spec": {"milestoneRef": "rm-milestone-tc3"}},
					{"kind": "Phase", "name": "rm-phase-tc3b", "spec": {"milestoneRef": "rm-milestone-tc3"}}
				]
			}`

			var envOut pkgdispatch.EnvelopeOut
			Expect(json.Unmarshal([]byte(fixtureJSON), &envOut)).To(Succeed())
			Expect(envOut.ChildCRDs).To(HaveLen(2))

			Expect(reporter.MaterializeChildCRDs(ctx, k8sClient, mgrClient.Scheme(), milestone, envOut.ChildCRDs)).To(Succeed())

			// Both child Phases must exist.
			for _, phaseName := range []string{"rm-phase-tc3a", "rm-phase-tc3b"} {
				var ph tideprojectv1alpha1.Phase
				Expect(k8sClient.Get(ctx, client.ObjectKey{Namespace: reporterNS, Name: phaseName}, &ph)).
					To(Succeed(), "Phase %q must exist after materialize", phaseName)
				Expect(ph.Spec.MilestoneRef).To(Equal(milestoneName))
			}
		})
	})
})
