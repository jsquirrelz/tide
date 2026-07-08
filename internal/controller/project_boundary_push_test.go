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
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/prometheus/client_golang/prometheus/testutil"

	tideprojectv1alpha2 "github.com/jsquirrelz/tide/api/v1alpha2"
	tidemetrics "github.com/jsquirrelz/tide/internal/metrics"
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

	getProject := func(name string) tideprojectv1alpha2.Project {
		var got tideprojectv1alpha2.Project
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: "default"}, &got)).To(Succeed())
		return got
	}

	pushJobName := func(uid types.UID) string { return fmt.Sprintf("tide-push-%s", uid) }

	// makeComplete creates a Project, sets it to Complete with a run branch, and
	// (when attempts > 0) seeds the BoundaryPush retry tally so the
	// re-derived-from-status cap path is exercised.
	makeComplete := func(name string, attempts int32) *tideprojectv1alpha2.Project {
		proj := &tideprojectv1alpha2.Project{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
			Spec: tideprojectv1alpha2.ProjectSpec{SchemaRevision: "v1alpha2",
				TargetRepo: "https://github.com/example/test.git",
				Git: &tideprojectv1alpha2.GitConfig{
					RepoURL:        "https://github.com/example/test.git",
					CredsSecretRef: "test-creds",
				},
			},
		}
		Expect(k8sClient.Create(ctx, proj)).To(Succeed())
		waitForCacheSync(name, "default", &tideprojectv1alpha2.Project{})

		got := getProject(name)
		patch := client.MergeFrom(got.DeepCopy())
		got.Status.Phase = tideprojectv1alpha2.PhaseComplete
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
					APIVersion:         tideprojectv1alpha2.GroupVersion.String(),
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

	// fakePushPodSuccess attaches a terminationMessage envelope for a SUCCESSFUL
	// push (exit 0, empty reason) carrying the landed run-branch headSHA — the
	// value the success arm reads to advance Status.Git.LastPushedSHA.
	fakePushPodSuccess := func(jobName, headSHA string) {
		env := pushResultEnvelope{
			APIVersion: "tideproject.k8s/v1alpha1",
			Kind:       "PushResult",
			HeadSHA:    headSHA,
			ExitCode:   0,
			Reason:     "",
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
		pod.Status.Phase = corev1.PodSucceeded
		pod.Status.ContainerStatuses = []corev1.ContainerStatus{{
			Name: pushContainerName,
			State: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{
				ExitCode: 0, Reason: "Completed", Message: string(raw),
			}},
		}}
		Expect(k8sClient.Status().Patch(ctx, pod, sp)).To(Succeed())
	}

	// fakePushPodWith attaches a terminationMessage envelope to a NAMED pod of the
	// given push Job, so a single Job can own MULTIPLE attempt pods (the push Job
	// carries BackoffLimit>0). podName lets a test control List ordering; phase +
	// exitCode + headSHA model a specific attempt's outcome.
	fakePushPodWith := func(podName, jobName string, phase corev1.PodPhase, headSHA, reason string, exitCode int) {
		env := pushResultEnvelope{
			APIVersion: "tideproject.k8s/v1alpha1",
			Kind:       "PushResult",
			HeadSHA:    headSHA,
			ExitCode:   exitCode,
			Reason:     reason,
		}
		raw, err := json.Marshal(env)
		Expect(err).NotTo(HaveOccurred())
		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      podName,
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
		pod.Status.Phase = phase
		termReason := "Completed"
		if exitCode != 0 {
			termReason = "Error"
		}
		pod.Status.ContainerStatuses = []corev1.ContainerStatus{{
			Name: pushContainerName,
			State: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{
				ExitCode: int32(exitCode), Reason: termReason, Message: string(raw),
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
		var p tideprojectv1alpha2.Project
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
				g.Expect(got.Status.Phase).To(Equal(tideprojectv1alpha2.PhaseComplete))
				g.Expect(got.Status.BoundaryPush.Attempts).To(BeNumerically(">=", 1))
				g.Expect(got.Status.BoundaryPush.LastError).NotTo(BeEmpty())
				c := meta.FindStatusCondition(got.Status.Conditions, tideprojectv1alpha2.ConditionBoundaryPushed)
				g.Expect(c).NotTo(BeNil())
				g.Expect(c.Status).To(Equal(metav1.ConditionFalse))
				g.Expect(c.Reason).To(Equal(tideprojectv1alpha2.ReasonPushing))
				// A push Job exists again (recreated, not left deleted).
				var job batchv1.Job
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: jn, Namespace: "default"}, &job)).To(Succeed())
			}, 5*time.Second, 100*time.Millisecond).Should(Succeed())
		})
	})

	Describe("Test 2: push Job Complete → BoundaryPushed=True/Pushed, retry state cleared, NO new Job", func() {
		const name = "bp13b-success"
		AfterEach(func() { cleanup(name) })

		It("marks Pushed, advances LastPushedSHA from the envelope, and does not create another Job", func() {
			const landedSHA = "0123456789abcdef0123456789abcdef01234567"
			proj := makeComplete(name, 1)
			jn := pushJobName(proj.UID)
			makePushJob(jn, proj.Name, proj.UID)
			// Success pod carries the landed headSHA so the success arm can
			// advance the --force-with-lease anchor (Status.Git.LastPushedSHA).
			fakePushPodSuccess(jn, landedSHA)
			markJobSucceeded(jn)

			r := newReconciler("tide-projects-bp13b-2")
			reconcileN(r, name, 3)

			Eventually(func(g Gomega) {
				got := getProject(name)
				c := meta.FindStatusCondition(got.Status.Conditions, tideprojectv1alpha2.ConditionBoundaryPushed)
				g.Expect(c).NotTo(BeNil())
				g.Expect(c.Status).To(Equal(metav1.ConditionTrue))
				g.Expect(c.Reason).To(Equal(tideprojectv1alpha2.ReasonPushed))
				g.Expect(got.Status.BoundaryPush.LastError).To(BeEmpty(), "retry state cleared on success")
				// Defect B fix: the lease anchor advances to the pushed SHA so the
				// next push carries a real --force-with-lease fence (Pitfall 13).
				g.Expect(got.Status.Git.LastPushedSHA).To(Equal(landedSHA),
					"LastPushedSHA must advance to the push-result envelope headSHA on success")
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
				g.Expect(got.Status.Phase).To(Equal(tideprojectv1alpha2.PhaseComplete),
					"exhausted retry must NOT regress Complete")
				c := meta.FindStatusCondition(got.Status.Conditions, tideprojectv1alpha2.ConditionBoundaryPushed)
				g.Expect(c).NotTo(BeNil())
				g.Expect(c.Status).To(Equal(metav1.ConditionFalse))
				g.Expect(c.Reason).To(Equal(tideprojectv1alpha2.ReasonPushFailed))
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
			Expect(got.Status.Phase).To(Equal(tideprojectv1alpha2.PhaseComplete),
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
			c := meta.FindStatusCondition(got.Status.Conditions, tideprojectv1alpha2.ConditionBoundaryPushed)
			Expect(c).NotTo(BeNil())
			Expect(c.Status).To(Equal(metav1.ConditionFalse))
			Expect(c.Reason).To(Equal(tideprojectv1alpha2.ReasonPushing))
		})
	})

	// Test 6: DASH-02 — under the intentional D-B5/R-05 single-writer coupling, the
	// shared tide-push-<uid> Job may own MULTIPLE attempt pods (BackoffLimit>0). A
	// transient first-attempt FAILURE (empty headSHA) must not mask the SUCCEEDED
	// attempt's landed headSHA when the boundary success-arm reads the envelope. This
	// pins the readPushEnvelope succeeded-pod preference — without it, pods.Items[0]
	// could surface the failed attempt's empty headSHA and freeze LastPushedSHA empty.
	Describe("Test 6: multi-pod Job — a failed attempt pod must not mask the succeeded pod's headSHA", func() {
		const name = "bp13b-multipod"
		AfterEach(func() { cleanup(name) })

		It("advances LastPushedSHA from the SUCCEEDED pod even when a failed-attempt pod sorts first", func() {
			const landedSHA = "89abcdef0123456789abcdef0123456789abcdef"
			proj := makeComplete(name, 1)
			jn := pushJobName(proj.UID)
			makePushJob(jn, proj.Name, proj.UID)
			markJobSucceeded(jn)
			// Two pods for the same (succeeded) Job. The FAILED attempt sorts FIRST
			// alphabetically (…-a-attempt) so a naive pods.Items[0] read surfaces its
			// empty headSHA; the SUCCEEDED attempt (…-z-attempt) carries the landed SHA.
			fakePushPodWith(jn+"-a-attempt", jn, corev1.PodFailed, "", "artifact-stage-failed", 1)
			fakePushPodWith(jn+"-z-attempt", jn, corev1.PodSucceeded, landedSHA, "", 0)

			r := newReconciler("tide-projects-bp13b-6")
			reconcileN(r, name, 3)

			Eventually(func(g Gomega) {
				got := getProject(name)
				c := meta.FindStatusCondition(got.Status.Conditions, tideprojectv1alpha2.ConditionBoundaryPushed)
				g.Expect(c).NotTo(BeNil())
				g.Expect(c.Status).To(Equal(metav1.ConditionTrue))
				g.Expect(c.Reason).To(Equal(tideprojectv1alpha2.ReasonPushed))
				g.Expect(got.Status.Git.LastPushedSHA).To(Equal(landedSHA),
					"LastPushedSHA must come from the SUCCEEDED pod, not a failed attempt's empty headSHA")
			}, 5*time.Second, 100*time.Millisecond).Should(Succeed())
		})
	})

	// Test 7: DASH-02 — a succeeded shared push Job whose headSHA is NOT readable (an
	// earlier artifact/level-boundary push whose Pod was GC'd, or a not-yet-populated
	// terminationMessage) must NOT be accepted as terminal success with an empty lease
	// anchor. Going terminal (BoundaryPushed=True) would freeze Status.Git.LastPushedSHA
	// empty forever (the terminal guard blocks all future capture). The success-arm must
	// instead replace the stale Job with a fresh owned push whose envelope is readable.
	Describe("Test 7: succeeded Job with an unreadable headSHA → re-dispatch, never wedge BoundaryPushed=True", func() {
		const name = "bp13b-nocapture"
		AfterEach(func() { cleanup(name) })

		It("re-dispatches a fresh owned push instead of going terminal without capturing the SHA", func() {
			proj := makeComplete(name, 0)
			jn := pushJobName(proj.UID)
			makePushJob(jn, proj.Name, proj.UID)
			markJobSucceeded(jn) // Job Complete, but NO pod/terminationMessage → headSHA unreadable

			r := newReconciler("tide-projects-bp13b-7")
			reconcileN(r, name, 4) // delete-stale + re-dispatch convergence

			Eventually(func(g Gomega) {
				got := getProject(name)
				// Must NOT have gone terminal-Pushed with an empty lease anchor.
				g.Expect(got.Status.Git.LastPushedSHA).To(BeEmpty())
				c := meta.FindStatusCondition(got.Status.Conditions, tideprojectv1alpha2.ConditionBoundaryPushed)
				g.Expect(c).NotTo(BeNil())
				g.Expect(c.Status).To(Equal(metav1.ConditionFalse),
					"an uncaptured headSHA must not terminally mark BoundaryPushed=True")
				g.Expect(c.Reason).To(Equal(tideprojectv1alpha2.ReasonPushing))
				g.Expect(got.Status.BoundaryPush.Attempts).To(BeNumerically(">=", 1),
					"the stale succeeded Job must be replaced by a fresh owned dispatch")
				// A push Job exists again (re-dispatched, not left as the stale one).
				var job batchv1.Job
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: jn, Namespace: "default"}, &job)).To(Succeed())
			}, 5*time.Second, 100*time.Millisecond).Should(Succeed())
		})
	})
})

