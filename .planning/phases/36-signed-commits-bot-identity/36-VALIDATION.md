---
phase: 36
slug: signed-commits-bot-identity
status: ready
nyquist_compliant: true
wave_0_complete: true
created: 2026-07-04
---

# Phase 36 — Validation Strategy

> Per-phase validation contract for feedback sampling during execution.

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework** | Go stdlib `testing` (+ Ginkgo v2.28/envtest behind `make test` for the controller suite) |
| **Config file** | Makefile targets (no separate test config) |
| **Quick run command** | `go test ./pkg/git/... ./internal/harness/... ./cmd/tide-push/... ./internal/dispatch/podjob/... ./api/... ./cmd/manager/...` |
| **Full suite command** | `make test` (unit tier, sets KUBEBUILDER_ASSETS), then `make test-int-fast` (Layer A) at the phase gate |
| **Estimated runtime** | quick ~60s; `make test` ~5-6 min |

---

## Sampling Rate

- **After every task commit:** Run the touched package's scoped `go test` (each task's `<automated>` command)
- **After every plan wave:** Run the quick run command + `make lint` (import firewalls)
- **Before `/gsd-verify-work`:** `make test` AND `make test-int-fast` green — read `MAKE_EXIT` and `grep -nE '^--- FAIL|^FAIL\s'` the log (Ginkgo-green is not sufficient, per CLAUDE.md)
- **Max feedback latency:** ~360 seconds (`make test` timeout budget)

---

## Per-Task Verification Map

| Task ID | Plan | Wave | Requirement | Threat Ref | Secure Behavior | Test Type | Automated Command | File Exists | Status |
|---------|------|------|-------------|------------|-----------------|-----------|-------------------|-------------|--------|
| 36-01-01 | 01 | 1 | SIGN-01 | — | N/A | unit | `go test ./pkg/git/ -run TestAgentIdentity -v` | ❌ created in-task (TDD) | ⬜ pending |
| 36-01-02 | 01 | 1 | SIGN-01 | — | N/A | unit | `go test ./internal/harness/ ./pkg/git/` | ✅ update in-task | ⬜ pending |
| 36-01-03 | 01 | 1 | SIGN-01 | — | N/A | unit + grep gate | `go test ./cmd/tide-push/` + legacy-name gate == 0 | ✅ update in-task | ⬜ pending |
| 36-02-01 | 02 | 2 | SIGN-01 | T-36-01 | admission rejects `<`,`>`,CR/LF in name; non `x@y` email | unit | `make generate manifests && go test ./api/...` | ✅ extend in-task | ⬜ pending |
| 36-02-02 | 02 | 2 | SIGN-01 | — | resolver pure, nil-safe | unit | `go test ./internal/controller/ -run TestResolveAgentIdentity -v` | ❌ created in-task (TDD) | ⬜ pending |
| 36-02-03 | 02 | 2 | SIGN-01 | — | empty-is-unset preserved (no premature defaulting) | unit | `go test ./cmd/manager/` | ✅ extend in-task | ⬜ pending |
| 36-03-01 | 03 | 3 | SIGN-01 | T-36-01 | env stamped as literal EnvVar values, no interpolation | unit | `go test ./internal/dispatch/podjob/` | ❌ created in-task (TDD) | ⬜ pending |
| 36-03-02 | 03 | 3 | SIGN-01 | T-36-01 | creds EnvFrom untouched; Env added alongside | unit | `go test ./internal/controller/ -run TestBuildPushJob -v` | ❌ created in-task (TDD) | ⬜ pending |
| 36-03-03 | 03 | 3 | SIGN-01 | — | N/A | full unit tier + lint | `make test` (MAKE_EXIT + FAIL grep) + `make lint` | ✅ existing suite | ⬜ pending |
| 36-04-01 | 04 | 3 | SIGN-01 | T-36-02 (accept) | chart tier is operator-only config | contract + regen gate | `make helm-controller helm-crds && make verify-chart-reproducible` | ✅ make targets | ⬜ pending |
| 36-04-02 | 04 | 3 | SIGN-01 | — | N/A | contract (plain go-test, no cluster) | `go test ./test/integration/kind/ -run TestHelmDeploymentTemplateRendersAgentIdentityEnv -v` | ❌ created in-task | ⬜ pending |
| 36-04-03 | 04 | 3 | SIGN-01 | T-36-02 (accept) | docs note future signing email-match; no key config documented | grep | `grep -c 'agentName\|agentEmail' docs/project-authoring.md` | ✅ docs file | ⬜ pending |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*

---

## Wave 0 Requirements

Existing infrastructure covers all phase requirements. New tests are additive within existing packages and are created test-first inside their own tasks (tdd="true" tasks in Plans 01-03) — no separate Wave 0 scaffold needed.

---

## Manual-Only Verifications

All phase behaviors have automated verification. (RESEARCH.md Open Question 2's end-to-end Layer B commit-authorship assertion is explicitly optional — unit + template tests cover SIGN-01's surface; it may be added opportunistically if a kind cluster is available at the phase gate.)

---

## Validation Sign-Off

- [x] All tasks have `<automated>` verify or Wave 0 dependencies
- [x] Sampling continuity: no 3 consecutive tasks without automated verify
- [x] Wave 0 covers all MISSING references (none exist)
- [x] No watch-mode flags
- [x] Feedback latency < 360s
- [x] `nyquist_compliant: true` set in frontmatter

**Approval:** approved 2026-07-04 (planner, headless run)
