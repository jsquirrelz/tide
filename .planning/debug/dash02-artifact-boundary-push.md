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

CONFIRMED ROOT CAUSE (two independent defects). The Current-Focus shared-Job-name
hypothesis is DISPROVEN by direct runtime evidence: neither the clone nor the push
Job pods EVER RAN, so no artifact/boundary push executed and there was no
`AlreadyExists` interaction at all.

reasoning_checkpoint:
  hypothesis: >
    The Layer-B spec fails PRIMARILY because the `tide-push` ServiceAccount is
    absent in the test namespace — the Job controller cannot admit the clone/push
    pods, so nothing ever pushes and `LastPushedSHA` never advances. SECONDARILY,
    even once a push Job succeeds, `reconcileBoundaryPush`'s success arm
    (project_controller.go:734-750) never reads the push-result envelope's HeadSHA
    nor writes `Status.Git.LastPushedSHA` — so the line-176 assertion would still
    fail after the SA gap is closed.
  confirming_evidence:
    - >
      kube-controller-manager Job controller logged, repeatedly, for BOTH
      `tide-clone-<uid>-` and `tide-push-<uid>-`:
      `forbidden: error looking up service account artifact-staging-test/tide-push:
      serviceaccount "tide-push" not found` (kind-logs control-plane pod log).
    - >
      git-http-server pod logs show ZERO git-receive-pack / git-upload-pack traffic
      → no clone, no push ever reached the bare remote.
    - >
      Captured test-namespace pod inventory has tide-init / subagent / reporter /
      task pods but NO tide-push-* and NO tide-clone-* pods.
    - >
      `createNamespace` (failure_test.go:130) provisions tide-subagent / tide-import
      / tide-reporter SAs but NOT tide-push; the chart's push-rbac.yaml creates
      tide-push only in .Release.Namespace (no projectNamespaces fan-out, unlike
      reporter-rbac.yaml).
    - >
      reconcileBoundaryPush success arm patches LeaseFailureCount / LastError /
      BoundaryPushed=True but never HeadSHA→LastPushedSHA; tide-push DOES emit
      headSHA on exitSuccess (cmd/tide-push/main.go:697) and readPushEnvelope DOES
      parse it — the success arm just never calls it. `grep -rnE '\.LastPushedSHA\s*=' internal/`
      returns nothing.
  falsification_test: >
    If the SA were present and the write-back implemented, git-http-server would log
    a git-receive-pack POST and Status.Git.LastPushedSHA would carry a 40-hex SHA.
    The run showed NEITHER — both predictions of the "it did push" alternative fail.
  fix_rationale: >
    (1) Provision tide-push SA+Role(secrets/get)+RoleBinding in the test namespace
    (mirror chart push-rbac.yaml, same pattern as ensureReporterSARBAC) → clone/push
    pods can be admitted and run. (2) In reconcileBoundaryPush's success arm, read
    the push-result envelope and advance Status.Git.LastPushedSHA = env.HeadSHA →
    satisfies the operator-facing lease-anchor contract the test asserts and the
    #13b comment at project_controller.go:473-475 always intended. (3) Poll the CR
    for the async boundary-push outcome instead of one-shot Expect on the Complete
    snapshot — #13b makes Complete non-gated on the push, so the snapshot races.
  blind_spots: >
    Full Layer-B kind run is DEFERRED (minikube-OOM + auto-mode gate). Envtest
    verifies the controller write-back but not the real push transport or the
    SA-admission path. medium_http_test.go likely shares the same SA gap but is not
    re-run here (createNamespace fix covers it too, purely additively).
next_action: >
  Implement (1) fixture SA helper, (2) controller LastPushedSHA write-back +
  envtest, (3) kind-spec poll. Run the debug13b envtest package. Then commit and
  return a human-verify checkpoint flagging the Layer-B run as DEFERRED.
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
  TWO independent defects, both required for the RED. (A/primary) The `tide-push`
  ServiceAccount is not provisioned in the Layer-B test namespace, so the Job
  controller cannot admit the clone/push Job pods — no clone, no push ever runs,
  nothing reaches the remote, and Status.Git.LastPushedSHA never advances.
  (B/latent) reconcileBoundaryPush's success arm never patches
  Status.Git.LastPushedSHA from the push-result envelope's HeadSHA, so even a
  successful push would leave the lease anchor empty and fail the line-176 assertion.
  A tertiary test defect (one-shot Expect on the Complete snapshot, which #13b makes
  non-gated on the async push) is fixed alongside so the spec waits for the push to land.
fix: >
  (1) internal/controller/project_controller.go — reconcileBoundaryPush success arm:
  read the push-result envelope and set Status.Git.LastPushedSHA = env.HeadSHA
  (best-effort; empty/unreadable envelope leaves the prior anchor untouched).
  (2) test/integration/kind — new ensurePushSARBAC(ns) mirroring chart push-rbac.yaml
  (SA tide-push + Role secrets/get + RoleBinding), called from createNamespace.
  (3) test/integration/kind/artifact_staging_test.go — poll the live CR (Eventually)
  for the terminal push state (LastPushedSHA non-empty, no PushLeaseFailed,
  LeaseFailureCount==0) instead of asserting on the Complete snapshot.
verification: >
  Controller write-back (defect B) verified via envtest (debug13b Test 2 extended to
  assert LastPushedSHA advances to the envelope HeadSHA). Fixture SA (A) + kind-spec
  poll (tertiary) verification requires the full Layer-B kind run — DEFERRED
  (minikube-OOM + auto-mode gate); flagged for the orchestrator to schedule.
files_changed:
  - internal/controller/project_controller.go
  - internal/controller/project_boundary_push_test.go
  - test/integration/kind/failure_test.go
  - test/integration/kind/artifact_staging_test.go
