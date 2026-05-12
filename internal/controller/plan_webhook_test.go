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
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	tideprojectv1alpha1 "github.com/jsquirrelz/tide/api/v1alpha1"
)

// PlanCustomValidator no-op behavior (Plan 07 Task 2 / revision Warning 9).
//
// Phase 1 contract: the webhook endpoint is registered with the Manager and
// fires on every CRUD, but every Validate* method returns (nil, nil) so any
// schema-valid Plan is admitted. Phase 2's REQ-PLAN-01 wire-in (cycle
// detection via pkg/dag.ComputeWaves per D-B3) lives behind the same seam
// without restructuring this test.
var _ = Describe("PlanCustomValidator (Phase 1 no-op)", func() {
	const namespace = "default"

	AfterEach(func() {
		// Best-effort cleanup; the controllers' finalizers may keep the object
		// in Terminating state because envtest doesn't run the GC controller,
		// but the Delete request itself exercises ValidateDelete and that's
		// what matters for this suite.
		plans := &tideprojectv1alpha1.PlanList{}
		_ = k8sClient.List(ctx, plans, client.InNamespace(namespace))
		for i := range plans.Items {
			_ = k8sClient.Delete(ctx, &plans.Items[i])
		}
	})

	It("allows ValidateCreate (Phase 1 no-op)", func() {
		plan := &tideprojectv1alpha1.Plan{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "webhook-create-plan",
				Namespace: namespace,
			},
			Spec: tideprojectv1alpha1.PlanSpec{
				PhaseRef: "some-phase",
			},
		}
		Expect(k8sClient.Create(ctx, plan)).To(Succeed(),
			"Plan create should succeed — Phase 1 validator is no-op (REQ-PLAN-01 cycle detection wires in Phase 2)")
	})

	It("allows ValidateUpdate (Phase 1 no-op)", func() {
		plan := &tideprojectv1alpha1.Plan{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "webhook-update-plan",
				Namespace: namespace,
			},
			Spec: tideprojectv1alpha1.PlanSpec{
				PhaseRef: "phase-a",
			},
		}
		Expect(k8sClient.Create(ctx, plan)).To(Succeed())

		// Re-fetch then mutate to avoid resource-version conflicts with the
		// PlanReconciler's finalizer/owner-ref stamping.
		Eventually(func() error {
			fresh := &tideprojectv1alpha1.Plan{}
			if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(plan), fresh); err != nil {
				return err
			}
			fresh.Spec.PhaseRef = "phase-b"
			return k8sClient.Update(ctx, fresh)
		}, "5s", "100ms").Should(Succeed(),
			"Plan update should succeed — Phase 1 validator is no-op")
	})

	It("allows ValidateDelete (Phase 1 no-op)", func() {
		plan := &tideprojectv1alpha1.Plan{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "webhook-delete-plan",
				Namespace: namespace,
			},
			Spec: tideprojectv1alpha1.PlanSpec{
				PhaseRef: "some-phase",
			},
		}
		Expect(k8sClient.Create(ctx, plan)).To(Succeed())
		// Delete itself only exercises the ValidateDelete admission path. The
		// object may linger in Terminating because of the finalizer + missing
		// GC controller in envtest; that's fine — the webhook fired.
		Expect(k8sClient.Delete(ctx, plan)).To(Succeed(),
			"Plan delete should succeed — Phase 1 validator is no-op")
	})

	It("rejects a Plan with empty PhaseRef (CEL MinLength=1, not the webhook)", func() {
		// Sanity check that schema-level CEL (added by Plan 05) is still the
		// authoritative gate for non-graph invariants. The webhook does NOT
		// fire on schema-invalid objects because admission decoder rejects
		// them earlier.
		bad := &tideprojectv1alpha1.Plan{
			ObjectMeta: metav1.ObjectMeta{Name: "bad-plan", Namespace: namespace},
			Spec:       tideprojectv1alpha1.PlanSpec{PhaseRef: ""},
		}
		err := k8sClient.Create(ctx, bad)
		Expect(err).To(HaveOccurred(),
			"empty PhaseRef should be rejected by CEL/MinLength schema validation, not the webhook")
		Expect(apierrors.IsInvalid(err) || apierrors.IsBadRequest(err)).To(BeTrue(),
			"expected schema rejection, got: %v", err)
	})
})
