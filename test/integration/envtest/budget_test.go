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

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	tideprojectv1alpha1 "github.com/jsquirrelz/tide/api/v1alpha1"
)

const budgetNamespace = "default"

var _ = Describe("Project budget cap enforcement", Label("envtest"), func() {
	ctx := context.Background()

	AfterEach(func() {
		projects := &tideprojectv1alpha1.ProjectList{}
		_ = k8sClient.List(ctx, projects, client.InNamespace(budgetNamespace))
		for i := range projects.Items {
			_ = k8sClient.Delete(ctx, &projects.Items[i])
		}
		pvcs := &corev1.PersistentVolumeClaimList{}
		_ = k8sClient.List(ctx, pvcs, client.InNamespace(budgetNamespace))
		for i := range pvcs.Items {
			_ = k8sClient.Delete(ctx, &pvcs.Items[i])
		}
	})

	// FAIL-04: a Project whose BudgetStatus.CostSpentCents exceeds AbsoluteCapCents
	// transitions to Phase=BudgetExceeded and dispatch halts.
	Describe("FAIL-04: budget exceeded halts dispatch", Label("FAIL-04"), func() {
		It("sets Project.Status.Phase=BudgetExceeded when CostSpentCents exceeds AbsoluteCapCents", func() {
			projectName := "budget-cap-project"
			makeBoundPVC(ctx, "tide-projects", budgetNamespace)

			project := &tideprojectv1alpha1.Project{
				ObjectMeta: metav1.ObjectMeta{
					Name:      projectName,
					Namespace: budgetNamespace,
				},
				Spec: tideprojectv1alpha1.ProjectSpec{
					TargetRepo: "https://github.com/example/budget-test.git",
					Budget: tideprojectv1alpha1.BudgetConfig{
						AbsoluteCapCents: 100, // $1.00 cap
					},
				},
			}
			Expect(k8sClient.Create(ctx, project)).To(Succeed())

			// Directly patch BudgetStatus to exceed the cap.
			Eventually(func() error {
				p := &tideprojectv1alpha1.Project{}
				if err := k8sClient.Get(ctx, client.ObjectKey{Name: projectName, Namespace: budgetNamespace}, p); err != nil {
					return err
				}
				p.Status.Budget.CostSpentCents = 200 // exceeds AbsoluteCapCents=100
				return k8sClient.Status().Update(ctx, p)
			}, "10s", "200ms").Should(Succeed())

			// After reconcile, the Project should be BudgetExceeded.
			Eventually(func() string {
				p := &tideprojectv1alpha1.Project{}
				if err := k8sClient.Get(ctx, client.ObjectKey{Name: projectName, Namespace: budgetNamespace}, p); err != nil {
					return ""
				}
				return p.Status.Phase
			}, "20s", "500ms").Should(Equal(tideprojectv1alpha1.PhaseBudgetExceeded),
				"Project.Status.Phase should be BudgetExceeded when cost exceeds cap")
		})
	})

	// FAIL-04: bypass annotation clears the BudgetExceeded halt.
	Describe("FAIL-04: bypass annotation clears BudgetExceeded", Label("FAIL-04"), func() {
		It("clears BudgetExceeded when bypass-budget=true annotation is applied", func() {
			projectName := "budget-bypass-project"
			makeBoundPVC(ctx, "tide-projects-bypass", budgetNamespace)

			project := &tideprojectv1alpha1.Project{
				ObjectMeta: metav1.ObjectMeta{
					Name:      projectName,
					Namespace: budgetNamespace,
				},
				Spec: tideprojectv1alpha1.ProjectSpec{
					TargetRepo: "https://github.com/example/bypass-test.git",
					Budget: tideprojectv1alpha1.BudgetConfig{
						AbsoluteCapCents: 50,
					},
				},
			}
			Expect(k8sClient.Create(ctx, project)).To(Succeed())

			// Force BudgetExceeded status.
			Eventually(func() error {
				p := &tideprojectv1alpha1.Project{}
				if err := k8sClient.Get(ctx, client.ObjectKey{Name: projectName, Namespace: budgetNamespace}, p); err != nil {
					return err
				}
				p.Status.Budget.CostSpentCents = 100
				p.Status.Phase = tideprojectv1alpha1.PhaseBudgetExceeded
				return k8sClient.Status().Update(ctx, p)
			}, "10s", "200ms").Should(Succeed())

			// Apply the one-shot bypass annotation.
			Eventually(func() error {
				p := &tideprojectv1alpha1.Project{}
				if err := k8sClient.Get(ctx, client.ObjectKey{Name: projectName, Namespace: budgetNamespace}, p); err != nil {
					return err
				}
				if p.Annotations == nil {
					p.Annotations = map[string]string{}
				}
				p.Annotations["tideproject.k8s/bypass-budget"] = "true"
				return k8sClient.Update(ctx, p)
			}, "10s", "200ms").Should(Succeed())

			// After reconcile, BudgetExceeded should be cleared.
			Eventually(func() string {
				p := &tideprojectv1alpha1.Project{}
				if err := k8sClient.Get(ctx, client.ObjectKey{Name: projectName, Namespace: budgetNamespace}, p); err != nil {
					return "error"
				}
				return p.Status.Phase
			}, "20s", "500ms").ShouldNot(Equal(tideprojectv1alpha1.PhaseBudgetExceeded),
				"BudgetExceeded should be cleared after bypass annotation is applied")
		})
	})
})

// makeBoundPVC creates a bound PVC for testing budget scenarios.
func makeBoundPVC(ctx context.Context, name, ns string) {
	// Idempotent — ignore AlreadyExists.
	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: ns,
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteMany},
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceStorage: resource.MustParse("1Gi"),
				},
			},
		},
	}
	_ = k8sClient.Create(ctx, pvc)
	// Patch the PVC status to Bound so the reconciler proceeds.
	existing := &corev1.PersistentVolumeClaim{}
	if err := k8sClient.Get(ctx, client.ObjectKey{Name: name, Namespace: ns}, existing); err == nil {
		pvcPatch := existing.DeepCopy()
		pvcPatch.Status.Phase = corev1.ClaimBound
		_ = k8sClient.Status().Update(ctx, pvcPatch)
	}
}
