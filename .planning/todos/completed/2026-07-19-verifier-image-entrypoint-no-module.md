---
name: verifier-image-entrypoint-no-module
description: The shipped tide-langgraph-verifier image ENTRYPOINT `python -m verifier` fails at runtime with `No module named verifier` — every dispatched verifier fail-closes to BLOCKED, so the Task-loop happy path (REPAIRABLE/APPROVED) is dead in a real cluster
type: candidate-finding
severity: HIGH
captured: 2026-07-19
source: Phase 51 live billable Task-loop proof on kind-tide-test (hand-driven proof-task-red, red gate)
relates_to:
  - phase-48 (cmd/tide-langgraph-verifier/Dockerfile — EVAL-01/02 image build)
  - phase-51 (the Task loop — dispatchVerifier consumes this image)
---

# Verifier image ENTRYPOINT fails: `No module named verifier`

## Finding (verified live 2026-07-19 on kind-tide-test)

A hand-driven Phase-51 live proof (contract-bearing Task, locked `verification.gateCommand: "false"`, real `~/.tide/anthropic.key` provider secret) drove the FULL Phase-51 forward path correctly:

- executor (stub, testMode=success) → **belief-complete** → Task `Phase=Verifying` (Phase-51 forward half)
- an **independent `role=verifier` Job** dispatched against the locked contract (`Vendor=langgraph`, image `tide-langgraph-verifier:test`)
- the **credproxy sidecar started correctly** (minted self-signed cert, `upstreamURL=https://api.anthropic.com`, taskUID matched)

BUT the verifier's `subagent` main container **exited 1** with:

```
/usr/local/bin/python: No module named verifier
```

before making any Anthropic call (credproxy logged `shut down cleanly` with zero proxied requests). No verdict was written.

The controller then did **exactly the right thing** (fail-closed, EVAL-03): `VerifierVerdictMissing` → BLOCKED → `ConditionVerifyHalt` (Task `Phase=VerifyHalted`, `loopStatus.exitReason=escalated`). **So the milestone's raison d'être — a Task with an unverifiable outcome is NEVER stamped Complete — is proven live.** But the happy path (REPAIRABLE→repair→exhaust, APPROVED→Succeeded) can never run until this image bug is fixed.

## Root cause (hypothesis)

`cmd/tide-langgraph-verifier/Dockerfile`: `WORKDIR /app`, then a multi-file `COPY cmd/.../verifier/__init__.py __main__.py envelope.py ... <dest>`, then `ENTRYPOINT ["python","-m","verifier"]`. For `-m verifier` to resolve, the files must land in `/app/verifier/` (a package dir importable from WORKDIR). The runtime failure means they did NOT — likely the multi-source `COPY` destination flattened the files into `/app/` (no `verifier/` subdir) instead of `/app/verifier/`. Verify the COPY destination ends with `verifier/` and that `python -c "import verifier"` succeeds inside the built image.

## Why unit/envtest/pytest missed it

`make test-langgraph-verifier` (pytest, 76 passing) and Layer-A envtest import the `verifier` package **directly** with the source tree on `sys.path` — they never exercise the shipped image's `ENTRYPOINT` end-to-end. `make test-verifier-readonly` (D-09b) runs the image but asserts git-write/push fails at the mount/credential layer — it does not assert the `-m verifier` entrypoint successfully starts. So the entrypoint has been latently broken since Phase 48; the Phase-51 live dispatch is the first end-to-end exercise of it.

## Fix + guard

1. Fix the Dockerfile so `python -m verifier` resolves (COPY into `/app/verifier/`, or set `WORKDIR`/`PYTHONPATH` accordingly).
2. Add a build-time smoke check to the verifier image build (or `test-verifier-readonly`): `docker run --rm --entrypoint python <img> -c "import verifier; import verifier.__main__"` must exit 0. This closes the test-invokes-module-directly / dispatch-invokes-entrypoint blind spot.

## Verify line

`docker run --rm --entrypoint python ghcr.io/jsquirrelz/tide-langgraph-verifier:test -c "import verifier"` exits 0; then re-run the live proof and confirm the red-gate Task exhibits REPAIRABLE→fresh-attempt→VerifyHalted (not fail-closed-missing-verdict).
