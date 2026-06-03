---
slug: nightly-int-flake-timeout
status: fix-applied-pending-ci-verify
trigger: "Nightly-integration workflow (run 26849997916, commit 0645e1a) is RED on two distinct CI-harness failures in `make test-int`. Neither reproduces locally (local: Layer A 29/29 + Layer B 14/14 green). Fix so nightly runs green; this gates the v1.0.0 release (tag held local-only at 8a8e843). FAILURE 1 — Layer A envtest flake (1 of 29): init_test.go:101 ART-01 'creates a tide-init-{UID} Job on the first reconcile' failed under CI contention; 28/29 passed. The per-push path (make test-int-fast) guards this contention-flaky class with -ginkgo.flake-attempts=3, but nightly's full make test-int runs the envtest layer WITHOUT flake-attempts (mixed go-test package — flag breaks non-Ginkgo pkgs). FAILURE 2 — Layer B kind BeforeSuite (suite_test.go:446): helm upgrade --install ... --wait --timeout 5m -> context deadline exceeded at 5m1s. Images ARE built + kind-loaded by Makefile test-int-kind-prep and installed pullPolicy=IfNotPresent (NOT ImagePull). Controller Deployment didn't reach Ready within 5m on the cold 2-core ubuntu-latest runner. OBSERVATION GAP: cannot tell 5m-too-tight vs real pod failure — post-failure `kind export logs` artifact was EMPTY because the suite's AfterSuite (suite_test.go:186) deletes the tide-test cluster BEFORE the workflow's failure-collection step (nightly-integration.yml:94-101) runs."
created: 2026-06-02
updated: 2026-06-02
phase: 07-project-to-milestone-authoring-and-self-bootstrap
---

# Debug: nightly-int-flake-timeout

## Symptoms

- **Expected:** `nightly-integration.yml` (carries `make test-int` = Layer A envtest + Layer B kind, then `make test-e2e-kind`) runs green in GitHub Actions. Locally both layers are green: Layer A 29/29 (`make test-int-fast`), Layer B 14/14, full `make test-int` exit 0.
- **Actual:** First-ever GitHub CI run (workflow_dispatch, run `26849997916`, 10m47s) FAILED at the `Layer B kind integration suite (make test-int)` step with exit 2. Two independent failures inside that one `make test-int`:
  1. **Layer A envtest:** `Ran 29 of 29 Specs in 44.699s … 28 Passed | 1 Failed`. The single failure is `Project init Job lifecycle ART-01: init Job created on first reconcile [It] creates a tide-init-{UID} Job on the first reconcile` at `test/integration/envtest/init_test.go:101` → `--- FAIL: TestIntegrationEnvtest (44.70s)`.
  2. **Layer B kind:** `[BeforeSuite] [FAILED] [321.793 seconds]` at `test/integration/kind/suite_test.go:446`. `helm upgrade --install failed after 5m1.644663844s: exit status 1` / `Release "tide" does not exist. Installing it now.` / `Error: context deadline exceeded`. → `Ran 0 of 14 Specs … A BeforeSuite node failed so all tests were skipped` → `--- FAIL: TestIntegrationKind (322.73s)`. `make: *** [Makefile:120: test-int] Error 1`.
