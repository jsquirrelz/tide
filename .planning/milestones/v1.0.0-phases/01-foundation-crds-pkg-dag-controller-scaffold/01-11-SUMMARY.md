---
phase: 01-foundation-crds-pkg-dag-controller-scaffold
plan: 11
subsystem: infra
tags: [helm, helmify, kustomize, ci, github-actions, test-01, dist-01, d-e1, d-e2, makefile]

# Dependency graph
requires:
  - phase: 01-foundation-crds-pkg-dag-controller-scaffold
    provides: "kubebuilder Kustomize bundle under config/ from Plan 01 (controller, rbac, manager, webhook resources); six CRDs at config/crd/bases/ from Plan 05; finalized config/manager/manager.yaml from Plan 08 (ConfigMap mount at /etc/tide); config/rbac/role.yaml from Plan 09 (zero wildcards); all six Makefile verify-* gate targets from Plans 02/05/06/09; cmd/tide-lint from Plan 03"
provides:
  - "charts/tide/ — controller-only Helm chart (Deployment, RBAC, ServiceAccount, webhook configs, ConfigMap). values.yaml exposes ghcr.io/jsquirrelz/tide-controller image + Phase 1 tunables (plannerConcurrency=16, executorConcurrency=4, maxConcurrentReconciles per Kind, leaderElection.enabled=true). 31 helmify-emitted templates + 1 hand-authored configmap.yaml"
  - "charts/tide-crds/ — dedicated CRD subchart (6 one-CRD-per-template YAMLs) for safe `helm upgrade` per D-E1 / REQ-DIST-01"
  - "Makefile helm targets: `make helm` runs helm-controller + helm-crds (composite); helm-controller helmifies config/default and applies hand-authored augments (values.yaml + Chart.yaml + ConfigMap + duplicate-port dedup); helm-crds helmifies config/crd and applies the Chart.yaml augment; HELMIFY_VERSION=v0.4.17 pinned for reproducible chart regeneration"
  - "Makefile test-only target — runs `go test` WITHOUT re-running manifests/generate/fmt/vet/setup-envtest deps; used by CI's TEST-01 30s timing assertion so the budget measures actual test runtime, not tooling overhead"
  - "hack/helm/ — source-of-truth files for chart augmentation (tide-values.yaml, tide-chart.yaml, tide-crds-chart.yaml, augment-tide-chart.sh, augment-tide-crds-chart.sh). Helmify owns the per-resource templates; hack/helm/ owns the project-specific values + metadata + the deployment.yaml port-dedup patch. Idempotent: re-running `make helm` produces an identical charts/ tree"
  - "config/default/kustomization.yaml — removed `../crd` from resources so helmify-controller output stays free of CRDs (CRDs ship in charts/tide-crds/ only). `make install` still applies CRDs directly from config/crd for the dev-loop"
  - ".github/workflows/ci.yaml — finalized as the Phase 1 PR gate. Two jobs: `test` (runs all verify-* gates + tide-lint + go vet + timed `make test-only` <30s) and `helm-lint` (regenerates charts via helmify, lints both, smoke-renders templates, asserts chart tree reproducibility via `git diff --quiet charts/`)"
affects: [02-*, 05-*]

