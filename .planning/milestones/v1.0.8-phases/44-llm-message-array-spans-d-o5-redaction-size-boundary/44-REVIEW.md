---
phase: 44-llm-message-array-spans-d-o5-redaction-size-boundary
reviewed: 2026-07-17T00:00:16Z
depth: standard
files_reviewed: 24
files_reviewed_list:
  - cmd/manager/main.go
  - cmd/tide-reporter/main.go
  - cmd/tide-reporter/main_test.go
  - internal/controller/dispatch_helpers.go
  - internal/controller/milestone_controller.go
  - internal/controller/phase_controller.go
  - internal/controller/plan_controller.go
  - internal/controller/project_controller.go
  - internal/controller/reporter_jobspec.go
  - internal/controller/reporter_jobspec_test.go
  - internal/controller/task_controller.go
  - internal/controller/task_traceonly_reporter_test.go
  - internal/harness/redact/redact.go
  - internal/harness/redact/redact_test.go
  - internal/reporter/tracesynth.go
  - internal/reporter/tracesynth_test.go
  - internal/reporter/testdata/children/task-01.json
  - internal/reporter/testdata/events_sample.jsonl
  - internal/reporter/testdata/events_truncated.jsonl
  - internal/reporter/testdata/in_executor.json
  - internal/reporter/testdata/in_planner.json
  - pkg/otelai/attrs.go
  - pkg/otelai/attrs_test.go
  - pkg/otelai/doc.go
findings:
  critical: 1
  warning: 5
  info: 5
  total: 11
status: issues_found
---

# Phase 44: Code Review Report

**Reviewed:** 2026-07-17T00:00:16Z
**Depth:** standard
**Files Reviewed:** 24
**Status:** issues_found

## Summary

Reviewed the Phase 44 LLM message-array span pipeline end-to-end: events.jsonl reconstruction (`ReconstructConversation`), the redact-then-truncate size pipeline (`redactTruncate`/`boundedMessageAttrs`), span emission (`EmitSpans`), the `pkg/otelai` D-03 extensions, the reporter binary's trace-only mode, the trace-only Job builder/spawner, and the manager/controller wiring. Verified against the called-into dependencies (`common.ReadLines`, `redact.SecretPatterns`, `otelinit.NewTracerProvider`, `traceparentForLevel`, `synthesizePlannerSpan`). Unit tests for the four changed packages pass; `go build`/`go vet` clean.

The D-09 redact-before-truncate ordering is correctly enforced at its sole call site (`redactTruncate`, tracesynth.go:454-457) and proven by a deliberately straddling test fixture. The D-10 exit-0 posture and D-06 endpoint gating are implemented and tested. However: the per-span 512 KiB size invariant is enforced per-*side*, not per-span — the documented batch-under-4-MiB math does not hold as coded (CR-01). Four warning-tier defects cluster in the tracesynth pipeline: inverted span timestamps on the first call of every real conversation, wholesale span loss on an oversized events.jsonl line, an unredacted/unbounded `Signature` attribute escaping the MSG-02 pipeline, and a path-traversal hole in the `promptPath` seed resolution.

## Critical Issues

### CR-01: Whole-span 512 KiB budget is enforced per-side, not per-span — the OTLP 4 MiB batch math does not hold

