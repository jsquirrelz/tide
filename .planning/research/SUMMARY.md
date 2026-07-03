# Project Research Summary

**Project:** TIDE — v1.0.7 First-Run Paper Cuts (Run Integrity & Operator Ergonomics)
**Domain:** Kubernetes-native workflow orchestrator — brownfield fixes to a shipped controller-runtime operator
**Researched:** 2026-07-03
**Confidence:** HIGH

## Executive Summary

This milestone is not greenfield work — it is a set of surgical fixes and ergonomics additions to the shipped v1.0.6 operator, motivated by the first external run (2026-07-03). The headline is run integrity: architecture research **confirmed a structural gap** in the wave-integration machinery — the final Kahn layer of every plan is never integrated into the run branch (`plan_controller.go:1192` skips the last wave, and the plan-boundary branch-collection path is dead code called with `taskItems=nil`). A single-wave plan integrates *nothing*. This fully explains the observed "Complete with a missing deliverable" miss. The fix is not a merge queue or filesystem locking: keep the per-wave Job model, extend coverage to the final wave, pass the cumulative Succeeded-branch set (merges are idempotent), verify with `git merge-base --is-ancestor` (exit 14 → sticky `integration-incomplete` condition), and add a kernel flock as belt-and-braces. Gate the *boundary push*, not `Complete` (preserving the #13b decision).

The remaining features are small and mostly convention-following: `spec.git.baseRef` (single string field, fail-fast typed condition, stamp resolved SHA — the Argo CD `targetRevision` shape), GPG-signed bot commits (pure-Go ProtonMail/go-crypto, self-hosted-Renovate model, opt-in Secret ref), CLI-side `--prompt-file` inlining (no CRD change), a dashboard artifact view (reporter writes artifacts to ConfigMaps as a display cache; git stays truth), log-drawer tri-state, Claude 5 pricing rows, and the Prometheus setup nudge. Stack additions are minimal: one Go dep promotion (go-crypto, already indirect via go-git) and three npm packages (react-markdown, remark-gfm, @tailwindcss/typography).

The two biggest risks: (1) **signing the integrate merge commits** — go-git cannot create three-way merges, so the merge site cannot use `SignKey`; the recommended path is a pure-Go `gpg.program` shim uniform across all three commit sites, and this deserves a research-flagged phase with an ASK-FIRST scope decision on key exposure in subagent pods (a mounted signing key in an LLM-executing pod is a signing oracle). (2) **The Verified badge is a policy problem, not a crypto problem** — committer email must match key UID and the bot account's verified email at every commit site; the hardcoded tide-push identity must be unified first or the feature ships "signed but Unverified."

## Key Findings

### Recommended Stack

Almost no new stack. Existing Go 1.26 / controller-runtime / go-git v5.19.0 / React 18 + Tailwind v4 covers everything except:

**Core technologies (new):**
- `github.com/ProtonMail/go-crypto/openpgp` v1.1.6: GPG signing — promote existing indirect dep to direct; the *only* type-compatible choice for go-git's `SignKey`. Never bump independently of go-git.
- `react-markdown` v10.1.0 + `remark-gfm` v4.0.1: render LLM-authored planning artifacts XSS-safe (no `dangerouslySetInnerHTML`; do NOT add `rehype-raw`).
- `@tailwindcss/typography` v0.5.20: `prose` styling; register via CSS `@plugin` (Tailwind v4 style).
- Pricing rows (data, not deps): `claude-sonnet-5` $3/$15 (standard, not intro), keep exact-ID matching with `-YYYYMMDD` normalizer and a *logged* most-expensive fallback.

### Expected Features

**Must have (table stakes):**
- Integration-miss gate + `status.git.lastPushedSHA` — trust in `Complete` is the product
- Claude 5 pricing rows — the 2.8× overcount makes the budget meter useless
- `spec.git.baseRef` (branch/tag/SHA, fail-fast condition, `status.git.baseSHA` stamp)
- Uniform configurable bot identity across all three commit sites
- Dashboard artifact view at gated nodes (rendered markdown) + project view + log-drawer tri-state
- Telemetry-disabled nudge (NOTES.txt + INSTALL.md + dashboard banner, same command verbatim)
- promptFile via CLI-side expansion
- v1.0.6 tech-debt carry (RetryOnConflict, plannerConcurrency default 4, envtest tier split)

**Should have (differentiators):**
- GPG-signed commits with email-match-triple docs — self-hosted Renovate is the only comparable; the docs are part of the feature
- PVC-direct artifact serving (no object-store artifact repo — preserve this advantage)

**Defer (v1.x+):**
- ConfigMap-ref promptFile union, SSH signing, log archiving (Argo's experience: a tarpit), verify-tier LLM subagents

### Architecture Approach

All changes are MODIFY, no new components. Key integration facts: all git Jobs share one RWO PVC (one node — flock is sound); the manager cannot mount project PVCs (artifact view needs ConfigMap transport, written by the reporter at materialization time, owner-ref'd for GC, size-capped ~512 KiB with truncation markers); the push envelope already carries `HeadSHA` — the success arm just never reads it (one-line-class fix that also arms the inert force-with-lease fence).

**Major components touched:**
1. Plan/Project reconcilers + tide-push — final-wave coverage, cumulative merges, ancestry verify, exit 14, lastPushedSHA stamp
2. Three commit sites (harness, integrate, tide-push) — bot identity unification + gpg-shim signing
3. Clone path (`EnsureRunBranch`) — baseRef resolution with classify-don't-retry on unresolvable refs
4. Reporter + dashboard — ConfigMap artifact persistence, chi API routes, drawer UI
5. tide CLI `apply.go` — prompt-file inlining

### Critical Pitfalls

1. **Serializing at the wrong layer** — no lockfile-existence protocols on the PVC (stale-lock deadlock on OOM); serialize at the control plane, batch branches into one Job; if locking, kernel `flock(2)` only. And don't over-serialize: tasks stay parallel, only run-branch merges serialize.
2. **Non-idempotent completeness gate** — count-merge-commits breaks on retries and empty diffs; use `merge-base --is-ancestor`, always recompute from git (never trust a cached `.status` verdict).
3. **Integrate merges can't be signed via go-git** — `--no-commit` + go-git commit silently produces one-parent commits (flattens topology). Use the pure-Go gpg-shim; spike it.
4. **Signed-but-Unverified** — committer email must match key UID and account verified email at *all* sites; validate the key at first reconcile, not commit #47; reject passphrase-protected keys with a clear message; never mount the key into subagent pods without an explicit scope decision.
5. **CRD field skew** — new fields silently pruned by a stale `tide-crds` chart AND dropped by v1alpha1⇄v1alpha2 conversion unless added to both versions + round-trip tested; no `+kubebuilder:default` on baseRef (absent = HEAD, one encoding).
6. **ServiceMonitor nobody scrapes** — kube-prometheus-stack's `release:` label selector; the doc step must include the label fix and end with a Targets-page verification.
7. **Pricing undercount riding the overcount fix** — verify the CLI's cache TTL (1h writes cost 2×, not 1.25×) before choosing the multiplier; surface unknown-model fallback as a metric/condition, not pod stderr.

## Implications for Roadmap

Suggested phase structure (follows ARCHITECTURE.md Q7 ordering: dependency + risk-retirement, chart changes batched):

### Phase 1: Run Integrity — Integration-Miss Gate + lastPushedSHA
**Rationale:** The headline and the trust contract; the merge code must be stable before signing touches it. lastPushedSHA is tiny, confirmed, and arms force-with-lease for everything after.
**Delivers:** Kind-suite repro (2-parallel-task *final* wave), final-wave integration coverage, cumulative idempotent merges + flock + `--is-ancestor` verify + exit 14, project-boundary verify + sticky condition, lastPushedSHA stamp (with RetryOnConflict).
**Avoids:** P1/P2/P3 — encode "tasks parallel, merges serialized" as a success criterion.

### Phase 2: baseRef
**Rationale:** Independent, small, clone-path-only; the milestone's first CRD schema change.
**Delivers:** `spec.git.baseRef` in both API versions + conversion, `status.git.baseSHA`, classify-don't-retry unresolvable-ref condition, CRD-upgrade kind test.
**Avoids:** P8/P9/P10 (pruning, conversion drop, CEL/defaulting).

### Phase 3: Signed Commits + Bot Identity
**Rationale:** Touches the commit sites Phase 1 stabilized; batch its chart bump with Phase 2's CRD change.
**Delivers:** Identity unification (prerequisite task), gpg-shim (or spike outcome), Secret ref plumbing, email-match-triple docs, nil-key regression test.
**Avoids:** P4/P5/P6/P7. **Contains an ASK-FIRST decision:** key exposure at the harness commit site (sign-controller-sites-only vs harness restructure vs documented risk).

### Phase 4: Dashboard Surfaces — Artifact View, Project View, Log-Drawer Tri-State
**Rationale:** Three features sharing one read-only manager-API surface; UI last because it consumes the reporter's ConfigMap contract. Fix log-drawer empty states first/together.
**Delivers:** Reporter → ConfigMap persistence (capped, owner-ref'd, cache-selectored), chi routes, rendered-markdown artifact drawer at gate-parked nodes, explicit loading/streaming/pod-gone log states.
**Avoids:** P11 (etcd caps, informer blow-up, GC), read-only-dashboard decision (no reader pods).

### Phase 5: Small Independents — Pricing, promptFile, Telemetry Nudge, Tech-Debt Carry
**Rationale:** Fully independent filler; can slot anywhere or split across earlier waves.
**Delivers:** Sonnet-5 pricing rows + unknown-model metric + drift-check extension + cache-TTL verification; CLI `--prompt-file`; NOTES.txt/INSTALL/banner triple with Targets-page verification; RetryOnConflict, plannerConcurrency=4 (via chart bump), envtest tier split with spec-count conservation.
**Avoids:** P12/P13.

### Phase Ordering Rationale

- Integration gate first: highest-value, and signing must land on stable merge code (both touch the same three commit sites).
- baseRef before signing so the two CRD/chart changes batch into one chart version bump (FIXED-contract rule).
- Dashboard last among the big items: it consumes the reporter ConfigMap contract and reuses the drawer surface the log fix repairs.
- promptFile/pricing/telemetry are order-independent.

### Research Flags

Phases likely needing deeper research during planning:
- **Phase 3 (Signed commits):** flag `research: true` — gpg-shim vs plumbing-level merge-commit spike; key-exposure scope decision is ASK-FIRST.
- **Phase 5 pricing sub-item:** one empirical check — which cache TTL the `claude` CLI uses (tee a request via the credproxy) before fixing the write multiplier.

Phases with standard patterns (skip research-phase):
- **Phase 1:** mechanism decided; defect sites confirmed at file:line.
- **Phase 2:** ecosystem-standard shape (Argo CD `targetRevision`); plumbing chain fully mapped.
- **Phase 4:** transport decision made (ConfigMap); acceptance checklist in PITFALLS P11.
- **Phase 5:** docs/data/CLI work with known patterns.

## Confidence Assessment

| Area | Confidence | Notes |
|------|------------|-------|
| Stack | HIGH | Every claim verified against primary sources 2026-07-03 (go-git source, go.mod, npm registry, live Anthropic pricing docs) |
| Features | MEDIUM-HIGH | Git-host verification rules HIGH (official docs); dashboard/UX conventions MEDIUM (ecosystem survey) |
| Architecture | HIGH | All claims grounded in direct v1.0.6 source reads at cited file:line; final-wave gap CONFIRMED structurally |
| Pitfalls | HIGH | Codebase-grounded + pricing HIGH; web-verified ecosystem claims MEDIUM |

**Overall confidence:** HIGH

### Gaps to Address

- **Exact race mechanism of the observed miss (MEDIUM):** the structural final-wave gap is confirmed, but attribution to the specific 2026-07-03 miss needs the kind-suite repro to pin the layer layout — build the repro first in Phase 1.
- **Claude CLI cache-write TTL:** unverified whether the dispatch surface uses 5m or 1h TTL; determines the 1.25× vs 2× multiplier. Settle with one teed request before the pricing fix ships.
- **Verified badge end-to-end:** `git verify-commit` in-cluster does not prove the badge; UAT must include one manual push to a real GitHub repo including an integrate merge commit.
- **kubectl-parity for promptFile:** CLI-only route accepted for v1.0.7; the ConfigMap union stays a compatible later addition.

## Sources

### Primary (HIGH confidence)
- Direct v1.0.6 source reads @ main 9344358: `pkg/git/*`, `internal/controller/{plan_controller,project_controller,boundary_push,push_helpers}.go`, `cmd/tide-push/main.go`, `cmd/tide/apply.go`, `cmd/dashboard/*`, `api/v1alpha1|2/`, `internal/subagent/anthropic/pricing.go`, `internal/harness/*`, `internal/reporter/materialize.go`, `charts/`
- go-git `options.go` (fetched 2026-07-03) — SignKey contract; `go.mod` — go-crypto v1.1.6 indirect
- platform.claude.com pricing docs (fetched live 2026-07-03) + claude-api skill — full pricing table, Sonnet 5 intro window, cache multipliers
- npm registry (2026-07-03) — react-markdown 10.1.0, remark-gfm 4.0.1, @tailwindcss/typography 0.5.20
- GitHub/GitLab/Gitea signature-verification docs; Kubernetes ConfigMap 1 MiB limit
- `.planning/todos/pending/2026-07-03-*.md`, `.planning/STATE.md` — first-run evidence

### Secondary (MEDIUM confidence)
- Argo CD/Workflows, Tekton, Dependabot, Renovate docs + issue trails — baseRef conventions, artifact/log fallback bug tails, self-hosted signing recipe
- KEP-4008 CRD ratcheting; prometheus-community #1631/#2381 — ServiceMonitor selector trap
- Tailwind v4 `@plugin` registration (community-confirmed, matches peerDeps)

### Tertiary (LOW confidence)
- None load-bearing; per-claim confidence noted in the four research files.

---
*Research completed: 2026-07-03*
*Ready for roadmap: yes*