// Phase 34 plan 34-05 Task 1: LastPushedSHA stamp (D-14), Pitfall-4
// stamp-skip tolerance, mid-run observation, and auto-clear.
var _ = Describe("ProjectReconciler — LastPushedSHA stamp + mid-run observation (Phase 34 D-14)", Label("envtest", "phase34", "lastpushedsha"), func() {
	ctx := context.Background()

	reconcileN := func(r *ProjectReconciler, name string, n int) {
		for range n {
			_, _ = r.Reconcile(ctx, reconcile.Request{NamespacedName: types.NamespacedName{Name: name, Namespace: "default"}})
		}
	}
	getProject := func(name string) tideprojectv1alpha2.Project {
		var got tideprojectv1alpha2.Project
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: "default"}, &got)).To(Succeed())
		return got
	}
	pushJobName := func(uid types.UID) string { return fmt.Sprintf("tide-push-%s", uid) }

	makeProjectAt := func(name string, phase string) *tideprojectv1alpha2.Project {
		proj := &tideprojectv1alpha2.Project{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
			Spec: tideprojectv1alpha2.ProjectSpec{SchemaRevision: "v1alpha2",
				TargetRepo: "https://github.com/example/test.git",
				Git: &tideprojectv1alpha2.GitConfig{
					RepoURL:        "https://github.com/example/test.git",
					CredsSecretRef: "test-creds",
				},
			},
		}
		Expect(k8sClient.Create(ctx, proj)).To(Succeed())
		waitForCacheSync(name, "default", &tideprojectv1alpha2.Project{})
		got := getProject(name)
		patch := client.MergeFrom(got.DeepCopy())
		got.Status.Phase = phase
		got.Status.Git.BranchName = "tide/run-" + name + "-1747200000"
		got.Status.Git.CloneComplete = true // skip clone dispatch for mid-run cases
		Expect(k8sClient.Status().Patch(ctx, &got, patch)).To(Succeed())
		return &got
	}

	makePushJobFor := func(jobName, projectName string, uid types.UID) {
		job := &batchv1.Job{
			ObjectMeta: metav1.ObjectMeta{
				Name:      jobName,
				Namespace: "default",
				OwnerReferences: []metav1.OwnerReference{{
					APIVersion:         tideprojectv1alpha2.GroupVersion.String(),
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
						Containers:    []corev1.Container{{Name: pushContainerName, Image: "ghcr.io/jsquirrelz/tide-push:test"}},
					},
				},
			},
		}
		Expect(k8sClient.Create(ctx, job)).To(Succeed())
	}

	markSucceeded := func(jobName string) {
		var job batchv1.Job
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: jobName, Namespace: "default"}, &job)).To(Succeed())
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

	attachEnvelopePod := func(jobName, headSHA string) {
		env := pushResultEnvelope{APIVersion: "tideproject.k8s/v1alpha1", Kind: "PushResult", HeadSHA: headSHA, ExitCode: 0}
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
		pod.Status.ContainerStatuses = []corev1.ContainerStatus{{
			Name: pushContainerName,
			State: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{
				ExitCode: 0, Reason: "Completed", Message: string(raw),
			}},
		}}
		Expect(k8sClient.Status().Patch(ctx, pod, sp)).To(Succeed())
	}

	cleanupSHA := func(name string) {
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
		var p tideprojectv1alpha2.Project
		if err := k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: "default"}, &p); err == nil {
			p.Finalizers = nil
			_ = k8sClient.Update(ctx, &p)
			_ = k8sClient.Delete(ctx, &p)
		}
	}

	newReconcilerSHA := func(pvc string) *ProjectReconciler {
		r := newTestProjectReconciler()
		r.TidePushImage = "ghcr.io/jsquirrelz/tide-push:test"
		r.SharedPVCName = pvc
		ensurePVC(ctx, pvc, "default")
		return r
	}

	Describe("Test 1: success arm stamps LastPushedSHA in the same pass as BoundaryPushed=True", func() {
		const name = "sha-complete-success"
		AfterEach(func() { cleanupSHA(name) })

		It("stamps Status.Git.LastPushedSHA from the envelope HeadSHA", func() {
			proj := makeProjectAt(name, tideprojectv1alpha2.PhaseComplete)
			jn := pushJobName(proj.UID)
			makePushJobFor(jn, proj.Name, proj.UID)
			markSucceeded(jn)
			attachEnvelopePod(jn, "abc123def456")

			r := newReconcilerSHA("tide-projects-sha-1")
			reconcileN(r, name, 3)

			Eventually(func(g Gomega) {
				got := getProject(name)
				c := meta.FindStatusCondition(got.Status.Conditions, tideprojectv1alpha2.ConditionBoundaryPushed)
				g.Expect(c).NotTo(BeNil())
				g.Expect(c.Status).To(Equal(metav1.ConditionTrue))
				g.Expect(got.Status.Git.LastPushedSHA).To(Equal("abc123def456"))
			}, 5*time.Second, 100*time.Millisecond).Should(Succeed())
		})
	})

	Describe("Test 2 (Pitfall 4): unreadable envelope does not block BoundaryPushed=True", func() {
		const name = "sha-stamp-skip"
		AfterEach(func() { cleanupSHA(name) })

		It("sets BoundaryPushed=True without an envelope and increments stamp-skip", func() {
			proj := makeProjectAt(name, tideprojectv1alpha2.PhaseComplete)
			jn := pushJobName(proj.UID)
			makePushJobFor(jn, proj.Name, proj.UID)
			markSucceeded(jn) // NO envelope pod attached — simulates TTL'd/GC'd pod

			before := testutil.ToFloat64(tidemetrics.IntegrationOutcomesTotal.WithLabelValues(name, "stamp-skip"))

			r := newReconcilerSHA("tide-projects-sha-2")
			reconcileN(r, name, 3)

			Eventually(func(g Gomega) {
				got := getProject(name)
				c := meta.FindStatusCondition(got.Status.Conditions, tideprojectv1alpha2.ConditionBoundaryPushed)
				g.Expect(c).NotTo(BeNil())
				g.Expect(c.Status).To(Equal(metav1.ConditionTrue), "BoundaryPushed=True must not block on envelope readability")
			}, 5*time.Second, 100*time.Millisecond).Should(Succeed())

			after := testutil.ToFloat64(tidemetrics.IntegrationOutcomesTotal.WithLabelValues(name, "stamp-skip"))
			Expect(after).To(BeNumerically(">", before))
		})
	})

	Describe("Test 3: mid-run (pre-Complete) success also stamps the SHA", func() {
		const name = "sha-midrun-success"
		AfterEach(func() { cleanupSHA(name) })

		It("stamps LastPushedSHA for a terminal push Job observed before Complete", func() {
			proj := makeProjectAt(name, tideprojectv1alpha2.PhaseRunning)
			jn := pushJobName(proj.UID)
			makePushJobFor(jn, proj.Name, proj.UID)
			markSucceeded(jn)
			attachEnvelopePod(jn, "midrun-sha-789")

			r := newReconcilerSHA("tide-projects-sha-3")
			reconcileN(r, name, 3)

			Eventually(func(g Gomega) {
				got := getProject(name)
				g.Expect(got.Status.Git.LastPushedSHA).To(Equal("midrun-sha-789"))
			}, 5*time.Second, 100*time.Millisecond).Should(Succeed())
		})
	})

	Describe("Test 3b: an empty-HeadSHA success envelope must not wipe the lease fence", func() {
		const name = "sha-empty-keeps-fence"
		AfterEach(func() { cleanupSHA(name) })

		It("keeps the previously-stamped LastPushedSHA and routes to stamp-skip", func() {
			proj := makeProjectAt(name, tideprojectv1alpha2.PhaseComplete)

			// A previous real push armed the D-B6 force-with-lease fence.
			pre := getProject(name)
			prePatch := client.MergeFrom(pre.DeepCopy())
			pre.Status.Git.LastPushedSHA = "armed-fence-sha-111"
			Expect(k8sClient.Status().Patch(ctx, &pre, prePatch)).To(Succeed())

			jn := pushJobName(proj.UID)
			makePushJobFor(jn, proj.Name, proj.UID)
			markSucceeded(jn)
			attachEnvelopePod(jn, "") // success envelope with empty HeadSHA

			before := testutil.ToFloat64(tidemetrics.IntegrationOutcomesTotal.WithLabelValues(name, "stamp-skip"))

			r := newReconcilerSHA("tide-projects-sha-3b")
			reconcileN(r, name, 3)

			Eventually(func(g Gomega) {
				got := getProject(name)
				c := meta.FindStatusCondition(got.Status.Conditions, tideprojectv1alpha2.ConditionBoundaryPushed)
				g.Expect(c).NotTo(BeNil())
				g.Expect(c.Status).To(Equal(metav1.ConditionTrue))
				g.Expect(got.Status.Git.LastPushedSHA).To(Equal("armed-fence-sha-111"),
					"an empty envelope HeadSHA must never clear the force-with-lease anchor")
			}, 5*time.Second, 100*time.Millisecond).Should(Succeed())

			after := testutil.ToFloat64(tidemetrics.IntegrationOutcomesTotal.WithLabelValues(name, "stamp-skip"))
			Expect(after).To(BeNumerically(">", before))
		})
	})

	Describe("Test 4: a later success auto-clears a sticky IntegrationIncomplete condition", func() {
		const name = "sha-autoclear"
		AfterEach(func() { cleanupSHA(name) })

		It("removes ConditionIntegrationIncomplete once a push succeeds", func() {
			proj := makeProjectAt(name, tideprojectv1alpha2.PhaseComplete)
			// Pre-seed the sticky condition as if a prior cap-exhaustion parked it.
			got := getProject(name)
			patch := client.MergeFrom(got.DeepCopy())
			meta.SetStatusCondition(&got.Status.Conditions, metav1.Condition{
				Type:               tideprojectv1alpha2.ConditionIntegrationIncomplete,
				Status:             metav1.ConditionTrue,
				Reason:             tideprojectv1alpha2.ReasonIntegrationIncomplete,
				Message:            "stale miss from a prior attempt",
				LastTransitionTime: metav1.Now(),
			})
			Expect(k8sClient.Status().Patch(ctx, &got, patch)).To(Succeed())

			jn := pushJobName(proj.UID)
			makePushJobFor(jn, proj.Name, proj.UID)
			markSucceeded(jn)
			attachEnvelopePod(jn, "cleared-sha")

			r := newReconcilerSHA("tide-projects-sha-4")
			reconcileN(r, name, 3)

			Eventually(func(g Gomega) {
				fresh := getProject(name)
				c := meta.FindStatusCondition(fresh.Status.Conditions, tideprojectv1alpha2.ConditionIntegrationIncomplete)
				g.Expect(c).To(BeNil(), "ConditionIntegrationIncomplete must auto-clear on a later successful push")
			}, 5*time.Second, 100*time.Millisecond).Should(Succeed())
		})
	})
})

