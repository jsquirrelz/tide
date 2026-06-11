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

// Plan 13-04 Task 1 (RED) + Task 2 (RED+GREEN) — BillingHalt dispatch-entry
// hold regression tests.
//
// HALT-01 / D-05: the billing halt is the third dispatch-entry hold after
// CheckRejected + checkParentApproval. Park semantics at every level:
//   - requeue 30s (operator-paced recovery, not automatic)
//   - Status.Phase unchanged (never "Failed" — wave-boundary failure semantics
//     preserved per spec §"Failure handling at wave boundaries")
//   - no per-level condition written (avoids status flapping per dogfood run 1)
//
// Run-1 regression (CONTEXT specifics verbatim):
//   billing-classified failure → BillingHalt=True on Project → sibling holds →
//   condition cleared (tide resume semantics) → dispatch resumes.
package controller

import (
	"context"
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

	tideprojectv1alpha1 "github.com/jsquirrelz/tide/api/v1alpha1"
	pkgdispatch "github.com/jsquirrelz/tide/pkg/dispatch"
)

// stampBillingHalt stamps BillingHalt=True on a Project and waits for the
// cache to reflect it. Shared across all per-level hold specs.
func stampBillingHalt(ctx context.Context, projectName string) {
	var proj tideprojectv1alpha1.Project
	Expect(k8sClient.Get(ctx, types.NamespacedName{Name: projectName, Namespace: "default"}, &proj)).To(Succeed())
	sp := client.MergeFrom(proj.DeepCopy())
	meta.SetStatusCondition(&proj.Status.Conditions, metav1.Condition{
		Type:               tideprojectv1alpha1.ConditionBillingHalt,
		Status:             metav1.ConditionTrue,
		Reason:             tideprojectv1alpha1.ReasonCreditBalanceTooLow,
		Message:            "Test: billing halt stamped",
		LastTransitionTime: metav1.Now(),
	})
	Expect(k8sClient.Status().Patch(ctx, &proj, sp)).To(Succeed())
	Eventually(func() bool {
		var p tideprojectv1alpha1.Project
		if err := mgrClient.Get(ctx, types.NamespacedName{Name: projectName, Namespace: "default"}, &p); err != nil {
			return false
		}
		c := meta.FindStatusCondition(p.Status.Conditions, tideprojectv1alpha1.ConditionBillingHalt)
		return c != nil && c.Status == metav1.ConditionTrue
	}, 5*time.Second, 50*time.Millisecond).Should(BeTrue(), "BillingHalt must be visible in cache")
}

// clearBillingHalt removes the BillingHalt condition from a Project (mirrors
// what `tide resume` does via meta.RemoveStatusCondition — D-06).
func clearBillingHalt(ctx context.Context, projectName string) {
	var proj tideprojectv1alpha1.Project
	Expect(mgrClient.Get(ctx, types.NamespacedName{Name: projectName, Namespace: "default"}, &proj)).To(Succeed())
	cp := client.MergeFrom(proj.DeepCopy())
	meta.RemoveStatusCondition(&proj.Status.Conditions, tideprojectv1alpha1.ConditionBillingHalt)
	Expect(k8sClient.Status().Patch(ctx, &proj, cp)).To(Succeed())
	Eventually(func() bool {
		var p tideprojectv1alpha1.Project
		if err := mgrClient.Get(ctx, types.NamespacedName{Name: projectName, Namespace: "default"}, &p); err != nil {
			return false
		}
		c := meta.FindStatusCondition(p.Status.Conditions, tideprojectv1alpha1.ConditionBillingHalt)
		return c == nil
	}, 5*time.Second, 50*time.Millisecond).Should(BeTrue(), "BillingHalt must be removed from cache")
}

// makeProjectForHalt creates a minimal Project with the given name for BillingHalt
// hold specs. Waits for cache sync before returning.
func makeProjectForHalt(ctx context.Context, name string) {
	p := &tideprojectv1alpha1.Project{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
		Spec: tideprojectv1alpha1.ProjectSpec{
			TargetRepo: "https://github.com/example/tide.git",
		},
	}
	Expect(k8sClient.Create(ctx, p)).To(Succeed())
	waitForCacheSync(name, "default", &tideprojectv1alpha1.Project{})
}

