# Phase 9: Cross-namespace envelope return (in-namespace reporter) - Context

**Gathered:** 2026-06-08
**Status:** Ready for planning
**Source:** Design discussion during debug session `real-claude-authoring-path` (defects #11/#12)

<domain>
## Phase Boundary

**Delivers:** the V1 mechanism for the Manager (in `tide-system`) to receive planner/executor authoring results from subagent pods that run in a *different* (the project's) namespace.

**Root cause this phase fixes (#11/#12):** PVCs are namespace-local. The Manager mounts `tide-system/tide-projects`; subagent pods run in the project namespace (Jobs are created in `task.Namespace`, `internal/dispatch/podjob/backend.go:295`) and write `out.json` to *that* namespace's PVC. So the Manager's `FilesystemEnvelopeReader` cannot see it. The "Manager-reads-PVC" premise — behind both #11's reader rewiring and #10b's prompt read — was always wrong cross-namespace; it was masked for the whole project history because only the termination message (Pod-status API, cross-namespace) ever carried results, and only worked while envelopes stayed under the 4 KB cap.

**This phase GATES the v1.0.0 retag.** v1.0.0 cannot ship until the medium sample drives to a legitimate `Complete` with real Claude over this mechanism.

**In scope:** the in-namespace reporter + RBAC, moving the executor prompt read in-pod, the tiny-status-via-termination-message split, surfacing cost on Project status (#6), and driving `examples/projects/medium` to a legitimate end-to-end `Complete`.

**Out of scope (deferred to a SUBSEQUENT phase):** the editable / re-appliable "envelopes as first-class reviewable artifacts" developer experience, and the clobbering / which-envelope-fields-are-authoritative-vs-regenerated-per-dispatch question.
</domain>

<decisions>
## Implementation Decisions (LOCKED)

### Namespace model
- Per-namespace pods + PVCs is CORRECT and stays. Rationale: tenant isolation of untrusted, LLM-authored code-mutation pods from the operator (Manager, signing key, cluster RBAC). The Blocker #2/#3 "single shared RWX PVC + all pods in tide-system + Manager FilesystemEnvelopeReader" design (`internal/dispatch/podjob/doc.go`) is the superseded early-phase shortcut — do NOT revert to it.

### Data-placement rule (governs all cross-boundary data)
- NEVER put blobs in etcd (ConfigMaps, CRDs, `.status` are small-structured-state only).
- Large + same-namespace (repo, worktrees, diffs, build output, prompt, verbose envelope `result`) → the per-project PVC + git worktrees. (Already the design; this is "large blobs between task pods.")
- Small structured + cross-namespace (childCRDs the Manager must act on) → the K8s API (see V1 below).
- Tiny + cross-namespace (usage / git SHA / exitCode / reason) → the 4 KB termination message.
- Large + cross-namespace → object store behind a pluggable interface, OFF by default — NOT in v1 (v1 has no large cross-namespace transfer). MinIO and ConfigMaps were explicitly rejected for this path.

### V1 envelope-return architecture
- A trusted, RBAC-scoped **in-namespace TIDE reporter** reads the subagent's `out.json` from the local (same-namespace) PVC and **creates the child CRs directly via the K8s API** (ownerRef → the same-namespace parent). The Manager watches them appear; materialization moves out of the Manager into the reporter. CEL admission still guards the child CRDs regardless of creator.
- Tiny status (usage / git SHA / exitCode / reason) returns via the termination message (fits 4 KB once childCRDs are no longer in it).
- The verbose `result` stays on the namespace PVC as the audit artifact — the Manager does not need it.
- The sandboxed subagent container must NOT gain kube-API/RBAC access (anti-pattern) — the reporter is a SEPARATE trusted component.

### Executor prompt read (fix #10b's latent cross-ns bug)
- Move the prompt read IN-POD: the executor reads `PromptPath` from its OWN namespace PVC. The Manager-side `buildEnvelopeIn` read of `PromptPath` (committed but latent-broken on `main` at `1f8fc86`) must be replaced. Same cross-namespace bug class as #11/#12.

### Cost surfacing (#6)
- Resolve `costSpentCents` / budget surfacing on Project status so the medium run reports `costSpentCents > 0` (the DoD requires it).

### Reporter execution model — RESOLVED 2026-06-08 (user picked Option C)
- **Manager-spawned reader Job.** On dispatch-Job completion, the Manager spawns a short-lived reader Job in the *project* namespace with its OWN least-privilege ServiceAccount (CR-create verbs on Milestone/Phase/Plan/Task); it reads `out.json` from the local same-namespace PVC, creates the child CRs (ownerRef → same-namespace parent), then exits. The Manager watches the children appear.
- **Why C (decision-shaping constraint):** Kubernetes has NO per-container ServiceAccount — all containers in a pod share one SA. An in-pod reporter (Options A/B) would expose CR-create verbs to the sandboxed subagent container (mitigable only via `automountServiceAccountToken:false` + per-container token projection — subtle), and native-sidecar termination is best-effort (SIGKILL race could kill the reporter mid-CR-create). Option C gives the reporter its own SA by construction and runs to completion independently. Cost accepted: +1 short Job per dispatch (pod churn + seconds of latency).
- The dispatch Job's termination message still carries the tiny status (usage/git/exitCode/reason) for the Manager's budget rollup + failure classification; the reader Job handles only childCRD creation. `out.json` (incl. verbose result) stays on the namespace PVC.

### Still Claude's Discretion (resolve in planning per RESEARCH.md)
- Materialization relocation: `MaterializeChildCRDs` + `childKindAllowlist` + the `childrenAlreadyMaterialized` spec-parent-ref idempotency guard (NOT ownerRef — cascade-9/10/11 lesson) move into the reader-Job binary; the Manager keeps its `Owns()` watches and stops materializing. CEL admission still guards the CRDs.
- Per-namespace RBAC: a reporter SA + Role/RoleBinding (least-privilege create on the child kinds), mirroring how `tide-subagent`/`tide-push` SAs are provisioned (chart + medium per-namespace-resources.yaml).
- Cost (#6): per-model price table in `internal/subagent/anthropic/` (the runner never computes `EstimatedCostCents` today → `RollUpUsage` accumulates zero); confirm Haiku model ID + current prices.
- Stub compatibility: the reader Job must consume BOTH stub- and real-authored `out.json`; confirm/add `SourcePath` on stub Task children (PromptPath MinLength=1).
</decisions>

<canonical_refs>
## Canonical References

**Downstream agents MUST read these before planning or implementing.**

### Decision record (authoritative for this phase)
- `.planning/debug/real-claude-authoring-path.md` — full #2–#12 defect chain, the namespace-locality root cause, and the V1 decision (status: root-cause-locked-routed-to-phase).
- `.planning/ROADMAP.md` (Phase 9 section) — goal + 6 success criteria + scope boundary.

### Cross-namespace envelope I/O (the code this phase reworks)
- `internal/dispatch/podjob/doc.go` — the superseded Blocker #2/#3 "shared PVC + subPath" rationale (read to understand what's being replaced).
- `internal/dispatch/podjob/backend.go` — `EnvelopeReader` / `FilesystemEnvelopeReader` / `PodStatusEnvelopeReader` (`ReadOut`); Jobs created in `task.Namespace` (~:295).
- `internal/dispatch/podjob/jobspec.go` — the `envelope-writer` init container (base64 `ENVELOPE_IN_B64` → `in.json`, ~:237-241); credproxy sidecar; `terminationMessagePolicy`.
- `internal/controller/task_controller.go` — `buildEnvelopeIn` (~:1037; the #10b cross-ns prompt read to move in-pod) and `handleJobCompletion` (~:693).
- `internal/controller/dispatch_helpers.go` — `MaterializeChildCRDs` / child-CRD materialization (the logic that moves to the reporter under V1).
- `internal/controller/project_controller.go` / `milestone_controller.go` / `phase_controller.go` / `plan_controller.go` — each reads `EnvelopeOut.ChildCRDs` and materializes children; `project_controller.go:57-58` documents the termination-message PVC-free read.
- `internal/subagent/anthropic/subagent.go` — the runner that reads children files into `EnvelopeOut.ChildCRDs` and writes `out.json` (#5 file-handoff).
- `api/v1alpha1/task_types.go` — `TaskSpec.PromptPath` (#10b).

### Project guardrails
- `CLAUDE.md` — CRD `.status` only / no external DB; per-Task CRD small; portability (no hidden host deps); anthropic-only code behind the `Subagent` interface; don't mount host `~/.claude`.
</canonical_refs>

<specifics>
## Specific Ideas

- DoD for the phase's live validation: `kubectl apply -f examples/projects/medium/project.yaml` drives `project/medium-project` to `status.phase=Complete` with real Claude (Haiku) — all descendants Succeeded, a per-run `tide/run-medium-project-*` branch pushed to the in-cluster `http://git-http-server…/demo-remote.git` remote with real authored code, and `status.budget.costSpentCents > 0` under the cap. Then 08-VERIFICATION.md SC-2 → fully PASS and the v1.0.0 retag is unblocked (retag/push remains user-gated, confirm-only).
- Live minikube repro is parked: context `minikube`, TIDE in `tide-system` runtime-patched to `--subagent-image=ghcr.io/jsquirrelz/tide-claude-subagent:1.0.0` (NOT chart-persisted; a controller rollout reverts it — re-patch arg index 5). `tide-sample-medium` namespace staged. Images may be stale after the #11 revert — rebuild + `minikube image rmi` before `minikube image load` (load is NOT idempotent).
- CI only ever dispatches the stub subagent; the real-Claude path is not CI-gated — so the reporter contract must keep the stub working too (stub builds ChildCRDs in Go; the reporter must handle both the stub's and the real runner's `out.json`).
</specifics>

<deferred>
## Deferred Ideas

- Editable / re-appliable "envelopes as first-class reviewable artifacts" DX (the write/replay surface; Phoenix/OpenInference = the read surface) — and the clobbering / which-fields-are-authoritative-vs-regenerated question. A deliberately separate SUBSEQUENT phase. The V1 reporter built here is its foundation.
- Moving envelope DELIVERY off the base64 `ENVELOPE_IN_B64` env var (the input side) — only revisit if input envelopes approach size limits; not needed for v1.
</deferred>

---

*Phase: 09-cross-namespace-envelope-return-in-namespace-reporter*
*Context gathered: 2026-06-08 from the real-claude-authoring-path debug-session design discussion*
