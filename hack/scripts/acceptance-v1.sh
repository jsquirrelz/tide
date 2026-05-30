#!/usr/bin/env bash
# Phase 5 D-A4 / Phase 6 D-05 — maintainer acceptance ritual.
#
# Two modes:
#   ACCEPTANCE_SAMPLE=large (default): $25 hard cap; requires ANTHROPIC_API_KEY + GH_PAT.
#     Spins a fresh kind cluster, helm-installs the two charts, creates the
#     tide-secrets Secret in the large-sample namespace, applies the large-
#     sample Project, waits up to 4h for Status.Phase=Complete (or BudgetExceeded),
#     captures evidence under .acceptance-runs/<unix-ts>/, then invokes the
#     7-check D-A3 verifier (Check 2 asserts 3-of-4 commit shapes per MEDIUM-6
#     — milestone shape is N/A for D-A1 Single Phase scope).
#
#   ACCEPTANCE_SAMPLE=small ($0 mode, D-05): no API key or PAT required.
#     Uses stub-subagent (cmd/stub-subagent) — zero LLM cost. Applies
#     examples/projects/small/project.yaml, waits 10m for Complete.
#     Skips acceptance-verify.sh large-sample git/budget/gitleaks assertions (D-06).
#
# NOT wired into CI per D-A4 — maintainer ritual only.
# Cost: $25 per large-sample run; $0 per small-sample run.
# Env vars required for large mode (fail-fast):
#   ANTHROPIC_API_KEY   — provider key for the credproxy / subagent (Phase 2 D-C1)
#   GH_PAT              — git PAT for the per-run-branch push (Phase 3 D-B6)
#
# Evidence under .acceptance-runs/<ts>/:
#   controller.log      — kubectl logs deploy/tide-controller-manager --since=<run-start>
#   crds.yaml           — kubectl get project,phase,plan,task,wave,job -A -o yaml
#   run-branch.log      — git log of tide/run-large-project-* per-run branch (D-A3 #1, large only)
#   verifier.log        — output of acceptance-verify.sh (large only)
#   (dashboard screenshot is manual per RESEARCH §"Open Questions Q5")
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/../.." && pwd)"

# ACCEPTANCE_SAMPLE controls which project is applied and whether API creds are required.
# large (default): the $25 large-project run (D-A1..D-A4)
# small:           the $0 stub-subagent run (D-05 / ACC-01 BOOT-04 revalidation)
ACCEPTANCE_SAMPLE="${ACCEPTANCE_SAMPLE:-large}"

# ── Env-gate (fail-fast before doing anything destructive) ──────────────────
# Small-sample mode ($0) explicitly requires no API key or git PAT — failing
# fast on their absence would prevent the $0 path from working (D-05).
if [ "${ACCEPTANCE_SAMPLE}" != "small" ]; then
  : "${ANTHROPIC_API_KEY:?ANTHROPIC_API_KEY required for acceptance-v1 — see docs/INSTALL.md}"
  : "${GH_PAT:?GH_PAT required for git creds Secret — see docs/INSTALL.md for PAT scopes (repo:write minimum)}"
fi

TIMESTAMP=$(date +%s)
RUN_START=$(date -u +%Y-%m-%dT%H:%M:%SZ)
ACCEPTANCE_DIR="${REPO_ROOT}/.acceptance-runs/${TIMESTAMP}"
mkdir -p "${ACCEPTANCE_DIR}"

CLUSTER_NAME="tide-acceptance-${TIMESTAMP}"

# Project name and namespace vary by sample mode (D-05 / CHANGE 2).
if [ "${ACCEPTANCE_SAMPLE}" = "small" ]; then
  PROJECT_NAME="small-project"
  PROJECT_NAMESPACE="tide-sample-small"
else
  PROJECT_NAME="large-project"
  PROJECT_NAMESPACE="tide-sample-large"
fi

echo "Phase 5/6 acceptance-v1 starting:"
echo "  ACCEPTANCE_DIR    = ${ACCEPTANCE_DIR}"
echo "  RUN_START         = ${RUN_START}"
echo "  CLUSTER_NAME      = ${CLUSTER_NAME}"
echo "  PROJECT_NAME      = ${PROJECT_NAME} (in ${PROJECT_NAMESPACE})"
echo "  Budget cap        = \$25 (no bypass — D-A2)"
echo ""

# ── Cluster bring-up ────────────────────────────────────────────────────────
echo "==> creating fresh kind cluster ${CLUSTER_NAME}..."
kind create cluster --name "${CLUSTER_NAME}"

# Auto-detect: pull published images or build+kind-load locally (IMG-LOAD-01 / RESEARCH Pitfall 1).
# Uses docker manifest inspect probe; never delegates to a Makefile target that hardcodes
# the cluster name. Ensures all 6 D-03 chart images are present before helm install.
IMAGE_TAG="1.0.0"  # matches chart appVersion after CHART-01
bash "${REPO_ROOT}/hack/scripts/load-images-if-needed.sh" "${CLUSTER_NAME}" "${IMAGE_TAG}"

