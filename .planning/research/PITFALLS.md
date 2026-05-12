# Pitfalls Research — TIDE

**Domain:** Kubernetes-native LLM-agent orchestrator (CRDs + controller + subagent Jobs)
**Researched:** 2026-05-12
**Confidence:** HIGH (cross-verified against `README.md` spec, `PROJECT.md` locked decisions, controller-runtime/Kubernetes official docs, and 2026 industry reporting on agentic and operator failure modes)

> Scope reminder. These are TIDE-class pitfalls, not generic K8s or generic agent advice. Each one lands at the intersection of: (a) the five-level paradigm in `README.md`, (b) the locked v1 decisions in `.planning/PROJECT.md` (CRD-`status`-only persistence, namespace-per-project, pod-per-task Jobs, strict-by-default failure profile, self-hosting MVP as the v1 bar), and (c) the OSS posture goal of running unmodified in arbitrary clusters.

---

## Critical Pitfalls

### Pitfall 1: Long-running work inside the reconcile loop

**Severity:** Catastrophic

**What goes wrong:**
A reconciler for `Wave` or `Task` blocks on subagent completion — waiting for a Job to finish, polling LLM provider status, or sleeping until a downstream artifact appears. The reconciler holds a worker slot for minutes-to-hours. The controller's work queue backs up; status updates lag; events on the same object cannot interrupt the running reconciliation; `kubectl describe` and the dashboard show stale state for the duration.

**Why it happens:**
Wave-walking reads naturally as "dispatch tasks → wait for them to finish → dispatch next wave." That reads as a single function. Controller-runtime's reconciler signature (`Reconcile(ctx) (Result, error)`) hides the fact that the *correct* model is to return early and let watches re-trigger the reconcile when child Jobs change status. The spec's worked example walks Kahn iteration-by-iteration in pseudocode, which subtly invites a synchronous mental model that does not match how K8s controllers should be written.

**How to avoid:**
- Reconcile must be event-driven, not procedural. Each invocation answers "what should the world look like *now*, given current cluster state?" and returns. The next invocation is triggered by watches on child Jobs.
- Set `Owns(&batchv1.Job{})` and `Owns(&tidev1.Task{})` on the controller builders so Job/Task status transitions re-enqueue the owning Wave/Plan.
- Use `RequeueAfter` only for genuinely time-based polling (e.g. external git remote freshness), never as a substitute for watch-driven re-entry.
- The wave-dispatch logic is: "for each Task with indegree 0 that isn't already running and isn't already completed, create a Job; return." Never wait inside Reconcile.

**Warning signs:**
- Reconcile p99 latency >1s in metrics
- `workqueue_depth` for any TIDE controller growing under load
- Subagent dispatch latency tracks subagent execution time (sign that you're serializing)
- Tests hang or time out when a subagent Job is slow

**Phase to address:**
Phase 1 (controller scaffold). Bakes in the wrong way if not enforced from the first reconciler. Lint rule or contributor-guide entry: "no `time.Sleep`, no blocking channel reads, no `client.Get` in a loop inside Reconcile."

---

### Pitfall 2: Re-deriving waves is treated as a smell, so the schedule gets cached

**Severity:** Catastrophic (architectural)

**What goes wrong:**
A well-meaning contributor notices that the controller computes wave layers on every Reconcile and "optimizes" by storing the computed waves on the `Plan.status` (or a sibling `Wave` CRD) so the controller can "just look up wave 2 instead of re-running Kahn." A plan edit lands a new task; the cached schedule is stale; the orchestrator dispatches against the wrong layout; debugging is a nightmare because the visible state (CRDs) disagrees with what the controller dispatched against (an in-memory cache or a stale `.status`).

**Why it happens:**
"Why recompute every time?" reads as the obvious optimization. The spec explicitly calls this out (`README.md` §"Properties of the algorithm" point 1: "Cheap enough to recompute after every plan edit. There is no reason to cache a stale schedule") and `CLAUDE.md` reiterates it, but the temptation recurs every time someone profiles the controller and sees Kahn in a flame graph.

**How to avoid:**
- Treat wave membership as a *derived view*, not stored state. The Task CRDs are the source of truth; the wave layer a Task belongs to is computed from `depends_on` + completed-task set at Reconcile time.
- If wave info needs to surface (dashboard, CLI), expose it through a virtual aggregation in the orchestrator API, not as a persisted field.
- Resumption state is exactly two things: the indegree map (derivable from the DAG) and the completed-task set (derivable from `Task.status.phase == Completed`). Anything else stored about waves is a smell.
- Code review rule: any PR adding `Status.Waves`, `Status.CurrentWave`, `Status.Schedule`, etc. requires explicit `README.md` spec amendment first.

**Warning signs:**
- New CRD field with "schedule," "wave," or "plan" in its name on Plan or Phase status
- Cache invalidation logic appearing in the wave-derivation code path
- "Stale wave" bugs reported (the cache will eventually disagree with the DAG)

**Phase to address:**
Phase 2 (Kahn implementation + Plan/Task CRDs). Encode it in the controller's type signature: the wave-derivation function takes `(tasks, edges, completed)` and returns `[]Wave`. It takes no orchestrator state pointer. It is pure. Add it to the CONTRIBUTING.md rationale section.

---

### Pitfall 3: Planning DAG and Execution DAG collapse into one generic "DAG" abstraction

**Severity:** Serious (architectural, hard to unwind once it spreads through types)

**What goes wrong:**
A clever refactor unifies `PlanningEdge` and `ExecutionEdge` into a generic `Edge` type, parameterized by node kind. The runtime then has one wave-walker that operates on `Graph[Node]`. This compiles, runs, and looks tidier — until the planner-pool and executor-pool budgets need to be sized independently, or the artifact-vs-code-mutation distinction needs to surface in subagent dispatch, or a planning failure needs different semantics than an execution failure. The unification has erased the type-level signal that separates them; failure semantics drift, log messages confuse "this task" with "this artifact," and the spec's two-budget rule is now an `if`-statement somewhere instead of a structural property.

**Why it happens:**
The Kahn algorithm is genuinely the same on both DAGs. Generic programming makes the unification look like clean DRY. The spec is clear ("Same algorithm, same properties, different inputs") but the *cost* of unification — losing typed dispatch, losing distinct budget config, losing failure-semantics specialization — is invisible from inside the refactor.

**How to avoid:**
- Keep `PlanningTask` and `ExecutionTask` (and their edge types) as distinct Go types. Yes, they have similar fields. That is fine.
- The Kahn implementation itself can be generic over node identifier types (`func Wave[T comparable](nodes []T, edges []Edge[T], completed set[T]) [][]T`) — that is the right place for generics. Wrap it in two distinct callers (`planningWaves`, `executionWaves`), each with its own context.
- Two separate `Controller`s in main.go: PlanningController watches Plan/Phase CRDs and dispatches planner Jobs; ExecutionController watches Task CRDs and dispatches executor Jobs. Two `--planner-concurrency` and `--executor-concurrency` flags, sized independently.
- CRDs: planner artifacts (`Milestone`, `Phase`, `Plan`) have a different group/kind from executor artifacts (`Task`, `Wave` if it exists). Even if the underlying schema is similar.

**Warning signs:**
- A single `Graph` or `DAG` type in the codebase used in both planning and execution code paths
- A single `--concurrency` flag instead of two
- Failure-semantic code that branches on a `Kind` field rather than living in distinct controllers
- The dashboard shows one DAG view that has to be told what mode it's in

**Phase to address:**
Phase 1 (CRD schema). Set the precedent in the API types before the controller code lands. Once the type split exists, refactor pressure to unify is naturally resisted.

---

### Pitfall 4: Treating CRD `.status` as truth instead of as cache

**Severity:** Catastrophic for the self-hosting bar

**What goes wrong:**
A resumption bug surfaces. The controller crashed mid-wave; on restart, the indegree-map cache it was holding in memory is gone. The "obvious" fix is to persist the indegree map to `Plan.status.indegreeMap`. Now the controller writes a 50-task DAG's indegree map every time a task completes. Two updates collide; one wins; the indegree map drifts from the artifact reality (the actual `depends_on` declarations in `PLAN.md` on the PVC and in git). A resume operation reads the cached `.status` and dispatches against a layout that doesn't match the artifacts. The bug is silent until a wave dispatches a task whose dependencies haven't actually merged yet.

**Why it happens:**
Resumption looks expensive when re-derived from artifacts (PVC reads, git checkout, parsing `PLAN.md`). Stuffing the derived state into `.status` looks like a clean fix. The spec explicitly warns ("`CLAUDE.md`: If the persistence layer starts wanting to store the full schedule, that's a smell — re-derive instead") but the warning lands as a stylistic preference unless the failure mode is concrete.

**How to avoid:**
- `.status` stores *observation* (what is the current phase of this Task?), not *plan* (what should the wave layout be?). Observation is cheap to update and not load-bearing for correctness — if it's wrong, the next reconcile fixes it.
- Resumption protocol: on controller restart, list all Task CRDs in the project's namespace, mark completed those whose `.status.phase == Completed`, re-parse `PLAN.md` from the PVC (or git if PVC is gone) to rebuild the DAG, recompute waves, dispatch.
- Acceptance test: kill the orchestrator pod mid-wave; bring it back; verify it picks up at the right wave without any persisted schedule.
- If etcd loses a Task CRD entirely (object deleted), the artifact (`PLAN.md` task entry, prior diff in git) is authoritative. The orchestrator can rebuild the CRD from artifacts.

**Warning signs:**
- CRD `.status` fields whose names sound like internal-state-not-observation (`indegreeMap`, `schedule`, `cachedWaves`, `derivedDag`)
- Resume code reads `.status` to know what to dispatch
- Backups/snapshots of etcd start being treated as authoritative against artifacts
- Tests pass with PVC wiped but fail if `.status` is reset

**Phase to address:**
Phase 1 (CRD schema design). Lock the principle: status fields must be observation, not derivation. Add a CRD-schema review checklist item.

---

### Pitfall 5: Cycle "recovery" creeps in instead of refusing cyclic plans

**Severity:** Serious (paradigm violation, debugging-hostile)

**What goes wrong:**
A user submits a plan with a cycle (probably accidentally — `A depends_on B`, `B depends_on A` after a rename refactor). The orchestrator dutifully refuses to run. The user files an issue. A maintainer adds a "cycle-breaking heuristic" — break the lowest-weight edge, log a warning, run anyway. Now plans with cycles silently produce wrong execution orders; the warning gets filtered to debug logs; and the cycle that should have been a 5-minute fix in the plan becomes a 3-day debug session because the orchestrator masked it.

**Why it happens:**
Refusing-to-run feels user-hostile in the moment. Heuristics feel "smart." Every operator team eventually has someone propose this. The spec is explicit (`README.md` §"Why this specific algorithm" point 4: "TIDE refuses to start a run on a cyclic DAG — cycles are bugs in the declared plan, not runtime conditions to recover from"; `PROJECT.md` Out of Scope: "Wave or cycle 'recovery' features").

**How to avoid:**
- Cycle detection runs at plan-validation time, before any Task CRD is admitted (validating webhook on Plan and Task CRDs).
- The error returned to the user must point to the offending edges concretely: "Cycle: A → B → C → A. Edges: A.depends_on=[B] at PLAN.md:42, B.depends_on=[C] at PLAN.md:51, C.depends_on=[A] at PLAN.md:60. Fix one of these and re-apply."
- No `--allow-cycles`, `--break-cycles`, `--cycle-policy` flag. Don't add the lever.
- Test case in admission webhook tests: a cyclic plan is rejected with a useful error; no `Wave` resources are ever produced.

**Warning signs:**
- PRs adding "cycle heuristic," "cycle breaker," or "cycle policy" code
- Issue-tracker pressure to "just warn and run anyway"
- Cycle detection moving from validation-time to dispatch-time (suggests prep for runtime recovery)

**Phase to address:**
Phase 2 (Kahn implementation + admission webhook). Bake the validating webhook in at the same time as the Kahn implementation; the webhook is the enforcement surface.

---

### Pitfall 6: Unified planner + executor worker pool

**Severity:** Serious (paradigm violation, performance regression)

**What goes wrong:**
For "simplicity," one worker pool sized at `--concurrency=20`. A heavy planning wave (most phases plan in parallel from the architecture spec — Planning DAG fans out wide) consumes all 20 slots writing `PLAN.md` files. Meanwhile, an execution wave of just 4 file-touching tasks (Execution DAG fans out narrow) is starved waiting for planners to finish. Or the inverse: long-running task executions starve a planning wave that could have completed in seconds.

The spec is loud about this ("`README.md` §"Why this is advantageous" point 3: 'tons of parallel planners, fewer parallel executors'") and `CLAUDE.md` reiterates ("Don't unify them into one worker pool"). Unification destroys the structural advantage.

**Why it happens:**
One pool is fewer flags, fewer code paths, less config surface. The spec's argument for two budgets is *empirical* (planning fans wide, execution fans narrow); it doesn't show up in a unit test.

**How to avoid:**
- Two separate sigs.k8s.io/controller-runtime `Manager`s, or two Controllers with their own `MaxConcurrentReconciles` settings.
- Two Kubernetes ResourceQuotas (or a custom dispatch-quota tracked in controller state) — one bounding the count of in-flight planner Jobs in the project's namespace, one bounding executor Jobs.
- `tide` CLI config: `--planner-concurrency` and `--executor-concurrency`, never `--concurrency`.
- Default ratio in the Helm chart: planner concurrency 4× executor concurrency, with comments explaining why.
- Metrics: `tide_planner_inflight` and `tide_executor_inflight` as separate gauges, never aggregated.

**Warning signs:**
- A single `WorkerPool` or `Dispatcher` type used for both kinds of work
- A single `--concurrency` flag
- Issues filed about "planning is slow when execution is busy" (sign that the budgets are coupled)

**Phase to address:**
Phase 1 (controller scaffold). Two controllers from day one; flags wired separately.

---

### Pitfall 7: Subagent context bleed via shared PVC

**Severity:** Catastrophic (security + correctness)

**What goes wrong:**
Two parallel subagents in the same wave share a PVC for artifacts. Subagent A is told to read `phase-1-brief.md` and write `plan-1.md`. Subagent B is told to read `phase-2-brief.md` and write `plan-2.md`. But A and B both have read access to the *entire* PVC and the executor harness's system prompt does not scope what they can read. Subagent A "helpfully" reads `phase-2-brief.md` (because it's there) and starts coordinating with phase 2, producing a `plan-1.md` that's actually entangled with phase 2's interface decisions — violating the dependency declaration. Worse: subagent B is later compromised by a prompt injection in an upstream artifact, and writes a malicious file to the PVC that A then reads.

