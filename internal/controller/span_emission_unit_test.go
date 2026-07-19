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

// Phase 42 Plan 04 Task 1: failing tests (TDD RED) for the shared
// retroactive span-synthesis helper (D-01..D-04, ATTR-01/ATTR-02) that
// span_emission.go implements. Plain testing.T functions — pure/K8s-object
// inputs never need envtest — so they run without pulling in the package's
// Ginkgo BeforeSuite (which only fires inside RunSpecs) via
// `go test ./internal/controller/ -run 'TestSpanEndTime|TestSynthesizePlannerSpan'`.
package controller

import (
	"context"
	"encoding/json"
	"slices"
	"testing"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
	tracenoop "go.opentelemetry.io/otel/trace/noop"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	tideprojectv1alpha3 "github.com/jsquirrelz/tide/api/v1alpha3"
	pkgdispatch "github.com/jsquirrelz/tide/pkg/dispatch"
	"github.com/jsquirrelz/tide/pkg/otelai"
)

// testProjectUID is a canonical, hyphenated K8s-UID-shaped fixture UID (32
// hex chars once dashes are stripped) — the shape otelai.TraceIDFromUID
// requires to derive a valid, non-zero deterministic TraceID. Tests that
// need synthesizePlannerSpan to actually emit (i.e. a non-nil project) use
// this exact UID unless the test is specifically about a different UID.
const testProjectUID = "11111111-1111-1111-1111-111111111111"

// fixtureProject builds a minimal Project with a valid-shaped UID and the
// given model, sufficient for synthesizePlannerSpan's nil-safety and
// deterministic-TraceID requirements.
func spanEmissionFixtureProject(uid, model string) *tideprojectv1alpha3.Project {
	return &tideprojectv1alpha3.Project{
		ObjectMeta: metav1.ObjectMeta{UID: types.UID(uid)},
		Spec: tideprojectv1alpha3.ProjectSpec{
			Subagent: tideprojectv1alpha3.SubagentConfig{Model: model},
		},
	}
}

// setupSpanExporter swaps the global TracerProvider for an in-memory one
// (synchronous WithSyncer — no flush needed), restoring the previous
// provider via t.Cleanup. tracetest ships inside the already-pinned
// go.opentelemetry.io/otel/sdk — zero go.mod changes.
func setupSpanExporter(t *testing.T) *tracetest.InMemoryExporter {
	t.Helper()
	exp := tracetest.NewInMemoryExporter()
	prev := otel.GetTracerProvider()
	otel.SetTracerProvider(sdktrace.NewTracerProvider(sdktrace.WithSyncer(exp)))
	t.Cleanup(func() { otel.SetTracerProvider(prev) })
	return exp
}

// attrValue scans a span's attributes for key, returning the raw value and
// whether it was found.
func attrValue(attrs []attribute.KeyValue, key attribute.Key) (attribute.Value, bool) {
	for _, a := range attrs {
		if a.Key == key {
			return a.Value, true
		}
	}
	return attribute.Value{}, false
}

func mustTime(t *testing.T, s string) time.Time {
	t.Helper()
	tm, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t.Fatalf("parse time %q: %v", s, err)
	}
	return tm
}

// ─── spanEndTime ───────────────────────────────────────────────────────────

func TestSpanEndTimeNilJob(t *testing.T) {
	got, ok := spanEndTime(nil)
	if ok {
		t.Errorf("spanEndTime(nil) ok = true, want false")
	}
	if !got.IsZero() {
		t.Errorf("spanEndTime(nil) time = %v, want zero", got)
	}
}

func TestSpanEndTimeSucceeded(t *testing.T) {
	want := mustTime(t, "2026-07-15T10:00:00Z")
	job := &batchv1.Job{
		Status: batchv1.JobStatus{
			CompletionTime: &metav1.Time{Time: want},
		},
	}
	got, ok := spanEndTime(job)
	if !ok {
		t.Fatalf("spanEndTime(succeeded) ok = false, want true")
	}
	if !got.Equal(want) {
		t.Errorf("spanEndTime(succeeded) = %v, want %v", got, want)
	}
}

// TestSpanEndTimeFailed — Pitfall 1: CompletionTime is nil on every failed
// Job; the end timestamp must fall back to the JobFailed condition's
// LastTransitionTime.
func TestSpanEndTimeFailed(t *testing.T) {
	want := mustTime(t, "2026-07-15T11:00:00Z")
	job := &batchv1.Job{
		Status: batchv1.JobStatus{
			CompletionTime: nil,
			Conditions: []batchv1.JobCondition{
				{Type: batchv1.JobFailed, Status: corev1.ConditionTrue, LastTransitionTime: metav1.Time{Time: want}},
			},
		},
	}
	got, ok := spanEndTime(job)
	if !ok {
		t.Fatalf("spanEndTime(failed) ok = false, want true")
	}
	if !got.Equal(want) {
		t.Errorf("spanEndTime(failed) = %v, want %v", got, want)
	}
}

func TestSpanEndTimeNeitherCondition(t *testing.T) {
	job := &batchv1.Job{Status: batchv1.JobStatus{}}
	got, ok := spanEndTime(job)
	if ok {
		t.Errorf("spanEndTime(no terminal condition) ok = true, want false")
	}
	if !got.IsZero() {
		t.Errorf("spanEndTime(no terminal condition) time = %v, want zero", got)
	}
}

// ─── plannerSpanResolvable ─────────────────────────────────────────────────

