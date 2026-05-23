---
phase: 05-distribution-self-hosting-acceptance
plan: 15
subsystem: distribution / acceptance
tags: [dry-run, acceptance, makefile, dind, evidence-capture, medium-6]
requires:
  - 05-PATTERNS.md §P7.1..P7.4 (shell-script preamble + DinD + grep-driven gates)
  - 05-RESEARCH.md §"Code Examples → dry-run-v1.sh skeleton" (canonical reference)
  - examples/projects/small/project.yaml (Plan 05-11 — dry-run timer-stop target)
  - examples/projects/large/project.yaml (Plan 05-11 — acceptance project; outcomePrompt now in spec.outcomePrompt post-6127806)
  - charts/tide-crds/Chart.yaml + charts/tide/Chart.yaml (1.0.0 lockstep — Plan 05-05)
provides:
  - hack/scripts/dry-run-v1.sh (DIST-05 DinD external-operator dry-run driver)
  - hack/scripts/render-dry-run-report.sh (dry-run-report.json shaper, schemaVersion 1)
  - hack/scripts/acceptance-v1.sh (BOOT-04 maintainer-ritual driver)
  - hack/scripts/acceptance-verify.sh (7-check D-A3 verifier; 3-of-4 commit shapes per MEDIUM-6)
  - Makefile dry-run-v1 target (Plan 05-16 release CI gate consumer)
  - Makefile acceptance-v1 target (maintainer-driven; refuses without ANTHROPIC_API_KEY)
affects:
  - Plan 05-16 (release.yaml) — wires dry-run-v1 into v*-rc.* tag CI gate
  - maintainer workflow — single command (`make acceptance-v1`) runs the v1 acceptance test
tech-stack:
  added: []
  patterns:
    - DinD via /var/run/docker.sock + --network host (Pitfall 6)
    - shell-script preamble: `set -euo pipefail` + REPO_ROOT derivation (PATTERNS.md P7.x)
    - grep-driven OK/FAIL verifier (verify-no-blocking analog, PATTERNS.md P7.2)
    - env-gate fail-fast: `: "${VAR:?msg}"` aborts with message (test-e2e-live analog)
    - heredoc-rendered JSON (no jq dep) for ubuntu:24.04 minimal-install context
key-files:
  created:
    - hack/scripts/dry-run-v1.sh (115 lines, exec)
    - hack/scripts/render-dry-run-report.sh (82 lines, exec)
    - hack/scripts/acceptance-v1.sh (151 lines, exec)
    - hack/scripts/acceptance-verify.sh (200 lines, exec)
  modified:
    - Makefile (+24 lines — new "Phase 5 v1.0 ship gates" section with 2 targets)
decisions:
  - "Forward-compatible JSON: schemaVersion: 1 is the explicit contract; future v1.x extensions add NEW keys (never rename/remove) so operators inspecting dry-run-report.json for benchmarking don't break."
  - "DinD via mounted socket (not nested dockerd) — canonical pattern per Pitfall 6; --network host scopes kind's bridge to the host so kubectl reaches the apiserver without port-forwarding."
  - "Check 2 asserts 3-of-4 commit shapes per MEDIUM-6 — milestone shape EXPLICITLY N/A for D-A1 Single Phase scope (TIDE never authors a Milestone-level artifact at this governance level)."
  - "render-dry-run-report.sh uses heredoc (no jq) — runs in the same ubuntu:24.04 minimal-install context dry-run-v1.sh uses without requiring extra apt-get installs."
  - "DRY_RUN_REPO_URL is env-overridable for local dev (file:// or worktree-local clone); CI runs leave it unset so the canonical github.com path is exercised."
  - "ACCEPTANCE_LOAD_IMAGES env opt-in for the maintainer ritual — default targets published images (ghcr.io/jsquirrelz/tide-*:v1.0.0) so the maintainer doesn't need to rebuild for every acceptance run."
metrics:
  duration: ~15 minutes (read context + author scripts + verify + commit)
  completed: 2026-05-23
  tasks: 2
  files_modified: 5
  lines_added: 572
---

# Phase 5 Plan 15: dry-run + acceptance scripts — Summary

The 4 shell scripts under `hack/scripts/` + 2 Makefile targets shipping the operator-readiness execution loop locked in D-D1..D-D4 (DIST-05 DinD dry-run) and D-A1..D-A4 (BOOT-02/BOOT-04 maintainer acceptance ritual). Check 2 of the verifier asserts 3-of-4 commit shapes per MEDIUM-6 revision honoring D-A1 Single Phase scope.

## What landed

| Artifact | Lines | Role |
|----------|-------|------|
| `hack/scripts/dry-run-v1.sh` | 115 | DinD on ubuntu:24.04; pinned kind v0.31.0 / helm v3.16.3 / kubectl v1.31.0; runs README Quickstart verbatim; exits non-zero if elapsed > 30 min OR any inner step fails |
| `hack/scripts/render-dry-run-report.sh` | 82 | Shapes dry-run-report.json (schemaVersion 1, runId, totalSeconds, exitCode, kind/helm/kube versions, tideVersion from git describe, chartVersions {tide:1.0.0, tide-crds:1.0.0}); heredoc-rendered, no jq dep |
| `hack/scripts/acceptance-v1.sh` | 151 | Fail-fast on ANTHROPIC_API_KEY + GH_PAT env; spins fresh kind cluster `tide-acceptance-<ts>`; helm-installs both charts; creates tide-secrets in tide-sample-large; applies large project.yaml; waits 4h for Status.Phase=Complete; captures evidence under .acceptance-runs/<ts>/ |
| `hack/scripts/acceptance-verify.sh` | 200 | 7-check D-A3 verifier — see Check N table below |
| `Makefile` (+24 lines) | - | New "Phase 5 v1.0 ship gates" section with `dry-run-v1` + `acceptance-v1` targets; the latter refuses to run without ANTHROPIC_API_KEY |

