#!/usr/bin/env bash
# Phase 5 D-A3 / BOOT-04 — 7-check acceptance verifier.
#
# Runs the 7 D-A3 pass criteria sequentially against a live cluster + a
# completed acceptance run. Each check emits `Check N: <name> ...` then on
# success `  OK: <detail>` else `  FAIL: <detail>` + increments FAILS.
#
# Check 2 (REVISED per MEDIUM-6, 2026-05-22): asserts 3-of-4 D-B2 commit-message
# shapes (plan/phase/project). The 4th milestone shape is EXPLICITLY N/A for
# D-A1 Single Phase scope — TIDE never authors a Milestone-level artifact at
# this governance level, so the milestone shape is implicit-not-present rather
# than a coverage gap. A future v1.x test with full Milestone scope adds the
# 4th-shape assertion as a fast-follow plan.
#
# Usage:
#   acceptance-verify.sh <project_name> <namespace> <run_start_iso8601>
#
# Exit code:
#   0  — all 7 checks passed
#   1+ — N checks failed (FAILS count surfaced in summary)
set -euo pipefail

if [ "$#" -lt 3 ]; then
  echo "FAIL: acceptance-verify.sh requires 3 positional args (project_name namespace run_start_iso8601)" >&2
  exit 2
fi

PROJECT_NAME="$1"
NAMESPACE="$2"
RUN_START="$3"

REPO_ROOT="$(cd "$(dirname "$0")/../.." && pwd)"

FAILS=0

echo "Phase 5 D-A3 acceptance-verify starting:"
echo "  PROJECT_NAME      = ${PROJECT_NAME}"
echo "  NAMESPACE         = ${NAMESPACE}"
echo "  RUN_START         = ${RUN_START}"
echo ""

# ───────────────────────────────────────────────────────────────────────────
# (1) per-run branch exists on the configured remote (Phase 3 D-B6 lock).
# Branch naming: tide/run-<project>-<unix-ts>. The acceptance run pushes the
# per-run branch; absence means TIDE never reached the push-phase boundary.
# ───────────────────────────────────────────────────────────────────────────
echo "Check 1: per-run branch tide/run-${PROJECT_NAME}-* exists on remote..."
if (cd "${REPO_ROOT}" && git ls-remote --heads origin "tide/run-${PROJECT_NAME}-*" 2>/dev/null | grep -q .); then
  RUN_BRANCH=$(cd "${REPO_ROOT}" && git ls-remote --heads origin "tide/run-${PROJECT_NAME}-*" | head -1 | awk '{print $2}' | sed 's|refs/heads/||')
  echo "  OK: per-run branch present (${RUN_BRANCH})"
else
  echo "  FAIL: no tide/run-${PROJECT_NAME}-* branch found on remote 'origin' (Phase 3 D-B6 push never completed)"
  FAILS=$((FAILS+1))
  RUN_BRANCH=""
fi

# ───────────────────────────────────────────────────────────────────────────
# (2) 3-of-4 D-B2 commit-message shapes present on the per-run branch.
# MEDIUM-6 revision (2026-05-22): the 4th milestone shape is N/A for D-A1
# Single Phase scope. Asserted shapes:
#   - tide: plan <name> authored + executed     (plan-level boundary)
#   - tide: phase <name> authored               (phase-level boundary)
#   - tide: project complete                    (project closeout)
# ───────────────────────────────────────────────────────────────────────────
echo "Check 2: 3-of-4 D-B2 commit shapes asserted (plan/phase/project); milestone shape N/A for D-A1 Single Phase scope..."
if [ -z "${RUN_BRANCH}" ]; then
  echo "  FAIL: cannot inspect commit shapes (no per-run branch from Check 1)"
  FAILS=$((FAILS+1))