- **Error:** Layer B: `helm upgrade --install failed (TIDE_REQUIRE_CONTROLLER=1): exit status 1` with underlying `Error: context deadline exceeded` (the `--wait --timeout 5m` in `helmControllerArgs()` suite_test.go:461 elapsed). The TIDE controller Deployment in `tide-system` did not reach ≥1 ready replica within 5m. **No pod-level evidence captured** — the failure-collection step reported `No files were found with the provided path: /tmp/kind-logs-tide-test/` because `AfterSuite` (suite_test.go:186) had already run `kind delete cluster --name tide-test` before `kind export logs` executed.
- **Timeline:** First time these heavy kind suites have ever executed in GitHub CI. Commit `0645e1a` (this session) moved Layer B kind + kind_e2e OFF the per-push critical path INTO `nightly-integration.yml`; this dispatch is the first run of that new workflow. Both suites pass on the local constrained dev VM (~7.65 GiB Docker). Predicted by the handoff as "may need reliability tuning; treat first-run failure as tuning, not regression."
- **Reproduction:** `gh workflow run nightly-integration.yml --ref main` then `gh run watch <id>`. CI-environment-specific (cold 2-core ubuntu-latest GitHub-hosted runner). Does NOT reproduce on the local VM as-is; local repro of Failure 2 would require constraining resources / cold cluster per the constrained-VM recipe (delete→recreate→pre-warm `tide-test`, one heavy run at a time).

## Current Focus

hypothesis:
- **Failure 1 (Layer A ART-01):** A contention-induced `Eventually`-timeout flake — the SAME class the per-push `test-int-fast` already mitigates with `-ginkgo.flake-attempts=3`. Nightly's full `make test-int` (Makefile:120) runs `go test ./test/integration/...` across a MIXED package set (Ginkgo envtest + kind specs AND plain go-tests like the helm-template contract tests), so `-ginkgo.flake-attempts` cannot be passed to that invocation (it errors "flag provided but not defined" on non-Ginkgo pkgs). Result: nightly Layer A runs WITHOUT the retry protection the fast path has → a single contention flake fails the whole package. NOT a product defect (ART-01 passes locally + per-push).
- **Failure 2 (Layer B helm --wait timeout):** UNDETERMINED between (a) 5m `--wait` genuinely too tight for cold-runner: build 4 images + kind-load + cert-manager + manager pod scheduling/cert-issuance on a 2-core runner, vs (b) a real manager-pod failure (CrashLoop / webhook-cert wait / config). Cannot disambiguate without pod-level logs, which were destroyed by AfterSuite before collection.

next_action: FIRST close the observability gap so the NEXT nightly run can disambiguate Failure 2's root cause — make the kind suite retain the cluster on failure (or collect pod logs/describe/events BEFORE AfterSuite teardown) so `helm`-timeout-vs-pod-failure becomes visible. Confirm whether the suite already supports `KEEP_KIND_CLUSTER` and whether AfterSuite honors it on failure. Only THEN decide Failure 2's true fix (raise `--wait` timeout + add a readiness-diagnostic dump, vs fix a real pod issue). In parallel, design Failure 1's fix: give nightly Layer A the same contention protection as the fast path WITHOUT passing `-ginkgo.flake-attempts` to the mixed package (e.g. split the envtest invocation in `make test-int` to a Ginkgo-only call with flake-attempts, like `test-int-fast` does, then run the remaining non-Ginkgo pkgs separately). Recommend thorough fixes only — NO skip/quarantine of ART-01, NO blanket timeout inflation without evidence. FAILURE 4 (this cycle, diagnostics-only): observability gap now CLOSED — spec-failure dump (`dumpE2ESpecFailureDiagnostics`) added to the kind_e2e gate_flow Ordered container via `AfterEach`+`CurrentSpecReport().Failed()`, dumping the `tide-e2e-gates` authoring Jobs/pods + CR ladder + milestone .status + controller-manager logs to stdout before teardown. Awaiting a fresh nightly to capture proof of WHY the milestone parks at Running (ImagePullBackOff of the dispatched authoring/subagent Job is the leading hypothesis). The real fix is DEFERRED to the next cycle, contingent on that evidence. No behavior/image/chart/timeout change this cycle.

## Evidence

