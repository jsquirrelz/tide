---
phase: 03-up-stack-reconcilers-git-integration-real-subagent-resumptio
plan: 09
subsystem: infra
tags: [helm, kubernetes, rbac, controller-runtime, claude, gitleaks, leader-election, env-wiring, docs]

# Dependency graph
requires:
  - phase: 03-up-stack-reconcilers-git-integration-real-subagent-resumptio
    provides: "ProjectReconciler.TidePushImage field (03-08), push_helpers.buildPushJob (03-06/03-08), HelmProviderDefaults on Milestone/Phase/Plan reconcilers (03-07), Claude subagent image + tide-push image build pipelines (waves 3-4)"
provides:
  - "cmd/manager reads Helm env vars (TIDE_PUSH_IMAGE, CLAUDE_SUBAGENT_IMAGE, TIDE_DEFAULT_MODEL_{MILESTONE,PHASE,PLAN,TASK}, TIDE_LEADER_{LEASE,RENEW,RETRY}_SECONDS)"
  - "Helm values.yaml exposes subagent/images.tidePush/images.claudeSubagent/gitleaks/leaderElection top-level keys with D-C4 defaults"
  - "charts/tide/templates/push-rbac.yaml: tide-push ServiceAccount + namespace-scoped Role + RoleBinding granting `secrets get` only (T-304 mitigation)"
  - "docs/git-hosts.md: ART-02 multi-host (GitHub/GitLab/Gitea) PAT recipes + ART-05 SSH caveats"
  - "Augment script (hack/helm/augment-tide-chart.sh) preserves Phase 3 env block + push-rbac.yaml across helmify regenerations"
affects:
  - "Phase 3 plan 03-10 (kind smoke / chaos-resume integration — depends on these values being deployed in test clusters)"
  - "Future Phase 4 dashboard install (chart-level Phase 5 readiness)"
  - "Operator-facing first-install experience for any TIDE deployment from v1.0 onward"

# Tech tracking
tech-stack:
  added:
    - "Helm template env-var injection pattern (phase3-env-injected marker)"
    - "controller-runtime LeaderElection timing override via env vars (D-D1 chaos-resume tuning)"
    - "Go test pattern for static-artifact contracts (cmd/manager/rbac_docs_test.go)"
  patterns:
    - "envOrDefault / atoiOrDefault helpers in cmd/manager/env.go — testable env-reader pattern for Helm-driven config"
    - "Per-level model defaults map (D-C4) flowing through controller.ProviderDefaults"
    - "hack/helm/<file>.yaml → augment script cp → charts/tide/templates/<file>.yaml flow for new chart resources (mirrors serviceaccount-subagent.yaml flow)"
    - "Push-job RBAC scoped to namespace + single `secrets get` verb — T-304 mitigation contract"

key-files:
  created:
    - "cmd/manager/env.go — env-var reader helpers (envOrDefault, atoiOrDefault, resolvePerLevelModels, tideHelmProviderDefaults, resolveLeaderElectionTiming)"
    - "cmd/manager/env_test.go — 6 unit tests for env helpers"
    - "cmd/manager/rbac_docs_test.go — 9 artifact-content contract tests (push-rbac.yaml + docs/git-hosts.md)"
    - "charts/tide/templates/push-rbac.yaml — tide-push SA + Role + RoleBinding"
    - "hack/helm/push-rbac.yaml — source-of-truth for push-rbac.yaml (mirrors serviceaccount-subagent.yaml flow)"
    - "docs/git-hosts.md — ART-02 + ART-05 deliverable (186 lines, 7 H2 sections)"
  modified:
    - "cmd/manager/main.go — reads new env vars, injects into Milestone/Phase/Plan/Project reconcilers + ctrl.Manager.Options leader-election timings"
    - "charts/tide/values.yaml — added subagent/gitleaks/leaderElection top-level keys + images.tidePush + images.claudeSubagent (ADDITIVE, no removals)"
    - "hack/helm/tide-values.yaml — mirror of charts/tide/values.yaml (source-of-truth pair)"
    - "charts/tide/templates/deployment.yaml — added env block injecting Phase 3 env vars before envFrom"
    - "hack/helm/augment-tide-chart.sh — added Section 8e (phase3-env-injected marker) + Section 6a (push-rbac.yaml cp)"

