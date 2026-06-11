---
phase: 06
slug: v1-image-publish-and-ship-readiness-revalidation
status: draft
nyquist_compliant: false
wave_0_complete: false
created: 2026-05-30
---

# Phase 06 — Validation Strategy

> Per-phase validation contract for feedback sampling during execution.
> Phase 6 is a CI/infra + shell-script phase — validation is **bash assertions + `grep` exit codes + `helm template` + `kubectl wait`**, NOT Go `_test.go` units. Source: 06-RESEARCH.md §"Validation Architecture".

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework** | bash assertions + `kubectl wait` + `grep` exit codes + `docker buildx --load` snapshot (no Go `_test.go` for Phase 6) |
| **Config file** | none — script-based validation |
| **Quick run command** | `grep -cE 'v0\.1\.0-dev' charts/tide/values.yaml hack/helm/tide-values.yaml` + `helm template charts/tide \| grep -E 'image:' \| grep -v '1\.0\.0\|1\.36'` |
| **Full suite command** | `ACCEPTANCE_SAMPLE=small make acceptance-v1` (the $0 BOOT-04 gate — D-05) |
| **Estimated runtime** | quick: < 5s · full $0 BOOT-04: ~10-15 min |

---

## Sampling Rate

- **After every task commit:** `grep -cE 'v0\.1\.0-dev' charts/tide/values.yaml` (< 1s) + (where chart touched) `helm template charts/tide | grep -E 'image:' | grep -v '1\.0\.0\|1\.36'` (should be empty)
- **After every plan wave:** `ACCEPTANCE_SAMPLE=small make acceptance-v1` (full $0 BOOT-04 end-to-end)
- **Before `/gsd-verify-work`:** Full $0 BOOT-04 run green
- **Max feedback latency:** quick assertions < 5s; full gate ~15 min

---

## Per-Task Verification Map

> Mapped at requirement level (plan/wave assigned by the planner). Every REQ-ID has an automated, **$0**, no-published-image-required command.

| Req ID | Wave | Requirement | Threat Ref | Behavior / Secure Behavior | Test Type | Automated Command | File Exists | Status |
|--------|------|-------------|------------|----------------------------|-----------|-------------------|-------------|--------|
| IMG-01 | by planner | 6 images build multi-arch (amd64+arm64), correct ghcr names | T-06 partial-manifest | snapshot build, no push | `docker buildx build --platform linux/amd64,linux/arm64 -f <dockerfile> .` per image (or `make docker-buildx-snapshot`) | ❌ W0 (new make target) | ⬜ pending |
| CHART-01 | by planner | no `v0.1.0-dev` tags remain | — | dead pins removed | `grep -cE 'v0\.1\.0-dev' charts/tide/values.yaml hack/helm/tide-values.yaml` == 0 | ✅ (command is the criterion) | ⬜ pending |
| CHART-01 | by planner | all 6 TIDE images resolve to `1.0.0`; third-party `1.36` preserved | — | tag alignment | `helm template charts/tide \| grep -E 'image:' \| grep -v '1\.0\.0\|1\.36'` returns empty | ✅ | ⬜ pending |
| DRY-01 | by planner | cert-manager bring-up in dry-run-v1 | — | no `cert-manager.io/v1 Certificate` deadlock | `grep -cE 'cert-manager' hack/scripts/dry-run-v1.sh` ≥ 1 | ❌ W0 (script edit) | ⬜ pending |
| IMG-LOAD-01 | by planner | auto-detect builds+loads 6 images, pods Running | — | no ImagePullBackOff with no published images | embedded in `ACCEPTANCE_SAMPLE=small make acceptance-v1`; helper `load-images-if-needed.sh` calls `kind load docker-image ... --name <cluster>` directly (NOT via `test-int-kind-prep`, which hardcodes `--name tide-test` — RESEARCH P2) | ❌ W0 (new helper) | ⬜ pending |
| ACC-01 | by planner | D-06 criteria all pass at $0 | — | controller Available + dashboard Running + small Project terminal + zero ERROR logs + no orphan Jobs + no ImagePullBackOff | `ACCEPTANCE_SAMPLE=small make acceptance-v1` exit 0 | ❌ W0 (new $0 mode) | ⬜ pending |
| DOC-01 | by planner | no uncorrected premature ship-ready claim; publish + fallback documented | — | doc correctness | manual review + `grep -riE 'ship.?ready' README.md docs/INSTALL.md` reconciled | ✅ (manual + grep) | ⬜ pending |
| HYG-01 | by planner | `.acceptance-runs/` ignored | — | worktree hygiene | `git check-ignore .acceptance-runs/` returns the path | ❌ W0 (gitignore edit) | ⬜ pending |
| HYG-01 | by planner | ImagePullBackOff troubleshooting entry | — | operator recipe present | `grep -ciE 'ImagePullBackOff' docs/troubleshooting.md` ≥ 1 | ❌ W0 (doc edit) | ⬜ pending |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*

---

## Wave 0 Requirements

- [ ] `hack/scripts/load-images-if-needed.sh` — new shared auto-detect helper (IMG-LOAD-01); calls `kind load docker-image ... --name ${cluster}` directly, NOT via `test-int-kind-prep` (RESEARCH P2: that target hardcodes `--name tide-test`).
- [ ] `make acceptance-v1-smoke` or `ACCEPTANCE_SAMPLE=small` mode in `acceptance-v1.sh` (ACC-01 / D-05).
- [ ] `make docker-buildx-snapshot` — optional but useful for IMG-01 pre-CI multi-arch verification.
- [ ] `.acceptance-runs/` line in `.gitignore` (HYG-01).
- [ ] ImagePullBackOff row in `docs/troubleshooting.md` (HYG-01).
- [ ] Reconcile `examples/projects/small/project.yaml` stub tag `v1.0.0` → `1.0.0` (RESEARCH A7) so the kind-loaded/chart-resolved tags match.

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| The real `v1.0.0` published-image multi-arch manifest on ghcr.io | IMG-01 | Requires cutting the `v*` tag (post-phase ship action — out of Phase 6 scope) | After tagging: `docker manifest inspect ghcr.io/jsquirrelz/tide-controller:1.0.0` shows amd64+arm64 |
| Ship-state doc-claim correctness | DOC-01 | Prose review — no machine assertion for "premature claim removed" | Maintainer reads README/INSTALL.md; confirms no pre-publish "v1.0 ship-ready" assertion |

---

## Validation Sign-Off

- [ ] All requirements have an automated `$0` verify command or Wave 0 dependency
- [ ] Sampling continuity: chart/script edits guarded by grep+helm assertions per commit
- [ ] Wave 0 covers all MISSING helpers/targets (`load-images-if-needed.sh`, `$0` acceptance mode, gitignore, troubleshooting)
- [ ] No watch-mode flags
- [ ] Feedback latency < 15 min (full $0 gate)
- [ ] `nyquist_compliant: true` set in frontmatter

**Approval:** pending
