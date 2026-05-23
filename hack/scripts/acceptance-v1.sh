#!/usr/bin/env bash
# Phase 5 D-A4 — maintainer-only acceptance ritual ($25 hard cap; requires ANTHROPIC_API_KEY).
#
# Spins a fresh kind cluster, helm-installs the two charts, creates the
# `tide-secrets` Secret in the large-sample namespace, applies the large-
# sample Project, waits up to 4h for Status.Phase=Complete (or BudgetExceeded),
# captures evidence under .acceptance-runs/<unix-ts>/, then invokes the
# 7-check D-A3 verifier (Check 2 asserts 3-of-4 commit shapes per MEDIUM-6
# — milestone shape is N/A for D-A1 Single Phase scope).
#
# NOT wired into CI per D-A4 — maintainer ritual only. Cost ≈ $25 per run.
# Env vars required (fail-fast):
#   ANTHROPIC_API_KEY   — provider key for the credproxy / subagent (Phase 2 D-C1)
#   GH_PAT              — git PAT for the per-run-branch push (Phase 3 D-B6)
#
# Evidence under .acceptance-runs/<ts>/:
#   controller.log      — kubectl logs deploy/tide-controller-manager --since=<run-start>
#   crds.yaml           — kubectl get project,phase,plan,task,wave,job -A -o yaml
#   run-branch.log      — git log of tide/run-large-project-* per-run branch (D-A3 #1)
#   verifier.log        — output of acceptance-verify.sh (the 7-check pass/fail report)
#   (dashboard screenshot is manual per RESEARCH §"Open Questions Q5")
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/../.." && pwd)"

# ── Env-gate (fail-fast before doing anything destructive) ──────────────────
: "${ANTHROPIC_API_KEY:?ANTHROPIC_API_KEY required for acceptance-v1 — see docs/INSTALL.md}"
: "${GH_PAT:?GH_PAT required for git creds Secret — see docs/INSTALL.md for PAT scopes (repo:write minimum)}"

TIMESTAMP=$(date +%s)
RUN_START=$(date -u +%Y-%m-%dT%H:%M:%SZ)
ACCEPTANCE_DIR="${REPO_ROOT}/.acceptance-runs/${TIMESTAMP}"
mkdir -p "${ACCEPTANCE_DIR}"

CLUSTER_NAME="tide-acceptance-${TIMESTAMP}"
PROJECT_NAME="large-project"
PROJECT_NAMESPACE="tide-sample-large"

echo "Phase 5 D-A4 acceptance-v1 starting:"
echo "  ACCEPTANCE_DIR    = ${ACCEPTANCE_DIR}"
echo "  RUN_START         = ${RUN_START}"
echo "  CLUSTER_NAME      = ${CLUSTER_NAME}"
echo "  PROJECT_NAME      = ${PROJECT_NAME} (in ${PROJECT_NAMESPACE})"
echo "  Budget cap        = \$25 (no bypass — D-A2)"
echo ""

# ── Cluster bring-up ────────────────────────────────────────────────────────
echo "==> creating fresh kind cluster ${CLUSTER_NAME}..."
kind create cluster --name "${CLUSTER_NAME}"

# Image load is optional — the acceptance harness can use published images
# (ghcr.io/jsquirrelz/tide-*:v1.0.0) via pullPolicy:IfNotPresent. Local builds
# can opt into image-load via env (skipped by default for the maintainer
# ritual which targets published images).
if [ -n "${ACCEPTANCE_LOAD_IMAGES:-}" ]; then
  echo "==> loading locally-built images into kind (ACCEPTANCE_LOAD_IMAGES set)..."
  echo "    (delegated to test-int-kind-prep — re-use existing kind cluster name)"
  KIND_CLUSTER="${CLUSTER_NAME}" make -C "${REPO_ROOT}" test-int-kind-prep || true
fi

# ── Helm install (both charts) ──────────────────────────────────────────────
echo "==> helm-installing tide-crds + tide..."
helm install tide-crds "${REPO_ROOT}/charts/tide-crds" -n tide-system --create-namespace
helm install tide "${REPO_ROOT}/charts/tide" -n tide-system
kubectl wait --for=condition=Available deploy/tide-controller-manager -n tide-system --timeout=5m

