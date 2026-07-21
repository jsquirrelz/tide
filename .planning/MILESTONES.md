# Milestones

## v1.0.9 Slack Tide — The Task Loop (Shipped: 2026-07-21, tag `v1.0.9`)

**Delivered:** TIDE closes its first real feedback loop — independent verification drives Task iteration, parameterized per level by one shared `LoopPolicy` contract, configured chart-first with a safe default posture, and surfaced on the dashboard.

**Phases completed:** 6 phases (48–53), 46 plans, 98 tasks · 28/28 requirements · ~4 days (2026-07-18 → 2026-07-21) · 354 commits, 339 files, +58.6k/−0.9k LOC

**Key accomplishments:**

- **The Task loop is real** — each Task's artifact is checked by an independent read-only LangGraph evaluator; REPAIRABLE drives fresh evidence-seeded attempts (never the prior agent's context), bounded by `LoopPolicy`, exhausting to `ConditionVerifyHalt`/`requireApproval` — proven by live billable red/green runs on kind (Phases 51 + 52).
- **One verification contract parameterized per level** — a single `ResolveLoopPolicy` resolver keyed on loop level (never CRD kind): Task auto-repair (maxIterations:N), plan-check re-plan (1, findings-seeded, severity-weighted stall detection), Phase/Milestone/Project escalation (clamped 0); per-value `onExhaustion` split (park-for-approval vs project-wide halt).
- **The LangGraph successor-runtime beachhead shipped** — `tide-langgraph-verifier`, the first non-CLI runtime behind the unchanged `pkg/dispatch.Subagent` envelope seam; read-only enforced structurally at three layers (mount/credentials/rootfs), credproxy-TLS trust proven live (`SSL_CERT_FILE` alone), `"langgraph"` vendor self-instruments (no double spans).
- **Fail-closed as a system property** — verdict classification (unparseable → BLOCKED never APPROVED), deterministic gate-command failure dominates the LLM judge (enforced in image AND controller), `TerminalReason` never a silent default, config parsing exits on malformed input, posture typos fail at render; anti-gaming: an attempt that edits the evaluator is a system escalation, never a pass.
- **Execution-loop evidence + loop-native observability** — derived `loopRunID`/`attemptID` (never minted), bounded references-only `RunEvidence`, `loop.*`/`evaluation.*`/`human_intervention` span keys on the v1.0.8 trace spine, EVALUATOR sibling spans, run-IDs structurally barred from Prometheus labels (dual static+runtime guard).
- **Chart-first config + dashboard provenance** — the verify tier configures through the `subagent.levels` precedence chain with a sticky install-ON/upgrade-OFF posture (lookup+keep marker; proven by a 5-leg render pair AND a live kind install→upgrade test); the dashboard shows nested loop provenance (current-iteration summary + findings via the existing artifacts API + Phoenix deep-links) with `VerifyHalt` visually distinct from `Failed`.

**Quality loop (the milestone practicing what it ships):** every phase ran code review + goal-backward verification; reviews caught and root-fixed 1 Critical + 20+ warnings across the milestone — including five stacked latent defects in the live Task-loop path (the ship-blocker verdict relay), two live-proof defects at plan level, and a namespace-dropped findings fetch that would have 404'd every real install. Three in-flight gap plans were authored mid-execution when execution surfaced missing links (the verdict-final push trigger, the verifier findings.json producer).

**Known deferred items at close:** 31 (see STATE.md Deferred Items) — 24 SUMMARY-frontmatter bookkeeping quick-tasks, 2 deliberately-deferred todos (GPG signing, CACHE-F1), 2 debug entries (KB file + tracked CI flake class), 3 approved-UAT bookkeeping artifacts (51/53 HUMAN-UAT + 53-VERIFICATION `human_needed`, live items operator-approved).

