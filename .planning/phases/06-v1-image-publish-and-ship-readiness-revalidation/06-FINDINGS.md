---
phase: 06-v1-image-publish-and-ship-readiness-revalidation
type: findings
status: draft
opened: 2026-05-30
opened_by: quick task 260530-hrc
tags: [phase-open, image-publish, boot-04-revalidation, v1-ship-readiness, findings]
supersedes_premature_closure: phase 5 (closed 2026-05-23 — deliverables shipped, gap not surfaced until 2026-05-30 BOOT-04 retry)
---

# Phase 6 — v1.0 Image-Publish Pipeline & Ship-Readiness Revalidation

## Scope of record (DRAFT)

This document is the scope-of-record for the SPEC/DISCUSS/PLAN sessions that follow. Every proposed requirement below is labeled **DRAFT** — final REQ-IDs and final scope come from `/gsd-discuss-phase 06`, not this doc. Phase 6 exists because Phase 5's "v1.0 ship-ready" claim was premature: the deliverables Phase 5 actually shipped (LICENSE, NOTICE, 5 new docs, 3-sample cost spectrum, chart version 1.0.0 lockstep, per-namespace-rolebinding, resource-policy:keep, dry-run.yaml, release.yaml chain, BOOT-02 + BOOT-04 plumbing) are intact and stay shipped — but BOOT-04 (the gate that should have caught the image-publish gap) is operator-only by D-A4 design and did not run end-to-end until 2026-05-30. Phase 6 is the catch-up phase, not a Phase 5 reopen.

## What happened today (2026-05-30)

Two background acceptance attempts cascaded today.

**cascade-1** — BG task `bess2gftr` (kickoff 2026-05-30T16:09:57Z; failure logged at T16:13:45Z): `make acceptance-v1` failed mid-`helm install tide` with `no matches for kind "Certificate" in version "cert-manager.io/v1"`. The tide chart's webhook + metrics Certificates require cert-manager CRDs to be in the cluster, but `hack/scripts/acceptance-v1.sh` jumped straight from `kind create cluster` to `helm install tide` with no cert-manager bring-up. Quick task `260530-h2h` (commits `adb1053` `fix(quick-260530-h2h): install cert-manager v1.20.2 before helm install tide in acceptance-v1.sh` + `7d3af9d` `docs(quick-260530-h2h): document cert-manager v1.20.2 prerequisite in INSTALL.md`) closed it by mirroring the Layer B integration test pattern at `test/integration/kind/suite_test.go:329-369`.

**cascade-2** — BG task `bs3ntw3rt` (kickoff 2026-05-30T16:25:00Z) progressed past cert-manager. Both helm installs reported success:

```
helm install tide-crds → STATUS: deployed
helm install tide      → STATUS: deployed
```

The next gate failed:

```
kubectl wait --for=condition=Available deploy/tide-controller-manager -n tide-system --timeout=5m
error: timed out waiting for the condition on deployments/tide-controller-manager
```

`kubectl describe pod` showed:

- `tide-controller-manager-*`: Pending, 0/1 ready, **ImagePullBackOff** on `ghcr.io/jsquirrelz/tide-controller:v0.1.0-dev`
- `tide-dashboard-d675d58d4-s2cfc`: Pending, **ImagePullBackOff** on `ghcr.io/jsquirrelz/tide-dashboard:1.0.0`

Neither image exists on ghcr.io. The chart references 6 component images total; none of them have ever been published by any workflow.

## Root cause

Three concrete findings — each with file/line references the next session can grep against.

**Finding 1 — No image-publish workflow exists.** `.goreleaser.yaml:30-51` defines one `builds:` entry for the `tide` CLI binary only. There is no `dockers:` section, no `docker_manifests:`. Confirmable by `grep -cE '^dockers:|^docker_manifests:' .goreleaser.yaml` → `0`. `grep -lrnE 'docker push|docker buildx|ko build|kaniko' .github/workflows/` returns zero hits across all 7 workflow files (`ci.yaml`, `dry-run.yaml`, `lint.yml`, `live-e2e.yml`, `release.yaml`, `test-e2e.yml`, `test.yml`). After tagging `v1.0.0`, the published charts will reference 6 images that don't exist on ghcr.io.

