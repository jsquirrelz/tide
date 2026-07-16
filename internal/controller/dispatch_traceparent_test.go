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

// dispatch_traceparent_test.go — envtest proof for plan 43-04 (PROP-01): both
// TRACEPARENT propagation hops carry exact W3C strings end-to-end.
//
// Spec 1/2 exercise the DISPATCH-PREP hop at the Plan level — Plan is the
// level whose immediate-parent (Phase) fetch is genuinely new plumbing this
// plan adds (Milestone's parent-Project fetch is free; Phase/Plan's are not).
// Spec 3 exercises the REPORTER hop at the Milestone level, reusing
// span_emission_test.go's direct-handler-call fixture shape.
package controller

import (
	"context"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	oteltrace "go.opentelemetry.io/otel/trace"

	batchv1 "k8s.io/api/batch/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	tideprojectv1alpha3 "github.com/jsquirrelz/tide/api/v1alpha3"
	pkgdispatch "github.com/jsquirrelz/tide/pkg/dispatch"
	"github.com/jsquirrelz/tide/pkg/otelai"
)

// jobEnvValue searches every container (regular and init) in the Job's pod
// template for an env var named name. Returns ("", false) when absent —
// callers use the ok bool to assert omission, not just an empty string
// (BuildJobSpec omits the var entirely for an empty TraceParent, per D-06).
func jobEnvValue(job *batchv1.Job, name string) (string, bool) {
	for _, c := range job.Spec.Template.Spec.Containers {
		for _, e := range c.Env {
			if e.Name == name {
				return e.Value, true
			}
		}
	}
	for _, c := range job.Spec.Template.Spec.InitContainers {
		for _, e := range c.Env {
			if e.Name == name {
				return e.Value, true
			}
		}
	}
	return "", false
}