// ---- Task-level holds are covered in task_gates_test.go (Tests 13a+13b). ----

// Phase 13 HALT-01: per-level planner dispatch-entry holds.
//
// For each planner level (milestone, phase, plan), a ready-to-dispatch object
// whose Project carries BillingHalt=True must: (a) not create a planner Job,
// (b) not change Status.Phase to "Failed", (c) requeue.
var _ = Describe("BillingHalt planner dispatch-entry holds (Phase 13 HALT-01)", Label("envtest", "phase13", "billing-halt"), func() {
	ctx := context.Background()

	// Helper: assert no Job labelled with parentUID was created.
	assertNoJobForParent := func(parentUID types.UID) {
		var jobs batchv1.JobList
		Expect(k8sClient.List(ctx, &jobs, client.InNamespace("default"))).To(Succeed())
		for _, j := range jobs.Items {
			Expect(j.Labels["tideproject.k8s/parent-uid"]).NotTo(Equal(string(parentUID)),
				"BillingHalt hold must prevent planner Job dispatch")
		}
	}

	// --- Milestone ---
	Describe("Milestone level: BillingHalt=True → no planner Job, Status.Phase unchanged", func() {
		const projName = "bh-ms-proj-1"
		const msName = "bh-ms-1"

		BeforeEach(func() {
			makeProjectForHalt(ctx, projName)
			stampBillingHalt(ctx, projName)

			ms := &tideprojectv1alpha1.Milestone{
				ObjectMeta: metav1.ObjectMeta{Name: msName, Namespace: "default"},
				Spec: tideprojectv1alpha1.MilestoneSpec{
					ProjectRef: projName,
				},
			}
			Expect(k8sClient.Create(ctx, ms)).To(Succeed())
			waitForCacheSync(msName, "default", &tideprojectv1alpha1.Milestone{})
		})
		AfterEach(func() {
			var ms tideprojectv1alpha1.Milestone
			_ = k8sClient.Get(ctx, types.NamespacedName{Name: msName, Namespace: "default"}, &ms)
			_ = k8sClient.Delete(ctx, &ms)
			cleanupProject(projName)
		})

		It("no planner Job created; Status.Phase unchanged while BillingHalt=True", func() {
			r := newBHMilestoneReconciler()
			name := types.NamespacedName{Name: msName, Namespace: "default"}
			for range 3 {
				_, _ = r.Reconcile(ctx, reconcile.Request{NamespacedName: name})
			}

			var ms tideprojectv1alpha1.Milestone
			Expect(mgrClient.Get(ctx, name, &ms)).To(Succeed())
			Expect(ms.Status.Phase).NotTo(Equal("Failed"),
				"T-13-15: BillingHalt must park the Milestone, not fail it")

			assertNoJobForParent(ms.UID)
		})
	})

	// --- Phase ---
	Describe("Phase level: BillingHalt=True → no planner Job, Status.Phase unchanged", func() {
		const projName = "bh-ph-proj-1"
		const phName = "bh-ph-1"
		const msName = "bh-ph-ms-1"

		BeforeEach(func() {
			makeProjectForHalt(ctx, projName)
			stampBillingHalt(ctx, projName)

			ms := &tideprojectv1alpha1.Milestone{
				ObjectMeta: metav1.ObjectMeta{
					Name:      msName,
					Namespace: "default",
				},
				Spec: tideprojectv1alpha1.MilestoneSpec{
					ProjectRef: projName,
				},
			}
			Expect(k8sClient.Create(ctx, ms)).To(Succeed())
			waitForCacheSync(msName, "default", &tideprojectv1alpha1.Milestone{})

			ph := &tideprojectv1alpha1.Phase{
				ObjectMeta: metav1.ObjectMeta{Name: phName, Namespace: "default",
					Labels: map[string]string{"tideproject.k8s/project": projName}},
				Spec: tideprojectv1alpha1.PhaseSpec{
					MilestoneRef: msName,
				},
			}
			Expect(k8sClient.Create(ctx, ph)).To(Succeed())
			waitForCacheSync(phName, "default", &tideprojectv1alpha1.Phase{})
		})
		AfterEach(func() {
			var ph tideprojectv1alpha1.Phase
			_ = k8sClient.Get(ctx, types.NamespacedName{Name: phName, Namespace: "default"}, &ph)
			_ = k8sClient.Delete(ctx, &ph)
			var ms tideprojectv1alpha1.Milestone
			_ = k8sClient.Get(ctx, types.NamespacedName{Name: msName, Namespace: "default"}, &ms)
			_ = k8sClient.Delete(ctx, &ms)
			cleanupProject(projName)
		})

		It("no planner Job created; Status.Phase unchanged while BillingHalt=True", func() {
			r := newBHPhaseReconciler()
			name := types.NamespacedName{Name: phName, Namespace: "default"}
			for range 3 {
				_, _ = r.Reconcile(ctx, reconcile.Request{NamespacedName: name})
			}

			var ph tideprojectv1alpha1.Phase
			Expect(mgrClient.Get(ctx, name, &ph)).To(Succeed())
			Expect(ph.Status.Phase).NotTo(Equal("Failed"),
				"T-13-15: BillingHalt must park the Phase, not fail it")

			assertNoJobForParent(ph.UID)
		})
	})

	// --- Plan ---
	Describe("Plan level: BillingHalt=True → no planner Job, Status.Phase unchanged", func() {
		const projName = "bh-plan-proj-1"
		const planName = "bh-plan-1"
		const msName = "bh-plan-ms-1"
		const phName = "bh-plan-ph-1"

		BeforeEach(func() {
			makeProjectForHalt(ctx, projName)
			stampBillingHalt(ctx, projName)

			ms := &tideprojectv1alpha1.Milestone{
				ObjectMeta: metav1.ObjectMeta{Name: msName, Namespace: "default"},
				Spec: tideprojectv1alpha1.MilestoneSpec{ProjectRef: projName},
			}
			Expect(k8sClient.Create(ctx, ms)).To(Succeed())
			waitForCacheSync(msName, "default", &tideprojectv1alpha1.Milestone{})

			ph := &tideprojectv1alpha1.Phase{
				ObjectMeta: metav1.ObjectMeta{Name: phName, Namespace: "default",
					Labels: map[string]string{"tideproject.k8s/project": projName}},
				Spec: tideprojectv1alpha1.PhaseSpec{MilestoneRef: msName},
			}
			Expect(k8sClient.Create(ctx, ph)).To(Succeed())
			waitForCacheSync(phName, "default", &tideprojectv1alpha1.Phase{})

			plan := &tideprojectv1alpha1.Plan{
				ObjectMeta: metav1.ObjectMeta{Name: planName, Namespace: "default",
					Labels: map[string]string{"tideproject.k8s/project": projName}},
				Spec: tideprojectv1alpha1.PlanSpec{
					PhaseRef: phName,
				},
			}
			Expect(k8sClient.Create(ctx, plan)).To(Succeed())
			waitForCacheSync(planName, "default", &tideprojectv1alpha1.Plan{})
		})
		AfterEach(func() {
			var plan tideprojectv1alpha1.Plan
			_ = k8sClient.Get(ctx, types.NamespacedName{Name: planName, Namespace: "default"}, &plan)
			_ = k8sClient.Delete(ctx, &plan)
			var ph tideprojectv1alpha1.Phase
			_ = k8sClient.Get(ctx, types.NamespacedName{Name: phName, Namespace: "default"}, &ph)
			_ = k8sClient.Delete(ctx, &ph)
			var ms tideprojectv1alpha1.Milestone
			_ = k8sClient.Get(ctx, types.NamespacedName{Name: msName, Namespace: "default"}, &ms)
			_ = k8sClient.Delete(ctx, &ms)
			cleanupProject(projName)
		})

		It("no planner Job created; Status.Phase unchanged while BillingHalt=True", func() {
			r := newBHPlanReconciler()
			name := types.NamespacedName{Name: planName, Namespace: "default"}
			for range 3 {
				_, _ = r.Reconcile(ctx, reconcile.Request{NamespacedName: name})
			}

			var plan tideprojectv1alpha1.Plan
			Expect(mgrClient.Get(ctx, name, &plan)).To(Succeed())
			Expect(plan.Status.Phase).NotTo(Equal("Failed"),
				"T-13-15: BillingHalt must park the Plan, not fail it")

			assertNoJobForParent(plan.UID)
		})
	})

	// --- Project ---
	Describe("Project level: BillingHalt=True → no planner Job created", func() {
		const projName = "bh-project-1"

		BeforeEach(func() {
			makeProjectForHalt(ctx, projName)
			stampBillingHalt(ctx, projName)
		})
		AfterEach(func() {
			cleanupProject(projName)
		})

		It("no planner Job created while BillingHalt=True on Project itself", func() {
			r := newBHProjectReconciler()
			name := types.NamespacedName{Name: projName, Namespace: "default"}
			for range 3 {
				_, _ = r.Reconcile(ctx, reconcile.Request{NamespacedName: name})
			}

			var proj tideprojectv1alpha1.Project
			Expect(mgrClient.Get(ctx, name, &proj)).To(Succeed())
			Expect(proj.Status.Phase).NotTo(Equal("Failed"),
				"T-13-15: BillingHalt must not fail the Project")

			assertNoJobForParent(proj.UID)
		})
	})
})