// TestPlannerSpanResolvable — 42-REVIEW WR-01: the mark-then-emit entry gate.
// A stamp is only allowed when a span is guaranteed emittable (non-nil Job,
// StartTime populated, end timestamp resolvable), so a durable marker can
// never suppress a span that was never exported.
func TestPlannerSpanResolvable(t *testing.T) {
	start := mustTime(t, "2026-07-15T10:00:00Z")
	end := mustTime(t, "2026-07-15T10:05:00Z")

	cases := []struct {
		name string
		job  *batchv1.Job
		want bool
	}{
		{name: "nil job (TTL-GC'd)", job: nil, want: false},
		{
			name: "no StartTime",
			job: &batchv1.Job{Status: batchv1.JobStatus{
				CompletionTime: &metav1.Time{Time: end},
			}},
			want: false,
		},
		{
			name: "no resolvable end timestamp",
			job: &batchv1.Job{Status: batchv1.JobStatus{
				StartTime: &metav1.Time{Time: start},
			}},
			want: false,
		},
		{
			name: "succeeded (CompletionTime)",
			job: &batchv1.Job{Status: batchv1.JobStatus{
				StartTime:      &metav1.Time{Time: start},
				CompletionTime: &metav1.Time{Time: end},
			}},
			want: true,
		},
		{
			name: "failed (JobFailed LastTransitionTime)",
			job: &batchv1.Job{Status: batchv1.JobStatus{
				StartTime: &metav1.Time{Time: start},
				Conditions: []batchv1.JobCondition{
					{Type: batchv1.JobFailed, Status: corev1.ConditionTrue, LastTransitionTime: metav1.Time{Time: end}},
				},
			}},
			want: true,
		},
	}
	for _, tc := range cases {
		if got := plannerSpanResolvable(tc.job); got != tc.want {
			t.Errorf("plannerSpanResolvable(%s) = %v, want %v", tc.name, got, tc.want)
		}
	}
}

// ─── synthesizePlannerSpan ─────────────────────────────────────────────────

func TestSynthesizePlannerSpanNilJob(t *testing.T) {
	exp := setupSpanExporter(t)
	gotID, _, gotOK := synthesizePlannerSpan(context.Background(), "milestone", "ms-1", "", nil, ProviderDefaults{}, nil, pkgdispatch.EnvelopeOut{}, false, trace.SpanID{})
	if gotOK {
		t.Errorf("synthesizePlannerSpan(nil job) ok = true, want false")
	}
	if gotID.IsValid() {
		t.Errorf("synthesizePlannerSpan(nil job) spanID = %v, want zero", gotID)
	}
	if len(exp.GetSpans()) != 0 {
		t.Errorf("synthesizePlannerSpan(nil job) recorded %d spans, want 0", len(exp.GetSpans()))
	}
}

// TestSynthesizePlannerSpanSucceededComplete — the ATTR-01/ATTR-02 happy
// path: attribute-complete AGENT span, exact StartTime/EndTime from the
// Job's own timestamps. 46 D-03: updated deliberately (not deleted, per the
// MSG-03 precedent) — the llm.token_count.* want-attrs this test asserted
// pre-Phase-46 are removed; TestSynthesizePlannerSpanOmitsTokenCount below is
// the new regression guard proving their absence at every level. This test
// now also proves the 46 D-05/OBS-02/OBS-03 enrichment triple.
func TestSynthesizePlannerSpanSucceededComplete(t *testing.T) {
	exp := setupSpanExporter(t)

	start := mustTime(t, "2026-07-15T10:00:00Z")
	end := mustTime(t, "2026-07-15T10:05:00Z")
	job := &batchv1.Job{
		Status: batchv1.JobStatus{
			StartTime:      &metav1.Time{Time: start},
			CompletionTime: &metav1.Time{Time: end},
			Conditions: []batchv1.JobCondition{
				{Type: batchv1.JobComplete, Status: corev1.ConditionTrue},
			},
		},
	}
	project := spanEmissionFixtureProject(testProjectUID, "claude-test-model")
	out := pkgdispatch.EnvelopeOut{
		Usage: pkgdispatch.Usage{
			InputTokens:         700,
			OutputTokens:        300,
			CacheReadTokens:     200,
			CacheCreationTokens: 100,
		},
	}

	_, _, gotOK := synthesizePlannerSpan(context.Background(), "milestone", "test-milestone-1", "", project, ProviderDefaults{}, job, out, true, trace.SpanID{})
	if !gotOK {
		t.Fatalf("synthesizePlannerSpan(succeeded) = false, want true")
	}

	spans := exp.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("synthesizePlannerSpan(succeeded) recorded %d spans, want exactly 1", len(spans))
	}
	span := spans[0]

	if span.Name != "tide.dispatch.milestone" {
		t.Errorf("span.Name = %q, want %q", span.Name, "tide.dispatch.milestone")
	}
	if !span.StartTime.Equal(start) {
		t.Errorf("span.StartTime = %v, want %v", span.StartTime, start)
	}
	if !span.EndTime.Equal(end) {
		t.Errorf("span.EndTime = %v, want %v", span.EndTime, end)
	}
	if span.Status.Code != codes.Ok {
		t.Errorf("span.Status.Code = %v, want codes.Ok", span.Status.Code)
	}

	wantStringAttrs := map[attribute.Key]string{
		"llm.model_name":          "claude-test-model",
		"llm.provider":            "anthropic",
		"llm.system":              "anthropic",
		"openinference.span.kind": "AGENT",
		"tide.role":               "planner",
		"tide.invocation.level":   "milestone",
		"session.id":              testProjectUID,
	}
	for key, want := range wantStringAttrs {
		val, ok := attrValue(span.Attributes, key)
		if !ok {
			t.Errorf("span missing attribute %q", key)
			continue
		}
		if val.AsString() != want {
			t.Errorf("attribute %q = %q, want %q", key, val.AsString(), want)
		}
	}

	// 46 OBS-03: metadata is a JSON string containing level/name.
	metaVal, ok := attrValue(span.Attributes, "metadata")
	if !ok {
		t.Fatalf("span missing attribute %q", "metadata")
	}
	var metaDecoded map[string]string
	if err := json.Unmarshal([]byte(metaVal.AsString()), &metaDecoded); err != nil {
		t.Fatalf("metadata attribute is not valid JSON: %v (value=%q)", err, metaVal.AsString())
	}
	if metaDecoded["level"] != "milestone" {
		t.Errorf("metadata[level] = %q, want %q", metaDecoded["level"], "milestone")
	}
	if metaDecoded["name"] != "test-milestone-1" {
		t.Errorf("metadata[name] = %q, want %q", metaDecoded["name"], "test-milestone-1")
	}

	// 46 OBS-03/Pitfall 4: tag.tags is a native STRINGSLICE containing the level.
	tagsVal, ok := attrValue(span.Attributes, "tag.tags")
	if !ok {
		t.Fatalf("span missing attribute %q", "tag.tags")
	}
	if tagsVal.Type() != attribute.STRINGSLICE {
		t.Errorf("tag.tags attribute Type() = %v, want %v", tagsVal.Type(), attribute.STRINGSLICE)
	}
	foundLevelTag := false
	for _, tg := range tagsVal.AsStringSlice() {
		if tg == "milestone" {
			foundLevelTag = true
		}
	}
	if !foundLevelTag {
		t.Errorf("tag.tags = %v, want to contain %q", tagsVal.AsStringSlice(), "milestone")
	}

	// 46 D-03: updated deliberately — AGENT spans no longer carry ANY
	// llm.token_count.* attribute, even with envReadOK=true and a populated
	// Usage. Per-call LLM spans (reporter tracesynth) are the sole source.
	for _, key := range []attribute.Key{
		"llm.token_count.prompt", "llm.token_count.completion",
		"llm.token_count.prompt_details.cache_read", "llm.token_count.prompt_details.cache_write",
		"llm.token_count.total",
	} {
		if _, found := attrValue(span.Attributes, key); found {
			t.Errorf("succeeded span unexpectedly carries token-count attribute %q (46 D-03)", key)
		}
	}
}