**Released 2026-07-21** (tag `v1.0.9` at `97dcc5d9`, rc-gated via `v1.0.9-rc.1` — green on the first candidate): goreleaser 5 platform binaries + 9 images (the langgraph-verifier joins the set) + 2 Helm OCI charts published to GHCR, all verified anon-pullable; published chart appVersion 1.0.9 confirmed (no version skew). Release pre-flight surfaced + root-fixed real drift: the Anthropic pricing page moved (drift-check URL repointed) and claude-sonnet-5 now bills intro rates — adopted (200/1000/20/250 ¢/MTok), making the 2026-07-03 run-mix replay land on exactly the $3.84 console actual (COST-01 gap now zero); the weekly drift automation structurally catches the post-2026-08-31 sticker bump-back.

**Archives:** [v1.0.9-ROADMAP.md](milestones/v1.0.9-ROADMAP.md) · [v1.0.9-REQUIREMENTS.md](milestones/v1.0.9-REQUIREMENTS.md)

## v1.0.8 Phoenix Rising — OpenInference Trace Emission + Self-Hosted Phoenix (Completed: 2026-07-17)

**Phases completed:** 6 phases (42–47), 32 plans, 67 tasks
**Timeline:** 2026-07-15 → 2026-07-17 · 240 commits · 229 files (+34.8k/−343)
**Acceptance:** PROOF-01 human-signed-off at close — a live $0.88 `medium-project` run on a from-the-docs kind cluster produced a queryable **392-span, five-level** OpenInference trace tree (trace `e9124906…`) in an auth-ON self-hosted Phoenix. Phase 47 is the milestone's live E2E acceptance proof; no separate `/gsd:audit-milestone` was run.
**Release status:** **RELEASED 2026-07-17** — tag `v1.0.8` at `6e5b8f8`, rc-gated via `v1.0.8-rc.3`; goreleaser 5 platform binaries + 8 component images + 2 Helm OCI charts published to GHCR (all verified anon-pullable). Release pre-flight caught + fixed 5 latent `ci.yaml`-gate issues the phase verification missed (stale dashboard embed, 9 lint offenses, example subagent pin skew, dashboard test flake ×2).
**Known deferred items at close:** 30 acknowledged (see STATE.md Deferred Items) — carried-forward bookkeeping: 24 quick-task `status:`-field false-positives (work shipped), 2 stale debug files (a KB + an old PR9 flake), and 4 todos (incl. 2 dispatch-gate correctness notes + cache-F1/vNext).

**Key accomplishments:**

