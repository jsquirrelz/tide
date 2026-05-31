---
phase: 7
slug: project-to-milestone-authoring-and-self-bootstrap
status: draft
nyquist_compliant: false
wave_0_complete: false
created: 2026-05-31
---

# Phase 7 — Validation Strategy

> Per-phase validation contract for feedback sampling during execution. Derived from `07-RESEARCH.md` §Validation Architecture, extended with REQ 7 (down-stack cascade fixes).

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework** | Ginkgo v2.28 + Gomega (integration) · `go test` (unit) |
| **Config file** | none — `go test ./...` + Layer B kind suite |
| **Quick run command** | `go test ./cmd/stub-subagent/... ./internal/controller/...` |
| **Full suite command** | `make test-int` (Layer B kind: bare-Project cascade + existing 7 specs) |
| **Estimated runtime** | unit ~15s · `make test-int` inner go test ≤ ~355s (Phase 02.2 budget; kindTestTimeout 7m) |
| **Acceptance gate** | `make acceptance-v1-smoke` (`$0`, no API key) → `Project status.phase=Complete` |

---

## Sampling Rate

- **After every task commit:** Run the quick command for the touched package (`go test ./cmd/stub-subagent/...` or `./internal/controller/...`)
- **After every plan wave:** Run `make test-int`
- **Before `/gsd-verify-work`:** `make test-int` green AND `make acceptance-v1-smoke` reaches Complete
- **Max feedback latency:** ~355s (full Layer B); ~15s (unit)

---

## Per-Requirement Verification Map

*(Task IDs assigned by the planner; rows are requirement-level until plans exist.)*

| Req | Behavior | Wave | Test Type | Automated Command | File | Status |
|-----|----------|------|-----------|-------------------|------|--------|
| REQ 1 | Project dispatches `level=project,role=planner` Job; Initialized→Running | later | integration | `make test-int` | ❌ W0 `bare_project_test.go` | ⬜ pending |
| REQ 2 | Milestone CR materializes from `EnvelopeOut`, owner=Project, idempotent re-reconcile | later | integration | `make test-int` | ❌ W0 `bare_project_test.go` | ⬜ pending |
| REQ 3 | Stub emits 1 child/level (project→Milestone, milestone→Phase, phase→Plan, plan→Task), 0 at task | early | unit | `go test ./cmd/stub-subagent/...` | ❌ W0 `main_test.go`/`planner_test.go` | ⬜ pending |
| REQ 4 | Project Running→Complete when all owned Milestones Succeeded; stays Running otherwise | later | integration | `make test-int` | ❌ W0 `bare_project_test.go` | ⬜ pending |
| REQ 5 | Full `Milestone→Phase→Plan→Task` tree materializes AND reaches Succeeded; Project=Complete | later | integration | `make test-int` | ❌ W0 `bare_project_test.go` | ⬜ pending |
| REQ 6 | `make acceptance-v1-smoke` exits 0 at `Project=Complete`, `$0`, no script edits | final | acceptance | `make acceptance-v1-smoke` | ✅ script exists (unchanged) | ⬜ pending |
| REQ 7a | `Plan.Status.ValidationState=="Validated"` stamped in production → Wave materializes → Task executor Job runs | later | integration | `make test-int` | ❌ W0 `bare_project_test.go` | ⬜ pending |
| REQ 7b | `PlanReconciler.patchPlanSucceeded` → Plan/Phase reach Succeeded (boundary advances) | later | integration | `make test-int` | ❌ W0 `bare_project_test.go` | ⬜ pending |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*

---

## Wave 0 Requirements

- [ ] `test/integration/kind/testdata/bare-project.yaml` — bare Project fixture (Project only, `gates: all auto`, stub image, no `spec.git`, namespace + per-namespace PVC/SA/signing-key per existing suite helpers)
- [ ] `test/integration/kind/bare_project_test.go` — Layer B spec: apply bare Project; `Eventually` assert Milestone (owner=Project) → Phase → Plan → Task materialize and reach `Succeeded`; `Plan.Status.ValidationState=="Validated"`; a `Wave` materializes; Project reaches `Complete`. Covers REQ 1/2/4/5/7.
- [ ] `cmd/stub-subagent/planner_test.go` (or extend `main_test.go`) — unit: feed `EnvelopeIn{Role:"planner",Level:X}` for X∈{project,milestone,phase,plan,task}; assert emitted `out.json` `ChildCRDs` shape (1 child of correct Kind for the four planner levels; 0 for task; Task child carries `filesTouched`+`declaredOutputPaths`+`Dev.TestMode=success`; no `Wave`). Covers REQ 3.

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| `$0` cost (no `ANTHROPIC_API_KEY` consumed) | REQ 6 | Negative assertion — absence of API spend | Run `make acceptance-v1-smoke` with no `ANTHROPIC_API_KEY`; confirm `absoluteCapCents: 0` and no `BudgetExceeded`; Project reaches Complete |

*All other phase behaviors have automated verification.*

---

## Validation Sign-Off

- [ ] All requirements have an automated verify or a Wave 0 dependency
- [ ] Sampling continuity: no 3 consecutive tasks without automated verify
- [ ] Wave 0 covers the 3 MISSING test files above
- [ ] No watch-mode flags
- [ ] Feedback latency < 355s
- [ ] `nyquist_compliant: true` set in frontmatter (set by validation auditor post-execution)

**Approval:** pending
