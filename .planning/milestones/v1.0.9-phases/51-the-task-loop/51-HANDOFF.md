# Phase 51 "The Task Loop" — Session Handoff

**Written:** 2026-07-19 · **CLOSED 2026-07-20:** phase COMPLETE — live proof PASSED both gates (red REPAIRABLE→repair→exhaust→VerifyHalted, green APPROVED→Succeeded), ESC-04 kind spec 1/1 green live, verification re-run APPROVED 5/5 (`8f0accd2`), SECURITY.md SECURED 36/36 (`36e4c6c9`), state advanced to Phase 52. The "one ship-blocker" below turned out to be the first of FIVE stacked latent defects (entrypoint packaging → structured output unwired → recursion cap → template semantics → verdict-relay ship-blocker), all root-fixed in `29e31374` + `076c9637`; full account in `51-HUMAN-UAT.md`. Historical content below.

**Status (superseded):** code COMPLETE + Layer-A verified; live proof PARTIAL (core loop + fail-closed proven; happy path blocked by one ship-blocker bug)

---

## TL;DR

Phase 51 is **code-complete and fully verified at Layer A** (envtest/unit/Python/lint all green; 5/5 success criteria, 11/11 requirements, both safety properties airtight, **12 defects caught+fixed**). It is **not marked complete** and **has not advanced to Phase 52** — it's `human_needed` pending the live happy-path proof, which is blocked by exactly one newly-found bug.

**The single blocker to closing the phase:** the verifier image `ENTRYPOINT ["python","-m","verifier"]` fails at runtime with **`No module named verifier`** (a packaging bug latent since Phase 48). Every dispatched verifier fail-closes to BLOCKED, so the Task-loop happy path can't run in a real cluster. Tracked finding: `.planning/todos/pending/2026-07-19-verifier-image-entrypoint-no-module.md`.

## What's proven LIVE (kind-tide-test, real key)

A hand-driven contract-bearing Task (`gateCommand: false`, real `~/.tide/anthropic.key`) drove:
executor complete → **`Verifying`** sub-state → **independent `langgraph` verifier Job** (credproxy sidecar live, upstream `api.anthropic.com`, real key) → verifier wrote no verdict → **fail-closed BLOCKED → `ConditionVerifyHalt`** (`Phase=VerifyHalted`, `exitReason=escalated`). This proves the milestone's raison d'être — **no silent Complete on an unverifiable outcome** — plus the `VerifierImage`/`TIDE_VERIFIER_IMAGE` wiring, the `Verifying` sub-state, and the distinct `VerifyHalted` terminal (HI-01 fix), all on the real Phase-51 controller.

## THE fix to unblock the happy path (do this first)

1. **Root-cause `cmd/tide-langgraph-verifier/Dockerfile`:** `WORKDIR /app` + a multi-file `COPY …/verifier/*.py <dest>` + `ENTRYPOINT ["python","-m","verifier"]`. The runtime `No module named verifier` means the files did NOT land in `/app/verifier/` (likely the multi-source COPY flattened them into `/app/`). Fix the COPY dest to `/app/verifier/` (or set `PYTHONPATH`/`WORKDIR`).
2. **Add a build-time guard** (closes the tests-import-directly / dispatch-runs-entrypoint blind spot): in the image build or `make test-verifier-readonly`, assert `docker run --rm --entrypoint python <img> -c "import verifier; import verifier.__main__"` exits 0.
3. **Rebuild + reload:** `make docker-build-langgraph-verifier && kind load docker-image ghcr.io/jsquirrelz/tide-langgraph-verifier:test --name tide-test`.
4. **Re-run the live proof** (below) and confirm the red-gate Task now shows **REPAIRABLE → fresh attempt (attempt 2) → VerifyHalted** (not fail-closed-missing-verdict), then a green-gate Task (`gateCommand: "true"`, maxIterations 1) → **APPROVED → Succeeded** (this is the billable happy path).

