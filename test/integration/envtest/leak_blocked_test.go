/*
Copyright 2026 TIDE Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

package envtest_integration

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
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/prometheus/client_golang/prometheus/testutil"

	tideprojectv1alpha1 "github.com/jsquirrelz/tide/api/v1alpha1"
	controller "github.com/jsquirrelz/tide/internal/controller"
	tidemetrics "github.com/jsquirrelz/tide/internal/metrics"
)

// Plan 04-06 Task 3 — W-1 integration envtest covering the push-result
// envelope reason path end-to-end.
//
//	TestProject_LeakBlocked          — exit-10 / reason="leak-detected"
//	                                    → PhasePushLeakBlocked + counter
//	                                    increments.
//	TestProject_LeaseFailed_NoLeakCounter — exit-11 / reason="lease-rejected"
//	                                    → PhasePushLeaseFailed (today's
//	                                    behavior); SecretLeakBlockedTotal
//	                                    counter UNCHANGED.
//
// Tests share a namespace but use distinct Project names so the metric
// label tuples don't collide.
var _ = Describe("Plan 04-06 Task 3 — W-1 leak-blocked integration envtest", Label("envtest", "phase4", "leak-blocked-integration"), func() {
	ctx := context.Background()

	// pushEnvelopePodHelper sets a Pod's terminationMessage to a synthetic
	// pushResult envelope so the ProjectReconciler reads (envelope.Reason,
	// envelope.ExitCode) and classifies the push outcome accordingly.
	type pushResultMirror struct {
		APIVersion string `json:"apiVersion"`
		Kind       string `json:"kind"`
		ProjectUID string `json:"projectUID"`
		Branch     string `json:"branch"`
		HeadSHA    string `json:"headSHA"`
		ExitCode   int    `json:"exitCode"`
		Reason     string `json:"reason"`
	}

	createPushPodWithEnvelope := func(jobName, namespace string, env pushResultMirror) {
		raw, err := json.Marshal(env)
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
						Name:  "push",
						Image: "ghcr.io/jsquirrelz/tide-push:test",
					},
				},
			},
		}
		Expect(k8sClient.Create(ctx, pod)).To(Succeed())
		statusPatch := client.MergeFrom(pod.DeepCopy())
		pod.Status.Phase = corev1.PodFailed
		pod.Status.ContainerStatuses = []corev1.ContainerStatus{
			{
				Name: "push",
				State: corev1.ContainerState{
					Terminated: &corev1.ContainerStateTerminated{
						ExitCode: int32(env.ExitCode),
						Reason:   "Error",
						Message:  string(raw),
					},
				},
			},
		}
		Expect(k8sClient.Status().Patch(ctx, pod, statusPatch)).To(Succeed())
	}

	markPushJobFailed := func(jobName, namespace string) {
		var job batchv1.Job
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: jobName, Namespace: namespace}, &job)).To(Succeed())
		now := metav1.Now()
		job.Status.StartTime = &now
		job.Status.Failed = 1
		job.Status.Conditions = []batchv1.JobCondition{
			{
				Type: batchv1.JobFailureTarget, Status: corev1.ConditionTrue,
				LastTransitionTime: now, Reason: "PodFailurePolicy",
			},
			{
				Type: batchv1.JobFailed, Status: corev1.ConditionTrue,
				LastTransitionTime: now, Reason: "PodFailurePolicy",
			},
		}
		Expect(k8sClient.Status().Update(ctx, &job)).To(Succeed())
	}

	createPushJob := func(name, namespace, projectName string, uid types.UID) {
		job := &batchv1.Job{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: namespace,
				OwnerReferences: []metav1.OwnerReference{
					{
						APIVersion:         tideprojectv1alpha1.GroupVersion.String(),
						Kind:               "Project",
						Name:               projectName,
						UID:                uid,
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
								Name:  "push",
								Image: "ghcr.io/jsquirrelz/tide-push:test",
							},
						},
					},
				},
			},
		}
		Expect(k8sClient.Create(ctx, job)).To(Succeed())
	}

	// ensureBoundPVCIT creates a Bound PVC if it doesn't exist (mirrors
	// project_phase3_test.go's helper).
	ensureBoundPVCIT := func(name, ns string) {
		var existing corev1.PersistentVolumeClaim
		if err := k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: ns}, &existing); err == nil {
			return
		}
		pvc := &corev1.PersistentVolumeClaim{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns},
			Spec: corev1.PersistentVolumeClaimSpec{
				AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteMany},
				Resources: corev1.VolumeResourceRequirements{
					Requests: corev1.ResourceList{
						corev1.ResourceStorage: resource.MustParse("1Gi"),
					},
				},
			},
		}
		Expect(k8sClient.Create(ctx, pvc)).To(Succeed())
		pvcStatus := pvc.DeepCopy()
		pvcStatus.Status.Phase = corev1.ClaimBound
		Expect(k8sClient.Status().Update(ctx, pvcStatus)).To(Succeed())
	}

	driveProjectReconcile := func(r *controller.ProjectReconciler, name string, n int) {
		for range n {
			_, _ = r.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: name, Namespace: "default"},
			})
		}
	}

	cleanupLB := func(projectName string) {
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
		proj := &tideprojectv1alpha1.Project{}
		if err := k8sClient.Get(ctx, types.NamespacedName{Name: projectName, Namespace: "default"}, proj); err == nil {
			proj.Finalizers = nil
			_ = k8sClient.Update(ctx, proj)
			_ = k8sClient.Delete(ctx, proj)
		}
	}

	Describe("TestProject_LeakBlocked", func() {
		const projectName = "lb-it-proj-1"
		const pvcName = "tide-projects-lb-1"

		AfterEach(func() {
			cleanupLB(projectName)
		})

		It("exit-10 leak-detected: Status.Phase=PhasePushLeakBlocked + SecretLeakBlockedTotal counter increments", func() {
			tidemetrics.SecretLeakBlockedTotal.Reset()
			ensureBoundPVCIT(pvcName, "default")

			proj := &tideprojectv1alpha1.Project{
				ObjectMeta: metav1.ObjectMeta{Name: projectName, Namespace: "default"},
				Spec: tideprojectv1alpha1.ProjectSpec{
					TargetRepo: "https://github.com/example/test.git",
					Git: &tideprojectv1alpha1.GitConfig{
						RepoURL:        "https://github.com/example/test.git",
						CredsSecretRef: "test-creds",
					},
				},
			}
			Expect(k8sClient.Create(ctx, proj)).To(Succeed())
			waitITCacheSync(projectName, &tideprojectv1alpha1.Project{})

			var got tideprojectv1alpha1.Project
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: projectName, Namespace: "default"}, &got)).To(Succeed())
			// Drive Project into PhaseComplete with BranchName so the push-job dispatch path runs.
			statusPatch := client.MergeFrom(got.DeepCopy())
			got.Status.Phase = tideprojectv1alpha1.PhaseComplete
			got.Status.Git.BranchName = "tide/run-" + projectName + "-1747200000"
			Expect(k8sClient.Status().Patch(ctx, &got, statusPatch)).To(Succeed())

			pushJobName := fmt.Sprintf("tide-push-%s", got.UID)
			createPushJob(pushJobName, "default", got.Name, got.UID)
			createPushPodWithEnvelope(pushJobName, "default", pushResultMirror{
				APIVersion: "tideproject.k8s/v1alpha1",
				Kind:       "PushResult",
				ProjectUID: string(got.UID),
				ExitCode:   10,
				Reason:     "leak-detected",
			})
			markPushJobFailed(pushJobName, "default")

			r := &controller.ProjectReconciler{
				Client:                  k8sClient,
				Scheme:                  k8sClient.Scheme(),
				Dispatcher:              &stubDispatcher{},
				MaxConcurrentReconciles: 1,
				SharedPVCName:           pvcName,
				TidePushImage:           "ghcr.io/jsquirrelz/tide-push:test",
			}
			driveProjectReconcile(r, projectName, 5)

			Eventually(func(g Gomega) {
				var p tideprojectv1alpha1.Project
				g.Expect(mgrClient.Get(ctx, types.NamespacedName{Name: projectName, Namespace: "default"}, &p)).To(Succeed())
				g.Expect(p.Status.Phase).To(Equal(tideprojectv1alpha1.PhasePushLeakBlocked))
				c := meta.FindStatusCondition(p.Status.Conditions, tideprojectv1alpha1.ConditionPushLeakBlocked)
				g.Expect(c).NotTo(BeNil(), "ConditionPushLeakBlocked should be set on leak-detected")
				g.Expect(c.Status).To(Equal(metav1.ConditionTrue))
				g.Expect(c.Reason).To(Equal("LeakDetected"))
			}, 5*time.Second, 100*time.Millisecond).Should(Succeed())

			// metrics.SecretLeakBlockedTotal{projectName, "", ""} >= 1.0
			counterValue := testutil.ToFloat64(tidemetrics.SecretLeakBlockedTotal.WithLabelValues(projectName, "", ""))
			Expect(counterValue).To(BeNumerically(">=", 1.0),
				"SecretLeakBlockedTotal counter must increment on leak-blocked push")
		})
	})

	Describe("TestProject_LeaseFailed_NoLeakCounter", func() {
		const projectName = "lb-it-proj-2"
		const pvcName = "tide-projects-lb-2"

		AfterEach(func() {
			cleanupLB(projectName)
		})

		It("exit-11 lease-rejected: PhasePushLeaseFailed AND SecretLeakBlockedTotal UNCHANGED", func() {
			tidemetrics.SecretLeakBlockedTotal.Reset()
			ensureBoundPVCIT(pvcName, "default")

			proj := &tideprojectv1alpha1.Project{
				ObjectMeta: metav1.ObjectMeta{Name: projectName, Namespace: "default"},
				Spec: tideprojectv1alpha1.ProjectSpec{
					TargetRepo: "https://github.com/example/test.git",
					Git: &tideprojectv1alpha1.GitConfig{
						RepoURL:        "https://github.com/example/test.git",
						CredsSecretRef: "test-creds",
					},
				},
			}
			Expect(k8sClient.Create(ctx, proj)).To(Succeed())
			waitITCacheSync(projectName, &tideprojectv1alpha1.Project{})

			var got tideprojectv1alpha1.Project
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: projectName, Namespace: "default"}, &got)).To(Succeed())
			statusPatch := client.MergeFrom(got.DeepCopy())
			got.Status.Phase = tideprojectv1alpha1.PhaseComplete
			got.Status.Git.BranchName = "tide/run-" + projectName + "-1747200001"
			Expect(k8sClient.Status().Patch(ctx, &got, statusPatch)).To(Succeed())

			pushJobName := fmt.Sprintf("tide-push-%s", got.UID)
			createPushJob(pushJobName, "default", got.Name, got.UID)
			createPushPodWithEnvelope(pushJobName, "default", pushResultMirror{
				APIVersion: "tideproject.k8s/v1alpha1",
				Kind:       "PushResult",
				ProjectUID: string(got.UID),
				ExitCode:   11,
				Reason:     "lease-rejected",
			})
			markPushJobFailed(pushJobName, "default")

			r := &controller.ProjectReconciler{
				Client:                  k8sClient,
				Scheme:                  k8sClient.Scheme(),
				Dispatcher:              &stubDispatcher{},
				MaxConcurrentReconciles: 1,
				SharedPVCName:           pvcName,
				TidePushImage:           "ghcr.io/jsquirrelz/tide-push:test",
			}
			driveProjectReconcile(r, projectName, 5)

			Eventually(func(g Gomega) {
				var p tideprojectv1alpha1.Project
				g.Expect(mgrClient.Get(ctx, types.NamespacedName{Name: projectName, Namespace: "default"}, &p)).To(Succeed())
				g.Expect(p.Status.Phase).To(Equal(tideprojectv1alpha1.PhasePushLeaseFailed))
				c := meta.FindStatusCondition(p.Status.Conditions, tideprojectv1alpha1.ConditionPushLeaseFailed)
				g.Expect(c).NotTo(BeNil())
				g.Expect(c.Status).To(Equal(metav1.ConditionTrue))
			}, 5*time.Second, 100*time.Millisecond).Should(Succeed())

			counterValue := testutil.ToFloat64(tidemetrics.SecretLeakBlockedTotal.WithLabelValues(projectName, "", ""))
			Expect(counterValue).To(BeNumerically("==", 0.0),
				"SecretLeakBlockedTotal counter must NOT increment on lease-rejected (T-04-W1-bypass guard)")
		})
	})
})
