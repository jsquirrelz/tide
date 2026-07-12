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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	tideprojectv1alpha3 "github.com/jsquirrelz/tide/api/v1alpha3"
)

var _ = Describe("BYPASS-02 clone idempotency", Label("envtest"), func() {
	const pvcName = "tide-projects-clone-idempotency"
	ctx := context.Background()

	BeforeEach(func() {
		ensurePVC(ctx, pvcName, "default")
	})

	AfterEach(func() {
		var jobs batchv1.JobList
		_ = k8sClient.List(ctx, &jobs, client.InNamespace("default"))
		for i := range jobs.Items {
			j := &jobs.Items[i]
			_ = k8sClient.Delete(ctx, j, client.PropagationPolicy(metav1.DeletePropagationBackground))
		}
	})

	Describe("Spec 1: no re-clone when CloneComplete=true", func() {
		const projectName = "test-clone-idempotency-no-reclone"

		AfterEach(func() {
			p := &tideprojectv1alpha3.Project{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: projectName, Namespace: "default"}, p); err == nil {
				p.Finalizers = nil
				_ = k8sClient.Update(ctx, p)
				_ = k8sClient.Delete(ctx, p)
			}
		})

		It("does not dispatch a clone Job when Status.Git.CloneComplete=true", func() {
			proj := &tideprojectv1alpha3.Project{
				ObjectMeta: metav1.ObjectMeta{Name: projectName, Namespace: "default"},
				Spec: tideprojectv1alpha3.ProjectSpec{
					SchemaRevision: "v1alpha3",
					TargetRepo:     "https://github.com/example/test.git",
					Git: &tideprojectv1alpha3.GitConfig{
						RepoURL:        "https://github.com/example/test.git",
						CredsSecretRef: "test-creds",
					},
				},
			}
			Expect(k8sClient.Create(ctx, proj)).To(Succeed())

			// Wait for create to propagate to cache.
			waitForCacheSync(projectName, "default", &tideprojectv1alpha3.Project{})

			// Patch status: CloneComplete=true + non-empty BranchName simulate an
			// already-cloned workspace. The controller must NOT re-dispatch the clone Job.
			var p tideprojectv1alpha3.Project
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: projectName, Namespace: "default"}, &p)).To(Succeed())
			statusPatch := client.MergeFrom(p.DeepCopy())
			p.Status.Git.CloneComplete = true
			p.Status.Git.BranchName = "tide/run-test-clone-idempotency-1000000000"
			p.Status.Phase = tideprojectv1alpha3.PhaseRunning
			Expect(k8sClient.Status().Patch(ctx, &p, statusPatch)).To(Succeed())

			r := &ProjectReconciler{
				Client:                  k8sClient,
				Scheme:                  k8sClient.Scheme(),
				Dispatcher:              &stubDispatcher{},
				MaxConcurrentReconciles: 1,
				SharedPVCName:           pvcName,
				TidePushImage:           "ghcr.io/jsquirrelz/tide-push:test",
			}

			// Drive a reconcile; the CloneComplete guard must prevent clone-Job creation.
			for range 3 {
				_, err := r.Reconcile(ctx, reconcile.Request{
					NamespacedName: types.NamespacedName{Name: projectName, Namespace: "default"},
				})
				if err != nil && !isConflict(err) {
					Expect(err).NotTo(HaveOccurred())
				}
			}

			// Assert the clone Job was never created.
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: projectName, Namespace: "default"}, &p)).To(Succeed())
			cloneJobName := fmt.Sprintf("tide-clone-%s", p.UID)
			Consistently(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{Name: cloneJobName, Namespace: "default"}, &batchv1.Job{})
			}, 1*time.Second, 100*time.Millisecond).Should(MatchError(ContainSubstring("not found")),
				"clone Job must NOT be re-dispatched when CloneComplete=true")
		})
	})

	Describe("Spec 2: CloneComplete flips to true on clone-Job terminal-succeeded", func() {
		const projectName = "test-clone-idempotency-set-on-success"

		AfterEach(func() {
			p := &tideprojectv1alpha3.Project{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: projectName, Namespace: "default"}, p); err == nil {
				p.Finalizers = nil
				_ = k8sClient.Update(ctx, p)
				_ = k8sClient.Delete(ctx, p)
			}
		})

		It("sets Status.Git.CloneComplete=true when the clone Job reports Succeeded>0", func() {
			proj := &tideprojectv1alpha3.Project{
				ObjectMeta: metav1.ObjectMeta{Name: projectName, Namespace: "default"},
				Spec: tideprojectv1alpha3.ProjectSpec{
					SchemaRevision: "v1alpha3",
					TargetRepo:     "https://github.com/example/test.git",
					Git: &tideprojectv1alpha3.GitConfig{
						RepoURL:        "https://github.com/example/test.git",
						CredsSecretRef: "test-creds",
					},
				},
			}
			Expect(k8sClient.Create(ctx, proj)).To(Succeed())
			waitForCacheSync(projectName, "default", &tideprojectv1alpha3.Project{})

			r := &ProjectReconciler{
				Client:                  k8sClient,
				Scheme:                  k8sClient.Scheme(),
				Dispatcher:              &stubDispatcher{},
				MaxConcurrentReconciles: 1,
				SharedPVCName:           pvcName,
				TidePushImage:           "ghcr.io/jsquirrelz/tide-push:test",
			}

			// Advance the project to the clone-dispatch point: simulate init Job success
			// so the reconciler reaches reconcilePhase3Lifecycle and dispatches the clone Job.
			var p tideprojectv1alpha3.Project
			for range 10 {
				_, err := r.Reconcile(ctx, reconcile.Request{
					NamespacedName: types.NamespacedName{Name: projectName, Namespace: "default"},
				})
				if err != nil && !isConflict(err) {
					Expect(err).NotTo(HaveOccurred())
				}
				// Patch the init Job to succeeded so the reconciler advances.
				if err := k8sClient.Get(ctx, types.NamespacedName{Name: projectName, Namespace: "default"}, &p); err == nil {
					initJobName := fmt.Sprintf("tide-init-%s", p.UID)
					var j batchv1.Job
					if err := k8sClient.Get(ctx, types.NamespacedName{Name: initJobName, Namespace: "default"}, &j); err == nil {
						if !isJobSucceeded(&j) {
							_ = makeFakeJobTerminal(ctx, k8sClient, initJobName, "default", true)
						}
					}
				}
			}

			// Locate the clone Job; it should exist by now.
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: projectName, Namespace: "default"}, &p)).To(Succeed())
			cloneJobName := fmt.Sprintf("tide-clone-%s", p.UID)

			// Patch the clone Job to terminal-succeeded.
			Eventually(func() error {
				return makeFakeJobTerminal(ctx, k8sClient, cloneJobName, "default", true)
			}, 5*time.Second, 200*time.Millisecond).Should(Succeed(),
				"clone Job should exist and be patchable to succeeded")

			// Phase 35 read-before-flip (D-11): the set-on-success arm now reads
			// the CloneResult envelope FIRST and stamps baseSHA in the same patch
			// that flips CloneComplete. Provide a success envelope pod so the
			// happy path flips immediately (without one it would requeue until the
			// sub-TTL cutoff — covered by the baseref-halt suite's Spec E).
			cloneEnv := pushResultEnvelope{Kind: "CloneResult", ProjectUID: string(p.UID), ExitCode: 0}
			envRaw, mErr := json.Marshal(cloneEnv)
			Expect(mErr).NotTo(HaveOccurred())
			envPod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      cloneJobName + "-pod",
					Namespace: "default",
					Labels:    map[string]string{"job-name": cloneJobName},
				},
				Spec: corev1.PodSpec{
					RestartPolicy: corev1.RestartPolicyNever,
					Containers:    []corev1.Container{{Name: pushContainerName, Image: "ghcr.io/jsquirrelz/tide-push:test"}},
				},
			}
			Expect(k8sClient.Create(ctx, envPod)).To(Succeed())
			podPatch := client.MergeFrom(envPod.DeepCopy())
			envPod.Status.Phase = corev1.PodSucceeded
			envPod.Status.ContainerStatuses = []corev1.ContainerStatus{{
				Name: pushContainerName,
				State: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{
					ExitCode: 0, Reason: "Completed", Message: string(envRaw),
				}},
			}}
			Expect(k8sClient.Status().Patch(ctx, envPod, podPatch)).To(Succeed())

			// Drive a few more reconciles so the controller detects success and patches CloneComplete.
			for range 5 {
				_, err := r.Reconcile(ctx, reconcile.Request{
					NamespacedName: types.NamespacedName{Name: projectName, Namespace: "default"},
				})
				if err != nil && !isConflict(err) {
					Expect(err).NotTo(HaveOccurred())
				}
			}

			// Assert CloneComplete is now true.
			Eventually(func(g Gomega) {
				var got tideprojectv1alpha3.Project
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: projectName, Namespace: "default"}, &got)).To(Succeed())
				g.Expect(got.Status.Git.CloneComplete).To(BeTrue(),
					"Status.Git.CloneComplete must be true after clone Job reports Succeeded>0")
			}, 5*time.Second, 100*time.Millisecond).Should(Succeed())
		})
	})

	// WR-03 (Phase 27): a terminally-failed clone Job (Failed>0, Succeeded==0)
	// must be deleted so the next reconcile re-dispatches a fresh clone, instead
	// of stalling for the clone Job's 300s TTL (CloneComplete never set,
	// IsNotFound never true). A CloneFailed condition surfaces the recovery.
	Describe("Spec 3: terminal-failed clone Job is deleted to re-dispatch (WR-03)", func() {
		const projectName = "test-clone-idempotency-failed-redispatch"

		AfterEach(func() {
			p := &tideprojectv1alpha3.Project{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: projectName, Namespace: "default"}, p); err == nil {
				p.Finalizers = nil
				_ = k8sClient.Update(ctx, p)
				_ = k8sClient.Delete(ctx, p)
			}
		})

		It("deletes a Failed clone Job and re-dispatches, setting CloneFailed", func() {
			proj := &tideprojectv1alpha3.Project{
				ObjectMeta: metav1.ObjectMeta{Name: projectName, Namespace: "default"},
				Spec: tideprojectv1alpha3.ProjectSpec{
					SchemaRevision: "v1alpha3",
					TargetRepo:     "https://github.com/example/test.git",
					Git: &tideprojectv1alpha3.GitConfig{
						RepoURL:        "https://github.com/example/test.git",
						CredsSecretRef: "test-creds",
					},
				},
			}
			Expect(k8sClient.Create(ctx, proj)).To(Succeed())
			waitForCacheSync(projectName, "default", &tideprojectv1alpha3.Project{})

			r := &ProjectReconciler{
				Client:                  k8sClient,
				Scheme:                  k8sClient.Scheme(),
				Dispatcher:              &stubDispatcher{},
				MaxConcurrentReconciles: 1,
				SharedPVCName:           pvcName,
				TidePushImage:           "ghcr.io/jsquirrelz/tide-push:test",
			}

			// Advance to the clone-dispatch point: drive reconciles + succeed the
			// init Job so reconcilePhase3Lifecycle dispatches the clone Job.
			var p tideprojectv1alpha3.Project
			for range 10 {
				_, err := r.Reconcile(ctx, reconcile.Request{
					NamespacedName: types.NamespacedName{Name: projectName, Namespace: "default"},
				})
				if err != nil && !isConflict(err) {
					Expect(err).NotTo(HaveOccurred())
				}
				if err := k8sClient.Get(ctx, types.NamespacedName{Name: projectName, Namespace: "default"}, &p); err == nil {
					initJobName := fmt.Sprintf("tide-init-%s", p.UID)
					var j batchv1.Job
					if err := k8sClient.Get(ctx, types.NamespacedName{Name: initJobName, Namespace: "default"}, &j); err == nil {
						if !isJobSucceeded(&j) {
							_ = makeFakeJobTerminal(ctx, k8sClient, initJobName, "default", true)
						}
					}
				}
			}

			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: projectName, Namespace: "default"}, &p)).To(Succeed())
			cloneJobName := fmt.Sprintf("tide-clone-%s", p.UID)

			// Patch the clone Job to terminal-FAILED (Failed>0, Succeeded==0). The
			// apiserver Job validation requires a FailureTarget=True condition before
			// Failed=True and rejects a completionTime on a failed Job, so set the
			// conditions explicitly rather than via makeFakeJobTerminal.
			Eventually(func() error {
				var j batchv1.Job
				if gErr := k8sClient.Get(ctx, types.NamespacedName{Name: cloneJobName, Namespace: "default"}, &j); gErr != nil {
					return gErr
				}
				now := metav1.Now()
				j.Status.StartTime = &now
				j.Status.Failed = 1
				j.Status.Conditions = []batchv1.JobCondition{
					{Type: batchv1.JobFailureTarget, Status: corev1.ConditionTrue, LastTransitionTime: now, Reason: "BackoffLimitExceeded"},
					{Type: batchv1.JobFailed, Status: corev1.ConditionTrue, LastTransitionTime: now, Reason: "BackoffLimitExceeded"},
				}
				return k8sClient.Status().Update(ctx, &j)
			}, 5*time.Second, 200*time.Millisecond).Should(Succeed(),
				"clone Job should exist and be patchable to failed")

			// Capture the failed Job's UID so we can prove it was actually deleted
			// (a re-dispatched Job has a new UID).
			var failedJob batchv1.Job
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: cloneJobName, Namespace: "default"}, &failedJob)).To(Succeed())
			failedUID := failedJob.UID

			// Drive reconciles: the WR-03 arm deletes the failed Job; the next
			// reconcile re-dispatches a fresh clone Job (new UID).
			for range 5 {
				_, err := r.Reconcile(ctx, reconcile.Request{
					NamespacedName: types.NamespacedName{Name: projectName, Namespace: "default"},
				})
				if err != nil && !isConflict(err) {
					Expect(err).NotTo(HaveOccurred())
				}
			}

			// The failed Job must be gone (deleted), and CloneFailed condition set.
			Eventually(func(g Gomega) {
				var got tideprojectv1alpha3.Project
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: projectName, Namespace: "default"}, &got)).To(Succeed())
				cond := false
				for i := range got.Status.Conditions {
					if got.Status.Conditions[i].Type == tideprojectv1alpha3.ConditionCloneFailed &&
						got.Status.Conditions[i].Status == metav1.ConditionTrue {
						cond = true
					}
				}
				g.Expect(cond).To(BeTrue(),
					"ConditionCloneFailed=True must be set when the clone Job terminal-fails")

				// Either the failed Job was deleted (NotFound) or a fresh one with a
				// new UID was re-dispatched — both prove the stall was broken.
				var cur batchv1.Job
				err := k8sClient.Get(ctx, types.NamespacedName{Name: cloneJobName, Namespace: "default"}, &cur)
				if err == nil {
					g.Expect(cur.UID).NotTo(Equal(failedUID),
						"a re-dispatched clone Job must have a new UID (the failed one was deleted)")
				}
			}, 5*time.Second, 100*time.Millisecond).Should(Succeed())
		})
	})
})
