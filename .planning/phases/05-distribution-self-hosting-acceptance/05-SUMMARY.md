---
phase: 05-distribution-self-hosting-acceptance
plan: 17
status: closeout
tags: [closeout, roadmap-update, state-update, phase-05-final, v1-ship-ready]
completed: 2026-05-23
duration: ~3h aggregate across 6 waves
closed: 2026-05-23
depends_on: ["05-01", "05-02", "05-03", "05-04", "05-05", "05-06", "05-07", "05-08", "05-09", "05-10", "05-11", "05-12", "05-13", "05-14", "05-15", "05-16"]
requirements_satisfied:
  - DIST-01  # Helm chart pair (chart-version lockstep + per-namespace-rolebinding + resource-policy:keep + OCI publish)
  - DIST-02  # helmify verify-only step in release.yaml
  - DIST-03  # Apache-2.0 LICENSE + NOTICE + Go file headers + verify-license gate
  - DIST-04  # 5 new docs (INSTALL, concepts, project-authoring, troubleshooting, rbac) + 3-sample cost spectrum + CONTRIBUTING + SECURITY + README Quickstart + docs index
  - DIST-05  # make dry-run-v1 + dry-run.yaml + dry-run-report.json schemaVersion 1
  - BOOT-02  # make acceptance-v1 with fresh kind + helm install
  - BOOT-04  # examples/projects/large/project.yaml Variant B + 7-check D-A3 verifier (3-of-4 commit shapes per MEDIUM-6)
  - AUTH-02  # Per-namespace RoleBinding template + docs/rbac.md (catch-up from Phase 1)
---

# Phase 5 Closeout — Distribution & Self-Hosting Acceptance

**Phase 5 closed 2026-05-23 — M0 → M_self bridge complete — v1.0 ship-ready.**

All 16 execution plans landed across 6 waves; closeout (this plan) is the 17th. ROADMAP Phase 5 row marked Complete (16/16, 2026-05-23); STATE.md frontmatter advanced to `completed_phases: 8`, `completed_plans: 100`, `percent: 100`; all 8 milestone phases now complete. HEAD at this closeout commit is the `v1.0.0-rc.1` candidate — the maintainer tags `v1.0.0-rc.1` from this SHA to trigger `dry-run.yaml`, then on rc green tags `v1.0.0` to fire the full `release.yaml` chain (goreleaser binaries + helmify-verify + OCI chart push to ghcr.io/jsquirrelz/tide-charts).

## Goal

Three loops closed simultaneously and the artifacts shipped that let an external operator install TIDE and use it without prior context:

- **OSS-readiness loop** — Apache-2.0 `LICENSE` at repo root (Go file headers already in place from Phase 1+), `NOTICE` (Apache ASF licensing-howto §"Required Third-Party Notices" propagation for k8s.io/*, controller-runtime, kubebuilder), `CONTRIBUTING.md` + `SECURITY.md`, README opens with a ~24-line Quickstart before the existing paradigm spec, and a `docs/README.md` index ordering the 7 existing docs + 4 new ones (INSTALL.md, project-authoring.md, troubleshooting.md, rbac.md) + concepts.md in reader-journey order per the Kueue/ArgoCD precedent.
- **Distribution loop** — Helm chart pair `charts/tide` + `charts/tide-crds` finalized for release: Chart.yaml version bump from `0.1.0-dev` to `1.0.0` lockstep, AUTH-02 carry-forward (per-namespace RoleBinding YAML template + `projectNamespaces: []` Helm value, opt-in default), `helm.sh/resource-policy: keep` annotation on every CRD subchart template (Pitfall 2 mitigation — `helm uninstall tide-crds` no longer cascade-deletes Project/Milestone/Phase/Plan/Task/Wave resources), `helmify` verify-only step + OCI chart publish wired into `release.yaml`.
- **Acceptance loop** — Two distinct proof points wrapping the M0 → M_self bridge. `make dry-run-v1` (DIST-05) is Docker-in-Docker, ubuntu:24.04 clean image, runs the README Quickstart commands verbatim and times them; the small-sample Project reaching `Status=Complete` is the timer-stop. Fires on `v*-rc.*` tag push; blocks goreleaser if >30 min or any step fails. Transcript + `dry-run-report.json` (schemaVersion 1 — forward-compatible) upload to the GitHub Release as assets. `make acceptance-v1` (BOOT-02/BOOT-04) is the maintainer-only ritual on the dev laptop: fresh kind + helm install + `kubectl apply -f examples/projects/large/project.yaml` drives TIDE to author the `internal/subagent/openai/` skeleton phase, single-phase scope, hard $25 cap (no bypass), 7-check verifier asserts pass criteria (3-of-4 commit shapes per MEDIUM-6 honoring D-A1 Single Phase scope).

Result: **M_self consumes M0 artifacts under the same `v1alpha1` CRD schema with no breaking changes across the bridge.** The plumbing is shipped; the maintainer's BOOT-04 ritual is the proof.

## Plans (16/16)

| Plan | Goal | Status | Closeout commit | Completed |
|------|------|--------|-----------------|-----------|
| 05-01 | LICENSE + NOTICE + verify-license.sh + Makefile target (DIST-03) | Complete | `9f145d2` | 2026-05-22 |
| 05-02 | CONTRIBUTING.md + SECURITY.md (DIST-04, D-C5) | Complete | `b64216a` | 2026-05-22 |
| 05-03 | README Quickstart prepend (DIST-04, D-C1 + D-X6 OCI commands) | Complete | `06bc274` | 2026-05-22 |
| 05-04 | docs/README.md 11-entry index + docs/concepts.md + verify-docs-coverage.sh (DIST-04, D-C3 — 11-entry per user-revision 2026-05-22) | Complete | `c32d9d3` | 2026-05-23 |
| 05-05 | Chart.yaml SOT lockstep version bump 0.1.0-dev → 1.0.0 (DIST-01, D-X3) | Complete | `5bd59cb` | 2026-05-21 |
| 05-06 | examples/tide-demo-fixture/ MIT-licensed scaffold (DIST-04, D-B3) | Complete | `1959d70` | 2026-05-22 |
| 05-07 | docs/INSTALL.md operator on-ramp (DIST-04, D-C2 + Pitfall 4 + Pitfall 8) | Complete | `14bc610` | 2026-05-21 |
| 05-08 | docs/project-authoring.md (DIST-04, Variant B prompt guidance + Project.Spec field reference) | Complete | `fa105f4` | 2026-05-21 |
| 05-09 | docs/rbac.md (DIST-04 + AUTH-02 catch-up doc + D-X7 webhook no-op) | Complete | `75119a2` | 2026-05-21 |
| 05-10 | docs/troubleshooting.md (DIST-04, D-C4 — 13-row Symptom/Cause/Recipe table) | Complete | `efd35e8` | 2026-05-21 |
| 05-11 | examples/projects/{small,large}/ samples (DIST-04 + BOOT-04 Variant B acceptance project.yaml) | Complete | `36c8ae1` | 2026-05-22 |
| 05-12 | cmd/tide-demo-init/ binary + medium/ sample (DIST-04 + D-B3 + Topic 4; MEDIUM-11 embed strategy locked + submodule shim) | Complete | `f3818c9` | 2026-05-23 |
| 05-13 | charts/tide/templates/per-namespace-rolebinding.yaml + projectNamespaces values key + test-per-ns-rb render gate (DIST-01 + AUTH-02 catch-up template) | Complete | `7f4a934` | 2026-05-23 |
| 05-14 | CRD-subchart resource-policy: keep annotation on all 6 CRDs (DIST-01 + Pitfall 2; moved to Wave 1 per HIGH-2 file-overlap fix) | Complete | `fe3fd99` | 2026-05-22 |
| 05-15 | Makefile dry-run-v1 + acceptance-v1 + 4 hack/scripts (DIST-05 + BOOT-02 + BOOT-04; 3-of-4 commit shapes per MEDIUM-6) | Complete | `7bdcf0a` | 2026-05-23 |
| 05-16 | release.yaml +3 jobs (helmify-verify + pre-flight + chart-publish) + dry-run.yaml on v*-rc.* tags (DIST-01 + DIST-02 + DIST-05; parent-version-filtered rc match per MEDIUM-9) | Complete | `10dd3c7` | 2026-05-23 |

## Requirements satisfied (8/8)

- **DIST-01** — Helm chart pair v1.0.0 lockstep + per-namespace-rolebinding template + resource-policy:keep annotation + OCI publish to ghcr.io/jsquirrelz/tide-charts. *Plans 05, 13, 14, 16.*
- **DIST-02** — helmify verify-only step in release.yaml (`make helm && git diff --exit-code charts/`). *Plan 16.*
- **DIST-03** — Apache-2.0 LICENSE at repo root + NOTICE + 254/254 Go files carry Apache-2.0 header (104 backfilled in Plan 05-01 commit `14e314e` per Rule 2 deviation) + `make verify-license` CI gate. *Plan 01.*
- **DIST-04** — 5 new docs (INSTALL.md, concepts.md, project-authoring.md, troubleshooting.md, rbac.md) + docs/README.md 11-entry reader-journey index + 3-sample cost spectrum (small/medium/large) + CONTRIBUTING.md + SECURITY.md + README Quickstart + examples/tide-demo-fixture/ MIT-licensed scaffold + cmd/tide-demo-init/ embed binary. *Plans 02, 03, 04, 06, 07, 08, 09, 10, 11, 12.*
- **DIST-05** — `make dry-run-v1` + `.github/workflows/dry-run.yaml` (v*-rc.* tag-triggered) + dry-run-report.json schemaVersion 1 (forward-compatible). *Plans 15, 16.*
- **BOOT-02** — `make acceptance-v1` with fresh kind + helm install (maintainer ritual on dev laptop; no CI integration per D-A4). *Plan 15.*
- **BOOT-04** — examples/projects/large/project.yaml Variant B outcome prompt (authored verbatim from RESEARCH §Topic 5) + 7-check D-A3 verifier (`hack/scripts/acceptance-verify.sh`) asserting per-run branch + 3-of-4 commit shapes (milestone N/A for D-A1 Single Phase scope per MEDIUM-6) + `Project.Status.Phase == Complete` + zero controller ERROR lines + zero orphan Jobs + zero gitleaks blocks + budget under $25 cap. *Plans 11, 15.*
- **AUTH-02 catch-up** — `charts/tide/templates/per-namespace-rolebinding.yaml` (opt-in via `projectNamespaces: []` Helm value, default empty) + `docs/rbac.md` (per-Kind verbs reference + central-SA pattern + opt-in usage) + `make test-per-ns-rb` render gate. *Plans 09, 13.*

## Decisions honored (21 D-* locks)

| ID | Decision | Implemented by |
|----|----------|----------------|
| D-A1 | **Single Phase scope** for v1 acceptance test (Phase brief + PLAN.md + 5-8 Task DAG; falls short of Milestone scope deliberately to bound first real-Claude descent) | Plan 11 (large/project.yaml) + Plan 15 (acceptance-verify.sh 3-of-4 commit shape assertion per MEDIUM-6) |
| D-A2 | **Hard cap $25, no bypass** (`costCeilingCents: 2500`; cap-firing IS itself one of the acceptance signals) | Plan 11 (large/project.yaml `budget.absoluteCapCents: 2500`) + Plan 15 (acceptance-verify.sh Check 7) |
| D-A3 | **Pass criteria — 7 concrete + machine-checkable signals** | Plan 15 (acceptance-verify.sh 7-check gate) |
| D-A4 | **Maintainer-only `make acceptance-v1` on dev laptop**, no CI integration; evidence captured under `.acceptance-runs/<timestamp>/` | Plan 15 (acceptance-v1.sh requires ANTHROPIC_API_KEY env, refuses without) |
| D-B1 | **Cost spectrum** (small $0 stub → medium ~$5 mini-Claude → large ~$25 acceptance) | Plans 11 (small + large) + 12 (medium) |
| D-B2 | **`examples/projects/{small,medium,large}/`** canonical location; `config/samples/` α…θ fixture stays where it is | Plans 11 + 12 (zero changes to `config/samples/`) |
| D-B3 | **examples/tide-demo-fixture/** MIT-licensed seed in THIS repo; medium sample uses local-only git remote (file://) | Plan 06 (fixture) + Plan 12 (cmd/tide-demo-init + medium/) |
| D-B4 | **Large sample outcome prompt:** "Author the scaffold for `internal/subagent/openai/` mirroring `internal/subagent/anthropic/`" (Variant B from RESEARCH §Topic 5) | Plan 11 (verbatim in large/project.yaml — initially via annotation, migrated to spec field by mid-phase root-cause fix 6127806) |
| D-C1 | **README stays the load-bearing paradigm spec**; ~24-line Quickstart prepended ABOVE | Plan 03 (README.md +24 lines top-of-file) |
| D-C2 | **`docs/INSTALL.md` is the on-ramp** (per-OS install paths + CRDs-first install order) | Plan 07 |
| D-C3 | **Docs reader-journey order in `docs/README.md`** — 11 numbered entries, 12 file links (entry #4 co-located dashboard + cli); concepts.md adopted at slot #2 per user revision 2026-05-22 (HIGH-4 + LOW-15 enforcement) | Plan 04 (docs/README.md + concepts.md + verify-docs-coverage.sh --strict / --non-strict modes) |
| D-C4 | **`docs/troubleshooting.md` shape:** single Markdown Symptom/Cause/Recipe table, ≥12 entries | Plan 10 (13-row table; finalizer-stuck recipe first per ROADMAP P5 mandate) |
| D-C5 | **Contributor OSS docs ship in v1**: CONTRIBUTING.md + SECURITY.md; CODE_OF_CONDUCT.md + GOVERNANCE.md deferred to post-v1 | Plan 02 (CONTRIBUTING + SECURITY; both root files) |
| D-D1 | **`make dry-run-v1`** is the DIST-05 mechanism (Docker-in-Docker + ubuntu:24.04 + README commands verbatim) | Plan 15 (hack/scripts/dry-run-v1.sh) |
| D-D2 | **Timer stops at small-sample `Status=Complete`**; uses stub-subagent (zero LLM cost — safely repeatable) | Plan 15 (dry-run-v1.sh timer logic + Plan 11's small/project.yaml as target) |
| D-D3 | **Release-candidate tag gate** — `make dry-run-v1` runs on `v*-rc.*` tag push only (no schedule cron; same posture as P2.4) | Plan 16 (.github/workflows/dry-run.yaml `on: push: tags: ['v*-rc.*']`) |
| D-D4 | **Evidence:** transcript + `dry-run-report.json` (schemaVersion 1) upload to GitHub Release | Plan 15 (render-dry-run-report.sh — heredoc-rendered JSON, no jq dep) + Plan 16 (release.yaml actions/upload-artifact + actions/download-artifact) |
| D-X1 | **LICENSE at repo root** — Apache-2.0 boilerplate, `Copyright 2026 The TIDE Authors`; NOTICE added for k8s.io/* + controller-runtime + kubebuilder propagation | Plan 01 (commit `4b4fb04`) |
| D-X2 | **`helmify` wiring into release.yaml** — verify-only step (lean: charts already hand-tuned, no auto-regen) | Plan 16 (helmify-verify job; `make helm && git diff --exit-code charts/`) |
| D-X3 | **Chart version bump 0.1.0-dev → 1.0.0 lockstep** (both tide + tide-crds via SOT files + augment scripts) | Plan 05 |
| D-X4 | **AUTH-02 catch-up — per-namespace RoleBinding template** with `projectNamespaces: []` Helm value (default empty; opt-in for multi-Project installs) | Plan 13 (charts/tide/templates/per-namespace-rolebinding.yaml + projectNamespaces values key in SOT) |
| D-X5 | **Chart distribution surface** — Helm OCI to ghcr.io/jsquirrelz/tide-charts as v1 primary (index.yaml deferred — single OCI channel for v1) | Plan 16 (chart-publish job: `helm push tide-crds-1.0.0.tgz oci://...` then `helm push tide-1.0.0.tgz oci://...`) |
| D-X6 | **README Quickstart uses OCI commands** (`helm install tide-crds oci://ghcr.io/jsquirrelz/tide-charts/tide-crds --version 1.0.0`) | Plan 03 (README.md +24 lines; 4 OCI commands verbatim) |
| D-X7 | **Conversion webhook stays a no-op** for v1.0 (v1alpha1 IS the hub; conversion-webhook activation is v1.x `v1beta1` work) | Plan 09 (docs/rbac.md §"Conversion webhook (D-X7 — no-op for v1.0)") |

(21 decisions = 4 acceptance-test D-A* + 4 sample D-B* + 5 docs D-C* + 4 dry-run D-D* + 7 cross-cutting D-X* — all locked in CONTEXT.md, all implemented.)

## Pitfalls mitigated (12)

Per `05-RESEARCH.md` §"Common Pitfalls":

- **Pitfall 1 (helmify regen drops augmentations)** — `hack/helm/augment-tide-crds-chart.sh` idempotent re-injector preserves resource-policy:keep + controller-gen catch-up on every `make helm-crds`. *Plans 13, 14.*
- **Pitfall 2 (CRD subchart upgrade drops CRDs + cascade-deletes data)** — `helm.sh/resource-policy: keep` annotation injected on all 6 CRD templates. *Plan 14 (moved to Wave 1 per HIGH-2).*
- **Pitfall 3 (helm push permissions)** — `packages: write` job-scoped permission on chart-publish job. *Plan 16.*
- **Pitfall 4 (CRDs not registered first)** — `docs/INSTALL.md` install order: `helm install tide-crds` BEFORE `helm install tide`. *Plan 07.*
- **Pitfall 5 (dry-run 30-min margin)** — `DRY_RUN_TIMEOUT_SECONDS` env var on dry-run-v1.sh; if first baseline > 25 min, Plan 16's kind-image caching is the documented quick-task hook (not pre-emptively applied — wait for empirical signal). *Plan 15.*
- **Pitfall 6 (DinD permissions)** — canonical `/var/run/docker.sock` mount + `--network host` (not nested dockerd, no `--privileged`). *Plan 15.*
- **Pitfall 7 (rolling-window reset mid-run)** — `examples/projects/large/project.yaml` carries explicit `budget.rollingWindowDuration: "24h"` so the $25 cap doesn't reset mid-acceptance-run. *Plan 11.*
- **Pitfall 8 (OSS-adoption-death-by-missing-docs)** — README Quickstart prepend (Plan 03) + concepts.md operator mental model (Plan 04) + "Is TIDE for me?" 3+3 framing in INSTALL.md (Plan 07).
- **Pitfall 9 (sample namespace collision)** — `tide-sample-*` prefix on all 3 sample namespaces (distinct from Phase 1's `tide-samples`). *Plans 11, 12.*
- **Pitfall 10 (large sample outcome vague)** — Variant B prompt embedded verbatim in large/project.yaml + project-authoring.md walkthrough. *Plans 08, 11.*
- **Pitfall 11 (goreleaser extra_files glob)** — `actions/upload-artifact` (in dry-run.yaml) + `actions/download-artifact` (in release.yaml) instead of goreleaser `extra_files`. *Plan 16.*
- **Pitfall 12 (tide-demo-init path unreachable)** — in-cluster Job + ReadWriteOnce PVC pattern; manifests applied to the `tide-sample-medium` namespace; binary embeds fixture via `//go:embed all:fixture` + Dockerfile COPY + submodule shim (go.mod → go.mod.txt at materialization time). *Plan 12.*

## Iteration 2 revisions applied (2026-05-22)

The post-planning plan-checker surfaced 14 findings; all addressed before Wave 1 dispatched:

- **HIGH-1** — VALIDATION.md per-task verification map populated with all 17 plan task IDs; frontmatter `nyquist_compliant: true` + `wave_0_complete: true`; approval flipped to "approved 2026-05-22".
- **HIGH-2** — Plan 14 moved from Wave 2 to Wave 1 (no file overlap with Plan 05; both touch different chart-tree paths).
- **HIGH-3** — RESEARCH.md Open Questions heading flipped to "## Open Questions (RESOLVED)" with per-question RESOLVED markers (Q1 → adopted concepts.md; Q6 → central-SA pattern; rest noted in research artifact).
- **HIGH-4** — Plan 04 reads 11 numbered entries (12 file links — entry #4 co-located dashboard + cli on a single numbered line) per CONTEXT.md D-C3 revision; LOW-15 made the verifier two-mode (--strict for CI, --non-strict for partial-Wave state).
- **HIGH-5** — Plan 01 adds Makefile to files_modified; Plan 04 moved to Wave 2 with depends_on [01] (sibling ##@ stanza ordering for verify-license + verify-docs targets).
- **MEDIUM-6** — Plan 15 `acceptance-verify.sh` Check 2 asserts 3-of-4 commit shapes (milestone N/A for D-A1 Single Phase scope — TIDE never authors Milestone-level artifact at this governance level).
- **MEDIUM-7** — Plan 11 small/project.yaml uses placeholder `targetRepo: file:///tmp/no-such-repo` (unreachable by design — proves stub-subagent doesn't resolve targetRepo).
- **MEDIUM-8** — Plan 12 split into 2 tasks (Task 1: Go binary + tests `56a745b`; Task 2: Dockerfile + manifests `6db7cf1`).
- **MEDIUM-9** — Plan 16 pre-flight job uses parent-version-filtered rc tag match (`git tag --list "v${PARENT_VERSION}-rc.*"`) — prevents stale rc from different version false-passing the release gate.
- **MEDIUM-10** — Plan 05 + 13 frontmatter removed derived files (charts/tide/values.yaml, charts/*/Chart.yaml) from files_modified — these are augment-script outputs, SOT files are the planning surface.
- **MEDIUM-11** — Plan 12 embed strategy LOCKED to `//go:embed all:fixture` + Dockerfile COPY + `//go:generate`. (Submodule shim — go.mod → go.mod.txt during materialization, restored at unpack time — added inline during Plan 12 execution to handle Go's same-module embed constraint.)
- **LOW-13** — Plan 11 verify command switched from `kubectl apply --dry-run=client` to `yq eval '.'` (or python yaml fallback when yq unavailable).
- **LOW-14** — Plan 17 (THIS plan) STATE.md updates degrade gracefully when SDK lacks `state-set` verb (the registry doesn't carry it; fallback path explicitly authorized in the plan body — direct frontmatter edit + body-prose match-to-frontmatter — used here). SDK roundtrip via `state.json` after 05-SUMMARY.md lands will return `completed_phases: 8`, `completed_plans: 100`, `percent: 100`.
- **LOW-15** — Plan 04 `verify-docs-coverage.sh` has `--non-strict` (default; Wave 1/2 partial-state passes) + `--strict` (CI gate; full coverage required) modes.

## Mid-phase root-cause fix wave (commit `6127806`, 2026-05-23)

A schema-gap pattern surfaced repeatedly across Wave 1-3 sample plans (05-11, 05-12) — `Project.Spec` was missing fields the planner-time assumed (`outcomePrompt` carried via annotation as a workaround; `file://` URLs blocked by CEL XValidation). The plan-bodies worked around the gaps by carrying `outcomePrompt` as the `tideproject.k8s/outcome-prompt` annotation and adding "schema gap" notes.

Rather than carry the workarounds into Wave 4+ where `acceptance-verify.sh` would need to read from annotation rather than spec, the maintainer landed a substantive root-cause fix BEFORE Wave 4 dispatched:

```
6127806 fix(05): close Wave 1-3 schema-gap deferrals — Project.Spec.OutcomePrompt + file:// URLs
```

Changes:

- `api/v1alpha1/project_types.go`: `Project.Spec.OutcomePrompt string` added (optional, multi-line YAML literal shape — read verbatim by the planner subagent at dispatch time).
- `ProjectSpec` XValidation relaxed to accept `file://` URLs alongside http(s) and SSH.
- `GitConfig.RepoURL` Pattern relaxed from `^https?://.+` to `^(https?://|file:///).+`.
- CRDs regenerated (`make manifests` + `make helm-crds`) — config/crd/bases + charts/tide-crds/templates updated.
- Sample migrations: `examples/projects/{large,medium}/project.yaml` annotation removed; outcome prompt now in `spec.outcomePrompt`. (`small/project.yaml` validates cleanly under the relaxed XValidation — no file change needed.)
- Schema test `TestProjectCRDSchemaHasRepoURLPattern` updated to match the new pattern.
- `deferred-items.md`: 2 of 8 entries flipped to **RESOLVED** with date + resolving commit shape.

Wave 4 (Plan 05-15) and Wave 5 (Plan 05-16) inherited a cleanly-typed schema: `acceptance-verify.sh` Check 1 reads outcome prompt from `spec.outcomePrompt`, not annotation; the dry-run small sample's `file:///tmp/no-such-repo` is now valid spec rather than schema-violating placeholder. **Iteration shape lesson:** when a workaround appears in 2+ plans, surface a root-cause fix before the next wave inherits the workaround as a load-bearing contract.

## Deferred items (8 total: 2 RESOLVED mid-phase, 6 v1.x backlog)

Logged in `.planning/phases/05-distribution-self-hosting-acceptance/deferred-items.md`:

**Resolved mid-phase (root-cause fix `6127806`):**

- ✅ **Project.Spec missing outcomePrompt field** (discovered in Plan 05-11; medium-sample 05-12 inherited same gap) — RESOLVED via `Project.Spec.OutcomePrompt` addition + CRD regen + sample migrations.
- ✅ **file:// URLs blocked by CEL XValidation** (discovered in Plans 05-11 + 05-12 — small/medium samples use file:// targetRepo) — RESOLVED via XValidation relaxation + `GitConfig.RepoURL` Pattern update.

**v1.x backlog (6 items remaining):**

- **Plan 05-01 gofmt drift in cmd/dashboard/api/{plans,tasks}.go** — already carry Apache-2.0 headers; verify-license gate unaffected. Defer to /gsd:quick or a Phase 6 cleanup pass.
- **Plan 05-05 SOT-vs-chart-tree drift in `hack/helm/{tide-values,projects-pvc}.yaml`** — `podAnnotations: {}` + `accessModes` template-loop missing from SOT (present in derived chart-tree). Augment-script re-runs would drop them. **Severity: Low** — current chart-tree is correct, CI helm-lint passes; the drift only manifests on next SOT bump. Future SOT-resync plan should promote the fields into SOT.
- **Plan 05-08 worktree-path resolution drift recovery** — known issue per `worktree-path-safety.md` #3099 (absolute paths constructed in agent context can land in main repo cwd). Plan 05-08 caught and recovered (mv from main into worktree). Worth pre-empting in any future worktree-spawning workflow.
- **Plan 05-12 Go embed submodule shim** — `//go:embed` rejects directories carrying a sibling `go.mod`. Plan 05-12 added a rename-shim (go.mod → go.mod.txt during materialization, restored at unpack). Document in CONTRIBUTING.md if/when other in-tree binaries need to embed module-shaped content (v1.x).
- **CONTEXT.md §"Deferred Ideas"** carry-forward: CODE_OF_CONDUCT.md (post-v1; solo-maintainer posture), GOVERNANCE.md (post-v1; BDFL works for v1.0), Mkdocs site at tide.dev/ (post-v1; docs/README.md index is the v1 surface), External public fixture repo `github.com/jsquirrelz/tide-demo-fixture` (explicitly rejected; local-only file:// remote is v1 mechanism), Friend-of-project cold-read pass (informal sanity check pre-v1.0, not gating), Nightly cron dry-run (rejected — tag-gate only), Cosign/SLSA provenance signing (v1.x scope per .goreleaser.yaml footer), OperatorHub/OLM bundle submission (v1.x), index.yaml chart distribution (deferred — OCI is v1 primary), Multiple-version chart coverage (v1.0 ships exactly one chart version), CodeQL/SARIF security scanning (v1.x).
- **REQUIREMENTS.md AUTH-02 catch-up** — satisfied by `charts/tide/templates/per-namespace-rolebinding.yaml` + `docs/rbac.md` (Plans 09 + 13). Update REQUIREMENTS.md traceability table if not already reflected post-closeout commit. (Future maintenance pass.)

## Residual UAT items

- **BOOT-04 acceptance ritual** — `make acceptance-v1` is a maintainer ritual on the dev laptop (D-A4). Plan 05-15 ships the plumbing; Plan 05-15 executor does NOT run the ritual end-to-end (cost ~$25 Anthropic + ~30-60 min). The maintainer runs this manually before tagging v1.0.0 release, captures evidence under `.acceptance-runs/<timestamp>/`, attaches transcript + dashboard screenshot to release notes.
- **Dashboard screenshot** — Chrome DevTools MCP step in acceptance-v1.sh is documented but operator-only (the maintainer's local browser + MCP screenshots the dashboard at run-completion). Phase 04.1 Plan 04.1-14 exercised this against the dashboard; Phase 5 reuses the pattern.
- **First dry-run baseline** — Phase 5's first `make dry-run-v1` will establish the timing baseline. If `totalSeconds > 1620` (27 min, leaving < 3 min margin on the 30-min Pitfall 5 threshold), Plan 16's kind-image caching optimization (or similar) becomes a quick-task target. Don't pre-emptively optimize — measure first.

## Next steps (maintainer playbook)

1. **Tag `v1.0.0-rc.1`** from this closeout commit. `dry-run.yaml` fires automatically:
   - Pulls ubuntu:24.04, mounts `/var/run/docker.sock`, runs `make dry-run-v1` (clone + kind create + helm install tide-crds + helm install tide + kubectl apply -f examples/projects/small/project.yaml + wait Status=Complete).
   - Produces `dry-run-report.json` artifact (schemaVersion 1) uploaded via `actions/upload-artifact`.
2. **Review the artifact** — `jq '.totalSeconds' dry-run-report.json` should be < 1800 (30 min). If close to the cap, evaluate kind-image caching.
3. **Run `make acceptance-v1` locally** — ANTHROPIC_API_KEY + GH_PAT envs required; refuses without (D-A4). Watches dashboard via `kubectl port-forward svc/tide-dashboard 8080` + Chrome DevTools MCP screenshots at run-completion. Inspects evidence under `.acceptance-runs/<timestamp>/`. Reviews the per-run branch `tide/run-large-acceptance-<unix-ts>` on this repo (the large sample targets THIS TIDE repo).
4. **If 7-check verifier passes** (per `hack/scripts/acceptance-verify.sh` — branch exists + 3-of-4 commit shapes + Status=Complete + zero controller ERROR lines + zero orphan Jobs + zero gitleaks blocks + budget under $25): **tag `v1.0.0`**. `release.yaml` fires:
   - **helmify-verify** job (`make helm && git diff --exit-code charts/`) — gates the rest of the chain.
   - **pre-flight** job (parent-version-filtered rc tag match per MEDIUM-9; uses `gh run list` to confirm rc dry-run passed).
   - **release** job (goreleaser builds binaries: tide CLI + manager + credproxy + tide-push + tide-demo-init + stub-subagent + claude-subagent).
   - **chart-publish** job (helm push tide-crds-1.0.0.tgz to oci://ghcr.io/jsquirrelz/tide-charts/tide-crds, then tide-1.0.0.tgz — order matters per Plan 16 decision: tide depends on tide-crds).
5. **v1.0.0 release page on GitHub** carries: binaries (goreleaser), Helm charts (OCI at ghcr.io/jsquirrelz/tide-charts), `dry-run-report.json` artifact (from rc workflow), maintainer's acceptance-run transcript + dashboard screenshot.

## Phase 5 final totals

| Category | Count |
|----------|-------|
| Total plans | 17 (16 execution + 1 closeout) |
| Waves | 6 (W1-W6) |
| OSS-readiness root files | 4 (LICENSE, NOTICE, CONTRIBUTING.md, SECURITY.md) |
| New docs | 5 (INSTALL, concepts, project-authoring, troubleshooting, rbac) + 1 index (docs/README.md) |
| Sample Projects | 3 (small/medium/large cost spectrum) |
| New chart templates | 2 (per-namespace-rolebinding.yaml + resource-policy:keep annotations on all 6 CRDs) |
| New CI workflows | 1 (dry-run.yaml on v*-rc.*) + 3 new jobs added to release.yaml |
| Hack scripts shipped | 6 (verify-license + verify-docs-coverage + test-per-ns-rb + dry-run-v1 + render-dry-run-report + acceptance-v1 + acceptance-verify = 7 total, 6 newly authored in Phase 5) |
| Makefile targets added | 5 (verify-license, verify-docs, test-per-ns-rb, dry-run-v1, acceptance-v1) |
| Requirements satisfied | 8 (DIST-01..05 + BOOT-02 + BOOT-04 + AUTH-02 catch-up) |
| Decisions honored (D-* locks) | 21 (4 D-A* + 4 D-B* + 5 D-C* + 4 D-D* + 7 D-X* — note: D-X4 + D-X5 + D-X6 + D-X7 carry forward from prior phase rolls) |
| Iteration 2 revisions | 14 (HIGH-1..5, MEDIUM-6..11, LOW-13..15) |
| Pitfalls mitigated | 12 (Pitfalls 1-12 from RESEARCH.md) |
| Mid-phase root-cause fixes | 1 (commit `6127806` closing 2 deferred items pre-Wave-4) |
| Deferred items at closeout | 8 total (2 RESOLVED + 6 v1.x backlog) |
| Outstanding UAT items | 3 (BOOT-04 ritual + dashboard screenshot + first dry-run baseline — all maintainer-side, not v1.0 blockers) |

## Deviations from PLAN.md

1. **`gsd-sdk query state-set` does not exist in the registry** (LOW-14 anticipated this). Used the plan-body-authorized fallback: direct frontmatter Edit on STATE.md with body-prose matched to frontmatter values literally. The plan body's explicit allowance ("If the SDK lacks `state-set`, fall back to direct frontmatter edit using `sed -i` with field-specific patterns") covered this case. SDK roundtrip via `gsd-sdk query state.json` after 05-SUMMARY.md lands will return `completed_phases: 8`, `completed_plans: 100`, `percent: 100` (verified by computing disk-derived counts: 100 PLAN files + 100 SUMMARY files across 8 milestone phase dirs).

2. **ROADMAP plan rows counted differently from Progress row.** Plan body asks for "16/16" in the Progress row + "16 bulleted lines `- [x] 05-NN-PLAN.md`" for plans 01-16; success_criteria from spawn context asks "All 17 plan checkboxes in Phase 5 plans list are `[x]`". Honored both: Progress row shows 16/16 (execution-plan count, matches the plan body's explicit `grep -q "5. Distribution & Self-Hosting Acceptance | 16/16 | Complete"` acceptance criterion); plan rows show 17/17 ticked (matches the spawn context + visible ROADMAP reality where 05-17 IS in the plan list). No semantic conflict — the count is "execution plans" not "all plans including this self-closeout".

3. **Top-level overview phase checkboxes (lines 19-23) left unticked.** ROADMAP carries TWO sets of phase markers — a flat 5-phase overview list at the top (lines 19-23, all `[ ]`) and per-phase Progress table rows (lines 300-306, source of truth). The overview list was never flipped by any prior closeout (Phases 1-4 all complete but those overview boxes still `[ ]`). Preserved that established pattern — the Progress table at lines 300+ is the SOT. Future cleanup could flip them, but it's not in scope for a closeout commit per the precedent.

No other deviations. All 3 plan tasks executed as specified.

## Commit shape

| # | Task | Commit |
|---|------|--------|
| 1 | ROADMAP.md updates (Progress row + Plans count + 17 plan rows + closing footer) | `93a6aca` |
| 2 | STATE.md updates (frontmatter counters + body Current Position + Current focus) | `27cee9d` |
| 3 | 05-SUMMARY.md authored + final closeout chore commit | TBD (closeout) |

## Self-Check: PASSED

Per the executor self-check protocol (verified post-Task-3 commit, pre-closeout commit):

**Files created:**
- ✅ `.planning/phases/05-distribution-self-hosting-acceptance/05-SUMMARY.md` (this file; 241 lines)

**Files modified:**
- ✅ `.planning/ROADMAP.md` (Plans count + 17 plan rows + Progress table row + closing footer)
- ✅ `.planning/STATE.md` (frontmatter counters + body Current Position + Current focus)

**Commits:**
- ✅ `93a6aca` — `docs(05-17): flip ROADMAP Phase 5 to 16/16 Complete + tick all 17 plan rows`
- ✅ `27cee9d` — `docs(05-17): bump STATE.md frontmatter + body for Phase 5 closeout`
- ✅ `2ddb1e7` — `docs(05-17): author 05-SUMMARY.md — Phase 5 closeout`

**SDK roundtrip verification (LOW-14):**

```bash
$ gsd-sdk query state.json | jq '.progress'
{
  "total_phases": 8,
  "completed_phases": 8,
  "total_plans": 100,
  "completed_plans": 100,
  "percent": 100
}
```

All 8 milestone phases at PLAN/SUMMARY parity (disk-derived counts match the STATE.md frontmatter values; no preservation-fallback required).

---

**Phase 5 closed 2026-05-23 — M0 → M_self bridge complete — v1.0 ship-ready.**