else
  # Fetch the per-run branch locally so `git log` works against it.
  (cd "${REPO_ROOT}" && git fetch origin "+refs/heads/${RUN_BRANCH}:refs/remotes/origin/${RUN_BRANCH}" 2>/dev/null || true)

  SHAPE_MISSING=()
  PLAN_HITS=$(cd "${REPO_ROOT}" && git log "origin/${RUN_BRANCH}" --grep='tide: plan .* authored + executed' --pretty=format:'%h' 2>/dev/null | wc -l | tr -d ' ')
  PHASE_HITS=$(cd "${REPO_ROOT}" && git log "origin/${RUN_BRANCH}" --grep='tide: phase .* authored' --pretty=format:'%h' 2>/dev/null | wc -l | tr -d ' ')
  PROJECT_HITS=$(cd "${REPO_ROOT}" && git log "origin/${RUN_BRANCH}" --grep='tide: project complete' --pretty=format:'%h' 2>/dev/null | wc -l | tr -d ' ')

  [ "${PLAN_HITS:-0}" -lt 1 ] && SHAPE_MISSING+=("plan")
  [ "${PHASE_HITS:-0}" -lt 1 ] && SHAPE_MISSING+=("phase")
  [ "${PROJECT_HITS:-0}" -lt 1 ] && SHAPE_MISSING+=("project")

  if [ "${#SHAPE_MISSING[@]}" -eq 0 ]; then
    echo "  OK: 3-of-4 in-scope commit shapes present (plan=${PLAN_HITS}, phase=${PHASE_HITS}, project=${PROJECT_HITS}); milestone N/A (D-A1)"
  else
    echo "  FAIL: missing commit shapes: ${SHAPE_MISSING[*]} (counts: plan=${PLAN_HITS}, phase=${PHASE_HITS}, project=${PROJECT_HITS})"
    FAILS=$((FAILS+1))
  fi
fi

# ───────────────────────────────────────────────────────────────────────────
# (3) Project.Status.Phase == Complete.
# ───────────────────────────────────────────────────────────────────────────
echo "Check 3: Project.Status.Phase == Complete..."
STATUS=$(kubectl get project "${PROJECT_NAME}" -n "${NAMESPACE}" -o jsonpath='{.status.phase}' 2>/dev/null || echo "<missing>")
if [ "${STATUS}" = "Complete" ]; then
  echo "  OK: Project.Status.Phase=Complete"
else
  echo "  FAIL: Project.Status.Phase=${STATUS} (expected Complete; common non-complete states: BudgetExceeded, Failed, Running)"
  FAILS=$((FAILS+1))
fi

# ───────────────────────────────────────────────────────────────────────────
# (4) zero ERROR-level controller logs since RUN_START.
# Uses the manager's JSON log shape ({"level":"ERROR", ...}). `|| true` after
# grep handles the no-match exit-non-zero gotcha.
# ───────────────────────────────────────────────────────────────────────────
echo "Check 4: zero ERROR-level controller log lines since RUN_START..."
ERROR_COUNT=$(kubectl logs -n tide-system deploy/tide-controller-manager --since-time="${RUN_START}" 2>/dev/null \
  | grep -cE '"level":"ERROR"' || true)
if [ "${ERROR_COUNT:-0}" -eq 0 ]; then
  echo "  OK: zero ERROR logs since ${RUN_START}"
else
  echo "  FAIL: ${ERROR_COUNT} ERROR-level controller logs since ${RUN_START}"
  FAILS=$((FAILS+1))
fi

# ───────────────────────────────────────────────────────────────────────────
# (5) no orphan Jobs. Every Job with project-uid label has
# status.succeeded=1. Project-uid scopes the query to THIS run's Jobs only.
# ───────────────────────────────────────────────────────────────────────────
echo "Check 5: all Jobs for this project succeeded (no orphans)..."
PROJECT_UID=$(kubectl get project "${PROJECT_NAME}" -n "${NAMESPACE}" -o jsonpath='{.metadata.uid}' 2>/dev/null || echo "")
if [ -z "${PROJECT_UID}" ]; then
  echo "  FAIL: cannot read Project UID for ${PROJECT_NAME} in ${NAMESPACE} (Project missing?)"
  FAILS=$((FAILS+1))
