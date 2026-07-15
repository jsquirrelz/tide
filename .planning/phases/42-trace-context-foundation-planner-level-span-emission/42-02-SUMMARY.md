---
phase: 42-trace-context-foundation-planner-level-span-emission
plan: 02
subsystem: observability
tags: [opentelemetry, otel, tracing, w3c-traceparent, propagation, pkg-otelai]

# Dependency graph
requires: []
provides:
  - "pkg/otelai/tracecontext.go: TraceIDFromUID, FormatTraceparent, ExtractRemoteParent — the Phase 43 propagation seam"
  - "Deterministic-value, round-trip, malformed-input, and K8s-import purity guard tests"
affects: [43-trace-context-propagation-task-parity, propagation, dispatch-span-emission]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "trace.TraceIDFromHex on a stripped/lowercased K8s UID for deterministic TraceID derivation (no custom IDGenerator, no UUID-parsing dependency)"
    - "propagation.TraceContext{}.Inject/Extract on propagation.MapCarrier for all W3C traceparent formatting/parsing — never hand-rolled string formatting"
    - "Source-grep purity guard test (reusing attrs_test.go's findRepoRoot) to enforce a package's zero-K8s-import architectural constraint at test time"

key-files:
  created:
    - pkg/otelai/tracecontext.go
    - pkg/otelai/tracecontext_test.go
  modified: []

key-decisions:
  - "Option A confirmed (from plan): Phase 42 planner spans stay fully independent SDK-random-TraceID roots; these primitives are unit-proven here with zero production call sites — Phase 43 is the consumer."
  - "TraceIDFromUID takes a plain string, not k8s.io/apimachinery types.UID, to keep pkg/otelai free of Kubernetes imports; callers pass string(project.UID)."

patterns-established:
  - "Pattern 2 from ARCHITECTURE.md (deterministic TraceID + explicitly-threaded parent SpanID) is now a concrete, tested primitive at pkg/otelai/tracecontext.go."

requirements-completed: [ATTR-03]

# Metrics
duration: 25min
completed: 2026-07-15
---

# Phase 42 Plan 02: Trace-Context Foundation Primitives Summary

**Pure `pkg/otelai/tracecontext.go` implementing deterministic TraceID-from-UID derivation, W3C traceparent formatting, and remote-parent extraction — all via the `go.opentelemetry.io/otel/{trace,propagation}` API, zero hand-rolled formatting, zero Kubernetes imports.**

## Performance

- **Duration:** ~25 min
- **Started:** 2026-07-15T20:38:00Z
- **Completed:** 2026-07-15T20:52:00Z
- **Tasks:** 2 completed
- **Files modified:** 2 (both new)

## Accomplishments

- `TraceIDFromUID(uid string) (trace.TraceID, error)` — strips dashes, lowercases, and hands the 32-hex-char result to `trace.TraceIDFromHex`, which enforces length/hex-validity and rejects the invalid all-zero ID. Deterministic (same UID → same TraceID every call) and case-insensitive.
- `FormatTraceparent(traceID, spanID, sampled) string` — builds a `trace.SpanContext` and renders the exact W3C `traceparent` header via `propagation.TraceContext{}.Inject` on a `MapCarrier`; returns `""` for an invalid (zero-value) ID pair since `Inject` no-ops on an invalid `SpanContext`.
- `ExtractRemoteParent(ctx, traceparent) context.Context` — thin wrapper over `propagation.TraceContext{}.Extract`; never panics on malformed input, callers check `trace.SpanContextFromContext(ctx).IsValid()`.
- Full test coverage: determinism, case-insensitivity, invalid-UID rejection (empty/malformed/all-zero), exact-string traceparent formatting (sampled and unsampled), round-trip Format→Extract, four classes of malformed-input no-panic cases, and a K8s-import purity guard verified live (temporarily injected a `k8s.io/apimachinery` import, confirmed the guard test fails, reverted cleanly).

## Task Commits

Each task was committed atomically (TDD RED→GREEN for Task 1; guard test as its own commit for Task 2):

1. **Task 1 RED: failing tests for trace-context primitives** — `1378b3a` (test)
2. **Task 1 GREEN: implement tracecontext.go** — `4c4ebad` (feat)
3. **Task 2: K8s-import purity guard test** — `ea47a95` (test)

**Plan metadata:** (this commit, docs: complete plan)

## Files Created/Modified

- `pkg/otelai/tracecontext.go` — the three primitives (96 lines), pure stdlib + `go.opentelemetry.io/otel/{trace,propagation}` only.
- `pkg/otelai/tracecontext_test.go` — 6 test functions (197 lines): `TestTraceContextTraceIDFromUID`, `TestTraceContextTraceIDFromUIDInvalid`, `TestTraceContextFormatTraceparent`, `TestTraceContextExtractRemoteParent`, `TestTraceContextExtractMalformedNoPanic`, `TestTraceContextNoK8sImports`.

## Decisions Made

