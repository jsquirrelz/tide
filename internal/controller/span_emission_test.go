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

// Phase 42 Plan 04 Task 3 (Milestone + Phase) and Plan 05 Task 2 (Plan +
// Project): envtest SpanEmission specs for all four planner levels, proven
// against a real in-memory OTel span exporter (ATTR-01/ATTR-02, D-01..D-04).
// Mirrors child_rollup_idempotency_test.go's direct-call shape
// (r.handle{JobCompletion,PlannerJobCompletion,ProjectJobCompletion}(ctx, obj,
// completedJob), never a full Reconcile round-trip) — synthetic Jobs are
// constructed in-memory since the handler only reads Status fields, no
// cluster Job object needed.
package controller

import (
	"context"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	oteltrace "go.opentelemetry.io/otel/trace"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	tideprojectv1alpha3 "github.com/jsquirrelz/tide/api/v1alpha3"
	pkgdispatch "github.com/jsquirrelz/tide/pkg/dispatch"
	"github.com/jsquirrelz/tide/pkg/otelai"
)

// seededParentSpanIDHex is a fixed, valid 16-hex-char SpanID used to seed a
// parent level's persisted {Level}TraceSpanID field before invoking a child
// level's completion handler — proving real parent-child SpanContext
// threading (TRACE-02) rather than an independent root.
const seededParentSpanIDHex = "b7ad6b7169203331"

// spanEmissionProject creates a minimal auto-gated Project with the given
// Subagent.Model, waits for cache, and returns it. Mirrors
// child_rollup_idempotency_test.go's childRollupProject but parameterizes
// the model so ResolveProvider resolves the same model at every level via
// the mid-chain Project.Spec.Subagent.Model fallback (avoiding per-level
// Levels override plumbing).
func spanEmissionProject(ctx context.Context, name, model string) *tideprojectv1alpha3.Project {
	proj := &tideprojectv1alpha3.Project{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
		Spec: tideprojectv1alpha3.ProjectSpec{
			SchemaRevision: "v1alpha3",
			TargetRepo:     "https://github.com/example/span-emission.git",
			Subagent:       tideprojectv1alpha3.SubagentConfig{Model: model},
			Gates:          tideprojectv1alpha3.Gates{Milestone: tideprojectv1alpha3.GatePolicy("auto")},
		},
	}
	Expect(k8sClient.Create(ctx, proj)).To(Succeed())
	waitForCacheSync(name, "default", &tideprojectv1alpha3.Project{})
	return proj
}

// cleanupSpanEmissionProject deletes the project (best-effort, removes
// finalizers first).
func cleanupSpanEmissionProject(ctx context.Context, name string) {
	p := &tideprojectv1alpha3.Project{}
	if err := k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: "default"}, p); err == nil {
		p.Finalizers = nil
		_ = k8sClient.Update(ctx, p)
		_ = k8sClient.Delete(ctx, p)
	}
}

// succeededPlannerJob builds an in-memory (never created in the cluster)
// terminal Job with a Complete condition. handleJobCompletion only reads
// Status fields off the object it is handed directly. The synthetic UID
// stands in for the server-assigned one: the SpanEmittedUID marker is keyed
// by Job UID, not name (42-REVIEW WR-02 / D-02).
func succeededPlannerJob(name string, start, end time.Time) *batchv1.Job {
	return &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{Name: name, UID: types.UID(name + "-uid")},
		Status: batchv1.JobStatus{
			StartTime:      &metav1.Time{Time: start},
			CompletionTime: &metav1.Time{Time: end},
			Conditions: []batchv1.JobCondition{
				{Type: batchv1.JobComplete, Status: corev1.ConditionTrue},
			},
		},
	}
}

// failedPlannerJob builds an in-memory terminal Job with a Failed condition
// and NO CompletionTime (Pitfall 1 — CompletionTime is nil on every failed
// Job; the end timestamp must come from the condition's LastTransitionTime).
// Synthetic UID for the same WR-02 reason as succeededPlannerJob.
func failedPlannerJob(name string, start, failedAt time.Time) *batchv1.Job {
	return &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{Name: name, UID: types.UID(name + "-uid")},
		Status: batchv1.JobStatus{
			StartTime: &metav1.Time{Time: start},
			Conditions: []batchv1.JobCondition{
				{Type: batchv1.JobFailed, Status: corev1.ConditionTrue, LastTransitionTime: metav1.Time{Time: failedAt}},
			},
		},
	}
}

// ─── Milestone level ─────────────────────────────────────────────────────────

