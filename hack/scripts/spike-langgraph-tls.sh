#!/usr/bin/env bash
# spike-langgraph-tls.sh — Phase 48 D-06 driver for the live credproxy-TLS
# spike (EVAL-02). Stands up the REAL internal/credproxy binary (fresh
# self-signed CA, real key injection, hardcoded route allowlist — unchanged,
# reused as-is) in a container, mints a throwaway HMAC signed token via
# hack/minttoken, and runs cmd/tide-langgraph-verifier/spike/tls_spike.py
# bind-mounted read-only inside the tide-langgraph-verifier image — sharing
# credproxy's network namespace so 127.0.0.1:8443 resolves correctly even on
# macOS Docker Desktop (a container there cannot otherwise reach the host's
# loopback). One real max_tokens=1 invoke.
#
# Costs ~fractions of a cent against the durable real key at
# ~/.tide/anthropic.key (never committed, never logged — lives outside the
# repo, survives teardowns, per the make eval/spike recipe precedent).
#
# The spike is a RETAINED, re-runnable artifact (48-CONTEXT.md "specifics")
# — re-run this on any of the 7 runtime pins bumping (D-10) to keep the
# SSL_CERT_FILE-alone trust answer a durable regression signal.
#
# Run: make spike-langgraph-tls
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
KEY_FILE="${HOME}/.tide/anthropic.key"
VERIFIER_IMG="${VERIFIER_IMG:-ghcr.io/jsquirrelz/tide-langgraph-verifier:test}"
CREDPROXY_IMG="${CREDPROXY_IMG:-ghcr.io/jsquirrelz/tide-credproxy:test}"
CREDPROXY_CTR="tide-spike-credproxy"

if [ ! -f "${KEY_FILE}" ]; then
  echo "ERROR: ${KEY_FILE} not found — refusing to run the live TLS spike" >&2
  echo "       Place the durable real Anthropic API key at ${KEY_FILE} (outside the repo)." >&2
  echo "       See .planning/phases/48-langgraph-evaluator-image-credproxy-tls-spike/48-05-PLAN.md" >&2
  exit 1
fi

if ! command -v docker >/dev/null 2>&1; then
  echo "ERROR: docker not on PATH — required to run the live TLS spike" >&2
  exit 1
fi

REAL_KEY="$(cat "${KEY_FILE}")"
if [ -z "${REAL_KEY}" ]; then
  echo "ERROR: ${KEY_FILE} is empty — refusing to run the live TLS spike" >&2
  exit 1
fi

# Build both images if not already present locally (mirrors
# test-verifier-readonly.sh's auto-build-if-missing convenience).
if ! docker image inspect "${VERIFIER_IMG}" >/dev/null 2>&1; then
  echo "building ${VERIFIER_IMG} (docker-build-langgraph-verifier)..."
  ( cd "${REPO_ROOT}" && docker build -t "${VERIFIER_IMG}" -f cmd/tide-langgraph-verifier/Dockerfile . )
fi
if ! docker image inspect "${CREDPROXY_IMG}" >/dev/null 2>&1; then
  echo "building ${CREDPROXY_IMG} (images/credproxy/Dockerfile)..."
  ( cd "${REPO_ROOT}" && docker build -t "${CREDPROXY_IMG}" -f images/credproxy/Dockerfile . )
fi

# Throwaway HMAC signing key (NEVER the real Anthropic key) — fresh per run,
# discarded on exit; only binds this run's minted token to this run's
# credproxy instance.
SIGNING_KEY="$(head -c 48 /dev/urandom | base64 | tr -dc 'A-Za-z0-9' | head -c 40)"
TASK_UID="tls-spike-$(date +%s)"

CERT_DIR="$(mktemp -d)"
cleanup() {
  docker rm -f "${CREDPROXY_CTR}" >/dev/null 2>&1 || true
  rm -rf "${CERT_DIR}"
}
trap cleanup EXIT

echo "starting credproxy (fresh self-signed CA, real key injection)..."
docker rm -f "${CREDPROXY_CTR}" >/dev/null 2>&1 || true
docker run -d --name "${CREDPROXY_CTR}" \
  -v "${CERT_DIR}:/etc/tide/proxy" \
  -e TIDE_TASK_UID="${TASK_UID}" \
  -e TIDE_SIGNING_KEY="${SIGNING_KEY}" \
  -e ANTHROPIC_API_KEY="${REAL_KEY}" \
  "${CREDPROXY_IMG}" >/dev/null

# Wait for credproxy's plaintext boot banner (the literal
# test/integration/kind/credproxy_test.go asserts on) — confirms the
# listener actually bound, not merely that the cert files exist yet.
READY=0
for _ in $(seq 1 30); do
  if docker logs "${CREDPROXY_CTR}" 2>&1 | grep -q "credproxy listening on"; then
    READY=1
    break
  fi
  sleep 0.5
done
if [ "${READY}" -ne 1 ] || [ ! -f "${CERT_DIR}/ca.crt" ]; then
  echo "ERROR: credproxy did not start within 15s" >&2
  docker logs "${CREDPROXY_CTR}" >&2 || true
  exit 1
fi

echo "minting throwaway signed token (hack/minttoken)..."
TOKEN="$(cd "${REPO_ROOT}" && go run ./hack/minttoken -signing-key="${SIGNING_KEY}" -task-uid="${TASK_UID}" -valid-for=5m)"
if [ -z "${TOKEN}" ]; then
  echo "ERROR: hack/minttoken produced no token" >&2
  exit 1
fi

echo "running the TLS spike inside ${VERIFIER_IMG} (sharing credproxy's network namespace)..."
set +e
docker run --rm \
  --network "container:${CREDPROXY_CTR}" \
  -v "${REPO_ROOT}/cmd/tide-langgraph-verifier/spike:/spike:ro" \
  -v "${CERT_DIR}:/etc/tide/proxy:ro" \
  -e ANTHROPIC_BASE_URL="https://127.0.0.1:8443" \
  -e SSL_CERT_FILE="/etc/tide/proxy/ca.crt" \
  -e TIDE_SIGNED_TOKEN="${TOKEN}" \
  --entrypoint python \
  "${VERIFIER_IMG}" \
  /spike/tls_spike.py
RC=$?
set -e

exit "${RC}"
