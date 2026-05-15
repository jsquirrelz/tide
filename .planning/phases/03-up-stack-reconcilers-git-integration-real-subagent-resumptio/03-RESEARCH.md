# Phase 3: Up-Stack Reconcilers, Git Integration, Real Subagent, Resumption — Research

**Researched:** 2026-05-15
**Domain:** Kubernetes operator integration fanout — up-stack planner reconcilers + pure-Go git client (HTTPS+PAT, gitleaks-on-diff, per-run branches, --force-with-lease) + real Claude-Code-backed subagent image (stream-json parsing) + chaos-resume integration test (controller-runtime leader election + deterministic Job names)
**Overall confidence:** HIGH for go-git/v5 API surface, gitleaks/v8 detect API, Claude Code CLI stream-json schema, Anthropic Go SDK Usage shape, controller-runtime leader-election parameters. MEDIUM for RWX-on-kind (multiple viable drivers, planner picks one). LOW only where flagged inline.

---

<phase_requirements>
## Phase Requirements

| ID | Description (from REQUIREMENTS.md) | Research Support |
|----|-----------------------------------|------------------|
| **ART-02** | Helm chart leaves `storageClassName` empty so cluster operators choose RWX driver; docs enumerate matrix | §"RWX PVC Drivers on kind" — Phase 2 chart already ships empty `storageClassName` + overridable `accessModes`; Phase 3 ships the documentation matrix (NFS-CSI + Longhorn + EFS/Filestore/Azure Files) |
| **ART-03** | `pkg/git` (using `go-git/v5`) pushes artifacts at every level boundary | §"go-git/v5 Surface Area" — `git.PlainClone` + `Worktree.Add/Commit` + `Repository.Push` covers the cycle; v5.19.0 has first-class `PushOptions.ForceWithLease` |
| **ART-04** | Git pushes happen from the orchestrator (push Job mounting per-Project PVC), not subagent pods | §"Push Job topology" — mirrors Phase 2 D-G1 `tide-init` pattern; D-B1 + D-B5 commit to dedicated `tide-push-{project-uid}` Job, serialized per Project |
| **ART-05** | HTTPS+PAT default (host-agnostic); SSH supported but documented with host-key caveats | §"go-git HTTPS+PAT path" — `&http.BasicAuth{Username: "x-access-token", Password: PAT}` works against GitHub/GitLab/Gitea (generic HTTPS); SSH is deferred per CONTEXT.md "Deferred Ideas" |
| **ART-06** | Per-run branches `tide/run-<project>-<timestamp>`, `--force-with-lease`, never `main` | §"Per-run branch + --force-with-lease integration" — D-B6 locks the lease semantics; `PushOptions.ForceWithLease` is the Go-side seam |
| **ART-07** | `gitleaks` at every push, `tide_secret_leak_blocked_total` Prometheus counter | §"gitleaks/v8/detect Go library" — `detect.NewDetectorDefaultConfig()` + `detector.DetectString(diffContent)` is the seam; counter wired in push Job's reconciler-observed exit path |
| **AUTH-01** | LLM API key + git creds as K8s `Secret` resources; Project CRD references via `secretRef` fields | §"Project.Spec.git.credsSecretRef" — `envFrom: secretRef` on push Job container only (mirrors Phase 2 D-C4 LLM key pattern) |
| **PERSIST-04** | `chaos-resume` integration test kills orchestrator mid-wave; new leader resumes from CRD status + PVC only | §"Chaos-resume pattern" — Phase 1 `TestLeaderElection` (lease HolderIdentity changes) is the precedent; D-D1..D4 lock the test design |
| **TEST-03** | Nightly live E2E with real Claude-backed Project on fixture repo, budget-capped | §"Real Claude image swap" — `images/claude-subagent/Dockerfile` with `@anthropic-ai/claude-code@2.1.142` pin; budget cap inherited from Phase 2 D-D2 |
| **TEST-04** | `chaos-resume` runs in integration tier (Layer B kind) | §"Chaos-resume pattern" — D-D1 commits Layer B kind tier; ~60s wall-clock budget under controller-runtime defaults |

</phase_requirements>

<user_constraints>
## User Constraints (from CONTEXT.md)

### Locked Decisions

**Up-stack dispatch & CRD materialization:**

- **D-A1:** Planner subagents emit **two parallel outputs** at every level: (1) human-reviewable Markdown (`MILESTONE.md` / phase brief / `PLAN.md`) committed to the per-run branch for review, and (2) a structured `EnvelopeOut.childCRDs []ChildCRDSpec` typed block consumed by the orchestrator. The structured block is the **authoritative source** for materializing child CRDs server-side; the orchestrator never parses Markdown. `ChildCRDSpec` shape is typed in `pkg/dispatch`: `{Kind: string, Name: string, Spec: runtime.RawExtension}`.
- **D-A2:** Each up-stack reconciler dispatches its own planner Job directly via `dispatch.Dispatcher.Run`: `MilestoneReconciler` → `MILESTONE.md` Job; `PhaseReconciler` → phase-brief Job; `PlanReconciler` → `PLAN.md` Job; `TaskReconciler` continues creating executor Jobs (Phase 2 D-B1). Four dispatch sites total.
- **D-A3:** `pkg/dispatch.Subagent.Run(ctx, EnvelopeIn) (EnvelopeOut, error)` interface (Phase 2 D-A1) preserved **verbatim**. `EnvelopeIn` gains `Role: planner|executor` + `Level: milestone|phase|plan|task` (already present in `pkg/dispatch/envelope.go` lines 36–41).
- **D-A4:** Pool semaphore acquisition lives at the **calling reconciler**, not in the dispatcher. Up-stack reconcilers acquire `plannerPool` (default size 16); `TaskReconciler` acquires `executorPool` (default size 4). POOL-03 analyzer continues to protect both sites.

**pkg/git invocation site, push cadence, gitleaks:**

- **D-B1:** Dedicated `tide-push-{project-uid}` Job per level boundary push. Mirrors Phase 2 D-G1 `tide-init` pattern. Push Job mounts (a) per-Project shared PVC at `/workspace`, (b) `Project.Spec.git.credsSecretRef` Secret as `envFrom` (HTTPS PAT only on push Job pod, never on controller).
- **D-B2:** Push cadence = **level boundaries only**. Four push points: (1) `Plan` all-Tasks-Succeeded, (2) `Phase` all-Plans-done, (3) `Milestone` all-Phases-done, (4) `Project.Status.phase==Complete`. Per-Task artifact diffs accumulate on PVC and ship together at Plan boundary.
- **D-B3:** gitleaks embedded as a **Go library** via `github.com/zricethezav/gitleaks/v8/detect`. Single-binary `images/tide-push/`. Default rule set compiled in via `go:embed`; per-Project overrides via ConfigMap at `/etc/tide/gitleaks-config.toml`. On match: push Job exits non-zero; reconciler increments `tide_secret_leak_blocked_total`.
- **D-B4:** **Worktrees-per-Task for executor parallelism.** Each executor Task harness runs `git worktree add /workspace/worktrees/{task-uid}` rooted at shared bare `/workspace/repo.git`. Per-task index; no `.git/index` race. PVC layout:
  ```
  /workspace/repo.git/                # shared bare clone (orchestrator-created at Project init)
  /workspace/worktrees/{task-uid}/    # per-Task working tree (harness setup)
  /workspace/artifacts/M-N/P-N/L-N/   # Phase 2 D-G2 layout, unchanged
  /workspace/envelopes/{task-uid}/    # Phase 2 D-A2 layout, unchanged
  ```
- **D-B5:** **Serialize push Jobs per Project.** At most one push Job per Project active at any time. Deterministic name `tide-push-{project-uid}` (Project UID alone, no level suffix); a second push attempt while the first is active hits K8s API `AlreadyExists`, calling reconciler requeues.
- **D-B6:** **One branch per Project lifetime.** Branch name `tide/run-<project-name>-<unix-timestamp>` (Unix epoch keeps refnames colon-free). Stored in `Project.Status.git.branchName` at Project creation. Subsequent push uses `--force-with-lease=refs/heads/<branch>:<lastPushedSHA>`. Lease fail → push Job exits non-zero, reconciler sets `Project.Status.phase=PushLeaseFailed` Condition + halts. Manual recovery via `kubectl annotate project foo tideproject.k8s/bypass-push-lease=true`.

**Real Claude image swap-in (HARN-06) — pluggable provider pattern:**

- **D-C1:** **Provider-pluggable layering:**
  ```
  pkg/dispatch/                       # PUBLIC — Subagent interface + EnvelopeIn/Out (Phase 2 D-A1, stable)
  internal/harness/                   # PROVIDER-AGNOSTIC — caps, redact, signed-token client, output-path validate
  internal/subagent/{provider}/       # PROVIDER-SPECIFIC — implements dispatch.Subagent.Run()
    internal/subagent/anthropic/      #   Phase 3 ships this
    internal/subagent/common/         #   shared JSONL stream-event reader, prompt-template loader
  cmd/{provider}-subagent/main.go     # THIN SHIM (~50 LOC) — load EnvelopeIn from PVC, instantiate harness, run, write EnvelopeOut
  images/{provider}-subagent/Dockerfile  # per-provider image
  ```
- **D-C2:** **Vendor + model are orthogonal axes.** `Project.Spec.subagent.{image, model, levels.{milestone,phase,plan,task}.{image, model, params}}`. Resolution chain: per-level → Project default → Helm-chart default. Resolved values stamped into `EnvelopeIn.Provider.{Vendor, Model, Params}`.
- **D-C3:** `EnvelopeIn.Provider` schema (envelope schema bump):
  ```go
  type ProviderSpec struct {
    Vendor string            // "anthropic" | "openai" | "google" | ...
    Model  string            // "claude-opus-4-7" | "claude-sonnet-4-6" | ...
    Params map[string]string // per-vendor tuning passthrough
  }
  ```
  `internal/subagent/anthropic/` reads `Provider.Model` → passes to `claude -p --model <model> --output-format stream-json`. Sanity-checks `Provider.Vendor == "anthropic"` at startup.
- **D-C4:** **Helm-chart per-level model defaults:** milestone → `claude-opus-4-7`, phase → `claude-sonnet-4-6`, plan → `claude-sonnet-4-6`, task → `claude-haiku-4-5`.
- **D-C5:** **Minimal stream-json parse.** `internal/subagent/anthropic/` extracts `usage.{inputTokens, outputTokens, cacheReadTokens, cacheCreationTokens}` → `EnvelopeOut.usage`, final assistant-message text → `EnvelopeOut.result`. Raw event log preserved at `/workspace/envelopes/{task-uid}/events.jsonl` (size-capped via harness tail-buffer). Phase 4 OpenInference parsing reads the raw log.

**Chaos-resume test design (PERSIST-04 / TEST-04):**

- **D-D1:** **Layer B kind tier.** `make test-int` runs chaos-resume spec; `kubectl delete pod -l app=tide-controller-manager` mid-wave; leader-election lease window (~15s default) elapses; new pod takes over. Real Jobs persist across pod kill. Test wall-clock budget: ~60s.
- **D-D2:** **Mixed-state 3-task fixture:** T-α (`testMode=success`, ~1s, Succeeded pre-kill); T-β (`testMode=wait-for-signal`, no deps, Running pre-kill); T-γ (`testMode=wait-for-signal`, `depends_on=[T-α]`, Running pre-kill).
- **D-D3:** **New stub mode `wait-for-signal`.** Stub polls `/workspace/envelopes/{task-uid}/release` every 500ms; on file appearance writes canned success envelope + exits 0. Extends Phase 2 D-F3.
- **D-D4:** **Four-pillar Ginkgo assertion set:** (1) Job UID continuity for in-flight Tasks; (2) `Task.Status.Attempt` unchanged; (3) Completed-set preserved (T-α stays Succeeded with same `CompletedAt`); (4) Observed completion across kill — after signaling β/γ release, both reach Succeeded, Wave reaches Succeeded, exactly 3 Jobs `status.succeeded=1`. Plus algorithmic invariant (5): `pkg/dag.ComputeWaves` post-restart produces identical wave structure (golden-file comparison).

### Claude's Discretion

