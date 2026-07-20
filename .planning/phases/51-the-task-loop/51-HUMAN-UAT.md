---
status: partial
phase: 51-the-task-loop
source: [51-VERIFICATION.md, 51-08-PLAN.md]
started: 2026-07-19
updated: 2026-07-20
---

## Current Test

[test 1 PASSED live 2026-07-20; test 2 (kind concurrency spec) still pending]

## Tests

### 1. Live billable Task-loop proof (Plan 08 Task 2 — blocking checkpoint:human-verify)
expected: On a kind cluster with a real Anthropic key, a Task whose locked `gateCommand` fails is dispatched → the independent read-only `langgraph` verifier runs the gate out-of-band → verdict REPAIRABLE → a fresh, evidence-seeded attempt is minted → on `maxIterations` exhaustion the Task lands in `ConditionVerifyHalt` (distinct from `Failed`); then flipping the gate to pass → APPROVED → Succeeded. Verifier pod is read-only (no git-write creds); cost stays bounded by `LoopPolicy.BudgetCents`.
result: **PASSED** (2026-07-20, kind-tide-test, real key, claude-sonnet-4-6 verifier)

**RED gate** (`proof-task-red`, `gateCommand: "test -f VERIFIED.md"`, maxIterations 2) — observed live:
```
01:18:14 a1 executor Running
01:18:55 Verifying (a1) — independent role=verifier Job Running (credproxy sidecar, real API)
01:19:15 verdict REPAIRABLE → "Verifier found repairable findings; dispatching fresh
         quality-iteration attempt 2" → a2 executor Running
01:19:56 Verifying (a2) — verifier attempt 2 Running
01:20:17 VerifyHalted | exitReason=iterationsExhausted | iteration=2
         loopStatus.lastEvaluation.decision=REPAIRABLE
         Condition: VerifyIterationsExhausted "verification loop exhausted
         MaxIterations=2 without an APPROVED verdict"
```
**GREEN gate** (`proof-task-green`, `gateCommand: "test -f proof.go"`, maxIterations 1) — after `tide resume`:
```
01:21:10 a1 executor Running
01:21:32 Verifying (a1) — verifier Running (~60s real sonnet agent run)
01:22:37 Succeeded | exitReason=approved | lastEvaluation.decision=APPROVED
         Condition: Succeeded=True VerifierApproved
```
`tide resume` (HI-01 recovery) was exercised live repeatedly: it cleared the project-wide
`ConditionVerifyHalt` each cycle and reset the VerifyHalted task for re-dispatch.

**Five defects found and root-fixed to get here** (all latent because nothing had ever run
the shipped verify path end-to-end; envtest feeds fake readers/models):
1. Verifier image entrypoint dead — `verdict.py` missing from the Dockerfile COPY, no
   `PYTHONPATH` for the pod's `/workspace` workingDir, and an import-time `go.mod`
   repo-root walk that can never succeed in the image. Fixed + a build-time
   `RUN cd / && python -c "import verifier, verifier.__main__"` guard in the Dockerfile
   (fails every build site). The guard caught defect 3 on its first run.
2. `create_agent` never wired `response_format=ToolStrategy(GateDecision)` — every LLM
   verdict was prose, `classify_verdict` fail-closed it to BLOCKED, making
   APPROVED/REPAIRABLE structurally unreachable. Wired; `RECURSION_LIMIT` 10→50 (the
   shell-sized cap starved the real multi-action prompt).
3. (caught by the new guard) — see 1.
4. `task_verifier.tmpl`: verdict semantics undefined + "non-zero exit dominates whatever
   verdict you would otherwise write" made every model answer BLOCKED on a red gate
   ("blocked regardless of artifact quality" — sonnet, verbatim), foreclosing the repair
   loop; also referenced paths the two tools cannot reach (repo.git README, envelopes,
   children JSON), which investigation-spiraled haiku to the recursion limit. Rewritten
   (verdict definitions, honest tool contract, termination pressure); PromptTemplateVersion
   v3→v4.
5. **Ship-blocker**: the controller read the verifier verdict via
   `PodStatusEnvelopeReader.ReadOut`, which (a) lists pods by task-uid label alone — the
   executor and verifier pods share it, coin-flip — and (b) unmarshals the termination
   message as `EnvelopeOut`, while the verifier image writes the tiny `TerminationStub`
   (`gateDecision` enum, D-05a) — so `out.Verdict` was ALWAYS nil in a real cluster and
   every verify fail-closed as `VerifierVerdictMissing`. Fixed: role-aware
   `ReadVerifierOut` (verifier-role pods, highest attempt, stub→Verdict graft) + `ReadOut`
   now skips verifier pods; `handleVerifierCompletion` uses the new seam via type
   assertion (envtest fakes unchanged). 4 new unit tests; full `make test` green.

Fixture/recipe notes (all committed alongside):
- `live-proof-fixture.yaml`: model must be REAL (`claude-sonnet-4-6` — the verifier
  inherits the task-level model; "stub" 404s upstream; haiku spirals). Red gate must be
  SEMANTICALLY repairable (`test -f VERIFIED.md`, not `false` — an honest judge calls a
  hardcoded `false` BLOCKED, correctly).
- `live-proof-green-task.yaml`: the green half (`test -f proof.go`, maxIterations 1).
- `live-proof-worktree-provisioner.yaml`: seeds `/workspace/worktrees/<uid>` (the stub
  executor takes its gitless fallback with no cloned repo, so nothing else materializes
  the worktree the verifier needs). Apply within the stub's 30s repo-wait.
- Hand-built namespaces lack the chart's `tide-reporter` SA → reporter Jobs FailedCreate
  (pre-existing, chart-scope; harmless to the loop).
- After any VerifyHalt: `tide resume <project>` before re-applying a task — new dispatch
  is frozen project-wide while the halt condition is set (this is Phase 51 working as
  designed; it also re-arms a reset VerifyHalted task, so delete tasks you don't want
  re-run).
- Minor observation (not blocking): the a2 repair executor's JOB showed Failed while the
  loop correctly treated the executor as complete and verified it (Verifying|a2 reached,
  verdict consumed). Task deletion GC'd the evidence; watch for recurrence.

why manual: requires the operator's real Anthropic key (`~/.tide/anthropic.key`) + real billable spend; not auto-approvable.

### 2. Live kind concurrency spec (Plan 08 Task 1 — `make test-int`)
expected: `test/integration/kind/verifier_concurrency_test.go` runs live — 3 contract-bearing Tasks dispatch concurrent `role=verifier` Jobs that never exceed the sized `verifierInFlightCount` cap (2), drain to zero, and leave no Task stranded in `Verifying`. (Spec compiles/vets/lints clean; not yet run against a live cluster.)
result: [pending]
why manual: needs a running kind cluster (Layer B); deferred from Task 1 as part of the same operator session.

## Operator runbook (prerequisites now satisfied)
Both prerequisites the executor flagged are FIXED: `TaskReconcilerDeps.VerifierImage` is wired (`450a20e4`, `TIDE_VERIFIER_IMAGE` env), and the verifier Job now carries the `estimated-cost` label for restart-safe reservations (`65747947`). The full working recipe is now: build+load `tide-langgraph-verifier:test` and `controller:test`, apply `live-proof-fixture.yaml` (regenerate the two secret values per its comments), provision the worktree per `live-proof-worktree-provisioner.yaml`, watch the red loop; then `tide resume`, apply `live-proof-green-task.yaml`, provision its worktree, watch it Succeed.

## Summary

total: 2
passed: 1
issues: 0
pending: 1
skipped: 0
blocked: 0

## Gaps
