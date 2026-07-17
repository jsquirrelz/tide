---
phase: 46-observability-enrichment-dashboard-deep-link
reviewed: 2026-07-17T06:21:04Z
depth: standard
files_reviewed: 43
files_reviewed_list:
  - charts/tide/templates/dashboard-deployment.yaml
  - charts/tide/values.yaml
  - cmd/dashboard/api/config_test.go
  - cmd/dashboard/api/config.go
  - cmd/dashboard/api/projects_test.go
  - cmd/dashboard/api/projects.go
  - cmd/dashboard/api/tasks_test.go
  - cmd/dashboard/api/tasks.go
  - cmd/dashboard/main_test.go
  - cmd/dashboard/main.go
  - cmd/dashboard/router.go
  - cmd/tide-reporter/main.go
  - dashboard/web/src/App.tsx
  - dashboard/web/src/components/__tests__/drawer.test.tsx
  - dashboard/web/src/components/__tests__/node-panel-integration.test.tsx
  - dashboard/web/src/components/PhoenixTraceLink.tsx
  - dashboard/web/src/components/TaskDetailDrawer.tsx
  - dashboard/web/src/lib/api.ts
  - dashboard/web/src/lib/phoenixLink.test.ts
  - dashboard/web/src/lib/phoenixLink.ts
  - dashboard/web/src/lib/tasks.ts
  - docs/observability.md
  - hack/helm/assert-phoenix-env.py
  - hack/helm/assert-telemetry-render.sh
  - hack/helm/tide-values.yaml
  - internal/controller/dispatch_helpers.go
  - internal/controller/milestone_controller.go
  - internal/controller/phase_controller.go
  - internal/controller/plan_controller.go
  - internal/controller/project_controller.go
  - internal/controller/reporter_jobspec_test.go
  - internal/controller/reporter_jobspec.go
  - internal/controller/span_emission_test.go
  - internal/controller/span_emission_unit_test.go
  - internal/controller/span_emission.go
  - internal/controller/task_controller.go
  - internal/otelinit/doc.go
  - internal/otelinit/provider.go
  - internal/reporter/tracesynth_test.go
  - internal/reporter/tracesynth.go
  - pkg/otelai/attrs_test.go
  - pkg/otelai/attrs.go
findings:
  critical: 0
  warning: 4
  info: 10
  total: 14
status: issues_found
---

# Phase 46: Code Review Report

**Reviewed:** 2026-07-17T06:21:04Z
**Depth:** standard
**Files Reviewed:** 43
**Status:** issues_found

## Summary

Reviewed the Phase 46 OpenInference enrichment + Phoenix deep-link surface end-to-end:
the enrichment triple (session.id / metadata / tag.tags) on all five AGENT spans and
reporter LLM spans, the D-03 token-count drop from AGENT spans, the D-02 sampled-bit
threading at all nine `traceparentForLevel` call sites, the chart sampler flip to 1.0,
and the full deep-link config chain (`phoenix.baseURL` → `PHOENIX_BASE_URL` →
`/api/v1/config` → `phoenixLink.ts` → both PhoenixTraceLink mounts).

Core invariants verified by tracing code (not just tests): `otelai.TokenCount` has
exactly one production call site (`internal/reporter/tracesynth.go:637`, the per-call
LLM spans) — the locked no-double-count invariant holds; the enrichment triple is
computed from the same `buildLevelEnrichment` inputs at both the AGENT span and the
reporter spawn in the common path; all four dispatch-time sites pass literal `true` and
all five reporter-spawn sites thread the real sampled bit; the reporter Job carries no
`OTEL_TRACES_SAMPLER` env, so its SDK-default `ParentBased(AlwaysSample)` correctly
follows the threaded traceparent flag. Verification: `go test` green on
`pkg/otelai`, `internal/reporter`, `cmd/tide-reporter`, `cmd/dashboard/...`, and the
controller unit-test subset; `go vet` clean; vitest green on the three SPA test files
(27/27); `hack/helm/tide-values.yaml` is byte-identical to `charts/tide/values.yaml`.

