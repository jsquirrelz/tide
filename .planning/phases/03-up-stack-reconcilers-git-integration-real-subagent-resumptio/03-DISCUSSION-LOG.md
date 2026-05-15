# Phase 3: Up-Stack Reconcilers, Git Integration, Real Subagent, Resumption - Discussion Log

> **Audit trail only.** Do not use as input to planning, research, or execution agents.
> Decisions are captured in `03-CONTEXT.md` — this log preserves the alternatives considered.

**Date:** 2026-05-15
**Phase:** 03-up-stack-reconcilers-git-integration-real-subagent-resumptio
**Areas discussed:** Up-stack dispatch & CRD materialization; pkg/git invocation site + push cadence + gitleaks; Real Claude image swap-in (HARN-06); Chaos-resume test design (PERSIST-04)

---

## Up-stack dispatch & CRD materialization

### Q1 — Authoritative source for child-CRD materialization

| Option | Description | Selected |
|--------|-------------|----------|
| Structured EnvelopeOut spec | Subagent writes Markdown artifact AND structured `EnvelopeOut.childCRDs: [{kind, name, spec}]` block. Orchestrator parses typed Go envelope (no Markdown parsing) and server-side-creates child CRDs. Preserves Phase 2 D-A4 zero-K8s-verbs subagent. | ✓ |
| Markdown parser in orchestrator | PhaseReconciler / PlanReconciler / MilestoneReconciler each contain a Go parser for the Markdown artifact. Single source of truth = Markdown. Schema risk: parser must track artifact format drift. | |
| Subagent kubectl-applies child CRDs | Subagent has K8s verbs and applies child CRDs directly from inside the Job pod. Breaks Phase 2 D-A4 zero-verb commitment. | |
| Hybrid: envelope authoritative, Markdown is artifact | Same as Option 1 with explicit framing. | |

**User's choice:** Structured EnvelopeOut spec
**Notes:** Locks the "envelope is the contract, Markdown is the human surface" framing. Codified as D-A1 in CONTEXT.md.

### Q2 — Who creates the planner Job at each up-stack level

| Option | Description | Selected |
|--------|-------------|----------|
| Each reconciler dispatches its own | MilestoneReconciler creates MILESTONE.md-authoring Job; PhaseReconciler creates phase-brief Job; PlanReconciler creates PLAN.md-authoring Job; TaskReconciler keeps creating executor Jobs (Phase 2 D-B1). All four reconcilers call `dispatch.Dispatcher.Run` directly. | ✓ |
| Generalize Task CRD with Role+Level fields | Add `Spec.Role: planner\|executor` and `Spec.Level: milestone\|phase\|plan\|task` to Task CRD. Up-stack reconcilers create role=planner Tasks. TaskReconciler stays sole Job creator. | |
| New PlannerRun CRD + new reconciler | Introduce `PlannerRun` CRD owned by Milestone/Phase/Plan; a new `PlannerRunReconciler` dispatches all planner Jobs uniformly. Adds a 7th CRD. | |

**User's choice:** Each reconciler dispatches its own
**Notes:** Symmetric to Phase 2's TaskReconciler pattern; no new CRDs; four total dispatch sites. Codified as D-A2.

### Q3 — Subagent interface preservation

| Option | Description | Selected |
|--------|-------------|----------|
| Single Subagent interface, Role in envelope | `Subagent.Run(ctx, EnvelopeIn) (EnvelopeOut, error)` stays as Phase 2 locked. `EnvelopeIn.Role + EnvelopeIn.Level` drive prompt selection inside subagent harness. Public contract unchanged. | ✓ |
| Add Subagent.RunPlanner / Subagent.RunExecutor | Two methods on the same interface; envelope types diverge. Types stronger; public-contract surface grows. | |
| Separate dispatch.Planner and dispatch.Executor interfaces | Two interfaces, two backends. Strongest type separation; breaks Phase 2 D-A1 public-contract stability. | |

**User's choice:** Single Subagent interface, Role in envelope
**Notes:** Preserves Phase 2 D-A1 verbatim. Codified as D-A3.

### Q4 — Pool semaphore acquisition site

| Option | Description | Selected |
|--------|-------------|----------|
| Reconciler caller acquires its pool | Up-stack reconcilers acquire `plannerPool`; TaskReconciler acquires `executorPool`. Pool semantics at the caller; matches spec's two-pool argument. POOL-03 analyzer rejects cross-pool wait. | ✓ |
| Dispatcher routes via EnvelopeIn.Role field | Dispatcher reads `EnvelopeIn.Role` and acquires pool based on envelope content. Single caller-path; pool acquisition moves into shared dispatch code. | |

