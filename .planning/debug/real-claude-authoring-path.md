
### 3. Real claude-subagent ships no `project_planner.tmpl` — FIXED (uncommitted; tests green, live-verified)

After fix #2 the planner gets PAST param validation and into the anthropic
runner's prompt-render step (step 3), then dies with:
`anthropic subagent: load prompt template (role="planner", level="project"):
common: load prompt template "templates/project_planner.tmpl": template:
pattern matches no files`.

- **Producer:** `ProjectReconciler.reconcilePlannerDispatch` dispatches a
  planner Job at `level="project"` (`internal/controller/project_controller.go:710`,
  `BuildPlannerEnvelope("project", project, project, …)`). A Project's planner
  authors the Milestone (Project → Milestone), mirroring the stub's
  `case "project"` at `cmd/stub-subagent/main.go:235` which emits a Milestone
  ChildCRD.
- **Consumer (real):** `internal/subagent/common/prompt_templates.go:59` resolves
  `level="project"` → `templates/project_planner.tmpl`. The `go:embed
  templates/*.tmpl` set shipped only FOUR templates — no `project_planner.tmpl`.
- **Contract conflict:** the orchestrator dispatches FIVE planner/executor levels
  (project, milestone, phase, plan, task) but the real subagent's embedded
  template set covered only FOUR.

**Resolution (fix-shape option a, per user decision 2026-06-08):** Added
`internal/subagent/common/templates/project_planner.tmpl` — a project-level
planner prompt that authors exactly ONE Milestone child-CRD from the project
outcome prompt, modeled on `milestone_planner.tmpl`/`phase_planner.tmpl` idioms
and the stub's `case "project"` project→Milestone contract. Levels UNCHANGED
(project→milestone→phase→plan→task; no collapse, no alias). Updated the loader
doc comment in `prompt_templates.go` (now enumerates five templates) and the
template-coverage test `TestLoadPromptTemplate_HappyPath` (added
`{planner, project}` row; "four"→"five"). `go test
./internal/subagent/common/... ./internal/subagent/anthropic/...
./cmd/claude-subagent/...` all green.

