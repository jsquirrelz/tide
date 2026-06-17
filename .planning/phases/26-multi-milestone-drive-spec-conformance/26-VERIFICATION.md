---
phase: 26-multi-milestone-drive-spec-conformance
verified: 2026-06-17T19:10:00Z
status: passed
gate_decision: APPROVED
score: 4/4 requirements verified (MS-01, MS-02, MS-03, SPEC-01) + carried-in debt (OQ-3, WR-02)
re_verification: false
---

# Phase 26: Multi-Milestone Drive + Spec Conformance — Verification Report

**Phase Goal:** A single Project drives multiple Milestones end-to-end via the Milestone DAG, with Tasks from different Milestones sharing global waves and per-milestone gate policy composing across the DAG — and the README cross-plan/cross-phase/cross-milestone worked example is pinned as an executable conformance test.
**Verified:** 2026-06-17
**Status:** passed
**Re-verification:** No — initial verification

## Goal Achievement

### Observable Truths

| # | Truth | Status | Evidence |
|---|-------|--------|----------|
| 1 | Planner emits a Milestone DAG (N milestones, each with `dependsOn`); every milestone's Tasks join the single global Execution DAG (MS-01) | ✓ VERIFIED | `project_planner.tmpl:13` "Emit one Milestone child-CRD per milestone in the DAG, each with its dependsOn"; HOW-TO-EMIT block + worked example (lines 21–45). Golden + ratchet co-updated in commit `9b1f4f5` (ratchet 2668 == golden byte count). Idempotency guard `project_controller.go:955` gates on Job existence (`tide-project-<uid>-1`), N>1-safe by design (comment 957–962 explains a count guard would abort mid-stream). `go test ./internal/eval/...` ok. |
| 2 | A Task in one Milestone shares a global wave with a Task in another; cross-milestone task deps honored (MS-02) | ✓ VERIFIED | SPEC-01 envtest passes: ζ (Milestone B) IS in Wave 0 (`Eventually ... sc-zeta`), η is NOT in Wave 0 (`Consistently` negative assertion for γ→η). `depgraph.go §6d` removed — `buildGlobalEdges` no longer takes a milestone param (commit `c279972`); only §6a/6b/6c remain (depgraph.go:217/227/242). README §two-distinct-DAGs note added (line 87, 178–179). |
| 3 | Milestone-level gate policy composes across the DAG — approve-every-milestone for N, full-auto + full-supervised expressible (MS-03) | ✓ VERIFIED | 3 MS-03 envtest specs PASS: `gates.milestone=approve` (both milestones reach AwaitingApproval, both release on approve-milestone); `gates.milestone=auto` (no holds); `gates.task=approve` (≥1 Task at AwaitingApproval, released by approve-task). No new gate schema added (D-04 honored — conformance over `Project.Spec.Gates`). |
| 4 | README α…θ worked example encoded as executable test producing `[{α,β,γ,ζ},{δ,η},{ε,θ}]`; README and implementation agree (SPEC-01) | ✓ VERIFIED | `spec_conformance_test.go` SPEC-01 spec PASS — asserts wave membership for all 8 tasks across waves 0/1/2 (real CRDs, real reconcilers), cross-milestone γ→η edge `sc-eta.DependsOn=[sc-gamma,sc-zeta]`. Both README mermaid fences REMOVED (zero `\`\`\`mermaid` in README.md); replaced by `docs/screenshots/planning-dag.png` (1624×580, 70KB) + `execution-dag.png` (1500×624, 68KB), both real dashboard renders of the SPEC-01 fixture (visually confirmed: WAVE 0 {α,β,γ,ζ}→WAVE 1 {δ,η}→WAVE 2 {ε,θ}, LR layout). |
| 5 | Carried-in debt D-08 (OQ-3 in-flight-safe prune guard, CR-01 PruneShrink green) | ✓ VERIFIED | `wave_controller.go:177` sets phase `ZeroMembers`; `project_controller.go:1652/1658` distinguishes zero-member (Phase=="" && TaskRefs==0) from in-flight, prunes only zero-member OR Succeeded. CR-01 PruneShrink envtest PASS; live log shows `skipping prune of in-flight wave` (Running, memberCount 1 → skip) vs zero-member (Phase "", memberCount 0 → prunable). |
| 6 | Carried-in debt D-09 (WR-02 globalDependentsMapper watch predicate) | ✓ VERIFIED | `task_controller.go:1771/1778` `builder.WithPredicates(statusPhaseOrDepsChanged)`. `TestStatusPhaseOrDepsChangedPredicate` PASS (7 sub-cases): phase change→true, dependsOn change→true, no-op resourceVersion→false, plus Create/Delete/Generic edge cases. |

