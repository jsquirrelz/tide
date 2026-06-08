# Phase 9: Cross-namespace envelope return (in-namespace reporter) - Research

**Researched:** 2026-06-08
**Domain:** Kubernetes operator architecture — cross-namespace control-plane data flow, in-pod K8s API clients, native sidecar lifecycle, per-namespace RBAC, controller-runtime watch/materialization split
**Confidence:** HIGH (architecture, RBAC, materialization, prompt-read, stub-compat verified against codebase + K8s docs; cost-table values MEDIUM pending Haiku model confirmation)

<user_constraints>
## User Constraints (from CONTEXT.md)

### Locked Decisions

**Namespace model** — Per-namespace pods + PVCs is CORRECT and stays (tenant isolation of untrusted LLM-authored code-mutation pods from the operator). The Blocker #2/#3 "single shared RWX PVC + all pods in tide-system + Manager FilesystemEnvelopeReader" design is the superseded early-phase shortcut. Do NOT revert to it.

**Data-placement rule (governs all cross-boundary data):**
- NEVER put blobs in etcd (ConfigMaps, CRDs, `.status` are small-structured-state only).
- Large + same-namespace (repo, worktrees, diffs, build output, prompt, verbose envelope `result`) → the per-project PVC + git worktrees.
- Small structured + cross-namespace (childCRDs the Manager must act on) → the K8s API.
- Tiny + cross-namespace (usage / git SHA / exitCode / reason) → the 4 KB termination message.
- Large + cross-namespace → object store behind a pluggable interface, OFF by default, NOT in v1. MinIO and ConfigMaps explicitly rejected for this path.

**V1 envelope-return architecture** — A trusted, RBAC-scoped **in-namespace TIDE reporter** reads the subagent's `out.json` from the local same-namespace PVC and **creates the child CRs directly via the K8s API** (ownerRef → the same-namespace parent). The Manager watches them appear; materialization moves out of the Manager into the reporter. CEL admission still guards child CRDs regardless of creator. Tiny status (usage/git SHA/exitCode/reason) returns via the termination message. The verbose `result` stays on the namespace PVC as audit. The sandboxed subagent container must NOT gain kube-API/RBAC access — the reporter is a SEPARATE trusted component.

**Executor prompt read** — Move the prompt read IN-POD: the executor reads `PromptPath` from its OWN namespace PVC. The Manager-side `buildEnvelopeIn` read of `PromptPath` (committed but latent-broken on `main` at `1f8fc86`) must be replaced.

