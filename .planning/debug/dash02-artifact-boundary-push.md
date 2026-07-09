---
status: resolved
slug: dash02-artifact-boundary-push
trigger: on the DASH-02 artifact-vs-boundary push interaction
created: 2026-07-08
updated: 2026-07-08
phase: 37-dashboard-surfaces-artifact-view-project-view-log-drawer-sta
---

# Debug: DASH-02 artifact-vs-boundary push interaction

## SESSION OUTCOME (2026-07-08)

**CHARTERED BUG — RESOLVED & LAYER-B GREEN.** The `LastPushedSHA`-stays-empty /
boundary-push-never-lands RED was caused by a one-line TEST-FIXTURE gap: the kind suite
never overrode `images.tidePush.tag`, so the manager's `TIDE_PUSH_IMAGE` resolved to the
unloaded `.Chart.AppVersion` (1.0.7) instead of the kind-loaded `:test` tag → clone/push
pods ImagePullBackOff (phase=Pending) forever. Fix: `--set images.tidePush.tag=test` +
`pullPolicy=IfNotPresent` in `suite_test.go` (mirrors the existing tideImport override).
VERIFIED live: pods Succeed, `LastPushedSHA` advances (4dccc52d…), the project artifact
stages on the run branch. The 5 prior runs' resource-starvation / PVC / capture-wedge
hypotheses were all wrong — fixes 1bcbea6 + 01b5004 remain as real latent-bug hardening
(envtest-green), but NONE was the RED cause.

**DEFECT E — ROOT-CAUSED, HANDED OFF (not fixed this session, per user).** A separate,
narrower failure surfaced once the chartered bug was green: only the `project`-level
planning artifact stages on the run branch; milestone/phase/plan never do (test
Assertion 4 `HaveKey("milestone")` fails). Confirmed product bug in the D-B5/R-05 shared
single-flight coupling — an EARLY project artifact push snapshots a `[project]`-only
cumulative map and wins the shared `tide-push-<uid>` name; the later fuller-map pushes
(including the authoritative boundary push at Complete) are suppressed by single-flight
and the Job outlives the cascade. Full trace + recommended fix (Option A: boundary push
supersedes a stale artifact Job, mirroring run-5 defect-C2) in "Run-9" below. Test
Assertion 4 is correct per 37-06's cumulative-staging design — do NOT relax it. Handed to
a planned fix (touches the deliberately-preserved coupling).

**Test hardening applied this session (all in test/integration/kind/):** the tide-push
tag fix; sched/wait disambiguation in the push diagnostic; the materialization poll now
waits for the milestone level with per-tick level logging; unique-per-run kind-log export
dir (fixes the run-3 stale-shared-logs blocker).

## Symptoms

- **Expected:** 37-09's Layer-B spec `artifact_staging_test.go` passes — after a stub Project reaches `Complete`, the boundary push lands on the bare remote and `Status.Git.LastPushedSHA` advances (non-empty), and `.tide/planning/<kind>/<name>/*.md` artifacts appear on the run branch full-fidelity.
- **Actual:** The test FAILS. `Status.Git.LastPushedSHA` stays **empty**. The Phase-37 artifact push fires correctly, but the boundary push never records a pushed SHA. The test never even reaches the artifact-content assertions (it fails on the `LastPushedSHA` precondition first).
- **Error:** `artifact_staging_test.go:176 — Expected <string>: (empty) not to be empty` ("lastPushedSHA must have advanced — the boundary-push machinery landed at least one push").
- **Timeline:** This is the **first-ever execution** of this spec. It was authored by plan 37-09 but env-gated (a live minikube blocked a second kind cluster), so it had only ever been statically verified (`go test -c`), never run. It was RED on both of its first two runs.
- **Reproduction:** Stop minikube (single-node OOM constraint), then `make test-int-kind-prep` and `go test ./test/integration/kind/... -v -timeout=40m -ginkgo.v -ginkgo.flake-attempts=3 -ginkgo.focus="Planning artifacts land on the run branch"` with `TIDE_BINARY=$(pwd)/bin/tide`. Watch the real exit code (`KIND_EXIT`), not the script exit (MAKE_EXIT gotcha).

## Evidence (from two runs, already gathered)

- timestamp: run-1 — Controller log line: `{"msg":"triggered artifact push", ..., "level":"project", "job":"tide-push-<project-uid>", "envelopes":1}`. So **Phase-37's artifact-push trigger (37-06) provably fires** and creates a `tide-push-<project-uid>` Job with 1 envelope.
- timestamp: run-1 — Project reaches `Phase=Complete` in ~45s (stub cascade), run branch `tide/run-...` is created, `LeaseFailureCount==0`, `Phase != PushLeaseFailed`. Only `LastPushedSHA` is empty. Test's direct `Expect(LastPushedSHA).NotTo(BeEmpty())` fails immediately after `Complete` (all 3 flake-attempts, identical ~45s).
- timestamp: run-2 (with an investigative `Eventually` poll wrapping the push-outcome assertions, since reverted) — `LastPushedSHA` STILL never advanced over a 5-min poll; the poll then hit the suite `ctx` deadline (`suite_test.go:128` `ctx = context.WithTimeout(context.Background(), kindTestTimeout)`) → `client rate limiter Wait returned an error: context deadline exceeded`. Controller log for run-2 shows the artifact push firing but **no boundary-push / LastPushedSHA-patch activity** (only benign resourceVersion-conflict "object has been modified" retries).
- Kind logs (run-2) still on disk: `/var/folders/z3/hmsm0sjd6fbgwkzmwwzs_8vh0000gn/T/kind-logs-tide-test/` (control-plane pod + container logs, incl. `tide-controller-manager`). Run stdout: `/Users/justinsearles/.claude/jobs/f3720340/tmp/layerb.log` (run-1) and `layerb2.log` (run-2).
- `grep -rnE '\.LastPushedSHA\s*=' internal/` returns NOTHING — `LastPushedSHA` is only ever *read* (as the `--force-with-lease` anchor) in the controller. The write-back is described only in a comment at `project_controller.go:473-475` ("read the push-result envelope from PVC; on success, patch Status.Git.LastPushedSHA").

