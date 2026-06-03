---
phase: 08-medium-sample-http-transport-and-production-git-transport-po
plan: "06"
subsystem: docs
tags: [docs, medium-sample, git-transport, http-transport, readme]
dependency_graph:
  requires: [08-03, 08-04]
  provides: [corrected-medium-readme, finalized-doc-go, updated-samples-index]
  affects: [examples/projects/medium/README.md, examples/projects/README.md, pkg/git/doc.go]
tech_stack:
  added: []
  patterns: [documentation-correction, false-claim-removal, 9-step-apply-sequence]
key_files:
  created: []
  modified:
    - examples/projects/medium/README.md
    - examples/projects/README.md
    - pkg/git/doc.go
decisions:
  - "Removed the false 'controller mounts demo-remote-pvc' claim from the medium README; replaced with explicit correction: controller clone/push Jobs do NOT mount demo-remote-pvc — they reach the repo via HTTP through git-http-server Service"
  - "Documented 9-step apply sequence with RWO PVC ordering rationale: init Job must Complete before git-http-server starts to avoid RWO mount conflict"
  - "Updated examples/projects/README.md to remove all file:// transport references; small row now uses RFC 2606 https://git.example.internal sentinel; medium row shows in-cluster http:// transport"
  - "Finalized pkg/git/doc.go with specific DNS name example and clarification that git-http-server bridges the init Job's PVC to the HTTP transport layer"
metrics:
  duration: "3m"
  completed_date: "2026-06-03"
  tasks_completed: 3
  tasks_total: 3
  files_changed: 3
---

# Phase 8 Plan 06: SC-4 Docs Correction Summary

**One-liner:** Removed false "controller mounts demo-remote-pvc" claim from medium README, documented the 9-step http:// apply sequence, removed all file:// transport descriptions from the top-level samples index, and finalized the pkg/git transport policy doc with the specific in-cluster DNS name example.

## Tasks Completed

| Task | Name | Commit | Key Changes |
|------|------|--------|-------------|
| 1 | Rewrite examples/projects/medium/README.md | 734b175 | False mount claim removed; 9-step sequence with RWO rationale; architecture section; minikube gotchas |
| 2 | Finalize pkg/git/doc.go transport policy | 5f8763d | DNS name example added; http-backend description; clarified init Job's bridge role |
| 3 | Update examples/projects/README.md samples index | 9da64a5 | Removed all file:// references; small row → https://git.example.internal sentinel; medium row → in-cluster http:// |

## Deviations from Plan

None — plan executed exactly as written.

The verification grep `controller.*mounts\|mounts.*demo-remote-pvc` in Task 1 matched two ACCURATE remaining lines:
- Line 47: "git-http-server mounts demo-remote-pvc" — TRUE (the server is supposed to mount it)
- Line 221: "temporary debug pod mounts demo-remote-pvc" — verification step

The false claim (the controller's clone Job "mounting the same demo-remote-pvc as the init Job", line 96 of the original) has been removed and replaced with an explicit correction: "The controller's clone and push Jobs do NOT mount demo-remote-pvc." The plan's success criteria specifically allows "accurate" references to remain.

## Success Criteria Verification

| Criterion | Status |
|-----------|--------|
| medium README: no false "controller mounts demo-remote-pvc" claim | PASS — removed and corrected |
| medium README: 9-step apply sequence with per-namespace-resources.yaml and git-http-server-deployment.yaml | PASS — both referenced with correct step ordering |
| examples/projects/README.md: no file:// references | PASS — `! grep -q 'file://' examples/projects/README.md` passes |
| examples/projects/README.md: small row shows https://git.example.internal sentinel | PASS |
| examples/projects/README.md: medium row shows in-cluster http:// transport | PASS |
| pkg/git/doc.go: "file:// is NOT" policy statement present | PASS |
| pkg/git/doc.go: tide-git-http-server referenced | PASS |
| go build ./pkg/git/... exits 0 | PASS |

## Known Stubs

None. All documentation describes implemented architecture accurately.

## Threat Flags

None. This plan only modifies documentation files (README.md and doc.go). No new network endpoints, auth paths, or schema changes introduced.

## Self-Check: PASSED

Commits exist:
- 734b175: `git log --oneline --all | grep 734b175` → found
- 5f8763d: `git log --oneline --all | grep 5f8763d` → found
- 9da64a5: `git log --oneline --all | grep 9da64a5` → found

Files modified:
- examples/projects/medium/README.md → exists, no false claim, 9-step sequence present
- pkg/git/doc.go → exists, "file:// is NOT" present, DNS name present, go build exits 0
- examples/projects/README.md → exists, no file:// references, sentinel and http:// descriptions present
