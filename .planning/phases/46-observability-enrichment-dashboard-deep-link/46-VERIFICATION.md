---
phase: 46-observability-enrichment-dashboard-deep-link
verified: 2026-07-17T06:46:57Z
status: passed
score: 4/4 must-haves verified
overrides_applied: 0
deferred:
  - truth: "Live Phoenix end-to-end proof — a real run's cost/trace/session rollup resolves in a self-hosted Phoenix and the rendered deep link navigates to the right span"
    addressed_in: "Phase 47"
    evidence: "Phase 47 goal: 'Self-Hosted Phoenix Install + End-to-End Proof — point TIDE's existing otel.exporter.endpoint chart value at [Phoenix], and see a real run's cost...'; every 46 SUMMARY notes the fix 'has not itself been proven against a live Phoenix instance (no live Phoenix in this plan's scope)'"
---

# Phase 46: Observability Enrichment + Dashboard Deep Link Verification Report

**Phase Goal:** Make the now-complete trace tree actually useful to an operator inside Phoenix and inside TIDE's own dashboard — a sane default sampler (so a demo run isn't a coin flip), a session identity that lets Phoenix compute an independent cost cross-check, filterable metadata/tags, and a one-click deep link from any DAG node straight to its trace.
**Verified:** 2026-07-17T06:46:57Z
**Status:** passed
**Re-verification:** No — initial verification

## Goal Achievement

### Observable Truths

| # | Truth (Success Criterion) | Status | Evidence |
| --- | --- | --- | --- |
| 1 | Chart trace-sampler default is 1.0 (up from 0.1) with opt-down documented (OBS-01) | ✓ VERIFIED | `tracesSamplerArg: "1.0"` in both `charts/tide/values.yaml:420` AND `hack/helm/tide-values.yaml:420` (byte-identical, `diff -q` clean). `helm template` default render emits `OTEL_TRACES_SAMPLER_ARG: "1.0"`. Only residual `0.1` is the legitimate opt-down example (`docs/observability.md:189` `--set otel.tracesSamplerArg=0.1`). Opt-down section is honest — states orphaned/rootless fragment semantics (`docs/observability.md:198`), not coherent per-span ratio sampling. Helm gate Permutation H (`assert-telemetry-render.sh`) pins 1.0 in CI via `make helm-assert` (ci.yaml:223) — ran green. |
| 2 | Every span carries `session.id` = Project UID; routed double-count resolved (OBS-02) | ✓ VERIFIED | `otelai.SessionID` (semconv-backed, `pkg/otelai/attrs.go:355`) stamped on every AGENT span (`span_emission.go:218`) and every reporter LLM span (`tracesynth.go:652`). D-03: `otelai.TokenCount` has exactly ONE production call site (`tracesynth.go:637`, per-call LLM spans) — grep confirms 0 in `span_emission.go` (the one match is the citing comment). All five AGENT levels drop token counts per `<planner_correction>` (supersedes RESEARCH Task-only). Regression guard `TestSynthesizePlannerSpanOmitsTokenCount` (5 levels) passes; full controller envtest suite green (114s). |
| 3 | Spans carry metadata/tag.tags enrichment for Phoenix DSL filtering (OBS-03) | ✓ VERIFIED | `buildLevelEnrichment` (`span_emission.go:317`) emits level kind + name, wave_index (Task-only via `owner.LabelWaveIndex`), gate_profile (omitted for project — Pitfall 5), failure_profile, failure_halt. `Tags()` → `attribute.STRINGSLICE`; `MetadataJSON()` → `attribute.STRING` JSON (Pitfall 4 encoding split, type-asserted in tests). Stamped on both span families identically (same-reconcile). `TestBuildLevelEnrichment{ProjectOmitsGateProfile,ConservativeFailureHalt,StrictDefault,NilProject}` pass. |
| 4 | Each DAG node deep-links to Phoenix when phoenix.baseURL set, no dead button when not (OBS-04) | ✓ VERIFIED | Full chain live: `phoenix.baseURL` value → conditional `PHOENIX_BASE_URL` env (`dashboard-deployment.yaml:76`) → `/api/v1/config` `phoenixBaseURL` (`config.go:55`) → App.tsx one-shot fetch (`App.tsx:390`) → `PhoenixTraceLink` at BOTH mounts (NodeDetailPanel×2 branches `App.tsx:652,683` + TaskDetailDrawer `TaskDetailDrawer.tsx:351`). Eligibility returns null on empty baseURL/spanId OR all-zero hex (`PhoenixTraceLink.tsx:44`, WR-01 read-path guard). One-place URL rule: 0 `/redirects/` outside `phoenixLink.ts` (incl. tests). SPA vitest 28/28 green including all-zero-spanId hide case. |