- **Push Job RBAC scope.** Dedicated `tide-push` SA with `secrets get` verb on `Project.Spec.git.credsSecretRef` only (lean to least-privilege).
- **Leader-election lease tuning for chaos-resume.** controller-runtime v0.24 defaults (15s lease + 10s renew + 2s retry) likely fine.
- **`pkg/git` API surface.** Provider-agnostic: `Clone`, `WorktreeAdd`, `Commit`, `Push` (with `ForceWithLease` option), `Fetch`. Planner picks exact signatures; keep generic enough for future `internal/git/{host}/` adapters.
- **`Project.Status.git.lastPushedSHA` write site.** Push Job emits its own envelope-shaped result on PVC; reconciler observes Job completion, patches Status. Planner finalizes patch site.
- **`internal/gitleaks` config customization.** Default rules embedded via `go:embed`; per-Project override via ConfigMap. Planner decides: top-level Helm value, `Project.Spec.git.leaksConfigRef` field, or both. Lean to `Project.Spec.git.leaksConfigRef`.
- **`claude` CLI version pin.** Pin to specific minor in Dockerfile; reserve right to bump weekly.
- **`internal/subagent/common/` shape.** Stream-event JSONL reader + prompt-template loader (compiled-in via `go:embed`). Planner picks exact API.
- **Worktree cleanup cadence.** Task-finalizer GC (delete worktree dir + `git worktree remove` from bare repo) preferred over Project-finalizer GC for shorter-lived disk footprint.
- **Prompt-template content.** The actual Go templates for `{milestone, phase, plan} × {planner, executor}` prompts. Researcher/planner draft initial templates; user reviews during plan-phase.

### Deferred Ideas (OUT OF SCOPE)

- Per-level cross-vendor image overrides (`Project.Spec.subagent.levels.{level}.image`) — v1.x or v2+.
- `internal/subagent/openai/`, `internal/subagent/google/`, `internal/subagent/xai/`, `internal/subagent/opencode/` — v1.x or community contributions.
- Per-host PR creation surfaces (GitHub/GitLab/Gitea PR-create APIs) — v2+.
- Full stream-json structured-events `EnvelopeOut.events` — Phase 4 work.
- OpenInference attribute names on OTel spans — Phase 4 OBS-03..05.
- `tide` CLI verbs (`tide approve --force-push`, `tide retry-push`, `tide describe-budget`) — Phase 4 CLI-01..04.
- Per-level human gate policy — Phase 4 GATE-01..03.
- Live E2E nightly test against real Claude on real fixture repo — TEST-03 is a separate plan from chaos-resume.
- SSH auth path for `pkg/git` — ART-05 doc-only; HTTPS+PAT is the v1 default.
- RWX PVC driver matrix testing — v1 docs only; community-verified.
- Conversion-webhook activation (CRD-05) — stays no-op through v1.0.
- gRPC streaming subagent contract — v2+.
- Multi-cluster dispatch / Kueue integration — v1.x / v2+.
- Dashboard mutation actions — v1 dashboard is read-only.

</user_constraints>

## Research Summary

Phase 3 is a four-track integration fanout, not a greenfield design. **The hard work is done in CONTEXT.md** — 19 D-decisions lock the dispatch contract bump, push-Job topology, branch-naming + lease semantics, provider-layering, and the chaos-resume four-pillar assertion set. Research's job is to verify the external library surfaces match the locked decisions and to flag the **two genuine open questions**: (1) **which RWX driver** the integration test fixture installs against kind (Phase 2 chart already supports both ReadWriteMany production-default and `--set accessModes={ReadWriteOnce}` test override; Phase 3 chaos-resume uses the existing fixture so this is potentially a non-issue if the multi-pod-on-one-PVC scenario stays within a single namespace's PVC), and (2) **which claude-code minor version** to pin against (v2.1.142 latest as of 2026-05-14; v2.1.139 is the STACK.md floor).

**Library surface findings — all green.** `go-git/v5` v5.19.0 has first-class `PushOptions.ForceWithLease *ForceWithLease` + worktree support; `&http.BasicAuth{Username, Password}` works against any HTTPS git remote (GitHub/GitLab/Gitea use the same pattern with PAT as `Password`). `github.com/zricethezav/gitleaks/v8/detect` (v8.30.1 latest) provides `NewDetectorDefaultConfig()` + `DetectString(content)` — exactly the scan-the-diff seam Phase 3 needs. The `claude -p --output-format stream-json` event schema (verified against the official docs at code.claude.com/docs/en/headless) is stable across v2.1.139..v2.1.142 and emits `system/init`, `system/api_retry`, `stream_event` (with `delta.text_delta`), and a final `result` event with `usage.{input_tokens, output_tokens, cache_read_input_tokens, cache_creation_input_tokens}` and `total_cost_usd` — exactly the fields D-C5 commits to extracting. The Anthropic Go SDK v1.42.x exposes the same field names on its `Usage` struct (`InputTokens`, `OutputTokens`, `CacheReadInputTokens`, `CacheCreationInputTokens`).

**Pitfall-13 lease coordination is straightforward.** `PushOptions.ForceWithLease *ForceWithLease` carries `{RefName plumbing.ReferenceName, Hash plumbing.Hash}` — the push Job reads `Project.Status.git.lastPushedSHA`, constructs `&git.ForceWithLease{RefName: "refs/heads/"+branch, Hash: plumbing.NewHash(lastPushedSHA)}`, and the remote refuses the push if its current ref no longer matches that hash. Lease-fail recovery is annotation-driven per D-B6 (`tideproject.k8s/bypass-push-lease=true`).

**Chaos-resume has a working precedent in this codebase.** Phase 1's `internal/controller/leader_election_test.go` already exercises "lease HolderIdentity changes across failover" against envtest — the kind-tier version of the same assertion plus the four pillars of D-D4 is a tractable extension, not net-new research territory.

**Primary recommendation:** Plan Phase 3 as 4 parallel-ish work streams (up-stack reconciler bodies; `pkg/git` + push Job; `internal/subagent/anthropic/` + image; chaos-resume test fixture) with **one shared Wave 1 envelope schema bump in `pkg/dispatch`** (adding `EnvelopeIn.Provider` + `EnvelopeOut.childCRDs` + `EnvelopeOut.git.headSHA`) gating the four streams. The chaos-resume test depends on the `wait-for-signal` stub mode (D-D3) — that single stub addition is itself in Wave 1.

## Architectural Responsibility Map

| Capability | Primary Tier | Secondary Tier | Rationale |
|------------|-------------|----------------|-----------|
| Authoring `MILESTONE.md` / phase brief / `PLAN.md` | Subagent Pod (planner image) | — | D-A4 from Phase 2 already locks subagent pods to zero K8s verbs; planner subagents are no different from executor subagents at the dispatch layer |
| Materializing child CRDs (Milestone→Phases, Phase→Plans, Plan→Tasks+Waves) | Orchestrator (reconciler) | — | Subagent pod has no K8s verbs (Phase 2 D-A4); reconciler reads `EnvelopeOut.childCRDs` and server-side-creates |
| Per-level pool acquisition before dispatch | Orchestrator (reconciler) | — | D-A4 (Phase 3) + POOL-01..03 (Phase 1) — each calling reconciler acquires `plannerPool` or `executorPool` before invoking `dispatch.Dispatcher.Run` |
| Git clone of target repo (one-time per Project) | Orchestrator (dedicated init/clone Job mounting PVC) | — | Phase 2 D-G1 `tide-init` Job pattern extends; secret-bearing pods stay scoped to the push Job, not the controller pod |
| Per-Task git worktree creation (executor parallelism) | Subagent Pod (executor harness) | — | D-B4 — each executor harness runs `git worktree add /workspace/worktrees/{task-uid}`; PVC layout supports this; no `.git/index` race because each Task gets its own working tree |
| Commit accumulation across executor Tasks in a Plan | Subagent Pod (executor harness, per-Task) | — | Each executor Task commits its own diff into its own worktree; `EnvelopeOut.git.headSHA` carries the per-Task SHA back to the orchestrator |
| `git push --force-with-lease` to per-run branch | Push Job (`tide-push-{project-uid}`) | — | D-B1 + D-B2 — orchestrator dispatches push Job at level boundaries; push Job mounts PVC + git creds Secret; pushes are serialized per Project via deterministic Job name (D-B5) |
| gitleaks scan on push diff | Push Job (in-binary library call) | — | D-B3 — `tide-push` binary imports `gitleaks/v8/detect`; scans the staged diff only, not the whole repo; exit non-zero on match |
| Real Claude API call (LLM I/O) | Subagent Pod (`internal/subagent/anthropic/` calling `claude -p`) | — | D-C1..C5 — `internal/subagent/anthropic/` is the provider-specific Subagent impl; harness wraps it |
| Credential proxy (signed-token forwarding) | Sidecar in Subagent Pod (Phase 2 D-C1) | — | Phase 2 already locked credproxy sidecar pattern; Phase 3 reuses verbatim, only the main container image changes |
| Stream-json parsing (usage + final result) | Subagent Pod (harness, in-band) | — | D-C5 — parsing happens inside the subagent pod's harness; raw events written to PVC for Phase 4 OpenInference extraction |
| Chaos-resume: leader-election handoff | controller-runtime Manager (orchestrator) | — | controller-runtime's built-in leader-election (Phase 1 CTRL-03 already wired); chaos-resume tests the existing mechanism, doesn't add a new one |
| Chaos-resume: indegree recompute post-restart | Orchestrator (TaskReconciler, on Reconcile) | — | Phase 2 D-B3 already locked "indegree recomputed per reconcile from sibling Tasks"; Phase 3 chaos-resume is the existence proof |
| Chaos-resume: no-dup-dispatch property | K8s API (deterministic Job name `AlreadyExists`) | — | Phase 2 D-B5 + SUB-03 — `tide-task-{task-uid}-{attempt-n}` Job name IS the dedup key; chaos-resume verifies this holds across leader handoff |

## Project Constraints (from CLAUDE.md)

**Working Rules (apply at every step):**

1. **Observe first.** When a runtime gate is BLOCKED in Phase 3, read the manager log + VERIFICATION.md frontmatter before hypothesizing a fix. The Phase 02.2 cascade chain (12 plans) showed this rule cuts iteration count by half.
2. **Execute, don't ask** — for non-destructive next steps inside the GSD workflow. But ASK at fix-shape decision points (Option A vs B), especially around `pkg/git` API surface and `internal/subagent/common/` shape (both are Claude's Discretion items where the user may have preferences).
3. **Verify before claiming.** Run the verification; read the gate_decision; grep for the artifact; then state what you observed.

**Operating Notes (Phase 02.2 lessons that apply to Phase 3):**

- **Don't predict "chain terminator."** If a plan in Phase 3 fails verification, frame the follow-up as iteration, not closure.
- **`gsd-sdk query state.begin-phase` resets STATE.md body fields** — edit body AFTER `state.begin-phase`, not before.
- **Use `--wave N` filter when a closeout plan is gated on a BLOCKED artifact.** Phase 3 has at least one such gate: ROADMAP/STATE closeout reads gate_decision from the chaos-resume VERIFICATION.md.
- **Subagent prompts: focus, don't restate.** Decision-only context in `<additional_context>` blocks, not full cascade history.

**Tech Stack constraints (binding):**

- **Pin Anthropic SDK to a minor (`v1.42.x`)** — weekly beta-surface rev-bumps.
- **`claude` CLI ≥ v2.1.139** per STACK.md; researcher recommends pinning **v2.1.142** (latest as of 2026-05-14, no breaking changes to stream-json).
- **Never bump `k8s.io/*` independently** — controller-runtime's `go.mod` dictates.
- **OTel trace API stable (v1.43.x); metric API still v0.65.x** — don't conflate; Phase 3 doesn't touch OTel anyway (Phase 4 work).
- **Pin kind node images by `@sha256` in E2E scripts** — Phase 02.2 already pins these in the cluster.yaml.

**Anti-patterns (binding):**