var _ = Describe("SpanEmission — Milestone level", Label("envtest", "heavy"), func() {
	ctx := context.Background()

	const (
		seMSProjName = "span-emission-ms-proj"
		seMSName     = "span-emission-ms"
	)

	var (
		envReader *mapEnvReader
		exp       *tracetest.InMemoryExporter
		prevTP    oteltrace.TracerProvider
	)

	BeforeEach(func() {
		// 42-REVIEW WR-04: capture + swap the global TracerProvider FIRST —
		// before any failable fixture step. If spanEmissionProject fails its
		// Expect, Ginkgo still runs AfterEach; restoring a never-captured
		// (nil) prevTP would poison the otel global and panic every
		// subsequent otel.Tracer call in the package. Mirrors
		// setupSpanExporter's ordering in span_emission_unit_test.go.
		exp = tracetest.NewInMemoryExporter()
		prevTP = otel.GetTracerProvider()
		otel.SetTracerProvider(sdktrace.NewTracerProvider(sdktrace.WithSyncer(exp)))

		spanEmissionProject(ctx, seMSProjName, "claude-test-model")
		envReader = newMapEnvReader()
	})

	AfterEach(func() {
		otel.SetTracerProvider(prevTP)

		ms := &tideprojectv1alpha3.Milestone{}
		if err := k8sClient.Get(ctx, types.NamespacedName{Name: seMSName, Namespace: "default"}, ms); err == nil {
			ms.Finalizers = nil
			_ = k8sClient.Update(ctx, ms)
			_ = k8sClient.Delete(ctx, ms)
		}
		cleanupSpanEmissionProject(ctx, seMSProjName)
	})

	newMilestoneReconciler := func() *MilestoneReconciler {
		return &MilestoneReconciler{
			Client: mgrClient,
			Scheme: k8sClient.Scheme(),
			Deps: PlannerReconcilerDeps{
				Dispatcher:     &stubDispatcher{},
				EnvReader:      envReader,
				CredproxyImage: testCredproxyImage,
				SigningKey:     testSigningKey,
				HelmProviderDefaults: ProviderDefaults{
					Image: testSubagentImage,
				},
			},
			PlannerPool: newPlannerPoolForTest(),
			// ReporterImage deliberately empty: spawnReporterIfNeeded returns
			// (true, nil) → isFirstCompletion=true without a PVC.
		}
	}

	createMilestone := func() *tideprojectv1alpha3.Milestone {
		ms := &tideprojectv1alpha3.Milestone{
			ObjectMeta: metav1.ObjectMeta{Name: seMSName, Namespace: "default"},
			Spec:       tideprojectv1alpha3.MilestoneSpec{ProjectRef: seMSProjName},
		}
		Expect(k8sClient.Create(ctx, ms)).To(Succeed())
		waitForCacheSync(seMSName, "default", &tideprojectv1alpha3.Milestone{})
		Expect(mgrClient.Get(ctx, types.NamespacedName{Name: seMSName, Namespace: "default"}, ms)).To(Succeed())

		statusPatch := client.MergeFrom(ms.DeepCopy())
		ms.Status.Phase = "Running"
		Expect(k8sClient.Status().Patch(ctx, ms, statusPatch)).To(Succeed())
		Expect(mgrClient.Get(ctx, types.NamespacedName{Name: seMSName, Namespace: "default"}, ms)).To(Succeed())
		return ms
	}

	It("emits one attribute-complete AGENT span and is idempotent", func() {
		ms := createMilestone()

		// TRACE-02: seed the immediate parent's (Project) persisted span ID
		// so this level's span is properly parented, not an independent root.
		var proj tideprojectv1alpha3.Project
		Expect(mgrClient.Get(ctx, types.NamespacedName{Name: seMSProjName, Namespace: "default"}, &proj)).To(Succeed())
		projPatch := client.MergeFrom(proj.DeepCopy())
		proj.Status.ProjectTraceSpanID = seededParentSpanIDHex
		Expect(k8sClient.Status().Patch(ctx, &proj, projPatch)).To(Succeed())
		// Wait for the manager's cache (mgrClient, read by the reconciler
		// below via r.Client) to observe this write — k8sClient.Status().Patch
		// goes straight to the API server; the informer watch that backs
		// mgrClient's cache syncs asynchronously. Without this wait the
		// reconciler's own Get can race the watch and read a stale
		// (pre-patch) parent, intermittently producing a zero parentSpanID.
		Eventually(func(g Gomega) {
			var synced tideprojectv1alpha3.Project
			g.Expect(mgrClient.Get(ctx, types.NamespacedName{Name: seMSProjName, Namespace: "default"}, &synced)).To(Succeed())
			g.Expect(synced.Status.ProjectTraceSpanID).To(Equal(seededParentSpanIDHex))
		}, 5*time.Second, 50*time.Millisecond).Should(Succeed())

		start := time.Now().Add(-5 * time.Minute)
		end := time.Now()
		jobName := fmt.Sprintf("tide-milestone-%s-1", ms.UID)
		job := succeededPlannerJob(jobName, start, end)

		envReader.SetOut(string(ms.UID), pkgdispatch.EnvelopeOut{
			TaskUID: string(ms.UID),
			Usage: pkgdispatch.Usage{
				InputTokens:         700,
				OutputTokens:        300,
				CacheReadTokens:     200,
				CacheCreationTokens: 100,
			},
		})

		r := newMilestoneReconciler()

		// First call: emits the span.
		_, err := r.handleJobCompletion(ctx, ms, job)
		Expect(err).NotTo(HaveOccurred())

		spans := exp.GetSpans()
		Expect(spans).To(HaveLen(1), "exactly one span must be recorded on first observation")
		span := spans[0]
		Expect(span.Name).To(Equal("tide.dispatch.milestone"))
		Expect(span.StartTime).To(BeTemporally("==", start))
		Expect(span.EndTime).To(BeTemporally("==", end))
		Expect(span.Status.Code).To(Equal(codes.Ok))

		modelVal, ok := attrValue(span.Attributes, "llm.model_name")
		Expect(ok).To(BeTrue(), "span missing llm.model_name")
		Expect(modelVal.AsString()).To(Equal("claude-test-model"))

		providerVal, ok := attrValue(span.Attributes, "llm.provider")
		Expect(ok).To(BeTrue(), "span missing llm.provider")
		Expect(providerVal.AsString()).To(Equal("anthropic"))

		promptVal, ok := attrValue(span.Attributes, "llm.token_count.prompt")
		Expect(ok).To(BeTrue(), "span missing llm.token_count.prompt")
		Expect(promptVal.AsInt64()).To(BeNumerically("==", 1000))

		totalVal, ok := attrValue(span.Attributes, "llm.token_count.total")
		Expect(ok).To(BeTrue(), "span missing llm.token_count.total")
		Expect(totalVal.AsInt64()).To(BeNumerically("==", 1300))

		kindVal, ok := attrValue(span.Attributes, "openinference.span.kind")
		Expect(ok).To(BeTrue(), "span missing openinference.span.kind")
		Expect(kindVal.AsString()).To(Equal("AGENT"))

		// TRACE-02: deterministic TraceID derived from Project.UID, and real
		// parent linkage to the seeded ProjectTraceSpanID (Remote, since the
		// parent SpanContext is reconstructed from durable status, not a
		// locally-held live span).
		expectedTID, tErr := otelai.TraceIDFromUID(string(proj.UID))
		Expect(tErr).NotTo(HaveOccurred())
		Expect(span.SpanContext.TraceID().String()).To(Equal(expectedTID.String()))
		Expect(span.Parent.SpanID().String()).To(Equal(seededParentSpanIDHex))
		Expect(span.Parent.IsRemote()).To(BeTrue())

		Eventually(func(g Gomega) {
			var fresh tideprojectv1alpha3.Milestone
			g.Expect(mgrClient.Get(ctx, types.NamespacedName{Name: seMSName, Namespace: "default"}, &fresh)).To(Succeed())
			g.Expect(fresh.Status.MilestoneSpanEmittedUID).To(Equal(string(job.UID)),
				"MilestoneSpanEmittedUID must be set to the planner Job UID")
			// PROP-02: this level's own span ID is durably persisted.
			g.Expect(fresh.Status.MilestoneTraceSpanID).To(Equal(span.SpanContext.SpanID().String()),
				"MilestoneTraceSpanID must be set to this level's own synthesized SpanID")
		}, 5*time.Second, 100*time.Millisecond).Should(Succeed())

		// Second call with the SAME completedJob, after re-fetching ms (marker
		// now set) — D-02/Pitfall 2: no duplicate span.
		Expect(mgrClient.Get(ctx, types.NamespacedName{Name: seMSName, Namespace: "default"}, ms)).To(Succeed())
		_, err = r.handleJobCompletion(ctx, ms, job)
		Expect(err).NotTo(HaveOccurred())
		Expect(exp.GetSpans()).To(HaveLen(1), "second call with the same Job must not emit a duplicate span")
	})

	It("failed Job → Error span with condition-derived end time", func() {
		ms := createMilestone()

		start := time.Now().Add(-5 * time.Minute)
		failedAt := time.Now()
		jobName := fmt.Sprintf("tide-milestone-%s-1", ms.UID)
		job := failedPlannerJob(jobName, start, failedAt)

		envReader.SetOut(string(ms.UID), pkgdispatch.EnvelopeOut{
			TaskUID:  string(ms.UID),
			ExitCode: 2,
			Reason:   "cap-hit",
		})

		r := newMilestoneReconciler()
		_, err := r.handleJobCompletion(ctx, ms, job)
		Expect(err).NotTo(HaveOccurred())

		spans := exp.GetSpans()
		Expect(spans).To(HaveLen(1))
		span := spans[0]
		Expect(span.Status.Code).To(Equal(codes.Error))
		Expect(span.Status.Description).To(Equal("cap-hit"))
		Expect(span.EndTime).To(BeTemporally("==", failedAt))

		exitCodeVal, ok := attrValue(span.Attributes, "tide.exit_code")
		Expect(ok).To(BeTrue(), "span missing tide.exit_code")
		Expect(exitCodeVal.AsInt64()).To(BeNumerically("==", 2))

		reasonVal, ok := attrValue(span.Attributes, "tide.reason")
		Expect(ok).To(BeTrue(), "span missing tide.reason")
		Expect(reasonVal.AsString()).To(Equal("cap-hit"))
	})

	It("nil completedJob → zero spans", func() {
		ms := createMilestone()

		r := newMilestoneReconciler()
		_, err := r.handleJobCompletion(ctx, ms, nil)
		Expect(err).NotTo(HaveOccurred())

		Expect(exp.GetSpans()).To(BeEmpty())

		var fresh tideprojectv1alpha3.Milestone
		Expect(mgrClient.Get(ctx, types.NamespacedName{Name: seMSName, Namespace: "default"}, &fresh)).To(Succeed())
		Expect(fresh.Status.MilestoneSpanEmittedUID).To(BeEmpty())
	})

	It("degraded envelope still emits", func() {
		ms := createMilestone()

		start := time.Now().Add(-5 * time.Minute)
		end := time.Now()
		jobName := fmt.Sprintf("tide-milestone-%s-1", ms.UID)
		job := succeededPlannerJob(jobName, start, end)

		// Deliberately no SetOut for ms.UID: ReadOut returns "no envelope out
		// for task UID" → envReadOK=false (D-04 degraded path).

		r := newMilestoneReconciler()
		_, err := r.handleJobCompletion(ctx, ms, job)
		Expect(err).NotTo(HaveOccurred())

		spans := exp.GetSpans()
		Expect(spans).To(HaveLen(1))
		span := spans[0]

		degradedVal, ok := attrValue(span.Attributes, "tide.envelope.degraded")
		Expect(ok).To(BeTrue(), "span missing tide.envelope.degraded")
		Expect(degradedVal.AsBool()).To(BeTrue())

		modelVal, ok := attrValue(span.Attributes, "llm.model_name")
		Expect(ok).To(BeTrue(), "degraded span must still resolve llm.model_name (D-04)")
		Expect(modelVal.AsString()).To(Equal("claude-test-model"))

		for _, key := range []attribute.Key{
			"llm.token_count.prompt", "llm.token_count.completion",
			"llm.token_count.prompt_details.cache_read", "llm.token_count.prompt_details.cache_write",
			"llm.token_count.total",
		} {
			_, found := attrValue(span.Attributes, key)
			Expect(found).To(BeFalse(), "degraded span must not carry token-count attribute %q", key)
		}
	})

	It("does not stamp MilestoneSpanEmittedUID when project resolution fails (CR-01)", func() {
		// 43-REVIEW CR-01: an unresolvable ProjectRef must leave the marker
		// unstamped so a later reconcile (once the transient failure clears)
		// still gets a chance to emit — mirrors Task's project != nil guard.
		ms := &tideprojectv1alpha3.Milestone{
			ObjectMeta: metav1.ObjectMeta{Name: seMSName, Namespace: "default"},
			Spec:       tideprojectv1alpha3.MilestoneSpec{ProjectRef: "does-not-exist"},
		}
		Expect(k8sClient.Create(ctx, ms)).To(Succeed())
		waitForCacheSync(seMSName, "default", &tideprojectv1alpha3.Milestone{})
		Expect(mgrClient.Get(ctx, types.NamespacedName{Name: seMSName, Namespace: "default"}, ms)).To(Succeed())

		start := time.Now().Add(-5 * time.Minute)
		end := time.Now()
		jobName := fmt.Sprintf("tide-milestone-%s-1", ms.UID)
		job := succeededPlannerJob(jobName, start, end)

		r := newMilestoneReconciler()
		_, err := r.handleJobCompletion(ctx, ms, job)
		Expect(err).NotTo(HaveOccurred())

		Expect(exp.GetSpans()).To(BeEmpty(), "no span may be emitted when project is unresolvable")

		var latest tideprojectv1alpha3.Milestone
		Expect(mgrClient.Get(ctx, types.NamespacedName{Name: seMSName, Namespace: "default"}, &latest)).To(Succeed())
		Expect(latest.Status.MilestoneSpanEmittedUID).To(BeEmpty(),
			"marker must stay unstamped so a future reconcile with a resolvable project can still emit this attempt's span")
	})
})

// ─── Phase level ─────────────────────────────────────────────────────────────

