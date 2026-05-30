# Phase 5 — Deferred Items

Out-of-scope discoveries logged during plan execution. Address in follow-up plans or `/gsd:quick`.

## Discovered 2026-05-22 (during plan 05-01 execution) — **RESOLVED 2026-05-27**

- `cmd/dashboard/api/plans.go` and `cmd/dashboard/api/tasks.go` are not gofmt-clean. Detected when `make fmt` ran during plan 05-01 verification. Out of scope for DIST-03 (these files already carry the Apache-2.0 header, so the verify-license gate is unaffected). Defer to a follow-up plan or `/gsd:quick`. **RESOLVED 2026-05-27** via quick task `260526-w11`, commit `489dd71` (`style(quick-260526-w11): gofmt cmd/dashboard/api/{plans,tasks}.go struct alignment`). Apache-2.0 headers byte-identical post-format; `verify-license` gate confirmed green.

## Discovered 2026-05-22 (during plan 05-01 execution, Rule 2 deviation)

**104 Go files lacked Apache-2.0 headers** — Plan 05-01's `must_haves.truths` line "Every Go file under api/, cmd/, internal/, pkg/, test/, tools/ already has the Apache-2.0 header (verified, NOT modified)" was factually wrong: 104/254 non-vendor Go files lacked headers. Executor backfilled them in commit `14e314e` (Rule 2 deviation, well-documented). Plan-checker should fact-check `must_haves.truths` assertions against the live tree in future iterations.

## Discovered 2026-05-22 (during plan 05-01 verify-license execution)

**verify-license.sh scope too wide** — `hack/scripts/verify-license.sh` checks Go files across the entire repo and finds: (a) `examples/tide-demo-fixture/main.go` + `main_test.go` which are intentionally MIT-licensed per D-B3 (NOT Apache-2.0 distribution code), and (b) stale `.claude/worktrees/agent-*/` paths that aren't real source files. Script should exclude `examples/` and `.claude/worktrees/`. Fix landed inline alongside this entry (small enough to defer-and-fix rather than open a separate plan).

## Discovered 2026-05-22 (during plan 05-05 execution)

### SOT drift in `hack/helm/tide-values.yaml` and `hack/helm/projects-pvc.yaml`

**Discovered by:** Plan 05-05 executor — running `bash hack/helm/augment-tide-chart.sh` produced unexpected modifications to `charts/tide/values.yaml` and `charts/tide/templates/projects-pvc.yaml` that have nothing to do with the chart version bump.

**Drift detected:**

1. **`charts/tide/values.yaml`** — SOT `hack/helm/tide-values.yaml` is missing two fields present in the generated chart:
   - `controllerManager.manager.podAnnotations: {}` (line 41)
   - `workspaces.pvc.accessModes: [ReadWriteMany]` (line 309)

   Running the augment script would drop these from `charts/tide/values.yaml`.

2. **`charts/tide/templates/projects-pvc.yaml`** — SOT `hack/helm/projects-pvc.yaml` uses a hardcoded `accessModes: [ReadWriteMany]` instead of the templated `{{- range (.Values.workspaces.pvc.accessModes | default (list "ReadWriteMany")) }}` loop that the generated chart now uses to support kind/minikube overrides.

**Why deferred:**

- Plan 05-05's scope is the lockstep version bump only (`files_modified: [hack/helm/tide-chart.yaml, hack/helm/tide-crds-chart.yaml]`).
- CLAUDE.md `Working Rules → 2. Execute, Don't Ask` lists "Edits to `charts/tide/values.yaml`" as an "always confirm or refuse" exception — chart is FIXED contract per Phase 02.2's anti-pattern.
- The augment scripts cleanly produced the desired `Chart.yaml` updates for both charts; the unrelated drift was reverted via `git checkout -- charts/tide/values.yaml charts/tide/templates/projects-pvc.yaml` to preserve plan scope.

**Remediation owner:** Future Phase 5 plan (likely a dedicated SOT-resync plan) should:

1. Promote the `podAnnotations`, `accessModes` template loop, and `accessModes` default into the SOT files at `hack/helm/tide-values.yaml` and `hack/helm/projects-pvc.yaml`.
2. Re-run `bash hack/helm/augment-tide-chart.sh` — expected diff should now be empty.
3. Add a CI guard so `make helm && git diff --exit-code charts/` fails fast on any future SOT drift.

**Severity:** Low — current chart-tree is correct and CI helm-lint passes. The drift only manifests if `augment-tide-chart.sh` is run alongside an unrelated bump, which is exactly what plan 05-05 did.

## Discovered 2026-05-22 (during plan 05-14 execution)

**Phase 3 + 04.1 helmified CRD subchart drift** — `charts/tide-crds/templates/*-crd.yaml` was stale relative to `config/crd/bases/`. Plan 05-14's `make helm-crds && git diff --exit-code charts/tide-crds/` acceptance criterion required landing the catch-up alongside the `helm.sh/resource-policy: keep` annotation. Affected fields: `additionalPrinterColumns`, `Project.Spec.{Git,Subagent,Budget.RollingWindowDuration}`, `ProviderConfig.AllowedRoutes`, `Project.Status.Git`, Task `wait-for-signal` mode. Documented in 05-14's commit body + SUMMARY's Deviations section. The root cause appears to be that prior phases didn't run the augment script after schema changes; recommend adding `make helm-crds` to the per-phase verification gate in future schema-modifying phases.