- timestamp: 2026-06-02 — Run 26849997916 job "Heavy kind suites" failed exit 2 in 10m47s. Step "Layer B kind integration suite (make test-int)" = X; step "TIDE kind_e2e e2e suite" = skipped (prior step `set -e` abort). Steps through "Prepare manifests and envtest binaries" all ✓.
- timestamp: 2026-06-02 — Layer A: `Ran 29 of 29 Specs in 44.699 seconds … 28 Passed | 1 Failed`; the one FAIL = `ART-01 … creates a tide-init-{UID} Job on the first reconcile` (`init_test.go:101`). envtest cert-watcher "no such file" ERRORs in the log are the normal envtest control-plane teardown noise (post-suite), not the failure cause.
- timestamp: 2026-06-02 — Layer B: BeforeSuite reused/created `tide-test`, applied CRDs, installed cert-manager v1.20.2 (`cert-manager ready`), then `Applying TIDE controller Deployment via helm @ 21:53:02.953`; `helm upgrade --install failed after 5m1.6s … Error: context deadline exceeded` at 21:58:04. `--wait --timeout 5m` confirmed in `helmControllerArgs()` (suite_test.go:461-484).
- timestamp: 2026-06-02 — Image path NOT the cause: Makefile `test-int-kind-prep` (prereq of `test-int`, Makefile:119) builds `controller:test` + `ghcr.io/jsquirrelz/tide-{stub-subagent,credproxy,push}:test` and `kind load`s all four into `tide-test`; `helmControllerArgs` sets every image `pullPolicy=IfNotPresent` + `tag=test`. So the 5m stall is post-pull (scheduling/readiness/cert), not ImagePullBackOff.
- timestamp: 2026-06-02 — Observability gap CONFIRMED: workflow "Collect kind cluster logs on failure" step ran `kind export logs /tmp/kind-logs-tide-test --name tide-test` but logged `No files… No artifacts will be uploaded`; the suite AfterSuite (suite_test.go:186) STEP "Deleting kind cluster tide-test @ 21:58:04.886" already removed the cluster. So no pod logs/describe/events exist for run 26849997916.
- timestamp: 2026-06-02 — **FAILURE 1 FIXED, confirmed in CI.** Re-run on commit 96a3b44 (run 26851857098): Layer A now runs Ginkgo-only with flake-attempts=3 and is GREEN — `Ran 29 of 29 Specs in 214.581 seconds` (the 214s vs 44s reflects the retry budget absorbing the contention flake). The flake-attempts split works.
- timestamp: 2026-06-02 — **FAILURE 2 ROOT CAUSE PINNED via dumpControllerDiagnostics() (run 26851857098).** `helm upgrade --install failed after 5m1.4s … context deadline exceeded`. Diagnostics `get pods -n tide-system` shows TWO pods: `tide-controller-manager-...-kg9wf 1/1 Running` (healthy, deployment READY 1/1 AVAILABLE 1) AND `tide-dashboard-795bf6f45d-5l78w 0/1 **ImagePullBackOff**`. helm `--wait` blocks until ALL release resources are Ready; the manager was fine, but the dashboard Deployment never became Ready → 5m deadline. The 5m timeout was a SYMPTOM, not the cause. **NOT a timeout-tightness problem.**
- timestamp: 2026-06-02 — Dashboard image gap CONFIRMED at source: chart `dashboard.enabled: true` by default (charts/tide/values.yaml:240), image `ghcr.io/jsquirrelz/tide-dashboard` (values.yaml:243). But Makefile `test-int-kind-prep` builds/kind-loads only FOUR images (controller, stub-subagent, credproxy, push) — **NOT the dashboard** — and `helmControllerArgs()` overrides only those four (`tag=test`/`IfNotPresent`), leaving the dashboard on its chart-default image which is absent from the fresh CI kind node → ImagePullBackOff. The dashboard IS properly built+loaded+installed by the SEPARATE `make test-e2e-kind` target (Makefile:98-100,112; installs `dashboard.enabled=true`), which is nightly's second step. The Layer B `make test-int` suite (14 controller/CRD reconciliation specs) does not exercise the dashboard at all.

