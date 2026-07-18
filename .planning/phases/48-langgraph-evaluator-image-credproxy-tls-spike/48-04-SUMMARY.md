---
phase: 48-langgraph-evaluator-image-credproxy-tls-spike
plan: 04
subsystem: infra
tags: [docker, langgraph, python, langchain-anthropic, ci, read-only-enforcement, adversarial-test]

# Dependency graph
requires:
  - phase: 48-01
    provides: cmd/tide-langgraph-verifier/ Python package (envelope.py, tools.py, agent.py, __main__.py), hash-locked requirements.txt, make verify-langgraph-pins
  - phase: 48-03
    provides: git_read/run_gate_command tool allowlist (the layer this plan's D-09b test deliberately bypasses to prove the layer beneath it)
provides:
  - ghcr.io/jsquirrelz/tide-langgraph-verifier image, digest-pinned single-stage Dockerfile, --require-hashes enforced install
  - docker-buildx-snapshot (8th image) + docker-build-langgraph-verifier convenience target
  - release.yaml build-images matrix entry (tide-langgraph-verifier, not yet chart-referenced — Phase 53)
  - hack/scripts/test-verifier-readonly.sh — D-09b adversarial behavioral proof (commit/push/write all fail at mount/credential/rootfs layer)
  - ci.yaml langgraph-verifier job (uv-provisioned Python suite + image build + behavioral test)
affects: [48-05, 51-task-reconciler-dispatch]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Single-stage Python Dockerfile mirroring images/claude-subagent/Dockerfile's digest-pin + git safe.directory + USER 1000 discipline, minus the Go build stage"
    - "Two independent supply-chain enforcement layers: make verify-langgraph-pins greps the hand-maintained requirements.in; pip install --require-hashes enforces the machine-generated requirements.txt at build time"
    - "Adversarial behavioral test via --entrypoint override + --read-only + :ro bind mount, deliberately bypassing the tool-layer allowlist to prove the mount/credential layer holds independently"

key-files:
  created:
    - cmd/tide-langgraph-verifier/Dockerfile
    - hack/scripts/test-verifier-readonly.sh
  modified:
    - Makefile
    - .github/workflows/release.yaml
    - .github/workflows/ci.yaml

key-decisions:
  - "Re-resolved python:3.13-slim-bookworm@sha256:9d7f287598e1a5a978c015ee176d8216435aaf335ed69ac3c38dd1bbb10e8d64 live via docker pull — identical to the RESEARCH.md-cited digest, zero drift"
  - "Scoped the Dockerfile COPY to five explicit verifier/*.py files (not a blanket verifier/ COPY) so requirements-dev.txt/pyproject.toml/verifier/tests/ never enter the shipped image, despite the .dockerignore re-include admitting the whole source tree to the build context"
  - "Adversarial script pre-materializes the fixture worktree (git init + commit + a credential-requiring HTTPS remote) entirely outside any container, before the first docker run — the container performs zero worktree creation itself (Pitfall D)"
  - "New ci.yaml langgraph-verifier job provisions Python via astral-sh/setup-uv (no separate actions/setup-python step) — mirrors make test-langgraph-verifier's local dev recipe exactly"

patterns-established:
  - "Single-stage Dockerfile pattern for pure-Python TIDE component images (as opposed to claude-subagent's Go-build + Node-runtime two-stage pattern)"

requirements-completed: [EVAL-01, EVAL-02]

# Metrics
duration: 45min
completed: 2026-07-18
---

# Phase 48 Plan 04: LangGraph Verifier Image + D-09b Adversarial Read-Only Proof Summary

**Built and hash-verified the `ghcr.io/jsquirrelz/tide-langgraph-verifier` image, then proved EVAL-01's read-only contract behaviorally: a real `docker run --read-only` container with an entrypoint override attempts `git commit`/`git push`/direct writes and fails at the mount (EROFS), credential (no ambient git creds), and rootfs (EROFS) layers respectively — not by prompt refusal.**

## Performance

- **Duration:** ~45 min
- **Completed:** 2026-07-18T18:51:58Z
- **Tasks:** 2/2 completed
- **Files modified:** 5 (1 new Dockerfile, 1 new test script, 3 modified: Makefile, release.yaml, ci.yaml)

## Accomplishments
- `cmd/tide-langgraph-verifier/Dockerfile`: single-stage, digest-pinned `python:3.13-slim-bookworm`, `pip install --require-hashes` as the sole install path, `USER 1000`, `ENV SSL_CERT_FILE=/etc/tide/proxy/ca.crt` — builds clean from repo root, zero `openinference`/`opentelemetry` packages present, `import verifier` resolves.
- `hack/scripts/test-verifier-readonly.sh` (+ `make test-verifier-readonly`): the D-09b adversarial structural proof — three probes (commit, push, direct write) all fail with the correct evidence class (`read-only file system`, `terminal prompts disabled`), independent of and deliberately bypassing 48-03's tool-layer allowlist.
- Image joins the Makefile `docker-buildx-snapshot` (now 8 images) and the release.yaml `build-images` matrix; `verify-chart-images-published` confirmed still green (matrix-only addition, not chart-referenced yet).
- `ci.yaml` gained a `langgraph-verifier` job wiring the Python pytest suite (deferred from 48-01), the image build, and the behavioral test — all three run against the real Docker daemon and real `uv`-provisioned Python 3.13.

## Task Commits

Each task was committed atomically:

1. **Task 1: Dockerfile + build wiring (snapshot target, release matrix)** - `a171127` (feat)
2. **Task 2: D-09b adversarial behavioral test — make test-verifier-readonly** - `2c495e3` (test)

**Plan metadata:** (this commit, docs: complete plan)

## Files Created/Modified
- `cmd/tide-langgraph-verifier/Dockerfile` - single-stage build: digest-pinned base, hash-enforced pip install, scoped COPY (runtime source only), USER 1000, SSL_CERT_FILE env
- `hack/scripts/test-verifier-readonly.sh` - pre-materializes a fixture git worktree, runs three adversarial `--entrypoint`-overridden probes against a `--read-only` + `:ro`-mounted container, asserts failure evidence for each layer
- `Makefile` - `docker-buildx-snapshot` gains the 8th image; new `docker-build-langgraph-verifier` and `test-verifier-readonly` targets
- `.github/workflows/release.yaml` - `build-images` matrix gains the `tide-langgraph-verifier` component/dockerfile entry
- `.github/workflows/ci.yaml` - new `langgraph-verifier` job (uv setup, `make test-langgraph-verifier`, `make docker-build-langgraph-verifier`, `make test-verifier-readonly`)

## Decisions Made
- Re-verified the base image digest live at execution time per D-10 discipline (`docker pull python:3.13-slim-bookworm` resolved to the exact same sha256 RESEARCH.md cited — no drift, pinned as-is).
- Used explicit per-file `COPY` (five `verifier/*.py` files) rather than a directory `COPY verifier/`, since the `.dockerignore` re-include (`!cmd/tide-langgraph-verifier/**`) admits the whole source tree — including `verifier/tests/` and the dev lockfile — to the build context; the scoped COPY is what actually keeps them out of the image, not the dockerignore.
- `make test-verifier-readonly` auto-builds the image (`docker image inspect` check) if `IMG` isn't already present, so the target is runnable standalone in addition to composing with `docker-build-langgraph-verifier`.
- CI provisions Python exclusively through `astral-sh/setup-uv@v5` (no `actions/setup-python` step) since `uv venv --python 3.13` downloads its own interpreter — this exactly mirrors the local `make test-langgraph-verifier` recipe, avoiding two separate Python-provisioning mechanisms in CI vs. local dev.

## Deviations from Plan

None - plan executed exactly as written. Both tasks' `<action>` and `<acceptance_criteria>` were followed literally; no Rule 1-4 triggers encountered (no bugs found, no missing critical functionality beyond what the plan specified, no blocking issues, no architectural changes needed).

## Issues Encountered

None. The base image digest re-verification (a required pre-check per the critical_reminders) came back identical to the RESEARCH.md citation on the first `docker pull`, so no digest update was needed. Both Docker-dependent verification commands (`make docker-build-langgraph-verifier`, `make test-verifier-readonly`) ran against the local Docker daemon successfully on the first attempt.

## User Setup Required

None - no external service configuration required. (The image is publishable via the release matrix but not yet chart-referenced; no cluster-side action is needed until Phase 53.)

## Next Phase Readiness

- The `ghcr.io/jsquirrelz/tide-langgraph-verifier` image exists under its D-05 name, is release-matrix-wired, and is ready for plan 48-05's live credproxy-TLS spike to build/run against.
- Phase 48 success criterion 3 (adversarial structural proof) is now fully discharged: 48-02's static D-09a jobspec assertions + this plan's D-09b behavioral proof are both green.
- Phase 48 success criterion 4 (pin gate) is fully closed: `make verify-langgraph-pins` (48-01) + `pip install --require-hashes` build enforcement (this plan) are both in place and CI-wired.
- No blockers for 48-05.

## Self-Check: PASSED

- FOUND: cmd/tide-langgraph-verifier/Dockerfile
- FOUND: hack/scripts/test-verifier-readonly.sh
- FOUND: commit a171127 (Task 1)
- FOUND: commit 2c495e3 (Task 2)

---
*Phase: 48-langgraph-evaluator-image-credproxy-tls-spike*
*Completed: 2026-07-18*
