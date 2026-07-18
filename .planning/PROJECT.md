# TIDE — Topologically-Indexed Dependency Execution

## What This Is

A Kubernetes-native orchestrator that runs hierarchical agentic coding work as a topologically-sorted DAG of subagent dispatches. A human applies a `Project` CRD (outcome prompt + target repo + creds); TIDE authors `MILESTONE.md`, phase briefs, `PLAN.md` files, and task diffs by dispatching specialist subagents at each level, parallelizing across waves derived from the declared task DAG. Built to be open-sourced and portable across clusters from day one.

## Core Value

**The five-level paradigm (Milestone → Phase → Plan → Task → Wave) runs as a real K8s orchestrator that can drive its own next milestone end-to-end.** If everything else fails, TIDE-on-TIDE must work — that's what proves the paradigm and the implementation simultaneously, and it's the bar for "v1 ships."

## Last Released Milestone: v1.0.8 — Phoenix Rising: OpenInference Trace Emission + Self-Hosted Phoenix (SHIPPED 2026-07-17, tag `v1.0.8`)

**Goal:** TIDE runs are observable in a self-hosted Arize Phoenix — the Milestone→Phase→Plan→Task dispatch chain emits real OpenTelemetry spans with OpenInference attributes (including full LLM input/output message arrays), and a documented Phoenix self-host recipe wires the chart's existing OTLP endpoint to consume them natively.

**Outcome (complete 2026-07-17):** Delivered across 6 phases / 32 plans (42–47); all 19 requirements met. The five-level dispatch chain emits attribute-complete OpenInference AGENT spans with a deterministic TraceID + W3C `traceparent` propagation; the reporter's trace-only mode emits redacted, size-bounded LLM message-array spans; a runtime-neutral adapter seam is in place for the LangGraph beachhead; sampler/`session.id`/metadata enrichment + a dashboard→Phoenix deep link ship; and a documented self-hosted Phoenix recipe was proven by a live $0.88 run producing a queryable **392-span five-level trace tree** (PROOF-01, human-signed-off 2026-07-17). **Released 2026-07-17** (tag `v1.0.8` at `6e5b8f8`, rc-gated via `v1.0.8-rc.3`): goreleaser 5 binaries + 8 images + 2 Helm OCI charts published to GHCR, all verified anon-pullable.

**Why now:** The OpenInference-on-OTel choice was made at project start precisely so traces land in standard AI observability tools, but emission was never wired — `pkg/otelai/` helpers have zero production call sites, no `tracer.Start` exists in reconcilers or the subagent path, and the env-driven TracerProvider defaults to no-op. A self-hosted Phoenix today would render empty. With v1.0.7's run-integrity + review surfaces shipped, trace-level observability is the next operator-facing gap — and it must land BEFORE the LangGraph specialist beachhead (vNext) so the trace contract is settled when a second runtime arrives.

**Target features:**
- **Dispatch-chain span emission (manager):** parent spans for the five-level hierarchy at dispatch/rollup sites — model, token counts, cost, duration, ArtifactPath refs from envelope data the manager already holds.
- **LLM message-array spans (in-namespace emitter):** `LLMInputMessages`/`LLMOutputMessages` populated from the per-Task `events.jsonl` capture; the reporter Job is the natural emitter (the manager cannot mount project PVCs); requires an explicit D-O5 payload-boundary decision (message arrays vs artifact payloads).
- **Self-hosted Phoenix surface (documented-install posture):** INSTALL.md/observability.md recipe using Phoenix's official chart/manifests + `otel.exporter.endpoint` wiring + NOTES.txt nudge — the TELEM-01 pattern; no subchart dependency, no version coupling to Arize's chart.
- **End-to-end proof:** a live run's trace tree visible and queryable in Phoenix at milestone close.