var _ = Describe("SpanEmission — Phase level", Label("envtest", "heavy"), func() {
	ctx := context.Background()

	const (
		sePHProjName = "span-emission-ph-proj"
		sePHMSName   = "span-emission-ph-ms"
		sePHName     = "span-emission-ph"
	)

	var (
		envReader *mapEnvReader
		exp       *tracetest.InMemoryExporter
		prevTP    oteltrace.TracerProvider
	)

	BeforeEach(func() {
		// 42-REVIEW WR-04: TracerProvider capture + swap FIRST, before any
		// failable fixture step (see the Milestone-level BeforeEach).
		exp = tracetest.NewInMemoryExporter()
		prevTP = otel.GetTracerProvider()
		otel.SetTracerProvider(sdktrace.NewTracerProvider(sdktrace.WithSyncer(exp)))

		spanEmissionProject(ctx, sePHProjName, "claude-test-model")
		ms := &tideprojectv1alpha3.Milestone{
			ObjectMeta: metav1.ObjectMeta{Name: sePHMSName, Namespace: "default"},
			Spec:       tideprojectv1alpha3.MilestoneSpec{ProjectRef: sePHProjName},
		}
		Expect(k8sClient.Create(ctx, ms)).To(Succeed())
		waitForCacheSync(sePHMSName, "default", &tideprojectv1alpha3.Milestone{})
		envReader = newMapEnvReader()
	})

	AfterEach(func() {
		otel.SetTracerProvider(prevTP)

		ph := &tideprojectv1alpha3.Phase{}
		if err := k8sClient.Get(ctx, types.NamespacedName{Name: sePHName, Namespace: "default"}, ph); err == nil {
			ph.Finalizers = nil
			_ = k8sClient.Update(ctx, ph)
			_ = k8sClient.Delete(ctx, ph)
		}
		ms := &tideprojectv1alpha3.Milestone{}
		if err := k8sClient.Get(ctx, types.NamespacedName{Name: sePHMSName, Namespace: "default"}, ms); err == nil {
			ms.Finalizers = nil
			_ = k8sClient.Update(ctx, ms)
			_ = k8sClient.Delete(ctx, ms)
		}
		cleanupSpanEmissionProject(ctx, sePHProjName)
	})

	newPhaseReconciler := func() *PhaseReconciler {
		return &PhaseReconciler{
			Client: mgrClient,
			Scheme: k8sClient.Scheme(),
			Deps: PlannerReconcilerDeps{
				Dispatcher:     &stubDispatcher{},
				EnvReader:      envReader,
				CredproxyImage: testCredproxyImage,
				SigningKey:     testSigningKey,
				HelmProviderDefaults: ProviderDefaults{
					Image: testSubagentImage,
				},
			},
			PlannerPool: newPlannerPoolForTest(),
			// ReporterImage empty: isFirstCompletion=true without a PVC.
		}
	}

	createPhase := func() *tideprojectv1alpha3.Phase {
		ph := &tideprojectv1alpha3.Phase{
			ObjectMeta: metav1.ObjectMeta{Name: sePHName, Namespace: "default"},
			Spec:       tideprojectv1alpha3.PhaseSpec{MilestoneRef: sePHMSName},
		}
		Expect(k8sClient.Create(ctx, ph)).To(Succeed())
		waitForCacheSync(sePHName, "default", &tideprojectv1alpha3.Phase{})
		Expect(mgrClient.Get(ctx, types.NamespacedName{Name: sePHName, Namespace: "default"}, ph)).To(Succeed())

		statusPatch := client.MergeFrom(ph.DeepCopy())
		ph.Status.Phase = "Running"
		Expect(k8sClient.Status().Patch(ctx, ph, statusPatch)).To(Succeed())
		Expect(mgrClient.Get(ctx, types.NamespacedName{Name: sePHName, Namespace: "default"}, ph)).To(Succeed())
		return ph
	}

	It("emits one attribute-complete AGENT span and is idempotent", func() {
		ph := createPhase()

		// TRACE-02: seed the immediate parent's (Milestone) persisted span ID
		// so this level's span is properly parented, not an independent root.
		var parentMs tideprojectv1alpha3.Milestone
		Expect(mgrClient.Get(ctx, types.NamespacedName{Name: sePHMSName, Namespace: "default"}, &parentMs)).To(Succeed())
		msPatch := client.MergeFrom(parentMs.DeepCopy())
		parentMs.Status.MilestoneTraceSpanID = seededParentSpanIDHex
		Expect(k8sClient.Status().Patch(ctx, &parentMs, msPatch)).To(Succeed())
		// Wait for the manager's cache to observe this write before the
		// reconciler's own Get reads it below — same race guarded against at
		// every other level's seed step in this file.
		Eventually(func(g Gomega) {
			var synced tideprojectv1alpha3.Milestone
			g.Expect(mgrClient.Get(ctx, types.NamespacedName{Name: sePHMSName, Namespace: "default"}, &synced)).To(Succeed())
			g.Expect(synced.Status.MilestoneTraceSpanID).To(Equal(seededParentSpanIDHex))
		}, 5*time.Second, 50*time.Millisecond).Should(Succeed())

		var proj tideprojectv1alpha3.Project
		Expect(mgrClient.Get(ctx, types.NamespacedName{Name: sePHProjName, Namespace: "default"}, &proj)).To(Succeed())

		start := time.Now().Add(-5 * time.Minute)
		end := time.Now()
		jobName := fmt.Sprintf("tide-phase-%s-1", ph.UID)
		job := succeededPlannerJob(jobName, start, end)

		envReader.SetOut(string(ph.UID), pkgdispatch.EnvelopeOut{
			TaskUID: string(ph.UID),
			Usage: pkgdispatch.Usage{
				InputTokens:         700,
				OutputTokens:        300,
				CacheReadTokens:     200,
				CacheCreationTokens: 100,
			},
		})

		r := newPhaseReconciler()

		_, err := r.handleJobCompletion(ctx, ph, job)
		Expect(err).NotTo(HaveOccurred())

		spans := exp.GetSpans()
		Expect(spans).To(HaveLen(1), "exactly one span must be recorded on first observation")
		span := spans[0]
		Expect(span.Name).To(Equal("tide.dispatch.phase"))
		Expect(span.StartTime).To(BeTemporally("==", start))
		Expect(span.EndTime).To(BeTemporally("==", end))
		Expect(span.Status.Code).To(Equal(codes.Ok))

		modelVal, ok := attrValue(span.Attributes, "llm.model_name")
		Expect(ok).To(BeTrue(), "span missing llm.model_name")
		Expect(modelVal.AsString()).To(Equal("claude-test-model"))

		promptVal, ok := attrValue(span.Attributes, "llm.token_count.prompt")
		Expect(ok).To(BeTrue(), "span missing llm.token_count.prompt")
		Expect(promptVal.AsInt64()).To(BeNumerically("==", 1000))

		totalVal, ok := attrValue(span.Attributes, "llm.token_count.total")
		Expect(ok).To(BeTrue(), "span missing llm.token_count.total")
		Expect(totalVal.AsInt64()).To(BeNumerically("==", 1300))

		// TRACE-02: deterministic TraceID derived from Project.UID, and real
		// parent linkage to the seeded MilestoneTraceSpanID.
		expectedTID, tErr := otelai.TraceIDFromUID(string(proj.UID))
		Expect(tErr).NotTo(HaveOccurred())
		Expect(span.SpanContext.TraceID().String()).To(Equal(expectedTID.String()))
		Expect(span.Parent.SpanID().String()).To(Equal(seededParentSpanIDHex))
		Expect(span.Parent.IsRemote()).To(BeTrue())

		Eventually(func(g Gomega) {
			var fresh tideprojectv1alpha3.Phase
			g.Expect(mgrClient.Get(ctx, types.NamespacedName{Name: sePHName, Namespace: "default"}, &fresh)).To(Succeed())
			g.Expect(fresh.Status.PhaseSpanEmittedUID).To(Equal(string(job.UID)),
				"PhaseSpanEmittedUID must be set to the planner Job UID")
			// PROP-02: this level's own span ID is durably persisted.
			g.Expect(fresh.Status.PhaseTraceSpanID).To(Equal(span.SpanContext.SpanID().String()),
				"PhaseTraceSpanID must be set to this level's own synthesized SpanID")
		}, 5*time.Second, 100*time.Millisecond).Should(Succeed())

		// Second call with the SAME completedJob, after re-fetching ph — no
		// duplicate span (D-02/Pitfall 2).
		Expect(mgrClient.Get(ctx, types.NamespacedName{Name: sePHName, Namespace: "default"}, ph)).To(Succeed())
		_, err = r.handleJobCompletion(ctx, ph, job)
		Expect(err).NotTo(HaveOccurred())
		Expect(exp.GetSpans()).To(HaveLen(1), "second call with the same Job must not emit a duplicate span")
	})

	// Unnested-fallback (Pitfall 2 bounded degradation): if the immediate
	// parent's persisted span ID is unavailable (Milestone never got its own
	// span emitted, so MilestoneTraceSpanID is still empty), Phase's span
	// still emits — unnested (invalid Parent SpanID) but still carrying the
	// deterministic TraceID, never blocked on the parent's state.
	It("unnested fallback: parent Milestone has no persisted span ID yet", func() {
		ph := createPhase()

		var proj tideprojectv1alpha3.Project
		Expect(mgrClient.Get(ctx, types.NamespacedName{Name: sePHProjName, Namespace: "default"}, &proj)).To(Succeed())
		expectedTID, tErr := otelai.TraceIDFromUID(string(proj.UID))
		Expect(tErr).NotTo(HaveOccurred())

		start := time.Now().Add(-5 * time.Minute)
		end := time.Now()
		jobName := fmt.Sprintf("tide-phase-%s-1", ph.UID)
		job := succeededPlannerJob(jobName, start, end)

		envReader.SetOut(string(ph.UID), pkgdispatch.EnvelopeOut{
			TaskUID: string(ph.UID),
			Usage:   pkgdispatch.Usage{InputTokens: 100, OutputTokens: 50},
		})

		r := newPhaseReconciler()
		_, err := r.handleJobCompletion(ctx, ph, job)
		Expect(err).NotTo(HaveOccurred())

		spans := exp.GetSpans()
		Expect(spans).To(HaveLen(1))
		span := spans[0]
		Expect(span.Parent.SpanID().IsValid()).To(BeFalse(),
			"span must be unnested (no valid parent) when the parent's span ID was never persisted")
		Expect(span.SpanContext.TraceID().String()).To(Equal(expectedTID.String()),
			"TraceID must still be the deterministic one even for an unnested span")
	})

	It("failed Job → Error span with condition-derived end time", func() {
		ph := createPhase()

		start := time.Now().Add(-5 * time.Minute)
		failedAt := time.Now()
		jobName := fmt.Sprintf("tide-phase-%s-1", ph.UID)
		job := failedPlannerJob(jobName, start, failedAt)

		envReader.SetOut(string(ph.UID), pkgdispatch.EnvelopeOut{
			TaskUID:  string(ph.UID),
			ExitCode: 2,
			Reason:   "cap-hit",
		})

		r := newPhaseReconciler()
		_, err := r.handleJobCompletion(ctx, ph, job)
		Expect(err).NotTo(HaveOccurred())

		spans := exp.GetSpans()
		Expect(spans).To(HaveLen(1))
		span := spans[0]
		Expect(span.Status.Code).To(Equal(codes.Error))
		Expect(span.Status.Description).To(Equal("cap-hit"))
		Expect(span.EndTime).To(BeTemporally("==", failedAt))

		exitCodeVal, ok := attrValue(span.Attributes, "tide.exit_code")
		Expect(ok).To(BeTrue(), "span missing tide.exit_code")
		Expect(exitCodeVal.AsInt64()).To(BeNumerically("==", 2))
	})

	It("nil completedJob → zero spans", func() {
		ph := createPhase()

		r := newPhaseReconciler()
		_, err := r.handleJobCompletion(ctx, ph, nil)
		Expect(err).NotTo(HaveOccurred())

		Expect(exp.GetSpans()).To(BeEmpty())

		var fresh tideprojectv1alpha3.Phase
		Expect(mgrClient.Get(ctx, types.NamespacedName{Name: sePHName, Namespace: "default"}, &fresh)).To(Succeed())
		Expect(fresh.Status.PhaseSpanEmittedUID).To(BeEmpty())
	})

	It("does not stamp PhaseSpanEmittedUID when project resolution fails (CR-01)", func() {
		// 43-REVIEW CR-01: an unresolvable MilestoneRef (so resolveProject
		// returns nil) must leave the marker unstamped so a later reconcile
		// still gets a chance to emit.
		ph := &tideprojectv1alpha3.Phase{
			ObjectMeta: metav1.ObjectMeta{Name: sePHName, Namespace: "default"},
			Spec:       tideprojectv1alpha3.PhaseSpec{MilestoneRef: "does-not-exist"},
		}
		Expect(k8sClient.Create(ctx, ph)).To(Succeed())
		waitForCacheSync(sePHName, "default", &tideprojectv1alpha3.Phase{})
		Expect(mgrClient.Get(ctx, types.NamespacedName{Name: sePHName, Namespace: "default"}, ph)).To(Succeed())

		start := time.Now().Add(-5 * time.Minute)
		end := time.Now()
		jobName := fmt.Sprintf("tide-phase-%s-1", ph.UID)
		job := succeededPlannerJob(jobName, start, end)

		r := newPhaseReconciler()
		_, err := r.handleJobCompletion(ctx, ph, job)
		Expect(err).NotTo(HaveOccurred())

		Expect(exp.GetSpans()).To(BeEmpty(), "no span may be emitted when project is unresolvable")

		var latest tideprojectv1alpha3.Phase
		Expect(mgrClient.Get(ctx, types.NamespacedName{Name: sePHName, Namespace: "default"}, &latest)).To(Succeed())
		Expect(latest.Status.PhaseSpanEmittedUID).To(BeEmpty(),
			"marker must stay unstamped so a future reconcile with a resolvable project can still emit this attempt's span")
	})
})