key-decisions:
  - "Factor env-var reading into cmd/manager/env.go (instead of inline in main.go) to make the wiring unit-testable — main() itself is unreachable from tests without spinning up controller-runtime Manager."
  - "tide-push SA created in .Release.Namespace (default tide-system) with namespace-scoped Role, NOT ClusterRole — preserves namespace boundary > convenience. Cross-namespace caveat documented in-file + values.yaml comment + docs/git-hosts.md."
  - "envOrDefault treats empty string as unset — a Helm value rendered as `value: \"\"` cleanly falls through to the binary's compile-time default rather than overriding it with empty string."
  - "atoiOrDefault tolerates non-integer env values (returns fallback) — a stray non-numeric value in the chart cannot crash-loop the controller."
  - "SSH deferred to v1.x with explicit reasoning in docs/git-hosts.md (host-key fussiness + agent-forwarding-not-container-native + workaround documentation) per ART-05."

patterns-established:
  - "Helm env-var pattern for controller config: hack/helm/<file>.yaml + augment script + chart template + cmd/manager helper + go test. This is now the canonical shape for any future controller config that the operator should be able to tune via Helm."
  - "Test pattern for static-artifact contracts: cmd/manager/rbac_docs_test.go demonstrates testing helm-templated files + markdown docs from Go without shelling out — readFile via filepath.Join(repoRoot, relPath), regex/strings.Contains for content assertions."
  - "Augment script idempotence marker pattern: each Phase's injected content uses a comment marker (phase2-args-injected, phase2-vmount-injected, phase3-env-injected, etc.) so the script can detect prior injection and skip re-applying."

requirements-completed: [ART-02, ART-05, AUTH-01]

# Metrics
duration: 36 min
completed: 2026-05-16
---

# Phase 03 Plan 09: Helm Env Wiring + tide-push RBAC + Multi-Host Git Docs Summary

**cmd/manager reads 9 Helm env vars (TIDE_PUSH_IMAGE, CLAUDE_SUBAGENT_IMAGE, TIDE_DEFAULT_MODEL_{4 levels}, TIDE_LEADER_{LEASE,RENEW,RETRY}_SECONDS) and ships tide-push namespace-scoped RBAC + 186-line ART-02 multi-host git docs.**

## Performance

- **Duration:** ~36 min
- **Started:** 2026-05-16T01:10:00Z (approx)
- **Completed:** 2026-05-16T01:46:14Z
- **Tasks:** 2 (each TDD: RED→GREEN)
- **Commits:** 4 (2 test + 2 feat)
- **Files modified:** 11 (8 net-new + 3 modified)

## Accomplishments

