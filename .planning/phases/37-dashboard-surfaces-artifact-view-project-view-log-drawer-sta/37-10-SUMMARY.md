---
phase: 37-dashboard-surfaces-artifact-view-project-view-log-drawer-sta
plan: 10
subsystem: dashboard
tags: [uat, checkpoint, human-verify, d-15, dash-01, dash-03, dash-04, live-cluster]

# Dependency graph
requires:
  - plan: 37-09
    provides: "all Phase-37 surfaces integrated + gate-green (Layer-B artifact staging)"
provides:
  - "live-cluster UAT of all Phase-37 surfaces (8/8) with operator D-15 sign-off"
  - "two gap plans (37-11, 37-12) surfaced, fixed, and re-verified live"
affects: [DASH-01, DASH-03, DASH-04, D-15]
---

# 37-10 — Live UAT + D-15 human sign-off

Human-verification checkpoint (`gate="blocking"`). Prep was automated end-to-end; the operator gave the LOCKED D-15 sign-off on 2026-07-09.

## What was done

An autonomous live-cluster UAT on a fresh **isolated** kind cluster (`tide-uat37`) with **current-tree** images and the deterministic stub subagent ($0 — the operator's minikube and its perishable PVC evidence were untouched). Two stub projects were driven: an all-auto project for the log-drawer / project / resize surfaces, and a `spec.git` + milestone-`approve`-gated project (in-cluster git remote via `tide-git-http-server` + `tide-demo-init`) for the artifact view + approve flow. Verification used headless Playwright (DOM/accessibility snapshots, screenshots, network inspection) plus backend `curl` probes.

## Result — 8/8 surfaces verified

1. Log drawer, running pod streams (CR-01, namespace-threaded URL) — PASS
2. Log drawer, GC'd pod honest state ("pod garbage-collected", no retry, single request) — PASS (**D-15 headline; empty-drawer bug gone**)
3. Log drawer, connection drop → reconnect (never blank, never false pod-gone, auto-resume) — PASS
4. Artifact view, milestone at gate ("Artifacts materializing" placeholder + `available` fetch/render pipeline) — PASS
5. Approve flow (strip copies `tide approve <project>` → run advances, no PVC reader pod) — PASS
6. Node shows its OWN typed artifact state — PASS
7. Project view / DASH-03 secret redaction (names+purpose only, no values) — PASS
8. Resize/collapse persistence across reload — PASS

Bonus: DASH-02 / Defect-E confirmed live (`lastPushedSHA` advanced on the run branch).

## Gaps found → fixed → re-verified

The UAT surfaced two operator-selected gaps, both closed in this phase and re-verified against the same live cluster:

- **37-11 (Gap 37-G1):** dashboard `resolveAuth` made scheme-conditional so an anonymous `http://` remote with an empty `GIT_PAT` fetches artifacts (`state:available`) instead of erroring — mirrors `cmd/tide-push`'s guard.
- **37-12 (Gap 37-G2):** manual "Reconnect" button added to the log-drawer reconnecting state; re-verified live (button appears on drop, resumes streaming).

Note 2 (rich milestone-body render needs a real Claude run) accepted as-is — a stub-fidelity coverage limit, not a defect.

## Sign-off

Operator approved D-15 on 2026-07-09. Full evidence + per-test detail in `37-UAT.md`; screenshots under the session evidence dir. Phase 37 closed.
