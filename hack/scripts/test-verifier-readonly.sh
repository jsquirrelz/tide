#!/usr/bin/env bash
# test-verifier-readonly.sh — Phase 48 EVAL-01 / D-09b adversarial behavioral
# proof that tide-langgraph-verifier's read-only contract holds at the
# filesystem/credential layer, not by prompt refusal.
#
# This is the SECOND, independent read-only proof layer: 48-02 already unit-
# asserts the jobspec statically (D-09a — ReadOnly:true mount, no push
# credentials, no child-CRD path). This script instead drives the REAL image
# against a REAL read-only mount and a pre-materialized fixture git worktree,
# then deliberately tries to break the contract from inside the container:
#
#   (a) `git commit` against the :ro-mounted worktree        -> EROFS
#   (b) `git push` against a credential-requiring remote      -> auth failure
#   (c) direct writes to the mount AND the container rootfs   -> EROFS (both)
#
# Each probe OVERRIDES the image's entrypoint (`--entrypoint`) to drive git/sh
# directly. This is DELIBERATE, not an oversight: it bypasses the verifier's
# own git_read/run_gate_command tool allowlist entirely, because the claim
# under test is that the mount/credential layer holds even when the tool
# layer is defeated — prompt refusal is explicitly NOT the mechanism under
# test here (EVAL-01).
#
# The fixture worktree is pre-materialized by THIS SCRIPT (git init + commit)
# BEFORE it is ever mounted read-only — the container performs zero worktree
# creation of its own (Pitfall D: `git worktree add` is not itself a
# read-only operation, so if the container ever ran it, it would need a
# writable mount; this test's fixture is created entirely outside the
# container to keep that concern out of scope).
#
# Run: `make test-verifier-readonly` (builds the image first via
# `make docker-build-langgraph-verifier` unless IMG is overridden).
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
IMG="${IMG:-ghcr.io/jsquirrelz/tide-langgraph-verifier:test}"

if ! command -v docker >/dev/null 2>&1; then
  echo "FAIL: docker not on PATH — required to run the adversarial behavioral probes." >&2
  exit 1
fi

if ! docker image inspect "${IMG}" >/dev/null 2>&1; then
  echo "building ${IMG} (docker-build-langgraph-verifier)..."
  ( cd "${REPO_ROOT}" && docker build -t "${IMG}" -f cmd/tide-langgraph-verifier/Dockerfile . )
fi

# ── Pre-materialize the fixture worktree OUTSIDE any container ──────────────
FIXTURE="$(mktemp -d)"
cleanup() { rm -rf "${FIXTURE}"; }
trap cleanup EXIT

git -C "${FIXTURE}" init -q
git -C "${FIXTURE}" config user.email "fixture@tide.test"
git -C "${FIXTURE}" config user.name "tide-fixture"
echo "fixture content" > "${FIXTURE}/README.md"
git -C "${FIXTURE}" add README.md
git -C "${FIXTURE}" commit -q -m "fixture: initial commit"
# A credential-requiring HTTPS remote — private-looking, cannot be pushed to
# anonymously. Never actually reachable/pushed; the push probe must fail on
# the LOCAL credential layer before any real network round-trip completes.
git -C "${FIXTURE}" remote add origin "https://github.com/jsquirrelz/tide-fixture-private-do-not-use.git"

fail=0

echo "=== D-09b adversarial behavioral probes against ${IMG} ==="
echo "NOTE: each probe overrides the image ENTRYPOINT to drive git/sh directly"
echo "against the container's mount + credential layer — deliberately BYPASSING"
echo "the verifier's own tool allowlist (git_read/run_gate_command). Prompt"
echo "refusal is explicitly NOT the mechanism under test here (EVAL-01)."
echo

# ── Probe (a): git commit against the :ro mount -> mount (EROFS) layer ──────
echo "--- Probe (a): git commit --allow-empty against :ro mount ---"
OUT_A="$(docker run --rm --read-only \
  -v "${FIXTURE}:/workspace:ro" \
  --entrypoint git "${IMG}" \
  -C /workspace commit --allow-empty -m x 2>&1)" && RC_A=0 || RC_A=$?
