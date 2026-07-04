# Architecture Research — v1.0.7 First-Run Paper Cuts (Integration Architecture)

**Domain:** Kubernetes operator (TIDE) — brownfield integration of run-integrity + git-ergonomics + dashboard-visibility features into the shipped v1.0.6 codebase
**Researched:** 2026-07-03
**Confidence:** HIGH (every claim below is grounded in direct reads of the v1.0.6 source at the cited file:line; the one MEDIUM-confidence item — the exact race mechanism of the observed miss — is flagged explicitly and needs the repro the todo already calls for)

## Existing Architecture (as-built, relevant slice)

```
┌──────────────────────────── manager pod ────────────────────────────┐
│ ProjectReconciler          PlanReconciler           Phase/Milestone │
│  · clone Job dispatch       · wave gate (interior    reconcilers    │
│  · boundary-push state        boundaries only)       · level push   │
│    machine (Complete-only)  · plan-boundary push       trigger      │
│  · reads push envelope        (nil branches!)                       │
│    (FAILED arm only)                                                │
└───────┬──────────────────────────┬──────────────────────┬───────────┘
        │ creates Jobs             │ creates Jobs         │
┌───────▼───────┐  ┌───────────────▼─────────┐  ┌─────────▼──────────┐
│ tide-clone-   │  │ tide-push-wave-         │  │ tide-push-         │
│ <proj.UID>    │  │ <plan.UID>-<k>          │  │ <proj.UID>         │
│ clone + Ensure│  │ merge wave-k branches   │  │ integrate (nil) +  │
│ RunBranch@HEAD│  │ into run branch, local  │  │ commit-skip + push │
└───────┬───────┘  └───────────────┬─────────┘  └─────────┬──────────┘
        │                          │                      │
        └───────────┬──────────────┴──────────────────────┘
                    ▼  all mount the SAME RWO PVC subPath <uid>/workspace
   /workspace/repo.git                    (shared bare repo)
   /workspace/worktrees/run-<branch>/     (ONE shared integration+push worktree)
   /workspace/worktrees/<task-uid>/       (per-task worktrees, executor commits)
   /workspace/envelopes/<cr-uid>/         (planner artifacts: *.md, children/*.json, out.json)
```

Key as-built facts the new features must integrate with:

| Fact | Where | Consequence |
|------|-------|-------------|
| Task branches `tide/wt-<uid>` fork from run branch via `git worktree add -b` | `pkg/git/worktree.go:59` (called from `internal/harness/worktree.go:31`) | Executor commits land in the shared bare repo's object DB directly; no push step |
| Integration = `git merge --no-ff` loops inside tide-push Jobs | `pkg/git/integrate.go:60`, invoked from `cmd/tide-push/main.go:361` | All merges run in the ONE shared worktree `worktrees/run-<branch>/` |
| Interior wave boundaries gated per plan: Job `tide-push-wave-<plan.UID>-<k>`, `Status.IntegratedThroughWave` ladder | `internal/controller/plan_controller.go:986-1078` | Serialized within a plan; NOT across plans |
| Boundary pushes are commit-skip pushes of run-branch HEAD | `cmd/tide-push/main.go:488-528` (`worktreeClean` → push existing HEAD) | A push "succeeding" says nothing about integration completeness |
| Push result envelope (incl. `HeadSHA`) written to `/dev/termination-log` + PVC | `cmd/tide-push/main.go:662-703` | The controller CAN read HeadSHA without mounting the PVC — it just doesn't on success |
| Manager/dashboard cannot mount per-project RWO PVCs | dashboard is a chi `manager.Runnable` (`cmd/dashboard/main.go:162`, `router.go:110`) | Artifact view needs an in-namespace transport |
| `tide artifact-get` already implements an inspector-pod PVC reader | `cmd/tide/artifact_get_run.go:154-283` | Precedent for reader-pod transport exists — CLI-side, not dashboard-side |

## Q1 — Where the wave-parallel integrate race lives

