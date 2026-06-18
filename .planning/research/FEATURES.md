# Feature Research — TIDE v1.0.3 Planning Resumption & Cost Resilience

**Domain:** Resumption and idempotent re-entry for an expensive multi-stage agentic orchestrator
**Researched:** 2026-06-18
**Confidence:** HIGH for Argo/Temporal/Prefect/Dagster patterns (official docs + verified secondary sources);
MEDIUM for LLM-agent-specific budget-halt patterns (emerging space, multiple sources agree directionally);
HIGH for TIDE invariant analysis (direct spec + codebase inspection).

---

## Research Context

TIDE v1.0.3 addresses a concrete failure mode: dogfood run #2 spent ~$90 authoring 3 milestones /
15 phases / 42 plans in the Planning DAG, then budget-halted with zero execution. All planning artifacts
survived on the PVC. Re-running the Project from scratch re-pays the full planning cost — there is no
resume mechanism. This research surveys how mature workflow orchestrators handle exactly this problem,
then maps each pattern to TIDE's invariants.

**TIDE invariants that constrain every feature in this document:**

1. Waves are derived, never declared — the schedule is the output of layered Kahn on the task DAG,
   rederived from the completed-task set. Caching the schedule itself violates this invariant.
2. Persistence is CRD-`.status` only — no external DB, no SQLite. Resumption state =
   indegree map + completed-task set, rederivable in O(V+E).
3. Cycles are bugs, not runtime conditions — cycle detection happens at plan-validation time;
   the planner-skip path must not allow a stale or structurally-invalid envelope to bypass
   cycle detection.
4. UID churn on fresh runs — K8s assigns new UIDs to each CRD object on every fresh apply.
   Envelopes are currently keyed by UID (`envelopes/<objUID>/out.json`). Plan-import must not
   silently adopt a stale envelope via a UID collision or skip validation.
5. Budget bypass must resume at `Running`, not `Pending` — a cleared halt that re-enters
   `Pending` resets the initialized state and re-fires the planner.

---

## Prior Art Survey

### Argo Workflows — Resubmit + Memoization

**Mechanism:** Argo distinguishes two resume verbs: `retry` (re-runs only failed/errored nodes in
place, same workflow object) and `resubmit --memoized` (creates a new workflow object but skips
nodes whose cache key matches a ConfigMap entry from the previous run).

**Cache key:** Template-level, declared in the YAML spec. Can be static strings or
`{{inputs.parameters.x}}`. Cached outputs stored in K8s ConfigMaps (1 MB limit per ConfigMap;
maxAge configurable in seconds or hours). ConfigMaps are directly inspectable via kubectl.

**Skip behavior:** When the cache key matches and the entry is not expired, Argo marks the node
`Succeeded` immediately without dispatching the Pod. Downstream DAG edges are honored as if the
node ran — the result is forwarded through the DAG without re-execution.

