# Phase 36: Signed Commits + Bot Identity (descoped to identity-only) - Research

**Researched:** 2026-07-04
**Domain:** Go/K8s internal plumbing — CRD field addition, env-var precedence chain, Helm chart config surface (no new external dependencies)
**Confidence:** HIGH (all findings verified directly against the codebase in this session)

<user_constraints>
## User Constraints (from CONTEXT.md)

### Locked Decisions

#### Scope: signing deferred (headline decision)
- **D-01:** Phase 36 is descoped to SIGN-01 only. SIGN-02 (opt-in GPG signing at all three sites), SIGN-03 (first-reconcile key validation), and SIGN-04 (Verified-badge operator docs) are deferred out of v1.0.7 entirely. Rationale: the functional payoff — repos with require-signed-commits branch protection — is hypothetical today (the first external run surfaced unverified badges as an annoyance, not a block); the cost is real (gpg-shim/plumbing spike, signing-oracle key-exposure design, manual external UAT); and signing is a leaf feature — once Phase 34 stabilizes the commit sites, deferring carries no compounding penalty.
- **D-02:** The intermediate slices were considered and rejected: controller-sites-only signing pays the spike cost but still fails branch protection (task commits are reachable from the run branch); boundary-commit-only signing avoids the spike but delivers almost nothing. If signing returns, it returns as full three-site signing.

#### Identity configuration surface
- **D-03:** New Project CRD fields `spec.git.agentName` / `spec.git.agentEmail` (in `GitConfig`), with precedence **Project spec → chart value → compiled-in default** — matching the validated image-resolution chain pattern (`Levels.<level>.Image` → `Spec.Subagent.Image` → helm default, v1.0.1).
- **D-04:** The bot→agent rename applies **everywhere**: env vars become `TIDE_AGENT_NAME` / `TIDE_AGENT_EMAIL` (the `TIDE_BOT_*` names are read in harness + integrate but set by nothing today — the rename is free), chart values use agent naming (e.g. `agent.name` / `agent.email`), and the compiled-in default identity becomes **`TIDE Agent <tide-agent@tideproject.k8s>`**. Accepted consequence: unconfigured installs see a one-time committer-identity change on new commits.
- **D-05:** tide-push's hardcoded `tideBotSignature()` is replaced by the same env-sourced identity chain. The W11 stability contract (name+email stable across runs; only timestamp varies) is preserved: values come from install/Project config, not per-run state.
- **D-06:** The CRD change batches with Phase 35's `baseRef` CRD change into **one chart version bump** (values.yaml is the FIXED contract; binary catches up to chart, never reverse).

#### Dependencies (revised by descope)
- **D-07:** The original Phase 34 dependency was signing-specific (signing touching just-stabilized merge code). Identity-only work merely renames/sets env reads at those sites — Phase 36 no longer meaningfully depends on Phase 34. The Phase 35 sequencing stays (chart bumps batch).

### Claude's Discretion
- Exact chart value shape/nesting for the agent identity (must not collide with the existing `signingKey` HMAC value — see code insights).
- Whether/what CEL validation the new CRD fields get (e.g. email format).
- Docs placement for the identity-config note (likely wherever GitConfig/creds are already documented).

### Deferred Ideas (OUT OF SCOPE)

#### GPG signing (SIGN-02/03/04) — deferred out of v1.0.7 (preserve this analysis)
- **Requirements deferred:** SIGN-02 (opt-in signing-key Secret ref, signed commits at all three sites incl. integrate merges), SIGN-03 (first-reconcile key validation: armored / no passphrase / email-match triple, clear failure condition), SIGN-04 (machine-account + keygen + public-key-upload docs recipe for GitHub/GitLab/Gitea).
- **The spike (was `research: true`):** go-git v5.19 cannot sign three-way merges via `SignKey`; `git merge --no-commit` + go-git commit silently flattens merge topology. Harness + integrate commit via the git CLI; only tide-push uses go-git. Any signing that covers integrate merge commits needs a **gpg-shim** (pure-Go `gpg.program` stand-in) or **plumbing-level** commit construction — spike before planning.
- **Key-exposure analysis (the ASK-FIRST decision, resolved by descope):** mounting the private key in the subagent pod makes it a signing oracle — LLM-executed code can read/exfiltrate it via prompt injection. Options analyzed: (A) controller-sites-only — weak middle: pays the spike cost yet still fails require-signed-commits branch protection because unsigned task commits are reachable from the run branch; (B) restructure the task commit out of the LLM pod — security-ideal but highest code risk (relocates `CommitWorktree`: envelope HeadSHA reporting, empty-diff→task-fail semantics); (C) mount-everywhere with documented risk + a dedicated TIDE-only machine-account key (bounded blast radius, easy revocation, optional gitleaks rule for armored-key blocks) — the coherent full-value shape. If signing returns, it likely returns as (C).
- **Implementation notes:** `ProtonMail/go-crypto` is already an indirect dep (promote when signing lands); no gpg binary in images; future Secret ref name must avoid the chart's `signingKey` HMAC collision (e.g. `git.signingKeySecretRef`); portable GPG only — no GitHub-API auto-sign (violates the no-hard-coded-git-host constraint).
- **UAT is external:** `git verify-commit` in-cluster does not prove the badge; needs one manual push to a real GitHub repo including an integrate merge commit.

#### Reviewed Todos (not folded)
- 8 other keyword matches from the pending-todo scan are tagged to their own phases (34: wave-parallel integration miss; 35: git baseRef; 37: dashboard log drawer, artifact view; 38: pricing table, Prometheus setup) or explicitly deferred (`subagent.levels` rename — own milestone; CACHE-F1 — vNext). None belong here.
</user_constraints>

<phase_requirements>
## Phase Requirements