// billingReason is the exact stderr excerpt from the run-1 incident (13-CONTEXT).
const billingReason = "claude exit 1: API Error: 400 Your credit balance is too low to access the Anthropic API."

// pumpTaskToRunning drives a task reconciler until task A reaches Running.
func pumpTaskToRunning(r *TaskReconciler, projName, taskName string) {
	name := types.NamespacedName{Name: taskName, Namespace: "default"}
	Eventually(func(g Gomega) {
		_, _ = reconcileN(r, name, 2)
		var t tideprojectv1alpha1.Task
		g.Expect(mgrClient.Get(context.Background(), name, &t)).To(Succeed())
		g.Expect(t.Status.Phase).To(Equal("Running"))
	}, 10*time.Second, 200*time.Millisecond).Should(Succeed(),
		fmt.Sprintf("task %s must reach Running", taskName))
}

// cleanupHaltTestJobs deletes all Jobs in default namespace that belong to the
// given project (by label). Used by AfterEach blocks in BillingHalt specs.
func cleanupHaltTestJobs(projName string) {
	var jobs batchv1.JobList
	_ = k8sClient.List(context.Background(), &jobs, client.InNamespace("default"))
	for i := range jobs.Items {
		j := jobs.Items[i]
		if j.Labels["tideproject.k8s/project"] == projName {
			_ = k8sClient.Delete(context.Background(), &j)
		}
	}
}

