---
phase: 33
slug: d4-planner-failure-semantics
status: verified
threats_open: 0
asvs_level: 1
created: 2026-06-29
---

# Phase 33 — Security

> Per-phase security contract: threat register, accepted risks, and audit trail.

Phase 33 adds internal controller state-transition logic only — a pure in-process
predicate, two status-patch helpers, a guard branch on already-read envelope fields,
an API string constant, tests, and a comment-only chart edit. No new external API
surface, no new input parsing, no new network/disk I/O, no secret handling, no new
go.mod dependency. The register was authored at plan time (`register_authored_at_plan_time: true`);
all plan-time threats verified CLOSED against the shipped code (the short-circuit path —
no retroactive-STRIDE scan required).

---

## Trust Boundaries

| Boundary | Description | Data Crossing |
|----------|-------------|---------------|
| planner Job → controller (envelope read) | Pre-existing, unchanged. The controller reads `out` (ExitCode, ChildCount) from the planner's tiny-status envelope; the new guard only adds a classification branch on already-read fields. | Tiny-status envelope (exit code + child count); no secrets |
| (none new) | The helper, constant, fail helpers, and chart-comment edit introduce no new trust boundary. | — |

---

## Threat Register

| Threat ID | Category | Component | Disposition | Mitigation | Status |
|-----------|----------|-----------|-------------|------------|--------|
| T-33-01 | Tampering | `isPlannerFailure` predicate correctness | mitigate | `envReadOK && out.ExitCode != 0 && out.ChildCount == 0` (planner_failure.go:51) pins the misclassification risk; the `envReadOK &&` prefix prevents a zero-value envelope (read error → ExitCode==0) from firing. Pinned by 4-case unit table (`TestIsPlannerFailure`, green). | closed |
| T-33-02 | Tampering | charts/tide/values.yaml default | mitigate | Comment-only edit; default `plannerConcurrency: 4` confirmed unchanged (`^plannerConcurrency: 4$` present at values.yaml:89); `helm template` valid. | closed |
| T-33-03 | Denial of Service | phase/milestone reconcile loop | mitigate | `patchPhaseFailed`/`patchMilestoneFailed` patch a permanent `Failed` condition and return `ctrl.Result{}, nil` (NOT a Go error) → no controller-runtime exponential requeue / API-server storm. Recovery is operator-gated `tide resume --retry-failed` (no auto-retry). Pinned by PLANFAIL-04 recovery test. | closed |
| T-33-04 | Tampering | planning-DAG integrity | mitigate | The phase's purpose: a failed planner can no longer falsely advance its parent. Guard ordered before the succeed-check AND before the gate-policy hook (CR-01 fix, commit `7e475fc`) at phase_controller.go:589 / milestone_controller.go:661. Pinned by PLANFAIL-01/02 (fail fires, under the production approve gate) and PLANFAIL-03 (genuine leaf still Succeeds). | closed |
| T-33-05 | Spoofing | guard firing on transient read error | mitigate | A transient envelope-read error yields zero-value `out` (ExitCode==0); the `envReadOK &&` prefix prevents the guard from misfiring as a failure. Pinned by unit case `(ExitCode:1, ChildCount:0, envReadOK:false) → false`. | closed |
| T-33-NEWDEP | Tampering | go.mod / packages | accept | No new packages, no go.mod/go.sum change in Phase 33 (verified: no phase-33 commit touches go.mod/go.sum). Nothing to mitigate. | closed |

*Status: open · closed*
*Disposition: mitigate (implementation required) · accept (documented risk) · transfer (third-party)*

---

## Accepted Risks Log

| Risk ID | Threat Ref | Rationale | Accepted By | Date |
|---------|------------|-----------|-------------|------|
| AR-33-01 | T-33-NEWDEP | No new dependency is introduced by this phase, so there is no supply-chain surface to mitigate; recorded as an accepted (non-applicable) risk. | jsquirrelz | 2026-06-29 |

*Accepted risks do not resurface in future audit runs.*

---

## Security Audit Trail

| Audit Date | Threats Total | Closed | Open | Run By |
|------------|---------------|--------|------|--------|
| 2026-06-29 | 6 | 6 | 0 | /gsd:secure-phase (orchestrator, plan-time register + code verification) |

---

## Sign-Off

- [x] All threats have a disposition (mitigate / accept / transfer)
- [x] Accepted risks documented in Accepted Risks Log
- [x] `threats_open: 0` confirmed
- [x] `status: verified` set in frontmatter

**Approval:** verified 2026-06-29
