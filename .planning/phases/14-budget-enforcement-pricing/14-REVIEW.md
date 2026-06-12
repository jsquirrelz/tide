---
phase: 14-budget-enforcement-pricing
reviewed: 2026-06-12T00:53:29Z
depth: standard
files_reviewed: 30
files_reviewed_list:
  - .github/workflows/pricing-drift.yaml
  - api/v1alpha1/shared_types.go
  - charts/tide/templates/deployment.yaml
  - charts/tide/values.yaml
  - cmd/claude-subagent/commit_test.go
  - cmd/claude-subagent/main_test.go
  - cmd/claude-subagent/main.go
  - cmd/manager/main.go
  - docs/releasing.md
  - hack/check-pricing-drift.sh
  - internal/budget/reservation_test.go
  - internal/budget/reservation.go
  - internal/controller/budget_blocked_regression_test.go
  - internal/controller/budget_blocked_test.go
  - internal/controller/budget_blocked.go
  - internal/controller/milestone_controller.go
  - internal/controller/phase_controller.go
  - internal/controller/plan_controller.go
  - internal/controller/project_controller.go
  - internal/controller/task_controller.go
  - internal/dispatch/podjob/backend.go
  - internal/dispatch/podjob/jobspec_test.go
  - internal/dispatch/podjob/jobspec.go
  - internal/subagent/anthropic/pricing_test.go
  - internal/subagent/anthropic/pricing.go
  - internal/subagent/anthropic/subagent.go
  - pkg/dispatch/pricing_test.go
  - pkg/dispatch/pricing.go
  - test/integration/kind/projects_pvc_test.go
findings:
  critical: 1
  warning: 10
  info: 4
  total: 15
status: issues_found
---

# Phase 14: Code Review Report

**Reviewed:** 2026-06-12T00:53:29Z
**Depth:** standard
**Files Reviewed:** 30
**Status:** issues_found

## Summary

Phase 14's core mechanics are sound: the corrected price table is internally consistent (cache rates follow the 1.25x/0.10x rule, conservative fallback re-points to the new most-expensive tier), the provider firewall holds (only `internal/subagent/anthropic/` interprets prices; `pkg/dispatch.ParsePricingOverrides` validates without pricing knowledge), the ReservationStore is goroutine-safe and never persisted to CRD status (PERSIST-02), all five dispatch sites carry the `checkBudgetBlocked` hold after `checkBillingHalt`, and the bidirectional `setBudgetBlockedIfNeeded` clear path is implemented and tested.

However, the D-03 pricing-drift automation is dead on arrival: a shell quirk in the workflow guarantees the drift-issue step can never fire (CR-01). The drift shell script has three independent fragilities that make it either silently wrong or noisy (WR-01..WR-03, WR-04). On the controller side, the budget-accuracy goal is undermined by a settle-before-rollup ordering race (WR-06), failure branches that drop real spend (WR-07), a reservation gate that ignores the rolling-window cap (WR-08), and a non-atomic spend roll-up (WR-09). One operator-visible condition is set and never cleared (WR-05).

## Critical Issues

### CR-01: `|| true` swallows the drift script's exit code — the drift issue can never be opened

**File:** `.github/workflows/pricing-drift.yaml:52-53`
**Issue:** The step captures the exit code *after* forcing success:

```bash
./hack/check-pricing-drift.sh > /tmp/drift-output.txt 2>&1 || true
DRIFT_EXIT=$?
```

After `cmd || true`, `$?` is the exit status of `true` — always `0` (verified: `bash -c 'false || true; echo $?'` prints `0`). `DRIFT_EXIT` is therefore `0` on every run regardless of drift, `exit_code` output is always `'0'`, the `if: steps.drift.outputs.exit_code == '1'` condition on the issue step never matches, and `exit ${DRIFT_EXIT}` always exits 0 (making the `continue-on-error` + outcome commentary moot). The entire weekly D-03 automation silently reports nothing, forever — exactly the "stale pricing table" failure class this phase exists to close.
**Fix:**
```bash
DRIFT_EXIT=0
./hack/check-pricing-drift.sh > /tmp/drift-output.txt 2>&1 || DRIFT_EXIT=$?
echo "exit_code=${DRIFT_EXIT}" >> "$GITHUB_OUTPUT"
```
(The `|| DRIFT_EXIT=$?` form also keeps the step alive under the default `bash -e` shell.) Add a workflow-level test or at minimum manually trigger `workflow_dispatch` with a known-drifted table to verify the issue opens.

