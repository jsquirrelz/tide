---
status: testing
phase: 37-dashboard-surfaces-artifact-view-project-view-log-drawer-sta
source: [37-10-PLAN.md, 37-01-SUMMARY.md, 37-05-SUMMARY.md, 37-07-SUMMARY.md, 37-08-SUMMARY.md, 37-09-SUMMARY.md]
started: 2026-07-08T15:25:32Z
updated: 2026-07-08T16:02:00Z
---

## Pre-UAT Environment Status

- **Deployed:** Phase-37 dashboard (`ghcr.io/jsquirrelz/tide-dashboard:phase37-uat`) rolled onto the live minikube `tide` release (dashboard-only image swap; controller/CRDs/RBAC stay on stable v1.0.6). Pod healthy: `ready=true, restarts=0`.
- **CR-01 verified in the SERVED bundle:** the deployed `index-DgctWc3k.js` contains the namespace-threaded log URL — `const n=\`/api/v1/tasks/${e}/log\`, r = t ? \`${n}?namespace=${t}\` : n`. The fix is live, not just committed.
- **Smoke clean:** `/` HTTP 200, `/api/v1/projects` → `[]`, `/healthz` 200, settings 404-for-missing. Empty-state renders ("No projects in this cluster"). The console SSE `text/html` errors are the pre-existing empty-cluster `/dev/null/no-project` sentinel (from Phase 15, `f374404`) — NOT a Phase-37 regression.
- **Blocker for the 8 tests below:** the cluster has no project (tide-cashboard was removed). Tests 1/3/4/5/6 need a running/parked project run; a stub-subagent sample project (free, deterministic) can generate those states but needs the controller+tide-push+RBAC at Phase-37 (a fuller helm upgrade). Tests 2/7/8 are testable against any project.

## DASH-02 Layer-B result — ⚠ FAILS (needs /gsd-debug)

Ran 37-09's `artifact_staging_test` (Layer-B kind, first-ever execution — it was authored but env-gated) twice. **Both runs RED — DASH-02 is NOT verified.** Evidence:
- Phase-37's artifact-push trigger **works** — controller logs show `"triggered artifact push", envelopes:1` creating a `tide-push-<uid>` Job.
- But `Status.Git.LastPushedSHA` **never advances**, so the test's push precondition fails.
- Two factors observed: (1) the test asserts `LastPushedSHA NotTo BeEmpty` with a direct `Expect` on the Complete-time snapshot, but per the controller's #13b contract (`project_controller.go:487-506`) `Complete` *precedes* the async `reconcileBoundaryPush` — a genuine test race; (2) even with an `Eventually` poll (5 min), `LastPushedSHA` still never advanced and no boundary-push/LastPushedSHA-patch activity appears in the logs — pointing at the **artifact/boundary shared `tide-push-<project-uid>` Job-name coupling** (code review **IN-01**, "mitigated but under-tested"). The poll also outran the suite's `kindTestTimeout` ctx.
- Test left pristine (investigative Eventually edit reverted). **Hypothesis, not confirmed root cause** — needs a dedicated `/gsd-debug` session on the artifact-vs-boundary push interaction.

## Current Test

number: 1
name: Log drawer — running pod streams (D-15, CR-01 regression)
expected: |
  With a task pod RUNNING (in a non-default namespace like tide-sample-medium/small),
  clicking "Open log stream" shows "Connecting…" then live log lines — never blank,
  never "pod garbage-collected". This specifically exercises the CR-01 fix (namespace
  now threaded into the pod-log SSE URL); pre-fix this falsely reported pod-gone.
awaiting: user response

## Tests

### 1. Log drawer — running pod streams (D-15, CR-01 regression)
expected: While a task pod is RUNNING (non-default namespace, e.g. tide-sample-medium/small): "Open log stream" → drawer shows "Connecting…" then streams lines. Never blank; never "pod garbage-collected" for a live pod. (Directly verifies the CR-01 namespace fix live.)
result: [pending]

### 2. Log drawer — GC'd pod shows honest terminal state
expected: After a task completes and its pod is garbage-collected: opening its log stream shows "Logs no longer available — pod garbage-collected." with NO retry button, and the browser network tab shows NO repeating reconnect requests (stream stays closed).
result: [pending]

### 3. Log drawer — port-forward kill → reconnect state
expected: Kill the port-forward mid-stream on a RUNNING task → drawer shows the reconnecting / stream-error copy with a Reconnect button (never blank, never falsely "pod-gone"). Restore the port-forward and click Reconnect → streaming resumes.
result: [pending]

### 4. Artifact view — milestone artifact renders at the gate (DASH-01)
expected: When the milestone parks AwaitingApproval, clicking the milestone node shows either the rendered artifact (markdown, GFM tables) OR an "Artifacts materializing" placeholder that resolves to the artifact within ~a minute WITHOUT reopening the panel (10s poll).
result: [pending]

### 5. Approve gate — dashboard-only review flow (DASH-01 headline)
expected: The pinned strip below the artifact shows a gate badge + Approve/Reject buttons that copy `tide approve <project>` / `tide reject <project>`. Paste-run the approve command in a terminal → the run proceeds. No PVC reader pod is used at any point.
result: [pending]

### 6. Artifact view — plan node shows its own planner output (DASH-01)
expected: Clicking a plan node after its planner ran renders that node's OWN artifacts (PLAN.md markdown + children JSON pretty-printed) — the node's own planner output, not its parent's.
result: [pending]

### 7. Project view — settings render, no secret values (DASH-03)
expected: Clicking the project node shows a status strip labeled "Status", a budget line, and cards (outcome prompt verbatim, repository, models with the effort note, budget, gates, secrets shown as NAMES with a "value not shown" suffix), plus a collapsible raw YAML. Spot-check: no secret VALUE appears anywhere (compare against the actual Secret).
result: [pending]

### 8. Resize / collapse persistence (D-06)
expected: Drag the panel's left edge (clamps at 360px / 70vw), collapse to the rail and re-expand, reload the page → width and collapsed state persist. Same for the log area's top-edge handle and collapse.
result: [pending]

## Summary

total: 8
passed: 0
issues: 0
pending: 8
skipped: 0
blocked: 0

## Gaps

[none yet]
