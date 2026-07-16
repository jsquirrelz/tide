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

// span_emission.go synthesizes retroactive AGENT-kind OpenTelemetry spans at
// planner-level Job completion (Phase 42 / v1.0.8 ATTR-01/ATTR-02, D-01..D-04).
//
// Spans are created and closed within a single call — never held open across
// a Reconcile() return (STATE.md binding constraint). Option A (plan 42-02's
// recorded decision) applies here: spans are independent roots, no remote
// SpanContext injection, no TraceIDFromUID call. Phase 43 threads parenting.
//
// Shared across all four planner-level completion handlers (this plan wires
// Milestone + Phase; plan 42-05 ports the identical pattern to Plan/Project).
package controller

import (
	"context"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"

	tideprojectv1alpha3 "github.com/jsquirrelz/tide/api/v1alpha3"
	pkgdispatch "github.com/jsquirrelz/tide/pkg/dispatch"
	"github.com/jsquirrelz/tide/pkg/otelai"
)

// spanEndTime resolves the span end timestamp for a terminal planner Job.
//
// Pitfall 1: batchv1.JobStatus.CompletionTime is documented "set when the
// job finishes successfully, and only then" — it is nil on every failed
// Job. The failure path falls back to the terminal JobFailed condition's
// LastTransitionTime instead. A nil job (already TTL-GC'd — a real,
// exercised path, see Pattern 3) or a terminal Job with neither timestamp
// populated both return ok=false — callers must never fabricate the
// wall-clock "now" as a substitute (Pattern 3 anti-pattern).
func spanEndTime(job *batchv1.Job) (time.Time, bool) {
	if job == nil {
		return time.Time{}, false
	}
	if job.Status.CompletionTime != nil {
		return job.Status.CompletionTime.Time, true
	}
	for _, c := range job.Status.Conditions {
		if c.Type == batchv1.JobFailed && c.Status == corev1.ConditionTrue {
			return c.LastTransitionTime.Time, true
		}
	}
	return time.Time{}, false
}

// plannerSpanResolvable reports whether a retroactive span can be
// synthesized for the Job at all: non-nil (not yet TTL-GC'd), a populated
// StartTime, and a resolvable end timestamp (spanEndTime — Pattern 3: never
// fabricate wall-clock substitutes). Callers MUST gate the SpanEmittedUID
// marker stamp on this predicate: mark-then-emit ordering (42-REVIEW WR-01)
// stamps the marker BEFORE emitting, and a stamp without a subsequent
// emission would suppress the attempt's span forever.
func plannerSpanResolvable(job *batchv1.Job) bool {
	if job == nil || job.Status.StartTime == nil {
		return false
	}
	_, ok := spanEndTime(job)
	return ok
}

// synthesizePlannerSpan synthesizes a retroactive AGENT span for a
// planner-level Job's completion (D-01: succeeded AND failed; D-02: one span
// per Job attempt, marker-gated by the caller — see 42-PATTERNS.md's
// deliberate deviations from the *RolledUpUID skeleton). Emission is
// at-most-once, never exactly-once (42-REVIEW WR-01): the caller durably
// stamps its level's SpanEmittedUID marker BEFORE calling (mark-then-emit,
// entry gated on plannerSpanResolvable), independent of envReadOK (D-04).
// Duplicate export is therefore impossible; a crash between stamp and
// emission loses that attempt's span — span loss is preferred over
// double-counted tokens/cost in Phoenix, whose cost views sum
// llm.token_count.* across spans. Returns true when a span was emitted
// (false only via the defensive re-check of the same preconditions
// plannerSpanResolvable already gated for the caller).
//
// level is one of "milestone" | "phase" | "plan" | "project" — the same
// literal ResolveProvider's dispatch sites already pass.
//
// model/provider resolution (ATTR-01, D-04, D-07): a SECOND, envelope-
// independent call to ResolveProvider (the same pure, nil-safe function
// already called at dispatch time) — never read from the envelope, which
// never carried a model field at any layer (RESEARCH.md Pattern 1).
//
// Token accounting (ATTR-02, D-08 / Pitfall 4): promptTokens sums
// InputTokens + CacheReadTokens + CacheCreationTokens — Phoenix's
// llm.token_count.prompt encodes the FULL prompt including cache subsets,
// not the uncached-only count. Degraded envelopes (envReadOK=false) carry
// zero token-count attributes plus the tide.envelope.degraded marker
// instead (D-04) — usage attributes are simply absent, never fabricated.
//
// End() is called explicitly (never deferred with a pre-branch timestamp):
// the resolved end time differs per success/failure outcome.
func synthesizePlannerSpan(
	ctx context.Context,
	level string,
	project *tideprojectv1alpha3.Project,
	helmDefaults ProviderDefaults,
	completedJob *batchv1.Job,
	out pkgdispatch.EnvelopeOut,
	envReadOK bool,
) bool {
	endTime, ok := spanEndTime(completedJob)
	if !ok || completedJob.Status.StartTime == nil {
		return false
	}
	startTime := completedJob.Status.StartTime.Time

	// D-04/D-07: second, envelope-independent call — nil-safe for project==nil.
	provider := ResolveProvider(project, level, helmDefaults)

	tracer := otel.Tracer("tide.dispatch")
	spanName := "tide.dispatch." + level
	_, span := tracer.Start(ctx, spanName, trace.WithTimestamp(startTime))

	span.SetAttributes(otelai.AgentInvocation(provider.Vendor, spanName, "planner", level)...)
	span.SetAttributes(otelai.LLMIdentity(provider.Vendor, provider.Model)...)

	if envReadOK {
		// D-08/Pitfall 4: prompt = uncached + cache-read + cache-write subsets.
		promptTokens := out.Usage.InputTokens + out.Usage.CacheReadTokens + out.Usage.CacheCreationTokens
		span.SetAttributes(otelai.TokenCount(
			int(promptTokens),
			int(out.Usage.OutputTokens),
			int(out.Usage.CacheReadTokens),
			int(out.Usage.CacheCreationTokens),
		)...)
	} else {
		span.SetAttributes(otelai.EnvelopeDegraded())
	}

	if isJobFailed(completedJob) {
		span.SetStatus(codes.Error, out.Reason)
		if envReadOK {
			span.SetAttributes(otelai.FailureDetail(out.ExitCode, out.Reason)...)
		}
	} else {
		span.SetStatus(codes.Ok, "")
	}

	span.End(trace.WithTimestamp(endTime))
	return true
}