// TestSynthesizePlannerSpanOmitsTokenCount — 46 D-03 regression guard (the
// planner_correction evidence chain in 46-04-PLAN.md): for EVERY level, the
// emitted AGENT span carries NO llm.token_count.* attribute even with
// envReadOK=true and a fully-populated Usage. Per-call LLM spans (the
// reporter's synthesizeSpans, running on both the combined-mode path at all
// four planner levels and the trace-only path at Task) are the sole
// llm.token_count.* source — Phoenix creates a SpanCost row per span with no
// span-kind gate, so a rolled-up total here would double-count every level.
func TestSynthesizePlannerSpanOmitsTokenCount(t *testing.T) {
	start := mustTime(t, "2026-07-15T10:00:00Z")
	end := mustTime(t, "2026-07-15T10:05:00Z")

	for _, level := range []string{"project", "milestone", "phase", "plan", "task"} {
		t.Run(level, func(t *testing.T) {
			exp := setupSpanExporter(t)

			job := &batchv1.Job{
				Status: batchv1.JobStatus{
					StartTime:      &metav1.Time{Time: start},
					CompletionTime: &metav1.Time{Time: end},
					Conditions: []batchv1.JobCondition{
						{Type: batchv1.JobComplete, Status: corev1.ConditionTrue},
					},
				},
			}
			project := spanEmissionFixtureProject(testProjectUID, "claude-test-model")
			out := pkgdispatch.EnvelopeOut{
				Usage: pkgdispatch.Usage{
					InputTokens:         700,
					OutputTokens:        300,
					CacheReadTokens:     200,
					CacheCreationTokens: 100,
				},
			}

			_, _, gotOK := synthesizePlannerSpan(context.Background(), level, "test-"+level, "", project, ProviderDefaults{}, job, out, true, trace.SpanID{})
			if !gotOK {
				t.Fatalf("synthesizePlannerSpan(%s, envReadOK=true) = false, want true", level)
			}

			spans := exp.GetSpans()
			if len(spans) != 1 {
				t.Fatalf("recorded %d spans, want exactly 1", len(spans))
			}
			span := spans[0]

			for _, key := range []attribute.Key{
				"llm.token_count.prompt", "llm.token_count.completion",
				"llm.token_count.prompt_details.cache_read", "llm.token_count.prompt_details.cache_write",
				"llm.token_count.total",
			} {
				if _, found := attrValue(span.Attributes, key); found {
					t.Errorf("level %q: span unexpectedly carries token-count attribute %q", level, key)
				}
			}
		})
	}
}

// TestSynthesizePlannerSpanFailed — D-01/D-03: a failed Job still emits a
// span, status Error with the classified Reason as description, end
// timestamp from the JobFailed condition (CompletionTime stays nil).
func TestSynthesizePlannerSpanFailed(t *testing.T) {
	exp := setupSpanExporter(t)

	start := mustTime(t, "2026-07-15T10:00:00Z")
	failedAt := mustTime(t, "2026-07-15T10:02:00Z")
	job := &batchv1.Job{
		Status: batchv1.JobStatus{
			StartTime: &metav1.Time{Time: start},
			Conditions: []batchv1.JobCondition{
				{Type: batchv1.JobFailed, Status: corev1.ConditionTrue, LastTransitionTime: metav1.Time{Time: failedAt}},
			},
		},
	}
	out := pkgdispatch.EnvelopeOut{ExitCode: 2, Reason: "cap-hit"}
	project := spanEmissionFixtureProject(testProjectUID, "")

	_, _, gotOK := synthesizePlannerSpan(context.Background(), "milestone", "ms-1", "", project, ProviderDefaults{}, job, out, true, trace.SpanID{})
	if !gotOK {
		t.Fatalf("synthesizePlannerSpan(failed) = false, want true")
	}

	spans := exp.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("synthesizePlannerSpan(failed) recorded %d spans, want exactly 1", len(spans))
	}
	span := spans[0]

	if span.Status.Code != codes.Error {
		t.Errorf("span.Status.Code = %v, want codes.Error", span.Status.Code)
	}
	if span.Status.Description != "cap-hit" {
		t.Errorf("span.Status.Description = %q, want %q", span.Status.Description, "cap-hit")
	}
	if !span.EndTime.Equal(failedAt) {
		t.Errorf("span.EndTime = %v, want %v (JobFailed LastTransitionTime)", span.EndTime, failedAt)
	}

	exitCode, ok := attrValue(span.Attributes, "tide.exit_code")
	if !ok || exitCode.AsInt64() != 2 {
		t.Errorf("tide.exit_code attribute = %v (found=%v), want 2", exitCode, ok)
	}
	reason, ok := attrValue(span.Attributes, "tide.reason")
	if !ok || reason.AsString() != "cap-hit" {
		t.Errorf("tide.reason attribute = %v (found=%v), want %q", reason, ok, "cap-hit")
	}
}

