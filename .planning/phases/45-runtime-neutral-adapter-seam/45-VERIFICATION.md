---
phase: 45-runtime-neutral-adapter-seam
verified: 2026-07-17T03:04:12Z
status: passed
score: 3/3 must-haves verified
overrides_applied: 0
---

# Phase 45: Runtime-Neutral Adapter Seam Verification Report

**Phase Goal:** Turn the events.jsonl→spans synthesizer from Phase 44 into a per-runtime adapter behind the Subagent seam, so a future self-instrumenting runtime (the LangGraph beachhead) can skip synthesis without any TIDE call site caring which runtime is live. Pure forward-compatibility scaffolding — every vendor today returns "not self-instrumenting," so behavior is unchanged; the seam and its contract test exist now so this isn't discovered for the first time when LangGraph actually lands.
**Verified:** 2026-07-17T03:04:12Z
**Status:** passed
**Re-verification:** No — initial verification

## Goal Achievement

### Observable Truths

| # | Truth | Status | Evidence |
|---|-------|--------|----------|
| 1 | A self-instrumenting capability flag travels as data derived from the manager's resolved `Provider.Vendor` — never a hard-coded per-runtime branch in the reporter or manager | ✓ VERIFIED | `pkg/dispatch/vendor_capabilities.go:38` `SelfInstruments(vendor string) bool` is a pure data lookup. All 5 spawn sites compute the flag from `SelfInstruments(ResolveProvider(project, "<level>", ...).Vendor)` (grep count = 5, one per controller with the correct level literal). Threaded through `ReporterOptions.SkipMessageSpans` → `--skip-message-spans` Job Arg. Source-shape gate `grep -rn "if.*[Vv]endor.*==" internal/controller cmd/tide-reporter internal/reporter` returns **zero hits** (exit 1) — no per-runtime branch anywhere |
| 2 | When the flag is set for a Task's resolved provider, the reporter skips message-span synthesis entirely — no double-emission path exists | ✓ VERIFIED | `cmd/tide-reporter/main.go:321` — `if cfg.SkipMessageSpans { ...; return }` is the **literal first statement** of `synthesizeSpans`, before `eventsPath` construction (:325) and before the sentinel `os.Stat` (:329). Both call sites (:202 trace-only, :229 combined) route through this one function; `reporter.EmitSpans`/`ReconstructConversation` are called **only** inside it (no bypass path). `TestRunTraceOnly_SkipsSynthesisWhenFlagSet` passes: zero spans, no `.spans-emitted` sentinel, exit 0, against the same fixture that normally yields 2 spans |
| 3 | A contract test using a stub self-instrumenting runtime proves zero duplicate spans end-to-end (env-carrier extraction only — no LangGraph-specific span shape assumed) | ✓ VERIFIED | `cmd/tide-reporter/adapter_seam_test.go:46` `TestAdapterSeam_SelfInstrumentingRuntimeNoDuplicateSpans` shares one `tracetest.InMemoryExporter` between a stub runtime span and a `SkipMessageSpans: true` reporter run over a real 2-call `events.jsonl`; asserts `len(spans) != 1` (exact-count, not `>=1`), span name `stub.graph.invoke`, injected TraceID, and sentinel absence. Uses only `otelai.ExtractRemoteParent` env-carrier extraction. `grep -i "langgraph\|langchain"` returns zero hits (exit 1). Passes fresh (`-count=1`) |

**Score:** 3/3 truths verified

### Required Artifacts