No blockers found. Four warnings: a dead-Phoenix-link path via persisted all-zero span
IDs from tracing-dark runs (mechanism verified against the pinned
otel/trace@v1.43.0 noop source), two cross-reconcile coherence gaps in the
sampled-bit/enrichment threading that contradict documented guarantees in edge paths,
and a missing parseFlags regression guard for the three new reporter CLI flags — a
crash-loop-class contract this repo's own comments declare and previously tested for
every prior flag addition.

## Warnings

### WR-01: Persisted all-zero span IDs from tracing-dark runs surface as dead Phoenix deep links

**File:** `internal/controller/span_emission.go:154-247` (mechanism), `cmd/dashboard/api/projects.go:260`, `cmd/dashboard/api/tasks.go:216`, `dashboard/web/src/components/PhoenixTraceLink.tsx:40` (surfacing)
**Confidence:** high on mechanism, medium on operator exposure
**Issue:** When `OTEL_EXPORTER_OTLP_ENDPOINT` is empty (tracing-dark, the chart default), `otelinit` installs the no-op TracerProvider. The noop `Tracer.Start` (verified in `go.opentelemetry.io/otel/trace@v1.43.0/noop/noop.go`) returns the *propagated parent* SpanContext untouched. In `synthesizePlannerSpan`, the reconstructed parent SpanContext for the Project root has a valid TraceID but a **zero SpanID**, and the SpanContext is non-zero, so `span.SpanContext().SpanID()` returns zero — yet `emitted=true` is returned and the handlers persist `ProjectTraceSpanID = "0000000000000000"`. Because every child level parents on that zero-hex (`spanIDFromHexOrZero` rejects it → zero again), **all five `{Level}TraceSpanID` status fields persist `"0000000000000000"` for any Job completed while tracing was dark**. This was cosmetic pre-Phase-46; now the dashboard serializes it (`omitempty` only filters `""`, not zero-hex) and `PhoenixTraceLink`'s eligibility check (`!baseURL || !spanId`) treats it as a real span — rendering a "View trace in Phoenix" link to `/redirects/spans/0000000000000000`. Realistic exposure: an operator runs TIDE dark, later enables OTLP + `phoenix.baseURL` — every pre-upgrade CR now shows a dead deep link. Similarly, with `tracesSamplerArg < 1.0`, a ratio-dropped root span persists a *real but never-exported* SpanID, producing a dead link Phoenix cannot resolve.
**Fix:** Treat the all-zero hex as absent at the serialization boundary (single change covers both mounts):
```go
// cmd/dashboard/api/projects.go / tasks.go — shared helper
func spanIDHexOrEmpty(hex string) string {
    if hex == "" || hex == "0000000000000000" {
        return ""
    }
    return hex
}
// detail.TraceSpanID = spanIDHexOrEmpty(p.Status.ProjectTraceSpanID) — and at every childRef/taskDetail site
```
Alternatively (defense in depth), extend `PhoenixTraceLink`'s eligibility: `if (!baseURL || !spanId || /^0+$/.test(spanId)) return null;`. Root-cause option: have `synthesizePlannerSpan` return `emitted=false` (or skip persistence) when the minted SpanID is invalid — `if !thisSpanID.IsValid()` at the handler persistence sites.

### WR-02: Project-level `sampled := true` default contradicts a ratio-dropped root when reporter spawn crosses a reconcile boundary

