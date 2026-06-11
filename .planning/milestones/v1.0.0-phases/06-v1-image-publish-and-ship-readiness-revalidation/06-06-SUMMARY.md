---
plan: 06-06
phase: 06-v1-image-publish-and-ship-readiness-revalidation
title: ACC-01 — $0 BOOT-04 closeout gate
status: complete-with-deferral
completed: 2026-05-30
requirements: [ACC-01]
---

# Plan 06-06 — ACC-01 `$0` BOOT-04 Closeout: SUMMARY

## Outcome

`make acceptance-v1-smoke` (the `$0` BOOT-04 revalidation) was run end-to-end for the first time.
It **proved Phase 6's image-publish + ship-readiness-revalidation goal** and **surfaced the real
v1.0 ship blocker** (cascade-7). Full chain + evidence: `06-ACCEPTANCE-FINDINGS.md`.

## D-06 result (split by what Phase 6 owns)

**PASS — infrastructure / image-publish proof (Phase 6 scope), validated live on a fresh kind cluster, `$0`, no API key:**
- [x] 6 component images build multi-arch + `kind load` into the cluster
- [x] `helm template` resolves all 6 tags to appVersion `1.0.0` (no `v0.1.0-dev`; busybox `1.36` preserved)
- [x] cert-manager rolled out; both helm installs `deployed`
- [x] `tide-controller-manager` `Available` + `tide-dashboard` `Running` + **no `ImagePullBackOff`** (cascade-2, the gap that opened Phase 6, is closed)
- [x] per-namespace PVC `Bound`, `tide-init` Job `Complete`, Project `Initialized`

**DEFERRED — "small Project reaches Complete" (D-06 dispatch criterion):** blocked by **cascade-7**,
a pre-existing **product gap** outside Phase 6 scope — the ProjectReconciler never authors a
Milestone from a bare Project (`project_controller.go:271-380` git-only after `Initialized`;
no `Create(Milestone)` anywhere; all Layer B tests pre-apply a Milestone fixture). The down-stack
reconcilers (Milestone→Phase→Plan→Task) are wired; only the Project→Milestone entry point is missing.
Deferred to a dedicated phase (recommend **Phase 7 — Project→Milestone authoring**). The acceptance
script needs no further changes — it already drives correctly through `Initialized`.

## What landed across Phase 6 (commits)

- `e09e45e` perf: BuildKit cache mounts + drop `-a` on 6 Dockerfiles (build acceleration: ~40min→~10min)
- `bee1be8`/`73ea9c9` CHART-01 + HYG-01 (06-01); `c40530f`/`ddbec86` D-02 Dockerfile cross-compile (06-02)
- `af081ed` IMG-01 `build-images` matrix + chart-publish gating (06-03)
- `34ceb39`/`7ea899c`/`9f833cb` IMG-LOAD-01 + DRY-01 + D-05 scripts/Makefile (06-04); `11a5b21` DOC-01 (06-05)
- `68f2d32` fix: `.dockerignore` `*.tmpl` (cascade-3); `9395006` fix: RWO PVC override (cascade-4); `819ab29` fix: per-namespace setup + prewarm (cascade-5)

## Verification commands (re-runnable)

```
grep -cE 'v0\.1\.0-dev' charts/tide/values.yaml hack/helm/tide-values.yaml   # → 0
helm template charts/tide | grep -E 'image:' | grep -v '1\.0\.0\|1\.36'      # → empty
grep -cE '^FROM --platform=\$BUILDPLATFORM' Dockerfile Dockerfile.dashboard images/*/Dockerfile  # → 6
make acceptance-v1-smoke   # drives through Initialized; reaches Complete once cascade-7 (Phase 7) lands
```

## Self-Check: PASS (for Phase 6 image-publish scope; Project→Complete deferred to Phase 7 per 06-ACCEPTANCE-FINDINGS.md)
