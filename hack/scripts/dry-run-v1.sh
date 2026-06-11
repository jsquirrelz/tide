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
#
# DinD heredoc split (IMG-LOAD-01 + DRY-01):
#   Pass 1: kind cluster creation + tool installs + git clone
#   Outer: load-images-if-needed.sh loads all 6 images into tide-dry-run cluster
#   Pass 2: cert-manager bring-up (DRY-01) + helm install + kubectl apply + wait
#
# The two-pass split works because the DinD container uses the host Docker daemon
# (via /var/run/docker.sock mount) — a kind cluster created in Pass 1 is
# reachable from the outer script via `kind get clusters`.
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/../.." && pwd)"

# Output / timing knobs (env-overridable for local dev + CI workflows).
DRY_RUN_DIR="${DRY_RUN_DIR:-/tmp/tide-dry-run-$(date +%s)}"
REPORT_PATH="${DRY_RUN_DIR}/dry-run-report.json"
TRANSCRIPT_PATH="${DRY_RUN_DIR}/transcript.log"
TIMEOUT_SECONDS="${DRY_RUN_TIMEOUT_SECONDS:-1800}"
DRY_RUN_IMAGE="${DRY_RUN_IMAGE:-ubuntu:24.04}"
# DRY_RUN_REPO_URL is the clone source as seen from INSIDE the DinD container.
# Default: /host-repo — the host checkout (REPO_ROOT) mounted read-only into
# the container. This keeps the Quickstart's `git clone` step real while
# working for a private repo (an unauthenticated https://github.com clone
# fails with "could not read Username") AND pins the dry-run to the exact
# tag/commit CI checked out instead of the remote default branch. Override
# with a real remote URL (e.g. once the repo is public) to exercise the
# network path.
DRY_RUN_REPO_URL="${DRY_RUN_REPO_URL:-/host-repo}"

# cert-manager version override (mirrors acceptance-v1.sh; default pinned to
# Phase 02.2 bump decision — K8s 1.33-compatible).
TIDE_CERT_MANAGER_VERSION="${TIDE_CERT_MANAGER_VERSION:-v1.20.2}"

mkdir -p "${DRY_RUN_DIR}"

echo "Phase 5 D-D1 dry-run starting:"
echo "  DRY_RUN_DIR        = ${DRY_RUN_DIR}"
echo "  TIMEOUT_SECONDS    = ${TIMEOUT_SECONDS}"
echo "  DRY_RUN_IMAGE      = ${DRY_RUN_IMAGE}"
echo "  DRY_RUN_REPO_URL   = ${DRY_RUN_REPO_URL}"
echo "  REPO_ROOT (host)   = ${REPO_ROOT}"

START_TIME=$(date +%s)

