# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Working Rules

Project-agnostic operating principles. Apply these before anything else, including before reading the rest of this file.

### 1. Observe First

Every wasted iteration in Phase 02.2 traced back to acting on assumptions instead of observations. The cascade-5 debug session hypothesized a "Wave Ready check" that didn't exist; the cascade-8 debug session hypothesized "ResourceVersion-conflict exponential-backoff suppression" when the actual cause was a never-assigned `Dispatcher` field. Both were avoidable.

1. When a runtime gate is BLOCKED → **read the manager log (`kubectl logs` or `/tmp/<plan>-clean-run.log`) and the VERIFICATION.md frontmatter BEFORE hypothesizing**
2. When a controller misbehaves → **`kubectl logs` + `kubectl describe <crd>` + `kubectl get <crd> -o yaml` BEFORE reading reconciler source**
3. When a debug hypothesis cites a specific code path → **grep the codebase to confirm it exists in the cited form** (`grep -nE "Wave.*Ready|Ready.*Wave" internal/controller/` would have refuted cascade-5's framing in seconds)
4. When a code change has no visible effect → **verify the binary rebuilt (image digest changed) and the Pod rotated (`kubectl get pods -n tide-system` shows new age)**, don't keep tweaking
5. When suggesting a Makefile target → **`grep -nE '^target-name:' Makefile`** to confirm it exists before recommending the command
6. When an agent could answer a question → **but `kubectl get`, `git log --grep`, or a single grep answers it faster, use the CLI**

Subagents are for understanding architecture and authoring artifacts. Runtime artifacts (manager logs, CRD status, git history) are for debugging runtime failures. Do not confuse the two.

### 2. Execute, Don't Ask

When the next step is obvious and non-destructive AND inside an active GSD workflow: do it. Don't present the command and ask for confirmation. This includes running `make test-int`, rerunning failing tests, creating planning artifacts, applying fixes specified in an approved `PLAN.md`, and committing per the executor's task protocol.

**Exceptions — always confirm or refuse:**

- **Production code edits outside an active GSD plan.** The `GSD Workflow Enforcement` section below is binding. Even if the fix is obvious, route through `/gsd:quick` or `/gsd:debug` so artifacts stay in sync.
- **Edits to `charts/tide/values.yaml`** — chart is FIXED contract per Phase 02.2's anti-pattern. Binary catches up to chart, never reverse.
- **`git push --force`** to any branch, especially `main`.
- **`git reset --hard`** outside a worktree's own HEAD-correction step.
- **`kind delete cluster`** outside an explicit clean-rerun sequence (it destroys all test state).
- **`rm -rf`** on `.git/`, `.planning/`, `.claude/`, or any directory containing uncommitted user work.
- **`gsd-sdk query state-set ...`** that downgrades phase status or rewrites tracked progress fields.

### 3. Verify Before Claiming

Never say "this should fix it," "the fix is complete," or "cascade-N is closed." Run the verification. Read the gate_decision. Grep for the artifact. Then state what you observed.

Concrete verification protocols for Phase 02.2-style runtime gates:

- **Plan landed:** `git log --oneline -1 -- <files_modified>` shows the expected commit on `main`
- **VERIFICATION.md exists:** `ls .planning/phases/<dir>/<plan>-VERIFICATION.md` returns a file
- **Gate APPROVED:** `grep -cE '^gate_decision: APPROVED$' <VERIFICATION>` returns exactly `1`
- **Test ran:** `tail -30 /tmp/<plan>-clean-run.log` shows Ginkgo summary line `Ran X Specs in Ys`
- **Dispatch is live (Phase 02.2-10 evidence shape):** `grep -E '"creating job"|"dispatch"' /tmp/<plan>-clean-run.log` returns ≥ 1 line, AND `kubectl get jobs --all-namespaces` shows Jobs in the test namespaces
- **Cascade closed:** the specific manager-log error that defined the cascade (e.g. `"no project found in namespace X"`) returns 0 in the new run's log
- **`make test-int` exit ≠ "Ginkgo green."** The `test/integration/kind` package bundles plain go-tests (e.g. the helm-template contract tests like `TestHelmDeploymentTemplateRendersManagerPodAnnotations`) alongside the Ginkgo specs. One RED go-test fails the package and trips `make test-int` non-zero **even when both layers print `Ran X of Y … SUCCESS!`**. Always read the echoed `MAKE_EXIT` AND `grep -nE '^--- FAIL|^FAIL\s'` the log — not just the Ginkgo summary. (Phase 7 nearly tagged v1.0 on "14/14 green" while `make test-int` was red on a dropped chart-template block.)
- **A subagent's "pre-existing / unrelated" dismissal of a failing test is a claim, not verification.** Before relaying it as safe, git-archaeology the regressing commit (`git log -S '<token>' -- <file>`) and confirm it does not fail the gate. (Two Phase-7 agents waved off the dropped `podAnnotations` render block as pre-existing; it was a real ship-blocker — the verifier caught it and a `make test-int` re-run proved it.)

## Operating Notes (Phase 02.2 lessons)

Rules learned from 10 BLOCKED iterations across Phase 02.2. Trust them.

- **Don't predict "chain terminator."** Plans 02.2-08/09/10 each predicted the end; all surfaced new cascades. Frame work as iterations; don't anchor on terminations.
- **Pre-empt worktree cwd-drift before merging.** Executor agents occasionally write artifacts to main's working tree instead of the worktree's. Pre-merge check:
  ```
  UNTRACKED=$(git status --porcelain | grep '^??' | grep "<plan>-" || true)
  ```
  If non-empty, diff against the worktree branch's commits; remove duplicates before `git merge`. (Plan 02.2-09 merge failed without this; Plan 02.2-10 succeeded with it.)
- **`gsd-sdk query state.begin-phase` resets STATE.md body fields.** It re-derives `Current Position` and `Status` from current plan inventory. Edit body fields AFTER `state.begin-phase` runs, not before, or the edit gets clobbered.
- **Question implicit framings when cascade classes diverge.** If 3+ consecutive cascades are the same class (e.g. cascades 2/3 both flag-mismatches), trust tactical iteration. If consecutive cascades differ in class (e.g. cascade-4 budget → cascade-5 harness-bug → cascade-6 timeouts), surface "is the underlying assumption still right?" before authoring the next plan. The cascade-8 debug session forced this question 7 plans late.
- **Use `--wave N` filter when a closeout plan is gated on a BLOCKED artifact.** Plan 02.2-02's `checkpoint:human-verify` reads `gate_decision: APPROVED` from a verification artifact; `--auto` mode would false-approve against a BLOCKED gate. Filter dispatch with `--wave <new-gap-wave>` until any plan records APPROVED and the rewire fires.
- **Subagent prompts: focus, don't restate.** Agents have their own system prompts that cover anti-patterns and protocols. `<additional_context>` blocks should carry decision-only context (the specific gap, the user's choice, the corrected hypothesis) — not the full cascade history or the entire anti-pattern table. Token budget directly affects subagent quality.
- **Honor `workflow.auto_advance: true` literally** — fire the chain across phase boundaries. But ASK FIRST at fix-shape decision points (Option A vs B at scope-defining moments). The user memory note covers this; respect it.
- **Decorative banners are noise unless they signal a real workflow transition.** `━━━ GSD ► STAGE ━━━` belongs at PLANNING / VERIFYING / EXECUTING boundaries (per `references/ui-brand.md`), not on every status update. Routine progress can be one-line.
- **End-of-turn summary: one or two sentences.** What changed and what's next. Resist the urge to recap multi-section status when the conversation thread is fresh.
- **Use TaskCreate for multi-cycle workflows.** A plan-check-commit-execute loop repeated 5+ times in one session genuinely benefits from task tracking. The system reminders about TaskCreate are not boilerplate.
- **STATE.md "Current Position" body text drifts.** It's prose, not derived; updates to it need to happen explicitly. The frontmatter (`progress.completed_plans`) is computed by SDK verbs; the body text is not.
- **Constrained-VM full-suite recipe (the dev Docker VM is ~7.65 GiB).** It DOES fit a clean `make test-int` if you don't accumulate state: delete → recreate → pre-warm (provisioner Ready + `kind load busybox:1.36`) a fresh kind cluster per heavy run, one heavy run at a time. Never let `make acceptance-v1-smoke` (it spins its OWN `tide-acceptance-<ts>` cluster) run while a `tide-test` cluster is still up — two single-node clusters OOM the node (exit 137). Phase 7's "env-gated, needs a bigger VM" deferral was actually solvable this way on the same 7.65 GiB host.
- **The acceptance `$0` path is a different deploy surface than Layer B — peel it one layer at a time.** Layer B fixtures carry a dummy `providerSecretRef` and use the test harness's helm install; the `$0` small-project has none and uses the chart defaults. So `$0` acceptance surfaces bugs Layer B masks (cascade-12 chart `default "latest"` image tags; cascade-13 credproxy hard-requiring `ANTHROPIC_API_KEY`). Expect each green layer to expose the next; don't predict the terminator.