**File:** `internal/controller/project_controller.go:1826-1831` (default), `:1915` (consumption); `docs/observability.md:201-208` (contradicted claim)
**Confidence:** high on mechanism, low on likelihood (requires non-default `tracesSamplerArg < 1.0` plus a Create-failure/crash between emission and spawn)
**Issue:** The `sampled := true` default is justified by the copy-pasted comment "matches today's behavior (and RESEARCH's SDK read that every non-root span is AlwaysSample'd)". That justification is correct at the four non-root levels — their reconstructed parent SpanContext hardcodes `trace.FlagsSampled`, so `ParentBased` always samples them and the default can never diverge from reality. But **Project is the root**: its span IS ratio-gated, so its real sampled bit can be `false`. When the span emission and the reporter-Job Create happen in the same reconcile (the common path), the real bit threads through correctly. When they diverge — reporter `Create` returns a transient error → handler returns error → requeue; or a crash between stamp/emit and spawn — the next reconcile skips emission (marker stamped), `sampled` stays at the default `true`, and the reporter receives a `-01`-flagged traceparent for a root span that was never exported. Its LLM message spans then export as exactly the "orphaned LLM-message fragment from the root level" that `docs/observability.md` explicitly claims can no longer happen ("a ratio-dropped Project root also drops its own reporter's LLM spans"). Fully inert at the 1.0 default; incorrect doc claim + inapplicable code comment regardless.
**Fix:** Minimum: correct the comment at the project site (the "every non-root span" rationale does not apply to the root) and soften the docs claim to "in the same-reconcile path". Behavioral fix if desired: since the root's sampled bit is deterministic for a given TraceID under `traceidratio`, it can be recomputed at spawn time instead of defaulted — or persist nothing and accept the documented limitation symmetrically with the cross-level case.

### WR-03: Enrichment metadata/tags recomputed at reporter-spawn time can diverge from the AGENT span's values, breaking the D-05 byte-identical invariant

**File:** `internal/controller/milestone_controller.go:652`, `internal/controller/phase_controller.go` (same pattern), `internal/controller/plan_controller.go:659`, `internal/controller/project_controller.go:1911`, `internal/controller/task_controller.go:1102`
**Confidence:** high on mechanism, low on likelihood
**Issue:** The AGENT span's `metadata`/`tag.tags` are computed inside `synthesizePlannerSpan` at emission time; the reporter Job's `--metadata`/`--tags` args are computed by a *second* `buildLevelEnrichment` call at spawn time. In the common same-reconcile path both calls see the same in-memory `project` and `json.Marshal`'s sorted-key output makes them byte-identical, as the code comments claim. But spawn is idempotent-retried across reconciles (Create failure → requeue; crash between emit and spawn), and each retry re-fetches the Project. If `ConditionFailureHalt` flips, `Spec.FailureProfile` or a gate policy is edited between the emission reconcile and the successful spawn reconcile, the reporter's LLM spans carry **different** `metadata`/`tag.tags` than the sibling AGENT span — silently breaking the "Phoenix's filter DSL and ProjectSession grouping require byte-identical values across sibling spans (D-05)" contract stated in `buildLevelEnrichment`'s own doc comment. `session.id` (Project UID) is immune; only metadata/tags drift. Same cross-reconcile root cause as WR-02.
**Fix:** Either document the same-reconcile scope of the byte-identical guarantee at `buildLevelEnrichment` (matching the WR-02 doc fix), or make it structural: compute the enrichment pair once per completion handler *before* emission and pass it into both `synthesizePlannerSpan` and the `ReporterOptions` — the values are then identical by construction within a reconcile, and the residual cross-reconcile drift window narrows to the sampled-bit one already documented.

### WR-04: No parseFlags regression tests for the three new reporter flags — the declared crash-loop-class flag-sync contract is untested on the consuming side

**File:** `cmd/tide-reporter/main.go:113-135` (flag set), `cmd/tide-reporter/main_test.go` (gap)
**Confidence:** high
**Issue:** `parseFlags`'s own doc comment declares the contract: "an Args entry without a registered flag would crash-loop every reporter Job in the cluster, so the flag set and BuildReporterJob's Args must stay in sync" (a mismatched flag makes `parseFlags` error → exit 2 → BackoffLimit=2 → materialization broken cluster-wide). Every prior flag addition honored this with a paired test — Test 7 (`--traceparent`, Phase 43) and Test 8b (`--skip-message-spans`, Phase 45) exist in `main_test.go`. Phase 46 adds three producer-side tests (`TestBuildReporterJob_SessionIDArg/_MetadataArg/_TagsArg` assert the Args are *emitted*) but **zero consumer-side tests**: nothing asserts `parseFlags` accepts `--session-id`/`--metadata`/`--tags` or that the parsed values land on `SessionID`/`MetadataJSON`/`TagsCSV` (the "registered-but-never-copied flag silently no-ops" pitfall Test 8b names is equally live here — a dropped struct-copy line would silently strip enrichment from every reporter LLM span with all current tests green). The CSV-splitting logic in `synthesizeSpans` (empty-segment filtering, `"a,,b"` → `["a","b"]`) is also untested.
**Fix:** Add the precedent-shaped test:
```go
func TestParseFlagsEnrichmentTriple(t *testing.T) {
    cfg, err := parseFlags([]string{
        "--task-uid=t", "--session-id=uid-1",
        `--metadata={"level":"task"}`, "--tags=task,strict",
    })
    if err != nil { t.Fatalf("parseFlags: %v", err) }
    if cfg.SessionID != "uid-1" { t.Errorf("SessionID = %q", cfg.SessionID) }
    if cfg.MetadataJSON != `{"level":"task"}` { t.Errorf("MetadataJSON = %q", cfg.MetadataJSON) }
    if cfg.TagsCSV != "task,strict" { t.Errorf("TagsCSV = %q", cfg.TagsCSV) }
}
```
plus a small table test for the tags split (empty, trailing comma, `"a,,b"`).

