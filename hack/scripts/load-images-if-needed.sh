#!/usr/bin/env bash
# Shared auto-detect image-load helper.
#
# For each of the 7 chart-referenced component images, checks registry existence
# via docker manifest inspect (no-pull probe); if absent (pre-publish), builds
# the image locally and kind-loads it into the named cluster.
#
# Usage: bash load-images-if-needed.sh <cluster_name> <image_tag>
#
# Arguments:
#   cluster_name  — kind cluster name (e.g. tide-acceptance-1234567890, tide-dry-run)
#   image_tag     — Docker image tag (e.g. 1.0.0 — no v prefix; matches chart appVersion after CHART-01)
#
# Calls kind load docker-image ... --name "${cluster_name}" directly (IMG-LOAD-01 Pitfall 1:
# never delegates to a Makefile target that hardcodes the cluster name).
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/../.." && pwd)"

CLUSTER_NAME="${1:?cluster_name argument required (e.g. tide-acceptance-1234567890 or tide-dry-run)}"
IMAGE_TAG="${2:?image_tag argument required (e.g. 1.0.0 — no v prefix; matches chart appVersion after CHART-01)}"

# D-03 fixed image inventory — 7 chart-referenced component images.
# Parallel arrays: IMAGES[i] is built from DOCKERFILES[i].
# The local build at ":${IMAGE_TAG}" matches what helm template charts/tide requests,
# so pullPolicy:IfNotPresent (set on all 7 images in the chart) causes the
# controller to use the kind-loaded image without attempting a registry pull.
# tide-reporter joined in Phase 9 (in-namespace reader Job, plan 09-04/09-06) —
# its omission here + in release.yaml made every reporter Job ErrImagePull.
IMAGES=(
  "ghcr.io/jsquirrelz/tide-controller"
  "ghcr.io/jsquirrelz/tide-dashboard"
  "ghcr.io/jsquirrelz/tide-stub-subagent"
  "ghcr.io/jsquirrelz/tide-credproxy"
  "ghcr.io/jsquirrelz/tide-push"
  "ghcr.io/jsquirrelz/tide-claude-subagent"
  "ghcr.io/jsquirrelz/tide-reporter"
)

DOCKERFILES=(
  "./Dockerfile"
  "./Dockerfile.dashboard"
  "images/stub-subagent/Dockerfile"
  "images/credproxy/Dockerfile"
  "images/tide-push/Dockerfile"
  "images/claude-subagent/Dockerfile"
  "images/tide-reporter/Dockerfile"
)

echo "==> load-images-if-needed: cluster=${CLUSTER_NAME} tag=${IMAGE_TAG}"

for i in "${!IMAGES[@]}"; do
  img="${IMAGES[$i]}:${IMAGE_TAG}"
  df="${DOCKERFILES[$i]}"

  if docker manifest inspect "${img}" > /dev/null 2>&1; then
    echo "==> ${img}: found in registry, skipping local build"
  else
    echo "==> ${img}: not found in registry, building locally..."
    docker build -t "${img}" -f "${df}" "${REPO_ROOT}"
    kind load docker-image "${img}" --name "${CLUSTER_NAME}"
    echo "==> ${img}: built and loaded into cluster ${CLUSTER_NAME}"
  fi
done

echo "==> load-images-if-needed: done"
