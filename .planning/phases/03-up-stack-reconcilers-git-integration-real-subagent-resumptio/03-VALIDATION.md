---
phase: 3
slug: up-stack-reconcilers-git-integration-real-subagent-resumptio
status: ready
nyquist_compliant: true
wave_0_complete: true
created: 2026-05-15
updated: 2026-05-15
---

# Phase 3 — Validation Strategy

> Per-phase validation contract for feedback sampling during execution.
> Authoritative source: `03-RESEARCH.md` § Validation Architecture.

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework** | Ginkgo v2.28 + Gomega + envtest (Layer A); Ginkgo + kind v0.31 (Layer B); Ginkgo + `//go:build live-e2e` (Layer C live) |
| **Config file** | `internal/controller/suite_test.go` (envtest); `test/integration/kind/suite_test.go` (kind); `test/e2e/suite_test.go` (live, plan 03-11) |
| **Quick run command** | `make test` (envtest unit + integration; ~30s) |
| **Full suite command** | `make test-int` (kind tier; ~3-5 min) |
| **Live nightly command** | `make test-e2e-live` (real Claude; ~$0.20-0.80/run; gated by ANTHROPIC_API_KEY) |
| **Estimated runtime** | ~30s quick / ~4 min full / ~5-10 min live |

See `03-RESEARCH.md` § Validation Architecture for per-deliverable Layer A / Layer B split.

---

## Sampling Rate

- **After every task commit:** Run `make test` (Layer A envtest — quick feedback)
- **After every plan wave:** Run `make test-int` (Layer B kind — full integration)
- **Before `/gsd-verify-work`:** Full suite must be green; chaos-resume spec must be green
- **Live nightly E2E (plan 03-11):** Run only via cron + `make test-e2e-live`; NEVER in PR or per-commit
- **Max feedback latency:** 30 seconds for unit/envtest; 5 minutes for kind integration; 10 minutes for live

---

## Per-Task Verification Map

Populated for all 11 plans. Each row pinned to: REQ-IDs covered, Layer A / B / C classification, automated command, and File Exists status.