**Finding 2 — Chart `values.yaml` component tags drifted relative to dashboard.** Plan 05-05 bumped the Helm chart version `0.1.0-dev` → `1.0.0` lockstep, but `charts/tide/values.yaml` (the hand-maintained chart contract per CLAUDE.md's chart-vs-binary anti-pattern) still hardcodes 5 component tags at `v0.1.0-dev`:

- `controllerManager.manager.image.tag: v0.1.0-dev` (`charts/tide/values.yaml:39`)
- `images.stubSubagent.tag: v0.1.0-dev` (`charts/tide/values.yaml:140`)
- `images.credProxy.tag: v0.1.0-dev` (`charts/tide/values.yaml:144`)
- `images.tidePush.tag: v0.1.0-dev` (`charts/tide/values.yaml:155`)
- `images.claudeSubagent.tag: v0.1.0-dev` (`charts/tide/values.yaml:165`)

Only `dashboard.image.tag: ""` (`charts/tide/values.yaml:244`) defaults to `.Chart.AppVersion` and resolves to `1.0.0`. This explains the `kubectl describe pod` output exactly — the controller pod pulled `v0.1.0-dev` and the dashboard pod pulled `1.0.0`; both nonexistent. The SOT lives at `hack/helm/tide-values.yaml`; per Phase 02.2's chart-vs-binary anti-pattern, the fix lands in SOT first then propagates via `make helm` (binary catches up to chart, never reverse — `charts/tide/values.yaml` is **not** the edit surface).

**Finding 3 — `dry-run-v1.sh` also lacks cert-manager — Door 3 doesn't work either.** `hack/scripts/dry-run-v1.sh:80-82` issues `kind create cluster --name tide-dry-run` → `helm install tide-crds` → `helm install tide` with no cert-manager bring-up between them. The same `cert-manager.io/v1 Certificate` "no matches" deadlock from cascade-1 will hit the rc-tag dry-run path (Door 3 of the v1.0 ship decision tree) once a `v*-rc.*` tag is cut. Quick task `260530-h2h` only fixed `acceptance-v1.sh` because that was the script the operator was invoking; the `dry-run-v1.sh` gap is structurally identical and was not in scope for `260530-h2h`. Phase 6 picks it up. Additionally, once cert-manager is bolted on, `dry-run-v1.sh` will then hit the **same** ImagePullBackOff deadlock — so cert-manager + image-load fallback need to land together for Door 3.

## Deeper lesson — Phase 5 closure was premature

Phase 5 closed 2026-05-23 claiming v1.0 ship-ready. The plumbing for BOOT-02 / BOOT-04 shipped (Plan 05-15: `make dry-run-v1` + `make acceptance-v1` + 4 hack/scripts). What Phase 5 did NOT ship: a workflow that actually publishes the 6 component Docker images those scripts assume exist. Plan 05-16 wired chart-publish + helmify-verify + release pipeline extensions, but the assumption that the controller / dashboard / 4 sidecar images "will be there when the chart says they should be" was never validated — because the workflow that puts them there was never authored.

The structural failure is D-A4 by design — BOOT-04 is operator-only, no CI integration ($25 real-money Anthropic spend per run). The gate that should have caught both gaps (cert-manager + image-publish) was in the operator's hands; the operator (the user) is exercising it for the first time on 2026-05-30. This is not an indictment of D-A4 — CI-ifying `acceptance-v1` has its own non-trivial tradeoffs. It IS an indictment of not having run the operator ritual once internally before declaring ship-readiness, even as a dry-run against the small/medium samples that don't burn LLM cost.

Phase 6 is the catch-up. It does NOT reopen Phase 5 — Phase 5's actually-shipped deliverables (LICENSE, NOTICE, docs, samples, chart additions, release.yaml chain, BOOT-02/BOOT-04 plumbing) all stand. Phase 6 plugs the missing image-publish piece + the dry-run-v1.sh cert-manager mirror + the chart-tag SOT alignment + an image-load fallback for the local-cluster case where published images don't yet exist.

## What's already in main

The next session must NOT re-author any of these — they're already done. Phase 6 plans build on this baseline.

| Commit  | Subject                                                                                              | Scope                                                      |
| ------- | ---------------------------------------------------------------------------------------------------- | ---------------------------------------------------------- |
| `489dd71` | `style(quick-260526-w11): gofmt cmd/dashboard/api/{plans,tasks}.go struct alignment`                 | Phase 5 closeout polish (Plan 05-17 deferred-item resolution) |
| `1769a60` | `docs(quick-260526-w11): reconcile ROADMAP Phase 5 Progress row to 17/17`                            | Phase 5 closeout polish                                    |
| `3f48032` | `docs(quick-260526-w11): Phase 5 closeout polish — SUMMARY + STATE + deferred-items RESOLVED`        | Phase 5 closeout polish                                    |
| `adb1053` | `fix(quick-260530-h2h): install cert-manager v1.20.2 before helm install tide in acceptance-v1.sh`   | Cascade-1 fix (cert-manager prereq in acceptance-v1.sh)    |
| `7d3af9d` | `docs(quick-260530-h2h): document cert-manager v1.20.2 prerequisite in INSTALL.md`                   | Cascade-1 fix (INSTALL.md prereq subsection)               |
| `a2193cf` | `docs(quick-260530-h2h): cert-manager prereq fix — SUMMARY + STATE + deferred-items RESOLVED`        | Cascade-1 closeout                                         |

SPEC/DISCUSS/PLAN should not author plans that re-touch these surfaces unless a follow-on bug is identified. The cert-manager fix is in `acceptance-v1.sh` ONLY (not `dry-run-v1.sh` — Finding 3 covers the dry-run mirror). `INSTALL.md` documents cert-manager as a prereq but does not yet document the missing image-publish step.

## Proposed scope (DRAFT — final scope set by /gsd-discuss-phase)

All proposed requirements below are labeled **DRAFT**. Final REQ-IDs and final scope come from `/gsd-discuss-phase 06`. The IDs `IMG-01..05`, `CHART-01`, `DRY-01..02`, `ACC-01..02` are proposed seeds, NOT final REQ-IDs.

1. **DRAFT IMG-01..05 — Docker image build + push pipeline for all 6 components.** Multi-arch (linux/amd64 + linux/arm64). Targets — `tide-controller`, `tide-dashboard`, `tide-stub-subagent`, `tide-credproxy`, `tide-push`, `tide-claude-subagent`. Two plausible shapes, both worth surfacing in `/gsd-discuss-phase`:
   - **Option A** — extend `.goreleaser.yaml` with `dockers:` + `docker_manifests:` sections; goreleaser builds images on `v*` tag push as part of the existing release pipeline.
   - **Option B** — author a new `docker-build-push` job (or workflow file) under `.github/workflows/` gated on `v*` tag push, using `docker/build-push-action` or `ko build` per-component.
   Tag derived from `.Version` (`ghcr.io/jsquirrelz/tide-controller:v1.0.0`, etc.). The choice between Option A and Option B is a `/gsd-discuss-phase` decision — both are valid; goreleaser-native keeps the release surface single-file, while a separate workflow keeps per-image build logic decoupled from the CLI release flow. **Do not commit to one shape pre-discuss.**

2. **DRAFT CHART-01 — Chart-side image-tag alignment SOT fix.** Update `hack/helm/tide-values.yaml` (the SOT — `charts/tide/values.yaml` is the augment-script output and is **not** the edit surface per CLAUDE.md): 5 hardcoded `v0.1.0-dev` tags → `""` so they default to `.Chart.AppVersion` (matching the dashboard pattern already at `charts/tide/values.yaml:244`). Re-run `bash hack/helm/augment-tide-chart.sh` so the generated `charts/tide/values.yaml` reflects the SOT. Verify with `helm template charts/tide | grep -E 'image:.*tide-'` to confirm rendered image tags match the chart's `appVersion`. Per Phase 02.2 chart-vs-binary anti-pattern: SOT edit first, then `make helm` propagates — never the reverse.

3. **DRAFT DRY-01 — `dry-run-v1.sh` cert-manager install + rollout-wait.** Mirror the fix already landed in `acceptance-v1.sh` (commit `adb1053`) into `hack/scripts/dry-run-v1.sh:80-82`. Without this, the rc-tag dry-run path also deadlocks on the cert-manager CRD "no matches" error before it even reaches the image-pull stage. Preserve the `TIDE_CERT_MANAGER_VERSION` env-override convention from `test/integration/kind/suite_test.go:336` (default `v1.20.2`).

4. **DRAFT DRY-02 — Image-load fallback for local clusters.** Both `acceptance-v1.sh` and `dry-run-v1.sh` need a code path that builds the 6 component images locally + `kind load docker-image`s them into the cluster, for the case where the operator is testing pre-tag (no published images yet) or testing a tag that doesn't match HEAD. Plausibly a new Makefile target `acceptance-images-load` (deferred to discuss-phase — do not pre-create here). Whether to make this default-on for `make acceptance-v1` vs gated behind an env var like `TIDE_USE_LOCAL_IMAGES=1` is a `/gsd-discuss-phase` decision — default-on is friendlier to first-run operators; gated keeps published-image runs unsurprising.

5. **DRAFT ACC-01 — BOOT-04 end-to-end revalidation** with published images (or local image-load fallback). The closeout gate of Phase 6. Demonstrates TIDE-on-TIDE actually works end-to-end against the v1.0 chart contract.

6. **DRAFT ACC-02 — README + INSTALL.md ship-state corrections.** Remove any premature "v1.0 ship-ready" claims; document `make container-images` (or the equivalent target name discuss-phase picks) if a local-build path is added; document the image-publish workflow in maintainer docs so the next operator can pre-verify before tagging.

7. **DRAFT plausible add-ons** (discuss-phase prioritization — explicit non-binding): `test-int-kind-prep` cluster-name parameterization (currently hardcoded to `tide-test` at the Makefile target, blocks parallel local test/acceptance/dry-run runs); `.acceptance-runs/` `.gitignore` entry (today's two failed runs littered the worktree with output files); operator-facing Troubleshooting entry in `docs/troubleshooting.md` for `ImagePullBackOff` mid-install with the recipe (check ghcr.io existence + check chart tag pins); potential `make doctor` preflight that checks for cert-manager + image existence before running `helm install tide`.

## Out-of-scope (explicit non-goals for Phase 6)

- Phase 5 reopen. Phase 5's actually-shipped deliverables (LICENSE, NOTICE, docs, samples, chart additions, release.yaml chain, BOOT-02/BOOT-04 plumbing) all stand. Phase 6 plugs gaps, it does not retract Phase 5.
- New chart features beyond image-tag alignment. The chart's contract surface is fixed per CLAUDE.md anti-pattern — Phase 6 aligns it with reality, not extends it.
- Multi-version chart distribution (carry-forward from Phase 5 `deferred-items.md`). v1.0 ships exactly one chart version.
- Cosign / SLSA / supply-chain signing (carry-forward from Phase 5 `deferred-items.md`; v1.x scope per `.goreleaser.yaml:113-114` footer comment).
- OperatorHub / OLM bundle submission (v1.x).
- Anything CI-only that doesn't unblock BOOT-04 revalidation. Phase 6 is bounded by the BOOT-04 success criterion.
- Conversion-webhook activation (carry-forward from D-X7 — `v1alpha1` stays the hub; conversion-webhook scaffolding is in place but unused in v1.0).

## Next-session playbook

1. `/gsd-spec-phase 06` — author `06-REQUIREMENTS.md` (formal REQ-IDs from the DRAFT seeds above).
2. `/gsd-discuss-phase 06` — lock decisions. Likely D-X scope includes: goreleaser-vs-separate-workflow choice (Option A vs Option B from DRAFT IMG-01..05); image-load-fallback default (DRY-02 — default-on vs gated by env var); dry-run-v1 cert-manager pinned version (mirror `v1.20.2` from acceptance-v1 or independent pin).
3. `/gsd-plan-phase 06` — decompose into wave-structured PLAN.md files. Likely 4-6 plans depending on parallelization (image-publish pipeline + chart-tag SOT + dry-run-v1 cert-manager + image-load fallback are all parallel-eligible after wave 1 sets up shared structure).
4. `/gsd-execute-phase 06` — run the plans.
5. After Phase 6 closeout: re-run `make acceptance-v1` end-to-end as the v1.0 ship gate. On green, tag `v1.0.0`.

## Cross-references

- `.planning/ROADMAP.md` Phase 6 row + section (added by this quick task, Task 1)
- `.planning/STATE.md` Current Position (reframed by this quick task, Task 1)
- `.planning/phases/05-distribution-self-hosting-acceptance/05-SUMMARY.md` (Phase 5 closeout — context for what's intact and out-of-scope to retouch)
- `.planning/phases/05-distribution-self-hosting-acceptance/deferred-items.md` (new 2026-05-30 entry at the bottom — back-reference to this doc)
- `.planning/quick/260530-h2h-boot-04-acceptance-v1-cert-manager-prere/260530-h2h-SUMMARY.md` (cascade-1 closure context)
- `.goreleaser.yaml` (the file whose missing `dockers:` section is the root of Finding 1)
- `charts/tide/values.yaml` (the file whose hardcoded `v0.1.0-dev` tags are the root of Finding 2; SOT is `hack/helm/tide-values.yaml`)
- `hack/scripts/dry-run-v1.sh` (the file with the cert-manager gap — Finding 3)
- `test/integration/kind/suite_test.go:329-369` (the proven Layer B cert-manager install pattern)
- `./CLAUDE.md` (chart-vs-binary anti-pattern from Phase 02.2; Observe First / Execute Don't Ask / Verify Before Claiming)
