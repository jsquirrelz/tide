#!/usr/bin/env bash
# verify-chart-images-published.sh — assert every ghcr.io/jsquirrelz/* image the
# chart references is built+pushed by the release pipeline.
#
# The chart (charts/tide/values.yaml) pins image repositories like
#   repository: ghcr.io/jsquirrelz/tide-import
# which a chart-based `helm install` pulls at <appVersion>. Those tags only
# exist if `.github/workflows/release.yaml`'s `build-images` matrix has a
# `- component: <name>` entry that builds + pushes them. If an image is added to
# the chart but not the matrix (the v1.0.4 tide-import miss), a chart install
# ImagePullBackOffs that component with no earlier signal — `verify-version-
# consistency` only checks version skew, not matrix↔chart image coverage.
#
# This gate closes that gap: it fails if any chart-referenced
# ghcr.io/jsquirrelz/<name> is absent from the release build-images matrix.
#
# Pure grep/sed (no yq) so it runs unmodified on a bare CI runner — same spirit
# and shape as verify-version-consistency.sh.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"
cd "${REPO_ROOT}"

VALUES="charts/tide/values.yaml"
RELEASE_WF=".github/workflows/release.yaml"

for f in "${VALUES}" "${RELEASE_WF}"; do
  if [ ! -f "${f}" ]; then
    echo "chart-images-published: missing file ${f}" >&2
    exit 1
  fi
done

# Chart-referenced images: every ghcr.io/jsquirrelz/<name> token in values.yaml
# (including doc comments — a referenced image must be publishable regardless of
# where it is named). The trailing char class stops at ':', '@', or quotes so a
# tag/digest suffix is dropped, leaving the bare image name.
chart_images="$(grep -oE 'ghcr\.io/jsquirrelz/[a-z0-9][a-z0-9-]*' "${VALUES}" \
  | sed -E 's#^ghcr\.io/jsquirrelz/##' | sort -u)"

# Matrix components: every `- component: <name>` line in the build-images matrix
# (these lines exist only in that matrix in release.yaml).
matrix_components="$(grep -E '^[[:space:]]*-[[:space:]]*component:[[:space:]]*' "${RELEASE_WF}" \
  | sed -E 's#^[[:space:]]*-[[:space:]]*component:[[:space:]]*##' | sort -u)"

if [ -z "${chart_images}" ]; then
  echo "chart-images-published: found no ghcr.io/jsquirrelz/* images in ${VALUES}" >&2
  echo "  (expected at least the controller image — has the registry moved?)" >&2
  exit 1
fi

fail=0
missing=""
while IFS= read -r img; do
  [ -z "${img}" ] && continue
  if ! printf '%s\n' "${matrix_components}" | grep -qxF "${img}"; then
    missing="${missing} ${img}"
    fail=1
  fi
done <<< "${chart_images}"

if [ "${fail}" -ne 0 ]; then
  echo "chart-images-published: FAILED — chart references ghcr.io/jsquirrelz images not built by the release matrix:" >&2
  for img in ${missing}; do
    echo "  - ${img} (referenced in ${VALUES}, absent from ${RELEASE_WF} build-images matrix)" >&2
  done
  echo "" >&2
  echo "Add a matrix entry (component/dockerfile/context) for each, or remove the chart reference." >&2
  exit 1
fi

count="$(printf '%s\n' "${chart_images}" | grep -c .)"
echo "OK: all ${count} chart-referenced ghcr.io/jsquirrelz images are built by the release matrix"
