---
phase: 38-small-independents-pricing-accuracy-promptfile-telemetry-nud
plan: 07
subsystem: controller-tests
tags: [debt, envtest, ginkgo, test-tiers, idempotency]
dependency_graph:
  requires: [38-02]
  provides:
    - "make test-heavy tier (Ginkgo Label(\"heavy\") controller envtests)"
    - "suite-level short-mode heavy guard in internal/controller/suite_test.go"
    - "PlannerRolledUpUID-named DEBT-01 exactly-once spec (focus-selectable)"
  affects:
    - Makefile (test-heavy, test-int Layer A2, test-int-fast Layer A2)
    - internal/controller unit-tier wall time (81s -> 54s standalone; 61s in umbrella)
tech_stack:
  added: []
  patterns:
    - "Ginkgo Label(\"heavy\") as single source of truth for the unit/heavy split"
    - "suite-level BeforeEach Skip guard instead of -ginkgo.label-filter on the umbrella (RESEARCH Q4: umbrella spans non-Ginkgo packages)"
key_files:
  created: []
  modified:
    - internal/controller/suite_test.go
    - internal/controller/project_rollup_idempotency_test.go
    - internal/controller/child_rollup_idempotency_test.go
    - internal/controller/budget_blocked_regression_test.go
    - internal/controller/project_baseref_halt_test.go
    - internal/controller/import_controller_test.go
    - internal/controller/plan_wavepause_test.go
    - internal/controller/project_boundary_push_test.go
    - internal/controller/project_controller_test.go
    - internal/controller/boundary_push_test.go
    - internal/controller/file_touch_gate_test.go
    - Makefile
decisions:
  - "DEBT-01 was already retired by the carried-in Phase 39 work (commits db7abe8 + 057047b, 2026-06-29) — executed only the remaining traceability delta (spec text names PlannerRolledUpUID); no production code changed"
  - "Layer A2 lines omit -ginkgo.flake-attempts=3: the Makefile's NO FLAKE TOLERANCE policy (7866329) postdates the plan and is a fixed contract"
  - "Heavy threshold >= 2s applied to the Ginkgo JSON timing report per spec, per plan; 12 specs selected"
metrics:
  duration: "25m (2026-07-11T13:23:15Z -> 2026-07-11T13:48:07Z)"
  completed: 2026-07-11
  tasks: 3
  commits: [8195986, 4581256, 096554d]
requirements_completed: [DEBT-01, DEBT-03]
status: complete
---

# Phase 38 Plan 07: DEBT-01 Stamp Hardening + DEBT-03 Heavy-Tier Split Summary

Heavy controller envtests (12 specs >= 2s, 32.8s aggregate) split into a labeled `make test-heavy` tier with zero specs lost (191 + 12 == 203, Y=204 conserved); DEBT-01's hardened PlannerRolledUpUID stamp was found already landed (Phase 39 carry-in) and only needed focus-selectable spec naming.

## Task Commits

| Task | Name | Commit | Files |
| ---- | ---- | ------ | ----- |
| 1 | DEBT-01 — PlannerRolledUpUID stamp + coverage | 8195986 | project_rollup_idempotency_test.go |
| 2 | DEBT-03 — heavy labels + suite short guard | 4581256 | suite_test.go + 10 *_test.go label sites |
| 3 | DEBT-03 — Makefile heavy tier | 096554d | Makefile |

## DEBT-01 (audit W1)