// TestSynthesizePlannerSpanDegradedEnvelope — D-04: envReadOK=false still
// emits a span carrying the degradation marker, zero token-count
// attributes, but the model name still resolves (ResolveProvider is
// envelope-independent).
func TestSynthesizePlannerSpanDegradedEnvelope(t *testing.T) {
	exp := setupSpanExporter(t)

	start := mustTime(t, "2026-07-15T10:00:00Z")
	end := mustTime(t, "2026-07-15T10:05:00Z")
	job := &batchv1.Job{
		Status: batchv1.JobStatus{
			StartTime:      &metav1.Time{Time: start},
			CompletionTime: &metav1.Time{Time: end},
			Conditions: []batchv1.JobCondition{
				{Type: batchv1.JobComplete, Status: corev1.ConditionTrue},
			},
		},
	}
	project := spanEmissionFixtureProject(testProjectUID, "claude-test-model")

	_, _, gotOK := synthesizePlannerSpan(context.Background(), "milestone", "ms-1", "", project, ProviderDefaults{}, job, pkgdispatch.EnvelopeOut{}, false, trace.SpanID{})
	if !gotOK {
		t.Fatalf("synthesizePlannerSpan(degraded) = false, want true")
	}

	spans := exp.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("synthesizePlannerSpan(degraded) recorded %d spans, want exactly 1", len(spans))
	}
	span := spans[0]

	degraded, ok := attrValue(span.Attributes, "tide.envelope.degraded")
	if !ok || !degraded.AsBool() {
		t.Errorf("tide.envelope.degraded attribute = %v (found=%v), want true", degraded, ok)
	}
	model, ok := attrValue(span.Attributes, "llm.model_name")
	if !ok || model.AsString() != "claude-test-model" {
		t.Errorf("llm.model_name attribute = %v (found=%v), want %q (D-04: model survives degradation)", model, ok, "claude-test-model")
	}
	for _, key := range []attribute.Key{
		"llm.token_count.prompt", "llm.token_count.completion",
		"llm.token_count.prompt_details.cache_read", "llm.token_count.prompt_details.cache_write",
		"llm.token_count.total",
	} {
		if _, ok := attrValue(span.Attributes, key); ok {
			t.Errorf("degraded span unexpectedly carries token-count attribute %q", key)
		}
	}
}

// TestSynthesizePlannerSpanNilProjectSkipsEmission — TRACE-02/Pitfall 5:
// Phase 43 deliberately changes Phase 42's nil-tolerant emission. A nil
// project has no deterministic TraceID, so emitting a span for it would
// break the one-trace-per-Project guarantee — span loss is preferred over
// an unanchored span; the caller gets (zero SpanID, false) and no span is
// exported.
func TestSynthesizePlannerSpanNilProjectSkipsEmission(t *testing.T) {
	exp := setupSpanExporter(t)

	start := mustTime(t, "2026-07-15T10:00:00Z")
	end := mustTime(t, "2026-07-15T10:05:00Z")
	job := &batchv1.Job{
		Status: batchv1.JobStatus{
			StartTime:      &metav1.Time{Time: start},
			CompletionTime: &metav1.Time{Time: end},
			Conditions: []batchv1.JobCondition{
				{Type: batchv1.JobComplete, Status: corev1.ConditionTrue},
			},
		},
	}

	gotID, _, gotOK := synthesizePlannerSpan(context.Background(), "milestone", "ms-1", "", nil, ProviderDefaults{}, job, pkgdispatch.EnvelopeOut{}, false, trace.SpanID{})
	if gotOK {
		t.Errorf("synthesizePlannerSpan(nil project) ok = true, want false")
	}
	if gotID.IsValid() {
		t.Errorf("synthesizePlannerSpan(nil project) spanID = %v, want zero", gotID)
	}
	if len(exp.GetSpans()) != 0 {
		t.Errorf("synthesizePlannerSpan(nil project) recorded %d spans, want 0 (no deterministic TraceID available)", len(exp.GetSpans()))
	}
}

// ─── parenting / TraceID / return-value coverage (Phase 43 TRACE-02) ───────

// TestSynthesizePlannerSpanDeterministicTraceID — every emitted span's
// TraceID equals otelai.TraceIDFromUID(project.UID), independent of level or
// parentSpanID (D-01).
func TestSynthesizePlannerSpanDeterministicTraceID(t *testing.T) {
	exp := setupSpanExporter(t)

	start := mustTime(t, "2026-07-15T10:00:00Z")
	end := mustTime(t, "2026-07-15T10:05:00Z")
	job := &batchv1.Job{
		Status: batchv1.JobStatus{
			StartTime:      &metav1.Time{Time: start},
			CompletionTime: &metav1.Time{Time: end},
			Conditions: []batchv1.JobCondition{
				{Type: batchv1.JobComplete, Status: corev1.ConditionTrue},
			},
		},
	}
	project := spanEmissionFixtureProject(testProjectUID, "claude-test-model")
	expectedTID, err := otelai.TraceIDFromUID(string(project.UID))
	if err != nil {
		t.Fatalf("otelai.TraceIDFromUID(%q): %v", project.UID, err)
	}

	_, _, gotOK := synthesizePlannerSpan(context.Background(), "milestone", "ms-1", "", project, ProviderDefaults{}, job, pkgdispatch.EnvelopeOut{}, false, trace.SpanID{})
	if !gotOK {
		t.Fatalf("synthesizePlannerSpan = false, want true")
	}

	spans := exp.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("recorded %d spans, want exactly 1", len(spans))
	}
	if got := spans[0].SpanContext.TraceID(); got != expectedTID {
		t.Errorf("span TraceID = %v, want %v (otelai.TraceIDFromUID(project.UID))", got, expectedTID)
	}
}

// TestSynthesizePlannerSpanParentLinkage — a non-zero parentSpanID produces
// a span whose Parent.SpanID() equals it (real parent-child threading,
// RESEARCH Pattern 2 — one SpanContext construction, no custom IDGenerator).
func TestSynthesizePlannerSpanParentLinkage(t *testing.T) {
	exp := setupSpanExporter(t)

	start := mustTime(t, "2026-07-15T10:00:00Z")
	end := mustTime(t, "2026-07-15T10:05:00Z")
	job := &batchv1.Job{
		Status: batchv1.JobStatus{
			StartTime:      &metav1.Time{Time: start},
			CompletionTime: &metav1.Time{Time: end},
			Conditions: []batchv1.JobCondition{
				{Type: batchv1.JobComplete, Status: corev1.ConditionTrue},
			},
		},
	}
	project := spanEmissionFixtureProject(testProjectUID, "claude-test-model")
	parentSpanID, err := trace.SpanIDFromHex("0102030405060708")
	if err != nil {
		t.Fatalf("trace.SpanIDFromHex: %v", err)
	}

	_, _, gotOK := synthesizePlannerSpan(context.Background(), "phase", "ph-1", "", project, ProviderDefaults{}, job, pkgdispatch.EnvelopeOut{}, false, parentSpanID)
	if !gotOK {
		t.Fatalf("synthesizePlannerSpan = false, want true")
	}

	spans := exp.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("recorded %d spans, want exactly 1", len(spans))
	}
	if got := spans[0].Parent.SpanID(); got != parentSpanID {
		t.Errorf("span Parent.SpanID() = %v, want %v", got, parentSpanID)
	}
}

