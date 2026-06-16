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
	"encoding/json"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	tideprojectv1alpha1 "github.com/jsquirrelz/tide/api/v1alpha2"
)

// Debug defect #13b — boundary-push observability + bounded controller-driven
// auto-retry. The boundary push lands the already-integrated run branch on the
// remote AFTER the Project reaches Complete. The retry is bounded
// (maxBoundaryPushAttempts) and surfaced via the non-terminal BoundaryPushed
// condition; Complete is NEVER gated on the push outcome.
var _ = Describe("ProjectReconciler — boundary-push bounded auto-retry (debug #13b)", Label("envtest", "debug13b"), func() {
	ctx := context.Background()

	// reconcileN runs the reconcile loop N times so multi-pass state machines
	// (delete-then-recreate) converge.
	reconcileN := func(r *ProjectReconciler, name string, n int) {
		for range n {
			_, _ = r.Reconcile(ctx, reconcile.Request{NamespacedName: types.NamespacedName{Name: name, Namespace: "default"}})
		}
	}

	getProject := func(name string) tideprojectv1alpha1.Project {
		var got tideprojectv1alpha1.Project
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: "default"}, &got)).To(Succeed())
		return got
	}

	pushJobName := func(uid types.UID) string { return fmt.Sprintf("tide-push-%s", uid) }

	// makeComplete creates a Project, sets it to Complete with a run branch, and
	// (when attempts > 0) seeds the BoundaryPush retry tally so the
	// re-derived-from-status cap path is exercised.
	makeComplete := func(name string, attempts int32) *tideprojectv1alpha1.Project {
		proj := &tideprojectv1alpha1.Project{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
			Spec: tideprojectv1alpha1.ProjectSpec{
				TargetRepo: "https://github.com/example/test.git",
				Git: &tideprojectv1alpha1.GitConfig{
					RepoURL:        "https://github.com/example/test.git",
					CredsSecretRef: "test-creds",
				},
			},
		}
		Expect(k8sClient.Create(ctx, proj)).To(Succeed())
		waitForCacheSync(name, "default", &tideprojectv1alpha1.Project{})

		got := getProject(name)
		patch := client.MergeFrom(got.DeepCopy())
		got.Status.Phase = tideprojectv1alpha1.PhaseComplete
		got.Status.Git.BranchName = "tide/run-" + name + "-1747200000"
		got.Status.BoundaryPush.Attempts = attempts
		Expect(k8sClient.Status().Patch(ctx, &got, patch)).To(Succeed())
		return &got
	}

	// makePushJob creates a placeholder push Job owned by the Project.
	makePushJob := func(name, projectName string, uid types.UID) {
		job := &batchv1.Job{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: "default",
				OwnerReferences: []metav1.OwnerReference{{
					APIVersion:         tideprojectv1alpha1.GroupVersion.String(),
					Kind:               "Project",
					Name:               projectName,
					UID:                uid,
					Controller:         new(true),
					BlockOwnerDeletion: new(true),
				}},
			},
			Spec: batchv1.JobSpec{
				Template: corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						RestartPolicy: corev1.RestartPolicyNever,
						Containers: []corev1.Container{{
							Name:  pushContainerName,
							Image: "ghcr.io/jsquirrelz/tide-push:test",
						}},
					},
				},
			},
		}
		Expect(k8sClient.Create(ctx, job)).To(Succeed())
	}

	markJobFailed := func(name string) {
		var job batchv1.Job
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: "default"}, &job)).To(Succeed())
		now := metav1.Now()
		job.Status.StartTime = &now
		job.Status.Failed = 1
		job.Status.Conditions = []batchv1.JobCondition{
			{Type: batchv1.JobFailureTarget, Status: corev1.ConditionTrue, LastTransitionTime: now, Reason: "BackoffLimitExceeded"},
			{Type: batchv1.JobFailed, Status: corev1.ConditionTrue, LastTransitionTime: now, Reason: "BackoffLimitExceeded"},
		}
		Expect(k8sClient.Status().Update(ctx, &job)).To(Succeed())
	}

	markJobSucceeded := func(name string) {
		var job batchv1.Job
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: "default"}, &job)).To(Succeed())
		now := metav1.Now()
		job.Status.StartTime = &now
		job.Status.Succeeded = 1
		job.Status.CompletionTime = &now
		job.Status.Conditions = []batchv1.JobCondition{
			{Type: batchv1.JobSuccessCriteriaMet, Status: corev1.ConditionTrue, LastTransitionTime: now, Reason: "CompletionsReached"},
			{Type: batchv1.JobComplete, Status: corev1.ConditionTrue, LastTransitionTime: now, Reason: "CompletionsReached"},
		}
		Expect(k8sClient.Status().Update(ctx, &job)).To(Succeed())
	}

	// fakePushPod attaches a terminationMessage envelope (empty reason = the
	// generic BackoffLimitExceeded #13b class) to the named push Job's pod.
	fakePushPod := func(jobName, reason string, exitCode int) {
		env := pushResultEnvelope{
			APIVersion: "tideproject.k8s/v1alpha1",
			Kind:       "PushResult",
			ExitCode:   exitCode,
			Reason:     reason,
		}
		raw, err := json.Marshal(env)
		Expect(err).NotTo(HaveOccurred())
		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      jobName + "-pod",
				Namespace: "default",
				Labels:    map[string]string{"job-name": jobName},
			},
			Spec: corev1.PodSpec{
				RestartPolicy: corev1.RestartPolicyNever,
				Containers:    []corev1.Container{{Name: pushContainerName, Image: "ghcr.io/jsquirrelz/tide-push:test"}},
			},
		}
		Expect(k8sClient.Create(ctx, pod)).To(Succeed())
		sp := client.MergeFrom(pod.DeepCopy())
		pod.Status.Phase = corev1.PodFailed
		pod.Status.ContainerStatuses = []corev1.ContainerStatus{{
			Name: pushContainerName,
			State: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{
				ExitCode: int32(exitCode), Reason: "Error", Message: string(raw),
			}},
		}}
		Expect(k8sClient.Status().Patch(ctx, pod, sp)).To(Succeed())
	}

	cleanup := func(name string) {
		var jobs batchv1.JobList
		_ = k8sClient.List(ctx, &jobs, client.InNamespace("default"))
		for i := range jobs.Items {
			j := jobs.Items[i]
			_ = k8sClient.Delete(ctx, &j)
		}
		var pods corev1.PodList
		_ = k8sClient.List(ctx, &pods, client.InNamespace("default"))
		for i := range pods.Items {
			p := pods.Items[i]
			_ = k8sClient.Delete(ctx, &p)
		}
		var p tideprojectv1alpha1.Project
		if err := k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: "default"}, &p); err == nil {
			p.Finalizers = nil
			_ = k8sClient.Update(ctx, &p)
			_ = k8sClient.Delete(ctx, &p)
		}
	}

	newReconciler := func(pvc string) *ProjectReconciler {
		r := newTestProjectReconciler()
		r.TidePushImage = "ghcr.io/jsquirrelz/tide-push:test"
		r.SharedPVCName = pvc
		ensurePVC(ctx, pvc, "default")
		return r
	}

	Describe("Test 1: failed push Job + under cap → delete + new Job, attempts++, BoundaryPushed=False/Pushing", func() {
		const name = "bp13b-retry"
		AfterEach(func() { cleanup(name) })

		It("recreates the push Job and increments attempts", func() {
			proj := makeComplete(name, 0)
			jn := pushJobName(proj.UID)
			makePushJob(jn, proj.Name, proj.UID)
			fakePushPod(jn, "", 1) // generic terminal failure (BackoffLimitExceeded)
			markJobFailed(jn)

			r := newReconciler("tide-projects-bp13b-1")
			// Pass 1: classify failed Job → foreground delete → dispatch fresh.
			// A few passes let the foreground delete + recreate converge.
			reconcileN(r, name, 4)

			Eventually(func(g Gomega) {
				got := getProject(name)
				g.Expect(got.Status.Phase).To(Equal(tideprojectv1alpha1.PhaseComplete))
				g.Expect(got.Status.BoundaryPush.Attempts).To(BeNumerically(">=", 1))
				g.Expect(got.Status.BoundaryPush.LastError).NotTo(BeEmpty())
				c := meta.FindStatusCondition(got.Status.Conditions, tideprojectv1alpha1.ConditionBoundaryPushed)
				g.Expect(c).NotTo(BeNil())
				g.Expect(c.Status).To(Equal(metav1.ConditionFalse))
				g.Expect(c.Reason).To(Equal(tideprojectv1alpha1.ReasonPushing))
				// A push Job exists again (recreated, not left deleted).
				var job batchv1.Job
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: jn, Namespace: "default"}, &job)).To(Succeed())
			}, 5*time.Second, 100*time.Millisecond).Should(Succeed())
		})
	})

	Describe("Test 2: push Job Complete → BoundaryPushed=True/Pushed, retry state cleared, NO new Job", func() {
		const name = "bp13b-success"
		AfterEach(func() { cleanup(name) })

		It("marks Pushed and does not create another Job", func() {
			proj := makeComplete(name, 1)
			jn := pushJobName(proj.UID)
			makePushJob(jn, proj.Name, proj.UID)
			markJobSucceeded(jn)

			r := newReconciler("tide-projects-bp13b-2")
			reconcileN(r, name, 3)

			Eventually(func(g Gomega) {
				got := getProject(name)
				c := meta.FindStatusCondition(got.Status.Conditions, tideprojectv1alpha1.ConditionBoundaryPushed)
				g.Expect(c).NotTo(BeNil())
				g.Expect(c.Status).To(Equal(metav1.ConditionTrue))
				g.Expect(c.Reason).To(Equal(tideprojectv1alpha1.ReasonPushed))
				g.Expect(got.Status.BoundaryPush.LastError).To(BeEmpty(), "retry state cleared on success")
			}, 5*time.Second, 100*time.Millisecond).Should(Succeed())

			// No SECOND push Job was created — the deterministic name is unique,
			// so a success must not be followed by a fresh dispatch.
			var jobs batchv1.JobList
			Expect(k8sClient.List(ctx, &jobs, client.InNamespace("default"))).To(Succeed())
			count := 0
			for i := range jobs.Items {
				if jobs.Items[i].Name == jn {
					count++
				}
			}
			Expect(count).To(Equal(1), "no new push Job after success")
		})
	})

	Describe("Test 3: attempts at cap → no new Job, BoundaryPushed=False/PushFailed, Event emitted", func() {
		const name = "bp13b-exhausted"
		AfterEach(func() { cleanup(name) })

		It("stops dispatching and surfaces PushFailed", func() {
			proj := makeComplete(name, maxBoundaryPushAttempts)
			jn := pushJobName(proj.UID)
			// No push Job present — at cap the controller must NOT create one.

			r := newReconciler("tide-projects-bp13b-3")
			reconcileN(r, name, 3)

			Eventually(func(g Gomega) {
				got := getProject(name)
				g.Expect(got.Status.Phase).To(Equal(tideprojectv1alpha1.PhaseComplete),
					"exhausted retry must NOT regress Complete")
				c := meta.FindStatusCondition(got.Status.Conditions, tideprojectv1alpha1.ConditionBoundaryPushed)
				g.Expect(c).NotTo(BeNil())
				g.Expect(c.Status).To(Equal(metav1.ConditionFalse))
				g.Expect(c.Reason).To(Equal(tideprojectv1alpha1.ReasonPushFailed))
			}, 5*time.Second, 100*time.Millisecond).Should(Succeed())

			// No push Job was created at cap.
			var job batchv1.Job
			err := k8sClient.Get(ctx, types.NamespacedName{Name: jn, Namespace: "default"}, &job)
			Expect(err).To(HaveOccurred(), "no push Job should be created once attempts >= cap")
		})
	})

	Describe("Test 4: Complete is never blocked by push state (succession independent of push)", func() {
		const name = "bp13b-complete"
		AfterEach(func() { cleanup(name) })

		It("a Project at cap with a failed push is still Complete", func() {
			proj := makeComplete(name, maxBoundaryPushAttempts)
			jn := pushJobName(proj.UID)
			makePushJob(jn, proj.Name, proj.UID)
			fakePushPod(jn, "", 1)
			markJobFailed(jn)

			r := newReconciler("tide-projects-bp13b-4")
			reconcileN(r, name, 3)

			got := getProject(name)
			Expect(got.Status.Phase).To(Equal(tideprojectv1alpha1.PhaseComplete),
				"Complete must hold regardless of boundary-push outcome")
		})
	})

	Describe("Test 5: no concurrent push Jobs — a Running push Job does NOT trigger a second", func() {
		const name = "bp13b-inflight"
		AfterEach(func() { cleanup(name) })

		It("requeues without creating a second Job while one is in flight", func() {
			proj := makeComplete(name, 1)
			jn := pushJobName(proj.UID)
			makePushJob(jn, proj.Name, proj.UID) // created, NOT marked terminal → Running

			r := newReconciler("tide-projects-bp13b-5")
			reconcileN(r, name, 3)

			// Exactly one push Job with this name still exists.
			var jobs batchv1.JobList
			Expect(k8sClient.List(ctx, &jobs, client.InNamespace("default"))).To(Succeed())
			count := 0
			for i := range jobs.Items {
				if jobs.Items[i].Name == jn {
					count++
				}
			}
			Expect(count).To(Equal(1), "a running push Job must not trigger a second")

			got := getProject(name)
			Expect(got.Status.BoundaryPush.Attempts).To(Equal(int32(1)),
				"in-flight requeue must not increment attempts")
			c := meta.FindStatusCondition(got.Status.Conditions, tideprojectv1alpha1.ConditionBoundaryPushed)
			Expect(c).NotTo(BeNil())
			Expect(c.Status).To(Equal(metav1.ConditionFalse))
			Expect(c.Reason).To(Equal(tideprojectv1alpha1.ReasonPushing))
		})
	})
})
