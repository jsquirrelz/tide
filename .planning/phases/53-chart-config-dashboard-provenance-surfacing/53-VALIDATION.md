---
phase: 53
slug: chart-config-dashboard-provenance-surfacing
status: planned
nyquist_compliant: true
wave_0_complete: false
created: 2026-07-21
---

# Phase 53 — Validation Strategy

> Per-phase validation contract for feedback sampling during execution.

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework** | go test (envtest via Ginkgo, helm-template contract tests, kind suite) + vitest (dashboard SPA) |
| **Config file** | Makefile targets (`test`, `test-int`, `lint`, `verify-dashboard-freshness`, `verify-chart-reproducible`) / `dashboard/web/vitest` config |
| **Quick run command** | `go test ./internal/controller/... ./cmd/dashboard/... ./pkg/dispatch/... && (cd dashboard/web && npx vitest run)` |
| **Full suite command** | `make test && make lint && make verify-chart-reproducible && make verify-dashboard-freshness && (cd dashboard/web && npm test) && make test-int` |
| **Estimated runtime** | quick ~90s · full ~15-25 min (kind suite dominates) |

---

## Sampling Rate

- **After every task commit:** Run the quick command scoped to the packages touched (the narrowest command in the map below)
- **After every plan wave:** Run `make test && make lint` (+ `verify-dashboard-freshness` when SPA files changed; + `make verify-chart-reproducible` + helm-template go tests when `hack/helm/` changed)
- **Before `/gsd:verify-work`:** Full suite must be green — MAKE_EXIT read explicitly, `grep -nE '^--- FAIL|^FAIL\s'` on the test-int log (CLAUDE.md discipline)
- **Max feedback latency:** 120 seconds

---

## Per-Task Verification Map

| Task ID | Plan | Wave | Requirement | Threat Ref | Secure Behavior | Test Type | Automated Command | File Exists | Status |
|---------|------|------|-------------|------------|-----------------|-----------|-------------------|-------------|--------|
| 53-01-T1 | 53-01 | 1 | CFG-01 | T-53-01 | chart supplies verifier image; regen reproducible | chart regen + render | `make helm-controller && git diff --exit-code charts/` | ✓ (targets exist) | ⬜ pending |
| 53-01-T2 | 53-01 | 1 | CFG-01 | T-53-02 | dropped augment block caught by fast test | go unit (no cluster) | `go test ./test/integration/kind/... -run TestHelmDeploymentTemplateRendersVerifierEnv -count=1` | ❌ Wave 0 (new file) | ⬜ pending |
| 53-02-T1 | 53-02 | 1 | CFG-01 | T-53-03 | malformed/out-of-range levels JSON rejected fail-closed | go unit (TDD) | `go test ./pkg/dispatch/... -run TestParseVerifyLevelDefaults -count=1` | ❌ Wave 0 (new file) | ⬜ pending |
| 53-02-T2 | 53-02 | 1 | CFG-01 | T-53-04 | absent config resolves OFF; authored > chart > off | go unit (own file, no Ginkgo filter) | `go test ./internal/controller/... -run TestVerificationEnabledForLevel -count=1` | ❌ Wave 0 (new file) | ⬜ pending |
| 53-03-T1 | 53-03 | 1 | OBS-04 | T-53-07 | verdict-final-only task staging | go unit | `go test ./internal/controller/... -run TestCollectStageEnvelopes -count=1` | ✓ extend | ⬜ pending |
| 53-03-T2 | 53-03 | 1 | OBS-04 | T-53-05 | closed kind allowlist preserved (traversal 400s) | go unit | `go test ./cmd/dashboard/api/... -run TestArtifacts -count=1` | ✓ extend | ⬜ pending |
| 53-04-T1 | 53-04 | 1 | OBS-04 | T-53-08 | LOOP-03: no history array on the wire; attemptMax≠verifyMaxIterations | go unit | `go test ./cmd/dashboard/api/... -run TestTask -count=1` | ✓ extend | ⬜ pending |
| 53-04-T2 | 53-04 | 1 | OBS-04 | T-53-09 | condition whitelist stays literal 3-way | go unit | `go test ./cmd/dashboard/api/... -run 'TestPlan|TestProject|TestSummarize' -count=1` | ✓ extend | ⬜ pending |
| 53-05-T1 | 53-05 | 2 | CFG-01, CFG-02 | T-53-11/12 | string-enum posture; upgrade renders OFF | chart regen + helm renders | acceptance renders in 53-05 Task 1 | ✓ (helm present) | ⬜ pending |
| 53-05-T2 | 53-05 | 2 | CFG-02 | T-53-12 | install-ON/upgrade-OFF + both override directions pinned | go unit (helm template, no cluster) | `go test ./test/integration/kind/... -run TestHelmDeploymentTemplateVerifyPostureInstallVsUpgrade -count=1` | ❌ Wave 0 (extends 53-01 file) | ⬜ pending |
| 53-06-T1 | 53-06 | 2 | CFG-01 | T-53-14 | one enablement helper ANDed at exactly 3 sites | go build + full controller suite | `go build ./... && go test ./internal/controller/... -count=1` | ✓ extend | ⬜ pending |
| 53-06-T2 | 53-06 | 2 | CFG-01, CFG-02 | T-53-13 | chart cannot re-open maxIter=0 clamp | go unit | `go test ./internal/controller/... -run 'TestResolveLoopPolicy|TestVerificationEnabledForLevel' -count=1` | ✓ extend | ⬜ pending |
| 53-07-T1 | 53-07 | 2 | OBS-04 | T-53-16 | unknown status/condition strings degrade gracefully; VerifyHalted ≠ Failed on 3 axes | vitest | `cd dashboard/web && npx vitest run StatusBadge ConditionBadge` | ✓ extend | ⬜ pending |
| 53-07-T2 | 53-07 | 2 | OBS-04 | — | wire types byte-match Go tags; embed fresh | tsc + vitest + freshness gate | `cd dashboard/web && npx tsc --noEmit -p . && npx vitest run && cd ../.. && make verify-dashboard-freshness` | ✓ | ⬜ pending |
| 53-08-T1 | 53-08 | 3 | OBS-04 | T-53-19/20 | findings rendered text-only (React escaping); clipboard-copy actions only | vitest | `cd dashboard/web && npx vitest run TaskDetailDrawer` | ❌ Wave 0 (new test file) | ⬜ pending |
| 53-08-T2 | 53-08 | 3 | OBS-04 | — | plan mirror; absence renders nothing; embed fresh | tsc + vitest + freshness gate | `cd dashboard/web && npx tsc --noEmit -p . && npx vitest run && cd ../.. && make verify-dashboard-freshness` | ✓ | ⬜ pending |
| 53-09-T1 | 53-09 | 4 | CFG-02 | T-53-21/22 | sticky posture proven live, isolated from shared release | kind (Ginkgo Label kind, Serial) | `go test ./test/integration/kind/... -run TestIntegrationKind --ginkgo.focus='sticky posture' -count=1` | ❌ Wave 0 (new file) | ⬜ pending |
| 53-09-T2 | 53-09 | 4 | CFG-01, CFG-02, OBS-04 | — | D-10 ci.yaml-only gates green in-phase | full gates | `make lint && make verify-chart-reproducible && make verify-dashboard-freshness && (cd dashboard/web && npm test) && make test-int` (MAKE_EXIT + FAIL-grep) | ✓ | ⬜ pending |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*