// TestSynthesizePlannerSpanRootWhenParentZero — a zero parentSpanID (D-02:
// Project's own case) produces a span with no valid parent, while the
// TraceID is still the deterministic one derived from Project.UID.
func TestSynthesizePlannerSpanRootWhenParentZero(t *testing.T) {
	exp := setupSpanExporter(t)

	start := mustTime(t, "2026-07-15T10:00:00Z")
	end := mustTime(t, "2026-07-15T10:05:00Z")
	job := &batchv1.Job{
		Status: batchv1.JobStatus{
			StartTime:      &metav1.Time{Time: start},
			CompletionTime: &metav1.Time{Time: end},
			Conditions: []batchv1.JobCondition{
				{Type: batchv1.JobComplete, Status: corev1.ConditionTrue},
			},
		},
	}
	project := spanEmissionFixtureProject(testProjectUID, "claude-test-model")
	expectedTID, err := otelai.TraceIDFromUID(string(project.UID))
	if err != nil {
		t.Fatalf("otelai.TraceIDFromUID(%q): %v", project.UID, err)
	}

	_, _, gotOK := synthesizePlannerSpan(context.Background(), "project", "proj-1", "", project, ProviderDefaults{}, job, pkgdispatch.EnvelopeOut{}, false, trace.SpanID{})
	if !gotOK {
		t.Fatalf("synthesizePlannerSpan = false, want true")
	}

	spans := exp.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("recorded %d spans, want exactly 1", len(spans))
	}
	span := spans[0]
	if span.Parent.SpanID().IsValid() {
		t.Errorf("span.Parent.SpanID() valid = true, want false (root span, D-02)")
	}
	if got := span.SpanContext.TraceID(); got != expectedTID {
		t.Errorf("span TraceID = %v, want %v (still deterministic for a root span)", got, expectedTID)
	}
}

// TestSynthesizePlannerSpanReturnsOwnSpanID — the returned SpanID is the
// minted span's own identity (span.SpanContext.SpanID()), not the parent's —
// this is the value the caller persists to {Level}TraceSpanID (PROP-02).
func TestSynthesizePlannerSpanReturnsOwnSpanID(t *testing.T) {
	exp := setupSpanExporter(t)

	start := mustTime(t, "2026-07-15T10:00:00Z")
	end := mustTime(t, "2026-07-15T10:05:00Z")
	job := &batchv1.Job{
		Status: batchv1.JobStatus{
			StartTime:      &metav1.Time{Time: start},
			CompletionTime: &metav1.Time{Time: end},
			Conditions: []batchv1.JobCondition{
				{Type: batchv1.JobComplete, Status: corev1.ConditionTrue},
			},
		},
	}
	project := spanEmissionFixtureProject(testProjectUID, "claude-test-model")

	gotID, _, gotOK := synthesizePlannerSpan(context.Background(), "plan", "plan-1", "", project, ProviderDefaults{}, job, pkgdispatch.EnvelopeOut{}, false, trace.SpanID{})
	if !gotOK {
		t.Fatalf("synthesizePlannerSpan = false, want true")
	}

	spans := exp.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("recorded %d spans, want exactly 1", len(spans))
	}
	if want := spans[0].SpanContext.SpanID(); gotID != want {
		t.Errorf("returned SpanID = %v, want %v (the exported span's own SpanID)", gotID, want)
	}
}

// TestSynthesizePlannerSpanTaskRoleExecutor — level "task" derives
// tide.role=executor (POOL-01 vocabulary), unlike the four planner levels
// which all derive tide.role=planner (asserted already by the
// SucceededComplete test).
func TestSynthesizePlannerSpanTaskRoleExecutor(t *testing.T) {
	exp := setupSpanExporter(t)

	start := mustTime(t, "2026-07-15T10:00:00Z")
	end := mustTime(t, "2026-07-15T10:05:00Z")
	job := &batchv1.Job{
		Status: batchv1.JobStatus{
			StartTime:      &metav1.Time{Time: start},
			CompletionTime: &metav1.Time{Time: end},
			Conditions: []batchv1.JobCondition{
				{Type: batchv1.JobComplete, Status: corev1.ConditionTrue},
			},
		},
	}
	project := spanEmissionFixtureProject(testProjectUID, "claude-test-model")

	_, _, gotOK := synthesizePlannerSpan(context.Background(), "task", "task-1", "", project, ProviderDefaults{}, job, pkgdispatch.EnvelopeOut{}, false, trace.SpanID{})
	if !gotOK {
		t.Fatalf("synthesizePlannerSpan = false, want true")
	}

	spans := exp.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("recorded %d spans, want exactly 1", len(spans))
	}
	role, ok := attrValue(spans[0].Attributes, "tide.role")
	if !ok || role.AsString() != "executor" {
		t.Errorf("tide.role attribute = %v (found=%v), want %q", role, ok, "executor")
	}
}