## What this repository is

**TIDE** (Topologically-Indexed Dependency Execution) — a Kubernetes-native orchestrator for hierarchical agentic coding work, designed to be open-sourced and to run against any project/codebase in any cluster. `README.md` is the design spec (and doubles as the public-facing README); check `git log` and `.planning/` for current implementation state.

## The spec is load-bearing

Everything downstream — schemas, APIs, controller logic, persistence — should trace back to the paradigm doc. When designing or implementing, preserve these distinctions; they exist for reasons the doc argues for explicitly:

- **Five-level hierarchy**: Milestone → Phase → Plan → Task → Wave. Each level has its own artifact (`MILESTONE.md`, phase brief, `PLAN.md`, diff, execution schedule) and dependency model. Don't collapse levels or invent new ones; if a real implementation pressure pushes back on the hierarchy, update the spec first.
- **Two distinct DAGs**: the *Planning DAG* (which artifacts must exist before another can be authored — shallow, fans out wide) and the *Execution DAG* (which code must exist before another can be written — deeper, fans out narrow). Same Kahn-layered algorithm runs on both. APIs and CRDs should keep these typed apart, not unified into one "DAG" abstraction.
- **Waves are derived, not declared**. They are the output of layered Kahn on the task DAG. The orchestrator never accepts a wave list as input — only a DAG. Re-deriving on every plan edit is intentional (the spec calls this out: O(V+E), cheap, no stale-schedule caching).
- **Cycles are bugs, not runtime conditions**. Cycle detection happens at plan-validation time and a cyclic DAG refuses to run. Don't add "cycle recovery" features; reject and surface.
- **Failure semantics at wave boundaries** (spec §"Failure handling at wave boundaries") are specific: failed task → siblings in same wave continue (they were declared independent), dependents in later waves never dispatch, non-dependents in later waves dispatch in strict-by-default but halt in conservative profile. Keep this contract intact when implementing the executor.
- **Resumption state is minimal**: indegree map + completed-task set. If the persistence layer starts wanting to store the full schedule, that's a smell — re-derive instead.

