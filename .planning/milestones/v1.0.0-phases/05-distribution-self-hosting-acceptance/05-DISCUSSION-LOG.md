# Phase 5: Distribution & Self-Hosting Acceptance - Discussion Log

> **Audit trail only.** Do not use as input to planning, research, or execution agents.
> Decisions are captured in CONTEXT.md — this log preserves the alternatives considered.

**Date:** 2026-05-22
**Phase:** 05-distribution-self-hosting-acceptance
**Areas discussed:** Acceptance-test target + budget, 3 sample Projects: small / medium / large, Documentation strategy + OSS readiness, External-operator dry-run methodology

---

## Acceptance-test target + budget (BOOT-04 / DIST-05)

### Question 1 — Scope of work for v1 acceptance test

| Option | Description | Selected |
|--------|-------------|----------|
| Full Milestone — multiple Phases authored + executed | Exercises every reconciler level. Most expensive ($$$). Most faithful to spec. Riskiest. | |
| Single Phase — phase brief + PLAN.md + task execution (Recommended) | TIDE authors phase brief, then PLAN.md, dispatches Wave-N task Jobs. Bounded budget (~tens of dollars). | ✓ |
| Single Plan only — PLAN.md authored + tasks executed | Skips Milestone+Phase reconciler dispatch. Weaker acceptance signal. | |
| Walking acceptance — staged shallow then full | Two runs; double budget; double dashboard validation. | |

**User's choice:** Single Phase — phase brief + PLAN.md + task execution
**Notes:** Best fit for v1 — proves full descent without Milestone-level fan-out blowing budget on first real run.

### Question 2 — Budget ceiling + halt behavior

| Option | Description | Selected |
|--------|-------------|----------|
| Hard cap $25, no bypass — fail-fast (Recommended) | `Project.Spec.budget.costCeilingCents: 2500`. If hit, dispatch halts + condition fires + test marks BLOCKED. Cap halt IS an acceptance signal. | ✓ |
| Hard cap $50, with `tide approve --bypass-budget` allowed | Higher ceiling with slack on first run. Exercises `tide approve` annotation flow. | |
| Soft cap $100, no halt | Higher ceiling without halt machinery. Loses production-readiness proof. | |
| No budget cap — trust the test fixture is small enough | Risky; doesn't exercise FAIL-04 at all. | |

**User's choice:** Hard cap $25, no bypass — fail-fast
**Notes:** Forces TIDE's budget infra (Phase 2 FAIL-04 + Phase 04.1 P4.1) to prove itself.

### Question 3 — Pass criteria

| Option | Description | Selected |
|--------|-------------|----------|
| Artifacts on branch + Project.Status=Complete + zero errors (Recommended) | Per-run branch exists with phase brief + PLAN.md + diffs, Status=Complete, no error logs, no orphan Jobs, gitleaks passed, under budget. Concrete + machine-checkable. | ✓ |
| Pass-by-construction — Project.Status.Phase=Complete is sufficient | Trust the status field. Skips artifact-shape validation. | |
| Reviewer-judges-quality — humans rate the authored PLAN.md | Authentic but subjective; can't be CI-gated. | |
| Strict signed-state — plus dashboard renders the run live | Stronger acceptance; heavier to wire. | |

**User's choice:** Artifacts on branch + Project.Status=Complete + zero errors
**Notes:** Matches ROADMAP success criterion #4 wording.

### Question 4 — Execution model + API key source

| Option | Description | Selected |
|--------|-------------|----------|
| Maintainer-only, manual `make acceptance-v1` on dev laptop (Recommended) | Maintainer ritual, not CI. Maintainer's own API key. Attached to release notes. | ✓ |
| GitHub Actions, workflow_dispatch only, secrets-gated | Shared evidence in CI logs. Requires every merger to be trusted with secret. | |
| Both — nightly CI + manual local | Doubles signal; burns budget continuously. Premature for pre-1.0. | |
| Pre-release tag gate only | Most token-economical. Bugs surface only at last moment. | |

