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
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	tideprojectv1alpha2 "github.com/jsquirrelz/tide/api/v1alpha2"
	pkggit "github.com/jsquirrelz/tide/pkg/git"
)

// Phase 34 plan 34-02 Task 2: git-writer shared helpers.
var _ = Describe("git-writer shared helpers (Phase 34 D-02/D-03/D-07)", Label("envtest", "phase34", "gitwriter"), func() {
	ctx := context.Background()

	makeTask := func(name, projectName, phase string) *tideprojectv1alpha2.Task {
		tsk := &tideprojectv1alpha2.Task{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: "default",
				Labels:    map[string]string{gitWriterProjectLabelKey: projectName},
			},
			Spec: tideprojectv1alpha2.TaskSpec{
				PlanRef:             "some-plan",
				FilesTouched:        []string{"file.go"},
				PromptPath:          "envelopes/x/children/task-01.json",
				DeclaredOutputPaths: []string{"file.go"},
			},
		}
		Expect(k8sClient.Create(ctx, tsk)).To(Succeed())
		waitForCacheSync(name, "default", &tideprojectv1alpha2.Task{})
		if phase != "" {
			var got tideprojectv1alpha2.Task
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: "default"}, &got)).To(Succeed())
			patch := client.MergeFrom(got.DeepCopy())
			got.Status.Phase = phase
			Expect(k8sClient.Status().Patch(ctx, &got, patch)).To(Succeed())
			Eventually(func() string {
				var g tideprojectv1alpha2.Task
				if err := k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: "default"}, &g); err != nil {
					return ""
				}
				return g.Status.Phase
			}, 5*time.Second, 50*time.Millisecond).Should(Equal(phase))
			return &got
		}
		return tsk
	}

	cleanupTasks := func(names ...string) {
		for _, n := range names {
			tsk := &tideprojectv1alpha2.Task{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: n, Namespace: "default"}, tsk); err == nil {
				tsk.Finalizers = nil
				_ = k8sClient.Update(ctx, tsk)
				_ = k8sClient.Delete(ctx, tsk)
			}
		}
	}

	Describe("succeededTaskBranches", func() {
		const projectName = "gw-proj-1"
		AfterEach(func() {
			cleanupTasks("gw-t1", "gw-t2", "gw-t3")
		})

		It("returns only Succeeded task branches, sorted, ignoring other projects and phases", func() {
			t1 := makeTask("gw-t1", projectName, "Succeeded")
			_ = makeTask("gw-t2", projectName, "Running")
			t3 := makeTask("gw-t3", projectName, "Succeeded")
			// Different project — must not leak in.
			makeTask("gw-t4-other-project", "some-other-project", "Succeeded")
			defer cleanupTasks("gw-t4-other-project")

			branches, err := succeededTaskBranches(ctx, k8sClient, "default", projectName)
			Expect(err).NotTo(HaveOccurred())

			want := []string{pkggit.TaskBranchName(string(t1.UID)), pkggit.TaskBranchName(string(t3.UID))}
			// sort.Strings applied inside the helper — assert deterministic order.
			if want[0] > want[1] {
				want[0], want[1] = want[1], want[0]
			}
			Expect(branches).To(Equal(want))
		})

		It("returns an empty slice when no Task has Succeeded", func() {
			makeTask("gw-t1", projectName, "Running")
			branches, err := succeededTaskBranches(ctx, k8sClient, "default", projectName)
			Expect(err).NotTo(HaveOccurred())
			Expect(branches).To(BeEmpty())
		})
	})

	Describe("gitWriterInFlightCount", func() {
		const projectName = "gw-proj-2"

		makeGitWriterJob := func(name string, terminal bool) {
			job := &batchv1.Job{
				ObjectMeta: metav1.ObjectMeta{
					Name:      name,
					Namespace: "default",
					Labels: map[string]string{
						gitWriterRoleLabelKey:    gitWriterRoleLabelValue,
						gitWriterProjectLabelKey: projectName,
					},
				},
				Spec: batchv1.JobSpec{
					Template: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							RestartPolicy: corev1.RestartPolicyNever,
							Containers:    []corev1.Container{{Name: "push", Image: "ghcr.io/jsquirrelz/tide-push:test"}},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, job)).To(Succeed())
			if terminal {
				Expect(makeFakeJobTerminal(ctx, k8sClient, name, "default", true)).To(Succeed())
			}
		}

		cleanupJobs := func(names ...string) {
			for _, n := range names {
				j := &batchv1.Job{}
				if err := k8sClient.Get(ctx, types.NamespacedName{Name: n, Namespace: "default"}, j); err == nil {
					_ = k8sClient.Delete(ctx, j)
				}
			}
		}

		It("counts non-terminal git-writer Jobs for the project, excluding the named Job and terminal Jobs", func() {
			makeGitWriterJob("gw-job-running", false)
			makeGitWriterJob("gw-job-done", true)
			defer cleanupJobs("gw-job-running", "gw-job-done")

			n, err := gitWriterInFlightCount(ctx, k8sClient, "default", projectName, "")
			Expect(err).NotTo(HaveOccurred())
			Expect(n).To(Equal(1), "only the non-terminal Job should count")

			// Self-exclusion (Pitfall 7): excluding the running Job's own name
			// must not count it as "another" writer.
			nExcluded, err := gitWriterInFlightCount(ctx, k8sClient, "default", projectName, "gw-job-running")
			Expect(err).NotTo(HaveOccurred())
			Expect(nExcluded).To(Equal(0))
		})
	})

	Describe("readJobPushEnvelope", func() {
		const jobName = "gw-envelope-job"

		fakeEnvelopePod := func(name string, envelope pushResultEnvelope) {
			raw, err := json.Marshal(envelope)
			Expect(err).NotTo(HaveOccurred())
			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      name,
					Namespace: "default",
					Labels:    map[string]string{"job-name": jobName},
				},
				Spec: corev1.PodSpec{
					RestartPolicy: corev1.RestartPolicyNever,
					Containers:    []corev1.Container{{Name: "push", Image: "ghcr.io/jsquirrelz/tide-push:test"}},
				},
			}
			Expect(k8sClient.Create(ctx, pod)).To(Succeed())
			statusPatch := client.MergeFrom(pod.DeepCopy())
			pod.Status.ContainerStatuses = []corev1.ContainerStatus{
				{
					Name: "push",
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

		AfterEach(func() {
			pod := &corev1.Pod{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: "gw-envelope-pod", Namespace: "default"}, pod); err == nil {
				_ = k8sClient.Delete(ctx, pod)
			}
		})

		It("parses the terminated container's termination message including MissingBranches/ConflictBranch", func() {
			env := pushResultEnvelope{
				APIVersion:      "v1",
				Kind:            "PushResult",
				ProjectUID:      "proj-uid",
				Branch:          "tide/run-x-1",
				HeadSHA:         "abc123",
				ExitCode:        14,
				Reason:          "integration-incomplete",
				MissingBranches: []string{"tide/wt-a", "tide/wt-b"},
				MissingTotal:    2,
				ConflictBranch:  "",
			}
			fakeEnvelopePod("gw-envelope-pod", env)

			got, ok := readJobPushEnvelope(ctx, k8sClient, "default", jobName)
			Expect(ok).To(BeTrue())
			Expect(got.Reason).To(Equal("integration-incomplete"))
			Expect(got.MissingBranches).To(Equal([]string{"tide/wt-a", "tide/wt-b"}))
			Expect(got.MissingTotal).To(Equal(2))
		})

		It("returns (zero, false) when no pod exists for the Job", func() {
			_, ok := readJobPushEnvelope(ctx, k8sClient, "default", "gw-no-such-job")
			Expect(ok).To(BeFalse())
		})
	})

	Describe("buildPushJob labels", func() {
		It("stamps both git-writer labels so the D-02 List gate can find it", func() {
			project := &tideprojectv1alpha2.Project{
				ObjectMeta: metav1.ObjectMeta{Name: "gw-label-proj", Namespace: "default", UID: types.UID("uid-label-proj")},
				Spec: tideprojectv1alpha2.ProjectSpec{
					SchemaRevision: "v1alpha2",
					TargetRepo:     "https://example.com/x.git",
					Git:            &tideprojectv1alpha2.GitConfig{RepoURL: "https://example.com/x.git", CredsSecretRef: "c"},
				},
			}
			job := buildPushJob(project, "tide-projects", PushOptions{
				TidePushImage: "ghcr.io/jsquirrelz/tide-push:test",
				Branch:        "tide/run-x-1",
				CommitMessage: "tide: project complete",
			}, k8sClient.Scheme())
			Expect(job.Labels).To(HaveKeyWithValue(gitWriterRoleLabelKey, gitWriterRoleLabelValue))
			Expect(job.Labels).To(HaveKeyWithValue(gitWriterProjectLabelKey, project.Name))
		})
	})
})
