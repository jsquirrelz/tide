# Requirements: TIDE v1.0.7 — First-Run Paper Cuts

**Defined:** 2026-07-03
**Core Value:** The five-level paradigm (Milestone → Phase → Plan → Task → Wave) runs as a real K8s orchestrator that can drive its own next milestone end-to-end. This milestone: the fixes a second external run needs to be trustworthy and reviewable.

## v1.0.7 Requirements

Requirements for this milestone. Each maps to roadmap phases.

### Pre-flight Tech-Debt Hardening (PREFLIGHT) — ✅ COMPLETE, Phase 39

Carried in from a parallel session that started a different (now-superseded) v1.0.7 scope the same day v1.0.6 shipped; landed for real before the two sessions merged. See `.planning/milestones/v1.0.7-floodtide-REQUIREMENTS.md` for the superseded milestone this was originally scoped under.

- [x] **PREFLIGHT-01**: The chart configmap default for `plannerConcurrency` is corrected from `16` to `4`, so a fresh default deploy can dispatch at most 4 concurrent planners (no latent 16-wide over-dispatch on a single node). Verified by rendering the chart and asserting the configmap value plus a controller-level assertion of the effective cap. **Satisfies DEBT-02 below.**
- [x] **PREFLIGHT-02**: The project-level rollup marker (`PlannerRolledUpUID` / equivalent) is hardened to the milestone/phase exactly-once pattern, so planning-cost rollup at the project level is exactly-once under reporter-Job TTL-GC. Verified by an envtest proving no double-count across TTL-GC at the project level. **Satisfies DEBT-01 below.**

### Run Integrity (INTEG)

