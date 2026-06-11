# Phase 6: v1.0 Image-Publish Pipeline & Ship-Readiness Revalidation — Specification

**Created:** 2026-05-30
**Ambiguity score:** 0.12 (gate: ≤ 0.20)
**Requirements:** 7 locked

## Goal

Every Docker image the `charts/tide` chart references is buildable and publishable from a real pipeline, the chart's component tags resolve to the chart `appVersion` instead of the dead `v0.1.0-dev` pin, and the BOOT-04 operator ritual completes end-to-end green at **$0** (locally-built + kind-loaded images, no real LLM spend) — closing the image-publish gap that the 2026-05-30 BOOT-04 retry exposed.

## Background

Phase 5 closed 2026-05-23 claiming "v1.0 ship-ready," but BOOT-04 (the gate that would have caught the gap) is operator-only by D-A4 design and did not run end-to-end until 2026-05-30. Two background acceptance attempts cascaded that day:

- **cascade-1** (fixed, commit `adb1053`): `make acceptance-v1` deadlocked on `no matches for kind "Certificate" in version "cert-manager.io/v1"` — `acceptance-v1.sh` jumped straight to `helm install tide` with no cert-manager bring-up. Quick task `260530-h2h` mirrored the Layer B integration pattern into `acceptance-v1.sh` only.
- **cascade-2** (this phase): both helm installs reported `deployed`, then `kubectl wait deploy/tide-controller-manager` timed out. `kubectl describe pod` showed `tide-controller-manager` **ImagePullBackOff** on `ghcr.io/jsquirrelz/tide-controller:v0.1.0-dev` and `tide-dashboard` **ImagePullBackOff** on `ghcr.io/jsquirrelz/tide-dashboard:1.0.0`. Neither image — nor any of the 6 chart-referenced component images — has ever been published.

Verified against live code (2026-05-30):

- **No image-publish pipeline.** `.goreleaser.yaml` builds only the `tide` CLI binary (`grep -cE '^dockers:|^docker_manifests:' .goreleaser.yaml` → `0`); zero image-publish steps across all 7 `.github/workflows/` files.
- **Chart tags drifted.** Both the SOT `hack/helm/tide-values.yaml` and the generated `charts/tide/values.yaml` hardcode 5 component tags at `v0.1.0-dev` (lines 39/140/144/155/165); only `dashboard` (`tag: ""`, line 244) resolves to `.Chart.AppVersion` (`1.0.0`). SOT line 148 (`tag: "1.36"`) is a **third-party** image and must remain untouched.
- **`dry-run-v1.sh` has the same cert-manager gap** (`grep cert-manager hack/scripts/dry-run-v1.sh` → 0 hits) and will deadlock identically once a `v*-rc.*` tag is cut. Neither script has any local image build/load path.

The 6 components and their chart repositories (confirmed):
`tide-controller` (`charts/tide/values.yaml:38`), `tide-stub-subagent` (139), `tide-credproxy` (143), `tide-push` (154), `tide-claude-subagent` (164), `tide-dashboard` (243) — all under `ghcr.io/jsquirrelz/`.

Phase 6 is the catch-up phase. It does **not** reopen Phase 5; Phase 5's shipped deliverables stand. Full scope-of-record: `06-FINDINGS.md`.

## Requirements

1. **IMG-01 — Multi-arch image build-and-publish pipeline for all 6 components**: A CI-triggered pipeline builds and pushes every chart-referenced component image.
   - Current: `.goreleaser.yaml` builds only the `tide` CLI; no `dockers:`/`docker_manifests:` section; zero image-publish steps across `.github/workflows/`. None of the 6 component images exists on ghcr.io.
   - Target: A `v*`-tag-gated pipeline (mechanism — goreleaser-native `dockers:`+`docker_manifests:` vs a separate `docker-build-push` workflow — is a discuss-phase HOW decision) builds and pushes `ghcr.io/jsquirrelz/{tide-controller, tide-dashboard, tide-stub-subagent, tide-credproxy, tide-push, tide-claude-subagent}`, multi-arch (`linux/amd64` + `linux/arm64`), tagged from the release version so the tag matches the chart `appVersion`.
   - Acceptance: A snapshot/dry-run build (no push, no tag required) produces all 6 images with correct `ghcr.io/jsquirrelz/tide-*` names, each carrying both `amd64` and `arm64`; the real push step is wired and gated on `v*` tag push. (Actually cutting the tag + pushing is OUT of Phase 6 — see Boundaries.)

