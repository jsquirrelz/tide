# Phase 34: Run Integrity — Integration-Miss Gate + lastPushedSHA - Context

**Gathered:** 2026-07-03
**Status:** Ready for planning

<domain>
## Phase Boundary

Every Succeeded task's worktree branch is provably merged into the run branch — the wave-parallel integration step cannot silently drop a merge, boundary push is gated on completeness verified from git, and a run can no longer stamp `Complete` while a declared deliverable is missing from the pushed branch. Four deliverables (INTEG-01..05): close the final-wave integration skip, serialize + idempotent run-branch merges while tasks stay parallel, gate every boundary push on `git merge-base --is-ancestor` completeness with a sticky `integration-incomplete` condition on a miss, and stamp `status.git.lastPushedSHA` from the push envelope's `HeadSHA` — locked by a kind-suite regression test reproducing the 2-parallel-task final-wave miss.

The mechanical, no-LLM degenerate case of the verify-stage seed. No LLM verify subagents in this phase (own milestone, STAGE-01).

</domain>

<decisions>
## Implementation Decisions

### Integration topology
- **D-01 — Both layers:** Per-wave integration Jobs extend to every wave *including the final one* (closing the `k < len(layers)-1` skip in `plan_controller.go`), AND the boundary-push Job keeps carrying the cumulative all-Succeeded-branch set as an idempotent re-merge. Structural fix plus self-healing defense in depth; matches INTEG-02's "cumulative Succeeded-branch set."
- **D-02 — Single-flight gate:** Control-plane serialization of run-branch merge writers: before creating any Job that merges into the run branch (wave-integration or boundary-push), the controller lists in-flight integration/push Jobs project-wide; if one is active, requeue. Mirrors the v1.0.6 D3 count-gate pattern (live `client.List` before dispatch). `flock(2)` inside the Job stays belt-and-braces only (locked constraint).
- **D-03 — Every trigger carries the set:** The cumulative Succeeded-branch set is computed inside the shared `triggerBoundaryPush` for ALL four levels — whichever level's trigger wins the deterministic `tide-push-<project.UID>` create race integrates identically. The nil-branches race (today only the plan-level trigger passes `IntegrateTaskBranches`) becomes harmless.
- **D-04 — Bounded retry for integration Jobs:** Wave-integration Job failures ride a bounded-retry state machine (attempts counter, capped backoff, terminal after cap) mirroring the boundary-push pattern — replacing today's first-failure → Plan Failed semantics. Transient infra failures (pod eviction, PVC contention) self-heal; merge conflicts are exempt (D-08).

### Gate mechanics
- **D-05 — Every push gates:** All four levels' boundary pushes (plan/phase/milestone/project) run the completeness check before pushing. No unverified push class; the project-boundary push preceding `Complete` is covered.
- **D-06 — Verify inside tide-push:** One Job does integrate → verify → push. After merging, before pushing, tide-push runs `merge-base --is-ancestor` per expected branch; on a miss it exits nonzero with a typed envelope reason + the missing branch list. Verify and push are atomic under the same flock; the single-flight gate (D-02) serializes writers. No separate verify Job.
- **D-07 — Project-wide expected set:** The controller lists every Succeeded Task CR owned by the project at dispatch time and passes their branch names to the Job. Tasks succeeding after dispatch are covered by their own wave Job and the next push. Planner note: mind arg-size limits if a project accumulates hundreds of tasks (file/ConfigMap handoff is an acceptable fallback).
- **Clarification (recorded, not asked):** INTEG-03 pins `merge-base --is-ancestor` as the predicate — an empty-diff Succeeded task passes naturally (its branch tip is already an ancestor). SC-1's "merge commit reachable" phrasing describes the normal `--no-ff` case; no per-task merge-commit bookkeeping.

