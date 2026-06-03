---
slug: nightly-int-flake-timeout
status: fix-applied-pending-ci-verify
trigger: "Nightly-integration workflow (run 26849997916, commit 0645e1a) is RED on two distinct CI-harness failures in `make test-int`. Neither reproduces locally (local: Layer A 29/29 + Layer B 14/14 green). Fix so nightly runs green; this gates the v1.0.0 release (tag held local-only at 8a8e843). FAILURE 1 — Layer A envtest flake (1 of 29): init_test.go:101 ART-01 'creates a tide-init-{UID} Job on the first reconcile' failed under CI contention; 28/29 passed. The per-push path (make test-int-fast) guards this contention-flaky class with -ginkgo.flake-attempts=3, but nightly's full make test-int runs the envtest layer WITHOUT flake-attempts (mixed go-test package — flag breaks non-Ginkgo pkgs). FAILURE 2 — Layer B kind BeforeSuite (suite_test.go:446): helm upgrade --install ... --wait --timeout 5m -> context deadline exceeded at 5m1s. Images ARE built + kind-loaded by Makefile test-int-kind-prep and installed pullPolicy=IfNotPresent (NOT ImagePull). Controller Deployment didn't reach Ready within 5m on the cold 2-core ubuntu-latest runner. OBSERVATION GAP: cannot tell 5m-too-tight vs real pod failure — post-failure `kind export logs` artifact was EMPTY because the suite's AfterSuite (suite_test.go:186) deletes the tide-test cluster BEFORE the workflow's failure-collection step (nightly-integration.yml:94-101) runs."
created: 2026-06-02
updated: 2026-06-03
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
- SIX failures total; all root-caused with evidence. Failures 1-5 are test-harness fixes (1/2/3 CI-confirmed green; 4 CI-confirmed working in run 26858194135 at 5 of 6 specs; 5 applied but NOT YET CI-verified because run 26858915802 died at `Install kind v0.31.0` BEFORE any test ran). Failure 6 is a CI-INFRA flake (not a test bug): the bare single-attempt `curl -Lo ./kind https://kind.sigs.k8s.io/dl/...` in nightly-integration.yml received 0 bytes on a transient CDN/redirect stall and failed the whole nightly with no retry/--fail. Fix APPLIED this cycle: hardened the kind-binary download with `curl -fsSL --retry 5 --retry-delay 3 --retry-all-errors --retry-connrefused --connect-timeout 30` + a `test -s ./kind` non-empty check before install (see Resolution -> Failure 6). Awaiting a fresh nightly that finally exercises BOTH the hardened download AND the F5 selector fix end-to-end.

next_action: Orchestrator triggers a fresh `nightly-integration.yml` run (`gh workflow run nightly-integration.yml --ref main`) and watches the `Install kind v0.31.0` step to green THEN both test steps (`make test-int` AND `make test-e2e-kind`) to green. The F6 fix is a workflow-only curl-hardening change in .github/workflows/nightly-integration.yml; the only bare remote download in the file (checkout/setup-go/setup-helm/upload-artifact are all pinned actions). This run is the FIRST to actually exercise the F5 selector fix (`control-plane=dashboard`) because every prior run either preceded F5 or died before tests ran. If green end-to-end: ALL six failures close, this debug session closes, and the v1.0.0 tag (held local-only at 8a8e843) is clear to ship. dumpControllerDiagnostics / dumpE2ESpecFailureDiagnostics hooks stay in place if any further blocker surfaces.

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

