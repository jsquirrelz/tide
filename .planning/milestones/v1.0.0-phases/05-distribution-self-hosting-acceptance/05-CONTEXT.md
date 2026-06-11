# Phase 5: Distribution & Self-Hosting Acceptance - Context

**Gathered:** 2026-05-22
**Status:** Ready for research/planning
**Depends on:** Phase 04.1 (Pre-v1 audit fixes + cross-phase UAT closeout — closed 2026-05-22 with audit-uat total_items = 0)

<domain>
## Phase Boundary

The v1.0 ship phase. Phase 5 closes three loops simultaneously and produces the artifacts that let an external operator install TIDE and use it without prior context:

- **OSS-readiness loop:** Apache-2.0 LICENSE at repo root (Go file headers already in place from Phase 1+), `CONTRIBUTING.md` + `SECURITY.md`, README opens with a ~30-line Quickstart before the existing paradigm spec, and a `docs/README.md` index ordering the 7 existing docs + 4 new ones (INSTALL.md, project-authoring.md, troubleshooting.md, rbac.md) in reader-journey order.
- **Distribution loop:** Helm chart pair `charts/tide` + `charts/tide-crds` is already split (Phase 1 D-E1). Phase 5 finalizes the chart for release — `helmify` integration into `release.yaml` (today the workflow runs goreleaser only), Chart.yaml version bump from `0.1.0-dev` to `1.0.0`, AUTH-02 carry-forward (per-namespace RoleBinding YAML template — deferred-to-Phase-5 from Phase 1 / REQUIREMENTS.md), and the OCI vs index.yaml chart distribution surface.
- **Acceptance loop:** Two distinct proof points wrapping the M0 → M_self bridge. **`make dry-run-v1`** — Docker-in-Docker scripted, ubuntu:24.04 clean image, runs the README Quickstart commands verbatim and times them; the small-sample Project reaching `Status=Complete` is the timer-stop. Fires on `v*-rc.*` tag push; blocks goreleaser if >30 min or any step fails. Transcript + `dry-run-report.json` upload to the GitHub Release as assets. **`make acceptance-v1`** — maintainer-only ritual on the dev laptop. Fresh kind + helm install + `kubectl apply -f examples/projects/large/project.yaml` drives TIDE to author the `internal/subagent/openai/` skeleton phase (mirroring Phase 3 D-C1 anthropic layering — a real v1.x deliverable). Single Phase scope. Hard $25 cap, no bypass. Pass = artifacts on per-run branch + `Project.Status=Complete` + zero controller error logs + no orphan Jobs + gitleaks passed + budget under cap.

Phase 5 also ships **3 sample Projects at `examples/projects/{small,medium,large}/`** as the OSS quickstart surface:

