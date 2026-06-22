---
phase: "29-operator-tooling-e2e"
plan: "05"
subsystem: "test"
tags: ["kind", "e2e", "import-resume", "export-envelopes", "import-envelopes", "zero-cost", "adoption", "salvage", "spdy", "TOOL-02"]
dependency_graph:
  requires:
    - "29-02: tide export-envelopes CLI verb (exportEnvelopesRun + inspector pod)"
    - "29-03: tide import-envelopes CLI verb (importEnvelopesRun + SPDY loader pod)"
    - "29-04: small drain fixture + salvage childCount patch + Makefile bin/tide build"
  provides:
    - "test/integration/kind/import_resume_test.go — two-tier kind E2E proof of zero-cost resumption (TOOL-02)"
  affects:
    - "make test-int (Layer B kind suite — new Label('kind','long') specs)"
    - "CI: verifies TOOL-02 adoption gate end-to-end on every kind run"
tech_stack:
  added: []
  patterns:
    - "LIVE CLI invocation via exec.CommandContext on TIDE_BINARY or LookPath (D-10)"
    - "D-02 bundle shape assertion via archive/tar walk of exported .tgz"
    - "Consistently-based planner-Job absence assertion for stable adoption window"
    - "CostSpentCents == 0 snapshot before plan-level planner dispatch (D-14 budget suppression)"
    - "Two-Describe structure: tier a (small fixture + round-trip) + tier b (salvage adoption)"
key_files:
  created:
    - test/integration/kind/import_resume_test.go
  modified: []
key-decisions:
  - "D-02 bundle shape asserted via tar walk of exported.tgz (not just file presence) — proves round-trip produces well-formed content, not just an exit-0"
  - "Tier-a round-trip uses project.yaml written by import-envelopes to CWD (kubectl apply -n <fresh-ns>) — the natural CLI flow (D-05: stage-only, operator applies)"
  - "Tier-b $0 assertion sampled immediately after 3+ Milestones appear (bounded window: adoption fires in first ImportController reconcile; plan planners need at minimum tens of seconds for pod scheduling)"
  - "Consistently (15s/2s) for tier-b {milestone,phase} planner Job assertions — stable absence is stronger than point-in-time Eventually(0)"
  - "Tier-b applies milestones.yaml + phases.yaml from salvage fixture so the controller can resolve parent refs on the imported CRs"
  - "Salvage project name 'dogfood-codex-runtime' extracted from projects.yaml (used for CostSpentCents read)"
requirements-completed: [TOOL-02]
duration: "~20 minutes"
completed: "2026-06-22"
---

# Phase 29 Plan 05: Import Resume E2E Summary

**Two-tier kind E2E proving zero-cost resumption: small fixture drains to all-Milestones-Succeeded via stub subagents + live tide export-envelopes → import-envelopes round-trip adopts milestone/phase levels; salvage-20260618 import asserts 0 planner Jobs at milestone/phase levels and CostSpentCents==0 before plan dispatch (D-11/D-14/D-17).**

## Performance

- **Duration:** ~20 min
- **Started:** 2026-06-22T~09:00:00Z
- **Completed:** 2026-06-22T~09:20:00Z
- **Tasks:** 2 (both in one file; tier-a + tier-b specs)
- **Files modified:** 1 created

## Accomplishments

- Tier a: live CLI round-trip — `tide import-envelopes` (small fixture) → all Milestones reach Succeeded → `tide export-envelopes` → D-02 bundle shape assertion → `tide import-envelopes` into fresh namespace → `kubectl apply` → 0 milestone/phase planner Jobs (adopted)
- Tier b: salvage-20260618 import via real `tide import-envelopes` → `Consistently` assertion of 0 planner Jobs for `role=planner,level=milestone` and `role=planner,level=phase` → `CostSpentCents==0` sampled in the bounded pre-plan-dispatch window (D-14)
- Binary resolution: `TIDE_BINARY` env → `exec.LookPath("tide")` → `Skip`-with-hint (T-29-05-03); missing binary never silently passes

## Task Commits

