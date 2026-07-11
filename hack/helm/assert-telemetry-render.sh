#!/usr/bin/env bash
# Copyright 2026 TIDE Authors
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.
#
# hack/helm/assert-telemetry-render.sh — Helm render gate for milestone
# exit-criterion #7 (EC-7 telemetry integration).
#
# Proves EC-7 end-to-end: the charts/tide chart correctly implements
# prometheus.endpoint (PROM_ENDPOINT env injection on the dashboard container)
# and prometheus.retentionTime (documentation-only operator flag) as added by
# phase-04. Distinct from hack/helm/assert-prometheus-env.py (phase-04's own
# render gate); this script covers the four permutations required by EC-7.
#
# Permutations:
#   A — Default/disabled posture: default render must NOT contain PROM_ENDPOINT.
#   B — Endpoint set: dashboard container MUST carry PROM_ENDPOINT with value.
#   C — Retention set: render succeeds + values file documents storage.tsdb.retention.time.
#   D — Lint: helm lint must exit 0.
#   E — Default render: PROMETHEUS_ENABLED env entry present with value "false"
#       (Phase 38 D-14 umbrella key — always rendered, explicit false is meaningful).
#   F — prometheus.enabled=true: PROMETHEUS_ENABLED value is "true".
#   G — NOTES.txt conditional (Phase 38 TELEM-02/D-12): warning present by
#       default, absent with prometheus.enabled=true.
#
# Usage: ./hack/helm/assert-telemetry-render.sh
# Requires: helm, grep (standard coreutils). No cluster connection needed.
set -euo pipefail

# ---------------------------------------------------------------------------
# Repo-root resolution — works from any CWD.
# Prefer git so symlinked worktrees resolve correctly; fall back to dirname.
# ---------------------------------------------------------------------------
if REPO_ROOT="$(git rev-parse --show-toplevel 2>/dev/null)"; then
  : # git found the root
else
  REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
fi

CHART_DIR="${REPO_ROOT}/charts/tide"
# Canonical hand-maintained values file (mirrored byte-identically into
# charts/tide/values.yaml by the augment-tide-chart.sh pipeline).
HACK_VALUES="${REPO_ROOT}/hack/helm/tide-values.yaml"
CHART_VALUES="${CHART_DIR}/values.yaml"

# ---------------------------------------------------------------------------
# Helper — fail fast with a descriptive message and exit non-zero.
# ---------------------------------------------------------------------------
die() {
  echo "FAIL: $*" >&2
  exit 1
}

echo "=== assert-telemetry-render.sh — EC-7 render gate ==="
echo "    chart: ${CHART_DIR}"
echo ""

# ---------------------------------------------------------------------------
# Permutation A — DEFAULT/DISABLED posture
#
# helm template with no overrides must:
#   1. exit 0
#   2. produce NO PROM_ENDPOINT env key (graceful-degradation posture)
# ---------------------------------------------------------------------------
echo "--- Permutation A: default render (no overrides) ---"

RENDER_A="$(helm template "${CHART_DIR}" 2>&1)" \
  || die "[A] helm template charts/tide (no overrides) exited non-zero:
${RENDER_A}"

# Match only an env-entry shape (- name: PROM_ENDPOINT), not any occurrence of
# the token — rendered comments mentioning PROM_ENDPOINT must not trip the gate.
if echo "${RENDER_A}" | grep -qE '^[[:space:]]*-?[[:space:]]*name:[[:space:]]*PROM_ENDPOINT[[:space:]]*$'; then
  die "[A] PROM_ENDPOINT env entry leaked into the default render — graceful-degradation posture violated.
When prometheus.endpoint is empty, the dashboard Deployment must NOT inject PROM_ENDPOINT."
fi

echo "PASS [A]: default render exits 0; no PROM_ENDPOINT in output (graceful-degradation OK)"

# ---------------------------------------------------------------------------
# Permutation B — ENDPOINT SET
#
# helm template --set prometheus.endpoint=http://prom:9090 must:
#   1. exit 0
#   2. contain PROM_ENDPOINT env key in the rendered output
#   3. contain the literal endpoint value http://prom:9090
# ---------------------------------------------------------------------------
echo ""
echo "--- Permutation B: prometheus.endpoint=http://prom:9090 ---"

