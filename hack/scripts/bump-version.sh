#!/usr/bin/env bash
# bump-version.sh — atomically set the TIDE chart version across every
# version-bearing file (charts + helmify source-of-truth).
#
# Usage: hack/scripts/bump-version.sh X.Y.Z   (or: make bump-version VERSION=X.Y.Z)
#
# Rewrites both the `version:` and `appVersion:` keys in every file listed by
# version-files.sh so they can never skew. Run `make helm` afterwards is NOT
# required (the generated charts are updated in place), but the chart-
# reproducibility gate will still pass because the helmify source carries the
# same value. Memory note: bump the chart/appVersion as release STEP ONE — an
# rc dry-run reuses prior published images otherwise, producing version skew.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"
# shellcheck source=hack/scripts/version-files.sh
source "${SCRIPT_DIR}/version-files.sh"

NEW_VERSION="${1:-}"
if [ -z "${NEW_VERSION}" ]; then
  echo "bump-version: missing VERSION argument" >&2
  echo "usage: make bump-version VERSION=X.Y.Z" >&2
  exit 2
fi

# Validate strict semver core (X.Y.Z) — optionally with a -prerelease suffix.
if ! printf '%s' "${NEW_VERSION}" | grep -qE '^[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?$'; then
  echo "bump-version: ${NEW_VERSION} is not a valid semver (expected X.Y.Z[-prerelease])" >&2
  exit 2
fi

cd "${REPO_ROOT}"

for f in "${VERSION_FILES[@]}"; do
  if [ ! -f "${f}" ]; then
    echo "bump-version: missing version file ${f}" >&2
    exit 1
  fi
  # version: unquoted; appVersion: quoted. Anchored to line start so neither the
  # `name:` key nor any future nested key is touched. Temp-file + mv (not sed -i)
  # so the script is portable across GNU (CI) and BSD/macOS sed.
  sed -e "s/^version: .*/version: ${NEW_VERSION}/" \
      -e "s/^appVersion: .*/appVersion: \"${NEW_VERSION}\"/" "${f}" > "${f}.tmp"
  mv "${f}.tmp" "${f}"
  echo "  ${f} → ${NEW_VERSION}"
done

echo "bump-version: set version + appVersion to ${NEW_VERSION} across ${#VERSION_FILES[@]} files"
echo "Next: review 'git diff', commit, then tag v${NEW_VERSION} per the release recipe."