Three concrete defect surfaces, in decreasing certainty. The fix design below covers all three; the repro test should discriminate which one bit the 2026-07-03 run.

### 1a. CONFIRMED structural gap: the final wave of every plan is never integrated

- `reconcileWaveBoundary` iterates `for k := 0; k < len(layers)-1; k++` — comment says it outright: *"Skip the last wave (no k+1 to gate on)"* (`plan_controller.go:1192-1193`). The final wave's branches get no `tide-push-wave-*` Job.
- The only code that would merge them at the plan boundary — the D-04 branch-collection loop in `PlanReconciler.maybeTriggerBoundaryPush` (`boundary_push.go:215-223`) — is **effectively dead code**: its sole caller passes `taskItems=nil` (`plan_controller.go:770`), and it fires at *planner-Job completion*, before Tasks even exist. The CR-03 comment at `plan_controller.go:757-766` documents this as a known deferred defect ("firing the push from a separate seam in the wave-materialization path on task-status updates — out of REVIEW-FIX scope").
- Phase/milestone/project boundary pushes pass no branches either (`boundary_push.go:200-207`, `project_controller.go:849-855`). Net: **any task in a plan's last Kahn layer relies on nothing to reach the run branch.** For a single-wave plan, NO task is ever integrated. This has been true since the gate was born (commit `7eb78d2`, Plan 11-03) and the kind suite never asserts run-branch content (`grep 'tide: integrate' test/integration/kind/` → zero hits).
- Fit to the observed miss: with layers `[[01,02],[03]]` this predicts exactly "integrate commits for 01 and 02, task 03 missing." The todo recalls 02+03 as the parallel pair — layer layout needs the repro to pin (MEDIUM confidence on attribution, HIGH on the gap itself).

### 1b. Job-identity hazard: the branch set is not part of the integration Job's identity

`tide-push-wave-<plan.UID>-<waveIndex>` is the idempotency key; the `--integrate-task-branches` CSV is frozen at first dispatch (`boundary_push.go:164-172`). If a Job is ever created against a stale informer-cache task snapshot, later reconciles see the Job exists → stamp `IntegratedThroughWave` on its success (`plan_controller.go:1038-1046`) → any branch missing from the original CSV is dropped **silently and permanently**. Nothing ever re-checks the set.

### 1c. Cross-plan / cross-Job concurrency on the shared worktree

Within a plan, the `IntegratedThroughWave` ladder serializes. Across the project it does not: two plans in the same global wave finishing near-simultaneously produce two concurrent `tide-push-wave-<planA/B.UID>-*` Jobs, and a level boundary push (`tide-push-<project.UID>`) can run concurrently with either. All of them mutate the same `worktrees/run-<branch>/` checkout and the same run-branch ref. Concurrent `git merge`/`commit` in one worktree is a lost-update surface (index.lock contention at best; ref read-merge-write interleaving at worst). Note the RWO PVC forces all these pods onto **one node** — which makes a filesystem `flock` a sound serialization primitive here.

### Serialization/retry recommendation (per-task-serialized vs retry-on-lost-merge vs merge queue)

**Recommendation: keep the per-wave Job model, add (i) final-wave coverage, (ii) idempotent full-set re-merge + post-merge ancestry verification inside tide-push, (iii) a per-workspace flock. Do NOT build a merge queue.**

- *Per-task serialized integrate* — unnecessary granularity; merges within one Job are already sequential in-process. The gap is between Jobs, not between tasks.
- *Merge queue* (single long-lived integrator per project) — a new component + queue state that violates the "resumption state is minimal" constraint; overkill for one-human-one-run scale.
- *Retry-on-lost-merge* — fits the reconcile model exactly: level-triggered, idempotent, verifiable. `git merge --no-ff` of an already-merged branch is an "Already up to date" no-op (exit 0, no commit), so passing the **full** set of Succeeded-task branches at every integration point converges. Verification is `git merge-base --is-ancestor <branch> HEAD` per branch after the merge loop; any failure → new exit code (e.g. `14` / reason `integration-incomplete`) → the reconciler's existing Failed-Job arms retry (wave gate: `plan_controller.go:1031-1037`; boundary push: bounded retry in `reconcileBoundaryPush`).

