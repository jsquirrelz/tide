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
	"fmt"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	batchv1 "k8s.io/api/batch/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	tideprojectv1alpha3 "github.com/jsquirrelz/tide/api/v1alpha3"
)

// makeWaveWithTasks creates a Wave and N Tasks for testing WaveReconciler.
// All tasks are assigned the given wave-index label.
// Waits for each object to appear in the mgrClient cache before returning.
func makeWaveWithTasks(planRef, waveName string, waveIndex int, taskNames []string) *tideprojectv1alpha3.Wave {
	wave := &tideprojectv1alpha3.Wave{
		ObjectMeta: metav1.ObjectMeta{
			Name:      waveName,
			Namespace: "default",
			OwnerReferences: []metav1.OwnerReference{
				{APIVersion: tideprojectv1alpha3.GroupVersion.String(), Kind: "Plan", Name: planRef, UID: "dummy-uid"},
			},
		},
		Spec: tideprojectv1alpha3.WaveSpec{
			// v1alpha3 Waves are global-scope: ProjectRef replaces the removed
			// PlanRef. The planRef arg is reused as a non-empty ref identifier for
			// these WaveReconciler tests (TODO(phase-24): plumb a real ProjectRef
			// once the global assembler creates Waves).
			ProjectRef: planRef,
			WaveIndex:  waveIndex,
		},
	}
	Expect(k8sClient.Create(context.Background(), wave)).To(Succeed())
	Eventually(func() error {
		return mgrClient.Get(context.Background(),
			types.NamespacedName{Name: waveName, Namespace: "default"},
			&tideprojectv1alpha3.Wave{})
	}, 5*time.Second, 50*time.Millisecond).Should(Succeed())

	for _, name := range taskNames {
		t := &tideprojectv1alpha3.Task{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: "default",
				Labels: map[string]string{
					"tideproject.k8s/wave-index": fmt.Sprintf("%d", waveIndex),
					// WR-01: the observational roll-up now scopes member Tasks by
					// project (owner.LabelProject == Wave.Spec.ProjectRef == planRef).
					"tideproject.k8s/project": planRef,
				},
			},
			Spec: tideprojectv1alpha3.TaskSpec{
				PlanRef:             planRef,
				FilesTouched:        []string{"src/main.go"},
				DeclaredOutputPaths: []string{"artifacts/out.txt"},
				PromptPath:          "envelopes/test/children/" + name + ".json",
			},
		}
		Expect(k8sClient.Create(context.Background(), t)).To(Succeed())
		taskName := name
		Eventually(func() error {
			return mgrClient.Get(context.Background(),
				types.NamespacedName{Name: taskName, Namespace: "default"},
				&tideprojectv1alpha3.Task{})
		}, 5*time.Second, 50*time.Millisecond).Should(Succeed())
	}
	return wave
}

// setTaskPhase patches a Task's status Phase and waits for the cache to reflect
// the update before returning, so reconcilers using mgrClient see the new phase.
func setTaskPhase(name, phase string) {
	task := &tideprojectv1alpha3.Task{}
	Expect(k8sClient.Get(context.Background(), types.NamespacedName{Name: name, Namespace: "default"}, task)).To(Succeed())
	patch := client.MergeFrom(task.DeepCopy())
	task.Status.Phase = phase
	Expect(k8sClient.Status().Patch(context.Background(), task, patch)).To(Succeed())
	// Wait for the cache to reflect the updated phase.
	Eventually(func() string {
		var t tideprojectv1alpha3.Task
		if err := mgrClient.Get(context.Background(), types.NamespacedName{Name: name, Namespace: "default"}, &t); err != nil {
			return ""
		}
		return t.Status.Phase
	}, 5*time.Second, 50*time.Millisecond).Should(Equal(phase))
}

// cleanupWave deletes a Wave and its member tasks.
func cleanupWave(waveName string, taskNames []string) {
	wave := &tideprojectv1alpha3.Wave{}
	if err := k8sClient.Get(context.Background(), types.NamespacedName{Name: waveName, Namespace: "default"}, wave); err == nil {
		r := &WaveReconciler{Client: k8sClient, Scheme: k8sClient.Scheme()}
		_ = k8sClient.Delete(context.Background(), wave)
		for range 3 {
			_, _ = r.Reconcile(context.Background(), reconcile.Request{
				NamespacedName: types.NamespacedName{Name: waveName, Namespace: "default"},
			})
		}
	}
	for _, name := range taskNames {
		t := &tideprojectv1alpha3.Task{}
		if err := k8sClient.Get(context.Background(), types.NamespacedName{Name: name, Namespace: "default"}, t); err == nil {
			_ = k8sClient.Delete(context.Background(), t)
		}
	}
}

