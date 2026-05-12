---
phase: 1
slug: foundation-crds-pkg-dag-controller-scaffold
status: draft
nyquist_compliant: false
wave_0_complete: false
created: 2026-05-12
---

# Phase 1 — Validation Strategy

> Per-phase validation contract for feedback sampling during execution.
> Sourced from `01-RESEARCH.md` §"Validation Architecture".

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework (`pkg/dag`)** | stdlib `testing` v1.26; `t.Run` table tests |
| **Framework (controller suite)** | `github.com/onsi/ginkgo/v2@v2.28.x` + `github.com/onsi/gomega@latest` + `sigs.k8s.io/controller-runtime/pkg/envtest` |
| **Framework (analyzer)** | `golang.org/x/tools/go/analysis/analysistest` |
| **Framework (pool / owner / finalizer / config)** | stdlib `testing` (table tests; fake clients via `controller-runtime/pkg/client/fake`) |
| **Config file** | `internal/controller/suite_test.go` (kubebuilder-scaffolded; assertions hand-added) |
| **Quick run command** | `go test ./pkg/dag/... ./internal/pool/... ./internal/owner/... ./internal/finalizer/... ./tools/analyzers/...` (<5s, no envtest) |
| **Full suite command** | `make test` (invokes `setup-envtest`, runs Ginkgo + stdlib tests) |
| **Estimated runtime** | ~25s for full suite (TEST-01 budget: <30s on CI) |

---

## Sampling Rate

- **After every task commit:** Run quick command (`go test ./pkg/dag/... ./internal/pool/... ./internal/owner/... ./internal/finalizer/... ./tools/analyzers/...`)
- **After every plan wave:** Run `make test` (full envtest suite)
- **Before `/gsd:verify-work`:** `make lint && make test && make verify-dag-imports && make tide-lint` all green
- **Max feedback latency:** 30 seconds (TEST-01 hard cap)

---

## Per-Task Verification Map

> The planner populates this table from the per-task `<acceptance_criteria>` in each `XX-PLAN.md`.
> Each row maps one task to its REQ-ID(s) and the automated command that verifies it.

| Task ID | Plan | Wave | Requirement | Test Type | Automated Command | File Exists | Status |
|---------|------|------|-------------|-----------|-------------------|-------------|--------|
| (filled by planner) | | | | | | | ⬜ pending |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*

**Reference for the planner — 26 REQ-IDs and their verification mechanisms (from `01-RESEARCH.md`):**

- **CRD-01..06** — envtest integration (`go test ./internal/controller/... -run TestCRDsAccept|TestOwnerRefCascade|TestCELValidation|TestPlanValidatorNoOp|TestConversionRoundtrip`) + CI grep for RBAC wildcards
- **DAG-01..05** — stdlib `testing` table tests (`go test ./pkg/dag/...`); `make verify-dag-imports` for DAG-05 boundary
- **CTRL-01..05** — envtest integration (`TestManagerSetup|TestLeaderElection|TestMaxConcurrentReconcilesHonored|TestFinalizerLifecycle`) + static grep against `time.Sleep`/blocking
- **POOL-01..03** — unit tests (`internal/pool`, `tools/analyzers/crosspool`) + `make tide-lint` CI gate
- **AUTH-02..03** — CI grep checks against `config/rbac/role.yaml` (no `verbs="*"`, no non-leader-election ClusterRole)
- **PERSIST-01..02** — CI grep checks against `go.mod` (no SQLite/Postgres deps) and `api/v1alpha1/*_types.go` (no aggregate schedule fields)
- **BOOT-01..03** — manual checklist (`grep "M0" ROADMAP.md`) + static `groupversion_info.go` v1alpha1 verification
- **TEST-01** — CI workflow timing assertion (`time make test` < 30s)

---

## Wave 0 Requirements

Files that don't yet exist and must be created in Wave 0 (kubebuilder scaffold + skeleton infrastructure):

- [ ] `go.mod` + `go.sum` — `go 1.26`, module `github.com/jsquirrelz/tide`
- [ ] kubebuilder scaffold (`kubebuilder init` + `kubebuilder create api` ×6 + `kubebuilder create webhook` ×2)
- [ ] `pkg/dag/kahn.go` + `kahn_test.go` + `doc.go`
- [ ] `api/v1alpha1/*_types.go` (six Kinds) + `shared_types.go` (status condition vocabulary) + `groupversion_info.go`
- [ ] `internal/controller/*_controller.go` (six reconciler stubs at Standard depth)
- [ ] `internal/controller/suite_test.go` (Ginkgo + envtest harness)
- [ ] `internal/owner/owner.go` + `owner_test.go` (`EnsureOwnerRef` helper)
- [ ] `internal/finalizer/finalizer.go` + `finalizer_test.go` (`HandleDeletion` recipe)
- [ ] `internal/pool/pool.go` + `pool_test.go` (semaphore + `PreCharge`)
- [ ] `internal/config/config.go` + `config_test.go` (runtime config loader)
- [ ] `internal/webhook/v1alpha1/plan_webhook.go` (no-op validator + conversion stubs)
- [ ] `internal/webhook/v1alpha1/wave_webhook.go` (no-op validator)
- [ ] `cmd/manager/main.go` (Manager wiring; replaces kubebuilder default)
- [ ] `cmd/tide-lint/main.go` (singlechecker entrypoint)
- [ ] `tools/analyzers/crosspool/analyzer.go` + `analyzer_test.go` + `testdata/src/{valid,violation}/main.go`
- [ ] `config/samples/kustomization.yaml` + `tide_v1alpha1_*.yaml` (Project, Milestone, Phase, Plan, 8 Tasks α…θ)
- [ ] `Makefile` targets: `helm`, `helm-controller`, `helm-crds`, `lint`, `tide-lint`, `verify-dag-imports`, `verify-no-aggregates`, `verify-no-rbac-wildcards`, `test`
- [ ] `.github/workflows/ci.yaml` (go test + lint + tide-lint + grep checks + timing assertion)
- [ ] `charts/tide/` + `charts/tide-crds/` (helmify output, committed)

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| BOOT-01: M0 commitment marker is in `.planning/ROADMAP.md` | BOOT-01 | Documentation-as-state; verified once at phase init | `grep -E '^- \*\*M0' .planning/ROADMAP.md` returns the M0 line |
| BOOT-03: Single `v1alpha1` schema across the M0→M_self bridge | BOOT-03 | Cross-milestone invariant; verified at every CRD change | `grep -c '^// +kubebuilder:.*v1alpha1' api/v1alpha1/groupversion_info.go` returns exactly 1 |
| Vocabulary discipline (water/tide metaphor) in identifiers and log lines | (style/PROJECT.md) | Subjective; PR review catches drift | Reviewer scans diffs for naming consistency |

---

## Validation Sign-Off

- [ ] All tasks have `<automated>` verify or Wave 0 dependencies
- [ ] Sampling continuity: no 3 consecutive tasks without automated verify
- [ ] Wave 0 covers all MISSING references (kubebuilder scaffold + all skeleton files listed above)
- [ ] No watch-mode flags (TEST-01 requires bounded single-pass runtime)
- [ ] Feedback latency < 30s (quick command <5s; full suite <30s)
- [ ] `nyquist_compliant: true` set in frontmatter (after planner fills Per-Task Verification Map)

**Approval:** pending