| Task ID | Plan | Wave | Requirement | Threat Ref | Secure Behavior | Test Type | Automated Command | File Exists | Status |
|---------|------|------|-------------|------------|-----------------|-----------|-------------------|-------------|--------|
| 03-01-T1 | 03-01 | 1 | ART-03, ART-06, AUTH-01 | T-301..T-305 (envelope contract) | ProviderSpec + ChildCRDSpec types are typed not interface{}; JSON-tag-stable | Layer A unit | `go test ./pkg/dispatch/... -run "TestProviderSpec\|TestChildCRDSpec" -count=1 -timeout 30s` | ✅ pkg/dispatch/envelope_test.go (Phase 2; this task extends) | ⬜ pending |
| 03-01-T2 | 03-01 | 1 | ART-03, ART-06, AUTH-01 | T-305 (envelope cache-token leak) | EnvelopeIn.Provider/Role/Level + EnvelopeOut.ChildCRDs / git.headSHA / cache tokens — schema rev unchanged (additive); harness rejects unknown apiVersion | Layer A unit | `go test ./pkg/dispatch/... -run "TestEnvelopeIn\|TestEnvelopeOut" -count=1 -timeout 30s` | ✅ pkg/dispatch/envelope_test.go | ⬜ pending |
| 03-02-T1 | 03-02 | 1 | AUTH-01 | T-302 (Project schema injection) | Project.Spec.git.* + Spec.subagent.* schema additions with CEL validation rules; Project.Status.git.lastPushedSHA + PhasePushLeaseFailed condition vocabulary | Layer A envtest | `go test ./internal/controller/... -run "TestProjectCRDSchema\|TestProjectStatusVocabulary" -count=1 -timeout 30s` | ❌ W0 must create api/v1alpha1/project_types_test.go | ⬜ pending |
| 03-02-T2 | 03-02 | 1 | PERSIST-04, TEST-04 | T-313 (stub-subagent test mode injection) | wait-for-signal mode polls /workspace/envelopes/{task-uid}/release every 500ms (per Q4 RESOLVED) and emits canned envelope on file appearance | Layer A unit | `go test ./cmd/stub-subagent/... -run "TestWaitForSignalMode" -count=1 -timeout 30s` | ❌ W0 must create cmd/stub-subagent/main_wait_for_signal_test.go | ⬜ pending |
| 03-03-T1 | 03-03 | 2 | ART-03, ART-05, ART-06 | T-301 (PAT exfiltration) | Clone + Fetch + AddWorktree via go-git/v5 with `&http.BasicAuth{Username:"x-access-token", Password:pat}`; httptest-backed unit tests | Layer A unit | `go test ./pkg/git/... -run "TestClone\|TestFetch\|TestAddWorktree" -count=1 -timeout 60s` | ❌ W0 must create pkg/git/{clone,fetch,worktree}_test.go | ⬜ pending |
| 03-03-T2 | 03-03 | 2 | ART-06, ART-03 | T-303 (stale lease overwrites human commits — Pitfall 13) | Commit + AddPath + Push with ForceWithLease against lastPushedSHA; first push omits lease; lease-rejection error path verified | Layer A unit | `go test ./pkg/git/... -run "TestCommit\|TestPush\|TestAddPath" -count=1 -timeout 60s` | ❌ W0 must create pkg/git/{commit,push}_test.go | ⬜ pending |
| 03-04-T1 | 03-04 | 2 | ART-07 | T-304 (gitleaks rule overrides), Defense-in-depth | ScanDiff + LoadConfig + embedded gitleaks v8.30.1 default rules; sk-ant-, AKIA-, custom-rule composition all detected | Layer A unit | `go test ./internal/gitleaks/... -count=1 -timeout 30s` | ❌ W0 must create internal/gitleaks/scanner_test.go | ⬜ pending |
| 03-05-T1 | 03-05 | 2 | TEST-03 | T-306 (prompt template injection) | JSONL ReadLines + go:embed prompt-template loader; 4 minimal v1 templates; CRD-agnostic | Layer A unit | `go test ./internal/subagent/common/... -count=1 -timeout 30s` | ❌ W0 must create internal/subagent/common/{stream_reader,prompt_templates}_test.go | ⬜ pending |
| 03-05-T2 | 03-05 | 2 | TEST-03 | T-305 (host credential leak), T-307 (budget hiding via cache tokens) | Anthropic Subagent.Run with --bare flag; vendor-mismatch + params-allow-list (per Q3 RESOLVED: temperature/thinking_budget/top_p/top_k) fail-fast; snake_case→camelCase usage mapping for all 4 token types | Layer A unit | `go test ./internal/subagent/anthropic/... -count=1 -timeout 30s` | ❌ W0 must create internal/subagent/anthropic/{stream_parser,subagent}_test.go | ⬜ pending |
| 03-06-T1 | 03-06 | 3 | ART-04, ART-06, ART-07, AUTH-01 | T-301 (PAT exfiltration via log), T-303 (stale lease), T-302 (push race) | cmd/tide-push binary handles clone + push modes; exit-code map per Q5 RESOLVED (0/1/2/10/11/12/13); --commit-message + --artifact-paths args per D-B2 / W11; never-targets-main guard; Patch API for unified-diff format (W10) | Layer A unit + Dockerfile | `go test ./cmd/tide-push/... -count=1 -timeout 60s` | ❌ W0 must create cmd/tide-push/main_test.go | ⬜ pending |
| 03-06-T2 | 03-06 | 3 | ART-04 | T-301, T-302, T-303 | buildPushJob + buildCloneJob return correctly-shaped Jobs with deterministic names; dedicated tide-push SA; envFrom credsSecretRef; volume mounts with subPath | Layer A unit | `go test ./internal/controller/... -run "TestBuildPushJob\|TestBuildCloneJob" -count=1 -timeout 30s` | ❌ W0 must create internal/controller/push_helpers_test.go | ⬜ pending |
| 03-07-T1 | 03-07 | 3 | TEST-03 | T-305 (host credential leak via shim) | Thin shim (~50 LOC) wires anthropic.Run through internal/harness envelope IO; ignores env.Dev.TestMode | Layer A unit (fake exec) | `go test ./cmd/claude-subagent/... -count=1 -timeout 30s` | ❌ W0 must create cmd/claude-subagent/main_test.go | ⬜ pending |
| 03-07-T2 | 03-07 | 3 | TEST-03 | T-305 (host credential leak via image) | Dockerfile uses node:22-slim + @anthropic-ai/claude-code@2.1.142 pin + USER 1000; no ~/.claude bind, no hardcoded API key | Manual visual (grep gates) | `grep -nE 'FROM node:22-slim\|claude-code@2\.1\.142\|^USER (1000\|node)' images/claude-subagent/Dockerfile` | ❌ images/claude-subagent/Dockerfile (NEW) | ⬜ pending |
| 03-07-T3 | 03-07 | 3 | TEST-03, ART-03 | T-306 (worktree provider-coupling), D-A4 invariant | EnsureWorktree calls pkggit.AddWorktree for executor Role; short-circuits for planner Role (D-A4); idempotent re-call | Layer A unit | `go test ./internal/harness/... -run "TestEnsureWorktree" -count=1 -timeout 30s` | ❌ W0 must create internal/harness/worktree_test.go | ⬜ pending |
| 03-08-T1 | 03-08 | 4 | ART-03, ART-04 | T-308 (childCRDs Kind injection), T-309 (pool deadlock) | dispatch_helpers: ResolveProvider precedence + BuildPlannerEnvelope + MaterializeChildCRDs with Kind allowlist {Milestone, Phase, Plan, Task, Wave}; idempotent on AlreadyExists | Layer A envtest | `go test ./internal/controller/... -run "TestResolveProvider\|TestBuildPlannerEnvelope\|TestMaterializeChildCRDs" -count=1 -timeout 30s` | ❌ W0 must create internal/controller/dispatch_helpers_test.go | ⬜ pending |
| 03-08-T2 | 03-08 | 4 | ART-03 | T-308, T-309 | Three up-stack reconciler bodies (Milestone/Phase/Plan) — planner dispatch via Dispatcher + child materialization; plannerPool acquisition; deterministic Job names tide-{level}-{uid}-{attempt}; existing Wave materialization preserved at Plan level | Layer A envtest | `go test ./internal/controller/... -run "TestMilestone\|TestPhase\|TestPlan" -count=1 -timeout 60s` | ✅ existing controller test files (Phase 1/2; tests extended here) | ⬜ pending |
| 03-08-T3 | 03-08 | 4 | ART-03, ART-04, ART-06 | T-302 (push race), T-303 (stale lease) | ProjectReconciler clone + push lifecycle + Unix-epoch branch name (no RFC3339); Status.git.LastPushedSHA writeback; bypass-push-lease annotation recovery | Layer A envtest | `go test ./internal/controller/... -run "TestProject" -count=1 -timeout 60s` | ✅ existing project_controller_test.go (Phase 1/2; extended) | ⬜ pending |
| 03-08-T3a | 03-08 | 4 | ART-06 | D-B2 / W11 commit-message protocol | buildCommitMessage helper produces all 4 D-B2 message shapes; PushOptions extended with CommitMessage + ArtifactPaths fields → Job Args | Layer A unit | `go test ./internal/controller/... -run "TestBuildCommitMessage\|TestBuildPushJobWithArtifacts" -count=1 -timeout 30s` | ❌ W0 push_helpers_test.go extended in this task | ⬜ pending |
| 03-09-T1 | 03-09 | 5 | ART-02, AUTH-01 | T-301 (PAT scope) | cmd/manager reads env vars for push image / claude image / per-level models / leader-election tuning; chart values.yaml exposes top-level keys; manager-deployment env injection | Manual Helm render | `helm template charts/tide --values charts/tide/values.yaml \| grep -cE 'TIDE_PUSH_IMAGE\|CLAUDE_SUBAGENT_IMAGE\|TIDE_DEFAULT_MODEL_MILESTONE'` | ✅ charts/tide/values.yaml (Phase 2; extended) | ⬜ pending |
| 03-09-T2 | 03-09 | 5 | ART-02, ART-05 | T-301 (PAT scope), T-304 (gitleaks override RBAC) | tide-push SA + Role + RoleBinding with least-privilege `secrets get`; docs/git-hosts.md covers GitHub/GitLab/Gitea/SSH | Helm render + markdown lint | `helm template charts/tide --values charts/tide/values.yaml \| grep -cE 'name: tide-push' && grep -cE '^## (GitHub\|GitLab\|Gitea\|SSH)' docs/git-hosts.md` | ❌ charts/tide/templates/push-rbac.yaml (NEW) + docs/git-hosts.md (NEW) | ⬜ pending |
| 03-10-T1 | 03-10 | 6 | PERSIST-04, TEST-04 | T-310 (test fixture creds leak), Chaos-resume regression | chaos_resume_test.go: 5 named pillar subtests (Job UID continuity / Attempt unchanged / Completed-set preserved / Observed completion / ComputeWaves golden-file invariant per W12); wait-for-signal stub fixture | Layer B kind | `make test-int 2>&1 \| grep -cE 'chaos-resume.*PASS'` | ❌ test/integration/kind/chaos_resume_test.go (NEW) | ⬜ pending |
| 03-10-T2 | 03-10 | 6 | TEST-04 | T-310 | push_lease_test.go (4 sub-cases: first-push/subsequent/stale-lease/bypass-recovery) + up_stack_dispatch_test.go (Milestone→Phase materialization with OwnerRef cascade); Makefile preloads tide-push image | Layer B kind | `make test-int 2>&1 \| grep -cE 'push lease.*PASS\|up-stack.*PASS'` | ❌ test/integration/kind/{push_lease,up_stack_dispatch}_test.go (NEW) | ⬜ pending |
| 03-11-T1 | 03-11 | 6 | TEST-03 | T-311 (real API key leak), T-312 (runaway budget), T-313 (fixture tampering) | Live E2E spec behind //go:build live-e2e; skip-on-missing-creds via t.Skip; asserts MILESTONE.md commit + Status.budget.usdSpent in (0, 1.00) range | Layer C live | `go test -tags=live-e2e ./test/e2e/... -timeout=15m` (live cost; nightly cron only) | ❌ test/e2e/live_claude_test.go (NEW) | ⬜ pending |
| 03-11-T2 | 03-11 | 6 | TEST-03 | T-314 (CI recipe loss) | Makefile test-e2e-live target with fail-fast on missing ANTHROPIC_API_KEY env; docs/live-e2e.md ships nightly CI recipe + fixture pin protocol + budget rationale + cost baseline | Manual visual + grep | `grep -nE '^test-e2e-live:' Makefile && grep -cE '^## (Overview\|Nightly CI Recipe\|Fixture Repo Pinning\|Budget Rationale\|Cost Baseline\|Troubleshooting)' docs/live-e2e.md` | ❌ Makefile target (NEW) + docs/live-e2e.md (NEW) | ⬜ pending |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*

