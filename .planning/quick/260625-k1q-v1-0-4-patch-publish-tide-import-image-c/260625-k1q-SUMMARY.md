---
quick_id: 260625-k1q
title: "v1.0.4 patch: publish tide-import image + chart-image release-matrix guardrail"
date: 2026-06-25
status: complete
commit: fd86a79
---

# Quick Task 260625-k1q — Summary

## What changed

**Task 1 — publish tide-import.** Added `tide-import` to the `build-images`
matrix in `.github/workflows/release.yaml` (`images/tide-import/Dockerfile`,
context `.`). Corrected the stale image-count comments ("6 component images"
and three "6 matrix jobs" → 8: controller, dashboard, stub-subagent, credproxy,
push, claude-subagent, reporter, import).

**Task 2 — coverage guardrail.** New `hack/scripts/verify-chart-images-published.sh`
+ `make verify-chart-images-published` (pure bash, mirrors
`verify-version-consistency`): asserts every `ghcr.io/jsquirrelz/*` image
referenced in `charts/tide/values.yaml` has a matching `- component:` in the
release build-images matrix. Wired into `ci.yaml` (per-PR, next to
version-consistency) and `release.yaml`'s `helmify-verify` job (defense-in-depth
at release time — this gate would have caught the tide-import miss before publish).

## Verification (observed)

- `make verify-chart-images-published` → `OK: all 8 chart-referenced
  ghcr.io/jsquirrelz images are built by the release matrix` (exit 0).
- Removing the tide-import matrix entry → gate FAILS naming `tide-import`
  (exit 1); restored → passes again.
- `grep -cE '^[[:space:]]*- component:' release.yaml` → 8.
- Both workflow YAMLs parse (`yaml.safe_load`).

All on commit `fd86a79`. Pre-commit hooks (chart-reproducibility,
version-consistency) skipped — no matching files in the code change.

## Out of scope (release steps — orchestrator, confirm-first)

`make bump-version VERSION=1.0.4` (release STEP ONE), push main, push
`v1.0.4-rc.N` (dry-run gate), then `v1.0.4`; verify `docker manifest inspect
ghcr.io/jsquirrelz/tide-import:1.0.4`.
