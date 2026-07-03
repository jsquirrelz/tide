# Requirements: TIDE v1.0.7 — First-Run Paper Cuts

**Defined:** 2026-07-03
**Core Value:** The five-level paradigm (Milestone → Phase → Plan → Task → Wave) runs as a real K8s orchestrator that can drive its own next milestone end-to-end. This milestone: the fixes a second external run needs to be trustworthy and reviewable.

## v1.0.7 Requirements

Requirements for this milestone. Each maps to roadmap phases.

### Run Integrity (INTEG)

- [ ] **INTEG-01**: Every Succeeded task's worktree branch is integrated into the run branch — including tasks in the final Kahn wave of a plan (today `plan_controller.go:1192` skips the last wave; a single-wave plan integrates nothing)
- [ ] **INTEG-02**: Wave-parallel task integrations cannot race — tasks execute in parallel, run-branch merges are serialized and idempotent (cumulative Succeeded-branch set, kernel flock, safe re-merge on retry)
- [ ] **INTEG-03**: Boundary push is gated on integration completeness verified from git (`git merge-base --is-ancestor` per Succeeded task); a miss raises a sticky `integration-incomplete` condition instead of pushing an incomplete run branch
- [ ] **INTEG-04**: `status.git.lastPushedSHA` is stamped on successful boundary push (the push envelope's `HeadSHA` is read in the success arm), arming the force-with-lease fence
- [ ] **INTEG-05**: A kind-suite regression test reproduces the 2-parallel-task final-wave integration miss and locks the fix

### Budget Accuracy (COST)

- [ ] **COST-01**: Claude 5 family models (claude-fable-5, claude-opus-4-8, claude-sonnet-5, claude-haiku-4.5) price at their real per-MTok rates via exact-ID lookup with a `-YYYYMMDD` normalizer
- [ ] **COST-02**: An unknown-model pricing fallback is observable as a metric/condition, not only a GC'd pod log line
- [ ] **COST-03**: The cache-write TTL multiplier is verified empirically (5m 1.25× vs 1h 2×) before the pricing table ships

### Git Base Ref (BASE)

- [ ] **BASE-01**: Operator can set `spec.git.baseRef` (branch, tag, or SHA) to base a run on a non-default ref; absent field keeps current HEAD behavior (no default marker)
- [ ] **BASE-02**: An unresolvable baseRef fails fast with a typed condition (classify-don't-retry), not a cryptic worktree-add failure
- [ ] **BASE-03**: The resolved base SHA is stamped in `status.git.baseSHA`; the field exists in both API versions with conversion round-trip and CRD upgrade-path tests

### Signed Commits (SIGN)

- [ ] **SIGN-01**: TIDE Bot identity (name/email) is uniformly configurable across all three commit sites — harness, integrate, tide-push (the tide-push hardcoded identity is removed)
- [ ] **SIGN-02**: With an optional signing-key Secret ref configured, commits at all three sites — including integrate merge commits — are GPG-signed; absent ref preserves current unsigned behavior
- [ ] **SIGN-03**: The signing key is validated at first reconcile (armored, not passphrase-protected, email-match triple) with a clear failure condition — not discovered at commit time
- [ ] **SIGN-04**: Operator docs cover the machine-account + key-generation + public-key-upload recipe for GitHub/GitLab/Gitea Verified badges

### Prompt File (PROMPT)

- [ ] **PROMPT-01**: Operator can pass `--prompt-file` to `tide apply` — the CLI inlines the file into `spec.outcomePrompt` (no CRD change; ConfigMap-ref union stays a compatible later addition)

### Dashboard Visibility (DASH)

- [ ] **DASH-01**: Clicking a Planning DAG node shows the artifacts it produced, markdown-rendered (children JSON pretty-printed); gate-parked nodes surface the artifact beside the approve action
- [ ] **DASH-02**: Planning artifacts are persisted as size-capped, owner-ref'd ConfigMaps at reporter materialization time (display cache with truncation markers; PVC/git remain source of truth)
- [ ] **DASH-03**: Operator can read the outcome prompt and project settings in a dashboard project view
- [ ] **DASH-04**: The log drawer renders explicit loading / streaming / pod-gone states (never silently empty)

### Telemetry Setup (TELEM)

- [ ] **TELEM-01**: INSTALL.md has an enable-telemetry step including the kube-prometheus-stack `release:` label fix and ending with a Targets-page verification
- [ ] **TELEM-02**: Chart NOTES.txt warns when `prometheus.enabled=false` that run telemetry beyond budget is unavailable
- [ ] **TELEM-03**: Dashboard shows a "telemetry disabled" banner distinguishing disabled-by-config from no-data

### Tech-Debt Carry (DEBT)

- [ ] **DEBT-01**: Project-level `PlannerRolledUpUID` stamp uses the hardened RetryOnConflict + optimistic-lock pattern (v1.0.6 audit W1)
- [ ] **DEBT-02**: Chart configmap `plannerConcurrency` default is 4, matching values.yaml (v1.0.6 audit W2)
- [ ] **DEBT-03**: Heavy controller envtest specs move out of the TEST-01 unit tier into the integration tier, with spec-count conservation (no specs lost in the split)

## Future Requirements

Deferred. Tracked but not in the current roadmap.

### Subagent Stages

- **STAGE-01**: Verify-tier LLM subagents (plan-check + level-verify) — seed `.planning/seeds/verify-level-subagent.md`; the mechanical case ships as INTEG-03
- **STAGE-02**: `subagent.levels` semantic rename (each key names the artifact being planned) — DECIDED but breaking; needs SchemaRevision/v1alpha3 treatment

### Provider/Caching

- **CACHE-F1**: Direct-SDK subagent backend realizing cross-pod prompt caching — `.planning/todos/pending/cache-f1-direct-sdk-cross-pod-caching.md`
- **PROV-01**: OpenAI/Codex backend + completing dogfood run #2 on multi-node infra

## Out of Scope

Explicitly excluded. Documented to prevent scope creep.

| Feature | Reason |
|---------|--------|
| ConfigMap-ref promptFile union (`outcomePromptFrom`) | CLI-side inlining covers the need; union field is a compatible later addition |
| SSH commit signing | GPG covers all three git hosts' Verified badges; go-git `Signer` seam keeps the door open |
| Log archiving (post-GC log persistence) | Argo's multi-year bug tail; honest pod-gone state + envelope residue instead |
| Verify-tier LLM subagents | Own milestone; this milestone ships only the mechanical completeness gate (INTEG-03) |
| `subagent.levels` rename | Breaking semantic remap needs SchemaRevision/v1alpha3 treatment — own milestone |
| Dashboard mutation auth hardening | Seed trigger (dashboard beyond trusted perimeter) has not fired |

## Traceability

Which phases cover which requirements. Updated during roadmap creation.

| Requirement | Phase | Status |
|-------------|-------|--------|
| INTEG-01 | Phase 34 | Pending |
| INTEG-02 | Phase 34 | Pending |
| INTEG-03 | Phase 34 | Pending |
| INTEG-04 | Phase 34 | Pending |
| INTEG-05 | Phase 34 | Pending |
| BASE-01 | Phase 35 | Pending |
| BASE-02 | Phase 35 | Pending |
| BASE-03 | Phase 35 | Pending |
| SIGN-01 | Phase 36 | Pending |
| SIGN-02 | Phase 36 | Pending |
| SIGN-03 | Phase 36 | Pending |
| SIGN-04 | Phase 36 | Pending |
| DASH-01 | Phase 37 | Pending |
| DASH-02 | Phase 37 | Pending |
| DASH-03 | Phase 37 | Pending |
| DASH-04 | Phase 37 | Pending |
| COST-01 | Phase 38 | Pending |
| COST-02 | Phase 38 | Pending |
| COST-03 | Phase 38 | Pending |
| PROMPT-01 | Phase 38 | Pending |
| TELEM-01 | Phase 38 | Pending |
| TELEM-02 | Phase 38 | Pending |
| TELEM-03 | Phase 38 | Pending |
| DEBT-01 | Phase 38 | Pending |
| DEBT-02 | Phase 38 | Pending |
| DEBT-03 | Phase 38 | Pending |

**Coverage:**
- v1.0.7 requirements: 26 total
- Mapped to phases: 26 ✓
- Unmapped: 0

---
*Requirements defined: 2026-07-03*
*Last updated: 2026-07-03 after v1.0.7 roadmap creation (traceability populated — Phases 34–38)*