### Miss remediation
- **D-08 — Retry then stick:** A gate miss fails the push Job with a typed reason; the existing #13b bounded-retry state machine re-dispatches (re-integrating and re-verifying). Only after the attempt cap does the sticky `integration-incomplete` condition park the push.
- **D-09 — Classify conflicts, don't retry:** tide-push distinguishes a genuine merge conflict from transient failure in the envelope reason. Conflicts skip remaining retries and park immediately with a distinct condition reason (content problem, human needed). Echoes Phase 35's BASE-02 classify-don't-retry principle.
- **D-10 — Same-wave conflict fails the Plan:** A wave-integration merge conflict marks the Plan Failed with a condition naming both branches — conflicting parallel tasks weren't actually independent, so the plan is defective ("cycles are bugs" philosophy). Recovery via the sanctioned `tide resume --retry-failed` after replanning. No conflict-resolution machinery in v1; no manual-merge-on-PVC protocol.
- **Carried forward (#13b, locked):** `Complete` still stamps as the control-plane rollup; the PUSH is what parks. The gate blocks the run branch from shipping, not the status rollup.

### Operator surface
- **D-11 — Project + Plan condition split:** `integration-incomplete` lives on the Project (beside `BoundaryPushed` — it's a push-gate outcome and the Project is what operators watch); merge-conflict failure conditions live on the failing Plan (where the defect is).
- **D-12 — Named detail + metric:** The `integration-incomplete` condition message names each missing task + branch (truncated past a bound, e.g. first 10 + total count), and a result-labeled integration-outcome counter lands beside the existing `PushJobsTotal`. Diagnosable from `kubectl describe project` alone — pod logs get GC'd (COST-02 lesson).
- **D-13 — Recovery via `tide resume`:** Extend `tide resume` to reset the boundary-push attempt counter, implemented via the existing annotation mechanism (PushLeaseFailed bypass precedent). Preserves the D-07/v1.0.1 single-recovery-verb principle; `kubectl annotate` remains the escape hatch. The condition auto-clears whenever a later verify+push succeeds.
- **D-14 — lastPushedSHA (INTEG-04, mechanical):** The push envelope's `HeadSHA` is read in the push-Job success arm and patched to `Status.Git.LastPushedSHA`, arming the force-with-lease fence. Scout confirmed no assignment exists anywhere in `internal/` today — the doc comment at `project_controller.go:472` promises it but the wiring never landed.

### Claude's Discretion
- flock lockfile placement/naming inside the Job, branch-list handoff format (args vs file vs ConfigMap, per D-07 note), retry cap sizing, condition/reason naming, exact metric labels, and the kind-suite repro harness shape (deterministic single-wave degenerate case + 2-parallel-task final-wave case per INTEG-05) — planner/executor decide within the decisions above.

### Folded Todos
- **Wave-parallel task integrate step skipped; Complete does not gate on unintegrated worktree branches** (`.planning/todos/pending/2026-07-03-wave-parallel-integration-miss.md`, `resolves_phase: 34`) — the phase's originating evidence. First external-repo run (2026-07-03): wave 1 ran 2 parallel tasks; both Succeeded with authored commits, but only one integrate commit landed on the run branch — task e088c86c's branch (`tide/wt-e088c86c-…`, commit d7d2234, +60-line test file) was never merged. Boundary push shipped the run branch missing a declared `filesTouched` deliverable; Project stamped Complete; `status.git.lastPushedSHA` never set despite BoundaryPushed=True. Evidence lives on the minikube `tide-projects` PVC (perishable — export before namespace/cluster cleanup) and the pushed run branch. User condition on folding verified: no private company/PII data beyond what this repo already commits.

</decisions>

<canonical_refs>
## Canonical References

**Downstream agents MUST read these before planning or implementing.**

### Planning artifacts
- `.planning/ROADMAP.md` — Phase 34 entry (goal, success criteria 1–5, dependency note: merge code must stabilize before Phase 36's signing touches the same commit sites)
- `.planning/REQUIREMENTS.md` — INTEG-01..05 (the binding requirement text, including the `plan_controller.go:1192` citation and the flock/cumulative-set language)
- `.planning/STATE.md` — "Accumulated Context" v1.0.7 binding constraints (gate-the-push-not-Complete, verdict-recomputed-from-git, flock-as-belt-and-braces, no-PVC-lockfile-protocols) + perishable-evidence warning
- `.planning/todos/pending/2026-07-03-wave-parallel-integration-miss.md` — folded todo; the run evidence and repro framing
- `.planning/seeds/verify-level-subagent.md` — the verify-stage seed whose mechanical degenerate case this phase ships (read for framing; do NOT implement the LLM stages)

### Code sites (the fix surface)
- `internal/controller/plan_controller.go` — wave-boundary Responsibilities A/B/C block (~line 1150–1200); the `for k := 0; k < len(layers)-1; k++` last-wave skip (INTEG-01)
- `internal/controller/boundary_push.go` — `triggerBoundaryPush` (shared 4-level trigger; nil-branches race per D-03) + `triggerWaveIntegrationJob` (per-wave Job dispatch)
- `internal/controller/push_helpers.go` — `PushOptions`, `buildPushJob`, `--integrate-task-branches` / `--last-pushed-sha` flag plumbing
- `internal/controller/project_controller.go` — `reconcilePhase3Lifecycle` (#13b boundary-push bounded-retry state machine, `dispatchBoundaryPush`, push envelope success arm where `LastPushedSHA` must be stamped per D-14)
- `pkg/git/integrate.go` — `IntegrateTaskBranches` (git-CLI `--no-ff` merges in the shared integration worktree; no locking today; "already checked out" tolerance)
- `pkg/dispatch/envelope.go` — `EnvelopeOut.Git.HeadSHA` / tiny-status flattening (the field D-14 reads)
- `cmd/tide-push/main.go` — the push Job binary where integrate → verify → push (D-06) and conflict classification (D-09) land

</canonical_refs>

<code_context>
## Existing Code Insights

### Reusable Assets
- **#13b bounded-retry state machine** (`project_controller.go`: `reconcileBoundaryPush` / `dispatchBoundaryPush`, `maxBoundaryPushAttempts`, capped backoff): D-04 and D-08 ride this pattern instead of inventing new retry machinery.
- **v1.0.6 D3 count-gate** (live `client.List` in-flight count before pool acquire): the proven template for D-02's single-flight integration gate.
- **PushLeaseFailed bypass-annotation pattern**: the mechanism under D-13's `tide resume` extension.
- **`tidemetrics.PushJobsTotal`**: the metric D-12's integration-outcome counter sits beside.

### Established Patterns
- Deterministic Job names as serialization keys (D-B5: `tide-push-<project.UID>`, `tide-push-wave-<plan.UID>-<k>`); AlreadyExists = idempotent success.
- Conditions + typed reasons for operator-visible failure (`BoundaryPushed`/`ReasonPushing`, `ReasonCycleDetected`); classify-don't-retry is already a milestone principle (BASE-02).
- Envelope-on-PVC as the Job→controller result channel; the manager cannot mount project PVCs, so all git operations run in Jobs.
- Bot identity via `TIDE_BOT_NAME`/`TIDE_BOT_EMAIL` env in `integrate.go` — Phase 36 makes this uniformly configurable; do not refactor identity here, just don't make it worse (Phase 36 lands on these stabilized sites).

### Integration Points
- Wave-boundary Responsibilities A/B/C in `plan_controller.go` — D-01's every-wave extension and D-04's bounded retry slot in here.
- `triggerBoundaryPush` — D-03's cumulative-set computation goes in the shared function, not per-level callers.
- tide-push `--mode=push` path — D-06 verify step and D-09 conflict classification are new phases of this binary's existing flow.
- Push-Job completion handling in `project_controller.go` — D-14's `LastPushedSHA` stamp and D-11's Project condition land in the success/failure arms.

</code_context>

<specifics>
## Specific Ideas

- The observed miss (run 2026-07-03) is the acceptance narrative: 2 parallel tasks in a final wave, both Succeeded, one branch never merged, push shipped, Complete stamped. INTEG-05's kind-suite test must reproduce exactly this shape RED against pre-fix code. The deterministic single-wave-plan case (a single-wave plan integrates *nothing* via wave Jobs today) is the cheapest RED repro of INTEG-01.
- Two distinct bug mechanisms to cover: the deterministic last-wave skip AND the nil-branches trigger race — the kind repro should not assume they're one bug.
- `status.git.lastPushedSHA` was observed unset while `BoundaryPushed=True (Pushed)` — verify the stamp lands in the same arm that sets that condition.

</specifics>

<deferred>
## Deferred Ideas

- **Dashboard display of `integration-incomplete` / `lastPushedSHA`** — belongs to Phase 37's project view (DASH-03); this phase only guarantees the condition/status fields exist and are named.
- **LLM verify-tier subagents** (plan-check + level-verify) — STAGE-01, own milestone; seed stays planted at `.planning/seeds/verify-level-subagent.md`.
- **Manual conflict-resolution protocol on the PVC** — explicitly rejected for v1 (D-10); revisit only if Plan-fails-on-conflict proves too blunt in practice.

### Reviewed Todos (not folded)
The todo matcher flagged 8 other pending todos; all map to later phases per REQUIREMENTS traceability and were not folded:
- `2026-07-03-git-baseref-run-branch.md` — Phase 35 (BASE-01..03)
- `2026-07-03-signed-commits-verified-badge.md` — Phase 36 (SIGN-01..04)
- `2026-07-03-dashboard-log-stream-drawer-empty.md`, `2026-07-03-dashboard-planning-dag-artifact-view.md` — Phase 37 (DASH-01..04)
- `2026-07-03-pricing-table-claude-5-family.md`, `2026-07-03-prometheus-setup-step-for-run-telemetry.md` — Phase 38 (COST/TELEM)
- `2026-07-03-project-level-subagent-override-slot.md` — deferred, own milestone (STAGE-02)
- `cache-f1-direct-sdk-cross-pod-caching.md` — deferred (CACHE-F1)

</deferred>

---

*Phase: 34-Run Integrity — Integration-Miss Gate + lastPushedSHA*
*Context gathered: 2026-07-03*
