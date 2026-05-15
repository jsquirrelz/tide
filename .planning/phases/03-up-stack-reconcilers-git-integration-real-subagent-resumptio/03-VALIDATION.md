---
phase: 3
slug: up-stack-reconcilers-git-integration-real-subagent-resumptio
status: draft
nyquist_compliant: false
wave_0_complete: false
created: 2026-05-15
---

# Phase 3 — Validation Strategy

> Per-phase validation contract for feedback sampling during execution.
> Authoritative source: `03-RESEARCH.md` § Validation Architecture.

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework** | Ginkgo v2.28 + Gomega + envtest (Layer A); Ginkgo + kind v0.31 (Layer B) |
| **Config file** | `internal/controller/suite_test.go` (envtest); `test/integration/suite_test.go` (kind) |
| **Quick run command** | `make test` (envtest unit + integration; ~30s) |
| **Full suite command** | `make test-int` (kind tier; ~3-5 min) |
| **Estimated runtime** | ~30s quick / ~4 min full |

See `03-RESEARCH.md` § Validation Architecture for per-deliverable Layer A / Layer B split.

---

## Sampling Rate

- **After every task commit:** Run `make test` (Layer A envtest — quick feedback)
- **After every plan wave:** Run `make test-int` (Layer B kind — full integration)
- **Before `/gsd-verify-work`:** Full suite must be green; chaos-resume spec must be green
- **Max feedback latency:** 30 seconds for unit/envtest; 5 minutes for kind integration

---

## Per-Task Verification Map

Populated by gsd-planner during plan authoring — each task in each PLAN.md must
emit a row pinned to: REQ-IDs covered, Layer A or B classification, automated
command, and a `File Exists` checkmark (✅ if the test file is already on
disk, ❌ W0 if Wave 0 must create the file before the task can verify).

| Task ID | Plan | Wave | Requirement | Threat Ref | Secure Behavior | Test Type | Automated Command | File Exists | Status |
|---------|------|------|-------------|------------|-----------------|-----------|-------------------|-------------|--------|
| {to be populated by planner} | | | | | | | | | |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*

---

## Wave 0 Requirements

Wave 0 = test scaffolding the planner must materialize before any production
task can verify. Phase 3 candidate Wave 0 items (planner finalizes):

- [ ] `pkg/git/git_test.go` — table-driven clone/commit/push fixtures (Layer A; httptest-backed remote)
- [ ] `internal/gitleaks/gitleaks_test.go` — DetectString fixtures (Layer A)
- [ ] `pkg/dispatch/envelope_test.go` extension — `ProviderSpec` + `ChildCRDSpec` + cache-token fields (Layer A)
- [ ] `internal/subagent/anthropic/parser_test.go` — stream-json line-by-line parse fixtures using captured JSONL samples (Layer A — no live API call)
- [ ] `test/integration/chaos_resume_test.go` — kind-tier four-pillar Ginkgo assertions with `wait-for-signal` stub fixture (Layer B; D-D2 mixed-state 3-task fixture)
- [ ] `test/integration/push_lease_test.go` — kind-tier first-push lease semantics + stale-lease rejection (Layer B; Pitfall 13)
- [ ] `test/integration/up_stack_dispatch_test.go` — kind-tier Milestone/Phase/Plan reconciler dispatch + childCRD materialization (Layer B; D-A1/D-A2)

*If none of the above already exist: planner must scaffold them as Wave 0 tasks before dependent production tasks.*

---

## Manual-Only Verifications

Phase 3 RESEARCH §"Validation Architecture" identifies one manual verification
needed for ART-02 (host-agnostic git push documentation):

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| Multi-host smoke (GitLab/Gitea generic HTTPS) | ART-02 | Phase 3 ships GitHub fixture only; GitLab/Gitea are documented "should-work" via the same generic HTTPS+PAT code path. Live verification requires real GitLab/Gitea credentials, deferred to v1.x | Manual smoke test recipe in `docs/git-hosts.md` (created in Phase 3): apply a Project pointing at GitLab/Gitea remote, observe per-run branch lands |

*All other phase behaviors have automated verification (Layer A envtest or Layer B kind int).*

---

## Validation Sign-Off

- [ ] All tasks have `<automated>` verify or Wave 0 dependencies
- [ ] Sampling continuity: no 3 consecutive tasks without automated verify
- [ ] Wave 0 covers all MISSING references
- [ ] No watch-mode flags
- [ ] Feedback latency < 30s for Layer A, < 5min for Layer B
- [ ] `nyquist_compliant: true` set in frontmatter

**Approval:** pending — gsd-plan-checker validates after PLAN.md authoring.