**User's choice:** Reconciler caller acquires its pool
**Notes:** Codified as D-A4.

---

## pkg/git invocation site, push cadence, gitleaks

### Q1 — Where pkg/git physically runs

| Option | Description | Selected |
|--------|-------------|----------|
| Dedicated push Job per level boundary | Reconciler spawns `tide-push-{level}-{uid}` Job that mounts git Secret, runs go-git/v5 + gitleaks, exits 0/non-zero. Mirrors Phase 2 D-G1 `tide-init` pattern. Git Secret scoped to push Job only. | ✓ |
| In-process in controller pod | Controller pod holds git Secret; reconciler calls go-git/v5 inline. Lower latency; enlarges controller's secret surface; goroutine + event channel needed for non-blocking. | |
| Hybrid: push in-process, gitleaks in Job | Controller pod does go-git push; gitleaks runs as pre-push Job. Splits secret surface from CPU surface; two failure modes per push. | |

**User's choice:** Dedicated push Job per level boundary
**Notes:** Codified as D-B1.

### Q2 — Push cadence

| Option | Description | Selected |
|--------|-------------|----------|
| Level boundaries only: 4 push points | Push fires on: Plan all-Tasks-Succeeded, Phase all-Plans-done, Milestone all-Phases-done, Project Status=Complete. Per-Task diffs accumulate on PVC and ship at Plan boundary. | ✓ |
| Level boundaries + per-Task push | Same 4 boundary pushes PLUS one push per Task completion. Finer-grained git history; push count proportional to Task count; --force-with-lease churn higher. | |
| Only at Project completion | All artifacts accumulate until Project Status=Complete, then one push. Simplest; no intermediate review; contradicts ROADMAP success criterion #4. | |

**User's choice:** Level boundaries only: 4 push points
**Notes:** Codified as D-B2.

### Q3 — Gitleaks integration

| Option | Description | Selected |
|--------|-------------|----------|
| Embed as Go library | Push Job's Go binary imports `github.com/zricethezav/gitleaks/v8/detect`. Single-binary push image; matches `go-git` pure-Go ethos. | ✓ |
| Shell out to gitleaks binary in image | Push Job image has both Go push binary and upstream `gitleaks` binary. Dual deps in image; subprocess error parsing fragility. | |
| gitleaks initContainer with official image | Push Job pod has init container `zricethezav/gitleaks:v8.x`. Cleanest separation; shared-volume artifact handoff between containers. | |

**User's choice:** Embed as Go library
**Notes:** Codified as D-B3.

### Q4 — Per-run branch lifetime + lease semantics (with worktree question raised)

| Option | Description | Selected |
|--------|-------------|----------|
| One branch per Project lifetime, lease against `Project.Status.git.lastPushedSHA` | Branch name `tide/run-<project-name>-<unix-timestamp>` fixed at Project creation. Subsequent pushes use `--force-with-lease=refs/heads/<branch>:<lastPushedSHA>`. Lease fail → `PushLeaseFailed` Condition. | ✓ (after worktree follow-up) |
| One branch per orchestrator-session | New branch each time TIDE's leader restarts. Branch churn proportional to leader-election events. | |
| Branch per Milestone | Each Milestone gets its own branch. Multi-milestone Projects produce multiple branches. | |

**User's first response:** "Won't worktrees need to be used for parallel execution?"
**Claude's response:** Acknowledged two distinct collision points:
- **Collision A** (parallel executor Tasks editing the repo): worktrees-per-Task is the right answer; alternative `flock` on `.git/index` is a smell.
- **Collision B** (parallel push Jobs at level boundaries): serialize push Jobs per Project; push cost is small, parallel pushes adds complexity for marginal latency win.

These were formalized as **D-B4 (worktrees-per-Task)** and **D-B5 (serialize push Jobs per Project)** in CONTEXT.md. The per-run branch question was then re-asked with serialization locked.

**User's final choice (re-asked question):** One branch per Project lifetime, lease against `Project.Status.git.lastPushedSHA`
**Notes:** Codified as D-B6.

---

## Real Claude image swap-in (HARN-06)

### Q1 — Image structure (asked with Claude-only framing; user reframed)