// newWaveReconciler builds a WaveReconciler wired for testing (D-B2: observational only).
// Uses mgrClient (the manager's cached client) so that MatchingFields queries against
// the in-process .spec.planRef field indexer work correctly.
func newWaveReconciler() *WaveReconciler {
	return &WaveReconciler{
		Client:     mgrClient,
		Scheme:     k8sClient.Scheme(),
		Dispatcher: &stubDispatcher{},
	}
}

// reconcileWaveN drives a WaveReconciler N times, retrying on 409 Conflict.
func reconcileWaveN(r *WaveReconciler, name types.NamespacedName, n int) (ctrl.Result, error) {
	var result ctrl.Result
	var err error
	for range n {
		for range 5 {
			result, err = r.Reconcile(context.Background(), reconcile.Request{NamespacedName: name})
			if err == nil {
				break
			}
			if strings.Contains(err.Error(), "the object has been modified") {
				err = nil
				continue
			}
			return result, err
		}
		if err != nil {
			return result, err
		}
	}
	return result, err
}

var _ = Describe("WaveReconciler observational roll-up", Label("envtest", "phase2"), func() {
	ctx := context.Background()

	Describe("TestWaveReconciler_PhaseSucceeded_WhenAllTasksSucceeded", func() {
		const planRef = "plan-wave-succ"
		const wName = "wave-succ"
		taskNames := []string{"wave-task-a-succ", "wave-task-b-succ", "wave-task-c-succ"}

		BeforeEach(func() {
			makeWaveWithTasks(planRef, wName, 90, taskNames)
		})
		AfterEach(func() {
			cleanupWave(wName, taskNames)
		})

		It("should set Wave.Status.Phase=Succeeded when all member Tasks are Succeeded", func() {
			r := newWaveReconciler()
			wavNS := types.NamespacedName{Name: wName, Namespace: "default"}

			_, _ = reconcileWaveN(r, wavNS, 3)
			for _, name := range taskNames {
				setTaskPhase(name, "Succeeded")
			}
			_, err := reconcileWaveN(r, wavNS, 1)
			Expect(err).NotTo(HaveOccurred())

			var wave tideprojectv1alpha3.Wave
			Expect(k8sClient.Get(ctx, wavNS, &wave)).To(Succeed())
			Expect(wave.Status.Phase).To(Equal("Succeeded"))
		})
	})

	Describe("TestWaveReconciler_PhaseFailed_WhenOneTaskFailed_OthersSucceeded", func() {
		const planRef = "plan-wave-fail"
		const wName = "wave-fail"
		taskNames := []string{"wave-task-a-fail", "wave-task-b-fail", "wave-task-c-fail"}

		BeforeEach(func() {
			makeWaveWithTasks(planRef, wName, 91, taskNames)
		})
		AfterEach(func() {
			cleanupWave(wName, taskNames)
		})

		It("should set Wave.Status.Phase=Failed when one Task is Failed", func() {
			r := newWaveReconciler()
			wavNS := types.NamespacedName{Name: wName, Namespace: "default"}

			_, _ = reconcileWaveN(r, wavNS, 3)

			setTaskPhase(taskNames[0], "Succeeded")
			setTaskPhase(taskNames[1], "Succeeded")
			setTaskPhase(taskNames[2], "Failed")

			_, err := reconcileWaveN(r, wavNS, 1)
			Expect(err).NotTo(HaveOccurred())

			var wave tideprojectv1alpha3.Wave
			Expect(k8sClient.Get(ctx, wavNS, &wave)).To(Succeed())
			Expect(wave.Status.Phase).To(Equal("Failed"))
		})
	})

	Describe("TestWaveReconciler_PhaseRunning_WhenSomeStillPending", func() {
		const planRef = "plan-wave-run"
		const wName = "wave-run"
		taskNames := []string{"wave-task-a-run", "wave-task-b-run"}

		BeforeEach(func() {
			makeWaveWithTasks(planRef, wName, 92, taskNames)
		})
		AfterEach(func() {
			cleanupWave(wName, taskNames)
		})

		It("should set Wave.Status.Phase=Running when some tasks are not yet terminal", func() {
			r := newWaveReconciler()
			wavNS := types.NamespacedName{Name: wName, Namespace: "default"}

			_, _ = reconcileWaveN(r, wavNS, 3)

			setTaskPhase(taskNames[0], "Succeeded")
			// taskNames[1] phase is empty (Pending)

			_, err := reconcileWaveN(r, wavNS, 1)
			Expect(err).NotTo(HaveOccurred())

			var wave tideprojectv1alpha3.Wave
			Expect(k8sClient.Get(ctx, wavNS, &wave)).To(Succeed())
			Expect(wave.Status.Phase).To(Equal("Running"))
		})
	})

	Describe("TestWaveReconciler_NeverCreatesJobs", func() {
		const planRef = "plan-wave-nojob"
		const wName = "wave-nojob"
		taskNames := []string{"wave-task-nojob"}

		BeforeEach(func() {
			makeWaveWithTasks(planRef, wName, 93, taskNames)
		})
		AfterEach(func() {
			cleanupWave(wName, taskNames)
		})

		It("should NEVER create a Job when reconciling a Wave (D-B1 load-bearing test)", func() {
			// Wire ONLY WaveReconciler — no TaskReconciler in this manager scope.
			// Any Job created would have to come from WaveReconciler itself, confirming D-B1.
			r := newWaveReconciler()
			wavNS := types.NamespacedName{Name: wName, Namespace: "default"}

			_, err := reconcileWaveN(r, wavNS, 5)
			Expect(err).NotTo(HaveOccurred())

			var task tideprojectv1alpha3.Task
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: taskNames[0], Namespace: "default"}, &task)).To(Succeed())

			var jobList batchv1.JobList
			Expect(k8sClient.List(ctx, &jobList,
				client.InNamespace("default"),
				client.MatchingLabels{"tideproject.k8s/task-uid": string(task.UID)},
			)).To(Succeed())
			Expect(jobList.Items).To(BeEmpty(),
				"WaveReconciler MUST NEVER create Jobs (D-B1)")
		})
	})

	Describe("TestWaveReconciler_PatchesTaskRefs", func() {
		const planRef = "plan-wave-refs"
		const wName = "wave-refs"
		taskNames := []string{"wave-task-ref-a", "wave-task-ref-b"}

		BeforeEach(func() {
			makeWaveWithTasks(planRef, wName, 94, taskNames)
		})
		AfterEach(func() {
			cleanupWave(wName, taskNames)
		})

		It("should populate Wave.Status.TaskRefs with all member task names", func() {
			r := newWaveReconciler()
			wavNS := types.NamespacedName{Name: wName, Namespace: "default"}

			_, _ = reconcileWaveN(r, wavNS, 3)
			_, err := reconcileWaveN(r, wavNS, 1)
			Expect(err).NotTo(HaveOccurred())

			var wave tideprojectv1alpha3.Wave
			Expect(k8sClient.Get(ctx, wavNS, &wave)).To(Succeed())
			Expect(wave.Status.TaskRefs).To(ConsistOf(taskNames))
		})
	})

	Describe("TestWaveReconciler_TransitiveTrigger_ViaTaskWatch", func() {
		const planRef = "plan-wave-trigger"
		// Phase 24: taskToWaveMapper derives the Wave name from the Task's global
		// labels (tide-wave-<project>-<wave-index>). The Wave must be named per that
		// scheme so the mapper's derived request matches an existing Wave.
		const wName = "tide-wave-plan-wave-trigger-95"
		taskNames := []string{"wave-task-trig-a"}

		BeforeEach(func() {
			makeWaveWithTasks(planRef, wName, 95, taskNames)
		})
		AfterEach(func() {
			cleanupWave(wName, taskNames)
		})

		It("verifies taskToWaveMapper enqueues the correct Wave when a Task changes", func() {
			r := newWaveReconciler()

			var task tideprojectv1alpha3.Task
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: taskNames[0], Namespace: "default"}, &task)).To(Succeed())

			// Call the mapper directly — it should return a request for our Wave.
			reqs := r.taskToWaveMapper(ctx, &task)
			Expect(reqs).NotTo(BeEmpty())

			found := false
			for _, req := range reqs {
				if req.Name == wName && req.Namespace == "default" {
					found = true
				}
			}
			Expect(found).To(BeTrue(),
				"taskToWaveMapper should return a request for wave %s", wName)
		})
	})
})

// ensure time import is used
var _ = time.Now
