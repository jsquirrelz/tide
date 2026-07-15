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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	tideprojectv1alpha3 "github.com/jsquirrelz/tide/api/v1alpha3"
	pkgdispatch "github.com/jsquirrelz/tide/pkg/dispatch"
)

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
// Status fields off the object it is handed directly.
func succeededPlannerJob(name string, start, end time.Time) *batchv1.Job {
	return &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{Name: name},
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
func failedPlannerJob(name string, start, failedAt time.Time) *batchv1.Job {
	return &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{Name: name},
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
		spanEmissionProject(ctx, seMSProjName, "claude-test-model")
		envReader = newMapEnvReader()

		exp = tracetest.NewInMemoryExporter()
		prevTP = otel.GetTracerProvider()
		otel.SetTracerProvider(sdktrace.NewTracerProvider(sdktrace.WithSyncer(exp)))
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

		Eventually(func(g Gomega) {
			var fresh tideprojectv1alpha3.Milestone
			g.Expect(mgrClient.Get(ctx, types.NamespacedName{Name: seMSName, Namespace: "default"}, &fresh)).To(Succeed())
			g.Expect(fresh.Status.MilestoneSpanEmittedUID).To(Equal(jobName),
				"MilestoneSpanEmittedUID must be set to the planner Job name")
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

		Expect(exp.GetSpans()).To(HaveLen(0))

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
		spanEmissionProject(ctx, sePHProjName, "claude-test-model")
		ms := &tideprojectv1alpha3.Milestone{
			ObjectMeta: metav1.ObjectMeta{Name: sePHMSName, Namespace: "default"},
			Spec:       tideprojectv1alpha3.MilestoneSpec{ProjectRef: sePHProjName},
		}
		Expect(k8sClient.Create(ctx, ms)).To(Succeed())
		waitForCacheSync(sePHMSName, "default", &tideprojectv1alpha3.Milestone{})
		envReader = newMapEnvReader()

		exp = tracetest.NewInMemoryExporter()
		prevTP = otel.GetTracerProvider()
		otel.SetTracerProvider(sdktrace.NewTracerProvider(sdktrace.WithSyncer(exp)))
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

		Eventually(func(g Gomega) {
			var fresh tideprojectv1alpha3.Phase
			g.Expect(mgrClient.Get(ctx, types.NamespacedName{Name: sePHName, Namespace: "default"}, &fresh)).To(Succeed())
			g.Expect(fresh.Status.PhaseSpanEmittedUID).To(Equal(jobName),
				"PhaseSpanEmittedUID must be set to the planner Job name")
		}, 5*time.Second, 100*time.Millisecond).Should(Succeed())

		// Second call with the SAME completedJob, after re-fetching ph — no
		// duplicate span (D-02/Pitfall 2).
		Expect(mgrClient.Get(ctx, types.NamespacedName{Name: sePHName, Namespace: "default"}, ph)).To(Succeed())
		_, err = r.handleJobCompletion(ctx, ph, job)
		Expect(err).NotTo(HaveOccurred())
		Expect(exp.GetSpans()).To(HaveLen(1), "second call with the same Job must not emit a duplicate span")
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

		Expect(exp.GetSpans()).To(HaveLen(0))

		var fresh tideprojectv1alpha3.Phase
		Expect(mgrClient.Get(ctx, types.NamespacedName{Name: sePHName, Namespace: "default"}, &fresh)).To(Succeed())
		Expect(fresh.Status.PhaseSpanEmittedUID).To(BeEmpty())
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

		exp = tracetest.NewInMemoryExporter()
		prevTP = otel.GetTracerProvider()
		otel.SetTracerProvider(sdktrace.NewTracerProvider(sdktrace.WithSyncer(exp)))
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

		Eventually(func(g Gomega) {
			var fresh tideprojectv1alpha3.Plan
			g.Expect(mgrClient.Get(ctx, types.NamespacedName{Name: sePlanName, Namespace: "default"}, &fresh)).To(Succeed())
			g.Expect(fresh.Status.PlanSpanEmittedUID).To(Equal(jobName),
				"PlanSpanEmittedUID must be set to the planner Job name")
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

		Expect(exp.GetSpans()).To(HaveLen(0))

		var fresh tideprojectv1alpha3.Plan
		Expect(mgrClient.Get(ctx, types.NamespacedName{Name: sePlanName, Namespace: "default"}, &fresh)).To(Succeed())
		Expect(fresh.Status.PlanSpanEmittedUID).To(BeEmpty())
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
		spanEmissionProject(ctx, seProjName, "claude-test-model")
		envReader = newMapEnvReader()

		exp = tracetest.NewInMemoryExporter()
		prevTP = otel.GetTracerProvider()
		otel.SetTracerProvider(sdktrace.NewTracerProvider(sdktrace.WithSyncer(exp)))
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

		Eventually(func(g Gomega) {
			var fresh tideprojectv1alpha3.Project
			g.Expect(mgrClient.Get(ctx, types.NamespacedName{Name: seProjName, Namespace: "default"}, &fresh)).To(Succeed())
			g.Expect(fresh.Status.PlannerSpanEmittedUID).To(Equal(jobName),
				"PlannerSpanEmittedUID must be set to the planner Job name")
		}, 5*time.Second, 100*time.Millisecond).Should(Succeed())

		// Second call with the SAME completedJob, after re-fetching proj (marker
		// now set) — D-02/Pitfall 2: no duplicate span. Asserts span count == 1
		// AND the marker remains stamped with the Job name (plan 42-05 acceptance
		// criteria).
		Expect(mgrClient.Get(ctx, types.NamespacedName{Name: seProjName, Namespace: "default"}, proj)).To(Succeed())
		_, err = r.handleProjectJobCompletion(ctx, proj, job)
		Expect(err).NotTo(HaveOccurred())
		Expect(exp.GetSpans()).To(HaveLen(1), "second call with the same Job must not emit a duplicate span")

		var fresh tideprojectv1alpha3.Project
		Expect(mgrClient.Get(ctx, types.NamespacedName{Name: seProjName, Namespace: "default"}, &fresh)).To(Succeed())
		Expect(fresh.Status.PlannerSpanEmittedUID).To(Equal(jobName),
			"PlannerSpanEmittedUID must remain set to the planner Job name after the idempotent second call")
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

		Expect(exp.GetSpans()).To(HaveLen(0))

		var fresh tideprojectv1alpha3.Project
		Expect(mgrClient.Get(ctx, types.NamespacedName{Name: seProjName, Namespace: "default"}, &fresh)).To(Succeed())
		Expect(fresh.Status.PlannerSpanEmittedUID).To(BeEmpty())
	})
})