**Runtime-neutrality constraints (2026-07-15, LangGraph forward-compatibility):** A future LangGraph subagent self-instruments natively (`openinference-instrumentation-langchain` emits OpenInference spans in-process to OTLP), so: (1) the trace-context contract is the durable seam — manager creates the dispatch span and injects W3C `traceparent` into the Job env/envelope, runtime-neutral, so synthesized and native spans parent identically; (2) the `events.jsonl` parser is a runtime ADAPTER behind the Subagent seam with a self-instrumenting capability flag (reporter skips synthesis for runtimes that emit natively — no double spans; the adapter stays load-bearing through the planner-first/executor-last migration ladder's mixed fleets); (3) attribute/span-kind conventions follow OpenInference semconv exactly as the LangChain instrumentation emits them, so Phoenix queries survive the runtime migration.

## Predecessor Milestone: v1.0.7 — First-Run Paper Cuts: Run Integrity & Operator Ergonomics (SHIPPED 2026-07-15, tag `v1.0.7`)

**Outcome (shipped 2026-07-15):** Everything the first external-repo run (2026-07-03) surfaced short of new subagent stages is closed across Phases 34–41 (8 phases, 51 plans): the silent wave-parallel integration miss (run branch now provably contains every Succeeded task's work — merges serialized, boundary push gated on `git merge-base --is-ancestor`, `lastPushedSHA` force-with-lease fence), the 2.8× Claude-5 budget overcount (exact-ID pricing + empirically-probed 125/100 cache-write multiplier + observable fallback), git ergonomics (`spec.git.baseRef`, uniform agent identity at all 3 commit sites, `tide apply --prompt-file`), the dashboard as a sufficient approve-gate review surface (git-as-artifact-store staged envelopes + gitfetch, project settings view, honest log-drawer states — live-UAT'd 8/8), telemetry setup guidance at 3 surfaces, plus two appended structural phases: the Phase 40 API version-lifecycle crank (v1alpha3 sole served+storage version, `subagent.levels` semantic rename, v1alpha1+v1alpha2 deleted, CI-gated) and the Phase 41 12-item non-breaking refactoring review. Audit `tech_debt` (44/44 reqs, 0 blockers); full `make test-int` green at close (envtest 56/56, kind 26/26). Deferred by choice: GPG signing (SIGN-02/03/04), verify-tier LLM subagents (seed planted), CACHE-F1 direct-SDK backend, dogfood run #2 retarget.

**Predecessor — v1.0.6 Adoption-Path Correctness & Dispatch Safety (SHIPPED 2026-06-29, tag `v1.0.6`):** all four adoption-path defects D1–D4 closed (cost rollup + lifecycle advance under adoption, dispatch concurrency caps, planner failure semantics); audit tech_debt 13/13. Its detail lives in [milestones/v1.0.6-ROADMAP.md](milestones/v1.0.6-ROADMAP.md).

## Current State (latest released: v1.0.8 — 2026-07-17, tag `v1.0.8`)

Nine milestones released (v1.0.0, v1.0.1, v1.0.3 [incl. Spring Tide], v1.0.4, v1.0.5, v1.0.6, v1.0.7, v1.0.8; v1.0.2 Ebb superseded); **v1.0.8 Phoenix Rising shipped 2026-07-17 (tag `v1.0.8`; 8 images + 2 OCI charts + 5 binaries, anon-public).** The two earliest are detailed below for reference; v1.0.2–v1.0.7 are archived under `.planning/milestones/`. The historical detail below is preserved as-is.

Two earliest milestones shipped:

- **v1.0.0 — Self-Hosting MVP** (2026-06-11) — published: goreleaser binaries (5 platforms), 7 component images and both Helm charts on GHCR (`oci://ghcr.io/jsquirrelz/tide-charts`), rc-gated release pipeline with a $0 Docker-in-Docker external-operator dry-run. Live medium DoD proven on minikube (Project=Complete, real authored commits pushed to a per-run branch). All 82 v1 requirements delivered — [milestones/v1.0.0-REQUIREMENTS.md](milestones/v1.0.0-REQUIREMENTS.md).
- **v1.0.1 — Orchestrator Trustworthiness + Telemetry Completion** (2026-06-13) — every dogfood run-1 finding fixed with a symptom-reproducing regression test: gate-semantics run-killer (approve-at-descent), reject/resume recovery, the image-resolution chain (closing the v1.0 stub-image bug), provider billing-400 project-wide halt, budget visibility with bounded overshoot, seven paper cuts, the telemetry foundation end-to-end, and the audit tech-debt subset. 28/28 requirements satisfied; milestone audit passed with zero blockers. [milestones/v1.0.1-REQUIREMENTS.md](milestones/v1.0.1-REQUIREMENTS.md).

**Current focus:** **v1.0.8 Phoenix Rising RELEASED 2026-07-17 (tag `v1.0.8`).** All 6 phases (42–47) shipped + verified; PROOF-01 human-signed-off on a live 392-span five-level trace tree in a self-hosted Phoenix; published to GHCR (8 images + 2 OCI charts + 5 binaries, anon-public). Next: scope the next milestone — the LangGraph specialist-verify beachhead (vNext) and dynamic workflows at lifecycle seams are queued. The headline beyond remains full TIDE-on-TIDE.

Everything below this line reflects v1 planning state, preserved for reference.

## Requirements

### Validated

<!-- Shipped and confirmed valuable. -->

- [x] **CRD set defining Project, Milestone, Phase, Plan, Task (and any orchestrator-internal Run/Wave bookkeeping)** — *Validated in Phase 1: Foundation*. Six CRDs in `v1alpha1` under group `tideproject.k8s` with Spec/Status separation, owner-ref cascade (`BlockOwnerDeletion: true`), CEL inline validation, and validating-admission + conversion-webhook scaffolding (firing as no-ops; Phase 2 wires cycle detection).
- [x] **Layered Kahn algorithm in Go (pure-Go stdlib `pkg/dag`)** — *Validated in Phase 1: Foundation*. `ComputeWaves(nodes, edges) ([][]string, error)` + `CycleError` shape; α…θ regression fixture pins the spec's worked example; DAG-05 import firewall via `make verify-dag-imports` + `tools/analyzers/dagimports/` analyzer with 97% test coverage. Cycle detection is wired at the algorithm layer; Phase 2 wires it into the Plan admission webhook.
- [x] **CRD-`status`-only persistence (no external DB, no SQLite for v1)** — *Validated in Phase 1: Foundation*. `make verify-no-sqlite-dep` greps go.mod for SQLite/Postgres/gorm; `make verify-no-aggregates` greps `api/v1alpha1/*_types.go` for `Schedule`/`Waves[]`/`IndegreeMap`. Both CI-gated; programmatic guard tests in `api/v1alpha1/aggregates_guard_test.go`.
- [x] **Namespace-per-project tenancy with namespace-scoped RBAC** — *Validated in Phase 1: Foundation* (runtime portion). Every reconciler has `WatchNamespace string` field + `predicate.NewPredicateFuncs` + `WithEventFilter` in `SetupWithManager`; Manager binary has `--watch-namespace` flag. Per-namespace `RoleBinding` template lands in Phase 5's Helm chart per the AUTH-02 traceability split.

### Active

<!-- Current scope. Building toward these. v1 = self-hosting MVP. -->

- [ ] Go controller (controller-runtime) reconciling each CRD level, watching status transitions, dispatching subagents — *Phase 1 scaffold landed (six reconcilers at Standard depth with owner refs + finalizers + status conditions; Dispatcher field nil-guarded); Phase 2 wires dispatch*
- [ ] Pluggable Subagent interface — `Pod-per-task Job + typed result envelope on artifact PVC + exit code` is the contract; at least one concrete implementation (Claude Code or direct Anthropic SDK in-container)
- [ ] Artifact persistence: shared PVC during a run; orchestrator pushes to the target repo's git remote at each level boundary
- [ ] Credentials via K8s `Secret` references on the `Project` CRD (LLM API keys + git push creds)
- [ ] Strict-by-default failure profile at wave boundaries (failed task → dependents never dispatch; non-dependents continue)
- [ ] Thin `tide` CLI wrapping the common ops (apply a project, tail a run, inspect a wave, resume)
- [ ] Read-only web dashboard rendering the live Planning + Execution DAGs, wave progress, per-task status, click-through to artifacts; streams `kubectl logs` from running task pods for live feedback
- [ ] Observability: structured JSON logs from orchestrator + subagent pods, Prometheus metrics on the orchestrator (waves dispatched, tasks completed, dispatch latency, failure rate), OpenTelemetry tracing with OpenInference conventions for the Milestone→Phase→Plan→Task subagent chain
- [ ] End-to-end self-hosting demo: TIDE in a cluster (kind for dev) drives its own next milestone on this repo, producing artifacts and a merged set of commits
- [ ] Helm chart / installable bundle (CRDs + controller + RBAC) that another cluster can deploy unmodified
- [ ] Apache 2.0 LICENSE, README/docs sufficient for an external operator to install + run a project end-to-end

### Out of Scope (but not set-in-stone if there is a good reason to investigate)

<!-- Explicit boundaries. Includes reasoning to prevent re-adding. -->

- **Multi-tenant cluster posture** — Defer to post-v1. Namespace-per-project covers the OSS user; full tenant isolation (per-tenant quotas, cross-tenant RBAC) is real work that doesn't move the paradigm.
- **gRPC streaming subagent protocol** — Pod-per-task Job + result envelope is enough for v1. A streaming sidecar can be added later behind the same Subagent interface without redesign.
- **External database (Postgres) / per-run SQLite** — CRD-status-only is technically sufficient at the scale of one human watching one run. Re-evaluate only if dashboard query shapes outgrow label-selector queries.
- **Dashboard mutation actions (retry wave, edit plan, pause/resume)** — v1 dashboard is read-only. Mutations route through `tide` CLI / `kubectl` so there's a single auth surface.
- **Vendored GSD workflow Markdown** — TIDE reads `get-shit-done` as design reference but the planner/executor logic and prompts are reimplemented in Go. Markdown workflows would lock TIDE to one bootstrap host.
- **Critical-path / HEFT / heterogeneous-resource schedulers** — The spec explicitly rejects these at the paradigm layer. Wave-internal sub-scheduling stays a v2+ extension behind layered Kahn.
- **Wave or cycle "recovery" features** — Cycles are bugs detected at plan-validation time. Refuse and surface, don't recover.
- **Non-Kubernetes runtime** — Docker Compose / bare metal / Nomad are deliberately not v1. The K8s pun is load-bearing; pod isolation, RBAC, watches, and Jobs are what make the dispatch model tractable.
- **Vendor lock-in to one LLM provider** — The Subagent interface is provider-agnostic by construction. Anthropic-first concrete impl, but no provider-specific code in the orchestrator.
- **External Secrets Operator first-class integration** — Plain K8s Secrets only for v1. ESO docs/examples can land later without changing the CRD contract.

## Context

- **The spec is load-bearing.** `README.md` (270 lines) is the design doc and the public-facing README. Schemas, APIs, controller logic, and persistence trace back to it. When implementation pressure pushes back on the hierarchy or the two-DAG split, the spec updates first — then the code.
- **Bootstrap path.** TIDE itself is built using `get-shit-done` (the local Markdown-based workflow system at `~/.claude/get-shit-done/`) on the developer's host. Once TIDE runs a Milestone end-to-end against this repo (the self-hosting MVP), it earns the right to drive its own next milestone. GSD's *paradigm* informs TIDE's design heavily; GSD's *code/Markdown* is not vendored into TIDE — the planner, executor, and dispatch logic are reimplemented in Go so TIDE is portable.
- **Two parallelism budgets.** Planner pool and executor pool are separately sized — the spec argues planning fans out wide (most phases plan in parallel from one architecture spec) and execution fans out narrow (real file-level deps serialize work). The orchestrator config exposes both budgets independently.
- **Artifacts as source of truth.** Every level boundary produces a reviewable file (`MILESTONE.md`, phase brief, `PLAN.md`, diff). Resumption reads from artifacts; the CRD `.status` index is a cache, rederivable from artifacts + completed-task set.
- **Vocabulary discipline.** The water/tide metaphor is intentional and consistent — Rising tide (planning wave), Slack tide (review checkpoint), Tidal lock (deps resolved phase), Tidepool (parallel sub-DAG), TIDE pod (deployment unit). Used in code names, CRD names, log lines, docs. Extend the metaphor over coining unrelated terms; fall back to plain prose if a name doesn't fit.
- **Human gates per level.** The spec requires gate policy be configurable per level (approve-every-milestone-but-auto-pass-plans should be as easy as fully-autonomous or fully-supervised). Gates are configured per `Project`, not baked into the controller.

## Constraints

- **Tech stack**: Go + sigs.k8s.io/controller-runtime + kubebuilder — K8s ecosystem default, idiomatic for CRDs/controllers, best contributor pool.
- **Tech stack**: Pluggable subagent runtime via a documented container image contract — never hard-coded to Anthropic SDK; v1 ships with a Claude-backed concrete impl behind the interface.
- **Distribution**: Apache 2.0, Helm chart from v1, designed for installation in arbitrary clusters with no hidden host dependencies.
- **Portability**: No hard-coded git host (GitHub, GitLab, Gitea must all work behind a generic git remote), no hard-coded LLM provider, no hard-coded auth model — abstract behind interfaces.
- **Persistence**: CRD `.status` only for v1 — no external DB, no SQLite. Per-object size stays well under etcd's 1.5 MiB hard limit by keeping per-Task CRDs small and label-indexed.
- **Failure semantics**: Wave boundary contract from spec §"Failure handling at wave boundaries" must be preserved exactly — failed task → siblings continue, dependents in later waves never dispatch, non-dependents dispatch in strict profile. Resumption state = indegree map + completed-task set, nothing more.
- **Resumability**: Long-running agentic work outlives single context windows. Every level boundary is a saved artifact; a fresh orchestrator restart re-derives waves from the task DAG + completed-task set in O(V+E).
- **Observability**: OpenTelemetry tracing must use OpenInference conventions for LLM/agent spans so traces are queryable in standard AI observability tools (Phoenix, LangSmith, Arize) without bespoke instrumentation.

## Key Decisions

<!-- Decisions that constrain future work. Add throughout project lifecycle. -->

| Decision | Rationale | Outcome |
|----------|-----------|---------|
| Go + controller-runtime + kubebuilder | K8s ecosystem default; idiomatic CRDs/controllers; best contributor pool for an OSS K8s operator | ✓ Validated in Phase 1 — Go 1.26 + controller-runtime v0.24.1 + kubebuilder v4.14.0 pinned and shipping |
| v1 = self-hosting MVP (TIDE drives its own next milestone) | Dogfood test proves paradigm and implementation simultaneously; bounded scope that's still real work | — Pending |
| Pluggable Subagent interface from day one | Spec is provider-agnostic; OSS posture demands no vendor lock-in; defining the contract early prevents Anthropic-specific code leaking into the orchestrator | — Pending |
| Pod-per-task K8s Job + result envelope on PVC + log streaming | Simplest contract that's K8s-native and matches how Claude Code already runs; streaming gRPC sidecar deferred to v2 behind the same interface | — Pending |
| Reimplement GSD paradigm in Go (not vendor Markdown) | TIDE is the production K8s generalization of the paradigm, not a port; vendoring workflows would couple TIDE to a single bootstrap host | — Pending |
| Artifacts on PVC during run; git push at level boundaries | Lower-latency than per-artifact commits; matches reviewable level-boundary cadence; one cred surface (git remote) | — Pending |
| CRD-`status`-only persistence for v1 | Technically sufficient at v1 scale; aligns with spec's "DB is cache, not truth"; defers operational burden until proven needed | ✓ Validated in Phase 1 — `make verify-no-sqlite-dep` + `make verify-no-aggregates` CI-gated |
| Namespace-per-project tenancy | Right tradeoff for OSS posture without v1 scope creep into full multi-tenancy | ✓ Validated in Phase 1 (runtime) — runtime predicate + `--watch-namespace` flag; per-namespace RoleBinding template deferred to Phase 5 Helm chart |
| K8s Secrets referenced by Project CRD for creds | Standard K8s, no extra deps; ESO/Vault integrations land later without breaking the contract | — Pending |
| Strict-by-default failure profile | Spec default; maximizes throughput on independent task failures; conservative profile becomes a per-Project setting later if needed | — Pending |
| Read-only web dashboard for v1 | All mutations go through `kubectl` / `tide` CLI for a single auth surface; viewer-only keeps scope honest | — Pending |
| Apache 2.0 license | K8s ecosystem default; patent grant; friendliest to enterprise contributors and downstream commercial use | — Pending |
| OpenTelemetry tracing with OpenInference conventions | Standard OTel infra compat + AI-native span attributes queryable in Phoenix/LangSmith/Arize without bespoke instrumentation | — Pending |
| Approve gate sits at descent (review the authored artifact before children spend) | run-1 finding-1/7: approving advanced a Milestone past 5 running Phase children → premature Project=Complete; gate-at-descent holds child dispatch until approval | ✓ Validated v1.0.1 Phase 12 |
| Provider billing-400 halts the entire project | run-1 burned ~$80 across dying sessions during two credit dry-outs; a project-wide `BillingHalt` condition stops the fan-out instead of failing one session at a time | ✓ Validated v1.0.1 Phase 13 |
| Image resolves via `Levels.<level>.Image` → `Spec.Subagent.Image` → helm default at every dispatch site | v1.0 hard-coded the stub image; the documented chain was only honored for Model, leaving `spec.subagent.image` dead config | ✓ Validated v1.0.1 Phase 13 |
| Reserve/settle budget accounting bounds in-flight overshoot | run-1 overshot ~$40 past the $100 cap from already-dispatched sessions; a ReservationStore (rederivable on restart) bounds overshoot to one wave | ✓ Validated v1.0.1 Phase 14 |
| `tide resume --retry-failed` is the one sanctioned recovery verb; approve never doubles as a spend-retry | run-1 needed a kubectl status-reset recipe to recover Failed levels; codifying it as a CLI verb keeps approval semantics clean (D-07) | ✓ Validated v1.0.1 Phase 12/17 |
| **Cross-pod prompt caching does NOT realize on caller content via `claude -p --bare`; SharedContext ships as token-minimization, cache benefit deferred to a direct-SDK backend (CACHE-F1)** | Phase 20 CACHE-01 spike: cross-pod caching *fires* but only for the CLI's ~1.1–1.3k-token tool/system scaffold; the CLI front-loads a per-request `cch` billing nonce ahead of caller content and exposes no suppression lever — the documented `--exclude-dynamic-system-prompt-sections` flag does not recover it | ✓ Validated v1.0.2 Phase 20 — 3 live runs on kind-tide-dogfood + official Claude Code caching docs |
| Adoption lifecycle advance + budget rollup share one seam; project-planner suppression is a durable CRD condition | run-2b D1+D2: cost meter never wired under import-adoption (spent blind) because the Project stalled at `Initialized`; advancing to `Running` on `ImportComplete` (without dispatching the project-planner) is what makes rollup + cap enforcement fire | ✓ Validated v1.0.6 Phase 31 |
| Planner concurrency capped by a live in-flight `client.List` count before pool acquire, single-node-safe default 4 | run-2b D3: the in-process semaphore caps concurrent `r.Create` calls, not in-flight running pods, so ~60 planner pods dispatched and OOM'd the single node; an explicit count-gate parks excess dispatches | ✓ Validated v1.0.6 Phase 32 |
| A planner that exits nonzero with zero children is marked `Failed` BEFORE the gate-policy hook | run-2b D4 + CR-01: phase/milestone took a direct `expected==0 → Succeeded` shortcut ignoring exitCode; and because the milestone default gate is `approve`, a guard placed after the gate hook would park the failure at `AwaitingApproval` instead of `Failed`. Plan/project are not exposed (they succeed only via `BoundaryDetected`, false on zero children) | ✓ Validated v1.0.6 Phase 33 |
| Boundary push gates on integration completeness recomputed from git (`merge-base --is-ancestor`), never cached in `.status`; wave-parallel merges serialize behind kernel flock | First external run shipped `Complete` with a deliverable silently dropped by a racing final-wave integration; a cached verdict could go stale, git is the truth | ✓ Validated v1.0.7 Phase 34 |
| Git is the planning-artifact store (staged envelopes on the run branch under `.tide/planning/`, read via gitfetch) — NOT ConfigMaps; full artifact visibility, no truncation anywhere | User rejected truncated artifact display; Argo-convention prior art (artifact store + server streams to UI, never etcd) using TIDE's always-present dependency (the git remote); supersedes the size-capped ConfigMap display-cache constraint | ✓ Validated v1.0.7 Phase 37 (live UAT 8/8) |
| API version lifecycle = full crank with reinstall-only migration: new version in, ALL old versions out, generalized two-constant SchemaRevision guard, CI-gated zero legacy refs | Serving stale versions indefinitely accretes dual-accept paths and stale docs; v1alpha1 was already served:false since Phase 23 while its examples sat broken in INSTALL.md | ✓ Validated v1.0.7 Phase 40 (v1alpha3 sole version) |
| NO FLAKE TOLERANCE in the integration suite: no retries, a non-deterministic spec is a bug to root-cause (timeout, race, ordering) | `-ginkgo.flake-attempts=3` hid a real tide-push regression for days; the 2026-07-15 push-lease red was root-caused to a namespace-Terminating recreate race (fire-and-forget teardown) and fixed at the harness root, not relabeled "flake" | ✓ Validated v1.0.7 (Makefile doctrine + quick 260715-4jd) |
| Retroactive span synthesis: spans are created AND closed inside `handleJobCompletion` from `completedJob.Status.{StartTime,CompletionTime}`, never held open across a `Reconcile()` return; creation is gated off the same state-transition edges that gate Job creation | Span creation has no natural idempotency (unlike Job `Create`); holding a span open across reconciles would leak on restart, and in-memory "already did this" checks do not survive a controller bounce | ✓ Validated v1.0.8 Phases 42–43 |
| Deterministic TraceID from `Project.UID` (128-bit ↔ OTel TraceID); span IDs mint fresh at each level completion — no custom `IDGenerator` | One run must render as ONE navigable trace across reconciler restarts without persisting a schedule; deriving the root from the Project UID makes the trace re-addressable from status alone | ✓ Validated v1.0.8 Phase 43 |
| Redact-before-truncate at a single `redactTruncate` chokepoint; `events.jsonl` stays raw/unredacted at the source | The capture file is raw by design; making redaction mandatory at the sole emission chokepoint (not the source) means a secret can never reach a span even as new call sites are added | ✓ Validated v1.0.8 Phase 44 (straddling-secret test; 0-hit key search over 392 live spans) |
| D-O5 payload boundary = triple guard (32 KiB/message head+tail elision, 512 KiB joint span budget, `OTEL_BSP_MAX_EXPORT_BATCH_SIZE=6`), grounded in 58 real fixture files | Research on real captures overturned the naive per-span framing — the real OTLP 4 MB risk was the aggregate export batch, not a single oversized span | ✓ Validated v1.0.8 Phase 44 |
| Runtime-neutral trace contract: manager injects W3C `traceparent`; the events.jsonl→spans synthesizer is a per-runtime adapter behind the Subagent seam, gated by a fail-closed `SelfInstruments` capability flag carried as data | A future LangGraph runtime self-instruments natively; the seam must exist BEFORE it lands so synthesized and native spans parent identically and no double-emission path is discovered late | ✓ Validated v1.0.8 Phase 45 (stub-runtime contract test, zero duplicate spans) |
| Phoenix is a separate `helm install`, never a TIDE subchart/bundled manifest; TIDE only wires `otel.exporter.endpoint` | Documented-install posture (TELEM-01 precedent) avoids version coupling to Arize’s near-daily chart releases | ✓ Validated v1.0.8 Phase 47 (live proof on chart 10.0.1 / appVersion 18.1.0) |

### CACHE-01 decision record (Phase 20 — cross-pod cache verification spike)

**Verdict: cross-pod prefix caching fires under `claude -p --bare`, but caller-controlled content does not realize a cross-pod cache benefit on the CLI path. The original blocker (per-pod `--add-dir`/CWD path busting the prefix) is REFUTED. SharedContext ships as token-minimization; the cache payoff is deferred to a direct-SDK backend (CACHE-F1).**

**Empirical evidence (3 live runs on `kind-tide-dogfood`, real Anthropic API via credproxy, model `claude-sonnet-4-6`):**
- Two sequential `claude -p --bare` dispatches with a byte-identical ≥1,024-token prefix and *distinct* `--add-dir` paths: dispatch A `cache_creation=12296`, dispatch B `cache_read=1307` / `cache_creation=10989` (`1307 + 10989 = 12296`). Reproducible across two runs.
- The teed credproxy request bodies are **byte-identical across pods A and B** except a per-request-random `cch=<hex>` token in an `x-anthropic-billing-header` system block — the `--add-dir` path is **not present in the request body** at all. This refutes the CACHE-01 premise that the per-pod path makes each pod's prefix unique.
- The realized cross-pod read (~1,307 tokens) is the CLI's **tool/system scaffold** only; the caller's user-message content (a CLI-injected ~25.5k-char block + our probe ≈ 10,989 tokens) is **re-created every dispatch** and never cross-pod cache-reads.
- The Claude Code caching docs confirm the mechanism: the cache is "scoped to one machine and directory" — the system prompt embeds working directory, platform, shell, OS, git branch, and recent commits ahead of caller content, and a change anywhere in the prefix recomputes everything after it.
- The documented fix for fleets, `--exclude-dynamic-system-prompt-sections` (moves the dynamic CWD/git sections out of the system prompt), was tested live and **did not recover** caller-content caching: B still `read=1061` / `create=12372`. The residual cap is the per-request `cch` nonce, which has **no CLI suppression lever** (`DISABLE_PROMPT_CACHING*` only disables caching entirely).

**Floor question (CACHE-03 `make eval`):** moot for cache benefit on the CLI path given the above — even a SharedContext-grown prefix that clears the floor will not cross-pod cache-read while the `cch` nonce sits ahead of it. Wave-scoped SharedContext content (~300–700 tok per 20-CONTEXT D-04) on top of today's ~200–500-tok templates approaches but does not reliably clear 1,024 on its own; a precise per-template live measurement folds into the CACHE-F1 follow-up.

**Per-provider cache floor table (known input; D-06 — NEVER hardcode 1,024, always "the active model's documented floor"):**

| Provider | Min cacheable prefix |
|----------|----------------------|
| OpenAI | 1,024 (+128-tok increments) |
| Anthropic | 1,024 Sonnet/Opus · 4,096 Haiku |
| Google Gemini | 1,024 Flash · 2,048 Pro |
| AWS Bedrock | 1,024 or 4,096 (model-dependent) |
| DeepSeek | none (64-tok chunks) |
| Mistral | 64 |

**Provider-neutrality (CACHE-05):** the SharedContext path carries no vendor markers/branches (grep-verified clean across `envelope.go`, `childcrd.go`, `dispatch_helpers.go`, and the four planner templates). OpenAI/Codex live parity is **deferred to the run-#2 milestone** — SharedContext is a plain stable-prefix string on the provider-agnostic `EnvelopeIn`.

**D-08 contingency resolution:** **scoped follow-up — no contained in-phase fix exists.** The plan's `normalize-in-phase` candidate (alias the `--add-dir` path) is refuted as the divergence; the `--exclude-dynamic-system-prompt-sections` flag does not recover caching; the `cch` nonce is CLI-internal with no suppression knob. No `subagent.go` change, no chart change. **Follow-up:** realize cross-pod cache benefit via a direct-SDK subagent backend (CACHE-F1) that sets the system prompt explicitly (no per-request nonce, no dynamic sections) and places `cache_control` breakpoints on the shared prefix.

## Evolution

This document evolves at phase transitions and milestone boundaries.

**After each phase transition** (via `/gsd:transition`):
1. Requirements invalidated? → Move to Out of Scope with reason
2. Requirements validated? → Move to Validated with phase reference
3. New requirements emerged? → Add to Active
4. Decisions to log? → Add to Key Decisions
5. "What This Is" still accurate? → Update if drifted

**After each milestone** (via `/gsd:complete-milestone`):
1. Full review of all sections
2. Core Value check — still the right priority?
3. Audit Out of Scope — reasons still valid?
4. Update Context with current state

---
*Last updated: 2026-07-17 after the v1.0.8 Phoenix Rising RELEASE (tag `v1.0.8`; 6 phases / 32 plans; PROOF-01 human-signed-off; published to GHCR — 8 images + 2 OCI charts + 5 binaries). Next: scope the next milestone.*