// markTaskJobFailed simulates a terminal Job failure for a Running task. It:
// 1. Waits until the Job created for taskUID (label tideproject.k8s/task-uid=<taskUID>) appears
// 2. Patches its status to JobFailed so isJobTerminal returns true
// This allows checkRunningState → handleJobCompletion to fire on next reconcile.
func markTaskJobFailed(taskUID types.UID) {
	var jobName string
	var jobNS string

	// Wait until the Job appears in the API (it may not be indexed yet immediately
	// after pumpTaskToRunning returns, due to informer lag).
	Eventually(func() bool {
		var jobs batchv1.JobList
		if err := k8sClient.List(context.Background(), &jobs, client.InNamespace("default")); err != nil {
			return false
		}
		for _, j := range jobs.Items {
			if j.Labels["tideproject.k8s/task-uid"] == string(taskUID) {
				jobName = j.Name
				jobNS = j.Namespace
				return true
			}
		}
		return false
	}, 5*time.Second, 50*time.Millisecond).Should(BeTrue(),
		fmt.Sprintf("job for task-uid %s must exist", taskUID))

	// Re-fetch the Job so we have a fresh ResourceVersion for the patch.
	var j batchv1.Job
	Expect(k8sClient.Get(context.Background(),
		types.NamespacedName{Name: jobName, Namespace: jobNS}, &j)).To(Succeed())

	jp := client.MergeFrom(j.DeepCopy())
	now := metav1.Now()
	// Kubernetes 1.33 requires startTime + FailureTarget=True before Failed=True.
	j.Status.StartTime = &now
	j.Status.Conditions = []batchv1.JobCondition{
		{
			Type:               batchv1.JobFailureTarget,
			Status:             corev1.ConditionTrue,
			Reason:             "PodFailed",
			LastTransitionTime: now,
		},
		{
			Type:               batchv1.JobFailed,
			Status:             corev1.ConditionTrue,
			Reason:             "BackoffLimitExceeded",
			LastTransitionTime: now,
		},
	}
	Expect(k8sClient.Status().Patch(context.Background(), &j, jp)).To(Succeed())

	// Wait for cache to reflect the patch.
	Eventually(func() bool {
		var updated batchv1.Job
		if err := mgrClient.Get(context.Background(),
			types.NamespacedName{Name: jobName, Namespace: jobNS}, &updated); err != nil {
			return false
		}
		for _, c := range updated.Status.Conditions {
			if c.Type == batchv1.JobFailed && c.Status == corev1.ConditionTrue {
				return true
			}
		}
		return false
	}, 5*time.Second, 50*time.Millisecond).Should(BeTrue(),
		fmt.Sprintf("job for task-uid %s must reflect Failed condition in cache", taskUID))
}

