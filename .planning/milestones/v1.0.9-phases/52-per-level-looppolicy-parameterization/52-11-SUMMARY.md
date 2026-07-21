---
phase: 52-per-level-looppolicy-parameterization
plan: 11
subsystem: test/integration/kind
tags: [kind, worktree-provisioning, ESC-01, live-proof, checkpoint]
key-files:
  created:
    - test/integration/kind/level_verify_worktree_test.go
    - .planning/phases/52-per-level-looppolicy-parameterization/live-proof-bare-cascade.yaml
    - .planning/phases/52-per-level-looppolicy-parameterization/live-proof-bare-cascade-project.yaml
    - .planning/phases/52-per-level-looppolicy-parameterization/live-proof-plan-check.yaml
  modified:
    - internal/controller/reporter_jobspec.go
    - internal/controller/plan_controller.go
    - internal/controller/reporter_spawn_idempotency_test.go
    - internal/controller/plan_verify_dispatch_test.go
    - api/v1alpha3/task_types.go
metrics:
  tasks_total: 2
  tasks_complete: 2
  tasks_checkpoint: 0
status: complete
---

# Plan 52-11 Summary — Live Worktree Proof + Operator-Gated Billable Checkpoint

## Commits

| Task | Commit | Description |
|------|--------|-------------|
| Task 1 | `e52e37f6` | test(52-11): kind spec — level-verify worktree-checkout init container on a real PVC |
| Task 2 | — | **CHECKPOINT (blocking): operator-gated billable live-loop proof — NOT executed** |

## Task 1 — Non-billable kind worktree proof (COMPLETE)

Wrote `test/integration/kind/level_verify_worktree_test.go` (492 lines), a Layer B
kind integration spec proving 52-05's worktree-provisioning mechanism against a
**real PVC + bare repo** on the `tide-test` kind cluster — the one behavior envtest
is structurally blind to (52-RESEARCH Pitfall 2; 52-VALIDATION Manual-Only row).

**Deliberately non-billable, verified structurally:**
- Drives `podjob.BuildJobSpec` directly with `Kind=JobKindVerifier`, a synthetic
  never-persisted Phase `ParentObj`, `Level="phase"`, and the worktree-checkout
  fields — the exact composition `level_verify.go`'s dispatch uses, minus every
  credential-bearing field.
- `opts.Project` is left nil, so `BuildJobSpec`'s own credproxy gate
  (`opts.Project != nil && ...ProviderSecretRef != ""`) skips credproxy entirely:
  no `ANTHROPIC_API_KEY`, no provider secret, no sidecar.
- The main subagent container is overridden to `busybox:stable` running
  `echo; exit 0` — the assertion target is exclusively the `worktree-checkout`
  **init** container, never a verifier verdict.

