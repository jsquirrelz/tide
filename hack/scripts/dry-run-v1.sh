#!/usr/bin/env bash
# Phase 5 D-D1 — Docker-in-Docker external-operator dry-run.
#
# Maps the README Quickstart against a clean ubuntu:24.04 image and times the
# whole run. The small-sample Project reaching `Status.Phase=Complete` is the
# timer-stop target (uses the $0 stub-subagent — repeatable without LLM cost).
#
# CI gate behavior (Plan 05-16 wires this into release.yaml on `v*-rc.*` tags):
#   - exits non-zero if the inner pipeline fails any step
#   - exits non-zero if wall-clock elapsed > DRY_RUN_TIMEOUT_SECONDS (default 1800 = 30 min, D-D3)
#   - on PASS, emits transcript.log + dry-run-report.json under DRY_RUN_DIR
#
# Pinned tool versions match `.github/workflows/ci.yaml` (RESEARCH §"P7.1"):
#   kind   v0.31.0
#   helm   v3.16.3
#   kubectl v1.31.0
#
# DinD invocation mounts /var/run/docker.sock (sibling-container pattern, NOT
# nested-dockerd) so `kind create cluster` inside the ubuntu:24.04 container
# reaches the host Docker daemon. --network host scopes kind's bridge to the
# host's network so kubectl from inside the container can reach the apiserver
# on the kind-created network without extra port-forwarding.
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/../.." && pwd)"

# Output / timing knobs (env-overridable for local dev + CI workflows).
DRY_RUN_DIR="${DRY_RUN_DIR:-/tmp/tide-dry-run-$(date +%s)}"
REPORT_PATH="${DRY_RUN_DIR}/dry-run-report.json"
TRANSCRIPT_PATH="${DRY_RUN_DIR}/transcript.log"
TIMEOUT_SECONDS="${DRY_RUN_TIMEOUT_SECONDS:-1800}"
DRY_RUN_IMAGE="${DRY_RUN_IMAGE:-ubuntu:24.04}"
# DRY_RUN_REPO_URL lets local developers point at a worktree-local checkout
# (file:// or http://localhost-served clone) instead of github.com — CI runs
# leave this unset so the canonical https://github.com/jsquirrelz/tide path is
# exercised.
DRY_RUN_REPO_URL="${DRY_RUN_REPO_URL:-https://github.com/jsquirrelz/tide}"

mkdir -p "${DRY_RUN_DIR}"

echo "Phase 5 D-D1 dry-run starting:"
echo "  DRY_RUN_DIR        = ${DRY_RUN_DIR}"
echo "  TIMEOUT_SECONDS    = ${TIMEOUT_SECONDS}"
echo "  DRY_RUN_IMAGE      = ${DRY_RUN_IMAGE}"
echo "  DRY_RUN_REPO_URL   = ${DRY_RUN_REPO_URL}"
echo "  REPO_ROOT (host)   = ${REPO_ROOT}"

START_TIME=$(date +%s)

# Inner DinD pipeline. Each `kind create cluster`, `helm install`, and `kubectl
# apply` runs verbatim from the README Quickstart so a successful dry-run is
# evidence the README is operator-followable end-to-end. The `set -euo
# pipefail` inside the heredoc preserves fail-fast inside the container; an
# inner failure surfaces as a non-zero PIPESTATUS[0] outside.
docker run --rm \
  -v /var/run/docker.sock:/var/run/docker.sock \
  -v "${DRY_RUN_DIR}":/workspace \
  --network host \
  "${DRY_RUN_IMAGE}" bash -c "
    set -euo pipefail
    apt-get update -qq && apt-get install -qq -y curl ca-certificates git

    # Pinned tool installs (match ci.yaml; D-D2)
    curl -fsSLo /usr/local/bin/kind https://kind.sigs.k8s.io/dl/v0.31.0/kind-linux-amd64
    chmod +x /usr/local/bin/kind

    curl -fsSL https://get.helm.sh/helm-v3.16.3-linux-amd64.tar.gz | tar xz -C /tmp
    mv /tmp/linux-amd64/helm /usr/local/bin/helm

    curl -fsSLo /usr/local/bin/kubectl https://dl.k8s.io/release/v1.31.0/bin/linux/amd64/kubectl
    chmod +x /usr/local/bin/kubectl

    # README Quickstart — cloned-repo path (the rc-tag gate runs before OCI
    # charts are published; OCI path goes live in Plan 05-16 once the chart is
    # pushed). Locally-cloned chart paths match the 'helm install ./charts/...'
    # alternative documented in docs/INSTALL.md.
    git clone ${DRY_RUN_REPO_URL} /workspace/tide
    cd /workspace/tide

    kind create cluster --name tide-dry-run
    helm install tide-crds ./charts/tide-crds -n tide-system --create-namespace
    helm install tide ./charts/tide -n tide-system
    kubectl wait --for=condition=Available deploy/tide-controller-manager -n tide-system --timeout=5m

    kubectl apply -f examples/projects/small/project.yaml
    kubectl wait --for=jsonpath='{.status.phase}'=Complete project/small-project -n tide-sample-small --timeout=10m
  " 2>&1 | tee "${TRANSCRIPT_PATH}"

EXIT_CODE=${PIPESTATUS[0]}
END_TIME=$(date +%s)
ELAPSED=$((END_TIME - START_TIME))

# Render dry-run-report.json via the shared shaper script (single-source-of-
# truth schema; Plan 05-16's release.yaml uploads this file alongside
# goreleaser tarballs to the GitHub Release).
bash "${REPO_ROOT}/hack/scripts/render-dry-run-report.sh" \
  "${REPORT_PATH}" \
  "${ELAPSED}" \
  "${EXIT_CODE}"

# Pass / fail evaluation.
if [ "${EXIT_CODE}" -ne 0 ]; then
  echo "FAIL: dry-run inner pipeline exited with code ${EXIT_CODE}"
  echo "Evidence: ${REPORT_PATH} + ${TRANSCRIPT_PATH}"
  exit "${EXIT_CODE}"
fi

if [ "${ELAPSED}" -gt "${TIMEOUT_SECONDS}" ]; then
  echo "FAIL: dry-run exceeded timeout (${ELAPSED}s > ${TIMEOUT_SECONDS}s)"
  echo "Evidence: ${REPORT_PATH} + ${TRANSCRIPT_PATH}"
  exit 1
fi

echo "PASS: dry-run completed in ${ELAPSED}s (under ${TIMEOUT_SECONDS}s cap)"
echo "Evidence: ${REPORT_PATH} + ${TRANSCRIPT_PATH}"