## Current Focus

CONFIRMED ROOT CAUSE (run-5 code trace, post fix 1bcbea6). Both defect A (tide-push
SA) and defect B (write-back exists at project_controller.go:748) landed, yet run-4
CLEANLY confirms LastPushedSHA never advances. Full end-to-end code trace of the
shared `tide-push-<project.UID>` Job lifecycle isolates the residual defect to the
CAPTURE path in `reconcileBoundaryPush`, NOT the write-back's absence.

reasoning_checkpoint:
  hypothesis: >
    The residual RED (LastPushedSHA stays empty even with the defect-B write-back) is
    caused by the boundary success-arm failing to DURABLY CAPTURE the shared Job's
    headSHA, then going TERMINAL. Two code defects, both downstream of the intentional
    D-B5/R-05 shared-Job-name coupling: (1) `readPushEnvelope`
    (project_controller.go:86) blindly reads `pods.Items[0]`, but a push Job has
    BackoffLimit:2 (push_helpers.go:237), so a transient first-attempt FAILED pod
    (envelope headSHA="") can be Items[0] and mask the SUCCEEDED pod's real headSHA.
    (2) the success arm (project_controller.go:735-760) sets BoundaryPushed=True
    TERMINALLY even when it captured NO headSHA; the terminal guard at line 678-681
    then returns early on every subsequent reconcile, freezing LastPushedSHA empty
    FOREVER. This is answer (c): the success-arm IS reached but does not capture.
  confirming_evidence:
    - >
      cmd/tide-push/main.go emits headSHA on EVERY success path — line 697 (normal)
      and line 688 (NoErrAlreadyUpToDate) — for BOTH the artifact-stage variant
      (--stage-envelopes) and the boundary variant; both are the SAME `--mode=push`
      binary via runPush. So option (b) "envelope lacks headSHA" is FALSE for a
      successful push. The ONLY success-without-envelope path is the integration-only
      early return (main.go:477), which requires --integrate-task-branches AND no
      artifacts/envelopes — and that path is only taken by the wave-integration Job,
      which uses a DISTINCT name (tide-push-wave-<plan.UID>-<waveIndex>,
      boundary_push.go:183), never the shared tide-push-<uid> name.
    - >
      readPushEnvelope (project_controller.go:94-97) returns pods.Items[0] with no
      preference for the succeeded pod; push Job BackoffLimit=2 means a Job can own
      both a Failed attempt pod and a Succeeded pod. K8s List order is not defined,
      so the failed attempt's empty-headSHA envelope can win.
    - >
      success arm (project_controller.go:747-757): `if env, ok := readPushEnvelope();
      ok && env.HeadSHA != "" { LastPushedSHA = env.HeadSHA }` then UNCONDITIONALLY
      setBoundaryPushedCondition(ConditionTrue, ReasonPushed). The terminal guard at
      678-681 (`if BoundaryPushed==True { return }`) blocks all future capture.
    - >
      The coupling itself is INTENTIONAL (R-05, artifact_push.go:158-164;
      boundary_push.go:42-45): every push carries the cumulative artifact map so a
      single writer class stages all levels behind ONE force-with-lease anchor.
      Decoupling the names would reintroduce the concurrent-lease race the unification
      deliberately solved — so the fix must live in the capture path, not the names.
  falsification_test: >
    If capture were robust, an envtest with a succeeded Job whose Items[0] is a
    failed-attempt pod would still advance LastPushedSHA to the succeeded pod's
    headSHA; and a succeeded Job with an UNreadable envelope would NOT terminally set
    BoundaryPushed=True with an empty anchor. Before the fix both wedge; after, both
    behave. (Layer-B transport confirmation is DEFERRED — see blind_spots.)
  fix_rationale: >
    (1) readPushEnvelope: prefer the SUCCEEDED pod (phase Succeeded / exit 0) among
    the Job's pods; fall back to the first parseable envelope so the terminal-failure
    classification arm still reads the failed pod's reason. (2) success arm: if the
    succeeded Job's headSHA is NOT captured (unreadable or empty), do NOT go terminal
    — delete the stale/foreign Job and re-dispatch a fresh project-boundary push the
    state machine OWNS (bounded by maxBoundaryPushAttempts). Its fresh pod's envelope
    is readable on the next observation; the re-push is an idempotent no-op
    fast-forward of the already-landed run-branch HEAD. Both fixes preserve the D-B5
    single-writer coupling — no name decoupling, no lease race.
  blind_spots: >
    Full Layer-B kind run is DEFERRED (minikube-OOM + auto-mode gate). Envtest pins
    the capture/terminality behavior but NOT the real in-cluster push transport. If
    the true live cause is instead a FAILING in-cluster push (anonymous http
    receive-pack rejected), this fix will not green Layer-B by itself — the added
    in-poll diagnostics (artifact_staging_test.go) will surface that on the
    orchestrator's next single run (push pod phase + receive-pack count + LastPushedSHA
    per tick, WHILE the ns still exists).
next_action: >
  Implement (1) readPushEnvelope succeeded-pod preference, (2) success-arm
  no-terminal-without-capture + bounded re-dispatch, (3) in-poll kind diagnostics.
  Add envtest Test 6 (multi-pod mask) + Test 7 (unreadable-success re-dispatch),
  RED-before/GREEN-after. Run debug13b envtest package. Commit; flag Layer-B DEFERRED.
tdd_checkpoint:

## Evidence

(see "Evidence (from two runs, already gathered)" above)

## Eliminated

