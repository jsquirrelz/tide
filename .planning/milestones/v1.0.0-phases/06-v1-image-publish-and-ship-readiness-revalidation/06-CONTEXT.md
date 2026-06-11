# Phase 6: v1.0 Image-Publish Pipeline & Ship-Readiness Revalidation - Context

**Gathered:** 2026-05-30
**Status:** Ready for planning

<domain>
## Phase Boundary

Build + publish the 6 Docker images that `charts/tide` references, align the chart's component tags to the chart `appVersion` (kill the dead `v0.1.0-dev` pins), mirror cert-manager bring-up into `dry-run-v1`, add an auto-detect (pull-then-build) image-load path to both acceptance scripts, and prove BOOT-04 end-to-end **at $0** (locally-built + kind-loaded images, stub-subagent, no real LLM spend). The catch-up phase for the image-publish gap that the 2026-05-30 BOOT-04 retry exposed — NOT a Phase 5 reopen.

</domain>

<spec_lock>
## Requirements (locked via SPEC.md)

**7 requirements are locked.** See `06-SPEC.md` for full requirements, boundaries, and acceptance criteria.

Downstream agents MUST read `06-SPEC.md` before planning or implementing. Requirements are not duplicated here. The locked REQ-IDs: IMG-01 (image pipeline), CHART-01 (tag SOT alignment), DRY-01 (dry-run cert-manager), IMG-LOAD-01 (auto-detect image-load), ACC-01 ($0 BOOT-04 gate), DOC-01 (doc corrections), HYG-01 (gitignore + troubleshooting).

**In scope (from SPEC.md):**
- Multi-arch (amd64+arm64) build-and-publish pipeline for all 6 chart-referenced component images (config + snapshot/local build verified; real push wired for tag time)
- Chart component-tag SOT alignment (5 `v0.1.0-dev` tags → `appVersion`)
- `dry-run-v1.sh` cert-manager install + rollout-wait (mirror of `acceptance-v1.sh`)
- Auto-detect (pull-then-build) local image-load fallback in both `acceptance-v1.sh` and `dry-run-v1.sh`
- BOOT-04 $0 local-image end-to-end revalidation (the closeout gate)
- Ship-state doc corrections + maintainer image-publish documentation
- `.acceptance-runs/` `.gitignore` entry + `docs/troubleshooting.md` ImagePullBackOff-mid-install entry

**Out of scope (from SPEC.md):**
- Cutting the `v1.0.0` tag + the $25 real-LLM published-image acceptance run (post-phase ship action)
- Phase 5 reopen; new chart features beyond tag alignment; multi-version chart distribution
- Cosign / SLSA / supply-chain signing (incl. base-image `@sha256` digest pinning); OperatorHub / OLM bundle
- CI-ification of `acceptance-v1` (real-LLM in CI — D-A4 keeps it operator-only); conversion-webhook activation
- `test-int-kind-prep` cluster-name parameterization; `make doctor` preflight (both deferred to backlog)

</spec_lock>

<decisions>
## Implementation Decisions

### Image-publish mechanism (REQ IMG-01)

- **D-01: Separate buildx matrix job, NOT goreleaser-native.** Image build+push uses `docker/build-push-action` + buildx in a matrix over the 6 existing Dockerfiles, multi-arch in one pass. goreleaser stays CLI-binary + chart only (no `dockers:`/`docker_manifests:` added to `.goreleaser.yaml`). **Rationale (grounded in code):** all 6 Dockerfiles are self-contained multi-stage builds that compile their own Go binary via buildx `ARG TARGETOS/TARGETARCH`, and the dashboard image builds a node-based Vite SPA (`node:22-slim` stage) — neither fits goreleaser's "copy a pre-built goreleaser artifact" model. goreleaser-native would require adding 5 binary `builds:` entries AND rewriting all 6 working Dockerfiles, with the dashboard's node build still falling outside goreleaser. The buildx-matrix approach reuses the Dockerfiles as-is and matches the existing `docker buildx` Makefile convention.

- **D-02: Minimal Dockerfile cross-compile refactor IN scope — `--platform=$BUILDPLATFORM` on all 6 builders.** Add `FROM --platform=$BUILDPLATFORM golang:1.26[-alpine] AS builder` to all 6 component Dockerfiles. **Currently none of them pin the builder platform** — so a buildx arm64 build emulates an arm64 builder under QEMU and runs `go build` emulated (slow/flaky). Pinning the builder to `$BUILDPLATFORM` makes it run native and lets Go cross-compile to `$TARGETARCH` (first-class, fast). This is load-bearing for IMG-01's multi-arch constraint. **Knock-on:** this collapses the deferred "multi-arch CI strategy" question — a single amd64 runner cross-compiles both arches; no QEMU emulation and no native-arm64 runner matrix needed.

