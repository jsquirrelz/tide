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

// task_traceonly_reporter_test.go — Plan 44-05 Task 2 (MSG-01). Proves
// spawnTaskTraceReporterIfNeeded's envtest behavior: the trace-only reporter
// Job fires with the right shape when an OTLP endpoint is configured, never
// fires when it isn't (D-06), and never perturbs Task's own terminal-state
// machinery. Deliberately a SEPARATE file from span_emission_test.go's
// "SpanEmission — Task level" block and task_dispatch_traceparent_test.go
// (file-disjointness convention) — reuses their fixture helpers
// (spanEmissionProject, succeededPlannerJob, newMapEnvReader, etc.) rather
// than duplicating them.
package controller

import (
	"context"
	"fmt"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	oteltrace "go.opentelemetry.io/otel/trace"

	batchv1 "k8s.io/api/batch/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	tideprojectv1alpha3 "github.com/jsquirrelz/tide/api/v1alpha3"
	pkgdispatch "github.com/jsquirrelz/tide/pkg/dispatch"
	"github.com/jsquirrelz/tide/pkg/otelai"
)

var _ = Describe("Task trace-only reporter spawn", Label("envtest", "heavy"), func() {
	ctx := context.Background()

	const (
		ttrProjName = "task-traceonly-proj"
		ttrMSName   = "task-traceonly-ms"
		ttrPhName   = "task-traceonly-ph"
		ttrPlanName = "task-traceonly-plan"
		ttrTaskName = "task-traceonly-task"

		testTTRReporterImage = "tide-reporter:test"
		testTTROTLPEndpoint  = "otel-collector.tide-system.svc:4317"
	)

	var (
		envReader *mapEnvReader
		exp       *tracetest.InMemoryExporter
		prevTP    oteltrace.TracerProvider
	)

	BeforeEach(func() {
		// Swap in an in-memory exporter FIRST (mirrors span_emission_test.go's
		// Task-level BeforeEach, 42-REVIEW WR-04) — spec 1 needs to observe the
		// Task's own span (minted by emitTaskSpanOnce, called immediately
		// before spawnTaskTraceReporterIfNeeded in the same handleJobCompletion
		// call) to assert the spawned Job's --traceparent Arg against it.
		exp = tracetest.NewInMemoryExporter()
		prevTP = otel.GetTracerProvider()
		otel.SetTracerProvider(sdktrace.NewTracerProvider(sdktrace.WithSyncer(exp)))

		spanEmissionProject(ctx, ttrProjName, "claude-test-model")
		ms := &tideprojectv1alpha3.Milestone{
			ObjectMeta: metav1.ObjectMeta{Name: ttrMSName, Namespace: "default"},
			Spec:       tideprojectv1alpha3.MilestoneSpec{ProjectRef: ttrProjName},
		}
		Expect(k8sClient.Create(ctx, ms)).To(Succeed())
		waitForCacheSync(ttrMSName, "default", &tideprojectv1alpha3.Milestone{})
		ph := &tideprojectv1alpha3.Phase{
			ObjectMeta: metav1.ObjectMeta{Name: ttrPhName, Namespace: "default"},
			Spec:       tideprojectv1alpha3.PhaseSpec{MilestoneRef: ttrMSName},
		}
		Expect(k8sClient.Create(ctx, ph)).To(Succeed())
		waitForCacheSync(ttrPhName, "default", &tideprojectv1alpha3.Phase{})
		envReader = newMapEnvReader()
	})

	AfterEach(func() {
		otel.SetTracerProvider(prevTP)

		task := &tideprojectv1alpha3.Task{}
		if err := k8sClient.Get(ctx, types.NamespacedName{Name: ttrTaskName, Namespace: "default"}, task); err == nil {
			task.Finalizers = nil
			_ = k8sClient.Update(ctx, task)
			_ = k8sClient.Delete(ctx, task)
		}
		plan := &tideprojectv1alpha3.Plan{}
		if err := k8sClient.Get(ctx, types.NamespacedName{Name: ttrPlanName, Namespace: "default"}, plan); err == nil {
			plan.Finalizers = nil
			_ = k8sClient.Update(ctx, plan)
			_ = k8sClient.Delete(ctx, plan)
		}
		ph := &tideprojectv1alpha3.Phase{}
		if err := k8sClient.Get(ctx, types.NamespacedName{Name: ttrPhName, Namespace: "default"}, ph); err == nil {
			ph.Finalizers = nil
			_ = k8sClient.Update(ctx, ph)
			_ = k8sClient.Delete(ctx, ph)
		}
		ms := &tideprojectv1alpha3.Milestone{}
		if err := k8sClient.Get(ctx, types.NamespacedName{Name: ttrMSName, Namespace: "default"}, ms); err == nil {
			ms.Finalizers = nil
			_ = k8sClient.Update(ctx, ms)
			_ = k8sClient.Delete(ctx, ms)
		}
		cleanupSpanEmissionProject(ctx, ttrProjName)

		var jobs batchv1.JobList
		_ = k8sClient.List(ctx, &jobs, client.InNamespace("default"))
		for i := range jobs.Items {
			j := jobs.Items[i]
			_ = k8sClient.Delete(ctx, &j)
		}
	})

	createTTRPlan := func() *tideprojectv1alpha3.Plan {
		plan := &tideprojectv1alpha3.Plan{
			ObjectMeta: metav1.ObjectMeta{Name: ttrPlanName, Namespace: "default"},
			Spec:       tideprojectv1alpha3.PlanSpec{PhaseRef: ttrPhName},
		}
		Expect(k8sClient.Create(ctx, plan)).To(Succeed())
		waitForCacheSync(ttrPlanName, "default", &tideprojectv1alpha3.Plan{})
		Expect(mgrClient.Get(ctx, types.NamespacedName{Name: ttrPlanName, Namespace: "default"}, plan)).To(Succeed())
		return plan
	}

	createTTRTask := func() *tideprojectv1alpha3.Task {
		task := &tideprojectv1alpha3.Task{
			ObjectMeta: metav1.ObjectMeta{Name: ttrTaskName, Namespace: "default"},
			Spec: tideprojectv1alpha3.TaskSpec{
				PlanRef:             ttrPlanName,
				FilesTouched:        []string{"src/main.go"},
				PromptPath:          "envelopes/test/children/" + ttrTaskName + ".json",
				DeclaredOutputPaths: []string{"artifacts/out.txt"},
			},
		}
		Expect(k8sClient.Create(ctx, task)).To(Succeed())
		waitForCacheSync(ttrTaskName, "default", &tideprojectv1alpha3.Task{})
		Expect(mgrClient.Get(ctx, types.NamespacedName{Name: ttrTaskName, Namespace: "default"}, task)).To(Succeed())

		statusPatch := client.MergeFrom(task.DeepCopy())
		task.Status.Phase = "Running"
		Expect(k8sClient.Status().Patch(ctx, task, statusPatch)).To(Succeed())
		Expect(mgrClient.Get(ctx, types.NamespacedName{Name: ttrTaskName, Namespace: "default"}, task)).To(Succeed())
		return task
	}

	newTTRReconciler := func(otlpEndpoint string) *TaskReconciler {
		return &TaskReconciler{
			Client: mgrClient,
			Scheme: k8sClient.Scheme(),
			Deps: TaskReconcilerDeps{
				Dispatcher:     &stubDispatcher{},
				EnvReader:      envReader,
				CredproxyImage: testCredproxyImage,
				SigningKey:     testSigningKey,
				HelmProviderDefaults: ProviderDefaults{
					Image: testSubagentImage,
				},
				ReporterImage: testTTRReporterImage,
				OTLPEndpoint:  otlpEndpoint,
			},
		}
	}

	It("spawns a trace-only reporter Job with the right shape when the endpoint is configured", func() {
		createTTRPlan()
		task := createTTRTask()

		var proj tideprojectv1alpha3.Project
		Expect(mgrClient.Get(ctx, types.NamespacedName{Name: ttrProjName, Namespace: "default"}, &proj)).To(Succeed())

		start := time.Now().Add(-5 * time.Minute)
		end := time.Now()
		// Short, deterministic Job name/UID — NOT derived from task.UID: the
		// reporter Job this spawns is named "tide-reporter-trace-"+completedJob.UID,
		// and Kubernetes' auto-injected job-name label caps the Job's own name
		// at 63 bytes. Real dispatch Jobs get a server-assigned 36-char UUID
		// (well under the cap); this fixture must stay short for the same reason.
		job := succeededPlannerJob("ttr-shape-job", start, end)

		envReader.SetOut(string(task.UID), pkgdispatch.EnvelopeOut{
			TaskUID:     string(task.UID),
			Result:      "success",
			CompletedAt: end,
		})

		r := newTTRReconciler(testTTROTLPEndpoint)
		_, err := r.handleJobCompletion(ctx, task, &proj, job)
		Expect(err).NotTo(HaveOccurred())

		// emitTaskSpanOnce (called immediately before spawnTaskTraceReporterIfNeeded
		// at this same call site) mints the Task's OWN span for this attempt and
		// mirrors its SpanID onto task.Status.TaskTraceSpanID in-memory — the exact
		// value spawnTaskTraceReporterIfNeeded reads via traceparentForLevel. Capture
		// it from the exporter (same technique as span_emission_test.go's Task-level
		// spec) rather than pre-seeding a constant, since THIS reconcile mints it.
		spans := exp.GetSpans()
		Expect(spans).To(HaveLen(1), "emitTaskSpanOnce must have emitted exactly one span for this attempt")
		emittedSpanID := spans[0].SpanContext.SpanID()

		expectedTID, tErr := otelai.TraceIDFromUID(string(proj.UID))
		Expect(tErr).NotTo(HaveOccurred())
		expectedTraceParentArg := fmt.Sprintf("--traceparent=00-%s-%s-01", expectedTID.String(), emittedSpanID.String())

		wantJobName := "tide-reporter-trace-" + string(job.UID)
		Eventually(func(g Gomega) {
			var got batchv1.Job
			g.Expect(mgrClient.Get(ctx, types.NamespacedName{Name: wantJobName, Namespace: "default"}, &got)).To(Succeed())

			g.Expect(got.Labels["tideproject.k8s/role"]).To(Equal("reporter"))

			ownerFound := false
			for _, ref := range got.OwnerReferences {
				if ref.UID == task.UID {
					ownerFound = true
				}
			}
			g.Expect(ownerFound).To(BeTrue(), "expected owner ref to the Task, got %v", got.OwnerReferences)

			g.Expect(got.Spec.Template.Spec.Containers).NotTo(BeEmpty())
			c := got.Spec.Template.Spec.Containers[0]

			g.Expect(c.Args).To(ContainElement("--trace-only"))
			g.Expect(c.Args).To(ContainElement("--task-uid=" + string(task.UID)))
			g.Expect(c.Args).To(ContainElement(expectedTraceParentArg))
			for _, a := range c.Args {
				g.Expect(strings.HasPrefix(a, "--parent-name")).To(BeFalse(), "trace-only Args must omit --parent-name, got %v", c.Args)
			}

			var gotEndpoint, gotBatchSize string
			for _, e := range c.Env {
				if e.Name == "OTEL_EXPORTER_OTLP_ENDPOINT" {
					gotEndpoint = e.Value
				}
				if e.Name == "OTEL_BSP_MAX_EXPORT_BATCH_SIZE" {
					gotBatchSize = e.Value
				}
			}
			g.Expect(gotEndpoint).To(Equal(testTTROTLPEndpoint))
			g.Expect(gotBatchSize).To(Equal("6"))
		}, 5*time.Second, 100*time.Millisecond).Should(Succeed())
	})

	It("never spawns a trace-only reporter Job when no OTLP endpoint is configured (D-06)", func() {
		createTTRPlan()
		task := createTTRTask()

		var proj tideprojectv1alpha3.Project
		Expect(mgrClient.Get(ctx, types.NamespacedName{Name: ttrProjName, Namespace: "default"}, &proj)).To(Succeed())

		start := time.Now().Add(-5 * time.Minute)
		end := time.Now()
		job := succeededPlannerJob("ttr-noendpoint-job", start, end)

		envReader.SetOut(string(task.UID), pkgdispatch.EnvelopeOut{
			TaskUID:     string(task.UID),
			Result:      "success",
			CompletedAt: end,
		})

		r := newTTRReconciler("") // D-06: empty OTLPEndpoint — ReporterImage still set.
		_, err := r.handleJobCompletion(ctx, task, &proj, job)
		Expect(err).NotTo(HaveOccurred())

		wantJobName := "tide-reporter-trace-" + string(job.UID)
		Consistently(func(g Gomega) {
			var got batchv1.Job
			getErr := mgrClient.Get(ctx, types.NamespacedName{Name: wantJobName, Namespace: "default"}, &got)
			g.Expect(apierrors.IsNotFound(getErr)).To(BeTrue(), "expected no trace-only Job when OTLPEndpoint is empty (D-06), got err=%v", getErr)
		}, 2*time.Second, 100*time.Millisecond).Should(Succeed())
	})

	It("does not perturb the Task's own terminal-state machinery", func() {
		createTTRPlan()
		task := createTTRTask()

		var proj tideprojectv1alpha3.Project
		Expect(mgrClient.Get(ctx, types.NamespacedName{Name: ttrProjName, Namespace: "default"}, &proj)).To(Succeed())

		start := time.Now().Add(-5 * time.Minute)
		end := time.Now()
		job := succeededPlannerJob("ttr-noninterfere-job", start, end)

		envReader.SetOut(string(task.UID), pkgdispatch.EnvelopeOut{
			TaskUID:     string(task.UID),
			Result:      "success",
			CompletedAt: end,
		})

		r := newTTRReconciler(testTTROTLPEndpoint)
		_, err := r.handleJobCompletion(ctx, task, &proj, job)
		Expect(err).NotTo(HaveOccurred())

		// The additive trace-only spawn must not perturb the Task's own
		// terminal phase — role=reporter discriminates the spawned Job from
		// the dispatch Job (T-09-13 precedent), so nothing about the spawned
		// Job's own (never-observed, still-Pending) lifecycle can flip Task
		// state.
		wantJobName := "tide-reporter-trace-" + string(job.UID)
		Eventually(func(g Gomega) {
			var got batchv1.Job
			g.Expect(mgrClient.Get(ctx, types.NamespacedName{Name: wantJobName, Namespace: "default"}, &got)).To(Succeed())
		}, 5*time.Second, 100*time.Millisecond).Should(Succeed())

		var fresh tideprojectv1alpha3.Task
		Expect(mgrClient.Get(ctx, types.NamespacedName{Name: ttrTaskName, Namespace: "default"}, &fresh)).To(Succeed())
		Expect(fresh.Status.Phase).To(Equal(tideprojectv1alpha3.LevelPhaseSucceeded))
	})
})
