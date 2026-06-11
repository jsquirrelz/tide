# Phase 3: Up-Stack Reconcilers, Git Integration, Real Subagent, Resumption - Context

**Gathered:** 2026-05-15
**Status:** Ready for planning

<domain>
## Phase Boundary

Light up the up-stack reconcilers (`Plan` → `Phase` → `Milestone` → `Project`) so planner subagents author `PLAN.md`, phase brief, and `MILESTONE.md` from compiled-in Go prompt templates; ship `pkg/git` with HTTPS+PAT, gitleaks, per-run branches, and `--force-with-lease` so the **orchestrator** (not subagent pods) pushes artifacts at every level boundary; swap the stub-subagent for a real Claude-Code-backed image (`claude -p --output-format stream-json` inside `internal/harness`) behind the same `pkg/dispatch.Subagent` interface; and prove `chaos-resume` works — kill the orchestrator pod mid-wave, new leader re-derives the wave schedule from `pkg/dag.ComputeWaves` against live Task CRDs + PVC contents only, no persisted schedule, no duplicate dispatch, no lost tasks.

Phase 3 ships:
- Up-stack reconciler bodies: `MilestoneReconciler` / `PhaseReconciler` / `PlanReconciler` each dispatch their own planner Jobs via `dispatch.Dispatcher.Run` (mirrors Phase 2 D-B1 for `TaskReconciler`). Each up-stack reconciler also consumes the planner's structured `EnvelopeOut.childCRDs` and server-side-creates child CRDs (`Milestone` → `Phase`s, `Phase` → `Plan`s, `Plan` → `Task`s + the materialized `Wave`s).
- `pkg/dispatch` envelope schema bump: `EnvelopeIn.Provider {Vendor, Model, Params}` + `EnvelopeIn.Role` (`planner | executor`) + `EnvelopeIn.Level` (`milestone | phase | plan | task`); `EnvelopeOut.childCRDs []ChildCRDSpec` + `EnvelopeOut.git.headSHA` + `EnvelopeOut.result` + `EnvelopeOut.usage` (extended with `cacheReadTokens`, `cacheCreationTokens`).
- `pkg/git`: thin Go API around `go-git/v5` for clone, worktree-add, commit, push (HTTPS+PAT) — provider-agnostic, host-agnostic (GitHub fixture verified; documented for GitLab/Gitea/generic remote).
- `internal/gitleaks`: thin wrapper around `github.com/zricethezav/gitleaks/v8/detect` invoked from the push Job binary; default rule set compiled in; per-Project rule overrides via ConfigMap.
- `cmd/tide-push/main.go` + `images/tide-push/Dockerfile`: dedicated push image. One Job per level boundary, deterministic-named `tide-push-{project-uid}` (serialization key — see D-B5).
- `internal/subagent/anthropic/`: provider-specific Subagent impl — invokes `claude -p --model <model> --output-format stream-json`, streams events to harness, extracts usage, preserves raw event log on PVC.
- `cmd/claude-subagent/main.go` + `images/claude-subagent/Dockerfile`: thin shim (~50 LOC) + `node:22-slim`-based image with `@anthropic-ai/claude-code@2.1.139+` installed alongside the Go wrapper.
- Stub-subagent extension: new mode `wait-for-signal` (PVC file polling at `/workspace/envelopes/{task-uid}/release`) so chaos-resume can pin Tasks at `Running` indefinitely and release them post-restart.
- `Project.Spec` schema extensions: `subagent.{image, model, levels.{milestone, phase, plan, task}.{image, model, params}}`, `git.{repoURL, credsSecretRef}`, `Project.Status.git.{branchName, lastPushedSHA, leaseFailureCount}`.
- Layer B kind chaos-resume integration spec asserting four pillars: Job UID continuity, `Task.Status.Attempt` unchanged, completed-set preserved, post-release Wave-reaches-Succeeded.

Phase 3 does NOT: ship per-level human gate policy (Phase 4 GATE-01..03 — Phase 3 has the unconditional auto-advance path only), wire OpenInference attribute extraction onto OTel spans (Phase 4 OBS-03..05 — Phase 3 preserves raw stream-json events; Phase 4 parses), implement `tide` CLI (Phase 4 CLI-01..04 — Phase 3 uses `kubectl` only), implement dashboard (Phase 4 DASH-01..05), expand provider matrix beyond Anthropic (`internal/subagent/openai/`, `internal/subagent/google/`, etc. are v1.x or community contributions — the layering pattern is the v1 commitment, additional providers are out of Phase 3 scope), add PR creation per git host (v2+ per REQUIREMENTS.md "Deferred"), or wire conversion webhooks (CRD-05 scaffold from Phase 1 stays a no-op in v1alpha1).

</domain>

<decisions>
## Implementation Decisions

### Up-stack dispatch & CRD materialization

- **D-A1:** Planner subagents emit **two parallel outputs** at every level: (1) the human-reviewable Markdown artifact (`MILESTONE.md` / phase brief / `PLAN.md`) committed to the per-run branch for review, and (2) a structured `EnvelopeOut.childCRDs []ChildCRDSpec` typed block consumed by the orchestrator. The structured block is the **authoritative source** for materializing child CRDs server-side; the orchestrator never parses Markdown. The two must be consistent — envelope is the contract, Markdown is the human surface. Preserves Phase 2 D-A4 (subagent pod has zero K8s verbs). `ChildCRDSpec` shape is typed in `pkg/dispatch`: `{Kind: string, Name: string, Spec: runtime.RawExtension}` — runtime-typed Spec lets each child CRD carry its own schema without dispatch package importing every CRD type.
- **D-A2:** Each up-stack reconciler dispatches its own planner Job directly via `dispatch.Dispatcher.Run`: `MilestoneReconciler` creates the `MILESTONE.md`-authoring Job; `PhaseReconciler` creates the phase-brief Job; `PlanReconciler` creates the `PLAN.md`-authoring Job; `TaskReconciler` continues creating executor Jobs (Phase 2 D-B1). Four dispatch sites total. Symmetric structural pattern, no new CRD types, no generalization of `Task` with a `Role` field. Each reconciler is the sole Job creator for its level — the same property Phase 2 locked at the Task level extends upward.
- **D-A3:** `pkg/dispatch.Subagent.Run(ctx, EnvelopeIn) (EnvelopeOut, error)` interface (Phase 2 D-A1) preserved **verbatim**. `EnvelopeIn` gains `Role: planner|executor` + `Level: milestone|phase|plan|task` (HARN-01) but the method signature is stable. Single public contract for all backends; pluggable image authors maintain one method.
- **D-A4:** Pool semaphore acquisition lives at the **calling reconciler**, not in the dispatcher. `MilestoneReconciler` / `PhaseReconciler` / `PlanReconciler` acquire `plannerPool` (Phase 1 POOL-01: default size 16) before calling `dispatch.Dispatcher.Run`; `TaskReconciler` acquires `executorPool` (default size 4). Matches the spec's "planning fans out wide, execution fans out narrow" argument. Phase 1's POOL-03 analyzer (cross-pool wait rejection) keeps protecting both call sites.

