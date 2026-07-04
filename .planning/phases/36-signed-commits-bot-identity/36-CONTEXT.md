# Phase 36: Signed Commits + Bot Identity - Context

**Gathered:** 2026-07-03
**Status:** Ready for planning

<domain>
## Phase Boundary

**DESCOPED at discussion (2026-07-03): this phase delivers SIGN-01 only.** GPG signing (SIGN-02/03/04) is deferred out of v1.0.7 wholesale — see Deferred Ideas for the preserved analysis.

Phase 36 delivers: the TIDE agent identity (name/email) is uniformly configurable across all three commit sites — harness task commits (`internal/harness/commit.go`), integrate merge commits (`pkg/git/integrate.go`), and tide-push boundary commits (`cmd/tide-push/main.go`) — with the tide-push hardcoded identity removed. No signing-key Secret ref, no key validation, no Verified-badge docs land in this phase.

The `research: true` flag on the original phase entry is void — the gpg-shim vs plumbing spike belongs to the future signing work, not this phase.

</domain>

<decisions>
## Implementation Decisions

### Scope: signing deferred (headline decision)
- **D-01:** Phase 36 is descoped to SIGN-01 only. SIGN-02 (opt-in GPG signing at all three sites), SIGN-03 (first-reconcile key validation), and SIGN-04 (Verified-badge operator docs) are deferred out of v1.0.7 entirely. Rationale: the functional payoff — repos with require-signed-commits branch protection — is hypothetical today (the first external run surfaced unverified badges as an annoyance, not a block); the cost is real (gpg-shim/plumbing spike, signing-oracle key-exposure design, manual external UAT); and signing is a leaf feature — once Phase 34 stabilizes the commit sites, deferring carries no compounding penalty.
- **D-02:** The intermediate slices were considered and rejected: controller-sites-only signing pays the spike cost but still fails branch protection (task commits are reachable from the run branch); boundary-commit-only signing avoids the spike but delivers almost nothing. If signing returns, it returns as full three-site signing.

### Identity configuration surface
- **D-03:** New Project CRD fields `spec.git.agentName` / `spec.git.agentEmail` (in `GitConfig`), with precedence **Project spec → chart value → compiled-in default** — matching the validated image-resolution chain pattern (`Levels.<level>.Image` → `Spec.Subagent.Image` → helm default, v1.0.1).
- **D-04:** The bot→agent rename applies **everywhere**: env vars become `TIDE_AGENT_NAME` / `TIDE_AGENT_EMAIL` (the `TIDE_BOT_*` names are read in harness + integrate but set by nothing today — the rename is free), chart values use agent naming (e.g. `agent.name` / `agent.email`), and the compiled-in default identity becomes **`TIDE Agent <tide-agent@tideproject.k8s>`**. Accepted consequence: unconfigured installs see a one-time committer-identity change on new commits.
- **D-05:** tide-push's hardcoded `tideBotSignature()` is replaced by the same env-sourced identity chain. The W11 stability contract (name+email stable across runs; only timestamp varies) is preserved: values come from install/Project config, not per-run state.
- **D-06:** The CRD change batches with Phase 35's `baseRef` CRD change into **one chart version bump** (values.yaml is the FIXED contract; binary catches up to chart, never reverse).

### Dependencies (revised by descope)
- **D-07:** The original Phase 34 dependency was signing-specific (signing touching just-stabilized merge code). Identity-only work merely renames/sets env reads at those sites — Phase 36 no longer meaningfully depends on Phase 34. The Phase 35 sequencing stays (chart bumps batch).

### Claude's Discretion
- Exact chart value shape/nesting for the agent identity (must not collide with the existing `signingKey` HMAC value — see code insights).
- Whether/what CEL validation the new CRD fields get (e.g. email format).
- Docs placement for the identity-config note (likely wherever GitConfig/creds are already documented).

### Folded Todos
- `2026-07-03-signed-commits-verified-badge.md` — **partially folds.** Its identity half (hardcoded tide-push identity, unset `TIDE_BOT_*` env) resolves in this phase. Its signing half (GPG at three sites, Secret ref, Verified badges) is deferred with SIGN-02/03/04 — the todo should be retagged/split rather than closed when this phase completes.

</decisions>

<canonical_refs>
## Canonical References

**Downstream agents MUST read these before planning or implementing.**

### Planning artifacts
- `.planning/ROADMAP.md` — Phase 36 entry (amended this session to identity-only scope)
- `.planning/REQUIREMENTS.md` — SIGN-01 (SIGN-02/03/04 moved out of v1.0.7 this session)
- `.planning/todos/pending/2026-07-03-signed-commits-verified-badge.md` — commit-site inventory and the original signing solution sketch (its signing half stays pending)