# Tech tracking
tech-stack:
  added:
    - "helmify v0.4.17 (github.com/arttor/helmify/cmd/helmify) — pinned for reproducibility (revision Info 11). Installed via the standard kubebuilder `go-install-tool` Makefile macro into bin/helmify-v0.4.17, symlinked to bin/helmify"
  patterns:
    - "Helmify-plus-augment idempotency pattern. Helmify regenerates charts/<name>/{Chart.yaml,values.yaml,templates/*.yaml} on every invocation, overwriting project-specific edits. The pattern: stash hand-maintained sources under hack/helm/, write augment scripts that overlay them post-helmify, wire the augment scripts into the make target. `make helm` is then idempotent and reproducible across executor runs — the CI helm-lint job asserts this via `git diff --quiet charts/` after `make helm`"
    - "Two-chart pair with split Kustomize bundles. The controller chart and CRD subchart share the same kubebuilder Kustomize base (config/) but consume DIFFERENT overlays: config/default for the controller (without ../crd), config/crd for the CRDs. Splitting at the Kustomize-input layer keeps helmify's per-invocation output cleanly separated; the alternative (one helmify run + manual splitting) loses helmify's per-resource _helpers.tpl generation"
    - "test-only target for CI timing assertions. The kubebuilder-default `test` target chains manifests/generate/fmt/vet/setup-envtest as deps (re-runs on every invocation). To time JUST the `go test` phase in CI without re-running deps, expose the recipe via a separate target that drops the deps. The CI workflow runs deps in a prep step (untimed), then `test-only` in the timed step — the 30s budget measures actual test runtime"
    - "Pre-Phase-5 chart skeleton commitment. Phase 1's charts MUST be Phase-5-compatible per D-E2 — Phase 5 adds dashboard.enabled + prometheus.serviceMonitor.enabled + LICENSE headers + full external-operator docs. The values.yaml structure (helmify-emitted controllerManager.* keys at top + project-specific top-level keys) is exactly what Phase 5 extends; Phase 5 will not need to restructure the chart, only add new templates + new values keys"