### pkg/git invocation site, push cadence, gitleaks

- **D-B1:** Dedicated `tide-push-{project-uid}` Job per level boundary push. Mirrors Phase 2 D-G1 `tide-init` Job pattern. The push Job mounts: (a) the per-Project shared PVC at `/workspace`, (b) `Project.Spec.git.credsSecretRef` Secret as `envFrom` (HTTPS PAT only on push Job pod, never on the controller pod). Reconciler stays non-blocking (Phase 2 D-B2 / CTRL-02 contract preserved); push Job is observed via `Owns(&batchv1.Job{})`. gitleaks resource cost isolated to push Job's lifetime.
- **D-B2:** Push cadence = **level boundaries only**. Four push points per Project:
  1. `Plan` all-Tasks-`Succeeded` → push with commit message `tide: plan <plan-name> authored + executed`.
  2. `Phase` all-`Plan`s-done → push with commit message `tide: phase <phase-name> authored`.
  3. `Milestone` all-`Phase`s-done → push with commit message `tide: milestone <milestone-name> authored`.
  4. `Project.Status.phase == Complete` → push with commit message `tide: project complete`.
  Per-Task artifact diffs accumulate on the PVC and ship together at the Plan boundary. Bounded push count (push count proportional to plan-count, not task-count). Cleaner git history; matches ROADMAP success criterion #4 wording.
- **D-B3:** gitleaks embedded as a **Go library**, not shelled out, not a separate container. Push Job binary imports `github.com/zricethezav/gitleaks/v8/detect`. Single-binary `images/tide-push/` image (matches `go-git/v5` pure-Go ethos in STACK.md). Default rule set compiled in via `go:embed` from gitleaks' upstream config; per-Project rule overrides supported via a ConfigMap mounted into the Job at `/etc/tide/gitleaks-config.toml`. On match: push Job exits non-zero with structured leak summary in stdout; reconciler increments `tide_secret_leak_blocked_total` Prometheus counter (label-bounded to project/phase/plan per Phase 4 OBS-02 pre-commitment).
- **D-B4:** **Worktrees-per-Task for executor parallelism.** Each executor Task pod's harness runs `git worktree add /workspace/worktrees/{task-uid}` rooted at the shared bare `/workspace/repo.git`. Per-task index, no `.git/index` race when two Tasks in the same wave commit concurrently. PVC layout extends Phase 2 D-G2:
  ```
  /workspace/repo.git/                # shared bare clone (orchestrator-created at Project init)
  /workspace/worktrees/{task-uid}/    # per-Task working tree (harness setup)
  /workspace/artifacts/M-N/P-N/L-N/   # Phase 2 D-G2 layout, unchanged
  /workspace/envelopes/{task-uid}/    # Phase 2 D-A2 layout, unchanged
  ```
  Planner Tasks (up-stack levels) don't need worktrees — they emit artifacts to `/workspace/artifacts/...` and envelopes only, never touch the working repo. Worktree setup is an executor-Task harness step. At level boundary, the push Job walks each Task's `EnvelopeOut.git.headSHA` and fast-forwards / merges the per-Task commits onto the per-run branch.
- **D-B5:** **Serialize push Jobs per Project.** At most one push Job per Project active at any time. Deterministic name `tide-push-{project-uid}` (project UID alone, no level suffix) — a second push attempt while the first is active hits K8s API `AlreadyExists`, the calling reconciler requeues. Sequential push throughput is fine because push cost is small (artifact diff + gitleaks scan on diff only). Avoids parallel-push-with-lease-retry complexity in v1.
- **D-B6:** **One branch per Project lifetime.** Branch name `tide/run-<project-name>-<unix-timestamp>` fixed at Project creation and stored in `Project.Status.git.branchName`. Unix epoch keeps refnames colon-free (RFC3339 would inject `:` into refname). Push Job updates `Project.Status.git.lastPushedSHA` on success. Subsequent push uses `--force-with-lease=refs/heads/<branch>:<lastPushedSHA>` — catches external pushes from outside TIDE (Pitfall 13). Lease fail → push Job exits non-zero, reconciler sets `Project.Status.phase=PushLeaseFailed` Condition + halts. Manual recovery via `kubectl annotate project foo tideproject.k8s/bypass-push-lease=true` (mirrors Phase 2 D-D4 bypass-budget annotation pattern; Phase 4 `tide approve --force-push` wraps it).

### Real Claude image swap-in (HARN-06) — pluggable provider pattern