- **cmd/manager Helm wiring:** Manager reads `TIDE_PUSH_IMAGE`, `CLAUDE_SUBAGENT_IMAGE`, four per-level model env vars (`TIDE_DEFAULT_MODEL_{MILESTONE,PHASE,PLAN,TASK}`), and three leader-election timing env vars (`TIDE_LEADER_{LEASE,RENEW,RETRY}_SECONDS`). Defaults match D-C4 (`milestone→opus-4-7`, `phase/plan→sonnet-4-6`, `task→haiku-4-5`) and D-D1 (15s/10s/2s — single-pod ≤25s failover ceiling).
- **Helm values.yaml extended additively:** Three new top-level keys (`subagent`, `gitleaks`, `leaderElection`) + two new `images` entries (`tidePush`, `claudeSubagent`). Every existing Phase 1/2 key preserved verbatim — no reductions (per CLAUDE.md anti-pattern: binary catches up to chart, never reverse).
- **tide-push namespace-scoped RBAC:** `charts/tide/templates/push-rbac.yaml` creates a dedicated `tide-push` ServiceAccount + Role + RoleBinding. Role grants only `secrets get` — no list, no watch, no wildcards, no write verbs (T-304 mitigation: compromised push Job pod cannot enumerate Secrets or read Secrets in other namespaces).
- **ART-02 + ART-05 docs:** `docs/git-hosts.md` (186 lines, 7 H2 sections) ships HTTPS+PAT default explanation, per-host PAT recipes for GitHub/GitLab/Gitea, K8s Secret setup, manual smoke verification commands, and a three-reason SSH deferral block per ART-05.
- **Augment script idempotence:** `hack/helm/augment-tide-chart.sh` updated with Section 6a (cp push-rbac.yaml) and Section 8e (phase3-env-injected marker) so a future `make helm-controller` regeneration preserves all Phase 3 wiring.

## Task Commits

Each task was committed atomically via TDD (RED → GREEN):

**Task 1: cmd/manager env wiring + values.yaml + manager-deployment.yaml**
1. `e639939` — `test(03-09): add failing tests for cmd/manager env helpers` (RED)
2. `3fbd936` — `feat(03-09): wire Helm env vars for push image, claude image, per-level models, leader election` (GREEN)

**Task 2: charts/tide/templates/push-rbac.yaml + docs/git-hosts.md**
3. `6eed9ff` — `test(03-09): add failing tests for push-rbac.yaml + docs/git-hosts.md` (RED)
4. `4164078` — `feat(03-09): add tide-push RBAC and ART-02 multi-host git docs` (GREEN)

## Files Created/Modified

**Created:**
- `cmd/manager/env.go` — env-var reader helpers (5 functions: envOrDefault, atoiOrDefault, resolvePerLevelModels, tideHelmProviderDefaults, resolveLeaderElectionTiming).
- `cmd/manager/env_test.go` — 6 unit tests for env helpers (env-set/unset/garbage paths + D-C4 default + override).
- `cmd/manager/rbac_docs_test.go` — 9 contract tests over `push-rbac.yaml` + `docs/git-hosts.md` (file existence, kinds present, SA name, least-privilege verbs, namespace scoping, doc H2 sections, REQ-ID citations, GIT_PAT documentation).
- `charts/tide/templates/push-rbac.yaml` — tide-push ServiceAccount + Role + RoleBinding template.
- `hack/helm/push-rbac.yaml` — source-of-truth copy (augment script cps into chart).
- `docs/git-hosts.md` — ART-02/ART-05 multi-host git host docs.

**Modified:**
- `cmd/manager/main.go` — reads env vars, injects into reconciler structs, threads LeaderElection timings into `ctrl.Options`.
- `charts/tide/values.yaml` — additive: new `subagent.{defaults,levels}`, `gitleaks.configMapName`, `leaderElection.{leaseDurationSeconds,renewDeadlineSeconds,retryPeriodSeconds}`, `images.tidePush`, `images.claudeSubagent`.
- `hack/helm/tide-values.yaml` — mirror of `charts/tide/values.yaml` (source-of-truth pair).
- `charts/tide/templates/deployment.yaml` — added env block injecting Phase 3 env vars before `envFrom:`.
- `hack/helm/augment-tide-chart.sh` — added Section 6a (cp push-rbac.yaml) + Section 8e (phase3-env-injected marker).

## Decisions Made

