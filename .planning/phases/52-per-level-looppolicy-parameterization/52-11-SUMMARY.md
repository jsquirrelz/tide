---
phase: 52-per-level-looppolicy-parameterization
plan: 11
subsystem: test/integration/kind
tags: [kind, worktree-provisioning, ESC-01, live-proof, checkpoint]
key-files:
  created:
    - test/integration/kind/level_verify_worktree_test.go
  modified: []
metrics:
  tasks_total: 2
  tasks_complete: 1
  tasks_checkpoint: 1
status: checkpoint
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
