## Quickstart

$0 LLM cost — tests the dispatch path end-to-end via the stub-subagent. Detailed install steps + per-OS prerequisites are in [docs/INSTALL.md](docs/INSTALL.md). For a **real-Claude run against your own repo, do NOT start here** — read the [production checklist](docs/production.md) first.

```bash
kind create cluster --name tide-demo

# cert-manager is required first — the tide chart's webhook + metrics Certificates need it.
kubectl apply -f https://github.com/cert-manager/cert-manager/releases/download/v1.20.2/cert-manager.yaml
kubectl -n cert-manager rollout status deploy/cert-manager-webhook --timeout=120s

helm install tide-crds oci://ghcr.io/jsquirrelz/tide-charts/tide-crds --version 1.0.0 -n tide-system --create-namespace
helm install tide oci://ghcr.io/jsquirrelz/tide-charts/tide --version 1.0.0 -n tide-system

# Apply the $0 stub sample (raw URL — the OCI install path doesn't clone the repo):
kubectl apply -f https://raw.githubusercontent.com/jsquirrelz/tide/v1.0.0/examples/projects/small/project.yaml

# Mirror the cluster-unique signing key into the sample namespace (the chart
# generates it in tide-system; dispatch Job pods read it from their own namespace):
SIGNING_KEY=$(kubectl get secret tide-signing-key -n tide-system -o jsonpath='{.data.TIDE_SIGNING_KEY}')
kubectl apply -f - <<EOF
apiVersion: v1
kind: Secret
metadata: { name: tide-signing-key, namespace: tide-sample-small }
type: Opaque
data: { TIDE_SIGNING_KEY: ${SIGNING_KEY} }
EOF

# Watch it run to Complete (~2 min):
kubectl wait --for=jsonpath='{.status.phase}'=Complete project/small-project -n tide-sample-small --timeout=10m
```

```text
# Expected output (abbreviated)
customresourcedefinition.apiextensions.k8s.io/projects.tideproject.k8s created
NAME: tide-crds   STATUS: deployed
NAME: tide        STATUS: deployed
project.tideproject.k8s/small-project created
secret/tide-signing-key created
project.tideproject.k8s/small-project condition met
```

> **First time?** Skip to [docs/INSTALL.md](docs/INSTALL.md) for the 4-command install with full prerequisites and troubleshooting.