- timestamp: 2026-06-03 — **FAILURE 4 ROOT CAUSE PROVEN** via dumpE2ESpecFailureDiagnostics (run 26857361275, commit 8462595). The Milestone DID dispatch its planner Job (status: `message: Planner Job tide-milestone-...-1 dispatched`, `phase: Running` — controller logic is CORRECT), but the Job is stuck `Running 0/1` because: `Warning FailedCreate … pods "tide-milestone-...-1-" is forbidden: error looking up service account tide-e2e-gates/tide-subagent: serviceaccount "tide-subagent" not found`. So the planner Job cannot create its pod → milestone never settles → stuck at Running 60s. **My ImagePullBackOff hypothesis was WRONG as the FIRST blocker** — the actual first blocker is the missing `tide-subagent` ServiceAccount in the Project's namespace. (The Job image IS the chart-default `ghcr.io/jsquirrelz/tide-stub-subagent:1.0.0`, which the e2e harness does NOT load — so ImagePullBackOff would be the NEXT blocker once the SA exists. Both must be fixed.) This is why diagnostics-first mattered.
- timestamp: 2026-06-03 — Complete F4 requirement set (from internal/dispatch/podjob/jobspec.go): a subagent Job in a Project namespace needs FOUR namespace-scoped resources the chart only creates in `.Release.Namespace` (tide-system): `tide-subagent` SA (jobspec.go:63,403), `tide-projects` PVC (jobspec.go:124, backend.go:200), `tide-signing-key` Secret (jobspec.go:294, credproxy sidecar envFrom), and the loadable subagent + credproxy images. The INTEGRATION suite already provides all of these for its namespace via `ensureSubagentSA(ns)` (suite_test.go:642), `ensureProjectsPVC(ns)` (:663) + PVC prewarm, the tide-signing-key mirror (:180), and test-int-kind-prep image load + helmControllerArgs `images.stubSubagent.tag=test`/`images.credProxy.tag=test` overrides. The e2e gate_flow test creates only the `tide-e2e-gates` namespace + the Project/Milestone — NONE of the subagent wiring. F4 fix = mirror the integration suite's per-namespace subagent wiring into the gate_flow test namespace + build/load/override stub-subagent + credproxy images in the e2e harness.

- timestamp: 2026-06-03 — **FAILURE 4 FIX APPLIED** (test-harness only — NO chart/workflow/hack/helm change). Confirmed jobspec.go requirements at source before editing: PodSpec.ServiceAccountName = ServiceAccountSubagent "tide-subagent" (jobspec.go:403, const :63); PVC ClaimName = opts.PVCName "tide-projects" (jobspec.go:389); credproxy native-sidecar injected because the gate_flow Project sets providerSecretRef (credproxyEnabled gate jobspec.go:271), and its envFrom references the `tide-signing-key` Secret (jobspec.go:294). (A) `test/e2e/kind_setup_test.go`: new exported-within-package helper `kindE2EEnsureSubagentWiring(ns)` that provisions the tide-subagent SA (`kindE2EEnsureSubagentSA`), tide-projects PVC (`kindE2EEnsureProjectsPVC`) + ClaimBound prewarm via a busybox pause Pod (`kindE2EPVCPrewarm`, mirrors the integration suite's pvcPrewarmPod for kind's WaitForFirstConsumer local-path provisioner), and the tide-signing-key Secret mirrored from `tide-system` into the target ns (`kindE2EEnsureSigningKeySecret`). (B) `kindBuildAndLoadImages()` refactored into a table of {tag, dockerfile} builds that now ALSO builds + kind-loads the stub-subagent (images/stub-subagent/Dockerfile) and credproxy (images/credproxy/Dockerfile) images at the shared `:phase4-test` tag (new const `kindE2EImageTag`), alongside the existing manager (Dockerfile) + dashboard (Dockerfile.dashboard) builds. (C) `kindApplyChart()` adds `--set images.stubSubagent.tag=phase4-test --set images.stubSubagent.pullPolicy=IfNotPresent` and the same for `images.credProxy`, so the dispatched authoring Job uses the kind-loaded images instead of the chart-default `:<appVersion>` refs absent on the fresh CI node. (D) `gate_flow_test.go` BeforeAll calls `kindE2EEnsureSubagentWiring(testNamespace)` right after the `tide-e2e-gates` namespace is created and before the Project/Milestone apply. The `dumpE2ESpecFailureDiagnostics` AfterEach hook is KEPT. Cheap local checks GREEN: `gofmt -l` clean on all three files, `go vet -tags=kind_e2e ./test/e2e/...` exit 0, `go test -tags=kind_e2e -run '^$' ./test/e2e/...` compiles (0.56s), `git diff --quiet charts/` + `git diff --quiet hack/helm/` both clean. Heavy kind suite NOT run locally (OOM, non-reproducing). Next: orchestrator triggers a fresh nightly to confirm gate_flow specs reach green; if a new blocker surfaces the spec-failure diagnostics will show it.