2. **CHART-01 — Chart image-tag SOT alignment**: The 5 hardcoded `v0.1.0-dev` component tags default to the chart `appVersion`.
   - Current: SOT `hack/helm/tide-values.yaml` + generated `charts/tide/values.yaml` pin 5 component tags at `v0.1.0-dev` (lines 39/140/144/155/165); only dashboard (`""`) resolves to `appVersion` `1.0.0`.
   - Target: Set the 5 TIDE-component tags in the SOT to `""` (matching the dashboard pattern), re-run `bash hack/helm/augment-tide-chart.sh` (or `make helm`) to regenerate `charts/tide/values.yaml`. The third-party image at SOT line 148 (`tag: "1.36"`) is preserved unchanged. Per the chart-vs-binary anti-pattern: SOT edited first, generated chart catches up — `charts/tide/values.yaml` is NOT the edit surface.
   - Acceptance: `grep -cE 'v0\.1\.0-dev' charts/tide/values.yaml hack/helm/tide-values.yaml` returns `0`; `helm template charts/tide | grep -E 'image:'` shows all 6 TIDE component images resolving to tag `1.0.0`; the third-party `1.36` image still present and unchanged.

3. **DRY-01 — `dry-run-v1.sh` cert-manager bring-up**: The rc-tag dry-run path installs cert-manager before `helm install tide`.
   - Current: `hack/scripts/dry-run-v1.sh` runs `kind create cluster` → `helm install tide-crds` → `helm install tide` with zero cert-manager bring-up; it will hit the identical `cert-manager.io/v1 Certificate` "no matches" deadlock cascade-1 hit.
   - Target: Mirror the cert-manager install + per-deployment `rollout status` already in `acceptance-v1.sh` (commit `adb1053`) into `dry-run-v1.sh` between cluster-create and helm-install; preserve the `TIDE_CERT_MANAGER_VERSION` override convention (default `v1.20.2`).
   - Acceptance: `grep -cE 'cert-manager' hack/scripts/dry-run-v1.sh` ≥ 1; a `dry-run-v1` invocation reaches `helm install tide` without the `no matches for kind "Certificate"` error.

4. **IMG-LOAD-01 — Auto-detect local image-load fallback in both scripts**: `acceptance-v1.sh` and `dry-run-v1.sh` use published images when present and build+load locally when not.
   - Current: Neither script has any local build/load path; both assume the 6 images exist on ghcr.io and hit `ImagePullBackOff` when they don't (the cascade-2 failure).
   - Target: Both scripts gain an auto-detect path — attempt to pull each of the 6 component images; for any that fails to pull, build it locally and `kind load docker-image` it into the cluster. Works pre-tag (build+load) and post-tag (pull) with **no operator flags**.
   - Acceptance: Run against a cluster with NO published images present → all 6 are built + kind-loaded and the controller-manager + dashboard pods reach `Running` (no `ImagePullBackOff`); run against published images → images are pulled, not rebuilt.

5. **ACC-01 — BOOT-04 $0 end-to-end revalidation (closeout gate)**: The operator ritual completes green with no real LLM spend.
   - Current: BOOT-04 has never completed end-to-end — the 2026-05-30 attempts cascaded on cert-manager (cascade-1, fixed) then ImagePullBackOff (cascade-2).
   - Target: `make acceptance-v1` (BOOT-04 ritual) completes end-to-end at **$0** using locally-built + kind-loaded images against a stub/small sample: cert-manager up → both helm installs `deployed` → `deploy/tide-controller-manager` Available + dashboard `Running` → sample Project drives to its expected terminal phase. No real Anthropic spend inside Phase 6.
   - Acceptance: A documented $0 BOOT-04 run reaches controller-manager `Available` + dashboard `Running` + the sample Project at its expected terminal status, exit code `0`, captured in the phase VERIFICATION/transcript.