**Cost surfacing (#6)** — Resolve `costSpentCents` / budget surfacing so the medium run reports `costSpentCents > 0`.

### Claude's Discretion (genuine forks — checkpoint with user)
- **How the reporter runs relative to the sandboxed subagent:** extend the credproxy sidecar vs. dedicated reporter container (needs after-main-exit coordination) vs. Manager-spawned reader Job. Research tradeoffs + the K8s-idiomatic option; surface for user decision.
- **Reporter-creates-CRs-directly (V1)** vs. reporter-patches-parent-status-and-Manager-materializes (V2 fallback). V1 is chosen; confirm feasibility + what Manager-side logic must move.
- **Per-namespace RBAC shape** for child-CRD creation (mirror `tide-subagent`, `tide-push`).

### Deferred Ideas (OUT OF SCOPE)
- Editable / re-appliable "envelopes as first-class reviewable artifacts" DX (write/replay surface; clobbering / which-fields-are-authoritative question). Separate SUBSEQUENT phase.
- Moving envelope DELIVERY off the base64 `ENVELOPE_IN_B64` env var (input side) — only if input envelopes approach size limits; not needed for v1.
</user_constraints>

<phase_requirements>
## Phase Requirements

Requirements derive during planning. The ROADMAP success criteria map to research support as follows:

| SC | Description | Research Support |
|----|-------------|------------------|
| SC-1 | Trusted in-namespace reporter creates child CRDs via K8s API; Manager no longer reads project-namespace PVC for envelope-out; materialization moves to reporter; CEL still guards | §Reporter Execution Model, §Materialization Relocation, §Per-Namespace RBAC |
| SC-2 | Executor sources prompt in-pod from own namespace PVC (PromptPath) — fixes #10b | §Prompt Read In-Pod |
| SC-3 | Tiny status via termination message; verbose `result` stays on namespace PVC as audit | §Manager Read Path, §Termination-Message Split |
| SC-4 | `examples/projects/medium` → legitimate `Complete` with real Claude (Haiku); per-run branch pushed; costSpentCents > 0 | §Validation Architecture (live acceptance) |
| SC-5 | Defect #6 resolved — costSpentCents/budget surfaced | §Cost Surfacing |
| SC-6 | Phase 8 SC-2 closed; v1.0.0 retag unblocked | gated on SC-4 |
</phase_requirements>

## Summary

The root cause (#11/#12) is settled and not under research: PVCs are namespace-local, so the Manager in `tide-system` cannot read `out.json` written by a subagent pod into the project namespace's PVC. The locked V1 fix routes cross-namespace **control data** (child CRDs) through the Kubernetes API rather than a shared filesystem — a trusted in-namespace reporter reads the local PVC and creates the child CRs directly, and the Manager watches them appear.

This research resolves the seven open implementation questions. The central fork — how the reporter runs relative to the sandboxed subagent — has a clear K8s-idiomatic answer once you account for native-sidecar lifecycle semantics: **a dedicated reporter as a second main container in the same pod, coordinated by a sentinel file on the shared PVC, is the recommended model.** A native sidecar (restartPolicy:Always) is the wrong tool because its graceful termination is explicitly best-effort — when the main container exits, the sidecar gets SIGTERM then SIGKILL and may not finish creating CRs `[CITED: kubernetes.io/docs/concepts/workloads/pods/sidecar-containers]`. The credproxy-sidecar-extension option (a) couples a security component to a control-plane writer and inherits the same SIGKILL race. The Manager-spawned reader Job (c) is the cleanest decoupling but costs +1 Job and a full pod-churn cycle per dispatch.

The other six questions resolve cleanly against the existing code: the Manager already holds a ClusterRole with `create` on all six CRD Kinds and is already bound per-namespace via `per-namespace-rolebinding.yaml`; the reporter needs the same shape but as a least-privilege per-namespace Role (create on Milestone/Phase/Plan/Task/Wave only). Materialization moves wholesale — `MaterializeChildCRDs` + `childKindAllowlist` move into the reporter binary; the spec-ref idempotency guard (`childrenAlreadyMaterialized`, the cascade-9/10 lesson) goes WITH it because the reporter is now the create-site. The prompt-read fix is a one-file move of the `ReadPrompt` call from the Manager's `buildEnvelopeIn` into the in-pod harness. Cost surfacing is a missing price-table multiply in the anthropic runner — `ParseStream` populates token counts but never computes `EstimatedCostCents`, so `RollUpUsage` adds zero.

**Primary recommendation:** Build a `cmd/tide-reporter` Go binary that runs as a **second main container** in the subagent pod, blocks on a sentinel file the subagent writes on clean exit, reads `out.json` from the local PVC, creates child CRs via an in-cluster controller-runtime client using a new least-privilege `tide-reporter` SA, and writes the tiny status (usage/git/exitCode/reason — NOT childCRDs) to its own `/dev/termination-log`. The subagent stops writing the termination message entirely. The Manager drops `MaterializeChildCRDs` from the planner-completion handlers and relies on its existing `Owns()`/watch wiring to observe children; it still reads the tiny status for budget rollup and failure classification.

## Architectural Responsibility Map

| Capability | Primary Tier | Secondary Tier | Rationale |
|------------|-------------|----------------|-----------|
| Author child-CRD JSON files | Subagent container (sandboxed, zero K8s verbs) | — | LLM output; locked anti-pattern: subagent gets no kube API |
| Read out.json + create child CRs | **Reporter container (in project namespace, trusted)** | — | Cross-namespace control data → K8s API; reporter is same-namespace as parent (ownerRef works) |
| Read executor prompt from PVC | **Subagent harness (in-pod)** | — | PVC is namespace-local; #10b — Manager cannot read it cross-ns |
| Tiny status return (usage/git/exitCode/reason) | Reporter container → termination message | Manager (consumer) | <4KB, cross-namespace → termination message (Pod-status API) |
| Verbose `result` audit | Project-namespace PVC | — | Large + same-namespace → PVC; Manager does not need it |
| Watch children, roll up status | **Manager (tide-system)** | — | Already has cross-ns ClusterRole + Owns() watches |
| Budget cost rollup | Manager (`RollUpUsage`) | Subagent (computes EstimatedCostCents) | Price table in-pod where token counts live; controller accumulates |
| CEL admission validation | apiserver (admission) | — | Guards CRDs regardless of creator — already in place |

## Reporter Execution Model — THE CENTRAL FORK

All three options keep the locked invariants: the sandboxed subagent never gets kube-API access; the reporter is a separate trusted identity; child CRs are created in the project namespace where the parent lives (ownerRefs are namespace-local, so this is sound `[VERIFIED: codebase — owner.EnsureOwnerRef enforces same-namespace per Pitfall 23]`).

### Native sidecar lifecycle — the load-bearing constraint

The project already uses K8s 1.33 native sidecars (credproxy: `RestartPolicy: ptr.To(corev1.ContainerRestartPolicyAlways)` `[VERIFIED: internal/dispatch/podjob/jobspec.go:277]`). Two facts govern the fork:

1. **A Job completes when the main container exits, regardless of running sidecars.** "the sidecar container in each Pod does not prevent the Job from completing after the main container has finished." `[CITED: kubernetes.io/docs/concepts/workloads/pods/sidecar-containers]`
2. **Sidecar graceful termination is best-effort.** "When other containers take all allotted graceful termination time, the sidecar containers will receive the SIGTERM signal, followed by the SIGKILL signal, before they have time to terminate gracefully." `[CITED: kubernetes.io/docs/concepts/workloads/pods/sidecar-containers]`
3. **There is no built-in mechanism for a sidecar to detect main-container exit** — you must poll a shared signal (sentinel file on the shared mount, or process-namespace sharing). `[CITED: kubernetes.io/docs/concepts/workloads/pods/sidecar-containers]`

Fact 2 is fatal to any "reporter as native sidecar" design that must do real work (multiple K8s API creates) AFTER the main container exits: the kubelet may SIGKILL the sidecar mid-create. This rules option (a) and the sidecar-variant of (b) down to runner-up status.

### Option B-main: Dedicated reporter as a SECOND MAIN CONTAINER (RECOMMENDED)

A pod can have multiple `containers[]` (ordinary main containers, NOT sidecars). The Job's completion semantics with `restartPolicy: Never` and `backoffLimit: 0` (already the project default `[VERIFIED: jobspec.go:429]`) — the Pod succeeds only when ALL main containers exit 0. So a second main container that does the reporting holds the Pod open until it finishes; it is NOT subject to the sidecar SIGKILL race.

**Flow:**
1. `envelope-writer` init container writes `in.json` (unchanged).
2. `tide-credproxy` native sidecar starts (unchanged; only when a provider secret exists).
3. Two main containers start concurrently: `subagent` and `tide-reporter`.
4. `tide-reporter` blocks polling for a sentinel file (e.g. `/workspace/envelopes/<UID>/.subagent-done`) on the shared PVC mount.
5. `subagent` runs the agent loop, writes `out.json` + `children/*.json`, writes the sentinel as its LAST action, exits.
6. `tide-reporter` observes the sentinel, reads `out.json`, runs the relocated `MaterializeChildCRDs` (idempotency guard included), then writes the tiny status to its own `/dev/termination-log` and exits 0.
7. Native credproxy sidecar terminates last (reverse order) — harmless.

**How it reads out.json:** local PVC, same mount + subPath the subagent uses (`/workspace`, subPath `{project-uid}/workspace`). No cross-namespace read.

**How it creates CRs:** in-cluster controller-runtime client. Build with `sigs.k8s.io/controller-runtime/pkg/client/config.GetConfig()` (honors in-cluster service-account token automatically) + `client.New(cfg, client.Options{Scheme: scheme})` with the tide scheme registered. `[CITED: controller-runtime client docs]` The reporter runs as a new SA `tide-reporter` whose token is auto-mounted.

**How it gets the parent ownerRef:** the parent UID/name/namespace are KNOWN at dispatch time. Pass them into the reporter via env vars on the container (the Manager already stamps `tideproject.k8s/<level>-uid` labels and knows `ParentObj`/`Level` in `BuildOptions`). The reporter Gets the parent by name to obtain its live UID for the ownerRef, then calls `owner.EnsureOwnerRef`. Alternatively pass parent name+namespace+kind via env and resolve. Reuse `internal/owner.EnsureOwnerRef` (it is import-safe from a cmd binary).

**Retry / idempotency:** `MaterializeChildCRDs` already treats `AlreadyExists` as success (Pitfall F / SUB-03) `[VERIFIED: dispatch_helpers.go:369-374]`. The spec-ref guard `childrenAlreadyMaterialized` moves with it so re-dispatch (new attempt, new pod) does not double-author. If the reporter crashes mid-batch, the next attempt's reporter re-runs idempotently.

**Tiny status:** the reporter writes the termination message (usage/git/exitCode/reason — NOT childCRDs). The subagent stops writing `/dev/termination-log`. `PodStatusEnvelopeReader.ReadOut` queries by `tideproject.k8s/task-uid` and reads `ContainerStatuses` for `ContainerNameSubagent` — this MUST change to read the reporter container's terminated message instead (new container-name constant). The tiny status fits 4KB by construction once childCRDs and verbose `result` are excluded.

**Tradeoffs:** +1 container per pod (no extra Job, no pod-churn). Adds a sentinel-file handshake (simple, race-free: subagent writes sentinel last). Reporter image is a small Go binary. The PVC must be readable by both containers (it already is — same subPath mount).

**Failure modes:** subagent crashes before sentinel → reporter blocks until Job `activeDeadlineSeconds` kills the pod → Manager sees a Failed Job and no children → handled (parent stays Running/Failed per existing logic). To avoid the reporter hanging the full deadline on a crashed subagent, the reporter should ALSO poll the subagent's liveness (process-namespace sharing, or a short out.json-presence timeout) — recommend: reporter watches for EITHER the sentinel OR out.json-with-nonzero-exitCode, and a bounded internal timeout below `activeDeadlineSeconds`.

### Option A: Extend the credproxy sidecar (RUNNER-UP — rejected)

Credproxy already runs for the subagent's lifetime as a trusted in-pod component `[VERIFIED: jobspec.go:259-326]`. Teach it to detect subagent completion (sentinel) and create the CRs.

**Why rejected:**
- It is a native sidecar → subject to the SIGKILL-before-finish race (fact 2). The moment the subagent main container exits, the kubelet begins terminating the sidecar; multi-create reporting work may be cut short.
- It violates separation of concerns: credproxy is a SECURITY boundary (HMAC token validation, outbound firewall). Giving it kube-API write verbs widens its blast radius — a credproxy compromise would now also be able to create CRs. The locked decision explicitly wants the reporter as a SEPARATE trusted component.
- credproxy is gated OFF when no provider secret exists (the `$0` stub path) `[VERIFIED: jobspec.go:271]` — but the reporter is needed for BOTH stub and real runs. Coupling would force credproxy-always-on or a second path.

### Option C: Manager-spawned reader Job in the project namespace (decoupled — viable alternative)

On Job completion (the Manager already watches via `Owns(&batchv1.Job{})`), the Manager creates a second short-lived Job in the project namespace running `tide-reporter`, which reads out.json and creates the CRs.

**Tradeoffs:**
- Cleanest decoupling (reporter lifecycle independent of subagent pod).
- +1 Job per dispatch → +1 full pod schedule/pull/start/stop cycle → added latency (seconds to tens of seconds on cold image cache) and pod churn. At fan-out (many tasks) this doubles the Job count.
- The reader Job still needs the same per-namespace `tide-reporter` SA + RBAC.
- More moving parts in the Manager (a new dispatch site + its own completion watch), partially re-creating the materialization logic the phase is trying to move OUT of the Manager.

**When C wins:** if a future requirement needs the reporter to run with a different resource profile, or to retry independently of the subagent pod's TTL, or to run on real cross-namespace transfer (the deferred object-store path). For v1's in-namespace control-data return, B-main is simpler and cheaper.

### Recommendation

**Option B-main (dedicated reporter as a second main container, sentinel-file coordinated).** It avoids the sidecar SIGKILL race, adds no extra Job, keeps the reporter a distinct least-privilege identity, works identically for stub and real runs, and reuses the existing PVC mount and `MaterializeChildCRDs`/idempotency code wholesale. Surface the B-main vs C decision to the user — C is the legitimate runner-up if decoupling/independent-retry is valued over per-dispatch cost.

> **ASSUMPTION A1:** B-main assumes the reporter container can be added without tripping the credproxy gate logic or the PodSpec validation (it is an ordinary `containers[]` entry, not an init container). Verify the Job-completion semantics with two main containers + one native sidecar under `restartPolicy: Never` + `backoffLimit: 0` in envtest/kind before committing the plan — specifically that the Job is marked Complete only when BOTH main containers exit 0.

## Per-Namespace RBAC for the Reporter

Mirror the established per-namespace identity pattern (`tide-subagent` zero-verb SA, `tide-push` get-secrets SA) `[VERIFIED: charts/tide/templates/serviceaccount-subagent.yaml, push-rbac.yaml]`.

**New identity: `tide-reporter` SA + Role + RoleBinding, per project namespace.** Least-privilege Role — create only on the five child CRD Kinds, plus `get` to resolve the parent for the ownerRef:

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: tide-reporter
  namespace: {{ project-namespace }}
rules:
  # Create child CRs (planner output) + get parent for ownerRef resolution.
  - apiGroups: ["tideproject.k8s"]
    resources: ["milestones", "phases", "plans", "tasks", "waves"]
    verbs: ["create", "get"]
```

**Why not reuse the Manager's ClusterRole binding?** The Manager already has `create` on all six Kinds via a ClusterRole bound per-namespace (`tide-orchestrator-{ns}` → `tide-manager-role`) `[VERIFIED: charts/tide/templates/per-namespace-rolebinding.yaml, manager-rbac.yaml]`. But the reporter runs in the project namespace with a DIFFERENT (untrusted-adjacent) blast radius than the operator; it must NOT inherit the Manager's broad verbs (delete/update/patch/finalizers/status on all Kinds, plus the core-resource grants the Manager holds). Least-privilege: create+get on child Kinds only. No list/watch/update/delete/patch. No `projects` create (the Project is human-applied). No secrets, no core resources.

**Provisioning, two surfaces (mirror the existing split):**
1. **Chart template** `charts/tide/templates/reporter-rbac.yaml` — renders the SA+Role+RoleBinding in `.Release.Namespace` (for co-located Projects), and additionally per entry in `.Values.projectNamespaces` (mirror `per-namespace-rolebinding.yaml`'s `range`). Source-of-truth under SOT discipline; mirrored by `hack/helm/augment-tide-chart.sh` per CLAUDE.md chart-is-fixed-contract.
2. **Medium sample** `examples/projects/medium/per-namespace-resources.yaml` — add `tide-reporter` SA+Role+RoleBinding alongside the existing `tide-subagent` + `tide-push` blocks `[VERIFIED: that file already mirrors tide-subagent + tide-push for the same reason — the controller's Jobs run in the project namespace]`.

**PodSpec change:** the reporter container/Job runs as `serviceAccountName: tide-reporter`. The subagent container stays `serviceAccountName: tide-subagent` (zero verbs). Since a Pod has ONE serviceAccountName, the two-main-container B-main model means BOTH containers share the pod SA. **This is a real constraint:** if subagent and reporter are in the SAME pod, they share the SA token — the subagent would gain the reporter's create verbs, violating the locked "subagent gets no kube API" anti-pattern.

> **ASSUMPTION A2 — must resolve in planning:** Option B-main as a single pod shares one ServiceAccount across both containers. To preserve subagent zero-verbs, EITHER (i) the reporter runs as a SEPARATE pod/Job (Option C) so it can have its own SA; OR (ii) accept that the pod SA is `tide-reporter` but the subagent's auto-mounted token is rendered unusable to it (e.g. `automountServiceAccountToken` cannot be per-container; the token is at a known path — the subagent image simply never reads it, and the credproxy firewall + network policy prevent the subagent from reaching the apiserver). Option (ii) weakens the guarantee from "cannot" to "does not"; the locked decision says the subagent must NOT gain access. **This SA-sharing constraint is the strongest argument for Option C (separate reporter Job/pod with its own SA).** Surface this to the user explicitly — it may flip the fork recommendation from B-main to C.

This is the single most important open question for the planner. Per-container ServiceAccounts do not exist in Kubernetes v1.33; a pod has exactly one SA. `[ASSUMED — verify: no per-container SA in K8s 1.33]`

## Materialization Relocation

**Moves OUT of the Manager INTO the reporter binary:**
- `MaterializeChildCRDs` (the Kind-allowlist + typed-unmarshal + ownerRef + create loop) — `[VERIFIED: dispatch_helpers.go:289-377]`. Move to a package importable by `cmd/tide-reporter` (e.g. keep in `internal/controller` only if cmd can import it, else lift to `internal/reporter` or `pkg/dispatch`). The Task-prompt-path wiring (`tk.Spec.PromptPath = child.SourcePath`, dispatch_helpers.go:349) moves with it.
- `childKindAllowlist` (the T-308 mitigation) — moves with `MaterializeChildCRDs`. Note the runner-side mirror in `internal/subagent/anthropic/subagent.go:334` stays (defense-in-depth).
- `childrenAlreadyMaterialized` (the cascade-9/10/11 spec-ref idempotency guard) — **moves WITH materialization** because the reporter is now the create-site. The guard's logic is unchanged: match by parent-specRef (`Phase.spec.milestoneRef`, etc.) with `metav1.IsControlledBy` fallback `[VERIFIED: dispatch_helpers.go:236-287]`. The cascade-9/10 lesson — "guard by spec-parent-ref, NOT ownerRef, because ownerRef is set asynchronously by the child's reconciler while specRef is set synchronously at apply" — translates intact: the reporter sets specRef at create time (synchronous, race-free); ownerRef remains the async fallback. `[VERIFIED: CLAUDE.md cascade-9/10 lesson + dispatch_helpers.go:218-235 comment]`

**STAYS in the Manager (it now WATCHES instead of materializes):**
- The `Owns(&Milestone{})` / `Owns(&Phase{})` / etc. watch wiring — already present (each up-stack reconciler Owns its child Kind). When the reporter creates a child, the Manager's existing watch re-enqueues the parent; the parent's reconciler runs its `checkComplete`/roll-up logic. `[VERIFIED: project_controller.go:826-828 comment "Owns(&Milestone{}) watch re-enqueues"]`
- The `handle*JobCompletion` handlers stay but their BODY changes: drop the `ReadOut` → `MaterializeChildCRDs` block. Keep reading the tiny status (usage/git/exitCode/reason) for budget rollup and failure classification (see §Manager Read Path).
- CEL admission (`x-kubernetes-validations` on the CRDs) guards children **regardless of creator** — no change needed. The apiserver validates on create whether the creator is the Manager or the reporter. `[VERIFIED: CEL is admission-time; locked decision confirms "CEL admission still guards the child CRDs regardless of creator"]`

**Per-reconciler completion handlers to edit** (all four read `EnvelopeOut.ChildCRDs` + materialize today): `project_controller.go:handleProjectJobCompletion` (line ~790), `milestone_controller.go`, `phase_controller.go`, `plan_controller.go`. `[VERIFIED: CONTEXT canonical_refs + project_controller.go:813-823]`

## Manager Read Path (tiny-status consumer)

Today the Manager uses `PodStatusEnvelopeReader` (termination message, with `FilesystemEnvelopeReader` same-namespace fallback) `[VERIFIED: backend.go:143-178; debug doc lines 124-126 — main restored to this after #11 revert]`.

**Under V1, for planner levels the Manager no longer needs the full envelope** — children arrive as CRs via the reporter and the watch. The Manager STILL needs from the tiny status:
- **Usage** → `RollUpUsage` for budget rollup `[VERIFIED: task_controller.go:796]`.
- **Git HeadSHA** → `EnvelopeOut.Git.HeadSHA` for per-run branch tracking `[VERIFIED: envelope.go:175-191 GitOutput.HeadSHA]`.
- **ExitCode / Reason** → failure classification (`conditionReasonFromEnvelopeResult`) `[VERIFIED: task_controller.go:767-786]`.

**Reader changes:**
- `PodStatusEnvelopeReader.ReadOut` currently scans `ContainerStatuses` for `ContainerNameSubagent` `[VERIFIED: backend.go:159]`. Change to read the REPORTER container's terminated message (new constant `ContainerNameReporter`), because the subagent stops writing the termination message.
- The tiny status is a strict subset of `EnvelopeOut` (usage/git/exitCode/reason, no childCRDs, no verbose result) — it deserializes into the same `EnvelopeOut` struct with `ChildCRDs` empty and `Result` empty/short. No schema change strictly required, but a dedicated `TerminationStub` type (as attempted in the reverted #11 work, `pkg/dispatch/envelope.go` — debug doc line 715) would make the <4KB guarantee explicit and testable. **Recommend re-introducing a small `TerminationStub` type** (the #11 attempt's `TerminationStub` + `NewTerminationStub` + `TestNewTerminationStub_StaysSmall` were sound; only their PVC-authoritative-read PREMISE was wrong — the stub-writer half was even live-confirmed). `[VERIFIED: debug doc lines 712-737]`
- The `FilesystemEnvelopeReader` fallback for `ReadOut` should be REMOVED for the cross-namespace planner path (it never worked cross-namespace — that is the whole #12 root cause). Keep `FilesystemEnvelopeReader` only for same-namespace local-test setups if any remain.

**`handleJobCompletion` per level** reconciles by: read tiny status from reporter termination message → if exitCode≠0 mark parent Failed → else roll up usage + git → DO NOT materialize (children arrive via watch) → existing requeue/boundary logic unchanged (#9 `hasChildPhases` guard etc. still applies because children now appear via the reporter+watch). `[VERIFIED: milestone_controller.go #9 fix still relies on children being visible — now visible via reporter create + watch]`

## Prompt Read In-Pod (#10b fix)

Today: `TaskReconciler.buildEnvelopeIn` calls `r.Deps.PromptReader.ReadPrompt(ctx, project.UID, task.Spec.PromptPath)` — the MANAGER reads the prompt off ITS PVC `[VERIFIED: task_controller.go:1064]`. This is the latent cross-namespace bug (same class as #12): the Manager's `/workspaces` PVC is a different namespace-local volume than the project's.

**Fix:** the prompt read moves IN-POD. The executor reads `PromptPath` from its OWN namespace PVC.

**Where in the in-pod harness:** The `envelope-writer` init container writes `in.json` from the base64 env var `[VERIFIED: jobspec.go:233-257]`. `EnvelopeIn` is delivered fully-formed today (Manager marshals it, including `Prompt`). The fix: the Manager stops setting `EnvelopeIn.Prompt` for executors and instead passes `PromptPath` (workspace-relative) into the envelope; the IN-POD subagent harness reads `.spec.prompt` from `<workspace>/<promptPath>` and renders the template against it.

**Two implementation shapes (planner picks):**
1. **Subagent harness reads it** — `cmd/claude-subagent` / `cmd/stub-subagent` (or `anthropic.Run`) reads `EnvelopeIn.PromptPath` from the local PVC (the existing `FilesystemEnvelopeReader.ReadPrompt` path-traversal-defended logic at backend.go:112-141 can move into the in-pod binary verbatim — it is pure filesystem, no K8s deps). Render `{{.Prompt}}` from the file content. Symmetric with how the runner already reads `children/*.json` in-pod.
2. **A prompt-reader init container** stamps the prompt into `in.json` before the subagent starts. More moving parts; option 1 is simpler and reuses the existing traversal-defense code.

**Recommend option 1** — move the `ReadPrompt` filesystem logic into the in-pod harness, add `PromptPath` to `EnvelopeIn`, remove `r.Deps.PromptReader` from the Manager's `buildEnvelopeIn`. The executor template renders `{{.Prompt}}` from the in-pod-read content. `[VERIFIED: task_executor.tmpl renders {{.Prompt}} per debug doc line 559]`

**Coupling:** `TaskSpec.PromptPath` (required, MinLength=1) stays as the CRD field `[VERIFIED: api/v1alpha1/task_types.go, debug doc line 622-640]`; only WHO reads it (Manager → in-pod) changes. The materializer still stamps `child.SourcePath → Task.Spec.PromptPath` (now in the reporter).

## Cost Surfacing (#6)

**Root cause confirmed:** `EstimatedCostCents` is NEVER computed by the real runner. The flow is `out.Usage.EstimatedCostCents` → `RollUpUsage(... usage)` → `Status.Budget.CostSpentCents += usage.EstimatedCostCents` `[VERIFIED: budget/tally.go:51]`. But `ParseStream` (anthropic) populates only token counts — `usage.InputTokens/OutputTokens/CacheReadTokens/CacheCreationTokens` — and NEVER sets `EstimatedCostCents` `[VERIFIED: internal/subagent/anthropic/stream_parser.go:99-107; subagent.go:269-276 assembles EnvelopeOut with Usage but no cost compute]`. Only the STUB sets it (hardcoded 0/1) `[VERIFIED: cmd/stub-subagent/main.go:394,420,464]`. So real runs roll up zero → `budget: {}` empty → `costSpentCents=0` despite real token bills (e.g. inputTokens=16579, outputTokens=7493).

**Where the price table / cost computation should live:** IN THE SUBAGENT (where token counts and the resolved model are both known), NOT in the controller. Rationale: the controller's `RollUpUsage` is provider-agnostic and just accumulates cents; the per-model price table is provider-specific and belongs behind the `Subagent` interface (CLAUDE.md anti-pattern: all anthropic-specific code behind the interface in `internal/subagent/anthropic/`). The runner has `in.Provider.Model` and the parsed `Usage` — multiply and set `usage.EstimatedCostCents` before assembling `EnvelopeOut`.

**Minimal fix:**
1. Add a per-model price table to `internal/subagent/anthropic/` (e.g. `pricing.go`): map model → {inputCentsPerMTok, outputCentsPerMTok, cacheReadCentsPerMTok, cacheWriteCentsPerMTok}.
2. In `anthropic.Run`, after `ParseStream`, compute `usage.EstimatedCostCents = ceil(inputTokens*inPrice + outputTokens*outPrice + cacheRead*cacheReadPrice + cacheCreation*cacheWritePrice)` and set it on `out.Usage` before returning. `[VERIFIED: subagent.go:269-276 is the assembly point]`
3. The tiny status (reporter termination message) carries `Usage` including the now-populated `EstimatedCostCents`; the Manager's `RollUpUsage` accumulates it → `costSpentCents > 0`.

**Price values (Claude Haiku — the medium sample's model):** Claude 3.5 Haiku is **$1/M input, $5/M output**; cache write 1.25× input (5-min TTL); cache read 90% cheaper than input (~$0.10/M). `[CITED: platform.claude.com/docs/en/about-claude/pricing]` In cents/MTok: input=100, output=500, cacheWrite=125, cacheRead=10. The table must key on the EXACT model string the medium sample resolves (confirm against `examples/projects/medium/project.yaml` `subagent.model` and the chart default).

> **ASSUMPTION A3:** Exact Haiku model ID + current per-MTok prices need confirmation against the live pricing page and the medium sample's resolved model string at planning time. Prices drift; the model may be `claude-3-5-haiku-*` or a 4.5 Haiku. The cost-table VALUES are MEDIUM confidence; the cost-computation LOCATION (in-runner) is HIGH.

## Stub Compatibility

CI dispatches ONLY the stub `[VERIFIED: CONTEXT specifics line 80; ROADMAP Phase 8 SC-5]`. The reporter MUST work for both stub and real `out.json`.

**Stub out.json shape vs reporter expectation:**
- The stub builds `ChildCRDs` directly in Go (`dispatchPlannerSuccess`) and writes them into `out.json` `[VERIFIED: cmd/stub-subagent/main.go:340-352]`. The reporter reads `EnvelopeOut.ChildCRDs` from `out.json` — IDENTICAL field for stub and real. The real runner populates `ChildCRDs` via `readChildCRDs` from `children/*.json` `[VERIFIED: anthropic/subagent.go:296-313]` then writes them into `out.json` (cmd/claude-subagent). So by the time `out.json` is written, BOTH paths have `EnvelopeOut.ChildCRDs` populated. **The reporter reads `out.json`'s `ChildCRDs` field — agnostic to how it was produced.** ✓
- **`SourcePath` for Task prompts:** the real runner stamps `child.SourcePath` `[VERIFIED: anthropic/subagent.go:427]`; the stub must also set `SourcePath` on Task children if the reporter copies it to `PromptPath` (the materializer does `tk.Spec.PromptPath = child.SourcePath`). Check whether the stub sets SourcePath on its Task children — if not, the reporter's `PromptPath` would be empty and Create would fail (MinLength=1). **Action item for planning:** confirm/add `SourcePath` on stub-authored Task children OR ensure the stub writes the prompt artifact the executor reads.
- **Sentinel file (B-main):** both stub and real subagent must write the sentinel as their last action. Add the sentinel write to both `cmd/stub-subagent` and `cmd/claude-subagent` shims (after `writeEnvelope(outPath, ...)`).
- **Termination message:** both currently write the FULL envelope to `/dev/termination-log` `[VERIFIED: stub main.go:207-209, claude-subagent main.go:129-131]`. Both must STOP (the reporter writes the tiny status instead). Remove the `writeEnvelope`-to-termination-log call from both subagent shims.

## Common Pitfalls

### Pitfall 1: Reporter SIGKILLed before finishing CR creation
**What goes wrong:** A reporter implemented as a native sidecar gets SIGTERM→SIGKILL when the main subagent exits and may not finish creating all child CRs.
**Why:** native sidecar graceful termination is best-effort `[CITED: kubernetes.io/docs/concepts/workloads/pods/sidecar-containers]`.
**How to avoid:** run the reporter as a MAIN container (B-main) or a separate Job (C) — not a sidecar.
**Warning signs:** intermittent "some children materialized, some missing" on Job completion.

### Pitfall 2: Shared ServiceAccount leaks create-verbs to the sandboxed subagent
**What goes wrong:** B-main single-pod shares one SA across subagent + reporter; the subagent inherits the reporter's create verbs — violating the zero-verb anti-pattern.
**Why:** Kubernetes has no per-container SA.
**How to avoid:** separate reporter pod/Job (C) with its own SA, OR network-isolate the subagent from the apiserver (weaker). **Resolve before coding.**
**Warning signs:** `kubectl auth can-i create milestones --as=system:serviceaccount:<ns>:tide-reporter` succeeds AND the subagent container can reach the apiserver.

### Pitfall 3: Idempotency guard left in the Manager after materialization moves
**What goes wrong:** double-authored child subtrees on re-dispatch if `childrenAlreadyMaterialized` stays in the Manager while creation moves to the reporter.
**Why:** the guard must live at the create-site (cascade-11 lesson) `[VERIFIED: dispatch_helpers.go:218-235]`.
**How to avoid:** move the guard WITH `MaterializeChildCRDs` into the reporter.
**Warning signs:** duplicate Milestones/Phases on a retried planner dispatch.

### Pitfall 4: Cost still zero because price table keyed on wrong model string
**What goes wrong:** `EstimatedCostCents` stays 0 because the price-table lookup misses the resolved model ID.
**How to avoid:** key the table on the exact `in.Provider.Model` string; add a fail-loud log (or a conservative default price) on table miss rather than silent 0.

### Pitfall 5: PromptPath read still cross-namespace
**What goes wrong:** moving `MaterializeChildCRDs` but forgetting to move the `ReadPrompt` call leaves #10b's cross-ns bug live.
**How to avoid:** remove `r.Deps.PromptReader` from `buildEnvelopeIn`; read in-pod.
**Warning signs:** executor dispatch fails with `read prompt ... no such file or directory` from the Manager's PVC.

## State of the Art

| Old Approach | Current Approach | When Changed | Impact |
|--------------|------------------|--------------|--------|
| Manager reads out.json from PVC (FilesystemEnvelopeReader) | Reporter creates CRs via K8s API; Manager watches | Phase 9 (this) | Cross-namespace control data works |
| Full EnvelopeOut in termination message (4KB truncation) | Tiny status only; verbose result on PVC audit | Phase 9 | No 4KB overflow (#11) |
| Manager reads PromptPath from its PVC | In-pod prompt read | Phase 9 | Fixes #10b latent cross-ns bug |
| Cost computed nowhere (real runner) | Price table in anthropic runner | Phase 9 | costSpentCents > 0 (#6) |

**Deprecated/superseded:**
- Single shared RWX `tide-projects` PVC + all pods in tide-system (Blocker #2/#3) — superseded by per-namespace isolation (locked).
- The #11 `TerminationStub` PVC-authoritative-read premise — reverted; the stub-WRITER half is reusable, the Manager-reads-PVC half is not.

## Runtime State Inventory

> Code/config-only phase (no rename/migration of stored data). Inventory for completeness:

| Category | Items Found | Action Required |
|----------|-------------|------------------|
| Stored data | None — no datastore keys/IDs change | None |
| Live service config | New `tide-reporter` SA+RBAC must exist in each project namespace (medium sample + chart) | Add to per-namespace-resources.yaml + chart template |
| OS-registered state | None | None |
| Secrets/env vars | Reporter container needs parent-name/ns/kind env vars (no new secrets) | Stamp in BuildOptions/jobspec |
| Build artifacts | New `cmd/tide-reporter` binary + image; both subagent images change (drop termination-log write, add sentinel) | Build + load `tide-reporter` image; rebuild subagent images |

## Validation Architecture

### Test Framework
| Property | Value |
|----------|-------|
| Framework | Ginkgo v2.28 + Gomega (integration); std `testing` (unit) `[VERIFIED: go.mod, test/integration]` |
| Config files | `test/integration/kind/cluster.yaml` (kindest/node:v1.33.7) ; envtest via `setup-envtest` |
| Quick run | `go test ./internal/... ./cmd/... ./pkg/... -short -count=1` |
| Layer A (envtest) | `make test-int-fast` (~90s, no Docker) |
| Layer B (kind) | `make test-int` (Layer A + kind) |
| Live acceptance | `make acceptance-v1` ($ cap, needs ANTHROPIC_API_KEY) / live minikube medium run |

### Phase Requirements → Test Map
| SC | Behavior | Test Type | Automated Command | File Exists? |
|----|----------|-----------|-------------------|-------------|
| SC-1 | Reporter creates child CRs in project ns via API; Manager observes via watch | integration (envtest) | new `reporter_materialize_test.go` — apply parent, run reporter logic with fake/real client, assert child CR exists with ownerRef + specRef | ❌ Wave 0 |
| SC-1 | Idempotency: re-run reporter → no duplicate children (spec-ref guard) | unit | new `MaterializeChildCRDs`/`childrenAlreadyMaterialized` test in reporter pkg (move existing dispatch_helpers tests) | ✅ move existing |
| SC-1 | CEL admission still rejects invalid child regardless of creator | integration (envtest) | extend `admission_test.go` — create via reporter SA, assert CEL rejects bad spec | ✅ extend |
| SC-1 | Two-main-container + sidecar Job completes only when both main exit 0 | integration (kind) | new `reporter_pod_test.go` (Layer B) | ❌ Wave 0 |
| SC-2 | Executor reads PromptPath in-pod | unit | test in-pod harness reads `.spec.prompt` from local PVC path | ❌ Wave 0 |
| SC-3 | Tiny status < 4KB; childCRDs NOT in termination message | unit | `TestTerminationStub_StaysSmall` (re-introduce from #11) | ❌ Wave 0 (resurrect) |
| SC-3 | PodStatusEnvelopeReader reads reporter container's terminated message | unit | extend `backend_test.go` | ✅ extend |
| SC-5 | EstimatedCostCents computed from token counts × price table | unit | new `pricing_test.go` in anthropic pkg | ❌ Wave 0 |
| SC-5 | RollUpUsage accumulates cost from tiny status | unit | existing `tally_test.go` | ✅ |
| SC-4/SC-6 | Medium → legitimate Complete, branch pushed, costSpentCents>0 | live (manual/CI) | live minikube medium run + Phase 8 SC-2 re-verify | ❌ manual gate |
| stub-compat | Reporter works for stub out.json (incl. SourcePath on Task children) | integration (kind) | extend `bare_project_test.go` / `up_stack_dispatch_test.go` | ✅ extend |

### Sampling Rate
- **Per task commit:** `go test ./<changed-pkg>/... -short -count=1`
- **Per wave merge:** `make test-int-fast` (Layer A envtest)
- **Phase gate:** full `make test-int` green (Layer A + Layer B) + live medium run → Project=Complete with costSpentCents>0 + per-run branch on the in-cluster http:// remote.

### Wave 0 Gaps
- [ ] `cmd/tide-reporter/main_test.go` + the reporter materialize/idempotency tests (move from dispatch_helpers_test.go)
- [ ] `test/integration/kind/reporter_pod_test.go` — two-main-container Job completion semantics
- [ ] `test/integration/envtest/reporter_materialize_test.go` — cross-ns create + ownerRef + watch observability
- [ ] `internal/subagent/anthropic/pricing_test.go` — per-model cost computation
- [ ] in-pod prompt-read test (subagent harness)
- [ ] resurrect `TestTerminationStub_StaysSmall` (#11 had it; reuse the writer-half, drop the PVC-read premise)
- [ ] Framework: existing infra covers Ginkgo/envtest/kind — no install needed

## Security Domain

### Applicable ASVS Categories
| ASVS Category | Applies | Standard Control |
|---------------|---------|-----------------|
| V1 Architecture / Trust boundaries | yes | Reporter is a distinct least-privilege identity; subagent stays zero-verb; trust boundary at the apiserver |
| V4 Access Control | yes | Per-namespace Role (create+get on 5 child Kinds only); no list/watch/delete; no secrets |
| V5 Input Validation | yes | Kind allowlist (T-308) + CEL admission + path-traversal defense on PromptPath read (reuse existing) |
| V6 Cryptography | no (unchanged) | HMAC signed-token + credproxy unchanged this phase |

### Known Threat Patterns
| Pattern | STRIDE | Standard Mitigation |
|---------|--------|---------------------|
| Subagent escalates to kube API via shared pod SA (Pitfall 2) | Elevation of Privilege | Separate reporter pod/Job with own SA (Option C), or network-isolate subagent from apiserver |
| Poisoned children/*.json declares non-TIDE Kind | Tampering | Kind allowlist enforced in reporter (moved) + runner mirror + CEL — defense in depth |
| Path traversal via PromptPath | Tampering | Reuse `ReadPrompt` traversal defense (abs-reject, ../-reject, base-prefix check) in-pod `[VERIFIED: backend.go:112-141]` |
| Reporter over-privileged | Elevation | Least-privilege Role: create+get on 5 Kinds, nothing else |

## Open Questions

1. **ServiceAccount sharing in B-main (THE blocker question)** — A single pod has one SA. B-main as one pod forces subagent + reporter to share it, leaking create-verbs to the subagent. *Recommendation:* this likely flips the fork to Option C (separate reporter Job with its own `tide-reporter` SA), OR accept B-main with a network-policy isolating the subagent from the apiserver. **Checkpoint with user.**
2. **Exact Haiku model ID + current prices** — confirm against live pricing page + medium sample's resolved model at planning. (ASSUMPTION A3)
3. **Stub Task children SourcePath** — confirm the stub sets `SourcePath` on Task children (needed for `PromptPath` MinLength=1) or add it.
4. **MaterializeChildCRDs package location** — `cmd/tide-reporter` must import the move target; decide `internal/reporter` vs lifting into `pkg/dispatch` (import-firewall check: cmd may import internal; the reporter needs `api/v1alpha1` + `internal/owner` — both import-safe).

## Sources

### Primary (HIGH confidence)
- Codebase (verified by direct read): `internal/dispatch/podjob/{backend,jobspec}.go`, `internal/controller/{dispatch_helpers,task_controller,project_controller}.go`, `internal/subagent/anthropic/{subagent,stream_parser}.go`, `internal/budget/tally.go`, `pkg/dispatch/{envelope,childcrd}.go`, `cmd/{stub,claude}-subagent/main.go`, `charts/tide/templates/{serviceaccount-subagent,push-rbac,per-namespace-rolebinding,manager-rbac}.yaml`, `examples/projects/medium/per-namespace-resources.yaml`, `go.mod`.
- `.planning/debug/real-claude-authoring-path.md` — defect chain + V1 decision.
- `.planning/phases/09-.../09-CONTEXT.md` — locked decisions.
- [Sidecar Containers | Kubernetes](https://kubernetes.io/docs/concepts/workloads/pods/sidecar-containers/) — native sidecar termination order + best-effort graceful termination + Job completion semantics.
- [Pricing | Claude API Docs](https://platform.claude.com/docs/en/about-claude/pricing) — Haiku pricing.

### Secondary (MEDIUM confidence)
- [Pod Lifecycle | Kubernetes](https://kubernetes.io/docs/concepts/workloads/pods/pod-lifecycle/), [Start Sidecar First (K8s blog)](https://kubernetes.io/blog/2025/06/03/start-sidecar-first/) — sidecar lifecycle corroboration.
- Anthropic pricing aggregators (cross-check Haiku $1/M in, $5/M out).

## Assumptions Log

| # | Claim | Section | Risk if Wrong |
|---|-------|---------|---------------|
| A1 | Two main containers + 1 native sidecar Job completes only when both main exit 0 under restartPolicy:Never/backoffLimit:0 | Reporter Execution Model | If a sidecar/second-main interaction differs, B-main coordination breaks — verify in kind before plan lock |
| A2 | No per-container ServiceAccount in K8s 1.33; B-main single pod shares SA → may force Option C | Per-Namespace RBAC, Open Q1 | Could flip the fork recommendation; HIGH-impact, resolve first |
| A3 | Haiku = $1/M input, $5/M output (cents: 100/500); model ID per medium sample | Cost Surfacing | costSpentCents wrong magnitude (still >0, still passes DoD, but inaccurate) |

## Metadata

**Confidence breakdown:**
- Reporter model + sidecar lifecycle: HIGH — K8s docs explicit; SA-sharing constraint surfaced as the key open question.
- Materialization relocation + idempotency: HIGH — verified against code + cascade-9/10/11 lesson.
- Prompt-read in-pod: HIGH — verified call site + reusable traversal-defense code.
- Cost surfacing: HIGH on location/mechanism, MEDIUM on price values.
- Per-namespace RBAC: HIGH — mirrors existing tide-subagent/tide-push pattern.
- Stub compat: HIGH — verified out.json shape parity; one action item (SourcePath on stub Task children).

**Research date:** 2026-06-08
**Valid until:** 2026-07-08 (stable; pricing values 2026-06-22)
