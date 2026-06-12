---
phase: 14-budget-enforcement-pricing
fixed_at: 2026-06-12T13:30:25Z
review_path: .planning/phases/14-budget-enforcement-pricing/14-REVIEW.md
iteration: 1
findings_in_scope: 11
fixed: 11
skipped: 0
status: all_fixed
---

# Phase 14: Code Review Fix Report

**Fixed at:** 2026-06-12T13:30:25Z
**Source review:** .planning/phases/14-budget-enforcement-pricing/14-REVIEW.md
**Iteration:** 1

**Summary:**
- Findings in scope: 11 (1 Critical + 10 Warnings; Info findings out of scope per `fix_scope: critical_warning`)
- Fixed: 11
- Skipped: 0

## Fixed Issues

### CR-01: `|| true` swallows the drift script's exit code

**Files modified:** `.github/workflows/pricing-drift.yaml`
**Commit:** 0fba8ca (pre-existing — fixed before this run)
**Applied fix:** Already fixed on `main` prior to this run. Verified present: the step now uses `DRIFT_EXIT=0; ./hack/check-pricing-drift.sh ... || DRIFT_EXIT=$?` (lines 53–54) with an explanatory comment. Not re-applied.

### WR-01: `mktemp` template with non-trailing X's breaks on macOS

**Files modified:** `hack/check-pricing-drift.sh`
**Commit:** d61d3cf
**Applied fix:** Dropped the `.md` suffix so the template ends in X's (`mktemp /tmp/anthropic-pricing-XXXXXX`) and added `trap 'rm -f "${PRICING_TMP}"' EXIT` so interrupted runs never leave a colliding file. Verified on this macOS host: the new template randomizes correctly.

### WR-02: Price extraction assumes first two `$` amounts are (input, output)

**Files modified:** `hack/check-pricing-drift.sh`
**Commit:** ae1d68c
**Applied fix:** Replaced positional extraction with header-anchored column parsing: an awk pass records the input/output/5m-cache-write/cache-read column indexes from the pricing table's header row, then reads exactly those cells from the model's row. The model match pattern now tolerates display names ("Claude Opus 4.8") as well as raw IDs — the live page uses display names, so the old `grep -i claude-opus-4-8` matched nothing at all. Comparison extended to cacheRead/cacheWrite; "mentioned on page but no parseable pricing row" now surfaces as a parse error. Cent conversion uses `printf "%.0f"` (rounds) instead of `%d` (truncates `$0.30*100` to 29) — incidentally resolving IN-04 in the rewritten lines.
**Verification:** Ran the real script (URL swapped to a `file://` fixture of the live Anthropic pricing page): clean page → exit 0; injected output/cache-read drift → exit 1 with correct values; header column order swapped → only genuinely transposed rows flagged, proving anchoring.

### WR-03: Missing-model scan greps every `claude-*` token on the page

**Files modified:** `hack/check-pricing-drift.sh`
**Commit:** 7a3d88c
**Applied fix:** Scoped `LIVE_MODELS` extraction to priced pipe-table rows only (`^|` rows containing `$`), excluding rows marked `deprecated`/`retired`. URL slugs, prose mentions, and code samples no longer generate permanent false drift.
**Verification:** Clean fixture stays exit 0; a new priced table row with an unknown `claude-haiku-9-9` ID is correctly flagged as missing-from-table.

### WR-04: Static heredoc delimiter allows `GITHUB_OUTPUT` injection

**Files modified:** `.github/workflows/pricing-drift.yaml`
**Commit:** 5a6bf1a
**Applied fix:** The `drift_body` heredoc now uses a per-run random delimiter (`DRIFT_EOF_$(head -c16 /dev/urandom | base64 | tr -d '=+/')`) per GitHub's hardening guidance, so fetched page content cannot terminate the heredoc early and inject `key=value` outputs. YAML parse and marker generation verified.

### WR-05: Per-Task `BudgetBlocked` condition is set but never cleared