- **Don't mount host `~/.claude/`** into executor containers. Use `ANTHROPIC_API_KEY` env from K8s Secret — already enforced via Phase 2's credproxy sidecar (D-C1). Phase 3's Claude image swap MUST keep this property: the subagent container env carries the **signed token**, never the raw key.
- **Don't use Claude Code's OAuth flow headless** (broken: claude-code#29983, #7100) — `ANTHROPIC_API_KEY` env path is the only supported headless mode.
- **Don't vendor GSD Markdown.** Phase 3's prompts are compiled-in Go templates in `internal/subagent/common/` (via `go:embed`).
- **Don't add cycle "recovery" features.** Plan admission webhook (Phase 2 D-E1) already refuses cyclic DAGs; Phase 3's Plan-CRD materialization from `EnvelopeOut.childCRDs` flows through the same webhook.
- **`pkg/git` and `cmd/tide-push/` MUST be free of LLM-SDK imports** — Phase 2's `providerfirewall` analyzer already covers `pkg/controller/...`, `pkg/dispatch/...`, `pkg/dag/...`; Phase 3 should consider extending the analyzer's deny-list to `pkg/git/...` and `cmd/tide-push/...` as a defense-in-depth (planner discretion).

## Standard Stack

### Core (Phase 3 additions, all verified versions)

| Library | Version | Purpose | Why Standard |
|---------|---------|---------|--------------|
| `github.com/go-git/go-git/v5` | **v5.19.0** (pinned in STACK.md) | Pure-Go git client — HTTPS+PAT clone/commit/push/fetch + worktree | `[VERIFIED: ctx7 /go-git/go-git docs]` Has first-class `PushOptions.ForceWithLease` field since v5.x. v6 is alpha-stage; STACK.md correctly pins v5. No `/bin/git` dependency in the push Job image. |
| `github.com/zricethezav/gitleaks/v8/detect` | **v8.30.1** (latest as of 2026-03-21) | Secret-pattern detection on push diffs | `[VERIFIED: ctx7 /gitleaks/gitleaks docs]` `NewDetectorDefaultConfig()` returns a detector with 150+ embedded rules; `DetectString(content)` returns `[]Finding`. Pure-Go; embedded in `tide-push` binary; no shell-out. |
| `@anthropic-ai/claude-code` (npm) | **v2.1.142** (pinned in `images/claude-subagent/Dockerfile`) | Claude headless runtime invoked as `claude -p --output-format stream-json` from inside subagent pod | `[VERIFIED: https://github.com/anthropics/claude-code/releases]` STACK.md floor is v2.1.139; v2.1.142 (May 14, 2026) is the latest stable. No breaking changes to stream-json schema across v2.1.139..v2.1.142. |
| `github.com/anthropics/anthropic-sdk-go` | **v1.42.x** (already pinned via STACK.md) | Reference for Anthropic Usage shape — `internal/subagent/anthropic/` parses stream-json events, not SDK output, but the field-name reference matches | `[VERIFIED: ctx7 /anthropics/anthropic-sdk-go docs]` `Usage` struct exposes `InputTokens`, `OutputTokens`, `CacheCreationInputTokens`, `CacheReadInputTokens`. The Phase 3 stub maps stream-json snake_case → Go camelCase. |

### Carrying forward from Phase 2 (already pinned)

- `pkg/dispatch` (envelope + Subagent interface) — Phase 3 extends `EnvelopeIn.Provider`, `EnvelopeOut.childCRDs`, `EnvelopeOut.git.headSHA`. Schema rev stays at `tideproject.k8s/v1alpha1`; **additive only** (Phase 2 D-A3 envelope-rejection-on-unknown remains — harness rejects unknown apiVersion).
- `internal/harness` — provider-agnostic core (caps, redact, signed-token client, output-path validation). Phase 3 keeps it verbatim; `internal/subagent/anthropic/` calls `harness.Execute()` from its `Run()` method.
- `internal/dispatch/podjob.PodJobBackend` — Phase 2's concrete Job-creator. Phase 3 reuses; only the dispatching reconciler changes (up-stack levels now call it directly).
- `internal/credproxy` — HMAC token + HTTPS proxy. Phase 3 reuses verbatim.
- `internal/budget` — token-bucket + per-Project rollup. Phase 3's planner Tasks roll into the same `Project.Status.budget` infrastructure.

### Alternatives Considered

| Instead of | Could Use | Tradeoff |
|------------|-----------|----------|
| `go-git/v5` (pure-Go) | `os/exec` to system `git` binary | Pure-Go avoids `/bin/git` in the push-Job image (smaller, no system-git CVE surface), and the API is type-safe. System-`git` is fine for one-off scripts but Phase 3's `tide-push` is a long-lived in-cluster binary — pure-Go wins. **Decision: go-git/v5 per STACK.md.** |
| `gitleaks/v8/detect` as library | Shell out to `gitleaks` CLI in push Job | D-B3 already locked Go library — keeps `tide-push` a single static binary, no `gitleaks` CLI to keep in the image, no subprocess management. **Decision: locked.** |
| `claude -p --output-format stream-json` (CLI) | Anthropic Go SDK direct `client.Messages.NewStreaming()` | The Claude Code CLI bundles the agent loop, hooks, MCP, skills, and bash/file tools. Direct SDK would force re-implementing the agent loop in Go. **Decision: CLI for v1 per HARN-06; reserve SDK for a future "ClaudeNative" provider if Claude Code CLI footprint becomes a problem.** |
| Per-Task push Jobs (parallel) | Per-Project serialized push Jobs | Push cost is small (artifact diff + gitleaks scan on diff only); per-Project serialization is dramatically simpler than parallel-push-with-lease-retry. **Decision: D-B5 locked — serial per Project.** |
| `flock` on shared `.git/index` for executor parallelism | `git worktree add` per Task | git provides the worktree mechanism for exactly this case. **Decision: D-B4 locked — worktrees.** |
| OAuth headless mode | `ANTHROPIC_API_KEY` env from Secret | OAuth in headless containers is broken (claude-code#29983, #7100). **Decision: API key env path, sidecar credproxy mediates.** |

**Installation (Phase 3 additions):**

```bash
# Go deps (added to go.mod)
go get github.com/go-git/go-git/v5@v5.19.0
go get github.com/zricethezav/gitleaks/v8@v8.30.1

# Image base for Claude subagent
# images/claude-subagent/Dockerfile:
#   FROM node:22-slim
#   RUN npm install -g @anthropic-ai/claude-code@2.1.142
#   COPY claude-subagent /usr/local/bin/
```

**Version verification (run before plan commits):**

```bash
go list -m github.com/go-git/go-git/v5  # → v5.19.0
go list -m github.com/zricethezav/gitleaks/v8  # → v8.30.1
docker run --rm node:22-slim sh -c 'npm view @anthropic-ai/claude-code version'  # → 2.1.142+
```

## Architecture Patterns

### System Architecture Diagram

```
                                    ┌─────────────────────────────┐
   kubectl apply Project ─────────► │  ProjectReconciler          │
   (spec.git + spec.subagent)       │  - tide-init Job (Phase 2)  │
                                    │  - tide-clone Job (NEW)     │
                                    │  - branch name init         │
                                    └──────────────┬──────────────┘
                                                   │ owns
                                                   ▼
                                    ┌─────────────────────────────┐
                                    │  MilestoneReconciler        │  ◄── plannerPool (size 16)
                                    │  - dispatch.Run(planner,    │
                                    │      level=milestone)       │
                                    │  - materialize Phases       │
                                    │      from EnvelopeOut.      │
                                    │      childCRDs              │
                                    │  - push Job at boundary     │
                                    └──────────────┬──────────────┘
                                                   │ owns N
                                                   ▼
                                    ┌─────────────────────────────┐
                                    │  PhaseReconciler            │  ◄── plannerPool
                                    │  (same shape, level=phase)  │
                                    └──────────────┬──────────────┘
                                                   │ owns N
                                                   ▼
                                    ┌─────────────────────────────┐
                                    │  PlanReconciler             │  ◄── plannerPool
                                    │  (level=plan; materializes  │
                                    │   Tasks + Waves)            │
                                    │  - Plan admission webhook   │
                                    │    validates DAG (Phase 2)  │
                                    └──────────────┬──────────────┘
                                                   │ owns N (Tasks + Waves)
                                                   ▼
                                    ┌─────────────────────────────┐
                                    │  TaskReconciler (Phase 2)   │  ◄── executorPool (size 4)
                                    │  - per-Task Job             │
                                    │  - harness: git worktree    │
                                    │    add /workspace/          │
                                    │    worktrees/{task-uid}     │
                                    │  - claude-subagent          │
                                    │    runs `claude -p`         │
                                    └──────────────┬──────────────┘
                                                   │ at level boundary
                                                   ▼
                                    ┌─────────────────────────────┐
                                    │  Push Job (tide-push-       │
                                    │  {project-uid})             │
                                    │  - mounts PVC + git Secret  │
                                    │  - go-git/v5 Push with      │
                                    │    ForceWithLease vs        │
                                    │    Status.git.lastPushedSHA │
                                    │  - gitleaks scans diff      │
                                    │  - exits 0 → reconciler     │
                                    │    patches Status.git.      │
                                    │    lastPushedSHA            │
                                    └─────────────────────────────┘

  Chaos-resume (PERSIST-04):
    kubectl delete pod -l app=tide-controller-manager
    → lease window (~15s)
    → new leader takes over
    → indegree recompute from live Task CRDs (Phase 2 D-B3)
    → no-dup-dispatch via deterministic Job names (Phase 2 D-B5)
    → completed-set preserved (T-α.Status.Phase=Succeeded preserved across kill)
```

### Recommended Project Structure (additions to Phase 2 layout)

```
pkg/dispatch/
├── envelope.go                      # EXTEND: add EnvelopeIn.Provider, EnvelopeOut.childCRDs, EnvelopeOut.git.headSHA
├── subagent.go                      # UNCHANGED — Run(ctx, EnvelopeIn) (EnvelopeOut, error)
├── childcrd.go                      # NEW — type ChildCRDSpec { Kind, Name string; Spec runtime.RawExtension }
└── provider.go                      # NEW — type ProviderSpec { Vendor, Model string; Params map[string]string }

pkg/git/                             # NEW package — provider-agnostic Go API
├── clone.go                         # Clone(ctx, repoURL, dest, &Auth{Username, PAT}) → *Repository
├── worktree.go                      # WorktreeAdd(repo, dir, branch) → *Worktree
├── commit.go                        # Commit(wt, msg, author) → plumbing.Hash
├── push.go                          # Push(repo, branch, lastPushedSHA, auth) — wraps go-git Push + ForceWithLease
├── fetch.go                         # Fetch(repo, auth)
└── doc.go                           # package doc — HTTPS+PAT default; SSH caveats noted

internal/gitleaks/                   # NEW package — thin wrapper around v8/detect
├── scanner.go                       # NewScanner() / Scan(diffContent) → []Finding
├── config.go                        # LoadConfig(configPath) — extends default rules
├── default_rules.toml               # NEW — go:embed source for default ruleset
└── scanner_test.go

internal/subagent/                   # NEW directory — provider-specific Subagent impls
├── common/
│   ├── stream_reader.go             # JSONL line-by-line reader (used by anthropic, future openai, etc.)
│   ├── prompt_templates.go          # go:embed templates/*.tmpl
│   ├── templates/
│   │   ├── milestone_planner.tmpl
│   │   ├── phase_planner.tmpl
│   │   ├── plan_planner.tmpl
│   │   └── task_executor.tmpl
│   └── ...
└── anthropic/
    ├── subagent.go                  # implements dispatch.Subagent.Run() — invokes `claude -p`
    ├── stream_parser.go             # parses stream-json events → EnvelopeOut.usage + .result
    └── subagent_test.go

internal/controller/
├── project_controller.go            # EXTEND — clone Job dispatch; branch name init at creation
├── milestone_controller.go          # EXTEND — planner Job dispatch + Phase materialization + push Job
├── phase_controller.go              # EXTEND — same shape at phase level
├── plan_controller.go               # EXTEND — same shape at plan level + Wave materialization
├── task_controller.go               # UNCHANGED at dispatch level; harness now does worktree-setup
├── dispatch_helpers.go              # NEW — shared resolveProvider() + materializeChildCRDs() helpers
└── push_helpers.go                  # NEW — buildPushJob(project, level, branch, lastSHA)

cmd/
├── tide-push/                       # NEW
│   └── main.go                      # reads /workspace, computes diff, runs gitleaks, runs go-git Push
├── claude-subagent/                 # NEW thin shim (~50 LOC)
│   └── main.go                      # loads EnvelopeIn → harness.Execute(anthropic.New())
└── stub-subagent/
    └── main.go                      # EXTEND — adds wait-for-signal mode (D-D3)

images/
├── tide-push/                       # NEW
│   └── Dockerfile                   # FROM gcr.io/distroless/static:nonroot; COPY tide-push /
├── claude-subagent/                 # NEW
│   └── Dockerfile                   # FROM node:22-slim; npm install -g @anthropic-ai/claude-code@2.1.142; COPY claude-subagent /
└── stub-subagent/
    └── Dockerfile                   # UNCHANGED at structure; binary updated

charts/tide/
├── values.yaml                      # EXTEND — subagent.defaults.{image, model}, subagent.levels.{...}, images.claudeSubagent.repository, images.tidePush.repository, gitleaks.configMapName, leaderElection.{leaseDurationSeconds, renewDeadlineSeconds, retryPeriodSeconds}
└── templates/
    ├── push-rbac.yaml               # NEW — dedicated tide-push SA + Role with secrets/get verb only
    └── claude-subagent-rbac.yaml    # ? — likely reuses tide-subagent SA from Phase 2

test/e2e/                            # Phase 02.2 fixture dir
└── chaos_resume_test.go             # NEW — Layer B kind spec, four-pillar assertion (D-D4)

api/v1alpha1/
├── project_types.go                 # EXTEND — Spec.subagent.{image,model,levels.{...}}, Spec.git.{repoURL, credsSecretRef, leaksConfigRef}, Status.git.{branchName, lastPushedSHA, leaseFailureCount}, Status.phase += "PushLeaseFailed"
└── shared_types.go                  # EXTEND — Condition types: "PushLeaseFailed", "Cloned", "AuthoringPlanner"
```

### Pattern 1: pkg/git Clone with HTTPS+PAT (verified)

```go
// Source: ctx7 /go-git/go-git docs (Clone Repository with Access Token Authentication)
import (
    git "github.com/go-git/go-git/v5"
    "github.com/go-git/go-git/v5/plumbing/transport/http"
)

func Clone(ctx context.Context, repoURL, destDir, pat string) (*git.Repository, error) {
    return git.PlainCloneContext(ctx, destDir, true /* bare */, &git.CloneOptions{
        URL: repoURL,
        Auth: &http.BasicAuth{
            Username: "x-access-token", // Convention: any non-empty string works; GitHub recognizes "x-access-token"
            Password: pat,              // GitHub/GitLab/Gitea PAT
        },
    })
}
```

**Notes:**
- `Username` must be non-empty; any value works for HTTPS+PAT (GitLab convention is `"oauth2"`, GitHub `"x-access-token"`, but both servers accept either with the PAT as Password).
- `bare = true` for the orchestrator's shared `/workspace/repo.git/` clone — D-B4 PVC layout.
- `PlainCloneContext` honors the context's deadline — the clone Job's `activeDeadlineSeconds` is the outer wall-clock cap.

### Pattern 2: Worktree Add (per Task executor, verified)

```go
// Source: go-git/v5 Repository.Worktree() + PlainOpen
// Per-Task harness invocation
import (
    git "github.com/go-git/go-git/v5"
)

func WorktreeAdd(bareRepoPath, worktreeDir, branch string) error {
    repo, err := git.PlainOpen(bareRepoPath)
    if err != nil {
        return err
    }
    // go-git/v5 does not have a first-class WorktreeAdd API equivalent to `git worktree add`.
    // Workaround: clone the bare repo to a new directory with --branch <branch>; this
    // creates an effective worktree on a per-Task subdir. See "Risks" §"go-git worktree
    // API gap" below.
    _, err = git.PlainClone(worktreeDir, false /* not bare */, &git.CloneOptions{
        URL:           bareRepoPath,
        ReferenceName: plumbing.NewBranchReferenceName(branch),
        SingleBranch:  true,
    })
    return err
}
```

**KEY FINDING (LOW confidence claim flagged for verification at plan-phase):** `go-git/v5` has `(*Repository).Worktree()` but does NOT have a first-class equivalent to `git worktree add <dir>` — it has one worktree per `Repository`. **The Phase 3 D-B4 "worktrees-per-Task" pattern needs to be implemented as per-Task `PlainClone` from the local bare repo path** (which is what `git worktree add` does under the hood for the working-tree case, minus the metadata-sharing optimization). Each Task's working tree is its own clone of the local bare repo on the same PVC — cheap (file-system clone path), correct (independent index, no `.git/index` race), and avoids the `git worktree add` semantics that go-git/v5 doesn't expose. **Planner should verify against the latest go-git/v5 GoDoc.** [ASSUMED — based on ctx7 examples showing only `PlainOpen`/`PlainClone` worktree handling; no `Worktree.Add(dir)` API surfaced in the docs returned by ctx7].

### Pattern 3: Push with --force-with-lease (verified)

```go
// Source: WebFetch https://pkg.go.dev/github.com/go-git/go-git/v5#PushOptions (verified shape)
import (
    git "github.com/go-git/go-git/v5"
    "github.com/go-git/go-git/v5/config"
    "github.com/go-git/go-git/v5/plumbing"
    "github.com/go-git/go-git/v5/plumbing/transport/http"
)

func Push(ctx context.Context, repo *git.Repository, branch, lastPushedSHA, pat string) error {
    refSpec := config.RefSpec(fmt.Sprintf("refs/heads/%s:refs/heads/%s", branch, branch))
    lease := &git.ForceWithLease{
        RefName: plumbing.NewBranchReferenceName(branch),
        Hash:    plumbing.NewHash(lastPushedSHA), // empty if first push
    }
    return repo.PushContext(ctx, &git.PushOptions{
        RemoteName: "origin",
        RefSpecs:   []config.RefSpec{refSpec},
        Auth: &http.BasicAuth{
            Username: "x-access-token",
            Password: pat,
        },
        ForceWithLease: lease,
    })
}
```

**Lease semantics:**
- On first push (`lastPushedSHA == ""`): pass `Hash: plumbing.ZeroHash` or omit `ForceWithLease` entirely on first push (a brand-new branch creation has nothing to lease against).
- On subsequent pushes: `Hash: plumbing.NewHash(lastPushedSHA)` — remote rejects if its current ref has moved off `lastPushedSHA`.
- Lease-fail surfaces as `transport.ErrPushRefRejected` or similar; recipe is to inspect the error and surface `Project.Status.phase=PushLeaseFailed` (D-B6).

### Pattern 4: gitleaks Detect on Diff (verified)

```go
// Source: ctx7 /gitleaks/gitleaks docs (Initialize Detector with Default Configuration)
import "github.com/zricethezav/gitleaks/v8/detect"

func ScanDiff(diffContent string) (bool, []detect.Finding, error) {
    detector, err := detect.NewDetectorDefaultConfig()
    if err != nil {
        return false, nil, err
    }
    findings := detector.DetectString(diffContent) // 150+ embedded rules
    return len(findings) > 0, findings, nil
}
```

**Notes:**
- `NewDetectorDefaultConfig()` embeds the upstream `config/gitleaks.toml` ruleset — no external config needed for v1.0.
- For per-Project overrides (D-B3 — `Project.Spec.git.leaksConfigRef`), read the ConfigMap TOML, pass through `viper.ReadConfig` + `vc.Translate()` to a `config.Config`, then `detect.NewDetector(cfg)` (per the gitleaks "Initialize Detector with Go Library" example).
- The push Job runs `git diff <lastPushedSHA>..<headSHA>` to produce the diff text, then passes that to `ScanDiff` — bounded scan scope, not whole-repo.

### Pattern 5: stream-json Parse (verified shape)

```go
// Source: WebFetch https://raw.githubusercontent.com/ericbuess/claude-code-docs/main/docs/headless.md + 
//         ctx7 /ericbuess/claude-code-docs (verified ResultMessage shape)
package anthropic

import (
    "bufio"
    "encoding/json"
    "io"
)

type streamEvent struct {
    Type    string `json:"type"`
    Subtype string `json:"subtype,omitempty"` // "init" | "plugin_install" | "api_retry" for type=system
    // assistant streaming
    Event   *struct {
        Type  string `json:"type"`  // "content_block_start" | "content_block_delta" | "content_block_stop" | ...
        Delta *struct {
            Type string `json:"type"` // "text_delta"
            Text string `json:"text"`
        } `json:"delta,omitempty"`
    } `json:"event,omitempty"`
    // result (final event)
    Result        string                 `json:"result,omitempty"`
    SessionID     string                 `json:"session_id,omitempty"`
    TotalCostUSD  float64                `json:"total_cost_usd,omitempty"`
    DurationMS    int64                  `json:"duration_ms,omitempty"`
    DurationAPIMS int64                  `json:"duration_api_ms,omitempty"`
    IsError       bool                   `json:"is_error,omitempty"`
    NumTurns      int                    `json:"num_turns,omitempty"`
    Usage         *streamUsage           `json:"usage,omitempty"`
    ModelUsage    map[string]modelUsage  `json:"model_usage,omitempty"`
}

type streamUsage struct {
    InputTokens              int64 `json:"input_tokens"`
    OutputTokens             int64 `json:"output_tokens"`
    CacheCreationInputTokens int64 `json:"cache_creation_input_tokens"`
    CacheReadInputTokens     int64 `json:"cache_read_input_tokens"`
}

type modelUsage struct {
    InputTokens              int64   `json:"inputTokens"`              // camelCase in this nested type per docs
    OutputTokens             int64   `json:"outputTokens"`
    CacheReadInputTokens     int64   `json:"cacheReadInputTokens"`
    CacheCreationInputTokens int64   `json:"cacheCreationInputTokens"`
    WebSearchRequests        int     `json:"webSearchRequests"`
    CostUSD                  float64 `json:"costUSD"`
    ContextWindow            int     `json:"contextWindow"`
    MaxOutputTokens          int     `json:"maxOutputTokens"`
}

// ParseStream reads JSONL events from r, returns final EnvelopeOut.usage + .result.
// Also tees raw events to events.jsonl for Phase 4 OpenInference extraction.
func ParseStream(r io.Reader, rawSink io.Writer) (usage dispatch.Usage, resultText string, err error) {
    scanner := bufio.NewScanner(r)
    scanner.Buffer(make([]byte, 0, 1024*1024), 16*1024*1024) // 16MB line budget — claude-code piped stdin cap is 10MB
    for scanner.Scan() {
        line := scanner.Bytes()
        if _, werr := rawSink.Write(append(line, '\n')); werr != nil {
            return usage, resultText, werr
        }
        var ev streamEvent
        if jerr := json.Unmarshal(line, &ev); jerr != nil {
            continue // tolerate non-JSON lines (rare, but defensive)
        }
        if ev.Type == "result" {
            resultText = ev.Result
            if ev.Usage != nil {
                usage.InputTokens = ev.Usage.InputTokens
                usage.OutputTokens = ev.Usage.OutputTokens
                // NEW Phase 3 fields on dispatch.Usage:
                // usage.CacheReadTokens = ev.Usage.CacheReadInputTokens
                // usage.CacheCreationTokens = ev.Usage.CacheCreationInputTokens
            }
        }
    }
    return usage, resultText, scanner.Err()
}
```

**Key field-name observations (snake_case from Claude Code CLI / `claude -p`, NOT camelCase from the SDK Usage struct):**
- Top-level `usage` in stream-json: `input_tokens`, `output_tokens`, `cache_creation_input_tokens`, `cache_read_input_tokens`.
- Per-model `model_usage` map: camelCase (`inputTokens`, `outputTokens`, `cacheReadInputTokens`, `cacheCreationInputTokens`) — quirk: claude-code passes the TypeScript ModelUsage type through unmodified.
- **Phase 3 envelope field naming decision (Claude's Discretion):** `EnvelopeOut.Usage.CacheReadTokens` and `EnvelopeOut.Usage.CacheCreationTokens` (drop the `Input` infix — Anthropic's API names them `cache_read_input_tokens` because they ARE input tokens; the existing Phase 2 envelope `Usage.InputTokens` already covers the dimension, and the cache columns are sub-categories of input. Cleaner field names. Planner finalizes.)

### Pattern 6: Claude Subagent Invocation (verified CLI shape)

```go
// Source: WebFetch https://code.claude.com/docs/en/headless (verified)
package anthropic

import (
    "context"
    "os/exec"
)

func runClaude(ctx context.Context, model, prompt string) (*exec.Cmd, error) {
    cmd := exec.CommandContext(ctx,
        "claude", "-p", prompt,
        "--model", model,
        "--output-format", "stream-json",
        "--verbose",
        "--include-partial-messages",
        "--bare",  // skip auto-discovery of hooks/skills/plugins/MCP/auto-memory/CLAUDE.md
                   // (per docs: "Bare mode is useful for CI and scripts where you need the same result on every machine.")
    )
    // ANTHROPIC_BASE_URL points at credproxy sidecar at 127.0.0.1:8443 (Phase 2 D-C1)
    // ANTHROPIC_API_KEY is the signed token (Phase 2 D-C3) — NEVER the real key
    cmd.Env = append(cmd.Environ(),
        "ANTHROPIC_BASE_URL=https://127.0.0.1:8443",
        "ANTHROPIC_API_KEY="+signedToken,
        // NODE_EXTRA_CA_CERTS=/etc/tide/proxy/ca.crt  (Phase 2 D-C2)
    )
    return cmd, nil
}
```

**Important — `--bare` flag (NEW in Phase 3, per the docs):** per claude.com/docs/en/headless: *"Bare mode is useful for CI and scripts where you need the same result on every machine. A hook in a teammate's ~/.claude or an MCP server in the project's .mcp.json won't run, because bare mode never reads them. Only flags you pass explicitly take effect. ... Bare mode skips OAuth and keychain reads. Anthropic authentication must come from ANTHROPIC_API_KEY or an apiKeyHelper in the JSON passed to --settings."* This perfectly aligns with TIDE's "never mount host ~/.claude/" anti-pattern (CLAUDE.md). **Planner should pin `--bare` as the canonical flag in the Phase 3 invocation.**

### Anti-Patterns to Avoid

- **Don't add an aggregate `Project.Status.Schedule` field** — Phase 1 PERSIST-02 forbids; chaos-resume D-D4 pillar #5 verifies wave-rederivation works from CRDs alone.
- **Don't shell out to system `git` from the push Job binary** — go-git/v5 is the contract; shell-out defeats the static-binary image story.
- **Don't materialize `ChildCRDSpec` blocks via Markdown parsing** — D-A1 locks `EnvelopeOut.childCRDs` as authoritative; Markdown is the human surface, never the orchestrator's input.
- **Don't push from inside subagent pods** — ART-04 + D-B1 + Pitfall 13: one credential surface, one push process. Subagent pods don't have git creds, period.
- **Don't reuse the same push Job name for the next level boundary** — `tide-push-{project-uid}` (no level suffix) IS the serialization key; the second-push-during-first-push case is `AlreadyExists` and the caller requeues. **NOT** `tide-push-{project-uid}-plan` or similar — that would un-serialize.
- **Don't use `--continue` / `--resume` on the Claude CLI** — Phase 3 dispatches independent planner Tasks; each is its own session. Resumption is at the K8s level (CRD restart), not at the LLM-session level.

## Don't Hand-Roll

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|-------------|-----|
| Secret-pattern detection on git diffs | Custom regex set | `github.com/zricethezav/gitleaks/v8/detect` with `NewDetectorDefaultConfig()` | 150+ embedded, community-maintained rules covering AWS, GCP, Anthropic, GitHub, Stripe, JWT, SSH keys, etc. Hand-rolled regex set would lag and miss novel patterns. |
| HTTPS+PAT git clone/commit/push | `os/exec` "git" with PAT in URL | `go-git/v5` `BasicAuth{Username, Password}` | Pure-Go; type-safe; no subprocess management; PAT never appears in process arg list (which would leak via `ps`). |
| `--force-with-lease` lease checking | Pre-push `git ls-remote` + compare + push | `PushOptions.ForceWithLease *ForceWithLease` | First-class in go-git/v5 v5.19.0; atomic with the push (no TOCTOU race between lease-check and push). |
| Claude stream-json event parsing | Re-implement the agent loop with Anthropic Go SDK | `claude -p --output-format stream-json --bare` | Claude Code CLI bundles the agent loop, hooks, MCP, tools. Reimplementing in Go doubles Phase 3 scope. |
| `git worktree add` semantics for per-Task isolation | `flock` on shared `.git/index` | Per-Task `git.PlainClone(bareRepoPath, ...)` (local clone, file-system-fast) | Each Task gets independent index; no race; PVC cost is minor (clone of bare repo's working tree, not full pack). |
| K8s leader election handoff testing | Custom watchdog goroutine | controller-runtime's built-in `manager.Options.LeaderElection` + `kubectl delete pod` | Phase 1 `TestLeaderElection` (envtest, lease HolderIdentity changes) is the proven pattern; Layer B kind tier just runs the same shape against real Jobs. |
| Stream-event parsing for OpenInference span emission | Parse during Phase 3 harness | Preserve raw `events.jsonl` on PVC; Phase 4 `pkg/otelai` reads it | D-C5 + Phase 4 OBS-03..05 boundary; doing this in Phase 3 doubles scope. |
| RWX storage on kind for integration tests | Hand-rolled NFS server + pod-affinity tricks | Phase 2's already-shipping chart override: `--set workspaces.pvc.accessModes={ReadWriteOnce}` | Phase 02.2 already proved this works for chaos-resume's namespace-local PVC scenario; no driver-install required for the test fixture. |

**Key insight:** Phase 3 is fanout, not invention. **Every "don't hand-roll" entry above corresponds to an existing battle-tested library or a Phase 1/2 precedent in this codebase.** The risk in Phase 3 is over-engineering — building custom abstractions where the library or precedent suffices.

## Runtime State Inventory

> Phase 3 is **not** a rename/refactor/migration phase — it's a greenfield integration fanout. Runtime state additions are NEW state (not migrations of existing state):

| Category | Items Found | Action Required |
|----------|-------------|------------------|
| Stored data | NEW: `/workspace/repo.git/` (bare clone), `/workspace/worktrees/{task-uid}/` (per-Task), `/workspace/envelopes/{task-uid}/events.jsonl` (raw stream-json log). NEW: per-Project git branch `tide/run-<project-name>-<unix-epoch>` on remote. | All net-new — D-B4 PVC layout extension + D-B6 branch naming. No data migration. |
| Live service config | NEW: per-Project git creds Secret (`Project.Spec.git.credsSecretRef`); NEW: optional per-Project gitleaks override ConfigMap (`Project.Spec.git.leaksConfigRef`); EXTENDED: chart `images.{claudeSubagent,tidePush}.repository` Helm values; EXTENDED: `subagent.{defaults,levels}` Helm values. | All net-new. Phase 5 Helm chart documentation captures these as user-config inputs. |
| OS-registered state | None — Phase 3 adds no OS-level registrations (no systemd, no Task Scheduler). Push Job is a K8s Job, not an OS service. | None required. |
| Secrets/env vars | NEW: `Project.Spec.git.credsSecretRef` Secret data key conventions (e.g., `GIT_PAT` data key); `ANTHROPIC_API_KEY` env var continues to flow only into the credproxy sidecar (Phase 2 D-C4) — the real Claude image is a TARGET for the env var via signed-token, NOT a recipient of the real key. | Document Secret data-key conventions in Phase 5 docs. No code rename. |
| Build artifacts | NEW: `images/tide-push/`, `images/claude-subagent/` Dockerfiles + build outputs in CI; EXTENDED: `images/stub-subagent/` rebuild with `wait-for-signal` mode (D-D3). | All produced by CI/release pipeline; no stale artifacts since these are NEW images. |

**Nothing found in category "OS-registered state":** Verified explicitly — Phase 3's only runtime presence is in K8s (Jobs, CRDs, Secrets, ConfigMaps). No host-level state.

## Common Pitfalls

### Pitfall 1: go-git/v5 worktree API gap (vs. CLI `git worktree add`)

**What goes wrong:** Phase 3's D-B4 wants per-Task worktrees for executor parallelism. The naïve assumption is that go-git/v5 exposes a function equivalent to CLI `git worktree add <dir> <branch>` — it does not (verified by ctx7 returning only `PlainOpen`/`PlainClone`/`Repository.Worktree()` APIs).

**Why it happens:** Linked-worktree metadata in `.git/worktrees/` is a fairly recent (post-2017) git feature; go-git's coverage of plumbing is more comprehensive than its porcelain.

**How to avoid:** Implement worktree-add as per-Task `git.PlainClone(bareRepoPath, worktreeDir, false, &git.CloneOptions{URL: bareRepoPath, ...})` against the local bare repo path. This is functionally equivalent for TIDE's case (each Task gets an independent index; commits push back to the shared bare via `Remote.Push` against the file:// URL). Slightly more disk than `git worktree add` (object directory not shared), but on the per-Project PVC with RWX, disk is the abundant resource.

**Warning signs:** Compile error on `bareRepo.Worktree().Add(...)`. Plan task that says "use go-git worktree API" without specifying the function name.

### Pitfall 2: `--force-with-lease` on first push has no lease to check

**What goes wrong:** First push to a brand-new `tide/run-<project>-<unix>` branch has no prior SHA to lease against. Passing `Hash: plumbing.NewHash("")` evaluates to `plumbing.ZeroHash` which the server interprets as "ref must not exist" — which is actually what we want on first push.

**Why it happens:** `--force-with-lease=branch:` (empty after colon) and `--force-with-lease=branch:0000...` have different semantics: the former is "I expect this ref to not exist yet"; the latter is "I expect this ref to be at the zero hash."

**How to avoid:** First push (when `Project.Status.git.lastPushedSHA == ""`): omit `ForceWithLease` from `PushOptions` entirely (just set `Force: false` — let the server natural-reject if the branch already exists from a prior failed attempt). Subsequent pushes: pass `&git.ForceWithLease{RefName: ..., Hash: plumbing.NewHash(lastPushedSHA)}`.

**Warning signs:** Phase 3 plan that says "always pass ForceWithLease" without conditioning on first-push state. Push Job test that fails on the first apply.

### Pitfall 3: Claude CLI not pinned; stream-json schema drifts mid-Phase

**What goes wrong:** STACK.md says `claude` CLI ≥ v2.1.139. If the Dockerfile uses `npm install -g @anthropic-ai/claude-code` (no version pin), the build picks up the latest weekly release; if a future minor changes the stream-json event names, Phase 3's `internal/subagent/anthropic/stream_parser.go` silently breaks.

**Why it happens:** npm's default is "latest"; weekly Anthropic releases occasionally tweak event shape (verified non-breaking through v2.1.142, but no guarantee for future minors).

**How to avoid:** Pin `@anthropic-ai/claude-code@2.1.142` in `images/claude-subagent/Dockerfile`. Helm chart's `images.claudeSubagent.tag` matches the pinned minor. Periodic schedule (weekly?) bumps the pin after testing against the new stream-json shape — but never auto-bump.

**Warning signs:** Dockerfile that `npm install -g @anthropic-ai/claude-code` without `@<version>`. Helm value `images.claudeSubagent.tag: latest`.

### Pitfall 4: Push Job races between two reconcilers at a level boundary

**What goes wrong:** `MilestoneReconciler` observes "all Phases done" and creates the push Job. `PhaseReconciler` (still finishing its last phase's push) tries to create another push Job. Both write to `Project.Status.git.lastPushedSHA` (the slow one writes a stale SHA last). Subsequent push leases fail.

**Why it happens:** Two reconcilers observe the same Project; their reconcile timings interleave; both think they should push.

**How to avoid:** D-B5 already locks the answer: deterministic Job name `tide-push-{project-uid}` is the per-Project serialization key. The second `Create` returns `AlreadyExists`; the calling reconciler enters a requeue. Per-reconciler logic: (a) check if `tide-push-{project-uid}` exists and is `status.active=1`; if yes, requeue with backoff; (b) only the reconciler whose push Job successfully `Created` patches `Status.git.lastPushedSHA` from the Job's emitted envelope. Phase 2 D-D2's "one write per Task completion" pattern is the precedent.

**Warning signs:** Multiple reconcilers patching `Status.git.lastPushedSHA` in the same code path. Push Jobs with level-suffixed names like `tide-push-{project-uid}-plan`.

### Pitfall 5: gitleaks default ruleset version-locked into the binary

**What goes wrong:** `tide-push` binary is built with gitleaks/v8.30.1's embedded ruleset. A novel secret pattern is published as a new gitleaks rule six months later. Operator clusters running the old `tide-push` image silently miss the new pattern.

**Why it happens:** D-B3 commits to embedded default ruleset; that means rule updates require a new `tide-push` image release.

**How to avoid:** Two layers of defense:
1. **Per-Project override via `Project.Spec.git.leaksConfigRef` ConfigMap** — operators can ship updated rules without redeploying the operator (the ConfigMap can declare `[extend] useDefault = true` + new rules, getting the embedded default PLUS new rules per the gitleaks `extend` mechanism in the ctx7 example).
2. **Document the gitleaks version pin** in Phase 5 install docs; release-cadence policy bumps `tide-push` image with each `gitleaks/v8.x` minor (likely quarterly).

**Warning signs:** No `Project.Spec.git.leaksConfigRef` override path. Helm chart that doesn't surface the gitleaks version.

### Pitfall 6: Chaos-resume — leader-election lease window racing the test budget

**What goes wrong:** controller-runtime defaults are LeaseDuration=15s, RenewDeadline=10s, RetryPeriod=2s. After `kubectl delete pod -l app=tide-controller-manager`, the killed leader's lease expires after up to 15s; new leader takes over within a few more seconds; total failover is ~15–20s. Test wall-clock budget is 60s. Tight but OK — except if the kind cluster is slow (CI runner, cold cache), failover can stretch beyond 20s and Eventually() assertions flake.

**Why it happens:** kind cluster scheduling latency + lease window + new pod startup latency are additive.

**How to avoid:** D-D1 budget is "~60s + 30s buffer = 90s". Phase 02.2's lesson: Eventually budgets that are "tight" become flake sources. Recommendation: kindTestTimeout for chaos-resume spec = `90s + (3 * RetryPeriod) + buffer` ≈ 2min on top of normal Layer B kindTestTimeout. Planner-tunable LeaseDuration/RenewDeadline/RetryPeriod via Helm values (already in CONTEXT.md Claude's Discretion section).

**Warning signs:** Test wall-clock budget under 60s. Eventually() polling intervals under 1s. CI failures that pass locally.

### Pitfall 7: Up-stack planner Tasks dispatched before tide-init Job completes

**What goes wrong:** `ProjectReconciler` creates a Project; the user expects `MilestoneReconciler` to immediately dispatch the milestone planner. But the planner needs `/workspace/repo.git/` to exist (so it can read the target repo's prior state for context). If the clone Job hasn't completed, the planner's harness fails on `worktreeAdd`.

**Why it happens:** Phase 2 D-G1 `tide-init` only `mkdir`s the layout; it doesn't clone. Phase 3 adds a clone step but the dispatch DAG ordering between reconcilers isn't explicit.

**How to avoid:** Two-Job init sequence:
1. `tide-init-{project-uid}` (Phase 2, unchanged) — creates `/workspace/{repo,artifacts,envelopes}/` skeleton + writes the `.git/` directory bare-clone target.
2. `tide-clone-{project-uid}` (NEW in Phase 3) — runs `pkg/git.Clone(repoURL, /workspace/repo.git, pat)` against `Project.Spec.git.repoURL` with `Project.Spec.git.credsSecretRef`.

`ProjectReconciler` sets `Project.Status.phase=Initialized` only after BOTH Jobs succeed. `MilestoneReconciler` watches `Project.Status.phase==Initialized` predicate; doesn't fire until both Jobs are done. Planner could fold both into a single Job, but two-Job split keeps Phase 2's `tide-init` semantics unchanged (Phase 02.2 lesson: don't perturb working code).

**Warning signs:** `MilestoneReconciler` reconcile-loop reading `/workspace/repo.git/` without checking init status. Plan that doesn't separate the clone step from the layout step.

## Code Examples

(All major examples shown in §"Architecture Patterns" above — Patterns 1-6 cover Clone, WorktreeAdd, Push, gitleaks Scan, stream-json Parse, and Claude CLI invocation.)

## State of the Art

| Old Approach | Current Approach | When Changed | Impact |
|--------------|------------------|--------------|--------|
| Manual `git push -f` to `main` from CI scripts | Per-run branches + `--force-with-lease` from K8s push Job | Pitfall 13 enforcement (Phase 3) | TIDE-on-self self-hosting safe — TIDE never overwrites human commits |
| OAuth headless for Claude in containers | `ANTHROPIC_API_KEY` env from K8s Secret + `--bare` mode | claude-code#29983, #7100 + claude.com/docs (May 2026) | Bare mode pinned as Phase 3 invocation default; skips OAuth/keychain |
| Hand-rolled regex set for secret detection | gitleaks/v8 as Go library with embedded ruleset + optional ConfigMap override | Phase 3 D-B3 | 150+ rules vs. ~10 hand-rolled; pattern coverage proportional to community contributions |
| `flock` on shared `.git/index` for parallel executors | Per-Task `PlainClone(bareRepoPath, ...)` worktrees | Phase 3 D-B4 | Independent indexes, no lock contention, no `.git/index.lock` deadlock |
| Aggregate `Status.Schedule` field for resumption | Re-derived from live Task CRDs via `pkg/dag.ComputeWaves` | Phase 1 PERSIST-02..03 + Phase 3 D-D4 pillar #5 | Chaos-resume is the existence proof |
| Single-vendor LLM integration | Vendor+model orthogonal axes via `EnvelopeIn.Provider` + `internal/subagent/{provider}/` layering | Phase 3 D-C1..C3 | v1.0 ships Anthropic; v1.x ships OpenAI/Google/etc. as net-new directories |

**Deprecated/outdated (do not use):**
- **`go-git/v5.18 and earlier`** — `PushOptions.ForceWithLease` only added at v5.19.x (per ctx7 documentation date). Pin minimum to v5.19.0.
- **Anthropic Go SDK `v0.x`** — pre-Stainless generation; v1.42.x is the current production line.
- **`@anthropic-ai/claude-code` < v2.1.128** — stdin >10MB cap fix lands at v2.1.128; if the Phase 3 envelope passes large prompts (likely for planner Tasks), the pre-v2.1.128 versions silently truncate. STACK.md floor of v2.1.139 already handles this.
- **`gitleaks v7.x` and earlier** — pre-Go-library API; STACK.md correctly pins v8.

## Validation Architecture

> Per `.planning/config.json`, `workflow.nyquist_validation` is `true` — this section is required.

### Test Framework

| Property | Value |
|----------|-------|
| Framework | Ginkgo v2.28 + Gomega + envtest (Layer A) + kind v0.31 (Layer B) — Phase 1 + Phase 2 stack, unchanged |
| Config file | `test/e2e/cluster.yaml` (kind config, Phase 02.2-shipped); `internal/controller/suite_test.go` (envtest harness) |
| Quick run command | `make test-int-fast` — Layer A envtest only (~90s) |
| Full suite command | `make test-int` — Layer A + Layer B (inner budget 1800s per Phase 02.2-09C) |
| Phase gate | Full suite green before `/gsd-verify-work`; specifically: 7+ Layer B PASS (3 Phase 2 originals + 4 Phase 3 new) + 18+ Layer A PASS |

### Phase Requirements → Test Map

| Req ID | Behavior | Test Type | Automated Command | File Exists? |
|--------|----------|-----------|-------------------|-------------|
| ART-02 | Helm chart `storageClassName` empty + override path | unit (helm template test) | `helm template charts/tide --set workspaces.pvc.storageClassName=foo \| grep "storageClassName: foo"` | ✅ Phase 02.2-03 added override; Phase 3 docs-only |
| ART-03 | `pkg/git.Push` calls `go-git` PushContext with HTTPS+PAT auth | unit | `go test ./pkg/git -run TestPush -count=1` | ❌ Wave 0 — `pkg/git/push_test.go` needed |
| ART-04 | Push happens from `tide-push-{project-uid}` Job (not subagent pod) | Layer A envtest | `go test ./internal/controller -run TestPushJobIsOrchestratorOwned -count=1` | ❌ Wave 0 — new envtest for MilestoneReconciler push-Job creation |
| ART-05 | HTTPS+PAT default works against generic remote | Layer A unit (against a `git daemon` test fixture) OR Layer B kind (against `gitea` Pod) | Layer B preferred: `go test ./test/e2e -run TestPushAgainstGiteaFixture -count=1` | ❌ Wave 0 — `test/e2e/gitea_fixture.go` + gitea image load in kind |
| ART-06 | Per-run branch name + `--force-with-lease` against `lastPushedSHA` | Layer A envtest | `go test ./internal/controller -run TestPushUsesForceWithLease -count=1` | ❌ Wave 0 |
| ART-07 | gitleaks scan blocks push + increments `tide_secret_leak_blocked_total` | Layer B kind (with planted secret in fixture diff) | `go test ./test/e2e -run TestGitleaksBlocksPush -count=1` | ❌ Wave 0 |
| AUTH-01 | `Project.Spec.git.credsSecretRef` mounted only on push Job | Layer A envtest | `go test ./internal/controller -run TestGitSecretMountedOnlyOnPushJob -count=1` | ❌ Wave 0 |
| PERSIST-04 | Chaos-resume: 4-pillar assertion across pod kill | Layer B kind | `go test ./test/e2e -run TestChaosResume -count=1` | ❌ Wave 0 — `test/e2e/chaos_resume_test.go` |
| TEST-03 | Live E2E with real Claude (nightly, separate plan) | Layer B kind + budget-capped real LLM | `go test ./test/e2e -tags=live -run TestLiveClaudeE2E -count=1` | ❌ Wave 0 — `test/e2e/live_claude_test.go` + nightly CI workflow |
| TEST-04 | (Same as PERSIST-04 — TEST-04 says "runs in integration tier") | Layer B kind | (same as PERSIST-04) | (same as above) |

**Plus Phase 3 schema bump tests:**

| (sub-req) | Behavior | Test Type | Automated Command |
|-----------|----------|-----------|-------------------|
| EnvelopeIn.Provider field added | `pkg/dispatch.EnvelopeIn` JSON-round-trip includes `Provider` | unit | `go test ./pkg/dispatch -run TestEnvelopeInProviderRoundtrip -count=1` |
| EnvelopeOut.childCRDs field added | `pkg/dispatch.EnvelopeOut` includes typed `ChildCRDSpec` | unit | `go test ./pkg/dispatch -run TestEnvelopeOutChildCRDs -count=1` |
| `wait-for-signal` stub mode | Stub blocks on `/workspace/envelopes/.../release` poll | unit | `go test ./cmd/stub-subagent -run TestWaitForSignalMode -count=1` |
| Provider resolution chain | `Project.Spec.subagent.levels.X.model` overrides `subagent.model` overrides Helm default | unit | `go test ./internal/controller -run TestProviderResolution -count=1` |
| Up-stack reconciler dispatches planner Job | `MilestoneReconciler` creates `tide-milestone-{uid}-1` Job on Project ready | Layer A envtest | `go test ./internal/controller -run TestMilestoneReconcilerDispatchesPlanner -count=1` |
| `ChildCRDSpec` → Phase materialization | Receiving Milestone planner's envelope creates Phase CRDs server-side | Layer A envtest | `go test ./internal/controller -run TestMilestoneMaterializesPhases -count=1` |

### Sampling Rate

- **Per task commit:** `make test-int-fast` (Layer A only, ~90s)
- **Per wave merge:** `make test-int` (Layer A + Layer B, inner budget 1800s)
- **Phase gate:** Full suite green before `/gsd-verify-work` invocation
- **Nightly (TEST-03):** Separate `make test-e2e-live` (budget-capped real Claude run, ~10min)

### Wave 0 Gaps

The following test files do NOT exist and must be created in Phase 3 Wave 0 (or in the same wave that creates the matching production code, depending on planner decision):

- [ ] `pkg/git/clone_test.go` — covers `pkg/git.Clone` HTTPS+PAT path
- [ ] `pkg/git/push_test.go` — covers `pkg/git.Push` with `ForceWithLease`
- [ ] `pkg/git/worktree_test.go` — covers per-Task local-clone worktree creation
- [ ] `internal/gitleaks/scanner_test.go` — covers `Scan(diffContent)` finding emission
- [ ] `internal/subagent/anthropic/stream_parser_test.go` — covers stream-json event → Usage + Result mapping
- [ ] `internal/subagent/common/stream_reader_test.go` — covers JSONL line-by-line reader
- [ ] `pkg/dispatch/childcrd_test.go` — covers `ChildCRDSpec` JSON round-trip
- [ ] `pkg/dispatch/provider_test.go` — covers `ProviderSpec` JSON round-trip
- [ ] `cmd/stub-subagent/wait_for_signal_test.go` — covers new D-D3 mode
- [ ] `internal/controller/milestone_controller_test.go` (extend) — covers MilestoneReconciler dispatch + Phase materialization
- [ ] `internal/controller/phase_controller_test.go` (extend) — covers PhaseReconciler dispatch + Plan materialization
- [ ] `internal/controller/plan_controller_test.go` (extend) — covers PlanReconciler dispatch + Task/Wave materialization (Plan admission webhook still fires)
- [ ] `internal/controller/project_controller_test.go` (extend) — covers clone Job + branch-name init
- [ ] `internal/controller/push_helpers_test.go` — covers push Job creation logic
- [ ] `test/e2e/chaos_resume_test.go` — Layer B kind chaos-resume spec (4-pillar assertion)
- [ ] `test/e2e/push_fixture_test.go` — Layer B kind push-Job spec (with optional `gitea` fixture)
- [ ] `test/e2e/live_claude_test.go` (TEST-03, separate plan) — Layer B kind real-Claude E2E

**Framework install:** None — Ginkgo v2.28 + Gomega + envtest + kind v0.31 already pinned (Phase 1 + Phase 2). No new test frameworks added in Phase 3.

## Risks & Mitigations

### Risk 1: Pitfall 13 lease coordination — stale `lastPushedSHA` causes recurring lease failures

**Risk:** If `Project.Status.git.lastPushedSHA` is patched stale (e.g., the patch arrives at the API server AFTER a successful push completes but BEFORE the next push reads it), the second push's lease check fails because the remote ref is ahead of the stale local-known SHA. Recurring lease failures (cascade) require manual `kubectl annotate` to clear.

**Mitigation:** D-B6 already commits to the architectural answer:
1. Push Job emits its own envelope-shaped result on PVC containing the new SHA.
2. Calling reconciler observes the Job's completion via `Owns(&batchv1.Job{})` watch.
3. Reconciler patches `Project.Status.git.lastPushedSHA` from the envelope, NOT from a `git ls-remote` round-trip (which would race).
4. The patch is **serialized** because (a) only one push Job is active per Project at a time (D-B5 deterministic name); (b) the reconciler only patches after observing `Job.Status.Succeeded`.

**Verification:** `TestPushUpdatesLastPushedSHA` envtest spec: create Project, simulate push Job success (write canned envelope to PVC mock), assert reconciler patches `Status.git.lastPushedSHA`. Plus chaos-resume's pillar #2 (Status preserved across leader kill) covers the durability angle.

**Recovery:** Per D-B6, manual `kubectl annotate project foo tideproject.k8s/bypass-push-lease=true`. ProjectReconciler watches the annotation, clears `Status.phase=PushLeaseFailed`, allows next push attempt. Phase 4 `tide approve --force-push` wraps the annotation.

### Risk 2: Claude Code stream-json event schema drift mid-Phase

**Risk:** Anthropic ships a weekly `claude-code` release. If a future minor renames `cache_read_input_tokens` → `cache_read_tokens` (dropping `_input`), the Phase 3 stream parser silently zero-fills the field.

**Mitigation:**
1. **Pin to a specific minor** in `images/claude-subagent/Dockerfile`: `@anthropic-ai/claude-code@2.1.142` (or whatever is current at Phase 3 plan-start).
2. **Stream-parser unit test** with frozen golden-file events: `internal/subagent/anthropic/testdata/result_event.golden.json` is the canonical event shape; the parser tests assert against it; bumping the pin requires re-recording the golden.
3. **Helm value `images.claudeSubagent.tag`** matches the Dockerfile pin; the chart documents the bump policy in `charts/tide/UPGRADING.md` (Phase 5 work).
4. **Smoke test in nightly TEST-03 live E2E** — catches stream-json drift before users notice (because the pinned image's events still parse cleanly, but a new pin's events might not).

**Verification:** `TestStreamParserGoldenEvents` unit spec.

### Risk 3: RWX driver flakiness on kind — chaos-resume PVC scenarios

**Risk:** Phase 3 chaos-resume's Job-UID-continuity pillar (D-D4 #1) requires that Jobs writing to per-Project PVC continue to write through the leader handoff. If the test fixture uses `--set workspaces.pvc.accessModes={ReadWriteOnce}` (Phase 02.2-03 override), the post-restart leader Pod may fail to re-attach the RWO volume that the killed leader Pod's CSI mount lock retains until lease expiry.

**Mitigation:**
1. **Phase 02.2 already proved the test works against namespace-local PVC with RWO** — `PodStatusEnvelopeReader` architectural pivot (cascade-10) removed the manager-side cross-namespace PVC mount. Manager doesn't need PVC visibility for chaos-resume; only the Task Pods do. Each Task Pod has its own PVC mount (namespace-local), and Tasks don't move across nodes during a leader kill.
2. **For the production-default RWX scenario** (chart's ReadWriteMany), Phase 3 docs (ART-02) enumerate the matrix: kind + `--set workspaces.pvc.accessModes={ReadWriteOnce}` for tests; EKS + EFS / GKE + Filestore / AKS + Azure Files / on-prem + csi-driver-nfs or Longhorn for production. Test fixture sticks with RWO override; documentation captures the production matrix.
3. **Explicit decision on Layer B kind chaos-resume fixture:** Use the existing RWO-override (Phase 02.2-03) — DO NOT install csi-driver-nfs / Longhorn for the test fixture. Single-node kind + RWO is the standard kind story, and Phase 02.2 already shipped a working PVC mount via `PodStatusEnvelopeReader`.

**Verification:** Re-run Phase 02.2's working `make test-int` against the Phase 3 schema additions; if Phase 02.2's PVC mount succeeded, Phase 3 chaos-resume's PVC mount succeeds (same Pod-status envelope transport).

**Recovery:** If csi-driver-nfs becomes necessary (e.g., if Phase 3 surfaces a multi-Pod-per-PVC scenario the RWO override can't handle), the planner adds a one-time `kind load + helm install csi-driver-nfs` step to `make test-int-kind-prep` — but treat this as a contingency, not the default.

### Risk 4: gitleaks rule-set version pinning — operator clusters miss new patterns

**Risk:** `tide-push` image is built with gitleaks/v8.30.1's embedded ruleset. New secret pattern types (e.g., a new cloud provider's API key format) ship in gitleaks v8.31; until operators redeploy the operator with a newer `tide-push` image, those patterns aren't detected.

**Mitigation:**
1. **`Project.Spec.git.leaksConfigRef` ConfigMap override** (D-B3 Claude's Discretion item) lets per-Project rules ship without redeploying the operator. The ConfigMap declares `[extend] useDefault = true` + new `[[rules]]` entries.
2. **`tide-push` image release cadence** is documented in Phase 5 docs as "quarterly with each major gitleaks release"; release notes call out the embedded gitleaks version.
3. **`tide_secret_leak_blocked_total` metric label includes the matched `ruleID`** — operators can detect "X% of pushes blocked by `aws-access-key` rule" patterns, surfacing whether new patterns are missed.

**Verification:** `TestGitleaksRuleConfigOverride` unit spec — load a custom config that extends defaults + adds one new rule; verify default rules still fire AND the new rule fires.

### Risk 5: Push Job serialization deadlocks — single-active-push assumption breaks

**Risk:** D-B5 commits "at most one push Job per Project active at any time". If a push Job hangs (e.g., remote-server timeout, never returns from `repo.PushContext`), the next level boundary's push attempt sees the active Job and requeues — forever. The Project's level-boundary progression stalls.

**Mitigation:**
1. **`activeDeadlineSeconds` on push Job pods** — Phase 02.2-09C pattern (Job timeout safety-net). Recommended budget: 5 minutes (network clone + push + gitleaks scan should be well under). On deadline-exceeded, K8s sets the Job's `Status.Failed` condition; reconciler observes, patches `Project.Status.phase=PushLeaseFailed` (treating timeout as a special case of lease-fail), and allows manual `kubectl annotate ... tideproject.k8s/bypass-push-lease=true` to retry.
2. **`tide_push_duration_seconds` histogram metric** surfaces tail-latency anomalies pre-deadline.
3. **Push Job retry on deadline-exceeded** — the K8s Job's `spec.backoffLimit` (default 6, planner to tune) provides automatic retry within `activeDeadlineSeconds` — different envelope from "lease-fail across attempts".

**Verification:** `TestPushJobActiveDeadlineSeconds` envtest spec — verify push Job's spec carries the configured `activeDeadlineSeconds`. `TestPushJobBackoffOnTimeout` Layer B spec (optional, may be hard to simulate reliably in kind — alternative: unit-test the Job-build helper).

## Sources

### Primary (HIGH confidence)

- **go-git/v5 PushOptions** — https://pkg.go.dev/github.com/go-git/go-git/v5#PushOptions (verified via WebFetch). Confirmed `ForceWithLease *ForceWithLease` field, `ForceWithLease{RefName plumbing.ReferenceName, Hash plumbing.Hash}` shape, v5.19.0 latest.
- **go-git COMPATIBILITY.md** — https://github.com/go-git/go-git/blob/main/COMPATIBILITY.md (via ctx7 `/go-git/go-git`). Authentication examples (HTTPS+PAT via `BasicAuth`).
- **gitleaks/v8 detect package** — https://github.com/zricethezav/gitleaks/v8 (via ctx7 `/gitleaks/gitleaks`). `NewDetectorDefaultConfig()`, `DetectString()`, `[extend] useDefault = true` override semantics, v8.30.1 latest.
- **Claude Code CLI headless** — https://code.claude.com/docs/en/headless (verified via WebFetch). `claude -p --output-format stream-json --bare --verbose --include-partial-messages` invocation; system/init + system/api_retry + stream_event/text_delta + result event schemas.
- **Claude Code stream-json reference** — https://raw.githubusercontent.com/ericbuess/claude-code-docs/main/docs/headless.md (via WebFetch — community mirror, but the schema verified against the official code.claude.com docs above).
- **Agent SDK ResultMessage Usage** — https://code.claude.com/docs/en/agent-sdk/python (via ctx7 `/ericbuess/claude-code-docs`). `input_tokens`, `output_tokens`, `cache_creation_input_tokens`, `cache_read_input_tokens` field names confirmed.
- **Anthropic Go SDK message.Usage** — https://github.com/anthropics/anthropic-sdk-go/blob/main/anthropic-sdk-go/message.go (via ctx7 `/anthropics/anthropic-sdk-go`). `Usage{InputTokens, OutputTokens, CacheCreationInputTokens, CacheReadInputTokens, ServiceTier}` Go struct confirmed.
- **controller-runtime leader-election** — https://pkg.go.dev/sigs.k8s.io/controller-runtime/pkg/manager#Options (via WebSearch). Default LeaseDuration=15s, RenewDeadline=10s, RetryPeriod=2s.
- **kind RWX limitation** — https://github.com/kubernetes-sigs/kind/issues/2371 (via WebSearch). local-path-provisioner hardcoded RWO; NFS or Longhorn are the workarounds.
- **Phase 1 leader-election test** — `internal/controller/leader_election_test.go` lines 40, 80–110. Existing pattern asserts `lease.Spec.HolderIdentity` changes across failover. Direct codebase grep.
- **Phase 2 envelope contract** — `pkg/dispatch/envelope.go`, `pkg/dispatch/subagent.go`, `pkg/dispatch/errors.go`. Confirmed `EnvelopeIn.Role`, `EnvelopeIn.Level`, `EnvelopeIn.Dev` already present (Phase 2 D-A1/D-F1).
- **Phase 2 chart RWX/RWO override** — `charts/tide/values.yaml` lines 152–173. Confirmed `workspaces.pvc.accessModes` value with documented `--set` override pattern (Phase 02.2-03).

### Secondary (MEDIUM confidence)

- **claude-code latest version** — https://github.com/anthropics/claude-code/releases (via WebFetch). v2.1.142 (May 14, 2026); v2.1.139 mid-week 13; no stream-json breaking changes in v2.1.139..v2.1.142 release notes.
- **csi-driver-nfs install on kind** — https://github.com/kubernetes-csi/csi-driver-nfs (via WebFetch). v4.13.2 latest; brings-your-own NFS server requirement.
- **gitleaks v8.30.1 release** — https://github.com/gitleaks/gitleaks/releases (via WebFetch). March 21, 2026 latest; no breaking detect-package API changes since v8.28.
- **Longhorn RWX on kind** — https://longhorn.io/docs/1.10.0/nodes-and-volumes/volumes/rwx-volumes/ (via WebSearch). v1.1.0+ supports RWX; share-manager pod runs NFS server inside the cluster.

### Tertiary (LOW confidence — flagged in inline `[ASSUMED]` tags)

- **go-git/v5 worktree API gap** — claim that v5 lacks a `Worktree.Add(dir, branch)` equivalent to CLI `git worktree add`. Based on ctx7 returning only `PlainOpen`/`PlainClone`/`Repository.Worktree()` APIs; no `Worktree.Add` surfaced. **Planner should verify against the latest go-git/v5 GoDoc at plan-phase before locking the per-Task PlainClone workaround in §"Pitfall 1".**

## Assumptions Log

> Claims tagged `[ASSUMED]` in this research that the planner/discuss-phase should confirm or refute:

| # | Claim | Section | Risk if Wrong |
|---|-------|---------|---------------|
| A1 | `go-git/v5` lacks a first-class `Worktree.Add(dir, branch)` API equivalent to CLI `git worktree add`; Phase 3 D-B4 worktrees-per-Task is implemented via per-Task `PlainClone(bareRepoPath, ...)`. | §"Pitfall 1: go-git/v5 worktree API gap" + Pattern 2 | If v5 DOES have a worktree-add API, Phase 3's per-Task isolation is cheaper (shared object directory). Verify against `go doc github.com/go-git/go-git/v5/...Worktree`. |

**If this table is empty:** All Phase 3 research is grounded in verified library docs + Phase 2 codebase precedents. Currently 1 assumption flagged for plan-phase confirmation.

## Open Questions (RESOLVED)

> These questions CONTEXT.md didn't pin have been resolved during plan authoring (2026-05-15). Each item below records the chosen resolution + the downstream plan that consumes it.

1. **`internal/subagent/common/` API shape — prompt-template signature. — RESOLVED**
   - Resolution: **Option (c)** — package-level `LoadPromptTemplate(role, level string) (*template.Template, error)` returning a `*template.Template`. Templates live under `internal/subagent/common/templates/{level}_{role}.tmpl` and are loaded via `go:embed templates/*.tmpl`. The caller renders against a `pkgdispatch.EnvelopeIn` struct so template fields can reference `{{.Level}}`, `{{.TaskUID}}`, `{{.Provider.Model}}`. Rationale: simpler than (a)'s wrapper struct; drop-in for next provider (the future `internal/subagent/openai/` calls the same `common.LoadPromptTemplate`).
   - Consumed in: **Plan 03-05** Task 1 (`internal/subagent/common/prompt_templates.go`).

2. **Prompt-template content — `milestone_planner.tmpl` content. — RESOLVED (Claude's Discretion)**
   - Resolution: **Punt to plan-phase author.** Plan 03-05 authors minimal v1 prompts (~10 lines each) referencing the README spec; iteration of prompt content does NOT require schema changes and is expected post-Phase-3 as the operator surfaces drift between author-intent and emitted artifacts.
   - Consumed in: **Plan 03-05** Task 1 (`internal/subagent/common/templates/{milestone,phase,plan}_planner.tmpl` + `task_executor.tmpl`).

3. **`Project.Spec.subagent.levels.{level}.params` validation — strict or permissive? — RESOLVED**
   - Resolution: **Reject-unknown at subagent startup (fail-fast)** with a documented allow-list of `{temperature, thinking_budget, top_p, top_k}` for Anthropic. Sanity-check happens in `internal/subagent/anthropic/subagent.go` (vendor+params validation in `New(Options)` or at the top of `Run`). v1.x can introduce `Project.Spec.subagent.allowUnknownParams=true` if the allow-list becomes maintenance-heavy. Rationale: catches typos at apply time vs failing silently in production; matches `--bare` flag fail-fast posture.
   - Consumed in: **Plan 03-05** Task 2 (`internal/subagent/anthropic/subagent.go` — params allow-list check next to the existing vendor-mismatch fail-fast).

4. **Chaos-resume `wait-for-signal` polling cadence. — RESOLVED**
   - Resolution: **Lock 500ms** per D-D3. No tuning unless test wall-clock budget pressure surfaces during Phase 3 verification. Phase 02.2 lesson: "Don't over-tune knobs that aren't observably broken."
   - Consumed in: **Plan 03-07** stub-subagent `wait-for-signal` mode (the polling loop body in `cmd/stub-subagent/main.go`).

5. **Push Job `gitleaks` / push-failure exit-code semantics. — RESOLVED**
   - Resolution: **Exit-code map locked**: `0` = success; `1` = generic failure (catch-all); `10` = gitleaks block (`reason=leak-detected`); `11` = lease-fail (`reason=lease-rejected`); `12` = auth-fail (`reason=auth-failed`); `13` = network/timeout (`reason=network-timeout`); `2` = invariant violation (`reason=invalid-branch` or `missing-creds`). Each exit code carries a structured `reason` field in the push-result envelope at `/workspace/envelopes/push/{project-uid}.json`. The ProjectReconciler in plan 03-08 maps `reason` to `Status.phase`: `leak-detected` → `Failed` + Condition; `lease-rejected` → `PushLeaseFailed` + Condition + LeaseFailureCount++; `auth-failed` and `network-timeout` → `Failed` with the reason surfaced.
   - Consumed in: **Plan 03-06** Task 1 (exit code constants in `cmd/tide-push/main.go`) and **Plan 03-08** Task 3 (ProjectReconciler push-completion handler reads `reason` from the envelope).

6. **`pkg/git.Fetch` — is it needed in Phase 3? — RESOLVED**
   - Resolution: **Ship `Fetch` in `pkg/git` (it's already in 03-03's `files_modified`), but the orchestrator does NOT call it in Phase 3.** Lease check at push time (per D-B6) is the detection mechanism. `Fetch` exists for future v1.x usage (e.g., a `tide refresh-remote` CLI verb or a Phase 4 dashboard refresh action) and to keep the pkg/git API surface complete + tested in isolation. Including the symbol now means no API-break later when `Fetch` lights up.
   - Consumed in: **Plan 03-03** Task 1 ships `Fetch` with unit tests; no Phase 3 caller wires it.

## Environment Availability

> Phase 3 introduces external dependencies beyond the Phase 1+2 stack:

| Dependency | Required By | Available | Version | Fallback |
|------------|------------|-----------|---------|----------|
| Go 1.26 | All Go builds | ✓ (Phase 1) | 1.26 toolchain | — |
| controller-runtime | Manager + all reconcilers | ✓ (Phase 1) | v0.24.x | — |
| go-git/v5 | `pkg/git`, `cmd/tide-push` | ✗ — Phase 3 adds | v5.19.0 (recommended pin) | — (no fallback; STACK.md commits) |
| gitleaks/v8/detect | `internal/gitleaks`, `cmd/tide-push` | ✗ — Phase 3 adds | v8.30.1 (recommended pin) | — |
| `@anthropic-ai/claude-code` (npm) | `images/claude-subagent` Dockerfile | Available via `node:22-slim` + `npm install -g` | v2.1.142 (recommended pin) | — (HARN-06 requires the CLI; no fallback) |
| Anthropic Go SDK | Reference for Usage shape only (Phase 3 doesn't import in production code per provider-firewall) | ✓ (already pinned, Phase 2 indirect) | v1.42.x | — |
| kind v0.31 | Layer B integration tests | ✓ (Phase 02.2) | v0.31.0 | — |
| docker / containerd | Image builds + kind | ✓ (Phase 1/2) | — | — |
| (optional) csi-driver-nfs OR Longhorn | RWX on kind for tests — **NOT NEEDED per Risk 3 mitigation** | — | — | **Use `--set workspaces.pvc.accessModes={ReadWriteOnce}` (Phase 02.2-03)** |
| (optional) Gitea image for ART-05 fixture test | If planner chooses Layer B push-against-real-remote test (vs Layer A unit only) | Available via `gitea/gitea:latest` Docker image | — | Layer A unit test against `git daemon` localhost is a fallback |

**Missing dependencies with no fallback:** None.

**Missing dependencies with viable fallbacks:**
- csi-driver-nfs / Longhorn — fallback to RWO override (already in chart values).
- Gitea fixture for ART-05 — fallback to Layer A unit test (less coverage but cheaper).

## Security Domain

> Required when `security_enforcement` is enabled (absent in config = enabled).

### Applicable ASVS Categories

| ASVS Category | Applies | Standard Control |
|---------------|---------|-----------------|
| V2 Authentication | yes | HTTPS+PAT for git (D-B6); signed-token for credproxy (Phase 2 D-C3); ANTHROPIC_API_KEY env from Secret (HARN-06) |
| V3 Session Management | no | Stateless Job dispatch; sessions are per-Task pod lifetime |
| V4 Access Control | yes | RBAC: dedicated `tide-push` SA with `secrets get` on Project's git secret only; `tide-subagent` SA with zero K8s verbs (Phase 2 D-A4) |
| V5 Input Validation | yes | Plan admission webhook validates DAG + file-touch (Phase 2 D-E1..E4); envelope `apiVersion + kind` rejected on mismatch (Phase 2 D-A3) |
| V6 Cryptography | yes | HMAC-SHA256 signed tokens (Phase 2 D-C3) — never hand-roll; use `crypto/hmac` + `crypto/sha256` (stdlib) |
| V8 Data Protection | yes | gitleaks/v8 on push diff (D-B3); harness redact on stdout/artifacts (Phase 2 HARN-04); raw `ANTHROPIC_API_KEY` never reaches subagent container (Phase 2 D-C4) |
| V9 Communication | yes | HTTPS to git remote (D-B6, never plain HTTP); localhost HTTPS to credproxy sidecar (Phase 2 D-C2 self-signed cert) |
| V12 Files & Resources | yes | Output-path validation (Phase 2 HARN-05); `filepath.EvalSymlinks` defense (Phase 2 Claude's Discretion); per-Task worktree confinement (D-B4) |
| V14 Configuration | yes | No host `~/.claude/` mount (HARN-06 anti-pattern); no OAuth flow (HARN-06); secrets via K8s Secret + `envFrom` (Phase 2 D-C4 + AUTH-01) |

### Known Threat Patterns for {Phase 3 stack}

| Pattern | STRIDE | Standard Mitigation |
|---------|--------|---------------------|
| Subagent commits secrets into per-run branch | I (Information Disclosure) | gitleaks scan on push diff (D-B3) + push Job exits non-zero on match (D-B3) + harness-side redact (Phase 2 HARN-04 first line of defense) |
| TIDE pushes overwrite human commits to `main` (Pitfall 13) | T (Tampering) | Per-run branch `tide/run-<project>-<unix>` never `main`; `--force-with-lease` against `lastPushedSHA` (D-B6); deploy-key scoped to `tide/*` refs (Phase 5 docs) |
| Subagent escapes declared output paths | T (Tampering) | Output-path validation post-Job (Phase 2 HARN-05) + per-Task worktree isolation (D-B4) |
| Stale lease causes push Job to clobber recent remote state | T (Tampering) | `--force-with-lease=refs/heads/<branch>:<lastPushedSHA>` (D-B6); failure → annotation-driven manual recovery, not auto-retry |
| Malicious claude-code config in container `~/.claude/` (CVE-style) | T + E (Elevation) | `--bare` flag (NEW Phase 3) skips auto-discovery of hooks/skills/plugins/MCP; container filesystem starts fresh per pod; no host mount |
| Subagent pod somehow obtains PAT from push Job's environment | I + E | Push Job runs in its own pod with its own SA; PAT Secret mounted only on push Job pod via `envFrom` (D-B1) — never on subagent pods; provider firewall lints prevent code-path leakage |
| chaos-resume reveals dispatch-state inconsistency (e.g., duplicate Jobs) | T (Tampering via inconsistent state) | Deterministic Job names (Phase 2 D-B5) + chaos-resume four-pillar assertion (D-D4) — the proof |
| gitleaks ruleset is older than the secrets being committed | I | Quarterly `tide-push` image release cadence + per-Project `Project.Spec.git.leaksConfigRef` ConfigMap override (D-B3 Claude's Discretion item) |

## Conflicts With CONTEXT.md

(empty)

**Verification:** Cross-checked every D-decision (D-A1..D-D4) against research findings. All locked decisions are consistent with verified library behavior, Phase 1+2 codebase precedents, and current docs. No conflicts to surface.

If the planner discovers a conflict during plan authoring (e.g., go-git/v5 turns out NOT to have `ForceWithLease`, or `claude --bare` turns out incompatible with `--include-partial-messages`), this section should be re-opened and the planner halts for user input.

## Metadata

**Confidence breakdown:**
- Up-stack reconciler dispatch pattern: **HIGH** — Phase 2 D-B1 (TaskReconciler) is the proven precedent; Phase 3 D-A2 extends the pattern symmetrically.
- `pkg/git` API surface: **HIGH** — go-git/v5 PushOptions.ForceWithLease verified via WebFetch; BasicAuth pattern verified across multiple ctx7 examples.
- `gitleaks/v8/detect` library integration: **HIGH** — `NewDetectorDefaultConfig()` + `DetectString()` is the canonical Go-library API; verified via ctx7.
- Claude Code stream-json schema: **HIGH** — verified against the official code.claude.com/docs/en/headless + ericbuess mirror (which agrees with official); claimed schema matches what the harness will see.
- Chaos-resume four-pillar pattern: **HIGH** — Phase 1 `TestLeaderElection` is the direct precedent for the lease-handoff portion; deterministic Job names (Phase 2 D-B5) provide the no-dup property structurally.
- RWX driver on kind for tests: **HIGH** — Phase 02.2 already proved RWO-override works (Pod-status envelope transport); no driver-install needed.
- go-git/v5 worktree API gap: **LOW** — single assumption flagged in Assumptions Log (A1). Planner verifies at plan-phase.
- Prompt template content: **LOW** — punted to plan-phase per CONTEXT.md "Claude's Discretion"; user reviews drafts.

**Research date:** 2026-05-15
**Valid until:** 2026-06-14 (30 days; stable library territory). Re-verify `claude-code` minor version pin if Phase 3 plan-start is past that date.

---

*Phase: 03-up-stack-reconcilers-git-integration-real-subagent-resumptio*
*Research conducted 2026-05-15.*