// ─── Plan level ──────────────────────────────────────────────────────────────

var _ = Describe("SpanEmission — Plan level", Label("envtest", "heavy"), func() {
	ctx := context.Background()

	const (
		sePlanProjName = "span-emission-plan-proj"
		sePlanMSName   = "span-emission-plan-ms"
		sePlanPhName   = "span-emission-plan-ph"
		sePlanName     = "span-emission-plan"
	)

	var (
		envReader *mapEnvReader
		exp       *tracetest.InMemoryExporter
		prevTP    oteltrace.TracerProvider
	)

	BeforeEach(func() {
		// 42-REVIEW WR-04: TracerProvider capture + swap FIRST, before any
		// failable fixture step (see the Milestone-level BeforeEach).
		exp = tracetest.NewInMemoryExporter()
		prevTP = otel.GetTracerProvider()
		otel.SetTracerProvider(sdktrace.NewTracerProvider(sdktrace.WithSyncer(exp)))

		spanEmissionProject(ctx, sePlanProjName, "claude-test-model")
		ms := &tideprojectv1alpha3.Milestone{
			ObjectMeta: metav1.ObjectMeta{Name: sePlanMSName, Namespace: "default"},
			Spec:       tideprojectv1alpha3.MilestoneSpec{ProjectRef: sePlanProjName},
		}
		Expect(k8sClient.Create(ctx, ms)).To(Succeed())
		waitForCacheSync(sePlanMSName, "default", &tideprojectv1alpha3.Milestone{})
		ph := &tideprojectv1alpha3.Phase{
			ObjectMeta: metav1.ObjectMeta{Name: sePlanPhName, Namespace: "default"},
			Spec:       tideprojectv1alpha3.PhaseSpec{MilestoneRef: sePlanMSName},
		}
		Expect(k8sClient.Create(ctx, ph)).To(Succeed())
		waitForCacheSync(sePlanPhName, "default", &tideprojectv1alpha3.Phase{})
		envReader = newMapEnvReader()
	})

	AfterEach(func() {
		otel.SetTracerProvider(prevTP)

		plan := &tideprojectv1alpha3.Plan{}
		if err := k8sClient.Get(ctx, types.NamespacedName{Name: sePlanName, Namespace: "default"}, plan); err == nil {
			plan.Finalizers = nil
			_ = k8sClient.Update(ctx, plan)
			_ = k8sClient.Delete(ctx, plan)
		}
		ph := &tideprojectv1alpha3.Phase{}
		if err := k8sClient.Get(ctx, types.NamespacedName{Name: sePlanPhName, Namespace: "default"}, ph); err == nil {
			ph.Finalizers = nil
			_ = k8sClient.Update(ctx, ph)
			_ = k8sClient.Delete(ctx, ph)
		}
		ms := &tideprojectv1alpha3.Milestone{}
		if err := k8sClient.Get(ctx, types.NamespacedName{Name: sePlanMSName, Namespace: "default"}, ms); err == nil {
			ms.Finalizers = nil
			_ = k8sClient.Update(ctx, ms)
			_ = k8sClient.Delete(ctx, ms)
		}
		cleanupSpanEmissionProject(ctx, sePlanProjName)
	})

	newPlanReconciler := func() *PlanReconciler {
		return &PlanReconciler{
			Client: mgrClient,
			Scheme: k8sClient.Scheme(),
			Deps: PlannerReconcilerDeps{
				Dispatcher:     &stubDispatcher{},
				EnvReader:      envReader,
				CredproxyImage: testCredproxyImage,
				SigningKey:     testSigningKey,
				HelmProviderDefaults: ProviderDefaults{
					Image: testSubagentImage,
				},
			},
			PlannerPool: newPlannerPoolForTest(),
			// ReporterImage empty: isFirstCompletion=true without a PVC.
		}
	}

	// resolveProjectForPlan resolves the project via Plan.Spec.PhaseRef →
	// Phase.Spec.MilestoneRef → Milestone.Spec.ProjectRef (mirrors
	// child_rollup_idempotency_test.go's Plan-level fixture chain).
	createPlan := func() *tideprojectv1alpha3.Plan {
		plan := &tideprojectv1alpha3.Plan{
			ObjectMeta: metav1.ObjectMeta{Name: sePlanName, Namespace: "default"},
			Spec:       tideprojectv1alpha3.PlanSpec{PhaseRef: sePlanPhName},
		}
		Expect(k8sClient.Create(ctx, plan)).To(Succeed())
		waitForCacheSync(sePlanName, "default", &tideprojectv1alpha3.Plan{})
		Expect(mgrClient.Get(ctx, types.NamespacedName{Name: sePlanName, Namespace: "default"}, plan)).To(Succeed())

		statusPatch := client.MergeFrom(plan.DeepCopy())
		plan.Status.Phase = "Running"
		Expect(k8sClient.Status().Patch(ctx, plan, statusPatch)).To(Succeed())
		Expect(mgrClient.Get(ctx, types.NamespacedName{Name: sePlanName, Namespace: "default"}, plan)).To(Succeed())
		return plan
	}

	It("emits one attribute-complete AGENT span and is idempotent", func() {
		plan := createPlan()

		// TRACE-02: seed the immediate parent's (Phase) persisted span ID so
		// this level's span is properly parented, not an independent root.
		var parentPh tideprojectv1alpha3.Phase
		Expect(mgrClient.Get(ctx, types.NamespacedName{Name: sePlanPhName, Namespace: "default"}, &parentPh)).To(Succeed())
		phPatch := client.MergeFrom(parentPh.DeepCopy())
		parentPh.Status.PhaseTraceSpanID = seededParentSpanIDHex
		Expect(k8sClient.Status().Patch(ctx, &parentPh, phPatch)).To(Succeed())
		// Wait for the manager's cache to observe this write before the
		// reconciler's own Get reads it below — same race guarded against at
		// every other level's seed step in this file.
		Eventually(func(g Gomega) {
			var synced tideprojectv1alpha3.Phase
			g.Expect(mgrClient.Get(ctx, types.NamespacedName{Name: sePlanPhName, Namespace: "default"}, &synced)).To(Succeed())
			g.Expect(synced.Status.PhaseTraceSpanID).To(Equal(seededParentSpanIDHex))
		}, 5*time.Second, 50*time.Millisecond).Should(Succeed())

		var proj tideprojectv1alpha3.Project
		Expect(mgrClient.Get(ctx, types.NamespacedName{Name: sePlanProjName, Namespace: "default"}, &proj)).To(Succeed())

		start := time.Now().Add(-5 * time.Minute)
		end := time.Now()
		jobName := fmt.Sprintf("tide-plan-%s-1", plan.UID)
		job := succeededPlannerJob(jobName, start, end)

		envReader.SetOut(string(plan.UID), pkgdispatch.EnvelopeOut{
			TaskUID: string(plan.UID),
			Usage: pkgdispatch.Usage{
				InputTokens:         700,
				OutputTokens:        300,
				CacheReadTokens:     200,
				CacheCreationTokens: 100,
			},
		})

		r := newPlanReconciler()

		// First call: emits the span.
		_, err := r.handlePlannerJobCompletion(ctx, plan, job)
		Expect(err).NotTo(HaveOccurred())

		spans := exp.GetSpans()
		Expect(spans).To(HaveLen(1), "exactly one span must be recorded on first observation")
		span := spans[0]
		Expect(span.Name).To(Equal("tide.dispatch.plan"))
		Expect(span.StartTime).To(BeTemporally("==", start))
		Expect(span.EndTime).To(BeTemporally("==", end))
		Expect(span.Status.Code).To(Equal(codes.Ok))

		modelVal, ok := attrValue(span.Attributes, "llm.model_name")
		Expect(ok).To(BeTrue(), "span missing llm.model_name")
		Expect(modelVal.AsString()).To(Equal("claude-test-model"))

		providerVal, ok := attrValue(span.Attributes, "llm.provider")
		Expect(ok).To(BeTrue(), "span missing llm.provider")
		Expect(providerVal.AsString()).To(Equal("anthropic"))

		promptVal, ok := attrValue(span.Attributes, "llm.token_count.prompt")
		Expect(ok).To(BeTrue(), "span missing llm.token_count.prompt")
		Expect(promptVal.AsInt64()).To(BeNumerically("==", 1000))

		totalVal, ok := attrValue(span.Attributes, "llm.token_count.total")
		Expect(ok).To(BeTrue(), "span missing llm.token_count.total")
		Expect(totalVal.AsInt64()).To(BeNumerically("==", 1300))

		// TRACE-02: deterministic TraceID derived from Project.UID, and real
		// parent linkage to the seeded PhaseTraceSpanID.
		expectedTID, tErr := otelai.TraceIDFromUID(string(proj.UID))
		Expect(tErr).NotTo(HaveOccurred())
		Expect(span.SpanContext.TraceID().String()).To(Equal(expectedTID.String()))
		Expect(span.Parent.SpanID().String()).To(Equal(seededParentSpanIDHex))
		Expect(span.Parent.IsRemote()).To(BeTrue())

		Eventually(func(g Gomega) {
			var fresh tideprojectv1alpha3.Plan
			g.Expect(mgrClient.Get(ctx, types.NamespacedName{Name: sePlanName, Namespace: "default"}, &fresh)).To(Succeed())
			g.Expect(fresh.Status.PlanSpanEmittedUID).To(Equal(string(job.UID)),
				"PlanSpanEmittedUID must be set to the planner Job UID")
			// PROP-02: this level's own span ID is durably persisted.
			g.Expect(fresh.Status.PlanTraceSpanID).To(Equal(span.SpanContext.SpanID().String()),
				"PlanTraceSpanID must be set to this level's own synthesized SpanID")
		}, 5*time.Second, 100*time.Millisecond).Should(Succeed())

		// Second call with the SAME completedJob, after re-fetching plan (marker
		// now set) — D-02/Pitfall 2: no duplicate span.
		Expect(mgrClient.Get(ctx, types.NamespacedName{Name: sePlanName, Namespace: "default"}, plan)).To(Succeed())
		_, err = r.handlePlannerJobCompletion(ctx, plan, job)
		Expect(err).NotTo(HaveOccurred())
		Expect(exp.GetSpans()).To(HaveLen(1), "second call with the same Job must not emit a duplicate span")
	})

	It("failed Job → Error span with condition-derived end time", func() {
		plan := createPlan()

		start := time.Now().Add(-5 * time.Minute)
		failedAt := time.Now()
		jobName := fmt.Sprintf("tide-plan-%s-1", plan.UID)
		job := failedPlannerJob(jobName, start, failedAt)

		envReader.SetOut(string(plan.UID), pkgdispatch.EnvelopeOut{
			TaskUID:  string(plan.UID),
			ExitCode: 2,
			Reason:   "cap-hit",
		})

		r := newPlanReconciler()
		_, err := r.handlePlannerJobCompletion(ctx, plan, job)
		Expect(err).NotTo(HaveOccurred())

		spans := exp.GetSpans()
		Expect(spans).To(HaveLen(1))
		span := spans[0]
		Expect(span.Status.Code).To(Equal(codes.Error))
		Expect(span.Status.Description).To(Equal("cap-hit"))
		Expect(span.EndTime).To(BeTemporally("==", failedAt))

		exitCodeVal, ok := attrValue(span.Attributes, "tide.exit_code")
		Expect(ok).To(BeTrue(), "span missing tide.exit_code")
		Expect(exitCodeVal.AsInt64()).To(BeNumerically("==", 2))

		reasonVal, ok := attrValue(span.Attributes, "tide.reason")
		Expect(ok).To(BeTrue(), "span missing tide.reason")
		Expect(reasonVal.AsString()).To(Equal("cap-hit"))
	})

	It("nil completedJob → zero spans", func() {
		plan := createPlan()

		r := newPlanReconciler()
		_, err := r.handlePlannerJobCompletion(ctx, plan, nil)
		Expect(err).NotTo(HaveOccurred())

		Expect(exp.GetSpans()).To(BeEmpty())

		var fresh tideprojectv1alpha3.Plan
		Expect(mgrClient.Get(ctx, types.NamespacedName{Name: sePlanName, Namespace: "default"}, &fresh)).To(Succeed())
		Expect(fresh.Status.PlanSpanEmittedUID).To(BeEmpty())
	})

	It("does not stamp PlanSpanEmittedUID when project resolution fails (CR-01)", func() {
		// 43-REVIEW CR-01: no project-label fast-path and an unresolvable
		// PhaseRef (so resolveProjectForPlan returns nil) must leave the
		// marker unstamped so a later reconcile still gets a chance to emit.
		plan := &tideprojectv1alpha3.Plan{
			ObjectMeta: metav1.ObjectMeta{Name: sePlanName, Namespace: "default"},
			Spec:       tideprojectv1alpha3.PlanSpec{PhaseRef: "does-not-exist"},
		}
		Expect(k8sClient.Create(ctx, plan)).To(Succeed())
		waitForCacheSync(sePlanName, "default", &tideprojectv1alpha3.Plan{})
		Expect(mgrClient.Get(ctx, types.NamespacedName{Name: sePlanName, Namespace: "default"}, plan)).To(Succeed())

		start := time.Now().Add(-5 * time.Minute)
		end := time.Now()
		jobName := fmt.Sprintf("tide-plan-%s-1", plan.UID)
		job := succeededPlannerJob(jobName, start, end)

		r := newPlanReconciler()
		_, err := r.handlePlannerJobCompletion(ctx, plan, job)
		Expect(err).NotTo(HaveOccurred())

		Expect(exp.GetSpans()).To(BeEmpty(), "no span may be emitted when project is unresolvable")

		var latest tideprojectv1alpha3.Plan
		Expect(mgrClient.Get(ctx, types.NamespacedName{Name: sePlanName, Namespace: "default"}, &latest)).To(Succeed())
		Expect(latest.Status.PlanSpanEmittedUID).To(BeEmpty(),
			"marker must stay unstamped so a future reconcile with a resolvable project can still emit this attempt's span")
	})
})

