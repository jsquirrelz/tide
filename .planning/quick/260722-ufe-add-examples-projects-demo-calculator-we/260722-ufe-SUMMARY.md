---
phase: quick-260722-ufe
plan: 01
type: execute
status: complete
subsystem: examples
tags: [samples, demo, git-http-server, project-crd]
requires: []
provides:
  - examples/projects/demo-calculator/ (7-file ~$10 real-Claude web-calculator sample)
  - demo-calculator row in root README samples table
affects: []
tech-stack:
  added: []
  patterns:
    - seed-image reuse — tide-git-http-server image doubles as the seed-job image (sh + git + UID 1000), replacing the tide-demo-init fixture path
    - idempotent-by-refusal seed script (exit 0 if calculator.git exists)
key-files:
  created:
    - examples/projects/demo-calculator/namespace.yaml
    - examples/projects/demo-calculator/calculator-remote-pvc.yaml
    - examples/projects/demo-calculator/seed-remote-job.yaml
    - examples/projects/demo-calculator/git-http-server.yaml
    - examples/projects/demo-calculator/per-namespace-resources.yaml
    - examples/projects/demo-calculator/project.yaml
    - examples/projects/demo-calculator/README.md
  modified:
    - README.md
decisions:
  - "Seed script uses `git checkout -b main` after cloning the empty bare repo — verified locally that -b succeeds on an unborn main (branch ref doesn't exist yet)"
metrics:
  duration: ~8 minutes
  completed: 2026-07-23
  tasks: 3
  files: 8
---

# Quick Task 260722-ufe: demo-calculator Sample Summary

**One-liner:** ~$10 real-Claude web-calculator demo sample (7 files, seed-Job → git-http-server → Project CRD architecture mirroring medium, with a fresh inline-seeded bare repo instead of the tide-demo-init fixture) plus a demo-calculator row in the root README samples table.

## What was built

- **Infra manifests (Task 1, `447de735`):** namespace `tide-demo-calculator`, `calculator-remote-pvc` (RWO 100Mi), `calculator-remote-init` seed Job, `git-http-server` Deployment + ClusterIP Service, and the per-namespace tide-projects PVC + tide-subagent/tide-push/tide-reporter RBAC (signing-key-mirror heredoc comment retained, namespace-swapped).
- **Project CRD + sample README (Task 2, `ae3b14d8`):** `demo-calculator` Project pinning `tide-claude-subagent:1.0.9`, `claude-sonnet-5`, $10 budget with explicit 24h rolling window, all-auto gates, strict fileTouchMode, maxAttemptsPerTask 3, calculator outcomePrompt (exactly 3 files, four ops, keyboard, no deps, ONE Phase / ONE Plan / 3-5 tasks). README mirrors medium's section structure with the 10-step apply sequence, kind 4b RWO override, newline-stripped Secret creation, and the clone-and-open-index.html payoff.
- **Root README row (Task 3, `9258d988`):** demo-calculator row between medium and large (cost-ascending $0 → $5 → $10 → $25).

## Verification observed

- `ls examples/projects/demo-calculator/` → exactly 7 files.
- All 6 YAML files parse under `yq eval-all`.
- `go test ./test/integration/kind/ -run TestExamplesSubagentImagePinsMatchChartAppVersion -count=1` → `ok ... 0.446s` (walks the 7 new files + README row; all pins == 1.0.9).
- `git diff --name-only <base>..HEAD` → exactly the 7 new files + root README.md; zero charts/ diff; zero deletions.
- Seed script executed locally end-to-end from the RENDERED YAML block scalar: seeds one commit on `main` (`git log` shows it), `http.receivepack=true`, and a second run refuses with "calculator.git already exists — leaving it untouched".
- `git checkout -b main` on a clone-of-empty-repo unborn branch verified working locally before authoring (exit 0, "Switched to a new branch 'main'").

## Deviations from Plan

None — plan executed exactly as written. One environment note (not a file deviation): the host's `grep` is ugrep 7.5.0, whose `-lv` semantics differ from GNU/BSD grep, so Task 1's verify command was re-run via `/usr/bin/grep` (BSD) where it passes exactly as written.

## Task Commits

| Task | Name | Commit |
| ---- | ---- | ------ |
| 1 | Infra manifests (5 YAML) | 447de735 |
| 2 | project.yaml + sample README | ae3b14d8 |
| 3 | Root README samples-table row | 9258d988 |

## Known Stubs

None — the sample is fully wired; the two fixture images it references (`tide-git-http-server:1.0.0`) are intentionally unpublished demo fixtures built locally per README step 1 (same contract as the medium sample).

## Threat Flags

None — the new manifests reuse the medium sample's already-modeled surface (ClusterIP-only anonymous git remote T-08-05-01, nonroot UID 1000 T-08-05-04, least-privilege tide-reporter T-09-07); no new endpoints, auth paths, or trust-boundary changes.

## Self-Check: PASSED

- All 7 created files exist on disk; root README.md row present (count == 1).
- Commits 447de735, ae3b14d8, 9258d988 all present in `git log`.
