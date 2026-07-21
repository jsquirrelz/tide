---
phase: 50-execution-loop-hardening-loop-native-observability
plan: 04
subsystem: api
tags: [go, dispatch-envelope, terminal-reason, run-evidence, ast-guard, cap-enforcement]

# Dependency graph
requires:
  - phase: 50-execution-loop-hardening-loop-native-observability
    provides: "Plan 50-01's TerminalReason enum, RunEvidence struct, LoopRunID/AttemptID fields on EnvelopeIn/EnvelopeOut (pkg/dispatch)"
provides:
  - "All three real production EnvelopeOut write sites (cmd/claude-subagent, internal/subagent/anthropic, cmd/stub-subagent) set an explicit TerminalReason on every populated literal"
  - "harness.CheckCaps wired into the live anthropic.Run() path — in-pod iteration/token cap violations now produce a real cap_exceeded envelope"
  - "harness.ChangedFileManifest — bounded git --name-status manifest completing RunEvidence at the claude-subagent success path"
  - "internal/subagent/common.PromptTemplateVersion — the compiled-in prompt-version RunEvidence source"
  - "pkg/dispatch/writesite_guard_test.go — an AST-based fail-closed guard enumerating every EnvelopeOut literal across the three write sites"
affects: [50-06-controller-side-cap-synthesis, 51-task-loop]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Base-literal-sets-the-optimistic-default, branches-downgrade — TerminalReason starts at completed on the base EnvelopeOut construction and every failure branch explicitly overwrites it, so there is no code path between construction and return that leaves the field unset"
    - "AST source-level guard for a field Go's type system cannot make mandatory (mirrors pkg/otelai/attrs_test.go's TestKeysUseSemconvModule idiom) — go/parser + ast.Inspect walking CompositeLit/KeyValueExpr, with an inventory-floor sanity check protecting against silent guard-scope shrinkage"
    - "Zero-element composite literals (EnvelopeOut{}) are the deliberate exemption class — dispatch-level error placeholders never written to out.json, wrapped by the caller's own populated (and guarded) literal"

key-files:
  created:
    - pkg/dispatch/writesite_guard_test.go
  modified:
    - internal/subagent/common/prompt_templates.go
    - internal/harness/commit.go
    - internal/harness/commit_test.go
    - internal/subagent/anthropic/subagent.go
    - internal/subagent/anthropic/subagent_test.go
    - cmd/claude-subagent/main.go
    - cmd/claude-subagent/main_test.go
    - cmd/stub-subagent/main.go

key-decisions:
  - "runtimeVersionProbe execs `claude --version` directly via exec.CommandContext, NOT through anthropic.Anthropic's a.execFunc test seam — reusing execFunc would have made the probe's fake output overwrite existing tests' captured-args assertions (e.g. TestRun_PromptViaStdinAndPermissionFlags) whenever the probe ran after the main invocation"
  - "The cap_exceeded test drives harness.CheckCaps via the InputTokens dimension, not Iterations — ParseStream never populates Usage.Iterations for any real Claude Code stream-json transcript (a pre-existing gap outside this plan's file scope), so an Iterations-cap trigger is not reachable through the public Run() path with the current stream-json fixture parser"
  - "failEnvelope/failOut in cmd/claude-subagent/main.go were changed to accept the full EnvelopeIn (not just TaskUID string) so LoopRunID/AttemptID echo naturally at every call site that has a real envelope, and the one call site without one (invalid-envelope, before ReadEnvelopeIn succeeds) passes a zero-value EnvelopeIn — matching its existing TaskUID-omission behavior rather than adding two more positional string params"

requirements-completed: [EXEC-01, EXEC-02, EXEC-03]

# Metrics
duration: 17min
completed: 2026-07-19
---

# Phase 50 Plan 04: Execution-Loop Write-Site Wiring Summary

**TerminalReason set at every populated EnvelopeOut literal across the three real executor write sites (cmd/claude-subagent, internal/subagent/anthropic, cmd/stub-subagent), harness.CheckCaps wired live into anthropic.Run() for in-pod cap_exceeded, RunEvidence completed with a bounded changed-file manifest, and an AST guard enforcing the "never a silent default" invariant going forward.**

## Performance