// ─── Project level ───────────────────────────────────────────────────────────

var _ = Describe("SpanEmission — Project level", Label("envtest", "heavy"), func() {
	ctx := context.Background()

	const seProjName = "span-emission-project"

	var (
		envReader *mapEnvReader
		exp       *tracetest.InMemoryExporter
		prevTP    oteltrace.TracerProvider
	)

	BeforeEach(func() {
		// 42-REVIEW WR-04: TracerProvider capture + swap FIRST, before any
		// failable fixture step (see the Milestone-level BeforeEach).
		exp = tracetest.NewInMemoryExporter()
		prevTP = otel.GetTracerProvider()
		otel.SetTracerProvider(sdktrace.NewTracerProvider(sdktrace.WithSyncer(exp)))

		spanEmissionProject(ctx, seProjName, "claude-test-model")
		envReader = newMapEnvReader()
	})

	AfterEach(func() {
		otel.SetTracerProvider(prevTP)
		cleanupSpanEmissionProject(ctx, seProjName)
	})

	newProjectReconciler := func() *ProjectReconciler {
		return &ProjectReconciler{
			Client: mgrClient,
			Scheme: k8sClient.Scheme(),
			Deps: PlannerReconcilerDeps{
				Dispatcher:     &stubDispatcher{},
				EnvReader:      envReader,
				CredproxyImage: testCredproxyImage,
				SigningKey:     testSigningKey,
				HelmProviderDefaults: ProviderDefaults{
					Image: testSubagentImage,
				},
			},
			PlannerPool: newPlannerPoolForTest(),
			// ReporterImage empty: isFirstCompletion=true without a PVC.
		}
	}

	// loadProject re-fetches the BeforeEach-created Project (server-assigned
	// UID) and sets Status.Phase=Running, mirroring
	// project_rollup_idempotency_test.go's fixture shape.
	loadProject := func() *tideprojectv1alpha3.Project {
		proj := &tideprojectv1alpha3.Project{}
		Expect(mgrClient.Get(ctx, types.NamespacedName{Name: seProjName, Namespace: "default"}, proj)).To(Succeed())

		statusPatch := client.MergeFrom(proj.DeepCopy())
		proj.Status.Phase = "Running"
		Expect(k8sClient.Status().Patch(ctx, proj, statusPatch)).To(Succeed())
		Expect(mgrClient.Get(ctx, types.NamespacedName{Name: seProjName, Namespace: "default"}, proj)).To(Succeed())
		return proj
	}

	It("emits one attribute-complete AGENT span and is idempotent", func() {
		proj := loadProject()

		start := time.Now().Add(-5 * time.Minute)
		end := time.Now()
		jobName := fmt.Sprintf("tide-project-%s-1", proj.UID)
		job := succeededPlannerJob(jobName, start, end)

		// Project-level envelope self-keys on project.UID for both parentUID
		// and taskUID — the Project IS the parent at this level (handler
		// calls ReadOut(ctx, string(project.UID), string(project.UID))).
		envReader.SetOut(string(proj.UID), pkgdispatch.EnvelopeOut{
			TaskUID: string(proj.UID),
			Usage: pkgdispatch.Usage{
				InputTokens:         700,
				OutputTokens:        300,
				CacheReadTokens:     200,
				CacheCreationTokens: 100,
			},
		})

		r := newProjectReconciler()

		// First call: emits the span.
		_, err := r.handleProjectJobCompletion(ctx, proj, job)
		Expect(err).NotTo(HaveOccurred())

		spans := exp.GetSpans()
		Expect(spans).To(HaveLen(1), "exactly one span must be recorded on first observation")
		span := spans[0]
		Expect(span.Name).To(Equal("tide.dispatch.project"))
		Expect(span.StartTime).To(BeTemporally("==", start))
		Expect(span.EndTime).To(BeTemporally("==", end))
		Expect(span.Status.Code).To(Equal(codes.Ok))

		modelVal, ok := attrValue(span.Attributes, "llm.model_name")
		Expect(ok).To(BeTrue(), "span missing llm.model_name")
		Expect(modelVal.AsString()).To(Equal("claude-test-model"))

		providerVal, ok := attrValue(span.Attributes, "llm.provider")
		Expect(ok).To(BeTrue(), "span missing llm.provider")
		Expect(providerVal.AsString()).To(Equal("anthropic"))

		promptVal, ok := attrValue(span.Attributes, "llm.token_count.prompt")
		Expect(ok).To(BeTrue(), "span missing llm.token_count.prompt")
		Expect(promptVal.AsInt64()).To(BeNumerically("==", 1000))

		totalVal, ok := attrValue(span.Attributes, "llm.token_count.total")
		Expect(ok).To(BeTrue(), "span missing llm.token_count.total")
		Expect(totalVal.AsInt64()).To(BeNumerically("==", 1300))

		// D-02: Project is the trace root — no parent span exists, but the
		// TraceID is still the deterministic one derived from its own UID.
		expectedTID, tErr := otelai.TraceIDFromUID(string(proj.UID))
		Expect(tErr).NotTo(HaveOccurred())
		Expect(span.SpanContext.TraceID().String()).To(Equal(expectedTID.String()))
		Expect(span.Parent.SpanID().IsValid()).To(BeFalse(), "Project's span must have no valid parent (D-02 root)")

		Eventually(func(g Gomega) {
			var fresh tideprojectv1alpha3.Project
			g.Expect(mgrClient.Get(ctx, types.NamespacedName{Name: seProjName, Namespace: "default"}, &fresh)).To(Succeed())
			g.Expect(fresh.Status.PlannerSpanEmittedUID).To(Equal(string(job.UID)),
				"PlannerSpanEmittedUID must be set to the planner Job UID")
			// PROP-02: this level's own span ID is durably persisted.
			g.Expect(fresh.Status.ProjectTraceSpanID).To(Equal(span.SpanContext.SpanID().String()),
				"ProjectTraceSpanID must be set to this level's own synthesized SpanID")
		}, 5*time.Second, 100*time.Millisecond).Should(Succeed())

		// Second call with the SAME completedJob, after re-fetching proj (marker
		// now set) — D-02/Pitfall 2: no duplicate span. Asserts span count == 1
		// AND the marker remains stamped with the Job UID (plan 42-05 acceptance
		// criteria, WR-02: UID-keyed).
		Expect(mgrClient.Get(ctx, types.NamespacedName{Name: seProjName, Namespace: "default"}, proj)).To(Succeed())
		_, err = r.handleProjectJobCompletion(ctx, proj, job)
		Expect(err).NotTo(HaveOccurred())
		Expect(exp.GetSpans()).To(HaveLen(1), "second call with the same Job must not emit a duplicate span")

		var fresh tideprojectv1alpha3.Project
		Expect(mgrClient.Get(ctx, types.NamespacedName{Name: seProjName, Namespace: "default"}, &fresh)).To(Succeed())
		Expect(fresh.Status.PlannerSpanEmittedUID).To(Equal(string(job.UID)),
			"PlannerSpanEmittedUID must remain set to the planner Job UID after the idempotent second call")
	})

	It("failed Job → Error span with condition-derived end time", func() {
		proj := loadProject()

		start := time.Now().Add(-5 * time.Minute)
		failedAt := time.Now()
		jobName := fmt.Sprintf("tide-project-%s-1", proj.UID)
		job := failedPlannerJob(jobName, start, failedAt)

		envReader.SetOut(string(proj.UID), pkgdispatch.EnvelopeOut{
			TaskUID:  string(proj.UID),
			ExitCode: 2,
			Reason:   "cap-hit",
		})

		r := newProjectReconciler()
		_, err := r.handleProjectJobCompletion(ctx, proj, job)
		Expect(err).NotTo(HaveOccurred())

		spans := exp.GetSpans()
		Expect(spans).To(HaveLen(1))
		span := spans[0]
		Expect(span.Status.Code).To(Equal(codes.Error))
		Expect(span.Status.Description).To(Equal("cap-hit"))
		Expect(span.EndTime).To(BeTemporally("==", failedAt))

		exitCodeVal, ok := attrValue(span.Attributes, "tide.exit_code")
		Expect(ok).To(BeTrue(), "span missing tide.exit_code")
		Expect(exitCodeVal.AsInt64()).To(BeNumerically("==", 2))

		reasonVal, ok := attrValue(span.Attributes, "tide.reason")
		Expect(ok).To(BeTrue(), "span missing tide.reason")
		Expect(reasonVal.AsString()).To(Equal("cap-hit"))
	})

	It("nil completedJob → zero spans", func() {
		proj := loadProject()

		r := newProjectReconciler()
		_, err := r.handleProjectJobCompletion(ctx, proj, nil)
		Expect(err).NotTo(HaveOccurred())

		Expect(exp.GetSpans()).To(BeEmpty())

		var fresh tideprojectv1alpha3.Project
		Expect(mgrClient.Get(ctx, types.NamespacedName{Name: seProjName, Namespace: "default"}, &fresh)).To(Succeed())
		Expect(fresh.Status.PlannerSpanEmittedUID).To(BeEmpty())
	})
})