## Vocabulary conventions

The water/tide metaphor is intentional and consistent — use it in code names, CRD names, log lines, and docs:

- Rising tide = planning wave fanning out across subagents
- Slack tide = review checkpoint between waves
- Tidal lock = phase whose dependencies have all resolved
- Tidepool = sub-DAG developed in parallel isolation
- TIDE pod = deployment unit running a TIDE orchestrator (the K8s pun is intentional and load-bearing)

Prefer extending the metaphor naturally over coining unrelated terms. If a name doesn't fit, prefer plain prose.

## Implementation guidance

- **Size planner and executor pools separately.** The spec argues planning fans out wide and execution fans out narrow — don't unify them into one worker pool.
- **Keep human-gate policy out of the controller.** Approve-every-milestone-but-auto-pass-plans must be as expressible as fully-autonomous or fully-supervised; the controller reads gate config, doesn't bake it in.

(Provider/host/auth abstraction, pluggable subagent runtime, artifacts-as-source-of-truth, and resumability rules live in the Project Constraints and Stack Anti-patterns sections — don't restate here.)

## Structural conventions in the spec document

When editing `README.md` (the spec) itself:

- Mermaid diagrams: nested `subgraph` containment for the planning graph; flat wave subgraphs with cross-wave edges for the execution graph. Match the existing style.
- Pseudocode uses Python-ish syntax with numbered-step comments (`# 1.`, `# 2.`).
- Worked examples follow pseudocode; the Kahn example walks the indegree map iteration-by-iteration. New algorithms get the same treatment.
- "Alternatives considered and rejected" is part of the doc's argumentative shape — when proposing a design choice, include the rejected alternatives, not just the winner.
- Voice is tight, declarative, em-dash-heavy. Match it rather than reverting to hedged corporate prose.

<!-- GSD:project-start source:PROJECT.md -->
## Project

**TIDE — Topologically-Indexed Dependency Execution**

A Kubernetes-native orchestrator that runs hierarchical agentic coding work as a topologically-sorted DAG of subagent dispatches. A human applies a `Project` CRD (outcome prompt + target repo + creds); TIDE authors `MILESTONE.md`, phase briefs, `PLAN.md` files, and task diffs by dispatching specialist subagents at each level, parallelizing across waves derived from the declared task DAG. Built to be open-sourced and portable across clusters from day one.

**Core Value:** **The five-level paradigm (Milestone → Phase → Plan → Task → Wave) runs as a real K8s orchestrator that can drive its own next milestone end-to-end.** If everything else fails, TIDE-on-TIDE must work — that's what proves the paradigm and the implementation simultaneously, and it's the bar for "v1 ships."

### Constraints

- **Tech stack**: Go + sigs.k8s.io/controller-runtime + kubebuilder — K8s ecosystem default, idiomatic for CRDs/controllers, best contributor pool.
- **Tech stack**: Pluggable subagent runtime via a documented container image contract — never hard-coded to Anthropic SDK; v1 ships with a Claude-backed concrete impl behind the interface.
- **Distribution**: Apache 2.0, Helm chart from v1, designed for installation in arbitrary clusters with no hidden host dependencies.
- **Portability**: No hard-coded git host (GitHub, GitLab, Gitea must all work behind a generic git remote), no hard-coded LLM provider, no hard-coded auth model — abstract behind interfaces.
- **Persistence**: CRD `.status` only for v1 — no external DB, no SQLite. Per-object size stays well under etcd's 1.5 MiB hard limit by keeping per-Task CRDs small and label-indexed.
- **Failure semantics**: Wave boundary contract from spec §"Failure handling at wave boundaries" must be preserved exactly — failed task → siblings continue, dependents in later waves never dispatch, non-dependents dispatch in strict profile. Resumption state = indegree map + completed-task set, nothing more.
- **Resumability**: Long-running agentic work outlives single context windows. Every level boundary is a saved artifact; a fresh orchestrator restart re-derives waves from the task DAG + completed-task set in O(V+E).
- **Observability**: OpenTelemetry tracing must use OpenInference conventions for LLM/agent spans so traces are queryable in standard AI observability tools (Phoenix, LangSmith, Arize) without bespoke instrumentation.
<!-- GSD:project-end -->

<!-- GSD:stack-start source:research/STACK.md -->
## Technology Stack

Pinned versions. See `research/STACK.md` for rationale, alternatives, and citations.

**Core:** Go 1.26 · controller-runtime v0.24.x · kubebuilder v4.14.0 · Kubernetes ≥ 1.33 (CEL CRD validation needs 1.29+) · Anthropic Go SDK v1.42.x · Claude Code CLI ≥ v2.1.139 · Helm 3 · Kustomize (kubebuilder-bundled).

**Supporting:** Ginkgo v2.28 + Gomega · envtest · kind v0.31 · logr v1.4 + zap v1.28 · prometheus/client_golang v1.23 · otel v1.43 (trace stable; metrics still v0.65 — keep metrics on client_golang) · OpenInference attribute names emitted on OTel spans (no Go SDK) · go-chi v5 · go-git v5.

**Dashboard:** React 18 + TypeScript · @xyflow/react v12 + dagre · Tailwind v4 · SSE (not WebSockets).

**Dev tools:** controller-gen · setup-envtest · golangci-lint (gosec, errcheck, staticcheck on) · helmify (Kustomize → Helm at release).

**Version coupling rules:**

- Never bump `k8s.io/*` independently — let controller-runtime's `go.mod` dictate.
- Pin Anthropic SDK to a minor (`v1.42.x`); it rev-bumps weekly with beta surfaces.
- OTel trace API is v1.x stable; metric/log APIs are v0.x — don't conflate the trains.
- Pin kind node images by `@sha256` in E2E scripts.

### Non-obvious choices to preserve

- **Layered Kahn in stdlib**, not Gonum or dominikbraun/graph. The spec's exposition is iteration-by-iteration; a graph library obscures it.
- **Native K8s Jobs**, not Argo or Tekton. The orchestrator owns the DAG; waves are derived, not declared as Workflow templates.
- **CRD `.status` only** — no external DB, no SQLite. Per-Task CRD stays small; the indegree map is in-process and rederivable.
- **OpenInference attribute names on OTel spans**, not the OTel GenAI semconv (still pre-stable in 2026). Phoenix/LangSmith/Arize consume OpenInference today.
- **CEL CRD validation (`x-kubernetes-validations`)**, not admission webhooks — except for cycle detection if CEL can't express all-paths.
- **zap behind logr**, not slog (~3× hot-path win for field-heavy reconcile logs).
- **chi router** as `manager.Runnable`, not gin/echo/fiber (fasthttp won't compose with controller-runtime's manager).
- **React Flow + DOM nodes**, not Cytoscape canvas. DOM lets every node be a real React component with live status.

### Anti-patterns

- Don't mount host `~/.claude/` into executor containers. Use `ANTHROPIC_API_KEY` env from a K8s Secret.
- Don't use Claude Code's OAuth flow headless (broken: claude-code#29983, #7100).
- Don't accept a wave list as CRD input — only a DAG. Don't cache the schedule in `.status` — rederive from the completed-task set.
- Don't hard-code one LLM provider, one git host, or one auth model in the controller. All Anthropic-specific code lives behind the `Subagent` interface in `internal/subagent/anthropic/`.
- Don't replace layered Kahn with CPM/HEFT. If pools become heterogeneous, add a wave-internal sub-scheduler behind Kahn.
- Don't add cycle "recovery" features. Refuse a cyclic DAG with a clear error.
- Don't vendor GSD Markdown. Re-implement planner/executor prompts as compiled-in Go templates.
- Default the chart's `ServiceMonitor` to `prometheus.enabled=false` to avoid CRD-not-found on plain clusters.
<!-- GSD:stack-end -->

<!-- GSD:conventions-start source:CONVENTIONS.md -->
## Conventions

Conventions not yet established. Will populate as patterns emerge during development.
<!-- GSD:conventions-end -->

<!-- GSD:architecture-start source:ARCHITECTURE.md -->
## Architecture

Architecture not yet mapped. Follow existing patterns found in the codebase.
<!-- GSD:architecture-end -->

<!-- GSD:workflow-start source:GSD defaults -->
## GSD Workflow Enforcement

Before using Edit, Write, or other file-changing tools, start work through a GSD command so planning artifacts and execution context stay in sync.

Use these entry points:
- `/gsd:quick` for small fixes, doc updates, and ad-hoc tasks
- `/gsd:debug` for investigation and bug fixing
- `/gsd:execute-phase` for planned phase work

Do not make direct repo edits outside a GSD workflow unless the user explicitly asks to bypass it.
<!-- GSD:workflow-end -->

<!-- GSD:profile-start -->
## Developer Profile

> Profile not yet configured. Run `/gsd:profile-user` to generate your developer profile.
> This section is managed by `generate-claude-profile` -- do not edit manually.
<!-- GSD:profile-end -->