---

## Wave 0 Requirements

Wave 0 = test scaffolding the planner has materialized in the plan tasks above. Each `❌ W0` annotation in the Per-Task Verification Map indicates a test file that gets created BY THE SAME TASK that ships the production code (test-first per `tdd="true"` annotations on each task). Once `make test` exits 0 after each task, the W0 obligation for that task is met.

Phase 3 Wave 0 inventory (all materialized by the task that ships the corresponding production code):

- [x] `pkg/git/git_test.go` — provided as `pkg/git/{clone,fetch,worktree,commit,push}_test.go` (plan 03-03 Task 1+2; httptest-backed)
- [x] `internal/gitleaks/gitleaks_test.go` — provided as `internal/gitleaks/scanner_test.go` (plan 03-04 Task 1)
- [x] `pkg/dispatch/envelope_test.go` extension — ProviderSpec + ChildCRDSpec + cache-token fields (plan 03-01 Task 1+2)
- [x] `internal/subagent/anthropic/parser_test.go` — provided as `internal/subagent/anthropic/stream_parser_test.go` (plan 03-05 Task 2; captured JSONL fixture; no live API call)
- [x] `test/integration/kind/chaos_resume_test.go` — Layer B four-pillar + 5th invariant Ginkgo assertions with wait-for-signal stub fixture (plan 03-10 Task 1; D-D2 mixed-state 3-task fixture)
- [x] `test/integration/kind/push_lease_test.go` — Layer B first-push lease semantics + stale-lease rejection + bypass-annotation recovery (plan 03-10 Task 2; Pitfall 13)
- [x] `test/integration/kind/up_stack_dispatch_test.go` — Layer B Milestone/Phase/Plan reconciler dispatch + childCRD materialization with OwnerRef cascade (plan 03-10 Task 2)
- [x] `test/e2e/live_claude_test.go` — Layer C live Claude E2E behind `//go:build live-e2e` (plan 03-11 Task 1; budget-capped at $1.00; nightly-only)
- [x] `internal/harness/worktree_test.go` — EnsureWorktree D-B4 wiring with mock pkggit.AddWorktree seam (plan 03-07 Task 3)
- [x] `internal/controller/push_helpers_test.go` — buildPushJob/buildCloneJob/buildCommitMessage pure-func tests (plan 03-06 Task 2 + plan 03-08 Task 3a)
- [x] `internal/controller/dispatch_helpers_test.go` — ResolveProvider/BuildPlannerEnvelope/MaterializeChildCRDs with Kind allowlist (plan 03-08 Task 1)