- **Duration:** 17 min
- **Started:** 2026-07-19T00:36:28-04:00 (first task commit)
- **Completed:** 2026-07-19T00:53:01-04:00 (lint-fix commit)
- **Tasks:** 3
- **Files modified:** 9 (1 created, 8 modified)

## Accomplishments
- `internal/subagent/anthropic/subagent.go`'s `Run()` — the real production executor — now sets `TerminalReason` on its base literal (optimistic `completed`, downgraded by every branch below), echoes `LoopRunID`/`AttemptID` verbatim from `EnvelopeIn`, and assembles the `RunEvidence` core (`Model` echoed from `Provider.Model`, `PromptVersion` from the new compiled-in `common.PromptTemplateVersion`, `RuntimeVersion` from a best-effort `claude --version` probe, `Commands` from the actual claude argv).
- `harness.CheckCaps` (previously orphaned — zero production call sites) is now live inside `anthropic.Run()`: an iteration/input-token/output-token cap violation downgrades the envelope to `TerminalReason: cap_exceeded`, sets `ExitCode: 1`, stamps a `"cap-hit: ..."` `Reason`, and skips child-CRD parsing. This closes the in-pod half of RESEARCH's Open Question 1 (the controller-side wall-clock half is Plan 50-06).
- `harness.ChangedFileManifest(worktreeDir, max)` — a new bounded `git --name-status` reader mirroring `CommitWorktree`'s exec idiom — completes `RunEvidence.ChangedFiles`/`ChangedFileTotal` at `cmd/claude-subagent/main.go`'s success path after a non-empty commit.
- `cmd/claude-subagent/main.go`'s six exit classes (`invalid-envelope`, `worktree-setup-failed`, `subagent-error`, `commit-failed`, `empty-diff`, success) each carry their mapped `TerminalReason`; `failEnvelope`/`failOut` echo `LoopRunID`/`AttemptID` wherever a real `EnvelopeIn` is in scope.
- `cmd/stub-subagent/main.go`'s all 15 populated `EnvelopeOut{...}` literals now set `TerminalReason` per the 8-row mapping table, including `forced-failure -> tool_failure` (RESEARCH A2's deliberately-injected generic-failure bucket) and `output-paths-violation -> blocked`. The two success literals also carry a minimal `RunEvidence` (`RuntimeVersion: "stub"`, `PromptVersion` left empty since the stub renders no templates).
- `pkg/dispatch/writesite_guard_test.go` (new) — an AST-based fail-closed guard: `TestEnvelopeOutWriteSites_AlwaysSetTerminalReason` parses the three real write-site files, walks every `EnvelopeOut{...}` composite literal, and fails any populated literal (≥1 element) that omits a `TerminalReason` key. Zero-element literals (dispatch-level error placeholders never written to `out.json`) are exempt. A ≥15-literal inventory floor protects against the guard's coverage silently shrinking if a write site moves. Verified locally that removing one `TerminalReason:` key trips the guard (then restored).

## Task Commits

1. **Task 1: Evidence sources + anthropic Run() terminal reasons + CheckCaps wiring** - `0237e640` (feat)
2. **Task 2: claude-subagent exit-path mapping + RunEvidence completion** - `b9566e42` (feat)
3. **Task 3: stub-subagent literal sites + AST fail-closed write-site guard** - `92580285` (test)
4. **Lint fix (Rule 1 auto-fix, discovered during `make lint` verification)** - `56aad79e` (fix)