// Phase 34 plan 34-05 Task 2: failure arms — miss retry-then-stick (D-08)
// and conflict park (D-09/D-11/D-12).
var _ = Describe("ProjectReconciler — integration-miss + merge-conflict failure arms (Phase 34)", Label("envtest", "phase34", "integrationincomplete"), func() {
	ctx := context.Background()

	reconcileN := func(r *ProjectReconciler, name string, n int) {
		for range n {
			_, _ = r.Reconcile(ctx, reconcile.Request{NamespacedName: types.NamespacedName{Name: name, Namespace: "default"}})
		}
	}
	getProject := func(name string) tideprojectv1alpha2.Project {
		var got tideprojectv1alpha2.Project
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: "default"}, &got)).To(Succeed())
		return got
	}
	pushJobName := func(uid types.UID) string { return fmt.Sprintf("tide-push-%s", uid) }

	makeCompleteIM := func(name string, attempts int32) *tideprojectv1alpha2.Project {
		proj := &tideprojectv1alpha2.Project{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
			Spec: tideprojectv1alpha2.ProjectSpec{SchemaRevision: "v1alpha2",
				TargetRepo: "https://github.com/example/test.git",
				Git: &tideprojectv1alpha2.GitConfig{
					RepoURL:        "https://github.com/example/test.git",
					CredsSecretRef: "test-creds",
				},
			},
		}
		Expect(k8sClient.Create(ctx, proj)).To(Succeed())
		waitForCacheSync(name, "default", &tideprojectv1alpha2.Project{})
		got := getProject(name)
		patch := client.MergeFrom(got.DeepCopy())
		got.Status.Phase = tideprojectv1alpha2.PhaseComplete
		got.Status.Git.BranchName = "tide/run-" + name + "-1747200000"
		got.Status.BoundaryPush.Attempts = attempts
		Expect(k8sClient.Status().Patch(ctx, &got, patch)).To(Succeed())
		return &got
	}

	makePushJobIM := func(jobName, projectName string, uid types.UID) {
		job := &batchv1.Job{
			ObjectMeta: metav1.ObjectMeta{
				Name:      jobName,
				Namespace: "default",
				OwnerReferences: []metav1.OwnerReference{{
					APIVersion:         tideprojectv1alpha2.GroupVersion.String(),
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
						Containers:    []corev1.Container{{Name: pushContainerName, Image: "ghcr.io/jsquirrelz/tide-push:test"}},
					},
				},
			},
		}
		Expect(k8sClient.Create(ctx, job)).To(Succeed())
	}

	markFailedIM := func(jobName string) {
		var job batchv1.Job
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: jobName, Namespace: "default"}, &job)).To(Succeed())
		now := metav1.Now()
		job.Status.StartTime = &now
		job.Status.Failed = 1
		job.Status.Conditions = []batchv1.JobCondition{
			{Type: batchv1.JobFailureTarget, Status: corev1.ConditionTrue, LastTransitionTime: now, Reason: "BackoffLimitExceeded"},
			{Type: batchv1.JobFailed, Status: corev1.ConditionTrue, LastTransitionTime: now, Reason: "BackoffLimitExceeded"},
		}
		Expect(k8sClient.Status().Update(ctx, &job)).To(Succeed())
	}

	attachMissEnvelope := func(jobName string, missing []string, total int) {
		env := pushResultEnvelope{
			APIVersion: "tideproject.k8s/v1alpha1", Kind: "PushResult",
			ExitCode: 14, Reason: "integration-incomplete",
			MissingBranches: missing, MissingTotal: total,
		}
		raw, err := json.Marshal(env)
		Expect(err).NotTo(HaveOccurred())
		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name: jobName + "-pod", Namespace: "default",
				Labels: map[string]string{"job-name": jobName},
			},
			Spec: corev1.PodSpec{
				RestartPolicy: corev1.RestartPolicyNever,
				Containers:    []corev1.Container{{Name: pushContainerName, Image: "ghcr.io/jsquirrelz/tide-push:test"}},
			},
		}
		Expect(k8sClient.Create(ctx, pod)).To(Succeed())
		sp := client.MergeFrom(pod.DeepCopy())
		pod.Status.ContainerStatuses = []corev1.ContainerStatus{{
			Name: pushContainerName,
			State: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{
				ExitCode: 14, Reason: "Error", Message: string(raw),
			}},
		}}
		Expect(k8sClient.Status().Patch(ctx, pod, sp)).To(Succeed())
	}

	attachConflictEnvelope := func(jobName, conflictBranch string) {
		env := pushResultEnvelope{
			APIVersion: "tideproject.k8s/v1alpha1", Kind: "PushResult",
			ExitCode: 15, Reason: "merge-conflict", ConflictBranch: conflictBranch,
		}
		raw, err := json.Marshal(env)
		Expect(err).NotTo(HaveOccurred())
		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name: jobName + "-pod", Namespace: "default",
				Labels: map[string]string{"job-name": jobName},
			},
			Spec: corev1.PodSpec{
				RestartPolicy: corev1.RestartPolicyNever,
				Containers:    []corev1.Container{{Name: pushContainerName, Image: "ghcr.io/jsquirrelz/tide-push:test"}},
			},
		}
		Expect(k8sClient.Create(ctx, pod)).To(Succeed())
		sp := client.MergeFrom(pod.DeepCopy())
		pod.Status.ContainerStatuses = []corev1.ContainerStatus{{
			Name: pushContainerName,
			State: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{
				ExitCode: 15, Reason: "Error", Message: string(raw),
			}},
		}}
		Expect(k8sClient.Status().Patch(ctx, pod, sp)).To(Succeed())
	}

	cleanupIM := func(name string) {
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
		var p tideprojectv1alpha2.Project
		if err := k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: "default"}, &p); err == nil {
			p.Finalizers = nil
			_ = k8sClient.Update(ctx, &p)
			_ = k8sClient.Delete(ctx, &p)
		}
	}

	newReconcilerIM := func(pvc string) *ProjectReconciler {
		r := newTestProjectReconciler()
		r.TidePushImage = "ghcr.io/jsquirrelz/tide-push:test"
		r.SharedPVCName = pvc
		ensurePVC(ctx, pvc, "default")
		return r
	}

	Describe("Test 1 (D-08 retry): integration-incomplete rides bounded retry, no sticky condition yet", func() {
		const name = "im-retry"
		AfterEach(func() { cleanupIM(name) })

		It("increments Attempts and does not park sticky", func() {
			proj := makeCompleteIM(name, 0)
			jn := pushJobName(proj.UID)
			makePushJobIM(jn, proj.Name, proj.UID)
			markFailedIM(jn)
			attachMissEnvelope(jn, []string{"tide/wt-uid-a"}, 1)

			before := testutil.ToFloat64(tidemetrics.IntegrationOutcomesTotal.WithLabelValues(name, "miss"))

			r := newReconcilerIM("tide-projects-im-1")
			reconcileN(r, name, 4)

			Eventually(func(g Gomega) {
				got := getProject(name)
				g.Expect(got.Status.BoundaryPush.Attempts).To(BeNumerically(">=", 1))
				g.Expect(got.Status.BoundaryPush.LastError).To(HavePrefix(integrationIncompleteLastErrorPrefix))
				c := meta.FindStatusCondition(got.Status.Conditions, tideprojectv1alpha2.ConditionIntegrationIncomplete)
				g.Expect(c).To(BeNil(), "sticky condition must not appear before the retry cap")
			}, 5*time.Second, 100*time.Millisecond).Should(Succeed())

			after := testutil.ToFloat64(tidemetrics.IntegrationOutcomesTotal.WithLabelValues(name, "miss"))
			Expect(after).To(BeNumerically(">", before))
		})
	})

	Describe("Test 2 (D-08 stick at cap): sticky condition names missing task+branch after cap", func() {
		const name = "im-stick"
		AfterEach(func() { cleanupIM(name) })

		It("parks with ConditionIntegrationIncomplete naming the missing branch", func() {
			proj := makeCompleteIM(name, maxBoundaryPushAttempts-1)
			jn := pushJobName(proj.UID)
			makePushJobIM(jn, proj.Name, proj.UID)
			markFailedIM(jn)
			attachMissEnvelope(jn, []string{"tide/wt-uid-missing"}, 1)

			r := newReconcilerIM("tide-projects-im-2")
			reconcileN(r, name, 4)

			Eventually(func(g Gomega) {
				got := getProject(name)
				g.Expect(got.Status.BoundaryPush.Attempts).To(Equal(int32(maxBoundaryPushAttempts)))
				c := meta.FindStatusCondition(got.Status.Conditions, tideprojectv1alpha2.ConditionIntegrationIncomplete)
				g.Expect(c).NotTo(BeNil())
				g.Expect(c.Status).To(Equal(metav1.ConditionTrue))
				g.Expect(c.Reason).To(Equal(tideprojectv1alpha2.ReasonIntegrationIncomplete))
				g.Expect(c.Message).To(ContainSubstring("tide/wt-uid-missing"))
			}, 5*time.Second, 100*time.Millisecond).Should(Succeed())

			// Parked: no further push Job dispatch attempts (Attempts stays at cap).
			got := getProject(name)
			Expect(got.Status.BoundaryPush.Attempts).To(Equal(int32(maxBoundaryPushAttempts)))
		})
	})

	Describe("Test 3 (D-09 conflict park): merge-conflict parks immediately, zero retries burned", func() {
		const name = "im-conflict"
		AfterEach(func() { cleanupIM(name) })

		It("parks with ConditionIntegrationIncomplete/MergeConflict and does not increment Attempts", func() {
			proj := makeCompleteIM(name, 0)
			jn := pushJobName(proj.UID)
			makePushJobIM(jn, proj.Name, proj.UID)
			markFailedIM(jn)
			attachConflictEnvelope(jn, "tide/wt-uid-conflicter")

			before := testutil.ToFloat64(tidemetrics.IntegrationOutcomesTotal.WithLabelValues(name, "conflict"))

			r := newReconcilerIM("tide-projects-im-3")
			reconcileN(r, name, 4)

			Eventually(func(g Gomega) {
				got := getProject(name)
				c := meta.FindStatusCondition(got.Status.Conditions, tideprojectv1alpha2.ConditionIntegrationIncomplete)
				g.Expect(c).NotTo(BeNil())
				g.Expect(c.Status).To(Equal(metav1.ConditionTrue))
				g.Expect(c.Reason).To(Equal(tideprojectv1alpha2.ReasonMergeConflict))
				g.Expect(c.Message).To(ContainSubstring("tide/wt-uid-conflicter"))
				g.Expect(got.Status.BoundaryPush.Attempts).To(Equal(int32(0)), "a conflict must not burn the retry budget")
			}, 5*time.Second, 100*time.Millisecond).Should(Succeed())

			after := testutil.ToFloat64(tidemetrics.IntegrationOutcomesTotal.WithLabelValues(name, "conflict"))
			Expect(after).To(BeNumerically(">", before))
		})
	})

	Describe("Test 3b (D-09 park is sticky): TTL-deleting the failed Job must not re-dispatch a doomed push", func() {
		const name = "im-conflict-ttl"
		AfterEach(func() { cleanupIM(name) })

		It("does not create a new push Job after the parked conflict Job is GC'd", func() {
			proj := makeCompleteIM(name, 0)
			jn := pushJobName(proj.UID)
			makePushJobIM(jn, proj.Name, proj.UID)
			markFailedIM(jn)
			attachConflictEnvelope(jn, "tide/wt-uid-ttl-conflicter")

			r := newReconcilerIM("tide-projects-im-3b")
			reconcileN(r, name, 3)

			Eventually(func(g Gomega) {
				got := getProject(name)
				c := meta.FindStatusCondition(got.Status.Conditions, tideprojectv1alpha2.ConditionIntegrationIncomplete)
				g.Expect(c).NotTo(BeNil())
				g.Expect(c.Reason).To(Equal(tideprojectv1alpha2.ReasonMergeConflict))
			}, 5*time.Second, 100*time.Millisecond).Should(Succeed())

			// TTLSecondsAfterFinished GC's the failed Job (and its pod).
			var job batchv1.Job
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: jn, Namespace: "default"}, &job)).To(Succeed())
			policy := metav1.DeletePropagationBackground
			Expect(k8sClient.Delete(ctx, &job, &client.DeleteOptions{PropagationPolicy: &policy})).To(Succeed())
			Eventually(func() bool {
				var j batchv1.Job
				return apierrors.IsNotFound(k8sClient.Get(ctx, types.NamespacedName{Name: jn, Namespace: "default"}, &j))
			}, 5*time.Second, 100*time.Millisecond).Should(BeTrue())

			// A deterministic content conflict cannot be fixed by retrying:
			// the Complete fast-path must NOT re-create the push Job while
			// the MergeConflict park stands.
			reconcileN(r, name, 3)
			Consistently(func() bool {
				var j batchv1.Job
				return apierrors.IsNotFound(k8sClient.Get(ctx, types.NamespacedName{Name: jn, Namespace: "default"}, &j))
			}, 2*time.Second, 200*time.Millisecond).Should(BeTrue(),
				"parked merge-conflict re-dispatched a doomed push Job after TTL GC")
		})
	})

	Describe("Test 4b (D-08 cap event): the exhaustion Warning fires once, not per reconcile", func() {
		const name = "im-cap-event"
		AfterEach(func() { cleanupIM(name) })

		It("emits a single IntegrationIncomplete event at the cap", func() {
			proj := makeCompleteIM(name, maxBoundaryPushAttempts-1)
			jn := pushJobName(proj.UID)
			makePushJobIM(jn, proj.Name, proj.UID)
			markFailedIM(jn)
			attachMissEnvelope(jn, []string{"tide/wt-uid-cap-missing"}, 1)

			r := newReconcilerIM("tide-projects-im-4b")
			rec := record.NewFakeRecorder(20)
			r.Recorder = rec

			// Classification pass takes Attempts to the cap, then several
			// parked passes — the Warning must not repeat per pass.
			reconcileN(r, name, 6)

			Eventually(func(g Gomega) {
				got := getProject(name)
				c := meta.FindStatusCondition(got.Status.Conditions, tideprojectv1alpha2.ConditionIntegrationIncomplete)
				g.Expect(c).NotTo(BeNil())
			}, 5*time.Second, 100*time.Millisecond).Should(Succeed())
			reconcileN(r, name, 3)

			count := 0
			for {
				select {
				case e := <-rec.Events:
					if strings.Contains(e, tideprojectv1alpha2.ReasonIntegrationIncomplete) {
						count++
					}
				default:
					Expect(count).To(Equal(1), "cap-exhaustion Warning must fire exactly once, not per reconcile")
					return
				}
			}
		})
	})
})