// TestSynthesizePlannerSpanTracingDarkNotEmitted (46-REVIEW WR-01) — with
// the no-op TracerProvider installed (tracing-dark: empty
// OTEL_EXPORTER_OTLP_ENDPOINT, the exact provider otelinit installs), the
// noop Tracer.Start propagates the reconstructed parent SpanContext, so no
// real SpanID is minted. synthesizePlannerSpan must return emitted=false —
// NOT emitted=true with the parent's SpanID — or every completion handler
// would persist "0000000000000000" (root / zero-hex parent) or the parent's
// own span ID (real pre-dark parent hex) into {Level}TraceSpanID, which the
// dashboard then renders as a dead Phoenix deep link.
func TestSynthesizePlannerSpanTracingDarkNotEmitted(t *testing.T) {
	prev := otel.GetTracerProvider()
	otel.SetTracerProvider(tracenoop.NewTracerProvider())
	t.Cleanup(func() { otel.SetTracerProvider(prev) })

	start := mustTime(t, "2026-07-15T10:00:00Z")
	end := mustTime(t, "2026-07-15T10:05:00Z")
	job := &batchv1.Job{
		Status: batchv1.JobStatus{
			StartTime:      &metav1.Time{Time: start},
			CompletionTime: &metav1.Time{Time: end},
			Conditions: []batchv1.JobCondition{
				{Type: batchv1.JobComplete, Status: corev1.ConditionTrue},
			},
		},
	}
	project := spanEmissionFixtureProject(testProjectUID, "claude-test-model")

	realParent, err := trace.SpanIDFromHex("0102030405060708")
	if err != nil {
		t.Fatalf("trace.SpanIDFromHex: %v", err)
	}

	cases := []struct {
		name         string
		parentSpanID trace.SpanID
	}{
		// The Project root: zero parent → noop propagates the zero SpanID.
		{name: "zero parent (root)", parentSpanID: trace.SpanID{}},
		// A child whose parent hex is a real pre-dark span ID → noop
		// propagates the PARENT's SpanID as the "minted" one.
		{name: "real parent (pre-dark hex)", parentSpanID: realParent},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotID, _, emitted := synthesizePlannerSpan(context.Background(), "milestone", "ms-1", "", project, ProviderDefaults{}, job, pkgdispatch.EnvelopeOut{}, true, tc.parentSpanID)
			if emitted {
				t.Errorf("emitted = true, want false (no real span is minted under the no-op provider)")
			}
			if gotID.IsValid() {
				t.Errorf("returned SpanID = %v, want zero — a handler persisting this would create a dead Phoenix deep link", gotID)
			}
		})
	}
}

// ─── D-05 loop.* AGENT-span stamping (50-06 Task 3) ────────────────────────

// TestSynthesizePlannerSpanLoopAttributes — a Task-level span synthesized
// from an envelope with AttemptID/LoopRunID/TerminalReason/Usage.Iterations/
// Git.HeadSHA set carries exactly the 6 loop.* keys D-05 populates this
// phase; evaluation.*/human_intervention stay unset (Phase 51's domain).
func TestSynthesizePlannerSpanLoopAttributes(t *testing.T) {
	exp := setupSpanExporter(t)

	start := mustTime(t, "2026-07-15T10:00:00Z")
	end := mustTime(t, "2026-07-15T10:05:00Z")
	job := &batchv1.Job{
		Status: batchv1.JobStatus{
			StartTime:      &metav1.Time{Time: start},
			CompletionTime: &metav1.Time{Time: end},
			Conditions: []batchv1.JobCondition{
				{Type: batchv1.JobComplete, Status: corev1.ConditionTrue},
			},
		},
	}
	project := spanEmissionFixtureProject(testProjectUID, "claude-test-model")
	out := pkgdispatch.EnvelopeOut{
		LoopRunID:      "task-uid-abc",
		AttemptID:      "task-uid-abc-2",
		TerminalReason: pkgdispatch.TerminalReasonCompleted,
		Usage:          pkgdispatch.Usage{Iterations: 3},
		Git:            &pkgdispatch.GitOutput{HeadSHA: "deadbeef"},
	}

	_, _, gotOK := synthesizePlannerSpan(context.Background(), "task", "task-1", "", project, ProviderDefaults{}, job, out, true, trace.SpanID{})
	if !gotOK {
		t.Fatalf("synthesizePlannerSpan(task, populated loop identity) = false, want true")
	}

	spans := exp.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("recorded %d spans, want exactly 1", len(spans))
	}
	span := spans[0]

	wantStringAttrs := map[attribute.Key]string{
		"loop.kind":              "execution",
		"loop.run_id":            "task-uid-abc-2",
		"loop.parent_run_id":     "task-uid-abc",
		"loop.candidate_version": "deadbeef",
		"loop.exit_reason":       "completed",
	}
	for key, want := range wantStringAttrs {
		val, ok := attrValue(span.Attributes, key)
		if !ok {
			t.Errorf("span missing attribute %q", key)
			continue
		}
		if val.AsString() != want {
			t.Errorf("attribute %q = %q, want %q", key, val.AsString(), want)
		}
	}
	iteration, ok := attrValue(span.Attributes, "loop.iteration")
	if !ok || iteration.AsInt64() != 3 {
		t.Errorf("loop.iteration = %v (found=%v), want 3", iteration, ok)
	}

	// D-05/CONTEXT <specifics>: defined but Phase-51-populated — Phase 50
	// must never fake-populate them.
	for _, key := range []attribute.Key{"evaluation.result", "evaluation.version", "human_intervention"} {
		if _, found := attrValue(span.Attributes, key); found {
			t.Errorf("span unexpectedly carries %q — Phase 51 populates this, not Phase 50", key)
		}
	}
}

// TestSynthesizePlannerSpanLoopAttributesAbsentWhenEmpty — an envelope with
// an empty AttemptID (e.g. a planner-level dispatch, never stamped this
// phase) produces a span with ZERO loop.* attributes — never fabricated.
func TestSynthesizePlannerSpanLoopAttributesAbsentWhenEmpty(t *testing.T) {
	exp := setupSpanExporter(t)

	start := mustTime(t, "2026-07-15T10:00:00Z")
	end := mustTime(t, "2026-07-15T10:05:00Z")
	job := &batchv1.Job{
		Status: batchv1.JobStatus{
			StartTime:      &metav1.Time{Time: start},
			CompletionTime: &metav1.Time{Time: end},
			Conditions: []batchv1.JobCondition{
				{Type: batchv1.JobComplete, Status: corev1.ConditionTrue},
			},
		},
	}
	project := spanEmissionFixtureProject(testProjectUID, "claude-test-model")

	_, _, gotOK := synthesizePlannerSpan(context.Background(), "milestone", "ms-1", "", project, ProviderDefaults{}, job, pkgdispatch.EnvelopeOut{}, true, trace.SpanID{})
	if !gotOK {
		t.Fatalf("synthesizePlannerSpan(empty envelope) = false, want true")
	}

	spans := exp.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("recorded %d spans, want exactly 1", len(spans))
	}
	for _, key := range []attribute.Key{
		"loop.kind", "loop.run_id", "loop.parent_run_id",
		"loop.iteration", "loop.candidate_version", "loop.exit_reason",
	} {
		if _, found := attrValue(spans[0].Attributes, key); found {
			t.Errorf("span unexpectedly carries %q with an empty AttemptID (never fabricate)", key)
		}
	}
}