# ── cert-manager (required by charts/tide webhook + metrics Certificates) ───
# The tide chart references cert-manager.io/v1 Certificate + Issuer resources
# in charts/tide/templates/{serving-cert,selfsigned-issuer,metrics-certs}.yaml.
# Helm refuses to apply CRs whose CRDs aren't present, so cert-manager MUST be
# installed BEFORE the helm-install steps below. Mirrors the Layer B
# integration test pattern at test/integration/kind/suite_test.go:329-369.
# Pinned v1.20.2 per Phase 02.2 cert-manager bump decision (K8s 1.33-compatible).
CERT_MANAGER_VERSION="${TIDE_CERT_MANAGER_VERSION:-v1.20.2}"
echo "==> installing cert-manager ${CERT_MANAGER_VERSION}..."
kubectl apply -f "https://github.com/cert-manager/cert-manager/releases/download/${CERT_MANAGER_VERSION}/cert-manager.yaml"
echo "==> waiting for cert-manager Deployments to roll out..."
for deploy in cert-manager cert-manager-cainjector cert-manager-webhook; do
  kubectl -n cert-manager rollout status deployment/"${deploy}" --timeout=120s
done

# ── Helm install (both charts) ──────────────────────────────────────────────
echo "==> helm-installing tide-crds + tide..."
helm install tide-crds "${REPO_ROOT}/charts/tide-crds" -n tide-system --create-namespace
# Override the chart's default RWX PVC accessModes → RWO for single-node kind:
# kind's default StorageClass (rancher.io/local-path) only supports ReadWriteOnce,
# so the chart's ReadWriteMany tide-projects PVC stays Pending and the controller
# never reaches Available. RWO is correct on a single node (same-node pods share it).
# Mirrors the proven Layer B pattern at test/integration/kind/suite_test.go:480.
helm install tide "${REPO_ROOT}/charts/tide" -n tide-system \
  --set 'workspaces.pvc.accessModes={ReadWriteOnce}'
kubectl wait --for=condition=Available deploy/tide-controller-manager -n tide-system --timeout=5m

# ── Apply project + watch (mode-specific, D-05 / CHANGE 4) ─────────────────
if [ "${ACCEPTANCE_SAMPLE}" = "small" ]; then
  # $0 small-sample mode — stub-subagent, no API key, no per-run branch (D-05).
  # absoluteCapCents: 0 in small/project.yaml enforces zero API cost at controller level.
  echo "==> applying examples/projects/small/project.yaml (\$0 stub mode — D-05)..."
  kubectl apply -f "${REPO_ROOT}/examples/projects/small/project.yaml"

  echo "==> waiting up to 10m for Project Status.Phase=Complete..."
  kubectl wait \
    --for=jsonpath="{.status.phase}"=Complete \
    "project/${PROJECT_NAME}" \
    -n "${PROJECT_NAMESPACE}" \
    --timeout=10m

else
  # ── Namespace + Secret for the large sample ───────────────────────────────
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

  # ── Apply the large Project + watch ───────────────────────────────────────
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
fi

# ── Evidence capture (D-A4) ─────────────────────────────────────────────────
echo "==> capturing evidence under ${ACCEPTANCE_DIR}/..."

# Controller logs since run-start (Check 4 reads this for ERROR-level filtering).
kubectl logs -n tide-system deploy/tide-controller-manager --since-time="${RUN_START}" \
  > "${ACCEPTANCE_DIR}/controller.log" 2>&1 || true

# All TIDE CRDs + Jobs in YAML form (Checks 3, 5, 7 read this).
kubectl get project,phase,plan,task,wave,job -A -o yaml \
  > "${ACCEPTANCE_DIR}/crds.yaml" 2>&1 || true

# Per-run-branch git log (Check 1 + Check 2 read this; large-sample only).
if [ "${ACCEPTANCE_SAMPLE}" != "small" ]; then
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
fi

echo ""
echo "Acceptance evidence captured. Open dashboard at http://localhost:8080 and"
echo "screenshot it for the release notes (manual per RESEARCH Q5)."
echo "    kubectl port-forward -n tide-system svc/tide-dashboard 8080:8080"
echo ""

# ── 7-check verifier (large-sample only — D-06) ─────────────────────────────
# The small-sample path skips acceptance-verify.sh: the small project has no
# per-run branch, no budget spent, and no gitleaks output to verify. Running
# acceptance-verify.sh unconditionally on the small path would assert large-
# sample invariants (D-A3 checks 1–2, budget counter, gitleaks) that do not
# apply — producing false failures (D-06).
if [ "${ACCEPTANCE_SAMPLE}" != "small" ]; then
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
else
  echo "==> small-sample mode: skipping acceptance-verify.sh (D-06 — large-sample assertions not applicable)"
fi

echo ""
echo "ACCEPTANCE PASS — evidence at ${ACCEPTANCE_DIR}/"
exit 0