- **Five-level trace tree (the headline):** the Milestone→Phase→Plan→Task dispatch chain emits real OpenInference `AGENT` spans — deterministic TraceID from `Project.UID`, W3C `traceparent` propagated at both pod hops (subagent + reporter), correct parent-child nesting across all five levels, and each level's span IDs persisted in `.status.trace`. Pure `pkg/otelai/tracecontext.go` (zero K8s imports); attribute-complete (model / provider / token counts) for succeeded and failed Jobs alike.
- **LLM message-array spans, safely (the highest-risk phase):** the reporter's new trace-only mode turns a Task's `events.jsonl` into redacted, size-bounded OpenInference LLM spans — redact-before-truncate (proven with a straddling-secret test), aggregate-batch DoS bounded (`OTEL_BSP_MAX_EXPORT_BATCH_SIZE=6` + a 32-span test), and the one-shot `tide-reporter`'s TracerProvider `Shutdown` proven to fire on every exit path.
- **Attribute completeness via official semconv:** every `pkg/otelai` attribute key resolves from the `openinference-semantic-conventions` Go module (no hand-rolled strings), so TIDE's spans already match what `openinference-instrumentation-langchain` will emit natively — the runtime-migration survival lever.
- **Runtime-neutral adapter seam:** a fail-closed `SelfInstruments` vendor-capability flag travels as data (from resolved `Provider.Vendor`) and lets the reporter skip synthesis for a self-instrumenting runtime, with a contract test proving zero duplicate spans — forward-compat scaffolding for the LangGraph beachhead, byte-identical behavior today.
- **Operator observability + dashboard deep link:** sampler default 0.1→1.0, `session.id` = Project UID (an independent Phoenix cost cross-check against TIDE's budget tally), `metadata`/`tag.tags` enrichment for DSL filtering, and a shared `<PhoenixTraceLink>` deep link from every Planning/Execution DAG node (renders nothing when unconfigured — no dead buttons).
- **Self-hosted Phoenix, documented and proven:** INSTALL.md / observability.md cover both storage paths (PVC-SQLite + bundled Postgres), the chart's auth-ON default, and the full OTLP-headers `secretKeyRef` forwarding chain; the live proof captured the trace tree as milestone-close evidence, and the two defects the run surfaced (boundary-push stale-lease; reporter double-spawn → partial enrichment) were root-fixed, not worked around.

---

## v1.0.7 First-Run Paper Cuts: Run Integrity & Operator Ergonomics (Shipped: 2026-07-15)

**Phases completed:** 8 phases (34–41, incl. folded-in 39), 51 plans
**Timeline:** 2026-06-29 → 2026-07-15 · 193 commits · 951 files (+60.6k/−61.8k — net-negative from the Phase 40 API deletion)
**Audit:** tech_debt — 44/44 requirements satisfied, 0 blockers ([milestones/v1.0.7-MILESTONE-AUDIT.md](milestones/v1.0.7-MILESTONE-AUDIT.md)) · full `make test-int` green at close (envtest 56/56, kind 26/26, MAKE_EXIT=0)
**Known deferred items at close:** 30 acknowledged (see STATE.md Deferred Items) — mostly historical bookkeeping; substantive carries are the W-2 dispatch-gate divergence todos, W-4 CRD pattern-lock re-establishment, and the Phase 36 'bot' vocabulary residue.

**Key accomplishments:**

- **Run integrity (the headline):** a pushed run branch provably contains every Succeeded task's work — final-wave integration fixed, wave-parallel merges serialized behind kernel flock, boundary push gated on `git merge-base --is-ancestor` completeness with a sticky `integration-incomplete` condition, `status.git.lastPushedSHA` stamped and armed as the force-with-lease fence, and a kind regression test reproducing the original 2-parallel-task miss.
- **Budget tally matches the provider console:** Claude 5 family models price at real per-MTok rates via exact-ID lookup + `-YYYYMMDD` normalizer (the 2.8× first-run overcount is a pinned regression test), the cache-write multiplier was empirically probed (125/100, 5m TTL), and unknown-model fallback is now observable as a durable condition + Prometheus counter.
- **Dashboard is a sufficient approve-gate review surface:** planning artifacts stage to the run branch (`.tide/planning/`, git-as-artifact-store — reworded from the ConfigMap design mid-phase) and render markdown in a node artifact viewer beside the approve action; a project settings view (secret names only, never values); honest log-drawer loading/streaming/pod-gone states — no more ad-hoc PVC reader pods. Verified live in an 8/8 autonomous Playwright UAT.
- **Full API version-lifecycle crank:** v1alpha3 is the sole served+storage version for all 6 CRDs — `subagent.levels` semantic rename (closes the silent top-level-model fallback), envelope contract decoupled to `dispatch.tideproject.k8s/v1alpha1`, v1alpha1+v1alpha2 Go packages deleted (~330 files repointed), and a CI-wired `verify-no-legacy-api-refs` gate proven alive by seeded failure.
- **Git ergonomics:** `spec.git.baseRef` (branch/tag/SHA) with typed fail-fast on unresolvable refs and `status.git.baseSHA` stamping; agent identity uniformly configurable at all three commit sites (`spec.git.agentName`/`agentEmail` → chart → compiled `TIDE Agent <tide-agent@tideproject.k8s>`); `tide apply --prompt-file`.
- **12-item refactoring review landed as non-breaking cleanup:** typed LevelPhase constants (~117 sites), shared `checkDispatchHolds`/`PlannerReconcilerDeps` extractions, `ConditionParentUnresolved` polarity fix, one envtest retry-driver family (~59 call sites repointed), and label/PVC-name centralization that made the latent dead `--workspaces-pvc-name` flag live end-to-end.

---

## v1.0.6 Adoption-Path Correctness & Dispatch Safety (Shipped: 2026-06-29)

**Phases completed:** 3 phases, 8 plans, 12 tasks

**Key accomplishments:**

- Contract-first wave: ConditionProjectPlannerSuppressed (D-01) + MilestoneRolledUpUID / PhaseRolledUpUID / PlanRolledUpUID scalar markers (D-03) added to api/v1alpha2 with regenerated CRD YAML and Helm chart templates
- Adoption lifecycle seam: adopted Project advances Initialized→Running with zero project-planner Jobs dispatched, durably suppresses re-dispatch via ConditionProjectPlannerSuppressed, and refuses planner dispatch via ConditionBudgetBlocked on an over-cap adopted Project
- Exactly-once child budget rollup gated on durable MilestoneRolledUpUID / PhaseRolledUpUID / PlanRolledUpUID markers (D-03/D-03a), proven by three Ginkgo envtest specs (ADOPT-02 accrual + ADOPT-04 no double-count across TTL-GC)
- D3 dispatch concurrency cap (Phase 32): live in-flight planner-count gate before pool acquire at all four dispatch sites with a single-node-safe default (plannerConcurrency=4, down from 16); excess dispatches park/requeue, planner and executor pools stay separately sized
- Carried-in hardening (Phase 32): RetryOnConflict + optimistic-lock on the child-level *RolledUpUID marker stamps (WR-02/WR-03); softened the chart plannerConcurrency sizing-policy comment (D-04, value held at 4 for single-node safety)
- D4 planner failure semantics (Phase 33): patchPhaseFailed/patchMilestoneFailed helpers + shared isPlannerFailure guard wired at both phase and milestone succession sites BEFORE the gate-policy hook (CR-01 fix), closing the false-leaf DAG-corruption bug — locked by PLANFAIL-01/02/03 envtests (run under the production approve gate) and a PLANFAIL-04 resume-recovery test

**Audit:** tech_debt — 13/13 requirements satisfied, 0 blockers, 2 verified tech-debt items deferred to v1.0.7 (project-level PlannerRolledUpUID hardening; configmap `default 16`→`4`). See milestones/v1.0.6-MILESTONE-AUDIT.md.
**Released:** tag `v1.0.6` — 8 component images + 2 OCI charts + 5 binaries @ 1.0.6, verified anon on ghcr; GitHub Release live.
**Known deferred items at close:** 22 stale open artifacts (20 historical Phase-02/03 quick-tasks + 1 todo + 1 uat_gap) — see STATE.md Deferred Items.

---

## v1.0.5 Resumable Import: Partial-Tree Resume (Shipped: 2026-06-27)

This close archives all planning work since v1.0.1 — **Phases 22–30, shipped across three release tags** (v1.0.3 Spring Tide + resumption, v1.0.4 image patch, v1.0.5 partial-tree resume). The headline is making the Topologically-Indexed paradigm real (one global Execution DAG) and making a halted run cheaply resumable.

**Scope:** Phases 22–30 · ~36 plans · published tags v1.0.3 / v1.0.4 / v1.0.5.

**Key accomplishments:**

- **Global Execution DAG (Spring Tide, Phases 22–26):** re-architected execution off per-plan waves onto ONE global DAG spanning all Milestones/Phases/Plans — v1alpha2 schema migration (Wave re-owned Plan→Project), global layered-Kahn wave derivation (no cached schedule), global dispatch + wave-boundary failure semantics + gates-as-holds + resumption, multi-milestone drive, and a README-pinned spec-conformance envtest deriving `[{α,β,γ,ζ},{δ,η},{ε,θ}]` with cross-milestone edges honored.
- **Budget-bypass resume correctness (Phase 27):** durable `CloneComplete` / `PlannerRolledUpUID` / `BypassBaselineCents` status fields — a budget halt resumes at Running with no re-clone, planning cost rolls up exactly once across halt→resume (even after reporter-Job TTL-GC), and raising the absolute cap alone makes a resume stick.
- **Plan-import core (Phase 28):** `cmd/tide-import` + `ImportController` adopt pre-authored envelopes by stable identity (UID-churn-safe), validate before adoption, run `dag.ComputeWaves` cycle-detection before any `client.Create`, never import Wave CRs, and gate import behind operator + PVC-origin verification.
- **Operator tooling + E2E (Phase 29):** `tide export-envelopes` / `import-envelopes` (+ `--dry-run`) with a zip-slip-safe bundle format; two-tier kind E2E proving zero-cost resumption against the real `salvage-20260618` fixture (0 planner Jobs at adopted levels, `CostSpentCents==0`).
- **Partial-tree resume (Phase 30, the v1.0.5 patch):** fixes the dogfood-run-#2 defect where incomplete-envelope nodes materialized as `Running`-with-no-envelope zombies — shared `IsEnvelopeComplete` at export time drives adopt-complete + re-plan-incomplete; Tier-c kind E2E drives a mixed partial import all the way to `Project=Complete`.

**Milestone audit (Phases 27–30):** `tech_debt` — 16/16 requirements satisfied, 0 blockers, Nyquist-compliant; non-blocking debt = integration finding F1 (latent legacy-bundle completeness-basis inconsistency) + Phase-27 IN-01/03/04 robustness follow-ups. See `milestones/v1.0.3-MILESTONE-AUDIT.md`.

**Released artifacts (v1.0.5):** 8 component images + 2 OCI charts + 5 binaries @ 1.0.5, GitHub Release v1.0.5, verified anonymously on ghcr.

---

## v1.0.1 — Orchestrator Trustworthiness + Telemetry Completion ✅ SHIPPED 2026-06-13

**One-liner:** Turn the self-hosting MVP into an orchestrator trustworthy
enough to gate a second dogfood run on — every dogfood run-1 finding fixed
with a regression test that reproduces its symptom, the telemetry foundation
completed end-to-end, and the milestone-audit tech-debt subset closed.

**Stats:** 6 phases (12–17) · 38 plans · 46 tasks · 330 commits ·
+43.7k/−0.9k LOC across 274 files · 2026-06-11 → 2026-06-13 (~2 days).
28/28 requirements satisfied (milestone audit: passed, zero blockers).

**Key accomplishments:**

1. **Gate semantics run-killer fixed (Phase 12):** approve sits at descent
   (review the authored artifact before children spend), approval never jumps
   a level past incomplete children, dispatch holds while a parent is parked,
   reject parks instead of fail-marking, and `tide resume --retry-failed` is
   the one sanctioned recovery verb. (GATE-01..04, RESUME-01)

2. **Image dispatch chain + provider halt (Phase 13):** `resolveImage`
   precedence (`Levels.<level>.Image` → `Spec.Subagent.Image` → helm default)
   wired at all six dispatch sites — closing the v1.0 stub-image bug — and a
   provider billing-400 raises a project-wide `BillingHalt` instead of burning
   sessions one at a time. (DISPATCH-01/02, HALT-01)

3. **Budget enforcement made visible (Phase 14):** current model IDs resolve
   in the pricing table (no `unknown model` fallback), a `BudgetBlocked`
   condition surfaces on the Project CR and dashboard, in-flight overshoot is
   bounded via a reserve/settle ReservationStore, and pricing-drift is
   automated. (BUDGET-01/02/03)

4. **Seven run-1 paper cuts closed (Phase 15):** reporter CR project labels,
   clean-tree push no-op, phase status-flapping convergence, a real
   `artifact-get` inspector Pod, the dashboard Complete chip, a cross-plan
   running-waves view (label-selector–derived), and strict-mode file-touch
   overlap rejection at admission. (CUTS-01..07)

5. **Telemetry foundation completed (Phase 16):** `PROM_ENDPOINT` drives the
   PromQL proxy, TelemetryView mounts as a tab, the six locked metrics emit
   with `{project, phase, wave}` labels, panel queries use the real metric
   names, the `hack/helm` gate is wired into the Makefile, and the proxy
   client is bounded. (TELEM-01..06)

6. **Audit tech-debt subset closed (Phase 17):** PlanReconciler self-heals the
   `tideproject.k8s/project` label (with the Project→Milestone reporter-edge
   create-site stamp), reject short-circuits ahead of reporter spawn without
   deleting in-flight Jobs, the approve guard is narrowed to the approval
   target, and a transient Plan envelope-read error is non-fatal. (DEBT-01..04)

**Known deferred items at close:** 15 v1.0.0-era quick-task records
acknowledged as administrative (work landed; artifact status fields never
flipped) — see STATE.md Deferred Items. Remaining audit robustness/UX notes
(WR-01 + Phase 13/15/16 misc) formally accepted into the docs/audit hardening
backlog, all adjudicated non-blocking.

**Archives:** [v1.0.1-ROADMAP.md](milestones/v1.0.1-ROADMAP.md) ·
[v1.0.1-REQUIREMENTS.md](milestones/v1.0.1-REQUIREMENTS.md) ·
[v1.0.1-MILESTONE-AUDIT.md](milestones/v1.0.1-MILESTONE-AUDIT.md)

---

## v1.0.0 — Self-Hosting MVP ✅ SHIPPED 2026-06-11

**One-liner:** The five-level paradigm (Milestone → Phase → Plan → Task → Wave)
runs as a real Kubernetes operator — installed via Helm into any cluster,
driving LLM subagent Jobs across derived waves, with gates, budget caps,
observability, a dashboard, and a CLI.

**Stats:** 14 phase directories (11 planned + 02.1/02.2/04.1/10/11 inserted) ·
137 plans · 965 commits · ~66k LOC Go · 2026-05-12 → 2026-06-11 (30 days).

**Key accomplishments:**

1. Six `tideproject.k8s/v1alpha1` CRDs with CEL validation + cycle-rejecting
   admission webhook; waves derived (never declared) via pure-Go layered Kahn.

2. Provider-firewalled subagent dispatch: `Subagent` interface → PodJob
   backend, signed-token credproxy (raw API key never enters the agent
   process), wall-clock/iteration/token caps, secret redaction, output-path
   validation.

3. Up-stack planner cascade with envelopes-as-artifacts: per-namespace PVC
   workspaces, in-namespace reporter Job materializing child CRs, ChildCount-
   gated succession, per-level boundary pushes to per-run git branches with
   gitleaks scanning and force-with-lease.

4. Gates (auto/approve/pause per level + between-wave slack tide), Prometheus
   + OTel/OpenInference observability, read-only SSE dashboard (two-DAG React
   Flow), `tide` CLI (9 verbs, kubectl-plugin compatible).

5. Distribution: Apache-2.0, helmify-generated chart pair on OCI, multi-arch
   images ×7, goreleaser binaries, rc-gated release pipeline whose dry-run
   simulates an external operator in Docker-in-Docker at $0 LLM cost.

6. Proof: live medium DoD on minikube — Project=Complete with real
   Claude-authored commits pushed to a run branch; $0 stub flow Complete in
   ~100s on a fresh kind cluster.

**Known deferred items at close:** 11 quick-task records and 1 UAT counting
artifact acknowledged as administrative (work landed; artifact status fields
never flipped) — see STATE.md Deferred Items. 4 of 137 plans from the final
ship sprint lack SUMMARY.md files.

**Archives:** [v1.0.0-ROADMAP.md](milestones/v1.0.0-ROADMAP.md) ·
[v1.0.0-REQUIREMENTS.md](milestones/v1.0.0-REQUIREMENTS.md)