RENDER_B="$(helm template "${CHART_DIR}" --set prometheus.endpoint=http://prom:9090 2>&1)" \
  || die "[B] helm template --set prometheus.endpoint=http://prom:9090 exited non-zero:
${RENDER_B}"

if ! echo "${RENDER_B}" | grep -qE '^[[:space:]]*-?[[:space:]]*name:[[:space:]]*PROM_ENDPOINT[[:space:]]*$'; then
  die "[B] PROM_ENDPOINT env entry not found in rendered output when prometheus.endpoint is set.
The dashboard Deployment template must inject a PROM_ENDPOINT env entry when prometheus.endpoint is non-empty."
fi

if ! echo "${RENDER_B}" | grep -q "http://prom:9090"; then
  die "[B] Value 'http://prom:9090' not found in rendered output.
PROM_ENDPOINT must carry the exact prometheus.endpoint value."
fi

echo "PASS [B]: PROM_ENDPOINT env entry with value http://prom:9090 is present in rendered output"

# ---------------------------------------------------------------------------
# Permutation C — RETENTION SET (documentation branch)
#
# helm template --set prometheus.retentionTime=30d must:
#   1. exit 0  (the value is accepted by Helm without error)
#
# Because the chart ships a ServiceMonitor (not a bundled Prometheus server),
# retentionTime is documentation-only. The documentation branch assertion:
#   grep the values file for storage.tsdb.retention.time — the operator-managed
#   Prometheus flag that the comment block directs operators to use.
# ---------------------------------------------------------------------------
echo ""
echo "--- Permutation C: prometheus.retentionTime=30d (documentation branch) ---"

RENDER_C="$(helm template "${CHART_DIR}" --set prometheus.retentionTime=30d 2>&1)" \
  || die "[C] helm template --set prometheus.retentionTime=30d exited non-zero:
${RENDER_C}"

# Check the values file documentation (operator-managed-Prometheus flag).
# Accept either the hand-maintained hack file or the chart's values.yaml.
VALUES_DOC_OK=0
for vf in "${HACK_VALUES}" "${CHART_VALUES}"; do
  if [ -f "${vf}" ] && grep -q "storage.tsdb.retention.time" "${vf}"; then
    VALUES_DOC_OK=1
    break
  fi
done

if [ "${VALUES_DOC_OK}" -eq 0 ]; then
  die "[C] Neither '${HACK_VALUES}' nor '${CHART_VALUES}' contains 'storage.tsdb.retention.time'.
The retentionTime comment block must document the operator-managed Prometheus flag
--storage.tsdb.retention.time so operators know how to apply it to their Prometheus instance."
fi

echo "PASS [C]: retentionTime=30d renders without error; values file documents storage.tsdb.retention.time"

# ---------------------------------------------------------------------------
# Permutation D — LINT
#
# helm lint must exit 0 (no template errors, no missing required values).
# ---------------------------------------------------------------------------
echo ""
echo "--- Permutation D: helm lint charts/tide ---"

LINT_OUT="$(helm lint "${CHART_DIR}" 2>&1)" \
  || die "[D] helm lint charts/tide exited non-zero:
${LINT_OUT}"

echo "PASS [D]: helm lint exits 0"

# ---------------------------------------------------------------------------
# Permutation E — PROMETHEUS_ENABLED default (Phase 38 D-14)
#
# The umbrella-key env entry is ALWAYS rendered on the dashboard container
# (unlike PROM_ENDPOINT which is conditional). Default render must carry
# `name: PROMETHEUS_ENABLED` followed by `value: "false"` — an explicit
# "false" is the banner's disabled-by-config signal.
# ---------------------------------------------------------------------------
echo ""
echo "--- Permutation E: default render — PROMETHEUS_ENABLED=false ---"

RENDER_E="$(helm template "${CHART_DIR}" 2>&1)" \
  || die "[E] helm template charts/tide (no overrides) exited non-zero:
${RENDER_E}"

if ! echo "${RENDER_E}" | grep -qE '^[[:space:]]*-?[[:space:]]*name:[[:space:]]*PROMETHEUS_ENABLED[[:space:]]*$'; then
  die "[E] PROMETHEUS_ENABLED env entry not found in the default render.