**Assertions (all proven live):** the init container terminates exit 0; a
follow-up read-only inspection Job asserts `/workspace/worktrees/<uid>/` HEAD
equals the seeded run-branch tip SHA and `git branch --show-current` is empty
(detached — `AddReadOnlyWorktree`'s `git worktree add --detach` mints no branch).

**Verification (CLAUDE.md MAKE_EXIT + FAIL-grep discipline):**
- `make test-int` → **MAKE_EXIT=0**, **28/28 kind specs pass**, **zero `^--- FAIL` / `^FAIL\s` lines** (1772s).
- The new spec ran: `Level-verify worktree-checkout init container provisions a real worktree from a real PVC (ESC-01)` — log shows the init-container-exit-0 wait and the HEAD-SHA/detached assertion steps.
- `go vet ./test/integration/kind/` clean on main.

## Task 2 — Billable live-loop proof (CHECKPOINT — awaiting operator)

**Not executed.** This is a `checkpoint:human-verify gate=blocking` task requiring
the operator's explicit billable-spend authorization on the `kind-tide-test`
cluster with the real Anthropic key (`~/.tide/anthropic.key`) — the same class of
live gate Phase 51 closed with its 51-08 runbook (which surfaced five stacked
latent defects the green suites missed). Under `--auto`, billable API spend is
**not** auto-approved.

The full runbook is in `52-11-PLAN.md` (Task 2 how-to-verify). In brief, it drives:
1. **Plan-check loop:** a Project with a Locked `Verification.Plan` gate command +
   a deliberately weak first Plan attempt → observe Verifying →
   `tide-verifier-plan-*-1` → REPAIRABLE → child-Task deletion →
   `tide-plan-*-2` planner Job carrying the findings block → APPROVED or resolved
   escalation.
2. **Level-verify:** a phase-level contract → after children succeed, the
   `tide-verifier-phase-*-1` init container provisions the worktree, the gate
   command runs for real, a non-APPROVED verdict parks the phase at
   `AwaitingApproval`; `tide approve` resumes it to `Succeeded` with no second
   verifier Job.
3. **ESC-04 rails live:** `kubectl get jobs -l tideproject.k8s/role=verifier`
   counts stay ≤ the concurrency cap throughout.

**Resume signal:** the operator runs the billable proof and replies "approved"
with observed outcomes (or pastes failures), or replies "skip live proof" to
defer with the phase marked accordingly.

## Task 2 — Live billable proof (OPERATOR-APPROVED, IN PROGRESS — checkpointed)

Operator approved the billable run 2026-07-20. Stood up a fresh `tide-test` kind
cluster, loaded all 8 dev-head images, deployed the manager via helm (test-image
overrides + `TIDE_VERIFIER_IMAGE` patch), created the real-key + signing-key
secrets. **The live gate immediately earned its keep — it surfaced a real
SHIP-BLOCKER the green suites and Phase 51 both missed** (the 51-08 pattern):

### DEFECT-A (FIXED + committed `8e5f7a49`) — CEL immutability blocks every P/M/P contract
A Locked verification contract at Phase/Milestone/Project level (`maxIterations:0`)
could NEVER progress. The controller's full-object `Update()` round-trips the spec
through Go, where `maxIterations,omitempty` drops the value `0`, so the apiserver
saw `oldSelf.maxIterations=0` (present) vs `self` absent and the `VerificationSpec`
CEL `self == oldSelf` immutability rule failed *"verification is immutable once
Locked"* on **every reconcile** — freezing the Project before it could even set
its run branch. This blocked the **entire per-level verification feature** at
exactly the levels Phase 52 adds. The Task level (`maxIterations>=1`) was
unaffected — which is precisely why Phase 51's Task-loop proof passed and envtest
missed it (the fake client does not enforce CEL; no test ran a real apiserver
`Update()` on a Locked-contract Project).
**Fix:** `0` is a MEANINGFUL value here, not "unset" — dropped `omitempty` + added
`+kubebuilder:default=0` so the apiserver stores a present `0` even when omitted at
apply time; stored and round-tripped forms then match and the rule holds. No
Go-logic change (the resolver already reads the int32 `0` and applies its per-level
default/clamp). Confirmed live: the exact reproduction (`kubectl patch` removing
`maxIterations` from a Locked contract) now succeeds, and the fixture Project
advances to `Running` with a run branch set.

### Checkpoint status (both loops NOT yet driven to a billable verifier dispatch)
After DEFECT-A's fix the Project runs, but driving the full hierarchy succession to
the phase-verify (and plan-check) dispatch hit **fixture-completeness friction**,
not further product defects: (1) the reporter Job needs a per-namespace
`tide-reporter` ServiceAccount+RBAC (the chart provisions it only for
chart-configured namespaces; the Phase-51 Task proof never needed the reporter
because the Task ran without hierarchy succession) — created manually; (2) the
direct-applied hierarchy (Project→…→Task, adopted via ownerRefs) does not cleanly
drive Plan→Phase→Milestone→Project succession the way a planner-authored tree does,
so the Phase never reaches its pre-Succeeded verify seam. Resolving this needs more
fixture bring-up than one session's budget allowed.

**Spend so far: ~1 cent** (only stub planners ran; NO verifier Job has billed yet —
the real-model verifier dispatch is the still-unreached step).

**Remaining to close Task 2:** either (a) drive the hierarchy via the stub planners
end-to-end (needs the stub planner to author a minimal succeeding tree + the
reporter SA per namespace), or (b) add a fixture that forces the Phase to its
verify seam directly. Then observe: phase-verify `tide-verifier-phase-*-1` worktree
init container → gate → non-APPROVED → AwaitingApproval park → `tide approve` →
Succeeded; and the plan-check REPAIRABLE→re-plan loop. Cluster `tide-test` is left
UP (deployed dev-head + real-key secrets + DEFECT-A's CRD fix applied) for a
follow-up session to resume from.

## Task 2 — COMPLETE (resumed session, 2026-07-20/21): both loops proven live; TWO more shipped defects surfaced + root-fixed

The resume session shipped DEFECT-A's Go change into the pod (rebuilt
`controller:test`, kind-loaded, rolled out), then closed the previous session's
succession blocker and drove BOTH loops end-to-end with real Anthropic calls.
**The live gate caught two more real shipped defects the green suites missed
(the 51-08 pattern, now at 3 for this checkpoint alone), both root-fixed with
RED-first regression specs and re-proven live.**

### Succession blocker resolved (fixture, not product)
The direct-applied hierarchy stalls because succession is fed by the
planner-authored materialization path; and the first fixture's reporter RBAC
was created AFTER the project planner spawned its reporter (spawn-once marker
already stamped → never retried). The proven recipe is the bare-Project
cascade (`live-proof-bare-cascade*.yaml`, mirroring `testdata/bare-project.yaml`):
provision namespace + SAs + reporter RBAC + PVC (+ a prewarm pod — the
WaitForFirstConsumer PVC never binds otherwise; the ProjectReconciler requeues
but never mounts) + secrets FIRST, then apply ONLY a Project; stub planners
author Milestone→Phase→Plan→Task ($0), and the level contract resolves from
`Project.Spec.Verification.<level>`. Seed the bare repo (templated seed pod)
between `Status.Git.BranchName` minting and the verifier dispatch.

### Level-verify proof (phase level) — PASSED, both legs
- **Red leg (`tide-lv2`):** cascade → all children Succeeded → Phase
  `Verifying` ("All children succeeded; dispatching an independent verifier
  against the locked verification contract") → `tide-verifier-phase-<uid>-1`
  (init containers `envelope-writer`/`worktree-checkout`/`tide-credproxy` all
  exit 0; checkout log: `worktree-checkout: provisioned <uid> @
  tide/run-tide-lv2-project-1784596833 -> /workspace/worktrees/<uid>`) → real
  Sonnet call, gate `test -f VERIFIED.md` red → termination stub
  `{"gateDecision": "REPAIRABLE", "findingsCount": 1, "highSeverityCount": 1}`
  → `maxIterations:0` exhaustion → `loopStatus {exitReason: escalated,
  iteration: 1, lastEvaluation: REPAIRABLE}` → **AwaitingApproval park** →
  `tide approve` → **Succeeded with exactly one verifier Job**; Milestone
  Succeeded, Project Complete.
- **Green leg (`tide-lv3`):** seed includes VERIFIED.md → verifier stub
  `{"gateDecision": "APPROVED", "findingsCount": 0}` → `loopStatus
  {exitReason: approved}` → **Succeeded with no park**, Project Complete.

### DEFECT-B (FIXED + committed `1d09e049`) — re-plan loop dead-stall: attempt-blind reporter Job name
First plan-check drive (`tide-lv4`): REPAIRABLE → `dispatchPlanRepair` deleted
the rejected child + re-dispatched `tide-plan-<uid>-2` — and then the loop
froze in `Running` with zero errors. Root cause: the materialization reporter's
Job name was `tide-reporter-<planUID>` (attempt-blind); attempt 2's spawn found
attempt 1's completed reporter Job by name (still inside its 300s TTL — a stub
planner attempt takes ~20s), skipped the Create as T-09-13 idempotency, stamped
`PlanReporterSpawnedUID` (proven: the marker held job-2's UID while the only
reporter pod predated job-2's existence), and **the re-planned attempt's
children were never materialized**. Invisible to envtest: the plan inline arm
was the one spawn site without its own idempotency spec. Fix:
`ReporterJobNameFor(parentUID, attempt)` — single name source for
BuildReporterJob + the controller Get; attempts >1 get `-<attempt>` (plan is
the only re-dispatching level; every other level byte-identical). RED-first
plan-level spec added to `reporter_spawn_idempotency_test.go`.

### DEFECT-C (FIXED + committed `5d2c299f`) — operator approval of an exhausted plan-check loop silently swallowed
Second plan-check drive (`tide-lv5`, fixed manager): the full loop ran
(attempt-1 REPAIRABLE → repair → **`tide-reporter-<uid>-2` spawned, fresh child
materialized** → attempt-2 Verifying → REPAIRABLE → **D-05 stall detection
fired live**: "re-plan loop stalled: the new plan-check verdict did not
strictly improve on the prior iteration" → AwaitingApproval park). But `tide
approve` bounced: the WaveOrLevelPaused transition advanced 01:44:58→01:45:26
with the same message — the resume returned the Plan to Running, BOTH Verifying
entry sites re-fired against the SAME consumed verdict, and the Plan re-parked
~30s later, an endless approve→re-verify→re-park cycle. The P/M/P levels are
immune (level_verify.go's T-52-25 ExitReason-convergence guard); the plan
controller had no analog. Fix: `planVerifyResolvedByOperator`
(ExitReason==escalated AND the ApprovedByUser/ResumedByUser record) guards both
entry sites — deliberately narrower than T-52-25 so a descent-gate approval
still verifies and a mid-loop repair still re-enters. RED-first spec (f) added
to `plan_verify_dispatch_test.go`.

### Plan-check proof — PASSED end-to-end on the fixed manager (`tide-lv5`)
Verifying → `tide-verifier-plan-<uid>-1` (child Task held un-dispatched, D-03)
→ REPAIRABLE (1 finding, 1 high-severity) → child-Task deletion + wave prune
(manager log: "deleted rejected plan-check attempt's child tasks ahead of
re-plan") → `tide-plan-<uid>-2` findings-seeded planner → attempt-suffixed
reporter → fresh child → second Verifying → `tide-verifier-plan-<uid>-2` →
REPAIRABLE → stall exhaustion → AwaitingApproval → `tide approve` → resume
STUCK (no re-verify/re-park across the entire post-approve window) → held Task
dispatched → **Task/Plan/Phase/Milestone Succeeded, Project Complete; exactly
2 verifier Jobs ever** (no third dispatch after operator resolution). This is
the UAT's "resolved escalation" arm, with the re-plan machinery (findings
annotation → `EnvelopeIn.RepairFindings`), stall detection, and both defects'
fixes all observed live.

### ESC-04 rails
`kubectl get jobs --all-namespaces -l tideproject.k8s/role=verifier` peaked at
2 cluster-wide (one Complete + one Running); concurrently-Running never
exceeded 1; cap (2) never exceeded. (The adversarial 3-vs-cap-2 case is pinned
live by `verifier_concurrency_test.go`, green in this phase's `make test-int`
28/28 run.)

### Spend
Five real Sonnet (claude-sonnet-4-6) verifier calls total (lv2 red, lv3 green,
lv4 attempt-1, lv5 attempts 1+2); every fixture project's rolled-up
`costSpentCents` reads 1. Aggregate for the entire proof ≈ well under $0.25,
against $5-per-project `absoluteCapCents: 500` backstops. Cumulative with the
prior session: still under $0.30.

### Cluster state at close
`tide-test` left UP. Fixture namespaces `tide-lv2/3/5` hold the completed
proof hierarchies (evidence); `tide-lv-proof` (superseded first fixture, Project
parked mid-planner) and `tide-lv4` (pre-DEFECT-B-fix stall evidence) are
inert — no billable path remains in any of them (planners are stub; no verifier
can dispatch). Manager pod runs `controller:test` at `5d2c299f` (DEFECT-A+B+C
all compiled in).

## Deviations

- Task 1's spec was authored + proven live by the executor, but the executor
  returned before committing it (it left `make test-int` running detached and
  idled out). The orchestrator waited for `MAKE_EXIT=0` + zero FAIL lines, then
  committed the (unchanged) spec to main directly — no worktree branch commit
  existed to merge.

## Self-Check

- [x] Task 1 kind spec written, non-billable (no ANTHROPIC key), asserts init-container provisioning
- [x] `make test-int` MAKE_EXIT=0 + zero FAIL lines (both gates per CLAUDE.md)
- [x] Task 2 NOT executed — no billable spend; surfaced as a blocking operator checkpoint
- [x] SUMMARY records Task 1 complete + Task 2 pending
