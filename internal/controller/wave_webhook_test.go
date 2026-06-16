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

	tideprojectv1alpha1 "github.com/jsquirrelz/tide/api/v1alpha2"
)

// WaveCustomValidator no-op behavior (Plan 07 Task 2 / revision Warning 9).
//
// Phase 1 contract: the webhook endpoint is registered with the Manager and
// fires on every CRUD, but every Validate* method returns (nil, nil) so any
// schema-valid Wave is admitted. Phase 2 wires the D-B1 rejection (only the
// WaveReconciler should produce Waves; client applies rejected) behind the
// same seam without restructuring this test.
//
// Note: in Phase 1 envtest, schema-valid Waves applied by the test client
// are admitted because the webhook is intentionally no-op. Once Phase 2
// wires D-B1, these specs will need to stamp the WaveReconciler owner-ref
// or be moved to assertions about the rejection itself.
var _ = Describe("WaveCustomValidator (Phase 1 no-op — D-B1 rejection wires in Phase 2)", func() {
	const namespace = "default"

	AfterEach(func() {
		waves := &tideprojectv1alpha1.WaveList{}
		_ = k8sClient.List(ctx, waves, client.InNamespace(namespace))
		for i := range waves.Items {
			_ = k8sClient.Delete(ctx, &waves.Items[i])
		}
	})

	It("rejects client-applied waves per D-B1", func() {
		wave := &tideprojectv1alpha1.Wave{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "webhook-create-wave",
				Namespace: namespace,
			},
			Spec: tideprojectv1alpha1.WaveSpec{
				PlanRef:   "some-plan",
				WaveIndex: 0,
			},
		}
		err := k8sClient.Create(ctx, wave)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("client-applied Waves not allowed"))
	})

	It("allows Reconciler-applied waves (has owner ref)", func() {
		wave := &tideprojectv1alpha1.Wave{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "webhook-create-wave-succ",
				Namespace: namespace,
				OwnerReferences: []metav1.OwnerReference{
					{APIVersion: "tideproject.k8s/v1alpha1", Kind: "Plan", Name: "some-plan", UID: "dummy-uid"},
				},
			},
			Spec: tideprojectv1alpha1.WaveSpec{
				PlanRef:   "some-plan",
				WaveIndex: 0,
			},
		}
		Expect(k8sClient.Create(ctx, wave)).To(Succeed())
	})

	It("allows ValidateUpdate (Phase 1 no-op)", func() {
		wave := &tideprojectv1alpha1.Wave{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "webhook-update-wave",
				Namespace: namespace,
				OwnerReferences: []metav1.OwnerReference{
					{APIVersion: "tideproject.k8s/v1alpha1", Kind: "Plan", Name: "plan-a", UID: "dummy-uid"},
				},
			},
			Spec: tideprojectv1alpha1.WaveSpec{
				PlanRef:   "plan-a",
				WaveIndex: 0,
			},
		}
		Expect(k8sClient.Create(ctx, wave)).To(Succeed())

		Eventually(func() error {
			fresh := &tideprojectv1alpha1.Wave{}
			if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(wave), fresh); err != nil {
				return err
			}
			fresh.Spec.WaveIndex = 1
			return k8sClient.Update(ctx, fresh)
		}, "5s", "100ms").Should(Succeed(),
			"Wave update should succeed — Phase 1 validator is no-op")
	})

	It("allows ValidateDelete (Phase 1 no-op)", func() {
		wave := &tideprojectv1alpha1.Wave{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "webhook-delete-wave",
				Namespace: namespace,
				OwnerReferences: []metav1.OwnerReference{
					{APIVersion: "tideproject.k8s/v1alpha1", Kind: "Plan", Name: "some-plan", UID: "dummy-uid"},
				},
			},
			Spec: tideprojectv1alpha1.WaveSpec{
				PlanRef:   "some-plan",
				WaveIndex: 0,
			},
		}
		Expect(k8sClient.Create(ctx, wave)).To(Succeed())
		Expect(k8sClient.Delete(ctx, wave)).To(Succeed(),
			"Wave delete should succeed — Phase 1 validator is no-op")
	})

	It("rejects WaveIndex=-1 (CEL Minimum=0, NOT the webhook)", func() {
		// Sanity check that the Plan 05 CEL Minimum=0 marker is still the
		// authoritative gate for the WaveIndex non-negativity invariant.
		// The webhook does not fire on schema-invalid objects.
		bad := &tideprojectv1alpha1.Wave{
			ObjectMeta: metav1.ObjectMeta{Name: "bad-wave", Namespace: namespace},
			Spec: tideprojectv1alpha1.WaveSpec{
				PlanRef:   "some-plan",
				WaveIndex: -1,
			},
		}
		err := k8sClient.Create(ctx, bad)
		Expect(err).To(HaveOccurred(),
			"WaveIndex=-1 should be rejected by CEL Minimum=0, not the webhook (Plan 05's CEL marker)")
		Expect(apierrors.IsInvalid(err) || apierrors.IsBadRequest(err)).To(BeTrue(),
			"expected schema rejection, got: %v", err)
	})
})