# ── Pass 1: cluster creation + tool installs + git clone ─────────────────────
# Creates the tide-dry-run kind cluster using the host Docker daemon via the
# /var/run/docker.sock mount. The outer script can then call
# load-images-if-needed.sh to kind-load images before Pass 2 runs helm install.
docker run --rm \
  -v /var/run/docker.sock:/var/run/docker.sock \
  -v "${DRY_RUN_DIR}":/workspace \
  -v "${REPO_ROOT}":/host-repo:ro \
  --network host \
  "${DRY_RUN_IMAGE}" bash -c "
    set -euo pipefail
    # docker.io provides the docker CLI — kind's 'docker info' needs it to reach
    # the mounted host daemon socket (the sibling-container DinD pattern). Without
    # it kind create cluster fails: exec: \"docker\": executable file not found.
    apt-get update -qq && apt-get install -qq -y curl ca-certificates git docker.io

    # Pinned tool installs (match ci.yaml; D-D2)
    curl -fsSLo /usr/local/bin/kind https://kind.sigs.k8s.io/dl/v0.31.0/kind-linux-amd64
    chmod +x /usr/local/bin/kind

    curl -fsSL https://get.helm.sh/helm-v3.16.3-linux-amd64.tar.gz | tar xz -C /tmp
    mv /tmp/linux-amd64/helm /usr/local/bin/helm

    curl -fsSLo /usr/local/bin/kubectl https://dl.k8s.io/release/v1.31.0/bin/linux/amd64/kubectl
    chmod +x /usr/local/bin/kubectl

    # Create the kind cluster (uses host Docker daemon via /var/run/docker.sock).
    kind create cluster --name tide-dry-run

    # Clone the repo into the shared workspace volume so Pass 2 can find it.
    # The default source is /host-repo (the runner checkout, mounted ro above)
    # — the container runs as root while the mount is owned by the host uid,
    # so mark it safe before git will read it. When cloning from /host-repo,
    # pin the clone to the host checkout's exact HEAD: on rc-tag CI runs the
    # host repo is in detached-HEAD state at the tag, and a plain clone would
    # otherwise land on the default branch (or nothing at all).
    if [ \"${DRY_RUN_REPO_URL}\" = \"/host-repo\" ]; then
      # '*' not a single path: the clone's upload-pack child resolves the
      # source as /host-repo/.git, which a bare /host-repo entry does not
      # cover ('detected dubious ownership in repository at /host-repo/.git').
      # The container is throwaway, so the blanket exception is safe.
      git config --global --add safe.directory '*'
      SRC_HEAD=\$(git -C /host-repo rev-parse HEAD)
      git clone /host-repo /workspace/tide
      git -C /workspace/tide checkout --detach \"\${SRC_HEAD}\"
    else
      git clone ${DRY_RUN_REPO_URL} /workspace/tide
    fi
  " 2>&1 | tee "${TRANSCRIPT_PATH}"

# ── Outer: load images into tide-dry-run cluster (IMG-LOAD-01) ───────────────
# The kind cluster created in Pass 1 is reachable from the outer script because
# it uses the host Docker daemon. load-images-if-needed.sh uses docker manifest
# inspect to probe registry availability; builds + kind-loads locally if absent.
echo "==> loading images into tide-dry-run cluster (IMG-LOAD-01)..."
bash "${REPO_ROOT}/hack/scripts/load-images-if-needed.sh" "tide-dry-run" "1.0.0" 2>&1 | tee -a "${TRANSCRIPT_PATH}"

# ── Pass 2: cert-manager + helm install + kubectl apply + wait ────────────────
# Runs inside the same DinD environment. The kind cluster and /workspace/tide
# clone are both available from Pass 1. cert-manager is installed BEFORE helm
# install because charts/tide references cert-manager.io/v1 CRDs (DRY-01).
docker run --rm \
  -v /var/run/docker.sock:/var/run/docker.sock \
  -v "${DRY_RUN_DIR}":/workspace \
  --network host \
  -e TIDE_CERT_MANAGER_VERSION="${TIDE_CERT_MANAGER_VERSION}" \
  "${DRY_RUN_IMAGE}" bash -c "
    set -euo pipefail

    # Re-install tools (new container; binaries from Pass 1 are gone).
    apt-get update -qq && apt-get install -qq -y curl ca-certificates

    curl -fsSLo /usr/local/bin/helm https://get.helm.sh/helm-v3.16.3-linux-amd64.tar.gz 2>/dev/null || true
    curl -fsSL https://get.helm.sh/helm-v3.16.3-linux-amd64.tar.gz | tar xz -C /tmp
    mv /tmp/linux-amd64/helm /usr/local/bin/helm

    curl -fsSLo /usr/local/bin/kubectl https://dl.k8s.io/release/v1.31.0/bin/linux/amd64/kubectl
    chmod +x /usr/local/bin/kubectl

    curl -fsSLo /usr/local/bin/kind https://kind.sigs.k8s.io/dl/v0.31.0/kind-linux-amd64
    chmod +x /usr/local/bin/kind

    cd /workspace/tide

    # DRY-01: cert-manager bring-up (mirrors acceptance-v1.sh lines 88-94).
    # charts/tide references cert-manager.io/v1 Certificate + Issuer resources;
    # helm refuses to apply CRs whose CRDs are not present.
    CERT_MANAGER_VERSION=\"\${TIDE_CERT_MANAGER_VERSION:-v1.20.2}\"
    echo \"==> installing cert-manager \${CERT_MANAGER_VERSION}...\"
    kubectl apply -f \"https://github.com/cert-manager/cert-manager/releases/download/\${CERT_MANAGER_VERSION}/cert-manager.yaml\"
    echo \"==> waiting for cert-manager Deployments to roll out...\"
    for deploy in cert-manager cert-manager-cainjector cert-manager-webhook; do
      kubectl -n cert-manager rollout status deployment/\"\${deploy}\" --timeout=120s
    done

    # Helm install (README Quickstart path — locally-cloned chart paths).
    helm install tide-crds ./charts/tide-crds -n tide-system --create-namespace
    # RWO PVC override for single-node kind (rancher.io/local-path has no RWX) —
    # mirrors acceptance-v1.sh + test/integration/kind/suite_test.go:480.
    helm install tide ./charts/tide -n tide-system --set 'workspaces.pvc.accessModes={ReadWriteOnce}'
    kubectl wait --for=condition=Available deploy/tide-controller-manager -n tide-system --timeout=5m

    kubectl apply -f examples/projects/small/project.yaml
    kubectl wait --for=jsonpath='{.status.phase}'=Complete project/small-project -n tide-sample-small --timeout=10m
  " 2>&1 | tee -a "${TRANSCRIPT_PATH}"

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