## Discovered 2026-05-23 (during plan 05-11 execution, Rule 1 deviation) — **RESOLVED 2026-05-23**

**Project.Spec was missing 5 fields the planner assumed exist** — Plan 05-11's plan body referenced `outcomePrompt`, `branchStrategy`, `governanceLevel`, `gitCredsSecretRef`, `rollingWindowDurationSeconds` as Project.Spec fields, but none existed in `api/v1alpha1/project_types.go`.

**RESOLVED:** Root-cause fix landed pre-Wave-4 (commit batch on 2026-05-23):
1. `Project.Spec.OutcomePrompt string` added to `api/v1alpha1/project_types.go` (optional, multi-line YAML literal shape).
2. CRDs regenerated via `make manifests` + propagated to `charts/tide-crds/templates/` via `make helm-crds`.
3. `examples/projects/large/project.yaml` migrated from `tideproject.k8s/outcome-prompt` annotation → `spec.outcomePrompt`.
4. `examples/projects/medium/project.yaml` migrated from annotation → `spec.outcomePrompt`.

**Other 4 fields** stay omitted/remapped — they don't need first-class Spec field equivalents:
- `governanceLevel`, `branchStrategy` → gate policy + per-run branch behavior live in existing fields/conventions.
- `gitCredsSecretRef` → `spec.git.credsSecretRef` already exists.
- `rollingWindowDurationSeconds` → `spec.budget.rollingWindowDuration` (metav1.Duration) already exists per Phase 04.1 P4.1.

## Discovered 2026-05-22 (during plan 05-08 execution, recovered #3099 worktree-path drift)

**Worktree-relative absolute-path resolution drift** — Plan 05-08 executor's initial `Write` of `docs/project-authoring.md` resolved to the main repo's checkout instead of the worktree's checkout. Recovered with `mv` into the worktree + re-verify. No state polluted in main repo. Same root cause as the orchestrator-cwd issue I (the orchestrator) hit during Wave 1 merge. This is a known issue (`#3099` — `worktree-path-safety.md`). Worth keeping in mind for any worktree-spawning future workflow runs; ensuring path safety checks land deterministically before `Write`/`Edit` calls would prevent the recovery dance.

## Discovered 2026-05-23 (during plan 05-12 execution — same v1.0 schema gaps as 05-11, medium-sample now hits them too) — **RESOLVED 2026-05-23**

**Medium sample previously inherited the same v1.0 schema gaps as the large sample.** All 3 gaps resolved via root-cause fix pre-Wave-4:

1. **`targetRepo: file:///demo-remote.git`** — RESOLVED. `ProjectSpec` XValidation extended to allow `file://` URLs alongside `http`/`git@`.
2. **`spec.git.repoURL: file:///demo-remote.git`** — RESOLVED. `GitConfig.RepoURL` Pattern relaxed from `^https?://.+` to `^(https?://|file:///).+`.
3. **`tideproject.k8s/outcome-prompt` annotation** — RESOLVED. `outcomePrompt` is now a first-class `Project.Spec.OutcomePrompt` field; medium + large samples migrated from annotation to spec.

All 3 samples now apply cleanly under strict-CEL admission. Schema test `TestProjectCRDSchemaHasRepoURLPattern` updated to match the new pattern.

## Discovered 2026-05-23 (during plan 05-12 execution, Rule 3 deviation — Go embed across go.mod boundary)

**`//go:embed` rejects directories carrying a sibling `go.mod`.** Plan 05-12 Task 1 hit this immediately on first build:

```
cmd/tide-demo-init/main.go:92:12: pattern all:fixture: cannot embed directory fixture: in different module
```

**Cause:** `examples/tide-demo-fixture/` ships its own `go.mod` / `go.sum` (it's a tiny standalone Go module the medium-sample's Claude task operates on). When the Dockerfile/`go:generate` copies it into `cmd/tide-demo-init/fixture/`, the sibling `go.mod` makes Go's embed treat `fixture/` as a different module — which embed refuses.

**Fix landed inline:** Rename `go.mod` → `go.mod.txt` and `go.sum` → `go.sum.txt` at materialization time (both in the `//go:generate` directive AND in the Dockerfile RUN steps). `cmd/tide-demo-init/main.go`'s `restoreShimmedName` helper reverses the rename at unpack time so the bare repo's working tree carries the canonical filenames byte-for-byte equivalent to the SOT.

**MEDIUM-11 lock honored:** The embed directive `//go:embed all:fixture` itself is unchanged. Only the on-disk layout that the embed reads from is renamed transiently — the directive shape is identical to the plan's locked value.

**Action items (v1.x):**

