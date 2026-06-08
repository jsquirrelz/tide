---
slug: real-claude-authoring-path
status: open
opened: 2026-06-08
opened_by: phase-08 live SC-2 validation
severity: high
blocks: v1.0.0 release (real-Claude authoring has never run end-to-end)
related: file-transport-git-missing (resolved), phase-08 08-VERIFICATION.md
---

# Real-Claude authoring path — chain of pre-existing defects

## Symptom

The real `claude-subagent` planner/executor pods fail **before any LLM call**
when driving a Project with `--subagent-image=…tide-claude-subagent:1.0.0`.
Surfaced during Phase 8's SC-2 live minikube re-test (2026-06-08). CI never
caught any of this because CI dispatches **only** the `stub-subagent`, which
tolerates the controller's envelope/param conventions. The large ($25) sample
is the only other real-Claude surface and is **not CI-gated** (D-A4) — so the
real-Claude authoring path appears to have **never run end-to-end**.

This is **orthogonal to Phase 8** (transport policy + http:// medium wiring).
Phase 8's transport deliverable is validated independently (the http:// clone
Job succeeds; see 08-VERIFICATION.md).

## Environment to reproduce (live, still up)

- minikube (context `minikube`, K8s v1.33.7). TIDE in `tide-system`.
- Controller patched at runtime to `--subagent-image=ghcr.io/jsquirrelz/tide-claude-subagent:1.0.0`
  (deployment arg index 5; **runtime patch only — NOT persisted to the chart**;
  a `helm upgrade` would revert it. The chart sources `--subagent-image` from
  `images.stubSubagent`, so a real-Claude install needs that arg set to the
  claude image OR left empty to fall back to `CLAUDE_SUBAGENT_IMAGE`).
- `tide-sample-medium` namespace fully staged: git-http-server (Available),
  `demo-remote-pvc`, `tide-projects` PVC, `tide-subagent` + `tide-push` SAs,
  `tide-secrets` (real ANTHROPIC_API_KEY + empty GIT_PAT), `tide-signing-key`.
- Repro: `kubectl apply -f examples/projects/medium/project.yaml` →
  the `tide-project-*` planner Job fails in ~12s. (Cost stays $0 — fails
  pre-LLM.)

## Confirmed defects (in discovery order)

### 1. claude-subagent envelope-path default — FIXED (commit 6dcffe6)

`cmd/claude-subagent/main.go` defaulted `--envelope-path` to
`/workspace/envelopes/in.json`, but the controller writes (and `stub-subagent`
reads) `/workspace/envelopes/$TIDE_TASK_UID/in.json`, and passes **no**
`--envelope-path` arg. Error was:
`harness: open envelope "/workspace/envelopes/in.json": no such file or directory`.
Fix aligned claude-subagent's resolution to the stub
(flag → `$TIDE_ENVELOPE_PATH` → per-task-uid path). **Done.**

### 2. `Provider.Params` overloaded with dispatch metadata (`parentName`) — OPEN

After fix #1 the planner reaches the anthropic runner and dies with:
`anthropic subagent: unknown param "parentName" (allowed: temperature, thinking_budget, top_p, top_k)`.

- **Producer:** `internal/controller/dispatch_helpers.go:178` injects
  `envIn.Provider.Params["parentName"] = parent.GetName()` so the **stub** planner
  can build child-CRD refs (`projectRef`/`milestoneRef`/…). The stub reads it at
  `cmd/stub-subagent/main.go:229`.
- **Rejector:** `internal/subagent/anthropic/subagent.go:166` treats
  `Provider.Params` as **model parameters only** and hard-rejects any key outside
  the allowlist (`subagent.go:65-67`: temperature, thinking_budget, top_p, top_k).
- **Contract conflict:** the controller overloads `Provider.Params` with BOTH
  model params (for the real runner) AND dispatch metadata (for the stub). The
  two subagents disagree on what `Provider.Params` means.

**Fix options (decide in the debug session — design call):**
- (a) anthropic runner ignores (skips) non-model keys instead of erroring — most
  forgiving, but loses the strict-validation safety for genuine typos.
- (b) anthropic runner allowlists `parentName` explicitly (passes it through /
  ignores it) — narrow, but more dispatch-metadata keys may exist or appear.
- (c) controller stops overloading `Provider.Params`: move `parentName` (and any
  other dispatch metadata) to a dedicated EnvelopeIn metadata field both
  subagents read — cleanest contract, largest change (envelope schema + both
  subagents + controller).

Recommend (c) long-term; (a)/(b) unblock fastest. The real planner likely does
not even need `parentName` (it authors from the outcome prompt, not canned refs
like the stub) — so the runner ignoring it (option a/b) may be functionally
sufficient.

### 3+. Downstream (UNVERIFIED — expect more)

The planner has never gotten past param validation, so nothing downstream of the
real anthropic `Run()` is proven: does the claude binary/CLI exist + work in the
image? does the runner emit a valid `ChildCRDs` envelope the controller can
materialize? do executor (task-level) pods work? do per-level model picks
resolve? **Treat each as unverified until a real run reaches `Complete`.**

## Definition of done

`kubectl apply -f examples/projects/medium/project.yaml` drives
`project/medium-project` to `status.phase=Complete` with real Claude (Haiku),
a per-run `tide/run-medium-project-*` branch pushed to the in-cluster http://
remote, and `status.budget.costSpentCents > 0` (real tokens billed, under the
$5 cap). Then update 08-VERIFICATION.md SC-2 → fully PASS, and unblock the
v1.0.0 retag.

## Notes / guardrails

- Fixes belong on `main` via GSD (`/gsd-debug` then commit). The claude-subagent
  and/or controller images must be rebuilt + `minikube image load` (rmi the stale
  tag first — `minikube image load` is NOT idempotent).
- The chart's default install wiring (`--subagent-image` ← `images.stubSubagent`)
  means **no operator gets real Claude by default** — worth deciding whether
  that's intended (stub-by-default is reasonable for a $0 smoke, but the medium/
  large samples' `spec.subagent.image: claude-subagent` is silently ignored,
  which is misleading). Consider honoring `Project.Spec.Subagent.Image` as an
  override, or documenting the install-time `--subagent-image` requirement.