- **D-03: The 6 components + repos are fixed by the chart contract.** `tide-controller` (`./Dockerfile`), `tide-dashboard` (`./Dockerfile.dashboard`), `tide-stub-subagent` (`images/stub-subagent/Dockerfile`), `tide-credproxy` (`images/credproxy/Dockerfile`), `tide-push` (`images/tide-push/Dockerfile`), `tide-claude-subagent` (`images/claude-subagent/Dockerfile`) — all under `ghcr.io/jsquirrelz/`. `images/tide-demo-init/Dockerfile` is NOT a chart-referenced component and is excluded from the pipeline.

- **D-04 (planner constraint, not a gray-area choice): image-publish integrates into the existing `v*`-tag release flow.** Phase 5's `release.yaml` (D-X5) already pushes the Helm charts to `ghcr.io/jsquirrelz/tide-charts/*` on `v*` tags. The image-publish job belongs in the same tag-triggered run; images must be pushed before (or in the same run as) the chart publish so the published chart never references not-yet-pushed images. Exact job placement/ordering within `release.yaml` is planner discretion.

### $0 BOOT-04 carrier (REQ ACC-01)

- **D-05: $0 small-sample mode added to `acceptance-v1`.** Teach `acceptance-v1` a $0 mode (e.g. `ACCEPTANCE_SAMPLE=small` env or a `make acceptance-v1-smoke` target — exact knob is planner discretion) that applies `examples/projects/small/project.yaml` (stub-subagent, no API key) instead of the `large`/$25 sample. **Rationale:** both 2026-05-30 cascades (cert-manager AND ImagePullBackOff) happened inside `make acceptance-v1` — the kind-based maintainer ritual — not in `dry-run-v1` (DinD). A $0 small-sample mode revalidates the EXACT path that broke, at zero spend. The `large`/$25 real-Claude run (D-A4) stays unchanged as the post-phase ship action.

- **D-06: $0-mode pass criteria = the infra+dispatch subset of Phase 5's D-A3.** The $0 stub run asserts: controller-manager `Available` + dashboard `Running` + small Project reaching its terminal phase + zero `ERROR` controller logs + no orphan Jobs + no `ImagePullBackOff`. It does NOT assert the per-run-branch / 4-commit-shapes / under-budget / gitleaks checks from D-A3 — those only exercise under a real-Claude git-pushing run and stay scoped to the `large`/$25 path.

### Claude's Discretion (researcher + planner finalize, constrained by SPEC.md)

- **Auto-detect detection method** (IMG-LOAD-01): `docker manifest inspect` (no-pull existence probe) vs `docker pull` attempt vs `crane`/`skopeo`. Lean toward `docker manifest inspect` (cheapest existence check, no layer download) but planner picks.
- **Auto-detect code shape**: shared helper script sourced by both `acceptance-v1.sh` + `dry-run-v1.sh` vs inline-per-script. Lean toward a shared helper (DRY, single SOT for the 6-image list) but planner picks.
- **BuildKit cache mounts** (`# syntax=docker/dockerfile:1` + `--mount=type=cache` for go-mod + build cache) on the 6 Dockerfiles — low-risk CI speedup, optional; and consolidating the `golang:1.26` vs `golang:1.26-alpine` builder-base split. Planner's call; not load-bearing.
- **`release.yaml` job placement/ordering** for the image-publish job relative to the chart push + goreleaser steps (see D-04 constraint).
- **Local-build tag** the scripts apply when building pre-tag must match what `helm template` resolves (the chart `appVersion`, `1.0.0`) so the auto-detect pull/build keys on the same tag.
- **`dry-run-v1` cert-manager pin**: mirror `acceptance-v1`'s `TIDE_CERT_MANAGER_VERSION` default `v1.20.2` (DRY-01) — treat as the obvious default, no independent pin.

</decisions>

<canonical_refs>
## Canonical References

**Downstream agents MUST read these before planning or implementing.**

### Locked requirements + scope-of-record (read FIRST)
- `.planning/phases/06-v1-image-publish-and-ship-readiness-revalidation/06-SPEC.md` — **Locked requirements — MUST read before planning.** 7 REQ-IDs, boundaries, acceptance criteria.
- `.planning/phases/06-v1-image-publish-and-ship-readiness-revalidation/06-FINDINGS.md` — scope-of-record: the 3 root-cause findings (no image-publish pipeline / chart-tag drift / dry-run cert-manager gap), what's already in `main`, and the DRAFT seeds the SPEC formalized.