var _ = Describe("Dispatch traceparent propagation (PROP-01)", Label("envtest", "heavy"), func() {
	ctx := context.Background()

	// ─── Spec 1/2: dispatch-prep hop, Plan level ──────────────────────────

	Describe("dispatch hop at Plan level", func() {
		const (
			tpDispatchMSName  = "tp-dispatch-ms"
			tpDispatchPhName  = "tp-dispatch-ph"
			tpDispatchProject = "tp-dispatch-project"
			tpDispatchPlan    = "tp-dispatch-plan"
		)

		// makeChain creates Project → Milestone → Phase → Plan and returns the
		// hydrated Project (UID populated by Create) and Phase (for seeding
		// its persisted PhaseTraceSpanID before driving Plan dispatch).
		makeChain := func() (*tideprojectv1alpha3.Project, *tideprojectv1alpha3.Phase) {
			proj := makeProjectForTask(tpDispatchProject)

			ms := &tideprojectv1alpha3.Milestone{
				ObjectMeta: metav1.ObjectMeta{Name: tpDispatchMSName, Namespace: "default"},
				Spec:       tideprojectv1alpha3.MilestoneSpec{ProjectRef: tpDispatchProject},
			}
			Expect(k8sClient.Create(ctx, ms)).To(Succeed())
			waitForCacheSync(tpDispatchMSName, "default", &tideprojectv1alpha3.Milestone{})

			ph := &tideprojectv1alpha3.Phase{
				ObjectMeta: metav1.ObjectMeta{Name: tpDispatchPhName, Namespace: "default"},
				Spec:       tideprojectv1alpha3.PhaseSpec{MilestoneRef: tpDispatchMSName},
			}
			Expect(k8sClient.Create(ctx, ph)).To(Succeed())
			waitForCacheSync(tpDispatchPhName, "default", &tideprojectv1alpha3.Phase{})

			p := &tideprojectv1alpha3.Plan{
				ObjectMeta: metav1.ObjectMeta{Name: tpDispatchPlan, Namespace: "default"},
				Spec:       tideprojectv1alpha3.PlanSpec{PhaseRef: tpDispatchPhName},
			}
			Expect(k8sClient.Create(ctx, p)).To(Succeed())
			Eventually(func() error {
				return mgrClient.Get(ctx, types.NamespacedName{Name: tpDispatchPlan, Namespace: "default"}, &tideprojectv1alpha3.Plan{})
			}, 5*time.Second, 50*time.Millisecond).Should(Succeed())

			return proj, ph
		}

		cleanupChain := func() {
			plan := &tideprojectv1alpha3.Plan{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: tpDispatchPlan, Namespace: "default"}, plan); err == nil {
				plan.Finalizers = nil
				_ = k8sClient.Update(ctx, plan)
				_ = k8sClient.Delete(ctx, plan)
			}
			ph := &tideprojectv1alpha3.Phase{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: tpDispatchPhName, Namespace: "default"}, ph); err == nil {
				ph.Finalizers = nil
				_ = k8sClient.Update(ctx, ph)
				_ = k8sClient.Delete(ctx, ph)
			}
			ms := &tideprojectv1alpha3.Milestone{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: tpDispatchMSName, Namespace: "default"}, ms); err == nil {
				ms.Finalizers = nil
				_ = k8sClient.Update(ctx, ms)
				_ = k8sClient.Delete(ctx, ms)
			}
			cleanupProject(tpDispatchProject)
			var jobs batchv1.JobList
			_ = k8sClient.List(ctx, &jobs, client.InNamespace("default"))
			for i := range jobs.Items {
				j := jobs.Items[i]
				_ = k8sClient.Delete(ctx, &j)
			}
		}

		newPlanReconciler := func() *PlanReconciler {
			return &PlanReconciler{
				Client: mgrClient,
				Scheme: k8sClient.Scheme(),
				Deps: PlannerReconcilerDeps{
					Dispatcher:     &stubDispatcher{},
					EnvReader:      newMapEnvReader(),
					CredproxyImage: testCredproxyImage,
					SigningKey:     testSigningKey,
					HelmProviderDefaults: ProviderDefaults{
						Image: testSubagentImage,
					},
				},
				PlannerPool: newPlannerPoolForTest(),
			}
		}

		AfterEach(func() {
			cleanupChain()
		})

		It("carries the full W3C traceparent when the parent's span ID was persisted", func() {
			proj, ph := makeChain()

			// TRACE-02/PROP-02: seed the immediate parent's (Phase) persisted
			// span ID — the exact carrier plan 43-03 added, plan 43-04 threads.
			phPatch := client.MergeFrom(ph.DeepCopy())
			ph.Status.PhaseTraceSpanID = seededParentSpanIDHex
			Expect(k8sClient.Status().Patch(ctx, ph, phPatch)).To(Succeed())

			// Wait for the manager's cache-backed client to observe THIS specific
			// status field before driving dispatch — Plan's dispatch-prep reads
			// the parent Phase via r.Get (cache-backed), and the Job is created
			// only once (later reconcileWithRetry attempts hit the AlreadyExists
			// idempotent path without re-reading the parent). Without this guard
			// the spec would race the informer's watch delivery exactly like the
			// pre-existing cache-lag flake in span_emission_test.go:270.
			Eventually(func(g Gomega) {
				var fresh tideprojectv1alpha3.Phase
				g.Expect(mgrClient.Get(ctx, types.NamespacedName{Name: tpDispatchPhName, Namespace: "default"}, &fresh)).To(Succeed())
				g.Expect(fresh.Status.PhaseTraceSpanID).To(Equal(seededParentSpanIDHex))
			}, 5*time.Second, 50*time.Millisecond).Should(Succeed())

			expectedTraceID, err := otelai.TraceIDFromUID(string(proj.UID))
			Expect(err).NotTo(HaveOccurred())
			expectedTraceparent := fmt.Sprintf("00-%s-%s-01", expectedTraceID.String(), seededParentSpanIDHex)

			r := newPlanReconciler()
			Expect(reconcileWithRetry(r.Reconcile, types.NamespacedName{Name: tpDispatchPlan, Namespace: "default"}, 5)).To(Succeed())

			Eventually(func(g Gomega) {
				var got tideprojectv1alpha3.Plan
				g.Expect(mgrClient.Get(ctx, types.NamespacedName{Name: tpDispatchPlan, Namespace: "default"}, &got)).To(Succeed())
				jobName := fmt.Sprintf("tide-plan-%s-1", got.UID)
				var job batchv1.Job
				g.Expect(mgrClient.Get(ctx, types.NamespacedName{Name: jobName, Namespace: "default"}, &job)).To(Succeed())
				val, ok := jobEnvValue(&job, "TRACEPARENT")
				g.Expect(ok).To(BeTrue(), "TRACEPARENT env var must be present on the Plan dispatch Job")
				g.Expect(val).To(Equal(expectedTraceparent))
			}, 5*time.Second, 100*time.Millisecond).Should(Succeed())
		})

		It("omits TRACEPARENT when the parent's span ID was never persisted", func() {
			makeChain()
			// Deliberately do NOT seed ph.Status.PhaseTraceSpanID — stays "".

			r := newPlanReconciler()
			Expect(reconcileWithRetry(r.Reconcile, types.NamespacedName{Name: tpDispatchPlan, Namespace: "default"}, 5)).To(Succeed())

			Eventually(func(g Gomega) {
				var got tideprojectv1alpha3.Plan
				g.Expect(mgrClient.Get(ctx, types.NamespacedName{Name: tpDispatchPlan, Namespace: "default"}, &got)).To(Succeed())
				jobName := fmt.Sprintf("tide-plan-%s-1", got.UID)
				var job batchv1.Job
				g.Expect(mgrClient.Get(ctx, types.NamespacedName{Name: jobName, Namespace: "default"}, &job)).To(Succeed())
				_, ok := jobEnvValue(&job, "TRACEPARENT")
				g.Expect(ok).To(BeFalse(), "TRACEPARENT env var must be ABSENT when no parent span ID was ever persisted — never a malformed value")
			}, 5*time.Second, 100*time.Millisecond).Should(Succeed())
		})
	})

	// ─── Spec 3: reporter hop, Milestone level ────────────────────────────

	Describe("reporter hop at Milestone level", func() {
		const (
			tpReporterProjName = "tp-reporter-ms-proj"
			tpReporterMSName   = "tp-reporter-ms"
		)

		var (
			envReader *mapEnvReader
			exp       *tracetest.InMemoryExporter
			prevTP    oteltrace.TracerProvider
		)

		BeforeEach(func() {
			// 42-REVIEW WR-04: capture + swap the global TracerProvider FIRST —
			// before any failable fixture step (mirrors span_emission_test.go).
			exp = tracetest.NewInMemoryExporter()
			prevTP = otel.GetTracerProvider()
			otel.SetTracerProvider(sdktrace.NewTracerProvider(sdktrace.WithSyncer(exp)))

			spanEmissionProject(ctx, tpReporterProjName, "claude-test-model")
			envReader = newMapEnvReader()
		})

		AfterEach(func() {
			otel.SetTracerProvider(prevTP)

			ms := &tideprojectv1alpha3.Milestone{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: tpReporterMSName, Namespace: "default"}, ms); err == nil {
				ms.Finalizers = nil
				_ = k8sClient.Update(ctx, ms)
				_ = k8sClient.Delete(ctx, ms)
			}
			cleanupSpanEmissionProject(ctx, tpReporterProjName)
			var jobs batchv1.JobList
			_ = k8sClient.List(ctx, &jobs, client.InNamespace("default"))
			for i := range jobs.Items {
				j := jobs.Items[i]
				_ = k8sClient.Delete(ctx, &j)
			}
		})

		It("threads the level's OWN just-synthesized span ID into the reporter Job's --traceparent Arg", func() {
			ms := &tideprojectv1alpha3.Milestone{
				ObjectMeta: metav1.ObjectMeta{Name: tpReporterMSName, Namespace: "default"},
				Spec:       tideprojectv1alpha3.MilestoneSpec{ProjectRef: tpReporterProjName},
			}
			Expect(k8sClient.Create(ctx, ms)).To(Succeed())
			waitForCacheSync(tpReporterMSName, "default", &tideprojectv1alpha3.Milestone{})
			Expect(mgrClient.Get(ctx, types.NamespacedName{Name: tpReporterMSName, Namespace: "default"}, ms)).To(Succeed())

			statusPatch := client.MergeFrom(ms.DeepCopy())
			ms.Status.Phase = "Running"
			Expect(k8sClient.Status().Patch(ctx, ms, statusPatch)).To(Succeed())
			Expect(mgrClient.Get(ctx, types.NamespacedName{Name: tpReporterMSName, Namespace: "default"}, ms)).To(Succeed())

			var proj tideprojectv1alpha3.Project
			Expect(mgrClient.Get(ctx, types.NamespacedName{Name: tpReporterProjName, Namespace: "default"}, &proj)).To(Succeed())
			expectedTraceID, err := otelai.TraceIDFromUID(string(proj.UID))
			Expect(err).NotTo(HaveOccurred())

			start := time.Now().Add(-5 * time.Minute)
			end := time.Now()
			jobName := fmt.Sprintf("tide-milestone-%s-1", ms.UID)
			job := succeededPlannerJob(jobName, start, end)

			envReader.SetOut(string(ms.UID), pkgdispatch.EnvelopeOut{
				TaskUID: string(ms.UID),
				Usage: pkgdispatch.Usage{
					InputTokens:  700,
					OutputTokens: 300,
				},
			})

			r := &MilestoneReconciler{
				Client: mgrClient,
				Scheme: k8sClient.Scheme(),
				Deps: PlannerReconcilerDeps{
					Dispatcher:     &stubDispatcher{},
					EnvReader:      envReader,
					CredproxyImage: testCredproxyImage,
					SigningKey:     testSigningKey,
					ReporterImage:  testReporterImage,
					HelmProviderDefaults: ProviderDefaults{
						Image: testSubagentImage,
					},
				},
				PlannerPool:   newPlannerPoolForTest(),
				SharedPVCName: "tp-reporter-pvc",
			}

			_, err = r.handleJobCompletion(ctx, ms, job)
			Expect(err).NotTo(HaveOccurred())

			// PROP-02: the expected span-ID segment comes from the re-fetched
			// CRD status, not a hardcoded value — proves emit → persist → mirror
			// → reporter threading end-to-end in one reconcile. The persistence
			// patch inside handleJobCompletion writes through the same
			// cache-backed client this Get reads from — Eventually absorbs the
			// informer's watch-delivery lag between that write and this read
			// (mirrors span_emission_test.go's identical MilestoneTraceSpanID
			// persistence assertion).
			var refreshed tideprojectv1alpha3.Milestone
			Eventually(func(g Gomega) {
				g.Expect(mgrClient.Get(ctx, types.NamespacedName{Name: tpReporterMSName, Namespace: "default"}, &refreshed)).To(Succeed())
				g.Expect(refreshed.Status.MilestoneTraceSpanID).NotTo(BeEmpty())
			}, 5*time.Second, 100*time.Millisecond).Should(Succeed())

			expectedArg := fmt.Sprintf("--traceparent=00-%s-%s-01", expectedTraceID.String(), refreshed.Status.MilestoneTraceSpanID)

			reporterJobName := fmt.Sprintf("tide-reporter-%s", ms.UID)
			Eventually(func(g Gomega) {
				var reporterJob batchv1.Job
				g.Expect(mgrClient.Get(ctx, types.NamespacedName{Name: reporterJobName, Namespace: "default"}, &reporterJob)).To(Succeed())
				g.Expect(reporterJob.Spec.Template.Spec.Containers).NotTo(BeEmpty())
				g.Expect(reporterJob.Spec.Template.Spec.Containers[0].Args).To(ContainElement(expectedArg))
			}, 5*time.Second, 100*time.Millisecond).Should(Succeed())
		})
	})
})