1. Document the submodule-shim pattern in `docs/contributing.md` if/when other in-tree binaries need to embed module-shaped content (e.g., future test fixtures that ship their own go.mod).
2. Consider whether to migrate the fixture content to avoid carrying `go.mod`/`go.sum` (would require rethinking `examples/tide-demo-fixture/` to be parseable as Go source without being a runnable module). Not pursued in v1.0 — the shim keeps the SOT intact and the bare-repo content authentic.

**Severity:** Low — shim is fully transparent to operators; only matters at build time.

## Discovered 2026-05-30 (during BOOT-04 acceptance retry attempt) — **RESOLVED 2026-05-30**

**`hack/scripts/acceptance-v1.sh` was missing a cert-manager install step.** Discovered when today's `make acceptance-v1` invocation (background task `bess2gftr` at 2026-05-30T16:13:45Z) failed mid-`helm install tide` with `no matches for kind "Certificate" in version "cert-manager.io/v1"`. The tide chart's webhook + metrics Certificates (`charts/tide/templates/{serving-cert,selfsigned-issuer,metrics-certs}.yaml`) require cert-manager CRDs to exist in the cluster, but the script jumped straight from `kind create cluster` to `helm install tide` with no cert-manager bring-up. The Layer B integration tests already wire this up at `test/integration/kind/suite_test.go:157,329-369` (pinned `v1.20.2`); the BOOT-04 ritual just didn't mirror the pattern. `docs/INSTALL.md` also didn't document cert-manager as a prereq, even though the dependency is mentioned in `docs/observability.md:64-65` and `docs/rbac.md:227-247`.

**RESOLVED 2026-05-30** via quick task `260530-h2h`:

1. `hack/scripts/acceptance-v1.sh` — cert-manager install + wait block inserted between kind create and helm install, mirroring `installCertManager()` from the Layer B integration tests. `TIDE_CERT_MANAGER_VERSION` env-override convention preserved (same as `test/integration/kind/suite_test.go:336`). Default pinned to `v1.20.2`. Commit `adb1053` (`fix(quick-260530-h2h): install cert-manager v1.20.2 before helm install tide in acceptance-v1.sh`).
2. `docs/INSTALL.md` — cert-manager prereq subsection added before the `helm install tide` instructions, with version pinning guidance + cross-references to `docs/observability.md` and `docs/rbac.md`. Commit `7d3af9d` (`docs(quick-260530-h2h): document cert-manager v1.20.2 prerequisite in INSTALL.md`).

**Lesson for plan authors:** when the BOOT-04 plan was authored, the planner-time research noted cert-manager as an installed dep but didn't trace the Helm install order — the acceptance script needs to mirror the integration-test bring-up pattern verbatim, not just the steady-state assumption that cert-manager "is already there." Future scripts that install `charts/tide` from scratch should always be cross-checked against `test/integration/kind/suite_test.go`'s suite-setup sequence.

## Discovered 2026-05-30 (during BOOT-04 acceptance retry, second cascade) — **DEFERRED to Phase 6**

**Image-publish pipeline gap + chart-tag drift.** BG task `bs3ntw3rt` (2026-05-30T16:25:00Z) made it past the cert-manager prereq fix from cascade-1 (quick task `260530-h2h`, commits `adb1053` + `7d3af9d`) but then hit `kubectl wait --for=condition=Available deploy/tide-controller-manager` timeout at 5m. `kubectl describe pod` showed `tide-controller-manager` Pending with ImagePullBackOff on `ghcr.io/jsquirrelz/tide-controller:v0.1.0-dev` and `tide-dashboard` Pending with ImagePullBackOff on `ghcr.io/jsquirrelz/tide-dashboard:1.0.0`.

**Root cause:** `.goreleaser.yaml` builds only the `tide` CLI binary (no `dockers:` section, no `docker_manifests:`); no `.github/workflows/*.yaml` publishes the 6 component Docker images that `charts/tide/values.yaml` references; additionally, chart values.yaml hardcodes 5 component tags at `v0.1.0-dev` while dashboard defaults to `.Chart.AppVersion` (`1.0.0`) — neither tag exists on ghcr.io. `hack/scripts/dry-run-v1.sh` also lacks the cert-manager prereq (cascade-1's fix only landed in `acceptance-v1.sh`), so Door 3 of the v1.0 ship decision tree is also deadlocked.

**DEFERRED to Phase 6** — `.planning/phases/06-v1-image-publish-and-ship-readiness-revalidation/06-FINDINGS.md` carries the scope-of-record (root cause + commits-already-in-main + DRAFT proposed requirements + out-of-scope + next-session playbook). Phase 6 will be opened via `/gsd-spec-phase 06` + `/gsd-discuss-phase 06` + `/gsd-plan-phase 06` + `/gsd-execute-phase 06` in subsequent sessions; this quick task (`260530-hrc`) handled the planning bookkeeping only (ROADMAP row + STATE reframe + `06-FINDINGS.md` + this back-reference). Phase 5's claim of "v1.0 ship-ready" was premature — the deliverables that Phase 5 actually shipped (LICENSE, NOTICE, docs, samples, chart-additions, release.yaml chain, BOOT-02/BOOT-04 plumbing) all stand; the image-publish pipeline they assumed exists is what's missing, and Phase 6 is the catch-up.
