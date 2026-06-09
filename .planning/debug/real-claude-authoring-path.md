---
slug: real-claude-authoring-path
status: root-cause-locked-routed-to-phase
trigger: "The medium ($5 real-Claude) sample never completes a real end-to-end authoring run: defects #1–#10 were fixed iteratively, but #11 (4 KB termination-message cap) and #12 (the Manager's PVC and the dispatched pod's PVC are different namespace-local volumes) revealed that the Manager-reads-PVC envelope-return premise is architecturally wrong cross-namespace. Root cause locked; fix routed to a dedicated phase, not the debug loop."
created: 2026-06-08
updated: 2026-06-09
phase: 08-medium-sample-http-transport-and-production-git-transport-po
resolution: routed-to-phase  # NOT resolved — DoD (real end-to-end Complete + pushed run branch + costSpentCents>0) is NOT met
---

# Debug: real-claude-authoring-path

## Status: PAUSED — root cause locked, routed to a dedicated phase

This session is **parked, not resolved.** The Definition of Done — a real
Claude-backed run that reaches Project `Complete`, pushes a per-run
`tide/run-medium-project-*` branch to the in-cluster `http://` remote, and
surfaces `costSpentCents > 0` — is **NOT met.** The in-progress envelope-return
fix (#11 → #12) is **superseded by a decided V1 architecture** and has been
**reverted from `main`**; the proper build is **routed to a new phase** (option
A), which also folds in the post-v1.0.0 "envelope as first-class artifact" work.
Once that phase lands, the real run is re-attempted and — on a legitimate
Complete with a pushed branch and real cost — unblocks the v1.0.0 retag.

## Confirmed root cause (defects #11 / #12)

**PVCs are namespace-local, so the Manager (in `tide-system`) cannot read a
planner/executor `out.json` written to the PVC in the project namespace.** The
entire "Manager reads the PVC" premise — behind #10b's prompt read AND #11's
`FilesystemEnvelopeReader` rewiring — is wrong across the namespace boundary.

Full evidence (confirmed by `kubectl get pvc`, see the detailed log below):

- Two (and at fan-out, three) distinct PVCs all named `tide-projects` exist in
  **different namespaces**, bound to **different PVs**:
  - `tide-system/tide-projects` → PV `pvc-dd17ce32…` (10Gi, **RWO**) — what the
    **Manager** mounts at `/workspaces` (chart `deployment.yaml:115-132`).
  - `tide-sample-medium/tide-projects` → PV `pvc-a0ab7f4d…` (1Gi, RWX) — what the
    **dispatched planner/task/clone pods** mount (the sample's
    `per-namespace-resources.yaml:39-52`).
- Jobs are created in `task.Namespace` (the project namespace), per
  `backend.go:295` — so the subagent writes `out.json` + `children/*.json` to the
  **medium-namespace** PVC, which the Manager never sees. The Manager read fails
  with `… out.json: no such file or directory` (`project_controller.go:802`),
  no Milestone materializes, Project stays `AuthoringPlanner=True`.
- The Blocker #2/#3 design (single shared RWX `tide-projects` PVC +
  Manager-side `FilesystemEnvelopeReader`) is the **early-phase shortcut** that
  does not fit the real multi-namespace topology. It only ever worked in
  same-namespace / single-PVC test setups. The removed `cmd/manager/main.go` doc
  comment said exactly this: "prefer the completed subagent container's
  termination message because Task PVCs are namespace-local; keep the filesystem
  reader as a same-namespace/local-test fallback."
- The SAME cross-namespace gap exists for the #10b `promptReader`
  (`FilesystemEnvelopeReader{/workspaces}` reading `Task.Spec.PromptPath`): it was
  "live-verified" earlier only for SourcePath *stamping* (subagent side), never
  for the Manager actually reading the prompt across the namespace boundary at
  executor dispatch.

## Decided architecture (V1) — verbatim user decision (2026-06-08)

- **Namespace model: per-namespace pods + PVCs is CORRECT** (tenant isolation of
  untrusted agent compute from the operator). The Blocker #2/#3
  single-shared-RWX-PVC + Manager-`FilesystemEnvelopeReader` design is the
  early-phase shortcut that does NOT fit; it is **superseded**.
- **Root cause of #11/#12:** PVCs are namespace-local, so the Manager
  (`tide-system`) cannot read planner/executor `out.json` in the project
  namespace. The Manager-reads-PVC premise (behind #10b's prompt read AND #11's
  `FilesystemEnvelopeReader` rewiring) is wrong cross-namespace.
- **Decided architecture (V1): cross-namespace CONTROL data returns via the
  Kubernetes API.** A trusted, RBAC-scoped in-namespace TIDE reporter reads the
  subagent's `out.json` from the local (same-namespace) PVC and **CREATES the
  child CRs directly via the API** (ownerRef to the same-namespace parent); the
  Manager watches them appear. Tiny status (usage / git / exitCode / reason) can
  ride the 4 KB termination message (fits once childCRDs are not in it). The
  verbose result stays on the namespace PVC as the audit artifact. **NEVER put
  blobs in etcd.** Large intra-project blobs between task pods = the per-project
  PVC + git worktrees (already the design). An object store (MinIO/S3) is **NOT**
  used; it would only ever apply, behind a pluggable interface and off by
  default, if large data must cross a namespace (not v1).
- **This fix is ROUTED TO A NEW PHASE (option A):** build V1 properly (+ fold in
  the post-v1.0.0 envelope-as-first-class-artifact work), THEN the real-Claude
  end-to-end run lands and unblocks the v1.0.0 retag. Do **not** fix it in this
  debug session.

## Still UNVERIFIED downstream (validate once V1 lands and a real run reaches Complete)

The real anthropic `Run()` now executes the agent loop and exits 0, and the
cascade has mechanically advanced Project → Milestone → Phase → Plan in prior
runs. But everything below the control-data-return boundary is unverified
end-to-end and must be re-validated after V1 closes the #12 root cause:

- **Real executor + worktree commit** — no Task/executor has actually authored +
  committed code to a worktree in a real run.
