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
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	tideprojectv1alpha1 "github.com/jsquirrelz/tide/api/v1alpha1"
)

var _ = Describe("Wave Controller", func() {
	Context("When reconciling a resource", func() {
		ctx := context.Background()

		It("accepts a valid Wave with PlanRef and non-negative WaveIndex (CRD-01, CRD-03)", func() {
			wave := &tideprojectv1alpha1.Wave{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "valid-wave",
					Namespace: "default",
				},
				Spec: tideprojectv1alpha1.WaveSpec{
					PlanRef:   "some-plan",
					WaveIndex: 0,
				},
			}
			Expect(k8sClient.Create(ctx, wave)).To(Succeed())

			By("Cleanup")
			Expect(k8sClient.Delete(ctx, wave)).To(Succeed())
		})

		It("rejects a Wave with WaveIndex=-1 (CEL Minimum=0)", func() {
			invalid := &tideprojectv1alpha1.Wave{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "negative-wave",
					Namespace: "default",
				},
				Spec: tideprojectv1alpha1.WaveSpec{
					PlanRef:   "some-plan",
					WaveIndex: -1,
				},
			}
			err := k8sClient.Create(ctx, invalid)
			Expect(err).To(HaveOccurred())
			Expect(apierrors.IsInvalid(err)).To(BeTrue(),
				"expected CEL Minimum=0 rejection, got: %v", err)
		})

		It("Owner-ref cascade: child reconcilers wire controller owner-refs up the chain (CRD-02, TestOwnerRefCascade, Pitfall 23)", func() {
			// envtest runs kube-apiserver + etcd but NOT the garbage-collector
			// controller, so deleting a parent does not asynchronously delete
			// children inside envtest. The cascade *contract* is that
			// child reconcilers wire controller-owner-refs (Controller=true,
			// BlockOwnerDeletion=true) via internal/owner.EnsureOwnerRef so a
			// real cluster's GC will cascade. We verify the owner-refs land
			// correctly down the entire Project → Milestone → Phase → Plan →
			// Wave chain — that's the assertion the cascade rests on.
			ns := "default"

			project := &tideprojectv1alpha1.Project{
				ObjectMeta: metav1.ObjectMeta{Name: "cascade-project", Namespace: ns},
				Spec: tideprojectv1alpha1.ProjectSpec{
					TargetRepo: "https://github.com/example/cascade.git",
				},
			}
			Expect(k8sClient.Create(ctx, project)).To(Succeed())

			milestone := &tideprojectv1alpha1.Milestone{
				ObjectMeta: metav1.ObjectMeta{Name: "cascade-milestone", Namespace: ns},
				Spec:       tideprojectv1alpha1.MilestoneSpec{ProjectRef: project.Name},
			}
			Expect(k8sClient.Create(ctx, milestone)).To(Succeed())

			phase := &tideprojectv1alpha1.Phase{
				ObjectMeta: metav1.ObjectMeta{Name: "cascade-phase", Namespace: ns},
				Spec:       tideprojectv1alpha1.PhaseSpec{MilestoneRef: milestone.Name},
			}
			Expect(k8sClient.Create(ctx, phase)).To(Succeed())

			plan := &tideprojectv1alpha1.Plan{
				ObjectMeta: metav1.ObjectMeta{Name: "cascade-plan", Namespace: ns},
				Spec:       tideprojectv1alpha1.PlanSpec{PhaseRef: phase.Name},
			}
			Expect(k8sClient.Create(ctx, plan)).To(Succeed())

			wave := &tideprojectv1alpha1.Wave{
				ObjectMeta: metav1.ObjectMeta{Name: "cascade-wave", Namespace: ns},
				Spec:       tideprojectv1alpha1.WaveSpec{PlanRef: plan.Name, WaveIndex: 0},
			}
			Expect(k8sClient.Create(ctx, wave)).To(Succeed())

			projectR := &ProjectReconciler{Client: k8sClient, Scheme: k8sClient.Scheme()}
			milestoneR := &MilestoneReconciler{Client: k8sClient, Scheme: k8sClient.Scheme()}
			phaseR := &PhaseReconciler{Client: k8sClient, Scheme: k8sClient.Scheme()}
			planR := &PlanReconciler{Client: k8sClient, Scheme: k8sClient.Scheme()}
			waveR := &WaveReconciler{Client: k8sClient, Scheme: k8sClient.Scheme()}

			projectName := types.NamespacedName{Name: project.Name, Namespace: ns}
			milestoneName := types.NamespacedName{Name: milestone.Name, Namespace: ns}
			phaseName := types.NamespacedName{Name: phase.Name, Namespace: ns}
			planName := types.NamespacedName{Name: plan.Name, Namespace: ns}
			waveName := types.NamespacedName{Name: wave.Name, Namespace: ns}

			// Drive each reconciler through enough passes to (a) add its
			// finalizer (which returns on pass 1) and (b) set the owner-ref
			// to its parent (on pass 2). Three passes gives slack for the
			// resource-version conflicts that an in-process test sometimes hits.
			for i := 0; i < 3; i++ {
				_, err := projectR.Reconcile(ctx, reconcile.Request{NamespacedName: projectName})
				Expect(err).NotTo(HaveOccurred())
				_, err = milestoneR.Reconcile(ctx, reconcile.Request{NamespacedName: milestoneName})
				Expect(err).NotTo(HaveOccurred())
				_, err = phaseR.Reconcile(ctx, reconcile.Request{NamespacedName: phaseName})
				Expect(err).NotTo(HaveOccurred())
				_, err = planR.Reconcile(ctx, reconcile.Request{NamespacedName: planName})
				Expect(err).NotTo(HaveOccurred())
				_, err = waveR.Reconcile(ctx, reconcile.Request{NamespacedName: waveName})
				Expect(err).NotTo(HaveOccurred())
			}

			// Verify the controller-owner-ref chain. Each child reports
			// exactly one OwnerReference with Controller=true pointing to
			// its parent Kind. BlockOwnerDeletion=true ensures a real
			// cluster's GC will cascade.
			expectControllerOwner := func(child metav1.Object, parentKind, parentName string) {
				owners := child.GetOwnerReferences()
				Expect(owners).NotTo(BeEmpty(),
					"%s/%s missing owner reference", parentKind, child.GetName())
				found := false
				for _, o := range owners {
					if o.Kind == parentKind && o.Name == parentName {
						Expect(o.Controller).NotTo(BeNil())
						Expect(*o.Controller).To(BeTrue(),
							"expected Controller=true for %s -> %s/%s", child.GetName(), parentKind, parentName)
						Expect(o.BlockOwnerDeletion).NotTo(BeNil())
						Expect(*o.BlockOwnerDeletion).To(BeTrue(),
							"expected BlockOwnerDeletion=true for %s -> %s/%s", child.GetName(), parentKind, parentName)
						found = true
						break
					}
				}
				Expect(found).To(BeTrue(),
					"%s expected to point at %s/%s", child.GetName(), parentKind, parentName)
			}

			fetchedMilestone := &tideprojectv1alpha1.Milestone{}
			Expect(k8sClient.Get(ctx, milestoneName, fetchedMilestone)).To(Succeed())
			expectControllerOwner(fetchedMilestone, "Project", project.Name)

			fetchedPhase := &tideprojectv1alpha1.Phase{}
			Expect(k8sClient.Get(ctx, phaseName, fetchedPhase)).To(Succeed())
			expectControllerOwner(fetchedPhase, "Milestone", milestone.Name)

			fetchedPlan := &tideprojectv1alpha1.Plan{}
			Expect(k8sClient.Get(ctx, planName, fetchedPlan)).To(Succeed())
			expectControllerOwner(fetchedPlan, "Phase", phase.Name)

			fetchedWave := &tideprojectv1alpha1.Wave{}
			Expect(k8sClient.Get(ctx, waveName, fetchedWave)).To(Succeed())
			expectControllerOwner(fetchedWave, "Plan", plan.Name)

			// Cleanup: drive each reconciler to completion after deletion.
			// envtest doesn't run GC; we manually delete each child + drive
			// its reconciler so finalizers come off and the object goes away.
			Expect(k8sClient.Delete(ctx, wave)).To(Succeed())
			Eventually(func() bool {
				_, _ = waveR.Reconcile(ctx, reconcile.Request{NamespacedName: waveName})
				return apierrors.IsNotFound(k8sClient.Get(ctx, waveName, &tideprojectv1alpha1.Wave{}))
			}, 5*time.Second, 100*time.Millisecond).Should(BeTrue())

			Expect(k8sClient.Delete(ctx, plan)).To(Succeed())
			Eventually(func() bool {
				_, _ = planR.Reconcile(ctx, reconcile.Request{NamespacedName: planName})
				return apierrors.IsNotFound(k8sClient.Get(ctx, planName, &tideprojectv1alpha1.Plan{}))
			}, 5*time.Second, 100*time.Millisecond).Should(BeTrue())

			Expect(k8sClient.Delete(ctx, phase)).To(Succeed())
			Eventually(func() bool {
				_, _ = phaseR.Reconcile(ctx, reconcile.Request{NamespacedName: phaseName})
				return apierrors.IsNotFound(k8sClient.Get(ctx, phaseName, &tideprojectv1alpha1.Phase{}))
			}, 5*time.Second, 100*time.Millisecond).Should(BeTrue())

			Expect(k8sClient.Delete(ctx, milestone)).To(Succeed())
			Eventually(func() bool {
				_, _ = milestoneR.Reconcile(ctx, reconcile.Request{NamespacedName: milestoneName})
				return apierrors.IsNotFound(k8sClient.Get(ctx, milestoneName, &tideprojectv1alpha1.Milestone{}))
			}, 5*time.Second, 100*time.Millisecond).Should(BeTrue())

			Expect(k8sClient.Delete(ctx, project)).To(Succeed())
			Eventually(func() bool {
				_, _ = projectR.Reconcile(ctx, reconcile.Request{NamespacedName: projectName})
				return apierrors.IsNotFound(k8sClient.Get(ctx, projectName, &tideprojectv1alpha1.Project{}))
			}, 5*time.Second, 100*time.Millisecond).Should(BeTrue())
		})
	})
})
