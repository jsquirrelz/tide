---
phase: "06"
plan: "04"
subsystem: scripts-wiring
tags: [scripts, acceptance, dry-run, kind, images, makefile, cert-manager]
dependency_graph:
  requires: [CHART-01]
  provides: [IMG-LOAD-01, DRY-01, ACC-01, D-05, D-06]
  affects:
    - hack/scripts/load-images-if-needed.sh
    - hack/scripts/acceptance-v1.sh
    - hack/scripts/dry-run-v1.sh
    - Makefile
tech_stack:
  added: []
  patterns:
    - docker-manifest-inspect-probe
    - kind-load-dynamic-cluster-name
    - dind-heredoc-split
    - acceptance-sample-mode
decisions:
  - "IMG-LOAD-01: load-images-if-needed.sh calls kind load docker-image --name ${CLUSTER_NAME} directly — never delegates to test-int-kind-prep which hardcodes --name tide-test"
  - "DRY-01: cert-manager installed inside Pass 2 DinD heredoc before helm install; mirrors acceptance-v1.sh pattern"
  - "D-05: ACCEPTANCE_SAMPLE=small mode bypasses ANTHROPIC_API_KEY/GH_PAT gates and applies small/project.yaml"
  - "D-06: acceptance-verify.sh call wrapped in ACCEPTANCE_SAMPLE != small guard — large-sample git/budget/gitleaks assertions don't apply to the stub-subagent $0 path"
  - "DinD split: Pass 1 creates kind cluster + git clone; outer script loads images; Pass 2 runs cert-manager + helm + kubectl — works because DinD uses host Docker daemon via /var/run/docker.sock"
metrics:
  duration: "~12 min"
  completed: "2026-05-30"
  tasks_completed: 3
  files_modified: 4
---

# Phase 06 Plan 04: Script Wiring — load-images-if-needed.sh, acceptance-v1.sh ACCEPTANCE_SAMPLE=small, dry-run-v1.sh DRY-01 Summary

**One-liner:** Created shared `load-images-if-needed.sh` helper (docker manifest inspect probe + direct kind load with dynamic cluster name); wired it into acceptance-v1.sh replacing the broken ACCEPTANCE_LOAD_IMAGES delegation; added ACCEPTANCE_SAMPLE=small mode (D-05, D-06); split dry-run-v1.sh's DinD heredoc for cert-manager bring-up (DRY-01) and image-load (IMG-LOAD-01); added `acceptance-v1-smoke` and `docker-buildx-snapshot` Makefile targets.

## Tasks Completed

| Task | Name | Commit | Files |
|------|------|--------|-------|
| 1 | Create hack/scripts/load-images-if-needed.sh shared auto-detect helper | 34ceb39 | hack/scripts/load-images-if-needed.sh |
| 2 | Modify acceptance-v1.sh for $0 small-sample mode (D-05) and wire image-load helper | 7ea899c | hack/scripts/acceptance-v1.sh |
| 3 | Modify dry-run-v1.sh (DRY-01 + IMG-LOAD-01 DinD split) + add Makefile targets | 9f833cb | hack/scripts/dry-run-v1.sh, Makefile |

## What Changed

### Task 1: load-images-if-needed.sh

New shared helper at `hack/scripts/load-images-if-needed.sh` (executable, bash shebang, `set -euo pipefail`).

- Usage: `bash load-images-if-needed.sh <cluster_name> <image_tag>`
- Positional args: `CLUSTER_NAME` (kind cluster name) and `IMAGE_TAG` (e.g. `1.0.0`)
- D-03 image inventory — 6 entries in parallel `IMAGES` / `DOCKERFILES` arrays:
  - `ghcr.io/jsquirrelz/tide-controller` / `./Dockerfile`
  - `ghcr.io/jsquirrelz/tide-dashboard` / `./Dockerfile.dashboard`
  - `ghcr.io/jsquirrelz/tide-stub-subagent` / `images/stub-subagent/Dockerfile`
  - `ghcr.io/jsquirrelz/tide-credproxy` / `images/credproxy/Dockerfile`
  - `ghcr.io/jsquirrelz/tide-push` / `images/tide-push/Dockerfile`
  - `ghcr.io/jsquirrelz/tide-claude-subagent` / `images/claude-subagent/Dockerfile`
