---
phase: 35
slug: git-base-ref
status: approved
nyquist_compliant: true
wave_0_complete: true
created: 2026-07-04
---

# Phase 35 — Validation Strategy

> Per-phase validation contract for feedback sampling during execution.

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework** | Go `testing` (plain go-tests) + Ginkgo v2.28/Gomega + envtest (Layer A) + kind (Layer B) |
| **Config file** | Makefile targets (no separate config); envtest via `setup-envtest` |
| **Quick run command** | `go test ./pkg/git/... ./cmd/tide-push/... ./api/... ./internal/controller/... -count=1` (dev env — go absent from host PATH, RESEARCH A4) |
| **Full suite command** | `make test` → `make test-int-fast` → `make test-int` (phase gate; constrained-VM recipe applies) |
| **Estimated runtime** | quick ~60s · `make test` ~6min · `make test-int` heavy (fresh kind cluster per run) |

---

## Sampling Rate

- **After every task commit:** Run the quick command scoped to the packages the task touched
- **After every plan wave:** Run `make test` (waves 1–2); `make test-int` after wave 3
- **Before `/gsd-verify-work`:** `make test-int` green — read `MAKE_EXIT` AND `grep -nE '^--- FAIL|^FAIL '` the log (Ginkgo summary alone is insufficient per CLAUDE.md)
- **Max feedback latency:** ~90 seconds (per-commit tier)

---

## Per-Task Verification Map

| Task ID | Plan | Wave | Requirement | Threat Ref | Secure Behavior | Test Type | Automated Command | File Exists | Status |
|---------|------|------|-------------|------------|-----------------|-----------|-------------------|-------------|--------|
| 35-01-01 | 01 | 1 | BASE-01, BASE-03 | T-35-01/T-35-04 | Pattern rejects leading `-`/space; MaxLength bounds cost; no default marker | unit + grep | `go build ./api/... && grep -c '^ *baseRef:' config/crd/bases/tideproject.k8s_projects.yaml` | ✅ existing infra | ⬜ pending |
| 35-01-02 | 01 | 1 | BASE-03 | — | N/A | static unit + render | `go test ./api/... -run 'BaseRef\|BaseSHA\|Phase35' -count=1; go test ./test/integration/kind/ -run TestHelmTideCRDsRenderBaseRefBothVersions -count=1` | ❌ lands with task (TDD) | ⬜ pending |
| 35-02-01 | 02 | 1 | BASE-01 | T-35-02 | Resolution errors carry only ref+hash, never URL/PAT | unit | `go test ./pkg/git/ -run TestEnsureRunBranch -count=1` | ❌ lands with task (TDD) | ⬜ pending |
| 35-02-02 | 02 | 1 | BASE-01, BASE-02 | T-35-02/T-35-05 | redactPAT on new stderr paths; envelope written last per exit path | unit | `go test ./cmd/tide-push/ -run TestRunClone -count=1` | ❌ lands with task (TDD) | ⬜ pending |
| 35-03-01 | 03 | 2 | BASE-02, BASE-03 | T-35-03 | Condition message = fixed template + admission-filtered ref | unit | `go test ./internal/controller/ -run TestBuildCloneJob -count=1` | ❌ lands with task (TDD) | ⬜ pending |
| 35-03-02 | 03 | 2 | BASE-02, BASE-03 | T-35-05/T-35-06 | Unparseable envelope → generic path, never halt/stamp | envtest | `go test ./internal/controller/ -count=1 -timeout 360s -ginkgo.focus='baseref'` | ❌ lands with task (TDD) | ⬜ pending |
| 35-04-01 | 04 | 3 | BASE-01, BASE-03 | T-35-07 | Chart-installed CRDs carry the field (no silent pruning) | kind (Layer B) | `make test-int` + MAKE_EXIT + FAIL-grep protocol | ❌ lands with task (TDD) | ⬜ pending |
| 35-04-02 | 04 | 3 | BASE-01, BASE-02, BASE-03 | T-35-07 | Docs state CRD-chart-first upgrade order | grep | `grep -c 'baseRef' docs/project-authoring.md && grep -ci 'tide-crds' docs/INSTALL.md` | ✅ docs exist | ⬜ pending |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*

---

## Wave 0 Requirements

Existing infrastructure covers all phase requirements — framework, envtest harness, kind suite, and fixture helpers (`seedBareRepo`, `withGit`, git-http-server image) all exist (RESEARCH §Wave 0 Gaps: none structural). New test files/cases land with their implementation tasks (TDD); the only fixture extensions are seeding a non-default branch + annotated/lightweight tags (pkg/git, plan 35-02) and a `base-ref-target` branch in the kind git-http fixture (plan 35-04).

---

## Manual-Only Verifications

All phase behaviors have automated verification.

---

## Validation Sign-Off

- [x] All tasks have `<automated>` verify or Wave 0 dependencies
- [x] Sampling continuity: no 3 consecutive tasks without automated verify
- [x] Wave 0 covers all MISSING references (none structural)
- [x] No watch-mode flags
- [x] Feedback latency < 90s (per-commit tier)
- [x] `nyquist_compliant: true` set in frontmatter

**Approval:** approved 2026-07-04 (headless planning run — conservative fill from 35-RESEARCH.md §Validation Architecture; see plan files for per-task acceptance criteria)