**Plan metadata:** pending (this SUMMARY's own commit)

## Files Created/Modified
- `internal/subagent/common/prompt_templates.go` - `PromptTemplateVersion` compiled-in const with an explicit per-template-family bump-discipline doc comment
- `internal/harness/commit.go` - `ChangedFileManifest(worktreeDir, max)`; uses `strings.SplitSeq` (modernize lint fix)
- `internal/harness/commit_test.go` - `TestChangedFileManifest` (unbounded + `max`-bounded subtests)
- `internal/subagent/anthropic/subagent.go` - `Run()`'s base literal + waitErr/cap/readChildCRDs branches all set `TerminalReason`; `harness.CheckCaps` wired post-usage; new `runtimeVersionProbe` helper
- `internal/subagent/anthropic/subagent_test.go` - `TestRun_TerminalReason_Completed`/`_ToolFailure`/`_CapExceeded`
- `cmd/claude-subagent/main.go` - `failEnvelope`/`failOut` take the full `EnvelopeIn` + a `TerminalReason` param; empty-diff sets `blocked`; new `completeRunEvidenceWithChangedFiles` helper
- `cmd/claude-subagent/main_test.go` - `TestClaudeSubagentMain_TerminalReasonMapping` (5-row table) + `TestClaudeSubagentMain_SuccessPathCompletesRunEvidence`
- `cmd/stub-subagent/main.go` - all 15 `EnvelopeOut{...}` literals mapped; two success literals gain a minimal `RunEvidence`
- `pkg/dispatch/writesite_guard_test.go` - NEW: the AST fail-closed write-site guard

## Decisions Made
- See `key-decisions` in frontmatter above (runtime-probe exec seam choice, cap-test dimension choice, `failEnvelope`/`failOut` signature choice).
- `RunEvidence.Bounded()` is applied at every construction site in this plan (anthropic base literal, claude-subagent's `completeRunEvidenceWithChangedFiles`, both stub-subagent success literals) even where the input is already small — consistent defensive discipline over "only bound when large," matching Plan 50-01's own `.Bounded()`-before-assignment contract.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug/Lint] `strings.SplitSeq` instead of `strings.Split` in `ChangedFileManifest`**
- **Found during:** Task 3's mandatory `make lint` verification pass (after all three tasks committed)
- **Issue:** `golangci-lint`'s `modernize`/`stringsseq` check flagged the `for _, line := range strings.Split(...)` loop added in Task 1 as less efficient than ranging over `strings.SplitSeq` directly
- **Fix:** Changed to `for line := range strings.SplitSeq(...)`
- **Files modified:** `internal/harness/commit.go`
- **Verification:** `make lint` re-run — `0 issues.`; `go test ./internal/harness/... -run TestChangedFileManifest` still green
- **Committed in:** `56aad79e`

---

**Total deviations:** 1 auto-fixed (1 lint/modernize)
**Impact on plan:** Zero behavioral change — a pure loop-idiom substitution caught by the mandatory `make lint` gate. No scope creep.

## Issues Encountered

- `internal/subagent/anthropic`'s `ParseStream` never populates `Usage.Iterations` for any real Claude Code stream-json transcript (confirmed via grep — no write site anywhere in the package sets it). The plan's Task 1 Test 3 literally specified "in.Caps.Iterations lower than actual usage.Iterations," which is structurally unreachable through the public `Run()` API with the current parser. Resolved by driving the same `harness.CheckCaps` wiring through the `InputTokens` dimension instead (the fixture stream reports `input_tokens=100`, so `Caps.InputTokens=5` reliably triggers `CapHitError{Reason:"input-tokens"}`) — this exercises the identical `CheckCaps` call and downgrade logic end-to-end; only the specific cap dimension differs from the plan's literal example. Documented in the test's own doc comment so a future reader doesn't mistake this for an oversight. Populating `Usage.Iterations` is out of this plan's file scope (`stream_parser.go` is not in `files_modified`) and is flagged here for a future plan if an iteration-count cap is needed.

## User Setup Required
None - no external service configuration required.

## Next Phase Readiness
- All three real EnvelopeOut write sites now set `TerminalReason` on every populated literal, proven by the AST guard (`pkg/dispatch/writesite_guard_test.go`) plus per-branch unit tests at each site.
- `cap_exceeded` is reachable in-pod today (iteration/token caps via `harness.CheckCaps`); the wall-clock half (`ActiveDeadlineSeconds`-killed Jobs, which never write `out.json`) remains Plan 50-06's controller-side synthesis, per the plan's explicit scope fence.
- `RunEvidence` is fully assembled on the success path: model, prompt version, runtime version, commands, and a bounded changed-file manifest.
- `grep -rn "harness.Harness{" cmd/ internal/subagent/` confirmed 0 hits — the dead `Harness.Run` orchestrator was not touched, per the RESEARCH correction.
- `go vet ./...` and `make lint` both pass clean (0 issues) as of the final commit.
- Plan 50-06 can now build the controller-side `cap_exceeded` synthesis for `DeadlineExceeded`-killed Jobs on top of a settled, guarded envelope contract — no further changes needed to the three write sites for that half.

---
*Phase: 50-execution-loop-hardening-loop-native-observability*
*Completed: 2026-07-19*

## Self-Check: PASSED

All created files and task commit hashes verified present on disk / in `git log --oneline --all` (see self-check step below).
