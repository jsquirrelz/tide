---
phase: 05-distribution-self-hosting-acceptance
plan: 02
subsystem: docs
tags: [contributing, security, oss-readiness, dco, conventional-commits, github-security-advisory, cosign-deferred]

# Dependency graph
requires:
  - phase: 01-foundation-crds-pkg-dag-controller-scaffold
    provides: Apache-2.0 Go file headers + Makefile test target inventory (test / test-int / test-e2e-kind / lint)
  - phase: 04.1-pre-v1-audit-fixes-cross-phase-uat-closeout
    provides: Cleanly-wired controller + closed audit-uat that v1.0 OSS readiness assumes
provides:
  - CONTRIBUTING.md at repo root (Go 1.26 / kubebuilder v4.14.0 / kind v0.31.0 / Helm 3.16.x prereqs; 4 make targets; branch-naming + Conventional Commits + DCO signoff)
  - SECURITY.md at repo root (GitHub Security Advisory primary channel; 48h ack SLA; controller + dashboard + CRDs + RBAC + chart + CLI + credproxy in-scope; LLM-key compromise + host K8s compromise + chart-signing out-of-scope with cosign-v1.x deferral)
affects:
  - phase 05 wave 4+ (docs reader-journey index docs/README.md may cross-link to CONTRIBUTING / SECURITY)
  - phase 05 release.yaml work (SECURITY.md cosign deferral aligned with .goreleaser.yaml supply-chain footer)
  - all post-v1 phases (DCO + Conventional Commits + branch-naming conventions now binding on contributors)

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Markdown doc audience/scope opener (PATTERNS.md §'Markdown doc audience/scope opener') applied to root docs, not just docs/"
    - "GitHub Security Advisory as the documented primary disclosure channel; placeholder email until a real mailbox is provisioned"

key-files:
  created:
    - CONTRIBUTING.md
    - SECURITY.md
  modified: []

key-decisions:
  - "Audience/Status/Scope opener convention (from docs/live-e2e.md and PATTERNS §'Markdown doc audience/scope opener') applied to BOTH root files, keeping the per-doc front-matter style consistent across docs/ and repo root."
  - "SECURITY.md primary channel is GitHub Security Advisory; the fallback email security@tide.example is documented as a stub to update once a real mailbox is provisioned. No fabricated real address."
  - "Cosign keyless signing deferral pulled forward into SECURITY.md Status block and Out of scope bullet, matching .goreleaser.yaml line 113 supply-chain footer."
  - "CONTRIBUTING.md routes Questions to GitHub Discussions (not Issues) so Issues stay actionable — matches the RESEARCH skeleton."
  - "CODE_OF_CONDUCT.md + GOVERNANCE.md remain DEFERRED per CONTEXT D-C5 / Deferred Ideas. Neither file mentions either deliverable, and no 'code of conduct' or 'governance' substring appears in either output."

patterns-established:
  - "OSS-readiness root docs (CONTRIBUTING / SECURITY) follow the same Audience/Status/Scope opener used by docs/*.md."
  - "Security disclosure scope is explicit (in-scope component list + out-of-scope list) — preempts later 'you should have covered X' disputes per T-05-02-02 mitigation."

requirements-completed: [DIST-04]

# Metrics
duration: ~22min
completed: 2026-05-22
---

# Phase 05 Plan 02: CONTRIBUTING.md + SECURITY.md Summary

**Two repo-root OSS-readiness docs landed: CONTRIBUTING.md (Go 1.26 / kubebuilder v4.14.0 / kind v0.31.0 / Helm 3.16.x prereqs, 4 canonical make targets, Conventional Commits + DCO signoff) and SECURITY.md (GitHub Security Advisory as primary channel, 48h ack SLA, in/out-of-scope sections with cosign-v1.x deferral documented).**

## Performance

- **Duration:** ~22 min
- **Started:** 2026-05-22T16:13:00Z (worktree spawn)
- **Completed:** 2026-05-22T16:36:45Z
- **Tasks:** 2 (both `type="auto"`)
- **Files created:** 2 (CONTRIBUTING.md 86 lines / 5277 bytes; SECURITY.md 53 lines / 4404 bytes)
- **Files modified:** 0

## Accomplishments

- **CONTRIBUTING.md** authored at repo root with 7 L2 sections (Prerequisites, Development workflow, Branch naming, Commit messages, DCO signoff, Pull request template, Issue triage). Pins toolchain to Go 1.26.x, kubebuilder v4.14.0, kind v0.31.0, Helm 3.16.x. Names the four canonical make targets (`make test`, `make test-int`, `make test-e2e-kind`, `make lint`) with one-line descriptions matching `Makefile` lines 81 / 145-147 / 138-139 / 200-202.
- **SECURITY.md** authored at repo root with 4 L2 sections (Reporting a vulnerability, Expected response time, In scope, Out of scope). Primary channel = GitHub Security Advisory link; 48h ack SLA + severity-based resolution timeline; in-scope list covers controller, dashboard, CRDs, RBAC, chart templates, CLI, credproxy; out-of-scope explicitly excludes third-party LLM key compromise, host K8s compromise, chart-signing (cosign v1.x deferral linked to `.goreleaser.yaml` supply-chain footer), and upstream Go-dep CVEs.
- **DIST-04 OSS-readiness sub-requirement (CONTRIBUTING + SECURITY) satisfied** — both files at repo root, both following the docs/live-e2e.md heading convention, both threaded into the project's existing decisions (D-C5 lock; T-05-02-01..03 STRIDE mitigations).

## Task Commits

Each task was committed atomically on `worktree-agent-ae5002a640a670f52`:

1. **Task 1: Author CONTRIBUTING.md** — `b6aebc1` (docs)
2. **Task 2: Author SECURITY.md** — `1f43bdb` (docs)

Plan metadata commit (this SUMMARY) will be created as the final docs commit after self-check.

## Files Created/Modified

- `CONTRIBUTING.md` — OSS contributor on-ramp; 7 L2 sections; pins Go/kubebuilder/kind/Helm versions; names `make {test, test-int, test-e2e-kind, lint}`; Conventional Commits + DCO signoff (`git commit -s`); routes Questions to GitHub Discussions.
- `SECURITY.md` — Vulnerability reporting policy; GitHub Security Advisory primary channel + placeholder email fallback; 48h ack + severity-tiered resolution; in/out-of-scope sections; cosign v1.x deferral acknowledged.

## Decisions Made

- **GitHub Security Advisory before email.** Chose the GitHub-native private-advisory flow as the primary disclosure channel and documented `security@tide.example` as a stub. Avoids inventing a real address (T-05-02-01 mitigation: "fallback email is placeholder, not a leakable real address").
- **Cosign deferral surfaced explicitly.** Added the cosign / SLSA provenance deferral to BOTH the SECURITY.md Status block and the Out of scope bullet, mirroring `.goreleaser.yaml` line 113 supply-chain footer. Pre-empts the "report: your chart is not cosign-signed" class of repudiation noise (T-05-02-02 mitigation).
- **Audience/Status/Scope opener at the root, not just under docs/.** Both root docs follow the same opener PATTERNS.md §"Markdown doc audience/scope opener" prescribes for `docs/*.md` — keeps the project's doc-style discipline consistent across the doc surface.
- **DCO signoff section names the verb form (`git commit -s`).** Acceptance criterion line 118 makes `git commit -s` literal-required; the section names the DCO link + the exact CLI verb in a fenced block to ensure both the gate and the human reader see the same canonical recipe.

## Deviations from Plan

None - plan executed exactly as written.

The Task 2 `<behavior>` block at PLAN line 144-145 wrote prose names "Scope (in)" / "Scope (out)", but the verification regex at line 165 binds the headings to `^## In scope` / `^## Out of scope`. Honored the verification regex (the deterministic gate) — this is consistent with both the heading convention and the acceptance criteria (no in-PLAN deviation, just regex-over-prose precedence inside a self-consistent task spec).

One small typo correction on SECURITY.md (`disclosess` → `discloses them`) was made post-Write and pre-commit; not tracked as a deviation since no committed content was changed.

## Issues Encountered

None.

## User Setup Required

None - no external service configuration required. CONTRIBUTING.md + SECURITY.md are pure documentation; the placeholder `security@tide.example` is documented as a stub for the maintainer to replace when a real mailbox is provisioned, but that replacement is not a Phase 5 gating step.

## Threat Model Compliance

The plan's `<threat_model>` defines three threat IDs (T-05-02-01 / T-05-02-02 / T-05-02-03). All three dispositions honored:

| Threat ID | Disposition | How honored |
|-----------|-------------|-------------|
| T-05-02-01 (Info Disclosure / reporting channel) | mitigate | GitHub Security Advisory documented as primary (private until disclosure); fallback email is a literal `security@tide.example` placeholder, NOT a real fabricated mailbox |
| T-05-02-02 (Repudiation / scope) | mitigate | In scope + Out of scope sections explicit; cosign signing deferral documented in BOTH Status block and Out of scope, removing "but you should have signed" ambiguity |
| T-05-02-03 (Tampering / DCO honor system) | accept | CONTRIBUTING.md DCO section is honor-system only (`git commit -s` trailer); no GPG enforcement added, consistent with CLAUDE.md anti-pattern guidance |

## Next Phase Readiness

- Wave 1 of Phase 5 progresses; the other Wave 1 plans (LICENSE, README Quickstart, docs/README.md index, INSTALL.md, etc.) are unaffected by this plan and continue in parallel under their own worktrees.
- DIST-04's other sub-requirements (3 sample Projects under `examples/projects/`) remain to be implemented in later waves; this plan covers only the OSS-doc subset.
- Post-v1 phases inherit the conventions documented here: Conventional Commits, branch-naming (`feat/`, `fix/`, `docs/`, `refactor/`), DCO signoff, GitHub Security Advisory for vuln reports. No further phase needs to redocument these rules.
- The placeholder `security@tide.example` is a known-stub. Replace before public v1.0 announcement once a real mailbox is in place; otherwise leave as-is — the GitHub Security Advisory channel is fully functional today.

## Self-Check

**Files verified to exist on disk:**

- `CONTRIBUTING.md` (86 lines, 5277 bytes) — FOUND
- `SECURITY.md` (53 lines, 4404 bytes) — FOUND
- `.planning/phases/05-distribution-self-hosting-acceptance/05-02-SUMMARY.md` — this file, FOUND

**Commits verified to exist in `git log --all`:**

- `b6aebc1` docs(05-02): add CONTRIBUTING.md (D-C5) — FOUND
- `1f43bdb` docs(05-02): add SECURITY.md (D-C5) — FOUND

**Plan verification block (PLAN line 201) re-run:**

`grep -lE 'TIDE Security Policy|Contributing to TIDE' CONTRIBUTING.md SECURITY.md` returns both files — PASSED.

**All 13 CONTRIBUTING.md acceptance criteria + 10 SECURITY.md acceptance criteria + 2 verification regex chains:** PASSED (full output captured in the worktree session).

## Self-Check: PASSED

---
*Phase: 05-distribution-self-hosting-acceptance*
*Plan: 02*
*Completed: 2026-05-22*