// TestSynthesizePlannerSpanLoopAttributesOmitTokenCount — 46 D-03 preserved:
// even with loop.* populated, the AGENT span still carries NO
// llm.token_count.* attribute (token counts remain the per-call LLM spans'
// sole responsibility).
func TestSynthesizePlannerSpanLoopAttributesOmitTokenCount(t *testing.T) {
	exp := setupSpanExporter(t)

	start := mustTime(t, "2026-07-15T10:00:00Z")
	end := mustTime(t, "2026-07-15T10:05:00Z")
	job := &batchv1.Job{
		Status: batchv1.JobStatus{
			StartTime:      &metav1.Time{Time: start},
			CompletionTime: &metav1.Time{Time: end},
			Conditions: []batchv1.JobCondition{
				{Type: batchv1.JobComplete, Status: corev1.ConditionTrue},
			},
		},
	}
	project := spanEmissionFixtureProject(testProjectUID, "claude-test-model")
	out := pkgdispatch.EnvelopeOut{
		LoopRunID:      "task-uid-abc",
		AttemptID:      "task-uid-abc-1",
		TerminalReason: pkgdispatch.TerminalReasonCompleted,
		Usage: pkgdispatch.Usage{
			InputTokens: 700, OutputTokens: 300, Iterations: 1,
		},
	}

	_, _, gotOK := synthesizePlannerSpan(context.Background(), "task", "task-1", "", project, ProviderDefaults{}, job, out, true, trace.SpanID{})
	if !gotOK {
		t.Fatalf("synthesizePlannerSpan(task, populated loop identity) = false, want true")
	}

	spans := exp.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("recorded %d spans, want exactly 1", len(spans))
	}
	for _, key := range []attribute.Key{
		"llm.token_count.prompt", "llm.token_count.completion",
		"llm.token_count.prompt_details.cache_read", "llm.token_count.prompt_details.cache_write",
		"llm.token_count.total",
	} {
		if _, found := attrValue(spans[0].Attributes, key); found {
			t.Errorf("span unexpectedly carries token-count attribute %q (46 D-03)", key)
		}
	}
}

// ─── spanIDFromHexOrZero ────────────────────────────────────────────────────

func TestSpanIDFromHexOrZero(t *testing.T) {
	valid, err := trace.SpanIDFromHex("0102030405060708")
	if err != nil {
		t.Fatalf("trace.SpanIDFromHex: %v", err)
	}
	cases := []struct {
		name string
		hex  string
		want trace.SpanID
	}{
		{name: "valid 16-hex", hex: "0102030405060708", want: valid},
		{name: "empty", hex: "", want: trace.SpanID{}},
		{name: "malformed", hex: "not-a-span-id", want: trace.SpanID{}},
	}
	for _, tc := range cases {
		if got := spanIDFromHexOrZero(tc.hex); got != tc.want {
			t.Errorf("spanIDFromHexOrZero(%s) = %v, want %v", tc.name, got, tc.want)
		}
	}
}

// ─── traceparentForLevel ────────────────────────────────────────────────────

func TestTraceparentForLevel(t *testing.T) {
	project := spanEmissionFixtureProject(testProjectUID, "claude-test-model")
	traceID, err := otelai.TraceIDFromUID(string(project.UID))
	if err != nil {
		t.Fatalf("otelai.TraceIDFromUID(%q): %v", project.UID, err)
	}
	spanID, err := trace.SpanIDFromHex("0102030405060708")
	if err != nil {
		t.Fatalf("trace.SpanIDFromHex: %v", err)
	}

	// D-02/Phase 46: the trailing flags byte tracks the sampled param exactly
	// — "01" when true, "00" when false.
	wantSampled := "00-" + traceID.String() + "-" + spanID.String() + "-01"
	if got := traceparentForLevel(project, "0102030405060708", true); got != wantSampled {
		t.Errorf("traceparentForLevel(project, valid hex, sampled=true) = %q, want %q", got, wantSampled)
	}
	wantUnsampled := "00-" + traceID.String() + "-" + spanID.String() + "-00"
	if got := traceparentForLevel(project, "0102030405060708", false); got != wantUnsampled {
		t.Errorf("traceparentForLevel(project, valid hex, sampled=false) = %q, want %q", got, wantUnsampled)
	}

	if got := traceparentForLevel(nil, "0102030405060708", true); got != "" {
		t.Errorf("traceparentForLevel(nil project, valid hex) = %q, want %q", got, "")
	}
	if got := traceparentForLevel(project, "", true); got != "" {
		t.Errorf("traceparentForLevel(project, empty hex) = %q, want %q", got, "")
	}
}

// TestTraceparentForLevelCarriesRealSampledBit — the Task-flow proof (D-02):
// synthesizePlannerSpan's own real IsSampled() return threads straight
// through to traceparentForLevel's flags byte, end to end, not just a
// hand-supplied bool.
func TestTraceparentForLevelCarriesRealSampledBit(t *testing.T) {
	setupSpanExporter(t)

	start := mustTime(t, "2026-07-15T10:00:00Z")
	end := mustTime(t, "2026-07-15T10:05:00Z")
	job := &batchv1.Job{
		Status: batchv1.JobStatus{
			StartTime:      &metav1.Time{Time: start},
			CompletionTime: &metav1.Time{Time: end},
			Conditions: []batchv1.JobCondition{
				{Type: batchv1.JobComplete, Status: corev1.ConditionTrue},
			},
		},
	}
	project := spanEmissionFixtureProject(testProjectUID, "claude-test-model")

	gotID, gotSampled, gotOK := synthesizePlannerSpan(context.Background(), "task", "task-1", "", project, ProviderDefaults{}, job, pkgdispatch.EnvelopeOut{}, false, trace.SpanID{})
	if !gotOK {
		t.Fatalf("synthesizePlannerSpan = false, want true")
	}

	wantFlag := "00"
	if gotSampled {
		wantFlag = "01"
	}
	got := traceparentForLevel(project, gotID.String(), gotSampled)
	if got == "" {
		t.Fatalf("traceparentForLevel(project, emitted spanID, real sampled bit) = \"\", want a non-empty traceparent")
	}
	if got[len(got)-2:] != wantFlag {
		t.Errorf("traceparentForLevel flags byte = %q, want %q (from synthesizePlannerSpan's real sampled=%v)", got[len(got)-2:], wantFlag, gotSampled)
	}
}