## Info

### IN-01: config handler Content-Type diverges from the rest of the dashboard API

**File:** `cmd/dashboard/api/config.go:60-70`
**Confidence:** high
**Issue:** `ConfigHandler.Get` hand-rolls its response with `Content-Type: application/json` while every other handler in the package goes through `writeJSON` and emits `application/json; charset=utf-8`. The divergence is locked in by `TestConfigHandlerGet`'s exact-match assertion, so any future consolidation is now a test-visible "breaking" change.
**Fix:** Route through `writeJSON(w, http.StatusOK, resp)` and update the test's Content-Type expectation, or accept and note the divergence.

### IN-02: `normalizeBaseURL` strips exactly one trailing slash; no scheme guard on the deep-link href

**File:** `dashboard/web/src/lib/phoenixLink.ts:23-25`
**Confidence:** high (behavior), low (exploitability — value is operator-controlled)
**Issue:** `"http://phoenix:6006//"` produces `http://phoenix:6006//redirects/...`. Also, the href is built from a config-served string with no scheme validation — a `javascript:` base URL would render a live anchor. The value comes from Helm values (trusted operator input), so this is defense-in-depth only.
**Fix:** `baseURL.replace(/\/+$/, "")` and, optionally, render nothing unless the URL parses with `http:`/`https:` protocol.

### IN-03: Project-detail trace identity is not coupled the way the task endpoint (and the api.ts comment) describe

**File:** `cmd/dashboard/api/projects.go:255-267` vs `cmd/dashboard/api/tasks.go:193-217`
**Confidence:** high
**Issue:** `tasks.go` degrades `traceId` and `traceSpanId` to empty *together* ("a spanId with no traceId to anchor it is not a usable Phoenix link"), and `api.ts` documents that coupling. `projects.go`'s `buildDetail` sets `TraceSpanID` unconditionally and only omits `TraceID` on a derive error — so a project/childRef can carry `traceSpanId` with no `traceId`. Harmless today because `PhoenixTraceLink` keys solely on `spanId`, but the two endpoints now express two different contracts for the same field pair.
**Fix:** Either couple them in `buildDetail` (clear `TraceSpanID` when `TraceIDFromUID` fails) or document the projects-side contract as spanId-only.

### IN-04: `PhoenixTraceLink` accepts a `traceId` prop that the implementation never reads

**File:** `dashboard/web/src/components/PhoenixTraceLink.tsx:21-40`
**Confidence:** high
**Issue:** `traceId` is required in `PhoenixTraceLinkProps` but not destructured or used — it exists only as the documented seam for the `phoenixTraceURL` fallback swap. Callers thread real data (`projectDetail?.traceId ?? ""`) into a prop with zero behavioral effect; a future reader may reasonably assume it affects eligibility.
**Fix:** Acceptable as a documented seam; consider marking it optional (`traceId?: string`) so the dead-data threading at the three mount points can be dropped until the fallback is actually needed.

### IN-05: `phoenixTraceURL` is exported production code with no production caller