### Prior-phase decisions that constrain Phase 6
- `.planning/phases/05-distribution-self-hosting-acceptance/05-CONTEXT.md`:
  - **D-X5** (Helm OCI charts publish to `ghcr.io/jsquirrelz/tide-charts/*` via `release.yaml` on `v*` tags) — images land in the same tag-triggered flow (D-04 above).
  - **D-D1..D-D4** (`make dry-run-v1` — DinD, ubuntu:24.04, README Quickstart verbatim, small/$0 sample, timer-to-Complete, `v*-rc.*` tag gate) — DRY-01 + IMG-LOAD-01 modify this script; it is NOT the closeout gate (D-05).
  - **D-A1..D-A4** (`make acceptance-v1` — maintainer ritual, `large`/$25 sample, hard $25 cap no bypass, kind-based, evidence under `.acceptance-runs/`, 7-check D-A3 pass criteria) — D-05 adds a $0 small-sample mode beside the unchanged $25 path; D-06 subsets D-A3.
  - **D-B1..D-B2** (cost spectrum: `small`=$0 stub / `medium`=~$5 / `large`=~$25; canonical at `examples/projects/{small,medium,large}/`) — the $0 BOOT-04 uses `examples/projects/small/`.
  - **D-C4** (`docs/troubleshooting.md` is a `Symptom | Cause | Recipe` table) — HYG-01 appends the ImagePullBackOff-mid-install row in this shape.

### Project working rules + invariants
- `CLAUDE.md` — chart-vs-binary anti-pattern (edit SOT `hack/helm/tide-values.yaml` first → `make helm` propagates; `charts/tide/values.yaml` is generated output, NOT the edit surface); Observe-First / Execute-Don't-Ask / Verify-Before-Claiming; stack pins (Go 1.26, Helm 3, goreleaser, kind). API group `tideproject.k8s` invariant.
- `.planning/REQUIREMENTS.md` — project requirements + traceability.
- `.planning/STATE.md` — current cursor: Phase 6 in planning, 8/9 phases complete.

### Files Phase 6 edits or builds on (with the verified current-state gap)
- `.goreleaser.yaml` — builds only the `tide` CLI (0 `dockers:`/`docker_manifests:`). D-01: stays CLI+chart only.
- `.github/workflows/release.yaml` — goreleaser + chart push on `v*` (Phase 5). D-04: image-publish job integrates here.
- `hack/helm/tide-values.yaml` — **the SOT.** 5 component tags pinned `v0.1.0-dev` (lines 39/140/144/155/165); line 148 `tag: "1.36"` is a THIRD-PARTY image — preserve it. CHART-01 sets the 5 → `""`.
- `charts/tide/values.yaml` — generated output (same 5 pins); regenerated via `bash hack/helm/augment-tide-chart.sh` / `make helm` after the SOT edit. Verify with `helm template charts/tide | grep image:`.
- `hack/scripts/acceptance-v1.sh` — kind-based ritual; has cert-manager (commit `adb1053`), no image-load. D-05 adds $0 small-sample mode; IMG-LOAD-01 adds auto-detect.
- `hack/scripts/dry-run-v1.sh` — DinD; NO cert-manager, no image-load. DRY-01 + IMG-LOAD-01.
- `test/integration/kind/suite_test.go` §329-369 — the proven Layer B cert-manager install pattern DRY-01 mirrors (`TIDE_CERT_MANAGER_VERSION` default `v1.20.2`).
- The 6 component Dockerfiles: `./Dockerfile`, `./Dockerfile.dashboard`, `images/{stub-subagent,credproxy,tide-push,claude-subagent}/Dockerfile` — D-02 adds `--platform=$BUILDPLATFORM` to each builder stage.
- `docs/troubleshooting.md` (exists) — HYG-01 appends ImagePullBackOff-mid-install row. `.gitignore` — HYG-01 adds `.acceptance-runs/`.
- `docs/INSTALL.md` — DOC-01 documents image-publish + local-image fallback; remove premature ship-ready claims.

### External tech references (read on demand; don't pre-load)
- `docker/build-push-action` + Docker buildx multi-platform — https://docs.docker.com/build/ci/github-actions/multi-platform/ — the publish mechanism (D-01) + `--platform=$BUILDPLATFORM` cross-compile pattern (D-02).
- goreleaser — https://goreleaser.com/customization/ — confirm it stays CLI+chart only (no `dockers:`).
- cert-manager v1.20.2 — release manifest URL pattern already in `acceptance-v1.sh:70`.

</canonical_refs>

<code_context>
## Existing Code Insights

