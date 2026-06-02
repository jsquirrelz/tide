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

	"github.com/prometheus/client_golang/prometheus/testutil"

	tideprojectv1alpha1 "github.com/jsquirrelz/tide/api/v1alpha1"
	"github.com/jsquirrelz/tide/internal/metrics"
)

// Plan 04-06 Task 1: push-result envelope reason parsing in ProjectReconciler.
//
// W-1 (leak-detected, exit code 10) → Status.Phase=PhasePushLeakBlocked,
// Condition PushLeakBlocked True (Reason=LeakDetected),
// metrics.SecretLeakBlockedTotal{project, "", ""}.Inc().
//
// Lease-rejected (exit code 11) → existing PhasePushLeaseFailed behavior
// preserved (no counter increment).
//
// Empty/unknown reason → fallback to PhasePushLeaseFailed (today's default,
// preserves bypass-push-lease annotation recovery path).
var _ = Describe("ProjectReconciler — push-result envelope reason parsing (Plan 04-06 Task 1)", Label("envtest", "phase4", "pushresult"), func() {
	ctx := context.Background()

	// fakePushJobPod creates a Pod labeled to be the "first pod" of the named
	// push Job. envtest does not run pods; we set ContainerStatuses on the
	// Pod's Status to carry the synthetic terminationMessage JSON.
	fakePushJobPod := func(jobName, namespace string, envelope pushResultEnvelope) {
		raw, err := json.Marshal(envelope)
		Expect(err).NotTo(HaveOccurred())

		podName := fmt.Sprintf("%s-pod", jobName)
		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      podName,
				Namespace: namespace,
				Labels: map[string]string{
					"job-name": jobName,
				},
			},
			Spec: corev1.PodSpec{
				RestartPolicy: corev1.RestartPolicyNever,
				Containers: []corev1.Container{
					{
						Name:  pushContainerName,
						Image: "ghcr.io/jsquirrelz/tide-push:test",
					},
				},
			},
		}
		Expect(k8sClient.Create(ctx, pod)).To(Succeed())

		// Patch Pod status to set ContainerStatuses[0].State.Terminated.Message
		// — the synthetic envelope JSON the reconciler parses.
		statusPatch := client.MergeFrom(pod.DeepCopy())
		pod.Status.Phase = corev1.PodFailed
		pod.Status.ContainerStatuses = []corev1.ContainerStatus{
			{
				Name: pushContainerName,
				State: corev1.ContainerState{
					Terminated: &corev1.ContainerStateTerminated{
						ExitCode: int32(envelope.ExitCode),
						Reason:   "Error",
						Message:  string(raw),
					},
				},
			},
		}
		Expect(k8sClient.Status().Patch(ctx, pod, statusPatch)).To(Succeed())
	}

	// markJobFailed transitions a Job to JobFailed=True so isJobFailed()
	// returns true. K8s 1.30+ enforces ordering: FailureTarget=true must be
	// set before Failed=true; completionTime requires Complete=true. We omit
	// completionTime on failure (Status.Failed counter is enough for
	// isJobFailed).
	markJobFailed := func(jobName, namespace string) {
		var job batchv1.Job
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: jobName, Namespace: namespace}, &job)).To(Succeed())
		now := metav1.Now()
		job.Status.StartTime = &now
		job.Status.Failed = 1
		job.Status.Conditions = []batchv1.JobCondition{
			{
				Type:               batchv1.JobFailureTarget,
				Status:             corev1.ConditionTrue,
				LastTransitionTime: now,
				Reason:             "PodFailurePolicy",
			},
			{
				Type:               batchv1.JobFailed,
				Status:             corev1.ConditionTrue,
				LastTransitionTime: now,
				Reason:             "PodFailurePolicy",
			},
		}
		Expect(k8sClient.Status().Update(ctx, &job)).To(Succeed())
	}

	// makePushJob creates a placeholder push Job (no pods, no terminal state).
	// The reconciler reads its phase before patching; we mark Failed via
	// markJobFailed after envelope pod is in place.
	makePushJob := func(name, namespace, projectName string, projectUID types.UID) *batchv1.Job {
		job := &batchv1.Job{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: namespace,
				OwnerReferences: []metav1.OwnerReference{
					{
						APIVersion:         tideprojectv1alpha1.GroupVersion.String(),
						Kind:               "Project",
						Name:               projectName,
						UID:                projectUID,
						Controller:         new(true),
						BlockOwnerDeletion: new(true),
					},
				},
			},
			Spec: batchv1.JobSpec{
				Template: corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						RestartPolicy: corev1.RestartPolicyNever,
						Containers: []corev1.Container{
							{
								Name:  pushContainerName,
								Image: "ghcr.io/jsquirrelz/tide-push:test",
							},
						},
					},
				},
			},
		}
		Expect(k8sClient.Create(ctx, job)).To(Succeed())
		return job
	}

	// cleanupProject removes the Project + push Job + push Pod.
	cleanupProject := func(name string) {
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

	// makeProjectInPhaseComplete creates a Project, drives it to
	// PhaseComplete, ensures Status.Git.BranchName + RepoURL are set so the
	// push Job dispatch path can run.
	makeProjectInPhaseComplete := func(name string) *tideprojectv1alpha1.Project {
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

		var got tideprojectv1alpha1.Project
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: "default"}, &got)).To(Succeed())
		patch := client.MergeFrom(got.DeepCopy())
		got.Status.Phase = tideprojectv1alpha1.PhaseComplete
		got.Status.Git.BranchName = "tide/run-" + name + "-1747200000"
		Expect(k8sClient.Status().Patch(ctx, &got, patch)).To(Succeed())
		return &got
	}

	Describe("Test 1: exit-10 (leak-detected) → PhasePushLeakBlocked + counter", func() {
		const projectName = "pushres-proj-1"

		AfterEach(func() {
			cleanupProject(projectName)
		})

		It("patches Status.Phase=PhasePushLeakBlocked AND increments SecretLeakBlockedTotal{project, '', ''}", func() {
			// Reset the metric before the test.
			metrics.SecretLeakBlockedTotal.Reset()

			proj := makeProjectInPhaseComplete(projectName)
			pushJobName := fmt.Sprintf("tide-push-%s", proj.UID)
			makePushJob(pushJobName, "default", proj.Name, proj.UID)
			fakePushJobPod(pushJobName, "default", pushResultEnvelope{
				APIVersion: "tideproject.k8s/v1alpha1",
				Kind:       "PushResult",
				ProjectUID: string(proj.UID),
				ExitCode:   10,
				Reason:     "leak-detected",
			})
			markJobFailed(pushJobName, "default")

			r := newTestProjectReconciler()
			r.TidePushImage = "ghcr.io/jsquirrelz/tide-push:test"
			r.SharedPVCName = "tide-projects-pushres-1"
			// Ensure the shared PVC exists so the Project reconcile path proceeds.
			ensurePVC(ctx, r.SharedPVCName, "default")

			for range 5 {
				_, _ = r.Reconcile(ctx, reconcile.Request{NamespacedName: types.NamespacedName{Name: projectName, Namespace: "default"}})
			}

			Eventually(func(g Gomega) {
				var got tideprojectv1alpha1.Project
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: projectName, Namespace: "default"}, &got)).To(Succeed())
				g.Expect(string(got.Status.Phase)).To(Equal(tideprojectv1alpha1.PhasePushLeakBlocked))
				c := meta.FindStatusCondition(got.Status.Conditions, tideprojectv1alpha1.ConditionPushLeakBlocked)
				g.Expect(c).NotTo(BeNil(), "ConditionPushLeakBlocked should be set")
				g.Expect(c.Status).To(Equal(metav1.ConditionTrue))
				g.Expect(c.Reason).To(Equal("LeakDetected"))
			}, 5*time.Second, 100*time.Millisecond).Should(Succeed())

			// SecretLeakBlockedTotal{project, "", ""} should be 1.0.
			count := testutil.ToFloat64(metrics.SecretLeakBlockedTotal.WithLabelValues(projectName, "", ""))
			Expect(count).To(BeNumerically(">=", 1.0), "SecretLeakBlockedTotal counter not incremented on leak path")
		})
	})

	Describe("Test 3: exit-11 (lease-rejected) → PhasePushLeaseFailed (unchanged)", func() {
		const projectName = "pushres-proj-2"

		AfterEach(func() {
			cleanupProject(projectName)
		})

		It("preserves today's PhasePushLeaseFailed behavior", func() {
			metrics.SecretLeakBlockedTotal.Reset()
			proj := makeProjectInPhaseComplete(projectName)
			pushJobName := fmt.Sprintf("tide-push-%s", proj.UID)
			makePushJob(pushJobName, "default", proj.Name, proj.UID)
			fakePushJobPod(pushJobName, "default", pushResultEnvelope{
				APIVersion: "tideproject.k8s/v1alpha1",
				Kind:       "PushResult",
				ProjectUID: string(proj.UID),
				ExitCode:   11,
				Reason:     "lease-rejected",
			})
			markJobFailed(pushJobName, "default")

			r := newTestProjectReconciler()
			r.TidePushImage = "ghcr.io/jsquirrelz/tide-push:test"
			r.SharedPVCName = "tide-projects-pushres-2"
			ensurePVC(ctx, r.SharedPVCName, "default")

			for range 5 {
				_, _ = r.Reconcile(ctx, reconcile.Request{NamespacedName: types.NamespacedName{Name: projectName, Namespace: "default"}})
			}

			Eventually(func(g Gomega) {
				var got tideprojectv1alpha1.Project
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: projectName, Namespace: "default"}, &got)).To(Succeed())
				g.Expect(string(got.Status.Phase)).To(Equal(tideprojectv1alpha1.PhasePushLeaseFailed))
				c := meta.FindStatusCondition(got.Status.Conditions, tideprojectv1alpha1.ConditionPushLeaseFailed)
				g.Expect(c).NotTo(BeNil())
				g.Expect(c.Status).To(Equal(metav1.ConditionTrue))
				g.Expect(c.Reason).To(Equal("LeaseRejected"))
			}, 5*time.Second, 100*time.Millisecond).Should(Succeed())

			// Test 5: lease-rejected does NOT increment SecretLeakBlockedTotal.
			count := testutil.ToFloat64(metrics.SecretLeakBlockedTotal.WithLabelValues(projectName, "", ""))
			Expect(count).To(BeNumerically("==", 0.0), "SecretLeakBlockedTotal must NOT increment on lease-rejected")
		})
	})

	Describe("Test 4: unknown reason → falls back to PhasePushLeaseFailed", func() {
		const projectName = "pushres-proj-3"

		AfterEach(func() {
			cleanupProject(projectName)
		})

		It("preserves bypass-push-lease annotation recovery path", func() {
			proj := makeProjectInPhaseComplete(projectName)
			pushJobName := fmt.Sprintf("tide-push-%s", proj.UID)
			makePushJob(pushJobName, "default", proj.Name, proj.UID)
			fakePushJobPod(pushJobName, "default", pushResultEnvelope{
				APIVersion: "tideproject.k8s/v1alpha1",
				Kind:       "PushResult",
				ProjectUID: string(proj.UID),
				ExitCode:   1,
				Reason:     "", // empty/unknown
			})
			markJobFailed(pushJobName, "default")

			r := newTestProjectReconciler()
			r.TidePushImage = "ghcr.io/jsquirrelz/tide-push:test"
			r.SharedPVCName = "tide-projects-pushres-3"
			ensurePVC(ctx, r.SharedPVCName, "default")

			for range 5 {
				_, _ = r.Reconcile(ctx, reconcile.Request{NamespacedName: types.NamespacedName{Name: projectName, Namespace: "default"}})
			}

			Eventually(func(g Gomega) {
				var got tideprojectv1alpha1.Project
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: projectName, Namespace: "default"}, &got)).To(Succeed())
				g.Expect(string(got.Status.Phase)).To(Equal(tideprojectv1alpha1.PhasePushLeaseFailed),
					"unknown reason should fall back to PhasePushLeaseFailed (preserves bypass recovery path)")
			}, 5*time.Second, 100*time.Millisecond).Should(Succeed())
		})
	})

	Describe("Test 6: ConditionPushLeakBlocked constant exists in api/v1alpha1", func() {
		It("api/v1alpha1.ConditionPushLeakBlocked equals 'PushLeakBlocked'", func() {
			Expect(tideprojectv1alpha1.ConditionPushLeakBlocked).To(Equal("PushLeakBlocked"))
		})
	})
})