**Score:** 4/4 truths verified

### Deferred Items

| # | Item | Addressed In | Evidence |
|---|------|-------------|----------|
| 1 | Live Phoenix end-to-end proof (real run's cost/trace/session rollup resolves; deep link navigates to the right span in a self-hosted Phoenix ≥ 14.2.0) | Phase 47 | Phase 47 = "Self-Hosted Phoenix Install + End-to-End Proof". All 46 SUMMARYs explicitly scope the live proof out ("no live Phoenix in this plan's scope"). Phase 46 delivers the code + config + render contracts, all test-proven; the live click-through is Phase 47's PROOF-01. |

### Required Artifacts

| Artifact | Expected | Status | Details |
| -------- | ----------- | ------ | ------- |
| `pkg/otelai/attrs.go` | SessionID/Metadata/MetadataJSON/Tags semconv-backed helpers | ✓ VERIFIED | All 4 present (355/367/380/390); 4 semconv key refs; 0 hand-rolled literals; correct STRING-JSON vs STRINGSLICE encodings |
| `internal/controller/reporter_jobspec.go` | ReporterOptions.SessionID/MetadataJSON/Tags + conditional Args (both Job shapes) | ✓ VERIFIED | Fields 130/138/145; Args appends 259-269 (after SkipMessageSpans, ride both shapes) |
| `cmd/tide-reporter/main.go` | --session-id/--metadata/--tags flags + splitTags + EmitSpans threading | ✓ VERIFIED | Flags 133-135; `splitTags` 314; threaded into EmitSpans 382 |
| `internal/reporter/tracesynth.go` | EmitSpans stamps enrichment on every LLM span | ✓ VERIFIED | 7-arg signature 594; conditional SetAttributes 652/655/658 |
| `internal/controller/span_emission.go` | buildLevelEnrichment + enriched synthesizePlannerSpan + traceparentForLevel(sampled) + D-03 drop + WR-01 zero-guard | ✓ VERIFIED | buildLevelEnrichment 317; enrichment 218-224; token-count drop (comment 227); sampled bit captured 246; WR-01 emitted=false guard 262-265 |
| `charts/tide/values.yaml` + `hack/helm/tide-values.yaml` | tracesSamplerArg 1.0 + phoenix.baseURL "" | ✓ VERIFIED | Both files 420/431, byte-identical (chart-reproducibility hook honored) |
| `hack/helm/assert-phoenix-env.py` | render gate (--expect-absent / --expect-value) | ✓ VERIFIED | 127 lines; wired at Makefile 716/718; passes live |
| `cmd/dashboard/api/config.go` + projects.go + tasks.go | phoenixBaseURL + traceId/traceSpanId + graceful degradation | ✓ VERIFIED | config 55/65; projects TraceID/TraceSpanID 101/102/120/260-266; tasks resolveTaskTraceIdentity 200 (couples both fields empty on chain break) |
| `dashboard/web/src/lib/phoenixLink.ts` | phoenixTraceURL/phoenixSpanURL, normalization + encode, one place | ✓ VERIFIED | Both exports 27/31; normalizeBaseURL 23; one-place rule grep-clean |
| `dashboard/web/src/components/PhoenixTraceLink.tsx` | shared eligibility + anchor (UI-SPEC shape) + zero-hex reject | ✓ VERIFIED | Eligibility 44 (incl. `/^0+$/`); real `<a>` target/rel/aria-label/ExternalLink/mono-13px 56-67 |
| `dashboard/web/src/App.tsx` + TaskDetailDrawer.tsx | both mount points + one-shot config fetch | ✓ VERIFIED | 2 NodeDetailPanel mounts + config fetch + drawer prop; drawer mount 351 |

### Key Link Verification

| From | To | Via | Status | Details |
| ---- | --- | --- | ------ | ------- |
| reporter_jobspec.go | tide-reporter main.go | `--session-id/--metadata/--tags` Args ↔ registered flags | ✓ WIRED | Crash-loop guard honored — Args (259-269) match flags (133-135); WR-04 `TestParseFlagsEnrichmentTriple` proves consumer side |
| tide-reporter main.go | tracesynth.go | synthesizeSpans → EmitSpans threading | ✓ WIRED | `EmitSpans(...cfg.SessionID, cfg.MetadataJSON, splitTags(cfg.TagsCSV))` at 382 |
| span_emission.go | pkg/otelai/attrs.go | otelai.SessionID/MetadataJSON/Tags in synthesizePlannerSpan | ✓ WIRED | 218-224 |
| task_controller.go (+4) | reporter_jobspec.go | ReporterOptions{SessionID,MetadataJSON,Tags} at all 5 spawns | ✓ WIRED | `MetadataJSON:` present at all 5 spawn sites (milestone/phase/plan/project/task) |
| span_emission.go | 9× traceparentForLevel | sampled bool threaded | ✓ WIRED | 9 call sites: 5 spawn-time pass real `sampled`, 4 dispatch-time pass literal `true` with limitation comment |
| main.go | config.go | PHOENIX_BASE_URL → Dependencies → ConfigHandler | ✓ WIRED | main.go:169 → router.go:221 → config.go:65 |
| projects.go | pkg/otelai/tracecontext.go | otelai.TraceIDFromUID(project.UID) | ✓ WIRED | projects.go:265 |
| App.tsx | PhoenixTraceLink.tsx | mounts + phoenixBaseURL prop from config fetch | ✓ WIRED | 2 mounts + drawer prop |
| PhoenixTraceLink.tsx | phoenixLink.ts | phoenixSpanURL(baseURL, spanId) href | ✓ WIRED | import 4, href 46 |
| api.ts | projects.go/tasks.go | TS mirrors of traceId/traceSpanId/phoenixBaseURL | ✓ WIRED | DashboardConfig 30, ChildRef/ProjectDetail/TaskDetailJSON mirrors present |

### Data-Flow Trace (Level 4)

| Artifact | Data Variable | Source | Produces Real Data | Status |
| -------- | ------------- | ------ | ------------------ | ------ |
| AGENT span attrs | session.id/metadata/tags | `project.UID` + `project.Spec/Status/Labels` (trusted CRD) via buildLevelEnrichment | Yes (in-reconciler from CRD) | ✓ FLOWING |
| reporter LLM spans | session.id/metadata/tags | manager-authored Job Args → cfg → EmitSpans | Yes (same buildLevelEnrichment inputs, byte-identical same-reconcile) | ✓ FLOWING |
| projectDetail/taskDetail | traceId/traceSpanId | `TraceIDFromUID(UID)` (deterministic) + `{Level}TraceSpanID` status | Yes; empty-on-failure, never fabricated; zero-hex never persisted (WR-01) | ✓ FLOWING |
| PhoenixTraceLink href | phoenixBaseURL + spanId | GET /api/v1/config (raw env) + payload traceSpanId | Yes when configured; null when empty/zero — no dead button | ✓ FLOWING |

### Behavioral Spot-Checks

| Behavior | Command | Result | Status |
| -------- | ------- | ------ | ------ |
| otelai/reporter/tide-reporter/dashboard tests | `go test ./pkg/otelai/... ./internal/reporter/... ./cmd/tide-reporter/... ./cmd/dashboard/...` | all ok | ✓ PASS |
| Controller envtest (D-03/enrichment/sampled/WR-01 guards) | `go test ./internal/controller/...` (KUBEBUILDER_ASSETS set) | `ok 114.737s` | ✓ PASS |
| SPA phoenix deep-link (render/hide/zero-hex/placement) | `npx vitest run phoenixLink.test.ts node-panel-integration.test.tsx drawer.test.tsx` | 28/28 | ✓ PASS |
| Default chart sampler render | `helm template --set dashboard.enabled=true \| grep OTEL_TRACES_SAMPLER_ARG` | `"1.0"` | ✓ PASS |
| PHOENIX_BASE_URL absent default / present when set | `helm template` ± `--set phoenix.baseURL=...` | absent(0) / `"http://phoenix:6006"` | ✓ PASS |
| Helm render gate (OBS-01 + phoenix env) | `make helm-telemetry-assert` | 8/8 permutations, EXIT=0 | ✓ PASS |
| No schema change (D-02) | `git diff --name-only caf0125~1 HEAD -- api/v1alpha3/ config/crd/` | empty | ✓ PASS |

### Requirements Coverage

| Requirement | Source Plan(s) | Description | Status | Evidence |
| ----------- | ---------- | ----------- | ------ | -------- |
| OBS-01 | 46-02 | Chart sampler default 0.1→1.0 + opt-down documented | ✓ SATISFIED | Truth 1 |
| OBS-02 | 46-01, 46-04 | Every span carries session.id = Project UID (independent cost rollup) | ✓ SATISFIED | Truth 2 |
| OBS-03 | 46-01, 46-04 | metadata/tag.tags enrichment for Phoenix DSL filtering | ✓ SATISFIED | Truth 3 |
| OBS-04 | 46-02, 46-03, 46-05 | Dashboard deep-links each DAG node to Phoenix; no dead button when unconfigured | ✓ SATISFIED | Truth 4 |

All four phase requirement IDs (OBS-01..04) are declared across the plans and satisfied. No orphaned requirements — REQUIREMENTS.md maps exactly OBS-01..04 to Phase 46, all claimed by a plan.

### Anti-Patterns Found

| File | Line | Pattern | Severity | Impact |
| ---- | ---- | ------- | -------- | ------ |
| (none) | — | No debt markers (TBD/FIXME/XXX) or stub returns in phase-modified files | — | Enrichment/config values are conditional-stamp ("absent when empty, never fabricated"); empty-string returns are the deliberate no-link/no-attr sentinel, not stubs (data-fetching/derivation writes real values on the happy path) |

### Human Verification Required

None for Phase 46's code-verifiable scope. The render/hide/href behavior of the deep link is fully covered by React integration tests (DOM href/target/rel/aria-label/testid + hide-states + all-zero-hex). The live browser-against-real-Phoenix click-through is explicitly Phase 47's deliverable (see Deferred Items) — not surfaced here to avoid double-flagging a scheduled phase.

### Gaps Summary

No gaps. All four success criteria are observably true in the codebase and test-proven:

- **OBS-01** — sampler default is 1.0 across both canonical values files, honestly opt-down-documented, CI-pinned; the only lingering `0.1` is the intended opt-down example.
- **OBS-02** — session.id = Project UID on all five AGENT spans and all reporter LLM spans; the routed double-count decision is resolved the way 46-04's `<planner_correction>` mandates (llm.token_count.* dropped at ALL five levels, not Task-only) so the single per-call LLM-span source is authoritative — `otelai.TokenCount` has exactly one production call site.
- **OBS-03** — metadata (JSON STRING) + tag.tags (STRINGSLICE) carry level/name/wave_index/gate_profile/failure_profile/failure_halt, byte-identical across sibling spans in the same reconcile.
- **OBS-04** — the full config chain renders a real `<a href>` deep link at both DAG-node detail surfaces when phoenix.baseURL is set and returns null (no dead button) when it isn't — including the WR-01 all-zero-hex path guarded on both the write side (never persist zero) and the read side (SPA eligibility + backend never-persist).

All four post-review fix commits (WR-01 `565daae`, WR-02 `8df3cfd`, WR-03 `2f1e26c`, WR-04 `8e85b9e`) are present on main and their effects verified in the current tree, not merely claimed: WR-01's `emitted=false` zero-span guard (`span_emission.go:262`) and SPA `/^0+$/` reject (`PhoenixTraceLink.tsx:44`); WR-04's `TestParseFlagsEnrichmentTriple` + `TestSplitTags`. WR-02/WR-03 were documentation-scope fixes (honest same-reconcile scoping) — the doc/comment text is present and the underlying behavior is inert at the 1.0 default.

Code-review Info items IN-01..IN-10 remain open by design (all non-blocking; IN-06/IN-08/IN-10 are pre-existing). Notable non-blocking observations:
- REQUIREMENTS.md traceability table still shows OBS-01 and OBS-04 as "Pending" (lines 87/90) with unchecked boxes (lines 41/44) — a tracking-doc lag, not a code gap; the code satisfies all four. The orchestrator normally reconciles these post-verification.

---

_Verified: 2026-07-17T06:46:57Z_
_Verifier: Claude (gsd-verifier)_
