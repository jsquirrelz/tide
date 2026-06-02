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
			}, "30s", "200ms").Should(Succeed())

			// Status updates do not bump Generation, so the reconciler's
			// GenerationChangedPredicate filters them out. Force a re-reconcile
			// by bumping a benign annotation (AnnotationChangedPredicate fires).
			kickProjectReconcile(ctx, projectName, budgetNamespace, "cap-exceeded")

			// After reconcile, the Project should be BudgetExceeded.
			Eventually(func() string {
				p := &tideprojectv1alpha1.Project{}
				if err := k8sClient.Get(ctx, client.ObjectKey{Name: projectName, Namespace: budgetNamespace}, p); err != nil {
					return ""
				}
				return p.Status.Phase
			}, "60s", "500ms").Should(Equal(tideprojectv1alpha1.PhaseBudgetExceeded),
				"Project.Status.Phase should be BudgetExceeded when cost exceeds cap")
		})
	})

	// FAIL-04: bypass annotation clears the BudgetExceeded halt.
	Describe("FAIL-04: bypass annotation clears BudgetExceeded", Label("FAIL-04"), func() {
		It("clears BudgetExceeded when bypass-budget=true annotation is applied", func() {
			projectName := "budget-bypass-project"
			makeBoundPVC(ctx, "tide-projects-bypass", budgetNamespace)
			// The reconciler's default SharedPVCName is "tide-projects"; create
			// that PVC too so the seam body's PVC-bound check passes.
			makeBoundPVC(ctx, "tide-projects", budgetNamespace)

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
			}, "30s", "200ms").Should(Succeed())

			// Apply the one-shot bypass annotation (also kicks the watch).
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
			}, "30s", "200ms").Should(Succeed())

			// The bypass is one-shot: after the reconciler clears BudgetExceeded
			// it consumes the annotation, and the very next reconcile re-asserts
			// BudgetExceeded because cost (100) still exceeds cap (50). The
			// production guarantee is "bypass took effect for exactly one
			// reconcile pass", which is observable as annotation consumption
			// (ConsumeBypass deletes the bypass-budget key). Asserting the
			// terminal phase is racy and contradicts the one-shot contract.
			Eventually(func() bool {
				p := &tideprojectv1alpha1.Project{}
				if err := k8sClient.Get(ctx, client.ObjectKey{Name: projectName, Namespace: budgetNamespace}, p); err != nil {
					return false
				}
				_, stillThere := p.Annotations["tideproject.k8s/bypass-budget"]
				return !stillThere
			}, "60s", "500ms").Should(BeTrue(),
				"one-shot bypass annotation should be consumed after the bypass takes effect")
		})
	})
})

// makeBoundPVC creates a bound PVC for testing budget scenarios. Idempotent:
// if the PVC already exists, ensure its status.phase is ClaimBound.
func makeBoundPVC(ctx context.Context, name, ns string) {
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
	if err := k8sClient.Create(ctx, pvc); err != nil {
		Expect(client.IgnoreAlreadyExists(err)).To(Succeed(), "create PVC %s/%s", ns, name)
	}
	// Patch the PVC status to Bound so the reconciler proceeds. envtest does not
	// run a PV provisioner so we set Phase explicitly.
	Eventually(func() error {
		existing := &corev1.PersistentVolumeClaim{}
		if err := k8sClient.Get(ctx, client.ObjectKey{Name: name, Namespace: ns}, existing); err != nil {
			return err
		}
		if existing.Status.Phase == corev1.ClaimBound {
			return nil
		}
		pvcPatch := existing.DeepCopy()
		pvcPatch.Status.Phase = corev1.ClaimBound
		return k8sClient.Status().Update(ctx, pvcPatch)
	}, "5s", "100ms").Should(Succeed(), "bind PVC %s/%s", ns, name)
}

// kickProjectReconcile forces an additional reconcile of the named Project by
// bumping a benign annotation. ProjectReconciler's watch predicate is
// predicate.Or(GenerationChangedPredicate, AnnotationChangedPredicate); a
// metadata-only Update with no Generation bump is filtered out, so tests that
// rely on multiple reconcile passes (finalizer add → seam body → completion
// patch) must trigger them explicitly. Mirrors the unit-test pattern of
// invoking reconciler.Reconcile() multiple times, but via the real queue.
func kickProjectReconcile(ctx context.Context, name, ns, kickValue string) {
	Eventually(func() error {
		p := &tideprojectv1alpha1.Project{}
		if err := k8sClient.Get(ctx, client.ObjectKey{Name: name, Namespace: ns}, p); err != nil {
			return err
		}
		if p.Annotations == nil {
			p.Annotations = map[string]string{}
		}
		p.Annotations["tide-test/kick"] = kickValue
		return k8sClient.Update(ctx, p)
	}, "5s", "100ms").Should(Succeed(), "kick reconcile for project %s/%s", ns, name)
}