**File:** `dashboard/web/src/lib/phoenixLink.ts:27-29`
**Confidence:** high
**Issue:** Only tests and doc comments reference it. The file's header documents it as the deliberate fallback floor (one-line swap in PhoenixTraceLink), so this is intentional — flagged for the record under the dead-code rule.
**Fix:** None required; keep the doc comment as the justification anchor.

### IN-06: NodeDetailPanel trace links render from a stale `projectDetail` snapshot

**File:** `dashboard/web/src/App.tsx:364-380, 645-700`
**Confidence:** high
**Issue:** The `traceId`/`traceSpanId` feeding both NodeDetailPanel `PhoenixTraceLink` mounts come from `projectDetail`, fetched once per `selectedProject`/`selectedNamespace` change with no SSE-triggered refetch. A node whose span emits while the operator has the panel open (the common "watch a run live" flow) shows no link until the project is re-selected or the page reloads. The task drawer does not have this problem (`useTaskDetail` refetches on SSE Task events).
**Fix:** Refetch `projectDetail` on the same debounced SSE trigger PlanningDAGView uses, or accept the staleness and note it.

### IN-07: `assert-telemetry-render.sh` env-value assertions match any container in the whole render

**File:** `hack/helm/assert-telemetry-render.sh:288-296` (permutation H; same pattern in E/F)
**Confidence:** high
**Issue:** `grep -A1 'name: OTEL_TRACES_SAMPLER_ARG' | grep 'value: "1.0"'` passes if *any* container's entry carries 1.0 — it cannot catch per-container drift (e.g. manager flipped to 1.0 while the dashboard template hardcodes something else). Both currently read the same values key, so drift requires template divergence. The sibling `assert-phoenix-env.py` does this correctly (walks YAML, targets the named container).
**Fix:** Low priority; if the gate should be per-container, extend the python walker pattern instead of grep -A1.

### IN-08: Reporter-spawn logic exists in three shapes; Phase 46 had to hand-edit each

**File:** `internal/controller/dispatch_helpers.go:102-141` (helper), `internal/controller/plan_controller.go:645-683`, `internal/controller/project_controller.go:1899-1930` (inline near-copies)
**Confidence:** high
**Issue:** Milestone/Phase spawn via `spawnReporterIfNeeded`; Plan and Project carry inline near-duplicates of the same Get→NotFound→Build→Create→AlreadyExists dance. The Phase 46 enrichment threading therefore touched four divergent sites (plus Task's trace-only variant) — exactly the lockstep-drift risk the helper's own header says it exists to prevent. Pre-existing structure; this phase widened the duplicated surface (field-order/`SkipMessageSpans` position already differ between sites).
**Fix:** Fold Plan/Project onto `spawnReporterIfNeeded` in a follow-up (Plan/Project need the `isFirstCompletion` distinction the helper already returns).

### IN-09: Tags CSV transport is safe only by enum discipline

**File:** `internal/controller/reporter_jobspec.go:266-270`, `cmd/tide-reporter/main.go:367-372`
**Confidence:** high (currently safe — verified `GatePolicy` is `+kubebuilder:validation:Enum=auto;approve;pause` and failure profiles are enum'd)
**Issue:** The comma-joined `--tags=` arg relies on "tag values are level/gate/profile enums — no commas possible". True today, and CRD validation enforces it. Nothing structural in `buildLevelEnrichment` or `BuildReporterJob` prevents a future tag source with a comma from silently splitting into two tags.
**Fix:** Cheap hardening when convenient: skip/reject tags containing `,` at the join site, or note the invariant in `buildLevelEnrichment` where new tags would be added.

### IN-10: Stale doc comment on `resolveWaveIndex` (pre-existing)

**File:** `cmd/dashboard/api/tasks.go:261-264`
**Confidence:** high
**Issue:** The function doc says it "filters to those whose Spec.PlanRef matches the Task's PlanRef", but v1alpha3 Waves have no `Spec.PlanRef` — the inline comment at :275-277 corrects this. Pre-existing (not introduced this phase); noted because the file was in scope.
**Fix:** Align the doc comment with the inline v1alpha3 note (Waves are global-scope; association is by TaskRefs membership only).

---

_Reviewed: 2026-07-17T06:21:04Z_
_Reviewer: Claude (gsd-code-reviewer)_
_Depth: standard_