| Artifact | Expected | Status | Details |
|----------|----------|--------|---------|
| `pkg/dispatch/vendor_capabilities.go` | `SelfInstruments(vendor string) bool` fail-closed lookup, D-02/D-03 doc contract | ✓ VERIFIED | 45 lines, zero imports, switch over 5 canonical vendors + `default`, all arms `false`. Doc comment cites D-02 (manager-computed/Job-carried), D-03 (default-safe/fail-closed), names `internal/reporter/tracesynth.go` as the anthropic-CLI adapter |
| `pkg/dispatch/vendor_capabilities_test.go` | D-10 polarity guard tests | ✓ VERIFIED | `TestSelfInstruments_KnownVendorsDefaultFalse` + `TestSelfInstruments_UnknownVendorDefaultsFalse` (covers `""` and unknown-vendor). Both PASS fresh |
| `internal/controller/reporter_jobspec.go` | `ReporterOptions.SkipMessageSpans` + conditional `--skip-message-spans` Args append | ✓ VERIFIED | Field at :121 with zero-value-safe doc comment; append at :229-231 placed after the `--traceparent` block, bareword (no `=value`), rides both Job shapes. No Env entry added (D-04) |
| `internal/controller/reporter_jobspec_test.go` | D-11 flag-threading test on built Job Args | ✓ VERIFIED | `TestBuildReporterJob_SkipMessageSpansArg` with 3 subtests (materialization-present, trace-only-present, absent-when-false). PASS |
| `cmd/tide-reporter/main.go` | flag registration + `reporterConfig.SkipMessageSpans` + D-05 skip guard | ✓ VERIFIED | `fs.Bool("skip-message-spans", false, ...)` at :127, copied into struct literal at :143; guard is first statement of `synthesizeSpans` (:321) |
| `cmd/tide-reporter/adapter_seam_test.go` | D-09 contract test, ≥40 lines | ✓ VERIFIED | 109 lines, substantive, meaningful exact-count assertion. PASS |
| `internal/reporter/tracesynth.go` | D-08 doc contract naming SelfInstruments as routing datum | ✓ VERIFIED | Package doc (:22-24) names it the anthropic-CLI adapter + `SelfInstruments` routing datum; inline comment (:623-626) resolves the stale Phase-45 forward reference. Diff since phase base is **comment-only** (zero non-comment `+/-` lines) |

### Key Link Verification

| From | To | Via | Status | Details |
|------|-----|-----|--------|---------|
| 5 controller spawn sites | `pkg/dispatch.SelfInstruments` | fresh `ResolveProvider` call per site | ✓ WIRED | milestone:639 `"milestone"`, phase:592 `"phase"`, plan:646 `"plan"`, project:1901 `"project"`, task:1079 `"task"` — each level literal matches the neighboring dispatch-time resolution |
| milestone/phase sites | `spawnReporterIfNeeded` | trailing `skipMessageSpans bool` param → `ReporterOptions.SkipMessageSpans` | ✓ WIRED | `dispatch_helpers.go:109` param, `:132` threaded into internal `ReporterOptions` literal |
| plan/project/task sites | inline `ReporterOptions{}` | `SkipMessageSpans: skipMessageSpans` field | ✓ WIRED | Confirmed at plan:652, project:1907, task:1086 |
| `reporter_jobspec.go` | reporter Job container Args | `append` gated on `opts.SkipMessageSpans` after `--traceparent` | ✓ WIRED | :229-231 |
| `main.go parseFlags` | `reporterConfig` | `fs.Bool` copied into struct literal | ✓ WIRED | `SkipMessageSpans: *skipMessageSpans` at :143 |
| `main.go synthesizeSpans` | skip decision | early-return guard on `cfg.SkipMessageSpans` as first statement | ✓ WIRED | :321-324 precedes all path construction |
| `adapter_seam_test.go` | `pkg/otelai/tracecontext.go` | `otelai.ExtractRemoteParent` env-carrier primitive | ✓ WIRED | :70 |

### Behavioral Spot-Checks

| Behavior | Command | Result | Status |
|----------|---------|--------|--------|
| SelfInstruments polarity (all vendors + unknown + empty → false) | `go test ./pkg/dispatch/... -run TestSelfInstruments -count=1` | ok, 2 tests PASS | ✓ PASS |
| Flag threads to built Job Args in both shapes | `go test ./internal/controller/... -run TestBuildReporterJob_SkipMessageSpansArg -count=1` | ok, PASS | ✓ PASS |
| Parse maps flag to struct; absent → false | `go test ./cmd/tide-reporter/... -run TestParseFlagsSkipMessageSpans -count=1` | present+absent subtests PASS | ✓ PASS |
| Flag set → zero synthesized spans, no sentinel, exit 0 | `go test ./cmd/tide-reporter/... -run TestRunTraceOnly_SkipsSynthesisWhenFlagSet -count=1` | PASS | ✓ PASS |
| D-09 contract: stub + skipped reporter → exactly 1 span | `go test ./cmd/tide-reporter/... -run TestAdapterSeam -count=1` | PASS | ✓ PASS |
| Behavior-unchanged inverse pin still green | `go test ./cmd/tide-reporter/... -run TestRunTraceOnly_EmitsSpans -count=1` | PASS | ✓ PASS |
| Affected packages compile | `go build ./pkg/dispatch/... ./cmd/tide-reporter/... ./internal/controller/... ./internal/reporter/...` | BUILD_OK | ✓ PASS |
| Source-shape gate: no per-vendor branch | `grep -rn "if.*[Vv]endor.*==" internal/controller cmd/tide-reporter internal/reporter` | zero hits (exit 1) | ✓ PASS |

