# Phase 5 — Deferred Items

Out-of-scope discoveries logged during plan execution. Address in follow-up plans or `/gsd:quick`.

## Discovered 2026-05-22 (during plan 05-01 execution)

- `cmd/dashboard/api/plans.go` and `cmd/dashboard/api/tasks.go` are not gofmt-clean. Detected when `make fmt` ran during plan 05-01 verification. Out of scope for DIST-03 (these files already carry the Apache-2.0 header, so the verify-license gate is unaffected). Defer to a follow-up plan or `/gsd:quick`.

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

## Discovered 2026-05-23 (during plan 05-11 execution, Rule 1 deviation)

**Project.Spec is missing 5 fields the planner assumed exist** — Plan 05-11's plan body referenced `outcomePrompt`, `branchStrategy`, `governanceLevel`, `gitCredsSecretRef`, `rollingWindowDurationSeconds` as Project.Spec fields, but none of them exist in `api/v1alpha1/project_types.go`. kubebuilder structural schema would silently prune them on `kubectl apply`.

**Most consequential gap: `outcomePrompt`.** The Variant B outcome prompt (1,643 chars) drives the v1 acceptance test (BOOT-04). The executor carried it as a `tideproject.k8s/outcome-prompt` **annotation** on the large sample's Project CRD. v1.0 controllers will need to read this annotation; v1.x schema work should promote it to a first-class `Project.Spec.OutcomePrompt` field.

**Other 4 fields** were either omitted (`governanceLevel`, `branchStrategy` — gate policy + per-run branch behavior are managed elsewhere in current schema) or remapped to existing schema paths (`gitCredsSecretRef` → `git.credsSecretRef` already exists; `rollingWindowDurationSeconds` → `budget.rollingWindowDuration` already exists per Phase 04.1 P4.1).

**Action items (v1.x):**
1. Add `Project.Spec.OutcomePrompt string` to `api/v1alpha1/project_types.go` (the most-needed field; controllers should read it from `Project.Spec.OutcomePrompt`, falling back to the annotation for v1.0-compat).
2. Update planner subagent code paths to read from the new field.
3. Migrate the large sample from annotation to Spec field once both controllers + samples are ready.

## Discovered 2026-05-22 (during plan 05-08 execution, recovered #3099 worktree-path drift)

**Worktree-relative absolute-path resolution drift** — Plan 05-08 executor's initial `Write` of `docs/project-authoring.md` resolved to the main repo's checkout instead of the worktree's checkout. Recovered with `mv` into the worktree + re-verify. No state polluted in main repo. Same root cause as the orchestrator-cwd issue I (the orchestrator) hit during Wave 1 merge. This is a known issue (`#3099` — `worktree-path-safety.md`). Worth keeping in mind for any worktree-spawning future workflow runs; ensuring path safety checks land deterministically before `Write`/`Edit` calls would prevent the recovery dance.
