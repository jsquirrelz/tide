#!/usr/bin/env bash
# Phase 5 D-D4 / Topic 10 — dry-run-report.json shaper (schemaVersion 1).
#
# Forward-compatible JSON shape: operators inspecting the report for
# benchmarking purposes shouldn't see breaking changes between v1.0 and v1.1.
# A v1.x extension would add NEW keys, never rename or remove existing ones;
# `schemaVersion: 1` is the explicit contract.
#
# Usage:
#   render-dry-run-report.sh <report_path> <elapsed_seconds> <exit_code> [<phases_json>]
#
# Positional args:
#   $1  report_path   — absolute path to write the JSON file
#   $2  elapsed       — total wall-clock seconds for the dry-run
#   $3  exit_code     — exit code of the inner pipeline (0 = pass)
#   $4  phases_json   — optional pre-rendered phases array JSON fragment
#                       (defaults to []; per-phase timing is a v1.1 extension)
#
# The script uses a heredoc + interpolation (no jq dependency) so it runs in
# the same ubuntu:24.04 minimal-install context the DinD dry-run uses without
# requiring extra apt-get installs.
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/../.." && pwd)"

if [ "$#" -lt 3 ]; then
  echo "FAIL: render-dry-run-report.sh requires 3 positional args (report_path elapsed exit_code [phases_json])" >&2
  exit 2
fi

REPORT_PATH="$1"
ELAPSED="$2"
EXIT_CODE="$3"
PHASES_JSON="${4:-[]}"

# Run ID — prefer GitHub Actions ref name (e.g. v1.0.0-rc.1) when set; fall back
# to a local-prefixed unix timestamp so local-dev invocations don't shadow CI
# artifacts in a shared evidence store.
RUN_ID="${GITHUB_REF_NAME:-local-$(date +%s)}"

# Versions: pinned to match dry-run-v1.sh's installs (single source-of-truth
# for the report's version-claim block).
KIND_VERSION="v0.31.0"
HELM_VERSION="v3.16.3"
KUBE_VERSION="v1.31.0"
BASE_IMAGE="ubuntu:24.04"

# TIDE version: derive from git describe when available (release tags),
# fallback to short SHA, fallback to "dev".
TIDE_VERSION=$(cd "${REPO_ROOT}" && git describe --tags --dirty 2>/dev/null || git rev-parse --short HEAD 2>/dev/null || echo "dev")

# Chart versions — D-X3 lockstep bump to 1.0.0. Hard-coded here because the
# report records the chart-version contract the dry-run exercised; if a future
# rev bumps both charts in lockstep, this constant moves with them.
CHART_TIDE_VERSION="1.0.0"
CHART_TIDE_CRDS_VERSION="1.0.0"

TIMESTAMP_UTC=$(date -u +%Y-%m-%dT%H:%M:%SZ)

mkdir -p "$(dirname "${REPORT_PATH}")"

cat > "${REPORT_PATH}" <<EOF
{
  "schemaVersion": 1,
  "runId": "${RUN_ID}",
  "tideVersion": "${TIDE_VERSION}",
  "totalSeconds": ${ELAPSED},
  "exitCode": ${EXIT_CODE},
  "kindVersion": "${KIND_VERSION}",
  "helmVersion": "${HELM_VERSION}",
  "kubeVersion": "${KUBE_VERSION}",
  "baseImage": "${BASE_IMAGE}",
  "chartVersions": {
    "tide": "${CHART_TIDE_VERSION}",
    "tide-crds": "${CHART_TIDE_CRDS_VERSION}"
  },
  "phases": ${PHASES_JSON},
  "timestamp": "${TIMESTAMP_UTC}"
}
EOF

echo "OK: rendered ${REPORT_PATH}"