## Warnings

### WR-01: `mktemp` template with non-trailing X's breaks on macOS (documented local-run platform)

**File:** `hack/check-pricing-drift.sh:28`
**Issue:** `mktemp /tmp/anthropic-pricing-XXXXXX.md` only randomizes trailing X's on BSD/macOS `mktemp`. Verified on this host: it creates the *literal* file `/tmp/anthropic-pricing-XXXXXX.md` (predictable path, no randomization — temp-file squatting/symlink exposure on shared `/tmp`), and a subsequent run after any interrupted prior run fails with `mkstemp failed ... File exists`, which under `set -e` exits non-zero — misread as "drift detected" per the exit-code contract in `docs/releasing.md` step 3. GNU mktemp on CI happens to imply `--suffix`, so the bug only bites the documented local pre-tag flow.
**Fix:** Drop the suffix: `PRICING_TMP="$(mktemp /tmp/anthropic-pricing-XXXXXX)"` or use `mktemp -t anthropic-pricing`. Also add a `trap 'rm -f "${PRICING_TMP}"' EXIT` so interrupted runs don't leave the file behind.

### WR-02: Price extraction assumes the first two `$` amounts on the row are (input, output)

**File:** `hack/check-pricing-drift.sh:101-102`
**Issue:** `head -1` / `sed -n '2p'` over all `$N` matches assumes column order `input, output, ...`. Anthropic pricing tables have historically ordered columns as input / cache-write / cache-read / output (and `grep -i "${MODEL_PAT}" | head -1` may land on a prose mention rather than the table row). If the live page uses any other column order, "output" silently compares against the cache-write price, producing a *wrong* drift report — and a human following D-03 would "correct" the compiled table to a wrong value. The script also never compares the cache dimensions even though the compiled table carries them.
**Fix:** Anchor extraction on column headers (parse the table header row to find the input/output column indexes) or at minimum extract the *last* dollar amount for output if the page layout is input-first/output-last; emit a parse error when more than two price columns are present and unrecognized. Extend comparison to cacheRead/cacheWrite.

### WR-03: Missing-model scan greps every `claude-*` token on the page — permanent false-drift noise

**File:** `hack/check-pricing-drift.sh:151-164`
**Issue:** Step 4 extracts `grep -oE 'claude-[a-z0-9-]+'` across the entire fetched page and flags any digit-containing ID not in the compiled table. The pricing page lists legacy/deprecated model pricing (e.g. older claude-3-x families), code samples, and URLs — every one of those becomes two permanent `DRIFT: ... missing from compiled table` lines on every weekly run. Because the workflow updates a single deduped issue, the issue can never be resolved-and-stay-closed, training operators to ignore it (cry-wolf failure of D-03).
**Fix:** Scope the missing-model scan to the current-models pricing table section (anchor on the section heading or table boundaries), or maintain an explicit ignore-list of known-legacy model IDs in the script.

### WR-04: Static heredoc delimiter allows `GITHUB_OUTPUT` injection from external page content

