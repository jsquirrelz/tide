---
phase: 8
slug: medium-sample-http-transport-and-production-git-transport-po
status: draft
nyquist_compliant: false
wave_0_complete: false
created: 2026-06-03
---

# Phase 8 — Validation Strategy

> Per-phase validation contract for feedback sampling during execution.
> Derived from `08-RESEARCH.md` § Validation Architecture.

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework** | Ginkgo v2.28 + Gomega (Layer B kind) / standard go test + envtest (Layer A) |
| **Config file** | `test/integration/kind/suite_test.go` (Ginkgo bootstrap) |
| **Quick run command** | `make test` (unit + envtest, < 3 min) |
| **Full suite command** | `make test-int` (Layer A + Layer B kind, ~10–15 min) |
| **Estimated runtime** | ~10–15 min full; < 3 min quick |

---

## Sampling Rate

- **After every task commit:** Run `make test`
- **After every plan wave:** Run `make test-int`
- **Before `/gsd-verify-work`:** Full suite green + live minikube re-test (SC-2)
- **Max feedback latency:** ~180s (quick)

---

## Per-Task Verification Map

> Concrete per-task rows are filled by the planner. The success-criteria → test map below is the contract each plan's `must_haves` must satisfy.

| Success Criterion | Behavior | Test Type | Automated Command | File Exists | Status |
|-------------------|----------|-----------|-------------------|-------------|--------|
| SC-1 | `tide-push` + `claude-subagent` have NO git binary | build verification | `docker run --rm --entrypoint which ghcr.io/jsquirrelz/tide-push:1.0.0 git` → exit 1 | ❌ W0 (CI image-smoke step) | ⬜ pending |
| SC-1 | `tide-demo-init` RETAINS git | build verification | `docker run --rm --entrypoint git ghcr.io/jsquirrelz/tide-demo-init:1.0.0 --version` → exit 0 | ❌ W0 | ⬜ pending |
| SC-2 | medium drives `Project status.phase=Complete` (real Haiku) | manual live (minikube) | `kubectl wait --for=jsonpath='{.status.phase}'=Complete project/... -n tide-sample-medium --timeout=30m` | manual | ⬜ pending |
| SC-3 | CEL rejects `file://` targetRepo at admission | unit (envtest admission) | `make test` (envtest admission spec) | ❌ W0 | ⬜ pending |
| SC-3 | small sample still admits + succeeds with new sentinel | Layer B kind | `ACCEPTANCE_SAMPLE=small make acceptance-v1-smoke` | ✅ | ⬜ pending |
| SC-4 | docs corrected (no false `demo-remote-pvc` mount claim) | source assertion | `grep -n 'demo-remote-pvc' examples/projects/medium/README.md` reflects reality | ✅ | ⬜ pending |
| SC-5 | hermetic git-http clone+push path in CI | kind integration | new Ginkgo spec in nightly Layer B | ❌ W0 | ⬜ pending |
| SC-6 | all sample manifests use `1.0.0` (no-v) | grep assertion | `grep -rn ':v1\.' examples/ \| grep image:` → 0 results | ✅ | ⬜ pending |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*

---

## Wave 0 Requirements

- [ ] `test/integration/envtest/admission_test.go` (or extension to existing webhook/CEL test) — CEL `file://` rejection + `https://` acceptance + small-sample sentinel acceptance (SC-3)
- [ ] `test/integration/kind/medium_http_test.go` — hermetic git-http server clone+push via stub-subagent (SC-5); added to nightly Layer B suite
- [ ] Audit + update all test YAML fixtures: `grep -rn 'targetRepo.*file://' test/` → migrate to sentinel before the CEL change lands (SC-3 prerequisite — CEL change breaks any fixture still on `file://`)
- [ ] CI image-smoke step in `nightly-integration.yml`: `docker run --entrypoint which git` presence/absence for SC-1

---

## Manual-Only Verifications

| Behavior | Criterion | Why Manual | Test Instructions |
|----------|-----------|------------|-------------------|
| Full medium sample drives `Project=Complete` with real Claude (Haiku) | SC-2 | Real LLM cost (~$5) + single-node cluster; not CI-automatable without a real key | Apply documented medium sequence on live minikube; `kubectl wait` for `status.phase=Complete`; confirm a per-run branch was pushed to the in-cluster `http://` remote |

---

## Validation Sign-Off

- [ ] All tasks have `<automated>` verify or Wave 0 dependencies
- [ ] Sampling continuity: no 3 consecutive tasks without automated verify
- [ ] Wave 0 covers all MISSING references
- [ ] No watch-mode flags
- [ ] Feedback latency < 180s
- [ ] `nyquist_compliant: true` set in frontmatter

**Approval:** pending
