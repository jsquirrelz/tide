/*
Copyright 2026 TIDE Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

package controller

import (
	"context"
	"fmt"
	"regexp"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	tideprojectv1alpha1 "github.com/jsquirrelz/tide/api/v1alpha1"
)

// ensurePVC creates a bound PVC if it doesn't already exist.
func ensurePVC(ctx context.Context, name, ns string) {
	var pvc corev1.PersistentVolumeClaim
	err := k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: ns}, &pvc)
	if err == nil {
		return // already exists
	}
	if !apierrors.IsNotFound(err) {
		Expect(err).NotTo(HaveOccurred())
	}
	makeTestBoundPVC(ctx, name, ns)
}

var _ = Describe("ProjectReconciler — Phase 3 lifecycle (clone + push + branch + bypass)", Label("envtest", "phase3"), func() {
	const projectName = "test-proj-phase3"
	const pvcName = "tide-projects-phase3"
	ctx := context.Background()

	BeforeEach(func() {
		// PVC may already exist from a previous test in this suite; check first.
		var existing batchv1.JobList
		_ = existing
		// Look for the PVC directly via the dynamic client; create only if absent.
		ensurePVC(ctx, pvcName, "default")
	})

	AfterEach(func() {
		p := &tideprojectv1alpha1.Project{}
		if err := k8sClient.Get(ctx, types.NamespacedName{Name: projectName, Namespace: "default"}, p); err == nil {
			p.Finalizers = nil
			_ = k8sClient.Update(ctx, p)
			_ = k8sClient.Delete(ctx, p)
		}
		// Cleanup any Jobs.
		var jobs batchv1.JobList
		_ = k8sClient.List(ctx, &jobs, client.InNamespace("default"))
		for i := range jobs.Items {
			j := jobs.Items[i]
			_ = k8sClient.Delete(ctx, &j)
		}
		// PVC cleanup: best-effort; tests reuse the PVC across BeforeEach.
	})

	It("Test 1: branch-name init sets Status.Git.BranchName to tide/run-<name>-<unix>", func() {
		proj := &tideprojectv1alpha1.Project{
			ObjectMeta: metav1.ObjectMeta{Name: projectName, Namespace: "default"},
			Spec: tideprojectv1alpha1.ProjectSpec{
				TargetRepo: "https://github.com/example/test.git",
				Git: tideprojectv1alpha1.GitConfig{
					RepoURL:        "https://github.com/example/test.git",
					CredsSecretRef: "test-creds",
				},
			},
		}
		Expect(k8sClient.Create(ctx, proj)).To(Succeed())
		waitForCacheSync(projectName, "default", &tideprojectv1alpha1.Project{})

		r := &ProjectReconciler{
			Client:                  k8sClient,
			Scheme:                  k8sClient.Scheme(),
			Dispatcher:              &stubDispatcher{},
			MaxConcurrentReconciles: 1,
			SharedPVCName:           pvcName,
			TidePushImage:           "ghcr.io/jsquirrelz/tide-push:test",
		}

		// Drive reconciles: finalizer, init job, init completion, then phase 3.
		// Simulate init job success so the reconciler advances.
		for i := 0; i < 8; i++ {
			_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: types.NamespacedName{Name: projectName, Namespace: "default"}})
			if err != nil {
				if !isConflict(err) {
					Expect(err).NotTo(HaveOccurred())
				}
			}
			// On a particular pass, the init Job should exist — patch it to Succeeded.
			var p tideprojectv1alpha1.Project
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: projectName, Namespace: "default"}, &p); err == nil {
				initJobName := fmt.Sprintf("tide-init-%s", p.UID)
				var job batchv1.Job
				if err := k8sClient.Get(ctx, types.NamespacedName{Name: initJobName, Namespace: "default"}, &job); err == nil {
					if !isJobSucceeded(&job) {
						_ = makeFakeJobTerminal(ctx, k8sClient, initJobName, "default", true)
					}
				}
			}
		}

		// Verify Status.Git.BranchName is set.
		Eventually(func(g Gomega) {
			var got tideprojectv1alpha1.Project
			g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: projectName, Namespace: "default"}, &got)).To(Succeed())
			g.Expect(got.Status.Git.BranchName).NotTo(BeEmpty(), "Status.Git.BranchName should be set after Phase 3 lifecycle")
			matched, _ := regexp.MatchString(`^tide/run-test-proj-phase3-\d+$`, got.Status.Git.BranchName)
			g.Expect(matched).To(BeTrue(), "BranchName %q should match tide/run-<name>-<unix>", got.Status.Git.BranchName)
			// Confirm no ":" (RFC3339 would inject one).
			for _, c := range got.Status.Git.BranchName {
				g.Expect(c).NotTo(Equal(':'), "BranchName must not contain ':' (refname constraint)")
			}
		}, 10*time.Second, 100*time.Millisecond).Should(Succeed())
	})

	It("Test 2: bypass annotation clears PushLeaseFailed and triggers retry", func() {
		proj := &tideprojectv1alpha1.Project{
			ObjectMeta: metav1.ObjectMeta{Name: projectName, Namespace: "default"},
			Spec: tideprojectv1alpha1.ProjectSpec{
				TargetRepo: "https://github.com/example/test.git",
				Git: tideprojectv1alpha1.GitConfig{
					RepoURL:        "https://github.com/example/test.git",
					CredsSecretRef: "test-creds",
				},
			},
		}
		Expect(k8sClient.Create(ctx, proj)).To(Succeed())
		waitForCacheSync(projectName, "default", &tideprojectv1alpha1.Project{})

		// Patch Project status to PushLeaseFailed manually.
		var p tideprojectv1alpha1.Project
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: projectName, Namespace: "default"}, &p)).To(Succeed())
		statusPatch := client.MergeFrom(p.DeepCopy())
		p.Status.Phase = tideprojectv1alpha1.PhasePushLeaseFailed
		p.Status.Git.BranchName = "tide/run-test-proj-phase3-1747200000"
		p.Status.Git.LeaseFailureCount = 1
		Expect(k8sClient.Status().Patch(ctx, &p, statusPatch)).To(Succeed())

		// Add the bypass annotation.
		annotPatch := client.MergeFrom(p.DeepCopy())
		if p.Annotations == nil {
			p.Annotations = map[string]string{}
		}
		p.Annotations[bypassPushLeaseAnnotation] = "true"
		Expect(k8sClient.Patch(ctx, &p, annotPatch)).To(Succeed())

		r := &ProjectReconciler{
			Client:                  k8sClient,
			Scheme:                  k8sClient.Scheme(),
			Dispatcher:              &stubDispatcher{},
			MaxConcurrentReconciles: 1,
			SharedPVCName:           pvcName,
			TidePushImage:           "ghcr.io/jsquirrelz/tide-push:test",
		}

		// Reconcile to process the annotation.
		for i := 0; i < 5; i++ {
			_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: types.NamespacedName{Name: projectName, Namespace: "default"}})
			if err != nil && !isConflict(err) {
				Expect(err).NotTo(HaveOccurred())
			}
		}

		// Verify PhasePushLeaseFailed is cleared.
		Eventually(func(g Gomega) {
			var got tideprojectv1alpha1.Project
			g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: projectName, Namespace: "default"}, &got)).To(Succeed())
			g.Expect(got.Status.Phase).NotTo(Equal(tideprojectv1alpha1.PhasePushLeaseFailed), "PhasePushLeaseFailed should be cleared after bypass annotation")
		}, 10*time.Second, 100*time.Millisecond).Should(Succeed())
	})
})
