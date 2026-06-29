---
phase: 32-d3-dispatch-concurrency-cap
verified: 2026-06-29T00:00:00Z
status: passed
score: 5/5 must-haves verified
overrides_applied: 0
re_verification:
  previous_status: gaps_found
  previous_score: 4/5
  gaps_closed:
    - "The executor pool is untouched and `make lint` stays green (CONCUR-03 success criterion) — `make lint` now exits 0 (0 issues) after the dead `countingStatusWriter` type + Status() method were removed in commit eef8912; the WR-04 single-patch assertion still routes through countingClient.Status()/countingStatusPatcher."
  gaps_remaining: []
  regressions: []
deferred: []
human_verification:
  - test: "Apply a Project that fans out to 5+ Milestones with plannerConcurrency=2 on a live single-node kind cluster; run `kubectl get jobs -l tideproject.k8s/role=planner -w` and tail the manager log."
    expected: "At most 2 non-terminal planner Jobs Running at any moment; as each terminates a new one starts; deferred-dispatch V(1) `planner dispatch deferred: concurrency cap reached` log lines accumulate without new Jobs exceeding the cap."
    why_human: "Roadmap success criterion #1 is explicitly a live-cluster observation. envtest does not run pods, so steady-state running-pod bounding cannot be asserted programmatically — only the count-gate logic + park return are unit-verified. 32-VALIDATION.md routes this to live confirmation."
advisories:
  - id: WR-01 (32-REVIEW)
    file: internal/controller/dispatch_helpers.go:304-322
    concern: "A planner Job stuck mid-deletion (foreground-GC stall) counts as non-terminal and permanently consumes one GLOBAL cap slot, throttling dispatch cluster-wide. Suggested fix: skip Jobs with non-zero DeletionTimestamp."
    status: "open — not implemented (narrow stall risk, not data-loss; advisory only)."
  - id: WR-02 (32-REVIEW)
    file: internal/controller/milestone_controller.go:69
    concern: "Stale doc comment '...size 16' on the PlannerPool field — the config default is now 4."
    status: "open — single drifted comment still present (cosmetic)."
  - id: WR-03 (32-REVIEW)
    file: internal/config/config.go:117 vs charts/tide/values.yaml:85-86
    concern: "Default 4 is narrower than the chart's own stated min-safe widest-wave (6-phase → >=6), so a wide milestone serializes phase dispatch at 10s/retry. Throughput concern, not correctness (RequeueAfter retries; no dropped work)."
    status: "open — code+chart agree on 4; prose argues for >=6. Human sizing decision."
---

# Phase 32: D3 — Dispatch Concurrency Cap Verification Report

**Phase Goal:** In-flight planner Jobs are bounded at steady state by a configurable cap (`plannerConcurrency`) so the planning cascade cannot OOM a single-node cluster; the cap parks (RequeueAfter) excess dispatches rather than silently truncating a wave; planner and executor pools remain separately sized; default lowered from 16 to a single-node-safe value in `charts/tide/values.yaml`.
**Verified:** 2026-06-29
**Status:** passed
**Re-verification:** Yes — after gap closure (commit `eef8912` removed the dead `countingStatusWriter` test type that was failing `make lint`)

## Re-Verification Summary

The prior pass (2026-06-29, score 4/5) returned `gaps_found` with a single BLOCKER: `make lint` was RED (`Error 1`, 2 `unused` findings) due to an orphaned `countingStatusWriter` type + its `Status()` method introduced by plan 32-02's WR-04 test — dead code the WR-04 assertion never used (it routes through `countingClient.Status()` → `countingStatusPatcher`).

Commit `eef8912` ("fix(32): remove unused countingStatusWriter dead code") removed the orphaned type. Re-confirmed against the current code and commands:

- **`make lint` now exits 0.** `MAKE_EXIT=0`, output `0 issues.` (echoed make exit code, not a background-task notification — per the CLAUDE.md Phase-7 lesson). The CONCUR-03 success criterion is satisfied.
- **`countingStatusWriter` is absent** from `adoption_lifecycle_test.go`; the file now defines `countingStatusPatcher` (61-81) and `countingClient` (84-91), and the WR-04 test wires through `countingClient.Status()` (line 89-91).
- **WR-04 single-patch test passes:** `go test ./internal/controller/... -run 'Adopt|Suppress|SinglePatch' -count=1` → `ok ... 4.781s` (exit 0).
- **Scope of the fix:** `git show --stat eef8912` shows it touched ONLY `internal/controller/adoption_lifecycle_test.go` (1 file, +5/-14). No dispatch, config, or chart file changed → CONCUR-01/02/04 are provably unchanged. Re-traced each below as a regression check; all hold.

## Goal Achievement

### Observable Truths

| # | Truth | Status | Evidence |
|---|-------|--------|----------|
| 1 | The cap is Option B — a live `client.List` count of NON-terminal planner Jobs checked BEFORE `PlannerPool.Acquire` at all four dispatch sites, returning RequeueAfter when count >= cap (NOT merely a lowered config value) | ✓ VERIFIED | `plannerInFlightCount` (dispatch_helpers.go:304) lists `MatchingLabels{tideproject.k8s/role: planner}` and counts `!isJobTerminal(&jobs.Items[i])` (317). Gate wired at milestone:385 / phase:383 / plan:389 / project:1185, each followed by `RequeueAfter: 10 * time.Second` and THEN `PlannerPool.Acquire` (milestone:398, phase:396, plan:402, project:1198). Gate line precedes Acquire line in every file → no slot leak (CONCUR-01). |
| 2 | Default plannerConcurrency is 4 in BOTH config.go AND the chart (+ canonical hack source); literal 16 gone from both | ✓ VERIFIED | config.go:117 `resolveField("plannerConcurrency", raw.PlannerConcurrency, 4, ...)`; charts/tide/values.yaml:88 + hack/helm/tide-values.yaml:88 both `plannerConcurrency: 4`. Repo-wide grep for `plannerConcurrency: 16` / `PlannerConcurrency, 16` across charts/, hack/, internal/config/ → 0 matches (CONCUR-02). |
| 3 | A deferred dispatch logs + RequeueAfter (never silent drop / Go error for cap-reached) | ✓ VERIFIED | All four sites emit `logf.FromContext(ctx).V(1).Info("planner dispatch deferred: concurrency cap reached", ...)` then `return ctrl.Result{RequeueAfter: 10 * time.Second}, nil` (plan site adds `, true,`). List error path returns a wrapped retryable error, not a drop. Tests `TestConcurrencyCapGate_*` assert RequeueAfter==10s + no extra Job created (CONCUR-04). |
| 4 | Executor pool untouched; no executor/task identifier in the planner gate; `make lint` green (CONCUR-03) | ✓ VERIFIED | Cross-pool dimension: gate helper references only `role: planner`; no executor/task identifier; phase-32 commits touched no executor logic. **`make lint` now exits 0 (0 issues)** after the dead-code removal — the sole gap from the prior pass is closed. |
| 5 | Carried-in WR-01/02/03/04 hardening implemented (32-02, via CONTEXT D-06) | ✓ VERIFIED | WR-02/03: RetryOnConflict + MergeFromWithOptimisticLock marker stamps at milestone/phase/plan. WR-01: project suppression patch switched to `MergeFromWithOptimisticLock`; false "Conflict is retryable" comment removed. WR-04: `countingStatusPatcher` Status-patch counter + single-patch assertion present and passing (test green); the orphaned `countingStatusWriter` that previously broke lint is removed. `Adopt|Suppress|SinglePatch` tests green. |

**Score:** 5/5 truths verified

### Required Artifacts