**Score:** 4/4 requirements verified (plus both carried-in debt items)

### Required Artifacts

| Artifact | Expected | Status | Details |
|----------|----------|--------|---------|
| `internal/subagent/common/templates/project_planner.tmpl` | N-milestone emission + dependsOn | ✓ VERIFIED | "per milestone in the DAG" instruction present |
| `internal/eval/testdata/goldie/project_planner.golden` + `ratchets/project_planner.txt` | Co-updated | ✓ VERIFIED | Same commit `9b1f4f5`, ratchet 2668 == golden bytes |
| `internal/controller/depgraph.go` | §6d removed, §6a/b/c kept | ✓ VERIFIED | §6a/6b/6c present; §6d gone (param dropped) |
| `internal/controller/project_controller.go` | N-safe idempotency + OQ-3 prune guard | ✓ VERIFIED | Job-existence guard + ZeroMembers-aware prune |
| `internal/controller/wave_controller.go` | ZeroMembers phase | ✓ VERIFIED | Line 177 |
| `internal/controller/task_controller.go` + `_predicate_test.go` | WR-02 predicate + test | ✓ VERIFIED | Predicate wired, 7-case test green |
| `test/integration/envtest/spec_conformance_test.go` | SPEC-01 + MS-03 envtest | ✓ VERIFIED | 4 specs, all pass |
| `docs/screenshots/{planning,execution}-dag.png` | Real dashboard renders | ✓ VERIFIED | Valid PNGs, visually confirmed content |
| `cmd/dashboard/api/execution_dag.go` + `router.go` | GET-only endpoint, real data | ✓ VERIFIED | GET-only; lists real TaskList, derives waveIndex from Wave.Status.TaskRefs |
| `dashboard/web/src/components/GlobalExecutionDAGView.tsx` + `App.tsx` | Global DAG view, wired | ✓ VERIFIED | 351 lines, reuses TaskNode/WaveBackground/applyDagreLayout(LR); App.tsx fetches endpoint, renders view |
| `README.md` + `REQUIREMENTS.md` | D-03 note + DEPS-02 reinterpretation | ✓ VERIFIED | README:87/178-179; REQUIREMENTS:30 |

### Behavioral Spot-Checks / Probe Execution

| Check | Command | Result | Status |
|-------|---------|--------|--------|
| eval golden+ratchet | `go test ./internal/eval/...` | ok | ✓ PASS |
| controller suite (-short) | `go test -short ./internal/controller/...` | ok 54.0s | ✓ PASS |
| WR-02 predicate | `go test -run Predicate` | 7/7 PASS | ✓ PASS |
| SPEC-01 + MS-03 conformance | `go test envtest -ginkgo.focus="SpecConformance\|MS03"` | Ran 4 of 55, 4 Passed 0 Failed | ✓ PASS |
| CR-01 PruneShrink | `go test envtest -ginkgo.focus="PruneShrink"` | 1 Passed 0 Failed | ✓ PASS |
| Full Layer A envtest tier | `go test ./test/integration/envtest/... -count=1` | exit 0, no `--- FAIL`/`FAIL` | ✓ PASS |
| build | `go build ./...` | exit 0 | ✓ PASS |
| `make verify-no-aggregates` | — | OK exit 0 | ✓ PASS |
| `make verify-no-sqlite-dep` | — | OK exit 0 | ✓ PASS |
| `make verify-dag-imports` | — | OK exit 0 | ✓ PASS |
| `make verify-dashboard-freshness` | — | PASS embed matches fresh build; telemetry marker present; 204 SPA tests pass | ✓ PASS |
| metric label set | grep `registry.go` | `{project,phase,plan,wave}`; no `task` label | ✓ PASS |