**The W1 site was already hardened.** The v1.0.6 audit (and the Phase 38 RESEARCH that cited it) described `project_controller.go` ~1376 as plain `client.MergeFrom` + swallowed error — but commit `db7abe8` ("fix(34-02): port exactly-once RetryOnConflict stamp to project-level PlannerRolledUpUID", 2026-06-29, the parallel session's Pre-flight Tech-Debt Hardening carried in as Phase 39) already applied the full WR-02/WR-03 pattern, and `057047b` added the exactly-once envtest (`project_rollup_idempotency_test.go`). REQUIREMENTS.md line 62 even records DEBT-01 as "Already satisfied — see PREFLIGHT-02".

Verified against the current tree (project_controller.go:1884-1923):
- `retry.RetryOnConflict(retry.DefaultRetry, ...)` with re-fetch of `latest` — line 1901
- idempotent early-return when `latest.Status.Budget.PlannerRolledUpUID == plannerJobName` — line 1906
- `client.MergeFromWithOptions(latest.DeepCopy(), client.MergeFromWithOptimisticLock{})` — line 1909
- `return ctrl.Result{}, fmt.Errorf("patch PlannerRolledUpUID: %w", mErr)` on exhaustion — line 1913 (return, not swallow)
- Pitfall-2 ordering preserved: `rollErr` branch logs non-fatally WITHOUT stamping (lines 1891-1892)
- plan 38-02's `setPricingFallbackIfNeeded` call adjacent and untouched (line 1919)

**Executed delta:** the RESEARCH validation map selects DEBT-01 coverage via `-ginkgo.focus='PlannerRolledUpUID'`, which matched no spec text. Renamed the Describe to "ProjectRollupIdempotency — project-level PlannerRolledUpUID stamp (PREFLIGHT-02 / DEBT-01 W1)" and noted the closure in the file header. Focused run: `Ran 2 of 204 Specs`, 2 passed.

## DEBT-03 heavy-tier split

### Heavy-spec selection (Ginkgo JSON report, threshold >= 2s, single -v -short baseline run)

| Runtime | Spec (container :: leaf) | File | Label site |
| ------- | ------------------------ | ---- | ---------- |
| 6.73s | BASE-02/BASE-03 :: stamps CloneFailed=True/BaseRefUnresolvable... | project_baseref_halt_test.go:241 | It |
| 3.17s | ImportReconciler :: sets ReasonCyclicPlanDetected and creates ZERO child CRs | import_controller_test.go:307 | It |
| 3.12s | BudgetBlocked run-1 regression (b) (single-It container) | budget_blocked_regression_test.go:151 | Describe |
| 2.54s | PlanReconciler PauseBetweenWaves :: No WaveOrLevelPaused condition is set | plan_wavepause_test.go:206 | It |
| 2.32s | ProjectReconciler integration-miss arms :: no new push Job after parked conflict GC'd | project_boundary_push_test.go:1175 | It |
| 2.31s | ProjectReconciler init + budget :: BYPASS-04 raise-absolute-cap-alone resume | project_controller_test.go:654 | It |
| 2.24s | Up-stack W-2 boundary push :: reject annotation, push Job NOT created | boundary_push_test.go:476 | It |
| 2.08s | file-touch gate :: does not park when mode is warn | file_touch_gate_test.go:342 | It |
| 2.08s | ChildRollupIdempotency — Phase level (single-It container) | child_rollup_idempotency_test.go:196 | Describe |
| 2.08s | ChildRollupIdempotency — Milestone level (single-It container) | child_rollup_idempotency_test.go:81 | Describe |
| 2.07s | ChildRollupIdempotency — Plan level (single-It container) | child_rollup_idempotency_test.go:322 | Describe |
| 2.03s | ProjectRollupIdempotency — PlannerRolledUpUID (single-It container) | project_rollup_idempotency_test.go:51 | Describe |

12 heavy specs, 32.8s aggregate of the 76.1s total spec runtime. The Leader Election spec was NOT labeled or modified (own `testing.Short()` guard; `git diff` on leader_election_test.go is clean).

### Conservation arithmetic (Pitfall 6 — same instrument: Ginkgo "Ran X of Y Specs", `-v -short -count=1`)

| Number | Value | Run |
| ------ | ----- | --- |
| X_pre | 203 | baseline `Ran 203 of 204 Specs in 80.860 seconds` (pre-label) |
| Y_pre | 204 | same baseline run |
| X_unit | 191 | post-split `Ran 191 of 204 Specs in 53.608 seconds`, 0 failed |
| X_heavy | 12 | `make test-heavy`: `Ran 12 of 204 Specs in 38.170 seconds`, 12 passed, MAKE_EXIT=0 |
| Y | 204 | identical in unit and heavy runs |

**X_unit + X_heavy = 191 + 12 = 203 = X_pre; Y unchanged.** Skipped delta 203-191 = 12 = heavy count. (The 13th skip in the unit run's "13 Skipped" is the leader-election Short() guard, present in both pre and post baselines — cancels.)

### Mechanism

- `Label("heavy")` is the single source of truth (added alongside existing labels where present, e.g. `Label("envtest", "heavy")`).
- Suite-level `BeforeEach` in suite_test.go: `testing.Short() && slices.Contains(CurrentSpecReport().Labels(), "heavy")` → `Skip(...)`. Code guard, not a flag, because the umbrella `make test` invocation spans non-Ginkgo packages where `-ginkgo.label-filter` is an unknown flag (RESEARCH Q4).
- Makefile: `test-heavy` target (test-leader-election's KUBEBUILDER_ASSETS one-liner shape, 20m timeout) + Layer A2 lines in `test-int` (inside the `set -e` shell) and `test-int-fast`. `grep -c "label-filter='heavy'" Makefile` == 3. Umbrella `test`/`test-only` recipes untouched.

## Verification (observed, host go1.26.4 darwin/arm64 + envtest 1.36.2)

- `make test`: MAKE_EXIT=0, 0 `--- FAIL` lines, all 40 packages ok; internal/controller 60.7s in-umbrella (baseline ~81s standalone)
- `make test-heavy`: MAKE_EXIT=0, `Ran 12 of 204`, 12 passed, 0 FAIL lines
- `make test-leader-election`: MAKE_EXIT=0; focused re-run confirms `Ran 1 of 204 Specs`, 1 passed
- Focused DEBT-01 run: `-ginkgo.focus='PlannerRolledUpUID'` → 2 specs, both green
- leader_election_test.go unmodified (git diff clean)

Note: the plan cited "Go absent on host Mac / run in docker golang:1.26" — Go 1.26.4 is now installed on the host, so all runs executed natively with worktree-local envtest assets (`make setup-envtest`).

## Deviations from Plan

**1. [Plan premise stale] DEBT-01 code + envtest already landed**
- **Found during:** Task 1 read_first
- **Issue:** The plan (and audit W1 / RESEARCH) described the W1 site as plain-MergeFrom + swallow; commits db7abe8 + 057047b (2026-06-29, Phase 39 carry-in) had already applied the hardened pattern and the exactly-once envtest. REQUIREMENTS.md already marked DEBT-01 complete.
- **Fix:** Executed only the remaining delta — spec-text naming so the validation map's focus selector works. No production code touched; 38-02's pricing-fallback block untouched.
- **Files modified:** internal/controller/project_rollup_idempotency_test.go
- **Commit:** 8195986

**2. [Broken acceptance instrument] Plan's awk range check is self-defeating**
- **Found during:** Task 1 acceptance
- **Issue:** `awk '/plannerJobName := fmt.Sprintf/,/^\t}/'` terminates at the `	} else if isFirstCompletion...` line (3 lines into the region), before the retry block — it returns 0 even on a fully hardened site.
- **Fix:** Verified the criterion with direct line evidence instead: `grep -n` shows RetryOnConflict (1901), MergeFromWithOptimisticLock (1909), and the returned `fmt.Errorf("patch PlannerRolledUpUID: %w", ...)` (1913) all inside the plannerJobName block (1884-1923).

**3. [Fixed-contract override] No `-ginkgo.flake-attempts=3` in Layer A2**
- **Found during:** Task 3
- **Issue:** The plan's test-int Layer A2 line prescribed `-ginkgo.flake-attempts=3`, but the current Makefile carries an explicit NO FLAKE TOLERANCE policy (commit 7866329, postdates the plan): retries masked the Phase 36/37 kind regression for days.
- **Fix:** Layer A2 invocations run with no retries, matching Layer A/B style.
- **Commit:** 096554d

**4. [Internally inconsistent acceptance] test-heavy help text reworded**
- **Found during:** Task 3 acceptance
- **Issue:** The plan prescribed help text containing the literal `label-filter='heavy'` AND `grep -c "label-filter='heavy'" Makefile == 3` — the prescribed text makes the count 4.
- **Fix:** Help text says `selects Ginkgo Label("heavy")` instead; the grep counts exactly the 3 invocation lines.

**5. [Out of scope — deferred] `make lint` fails on 4 pre-existing modernize issues**
- **Found during:** Final lint verification
- **Issue:** golangci-lint modernize flags 4 `ptr(x)`→`new(x)` sites in cmd/dashboard/main_test.go, introduced by wave-1 commit d57209a (plan 38-05). Not touched by this plan.
- **Fix:** Logged to `deferred-items.md`; not fixed (scope boundary — dashboard surface belongs to 38-05; lint is clean for all files this plan modified).

## Threat Register Outcomes

- **T-38-19 (lost stamp under conflict):** mitigated — hardened pattern verified present with exactly-once envtest coverage (pre-existing via Phase 39; traceability closed here).
- **T-38-20 (specs silently dropped):** mitigated — conservation arithmetic above, same instrument, all five numbers recorded.
- **T-38-21 (heavy tier bloating CI):** accepted per plan — heavy tier rides test-int-fast/test-int, off the per-push TEST-01 path.
- No new threat surface introduced (test files + Makefile only).

## Self-Check: PASSED

- 38-07-SUMMARY.md exists on disk
- Task commits 8195986, 4581256, 096554d all present in git log (the docs commit carrying this file cannot name its own hash)
- STATE.md / ROADMAP.md untouched (0 diff vs fork base 4882b42)
- No file deletions across the plan's commits; working tree clean (bin/ envtest assets are gitignored)