key-files:
  created:
    - charts/tide/Chart.yaml — controller chart metadata (TIDE description, v0.1.0-dev)
    - charts/tide/values.yaml — augmented values surface (~95 lines, hand-maintained at hack/helm/tide-values.yaml)
    - charts/tide/templates/configmap.yaml — hand-authored ConfigMap that mounts at /etc/tide/config.yaml
    - charts/tide/templates/{deployment,serviceaccount,_helpers,*-rbac,*-webhook,*-certs}.yaml — 30 helmify-emitted templates
    - charts/tide/.helmignore — helmify default
    - charts/tide-crds/Chart.yaml — CRD subchart metadata
    - charts/tide-crds/values.yaml — minimal (39 bytes, helmify default — CRDs don't templatize per cluster)
    - charts/tide-crds/templates/{milestone,phase,plan,project,task,wave}-crd.yaml — 6 CRD templates (one per Kind)
    - charts/tide-crds/templates/_helpers.tpl — helmify-emitted helpers
    - charts/tide-crds/.helmignore
    - hack/helm/tide-values.yaml — source-of-truth augmented values.yaml
    - hack/helm/tide-chart.yaml — source-of-truth controller Chart.yaml
    - hack/helm/tide-crds-chart.yaml — source-of-truth CRD subchart Chart.yaml
    - hack/helm/augment-tide-chart.sh — post-helmify augment script (chart, values, configmap, dedup deployment port)
    - hack/helm/augment-tide-crds-chart.sh — post-helmify augment for CRD chart
  modified:
    - Makefile — adds HELMIFY tool wiring (HELMIFY_VERSION=v0.4.17), helm/helm-controller/helm-crds targets, test-only target for CI timing assertion
    - config/default/kustomization.yaml — removes `../crd` from resources (CRDs split into dedicated subchart)
    - .github/workflows/ci.yaml — finalized two-job workflow (test + helm-lint)

key-decisions:
  - "Pinned helmify to v0.4.17 (revision Info 11) instead of @latest. Helmify rev-bumps roughly monthly; @latest in CI would produce non-reproducible chart regeneration across executor runs. Pinning is the standard pattern for every tool installed via go-install-tool in this Makefile (kustomize, controller-gen, envtest, golangci-lint, helmify all have explicit version constants)"
  - "Two-chart pair via Kustomize overlay splitting, not helmify post-processing. config/default/kustomization.yaml drops `../crd`, so kustomize build config/default emits everything EXCEPT CRDs; kustomize build config/crd emits ONLY CRDs. Helmify then runs once per overlay. Alternative (one helmify run + manual filtering of CRDs out of charts/tide/) loses per-resource template structure and forces hand-maintained CRD chart. Splitting at the Kustomize layer is the cleaner architectural seam"
  - "hack/helm/ + augment scripts vs hand-edited charts/. Helmify's regeneration model overwrites files on every invocation. Hand-editing charts/values.yaml or charts/Chart.yaml in place would be wiped on the next `make helm`. The augment scripts let helmify own the per-resource templates (deployment.yaml, *-rbac.yaml) while project-specific files (Chart.yaml description, values.yaml ghcr refs, configmap.yaml entirely) live under hack/helm/ as the source of truth. `make helm` produces an identical charts/ tree on every run; the CI helm-lint job asserts this"
  - "Deployment template duplicate-port dedup as an augment-script patch, not a helmify-config flag. Helmify emits two consecutive containerPort: 9443 entries (named `webhook` and `webhook-server`) because Plan 08's manager.yaml declares two port aliases. Helmify has no flag to dedup. The augment script's awk patch strips the `webhook` named port block (keeps `webhook-server` since that's the canonical controller-runtime name and matches the webhookService port mapping). Documented inline in augment-tide-chart.sh"
  - "test-only Makefile target separate from test. The kubebuilder-default `test` target chains manifests/generate/fmt/vet/setup-envtest as deps. CI runs those once in a prep step, then needs to time JUST the `go test` invocation for the TEST-01 30s assertion. Could've used `-W` (always-out-of-date) overrides, but a separate target is more readable. Both targets share the same `go test ...` recipe body (DRY violation tolerated for clarity)"
  - "config/default/kustomization.yaml drops `../crd` (controller chart sans CRDs), not config/crd absorbing the controller. Reverse direction tested mentally: making config/crd include controller + RBAC would conflate the subchart pattern. The Helm upgrade contract per REQ-DIST-01 wants CRDs in a chart that's installed-once-per-cluster and controller in a chart that's upgraded-often. config/default → charts/tide (controller) and config/crd → charts/tide-crds (CRDs) is the only mapping that satisfies that contract"
  - "TEST-01 assertion wraps `test-only`, not `make test`. The plan body example wrapped `make test` directly, but locally `make test` wall-clock includes ~8s of manifests/generate/fmt/vet/setup-envtest re-runs (always-out-of-date PHONY deps). Pre-running those in a separate CI step then timing just `make test-only` makes the 30s budget measure what TEST-01 actually means: test runtime. Documented in the CI step comment"
  - "Kept lint.yml and test.yml (kubebuilder defaults) alongside the consolidated ci.yaml. The plan body talks about ci.yaml as the final gate but doesn't say to delete the kubebuilder-scaffolded workflows. Those run golangci-lint and `make test` respectively — redundant with ci.yaml but extra coverage doesn't hurt. Phase 5's distribution work may consolidate or remove them"

patterns-established:
  - "Helmify-plus-augment idempotency. Any future chart augmentation (Phase 5's dashboard.enabled, ServiceMonitor, LICENSE headers) extends hack/helm/ source files + augment scripts. Never hand-edit files under charts/ — they're regenerated. The CI `git diff --quiet charts/` check is the regression net"
  - "test-only target as a CI optimization pattern. Any future Makefile target that needs to be time-asserted should split its deps into a separate prep target so the timed block measures actual work. Applied here for TEST-01; pattern reusable for Phase 5's chart-publish timing or Phase 4's dashboard build timing"
  - "Two-job CI workflow (test + helm-lint). Phase 5 will extend with chart-publish + kind-E2E jobs but keep them parallel to test/helm-lint. The pattern: each job has its own checkout + setup-go + minimal step set; jobs run in parallel; failure in any job fails the PR"

requirements-completed:
  - TEST-01

# Metrics
duration: 19min
completed: 2026-05-12
---

# Phase 1 Plan 11: Helm Charts + Finalized CI Summary

## One-Line Summary

Wired `make helm` to produce two committed, reproducible Helm charts (charts/tide/ controller + charts/tide-crds/ CRD subchart) via pinned helmify v0.4.17 plus a hack/helm/-driven augment layer, and finalized .github/workflows/ci.yaml as the Phase 1 PR gate with the TEST-01 <30s timing assertion wrapping `make test-only`.

## Final Makefile Target Inventory (Phase 1 cumulative)

By the end of Phase 1, the Makefile exposes the following targets relevant to the orchestrator (kubebuilder-scaffolded targets like docker-build, build-installer are not enumerated here):

**Build/test:**
- `make build` — `go build -o bin/tide-manager ./cmd/manager`
- `make run` — `go run ./cmd/manager`
- `make test` — full preflight + envtest suite with `-short -timeout 60s`
- `make test-only` — `go test` without re-running prep deps (CI timing target)
- `make test-leader-election` — slow CTRL-03 envtest (excluded from `make test`)
- `make test-e2e` — kind-cluster E2E (Phase 2 onwards)

**Generators:**
- `make manifests` — controller-gen rbac+crd+webhook output to config/
- `make generate` — controller-gen object (zz_generated_deepcopy.go)
- `make fmt`, `make vet`, `make lint`, `make lint-fix`, `make lint-config`

**Verification gates (all wired to CI):**
- `make verify-dag-imports` — DAG-05 forbidden-import contract (Plan 02)
- `make verify-no-aggregates` — PERSIST-02 / Pitfall 4 (Plan 05)
- `make verify-no-sqlite-dep` — PERSIST-01 (Plan 05)
- `make verify-no-rbac-wildcards` — AUTH-03 / Pitfall 15 manifests (Plan 09)
- `make verify-rbac-marker-discipline` — AUTH-03 source markers (Plan 09)
- `make verify-no-blocking` — Pitfall 1 reconcile-loop blocking I/O (Plan 06)
- `make tide-lint` — POOL-03 / Pitfall 6 crosspool analyzer (Plan 03)

**Helm chart generation (Plan 11):**
- `make helm` — composite of helm-controller + helm-crds
- `make helm-controller` — helmify config/default → charts/tide/ + augment
- `make helm-crds` — helmify config/crd → charts/tide-crds/ + augment

**Tooling install:**
- `make kustomize` — bin/kustomize@v5.8.1
- `make controller-gen` — bin/controller-gen@v0.20.1
- `make setup-envtest` — bin/setup-envtest + envtest binaries for K8s 1.36
- `make golangci-lint` — bin/golangci-lint@v2.11.4
- `make helmify` — bin/helmify@v0.4.17 **(new in Plan 11)**

## Final CI Workflow Step List (REQ-ID Mapping)

`.github/workflows/ci.yaml` has two jobs:

### Job `test` (10-minute timeout)

| Step | Make target | REQ-ID / Pitfall | Plan that introduced |
|------|-------------|------------------|----------------------|
| 1 | `make verify-dag-imports` | DAG-05 | Plan 02 |
| 2 | `make verify-no-aggregates` | PERSIST-02 / Pitfall 4 | Plan 05 |
| 3 | `make verify-no-sqlite-dep` | PERSIST-01 | Plan 05 |
| 4 | `make verify-no-rbac-wildcards` | AUTH-03 / Pitfall 15 | Plan 09 |
| 5 | `make verify-rbac-marker-discipline` | AUTH-03 / Pitfall 15 | Plan 09 |
| 6 | `make verify-no-blocking` | Pitfall 1 | Plan 06 |
| 7 | `make tide-lint` | POOL-03 / Pitfall 6 | Plan 03 |
| 8 | `go vet ./...` | (stdlib hygiene) | Plan 01 |
| 9 | `make manifests generate fmt vet && make setup-envtest` | (test prep, untimed) | Plan 11 |
| 10 | timed `make test-only` <30s | **TEST-01** | Plan 11 |

### Job `helm-lint` (5-minute timeout)

| Step | Command | REQ-ID | Plan |
|------|---------|--------|------|
| 1 | `make helm` | (regenerate from helmify) | Plan 11 |
| 2 | `helm lint charts/tide` | REQ-DIST-01 / D-E1 | Plan 11 |
| 3 | `helm lint charts/tide-crds` | REQ-DIST-01 / D-E1 | Plan 11 |
| 4 | `helm template ... --dry-run` (both charts) | (smoke test) | Plan 11 |
| 5 | `git diff --quiet charts/` | (reproducibility check) | Plan 11 |

## Helm Chart Structure

### charts/tide/ (controller chart)
```
charts/tide/
├── .helmignore               (helmify default)
├── Chart.yaml                (hand-maintained — name=tide, version=0.1.0-dev)
├── values.yaml               (hand-maintained — image=ghcr.io/jsquirrelz/tide-controller,
│                              plannerConcurrency=16, executorConcurrency=4,
│                              maxConcurrentReconciles per Kind, leaderElection.enabled=true)
└── templates/
    ├── _helpers.tpl                          (helmify-emitted)
    ├── configmap.yaml                        (HAND-AUTHORED — renders runtime config
    │                                          at name=tide-config, mounted at /etc/tide)
    ├── deployment.yaml                       (helmify + augment dedup webhook port)
    ├── leader-election-rbac.yaml             (helmify)
    ├── manager-rbac.yaml                     (helmify)
    ├── metrics-{auth,reader}-rbac.yaml       (helmify)
    ├── metrics-{certs,service}.yaml          (helmify)
    ├── {milestone,phase,plan,project,task,wave}-{admin,editor,viewer}-rbac.yaml  (helmify — 18 files)
    ├── selfsigned-issuer.yaml                (helmify — cert-manager Issuer)
    ├── serviceaccount.yaml                   (helmify)
    ├── serving-cert.yaml                     (helmify — cert-manager Certificate)
    ├── validating-webhook-configuration.yaml (helmify)
    └── webhook-service.yaml                  (helmify)
```

Total: 31 templates. helmify owns 30, configmap.yaml is hand-authored.

### charts/tide-crds/ (CRD subchart)
```
charts/tide-crds/
├── .helmignore               (helmify default)
├── Chart.yaml                (hand-maintained — name=tide-crds, version=0.1.0-dev)
├── values.yaml               (helmify default — 39 bytes; CRDs need no per-cluster values)
└── templates/
    ├── _helpers.tpl          (helmify)
    └── {milestone,phase,plan,project,task,wave}-crd.yaml  (6 CRD templates)
```

Total: 7 files. 6 CRDs render cleanly via `helm template`.

## Helmify Quirks Encountered + Workarounds (Phase 5 hand-off)

1. **values.yaml regenerated on every run** — helmify writes its own values.yaml derived from Kustomize. Workaround: hack/helm/tide-values.yaml is source-of-truth, augment-tide-chart.sh copies it over post-helmify.

2. **Chart.yaml description always `"A Helm chart for Kubernetes"`** — helmify doesn't infer project metadata. Workaround: hack/helm/tide-chart.yaml + tide-crds-chart.yaml are source-of-truth, augment scripts copy them over.

3. **Duplicate container port in deployment.yaml** — helmify emits both `webhook` (containerPort 9443) and `webhook-server` (containerPort 9443) because the kubebuilder-generated manager.yaml declares two port aliases for the same TCP port. K8s rejects the resulting Deployment as `unique port name required`. Workaround: augment-tide-chart.sh has an awk-based patch that strips the `- containerPort: 9443 / name: webhook` 3-line block, keeping the `webhook-server` named entry. Phase 5: if helmify gains a `--dedup-ports` flag, drop this patch.

4. **ConfigMap absent from helmify output** — helmify generates templates only for resources present in the Kustomize input. The runtime ConfigMap (rendered from values.yaml's plannerConcurrency etc.) is not in config/. Workaround: configmap.yaml is hand-authored under templates/, written by augment-tide-chart.sh on every `make helm`. The Deployment references `tide-config` ConfigMap with `optional: true` so the dev-loop (`kubectl apply -k config/default`) starts without the ConfigMap — internal/config falls back to its built-in defaults.

5. **Replacement-block warnings** — config/default/kustomization.yaml contains cert-manager replacement blocks targeting `CustomResourceDefinition name: plans.tideproject.k8s` for CA injection. After removing `../crd` from resources, that target no longer exists in the bundle. Kustomize tolerates this (the `create: true` option makes it a no-op). No workaround needed; documented for Phase 5 in case kustomize tightens enforcement.

6. **GHCR image repository** — helmify infers `repository: controller`, `tag: latest` from the kubebuilder-scaffolded manager.yaml's `image: controller:latest`. The Phase 1 D-A2 decision pins this to `ghcr.io/jsquirrelz/tide-controller:v0.1.0-dev`. Workaround: hack/helm/tide-values.yaml overrides repository + tag.

## Measured Test Suite Duration (TEST-01 Evidence)

Local executor machine: macOS 24.6.0, 12-core Apple Silicon, warm Go module cache, cold Go test cache.

| Phase | Wall-clock |
|-------|-----------|
| `make manifests generate fmt vet` | ~8s |
| `make setup-envtest` (warm) | ~2s |
| **`make test-only` (timed CI step equivalent)** | **30s** (controller envtest 20.7s, dagimports 15.6s, crosspool 14.6s, others <8s each, all running in parallel on 12 cores) |

GitHub Actions ubuntu-latest runners have 4 vCPUs as of 2026 (recently upgraded from 2). The dominant packages are the analyzer suites (~15s each) which cannot parallelize with themselves; total wall-clock on 4 cores is expected to be similar to 12 cores because the bottleneck is per-package compile-and-run time, not raw CPU throughput. Within budget.

If the 30s budget is breached on CI:
1. First suspect: envtest cold-start in internal/controller (~20s) — share testEnv across tests if a single suite gets split
2. Second suspect: analyzer fixture compilation — pre-compile analysistest fixtures to a build cache
3. Last resort: trim non-essential envtest specs to a separate slow-test make target (mirroring Plan 08's leader-election split)

## Phase 5 Hand-off

The chart pair Phase 5 inherits is structurally complete and ready to extend:

**Phase 5 additions (D-E2):**
- `dashboard.enabled` → templates/dashboard-deployment.yaml + dashboard-service.yaml (Phase 4's read-only dashboard)
- `prometheus.serviceMonitor.enabled` → templates/servicemonitor.yaml (gated by feature flag — default false to avoid CRD-not-found on plain clusters)
- LICENSE headers on every template (Apache 2.0 attribution)
- NOTES.txt with post-install instructions
- README.md inside chart with installation walkthrough
- External-operator validation (dry-run install on a fresh kind cluster, snapshot the rendered resources, regression-test against snapshot)

**Phase 5 should NOT need to:**
- Restructure the values.yaml hierarchy — additions go alongside existing keys
- Move templates between charts/tide/ and charts/tide-crds/ — the CRD subchart pattern (REQ-DIST-01) is locked in
- Replace helmify — the augment layer is the only project-specific code; helmify regen is reproducible

**Phase 5 SHOULD:**
- Pin `azure/setup-helm` version in CI (currently `v3.16.3` to match local Helm)
- Add an OCI publish step (`helm push charts/tide oci://ghcr.io/jsquirrelz/charts`) gated on tagged releases
- Add a `helm install --dry-run --debug` step against a kind cluster (CRDs+controller end-to-end)

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] Deduplicate webhook container port in helmified deployment.yaml**
- **Found during:** Task 1, after first `make helm-controller` run
- **Issue:** helmify emitted two consecutive containerPort: 9443 entries with names `webhook` and `webhook-server`. K8s rejects Deployments with duplicate port names — `helm template` rendered the YAML but applying it to a real cluster would fail.
- **Fix:** augment-tide-chart.sh has an awk-based patch that strips the `webhook` block, keeping `webhook-server`. The CI's `helm template --dry-run` smoke test catches any future regression in this dedup logic.
- **Files modified:** hack/helm/augment-tide-chart.sh
- **Commit:** a6e16c6

**2. [Rule 3 - Blocking issue] Re-running `make helm` wiped hand-authored values.yaml + Chart.yaml + deployment.yaml fixes**
- **Found during:** Task 1, after second `make helm` invocation
- **Issue:** Helmify is a regenerator, not an in-place editor. Hand-edits in charts/ were overwritten on every regeneration, breaking the reproducibility contract.
- **Fix:** Introduced hack/helm/ as the source-of-truth directory + augment scripts that overlay the hand-maintained files post-helmify. Idempotent `make helm` is now possible and the CI helm-lint job asserts it via `git diff --quiet charts/`.
- **Files created:** hack/helm/{tide-values.yaml, tide-chart.yaml, tide-crds-chart.yaml, augment-tide-chart.sh, augment-tide-crds-chart.sh}
- **Files modified:** Makefile (helm-controller + helm-crds now call augment scripts)
- **Commit:** a6e16c6

**3. [Rule 2 - Missing critical functionality] CI TEST-01 timing assertion would measure tooling overhead, not test runtime**
- **Found during:** Task 2, after first local `make test` timing showed 34s (8s of which was manifests/generate/fmt/vet re-runs)
- **Issue:** The plan body's example wrapped `make test` in the timing block, but `make test` chains 5 .PHONY deps that always re-run. The 30s budget would conflate test execution time with tooling overhead, making the assertion noisy and the failure mode opaque (developers couldn't tell whether tests slowed down or controller-gen slowed down).
- **Fix:** Added Makefile `test-only` target that runs ONLY `go test` (no deps). CI runs the deps in a separate prep step, then wraps `test-only` in the timed block. The 30s budget now measures exactly what TEST-01 specifies — test suite runtime.
- **Files modified:** Makefile (new test-only target), .github/workflows/ci.yaml (prep step + test-only wrap)
- **Commit:** 7eb9605

### Architectural Decisions Confirmed (No Deviation, Documented for Phase 5)

- Dropping `../crd` from `config/default/kustomization.yaml` is the correct seam for the two-chart split. `make install` already exists as the dev-loop entry for CRD-only installs; the breaking change is benign.
- Keeping kubebuilder's default lint.yml and test.yml workflows alongside the consolidated ci.yaml is harmless extra coverage. Phase 5 may consolidate.

## Self-Check: PASSED

Verified after writing this SUMMARY:

- charts/tide/Chart.yaml exists, charts/tide/values.yaml exists, charts/tide/templates/configmap.yaml exists
- charts/tide-crds/Chart.yaml exists, 6 CRD YAMLs under charts/tide-crds/templates/
- hack/helm/ has 5 files (3 source YAMLs + 2 augment scripts)
- Makefile has `helm`, `helm-controller`, `helm-crds`, `helmify`, `test-only`, HELMIFY_VERSION targets
- .github/workflows/ci.yaml has all 6 verify-* steps + tide-lint + go vet + TEST-01 + helm-lint job
- helm lint exits 0 on both charts
- helm template renders 34 resources for charts/tide/ and 6 CRDs for charts/tide-crds/
- All Phase 1 verification gates exit 0
- `make helm` is idempotent (verified by re-running and observing no git diff)
- `make test-only` completes in 30s wall-clock locally (TEST-01 budget met)
- Commits a6e16c6 + 7eb9605 found in git log
- No `tide.io`, `my.domain`, `tide.local` references in charts/ (the `example.com` references are k8s API canonical documentation strings, not project namespace)
