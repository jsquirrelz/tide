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
	"testing"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	tideprojectv1alpha3 "github.com/jsquirrelz/tide/api/v1alpha3"
	pkgdispatch "github.com/jsquirrelz/tide/pkg/dispatch"
)

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

// ─── synthesizePlannerSpan ─────────────────────────────────────────────────

func TestSynthesizePlannerSpanNilJob(t *testing.T) {
	exp := setupSpanExporter(t)
	got := synthesizePlannerSpan(context.Background(), "milestone", nil, ProviderDefaults{}, nil, pkgdispatch.EnvelopeOut{}, false)
	if got {
		t.Errorf("synthesizePlannerSpan(nil job) = true, want false")
	}
	if len(exp.GetSpans()) != 0 {
		t.Errorf("synthesizePlannerSpan(nil job) recorded %d spans, want 0", len(exp.GetSpans()))
	}
}

// TestSynthesizePlannerSpanSucceededComplete — the ATTR-01/ATTR-02 happy
// path: attribute-complete AGENT span, exact StartTime/EndTime from the
// Job's own timestamps, prompt re-mapped to include cache subsets (D-08).
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
	project := &tideprojectv1alpha3.Project{
		Spec: tideprojectv1alpha3.ProjectSpec{
			Subagent: tideprojectv1alpha3.SubagentConfig{Model: "claude-test-model"},
		},
	}
	out := pkgdispatch.EnvelopeOut{
		Usage: pkgdispatch.Usage{
			InputTokens:         700,
			OutputTokens:        300,
			CacheReadTokens:     200,
			CacheCreationTokens: 100,
		},
	}

	got := synthesizePlannerSpan(context.Background(), "milestone", project, ProviderDefaults{}, job, out, true)
	if !got {
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

	wantIntAttrs := map[attribute.Key]int64{
		"llm.token_count.prompt": 1000, // D-08: InputTokens+CacheReadTokens+CacheCreationTokens
		"llm.token_count.total":  1300,
	}
	for key, want := range wantIntAttrs {
		val, ok := attrValue(span.Attributes, key)
		if !ok {
			t.Errorf("span missing attribute %q", key)
			continue
		}
		if val.AsInt64() != want {
			t.Errorf("attribute %q = %d, want %d", key, val.AsInt64(), want)
		}
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

	got := synthesizePlannerSpan(context.Background(), "milestone", nil, ProviderDefaults{}, job, out, true)
	if !got {
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
	project := &tideprojectv1alpha3.Project{
		Spec: tideprojectv1alpha3.ProjectSpec{
			Subagent: tideprojectv1alpha3.SubagentConfig{Model: "claude-test-model"},
		},
	}

	got := synthesizePlannerSpan(context.Background(), "milestone", project, ProviderDefaults{}, job, pkgdispatch.EnvelopeOut{}, false)
	if !got {
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

// TestSynthesizePlannerSpanNilProjectEmptyModel — Pitfall 5: a nil project
// with no Helm default resolves the model to "" — the span must omit
// llm.model_name entirely rather than emit an empty string.
func TestSynthesizePlannerSpanNilProjectEmptyModel(t *testing.T) {
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

	got := synthesizePlannerSpan(context.Background(), "milestone", nil, ProviderDefaults{}, job, pkgdispatch.EnvelopeOut{}, false)
	if !got {
		t.Fatalf("synthesizePlannerSpan(nil project) = false, want true")
	}

	spans := exp.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("synthesizePlannerSpan(nil project) recorded %d spans, want exactly 1", len(spans))
	}
	span := spans[0]

	provider, ok := attrValue(span.Attributes, "llm.provider")
	if !ok || provider.AsString() != "anthropic" {
		t.Errorf("llm.provider attribute = %v (found=%v), want %q", provider, ok, "anthropic")
	}
	if _, ok := attrValue(span.Attributes, "llm.model_name"); ok {
		t.Errorf("llm.model_name attribute present, want absent (Pitfall 5: empty model omitted)")
	}
}
