---
phase: 9
slug: cross-namespace-envelope-return-in-namespace-reporter
status: draft
nyquist_compliant: false
wave_0_complete: false
created: 2026-06-08
---

# Phase 9 — Validation Strategy

> Per-phase validation contract for feedback sampling during execution.

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework** | Go `testing` + Ginkgo v2 / Gomega · envtest (Layer A) · kind (Layer B integration) |
| **Config file** | Makefile targets; `test/integration/kind/suite_test.go` |
| **Quick run command** | `go test ./internal/... ./cmd/... ./api/... -short` |
| **Full suite command** | `make test-int` (Layer A envtest + Layer B kind) |
| **Estimated runtime** | ~30s quick; multi-min full (kind) |

---

## Sampling Rate

- **After every task commit:** `go test <touched packages> -short`
- **After every plan wave:** `make test` (vet + unit) ; Layer B kind specs for cross-namespace CR-creation behavior
- **Before `/gsd-verify-work`:** full `make test-int` green AND the live medium-sample acceptance (real-Claude end-to-end Complete) per RESEARCH.md "## Validation Architecture"
- **Max feedback latency:** ~30s for unit; minutes for kind/live

---

## Per-Task Verification Map

| Task ID | Plan | Wave | Requirement | Threat Ref | Secure Behavior | Test Type | Automated Command | File Exists | Status |
|---------|------|------|-------------|------------|-----------------|-----------|-------------------|-------------|--------|
| _to be filled by planner_ | | | | | | | | | ⬜ pending |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*

---

## Wave 0 Requirements

- _to be filled by planner_ — likely: reader-Job binary scaffolding + envtest fixtures for cross-namespace child-CR creation; a fake/stub `out.json` fixture exercising both stub- and real-authored shapes.

*See RESEARCH.md "## Validation Architecture" for the cross-namespace test approach (envtest for the reader binary's create logic; kind Layer B for Manager-spawns-reader-Job → children-appear; live medium-sample for the real-Claude acceptance).*

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| Live real-Claude medium-sample end-to-end Complete (real branch pushed, costSpentCents>0) | SC-4/SC-5/SC-6 | Requires real ANTHROPIC_API_KEY + live cluster (minikube); not CI-gated (cost) | `kubectl apply -f examples/projects/medium/project.yaml` on the parked minikube repro; watch the full tree to Complete |

---

## Validation Sign-Off

- [ ] All tasks have `<automated>` verify or Wave 0 dependencies
- [ ] Sampling continuity: no 3 consecutive tasks without automated verify
- [ ] Wave 0 covers all MISSING references
- [ ] No watch-mode flags
- [ ] Feedback latency < 30s (unit)
- [ ] `nyquist_compliant: true` set in frontmatter

**Approval:** pending