- **Per-run branch push** — the `tide/run-medium-project-*` branch has NEVER
  appeared on the in-cluster `http://` remote (master unchanged in every run so
  far; the #9 fix stopped the *hollow* Complete, but no real push has landed).
- **Per-level model resolution** — each planner/executor level resolving its
  configured model is unproven past the levels reached.
- **Defect #6 — `estimatedCostCents` = 0 / empty `budget: {}`** despite real
  token bills (e.g. inputTokens=16579, outputTokens=7493). DoD requires
  `costSpentCents > 0`; the per-model price table or the controller-side
  usage→cost roll-up is unverified. Deferred until a legitimate Complete.

Treat each as unverified until a real run reaches `Complete`.

## Clean-pause disposition of the working tree (2026-06-08)

- **COMMITTED to `main` (independently necessary):** `17f2d10` —
  `test(08): set required Task.PromptPath on controller test fixtures`. Commit
  `1f8fc86` made `TaskSpec.PromptPath` required (MinLength=1, no omitempty) but
  six envtest Task fixtures across `plan_controller_test.go`,
  `plan_webhook_test.go`, `task_controller_test.go`, `wave_controller_test.go`
  never set it → 36 envtest specs RED on clean HEAD. This atomic commit adds a
  valid workspace-relative `PromptPath` to each, restoring green.
- **REVERTED from the working tree (superseded #11 envelope-return set):** the
  `TerminationStub` type + `NewTerminationStub` (`pkg/dispatch/envelope.go` and
  its tests), the `writeEnvelope`/`writeTerminationMessage` stub-writer changes
  (`cmd/claude-subagent/main.go`, `cmd/stub-subagent/main.go` + test), the
  `cmd/manager/main.go` rewire from `PodStatusEnvelopeReader` →
  `FilesystemEnvelopeReader`, and the `backend_test.go` oversized-read regression.
  These are a **coupled** set (the new reader path depends on the stub writer;
  reverting one without the other leaves a broken read path) and are
  **cross-namespace-broken** per #12. `main` is restored to the last-coherent
  committed state — the termination-message read (`PodStatusEnvelopeReader` with a
  `FilesystemEnvelopeReader` same-namespace fallback) that works for small
  envelopes. Acceptable while parked: the new phase rebuilds this area under V1.
- **`main` verified GREEN after the revert + commit:** `go build ./...` exit 0;
  `go vet ./internal/... ./api/... ./cmd/...` exit 0; `gofmt -l` clean;
  `go test ./internal/controller/... ./internal/subagent/... ./internal/dispatch/...
  ./api/... ./cmd/... -short -count=1` → **TEST_EXIT=0**, every package `ok`, zero
  `--- FAIL`/`FAIL`.

## Live repro env — PARKED

minikube context `minikube`; medium Project at `AuthoringPlanner=True`. The
controller is runtime-patched with `--subagent-image=…tide-claude-subagent:1.0.0`
(arg index 5). Images in the cluster may be **stale** after this revert (the
controller image carried the reverted `FilesystemEnvelopeReader` rewire) — the
new phase will rebuild the controller + subagent images cleanly. Do not rely on
the parked cluster state; it is illustrative, not a coherent base.

---

# Investigation log (chronological evidence trail — preserved verbatim)

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

---

### 4. Project outcome prompt never threaded into `EnvelopeIn.Prompt` — FIXED (committed f71dd47; live-verified)

**Resolution (fix-shape option b, per user decision 2026-06-08):** Added a
distinct `prompt string` parameter to `BuildPlannerEnvelope`
(`internal/controller/dispatch_helpers.go`) and assigned it to
`EnvelopeIn.Prompt`. All four planner call sites updated: project passes
`project.Spec.OutcomePrompt`; milestone/phase/plan pass the same outcome via a
nil-safe `outcomePromptOf(project)` helper (parent artifact context lives on the
PVC; the templates instruct reading it; ParentName flows via
`EnvelopeIn.Dispatch`). `token` and `prompt` are now distinct params (previously
all four sites passed `""` as the TOKEN arg, never a prompt). Added
`TestBuildPlannerEnvelopePromptThreading` (threading + JSON round-trip +
nil-safety) and a Prompt assertion to the existing structure test.

**Live-verified (2026-06-08):** the re-applied medium run's `in.json` now carries
the full FormattedNow outcome prompt — `"prompt":"The targetRepo is a tiny Go
scaffold ... Add a second exported function ... FormattedNow() string ..."`. The
EMPTY_PROMPT warning is gone; Claude planned the correct milestone
(`milestone-01-formatted-now-function`) directly from the real outcome. Prompt
threading confirmed end-to-end.

### 5. Anthropic runner never parses `ChildCRDs` — FIXED in code (committed f71dd47); BLOCKED end-to-end by defect #7

**Resolution (fix-shape option a, per user decision 2026-06-08):** File-handoff
contract. `Anthropic.Run` now reads `<eventsDir>/children/*.json` on a clean
planner exit into typed `[]ChildCRDSpec` via new `readChildCRDs`
(`internal/subagent/anthropic/subagent.go`): traversal-safe (symlink + escape
rejection via `EvalSymlinks`/`Lstat`), Kind allowlist
(Milestone/Phase/Plan/Task/Wave), deterministic filename order, empty-name
rejection, missing-dir → zero children (not an error), poisoned-dir → task-level
failure (ExitCode/Reason). Only runs for `Role=="planner"`. All four planner
templates updated with the explicit "use your Write tool to create
`/workspace/envelopes/{{.TaskUID}}/children/<kind>-NN.json`" instruction and the
exact `{kind,name,spec}` JSON shape. Tests: happy-path populate, missing-dir,
Kind-allowlist, empty-name, symlink-traversal, malformed-JSON. All green.

**End-to-end status:** the read+validate logic is correct and tested, but the
live run produced NO `children/` dir because Claude could not write the file —
see defect #7. The runner correctly returned zero children (missing dir is not
an error), so no Milestone materialized and the Project stayed at
AuthoringPlanner=True. This fix is verified at the unit level and will materialize
children as soon as #7 unblocks Claude's Write tool.

### 7. claude CLI `--bare -p` runs with file-write tools sandboxed/denied — OPEN (NEW FIX-SHAPE FORK)

**This is the new terminator-candidate fork.** With #4 and #5 deployed (fresh
controller `ec3e2694…` + claude-subagent `10989704…` images loaded into minikube,
subagent-image patch verified at arg index 5), the re-applied medium planner
ran the real Haiku agent loop, exited 0, and authored the correct milestone
content — but COULD NOT WRITE the child-CRD file.

**Direct evidence (out.json `result` field, this run):**
> "I'm encountering sandbox restrictions that prevent direct file creation, even
> though I'm running as the `node` user who owns the directories. Let me provide
> the content in a clear format that shows exactly what needs to be created..."

