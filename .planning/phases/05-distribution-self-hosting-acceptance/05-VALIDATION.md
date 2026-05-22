---
phase: 5
slug: distribution-self-hosting-acceptance
status: draft
nyquist_compliant: false
wave_0_complete: false
created: 2026-05-22
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

> Filled by planner during PLAN.md authoring. Each task that lands code/config/docs must list its automated verify command or declare a Wave 0 dependency that supplies it.

| Task ID | Plan | Wave | Requirement | Threat Ref | Secure Behavior | Test Type | Automated Command | File Exists | Status |
|---------|------|------|-------------|------------|-----------------|-----------|-------------------|-------------|--------|
| 05-XX-YY | XX | N | DIST-01 | — | Helm chart pair lints + renders cleanly | smoke | `helm lint charts/tide && helm lint charts/tide-crds && helm template charts/tide --set dashboard.enabled=true > /dev/null` | ✅ (ci.yaml `helm-lint` job) | ⬜ pending |
| 05-XX-YY | XX | N | DIST-01 / AUTH-02 | — | `per-namespace-rolebinding.yaml` renders with non-empty `projectNamespaces` | unit (helm template render) | `helm template charts/tide --set projectNamespaces='{ns1,ns2}' \| grep -c '^kind: RoleBinding'` (expects ≥ 2) | ❌ Wave 0 (new `hack/scripts/test-per-ns-rb.sh`) | ⬜ pending |
| 05-XX-YY | XX | N | DIST-02 | — | helmify reproduces `charts/` tree (no drift) | integration (CI gate) | `make helm && git diff --exit-code charts/` | ✅ (ci.yaml `helm-lint` job, line 152-159) | ⬜ pending |
| 05-XX-YY | XX | N | DIST-03 | — | LICENSE file is canonical Apache-2.0 boilerplate | unit (sha256 match) | `sha256sum LICENSE \| awk '{print $1}'` matches canonical hash | ❌ Wave 0 (new `hack/scripts/verify-license.sh`) | ⬜ pending |
| 05-XX-YY | XX | N | DIST-03 | — | NOTICE file present + non-empty (k8s.io/* Apache deps require) | unit | `test -s NOTICE` | ❌ Wave 0 | ⬜ pending |
| 05-XX-YY | XX | N | DIST-03 | — | Every Go file has Apache-2.0 header | grep gate | `find . -name '*.go' -not -path './vendor/*' -not -path '*/testdata/*' \| xargs grep -L "Apache License, Version 2.0" \| (! grep .)` | ❌ Wave 0 (kubebuilder boilerplate.go.txt; planner verifies coverage) | ⬜ pending |
| 05-XX-YY | XX | N | DIST-04 | — | All 5 new docs exist + non-empty | smoke | `for f in docs/INSTALL.md docs/project-authoring.md docs/troubleshooting.md docs/rbac.md docs/README.md; do test -s "$f"; done` | ❌ Wave 0 | ⬜ pending |
| 05-XX-YY | XX | N | DIST-04 | — | `docs/README.md` index links to all 10 docs | unit | `grep -c '](.*\.md)' docs/README.md` (expects 10) | ❌ Wave 0 (new `hack/scripts/verify-docs-coverage.sh`) | ⬜ pending |
| 05-XX-YY | XX | N | DIST-04 | — | `examples/projects/{small,medium,large}/project.yaml` all valid YAML + `tideproject.k8s/v1alpha1` | unit | `for d in examples/projects/{small,medium,large}; do kubectl apply --dry-run=client -f "$d/project.yaml"; done` | ❌ Wave 0 | ⬜ pending |
| 05-XX-YY | XX | N | DIST-04 | — | Quickstart prepended at TOP of README (≤ 40 lines from top) | grep | `head -40 README.md \| grep -qE 'kind create cluster\|helm install'` | ❌ Wave 0 | ⬜ pending |
| 05-XX-YY | XX | N | DIST-05 | — | `make dry-run-v1` completes < 30 min on ubuntu:24.04 (Docker-in-Docker) | E2E (slow — release-time only) | `make dry-run-v1` exits 0 + `jq '.totalSeconds < 1800' dry-run-report.json` | ❌ Wave 0 (new `hack/scripts/dry-run-v1.sh` + `hack/scripts/render-dry-run-report.sh`) | ⬜ pending |
| 05-XX-YY | XX | N | DIST-05 | — | `dry-run-report.json` conforms to schemaVersion 1 | unit | `jq '.schemaVersion == 1 and (.totalSeconds \| type) == "number" and (.kindVersion \| type) == "string"' dry-run-report.json` | ❌ Wave 0 | ⬜ pending |
| 05-XX-YY | XX | N | BOOT-02 / BOOT-04 | — | `make acceptance-v1` ritual produces `Project.Status.Phase == Complete` | manual-only (D-A4 locks maintainer-only) | `make acceptance-v1` exits 0 + `hack/scripts/acceptance-verify.sh` 7-check passes | ❌ Wave 0 (new `hack/scripts/acceptance-v1.sh` + `hack/scripts/acceptance-verify.sh`) | ⬜ pending |
| 05-XX-YY | XX | N | BOOT-02 / BOOT-04 | — | All 7 D-A3 pass criteria automated (branch + commits + status + zero errors + no orphans + gitleaks + under budget) | unit (shell script) | `hack/scripts/acceptance-verify.sh .acceptance-runs/<latest>/` exit 0 | ❌ Wave 0 | ⬜ pending |
| 05-XX-YY | XX | N | AUTH-02 catch-up | — | `per-namespace-rolebinding.yaml` renders correct `subjects` + `roleRef` | unit | `helm template charts/tide --set projectNamespaces='{tide-acme}' \| yq '.subjects[0].namespace'` == `tide-system` | ❌ Wave 0 | ⬜ pending |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*

---

## Wave 0 Requirements

Wave 0 lands the testing infrastructure scripts/binaries the verification commands above depend on. Planner sequences Wave 0 BEFORE any feature plan that needs the corresponding command.

### Scripts under `hack/scripts/` (new)

- [ ] `hack/scripts/dry-run-v1.sh` — DIST-05 driver. Docker-in-Docker, ubuntu:24.04, runs Quickstart commands verbatim, times each phase, writes `dry-run-report.json`.
- [ ] `hack/scripts/render-dry-run-report.sh` — DIST-05 report shaper. Emits `dry-run-report.json` with `schemaVersion: 1`, per-phase elapsed seconds, kind/helm/kube versions.
- [ ] `hack/scripts/acceptance-v1.sh` — BOOT-04 driver. Maintainer ritual: spin fresh kind, helm install, apply `examples/projects/large/project.yaml`, watch to completion, capture evidence under `.acceptance-runs/<timestamp>/`.
- [ ] `hack/scripts/acceptance-verify.sh` — BOOT-04 verifier. Runs the 7 D-A3 pass checks against a captured run.
- [ ] `hack/scripts/test-per-ns-rb.sh` — DIST-01 + AUTH-02 helm template render assertion.
- [ ] `hack/scripts/verify-license.sh` — DIST-03 LICENSE + NOTICE + Go-header coverage check.
- [ ] `hack/scripts/verify-docs-coverage.sh` — DIST-04 `docs/README.md` index completeness check.

### New Go binary

- [ ] `cmd/tide-demo-init/main.go` — medium-sample bootstrap (Topic 4 of RESEARCH.md — runs as in-cluster Job, initializes bare local-only git repo on aux PVC from `examples/tide-demo-fixture/`).
- [ ] `cmd/tide-demo-init/main_test.go` — bootstrap unit test (verifies bare repo init + content unpack).

### Sample artifacts (new)

- [ ] `examples/projects/{small,medium,large}/project.yaml` × 3 — three sample Project CRDs (stub / mini / acceptance).
- [ ] `examples/projects/{small,medium,large}/README.md` × 3 — per-sample setup/teardown notes.
- [ ] `examples/tide-demo-fixture/{main.go,main_test.go,go.mod,go.sum,README.md}` — scaffold content for medium sample's local-only git remote.

### Makefile targets (new)

- [ ] `make dry-run-v1` — wraps `hack/scripts/dry-run-v1.sh`.
- [ ] `make acceptance-v1` — wraps `hack/scripts/acceptance-v1.sh`.
- [ ] `make helm-lint-validate` — existing target (Phases 1-4) used in this phase's full-suite command; no change needed.

### CRD subchart safety annotation (Wave 0 critical — Research finding #5)

- [ ] All 6 CRD templates under `charts/tide-crds/templates/` get `metadata.annotations.helm.sh/resource-policy: keep` to prevent catastrophic `helm uninstall tide-crds` data loss.
- [ ] `hack/helm/augment-tide-crds-chart.sh` updated to preserve the annotation through `make helm` regeneration.

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| TIDE-on-TIDE acceptance run produces a useful authored phase | BOOT-04 D-A4 | Live LLM cost ($25 cap) + maintainer-grade outcome judgment; not CI-budgetable | `make acceptance-v1` with maintainer's `ANTHROPIC_API_KEY` env; review the authored `internal/subagent/openai/` skeleton on the per-run branch |
| Dashboard renders the acceptance run live | D-A3 (optional 8th check) | Requires browser + Chrome DevTools MCP attached; not a CI artifact | Maintainer captures screenshot during/after run; attaches to release notes |
| "Is TIDE right for me?" docs framing reads well to fresh K8s operators | DIST-04 + Pitfall 24 | Subjective copy quality; structural automated checks (file exists, link count) don't measure clarity | Maintainer + 1 friend-of-project cold-read pass before v1.0 tag (informal, not gating) |

---

## Validation Sign-Off

- [ ] All tasks have `<automated>` verify or Wave 0 dependencies declared
- [ ] Sampling continuity: no 3 consecutive tasks without automated verify
- [ ] Wave 0 covers all MISSING references (the 12 ❌ items in Wave 0 Requirements above)
- [ ] No watch-mode flags (all commands single-shot, exit code semantics)
- [ ] Feedback latency < 60s for unit/lint, < 5min for wave gate
- [ ] CRD subchart `helm.sh/resource-policy: keep` annotation lands in Wave 0
- [ ] `nyquist_compliant: true` set in frontmatter

**Approval:** pending — flips to `approved 2026-MM-DD` when planner finishes mapping every task to a verify column above.