- hypothesis: Shared-Job-name coupling (artifact push creates `tide-push-<uid>` first; boundary push hits `AlreadyExists` and never runs its write-back). **DISPROVEN by runtime evidence** — neither the clone NOR the push Job pods were ever admitted (`serviceaccount "tide-push" not found`), so no push Job pod ran at all. There was no `AlreadyExists` interaction; the Jobs existed as objects but produced zero pods. The coupling (code-review IN-01) may still be a latent under-test concern, but it is NOT the cause of this RED.
  evidence: kube-controller-manager Job controller: `pods "tide-clone-<uid>-"/"tide-push-<uid>-" is forbidden: error looking up service account artifact-staging-test/tide-push: serviceaccount "tide-push" not found` (repeated); git-http-server logs show zero receive-pack/upload-pack traffic.
  timestamp: run-2 kind logs (2026-07-08)
- hypothesis: Pure test race (checking the `Complete`-time snapshot before the async boundary push lands). PARTIALLY eliminated as the WHOLE cause: wrapping the assertion in a 5-min `Eventually` did NOT make `LastPushedSHA` advance — because the real blocker was that no push ever ran (missing SA) AND the controller never writes `LastPushedSHA`. The direct-`Expect`-on-snapshot IS a real, additional test defect per the #13b contract (Complete is not gated on the push), and is fixed as part of the resolution — but it was never the root cause.
- hypothesis: Phase-37 artifact-push trigger broken. Eliminated: the trigger provably fires (`"triggered artifact push", envelopes:1`) — it CREATED the Job object; the Job just never produced a pod.
- hypothesis: Push transport / git-http fixture broken. Eliminated: the fixture (bare repo + in-cluster git-http server) came up Available; it received zero requests because no push pod ever ran, not because the transport failed.

## Evidence (confirmation pass, 2026-07-08)

- timestamp: 2026-07-08 — checked: kind-logs control-plane pod log. found: Job controller repeatedly rejected pod creation for BOTH `tide-clone-<uid>-` and `tide-push-<uid>-` with `serviceaccount "tide-push" not found` in ns artifact-staging-test. implication: clone + push Jobs exist as objects but admit ZERO pods → no clone worktree, no push.
- timestamp: 2026-07-08 — checked: git-http-server pod logs (both replicas). found: no `git-receive-pack` / `git-upload-pack` / POST push traffic at all. implication: nothing ever reached the bare remote — corroborates the pods-never-ran finding.
- timestamp: 2026-07-08 — checked: captured pod inventory for ns artifact-staging-test. found: tide-init, tide-milestone/phase/plan/project (subagent SA), tide-reporter, tide-task pods present; NO tide-push-* / tide-clone-* pods. implication: only Jobs whose SA is provisioned ran; the tide-push-SA Jobs did not.
- timestamp: 2026-07-08 — checked: test/integration/kind/failure_test.go:130 createNamespace + suite helpers. found: provisions tide-subagent, tide-import, tide-reporter SAs (+ reporter RBAC) but NOT tide-push. implication: fixture gap — the tide-push SA the clone/push Jobs reference is never created in the per-test namespace.
- timestamp: 2026-07-08 — checked: internal/controller/project_controller.go:734-750 (reconcileBoundaryPush success arm) + cmd/tide-push/main.go:697 + readPushEnvelope. found: tide-push emits `headSHA` on exitSuccess and readPushEnvelope parses it, but the success arm patches only LeaseFailureCount/LastError/BoundaryPushed=True — never HeadSHA→LastPushedSHA. implication: SECOND, independent defect — even a fully-successful push leaves Status.Git.LastPushedSHA empty, so line-176 would still fail after the SA gap is closed.

## Resolution

root_cause: >
  Three defects total across the session. (A) tide-push SA absent in the Layer-B
  namespace — FIXED in 1bcbea6 (ensurePushSARBAC); run-3 pod dirs confirm pods now
  admitted. (B) reconcileBoundaryPush never wrote LastPushedSHA — write-back added in
  1bcbea6, envtest-green. (C, THE RESIDUAL RED — run-5 code trace) even WITH the
  write-back, LastPushedSHA never advances in the live kind run because the boundary
  success-arm fails to DURABLY CAPTURE the shared `tide-push-<project.UID>` Job's
  headSHA and then goes TERMINAL. Two code-level mechanisms, both downstream of the
  INTENTIONAL D-B5/R-05 single-writer coupling (artifact + boundary pushes share the
  Job name on purpose, behind one force-with-lease anchor):
    (C1) readPushEnvelope (project_controller.go:86) read pods.Items[0] blindly.
         A push Job has BackoffLimit:2, so a transient first-attempt FAILED pod
         (envelope headSHA="") can be Items[0] and mask the SUCCEEDED pod's real
         headSHA — leaving env.HeadSHA=="".
    (C2) the success arm (project_controller.go:735-760) set BoundaryPushed=True
         TERMINALLY even when it captured NO headSHA; the terminal guard at 678-681
         then returns early on every subsequent reconcile, freezing LastPushedSHA
         empty FOREVER.
  This is answer (c): the boundary success-arm IS reached but does not capture the
  shared Job's headSHA. Answer (b) is FALSE — cmd/tide-push emits headSHA on EVERY
  success path (main.go:688,697) for BOTH the --stage-envelopes variant and the
  --mode=push boundary variant (same runPush binary); the only no-envelope success is
  the integration-only early return (main.go:477), reachable ONLY by the
  distinctly-named tide-push-wave-<plan.UID>-<waveIndex> Job, never the shared name.
fix: >
  Controller-only fix (preserves the D-B5 shared-name coupling — NO name decoupling,
  which would reintroduce the concurrent-lease race the R-05 unification solved).
  (C1) internal/controller/project_controller.go readPushEnvelope: prefer the
  SUCCEEDED pod (phase Succeeded / exit 0) among the Job's pods; fall back to the
  first parseable envelope so the leak/lease failure-classification arms still read
  the failed pod's reason.
  (C2) reconcileBoundaryPush success arm: if a succeeded Job's headSHA is unreadable
  or empty, do NOT go terminal — delete the stale Job and re-dispatch a fresh
  project-boundary push the state machine OWNS (fresh, readable Pod envelope; bounded
  by maxBoundaryPushAttempts; idempotent no-op re-push of the already-landed HEAD).
  (diagnostics) test/integration/kind/artifact_staging_test.go: per-tick in-poll
  logging (push/clone pod phase, git-http git-receive-pack count, LastPushedSHA) via
  GinkgoWriter INSIDE the Eventually — WHILE the ns exists, NOT in DeferCleanup/AfterAll
  (the run-4 flaw), so the orchestrator's next single Layer-B run captures decisive
  evidence.
