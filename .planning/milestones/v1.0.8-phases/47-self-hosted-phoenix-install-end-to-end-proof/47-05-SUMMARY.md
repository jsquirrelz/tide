---
phase: 47-self-hosted-phoenix-install-end-to-end-proof
plan: 05
subsystem: observability
tags: [phoenix, evidence, proof, screenshots, redaction, deep-link]

# Dependency graph
requires:
  - phase: 47-04 (wave 3)
    provides: live tide-phoenix-proof cluster + completed medium-project run + trace e9124906f6ee4aeba650a6fdd93b86fd in auth-ON Phoenix + 47-PROOF-RUNLOG.md
provides:
  - PROOF-01 milestone-close evidence set — four browser-captured PNGs + 47-EVIDENCE.md (trace IDs, exact DSL query, redaction verification, known-limitations honesty, named defects)
  - Live validation of the OBS-04 deep link (dashboard → /redirects/spans/{id} → correct span) and the enrichment-filter DSL queryability on Phoenix 18.1.0
  - Two live-only defect findings preserved with diagnostic data for gap closure (partial enrichment coverage; push-lease status flap made visible on the dashboard)
affects: [phase-close, gap-closure, milestone-close]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Orchestrator self-capture (Phase 22/26 precedent): the executor subagent has no browser tools, so the orchestrator session drove chrome-devtools directly — the plan's own capture-mechanism note anticipated this"
    - "Phoenix tree-search filter ('tide.dispatch') collapses a 392-span trace to the six-AGENT dispatch chain with hierarchy indentation — the cleanest single-frame five-level view"
    - "Authenticated evidence access via a dedicated capture ADMIN user minted through Phoenix's API (47-04's session held the only admin credential); credentials session-scoped, outside the repo"

key-files:
  created:
    - .planning/phases/47-self-hosted-phoenix-install-end-to-end-proof/47-EVIDENCE.md
    - .planning/phases/47-self-hosted-phoenix-install-end-to-end-proof/47-evidence-trace-tree.png
    - .planning/phases/47-self-hosted-phoenix-install-end-to-end-proof/47-evidence-llm-span-redacted.png
    - .planning/phases/47-self-hosted-phoenix-install-end-to-end-proof/47-evidence-query.png
    - .planning/phases/47-self-hosted-phoenix-install-end-to-end-proof/47-evidence-deeplink.png
  modified: []

key-decisions:
  - "Tree + deeplink PNGs are labeled composites of two live frames each (disclosed in 47-EVIDENCE.md) — the UI cannot show all five levels plus LLM children, or a click-through's both ends, in one viewport"
  - "No redaction/elision markers appear in the LLM-span capture because none were warranted: zero secret-pattern matches in the run's content and max message attribute 21,573 B < 32 KiB cap — pass-through is the boundary's correct output, proven safe by the 0-hit key search over all 392 spans"
  - "Task 3 human-verify checkpoint resolved under auto-mode via its named-gaps branch: shortfalls converted to named gaps per D-14 (see Gaps below); evidence committed for asynchronous human review"
  - "Cluster decision (auto-selected, reversible): KEEP tide-phoenix-proof + Phoenix running — Defect #2's root-cause needs the reporter Job logs on that cluster"

# Commits
commits:
  - hash: bf83608
    message: "docs(47-05): capture PROOF-01 evidence — four screenshots + evidence record"
---

# Plan 47-05 Summary — PROOF-01 Evidence Capture

One-liner: the live run's five-level trace tree (392 spans), redaction posture, enrichment queryability, and dashboard deep link are captured as four honest screenshots + an evidence record; two real defects the live proof surfaced are named with diagnostic data and routed to gap closure.

## Tasks

| # | Task | Status |
|---|------|--------|
| 1 | Browser-driven capture (tree, LLM span, query, deep link) + redaction spot-check | ✓ (automated gate: 4 valid PNGs; key-prefix search 0 hits across all 392 spans) |
| 2 | 47-EVIDENCE.md (trace IDs, query, observations, known limitations) | ✓ (automated gates green: 32-hex ID present, Known limitations present, zero key-prefix strings, 4 screenshots indexed) |
| 3 | Human review vs the PROOF-01 bar | ⚡ auto-mode: resolved via the checkpoint's named-gaps branch (defects converted to gaps per D-14); evidence awaits asynchronous human review |

## Deviations

- Executed inline by the orchestrator (browser tools unavailable to the executor subagent) — the plan's Task 1 explicitly anticipated this environment constraint; the Phase 22/26 self-capture precedent applied.
- The LLM-span screenshot shows intact (unredacted-marker-free) message arrays — not a shortfall; see key-decisions and 47-EVIDENCE.md §4.

## Gaps (named per D-14 — feed gap-closure planning)

1. **OBS-02/OBS-03 partial enrichment coverage on live LLM spans:** 115/386 enriched (session.id + metadata + tags + token counts), 271/386 carry only provider+model. ~⅓ enriched uniformly per level; zero conversation-fingerprint overlap between populations; runlog independently records project-level trace-only reporter dispatching ×2. "Every span carries session.id" (OBS-02 letter) does not hold live. Root-cause evidence source: reporter Job args/logs on the still-running cluster. Full diagnostics in 47-EVIDENCE.md §6.2.
2. **Boundary-push `--force-with-lease` stale-`LastPushedSHA` retry defect** (found by 47-04, visible during this capture as the project node's status flap + empty artifacts panel): pre-existing run-integrity-class defect, documented in 47-04-SUMMARY.md deviations; follow-up debug session flagged.
3. **`examples/projects/medium` RWX PVC fixture gap on kind** (47-04 workaround documented): small fixture fix outstanding.

## Self-Check: PASSED

- 4 PNGs on disk, `file` confirms PNG image data — verified
- 47-EVIDENCE.md gates (32-hex trace ID, Known limitations, zero key-prefix strings, ≥4 screenshot refs, Defects section) — verified
- No secret values in any committed artifact; credentials referenced by Secret name only — verified
- Evidence claims cross-checked against 47-PROOF-RUNLOG.md (cost, span counts, trace ID, reporter tally) — verified
