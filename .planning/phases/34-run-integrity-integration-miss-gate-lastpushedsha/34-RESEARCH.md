# Phase 34: Run Integrity — Integration-Miss Gate + lastPushedSHA - Research

**Researched:** 2026-07-04
**Domain:** Go controller-runtime reconcilers + git-CLI Job binary (tide-push) — run-branch integration completeness
**Confidence:** HIGH (all core claims verified by reading the code in-session or by scratch-repo git experiments; no external dependencies introduced)

<user_constraints>
## User Constraints (from CONTEXT.md)

### Locked Decisions

**Integration topology**
- **D-01 — Both layers:** Per-wave integration Jobs extend to every wave *including the final one* (closing the `k < len(layers)-1` skip in `plan_controller.go`), AND the boundary-push Job keeps carrying the cumulative all-Succeeded-branch set as an idempotent re-merge. Structural fix plus self-healing defense in depth; matches INTEG-02's "cumulative Succeeded-branch set."
- **D-02 — Single-flight gate:** Control-plane serialization of run-branch merge writers: before creating any Job that merges into the run branch (wave-integration or boundary-push), the controller lists in-flight integration/push Jobs project-wide; if one is active, requeue. Mirrors the v1.0.6 D3 count-gate pattern (live `client.List` before dispatch). `flock(2)` inside the Job stays belt-and-braces only (locked constraint).
- **D-03 — Every trigger carries the set:** The cumulative Succeeded-branch set is computed inside the shared `triggerBoundaryPush` for ALL four levels — whichever level's trigger wins the deterministic `tide-push-<project.UID>` create race integrates identically. The nil-branches race (today only the plan-level trigger passes `IntegrateTaskBranches`) becomes harmless.
- **D-04 — Bounded retry for integration Jobs:** Wave-integration Job failures ride a bounded-retry state machine (attempts counter, capped backoff, terminal after cap) mirroring the boundary-push pattern — replacing today's first-failure → Plan Failed semantics. Transient infra failures (pod eviction, PVC contention) self-heal; merge conflicts are exempt (D-08).

**Gate mechanics**
- **D-05 — Every push gates:** All four levels' boundary pushes (plan/phase/milestone/project) run the completeness check before pushing. No unverified push class; the project-boundary push preceding `Complete` is covered.
- **D-06 — Verify inside tide-push:** One Job does integrate → verify → push. After merging, before pushing, tide-push runs `merge-base --is-ancestor` per expected branch; on a miss it exits nonzero with a typed envelope reason + the missing branch list. Verify and push are atomic under the same flock; the single-flight gate (D-02) serializes writers. No separate verify Job.
- **D-07 — Project-wide expected set:** The controller lists every Succeeded Task CR owned by the project at dispatch time and passes their branch names to the Job. Tasks succeeding after dispatch are covered by their own wave Job and the next push. Planner note: mind arg-size limits if a project accumulates hundreds of tasks (file/ConfigMap handoff is an acceptable fallback).
- **Clarification (recorded, not asked):** INTEG-03 pins `merge-base --is-ancestor` as the predicate — an empty-diff Succeeded task passes naturally (its branch tip is already an ancestor). SC-1's "merge commit reachable" phrasing describes the normal `--no-ff` case; no per-task merge-commit bookkeeping.

