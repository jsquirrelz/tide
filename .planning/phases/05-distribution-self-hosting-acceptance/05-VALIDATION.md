---
phase: 5
slug: distribution-self-hosting-acceptance
status: draft
nyquist_compliant: true
wave_0_complete: true
created: 2026-05-22
revised: 2026-05-22
---

# Phase 5 — Validation Strategy

> Per-phase validation contract for feedback sampling during execution.
> Derived from `05-RESEARCH.md` §"Validation Architecture" (lines 936–991).

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework (Go)** | Ginkgo v2 + Gomega (existing — Phases 1-4 already pinned) |
| **Framework (shell)** | bash + `jq` + `grep` + `kubectl` + `helm` + `sha256sum`/`shasum` |
| **Framework (chart)** | `helm lint` + `helm template` + `git diff --exit-code` (existing in `ci.yaml`) |
| **Config files** | `Makefile` (existing — extended with `dry-run-v1`/`acceptance-v1` targets), `.github/workflows/release.yaml` (modified — helmify + dry-run + OCI push), `.github/workflows/ci.yaml` (unchanged) |
| **Quick run command** | `make test && make lint` — < 60s (no Phase 5 Go changes; shell linting only) |
| **Full suite command** | `make test && make test-int && make helm-lint-validate && make dry-run-v1` — full Phase 5 gate (~15-20 min including DinD-based dry-run) |
| **Estimated runtime** | Quick: < 60s • Full: 15-20 min • Dry-run gate alone: 8-30 min |

---

## Sampling Rate

- **After every task commit:** Run `make test && make lint` (< 60s, hard ceiling)
- **After every plan wave:** Run `make test-int && make helm-lint-validate` (~5 min)
- **Before `/gsd-verify-work`:** Full suite must be green: `make test && make test-int && make helm-lint-validate && make dry-run-v1`
- **Before `git tag v1.0.0-rc.1`:** Full suite + `make acceptance-v1` (maintainer ritual)
- **Max feedback latency:** 60s for unit/lint; 5min wave; 20min phase-gate

---

## Per-Task Verification Map

> Populated post-revision with the actual `plan-task` IDs across all 17 PLAN.md files.

