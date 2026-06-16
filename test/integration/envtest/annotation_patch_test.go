/*
Copyright 2026 TIDE Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

// annotation_patch_test.go — WR-03 + WR-14 regression test.
//
// The reconcilers consume annotations via the pattern:
//
//   newAnno := gates.ConsumeApprove(obj, "<level>")   // (or budget.ConsumeBypass)
//   patch := client.MergeFrom(obj.DeepCopy())
//   obj.SetAnnotations(newAnno)
//   r.Patch(ctx, obj, patch)
//
// This works because controller-runtime's MergeFrom emits an RFC 7396 JSON
// merge patch and for metadata.annotations the diff sends the entire (new)
// annotations map, replacing the prior value and dropping the approve key.
//
// WR-03 / WR-14 risk: a future controller-runtime refactor that diffs
// annotation sub-paths instead of replacing the whole map could silently
// regress the consume semantics — the same risk applies to any caller
// that switches to client.StrategicMergeFrom (strategic merge patch treats
// map elements as merge keys).
//
// This test asserts the apiserver-side semantics directly:
//   1. Create a CRD with annotations {approve-X: "true", other: "y"}.
//   2. Build the in-memory consumed map via gates.ConsumeApprove (or
//      budget.ConsumeBypass).
//   3. Apply the MergeFrom patch.
//   4. Get the CRD back from the apiserver and assert approve-X is gone
//      AND other is preserved.

package envtest_integration

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	tideprojectv1alpha1 "github.com/jsquirrelz/tide/api/v1alpha2"
	"github.com/jsquirrelz/tide/internal/budget"
	"github.com/jsquirrelz/tide/internal/gates"
)

// projectAnnotationCleanup deletes a Project + clears any finalizers so
// re-runs of the test do not collide on the same Project name.
func projectAnnotationCleanup(name string) {
	var p tideprojectv1alpha1.Project
	if err := k8sClient.Get(context.Background(), types.NamespacedName{Name: name, Namespace: "default"}, &p); err == nil {
		p.Finalizers = nil
		_ = k8sClient.Update(context.Background(), &p)
		_ = k8sClient.Delete(context.Background(), &p)
	}
}

func milestoneAnnotationCleanup(name string) {
	var m tideprojectv1alpha1.Milestone
	if err := k8sClient.Get(context.Background(), types.NamespacedName{Name: name, Namespace: "default"}, &m); err == nil {
		m.Finalizers = nil
		_ = k8sClient.Update(context.Background(), &m)
		_ = k8sClient.Delete(context.Background(), &m)
	}
}

var _ = Describe("Annotation removal via MergeFrom is apiserver-observable (WR-03 / WR-14)",
	Label("envtest", "phase4", "wr-03", "wr-14"), func() {
		ctx := context.Background()

		// Helper: assert that after the MergeFrom-based patch the named annotation
		// is gone from the apiserver-side object AND the other annotations are
		// preserved.
		assertAnnotationRemovedFromAPIServer := func(getFromAPI func() map[string]string, removedKey, preservedKey, preservedVal string) {
			Eventually(func(g Gomega) {
				anno := getFromAPI()
				_, has := anno[removedKey]
				g.Expect(has).To(BeFalse(), "annotation %q should be removed by MergeFrom patch (WR-03/WR-14 regression)", removedKey)
				g.Expect(anno[preservedKey]).To(Equal(preservedVal),
					"unrelated annotation %q should be preserved across the patch", preservedKey)
			}, 5*time.Second, 100*time.Millisecond).Should(Succeed())
		}

		Describe("WR-03: gates.ConsumeApprove on a Milestone", func() {
			const msName = "wr03-ms-approve"
			AfterEach(func() {
				milestoneAnnotationCleanup(msName)
			})

			It("MergeFrom patch drops the approve-milestone annotation from the apiserver state", func() {
				ms := &tideprojectv1alpha1.Milestone{
					ObjectMeta: metav1.ObjectMeta{
						Name:      msName,
						Namespace: "default",
						Annotations: map[string]string{
							gates.AnnotationApprovePrefix + "milestone": "true",
							"unrelated.example.com/keep":                "preserved-value",
						},
					},
					Spec: tideprojectv1alpha1.MilestoneSpec{ProjectRef: "any"},
				}
				Expect(k8sClient.Create(ctx, ms)).To(Succeed())

				// Re-read so we have the apiserver-assigned resourceVersion etc.
				var fresh tideprojectv1alpha1.Milestone
				Expect(k8sClient.Get(ctx, types.NamespacedName{Name: msName, Namespace: "default"}, &fresh)).To(Succeed())

				// Apply the same shape the reconciler uses.
				newAnno := gates.ConsumeApprove(&fresh, "milestone")
				patch := client.MergeFrom(fresh.DeepCopy())
				fresh.SetAnnotations(newAnno)
				Expect(k8sClient.Patch(ctx, &fresh, patch)).To(Succeed())

				assertAnnotationRemovedFromAPIServer(func() map[string]string {
					var got tideprojectv1alpha1.Milestone
					if err := k8sClient.Get(ctx, types.NamespacedName{Name: msName, Namespace: "default"}, &got); err != nil {
						return nil
					}
					return got.Annotations
				}, gates.AnnotationApprovePrefix+"milestone", "unrelated.example.com/keep", "preserved-value")
			})
		})

		Describe("WR-14: budget.ConsumeBypass on a Project", func() {
			const projectName = "wr14-proj-bypass"
			AfterEach(func() {
				projectAnnotationCleanup(projectName)
			})

			It("MergeFrom patch drops the bypass-budget annotation from the apiserver state", func() {
				proj := &tideprojectv1alpha1.Project{
					ObjectMeta: metav1.ObjectMeta{
						Name:      projectName,
						Namespace: "default",
						Annotations: map[string]string{
							"tideproject.k8s/bypass-budget": "true",
							"unrelated.example.com/keep":    "preserved-value",
						},
					},
					Spec: tideprojectv1alpha1.ProjectSpec{
						TargetRepo: "https://github.com/example/wr14.git",
					},
				}
				Expect(k8sClient.Create(ctx, proj)).To(Succeed())

				var fresh tideprojectv1alpha1.Project
				Expect(k8sClient.Get(ctx, types.NamespacedName{Name: projectName, Namespace: "default"}, &fresh)).To(Succeed())

				newAnno := budget.ConsumeBypass(&fresh)
				patch := client.MergeFrom(fresh.DeepCopy())
				fresh.SetAnnotations(newAnno)
				Expect(k8sClient.Patch(ctx, &fresh, patch)).To(Succeed())

				assertAnnotationRemovedFromAPIServer(func() map[string]string {
					var got tideprojectv1alpha1.Project
					if err := k8sClient.Get(ctx, types.NamespacedName{Name: projectName, Namespace: "default"}, &got); err != nil {
						return nil
					}
					return got.Annotations
				}, "tideproject.k8s/bypass-budget", "unrelated.example.com/keep", "preserved-value")
			})
		})
	})
