---
phase: 06-v1-image-publish-and-ship-readiness-revalidation
plan: "02"
subsystem: infra
tags: [docker, buildx, multi-arch, cross-compile, golang, dockerfile]

# Dependency graph
requires:
  - phase: 06-v1-image-publish-and-ship-readiness-revalidation/06-01
    provides: CI workflow skeleton that calls docker buildx build --platform linux/amd64,linux/arm64 for all 6 components
provides:
  - "All 6 component Dockerfiles have --platform=$BUILDPLATFORM on their Go builder stage"
  - "4 alpine images have ARG TARGETOS/TARGETARCH + GOARCH=${TARGETARCH} in go build RUN"
  - "Native cross-compile path for Go stages (no QEMU needed for amd64->arm64)"
affects:
  - 06-03
  - 06-04
  - CI build-images job

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "FROM --platform=$BUILDPLATFORM on Go builder stages enables native cross-compile via BuildKit; no ARG BUILDPLATFORM needed (BuildKit auto-provides)"
    - "Alpine sidecar Dockerfiles pattern: FROM --platform=$BUILDPLATFORM + ARG TARGETOS + ARG TARGETARCH + GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH}"
    - "Non-alpine images (controller, dashboard): one-line change only; ARG TARGETOS/TARGETARCH already present"
    - "node:22-slim runtime stage in claude-subagent must NOT get --platform=$BUILDPLATFORM (QEMU handles it; per-spec)"

key-files:
  created: []
  modified:
    - Dockerfile
    - Dockerfile.dashboard
    - images/stub-subagent/Dockerfile
    - images/credproxy/Dockerfile
    - images/tide-push/Dockerfile
    - images/claude-subagent/Dockerfile

key-decisions:
  - "No ARG BUILDPLATFORM added to any Dockerfile — BuildKit auto-provides BUILDPLATFORM when --platform= is used on a FROM line; adding ARG BUILDPLATFORM would be redundant and misleading"
  - "GOOS=${TARGETOS:-linux} with :-linux fallback chosen for alpine images to match the non-alpine pattern and handle edge cases where TARGETOS is unset"
  - "Only Go builder stages receive --platform=$BUILDPLATFORM; node:22-slim runtime stage in claude-subagent left untouched (QEMU acceptable for npm install per T-06-02-02 threat model accept)"

patterns-established:
  - "BuildKit cross-compile pattern: builder stage = --platform=$BUILDPLATFORM (runs on native runner); runtime stage = no --platform (matches target arch via COPY --from=builder)"

requirements-completed: [IMG-01]

# Metrics
duration: 2min
completed: 2026-05-30
---

# Phase 06 Plan 02: Dockerfile Cross-Compile Fix Summary

**`FROM --platform=$BUILDPLATFORM` added to all 6 Go builder stages; alpine sidecars get ARG TARGETOS/TARGETARCH + GOARCH passthrough, enabling native amd64-to-arm64 cross-compile without QEMU for all Go build steps**

## Performance

- **Duration:** 2 min
- **Started:** 2026-05-30T18:23:16Z
- **Completed:** 2026-05-30T18:24:45Z
- **Tasks:** 2
- **Files modified:** 6

## Accomplishments
- Fixed all 6 component Dockerfiles with `FROM --platform=$BUILDPLATFORM` on Go builder stages (D-02 requirement)
- Added `ARG TARGETOS` and `ARG TARGETARCH` to the 4 alpine images that lacked them (stub-subagent, credproxy, tide-push, claude-subagent)
- Patched go build RUN in those 4 alpine images: `GOOS=linux` -> `GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH}` so cross-compiled binaries target the correct architecture
- Preserved node:22-slim runtime stage in claude-subagent unchanged (no --platform prefix; QEMU accepted per threat model)

## Task Commits

Each task was committed atomically:

1. **Task 1: Fix golang:1.26 (non-alpine) builder stages** - `c40530f` (feat)
2. **Task 2: Fix golang:1.26-alpine builder stages + ARG/GOARCH** - `ddbec86` (feat)

**Plan metadata:** (docs commit follows)

## Files Created/Modified
- `Dockerfile` - builder stage: `FROM golang:1.26` -> `FROM --platform=$BUILDPLATFORM golang:1.26`
- `Dockerfile.dashboard` - same one-line change; ARG lines already present
- `images/stub-subagent/Dockerfile` - --platform added; ARG TARGETOS/TARGETARCH added; GOARCH passthrough added
- `images/credproxy/Dockerfile` - same three-part fix
- `images/tide-push/Dockerfile` - same three-part fix
- `images/claude-subagent/Dockerfile` - same three-part fix (Go builder only; node:22-slim runtime untouched)

## Decisions Made
- No `ARG BUILDPLATFORM` line added to any Dockerfile — BuildKit provides it automatically when `--platform=` is used on a FROM line. Adding it explicitly is redundant and contradicts the anti-pattern documented in the plan's `<interfaces>` block.
- `GOOS=${TARGETOS:-linux}` (with `:-linux` fallback) used in alpine images to match the pre-existing pattern in the non-alpine images and be robust when `TARGETOS` is unset.
- `FROM node:22-slim` in claude-subagent intentionally left without `--platform=$BUILDPLATFORM`. BuildKit + QEMU handles the node/npm install stage for arm64; this is slow (2-5 min) but within the 30-minute CI job timeout (T-06-02-02 in threat model — accepted).

## Deviations from Plan

None - plan executed exactly as written.

## Issues Encountered
None.

## User Setup Required
None - no external service configuration required.

## Next Phase Readiness
- All 6 Dockerfiles ready for `docker buildx build --platform linux/amd64,linux/arm64`
- Plan 03 (build-images CI job) can now rely on native cross-compile for all Go stages
- arm64 QEMU overhead limited to the npm install step in claude-subagent only

## Threat Surface Scan
No new network endpoints, auth paths, file access patterns, or schema changes introduced. Dockerfile changes are build-time only with no runtime security surface changes.

---
*Phase: 06-v1-image-publish-and-ship-readiness-revalidation*
*Completed: 2026-05-30*