- timestamp: 2026-06-03 — **FAILURE 4 FIX CONFIRMED WORKING + FAILURE 5 surfaced** (run 26858194135, commit f93e074). The subagent wiring fix worked: `Ran 6 of 6 Specs: 5 Passed | 1 Failed` (was 4 of 6 / 3 passed). Both gate-approval specs now PASS — the milestone Job ran to `Complete 1/1` using `stub-subagent:phase4-test` + `credproxy:phase4-test` sidecar; init pod Completed; phases dispatched. The CR cascade works end-to-end. ONE failure left.
- timestamp: 2026-06-03 — **FAILURE 5 (last blocker) root-caused at source.** `gate_flow_test.go:177` (`It tide tail streams a pod log and cancels within 1s of SIGINT (Pitfall 25)`): `Expect(podName).NotTo(BeEmpty())` failed — `no dashboard Pod found for tail-cancel smoke`. The spec locates the dashboard pod via `kindGetFirstPodName(kindE2EControllerNamespace="tide-system", "app.kubernetes.io/name=tide-dashboard")` (gate_flow_test.go:158,235). ROOT CAUSE: `helm template charts/tide --set dashboard.enabled=true` shows the dashboard pod template has a DUPLICATE `app.kubernetes.io/name` key — explicit `tide-dashboard` followed by `tide` from the `tide.labels` helper; YAML last-key-wins so the pod's effective label is `app.kubernetes.io/name=tide`, NOT `tide-dashboard`. So the test selector matches zero pods. The Deployment works (its matchLabels is self-consistent under last-wins) and the 3 dashboard specs pass (they reach it via `svc/tide-dashboard`, not a pod-label lookup). This is a TEST-HARNESS selector bug, not a product/chart defect.
- timestamp: 2026-06-03 — FAILURE 5 fix decision: change ONLY the test selector to `control-plane=dashboard` (uniquely identifies the dashboard pod; the manager is `control-plane=controller-manager`). Do NOT change the chart: (a) `app.kubernetes.io/name=tide` + `control-plane=<component>` is a valid Helm convention; (b) chart is a FIXED contract per CLAUDE.md; (c) a Deployment's selector is immutable — changing it would force a release reinstall and risks the helm-rbac-assert / contract tests. The duplicate `app.kubernetes.io/name` key in dashboard-deployment.yaml is a BENIGN chart smell (the dead `tide-dashboard` value) — noted as optional future cleanup, NOT fixed here.
- timestamp: 2026-06-03 — **FAILURE 5 FIX APPLIED** (test-harness only — NO chart/workflow/hack/helm change). Verified at source via `helm template charts/tide --set dashboard.enabled=true`: the dashboard pod template (`spec.template.metadata.labels`) carries `control-plane: dashboard` as a UNIQUE non-clobbered label, while its `app.kubernetes.io/name: tide-dashboard` is immediately overridden to `tide` by the `tide.labels` helper (YAML last-key-wins). The manager pod carries `control-plane: controller-manager`, so `control-plane=dashboard` is unambiguous. CHANGE: in `test/e2e/gate_flow_test.go` the dashboard-pod lookup selector at the `tide tail` spec changed from `app.kubernetes.io/name=tide-dashboard` to `control-plane=dashboard`, with an 8-line comment explaining WHY (helper override + last-wins + unique discriminator). One-line selector swap; no behavior change to the SIGINT-cancel assertion. The chart and hack/helm/ are UNTOUCHED — the duplicate `app.kubernetes.io/name` key in dashboard-deployment.yaml remains as documented benign future-cleanup. Cheap local checks GREEN: `gofmt -l test/e2e/gate_flow_test.go` clean, `go vet -tags=kind_e2e ./test/e2e/...` exit 0, `go test -tags=kind_e2e -run '^$' ./test/e2e/...` compiles (0.53s), `git diff --quiet charts/` + `git diff --quiet hack/helm/` both clean. Heavy kind suite NOT run locally (OOM, non-reproducing). `dumpE2ESpecFailureDiagnostics` AfterEach hook KEPT. Next: fresh nightly confirms all 6 gate_flow specs green → suite fully green → debug session closes → v1.0.0 release gate clears.

