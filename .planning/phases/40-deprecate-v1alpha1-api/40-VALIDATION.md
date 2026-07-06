---
phase: 40
slug: deprecate-v1alpha1-api
status: planned
nyquist_compliant: true
wave_0_complete: false
created: 2026-07-06
updated: 2026-07-06
---

# Phase 40 — Validation Strategy

> Per-phase validation contract for feedback sampling during execution.

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework** | go test + Ginkgo v2.28 / Gomega (envtest Layer A; kind Layer B) |
| **Config file** | Makefile (test targets), test/integration/kind for Layer B |
| **Quick run command** | `go build ./... && make test` (unit + envtest tier) |
| **Full suite command** | `make test-int` (read MAKE_EXIT + grep '^--- FAIL', not just Ginkgo summary) |
| **Estimated runtime** | ~120 seconds (quick) / ~15+ min (full kind suite, one heavy run at a time) |

---

## Sampling Rate

- **After every task commit:** Run `go build ./... && make test`
- **After every plan wave:** Run `make test-int-fast` (Layer A envtest); `make test-int` at phase boundary only (constrained-VM recipe: fresh kind cluster per heavy run)
- **Before `/gsd:verify-work`:** Full suite must be green (plan 40-07 Task 2 is the phase gate)
- **Max feedback latency:** 180 seconds

---

## Per-Task Verification Map

| Task ID | Plan | Wave | Requirement | Threat Ref | Secure Behavior | Test Type | Automated Command | File Exists | Status |
|---------|------|------|-------------|------------|-----------------|-----------|-------------------|-------------|--------|
| 40-01-T1 | 40-01 | 1 | CRANK-01 | T-40-01/02 | v1alpha3 copy compiles; storage marker moved atomically; ModelSelection gone | build + grep | `go build ./api/... && make generate` + marker/field greps | ✅ mechanism exists | ⬜ pending |
| 40-01-T2 | 40-01 | 1 | CRANK-01 | T-40-03 | 3-version transitional CRDs, exactly one storage:true each; chart reproducible | unit + build-check | `go test ./api/v1alpha3/... && make manifests && make helm-crds && make verify-chart-reproducible` | ❌ Wave 0 — api/v1alpha3/schema_test.go created in-task | ⬜ pending |
| 40-02-T1 | 40-02 | 1 | CRANK-02 | T-40-04 | old CRD-group string rejected by ValidateAPIVersionKind | unit (RED→GREEN) | `go test ./pkg/dispatch/... && make verify-dispatch-imports` | ✅ envelope_test.go exists — extend | ⬜ pending |
| 40-02-T2 | 40-02 | 1 | CRANK-02 | T-40-04/05/06 | all envelope-literal copies move together; only owner-ref v1alpha1 literals remain | build + unit + grep | `go build ./... && make test` + envelope-literal grep | ✅ | ⬜ pending |
| 40-03-T1 | 40-03 | 2 | CRANK-03 | T-40-07/09/10 | consumers on v1alpha3; guard fail-closed on non-v1alpha3; credproxy gains no api/ import | build + vet + unit | `go build ./... && go vet ./... && make test` + credproxy grep | ✅ guard test exists — rewrite in-task | ⬜ pending |
| 40-03-T2 | 40-03 | 2 | CRANK-03 | T-40-08 | owner-ref single-GroupVersion; all live fixtures apply under v1alpha3 | unit + envtest | `make test && make test-int-fast` + fixture greps | ✅ | ⬜ pending |
| 40-04-T1 | 40-04 | 3 | CRANK-04 | T-40-11/13 | 5 dispatch surfaces resolve DECIDED override slots; dispatch identity unchanged | envtest (RED→GREEN, tdd) | `go test ./test/integration/envtest/... -run 'planner' -count=1 && make test` | ⚠️ extend planner_dispatch_test.go (task-level addition) | ⬜ pending |
| 40-04-T2 | 40-04 | 3 | CRANK-04 | T-40-12 | resolved model logged at all 5 dispatch sites | unit + grep count | `make test` + `grep -c 'resolved subagent dispatch'` == 5 | ✅ | ⬜ pending |
| 40-05-T1 | 40-05 | 3 | CRANK-05 | T-40-14/16 | packages deleted; aggregates gate provably alive (seeded exit-1); dogfood coverage relocated | unit + gate + liveness seed | `go build ./... && make test && make verify-no-aggregates && go test ./test/schema/...` | ❌ Wave 0 — test/schema/dogfood_manifests_test.go created in-task (relocation) | ⬜ pending |
| 40-05-T2 | 40-05 | 3 | CRANK-05 | T-40-15 | 6 single-version CRDs; chart reproducible | build-check + envtest | `make manifests && make helm-crds && make verify-chart-reproducible && make test-int-fast` | ✅ | ⬜ pending |
| 40-06-T1 | 40-06 | 3 | CRANK-06 | T-40-18 | migration chapter exists at the guard-constant path with remap table | file + grep gates | `test -f docs/migration/v1alpha2-to-v1alpha3.md` + content greps | ❌ new doc, created in-task | ⬜ pending |
| 40-06-T2 | 40-06 | 3 | CRANK-06 | T-40-17/18 | living docs on v1alpha3 + schemaRevision; snapshots untouched | grep gates | per-file v1alpha3-present / v1alpha1-absent bash loop | ✅ | ⬜ pending |
| 40-06-T3 | 40-06 | 3 | CRANK-06 | T-40-18 | 12 samples renamed + kustomization lockstep | ls + grep gates | rename-count + zero-legacy-grep bash assertion | ✅ | ⬜ pending |
| 40-07-T1 | 40-07 | 4 | CRANK-07 | T-40-20 | durable legacy-refs gate, CI-wired, seeded-failure proven | gate + liveness seed | `make verify-no-legacy-api-refs` + ci.yaml grep | ❌ new Makefile target, created in-task | ⬜ pending |
| 40-07-T2 | 40-07 | 4 | CRANK-07 | T-40-21 | full suite green with honest verdict protocol | full (Layer A + B) | verify suite + `make test-int` (MAKE_EXIT + '^--- FAIL' grep) | ✅ | ⬜ pending |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*