The dashboard Deployment must ALWAYS render PROMETHEUS_ENABLED (Phase 38 D-14) —
an explicit \"false\" distinguishes disabled-by-config from a legacy chart lacking the key."
fi

if ! echo "${RENDER_E}" | grep -A1 -E '^[[:space:]]*-?[[:space:]]*name:[[:space:]]*PROMETHEUS_ENABLED[[:space:]]*$' \
    | grep -qE '^[[:space:]]*value:[[:space:]]*"false"[[:space:]]*$'; then
  die "[E] PROMETHEUS_ENABLED default value is not \"false\".
prometheus.enabled defaults to false in values.yaml; the env must render value: \"false\"."
fi

echo "PASS [E]: default render carries PROMETHEUS_ENABLED with value \"false\""

# ---------------------------------------------------------------------------
# Permutation F — PROMETHEUS_ENABLED set true
# ---------------------------------------------------------------------------
echo ""
echo "--- Permutation F: prometheus.enabled=true — PROMETHEUS_ENABLED=true ---"

RENDER_F="$(helm template "${CHART_DIR}" --set prometheus.enabled=true 2>&1)" \
  || die "[F] helm template --set prometheus.enabled=true exited non-zero:
${RENDER_F}"

if ! echo "${RENDER_F}" | grep -A1 -E '^[[:space:]]*-?[[:space:]]*name:[[:space:]]*PROMETHEUS_ENABLED[[:space:]]*$' \
    | grep -qE '^[[:space:]]*value:[[:space:]]*"true"[[:space:]]*$'; then
  die "[F] PROMETHEUS_ENABLED value is not \"true\" when prometheus.enabled=true.
The env must carry the quoted umbrella-key value verbatim."
fi

echo "PASS [F]: prometheus.enabled=true renders PROMETHEUS_ENABLED value \"true\""

# ---------------------------------------------------------------------------
# Permutation G — NOTES.txt conditional warning (Phase 38 TELEM-02 / D-12)
#
# NOTES rendering invocation: `helm template --show-only templates/NOTES.txt`
# does NOT emit NOTES.txt on this repo's helm (verified against helm v4.2.0 —
# NOTES is excluded from manifest output), so the codified invocation is the
# client-side dry-run install, which prints the rendered NOTES block without
# needing a cluster connection.
# ---------------------------------------------------------------------------
echo ""
echo "--- Permutation G: NOTES.txt telemetry warning conditional ---"

WARNING_TEXT="run telemetry beyond the budget tally is unavailable"

NOTES_DEFAULT="$(helm install tide-notes "${CHART_DIR}" --dry-run=client 2>&1 | sed -n '/^NOTES:/,$p')" \
  || die "[G] helm install --dry-run=client (default values) exited non-zero:
${NOTES_DEFAULT}"

if [ -z "${NOTES_DEFAULT}" ]; then
  die "[G] No NOTES: block in the default dry-run output — templates/NOTES.txt missing or empty."
fi

if ! echo "${NOTES_DEFAULT}" | grep -qF "${WARNING_TEXT}"; then
  die "[G] Telemetry warning missing from NOTES with default values.
prometheus.enabled defaults to false, so NOTES.txt must print:
  '${WARNING_TEXT}'"
fi

NOTES_ENABLED="$(helm install tide-notes "${CHART_DIR}" --dry-run=client --set prometheus.enabled=true 2>&1 | sed -n '/^NOTES:/,$p')" \
  || die "[G] helm install --dry-run=client --set prometheus.enabled=true exited non-zero:
${NOTES_ENABLED}"

if echo "${NOTES_ENABLED}" | grep -qF "${WARNING_TEXT}"; then
  die "[G] Telemetry warning leaked into NOTES with prometheus.enabled=true.
The warning block must be gated on '{{ if not .Values.prometheus.enabled }}'."
fi

echo "PASS [G]: NOTES warning present by default, absent with prometheus.enabled=true"

# ---------------------------------------------------------------------------
# Summary
# ---------------------------------------------------------------------------
echo ""
echo "PASS: all 7 permutations passed — EC-7 render gate satisfied"