| Option | Description | Selected |
|--------|-------------|----------|
| Separate images, shared internal/harness library | `tide-stub-subagent` (Phase 2) and `tide-claude-subagent` (Phase 3). Both link `internal/harness/` library. Independent image lifecycles. | (reframed) |
| Single image with --mode=stub\|claude flag | One `tide-subagent` image bundling stub Go code AND claude CLI + Node runtime. Single Dockerfile. | |
| Same Dockerfile, build-arg-driven tags | One Dockerfile with `ARG INCLUDE_CLAUDE=false`. | |

**User's first response:** "Thinking generically if we want to eventually extend to Codex, Gemini, OpenCode, Grok, etc., what would the right pattern be?"
**Claude's response:** Reframed as a **provider-pluggable layering** pattern:
- `pkg/dispatch/` (public Subagent interface + EnvelopeIn/Out)
- `internal/harness/` (provider-agnostic core)
- `internal/subagent/{provider}/` (provider-specific Subagent impls — anthropic, openai, google, xai, opencode, ...)
- `internal/subagent/common/` (shared building blocks)
- `cmd/{provider}-subagent/main.go` (thin ~50 LOC shims)
- `images/{provider}-subagent/Dockerfile` (per-provider Dockerfiles)

Plus orchestrator-side selection model: `Project.Spec.subagentImage` default with optional per-level overrides; Phase 3 ships Anthropic only; future providers add net-new directories without touching Phase 3 core.

**User's confirmation:** "Confirm" — but also raised the model-axis question.
**Codified as:** D-C1 (layering) in CONTEXT.md.

### Q1.5 — Vendor + model orthogonality (user-raised follow-up)

**User's question:** "For the per-level override, we'd still want to define the model within the Anthropic ecosystem (Opus, Sonnet, Haiku, etc.). Does this pattern support vendor + model choice?"

**Claude's response:** Yes — model is its own axis, orthogonal to vendor (image). Proposed:
- **CRD shape:** `Project.Spec.subagent.{image, model, levels.{milestone,phase,plan,task}.{image, model, params}}` with resolution chain → Helm defaults.
- **Envelope schema bump:** `EnvelopeIn.Provider {Vendor, Model, Params}` (extends Phase 2 D-A4).
- **Helm defaults:** Opus@milestone, Sonnet@phase/plan, Haiku@task — matches spec's "planning fans out wide, execution fans out narrow".
- **Provider impl reads:** `Provider.Model` → `claude -p --model <model> --output-format stream-json`; vendor-mismatch fail-fast.

**User's confirmation:** "Confirm"
**Codified as:** D-C2 (CRD shape), D-C3 (envelope contract), D-C4 (Helm defaults) in CONTEXT.md.

### Q2 — Stream-json parsing depth

| Option | Description | Selected |
|--------|-------------|----------|
| Minimal parse: usage + final result, raw events to PVC | Harness extracts `usage.{inputTokens, outputTokens, cacheReadTokens, cacheCreationTokens}` + final assistant-message text. Raw events at `/workspace/envelopes/{task-uid}/events.jsonl`. Phase 4 parses raw log for OpenInference. | ✓ |
| Full parse: structured events in EnvelopeOut | Harness parses every event type into typed Go structs; `EnvelopeOut.events: []TypedEvent`. Front-loads Phase 4 work; EnvelopeOut grows large per Task. | |
| Passthrough: raw events to PVC, exit code in envelope | Harness writes raw stream-json only; EnvelopeOut has just exit code + completedAt. Phase 2 D-D2 budget tally cannot fully passthrough. | |

**User's choice:** Minimal parse: usage + final result, raw events to PVC
**Notes:** Codified as D-C5.

---

## Chaos-resume test design (PERSIST-04 / TEST-04)

### Q1 — Test layer

| Option | Description | Selected |
|--------|-------------|----------|
| Layer B kind — real Job lifecycle + real leader election | `kubectl delete pod -l app=tide-controller-manager` mid-wave; leader-election lease elapses; new pod takes over. Real Jobs persist across pod kill. Test wall-clock ~60s. | ✓ |
| Layer A envtest — in-process Manager restart | Mock JobBackend records dispatch calls. Faster (<10s); no real Jobs; doesn't exercise real leader-election failover. | |
| Both — Layer A fast feedback + Layer B canonical | Layer A version proves algorithmic invariants; Layer B proves runtime invariants. Doubles test surface. | |

**User's choice:** Layer B kind — real Job lifecycle + real leader election
**Notes:** Codified as D-D1.

### Q2 — Pre-kill fixture state