### Code (the three commit sites + config surfaces)
- `internal/harness/commit.go` — harness task-commit site; reads `TIDE_BOT_NAME`/`TIDE_BOT_EMAIL` (to rename), commits via git CLI `-c user.name/user.email`
- `pkg/git/integrate.go` — integrate merge-commit site; same env reads, git CLI merges
- `cmd/tide-push/main.go` — `tideBotSignature()` hardcodes `tide-bot@tideproject.k8s` (to remove); go-git commit path; W11 stability comment
- `api/v1alpha1/project_types.go` — `GitConfig` (new `agentName`/`agentEmail` fields live here, alongside `CredsSecretRef`)
- `charts/tide/values.yaml` — FIXED contract; agent identity values ride the Phase 35 batched chart bump

</canonical_refs>

<code_context>
## Existing Code Insights

### Reusable Assets
- **Image-resolution chain pattern** (`Levels.<level>.Image` → `Spec.Subagent.Image` → helm default, validated v1.0.1): the identity precedence chain (Project → chart → compiled-in) should mirror this implementation shape.
- **`GitConfig.CredsSecretRef` pattern**: the established home for per-project git configuration on the Project CRD.
- **Existing env reads**: harness and integrate already read `TIDE_BOT_NAME`/`TIDE_BOT_EMAIL` with compiled-in fallbacks — the mechanism exists; it needs renaming, and something must finally *set* the env at the dispatch sites (subagent Job env + push Job env).

### Established Patterns
- Chart values.yaml is a FIXED contract — schema/values additions ride a version bump, batched with Phase 35's.
- **Naming collision to avoid:** the chart already has `signingKey.secretName: tide-signing-key` — that is the **HMAC envelope-signing** Secret (D-C3), unrelated to git identity or GPG. Any future GPG config (deferred) and any new value names must not collide with it.

### Integration Points
- Subagent Job env construction (dispatch site) — where `TIDE_AGENT_NAME`/`TIDE_AGENT_EMAIL` must be injected for the harness.
- Push Job env construction — same injection for integrate + tide-push.
- Chart templates that render manager/job env from values.

</code_context>

<specifics>
## Specific Ideas

- Vocabulary: the user explicitly renamed the surface from "bot" to **"agent"** (`agentName`/`agentEmail`) — apply agent terminology consistently across new fields, env vars, chart values, and the default identity string.
- Docs should note that the configured email becomes meaningful later: when (deferred) signing lands, the committer email must match a verified email on the machine account holding the public key — choosing a real, routable address now avoids churn.

</specifics>

<deferred>
## Deferred Ideas

### GPG signing (SIGN-02/03/04) — deferred out of v1.0.7 (preserve this analysis)
- **Requirements deferred:** SIGN-02 (opt-in signing-key Secret ref, signed commits at all three sites incl. integrate merges), SIGN-03 (first-reconcile key validation: armored / no passphrase / email-match triple, clear failure condition), SIGN-04 (machine-account + keygen + public-key-upload docs recipe for GitHub/GitLab/Gitea).
- **The spike (was `research: true`):** go-git v5.19 cannot sign three-way merges via `SignKey`; `git merge --no-commit` + go-git commit silently flattens merge topology. Harness + integrate commit via the git CLI; only tide-push uses go-git. Any signing that covers integrate merge commits needs a **gpg-shim** (pure-Go `gpg.program` stand-in) or **plumbing-level** commit construction — spike before planning.
- **Key-exposure analysis (the ASK-FIRST decision, resolved by descope):** mounting the private key in the subagent pod makes it a signing oracle — LLM-executed code can read/exfiltrate it via prompt injection. Options analyzed: (A) controller-sites-only — weak middle: pays the spike cost yet still fails require-signed-commits branch protection because unsigned task commits are reachable from the run branch; (B) restructure the task commit out of the LLM pod — security-ideal but highest code risk (relocates `CommitWorktree`: envelope HeadSHA reporting, empty-diff→task-fail semantics); (C) mount-everywhere with documented risk + a dedicated TIDE-only machine-account key (bounded blast radius, easy revocation, optional gitleaks rule for armored-key blocks) — the coherent full-value shape. If signing returns, it likely returns as (C).
- **Implementation notes:** `ProtonMail/go-crypto` is already an indirect dep (promote when signing lands); no gpg binary in images; future Secret ref name must avoid the chart's `signingKey` HMAC collision (e.g. `git.signingKeySecretRef`); portable GPG only — no GitHub-API auto-sign (violates the no-hard-coded-git-host constraint).
- **UAT is external:** `git verify-commit` in-cluster does not prove the badge; needs one manual push to a real GitHub repo including an integrate merge commit.

### Reviewed Todos (not folded)
- 8 other keyword matches from the pending-todo scan are tagged to their own phases (34: wave-parallel integration miss; 35: git baseRef; 37: dashboard log drawer, artifact view; 38: pricing table, Prometheus setup) or explicitly deferred (`subagent.levels` rename — own milestone; CACHE-F1 — vNext). None belong here.

</deferred>

---

*Phase: 36-Signed Commits + Bot Identity (descoped to identity-only)*
*Context gathered: 2026-07-03*