**Note on `make test-int` (CLAUDE.md gate semantics):** Layer B kind tests (`medium_http`, `bare_project`) fail on this host for documented ENVIRONMENTAL reasons (missing pre-built fixture images, co-tenant cluster). Verified independently: `git log b881fdf..HEAD -- test/integration/kind/` returns ZERO Phase-26 commits — no kind file was touched this phase. The authoritative surface for Phase 26's controller changes is Layer A envtest, which passes clean (exit 0, no FAIL).

### Requirements Coverage

| Requirement | Source Plan | Description | Status | Evidence |
|-------------|-------------|-------------|--------|----------|
| MS-01 | 26-01, 26-03 | N-milestone planner + Milestone DAG; tasks join global DAG | ✓ SATISFIED | Truth 1, SPEC-01 envtest |
| MS-02 | 26-01, 26-02, 26-03 | Cross-milestone shared waves; §6d removed | ✓ SATISFIED | Truth 2 (ζ free, γ→η honored) |
| MS-03 | 26-03 | Gate composition (approve/auto/full-supervised) | ✓ SATISFIED | Truth 3, 3 MS-03 specs |
| SPEC-01 | 26-03, 26-04 | Executable + visual conformance | ✓ SATISFIED | Truth 4, screenshots + envtest |
| DEPS-02 (reinterpreted, Phase 23) | 26-01 | §6d removed; §6b/6c retained | ✓ SATISFIED | REQUIREMENTS.md:30 records reinterpretation |

### Anti-Patterns Found

| File | Line | Pattern | Severity | Impact |
|------|------|---------|----------|--------|
| (none) | — | No unreferenced TBD/FIXME/XXX in any Phase-26-modified source file | — | Clean |

### Minor Observations (non-blocking)

- `assertWaveMembership` (spec_conformance_test.go:125) is a subset check (`expected ⊆ TaskRefs`), not strict set-equality — it would not catch an *extra* task in a wave on its own. However, the schedule is fully pinned by the combination of (a) all 8 tasks asserted into waves 0/1/2, (b) only waves 0/1/2 asserted to exist, and (c) the strict negative `Consistently` assertion that sc-eta is NOT in Wave 0. The load-bearing cross-milestone proof is strict. Not a gap.
- No dedicated unit test named "Milestone DependsOn adds zero execution edges," but `buildGlobalEdges`'s signature no longer accepts milestones (structurally impossible for §6d to fire), and the SPEC-01 envtest proves the behavior end-to-end. Adequate coverage.

### Human Verification Required

None. The visual deliverable (D-07 screenshots) was verified by reading the committed PNGs directly — both render the SPEC-01 fixture with the documented wave schedule and containment hierarchy.

### Gaps Summary

No gaps. All four phase requirements (MS-01, MS-02, MS-03, SPEC-01) and both carried-in Phase-25 debt items (OQ-3 D-08, WR-02 D-09) are delivered and verified against the codebase with passing tests and live behavioral evidence. All four locked guards (no-aggregates, no-sqlite, dag-imports, dashboard-freshness) are green; the metric label set is unchanged; the new endpoint is GET-only; no unreferenced debt markers were introduced. The README and implementation agree both textually (D-03 planning-edge note) and visually (mermaid replaced by real dashboard renders).

---

_Verified: 2026-06-17_
_Verifier: Claude (gsd-verifier)_
