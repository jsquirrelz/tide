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
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	tideprojectv1alpha1 "github.com/jsquirrelz/tide/api/v1alpha1"
	controller "github.com/jsquirrelz/tide/internal/controller"
	"github.com/jsquirrelz/tide/internal/gates"
	pkgdispatch "github.com/jsquirrelz/tide/pkg/dispatch"
)

// Plan 04-05 Task 3: Layer A integration envtest covering the three core gate
// flows end-to-end:
//
//   1. TestGateApproveFlow — Project.Gates.Milestone=approve → Milestone parks
//      at AwaitingApproval → annotate approve-milestone=true → Eventually
//      Succeeded and annotation consumed.
//   2. TestRejectHalts — operator writes reject annotation on Project mid-run
//      → all in-flight up-stack CRDs reach Status.Phase=Failed with
//      Reason=RejectedByUser and Message containing the operator reason.
//   3. TestWavePauseBetweenWaves — Project.Gates.PauseBetweenWaves=true; 2-
//      wave Task DAG; wave 0 Succeeded → Plan Condition WaveOrLevelPaused
//      True → annotate approve-wave-1=true → Eventually wave 2 dispatches
//      (the Tasks lose the wave-paused label) and the Condition flips False.
var _ = Describe("Plan 04-05 Task 3 — gate-flow envtest", Label("envtest", "phase4", "gates-integration"), func() {
	ctx := context.Background()

	makeFakeJobTerminalGates := func(name, namespace string) error {
		var job batchv1.Job
		if err := mgrClient.Get(ctx, types.NamespacedName{Name: name, Namespace: namespace}, &job); err != nil {
			return err
		}
		now := metav1.Now()
		job.Status.StartTime = &now
		job.Status.CompletionTime = &now
		job.Status.Succeeded = 1
		job.Status.Conditions = []batchv1.JobCondition{
			{Type: batchv1.JobSuccessCriteriaMet, Status: corev1.ConditionTrue, LastTransitionTime: now},
			{Type: batchv1.JobComplete, Status: corev1.ConditionTrue, LastTransitionTime: now},
		}
		return mgrClient.Status().Update(ctx, &job)
	}

	driveMSReconcile := func(r *controller.MilestoneReconciler, name string, n int) {
		for i := 0; i < n; i++ {
			_, _ = r.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: name, Namespace: "default"},
			})
		}
	}
	drivePlanReconcile := func(r *controller.PlanReconciler, name string, n int) {
		for i := 0; i < n; i++ {
			_, _ = r.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: name, Namespace: "default"},
			})
		}
	}

	// TestGateApproveFlow — Milestone-level approve gate handshake.
	Describe("TestGateApproveFlow", func() {
		const projectName = "gate-it-proj-1"
		const msName = "gate-it-ms-1"

		AfterEach(func() {
			cleanupGateFlowFixture(projectName, "", msName, "")
		})

		It("approve-milestone annotation handshake transitions Milestone Succeeded", func() {
			// 1. Apply Project with Gates.Milestone=approve.
			proj := &tideprojectv1alpha1.Project{
				ObjectMeta: metav1.ObjectMeta{Name: projectName, Namespace: "default"},
				Spec: tideprojectv1alpha1.ProjectSpec{
					TargetRepo: "https://github.com/example/tide.git",
					Gates:      tideprojectv1alpha1.Gates{Milestone: gates.PolicyApprove},
				},
			}
			Expect(k8sClient.Create(ctx, proj)).To(Succeed())
			waitITCacheSync(projectName, &tideprojectv1alpha1.Project{})

			// 2. Apply Milestone owned by Project.
			ms := &tideprojectv1alpha1.Milestone{
				ObjectMeta: metav1.ObjectMeta{Name: msName, Namespace: "default"},
				Spec:       tideprojectv1alpha1.MilestoneSpec{ProjectRef: projectName},
			}
			Expect(k8sClient.Create(ctx, ms)).To(Succeed())
			waitITCacheSync(msName, &tideprojectv1alpha1.Milestone{})

			// 3. Drive MilestoneReconciler through the planner-dispatch + job-completion seam.
			envReader := newMapEnvReader()
			r := newMilestoneReconcilerForGateIT(envReader)
			driveMSReconcile(r, msName, 5)

			// 3a. Fetch UID and patch envelope-out + Job terminal.
			var got tideprojectv1alpha1.Milestone
			Expect(mgrClient.Get(ctx, types.NamespacedName{Name: msName, Namespace: "default"}, &got)).To(Succeed())
			envReader.SetOut(string(got.UID), pkgdispatch.EnvelopeOut{TaskUID: string(got.UID), ExitCode: 0})
			Expect(makeFakeJobTerminalGates(fmt.Sprintf("tide-milestone-%s-1", got.UID), "default")).To(Succeed())
			driveMSReconcile(r, msName, 3)

			// 4. Assert Milestone parked at AwaitingApproval.
			Eventually(func(g Gomega) {
				var ms2 tideprojectv1alpha1.Milestone
				g.Expect(mgrClient.Get(ctx, types.NamespacedName{Name: msName, Namespace: "default"}, &ms2)).To(Succeed())
				g.Expect(ms2.Status.Phase).To(Equal("AwaitingApproval"))
				c := meta.FindStatusCondition(ms2.Status.Conditions, tideprojectv1alpha1.ConditionWaveOrLevelPaused)
				g.Expect(c).NotTo(BeNil())
				g.Expect(c.Status).To(Equal(metav1.ConditionTrue))
				g.Expect(c.Reason).To(Equal(tideprojectv1alpha1.ReasonAwaitingApproval))
			}, 5*time.Second, 100*time.Millisecond).Should(Succeed())

			// 5. Apply approve-milestone annotation.
			var current tideprojectv1alpha1.Milestone
			Expect(mgrClient.Get(ctx, types.NamespacedName{Name: msName, Namespace: "default"}, &current)).To(Succeed())
			patch := client.MergeFrom(current.DeepCopy())
			if current.Annotations == nil {
				current.Annotations = map[string]string{}
			}
			current.Annotations[gates.AnnotationApprovePrefix+"milestone"] = "true"
			Expect(k8sClient.Patch(ctx, &current, patch)).To(Succeed())

			// 6. Drive reconcile — consume annotation + patch Succeeded.
			driveMSReconcile(r, msName, 3)

			Eventually(func(g Gomega) {
				var ms2 tideprojectv1alpha1.Milestone
				g.Expect(mgrClient.Get(ctx, types.NamespacedName{Name: msName, Namespace: "default"}, &ms2)).To(Succeed())
				g.Expect(ms2.Status.Phase).To(Equal("Succeeded"))
				_, has := ms2.Annotations[gates.AnnotationApprovePrefix+"milestone"]
				g.Expect(has).To(BeFalse(), "approve-milestone annotation should be consumed")
			}, 5*time.Second, 100*time.Millisecond).Should(Succeed())
		})
	})

	// TestRejectHalts — reject annotation on Project halts up-stack reconcilers.
	Describe("TestRejectHalts", func() {
		const projectName = "gate-it-proj-2"
		const msName = "gate-it-ms-2"

		AfterEach(func() {
			cleanupGateFlowFixture(projectName, "", msName, "")
		})

		It("Milestone reaches Status.Phase=Failed with Reason=RejectedByUser and Message containing the operator reason", func() {
			proj := &tideprojectv1alpha1.Project{
				ObjectMeta: metav1.ObjectMeta{Name: projectName, Namespace: "default"},
				Spec: tideprojectv1alpha1.ProjectSpec{
					TargetRepo: "https://github.com/example/tide.git",
					Gates:      tideprojectv1alpha1.Gates{Milestone: gates.PolicyAuto},
				},
			}
			Expect(k8sClient.Create(ctx, proj)).To(Succeed())
			waitITCacheSync(projectName, &tideprojectv1alpha1.Project{})

			// Annotate Project rejected mid-run.
			var p tideprojectv1alpha1.Project
			Expect(mgrClient.Get(ctx, types.NamespacedName{Name: projectName, Namespace: "default"}, &p)).To(Succeed())
			patch := client.MergeFrom(p.DeepCopy())
			if p.Annotations == nil {
				p.Annotations = map[string]string{}
			}
			p.Annotations[gates.AnnotationReject] = "operator stop"
			Expect(k8sClient.Patch(ctx, &p, patch)).To(Succeed())
			Eventually(func() string {
				var pp tideprojectv1alpha1.Project
				if err := mgrClient.Get(ctx, types.NamespacedName{Name: projectName, Namespace: "default"}, &pp); err != nil {
					return ""
				}
				return pp.Annotations[gates.AnnotationReject]
			}, 5*time.Second, 50*time.Millisecond).Should(Equal("operator stop"))

			ms := &tideprojectv1alpha1.Milestone{
				ObjectMeta: metav1.ObjectMeta{Name: msName, Namespace: "default"},
				Spec:       tideprojectv1alpha1.MilestoneSpec{ProjectRef: projectName},
			}
			Expect(k8sClient.Create(ctx, ms)).To(Succeed())
			waitITCacheSync(msName, &tideprojectv1alpha1.Milestone{})

			envReader := newMapEnvReader()
			r := newMilestoneReconcilerForGateIT(envReader)
			driveMSReconcile(r, msName, 5)

			var got tideprojectv1alpha1.Milestone
			Expect(mgrClient.Get(ctx, types.NamespacedName{Name: msName, Namespace: "default"}, &got)).To(Succeed())
			envReader.SetOut(string(got.UID), pkgdispatch.EnvelopeOut{TaskUID: string(got.UID), ExitCode: 0})
			Expect(makeFakeJobTerminalGates(fmt.Sprintf("tide-milestone-%s-1", got.UID), "default")).To(Succeed())
			driveMSReconcile(r, msName, 3)

			Eventually(func(g Gomega) {
				var ms2 tideprojectv1alpha1.Milestone
				g.Expect(mgrClient.Get(ctx, types.NamespacedName{Name: msName, Namespace: "default"}, &ms2)).To(Succeed())
				g.Expect(ms2.Status.Phase).To(Equal("Failed"))
				c := meta.FindStatusCondition(ms2.Status.Conditions, tideprojectv1alpha1.ConditionFailed)
				g.Expect(c).NotTo(BeNil())
				g.Expect(c.Reason).To(Equal(tideprojectv1alpha1.ReasonRejectedByUser))
				g.Expect(c.Message).To(ContainSubstring("operator stop"))
			}, 5*time.Second, 100*time.Millisecond).Should(Succeed())
		})
	})

	// TestWavePauseBetweenWaves — PauseBetweenWaves dispatch boundary.
	Describe("TestWavePauseBetweenWaves", func() {
		const projectName = "gate-it-proj-3"
		const msName = "gate-it-ms-3"
		const phaseName = "gate-it-ph-3"
		const planName = "gate-it-plan-3"

		AfterEach(func() {
			cleanupGateFlowFixture(projectName, planName, msName, phaseName)
		})

		It("wave 1 dispatch blocked until approve-wave-1 annotation lands on Plan", func() {
			// 1. Project with PauseBetweenWaves=true.
			proj := &tideprojectv1alpha1.Project{
				ObjectMeta: metav1.ObjectMeta{Name: projectName, Namespace: "default"},
				Spec: tideprojectv1alpha1.ProjectSpec{
					TargetRepo: "https://github.com/example/tide.git",
					Gates:      tideprojectv1alpha1.Gates{PauseBetweenWaves: true},
				},
			}
			Expect(k8sClient.Create(ctx, proj)).To(Succeed())
			waitITCacheSync(projectName, &tideprojectv1alpha1.Project{})

			// 2. Milestone + Phase chain so resolveProjectForPlan walks the chain.
			ms := &tideprojectv1alpha1.Milestone{
				ObjectMeta: metav1.ObjectMeta{Name: msName, Namespace: "default"},
				Spec:       tideprojectv1alpha1.MilestoneSpec{ProjectRef: projectName},
			}
			Expect(k8sClient.Create(ctx, ms)).To(Succeed())
			waitITCacheSync(msName, &tideprojectv1alpha1.Milestone{})
			ph := &tideprojectv1alpha1.Phase{
				ObjectMeta: metav1.ObjectMeta{Name: phaseName, Namespace: "default"},
				Spec:       tideprojectv1alpha1.PhaseSpec{MilestoneRef: msName},
			}
			Expect(k8sClient.Create(ctx, ph)).To(Succeed())
			waitITCacheSync(phaseName, &tideprojectv1alpha1.Phase{})

			// 3. Plan + 2-wave Task DAG. Mark Plan Validated post-create.
			plan := &tideprojectv1alpha1.Plan{
				ObjectMeta: metav1.ObjectMeta{Name: planName, Namespace: "default"},
				Spec:       tideprojectv1alpha1.PlanSpec{PhaseRef: phaseName},
			}
			Expect(k8sClient.Create(ctx, plan)).To(Succeed())
			waitITCacheSync(planName, &tideprojectv1alpha1.Plan{})
			var planObj tideprojectv1alpha1.Plan
			Expect(mgrClient.Get(ctx, types.NamespacedName{Name: planName, Namespace: "default"}, &planObj)).To(Succeed())
			vpatch := client.MergeFrom(planObj.DeepCopy())
			planObj.Status.ValidationState = "Validated"
			Expect(k8sClient.Status().Patch(ctx, &planObj, vpatch)).To(Succeed())
			Eventually(func() string {
				var pp tideprojectv1alpha1.Plan
				if err := mgrClient.Get(ctx, types.NamespacedName{Name: planName, Namespace: "default"}, &pp); err != nil {
					return ""
				}
				return pp.Status.ValidationState
			}, 5*time.Second, 50*time.Millisecond).Should(Equal("Validated"))

			_ = makeGateITTask(planName+"-alpha", planName, projectName, nil)
			_ = makeGateITTask(planName+"-beta", planName, projectName, nil)
			gamma := makeGateITTask(planName+"-gamma", planName, projectName, []string{planName + "-alpha"})

			// 4. Drive PlanReconciler — materializes Waves + stamps labels.
			rPlan := newPlanReconcilerForGateIT()
			drivePlanReconcile(rPlan, planName, 5)

			// 5. Mark wave 0 (alpha + beta) Succeeded.
			markGateITTaskSucceeded(planName + "-alpha")
			markGateITTaskSucceeded(planName + "-beta")

			// 6. Drive PlanReconciler — pause boundary detection.
			drivePlanReconcile(rPlan, planName, 3)

			Eventually(func(g Gomega) {
				var pp tideprojectv1alpha1.Plan
				g.Expect(mgrClient.Get(ctx, types.NamespacedName{Name: planName, Namespace: "default"}, &pp)).To(Succeed())
				c := meta.FindStatusCondition(pp.Status.Conditions, tideprojectv1alpha1.ConditionWaveOrLevelPaused)
				g.Expect(c).NotTo(BeNil())
				g.Expect(c.Status).To(Equal(metav1.ConditionTrue))
				g.Expect(c.Reason).To(Equal(tideprojectv1alpha1.ReasonPausedAtBoundary))
				var gammaObj tideprojectv1alpha1.Task
				g.Expect(mgrClient.Get(ctx, types.NamespacedName{Name: gamma.Name, Namespace: "default"}, &gammaObj)).To(Succeed())
				g.Expect(gammaObj.Labels["tideproject.k8s/wave-paused"]).To(Equal("1"))
			}, 5*time.Second, 100*time.Millisecond).Should(Succeed())

			// 7. Annotate Plan with approve-wave-1=true.
			var current tideprojectv1alpha1.Plan
			Expect(mgrClient.Get(ctx, types.NamespacedName{Name: planName, Namespace: "default"}, &current)).To(Succeed())
			apatch := client.MergeFrom(current.DeepCopy())
			if current.Annotations == nil {
				current.Annotations = map[string]string{}
			}
			current.Annotations[gates.AnnotationApproveWavePrefix+"1"] = "true"
			Expect(k8sClient.Patch(ctx, &current, apatch)).To(Succeed())

			// 8. Drive PlanReconciler — consume annotation, clear labels, flip Condition.
			drivePlanReconcile(rPlan, planName, 3)

			Eventually(func(g Gomega) {
				var pp tideprojectv1alpha1.Plan
				g.Expect(mgrClient.Get(ctx, types.NamespacedName{Name: planName, Namespace: "default"}, &pp)).To(Succeed())
				c := meta.FindStatusCondition(pp.Status.Conditions, tideprojectv1alpha1.ConditionWaveOrLevelPaused)
				g.Expect(c).NotTo(BeNil())
				g.Expect(c.Status).To(Equal(metav1.ConditionFalse))
				_, hasAnno := pp.Annotations[gates.AnnotationApproveWavePrefix+"1"]
				g.Expect(hasAnno).To(BeFalse(), "approve-wave-1 annotation should be consumed")
				var gammaObj tideprojectv1alpha1.Task
				g.Expect(mgrClient.Get(ctx, types.NamespacedName{Name: gamma.Name, Namespace: "default"}, &gammaObj)).To(Succeed())
				_, hasLabel := gammaObj.Labels["tideproject.k8s/wave-paused"]
				g.Expect(hasLabel).To(BeFalse(), "gamma wave-paused label should be cleared")
			}, 5*time.Second, 100*time.Millisecond).Should(Succeed())
		})
	})
})