| Task ID | Plan | Wave | Requirement | Threat Ref | Secure Behavior | Test Type | Automated Command | File Exists | Status |
|---------|------|------|-------------|------------|-----------------|-----------|-------------------|-------------|--------|
| 05-13-02 | 13 | 3 | DIST-01 | T-05-13-01 | Helm chart pair lints + renders cleanly | smoke | `helm lint charts/tide && helm lint charts/tide-crds && helm template charts/tide --set dashboard.enabled=true > /dev/null` | ✅ (ci.yaml `helm-lint` job) | ⬜ pending |
| 05-13-02 | 13 | 3 | DIST-01 / AUTH-02 | T-05-13-01 | `per-namespace-rolebinding.yaml` renders with non-empty `projectNamespaces` | unit (helm template render) | `helm template charts/tide --set projectNamespaces='{ns1,ns2}' \| grep -c '^kind: RoleBinding'` (expects ≥ 2) | ✅ Wave 3 (new `hack/scripts/test-per-ns-rb.sh` in Plan 05-13) | ⬜ pending |
| 05-16-01 | 16 | 5 | DIST-02 | T-05-16-03 | helmify reproduces `charts/` tree (no drift) | integration (CI gate) | `make helm && git diff --exit-code charts/` | ✅ (ci.yaml `helm-lint` job, line 152-159; release.yaml helmify-verify job added Plan 05-16) | ⬜ pending |
| 05-01-01 | 01 | 1 | DIST-03 | T-05-01-01 | LICENSE file is canonical Apache-2.0 boilerplate | unit (grep gate) | `grep -q "Apache License" LICENSE && grep -q "Copyright 2026 The TIDE Authors" LICENSE` | ✅ Wave 1 (new `hack/scripts/verify-license.sh` in Plan 05-01) | ⬜ pending |
| 05-01-01 | 01 | 1 | DIST-03 | T-05-01-02 | NOTICE file present + non-empty (k8s.io/* Apache deps require) | unit | `test -s NOTICE && grep -cE "Apache-2.0" NOTICE` (≥ 3) | ✅ Wave 1 (Plan 05-01) | ⬜ pending |
| 05-01-02 | 01 | 1 | DIST-03 | T-05-01-03 | Every Go file has Apache-2.0 header | grep gate | `find . -name '*.go' -not -path './vendor/*' -not -path '*/testdata/*' \| xargs grep -L "Apache License, Version 2.0" \| (! grep .)` | ✅ Wave 1 (kubebuilder boilerplate.go.txt; Plan 05-01 verifier) | ⬜ pending |
| 05-04-01, 05-04-02, 05-07-01, 05-08-01, 05-09-01, 05-10-01 | 04, 07, 08, 09, 10 | 1+2 | DIST-04 | T-05-04-01 | All 5 new docs exist + non-empty (INSTALL, project-authoring, troubleshooting, rbac, concepts) | smoke | `for f in docs/INSTALL.md docs/project-authoring.md docs/troubleshooting.md docs/rbac.md docs/concepts.md docs/README.md; do test -s "$f"; done` | ✅ Wave 1+2 (Plans 04, 07, 08, 09, 10) | ⬜ pending |
| 05-04-03 | 04 | 2 | DIST-04 | T-05-04-01 | `docs/README.md` index links to all 11 docs (12 files — entry #4 co-located) | unit (strict mode) | `bash hack/scripts/verify-docs-coverage.sh --strict` (expects 11 entries, 12 files present) | ✅ Wave 2 (new `hack/scripts/verify-docs-coverage.sh` in Plan 05-04) | ⬜ pending |
| 05-11-01, 05-11-02, 05-12-02 | 11, 12 | 2, 3 | DIST-04 | T-05-11-02 | `examples/projects/{small,medium,large}/project.yaml` all valid YAML + `tideproject.k8s/v1alpha1` | unit | `for d in examples/projects/{small,medium,large}; do yq eval '.' "$d/project.yaml" > /dev/null; done` | ✅ Wave 2+3 (Plans 05-11 + 05-12) | ⬜ pending |
| 05-03-01 | 03 | 1 | DIST-04 | T-05-03-01 | Quickstart prepended at TOP of README (≤ 40 lines from top) | grep | `head -40 README.md \| grep -qE 'kind create cluster\|helm install'` | ✅ Wave 1 (Plan 05-03) | ⬜ pending |
| 05-15-01 | 15 | 4 | DIST-05 | T-05-16-05 | `make dry-run-v1` completes < 30 min on ubuntu:24.04 (Docker-in-Docker) | E2E (slow — release-time only) | `make dry-run-v1` exits 0 + `jq '.totalSeconds < 1800' dry-run-report.json` | ✅ Wave 4 (new `hack/scripts/dry-run-v1.sh` + `hack/scripts/render-dry-run-report.sh` in Plan 05-15) | ⬜ pending |
| 05-15-01 | 15 | 4 | DIST-05 | T-05-15-01 | `dry-run-report.json` conforms to schemaVersion 1 | unit | `jq '.schemaVersion == 1 and (.totalSeconds \| type) == "number" and (.kindVersion \| type) == "string"' dry-run-report.json` | ✅ Wave 4 (Plan 05-15) | ⬜ pending |
| 05-15-02 | 15 | 4 | BOOT-02 / BOOT-04 | T-05-15-01 | `make acceptance-v1` ritual produces `Project.Status.Phase == Complete` | manual-only (D-A4 locks maintainer-only) | `make acceptance-v1` exits 0 + `hack/scripts/acceptance-verify.sh` 7-check passes (Single Phase scope: 3 of 4 commit shapes) | ✅ Wave 4 (new `hack/scripts/acceptance-v1.sh` + `hack/scripts/acceptance-verify.sh` in Plan 05-15) | ⬜ pending |
| 05-15-02 | 15 | 4 | BOOT-02 / BOOT-04 | T-05-15-05 | All 7 D-A3 pass criteria automated (branch + 3-of-4 commits per D-A1 Single Phase + status + zero errors + no orphans + gitleaks + under budget) | unit (shell script) | `hack/scripts/acceptance-verify.sh .acceptance-runs/<latest>/` exit 0 | ✅ Wave 4 (Plan 05-15) | ⬜ pending |
| 05-13-02 | 13 | 3 | AUTH-02 catch-up | T-05-13-01 | `per-namespace-rolebinding.yaml` renders correct `subjects` + `roleRef` | unit | `helm template charts/tide --set projectNamespaces='{tide-acme}' \| grep -A 5 'subjects:' \| grep -q 'namespace: tide-system'` | ✅ Wave 3 (Plan 05-13) | ⬜ pending |
| 05-14-01 | 14 | 1 | DIST-01 | T-05-14-01 | All 6 CRD templates carry `helm.sh/resource-policy: keep` annotation (Pitfall 2 mitigation) | unit | `[ "$(helm template charts/tide-crds \| grep -c 'helm.sh/resource-policy: keep')" -eq 6 ]` | ✅ Wave 1 (Plan 05-14 — MOVED here from Wave 2 per HIGH-2 revision) | ⬜ pending |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*

---

## Wave 0 Requirements

Wave 0 lands the testing infrastructure scripts/binaries the verification commands above depend on. Planner sequences Wave 0 BEFORE any feature plan that needs the corresponding command.

> All Wave 0 deliverables are now mapped into specific plans below — `wave_0_complete: true` in frontmatter.

### Scripts under `hack/scripts/` (new)

- [x] `hack/scripts/dry-run-v1.sh` — DIST-05 driver. Lands in Plan 05-15 (Wave 4).
- [x] `hack/scripts/render-dry-run-report.sh` — DIST-05 report shaper. Plan 05-15 (Wave 4).
- [x] `hack/scripts/acceptance-v1.sh` — BOOT-04 driver. Plan 05-15 (Wave 4).
- [x] `hack/scripts/acceptance-verify.sh` — BOOT-04 verifier. Plan 05-15 (Wave 4).
- [x] `hack/scripts/test-per-ns-rb.sh` — DIST-01 + AUTH-02 helm template render assertion. Plan 05-13 (Wave 2).
- [x] `hack/scripts/verify-license.sh` — DIST-03 LICENSE + NOTICE + Go-header coverage check. Plan 05-01 (Wave 1).
- [x] `hack/scripts/verify-docs-coverage.sh` — DIST-04 `docs/README.md` index completeness check (default + `--strict` modes per LOW-15 revision). Plan 05-04 (Wave 2).

### New Go binary

- [x] `cmd/tide-demo-init/main.go` — medium-sample bootstrap. Plan 05-12 Task 1 (Wave 3).
- [x] `cmd/tide-demo-init/main_test.go` — bootstrap unit test. Plan 05-12 Task 1 (Wave 3).

### Sample artifacts (new)

- [x] `examples/projects/{small,large}/project.yaml` — Plan 05-11 (Wave 2).
- [x] `examples/projects/medium/project.yaml` — Plan 05-12 Task 2 (Wave 3).
- [x] `examples/projects/{small,medium,large}/README.md` × 3 — Plans 05-11 + 05-12.
- [x] `examples/tide-demo-fixture/{main.go,main_test.go,go.mod,go.sum,README.md}` — Plan 05-06 (Wave 1).

### Makefile targets (new)

- [x] `make verify-license` — Plan 05-01 (Wave 1, adds `Makefile` target; HIGH-5 revision adds Makefile to files_modified).
- [x] `make verify-docs` — Plan 05-04 (Wave 2; depends on Plan 05-01's Makefile edit).
- [x] `make test-per-ns-rb` — Plan 05-13 (Wave 2).
- [x] `make dry-run-v1` — Plan 05-15 (Wave 4).
- [x] `make acceptance-v1` — Plan 05-15 (Wave 4).
- [x] `make helm-lint-validate` — existing target (Phases 1-4) used in this phase's full-suite command; no change needed.

### CRD subchart safety annotation (Wave 1 critical — Research finding #5)

- [x] All 6 CRD templates under `charts/tide-crds/templates/` get `metadata.annotations.helm.sh/resource-policy: keep` to prevent catastrophic `helm uninstall tide-crds` data loss. Plan 05-14 (MOVED from Wave 2 to Wave 1 per HIGH-2 revision).
- [x] `hack/helm/augment-tide-crds-chart.sh` updated to preserve the annotation through `make helm` regeneration. Plan 05-14.

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| TIDE-on-TIDE acceptance run produces a useful authored phase | BOOT-04 D-A4 | Live LLM cost ($25 cap) + maintainer-grade outcome judgment; not CI-budgetable | `make acceptance-v1` with maintainer's `ANTHROPIC_API_KEY` env; review the authored `internal/subagent/openai/` skeleton on the per-run branch |
| Dashboard renders the acceptance run live | D-A3 (optional 8th check) | Requires browser + Chrome DevTools MCP attached; not a CI artifact | Maintainer captures screenshot during/after run; attaches to release notes |
| "Is TIDE right for me?" docs framing reads well to fresh K8s operators | DIST-04 + Pitfall 24 | Subjective copy quality; structural automated checks (file exists, link count) don't measure clarity | Maintainer + 1 friend-of-project cold-read pass before v1.0 tag (informal, not gating) |

---

## Validation Sign-Off

- [x] All tasks have `<automated>` verify or Wave 0 dependencies declared
- [x] Sampling continuity: no 3 consecutive tasks without automated verify
- [x] Wave 0 covers all MISSING references (all 12 items mapped above)
- [x] No watch-mode flags (all commands single-shot, exit code semantics)
- [x] Feedback latency < 60s for unit/lint, < 5min for wave gate
- [x] CRD subchart `helm.sh/resource-policy: keep` annotation lands in Wave 1 (Plan 05-14 moved per HIGH-2)
- [x] `nyquist_compliant: true` set in frontmatter

**Approval:** approved 2026-05-22 — planner finished mapping every task to a verify column above after iteration 2 revisions (HIGH-1 fix).