**User's choice:** Maintainer-only, manual `make acceptance-v1` on dev laptop
**Notes:** Live LLM cost per PR/nightly unjustified before OSS adoption proves demand.

---

## 3 sample Projects: small / medium / large (DIST-04)

### Question 1 — Spectrum the 3 samples cover

| Option | Description | Selected |
|--------|-------------|----------|
| Cost spectrum — $0 stub → ~$5 mini → ~$25 acceptance run (Recommended) | small uses stub-subagent, medium real Claude with tiny throwaway, large is acceptance run. | ✓ |
| Repo-scope spectrum — throwaway repo → sample repo → this repo | All real LLM. Authentic but loses free smoke test affordance. | |
| Level spectrum — Task-only → Plan → Phase | Demonstrates each reconciler level discretely. Pedagogical. | |
| Provider spectrum — stub → anthropic-default → multi-vendor | Multi-vendor matrix is v1.x; confuses scope. | |

**User's choice:** Cost spectrum — $0 stub → ~$5 mini → ~$25 acceptance run
**Notes:** Lets first-time operators feel TIDE before paying.

### Question 2 — Medium sample's target repo

| Option | Description | Selected |
|--------|-------------|----------|
| Dedicated fixture repo: `jsquirrelz/tide-demo-fixture` | New public repo; stable target; needs PAT or fork. | |
| Bring-your-own-repo — sample is a template | Most flexible; worst onboarding. | |
| TIDE itself, narrowly scoped | Less authentic; risk operators misread per-run branch as main. | |
| Reference an external well-known small repo | Worst stability. | |