- Followed the plan's mid-milestone trace-shape decision (Option A) as directed — no production call sites added in this plan; Phase 43 is the documented consumer.
- Reworded two doc-comment passages in `tracecontext.go` (originally written mentioning literal `k8s.io/`, `sigs.k8s.io/`, and `fmt.Sprintf` substrings in prose) to avoid tripping the plan's own literal-grep acceptance criteria (`grep -c 'fmt.Sprintf'` must be 0; `TestTraceContextNoK8sImports`/`grep -n 'k8s.io'` must find nothing). The functional doc-comment content and citations are unchanged, just phrased to not contain the literal forbidden substrings (e.g. "never a hand-rolled format string" instead of naming `fmt.Sprintf`; "the Kubernetes apimachinery UID type" instead of `k8s.io/apimachinery`).

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] Doc comments in tracecontext.go tripped the plan's own literal-grep verification checks**
- **Found during:** Task 1 verification (`grep -c 'fmt.Sprintf' pkg/otelai/tracecontext.go` returned 1; then Task 2's `TestTraceContextNoK8sImports` failed against the initial doc comments that named `k8s.io/apimachinery`)
- **Issue:** Explanatory prose in the doc comments ("never hand-rolled fmt.Sprintf", "NOT k8s.io/apimachinery types.UID") used the exact literal substrings the plan's acceptance-criteria greps and Task 2's purity-guard test are designed to catch.
- **Fix:** Reworded both comments to preserve the same meaning without the literal trigger substrings (see Decisions Made above).
- **Files modified:** `pkg/otelai/tracecontext.go`
- **Verification:** `grep -c 'fmt.Sprintf' pkg/otelai/tracecontext.go` → 0; `grep -n 'k8s.io' pkg/otelai/tracecontext.go` → no matches; `TestTraceContextNoK8sImports` passes.
- **Committed in:** `4c4ebad` (Task 1 GREEN commit)

**2. [Process pragmatism] Task 2's guard test was authored alongside Task 1's RED tests, then re-split to match the plan's task boundary**
- **Found during:** Task 1 RED commit — all 6 tests (5 from Task 1's behavior block + Task 2's `TestTraceContextNoK8sImports`) were written together in one pass, since they share the same test file.
- **Issue:** Committing all 6 tests in the Task 1 RED commit would blur the plan's explicit two-task structure (Task 1 = tdd, Task 2 = separate purity-guard task with its own commit).
- **Fix:** Removed `TestTraceContextNoK8sImports` from the Task 1 RED/GREEN commits and re-added it as Task 2's own commit (`ea47a95`), matching the plan's task-level commit granularity.
- **Files modified:** `pkg/otelai/tracecontext_test.go`
- **Verification:** `git log --oneline` shows 3 commits matching the plan's 2-task structure (RED, GREEN, Task-2-guard); full package `go test ./pkg/otelai/...` green after each commit.
- **Committed in:** `1378b3a`, `4c4ebad`, `ea47a95`

---

**Total deviations:** 2 auto-fixed (1 bug — self-tripped grep checks; 1 process pragmatism — commit re-splitting)
**Impact on plan:** No scope creep. Both fixes keep the plan's own acceptance criteria and task-commit structure intact; no functional behavior changed beyond the doc-comment wording.

## Issues Encountered

- `go build ./...` (repo-wide, cited in the plan's overall `<verification>` block) fails on an unrelated pre-existing issue: `cmd/tide-demo-init/main.go`'s `//go:embed all:fixture` has no matching files in this worktree checkout. Zero commits in this plan touch `cmd/tide-demo-init/`; confirmed out of scope via `git diff --stat` on this plan's 3 commits (only `pkg/otelai/tracecontext.go` and `pkg/otelai/tracecontext_test.go` touched). Logged to `.planning/phases/42-trace-context-foundation-planner-level-span-emission/deferred-items.md` per the scope-boundary rule; not fixed. This plan's actual package builds and tests clean: `go build ./pkg/otelai/...` and `go test ./pkg/otelai/...` both pass.

## User Setup Required

None - no external service configuration required.

## Next Phase Readiness

- `pkg/otelai/tracecontext.go` is ready for Phase 43 to wire into Job env / reporter args (`TRACEPARENT` injection at dispatch, `.status.trace.spanID` persistence, child-dispatch parent-lookup) per ARCHITECTURE.md Pattern 2's suggested build order (this file is step 1 of that order — "Zero K8s dependencies, fully unit-testable in isolation, and every other step depends on it").
- No blockers. `go.mod`/`go.sum` untouched as required by the wave-1 constraint (sibling plan 42-01 owns those files) — verified via `git diff --stat` against the wave's shared base commit.
- Pre-existing, unrelated `cmd/tide-demo-init` embed-fixture build failure carried forward in `deferred-items.md` for whichever future plan/verifier touches that package.

---
*Phase: 42-trace-context-foundation-planner-level-span-emission*
*Completed: 2026-07-15*

## Self-Check: PASSED

- FOUND: pkg/otelai/tracecontext.go
- FOUND: pkg/otelai/tracecontext_test.go
- FOUND: .planning/phases/42-trace-context-foundation-planner-level-span-emission/42-02-SUMMARY.md
- FOUND: commit 1378b3a (test RED)
- FOUND: commit 4c4ebad (feat GREEN)
- FOUND: commit ea47a95 (test purity guard)