// ----- gate-flow test helpers -----

// newMilestoneReconcilerForGateIT constructs a MilestoneReconciler with the
// Dispatcher seam wired so handleJobCompletion runs (where the Plan 04-05
// gate-policy hook lives).
func newMilestoneReconcilerForGateIT(envReader *mapEnvReader) *controller.MilestoneReconciler {
	return &controller.MilestoneReconciler{
		Client:        mgrClient,
		Scheme:        k8sClient.Scheme(),
		Dispatcher:    &stubDispatcher{},
		EnvReader:     envReader,
		SubagentImage: testSubagentImage,
	}
}

// newPlanReconcilerForGateIT constructs a PlanReconciler with the Dispatcher
// seam wired.
func newPlanReconcilerForGateIT() *controller.PlanReconciler {
	return &controller.PlanReconciler{
		Client:     mgrClient,
		Scheme:     k8sClient.Scheme(),
		Dispatcher: &stubDispatcher{},
	}
}

// makeGateITTask creates a Task with the project label so TaskReconciler's
// resolveProject finds the parent Project.
func makeGateITTask(name, planRef, projectName string, dependsOn []string) *tideprojectv1alpha1.Task {
	t := &tideprojectv1alpha1.Task{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "default",
			Labels: map[string]string{
				"tideproject.k8s/project": projectName,
			},
		},
		Spec: tideprojectv1alpha1.TaskSpec{
			PlanRef:             planRef,
			FilesTouched:        []string{"src/" + name + ".go"},
			DeclaredOutputPaths: []string{"artifacts/" + name + ".txt"},
			DependsOn:           dependsOn,
		},
	}
	Expect(k8sClient.Create(context.Background(), t)).To(Succeed())
	waitITCacheSync(name, &tideprojectv1alpha1.Task{})
	return t
}