This compounds with the prompt-injection threat surface. Google researchers report a 32% YoY increase in malicious prompt-injection payloads in web content (Nov 2025 → Feb 2026), and multi-agent pipelines specifically allow hijacked agents to "propagate the attack downstream — instructing subsequent agents, poisoning shared memory, or manipulating orchestrator decisions" ([arxiv: From Prompt Injections to Protocol Exploits](https://arxiv.org/html/2506.23260v1)).

**Why it happens:**
A shared PVC for "the run's artifacts" is the obvious implementation. Per-subagent volume scoping is more work. The spec's locked decision in `PROJECT.md` is "shared PVC during a run" — which is right for *persistence* but is silent on read scoping.

**How to avoid:**
- The shared PVC is the *write* target. *Reads* into a subagent container should be scoped: mount a per-Job subdirectory read-write, and mount only the explicitly-declared upstream artifacts read-only.
- The subagent dispatcher computes the read-set from declared `depends_on` (artifacts produced by upstream tasks/plans) and writes a manifest the harness uses to assemble the container's view.
- Artifacts written by subagents are treated as *untrusted input* when read by a downstream subagent. The orchestrator does not template upstream-subagent output directly into a downstream-subagent's system prompt — it goes in as a `<file>` reference the downstream agent can read but is wrapped in clear "this is data, not instructions" framing.
- Never let a subagent write to anything outside its task's declared output paths. Enforce at the harness layer: post-Job, validate that the diff produced touches only declared files. Reject the result if it doesn't.

**Warning signs:**
- Subagent system prompts include "you may read any file in `/workspace/`"
- No declared `outputs` field on Task CRD (so anything goes)
- Plans completing "faster than expected" or with surprising cross-phase coupling
- A subagent's diff touches files the task didn't declare

**Phase to address:**
Phase 2 (Subagent interface + harness). Designing the harness without read-scoping bakes in the vulnerability. The Subagent interface contract should require declaring `inputs` and `outputs`.

---

### Pitfall 8: Runaway agent loops drain budget

**Severity:** Catastrophic (financial)

**What goes wrong:**
A subagent hits an unexpected error, retries, hits a different error, replans, retries. Each iteration burns tokens. Industry reports document "$25-per-conversation" and "four-figure spend in 90 seconds" runaway cases ([RelayPlane: Agent Runaway Costs](https://relayplane.com/blog/agent-runaway-costs-2026), [LangWatch 2026 monitoring tools](https://langwatch.ai/blog/4-best-tools-for-monitoring-llm-agentapplications-in-2026)). A single buggy plan can produce a wave where five parallel subagents each burn $500 before someone notices.

**Why it happens:**
The default subagent harness ships a model that runs until it decides it's done. Without per-Job iteration caps and per-Job token-spend caps, a buggy plan or a flaky tool turns into a budget event. The strict-by-default failure profile (`PROJECT.md`) does not protect against this — it protects *dependents* from running, not the failing task itself from spending.

**How to avoke:**
- Per-Task budget: max wall-clock, max iterations, max input tokens, max output tokens. Set on the Task CRD spec, defaults in the Project CRD, hard cap in the orchestrator config that no Project can exceed.
- The subagent harness enforces all four — refuses to start if not set, kills the inner agent loop when any cap hits, returns a structured "budget-exceeded" failure to the orchestrator.
- Per-Project rolling-window spend gate. If the project burns >3× its trailing-7-day spend rate in the current 15-minute window, the controller pauses new dispatches and surfaces a Slack-tide review checkpoint.
- Per-Project absolute cost cap (configurable). Project paused when hit. Resumes require explicit human approval (CLI command).
- Metrics: per-Task, per-wave, per-project token spend; export to Prometheus with project/phase/plan/task labels (bounded cardinality — see Pitfall 17).

**Warning signs:**
- Subagent Jobs with no `activeDeadlineSeconds`
- A Task taking >10× expected duration
- The same task failing and being retried automatically without an iteration cap
- Spend metrics not exported by the orchestrator at all

**Phase to address:**
Phase 2 (Subagent harness). Budget enforcement is a *harness* property, not an orchestrator afterthought. Must land with the first concrete subagent impl.

---

### Pitfall 9: LLM API rate-limit handling across parallel subagent Jobs

**Severity:** Serious

**What goes wrong:**
A wave dispatches 20 parallel subagent Jobs. Each makes calls to Anthropic. The provider's per-org request-per-minute or token-per-minute limit trips. Pods get 429s. The harness retries with naive backoff; all 20 retry at roughly the same time; thundering herd; more 429s; some Jobs eventually fail their `backoffLimit`; tasks marked failed; dependents never dispatch; downstream waves never run; the run is bricked despite the per-task work being correct.

**Why it happens:**
The orchestrator dispatches at the wave level without provider-aware concurrency. Each subagent pod retries in isolation, blind to its siblings. The K8s Job controller's `backoffLimit` (default 6, with exponential backoff) was not designed for cross-job rate-limit coordination.

**How to avoid:**
- Token-bucket rate limiter shared across the project (or namespace, or whole installation — depending on whether keys are per-project), implemented in the controller layer. Subagent dispatches that would exceed the bucket are *not Job-created* — they wait in the controller's work queue.
- Provider-specific rate-limit budgets surfaced as Project CRD config (`spec.providers.anthropic.requestsPerMinute`, `spec.providers.anthropic.tokensPerMinute`). Defaults populated from documented tier limits, overridable.
- 429 responses inside the subagent harness *exit the Job non-zero with a typed retryable error*. The controller treats this as "wave-internal soft failure, retry with backoff at the dispatch layer," not as a hard task failure.
- Wave dispatch is *not* "create all N Jobs simultaneously." It's "create up to min(wave_size, executor_budget, rate_budget) Jobs; create more as earlier ones complete or as the rate bucket refills."
- Telemetry: `tide_provider_rate_limit_hits_total` counter per provider, per project.

**Warning signs:**
- Subagent Job logs show 429 errors
- `Job.spec.backoffLimit` is the only retry mechanism for rate limits
- Concurrent Job count in a namespace matches wave size exactly (sign of no rate-aware throttling)
- Failed runs that succeed if you re-apply the same Project later (overload was transient)

**Phase to address:**
Phase 2 (Subagent harness + dispatch). Build the rate-aware dispatch loop before the first multi-task wave can run.

---

### Pitfall 10: Indegree updates on partial wave failures

**Severity:** Serious (correctness)

**What goes wrong:**
Wave 2 dispatches three tasks: A, B, C. A and B succeed; C fails. The orchestrator updates indegree for *all* downstream tasks (assuming all of wave 2 completed), so a task in wave 3 that depended only on C has its indegree decremented to zero — and gets dispatched, even though C never produced its output. The downstream task either fails or, worse, succeeds against a missing-or-stale input and produces garbage.

**Why it happens:**
The cleanest implementation of "wave complete → decrement successors" assumes wave atomicity. The spec's failure handling section is specific (`README.md` §"Failure handling at wave boundaries": "Tasks in wave k+1 that depend on the failed task → never dispatched. Their indegree never reaches zero"), but a naive controller implements wave-completion as a single transition rather than per-task completion.

**How to avoid:**
- Indegree decrements are *per-task-completion*, not per-wave. When Task X completes successfully, decrement indegree for each task that lists X in its `depends_on`. When Task X *fails*, do nothing to indegrees — by construction, dependents of X have a non-zero indegree contribution from X that will never decrement.
- The "wave" abstraction is a *view* (layer-k tasks that are currently dispatchable), not a synchronization point that triggers downstream dispatch.
- Test case: a wave of three tasks, one fails. Verify dependents of the failed task have indegree > 0 forever (never dispatched). Verify dependents of the *successful* siblings dispatch normally.
- Test case: a "wave 3" task depends on (succeeded A, failed C). Verify it never dispatches, regardless of how the orchestrator counts wave completions.

**Warning signs:**
- A `Wave` CRD or in-memory wave object with a "wave completed" event that triggers downstream dispatch
- Successor dispatch logic looks at `wave.status` rather than per-task `Task.status`
- Indegree decrements happen in batch at end-of-wave

**Phase to address:**
Phase 2 (Kahn implementation). Encode in the algorithm signature: `OnTaskCompleted(taskID)` is the only entry point that mutates indegree. There is no `OnWaveCompleted`.

---

### Pitfall 11: Watch-lag duplicate dispatch

**Severity:** Serious (correctness)

**What goes wrong:**
The controller's informer cache reports a Task's status as `Pending`. The controller creates a Job for it. The Job starts. The controller crashes (or reconciler enqueues twice, or leader election flaps). The new reconcile run reads from a stale informer cache, sees the Task as still `Pending` (the controller's status update hadn't propagated), creates a *second* Job for the same Task. Two subagents now race to write the same artifact; git push collisions; cost doubled; non-deterministic output depending on which finishes first.

K8s' own community has been wrestling with this — watch cache lag is "100-500ms typical, 5-30 seconds possible on a busy cluster" ([Shan Valleru: Eventual Consistency and Stale Caches](https://svalle.ru/posts/kubernetes/stale-cache-controllers/)).

**Why it happens:**
Watches are eventually consistent. The informer cache is a *snapshot*. Two reconciles for the same object can both observe a pre-mutation state. The naive create-if-not-exists check uses the informer, not a fresh API read.

**How to avoid:**
- Make Job creation idempotent. Use a deterministic Job name derived from Task UID + attempt number (`tide-task-{task-uid}-{attempt}`). Two reconciles trying to create the same Job → the second hits `AlreadyExists` and treats that as success, not as a new dispatch.
- Owner references: every Job created for a Task has `ownerReferences=[{kind: Task, uid: <task-uid>}]`. K8s garbage-collects on Task deletion.
- Status guards: before creating a Job, check `Task.status.activeJobName != ""`. If set, the orchestrator already dispatched (or thinks it did). Verify by reading the Job; resync if it exists; clear and re-dispatch if it doesn't.
- For the rare case where a fresh API read is needed (to defeat informer staleness), use `client.New(...)` with `CacheReader: false` for that specific check, or `apireader.Get`.
- Acceptance test: kill the controller during dispatch; restart; verify only one Job exists per Task.

**Warning signs:**
- `Job` names are randomized (sign that they're not idempotent)
- Two Jobs found for the same Task UID in a debug session
- Doubled token spend on a single run
- `kubectl get jobs -l tide.io/task=<uid>` returns >1 active job

**Phase to address:**
Phase 2 (subagent dispatch). Idempotent dispatch is the first thing to get right; retrofitting it later is much harder.

---

### Pitfall 12: Bootstrap deadlock — can't build the next milestone because the orchestrator can't run yet

**Severity:** Catastrophic for v1 (blocks the self-hosting bar from being reached)

**What goes wrong:**
The v1 bar is "TIDE drives its own next milestone" (`PROJECT.md` Core Value). Reaching that bar requires TIDE to be deployable and reasonably working *before* TIDE can drive the milestones that finish making it deployable and reasonably working. Without explicit thought, this becomes a circular dependency:
- Milestone N requires CRDs, controller, dispatch loop, harness
- TIDE-orchestrated authoring of Milestone N requires CRDs, controller, dispatch loop, harness
- Therefore Milestone N must be authored manually
- But every subsequent milestone might *also* require manual work until "good enough" is reached
- The "good enough" threshold is fuzzy; it slips; v1 ships without self-hosting actually happening

This is the classic compiler bootstrap problem ([Wikipedia: Bootstrapping (compilers)](https://en.wikipedia.org/wiki/Bootstrapping_(compilers))). Two-stage and three-stage bootstraps are standard mitigation in that domain; TIDE needs an explicit analog.

**Why it happens:**
"Dogfooding" is treated as the *end goal*, not as a *milestone in the plan*. Without explicit stages, the bar slides indefinitely.

**How to avoid:**
- Designate an explicit *bootstrap milestone* (call it M0 — "TIDE-on-host-runs-TIDE-on-self"). This is the *minimum* set of TIDE features required for TIDE-the-orchestrator-on-the-host to author a real `MILESTONE.md` / phase brief / `PLAN.md` for the *next* TIDE milestone in this repo. It is hand-authored using GSD, but its scope is bounded to "just enough to dogfood."
- Designate a *self-hosting milestone* (call it M_self — "TIDE-in-cluster-runs-TIDE-on-self"). M_self consumes the artifacts of M0; in M_self, a fresh TIDE installation in a kind cluster takes the M0 outputs and *re-derives* the same artifacts (or improves them), proving the orchestrator can do what the human did with GSD.
- Two-version skew tolerance: the running TIDE (bootstrap version) authors artifacts that the next-version TIDE consumes. CRD schema must not be breaking-changed between bootstrap-TIDE and self-hosted-TIDE *within v1*. (After v1, conversion webhooks handle this — but v1 keeps schema stable.)
- Acceptance criterion for v1 shipping: a fresh kind cluster + Helm install + `tide` CLI authoring this repo's next milestone, producing artifacts a human would have written.

**Warning signs:**
- "We'll dogfood eventually" without an explicit milestone for it
- M_self keeps getting pushed back because of last-minute scope
- Bootstrap milestone keeps growing as people add "while we're at it" features
- Active CRD schema changes within v1 between milestones (sign that version skew will be unmanageable when self-hosting kicks in)

**Phase to address:**
Phase 0 (roadmap construction). The roadmap itself must name and order M0 and M_self explicitly, before any code is written.

---

### Pitfall 13: TIDE-orchestrated artifacts overwrite manual work mid-self-hosting

**Severity:** Serious (loses self-debuggability)

**What goes wrong:**
During the self-hosting transition, a human is iterating on TIDE's controller code on the host. TIDE-in-cluster, driving its own next milestone, decides the right `PLAN.md` for a refactor and commits it to a feature branch. Meanwhile the human committed a divergent set of changes to the same files. Merge conflicts; or worse, the orchestrator's commits silently overwrite the human's mid-air work because the orchestrator pushes faster. The human loses the ability to debug TIDE by editing TIDE's source — because TIDE keeps rewriting it.

**Why it happens:**
Once dogfooding starts, both the human and the orchestrator can author commits. Git's last-writer-wins push semantics combined with parallel branches turns this into a coordination problem ([git push race conditions](https://git.vger.kernel.narkive.com/9Rkrrepp/push-race-condition)).

**How to avoid:**
- TIDE-driven runs always work on a branch named after the run (`tide/run-<project>-<timestamp>`). Never on `main`, never on a human's working branch.
- Each level boundary pushes to the run's branch with `--force-with-lease` (refuses to clobber unexpected upstream changes).
- The human merges TIDE's branch into `main` after review. The merge is the slack-tide gate.
- `git push` from the orchestrator never targets `main` directly. Configure the git push credentials so this is enforced at the remote, not just at the orchestrator (use a deploy key scoped to `tide/*` refs).
- During active TIDE development, the human and TIDE work on different branches. The roadmap explicitly schedules "supervised mode" milestones during the transition to autonomy, where every level boundary requires human approval before the next dispatches.

**Warning signs:**
- The orchestrator's git push targets `main`
- The orchestrator does not use `--force-with-lease`
- Human edits to a file are reverted by a TIDE commit and no one notices
- The human cannot work on TIDE source while TIDE runs

**Phase to address:**
Phase 3 (git integration). The branching discipline must be encoded before TIDE pushes to a repo it might also be a contributor to.

---

### Pitfall 14: Hard-coded provider/host assumptions slip into "agnostic" code

**Severity:** Serious (OSS posture)

**What goes wrong:**
`PROJECT.md` is explicit: "Pluggable Subagent interface from day one." "No hard-coded git host." "No hard-coded LLM provider." But under deadline pressure, an Anthropic-specific retry-after header parser lands in the orchestrator (not the harness); a GitHub-style `Pull-Request-Number` field appears on Status (not the git remote driver); the dashboard's diff renderer hardcodes GitHub's URL pattern. Each is small; cumulatively they make TIDE un-installable in clusters using GitLab + a non-Anthropic provider.

**Why it happens:**
Day-one abstraction is hard to police. The concrete impl (Anthropic + GitHub) makes for legible code; abstraction adds layers that look like overengineering until the second impl arrives. By then, leaks have spread through types.

**How to avoid:**
- The `subagent` and `gitremote` package boundaries are *firewalls*: the orchestrator depends on the interfaces; concrete implementations live behind build tags or as separate packages. The orchestrator's go.mod has no direct dependency on the Anthropic SDK or GitHub-specific libraries.
- Lint rule (custom go-analyzer): the `pkg/orchestrator/` tree may not import any package matching `*/anthropic/*` or `*/github/*`. CI fails the build.
- Second-impl test: ship a `stub-subagent` and `stub-gitremote` that record dispatches without actually calling anything. Run all integration tests against both the real and stub impls. Drift between them surfaces immediately.
- CRD field names are provider-agnostic: `spec.modelProfile` not `spec.claudeModel`; `spec.gitRemote.url` not `spec.github.repo`.

**Warning signs:**
- Imports of provider-specific SDKs in non-harness packages
- CRD field names containing provider proper nouns
- Provider-specific config keys at the orchestrator level (vs. inside provider-specific Secret payloads)
- "We'll add another provider later" justifying a leak now

**Phase to address:**
Phase 2 (Subagent interface). Once the interface lands, the lint rule is the cheap enforcer.

---

### Pitfall 15: K8s RBAC scope creep

**Severity:** Serious (security, OSS adoption blocker)

**What goes wrong:**
The controller needs to manage CRDs in the project's namespace. Initially the RBAC binds tightly. Then a feature needs to read a ConfigMap in another namespace; the rolebinding becomes a clusterrolebinding "for now." Then someone needs `secrets` access cluster-wide. Then `verbs=*`. Now TIDE installs require cluster-admin during installation, security teams reject it, OSS adoption stalls. Industry reporting documents this exact pattern as the #1 cause of operator-adoption friction in security-conscious environments ([Kubernetes RBAC good practices](https://kubernetes.io/docs/concepts/security/rbac-good-practices/), [Portainer: Kubernetes RBAC 2026](https://www.portainer.io/blog/kubernetes-rbac)).

**Why it happens:**
RBAC files are tedious. Wildcards are easy. The first time something doesn't work, "give it cluster-admin and verify the rest works" is the fast debug path that often becomes the permanent fix.

**How to avoid:**
- Use kubebuilder RBAC markers per controller (`+kubebuilder:rbac:groups=tide.io,resources=tasks,verbs=get;list;watch;update;patch`). Never wildcards.
- Strictly enumerate verbs and resources. `verbs=*` and `resources=*` rejected at PR review.
- Project-namespace-scoped: most permissions are Role + RoleBinding in the project's namespace, not ClusterRole. The only ClusterRole TIDE needs is for cluster-scoped resources it watches (and ideally there are none).
- The Helm chart installs the minimum RBAC needed for the feature flags enabled. Optional features (e.g. cluster-wide dashboards) require *opting in* to additional permissions.
- Test: install in a cluster as a non-admin user with only the documented install permissions. If the install fails, the docs lie; fix the docs *or* reduce the RBAC.
- Document the exact permissions and *why*, in `docs/RBAC.md`. Security review teams read this first.

**Warning signs:**
- A `*` in any verbs or resources field of an RBAC manifest
- Cluster-scoped role grants that aren't justified in a code comment
- "Just give it cluster-admin to test" appearing in install instructions
- Issues filed about install failures in restricted-RBAC environments

**Phase to address:**
Phase 1 (controller scaffold). Kubebuilder markers on every controller from the first PR.

---

### Pitfall 16: Breaking CRD schema changes after release

**Severity:** Catastrophic (post-v1; planning matters now)

**What goes wrong:**
v1.0 ships with CRD `tasks.tide.io/v1`. v1.1 adds a required field; existing Task resources in clusters that upgraded fail validation; clusters refuse to upgrade; users must `kubectl delete` Tasks before upgrading, losing run state. Or: a field is renamed without a conversion webhook; old objects in etcd cannot be deserialized; the controller crashloops on startup; only a full wipe and reinstall recovers.

Helm makes this worse: "CRDs are never installed on upgrade or rollback" by default — Helm v3 doesn't update CRDs to prevent accidental data loss, which causes version skew ([Helm CRD installation upgrades guide 2026](https://oneuptime.com/blog/post/2026-01-17-helm-crd-installation-upgrades/view)).

**Why it happens:**
The first CRD schema is rarely right. The temptation to "fix" it in a minor version is enormous. K8s' CRD versioning + conversion webhook story is non-trivial ([CRD versioning](https://kubernetes.io/docs/tasks/extend-kubernetes/custom-resources/custom-resource-definition-versioning/)) and skipping it looks reasonable until it bites.

**How to avoid:**
- CRD schemas are conservative for v1: every new field is optional with sensible defaults; nothing in `spec` is `required` unless absolutely necessary; `status` schema is permissive.
- Adopt "v1alpha1 → v1beta1 → v1" naming for pre-stable schemas. Bump alpha freely; bump beta when something stable-looking is reached; bump v1 only when the API is committed.
- Conversion webhook scaffold from day one — even if v1alpha1 is the only version, the *infrastructure* for serving conversions is in place. Adding v1alpha2 later is then a schema PR, not a infrastructure PR.
- Helm chart includes a dedicated CRD subchart so upgrades update CRDs explicitly. Document the upgrade path.
- Run `kubectl-convert`-style migration tests in CI: take v1alpha1 fixtures, convert to v1alpha2, ensure roundtrip equality where required.
- Never remove a field within the same version. Mark it deprecated; remove only when bumping the version (and provide the conversion).

**Warning signs:**
- New required fields in a CRD without a version bump
- Helm chart doesn't include CRD upgrades
- Conversion webhook missing or untested
- Issues filed about upgrade failures
- No `kubectl-convert` test fixtures

**Phase to address:**
Phase 1 (CRD schema). Set the version-bump discipline before users exist. Conversion webhook scaffolding before any second-version pressure.

---

### Pitfall 17: Observability data volume explodes

**Severity:** Serious

**What goes wrong:**
The spec calls for OpenTelemetry tracing with OpenInference conventions on every Milestone → Phase → Plan → Task subagent chain. Each LLM call becomes a span. Each span carries the prompt and completion as attributes (because OpenInference says so). For a 200-task run, that's ~1000 spans with multi-kilobyte attributes each. Industry reporting: "adding AI workload monitoring increased observability bills by 40-200%" ([OneUptime: AI Workload Observability Cost Crisis](https://oneuptime.com/blog/post/2026-04-01-ai-workload-observability-cost-crisis/view), [Uptrace: OpenTelemetry for AI Systems 2026](https://uptrace.dev/blog/opentelemetry-ai-systems)). High-cardinality labels (per-task UID metric labels) explode Prometheus storage.

**Why it happens:**
Tracing every LLM call feels right. The cost shows up later, on a bill, not in code.

**How to avoid:**
- Tail-based sampling: complete the trace, then decide. Head-based sampling does not work for AI workloads ([Uptrace 2026](https://uptrace.dev/blog/opentelemetry-ai-systems)). Sample 100% of failed traces; sample a fraction of succeeded ones; sample expensive ones unconditionally.
- Prompt/completion payloads are large: store them as artifact references (PVC + URL), not as span attributes. Spans carry the *reference*, not the content.
- Prometheus label cardinality discipline: per-task UID labels are *forbidden* on metrics. Aggregate by project, phase, plan, level — not task. If per-task observability is needed, that's tracing, not metrics.
- Document the expected observability cost per run-of-N-tasks in the OSS docs so operators can budget.
- The Helm chart's default OTel exporter ships with conservative sampling and aggregate-only metrics. "Full fidelity" is opt-in.

**Warning signs:**
- Prometheus cardinality explosion alerts
- LLM-payload bytes in span attributes
- OTel-collector OOM kills under load
- Users filing issues about observability cost

**Phase to address:**
Phase 4 (Observability). Cardinality discipline must be baked into the instrumentation; retrofitting is expensive.

---

### Pitfall 18: Secret leakage in artifacts and logs

**Severity:** Catastrophic (security)

**What goes wrong:**
A subagent is given a task that involves "set up the OAuth callback." The harness passes API keys via env vars (correct). But the subagent, given access to those env vars, helpfully writes them into a `PLAN.md` comment: "I've configured the callback with key=sk-ant-xxx". The PLAN.md is pushed to the git remote. Now an API key is in a git repo. Or: the subagent writes "the credentials are configured" to stdout; the orchestrator captures stdout into logs; logs go to Loki; logs are indexed and searchable. GitGuardian reports 28.6M secrets exposed in public GitHub commits in 2025 — a 34% YoY increase — with one provider (OpenRouter) seeing leaks grow >48x year-over-year ([Help Net Security 2026](https://www.helpnetsecurity.com/2026/04/14/gitguardian-ai-agents-credentials-leak/), [Doppler: Advanced LLM security](https://www.doppler.com/blog/advanced-llm-security)).

Research consensus from 2026: "If an agent can see it, it can leak it. The safest approach is preventing secrets from entering the agent's context at all."

**Why it happens:**
Subagents have agency; verbose output is the default; "redact at log-write" is hard to make complete.

**How to avoid:**
- Subagents do not get raw API keys in env vars or in their context. The harness mediates: it provides a *signed token* the subagent uses to make API calls via a local proxy in the same pod. The proxy injects the real key. The token is meaningless if leaked.
- For credentials that genuinely must be in the subagent context (e.g. a git remote PAT for the case where the harness can't proxy the git push): use scoped, short-lived credentials minted per-Job from a base credential held by the orchestrator. The subagent's leak is bounded in scope and time.
- Log scrubbing: the harness wraps subagent stdout/stderr with a regex-based redactor (`sk-ant-*`, `sk-*`, `gh[ps]_*`, `xox[abp]-*`, etc.). Tested against known token formats. Logged after redaction. Never logged before.
- Artifact scanning: at every git-push level boundary, run a secrets-scanner (gitleaks or equivalent) over the diff. If a secret pattern is detected, the push fails, the task is marked failed-with-leak, the leak event is surfaced to the operator immediately.
- Never put credentials in CRD spec. Always reference K8s Secrets by name (already locked in `PROJECT.md`).
- Subagent system prompts explicitly instruct: "never reference credentials, API keys, or tokens in any output."

**Warning signs:**
- Logs contain strings matching API key formats
- Subagent context includes env vars whose names match `*_KEY`, `*_TOKEN`, `*_SECRET`
- No secrets-scanner in the git-push pipeline
- Audit shows credentials accessed by tasks that shouldn't need them

**Phase to address:**
Phase 2 (Subagent harness) and Phase 3 (git integration). Both the harness-side proxy and the push-side scanner are required; neither alone is sufficient.

---

### Pitfall 19: Hallucinated `depends_on` edges that pass validation

**Severity:** Serious

**What goes wrong:**
A planner subagent writes a `PLAN.md` that declares `task-7 depends_on [task-3, task-5]`. task-3 and task-5 exist, so admission validation passes. But the *real* dependency is on task-4 — the planner missed it because the file-touch sets are similar. Task-7 dispatches in an earlier wave than it should, against incomplete state. The diff it produces is wrong; tests fail; or worse, tests pass against the wrong baseline and the failure surfaces three plans later.

**Why it happens:**
DAG validation checks *consistency* (no cycles, all referenced tasks exist), not *correctness* (the declared dependencies match the real ones). LLMs hallucinate plausible-looking edges.

**How to avoid:**
- Derive expected edges from declared file-touch sets, not from prose. A `PLAN.md` task entry declares `outputs: [path/to/file.go]` and `inputs: [path/to/other.go]`. The orchestrator computes "if task A's output is in task B's inputs, B depends on A." This *generated* DAG is reconciled against the LLM-declared DAG; mismatches surface as warnings (strict mode: rejections).
- Plan-review subagent: a separate subagent reviews the declared DAG against the architecture spec before admission. This is itself a Slack-tide gate.
- The plan author writes natural-language *reasons* for each edge; a reviewer subagent (or human) sanity-checks the reasons. Reasons that are obvious tautologies (`task-7 depends_on task-3 because task-3 is upstream`) are rejected.
- Acceptance tests at the plan level: every dispatched task that fails because of a missing input is automatically tagged as "missing-dependency" in its failure mode, and the surfaced report says exactly which file was missing and which task produced it.
- Over time, the orchestrator can use historical "missing-dependency" failures to *suggest* edges the planner missed.

**Warning signs:**
- Tasks failing with "file not found" errors that *would* have been produced by a later task
- Plans that produce surprisingly few waves (sign that dependencies aren't being declared)
- Plans that produce surprisingly many waves (sign of over-declared deps, the safer error)
- File-touch consistency check disabled or skipped

**Phase to address:**
Phase 2 (Plan CRD + admission). File-touch derived edges must be in the admission flow from day one.

---

### Pitfall 20: Tests requiring real LLM API credits

**Severity:** Annoying (but corrosive — slows iteration)

**What goes wrong:**
Integration tests hit real Anthropic to verify end-to-end. The test suite costs $5/run. Contributors don't run it locally; CI burns the budget; flaky LLM responses cause flaky tests; the team adds retries; the test suite becomes slow and untrustworthy. Or worse: the LLM impl is mocked in a way that doesn't catch real failure modes (always-returns-success mock makes the executor look correct when it can't actually handle a 429 or a malformed completion).

Research from 2026: "Mock tool functions while using actual LLM tool calling mechanisms. This is the most common mocking pattern for modern agents" ([LangWatch 2026 testing guide](https://langwatch.ai/scenario/testing-guides/mocks/)).

**Why it happens:**
End-to-end faith requires real LLM calls; cost requires mocks; the team picks one and lives with the consequences.

**How to avoid:**
- Three test tiers, run in CI in this order:
  1. **Unit (no LLM):** Kahn algorithm, indegree updates, validation logic, controller reconcile-without-external-effects. Runs in <30s. Required for every PR.
  2. **Integration with stub-subagent:** envtest + a `stub-subagent` impl that returns canned responses (success/failure/rate-limit/malformed) on demand. Runs in <5min. Required for every PR. Catches dispatch logic, wave walking, status transitions, RBAC, finalizers.
  3. **Live E2E with real LLM:** Real provider, real kind cluster, small fixture project. Runs nightly, not per-PR. Costs bounded ($N/run cap enforced by the harness's budget caps from Pitfall 8).
- The `stub-subagent` impl is part of v1 (locked in `PROJECT.md` already implies pluggability — make stub explicit).
- The stub can be programmed to return specific failure modes per-test, so failure-handling logic gets test coverage that real LLMs make non-deterministic.
- envtest gaps: envtest doesn't run a Kubelet, so Job pods aren't actually scheduled. Pair envtest with a fake-Pod controller that simulates Job lifecycle (success/failure/timeout) on a configurable delay. This catches dispatch-loop bugs without a real cluster.

**Warning signs:**
- Test cost monthly > $50 for the project
- CI tests skipped because "they're flaky"
- Failure-mode coverage relies on observing real LLM flakes rather than deterministic stubs
- No `stub-subagent` impl

**Phase to address:**
Phase 2 (Subagent interface). The stub impl ships with the interface, before the concrete impl.

---

### Pitfall 21: Finalizer leaks

**Severity:** Serious

**What goes wrong:**
Project CRDs have finalizers (`tide.io/cleanup-jobs`). The controller crashes during a `Project` deletion; the finalizer is not removed. The Project is stuck in `Terminating` forever; its namespace is stuck in `Terminating` forever; users can't redeploy; the only fix is `kubectl patch ... --type=merge -p '{"metadata":{"finalizers":null}}'`. This is a documented K8s pain point ([Kubernetes Finalizers](https://kubernetes.io/docs/concepts/overview/working-with-objects/finalizers/), [Jorijn: namespace stuck in Terminating](https://jorijn.com/en/knowledge-base/kubernetes/troubleshooting/kubernetes-namespace-stuck-terminating/)).

**Why it happens:**
Finalizers are the right tool for cleanup; their failure modes are easy to underestimate. A finalizer that depends on an external system (git remote, LLM provider) inherits that system's downtime as deletion downtime.

**How to avoid:**
- Finalizer logic must be *bounded in time*. Set a deadline (5 minutes). After deadline, log loudly, remove the finalizer anyway, surface a `FinalizerTimedOut` event.
- Finalizer logic must be *idempotent*. Deleting Jobs that don't exist is a noop, not an error.
- Document the manual unstick command in the runbook (`kubectl patch ... --type=merge -p '{"metadata":{"finalizers":null}}'`). Make sure operators can recover when the controller is genuinely down.
- The cleanup logic the finalizer runs is "delete child Jobs," not "git push the final state" — the latter is a level-boundary operation, not a deletion operation. Don't conflate.
- Test: kill the controller mid-deletion; verify the manual unstick works and is documented.

**Warning signs:**
- Projects stuck in `Terminating`
- Finalizer logic that calls external services
- Finalizer code without timeouts
- Issues filed about "can't delete my Project"

**Phase to address:**
Phase 1 (CRD lifecycle). Bake the deadline + idempotence rules into the first finalizer that ships.

---

### Pitfall 22: Dashboard observability leaks (websockets, logs, costs)

**Severity:** Annoying-to-Serious

**What goes wrong:**
The read-only dashboard streams `kubectl logs` from running task pods via websocket. A user opens the dashboard, walks away, closes the laptop. The websocket leaks; the orchestrator keeps the connection open and keeps streaming. Multiplied by N users and M tabs, the orchestrator OOMs. Or: dashboard streams *every* log line by default; long-running agentic chatter floods Loki / cluster logging at gigabytes per run.

**Why it happens:**
Real-time streaming is a great DX feature. Resource hygiene is an afterthought.

**How to avoid:**
- Websockets have idle-timeout + max-duration. Dashboard reconnects on demand; orchestrator drops idle streams.
- Log streaming is opt-in *per task*, not default-on-all-tasks. Default view shows status; click a task to start streaming its logs.
- Stream-rate-limit: cap bytes/second per connection. If a chatty agent exceeds the cap, the dashboard shows "log volume too high; download full log instead."
- Subagent harness applies its own per-Job log size cap. Anything over the cap is truncated; full log is on the PVC; the K8s-side log is bounded.
- Acceptance test: open N dashboard sessions, leave them idle 1 hour, verify orchestrator memory is bounded.

**Warning signs:**
- Orchestrator memory grows with active dashboard sessions, not with active runs
- Loki bills scale with run count linearly
- Connection leak metrics (`tide_dashboard_active_connections` not draining after users disconnect)

**Phase to address:**
Phase 4 (Dashboard). Resource discipline is part of the dashboard spec, not a post-launch fix.

---

### Pitfall 23: Missing or wrong owner references

**Severity:** Serious

**What goes wrong:**
A Wave creates Task CRDs. Tasks create Jobs. If owner references aren't set right:
- Deleting a Plan should garbage-collect its Tasks and their Jobs. Without owner refs: orphans linger forever, holding PVCs and consuming etcd.
- An owner-ref pointing to the wrong UID (because of recreate-after-delete) means the GC kicks in unexpectedly. Tasks vanish mid-run.
- Cross-namespace owner refs (a Plan in namespace A owning Tasks in namespace B) are *silently ignored* by K8s — owner refs must be in the same namespace. A bug here looks like "the GC doesn't work" but is actually "the owner ref is meaningless."

**Why it happens:**
Owner references are a small detail with large consequences. The K8s docs are clear but easy to misread.

**How to avoid:**
- Every CRD-creates-CRD operation goes through a `setOwnerReference(child, parent)` helper. The helper enforces same-namespace, blocks cross-namespace, panics on invalid input.
- The helper sets `BlockOwnerDeletion: true` for parents the orchestrator cares about (so cascade-delete is well-ordered).
- Test: create a Project, advance through a wave, delete the Project, verify all child resources (Phases, Plans, Tasks, Jobs, ConfigMaps) are GC'd.
- Test: delete a Plan mid-run; verify Tasks and Jobs underneath are GC'd; verify orphaned Pods don't linger.

**Warning signs:**
- Resources lingering after their parent is deleted
- "I deleted the Project but the Jobs are still running"
- Cross-namespace ownership relationships in CRD design
- Manual cleanup runbooks beyond the unstick-finalizer one

**Phase to address:**
Phase 1 (CRD scaffold). Helper exists before the first child resource is created.

---

### Pitfall 24: OSS adoption death by missing docs

**Severity:** Catastrophic (post-v1; the OSS bar)

**What goes wrong:**
v1 ships. Apache 2.0 license attached. Helm chart works. Three external users try it. Two give up because docs don't cover "how do I use my own LLM provider" or "how do I configure for GitLab" or "what happens if a wave fails halfway." The third files an issue that languishes because no one's reading it. The OSS-readiness check from `PROJECT.md` ("Apache 2.0 LICENSE, README/docs sufficient for an external operator to install + run a project end-to-end") was checked because *installation* works — but *operation* needs more.

**Why it happens:**
Code is fun; docs are work. The team that built TIDE knows everything; the docs assume that knowledge. The gap between "works on the dev's host" and "an external operator can install and run" is exactly documentation.

**How to avoid:**
- v1 docs minimum: install (Helm + kubectl + kind dev loop), Project authoring (CRD examples), provider configuration (Anthropic, with notes on adding others), git remote configuration (GitHub, GitLab, Gitea examples), failure recovery (manual finalizer unstick, run resume, wave retry), RBAC reference, troubleshooting.
- Acceptance test for the "external operator" bar: a contributor unfamiliar with the codebase follows the docs and runs a project end-to-end. Time them. If it takes >30 minutes from clone to first run, the docs need work.
- Examples directory with full Project CRD samples for at least three common scenarios.
- Don't ship undocumented flags or undocumented Project spec fields. Discover by `grep -r "spec\." docs/ | wc -l` matches `grep -r "spec\." apis/ | wc -l`.

**Warning signs:**
- The team's onboarding doc is more detailed than the public docs
- "How do I X" questions on issues that should have been one-line docs links
- Examples directory empty or stale
- The maintainers can't reproduce a user's reported issue because they don't know the user's setup

**Phase to address:**
Phase 5 (OSS readiness). Continuous, not a final-week sprint.

---

## Technical Debt Patterns

Shortcuts that look reasonable but degrade the design.

| Shortcut | Immediate Benefit | Long-term Cost | When Acceptable |
|----------|-------------------|----------------|-----------------|
| Cache wave schedule in `Status.Waves` | Faster Reconcile, fewer DAG re-computes | Stale schedules; bugs where stored state disagrees with artifacts; resumption uses cache instead of artifacts | Never |
| Single worker pool for planner+executor | One config flag, simpler code | Loses two-budget structural advantage from spec; planning starves execution or vice versa | Never |
| Sync-wait inside Reconcile for Job completion | Code reads top-to-bottom | Work queue blocks; status drift; controller can't interrupt long ops | Never |
| Anthropic SDK imports in orchestrator package | Concrete impl is legible | OSS posture broken; second-provider impl requires major refactor | Never (move to harness package) |
| `verbs=*` in RBAC for "now" | Unblocks a feature in 5 minutes | Security review rejects install; OSS adoption blocked | Never |
| Store full subagent stdout in CRD status | Easy debugging from `kubectl describe` | etcd object size limit (~1MB per object) breached; controller crashes parsing huge statuses | Never (use artifact PVC + log streaming) |
| `kubectl-style` ad-hoc CRD updates in scripts | Fast iteration during dev | Production users break on schema changes; conversion webhooks missing | Until first external user |
| One CRD version forever | No conversion webhook complexity | First breaking change forces wipe-and-reinstall | Until v1 ships; require version-bump discipline from v1.1 |
| Mock the LLM by always returning success | Tests run fast and pass | Failure-handling code paths get no coverage; production breaks on first real failure | Never; use programmable stub |
| Skip secret scanning on git push | One less integration | First credential leak in a public repo | Never |
| Read all artifacts into subagent context | Subagent has "full information" | Context bleed (Pitfall 7); cost explosion; prompt injection surface multiplies | Never (declared inputs only) |

---

## Integration Gotchas

| Integration | Common Mistake | Correct Approach |
|-------------|----------------|------------------|
| Anthropic API | Treating 429 as a hard failure (Job exits, Task fails) | Typed retryable-error exit code; controller re-dispatches with backoff against the rate-bucket |
| Anthropic API | Polling for completion with naive client backoff | Use SDK's built-in retry with respect for `Retry-After` headers; ensure base concurrency is rate-aware |
| GitHub / GitLab / Gitea | Hard-coding GitHub PR URL patterns in dashboard or CRD status | Abstract via `gitremote.Driver` interface; URLs are opaque strings from the driver |
| GitHub / GitLab / Gitea | Using a single long-lived PAT for all pushes | Short-lived per-Job tokens minted from a base credential; deploy key scoped to `tide/*` branches; never push to `main` |
| Git remote | `git push --force` from the orchestrator | Always `--force-with-lease`; never plain `--force` |
| K8s API server | Reading from informer cache when freshness matters | `apireader.Get(ctx, key, obj)` to bypass cache for critical pre-dispatch checks |
| K8s API server | Updating `.spec` and `.status` in the same call | Use the status subresource; spec updates and status updates are separate API calls |
| K8s API server | `client.Update` after a `client.Get` from cache | Server-side apply (SSA) with `FieldManager`; avoids stale-write conflicts |
| K8s Job | Relying on `backoffLimit` for LLM-rate-limit retries | Exit code → orchestrator dispatch retry; Job backoff is for genuinely fatal failures |
| K8s Job | Default `activeDeadlineSeconds` (none) | Always set; subagent harness honors a configurable timeout, Job enforces a hard cap |
| K8s Pod | Capturing all stdout/stderr into the K8s log stream | Harness applies size cap + redaction; full log on PVC; K8s log is bounded summary |
| K8s Secret | Mounting the Anthropic key as an env var in subagent pods | Mount in the harness sidecar/proxy; subagent receives a per-Job short-lived signed token |
| K8s PVC | Assuming `ReadWriteMany` is available everywhere | Document the requirement; OSS Helm chart asks for storage class with RWX; fall back to RWO with shared-volume topology if not |
| K8s CRD | Removing or renaming a field within a version | Never. Mark deprecated; remove only when bumping version; provide conversion |
| Helm | Assuming `helm upgrade` updates CRDs | Use a dedicated CRD subchart that explicitly handles updates; document upgrade order |
| OpenTelemetry | Tagging spans with task UIDs as a label | Use UIDs as resource attributes (high-cardinality OK on attributes); metrics labels stay low-cardinality |
| Prometheus | Per-task `task_id` label on counters | Aggregate by project/phase/plan; per-task is tracing territory |

---

## Performance Traps

| Trap | Symptoms | Prevention | When It Breaks |
|------|----------|------------|----------------|
| Reconcile reads full PVC contents on every Reconcile | Reconcile latency grows with run age | Read artifacts on demand from artifact metadata in CRD status (paths only) | Run with >20 artifacts |
| CRD `.status` accumulates per-iteration log lines | etcd object size approaches 1MB | Log lines go to harness log → Loki/PVC; status carries terminal state only | Single Task with >100 iterations |
| Watch event flood when wave size is large | Controller CPU spikes; reconcile queue grows | `MaxConcurrentReconciles` tuned per controller; informer resync period not aggressive | Wave size >50 |
| Subagent Jobs created simultaneously hit Job-controller throttle | Some Jobs start slowly; metrics show staggered start times | Dispatch in bounded batches; honor controller-runtime's rate limit on parallel creates | Wave size > the K8s Job-controller burst limit (typically 20-50) |
| Full DAG re-Kahn on every status update | Reconcile CPU dominated by Kahn re-runs | Kahn is O(V+E) and fast; don't cache, but *do* memoize within a single Reconcile invocation | DAG with >1000 tasks |
| Trace span attribute size for prompts/completions | OTel-collector OOMs; traces dropped | Store payloads as artifact refs; spans carry references | Per-task LLM exchange >100KB |
| Prometheus cardinality from per-task labels | Prometheus storage explodes; queries time out | Label discipline: project/phase/plan only on metrics; tasks are span attributes | >1000 tasks across all runs |
| etcd compaction lag from frequent CRD updates | Cluster API server latency rises | Update CRDs at level boundaries (Slack tides), not on every subagent iteration | Long-running phases with frequent status updates |

---

## Security Mistakes

| Mistake | Risk | Prevention |
|---------|------|------------|
| API keys in env vars of subagent pods | Subagent leaks key into artifact / log / git | Harness proxy + per-Job short-lived signed tokens |
| Unfiltered subagent output in K8s logs | Credentials leak into Loki / Splunk / Datadog | Redaction wrapper on subagent stdout/stderr; tested against known token patterns |
| Plain text credentials in CRD spec | Anyone with `get` on Project sees keys | `secretRef` only; never plain text |
| `cluster-admin` for the controller | Compromise of controller compromises cluster | RBAC kubebuilder markers; enumerated verbs and resources |
| Cross-namespace owner refs | Silent failures; weakened isolation | Helper enforces same-namespace |
| Subagent has read access to entire PVC | Context bleed; prompt injection propagation | Per-Job mounts of declared inputs only |
| No secret scanning on artifacts at git-push time | Credential leak into a public repo | gitleaks (or equivalent) at every push level boundary; push fails if pattern matches |
| Subagent has unrestricted egress | Subagent calls arbitrary external services | NetworkPolicy in the project namespace; egress allowed only to configured providers + the git remote |
| LLM provider key used directly by subagent | Single leaked key compromises the whole project | Per-Project keys; rotate on detected leak |
| Conversion webhook with TLS cert from manual config | Cert expiry breaks all API calls | cert-manager or equivalent automation; HA-deploy the webhook |
| Job uses default service account | Subagent pod can do whatever the default SA can | Explicit per-Job service account with minimal permissions (just artifact PVC access) |

---

## Self-Hosting / Dogfooding Pitfalls

| Pitfall | Risk | Prevention |
|---------|------|------------|
| Bootstrap milestone scope creep | M0 keeps growing; self-hosting bar slips | Explicit, narrowly-scoped M0 in the roadmap (Pitfall 12) |
| CRD schema drift between bootstrap-TIDE and self-hosted-TIDE | TIDE-in-cluster can't read artifacts authored by TIDE-on-host | Single v1alpha1 schema for the entire bootstrap-through-self-hosting window |
| Bootstrap-TIDE and self-hosted-TIDE diverge in dispatch logic | Self-hosted run produces different artifacts than the human did with GSD | Bootstrap-TIDE is just early-version TIDE-the-binary, not a separate codebase |
| Self-hosting milestone marked complete before a fresh kind+helm install actually drives a milestone | "Self-hosting works" but only on the dev's machine | Acceptance test: kind cluster from scratch + `helm install` + `kubectl apply -f project.yaml` + observe the run produce the expected artifacts |
| Concurrent TIDE-and-human commits on the same files | Lost human work; can't debug TIDE while TIDE runs | Per-run branches; `--force-with-lease`; never push to `main` (Pitfall 13) |
| Manual fixes to TIDE-authored artifacts get overwritten | Human review can't apply | Per-level slack-tide gate; orchestrator pauses on manual edit detection |

---

## "Looks Done But Isn't" Checklist

Things that pass a casual demo but are missing critical pieces.

- [ ] **Wave dispatch:** verify failed task → dependents *never* dispatched (not "eventually dispatched on retry"). Test with a 3-task wave where one fails.
- [ ] **Resumption:** kill the controller pod mid-wave; verify resume picks up at the right wave with no extra work.
- [ ] **CRD upgrade:** verify a v1alpha1 Task still works after the orchestrator binary is bumped (until a real v2 lands).
- [ ] **RBAC:** verify install in a non-cluster-admin namespace by a user with only the documented permissions.
- [ ] **Helm chart:** verify `helm upgrade` actually updates CRDs (or document the explicit upgrade path).
- [ ] **Cycle rejection:** submit a cyclic plan; verify rejection with a useful error pointing to the offending edges.
- [ ] **Provider swap:** verify `stub-subagent` impl runs the full test suite without an Anthropic key.
- [ ] **Git remote swap:** verify with GitLab and Gitea, not just GitHub.
- [ ] **Cost cap:** trigger a runaway loop in a Job; verify per-Task budget kills it; verify per-Project rolling-window gate fires.
- [ ] **Secret scanning:** plant a fake API key in an artifact; verify git-push level boundary fails the wave.
- [ ] **Owner refs:** delete a Project; verify all child Jobs, ConfigMaps, PVCs are GC'd within reasonable time.
- [ ] **Finalizer timeout:** simulate an external system being down during Project deletion; verify the finalizer eventually times out and unsticks.
- [ ] **Idempotent dispatch:** kill the controller during dispatch; verify only one Job exists per Task on restart.
- [ ] **Strict-by-default failure semantics:** a wave with one failure → siblings continue, dependents stop, non-dependents in next wave run. Verify all three.
- [ ] **Two-budget enforcement:** simulate a heavy planning wave; verify executor dispatches still happen on the executor pool, unblocked.
- [ ] **OSS install:** unfamiliar user follows the README; runs a project end-to-end in <30min.

---

## Recovery Strategies

| Pitfall | Recovery Cost | Recovery Steps |
|---------|---------------|----------------|
| Long-running reconcile blocks queue | LOW | Drain queue; refactor reconcile to be event-driven; restart |
| Cached wave schedule got stale | LOW (data) / HIGH (codebase) | Drop the cache field; redeploy; re-derive on next reconcile. Removing the field from the CRD requires a version bump |
| DAG/Execution unified | HIGH | Major refactor; split types; split controllers; redeploy CRDs |
| Status-as-truth resumption bug | MEDIUM | Restart controller; re-derive from artifacts; verify outputs against last-known-good git state |
| Cycle "recovery" feature shipped | MEDIUM | Revert; restore validation-time rejection; reach out to affected users to fix their cyclic plans |
| Unified worker pool | LOW (config) / MEDIUM (code) | Split flags; add two controllers; cap each |
| Subagent context bleed | MEDIUM | Add per-Job mount scoping; reject artifacts that violate declared inputs; replay affected runs |
| Runaway cost event | HIGH (financial) | Pause all projects via global flag; investigate which Project; add per-Project absolute cap; resume |
| Rate-limit cascade failure | LOW | Wait for backoff; restart paused tasks; tune rate-bucket config |
| Indegree update bug on partial failure | HIGH | Roll back to last-known-good controller version; verify in-flight runs; resume |
| Watch-lag duplicate dispatch | MEDIUM | Make dispatch idempotent (deterministic Job names); ride out the duplicate; reconcile fixes itself |
| Bootstrap deadlock (M0 slipping) | HIGH | Re-scope M0 down; freeze CRD schema; commit to hand-authoring M0 |
| TIDE overwrote human commits | HIGH | `git reflog` recovery; introduce per-run branches; never re-enable orchestrator pushes to `main` |
| Provider/host leak | MEDIUM | Identify leak via lint rule; refactor to interface; ship in next minor |
| RBAC scope creep installed in prod | LOW (technically) / HIGH (politically) | Tighter RBAC in next chart version; document the change as breaking |
| Breaking CRD change shipped | HIGH | Conversion webhook + emergency patch release; in worst case, document manual migration |
| Observability data volume spike | LOW | Tighten sampling; drop high-cardinality metric labels; redeploy |
| Secret leaked to git | CATASTROPHIC | Rotate the leaked credential immediately; force-rewrite git history (operator decision); add scanner to push pipeline |
| Hallucinated `depends_on` shipped | MEDIUM | Enable file-touch derived edge check in admission; reject offending plans |
| Test relying on real LLM credits | LOW | Ship `stub-subagent`; retag tests; gate live E2E to nightly |
| Finalizer leak | MEDIUM | Manual `kubectl patch ... finalizers:null`; add deadline to finalizer code |
| Dashboard websocket leak | LOW | Add idle timeout; redeploy; drain stuck connections |
| Wrong owner refs | MEDIUM | Manually clean up orphans; ship helper; re-verify GC |
| OSS docs failure | MEDIUM | Onboarding sprint; external-operator dry-run; iterate |

---

## Pitfall-to-Phase Mapping

How roadmap phases should address these. Phase numbers are illustrative — the roadmap derives the actual ordering.

| Pitfall | Severity | Prevention Phase | Verification |
|---------|----------|------------------|--------------|
| 1. Long-running reconcile | Catastrophic | Phase 1 (controller scaffold) | Reconcile p99 latency metric; lint rule for sleep/blocking in Reconcile |
| 2. Cached wave schedule | Catastrophic | Phase 2 (Kahn) | Wave-derivation function is pure; no schedule fields in CRDs |
| 3. DAG unification | Serious | Phase 1 (CRD schema) | Distinct planner/executor types in API package |
| 4. Status-as-truth | Catastrophic | Phase 1 (CRD schema) | Resumption test from cold start without `.status` |
| 5. Cycle recovery | Serious | Phase 2 (admission webhook) | Webhook test: cyclic plan rejected with edge list |
| 6. Unified worker pool | Serious | Phase 1 (controller scaffold) | Two `MaxConcurrentReconciles` flags wired separately |
| 7. Subagent context bleed | Catastrophic | Phase 2 (Subagent interface) | Harness rejects diff touching undeclared files |
| 8. Runaway cost | Catastrophic | Phase 2 (Subagent harness) | Per-Task budget caps; per-Project rolling-window gate |
| 9. Rate-limit handling | Serious | Phase 2 (Subagent harness + dispatch) | Token bucket exists; 429 → controller retry, not Job failure |
| 10. Indegree on partial failure | Serious | Phase 2 (Kahn) | Test: one-of-three wave fails; correct downstream dispatch |
| 11. Watch-lag duplicate dispatch | Serious | Phase 2 (subagent dispatch) | Deterministic Job names; AlreadyExists test |
| 12. Bootstrap deadlock | Catastrophic | Phase 0 (roadmap construction) | M0 and M_self named with bounded scope |
| 13. TIDE overwrites human commits | Serious | Phase 3 (git integration) | Per-run branch; --force-with-lease; never push main |
| 14. Provider/host leaks | Serious | Phase 2 (Subagent interface) | Custom lint rule; stub impl runs full tests |
| 15. RBAC scope creep | Serious | Phase 1 (controller scaffold) | Kubebuilder markers; no wildcards in generated manifests |
| 16. CRD breaking change | Catastrophic | Phase 1 (CRD schema) | Conversion webhook scaffold; alpha/beta/v1 naming |
| 17. Observability volume | Serious | Phase 4 (Observability) | Tail-sampling; bounded-cardinality labels |
| 18. Secret leakage | Catastrophic | Phase 2 + Phase 3 | Harness proxy; gitleaks at push; redaction tests |
| 19. Hallucinated deps | Serious | Phase 2 (Plan CRD admission) | File-touch derived edges reconciled vs declared |
| 20. Test cost / mock coverage | Annoying | Phase 2 (Subagent interface) | `stub-subagent` ships with interface; three test tiers in CI |
| 21. Finalizer leaks | Serious | Phase 1 (CRD lifecycle) | Finalizer-timeout test; idempotence verified |
| 22. Dashboard leaks | Annoying-Serious | Phase 4 (Dashboard) | Idle-session test; stream-rate cap |
| 23. Wrong owner refs | Serious | Phase 1 (CRD scaffold) | Helper enforces same-namespace; cascade-delete test |
| 24. OSS docs death | Catastrophic | Phase 5 (OSS readiness) | External-operator dry-run; <30min install-to-first-run |

---

## Sources

Verified against (HIGH = official docs / spec, MEDIUM = 2026 industry reporting):

- HIGH: [`README.md`](../../README.md) — TIDE spec (especially "Failure handling at wave boundaries", "Properties of the algorithm", "Alternatives considered and rejected")
- HIGH: [`PROJECT.md`](../PROJECT.md) — Locked v1 decisions and out-of-scope items
- HIGH: [`CLAUDE.md`](../../CLAUDE.md) — Implementation guidance, things explicitly NOT to do
- HIGH: [Kubernetes Finalizers](https://kubernetes.io/docs/concepts/overview/working-with-objects/finalizers/)
- HIGH: [Kubernetes CRD Versioning](https://kubernetes.io/docs/tasks/extend-kubernetes/custom-resources/custom-resource-definition-versioning/)
- HIGH: [Kubernetes Jobs](https://kubernetes.io/docs/concepts/workloads/controllers/job/)
- HIGH: [Kubernetes RBAC Good Practices](https://kubernetes.io/docs/concepts/security/rbac-good-practices/)
- HIGH: [Kubebuilder Book — Good Practices](https://book.kubebuilder.io/reference/good-practices)
- HIGH: [Kubebuilder Book — Using Finalizers](https://book.kubebuilder.io/reference/using-finalizers.html)
- HIGH: [controller-runtime FAQ](https://github.com/kubernetes-sigs/controller-runtime/blob/main/FAQ.md)
- HIGH: [Bootstrapping (compilers) — Wikipedia](https://en.wikipedia.org/wiki/Bootstrapping_(compilers))
- MEDIUM: [Shan Valleru: Eventual Consistency and Stale Caches in Kubernetes Controllers](https://svalle.ru/posts/kubernetes/stale-cache-controllers/)
- MEDIUM: [Shan Valleru: Leader Election in Kubernetes Controllers](https://svalle.ru/posts/kubernetes/leader-election/)
- MEDIUM: [OneUptime: How to Handle CRD Version Upgrades with Conversion Webhooks (2026)](https://oneuptime.com/blog/post/2026-02-09-crd-version-upgrades-conversion/view)
- MEDIUM: [OneUptime: How to Upgrade Kubernetes Operators and CRDs Safely (2026)](https://oneuptime.com/blog/post/2026-02-09-upgrade-operators-crds-safely/view)
- MEDIUM: [OneUptime: Operator Status Subresource (2026)](https://oneuptime.com/blog/post/2026-02-09-operator-status-subresource/view)
- MEDIUM: [OneUptime: AI Workload Observability Cost Crisis (2026)](https://oneuptime.com/blog/post/2026-04-01-ai-workload-observability-cost-crisis/view)
- MEDIUM: [OneUptime: Helm CRD Installation and Upgrades (2026)](https://oneuptime.com/blog/post/2026-01-17-helm-crd-installation-upgrades/view)
- MEDIUM: [Uptrace: OpenTelemetry for AI Systems — LLM and Agent Observability (2026)](https://uptrace.dev/blog/opentelemetry-ai-systems)
- MEDIUM: [RelayPlane: Agent Runaway Costs and LLM Budget Limits (2026)](https://relayplane.com/blog/agent-runaway-costs-2026)
- MEDIUM: [LangWatch: 4 best tools for monitoring LLM & agent applications (2026)](https://langwatch.ai/blog/4-best-tools-for-monitoring-llm-agentapplications-in-2026)
- MEDIUM: [LangWatch: Mocking External APIs in Agent Tests](https://langwatch.ai/scenario/testing-guides/mocks/)
- MEDIUM: [TrueFoundry: AI Cost Observability for LLM and Agent Workloads](https://www.truefoundry.com/blog/ai-cost-observability)
- MEDIUM: [TrueFoundry: Agentic Token Explosion — LLM Cost Attribution in CI/CD](https://www.truefoundry.com/blog/llm-cost-attribution-agentic-cicd)
- MEDIUM: [Doppler: Advanced LLM security — Preventing secret leakage across agents and prompts](https://www.doppler.com/blog/advanced-llm-security)
- MEDIUM: [GitGuardian via Help Net Security: 29 million leaked secrets in 2025 (2026)](https://www.helpnetsecurity.com/2026/04/14/gitguardian-ai-agents-credentials-leak/)
- MEDIUM: [Microsoft Security Blog: When prompts become shells — RCE in AI agent frameworks (May 2026)](https://www.microsoft.com/en-us/security/blog/2026/05/07/prompts-become-shells-rce-vulnerabilities-ai-agent-frameworks/)
- MEDIUM: [Penligent: AI Agents Hacking in 2026](https://www.penligent.ai/hackinglabs/ai-agents-hacking-in-2026-defending-the-new-execution-boundary/)
- MEDIUM: [arxiv: From Prompt Injections to Protocol Exploits — Threats in LLM-Powered AI Agent Workflows](https://arxiv.org/html/2506.23260v1)
- MEDIUM: [arxiv: ARGUS — Defending LLM Agents Against Context-Aware Prompt Injection](https://arxiv.org/abs/2605.03378v1)
- MEDIUM: [Markaicode: How to Scale LLM APIs on Kubernetes (2026)](https://markaicode.com/scaling-llm-api-kubernetes-guide/)
- MEDIUM: [Portainer: Kubernetes RBAC — Roles, Permissions & Best Practices (2026)](https://www.portainer.io/blog/kubernetes-rbac)
- MEDIUM: [Jorijn: Kubernetes namespace stuck in Terminating — finalizer holding it](https://jorijn.com/en/knowledge-base/kubernetes/troubleshooting/kubernetes-namespace-stuck-terminating/)
- MEDIUM: [LearnKube: Why etcd breaks at scale in Kubernetes](https://learnkube.com/etcd-breaks-at-scale)
- MEDIUM: [MachineLearningMastery: Handling Race Conditions in Multi-Agent Orchestration](https://machinelearningmastery.com/handling-race-conditions-in-multi-agent-orchestration/)
- MEDIUM: [git push race conditions discussion (git mailing list)](https://git.vger.kernel.narkive.com/9Rkrrepp/push-race-condition)

---
*Pitfalls research for: TIDE — Kubernetes-native hierarchical agentic coding orchestrator*
*Researched: 2026-05-12*