echo "${OUT_A}"
if [ "${RC_A}" -eq 0 ]; then
  echo "FAIL (a): git commit succeeded against a read-only mount — the mount layer did not hold." >&2
  fail=1
elif ! printf '%s' "${OUT_A}" | grep -qiE 'read-only file system'; then
  echo "FAIL (a): git commit failed (exit ${RC_A}) but NOT with a read-only-file-system error — evidence does not match the mount layer:" >&2
  echo "${OUT_A}" >&2
  fail=1
else
  echo "PASS (a): commit rejected at the mount layer (read-only file system), exit ${RC_A}."
fi
echo

# ── Probe (b): git push against a credential-requiring remote -> credential layer ─
echo "--- Probe (b): git push origin HEAD (no ambient credentials) ---"
OUT_B="$(docker run --rm --read-only \
  -v "${FIXTURE}:/workspace:ro" \
  -e GIT_TERMINAL_PROMPT=0 \
  --entrypoint git "${IMG}" \
  -C /workspace push origin HEAD 2>&1)" && RC_B=0 || RC_B=$?
echo "${OUT_B}"
if [ "${RC_B}" -eq 0 ]; then
  echo "FAIL (b): git push succeeded — no credential layer proven (or the fixture remote is reachable/writable, which it must not be)." >&2
  fail=1
elif ! printf '%s' "${OUT_B}" | grep -qiE 'could not read Username|Authentication failed|terminal prompts disabled|Repository not found'; then
  echo "FAIL (b): git push failed (exit ${RC_B}) but NOT with a recognized credential/auth failure — evidence does not match the credential layer:" >&2
  echo "${OUT_B}" >&2
  fail=1
else
  echo "PASS (b): push rejected at the credential layer (no ambient git credentials in the image), exit ${RC_B}."
fi
echo

# ── Probe (c): direct writes to the mount AND the container rootfs ──────────
echo "--- Probe (c): direct write attempts against /workspace (mount) and / (rootfs) ---"
OUT_C="$(docker run --rm --read-only \
  -v "${FIXTURE}:/workspace:ro" \
  --entrypoint sh "${IMG}" \
  -c 'touch /workspace/x; echo "workspace_rc=$?"; touch /x; echo "rootfs_rc=$?"' 2>&1)" && RC_C=0 || RC_C=$?
echo "${OUT_C}"
WORKSPACE_RC="$(printf '%s\n' "${OUT_C}" | sed -nE 's/^workspace_rc=([0-9]+)$/\1/p')"
ROOTFS_RC="$(printf '%s\n' "${OUT_C}" | sed -nE 's/^rootfs_rc=([0-9]+)$/\1/p')"
if [ "${WORKSPACE_RC:-1}" -eq 0 ] || [ "${ROOTFS_RC:-1}" -eq 0 ]; then
  echo "FAIL (c): a direct write succeeded (workspace_rc=${WORKSPACE_RC:-?}, rootfs_rc=${ROOTFS_RC:-?}) — --read-only + :ro did not hold everywhere." >&2
  fail=1
elif ! printf '%s' "${OUT_C}" | grep -qiE 'read-only file system'; then
  echo "FAIL (c): both writes failed but no 'read-only file system' evidence was found in the output — evidence does not match the rootfs/mount layer:" >&2
  echo "${OUT_C}" >&2
  fail=1
else
  echo "PASS (c): both the mount write and the rootfs write were rejected as read-only (workspace_rc=${WORKSPACE_RC}, rootfs_rc=${ROOTFS_RC})."
fi
echo

if [ "${fail}" -ne 0 ]; then
  echo "=== D-09b adversarial probes: FAILED — see FAIL lines above ===" >&2
  exit 1
fi

echo "=== D-09b adversarial probes: ALL PASSED (mount/credential/rootfs layers hold structurally) ==="