---

 # TIDE — Topologically-Indexed Dependency Execution
  The Milestone → Phase → Plan → Task & Wave paradigm for autonomous coding agents

  The acronym

  T — Topologically. All ordering is produced by a topological sort of a declared task DAG (Kahn's algorithm in layered form — see "Wave computation" below). The math operation is the source of truth, not human prose.

  I — Indexed. The layered sort produces a stable, queryable index: given any task, you know its wave; given any wave, you know its tasks. This is what makes parallel dispatch, resumption, and incremental re-planning tractable.

  D — Dependency. All edges are declared explicitly in the plan. Ordering is never inferred from prose, filesystem layout, or convention. Cycles are rejected at plan-validation time; they are bugs, not runtime conditions.

  E — Execution. The runtime concern. TIDE structures the planning → execution handoff. It is a paradigm for *running* hierarchical agentic work, not a substitute for design or specification upstream of it.

  The water metaphor extends without strain into the rest of the vocabulary:

  - Rising tide: a planning wave fanning out across subagents
  - Slack tide: a review checkpoint between waves
  - Tidal lock: a phase whose dependencies have all resolved
  - Tidepool: a sub-DAG developed in parallel isolation
  - TIDE pod: a deployment unit running a TIDE orchestrator (yes, the K8s pun is intentional and load-bearing)

  Generic definition

  A five-level decomposition that turns "build this system" into a structured graph of work an autonomous agent (or pool of agents) can traverse.

| Level | Unit | Purpose | Artifact produced | Dependency model |
|-------|------|---------|-------------------|------------------|
| 1 | **Milestone** | An outcome-bearing capability set. The largest unit a human stakeholder cares about. | `MILESTONE.md` — outcome statement, exit criteria, upstream/downstream milestones | DAG across milestones (not linear) |
| 2 | **Phase** | A coherent slice within a milestone that delivers something incrementally testable. | Phase brief — one paragraph of intent + dependency declarations | DAG within (and across) milestones, expressed at the interface level |
| 3 | **Plan** | The file/line-level specification of one slice of a phase's work. A phase can have multiple plans; plans within a phase may execute in parallel or serially depending on their task dependencies. | `PLAN.md` — file paths, type signatures, test cases, acceptance checklist | Consumes its phase's brief + upstream phases' interface contracts. Sibling plans within the same phase declare any interface dependencies they have on each other. |
| 4 | **Task** | The atomic unit of code mutation. Small enough to finish in one pass; large enough to be meaningful. | A diff, a test, a file. | DAG within the plan; declares the files it touches |
| 5 | **Wave** | A horizontal grouping of tasks with no mutual dependencies. Tasks within a wave execute in parallel; waves execute sequentially. | An execution schedule | Derived from the task DAG by topological layering |

  Two distinct DAGs run through this:

  - Planning DAG — which artifacts must exist before another artifact can be written. Usually shallow; most phases can be planned the moment the architecture spec is locked, and the plans within a phase are usually drafted in parallel.
  - Execution DAG — which code must exist/run before another piece of code can be written/run. Usually deeper; reflects real interface dependencies. Whether a single plan's tasks fan out into one wave or serialize across several is a property of that plan's task DAG, not of the plan itself.

  The wave model only matters for the execution DAG. Planning is almost always fan-out-able to the breadth of subagent capacity.

  A critical boundary: `Milestone.dependsOn` is a **planning-DAG edge only** — it governs planning order and gate-descent, and contributes zero execution edges. Cross-milestone execution coupling is expressed exclusively via task-level (or plan/phase-level) `dependsOn` that crosses milestone boundaries. This is why ζ (in Milestone B) is free in execution Wave 1 even when Milestone B's planning depends on Milestone A — no execution edge is implied by the Milestone-level dependency.

  Abstract visualization

  Planning graph — nested containment of Milestones → Phases → Plans → Tasks, with DAG edges where dependencies exist:

  ![TIDE planning DAG — nested Milestone → Phase → Plan containment for the SPEC-01 conformance fixture, rendered left-to-right by the dashboard PlanningDAGView](docs/screenshots/planning-dag.png)

  *A real dashboard render (`PlanningDAGView`) of the SPEC-01 conformance fixture — Project `spec-conformance-project` → Milestones `ms-spec-a` / `ms-spec-b` → Phases → Plans. `ms-spec-b` declares `dependsOn: [ms-spec-a]` (a planning-DAG edge only). Generated with TIDE v1.0.2 from the executable fixture in `test/integration/envtest/spec_conformance_test.go`, so this picture and the implementation cannot drift.*

  Execution graph — task waves derived from the task DAG. Tasks within a wave run in parallel; waves run sequentially:

  ![TIDE execution DAG — global wave schedule {α,β,γ,ζ} → {δ,η} → {ε,θ} with the cross-milestone γ→η edge, rendered left-to-right by the dashboard GlobalExecutionDAGView](docs/screenshots/execution-dag.png)

  *A real dashboard render (`GlobalExecutionDAGView`) of the same SPEC-01 fixture, derived by the global wave engine across milestone boundaries. The dashboard labels waves 0-indexed, so its `WAVE 0 / 1 / 2` correspond to `Wave 1 / 2 / 3` in the walkthrough below; the schedule `[{α,β,γ,ζ}, {δ,η}, {ε,θ}]` is identical. ζ (Milestone B) is free in the first wave — `Milestone.dependsOn` adds no execution edge — and the cross-milestone edge γ→η is honored (η waits for γ). Generated with TIDE v1.0.2 from `test/integration/envtest/spec_conformance_test.go`.*

  Cross-reference between the two graphs: Plan A.1.1 (tasks α, β) runs fully in Wave 1 — its tasks are independent, so the plan parallelizes. Plan A.2.1 (tasks δ, ε) and Plan B.1.2 (tasks η, θ) each have tasks split across waves — those plans serialize internally because their task DAGs declare ordering. Same paradigm, different per-plan parallelism, all expressed by the task DAG rather than the plan boundary.

  Wave computation — the topological sort

  The mechanism that converts a task DAG into a list of waves is *Kahn's algorithm in layered form*. The algorithm itself is textbook; the *layered* variant is the property TIDE relies on.

  Inputs:

  - Tasks (nodes): the leaves of the planning hierarchy — atomic units of code mutation declared in PLAN.md files
  - Dependencies (edges): "task u must complete before task v" — derived from declared file-touch sets, declared interface contracts, or explicit `depends_on` declarations in the PLAN

  Output:

  - An ordered list of waves: [W₁, W₂, ..., Wₙ]
  - Each Wₖ is the set of tasks whose upstream dependencies are all satisfied once W₁..Wₖ₋₁ have completed
  - Equivalently: Wₖ contains every task whose longest dependency chain from a root is exactly k

  Pseudocode:

  ```
  function computeWaves(tasks, edges):
      # 1. Count incoming edges per task
      indegree = {t: 0 for t in tasks}
      for (u, v) in edges:
          indegree[v] += 1

      waves = []
      remaining = set(tasks)

      # 2. Peel off zero-indegree layers until empty
      while remaining:
          current = {t for t in remaining if indegree[t] == 0}
          if not current:
              raise CycleError("declared task DAG contains a cycle")
          waves.append(current)
          for t in current:
              remaining.remove(t)
              for v in successors_of(t):
                  indegree[v] -= 1

      return waves
  ```

  Worked example — the tasks from the execution graph above:

  ```
  Tasks:  α, β, γ, δ, ε, ζ, η, θ
  Edges:  α→δ, β→δ, γ→η, ζ→η, δ→ε, η→θ

  Initial indegree:  α=0, β=0, γ=0, ζ=0, δ=2, η=2, ε=1, θ=1

  Iteration 1:  zero-indegree set = {α, β, γ, ζ}    →  Wave 1
                after decrement:    δ=0, η=0, ε=1, θ=1
  Iteration 2:  zero-indegree set = {δ, η}          →  Wave 2
                after decrement:    ε=0, θ=0
  Iteration 3:  zero-indegree set = {ε, θ}          →  Wave 3
                after decrement:    (empty)

  Done.  Schedule = [{α,β,γ,ζ}, {δ,η}, {ε,θ}]
  ```

  This is exactly the wave structure rendered in the execution graph above. The topological sort *is* the wave schedule — no further transformation, no separate scheduler.

  Properties of the algorithm:

  1. Time complexity: O(V + E). Cheap enough to recompute after every plan edit. There is no reason to cache a stale schedule.
  2. Wave count is optimal: the number of waves equals the length of the longest path in the DAG. You cannot execute in fewer waves without violating a declared dependency, so layered Kahn is wall-clock-optimal under the constraint that subagent capacity is unlimited.
  3. Maximum concurrency in wave k: |Wₖ| tasks. The orchestrator's actual dispatch is min(|Wₖ|, subagent_capacity). If capacity is the binding constraint, the wave splits into sub-batches that still complete before Wₖ₊₁ dispatches — concurrency degrades gracefully without changing correctness.
  4. Cycle detection is free. If the loop exits with `remaining ≠ ∅`, the DAG contains a cycle. TIDE refuses to start a run on a cyclic DAG — cycles are bugs in the declared plan, not runtime conditions to recover from.
  5. Monotonic under DAG edits. Adding an edge can only push a task *later*, never earlier. Adding a node lands it in the earliest wave consistent with its declared edges. Incremental re-planning after a plan revision is well-behaved and predictable.

  Two-DAG application:

  TIDE runs the same algorithm twice in a project's lifecycle:

  - Planning DAG: nodes are *artifacts to be authored* (MILESTONE.md, phase briefs, PLAN.md files). Edges are "this artifact's authoring requires another artifact's interface to be locked." Usually shallow; most phases plan in parallel once the architecture spec exists. Output: schedule for dispatching planner subagents. Milestone.dependsOn entries are planning-DAG edges — they sequence artifact authoring and gate-descent, not code execution.
  - Execution DAG: nodes are *code mutations to be made* (tasks). Edges are "this code change requires another to be merged first." Usually deeper; reflects real interface dependencies. Output: schedule for dispatching executor subagents. Only task-level (or plan/phase-level) DependsOn contributes execution edges — Milestone-level DependsOn contributes zero execution edges.

  Same algorithm, same properties, different inputs. The orchestrator's wave-walking logic is identical for both phases of the project.

  Failure handling at wave boundaries:

  - A task fails inside wave k → the wave's join barrier surfaces the failure. Sibling tasks in wave k continue; they were dispatched in parallel because they were declared independent, so aborting them is wasteful.
  - Tasks in wave k+1 that depend on the failed task → never dispatched. Their indegree never reaches zero.
  - Tasks in wave k+1 that *don't* depend on the failed task → dispatched normally in strict-by-default profile; halted in conservative profile. Configurable.
  - Resumption picks up at the same wave once the failure cause is addressed. State to persist is small: the indegree map + the completed-task set.

  Why this specific algorithm:

  - Layered Kahn produces minimum-depth waves, which minimizes wall-clock execution time under the unlimited-capacity assumption.
  - Output is exactly what a parallel-dispatch orchestrator needs — no transformation, no second-tier scheduler.
  - Cycle detection falls out of the algorithm's termination condition. No separate validator needed.
  - Monotonic and incremental under DAG edits — re-planning is well-behaved.
  - Trivially explainable to a human reviewer: "remove the things with no remaining dependencies, repeat."

  Alternatives considered and rejected:

  - DFS-based topological sort: produces a single linear ordering. Loses the wave structure; useless for parallelism.
  - Critical-path scheduling (CPM / PERT): optimizes for wall-clock time when task durations are known. LLM agent task durations are high-variance; the duration estimates CPM needs aren't trustworthy. In the unknown-duration case, CPM degenerates to Kahn-layered anyway.
  - Heterogeneous-resource schedulers (HEFT and relatives): optimize when worker pools differ in capability. Premature at the paradigm layer. If subagent pools become heterogeneous (Opus for hard tasks, Haiku for mechanical), TIDE adds a wave-internal sub-scheduler rather than replacing Kahn-layered at the wave level.

  Why this is advantageous for autonomous coding agents

  1. Context-window economics. LLM agents have bounded context. A flat "build this whole system" prompt either overflows or omits critical state. The hierarchy gives each level a minimal-sufficient context: a milestone planner needs the
  architecture spec; a phase planner needs one milestone's brief plus upstream interface contracts; a task executor needs one plan's diff target. Each level reads exactly what it needs and nothing more.

  2. Concurrency is expressed, not inferred. The wave model makes parallelism a first-class structural property. An orchestrator can dispatch N subagents against Wave K, wait for the join, then dispatch Wave K+1. No global lock analysis, no
  speculative scheduling — the DAG already declared what's safe to run together.

  3. Two distinct parallelism budgets. Planning DAGs fan out wide because plans don't write code yet — most phases can be planned from the architecture spec alone. Execution DAGs fan out narrow because real file-level dependencies serialize work.
  Separating the two lets you spend your subagent budget where it pays off: tons of parallel planners, fewer parallel executors.

  4. Resumability across context boundaries. Long-running agentic work routinely outlives a single context window. When a session is compacted or interrupted, the hierarchy provides natural resumption points: every level boundary is a saved
  artifact. A new session reads the milestone doc, the relevant phase plan, the task list with completion state, and picks up at the next undone wave. No re-derivation, no lossy summarization of state.

  5. Failure isolation by design. A failed task fails its wave, not its plan. A failed plan fails its phase, not its milestone. Recovery is granular: re-run the task, re-plan the plan, re-scope the phase. Without the hierarchy, a failure anywhere
  is a failure everywhere — the agent has to either retry the world or human-escalate.

  6. Specialized sub-agents per level. Different levels have different cognitive shapes. Milestone reasoning is architectural and goal-backward. Phase planning is interface-design and dependency-aware. Plan writing is detail-oriented and
  codebase-grounded. Task execution is mechanical and test-driven. You can configure different model sizes, system prompts, tool allowances, and review gates per level — Opus for milestone synthesis, Haiku for mechanical task execution.

  7. Auditability and human-in-the-loop gates. Each level produces a reviewable artifact (milestone doc → phase brief → PLAN.md → diff). Humans gate as much or as little as they want: approve every milestone but auto-pass plans, or approve every
  plan but auto-pass tasks. The same paradigm scales from "fully supervised" to "fully autonomous" without restructuring.

  8. Dependency clarity prevents implicit ordering bugs. When an agent infers ordering from prose ("first do X, then Y"), it routinely violates ordering it didn't read carefully. When ordering is a declared DAG with file-touch sets per task,
  conflicts are detectable mechanically — at plan time, not at runtime.

  9. Scale-invariance. The same five-level shape works for a 10-task feature or a 1000-task system. The tree gets deeper, but the dispatch logic, review gates, and resumption protocol are identical. An orchestrator written against this paradigm
  doesn't need to know how big the work is.

  10. Compatible with human reasoning. Engineers already think in this hierarchy when they plan large work — they just don't usually write it down formally. Making it explicit means the artifacts an agent produces are reviewable and editable by
  humans without translation, which is what keeps the human/agent feedback loop tight.

  The pattern's net effect: turns "agent stares at a 10,000-line architecture spec and tries to build it" into "orchestrator walks a DAG, dispatching the right specialist agent at the right level with exactly the context that level needs,
  parallelizing wherever the DAG permits, checkpointing at every level boundary."