---

## Wave 0 Requirements

Created by the plan that needs them (each plan's first verify writes its own test file — no shared blocking scaffold):

- [ ] `test/integration/kind/verify_chart_config_test.go` — 53-01 T2 creates; 53-05 T2 extends (render pair)
- [ ] `pkg/dispatch/verify_defaults_test.go` — 53-02 T1 (TDD RED first)
- [ ] `internal/controller/verification_enabled_unit_test.go` — 53-02 T2 (own Test entry, never a TestControllers filter — Phase 51-03 lesson)
- [ ] `dashboard/web/src/components/TaskDetailDrawer.test.tsx` — 53-08 T1 (confirmed absent 2026-07-21)
- [ ] `test/integration/kind/verify_posture_sticky_test.go` — 53-09 T1

*Existing infrastructure (envtest suite, kind suite, vitest, helm-template contract tests, artifact_push/artifacts/tasks/plans/projects test files, StatusBadge/ConditionBadge test files) covers the remainder — verified present 2026-07-21.*

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| Dashboard provenance renders legibly on a live cluster | OBS-04 | Visual quality judgment beyond DOM assertions | Deploy dashboard on kind, open a Task with loop iterations, inspect drawer + badges (defer to phase verification / UAT — not a plan gate) |

---

## Validation Sign-Off

- [x] All tasks have `<automated>` verify or Wave 0 dependencies
- [x] Sampling continuity: no 3 consecutive tasks without automated verify
- [x] Wave 0 covers all MISSING references
- [x] No watch-mode flags
- [x] Feedback latency < 120s (narrowest-command discipline per task)
- [x] `nyquist_compliant: true` set in frontmatter

**Approval:** planned 2026-07-21 (planner-filled from 53-01..53-09 PLAN.md tasks)
