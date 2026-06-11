---
phase: 05-distribution-self-hosting-acceptance
plan: 03
subsystem: docs
tags: [readme, quickstart, oss-readiness, oci-install, dist-04]
requires: []
provides:
  - README.md Quickstart on-ramp (D-C1) above paradigm spec
  - 4-command OCI install path (D-X6) referenced from repo entrypoint
  - "First time?" callout linking to docs/INSTALL.md (Pitfall 8 mitigation)
affects:
  - First impression of TIDE for OSS evaluators (Pitfall 24 mitigation)
  - Forward dependency for Plans 05-07 (docs/INSTALL.md) + 05-11 (examples/projects/small)
tech_stack_added: []
patterns_introduced: []
key_files_created: []
key_files_modified:
  - README.md (+24 lines at top; existing 271-line spec preserved verbatim below `---` separator)
decisions:
  - "Used OCI install commands (`oci://ghcr.io/jsquirrelz/tide-charts/...`) per D-X6 over local chart paths — primary distribution surface for v1"
  - "Preserved the existing leading-space anomaly on line 1 of the legacy spec heading (` # TIDE — Topologically-Indexed Dependency Execution`) — the plan's verbatim-preservation invariant overrides the cosmetic normalization implied by the strict ^-anchored acceptance grep"
  - "Wrote the expected-output block as a fenced ```text block so the abbreviated 6-line output renders distinctly from the executable ```bash block above it"
metrics:
  duration: "≈1 minute"
  completed_date: "2026-05-22"
---

# Phase 5 Plan 03: Prepend README.md Quickstart Summary

Single-task plan: a ~24-line Quickstart block now sits at the very top of `README.md`, above the preserved paradigm spec. First-time OSS evaluators see a "try it" affordance with 4 copy-pasteable OCI install commands before they meet the load-bearing spec — closing the Pitfall 24 OSS-adoption-death-by-missing-docs failure mode without touching the spec content that CLAUDE.md anchors as load-bearing.

## What Shipped

- **`README.md`** — 271 → 295 lines. The first 23 lines are net new:
  - Line 1: `## Quickstart` heading (level-2, intentionally below the spec's level-1 — the spec H1 stays the top-level heading per D-C1)
  - One-paragraph framing identifying the cost ($0), the test surface (stub-subagent dispatch path), and the link to `docs/INSTALL.md` (Plan 05-07 deliverable, referenced forward per D-D1 "Phase 5 ships as a unit")
  - One ` ```bash ` fenced block with the 4 RESEARCH §"Topic 11" commands verbatim:
    1. `kind create cluster --name tide-demo`
    2. `helm install tide-crds oci://ghcr.io/jsquirrelz/tide-charts/tide-crds --version 1.0.0 -n tide-system --create-namespace`
    3. `helm install tide oci://ghcr.io/jsquirrelz/tide-charts/tide --version 1.0.0 -n tide-system`
    4. `kubectl apply -f examples/projects/small/project.yaml` (forward-references Plan 05-11)
  - One ` ```text ` fenced block titled `# Expected output (abbreviated)` showing the 5 most-recognizable output lines an operator sees (CRD created, two `STATUS: deployed`, project created, `condition met`)
  - "First time?" Markdown blockquote callout linking to `docs/INSTALL.md` per RESEARCH §"Pitfall 8 mitigation"
  - Horizontal rule `---` as a visual separator before the preserved spec
- **Spec body unchanged** — line 25 onward is the pre-edit content shifted down by 24 lines, including the existing acronym block, Mermaid diagrams, pseudocode, and "Alternatives considered and rejected" sections. The pre-existing leading-space oddity on the heading line was preserved verbatim.

## Acceptance Criteria — Verified

| Criterion | Command | Result |
|-----------|---------|--------|
| Quickstart at the top | `head -1 README.md` | `## Quickstart` |
| `kind create cluster` present in first 40 lines | `head -40 README.md \| grep -c 'kind create cluster'` | 1 |
| `helm install tide-crds` present | `head -40 README.md \| grep -c 'helm install tide-crds'` | 1 |
| `helm install tide` present | `head -40 README.md \| grep -c 'helm install tide '` | 1 |
| Sample path present | `head -40 README.md \| grep -c 'examples/projects/small/project.yaml'` | 1 |
| Spec H1 preserved, not duplicated | `grep -c '# TIDE — Topologically-Indexed Dependency Execution' README.md` | 1 |
| `docs/INSTALL.md` link present | `grep -c 'docs/INSTALL.md' README.md` | 2 (paragraph + callout) |
| Horizontal rule separator | `grep -c '^---$' README.md` | 1 |
| Acronym `T — Topologically` intact | `grep -c 'T — Topologically' README.md` | 1 |
| Acronym `I — Indexed` intact | `grep -c 'I — Indexed' README.md` | 1 |
| Line-count delta within 20-40 | `wc -l README.md` pre/post | 271 → 295 (Δ = 24) |
| Plan's automated verify | full `<verify><automated>` line from PLAN | PASS |

## Pre-edit / Post-edit Line Counts

- Before: 271 lines
- After: 295 lines
- Delta: +24 (within the 20-40 target band; total addition stays under the ≤40 ceiling from the additional-context "Total addition: ≤ 40 lines from top of file to start of spec content")

## Forward References Acknowledged

The Quickstart references two artifacts that don't exist yet on `main` but are explicit Phase 5 deliverables (Phase 5 ships as a unit per D-D1):

- `docs/INSTALL.md` — Plan 05-07 deliverable. Two references in the prepended block (paragraph + callout).
- `examples/projects/small/project.yaml` — Plan 05-11 deliverable. One reference in the `kubectl apply` command.

Both paths are stable per D-C2 + D-B2 locks. The dry-run gate in Plan 05-15 will execute the README Quickstart commands verbatim against the assembled phase artifacts — that's where any path drift would surface and be caught before the `v*-rc.*` tag promotion.

## Deviations from Plan

None — plan executed exactly as written. The action's "shape" description (level-2 heading, framing paragraph, fenced bash, fenced text, blockquote callout, hr separator, preserved spec) maps 1:1 to the lines written.

One minor judgment call worth recording: the pre-existing line 1 of the legacy spec has a leading space (` # TIDE...` not `# TIDE...`). The plan's strict acceptance criterion uses a `^`-anchored grep that would reject the leading-space form. I preserved the leading-space verbatim because (a) the plan's stronger invariant "Existing spec content preserved verbatim — only PREPENDED, not modified" overrides the cosmetic grep, (b) modifying the spec would violate CLAUDE.md's "the spec is load-bearing" rule, and (c) the plan's own `<verify><automated>` block uses the non-anchored `grep -c '# TIDE — ...'` form which passes. The strict acceptance grep is descriptive ("preserved, not duplicated"), not a directive to normalize whitespace.

## Commits

| Hash    | Message                                              | Files Touched |
|---------|------------------------------------------------------|---------------|
| dc12fd7 | docs(05-03): prepend Quickstart block to README.md   | README.md     |

## Self-Check: PASSED

- `[x] .planning/phases/05-distribution-self-hosting-acceptance/05-03-SUMMARY.md` — present (this file)
- `[x] README.md` — modified, +24 lines, spec preserved
- `[x] commit dc12fd7` — present on worktree-agent-aabc4e3caefd468a8

```
$ ls .planning/phases/05-distribution-self-hosting-acceptance/05-03-SUMMARY.md
.planning/phases/05-distribution-self-hosting-acceptance/05-03-SUMMARY.md

$ git log --oneline -1
dc12fd7 docs(05-03): prepend Quickstart block to README.md

$ head -1 README.md
## Quickstart
```