- **Factor env-var reading into a separate file** (`cmd/manager/env.go`) instead of inlining in `main.go`. Rationale: `main()` is not unit-testable because it constructs the controller-runtime Manager and blocks on `mgr.Start()`. Factoring helpers out makes the env-reading wiring testable as pure functions.
- **tide-push SA is namespace-scoped, not ClusterRole.** Rationale: T-304 mitigation requires the SA's blast radius to be bounded by the namespace boundary. Cross-namespace caveat (push Jobs in Project namespaces vs. SA in tide-system) is documented in-file + in `values.yaml` comment + in `docs/git-hosts.md` so operators have three places to find it.
- **`envOrDefault` treats empty string as unset.** A Helm value rendered as `value: ""` cleanly falls through to the binary's compile-time default. Avoids the trap where an operator setting `--set images.tidePush.tag=""` would render `TIDE_PUSH_IMAGE=":'"`-shaped env (with a colon and empty tag) and crash the controller at first push.
- **`atoiOrDefault` tolerates non-integer values.** Returns fallback for any parse error. A stray non-numeric value in the chart (e.g. `leaseDurationSeconds: "fifteen"`) cannot crash-loop the controller — it falls back to the production default 15s. controller-runtime still validates `lease > renew > retry` at Manager construction time.
- **SSH deferred to v1.x with three documented reasons** (host-key fussiness, agent-forwarding-not-container-native, workaround for SSH-only hosts). Rationale: HTTPS+PAT is host-agnostic and the v1.0 default; SSH would require a host-key-management story that doesn't have a clean Helm-chart surface yet. Per ART-05.

## Deviations from Plan

None — plan executed exactly as written, including the TDD-cycle structure for both tasks.

## Issues Encountered

- **`kubectl apply --dry-run=client` requires cluster connectivity** for OpenAPI validation. Locally, no cluster is running, so the K8s manifest validation path produced "unable to recognize" connection errors. **Resolution:** Verified the manifests render as valid YAML offline via Python `yaml.safe_load_all` (37 manifests parsed successfully) + `helm template` exit 0 + `helm lint` clean. The Go contract tests assert per-resource invariants (SA/Role/RoleBinding kinds, name, verbs) without needing a live API server.
- **envtest BeforeSuite fails** (`/usr/local/kubebuilder/bin/etcd` missing on this workstation). Pre-existing environmental issue, NOT caused by this plan. Pure-unit-test paths (TestResolveProvider, TestPushHelpers, cmd/manager tests) all pass. The envtest suite is exercised in CI / kind smoke tests, which are downstream of this plan.

## User Setup Required

None — chart values are operator-facing but ship with safe defaults (D-C4 per-level models, D-D1 leader timing). Operators who want to override the defaults can `--set` any of the new keys (`subagent.levels.task.model`, `leaderElection.leaseDurationSeconds`, `images.tidePush.tag`, etc.).

## Next Phase Readiness

- **Plan 03-10 (kind smoke / chaos-resume integration) can now consume** the deployed chart with real D-C4 model defaults wired through the controller. The `TIDE_LEADER_*_SECONDS` knobs are particularly relevant for chaos-resume (D-D1) — Plan 03-10 can set aggressive timings (e.g. 5s lease) to compress test cycles.
- **`make helm-controller` regeneration is safe** — the augment script preserves all Phase 3 wiring (push-rbac.yaml cp + phase3-env-injected marker) idempotently.
- **No outstanding blockers for the phase.** All success criteria pass; chart renders 37 manifests cleanly; go build + go vet + cmd/manager tests all green.

## Self-Check

**Created files verified:**
- `cmd/manager/env.go` — FOUND
- `cmd/manager/env_test.go` — FOUND
- `cmd/manager/rbac_docs_test.go` — FOUND
- `charts/tide/templates/push-rbac.yaml` — FOUND
- `hack/helm/push-rbac.yaml` — FOUND
- `docs/git-hosts.md` — FOUND

**Commits verified in git log:**
- `e639939` (test RED Task 1) — FOUND
- `3fbd936` (feat GREEN Task 1) — FOUND
- `6eed9ff` (test RED Task 2) — FOUND
- `4164078` (feat GREEN Task 2) — FOUND

**Self-Check: PASSED**

---
*Phase: 03-up-stack-reconcilers-git-integration-real-subagent-resumptio*
*Completed: 2026-05-16*
