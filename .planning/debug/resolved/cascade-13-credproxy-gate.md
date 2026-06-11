---
slug: cascade-13-credproxy-gate
status: resolved
trigger: "Phase 7 $0 acceptance (make acceptance-v1-smoke) stalls at Project=Running: the credproxy native-sidecar init container is injected unconditionally (jobspec.go:365) but exits 1 via requireEnv(ANTHROPIC_API_KEY) (jobspec.go:289 only wires the key when Project.Spec.ProviderSecretRef != \"\"). The $0 small-project has no providerSecretRef → credproxy CrashLoopBackOff → planner pod never runs."
created: 2026-05-31
updated: 2026-05-31
phase: 07-project-to-milestone-authoring-and-self-bootstrap
---

# Debug: cascade-13-credproxy-gate

## Symptoms

- **Expected:** `make acceptance-v1-smoke` (ACCEPTANCE_SAMPLE=small) drives bare `small-project` to `status.phase=Complete` at $0 with no API key (REQ-6).
- **Actual:** Project stalls at `Running`; script `kubectl wait ... --timeout=10m` fails (`error: timed out waiting for the condition on projects/small-project`, ACC2_EXIT=2). The project planner pod `tide-project-<uid>-1-*` is `Init:CrashLoopBackOff`.
- **Error (credproxy init container log):** `{"level":"error","caller":"credproxy/main.go:156","msg":"required environment variable not set","var":"ANTHROPIC_API_KEY"}` → `exit 1`.
- **Timeline:** Surfaced 2026-05-31 on the $0 acceptance AFTER cascade-12 (chart image-tag) was fixed — cascade-12's ImagePullBackOff previously masked this. Second masked onion-layer (CLAUDE.md "don't predict chain terminator").
- **Reproduction:** `ACCEPTANCE_SAMPLE=small make acceptance-v1-smoke` (fresh cluster, no ANTHROPIC_API_KEY).

## Root cause (confirmed)

`internal/dispatch/podjob/jobspec.go`:
- Line ~365: `InitContainers: []corev1.Container{envelopeWriter, credproxy}` — credproxy native sidecar injected UNCONDITIONALLY.
- Line ~289: credproxy gets `ANTHROPIC_API_KEY` via `EnvFrom SecretRef` ONLY `if opts.Project != nil && opts.Project.Spec.ProviderSecretRef != ""`.
- `cmd/credproxy/main.go:156` `requireEnv("ANTHROPIC_API_KEY")` → `exit 1` when absent.
- The $0 `examples/projects/small/project.yaml` has NO `providerSecretRef`; the acceptance script's `small` branch creates NO provider secret (only `large` mode creates `tide-secrets`). → credproxy starts with no key → crash → native-sidecar gate blocks the subagent container → planner pod never completes → no Milestone authored → Project stuck at Running.
- Layer B's bare-project test passes only because cascade-8 added a DUMMY `tide-provider-secret` + `providerSecretRef` to its fixture; the $0 acceptance path deliberately has none.

## Approved fix (Option A — user-confirmed)

Gate the credproxy sidecar injection on `Project.Spec.ProviderSecretRef`. In `jobspec.go`, include the `credproxy` container in `InitContainers` ONLY when `opts.Project != nil && opts.Project.Spec.ProviderSecretRef != ""`. When there is no provider secret ($0 stub mode), there is nothing for the D-C1 provider firewall to proxy, so skip credproxy entirely; the planner pod runs `envelope-writer` (init) + the stub subagent only. Rationale: a project with no providerSecretRef cannot make real provider calls anyway, so the firewall is moot — this is consistent with D-C1, honors REQ-6 (no API key, no acceptance-script/sample edits), and has zero blast radius on Layer B (all Layer B fixtures carry a providerSecretRef post-cascade-8, so credproxy is still injected there).

Implementation notes for the fix:
- Only the `InitContainers` membership of `credproxy` is conditional. The subagent container's `ANTHROPIC_BASE_URL`→localhost:8443 + `ANTHROPIC_API_KEY=SignedToken` env (jobspec.go ~326-329) is harmless for the stub (it makes no provider call), but consider whether to also skip those subagent env overrides / the shared cert volume + mount when credproxy is absent, to keep the PodSpec coherent. Keep the change minimal and self-consistent; the stub ignores a dangling base-url, but do not leave a mount referencing a volume only credproxy provided (verify the VolumeCertShared emptyDir + mounts still validate, or gate them too).
- Update jobspec unit tests (`jobspec_test.go`): assert credproxy IS injected when ProviderSecretRef is set, and is ABSENT when it is empty. Test-harness/unit edits to the controller's own tests are in scope; do NOT edit integration fixtures (testdata/*.yaml), charts/values.yaml, or hack/scripts/.