verification: >
  ENVTEST (allowed surface) — GREEN. New debug13b Test 6 (multi-pod: a failed-attempt
  pod must not mask the succeeded pod's headSHA → LastPushedSHA advances) and Test 7
  (succeeded Job with unreadable headSHA → re-dispatch, never wedge BoundaryPushed=True
  with an empty anchor). RED-before (2 Failed against pre-fix project_controller.go
  with the new tests present), GREEN-after (Ran 8 of 170 debug13b specs, 8 Passed /
  0 Failed). Existing Test 2 (single readable success) unchanged/passing; broader
  push/lease/leak/boundary controller specs pass (no regression). golangci-lint 0
  issues, gofmt clean, go vet clean, kind package compiles.
  LAYER-B KIND RUN: DEFERRED to the orchestrator (live minikube blocks a 2nd kind
  cluster; make test-int / test-int-kind-prep are orchestrator-gated). NO Layer-B
  green is claimed. If the true live cause is instead a FAILING in-cluster push
  (anonymous http receive-pack rejected) rather than the capture wedge, this fix will
  not green Layer-B alone — the added in-poll diagnostics will surface that on the
  next run.
files_changed:
  - internal/controller/project_controller.go        # C1 readPushEnvelope + C2 success-arm (this session)
  - internal/controller/project_boundary_push_test.go # debug13b Test 6 + Test 7 (this session)
  - test/integration/kind/artifact_staging_test.go    # in-poll diagnostics (this session)
  - test/integration/kind/failure_test.go             # ensurePushSARBAC (prior fix 1bcbea6)

## Run-3 (Layer-B confirmation of fix 1bcbea6) — STILL RED (reopened)

Ran the full Layer-B `artifact_staging` spec against a fresh kind cluster built from HEAD (fix included). `KIND_EXIT=1`, spec RED (~1076s). Findings:

- **Defect A fix WORKED (partial):** the run-3 export shows a `tide-push-<uid>` **and** a `tide-clone-<uid>` pod dir — the clone/push Job pods are now ADMITTED (previously rejected with `serviceaccount "tide-push" not found`). So the SA provisioning is effective.
- **But `LastPushedSHA` still never advances within the 5-min poll** (all 3 flake-attempts run the full poll → never succeeds). So the fix is **insufficient end-to-end**: the push pod is created but its SHA never reaches `Status.Git.LastPushedSHA`. Unknown whether the push pod SUCCEEDS (lands the push + writes the envelope) or fails — **could not confirm** because the shared kind-logs path `/var/folders/.../kind-logs-tide-test` was NOT overwritten by run-3 (still holds run-1's logs; UTC 16:37 ≠ run-3's 18:04) — a stale-shared-logs analysis blocker.
- **Tertiary test-harness bug (blocks green regardless):** the poll's `k8sClient.Get(ctx, …)` uses the suite `ctx = context.WithTimeout(..., kindTestTimeout=18m)` (suite_test.go:96). Because each attempt runs the FULL 5-min poll (LastPushedSHA never comes) and `flake-attempts=3`, the 3 attempts consume the 18-min ctx and the final poll dies on `client rate limiter Wait returned an error: context deadline exceeded` (artifact_staging_test.go:183) — masking the real LastPushedSHA state. The poll must use a FRESH context, not the expiring suite ctx.

### Reopened focus — do NOT blind-re-run; instrument first
next_action_v2: >
  (1) TEST HARNESS: in artifact_staging_test.go, give the poll's Get a fresh context
  (e.g. context.Background() or a new WithTimeout) so it survives the poll; and make
  exportKindLogs write to a UNIQUE dir per run (or read logs BEFORE AfterAll deletes
  the ns) so run-N logs are analyzable. (2) INSTRUMENT: log, per poll tick, the push
  pod phase (Succeeded/Failed/Running) + Status.Git.LastPushedSHA + the git-http-server
  receive-pack count, via GinkgoWriter — so the NEXT run yields the decisive evidence
  in-band (does the push pod succeed? does readPushEnvelope find a HeadSHA? does the
  write-back fire?) WITHOUT depending on the flaky shared kind-logs path. (3) Only then
  re-run Layer-B. Likely remaining root cause candidates: the push pod fails in-cluster
  (anonymous http receive-pack rejected, or clone/push ordering), OR readPushEnvelope
  can't find/parse the envelope on the PVC at the path the write-back reads. Confirm
  from the instrumented run, don't assume.

## Run-4 (instrumented, flake-attempts=1) — STILL RED, but LastPushedSHA-empty now CLEANLY isolated

- **ctx mask removed:** with `-ginkgo.flake-attempts=1` (single ~6-min attempt << 18-min suite ctx), the poll failed CLEANLY at line 229: `lastPushedSHA ... Expected <string>: (empty) not to be empty` after the full 300s poll — NOT a ctx-deadline error. So it is now CONFIRMED beyond doubt: **the boundary push never advances LastPushedSHA in the live kind run**, even with fix 1bcbea6 (defect A SA + defect B write-back).
- **Instrumentation FLAW (mine):** the `DeferCleanup` diagnostic dump ran AFTER teardown (CR/ns already deleted — `project not found`, `namespaces "artifact-staging-test" not found`, 0 pods), so it captured nothing. The push-pod-level evidence (did the push pod Succeed/Fail? did git-http log a receive-pack? did readPushEnvelope find a HeadSHA?) was NOT obtained. To capture it, log INSIDE the Eventually poll (per tick, while the ns exists), NOT in DeferCleanup/AfterAll.
- **Code-confirmed live suspect (IN-01, re-opened):** `internal/controller/artifact_push.go:212` and `internal/controller/boundary_push.go:94` build the IDENTICAL deterministic Job name `fmt.Sprintf("tide-push-%s", project.UID)`, and BOTH tolerate `AlreadyExists` as idempotent success. The artifact push fires FIRST (planner completion) and wins the create; the boundary push then hits `AlreadyExists` and NEVER creates its own Job. So the Job that runs is the ARTIFACT-push variant. The debugger disproved IN-01 on run-1 (no pods, SA missing) — but with the SA fix, pods run and this coupling is BACK as the prime suspect for why the boundary success-arm's `readPushEnvelope`→`HeadSHA` never yields a value. **Still unconfirmed** whether (a) the shared push pod FAILS in-cluster (anonymous http receive-pack rejected / clone fails), or (b) it succeeds but its envelope has no HeadSHA the boundary arm reads, or (c) the shared Job never reaches the boundary success-arm at all.

### next_action_v3 (do NOT blind-re-run — instrument IN-POLL, then ONE run)
1. Move the diagnostics INTO the poll's Eventually (log per tick: push/clone pod phase, git-http receive-pack count, LastPushedSHA) so they fire WHILE the ns exists.
2. Run Layer-B once with `-ginkgo.flake-attempts=1`. The in-poll log will finally say whether the push pod SUCCEEDS and whether a HeadSHA envelope is produced/read.
3. If the push pod fails → fix the in-cluster push (transport/creds/ordering). If it succeeds but no HeadSHA reaches the CR → the artifact-vs-boundary shared-Job-name coupling is the bug: decouple the two push Job names (or make the boundary success-arm read the shared Job's headSHA regardless of which path created it). This is a CODE fix in artifact_push.go / boundary_push.go / project_controller.go, NOT just a test fix.

STATUS: 4 Layer-B runs done (~90 min cluster time); halting per stop-brute-forcing. Fix 1bcbea6 stays committed (defect A partial + defect B verified); DASH-02 remains RED/Pending. Resume with `/gsd-debug continue dash02-artifact-boundary-push`.

## Run-5 (code-only, no cluster) — residual RED traced to the boundary success-arm CAPTURE path; controller fix applied, envtest RED→GREEN

End-to-end code trace of the shared `tide-push-<project.UID>` Job lifecycle (no Layer-B run — orchestrator-gated). Findings and fix:

- **Answer to (a)/(b)/(c): (c).** The boundary success-arm IS reached but does not durably capture the shared Job's headSHA, then goes terminal — freezing `LastPushedSHA` empty forever via the terminal guard (project_controller.go:678-681).
  - (b) ruled out by code: cmd/tide-push emits headSHA on EVERY success path (main.go:688, 697) for BOTH the `--stage-envelopes` artifact variant and the `--mode=push` boundary variant (identical runPush binary). The only no-envelope success (integration-only, main.go:477) is reachable ONLY by the distinctly-named `tide-push-wave-<plan.UID>-<waveIndex>` Job, never the shared name.
  - (a) is half-true and NOT the RED cause: the artifact-push variant wins the shared-name create and the D-B2-shaped boundary COMMIT is never authored, BUT the artifact variant DOES commit+push the run branch (with artifacts), so the run branch lands on the remote. The gap is purely capture, not "nothing pushed."
- **Two enabling code defects, both fixed in `internal/controller/project_controller.go` (D-B5 coupling preserved — NO name decoupling):**
  - **C1 — readPushEnvelope multi-pod mask:** read `pods.Items[0]` blindly; a `BackoffLimit:2` Job can own a failed-attempt pod (headSHA="") beside the succeeded pod. FIX: prefer the Succeeded/exit-0 pod; fall back to first parseable envelope (leak/lease arms still read the failed reason).
  - **C2 — terminal-without-capture wedge:** the success arm set `BoundaryPushed=True` even with no captured headSHA. FIX: if a succeeded Job's headSHA is unreadable/empty, delete the stale Job and re-dispatch a fresh OWNED boundary push (bounded, idempotent no-op re-push) rather than wedging terminal.
- **Diagnostics (test/integration/kind/artifact_staging_test.go):** per-tick in-poll logging (push/clone pod phase, git-http `git-receive-pack` count, LastPushedSHA) via GinkgoWriter INSIDE the Eventually — fires WHILE the ns exists (fixes the run-4 DeferCleanup-after-teardown flaw).
- **ENVTEST verdict (allowed surface): RED→GREEN.** New debug13b Test 6 (multi-pod mask) + Test 7 (unreadable-success re-dispatch). RED-before = 2 Failed against pre-fix controller code (tests present); GREEN-after = `Ran 8 of 170 debug13b specs, 8 Passed / 0 Failed`. Broader push/lease/leak/boundary specs pass (no regression). lint 0 / gofmt / vet clean; kind pkg compiles.
- **LAYER-B: DEFERRED to the orchestrator.** No Layer-B green claimed. The in-poll diagnostics will decisively confirm on the next single run whether the shared push pod SUCCEEDS and a headSHA reaches the CR (this fix's target) or whether the live cause is a failing in-cluster push (would need a transport fix).

## Run-5 (verify fix 01b5004, flake-attempts=1, in-poll diag) — STILL RED, and the diagnostics REVEAL THE REAL CAUSE

status back to `investigating`. The correctly-placed in-poll diagnostics fired every tick and are unambiguous:
```
artifact-staging diag: push/clone pods=[tide-clone-<uid>=Pending tide-push-<uid>=Pending] git-receive-pack=0 LastPushedSHA=""
```
For the ENTIRE 5-min poll the clone AND push pods sit at **Pending** — they NEVER start (never ContainerCreating/Running/Succeeded/Failed). git-receive-pack=0, LastPushedSHA="" throughout.

**THE REAL ROOT CAUSE IS UPSTREAM OF DEFECTS A/B/C:** the clone/push pods are **unschedulable (Pending)** — so no push ever runs, which is why LastPushedSHA never advances. Defects A (SA), B (write-back), C (capture wedge) are all REAL latent bugs and their fixes are correct + envtest-green, but NONE of them is the cause of this RED. The push never even starts.

**Strong hypothesis (unconfirmed — needs `kubectl describe pod`):** Pending (not ContainerCreating) = a SCHEDULING block. On this memory-constrained dev host (8.3 GiB; kind node small), the likeliest cause is **insufficient allocatable node CPU/memory** — by the time the clone/push pods are created (after the stub subagent + reporter + init pods already reserved the node), the single kind node can't schedule them. i.e. an **ENVIRONMENT/resource artifact of this host, NOT a product bug** — DASH-02's push path may well be correct and would likely pass on a properly-resourced CI node. (Same env-gate theme that started this saga.) Alternative candidates: an unbound/RWO tide-projects PVC blocking the mount, or a nodeSelector/taint mismatch on the clone/push pod spec.

### next_action_v4 (ONE more run, describe-instrumented — or run on a bigger host)
- Add `kubectl describe pod <tide-push/clone pod>` (or dump `.status.conditions` + scheduler events) to the in-poll diagnostics so the NEXT run prints the exact Pending reason (`FailedScheduling: Insufficient cpu/memory` vs `unbound PVC` vs taint). OR simply run `make test-int` on an isolated, properly-resourced host (real CI / a bigger VM) — if it greens there, the RED was purely this host's kind-node resource starvation and DASH-02 is actually sound (fixes A/B/C are still worth keeping as latent hardening).
- HALTED here: 5 Layer-B runs (~110 min), orchestrator context exhausted. Fixes 1bcbea6 + 01b5004 stay committed (real latent-bug hardening, envtest-green). DASH-02 remains RED/Pending pending confirmation of the Pending-pod cause.

## Run-6 (code-only, no cluster) — TRUE ROOT CAUSE: tide-push image-tag fixture gap → ImagePullBackOff (NOT scheduling)

The run-5 "unschedulable → kind-node resource starvation" conclusion is **DISPROVEN by code**, and the actual cause is a one-line test-fixture gap. Confirmed statically (no Layer-B run) and verified via `helm template`.

- **Resource starvation — REFUTED.** The clone/push Job pods set NO cpu/memory requests anywhere in `internal/controller` (`grep -rnE 'corev1.ResourceList|ResourceCPU|ResourceMemory|resource.MustParse' internal/` hits only a PVC `storage:1Gi` + test files). A request-less pod passes the scheduler's `NodeResourcesFit` unconditionally — it CANNOT fail on "Insufficient cpu/memory" no matter how tight the 8.32 GiB Docker VM is.
- **PVC as scheduling blocker — REFUTED.** The `tide-projects` claim is a SHARED RWO PVC (WaitForFirstConsumer local-path, single-node kind: `cluster.yaml` has one control-plane node). The subagent/planner pods mount the IDENTICAL claim (`internal/dispatch/podjob/jobspec.go:441`, subPath `<uid>/...`) and scheduled + ran to `Complete`. Same claim + same node + same access mode ⇒ if the PVC blocked scheduling, the subagent pods would be blocked too. They weren't. So the PVC does not gate clone/push scheduling.
- **The run-5 diagnostic conflated two different `Pending` states.** It captured only `pod.Status.Phase`, which is `Pending` for BOTH "unschedulable (FailedScheduling)" AND "scheduled but ContainerCreating/ImagePullBackOff." It never captured `.status.conditions[PodScheduled]` (would have been `True`) or `.status.containerStatuses[].state.waiting.reason` (would have said `ImagePullBackOff`). So "not ContainerCreating = scheduling block" was an unverified assertion.
- **TRUE ROOT CAUSE — tide-push image-tag mismatch (cascade-12 shape).** The chart sets the manager env `TIDE_PUSH_IMAGE = {{ images.tidePush.repository }}:{{ images.tidePush.tag | default .Chart.AppVersion }}` (`charts/tide/templates/deployment.yaml:47`). `images.tidePush.tag` defaults to `""` and the kind suite's helm `--set` block (`test/integration/kind/suite_test.go`) overrides `.tag=test` for controller/stubSubagent/credProxy/tideReporter/tideImport — but **NOT for tidePush**. So it resolves to `.Chart.AppVersion` = `1.0.7` (`Chart.yaml:12`). `make test-int-kind-prep` builds+loads ONLY `ghcr.io/jsquirrelz/tide-push:test` (`Makefile:164,190`) — never `:1.0.7`. Result: the controller dispatches clone/push Jobs with `tide-push:1.0.7`, absent from the node; `pullPolicy:IfNotPresent` → pull from ghcr.io → fails (private/no-net) → **ImagePullBackOff → phase Pending** for the whole 5-min poll → `LastPushedSHA` never advances. Every OTHER image ran because it got both the load AND the `--set .tag=test` override; tide-push had only the load. (Makefile:194 even documents this exact trap for tideImport.)
- **Defects A/B/C were real latent bugs but NONE was the RED cause.** The push pods never ran at all — no capture path was ever exercised in the live run.

reasoning_checkpoint_v2:
  hypothesis: >
    Clone/push pods are ImagePullBackOff (phase=Pending) because the kind suite
    never overrides images.tidePush.tag, so the manager's TIDE_PUSH_IMAGE resolves to
    the unloaded Chart.AppVersion tag (1.0.7) instead of the loaded :test tag.
  falsification_test: >
    helm template with the suite's --set flags renders TIDE_PUSH_IMAGE. If the tag is
    1.0.7 without the override and :test with it, the fixture-gap hypothesis holds.
  confirming_evidence:
    - >
      helm template BEFORE (no tidePush override): TIDE_PUSH_IMAGE =
      "ghcr.io/jsquirrelz/tide-push:1.0.7". AFTER (--set images.tidePush.tag=test):
      "ghcr.io/jsquirrelz/tide-push:test". Decisive, no cluster.
    - >
      Makefile:164,190 build+load ONLY tide-push:test; no :1.0.7 image ever exists on
      the node. suite_test.go grep for 'images.tidePush' tag override = EMPTY.

### FIX APPLIED (test-fixture only — no product code, values.yaml untouched)
- `test/integration/kind/suite_test.go`: added `--set images.tidePush.tag=test` +
  `--set images.tidePush.pullPolicy=IfNotPresent` to the helm install block, mirroring
  the existing tideImport override. `go vet ./test/integration/kind/...` = clean (exit 0).
- VERIFIED (no spin): `helm template` confirms TIDE_PUSH_IMAGE now resolves to `:test`.
- LAYER-B GREEN: still needs ONE kind run to prove end-to-end (minikube is currently
  Stopped, so the 2nd-cluster gate is clear). NOT claimed green yet. But this is now a
  targeted fix with render-level proof, not a blind re-run.

## Eliminated (Run-6)

- hypothesis: kind-node CPU/memory resource starvation (run-5's "strong hypothesis").
  **DISPROVEN by code** — clone/push pods declare zero cpu/memory requests, so
  NodeResourcesFit always passes; "Insufficient cpu/memory" is impossible for them.
  evidence: `grep -rnE 'corev1.ResourceList|ResourceCPU|ResourceMemory' internal/` returns only a PVC storage request + test files; no pod resource requests exist.
- hypothesis: shared RWO `tide-projects` PVC blocks clone/push scheduling (WFC / node
  affinity / RWO multi-attach). **DISPROVEN** — subagent pods mount the identical claim
  on the same single node and scheduled fine; a scheduling-level PVC gate would block
  them equally.
  evidence: internal/dispatch/podjob/jobspec.go:441 (subagents mount tide-projects); test/integration/kind/cluster.yaml (single control-plane node); subagent pods reached Complete in run-1.

## Run-7 (Layer-B, fix applied, disambiguated diag) — ORIGINAL BUG GREEN; new narrower failure isolated

Ran the focused artifact_staging spec against a fresh kind cluster with the tide-push
image-tag fix + the sched/wait diagnostics. `KIND_EXIT=1`, but the RED moved PAST the
original defect to a new, later assertion. Decisive evidence (all from this run's
GinkgoWriter, not the stale logs):

- **Image-tag fix CONFIRMED live.** Diag: `tide-clone-<uid>=Succeeded|sched=True|wait=
  tide-push-<uid>=Succeeded|sched=True|wait=`. Both pods now SCHEDULE and SUCCEED — the
  ImagePullBackOff is gone. Run-5's "Pending forever" is fully resolved.
- **`LastPushedSHA` ADVANCES.** `LastPushedSHA="4dccc52d3986b65004083b3f26c96951017bd9f8"`.
  The line-200/176 precondition that was RED for 5 runs now PASSES. The boundary push
  lands, no lease failure, Phase != PushLeaseFailed. **The chartered DASH-02 bug
  (artifact-vs-boundary push interaction → empty LastPushedSHA) is FIXED and GREEN.**
- **Artifact stages on the run branch.** `.tide/ paths on run branch:
  [.tide/planning/project/artifact-staging-project-1783542624/MILESTONES.md]`. The push
  pipeline works end-to-end.
- **NEW FAILURE (artifact_staging_test.go:281):** `kindsSeen = {"project": true}` but
  Assertion 4 requires `HaveKey("milestone")`. Only the project-kind artifact is on the
  branch when the assertion runs.

### Root cause of the new failure (very likely a TEST-POLL RACE, not the chartered bug)
- The materialization poll (artifact_staging_test.go:213-227) exits as soon as
  `mdCount >= 1` — the FIRST *.md to appear. It caught `project/MILESTONES.md` and
  stopped; Assertion 4 then demanded a `milestone` kind that had not landed yet.
- Artifact pushes fire PER-LEVEL incrementally (milestone_controller.go:290/713, phase,
  plan, project each call triggerArtifactPush), so levels materialize on the run branch
  at DIFFERENT times. The poll's mdCount>=1 exit condition does not wait for the full
  expected level set → it can (and did) exit with only one level present.
- The test's own comment (line 275) assumed "milestone reaches Succeeded first and most
  reliably" — but PROJECT landed first here, so that assumption is stale.
- NOT yet fully distinguished from the alternative (milestone artifact never stages)
  because the exported controller log is STALE (run-3 shared-kind-logs-path bug: mtime
  12:39 ≠ this run's 16:32). The poll fix below is decisive for BOTH: it either waits
  out the race → GREEN, or times out listing exactly which levels DID stage → proves a
  real product gap in-band, no stale-log dependency.

### next_action_v5 (test-harness fix, then ONE run — decisive either way)
- artifact_staging_test.go materialization poll: replace the `mdCount >= 1` exit with a
  wait for the EXPECTED level set (at minimum `milestone`, ideally all levels the stub
  cascade emits), with a timeout message listing the levels seen so far. This removes the
  race if it is one, and yields decisive in-band evidence of a real staging gap if it is
  not. Also worth fixing the stale-shared-kind-logs export (unique dir per run) so the
  controller log is analyzable next time.
- SEPARATE from the chartered bug: this is artifact-staging COMPLETENESS across levels,
  not the artifact-vs-boundary push interaction. The chartered bug is resolved.

### Fixes to preserve (this session)
- test/integration/kind/suite_test.go: `--set images.tidePush.tag=test` +
  `pullPolicy=IfNotPresent` (THE fix — resolves the chartered RED; render-verified +
  live-confirmed).
- test/integration/kind/artifact_staging_test.go: sched/wait disambiguation in the
  in-poll diagnostic (converted the ambiguous phase=Pending into decisive evidence).

## Run-8 (Layer-B, poll waits for milestone level) — NOT a race: REAL multi-level staging gap

Re-ran with the poll waiting for the `milestone` level + per-tick level logging + the
unique-per-run log-dir fix. `KIND_EXIT=1`, timed out after the full 5-min poll (414s).

- **DECISIVE — not a poll race.** The per-tick log printed `staged planner levels so
  far: [project]` for ALL 59 ticks (5 min). `milestone` NEVER appeared. The old
  mdCount>=1 exit was not the whole story — even waiting the full budget, only `project`
  ever stages.
- **Mechanism (fresh controller log — the unique-dir fix worked, mtime matches this run):**
  exactly ONE `"triggered artifact push"` event, carrying `"level":"project"`. NO
  milestone/phase/plan artifact push fires. The project push carries a single envelope
  (`envelopes:1`), NOT a cumulative multi-level map. Then `"boundary push landed on
  remote"`. So `.tide/planning/project/<proj>/MILESTONES.md` is the ONLY artifact staged.
- **This is a SEPARATE issue from the chartered bug (which is fixed + green).** It is
  artifact-staging COMPLETENESS: milestone/phase/plan level artifacts never reach the run
  branch because their per-level artifact push is never triggered in this cascade. Open
  question (needs artifact_push.go + 37-06/37-09 design intent): is multi-level staging
  the intended contract (→ product bug: per-level triggers/cumulative map not firing) or
  is only project-level staging intended (→ test Assertion 4 is over-strict)?
- Minor diag caveat: git-receive-pack=0 in the push diag despite a landed push — the
  receive-pack counter likely mis-targets the git-http-server container/log; not central.

### next_action_v6 (NEW investigation — scope decision for the user)
- Root-cause the milestone/phase/plan artifact-push non-trigger: read triggerArtifactPush
  call sites (milestone_controller.go:290/713, phase, plan) and the cumulative
  stage-envelopes map builder (artifact_push.go) against the 37-06/37-09 intent. Decide
  product-bug (fix the triggers/map) vs test-over-strict (relax Assertion 4 to the
  levels actually contracted). This is a fresh cascade, tracked separately from DASH-02's
  chartered artifact-vs-boundary-push bug.

## Run-9 (code + fresh-log trace) — DEFECT E ROOT-CAUSED: shared single-flight freezes an early [project]-only map

Investigated the staging gap with the fresh (unique-dir) controller log + code trace.
CONFIRMED product bug, not test-over-strict. Assertion 4 (milestone must stage) is
correct per 37-06's cumulative multi-level staging design (collectStageEnvelopes lists
all planner-materialized milestone/phase/plan children + project).

**Mechanism (fresh log, chronological, project UID 08c196e0):**
1. 22:49:35 — the PROJECT controller's triggerArtifactPush (project_controller.go:1418)
   fires FIRST, before the milestone/phase/plan children materialize. collectStageEnvelopes
   returns `[project]` only (a milestone exists → project included via len(msList)>0, but
   no child is plannerMaterialized yet). It wins the shared `tide-push-<uid>` name.
   Logged: `triggered artifact push level=project envelopes=1 job=tide-push-08c196e0`.
2. 22:49:43-59 — children materialize (reporters spawn AFTER the push). Their per-level
   triggerArtifactPush calls AND the project-Complete triggerBoundaryPush (which DOES
   re-collect the full cumulative map at boundary_push.go:124) ALL hit the single-flight
   guard (artifact_push.go:213-219 / boundary_push.go:99-101 `if getErr==nil {return nil}`)
   because the early Job still exists → return early, NO push.
3. 22:50:07 — `boundary push landed on remote`. DECISIVE: `triggered boundary push`
   (boundary_push.go:149) count = **0** — the boundary push NEVER created its own Job.
   The landed push is the STALE early artifact Job (StageEnvelopes=[project]) completing
   under the shared D-B5 name.
4. The shared Job lingers (TTLSecondsAfterFinished:300s) until ~22:54:35; the fast stub
   cascade completes and CLEANS UP at 22:55:08 (project/milestone/phase/plan cleanup)
   before any further trigger can fire. The `artifact_push.go:216` "self-heals on the
   next push" assumption is DEFEATED — there is no next push.

**Net:** only `.tide/planning/project/<proj>/MILESTONES.md` ever stages; milestone/phase/
plan artifacts never reach the run branch. The bug is the D-B5/R-05 shared single-flight
coupling (the SAME coupling run-5's defect-C fix deliberately preserved) exposing a second
failure mode: an EARLY artifact push snapshots a `[project]`-only cumulative map and freezes
it — every later fuller-map push (including the authoritative boundary push at Complete)
is suppressed by single-flight, and the Job outlives the cascade.

**E1 vs E2 resolved → E1.** The boundary push's collectStageEnvelopes at Complete WOULD
carry the full map (children materialized by then), but it never ran (triggered boundary
push=0). The blocker is the single-flight skip, not a never-materializing child.

### FIX SHAPE (product code, touches the deliberately-preserved D-B5 coupling — needs a decision)
- **Option A (recommended, mirrors run-5 defect-C2 pattern):** when the boundary push
  fires at a terminal boundary (project Complete) and finds an existing `tide-push-<uid>`
  Job that is a STALE ARTIFACT push (created with a smaller cumulative map than now
  materialized), the state machine should OWN a fresh authoritative push: let the stale
  Job finish, then re-dispatch the boundary push with the full collectStageEnvelopes map
  (idempotent no-op restages for already-staged levels via the 37-02 clean-tree skip;
  bounded like maxBoundaryPushAttempts). Preserves single-writer (one Job at a time) — no
  name decoupling, no lease race.
- Option C (weaker): shorten the artifact-push Job TTL so it clears fast enough for a
  later re-trigger. Racy; doesn't guarantee the final push carries the full map.
- Requires envtest RED-before/GREEN-after (the boundary push must supersede a stale
  artifact Job and stage all materialized levels) + a Layer-B re-run to confirm the
  milestone artifact reaches the run branch. Test Assertion 4 is correct as-is; do NOT relax it.

