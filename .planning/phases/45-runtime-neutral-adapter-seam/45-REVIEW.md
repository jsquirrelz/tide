---
phase: 45-runtime-neutral-adapter-seam
reviewed: 2026-07-17T02:57:34Z
depth: standard
files_reviewed: 14
files_reviewed_list:
  - cmd/tide-reporter/adapter_seam_test.go
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
  - internal/reporter/tracesynth.go
  - pkg/dispatch/vendor_capabilities.go
  - pkg/dispatch/vendor_capabilities_test.go
findings:
  critical: 0
  warning: 2
  info: 0
  total: 2
status: issues_found
---

# Phase 45: Code Review Report

**Reviewed:** 2026-07-17T02:57:34Z
**Depth:** standard
**Files Reviewed:** 14
**Status:** issues_found

## Summary

Phase 45 wraps the Phase-44 events.jsonl synthesizer behind a runtime-neutral
adapter seam: `pkg/dispatch.SelfInstruments(vendor)` (fail-closed capability
lookup), a `ReporterOptions.SkipMessageSpans` → `--skip-message-spans`
bareword Arg threaded through `BuildReporterJob`, and a first-statement skip
guard in `cmd/tide-reporter/main.go`'s `synthesizeSpans`. I traced every new
call site (milestone/phase/plan/project/task controllers →
`dispatch_helpers.go`/`reporter_jobspec.go` → `cmd/tide-reporter/main.go`) and
confirmed:

- `SelfInstruments` is genuinely fail-closed (unknown vendor, empty string,
  and every currently-canonical vendor all return `false`).
- The skip decision is manager-authored only — it is threaded from
  `ResolveProvider(...).Vendor` through `BuildReporterJob`'s `Args` slice; it
  never reads pod-writable PVC state (events.jsonl/out.json/in.json). The
  trust-boundary requirement holds.
- `synthesizeSpans` checks `cfg.SkipMessageSpans` as its literal first
  statement, before the sentinel-file check, so a skipped run never touches
  `.spans-emitted` — matches the `adapter_seam_test.go` and
  `TestRunTraceOnly_SkipsSynthesisWhenFlagSet` assertions.
- The `--skip-message-spans` Arg composes uniformly across both the
  materialization and trace-only `BuildReporterJob` shapes, and is correctly
  omitted (not `=false`) when unset, preserving D-03 default-safe absence
  semantics.
- All 5 completion-handler call sites (`milestone_controller.go:639`,
  `phase_controller.go:592`, `plan_controller.go:646`,
  `project_controller.go:1901`, `task_controller.go:1079`) pass the same
  level-string literal to `ResolveProvider` that the corresponding
  dispatch-time call in the same reconciler uses, so there is no
  copy-paste level mismatch today.
- `go build ./...`, `go vet` on the touched packages, `gofmt -l`, the full
  `go test` run for the 4 touched packages, and `./bin/golangci-lint run` on
  the touched packages are all clean (0 issues, all green).

Two forward-looking WARNINGs came out of tracing the new completion-time
`ResolveProvider` call against its dispatch-time counterpart and against test
coverage — both are currently unexploitable only because `ProviderSpec.Vendor`
is hardcoded to `"anthropic"` everywhere, and both will bite the moment
per-vendor selection (already flagged in-repo as a "Deferred Idea") lands.

## Warnings

### WR-01: Completion-time `ResolveProvider` re-resolves against the *live* Project object instead of the dispatch-time-resolved vendor

**File:** `internal/controller/milestone_controller.go:639` (same pattern at `phase_controller.go:592`, `plan_controller.go:646`, `project_controller.go:1901`, `task_controller.go:1079`)

**Issue:** Every one of the 5 new call sites computes `skipMessageSpans` by
calling `ResolveProvider(project, "<level>", r.Deps.HelmProviderDefaults)`
against the reconciler's freshly-fetched (i.e., *current*) `Project` object at
Job-*completion* time — not the `Project` snapshot that was live when the Job
was originally *dispatched*. `BuildPlannerEnvelope`/`resolveImage` already
call the equivalent resolution at dispatch time and stamp the result into
`EnvelopeIn.Provider`, but that resolved value is only logged
(`milestone_controller.go:445`) — it is never persisted to CRD `.status` or
otherwise carried forward, so the completion handler has no way to recover
"what vendor actually produced this events.jsonl." It re-derives the answer
from whatever `Project.Spec.Subagent` currently says.