// Phase 13 HALT-01: run-1 regression Leg 1 — billing failure stamps BillingHalt.
var _ = Describe("BillingHalt run-1 regression Leg 1: backstop stamps condition", Label("envtest", "phase13", "billing-halt", "regression"), func() {
	ctx := context.Background()
	const (
		projName = "run1-reg-l1-proj"
		planRef  = "run1-reg-l1-plan"
		taskA    = "run1-reg-l1-task-a"
		taskB    = "run1-reg-l1-task-b"
	)
	var (
		envReader  *mapEnvReader
		reconciler *TaskReconciler
	)
	BeforeEach(func() {
		envReader = newMapEnvReader()
		reconciler = newTaskReconciler(envReader)
		makeProjectForHalt(ctx, projName)
		makeTask(taskA, planRef, nil, projName)
		makeTask(taskB, planRef, nil, projName)
		pumpTaskToRunning(reconciler, projName, taskA)
	})
	AfterEach(func() {
		cleanupTask(taskA)
		cleanupTask(taskB)
		cleanupProject(projName)
		cleanupHaltTestJobs(projName)
	})

	It("billing-classified failure stamps BillingHalt=True on Project", func() {
		var t tideprojectv1alpha1.Task
		Expect(mgrClient.Get(ctx, types.NamespacedName{Name: taskA, Namespace: "default"}, &t)).To(Succeed())

		// Simulate Job terminal state so checkRunningState → handleJobCompletion fires.
		markTaskJobFailed(t.UID)

		// Inject billing-classified envelope AFTER marking the Job terminal so the
		// reconcile path reads out.Reason from the fake EnvReader.
		envReader.SetOut(string(t.UID), pkgdispatch.EnvelopeOut{
			ExitCode:    1,
			Reason:      billingReason,
			Result:      "failed",
			CompletedAt: metav1.Now().Time,
		})
		nameA := types.NamespacedName{Name: taskA, Namespace: "default"}
		_, err := reconcileN(reconciler, nameA, 3)
		Expect(err).NotTo(HaveOccurred())

		// Task A must be Failed (genuine-failure patch*Failed semantics preserved).
		var ta tideprojectv1alpha1.Task
		Expect(mgrClient.Get(ctx, nameA, &ta)).To(Succeed())
		Expect(ta.Status.Phase).To(Equal("Failed"),
			"billing failure must still mark the Task as Failed (genuine-failure semantics preserved)")

		// Project must have BillingHalt=True (backstop).
		Eventually(func(g Gomega) {
			var p tideprojectv1alpha1.Project
			g.Expect(mgrClient.Get(ctx, types.NamespacedName{Name: projName, Namespace: "default"}, &p)).To(Succeed())
			c := meta.FindStatusCondition(p.Status.Conditions, tideprojectv1alpha1.ConditionBillingHalt)
			g.Expect(c).NotTo(BeNil(), "backstop must stamp BillingHalt=True on the Project")
			g.Expect(c.Status).To(Equal(metav1.ConditionTrue))
			g.Expect(c.Reason).To(Equal(tideprojectv1alpha1.ReasonCreditBalanceTooLow))
		}, 5*time.Second, 100*time.Millisecond).Should(Succeed())
	})
})