**Status:** fixed (commit `266e81b`)
**File:** `internal/reporter/tracesynth.go:59-65, 467-510, 542-543`
**Issue:** The `maxSpanPayloadBytes` doc comment (lines 59-65) states the cap bounds "the SUM of one LLM span's LLMInputMessages+LLMOutputMessages attribute bytes," and the reporter Job env pins `OTEL_BSP_MAX_EXPORT_BATCH_SIZE=6` on the math "6 spans × 512 KiB whole-span cap = 3 MiB per export batch, ~25% headroom under the 4 MiB OTLP gRPC ceiling" (reporter_jobspec.go:221-226). But `boundedMessageAttrs` is invoked separately for the input side and the output side (`EmitSpans`, lines 542-543), and each invocation gets its own full 512 KiB budget (`total > maxSpanPayloadBytes`, line 495). A span's actual worst-case payload is therefore ~1 MiB (both sides at just under 512 KiB), and a batch of 6 such spans is ~6 MiB — over the 4 MiB gRPC ceiling. Because a long conversation's input snapshots grow monotonically, several *consecutive* calls can sit in the 400–512 KiB input band (just under the degrade trip) within one batch, so even single-sided content pushes a batch past the claimed 3 MiB toward the ceiling; the byte accounting also excludes attribute keys, role strings, `ToolCall.ID`/`Name`, and `Signature` values, eroding headroom further. When a batch exceeds the collector's gRPC max message size, the export fails and the BatchSpanProcessor drops the entire batch — silent loss of exactly the telemetry this phase exists to capture, in exactly the pathological-content cases the backstop was designed for. `TestEmitSpans_BatchAggregateUnderCeiling` does not cover the both-sides-near-cap case (its outputs are one small message per call).
**Fix:**
```go
// EmitSpans: compute both sides under ONE shared budget.
inputAttrs, outputAttrs, degraded := boundedSpanAttrs(call.InputMessages, call.OutputMessages)

// boundedSpanAttrs bounds input+output jointly:
inBounded, inTotal := boundMessages(call.InputMessages)
outBounded, outTotal := boundMessages(call.OutputMessages)
if inTotal+outTotal > maxSpanPayloadBytes {
    // degrade the larger side first (or both) to role-only until under budget
}
```
Alternatively, keep the per-side structure but halve the per-side budget (`maxSpanPayloadBytes/2`) so the per-span sum honors the documented 512 KiB cap, and extend the batch test with a both-sides-near-cap fixture.

## Warnings

### WR-01: First-call spans emit with end time before start time (negative duration) on every real multi-call conversation

**Status:** fixed (commit `9390f4e`)
**File:** `internal/reporter/tracesynth.go:536-539, 566-570`
**Issue:** The first call of a conversation always has `StartTime` zero (no user event precedes the first `message_start` — `haveLastUserTime` is false), but its `EndTime` is back-filled from the first user event's historical timestamp (lines 404-411). `EmitSpans` fills a zero `StartTime` with `time.Now()` — the reporter's wall-clock, which in production runs minutes after the dispatch Job wrote events.jsonl — while `EndTime` stays the historical timestamp. Result: `span.End(WithTimestamp(endTime))` with `endTime < startTime` — a negative-duration span on the first call of essentially every real conversation that used tools. Symmetrically, the *last* call gets `StartTime` = the last historical user timestamp and `EndTime` = `time.Now()`, inflating its duration by the entire Job-completion→reporter-spawn latency. The `writeTraceOnlyFixture`/`events_sample.jsonl` tests never assert on emitted timestamps, so nothing catches this.
**Fix:** Preserve temporal ordering in the fallback logic:
```go
startTime, endTime := call.StartTime, call.EndTime
switch {
case startTime.IsZero() && !endTime.IsZero():
    startTime = endTime // zero-duration, correctly ordered
case !startTime.IsZero() && endTime.IsZero():
    endTime = startTime
case startTime.IsZero() && endTime.IsZero():
    now := time.Now()
    startTime, endTime = now, now
}
if endTime.Before(startTime) {
    endTime = startTime
}
```

### WR-02: One oversized events.jsonl line drops ALL conversation spans — partial calls are returned then discarded