| ID | Description | Research Support |
|----|-------------|------------------|
| SIGN-01 | TIDE agent identity (name/email) uniformly configurable across all three commit sites — harness, integrate, tide-push — via `spec.git.agentName`/`agentEmail` → chart value → compiled-in default precedence (tide-push hardcoded identity removed; agent terminology replaces bot everywhere, incl. compiled-in default `TIDE Agent <tide-agent@tideproject.k8s>`) | All three sites located and read (§Commit-Site Inventory). Injection surfaces mapped (§Env Injection Surfaces — subagent Job + push Job; neither sets identity env today). Precedence pattern to mirror verified (`resolveImage`, `ProviderDefaults`). Chart wiring path verified (hack/helm canonical sources → `make helm-controller`). CRD dual-version shape verified (v1alpha1 served:false; type-parity only, no conversion webhook exists). |
</phase_requirements>

## Project Constraints (from CLAUDE.md)

- **`charts/tide/values.yaml` is a FIXED contract** — chart schema/values additions ride a version bump; binary catches up to chart, never reverse. Phase 36's chart additions batch with Phase 35's into ONE version bump (D-06).
- **Chart sources are generated**: `make helm-controller` regenerates `charts/tide/` via helmify then re-applies augmentations from `hack/helm/` — edits must land in `hack/helm/tide-values.yaml`, `hack/helm/augment-tide-chart.sh`, `hack/helm/tide-chart.yaml`, never directly in `charts/tide/` [VERIFIED: Makefile:705-715, hack/helm/augment-tide-chart.sh].
- **`make test-int` exit ≠ Ginkgo green** — the kind package bundles plain go-tests (helm-template contract tests) beside Ginkgo specs; always read `MAKE_EXIT` and grep `^--- FAIL`.
- **No hard-coded git host / LLM provider / auth model** — agent identity is host-agnostic config; do not add any GitHub-specific behavior.
- **Verify before claiming**: run the tests, read the output; a subagent's "pre-existing" dismissal is a claim, not verification.
- **GSD workflow enforcement**: production edits route through GSD plans (this phase's plans).
- **Don't vendor GSD Markdown; match existing code conventions** (comment density in this repo is high and decision-referenced — e.g. `(D-03)`, `(W11)` tags; new code should carry the same style).
- **Lint gate includes import firewalls**: `make lint` runs golangci-lint + `verify-dag-imports` + `verify-dispatch-imports` + `verify-import-firewall` [VERIFIED: Makefile:266] — new cross-package imports must pass these.

## Summary

Phase 36 is a pure internal-plumbing phase: rename two env vars, unify three compiled-in defaults into one, add two optional CRD string fields, add two chart values, and — the only genuinely new mechanism — actually **inject** the identity env vars into the two Job pod specs where the three commit binaries run (nothing sets `TIDE_BOT_*` today; the env-read machinery has been dead config since v1.0.0 plan 11-01).

The three commit sites split across two pod types: the **subagent Job** runs the harness (`CommitWorktree`, task commits), and the **push Job** runs `tide-push`, which itself calls `pkg/git.IntegrateTaskBranches` (integrate merge commits) before its own boundary commit. So two env-injection surfaces cover all three sites: `internal/dispatch/podjob/jobspec.go` (subagent container env) and `internal/controller/push_helpers.go` `buildPushJob` (which today has `EnvFrom` only, no `Env` block). The clone Job creates no commits and needs nothing.

Precedence resolution should mirror the validated `resolveImage` chain exactly: a controller-side resolver walks `project.Spec.Git.AgentName` → helm default → compiled-in constant, and the resolved final value is stamped into Job env. The helm default reaches every Job-building reconciler for free by extending the existing `ProviderDefaults` struct (already threaded to five reconcilers — Project/Milestone/Phase/Plan/Task — as `HelmProviderDefaults`; Wave and Import build no Jobs and don't carry the field). The compiled-in default lives once, as exported constants in `pkg/git`, imported by all three binaries plus the manager — fixing the existing latent inconsistency where tide-push defaults to `tide-bot` (lowercase) while harness/integrate default to `TIDE Bot`.

**Primary recommendation:** One shared identity source in `pkg/git` (constants + env-read helper), controller-side resolution mirroring `resolveImage`, unconditional injection of resolved values into subagent-Job and push-Job env, chart values `agent.name`/`agent.email` → manager env `TIDE_AGENT_NAME`/`TIDE_AGENT_EMAIL` via the augment script, CRD fields in both API type packages with kubebuilder Pattern validation forbidding commit-header-corrupting characters.

## Architectural Responsibility Map

| Capability | Primary Tier | Secondary Tier | Rationale |
|------------|-------------|----------------|-----------|
| CRD fields `spec.git.agentName`/`agentEmail` | API types (`api/v1alpha1` + `api/v1alpha2`) | CRD manifests (`config/crd/bases` → `charts/tide-crds`) | Schema is the operator-facing contract; v1alpha2 is storage, v1alpha1 kept for type parity (served:false) |
| Precedence resolution (spec → chart → compiled) | Controller (`internal/controller`) | podjob backend (inline mirror, fixture path) | Controller is the only place that sees Project spec + helm defaults together; mirrors `resolveImage` precedent |
| Chart default transport (values → manager) | Helm chart (`hack/helm/` sources) | Manager env parsing (`cmd/manager/env.go`) | Same channel as `TIDE_DEFAULT_MODEL_*` / `CLAUDE_SUBAGENT_IMAGE` |
| Env injection into commit-site pods | Job builders (`jobspec.go`, `push_helpers.go`) | — | Pod env is the only channel into the in-Job binaries; today NOTHING injects identity |
| Commit identity application | In-Job binaries (harness, pkg/git integrate, tide-push) | — | Already read env with fallback; rename reads + swap defaults |
| Compiled-in default constant | Shared library (`pkg/git`) | — | Importable by all three binaries + manager without cycles (`internal/controller` → `pkg/git` OK; podjob → `pkg/git` OK) |
| Operator docs | `docs/project-authoring.md` | chart values comments | GitConfig table already documented there (line 49) |

## Standard Stack

### Core

No new libraries. The phase uses only what is already in `go.mod`:

| Library | Version | Purpose | Why Standard |
|---------|---------|---------|--------------|
| (stdlib) `os`, `os/exec` | Go 1.26.0 | env reads, git CLI invocation | Already the mechanism at all three sites [VERIFIED: internal/harness/commit.go, pkg/git/integrate.go] |
| go-git/v5 `object.Signature` | existing dep | tide-push boundary commit author | Already used; only the signature values change [VERIFIED: cmd/tide-push/main.go:131-137] |
| controller-gen / kubebuilder markers | existing toolchain | CRD field validation + deepcopy regen | `make generate manifests` pipeline exists [VERIFIED: Makefile:52,66] |

### Alternatives Considered

| Instead of | Could Use | Tradeoff |
|------------|-----------|----------|
| kubebuilder `Pattern` markers | CEL `x-kubernetes-validations` | Codebase precedent is `Pattern` (`RepoURL` at project_types.go:211); CEL adds nothing for a per-field regex — use Pattern |
| Extending `ProviderDefaults` | New `HelmAgentIdentity` struct threaded to five reconcilers | ProviderDefaults is already threaded everywhere it's needed (`HelmProviderDefaults` field on the five Project/Milestone/Phase/Plan/Task reconcilers; Wave and Import build no Jobs) — extension is zero new plumbing |
| Unconditional Job-env injection of resolved value | Conditional injection (PricingOverridesJSON pattern) | Resolved value is never empty (compiled default backstops), so unconditional is simpler and makes the pod spec self-documenting |

**Installation:** none — no packages installed.

## Package Legitimacy Audit

No external packages are installed by this phase. All work uses the existing Go module graph and toolchain.

**Packages removed due to [SLOP] verdict:** none
**Packages flagged as suspicious [SUS]:** none

## Architecture Patterns

### System Architecture Diagram — identity flow (target state)

```
                       ┌──────────────────────────────────────────────┐
 Operator config       │  Precedence (resolved in controller):        │
                       │  spec.git.agentName/Email                    │
 Project CRD ────────► │    → chart agent.name/email                  │
 (spec.git.agent*)     │      → compiled default                      │
                       │        "TIDE Agent <tide-agent@...k8s>"      │
 Helm values           └──────────────┬───────────────────────────────┘
 agent.name/email                     │ resolveAgentIdentity(project, helmDefaults)
   │ (augment script)                 │
   ▼                                  ├──────────────────────────────┐
 manager Deployment env               ▼                              ▼
 TIDE_AGENT_NAME/EMAIL      subagent Job env                 push Job env
   │ envOrDefault("")       TIDE_AGENT_NAME/EMAIL            TIDE_AGENT_NAME/EMAIL
   ▼                        (jobspec.go subagentEnv)         (buildPushJob — new Env block)
 ProviderDefaults                     │                              │
 .AgentName/.AgentEmail               ▼                              ▼
 (threaded to all reconcilers)  harness CommitWorktree     tide-push runPush
                                (task commits,             ├─ pkg/git.IntegrateTaskBranches
                                 git CLI -c user.*)        │  (merge commits, git CLI -c user.*)
                                                           └─ pkg/git.Commit(sig)
                                                              (boundary commit, go-git Signature)
```

All three commit sites read `TIDE_AGENT_NAME`/`TIDE_AGENT_EMAIL` from pod env with the shared compiled-in fallback — the env fallback is only exercised in tests/non-K8s runs because the controller always injects a resolved value.

### Commit-Site Inventory (verified)

| # | Site | File:Lines | Mechanism | Current identity source |
|---|------|-----------|-----------|------------------------|
| 1 | Harness task commit | `internal/harness/commit.go:56-72` | git CLI `-c user.name -c user.email commit` | `TIDE_BOT_NAME`/`TIDE_BOT_EMAIL` env, fallback `TIDE Bot <tide-bot@tideproject.k8s>` |
| 2 | Integrate merge commits | `pkg/git/integrate.go:85-101` | git CLI `-c user.* merge --no-ff` | Same env reads, same fallback |
| 3 | tide-push boundary commit | `cmd/tide-push/main.go:131-137, 521` | go-git `pkggit.Commit(wt, msg, tideBotSignature())` | **Hardcoded** `tide-bot <tide-bot@tideproject.k8s>` — note lowercase `tide-bot`, INCONSISTENT with sites 1-2 |

Where they run: site 1 runs in the **subagent Job** pod (claude-subagent image); sites 2 and 3 both run in the **push Job** pod (`tide-push` binary calls `IntegrateTaskBranches` in-process at main.go:361) [VERIFIED: cmd/tide-push/main.go:359-380]. The clone Job (same image, `--mode=clone`) creates no commits — `EnsureRunBranch` only creates a ref [VERIFIED: cmd/tide-push/main.go:259-326].

`cmd/tide-demo-init` has its own deliberately distinct seeding identity ("Distinct from cmd/tide-push's tide-bot" — main.go:135) and reads no `TIDE_BOT_*` env — **out of scope, do not rename** [VERIFIED: grep].

### Env Injection Surfaces (verified — the net-new work)

Nothing sets `TIDE_BOT_*` today: grep across all `.go`/`.yaml`/`.tpl` finds only the two readers, tests, and planning docs [VERIFIED: repo-wide grep]. The injection surfaces:

1. **Subagent Job**: `internal/dispatch/podjob/jobspec.go:357-362` `subagentEnv` — add `TIDE_AGENT_NAME`/`TIDE_AGENT_EMAIL` from new `BuildOptions` fields. `BuildOptions` construction sites that must populate them: `task_controller.go:762` and `:1414` (executor paths), `project_controller.go:1245`, `milestone_controller.go:486`, `phase_controller.go` and `plan_controller.go` planner paths, and `podjob/backend.go:284` (`Run`, fixture path — mirrors the inline image walk at backend.go:267-277). Planner subagents never commit, but uniform injection is simpler and harmless.
2. **Push Job**: `internal/controller/push_helpers.go` `buildPushJob` (line 144) — container currently has `EnvFrom` (creds Secret) and **no `Env` block** [VERIFIED: push_helpers.go:234-259]. Add `Env` with the two vars from new `PushOptions` fields. Sole construction choke point: `boundary_push.go` `triggerBoundaryPush` (PushOptions built at lines 116 and 168), which has `project` in scope and is called from Milestone/Phase/Plan reconcilers (lines 201/206/222) — all of which already carry `HelmProviderDefaults` or can reach it.
3. **Clone Job** (`buildCloneJob`): no identity env needed (no commits).

### Pattern 1: Precedence resolution — mirror `resolveImage`

**What:** Controller-side pure function walking Project spec → helm default → compiled default.
**When to use:** at every Job-build call site, immediately before stamping env.
**Example (shape, adapted from the verified pattern):**

```go
// Source: mirrors internal/controller/dispatch_helpers.go:269-291 (resolveImage)
// resolveAgentIdentity walks the D-03 precedence chain:
//   Spec.Git.AgentName → helmDefaults.AgentName → pkggit.DefaultAgentName
func resolveAgentIdentity(project *tideprojectv1alpha2.Project, helmDefaults ProviderDefaults) (name, email string) {
	name, email = pkggit.DefaultAgentName, pkggit.DefaultAgentEmail
	if helmDefaults.AgentName != "" {
		name = helmDefaults.AgentName
	}
	if helmDefaults.AgentEmail != "" {
		email = helmDefaults.AgentEmail
	}
	if project != nil && project.Spec.Git != nil { // Spec.Git is *GitConfig — nil-check required
		if project.Spec.Git.AgentName != "" {
			name = project.Spec.Git.AgentName
		}
		if project.Spec.Git.AgentEmail != "" {
			email = project.Spec.Git.AgentEmail
		}
	}
	return name, email
}
```

Extend `ProviderDefaults` (dispatch_helpers.go:114) with `AgentName`/`AgentEmail`, populated in `cmd/manager/env.go` `tideHelmProviderDefaults` via `envOrDefault("TIDE_AGENT_NAME", "")` — empty string means "chart tier not configured", falling through to the compiled default at resolve time (this is exactly the documented `envOrDefault` empty-is-unset convention, env.go:34-42).

**Import-direction constraint:** `internal/controller` imports `internal/dispatch/podjob`; podjob must NOT import controller. The shared compiled-in constants therefore live in `pkg/git` (already imported by cmd/tide-push; adding it to harness/controller/podjob is a leaf import). The podjob backend's `Run` mirrors the walk inline, exactly as it already does for images (backend.go:267 comment: "Inline image precedence walk — mirrors controller.resolveImage").

### Pattern 2: Shared compiled-in default + env-read helper

**What:** Single source of truth for the default identity and the env-var names.
**Example:**

```go
// pkg/git/identity.go (new)
const (
	DefaultAgentName  = "TIDE Agent"
	DefaultAgentEmail = "tide-agent@tideproject.k8s"
	EnvAgentName      = "TIDE_AGENT_NAME"
	EnvAgentEmail     = "TIDE_AGENT_EMAIL"
)

// AgentIdentity returns the commit identity from TIDE_AGENT_NAME/EMAIL env,
// falling back to the compiled-in default (D-04). All three commit sites —
// harness CommitWorktree, IntegrateTaskBranches, tide-push — call this so
// the default lives exactly once.
func AgentIdentity() (name, email string) { ... }
```

Sites 1-2 replace their duplicated `os.Getenv` blocks with this helper; site 3 replaces `tideBotSignature()` with a signature built from it (`object.Signature{Name: name, Email: email, When: time.Now()}` — the W11 comment contract "name+email stable across runs; only timestamp varies" must be preserved on the new function).

### Pattern 3: Chart value → manager env via the augment script

Chart env additions go in `hack/helm/augment-tide-chart.sh` (the ENV3 block precedent at lines 229-275 shows the exact mechanism: a marker-guarded heredoc inserted before `envFrom:` on the manager container). Values go in `hack/helm/tide-values.yaml` (the canonical source that `make helm-controller` copies over helmify's output). Recommended value shape (Claude's discretion, resolved):

```yaml
# hack/helm/tide-values.yaml — agent commit identity (Phase 36 / SIGN-01).
# Precedence: Project.spec.git.agentName/agentEmail → these values → compiled-in
# default "TIDE Agent <tide-agent@tideproject.k8s>". Empty = use compiled default.
agent:
  name: ""
  email: ""
```

Rendered env (new marker section in the augment script, e.g. `# phase36-agent-env-injected`):

```yaml
- name: TIDE_AGENT_NAME
  value: "{{ .Values.agent.name }}"
- name: TIDE_AGENT_EMAIL
  value: "{{ .Values.agent.email }}"
```

No collision: the existing `signingKey.secretName` (HMAC envelope-signing, values.yaml:274) is untouched and namespaced differently. Alternatives rejected: `subagent.agent.*` (identity applies to the push Job too, not just subagents); a top-level `git.*` block (no such block exists; `gitleaks` is separate).

### Recommended change map (files → work)

```
pkg/git/identity.go                        # NEW — constants + AgentIdentity() helper
internal/harness/commit.go                 # rename env reads → helper; update doc comment (lines 32-34)
pkg/git/integrate.go                       # same (lines 56-59, 85-92)
cmd/tide-push/main.go                      # replace tideBotSignature() (131-137, 521); update pkg doc (32-34)
api/v1alpha2/project_types.go              # GitConfig + AgentName/AgentEmail (+optional, Pattern, MaxLength)
api/v1alpha1/project_types.go              # identical fields (type parity; served:false)
internal/controller/dispatch_helpers.go    # ProviderDefaults + AgentName/AgentEmail; resolveAgentIdentity()
internal/controller/push_helpers.go        # PushOptions + fields; buildPushJob Env block
internal/controller/boundary_push.go       # thread resolved identity into PushOptions (lines 116, 168)
internal/controller/{task,project,milestone,phase,plan}_controller.go  # populate BuildOptions identity fields
internal/dispatch/podjob/jobspec.go        # BuildOptions + fields; subagentEnv injection
internal/dispatch/podjob/backend.go        # inline mirror walk in Run (like image walk at :267)
cmd/manager/env.go                         # envOrDefault reads → tideHelmProviderDefaults
hack/helm/tide-values.yaml                 # agent.name/agent.email block
hack/helm/augment-tide-chart.sh            # TIDE_AGENT_NAME/EMAIL env injection (new marker)
hack/helm/tide-chart.yaml                  # version bump — BATCHED with Phase 35 (check before bumping)
docs/project-authoring.md                  # GitConfig table rows + routable-email note
(regen) make generate manifests helm-controller helm-crds  # deepcopy, CRD bases, charts/
```

### Anti-Patterns to Avoid

- **Editing `charts/tide/` directly:** helmify regenerates it; `make verify-chart-reproducible` fails CI on drift. Canonical sources are `hack/helm/` [VERIFIED: Makefile:705-715].
- **Injecting only where commits happen "today":** the env-with-fallback design means a missed injection surface silently commits with the compiled default — it *looks* correct on an unconfigured install and only fails when an operator configures a custom identity. Builder unit tests must assert env presence.
- **Preserving either old default for "compatibility":** the three sites disagree today (`tide-bot` vs `TIDE Bot`); success criterion 2 demands one default. D-04 explicitly accepts the one-time identity change.
- **Deriving identity from per-run state:** violates the W11 stability contract (D-05). Values come only from spec/chart/compiled config.
- **Adding a conversion webhook for the v1alpha1 field:** none exists in the codebase (grep found no ConvertTo/ConvertFrom/Hub in api/); v1alpha1 is `served: false, storage: false` [VERIFIED: config/crd/bases/tideproject.k8s_projects.yaml:548-549]. Follow whatever dual-version pattern Phase 35 establishes for `baseRef` — type parity + schema tests, no webhook.

## Don't Hand-Roll

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|-------------|-----|
| Email format validation | RFC 5322 regex or a validation lib | kubebuilder `Pattern` marker with a minimal `x@y` shape | Git itself doesn't validate emails; the CRD gate only needs to reject commit-header-corrupting input, matching the `RepoURL` Pattern precedent |
| Config precedence framework | Generic layered-config package | The existing 3-line walk (`resolveImage` shape) | Two fields; the codebase pattern is validated and self-documenting |
| Chart env templating | New helper template | Augment-script heredoc block (ENV3 precedent) | The marker-guarded insertion is the established idempotent mechanism |

**Key insight:** every mechanism this phase needs already exists in validated form somewhere in the repo — the work is wiring, renaming, and unifying, not inventing.

## Runtime State Inventory

This is a rename phase (`TIDE_BOT_*` → `TIDE_AGENT_*`, `tide-bot`/`TIDE Bot` → `TIDE Agent`). All five categories answered explicitly:

| Category | Items Found | Action Required |
|----------|-------------|------------------|
| Stored data | Existing commits in PVC bare repos and on pushed remote branches carry the old committer identities (`tide-bot <tide-bot@...>` from boundary/merge commits, `TIDE Bot <tide-bot@...>` from task commits). Git history is immutable. | **None** — D-04 explicitly accepts the one-time identity change on *new* commits; historical commits keep old identity. No migration. |
| Live service config | Nothing sets `TIDE_BOT_*` anywhere — no chart template, no Job builder, no Deployment env renders them (verified by repo-wide grep; only readers + tests exist). Deployed clusters (operator's minikube per MEMORY.md) have no identity env to migrate. | **None** — the rename is free at runtime, exactly as D-04 states. Running clusters pick up the new identity on next image upgrade. |
| OS-registered state | None. K8s-only. Jobs carry `TTLSecondsAfterFinished: 300` [VERIFIED: push_helpers.go:212] — no long-lived Job objects embed the old env names. | None — verified by grep + Job TTL. |
| Secrets/env vars | `GIT_PAT` (creds Secret) and `TIDE_SIGNING_KEY` (HMAC Secret `tide-signing-key`) are unrelated and unchanged. No secret key names contain "bot". | None — verified by grep of chart templates + secret refs. |
| Build artifacts | Compiled defaults live in three images: tide-controller (manager resolution), tide-claude-subagent (harness), tide-push (integrate + boundary). Kind-loaded test images go stale after the change. | Rebuild via existing `make test-int-kind-prep` (builds + loads all images); production installs pick up defaults at next chart/image upgrade. |

## Common Pitfalls

### Pitfall 1: Editing generated chart files
**What goes wrong:** Changes to `charts/tide/values.yaml` or `templates/deployment.yaml` vanish on the next `make helm-controller`.
**Why it happens:** helmify regenerates the chart; `hack/helm/augment-tide-chart.sh` re-applies augmentations from `hack/helm/` sources.
**How to avoid:** Edit `hack/helm/tide-values.yaml` + `augment-tide-chart.sh` + `tide-chart.yaml`, then run `make helm-controller` and commit the regenerated `charts/` output together.
**Warning signs:** `make verify-chart-reproducible` (CI helm-lint gate) reports drift.

### Pitfall 2: The three sites do NOT agree today
**What goes wrong:** Tests or assumptions that the current default is uniform. tide-push hardcodes `tide-bot` (lowercase, main.go:133); harness/integrate default to `TIDE Bot` (capitalized). Existing tests pin both: `cmd/tide-push/main_test.go:869-873` asserts `tide-bot`; `internal/harness/commit_test.go:98-109` uses `TIDE_BOT_*`.
**How to avoid:** Update all pinned-identity test assertions to `TIDE Agent` / `tide-agent@tideproject.k8s` in the same task that changes the site, and add a cross-site consistency test (all three constants come from `pkg/git`, so a single-source test suffices).
**Warning signs:** grep for `tide-bot\|TIDE Bot\|TIDE_BOT` returning any hit outside `.planning/` and `cmd/tide-demo-init` after the phase.

### Pitfall 3: Silent fallback masks missed injection
**What goes wrong:** Forgetting one Job-env surface (there are ~7 `BuildOptions` construction sites plus `buildPushJob`) is invisible on an unconfigured install — every binary falls back to the same compiled default, so all smoke tests pass; only a *configured* identity reveals the gap at the missed site.
**How to avoid:** Unit tests on both builders asserting `TIDE_AGENT_NAME`/`TIDE_AGENT_EMAIL` presence in the rendered pod spec (follow the `TIDE_PRICING_OVERRIDES_JSON` transport-test pattern at jobspec_test.go:828+). Success criterion 1 verification must use a **non-default** configured identity.
**Warning signs:** an env-presence test only on one of the two Job types.

### Pitfall 4: Import cycles and import firewalls
**What goes wrong:** Putting the resolver where podjob would need `internal/controller` (cycle: controller → podjob), or tripping `make lint`'s `verify-dag-imports`/`verify-dispatch-imports`/`verify-import-firewall`.
**How to avoid:** Constants/helper in `pkg/git` (leaf); controller-side resolver in `dispatch_helpers.go`; podjob mirrors inline (image-walk precedent, backend.go:267). Run `make lint` after adding imports.

### Pitfall 5: Commit-header corruption via unvalidated identity strings
**What goes wrong:** go-git serializes `object.Signature` name/email raw into the commit header — a newline or angle bracket in `agentName` malforms the commit object; the git CLI path is more tolerant but produces mangled identities.
**Why it happens:** Operator-supplied CRD strings flow into commit construction unsanitized (exec.Command args are shell-safe, but content is not header-safe).
**How to avoid:** kubebuilder markers on both fields — recommend `+kubebuilder:validation:MaxLength=100` + `Pattern=^[^<>\r\n]+$` for agentName; `+kubebuilder:validation:MaxLength=254` + `Pattern=^[^<>@\s]+@[^<>@\s]+$` for agentEmail (Claude's-discretion CEL question resolved: Pattern markers, matching the RepoURL precedent — no CEL needed).
**Warning signs:** `git fsck` errors or `git log` showing `<>` artifacts in test repos.

### Pitfall 6: Chart version double-bump vs Phase 35
**What goes wrong:** Phase 35 (baseRef) and Phase 36 batch into ONE chart version bump (D-06). If Phase 35 already bumped `hack/helm/tide-chart.yaml` (currently `1.0.6` [VERIFIED]), Phase 36 must not bump again; if 36 executes first or 35 slipped, 36 carries the bump.
**How to avoid:** Plan a conditional task: read `hack/helm/tide-chart.yaml` at execution time; bump only if still `1.0.6`. Note also both `version` and `appVersion` lines, and the CRD subchart (`hack/helm/tide-crds-chart.yaml` via `make helm-crds`) since the CRD schema changes too.

### Pitfall 7: `Spec.Git` is a nil-able pointer with required siblings
**What goes wrong:** `ProjectSpec.Git` is `*GitConfig` (`+optional`); dereferencing without nil-check panics in the resolver. Separately, `GitConfig.RepoURL` and `CredsSecretRef` have no `omitempty`/`+optional` — an operator cannot set `agentName` without a full git block. Acceptable (identity only matters when git is configured), but the CRD docs should not imply otherwise.
**How to avoid:** nil-check in the resolver (see Pattern 1); mark both new fields `+optional` with `omitempty`.

### Pitfall 8: Author vs committer asymmetry
**What goes wrong:** The git CLI `-c user.name/-c user.email` sets BOTH author and committer; go-git `CommitOptions` with only `Author` set copies it to Committer. Existing tests assert `commit.Author` — success criteria say "committer identity". Both paths yield matching author=committer today; keep it that way (don't set a distinct Committer on the go-git path).

## Code Examples

### Conditional-free env injection into the push Job (buildPushJob)

```go
// Source: pattern from internal/dispatch/podjob/jobspec.go:357 + push_helpers.go:234
Containers: []corev1.Container{{
    Name:  pushContainerName,
    Image: opts.TidePushImage,
    Args:  args,
    Env: []corev1.EnvVar{
        // SIGN-01: resolved agent identity — read by IntegrateTaskBranches
        // (merge commits) and the boundary-commit signature in tide-push.
        {Name: pkggit.EnvAgentName, Value: opts.AgentName},
        {Name: pkggit.EnvAgentEmail, Value: opts.AgentEmail},
    },
    EnvFrom: []corev1.EnvFromSource{ /* existing creds SecretRef unchanged */ },
    ...
}},
```

### Manager env → ProviderDefaults (cmd/manager/env.go)

```go
// Source: pattern from cmd/manager/env.go:96-101 (tideHelmProviderDefaults)
func tideHelmProviderDefaults(claudeImage string) controller.ProviderDefaults {
	return controller.ProviderDefaults{
		Image:      claudeImage,
		Models:     resolvePerLevelModels(),
		AgentName:  envOrDefault("TIDE_AGENT_NAME", ""),  // "" = chart tier unset → compiled default wins
		AgentEmail: envOrDefault("TIDE_AGENT_EMAIL", ""),
	}
}
```

### CRD field shape (both api versions)

```go
// Source: pattern from api/v1alpha2/project_types.go:205-224 (GitConfig)
	// AgentName is the commit author/committer name used at all three TIDE
	// commit sites (harness task commits, integrate merges, boundary pushes).
	// Precedence: this field → chart agent.name → compiled-in "TIDE Agent"
	// (SIGN-01 / Phase 36 D-03). Angle brackets and newlines are rejected —
	// they corrupt git commit headers.
	// +kubebuilder:validation:MaxLength=100
	// +kubebuilder:validation:Pattern=`^[^<>\r\n]+$`
	// +optional
	AgentName string `json:"agentName,omitempty"`

	// AgentEmail is the commit author/committer email. Choose a real,
	// routable address: when (deferred) commit signing lands, this email
	// must match a verified email on the machine account holding the key.
	// +kubebuilder:validation:MaxLength=254
	// +kubebuilder:validation:Pattern=`^[^<>@\s]+@[^<>@\s]+$`
	// +optional
	AgentEmail string `json:"agentEmail,omitempty"`
```

## State of the Art

| Old Approach | Current Approach | When Changed | Impact |
|--------------|------------------|--------------|--------|
| `TIDE_BOT_*` env read-but-never-set + hardcoded tide-push signature (v1.0.0 plan 11-01 D-03: "not a user-facing configuration concern at v1") | Full precedence chain, uniformly injected (this phase) | v1.0.7 | The v1.0.0 decision is explicitly superseded by SIGN-01 — do not treat old plan 11 docs as current guidance |
| Startup `--subagent-image` flag | `CLAUDE_SUBAGENT_IMAGE` env via `subagent.defaults.image` (Phase 13 D-01) | v1.0.x | Precedent that config flows values → manager env → resolution, not new CLI flags — follow it for identity |

**Deprecated/outdated:** `tideBotSignature()` and both `TIDE_BOT_*` env names are removed entirely by this phase (success criterion 2: "the `TIDE_BOT_*` env names are gone").

## Assumptions Log

| # | Claim | Section | Risk if Wrong |
|---|-------|---------|---------------|
| A1 | Phase 35 will add `baseRef` to both API type packages without a conversion webhook, and Phase 36 follows the same dual-version shape | Architecture Patterns | If Phase 35 introduces a webhook/parity test harness, Phase 36 must slot fields into that harness instead — check Phase 35's landed code at planning/execution time |
| A2 | Planner subagent Jobs receiving identity env is harmless (planners never commit) | Env Injection Surfaces | None functionally; if undesired, restrict injection to executor path (task_controller sites only) |
| A3 | `go` is available in the operator's normal execution shell despite being absent from this session's non-interactive PATH (repo history shows continuous local builds) | Environment Availability | If truly absent, every build/test task blocks — verify `go version` before Wave 1 |
| A4 | Chart value shape `agent.name`/`agent.email` (top-level block) matches user intent — D-04 gives it as "e.g." | Pattern 3 | Low; it is the user's own example and collides with nothing |

## Open Questions (RESOLVED)

1. **Does Phase 35 land before Phase 36 executes?** — RESOLVED: plans adopt the recommendation. 36-04 Task 1 makes the chart version bump conditional on the observed `hack/helm/tide-chart.yaml` value (Pitfall 6); 36-02 Task 1 checks Phase 35's landed `baseRef` pattern before slotting the CRD fields.
   - What we know: D-06 batches both CRD/chart changes into one version bump; STATE.md shows Phase 34 in planning, 35/36 not started.
   - What's unclear: execution order at the time this phase runs.
   - Recommendation: the version-bump task must be conditional on the observed `hack/helm/tide-chart.yaml` value (Pitfall 6), and the CRD-field task should diff against Phase 35's landed `baseRef` pattern if present.

2. **Should the kind Layer B suite gain an end-to-end identity assertion?** — RESOLVED: no. Unit + template coverage chosen (36-03 builder env-presence tests, 36-04 helm-template contract tests); the Layer B `git log` authorship assertion stays an optional future addition.
   - What we know: unit tests can pin builder env + per-binary fallback; the kind suite exercises real commits.
   - What's unclear: whether an existing Layer B spec inspects commit authorship (none found asserting identity outside cmd/tide-push unit tests).
   - Recommendation: cheap addition — after an existing Layer B run creates commits, `git log --format='%an <%ae>'` in the test namespace's bare repo asserting `TIDE Agent <tide-agent@tideproject.k8s>` at all three commit shapes. Optional; unit + template tests already cover SIGN-01's surface.

## Environment Availability

| Dependency | Required By | Available | Version | Fallback |
|------------|------------|-----------|---------|----------|
| Go toolchain | all builds/tests (go.mod pins go 1.26.0) | ✗ (not on this session's non-interactive PATH; not in asdf/homebrew standard paths) | — | Verify `go version` in the execution shell before Wave 1; install via `brew install go` if genuinely missing (see A3) |
| GNU make | Makefile targets | ✓ | 3.81 | — |
| Docker | image builds, kind prep | ✓ | daemon responding | — |
| kind | `make test-int` (Layer B) | ✗ | — | `make test` + `make test-int-fast` (envtest Layer A) cover the unit/env tiers; install kind (`brew install kind`) only if the phase gate requires Layer B |
| helm | chart lint/render checks | ✓ | v4.2.0 | Note: stack doc says Helm 3; v4 renders fine for template-grep contract tests, but flag if `helm lint` behaves differently |
| controller-gen / helmify / kustomize | `make generate manifests helm-controller` | assumed (Makefile self-installs to `bin/` via tool targets) | pinned in Makefile | Makefile downloads on demand — needs network + working go (blocks on the go gap above) |

**Missing dependencies with no fallback:**
- Go toolchain — blocks everything if absent from the execution shell. Highest-priority pre-flight check.

**Missing dependencies with fallback:**
- kind — Layer A (`make test`, `make test-int-fast`) verifies all phase behavior except live-cluster smoke; the helm-template contract tests are plain go-tests readable without a cluster.

## Validation Architecture

### Test Framework
| Property | Value |
|----------|-------|
| Framework | Go stdlib `testing` + Ginkgo v2.28/Gomega (integration tiers), envtest, kind |
| Config file | Makefile targets (no separate test config) |
| Quick run command | `go test ./pkg/git/... ./internal/harness/... ./cmd/tide-push/... ./internal/dispatch/podjob/... ./internal/controller/... ./api/... ./cmd/manager/... -short` |
| Full suite command | `make test` (unit tier), then `make test-int-fast` (Layer A); `make test-int` (kind Layer B) only if kind is installed |

### Phase Requirements → Test Map
SIGN-01 decomposes into verifiable behaviors:

| Req ID | Behavior | Test Type | Automated Command | File Exists? |
|--------|----------|-----------|-------------------|-------------|
| SIGN-01a | Harness commit uses `TIDE_AGENT_*` env, new default | unit | `go test ./internal/harness/ -run TestCommitWorktree` | ✅ update `commit_test.go:98-109` |
| SIGN-01b | Integrate merges use env identity, new default | unit | `go test ./pkg/git/ -run TestIntegrate` | ✅ extend `integrate_test.go` (add merge-commit identity assertion — none exists today) |
| SIGN-01c | tide-push boundary commit env-sourced; `tideBotSignature` gone | unit | `go test ./cmd/tide-push/ -run TestRunPush` | ✅ update `main_test.go:869-873` |
| SIGN-01d | Subagent Job pod spec carries `TIDE_AGENT_*` | unit | `go test ./internal/dispatch/podjob/ -run TestBuildJobSpec` | ❌ new test (pattern: jobspec_test.go:828 pricing transport) |
| SIGN-01e | Push Job pod spec carries `TIDE_AGENT_*` | unit | `go test ./internal/controller/ -run TestBuildPushJob` | ❌ new test |
| SIGN-01f | Precedence: spec beats chart beats compiled | unit | `go test ./internal/controller/ -run TestResolveAgentIdentity` | ❌ new test (mirror resolveImage tests) |
| SIGN-01g | CRD fields present in both api versions, validation markers | unit | `go test ./api/...` | ✅ extend `phase3_schema_test.go:303` (TestGitConfigRoundTrip) + `v1alpha2/schema_test.go` |
| SIGN-01h | Chart renders `TIDE_AGENT_*` from `agent.*` values | contract (plain go-test in kind pkg) | `go test ./test/integration/kind/ -run TestHelm` (template-grep, no cluster needed for the assertion style at projects_pvc_test.go:165-176) | ❌ new test |
| SIGN-01i | Manager env → ProviderDefaults wiring | unit | `go test ./cmd/manager/ -run TestTideHelmProviderDefaults` | ✅ extend `env_test.go` |

### Sampling Rate
- **Per task commit:** package-scoped `go test ./<touched-pkg>/...`
- **Per wave merge:** the quick-run command above + `make lint` (import firewalls)
- **Phase gate:** `make test` green AND `make test-int-fast` green; read `MAKE_EXIT` and grep `^--- FAIL` per CLAUDE.md (Ginkgo-green is not sufficient)

### Wave 0 Gaps
None — existing test infrastructure covers all phase requirements; new tests are additive within existing files/packages.

## Security Domain

### Applicable ASVS Categories

| ASVS Category | Applies | Standard Control |
|---------------|---------|-----------------|
| V2 Authentication | no | No auth surface changes (GIT_PAT flow untouched) |
| V3 Session Management | no | — |
| V4 Access Control | no | No new RBAC; identity env is non-secret pod config |
| V5 Input Validation | yes | kubebuilder Pattern markers on `agentName`/`agentEmail` (commit-header injection — Pitfall 5) |
| V6 Cryptography | no | GPG signing explicitly deferred (D-01); HMAC signingKey untouched |

### Known Threat Patterns for this change

| Pattern | STRIDE | Standard Mitigation |
|---------|--------|---------------------|
| Commit-header corruption via CRD strings (`<`, `>`, newline in name/email) | Tampering | CRD Pattern validation rejects at admission; strings flow to `exec.Command` args (no shell) and go-git Signature |
| Identity spoofing (operator sets arbitrary name/email) | Spoofing | Accepted by design — same trust tier as chart install; precedent T-11-01-04 (identity is public commit metadata, not a secret). Docs note the future email-match requirement for signing. |
| Identity leak into logs | Information Disclosure | Non-issue (accepted, T-11-01-04); values are public commit metadata |

## Sources

### Primary (HIGH confidence — all verified by direct file read/grep this session)
- `internal/harness/commit.go`, `pkg/git/integrate.go`, `cmd/tide-push/main.go` — the three commit sites
- `internal/dispatch/podjob/jobspec.go`, `backend.go` — subagent Job env + BuildOptions + inline image walk
- `internal/controller/push_helpers.go`, `boundary_push.go`, `dispatch_helpers.go` — push Job builder, PushOptions choke point, resolveImage/ProviderDefaults
- `cmd/manager/env.go`, `cmd/manager/main.go` — chart-env → reconciler wiring pattern
- `api/v1alpha1/project_types.go`, `api/v1alpha2/project_types.go`, `config/crd/bases/tideproject.k8s_projects.yaml` — GitConfig shape, served/storage flags
- `hack/helm/augment-tide-chart.sh`, `hack/helm/tide-values.yaml`, `hack/helm/tide-chart.yaml`, `Makefile:705-760` — chart generation pipeline
- `charts/tide/values.yaml` — value namespace collision check (`signingKey`)
- Test files: `internal/harness/commit_test.go`, `cmd/tide-push/main_test.go`, `pkg/git/integrate_test.go`, `internal/dispatch/podjob/jobspec_test.go`, `api/v1alpha1/phase3_schema_test.go`, `test/integration/kind/projects_pvc_test.go`
- `.planning/phases/36-signed-commits-bot-identity/36-CONTEXT.md`, `.planning/REQUIREMENTS.md`, `.planning/STATE.md`
- `docs/project-authoring.md:49` — GitConfig documentation anchor

### Secondary (MEDIUM confidence)
- None needed — no external research performed (no new dependencies; all questions answerable from the codebase).

### Tertiary (LOW confidence)
- None.

## Metadata

**Confidence breakdown:**
- Commit-site inventory & injection surfaces: HIGH — every file read directly; env-not-set verified by repo-wide grep
- Precedence/chart wiring pattern: HIGH — mirrors two validated in-repo precedents (resolveImage, ENV3 augment block)
- Phase 35 interaction (version bump, dual-version shape): MEDIUM — Phase 35 not yet planned/executed; conditional handling recommended (A1, Pitfall 6)
- Environment (go on PATH): MEDIUM — absent in this sandbox shell; likely a shell-init artifact (A3)

**Research date:** 2026-07-04
**Valid until:** Phase 35 execution (whichever comes first) or 30 days — internal codebase facts are stable but Phase 35 changes the same files (`project_types.go` GitConfig area, `hack/helm/tide-chart.yaml`)