6. **DOC-01 — Ship-state doc corrections + maintainer image-publish docs**: Premature ship claims are corrected and the publish path is documented.
   - Current: README/INSTALL.md may carry premature "v1.0 ship-ready" claims; INSTALL.md documents the cert-manager prereq but not the image-publish step; no maintainer doc explains how the 6 images get published or pre-verified.
   - Target: Remove or qualify any premature "v1.0 ship-ready" assertion; document the image-publish pipeline and the auto-detect local-image fallback in INSTALL.md / maintainer docs so the next operator can pre-verify before tagging.
   - Acceptance: No uncorrected pre-publish "v1.0 ship-ready" claim remains; INSTALL.md (or a maintainer doc) describes how the 6 images publish and how the local-image fallback behaves.

7. **HYG-01 — Add-on hygiene (the two in-scope add-ons)**: Worktree clutter is ignored and the failure has a troubleshooting entry.
   - Current: `.acceptance-runs/` (today's failed-run output) is untracked and clutters the worktree; `docs/troubleshooting.md` has no entry for ImagePullBackOff-mid-install.
   - Target: Add `.acceptance-runs/` to `.gitignore`; append an `ImagePullBackOff` mid-install entry to `docs/troubleshooting.md` with the recipe (check ghcr.io image existence + check chart tag pins).
   - Acceptance: `git check-ignore .acceptance-runs/` returns the path; `grep -ciE 'ImagePullBackOff' docs/troubleshooting.md` ≥ 1.

## Boundaries

**In scope:**
- Multi-arch (amd64+arm64) build-and-publish pipeline for all 6 chart-referenced component images (config + snapshot/local build verified; real push wired for tag time)
- Chart component-tag SOT alignment (5 `v0.1.0-dev` tags → `appVersion`)
- `dry-run-v1.sh` cert-manager install + rollout-wait (mirror of `acceptance-v1.sh`)
- Auto-detect (pull-then-build) local image-load fallback in both `acceptance-v1.sh` and `dry-run-v1.sh`
- BOOT-04 $0 local-image end-to-end revalidation (the closeout gate)
- Ship-state doc corrections + maintainer image-publish documentation
- `.acceptance-runs/` `.gitignore` entry + `docs/troubleshooting.md` ImagePullBackOff-mid-install entry

**Out of scope:**
- **Cutting the `v1.0.0` tag and running the $25 real-LLM published-image acceptance run** — post-phase ship action per the locked closeout-gate decision (Phase 6 closes on the $0 local-image path)
- Phase 5 reopen — Phase 5's shipped deliverables (LICENSE, NOTICE, docs, samples, chart additions, release.yaml chain, BOOT-02/04 plumbing) all stand
- New chart features beyond tag alignment — chart contract is fixed per the chart-vs-binary anti-pattern
- Multi-version chart distribution — carry-forward from Phase 5; v1.0 ships exactly one chart version
- Cosign / SLSA / supply-chain signing — v1.x scope
- OperatorHub / OLM bundle submission — v1.x
- CI-ification of `acceptance-v1` (real-LLM in CI) — D-A4 keeps it operator-only
- Conversion-webhook activation — `v1alpha1` stays the hub
- `test-int-kind-prep` cluster-name parameterization — deferred to backlog (user decision)
- `make doctor` preflight target — deferred to backlog (user decision)

## Constraints

- **Multi-arch:** all 6 images build for `linux/amd64` + `linux/arm64`.
- **Tagging:** image tags derive from the release version and resolve to the chart `appVersion` (`1.0.0`) — consistent with the dashboard's existing `tag: ""` → `.Chart.AppVersion` convention.
- **Registry:** `ghcr.io/jsquirrelz/`.
- **cert-manager:** pinned `v1.20.2` with `TIDE_CERT_MANAGER_VERSION` override (mirror of `acceptance-v1.sh`).
- **Chart-vs-binary anti-pattern (binding, per CLAUDE.md):** edit the SOT `hack/helm/tide-values.yaml` first, then propagate via `make helm`; `charts/tide/values.yaml` is generated output, NOT the edit surface; SOT line 148's third-party `1.36` tag must be preserved.
- **Publish trigger:** the real push is gated on `v*` tag push and is exercised post-phase; Phase 6 verifies the pipeline via snapshot/local build only.
- **$0 closeout:** no real Anthropic spend inside Phase 6.
- **Auto-detect image-load:** pull-then-build, no operator flags required.

## Acceptance Criteria

- [ ] A snapshot/dry-run build produces all 6 images named `ghcr.io/jsquirrelz/tide-{controller,dashboard,stub-subagent,credproxy,push,claude-subagent}`, each multi-arch (amd64+arm64); the real push step is wired and gated on `v*` tag push
- [ ] `grep -cE 'v0\.1\.0-dev' charts/tide/values.yaml hack/helm/tide-values.yaml` returns `0`; `helm template charts/tide | grep -E 'image:'` shows all 6 TIDE images at tag `1.0.0`; the third-party `1.36` image is unchanged
- [ ] `grep -cE 'cert-manager' hack/scripts/dry-run-v1.sh` ≥ 1 and a `dry-run-v1` run reaches `helm install tide` without `no matches for kind "Certificate"`
- [ ] With no published images present, `acceptance-v1` + `dry-run-v1` build + kind-load all 6 and pods reach `Running` (no `ImagePullBackOff`); with published images present, they pull (no rebuild)
- [ ] A $0 BOOT-04 run reaches `deploy/tide-controller-manager` Available + dashboard `Running` + the sample Project at its expected terminal status, exit code `0`, captured in the phase transcript
- [ ] No uncorrected pre-publish "v1.0 ship-ready" claim remains; INSTALL.md / maintainer docs cover image publish + local-image fallback
- [ ] `git check-ignore .acceptance-runs/` returns the path; `docs/troubleshooting.md` has an ImagePullBackOff-mid-install entry

## Ambiguity Report

| Dimension          | Score | Min  | Status | Notes                                                        |
|--------------------|-------|------|--------|--------------------------------------------------------------|
| Goal Clarity       | 0.90  | 0.75 | ✓      | Publish 6 images, align tags, fix dry-run, $0 BOOT-04 green  |
| Boundary Clarity   | 0.92  | 0.70 | ✓      | Add-on scope + closeout boundary locked; $25 run is OUT      |
| Constraint Clarity | 0.82  | 0.65 | ✓      | Multi-arch, appVersion tag, cert-manager v1.20.2, SOT-first  |
| Acceptance Criteria| 0.88  | 0.70 | ✓      | $0 local-image BOOT-04 is the falsifiable closeout gate      |
| **Ambiguity**      | 0.12  | ≤0.20| ✓      |                                                              |

Status: ✓ = met minimum, ⚠ = below minimum (planner treats as assumption)

All dimensions cleared their minimums. No requirement is flagged as an assumption.

## Interview Log

| Round | Perspective              | Question summary                                  | Decision locked                                                                 |
|-------|--------------------------|---------------------------------------------------|---------------------------------------------------------------------------------|
| 0     | Researcher (FINDINGS)    | What exists today / what's the gap?               | Grounded by 06-FINDINGS.md + live-code verification; all 3 findings confirmed    |
| 1     | Boundary + Failure + Seed| What is the Phase 6 closeout gate?                | **$0 local-image BOOT-04** — the $25 published-image run + v1.0.0 tag is post-phase |
| 1     | Boundary Keeper          | Which optional add-ons are IN scope?              | `.acceptance-runs/` gitignore + ImagePullBackOff troubleshooting doc IN; kind cluster-name param + `make doctor` deferred to backlog |
| 1     | Failure Analyst          | Local-image behavior when nothing is published?   | **Auto-detect** — pull published images, fall back to local build + kind-load; no flags |

The Researcher/Simplifier/Boundary groundwork was pre-resolved by `06-FINDINGS.md` (authored as scope-of-record by quick task 260530-hrc) plus direct live-code verification, so the single live round targeted the three genuinely-open decisions. Mechanism choices flagged in FINDINGS (goreleaser-native vs separate workflow) are deferred to `/gsd-discuss-phase` as HOW decisions.

---

*Phase: 06-v1-image-publish-and-ship-readiness-revalidation*
*Spec created: 2026-05-30*
*Next step: /gsd-discuss-phase 06 — implementation decisions (goreleaser `dockers:` vs separate workflow, per-image build mechanics, auto-detect implementation)*