### Requirements Coverage

| Requirement | Source Plan | Description | Status | Evidence |
|-------------|-------------|-------------|--------|----------|
| ADAPT-01 | 45-01, 45-02 | events.jsonl→spans synthesizer is a per-runtime adapter behind the Subagent seam: capability flag travels as data via resolved `Provider.Vendor`, reporter skips synthesis when set, contract test proves no duplicate spans | ✓ SATISFIED | All 3 success criteria VERIFIED above. Declared in both plans' `requirements:` frontmatter; mapped to Phase 45 in REQUIREMENTS.md (lines 32/84). No orphaned requirements — ADAPT-01 is the sole Phase-45 requirement |

### Anti-Patterns Found

| File | Line | Pattern | Severity | Impact |
|------|------|---------|----------|--------|
| — | — | No TBD/FIXME/XXX/HACK/PLACEHOLDER/stub markers in any created or modified file | — | None |

### Behavior-Unchanged Verification (D-10)

- `git diff <phase-base>..HEAD -- cmd/tide-reporter/main_test.go` is **insert-only** (0 removed lines) — `TestRunTraceOnly_EmitsSpans` and all pre-existing reporter tests are textually unmodified.
- `git diff <phase-base>..HEAD -- internal/reporter/tracesynth.go` is **comment-only** — no span-shape, redaction, or size-bound changes; Phase 44 synthesis logic byte-identical.
- No chart/RBAC/go.mod diff: `git diff --stat <phase-base>..HEAD -- charts/ go.mod` is empty.
- Every vendor resolves `false` today → `--skip-message-spans` is never emitted → all Phase 42–44 behavior unchanged.

### Human Verification Required

None. This phase is pure forward-compatibility scaffolding with no UI, external service, real-time, or visual surface. All three success criteria are provable programmatically (contract test runs fully in-process via `runWithClient`; capability lookup and source-shape gates are grep/test verifiable) and were independently executed. The end-to-end LangGraph activation is explicitly deferred until the LangGraph runtime exists (REQUIREMENTS.md line 57) and is out of scope for this phase.

### Gaps Summary

None. All 3 roadmap success criteria are VERIFIED against the codebase:

1. **Flag-as-data** — `SelfInstruments` is a compiled-in data lookup consulted from `ResolveProvider(...).Vendor` at all 5 spawn sites; zero `if vendor ==` branches (gate grep clean).
2. **Reporter skips synthesis, no double-emission** — first-statement guard in the sole `synthesizeSpans` function; `EmitSpans`/`ReconstructConversation` reachable only through it; inverse-pin test proves zero spans against a fixture that otherwise yields 2.
3. **Contract test proves zero duplicates** — `TestAdapterSeam_...` asserts exactly 1 span with a shared exporter, env-carrier extraction only, no LangGraph span-shape assumption.

ADAPT-01 is fully satisfied. The two `45-REVIEW.md` warnings (WR-01 completion-time re-resolution, WR-02 5x-duplicated wiring untestable against regression) are `critical: 0`, explicitly advisory, and forward-looking — both are unexploitable today because `ProviderSpec.Vendor` is hardcoded to `"anthropic"` and every vendor resolves `false`; they surface only when per-vendor selection lands (a future milestone, not this phase). They do not block the Phase 45 goal.

---

_Verified: 2026-07-17T03:04:12Z_
_Verifier: Claude (gsd-verifier)_