- timestamp: 2026-06-03 — **FAILURE 6 (infra flake, NOT a test failure)** in run 26858915802 (commit 420f952): the `Install kind v0.31.0` workflow step FAILED at 5m11s before any test ran. `curl -Lo ./kind https://kind.sigs.k8s.io/dl/v0.31.0/kind-linux-amd64` received 0 bytes the entire time (`0:00:07+ ... 0 0 0` — host unreachable/stalled). Transient network failure downloading the kind binary from kind.sigs.k8s.io; unrelated to the F1-F5 test-harness fixes (tests never executed). The bare single-attempt `curl` (nightly-integration.yml:50-55) has no retry/--fail, so a momentary CDN/redirect blip fails the whole nightly. FIX: harden the kind-install curl with retries + --fail (CI-reliability, same theme as this session). The F5 fix is unverified by this run (kind never installed) — the hardened re-run also re-tests F5.

## Eliminated

- hypothesis: "ART-01 / the kind helm-install is a v1 product regression" — ELIMINATED (pending re-verify): both layers are green locally (Layer A 29/29, Layer B 14/14, full `make test-int` exit 0) and the per-push `test`/`Tests`/`Lint` workflows are green on the same commit 0645e1a. Failures are confined to the cold-runner CI environment.
- hypothesis: "Layer B failed on ImagePullBackOff of the FOUR test-loaded images" — ELIMINATED: the controller/stub-subagent/credproxy/push images ARE kind-loaded by `test-int-kind-prep` and installed `IfNotPresent`; the manager pod ran 1/1 Ready. CORRECTION (run 26851857098): it WAS an ImagePullBackOff after all — but of the FIFTH, un-handled `tide-dashboard` image (not built/loaded by test-int-kind-prep, not overridden by helmControllerArgs), which blocked helm `--wait`. The "5m readiness deadline" framing was the symptom; the dashboard pull failure is the cause.
- hypothesis: "Failure 2 is a too-tight 5m --wait on a cold runner (raise the timeout)" — ELIMINATED: the manager Deployment reached READY 1/1 well within the window; raising the timeout would never help because the dashboard pod can never pull its image on the fresh node. Fix is to remove the dashboard from the Layer B install, not inflate the timeout.
- hypothesis: "Failure 4 is an ImagePullBackOff of the dispatched authoring Job (the FIRST blocker)" — ELIMINATED as the FIRST blocker (run 26857361275 proof): the planner Job pod never even got created — the `tide-subagent` ServiceAccount was missing in `tide-e2e-gates`, so the Job sat `Running 0/1` with `FailedCreate … serviceaccount "tide-subagent" not found` before any image pull was attempted. ImagePullBackOff would have been the NEXT blocker once the SA existed (the chart-default `:1.0.0` subagent/credproxy images are not on the node). The F4 fix addresses BOTH (SA+PVC+Secret wiring AND the image load+override), so neither blocker resurfaces.

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
  FAILURE 4 (test-e2e-kind gate_flow spec parks Milestone at Running): PROVEN via
  dumpE2ESpecFailureDiagnostics (run 26857361275, commit 8462595). The gate_flow fixture
  applies ONLY a Project + Milestone, so the controller must AUTHOR children by dispatching
  a planner Job into the Project's namespace (tide-e2e-gates) to reach AwaitingApproval.
  The controller did this correctly (Milestone status `message: Planner Job ... dispatched`,
  phase Running), but the Job sat `Running 0/1` with `Warning FailedCreate … pods is
  forbidden: error looking up service account tide-e2e-gates/tide-subagent: serviceaccount
  "tide-subagent" not found`. A subagent Job (jobspec.go) needs FOUR namespace-scoped
  resources the chart only templates in .Release.Namespace (tide-system): the tide-subagent
  SA (jobspec.go:63/:403 — the PROVEN first blocker), the tide-projects PVC (jobspec.go:389),
  the tide-signing-key Secret (jobspec.go:294, credproxy sidecar envFrom — the gate_flow
  Project has providerSecretRef so credproxy IS injected, cascade-13), and loadable
  stub-subagent + credproxy images. The e2e harness created the test namespace + Project/
  Milestone but NONE of this wiring, while the integration suite already provides all of it
  for its own namespace (ensureSubagentSA / ensureProjectsPVC + prewarm / ensureSigningKeySecret
  / test-int-kind-prep image load + helmControllerArgs overrides). This is the SAME
  cross-namespace-Project gap as the integration suite once had, just in the e2e runtime path.
  The chart correctly scopes these resources to .Release.Namespace for real single-namespace
  installs; wiring a cross-namespace test Project is the test harness's responsibility.
  FAILURE 5 (test-e2e-kind gate_flow `tide tail` spec finds no dashboard Pod): root-caused
  at source via `helm template charts/tide --set dashboard.enabled=true`. The spec located the
  dashboard pod by selector `app.kubernetes.io/name=tide-dashboard`, but the dashboard pod
  template (spec.template.metadata.labels) sets `app.kubernetes.io/name: tide-dashboard`
  immediately followed by `app.kubernetes.io/name: tide` injected by the `tide.labels` helper.
  YAML last-key-wins, so the RUNNING pod's effective label is `app.kubernetes.io/name=tide` —
  the selector matched ZERO pods, `kindGetFirstPodName` returned "", and the NotTo(BeEmpty)
  assertion failed ("no dashboard Pod found"). This only surfaced NOW because F4's wiring fix
  let specs 1+2 pass, so the Ordered container no longer fail-fast-skipped this spec. The
  Deployment itself works (its matchLabels is self-consistent under last-wins) and the 3
  dashboard specs pass because they reach the dashboard via svc/tide-dashboard, not a pod-label
  lookup. This is a TEST-HARNESS selector bug. The duplicate `app.kubernetes.io/name` key in
  charts/tide/templates/dashboard-deployment.yaml (and its hack/helm/ source) is a BENIGN chart
  smell (the dead tide-dashboard value) — optional future cleanup, NOT a product/chart defect.
  FAILURE 6 (nightly CI-infra flake, NOT a test/product defect): run 26858915802
  (commit 420f952) failed at the `Install kind v0.31.0` workflow step after 5m11s,
  BEFORE any test executed. `curl -Lo ./kind https://kind.sigs.k8s.io/dl/v0.31.0/
  kind-linux-amd64` (nightly-integration.yml, the sole bare remote download) received
  0 bytes the entire time — kind.sigs.k8s.io was unreachable/stalled from the runner on
  a transient CDN/redirect blip. The single-attempt curl had no `--retry` and no `--fail`,
  so one momentary network glitch failed the whole nightly. This is orthogonal to the
  F1-F5 test-harness fixes (tests never ran), so the F5 selector fix is also still
  CI-unverified by this run. STACK.md's "pin kind node images by @sha256" guidance is
  about the kind NODE image at cluster-create, NOT this kind BINARY download, so node-image
  pinning is left alone; curl-retry on the binary fetch is the correct, minimal fix.
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
  so evidence survives the e2e AfterSuite teardown. The chart and Dockerfile.dashboard
  themselves are untouched.
  FAILURE 4 (thorough, test-harness only — NO chart/workflow/hack/helm change): mirror the
  integration suite's per-namespace subagent wiring into the e2e gate_flow path.
  (A) In test/e2e/kind_setup_test.go, added kindE2EEnsureSubagentWiring(ns) and its four
  sub-helpers — kindE2EEnsureSubagentSA (tide-subagent SA), kindE2EEnsureProjectsPVC
  (tide-projects RWO PVC), kindE2EPVCPrewarm (busybox pause Pod that mounts the PVC and waits
  for ClaimBound, compensating for kind's WaitForFirstConsumer local-path provisioner; idempotent
  no-op if already Bound), and kindE2EEnsureSigningKeySecret (reads the helm-created
  tide-signing-key from tide-system and applies a copy into the target ns). Ported field-for-
  field from the integration suite's ensureSubagentSA / ensureProjectsPVC / pvcPrewarmPod /
  ensureSigningKeySecret, adapted to the kindE2E* names (kindE2EKubeconfigPath, kindE2ECtx,
  kindE2EClient).
  (B) Refactored kindBuildAndLoadImages() into a {tag, dockerfile} build table that now ALSO
  builds + kind-loads the stub-subagent (images/stub-subagent/Dockerfile) and credproxy
  (images/credproxy/Dockerfile) images at a shared `:phase4-test` tag (new const
  kindE2EImageTag), alongside the existing manager + dashboard builds.
  (C) kindApplyChart() now also passes `--set images.stubSubagent.tag=phase4-test
  --set images.stubSubagent.pullPolicy=IfNotPresent` and the same for `images.credProxy`, so
  the dispatched authoring Job uses the kind-loaded images instead of the chart-default
  `:<appVersion>` refs absent on the fresh CI node.
  (D) gate_flow_test.go BeforeAll calls kindE2EEnsureSubagentWiring(testNamespace) right after
  the tide-e2e-gates namespace is created and before the Project/Milestone apply.
  The dumpE2ESpecFailureDiagnostics AfterEach hook is KEPT (durable future-proofing; it proved
  the root cause). The chart and hack/helm/ are untouched — cross-namespace test wiring is the
  harness's job, exactly as the integration suite handles it.
  FAILURE 5 (thorough, test-harness only — NO chart/workflow/hack/helm change): in
  test/e2e/gate_flow_test.go, changed the dashboard-pod lookup selector in the `tide tail`
  spec from `app.kubernetes.io/name=tide-dashboard` to `control-plane=dashboard`. That label
  uniquely identifies the dashboard pod (the manager pod carries control-plane=controller-manager,
  so the selector is unambiguous) and is NOT clobbered by the tide.labels helper. Added an
  8-line comment explaining WHY (the app.kubernetes.io/name label resolves to "tide" via the
  helper override + YAML last-wins; control-plane is the unique discriminator). One-line selector
  swap — no behavior change to the SIGINT-cancel assertion. The chart is UNTOUCHED: the
  app.kubernetes.io/name=tide + control-plane=<component> labeling is a valid Helm convention,
  the chart is a FIXED contract, and a Deployment selector is immutable (changing it would force
  a release reinstall and risk the helm-rbac-assert / helm-template contract tests). The dead
  tide-dashboard key in dashboard-deployment.yaml is noted as benign future cleanup, not fixed here.
  The dumpE2ESpecFailureDiagnostics AfterEach hook is KEPT.
  FAILURE 6 (CI-reliability hardening, workflow-only — NO charts/ / hack/helm/ / ci.yaml
  change): hardened the `Install kind v0.31.0` step in .github/workflows/nightly-integration.yml.
  The download is now `curl -fsSL --retry 5 --retry-delay 3 --retry-all-errors
  --retry-connrefused --connect-timeout 30 -o ./kind https://kind.sigs.k8s.io/dl/v0.31.0/
  kind-linux-amd64` — `--fail` errors on HTTP>=400 (instead of writing an HTML error body
  to ./kind), the retry flags ride out transient CDN/redirect/connrefused blips (5 attempts,
  3s apart, all error classes), `--connect-timeout 30` bounds a stalled connect, and `-L` is
  kept for the release redirect. A `test -s ./kind` non-empty check guards the install before
  `chmod +x` / `sudo mv`, and the existing `kind version` post-install smoke is kept. The kind
  VERSION stays v0.31.0 (STACK.md pin). Scanned the rest of the workflow: this is the only bare
  single-attempt remote download — checkout/setup-go/setup-helm/upload-artifact are pinned
  GitHub Actions with their own resilience, so no other step needed hardening. The cert-manager
  apply inside the suites is in test code via kubectl (out of scope, has its own handling).