| Artifact | Expected | Status | Details |
|----------|----------|--------|---------|
| `internal/controller/dispatch_helpers.go` | `plannerInFlightCount` (non-terminal planner Job count) | ✓ VERIFIED | func present (304); reuses `isJobTerminal`; namespace-aware via `watchNamespace`. |
| `internal/pool/pool.go` | `Pool.Capacity()` | ✓ VERIFIED | `func (p *Pool) Capacity() int { return cap(p.sem) }`. |
| `internal/config/config.go` | plannerConcurrency default 4 | ✓ VERIFIED | line 117 default literal `4`. |
| `charts/tide/values.yaml` | single-node-safe default + sizing doc | ✓ VERIFIED | `plannerConcurrency: 4` (88) + multi-line CONCUR-04 sizing comment (79-86) incl. ">= 6 for a 6-phase milestone". |
| `internal/controller/dispatch_concurrency_cap_test.go` | cap-gate coverage | ✓ VERIFIED | `TestConcurrencyCapGate_*` + `TestGatePrecedesAcquire_SlotNotConsumed`; package green. |
| `internal/controller/adoption_lifecycle_test.go` | single-patch atomicity assertion | ✓ VERIFIED | WR-04 assertion present + passing; routes through `countingClient`/`countingStatusPatcher`; dead `countingStatusWriter` removed → `make lint` green. |

### Key Link Verification

| From | To | Via | Status | Details |
|------|----|-----|--------|---------|
| milestone_controller.go | plannerInFlightCount | gate before Acquire | ✓ WIRED | :385 gate < :398 Acquire |
| phase_controller.go | plannerInFlightCount | gate before Acquire | ✓ WIRED | :383 gate < :396 Acquire |
| plan_controller.go | plannerInFlightCount | gate before Acquire (bool sig) | ✓ WIRED | :389 gate < :402 Acquire; returns `..., true, nil` |
| project_controller.go | plannerInFlightCount | gate before Acquire | ✓ WIRED | :1185 gate < :1198 Acquire |

### Behavioral Spot-Checks

| Behavior | Command | Result | Status |
|----------|---------|--------|--------|
| Lint (CONCUR-03 gate) | `make lint` | `MAKE_EXIT=0`, `0 issues.` | ✓ PASS |
| WR-04 single-patch + adoption | `go test ./internal/controller/... -run 'Adopt\|Suppress\|SinglePatch' -count=1` | `ok ... 4.781s`, exit 0 | ✓ PASS |
| Fix scope (no dispatch/config/chart touched) | `git show --stat eef8912` | 1 file (`adoption_lifecycle_test.go`), +5/-14 | ✓ PASS |
| Gate-before-Acquire ordering (4 sites) | grep gate line < Acquire line | 385<398, 383<396, 389<402, 1185<1198 | ✓ PASS |
| Default=4 / no literal 16 | grep config + chart + hack | 4 everywhere; `plannerConcurrency: 16` → 0 matches | ✓ PASS |

### Requirements Coverage

