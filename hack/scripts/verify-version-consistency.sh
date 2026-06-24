#!/usr/bin/env bash
# verify-version-consistency.sh — assert every version-bearing file agrees on a
# single version, and that `version:` == `appVersion:` within each file.
#
# Usage:
#   hack/scripts/verify-version-consistency.sh           # all files must agree
#   hack/scripts/verify-version-consistency.sh X.Y.Z     # ...and equal X.Y.Z
#
# Pure grep/sed (no yq) so it runs unmodified on a bare CI runner. Guards against
# the v1.0.1→v1.0.3 class of bug where a chart Chart.yaml is bumped but the
# helmify source (or the sibling subchart) is left behind, skewing the published
# image tag from the chart appVersion.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"
# shellcheck source=hack/scripts/version-files.sh
source "${SCRIPT_DIR}/version-files.sh"

EXPECTED="${1:-}"
cd "${REPO_ROOT}"

# Extract a key's value from a Chart.yaml: strip the key, surrounding quotes,
# and whitespace. Returns empty if the key is absent (which is itself a failure).
extract() { # <file> <key>
  sed -nE "s/^$2:[[:space:]]*\"?([^\"]*)\"?[[:space:]]*$/\1/p" "$1" | head -n1
}

fail=0
canonical=""
for f in "${VERSION_FILES[@]}"; do
  if [ ! -f "${f}" ]; then
    echo "version-consistency: missing file ${f}" >&2
    fail=1
    continue
  fi
  v="$(extract "${f}" version)"
  a="$(extract "${f}" appVersion)"
  if [ -z "${v}" ] || [ -z "${a}" ]; then
    echo "version-consistency: ${f} missing version (${v:-<none>}) or appVersion (${a:-<none>})" >&2
    fail=1
    continue
  fi
  if [ "${v}" != "${a}" ]; then
    echo "version-consistency: ${f} version (${v}) != appVersion (${a})" >&2
    fail=1
  fi
  if [ -z "${canonical}" ]; then
    canonical="${v}"
  elif [ "${v}" != "${canonical}" ]; then
    echo "version-consistency: ${f} version (${v}) != first-seen version (${canonical})" >&2
    fail=1
  fi
done

if [ "${fail}" -ne 0 ]; then
  echo "version-consistency: FAILED — run 'make bump-version VERSION=X.Y.Z' to realign" >&2
  exit 1
fi

if [ -n "${EXPECTED}" ] && [ "${canonical}" != "${EXPECTED}" ]; then
  echo "version-consistency: charts are at ${canonical} but expected ${EXPECTED}" >&2
  exit 1
fi

echo "OK: all ${#VERSION_FILES[@]} version files agree on ${canonical}${EXPECTED:+ (matches expected ${EXPECTED})}"