- **D-C1:** **Provider-pluggable layering** (extends Phase 2 D-A1 public contract):
  ```
  pkg/dispatch/                       # PUBLIC — Subagent interface + EnvelopeIn/Out (Phase 2 D-A1, stable)
  internal/harness/                   # PROVIDER-AGNOSTIC — caps, redaction, signed-token client,
                                      #   output-path validation, stream-event tail buffer.
                                      #   Knows about envelopes; knows NOTHING about LLM vendors.
  internal/subagent/{provider}/       # PROVIDER-SPECIFIC — implements dispatch.Subagent.Run()
    internal/subagent/anthropic/      #   Phase 3 ships this
    internal/subagent/common/         #   shared JSONL stream-event reader, prompt-template loader,
                                      #   harness↔provider plumbing
  cmd/{provider}-subagent/main.go     # THIN SHIM — load EnvelopeIn from PVC, instantiate harness,
                                      #   instantiate internal/subagent/{provider}.New() as the
                                      #   dispatch.Subagent impl, run harness.Execute(), write
                                      #   EnvelopeOut. ~50 LOC per provider.
  images/{provider}-subagent/Dockerfile  # per-provider image: base + provider CLI + Go binary
  ```
  Phase 2's `cmd/stub-subagent/main.go` already follows this shape — the stub-ness lives in the thin shim, `internal/harness/` doesn't know. CLAUDE.md anti-pattern ("All Anthropic-specific code lives behind the Subagent interface in `internal/subagent/anthropic/`") is honored by this structure.
- **D-C2:** **Vendor + model are orthogonal axes.** `Project.Spec.subagent` CRD shape:
  ```yaml
  spec:
    subagent:
      image: ghcr.io/jsquirrelz/tide-claude-subagent:v0.1.0   # vendor selection (default for all levels)
      model: claude-sonnet-4-6                                # default model for all levels
      levels:                                                 # per-level overrides (all optional)
        milestone: { model: claude-opus-4-7 }                 # heavier model for planning
        phase:     { model: claude-sonnet-4-6 }
        plan:      { model: claude-sonnet-4-6 }
        task:      { model: claude-haiku-4-5 }
      # cross-vendor per level (v1.x or v2+):
      # levels:
      #   task: { image: ghcr.io/jsquirrelz/tide-openai-subagent:v0.1.0, model: o1-mini }
  ```
  Resolution chain at dispatch time (in each reconciler before calling `dispatch.Dispatcher.Run`): `Project.Spec.subagent.levels.{level}.{image,model}` → `Project.Spec.subagent.{image,model}` → Helm-chart default. Resolved values stamped into `EnvelopeIn.Provider.{Vendor, Model, Params}` and into the dispatched Job's `image`.
- **D-C3:** `EnvelopeIn.Provider` schema (Phase 2 D-A4 envelope schema bump):
  ```go
  type ProviderSpec struct {
    Vendor string            // "anthropic" | "openai" | "google" | "xai" | "opencode" | ...
    Model  string            // "claude-opus-4-7" | "claude-sonnet-4-6" | "o1-mini" | ...
    Params map[string]string // per-vendor tuning passthrough (temperature, thinking budget, etc.)
  }
  ```
  `internal/subagent/anthropic/` reads `Provider.Model` → passes to `claude -p --model <model> --output-format stream-json`. Sanity-checks `Provider.Vendor == "anthropic"` at startup (fail-fast if image and envelope disagree — defends against config drift). Future provider impls (`internal/subagent/openai/`, etc.) read the same fields with their own vendor sentinel.