## Live-proof recipe (reproducible)

Environment is already primed (see below), so re-running is: fix image → reload → reset the Task.

- **Cluster:** `kind-tide-test` (v1alpha3 CRDs applied from `config/crd/bases/`), context `kind-tide-test`.
- **Manager:** deployed as `controller:test` (Phase-51 code), patched with `TIDE_VERIFIER_IMAGE=ghcr.io/jsquirrelz/tide-langgraph-verifier:test` via `kubectl set env deploy/tide-controller-manager -c manager -n tide-system …`. Chart is 1.0.8 (does NOT wire the verifier env — that's Phase 53).
- **Images loaded into tide-test:** `controller:test`, `tide-langgraph-verifier:test`, `tide-credproxy:test`, `tide-stub-subagent:test` (+ reporter/push/import from earlier).
- **Fixture:** `.planning/phases/51-the-task-loop/live-proof-fixture.yaml` (secrets redacted — regenerate the two `data:` values per the inline comments: real key from `~/.tide/anthropic.key`, signing key copied from `tide-system/tide-signing-key`). It creates ns `tide-verify-proof` + `tide-subagent` SA + `tide-projects` PVC (1Gi RWO) + the two secrets + Project→Milestone→Phase→Plan→Task (red gate, `testMode: success`, `verification.phase: Locked`, maxIterations 2).
- **Reset a run:** `kubectl delete task proof-task-red -n tide-verify-proof; kubectl apply -f <fixture>`.
- **Watch:** `kubectl get task proof-task-red -n tide-verify-proof -o jsonpath='{.status.phase}/{.status.attempt} {.status.loopStatus}'` and `kubectl get jobs -n tide-verify-proof -l tideproject.k8s/role=verifier`.

## Environment cautions (constrained VM — the memory lesson bit us live)

- The Docker VM is ~59 GB and **hit 100% disk** with two kind clusters + fresh images (pods failed `No space left on device`). I deleted the stale `tide-phoenix-proof` cluster and pruned build cache → ~82%. **Run one heavy kind cluster at a time.**
- If a pod stalls in `PodInitializing`, check the sidecar image is loaded (`crictl images | grep credproxy`) — the credproxy/stub images had to be built+loaded (they weren't present).

## Session accomplishments (commits on `main`)

- discuss → plan (2 review iterations) → execute (8 plans, all Layer-A green) → code review (BLOCKER + HIGH + 3 MED + 4 LOW, **all root-fixed** `29c478c5`…`9ca2e39f`) → verify (5/5).
- Extra defects fixed: `VerifierImage` unwiring `450a20e4`, verifier `estimated-cost` restart gap `65747947`, dead-`commands` plan blocker.
- Kind-infra gaps closed `79d3dae5` (verifier image build/load in `test-int-kind-prep` + `TIDE_VERIFIER_IMAGE` in the harness).
- Two folded W-2 dispatch-gate todos closed; artifacts: `51-REVIEW.md` (`16da4903`), `51-VERIFICATION.md` (human_needed, `3936d7f1`), `51-HUMAN-UAT.md`.

## Remaining checklist to CLOSE Phase 51

- [ ] Fix verifier-image entrypoint bug + build-time import guard (the blocker above).
- [ ] Re-run live proof → confirm REPAIRABLE→VerifyHalted (red) AND APPROVED→Succeeded (green). Record in `51-HUMAN-UAT.md`.
- [ ] Optional: run the now-runnable ESC-04 kind spec (`make test-int-kind-prep` builds the verifier image; harness wires the env) — `go test ./test/integration/kind/... --ginkgo.focus 'Verifier concurrent-dispatch'`.
- [ ] `/gsd:execute-phase 51` re-verify → `passed`; then `phase.complete` + advance to Phase 52.
- [ ] `/gsd:secure-phase 51` (no SECURITY.md yet).
- [ ] Cleanup: `kubectl delete ns tide-verify-proof` when done.