**Miss remediation**
- **D-08 — Retry then stick:** A gate miss fails the push Job with a typed reason; the existing #13b bounded-retry state machine re-dispatches (re-integrating and re-verifying). Only after the attempt cap does the sticky `integration-incomplete` condition park the push.
- **D-09 — Classify conflicts, don't retry:** tide-push distinguishes a genuine merge conflict from transient failure in the envelope reason. Conflicts skip remaining retries and park immediately with a distinct condition reason (content problem, human needed). Echoes Phase 35's BASE-02 classify-don't-retry principle.
- **D-10 — Same-wave conflict fails the Plan:** A wave-integration merge conflict marks the Plan Failed with a condition naming both branches — conflicting parallel tasks weren't actually independent, so the plan is defective ("cycles are bugs" philosophy). Recovery via the sanctioned `tide resume --retry-failed` after replanning. No conflict-resolution machinery in v1; no manual-merge-on-PVC protocol.
- **Carried forward (#13b, locked):** `Complete` still stamps as the control-plane rollup; the PUSH is what parks. The gate blocks the run branch from shipping, not the status rollup.

**Operator surface**
- **D-11 — Project + Plan condition split:** `integration-incomplete` lives on the Project (beside `BoundaryPushed` — it's a push-gate outcome and the Project is what operators watch); merge-conflict failure conditions live on the failing Plan (where the defect is).
- **D-12 — Named detail + metric:** The `integration-incomplete` condition message names each missing task + branch (truncated past a bound, e.g. first 10 + total count), and a result-labeled integration-outcome counter lands beside the existing `PushJobsTotal`. Diagnosable from `kubectl describe project` alone — pod logs get GC'd (COST-02 lesson).
- **D-13 — Recovery via `tide resume`:** Extend `tide resume` to reset the boundary-push attempt counter, implemented via the existing annotation mechanism (PushLeaseFailed bypass precedent). Preserves the D-07/v1.0.1 single-recovery-verb principle; `kubectl annotate` remains the escape hatch. The condition auto-clears whenever a later verify+push succeeds.
- **D-14 — lastPushedSHA (INTEG-04, mechanical):** The push envelope's `HeadSHA` is read in the push-Job success arm and patched to `Status.Git.LastPushedSHA`, arming the force-with-lease fence. Scout confirmed no assignment exists anywhere in `internal/` today — the doc comment at `project_controller.go:472` promises it but the wiring never landed.

**Binding constraints from STATE.md (v1.0.7):**
- Gate the *boundary push* on integration completeness, not `Complete` directly; the completeness verdict is always recomputed from git (`merge-base --is-ancestor`), never cached in `.status`.
- Tasks stay parallel; only run-branch merges serialize. No lockfile-existence protocols on the PVC — kernel `flock(2)` only, as belt-and-braces behind control-plane serialization.
- `charts/tide/values.yaml` is a FIXED contract.
- Repro evidence is perishable — lives on the minikube `tide-projects` PVC (the running `minikube` container on this host); do not delete that namespace/cluster.

### Claude's Discretion
- flock lockfile placement/naming inside the Job, branch-list handoff format (args vs file vs ConfigMap, per D-07 note), retry cap sizing, condition/reason naming, exact metric labels, and the kind-suite repro harness shape (deterministic single-wave degenerate case + 2-parallel-task final-wave case per INTEG-05) — planner/executor decide within the decisions above.

### Deferred Ideas (OUT OF SCOPE)
- **Dashboard display of `integration-incomplete` / `lastPushedSHA`** — Phase 37 (DASH-03); this phase only guarantees the condition/status fields exist and are named.
- **LLM verify-tier subagents** (plan-check + level-verify) — STAGE-01, own milestone; seed stays at `.planning/seeds/verify-level-subagent.md`.
- **Manual conflict-resolution protocol on the PVC** — explicitly rejected for v1 (D-10).
</user_constraints>

<phase_requirements>
## Phase Requirements

| ID | Description | Research Support |
|----|-------------|------------------|
| INTEG-01 | Every Succeeded task's worktree branch is integrated into the run branch — including tasks in the final Kahn wave (`plan_controller.go:1193` skips the last wave; a single-wave plan integrates nothing) | Skip verified at exact line: `for k := 0; k < len(layers)-1; k++` (plan_controller.go:1193). Extending the loop to include the final boundary also delays `patchPlanSucceeded` until final-wave integration completes (Succeeded check at :1208 runs after the loop) — a beneficial structural consequence. See "Fix Surface" and Pitfall 6. |
| INTEG-02 | Wave-parallel integrations cannot race — merges serialized + idempotent (cumulative set, kernel flock, safe re-merge) | Verified empirically: `git merge --no-ff <already-merged>` exits 0 with "Already up to date." and creates no commit → re-merge is idempotent. Serialization design: D-02 List-gate (template: `plannerInFlightCount`, dispatch_helpers.go:304) + `flock(2)` in tide-push. Push/wave Jobs currently carry NO labels — labels must be added for the List gate. See Patterns 2–3. |
| INTEG-03 | Boundary push gated on `git merge-base --is-ancestor` per Succeeded task; miss → sticky `integration-incomplete` condition | Verified empirically: `--is-ancestor` exits 0 for merged branch AND for empty-diff branch whose tip is an ancestor; exits 1 for unmerged. Verify step lands in `runPush` (cmd/tide-push/main.go) between integrate (:359) and push (:564). Envelope reason channel (`writePushEnvelope` → termination-log → `readPushEnvelope`) already exists. See Pattern 4. |
| INTEG-04 | `status.git.lastPushedSHA` stamped on successful boundary push from envelope `HeadSHA` | Verified: `LastPushedSHA` is READ at 3 dispatch sites but ASSIGNED NOWHERE in non-test code (`grep -rn "LastPushedSHA" internal/ cmd/ pkg/ api/`). The success arm at project_controller.go:734–748 sets `BoundaryPushed=True/Pushed` but never calls `readPushEnvelope` — the stamp belongs exactly there (same arm that sets the condition, per CONTEXT specifics). Envelope `HeadSHA` written by tide-push on success (main.go:571). |
| INTEG-05 | Kind-suite regression test reproduces 2-parallel-task final-wave integration miss (RED pre-fix) and locks the fix | Harness exists: hermetic git-http-server + stub-subagent (medium_http_test.go — reaches Complete over in-cluster http:// with anonymous push), typed fixtures (`newStubProject/Plan/Task` with `withTaskDependsOn`, wave_test.go), PVC-inspection Job pattern (chaos_resume_test.go:367 — inline Job mounting `tide-projects` PVC). See "Test Harness Shape". |
</phase_requirements>

## Project Constraints (from CLAUDE.md)

- **GSD workflow enforcement:** production code edits only through GSD plans (this phase's plans are that vehicle).
- **`charts/tide/values.yaml` is a FIXED contract** — no chart edits from this phase (no new chart values are needed; all changes are controller/binary/CRD-condition surface).
- **Two-DAG split / waves are derived:** the fix must not cache a schedule or accept wave lists; `ComputeWaves` on every reconcile stays (PERSIST-03 comment at plan_controller.go:1137).
- **Failure semantics at wave boundaries** are spec-pinned: failed task → siblings continue, dependents never dispatch. D-04's bounded retry for *integration Jobs* must not alter task-level semantics.
- **CRD `.status` only** — no external DB; the completeness verdict is recomputed from git, never cached (STATE.md constraint aligns).
- **Verification discipline:** `make test-int` exit ≠ Ginkgo green — read `MAKE_EXIT` and grep `^--- FAIL|^FAIL\s`; plain go-tests share the kind package.
- **Conditions + typed reasons** for operator-visible failure; classify-don't-retry is a milestone principle (BASE-02).
- **Vocabulary:** water/tide metaphor in names and log lines where natural.
- **Don't refactor bot identity here** (`TIDE_BOT_NAME`/`TIDE_BOT_EMAIL` in integrate.go, `tideBotSignature()` in tide-push) — Phase 36 lands on these sites; don't make them worse.

## Summary

This phase is a pure in-repo controller + Job-binary change with zero new external dependencies. The entire fix surface was read in-session and every load-bearing claim verified: (1) the final-wave integration skip exists verbatim at `plan_controller.go:1193`; (2) **no live trigger currently passes task branches to any boundary push** — the plan-level `maybeTriggerBoundaryPush` (the only signature that accepts branches) is invoked exactly once at planner-Job completion with `nil` taskItems, *before Tasks exist* (plan_controller.go:770, documented in the CR-03 comment), and the project-level `dispatchBoundaryPush` builds `PushOptions` without `IntegrateTaskBranches` (project_controller.go:849–855); (3) `Status.Git.LastPushedSHA` is read at three dispatch sites and assigned nowhere. Consequence: today the *only* code path that merges task branches into the run branch is the per-wave integration Job for non-final waves — a Succeeded task in a plan's final Kahn wave is integrated by nothing. This is stronger than CONTEXT's "nil-branches race" framing: at present the boundary-push nil-branch state is unconditional, which is why D-03 (compute the cumulative set *inside* `triggerBoundaryPush`, via a live List) is the correct fix rather than fixing callers.

The git semantics the decisions lean on were verified in a scratch repo: re-merging an already-merged branch with `--no-ff` exits 0 ("Already up to date.") and creates no commit → cumulative re-merge is idempotent; `git merge-base --is-ancestor` exits 0 for a merged branch *and* for an empty-diff branch whose tip is already an ancestor, 1 for an unmerged branch → the INTEG-03 predicate needs no empty-diff special case; a conflict exits 1 with `CONFLICT` / `Automatic merge failed; fix conflicts` in combined output → D-09 classification is a string match, and the conflicted worktree retains `MERGE_HEAD` on the shared PVC, so tide-push must `git merge --abort` before exiting on conflict or every retry breaks differently (Pitfall 1).

All the machinery the decisions call for already exists as reusable patterns: the #13b bounded-retry state machine (`reconcileBoundaryPush`/`dispatchBoundaryPush`, cap 5, capped backoff), the D3 in-flight List gate (`plannerInFlightCount` keyed on a Job label — push/wave Jobs currently carry no labels and must gain them), the termination-log envelope channel (`writePushEnvelope`/`readPushEnvelope` — the reader is a ProjectReconciler method and needs generalizing for PlanReconciler use), and `tide resume`'s annotation + status-reset seams. The one environment caveat: `go` is **not on this Mac host's PATH** (kind v0.32.0 and kubectl v1.36.1 ARE present via homebrew) — builds/tests must run in the repo's devcontainer (golang image + docker-in-docker) or after `brew install go`; Docker 29.5.3 is up and the `minikube` container holding the perishable repro evidence is running (do not delete).

**Primary recommendation:** Plan the phase as (a) controller-side: extend the wave loop, add the shared cumulative-set helper + single-flight label/List gate, bounded retry for wave Jobs, `LastPushedSHA` stamp + `integration-incomplete` condition + metric in the existing success/failure arms; (b) tide-push side: verify step + conflict/miss envelope classification + flock + merge-abort hygiene; (c) kind-suite: single-wave degenerate RED repro first (cheapest), then the 2-parallel-task final-wave repro, using the medium-http hermetic git server and a PVC-mounted assertion Job running `git merge-base --is-ancestor`.

## Architectural Responsibility Map

| Capability | Primary Tier | Secondary Tier | Rationale |
|------------|-------------|----------------|-----------|
| Extend integration to final wave (INTEG-01) | Controller — `PlanReconciler` (`plan_controller.go`) | — | The `k < len(layers)-1` loop is reconcile logic; wave derivation stays controller-owned |
| Cumulative Succeeded-branch set (D-03/D-07) | Controller — shared helper called by `triggerBoundaryPush` + `dispatchBoundaryPush` | — | Only the controller can `client.List` Task CRs; the Job receives the set as args |
| Single-flight writer gate (D-02) | Controller — dispatch sites in `boundary_push.go` + `project_controller.go` | K8s API (AlreadyExists on deterministic names remains the base layer) | Live List before Create mirrors D3 count-gate; requires new Job labels |
| flock serialization (belt-and-braces) | Job binary — `cmd/tide-push` | `pkg/git` (lock acquired around `IntegrateTaskBranches` + verify + push) | Manager cannot mount project PVCs; all git ops run in Jobs (established pattern) |
| Integrate → verify → push atomicity (D-06) | Job binary — `cmd/tide-push` `runPush` | `pkg/git` for the merge primitive | One Job, one flock scope; no separate verify Job per locked decision |
| Conflict vs transient classification (D-09) | Job binary — envelope `reason` | Controller consumes reason via termination-log | Job is where the git error text lives; controller maps reason → condition |
| Bounded retry for wave Jobs (D-04) | Controller — `PlanReconciler.reconcileWaveBoundary` | `Plan.Status` (new attempts field, both API versions) | Mirrors #13b which lives in the owning reconciler |
| `integration-incomplete` condition + metric (D-11/D-12) | Controller — `ProjectReconciler` push arms; Plan conditions for conflicts | `internal/metrics/registry.go` | Conditions are status writes; metric beside `PushJobsTotal` |
| `LastPushedSHA` stamp (D-14) | Controller — `reconcileBoundaryPush` success arm | Envelope via `readPushEnvelope` | Same arm that sets `BoundaryPushed=True` (CONTEXT specifics require co-location) |
| `tide resume` counter reset (D-13) | CLI — `cmd/tide/resume.go` | Controller consumes annotation | Annotation-mechanism precedent (`bypassPushLeaseAnnotation`) |
| Regression repro (INTEG-05) | Test — `test/integration/kind` Layer B | envtest Layer A for controller-logic units | Only kind exercises real Jobs + real git on a PVC |

## Standard Stack

### Core (no changes — existing pinned stack)

| Library | Version | Purpose | Why Standard |
|---------|---------|---------|--------------|
| controller-runtime | v0.24.x | Reconcilers, client, conditions | Project-pinned [VERIFIED: go.mod] |
| Ginkgo/Gomega | v2.28 / project-pinned | Layer A/B test tiers | Project-pinned [VERIFIED: test suite] |
| go-git/v5 | v5.19.0 | tide-push open/commit/push | Already in use; **merges stay git-CLI** (go-git can't three-way merge — documented in pkg/git/integrate.go D-01 note) [VERIFIED: integrate.go:32] |
| git CLI (in tide-push image) | — | `merge --no-ff`, `merge-base --is-ancestor`, `worktree` | Already the merge engine; the verify predicate is one more git-CLI call in the same binary [VERIFIED: integrate.go] |
| stdlib `syscall` / `golang.org/x/sys/unix` | x/sys v0.44.0 (indirect, in go.sum) | `Flock(fd, LOCK_EX)` in tide-push | No new module download; x/sys already resolved. Recommend `golang.org/x/sys/unix.Flock` (promotes indirect→direct, no version change; the maintained superset of deprecated-ish `syscall`) [VERIFIED: go.mod:159] |

### Supporting

| Library | Version | Purpose | When to Use |
|---------|---------|---------|-------------|
| prometheus/client_golang | v1.23 (pinned) | integration-outcome counter | Register beside `PushJobsTotal` in `internal/metrics/registry.go:173` |
| batchv1 Jobs | k8s.io (controller-runtime-dictated) | wave-integration + push Jobs | Existing; add labels for the D-02 List gate |

### Alternatives Considered

| Instead of | Could Use | Tradeoff |
|------------|-----------|----------|
| git-CLI `merge-base --is-ancestor` | go-git ancestry walk (`commit.IsAncestor`) | go-git v5 has `Commit.IsAncestor`, but the integration worktree ops are already git-CLI and the bare repo is the source of truth; one `exec.Command` per branch is simpler and matches INTEG-03's pinned predicate. Use git CLI. |
| `x/sys/unix.Flock` | stdlib `syscall.Flock` | Both work on linux (Job runtime) and darwin (unit tests). x/sys is the maintained path and already in go.sum. Either is acceptable; planner should pick one and be consistent. |
| CSV args for branch list (existing) | file/ConfigMap handoff | CSV branch names are ~45 bytes each (`tide/wt-<36-char-uid>`); 500 tasks ≈ 23 KB — comfortably inside container-arg limits. Keep CSV for v1; the D-07 fallback threshold is far away. [ASSUMED — arithmetic, not load-tested] |

**Installation:** none — no new packages.

## Package Legitimacy Audit

No external packages are installed by this phase. The only dependency-adjacent change is promoting `golang.org/x/sys` (already in `go.sum` at v0.44.0 as an indirect dependency) to direct if `unix.Flock` is used — not a new install, no registry fetch of a new artifact.

**Packages removed due to [SLOP] verdict:** none
**Packages flagged as suspicious [SUS]:** none

## Verified Fix Surface (file-by-file)

All claims below are `[VERIFIED: read in-session]` at the cited lines.

### `internal/controller/plan_controller.go`
- **:1193** — `for k := 0; k < len(layers)-1; k++` — the INTEG-01 skip. Extending to `k < len(layers)` makes `reconcileWaveBoundary` run for the final boundary; `layers[k]` indexing stays valid (`k = len(layers)-1` is the final wave). REQUIREMENTS.md cites ":1192" (comment line); the loop is :1193.
- **:997–1009** — `reconcileWaveBoundary` early-outs: already-integrated (`IntegratedThroughWave >= waveNum`) and the no-git/no-image short-circuit (`project == nil || Spec.Git == nil || RepoURL == "" || TidePushImage == ""`). The short-circuit keeps stub/test projects unblocked when the loop extends — final-wave gating only engages for real git projects.
- **:1031–1036** — RESPONSIBILITY A failure arm: `integJob.Status.Failed > 0` → `patchPlanFailed(ReasonWaveIntegrationFailed)` immediately. This is what D-04 replaces with bounded retry (and D-09/D-10 split by envelope reason).
- **:1208–1214** — `BoundaryDetected` → `patchPlanSucceeded` runs AFTER the wave-boundary loop. Extending the loop therefore delays Plan=Succeeded until the final integration Job completes (returns handled=true requeue while running). Structural win: `Plan=Succeeded` will imply all waves integrated.
- **:770** — the only live plan-level `maybeTriggerBoundaryPush` call — passes `nil` taskItems, fires at planner-Job completion *before Tasks exist* (CR-03 comment at :757–769 explains why a task-complete-time plan push seam doesn't exist). **Implication for D-03:** the cumulative set cannot come from callers; it must be computed inside `triggerBoundaryPush` from a live List.

### `internal/controller/boundary_push.go`
- **:58–134** — `triggerBoundaryPush`: deterministic `tide-push-<project.UID>` name, Get-then-Create, AlreadyExists tolerated. D-03's List-based cumulative set + D-02's in-flight gate go here. Note `:117–118`: `Branch` and `LastPushedSHA` come from `project.Status.Git` — the lease anchor is live-read (stays empty today because nothing stamps it).
- **:146–193** — `triggerWaveIntegrationJob`: builds via `buildPushJob` then renames to `tide-push-wave-<plan.UID>-<k>`, re-owners to Plan. Wave Jobs carry `--project-uid` (from buildPushJob) so envelope writes work.
- **:200–223** — the four per-level entry points: milestone/phase pass `nil` branches; plan collects Succeeded branches from `taskItems` — but see plan_controller.go:770 (live call passes nil).

### `internal/controller/project_controller.go`
- **:472–474** — the doc comment promising "on success, patch Status.Git.LastPushedSHA" — never implemented (D-14 scout confirmed; re-verified via grep: no assignment exists in non-test code).
- **:496–505** — Complete fast-path: a Complete project runs ONLY `reconcileBoundaryPush` (#13b). `Complete` stamps as rollup; the push parks — the carried-forward constraint's mechanical home.
- **:734–748** — push-Job success arm: sets `BoundaryPushed=True/Pushed`, resets `LeaseFailureCount`, **does not read the envelope**. D-14's stamp goes here via `readPushEnvelope` (the CONTEXT specifics demand the stamp land in the same arm that sets the condition).
- **:753–823** — failure classification arms keyed on envelope `reason` (`leak-detected`, `lease-rejected`, default bounded retry with `deleteFailedPushJob` + re-dispatch). D-08's `integration-incomplete` (post-cap park) and D-09's conflict park slot into this switch as new reason cases.
- **:700–718** — attempt-cap exhaustion arm (cap = `maxBoundaryPushAttempts = 5`, project_controller.go:137). D-08's sticky condition parks here after cap.
- **:85–109** — `readPushEnvelope`: reads pod termination message by `job-name` label. **ProjectReconciler method** — needs generalizing (package-level func) so PlanReconciler can classify wave-Job failures for D-09/D-10.
- **:842–884** — `dispatchBoundaryPush`: builds `PushOptions` **without** `IntegrateTaskBranches` — the project-level nil-branch site D-03 fixes.

### `pkg/git/integrate.go`
- **:60–110** — `IntegrateTaskBranches`: per-branch `git merge --no-ff` in the shared integration worktree `<workspace>/worktrees/run-<branch>/`; idempotent worktree provisioning ("already checked out"/"already exists" tolerated); **no locking; no merge-abort on failure** (Pitfall 1). Bot identity via `TIDE_BOT_NAME`/`TIDE_BOT_EMAIL` env — do not refactor (Phase 36).

### `cmd/tide-push/main.go`
- **:359–380** — integrate step, then the integration-only early-exit (`ArtifactPaths` empty → exit 0 **without writing an envelope**). D-09 needs wave Jobs to write a success/conflict envelope; the failure path at :362 already writes `reason: "integration-failed"` (undifferentiated — D-09 splits it).
- **:488–528** — clean-tree handling: boundary pushes with no artifacts push the already-integrated HEAD (the medium-DoD fix). The D-06 verify step belongs after integrate, before commit/push (~:380–390).
- **:560–572** — push + `writePushEnvelope(cfg, newHash.String(), exitSuccess, "")` — the `HeadSHA` D-14 reads.
- **:655–703** — envelope writes to `/dev/termination-log` (per-pod, safe) AND `<workspace>/envelopes/push/<project-uid>.json` (**shared path — wave Jobs and boundary pushes collide**, Pitfall 3).
- **:705–734** — `classifyPushError` — the existing string-matching classification pattern D-09 mirrors for merge output.

### `internal/controller/push_helpers.go`
- **:144–275** — `buildPushJob`: `BackoffLimit: 2`, `TTLSecondsAfterFinished: 300`, **no labels on the Job** (D-02's List gate needs e.g. `tideproject.k8s/role: git-writer` + `tideproject.k8s/project: <name>`), termination-message policy `FallbackToLogsOnError` already set.

### `api/v1alpha2` + `api/v1alpha1`
- `Plan.Status.IntegratedThroughWave` exists in **both** versions (v1alpha2 plan_types.go:74–80, v1alpha1 plan_types.go:61–67). `BoundaryPushStatus` (Attempts/LastAttemptTime/LastError) exists in both (v1alpha2 project_types.go:289+, v1alpha1 project_types.go:271+). **Any new status field (e.g., a wave-integration attempts counter on Plan.Status) must land in both versions with conversion parity** — the BASE-03 pattern next phase; keep this phase's schema additions minimal (conditions need no schema change).
- Condition/reason vocabulary lives in `api/v1alpha2/shared_types.go` (`ConditionBoundaryPushed`, `ReasonPushed/Pushing/PushFailed`, `ReasonWaveIntegrationFailed` at :203). New: an `integration-incomplete`-flavored condition type + reasons (naming is Claude's discretion; follow existing PascalCase reason style, e.g. `ConditionIntegrationIncomplete` / `ReasonIntegrationIncomplete`, `ReasonMergeConflict`).

### `cmd/tide/resume.go`
- `resumeRun` (:70) patterns: annotation consume + status-subresource resets + condition stamping, with careful re-fetch between metadata and status patches. D-13 extension: reset `Status.BoundaryPush.Attempts` (and clear the sticky condition) — either directly via status patch (CLI already does status patches) or via a bypass-style annotation the controller consumes (`bypassPushLeaseAnnotation` precedent, project_controller.go:128). CONTEXT pins the **annotation mechanism**.

### `internal/metrics/registry.go`
- **:173–179** — `PushJobsTotal = NewCounterVec({project, outcome})`, outcomes `{success, leak, lease, auth, internal, dispatched, exhausted}` observed in code. D-12's counter (e.g. `tide_integration_outcomes_total{project, outcome}`) registers beside it; registry has a seed-and-assert test pattern (registry_test.go).

## Architecture Patterns

### System Architecture Diagram

```
                          ┌────────────────────────── CONTROL PLANE (manager pod — cannot mount PVC) ─────────────────────────┐
                          │                                                                                                    │
 Task Succeeded ──────────┤ PlanReconciler.reconcileWaveMaterialization                                                        │
 (status update event)    │   ComputeWaves (every reconcile)                                                                   │
                          │   for k := 0; k < len(layers); k++   ◄── INTEG-01: loop now includes final boundary                │
                          │     reconcileWaveBoundary(k)                                                                       │
                          │       ├─ A: Job exists? → success: stamp IntegratedThroughWave                                     │
                          │       │                 → failed: read envelope reason ──► conflict? → Plan Failed (D-10)          │
                          │       │                                              └──► transient? → bounded retry (D-04)        │
                          │       └─ B: wave-k all Succeeded + no in-flight git-writer Job (D-02 List gate)                    │
                          │            → create tide-push-wave-<plan.UID>-<k>  [cumulative Succeeded set]                      │
                          │                                                                                                    │
 Level boundary ──────────┤ triggerBoundaryPush (shared, 4 levels)                                                             │
 (plan/phase/mile/proj)   │   List Succeeded Tasks project-wide (D-03/D-07) → IntegrateTaskBranches = cumulative set           │
                          │   D-02 gate: List in-flight git-writer Jobs → requeue if any                                       │
                          │   Create tide-push-<project.UID>  (AlreadyExists = idempotent)                                     │
                          │                                                                                                    │
 Complete fast-path ──────┤ ProjectReconciler.reconcileBoundaryPush (#13b state machine, cap 5)                                │
                          │   success arm: readPushEnvelope → stamp Status.Git.LastPushedSHA (D-14) + BoundaryPushed=True      │
                          │   failed arm:  reason switch → integration-incomplete (post-cap, D-08) / merge-conflict park (D-09)│
                          └──────────────────────────────┬─────────────────────────────────────────────────────────────────────┘
                                                         │ creates batchv1 Jobs (labeled: role=git-writer, project=<name>)
                          ┌──────────────────────────────▼───────────────── DATA PLANE (Job pod, PVC-mounted) ─────────────────┐
                          │ tide-push --mode=push                                                                              │
                          │   flock(<workspace>/repo.git/... lockfile)          ◄── belt-and-braces (D-02 primary is List gate)│
                          │   1. IntegrateTaskBranches: git merge --no-ff per branch (idempotent: "Already up to date")        │
                          │      conflict → git merge --abort → envelope reason=merge-conflict, exit nonzero (D-09)            │
                          │   2. VERIFY (D-06): per expected branch: git merge-base --is-ancestor <br> <runBranch>             │
                          │      miss → envelope reason=integration-incomplete + missing list, exit nonzero                    │
                          │   3. stage/commit (if artifacts) → gitleaks scan → git push --force-with-lease=<lastPushedSHA>     │
                          │   4. writePushEnvelope{HeadSHA, reason} → /dev/termination-log (+ PVC)                             │
                          └────────────────────────────────────────────────────────────────────────────────────────────────────┘
                                                         │ push
                                                         ▼
                                              remote (git-http-server in kind / real host in prod)
```

### Recommended change layout (no new packages)

```
internal/controller/
├── plan_controller.go        # loop extension, bounded wave retry, conflict→Plan Failed
├── boundary_push.go          # cumulative-set helper, D-02 gate, Job labels
├── project_controller.go     # LastPushedSHA stamp, integration-incomplete arms, envelope reader generalization
├── push_helpers.go           # Job labels, (optional) new PushOptions verify field
pkg/git/
├── integrate.go              # merge --abort hygiene on conflict; (optional) typed conflict error
cmd/tide-push/
├── main.go                   # flock, verify step, conflict classification, wave-Job success envelope
cmd/tide/
├── resume.go                 # D-13 attempts-reset extension
api/v1alpha2 (+ v1alpha1 parity if any new status field)
├── shared_types.go           # new condition/reason constants
internal/metrics/
├── registry.go               # integration-outcome counter
test/integration/kind/
├── integration_miss_test.go  # INTEG-05 repro (new file; single-wave + 2-parallel-task cases)
```

### Pattern 1: Cumulative Succeeded-branch set (D-03/D-07)

**What:** A single helper listing Succeeded Task CRs project-wide, mapping to branch names.
**When to use:** Inside `triggerBoundaryPush` AND `dispatchBoundaryPush` (and wave dispatch if the planner opts for cumulative wave sets per D-01's "cumulative" language).
**Example (shapes verified against existing code):**
```go
// Source: pattern composed from cmd/tide/resume.go:274 (List by project label)
// and boundary_push.go:215 (branch mapping). Label verified present:
// fixtures_test.go:106 labelProject = "tideproject.k8s/project".
func succeededTaskBranches(ctx context.Context, c client.Client, ns, projectName string) ([]string, error) {
    var tasks tideprojectv1alpha2.TaskList
    if err := c.List(ctx, &tasks,
        client.InNamespace(ns),
        client.MatchingLabels{"tideproject.k8s/project": projectName},
    ); err != nil {
        return nil, err
    }
    var branches []string
    for i := range tasks.Items {
        if tasks.Items[i].Status.Phase == "Succeeded" {
            branches = append(branches, pkggit.TaskBranchName(string(tasks.Items[i].UID)))
        }
    }
    sort.Strings(branches) // deterministic order → deterministic Job args across retries
    return branches, nil
}
```

### Pattern 2: Single-flight git-writer gate (D-02)

**What:** Live `client.List` of in-flight run-branch-writer Jobs before creating another; requeue if any.
**When to use:** Every site that creates a Job which merges into or pushes the run branch: `reconcileWaveBoundary` RESPONSIBILITY B, `triggerBoundaryPush`, `dispatchBoundaryPush`.
**Prerequisite:** push/wave Jobs currently carry **no labels** (verified: `buildPushJob` ObjectMeta has only Name/Namespace). Add e.g. `tideproject.k8s/role: git-writer` + `tideproject.k8s/project: <name>` in `buildPushJob`.
**Template:** `plannerInFlightCount` (dispatch_helpers.go:304) — MatchingLabels List, skip `DeletionTimestamp != nil`, count non-terminal via `isJobTerminal`.

### Pattern 3: flock belt-and-braces in tide-push

**What:** Kernel advisory lock held across integrate → verify → push inside the Job.
**Locked constraint:** flock(2) only — no lockfile-existence protocols (the lockfile may exist forever; only the kernel lock state matters, so a crashed Job never wedges a successor).
**Example:**
```go
// Source: golang.org/x/sys/unix (v0.44.0 already in go.sum); syscall.Flock equivalent.
// Lockfile placement (discretion): inside repo.git so it rides the repo's lifecycle
// and no git checkout ever cleans it.
lockPath := filepath.Join(cfg.Workspace, "repo.git", "tide-integrate.lock")
f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o664)
if err != nil { /* envelope + exit */ }
defer f.Close()
if err := unix.Flock(int(f.Fd()), unix.LOCK_EX); err != nil { /* envelope + exit */ }
// kernel releases the lock automatically on process exit — no unlock protocol needed
```
[ASSUMED — flock is effective on the RWO PVC filesystems in play (kind local-path, minikube hostpath: both node-local ext4/overlay where flock works; RWO pins all writers to one node). Low risk: control-plane serialization is primary.]

### Pattern 4: Verify step in tide-push (D-06)

**What:** After merging, before commit/push: per expected branch, `git merge-base --is-ancestor <branch> <runBranch>` in the integration worktree; collect misses.
**Verified semantics (scratch-repo, git 2.54):** exit 0 = ancestor (including an empty-diff branch whose tip is already an ancestor — no special case needed); exit 1 = not ancestor; exit ≥128 = error (distinguish via exit code, not just nonzero).
```go
// Source: verified in-session (scratch repo). exec.ExitError.ExitCode() == 1 → miss;
// other errors are infrastructure failures, not misses.
var missing []string
for _, br := range expectedBranches {
    cmd := exec.Command("git", "-C", integrationDir, "merge-base", "--is-ancestor", br, runBranch)
    if err := cmd.Run(); err != nil {
        var ee *exec.ExitError
        if errors.As(err, &ee) && ee.ExitCode() == 1 {
            missing = append(missing, br)
            continue
        }
        return fmt.Errorf("merge-base --is-ancestor %s: %w", br, err) // infra error, typed separately
    }
}
if len(missing) > 0 {
    writePushEnvelope(cfg, "", exitIntegrationMiss, "integration-incomplete:"+strings.Join(missing, ","))
    return exitIntegrationMiss
}
```
(Reason string format / new exit code number: Claude's discretion; follow the existing exit-code map style at main.go:36–46. The missing-branch list must reach the condition message per D-12 — the termination-log envelope is the channel; it is capped at 4096 bytes, so truncate the list in the envelope too, e.g. first 10 + count.)

### Pattern 5: Conflict classification (D-09/D-10)

**Verified conflict shape (scratch repo):** `git merge --no-ff` on conflict → exit 1, combined output contains `CONFLICT` and `Automatic merge failed; fix conflicts and then commit the result.` A string match on `CONFLICT (` or `Automatic merge failed` distinguishes content conflicts from transient failures (network, permissions, missing ref). Mirror `classifyPushError`'s conservative string-matching style (main.go:710).
**Mandatory hygiene:** on conflict, run `git -C <integrationDir> merge --abort` before exiting — the integration worktree lives on the shared PVC and a lingering `MERGE_HEAD` breaks every subsequent Job differently ("You have not concluded your merge"). Verified: `merge --abort` restores the worktree cleanly. Also consider a defensive `git merge --abort || true` / `git reset --merge` at integration start (self-healing against a prior crashed Job).

### Pattern 6: Bounded retry for wave-integration Jobs (D-04)

**Template:** the #13b machine — `Attempts` counter in status, `deleteFailedPushJob`-style Background-propagation delete (project_controller.go:895 — Background, NOT Foreground: foreground finalizers never clear under envtest, wedging the deterministic name), capped backoff (`boundaryPushRequeue`), cap exhaustion → terminal condition.
**State home:** a per-plan attempts counter. Options: (a) new `Plan.Status` field (requires v1alpha1+v1alpha2 parity — small but real API churn), (b) per-wave annotation on the Plan, (c) derive from Job generation/failure count. The #13b precedent uses status (`BoundaryPushStatus`); mirroring it on Plan.Status (e.g. `WaveIntegration BoundaryPushStatus`-shaped) is the consistent choice — planner decides, but budget for the two-version API touch either way.
**Conflict exemption (D-09/D-10):** read the wave Job's envelope reason via the generalized `readPushEnvelope`; `merge-conflict` → skip retries, `patchPlanFailed` with a condition naming both branches (the merge error text from `IntegrateTaskBranches` already carries `merge %s → %s` — surface branch names, verified integrate.go:104).

### Anti-Patterns to Avoid

- **Caching the completeness verdict in `.status`:** locked constraint — the verdict is recomputed from git on every push. `IntegratedThroughWave` is progress bookkeeping (which boundary Jobs ran), not the gate verdict; don't promote it into one.
- **Foreground Job deletion before re-dispatch:** verified footgun (project_controller.go:889–894 comment) — Background propagation only.
- **Reading `plan.Status.IntegratedThroughWave` to skip the verify:** the verify must run inside tide-push from git state (D-06), not be short-circuited by controller bookkeeping.
- **Adding chart values:** nothing here needs one; `values.yaml` is FIXED.
- **Refactoring bot identity while touching integrate.go/tide-push:** Phase 36 lands on these stabilized sites.
- **A separate verify Job:** rejected by D-06 — verify and push must be atomic under one flock.

## Don't Hand-Roll

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|-------------|-----|
| Completeness bookkeeping | Per-task merge-commit ledger in `.status` | `git merge-base --is-ancestor` recomputed from git | Locked (INTEG-03 clarification); git IS the ledger; empty-diff tasks pass naturally (verified) |
| Merge engine | go-git three-way merge | git CLI `merge --no-ff` (existing `IntegrateTaskBranches`) | go-git v5.19 only fast-forwards (documented in integrate.go D-01 note) |
| Writer serialization | Distributed lock / lease CRD / PVC lockfile-existence protocol | K8s deterministic Job names (AlreadyExists) + D-02 List gate + kernel flock | Locked constraints; the API server is already the serialization point (D-B5) |
| Retry machinery | New backoff framework for wave Jobs | #13b pattern (`BoundaryPushStatus`, `boundaryPushRequeue`, Background delete) | Proven in-repo; survives controller restarts (re-derived from status, not memory) |
| Job→controller result channel | Log scraping / PVC polling from manager | termination-log envelope (`writePushEnvelope`/`readPushEnvelope`) | Existing contract; manager can't mount project PVCs; logs get GC'd (COST-02 lesson) |
| Operator recovery verb | New CLI subcommand | `tide resume` extension via annotation (PushLeaseFailed bypass precedent) | Locked (D-13, single-recovery-verb principle) |

**Key insight:** every mechanism this phase needs already exists in the repo as a named, battle-tested pattern; the phase is composition + two genuinely new behaviors (the verify predicate and the flock), both of which are single git/syscall primitives.

## Common Pitfalls

### Pitfall 1: Conflicted merge leaves `MERGE_HEAD` on the shared PVC
**What goes wrong:** `IntegrateTaskBranches` returns the merge error without aborting; the integration worktree persists across Jobs (PVC). The retry Job then fails with "You have not concluded your merge" — a *different* error that misclassifies as transient, burning the whole retry budget.
**Why it happens:** integrate.go:102–106 has no `merge --abort` on failure (verified).
**How to avoid:** abort on conflict before exiting; defensively `git merge --abort`/`git reset --merge` at integration start.
**Warning signs:** kind repro retry logs showing "not concluded your merge" instead of the original CONFLICT text.

### Pitfall 2: Wave-Job envelope collides with boundary-push envelope on the PVC
**What goes wrong:** `writePushEnvelope` writes to the shared `<workspace>/envelopes/push/<project-uid>.json` for ALL push-mode Jobs (main.go:694–702); a failing wave Job overwrites the boundary push's envelope (and vice versa).
**How to avoid:** the controller-side reader (`readPushEnvelope`) uses the **termination-log** surface keyed by `job-name` label — per-pod, collision-free. Read via termination log only (as today); if the PVC copy is kept, key it by Job name, not project UID. D-14's stamp reads the boundary-push Job's pod specifically — safe.
**Warning signs:** `lastPushedSHA` showing a wave-job value, or conflict reasons appearing on the wrong arm.

### Pitfall 3: Integration-only success writes NO envelope today
**What goes wrong:** the wave-Job success path exits at main.go:374–379 before `writePushEnvelope`; the D-12 outcome metric and any success-side classification for wave Jobs have no envelope to read.
**How to avoid:** add an envelope write to the integration-only success arm (termination log). Cheap, and makes wave-Job outcomes uniformly observable.

### Pitfall 4: `TTLSecondsAfterFinished: 300` erases the evidence
**What goes wrong:** push Jobs and their pods vanish 5 minutes after finishing (push_helpers.go:212). If the controller is down/slow past that window, the success arm finds `BoundaryPushed` unset, the Job gone, and no envelope → D-14 stamp silently skipped; worse, the state machine re-dispatches a fresh push (idempotent, but the SHA from the *first* push is lost until the second lands).
**How to avoid:** stamp `LastPushedSHA` in the same reconcile that observes Job success (it already sets the condition there — co-location per CONTEXT specifics); tolerate a missing envelope by keeping the condition transition but logging + metric on stamp-skip. Do NOT block `BoundaryPushed=True` on envelope readability (envelope-unreadable is already a recognized state, :807).
**Warning signs:** `BoundaryPushed=True` with empty `lastPushedSHA` after a controller restart — exactly the observed first-run state, so the INTEG-05 assertions must check both together.

### Pitfall 5: `BackoffLimit: 2` interacts with the controller-level attempts counter
**What goes wrong:** each Job internally retries its pod up to 2 times before `Failed > 0`; the controller-level cap (5) counts *Jobs*. Worst case = 15 pod executions of the merge sequence. Each rerun re-merges — safe only because re-merge is idempotent (verified) AND conflict aborts cleanly (Pitfall 1).
**How to avoid:** keep `BackoffLimit` as-is; don't double-classify — classification happens once per terminal Job, from the last pod's termination message (K8s keeps the last pod until Job deletion/TTL).

### Pitfall 6: Extending the wave loop changes Plan=Succeeded timing
**What goes wrong:** Plan now stamps Succeeded only after the final-wave integration Job completes. Existing envtest/kind specs that assert quick `Plan=Succeeded` on git-configured fixtures may start flaking or timing out; stub projects are unaffected (no-git short-circuit at :1007).
**How to avoid:** audit Layer A/B specs that use `withGit(...)` fixtures + Plan-phase assertions; extend timeouts or assert the new intermediate state. Also confirms SC-1's ordering: `Complete` cannot precede final-wave integration anymore for git projects.

### Pitfall 7: The single-flight gate can deadlock against the deterministic push job
**What goes wrong:** naive D-02 ("if ANY git-writer Job in flight → requeue") applied to `reconcileBoundaryPush`'s own retry loop could see the very Job it manages and requeue forever; or a wave Job blocks the boundary push which blocks Plan progress.
**How to avoid:** the gate applies to *creation* of a NEW writer while a DIFFERENT writer is active. Exclude the Job the state machine itself owns/is observing (match on name). Keep requeue intervals short (5s, matching existing).

### Pitfall 8: Verify-before-push race with tasks succeeding mid-flight
**What goes wrong:** a task succeeds after the expected set was computed at dispatch; the pushed branch lacks it, yet verify passes (set was smaller). This is by design (D-07: "Tasks succeeding after dispatch are covered by their own wave Job and the next push") — do not "fix" it by re-reading the cluster from inside the Job (the Job has no API access, by trust-boundary design).
**Warning signs:** none — document in code comments so a future reader doesn't treat it as a bug.

### Pitfall 9: RED-first discipline for INTEG-05 in an expensive suite
**What goes wrong:** the kind suite is heavy (fresh cluster per heavy run on the constrained VM); authoring the regression test after the fix means the RED state is never demonstrated.
**How to avoid:** plan a task ordering where the repro spec lands and runs RED against pre-fix code (cheapest RED: the single-wave degenerate case — a single-wave git-configured plan produces zero wave-integration Jobs today, deterministically) before the fix tasks merge. The 2-parallel-task final-wave case locks the full observed shape.

## Test Harness Shape (INTEG-05)

All ingredients verified present in `test/integration/kind`:

- **Hermetic git remote:** `medium_http_test.go` — git-http-server Deployment + Service + demo-remote-init Job, anonymous push enabled (`http.receivepack=true` asserted), `mediumHTTPTargetRepo` in-cluster URL. Reuse this fixture stack in a dedicated namespace.
- **Typed fixtures:** `newStubProject(... withGit(repo, secret))`, `newStubPlan`, `newStubTask(... withTaskDependsOn(...))` (fixtures_test.go; wave_test.go shows the parents+tasks direct-apply pattern). Stub-subagent `success` mode writes a canned file under the first DeclaredOutputPath (cmd/stub-subagent) — each task branch gets a real authored commit, so a dropped merge is observable as missing content.
- **Two repro cases (CONTEXT: treat as distinct mechanisms):**
  1. *Single-wave degenerate (cheapest RED for INTEG-01):* one Plan, 2 tasks, no deps → one wave → today zero wave-integration Jobs run; assert both task branches are ancestors of the run branch → RED pre-fix.
  2. *2-parallel-task final wave (the observed shape):* one Plan, task A (wave 0), tasks B+C (`dependsOn: A`, final wave) → today only wave-0 integrates; assert all three branches are ancestors + `lastPushedSHA` set + `BoundaryPushed=True` — reproducing "Complete + BoundaryPushed=True + missing deliverable + empty lastPushedSHA".
- **Branch-ancestry assertion vehicle:** the chaos_resume pattern (chaos_resume_test.go:367+) — an inline Job mounting the `tide-projects` PVC at subPath `<project.UID>/workspace`, running `git -C /workspace/repo.git merge-base --is-ancestor <task-branch> <run-branch>`, result via exit code/termination message. Needs a git-capable image: the tide-push image is already built+loaded by `make test-int-kind-prep` (`loadRequiredImage` pattern). Alternative: clone from the git-http-server Service. Planner's choice; PVC-Job is more direct and asserts the same repo the push reads.
- **Task branch names:** `pkggit.TaskBranchName(string(task.UID))` → `tide/wt-<uid>` — resolvable in-test from the Task CR.

## State of the Art (repo-internal)

| Old Approach (current HEAD) | Current Approach (this phase) | When Changed | Impact |
|--------------|------------------|--------------|--------|
| Wave loop `k < len(layers)-1` — final wave never integrates | Loop over every boundary incl. final | Phase 34 | Single-wave plans integrate; Plan=Succeeded implies integrated |
| All boundary pushes carry nil branches (plan-level call at :770 fires pre-Tasks; project-level omits the field) | Cumulative Succeeded set computed inside the shared trigger + dispatch | Phase 34 | Whichever trigger wins the create race integrates identically |
| Wave-Job failure → instant Plan Failed | Bounded retry; conflicts exempt (classify → park/fail) | Phase 34 | Transient infra self-heals; defective plans fail fast with named branches |
| Push fires unverified | integrate → verify (`merge-base --is-ancestor`) → push, atomic under flock | Phase 34 | Incomplete run branch cannot ship; miss → sticky condition after cap |
| `lastPushedSHA` never stamped (lease anchor always empty) | Stamped from envelope HeadSHA in the success arm | Phase 34 | force-with-lease fence armed for the first time |

**Deprecated/outdated:** none removed; `ReasonWaveIntegrationFailed` semantics narrow (terminal only after retry cap / conflict classification).

## Assumptions Log

| # | Claim | Section | Risk if Wrong |
|---|-------|---------|---------------|
| A1 | flock(2) is effective on the production PVC filesystems (kind local-path, minikube hostpath — node-local; RWO pins writers to one node) | Pattern 3 | Low — flock is belt-and-braces; D-02 control-plane gate is the primary serialization |
| A2 | CSV branch-list args stay within container arg limits for v1 scale (~45 B/branch; 500 tasks ≈ 23 KB) | Alternatives | Low — D-07 pre-authorizes file/ConfigMap fallback if a project ever exceeds it |
| A3 | The exact live mechanism that integrated task 02 but not 03 in the observed run is not fully reconstructed from code alone (global-wave topology + trigger timing; evidence on the minikube PVC) | Summary / Test Harness | Low for planning — CONTEXT already mandates covering both mechanisms independently; the two repro cases don't depend on resolving it |
| A4 | Termination-message 4096-byte cap comfortably fits the envelope + truncated missing-branch list (first 10 + count ≈ <700 B) | Pattern 4 | Low — enforce truncation in tide-push; `FallbackToLogsOnError` already set |

## Open Questions

1. **Where does the wave-integration attempts counter live?**
   - What we know: #13b precedent puts retry state in status (`BoundaryPushStatus`); any new Plan.Status field needs v1alpha1+v1alpha2 parity.
   - What's unclear: status field vs Plan annotation vs Job-recreation counting.
   - Recommendation: mirror #13b with a small status struct on Plan (both API versions); it survives restarts and is operator-visible. Budget the two-version touch in the plan.
2. **Are mid-run (pre-Complete) boundary pushes observed at all — success (SHA stamp) AND failure (condition + retry)?**
   - What we know: `reconcileBoundaryPush` — the only push-Job completion observer / the whole #13b machine — is called solely from the Complete fast-path (project_controller.go:504). Mid-run pushes created via `triggerBoundaryPush` (boundary_push.go:201/206/222) complete AND fail unobserved, then TTL away after 300s. Both arms are affected: on success, the SHA is lost (stale/empty lease anchor for subsequent pushes); on failure, a mid-run gate miss produces **no `integration-incomplete` condition and no D-08 bounded retry** until the project reaches Complete or the next level trigger recreates the deterministic-name Job. The in-Job verify (D-06) still blocks the push itself at all four levels, so no incomplete branch ships — the gap is observability + remediation, not integrity.
   - What's unclear: whether SC-3/D-08 semantics ("miss → typed reason → bounded retry → sticky condition after cap") are required for mid-run pushes, or only for the project-boundary push preceding Complete (SC-4's "after a successful boundary push" is generic).
   - Recommendation: the plan must make ONE explicit scope decision covering both consequences — either (a) minimum bar: condition/retry/stamp machinery lives only in the Complete-path arm (a mid-run miss surfaces at Complete; the in-Job gate still protects mid-run pushes), or (b) extend ProjectReconciler to observe any `tide-push-<project.UID>` Job terminal state via the existing Owns(Job) event flow, handling both the success arm (SHA stamp) and the failure arm (condition + D-08 retry). This research already recommends (b)-lite for success; the decision must extend to the failure arm rather than defaulting silently. Flag the choice explicitly in the plan.
3. **Do wave-integration Jobs carry the cumulative set or just wave-k branches?**
   - What we know: D-01 says the *boundary-push* Job carries the cumulative set; INTEG-02 says "cumulative Succeeded-branch set" for integration generally; re-merge is idempotent so cumulative is safe.
   - Recommendation: cumulative everywhere (one helper, one behavior, self-healing per D-01's defense-in-depth intent).

## Environment Availability

| Dependency | Required By | Available | Version | Fallback |
|------------|------------|-----------|---------|----------|
| Docker | kind cluster, image builds | ✓ | server 29.5.3 | — |
| git | scratch verification, local ops | ✓ | 2.54.0 | — |
| **go** | build, `make test*` | **✗ (not on host PATH — the only missing toolchain piece)** | — | Devcontainer (`.devcontainer/devcontainer.json`: golang image + docker-in-docker) or `brew install go` (go.mod requires go 1.26.0). `make test*` still fails without it |
| kind | Layer B suite (`make test-int`) | ✓ | v0.32.0 (`/opt/homebrew/bin/kind`) | — installed v0.32 vs the repo's pinned v0.31 (STACK.md); pin node image by `@sha256` per STACK rules so the binary-version skew doesn't change Layer B node images |
| kubectl | suite helpers, evidence inspection | ✓ | Client v1.36.1 (`/opt/homebrew/bin/kubectl`) | — |
| minikube container | perishable repro evidence (`tide-projects` PVC) | ✓ running (Up 20h) | kicbase v0.0.50 | — **do not delete**; the kind repro (INTEG-05) reduces dependence on it |

**Missing dependencies with no fallback:** none.
**Missing dependencies with fallback (planner MUST address):** `go` is the only missing toolchain piece on this Mac host's non-interactive PATH (verified: `command -v go` fails; kind v0.32.0 and kubectl v1.36.1 ARE present via homebrew at `/opt/homebrew/bin` — an earlier probe of this table was wrong). Executor tasks that run `make test`, `make test-int-fast`, or `make test-int` will still fail on the host shell without go. The plan needs an explicit execution-surface decision: (a) `brew install go` (go 1.26) on the host — kind/kubectl already satisfied, or (b) run builds/tests inside the devcontainer (`golang` image, docker-in-docker feature, `--privileged`) — untested this session.

**Planner directive — Task 0 toolchain gate:** the plan MUST include a Task 0 that establishes and *verifies* the execution surface before any test task runs: smoke `go build ./...` + `kind version` + `kubectl version --client` on the chosen surface (host after go install, or devcontainer bring-up). Every Wave 0 test gap — including the INTEG-05 RED repro the phase's verification hinges on — gates on this task. Note the installed kind v0.32 vs the repo's pinned v0.31 in case the pin matters for Layer B (node images are pinned by sha regardless). Prior milestones' notes reference a ~7.65 GiB Docker VM for heavy runs — carry the constrained-VM recipe (fresh kind cluster per heavy run, one heavy run at a time) into the kind-suite tasks.

## Validation Architecture

### Test Framework
| Property | Value |
|----------|-------|
| Framework | Ginkgo v2 + Gomega (Layer A envtest, Layer B kind); plain go-test units alongside |
| Config file | Makefile targets (no ginkgo config file); kind cluster spec `test/integration/kind/cluster.yaml` |
| Quick run command | `go test ./internal/controller/ -short -timeout 5m` (unit tier; needs go on PATH / devcontainer) |
| Full suite command | `make test-int` (Layer A envtest + Layer B kind; requires Docker + kind + `make test-int-kind-prep` images) |

### Phase Requirements → Test Map
| Req ID | Behavior | Test Type | Automated Command | File Exists? |
|--------|----------|-----------|-------------------|-------------|
| INTEG-01 | Final-wave/single-wave integration Jobs dispatch | unit/envtest + kind | `go test ./internal/controller/ -run 'TestPlan.*Wave' ` + kind spec | ❌ Wave 0 (new specs) |
| INTEG-02 | Serialized, idempotent merges under 2-parallel tasks | kind | `go test ./test/integration/kind -ginkgo.label-filter=kind -ginkgo.focus='integration miss'` | ❌ Wave 0 (`integration_miss_test.go`) |
| INTEG-03 | Verify gate: miss → typed envelope + sticky condition | unit (tide-push `run()` seam) + kind | `go test ./cmd/tide-push/ -run TestRunPush` (existing `main_test.go` drives `run(cfg)` in-process) | partial — `cmd/tide-push/main_test.go` exists; verify cases ❌ Wave 0 |
| INTEG-04 | `lastPushedSHA` stamped in success arm | envtest | `go test ./internal/controller/ -run '.*BoundaryPush.*'` | ❌ Wave 0 (extend existing boundary-push envtest specs) |
| INTEG-05 | RED repro of the observed miss, locks fix | kind | focused ginkgo run of the new spec file | ❌ Wave 0 |

### Sampling Rate
- **Per task commit:** package-scoped `go test ./<changed-pkg>/ -short` (controller / cmd/tide-push / pkg/git)
- **Per wave merge:** `make test-int-fast` (Layer A envtest, no Docker)
- **Phase gate:** `make test-int` green (read `MAKE_EXIT` AND `grep -nE '^--- FAIL|^FAIL\s'` the log — Ginkgo summary alone is insufficient per CLAUDE.md)

### Wave 0 Gaps
- [ ] `test/integration/kind/integration_miss_test.go` — INTEG-05 (both repro cases; RED against pre-fix code first)
- [ ] tide-push verify/conflict unit cases in `cmd/tide-push/main_test.go` — INTEG-03/D-09 (in-process `run(cfg)` seam already exists)
- [ ] envtest specs for cumulative-set helper, D-02 gate, `LastPushedSHA` stamp, condition arms — INTEG-01/02/04
- [ ] `pkg/git` conflict-abort + idempotent re-merge units (scratch-repo style; pure git, no cluster)
- [ ] Task 0 toolchain gate: `go` on the execution surface (kind/kubectl already on host — see Environment Availability), verified via smoke `go build ./...` + `kind version` — blocking prerequisite for ALL of the above

## Security Domain

`security_enforcement` not configured (absent = enabled). Scope is narrow — no new inputs, no new network surfaces, no crypto.

### Applicable ASVS Categories

| ASVS Category | Applies | Standard Control |
|---------------|---------|-----------------|
| V2 Authentication | no (unchanged) | GIT_PAT stays Job-pod-only via envFrom (D-B1 trust boundary — preserve) |
| V3 Session Management | no | — |
| V4 Access Control | yes (minor) | New Job labels must not widen RBAC; assertion Jobs in tests reuse existing SAs (`tide-push`/`tide-subagent`) |
| V5 Input Validation | yes | Branch names flow controller→args: they are controller-generated (`tide/wt-<uid>`), never user input; keep it that way — do not accept branch lists from CRD spec |
| V6 Cryptography | no | Signing descoped (SIGN-02..04 deferred) |

### Known Threat Patterns for this stack

| Pattern | STRIDE | Standard Mitigation |
|---------|--------|---------------------|
| PAT leakage via new error paths (verify/conflict output) | Information disclosure | Route ALL new stderr/envelope text through `redactPAT` (main.go:774) — merge/verify output can embed remote URLs on some failure modes |
| Secret in condition message (missing-branch list) | Information disclosure | Condition messages carry branch names + task names only — never diff content (gitleaks scan remains the diff gate) |
| Envelope spoofing via shared PVC path | Tampering | Prefer the termination-log surface (pod-scoped, API-server-mediated) over the PVC JSON for controller decisions (already the pattern; Pitfall 2 reinforces) |
| Un-pushed incomplete branch shipped | Integrity (the phase's raison d'être) | D-05/D-06 gate; verdict recomputed from git |

## Sources

### Primary (HIGH confidence — read/executed in-session)
- `internal/controller/plan_controller.go` (:981–1221, :720–830) — wave-boundary machinery, skip loop, trigger call
- `internal/controller/boundary_push.go` (full) — shared trigger, wave-Job dispatch, per-level entry points
- `internal/controller/project_controller.go` (:55–140, :440–540, :660–900) — #13b state machine, envelope reader, dispatch, doc-comment :472
- `internal/controller/push_helpers.go` (full) — PushOptions, buildPushJob (labels absent, BackoffLimit 2, TTL 300)
- `pkg/git/integrate.go` (full) — merge engine, no locking, no abort
- `cmd/tide-push/main.go` (full) — modes, envelope, classification, integration-only early exit
- `cmd/tide/resume.go` (full) — D-13 extension surface
- `internal/controller/dispatch_helpers.go` (:293–329) — D3 count-gate template
- `api/v1alpha2/{shared_types,project_types,plan_types}.go`, `api/v1alpha1/*` — conditions, BoundaryPushStatus, IntegratedThroughWave parity
- `internal/metrics/registry.go` (:173) — PushJobsTotal
- `test/integration/kind/{wave,medium_http,push_lease,chaos_resume,fixtures}_test.go` — harness ingredients
- Scratch-repo git experiments (git 2.54.0, this session): idempotent re-merge, `--is-ancestor` exit codes incl. empty-diff case, conflict output shape, `merge --abort`
- Host probes (re-verified 2026-07-04): `go` absent from PATH; kind v0.32.0 + kubectl v1.36.1 present at `/opt/homebrew/bin`; Docker 29.5.3; minikube container running; `.devcontainer/devcontainer.json`

### Secondary (MEDIUM)
- `.planning/todos/pending/2026-07-03-wave-parallel-integration-miss.md` — observed-run evidence framing
- `.planning/STATE.md` accumulated context — binding constraints, perishable-evidence warning

### Tertiary (LOW)
- none (no web research required — domain is fully in-repo; git semantics verified locally instead of cited)

## Metadata

**Confidence breakdown:**
- Fix surface / current behavior: HIGH — every cited line read this session; negative claims (no LastPushedSHA assignment, no Job labels, no flock usage) verified by grep
- Git semantics (idempotent re-merge, is-ancestor, conflict shape): HIGH — executed in a scratch repo on git 2.54
- Test harness shape: HIGH — all fixture patterns located in the existing suite
- Environment: HIGH after re-verification (initial probe wrongly marked kind/kubectl absent; corrected 2026-07-04 — only `go` is missing on the host); MEDIUM for the devcontainer remedy (untested this session)
- flock-on-PVC effectiveness: MEDIUM (A1 — by design belt-and-braces only)

**Research date:** 2026-07-04
**Valid until:** ~2026-08-03 (stable in-repo domain; re-verify line numbers after any merge to `main` touching the cited files)
