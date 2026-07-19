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
	"encoding/json"
	"strconv"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"

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
// double-counted cost in Phoenix (46 D-03: per-call LLM spans are the sole
// llm.token_count.* source; see below). Returns the minted span's own
// SpanID, the span's real sampled bit (trace.SpanContext.IsSampled(),
// captured before End — PROP-02/D-02, Phase 46), and true when a span was
// emitted (zero SpanID, sampled=true, false when emission was skipped —
// either the defensive re-check of plannerSpanResolvable's preconditions, a
// nil project, a TraceIDFromUID error, or a tracing-dark run in which the
// no-op TracerProvider minted no real SpanID (46-REVIEW WR-01, see the
// guard before the return); sampled is meaningless in this case and callers
// must gate on the third return value first).
//
// level is one of "milestone" | "phase" | "plan" | "project" | "task" — the
// same literal ResolveProvider's dispatch sites already pass.
//
// levelName is the dispatching CR's own Name (ms.Name / ph.Name / plan.Name
// / project.Name / task.Name) and waveIndex is Task's
// tideproject.k8s/wave-index label value ("" at every other level) — both
// feed buildLevelEnrichment below (46 D-05/D-06/OBS-03).
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
// Token accounting (46 D-03, planner_correction — overturns the earlier
// ATTR-02/D-08/Pitfall 4 design): this span carries NO llm.token_count.*
// attribute at any level, succeeded or degraded. The reporter's
// synthesizeSpans emits per-call LLM message spans on BOTH the combined-mode
// path (all four planner levels) and the trace-only path (Task) — every
// level has a sibling per-call span source — and Phoenix creates a SpanCost
// row per span carrying llm.token_count.* with no span-kind gate (RESEARCH
// A2). A rolled-up total on this AGENT span would double-count every run in
// Phoenix's session/trace cost rollups, violating the LOCKED invariant that
// per-call spans are the sole authoritative token-count source. Degraded
// envelopes (envReadOK=false) still carry the tide.envelope.degraded marker
// (D-04) — that marker is independent of token-count emission, which is now
// unconditionally absent.
//
// End() is called explicitly (never deferred with a pre-branch timestamp):
// the resolved end time differs per success/failure outcome.
func synthesizePlannerSpan(
	ctx context.Context,
	level string,
	levelName string,
	waveIndex string,
	project *tideprojectv1alpha3.Project,
	helmDefaults ProviderDefaults,
	completedJob *batchv1.Job,
	out pkgdispatch.EnvelopeOut,
	envReadOK bool,
	parentSpanID trace.SpanID,
) (trace.SpanID, bool, bool) {
	endTime, ok := spanEndTime(completedJob)
	if !ok || completedJob.Status.StartTime == nil {
		return trace.SpanID{}, true, false
	}
	startTime := completedJob.Status.StartTime.Time

	// TRACE-02/Pitfall 5: no project, no deterministic TraceID — skip rather
	// than emit an unanchored span that would break the one-trace guarantee.
	if project == nil {
		logf.FromContext(ctx).Info("skipping span emission: nil project (no deterministic TraceID available)", "level", level)
		return trace.SpanID{}, true, false
	}
	traceID, err := otelai.TraceIDFromUID(string(project.UID))
	if err != nil {
		logf.FromContext(ctx).Error(err, "skipping span emission: TraceIDFromUID failed", "level", level, "project", project.Name)
		return trace.SpanID{}, true, false
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

	// 46 D-05/OBS-02/OBS-03: session.id + metadata/tags, computed from the
	// SAME buildLevelEnrichment inputs the level's reporter spawn uses (Task
	// 2) so a Task's AGENT span and its reporter's LLM spans carry
	// byte-identical values — Phoenix's ProjectSession groups on an exact
	// session.id string match.
	span.SetAttributes(otelai.SessionID(string(project.UID)))
	md, tags := buildLevelEnrichment(project, level, levelName, waveIndex)
	if md != "" {
		span.SetAttributes(otelai.MetadataJSON(md))
	}
	if len(tags) > 0 {
		span.SetAttributes(otelai.Tags(tags...))
	}

	// D-05 (50-06 Task 3, OBS-01): loop.* identity, gated on out.AttemptID
	// being non-empty — planner-level dispatches (never stamped this phase,
	// D-01) leave AttemptID empty and correctly carry ZERO loop.* attributes
	// rather than a fabricated empty. candidateVersion is out.Git.HeadSHA
	// (D-03's locking/head commit, empty when out.Git is nil); exitReason is
	// out.TerminalReason verbatim (D-02b — loop.exit_reason IS the envelope's
	// TerminalReason, one source of truth). The otelai helper below already
	// omits the parent_run_id/candidate_version/exit_reason optionals when
	// empty, so an unclassified synthetic envelope (50-06 Task 2's
	// synthesizeNoEnvelopeOut with an unmapped failure reason) yields no
	// loop.exit_reason rather than an empty one.
	//
	// evaluation.result / evaluation.version / human_intervention are NOT
	// stamped here — Phase 51's EVALUATOR span populates them (CONTEXT
	// <specifics>: do not fake-populate ahead of the owning phase).
	if out.AttemptID != "" {
		candidateVersion := ""
		if out.Git != nil {
			candidateVersion = out.Git.HeadSHA
		}
		span.SetAttributes(otelai.LoopAttributes(
			otelai.LoopKindExecution, out.AttemptID, out.LoopRunID,
			out.Usage.Iterations, candidateVersion, string(out.TerminalReason),
		)...)
	}

	// 46 D-03 (planner_correction, see the doc comment above): NO
	// otelai.TokenCount call at any level — the per-call LLM spans the
	// reporter emits are the sole llm.token_count.* source. The degraded
	// marker below is independent of token-count emission.
	if !envReadOK {
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

	// D-02/Phase 46: capture the real sampled bit BEFORE End() — End() does
	// not change it, but the read is only meaningful pre-export.
	sampled := span.SpanContext().IsSampled()
	span.End(trace.WithTimestamp(endTime))

	// 46-REVIEW WR-01: a tracing-dark run (OTEL_EXPORTER_OTLP_ENDPOINT empty,
	// the chart default) has otelinit install the no-op TracerProvider, whose
	// Tracer.Start propagates the reconstructed parent SpanContext untouched —
	// the "minted" SpanID is then the parent's: zero at the Project root, and
	// zero again at every child level (each parents on the zero-hex the level
	// above persisted). Returning emitted=true here would have the callers
	// persist "0000000000000000" into {Level}TraceSpanID status, which the
	// dashboard surfaces as a dead /redirects/spans/0000000000000000 Phoenix
	// deep link once phoenix.baseURL is set. Return emitted=false instead —
	// nothing is lost (the no-op provider exports nothing, so there is no
	// span to link). The equality guard covers the same noop path when the
	// parent hex is a REAL pre-dark span ID: propagating it as this level's
	// own would corrupt parent-child linkage and deep-link identity alike.
	thisSpanID := span.SpanContext().SpanID()
	if !thisSpanID.IsValid() || thisSpanID == parentSpanID {
		return trace.SpanID{}, true, false
	}
	return thisSpanID, sampled, true
}

// buildLevelEnrichment computes the 46 D-05/D-06/OBS-03 metadata/tags
// enrichment pair shared identically between a level's own AGENT span (via
// synthesizePlannerSpan above) and every reporter-emitted LLM span for the
// same dispatch (the caller threads the same md/tags into
// ReporterOptions.MetadataJSON/Tags at the reporter spawn site) — Phoenix's
// filter DSL and ProjectSession grouping require byte-identical values
// across sibling spans (D-05).
//
// The byte-identical guarantee is SAME-RECONCILE scoped (46-REVIEW WR-03):
// the AGENT-span call and the reporter-spawn call read the same in-memory
// Project, and json.Marshal's sorted-key output makes equal inputs
// byte-equal. A reporter spawn retried on a LATER reconcile (transient
// Create failure → requeue, or a crash between emission and spawn)
// re-fetches the Project, so the mutable inputs — failure_halt, an edited
// Spec.FailureProfile or gate policy — can drift from what the sibling
// AGENT span carried. Accepted as bounded drift, same window as the
// sampled-bit limitation documented in docs/observability.md: session.id
// (the immutable Project UID) is grouping-critical and immune;
// metadata/tags are filter conveniences whose drift is confined to a
// failed-spawn retry.
//
// Metadata keys (D-08 — plain names; the tide.* namespace governs only the
// top-level attribute key, not this JSON payload's internal keys):
//
//   - "level"           — one of milestone|phase|plan|project|task
//   - "name"             — the dispatching CR's own Name (levelName)
//   - "wave_index"       — included ONLY when waveIndex != "" (Task-only —
//     read from the tideproject.k8s/wave-index label; D-07 only-if-free —
//     planner levels never populate this key)
//   - "gate_profile"     — project.Spec.Gates.{Milestone,Phase,Plan,Task}
//     keyed on level; key is OMITTED for level=="project" (Gates has no
//     Project field, Pitfall 5) and omitted whenever the resolved policy
//     string is empty (unset)
//   - "failure_profile"  — "strict" (the API default, when
//     project.Spec.FailureProfile == "") or the lowercase
//     Spec.FailureProfile value ("conservative")
//   - "failure_halt"     — "true"/"false" from
//     meta.IsStatusConditionTrue(project.Status.Conditions,
//     tideprojectv1alpha3.ConditionFailureHalt)
//
// Tags (low-cardinality categorical filterables, D-06): [level], the
// gate_profile value when present, the failure_profile value, and the
// literal "failure-halt" appended only when that condition is true.
//
// Returns ("", nil) when project is nil (nil-safe, matches ResolveProvider's
// convention) or on a JSON marshal error (practically impossible for this
// map[string]string shape) — observability never gates, so callers treat an
// empty metadata string as "omit the attribute", never a fabricated value.
func buildLevelEnrichment(project *tideprojectv1alpha3.Project, level, levelName, waveIndex string) (string, []string) {
	if project == nil {
		return "", nil
	}

	md := map[string]string{
		"level": level,
		"name":  levelName,
	}
	if waveIndex != "" {
		md["wave_index"] = waveIndex
	}

	var gateProfile string
	if level != "project" {
		switch level {
		case "milestone":
			gateProfile = string(project.Spec.Gates.Milestone)
		case "phase":
			gateProfile = string(project.Spec.Gates.Phase)
		case "plan":
			gateProfile = string(project.Spec.Gates.Plan)
		case "task":
			gateProfile = string(project.Spec.Gates.Task)
		}
	}
	if gateProfile != "" {
		md["gate_profile"] = gateProfile
	}

	failureProfile := string(project.Spec.FailureProfile)
	if failureProfile == "" {
		failureProfile = string(tideprojectv1alpha3.FailureProfileStrict)
	}
	md["failure_profile"] = failureProfile

	failureHalt := meta.IsStatusConditionTrue(project.Status.Conditions, tideprojectv1alpha3.ConditionFailureHalt)
	md["failure_halt"] = strconv.FormatBool(failureHalt)

	encoded, err := json.Marshal(md)
	if err != nil {
		return "", nil
	}

	tags := []string{level}
	if gateProfile != "" {
		tags = append(tags, gateProfile)
	}
	tags = append(tags, failureProfile)
	if failureHalt {
		tags = append(tags, "failure-halt")
	}

	return string(encoded), tags
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
//
// sampled (D-02, Phase 46) is the traceparent's flags byte: real and
// knowable at the five same-reconcile reporter-spawn sites (the level's own
// span was just synthesized in this call — its IsSampled() bit is threaded
// straight through); literal true at the four dispatch-time subagent-Job
// sites, where the bit is NOT knowable — those sites read a PARENT level's
// persisted span ID across a reconcile boundary, and the parent's sampled
// bit is not itself persisted (RESEARCH Pitfall 3's rejected schema change:
// no new {Level}TraceSampled status field). See docs/observability.md for
// the documented limitation.
func traceparentForLevel(project *tideprojectv1alpha3.Project, spanIDHex string, sampled bool) string {
	if project == nil {
		return ""
	}
	traceID, err := otelai.TraceIDFromUID(string(project.UID))
	if err != nil {
		return ""
	}
	return otelai.FormatTraceparent(traceID, spanIDFromHexOrZero(spanIDHex), sampled)
}