- Probe: `docker manifest inspect "${img}" > /dev/null 2>&1`
  - exit 0 → "found in registry, skipping local build"
  - non-zero → `docker build -t "${img}" -f "${df}" "${REPO_ROOT}"` + `kind load docker-image "${img}" --name "${CLUSTER_NAME}"`
- Never calls `test-int-kind-prep` (Makefile target that hardcodes `--name tide-test`)

### Task 2: acceptance-v1.sh modifications

Four changes in order:

1. **ACCEPTANCE_SAMPLE variable + conditional env-gates:** `ACCEPTANCE_SAMPLE="${ACCEPTANCE_SAMPLE:-large}"` added near top. `ANTHROPIC_API_KEY` and `GH_PAT` fail-fast wrapped in `if [ "${ACCEPTANCE_SAMPLE}" != "small" ]; then ... fi`.

2. **Conditional PROJECT_NAME / PROJECT_NAMESPACE:** `if [ "${ACCEPTANCE_SAMPLE}" = "small" ]; then PROJECT_NAME=small-project / PROJECT_NAMESPACE=tide-sample-small; else PROJECT_NAME=large-project / PROJECT_NAMESPACE=tide-sample-large; fi`

3. **Replaced broken ACCEPTANCE_LOAD_IMAGES block (lines 55-59):** The `if [ -n "${ACCEPTANCE_LOAD_IMAGES:-}" ]; then KIND_CLUSTER="${CLUSTER_NAME}" make -C "${REPO_ROOT}" test-int-kind-prep` block removed. Replaced with unconditional call: `bash "${REPO_ROOT}/hack/scripts/load-images-if-needed.sh" "${CLUSTER_NAME}" "${IMAGE_TAG}"` (IMAGE_TAG=1.0.0, matching chart appVersion after CHART-01).

4. **Conditional project-apply + acceptance-verify.sh guard (D-06):** Large-sample namespace/Secret/apply/4h-wait moved into `else` branch. Small-sample path applies `examples/projects/small/project.yaml` and waits 10m. The `acceptance-verify.sh` call wrapped in `if [ "${ACCEPTANCE_SAMPLE}" != "small" ]; then ... fi` — the $0 path has no per-run branch, no budget spent, no gitleaks output.

### Task 3: dry-run-v1.sh DinD split + Makefile targets

**dry-run-v1.sh:**

Single docker run heredoc split into two separate `docker run` invocations:
- **Pass 1:** apt-get tools + kind v0.31.0 + helm v3.16.3 + kubectl v1.31.0 installs + `kind create cluster --name tide-dry-run` + `git clone ${DRY_RUN_REPO_URL} /workspace/tide`. Tools re-installed in Pass 2 (new container).
- **Outer script:** `bash "${REPO_ROOT}/hack/scripts/load-images-if-needed.sh" "tide-dry-run" "1.0.0"` (IMG-LOAD-01). Works because both passes use the host Docker daemon via `/var/run/docker.sock` — the kind cluster created in Pass 1 is visible to the outer script.
- **Pass 2:** DRY-01 cert-manager block (mirrors acceptance-v1.sh): `kubectl apply -f cert-manager.yaml` + rollout status loop for `cert-manager`, `cert-manager-cainjector`, `cert-manager-webhook`. Then: `helm install tide-crds` + `helm install tide` + `kubectl wait controller Available` + `kubectl apply small/project.yaml` + `kubectl wait Complete 10m`.
- `TIDE_CERT_MANAGER_VERSION` passed via `-e` flag on Pass 2 `docker run` for operator override. Default `v1.20.2` (Phase 02.2 pin, K8s 1.33-compatible).
- `EXIT_CODE=${PIPESTATUS[0]}` captures Pass 2 exit; rest of the outer script (render-dry-run-report, ELAPSED check) unchanged.