- timestamp: 2026-06-02 — **make test-int FULLY GREEN in CI** (run 26853449717, commit 201ef1c): Layer A `Ran 29 of 29 in 91.4s`, Layer B kind `Ran 14 of 14 in 500.2s`. Failures 1 + 2 both closed. The dashboard-disable fix worked.
- timestamp: 2026-06-02 — **FAILURE 3 surfaced + root-caused (same run).** With test-int green, the SECOND nightly step `make test-e2e-kind` failed: `[BeforeSuite] FAILED` at `test/e2e/kind_setup_test.go:374`, `helm upgrade --install failed after 5m1.2s … context deadline exceeded` → `Ran 0 of 6 Specs` → `--- FAIL: TestKindE2E`. This is a DISTINCT suite (cluster `tide-e2e-phase4`, package test/e2e) that legitimately installs `dashboard.enabled=true` (it tests the dashboard) and has NO diagnostics dump.
- timestamp: 2026-06-02 — **FAILURE 3 ROOT CAUSE (confirmed by source, not hypothesis):** `kindBuildAndLoadImages()` (kind_setup_test.go:320-324) builds the dashboard tag `ghcr.io/jsquirrelz/tide-dashboard:phase4-test` from `-f Dockerfile` — the MANAGER Dockerfile, which produces `/manager`. But the chart's dashboard-deployment runs `/dashboard` as its container command. `Dockerfile.dashboard`'s own header documents this exact trap: "Reusing the manager image (which produces /manager) leaves the container in CrashLoopBackOff with 'exec: /dashboard: not found'." So the dashboard pod CrashLoops → never Ready → helm `--wait` 5m deadline. The dedicated `Dockerfile.dashboard` was created to fix this but `kindBuildAndLoadImages()` was never switched to it (stale Phase-4 "tag the manager image as the dashboard" shim).
- timestamp: 2026-06-02 — Fix is self-contained, NO workflow/npm change needed: the Vite SPA dist IS committed (`git ls-files cmd/dashboard/embed/dist/` = 3 files: index.html + assets/*.js + *.css), so `//go:embed all:dist` (embed.go:39) + `Dockerfile.dashboard`'s `go build ./cmd/dashboard` embed the prebuilt SPA without npm. Nightly workflow has no node/npm and does not need it.
- timestamp: 2026-06-02 — **FAILURE 3 FIX APPLIED** (test-harness only, no chart/workflow change). `kindBuildAndLoadImages()` now builds the manager from `Dockerfile` and the dashboard from `Dockerfile.dashboard` (two distinct `docker build` invocations, both tags still kind-loaded into `tide-e2e-phase4`); stale shim comment replaced. Added `dumpE2EControllerDiagnostics()` (adapted from the integration suite's `dumpControllerDiagnostics()`) called on the `kindApplyChart()` helm-fail path BEFORE `Fail()`, dumping deployments/pods/events + manager AND dashboard logs (current+previous; dashboard selector `control-plane=dashboard` confirmed against charts/tide/templates/dashboard-deployment.yaml) so evidence survives the e2e AfterSuite teardown. Local cheap checks GREEN: `gofmt -l` clean, `go vet -tags=kind_e2e ./test/e2e/...` clean, `go build -o /dev/null ./cmd/dashboard` ok (embed dist intact: 3 files), `go test -tags=kind_e2e -run '^$' ./test/e2e/...` compiles (0.56s), `docker buildx build --check -f Dockerfile.dashboard .` = no warnings, `git diff --quiet charts/` clean (no chart drift).

- timestamp: 2026-06-02 — **FAILURE 3 FIXED, confirmed in CI** (run 26854475599, commit c989625): e2e `[BeforeSuite] PASSED [91.760s]` (`helm install completed in 19.5s` — dashboard pod now Ready), and the 3 dashboard specs PASS (/healthz, /readyz, GET /api/v1/projects). The Dockerfile.dashboard fix closed the CrashLoop. `make test-int` also still green.
- timestamp: 2026-06-02 — **FAILURE 4 surfaced (same run, e2e spec, not setup):** `gate_flow_test.go:106` `[It] reaches Status.Phase=AwaitingApproval once children settle and the gate fires` — `Timed out after 60.001s. Expected <string>: Running to equal AwaitingApproval` ("Milestone did not reach AwaitingApproval — gate hook missing or annotation read broken"). `Ran 4 of 6 Specs: 3 Passed | 1 Failed | 2 Skipped` (2 gate specs skipped via ordered-container fail-fast). The milestone stayed at `Running` the full 60s (spec comment: healthy <15s).
- timestamp: 2026-06-02 — FAILURE 4 leading hypothesis (NOT yet confirmed with runtime evidence — diagnostics-only round chosen): the gate_flow YAML applies ONLY a Project + Milestone (gate_flow_test.go:50-92, no Phase/Plan/Task), so the controller must AUTHOR children by dispatching a planner/subagent Job to reach AwaitingApproval. But the e2e harness `kindBuildAndLoadImages()` loads only controller + dashboard images (kind_setup_test.go:336/346), and `kindApplyChart()` overrides only controller + dashboard image refs — it does NOT load/override stub-subagent/credproxy/tide-push (the integration suite's helmControllerArgs DOES load+override all of these, and its gate flows pass). So the authoring Job likely sits in ImagePullBackOff → milestone never settles → stuck at Running. Same missing-image CLASS as F2/F3, but in the e2e runtime path. NEEDS PROOF: e2e diagnostics currently fire ONLY on the BeforeSuite helm-fail path, not on spec failures, so no Job/pod evidence was captured for this spec.

- timestamp: 2026-06-02 — **DIAGNOSTICS ADDED (observability-only cycle, NO behavior/image fix).** To capture definitive evidence for Failure 4 on the NEXT nightly: added a spec-failure-triggered dump to the kind_e2e suite. New helper `dumpE2ESpecFailureDiagnostics(reason, testNs)` in `test/e2e/spec_diagnostics_test.go` (mirrors the existing `dumpE2EControllerDiagnostics`/`dumpControllerDiagnostics` pattern; writes to stdout so it survives AfterSuite cluster teardown). Wired via an `AfterEach` in `gate_flow_test.go`'s Ordered container guarded by `CurrentSpecReport().Failed()`, scoped to the gate-flow test namespace `tide-e2e-gates` AND controller namespace `tide-system`; it fires BEFORE `AfterAll` deletes the ns. Dumps: `kubectl get jobs,pods -n tide-e2e-gates -o wide` (THE key signal — authoring Job ImagePullBackOff vs Running vs Completed), `describe pods` (pull errors/events), `get events --sort-by=.lastTimestamp`, the full CR ladder `get projects,milestones,phases,plans,tasks,waves -o wide` + `get milestones -o yaml` (so .status conditions reveal WHY it's parked), and `kubectl logs -n tide-system -l control-plane=controller-manager --all-containers --tail=300` (the reconcile decisions). Greppable `=== E2E SPEC-FAILURE DIAGNOSTICS ... ===` header/footer. NO image-loading / chart-arg / timeout / product change; charts/ + hack/helm/ untouched. Cheap local checks GREEN: `gofmt -l` clean, `go vet -tags=kind_e2e ./test/e2e/...` exit 0, `go test -tags=kind_e2e -run '^$' ./test/e2e/...` compiles (0.55s), `git diff --quiet charts/` + `git diff --quiet hack/helm/` both clean. Next: orchestrator triggers a fresh nightly; the captured dump confirms/refutes the ImagePullBackOff-of-authoring-Job hypothesis, then a follow-up cycle applies the real fix.

## Eliminated

- hypothesis: "ART-01 / the kind helm-install is a v1 product regression" — ELIMINATED (pending re-verify): both layers are green locally (Layer A 29/29, Layer B 14/14, full `make test-int` exit 0) and the per-push `test`/`Tests`/`Lint` workflows are green on the same commit 0645e1a. Failures are confined to the cold-runner CI environment.
- hypothesis: "Layer B failed on ImagePullBackOff of the FOUR test-loaded images" — ELIMINATED: the controller/stub-subagent/credproxy/push images ARE kind-loaded by `test-int-kind-prep` and installed `IfNotPresent`; the manager pod ran 1/1 Ready. CORRECTION (run 26851857098): it WAS an ImagePullBackOff after all — but of the FIFTH, un-handled `tide-dashboard` image (not built/loaded by test-int-kind-prep, not overridden by helmControllerArgs), which blocked helm `--wait`. The "5m readiness deadline" framing was the symptom; the dashboard pull failure is the cause.
- hypothesis: "Failure 2 is a too-tight 5m --wait on a cold runner (raise the timeout)" — ELIMINATED: the manager Deployment reached READY 1/1 well within the window; raising the timeout would never help because the dashboard pod can never pull its image on the fresh node. Fix is to remove the dashboard from the Layer B install, not inflate the timeout.

## Resolution

root_cause: |
  Two independent CI-harness defects, both confined to the cold 2-core ubuntu-latest
  nightly runner (neither reproduces on the local dev VM; per-push CI green on 0645e1a).
  FAILURE 1 (Layer A ART-01 flake): nightly's full `make test-int` ran the envtest layer
  via a single `go test ./test/integration/...` that could NOT carry
  `-ginkgo.flake-attempts` (the kind package bundles plain go-tests, so the flag is
  invalid on the mixed set). The per-push `test-int-fast` already guards this
  contention-flaky class with flake-attempts=3; nightly Layer A had no such retry, so one
  Eventually-timeout flake on a contended runner failed the whole package.
  FAILURE 2 (Layer B helm --wait timeout): PINNED. The dumpControllerDiagnostics() output
  on the next nightly (run 26851857098, commit 96a3b44) showed the manager Deployment was
  healthy (tide-controller-manager 1/1 Running, READY 1/1) while a SECOND release pod,
  tide-dashboard, sat at 0/1 ImagePullBackOff. The chart defaults dashboard.enabled=true
  (charts/tide/values.yaml:240) on image ghcr.io/jsquirrelz/tide-dashboard, but Makefile
  test-int-kind-prep builds + kind-loads only the four controller-side images (controller,
  stub-subagent, credproxy, push) and helmControllerArgs() overrides only those four — the
  dashboard image is never present on the fresh CI kind node. helm `--wait` blocks until
  ALL release resources are Ready, so the unpullable dashboard pod held the install open
  to the 5m deadline. The 5m timeout was the SYMPTOM, not the cause; raising it would never
  help. The dashboard is exercised only by the separate `make test-e2e-kind` target (which
  builds/loads/installs it); the Layer B suite's 14 controller/CRD specs never touch it.
  FAILURE 3 (test-e2e-kind BeforeSuite helm --wait timeout): the e2e suite's
  kindBuildAndLoadImages() built the dashboard tag from `-f Dockerfile` (the MANAGER
  Dockerfile, which ships /manager), but the chart's dashboard-deployment runs
  /dashboard. The dashboard pod CrashLoopBackOff'd ("exec: /dashboard: not found"),
  never became Ready, and held helm `--wait` open to the 5m deadline. The dedicated
  Dockerfile.dashboard (built precisely to fix this; binary /dashboard embeds the
  committed Vite SPA via //go:embed all:dist) existed but kindBuildAndLoadImages() was
  never switched to it — a stale Phase-4 "tag the manager image as the dashboard" shim.
  Unlike Failure 2, the dashboard is NOT disabled here: the e2e suite legitimately tests
  the dashboard, so the fix is to build the correct image, not to disable it.
fix: |
  FAILURE 1 (thorough, no quarantine): split `make test-int` (Makefile) into TWO separate
  go-test invocations under one `set -e` shell — (a) Layer A envtest as a Ginkgo-ONLY call
  carrying `-ginkgo.flake-attempts=3 --ginkgo.label-filter=envtest` (identical protection to
  test-int-fast), and (b) Layer B kind as its own `go test ./test/integration/kind/...`
  WITHOUT the flag (mixed Ginkgo + plain-go contract tests). A failure in either still fails
  the target. Confirmed GREEN in CI run 26851857098 (Layer A `Ran 29 of 29 Specs in 214.581s`).
  FAILURE 2 (thorough, scoped, test-harness-only): added `--set dashboard.enabled=false` to
  helmControllerArgs() in test/integration/kind/suite_test.go (with a code comment explaining
  WHY: the dashboard image is not built/loaded by test-int-kind-prep; the dashboard belongs to
  the e2e suite). This removes the unpullable dashboard pod from the Layer B release so helm
  `--wait` completes once the manager Deployment is Ready. The chart default
  (dashboard.enabled=true) is intentionally LEFT UNTOUCHED — it is correct for real installs and
  for `make test-e2e-kind`. dumpControllerDiagnostics() (added the prior cycle) is KEPT in place
  as durable future-proofing; it was the tool that pinned this root cause.
  FAILURE 3 (thorough, test-harness only — NO chart/workflow/npm change): in
  test/e2e/kind_setup_test.go, split kindBuildAndLoadImages() into two docker builds —
  manager from `Dockerfile` (controller:phase4-test) and dashboard from
  `Dockerfile.dashboard` (ghcr.io/jsquirrelz/tide-dashboard:phase4-test) — keeping both
  tags kind-loaded into tide-e2e-phase4. Replaced the now-inaccurate "tag the manager
  image as the dashboard" comment block with a note that the dashboard builds from
  Dockerfile.dashboard (binary /dashboard; embeds the committed SPA via //go:embed
  all:dist). Added dumpE2EControllerDiagnostics() (mirrors the integration suite's
  dumpControllerDiagnostics()) on the helm-install failure path, dumping
  deployments/pods/events + manager and dashboard logs (current+previous) BEFORE Fail()
  so evidence survives the e2e AfterSuite teardown (the workflow log-collection step only
  targets tide-test). The chart and Dockerfile.dashboard themselves are untouched.
verification: |
  Local cheap checks (where Failure 2 does NOT reproduce — heavy kind run skipped to avoid VM
  OOM): gofmt clean on suite_test.go; `go vet ./test/integration/kind/...` clean; kind test
  binary compiles (`go test -run '^$' ./test/integration/kind/...` ok, 0.589s); `git diff
  --quiet charts/` CLEAN (no chart drift — fix is test-harness-only). FAILURE 1 already proven
  GREEN in CI (run 26851857098, Layer A 29/29). FAILURE 2 fix shape validated by the pinned
  diagnostics; CI BAR (pending — run by orchestrator/user): a fresh full nightly-integration.yml
  run GREEN end-to-end on GitHub-hosted runners across BOTH steps — Layer B `make test-int` AND
  `make test-e2e-kind`. Trigger via `gh workflow run nightly-integration.yml --ref main` and
  watch both steps. If anything recurs, dumpControllerDiagnostics() output will disambiguate.
files_changed:
  - Makefile (test-int split: flake-guarded Layer A + separate Layer B) [prior cycle, commit 96a3b44]
  - test/integration/kind/suite_test.go (dumpControllerDiagnostics on helm-fail path [prior cycle]; --set dashboard.enabled=false [this cycle])
  - test/e2e/kind_setup_test.go (kindBuildAndLoadImages builds dashboard from Dockerfile.dashboard; dumpE2EControllerDiagnostics on helm-fail path) [Failure 3, this cycle]