// Phase 13 HALT-01: run-1 regression Leg 2 — sibling holds while BillingHalt present.
var _ = Describe("BillingHalt run-1 regression Leg 2: sibling holds while halted", Label("envtest", "phase13", "billing-halt", "regression"), func() {
	ctx := context.Background()
	const (
		projName = "run1-reg-l2-proj"
		planRef  = "run1-reg-l2-plan"
		taskA    = "run1-reg-l2-task-a"
		taskB    = "run1-reg-l2-task-b"
	)
	var (
		envReader  *mapEnvReader
		reconciler *TaskReconciler
	)
	BeforeEach(func() {
		envReader = newMapEnvReader()
		reconciler = newTaskReconciler(envReader)
		makeProjectForHalt(ctx, projName)
		makeTask(taskA, planRef, nil, projName)
		makeTask(taskB, planRef, nil, projName)
		pumpTaskToRunning(reconciler, projName, taskA)
	})
	AfterEach(func() {
		cleanupTask(taskA)
		cleanupTask(taskB)
		cleanupProject(projName)
		cleanupHaltTestJobs(projName)
	})

	It("sibling Task B creates no Job while BillingHalt is present", func() {
		var t tideprojectv1alpha1.Task
		Expect(mgrClient.Get(ctx, types.NamespacedName{Name: taskA, Namespace: "default"}, &t)).To(Succeed())
		markTaskJobFailed(t.UID)
		envReader.SetOut(string(t.UID), pkgdispatch.EnvelopeOut{
			ExitCode: 1, Reason: billingReason, Result: "failed",
			CompletedAt: metav1.Now().Time,
		})
		nameA := types.NamespacedName{Name: taskA, Namespace: "default"}
		_, err := reconcileN(reconciler, nameA, 3)
		Expect(err).NotTo(HaveOccurred())

		// Wait for BillingHalt to land on Project.
		Eventually(func() bool {
			var p tideprojectv1alpha1.Project
			if err := mgrClient.Get(ctx, types.NamespacedName{Name: projName, Namespace: "default"}, &p); err != nil {
				return false
			}
			c := meta.FindStatusCondition(p.Status.Conditions, tideprojectv1alpha1.ConditionBillingHalt)
			return c != nil && c.Status == metav1.ConditionTrue
		}, 5*time.Second, 50*time.Millisecond).Should(BeTrue())

		// Now reconcile task B — it must NOT create a Job (held by BillingHalt).
		nameB := types.NamespacedName{Name: taskB, Namespace: "default"}
		result, err := reconcileN(reconciler, nameB, 3)
		Expect(err).NotTo(HaveOccurred())
		Expect(result.RequeueAfter).To(Equal(30*time.Second),
			"BillingHalt hold must park task B with 30s requeue")

		var tb tideprojectv1alpha1.Task
		Expect(mgrClient.Get(ctx, nameB, &tb)).To(Succeed())
		Expect(tb.Status.Phase).NotTo(Equal("Failed"),
			"D-05: sibling task B must not be Failed while BillingHalt holds")

		taskBUID := tb.UID
		var jobs batchv1.JobList
		Expect(k8sClient.List(ctx, &jobs, client.InNamespace("default"))).To(Succeed())
		for _, j := range jobs.Items {
			Expect(j.Labels["tideproject.k8s/task-uid"]).NotTo(Equal(string(taskBUID)),
				"Leg 2: no Job for task B while BillingHalt present")
		}
	})
})