// ─── buildLevelEnrichment ───────────────────────────────────────────────────

// decodeMetadata unmarshals a buildLevelEnrichment metadata JSON string,
// failing the test on invalid JSON.
func decodeMetadata(t *testing.T, encoded string) map[string]string {
	t.Helper()
	var m map[string]string
	if err := json.Unmarshal([]byte(encoded), &m); err != nil {
		t.Fatalf("metadata %q is not valid JSON: %v", encoded, err)
	}
	return m
}

func containsTag(tags []string, want string) bool {
	return slices.Contains(tags, want)
}

// TestBuildLevelEnrichmentProjectOmitsGateProfile — Pitfall 5: Gates has no
// Project field, so level=="project" must never carry a gate_profile key,
// even though the project has other Gates set for its child levels.
func TestBuildLevelEnrichmentProjectOmitsGateProfile(t *testing.T) {
	project := &tideprojectv1alpha3.Project{
		ObjectMeta: metav1.ObjectMeta{UID: types.UID(testProjectUID)},
		Spec: tideprojectv1alpha3.ProjectSpec{
			Gates: tideprojectv1alpha3.Gates{Milestone: "auto", Phase: "approve"},
		},
	}

	md, tags := buildLevelEnrichment(project, "project", "my-project", "")
	decoded := decodeMetadata(t, md)
	if _, ok := decoded["gate_profile"]; ok {
		t.Errorf("metadata[gate_profile] present = %v, want absent for level=project", decoded["gate_profile"])
	}
	if decoded["level"] != "project" {
		t.Errorf("metadata[level] = %q, want %q", decoded["level"], "project")
	}
	if decoded["name"] != "my-project" {
		t.Errorf("metadata[name] = %q, want %q", decoded["name"], "my-project")
	}
	if containsTag(tags, "auto") || containsTag(tags, "approve") {
		t.Errorf("tags = %v, must not carry a gate_profile-derived tag for level=project", tags)
	}
}

// TestBuildLevelEnrichmentConservativeFailureHalt — a conservative-profile
// project with ConditionFailureHalt=True yields failure_profile
// "conservative", failure_halt "true", and the "failure-halt" tag.
func TestBuildLevelEnrichmentConservativeFailureHalt(t *testing.T) {
	project := &tideprojectv1alpha3.Project{
		ObjectMeta: metav1.ObjectMeta{UID: types.UID(testProjectUID)},
		Spec: tideprojectv1alpha3.ProjectSpec{
			Gates:          tideprojectv1alpha3.Gates{Task: "auto"},
			FailureProfile: tideprojectv1alpha3.FailureProfileConservative,
		},
	}
	meta.SetStatusCondition(&project.Status.Conditions, metav1.Condition{
		Type:               tideprojectv1alpha3.ConditionFailureHalt,
		Status:             metav1.ConditionTrue,
		Reason:             "TaskFailedHalt",
		LastTransitionTime: metav1.Now(),
	})

	md, tags := buildLevelEnrichment(project, "task", "my-task", "3")
	decoded := decodeMetadata(t, md)

	if decoded["failure_profile"] != "conservative" {
		t.Errorf("metadata[failure_profile] = %q, want %q", decoded["failure_profile"], "conservative")
	}
	if decoded["failure_halt"] != "true" {
		t.Errorf("metadata[failure_halt] = %q, want %q", decoded["failure_halt"], "true")
	}
	if decoded["gate_profile"] != "auto" {
		t.Errorf("metadata[gate_profile] = %q, want %q", decoded["gate_profile"], "auto")
	}
	if decoded["wave_index"] != "3" {
		t.Errorf("metadata[wave_index] = %q, want %q", decoded["wave_index"], "3")
	}
	if !containsTag(tags, "failure-halt") {
		t.Errorf("tags = %v, want to contain %q", tags, "failure-halt")
	}
	if !containsTag(tags, "conservative") {
		t.Errorf("tags = %v, want to contain %q", tags, "conservative")
	}
}

// TestBuildLevelEnrichmentStrictDefault — an empty Spec.FailureProfile
// resolves to "strict" (the API default), failure_halt "false" (no
// condition set), and no "failure-halt" tag.
func TestBuildLevelEnrichmentStrictDefault(t *testing.T) {
	project := &tideprojectv1alpha3.Project{
		ObjectMeta: metav1.ObjectMeta{UID: types.UID(testProjectUID)},
	}

	md, tags := buildLevelEnrichment(project, "milestone", "my-milestone", "")
	decoded := decodeMetadata(t, md)

	if decoded["failure_profile"] != "strict" {
		t.Errorf("metadata[failure_profile] = %q, want %q", decoded["failure_profile"], "strict")
	}
	if decoded["failure_halt"] != "false" {
		t.Errorf("metadata[failure_halt] = %q, want %q", decoded["failure_halt"], "false")
	}
	if _, ok := decoded["wave_index"]; ok {
		t.Errorf("metadata[wave_index] present = %v, want absent when waveIndex is empty", decoded["wave_index"])
	}
	if _, ok := decoded["gate_profile"]; ok {
		t.Errorf("metadata[gate_profile] present = %v, want absent when the policy is unset", decoded["gate_profile"])
	}
	if containsTag(tags, "failure-halt") {
		t.Errorf("tags = %v, must not contain %q when FailureHalt is unset", tags, "failure-halt")
	}
	if !containsTag(tags, "strict") {
		t.Errorf("tags = %v, want to contain %q", tags, "strict")
	}
	if !containsTag(tags, "milestone") {
		t.Errorf("tags = %v, want to contain the level %q", tags, "milestone")
	}
}

// TestBuildLevelEnrichmentNilProject — nil-safe: matches ResolveProvider's
// convention of returning a zero value rather than panicking.
func TestBuildLevelEnrichmentNilProject(t *testing.T) {
	md, tags := buildLevelEnrichment(nil, "task", "my-task", "1")
	if md != "" {
		t.Errorf("buildLevelEnrichment(nil project) metadata = %q, want empty", md)
	}
	if tags != nil {
		t.Errorf("buildLevelEnrichment(nil project) tags = %v, want nil", tags)
	}
}