Today this is inert: `ResolveProvider` hardcodes `Vendor: "anthropic"`
unconditionally (`dispatch_helpers.go:325`), so no operator edit to the
`Project` CRD between dispatch and completion can change the outcome. But the
moment per-vendor selection ships (explicitly called out in this same file as
deferred — "per-vendor selection deferred -- CONTEXT.md 'Deferred Ideas'"), an
operator who edits `Project.Spec.Subagent.Levels.<X>.Vendor` while a dispatch
is in flight will cause the reporter's skip decision to reflect the *new*
vendor, not the one that actually ran. Per this phase's own design principle
("always fail toward visibility, never toward silence" — `vendor_capabilities.go:26-30`),
the dangerous direction is: dispatch ran on a non-self-instrumenting vendor
(→ needs synthesis), operator edits the CRD to a self-instrumenting vendor
before completion, and the reporter now silently skips synthesis for a run
whose events.jsonl was never self-instrumented — the exact silent-span-loss
failure mode this phase set out to prevent.

**Fix:** Persist the dispatch-time-resolved `ProviderSpec` (or at minimum
`.Vendor`) on the CRD `.status` alongside the existing trace-span-ID fields,
and have the completion handler read that stamped value instead of
re-calling `ResolveProvider` against the live `Project`:

```go
// at dispatch time (e.g. milestone_controller.go, alongside envIn construction)
ms.Status.MilestoneDispatchVendor = envIn.Provider.Vendor

// at completion time (milestone_controller.go:639)
skipMessageSpans := pkgdispatch.SelfInstruments(ms.Status.MilestoneDispatchVendor)
```

If a status field is too heavy for this phase, at minimum leave a `// TODO`
at each of the 5 call sites pointing at this gap so it is not silently
inherited by the vendor-selection phase.

### WR-02: SkipMessageSpans controller wiring is duplicated 5x and structurally untestable against a wiring regression

**File:** `internal/controller/milestone_controller.go:639`, `phase_controller.go:592`, `plan_controller.go:646`, `project_controller.go:1901`, `task_controller.go:1079`

**Issue:** The one-line expression
`pkgdispatch.SelfInstruments(ResolveProvider(project, "<level>", r.Deps.HelmProviderDefaults).Vendor)`
is duplicated verbatim (modulo the level-string literal) across all 5
completion handlers. There is no dedicated unit test for `spawnReporterIfNeeded`
(grep confirms zero direct test call sites — it is only exercised indirectly
via full-reconcile tests that set `ReporterImage=""` and short-circuit before
reaching this line) and no controller-level test asserts the *forwarded*
`skipMessageSpans` value on the constructed Job's Args.

Because `SelfInstruments` returns `false` for every input today, this is a
coverage blind spot with zero ability to distinguish correct wiring from a
broken one: a copy-paste mistake at any of the 5 sites (wrong level string,
e.g. `"plan"` accidentally passed in `phase_controller.go`, or the
`skipMessageSpans` positional argument silently dropped in a future edit to
`spawnReporterIfNeeded`'s call in `milestone_controller.go`/`phase_controller.go`)
would compile clean, pass every existing test, and only surface once a
self-instrumenting vendor ships — at which point it reproduces exactly the
duplicate-span or silent-span-loss failure modes this phase exists to
prevent, with no test signal pointing at the regression's origin.

**Fix:** Extract the duplicated one-liner into a single shared helper (this
also directly enables WR-01's fix by giving it one call site to update) and
unit-test the level→override-key mapping directly, independent of
`SelfInstruments`'s current always-false output:

```go
// dispatch_helpers.go
func skipMessageSpansFor(project *tideprojectv1alpha3.Project, level string, defaults ProviderDefaults) bool {
    return pkgdispatch.SelfInstruments(ResolveProvider(project, level, defaults).Vendor)
}
```

```go
// dispatch_helpers_test.go — proves the level string reaching ResolveProvider
// is the one the caller intended, independent of SelfInstruments's current
// all-false behavior.
func TestSkipMessageSpansFor_UsesLevelOverrideKey(t *testing.T) {
    // stub ResolveProvider indirectly via a Project whose Levels.Phase.Model
    // is set, asserting level="milestone" resolves through Levels.Phase per
    // levelOverrideKey — a wiring/level-mismatch regression would flip which
    // LevelConfig is consulted and this test would catch it even though
    // SelfInstruments itself still returns false.
}
```

---

_Reviewed: 2026-07-17T02:57:34Z_
_Reviewer: Claude (gsd-code-reviewer)_
_Depth: standard_