**Live-verified (2026-06-08):** rebuilt claude-subagent image (host
`sha256:5236aa8f275c…`), deleted leftover Errored planner pod (released the
stale image), `docker rmi -f` the stale `63296e8c1422` in minikube, reloaded
the fresh tag, re-applied medium-project. New planner pod
`tide-project-e6431d61-…` ran the **real Claude (Haiku) agent loop** and
**exited 0** with real token usage (inputTokens=25171, outputTokens=3801,
cacheRead=70422). The `template: pattern matches no files` error returns **0**.
Reaching a real LLM call is the first time the real authoring path has ever
executed end-to-end through prompt render. **Note:** `estimatedCostCents` came
back 0 in the envelope despite real token usage (see defect #6) — the run did
bill real tokens at the proxy, the cost-accounting in the envelope is a
separate downstream defect.

### 4. Project outcome prompt never threaded into `EnvelopeIn.Prompt` — OPEN (NEW FIX-SHAPE FORK)

The real planner ran but Claude reported the prompt was EMPTY
(`"prompt": ""`) and emitted an `EMPTY_PROMPT` warning, authoring only a
generic placeholder milestone. The stub masked this — it emits canned
children and never reads `.Prompt`.

- **Producer:** `BuildPlannerEnvelope` (`internal/controller/dispatch_helpers.go:158`)
  constructs `EnvelopeIn` with **no `Prompt:` field assignment at all** (lines
  159-169). The project reconciler additionally passes the prompt arg as the
  empty string literal `""` (`project_controller.go:710`,
  `BuildPlannerEnvelope("project", project, project, attempt, "", …)`) — and
  `BuildPlannerEnvelope`'s signature doesn't even have a prompt parameter; the
  `""` is the `token` arg. So `EnvelopeIn.Prompt` is ALWAYS empty at every
  level, not just project.
- **Source of truth:** `Project.Spec.OutcomePrompt` (CRD field
  `spec.outcomePrompt`) IS populated on the medium CR. It needs to flow into
  `EnvelopeIn.Prompt` for the project planner (and an appropriately-scoped
  prompt at milestone/phase/plan/task levels too).

**Fix options (design call):**
- (a) Thread `project.Spec.OutcomePrompt` into `EnvelopeIn.Prompt` inside
  `BuildPlannerEnvelope` (it already takes `project *Project`). Narrowest fix
  for the project level; milestone/phase/plan planners would inherit the same
  outcome prompt unless level-specific prompt assembly is added.
- (b) Add a `prompt string` parameter to `BuildPlannerEnvelope` and have each
  reconciler pass the level-appropriate prompt (project→outcomePrompt;
  milestone→outcome + MILESTONE.md context; etc.). Cleaner level semantics,
  touches all four planner-dispatch call sites.
- (c) Assemble the prompt entirely inside `BuildPlannerEnvelope` from
  `project.Spec.OutcomePrompt` + level + parent context. Most encapsulated.

NOTE: this is coupled to defect #5 — even with a real prompt, the controller
won't get a Milestone unless the runner returns ChildCRDs.

### 5. Anthropic runner never parses `ChildCRDs` from Claude's output — OPEN (NEW FIX-SHAPE FORK)

The planner exited 0, but `kubectl get milestone -n tide-sample-medium` shows
**no Milestone materialized**; the Project is stuck `Running`
(AuthoringPlanner=True). Claude DID emit a Milestone CRD — as fenced ```json
inside the `result` TEXT — but `EnvelopeOut.ChildCRDs` is empty.

- **Consumer (controller):** `handleProjectJobCompletion`
  (`project_controller.go:783`) reads `EnvelopeOut` and only materializes when
  `len(envOut.ChildCRDs) > 0` (line 813). The path is correct and waiting.
- **Producer (real runner):** `Anthropic.Run`
  (`internal/subagent/anthropic/subagent.go:243-263`) assembles `EnvelopeOut`
  with `Result`, `Usage`, `ExitCode`, `Reason` — but **never sets `ChildCRDs`**.
  It does not parse the model's structured output into typed `ChildCRDSpec`
  values. The stub masks this because the stub builds `ChildCRDs` directly in
  Go (`dispatchPlannerSuccess`) and never calls an LLM.
- **Contract conflict:** the controller's planner→child materialization
  contract requires `EnvelopeOut.ChildCRDs`; the real runner produces only
  free-text `Result`. Nothing extracts the model's emitted CRD JSON into the
  envelope.

**Fix options (design call):**
- (a) Have the project_planner.tmpl instruct Claude to write the Milestone CRD
  to a known PVC path (e.g. via a tool/Write), and have the runner read that
  file into `ChildCRDs` — file-handoff contract. Robust, but needs a
  tool-write convention + path validation.
- (b) Parse a structured block out of the model's final `result` text (e.g. a
  fenced ```json envelope or a sentinel-delimited region) into `ChildCRDs`
  inside `Anthropic.Run` / `ParseStream`. No tool dependency, but brittle to
  model formatting drift.
- (c) Use the claude CLI's structured-output / tool-call mechanism to emit
  typed child-CRD specs the runner deserializes directly. Most robust, largest
  change to the runner + template + possibly the CLI invocation flags.

This is the deepest unverified link — it gates whether ANY real run can
materialize a child and proceed. Likely repeats at milestone/phase/plan
(every planner level needs ChildCRDs) and at task (executor needs artifacts).

### 6. `estimatedCostCents` = 0 despite real token usage — OPEN (lower priority; likely accounting)

The exit-0 envelope reported `inputTokens=25171, outputTokens=3801,
cacheRead=70422, cacheCreation=16626` but `estimatedCostCents: 0`. The DoD
requires `costSpentCents > 0`. Either the per-model price table isn't wired for
Haiku, or the cost is computed elsewhere (controller-side from usage) and not
in the envelope. Investigate after #4/#5 unblock a full run — defer until a
Milestone materializes.

### 3+. Downstream (UNVERIFIED — expect more)

Now that the real anthropic `Run()` executes the agent loop and exits 0, the
remaining unverified links are: ChildCRDs extraction (#5), prompt threading
(#4), cost accounting (#6), then — once a Milestone exists — milestone/phase/
plan planner dispatches (same #4/#5 shapes recur per level), task executor
pods, the per-run `tide/run-medium-project-*` branch push to the in-cluster
http:// remote, and per-level model resolution. **Treat each as unverified
until a real run reaches `Complete`.**