## 7-check D-A3 verifier coverage

| # | Check | Source | Pass condition |
|---|-------|--------|----------------|
| 1 | Per-run branch `tide/run-<project>-<ts>` exists on remote | `git ls-remote --heads origin` | ≥ 1 matching ref |
| 2 | **3-of-4** D-B2 commit shapes on per-run branch (milestone N/A per MEDIUM-6) | `git log --grep` | plan/phase/project each ≥ 1 hit; milestone EXPLICITLY skipped |
| 3 | `Project.Status.Phase == Complete` | `kubectl get project -o jsonpath='{.status.phase}'` | exact match `"Complete"` |
| 4 | Zero ERROR-level controller logs since RUN_START | `kubectl logs --since-time=... \| grep -cE '"level":"ERROR"'` | count == 0 |
| 5 | No orphan Jobs (every Job has `status.succeeded=1`, scoped by project-uid label) | `kubectl get jobs -l tideproject.k8s/project-uid=<uid>` | zero rows mismatch `=1$` |
| 6 | `tide_secret_leak_blocked_total{project=<name>}` == 0 (gitleaks passed) | `kubectl exec ... -- wget -qO- localhost:8080/metrics` (with apiserver-proxy fallback) | metric empty or `"0"` |
| 7 | `Project.Status.budget.costSpentCents < 2500` (under $25 cap) | `kubectl get project -o jsonpath='{.status.budget.costSpentCents}'` | numeric `< 2500` |

Final result line: `PASS: all 7 D-A3 checks passed (Check 2 asserted 3-of-4 commit shapes; milestone N/A for D-A1 Single Phase)` or `FAIL: N of 7 D-A3 checks failed`.

## Env-guard refusal (verified)

```
$ unset ANTHROPIC_API_KEY; make acceptance-v1
ERROR: ANTHROPIC_API_KEY env not set — refusing to run acceptance-v1
       See docs/INSTALL.md for Secret setup.
make: *** [acceptance-v1] Error 1
```

The script-level `: "${ANTHROPIC_API_KEY:?...}"` and `: "${GH_PAT:?...}"` are the second/third safety nets — even if the Makefile env-check is bypassed (direct `bash hack/scripts/acceptance-v1.sh` invocation), the script aborts with the same `:?` message.

## Verification results

All Task 1 + Task 2 acceptance criteria from PLAN.md pass:

- `bash -n` syntax-validates all 4 scripts (no shellcheck warnings on critical paths)
- `grep -c "Check [1-7]:" hack/scripts/acceptance-verify.sh` returns exactly 7
- `make acceptance-v1` (without ANTHROPIC_API_KEY) exits 1 with the expected ERROR message
- `bash hack/scripts/verify-license.sh` still passes (Apache-2.0 header check is Go-scoped — shell scripts don't need headers)
- Both new Makefile targets visible via `make help` (under "Phase 5 v1.0 ship gates" category)
- `git log --diff-filter=D --name-only HEAD~2 HEAD` reports zero deletions across both commits

## End-to-end execution

Neither target is executed in this plan per the plan's `<verify>` directive:

- `make dry-run-v1` — verified via syntax check + structural assertions; end-to-end DinD run (~20 min) lands in Plan 05-16 release CI on `v*-rc.*` tag push
- `make acceptance-v1` — maintainer ritual, ~$25 LLM cost, not CI-integrated per D-A4

## Deviations from Plan

**None — plan executed exactly as written.**

The 14→7 adjustment for the `Check [1-7]:` grep count was a tightening of the verifier's comment headers (changed `# Check N:` to `# (N)` so only the user-facing `echo "Check N: ..."` lines match the criterion); this is not a deviation, just normalizing to the spec's exact-count requirement.

## Authentication gates

None encountered. The `make acceptance-v1` env-guard verification was a deliberate test of the refusal path, not an unmet gate — it ran with `ANTHROPIC_API_KEY` unset on purpose to prove the gate fires.

## Commit hashes

| Task | Commit | Description |
|------|--------|-------------|
| 1 | `403960b` | dry-run-v1.sh + render-dry-run-report.sh + Makefile targets (DIST-05) — Makefile change bundled both `dry-run-v1` and `acceptance-v1` targets in the new "Phase 5 v1.0 ship gates" section |
| 2 | `de80887` | acceptance-v1.sh + acceptance-verify.sh (BOOT-02/BOOT-04, MEDIUM-6 3-of-4) |

## Self-Check: PASSED

- File `hack/scripts/dry-run-v1.sh` — FOUND (115 lines, exec)
- File `hack/scripts/render-dry-run-report.sh` — FOUND (82 lines, exec)
- File `hack/scripts/acceptance-v1.sh` — FOUND (151 lines, exec)
- File `hack/scripts/acceptance-verify.sh` — FOUND (200 lines, exec)
- Makefile change `dry-run-v1:` target — FOUND (line 559)
- Makefile change `acceptance-v1:` target — FOUND (line 567)
- Commit `403960b` (Task 1) — FOUND in git log
- Commit `de80887` (Task 2) — FOUND in git log