**Makefile:**

- `IMAGE_TAG ?= 1.0.0` variable added near `PLATFORMS` (docker target section)
- `.PHONY: acceptance-v1-smoke` target added after `acceptance-v1`: runs `ACCEPTANCE_SAMPLE=small hack/scripts/acceptance-v1.sh` — no API key required, $0 LLM cost
- `.PHONY: docker-buildx-snapshot` target added after `docker-buildx`: local multi-arch build of all 6 component images (linux/amd64,linux/arm64) at `IMAGE_TAG` (default 1.0.0), no push

## Verification Results

All plan `must_haves.truths` confirmed:

| Check | Result |
|-------|--------|
| `bash -n load-images-if-needed.sh` exits 0 | PASS |
| `grep -cE 'kind load docker-image.*--name.*CLUSTER_NAME' load-images-if-needed.sh` = 1 | PASS |
| `grep -c 'test-int-kind-prep' load-images-if-needed.sh` = 0 | PASS |
| `grep -cE 'docker manifest inspect' load-images-if-needed.sh` = 1 (count=2, both hits are the probe) | PASS |
| `test -x load-images-if-needed.sh` exits 0 | PASS |
| `bash -n acceptance-v1.sh` exits 0 | PASS |
| `grep -c 'load-images-if-needed.sh' acceptance-v1.sh` = 1 | PASS |
| `grep -c 'test-int-kind-prep' acceptance-v1.sh` = 0 | PASS |
| `grep -cE 'ACCEPTANCE_SAMPLE' acceptance-v1.sh` >= 4 (count=9) | PASS |
| `grep -c 'small-project' acceptance-v1.sh` >= 1 | PASS |
| `grep -cE 'examples/projects/small/project.yaml' acceptance-v1.sh` >= 1 (count=3) | PASS |
| `grep -cE 'cert-manager' acceptance-v1.sh` >= 3 (count=9; existing block preserved) | PASS |
| `bash -n dry-run-v1.sh` exits 0 | PASS |
| `grep -cE 'cert-manager' dry-run-v1.sh` >= 2 (count=12) | PASS |
| `grep -cE 'load-images-if-needed' dry-run-v1.sh` = 1 (count=4, all in outer section) | PASS |
| `grep -cE 'TIDE_CERT_MANAGER_VERSION' dry-run-v1.sh` >= 1 (count=3) | PASS |
| `grep -cE '^.PHONY: acceptance-v1-smoke' Makefile` = 1 | PASS |
| `grep -cE '^.PHONY: docker-buildx-snapshot' Makefile` = 1 | PASS |
| `grep -cE 'ACCEPTANCE_SAMPLE=small' Makefile` = 1 | PASS |

## Deviations from Plan

None - plan executed exactly as written. The DinD split required tools to be re-installed in Pass 2 (standard for separate `docker run --rm` invocations with ephemeral containers), which was anticipated by the plan's implementation guidance.

## Known Stubs

None. All scripts are fully wired. The `load-images-if-needed.sh` script will build and load images if they are absent from the registry — the `docker manifest inspect` probe handles both the pre-publish (all 6 images built locally) and post-publish (all 6 images pulled from registry) scenarios correctly.

## Threat Flags

None beyond those in the plan's threat model (T-06-04-01 through T-06-04-03 accepted).

## Self-Check: PASSED

- `hack/scripts/load-images-if-needed.sh` exists, is executable, has dynamic --name pattern
- `hack/scripts/acceptance-v1.sh` modified with ACCEPTANCE_SAMPLE=small mode
- `hack/scripts/dry-run-v1.sh` modified with DinD split + cert-manager + image-load
- `Makefile` has acceptance-v1-smoke and docker-buildx-snapshot .PHONY targets
- Commits 34ceb39, 7ea899c, 9f833cb confirmed in git log
