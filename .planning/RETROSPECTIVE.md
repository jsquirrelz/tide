# Project Retrospective

*A living document updated after each milestone. Lessons feed forward into future planning.*

## Milestone: v1.0.1 — Orchestrator Trustworthiness + Telemetry Completion

**Shipped:** 2026-06-13
**Phases:** 6 (12–17) | **Plans:** 38 | **Tasks:** 46 | **Commits:** 330

### What Was Built
- Gate semantics correctness: approve-at-descent (review the artifact before children spend), no level jumps past incomplete children, dispatch holds while parked, reject parks instead of fail-marking, `tide resume --retry-failed` as the one recovery verb.
- Image-resolution chain (`Levels.<level>.Image` → `Spec.Subagent.Image` → helm default) at all six dispatch sites — closing the v1.0 stub-image bug — plus a provider billing-400 project-wide `BillingHalt`.
- Budget visibility: current model IDs in the pricing table, `BudgetBlocked` on the Project + dashboard, reserve/settle accounting bounding in-flight overshoot, pricing-drift automation.
- Seven run-1 paper cuts (reporter CR labels, clean-tree push no-op, status-flapping convergence, real `artifact-get` inspector Pod, dashboard Complete chip, cross-plan running-waves view, strict file-touch overlap rejection).
- Telemetry foundation end-to-end (`PROM_ENDPOINT`→PromQL proxy, mounted TelemetryView, six locked metrics with labels, panel name alignment, Makefile gate, bounded proxy client).
- Audit tech-debt subset (Phase 17): Plan-level project-label self-heal, reject-before-reporter-spawn, narrowed approve guard, non-fatal envelope-read.

### What Worked
- **Findings-as-tests.** Every requirement carried an implicit acceptance criterion: a regression test that reproduces the dogfood run-1 symptom. Bugs were pinned to behavior, not implementation — and the run-killer (premature `Complete`) can't silently return.
- **Gap-closure waves caught real regressions before ship.** Phases 12, 13, 14, and 16 each spawned a gap-closure wave from their VERIFICATION/REVIEW artifacts rather than shipping on first-green.
- **The audit→closure loop.** The milestone audit held at `tech_debt` for a deliberate accept-or-cleanup decision; Phase 17 executed that decision (closed the in-scope subset, formally deferred the rest) instead of waving the items off.
- **Sibling-pattern mirroring.** Fixes that mirrored an already-shipped in-tree template (milestone/phase label backfill, reject short-circuit, Pitfall-1 non-fatal envelope read) were low-risk and consistent across reconcilers.

### What Was Inefficient
- **Executor under-reported `requirements_completed`.** 17-01/02/03 (and several earlier plans) left the SUMMARY frontmatter `requirements_completed` empty, forcing the auto-extraction to surface raw section headers and the audit to cross-check coverage manually against VERIFICATION file:line evidence. Same pattern flagged at v1.0.0 close — still unfixed.
- **ROADMAP.md was rewritten per-phase to only the active phase.** The milestone-wide phase list/detail was not retained in the live file, so the v1.0.1 archive had to be reconstructed from a prior git revision (`60a2841`) spliced with the Phase 17 block.
- **`milestone.complete` accomplishment extraction is naive** — it grabbed the first line of each SUMMARY (often "Task 1 —" or "One-liner:"), requiring a manual rewrite of the MILESTONES.md entry.

### Patterns Established
- **Dogfood finding → symptom-reproducing regression test** as the unit of trustworthiness work. The finding's run-1 symptom IS the test's red state.
- **Reject-before-reporter-spawn ordering** and **project-label self-heal backfill** are now the canonical shapes across the milestone/phase/plan reconcilers.
- **Audit `tech_debt` is a real gate**, not a rubber stamp — it routes to a closure phase that adjudicates each item IN-scope (fix) or DEFERRED (backlog), with rationale.

### Key Lessons
1. If `requirements_completed` frontmatter keeps coming back empty, the executor SUMMARY contract (or its template) needs a hard prompt — two milestones of manual cross-checking is a process smell, not a one-off.
2. The per-phase ROADMAP rewrite trades milestone-archive fidelity for context economy; the archive step must reconstruct from git. Consider retaining collapsed prior-phase entries in the live ROADMAP so close-out is mechanical.
3. Gate semantics that touch spend (approve/reject/resume) are worth a dedicated phase with a shared test-fixture and a live repro environment — the run-killer lived exactly here.

### Cost Observations
- Model profile: `quality` (`gsd-planner` → fable, `gsd-verifier` → opus, default opus). Per-session token mix not instrumented this milestone.
- Notable: the telemetry that would have measured this milestone's own cost (the six locked metrics) shipped *in* this milestone — next milestone can self-report.

---

## Cross-Milestone Trends

### Process Evolution

| Milestone | Phases | Plans | Key Change |
|-----------|--------|-------|------------|
| v1.0.0 | 14 dirs (1–11 + inserts) | 137 | Built the operator end-to-end; rc-gated release with $0 DinD dry-run |
| v1.0.1 | 6 (12–17) | 38 | Trustworthiness pass driven by dogfood run-1 findings; findings-as-regression-tests; audit→closure-phase loop |

### Recurring Friction (verify whether next milestone fixes it)

1. Executor leaves SUMMARY `requirements_completed` empty → manual coverage cross-check at audit (v1.0.0 and v1.0.1).
2. Administrative quick-task status fields never flipped → carried as deferred items at both closes (11 at v1.0.0, 15 at v1.0.1).

### Top Lessons (Verified Across Milestones)

1. The milestone audit earns its keep — it caught the dropped `podAnnotations` render block before v1.0 and routed the v1.0.1 tech-debt subset into a real closure phase. Don't skip it.
2. `make test-int` green ≠ ship-ready: read `MAKE_EXIT` and grep `^--- FAIL` — Ginkgo "SUCCESS!" can coexist with a red plain go-test in the same package.
