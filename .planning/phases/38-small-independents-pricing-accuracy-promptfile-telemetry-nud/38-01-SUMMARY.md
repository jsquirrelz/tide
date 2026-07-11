---
phase: 38-small-independents-pricing-accuracy-promptfile-telemetry-nud
plan: 01
subsystem: subagent
tags: [pricing, prompt-caching, credproxy, minikube, testdata, cost-tally]
requires: []
provides:
  - "COST-03 probe verdict artifact 38-01-PROBE-RESULT.md — verdict: cacheWriteMultiplier = 125/100 (5m TTL, not mixed)"
  - "internal/subagent/anthropic/testdata/first_run_2026-07-03_usage.json — zero-recovery fixture (dispatches: []) with honest provenance of the PVC evidence loss"
affects: [38-06]
tech-stack:
  added: []
  patterns: ["operator-evidence gate artifacts recorded in-repo before dependent code ships (D-07)"]
key-files:
  created:
    - internal/subagent/anthropic/testdata/first_run_2026-07-03_usage.json
    - .planning/phases/38-small-independents-pricing-accuracy-promptfile-telemetry-nud/38-01-PROBE-RESULT.md
  modified: []
key-decisions:
  - "D-04 fallback invoked with zero recovery: the tide-cashboard namespace (and its PVC, reclaimPolicy Delete) was deleted after the 2026-07-03 run — per-dispatch token counts are unrecoverable; fixture ships dispatches: [] with full provenance, no fabricated counts"
  - "COST-03 probe run 2026-07-11 by the orchestrator with the operator's explicit permission, superseding the D-05/D-06 operator-only lock for this run; vehicle was the HOST CLI 2.1.207 with the production flag set (not the subagent image's pinned CLI) — caveat recorded in the artifact"
  - "Verdict: cacheWriteMultiplier = 125/100 — only cache_control shape observed was {\"type\":\"ephemeral\"} (no ttl key); unambiguous, no mixed-ttl flag"
patterns-established: []
requirements-completed: [COST-03, COST-01]
coverage:
  - id: D1
    description: "2026-07-03 first-run usage fixture preserved in testdata with honest provenance (zero-recovery outcome — PVC evidence destroyed before export)"
    requirement: "COST-01"
    verification:
      - kind: other
        ref: "jq -e '.provenance and (.dispatches | type == \"array\")' internal/subagent/anthropic/testdata/first_run_2026-07-03_usage.json"
        status: pass
    human_judgment: true
    rationale: "The automated check proves schema validity, not the export outcome. The zero-recovery result (dispatches: []) changes what plan 38-06's run-mix regression can assert — a human should acknowledge the evidence grade."
  - id: D2
    description: "COST-03 probe verdict recorded as the D-08 evidence artifact with exactly one machine-greppable verdict line (125/100, 5m TTL)"
    requirement: "COST-03"
    verification:
      - kind: other
        ref: "grep -cE '^verdict: cacheWriteMultiplier = (125|200)/100$' .planning/phases/38-small-independents-pricing-accuracy-promptfile-telemetry-nud/38-01-PROBE-RESULT.md == 1"
        status: pass
    human_judgment: false
duration: 15min
completed: 2026-07-11
status: complete
---

# Phase 38 Plan 01: Operator-Evidence Gates (COST-01 export + COST-03 probe) Summary

**COST-03 probe verdict `cacheWriteMultiplier = 125/100` (5m ephemeral TTL, host CLI 2.1.207, 2026-07-11) recorded as the D-08 artifact; the 2026-07-03 usage export came back empty — the tide-cashboard PVC was deleted post-run, so the fixture ships zero dispatches with full loss provenance.**

## Performance

- **Duration:** ~15 min active (plus checkpoint pause for the operator-authorized probe)
- **Started:** 2026-07-11T12:44:15Z
- **Completed:** 2026-07-11T12:58:00Z
- **Tasks:** 3 (2 auto + 1 human-action checkpoint)
- **Files modified:** 2 created

## Accomplishments

- **Probe verdict (COST-03):** `verdict: cacheWriteMultiplier = 125/100` — every `cache_control` object in the teed request bodies was `{"type":"ephemeral"}` with no `ttl` key (5-minute TTL). Not mixed; no `mixed-ttl` flag. Plan 38-06's pricing rows are unblocked per D-07, and D-08's constant comment can cite `38-01-PROBE-RESULT.md` verbatim.
- **Export outcome (COST-01/D-04): zero recovery — none.** The `tide-cashboard` namespace was deleted after the 2026-07-03 run, destroying its PVC (reclaimPolicy `Delete`) and the envelope tree. Verified exhaustively: `tide-projects` PVC empty, node-wide `find` for envelopes returned nothing, no CRD objects survive, no host-side export exists. The fixture was committed with `dispatches: []` and provenance documenting the loss plus the run-day aggregates ($10.86 TIDE tally vs $3.84 console; prose model mix) from the 2026-07-03 pricing todo.

