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

// Phase 04.1 P1.4 — envtest Layer A spec for the ParentUnresolved condition.
//
// Verifies that a Task with NO tideproject.k8s/project label and NO owner-ref
// chain in a namespace containing TWO Projects:
//   - gets ConditionParentUnresolved=True (not silently adopted by either Project)
//   - never gets a Job created for it (no silent dispatch)
//
// This closes the multi-Project mis-routing bug class: the prior
// `projectList.Items[0]` fallback would adopt whichever Project sorted first,
// dispatching the Task against the wrong Project. The new owner-chain walk
// returns ErrParentUnresolved on miss; the reconciler sets the condition and
// requeues after 30s without dispatching.
//
// Test design notes:
//   - Uses `default` namespace (the shared envtest namespace) to avoid needing
//     a namespace provisioner; the two Projects use unique names prefixed with
//     "pu-" (parent-unresolved) to avoid collisions with other specs.
//   - Does NOT set the tideproject.k8s/project label on the Task — that's the
//     regression input.
//   - Does NOT wire owner refs from the Task to any Project — belt-and-suspenders
//     regression input for the owner-chain walk.
//   - Uses the live manager (mgrClient) for Eventually assertions so the
//     running TaskReconciler processes the Task normally.

import (
	"context"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	batchv1 "k8s.io/api/batch/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	tideprojectv1alpha2 "github.com/jsquirrelz/tide/api/v1alpha2"
)

var _ = Describe("Phase 04.1 P1.4 — ParentUnresolved condition", Label("envtest", "phase04.1", "parent-unresolved"), func() {
	ctx := context.Background()

	// Use a test-run-unique suffix to avoid collision with other specs that
	// create Tasks in the shared `default` namespace.
	const ns = "default"
	const taskName = "pu-orphan-task"
	const projectA = "pu-project-a"
	const projectB = "pu-project-b"

	BeforeEach(func() {
		// Two Projects in default — the prior projectList.Items[0] fallback would
		// silently adopt whichever sorted first. Create-or-wait (helpers_test.go):
		// idempotent AND safe against this Describe's own AfterEach deletion still
		// terminating when the next It's BeforeEach runs.
		for _, name := range []string{projectA, projectB} {
			ensureLiveProject(ctx, &tideprojectv1alpha2.Project{
				ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns},
				Spec: tideprojectv1alpha2.ProjectSpec{SchemaRevision: "v1alpha2",
					TargetRepo: fmt.Sprintf("https://github.com/example/%s.git", name),
				},
			})
		}

		// Task with NO tideproject.k8s/project label and NO owner-ref to either Project.
		// This is the regression input: before P1.4, the reconciler would have found
		// projectList.Items[0] and dispatched against it. After P1.4, it sets the condition.
		task := &tideprojectv1alpha2.Task{
			ObjectMeta: metav1.ObjectMeta{
				Name:      taskName,
				Namespace: ns,
				// Intentionally NO tideproject.k8s/project label.
			},
			Spec: tideprojectv1alpha2.TaskSpec{
				PlanRef:             "pu-plan",
				PromptPath:          "envelopes/test/children/" + taskName + ".json",
				FilesTouched:        []string{"pu.go"},
				DeclaredOutputPaths: []string{"pu.go"},
			},
		}
		Expect(k8sClient.Create(ctx, task)).To(Succeed())
	})

	AfterEach(func() {
		task := &tideprojectv1alpha2.Task{}
		if err := k8sClient.Get(ctx, types.NamespacedName{Name: taskName, Namespace: ns}, task); err == nil {
			task.Finalizers = nil
			_ = k8sClient.Update(ctx, task)
			_ = k8sClient.Delete(ctx, task)
		}
		for _, name := range []string{projectA, projectB} {
			proj := &tideprojectv1alpha2.Project{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: ns}, proj); err == nil {
				proj.Finalizers = nil
				_ = k8sClient.Update(ctx, proj)
				_ = k8sClient.Delete(ctx, proj)
			}
		}
	})

	It("sets ConditionParentUnresolved=True on an unlabeled Task in a 2-Project namespace", func() {
		// Eventually the running TaskReconciler should set ConditionParentUnresolved=True.
		Eventually(func() bool {
			var fetched tideprojectv1alpha2.Task
			if err := mgrClient.Get(ctx, types.NamespacedName{Namespace: ns, Name: taskName}, &fetched); err != nil {
				return false
			}
			cond := meta.FindStatusCondition(fetched.Status.Conditions, tideprojectv1alpha2.ConditionParentUnresolved)
			return cond != nil && cond.Status == metav1.ConditionTrue
		}, 30*time.Second, 1*time.Second).Should(BeTrue(),
			"Task should have ConditionParentUnresolved=True within 30s")

		// Verify the condition Reason is set correctly.
		var fetched tideprojectv1alpha2.Task
		Expect(mgrClient.Get(ctx, types.NamespacedName{Namespace: ns, Name: taskName}, &fetched)).To(Succeed())
		cond := meta.FindStatusCondition(fetched.Status.Conditions, tideprojectv1alpha2.ConditionParentUnresolved)
		Expect(cond).NotTo(BeNil())
		Expect(cond.Reason).To(Equal(tideprojectv1alpha2.ReasonNoProjectLabel),
			"Reason should be NoProjectLabel when label is absent")

		// No Job should have been created for this Task — the reconciler short-circuits
		// before dispatch when the parent is unresolved.
		Consistently(func() int {
			var jobs batchv1.JobList
			_ = k8sClient.List(ctx, &jobs, client.InNamespace(ns))
			count := 0
			for _, j := range jobs.Items {
				// Only count Jobs that belong to this Task (check owner refs or name pattern).
				for _, ref := range j.OwnerReferences {
					if ref.Kind == "Task" && ref.Name == taskName {
						count++
					}
				}
			}
			return count
		}, 5*time.Second, 500*time.Millisecond).Should(Equal(0),
			"No Job should be created for an unlabeled Task (ParentUnresolved)")
	})
})