- **small** — $0 stub-subagent; runs the Phase 1 α…θ Task fixture as a smoke test against any cluster. No API key needed. This is the DIST-05 dry-run target.
- **medium** — ~$5 with real Claude; scaffolded from `examples/tide-demo-fixture/` (a tiny Go-file + test seed kept in THIS repo); on apply, the sample initializes a fresh local-only git remote (file:// or temp-dir `git init`) so no external public repo dependency exists. Operators see TIDE clone + plan + commit + push in a contained loop on their own machine.
- **large** — ~$25 with real Claude; IS the acceptance-test `project.yaml`. Targets THIS TIDE repo. Outcome prompt: author the scaffold for `internal/subagent/openai/` mirroring `internal/subagent/anthropic/` (Phase 3 D-C1 layering pattern), stub the `Subagent.Run()` method, ship the Dockerfile, wire one e2e test.

Phase 5 does NOT: ship a multi-vendor subagent matrix (`internal/subagent/openai/` is the *output* of the acceptance test, not part of Phase 5's source-of-truth deliverables — what Phase 5 ships is the contract that lets TIDE author it); add `CODE_OF_CONDUCT.md` or `GOVERNANCE.md` (solo-maintainer posture; deferred to post-v1); stand up a `tide.dev/` docs site (Mkdocs deferred); change CRD schema or API group (`tideproject.k8s/v1alpha1` is locked across M0 → M_self per Phase 1 D-A3 / REQUIREMENTS.md BOOT-03); ship a fixture-repo at `github.com/jsquirrelz/tide-demo-fixture` (no external public repo per user decision); run live nightly CI against Claude (acceptance is maintainer ritual per the cost-control decision); add a friend-of-project cold-read pass to DIST-05 (scripted dry-run is the gating signal).

</domain>

<decisions>
## Implementation Decisions

### Acceptance-test target + budget (BOOT-04 / DIST-05)

- **D-A1:** **Single Phase scope** for the v1 acceptance test. `project.yaml` targets THIS TIDE repo. TIDE authors one **phase brief**, then one **`PLAN.md`**, then dispatches the resulting Task DAG via executor Jobs (typical 5-8 tasks). Exercises Phase → Plan → Task → Wave reconciler dispatch + push at every level boundary. Bounded budget. Self-contained pass criteria. Falls short of the spec's "Milestone" depth deliberately — full Milestone-level fan-out on the first real Claude run would compound failures and blow the budget. The acceptance signal is: full descent works once, repeatably, under a hard cap. Future milestone-level dogfood happens organically post-v1 when the team uses TIDE for real work.

- **D-A2:** **Hard cap $25, no bypass.** `Project.Spec.budget.costCeilingCents: 2500`. If hit, dispatch halts + `BudgetExceeded` condition fires (Phase 2 D-D2 infra + Phase 04.1 D-P4 rolling-window-reset infra) + `make acceptance-v1` exits non-zero + marks BLOCKED. The budget cap halt is itself one of the acceptance signals — proves Phase 2 FAIL-04 + Phase 04.1 P4.1 work in production. `tide approve --bypass-budget` exists but is **not used** in the acceptance run (the cap fires = test fails, the cap doesn't fire = test continues; bypass is a manual-recovery affordance, not an acceptance affordance).

- **D-A3:** **Pass criteria — concrete + machine-checkable.** All of:
  1. Per-run branch `tide/run-<project>-<unix-timestamp>` exists on the configured remote (the large sample uses THIS TIDE repo; the branch lives here too) — Phase 3 D-B6 lock.
  2. Per-run branch has commits matching all 4 D-B2 commit-message shapes (`tide: plan <name> authored + executed`, `tide: phase <name> authored`, `tide: milestone — N/A for Single Phase scope`, `tide: project complete`).
  3. `Project.Status.Phase == Complete`.
  4. `kubectl logs -n tide-system deploy/tide-controller-manager --since=<run-start>` contains zero `ERROR` level lines.
  5. `kubectl get jobs --all-namespaces -l tideproject.k8s/project-uid=<uid>` shows all Jobs in `status.succeeded=1` (zero orphans).
  6. `tide_secret_leak_blocked_total{project=<name>}` == 0 (gitleaks passed).
  7. `Project.Status.budget.costSpentCents < 2500` (under the cap).
  These 7 checks compose into the `make acceptance-v1` exit code. Pass = exit 0; any check failing = exit non-zero + halt + maintainer inspects.

- **D-A4:** **Maintainer-only `make acceptance-v1` on dev laptop.** No CI integration. The Make target spins fresh kind cluster + `helm install tide ./charts/tide` + applies `examples/projects/large/project.yaml` with maintainer-supplied `ANTHROPIC_API_KEY` env. Captures evidence on local disk under `.acceptance-runs/<timestamp>/`: controller logs, Project + child CRDs (yaml -o), per-run-branch git log, dashboard screenshot (Chrome DevTools MCP). Maintainer attaches transcript to v1.0 release notes. Acceptance is a maintainer ritual, not a CI gate — live LLM cost per PR/nightly is unjustified before OSS adoption proves there's demand; reusable on every release thereafter. (The dry-run — D-D1..D-D4 — is the CI-runnable proof.)

### 3 sample Projects (DIST-04)

- **D-B1:** **Cost spectrum** for the three samples — small ($0 stub) → medium (~$5 mini real-Claude) → large (~$25 acceptance run). Cost is the discriminator new operators care about most. Lets first-time operators feel TIDE end-to-end with `examples/projects/small/` before paying a cent; medium gives the first taste of real LLM-driven planning; large is the full acceptance test. No multi-vendor matrix as a sample axis (deferred to v1.x community contributions per Phase 3 D-C1).

- **D-B2:** **`examples/projects/{small,medium,large}/`** is the canonical location. Top-level, operator-discoverable, README Quickstart reads `kubectl apply -f examples/projects/small/project.yaml`. Phase 1's `config/samples/` α…θ Task fixture **stays where it is** — those are kubebuilder test fixtures for `pkg/dag`'s worked example, not operator-facing demos. No symlinks / mirrors between the two paths — they serve different audiences and live independently.

- **D-B3:** **`examples/tide-demo-fixture/`** lives in THIS TIDE repo as the source-of-truth scaffold for the medium sample's target content. Contents: 1 README.md + 1 small Go file + 1 minimal Go test, MIT-licensed within itself (for clarity that it's not part of TIDE's Apache-2.0 distribution). On `kubectl apply -f examples/projects/medium/project.yaml`, the controller (or a small bootstrap script invoked by the sample) initializes a **fresh local-only git remote** from this scaffold — file:// URL or temp-dir + `git init` + initial commit. TIDE clones from that local remote, plans, commits artifacts, pushes back. Zero external/third-party dependency. Repeatable. Operators can run the medium sample offline.

- **D-B4:** **Large sample's outcome prompt:** "Author the scaffold for `internal/subagent/openai/` mirroring `internal/subagent/anthropic/` — stub the `Subagent.Run()` method, ship the Dockerfile, wire one e2e test." Authentic v1.x work (the layering pattern from Phase 3 D-C1 is the contract — the OpenAI provider is real demand-side work). Tight scope (~5-8 task DAG). Reviewable. Useful. If the acceptance run succeeds, the maintainer has the option to actually merge the authored skeleton — that's a bonus, not a Phase 5 deliverable.

### Documentation strategy + OSS readiness (DIST-03 + DIST-04 + Pitfall 24)

- **D-C1:** **README stays the load-bearing paradigm spec** (per CLAUDE.md "README.md is the design spec and doubles as the public-facing README"). Spec content stays. Phase 5 adds **a ~30-line Quickstart block at the TOP** before the spec, with: 4 copy-pasteable commands (`kind create cluster && helm install tide-crds ./charts/tide-crds && helm install tide ./charts/tide && kubectl apply -f examples/projects/small/project.yaml`), expected output, ~3 lines linking to `docs/INSTALL.md` and `docs/project-authoring.md`. First-time readers get "try it" before "read the theory"; spec text remains the authoritative paradigm doc.

- **D-C2:** **`docs/INSTALL.md` is the on-ramp.** Detailed install steps (prerequisites: Docker, kubectl, helm 3, kind; cluster create; CRD subchart install separate from main chart with explanation; ANTHROPIC_API_KEY Secret creation; git creds Secret creation; project apply). Sections cover the 3 OS install paths (macOS via brew; Linux via package manager + go install; Windows via WSL2 + scoop). Links forward to `docs/project-authoring.md` for next step. Single source of truth for "how do I install TIDE"; README Quickstart points here.

- **D-C3:** **Docs reader-journey order** in `docs/README.md` (new index file) — **11 numbered entries, 12 doc files** (entry #4 covers two co-located docs for dashboard + CLI):
  1. **install** — `docs/INSTALL.md` (new) — prerequisites + Helm install + first sample
  2. **concepts** — `docs/concepts.md` (new) — TIDE's five-level paradigm in operator-readable language (~1 page; precedent: Kueue + ArgoCD both ship "Core Concepts" docs before Getting Started; **adopted per user revision 2026-05-22 after researcher Open Question #1**)
  3. **project authoring** — `docs/project-authoring.md` (new) — Project.Spec field reference + 3 sample walkthrough
  4. **dashboard + CLI** — `docs/dashboard.md` (exists) + `docs/cli.md` (exists)
  5. **gates** — `docs/gates.md` (exists)
  6. **observability** — `docs/observability.md` (exists)
  7. **git hosts** — `docs/git-hosts.md` (exists)
  8. **storage drivers** — `docs/rwx-drivers.md` (exists)
  9. **live E2E** — `docs/live-e2e.md` (exists)
  10. **troubleshooting** — `docs/troubleshooting.md` (new)
  11. **RBAC reference** — `docs/rbac.md` (new) — per-Kind verbs, namespace-scoped role binding template
  No mkdocs site, no GitHub Pages publish — that's post-v1. The `docs/` directory + `docs/README.md` index is the v1 surface.

- **D-C4:** **`docs/troubleshooting.md` shape:** single Markdown table `Symptom | Cause | Recipe`, ~12 entries covering: finalizer stuck (with `kubectl patch finalizers` recipe — ROADMAP mandates this), `ANTHROPIC_API_KEY` invalid (401 in controller logs), push lease conflict (Phase 3 D-B6 `PushLeaseFailed` + bypass annotation), gitleaks blocked (`tide_secret_leak_blocked_total` increment + per-Project gitleaks-config override), PVC `ReadWriteMany` missing (links to `rwx-drivers.md`), dashboard 404 (port-forward needed; ingress doc), CRDs not registered (CRD subchart not installed — common misstep given the split), admission webhook 422 (cycle detected or file-touch mismatch — Phase 2 D-E1..E4), budget halted (`BudgetExceeded` + bypass annotation), gate awaiting approval (`tide approve` or `kubectl annotate`), pod `ImagePullBackoff` (image pull secret or registry credentials), leader election lost (controller restart — chaos-resume infra). Each row links to deeper doc if applicable. Scan-friendly for ops in incident.

- **D-C5:** **Contributor OSS docs ship in v1:** `CONTRIBUTING.md` + `SECURITY.md` at repo root.
  - `CONTRIBUTING.md` covers: prerequisites (Go 1.26, kubebuilder, kind, helm), `make test` / `make test-int` / `make test-e2e-kind`, branch naming convention (`feat/`, `fix/`, `docs/`), commit message conventions (Conventional Commits), PR template (link to issue, test plan, breaking change?), DCO signoff (`git commit -s`).
  - `SECURITY.md` covers: how to report a vulnerability (email + key fingerprint or GitHub Security Advisory), expected response time (48h ack), what's in scope (controller, dashboard, CRDs, RBAC), what's out (third-party LLM provider key compromise, K8s cluster compromise).
  - `CODE_OF_CONDUCT.md` + `GOVERNANCE.md` **deferred to post-v1** per solo-maintainer posture.

### External-operator dry-run methodology (DIST-05)

- **D-D1:** **`make dry-run-v1`** is the DIST-05 mechanism. Docker-in-Docker: spin a fresh `ubuntu:24.04` container with Docker socket mounted, `git clone https://github.com/jsquirrelz/tide` inside it, then run the README Quickstart commands verbatim. Measures wall-clock from `git clone` to small-sample `Project.Status.Phase == Complete`. The Make target lives in the main `Makefile` so contributors can run it locally on a Linux/macOS box without GitHub Actions. CI uses the same target (no divergence between local + CI dry-run).

- **D-D2:** **Timer stops at small-sample Project Status=Complete.** Concretely:
  ```bash
  $ time (
    git clone https://github.com/jsquirrelz/tide && cd tide \
      && kind create cluster --name tide-dry-run \
      && helm install tide-crds ./charts/tide-crds \
      && helm install tide ./charts/tide \
      && kubectl wait --for=condition=Available deploy/tide-controller-manager -n tide-system --timeout=5m \
      && kubectl apply -f examples/projects/small/project.yaml \
      && kubectl wait --for=jsonpath='{.status.phase}'=Complete project/small --timeout=10m
  )
  ```
  Uses the **small** ($0 stub) sample, so the dry-run has zero LLM cost — safely repeatable. Timer must report < 30 min for pass. Captures elapsed per-phase: clone, chart-install, crd-ready, project-apply, project-complete.

- **D-D3:** **Release-candidate tag gate.** `make dry-run-v1` runs in a GitHub Actions job triggered ONLY on `v*-rc.*` tag push (release candidate, not full release). Blocks the goreleaser release job if the dry-run fails or wall-clock exceeds 30 min. Cheap (runs only on release prep, not on every PR or nightly), high signal (catches install-flow regressions exactly when we care). No `schedule:` cron — same posture as `.github/workflows/live-e2e.yml` (manual `workflow_dispatch` + tag gate per Phase 04.1 P2.4 lock).

- **D-D4:** **Evidence:** transcript + `dry-run-report.json` upload to the GitHub Release as assets alongside goreleaser tarballs. `dry-run-report.json` shape: `{"runId": "<rc-tag>", "totalSeconds": N, "phases": [{"name": "clone", "elapsedSeconds": N, "exitCode": 0}, {"name": "chart-install", ...}, ...], "kindVersion": "...", "helmVersion": "...", "kubeVersion": "..."}`. Operators downloading the v1.0 release see proof of install-flow correctness in the release page. CI status alone is insufficient (workflow runs garbage-collected after 90 days).

### Cross-cutting (Claude's Discretion — researcher + planner finalize)

- **D-X1:** **LICENSE file at repo root.** Apache-2.0 boilerplate, `[yyyy]` = `2026`, `[name of copyright owner]` = `The TIDE Authors` (matches existing Go file header convention `Copyright 2026 TIDE Authors`). No `NOTICE` file unless a third-party dep mandates one — researcher checks `go.sum` for Apache-2.0 deps that require NOTICE redistribution (e.g., some `k8s.io/*` deps do).

- **D-X2:** **`helmify` wiring into `release.yaml`.** Today `release.yaml` runs goreleaser only (Phase 04.1 P2.4). Phase 5 adds a `helmify` step that takes the kubebuilder Kustomize output (`config/`) and produces the Helm chart bundle at `charts/tide/` + `charts/tide-crds/` as a regenerate-from-Kustomize step (or verifies the existing charts are in sync). Planner decides: regenerate-and-commit-as-part-of-release vs verify-only-and-fail-if-drifted. Lean toward verify-only — the charts are already hand-tuned and shouldn't be auto-regenerated.

- **D-X3:** **Chart version bump 0.1.0-dev → 1.0.0.** Both `charts/tide/Chart.yaml` and `charts/tide-crds/Chart.yaml` get `version: 1.0.0` + `appVersion: "1.0.0"` synchronized to the release tag. Lockstep version bump prevents the "did this CRD chart upgrade match this controller chart?" question.

- **D-X4:** **AUTH-02 catch-up — per-namespace RoleBinding template.** Phase 1 left this for here per REQUIREMENTS.md ("Phase 5 satisfies via per-namespace RoleBinding YAML template in Helm chart"). Add `charts/tide/templates/per-namespace-rolebinding.yaml` that takes a Helm value `projectNamespaces: [...]` and emits one RoleBinding per listed namespace binding `tide-orchestrator` SA to the per-Kind ClusterRoles already shipped from Phase 1. Documented in `docs/rbac.md`.

- **D-X5:** **Chart distribution surface.** Helm OCI (`ghcr.io/jsquirrelz/tide-charts/{tide,tide-crds}:v1.0.0`) is the v1 primary distribution channel because GitHub Container Registry is already wired by goreleaser. Optional `index.yaml` (gh-pages branch) added in the same release.yaml step for `helm repo add tide ...` users. Both surfaces ship the same chart bundle.

- **D-X6:** **README Quickstart command shape uses OCI** (`helm install tide-crds oci://ghcr.io/jsquirrelz/tide-charts/tide-crds --version 1.0.0`) for fresh installs, with a note that `helm install ./charts/tide-crds` works when readers have already cloned the repo. Two install paths, one quickstart block.

- **D-X7:** **Conversion webhook stays a no-op.** CRD-05 scaffold from Phase 1 stays the no-op it shipped as. v1.0 has a single `v1alpha1` schema; conversion-webhook activation is v1.x `v1beta1` work. Documented in `docs/rbac.md` (the webhook still needs RBAC plumbing even as a no-op).

</decisions>

<canonical_refs>
## Canonical References

**Downstream agents MUST read these before researching or planning Phase 5.**

### Project paradigm + vocabulary + working rules
- `README.md` — Five-level paradigm, two-DAG framing, water/tide vocabulary, wave-boundary failure semantics. Phase 5 adds a ~30-line Quickstart block at the TOP before this content.
- `CLAUDE.md` — Project working rules; API group `tideproject.k8s` invariant (Phase 1 D-A3); Helm-values-chart-is-fixed-contract anti-pattern; ServiceMonitor-disabled-by-default rule; technology stack pins (Go 1.26, controller-runtime v0.24, Helm 3, helmify, goreleaser).
- `.planning/PROJECT.md` — Vision, locked Key Decisions; v1 = self-hosting MVP (TIDE drives its own next milestone).
- `.planning/REQUIREMENTS.md` — 7 Phase 5 REQ-IDs (DIST-01..05, BOOT-02, BOOT-04) + AUTH-02 catch-up (per-namespace RoleBinding template — explicit Phase 5 owner per traceability table).
- `.planning/STATE.md` — Current cursor: Phase 04.1 closed 2026-05-22; 7/8 phases complete; next focus = Phase 5 (this phase).

### Phase 1 carry-forward (decisions that constrain Phase 5)
- `.planning/phases/01-foundation-crds-pkg-dag-controller-scaffold/01-CONTEXT.md`:
  - **D-A3** (API group `tideproject.k8s` locked) — Phase 5 chart references stay under this group.
  - **D-E1** (CRD subchart `charts/tide-crds` separate from main `charts/tide`) — Phase 5 finalizes version bump + OCI publish for both.
  - **D-G1..G2** (helmify integration — Phase 1 generated initial charts; Phase 5 wires release.yaml step).
- `config/samples/` (Phase 1 α…θ Task fixture) — stays as kubebuilder test fixture for `pkg/dag`'s worked example. Phase 5 does NOT touch these. New samples live at `examples/projects/`.

### Phase 2 carry-forward (decisions that constrain Phase 5)
- `.planning/phases/02-dispatch-plan-validation-innermost-reconcilers-harness/02-CONTEXT.md`:
  - **D-D2** (Per-Project budget cap halt + bypass annotation) — Phase 5 acceptance test relies on this (D-A2 hard cap, no bypass used).
  - **D-G1..G2** (PVC layout + init Job pattern) — Phase 5 medium-sample local-only-git-remote bootstrap follows this pattern.
  - **D-H1..H4** (Layer A envtest + Layer B kind tier) — Phase 5 `make dry-run-v1` is a new Layer C (operator-readiness tier).

### Phase 3 carry-forward (decisions that constrain Phase 5)
- `.planning/phases/03-up-stack-reconcilers-git-integration-real-subagent-resumptio/03-CONTEXT.md`:
  - **D-A1..A2** (Up-stack reconciler dispatch + ChildCRDSpec materialization) — Phase 5 acceptance test exercises Phase → Plan → Task dispatch end-to-end.
  - **D-B2** (4 commit-message shapes at level boundaries) — Phase 5 D-A3 pass criteria #2 verifies these.
  - **D-B6** (per-run branch `tide/run-<project>-<unix-ts>`, never `main`, `--force-with-lease`) — Phase 5 D-A3 pass criteria #1.
  - **D-C1** (`internal/subagent/{provider}/` layering pattern) — Phase 5 large-sample outcome prompt asks TIDE to author `internal/subagent/openai/` mirroring this pattern (D-B4).

### Phase 4 carry-forward (decisions that constrain Phase 5)
- `.planning/phases/04-gates-observability-dashboard-cli/04-CONTEXT.md`:
  - **D-D2** (Dashboard SA + apiserver proxy, no browser-direct apiserver) — Phase 5 docs/dashboard.md already exists; Phase 5 references for install troubleshooting (port-forward path).
  - **D-O2** (Prometheus metric label cardinality bounded to project/phase/plan) — Phase 5 D-A3 pass criteria uses `tide_secret_leak_blocked_total{project=...}` directly.
  - **D-X3** (Helm chart additions: dashboard-deployment, dashboard-rbac, servicemonitor gated) — Phase 5 chart bundle already includes these; version bumps to 1.0.0 lockstep.

### Phase 04.1 carry-forward (decisions that constrain Phase 5)
- `.planning/phases/04.1-pre-v1-audit-fixes-cross-phase-uat-closeout/04.1-PLAN.md` (and per-plan SUMMARY.md files):
  - **P1.1..P1.4** (4 production-wiring fixes) — Phase 5 acceptance test inherits a cleanly-wired controller (no first-Project fallback, ProjectReconciler.Dispatcher wired, DefaultCaps applied, planner-Job EnvelopeIn no longer discarded).
  - **P2.4** (`.github/workflows/live-e2e.yml` uses `workflow_dispatch:` ONLY, no `schedule:` cron — locked decision) — Phase 5 dry-run gate follows the same posture: tag-triggered only, no scheduled runs.
  - **P4.1** (Budget rolling-window reset + `RollingWindowDuration` field) — Phase 5 acceptance test relies on this for the $25 cap behavior.

### Existing v1 doc surface (7 docs already shipped; Phase 5 adds 4 new + indexes all 11)
- `docs/cli.md` (368 lines) — `tide` CLI verbs reference. Phase 5 references for troubleshooting recipes.
- `docs/dashboard.md` (99 lines) — Dashboard install + port-forward. Phase 5 references for INSTALL.md.
- `docs/gates.md` (102 lines) — Per-level gate policy. Phase 5 references for troubleshooting "gate awaiting approval".
- `docs/git-hosts.md` (186 lines) — GitHub/GitLab/Gitea HTTPS+PAT setup. Phase 5 references for project-authoring.md.
- `docs/live-e2e.md` (148 lines) — Existing live-e2e doc (TEST-03 / Phase 3 + Phase 04.1 P2.4 work). Phase 5 references for acceptance-test docs.
- `docs/observability.md` (121 lines) — OTel + Prometheus setup. Phase 5 references for troubleshooting (metric cardinality, ServiceMonitor gate).
- `docs/rwx-drivers.md` (82 lines) — RWX PVC driver matrix. Phase 5 references for troubleshooting (PVC issues).

### Distribution + release infra (existing)
- `charts/tide/Chart.yaml` — Currently `version: 0.1.0-dev`. Phase 5 bumps to `1.0.0`.
- `charts/tide-crds/Chart.yaml` — Currently `version: 0.1.0-dev`. Phase 5 bumps to `1.0.0` synchronized with main chart.
- `charts/tide/templates/` — 27+ templates (controller deployment, dashboard, RBAC per-Kind, metrics, webhooks). Phase 5 adds `per-namespace-rolebinding.yaml` (AUTH-02 catch-up).
- `.github/workflows/release.yaml` — Today goreleaser-only on `v*` tag. Phase 5 extends with `helmify` verify step + dry-run gate on `v*-rc.*` + OCI chart push to ghcr.io.
- `.github/workflows/live-e2e.yml` — Phase 04.1 P2.4 added with `workflow_dispatch:` only. Phase 5 follows same posture for the dry-run workflow.

### External tech specifications (read on demand; don't pre-load)
- **Apache-2.0 LICENSE** — https://www.apache.org/licenses/LICENSE-2.0.txt — boilerplate for repo-root LICENSE file (D-X1).
- **`helmify`** — https://github.com/arttor/helmify — Kustomize-to-Helm tool used in release pipeline.
- **`goreleaser`** — already wired; https://goreleaser.com/customization/ — `helmify` step plugs in via `prebuild:` hook (planner verifies).
- **Helm OCI** — https://helm.sh/docs/topics/registries/ — `helm push <chart> oci://ghcr.io/...` for chart publish (D-X5).
- **Krew plugin manifest** — https://krew.sigs.k8s.io/docs/developer-guide/develop/manifest/ — `tide` CLI is already wired through Krew (Phase 4 D-C2); Phase 5 verifies the manifest entry matches the released binary.
- **Contributor Covenant (deferred)** — https://www.contributor-covenant.org/version/2/1/code_of_conduct/ — for post-v1 CODE_OF_CONDUCT.md if adopted.

</canonical_refs>

<code_context>
## Existing Code Insights

### Reusable Assets

- **`charts/tide/` + `charts/tide-crds/` Helm chart pair** (Phase 1 D-E1, finalized through Phase 4 D-X3) — already split into subchart pattern per REQ-DIST-01. Phase 5 adds `per-namespace-rolebinding.yaml` + bumps versions + wires OCI publish + helmify verify step. No structural chart rework.
- **`.github/workflows/release.yaml`** (Phase 04-09) — goreleaser tag-triggered pipeline. Phase 5 extends with helmify verify step + dry-run gate + OCI chart push. Existing workflow preserved.
- **`.github/workflows/live-e2e.yml`** (Phase 04.1 P2.4) — `workflow_dispatch:` only, no cron. Phase 5 dry-run gate mirrors this posture for `v*-rc.*` tag.
- **Existing 7 docs at `docs/`** (cli/dashboard/gates/git-hosts/live-e2e/observability/rwx-drivers) — content stays; Phase 5 adds INSTALL.md + project-authoring.md + troubleshooting.md + rbac.md + docs/README.md index (reader-journey ordering).
- **Go file Apache-2.0 headers** (every `.go` file under `api/`, `cmd/`, `internal/`, `pkg/`, `test/`, `tools/`) — already present from Phase 1+ codegen. Phase 5 only adds LICENSE root file.
- **`config/samples/` α…θ Task fixture** (Phase 1) — stays as kubebuilder test fixture; not relocated, not modified.
- **`Project.Spec.budget.costCeilingCents`** (Phase 2 D-D2 + Phase 04.1 P4.1 rolling-window reset) — acceptance-test D-A2 hard $25 cap reads this field.
- **Per-run branch `tide/run-<project>-<unix-ts>`** (Phase 3 D-B6) — acceptance-test D-A3 pass criteria #1 verifies this.
- **4 commit-message shapes** (Phase 3 D-B2) — acceptance-test D-A3 pass criteria #2 verifies these.
- **Phase 4 dashboard at `cmd/dashboard/` + `dashboard/web/`** — acceptance-test optionally screenshot via Chrome DevTools MCP for evidence capture.

### Established Patterns

- **Tag-triggered release workflows** (Phase 04 D-X3 — release.yaml on `v*`; Phase 04.1 P2.4 — live-e2e.yml on `workflow_dispatch:` only). Phase 5 dry-run follows the same posture: `v*-rc.*` tag triggers only, blocks downstream goreleaser step on fail.
- **CRD-subchart-separate-from-controller-chart for upgrade safety** (Phase 1 D-E1). Phase 5 preserves this.
- **No external/third-party dependencies in test paths** (Phase 2 D-H1..H2 stub-subagent for Layer A envtest, Phase 02.1+02.2 cert-manager managed locally). Phase 5 medium sample extends this principle to operator-runnable demos: local-only git remote scaffolded from in-repo content.
- **Make targets as the user-facing entry surface** (`make test`, `make test-int`, `make test-e2e-kind`, `make lint`). Phase 5 adds `make dry-run-v1` + `make acceptance-v1` following the same convention.
- **Evidence capture under `.acceptance-runs/<timestamp>/`** — convention from Phase 02.2 + 04.1 closeout iterations; Phase 5 reuses for the maintainer ritual.

### Integration Points

- **`charts/tide/values.yaml`** — chart-is-fixed-contract per CLAUDE.md anti-pattern. Phase 5 adds new value keys for `projectNamespaces: []` (per-namespace RoleBinding template) + `prometheus.serviceMonitor.enabled: false` (already from Phase 4 D-O6, just documents in INSTALL.md). New keys are additive, default-safe.
- **`.github/workflows/release.yaml`** — Phase 5 inserts helmify verify step BEFORE goreleaser; inserts dry-run gate trigger on `v*-rc.*` BEFORE the main `v*` release path (rc tag is the gate, full tag is the release).
- **`README.md`** — Phase 5 prepends ~30-line Quickstart block ABOVE the existing spec content. Spec content remains the bulk and stays unchanged.
- **`docs/` directory** — Phase 5 adds 4 new files + 1 index file. Existing 7 docs unchanged except possibly minor cross-linking edits.
- **No new Go packages.** Phase 5 is documentation, samples, release-pipeline polish, and the per-namespace RoleBinding template. Zero new code under `pkg/`, `internal/`, `cmd/`. The acceptance test IS the proof Phase 5's deliverables are sufficient — not a Go test in `_test.go` files.

</code_context>

<specifics>
## Specific Ideas

- **Two distinct acceptance proof points** — `make dry-run-v1` (CI-runnable, no LLM cost, install-flow only) + `make acceptance-v1` (maintainer ritual, real LLM, full Phase scope). They prove different properties: dry-run proves "operators can install"; acceptance-v1 proves "TIDE actually works." Don't conflate them in plans.

- **The medium sample's local-only git remote is the most novel mechanic in Phase 5.** Researcher should investigate the cleanest way to bootstrap a fresh git remote from `examples/tide-demo-fixture/` at sample-apply time — options include: (a) a Job in `examples/projects/medium/` that runs `git init` + `git remote add` on the PVC; (b) a small Go binary at `cmd/tide-demo-init/` that operators run before `kubectl apply -f`; (c) embedding the demo-fixture content in the Project CRD itself via a ConfigMap. Lean toward (b) — explicit step, no in-cluster magic, matches the "stateless CLI" affordance.

- **The large sample's outcome prompt is the contract.** TIDE has to read the project.yaml outcome prompt and produce a phase brief + PLAN.md + tasks that, when executed, scaffold `internal/subagent/openai/`. The prompt phrasing matters — too vague and TIDE wanders; too prescriptive and we're hand-authoring the plan. Researcher prototypes 2-3 outcome-prompt variants and recommends one.

- **The `dry-run-report.json` schema is forward-compatible.** Add a `schemaVersion: 1` field so future versions can extend without breaking older readers. Operators who inspect the JSON for benchmarking purposes shouldn't see breaking changes between v1.0 and v1.1.

- **Helm OCI vs `index.yaml`** — both ship in v1, OCI is primary. README Quickstart uses OCI command (`helm install tide-crds oci://ghcr.io/jsquirrelz/tide-charts/tide-crds --version 1.0.0`). `docs/INSTALL.md` shows both paths. Avoids forcing users into one distribution channel.

- **`per-namespace-rolebinding.yaml` template uses a Helm range** over `.Values.projectNamespaces`. Empty default (no per-namespace RoleBinding shipped unless user opts in). Documented as "for multi-Project installs, list your project namespaces here."

- **License file content** — pure boilerplate from https://www.apache.org/licenses/LICENSE-2.0.txt (the official Apache-2.0 text). Repo-root `LICENSE` file. No `NOTICE` file unless `go.sum` audit surfaces a third-party Apache-2.0 dep that mandates one (likely some `k8s.io/*` deps do — researcher verifies).

- **README Quickstart "expected output" block** — under each command, show 2-3 lines of the actual output a reader would see (e.g., `helm install` shows "STATUS: deployed"; `kubectl wait` shows "condition met"). Sets expectation, builds confidence. ~10 of the 30 lines.

- **Chrome DevTools MCP for evidence capture in `make acceptance-v1`** — Phase 04.1 Plan 04.1-14 already exercises this against the dashboard. Phase 5 reuses the pattern: maintainer's local browser + MCP screenshots the dashboard at run-completion as part of evidence capture.

</specifics>

<deferred>
## Deferred Ideas

- **`CODE_OF_CONDUCT.md` (Contributor Covenant v2.1)** — deferred to post-v1. Solo-maintainer posture; adopt when team grows or community contributions warrant.
- **`GOVERNANCE.md` — decision-making structure** — deferred to post-v1. BDFL posture works for v1.0; revisit when there are ≥2 maintainers.
- **Mkdocs site at `tide.dev/` or GitHub Pages** — deferred to post-v1. `docs/` directory + index is sufficient for v1; site infra is scope creep.
- **External public fixture repo `github.com/jsquirrelz/tide-demo-fixture`** — explicitly rejected for v1 per user decision. Local-only git remote (D-B3) is the v1 mechanism. Revisit if community adoption surfaces friction.
- **Friend-of-project cold-read dry-run pass** — combined option from DIST-05 question 1, rejected in favor of scripted-only. Worth doing once informally before v1.0 release as a sanity check, but not a gating signal.
- **Nightly cron dry-run** — rejected in favor of tag-gate posture. Live CI on every PR/nightly is unjustified at v1 scale.
- **`docs/runbooks/` directory with per-incident runbooks** — rejected in favor of single `docs/troubleshooting.md` table for v1. Revisit if operator base grows large enough to justify deeper per-incident content.
- **GovernanceLevel-spectrum samples (Task-only / Plan / Phase)** — rejected in favor of cost-spectrum (D-B1). Pedagogical value didn't justify giving up cost-discriminator clarity.
- **Multi-vendor provider matrix in samples** — rejected for v1; the layering pattern (Phase 3 D-C1) IS the v1 commitment, additional providers are v1.x. The acceptance test authors `internal/subagent/openai/` AS the test, but doesn't ship a sample that uses it.
- **`tide.dev/` custom domain** — deferred to post-v1.
- **GitHub Pages site as primary doc surface** — deferred to post-v1; `docs/` in repo is the v1 surface.
- **Mutiple-version coverage in chart distribution** — v1.0 ships exactly one chart version; v1.x will add per-minor-version OCI tags as needed.
- **CodeQL / SARIF security scanning in release pipeline** — v1.0 ships `make lint` + `make verify-*` gates; deeper static analysis (CodeQL, Trivy on images) is a post-v1 OSS hardening step.
- **OperatorHub / OLM bundle submission** — deferred to v1.x. v1.0 ships Helm chart only.
- **CRD subchart published independently** — both charts version-bump in lockstep for v1; independent versioning is a v1.x consideration if CRD schema lifecycle diverges from controller release cadence.

</deferred>

---

*Phase: 05-distribution-self-hosting-acceptance*
*Context gathered: 2026-05-22 via /gsd-discuss-phase*
*Successor of: Phase 04.1 (Pre-v1 audit fixes + cross-phase UAT closeout) — closed 2026-05-22, audit-uat = 0.*
*Final phase of M0 → M_self bridge. Phase 5 deliverables prove TIDE-on-TIDE works end-to-end.*