| Option | Description | Selected |
|--------|-------------|----------|
| Mixed: 1 Succeeded + 1 Running + 1 Pending-with-dep | T-α (success-fast) Succeeded; T-β (hang) Running; T-γ (depends_on=[α], hang) Running. All three lifecycle states. Strongest assertions. | ✓ |
| All Running — simpler fixture | T-α + T-β + T-γ all no-deps, all hang. Tests no-dup-dispatch only. | |
| Two-wave fixture: wave-1 Succeeded + wave-2 Running | Wave 1 = α (success). Wave 2 = β, γ (depends_on=[α], hang). Tests cross-wave resumption; broader scope; more fixture complexity. | |

**User's choice:** Mixed: 1 Succeeded + 1 Running + 1 Pending-with-dep
**Notes:** Codified as D-D2.

### Q3 — Hang-release mechanism

| Option | Description | Selected |
|--------|-------------|----------|
| New stub mode `wait-for-signal` (PVC file polling) | Extends Phase 2 D-F3 stub modes. Stub polls `/workspace/envelopes/{task-uid}/release` every 500ms; on file appearance writes canned success envelope. Test fixture creates release files post-restart. | ✓ |
| Existing `hang` mode + short wallClockSeconds + cap-hit | Use Phase 2 `hang` mode. β/γ hit harness cap after restart; verified as Failed-due-to-cap. Mixes chaos-resume with cap-hit testing. | |
| Existing `hang` + cap-hit + a sibling success-after-resume Task | Add T-δ depending on T-γ. β/γ cap-hit Failed; δ stays Pending forever. Scope creeps into FAIL-01 testing. | |

**User's choice:** New stub mode `wait-for-signal`
**Notes:** Codified as D-D3.

### Q4 — Assertion shape

| Option | Description | Selected |
|--------|-------------|----------|
| All four pillars + algorithmic invariant | (1) Job UID continuity, (2) Attempt counter unchanged, (3) α stays Succeeded, (4) post-release all Tasks Succeeded + Wave Succeeded, (5) `pkg/dag.ComputeWaves` identical wave structure (golden-file). | ✓ |
| Minimal: Job count + Wave final state only | Pre-kill: 3 Jobs. Post-restart + release: still 3 Jobs, Wave Succeeded. Simpler; misses indegree-recompute and Attempt-counter checks. | |
| Spec-driven: criterion #5 wording verbatim + 2 invariants | Translate ROADMAP success criterion #5 into assertion bullets verbatim. Tight mapping to criterion text. | |

**User's choice:** All four pillars + algorithmic invariant
**Notes:** Codified as D-D4.

---

## Claude's Discretion

Areas where the planner has flexibility (all listed in `03-CONTEXT.md` Implementation Decisions §"Claude's Discretion"):

- Push Job RBAC scope (dedicated SA vs reused `tide-orchestrator` SA)
- Leader-election lease tuning for chaos-resume (controller-runtime v0.24 defaults; adjust if test flakiness)
- `pkg/git` API surface (specific function signatures; future GitLab/Gitea adapter seams)
- `Project.Status.git.lastPushedSHA` write site (which reconciler patches; envelope shape from push Job)
- `internal/gitleaks` config customization (ConfigMap top-level Helm value vs `Project.Spec.git.leaksConfigRef` field)
- `claude` CLI version pinning + image rebuild cadence (specific pin; weekly bump policy)
- `internal/subagent/common/` shape (stream-event reader, prompt-template loader APIs)
- Worktree cleanup cadence (Task finalizer GC vs Project finalizer reliance)
- Prompt-template content (Go template draft drafted by researcher/planner; user reviews)

## Deferred Ideas

Captured in `03-CONTEXT.md` §"Deferred Ideas":

- Per-level cross-vendor image overrides (v1.x)
- `internal/subagent/openai/`, `google/`, `xai/`, `opencode/` (v1.x or community)
- Per-host PR creation surfaces (v2+)
- Full stream-json structured events in EnvelopeOut (Phase 4)
- OpenInference attribute names on OTel spans (Phase 4 OBS-03..05)
- `tide` CLI verbs (`tide approve --force-push`, `tide retry-push`, etc.) (Phase 4)
- Per-level human gate policy (Phase 4 GATE-01..03)
- Live E2E nightly test against real Claude (Phase 3 REQ-TEST-03 — separate plan, separate fixture design)
- SSH auth path for `pkg/git` (Phase 3 docs scope only; HTTPS+PAT is default)
- RWX PVC driver matrix testing (community-verified beyond kind default)
- Conversion-webhook activation (v1.x `v1beta1` adoption)
- gRPC streaming subagent contract (v2+)
- Multi-cluster dispatch / Kueue integration (v1.x / v2+)
- Dashboard mutation actions (v1 read-only per DASH-05)