// Phase 13 HALT-01: run-1 regression Leg 3 — dispatch resumes after halt cleared.
var _ = Describe("BillingHalt run-1 regression Leg 3: dispatch resumes after clear", Label("envtest", "phase13", "billing-halt", "regression"), func() {
	ctx := context.Background()
	const (
		projName = "run1-reg-l3-proj"
		planRef  = "run1-reg-l3-plan"
		taskA    = "run1-reg-l3-task-a"
		taskB    = "run1-reg-l3-task-b"
	)
	var (
		envReader  *mapEnvReader
		reconciler *TaskReconciler
	)
	BeforeEach(func() {
		envReader = newMapEnvReader()
		reconciler = newTaskReconciler(envReader)
		makeProjectForHalt(ctx, projName)
		makeTask(taskA, planRef, nil, projName)
		makeTask(taskB, planRef, nil, projName)
		pumpTaskToRunning(reconciler, projName, taskA)
	})
	AfterEach(func() {
		cleanupTask(taskA)
		cleanupTask(taskB)
		cleanupProject(projName)
		cleanupHaltTestJobs(projName)
	})

	It("dispatch resumes after BillingHalt cleared (tide resume semantics)", func() {
		var ta tideprojectv1alpha1.Task
		Expect(mgrClient.Get(ctx, types.NamespacedName{Name: taskA, Namespace: "default"}, &ta)).To(Succeed())
		markTaskJobFailed(ta.UID)
		envReader.SetOut(string(ta.UID), pkgdispatch.EnvelopeOut{
			ExitCode: 1, Reason: billingReason, Result: "failed",
			CompletedAt: metav1.Now().Time,
		})
		nameA := types.NamespacedName{Name: taskA, Namespace: "default"}
		_, _ = reconcileN(reconciler, nameA, 3)
		Eventually(func() bool {
			var p tideprojectv1alpha1.Project
			if err := mgrClient.Get(ctx, types.NamespacedName{Name: projName, Namespace: "default"}, &p); err != nil {
				return false
			}
			c := meta.FindStatusCondition(p.Status.Conditions, tideprojectv1alpha1.ConditionBillingHalt)
			return c != nil && c.Status == metav1.ConditionTrue
		}, 5*time.Second, 50*time.Millisecond).Should(BeTrue())

		nameB := types.NamespacedName{Name: taskB, Namespace: "default"}
		result, _ := reconcileN(reconciler, nameB, 2)
		Expect(result.RequeueAfter).To(Equal(30 * time.Second))

		// Clear BillingHalt (tide resume).
		clearBillingHalt(ctx, projName)

		// Re-reconcile task B — dispatch must resume.
		_, err := reconcileN(reconciler, nameB, 3)
		Expect(err).NotTo(HaveOccurred())

		Eventually(func(g Gomega) {
			var tb tideprojectv1alpha1.Task
			g.Expect(mgrClient.Get(ctx, nameB, &tb)).To(Succeed())
			g.Expect(tb.Status.Phase).To(Equal("Running"),
				"Leg 3: task B must dispatch (Running) after BillingHalt cleared")
		}, 5*time.Second, 100*time.Millisecond).Should(Succeed())
	})
})

