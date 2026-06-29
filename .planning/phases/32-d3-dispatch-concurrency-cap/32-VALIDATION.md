---
phase: 32
slug: d3-dispatch-concurrency-cap
status: draft
nyquist_compliant: false
wave_0_complete: false
created: 2026-06-28
---

# Phase 32 — Validation Strategy

> Per-phase validation contract for feedback sampling during execution.

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework** | go test + Ginkgo/Gomega + envtest (Layer A controller suite) |
| **Config file** | none — `setup-envtest` provisions assets via `make test` |
| **Quick run command** | `go test ./internal/controller/... -run <FocusPattern>` |
| **Full suite command** | `make test` (envtest controller suite + analyzers + `make lint` cross-pool) |
| **Estimated runtime** | ~150 seconds (controller suite) |

---

## Sampling Rate

- **After every task commit:** Run the focused controller spec for the touched site
- **After every plan wave:** Run `make test`
- **Before `/gsd:verify-work`:** Full suite green AND `make lint` (cross-pool analyzer) green
- **Max feedback latency:** ~150 seconds

---

## Per-Task Verification Map

> Populated by the planner. Each CONCUR requirement maps to an envtest spec; the cap-behavior (CONCUR-01) is provable in envtest by stubbing N+1 dispatchable parents and asserting at most N non-terminal planner Jobs exist + a RequeueAfter is returned for the (N+1)th.

| Task ID | Plan | Wave | Requirement | Threat Ref | Secure Behavior | Test Type | Automated Command | File Exists | Status |
|---------|------|------|-------------|------------|-----------------|-----------|-------------------|-------------|--------|
| 32-01-01 | 01 | 1 | CONCUR-01 | — | in-flight count gate caps non-terminal planner Jobs at N | envtest | `go test ./internal/controller/... -run Concurrency` | ❌ W0 | ⬜ pending |

---

## Wave 0 Requirements

- [ ] `internal/controller/dispatch_concurrency_cap_test.go` — envtest specs for CONCUR-01 (cap enforced), CONCUR-04 (deferral logged + requeued, not dropped)
- [ ] `internal/config/config_test.go` — assert new default for `plannerConcurrency` (CONCUR-02)
- [ ] `make lint` cross-pool analyzer remains green (CONCUR-03 — pools stay separate)

*Existing Ginkgo/envtest infrastructure covers the controller-level assertions; only new spec files are needed.*

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| At most N planner Jobs Running in a live cluster while 5+ Milestones enqueued | CONCUR-01 | Requires a running kind cluster + real Job scheduling (envtest does not run pods) | `kubectl get jobs -l tideproject.k8s/role=planner -w` with `plannerConcurrency=2` and 5 Milestones; confirm ≤2 non-terminal at a time |

---

## Validation Sign-Off

- [ ] All tasks have `<automated>` verify or Wave 0 dependencies
- [ ] Sampling continuity: no 3 consecutive tasks without automated verify
- [ ] Wave 0 covers all MISSING references
- [ ] No watch-mode flags
- [ ] Feedback latency < 150s
- [ ] `nyquist_compliant: true` set in frontmatter

**Approval:** pending
