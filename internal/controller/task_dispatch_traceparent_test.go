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

// task_dispatch_traceparent_test.go — Plan 43-05 (PROP-01, Task's dispatch
// hop). Mirrors dispatch_image_test.go's "project-wide image pin ... at task
// dispatch" fixture shape: create a real Project + Plan + Task, drive a real
// Task dispatch through TaskReconciler.Reconcile, and fetch the created
// executor Job to assert its subagent container's TRACEPARENT env var.
//
// Deliberately a SEPARATE file from plan 43-04's dispatch_traceparent_test.go
// (which covers the four planner levels' dispatch-prep + reporter hops) so
// the two wave-3 plans never touch the same file (43-05-PLAN.md Task 2).

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

	tideprojectv1alpha3 "github.com/jsquirrelz/tide/api/v1alpha3"
	"github.com/jsquirrelz/tide/internal/dispatch/podjob"
	"github.com/jsquirrelz/tide/pkg/otelai"
)

var _ = Describe("Task dispatch-hop TRACEPARENT (PROP-01)", Label("envtest", "heavy"), func() {
	const (
		tptProjName = "task-traceparent-proj"
		tptPlanName = "task-traceparent-plan"
		tptTaskName = "task-traceparent-task"
	)

	ctx := context.Background()

	BeforeEach(func() {
		p := &tideprojectv1alpha3.Project{
			ObjectMeta: metav1.ObjectMeta{Name: tptProjName, Namespace: "default"},
			Spec: tideprojectv1alpha3.ProjectSpec{
				SchemaRevision: "v1alpha3",
				TargetRepo:     "https://github.com/example/tide.git",
			},
		}
		Expect(k8sClient.Create(ctx, p)).To(Succeed())
		waitForCacheSync(tptProjName, "default", &tideprojectv1alpha3.Project{})

		makePlan(tptPlanName, "nonexistent-phase", "Validated")

		// PROP-01: seed the Plan's own persisted span ID — the parent value
		// Task's dispatch-prep hop must read via traceparentForLevel.
		var plan tideprojectv1alpha3.Plan
		Expect(mgrClient.Get(ctx, types.NamespacedName{Name: tptPlanName, Namespace: "default"}, &plan)).To(Succeed())
		planPatch := client.MergeFrom(plan.DeepCopy())
		plan.Status.PlanTraceSpanID = seededParentSpanIDHex
		Expect(k8sClient.Status().Patch(ctx, &plan, planPatch)).To(Succeed())
		// Wait for the manager's cache (read by createDispatchJob's own Get)
		// to observe this write before driving dispatch below — same
		// cache-sync race guarded against throughout span_emission_test.go.
		Eventually(func(g Gomega) {
			var synced tideprojectv1alpha3.Plan
			g.Expect(mgrClient.Get(ctx, types.NamespacedName{Name: tptPlanName, Namespace: "default"}, &synced)).To(Succeed())
			g.Expect(synced.Status.PlanTraceSpanID).To(Equal(seededParentSpanIDHex))
		}, 5*time.Second, 50*time.Millisecond).Should(Succeed())
	})

	AfterEach(func() {
		task := &tideprojectv1alpha3.Task{}
		if err := k8sClient.Get(ctx, types.NamespacedName{Name: tptTaskName, Namespace: "default"}, task); err == nil {
			task.Finalizers = nil
			_ = k8sClient.Update(ctx, task)
			_ = k8sClient.Delete(ctx, task)
		}
		plan := &tideprojectv1alpha3.Plan{}
		if err := k8sClient.Get(ctx, types.NamespacedName{Name: tptPlanName, Namespace: "default"}, plan); err == nil {
			plan.Finalizers = nil
			_ = k8sClient.Update(ctx, plan)
			_ = k8sClient.Delete(ctx, plan)
		}
		cleanupProject(tptProjName)
		var jobs batchv1.JobList
		_ = k8sClient.List(ctx, &jobs, client.InNamespace("default"))
		for i := range jobs.Items {
			j := jobs.Items[i]
			_ = k8sClient.Delete(ctx, &j)
		}
	})

	It("subagent dispatch Job carries TRACEPARENT sourced from the parent Plan's persisted span ID", func() {
		t := &tideprojectv1alpha3.Task{
			ObjectMeta: metav1.ObjectMeta{
				Name:      tptTaskName,
				Namespace: "default",
				Labels:    map[string]string{"tideproject.k8s/project": tptProjName},
			},
			Spec: tideprojectv1alpha3.TaskSpec{
				PlanRef:             tptPlanName,
				FilesTouched:        []string{"src/main.go"},
				DeclaredOutputPaths: []string{"artifacts/out.txt"},
				PromptPath:          "envelopes/test/children/" + tptTaskName + ".json",
			},
		}
		Expect(k8sClient.Create(ctx, t)).To(Succeed())
		waitForCacheSync(tptTaskName, "default", &tideprojectv1alpha3.Task{})

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
				HelmProviderDefaults: ProviderDefaults{
					Image: testSubagentImage,
				},
			},
		}

		_ = reconcileWithRetry(r.Reconcile, types.NamespacedName{Name: tptTaskName, Namespace: "default"}, 4)

		var proj tideprojectv1alpha3.Project
		Expect(mgrClient.Get(ctx, types.NamespacedName{Name: tptProjName, Namespace: "default"}, &proj)).To(Succeed())
		expectedTID, tErr := otelai.TraceIDFromUID(string(proj.UID))
		Expect(tErr).NotTo(HaveOccurred())
		expectedTraceParent := fmt.Sprintf("00-%s-%s-01", expectedTID.String(), seededParentSpanIDHex)

		Eventually(func(g Gomega) {
			var got tideprojectv1alpha3.Task
			g.Expect(mgrClient.Get(ctx, types.NamespacedName{Name: tptTaskName, Namespace: "default"}, &got)).To(Succeed())
			jobName := podjob.JobName(got.UID, got.Status.Attempt)
			var job batchv1.Job
			g.Expect(mgrClient.Get(ctx, types.NamespacedName{Name: jobName, Namespace: "default"}, &job)).To(Succeed())
			g.Expect(job.Spec.Template.Spec.Containers).NotTo(BeEmpty())
			subagent := job.Spec.Template.Spec.Containers[0]
			var gotVal string
			var found bool
			for _, e := range subagent.Env {
				if e.Name == "TRACEPARENT" {
					found = true
					gotVal = e.Value
				}
			}
			g.Expect(found).To(BeTrue(), "subagent container missing TRACEPARENT env var")
			g.Expect(gotVal).To(Equal(expectedTraceParent))
		}, 5*time.Second, 100*time.Millisecond).Should(Succeed())
	})
})