Concrete changes (all MODIFY, no new components):

| Change | Site |
|--------|------|
| Extend the wave loop to `k < len(layers)` so the final wave gets a `tide-push-wave-<plan.UID>-<len(layers)>` Job, and gate `patchPlanSucceeded` on it | `plan_controller.go:1192-1214` |
| Pass all-Succeeded branches (cumulative, not just wave-k) into each integration Job; merges are idempotent so this self-heals 1b | `plan_controller.go:1063-1069`, `boundary_push.go:146-176` |
| `flock` on `<workspace>/integrate.lock` around the merge loop | `pkg/git/integrate.go:60` (new guard at top) |
| Post-merge `--is-ancestor` verification + exit 14 | `pkg/git/integrate.go` (new verify step), `cmd/tide-push/main.go` exit-code map |

## Q2 — Completeness gate placement + lastPushedSHA stamping

### Completeness gate: two seams, upstream-prevent + downstream-detect

The manager cannot run git against the PVC (it can't mount it), so **verification must execute inside a Job**; the controller's job is to pass the expected set and interpret the exit code.

1. **Plan level (prevent):** plan `Succeeded` is only stamped after the final-wave integration Job (which now merges + verifies every Succeeded task branch of the plan) succeeds. This is the Q1 fix — same seam.
2. **Project boundary push (detect):** `runPush` gains `--verify-task-branches=<CSV>` — verify-only ancestry check of every Succeeded Task's `tide/wt-<uid>` branch against run-branch HEAD *before* pushing. The ProjectReconciler collects the set with a cheap `List` of Tasks by the `tideproject.k8s/project` label filtered to `Status.Phase=="Succeeded"`, in `dispatchBoundaryPush` (`project_controller.go:842-856`). On exit 14 the existing envelope-classification arm (`project_controller.go:753-824`) maps `integration-incomplete` to a **new sticky condition** (mirror the leak/lease operator-recovery arms) — the run branch never ships incomplete again, and the miss is loud.
   - Respect the #13b decision: `Complete` stays a control-plane rollup and is **not** gated on the push (`project_controller.go:637-641` documents this deliberately). The trust signal is `BoundaryPushed=True` + the new verification — gate the *push*, not `Complete`.
   - No empty-diff marker needed: `CommitWorktree` already fails empty-diff tasks (`internal/harness/commit.go:37-47`), so every Succeeded task is guaranteed a branch with a real commit.

### lastPushedSHA: CONFIRMED missing stamp, exact location

`reconcileBoundaryPush`'s success arm (`project_controller.go:734-749`) sets `BoundaryPushed=True` and clears `LeaseFailureCount` — but **never calls `readPushEnvelope`**; that helper is only invoked in the `isJobFailed` arm (`:754`). The envelope's `HeadSHA` exists precisely for this (design comment at `project_controller.go:472-473`: "on success, patch Status.Git.LastPushedSHA" — never implemented). Fix: in the success arm, call the existing `readPushEnvelope(ctx, ns, pushJobName)` and add `project.Status.Git.LastPushedSHA = env.HeadSHA` to the same status patch.

Side effect worth noting in the plan: today every push runs with an empty lease anchor (`--last-pushed-sha=""` → `pkg/git/push.go:70` omits force-with-lease entirely), so the D-B6 external-mutation fence has been inert. Stamping fixes the fence too. Also note mid-run level pushes (`triggerBoundaryPush` from plan/phase/milestone) are fire-and-forget — nothing observes their envelopes; acceptable for v1.0.7 if the project-boundary stamp lands, but the plan should state that scope choice.

## Q3 — baseRef plumbing

Chain (matches the todo; all sites verified):

```
Project.spec.git.baseRef                    api/v1alpha2/project_types.go:205 (GitConfig — NEW optional field, CEL: optional string)
  → ProjectReconciler clone dispatch        project_controller.go:571-580 (CloneOptions gains BaseRef)
  → buildCloneJob args --base-ref=<ref>     push_helpers.go:292-303
  → runClone                                cmd/tide-push/main.go:259-326
  → EnsureRunBranch(bareRepo, branch, ref)  pkg/git/branch.go:40 (resolve refs/heads/<ref> → refs/tags/<ref> (peel) → SHA via CommitObject; today it hard-codes repo.Head() at :58)
```

- Fetch coverage: `Clone` is a full bare `PlainCloneContext(bare=true)` with no depth (`pkg/git/clone.go:45`) — all heads + tags arrive, so branch/tag refs need no CloneOptions change; an arbitrary SHA resolves iff reachable from a fetched ref (document that limit).
- Failure surface: unresolvable ref → exit 2 invariant with a distinct reason written to the termination log (clone-mode currently writes **no envelope** — add one, or reuse `TerminationMessageFallbackToLogsOnError` which `buildCloneJob` doesn't set today). The ProjectReconciler's terminal-failed clone arm (`project_controller.go:610-628`) currently deletes + re-dispatches ANY failed clone forever-ish — it must classify: `baseref-unresolvable` → permanent condition (e.g. `CloneFailed/BaseRefUnresolvable`), no re-dispatch loop. That satisfies the todo's "clear condition rather than a cryptic worktree-add failure."
- Chart: CRD schema addition rides a chart version bump (FIXED-contract rule); no values change needed beyond the CRD.

## Q4 — Signing at the three commit sites + Secret plumbing

The three sites split by mechanism, which drives the design:

| Site | Mechanism today | go-git-native signing possible? |
|------|-----------------|--------------------------------|
| Executor task commit — `internal/harness/commit.go:40-82` | shells out `git commit` | Would require rewriting to go-git `Worktree.Commit` |
| Integrate merge commits — `pkg/git/integrate.go:94-107` | shells out `git merge --no-ff` | No — go-git can't create three-way merges (the D-01 reason the CLI is used) |
| Boundary commit — `cmd/tide-push/main.go:521` via `pkggit.Commit` | go-git `Worktree.Commit` | Yes — `CommitOptions.SignKey` (`*openpgp.Entity`, ProtonMail/go-crypto) |

**Recommendation: one pure-Go gpg-shim, uniform across all three sites.** Because the merge site *cannot* move to go-git, per-site go-git signing can never cover everything; a mixed approach (SignKey at site 3, shim at 1–2) is two code paths for one feature. Instead ship a tiny `tide-signer` binary (or subcommand of the existing harness/tide-push binaries — both images already exist) that implements git's `gpg.program` interface (sign: read payload on stdin → armored detached sig on stdout → `[GNUPG:] SIG_CREATED` on the status fd) using ProtonMail/go-crypto — no gpg binary in images, satisfying the todo. Each git invocation gains, when `GIT_SIGNING_KEY` is present:
`-c gpg.program=tide-signer -c commit.gpgsign=true -c user.signingkey=<key-id>` (git merge respects `commit.gpgsign` for merge commits). Site 3 can use go-git `SignKey` directly OR route through the same env — pick one in planning; `SignKey` is less code for that site.

Secret + identity plumbing (all MODIFY):

- `GitConfig.SigningKeySecretRef` (new optional field next to `CredsSecretRef`, `project_types.go:214`), data key `GIT_SIGNING_KEY` = armored private key. Absent = current unsigned behavior.
- Env injection at the three pod-spec builders: executor Jobs (`internal/dispatch/podjob/jobspec.go:357` subagentEnv), push/integration Jobs (`push_helpers.go` `buildPushJob` — wave-integration Jobs reuse it, so one change covers merge + boundary sites).
- Bot identity unification: `cmd/tide-push/main.go:131-137` `tideBotSignature()` hardcodes name/email — switch to the same `TIDE_BOT_NAME`/`TIDE_BOT_EMAIL` env fallback the other two sites use (`commit.go:56-63`, `integrate.go:85-92`), and surface both in chart values + optionally Project spec. **Committer email must match a verified email on the machine account holding the public key** — that's an operator-docs requirement (docs/project-authoring.md), not code.
- Chart bump carries: CRD field, values for bot identity, env wiring in any chart-templated pod specs.

## Q5 — promptFile resolution point

**Recommendation: CLI-side inline in `cmd/tide/apply.go` (a `--outcome-prompt-file` flag or sibling-file convention resolved in `runApply` after decode, before the SSA patch at `apply.go:94`).**

| | CLI-side inline | Controller-side ConfigMap keyRef |
|---|---|---|
| Schema change | none (spec.outcomePrompt already a plain string, `project_types.go:345`) | new `outcomePromptFrom.configMapKeyRef` + CEL oneOf vs `outcomePrompt` |
| RBAC | none | planner-dispatch path needs ConfigMap read |
| Staleness | prompt frozen at apply (desirable — the prompt is the run's contract) | CM edits mid-run create ambiguity |
| Dashboard project view (also in this milestone) | reads `spec.outcomePrompt` directly — free | must resolve the ref too |
| Works via bare kubectl | no (feature is CLI-only) | yes |
| Chart impact | none | CRD bump |

`runApply` decodes to `unstructured.Unstructured` (`apply.go:69-76`), so the injection is: detect `kind: Project` + flag present → `unstructured.SetNestedField(obj.Object, string(fileBytes), "spec", "outcomePrompt")`. Guard prompt size (etcd 1.5 MiB object cap; warn well below). The kubectl-parity gap is acceptable for v1.0.7 and reversible — the ConfigMap route can be added later without breaking the inline path.

## Q6 — Artifact-view transport (the approve-gate review surface)

Ground truth: artifacts exist ONLY at `/workspace/<project-uid>/workspace/envelopes/<cr-uid>/` (`internal/harness/envelope_io.go:31,115`) on the RWO per-project PVC. The reporter Job already mounts exactly that subPath in-namespace (`reporter_jobspec.go:205`) at the exact moment the artifacts are fresh, and gate-at-descent parks the parent *after* the planner writes artifacts and the reporter materializes children — so reporter-time is at-or-before every review moment.

| Option | Timeliness at approve gate | New moving parts | Posture cost |
|--------|---------------------------|------------------|--------------|
| **A. On-demand reader Job from the dashboard** (mirror `tide artifact-get`'s inspector pod, `artifact_get_run.go:154`) | OK but 5–15 s pod-startup latency per click | manager creates pods in every project namespace | **Violates the read-only-dashboard decision** (all mutations via CLI/kubectl); RBAC expansion (pods create/delete across project namespaces) |
| **B. Persist to ConfigMaps at reporter-materialization time** ✅ | Available the instant the gate parks | reporter writes CMs; dashboard reads via K8s API it already has | reporter SA gains configmaps create/update (namespace-scoped); bounded etcd growth |
| C. Commit artifacts to the run branch at boundary push | **Too late** — boundary push fires only after the gate passes; useless for the review moment | git-host linking UI | disqualified for this feature (independently nice someday) |

**Recommendation: B.** Concretely: in the reporter flow (`cmd/tide-reporter` / `internal/reporter/materialize.go`), after reading `out.json`, write one ConfigMap per CR UID (`tide-artifacts-<cr-uid>`), keys = artifact filenames (`MILESTONE.md`, `children.json`, `out.json` summary), labels `tideproject.k8s/artifact-of: <cr-uid>` + project label, owner-ref to the CR (GC for free), size-capped per key + total (~512 KiB / 1 MiB) with a `truncated` annotation — the reporter already has the etcd-guard idiom (`maxSharedContextBytes`, `materialize.go:48-56`). Dashboard: new chi routes on `cmd/dashboard/router.go` (`GET /api/.../artifacts/<cr-uid>`), served from the informer cache; UI reuses the log-drawer surface (fix its empty-state handling first/together per the companion todo), markdown-render `*.md`, pretty-print JSON, and surface the artifact front-and-center on gate-parked nodes next to the approve action. Not a PERSIST-02 violation: these are source documents, not derived schedules — but say so in the plan and keep the caps.

## Q7 — Suggested build order

Ordering by dependency + risk-retirement (headline first, chart changes batched):

1. **lastPushedSHA stamp** — tiny, confirmed, zero deps; also arms force-with-lease for everything after (`project_controller.go` success arm).
2. **Integration-miss gate** (headline): (a) kind-suite repro — 2-parallel-task **final** wave asserting `tide: integrate` merge commits + run-branch ancestry; (b) final-wave coverage + gate `patchPlanSucceeded`; (c) tide-push full-set idempotent merge + flock + `--is-ancestor` verify + exit 14; (d) project-boundary `--verify-task-branches` + sticky `integration-incomplete` condition. Touches `tide-push`, `pkg/git`, plan/project reconcilers — land before signing so the merge code is stable under it.
3. **baseRef** — independent, small, clone-path-only; first CRD schema change of the milestone.
4. **Signed commits + bot identity** — touches all three commit sites and the pod-spec builders step 2 just stabilized; second CRD/values change. Batch the chart version bump to cover 3+4.
5. **promptFile** — CLI-only, independent; any time after planning settles the route.
6. **Artifact view** — reporter ConfigMap persistence → dashboard API routes → drawer UI (sequence within: log-drawer empty-state fix, then artifact drawer). Independent of 1–5; UI last because it consumes the CM contract.
7. **Pricing table, Prometheus setup step, v1.0.6 tech-debt carry** — fully independent; slot anywhere as filler waves.

## Anti-Patterns (project-specific, for the fix work)

- **Don't gate `Complete` on the push.** Debug #13b deliberately decoupled them (`project_controller.go:486-495`); gate the push and the plan-Succeeded seam instead.
- **Don't cache "integrated" as a branch list in `.status`.** `IntegratedThroughWave` (an int watermark) + re-derivable ancestry checks stay within the minimal-resumption-state constraint; a stored branch set would be a PERSIST-02 smell.
- **Don't reach for a merge-queue component or per-task Jobs.** The Job-per-boundary model + idempotent re-merge + flock converges; new long-lived components fight the reconcile model.
- **Don't sign via a gpg binary in images** (todo constraint) and don't try go-git for merge commits (`ErrUnsupportedMergeStrategy` — the documented D-01 reason `integrate.go` shells out).
- **Don't have the dashboard create pods.** Read-only dashboard is a logged Key Decision; option A quietly breaks it.
- **Don't edit `charts/tide/values.yaml` outside the batched chart bump.** Chart is the FIXED contract; binary catches up to chart.

## Sources

Primary (code read directly, v1.0.6 @ `main` 9344358):
- `pkg/git/integrate.go`, `pkg/git/worktree.go`, `pkg/git/branch.go`, `pkg/git/clone.go`, `pkg/git/push.go`
- `internal/harness/commit.go`, `internal/harness/envelope_io.go`
- `internal/controller/plan_controller.go` (:690-790, :981-1220), `boundary_push.go`, `push_helpers.go`, `project_controller.go` (:454-930), `reporter_jobspec.go`
- `cmd/tide-push/main.go`, `cmd/tide/apply.go`, `cmd/tide/artifact_get_run.go`, `cmd/dashboard/{main,router}.go`
- `api/v1alpha2/project_types.go` (:202-360), `internal/reporter/materialize.go`, `internal/dispatch/podjob/jobspec.go`
- git history: `7eb78d2` (wave gate born with final-wave exclusion), `69c1c78` (plan boundary push wiring)
- `.planning/todos/pending/2026-07-03-*.md` (4 todos), `.planning/STATE.md` (run evidence)

---
*Architecture research for: TIDE v1.0.7 First-Run Paper Cuts*
*Researched: 2026-07-03*