else
  # Each Job emits "<name>=<succeeded>" — non-succeeded Jobs surface as
  # status.succeeded empty or != 1.
  JOB_REPORT=$(kubectl get jobs --all-namespaces -l "tideproject.k8s/project-uid=${PROJECT_UID}" \
    -o jsonpath='{range .items[*]}{.metadata.namespace}/{.metadata.name}={.status.succeeded}{"\n"}{end}' 2>/dev/null || true)
  if [ -z "${JOB_REPORT}" ]; then
    echo "  FAIL: zero Jobs found with label tideproject.k8s/project-uid=${PROJECT_UID} (expected ≥ 1 — at minimum the planner Job)"
    FAILS=$((FAILS+1))
  else
    ORPHANS=$(echo "${JOB_REPORT}" | grep -vE '=1$' || true)
    if [ -n "${ORPHANS}" ]; then
      echo "  FAIL: orphan Jobs (status.succeeded != 1):"
      echo "${ORPHANS}" | sed 's/^/    /'
      FAILS=$((FAILS+1))
    else
      JOB_COUNT=$(echo "${JOB_REPORT}" | wc -l | tr -d ' ')
      echo "  OK: all ${JOB_COUNT} Job(s) succeeded"
    fi
  fi
fi

# ───────────────────────────────────────────────────────────────────────────
# (6) tide_secret_leak_blocked_total{project=<name>} == 0.
# Hit the manager's /metrics endpoint via kubectl exec; tolerate missing
# wget by falling back to a `--raw` proxy URL. Either way, the metric value
# must be 0 or empty (gitleaks blocked nothing → no leaks detected).
# ───────────────────────────────────────────────────────────────────────────
echo "Check 6: tide_secret_leak_blocked_total{project=${PROJECT_NAME}} == 0..."
METRICS=$(kubectl exec -n tide-system deploy/tide-controller-manager -c manager -- wget -qO- localhost:8080/metrics 2>/dev/null \
  || kubectl get --raw "/api/v1/namespaces/tide-system/services/tide-controller-manager-metrics-service:8443/proxy/metrics" 2>/dev/null \
  || echo "")
if [ -z "${METRICS}" ]; then
  echo "  FAIL: could not reach /metrics endpoint (wget unavailable + apiserver proxy returned empty)"
  FAILS=$((FAILS+1))
else
  LEAK_VALUE=$(echo "${METRICS}" \
    | grep "^tide_secret_leak_blocked_total" \
    | grep "project=\"${PROJECT_NAME}\"" \
    | awk '{print $2}' \
    | head -1)
  # Empty or 0 = pass; any other value = fail.
  if [ -z "${LEAK_VALUE}" ] || [ "${LEAK_VALUE}" = "0" ]; then
    echo "  OK: tide_secret_leak_blocked_total{project=${PROJECT_NAME}}=${LEAK_VALUE:-0}"
  else
    echo "  FAIL: tide_secret_leak_blocked_total{project=${PROJECT_NAME}}=${LEAK_VALUE} (gitleaks blocked ${LEAK_VALUE} push(es))"
    FAILS=$((FAILS+1))
  fi
fi

# ───────────────────────────────────────────────────────────────────────────
# (7) Project.Status.budget.costSpentCents < 2500 ($25 cap, D-A2).
# Empty / null treated as 0 (early-failure scenarios where budget never
# accumulated). Hitting the cap (=2500) fails Check 7 by intent — the cap
# firing means the acceptance test halted on cost.
# ───────────────────────────────────────────────────────────────────────────
echo "Check 7: Project.Status.budget.costSpentCents < 2500..."
SPENT=$(kubectl get project "${PROJECT_NAME}" -n "${NAMESPACE}" -o jsonpath='{.status.budget.costSpentCents}' 2>/dev/null || echo "")
SPENT="${SPENT:-0}"
if [ "${SPENT}" -lt 2500 ] 2>/dev/null; then
  echo "  OK: costSpentCents=${SPENT} (under \$25 cap)"
else
  echo "  FAIL: costSpentCents=${SPENT} (>= 2500 — \$25 cap exceeded or status missing)"
  FAILS=$((FAILS+1))
fi

# ───────────────────────────────────────────────────────────────────────────
# Summary
# ───────────────────────────────────────────────────────────────────────────
echo ""
if [ "${FAILS}" -gt 0 ]; then
  echo "FAIL: ${FAILS} of 7 D-A3 checks failed"
  exit 1
fi

echo "PASS: all 7 D-A3 checks passed (Check 2 asserted 3-of-4 commit shapes; milestone N/A for D-A1 Single Phase)"
