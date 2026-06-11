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
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	batchv1 "k8s.io/api/batch/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	tideprojectv1alpha1 "github.com/jsquirrelz/tide/api/v1alpha1"
	"github.com/jsquirrelz/tide/internal/dispatch/podjob"
)

// dispatch_image_test.go — envtest regression for DISPATCH-01.
//
// Asserts that CRD image fields (Spec.Subagent.Levels.<level>.Image and
// Spec.Subagent.Image) land in the created Job's subagent container image,
// NOT the r.SubagentImage / r.Deps.SubagentImage reconciler field.
// This is the structural negation of the v1.0 stub-image bug (run-1
// signature: planner pod completing in seconds with "planner stub success").

var _ = Describe("Dispatch image resolution (DISPATCH-01)", Label("envtest", "dispatch-image"), func() {
	const milestoneRef = "dispatch-image-ms"
	const phaseRef = "dispatch-image-ph"
	ctx := context.Background()

	// makeFullHierarchy creates Project → Milestone → Phase → Plan.
	makeFullHierarchy := func(projectName, planName string) {
		makeProjectForTask(projectName)
		ms := &tideprojectv1alpha1.Milestone{
			ObjectMeta: metav1.ObjectMeta{Name: milestoneRef, Namespace: "default"},
			Spec:       tideprojectv1alpha1.MilestoneSpec{ProjectRef: projectName},
		}
		Expect(k8sClient.Create(ctx, ms)).To(Succeed())
		waitForCacheSync(milestoneRef, "default", &tideprojectv1alpha1.Milestone{})
		ph := &tideprojectv1alpha1.Phase{
			ObjectMeta: metav1.ObjectMeta{Name: phaseRef, Namespace: "default"},
			Spec:       tideprojectv1alpha1.PhaseSpec{MilestoneRef: milestoneRef},
		}
		Expect(k8sClient.Create(ctx, ph)).To(Succeed())
		waitForCacheSync(phaseRef, "default", &tideprojectv1alpha1.Phase{})
		p := &tideprojectv1alpha1.Plan{
			ObjectMeta: metav1.ObjectMeta{Name: planName, Namespace: "default"},
			Spec:       tideprojectv1alpha1.PlanSpec{PhaseRef: phaseRef},
		}
		Expect(k8sClient.Create(ctx, p)).To(Succeed())
		Eventually(func() error {
			return mgrClient.Get(ctx, types.NamespacedName{Name: planName, Namespace: "default"}, &tideprojectv1alpha1.Plan{})
		}, 5*time.Second, 50*time.Millisecond).Should(Succeed())
	}

	cleanupFullHierarchy := func(projectName, planName string) {
		plan := &tideprojectv1alpha1.Plan{}
		if err := k8sClient.Get(ctx, types.NamespacedName{Name: planName, Namespace: "default"}, plan); err == nil {
			plan.Finalizers = nil
			_ = k8sClient.Update(ctx, plan)
			_ = k8sClient.Delete(ctx, plan)
		}
		ph := &tideprojectv1alpha1.Phase{}
		if err := k8sClient.Get(ctx, types.NamespacedName{Name: phaseRef, Namespace: "default"}, ph); err == nil {
			ph.Finalizers = nil
			_ = k8sClient.Update(ctx, ph)
			_ = k8sClient.Delete(ctx, ph)
		}
		ms := &tideprojectv1alpha1.Milestone{}
		if err := k8sClient.Get(ctx, types.NamespacedName{Name: milestoneRef, Namespace: "default"}, ms); err == nil {
			ms.Finalizers = nil
			_ = k8sClient.Update(ctx, ms)
			_ = k8sClient.Delete(ctx, ms)
		}
		cleanupProject(projectName)
		var jobs batchv1.JobList
		_ = k8sClient.List(ctx, &jobs, client.InNamespace("default"))
		for i := range jobs.Items {
			j := jobs.Items[i]
			_ = k8sClient.Delete(ctx, &j)
		}
	}

	// jobHasContainerWithImage checks whether any container (init or regular)
	// in the Job pod template carries the given image ref.
	jobHasContainerWithImage := func(job *batchv1.Job, image string) bool {
		for _, c := range job.Spec.Template.Spec.Containers {
			if c.Image == image {
				return true
			}
		}
		for _, c := range job.Spec.Template.Spec.InitContainers {
			if c.Image == image {
				return true
			}
		}
		return false
	}

	// --- Spec 1: plan-level Levels.Plan.Image overrides helmDefault ---

	Describe("plan-level image pin (Levels.Plan.Image)", func() {
		const projectName = "dispatch-img-project-plan"
		const planName = "dispatch-img-plan"

		BeforeEach(func() {
			makeFullHierarchy(projectName, planName)
		})

		AfterEach(func() {
			cleanupFullHierarchy(projectName, planName)
		})

		It("dispatches the pinned level image, not the helm default", func() {
			// Patch the project to set Levels.Plan.Image.
			proj := &tideprojectv1alpha1.Project{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: projectName, Namespace: "default"}, proj)).To(Succeed())
			patch := client.MergeFrom(proj.DeepCopy())
			proj.Spec.Subagent.Levels.Plan = &tideprojectv1alpha1.LevelConfig{
				Image: "ghcr.io/example/pinned-planner:test",
			}
			Expect(k8sClient.Patch(ctx, proj, patch)).To(Succeed())
			waitForCacheSync(projectName, "default", &tideprojectv1alpha1.Project{})

			const helmDefaultImage = "tide-stub-subagent:test"
			r := &PlanReconciler{
				Client:      mgrClient,
				Scheme:      k8sClient.Scheme(),
				Dispatcher:  &stubDispatcher{},
				PlannerPool: newPlannerPoolForTest(),
				EnvReader:   newMapEnvReader(),
				// SubagentImage intentionally set to helm default — reconciler field must NOT win.
				SubagentImage:  helmDefaultImage,
				CredproxyImage: testCredproxyImage,
				SigningKey:     testSigningKey,
				HelmProviderDefaults: ProviderDefaults{
					Image: helmDefaultImage,
				},
			}

			Expect(reconcileWithRetry(r.Reconcile, types.NamespacedName{Name: planName, Namespace: "default"}, 5)).To(Succeed())

			Eventually(func(g Gomega) {
				var got tideprojectv1alpha1.Plan
				g.Expect(mgrClient.Get(ctx, types.NamespacedName{Name: planName, Namespace: "default"}, &got)).To(Succeed())
				expectedJobName := fmt.Sprintf("tide-plan-%s-1", got.UID)
				var job batchv1.Job
				g.Expect(mgrClient.Get(ctx, types.NamespacedName{Name: expectedJobName, Namespace: "default"}, &job)).To(Succeed())
				// Assert the subagent container carries the pinned level image.
				g.Expect(jobHasContainerWithImage(&job, "ghcr.io/example/pinned-planner:test")).To(BeTrue(),
					"Job subagent container image must equal the pinned level image %q, not the helm default %q",
					"ghcr.io/example/pinned-planner:test", helmDefaultImage)
			}, 5*time.Second, 100*time.Millisecond).Should(Succeed())
		})
	})

	// --- Spec 2: task-level Spec.Subagent.Image overrides Deps.SubagentImage ---

	Describe("project-wide image pin (Spec.Subagent.Image) at task dispatch", func() {
		const projectName = "dispatch-img-proj-task"
		const taskName = "dispatch-img-task"
		const planRefName = "dispatch-img-task-plan"

		BeforeEach(func() {
			// Create project with Spec.Subagent.Image pinned.
			p := &tideprojectv1alpha1.Project{
				ObjectMeta: metav1.ObjectMeta{Name: projectName, Namespace: "default"},
				Spec: tideprojectv1alpha1.ProjectSpec{
					TargetRepo: "https://github.com/example/tide.git",
					Subagent: tideprojectv1alpha1.SubagentConfig{
						Image: "ghcr.io/example/pinned-everywhere:test",
					},
				},
			}
			Expect(k8sClient.Create(ctx, p)).To(Succeed())
			waitForCacheSync(projectName, "default", &tideprojectv1alpha1.Project{})

			makePlan(planRefName, "nonexistent-phase", "Validated")
		})

		AfterEach(func() {
			task := &tideprojectv1alpha1.Task{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: taskName, Namespace: "default"}, task); err == nil {
				_ = k8sClient.Delete(ctx, task)
			}
			plan := &tideprojectv1alpha1.Plan{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: planRefName, Namespace: "default"}, plan); err == nil {
				plan.Finalizers = nil
				_ = k8sClient.Update(ctx, plan)
				_ = k8sClient.Delete(ctx, plan)
			}
			cleanupProject(projectName)
			var jobs batchv1.JobList
			_ = k8sClient.List(ctx, &jobs, client.InNamespace("default"))
			for i := range jobs.Items {
				j := jobs.Items[i]
				_ = k8sClient.Delete(ctx, &j)
			}
		})

		It("task Job uses Spec.Subagent.Image, not Deps.SubagentImage (v1.0 bug negation)", func() {
			// Create a task with the project label so the reconciler can resolve it.
			t := &tideprojectv1alpha1.Task{
				ObjectMeta: metav1.ObjectMeta{
					Name:      taskName,
					Namespace: "default",
					Labels:    map[string]string{"tideproject.k8s/project": projectName},
				},
				Spec: tideprojectv1alpha1.TaskSpec{
					PlanRef:             planRefName,
					FilesTouched:        []string{"src/main.go"},
					DeclaredOutputPaths: []string{"artifacts/out.txt"},
					PromptPath:          "envelopes/test/children/" + taskName + ".json",
				},
			}
			Expect(k8sClient.Create(ctx, t)).To(Succeed())
			waitForCacheSync(taskName, "default", &tideprojectv1alpha1.Task{})

			const depsSubagentImage = "tide-stub-subagent:test" // the v1.0 wrong value
			r := &TaskReconciler{
				Client: mgrClient,
				Scheme: k8sClient.Scheme(),
				Deps: TaskReconcilerDeps{
					Dispatcher:     &stubDispatcher{},
					Budget:         testBudgetStore,
					Defaults:       testBudgetDefaults,
					SigningKey:     testSigningKey,
					CredproxyImage: testCredproxyImage,
					EnvReader:      newMapEnvReader(),
					// SubagentImage intentionally set to stub — must NOT win when CRD field is set.
					SubagentImage: depsSubagentImage,
					HelmProviderDefaults: ProviderDefaults{
						Image: depsSubagentImage,
					},
				},
			}

			_, _ = reconcileN(r, types.NamespacedName{Name: taskName, Namespace: "default"}, 4)

			Eventually(func(g Gomega) {
				var got tideprojectv1alpha1.Task
				g.Expect(mgrClient.Get(ctx, types.NamespacedName{Name: taskName, Namespace: "default"}, &got)).To(Succeed())
				jobName := podjob.JobName(got.UID, got.Status.Attempt)
				var job batchv1.Job
				g.Expect(mgrClient.Get(ctx, types.NamespacedName{Name: jobName, Namespace: "default"}, &job)).To(Succeed())
				// Assert the pinned project-level image is used, not depsSubagentImage.
				g.Expect(jobHasContainerWithImage(&job, "ghcr.io/example/pinned-everywhere:test")).To(BeTrue(),
					"Task Job subagent container image must equal Spec.Subagent.Image %q, not Deps.SubagentImage %q (v1.0 stub-image bug negation)",
					"ghcr.io/example/pinned-everywhere:test", depsSubagentImage)
			}, 5*time.Second, 100*time.Millisecond).Should(Succeed())
		})
	})
})
