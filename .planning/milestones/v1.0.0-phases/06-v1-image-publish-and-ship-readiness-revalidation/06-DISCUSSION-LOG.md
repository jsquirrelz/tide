# Phase 6: v1-image-publish-and-ship-readiness-revalidation - Discussion Log

> **Audit trail only.** Do not use as input to planning, research, or execution agents.
> Decisions are captured in CONTEXT.md — this log preserves the alternatives considered.

**Date:** 2026-05-30
**Phase:** 06-v1-image-publish-and-ship-readiness-revalidation
**Areas discussed:** Image-publish mechanism, $0 BOOT-04 carrier

---

## Gray-area selection

| Option | Description | Selected |
|--------|-------------|----------|
| Image-publish mechanism | goreleaser-native vs separate workflow | ✓ |
| $0 BOOT-04 carrier | which command IS the closeout gate | ✓ |
| Auto-detect image-load shape | shared helper vs inline; detection method | (to Claude discretion) |
| Multi-arch CI strategy | buildx+QEMU vs native arm64 matrix | (collapsed — resolved by D-02) |

**Notes:** User chose the two pivotal decisions; the other two flow to researcher/planner as discretion. The multi-arch CI question was effectively answered by the Dockerfile `--platform` refactor (D-02).

---

## Image-publish mechanism

### Q1 — How should the 6 images build + push on a `v*` tag?

| Option | Description | Selected |
|--------|-------------|----------|
| Separate buildx matrix job | docker/build-push-action + buildx over the 6 existing Dockerfiles; goreleaser stays CLI+chart only | ✓ (via the refactor question below) |
| goreleaser-native dockers: | add `dockers:`+`docker_manifests:`; requires 5 new binary builds + rewriting all 6 Dockerfiles | |

**User's choice:** Picked "Other" → asked *"Should the existing Dockerfiles self-building via buildx be refactored to better reflect best practices?"* — a thinking-partner question rather than a direct A/B pick.

**Notes:** Audit of all 6 Dockerfiles showed they are ~80% best-practice (multi-stage, distroless/static:nonroot, CGO_ENABLED=0, buildx `TARGETARCH`, dep-layer caching) but **none pin `--platform=$BUILDPLATFORM`** on the builder stage → arm64 builds emulate under QEMU. Also no BuildKit cache mounts; inconsistent `golang:1.26` vs `golang:1.26-alpine` bases. Assessment: keep the buildx-matrix mechanism (goreleaser-native would discard these working buildx-native Dockerfiles), and fold a minimal cross-compile refactor into the phase. This resolved into the refactor-scope question below.

### Q2 — Dockerfile refactor scope alongside the buildx-matrix job?

| Option | Description | Selected |
|--------|-------------|----------|
| Minimal: --platform only | add `FROM --platform=$BUILDPLATFORM` to all 6 builders; cache-mounts/base-consolidation = planner discretion; digest-pinning out (v1.x SLSA) | ✓ |
| Minimal + CI cache mounts | + BuildKit `--mount=type=cache` on all 6 | |
| Full hardening pass | + base consolidation + `@sha256` digest pinning | |

**User's choice:** Minimal: --platform only.
**Notes:** `@sha256` digest pinning overlaps the v1.x cosign/SLSA work already out of scope; full hardening would expand the phase blast radius beyond the BOOT-04 gate. Locked as D-01 (mechanism) + D-02 (minimal refactor).

---

## $0 BOOT-04 carrier

### Q1 — Which command IS the Phase 6 $0 closeout gate?

| Option | Description | Selected |
|--------|-------------|----------|
| Small-sample mode in acceptance-v1 | $0 mode applying examples/projects/small + stub-subagent; revalidates the exact kind-based path that broke | ✓ |
| Reuse dry-run-v1 as the gate | already small/$0/DinD + being fixed this phase; least new code, but proves the DinD path not the kind path | |
| New dedicated target | purpose-built kind-based `make boot-04-local`; cleanest separation, most net-new surface | |

**User's choice:** Small-sample mode in acceptance-v1.
**Notes:** Decisive grounding — both 2026-05-30 cascades (cert-manager AND ImagePullBackOff) happened inside `make acceptance-v1` (kind ritual), not `dry-run-v1` (DinD). The $0 small-sample mode re-runs the exact failed chain at zero spend. Locked as D-05; D-06 subsets Phase 5's D-A3 pass criteria to the infra+dispatch checks (no git/budget/gitleaks, which only the real $25 run exercises).

---

## Claude's Discretion

- Auto-detect detection method (`docker manifest inspect` vs `docker pull` vs crane/skopeo) — lean manifest-inspect.
- Auto-detect code shape (shared helper sourced by both scripts vs inline) — lean shared helper.
- BuildKit cache mounts + builder base-image consolidation on the 6 Dockerfiles — optional CI polish.
- `release.yaml` image-publish job placement/ordering relative to chart push (constraint: images push before/with chart).
- `acceptance-v1` $0-mode knob name (`ACCEPTANCE_SAMPLE=small` env vs `make acceptance-v1-smoke` target).
- `dry-run-v1` cert-manager pin = mirror `v1.20.2` default (obvious; no independent pin).

## Deferred Ideas

- Dockerfile `@sha256` digest pinning + cosign/SLSA supply-chain hardening — v1.x.
- `test-int-kind-prep` cluster-name parameterization — backlog.
- `make doctor` preflight — backlog.
- Reusing `dry-run-v1` as the gate — rejected (proves DinD not kind path); dry-run-v1 still gets its DRY-01 + IMG-LOAD-01 fixes for the rc-tag path.