## Verification bar

1. `go test ./internal/dispatch/podjob/...` → PASS, including new present/absent credproxy assertions.
2. `make test-int-fast` (Layer A / envtest) → `Ran 29 of 29 ... 29 Passed | 0 Failed` (no regression).
3. (Orchestrator handles) re-run `ACCEPTANCE_SAMPLE=small make acceptance-v1-smoke` → `Project status.phase=Complete` at $0, ACC_EXIT=0.

## Resolution

- **root_cause:** `internal/dispatch/podjob/jobspec.go` injected the `tide-credproxy` native-sidecar init container UNCONDITIONALLY (`InitContainers: []corev1.Container{envelopeWriter, credproxy}`), but only wired `ANTHROPIC_API_KEY` into it when `opts.Project.Spec.ProviderSecretRef != ""`. `cmd/credproxy/main.go:156` does `requireEnv("ANTHROPIC_API_KEY")` → `exit 1`. The $0 small-project has no providerSecretRef → credproxy `Init:CrashLoopBackOff` → native-sidecar gate blocks the subagent container → planner pod never runs → Project stalls at `Running`.

- **fix:** Option A (user-confirmed). Gated credproxy injection on a provider secret via `credproxyEnabled := opts.Project != nil && opts.Project.Spec.ProviderSecretRef != ""`. When false ($0 stub path), the PodSpec omits: (1) the `tide-credproxy` init container, (2) the `cert-shared` emptyDir volume (credproxy is its sole producer), (3) the subagent's `cert-shared` volume mount, and (4) the subagent's `ANTHROPIC_BASE_URL` / `SSL_CERT_FILE` / `NODE_EXTRA_CA_CERTS` env vars. The subagent's signed-token `ANTHROPIC_API_KEY`/`ANTHROPIC_AUTH_TOKEN` and the workspace PVC mount are unchanged. This keeps the rendered PodSpec valid (no mount references a removed volume) and coherent (no `SSL_CERT_FILE` pointing at an empty emptyDir) in both configurations. Zero blast radius on Layer B (all Layer B fixtures carry a providerSecretRef post-cascade-8, so credproxy is still injected there).

  **PodSpec shape change for the no-secret path:** YES — when there is no providerSecretRef, the cert-shared volume + its mounts and the subagent cert/base-url env are now ALSO gated out (not just the credproxy container). This was required to keep the PodSpec valid; a mount referencing a removed volume is a hard K8s API error.

- **verification:**
  1. `go test ./internal/dispatch/podjob/...` → `ok github.com/jsquirrelz/tide/internal/dispatch/podjob` (PASS). New assertions: `TestBuildJobSpec_Credproxy_PresentWhenProviderSecretRefSet`, `TestBuildJobSpec_Credproxy_AbsentWhenNoProviderSecretRef`, `TestBuildJobSpec_PodSpecValid_BothSecretConfigurations` (executor/planner × with/without secret) all PASS.
  2. `make test-int-fast` (Layer A / envtest) → `Ran 29 of 29 Specs in 25.844 seconds` / `SUCCESS! -- 29 Passed | 0 Failed | 0 Pending | 0 Skipped`. (First run surfaced 1 real regression in `test/integration/envtest/planner_dispatch_test.go:168` — its Project fixture had no providerSecretRef so the test asserted the old unconditional-credproxy behavior; fixed by adding `ProviderSecretRef: "pd-test-provider-secret"` to that fixture so the test keeps exercising the full credproxy-present dispatch contract.)
  3. `go build ./...` → BUILD_OK.
  4. (Orchestrator handles) `ACCEPTANCE_SAMPLE=small make acceptance-v1-smoke` → expect `Project status.phase=Complete` at $0.

- **files_changed:**
  - `internal/dispatch/podjob/jobspec.go` — gated credproxy container + cert-shared volume + subagent cert mount/env on `credproxyEnabled`.
  - `internal/dispatch/podjob/jobspec_test.go` — added `buildNoSecretTestOptions`, `validatePodSpecVolumeMountRefs`, and present/absent + PodSpec-validity tests.
  - `test/integration/envtest/planner_dispatch_test.go` — added `ProviderSecretRef` to the Project fixture so the full-dispatch-contract spec still exercises the credproxy-present path.

---
**Closed at v1.0.0 milestone completion (2026-06-11).** The defect class this
session tracked was fixed and validated before ship: full `make test-int`
green (Layer A 36/36 + Layer B), nightly-integration green, live medium DoD
on minikube (Project=Complete, BoundaryPushed=True), and the v1.0.0-rc dry-run
gate green end-to-end.
