---
phase: 05-distribution-self-hosting-acceptance
plan: 07
subsystem: docs
type: execute
wave: 2
tags: [docs, install, on-ramp, dist-04]
status: complete
completed: 2026-05-21

dependency_graph:
  requires:
    - 05-05 (chart version 1.0.0 lockstep bump — cited explicitly in INSTALL.md helm commands)
  provides:
    - "docs/INSTALL.md — single-source-of-truth operator install on-ramp (D-C2 lock)"
    - "Forward-link target for README Quickstart and docs/README.md index entry #1"
  affects:
    - "Plan 05-08 (docs/project-authoring.md) — INSTALL.md forward-links to it as the next step after install"
    - "Plan 05-10 (docs/troubleshooting.md) — INSTALL.md forward-links to it for canonical install failure recipes"
    - "Plan 05-15 (dry-run verification) — INSTALL.md OCI install commands are the verified install path"

tech_stack:
  added: []
  patterns:
    - "docs/git-hosts.md per-platform recipes shape (PATTERNS §P2.3 analog) reused for per-OS prereq install sections"
    - "docs/live-e2e.md Audience/Status/Scope opener convention reused for INSTALL.md opener"

key_files:
  created:
    - "docs/INSTALL.md (244 lines, 8 level-2 sections, 8 level-3 sections, ~1.7k words)"
  modified: []

decisions:
  - "Lifted helm OCI commands verbatim from RESEARCH §Pitfall 4 / CONTEXT §D-X6 — `oci://ghcr.io/jsquirrelz/tide-charts/{tide-crds,tide} --version 1.0.0`"
  - "Pinned per-OS prereq versions (kubectl 1.31.0, helm 3.16.3, kind v0.31.0) to match the dry-run baseline (D-D2) so local installs equal the install path CI exercises on every v*-rc.* release"
  - "Threaded the 'small sample uses stub-subagent — no API key needed' point into Prerequisites, Provider Secret, and First Project Apply sections so first-time readers don't think a key is required to feel TIDE end-to-end"
  - "Added an explicit Verifying the install subsection inside Install order so operators get a single copy-pasteable verification block rather than scattered checks"

metrics:
  duration_minutes: 13
  files_changed: 1
  lines_added: 244
  lines_deleted: 0
  tasks_completed: 1
  commits: 1
---

# Phase 05 Plan 07: docs/INSTALL.md (operator on-ramp) Summary

**One-liner:** Landed `docs/INSTALL.md` as the single-source-of-truth install on-ramp for v1.0 — prerequisites + per-OS install + CRDs-first install order (Pitfall 4 mitigation) + Secret bootstrap + first-Project apply + "Is TIDE for me?" 3+3 framing (Pitfall 8 mitigation), all cross-linked into the rest of the docs surface.

## What landed

**1 file created — `docs/INSTALL.md`** (244 lines, 8 level-2 sections, 8 level-3 sections).

Section inventory (level-2):

1. `## Prerequisites` — version-pinned table (kubectl 1.31+, helm 3.16.x, kind v0.31.0, Docker 24.x) + explicit note that the small sample uses the stub-subagent and needs no API key.
2. `## Per-OS prerequisite install` — three level-3 subsections (macOS via brew, Linux via curl/apt with versions pinned to RESEARCH §P7.1, Windows via WSL2 reusing the Linux recipes).
3. `## Install order (Pitfall 4 — CRDs first)` — explicit ordering rule + rationale, then two level-3 subsections: Quickstart (OCI registry — primary path) and Cloned-repo install path, then a third Verifying the install subsection with the CRD + controller + dashboard checks.
4. `## Provider Secret (ANTHROPIC_API_KEY)` — `kubectl create secret generic` pattern via `$ANTHROPIC_API_KEY` env (never inline YAML, per T-05-07-02 mitigation).
5. `## Git credentials Secret` — same `kubectl create secret generic` shape via `$GIT_PAT`; forward-link to `docs/git-hosts.md` for per-host PAT scoping.
6. `## First Project apply` — `kubectl apply -f examples/projects/small/project.yaml` + `kubectl wait --for=jsonpath='{.status.phase}'=Complete` with expected output; this is the dry-run gate's timer-stop signal.
7. `## Is TIDE right for me?` — Pitfall 8 mitigation; two level-3 subsections (Yes, if: / No, if:) with three bullets each (six bullets total) covering K8s posture, team-size threshold, batch-vs-interactive workload, observability tolerance.
8. `## Next steps` — forward links to `docs/{project-authoring,dashboard,cli,gates,observability,rbac,troubleshooting,git-hosts,live-e2e}.md`.