// Phase 13 HALT-01: backstop classifier specificity — a failed envelope with
// Reason="forced-failure" must NOT set BillingHalt on the Project.
// This is an envtest spec to confirm the call-site specificity (complements the
// unit-level TestIsBillingFailureReason_ForcedFailure_False in billing_halt_test.go).
var _ = Describe("BillingHalt backstop: non-billing failure does not set condition", Label("envtest", "phase13", "billing-halt"), func() {
	ctx := context.Background()

	const (
		projName = "bh-backstop-specificity-proj"
		planRef  = "bh-backstop-specificity-plan"
		taskName = "bh-backstop-specificity-task"
	)

	var (
		envReader  *mapEnvReader
		reconciler *TaskReconciler
	)

	BeforeEach(func() {
		envReader = newMapEnvReader()
		reconciler = newTaskReconciler(envReader)
		makeProjectForHalt(ctx, projName)
		makeTask(taskName, planRef, nil, projName)

		// Pump to Running.
		name := types.NamespacedName{Name: taskName, Namespace: "default"}
		Eventually(func(g Gomega) {
			_, _ = reconcileN(reconciler, name, 2)
			var t tideprojectv1alpha1.Task
			g.Expect(mgrClient.Get(ctx, name, &t)).To(Succeed())
			g.Expect(t.Status.Phase).To(Equal("Running"))
		}, 10*time.Second, 200*time.Millisecond).Should(Succeed())
	})
	AfterEach(func() {
		cleanupTask(taskName)
		cleanupProject(projName)
		var jobs batchv1.JobList
		_ = k8sClient.List(ctx, &jobs, client.InNamespace("default"))
		for i := range jobs.Items {
			j := jobs.Items[i]
			if j.Labels["tideproject.k8s/project"] == projName {
				_ = k8sClient.Delete(ctx, &j)
			}
		}
	})

	It("forced-failure Reason does NOT set BillingHalt on Project (classifier specificity)", func() {
		var t tideprojectv1alpha1.Task
		Expect(mgrClient.Get(ctx, types.NamespacedName{Name: taskName, Namespace: "default"}, &t)).To(Succeed())

		// Mark the Job terminal so checkRunningState → handleJobCompletion fires.
		markTaskJobFailed(t.UID)

		envReader.SetOut(string(t.UID), pkgdispatch.EnvelopeOut{
			ExitCode:    1,
			Reason:      "forced-failure",
			Result:      "failed",
			CompletedAt: metav1.Now().Time,
		})

		name := types.NamespacedName{Name: taskName, Namespace: "default"}
		_, err := reconcileN(reconciler, name, 3)
		Expect(err).NotTo(HaveOccurred())

		// Task must be Failed (genuine failure).
		var updated tideprojectv1alpha1.Task
		Expect(mgrClient.Get(ctx, name, &updated)).To(Succeed())
		Expect(updated.Status.Phase).To(Equal("Failed"))

		// Project must NOT have BillingHalt condition.
		var proj tideprojectv1alpha1.Project
		Expect(mgrClient.Get(ctx, types.NamespacedName{Name: projName, Namespace: "default"}, &proj)).To(Succeed())
		c := meta.FindStatusCondition(proj.Status.Conditions, tideprojectv1alpha1.ConditionBillingHalt)
		Expect(c).To(BeNil(),
			"forced-failure must NOT set BillingHalt (classifier specificity preserved at call site)")
	})
})

// newBHMilestoneReconciler builds a MilestoneReconciler for BillingHalt hold specs.
// Uses testSigningKey so the dispatch guard (len(SigningKey) > 0) passes and
// the reconciler enters the dispatch path before hitting the BillingHalt gate.
func newBHMilestoneReconciler() *MilestoneReconciler {
	return &MilestoneReconciler{
		Client:         mgrClient,
		Scheme:         k8sClient.Scheme(),
		SigningKey:     testSigningKey,
		CredproxyImage: testCredproxyImage,
		HelmProviderDefaults: ProviderDefaults{
			Image: testSubagentImage,
		},
		EnvReader: newMapEnvReader(),
	}
}

// newBHPhaseReconciler builds a PhaseReconciler for BillingHalt hold specs.
func newBHPhaseReconciler() *PhaseReconciler {
	return &PhaseReconciler{
		Client:         mgrClient,
		Scheme:         k8sClient.Scheme(),
		SigningKey:     testSigningKey,
		CredproxyImage: testCredproxyImage,
		HelmProviderDefaults: ProviderDefaults{
			Image: testSubagentImage,
		},
		EnvReader: newMapEnvReader(),
	}
}

// newBHPlanReconciler builds a PlanReconciler for BillingHalt hold specs.
func newBHPlanReconciler() *PlanReconciler {
	return &PlanReconciler{
		Client:         mgrClient,
		Scheme:         k8sClient.Scheme(),
		SigningKey:     testSigningKey,
		CredproxyImage: testCredproxyImage,
		HelmProviderDefaults: ProviderDefaults{
			Image: testSubagentImage,
		},
		EnvReader: newMapEnvReader(),
	}
}

// newBHProjectReconciler builds a ProjectReconciler for BillingHalt hold specs.
func newBHProjectReconciler() *ProjectReconciler {
	return &ProjectReconciler{
		Client:         mgrClient,
		Scheme:         k8sClient.Scheme(),
		SigningKey:     testSigningKey,
		CredproxyImage: testCredproxyImage,
		HelmProviderDefaults: ProviderDefaults{
			Image: testSubagentImage,
		},
		EnvReader: newMapEnvReader(),
	}
}

// ---- unused import guard ----
var _ = fmt.Sprintf