// Phase 34 plan 34-05 Task 3: controller-side consumption of the
// reset-boundary-push annotation (D-13).
var _ = Describe("ProjectReconciler — reset-boundary-push annotation consumption (Phase 34 D-13)", Label("envtest", "phase34", "resetboundarypush"), func() {
	ctx := context.Background()

	Describe("consuming the annotation resets Attempts, clears the condition, and re-dispatches", func() {
		const name = "reset-bp-consume"

		AfterEach(func() {
			var jobs batchv1.JobList
			_ = k8sClient.List(ctx, &jobs, client.InNamespace("default"))
			for i := range jobs.Items {
				j := jobs.Items[i]
				_ = k8sClient.Delete(ctx, &j)
			}
			var p tideprojectv1alpha2.Project
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: "default"}, &p); err == nil {
				p.Finalizers = nil
				_ = k8sClient.Update(ctx, &p)
				_ = k8sClient.Delete(ctx, &p)
			}
		})

		It("resets BoundaryPush state, removes the condition, consumes the annotation once, and re-dispatches", func() {
			proj := &tideprojectv1alpha2.Project{
				ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
				Spec: tideprojectv1alpha2.ProjectSpec{SchemaRevision: "v1alpha2",
					TargetRepo: "https://github.com/example/test.git",
					Git: &tideprojectv1alpha2.GitConfig{
						RepoURL:        "https://github.com/example/test.git",
						CredsSecretRef: "test-creds",
					},
				},
			}
			Expect(k8sClient.Create(ctx, proj)).To(Succeed())
			waitForCacheSync(name, "default", &tideprojectv1alpha2.Project{})

			var got tideprojectv1alpha2.Project
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: "default"}, &got)).To(Succeed())
			patch := client.MergeFrom(got.DeepCopy())
			got.Status.Phase = tideprojectv1alpha2.PhaseComplete
			got.Status.Git.BranchName = "tide/run-" + name + "-1747200000"
			got.Status.BoundaryPush.Attempts = maxBoundaryPushAttempts
			got.Status.BoundaryPush.LastError = integrationIncompleteLastErrorPrefix + "stale detail"
			meta.SetStatusCondition(&got.Status.Conditions, metav1.Condition{
				Type:               tideprojectv1alpha2.ConditionIntegrationIncomplete,
				Status:             metav1.ConditionTrue,
				Reason:             tideprojectv1alpha2.ReasonIntegrationIncomplete,
				Message:            "stale detail",
				LastTransitionTime: metav1.Now(),
			})
			Expect(k8sClient.Status().Patch(ctx, &got, patch)).To(Succeed())

			// Apply the reset annotation (the CLI's write-back surface).
			var toAnnotate tideprojectv1alpha2.Project
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: "default"}, &toAnnotate)).To(Succeed())
			annPatch := client.MergeFrom(toAnnotate.DeepCopy())
			if toAnnotate.Annotations == nil {
				toAnnotate.Annotations = map[string]string{}
			}
			toAnnotate.Annotations["tideproject.k8s/reset-boundary-push"] = "true"
			Expect(k8sClient.Patch(ctx, &toAnnotate, annPatch)).To(Succeed())

			r := newTestProjectReconciler()
			r.TidePushImage = "ghcr.io/jsquirrelz/tide-push:test"
			r.SharedPVCName = "tide-projects-reset-bp"
			ensurePVC(ctx, "tide-projects-reset-bp", "default")

			for range 4 {
				_, _ = r.Reconcile(ctx, reconcile.Request{NamespacedName: types.NamespacedName{Name: name, Namespace: "default"}})
			}

			Eventually(func(g Gomega) {
				fresh := getProjectResetBP(ctx, name)
				g.Expect(fresh.Annotations).NotTo(HaveKey("tideproject.k8s/reset-boundary-push"), "annotation must be consumed (removed)")
				// The reset zeroes Attempts, but this same test loop lets the
				// state machine re-dispatch immediately afterward (Complete +
				// no Job yet), which increments Attempts back to 1 — so the
				// observable post-loop value is <=1, not necessarily ==0.
				// The reset itself is proven by: condition gone + a fresh Job
				// dispatched + LastError cleared (not the stale pre-reset value).
				g.Expect(fresh.Status.BoundaryPush.Attempts).To(BeNumerically("<=", 1))
				g.Expect(fresh.Status.BoundaryPush.LastError).NotTo(ContainSubstring("stale detail"))
				c := meta.FindStatusCondition(fresh.Status.Conditions, tideprojectv1alpha2.ConditionIntegrationIncomplete)
				g.Expect(c).To(BeNil())
				// Re-dispatch occurred: a fresh push Job exists.
				var job batchv1.Job
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: fmt.Sprintf("tide-push-%s", fresh.UID), Namespace: "default"}, &job)).To(Succeed())
			}, 5*time.Second, 100*time.Millisecond).Should(Succeed())
		})
	})
})

func getProjectResetBP(ctx context.Context, name string) tideprojectv1alpha2.Project {
	var got tideprojectv1alpha2.Project
	Expect(k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: "default"}, &got)).To(Succeed())
	return got
}
