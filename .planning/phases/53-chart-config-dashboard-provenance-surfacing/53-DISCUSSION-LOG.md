# Phase 53: Chart Config + Dashboard Provenance Surfacing - Discussion Log

> **Audit trail only.** Do not use as input to planning, research, or execution agents.
> Decisions are captured in CONTEXT.md — this log preserves the alternatives considered.

**Date:** 2026-07-21
**Phase:** 53-chart-config-dashboard-provenance-surfacing
**Mode:** `--auto` — all areas auto-selected; every question resolved to the
recommended option without AskUserQuestion. Each row below logs the
auto-selection for operator review.
**Areas discussed:** Chart config shape, Default posture semantics,
Install-vs-upgrade mechanism, Dashboard provenance surface, VerifyHalt visual
vocabulary

---

## Chart config shape + env flow (CFG-01)

| Option | Description | Selected |
|--------|-------------|----------|
| `subagent.verify` per-LEVEL block, JSON env for the defaults map | Extends the existing `subagent.levels` idiom; scalars ride discrete envs (`TIDE_VERIFIER_IMAGE` exists), the per-level map rides one JSON env (pricing-overrides precedent) | ✓ |
| Top-level `verification:` values block | Detaches the tier from the `subagent.levels`/`resolveImage` chain CFG-01 names | |
| Research Q6's per-stage `verify.stages.{planCheck,levelVerify,integrationCheck}` shape | Superseded three-stage vocabulary — pre-dates the five-loop reframe (Phase 52 = ONE contract per level) | |

**Auto-selection:** `[auto] Chart shape — Q: "values layout + env encoding?" → Selected: "subagent.verify per-level + JSON env" (recommended default)`
**Notes:** Evaluator image/model precedence locked as authored `Evaluator` > chart default > compiled default (D-02), replacing the verifier's current borrow-the-task-executor-model fallback only when config supplies one.

---

## Default posture semantics (CFG-02)

| Option | Description | Selected |
|--------|-------------|----------|
| Fresh install: task ON, milestone+project ON, plan/phase OFF; posture gates dispatch, not authoring | Requirement text names "Task-loop auto-repair" enabled; research's plan-off matches; boundedness from concurrency cap + budget + requireApproval | ✓ |
| Fresh install: milestone+project ONLY (task OFF) | PITFALLS §96's literal reading — but it pre-dates the Task-loop reframe; contradicts CFG-02/SC2 naming task auto-repair as enabled | |
| Gate contract authoring in planner templates instead of dispatch | Splits the off-switch across two surfaces; posture flips would require re-planning | |

**Auto-selection:** `[auto] Default posture — Q: "what does 'enabled at milestone+project scope' enable?" → Selected: "task+milestone+project ON, plan/phase OFF; dispatch-gated" (recommended interpretation, flagged in CONTEXT D-03 for cheap human veto)`
**Notes:** CFG-02's phrasing is genuinely ambiguous; the interpretation is isolated in D-03 so flipping it later touches one decision.

---

## Install-vs-upgrade mechanism (CFG-02)

| Option | Description | Selected |
|--------|-------------|----------|
| Sticky posture marker via signing-secret `lookup` + `resource-policy: keep` idiom + explicit `enabled|disabled|auto` override | The chart's own proven install-time-state mechanism; fresh install stays ON across later upgrades; pre-existing installs stay OFF | ✓ |
| Values default off + documented `--set` at install (research Q6's ServiceMonitor pattern) | Fails ROADMAP SC2 — a bare fresh `helm install` would get the tier OFF | |
| Bare `.Release.IsInstall` conditional | Fresh install's posture flips OFF on its first `helm upgrade` | |

**Auto-selection:** `[auto] Upgrade posture — Q: "how does the chart distinguish install from upgrade?" → Selected: "lookup+keep sticky marker" (recommended default)`
**Notes:** Upgrade-path test = helm-template render pair (plain vs `--is-upgrade`) + kind sticky proof (D-06). `helm template`'s IsInstall=true / nil-lookup behavior flagged as a research question for CI-render + helmify-verify safety.

---

## Dashboard provenance surface (OBS-04)

| Option | Description | Selected |
|--------|-------------|----------|
| Current `LoopStatus` summary in the existing task payload + findings via existing artifacts API + Phoenix deep-link; rendered in TaskDetailDrawer/NodeDetailPanel | No new endpoint (requirement), no etcd history (LOOP-03); history lives in artifacts + traces by design | ✓ |
| New provenance endpoint / dedicated top-level provenance view | Violates OBS-04's "no new endpoint"; a new view for summary-depth data over-builds | |
| Iteration-history timeline rendered from CRD status | The data does not exist in etcd — LOOP-03 forbids it structurally | |

**Auto-selection:** `[auto] Provenance surface — Q: "where does nested provenance render and from what source?" → Selected: "existing payload + artifacts + Phoenix links, drawer-scoped" (recommended default)`
**Notes:** `attemptMax` (from `Caps.Iterations`) must stay distinct from `maxIterations` in the UI — the infra-vs-quality split made visible.

---

## VerifyHalt visual vocabulary (OBS-04)

| Option | Description | Selected |
|--------|-------------|----------|
| Blocked-family ConditionBadge row + `Verifying`/`VerifyHalted` StatusBadge rows + server filter addition + distinctness test | Joins the BudgetBlocked/BillingHalt policy-halt family — distinct from Failed by color family, glyph, and label; SR text teaches `tide resume` | ✓ |
| Error-family (red/Failed-adjacent) styling for VerifyHalt | Contradicts the locked §C1 semantics — a halt is operator-recoverable policy state, not a failure; also weakens "distinct from Failed" | |

**Auto-selection:** `[auto] VerifyHalt vocabulary — Q: "which color family + which surfaces?" → Selected: "blocked-family, all four surfaces, with a distinctness test" (recommended default)`
**Notes:** Scout observed `FailureHalt` is also missing from the dashboard filter — recorded as deferred/discretionary, not OBS-04 scope.

---

## Claude's Discretion

- Exact values-key spellings, JSON env name/schema, `VerifyDefaults` struct home
- Posture-marker ConfigMap name/shape + override vocabulary
- Verifier-model env var name + defaults-map placement
- Icons/labels for the three new badge rows (within blocked-family constraint)
- Plan-check provenance depth this phase (full drawer section vs summary line)
- Whether to close the FailureHalt filter gap in the same table edit

## Deferred Ideas

- Broaden default posture to phase/plan levels (FEATURES.md:202 trigger)
- FailureHalt dashboard filter gap (unless closed at discretion)
- Dashboard mutation actions for halt recovery (read-only stays)
- Integration-check as a distinct rubric (research Q7 → future arc)
- Per-Project fine-grained disable knob on authored contracts
- Reviewed todos (not folded): signed-commits-verified-badge (SIGN-02/03/04, deferred by choice), cache-f1-direct-sdk-cross-pod-caching (vNext+)
