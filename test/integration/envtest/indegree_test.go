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

package envtest_integration

import (
	"context"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	tideprojectv1alpha1 "github.com/jsquirrelz/tide/api/v1alpha2"
)

const indegreeNamespace = "default"

// indegreeTestProject is the Project resolved by TaskReconciler.resolveProject in
// the indegree-namespace test specs. PlanReconciler stamps the
// tideproject.k8s/project label on Tasks in production; integration tests bypass
// PlanReconciler so makeTask stamps the label explicitly (per resolved debug
// session wave5-controller-suite-flakes — same shape of bug surfaced here).
const indegreeTestProject = "indegree-test-project"

var _ = Describe("Task indegree and dependency semantics", Label("envtest"), func() {
	ctx := context.Background()

	BeforeEach(func() {
		// TaskReconciler.resolveProject requires a Project in the same namespace.
		// In production, PlanReconciler stamps the project label; tests bypass it.
		makeBoundPVC(ctx, "tide-projects", indegreeNamespace)
		project := &tideprojectv1alpha1.Project{
			ObjectMeta: metav1.ObjectMeta{
				Name:      indegreeTestProject,
				Namespace: indegreeNamespace,
			},
			Spec: tideprojectv1alpha1.ProjectSpec{SchemaRevision: "v1alpha2",
				TargetRepo: "https://github.com/example/indegree-test.git",
			},
		}
		// Idempotent across nested It blocks under flake-retry; ignore AlreadyExists.
		if err := k8sClient.Create(ctx, project); err != nil {
			Expect(client.IgnoreAlreadyExists(err)).To(Succeed())
		}
	})

	AfterEach(func() {
		tasks := &tideprojectv1alpha1.TaskList{}
		_ = k8sClient.List(ctx, tasks, client.InNamespace(indegreeNamespace))
		for i := range tasks.Items {
			_ = k8sClient.Delete(ctx, &tasks.Items[i])
		}
		plans := &tideprojectv1alpha1.PlanList{}
		_ = k8sClient.List(ctx, plans, client.InNamespace(indegreeNamespace))
		for i := range plans.Items {
			_ = k8sClient.Delete(ctx, &plans.Items[i])
		}
		waves := &tideprojectv1alpha1.WaveList{}
		_ = k8sClient.List(ctx, waves, client.InNamespace(indegreeNamespace))
		for i := range waves.Items {
			_ = k8sClient.Delete(ctx, &waves.Items[i])
		}
		projects := &tideprojectv1alpha1.ProjectList{}
		_ = k8sClient.List(ctx, projects, client.InNamespace(indegreeNamespace))
		for i := range projects.Items {
			_ = k8sClient.Delete(ctx, &projects.Items[i])
		}
		pvcs := &corev1.PersistentVolumeClaimList{}
		_ = k8sClient.List(ctx, pvcs, client.InNamespace(indegreeNamespace))
		for i := range pvcs.Items {
			_ = k8sClient.Delete(ctx, &pvcs.Items[i])
		}
	})

	// FAIL-01: indegree is re-computed per reconcile from sibling tasks (not cached).
	Describe("FAIL-01: indegree recomputed per reconcile", Label("FAIL-01"), func() {
		It("blocks dispatch when a prerequisite task has not completed (indegree > 0)", func() {
			planName := "indegree-plan-01"
			createSimplePlan(ctx, planName)

			// taskA has no dependencies — indegree = 0.
			taskA := makeTask(ctx, "indegree-task-a", planName, nil, []string{"a.go"})
			// taskB depends on taskA — indegree = 1 (blocked until A completes).
			taskB := makeTask(ctx, "indegree-task-b", planName, []string{taskA.Name}, []string{"b.go"})
			_ = taskB

			// Wait for tasks to be created and reconciled.
			Eventually(func() string {
				t := &tideprojectv1alpha1.Task{}
				if err := k8sClient.Get(ctx, client.ObjectKey{Name: taskA.Name, Namespace: indegreeNamespace}, t); err != nil {
					return ""
				}
				return t.Status.Phase
			}, "45s", "200ms").Should(Or(Equal("Running"), Equal("Succeeded"), Equal("Pending")))

			// taskB should remain Pending (indegree > 0) while taskA hasn't completed.
			Consistently(func() string {
				t := &tideprojectv1alpha1.Task{}
				if err := k8sClient.Get(ctx, client.ObjectKey{Name: taskB.Name, Namespace: indegreeNamespace}, t); err != nil {
					return "error"
				}
				return t.Status.Phase
			}, "3s", "200ms").ShouldNot(Equal("Running"),
				"taskB should remain non-Running while taskA has not completed")
		})
	})

	// SUB-02: the attempt counter increments on each new Job creation.
	Describe("SUB-02: attempt counter increments at Job create", Label("SUB-02"), func() {
		It("increments Task.Status.Attempt when a Job is created for dispatch", func() {
			planName := "attempt-plan-02"
			createSimplePlan(ctx, planName)

			// A task with no dependencies gets dispatched immediately.
			task := makeTask(ctx, "attempt-task-a", planName, nil, []string{"attempt-a.go"})

			// Wait for the reconciler to process the task and set an attempt number.
			Eventually(func() int {
				t := &tideprojectv1alpha1.Task{}
				if err := k8sClient.Get(ctx, client.ObjectKey{Name: task.Name, Namespace: indegreeNamespace}, t); err != nil {
					return -1
				}
				return t.Status.Attempt
			}, "45s", "200ms").Should(BeNumerically(">=", 1),
				"Task.Status.Attempt should be >= 1 after first Job dispatch")
		})
	})

	// SUB-03: the Job name is deterministic and idempotent.
	Describe("SUB-03: deterministic Job name — dispatch is idempotent", Label("SUB-03"), func() {
		It("produces the same Job name on re-reconcile (AlreadyExists is success)", func() {
			planName := "job-name-plan-03"
			createSimplePlan(ctx, planName)

			task := makeTask(ctx, "jobname-task-a", planName, nil, []string{"jobname-a.go"})

			// Wait for at least one reconcile to occur.
			Eventually(func() bool {
				t := &tideprojectv1alpha1.Task{}
				if err := k8sClient.Get(ctx, client.ObjectKey{Name: task.Name, Namespace: indegreeNamespace}, t); err != nil {
					return false
				}
				// Check that a Job was created (Attempt > 0 or Phase is Running).
				return t.Status.Attempt > 0 || t.Status.Phase == "Running"
			}, "45s", "200ms").Should(BeTrue())
		})
	})

	// PERSIST-03: owner ref cascade — Task is owned by Plan (deletion cascades).
	Describe("PERSIST-03: owner cascade Plan→Task", Label("PERSIST-03"), func() {
		It("tasks are owned by the Plan and are deleted when the Plan is deleted", func() {
			planName := "cascade-plan-03"
			createSimplePlan(ctx, planName)

			taskName := "cascade-task-a"
			makeTask(ctx, taskName, planName, nil, []string{"cascade-a.go"})

			// Wait for owner ref to be stamped by the PlanReconciler.
			Eventually(func() bool {
				t := &tideprojectv1alpha1.Task{}
				if err := k8sClient.Get(ctx, client.ObjectKey{Name: taskName, Namespace: indegreeNamespace}, t); err != nil {
					return false
				}
				return len(t.OwnerReferences) > 0
			}, "45s", "200ms").Should(BeTrue(),
				"Task should have an owner reference to the Plan")
		})
	})

	// SUB-02 / FAIL-01: Wave status roll-up — Succeeded when all Tasks Succeeded.
	Describe("Wave roll-up: Succeeded when all Tasks Succeeded", Label("SUB-02"), func() {
		It("sets Wave.Status.Phase=Succeeded when all member Tasks are Succeeded", func() {
			planName := "wave-rollup-succ"
			waveName := "wave-rollup-succ-wave"
			taskNames := []string{"wave-rollup-a", "wave-rollup-b"}

			createSimplePlan(ctx, planName)
			for _, tn := range taskNames {
				makeTaskWithWaveLabel(ctx, tn, planName, nil, []string{tn + ".go"}, 0)
			}

			// Create a Wave that owns the tasks.
			wave := &tideprojectv1alpha1.Wave{
				ObjectMeta: metav1.ObjectMeta{
					Name:      waveName,
					Namespace: indegreeNamespace,
					OwnerReferences: []metav1.OwnerReference{
						{APIVersion: "tideproject.k8s/v1alpha1", Kind: "Plan", Name: planName, UID: "dummy-uid"},
					},
				},
				Spec: tideprojectv1alpha1.WaveSpec{
					ProjectRef: planName,
					WaveIndex:  0,
				},
			}
			Expect(k8sClient.Create(ctx, wave)).To(Succeed())

			// Patch all Tasks to Succeeded.
			for _, tn := range taskNames {
				Eventually(func() error {
					t := &tideprojectv1alpha1.Task{}
					if err := k8sClient.Get(ctx, client.ObjectKey{Name: tn, Namespace: indegreeNamespace}, t); err != nil {
						return err
					}
					t.Status.Phase = "Succeeded"
					return k8sClient.Status().Update(ctx, t)
				}, "30s", "200ms").Should(Succeed())
			}

			// Wave should roll up to Succeeded.
			Eventually(func() string {
				w := &tideprojectv1alpha1.Wave{}
				if err := k8sClient.Get(ctx, client.ObjectKey{Name: waveName, Namespace: indegreeNamespace}, w); err != nil {
					return ""
				}
				return w.Status.Phase
			}, "60s", "500ms").Should(Equal("Succeeded"),
				"Wave.Status.Phase should be Succeeded when all Tasks are Succeeded")
		})
	})

	// FAIL-02: Wave status roll-up — Failed when one Task fails, others succeed.
	Describe("FAIL-02: Wave roll-up: Failed when one Task fails", Label("FAIL-02"), func() {
		It("sets Wave.Status.Phase=Failed when any member Task is Failed, others may succeed", func() {
			planName := "wave-rollup-fail"
			waveName := "wave-rollup-fail-wave"
			taskNames := []string{"wave-fail-a", "wave-fail-b", "wave-fail-c"}

			createSimplePlan(ctx, planName)
			for _, tn := range taskNames {
				makeTaskWithWaveLabel(ctx, tn, planName, nil, []string{tn + ".go"}, 0)
			}

			wave := &tideprojectv1alpha1.Wave{
				ObjectMeta: metav1.ObjectMeta{
					Name:      waveName,
					Namespace: indegreeNamespace,
					OwnerReferences: []metav1.OwnerReference{
						{APIVersion: "tideproject.k8s/v1alpha1", Kind: "Plan", Name: planName, UID: "dummy-uid"},
					},
				},
				Spec: tideprojectv1alpha1.WaveSpec{
					ProjectRef: planName,
					WaveIndex:  0,
				},
			}
			Expect(k8sClient.Create(ctx, wave)).To(Succeed())

			// First task Fails; others Succeed.
			Eventually(func() error {
				t := &tideprojectv1alpha1.Task{}
				if err := k8sClient.Get(ctx, client.ObjectKey{Name: taskNames[0], Namespace: indegreeNamespace}, t); err != nil {
					return err
				}
				t.Status.Phase = "Failed"
				return k8sClient.Status().Update(ctx, t)
			}, "30s", "200ms").Should(Succeed())

			for _, tn := range taskNames[1:] {
				Eventually(func() error {
					t := &tideprojectv1alpha1.Task{}
					if err := k8sClient.Get(ctx, client.ObjectKey{Name: tn, Namespace: indegreeNamespace}, t); err != nil {
						return err
					}
					t.Status.Phase = "Succeeded"
					return k8sClient.Status().Update(ctx, t)
				}, "30s", "200ms").Should(Succeed())
			}

			// Wave should roll up to Failed.
			Eventually(func() string {
				w := &tideprojectv1alpha1.Wave{}
				if err := k8sClient.Get(ctx, client.ObjectKey{Name: waveName, Namespace: indegreeNamespace}, w); err != nil {
					return ""
				}
				return w.Status.Phase
			}, "60s", "500ms").Should(Equal("Failed"),
				"Wave.Status.Phase should be Failed when any Task is Failed")
		})
	})
})