**Files modified:** `internal/controller/task_controller.go`
**Commit:** cacb0a8
**Applied fix:** Dropped the per-Task condition stamp (review's first option), making this gate consistent with the other four dispatch gates ("operator signal is the single Project condition") and with the anti-status-flapping doctrine. Confirmed before choosing this shape: no test, dashboard, or CLI code consumes the per-Task condition. The park behavior (30s requeue) and V(1) log line are unchanged; `budget-blocked`-labeled envtest regression specs pass.

### WR-06: Reservation settled before spend rolls up

**Files modified:** `internal/controller/task_controller.go`
**Commit:** 93694e1
**Applied fix:** Replaced the early `Settle` call at the top of `handleJobCompletion` with `defer r.Deps.Reservations.Settle(string(task.UID))` — the reservation now drops only after `budget.RollUpUsage` has landed the actual cost (closing the multi-API-call under-count window), while the defer still covers every early-return Failed branch the original early call was protecting. Full `internal/controller` envtest suite passes.
**Status note:** fixed — requires human verification (ordering correctness rests on code reading; no existing spec discriminates settle-after-roll-up timing).

### WR-07: Failure early-returns skip `RollUpUsage`

**Files modified:** `internal/controller/task_controller.go`
**Commit:** 8f55cb3
**Applied fix:** Added non-fatal `budget.RollUpUsage(ctx, r.Client, project, out.Usage)` calls (same pattern as the terminal-path roll-up) in the `OutputValidationError` and `OutputPathsViolation` branches, placed after the successful terminal status patch to avoid double-counting on patch-failure replays. `EnvelopeReadFailed` left as-is per the review (no usage to roll up). Full `internal/controller` envtest suite passes.
**Status note:** fixed — requires human verification (no new spec asserts roll-up in these branches).

### WR-08: `HasHeadroom` ignores `RollingWindowCapCents`

**Files modified:** `internal/budget/reservation.go`, `internal/budget/reservation_test.go`
**Commit:** 1d0f344
**Applied fix:** `HasHeadroom` now computes the effective cap as the tightest configured cap — `min(AbsoluteCapCents, RollingWindowCapCents)` treating `<= 0` as unset — matching the pair of caps `IsCapExceeded` enforces. Added four table-test cases (rolling tighter, rolling-only, absolute-still-binds, under-both); `internal/budget` tests pass.

### WR-09: `RollUpUsage` read-modify-write loses concurrent spend

**Files modified:** `internal/budget/tally.go`, `internal/budget/tally_test.go`
**Commit:** 3db8eea
**Applied fix:** `RollUpUsage` now wraps the increment in `retry.RetryOnConflict`: re-fetch the Project, patch with `client.MergeFromWithOptimisticLock{}`, and re-run on conflict so concurrent completions serialize instead of clobbering. The caller's in-memory `project.Status.Budget` is updated to the rolled-up state (the TaskReconciler feeds the same struct to `setBudgetBlockedIfNeeded` immediately after). Added `TestRollUpUsage_RetriesOnConflict` using an interceptor that injects a concurrent roll-up before the first patch — the test fails (150 ≠ 220 tokens) without the optimistic lock, passes with it. All `internal/budget` + `internal/controller` tests pass.

### WR-10: Release checklist step 4 is impossible to satisfy

**Files modified:** `docs/releasing.md`
**Commit:** 4f3e8bd
**Applied fix:** Replaced the always-failing `grep -c 'stub' charts/tide/values.yaml` with `helm template charts/tide | grep -cE '(image:|value:).*tide-stub-subagent'` (must print 0). Note: the review's literal suggestion (`grep -c 'tide-stub-subagent'` on the rendered output) would also false-fail — the rendered template carries a stub opt-in *comment*; the check was scoped to `image:`/`value:` fields. Verified the documented command prints `0` on the current chart.

## Observations (out of scope, not fixed)

- **The drift script's hardcoded `PRICING_URL` (`https://platform.claude.com/docs/en/pricing.md`) returned HTTP 404 during verification** (tested 2026-06-12 from this host; `https://docs.anthropic.com/en/docs/about-claude/pricing.md` works and was used as the test fixture). The script correctly exits 2 (fetch failure, no issue filed) — but that means the weekly D-03 automation silently reports nothing until the URL is corrected. Not a REVIEW.md finding; flagged for follow-up.
- Info findings (IN-01..IN-04) were out of scope per `fix_scope: critical_warning`. IN-04 (truncating cent conversion) was incidentally resolved inside the WR-02 rewrite; IN-01 (dead `DRIFT_BODY` variable) remains.

---

_Fixed: 2026-06-12T13:30:25Z_
_Fixer: Claude (gsd-code-fixer)_
_Iteration: 1_