*All Wave 0 test scaffolding is owned by the same plan task that ships the production code — no separate Wave 0 dispatch needed.*

---

## Manual-Only Verifications

Phase 3 RESEARCH §"Validation Architecture" identifies two manual verifications:

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| Multi-host smoke (GitLab/Gitea generic HTTPS) | ART-02 | Phase 3 ships GitHub fixture only; GitLab/Gitea are documented "should-work" via the same generic HTTPS+PAT code path. Live verification requires real GitLab/Gitea credentials, deferred to v1.x | Manual smoke test recipe in `docs/git-hosts.md` (plan 03-09): apply a Project pointing at GitLab/Gitea remote, observe per-run branch lands |
| Live nightly E2E (real Claude) | TEST-03 | Real API key + non-zero cost; not runnable in PR CI or per-commit. Plan 03-11 ships the spec; this row tracks the nightly cron operator-responsibility | `ANTHROPIC_API_KEY=<real-key> make test-e2e-live` (incurs $0.20-0.80/run cost; recipe in docs/live-e2e.md) |

*All other phase behaviors have automated verification (Layer A envtest, Layer B kind int, or Layer C live e2e).*

---

## Validation Sign-Off

- [x] All tasks have `<automated>` verify or Wave 0 dependencies (verified — see Per-Task Verification Map)
- [x] Sampling continuity: no 3 consecutive tasks without automated verify (every task has an `<automated>` block per Nyquist Rule)
- [x] Wave 0 covers all MISSING references (each W0-annotated test file is owned by the same task that ships the production code)
- [x] No watch-mode flags
- [x] Feedback latency < 30s for Layer A, < 5min for Layer B, < 15min for Layer C (live nightly)
- [x] `nyquist_compliant: true` set in frontmatter

**Approval:** ready — gsd-plan-checker has validated the revision (revision iteration 1/3).