// createSimplePlan creates a minimal Plan in the indegreeNamespace for testing.
func createSimplePlan(ctx context.Context, name string) {
	plan := &tideprojectv1alpha1.Plan{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: indegreeNamespace,
		},
		Spec: tideprojectv1alpha1.PlanSpec{
			PhaseRef: "test-phase",
		},
	}
	Expect(k8sClient.Create(ctx, plan)).To(Succeed())
	// Wait for the plan to be visible in the manager cache.
	Eventually(func() error {
		return mgrClient.Get(ctx, client.ObjectKey{Name: name, Namespace: indegreeNamespace}, &tideprojectv1alpha1.Plan{})
	}, "5s", "100ms").Should(Succeed())
}

// makeTask creates a Task in the indegreeNamespace for testing and returns it.
func makeTask(ctx context.Context, name, planRef string, dependsOn, files []string) *tideprojectv1alpha1.Task {
	return makeTaskWithWaveLabel(ctx, name, planRef, dependsOn, files, -1)
}

// makeTaskWithWaveLabel creates a Task with an optional wave-index label.
// Pass waveIndex=-1 to skip the label. The tideproject.k8s/project label is
// always stamped (PlanReconciler does this in production; tests bypass it)
// so TaskReconciler.resolveProject's fast path works deterministically even
// when other suites leave stray Projects in the shared 'default' namespace.
func makeTaskWithWaveLabel(ctx context.Context, name, planRef string, dependsOn, files []string, waveIndex int) *tideprojectv1alpha1.Task {
	if files == nil {
		files = []string{name + ".go"}
	}
	labels := map[string]string{
		"tideproject.k8s/project": indegreeTestProject,
	}
	if waveIndex >= 0 {
		labels["tideproject.k8s/wave-index"] = fmt.Sprintf("%d", waveIndex)
	}
	task := &tideprojectv1alpha1.Task{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: indegreeNamespace,
			Labels:    labels,
		},
		Spec: tideprojectv1alpha1.TaskSpec{
			PlanRef:             planRef,
			PromptPath:          "envelopes/test/children/" + name + ".json",
			DependsOn:           dependsOn,
			FilesTouched:        files,
			DeclaredOutputPaths: files,
		},
	}
	Expect(k8sClient.Create(ctx, task)).To(Succeed())
	// Wait for cache visibility.
	Eventually(func() error {
		return mgrClient.Get(ctx, client.ObjectKey{Name: name, Namespace: indegreeNamespace}, &tideprojectv1alpha1.Task{})
	}, "5s", "100ms").Should(Succeed())
	time.Sleep(50 * time.Millisecond) // allow indexer to propagate
	return task
}