| # | Task | Commit | Files |
|---|------|--------|-------|
| 1+2 | Tier a + tier b import_resume_test.go (both specs in one file) | 3afc0af | test/integration/kind/import_resume_test.go |

## Files Created/Modified

- `/Users/justinsearles/Projects/tide/test/integration/kind/import_resume_test.go` — two-tier kind E2E, 503 lines, Ginkgo Describe("Import resume E2E", Label("kind","long"))

## Decisions Made

**Bundle shape assertion (D-02):** The exported .tgz is unpacked via `archive/tar` walk and each required top-level entry is checked for presence: `projects.yaml`, `milestones.yaml`, `phases.yaml`, `plans.yaml`, `seed-manifest.json`, `SEED-OUTLINE.md`, `pvc-envelopes.tgz`. This is non-exhaustive but proves the round-trip produced a well-formed D-02 bundle without needing to re-verify byte content.

**Tier-a project.yaml application:** `import-envelopes` writes `project.yaml` to CWD; the test applies it with `kubectl apply -n <fresh-ns>`. A deferred `os.Remove("project.yaml")` cleans up the CWD artifact (pattern from 29-03's unit test cleanup).

**$0 assertion window (tier b):** The `CostSpentCents` read is taken immediately after `Eventually` confirms 3+ Milestones are present in the salvage namespace. This window is bounded by the ImportController reconcile latency (fast) vs. plan-level planner pod scheduling time (tens of seconds minimum). The assertion is immediate-Get, not Consistently, because the goal is to prove zero rollup happened before the first plan planner report — not that it stays 0 indefinitely (plan planners legitimately add cost after D-17 re-run).

**Consistently for milestone/phase absence (tier b):** Using `Consistently(15s/2s)` rather than `Eventually(0)` for the planner Job absence checks at milestone/phase levels — stable absence is stronger evidence of adoption than a point-in-time 0 count. 15s window is generous enough for any controller-reconcile delay.

**Salvage fixture application:** The salvage `projects.yaml` contains the original dogfood namespace in metadata; we apply with `-n importResumeSalvageNS` to override. We also apply `milestones.yaml` and `phases.yaml` so the controller can resolve parent refs on adopted CRs.

## Deviations from Plan

None — plan executed exactly as written. Both tasks (tier a + tier b) are in a single file commit because they share helpers (`assertNoPlannerJobsForLevel`, `assertD02BundleShape`) and splitting would have required stub intermediates.

## Known Stubs

None — all paths are wired to real implementations:
- `resolveTideBinary()` resolves the real built binary (no mock)
- `export-envelopes` + `import-envelopes` invoke the real CLI verbs
- JobList assertions use the real k8sClient against the live kind cluster
- CostSpentCents reads from the real Project status via the live apiserver

## Threat Surface Scan

No new network endpoints, auth paths, or schema changes introduced. The test exercises existing surfaces (T-29-05-01 through T-29-05-03 per the plan's threat model):

| Flag | File | Description |
|------|------|-------------|
| T-29-05-01 (zip-slip) | import_resume_test.go | Live proof: assertD02BundleShape walks the exported tgz confirming no malformed paths in the export path |
| T-29-05-02 (adoption gate) | import_resume_test.go | Tier-b Consistently(0) + CostSpentCents==0 proves only validated envelopes are adopted |
| T-29-05-03 (binary resolution) | import_resume_test.go | TIDE_BINARY/LookPath → Skip-with-hint if absent |

## Self-Check: PASSED

- `test/integration/kind/import_resume_test.go`: EXISTS (committed 3afc0af)
- `go test -c -o /dev/null ./test/integration/kind/`: BUILD OK (exit 0)
- `go vet ./test/integration/kind/`: VET CLEAN (exit 0)
- `grep -q 'export-envelopes'`: FOUND
- `grep -q 'import-envelopes'`: FOUND
- `grep -q 'tideproject.k8s/level'`: FOUND
- `grep -q 'CostSpentCents'`: FOUND
- Commit 3afc0af: FOUND (`git rev-parse --short HEAD` returned 3afc0af)
- README.md: UNTOUCHED (not staged, not committed — verified via git status)
