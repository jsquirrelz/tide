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

var _ = Describe("Project Controller", func() {
	Context("When reconciling a resource", func() {
		ctx := context.Background()

		It("accepts a valid Project CRD apply", func() {
			project := &tideprojectv1alpha1.Project{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "valid-project",
					Namespace: "default",
				},
				Spec: tideprojectv1alpha1.ProjectSpec{
					TargetRepo: "https://github.com/example/repo.git",
				},
			}
			Expect(k8sClient.Create(ctx, project)).To(Succeed())

			By("Cleanup")
			Expect(k8sClient.Delete(ctx, project)).To(Succeed())
		})

		It("rejects a Project with an invalid targetRepo (CEL XValidation)", func() {
			invalid := &tideprojectv1alpha1.Project{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "invalid-project",
					Namespace: "default",
				},
				Spec: tideprojectv1alpha1.ProjectSpec{
					TargetRepo: "not-a-valid-url",
				},
			}
			err := k8sClient.Create(ctx, invalid)
			Expect(err).To(HaveOccurred())
			Expect(apierrors.IsInvalid(err)).To(BeTrue(),
				"expected CEL XValidation rejection, got: %v", err)
		})

		It("sets the finalizer on create (CTRL-05)", func() {
			project := &tideprojectv1alpha1.Project{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "finalizer-set",
					Namespace: "default",
				},
				Spec: tideprojectv1alpha1.ProjectSpec{
					TargetRepo: "https://github.com/example/repo.git",
				},
			}
			Expect(k8sClient.Create(ctx, project)).To(Succeed())

			reconciler := &ProjectReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			name := types.NamespacedName{Name: project.Name, Namespace: project.Namespace}

			// First reconcile: adds the finalizer and returns.
			_, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: name})
			Expect(err).NotTo(HaveOccurred())

			fetched := &tideprojectv1alpha1.Project{}
			Expect(k8sClient.Get(ctx, name, fetched)).To(Succeed())
			Expect(fetched.Finalizers).To(ContainElement("tideproject.k8s/project-cleanup"))

			By("Cleanup")
			Expect(k8sClient.Delete(ctx, fetched)).To(Succeed())
			// Drive cleanup so the finalizer is removed and GC proceeds.
			_, err = reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: name})
			Expect(err).NotTo(HaveOccurred())
		})

		It("removes finalizer on deletion (TestFinalizerLifecycle, Pitfall 21)", func() {
			project := &tideprojectv1alpha1.Project{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "finalizer-lifecycle",
					Namespace: "default",
				},
				Spec: tideprojectv1alpha1.ProjectSpec{
					TargetRepo: "https://github.com/example/repo.git",
				},
			}
			Expect(k8sClient.Create(ctx, project)).To(Succeed())

			reconciler := &ProjectReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}
			name := types.NamespacedName{Name: project.Name, Namespace: project.Namespace}

			// Add finalizer.
			_, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: name})
			Expect(err).NotTo(HaveOccurred())

			fetched := &tideprojectv1alpha1.Project{}
			Expect(k8sClient.Get(ctx, name, fetched)).To(Succeed())
			Expect(fetched.Finalizers).To(ContainElement("tideproject.k8s/project-cleanup"))

			// Issue a delete — object enters Terminating state because of the finalizer.
			Expect(k8sClient.Delete(ctx, fetched)).To(Succeed())

			Expect(k8sClient.Get(ctx, name, fetched)).To(Succeed())
			Expect(fetched.DeletionTimestamp.IsZero()).To(BeFalse(), "expected DeletionTimestamp set")

			// Drive cleanup — HandleDeletion runs the no-op callback and removes the finalizer.
			_, err = reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: name})
			Expect(err).NotTo(HaveOccurred())

			// The object should be GC'd within a short window.
			Eventually(func() bool {
				err := k8sClient.Get(ctx, name, &tideprojectv1alpha1.Project{})
				return apierrors.IsNotFound(err)
			}, 10*time.Second, 250*time.Millisecond).Should(BeTrue(),
				"expected Project to be garbage-collected after finalizer removal")
		})
	})
})