**Status:** fixed (commit `1330ced`)
**File:** `cmd/tide-reporter/main.go:316-320`; `internal/reporter/tracesynth.go:416-418`
**Issue:** `common.ReadLines` returns `bufio.ErrTooLong` for any line over 16 MB (stream_reader.go:56-65) — plausible for exactly the "tool result that dumps an entire generated file" case the 32 KiB truncation backstop was designed around. `bufio.Scanner` cannot resume past an oversized line, so `ReconstructConversation` deliberately returns the partial `calls` slice *alongside* the error (line 417) — but `synthesizeSpans` checks `err != nil` and returns without calling `EmitSpans`, discarding every successfully reconstructed call. The still-open `pending` call is also dropped (the EOF flush at lines 422-426 is skipped by the early return). This contradicts the D-05/D-11 posture the file documents ("a failed planner Job … still emits its conversation spans"): a single pathological line silently zeroes out the whole conversation's telemetry. Exit code is unaffected (D-10 holds), but the data is gone.
**Fix:** In `synthesizeSpans`, emit whatever was reconstructed before the error:
```go
calls, err := reporter.ReconstructConversation(eventsPath, inJSONPath, cfg.Workspace)
if err != nil {
    fmt.Fprintf(stderr, "tide-reporter: reconstruct conversation (partial, %d calls recovered): %v\n", len(calls), err)
}
if len(calls) == 0 {
    return
}
// mark the tail call Degraded when err != nil, then EmitSpans(calls...)
```
(Optionally also flush the pending call as Degraded before returning the read error in `ReconstructConversation`.)

### WR-03: `MessageContent.Signature` bypasses both the MSG-02 redaction pass and the size accounting

**Status:** fixed (commit `309ef6c`)
**File:** `internal/reporter/tracesynth.go:483-491`; `pkg/otelai/attrs.go:176-184`
**Issue:** MSG-02 mandates every message attribute passes `redact.SecretPatterns` before span emission. In `boundedMessageAttrs`, `Content`, `ArgumentsJSON`, and reasoning `Text` all go through `redactTruncate` — but `Signature` is copied verbatim (`Signature: c.Signature`, line 488) and emitted as `message_content.signature` (attrs.go:181-183) with no redaction, no truncation, and no contribution to the whole-span `total` (only `len(bc.Text)` is counted at line 489). Signatures originate from the wire stream the subagent tees to a PVC the subagent itself can write, so the field is attacker-populatable in the same threat model that justifies redacting `Content`. It is also the only unbounded string in the pipeline: a forged multi-megabyte `signature` value rides straight past both the 32 KiB per-message floor and the 512 KiB span cap into the export batch. Input snapshots repeat prior assistant turns, so one large signature is re-emitted on every subsequent call's span.
**Fix:** Route it through the same pipeline and count it:
```go
bc := otelai.MessageContent{
    Type:      c.Type,
    Text:      redactTruncate(c.Text),
    Signature: redactTruncate(c.Signature),
}
total += len(bc.Text) + len(bc.Signature)
```

### WR-04: `seedPrompt` follows an unvalidated `promptPath` — path traversal out of the workspace into the trace stream

**Status:** fixed (commit `7cb930f` — `os.Root` confinement, stronger than the suggested lexical check: also blocks symlink escapes)
**File:** `internal/reporter/tracesynth.go:186-217` (line 204)
**Issue:** `seedPrompt` joins `in.PromptPath` against `workspaceRoot` with `filepath.Join` and reads whatever it resolves to. `in.json` lives on the per-project PVC subPath that the dispatch Job's subagent mounts read-write — a prompt-injected or compromised subagent can rewrite it with `"promptPath": "../../../../var/run/secrets/kubernetes.io/serviceaccount/token"` (or any file readable inside the reporter container), and the reporter will read that file and emit its contents as the conversation's seed turn — i.e., into the OTLP collector and long-term trace storage. The MSG-02 redaction pass mitigates key/JWT-shaped leaks (the SA token is a JWT and would be caught by the `eyJ…` pattern), but `SecretPatterns` is a denylist — PEM keys, config files, and other envelopes' artifacts are not covered. The reporter reads files the subagent cannot, so this is a genuine privilege step-up, not a no-op.
**Fix:** Reject any promptPath that escapes the workspace before reading:
```go
full := filepath.Join(workspaceRoot, in.PromptPath)
rel, err := filepath.Rel(workspaceRoot, full)
if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
    return "", false // traversal attempt — degrade, don't read
}
```
(Go 1.20+: `if !filepath.IsLocal(in.PromptPath) { return "", false }` is the simplest guard.)