| Requirement | Source Plan | Description | Status | Evidence |
|-------------|-------------|-------------|--------|----------|
| CONCUR-01 | 32-01 | In-flight planner Jobs bounded (running pods, not Create calls) | ✓ SATISFIED | Option-B live-List gate at 4 sites; non-terminal counting via isJobTerminal; park before Acquire. Live steady-state confirmation routed to human (SC#1). |
| CONCUR-02 | 32-01 | Default reduced from 16, documented in values.yaml | ✓ SATISFIED | 4 in config + chart + canonical hack; 16 absent everywhere. |
| CONCUR-03 | 32-01 | Pools separately sized; executor unchanged; `make lint` green | ✓ SATISFIED | Executor untouched + no cross-pool bleed; `make lint` now exits 0 (gap closed by eef8912). |
| CONCUR-04 | 32-01 | Deferred dispatch observable + never dropped + chart documents sizing | ✓ SATISFIED | V(1) log + RequeueAfter(10s) at all 4 sites; chart sizing comment present. |
| WR-01..04 | 32-02 | Carried-in 31-REVIEW hardening (per CONTEXT D-06; not REQUIREMENTS.md IDs) | ✓ SATISFIED | Optimistic-lock suppression patch, RetryOnConflict marker stamps, single-patch test green. WR-04's dead-code lint failure is resolved. |

No orphaned REQUIREMENTS.md IDs: REQUIREMENTS.md maps exactly CONCUR-01..04 to Phase 32; all four appear in 32-01 frontmatter. WR-01..04 are correctly sourced from 31-REVIEW.md via CONTEXT D-06, not REQUIREMENTS.md.

### Anti-Patterns Found

| File | Line | Pattern | Severity | Impact |
|------|------|---------|----------|--------|
| — | — | None blocking | — | The prior pass's lone blocker (unused `countingStatusWriter` dead code) is removed; `make lint` is green. Three 32-REVIEW advisories remain (below) — none blocking. |

### Advisory Items (from 32-REVIEW.md — recorded for human awareness, NOT blocking)

| ID | File | Concern | Disposition |
|----|------|---------|-------------|
| WR-01 (review) | dispatch_helpers.go:304-322 | A planner Job stuck mid-deletion (foreground-GC stall) counts as non-terminal and permanently consumes one GLOBAL cap slot, throttling dispatch cluster-wide. Suggested fix: skip Jobs with non-zero `DeletionTimestamp`. | Open — not implemented. Narrow stall risk, not data-loss; advisory only. |
| WR-02 (review) | milestone_controller.go:69 | Stale doc comment "...size 16" — default is now 4. | Open — single drifted comment still present; cosmetic. |
| WR-03 (review) | config.go:117 vs values.yaml:85-86 | Default 4 is narrower than the chart's own stated min-safe widest-wave (6-phase → >=6), so a wide milestone serializes phase dispatch at 10s/retry. Throughput concern, not correctness (RequeueAfter retries; no dropped work). | Open — code+chart agree on 4; prose argues for 6. Human sizing decision. |
| IN-01/02/03 (review) | test files / pool.go | Hand-rolled `contains`; Capacity() zero-cap invariant undocumented; List-error branch untested. | Open — INFO only. |

### Human Verification Required

#### 1. Live steady-state cap bounding (Roadmap SC #1)

**Test:** Apply a Project fanning out to 5+ Milestones with `plannerConcurrency=2` on a live single-node kind cluster; `kubectl get jobs -l tideproject.k8s/role=planner -w` and tail the manager log.
**Expected:** At most 2 non-terminal planner Jobs Running at any moment; new Jobs start only as prior ones terminate; manager log shows accumulating `planner dispatch deferred: concurrency cap reached` V(1) lines without the cap being exceeded.
**Why human:** envtest does not run pods — the count-gate logic and park return are unit-verified, but actual running-pod steady-state bounding requires a live cluster. 32-VALIDATION.md explicitly routes this to live confirmation.

### Gaps Summary

No blocking gaps. The D3 cap mechanism is correct and complete: Option B (live `client.List` of non-terminal planner Jobs, gated before `PlannerPool.Acquire` at all four sites, parking with `RequeueAfter(10s)`) is implemented exactly as the goal demands — genuinely NOT a lowered-config-value-only change. CONCUR-01/02/04 are fully satisfied in code and unit-tested green; the carried-in WR-01/02/03/04 hardening is functionally implemented and tested.

The single blocker from the prior pass — `make lint` RED on dead `countingStatusWriter` test code — is closed by commit `eef8912`, which touched only `adoption_lifecycle_test.go` (+5/-14) and leaves the WR-04 single-patch assertion routing intact through `countingClient`/`countingStatusPatcher`. Re-confirmed live: `make lint` exits 0 (`MAKE_EXIT=0`, `0 issues.`); the WR-04 test passes; and the dispatch/config/chart surfaces are byte-for-byte unchanged from the verified state.

The only remaining item is the inherently live-cluster SC#1 (steady-state running-pod bounding), routed to human verification, plus three non-blocking 32-REVIEW advisories recorded above for human awareness.

---

_Verified: 2026-06-29_
_Verifier: Claude (gsd-verifier)_
