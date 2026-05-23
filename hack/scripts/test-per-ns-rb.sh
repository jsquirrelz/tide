#!/usr/bin/env bash
# Phase 5 plan 05-13 — DIST-01 + AUTH-02 catch-up.
# Verify charts/tide/templates/per-namespace-rolebinding.yaml renders correctly:
#   (1) Empty default — zero per-namespace RoleBindings emitted
#       (only the chart's existing baseline RoleBindings remain).
#   (2) Non-empty — one per-namespace RoleBinding per entry in
#       `.Values.projectNamespaces`, named `tide-orchestrator-<ns>`.
#   (3) subjects[0].namespace == operator namespace (Open Question 6 RESOLVED:
#       central-SA pattern; SA lives in `tide-system`, RoleBindings in the
#       Project namespace).
#
# Run: `make test-per-ns-rb`.
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
CHART_DIR="${REPO_ROOT}/charts/tide"
OPERATOR_NS="tide-system"

if ! command -v helm >/dev/null 2>&1; then
  echo "FAIL: helm not on PATH — install Helm 3.x to run this gate." >&2
  exit 1
fi

# ── Assertion 1: empty default — zero per-namespace RoleBindings ─────────────
EMPTY_COUNT=$(helm template "${CHART_DIR}" -n "${OPERATOR_NS}" 2>/dev/null \
  | grep -c 'name: tide-orchestrator-' || true)
if [ "${EMPTY_COUNT}" -ne 0 ]; then
  echo "FAIL: empty projectNamespaces default emitted ${EMPTY_COUNT} per-namespace RoleBinding(s); expected 0." >&2
  echo "Re-render: helm template ${CHART_DIR} -n ${OPERATOR_NS} | grep 'tide-orchestrator-'" >&2
  exit 1
fi

# ── Assertion 2: --set projectNamespaces='{ns1,ns2}' emits 2 RoleBindings ────
NONEMPTY_COUNT=$(helm template "${CHART_DIR}" \
    --set 'projectNamespaces={tide-acme,tide-globex}' \
    -n "${OPERATOR_NS}" 2>/dev/null \
  | grep -c 'name: tide-orchestrator-' || true)
if [ "${NONEMPTY_COUNT}" -ne 2 ]; then
  echo "FAIL: --set projectNamespaces='{tide-acme,tide-globex}' emitted ${NONEMPTY_COUNT} per-namespace RoleBinding(s); expected 2." >&2
  echo "Re-render: helm template ${CHART_DIR} --set 'projectNamespaces={tide-acme,tide-globex}' -n ${OPERATOR_NS}" >&2
  exit 1
fi

# ── Assertion 3: subjects.namespace == operator namespace (tide-system) ──────
# Render the single-namespace case so the awk window is deterministic, then check
# the subjects[0].namespace value follows the tide-orchestrator-tide-acme block.
SUBJECT_NS=$(helm template "${CHART_DIR}" \
    --set 'projectNamespaces={tide-acme}' \
    -n "${OPERATOR_NS}" 2>/dev/null \
  | awk '
      /name: tide-orchestrator-tide-acme/ { in_rb = 1; next }
      in_rb && /^subjects:/ { in_subjects = 1; next }
      in_subjects && /namespace:/ { print $2; exit }
    ')
if [ "${SUBJECT_NS}" != "${OPERATOR_NS}" ]; then
  echo "FAIL: per-namespace RoleBinding subjects[0].namespace == '${SUBJECT_NS}'; expected '${OPERATOR_NS}' (central-SA pattern, OQ6 RESOLVED)." >&2
  exit 1
fi

# ── Assertion 4: roleRef binds to the Phase 1 manager-role ClusterRole ───────
# Confirms the per-namespace binding grants the consolidated controller verbs
# (no aggregation gap vs the cluster-scoped manager-rolebinding shape).
ROLEREF_HITS=$(helm template "${CHART_DIR}" \
    --set 'projectNamespaces={tide-acme}' \
    -n "${OPERATOR_NS}" 2>/dev/null \
  | awk '
      /name: tide-orchestrator-tide-acme/ { in_rb = 1 }
      in_rb && /^roleRef:/ { in_rr = 1; next }
      in_rr && /name:/ { print $2; in_rr = 0; in_rb = 0 }
    ' \
  | grep -c -- '-manager-role' || true)
if [ "${ROLEREF_HITS}" -lt 1 ]; then
  echo "FAIL: per-namespace RoleBinding roleRef.name does not reference *-manager-role ClusterRole." >&2
  exit 1
fi

echo "PASS: per-namespace-rolebinding template renders correctly (empty=0, 2-set=2, subjects=${OPERATOR_NS}, roleRef matches *-manager-role)."