- [x] **INTEG-01**: Every Succeeded task's worktree branch is integrated into the run branch — including tasks in the final Kahn wave of a plan (today `plan_controller.go:1192` skips the last wave; a single-wave plan integrates nothing)
- [x] **INTEG-02**: Wave-parallel task integrations cannot race — tasks execute in parallel, run-branch merges are serialized and idempotent (cumulative Succeeded-branch set, kernel flock, safe re-merge on retry)
- [x] **INTEG-03**: Boundary push is gated on integration completeness verified from git (`git merge-base --is-ancestor` per Succeeded task); a miss raises a sticky `integration-incomplete` condition instead of pushing an incomplete run branch
- [x] **INTEG-04**: `status.git.lastPushedSHA` is stamped on successful boundary push (the push envelope's `HeadSHA` is read in the success arm), arming the force-with-lease fence
- [x] **INTEG-05**: A kind-suite regression test reproduces the 2-parallel-task final-wave integration miss and locks the fix

### Budget Accuracy (COST)

- [x] **COST-01**: Claude 5 family models (claude-fable-5, claude-opus-4-8, claude-sonnet-5, claude-haiku-4.5) price at their real per-MTok rates via exact-ID lookup with a `-YYYYMMDD` normalizer
- [x] **COST-02**: An unknown-model pricing fallback is observable as a metric/condition, not only a GC'd pod log line
- [x] **COST-03**: The cache-write TTL multiplier is verified empirically (5m 1.25× vs 1h 2×) before the pricing table ships

### Git Base Ref (BASE)

- [ ] **BASE-01**: Operator can set `spec.git.baseRef` (branch, tag, or SHA) to base a run on a non-default ref; absent field keeps current HEAD behavior (no default marker)
- [ ] **BASE-02**: An unresolvable baseRef fails fast with a typed condition (classify-don't-retry), not a cryptic worktree-add failure
- [ ] **BASE-03**: The resolved base SHA is stamped in `status.git.baseSHA`; the field exists in both API versions with conversion round-trip and CRD upgrade-path tests

### Agent Identity (SIGN)

- [x] **SIGN-01**: TIDE agent identity (name/email) is uniformly configurable across all three commit sites — harness, integrate, tide-push — via `spec.git.agentName`/`agentEmail` → chart value → compiled-in default precedence (the tide-push hardcoded identity is removed; agent terminology replaces bot everywhere, including the compiled-in default `TIDE Agent <tide-agent@tideproject.k8s>`)

> **Descoped 2026-07-03 (Phase 36 discussion):** SIGN-02/03/04 (GPG signing, key validation, Verified-badge docs) are deferred out of v1.0.7 — the branch-protection payoff is hypothetical today and the cost (gpg-shim spike, signing-oracle key-exposure design, external UAT) is real. Moved to Future Requirements below; full analysis preserved in `.planning/phases/36-signed-commits-bot-identity/36-CONTEXT.md`.

### Prompt File (PROMPT)

- [x] **PROMPT-01**: Operator can pass `--prompt-file` to `tide apply` — the CLI inlines the file into `spec.outcomePrompt` (no CRD change; ConfigMap-ref union stays a compatible later addition)

### Dashboard Visibility (DASH)

- [x] **DASH-01**: Clicking a Planning DAG node shows the artifacts it produced, markdown-rendered (children JSON pretty-printed); gate-parked nodes surface the artifact beside the approve action
- [ ] **DASH-02**: Planning artifacts are persisted as size-capped, owner-ref'd ConfigMaps at reporter materialization time (display cache with truncation markers; PVC/git remain source of truth)
- [x] **DASH-03**: Operator can read the outcome prompt and project settings in a dashboard project view
- [x] **DASH-04**: The log drawer renders explicit loading / streaming / pod-gone states (never silently empty)

### Telemetry Setup (TELEM)

- [x] **TELEM-01**: INSTALL.md has an enable-telemetry step including the kube-prometheus-stack `release:` label fix and ending with a Targets-page verification
- [x] **TELEM-02**: Chart NOTES.txt warns when `prometheus.enabled=false` that run telemetry beyond budget is unavailable
- [x] **TELEM-03**: Dashboard shows a "telemetry disabled" banner distinguishing disabled-by-config from no-data

### Tech-Debt Carry (DEBT)

- [x] **DEBT-01**: Project-level `PlannerRolledUpUID` stamp uses the hardened RetryOnConflict + optimistic-lock pattern (v1.0.6 audit W1). **Already satisfied — see PREFLIGHT-02 (Phase 39, completed 2026-06-29).**
- [x] **DEBT-02**: Chart configmap `plannerConcurrency` default is 4, matching values.yaml (v1.0.6 audit W2). **Already satisfied — see PREFLIGHT-01 (Phase 39, completed 2026-06-29).**
- [x] **DEBT-03**: Heavy controller envtest specs move out of the TEST-01 unit tier into the integration tier, with spec-count conservation (no specs lost in the split)

### API Version Lifecycle — Phase 40 (CRANK)

Added 2026-07-06 (Phase 40 rescope discussion; IDs minted at plan time per 40-CONTEXT.md Claude's Discretion). One full version-lifecycle turn: v1alpha3 in, v1alpha1 + v1alpha2 out. Decisions locked in `.planning/phases/40-deprecate-v1alpha1-api/40-CONTEXT.md` (D-01..D-12).

- [ ] **CRANK-01**: `api/v1alpha3` exists as the copy-and-reshape of v1alpha2 — `SchemaRevision` enum `v1alpha3`, dead `ProjectSpec.ModelSelection` dropped (D-10), storageversion markers moved atomically, `LevelOverrides` docs carry the artifact-first semantics — with CRDs and the tide-crds chart regenerating reproducibly
- [ ] **CRANK-02**: Envelope contract decoupled to `dispatch.tideproject.k8s/v1alpha1` (D-08, kubeadm pattern) — the old CRD-group string is rejected under test, the tide-push/tide-eval literal drift is closed, and doc.go's superseded v1beta1 bump plan is erased
- [ ] **CRANK-03**: Every consumer (controllers, webhooks, dispatch, CLI, dashboard, Job images, tests, live fixtures) runs on v1alpha3; the SchemaRevision guard is generalized to a two-constant crank mechanism (D-04); owner-ref dual-accepts are dropped (D-05)
- [ ] **CRANK-04**: `subagent.levels` semantics renamed per the DECIDED todo mapping (D-02/D-11) — each `levels.X` key resolves the model that plans level X, implemented as override-key mapping with dispatch identity (envelope Level, Job labels, subagent template selection) unchanged; the resolved model is logged at all 5 dispatch sites
- [ ] **CRANK-05**: `api/v1alpha1` and `api/v1alpha2` deleted; 6 single-version CRD manifests; `verify-no-aggregates` hardened to a version-agnostic fail-closed glob in the same commit (D-12 mandatory); `PROJECT` metadata fixed; dogfood strict-decode coverage relocated, not lost
- [ ] **CRANK-06**: Deep docs/samples accuracy pass (D-06): migration chapter with the levels-remap table; INSTALL/gates/git-hosts/project-authoring/README examples on v1alpha3 + `schemaRevision`; 12 samples renamed with kustomization in lockstep; SECURITY.md/rbac.md conversion-webhook staleness fixed while audit snapshots stay untouched (D-12)
- [ ] **CRANK-07**: End state enforced: a CI-wired `verify-no-legacy-api-refs` gate (zero v1alpha1/v1alpha2 references outside the sanctioned exclusion set) proven alive by a seeded-failure check, and full `make test-int` green on the final tree

## Future Requirements

Deferred. Tracked but not in the current roadmap.

### Subagent Stages

- **STAGE-01**: Verify-tier LLM subagents (plan-check + level-verify) — seed `.planning/seeds/verify-level-subagent.md`; the mechanical case ships as INTEG-03
- ~~**STAGE-02**: `subagent.levels` semantic rename~~ — **FOLDED INTO Phase 40 as CRANK-04 (2026-07-06 discussion; supersedes the "own milestone" routing)**

### Provider/Caching

- **CACHE-F1**: Direct-SDK subagent backend realizing cross-pod prompt caching — `.planning/todos/pending/cache-f1-direct-sdk-cross-pod-caching.md`
- **PROV-01**: OpenAI/Codex backend + completing dogfood run #2 on multi-node infra. A detailed (but superseded on the single-node assumption) execution plan already exists from a parallel session — `.planning/milestones/v1.0.7-floodtide-REQUIREMENTS.md` and `-ROADMAP.md` (INFRA/IMPORT/RUN/REVIEW/RELEASE, 14 reqs) — re-validate infra sizing against the multi-node finding before reusing it when this milestone is planned.

### Signed Commits (deferred from v1.0.7, 2026-07-03)

- **SIGN-02**: With an optional signing-key Secret ref configured, commits at all three sites — including integrate merge commits — are GPG-signed; absent ref preserves current unsigned behavior. Requires the gpg-shim vs plumbing spike (go-git cannot sign three-way merges via `SignKey`) and the key-exposure decision (likely mount-everywhere + dedicated machine-account key) — see `36-CONTEXT.md` deferred section.
- **SIGN-03**: Signing key validated at first reconcile (armored / no passphrase / email-match triple) with a clear failure condition
- **SIGN-04**: Operator docs for the machine-account + keygen + public-key-upload Verified-badge recipe (GitHub/GitLab/Gitea); UAT is one manual push to a real GitHub repo including an integrate merge commit

## Out of Scope

Explicitly excluded. Documented to prevent scope creep.

| Feature | Reason |
|---------|--------|
| ConfigMap-ref promptFile union (`outcomePromptFrom`) | CLI-side inlining covers the need; union field is a compatible later addition |
| SSH commit signing | GPG covers all three git hosts' Verified badges; go-git `Signer` seam keeps the door open |
| Log archiving (post-GC log persistence) | Argo's multi-year bug tail; honest pod-gone state + envelope residue instead |
| Verify-tier LLM subagents | Own milestone; this milestone ships only the mechanical completeness gate (INTEG-03) |
| ~~`subagent.levels` rename~~ | **No longer out of scope — folded into Phase 40 as CRANK-04 (2026-07-06)** |
| Dashboard mutation auth hardening | Seed trigger (dashboard beyond trusted perimeter) has not fired |
| Envelope stability declaration (`dispatch.tideproject.k8s/v1`) | Deliberately NOT taken in Phase 40 — revisit once the post-rename contract has soaked (40-CONTEXT.md deferred) |

## Traceability

Which phases cover which requirements. Updated during roadmap creation.

| Requirement | Phase | Status |
|-------------|-------|--------|
| PREFLIGHT-01 | Phase 39 | Complete |
| PREFLIGHT-02 | Phase 39 | Complete |
| INTEG-01 | Phase 34 | Complete |
| INTEG-02 | Phase 34 | Complete |
| INTEG-03 | Phase 34 | Complete |
| INTEG-04 | Phase 34 | Complete |
| INTEG-05 | Phase 34 | Complete |
| BASE-01 | Phase 35 | Pending |
| BASE-02 | Phase 35 | Pending |
| BASE-03 | Phase 35 | Pending |
| SIGN-01 | Phase 36 | Complete |
| DASH-01 | Phase 37 | Complete |
| DASH-02 | Phase 37 | Pending |
| DASH-03 | Phase 37 | Complete |
| DASH-04 | Phase 37 | Complete |
| COST-01 | Phase 38 | Complete |
| COST-02 | Phase 38 | Complete |
| COST-03 | Phase 38 | Complete |
| PROMPT-01 | Phase 38 | Complete |
| TELEM-01 | Phase 38 | Complete |
| TELEM-02 | Phase 38 | Complete |
| TELEM-03 | Phase 38 | Complete |
| DEBT-01 | Phase 38 | Complete (Phase 39) |
| DEBT-02 | Phase 38 | Complete (Phase 39) |
| DEBT-03 | Phase 38 | Complete |
| CRANK-01 | Phase 40 | Pending |
| CRANK-02 | Phase 40 | Pending |
| CRANK-03 | Phase 40 | Pending |
| CRANK-04 | Phase 40 | Pending |
| CRANK-05 | Phase 40 | Pending |
| CRANK-06 | Phase 40 | Pending |
| CRANK-07 | Phase 40 | Pending |

**Coverage:**

- v1.0.7 "First-Run Paper Cuts" requirements: 30 total (23 original + 7 CRANK minted 2026-07-06), 100% mapped (2 — DEBT-01/DEBT-02 — already satisfied by the carried-in Phase 39)
- Carried-in requirements (PREFLIGHT-01/02, Phase 39): 2 total, 2 complete
- Unmapped: 0 ✓

---
*Requirements defined: 2026-07-03*
*Last updated: 2026-07-06 — minted CRANK-01..07 for Phase 40 (full API version-lifecycle turn; requirement IDs were TBD at roadmap time, minted at plan time per 40-CONTEXT.md). STAGE-02 and the "subagent.levels rename" out-of-scope row superseded by the Phase 40 fold. Previously: 2026-07-04 merge of the parallel "Flood Tide" session (PREFLIGHT carried in as Phase 39; dogfood-run-#2/OpenAI scope archived); 2026-07-03 Phase 36 descope (SIGN-02/03/04 → Future).*
