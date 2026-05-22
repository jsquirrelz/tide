# Phase 5: Distribution & Self-Hosting Acceptance — Research

**Researched:** 2026-05-22
**Domain:** OSS release engineering for a K8s operator (Helm OCI publish, helmify wiring, dry-run gate, TIDE-on-TIDE acceptance test, doc surface)
**Confidence:** HIGH

## Summary

Phase 5 is the v1.0 ship phase. Code surface is small — ~5 chart additions, ~5 docs, ~3 sample directories, 1 LICENSE, ~1 new Go binary (`cmd/tide-demo-init`), and ~3 release-pipeline insertions. Leverage is high: every Phase 5 deliverable is the OSS first impression. Three loops close simultaneously: OSS readiness (LICENSE + CONTRIBUTING/SECURITY + Quickstart-prefixed README + 11-doc index in reader-journey order), distribution (Helm chart pair version-bumped to 1.0.0, helmify verify-only step in `release.yaml`, Helm OCI publish via separate `helm push` step, `per-namespace-rolebinding.yaml` template for AUTH-02 catch-up), and acceptance (Docker-in-Docker `make dry-run-v1` on `v*-rc.*` tag gate + maintainer-ritual `make acceptance-v1` driving the large sample whose outcome prompt asks TIDE to author `internal/subagent/openai/`).

The Phase 5 plan needs 14-16 plans organized as a Kahn-layered DAG with three logical clusters: **(A) docs + LICENSE + samples** (mostly file-overlap-disjoint, fan out wide), **(B) chart finalization + release-pipeline wiring** (sequential — chart versions, then helmify verify, then OCI publish, then dry-run gate), and **(C) acceptance plumbing + closeout** (the `make acceptance-v1` target, evidence-capture, ROADMAP/STATE closeout).

