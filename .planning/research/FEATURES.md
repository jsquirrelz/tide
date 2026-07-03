# Feature Research

**Domain:** K8s-native workflow orchestrator — first-run operator ergonomics (v1.0.7 First-Run Paper Cuts)
**Researched:** 2026-07-03
**Confidence:** MEDIUM-HIGH (git-host verification rules HIGH from official docs; dashboard/UX conventions MEDIUM from ecosystem survey)

Scope: how the v1.0.7 target capabilities work in comparable tools (Renovate, Dependabot, Argo CD/Workflows, Tekton, kubebuilder-ecosystem operators) and what operators expect. The two run-integrity features (integration-miss gate, pricing table) are TIDE-internal correctness fixes with no external comparable — they appear in the landscape tables but the ecosystem research below covers the five behavior-shaped features: baseRef, signed commits, promptFile, dashboard artifact/log surfaces, telemetry setup nudge.

## Ecosystem Findings (per question)

### 1. Base-ref selection for automation-created branches

How comparable tools do it:

- **Dependabot** — `target-branch` per `package-ecosystem` block in `dependabot.yml`. Branch name only. Applies to version updates only (security updates always target the default branch). Unknown branch → run fails with a config error surfaced in the Dependabot UI; no silent fallback.
- **Renovate** — `baseBranches` array (multiple branches, regex patterns supported). Missing branch → warning/error in the job log, not silent.
- **Argo CD** — `spec.source.targetRevision` accepts branch, tag, or commit SHA (also `HEAD`). Unresolvable ref → app creation **rejected** with `Unable to resolve 'X' to a commit SHA`, and at sync time a `ComparisonError` condition on the Application. The resolved SHA is stamped in `status.sync.revision`. Docs recommend explicit branch/SHA over `HEAD`.
- **Tekton** — git-clone task `revision` param (branch/tag/SHA; empty = remote default branch). Bad ref → TaskRun `Failed` with the raw git stderr in the step log; historically rough edges around tag refs (tektoncd/pipeline#2425).
- **Argo Workflows** — GitArtifact `revision`; notably rejects the fully-qualified `refs/heads/*` form (argo-workflows#5629) — accept short names, be liberal in what the field takes or document the accepted forms precisely.

**Operator expectations distilled:**
1. One optional string field accepting branch OR tag OR SHA — tools that split these into separate fields are the minority; a single `revision`-style string is the convention.
2. Default = the remote's default branch when unset (never require it).
3. **Fail fast with a typed condition** — reject at admission/first-reconcile with a message naming the unresolvable ref (Argo CD's `Unable to resolve 'X' to a commit SHA` is the canonical wording), never mid-run.
4. **Pin the resolved SHA in status** (Argo CD `status.sync.revision` pattern) so the run is reproducible even if the branch moves — pairs naturally with the planned `status.git.lastPushedSHA`.

TIDE's planned `spec.git.baseRef` + reject-unresolvable-with-condition matches the ecosystem exactly. The one addition worth making: stamp `status.git.baseSHA` (resolved at run start) alongside `lastPushedSHA`.

### 2. Bot commit signing + Verified badges

Verification rules per host (this is where bots most often "sign but never verify"):

- **GitHub** — a signature shows **Verified** only when: (a) the signature is cryptographically valid, (b) the public key is registered on a GitHub account, and (c) the **committer email matches an identity (UID) on the key AND a verified email on that account**. GPG and SSH signing keys are both supported. Hosted bots (Dependabot, the Mend Renovate app) get Verified *without* managing keys — they commit via the GitHub API as a GitHub App, and **GitHub signs with its own key**. That route requires committing via the REST API, not `git push` — unavailable to TIDE's generic-git-remote architecture.
- **GitLab** — committer email must match a **verified email of the GPG key's owner account**; GitLab verifies against its own keyring (no keyservers). SSH-signed commits also supported. Well-documented failure mode: valid key + mismatched committer email → "Unverified" (gitlab-org/gitlab#29368).
- **Gitea** — marks Verified only if the key is registered on a Gitea account **and the committer email is registered on the account that owns the key** (the documented Renovate+Gitea recipe: gitAuthor email must match the bot account's email or "the commits sign but never verify"). Gitea uniquely also supports instance-level signing (`SIGNING_NAME`/`SIGNING_EMAIL`) and allows signer ≠ committer in some flows — but the portable rule is the same email-match triple.

Self-hosted **Renovate** is the closest architectural comparable to TIDE (signs locally, pushes over git): a single env var (`RENOVATE_GIT_PRIVATE_KEY`, ASCII-armored GPG private key) + the constraint that `gitAuthor` name/email must match the key's UID. That is exactly the shape TIDE plans: optional Secret ref carrying the armored key + uniformly configurable bot identity.

**Operator expectations distilled:**
1. GPG (openpgp) is the interop-widest choice — all three hosts verify it; SSH signing is newer and Gitea/GitLab support lags versions. Pure-Go `openpgp` + go-git's `CommitOptions.SignKey` is the standard implementation path (no gpg binary in the container).
2. Signing must be **uniform across every commit site** — a run with mixed Verified/unverified commits reads worse than unsigned (TIDE's tide-push hardcoded identity is precisely this bug class).
3. **Document the email-match triple** (key UID = committer email = registered bot-account email) — it's the #1 support issue for every bot that signs; the feature is incomplete without that doc paragraph.
4. Key passphrase support: armored keys in Secrets are conventionally unencrypted (Renovate's is); supporting passphrases is optional polish.
5. Anti-pattern for TIDE: the GitHub-App API-commit route — host-specific, violates the no-hard-coded-git-host constraint.

### 3. Prompt-from-file config UX

Kubernetes ecosystem patterns for "a chunk of text the user authored in a file":

- **valueFrom-style union in the CRD** — inline field XOR ConfigMap/Secret key selector, mutually exclusive, CEL/webhook-validated. Canonical examples: KubeVirt `userData` vs `userDataSecretRef`, Grafana Operator dashboard `json` vs `configMapRef` vs `url`, Prometheus Operator's `additionalScrapeConfigs` SecretKeySelector. This is the pattern when the content must be **cluster-resident and GitOps-manageable** independent of the CR.
- **CLI-side file expansion** — the client reads the file and inlines it into the spec at apply time (`kubectl create configmap --from-file` is the archetype). This is the pattern when the field is small, immutable-per-run, and the pain being solved is *authoring YAML multiline strings*, not content lifecycle.
- Size bounds: ConfigMaps cap at 1 MiB; whole CR objects at etcd's ~1.5 MiB. Outcome prompts are KBs — both routes fit trivially.

**Which fits TIDE:** the pain from the first run is authoring ergonomics (writing a long prompt as a YAML block scalar), not prompt lifecycle. A ConfigMap ref buys nothing here and costs: a second object to create/RBAC/garbage-collect, a watch (or a deliberate no-watch policy — and a mutable prompt under a running Project is semantically wrong anyway), and a resolution failure mode. **CLI-side `--prompt-file` (or `promptFile:` in the tide CLI's project file, expanded before apply) is the low-complexity, convention-consistent route.** The CRD keeps one field, `spec.outcomePrompt`, and stays the single source of truth. A `configMapRef` union can be added later behind the same field pair without breaking anything if GitOps-managed prompts become a real demand.

### 4. Artifact/log review surfaces at approval gates

- **Argo Workflows UI** — clicking a node opens a panel with INPUTS/OUTPUTS artifact lists (download links; in-browser preview for some types) plus a logs tab. When the pod is GC'd, the UI falls back to **archived logs** from the artifact repository — the interim state literally renders *"Still waiting for data or an error? Try getting logs from the artifacts."* This fallback is chronically buggy (issues #12948, #8814, #12759, #13785: "artifact not found: main-logs", 500s, per-node-type gaps) — the ecosystem lesson is that **the fallback state must be explicit and tested per node type**, not best-effort.
- **Tekton Dashboard** — live logs stream from the pod; when the pod is gone it shows *"Unable to fetch logs"* and supports a configured **external logs fallback service** (`--external-logs`, object-storage-backed); Tekton Results persists logs past pruning. Again: an explicit "pod gone → here's the fallback (or here's why there isn't one)" state.
- **Argo CD** — approval-adjacent review happens against *rendered manifests + diff view*, i.e. the reviewed artifact is shown **in-UI, rendered**, not as a download link. That's the right analogy for TIDE's approve gates: the operator is approving `MILESTONE.md`/phase briefs/`PLAN.md` — rendered markdown in the dashboard, not a raw-file download.

**Operator expectations distilled:**
1. At an approve gate, the artifact under review is viewable **in the dashboard, rendered** (markdown), one click from the gated node. The first external run needed three ad-hoc PVC reader pods for this — the exact failure these tools' artifact viewers exist to prevent.
2. Log surfaces have a **mandatory tri-state**: loading (spinner + "connecting to pod"), streaming (live tail), and pod-gone (explicit message + fallback if any). A silently empty drawer is indistinguishable from broken — TIDE's current state.
3. TIDE has a structural advantage over Argo/Tekton here: artifacts already live on the run PVC and are pushed to git at level boundaries. The dashboard's manager API can serve them from the PVC directly — no object-storage artifact repo needed (Argo's biggest fallback-complexity source). For the pod-gone log state, v1.0.7 does *not* need log archiving (Argo's experience says it's a tarpit); an honest "pod completed and was garbage-collected — see the task's result envelope / artifact" message is the table-stakes fix, with envelope stdout/stderr capture as the cheap fallback if the envelope already carries it.

### 5. "Telemetry disabled" empty states + setup nudges

- **NOTES.txt convention** — Go-templated conditional blocks keyed on values: when an optional integration is off, print a short warning naming the flag, the exact enable command (`helm upgrade ... --set prometheus.enabled=true`), and a docs link. Keep it brief (it prints on install/upgrade/status); point to README/INSTALL for depth. kube-prometheus-stack-family charts and most ServiceMonitor-optional charts do exactly this. TIDE's `prometheus.enabled=false` default (chosen to avoid ServiceMonitor CRD-not-found) makes the nudge mandatory — dark-by-default without a nudge reads as broken telemetry.
- **Dashboard empty-state convention** — the load-bearing distinction is **"disabled by configuration" vs "enabled but no data" vs "error"**. A good disabled-state banner: states that telemetry is off (not failing), shows the one-line enable step, links docs. Tools that conflate "no data" with "not configured" generate a steady stream of confused issues; the fix is always the same explicit banner.
- **INSTALL.md step** — a numbered optional step ("Enable telemetry") mirroring the NOTES.txt text, so the three surfaces (INSTALL, NOTES.txt, dashboard banner) say the same thing with the same command.

## Feature Landscape

### Table Stakes (Users Expect These)

| Feature | Why Expected | Complexity | Notes |
|---------|--------------|------------|-------|
| Integration-miss gate (serialize/retry task→run-branch merges; gate `Complete` on reachability; `status.git.lastPushedSHA`) | A run stamped `Complete` with a deliverable silently missing breaks the core trust contract; every CI system treats "reported success = artifacts present" as axiomatic | MEDIUM | TIDE-internal, no ecosystem comparable needed. Mechanical degenerate case of the verify-stage seed |
| Claude 5 pricing rows | Budget meter overcounting 2.8× makes `absoluteCapCents` useless; correct pricing per model is assumed | LOW | Pure table addition + fallback-tier audit. Keep the "unknown model → most-expensive tier" fallback but log it loudly |
| `spec.git.baseRef` accepting branch/tag/SHA, default = remote default branch | Every comparable (Dependabot `target-branch`, Renovate `baseBranches`, Argo CD `targetRevision`, Tekton `revision`) offers this; single-string field is the convention | LOW-MEDIUM | Reject unresolvable ref at first reconcile with a typed condition (Argo CD wording is canonical). Stamp resolved `status.git.baseSHA`. Document accepted forms (avoid Argo Workflows' `refs/heads/*` rejection surprise) |
| Uniform, configurable bot identity across all three commit sites | Mixed identities/signatures across one run's commits read as broken; prerequisite for verification email-match | LOW | tide-push currently hardcodes it — fix regardless of signing |
| Dashboard artifact view at gated Planning-DAG nodes (rendered markdown) | Argo CD reviews rendered manifests in-UI; Argo Workflows shows per-node artifacts. Operators expect to review what they're approving without `kubectl exec`/reader pods | MEDIUM | Serve from run PVC via manager API — TIDE needs no artifact-repo object storage. This is the approve-gate review surface, the milestone's biggest ergonomics win |
| Log-drawer tri-state (loading / streaming / pod-gone) | Every workflow dashboard (Argo, Tekton) has explicit states; silently empty = indistinguishable from broken | LOW-MEDIUM | Pod-gone state: honest message + point to result envelope/artifact. Do NOT build log archiving (see anti-features) |
| Telemetry-disabled nudge (NOTES.txt conditional + INSTALL.md step + dashboard banner) | Dark-by-default (`prometheus.enabled=false`) without a nudge reads as broken telemetry; NOTES.txt conditional warnings are standard chart practice | LOW | Same enable command verbatim on all three surfaces. Banner must distinguish "disabled by config" from "no data" |
| promptFile via CLI-side expansion (`--prompt-file` / `promptFile:` expanded to `spec.outcomePrompt` at apply) | `kubectl --from-file` archetype; solves the actual pain (YAML block-scalar authoring) with zero new CRD surface | LOW | CRD unchanged; single source of truth stays `spec.outcomePrompt` |

### Differentiators (Competitive Advantage)

| Feature | Value Proposition | Complexity | Notes |
|---------|-------------------|------------|-------|
| GPG-signed bot commits (pure-Go openpgp, optional Secret ref) | Verified badges on orchestrator commits across GitHub/GitLab/Gitea; self-hosted Renovate is the only comparable that does portable local signing — hosted bots cheat via host APIs | MEDIUM | go-git `CommitOptions.SignKey`. Opt-in (unset Secret ref = unsigned, today's behavior). MUST ship with the email-match-triple doc (key UID = committer email = bot-account verified email) per host, or users file "signs but never verifies" issues |
| Project view (outcome prompt + settings in dashboard) | Operators reviewing a gate want the run's intent visible next to it; comparable dashboards show the app/workflow spec | LOW | Read-only render of Project spec; pairs with artifact view |
| PVC-direct artifact serving (no artifact-repo dependency) | Argo's artifact-fallback bug tail comes from object-storage indirection; TIDE's PVC + git-boundary-push design lets the dashboard serve artifacts with zero extra infra | — | Architectural advantage to preserve, not a work item — don't add an object-store artifact repo |
| `status.git.baseSHA` pinning | Reproducibility: the run records what it branched from even if the ref moves (Argo CD `status.sync.revision` pattern) | LOW | Cheap addition riding the baseRef work |

### Anti-Features (Commonly Requested, Often Problematic)

| Feature | Why Requested | Why Problematic | Alternative |
|---------|---------------|-----------------|-------------|
| ConfigMap-ref promptFile (`spec.promptRef`) | "K8s-native", GitOps-manageable prompts | Second object lifecycle (create/RBAC/GC), resolution failure modes, and mutable-prompt-under-running-Project semantics that are wrong anyway; solves lifecycle when the pain is authoring | CLI-side expansion now; a valueFrom-style union (`outcomePrompt` XOR `promptRef`, CEL mutual exclusion) can be added compatibly later if GitOps demand materializes |
| GitHub-App API-commit signing ("GitHub signs for you") | Zero key management, how Dependabot/hosted-Renovate get Verified | Requires committing via GitHub's REST API, not `git push` — hard-codes one git host, violating TIDE's portability constraint | Local GPG signing over the generic git remote (self-hosted-Renovate model) |
| Log archiving to object storage (Argo `archiveLogs` model) | "Logs survive pod GC" | Argo's own docs say don't rely on it; its fallback UI is a multi-year bug tail (artifact-not-found, 500s, per-node-type gaps); adds an artifact-repo dependency TIDE deliberately doesn't have | Explicit pod-gone state + result-envelope stdout/stderr as the reviewable residue; artifacts (the actual deliverables) are already on PVC + git |
| Auto-fallback when baseRef unresolvable ("just use default branch") | "Don't fail my run over a typo" | Silent fallback = run built from the wrong base, discovered after spend; every comparable fails fast instead | Typed condition + clear message at first reconcile, before any subagent spend |
| SSH commit signing (instead of / alongside GPG) | Simpler key format, newer hotness | Verification support is uneven across GitLab/Gitea versions; GPG verifies everywhere; supporting both doubles the doc/test matrix | GPG-only for v1.0.7; SSH signing later if demanded |
| In-dashboard approve/reject buttons on the new artifact view | "I can see it, let me approve it" | Breaks the v1 read-only-dashboard decision (single auth surface via CLI/kubectl) | Artifact view shows the gate state + the exact `tide approve` command to copy |

## Feature Dependencies

```
Artifact view (approve-gate surface)
    └──requires──> manager API endpoint serving PVC artifacts
    └──enhanced by──> Project view (spec/settings render — same read-only API surface)

Log-drawer tri-state
    └──requires──> same manager API/SSE surface (pod-log proxy states)

Signed commits
    └──requires──> uniform configurable bot identity (email-match rule spans all 3 commit sites)
    └──interacts──> integration-miss gate (both touch the same 3 commit sites; sign the
                    merge commits the gate serializes — land gate first or together)

spec.git.baseRef
    └──independent; pairs with──> status.git.lastPushedSHA (both live in status.git)

Telemetry nudge (NOTES.txt + INSTALL + banner)
    └──requires──> dashboard knowing prometheus.enabled state (chart → manager config → API)

promptFile (CLI-side)
    └──independent──> touches tide CLI only; no CRD/controller change

Pricing table ──independent
Integration-miss gate ──independent (headline; touches integrate + push + Complete gating)
```

### Dependency Notes

- **Signed commits require identity uniformity first:** the Verified badge depends on committer email matching the key UID and the bot account's verified email at *every* commit site; the hardcoded tide-push identity must become configurable before (or with) signing, or some commits verify and others don't.
- **Signed commits interact with the integration-miss gate:** both touch the harness/integrate/tide-push commit sites. Sequencing them in the same phase (or gate-first) avoids double-touching merge-commit creation.
- **Artifact view, project view, and log-drawer states share one surface:** all three are read-only manager-API + dashboard work; batching them into one dashboard phase amortizes the API plumbing.
- **Banner needs config plumbing:** the dashboard can't render "telemetry disabled" without the manager exposing whether metrics/ServiceMonitor are enabled — small chart→manager→API thread.

## MVP Definition

### Launch With (v1.0.7)

- [ ] Integration-miss gate + `status.git.lastPushedSHA` — run-integrity headline; trust in `Complete` is the product
- [ ] Claude 5 pricing rows — budget meter correctness; blocks trustworthy second run
- [ ] `spec.git.baseRef` with fail-fast condition (+ `status.git.baseSHA` stamp) — low cost, ecosystem-standard shape
- [ ] Uniform configurable bot identity + GPG signing behind optional Secret ref + email-match docs — the docs are part of the feature
- [ ] promptFile via CLI-side expansion — lowest-complexity route, no CRD change
- [ ] Dashboard: artifact view at gated nodes (rendered), project view, log-drawer tri-state — the approve-gate review surface
- [ ] Telemetry nudge: NOTES.txt conditional + INSTALL.md step + dashboard banner (same command on all three)
- [ ] v1.0.6 tech-debt carry (RetryOnConflict hardening, plannerConcurrency default 4, envtest tier split)

### Add After Validation (v1.x)

- [ ] `promptRef` ConfigMap union — trigger: real GitOps-managed-prompt demand
- [ ] SSH commit signing — trigger: user demand + Gitea/GitLab version support stabilizing
- [ ] Result-envelope stdout/stderr surfaced in the pod-gone log state — trigger: operators still wanting post-GC log residue after the honest empty state ships

### Future Consideration (v2+)

- [ ] Verify-tier LLM subagents (seed) — deferred by milestone decision; mechanical case ships now
- [ ] Log persistence/archiving — only if the envelope-residue approach proves insufficient; Argo's experience says avoid

## Feature Prioritization Matrix

| Feature | User Value | Implementation Cost | Priority |
|---------|------------|---------------------|----------|
| Integration-miss gate | HIGH | MEDIUM | P1 |
| Pricing table (Claude 5 rows) | HIGH | LOW | P1 |
| Dashboard artifact view (gate surface) | HIGH | MEDIUM | P1 |
| Log-drawer tri-state | MEDIUM | LOW-MEDIUM | P1 |
| `spec.git.baseRef` | MEDIUM | LOW-MEDIUM | P1 |
| Bot identity uniformity | MEDIUM | LOW | P1 (prereq for signing) |
| GPG-signed commits + docs | MEDIUM | MEDIUM | P2 |
| promptFile (CLI-side) | MEDIUM | LOW | P2 |
| Project view | MEDIUM | LOW | P2 |
| Telemetry nudge (3 surfaces) | MEDIUM | LOW | P2 |
| Tech-debt carry | MEDIUM | LOW-MEDIUM | P2 |

## Competitor Feature Analysis

| Feature | Renovate/Dependabot | Argo CD/Workflows/Tekton | Our Approach |
|---------|---------------------|--------------------------|--------------|
| Base-ref config | `baseBranches` array / `target-branch` per block; config error on missing branch | `targetRevision` / `revision` single string (branch/tag/SHA); Argo CD rejects unresolvable with "Unable to resolve to a commit SHA", pins resolved SHA in status | Single optional `spec.git.baseRef` string; fail-fast typed condition; stamp resolved SHA in `status.git` |
| Verified bot commits | Hosted: GitHub App API commits (GitHub's key). Self-hosted: armored GPG key env var + gitAuthor email match | N/A (don't author commits as bots the same way) | Self-hosted-Renovate model: armored GPG key in Secret ref, go-git SignKey, uniform identity, email-match-triple docs for GitHub/GitLab/Gitea |
| Prompt/config from file | N/A | Inline vs `workflowTemplateRef`; ecosystem valueFrom unions (KubeVirt, Grafana Operator) | CLI-side file expansion into the existing inline field; union deferred |
| Artifact review at gates | N/A (PR diff IS the review surface) | Argo Workflows node panel artifact lists; Argo CD rendered-manifest diff; Tekton external-logs fallback | Rendered markdown artifacts served from run PVC via manager API; no object-store dependency |
| Logs after pod GC | N/A | Argo `archiveLogs` → artifact repo (buggy fallback UI); Tekton `--external-logs` service | Explicit pod-gone state, no archiving; envelope residue as cheap fallback later |
| Optional-telemetry nudge | N/A | Standard NOTES.txt conditionals in ServiceMonitor-optional charts | Conditional NOTES.txt + INSTALL step + dashboard "disabled by config" banner, identical enable command |

## Sources

**Base-ref selection (MEDIUM-HIGH):**
- [Dependabot options reference — GitHub Docs](https://docs.github.com/en/code-security/reference/supply-chain-security/dependabot-options-reference) (target-branch; version-updates-only)
- [Argo CD tracking strategies](https://argo-cd.readthedocs.io/en/stable/user-guide/tracking_strategies/) (targetRevision forms)
- [argo-cd#7282 — "Unable to resolve to a commit SHA"](https://github.com/argoproj/argo-cd/issues/7282) (fail-fast behavior)
- [argo-workflows#5629 — refs/heads/* rejected](https://github.com/argoproj/argo-workflows/issues/5629); [tektoncd/pipeline#2425 — tag revision handling](https://github.com/tektoncd/pipeline/issues/2425)

**Signed commits (HIGH — official docs):**
- [About commit signature verification — GitHub Docs](https://docs.github.com/en/authentication/managing-commit-signature-verification/about-commit-signature-verification); [Using a verified email address in your GPG key](https://docs.github.com/en/authentication/troubleshooting-commit-signature-verification/using-a-verified-email-address-in-your-gpg-key)
- [Sign commits with GPG — GitLab Docs](https://docs.gitlab.com/user/project/repository/signed_commits/gpg/); [gitlab#29368 — email-mismatch → unverified](https://gitlab.com/gitlab-org/gitlab/-/issues/29368)
- [GPG/SSH Commit Signatures — Gitea Docs](https://docs.gitea.com/administration/signing)
- [renovate discussion #22751 — self-hosted verified commits](https://github.com/renovatebot/renovate/discussions/22751); [Signed Renovate commits on Gitea (2026)](https://johanneskueber.com/posts/2026-05-26-sign-renovate-commits/); [GitHub community #50055 — commit signing with GitHub Apps](https://github.com/orgs/community/discussions/50055)

**promptFile patterns (MEDIUM):**
- [Kubernetes configuration patterns, Part 2 — Red Hat Developer](https://developers.redhat.com/blog/2021/05/05/kubernetes-configuration-patterns-part-2-patterns-for-kubernetes-controllers) (large config → ConfigMap ref; validated config → inline)
- [ConfigMaps — Kubernetes docs](https://kubernetes.io/docs/concepts/configuration/configmap/) (1 MiB limit)

**Artifact/log surfaces (MEDIUM):**
- [Configuring Archive Logs — Argo Workflows docs](https://argo-workflows.readthedocs.io/en/latest/configure-archive-logs/) ("does not recommend relying on Argo to archive logs")
- [argo-workflows discussion #7656](https://github.com/argoproj/argo-workflows/discussions/7656), [#12948](https://github.com/argoproj/argo-workflows/issues/12948), [#8814](https://github.com/argoproj/argo-workflows/issues/8814), [#12759](https://github.com/argoproj/argo-workflows/issues/12759) (fallback bug tail)
- [Tekton Dashboard logs-persistence walkthrough](https://github.com/tektoncd/dashboard/blob/main/docs/walkthrough/walkthrough-logs.md) (external-logs fallback, "Unable to fetch logs")

**Telemetry nudges (MEDIUM):**
- [Creating a NOTES.txt File — Helm docs](https://helm.sh/docs/chart_template_guide/notes_files/); [NOTES.txt guide — devopscube](https://devopscube.com/helm-notes-txt-file/) (conditional sections, keep brief)

---
*Feature research for: TIDE v1.0.7 — First-Run Paper Cuts (Run Integrity & Operator Ergonomics)*
*Researched: 2026-07-03*