- **D-C4:** **Helm-chart per-level model defaults** (matches spec's "planning fans out wide, execution fans out narrow"):
  - `milestone` → `claude-opus-4-7` (heaviest planning, lowest fan-out)
  - `phase` → `claude-sonnet-4-6` (planning, medium fan-out)
  - `plan` → `claude-sonnet-4-6` (planning, medium fan-out)
  - `task` → `claude-haiku-4-5` (execution, highest fan-out, cost-bounded)
  User can override per-Project; Helm defaults are sensible-for-self-hosting. Budget observability (Phase 2 D-D2 / Phase 4 OBS-02) naturally captures the cost mix because `Project.Status.budget` rolls up per `EnvelopeOut.usage`.
- **D-C5:** **Minimal stream-json parse in Phase 3 harness.** `internal/subagent/anthropic/` reads `claude --output-format stream-json` events line-by-line and extracts:
  - `usage.{inputTokens, outputTokens, cacheReadTokens, cacheCreationTokens}` → `EnvelopeOut.usage` (Phase 2 D-D2 budget tally needs this).
  - Final assistant-message text → `EnvelopeOut.result`.
  Raw event log preserved at `/workspace/envelopes/{task-uid}/events.jsonl` (size-capped via harness tail-buffer to bound PVC consumption on chatty runs). **Phase 4 OpenInference span attribute extraction reads the raw log** — no re-parsing of work Phase 3 already does; structured envelope stays small (etcd-friendly under PERSIST-02).

### Chaos-resume test design (PERSIST-04 / TEST-04)

- **D-D1:** **Layer B kind tier.** `make test-int` runs the chaos-resume spec against the kind cluster. `kubectl delete pod -l app=tide-controller-manager` mid-wave; leader-election lease window (~15s default) elapses; new pod takes over. Real Jobs persist across pod kill (Jobs are owned by Tasks, not by the controller pod). Deterministic Job names (Phase 2 D-B5: `tide-task-{task-uid}-{attempt-n}`) exercised under real K8s admission. Closest to ROADMAP success criterion #5 wording ("kills the orchestrator pod"). Test wall-clock budget: ~60s (leader-election + 30s buffer).
- **D-D2:** **Mixed-state 3-task fixture** (ROADMAP success criterion #5: "three Tasks in flight"):
  - T-α — no deps; `testMode=success`; runs fast (~1s); reaches `Status.Phase=Succeeded` pre-kill.
  - T-β — no deps; `testMode=wait-for-signal` (see D-D3); reaches `Status.Phase=Running` pre-kill (Job created, harness hanging on signal poll).
  - T-γ — `depends_on=[T-α]`; `testMode=wait-for-signal`; dispatches after α succeeds; reaches `Status.Phase=Running` pre-kill.
  Wave has all three lifecycle states pre-kill. Strongest assertions: completed-set preserved (α stays Succeeded), no-dup-dispatch (β/γ Job UIDs unchanged), indegree recompute (post-restart, γ still sees α=Succeeded and remains correctly dispatched).
- **D-D3:** **New stub-subagent mode `wait-for-signal`.** Extends Phase 2 D-F3 stub modes (`success | fail-exit-1 | hang | exceed-output-paths`). Stub polls `/workspace/envelopes/{task-uid}/release` every 500ms; on file appearance writes canned success envelope (Phase 2 D-F3 success-mode shape) + exits 0. Test fixture sequence:
  1. Apply Plan with α, β, γ; wait for the three pre-kill states (D-D2).
  2. Snapshot pre-kill state: `Map[TaskName → {JobUID, Attempt, Phase, JobCreationTimestamp}]`.
  3. `kubectl delete pod -l app=tide-controller-manager`.
  4. Wait for new leader (poll lease holder until distinct from killed pod, max 30s).
  5. Re-snapshot post-restart; assert D-D4 pillars.
  6. `kubectl exec` (or use a small PVC-write init Job) to `touch /workspace/envelopes/{β-uid}/release` and `{γ-uid}/release`.
  7. Wait for β, γ to reach Succeeded; verify Wave reaches Succeeded.
- **D-D4:** **Four-pillar Ginkgo assertion set:**
  1. **Job UID continuity** — for every in-flight Task (β, γ), `kubectl get job -l tideproject.k8s/task-uid=<task-uid>` returns the same UID pre- and post-restart. No re-creation of Jobs.
  2. **Attempt counter unchanged** — `Task.Status.Attempt` identical pre- and post-restart. No retry triggered by the leader handoff.
  3. **Completed-set preserved** — T-α remains `Status.Phase=Succeeded` post-restart with same `CompletedAt` timestamp.
  4. **Observed completion across kill** — after signaling β/γ release, both Tasks reach `Succeeded`; Wave reaches `Status.Phase=Succeeded`; no orphan Jobs (verified by `kubectl get jobs --all-namespaces -l tideproject.k8s/project-uid=<project-uid>` returning exactly 3 Jobs in `status.succeeded=1`).
  Plus an algorithmic invariant (5): `pkg/dag.ComputeWaves` invoked post-restart against the live Task CRDs produces identical wave structure (same node sets per wave layer) to the pre-restart computation — verified by golden-file comparison against a snapshot taken pre-kill.

### Claude's Discretion

- **Push Job RBAC scope.** Push Job's ServiceAccount needs: `secrets get` on `Project.Spec.git.credsSecretRef`; nothing else from the K8s API. Researcher and planner finalize: dedicated `tide-push` SA with single-Kind verb, or reuse `tide-orchestrator` SA. Lean toward dedicated SA for least-privilege.
- **Leader-election lease tuning for chaos-resume.** controller-runtime v0.24 defaults (15s lease + 10s renew + 2s retry) likely fine. Test budget allows up to 30s for failover. Planner may adjust `LeaseDuration` / `RenewDeadline` / `RetryPeriod` knobs if test flakiness surfaces.
- **`pkg/git` API surface.** Provider-agnostic Go API surface in `pkg/git`: `Clone`, `WorktreeAdd`, `Commit`, `Push` (with `ForceWithLease` option), `Fetch`. Planner picks exact function signatures; keep them generic enough that a future `internal/git/gitlab/` or `internal/git/gitea/` adapter could plug behind the same `pkg/git.Remote` interface if PR-creation surfaces later (v2+).
- **`Project.Status.git.lastPushedSHA` write site.** Push Job exits 0 only after `git push --force-with-lease` returns success; `PushReconciler` (or whichever up-stack reconciler is at the boundary) observes Job completion and patches `Status.git.lastPushedSHA` with the SHA from `EnvelopeOut` (the push Job emits its own envelope-shaped result on PVC). Planner finalizes patch site.
- **`internal/gitleaks` config customization.** Default rules embedded via `go:embed` from upstream gitleaks v8 config; per-Project override via ConfigMap mounted at `/etc/tide/gitleaks-config.toml`. Planner decides: ConfigMap is a top-level Helm value, or a `Project.Spec.git.leaksConfigRef` field, or both. Lean toward `Project.Spec.git.leaksConfigRef` for per-Project tunability.
- **`claude` CLI version pinning + image rebuild cadence.** STACK.md says `>= v2.1.139`. Pin to a specific minor (e.g., `2.1.139`) in `images/claude-subagent/Dockerfile`; reserve right to bump weekly. Planner picks exact pin; Helm chart image tag matches.
- **`internal/subagent/common/` shape.** Stream-event JSONL reader (used by `anthropic`, future `openai`, future `google`). Prompt-template loader (compiled-in via `go:embed` per the "compiled-in Go templates" commitment). Planner picks exact API; aim for "drop-in for next provider".
- **Worktree cleanup cadence.** Per-Task worktrees at `/workspace/worktrees/{task-uid}/` accumulate over a Project's lifetime; PVC could fill. Planner picks: GC on Task finalizer (delete worktree dir + `git worktree remove` from bare repo), or rely on Project deletion finalizer (Phase 1 CTRL-05). Lean toward Task-finalizer GC for shorter-lived disk footprint.
- **Prompt-template content.** The actual Go templates for `{milestone, phase, plan}` × `{planner, executor}` prompts. STACK.md commits to compiled-in (not vendored GSD Markdown). Researcher and planner draft initial templates referencing the README spec; user reviews drafts during plan-phase.

</decisions>

<canonical_refs>
## Canonical References

**Downstream agents MUST read these before planning or implementing Phase 3.**

### Spec & paradigm

- `README.md` — TIDE paradigm spec: five-level hierarchy, two-DAG split, **§"Failure handling at wave boundaries"** (FAIL-01 + PERSIST-04 contract intact), **§"Alternatives considered and rejected"** (no cycle recovery, no cached schedules).
- `CLAUDE.md` — Project instructions:
  - **Working Rules** (Observe First / Execute Don't Ask / Verify Before Claiming) — apply at every iteration.
  - **Operating Notes (Phase 02.2 lessons)** — 10 rules from the 12-plan cascade chain. Plan 02.2-08/09/10 framings that turned out wrong are encoded here.
  - **Technology Stack** — Go 1.26, controller-runtime v0.24.x, `go-git/v5`, Claude Code CLI ≥ v2.1.139, OpenInference attributes on OTel spans (Phase 4 work, but Phase 3 raw-event preservation feeds it).
  - **Anti-patterns** — "Don't mount host `~/.claude/`", "All Anthropic-specific code lives behind the Subagent interface in `internal/subagent/anthropic/`", "Don't accept a wave list as CRD input — only a DAG".

### Project frame

- `.planning/PROJECT.md` — Vision, 13 locked Key Decisions; Phase 1 + Phase 2 entries marked ✓ Validated.
- `.planning/REQUIREMENTS.md` — 10 Phase 3 REQ-IDs: `ART-02`, `ART-03`, `ART-04`, `ART-05`, `ART-06`, `ART-07`, `AUTH-01`, `PERSIST-04`, `TEST-03`, `TEST-04`. Traceability table maps each to specific decision codes here.
- `.planning/ROADMAP.md` **§"Phase 3: Up-Stack Reconcilers, Git Integration, Real Subagent, Resumption"** — Five success criteria; research-flag noted (`go-git` vs shell-out, RWX PVC driver matrix, per-run branch + `--force-with-lease`, Pitfall 13 coordination).
- `.planning/STATE.md` — Current cursor: Phase 02.2 COMPLETE (12 iterations, cascade chain empirically closed); 4/7 phases complete; next focus = Phase 3.

### Phase 1 + Phase 2 carry-forward (decisions that constrain Phase 3)

- `.planning/phases/01-foundation-crds-pkg-dag-controller-scaffold/01-CONTEXT.md`:
  - **D-A3** (API group `tideproject.k8s`) — Phase 3 schema additions stay under this group.
  - **D-B1..B3** (Wave-as-Kind ownership, Spec carries only `planRef + waveIndex`, cycle detection at Plan admission) — Phase 3 PlanReconciler materializes Wave CRDs from `EnvelopeOut.childCRDs`.
  - **D-C1..C2** (Standard reconciler stub depth, no `time.Sleep` in `Reconcile()`) — Phase 3 up-stack reconciler bodies inherit this discipline; push Job is the offload site for blocking I/O.
  - **D-F1..F2** (`Task.Spec.DependsOn` sibling-name strings; `Task.Spec.FilesTouched` required non-empty) — Phase 3's planner-emitted `ChildCRDSpec` for Task CRDs must satisfy these.
  - **D-D1** (POOL-03 analyzer + CI gate; `cmd/tide-lint` flipped to `multichecker` in Phase 2) — Phase 3 worktree-add code stays in `internal/harness` and `pkg/git`, behind the import firewall.
- `.planning/phases/02-dispatch-plan-validation-innermost-reconcilers-harness/02-CONTEXT.md`:
  - **D-A1** (`pkg/dispatch` public envelope contract) — Phase 3's `EnvelopeIn.Provider` + `EnvelopeIn.Role` + `EnvelopeIn.Level` + `EnvelopeOut.childCRDs` are **schema extensions to a public contract**. Naming + field ordering + JSON tag stability matter — break-or-extend decisions are versioned.
  - **D-A2** (envelopes are JSON files on PVC at `/workspace/envelopes/{task-uid}/{in,out}.json`) — Phase 3 inherits; raw event log lives alongside as `events.jsonl`.
  - **D-A4** (subagent pod ServiceAccount has zero K8s verbs) — Phase 3's planner subagents inherit; structured `EnvelopeOut.childCRDs` is the workaround.
  - **D-B1** (TaskReconciler is sole Job creator at task level) — Phase 3 extends pattern: each up-stack reconciler is sole Job creator at its level.
  - **D-B5** (deterministic Job names `tide-task-{task-uid}-{attempt-n}`) — Phase 3's planner Jobs follow the same pattern: `tide-{level}-{level-uid}-{attempt-n}` (e.g., `tide-milestone-{milestone-uid}-1`). Chaos-resume D-D1 relies on this for no-dup property.
  - **D-C1..C4** (sidecar credproxy + HMAC signed token + raw `ANTHROPIC_API_KEY` only on sidecar) — Phase 3's Claude subagent inherits the credproxy sidecar **verbatim**; only the main container image changes from stub to claude.
  - **D-D1..D4** (in-memory rate-limit bucket; `Project.Status.budget` rolled up at Task completion; bucket scope per credential Secret UID; bypass via annotation) — Phase 3's planner-Task usage rolls into the same `Project.Status.budget` infrastructure.
  - **D-E1..E4** (Plan admission cycle detection + file-touch reconciliation, strict/warn modes) — Phase 3's PlanReconciler still relies on this. Plans produced by Claude planner subagents go through the same webhook.
  - **D-F1..F4** (stub-subagent modes) — Phase 3 extends with new `wait-for-signal` mode (D-D3) for chaos-resume.
  - **D-G1..G3** (PVC layout, init Job, UIDs) — Phase 3 extends `/workspace/` layout with `repo.git/` + `worktrees/`.
  - **D-H1..H4** (test tier: Layer A envtest + Layer B kind) — Phase 3's chaos-resume runs in Layer B (D-D1).
- `internal/dispatch/podjob/backend.go` — Phase 2's `PodJobBackend`. Phase 3 reuses verbatim; only `EnvelopeIn` content changes (Provider field added).
- `internal/harness/` — Phase 2's caps + redact + signed-token client + output-path validation. Phase 3 keeps as the provider-agnostic core; `internal/subagent/anthropic/` calls `harness.Execute()` from its `Run()` method.
- `api/v1alpha1/task_types.go` — `TaskSpec` locked (Phase 1 D-F1). Phase 3's `ChildCRDSpec.Spec` for Task CRDs must satisfy `PlanRef`, `DependsOn`, `FilesTouched`, `PromptRef`.
- `api/v1alpha1/project_types.go` — Phase 3 extends `Spec` with `subagent.{image, model, levels}` + `git.{repoURL, credsSecretRef}`; extends `Status` with `git.{branchName, lastPushedSHA, leaseFailureCount}` and adds `phase=PushLeaseFailed` to the condition vocabulary.
- `cmd/manager/main.go` — Phase 1 + Phase 2 wiring. Phase 3 adds: `subagent` default config injection from Helm, `git` config injection, push Job image config injection.

### Research synthesis (Phase 3 is the integration fanout)

- `.planning/research/STACK.md`:
  - **Claude Code CLI ≥ v2.1.139** with `claude -p --output-format stream-json`; `ANTHROPIC_API_KEY` env from Secret; OAuth flow broken (claude-code#29983, #7100).
  - **`go-git/v5`** — HTTPS+PAT path is the smooth one; SSH host-key handling is fussy (Phase 3 ART-05 documents SSH caveats but defaults to HTTPS+PAT).
  - **"Vendoring `get-shit-done` Markdown" rejected** — prompts as compiled-in Go templates in the binary (D-C1 `internal/subagent/common/` prompt-template loader).
- `.planning/research/PITFALLS.md`:
  - **§"Pitfall 13: TIDE-orchestrated artifacts overwrite manual work mid-self-hosting"** — D-B6 per-run branch + `--force-with-lease` against `lastPushedSHA` is the structural mitigation.
  - **§"Pitfall 18: Secret leakage in artifacts and logs"** — D-B3 gitleaks embedded as Go library + Phase 2 D-C1..C4 credproxy on the harness side.
  - **§"Pitfall 7: subagent context bleed"** — Phase 2 HARN-05 output-path validation + Phase 3 D-B4 worktrees-per-Task (each Task confined to its own worktree dir).
- `.planning/research/ARCHITECTURE.md` — Component responsibilities; relevant patterns: Pattern 1 (one reconciler per Kind — Phase 3 keeps the count at 6, adds bodies), Pattern 2 (owner-ref cascade — Phase 3's `EnvelopeOut.childCRDs` materialization keeps the cascade).
- `.planning/research/FEATURES.md` — Phase 3-relevant feature classifications.

### External references (read on demand; don't pre-load)

- **Claude Code releases** — https://github.com/anthropics/claude-code/releases — pin to `v2.1.139` or newer; verify `--output-format stream-json` event shape stability.
- **Anthropic API streaming reference** — https://docs.anthropic.com/api/messages-streaming — for stream-json event types the harness parses (D-C5).
- **`go-git/v5` docs** — https://pkg.go.dev/github.com/go-git/go-git/v5 — HTTP basic auth (PAT), `--force-with-lease` via `RefSpec` + `Force` + lease impl (manual lease check pre-push if v5 lacks first-class support; verify during research).
- **`go-git/v5` worktrees** — https://pkg.go.dev/github.com/go-git/go-git/v5#Worktree — bare-repo + per-task worktree dir mechanics (D-B4).
- **gitleaks v8** — https://github.com/gitleaks/gitleaks — `detect` package as Go library; default rule TOML; `--config` override path.
- **controller-runtime leader-election** — https://pkg.go.dev/sigs.k8s.io/controller-runtime/pkg/manager#Options — `LeaseDuration`, `RenewDeadline`, `RetryPeriod` knobs for chaos-resume D-D1 failover budget.
- **K8s Job retry semantics** — Phase 3's chaos-resume D-D4 pillar #2 (Attempt counter unchanged) relies on the new leader not bumping `Task.Status.Attempt` for in-flight Tasks. Phase 2's TaskReconciler is the reference.

</canonical_refs>

<code_context>
## Existing Code Insights

### Reusable Assets

- **`pkg/dispatch`** (Phase 2) — `EnvelopeIn / EnvelopeOut / Subagent / Dispatcher / Errors`. Phase 3 extends `EnvelopeIn` with `Provider`, `Role`, `Level` fields and `EnvelopeOut` with `childCRDs`, `git.headSHA`. Schema bump is additive (v1alpha1 envelope schema rev `1` → `2`); harness rejects unknown rev (Phase 2 D-A3).
- **`internal/harness/`** (Phase 2) — cap enforcement, signed-token client, secret redaction, output-path validation. Phase 3 reuses verbatim as the provider-agnostic core; `internal/subagent/anthropic/` calls `harness.Execute()` from its `Run()` method.
- **`internal/harness/redact/`** (Phase 2 D-F4) — known-pattern detector with `sk-ant-*`, `sk-*`, `gh[ps]_*`, `xox[abp]-*`, `AKIA*`, JWT patterns. gitleaks v8 in Phase 3 (D-B3) is the push-side analog; redact is the harness-side first line; gitleaks is the second line of defense at git-push.
- **`internal/dispatch/podjob`** (Phase 2) — `PodJobBackend` creates Jobs, watches for completion, reads `EnvelopeOut` from PVC. Phase 3 reuses verbatim; only the dispatching reconciler changes (up-stack levels call it directly, not just `TaskReconciler`).
- **`internal/credproxy/`** (Phase 2) — HMAC-SHA256 signed-token validator + HTTPS proxy with self-signed cert. Phase 3's Claude subagent gets `ANTHROPIC_BASE_URL=https://127.0.0.1:8443` + `ANTHROPIC_API_KEY=<signed-token>` via the same sidecar pattern — **zero changes to credproxy** for Phase 3.
- **`internal/budget/`** (Phase 2) — token-bucket rate limiter + per-Project rollup. Phase 3's planner Tasks roll up usage into `Project.Status.budget` through the existing path.
- **`cmd/stub-subagent/`** (Phase 2) — Phase 3 extends with `wait-for-signal` mode (D-D3); no architectural change.
- **`pkg/dag.ComputeWaves`** (Phase 1) — Phase 3's chaos-resume D-D4 pillar #5 invokes this post-restart and compares against pre-restart golden file. PERSIST-03 "no cached schedule" is exactly the property D-D4 validates.
- **`api/v1alpha1/`** types — Phase 3 extends `Project.Spec` / `Project.Status` (D-B6, D-C2); other CRD schemas unchanged.
- **`cmd/tide-lint/`** with `crosspool` + `providerfirewall` analyzers (Phase 2) — provider-firewall already rejects `github.com/anthropics/*` under `pkg/controller/...`, `pkg/dispatch/...`, `pkg/dag/...`. Phase 3 places `internal/subagent/anthropic/` outside those firewalled boundaries (anthropic-specific code is FINE there); `pkg/git` and `cmd/tide-push/` stay free of LLM-SDK imports.
- **Helm chart pair `charts/{tide,tide-crds}`** — Phase 3 schema additions (Project.Spec.subagent, Project.Spec.git) flow through controller-gen → kustomize → helmify. New Helm values: `subagent.defaults.{image, model}`, `subagent.levels.{milestone,phase,plan,task}.model`, `images.claudeSubagent.repository`, `images.tidePush.repository`, `gitleaks.configMapName` (optional).

### Established Patterns

- **Per-level reconciler-is-sole-Job-creator** (Phase 2 D-B1, extended in Phase 3 D-A2). Each reconciler holds the dispatch responsibility for its level; no shared "DispatchService".
- **Deterministic Job names = dedup key** (Phase 2 D-B5, extended in Phase 3 D-B5 for push Jobs). `tide-push-{project-uid}` serializes pushes per Project naturally.
- **Status conditions vocabulary** (Phase 1 + Phase 2): `Pending`, `Ready`, `Reconciling`, `Failed`, `Running`, `Succeeded`, `BudgetExceeded`, `Validated`. Phase 3 adds: `PushLeaseFailed` (Project), `Cloned` (Project — when `pkg/git` clone Job succeeds), `AuthoringPlanner` (Milestone/Phase/Plan — when planner Job is dispatched but not yet complete).
- **In-memory state, re-derive from CRDs on restart** (Phase 1 PERSIST-01..03, Phase 2 D-D1 rate-limit bucket, Phase 3 chaos-resume D-D4 wave structure). The chaos-resume test is the existence proof for this property.
- **Idempotent Job creation via deterministic names** (Phase 2 SUB-03). Phase 3 extends to push Jobs (D-B5) and to per-level planner Jobs.
- **Helm augment-layer for new chart values** (Phase 2 D-G1 hack/helm/) — Phase 3's new Helm values plug in through the same seam.
- **Two-container Pod topology** (Phase 2 D-C1): credproxy sidecar + subagent main. Phase 3's Claude image swap-in keeps the topology; only the main container changes.

### Integration Points

- **Up-stack reconciler bodies** (`internal/controller/{milestone,phase,plan,project}_controller.go`). Phase 1 + 2 left these at Standard depth. Phase 3 fills:
  - `ProjectReconciler.Reconcile`: clone repo (separate clone Job, mirrors `tide-init` pattern) → dispatch milestone planner via `MilestoneReconciler` (cascade) → eventually push at Project completion.
  - `MilestoneReconciler.Reconcile`: acquire `plannerPool`, call `dispatch.Dispatcher.Run` with envelope role=planner / level=milestone, on completion materialize `Phase` CRDs from `EnvelopeOut.childCRDs`, fire push Job at Milestone boundary.
  - `PhaseReconciler.Reconcile`: same shape at phase level — author phase brief, materialize `Plan` CRDs.
  - `PlanReconciler.Reconcile`: same shape at plan level — author `PLAN.md`, materialize `Task` + `Wave` CRDs (Plan admission webhook from Phase 2 D-E1 validates the DAG before Wave materialization), fire push Job at Plan boundary.
  - `TaskReconciler.Reconcile` (Phase 2): mostly unchanged; harness now does worktree-setup before dispatching the subagent's `claude -p` invocation.
- **`pkg/dispatch` schema bump** — `EnvelopeIn.Provider`, `EnvelopeIn.Role`, `EnvelopeIn.Level`, `EnvelopeOut.childCRDs`, `EnvelopeOut.git.headSHA`. JSON tag stability + ordering documented; harness rejects unknown envelope `apiVersion`.
- **`cmd/manager/main.go`** — wires `pkg/git` + push image config + per-level subagent image+model defaults from Helm values into the orchestrator. Adds the per-Provider Subagent registry indexed by `EnvelopeIn.Provider.Vendor` (optional v1.x — for now, single-vendor binary works against the image-config-only path).
- **Plan admission webhook** (Phase 2 D-E1..E4) — unchanged in Phase 3. Plans authored by Claude planner subagents flow through the existing cycle-detection + file-touch reconciliation path. The webhook is a structural quality gate for LLM-authored DAGs.
- **`internal/controller/dispatch_helpers.go`** (new in Phase 3) — shared planner-dispatch logic: resolve Provider config from Project.Spec, stamp envelope, acquire `plannerPool`, call `Dispatcher.Run`, materialize child CRDs from `EnvelopeOut.childCRDs`. Three of the four up-stack reconcilers call this; helps prevent reconciler-body drift.

</code_context>

<specifics>
## Specific Ideas

- **Provider abstraction is the most load-bearing decision in Phase 3.** The user explicitly anchored on multi-vendor pluggability (Codex, Gemini, OpenCode, Grok, ...) — the `internal/subagent/{provider}/` layering (D-C1) and the orthogonal vendor+model axes (D-C2) are the structural commitment. Future provider additions are net-new directories + new Dockerfiles, no changes to `pkg/dispatch`, `internal/harness/`, or any reconciler. This is the contract that lets the user (or community) ship a new provider in v1.x without touching v1.0 code.
- **The `EnvelopeOut.childCRDs` block IS the planning artifact contract.** The Markdown artifacts (`MILESTONE.md`, phase brief, `PLAN.md`) are committed to the per-run branch for human review, but the structured envelope is what shapes the K8s state. Two outputs from one planner subagent run; the orchestrator validates they're consistent (the planner's responsibility) but treats the envelope as authoritative. The Markdown human-surface stays optional from a control-flow perspective — a planner subagent could in principle emit only the structured block and produce no Markdown — but the spec's "Artifacts as source of truth" principle (PROJECT.md context) and ROADMAP success criterion #4 ("committed artifacts with structured commit messages") require the Markdown.
- **Worktrees-per-Task is the right answer to parallel-executor file collisions, not `flock` on `.git/index`.** The user surfaced this question directly. The alternative (`flock` on the shared `.git/index` from each Task harness) is a smell — git already provides the worktree mechanism for exactly this. PVC layout extension is bounded (a per-Task subdir under `/workspace/worktrees/`) and Task-finalizer GC keeps it from accumulating.
- **Per-Project push serialization is the right answer to parallel-plan-completion races.** Push cost is bounded (artifact diff + gitleaks scan), so serializing per Project trades negligible latency for big complexity savings. The alternative (parallel pushes with worktree-based commits + lease-retry loops) would buy parallel-push throughput we don't need at v1 scale.
- **Claude Code CLI's `--output-format stream-json` is the parsing seam.** Phase 3 extracts only what budget (D-C5) needs and preserves the raw events for Phase 4 OpenInference attribute extraction. Don't be tempted to "just structure all the events while we're parsing them" — that doubles Phase 3 scope and breaks the Phase 4 boundary cleanly drawn by REQUIREMENTS.md OBS-03..05 (Phase 4 only).
- **Chaos-resume's four-pillar assertion is a single integration test, not a constellation of small tests.** The test exists to prove the property "resumption state = indegree map + completed-task set, nothing more" from the spec's resumption-state contract. Splitting it into many tiny tests would obscure the load-bearing claim.
- **Per-run branch naming uses Unix epoch, not RFC3339.** Refnames can't contain `:`. The user didn't push back on this; lock it.
- **The `wait-for-signal` stub mode is dual-purpose.** Primary purpose: chaos-resume harness (D-D3). Secondary purpose: any future test that needs to pin a Task at `Running` long enough to inject a kill, a network partition, an annotation change, etc. The mode is stable test infrastructure.
- **`Project.Spec.subagent.levels.{level}.params` is the per-vendor tuning escape hatch.** `Params map[string]string` is intentionally untyped — it absorbs vendor-specific knobs (Anthropic thinking-budget, OpenAI temperature, etc.) without forcing a CRD schema bump every time a new provider feature lands. Validation lives in the provider's Subagent impl (`internal/subagent/anthropic/` rejects unknown params at startup).

</specifics>

<deferred>
## Deferred Ideas

- **Per-level cross-vendor image overrides** (`Project.Spec.subagent.levels.{level}.image`). D-C2 commits to per-level model overrides in v1.0; per-level image (vendor) overrides are wired in the same schema but deferred to v1.x. v1.0 ships single-vendor-per-Project.
- **`internal/subagent/openai/`, `internal/subagent/google/`, `internal/subagent/xai/`, `internal/subagent/opencode/`** — v1.x or community contributions. The layering pattern (D-C1) is the v1.0 commitment; additional providers are net-new directories that don't touch Phase 3 core.
- **Per-host PR creation surfaces** (GitHub, GitLab, Gitea PR-create APIs). REQUIREMENTS.md "Deferred" entry. Phase 3's `pkg/git` is plain `git push` only; humans open PRs in v1.
- **Full stream-json structured-events `EnvelopeOut.events`** — Phase 4 work. Phase 3's raw `events.jsonl` is the input to Phase 4's OpenInference parsing; structured-events extraction lives in Phase 4 `pkg/otelai`.
- **OpenInference attribute names on OTel spans** — Phase 4 OBS-03..05.
- **`tide` CLI verbs** (`tide approve --force-push`, `tide retry-push`, `tide describe-budget`) — Phase 4 CLI-01..04. Phase 3 uses `kubectl annotate` for bypass.
- **Per-level human gate policy** — Phase 4 GATE-01..03. Phase 3 has the unconditional auto-advance path only (every level boundary transitions automatically once child CRDs are all Succeeded).
- **Live E2E nightly test against real Claude on real fixture repo** — Phase 3 REQ-TEST-03. The plan-phase researcher should treat TEST-03 as a separate plan from chaos-resume; live cost is bounded by Phase 2 budget cap infrastructure but the test fixture (which fixture repo? Snapshotted at which SHA?) is a separable design surface.
- **SSH auth path for `pkg/git`** — ART-05 says "SSH is supported but documented with host-key caveats". Phase 3 plans the HTTPS+PAT default; SSH wiring can land in the same plan family but is lower-priority. Phase 3 docs should note the SSH host-key caveat.
- **RWX PVC driver matrix testing** — research-flag item from ROADMAP. Phase 3 verifies against kind's default RWX (likely `csi-driver-nfs` or Longhorn — planner picks). EFS / Filestore / Azure Files matrix testing is documentation only in v1; community-verified.
- **Conversion-webhook activation** — CRD-05 scaffold from Phase 1 stays a no-op through v1.0. v1.x `v1beta1` adoption activates it.
- **gRPC streaming subagent contract** — v2+ per REQUIREMENTS.md "Deferred". Phase 3's PodJobBackend + envelope-file contract stays the v1 concrete impl; streaming is additive behind the same `Subagent` interface.
- **Multi-cluster dispatch / Kueue integration** — v1.x / v2+ per REQUIREMENTS.md "Deferred".
- **Dashboard mutation actions** (retry-push, edit-plan, pause-resume) — v1 dashboard is read-only per Phase 4 DASH-05 + PROJECT.md "Out of Scope". Phase 3 surfaces all halt-and-resume via annotations.

</deferred>

---

*Phase: 03-up-stack-reconcilers-git-integration-real-subagent-resumptio*
*Context gathered: 2026-05-15*