func markGateITTaskSucceeded(name string) {
	var t tideprojectv1alpha1.Task
	Expect(k8sClient.Get(context.Background(), types.NamespacedName{Name: name, Namespace: "default"}, &t)).To(Succeed())
	patch := client.MergeFrom(t.DeepCopy())
	t.Status.Phase = "Succeeded"
	Expect(k8sClient.Status().Patch(context.Background(), &t, patch)).To(Succeed())
	Eventually(func() string {
		var got tideprojectv1alpha1.Task
		if err := mgrClient.Get(context.Background(), types.NamespacedName{Name: name, Namespace: "default"}, &got); err != nil {
			return ""
		}
		return got.Status.Phase
	}, 5*time.Second, 50*time.Millisecond).Should(Equal("Succeeded"))
}

// waitITCacheSync mirrors internal/controller waitForCacheSync but uses
// mgrClient available in this package's BeforeSuite.
func waitITCacheSync(name string, obj client.Object) {
	Eventually(func() error {
		return mgrClient.Get(context.Background(), types.NamespacedName{Name: name, Namespace: "default"}, obj)
	}, 5*time.Second, 50*time.Millisecond).Should(Succeed(),
		"timed out waiting for cache to sync object default/%s", name)
}

// cleanupGateFlowFixture removes Project + Milestone + Phase + Plan + Tasks
// after each test. planName / phaseName may be empty for tests that don't
// create those resources.
func cleanupGateFlowFixture(projectName, planName, msName, phaseName string) {
	c := context.Background()
	if planName != "" {
		var taskList tideprojectv1alpha1.TaskList
		_ = k8sClient.List(c, &taskList, client.InNamespace("default"))
		for i := range taskList.Items {
			t := taskList.Items[i]
			if t.Spec.PlanRef == planName {
				t.Finalizers = nil
				_ = k8sClient.Update(c, &t)
				_ = k8sClient.Delete(c, &t)
			}
		}
		var waveList tideprojectv1alpha1.WaveList
		_ = k8sClient.List(c, &waveList, client.InNamespace("default"))
		for i := range waveList.Items {
			w := waveList.Items[i]
			if w.Spec.PlanRef == planName {
				w.Finalizers = nil
				_ = k8sClient.Update(c, &w)
				_ = k8sClient.Delete(c, &w)
			}
		}
		plan := &tideprojectv1alpha1.Plan{}
		if err := k8sClient.Get(c, types.NamespacedName{Name: planName, Namespace: "default"}, plan); err == nil {
			plan.Finalizers = nil
			_ = k8sClient.Update(c, plan)
			_ = k8sClient.Delete(c, plan)
		}
	}
	if phaseName != "" {
		ph := &tideprojectv1alpha1.Phase{}
		if err := k8sClient.Get(c, types.NamespacedName{Name: phaseName, Namespace: "default"}, ph); err == nil {
			ph.Finalizers = nil
			_ = k8sClient.Update(c, ph)
			_ = k8sClient.Delete(c, ph)
		}
	}
	if msName != "" {
		ms := &tideprojectv1alpha1.Milestone{}
		if err := k8sClient.Get(c, types.NamespacedName{Name: msName, Namespace: "default"}, ms); err == nil {
			ms.Finalizers = nil
			_ = k8sClient.Update(c, ms)
			_ = k8sClient.Delete(c, ms)
		}
	}
	proj := &tideprojectv1alpha1.Project{}
	if err := k8sClient.Get(c, types.NamespacedName{Name: projectName, Namespace: "default"}, proj); err == nil {
		proj.Finalizers = nil
		_ = k8sClient.Update(c, proj)
		_ = k8sClient.Delete(c, proj)
	}
	var jobs batchv1.JobList
	_ = k8sClient.List(c, &jobs, client.InNamespace("default"))
	for i := range jobs.Items {
		j := jobs.Items[i]
		_ = k8sClient.Delete(c, &j)
	}
}

// silence unused-import warnings if ctrl/result types drift; kept for symmetry
// with the controller-suite drive helpers.
var _ ctrl.Result
var _ reconcile.Result
