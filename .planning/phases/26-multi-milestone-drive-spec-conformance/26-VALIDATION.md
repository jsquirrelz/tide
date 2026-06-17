---
phase: 26
slug: multi-milestone-drive-spec-conformance
status: draft
nyquist_compliant: false
wave_0_complete: false
created: 2026-06-17
---

# Phase 26 — Validation Strategy

> Per-phase validation contract for feedback sampling during execution.

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework** | go test + Ginkgo v2 / envtest (controller-runtime) + vitest (dashboard) |
| **Config file** | none — existing `test/integration/envtest/` suite + `dashboard/web` vitest config |
| **Quick run command** | `go test ./internal/... ./api/...` |
| **Full suite command** | `make test-int` (envtest + helm-template contract tests) |
| **Estimated runtime** | ~120–180 seconds (envtest); dashboard build/freshness adds ~60s |

---

## Sampling Rate

- **After every task commit:** Run `go test ./<touched-package>/...`
- **After every plan wave:** Run `make test-int`
- **Before `/gsd:verify-work`:** Full suite must be green AND `make verify-dashboard-freshness` / `verify-no-aggregates` / `verify-no-sqlite-dep` / `verify-dag-imports` green
- **Max feedback latency:** ~180 seconds

---

## Per-Task Verification Map

> Filled per-plan by the planner; rows below are the phase-requirement anchors. Read MAKE_EXIT and `grep -nE '^--- FAIL|^FAIL\s'`, not just the Ginkgo summary (CLAUDE.md gate).

| Task ID | Plan | Wave | Requirement | Threat Ref | Secure Behavior | Test Type | Automated Command | File Exists | Status |
|---------|------|------|-------------|------------|-----------------|-----------|-------------------|-------------|--------|
| 26-XX-XX | — | — | MS-01 | — | N/A (orchestration) | unit+golden | `go test ./internal/subagent/... ./internal/eval/...` | ❌ W0 | ⬜ pending |
| 26-XX-XX | — | — | MS-02 | — | N/A | envtest | `go test ./test/integration/envtest/...` | ✅ | ⬜ pending |
| 26-XX-XX | — | — | MS-03 | — | N/A | envtest | `go test ./test/integration/envtest/...` | ✅ | ⬜ pending |
| 26-XX-XX | — | — | SPEC-01 | — | N/A | envtest+visual | `go test ./test/integration/envtest/...` + dashboard render | ✅ | ⬜ pending |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*

---

## Wave 0 Requirements

- [ ] SPEC-01 2-milestone fixture authored in `test/integration/envtest/` (Project + 2 Milestones w/ `dependsOn` + phases + plans + 8 Tasks α…θ incl. cross-milestone `γ→η`)
- [ ] `project_planner.golden` + `ratchets/project_planner.txt` updated in the same commit as the template change (MS-01)
- [ ] Existing envtest infrastructure (`createSimplePhase/Milestone`, `makeGlobalWaveTask`, `assertWaveExists`) covers the rest — no new framework

*Most phase requirements ride existing infrastructure; the SPEC-01 fixture is the primary Wave 0 artifact.*

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| README mermaid diagrams replaced by real dashboard screenshots of the SPEC-01 fixture | SPEC-01 / D-07 | Requires a running cluster + dashboard with the fixture applied; static image asset capture | Apply the 2-milestone fixture to `kind-tide-dogfood`, open the global execution-DAG view (dagre LR), screenshot, commit as image asset, replace both README mermaid blocks (≈ README lines 91–159) |
| README ↔ implementation textual agreement (Milestone-edge = planning-DAG-only) | SPEC-01 / D-03 | Prose edit to `README.md` §"two distinct DAGs" | Reviewer confirms README states the Milestone edge contributes zero execution edges and matches §6d removal |

---

## Validation Sign-Off

- [ ] All tasks have `<acceptance_criteria>` with automated verify or Wave 0 dependencies
- [ ] Sampling continuity: no 3 consecutive tasks without automated verify
- [ ] Wave 0 covers the SPEC-01 fixture + golden/ratchet update
- [ ] No watch-mode flags
- [ ] Feedback latency < 180s
- [ ] `nyquist_compliant: true` set in frontmatter

**Approval:** pending