// ─── Task level ──────────────────────────────────────────────────────────────

// Plan 43-05: TRACE-01 closes the last dispatch-chain gap. Task's
// handleJobCompletion has FOUR terminal paths (versus the planner levels'
// one) — the generalized Option B decision (43-05-PLAN.md) places two
// emitTaskSpanOnce call sites so every one is covered: the EnvelopeReadFailed
// branch (envReadOK=false) and a single site immediately after a successful
// envelope read, BEFORE the OutputValidationError/OutputPathsViolation/
// standard-result branch divergence (envReadOK=true).
var _ = Describe("SpanEmission — Task level", Label("envtest", "heavy"), func() {
	ctx := context.Background()

	const (
		seTaskProjName = "span-emission-task-proj"
		seTaskMSName   = "span-emission-task-ms"
		seTaskPhName   = "span-emission-task-ph"
		seTaskPlanName = "span-emission-task-plan"
		seTaskName     = "span-emission-task"
	)

	var (
		envReader *mapEnvReader
		exp       *tracetest.InMemoryExporter
		prevTP    oteltrace.TracerProvider
	)

	BeforeEach(func() {
		// 42-REVIEW WR-04: TracerProvider capture + swap FIRST, before any
		// failable fixture step (see the Milestone-level BeforeEach).
		exp = tracetest.NewInMemoryExporter()
		prevTP = otel.GetTracerProvider()
		otel.SetTracerProvider(sdktrace.NewTracerProvider(sdktrace.WithSyncer(exp)))

		spanEmissionProject(ctx, seTaskProjName, "claude-test-model")
		ms := &tideprojectv1alpha3.Milestone{
			ObjectMeta: metav1.ObjectMeta{Name: seTaskMSName, Namespace: "default"},
			Spec:       tideprojectv1alpha3.MilestoneSpec{ProjectRef: seTaskProjName},
		}
		Expect(k8sClient.Create(ctx, ms)).To(Succeed())
		waitForCacheSync(seTaskMSName, "default", &tideprojectv1alpha3.Milestone{})
		ph := &tideprojectv1alpha3.Phase{
			ObjectMeta: metav1.ObjectMeta{Name: seTaskPhName, Namespace: "default"},
			Spec:       tideprojectv1alpha3.PhaseSpec{MilestoneRef: seTaskMSName},
		}
		Expect(k8sClient.Create(ctx, ph)).To(Succeed())
		waitForCacheSync(seTaskPhName, "default", &tideprojectv1alpha3.Phase{})
		envReader = newMapEnvReader()
	})

	AfterEach(func() {
		otel.SetTracerProvider(prevTP)

		task := &tideprojectv1alpha3.Task{}
		if err := k8sClient.Get(ctx, types.NamespacedName{Name: seTaskName, Namespace: "default"}, task); err == nil {
			task.Finalizers = nil
			_ = k8sClient.Update(ctx, task)
			_ = k8sClient.Delete(ctx, task)
		}
		plan := &tideprojectv1alpha3.Plan{}
		if err := k8sClient.Get(ctx, types.NamespacedName{Name: seTaskPlanName, Namespace: "default"}, plan); err == nil {
			plan.Finalizers = nil
			_ = k8sClient.Update(ctx, plan)
			_ = k8sClient.Delete(ctx, plan)
		}
		ph := &tideprojectv1alpha3.Phase{}
		if err := k8sClient.Get(ctx, types.NamespacedName{Name: seTaskPhName, Namespace: "default"}, ph); err == nil {
			ph.Finalizers = nil
			_ = k8sClient.Update(ctx, ph)
			_ = k8sClient.Delete(ctx, ph)
		}
		ms := &tideprojectv1alpha3.Milestone{}
		if err := k8sClient.Get(ctx, types.NamespacedName{Name: seTaskMSName, Namespace: "default"}, ms); err == nil {
			ms.Finalizers = nil
			_ = k8sClient.Update(ctx, ms)
			_ = k8sClient.Delete(ctx, ms)
		}
		cleanupSpanEmissionProject(ctx, seTaskProjName)
	})

	newTaskReconcilerForSpanEmission := func() *TaskReconciler {
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
			},
		}
	}

	createTaskPlan := func() *tideprojectv1alpha3.Plan {
		plan := &tideprojectv1alpha3.Plan{
			ObjectMeta: metav1.ObjectMeta{Name: seTaskPlanName, Namespace: "default"},
			Spec:       tideprojectv1alpha3.PlanSpec{PhaseRef: seTaskPhName},
		}
		Expect(k8sClient.Create(ctx, plan)).To(Succeed())
		waitForCacheSync(seTaskPlanName, "default", &tideprojectv1alpha3.Plan{})
		Expect(mgrClient.Get(ctx, types.NamespacedName{Name: seTaskPlanName, Namespace: "default"}, plan)).To(Succeed())
		return plan
	}

	// createSETask builds the Task fixture. DeclaredOutputPaths is always
	// non-empty (CRD schema requires MinItems=1 — task_types.go has no
	// +optional on this field), but the output-path-validation block in
	// handleJobCompletion additionally requires Status.StartedAt != nil to
	// activate (task_controller.go ~962) — setStartedAt controls that
	// independently so most specs skip the block entirely.
	createSETask := func(setStartedAt bool) *tideprojectv1alpha3.Task {
		task := &tideprojectv1alpha3.Task{
			ObjectMeta: metav1.ObjectMeta{Name: seTaskName, Namespace: "default"},
			Spec: tideprojectv1alpha3.TaskSpec{
				PlanRef:             seTaskPlanName,
				FilesTouched:        []string{"src/main.go"},
				PromptPath:          "envelopes/test/children/" + seTaskName + ".json",
				DeclaredOutputPaths: []string{"artifacts/out.txt"},
			},
		}
		Expect(k8sClient.Create(ctx, task)).To(Succeed())
		waitForCacheSync(seTaskName, "default", &tideprojectv1alpha3.Task{})
		Expect(mgrClient.Get(ctx, types.NamespacedName{Name: seTaskName, Namespace: "default"}, task)).To(Succeed())

		statusPatch := client.MergeFrom(task.DeepCopy())
		task.Status.Phase = "Running"
		if setStartedAt {
			past := metav1.NewTime(time.Now().Add(-1 * time.Minute))
			task.Status.StartedAt = &past
		}
		Expect(k8sClient.Status().Patch(ctx, task, statusPatch)).To(Succeed())
		Expect(mgrClient.Get(ctx, types.NamespacedName{Name: seTaskName, Namespace: "default"}, task)).To(Succeed())
		return task
	}

	It("emits one attribute-complete, Plan-parented AGENT span and is idempotent", func() {
		createTaskPlan()
		task := createSETask(false)

		// TRACE-02: seed the immediate parent's (Plan) persisted span ID so
		// this level's span is properly parented, not an independent root.
		var parentPlan tideprojectv1alpha3.Plan
		Expect(mgrClient.Get(ctx, types.NamespacedName{Name: seTaskPlanName, Namespace: "default"}, &parentPlan)).To(Succeed())
		planPatch := client.MergeFrom(parentPlan.DeepCopy())
		parentPlan.Status.PlanTraceSpanID = seededParentSpanIDHex
		Expect(k8sClient.Status().Patch(ctx, &parentPlan, planPatch)).To(Succeed())
		// Wait for the manager's cache to observe this write before the
		// reconciler's own Get reads it below — same race guarded against at
		// every other level's seed step in this file.
		Eventually(func(g Gomega) {
			var synced tideprojectv1alpha3.Plan
			g.Expect(mgrClient.Get(ctx, types.NamespacedName{Name: seTaskPlanName, Namespace: "default"}, &synced)).To(Succeed())
			g.Expect(synced.Status.PlanTraceSpanID).To(Equal(seededParentSpanIDHex))
		}, 5*time.Second, 50*time.Millisecond).Should(Succeed())

		var proj tideprojectv1alpha3.Project
		Expect(mgrClient.Get(ctx, types.NamespacedName{Name: seTaskProjName, Namespace: "default"}, &proj)).To(Succeed())

		start := time.Now().Add(-5 * time.Minute)
		end := time.Now()
		jobName := fmt.Sprintf("tide-task-%s-1", task.UID)
		job := succeededPlannerJob(jobName, start, end)

		envReader.SetOut(string(task.UID), pkgdispatch.EnvelopeOut{
			TaskUID: string(task.UID),
			Result:  "success",
			Usage: pkgdispatch.Usage{
				InputTokens:         700,
				OutputTokens:        300,
				CacheReadTokens:     200,
				CacheCreationTokens: 100,
			},
			CompletedAt: end,
		})

		r := newTaskReconcilerForSpanEmission()

		// First call: emits the span.
		_, err := r.handleJobCompletion(ctx, task, &proj, job)
		Expect(err).NotTo(HaveOccurred())

		spans := exp.GetSpans()
		Expect(spans).To(HaveLen(1), "exactly one span must be recorded on first observation")
		span := spans[0]
		Expect(span.Name).To(Equal("tide.dispatch.task"))
		Expect(span.StartTime).To(BeTemporally("==", start))
		Expect(span.EndTime).To(BeTemporally("==", end))
		Expect(span.Status.Code).To(Equal(codes.Ok))

		roleVal, ok := attrValue(span.Attributes, "tide.role")
		Expect(ok).To(BeTrue(), "span missing tide.role")
		Expect(roleVal.AsString()).To(Equal("executor"), "Task's role is executor, not planner (POOL-01)")

		levelVal, ok := attrValue(span.Attributes, "tide.invocation.level")
		Expect(ok).To(BeTrue(), "span missing tide.invocation.level")
		Expect(levelVal.AsString()).To(Equal("task"))

		kindVal, ok := attrValue(span.Attributes, "openinference.span.kind")
		Expect(ok).To(BeTrue(), "span missing openinference.span.kind")
		Expect(kindVal.AsString()).To(Equal("AGENT"))

		modelVal, ok := attrValue(span.Attributes, "llm.model_name")
		Expect(ok).To(BeTrue(), "span missing llm.model_name")
		Expect(modelVal.AsString()).To(Equal("claude-test-model"))

		promptVal, ok := attrValue(span.Attributes, "llm.token_count.prompt")
		Expect(ok).To(BeTrue(), "span missing llm.token_count.prompt")
		Expect(promptVal.AsInt64()).To(BeNumerically("==", 1000))

		totalVal, ok := attrValue(span.Attributes, "llm.token_count.total")
		Expect(ok).To(BeTrue(), "span missing llm.token_count.total")
		Expect(totalVal.AsInt64()).To(BeNumerically("==", 1300))

		// TRACE-02: deterministic TraceID derived from Project.UID, and real
		// parent linkage to the seeded PlanTraceSpanID.
		expectedTID, tErr := otelai.TraceIDFromUID(string(proj.UID))
		Expect(tErr).NotTo(HaveOccurred())
		Expect(span.SpanContext.TraceID().String()).To(Equal(expectedTID.String()))
		Expect(span.Parent.SpanID().String()).To(Equal(seededParentSpanIDHex))
		Expect(span.Parent.IsRemote()).To(BeTrue())

		Eventually(func(g Gomega) {
			var fresh tideprojectv1alpha3.Task
			g.Expect(mgrClient.Get(ctx, types.NamespacedName{Name: seTaskName, Namespace: "default"}, &fresh)).To(Succeed())
			g.Expect(fresh.Status.TaskSpanEmittedUID).To(Equal(string(job.UID)),
				"TaskSpanEmittedUID must be set to the executor Job UID")
			// PROP-02: this level's own span ID is durably persisted, read
			// fresh from the API (not the in-memory object).
			g.Expect(fresh.Status.TaskTraceSpanID).To(Equal(span.SpanContext.SpanID().String()),
				"TaskTraceSpanID must be set to this level's own synthesized SpanID")
		}, 5*time.Second, 100*time.Millisecond).Should(Succeed())

		// Second call with the SAME completedJob, after re-fetching task
		// (marker now set) — D-02/Pitfall 2 generalized: no duplicate span.
		Expect(mgrClient.Get(ctx, types.NamespacedName{Name: seTaskName, Namespace: "default"}, task)).To(Succeed())
		_, err = r.handleJobCompletion(ctx, task, &proj, job)
		Expect(err).NotTo(HaveOccurred())
		Expect(exp.GetSpans()).To(HaveLen(1), "second call with the same Job must not emit a duplicate span")
	})

	It("failed Job → Error span with FailureDetail attributes", func() {
		createTaskPlan()
		task := createSETask(false)

		var proj tideprojectv1alpha3.Project
		Expect(mgrClient.Get(ctx, types.NamespacedName{Name: seTaskProjName, Namespace: "default"}, &proj)).To(Succeed())

		start := time.Now().Add(-5 * time.Minute)
		failedAt := time.Now()
		jobName := fmt.Sprintf("tide-task-%s-1", task.UID)
		job := failedPlannerJob(jobName, start, failedAt)

		envReader.SetOut(string(task.UID), pkgdispatch.EnvelopeOut{
			TaskUID:     string(task.UID),
			ExitCode:    2,
			Reason:      "cap-hit",
			Result:      "cap-hit",
			CompletedAt: failedAt,
		})

		r := newTaskReconcilerForSpanEmission()
		_, err := r.handleJobCompletion(ctx, task, &proj, job)
		Expect(err).NotTo(HaveOccurred())

		spans := exp.GetSpans()
		Expect(spans).To(HaveLen(1))
		span := spans[0]
		Expect(span.Status.Code).To(Equal(codes.Error))
		Expect(span.Status.Description).To(Equal("cap-hit"))
		Expect(span.EndTime).To(BeTemporally("==", failedAt))

		exitCodeVal, ok := attrValue(span.Attributes, "tide.exit_code")
		Expect(ok).To(BeTrue(), "span missing tide.exit_code")
		Expect(exitCodeVal.AsInt64()).To(BeNumerically("==", 2))

		reasonVal, ok := attrValue(span.Attributes, "tide.reason")
		Expect(ok).To(BeTrue(), "span missing tide.reason")
		Expect(reasonVal.AsString()).To(Equal("cap-hit"))
	})

	It("nil completedJob → zero spans", func() {
		createTaskPlan()
		task := createSETask(false)

		var proj tideprojectv1alpha3.Project
		Expect(mgrClient.Get(ctx, types.NamespacedName{Name: seTaskProjName, Namespace: "default"}, &proj)).To(Succeed())

		r := newTaskReconcilerForSpanEmission()
		_, err := r.handleJobCompletion(ctx, task, &proj, nil)
		Expect(err).NotTo(HaveOccurred())

		Expect(exp.GetSpans()).To(BeEmpty())

		var fresh tideprojectv1alpha3.Task
		Expect(mgrClient.Get(ctx, types.NamespacedName{Name: seTaskName, Namespace: "default"}, &fresh)).To(Succeed())
		Expect(fresh.Status.TaskSpanEmittedUID).To(BeEmpty())
	})

	It("EnvelopeReadFailed → degraded span AND Task still lands Failed/EnvelopeReadFailed (D-07)", func() {
		createTaskPlan()
		task := createSETask(false)

		var proj tideprojectv1alpha3.Project
		Expect(mgrClient.Get(ctx, types.NamespacedName{Name: seTaskProjName, Namespace: "default"}, &proj)).To(Succeed())

		start := time.Now().Add(-5 * time.Minute)
		end := time.Now()
		jobName := fmt.Sprintf("tide-task-%s-1", task.UID)
		job := succeededPlannerJob(jobName, start, end)

		// Deliberately no SetOut for task.UID: ReadOut returns "no envelope
		// out for task UID" — the ONLY Task terminal path reachable with
		// envReadOK=false (call site 1, inside the EnvelopeReadFailed
		// branch), generalized Option B (43-05-PLAN.md).

		r := newTaskReconcilerForSpanEmission()
		_, err := r.handleJobCompletion(ctx, task, &proj, job)
		Expect(err).NotTo(HaveOccurred())

		spans := exp.GetSpans()
		Expect(spans).To(HaveLen(1))
		span := spans[0]

		degradedVal, ok := attrValue(span.Attributes, "tide.envelope.degraded")
		Expect(ok).To(BeTrue(), "span missing tide.envelope.degraded")
		Expect(degradedVal.AsBool()).To(BeTrue())

		for _, key := range []attribute.Key{
			"llm.token_count.prompt", "llm.token_count.completion",
			"llm.token_count.prompt_details.cache_read", "llm.token_count.prompt_details.cache_write",
			"llm.token_count.total",
		} {
			_, found := attrValue(span.Attributes, key)
			Expect(found).To(BeFalse(), "degraded span must not carry token-count attribute %q", key)
		}

		// Existing behavior preserved: the Task still lands terminal
		// Failed/EnvelopeReadFailed — span emission is additive, not a
		// behavior change (43-05-PLAN.md Task 1 acceptance criteria).
		// Eventually: mgrClient reads through the manager's cache, which
		// syncs asynchronously after r.Status().Patch's write to the API
		// server (same convention as the persistence checks above).
		Eventually(func(g Gomega) {
			var fresh tideprojectv1alpha3.Task
			g.Expect(mgrClient.Get(ctx, types.NamespacedName{Name: seTaskName, Namespace: "default"}, &fresh)).To(Succeed())
			g.Expect(fresh.Status.Phase).To(Equal(tideprojectv1alpha3.LevelPhaseFailed))
			failedCond := meta.FindStatusCondition(fresh.Status.Conditions, tideprojectv1alpha3.ConditionFailed)
			g.Expect(failedCond).NotTo(BeNil(), "Task must carry a Failed condition")
			g.Expect(failedCond.Reason).To(Equal("EnvelopeReadFailed"))
		}, 5*time.Second, 100*time.Millisecond).Should(Succeed())
	})

	It("output-path-validation block entered → call-site-2 span already persisted through the skipped-validation fallback", func() {
		createTaskPlan()
		// setStartedAt=true activates the output-path-validation block
		// (task_controller.go ~962: DeclaredOutputPaths non-empty AND
		// Status.StartedAt != nil). The hardcoded taskWorkspaceRoot
		// ("/workspaces/<project.UID>/workspace") does not exist in envtest
		// and cannot be created here (this sandbox's root filesystem is
		// read-only outside /tmp and the repo checkout), so
		// validateControllerOutputPaths degrades to skipped=true
		// (errors.Is(err, fs.ErrNotExist)) rather than a real vErr or
		// violations — the same environmental limit the pre-existing
		// TestTaskReconciler_OnJobSucceeded_FlagsOutputPathsViolation test
		// already hedges on (task_controller_test.go, "Either way, task
		// moves to terminal state"). What IS provable here: call site 2 sits
		// BEFORE this block, so entering and falling through it neither
		// duplicates nor loses the already-emitted span.
		task := createSETask(true)

		var proj tideprojectv1alpha3.Project
		Expect(mgrClient.Get(ctx, types.NamespacedName{Name: seTaskProjName, Namespace: "default"}, &proj)).To(Succeed())

		start := time.Now().Add(-5 * time.Minute)
		end := time.Now()
		jobName := fmt.Sprintf("tide-task-%s-1", task.UID)
		job := succeededPlannerJob(jobName, start, end)

		envReader.SetOut(string(task.UID), pkgdispatch.EnvelopeOut{
			TaskUID:     string(task.UID),
			Result:      "success",
			CompletedAt: end,
			Usage: pkgdispatch.Usage{
				InputTokens:  500,
				OutputTokens: 100,
			},
		})

		r := newTaskReconcilerForSpanEmission()
		_, err := r.handleJobCompletion(ctx, task, &proj, job)
		Expect(err).NotTo(HaveOccurred())

		spans := exp.GetSpans()
		Expect(spans).To(HaveLen(1),
			"call site 2 must emit exactly one span even when the output-path-validation block subsequently runs")
		span := spans[0]
		Expect(span.Status.Code).To(Equal(codes.Ok))

		promptVal, ok := attrValue(span.Attributes, "llm.token_count.prompt")
		Expect(ok).To(BeTrue(), "envReadOK=true span must still carry token-count attributes")
		Expect(promptVal.AsInt64()).To(BeNumerically("==", 500))

		// Task still reaches a terminal phase — the validation block's
		// skipped fallback does not wedge the reconcile.
		Eventually(func(g Gomega) {
			var fresh tideprojectv1alpha3.Task
			g.Expect(mgrClient.Get(ctx, types.NamespacedName{Name: seTaskName, Namespace: "default"}, &fresh)).To(Succeed())
			g.Expect(fresh.Status.Phase).To(Equal(tideprojectv1alpha3.LevelPhaseSucceeded))
		}, 5*time.Second, 100*time.Millisecond).Should(Succeed())
	})
})