Claude then printed the exact correct `milestone-01.json` body and noted
"**File Location Required:** `/workspace/envelopes/af7027ce-…/children/milestone-01.json`"
— it knew the path and the shape (the #5 template instruction landed perfectly),
but the Write tool was denied. `events.jsonl` shows the thinking trace planning
the write; no `tool_use` Write event succeeded; no `children/` dir exists on the
PVC.

- **Producer (runner):** `Anthropic.Run` builds
  `claude -p <prompt> --model <m> --output-format stream-json --verbose
  --include-partial-messages --bare` (subagent.go ~line 187). There is NO
  permission-mode flag. In non-interactive `-p` mode the claude CLI defaults to
  a restrictive permission posture; file-mutating tools (Write/Edit) require
  approval, and with no interactive approver the writes are denied. `--bare`
  hardens hermeticity but does not grant tool permissions.
- **Why the stub masked it:** the stub builds ChildCRDs in Go and never invokes
  a tool. #5's file-handoff is the FIRST code path that requires Claude's Write
  tool to actually function in-pod.

**Token usage this run (real bill, under cap):** inputTokens=20637,
outputTokens=16800, cacheRead=1010395, cacheCreation=31782; estimatedCostCents=0
(defect #6 cost-accounting still open). The agent loop did real work; only the
filesystem write was blocked.

**Fix options (design call — NEW fork, needs user decision on shape):**
- (a) Add a non-interactive permission flag to the claude invocation that allows
  the Write tool within the per-task children dir — e.g.
  `--permission-mode acceptEdits` (or `--allowedTools "Write Edit"`, or the
  CLI's current equivalent). Narrowest: keep `--bare`, grant just the write
  capability. Must confirm the EXACT flag name/spelling supported by the pinned
  CLI (`@anthropic-ai/claude-code@2.1.142`) before applying — flag surface
  drifts. Verify it does not re-enable host config auto-discovery.
- (b) Constrain writes to the children dir via `--add-dir` /
  working-directory + an allow-list so the granted write capability is scoped to
  `/workspace/envelopes/<UID>/children` only (defense-in-depth on top of the
  runner's existing traversal guard).
- (c) Switch the handoff OFF the Write tool entirely: have the runner parse the
  fenced ```json child-CRD block out of the final `result` text (defect #5
  option b, previously deferred). Removes the tool-permission dependency, but
  reintroduces the brittleness the user explicitly rejected for #5. NOT
  recommended — listed only because the model already emits parseable JSON in
  `result` today.

Recommended: (a)+(b) together — grant the Write tool but scope it to the
children dir, keeping `--bare` hermeticity. This preserves the file-handoff
contract the user chose for #5 and keeps the runner's traversal/Kind/empty-name
guards as the second line of defense.

**Repro env (for the continuation):** minikube context `minikube`; controller
pod runs fresh `ec3e2694…`; `--subagent-image=…tide-claude-subagent:1.0.0` is
present at args[5]; medium Project re-applied and stuck at AuthoringPlanner=True;
git-http-server serving `demo-remote.git` (branch `master`@`32387267`); all
medium namespace infra (PVCs, secrets, SAs) intact. The deleted `demo-remote-init`
Job is not needed — the bare repo on `demo-remote-pvc` is already populated.

---

### 7. claude CLI `--bare -p` ran with file-write tools denied — FIXED (committed 7d9cd6e; live-verified)
### 8. Prompt passed as single `-p` CLI arg → MAX_ARG_STRLEN risk — FIXED (committed 7d9cd6e; live-verified)

**Resolution (both, one image rebuild — fix shapes DETERMINED against pinned
`@anthropic-ai/claude-code@2.1.142`):** in
`internal/subagent/anthropic/subagent.go` the claude invocation now:
- omits the positional prompt (`-p` with no arg) and delivers `renderedPrompt`
  via `cmd.Stdin = strings.NewReader(renderedPrompt)` (#8 — stdin cap 10 MB vs
  128 KiB per-arg; no `--prompt-file` flag exists in 2.1.142; no envelope-schema
  change — `EnvelopeIn.Prompt` still travels in file-based `in.json`);
- adds `--permission-mode acceptEdits` (the only headless mode that auto-approves
  Write/Edit) and `--add-dir <eventsDir>` scoped to the per-Task dir
  `<WorkspaceRoot>/envelopes/<TaskUID>` (the same dir `readChildCRDs` scans
  `children/` under) — minimal privilege; `--bare` retained (#7).
`eventsDir` is now computed/created before the args build so `--add-dir` can
reference it. New `TestRun_PromptViaStdinAndPermissionFlags` asserts the prompt
is absent from `cmd.Args`, delivered via stdin, and the new flags + `--bare` are
present. `go test ./internal/subagent/anthropic/... ./internal/subagent/common/...
./cmd/claude-subagent/...` all green; `go vet` + `gofmt` clean.

**Live-verified (2026-06-08):** rebuilt ONLY the claude-subagent image
(`sha256:384808b1…`, replacing stale `10989704…`; `minikube image rm` then
`load`), controller untouched (`--subagent-image=…:1.0.0` patch intact). Deleted
+ re-applied the medium Project (fresh UID `b560b576`). The project planner
exited **Completed**, Claude's **Write to `children/` SUCCEEDED** (no "sandbox
restrictions" string), `readChildCRDs` populated `EnvelopeOut.ChildCRDs`, and a
real **Milestone `milestone-01-formattednow-rfc3339` materialized** (authored
from the real outcome prompt). The cascade then advanced mechanically with the
same #4/#5/#7/#8 patterns: milestone planner → Phase
`phase-01-implement-formattednow` materialized → phase planner → Plan
`89b2eb4d…` materialized → plan planner Running. #7 and #8 are CLOSED.

### 9. Milestone (+ Project) succeed on child *materialization*, not child *completion* — OPEN (NEW FIX-SHAPE FORK)

**Observed on the #7/#8 verify run:** within ~2 min the Project reported
`status.phase=Complete` ("All owned Milestones reached Succeeded") and the
Milestone reported `Succeeded` ("Milestone planner completed; Phase children
materialized") — WHILE the child Phase was still `Running`, only ONE Plan had
materialized, ZERO Tasks/executors had run, and the run branch
`tide/run-medium-project-1780923091` did **not exist on the git remote** (master
unchanged, no commits). The "Complete" is hollow: no code was authored/committed.
The lower-level cascade (plan planner → tasks → executor → push) was still in
flight underneath the terminal top-level status.

- **Root cause (controller):** `MilestoneReconciler.handleJobCompletion`
  (`internal/controller/milestone_controller.go:453-465`) gates only the
  *boundary push* on `gates.BoundaryDetected(ctx, …, "Phase")` (all child Phases
  Succeeded) but then **falls through to `patchMilestoneSucceeded`
  unconditionally** (the `else` branch at :461 merely logs). The doc comment at
  :448-452 makes this explicit: "the milestone level's own Job completion is a
  sufficient signal for parent-Status=Succeeded; the push semantic is what's
  tightened, not the level transition." That premise is the bug.
- **The Phase controller already has the correct fix (Phase 04.1).**
  `PhaseReconciler.handleJobCompletion` (`phase_controller.go:373-389`): when
  child Plans exist but are NOT all Succeeded, it **requeues
  (`RequeueAfter: 5s`) instead of patching Succeeded** (:381-384). That's exactly
  why the Phase correctly stayed `Running`. The Milestone never received the
  same `hasChildPhases`+requeue guard.
- **Project inherits the bug:** `ProjectReconciler` rolls up
  `MilestonesSucceeded` (`project_controller.go:623`) from the milestone's
  (wrongly-early) `Succeeded` status → Project `Complete`.

**Fix options (design call — NEW fork, needs user decision on shape):**
- (a) **Mirror Phase 04.1 onto the Milestone (RECOMMENDED).** Add a
  `hasChildPhases` check; when child Phases exist and `BoundaryDetected("Phase")`
  is false, `return ctrl.Result{RequeueAfter: 5s}, nil` instead of calling
  `patchMilestoneSucceeded`. Milestone reaches `Succeeded` only once all child
  Phases Succeed; Project `Complete` then correctly follows. Smallest change,
  directly parallels the already-shipped Phase fix. Will require updating the
  milestone gate-test fixtures the :448-452 comment warns assert immediate
  Succeeded on auto-gate (they encode the buggy contract).
- (b) Introduce an explicit intermediate Milestone phase (e.g.
  `ChildrenRunning`) so the level transition reflects "planned, not done," and
  only patch `Succeeded` when children complete. Cleaner status semantics,
  larger surface (new phase value, CEL/printer columns, Project roll-up update).
- (c) Leave the Milestone status as-is and tighten only the **Project** roll-up
  to require all transitive Phases Succeeded before `Complete`. Narrowest at the
  top, but leaves Milestone status semantically wrong (Succeeded-while-Running).

**Verify note:** this same premature-succession pattern should be checked at the
Project planner→Milestone hop too (Project may flip Complete on milestone
*materialization* independent of milestone status — though here it rolled up from
the milestone's early Succeeded, so fixing (a) likely fixes the Project path as a
side effect; confirm after the fix).

**Repro env (live, for continuation):** minikube context `minikube`; controller
unchanged; fresh claude-subagent `384808b1…` loaded; medium Project UID
`b560b576` is `Complete` (prematurely) with Milestone `Succeeded`, Phase
`Running`, 1 Plan, plan planner `tide-plan-89b2eb4d…` Running. #7/#8 are CLOSED
and proven on this run. costSpentCents/budget accounting (#6) shows
`budget: {}` empty on Project status — still OPEN, deferred.

---

### 9. Milestone (+ Project) succeed on child *materialization*, not child *completion* — FIXED in code (uncommitted; unit-verified). Awaiting live re-verify.

**Resolution (fix-shape option a, per user decision 2026-06-08):** Mirrored the
Phase 04.1 guard onto `MilestoneReconciler.handleJobCompletion`
(`internal/controller/milestone_controller.go`). Added `hasChildPhases(ctx, ms)`
(symmetric with `PhaseReconciler.hasChildPlans`). The boundary block now:
`if detected { push } else if justMaterialized || r.hasChildPhases(...) { return
ctrl.Result{RequeueAfter: 5s} } else { skip }`, then
`patchMilestoneSucceeded`. The Milestone reaches Succeeded ONLY once all child
Phases Succeed; Project `Complete` follows via the existing MilestonesSucceeded
roll-up (no Project-controller change needed — confirmed by reasoning + green
project tests).

**One addition beyond the literal Phase 04.1 mirror — `justMaterialized`:** the
straight mirror (`hasChildPhases` only) FAILED a new unit test because on the
*same* reconcile that materializes the child Phase, the cached client may not yet
list the just-created Phase, so `hasChildPhases` returned false and the code fell
through to `patchMilestoneSucceeded` — terminal, never re-evaluated (Step-1
short-circuit at :186 returns on Succeeded/Failed). This is a latent race the
Phase controller shares; the Milestone fix is strictly more correct: when
`MaterializeChildCRDs` runs THIS reconcile, set `justMaterialized=true` and
requeue unconditionally (the boundary cannot possibly be satisfied yet).

**Default-gate gotcha (why the first test draft mis-fired):** `gates.EvaluatePolicy`
defaults `milestone` to `PolicyApprove` when unset. The bug path is the AUTO
milestone-gate (the medium sample sets `gates.milestone=auto`). Test 4 creates a
dedicated `Gates{Milestone:"auto"}` Project so the reconcile reaches the boundary
check instead of parking at AwaitingApproval.

**Tests:** added `Test 4 (debug #9)` to `milestone_controller_test.go` — asserts
RequeueAfter>0 + Status≠Succeeded while a materialized child Phase is pending,
then Succeeded once the child Phase is patched Succeeded. Existing gate Test 2 /
Test 4 (assert immediate Succeeded on the *childless* auto path) still pass —
unchanged, because the auto-gate fixtures emit NO ChildCRDs so `hasChildPhases`
is false and the childless fall-through to Succeeded is still correct. Full
`go test ./internal/controller/ -short` GREEN (27s); gofmt + go vet clean.

**NOT yet committed; NOT yet live-verified** — controller image rebuild + rollout
+ subagent-image re-patch + medium re-apply still pending (blocked on the #10
decision below so a single rollout covers both controller changes if #10 also
touches controller code).

### 10. Plan-authored Task CRD rejected: `spec.declaredOutputPaths: Required value` + executor instruction never threaded — OPEN (NEW FIX-SHAPE FORK)

**Observed on the live #7/#8/#9 repro (OBSERVE FIRST):** with the cascade now
advancing past the Milestone, the **plan planner ran and authored a Task child**,
but materialization FAILED:
`kubectl get plan -n tide-sample-medium plan-01-add-formattednow` →
`status.phase=Failed`, condition:
`MaterializeChildCRDs: create Task/task-01-implement-formattednow:
Task.tideproject.k8s "task-01-implement-formattednow" is invalid:
spec.declaredOutputPaths: Required value`. The Phase is correctly held `Running`
(so the #9-class held at the Phase level already); the Plan is `Failed`.

**Root cause #10a (template/CRD shape mismatch — same class as #5/#7/#8):**
`internal/subagent/common/templates/plan_planner.tmpl`'s required-shape example
for the Task `spec` is `{ planRef, dependsOn, filesTouched, prompt }` — it
**omits `declaredOutputPaths`**, which the Task CRD requires
(`api/v1alpha1/task_types.go:89`, `+kubebuilder:validation:MinItems=1`, and in
the CRD `required` list alongside `filesTouched`+`planRef`). Claude faithfully
followed the template and omitted the field → CRD apply rejected. The template
also shows `filesTouched: []` (empty) which violates `FilesTouched` MinItems=1,
and a `prompt` key that is NOT a valid Task spec field (the stub builds the full
spec in Go; this is the first real plan-planner authored Task).

**Root cause #10b (executor prompt never threaded — #4-class recurrence at the
EXECUTOR level — the real fork):** even once the Task materializes, the executor
will run with an EMPTY prompt:
- `task_executor.tmpl:40` renders `{{.Prompt}}` — the executor template EXPECTS
  the task instruction in `EnvelopeIn.Prompt`.
- `TaskReconciler.buildEnvelopeIn` (`task_controller.go:1055-1068`) threads
  `FilesTouched/DependsOn/DeclaredOutputPaths/Caps/...` but **never sets
  `EnvelopeIn.Prompt`** → executor prompt is "" (exactly defect #4, now at the
  task level — the stub masked it because the stub executor reads
  `DeclaredOutputPaths` and writes a canned artifact, never a prompt).
- The Task CRD has **no inline prompt field**. `TaskSpec.PromptRef` (a ConfigMap
  name) EXISTS but is **dead — nothing in internal/, cmd/, or the subagent reads
  PromptRef anywhere**. So there is currently no field carrying the per-task
  executor instruction from the plan-authored Task CRD into `EnvelopeIn.Prompt`.

**Why a fork:** #10a is mechanical (template-shape, decided #5/#7/#8 class —
add the required fields). But #10b needs a design call on WHERE the per-task
executor instruction lives and how it reaches `EnvelopeIn.Prompt`:

- (a) **Add an inline `prompt string` field to `TaskSpec`** (e.g. `prompt`,
  optional or required-MinLength). plan_planner.tmpl already emits `prompt` in
  the example, so the template barely changes; `buildEnvelopeIn` threads
  `task.Spec.Prompt` → `EnvelopeIn.Prompt`. Smallest end-to-end change; one new
  CRD field + regen + one controller line + template keeps `prompt`. Per-Task
  CRD stays small (instruction is a few lines). Mirrors how the planner levels
  thread the outcome prompt via #4's `BuildPlannerEnvelope(..., prompt, ...)`.
- (b) **Wire the existing `PromptRef` ConfigMap path**: plan planner writes a
  ConfigMap per Task and sets `spec.promptRef`; `buildEnvelopeIn` reads the
  ConfigMap into `EnvelopeIn.Prompt`. Uses the already-declared field, keeps the
  Task `.status`/spec tiny for long prompts, but adds a ConfigMap create on the
  planner write path + a Get on the executor dispatch path + RBAC; the planner
  template's file-handoff is children/*.json, not ConfigMaps, so this also needs
  a second handoff convention. Larger surface.
- (c) **Derive the executor prompt from the Plan/Phase artifacts on the PVC**
  (PLAN.md task section) inside `buildEnvelopeIn`, no new field. Most
  encapsulated, but couples the controller to artifact parsing and the exact
  PLAN.md task layout — brittle, and the controller would have to read the PVC
  (it currently does not).

Recommended: **(a)** — symmetric with the #4 planner-prompt threading the user
chose (option b there: a distinct prompt param), keeps the children/*.json
file-handoff contract from #5, smallest correct change, and the Task CRD stays
well under etcd limits. #10a (declaredOutputPaths + filesTouched MinItems in the
template) applies mechanically regardless of the #10b choice. NOTE: whichever
shape, the plan_planner.tmpl example must also instruct a non-empty
`declaredOutputPaths` (the executor's artifact output paths) and non-empty
`filesTouched`.

**Coupling to the rollout:** #10b option (a) and the #9 fix are BOTH
controller-side (#10b also touches the CRD + buildEnvelopeIn). If (a) is chosen,
ONE controller image rebuild + rollout covers #9 + #10b together (then re-apply
the subagent-image patch at arg index 5). #10a + the template prompt change are
claude-subagent-side → one subagent rebuild. So the decision on #10b determines
whether this is 1 or 2 image rebuilds.

**Repro env (live, for continuation):** minikube `minikube`; controller still the
pre-#9 image; claude-subagent `384808b1…`; medium Project UID `b560b576` =
`Complete` (prematurely, pre-#9-rollout) with Milestone `Succeeded`, Phase
`Running`, Plan `plan-01-add-formattednow` `Failed` (declaredOutputPaths). #6
(cost accounting / empty budget) still OPEN+deferred until a legitimate Complete.

---

### 9. Milestone premature-succession — FIXED (committed d163384; LIVE-VERIFIED)
### 10a. plan_planner Task example missing required fields — FIXED (committed 29d72cf)
### 10b. Executor prompt as first-class PVC artifact (PromptPath) — FIXED in code (committed 1f8fc86); SourcePath stamping LIVE-VERIFIED

**Resolution #9 (option a, committed d163384):** mirrored the Phase 04.1 guard onto
`MilestoneReconciler.handleJobCompletion` — `hasChildPhases` + `justMaterialized`
race guard → requeue(5s) instead of unconditional `patchMilestoneSucceeded`.

**Resolution #10a (committed 29d72cf):** plan_planner.tmpl Task example now carries
non-empty `filesTouched` + `declaredOutputPaths` and a full per-task `prompt`.

**Resolution #10b (Path A, committed 1f8fc86):** prompt is a first-class PVC
artifact. Removed dead `PromptRef`; added `TaskSpec.PromptPath` (required,
MinLength=1). Added `ChildCRDSpec.SourcePath`, stamped by the subagent's
`readChildCRDs` with the workspace-relative origin of each child file; the
materializer copies `child.SourcePath` → `Task.Spec.PromptPath`. New narrow
`PromptReader` interface + `FilesystemEnvelopeReader.ReadPrompt` (traversal/
absolute/empty defenses) reads `.spec.prompt` fresh from the children file each
dispatch; `buildEnvelopeIn` threads it → `EnvelopeIn.Prompt` (hard error on
unreadable/empty). Delivery path (base64 env → in.json) unchanged. Regenerated
config/crd + charts/tide-crds; values.yaml untouched. Unit tests green.

**LIVE re-verify (2026-06-08, fresh controller c5f2c18f24d3 + claude-subagent
e16e0f56c8fe loaded into minikube; CRD PromptRef→PromptPath applied; medium
re-applied UID cbfba2cb):**
- #9 HELD: Milestone stayed `Running` while child Phase was in flight — NO
  premature Succeeded/Complete. (Previously flipped Complete in ~2 min.)
- #10b SourcePath stamping CONFIRMED: the phase planner's EnvelopeOut.childCRDs
  carry `"sourcePath":"envelopes/<plannerUID>/children/plan-01.json"` for each
  child (visible in the termination message tail). The subagent-side wiring works.
- #10a/#10b end-to-end at the TASK level NOT yet reached — the cascade failed one
  level higher at the Phase planner (defect #11 below) before any Plan/Task
  materialized. #10 will be confirmed once #11 unblocks the cascade past Phase.

### 11. PodStatusEnvelopeReader chokes on the 4 KB-capped termination message — OPEN (NEW FIX-SHAPE FORK)

**Observed (OBSERVE FIRST):** the medium re-run advanced Project→Milestone→Phase
correctly, the phase planner pod **Completed** and authored 2 valid Plan children
on the PVC — but the Phase went `Failed`:
`Failed=True reason=EnvelopeReadFailed msg=unmarshal termination envelope for pod
.../tide-phase-...-qzqjr: invalid character 'h' looking for beginning of value`.
No Plan materialized; the cascade halted at the Phase.

**Root cause:** `PodStatusEnvelopeReader.ReadOut` (backend.go ~92-119) reads the
EnvelopeOut from the subagent container's `state.terminated.message`. Kubernetes
caps that message at **4096 bytes** (confirmed: the message is EXACTLY 4096 bytes
and starts mid-JSON at `" have successfully completed..."` — the leading
`{"...,"result":"I` was truncated off the FRONT). The subagent writes the FULL
EnvelopeOut — including the model's verbose `result` summary — to
`/dev/termination-log`, so any envelope whose `result` pushes it past 4 KB is
truncated → malformed JSON. The #7/#8 run only passed because those planner
results happened to fit under 4 KB; this phase planner's chatty summary overflowed.
Worse: when the termination message is present-but-malformed, `ReadOut` returns the
parse error IMMEDIATELY and does **not** fall through to the `Fallback`
(FilesystemEnvelopeReader), even though the COMPLETE, valid out.json sits on the
PVC. (PVC out.json confirmed present; controller is distroless so couldn't `cat`
it, but the childCRDs+sourcePath visible in the truncated tail prove the writer
emitted a full envelope.)

**Fix options (design call — NEW fork, needs user decision on shape):**
- (a) **Fall back to the PVC on a malformed/oversized termination message.** In
  `PodStatusEnvelopeReader.ReadOut`, if the termination message fails to unmarshal
  (or is suspiciously ~4096 bytes), log + fall through to `r.Fallback.ReadOut`
  (the FilesystemEnvelopeReader reading the authoritative out.json on the PVC)
  instead of returning the parse error. Smallest change; the PVC file is already
  the source of truth and already mounted on the Manager. Keeps the
  termination-message fast-path for small envelopes. RECOMMENDED.
- (b) **Stop putting the full envelope in the termination message.** Have the
  subagent write only a tiny status stub (exit code + out.json path) to
  `/dev/termination-log`, and make the controller always read the full EnvelopeOut
  from the PVC. Cleaner long-term (the 4 KB cap stops being load-bearing) but
  touches the subagent writer + the reader contract + tests; larger surface.
- (c) **Truncate `result` in the envelope writer** so the termination message
  stays under 4 KB. Brittle (any field can overflow), and it loses the full result
  text the operator may want; NOT recommended.

Recommended: **(a)** — the PVC already holds the complete envelope and the Manager
already mounts it; treat the termination message as a best-effort fast-path and
fall back to the PVC whenever it doesn't parse. (b) is the cleaner follow-up but is
a broader contract change; (a) unblocks the cascade now with minimal risk.

**Repro env (live):** minikube; controller c5f2c18f24d3 (fresh, #9+#10b);
claude-subagent e16e0f56c8fe (fresh, #10a+#10b SourcePath); Task CRD schema
updated in-cluster (promptPath required); medium Project UID cbfba2cb is
`Running` with Milestone `Running` (correctly, #9 holds), Phase
`phase-01-implement-time-function` `Failed` (EnvelopeReadFailed), 0 Plans/Tasks.
The phase planner's complete out.json (with 2 Plan children + sourcePath) is on
the PVC at envelopes/6005db90-.../out.json. Real Haiku tokens billed
(inputTokens=16579, outputTokens=7493; estimatedCostCents=0 — #6 still open).

---

### 11. PodStatusEnvelopeReader chokes on the 4 KB-capped termination message — FIX ATTEMPTED (option b), then SUPERSEDED by defect #12; ALL #11 CODE REVERTED FROM main

**Fix-shape option b was attempted (root-cause fix): the termination message
becomes a SMALL STATUS STUB and the PVC out.json is the authoritative copy.** The
work (TerminationStub + NewTerminationStub in `pkg/dispatch/envelope.go`; stub
writers in `cmd/{claude,stub}-subagent/main.go`; the `cmd/manager/main.go`
`envReader` switch from `PodStatusEnvelopeReader` → `FilesystemEnvelopeReader`;
plus the `TestNewTerminationStub_StaysSmall` /
`TestFilesystemEnvelopeReaderReadsOversizedEnvelope` /
`TestWriteEnvelopeWritesSmallTerminationStub` tests) was unit-green and the stub
HALF was even live-confirmed at runtime (the project planner's termination
message was the small stub, the full envelope on the PVC).

**But the option-b PREMISE — "make the Manager's PVC read of out.json
authoritative" — is architecturally wrong cross-namespace.** See defect #12.
**This entire #11 code set has been REVERTED from `main`** during the clean-pause
(2026-06-08). `main` is restored to the last-coherent committed state: the
termination-message read (`PodStatusEnvelopeReader` with a same-namespace
`FilesystemEnvelopeReader` fallback) that works for small envelopes. The proper
fix is rebuilt under the new phase via the decided V1 architecture
(trusted-in-namespace reporter creates child CRs via the K8s API).

The ONE independently-necessary fix from this pass — adding the now-required
`TaskSpec.PromptPath` (commit `1f8fc86`) to six envtest fixtures — was kept and
committed separately as `17f2d10` so `main`'s unit tests stay green. It is NOT
part of the reverted envelope-return set.

### 12. The Manager's PVC and the dispatched-pod's PVC are DIFFERENT namespace-local volumes — ROOT CAUSE LOCKED; supersedes #11; ROUTED TO A PHASE

**Observed (OBSERVE FIRST):** after the #11 fix, the project planner exited 0 and
wrote a valid out.json + children/milestone-01.json to the PVC, but the
controller logs (current, not stale — repeating every reconcile, e.g.
`2026-06-08T14:52:10Z`):
`project planner envelope read failed; proceeding without ChildCRDs ...
read envelope out "/workspaces/bce318d6-.../workspace/envelopes/bce318d6-.../out.json":
... no such file or directory` (`project_controller.go:802`). No Milestone
materializes; Project stuck `AuthoringPlanner=True`.

**Root cause (CONFIRMED by `kubectl get pvc`):** there are TWO different PVCs
both named `tide-projects`, in different namespaces, bound to DIFFERENT PVs:
- `tide-system/tide-projects` → PV `pvc-dd17ce32...` (10Gi, **RWO**) — what the
  **Manager** mounts at `/workspaces` (chart `deployment.yaml:115-132`).
- `tide-sample-medium/tide-projects` → PV `pvc-a0ab7f4d...` (1Gi, RWX) — what the
  **dispatched planner/task/clone pods** mount (created by the sample's
  `per-namespace-resources.yaml:39-52`). A busybox mounting THIS PVC sees the
  out.json + MILESTONE.md + children/ the planner wrote.

So the planner writes out.json to the medium-namespace PVC; the Manager reads a
completely different tide-system PVC and never sees it. **PVCs are
namespace-local** — which is exactly why the original design used
`PodStatusEnvelopeReader` (termination message) as the PRIMARY read path. The
removed doc comment said so verbatim: "prefer the completed subagent container's
termination message because Task PVCs are namespace-local; keep the filesystem
reader as a same-namespace/local-test fallback." The FilesystemEnvelopeReader
fallback only ever worked in same-namespace / single-PVC test setups.

This means the #11 option-b choice (make the PVC out.json filesystem-read
authoritative from the Manager) is architecturally unworkable in the real
multi-namespace topology — the Manager cannot mount every per-namespace task PVC.
The user-decision premise "the controller already mounts the PVC for reads (it
does)" is true only for the tide-system PVC, NOT for the namespace-local task PVC
the planner actually writes.

NOTE: the SAME cross-namespace gap exists for the #10b `promptReader`
(`FilesystemEnvelopeReader{/workspaces}` reads `Task.Spec.PromptPath` off the
Manager's PVC) — it was "live-verified" earlier only for SourcePath *stamping*
(subagent side), never for the Manager actually reading the prompt across the
namespace boundary at executor dispatch. #12's fix must cover the prompt read too.

**Fix options that were considered (a–d):** (a) revert to the termination message
as authoritative carrier with bounded payload; (b) keep PVC-authoritative reads
but have the Manager read the task's namespace-local PVC via an ephemeral reader
Pod; (c) hybrid transport-envelope on the termination message with result
truncated; (d) single shared cluster-wide RWX PVC (rejected — conflicts with the
namespace-local isolation the sample deliberately sets up).

**DECISION (final, 2026-06-08): none of a–d as framed.** The user chose a cleaner
V1 design that supersedes the whole Manager-reads-PVC premise — see the top-of-file
"Decided architecture (V1)" section: **cross-namespace CONTROL data returns via
the Kubernetes API.** A trusted, RBAC-scoped in-namespace TIDE reporter reads
out.json from the local PVC and CREATES the child CRs directly via the API
(ownerRef to the same-namespace parent); the Manager watches them appear. Tiny
status rides the 4 KB termination message; verbose result stays on the PVC as
audit; NEVER blobs in etcd; object store only ever behind a pluggable interface,
off by default, and not in v1. The per-namespace pods+PVCs model is CONFIRMED
correct (tenant isolation). This is **routed to a new phase (option A)**, which
also folds in the post-v1.0.0 envelope-as-first-class-artifact work; the real
end-to-end run + v1.0.0 retag follow once that phase lands.

**Repro env (live, PARKED):** minikube context `minikube`; medium Project UID
`bce318d6` is `Running`/`AuthoringPlanner=True`; project planner pod Completed
with the small stub; out.json + children/milestone-01.json present on the
medium-ns `tide-projects` PVC (`pvc-a0ab7f4d`) but NOT on the tide-system PVC
(`pvc-dd17ce32`) the Manager reads. No Milestone materialized. clone Jobs were
erroring (likely separate/incidental retries). #6 (cost accounting,
estimatedCostCents=0) still OPEN+deferred. The in-cluster images may be stale
after the revert — the phase rebuilds them. The cluster is illustrative, not a
coherent base.

---

## LIVE RE-VERIFY (2026-06-09): fixes 546e84e + 8c1ccf7 confirmed; THIRD boundary-push defect (#13) found + FIXED

The earlier-committed fixes (the cascade stall + the EnvelopeReadFailed layer)
were live re-verified on a fresh medium run and are SUCCESSFUL: the run drove
the full cascade to Project=Complete (milestone/phase/plan Succeeded, tasks 3/3
Succeeded, status.budget costSpentCents=152, tokensSpent=271366). Both the
original stall and the EnvelopeReadFailed layer are confirmed fixed live.

But the re-verify exposed a THIRD defect in the boundary-push EXECUTION — the
merged run branch NEVER reached the remote, so the DoD (legitimate Complete +
pushed run branch) is still NOT met until a final live re-verify.

### 13. Boundary push attempts an empty commit on a clean tree and never pushes — FIXED in code (build+tests green; LIVE RE-VERIFY PENDING)

**Live evidence (ns tide-sample-medium, run branch
tide/run-medium-project-1781045746):**
- Per-wave integration push job `tide-push-wave-7bb6f8fd-...-1`: Complete. Log:
  "integration-only run — merged 1 task branch(es) into
  tide/run-medium-project-1781045746 locally; no artifacts to commit/push"
  → merges task branches into the run branch LOCALLY, does NOT push to the remote
  (intended, wave-internal).
- Project-level boundary push job `tide-push-fc1cefea-...`: FAILED 3/3
  (BackoffLimitExceeded). Pod log:
  "tide-push: commit failed: git commit \"tide: phase
  phase-01-implement-formatted-now authored\": cannot create empty commit: clean
  working tree" → it tries to git commit on a clean tree (the per-wave merge
  already integrated everything) and hard-fails BEFORE pushing.
- Remote bare repo /srv/git/demo-remote.git: only refs/heads/master at the
  initial fixture commit 3238726. NO tide/run-* branch. Nothing authored reached
  the remote.

**Root cause (CONFIRMED by reading the code — Observe First, not hypothesis):**
- `cmd/tide-push/main.go runPush` unconditionally called `pkggit.Commit`
  (line ~470) after the artifact-stage loop. The level boundary pushes
  (phase/milestone/project, dispatched by `triggerBoundaryPush` in
  `internal/controller/boundary_push.go`) carry NO `--artifact-paths` and NO
  `--integrate-task-branches` — production code NEVER sets `PushOptions.ArtifactPaths`
  (grep-confirmed). The per-wave integration job (commit 8e57348) already merged
  every task branch into the run branch on the shared PVC, leaving a CLEAN
  working tree. `pkggit.Commit` on a clean tree returns "cannot create empty
  commit: clean working tree" → exit 1 → the push at line ~507 is never reached.
- This is the SAME failure mode 8e57348 ("per-wave integration job is merge-only
  — no empty boundary commit") fixed for the WAVE job, but the project/milestone/
  phase boundary-push path still attempted the empty commit.
- **Contract clarified:** the per-wave integration job is correctly merge-only
  (wave-internal, no remote push — the next wave's worktrees only need the LOCAL
  run-branch advance). The LEVEL BOUNDARY push (phase/milestone/project) is the
  job responsible for pushing the run branch to the remote. With a clean tree it
  must SKIP the commit and STILL push the already-integrated HEAD.

**Fix (mirrors 8e57348 into the boundary-push execution path —
`cmd/tide-push/main.go runPush`):**
- After the artifact-stage loop, check `worktreeClean(worktreeDir)` (new helper:
  `git status --porcelain` empty — the same primitive
  `internal/harness.CommitWorktree` uses for the executor empty-diff policy).
- If CLEAN: skip the commit, set the pushed `newHash` to the current HEAD (the
  run-branch tip the integration merge advanced), and push it. The gitleaks scan
  base becomes `cfg.LastPushedSHA` (the remote anchor) so only newly-arriving
  content is scanned; with no anchor (first push of the run branch) the scan base
  falls back to HEAD → empty diff → no-op scan, push proceeds.
- If DIRTY (artifacts staged): commit then push (unchanged behavior).
- The push is idempotent: re-pushing an already-present run-branch HEAD is a
  no-op fast-forward, so a retried boundary-push Job converges (handles the
  BackoffLimit retry cleanly).
- The per-wave integration-only short-circuit (integrate set + no artifacts →
  merge locally, exit 0, no push) is UNCHANGED — still wave-internal.

**Tests:** added `TestRunPushBoundaryCleanTreePushesIntegratedBranch`
(`cmd/tide-push/main_test.go`): clone+provision run worktree → create a task
branch → per-wave integration push (asserts the remote run branch is NOT created)
→ level boundary push with NO artifacts/NO integrate (clean tree) → asserts exit
0, NO "cannot create empty commit", the merged run branch IS pushed to the remote
and carries the task file, and the push envelope reports success with a
HeadSHA == the remote ref. Existing `TestRunPushIntegrationOnlyNoArtifacts`,
`TestRunPushModeCleanFirstPush`, `TestRunPushModeWritesExactBoundaryCommitMessage`,
`TestRunPushWithIntegrateTaskBranches` all still pass.

**Verification (observed exit codes):** `make build` exit 0; `go test
./internal/controller/... -short` → `ok` (31.7s); `go test ./cmd/tide-push/...`
→ `ok` (8.4s); `make test` (full unit tier) → every package `ok`, zero `--- FAIL`/
`FAIL`, MAKE_TEST_EXIT=0. `gofmt -l` clean; `go vet ./...` clean.

**IMAGE REBUILD for the live re-verify:** this fix is in the `cmd/tide-push`
binary → the **tide-push image** must be rebuilt + reloaded. The controller
(`cmd/manager`) was NOT changed by this fix, so the controller image does not
strictly need a rebuild for #13 — though the cluster controller image may already
be stale from prior work and warrant a refresh independently.

### #13b FLAGGED (NOT decided — needs a user call): succession does not gate on push success

**Observed:** the Project reached `Complete` even though its boundary push FAILED
3/3 (BackoffLimitExceeded). Succession (Project→Milestone→Phase→Plan Succeeded
roll-up) does NOT gate on the boundary push exit code — a failed/never-completed
push leaves the Project `Complete` with NOTHING on the remote. That is how the
medium run reported `Complete` while `demo-remote.git` still showed only the
initial fixture commit.

This is a SEPARATE gap from #13 (the push-execution bug). #13's fix makes the
push SUCCEED on a clean tree, which masks the symptom for the happy path — but
the underlying "Complete can be reported with a failed push" remains. **This was
NOT changed** (the directive was to flag, not silently decide). Options for the
user:
- (a) Leave as-is: the push is best-effort observability; succession is a
  control-plane concept independent of the remote landing. (Current behavior.)
- (b) Gate the top-level Succeeded/Complete transition on a successful
  boundary-push envelope (reason=="" / exitCode==0), so a Project cannot report
  Complete until its run branch is actually on the remote — strongest DoD
  alignment, larger surface (the Project/Milestone roll-up would consume the push
  envelope status).
- (c) Surface push failure as a distinct non-terminal Project condition
  (e.g. `PushFailed=True`) without blocking Complete — visibility without a
  hard gate.

**Status after #13:** the boundary-push execution bug is FIXED (build+tests
green, committed). A FINAL live re-verify is still PENDING and is the gating
DoD check: a real medium run must reach Project=Complete AND the
`tide/run-medium-project-*` branch must be present on the in-cluster http://
remote (`demo-remote.git`). Rebuild + reload the **tide-push image** before that
re-verify. The #13b succession-vs-push-gate question is open for a user decision.