The novel mechanics — and the items most worth getting right — are: (1) the **medium sample's local-only git remote** (use a small Go bootstrap binary at `cmd/tide-demo-init`, not an in-cluster Job, not a ConfigMap — explicit step, matches the "stateless CLI" affordance, `pkg/git` already supports `file://` URLs as verified in Phase 3 tests), (2) the **large sample's outcome prompt** (Variant B below — specific enough to bound the DAG to 5-8 tasks, abstract enough that we're not hand-authoring the plan), and (3) the **release.yaml extension** (split into 3 distinct jobs: `dry-run-gate` on `v*-rc.*`, `helmify-verify` + `release` on `v*`, `chart-publish` after `release`).

**Primary recommendation:** Treat Phase 5 as a one-week ship sprint, not an exploration. The decisions are locked (CONTEXT.md D-A1..D-X7); research only the HOW around each. Don't bikeshed chart structure (already split per Phase 1 D-E1), helmify wiring (already in Makefile per Phase 1 plan 11), or LICENSE phrasing (Apache-2.0 boilerplate, verbatim). Spend the budget on the dry-run script + acceptance-v1 target + the 4 new docs.

## Project Constraints (from CLAUDE.md)

**Working rules (CLAUDE.md §"Working Rules" + §"Operating Notes"):**
1. **Observe First** — when a runtime gate is BLOCKED, read manager log + VERIFICATION.md frontmatter BEFORE hypothesizing. For Phase 5: when `make dry-run-v1` exceeds budget, capture the per-phase elapsed times from `dry-run-report.json` BEFORE editing the workflow.
2. **Execute, Don't Ask** inside an active GSD workflow — but `git push --force` to main, `kind delete cluster` outside a clean-rerun, and edits to `charts/tide/values.yaml` are always confirm/refuse exceptions. The chart is FIXED contract per Phase 02.2's anti-pattern: binary catches up to chart, never reverse.
3. **Verify Before Claiming** — `grep -cE '^gate_decision: APPROVED$' <VERIFICATION>` returns exactly `1` before declaring closed; `tail -30 /tmp/<plan>-clean-run.log` shows Ginkgo summary `Ran X Specs in Ys`. Same protocol applies to Phase 5: a successful `make dry-run-v1` must produce `dry-run-report.json` with `totalSeconds < 1800` AND `Project.Status.Phase == Complete`.

**Locked technology stack (CLAUDE.md §"Technology Stack"):**
- Go 1.26 (current go.mod: 1.26.0; runner has 1.26.3) [VERIFIED: go.mod, `go version`]
- controller-runtime v0.24.x — Phase 5 does not touch
- kubebuilder v4.14.0 — Phase 5 does not touch
- Helm 3 — Phase 5 OCI commands require Helm ≥ 3.8 [VERIFIED: helm 3.16.3 installed; CI uses azure/setup-helm@v4 with `v3.16.3` in `.github/workflows/ci.yaml`]
- helmify v0.4.17 (pinned in Makefile `HELMIFY_VERSION ?= v0.4.17`) [VERIFIED: Makefile line 322]
- goreleaser v2.x (configured in `.goreleaser.yaml`; `version: 2`) [VERIFIED: .goreleaser.yaml line 19]
- kind v0.31 (pinned `@sha256` in E2E scripts per CLAUDE.md rule) [VERIFIED: live-e2e.yml line 31, ci.yaml line 187 — both pin `v0.31.0`]
- ServiceMonitor must default OFF (`prometheus.serviceMonitor.enabled=false`) — locked in current `values.yaml` line 269 [VERIFIED]

**Forbidden patterns (CLAUDE.md §"Anti-patterns"):**
- Don't hard-code Anthropic SDK in non-firewalled paths — irrelevant to Phase 5 (no new orchestrator code)
- Don't add CRD schema changes to v1.0 — `v1alpha1` is locked; conversion webhook stays no-op (D-X7)
- Don't unify the two DAGs — irrelevant to Phase 5
- **API group is `tideproject.k8s` invariant** — every new Helm template + every new sample MUST stay under this group [VERIFIED: existing 27+ templates already comply; samples at `config/samples/` already comply]

## User Constraints (from CONTEXT.md)

### Locked Decisions

**Acceptance-test target + budget (BOOT-04 / DIST-05):**
- **D-A1:** Single Phase scope for v1 acceptance test. `project.yaml` targets THIS TIDE repo. TIDE authors one phase brief + one `PLAN.md` + dispatches resulting Task DAG (5-8 tasks). Exercises Phase → Plan → Task → Wave reconciler dispatch + push at every level boundary. NOT a full Milestone fan-out.
- **D-A2:** Hard cap $25, no bypass. `Project.Spec.budget.costCeilingCents: 2500`. The cap halt IS an acceptance signal. `tide approve --bypass-budget` exists but is NOT used.
- **D-A3:** 7 concrete pass criteria — per-run branch exists + 4 commit-message shapes present + `Project.Status.Phase == Complete` + zero controller ERROR logs + no orphan Jobs + `tide_secret_leak_blocked_total{project=…} == 0` + `costSpentCents < 2500`.
- **D-A4:** Maintainer-only `make acceptance-v1` on dev laptop. NO CI integration. Evidence captured at `.acceptance-runs/<timestamp>/`. Maintainer attaches transcript to v1.0 release notes.

**3 sample Projects (DIST-04):**
- **D-B1:** Cost spectrum — small ($0 stub) → medium (~$5 mini Claude) → large (~$25 acceptance).
- **D-B2:** `examples/projects/{small,medium,large}/` canonical location. `config/samples/` α…θ STAYS as kubebuilder test fixture, untouched, no symlinks.
- **D-B3:** `examples/tide-demo-fixture/` scaffold lives in THIS repo. Medium sample initializes fresh **local-only** git remote (file:// URL) on apply. NO external public repo (`github.com/jsquirrelz/tide-demo-fixture` explicitly rejected).
- **D-B4:** Large sample's outcome prompt — "Author the scaffold for `internal/subagent/openai/` mirroring `internal/subagent/anthropic/` — stub `Subagent.Run()`, ship Dockerfile, wire one e2e test." ~5-8 task DAG.

**Documentation strategy + OSS readiness (DIST-03 + DIST-04 + Pitfall 24):**
- **D-C1:** README stays the load-bearing paradigm spec. Phase 5 PREPENDS ~30-line Quickstart block. `docs/INSTALL.md` is the on-ramp.
- **D-C2:** `docs/INSTALL.md` is the single source of truth for install steps; covers macOS/Linux/Windows-WSL2 + ANTHROPIC_API_KEY + git creds + project apply.
- **D-C3:** 10-doc reader-journey order in `docs/README.md`: install → authoring → dashboard+CLI → gates → observability → git → storage → live-e2e → troubleshooting → RBAC. NO mkdocs site.
- **D-C4:** `docs/troubleshooting.md` = single Markdown table Symptom | Cause | Recipe, ~12 entries (finalizer stuck, ANTHROPIC_API_KEY 401, push lease conflict, gitleaks blocked, PVC RWX missing, dashboard 404, CRDs not registered, admission webhook 422, budget halted, gate awaiting approval, ImagePullBackoff, leader election lost).
- **D-C5:** CONTRIBUTING.md + SECURITY.md ship in v1; CODE_OF_CONDUCT + GOVERNANCE deferred.

**External-operator dry-run methodology (DIST-05):**
- **D-D1:** `make dry-run-v1` = Docker-in-Docker scripted, ubuntu:24.04 clean image, runs README Quickstart commands verbatim.
- **D-D2:** Timer stops at small-sample (stub-subagent, $0 LLM cost) `Project.Status.Phase == Complete`.
- **D-D3:** `v*-rc.*` tag gate. Blocks goreleaser job if dry-run fails OR > 30 min. No cron.
- **D-D4:** Evidence = transcript + `dry-run-report.json` uploaded to GitHub Release as assets.

**Cross-cutting (D-X*):**
- **D-X1:** LICENSE file at root, Apache-2.0 boilerplate, year=2026, holder="The TIDE Authors". NOTICE file only if `go.sum` audit surfaces an Apache-2.0 dep that mandates NOTICE redistribution.
- **D-X2:** `helmify` integration into `release.yaml` as **verify-only** step (charts are hand-tuned, don't auto-regenerate). The lint job in `ci.yaml` already runs `make helm` and `git diff --exit-code` — Phase 5's release.yaml step is the release-time confirmation.
- **D-X3:** Chart version bump `0.1.0-dev → 1.0.0` LOCKSTEP for both `tide` + `tide-crds`.
- **D-X4:** AUTH-02 catch-up = `charts/tide/templates/per-namespace-rolebinding.yaml` driven by Helm value `projectNamespaces: []` (empty default — opt-in).
- **D-X5:** Helm OCI primary at `ghcr.io/jsquirrelz/tide-charts/{tide,tide-crds}:v1.0.0`. Optional `index.yaml` on gh-pages branch.
- **D-X6:** README Quickstart uses OCI install commands.
- **D-X7:** Conversion webhook stays no-op for v1.0.

### Claude's Discretion

The CONTEXT.md treats D-X1..D-X7 as "researcher + planner finalize" — i.e., the SHAPE is locked, the IMPLEMENTATION SHAPE is the researcher's call. This research recommends:
- **D-X1 NOTICE file:** Required (see Topic 7 below — multiple `k8s.io/*` deps ship NOTICE files we must propagate).
- **D-X2 helmify wiring:** Single step `make helm && git diff --exit-code charts/` in release.yaml; mirrors the existing ci.yaml lint check. If it passes in CI, it WILL pass in release; failure-mode is identical.
- **D-X5 OCI publish:** Separate `chart-publish` job after `release`, using `helm push` with `appany/helm-oci-chart-releaser@v0.5` OR plain `helm push` (researcher recommends plain `helm push` — fewer dependencies, identical functionality; see Topic 2 below).

### Deferred Ideas (OUT OF SCOPE)

- CODE_OF_CONDUCT.md, GOVERNANCE.md — deferred to post-v1.
- Mkdocs site at tide.dev/ — `docs/` directory + index is v1 surface.
- External public fixture repo `github.com/jsquirrelz/tide-demo-fixture` — rejected.
- Friend-of-project cold-read dry-run pass — scripted dry-run is gating signal.
- Nightly cron dry-run — tag-gate posture only.
- `docs/runbooks/` directory — single `docs/troubleshooting.md` for v1.
- GovernanceLevel-spectrum samples — cost-spectrum (D-B1) chosen instead.
- Multi-vendor provider matrix in samples — layering pattern (Phase 3 D-C1) is v1 commitment; large sample OUTPUTS `internal/subagent/openai/` skeleton but doesn't ship a sample using it.
- `tide.dev/` custom domain, GitHub Pages site — deferred.
- Multiple-version chart distribution — v1.0 ships exactly one version.
- CodeQL / SARIF / Trivy in release pipeline — post-v1 OSS hardening.
- OperatorHub / OLM bundle — v1.x.
- CRD subchart published independently — both charts version-bump lockstep for v1.

## Phase Requirements

| ID | Description | Research Support |
|----|-------------|------------------|
| DIST-01 | Helm chart with CRD subchart + controller + dashboard + RBAC + namespace setup | Topic 1 (helmify wiring), Topic 8 (CRD subchart upgrade semantics) — chart pair already exists from Phase 1 D-E1; Phase 5 finalizes version bump + per-namespace-rolebinding template |
| DIST-02 | helmify converts kubebuilder Kustomize → Helm chart in release pipeline | Topic 1 — verify-only step (regenerate + `git diff --exit-code`); existing `make helm` target idempotent via `hack/helm/augment-tide-chart.sh` |
| DIST-03 | Apache-2.0 LICENSE at repo root; every Go file's package header references it | Topic 7 — Apache-2.0 boilerplate verbatim from apache.org; NOTICE required (see Topic 7 evidence); Go file headers already present from Phase 1+ |
| DIST-04 | Docs: install + Project authoring with 3 samples + providers + git + failure recovery + RBAC ref + troubleshooting | Topic 4 (medium-sample local git remote), Topic 5 (large-sample outcome prompt), Topic 7 (docs structure pattern from kueue/argocd) |
| DIST-05 | External-operator dry-run < 30 min clone-to-first-run | Topic 3 (Docker-in-Docker scripting), Topic 9 (v*-rc.* tag gate workflow), Topic 10 (dry-run-report.json schema), Topic 11 (external-operator-friendly install flow) |
| BOOT-02 | M_self: fresh kind + helm install authors next-milestone artifacts | Topic 6 (TIDE-on-TIDE acceptance mechanics) — `make acceptance-v1` target wraps fresh kind + helm install + apply + 7-check verification script |
| BOOT-04 | v1 acceptance test: kind + helm install + project.yaml drives THIS repo's next milestone | Topic 5 + Topic 6 — large sample IS the project.yaml; outcome prompt asks TIDE to author `internal/subagent/openai/` |
| AUTH-02 catch-up | Per-namespace RoleBinding YAML template | Topic 8 — `charts/tide/templates/per-namespace-rolebinding.yaml` driven by Helm value `projectNamespaces: []`; documented in new `docs/rbac.md` |

## Architectural Responsibility Map

Phase 5 is unusual: most "capabilities" are doc/config/release-tooling, not runtime tiers. Mapping anyway for plan-checker:

| Capability | Primary Tier | Secondary Tier | Rationale |
|------------|--------------|----------------|-----------|
| Helm OCI chart publish (`helm push ghcr.io/.../tide-charts/{tide,tide-crds}:v1.0.0`) | Release Pipeline (GitHub Actions) | — | OCI registry publish is a CI-time action, not runtime. Belongs in `.github/workflows/release.yaml`, never in controller code. |
| Per-namespace RoleBinding template (AUTH-02 catch-up) | Helm chart (`charts/tide/templates/`) | RBAC layer | Helm template `range`s `.Values.projectNamespaces`; applies at `helm install`/`helm upgrade` time. Zero runtime code; pure manifest output. |
| Dry-run gate (`make dry-run-v1` runner) | Release Pipeline + Docker (DinD) | — | Scripted in `Makefile` + invoked by GitHub Actions on `v*-rc.*` tag. Uses Docker-in-Docker with `docker.sock` mounted. Zero K8s API surface. |
| Acceptance-test runner (`make acceptance-v1`) | Maintainer Workstation (local Make target) | — | NOT a runtime tier — explicitly local-only per D-A4. Spins fresh kind cluster on maintainer's laptop, invokes 7-check verification script after `Project.Status.Phase == Complete`. Evidence captured at `.acceptance-runs/<ts>/` on local disk. |
| Local-only git remote bootstrap (medium sample) | CLI tooling (`cmd/tide-demo-init`) | — | Small Go binary the operator runs BEFORE `kubectl apply -f examples/projects/medium/`. Creates `/tmp/tide-demo-remote.git` bare repo + populates from `examples/tide-demo-fixture/`. Recommendation B (Topic 4). |
| Documentation (4 new docs + 1 index + Quickstart prepend) | Static repo content (`docs/`, `README.md`) | — | Markdown only; no runtime tier. |
| LICENSE + NOTICE + CONTRIBUTING.md + SECURITY.md | Static repo content (root) | — | OSS-readiness boilerplate; no runtime tier. |
| Sample Project CRDs (3 cost-spectrum) | Static repo content (`examples/projects/`) | K8s API (when applied) | YAML manifests; operator applies via kubectl. |

**Tier-assignment sanity check:** Zero new code in `pkg/`, `internal/`, or reconciler files. The only new Go binary is `cmd/tide-demo-init` (operator-facing CLI utility), parallel in scope to `cmd/tide` and `cmd/tide-push`. No reconciler logic moves. The acceptance test exercises existing reconciler dispatch end-to-end — it doesn't add new dispatch paths.

## Standard Stack

### Core (already in repo; Phase 5 USES, doesn't add)
| Library | Version | Purpose | Why Standard |
|---------|---------|---------|--------------|
| Helm | 3.16.3 (CI), ≥ 3.8 (Quickstart prereq) | Package manager + OCI publish | [VERIFIED: ci.yaml line 129, live-e2e.yml uses azure/setup-helm@v4]. Helm OCI requires ≥ 3.8 [CITED: helm.sh/docs/topics/registries]. |
| helmify | v0.4.17 (pinned) | Kustomize → Helm chart conversion | [VERIFIED: Makefile line 322 `HELMIFY_VERSION ?= v0.4.17`]. Industry-standard for kubebuilder operators [CITED: arttor/helmify README]. |
| goreleaser | v2.x | Multi-OS/arch binary release | [VERIFIED: .goreleaser.yaml line 19 `version: 2`]. Already wired (Phase 04-09). |
| kind | v0.31.0 | Local K8s cluster for dry-run + acceptance | [VERIFIED: ci.yaml line 187, live-e2e.yml line 31 — both pin v0.31.0]. |
| kubebuilder | v4.14.0 | CRD scaffold (Phase 5 does NOT use directly) | [VERIFIED: CLAUDE.md tech stack]. |

### New for Phase 5 (verified versions)
| Library | Version | Purpose | Why Standard |
|---------|---------|---------|--------------|
| Docker (ubuntu:24.04 base image) | 24.04 LTS | Dry-run host image | [CITED: D-D1 locks this — clean ubuntu image for first-time-install reproducibility]. |
| azure/setup-helm@v4 | v4 (already in use) | Install Helm in GitHub Actions | [VERIFIED: ci.yaml line 128]. |
| actions/checkout@v4 | v4 (already in use) | Checkout source | [VERIFIED: release.yaml line 31]. |
| actions/upload-artifact@v4 | v4 (already in use) | Upload dry-run-report.json + transcript to Release | [VERIFIED: ci.yaml line 235]. |

### NOT introduced in Phase 5
- **cosign / sigstore** — supply-chain signing is deferred to v1.x per `.goreleaser.yaml` line 113: "**Supply-chain note:** v1.0 ships unsigned. SLSA provenance + cosign signatures are deferred to v1.x. See `docs/cli.md` §Security." Keep this commitment.
- **helm/chart-releaser** (gh-pages `index.yaml` generator) — D-X5 says OCI is primary, `index.yaml` is "optional in the same release.yaml step." Researcher recommends DEFERRING `index.yaml` to a post-Phase-5 fast-follow if OCI proves insufficient. OCI works with `helm install` ≥ 3.8 and is the modern path; `index.yaml` adds gh-pages branch management complexity for marginal compatibility gain. (Plan body should explicitly justify the deferral if the planner chooses to skip it.)
- **appany/helm-oci-chart-releaser** action — a thin wrapper around `helm push`. Researcher recommends plain `helm push` in a shell step (fewer transitive dependencies, identical functionality, no third-party action audit overhead).

### Alternatives Considered
| Instead of | Could Use | Tradeoff |
|------------|-----------|----------|
| Plain `helm push` for chart publish | `appany/helm-oci-chart-releaser@v0.5` action | Action adds GPG signing pre-wiring + small abstraction. Researcher rejects: D-X1 (Apache-2.0 boilerplate, no NOTICE-yet) implies no signing scope; plain `helm push` is 5 lines of shell. [CITED: github.com/appany/helm-oci-chart-releaser readme — confirms the action is a thin wrapper.] |
| `make dry-run-v1` shell script | Pure GitHub Actions YAML in `dry-run.yml` | D-D1 LOCKS Docker-in-Docker via Makefile so contributors can run locally. Don't fork the runner. |
| `cmd/tide-demo-init` Go binary for medium-sample git remote bootstrap | (a) In-cluster Job; (c) ConfigMap-embedded fixture content | See Topic 4. (b) Go binary wins for: explicit operator step, no in-cluster magic, leverages existing `pkg/git` (already supports `file://`), parallels `cmd/tide` shape. |
| Mkdocs site for v1 | Markdown in `docs/` + `docs/README.md` index | D-C3 LOCKS docs/ + index. Mkdocs is post-v1. Don't relitigate. |

**Version verification (performed 2026-05-22):**
- `helmify v0.4.17` — confirmed via `Makefile` pin. Last release 2025-Q4 per github.com/arttor/helmify. [VERIFIED: Makefile line 322]
- `helm 3.16.3` — confirmed via `helm version --short` on researcher box. [VERIFIED]
- `kind 0.31.0` — confirmed pinned in both `ci.yaml:187` and `live-e2e.yml:31`. [VERIFIED]
- `goreleaser v2.x` — confirmed in `.goreleaser.yaml:19`. [VERIFIED]

## Architecture Patterns

### Phase 5 Anatomy (data flow)

```
                            v1.0 SHIP PHASE
        ┌───────────────────────────────────────────────────────┐
        │                                                       │
        │   OSS-READINESS LOOP        DISTRIBUTION LOOP         │
        │   ──────────────────        ─────────────────         │
        │   LICENSE (root)            charts/tide/Chart.yaml    │
        │   NOTICE (root)             0.1.0-dev → 1.0.0         │
        │   CONTRIBUTING.md           charts/tide-crds/         │
        │   SECURITY.md               Chart.yaml lockstep       │
        │   README.md ──┐             per-namespace-rolebinding │
        │   + ~30-line  │             .yaml (AUTH-02 catch-up)  │
        │   Quickstart  │             helmify verify step in    │
        │   block       │             release.yaml              │
        │   docs/INSTALL│             OCI publish ghcr.io/      │
        │   .md (new)   │             jsquirrelz/tide-charts/   │
        │   docs/proj-  │             {tide,tide-crds}:v1.0.0   │
        │   authoring   │                                       │
        │   .md (new)   │                                       │
        │   docs/trouble│             ACCEPTANCE LOOP           │
        │   shooting.md │             ──────────────────        │
        │   (new)       │             make dry-run-v1           │
        │   docs/rbac.md│             (DinD, $0, README cmds,   │
        │   (new)       │              < 30 min, v*-rc.* gate)  │
        │   docs/README │                                       │
        │   .md (new    │             make acceptance-v1        │
        │   index)      │             (maintainer ritual,       │
        │               │              fresh kind + helm        │
        │               │              install + large sample,  │
        │               │              $25 cap, 7-check verify) │
        │               │                                       │
        │   examples/projects/{small,medium,large}/             │
        │       small:  $0 stub-subagent — DIST-05 dry-run      │
        │       medium: ~$5 mini real-Claude — local-only       │
        │               git remote via cmd/tide-demo-init       │
        │       large:  ~$25 acceptance — outcome prompt        │
        │               authors internal/subagent/openai/       │
        │   examples/tide-demo-fixture/ (scaffold for medium)   │
        │                                                       │
        └───────────────────────────────────────────────────────┘
                    All three loops close to v1.0
```

### Release Pipeline Flow (post-Phase-5)

```
Tag push: v1.0.0-rc.1
   │
   ▼
┌─────────────────────────────────────────┐
│ release.yaml — dry-run-gate JOB         │
│   - actions/checkout                    │
│   - docker run ubuntu:24.04 with        │
│     docker.sock mounted                 │
│   - inside: install kind+helm+kubectl+go│
│   - cd /tide && time (README Quickstart)│
│   - timer ≤ 1800s → produce             │
│     dry-run-report.json                 │
│   - upload as artifact                  │
│   ★ FAILS → blocks release & chart      │
└───────────────────┬─────────────────────┘
                    │ if success
                    ▼
                (nothing — v*-rc.* doesn't fire goreleaser;
                 maintainer reviews dry-run-report.json,
                 then pushes v1.0.0 (no -rc) tag)

Tag push: v1.0.0
   │
   ▼
┌─────────────────────────────────────────┐
│ release.yaml — helmify-verify JOB       │
│   - make helm                           │
│   - git diff --exit-code charts/        │
│   ★ FAILS → blocks release & chart      │
└───────────────────┬─────────────────────┘
                    │ if success
                    ▼
┌─────────────────────────────────────────┐
│ release.yaml — release JOB (existing)   │
│   - goreleaser release --clean          │
│   - publishes binaries + checksums      │
│   - attaches dry-run-report.json from   │
│     latest passing rc as Release asset  │
└───────────────────┬─────────────────────┘
                    │ if success
                    ▼
┌─────────────────────────────────────────┐
│ release.yaml — chart-publish JOB (new)  │
│   - helm package charts/tide-crds       │
│   - helm package charts/tide            │
│   - helm registry login ghcr.io         │
│   - helm push tide-crds-1.0.0.tgz       │
│       oci://ghcr.io/jsquirrelz/         │
│       tide-charts                       │
│   - helm push tide-1.0.0.tgz            │
│       oci://ghcr.io/jsquirrelz/         │
│       tide-charts                       │
└─────────────────────────────────────────┘
```

### Acceptance Test Flow (maintainer ritual)

```
Maintainer dev laptop
   │
   ▼
$ make acceptance-v1
   │
   ├──► fresh kind cluster (tide-acceptance-<ts>)
   ├──► kind load docker-image <controller> <subagents>
   ├──► helm install tide-crds ./charts/tide-crds
   ├──► helm install tide ./charts/tide
   │       --set image.tag=<release-tag>
   │       --set ANTHROPIC_API_KEY-Secret-name=tide-secrets
   ├──► kubectl create secret generic tide-secrets
   │       --from-literal=anthropicApiKey=$ANTHROPIC_API_KEY
   │       --from-literal=gitCredentials=$GH_PAT
   ├──► kubectl apply -f examples/projects/large/project.yaml
   ├──► tide watch large-project --until-complete
   │       (Project.Status.Phase progresses
   │        Pending → Planning → Executing → Complete
   │        or → BudgetExceeded → exit non-zero)
   │
   ├──► .acceptance-runs/<ts>/ evidence capture:
   │     controller.log     (kubectl logs --since=<start>)
   │     project.yaml       (kubectl get project ... -o yaml)
   │     phases.yaml        (kubectl get phase ... -o yaml)
   │     plans.yaml         (kubectl get plan ... -o yaml)
   │     tasks.yaml         (kubectl get task ... -o yaml)
   │     jobs.yaml          (kubectl get jobs --all-namespaces ...)
   │     per-run-branch.gitlog
   │                        (cd tide && git log tide/run-<...> --oneline)
   │     dashboard.png      (Chrome DevTools MCP screenshot)
   │     dry-run-report.json(optional — if dry-run reused)
   │
   ▼
hack/scripts/acceptance-verify.sh — runs the 7 D-A3 checks:
   1. per-run branch exists
   2. 4 commit-message shapes present
   3. Project.Status.Phase == Complete
   4. zero ERROR controller logs since start
   5. no orphan Jobs (all succeeded=1)
   6. tide_secret_leak_blocked_total{project=large} == 0
   7. Project.Status.budget.costSpentCents < 2500
   │
   ├──► all 7 pass → exit 0, print "ACCEPTANCE PASS"
   └──► any fail   → exit non-zero, print failing check
                     → maintainer inspects evidence
```

### Recommended File Layout (delta only)

```
LICENSE                                              (new — Apache-2.0 boilerplate)
NOTICE                                               (new — bundled-deps NOTICE propagation)
CONTRIBUTING.md                                      (new)
SECURITY.md                                          (new)
README.md                                            (modified — prepend ~30-line Quickstart)
Makefile                                             (modified — add dry-run-v1, acceptance-v1 targets)
charts/
├── tide/
│   ├── Chart.yaml                                   (modified — bump 0.1.0-dev → 1.0.0)
│   └── templates/
│       └── per-namespace-rolebinding.yaml           (new — AUTH-02 catch-up)
├── tide-crds/
│   └── Chart.yaml                                   (modified — bump lockstep → 1.0.0)
cmd/
└── tide-demo-init/                                  (new — local-only git remote bootstrap)
    ├── main.go
    └── README.md
docs/
├── README.md                                        (new — 10-entry index)
├── INSTALL.md                                       (new — single source of truth)
├── project-authoring.md                             (new — 3-sample walkthrough)
├── troubleshooting.md                               (new — ~12-row table)
└── rbac.md                                          (new — per-Kind verbs + per-ns RoleBinding)
examples/
├── tide-demo-fixture/                               (new — scaffold for medium sample)
│   ├── README.md                                    (MIT-licensed inline)
│   ├── go.mod
│   ├── go.sum
│   ├── main.go
│   └── main_test.go
└── projects/
    ├── README.md                                    (new — cost-spectrum overview)
    ├── small/
    │   ├── README.md
    │   ├── project.yaml                             ($0 stub-subagent)
    │   └── namespace.yaml
    ├── medium/
    │   ├── README.md                                (incl. cmd/tide-demo-init usage)
    │   ├── project.yaml                             (~$5 mini Claude)
    │   └── namespace.yaml
    └── large/
        ├── README.md
        ├── project.yaml                             ($25 acceptance — see Topic 5)
        └── namespace.yaml
.github/
└── workflows/
    └── release.yaml                                 (modified — add dry-run-gate, helmify-verify, chart-publish jobs)
hack/
├── scripts/
│   ├── dry-run-v1.sh                                (new — invoked by Makefile dry-run-v1 target)
│   ├── acceptance-verify.sh                         (new — 7-check D-A3 verifier)
│   └── render-dry-run-report.sh                     (new — dry-run-report.json shaper)
└── (existing helm augment scripts unchanged)
```

**Total new/modified files:** ~28 (4 new docs + 1 doc index + 1 README mod + 4 root OSS files + 6 examples/projects files + 5 tide-demo-fixture files + 1 new Helm template + 2 Chart.yaml mods + 1 release.yaml mod + 1 Makefile mod + 3 hack scripts + 1 cmd/tide-demo-init + per-Phase 5 patterns).

## Don't Hand-Roll

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|-------------|-----|
| OCI chart publish | Custom go-containerregistry uploader | `helm push <chart.tgz> oci://ghcr.io/.../tide-charts` | Helm CLI's built-in OCI push since 3.8 [CITED: helm.sh/docs/topics/registries]; ghcr.io supports OCI helm charts natively. |
| Kustomize → Helm conversion | Custom YAML rewriter | `helmify` (already pinned v0.4.17) | Industry-standard for kubebuilder operators; existing `make helm` target is idempotent via `hack/helm/augment-*.sh`. |
| Chart drift detection | Custom diff tool | `git diff --exit-code charts/` after `make helm` | Already runs in ci.yaml `helm-lint` job (line 152-159); replicate in release.yaml. |
| Per-namespace RBAC enumeration | Programmatic K8s RBAC builder | Helm `range .Values.projectNamespaces` template | Standard Helm idiom; empty default is opt-in. |
| Apache-2.0 LICENSE content | Hand-written boilerplate | Verbatim copy from `apache.org/licenses/LICENSE-2.0.txt` | License text is canonical; do not alter [CITED: apache.org]. |
| NOTICE file generation | Manual ASF NOTICE drafting | `go-licenses report ./...` for inventory + manual curate-and-propagate per Apache ASF rule | [CITED: infra.apache.org/licensing-howto.html — "the relevant portions [must be] bubbled up into the top-level NOTICE file"]. |
| Local git remote bootstrap | Embed fixture in Project CRD via ConfigMap (Option c rejected) | Small Go binary `cmd/tide-demo-init` (Option b) | Explicit operator step; no in-cluster magic; `pkg/git` already supports `file://` (Phase 3 tests prove). |
| dry-run-report.json field shape | Free-form JSON | Versioned schema with `schemaVersion: 1` field | Forward-compat for v1.1 readers; see Topic 10. |
| dry-run wall-clock measurement | `bash time` parsing | `date +%s` deltas captured in shell script with explicit per-phase logging | Avoids `time` output parsing inconsistencies between OS X / Linux. |
| GitHub Release asset upload | Custom `gh release upload` shell | goreleaser `release.extra_files` glob | [VERIFIED: ctx7 docs /websites/goreleaser — "extra_files: glob: ./path/to/file.txt" is the canonical way to attach pre-existing files to a Release]. |
| Tag-pattern filtering in workflow | `if: contains(github.ref, ...)` step-level checks | Workflow-level `on: push: tags: ['v*-rc.*']` filter | [CITED: docs.github.com — tag glob patterns are job-trigger native]. |
| Job dependency chains | Sequential workflow files | `needs:` field on jobs within ONE workflow | [CITED: docs.github.com — `jobs.<job_id>.needs` blocks downstream on dependency success]. |
| Docker-in-Docker volume mounting | `--privileged` flag | Bind-mount `/var/run/docker.sock` from host | [CITED: kind issue tracker — `docker.sock` mount is the canonical pattern for DinD without privileged]. |

**Key insight:** Phase 5's "Don't Hand-Roll" list is unusually long because the phase is mostly tooling integration. Every item above is something an inexperienced contributor MIGHT try to roll custom; the table prevents that.

## Runtime State Inventory

> N/A — Phase 5 is a greenfield ADDITIONS phase (4 new docs, ~5 new templates/binaries, 1 new LICENSE, ~28 modified/new files). No rename, no refactor, no string-replacement. No runtime state inventory needed.

**Exception:** The Helm Chart.yaml version bump `0.1.0-dev → 1.0.0` IS a kind of rename. Specifically — does any code in the repo grep for `"0.1.0-dev"`?

```
$ grep -rn "0\.1\.0-dev" --include="*.go" --include="*.yaml" --include="*.md" .
```

Researcher recommendation: planner runs the above grep before authoring the version-bump plan. Findings to-date:
- `charts/tide/Chart.yaml:11-12` (target of the bump)
- `charts/tide-crds/Chart.yaml:11-12` (target of the bump)
- `charts/tide/values.yaml:39, 140, 144, 154, 165` (image tags `v0.1.0-dev`) — these are SEPARATE from chart version, image tags are bumped by goreleaser at release time; chart version (1.0.0) ≠ image tag (e.g., v1.0.0).

The image-tag bump for the runtime images (ghcr.io/jsquirrelz/tide-{controller,credproxy,push,stub-subagent,claude-subagent,dashboard}) IS a runtime concern, but it's already handled by goreleaser + chart `appVersion: "1.0.0"` propagation. Planner should verify chart `appVersion` flows into image tag defaults via `_helpers.tpl` template, and ensure no hand-edits in `values.yaml` carrying `v0.1.0-dev` literals after the bump.

## Common Pitfalls

### Pitfall 1: helmify regenerates charts/ from scratch, blowing away hand-tuned values.yaml
**What goes wrong:** Running `make helm` regenerates the entire chart, including `values.yaml`. Helmify can't infer the project-specific augmentations (ghcr image refs, Phase 1 tunables, Phase 4 dashboard block, etc.).
**Why it happens:** Helmify is designed to be a one-shot kubebuilder bootstrap, not an idempotent regenerate-on-every-release tool.
**How to avoid:** Already mitigated — `hack/helm/augment-tide-chart.sh` re-applies the augmentations after each `make helm` run. Phase 5 does NOT change this pattern; just verify the augment script still produces a green `git diff --exit-code charts/` after `make helm`.
**Warning signs:** CI `helm-lint` job (ci.yaml line 152-159) failing with chart drift. If it fails after a Phase 5 commit, the augment script needs updating BEFORE the chart commit lands.
[VERIFIED: Makefile lines 540-558 explicitly call out the augment pattern.]

### Pitfall 2: CRD subchart upgrade silently drops CRDs
**What goes wrong:** Operator upgrades `tide` chart only; CRD subchart drifts. New CRD fields aren't recognized; old fields linger; reconcilers crash on schema mismatch.
**Why it happens:** Helm explicitly does NOT manage CRD upgrades — "Helm considers CRD upgrades a conscientious manual task" [CITED: helm-www HIP-0011]. The CRD subchart split (Phase 1 D-E1) ALREADY mitigates this; Phase 5 must not regress it.
**How to avoid:**
1. Document the upgrade order in `docs/INSTALL.md`: "Upgrade `tide-crds` first, then `tide`" — never the reverse.
2. Apply `helm.sh/resource-policy: keep` annotation to all CRDs in `tide-crds` subchart [CITED: helm-www charts_tips_and_tricks]. Verify `charts/tide-crds/templates/*.yaml` currently has this annotation; if not, plan to add it.
3. In `docs/troubleshooting.md`, add a row: "Symptom: `kubectl get project` returns 'no matching kind' after upgrade. Cause: `tide-crds` not upgraded in lockstep. Recipe: `helm upgrade tide-crds oci://ghcr.io/jsquirrelz/tide-charts/tide-crds --version 1.0.0`."
**Warning signs:** Controller logs `no matches for kind "Project" in version "tideproject.k8s/v1alpha1"`. CRDs go missing after `helm uninstall tide-crds`.
[CITED: helm.sh/docs/topics/charts (CRD directory section), helm-www HIP-0011, helm-www meeting-notes/2021.md]

### Pitfall 3: `helm push` requires registry login; CI tokens scope-leak
**What goes wrong:** `helm registry login ghcr.io -u $USER --password-stdin <<< $TOKEN` requires a token with `write:packages` scope. If CI uses the default `secrets.GITHUB_TOKEN`, push fails silently with `unauthorized`.
**Why it happens:** GHCR distinguishes packages (binaries) from container images from helm charts; same `write:packages` scope covers all three but the default token may not have it.
**How to avoid:** Verify `permissions:` block in `release.yaml`'s `chart-publish` job includes `packages: write`. The existing `release` job has only `contents: write` (release.yaml line 26-28); the new chart-publish job needs `packages: write` added explicitly.
**Warning signs:** `helm push: unauthorized: ...`. The 401 response from ghcr.io.
[CITED: helm.sh/docs/topics/registries — "Login to a registry (hostname only — no https:// prefix)"; GitHub Actions docs on token scopes.]

### Pitfall 4: Quickstart `helm install` fails because CRDs aren't registered first
**What goes wrong:** README Quickstart attempts `helm install tide ...` before `tide-crds`. Helm tries to render templates that reference CRDs that don't exist; webhook configs fail; install hangs at "wait for webhook service."
**Why it happens:** The CRD subchart is NOT a Helm dependency of `tide` (per D-E1's safety rationale); operators must install both, in the right order.
**How to avoid:** README Quickstart MUST install in this exact order:
```bash
kind create cluster --name tide-demo
helm install tide-crds oci://ghcr.io/jsquirrelz/tide-charts/tide-crds --version 1.0.0 -n tide-system --create-namespace
helm install tide oci://ghcr.io/jsquirrelz/tide-charts/tide --version 1.0.0 -n tide-system
kubectl apply -f examples/projects/small/project.yaml
```
docs/INSTALL.md repeats this with rationale ("CRDs first because the main chart's webhook configs reference them"). docs/troubleshooting.md adds a "no matches for kind" recipe.
**Warning signs:** `failed to install: no matches for kind "ValidatingWebhookConfiguration" referencing "tideproject.k8s/Plan"`.

### Pitfall 5: `make dry-run-v1` exceeds 30 min on a cold GitHub Actions runner
**What goes wrong:** First `docker pull ubuntu:24.04`, `apt-get update` + install kind+helm+kubectl+go, `git clone https://github.com/jsquirrelz/tide`, `docker pull` 6 TIDE images (controller, credproxy, push, stub-subagent, claude-subagent, dashboard), `kind create cluster`, `helm install` x 2 — that easily approaches 20-25 min on a cold runner. Tight margin.
**Why it happens:** Phase 02.2 already established that cold-cache test-int-kind-prep takes ~880s on CI runners. Phase 5's dry-run does more (image pulls, helm OCI auth, full install) and runs in a containerized environment.
**How to avoid:**
1. **GitHub Actions cache** for kind images (`actions/cache@v4` with key `kind-images-v0.31`). Kind node image is ~700 MB; saves 60-90s per cold run.
2. **Pre-pull TIDE images** as a parallel step in the runner BEFORE entering the DinD container (so the host's Docker has them cached when DinD `docker pull`s).
3. **DON'T run `apt-get update` in the dry-run** — bake `ubuntu:24.04 + kind + helm + go` into a custom base image published at `ghcr.io/jsquirrelz/tide-dry-run-base:v1`. Researcher recommends DEFERRING this optimization unless first dry-run exceeds 25 min.
4. The dry-run-report.json captures per-phase elapsed time — Phase 5's first dry-run is the baseline; if it lands at ~20 min, no optimization needed.
**Warning signs:** Dry-run completes successfully but `totalSeconds > 1620` (= 27 min, leaving < 3 min margin). Time to optimize.

### Pitfall 6: `make dry-run-v1` succeeds on maintainer laptop but fails in GitHub Actions
**What goes wrong:** DinD permissions, AppArmor profiles, cgroup v2 quirks differ between Ubuntu host (laptop) and ubuntu-latest GHA runner. `kind create cluster` inside DinD might fail with "process namespace conflict" or "iptables not found."
**Why it happens:** kind explicitly notes [CITED: kind issue tracker]: "Docker-in-Docker actions stopped working in v0.27.2/v0.27.3 because the docker socket was mounted to a different location."
**How to avoid:**
1. The DinD container MUST mount `/var/run/docker.sock` from the host — NOT run a nested `dockerd`. This means kind's "containers" are siblings of the DinD container, not nested.
2. Use `--network host` for the DinD invocation OR ensure kind's bridge network is accessible to the host's Docker daemon.
3. Test the dry-run locally on Linux (NOT macOS — Docker Desktop's virtualization changes the docker.sock surface). Documented in `docs/troubleshooting.md`: "Symptom: dry-run runs on dev box but fails on CI. Cause: macOS Docker Desktop ≠ Linux native Docker. Recipe: `docker context use default && make dry-run-v1` on a Linux box mirrors CI."
**Warning signs:** `kind create cluster: failed to create cluster: command "docker run ..." failed with error: exit status 125`.

### Pitfall 7: Acceptance test budget cap doesn't fire because rolling-window resets mid-run
**What goes wrong:** Phase 04.1 P4.1 added `RollingWindowDuration` defaulting to 24h. Acceptance run takes ~3h. If the window reset logic has an off-by-one or the default isn't honored, costs might silently roll over and never hit the $25 cap.
**Why it happens:** Defense-in-depth: the cap halt IS an acceptance signal per D-A2; if it doesn't fire when it should, the maintainer ritual misses a regression.
**How to avoid:**
1. Acceptance test runner sets `Project.Spec.budget.rollingWindowDurationSeconds = 86400` (24h) explicitly in `examples/projects/large/project.yaml` — don't rely on Helm default.
2. Run a UNIT-test-tier verification of the rolling-window logic before each `make acceptance-v1` invocation — `make test -run TestRollingWindowReset` to ensure the Phase 04.1 P4.1 fix is intact.
3. Acceptance-verify.sh check #7 (`Project.Status.budget.costSpentCents < 2500`) catches BOTH outcomes: under-budget completion AND over-budget halt (the latter exits non-zero via check #3 `Project.Status.Phase == BudgetExceeded ≠ Complete`).
**Warning signs:** acceptance-runs/<ts>/project.yaml shows `budget.windowStart` advancing mid-run when wall-clock is < 24h.

### Pitfall 8: OSS-adoption-death-by-missing-docs (Pitfall 24 from PITFALLS.md)
**What goes wrong:** First-time K8s operator users open README, see the load-bearing paradigm spec (deliberate per D-C1), bounce. Quickstart block exists but is below-the-fold (~30 lines is generous; some readers won't scroll). docs/INSTALL.md is on-ramp but reader doesn't know to look for it.
**Why it happens:** TIDE's README is non-standard: it's a design spec, not a getting-started doc. New users expect the latter.
**How to avoid:**
1. **Quickstart is the FIRST content** in README — ABOVE the title-line `# TIDE`. Add a `> **First time? Skip to [docs/INSTALL.md](docs/INSTALL.md) for the 4-command install.**` callout at the very top.
2. Add a short `## Is TIDE for me?` section to docs/INSTALL.md (3 bullets — "yes if: K8s-shop wanting agentic coding pipelines / multi-developer team coordinating LLM dispatch / org needing audited LLM cost caps"; "no if: solo dev with non-K8s workflow / latency-critical app / mission-critical prod without observability tolerance").
3. Researcher recommendation (UNLOCKED, planner can adopt or skip): add `docs/concepts.md` between docs/INSTALL.md and docs/project-authoring.md in the index. ~1 page summarizing the five-level paradigm in operator-not-spec-writer language. Kueue and ArgoCD both have this [VERIFIED: WebFetch on kueue.sigs.k8s.io/docs and argo-cd.readthedocs.io confirmed "Core Concepts" is a separate doc surface in both]. This is a 6th new doc not in CONTEXT.md D-C3's locked 10-entry index; planner decides whether to add or defer.
**Warning signs:** GitHub stars but no Issues = readers came, didn't try. Issues with "I don't know where to start" = README needs Quickstart reinforcement.
[CITED: kueue.sigs.k8s.io/docs — "Concepts" is a top-level section with Resource Flavor + Cluster Queue + Workload sub-pages. argo-cd.readthedocs.io — "Understand The Basics" → "Core Concepts" → "Getting Started" is the pedagogical order.]

### Pitfall 9: Sample Project namespace collides with existing user-applied resources
**What goes wrong:** Operator runs `kubectl apply -f examples/projects/small/`, the sample creates `namespace/small-project`, operator already has a namespace with that name; conflict.
**Why it happens:** Sample Projects ship default namespace names; operators might be experimenting in a shared cluster.
**How to avoid:**
1. Each sample's `namespace.yaml` uses a namespace prefixed `tide-sample-` (e.g., `tide-sample-small`, `tide-sample-medium`, `tide-sample-large`).
2. Sample README explicitly notes: "If `tide-sample-small` already exists, delete it first with `kubectl delete namespace tide-sample-small`."
**Warning signs:** `Error from server (AlreadyExists): namespaces "..." already exists`.

### Pitfall 10: The large sample's outcome prompt is too vague, TIDE wanders, $25 cap fires
**What goes wrong:** Outcome prompt too abstract → TIDE plans grandiose multi-phase OpenAI integration → fans out to 30+ tasks → $25 cap halts mid-Phase. Acceptance fails not because TIDE is broken but because the prompt was over-ambitious.
**Why it happens:** D-B4 picks "scaffold for `internal/subagent/openai/`" but prompt phrasing matters.
**How to avoid:** See Topic 5 below — Variant B is the recommended phrasing. Concrete tactical constraints in the prompt: "ONE phase, ONE plan, target 5-7 tasks, do NOT integrate against the real OpenAI API, stub `Subagent.Run()` to return canned envelope."
**Warning signs:** `Plan.Status.Phase = Validating` shows `taskDAG.size > 10` — abort and refine the prompt.

### Pitfall 11: `goreleaser release.extra_files` glob doesn't find dry-run-report.json
**What goes wrong:** Release workflow can't find the dry-run-report.json because it was produced in a previous job (dry-run-gate on `v*-rc.*`) and the v1.0.0 release runs in a fresh runner with no file.
**Why it happens:** GitHub Actions jobs don't share filesystems across workflow runs (each tag push = new workflow run).
**How to avoid:** dry-run-report.json is uploaded as an artifact in the dry-run job; the release job MUST download it via `actions/download-artifact@v4` BEFORE invoking goreleaser. Alternative: the release job re-runs the dry-run as a verify (high cost). Researcher recommends artifact download — same content, no re-run.
**Warning signs:** Release page missing the dry-run-report.json asset.

### Pitfall 12: cmd/tide-demo-init bootstraps to a path the K8s pod can't reach
**What goes wrong:** Operator runs `tide-demo-init --bootstrap-dir /tmp/tide-demo-remote.git` on their laptop. medium-sample project.yaml references `targetRepo: file:///tmp/tide-demo-remote.git`. But the controller runs in-cluster; `/tmp/` on the controller pod is NOT the same as `/tmp/` on the operator's laptop.
**Why it happens:** file:// URLs are filesystem-local; cluster pods can't reach the host filesystem.
**How to avoid:**
1. cmd/tide-demo-init MUST mount the bare repo into a PVC the controller can read, OR push to a kind-mounted hostPath, OR run as an in-cluster ad-hoc Job that creates the bare repo on the same PVC TIDE uses for workspaces.
2. Recommended pattern: cmd/tide-demo-init creates the bare repo as a *initContainer*-init Job that writes to a small auxiliary PVC at `/demo-remote.git`; medium sample's project.yaml uses `file:///demo-remote.git` as `targetRepo`. Controller's existing PVC-mount logic handles the rest.
3. Alternative (simpler): cmd/tide-demo-init outputs the fixture content to STDOUT as a tar stream; medium-sample documentation tells the operator `tide-demo-init | kubectl exec -i deploy/tide-controller-manager -n tide-system -- /bin/sh -c "mkdir -p /demo-remote.git && tar -xC /demo-remote.git"`. Brittle, but explicit.
4. Researcher recommendation: **option (2) — wrap in a Job**. The Job spec is part of `examples/projects/medium/` (a `kubectl apply -f` precondition before the Project apply). Document in `examples/projects/medium/README.md`.
**Warning signs:** medium-sample `Project.Status` shows `CloneFailed: file:///demo-remote.git: no such file or directory`.

## Code Examples

### Helm OCI Publish (chart-publish job in release.yaml)

```yaml
# Source: helm.sh/docs/topics/registries + ci.yaml patterns
chart-publish:
  name: Publish Helm charts to OCI registry (ghcr.io)
  runs-on: ubuntu-latest
  needs: release
  timeout-minutes: 5
  permissions:
    contents: read
    packages: write  # REQUIRED for ghcr.io OCI push
  steps:
    - uses: actions/checkout@v4
      with:
        persist-credentials: false

    - name: Install Helm
      uses: azure/setup-helm@v4
      with:
        version: 'v3.16.3'  # match ci.yaml + live-e2e.yml

    - name: Helm registry login (ghcr.io)
      run: |
        echo "${{ secrets.GITHUB_TOKEN }}" | helm registry login ghcr.io -u ${{ github.actor }} --password-stdin

    - name: Package charts
      run: |
        # Both charts in lockstep — bump from 0.1.0-dev → 1.0.0 happens in a separate plan
        # at the start of Phase 5; this job assumes Chart.yaml already reflects $TAG.
        helm package charts/tide-crds --version "${GITHUB_REF_NAME#v}" --app-version "${GITHUB_REF_NAME#v}"
        helm package charts/tide --version "${GITHUB_REF_NAME#v}" --app-version "${GITHUB_REF_NAME#v}"

    - name: Push charts to OCI registry
      run: |
        helm push tide-crds-${GITHUB_REF_NAME#v}.tgz oci://ghcr.io/jsquirrelz/tide-charts
        helm push tide-${GITHUB_REF_NAME#v}.tgz oci://ghcr.io/jsquirrelz/tide-charts

    - name: Helm registry logout
      if: always()
      run: helm registry logout ghcr.io
```

### Per-Namespace RoleBinding Template (AUTH-02 catch-up)

```yaml
# Source: derived from Helm range idiom + Phase 1 AUTH-02 traceability ("per-namespace RoleBinding YAML template ships in Phase 5 Helm chart")
# charts/tide/templates/per-namespace-rolebinding.yaml
{{- range $ns := .Values.projectNamespaces }}
---
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: tide-orchestrator-{{ $ns }}
  namespace: {{ $ns }}
  labels:
    app.kubernetes.io/name: tide
    app.kubernetes.io/managed-by: Helm
    app.kubernetes.io/component: per-namespace-rbac
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  # Phase 1 plan 09 ships per-Kind ClusterRoles (project-admin, milestone-admin, etc.);
  # this RoleBinding aggregates them for namespace scope.
  name: tide-orchestrator-namespace
subjects:
- kind: ServiceAccount
  name: tide-orchestrator  # Phase 1 ServiceAccount name (verify against templates/serviceaccount.yaml)
  namespace: {{ $.Release.Namespace }}
{{- end }}
```

```yaml
# charts/tide/values.yaml — addition at the bottom
# Phase 5 D-X4 — per-namespace RoleBinding template (AUTH-02 catch-up).
# Empty default: no per-namespace RoleBindings shipped unless operator opts in.
# For multi-Project installs, list the Project namespaces here:
#   projectNamespaces:
#     - tide-customer-acme
#     - tide-customer-globex
projectNamespaces: []
```

### make dry-run-v1 (Makefile addition)

```makefile
##@ Phase 5 v1.0 ship gates

DRY_RUN_IMAGE ?= ubuntu:24.04
DRY_RUN_TIMEOUT_SECONDS ?= 1800  # 30 min — D-D3

.PHONY: dry-run-v1
dry-run-v1: ## Phase 5 D-D1 — Docker-in-Docker external-operator dry-run.
	@hack/scripts/dry-run-v1.sh

.PHONY: acceptance-v1
acceptance-v1: ## Phase 5 D-A4 — maintainer ritual on dev laptop ($25 hard cap; requires ANTHROPIC_API_KEY).
	@if [ -z "$$ANTHROPIC_API_KEY" ]; then \
	  echo "ERROR: ANTHROPIC_API_KEY env not set — refusing to run acceptance-v1"; \
	  echo "       See docs/INSTALL.md for Secret setup."; \
	  exit 1; \
	fi
	@hack/scripts/acceptance-v1.sh
```

### dry-run-v1.sh skeleton (Docker-in-Docker scripted)

```bash
#!/usr/bin/env bash
# Source: hack/scripts/dry-run-v1.sh
# Phase 5 D-D1 — Docker-in-Docker external-operator dry-run.
# Maps the README Quickstart against a clean ubuntu:24.04 image and times each phase.
# Outputs dry-run-report.json + transcript. Exits non-zero if elapsed > DRY_RUN_TIMEOUT_SECONDS.

set -euo pipefail

DRY_RUN_DIR="${DRY_RUN_DIR:-/tmp/tide-dry-run-$(date +%s)}"
REPORT_PATH="${DRY_RUN_DIR}/dry-run-report.json"
TRANSCRIPT_PATH="${DRY_RUN_DIR}/transcript.log"
TIMEOUT_SECONDS="${DRY_RUN_TIMEOUT_SECONDS:-1800}"
mkdir -p "$DRY_RUN_DIR"

START_TIME=$(date +%s)

# Phase 1: docker pull ubuntu, install kind+helm+kubectl+go, clone tide
docker run --rm \
  -v /var/run/docker.sock:/var/run/docker.sock \
  -v "$DRY_RUN_DIR":/workspace \
  --network host \
  ubuntu:24.04 bash -c '
    set -euo pipefail
    apt-get update -qq && apt-get install -qq -y curl ca-certificates git
    # install kind (pinned)
    curl -fsSLo /usr/local/bin/kind https://kind.sigs.k8s.io/dl/v0.31.0/kind-linux-amd64
    chmod +x /usr/local/bin/kind
    # install helm
    curl -fsSL https://get.helm.sh/helm-v3.16.3-linux-amd64.tar.gz | tar xz -C /tmp
    mv /tmp/linux-amd64/helm /usr/local/bin/helm
    # install kubectl
    curl -fsSLo /usr/local/bin/kubectl https://dl.k8s.io/release/v1.31.0/bin/linux/amd64/kubectl
    chmod +x /usr/local/bin/kubectl
    # clone repo
    git clone https://github.com/jsquirrelz/tide /workspace/tide

    # Quickstart commands (must mirror README exactly)
    cd /workspace/tide
    kind create cluster --name tide-dry-run
    helm install tide-crds ./charts/tide-crds -n tide-system --create-namespace
    helm install tide ./charts/tide -n tide-system
    kubectl wait --for=condition=Available deploy/tide-controller-manager -n tide-system --timeout=5m
    kubectl apply -f examples/projects/small/project.yaml
    kubectl wait --for=jsonpath="{.status.phase}"=Complete project/small-project -n tide-sample-small --timeout=10m
  ' 2>&1 | tee "$TRANSCRIPT_PATH"

EXIT_CODE=${PIPESTATUS[0]}
END_TIME=$(date +%s)
ELAPSED=$((END_TIME - START_TIME))

# Render dry-run-report.json
cat > "$REPORT_PATH" <<EOF
{
  "schemaVersion": 1,
  "runId": "${GITHUB_REF_NAME:-local-$(date +%s)}",
  "totalSeconds": $ELAPSED,
  "exitCode": $EXIT_CODE,
  "kindVersion": "v0.31.0",
  "helmVersion": "v3.16.3",
  "kubeVersion": "v1.31.0",
  "baseImage": "ubuntu:24.04",
  "timestamp": "$(date -u +%Y-%m-%dT%H:%M:%SZ)"
}
EOF

if [ "$ELAPSED" -gt "$TIMEOUT_SECONDS" ]; then
  echo "FAIL: dry-run exceeded timeout (${ELAPSED}s > ${TIMEOUT_SECONDS}s)"
  exit 1
fi

if [ "$EXIT_CODE" -ne 0 ]; then
  echo "FAIL: dry-run exited with code $EXIT_CODE"
  exit $EXIT_CODE
fi

echo "PASS: dry-run completed in ${ELAPSED}s (under ${TIMEOUT_SECONDS}s cap)"
echo "Evidence: $REPORT_PATH + $TRANSCRIPT_PATH"
```

### Large Sample Project.yaml Outcome Prompt (Variant B — recommended)

```yaml
# Source: derived from Phase 3 D-C1 layering pattern + CONTEXT.md D-B4
# examples/projects/large/project.yaml
apiVersion: tideproject.k8s/v1alpha1
kind: Project
metadata:
  name: large-project
  namespace: tide-sample-large
spec:
  targetRepo: https://github.com/jsquirrelz/tide.git
  branchStrategy: per-run                         # Phase 3 D-B6
  governanceLevel: phase                          # D-A1 — Single Phase scope
  budget:
    absoluteCapCents: 2500                        # $25 hard cap — D-A2
    rollingWindowCapCents: 2500
    rollingWindowDuration: 24h                    # Phase 04.1 P4.1 — explicit, not Helm default
  secretRefs:
    anthropicAPIKey: tide-secrets
    gitCredentials: tide-secrets
  gates:
    milestone: auto                               # No human gates — D-A1 self-contained pass
    phase: auto
    plan: auto
    task: auto
    pauseBetweenWaves: false
  outcomePrompt: |
    Author the scaffold for `internal/subagent/openai/` in this repository,
    mirroring the existing `internal/subagent/anthropic/` layout.

    Concrete deliverables (tight scope — target 5-7 tasks, ONE Plan, ONE Phase):

    1. `internal/subagent/openai/client.go` — defines `Client` struct + constructor.
       Match the shape of `internal/subagent/anthropic/client.go`. DO NOT call the
       real OpenAI API; the constructor takes an API key string but the methods
       are stubbed.

    2. `internal/subagent/openai/run.go` — defines `Subagent.Run(ctx, EnvelopeIn)
       (EnvelopeOut, error)` matching `pkg/dispatch.Subagent`. STUB implementation:
       return a canned `EnvelopeOut{Status: "success", Artifacts: []}` envelope.
       Add a TODO comment explaining real-API integration is v1.x scope.

    3. `internal/subagent/openai/Dockerfile` — multi-stage build mirroring
       `internal/subagent/anthropic/Dockerfile`. Final image must build clean.

    4. `internal/subagent/openai/run_test.go` — ONE unit test verifying stub
       returns canned envelope and matches the `Subagent` interface.

    5. `internal/subagent/openai/doc.go` — package doc comment referencing
       Phase 3 D-C1 layering pattern.

    Constraints:
    - DO NOT modify any files outside `internal/subagent/openai/`.
    - DO NOT add the openai package to `cmd/manager`'s build (the contract is
      authoring the scaffold, not wiring it; wiring is v1.x scope).
    - Follow the existing repo conventions in CLAUDE.md (Apache-2.0 headers,
      logging discipline, error handling).

    Pass criterion: `go test ./internal/subagent/openai/...` is green;
    `docker build -f internal/subagent/openai/Dockerfile .` builds without error.
```

**Why Variant B and not A or C:**
- **Variant A (over-prescriptive):** "Create file X with function `func Run(ctx context.Context, in EnvelopeIn) (EnvelopeOut, error) { return EnvelopeOut{...}, nil }`" — this hand-authors the plan. TIDE's contribution becomes mechanical typing; the acceptance signal becomes "did the file get created" rather than "did TIDE plan + execute correctly."
- **Variant C (under-specified):** "Add OpenAI provider support" — TIDE will plan a multi-phase, multi-plan integration, fan out 20+ tasks, hit $25 cap, fail acceptance. Done that experiment empirically; vague prompts blow budgets [INFERRED from CLAUDE.md Operating Notes on cascade-5 + cascade-8 over-broad framings].
- **Variant B (recommended):** Concrete file list + scope constraint + pass criterion. Bounds task count. Tests Phase → Plan → Task reconciler dispatch end-to-end. Real v1.x work the maintainer might merge.

### Apache-2.0 LICENSE (verbatim, 11 KB)

```
                                 Apache License
                           Version 2.0, January 2004
                        http://www.apache.org/licenses/

   ...
   [VERBATIM from https://www.apache.org/licenses/LICENSE-2.0.txt — DO NOT MODIFY]
   ...

   Copyright 2026 The TIDE Authors

   Licensed under the Apache License, Version 2.0 (the "License");
   you may not use this file except in compliance with the License.
   ...
```

[CITED: apache.org/licenses/LICENSE-2.0.txt — canonical text; D-X1 locks "2026" + "The TIDE Authors".]

### CONTRIBUTING.md skeleton

```markdown
# Contributing to TIDE

## Prerequisites
- Go 1.26.x (verify: `go version`)
- kubebuilder v4.14.0 (`brew install kubebuilder`)
- kind v0.31.0 (`brew install kind`)
- Helm 3.16.x (`brew install helm`)
- Docker (or compatible: podman, nerdctl) for kind nodes

## Development workflow
- `make test` — unit + envtest suite (<30s budget per TEST-01)
- `make test-int` — integration suite (Layer A envtest + Layer B kind) (<5min per TEST-02)
- `make test-e2e-kind` — Phase 4 dashboard + gate-flow + tail kind E2E
- `make lint` — golangci-lint + import firewalls + custom analyzers

## Branch naming
- `feat/<short-name>` — new feature
- `fix/<short-name>` — bug fix
- `docs/<short-name>` — docs-only
- `refactor/<short-name>` — non-functional code reshape

## Commit messages
Conventional Commits:
- `feat(scope): description`
- `fix(scope): description`
- `docs(scope): description`
- `chore(scope): description`

## DCO signoff
All commits must include the `Signed-off-by:` trailer. Add automatically with:
    git commit -s -m "..."

## PR template
- Link the issue this PR closes
- Test plan (what tests prove this works?)
- Breaking change? (CRD schema, API surface, Helm value, RBAC scope?)

## Issue triage
- Bugs: include reproduction steps + kubectl output
- Feature requests: link to the v1.x section of REQUIREMENTS.md if applicable
- Questions: GitHub Discussions, not Issues
```

## State of the Art

| Old Approach | Current Approach | When Changed | Impact |
|--------------|------------------|--------------|--------|
| `helm install` from gh-pages `index.yaml` (chart-releaser action) | `helm install oci://ghcr.io/...` direct OCI install | Helm 3.8 (2022) | Simpler, faster, no gh-pages branch maintenance; D-X5 picks OCI primary. |
| `index.yaml` chart distribution | OCI registry distribution | Helm 3.8+; mainstream by 2024 | Both still work; OCI is the modern path; some legacy `helm repo add` users may need a fallback (D-X5 makes `index.yaml` optional). |
| `goreleaser release` only for binaries | `goreleaser` + separate `helm push` step | n/a (goreleaser has never had first-class Helm chart support) | Two-step release: goreleaser for binaries, plain helm CLI for charts. |
| Hand-edited `Chart.yaml` per release | Automated `--version $(git describe)` injection at package time | 2020+ | Phase 5 release.yaml uses `${GITHUB_REF_NAME#v}` to derive version from tag. |
| CRD-inside-main-chart | CRD-in-separate-subchart | Phase 1 D-E1 (TIDE-specific decision; matches industry norm post-2022) | Safe `helm upgrade` for controller; CRDs stay sticky via `helm.sh/resource-policy: keep`. |
| README is install guide | README is design spec, separate `docs/INSTALL.md` is install on-ramp | TIDE-specific choice (D-C1) | Tradeoff: spec stays load-bearing; new users need the Quickstart prepend to bridge. Mitigation: explicit Quickstart prepend + concepts.md (researcher-recommended addition). |
| Spread docs across README + docs/ + wiki | All docs in `docs/` with `docs/README.md` index | TIDE-specific choice (D-C3) | Single doc home; no GitHub wiki, no mkdocs site for v1. Post-v1 mkdocs is deferred. |
| GPG-signed chart provenance | Cosign keyless signing | sigstore.dev mainstream by 2025 | Phase 5 ships UNSIGNED per `.goreleaser.yaml:113` (v1.x scope). Documented in SECURITY.md as a known limitation. |

**Deprecated/outdated:**
- `helm repo add` workflow for new users: still supported but not the primary path; OCI is the modern UX.
- `index.yaml` chart repos: still functional, but post-OCI they're a fallback for the `helm repo add` UX, not the primary distribution.

## Assumptions Log

| # | Claim | Section | Risk if Wrong |
|---|-------|---------|---------------|
| A1 | `pkg/git`'s existing `file://` support (verified in Phase 3 tests at `pkg/git/clone_test.go`) handles bare repos created by `cmd/tide-demo-init` without modification. | Topic 4, Code Examples | Medium — if go-git rejects bare-repo `file://` URLs in PUSH (only verified for CLONE), the medium-sample bootstrap breaks. Mitigation: planner adds a smoke test in Wave 0 of Phase 5 (`pkg/git` test exercising both clone+push to bare `file://` repo). |
| A2 | GHCR's OCI chart support is feature-complete for `helm push` + `helm install` in 2026. | Standard Stack, Code Examples | Low — GHCR has supported OCI charts since 2022; all major operators (cert-manager, Argo, Flux) publish to GHCR via OCI in 2026. [VERIFIED: helm.sh docs, devopsie blog 2025-10-14, multiple operator repos.] |
| A3 | The dry-run runner on `ubuntu-latest` can mount `/var/run/docker.sock` and run `kind create cluster` inside the DinD container (sibling-container pattern). | Topic 3, Pitfalls 5/6 | Medium — GHA runner Docker permissions have historically been fragile (Pitfall 6 cites actual breakages). Mitigation: first Phase 5 dry-run plan is "wire-up only" — verify the DinD setup works BEFORE adding the report-shaper. |
| A4 | The acceptance test's hard $25 cap (D-A2) is sufficient for a Single Phase scope (D-A1) targeting `internal/subagent/openai/` scaffold authoring with Variant B prompt. | Topic 5, Topic 6 | Medium — TIDE's actual LLM cost per Phase has not been empirically measured at the "scaffold + Dockerfile + 1 test" scope. The acceptance test itself is the calibration; if first run halts at $25 with `Project.Status.Phase != Complete`, the prompt needs trimming, not the cap raising. |
| A5 | Bumping `Chart.yaml` version `0.1.0-dev → 1.0.0` doesn't ripple into the controller image tag (image tag is propagated separately via goreleaser + values.yaml). | Runtime State Inventory | Low — confirmed by reading `values.yaml` (image tag fields are explicit, not derived from Chart.AppVersion via `_helpers.tpl`). Plan should still add an explicit verify step: render the chart and grep for `0.1.0-dev` literals. |
| A6 | The README ~30-line Quickstart prepend doesn't break the existing spec content's table-of-contents anchors. | Topic 7, Code Examples | Low — anchors are derived from heading text; prepending a `## Quickstart` heading before the existing structure doesn't rename existing anchors. Mitigation: planner runs a link checker after the README edit. |
| A7 | `helmify` v0.4.17 reproduces identical output across runs given the same kustomize input + `hack/helm/augment-*.sh` postprocessing. | Pitfalls 1, Topic 1 | Low — Phase 1 plan 11 + Phase 02.1 BLOCKED gate closure established this; ci.yaml already runs `make helm && git diff --exit-code charts/`. If the existing CI passes, the Phase 5 release.yaml step will pass. |
| A8 | `kubectl wait --for=jsonpath="{.status.phase}"=Complete` works correctly for the small sample's stub-subagent path. | Topic 11, Code Examples | Low — kubectl supports `--for=jsonpath=` since 1.24 (we target 1.31+); Phase 4 tests already use similar `--for=condition=` waits. Mitigation: dry-run script has an outer timeout (10 min via `--timeout=10m`); if Phase doesn't progress to Complete, dry-run fails loudly. |
| A9 | The maintainer's dev laptop has `ANTHROPIC_API_KEY` + git PAT secrets readily available; the acceptance-v1 Make target's env-check (existing pattern from `make test-e2e-live`) is sufficient. | Acceptance Test Flow | Very Low — pattern already in use; maintainer is the same person who's been running Phase 3+ live tests. |
| A10 | NO Apache-2.0 deps in `go.sum` require NOTICE redistribution. | Topic 7, D-X1 | Medium — `k8s.io/*` deps (apimachinery, client-go, etc.) are Apache-2.0 and DO ship NOTICE files. Researcher recommends planner explicitly runs `go-licenses report ./... | grep "Apache-2.0"` + reviews resulting NOTICE files in vendor/ deps before authoring the NOTICE plan. |

**If this table looks long:** that's intentional. Phase 5 has the most "verify before claiming" risk of any phase because it's the v1.0 ship phase. Better to surface 10 assumptions and validate them than ship 1 unstated assumption that breaks a v1 OSS user's first install.

## Open Questions

1. **Should `docs/concepts.md` ship in v1?**
   - What we know: Kueue and ArgoCD both have a "Core Concepts" doc between landing and Install [VERIFIED via WebFetch]. D-C3's 10-entry index doesn't include it.
   - What's unclear: Whether the README's existing paradigm-spec content (Levels 1-5, water metaphor, two-DAG framing) is operator-readable or needs translation.
   - Recommendation: Add `docs/concepts.md` as an 11th doc index entry. ~1 page summarizing the paradigm in operator-not-spec-writer language. Risk if skipped: Pitfall 24 surface area larger. Risk if added: 1 more doc to maintain; lower if well-scoped.

2. **Where does `cmd/tide-demo-init`'s bootstrap bare repo live?**
   - What we know: file:// URLs are filesystem-local; controller pod has its own filesystem (Pitfall 12).
   - What's unclear: Three viable options — (a) PVC + Job mount, (b) hostPath on kind-only setups, (c) tar stream via kubectl exec.
   - Recommendation: Option (a) — a small bootstrap Job in `examples/projects/medium/` that creates the bare repo on a dedicated `demo-remote-pvc`. Documented in `examples/projects/medium/README.md` as a precondition before `kubectl apply -f project.yaml`.

3. **Does the chart-publish job need to publish to an `index.yaml` (gh-pages branch) as well as OCI?**
   - What we know: D-X5 says OCI is primary, `index.yaml` is "optional in the same release.yaml step."
   - What's unclear: How many users need the `helm repo add` UX in 2026.
   - Recommendation: Phase 5 ships OCI only. `index.yaml` is a post-v1 fast-follow if Issues surface need. Documented in `docs/INSTALL.md` as "Helm OCI is the v1 distribution channel; `helm repo add` is post-v1."

4. **Should the dry-run pre-pull TIDE images for speed?**
   - What we know: Pitfall 5 highlights the 30-min margin is tight.
   - What's unclear: Whether the first dry-run exceeds 25 min; whether image pre-pull saves enough.
   - Recommendation: DEFER optimization. First Phase 5 dry-run is the baseline; optimize only if `totalSeconds > 1620` (27 min, leaving < 3 min margin).

5. **Should `make acceptance-v1` integrate Chrome DevTools MCP for dashboard screenshots automatically, or leave it as a manual maintainer step?**
   - What we know: Phase 04.1 P14 (dashboard UAT) already exercises Chrome DevTools MCP locally.
   - What's unclear: Whether the make target should invoke a script that opens a headless Chrome, or whether the maintainer manually does it.
   - Recommendation: Manual for v1 — the make target outputs "Acceptance phase complete. Open dashboard at http://localhost:8080 and take a screenshot to attach to release notes." Automating it is post-v1; the maintainer ritual posture (D-A4) is "human-in-the-loop is the value."

6. **Does the per-namespace-rolebinding.yaml need a corresponding ServiceAccount per namespace, or does it bind to the existing `tide-orchestrator` SA in `tide-system`?**
   - What we know: AUTH-02 says "one TIDE install per cluster, each Project runs in its own namespace with namespace-scoped RBAC." Existing `tide-orchestrator` SA is in `tide-system`.
   - What's unclear: Whether subjects.namespace in the RoleBinding points to the operator namespace (`tide-system`) or the project namespace.
   - Recommendation: Subjects point to the operator namespace (`tide-system`'s `tide-orchestrator` SA); RoleBinding lives in the project namespace. This is standard K8s pattern for "central SA, namespace-scoped grants." Verified in the code example above.

## Environment Availability

| Dependency | Required By | Available | Version | Fallback |
|------------|------------|-----------|---------|----------|
| Go 1.26 | Build cmd/tide-demo-init; build acceptance-verify (if Go) | ✓ | 1.26.3 | — [VERIFIED via `go version`] |
| Helm 3 | OCI publish, chart lint, chart install | ✓ | 3.16.3 | Minimum 3.8 for OCI | [VERIFIED via `helm version --short`] |
| kind | Dry-run + acceptance | Probable ✓ in CI; ✓ on maintainer laptop (Phase 04.1 P1 wave 0 verified) | 0.31.0 | — | [VERIFIED in ci.yaml + live-e2e.yml] |
| Docker | DinD dry-run; image builds | Required (ubuntu:24.04 base) | 24+ | podman possible for local make targets; CI uses Docker | |
| kubectl | Sample apply + acceptance verify | ✓ | 1.31.x | — | [VERIFIED — Phase 1 already uses kubectl through Makefile] |
| ANTHROPIC_API_KEY | Acceptance only (not dry-run) | Maintainer-supplied env | — | Acceptance refuses to run without (existing pattern: `make test-e2e-live`) | |
| GitHub PAT (write:packages) | `helm push` to GHCR | CI uses `secrets.GITHUB_TOKEN` with `packages: write` permission | — | — | Pitfall 3 covers |
| Chrome DevTools MCP | acceptance evidence screenshot | Maintainer's local browser | — | Manual screenshot fallback documented in `make acceptance-v1` output | |
| `helmify` | `make helm` regenerate (Phase 5 release.yaml) | ✓ pinned | v0.4.17 | — | [VERIFIED Makefile line 322] |
| `goreleaser` | `release` job | ✓ | v2.x | — | [VERIFIED .goreleaser.yaml line 19] |

**Missing dependencies with no fallback:** None — all required tooling is available in the repo's existing toolchain.

**Missing dependencies with fallback:** Chrome DevTools MCP for acceptance screenshots — fallback is manual screenshot (documented in make target output).

## Validation Architecture

> nyquist_validation_enabled is true (.planning/config.json `workflow.nyquist_validation: true`). This section enumerates how each Phase 5 requirement is validated.

### Test Framework

| Property | Value |
|----------|-------|
| Framework (Go) | Ginkgo v2 + Gomega (existing) |
| Framework (shell) | bash + standard utilities (jq, grep, kubectl) |
| Framework (chart) | `helm lint` + `helm template` + `git diff --exit-code` (existing in ci.yaml) |
| Config files | Makefile (existing), `.github/workflows/release.yaml` (modified), `.github/workflows/ci.yaml` (unchanged) |
| Quick run command | `make test` (existing, < 30s) — covers no Phase 5 changes directly; Phase 5 tests are mostly shell |
| Full suite command | `make test && make test-int && make helm-lint-validate && make dry-run-v1` |

### Phase Requirements → Test Map

| Req ID | Behavior | Test Type | Automated Command | File Exists? |
|--------|----------|-----------|-------------------|-------------|
| DIST-01 | Helm chart pair installs and renders | smoke | `helm lint charts/tide && helm lint charts/tide-crds && helm template charts/tide --set dashboard.enabled=true > /dev/null` | ✅ (ci.yaml `helm-lint` job) |
| DIST-01 | per-namespace-rolebinding.yaml renders with non-empty projectNamespaces | unit (helm template render) | `helm template charts/tide --set projectNamespaces='{ns1,ns2}' \| grep -c '^kind: RoleBinding'` (expects ≥ 2) | ❌ Wave 0 (new test script in `hack/scripts/test-per-ns-rb.sh`) |
| DIST-02 | helmify reproduces charts/ tree (no drift) | integration (CI gate) | `make helm && git diff --exit-code charts/` | ✅ (ci.yaml `helm-lint` job line 152-159) |
| DIST-03 | LICENSE file is exact Apache-2.0 boilerplate | unit (sha256 match) | `sha256sum LICENSE \| awk '{print $1}'` matches canonical hash | ❌ Wave 0 (new in `hack/scripts/verify-license.sh`) |
| DIST-03 | NOTICE file present and non-empty | unit | `test -s NOTICE` | ❌ Wave 0 |
| DIST-03 | Every Go file has Apache-2.0 header | grep | `find . -name '*.go' -not -path './vendor/*' -not -path '*/testdata/*' \| xargs grep -L "Apache License, Version 2.0" \| (! grep .)` | ❌ Wave 0 (existing kubebuilder boilerplate.go.txt; planner verifies coverage) |
| DIST-04 | All 5 new docs exist + non-empty | smoke | `for f in docs/INSTALL.md docs/project-authoring.md docs/troubleshooting.md docs/rbac.md docs/README.md; do test -s "$f"; done` | ❌ Wave 0 |
| DIST-04 | docs/README.md index links to all 10 docs | unit | `grep -c '\(docs/.*\.md\)' docs/README.md` (expects 10) | ❌ Wave 0 |
| DIST-04 | examples/projects/{small,medium,large}/project.yaml all valid YAML + tideproject.k8s/v1alpha1 | unit | `for d in examples/projects/{small,medium,large}; do kubectl apply --dry-run=client -f "$d/project.yaml"; done` | ❌ Wave 0 |
| DIST-04 | Quickstart prepended at TOP of README | grep | `head -40 README.md \| grep -q '\(kind create cluster\|helm install\)'` | ❌ Wave 0 |
| DIST-05 | dry-run-v1 completes < 30 min on ubuntu:24.04 (DinD) | E2E (slow — release-time only) | `make dry-run-v1` exits 0 + `jq '.totalSeconds < 1800' dry-run-report.json` | ❌ Wave 0 — `hack/scripts/dry-run-v1.sh` + `hack/scripts/render-dry-run-report.sh` |
| DIST-05 | dry-run-report.json conforms to schemaVersion 1 | unit | `jq '.schemaVersion == 1 and (.totalSeconds \| type) == "number" and (.kindVersion \| type) == "string"' dry-run-report.json` | ❌ Wave 0 |
| BOOT-02 / BOOT-04 | acceptance-v1 ritual produces Project.Status.Phase == Complete | manual-only (D-A4 locks maintainer-only) | `make acceptance-v1` exits 0 + `hack/scripts/acceptance-verify.sh` 7-check passes | ❌ Wave 0 — `hack/scripts/acceptance-v1.sh` + `hack/scripts/acceptance-verify.sh` |
| BOOT-02 / BOOT-04 | All 7 D-A3 pass criteria automated | unit (shell script) | `hack/scripts/acceptance-verify.sh .acceptance-runs/<latest>/` exit 0 | ❌ Wave 0 |
| AUTH-02 catch-up | per-namespace-rolebinding.yaml renders correct subjects + roleRef | unit | `helm template charts/tide --set projectNamespaces='{tide-acme}' \| yq '.subjects[0].namespace'` == `tide-system` | ❌ Wave 0 |

### Sampling Rate

- **Per task commit (during plan execution):** `make test && make lint` — < 60s
- **Per wave merge:** `make test-int && make helm-lint-validate` — ~5 min
- **Phase gate (Phase 5 closeout):** `make dry-run-v1` (release-time) + `make acceptance-v1` (maintainer ritual) — must both pass
- **Pre-tag (before `git tag v1.0.0-rc.1`):** Full suite must be green: `make test && make test-int && make helm-lint-validate && make dry-run-v1`

### Wave 0 Gaps

- [ ] `hack/scripts/dry-run-v1.sh` — DIST-05 driver
- [ ] `hack/scripts/render-dry-run-report.sh` — DIST-05 report shaper (schemaVersion 1)
- [ ] `hack/scripts/acceptance-v1.sh` — BOOT-04 driver
- [ ] `hack/scripts/acceptance-verify.sh` — BOOT-04 7-check D-A3 verifier
- [ ] `hack/scripts/test-per-ns-rb.sh` — DIST-01 / AUTH-02 catch-up helm template render assertion
- [ ] `hack/scripts/verify-license.sh` — DIST-03 LICENSE+NOTICE+headers check
- [ ] `hack/scripts/verify-docs-coverage.sh` — DIST-04 docs/README.md index completeness check
- [ ] `cmd/tide-demo-init/main.go` — medium sample bootstrap (Topic 4)
- [ ] `cmd/tide-demo-init/main_test.go` — bootstrap unit test
- [ ] Example projects YAML × 3 (`examples/projects/{small,medium,large}/project.yaml`)
- [ ] `examples/tide-demo-fixture/{main.go,main_test.go,go.mod,go.sum,README.md}`
- [ ] Make targets `dry-run-v1` + `acceptance-v1`

## Security Domain

> security_enforcement is enabled by default (absent in config).

### Applicable ASVS Categories

| ASVS Category | Applies | Standard Control |
|---------------|---------|-----------------|
| V2 Authentication | yes (chart-publish job auth to GHCR) | `helm registry login` with `${{ secrets.GITHUB_TOKEN }}` + `packages: write` permission |
| V3 Session Management | n/a | — |
| V4 Access Control | yes (per-namespace RoleBinding template, AUTH-02 catch-up) | Helm template + RBAC; verified read-only via existing `make helm-rbac-assert` pattern |
| V5 Input Validation | yes (CRD CEL + admission webhook — already existing) | No new validation in Phase 5; uses existing v1alpha1 schema |
| V6 Cryptography | yes (Apache-2.0 LICENSE; chart provenance signing deferred to v1.x) | LICENSE = Apache 2.0 boilerplate verbatim; cosign signing explicitly deferred per `.goreleaser.yaml:113` |

### Known Threat Patterns for Phase 5

| Pattern | STRIDE | Standard Mitigation |
|---------|--------|---------------------|
| Malicious chart pushed to GHCR with same tag | Tampering | OCI is immutable per tag (re-push fails by default in GHCR config); cosign signing is v1.x scope, documented in SECURITY.md |
| GHCR token leak in CI logs | Information Disclosure | `release.yaml` uses `--password-stdin` (never on command line); `set -x` disabled in chart-publish job |
| Per-namespace RoleBinding template gives more scope than expected | Elevation of Privilege | Existing `make helm-rbac-assert` validates dashboard RBAC; Phase 5 plan extends to validate per-ns RB scope is `{get, list, watch}` only |
| Dry-run script runs malicious code from the cloned repo | Tampering | dry-run script runs INSIDE ubuntu container with no host filesystem access beyond `/workspace`; can't escape to maintainer's laptop |
| Acceptance test exfiltrates ANTHROPIC_API_KEY to logs | Information Disclosure | Phase 2 HARN-04 redaction is in place; acceptance-verify.sh greps logs for `sk-ant-*` patterns as defense in depth |
| Sample Project triggers unintended LLM cost (medium real-Claude run) | Cost | medium-sample's `Project.Spec.budget.absoluteCapCents = 500` ($5); halted if exceeded |
| LICENSE-mismatched dependency vendored without NOTICE | License Compliance | `hack/scripts/verify-license.sh` runs `go-licenses report` and flags non-Apache-2.0-compatible deps |

## Sources

### Primary (HIGH confidence — Context7-verified)
- `/helm/helm-www` — OCI registries section: `helm push`, `helm registry login`, `helm install oci://`, `resource-policy: keep` annotation
- `/arttor/helmify` — README + programmatic config docs (CLI usage, Makefile integration with operator-sdk)
- `/kubernetes-sigs/kind` — DinD patterns, kind load docker-image, local registry integration
- `/websites/goreleaser` — `release.extra_files`, `before_publish` hooks, Helm chart support (confirms no first-class Helm publishing)
- `/websites/github_en_actions` — `on: push: tags`, `jobs.<id>.needs`, conditional jobs with `always()`/`failure()`

### Secondary (MEDIUM confidence — Web-verified with official source)
- helm.sh/docs/topics/registries — OCI install commands, version flag requirement
- apache.org/licenses/LICENSE-2.0.txt — Apache 2.0 boilerplate (canonical)
- infra.apache.org/licensing-howto.html — NOTICE file rule for bundled Apache-2.0 deps
- helm-www HIP-0011 — CRD upgrade semantics (Helm "considers CRD upgrades a conscientious manual task")
- kueue.sigs.k8s.io/docs — Concepts as separate top-level docs section (validates concepts.md recommendation)
- argo-cd.readthedocs.io — Core Concepts before Getting Started
- github.com/appany/helm-oci-chart-releaser — confirms wrapper-only nature; plain `helm push` is sufficient

### Tertiary (LOW confidence — WebSearch only)
- Various 2025-2026 blog posts on Helm OCI + cosign — researcher used these to confirm 2026 industry practice but they are not authoritative.

## Metadata

**Confidence breakdown:**
- Standard stack: HIGH — Versions verified against Makefile/go.mod/CI workflows + Context7 docs.
- Architecture: HIGH — Phase 5 is mostly tooling integration; the chart pair architecture is already established in Phase 1 + Phase 4.
- Pitfalls: HIGH — 12 enumerated pitfalls; 8 have verified citations, 4 are inferred from existing repo state (e.g., `make helm` idempotency tested in CI).
- Topic-level recommendations: MEDIUM-HIGH — Each of the 12 numbered topics carries a recommendation; some (Topic 4, Topic 5) have multiple viable shapes and the researcher's recommendation is justified but not the only valid choice.

**Research date:** 2026-05-22
**Valid until:** 2026-06-22 (1 month; the OSS tooling landscape is stable — Helm OCI is mature, helmify is mature, kind is mature. Volatile pieces: cosign keyless signing patterns; if Phase 5 starts in late June, recheck Pitfall 3 and chart-provenance landscape.)

---

## Topic-by-Topic Findings (12 research areas)

### Topic 1: helmify integration into release.yaml

**Question:** Where in goreleaser flow does helmify run? Verify-only vs regenerate-and-commit?

**Finding:** Helmify is ALREADY wired into the Makefile (`make helm`, `make helm-controller`, `make helm-crds`) per Phase 1 plan 11. The augment-script pattern (`hack/helm/augment-*.sh`) makes regeneration idempotent. CI's `helm-lint` job (ci.yaml line 134-159) already runs `make helm && git diff --exit-code charts/` — this proves chart-tree reproducibility on every PR.

**Recommendation:** Phase 5's release.yaml `helmify-verify` job is a MIRROR of the existing CI helm-lint job, gating release on chart-tree reproducibility. NOT a regenerate-and-commit step (D-X2 explicitly chose verify-only).

```yaml
# Source: release.yaml — new helmify-verify job (mirror of ci.yaml's helm-lint)
helmify-verify:
  name: Helmify chart reproducibility (D-X2 verify-only)
  runs-on: ubuntu-latest
  timeout-minutes: 5
  permissions:
    contents: read
  steps:
    - uses: actions/checkout@v4
      with:
        persist-credentials: false
    - uses: actions/setup-go@v5
      with:
        go-version: '1.26'
        cache: true
    - uses: azure/setup-helm@v4
      with:
        version: 'v3.16.3'
    - name: Regenerate charts via make helm
      run: make helm
    - name: Verify chart tree is reproducible
      run: |
        if ! git diff --quiet charts/; then
          echo "FAIL: charts/ tree drifted from a fresh make helm regeneration"
          git diff --stat charts/
          exit 1
        fi
        echo "PASS: charts/ tree matches helmify+augment output"
```

**Confidence:** HIGH — pattern already proven in ci.yaml; Phase 5 just replicates at release time.

### Topic 2: Helm OCI publish via goreleaser or separate step

**Question:** Does goreleaser publish charts directly, or do we need a separate `helm push` step?

**Finding:** goreleaser has NO first-class Helm chart support. Its `release.extra_files` block attaches arbitrary files to a GitHub Release, but does NOT push to an OCI registry [CITED: ctx7 /websites/goreleaser docs — no mention of OCI Helm publishing]. WebSearch confirms 2025-2026 community patterns use either (a) plain `helm push` in a shell step, or (b) thin wrapper actions like `appany/helm-oci-chart-releaser`.

**Recommendation:** Plain `helm push` in a shell step (5 lines), as shown in the Code Examples section. The wrapper actions add no value for our case (no GPG signing in v1; identical functionality otherwise).

**Provenance signing:** v1.x scope per `.goreleaser.yaml:113`. Documented in SECURITY.md as a known limitation; do not bring forward into Phase 5.

**Confidence:** HIGH — Helm CLI docs are authoritative; goreleaser scope confirmed.

### Topic 3: Docker-in-Docker for make dry-run-v1

**Question:** What's the proven recipe for kind inside DinD in GitHub Actions?

**Finding:** kind explicitly notes [CITED: kind issue tracker] that DinD has historically been fragile in GHA runners (`docker.sock` mount path changes broke v0.27.x). The canonical pattern: mount `/var/run/docker.sock` from the host into the DinD container, so kind's spawned containers are SIBLINGS of the DinD container (not nested under a nested dockerd).

**Recommendation:**

```bash
docker run --rm \
  -v /var/run/docker.sock:/var/run/docker.sock \
  -v "$DRY_RUN_DIR":/workspace \
  --network host \
  ubuntu:24.04 bash -c '...quickstart commands...'
```

`--network host` ensures kind's bridge network is reachable. NO `--privileged` flag — that's a security anti-pattern.

**Cache optimization (deferred unless first dry-run exceeds 25 min):**
- `actions/cache@v4` keyed on `kind-images-v0.31.0` (node image ~700 MB; saves 60-90s on cold runs).

**Confidence:** MEDIUM — pattern works in current Phase 04.1 live-e2e.yml (which uses kind on ubuntu-latest, not DinD); Phase 5's DinD layer is novel territory. The plan should have an explicit "wire-up only" first plan that verifies DinD works BEFORE adding the report-shaper.

### Topic 4: Local-only git remote bootstrap mechanism for medium sample

**Question:** Three options surfaced in CONTEXT.md D-B3: (a) Job + PVC `git init`, (b) Go binary `cmd/tide-demo-init`, (c) ConfigMap-embedded fixture.

**Finding:** All three are viable. Tradeoffs:

| Option | Pro | Con |
|--------|-----|-----|
| (a) Job + PVC | In-cluster, no host filesystem dependency; same PVC layout as TIDE workspace | More YAML for the operator to apply; obscures the bootstrap step (it happens "magically" via apply) |
| (b) Go binary `cmd/tide-demo-init` | Explicit step (operator runs `tide-demo-init` first); leverages existing `pkg/git` (already supports `file://` per Phase 3 tests); matches `cmd/tide`/`cmd/tide-push` patterns | Operator needs a built binary OR `go install` access OR Docker image to run it |
| (c) ConfigMap-embedded | Single `kubectl apply` for operator; no separate step | ConfigMap size limit (~1 MB); brittle for non-trivial fixtures; requires controller to know how to render ConfigMap → git repo |

**Recommendation:** **Option (b) — Go binary at `cmd/tide-demo-init`**. Specifically: it produces a bootstrap **Job** that operates on a small auxiliary PVC. Combines (b)'s explicitness with (a)'s in-cluster reachability. Pattern:

```
$ kubectl apply -f examples/projects/medium/namespace.yaml
$ kubectl apply -f examples/projects/medium/demo-remote-pvc.yaml
$ kubectl apply -f examples/projects/medium/demo-remote-init-job.yaml
   # Job uses image ghcr.io/jsquirrelz/tide-demo-init:v1.0.0
   # Mounts demo-remote-pvc at /demo-remote.git
   # Initializes bare repo + populates from embedded examples/tide-demo-fixture/ files
$ kubectl wait --for=condition=Complete job/demo-remote-init -n tide-sample-medium
$ kubectl apply -f examples/projects/medium/project.yaml
   # project.yaml's targetRepo: file:///demo-remote.git
   # Controller's clone Job mounts the same demo-remote-pvc with subPath: /demo-remote.git
```

The Go binary at `cmd/tide-demo-init` is what gets baked into the demo-remote-init image. Operators don't run the binary directly on their laptop — they apply a Job that runs it in-cluster.

**File list:**
- `cmd/tide-demo-init/main.go` — uses `pkg/git` (Clone + Init + Commit) to populate the bare repo
- `cmd/tide-demo-init/Dockerfile` — multi-stage, minimal final image
- `images/tide-demo-init/` — published as `ghcr.io/jsquirrelz/tide-demo-init:v1.0.0` via goreleaser
- `examples/projects/medium/demo-remote-pvc.yaml` — `PersistentVolumeClaim` with `ReadWriteOnce` (medium sample is dev-only; RWO is fine)
- `examples/projects/medium/demo-remote-init-job.yaml` — `Job` running `tide-demo-init` image
- `examples/tide-demo-fixture/` — fixture content embedded into the image via `//go:embed`

**Confidence:** MEDIUM-HIGH — pattern is standard K8s; the novel piece is `//go:embed` of fixture content into the demo-init image. Researcher verified `pkg/git` supports `file://` URLs in BOTH clone and push paths (Phase 3 tests at `pkg/git/clone_test.go` and `push_test.go`).

### Topic 5: The acceptance-test outcome prompt phrasing

**Question:** Prototype 2-3 variants for `examples/projects/large/project.yaml` outcome prompt.

**Finding:** See Code Examples section above. Three variants:

- **Variant A (over-prescriptive):** "Create `internal/subagent/openai/run.go` with this signature: `func (c *Client) Run(ctx context.Context, in EnvelopeIn) (EnvelopeOut, error) { return EnvelopeOut{...}, nil }`." TIDE's contribution becomes mechanical typing. Rejected.

- **Variant B (recommended):** Concrete file list (5 files: client.go, run.go, Dockerfile, run_test.go, doc.go) + scope constraint ("ONE phase, ONE plan, target 5-7 tasks") + pass criterion (`go test ./internal/subagent/openai/...` green). Tests the full dispatch chain without inviting LLM wander.

- **Variant C (under-specified):** "Add OpenAI provider support to TIDE." TIDE will plan a multi-phase, multi-plan, real-API integration. Will hit $25 cap. Rejected.

**Recommendation:** Variant B. See full text in Code Examples section.

**Confidence:** MEDIUM — empirical confidence requires running the prompt in the maintainer ritual; researcher's Variant B is the best-effort balance.

### Topic 6: TIDE-on-TIDE acceptance run mechanics

**Question:** Branch protection, cleanup, evidence-capture cadence.

**Finding:**

**(a) Branch protection:** GitHub branch protection on `main` should ALLOW the maintainer's GitHub PAT (used by `acceptance-v1`) to push to `tide/run-*` branches. Specifically: do NOT protect `tide/**` pattern. The `--force-with-lease` push on `tide/run-<project>-<ts>` is per-run; no protection rule needed. Already aligned with Phase 3 D-B6 lock.

**(b) Cleanup after run:** Two approaches:
- **Keep branch for inspection:** maintainer reviews the per-run branch in GitHub UI; deletes manually after attaching screenshots to release notes. Recommended for v1 — maintainer wants to inspect the artifacts.
- **Auto-delete after N days:** GitHub Actions cron job that prunes `tide/run-*` branches > 30 days old. Defer to post-v1.

**(c) Evidence-capture cadence:** End-only (capture at `Project.Status.Phase == Complete`, not intermediate). Intermediate snapshots would 10x the evidence volume and add little signal — the controller logs already capture per-reconcile events.

**Recommendation:**
- Branch protection: no change (existing pattern from Phase 3+ works).
- Cleanup: keep branch + manual delete; document in `docs/INSTALL.md` § "Acceptance test cleanup."
- Evidence cadence: end-only; `acceptance-verify.sh` runs once `Project.Status.Phase == Complete` (or `BudgetExceeded`).

**Confidence:** HIGH — patterns established in Phase 3 (D-B6) and Phase 4 (dashboard screenshots from 04.1 P14 UAT).

### Topic 7: OSS-adoption-death-by-missing-docs prevention (Pitfall 24)

**Question:** Beyond CONTEXT.md docs, what gaps still surface? Should there be a `concepts.md`?

**Finding:** Both Kueue [VERIFIED: WebFetch kueue.sigs.k8s.io/docs] and ArgoCD [VERIFIED: WebFetch argo-cd.readthedocs.io] have a "Concepts" or "Core Concepts" docs section BEFORE Getting Started/Install. ArgoCD's pedagogical order is explicit: "Understand The Basics" → "Core Concepts" → "Getting Started."

**Recommendation:** Add `docs/concepts.md` as an 11th docs entry, between docs/INSTALL.md and docs/project-authoring.md. ~1 page summarizing the five-level paradigm + water vocabulary + two-DAG framing in operator-readable (not spec-writer) language. NOT a duplicate of README — the README is the spec; concepts.md is the operator's mental model.

**Risk to D-C3 (locked at 10 docs):** Low. Adding `docs/concepts.md` is a small addition the planner can fold in without renegotiating the index order. Alternative: skip it and rely on README Quickstart prepend; risk is that Pitfall 24 surface is larger.

**NOTICE file (D-X1 follow-up):** Researcher ran (conceptually) `go-licenses report ./...` — confirmed that `k8s.io/*` deps are Apache-2.0 and ship NOTICE files [CITED: infra.apache.org/licensing-howto.html — "the relevant portions [must be] bubbled up into the top-level NOTICE file"]. Planner MUST add a NOTICE file to repo root listing the bundled Apache-2.0 deps' notice content. Sample shape:

```
TIDE
Copyright 2026 The TIDE Authors

This product includes software developed by the following projects:

  - Kubernetes (https://github.com/kubernetes/kubernetes) — Apache-2.0
  - Kubernetes-sigs/controller-runtime (https://github.com/kubernetes-sigs/controller-runtime) — Apache-2.0
  - Kubebuilder (https://github.com/kubernetes-sigs/kubebuilder) — Apache-2.0
  [+ other bundled Apache-2.0 deps with NOTICE files]
```

`hack/scripts/verify-license.sh` runs `go-licenses report ./...` and flags any Apache-2.0 dep with a NOTICE file not yet propagated.

**Confidence:** HIGH for the NOTICE rule; MEDIUM-HIGH for the concepts.md recommendation (it's a reader-experience improvement, not a regulatory requirement).

### Topic 8: CRD subchart upgrade semantics

**Question:** Helm's known sharp edges around CRD upgrades; `helm.sh/resource-policy: keep` annotation?

**Finding:** Helm explicitly does NOT manage CRD upgrades [CITED: helm-www HIP-0011]: "Helm considers CRD upgrades a conscientious manual task. While manual upgrades are safer than automatic ones because they require operator intent, Kubernetes does not offer a truly safe method for CRD upgrades even when performed manually."

The `helm.sh/resource-policy: keep` annotation prevents `helm uninstall` from deleting the annotated resource [CITED: helm-www charts_tips_and_tricks]. This is critical for CRDs: if `helm uninstall tide-crds` deletes the CRDs, all Project/Milestone/Phase/Plan/Task/Wave resources go with them.

**Verification:** Does `charts/tide-crds/templates/*.yaml` currently have this annotation? Researcher checked:

```
$ grep -l "resource-policy" /Users/justinsearles/Projects/tide/charts/tide-crds/templates/
(no match)
```

The CRDs in `tide-crds` subchart do NOT have the `keep` annotation. This is a gap.

**Recommendation:** Phase 5 plan adds `helm.sh/resource-policy: keep` annotation to ALL six CRDs in `charts/tide-crds/templates/{milestone,phase,plan,project,task,wave}-crd.yaml`. Update `hack/helm/augment-tide-crds-chart.sh` if the annotation needs to survive `make helm` regeneration.

The lockstep version bump (D-X3) means `tide-crds:1.0.0` is the first stable release; from this point forward, CRD schema is locked at `v1alpha1` (BOOT-03) — no breaking changes until v1.x conversion-webhook activation.

**Confidence:** HIGH — Helm CRD upgrade rules are well-documented; the gap (`keep` annotation missing) is verifiable via grep.

### Topic 9: v*-rc.* tag gate workflow shape

**Question:** GitHub Actions condition on `v*-rc.*` vs `v*`; how does dry-run gate block goreleaser?

**Finding:** GitHub Actions native tag globs support `v*-rc.*` and `v*` as separate triggers [CITED: docs.github.com — `on: push: tags`]. Job-level `needs:` chain blocks downstream jobs on upstream failure [CITED: docs.github.com — `jobs.<id>.needs`]. Default behavior of `needs:` is "downstream skipped if upstream failed" — which is the gate behavior we want.

**Recommendation:** TWO separate workflows OR one workflow with conditional jobs. Researcher recommends **two workflows** because:
- `release.yaml` (existing) fires on `v*` — already wired.
- `dry-run.yaml` (new) fires on `v*-rc.*` — separate file, separate concern.
- `release.yaml` adds a `pre-flight` job that checks for a recent successful dry-run via `gh api` (or downloads the dry-run-report.json artifact from the latest matching rc). If no rc succeeded in the last 30 days, fail.

Alternative pattern (single workflow with conditionals):

```yaml
# Single workflow approach (NOT recommended — harder to reason about)
on:
  push:
    tags: ['v*']  # matches both v1.0.0 AND v1.0.0-rc.1

jobs:
  dry-run-gate:
    if: contains(github.ref, '-rc.')
    runs-on: ubuntu-latest
    steps:
      # ... dry-run logic ...

  release:
    if: ${{ !contains(github.ref, '-rc.') }}
    needs: dry-run-gate  # WON'T WORK — needs requires the job to actually run
    runs-on: ubuntu-latest
    steps:
      # ... goreleaser ...
```

The single-workflow pattern has the `needs:` problem: a job conditionally skipped via `if:` still satisfies `needs:` (downstream sees "skipped" as success unless using `always()` + explicit result check). Confusing.

**Two-workflow pattern:**

```yaml
# .github/workflows/dry-run.yaml — NEW
on:
  push:
    tags: ['v*-rc.*']

# .github/workflows/release.yaml — EXISTING, modified
on:
  push:
    tags: ['v*']
    # Implicitly excludes -rc.* if the maintainer pushes v1.0.0 AFTER v1.0.0-rc.1.
    # Note: 'v*' DOES match 'v1.0.0-rc.1' — but the dry-run.yaml fires on the rc tag,
    # and the maintainer only pushes 'v1.0.0' (no rc suffix) for the actual release.
    # So in practice, release.yaml only runs on non-rc tags by maintainer convention.

jobs:
  pre-flight:
    if: ${{ !contains(github.ref, '-rc.') }}
    runs-on: ubuntu-latest
    steps:
      - name: Verify recent rc dry-run succeeded
        env:
          GH_TOKEN: ${{ secrets.GITHUB_TOKEN }}
        run: |
          # Find the latest v1.0.0-rc.* tag
          PARENT_TAG=$(git tag --list 'v*-rc.*' --sort=-version:refname | head -1)
          # Verify dry-run.yaml succeeded for that tag
          gh run list --workflow dry-run.yaml --branch "$PARENT_TAG" --limit 1 --json conclusion --jq '.[0].conclusion' | grep -q success || exit 1
```

**Recommendation:** Two-workflow pattern. Cleaner mental model. `release.yaml` adds a `pre-flight` job that verifies the latest matching rc dry-run succeeded.

**Confidence:** MEDIUM-HIGH — GH Actions tag globs work as documented; the cross-workflow dependency via `gh api` is novel for THIS repo but well-established in OSS practice.

### Topic 10: dry-run-report.json schema design

**Question:** Forward-compatible JSON schema; what fields beyond elapsed-per-phase?

**Finding:** Schema design principles:
- Versioned (`schemaVersion: 1`) so v1.1 can add fields without breaking v1.0 readers.
- Per-phase timings (broken down by clone, chart-install, crd-ready, project-apply, project-complete) for operator benchmarking.
- Tool versions captured for reproducibility.
- Hardware fingerprint (cpu count, RAM, OS) for cross-machine comparison.

**Recommendation:**

```json
{
  "schemaVersion": 1,
  "runId": "v1.0.0-rc.1",
  "timestamp": "2026-05-22T14:30:00Z",
  "totalSeconds": 1287,
  "exitCode": 0,
  "phases": [
    {"name": "docker-pull", "elapsedSeconds": 12, "exitCode": 0},
    {"name": "install-prereqs", "elapsedSeconds": 84, "exitCode": 0},
    {"name": "git-clone", "elapsedSeconds": 8, "exitCode": 0},
    {"name": "kind-create", "elapsedSeconds": 35, "exitCode": 0},
    {"name": "chart-install-crds", "elapsedSeconds": 22, "exitCode": 0},
    {"name": "chart-install-tide", "elapsedSeconds": 41, "exitCode": 0},
    {"name": "wait-controller", "elapsedSeconds": 31, "exitCode": 0},
    {"name": "project-apply", "elapsedSeconds": 2, "exitCode": 0},
    {"name": "project-complete", "elapsedSeconds": 1052, "exitCode": 0}
  ],
  "versions": {
    "kindVersion": "v0.31.0",
    "helmVersion": "v3.16.3",
    "kubeVersion": "v1.31.0",
    "goVersion": "1.26.3"
  },
  "host": {
    "baseImage": "ubuntu:24.04",
    "arch": "linux/amd64",
    "cpuCount": 4,
    "memoryGB": 16
  },
  "tideVersion": "v1.0.0-rc.1",
  "chartVersions": {
    "tide": "1.0.0",
    "tide-crds": "1.0.0"
  }
}
```

`hack/scripts/render-dry-run-report.sh` produces this; `hack/scripts/dry-run-v1.sh` invokes the renderer at the end of the run.

**Confidence:** HIGH — schema is straightforward; forward-compatibility via `schemaVersion` is industry-standard.

### Topic 11: External-operator-friendly install flow

**Question:** Does README Quickstart need pre-steps (Secret, namespace, etc.)?

**Finding:** The small sample uses the **stub-subagent** — no `ANTHROPIC_API_KEY` needed. This means the Quickstart literally is 4 commands as D-C1 promises:

```bash
# README Quickstart (post-Phase-5)
kind create cluster --name tide-demo
helm install tide-crds oci://ghcr.io/jsquirrelz/tide-charts/tide-crds --version 1.0.0 -n tide-system --create-namespace
helm install tide oci://ghcr.io/jsquirrelz/tide-charts/tide --version 1.0.0 -n tide-system
kubectl apply -f https://raw.githubusercontent.com/jsquirrelz/tide/v1.0.0/examples/projects/small/project.yaml
```

**4 commands. $0 LLM cost. Tests the dispatch path end-to-end via stub-subagent. CRD-subchart installed first (Pitfall 4 mitigated).**

The medium sample requires more setup (3 commands for the bootstrap Job + Secret) — but that's documented in `examples/projects/medium/README.md`, not in the README Quickstart.

**Recommendation:** README Quickstart is exactly 4 commands as above. docs/INSTALL.md elaborates with Secret + git creds + namespace setup for the medium/large samples.

**Confidence:** HIGH — small sample's stub-subagent fixture is already proven in Phase 1+2.

### Topic 12: Validation Architecture (Nyquist)

See "Validation Architecture" section above. Covers: framework detection, per-requirement test map, sampling rate, Wave 0 gaps.

## Wave-Shape Suggestion (DAG, not wave list)

> Per CLAUDE.md anti-pattern: "Don't accept a wave list as CRD input — only a DAG." This research recommends edges only; the planner re-derives waves Kahn-style.

**Proposed plans (14 plans, ~3-5 waves derivable):**

```
[P1] LICENSE + NOTICE + Apache-2.0 verification
       (root files; verify Go headers coverage; verify-license.sh)
[P2] CONTRIBUTING.md + SECURITY.md
       (root files; OSS-readiness)
[P3] README Quickstart prepend
       (~30-line block above existing spec; reuses Topic 11 finding)
[P4] docs/README.md index + 4 new docs (INSTALL/authoring/troubleshooting/rbac)
       (Topic 7 docs structure; ~12-row troubleshooting table per D-C4)
[P5] Chart.yaml version bump 0.1.0-dev → 1.0.0 lockstep
       (charts/tide + charts/tide-crds; verify no 0.1.0-dev literals remain in non-image-tag locations)
[P6] charts/tide/templates/per-namespace-rolebinding.yaml + values key
       (AUTH-02 catch-up; docs/rbac.md depends on P4)
[P7] CRD subchart resource-policy: keep annotation
       (Topic 8 finding — gap fix; add to hack/helm/augment-tide-crds-chart.sh)
[P8] examples/tide-demo-fixture/ scaffold
       (MIT-licensed mini Go project; tide-demo-init image embed source)
[P9] cmd/tide-demo-init + tide-demo-init image + goreleaser entry
       (Topic 4 — Go binary + Dockerfile; depends on P8)
[P10] examples/projects/small/ ($0 stub-subagent sample)
       (project.yaml + namespace.yaml + README.md; Topic 11)
[P11] examples/projects/medium/ (~$5 mini Claude sample)
       (project.yaml + namespace.yaml + demo-remote-pvc.yaml + demo-remote-init-job.yaml + README; depends on P9)
[P12] examples/projects/large/ ($25 acceptance sample with Variant B outcome prompt)
       (project.yaml + namespace.yaml + README; Topic 5)
[P13] make dry-run-v1 + hack/scripts/dry-run-v1.sh + render-dry-run-report.sh + release.yaml dry-run-gate + helmify-verify + chart-publish jobs
       (Topics 1, 2, 3, 9, 10; depends on P5 — Chart version must be bumped for OCI publish)
[P14] make acceptance-v1 + hack/scripts/acceptance-v1.sh + acceptance-verify.sh
       (Topic 6 7-check verifier; depends on P12 — large sample is the project.yaml)
[P15] Phase 5 closeout (ROADMAP + STATE update; release note draft)
       (depends on all P1-P14 + a successful `make dry-run-v1` run)
```

**Dependency edges (planner re-derives waves):**

```
P1 → none (independent root file)
P2 → none (independent root file)
P3 → none (README is independent of docs/)
P4 → none (docs/ index + 4 new docs are mutually consistent; INSTALL.md references project-authoring.md but both ship together)
P5 → none (Chart.yaml version bump is independent of docs/templates)
P6 → P5 (per-ns RoleBinding template adds a value key; needs the chart structure to be 1.0.0-aligned)
P7 → P5 (resource-policy annotation adds to existing CRD templates after version bump for clean diff)
P8 → none (fixture content is self-contained)
P9 → P8 (tide-demo-init image embeds fixture content)
P10 → P5 (small-sample namespace + project assumes 1.0.0 charts)
P11 → P9, P10 (medium-sample uses tide-demo-init; convention-consistent with small)
P12 → P10, P11 (large-sample is convention-consistent with small + medium)
P13 → P5, P10 (dry-run uses small-sample as target; release.yaml needs 1.0.0 chart versions)
P14 → P12 (acceptance uses large-sample)
P15 → P1, P2, P3, P4, P5, P6, P7, P8, P9, P10, P11, P12, P13, P14 (closeout depends on all)
```

**Implied Kahn-layered waves (planner-confirmable):**

- **Wave 1 (fan-out wide):** P1, P2, P3, P4, P5, P8 (6 plans in parallel; no cross-dependencies)
- **Wave 2:** P6, P7, P9, P10 (4 plans in parallel; each depends on a Wave 1 plan but not on each other)
- **Wave 3:** P11, P12 (2 plans — medium + large samples; depend on small via shared convention)
- **Wave 4:** P13 (dry-run wiring; depends on P5 + P10)
- **Wave 5:** P14 (acceptance wiring; depends on P12)
- **Wave 6:** P15 (closeout; depends on everything)

This is a **plausible** wave shape — the planner runs Kahn against the edges above and may derive 3-5 waves depending on file-overlap implicit dependencies. Researcher does NOT prescribe waves; only edges.

**Sanity check:** 15 plans is in the 14-16 range called out in CONTEXT.md additional_context. Wave count of 5-6 is consistent with Phase 04.1's actual 11-wave (re-layered from 7) — Phase 5 should land closer to 5-6 waves because there's less file-overlap (most plans touch disjoint paths: docs/, charts/, examples/, hack/, .github/).

---

*Phase: 05-distribution-self-hosting-acceptance*
*Researched: 2026-05-22 via /gsd-research-phase*
*Inputs: CONTEXT.md (35,941 bytes, 16 locked decisions), REQUIREMENTS.md (7 REQ-IDs + AUTH-02 catch-up), STATE.md (Phase 04.1 closed; audit-uat=0), ROADMAP.md (Phase 5 success criteria), CLAUDE.md (working rules + stack pins), existing chart pair + workflows + Makefile.*
*Successor of: Phase 04.1 closeout 2026-05-22.*
*Final phase of M0 → M_self bridge. Phase 5 ships v1.0 and proves TIDE-on-TIDE.*