### WR-05: Combined-mode reporter Job retries re-emit the full span set — token counts and costs multi-count in Phoenix

**Status:** mitigated (sentinel-file gate; residual documented in Fix Summary below)
**File:** `cmd/tide-reporter/main.go:220-225`; `internal/controller/reporter_jobspec.go:243`
**Issue:** In combined mode `synthesizeSpans` runs before out.json read/validation (deliberate D-05 placement), and the reporter Job has `BackoffLimit: 2`. Any non-zero exit after synth — missing out.json (exit 2, the normal failed-planner case), K8s API error (exit 1), allowlist rejection (exit 2) — fails the pod and triggers a retry, and every retry re-runs `synthesizeSpans` with freshly minted span IDs (acknowledged at tracesynth's own comment: "a Job retry re-running this step emits a duplicate span"). A failed planner Job therefore emits up to 3 identical conversation span sets, each carrying full `llm.token_count.*` attributes — Phoenix's cost dashboards sum token counts across spans, so a failed planner's spend is triple-counted, and even a transient-then-successful materialization double-counts. The trace-only shape is immune (always exits 0); only the combined shape multiplies.
**Fix:** Add a cheap idempotency guard keyed to the run, e.g. a sentinel file next to the artifact (`envelopes/<taskUID>/.spans-emitted`) written after a successful `EmitSpans` and checked before synth — the PVC is already the durable medium the reporter trusts. Alternatively derive deterministic span IDs (hash of taskUID + call index) via a custom IDGenerator so retried emission overwrites rather than duplicates.

## Info

### IN-01: `truncateHeadTail`'s `limit` parameter is decoupled from the hardcoded `truncationHalf` — latent panic

**File:** `internal/reporter/tracesynth.go:439-447`
**Issue:** The function accepts an arbitrary `limit` but always slices `s[:truncationHalf]` / `s[len(s)-truncationHalf:]`. Any future call with `limit < 2*truncationHalf` panics with a slice-bounds error for inputs in `(limit, truncationHalf)`. Safe today only because the sole call site passes `maxMessageContentBytes == 2*truncationHalf`.
**Fix:** Derive the halves from the parameter (`half := limit / 2`) or drop the parameter entirely and use the constants.

### IN-02: `tide.trace.parse_degraded` is overloaded to also mean "size-budget degrade"

**File:** `internal/reporter/tracesynth.go:495-504, 561-563`; `pkg/otelai/attrs.go:104-107`
**Issue:** `keyParseDegraded` is documented as "reconstructed from an events.jsonl stream with skipped or truncated lines," but `EmitSpans` also sets it when `boundedMessageAttrs` drops a side to role-only for exceeding the whole-span budget. A Phoenix user cannot distinguish "the parse was lossy" from "content was deliberately dropped for size" — different debugging actions.
**Fix:** Either add a distinct `tide.trace.size_degraded` marker for the budget path or extend `keyParseDegraded`'s doc comment to cover both semantics explicitly.

### IN-03: Span name and `llm.model_name` come from the untrusted stream without redaction or bounding

**File:** `internal/reporter/tracesynth.go:531-534, 549`
**Issue:** `call.Model` is parsed straight from events.jsonl (subagent-writable) and used verbatim as the span name and `llm.model_name` — the only stream-derived strings that skip the redact/bound pipeline besides WR-03's Signature. Bounded in practice by the 16 MB line cap only.
**Fix:** `spanName = redact.String(call.Model)` with a small length clamp (e.g. 256 bytes) at reconstruction time.

### IN-04: Reporter Job env omits `OTEL_SERVICE_NAME` — spans land under `unknown_service`

**File:** `internal/controller/reporter_jobspec.go:227-233`
**Issue:** The reporter's TracerProvider builds its resource from env (`resource.WithFromEnv()`), but the Job env carries only the endpoint and batch size. Reporter-emitted LLM spans will group under `unknown_service:tide-reporter` in Phoenix/LangSmith rather than a stable service identity, fragmenting per-service views against the manager's configured service name.
**Fix:** Add `{Name: "OTEL_SERVICE_NAME", Value: "tide-reporter"}` to the env block when `opts.OTLPEndpoint != ""`.

### IN-05: Trace-only reporter mounts the PVC read-write despite never writing

**File:** `internal/controller/reporter_jobspec.go:302-311`
**Issue:** Neither reporter shape writes to the workspace, but the volume mount omits `ReadOnly: true`. The materialization shape inherited this from Phase 09; the new trace-only shape was an opportunity to tighten it (least-privilege posture consistent with the SA reuse rationale in T-44-06).
**Fix:** Set `ReadOnly: true` on the trace-only shape's VolumeMount (both shapes if the materialization path is confirmed write-free).

---

## Fix Summary (2026-07-16)

Fix scope: CR-01 + WR-01..WR-05 (Info findings deliberately out of scope). Every fix carries a proving test; full gate re-run green: `go build ./...`, `go test ./internal/reporter/... ./cmd/tide-reporter/... ./pkg/otelai/... ./internal/harness/redact/... -count=1`, `go vet ./internal/reporter/... ./cmd/tide-reporter/...`.

| Finding | Disposition | Commit | Mechanism |
|---------|-------------|--------|-----------|
| CR-01 | fixed | `266e81b` | `boundedSpanAttrs` enforces `maxSpanPayloadBytes` on the SUM of input+output attribute bytes (larger side degrades to role-only first, both when the survivor alone still exceeds the budget); ToolCall ID/Name bytes now counted. Constants unchanged. |
| WR-01 | fixed | `9390f4e` | Zero-time fallback collapses the missing side onto the known side (zero-duration, correctly ordered) + defensive `end<start` clamp; `TimingSynthetic` already marks all timing. |
| WR-02 | fixed | `1330ced` | `ReconstructConversation` flushes the still-open pending call (Degraded) before returning a read error; `synthesizeSpans` emits the partial calls with the tail marked Degraded (D-11). |
| WR-03 | fixed | `309ef6c` | `Signature` passes the same `redactTruncate` pipeline as Content/Text and counts toward the joint span budget. |
| WR-04 | fixed | `7cb930f` | `seedPrompt` resolves `promptPath` via `os.Root` — rejects `..`-traversal, absolute paths, AND symlink escapes (stronger than the suggested lexical `filepath.Rel` check); rejection degrades the seed per D-05. |
| WR-05 | mitigated | this commit | Idempotency sentinel `envelopes/<taskUID>/.spans-emitted` written on the PVC after a successful `EmitSpans`, checked before synthesis. Smallest mechanism consistent with D-10 (best-effort, never affects exit code) and CRD-status-only persistence (the PVC is already the reporter's durable medium). |

### WR-05 residual risk

1. **Sentinel precedes the export flush.** `EmitSpans` hands spans to the async BatchSpanProcessor; actual export happens at provider Shutdown. A pod killed between the sentinel write and the flush loses that conversation's spans permanently (the retry skips synthesis). This trades rare at-most-once loss for the multi-counted-costs bug — consistent with D-10's "broken tracing pipeline is visible only in pod logs" posture. A fully correct fix is deterministic span IDs (hash of taskUID+call index) via a custom IDGenerator threaded through `otelinit.NewTracerProvider` — a cross-phase surface change deferred deliberately.
2. **Depends on the reporter's read-write PVC mount.** Holds today (both shapes mount RW). If IN-05's least-privilege suggestion (RO mount) is adopted later, the sentinel write fails (logged, best-effort) and retry-duplication returns silently — IN-05's implementation must either except the sentinel path or land the deterministic-ID fix first.
3. **Sentinel outlives a legitimate re-emission need.** If an operator wipes Phoenix and wants spans re-synthesized from the PVC, the sentinel must be deleted manually.

---

_Reviewed: 2026-07-17T00:00:16Z_
_Reviewer: Claude (gsd-code-reviewer)_
_Depth: standard_
_Fixes applied: 2026-07-16 — Claude (gsd-code-fixer)_