## Acceptance criteria verification

All criteria from `<acceptance_criteria>` passed (verified post-commit):

| Check | Expected | Actual |
|-------|----------|--------|
| `test -s docs/INSTALL.md` | exit 0 | PASS |
| `head -1` | `# TIDE Install Guide` | PASS |
| Level-2 section count | ≥ 7 | 8 |
| Level-3 section count | ≥ 5 | 8 |
| Install-order grep (case-insensitive) | match | PASS — `tide-crds BEFORE tide` + `CRDs first` both present |
| `macOS` present | yes | PASS |
| `Linux` present | yes | PASS |
| `WSL2` present | yes | PASS |
| `ANTHROPIC_API_KEY` present | yes | 7 occurrences |
| `Is TIDE right for me` present | yes | 2 occurrences (section heading + scope bullet) |
| `oci://ghcr.io/jsquirrelz/tide-charts/tide` present | yes | 2 (tide-crds + tide install lines) |
| `--version 1.0.0` present | yes | 3 (OCI tide-crds + OCI tide + Prerequisites cross-reference) |
| `project-authoring.md` present | yes | 3 occurrences |
| `troubleshooting.md` present | yes | 3 occurrences |
| Line count between 100–400 | yes | 244 |

The combined `<verify>` automation block exits 0.

## Deviations from Plan

None — plan executed exactly as written. The level-3 count came in at 8 (plan minimum: 5) because the Install order section ended up needing three subsections (Quickstart OCI, Cloned-repo install path, Verifying the install) for the verification block to live cleanly inside the install flow rather than as a standalone level-2 section; this is additive, not a structural deviation.

## Threat-model mitigations applied

| Threat ID | Disposition | How mitigated in INSTALL.md |
|-----------|-------------|------------------------------|
| T-05-07-01 (Tampering — install order doc) | mitigate | Dedicated level-2 section `## Install order (Pitfall 4 — CRDs first)` with bold rationale paragraph + acceptance grep enforces presence; Plan 05-15 dry-run will execute this path live |
| T-05-07-02 (Info disclosure — ANTHROPIC_API_KEY) | mitigate | Instructions use `export ANTHROPIC_API_KEY=...` + `kubectl create secret generic --from-literal=ANTHROPIC_API_KEY="$ANTHROPIC_API_KEY"` — explicit warning "Never paste the raw key into a YAML manifest"; controller-pod-never-sees-key argument lifted from git-hosts.md threat model |
| T-05-07-03 (Info disclosure — git PAT) | mitigate | Same `$GIT_PAT` env + `--from-literal` pattern; forward-link to `docs/git-hosts.md` for per-host minimally-scoped PAT recipes (fine-grained GitHub tokens, GitLab `write_repository` scope, Gitea per-repo tokens) |
| T-05-07-04 (Tampering — chart version drift) | mitigate | All three `--version 1.0.0` occurrences (OCI tide-crds, OCI tide, Prerequisites cross-reference) hardcode the version; acceptance grep on `--version 1.0.0` ensures INSTALL.md does not silently drift from Plan 05-05's chart bump |

## Known Stubs

None — no placeholder/coming-soon language in the file.

## Commit log

| Task | Commit | Files | Summary |
|------|--------|-------|---------|
| Task 1 | `fc42713` | docs/INSTALL.md (created) | Author docs/INSTALL.md as the operator on-ramp (DIST-04) |

Full SHA: `fc4271307f5431689157a3ff43b26511fd586a60`

## Self-Check: PASSED

- `[x] docs/INSTALL.md` exists at the expected path (verified via `test -s`).
- `[x] Commit fc42713` exists in `git log` on the worktree branch `worktree-agent-a85da4c171cd20aff`.
- `[x]` 244 lines, 8 level-2 sections, 8 level-3 sections — all within plan target ranges.
- `[x]` All 15 `<acceptance_criteria>` checks pass.
- `[x]` All four `<threat_model>` mitigations applied with traceable rationale.

## DIST-04 status

Plan 05-07 delivers the `INSTALL.md` slice of REQ-DIST-04. The remaining DIST-04 slices (project-authoring.md, troubleshooting.md, rbac.md, docs/README.md index) ship in Plans 05-08 through 05-10 + the docs index plan. `bash hack/scripts/verify-docs-coverage.sh --require-all-files` will continue to exit non-zero until those plans land — but the `docs/INSTALL.md` row no longer fails (the file is non-empty per acceptance check).