**File:** `.github/workflows/pricing-drift.yaml:58-62`
**Issue:** The drift body is fetched external content, yet it is written to `GITHUB_OUTPUT` between fixed `DRIFT_EOF` markers. A page (or MITM'd response) containing a line that is exactly `DRIFT_EOF` terminates the heredoc early and the remaining lines are parsed as new `key=value` outputs — e.g. a later `exit_code=1` line overrides the earlier output (last write wins), forcing the issue step to run with attacker-shaped body. T-14-09 explicitly treats this content as data; the transport doesn't.
**Fix:** Use a random delimiter per GitHub's hardening guidance:
```bash
EOF_MARKER="DRIFT_EOF_$(head -c16 /dev/urandom | base64 | tr -d '=+/')"
{ echo "drift_body<<${EOF_MARKER}"; cat /tmp/drift-output.txt; echo "${EOF_MARKER}"; } >> "$GITHUB_OUTPUT"
```

### WR-05: Per-Task `BudgetBlocked` condition is set but never cleared

**File:** `internal/controller/task_controller.go:381-397`
**Issue:** When the budget gate parks a Task it stamps `ConditionBudgetBlocked=True` on the *Task* status. No code path ever flips this condition to False: after the operator raises the cap, the Project condition clears (bidirectional helper), the Task dispatches, runs, and reaches `Succeeded` — still permanently carrying `BudgetBlocked=True` in `kubectl get task -o yaml`. The other four dispatch sites deliberately write *no* per-level condition ("operator signal is the single Project condition") — this site is the inconsistent one, and the stale condition is operator-visible misinformation.
**Fix:** Either drop the per-Task condition stamp for consistency with the other four gates, or clear it (`Status=False, Reason=BudgetCapCleared`) on the first reconcile that passes the budget gate (e.g. just before `checkReadinessGates` returns the pass-through result).

### WR-06: Reservation settled before spend rolls up — headroom under-counts during the window

**File:** `internal/controller/task_controller.go:817` (settle) vs `:922` (roll-up)
**Issue:** `handleJobCompletion` calls `Reservations.Settle(taskUID)` as its first statement, but `budget.RollUpUsage` (which lands the actual cost into `CostSpentCents`) runs ~100 lines later, after the envelope read, output-path validation, and two status patches — each a real API round-trip. In that window `TotalReserved()` has dropped by the estimate while `CostSpentCents` hasn't risen, so a concurrent Task reconcile's `HasHeadroom` (and `setBudgetBlockedIfNeeded`) sees artificially low committed spend and can dispatch past the cap. This is a *wider* overshoot than the documented T-14-06 bound of "one estimate per concurrent reconcile," because the window spans multiple API calls and every in-flight completion contributes.
**Fix:** Move `Settle` to immediately *after* the `RollUpUsage` call (and call it explicitly in each early-return failure branch, or via `defer` guarded to run after roll-up state is decided). The comment's rationale ("avoids missing the early-return Failed branches") is better served by a `defer r.Deps.Reservations.Settle(string(task.UID))` placed after roll-up ordering is fixed.

### WR-07: Failure early-returns skip `RollUpUsage` — real spend silently dropped from the cap

**File:** `internal/controller/task_controller.go:820-877`
**Issue:** Three terminal-Failed branches return before the roll-up at line 922: `EnvelopeReadFailed`, `OutputValidationError`, and `OutputPathsViolation`. For the latter two the envelope *was* read successfully and `out.Usage` carries real token spend — a session that burned, say, $3 of opus tokens and then touched an undeclared path contributes $0 to `CostSpentCents`. Combined with WR-06 the cap systematically under-counts on failure-heavy runs, which is precisely the run-1 regression class this phase closes. (The `ExitCode != 0` path correctly falls through to roll-up; these three do not.)
**Fix:** In the `OutputPathsViolation` and `OutputValidationError` branches, call `budget.RollUpUsage(ctx, r.Client, project, out.Usage)` (non-fatal, same pattern as line 922) before returning. `EnvelopeReadFailed` has no usage to roll up — acceptable.

### WR-08: `HasHeadroom` ignores `RollingWindowCapCents` — reservation gate doesn't bound the rolling cap

**File:** `internal/budget/reservation.go:118-131`
**Issue:** `HasHeadroom` reads only `Spec.Budget.AbsoluteCapCents`, but `budget.IsCapExceeded` (cap.go:44-57) enforces *both* the absolute and rolling-window caps, and `charts/tide/values.yaml` ships `rollingWindowCapCents: 5000` as a default. A wide wave can therefore commit unbounded in-flight estimates against the rolling window: every task passes `HasHeadroom` (absolute headroom plentiful), dispatch proceeds, and the rolling cap only trips after roll-up — reproducing the run-1 wave-wide overshoot for the rolling dimension that D-05 was built to bound.
**Fix:** Compute headroom against the effective cap: `cap := min(positive(AbsoluteCapCents), positive(RollingWindowCapCents))` (treating <=0 as unset), or check both independently and require headroom under each configured cap.

### WR-09: `RollUpUsage` read-modify-write without optimistic lock — concurrent completions lose spend

**File:** `internal/controller/task_controller.go:922` → `internal/budget/tally.go:45-63` (cross-file)
**Issue:** `RollUpUsage` computes `CostSpentCents += usage.EstimatedCostCents` on a Project read from the informer cache and patches with plain `client.MergeFrom` (no `WithOptimisticLock`), writing an *absolute* value. With Task `MaxConcurrentReconciles: 16` (values.yaml), two tasks completing near-simultaneously both read base `N`, write `N+a` and `N+b`; last write wins and one task's cost vanishes from the tally permanently. The helper pre-dates Phase 14, but this phase ships budget *enforcement* on top of it — an under-counted `CostSpentCents` directly defeats both `IsCapExceeded` and `HasHeadroom`.
**Fix:** Use `client.MergeFromWithOptions(project.DeepCopy(), client.MergeFromWithOptimisticLock{})` and retry on conflict (e.g. `retry.RetryOnConflict` re-fetching the Project), so concurrent increments serialize instead of clobbering.

### WR-10: Release checklist step 4 is impossible to satisfy as written

**File:** `docs/releasing.md:35-37`
**Issue:** "`grep -c 'stub' charts/tide/values.yaml` returns 0 for production image fields" — the file *by design* contains the `images.stubSubagent:` key block plus multiple "stub" mentions in comments (values.yaml:144-160, 211-215), so the grep returns a large non-zero count on a perfectly correct chart. An operator following the checklist either fails the gate on every release or learns to skip checklist items — both bad outcomes for a pre-tag document.
**Fix:** Replace with a check that matches the actual contract, e.g. `helm template charts/tide | grep -c 'tide-stub-subagent'` returns 0 (the rendered default must not reference the stub image), or `grep -c 'image: ghcr.io/jsquirrelz/tide-stub-subagent' charts/tide/values.yaml` scoped to the `subagent.defaults.image` value.

## Info

### IN-01: Dead variable in workflow step

**File:** `.github/workflows/pricing-drift.yaml:56`
**Issue:** `DRIFT_BODY=$(cat /tmp/drift-output.txt)` is assigned and never used (the heredoc block below re-reads the file).
**Fix:** Delete the line.

### IN-02: Conservative-tier fallback ignores per-instance overrides

**File:** `internal/subagent/anthropic/pricing.go:117,137-139`
**Issue:** `estimatedCostCents` falls back to the package-level `conservativeTier` (compiled fable-5 rates) on a table miss. If an operator's overrides add a model *more expensive* than fable-5, or lower fable-5's own rates, the unknown-model fallback no longer reflects "most-expensive known tier" of the effective table.
**Fix:** Derive the conservative tier per-instance in `New()` (max across the merged `effective` map), or document the compiled-table-only semantics at the `conservativeTier` declaration.

### IN-03: `RederiveReservations` skips Jobs with `Status.Active <= 0` that still hold headroom

**File:** `internal/budget/reservation.go:151`
**Issue:** A just-created Job whose pod hasn't started (`Active` not yet incremented) and a completed Job whose Task reconcile hasn't yet rolled up its cost both have `Active <= 0` at restart scan time and are skipped — a small post-restart undercount window beyond the documented pre-Phase-14-label case. Also note the startup comment in `cmd/manager/main.go:581-585` claims the store is "populated before the first reconcile," but `mgr.Add` runnables start concurrently with the controllers after leader election — there is no ordering barrier, so a brief empty-store dispatch window exists.
**Fix:** Consider also rederiving Jobs without a terminal condition (use `isJobTerminal`-style condition check rather than `Active`), and soften/correct the main.go comment or gate first dispatch on a `store.Ready()` flag if the bound matters.

### IN-04: Fractional-cent live prices truncate instead of rounding

**File:** `hack/check-pricing-drift.sh:112-113`
**Issue:** `awk "BEGIN { printf \"%d\", ${INPUT_DOLLARS} * 100 }"` truncates (e.g. a hypothetical $0.075/MTok → 7, not 8), which would report a stable off-by-one "drift" against a correctly-rounded compiled value.
**Fix:** Use `printf "%.0f"` for round-half-up behavior.

---

_Reviewed: 2026-06-12T00:53:29Z_
_Reviewer: Claude (gsd-code-reviewer)_
_Depth: standard_