### Reusable Assets
- **6 self-contained multi-stage Dockerfiles** — already build their own binaries via buildx `TARGETARCH`; reused as-is by the buildx-matrix job. Only change: the `--platform=$BUILDPLATFORM` builder pin (D-02).
- **`acceptance-v1.sh` cert-manager block (commit `adb1053`, lines 61-73)** — DRY-01 mirrors this verbatim into `dry-run-v1.sh`; `TIDE_CERT_MANAGER_VERSION` override already established.
- **`test-int-kind-prep` `kind load docker-image` lines (Makefile ~161-167)** — existing precedent for `kind load docker-image ghcr.io/jsquirrelz/tide-*` into the test cluster; IMG-LOAD-01's local-load path follows this exact form.
- **`hack/helm/augment-tide-chart.sh`** — the SOT→chart regeneration step CHART-01 re-runs after editing `hack/helm/tide-values.yaml`.
- **`examples/projects/small/project.yaml`** ($0 stub sample, Phase 5 D-B2) — the sample the $0 BOOT-04 mode applies.
- **`release.yaml` v*-tag flow + chart-push (Phase 5 D-X5)** — image-publish job integrates into this existing pipeline.

### Established Patterns
- **Chart-vs-binary anti-pattern (CLAUDE.md, Phase 02.2):** SOT (`hack/helm/tide-values.yaml`) edited first, `charts/tide/values.yaml` regenerated — never the reverse. Binding for CHART-01.
- **Tag-triggered release workflows on `v*` / `v*-rc.*`** (Phase 04 D-X3, Phase 5 D-D3) — image-publish gates on `v*` tag push; real push exercised post-phase.
- **Make targets as the operator entry surface** (`make acceptance-v1`, `make dry-run-v1`) — D-05's $0 mode follows this convention.
- **Distroless/static:nonroot runtime + CGO_ENABLED=0 static Go** across all 6 images — the right shape; kept unchanged.

### Integration Points
- **`release.yaml`** — new image-publish buildx-matrix job inserted into the `v*` flow, ordered so images push before/with the chart push (D-04).
- **`hack/helm/tide-values.yaml` → `charts/tide/values.yaml`** — SOT edit propagated by `make helm`; the chart's `appVersion` (1.0.0) becomes the resolved image tag for all 6 components after CHART-01.
- **Both acceptance scripts** — shared auto-detect image-load logic (pull-then-build) added; planner decides shared-helper vs inline.

</code_context>

<specifics>
## Specific Ideas

- **The `--platform=$BUILDPLATFORM` fix is the single highest-value Dockerfile change** and is what makes single-runner multi-arch cheap — without it the deferred "native arm64 runner vs QEMU" question would actually matter. With it, that question evaporates.
- **Auto-detect keys on the chart's resolved tag.** Pre-`v1.0.0`-tag the operator is on `main`, so pulling `ghcr.io/jsquirrelz/tide-*:1.0.0` fails → build locally + `kind load` at the same `:1.0.0` tag (matching what `helm template` requests). Post-tag, the pull succeeds and nothing rebuilds. No operator flags either way.
- **The $0 mode is a true revalidation, not a proxy.** Because both real cascades hit `acceptance-v1` (kind), running `acceptance-v1` in $0 small-sample mode re-exercises the exact cert-manager → helm-install → image-pull → controller-Available → Project-Complete chain that failed — just with the stub subagent instead of real Claude.

</specifics>

<deferred>
## Deferred Ideas

- **Dockerfile `@sha256` digest pinning + cosign/SLSA supply-chain hardening** — v1.x. Out of Phase 6 per SPEC (overlaps the already-deferred supply-chain signing work).
- **BuildKit cache mounts + builder base-image consolidation** — surfaced as low-risk polish; left to planner discretion within Phase 6 (D-02 area), not a separate phase.
- **`test-int-kind-prep` cluster-name parameterization** — backlog (confirmed out in SPEC); unblocks parallel local test/acceptance/dry-run runs.
- **`make doctor` preflight (cert-manager + image-existence check before `helm install`)** — backlog (confirmed out in SPEC).
- **Reusing `dry-run-v1` (DinD) as the closeout gate** — considered and rejected (D-05): it proves the DinD path, not the kind path that actually broke. `dry-run-v1` still gets its DRY-01 + IMG-LOAD-01 fixes for the rc-tag path, just isn't the gate.

### Reviewed Todos (not folded)
None — no pending todos matched this phase (`todo.match-phase` returned no matches).

</deferred>

---

*Phase: 06-v1-image-publish-and-ship-readiness-revalidation*
*Context gathered: 2026-05-30*