**Known limitations (HIGH confidence — GitHub issues #12936, #10906):**
- `resubmit --memoized` on a workflow with failed nodes has a documented bug (issue #12936): DAG
  dependency ordering is not always respected after skipping memoized nodes — all unblocked
  successors can dispatch simultaneously instead of in wave order. This is a real correctness
  hazard that TIDE must avoid.
- The difference between `retry` and `resubmit` was not clearly documented until recently
  (issue #10906); prior to v3.5, only nodes with outputs could be memoized.

**TIDE mapping:** Argo's "resubmit with memoization" is the closest prior-art analog to TIDE's
plan-import requirement. The lesson: key on stable, input-derived identifiers (not UIDs); validate
before skipping; maintain DAG ordering through the skip operation.

**Confidence:** MEDIUM-HIGH (official docs + GitHub issues cross-verified)

---

### Temporal — Event History Replay + Determinism Requirement

**Mechanism:** Temporal persists every state transition as an immutable Event in a workflow's Event
History. On resume (worker restart, crash, new deployment), the worker *replays* the workflow
function from the beginning of the event history. Completed Activity calls are *not* re-executed —
the worker reads the recorded result from history and returns it immediately. From the caller's
perspective, resumption is transparent.

**Determinism requirement:** Workflow code must be deterministic — given the same event history, it
must emit the same commands. Non-deterministic code paths produce a non-determinism error on replay.
This is the mechanism that makes replay safe: the same function path + the same recorded results =
the same output without any work being repeated.

**Continue-as-New:** For workflows that would accumulate unbounded event history (e.g., a workflow
running thousands of steps over days), Temporal provides `ContinueAsNew`, which atomically completes
the current execution and starts a new one with the same workflow ID but a fresh history, passing
current state as input. This prevents event history from growing beyond the size limit.

**Budget-halt pattern:** Temporal has no native budget-halt concept, but the durable execution model
means any externally-injected halt (via Signal) is recorded as an event. Resume after clearing the
halt replays up to the Signal event and then continues forward — no work is repeated.

**TIDE mapping:** TIDE cannot adopt Temporal's replay model directly (it uses K8s CRD `.status` not
an event log), but the principle is clear: completed planner calls should have their results recorded
as CRD status, so a resumed run reads the recorded result instead of re-dispatching the planner.
This is exactly the plan-import/envelope-resumption requirement.

**Confidence:** HIGH (official Temporal docs + Zylos AI checkpointing research + Augment Code guide)

---

### Prefect — Task Caching with Cache Policy + Result Persistence

**Mechanism:** Prefect task caching allows a task to return a predetermined value without executing
its body. Caching is opt-in via `cache_policy` parameter on the `@task` decorator. Two primary
policies:

- `INPUTS`: cache key is the hash of the task's input arguments. Same inputs = cache hit.
- `TASK_SOURCE`: cache key includes the task's source code hash. Code change = cache miss.

Cache storage requires explicit result persistence (off by default). Results are stored in a
configured result backend (local filesystem, S3, etc.).

**Skip behavior:** On cache hit, the task enters `Completed` state immediately with the cached
value. Downstream tasks receive the cached result as if the task had run. Cache misses execute
normally and store the result.

**Key constraint:** Caching requires result persistence to be enabled explicitly. This is a
footgun — the caching *policy* can be configured but if result persistence is off, nothing is
actually stored and every run is a cache miss.

**TIDE mapping:** Prefect's "task with cache policy" is the clean abstraction. TIDE's equivalent is
a planner invocation that checks for an existing valid envelope before dispatching — cache key is
the stable identity of the artifact being planned (name + parent chain), not the K8s UID.

**Confidence:** HIGH (official Prefect docs)

---

### Dagster — Asset Versioning and Staleness Model

**Mechanism:** Dagster tracks two versioning dimensions per asset:

- `code_version`: a string the developer declares on the `@asset` decorator, representing the
  computational logic version.
- `data_version`: a hash of the asset's output value, computed automatically or overridable.

An asset is "unsynced" (stale) when its code version or input data has changed since last
materialization. Dagster can skip materialization of an up-to-date asset and return the cached value.

**Staleness is not transitive by default:** downstream assets only become stale if their last
materialization used an outdated upstream version — not merely because an upstream ran again.

**TIDE mapping:** Dagster's staleness model maps to TIDE's envelope validation. An envelope is
"valid" (skip the planner) when: (a) the envelope exists, (b) the envelope's content hash matches
the expected input fingerprint (parent artifact chain + project outcome). An envelope is "stale"
when the input has changed — requiring a re-plan. The staleness check is the validation gate that
makes plan-import safe.

**Confidence:** HIGH (official Dagster docs)

---

### Apache Airflow — Backfill Reprocessing Modes

**Mechanism:** Airflow 3 backfills support three reprocessing modes:

- `Missing Runs`: creates and runs only DAG runs that do not already exist (idempotent forward fill).
- `Missing and Errored Runs`: re-runs failed DAG runs; skips succeeded ones.
- `All Runs`: clears and re-runs everything.

The `--rerun-failed-tasks` flag selectively re-runs only failed task instances within a backfill
date range.

**Idempotency requirement:** Airflow tasks are expected to be idempotent — running the same task
twice with the same inputs should produce the same result. This is a design requirement on the task
author, not enforced by the framework.

**Known issue:** If a task was previously marked `skipped`, backfill can hang indefinitely (issue
#570). This documents the classic pitfall of confusing "skipped" (not-executed by branching) with
"succeeded" — TIDE must distinguish these states clearly.

**TIDE mapping:** Airflow's "skip succeeded, re-run failed" mode is exactly the planning resumption
contract. A succeeded planner envelope → skip. A failed or missing envelope → re-plan.

**Confidence:** MEDIUM (official docs + Astronomer docs + GitHub issues)

---

### Bazel / Nix — Content-Addressed Action Cache

**Mechanism:** Bazel breaks a build into discrete *actions*. Each action has:

- An *action key* (aka action digest): a deterministic hash of the command, arguments,
  environment variables, and a Merkle tree digest of all input files.
- A *content-addressable store (CAS)*: where output artifacts are stored by their content hash.
- An *action cache (AC)*: maps action key → output artifact hashes. If the action key hits the
  AC, the output CAS entries are fetched without executing the action.

This is input-addressed memoization: any change to any input changes the action key and busts
the cache. The key is stable as long as inputs are identical.

**TIDE mapping:** The Bazel action cache is the purest prior-art model for TIDE envelope
memoization. The envelope cache key should be a deterministic hash of:
- The stable identifier of the object being planned (name + level + parent chain)
- The project outcome prompt (or a stable hash of it)
- The TIDE planner template version

If any of these inputs change, the envelope is stale and must be re-planned. This directly
addresses the UID-churn problem: the key is content-derived, not UID-derived.

**Confidence:** HIGH (official Bazel docs + ACM Queue article)

---

### CI Systems — Skip-on-Cache-Hit

**Mechanism (GitHub Actions, CircleCI):** CI systems use explicit cache keys. On `cache-hit: true`,
subsequent steps can be gated with `if: steps.cache.outputs.cache-hit != 'true'`. This is the
step-level "skip if cached" pattern. The cache key is typically a hash of dependency manifest files
(e.g., `hashFiles('**/package-lock.json')`).

**Key insight:** CI cache keys are always content-addressed and input-derived. Stable input files
= stable cache key = cache hit = skip. The cache key is never the job's own output ID (that would
be circular); it's always derived from the inputs that determine whether the output is still valid.

**TIDE mapping:** The same discipline applies. The plan-import cache key must be derived from inputs
(outcome + parent chain + planner version), not from the previously-generated output's UID.

**Confidence:** HIGH (official GitHub Actions docs)

---

### Long-Running LLM Agent Systems (Anthropic, LangGraph, Augment)

**Budget exhaustion patterns observed in the field:**

1. **Structured handoff file (Anthropic internal pattern):** For runs that exceed a single context
   window, the harness tears the session down and rebuilds it from a structured handoff file —
   equivalent to onboarding a new team member from documentation. State is external, artifacts
   survive the session boundary.

2. **Per-step snapshots (LangGraph):** Serialize complete graph state after each node execution.
   Resume from the exact failure point. Requires a persistent state backend.

3. **Budget backstop with forced generation:** When budget is exhausted, emit a forced terminal
   action ("wrap up now") rather than a hard kill. Avoids leaving partial state that is neither
   complete nor clearly failed.

4. **User expectation — "no double billing":** The implicit contract documented across multiple
   sources is that resumption from a checkpoint is *free work*. Users who resume after a budget
   halt expect to pay only for genuinely new execution from the failure point forward. Re-paying
   the planning cost on every restart is experienced as a bug, not a feature.

**Budget-bypass specific patterns:**
- Soft limits with human-in-the-loop escalation: halt is observable, user can raise cap and
  explicitly resume. This is TIDE's current model (BillingHalt condition + `tide resume`).
- Cap raise ergonomics: raising one of (absolute cap, rolling cap) should not immediately re-halt
  on the other. This is a known TIDE bug identified in PROJECT.md.

**Confidence:** MEDIUM (multiple sources agree; no single authoritative spec)

---

## Feature Landscape

### Table Stakes (Users Expect These — v1.0.3 Must Have)

Features that users of any mature workflow orchestrator take for granted. Missing these means
the budget-halt experience remains broken.

| # | Feature | Prior Art Basis | Why Expected | Complexity | TIDE Invariant Impact |
|---|---------|----------------|--------------|------------|----------------------|
| TS-R1 | **Envelope-based planner skip** — before dispatching a planner Job, check whether a valid envelope already exists for this object's stable identity; if yes, skip dispatch and materialize directly from the envelope | Argo memoization; Prefect task caching; Bazel action cache | Any workflow system that re-runs expensive computations unconditionally is broken by definition. Users expect "already done" to mean "don't do again." | MEDIUM — requires name-based / content-keyed envelope lookup instead of UID-keyed lookup; touches all planner dispatch sites in `project_controller.go` | Does NOT cache the wave schedule — envelopes are inputs to Kahn, not outputs; invariant preserved |
| TS-R2 | **Stable envelope cache key** — the lookup key is a deterministic hash of (stable object name + parent chain + project outcome hash + planner template version), NOT the K8s UID | Bazel action key; GitHub Actions `hashFiles`; CI cache key discipline | UID churn is guaranteed on every fresh Project apply. Any UID-keyed scheme is a no-op on resume. Content-addressed keys are the only correct design. | MEDIUM — requires a keying function in the dispatcher; PVC path changes from `envelopes/<UID>/` to `envelopes/<stableKey>/` | No invariant conflict — keys are content-derived, not schedule-derived |
| TS-R3 | **Envelope staleness validation before skip** — before skipping a planner, verify the stored envelope: (a) parses as valid JSON, (b) passes the same cycle-detection and schema validation that a freshly-authored envelope would, (c) fingerprint matches the stable key | Dagster staleness check; Temporal determinism requirement | Silently adopting a corrupt or stale envelope violates the spec's cycle-detection contract. Argo's memoization bug (#12936) was a DAG-ordering failure caused by skipping without maintaining dependency semantics. | MEDIUM — reuse the existing envelope validation path; add fingerprint comparison | CRITICAL — preserves cycle-detection invariant; prevents invalid envelopes from bypassing plan-validation |
| TS-R4 | **Budget-bypass resume at `Running`, not `Pending`** — when a BillingHalt is cleared, the project controller must resume at `Running` phase; if it re-enters `Pending`, the planner re-init fires and all pre-existing child CRDs are orphaned or duplicated | Temporal signal-and-resume; Airflow "skip succeeded" | The current code bug (PROJECT.md: `project_controller.go:1257`) means clearing a halt re-enters `Pending` and re-dispatches planners. Any resumed run re-pays planning cost. This is the most damaging immediate fix. | LOW — targeted controller fix at one branch; regression-test required | No invariant conflict — fixing a state machine bug |
| TS-R5 | **Idempotent child-CRD re-init guard** — the planner dispatch site must check whether child CRDs already exist before creating new ones; if children exist, skip planner dispatch even if the controller is in `Running` state | Prefect caching requires result persistence; Temporal skip-on-history | A resumed run that re-enters `Running` must not re-create child CRDs that already exist. Without this guard, resume creates duplicate Milestone/Phase/Plan CRDs, breaking the hierarchy. | LOW — check `.status.children` or list existing CRDs before dispatch; the idempotency check is simpler than the full envelope-skip | Complements TS-R4; both are needed to make budget-bypass safe |
| TS-R6 | **Cap-raise ergonomics: raising one cap does not immediately re-halt on the other** — when the user raises the absolute spend cap but the rolling window is also near-exhausted (or vice versa), the system should not immediately re-halt | LLM agent budget patterns; soft-limit-with-escalation | Users raising the absolute cap expect the run to continue. Being immediately re-halted by the rolling cap (or absolute cap that was just raised) is UX failure. Needs coordinated re-check before resuming dispatch. | LOW-MEDIUM — BillingHalt condition logic and resume path; test both caps simultaneously | No invariant conflict |
| TS-R7 | **Regression coverage for the project-controller ordering fix (`2a5e0dc`)** — the Running-branch terminal-completion check must precede the idempotency early-return at the project→milestone dispatch site | Standard regression testing discipline | The fix already landed but has no automated regression test. Without a test, the bug can be silently re-introduced by any future controller refactor. | LOW — envtest covering the specific ordering: dispatch runs to terminal completion before idempotency guard fires | No invariant conflict |

---

### Differentiators (Competitive Advantage — Meaningful for v1.0.3)

Features beyond table stakes that make TIDE's resumption model distinctive.

| # | Feature | Prior Art Basis | Value Proposition | Complexity | TIDE Invariant Impact |
|---|---------|----------------|-------------------|------------|----------------------|
| D-R1 | **`tide resume --from-envelope <path>` import command** — a CLI verb that reads a saved envelope tree from a local directory (e.g., `examples/projects/dogfood/salvage-20260618/`), validates each envelope's schema + cycle-detection, computes stable keys, and writes to the PVC at the expected key path; the subsequent Project apply then hits the envelope cache and skips all planning | Argo `--memoized` resubmit; Airflow backfill `Missing Runs` mode | Enables adopting the salvaged dogfood artifacts without any manual PVC manipulation. Closes the immediate TIDE-on-TIDE cost recovery gap. | HIGH — requires the stable-key computation, the import command, envelope path mapping, and the PVC write; CLI subcommand + validation + key rewrite | Reuses TS-R3 validation; preserves cycle-detection; the import writes to the expected key path so the normal planner-skip path fires naturally |
| D-R2 | **Plan-import dry-run mode** — `tide resume --from-envelope <path> --dry-run` reports which envelopes would be accepted vs rejected (with rejection reason) without writing to the PVC | Dagster staleness UI; Argo workflow dry-run | Lets operators validate an import before committing, avoiding partial-state failures where some envelopes import and others fail mid-import | MEDIUM — dry-run flag on the import command; print acceptance/rejection report | No invariant conflict |
| D-R3 | **Per-level resume status on dashboard** — show each Milestone/Phase/Plan node with a "Resumed from envelope" badge distinct from "Freshly planned"; surface which nodes were skipped vs re-planned in a given run | Temporal event history visualization; LangGraph checkpoint browser | Operators need to know which work was actually redone after a resume. Without this, there's no way to audit that the resume was correct. Closes the observability gap. | MEDIUM — new node-level status field on Milestone/Phase/Plan CRD + dashboard rendering | No invariant conflict; CRD `.status` is the correct persistence point |
| D-R4 | **Envelope export command** — `tide export-envelopes <project> <namespace> --output-dir <path>` reads all existing planner envelopes from the PVC and writes them to a local directory in the stable-key naming convention; makes the salvage workflow portable | Airflow DAG export; Bazel CAS export | Enables the dogfood salvage pattern as a first-class operation: run halts → export envelopes → fix the issue → import on fresh run. Preserves planning artifacts across cluster teardowns. | MEDIUM — reads from PVC via a Job or `kubectl cp`; writes to a deterministic directory structure | No invariant conflict |

---

### Anti-Features (Deliberately NOT in v1.0.3)

| # | Anti-Feature | Why Requested | Why Rejected / TIDE Invariant Violation | Alternative |
|---|--------------|---------------|----------------------------------------|-------------|
| AF-R1 | **Cache the wave schedule in `.status`** | "Just save the wave list so we don't re-derive it on every reconcile" | DIRECT INVARIANT VIOLATION: waves are derived, never declared (spec §"Wave computation"). The wave schedule must be re-derived from the task DAG + completed-task set on every reconcile. Caching a stale schedule can silently skip tasks whose predecessors failed. Argo's memoization bug (#12936) is the prior-art failure mode for exactly this pattern. | Re-derive waves in O(V+E) on every reconcile — it's cheap, it's correct, it's what the spec says. |
| AF-R2 | **Accept a UID-keyed envelope directly on fresh Project apply** | "The envelopes are already on the PVC from the previous run; just use them" | UID churn is guaranteed — a fresh Project apply assigns new UIDs to all child CRDs. Adopting envelopes keyed by old UIDs silently re-uses stale planning artifacts from a different object identity. This is the adoption-without-validation failure mode. | Use content-addressed stable keys (TS-R2). |
| AF-R3 | **Skip cycle detection on import to save time** | "These envelopes already passed validation when they were first authored" | DIRECT INVARIANT VIOLATION: cycle detection happens at plan-validation time and cannot be bypassed. An envelope is only safe to adopt if it currently passes validation — it may have been authored against an older schema or template version. Argo's memoization skipped DAG ordering and caused a correctness bug. | Run the same validation path on import as on fresh authoring (TS-R3). Validation is O(V+E) and fast. |
| AF-R4 | **Partial import (skip validation for "obviously good" envelopes)** | "Most envelopes are fine; only validate the top-level ones" | One invalid envelope in the middle of the tree can propagate invalid assumptions downstream. Dagster's staleness model and Bazel's action cache both validate every node. Partial validation introduces a correctness hazard that is not worth the time saved. | Validate all envelopes atomically (TS-R3). Reject the import if any envelope fails. |
| AF-R5 | **Envelope TTL / cache expiry** | "Envelopes older than N days should be considered stale and re-planned" | Time is not a valid staleness signal for planning artifacts. A 30-day-old envelope for an unchanged project outcome + template is still correct. A 5-minute-old envelope for a changed outcome is stale. The staleness signal is input-fingerprint mismatch, not age. Argo's `maxAge` for memoization is appropriate for data-processing workflows where freshness has inherent value; TIDE's planning artifacts are deterministic given stable inputs. | Use content-hash staleness (TS-R2 + TS-R3), not time-based expiry. |
| AF-R6 | **Automatic resume on any halt (including non-budget halts)** | "Just resume automatically after any recoverable error" | Temporal's pattern: the developer explicitly decides what is resumable. A budget halt is resumable after cap-raise. A cycle-detection failure is not resumable without a plan fix. An invalid envelope is not resumable without a re-plan. Auto-resume without discriminating halt type would silently re-run broken plans. | Discriminate halt types in `tide resume`: budget-halt → resume at Running; cycle-error → require fix; validation-error → require re-plan or import. |
| AF-R7 | **In-memory envelope cache across reconciles** | "Cache the envelope lookup in the controller's memory to avoid PVC reads" | Violates CRD-`.status`-only persistence. An in-memory cache is lost on controller restart and creates divergence between the controller's view and the actual PVC state. Temporal's durable execution model explicitly avoids this pattern. | Read from PVC (the source of truth) at each planner dispatch site. PVC reads are fast; the envelope JSON is small. |
| AF-R8 | **Collapse `retry` and `resume` into one verb** | "Simplify the CLI: just have one recovery command" | Argo's documentation failure (issue #2320 — "retry vs resubmit not documented") shows this causes user confusion when the verbs have different semantics. TIDE already has `tide resume --retry-failed` (retry failed execution tasks) and needs `tide resume --from-envelope` (skip planning). These must remain distinct with explicit flags — semantics are different and conflation breaks the mental model. | Keep `--retry-failed` (execution recovery) and `--from-envelope` (planning import) as distinct modes of `tide resume`. |

---

## Feature Dependencies

```
TS-R2 (stable envelope cache key)
    └──required-by──> TS-R1 (envelope-based planner skip)
                          └──required-by──> D-R1 (tide resume --from-envelope)
                                                └──required-by──> D-R4 (envelope export)
                                                └──enhanced-by──> D-R2 (dry-run mode)

TS-R3 (envelope staleness validation)
    └──required-by──> TS-R1 (planner skip — must validate before skipping)
    └──required-by──> D-R1 (import — must validate before writing)

TS-R4 (budget-bypass resume at Running)
    └──required-by──> TS-R5 (idempotent child-CRD re-init guard)
                          (TS-R4 is useless without TS-R5: resumes at Running but re-creates children)

TS-R6 (cap-raise ergonomics)
    └──requires──> TS-R4 (cap-raise only matters once resume path is correct)

TS-R7 (regression test for 2a5e0dc ordering fix)
    └──independent (can ship with TS-R4/TS-R5 or standalone)

D-R3 (per-level resume status on dashboard)
    └──requires──> TS-R1 (envelope skip must record that a skip happened in .status)

D-R4 (envelope export)
    └──enhances──> D-R1 (export creates the import-ready directory; import consumes it)
    └──requires──> TS-R2 (export writes using stable-key naming so import can ingest)
```

### Dependency Notes

- **TS-R2 is the foundation.** Stable keys are required by everything else. This is a schema and
  naming change (PVC path layout) that must be decided before any other work starts.
- **TS-R3 validation must gate TS-R1.** The planner skip is only safe if the envelope passes
  validation. Never skip without validating — this is the lesson from Argo memoization bug #12936.
- **TS-R4 + TS-R5 are a unit.** Fixing the `Pending` re-entry without adding the child-CRD
  idempotency guard is incomplete: the controller resumes at `Running` but then creates duplicate
  children. Both must ship together.
- **D-R1 (import command) is the headline feature** but depends on TS-R1/R2/R3 being correct
  first. Do not build the import CLI before the underlying planner-skip path is correct and tested.
- **D-R3 (dashboard badges) is independent** of the core controller work and can ship in a
  later plan within the milestone.

---

## MVP Definition

### Launch With (v1.0.3)

Minimum viable resumption — sufficient to make a budget-halted planning run resumable without
re-paying the planning cost.

- [ ] **TS-R4** — Budget-bypass resume at `Running` (fix `project_controller.go:1257`) — *why
  essential: the most damaging immediate bug; without this, clearing a halt always re-pays planning*
- [ ] **TS-R5** — Idempotent child-CRD re-init guard — *why essential: companion to TS-R4; without
  the guard, `Running` re-entry still re-creates children*
- [ ] **TS-R2** — Stable envelope cache key (content-addressed, not UID-keyed) — *why essential:
  UID churn makes any UID-keyed scheme a no-op on fresh Project apply*
- [ ] **TS-R1** — Envelope-based planner skip at all planner dispatch sites — *why essential: this
  is the mechanism that converts the stable key into actual cost savings*
- [ ] **TS-R3** — Envelope staleness validation before skip — *why essential: must preserve cycle-
  detection invariant; prevents stale envelopes from bypassing validation*
- [ ] **TS-R6** — Cap-raise ergonomics (coordinated re-check) — *why essential: without this, cap
  raise is frustrating UX and users may overshoot trying to find a cap that doesn't immediately re-halt*
- [ ] **TS-R7** — Regression test for ordering fix (`2a5e0dc`) — *why essential: a shipped fix
  without a regression test is not verifiably fixed*

### Add After Core is Working (v1.0.3 extension)

- [ ] **D-R1** — `tide resume --from-envelope <path>` import command — *trigger: TS-R1/R2/R3
  correct and tested; import command is the user-facing closure of the dogfood salvage gap*
- [ ] **D-R2** — Import dry-run mode — *trigger: D-R1 exists; dry-run adds safety, especially
  for large envelope trees*
- [ ] **D-R4** — `tide export-envelopes` command — *trigger: D-R1 exists; export makes the
  import pattern reusable without manual PVC access*

### Future Consideration (v1.0.4+)

- [ ] **D-R3** — Per-level "Resumed from envelope" dashboard badge — *why defer: non-blocking;
  the functional correctness of resume matters more than the observability at v1.0.3*

---

## Feature Prioritization Matrix

| Feature | User Value | Implementation Cost | Priority |
|---------|------------|---------------------|----------|
| TS-R4 (budget-bypass resume at Running) | HIGH — immediate re-cost-avoidance | LOW — targeted controller fix | P1 |
| TS-R5 (idempotent child-CRD guard) | HIGH — companion to TS-R4 | LOW — existence check at dispatch site | P1 |
| TS-R2 (stable envelope cache key) | HIGH — prerequisite for all envelope-skip | MEDIUM — PVC path layout change + keying function | P1 |
| TS-R1 (envelope-based planner skip) | HIGH — core cost-avoidance mechanism | MEDIUM — touches all planner dispatch sites | P1 |
| TS-R3 (envelope staleness validation) | HIGH — safety gate preserving cycle-detection invariant | MEDIUM — reuse validation path + add fingerprint check | P1 |
| TS-R6 (cap-raise ergonomics) | MEDIUM — UX fix | LOW — billing halt condition logic | P1 |
| TS-R7 (regression test for 2a5e0dc) | MEDIUM — correctness assurance | LOW — envtest addition | P1 |
| D-R1 (tide resume --from-envelope) | HIGH — closes dogfood salvage gap | HIGH — CLI command + key rewrite + validation | P2 |
| D-R2 (import dry-run) | MEDIUM — safety for operators | LOW — flag on D-R1 | P2 |
| D-R4 (envelope export command) | MEDIUM — makes import portable | MEDIUM — Job or kubectl cp wrapper | P2 |
| D-R3 (dashboard resume badge) | LOW — observability | MEDIUM — CRD field + dashboard component | P3 |

**Priority key:**
- P1: Required for v1.0.3 core — directly delivers resumption correctness
- P2: Add within milestone once P1 is working
- P3: Future milestone

---

## TIDE Invariant Compliance Summary

| Feature | Waves derived not declared | CRD-status-only persistence | Cycles are bugs (no bypass) | UID-churn safe | Notes |
|---------|--------------------------|----------------------------|---------------------------|----------------|-------|
| TS-R1 (planner skip) | SAFE — envelopes are planner inputs, not wave schedule | SAFE — skip decision is in-process, result in .status | SAFE — only if TS-R3 gates it | SAFE — only if TS-R2 used | Requires TS-R2 + TS-R3 to be safe |
| TS-R2 (stable key) | N/A | N/A | N/A | SAFE — content-addressed, not UID-keyed | Foundation of all other safety |
| TS-R3 (validation before skip) | N/A | N/A | REQUIRED — preserves cycle-detection | N/A | The invariant enforcement gate |
| TS-R4 (resume at Running) | N/A | N/A | N/A | N/A | State machine fix; no invariant conflict |
| TS-R5 (idempotent child-CRD) | N/A | N/A | N/A | N/A | Existence check; no invariant conflict |
| AF-R1 (cache wave schedule) | VIOLATION | VIOLATION | VIOLATION | N/A | Do not build |
| AF-R3 (skip cycle detection on import) | N/A | N/A | VIOLATION | N/A | Do not build |
| AF-R7 (in-memory envelope cache) | N/A | VIOLATION | N/A | N/A | Do not build |

---

## Sources

### Argo Workflows (HIGH confidence — official docs + GitHub issues)

- [Argo Workflows Step Level Memoization](https://argo-workflows.readthedocs.io/en/latest/memoization/) — cache key construction, ConfigMap storage, maxAge, skip behavior
- [Argo Workflows Retries documentation](https://argo-workflows.readthedocs.io/en/latest/retries/) — retry policy, backoff, conditional retry
- [Argo issue #12936 — memoized resubmit DAG ordering bug](https://github.com/argoproj/argo-workflows/issues/12936) — correctness hazard when skipping memoized nodes in a DAG
- [Argo issue #2320 — retry vs resubmit undocumented difference](https://github.com/argoproj/argo-workflows/issues/2320) — motivation for keeping resume verbs distinct

### Temporal (HIGH confidence — official docs + verified secondary)

- [Temporal Workflow Execution overview](https://docs.temporal.io/workflow-execution) — event history, replay, continue-as-new
- [Replay Testing to Avoid Non-Determinism in Temporal Workflows (Bitovi)](https://www.bitovi.com/blog/replay-testing-to-avoid-non-determinism-in-temporal-workflows) — determinism requirement, what breaks replay
- [Zylos AI — AI Agent Workflow Checkpointing and Resumability](https://zylos.ai/research/2026-03-04-ai-agent-workflow-checkpointing-resumability/) — Temporal event-history pattern + user expectation of "no double billing"

### Prefect (HIGH confidence — official docs)

- [Configure task caching — Prefect](https://docs.prefect.io/v3/develop/task-caching) — INPUTS cache policy, result persistence requirement, cache hit skip behavior

### Dagster (HIGH confidence — official docs)

- [Asset versioning and caching — Dagster](https://docs.dagster.io/guides/build/assets/asset-versioning-and-caching) — code_version + data_version staleness model, skip-if-unsynced behavior

### Apache Airflow (MEDIUM confidence — official docs + Astronomer)

- [Rerun Airflow DAGs and tasks — Astronomer](https://www.astronomer.io/docs/learn/rerunning-dags/) — three reprocessing modes, --rerun-failed-tasks
- [DAG Run Status — Apache Airflow](https://airflow.apache.org/docs/apache-airflow/stable/core-concepts/dag-run.html) — task state semantics
- [Airflow issue #570 — skipped tasks hang backfill](https://github.com/apache/airflow/issues/570) — pitfall: Skipped ≠ Succeeded

### Bazel / Content-Addressed Caches (HIGH confidence — official docs + ACM Queue)

- [Remote Caching — Bazel](https://bazel.build/remote/caching) — action key construction, CAS + AC separation
- [Using Remote Cache Service for Bazel — ACM Queue](https://queue.acm.org/detail.cfm?id=3287302) — action fingerprint details, input-addressed memoization

### GitHub Actions (HIGH confidence — official docs)

- [Dependency caching reference — GitHub Docs](https://docs.github.com/en/actions/reference/workflows-and-actions/dependency-caching) — cache-hit output, skip-step-on-cache-hit pattern

### LLM agent patterns (MEDIUM confidence — multiple sources)

- [Async AI Agent Workflows Survive Failures — Augment Code](https://www.augmentcode.com/guides/async-ai-agent-workflows) — per-step snapshots, event-history replay for LLM agent workflows
- [Long-Running Agents — Addy Osmani](https://addyosmani.com/blog/long-running-agents/) — structured handoff files, artifact-based session continuity, budget circuit breakers
- [Budget exceeded: response playbook — Opsmeter](https://opsmeter.io/blog/budget-exceeded-response-playbook-for-llm-teams) — soft limits with human-in-the-loop escalation

---

*Feature research for: TIDE v1.0.3 — Planning Resumption & Cost Resilience*
*Researched: 2026-06-18*
*Scope: NEW resumption/import features only; v1.0.2 features are in the prior FEATURES.md version (2026-06-15)*