verification: |
  Local cheap checks (where Failures 2/3/4 do NOT reproduce — heavy kind run skipped to avoid
  VM OOM): FAILURE 4 cycle — `gofmt -l test/e2e/{kind_setup_test.go,gate_flow_test.go,
  spec_diagnostics_test.go}` clean; `go vet -tags=kind_e2e ./test/e2e/...` exit 0;
  `go test -tags=kind_e2e -run '^$' ./test/e2e/...` compiles (0.56s); `git diff --quiet charts/`
  + `git diff --quiet hack/helm/` both CLEAN (test-harness-only). FAILURES 1/2/3 already proven
  GREEN in CI (run 26851857098 Layer A 29/29; run 26853449717 make test-int fully green; run
  26854475599 e2e BeforeSuite PASSED + 3 dashboard specs pass). FAILURE 4 fix shape validated by
  the run-26857361275 diagnostics (missing SA proven), then CONFIRMED WORKING in run 26858194135
  (commit f93e074): Ran 6 of 6 Specs, 5 Passed | 1 Failed — both gate-approval specs pass, only
  Failure 5 left. FAILURE 5 cycle — root cause verified by `helm template charts/tide --set
  dashboard.enabled=true` (dashboard pod effective label app.kubernetes.io/name=tide; unique
  control-plane=dashboard); `gofmt -l test/e2e/gate_flow_test.go` clean, `go vet -tags=kind_e2e
  ./test/e2e/...` exit 0, `go test -tags=kind_e2e -run '^$' ./test/e2e/...` compiles (0.53s),
  `git diff --quiet charts/` + `git diff --quiet hack/helm/` both CLEAN. CI BAR (pending — run by orchestrator/user):
  a fresh full nightly-integration.yml run GREEN end-to-end on GitHub-hosted runners across BOTH
  steps — Layer B `make test-int` AND `make test-e2e-kind` (gate_flow specs reach AwaitingApproval
  → approve → leave AwaitingApproval). Trigger via `gh workflow run nightly-integration.yml --ref
  main` and watch both steps. If anything recurs, dumpControllerDiagnostics() /
  dumpE2ESpecFailureDiagnostics() output will disambiguate.
  FAILURE 6 cycle (workflow-only): `python3 -c "import yaml; yaml.safe_load(open('.github/
  workflows/nightly-integration.yml'))"` parses OK; `grep -nE 'curl|wget'` confirms the
  hardened kind curl is the only remote download; `git diff --quiet charts/`, `git diff --quiet
  hack/helm/`, `git diff --quiet .github/workflows/ci.yaml` all CLEAN (nightly workflow only).
  actionlint/yamllint not installed on this host (optional). No Go changes (tests never ran in
  the failing run, so no test-harness change is implicated by F6).