## Task Commits

Each task was committed atomically:

1. **Task 1: Export the 2026-07-03 first-run envelope usage into testdata** - `a7cf915` (chore)
2. **Task 2: COST-03 probe (checkpoint:human-action)** - no commit (operator-authorized probe run by the orchestrator; evidence recorded by Task 3)
3. **Task 3: Record the probe verdict as the D-08 evidence artifact** - `1bea2eb` (docs)

## Files Created/Modified

- `internal/subagent/anthropic/testdata/first_run_2026-07-03_usage.json` - Zero-recovery usage fixture: `provenance` string documenting the full export attempt and loss; `dispatches: []`
- `.planning/phases/38-small-independents-pricing-accuracy-promptfile-telemetry-nud/38-01-PROBE-RESULT.md` - COST-03 probe report (verbatim), host-CLI evidence-grade caveat, and the single verdict line `verdict: cacheWriteMultiplier = 125/100`

## Decisions Made

- **D-05/D-06 lock superseded for this run:** the operator explicitly delegated the probe ("You have permission to run the recipe above") after updating `~/.tide/secrets`; the orchestrator ran it. Recorded in the artifact's provenance.
- **Probe vehicle deviation from D-05's ideal:** host CLI 2.1.207 via direct `claude -p --bare` with the production flag set (subagent.go:285-294), not the subagent image's pinned CLI — the tide-spike wrapper hardcodes a root-owned `NODE_EXTRA_CA_CERTS` path. Caveat recorded; re-probe via the docker-image variant if the pinned CLI ever diverges.
- **No fabricated token counts:** D-04 forbids reconstruction without evidence; the fixture honestly ships zero dispatches rather than encoding the prose model mix as fake per-dispatch entries.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking] minikube cluster was stopped**
- **Found during:** Task 1 (envelope export)
- **Issue:** `kubectl config use-context minikube` failed — no context existed; `minikube status` showed the node Stopped
- **Fix:** `minikube start` (non-destructive restart of the existing profile); cluster and kubeconfig restored
- **Verification:** manager pod visible in `tide-system`; PVC inspectable
- **Committed in:** n/a (environment action, no repo change)

**2. [Rule 3 - Blocking] Manager image is distroless — recipe's `kubectl exec`/`cp` impossible**
- **Found during:** Task 1 (envelope export)
- **Issue:** `kubectl exec $MGR -- ls /workspaces` failed with `exec: "ls": executable file not found` — the RESEARCH recipe assumed an exec-able manager pod
- **Fix:** created a short-lived busybox helper pod mounting the `tide-projects` PVC read-only, inspected via it, deleted it afterward
- **Verification:** PVC contents listed (empty); helper pod deleted
- **Committed in:** n/a (cluster-side only, no repo change)

---

**Total deviations:** 2 auto-fixed (both Rule 3 - blocking)
**Impact on plan:** Both were environment blockers on the export path; neither changed scope. The zero-recovery outcome itself is the plan's own pre-authorized D-04 fallback, not a deviation.

## Issues Encountered

- **The 2026-07-03 evidence was already destroyed before this plan ran.** Root cause: the `tide-cashboard` namespace (holding the per-project PVC) was deleted post-run; `reclaimPolicy: Delete` wiped the envelope tree. RESEARCH Pitfall 2 ("perishable PVC") materialized in full. The plan's fallback handled it; the loss is disclosed in both artifacts.

## User Setup Required

None - no external service configuration required. (Note: minikube was left running after the probe.)

## Next Phase Readiness

- **Plan 38-06 is unblocked per D-07:** verdict is unambiguous 125/100; the `cacheWriteMultiplier` constant keeps the current 1.25× literals (zero numeric change) with the probe citation (D-08).
- **Impact on 38-06's run-mix regression fixture:** `first_run_2026-07-03_usage.json` has `dispatches: []` — the run-mix regression test cannot assert per-dispatch replay against real counts. 38-06 should either assert against the documented run-level aggregates cited in the provenance ($10.86 old-table tally vs $3.84 console) or restate the fixture's role; do not fabricate dispatch entries.
- **Evidence-grade caveat for D-08's comment:** the probe observed the host CLI 2.1.207, not the subagent image's pinned CLI (same production flag set).

## Self-Check: PASSED

- FOUND: `internal/subagent/anthropic/testdata/first_run_2026-07-03_usage.json`
- FOUND: `.planning/phases/38-small-independents-pricing-accuracy-promptfile-telemetry-nud/38-01-PROBE-RESULT.md`
- FOUND: commit `a7cf915` (Task 1)
- FOUND: commit `1bea2eb` (Task 3)
- `jq -e '.provenance and (.dispatches | type == "array")'` → true
- `grep -cE '^verdict: cacheWriteMultiplier = (125|200)/100$'` → 1

---
*Phase: 38-small-independents-pricing-accuracy-promptfile-telemetry-nud*
*Completed: 2026-07-11*