**User's choice:** [Other / free-text] "Scaffold tide-demo-fixture in examples and copy it to make a separate repo internally."
**Clarification:** User confirmed NO external public repo. Scaffold lives at `examples/tide-demo-fixture/` inside THIS TIDE repo. Sample initializes a fresh local-only git remote on demand from that scaffold (file:// URL or temp-dir + `git init`). No third-party / external dependency.

### Question 3 — Large sample's outcome prompt

| Option | Description | Selected |
|--------|-------------|----------|
| TIDE authors a real v1.x phase — `internal/subagent/openai/` skeleton (Recommended) | Authentic v1.x work; Phase 3 D-C1 layering pattern; ~5-8 task DAG. | ✓ |
| TIDE authors a docs phase — e.g., a tutorial doc | Low risk; loses "TIDE writes Go code" proof point. | |
| TIDE authors a synthetic phase whose outcome we discard | Avoids needing output to be production-quality. | |
| TIDE authors something concrete user picks at run time | Most flexible; harder to compare runs. | |

**User's choice:** TIDE authors a real v1.x phase — "OpenAI subagent skeleton"
**Notes:** Authentic, useful, reviewable. If acceptance succeeds, the authored skeleton can optionally be merged as bonus.

### Question 4 — Sample location

| Option | Description | Selected |
|--------|-------------|----------|
| `examples/projects/{small,medium,large}/` (Recommended) | OSS convention; operator-discoverable. Phase 1's `config/samples/` stays as kubebuilder fixture. | ✓ |
| `config/samples/projects/{small,medium,large}/` (kubebuilder) | Kubebuilder default; less discoverable. | |
| Both — canonical + mirrored | Doubles file count; symlinks fragile. | |
| `docs/samples/{small,medium,large}/` | Mixes YAML with Markdown; unusual. | |

**User's choice:** `examples/projects/{small,medium,large}/`
**Notes:** README quickstart reads `kubectl apply -f examples/projects/small/project.yaml`.

---

## Documentation strategy + OSS readiness (DIST-04 + Pitfall 24)

### Question 1 — Doc organization

| Option | Description | Selected |
|--------|-------------|----------|
| README is the spec; `docs/INSTALL.md` is the on-ramp (Recommended) | README stays paradigm spec. New docs at `docs/`. Index in reader-journey order. | ✓ |
| Mkdocs site published to GitHub Pages | Better discoverability; scope creep for v1. | |
| Single big NOTES.md / WIKI.md, no `docs/` directory | Hostile to first-time readers; past this point already. | |
| Move docs to `.github/` directory | Trades discoverability for GitHub UI surface. Unusual. | |

**User's choice:** README is the spec; `docs/INSTALL.md` is the on-ramp

### Question 2 — README Quickstart placement

| Option | Description | Selected |
|--------|-------------|----------|
| Add ~30-line Quickstart at TOP of README, spec below (Recommended) | First-time readers get "try it" before "theory". Spec content untouched. | ✓ |
| Add the Quickstart at the BOTTOM of README | Spec stays opening face; readers bail at fold. | |
| Don't touch README; quickstart entirely in docs/INSTALL.md | Cleanest separation; worst onboarding. | |
| Restructure README into "Try TIDE" + "How TIDE thinks" | Major rewrite; highest churn risk. | |

**User's choice:** ~30-line Quickstart at TOP of README
**Notes:** Quickstart is 4 copy-pasteable commands + expected output + ~3 links.

### Question 3 — Troubleshooting depth

| Option | Description | Selected |
|--------|-------------|----------|
| Symptom → Cause → Recipe table, ~12 entries (Recommended) | Scan-friendly table for ops in incident. Each row links deeper if applicable. | ✓ |
| Decision tree by status (top-down) | Stronger for first-time ops; doubles word count; worse for experienced ops. | |
| Minimal — just finalizer + kubectl-patch recipe + "open an issue" | Matches ROADMAP wording verbatim; loses OSS-readiness. | |
| Runbook-per-incident in separate `docs/runbooks/` directory | Most production-grade; highest churn. | |

**User's choice:** Symptom → Cause → Recipe table, ~12 entries

### Question 4 — Contributor OSS docs in v1

| Option | Description | Selected |
|--------|-------------|----------|
| CONTRIBUTING.md — dev env setup, tests, PRs | K8s ecosystem default. Pitfall 24 mitigation. | ✓ |
| CODE_OF_CONDUCT.md — Contributor Covenant v2.1 | CNCF default. Adopted by most projects. | |
| GOVERNANCE.md — who decides, how decisions made | Premature for solo-maintainer v1. | |
| SECURITY.md — how to report a vulnerability | Recommended for K8s operators; renders as "Security policy" link. | ✓ |

**User's choice:** CONTRIBUTING.md + SECURITY.md
**Notes:** CODE_OF_CONDUCT + GOVERNANCE deferred to post-v1.

---

## External-operator dry-run methodology (DIST-05)

### Question 1 — Simulation approach

| Option | Description | Selected |
|--------|-------------|----------|
| Docker-in-Docker scripted dry-run from clean image (Recommended) | `make dry-run-v1` spins fresh ubuntu:24.04, runs Quickstart verbatim. Reproducible, cheap, CI-runnable. | ✓ |
| Friend-of-project runs it cold on their laptop | Authentic friction signal; single data point; hard to repeat. | |
| GitHub Codespaces with TIDE pre-cloned | Skips git clone step; ties test to Codespaces availability. | |
| Multiple methods — scripted + human cold-read | Strongest; doubles effort. | |

**User's choice:** Docker-in-Docker scripted dry-run from clean image

### Question 2 — Timer stop point

| Option | Description | Selected |
|--------|-------------|----------|
| Small-sample Project reaches Status=Complete (Recommended) | Uses stub-subagent ($0). Proves clone + install + CRDs + reconcile + dispatch + status convergence. | ✓ |
| Controller + dashboard Deployments Ready, CRDs registered | Stops earlier; misses use check. | |
| Controller Ready + sample admitted by webhook | Middle ground; no end-to-end. | |
| Medium-sample (~$5) reaches Status=Complete | Costs $5 every dry-run; prohibitive. | |

**User's choice:** Small-sample Project reaches Status=Complete
**Notes:** No LLM cost — safely repeatable in CI.

### Question 3 — CI/release pipeline placement

| Option | Description | Selected |
|--------|-------------|----------|
| Release-candidate tag gate — fires on `v*-rc.*` tag (Recommended) | Blocks goreleaser if fails or wall-clock >30 min. Cheap, high signal. | ✓ |
| Nightly cron on main + maintainer-runnable local | Costs CI minutes nightly (~8-12 min). | |
| Every PR — always | Adds ~10 min to every PR's wall-clock; too expensive. | |
| Maintainer ritual only — not in CI | Cheapest; lowest signal. | |

**User's choice:** Release-candidate tag gate — fires on `v*-rc.*` tag push
**Notes:** Mirrors live-e2e.yml posture from Phase 04.1 P2.4 (no `schedule:` cron).

### Question 4 — Evidence artifact

| Option | Description | Selected |
|--------|-------------|----------|
| Transcript + timing JSON in GitHub Release (Recommended) | stdout/stderr transcript + `dry-run-report.json` upload as Release assets. Operators see proof. | ✓ |
| Just CI status — green job is the evidence | Rots after 90 days. Sufficient for maintainer, weak for OSS readers. | |
| Commit transcript into `docs/dist-validation/v1.x-dry-run.md` | Permanent record; bloats repo history. | |
| Publish to status page (GitHub Pages site) | Most polished; out of scope for v1. | |

**User's choice:** Transcript + timing JSON in GitHub Release
**Notes:** `dry-run-report.json` includes per-phase elapsed seconds. Schema-versioned for forward compat.

---

## Claude's Discretion

Areas where Claude/researcher/planner has flexibility:

- **LICENSE copyright holder name** — chose `The TIDE Authors` to match existing Go file headers; researcher verifies no `NOTICE` file required by Apache-2.0 deps in `go.sum`.
- **`helmify` integration into `release.yaml`** — verify-only vs regenerate-and-commit; lean verify-only (charts are hand-tuned).
- **Chart version bump cadence** — lockstep 0.1.0-dev → 1.0.0 for both `tide` + `tide-crds`; independent versioning is v1.x consideration.
- **OCI vs `index.yaml` chart distribution** — both ship in v1, OCI is primary; README Quickstart shows OCI command.
- **Conversion webhook stays no-op** — CRD-05 scaffold from Phase 1 stays as shipped; v1.x `v1beta1` activates.
- **Local-only git remote bootstrap mechanism for medium sample** — Job vs CLI binary vs ConfigMap; lean toward a small `cmd/tide-demo-init/` CLI binary (explicit step, matches stateless CLI affordance).
- **Outcome prompt phrasing for large sample** — researcher prototypes 2-3 variants and recommends one balancing specificity vs LLM-wander.
- **`per-namespace-rolebinding.yaml` template structure** — Helm range over `.Values.projectNamespaces`; empty default.
- **AUTH-02 docs** — `docs/rbac.md` covers per-Kind verbs + RoleBinding template + conversion webhook RBAC.

## Deferred Ideas

Captured for future phases / post-v1:

- `CODE_OF_CONDUCT.md` — Contributor Covenant v2.1 (post-v1).
- `GOVERNANCE.md` — decision-making structure (post-v1; ≥2 maintainers).
- Mkdocs site at `tide.dev/` or GitHub Pages (post-v1).
- External public fixture repo `github.com/jsquirrelz/tide-demo-fixture` (rejected for v1; revisit if community adoption surfaces friction).
- Friend-of-project cold-read dry-run pass (informal sanity check, not gating).
- Nightly cron dry-run (rejected for v1 cost; revisit if regressions surface).
- `docs/runbooks/` directory (rejected for v1 single-table approach; revisit if operator base grows).
- Level-spectrum samples (Task-only / Plan / Phase) — rejected in favor of cost spectrum.
- Multi-vendor provider matrix in samples — v1.x community work.
- `tide.dev/` custom domain (post-v1).
- CodeQL / SARIF security scanning in release pipeline (post-v1 OSS hardening).
- OperatorHub / OLM bundle submission (v1.x).
- Independent CRD subchart versioning (v1.x if schema lifecycle diverges).