files_changed:
  - Makefile (test-int split: flake-guarded Layer A + separate Layer B) [Failure 1, commit 96a3b44]
  - test/integration/kind/suite_test.go (dumpControllerDiagnostics on helm-fail path; --set dashboard.enabled=false) [Failures 1/2, prior cycles]
  - test/e2e/kind_setup_test.go (Failure 3: dashboard from Dockerfile.dashboard + dumpE2EControllerDiagnostics; Failure 4: kindE2EEnsureSubagentWiring SA+PVC+prewarm+signing-key helpers, stub-subagent+credproxy image build/load table, images.stubSubagent/credProxy chart overrides)
  - test/e2e/spec_diagnostics_test.go (dumpE2ESpecFailureDiagnostics spec-failure dump) [Failure 4 diagnostics, prior cycle]
  - test/e2e/gate_flow_test.go (Failure 4: BeforeAll calls kindE2EEnsureSubagentWiring before Project apply; AfterEach spec-failure dump kept. Failure 5: dashboard-pod lookup selector changed app.kubernetes.io/name=tide-dashboard -> control-plane=dashboard with explanatory comment)
  - .github/workflows/nightly-integration.yml (Failure 6: harden `Install kind v0.31.0` curl with --fail/--retry/--connect-timeout + non-empty download check; workflow-only CI-reliability fix)
