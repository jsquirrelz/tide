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
// a Reconcile() return (STATE.md binding constraint). Phase 43 (TRACE-02)
// retrofits Phase 42's independent-root spans into one connected trace: a
// deterministic TraceID derived from Project.UID (otelai.TraceIDFromUID) and
// real parent-child SpanContext threading via the caller-supplied
// parentSpanID — no custom trace ID generator.
//
// Shared across all five levels' completion handlers (four planner levels
// plus Task, which reuses the same synthesizer with role="executor").
package controller

import (
	"context"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"

	logf "sigs.k8s.io/controller-runtime/pkg/log"

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
// llm.token_count.* across spans. Returns the minted span's own SpanID and
// true when a span was emitted (zero SpanID and false when emission was
// skipped — either the defensive re-check of plannerSpanResolvable's
// preconditions, a nil project, or a TraceIDFromUID error).
//
// level is one of "milestone" | "phase" | "plan" | "project" | "task" — the
// same literal ResolveProvider's dispatch sites already pass.
//
// parentSpanID (TRACE-02, Phase 43) is the immediate parent's own persisted
// span ID — trace.SpanID{} (zero) for Project, the true root (D-02); the
// real, durably-persisted parent span ID otherwise. The SDK's newSpan()
// inherits the incoming context's TraceID whenever the parent SpanContext's
// TraceID is valid, independent of the parent SpanID's validity — so the
// same deterministic-TraceID SpanContext yields a root span when parentSpanID
// is zero and a properly-parented span when it's real, with no custom
// trace ID generator (RESEARCH Pattern 2).
//
// A nil project (Pitfall 5) skips emission entirely rather than emitting a
// span with a random or zero TraceID — a span loss is preferred over
// breaking TRACE-02's one-trace-per-Project guarantee, the same
// "loss over incorrect Phoenix data" policy the marker-gate above already
// accepts. Likewise a TraceIDFromUID error (malformed UID) is logged
// non-fatally and skips emission.
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
	parentSpanID trace.SpanID,
) (trace.SpanID, bool) {
	endTime, ok := spanEndTime(completedJob)
	if !ok || completedJob.Status.StartTime == nil {
		return trace.SpanID{}, false
	}
	startTime := completedJob.Status.StartTime.Time

	// TRACE-02/Pitfall 5: no project, no deterministic TraceID — skip rather
	// than emit an unanchored span that would break the one-trace guarantee.
	if project == nil {
		logf.FromContext(ctx).Info("skipping span emission: nil project (no deterministic TraceID available)", "level", level)
		return trace.SpanID{}, false
	}
	traceID, err := otelai.TraceIDFromUID(string(project.UID))
	if err != nil {
		logf.FromContext(ctx).Error(err, "skipping span emission: TraceIDFromUID failed", "level", level, "project", project.Name)
		return trace.SpanID{}, false
	}

	// D-02: Remote:true because parentSpanID (when non-zero) is reconstructed
	// from durable status written by an earlier reconcile — possibly another
	// replica, never a locally-held live SpanContext.
	sc := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    traceID,
		SpanID:     parentSpanID,
		TraceFlags: trace.FlagsSampled,
		Remote:     true,
	})
	ctx = trace.ContextWithSpanContext(ctx, sc)

	// D-04/D-07: second, envelope-independent call — nil-safe for project==nil.
	provider := ResolveProvider(project, level, helmDefaults)

	tracer := otel.Tracer("tide.dispatch")
	spanName := "tide.dispatch." + level
	_, span := tracer.Start(ctx, spanName, trace.WithTimestamp(startTime))

	// POOL-01 vocabulary: every level is a "planner" dispatch except Task,
	// which is an "executor" dispatch (plan 43-05 reuses this synthesizer).
	role := "planner"
	if level == "task" {
		role = "executor"
	}
	span.SetAttributes(otelai.AgentInvocation(provider.Vendor, spanName, role, level)...)
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
	return span.SpanContext().SpanID(), true
}

// spanIDFromHexOrZero parses a persisted status hex string into a
// trace.SpanID. Returns trace.SpanID{} (zero/invalid) on empty or malformed
// input — never errors, so callers can use it unconditionally on any
// {Level}TraceSpanID status field, including one that was never set.
func spanIDFromHexOrZero(hex string) trace.SpanID {
	if hex == "" {
		return trace.SpanID{}
	}
	id, err := trace.SpanIDFromHex(hex)
	if err != nil {
		return trace.SpanID{}
	}
	return id
}

// traceparentForLevel builds the W3C traceparent string for a persisted
// span-ID hex under the project's deterministic TraceID (PROP-02's
// propagation seam — the value threaded into TRACEPARENT/--traceparent
// starting Wave 3). Returns "" when project is nil, TraceIDFromUID errors,
// or spanIDHex is empty/invalid (FormatTraceparent already returns "" for an
// invalid SpanContext, so an empty/malformed hex degrades gracefully).
func traceparentForLevel(project *tideprojectv1alpha3.Project, spanIDHex string) string {
	if project == nil {
		return ""
	}
	traceID, err := otelai.TraceIDFromUID(string(project.UID))
	if err != nil {
		return ""
	}
	return otelai.FormatTraceparent(traceID, spanIDFromHexOrZero(spanIDHex), true)
}