# ── Namespace + Secret for the large sample ─────────────────────────────────
echo "==> creating namespace + tide-secrets Secret..."
# The namespace ships inline with the large sample's multi-doc YAML — apply
# the namespace first so the Secret can land before the Project.
kubectl apply -f "${REPO_ROOT}/examples/projects/large/project.yaml" --dry-run=client -o yaml 2>/dev/null \
  | awk '/^---$/{p=0} /^apiVersion: v1$/&&!p{p=1} p' \
  | kubectl apply -f - || kubectl create namespace "${PROJECT_NAMESPACE}" --dry-run=client -o yaml | kubectl apply -f -

# Build the Secret from env (never write the key to disk). Combined Secret
# carries both ANTHROPIC_API_KEY (for credproxy/subagent) + GIT_PAT (for the
# per-run-branch push).
kubectl create secret generic tide-secrets \
  --from-literal=ANTHROPIC_API_KEY="${ANTHROPIC_API_KEY}" \
  --from-literal=GIT_PAT="${GH_PAT}" \
  -n "${PROJECT_NAMESPACE}" \
  --dry-run=client -o yaml \
  | kubectl apply -f -

# ── Apply the large Project + watch ─────────────────────────────────────────
echo "==> applying examples/projects/large/project.yaml..."
kubectl apply -f "${REPO_ROOT}/examples/projects/large/project.yaml"

echo "==> waiting up to 4h for Project Status.Phase=Complete (D-A3 #3)..."
# kubectl wait returns non-zero if the Project enters a non-matching phase
# (e.g. BudgetExceeded fires before Complete). The verifier catches both
# success and failure modes via the 7-check sweep — wait failure here is
# captured but doesn't short-circuit evidence capture.
kubectl wait \
  --for=jsonpath="{.status.phase}"=Complete \
  "project/${PROJECT_NAME}" \
  -n "${PROJECT_NAMESPACE}" \
  --timeout=4h \
  || echo "WARN: kubectl wait did not see Complete within 4h — verifier will report final state."

# ── Evidence capture (D-A4) ─────────────────────────────────────────────────
echo "==> capturing evidence under ${ACCEPTANCE_DIR}/..."

# Controller logs since run-start (Check 4 reads this for ERROR-level filtering).
kubectl logs -n tide-system deploy/tide-controller-manager --since-time="${RUN_START}" \
  > "${ACCEPTANCE_DIR}/controller.log" 2>&1 || true

# All TIDE CRDs + Jobs in YAML form (Checks 3, 5, 7 read this).
kubectl get project,phase,plan,task,wave,job -A -o yaml \
  > "${ACCEPTANCE_DIR}/crds.yaml" 2>&1 || true

# Per-run-branch git log (Check 1 + Check 2 read this).
# Fetch the per-run branch from the configured remote; if the branch is not
# pushed yet (e.g. acceptance failed mid-Phase), the verifier surfaces the
# absence as Check 1 fail.
(
  cd "${REPO_ROOT}"
  git fetch origin "+refs/heads/tide/run-${PROJECT_NAME}-*:refs/remotes/origin/tide/run-${PROJECT_NAME}-*" 2>/dev/null || true
  git log --all --oneline --decorate \
    --grep="^tide:" \
    --branches="origin/tide/run-${PROJECT_NAME}-*" \
    > "${ACCEPTANCE_DIR}/run-branch.log" 2>&1 || true
)

echo ""
echo "Acceptance evidence captured. Open dashboard at http://localhost:8080 and"
echo "screenshot it for the release notes (manual per RESEARCH Q5)."
echo "    kubectl port-forward -n tide-system svc/tide-dashboard 8080:8080"
echo ""

# ── 7-check verifier ────────────────────────────────────────────────────────
echo "==> running acceptance-verify.sh (7 D-A3 checks, 3-of-4 commit shapes per D-A1)..."
set +e
bash "${REPO_ROOT}/hack/scripts/acceptance-verify.sh" \
  "${PROJECT_NAME}" \
  "${PROJECT_NAMESPACE}" \
  "${RUN_START}" \
  2>&1 | tee "${ACCEPTANCE_DIR}/verifier.log"
VERIFIER_EXIT=${PIPESTATUS[0]}
set -e

if [ "${VERIFIER_EXIT}" -ne 0 ]; then
  echo ""
  echo "ACCEPTANCE FAIL — verifier exited with ${VERIFIER_EXIT}"
  echo "Evidence at ${ACCEPTANCE_DIR}/"
  exit "${VERIFIER_EXIT}"
fi

echo ""
echo "ACCEPTANCE PASS — evidence at ${ACCEPTANCE_DIR}/"
exit 0
