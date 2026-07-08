---
status: awaiting_human_verify
slug: dash02-artifact-boundary-push
trigger: on the DASH-02 artifact-vs-boundary push interaction
created: 2026-07-08
updated: 2026-07-08
phase: 37-dashboard-surfaces-artifact-view-project-view-log-drawer-sta
---

# Debug: DASH-02 artifact-vs-boundary push interaction

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