---

## Wave 0 Requirements

Wave-0-class gaps and where they land (each is created by the task that needs it, before its assertions run):

- [ ] `api/v1alpha3/schema_test.go` — created in 40-01-T2 (mirrors api/v1alpha2/schema_test.go reflect idiom)
- [ ] `test/schema/dogfood_manifests_test.go` — created in 40-05-T1 (relocation of api/v1alpha1/dogfood_manifests_test.go; stays in the unit tier — `go list ./...` filter excludes only /e2e and /test/integration)
- [ ] `pkg/dispatch/envelope_test.go` — EXISTS (RESEARCH's "verify at plan time" hedge resolved by PATTERNS: confirmed present); extended in 40-02-T1
- [ ] `verify-no-aggregates` hardened glob — lands in 40-05-T1, SAME commit as the api/ deletions (D-12 mandatory)
- [ ] End-state grep harness — `verify-no-legacy-api-refs` Makefile target created in 40-07-T1 (durable gate, not a one-off)

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| Reinstall path on a live cluster | CRANK-03/CRANK-06 | Requires a real cluster reinstall cycle | Substantially covered automatically: 40-07-T2's fresh-kind `make test-int` run IS a fresh cluster + v1alpha3-only CRDs + v1alpha3 objects reconciling end-to-end, and the guard envtest covers stale-revision rejection. Optional human walkthrough: fresh kind cluster → install tide-crds + tide charts → apply a Project WITHOUT schemaRevision → observe RequiresReinstall condition citing docs/migration/v1alpha2-to-v1alpha3.md → re-apply per the migration doc → Project reconciles. |

---

## Validation Sign-Off

- [x] All tasks have `<automated>` verify or Wave 0 dependencies
- [x] Sampling continuity: no 3 consecutive tasks without automated verify
- [x] Wave 0 covers all MISSING references
- [x] No watch-mode flags
- [x] Feedback latency < 180s
- [x] `nyquist_compliant: true` set in frontmatter

**Approval:** pending execution
