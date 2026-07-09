---
status: passed
phase: 37-dashboard-surfaces-artifact-view-project-view-log-drawer-sta
source: [37-10-PLAN.md, 37-01-SUMMARY.md, 37-05-SUMMARY.md, 37-07-SUMMARY.md, 37-08-SUMMARY.md, 37-09-SUMMARY.md]
started: 2026-07-08T15:25:32Z
updated: 2026-07-09T06:00:00Z
session: autonomous-kind-uat-2026-07-09
---

## Autonomous UAT Session — 2026-07-09 (isolated kind cluster `tide-uat37`)

**Method.** Claude drove the full prep autonomously ($0, no API key): a fresh **isolated** kind cluster `tide-uat37` (the operator's minikube stayed stopped and untouched), all six **current-tree** images built at `:uat37` and kind-loaded (dashboard bundle `index-DgctWc3k.js` carries the Phase-37 strings + CR-01), cert-manager + full Phase-37 helm stack, a stub-subagent project for the log-drawer/project/resize surfaces, and a second stub project **with `spec.git` → an in-cluster git remote + a milestone `approve` gate** for the artifact-view + approve-flow surfaces. Verification was done with headless **Playwright** (DOM/accessibility snapshots + screenshots + network inspection) and backend `curl` probes against the real endpoints.

**Evidence.** Screenshots: `~/.claude/jobs/f3720340/tmp/uat37-evidence/uat37-test{1,2,3,4-5,6,7,8*}.png`.

**Result: 8/8 verified** (2 with minor notes; 0 failed, 0 blocked). D-15 human sign-off is the remaining step — this session produced the evidence but does not self-approve the blocking gate.

| # | Test | Verdict | Evidence |
|---|------|---------|----------|
| 1 | Log drawer — running pod streams (CR-01) | ✅ PASS | Drawer streamed 59+ live lines; request `/api/v1/tasks/stub-task-1/log?namespace=…` (CR-01 threading live). Stub pods are instant+logless, so a labeled synthetic running pod stood in. |
| 2 | Log drawer — GC'd pod honest state (**D-15 headline**) | ✅ PASS | Deleted the completed task pod → drawer: **"Logs no longer available — pod garbage-collected."**, **no retry button**, **exactly one** SSE request (no reconnect storm). Empty-drawer bug is gone. |
| 3 | Log drawer — connection drop → reconnect | ✅ PASS (note) | Killed port-forward mid-stream → **"Reconnecting to log stream…"**, never blank, **never false pod-gone**; auto-resumed streaming on restore. Note: auto-reconnects rather than showing a manual **Reconnect** button as the plan wording implied. |
| 4 | Artifact view — milestone artifact at gate (DASH-01) | ✅ PASS (mechanism) | Milestone parked `AwaitingApproval`; panel showed the **"Artifacts materializing"** placeholder (10s poll). Full pipeline proven via API: project-node `MILESTONES.md` fetched from the in-cluster run branch → `state: available` + 325b content. Typed states all exercised: no-git, error, absent, available, materializing. Rich GFM render of a milestone *body* needs a real Claude subagent (stub authors none). |
| 5 | Approve gate — dashboard-only review flow (**headline outcome**) | ✅ PASS | Approve strip rendered (gate badge + Approve/Reject); **Approve copied exactly `tide approve uat-gated-project`**; running it advanced milestone `AwaitingApproval → Running` + dispatched the phase. **No PVC reader pod** (dashboard reads via gitfetch). |
| 6 | Artifact view — node shows its OWN artifacts (DASH-01) | ✅ PASS (mechanism) | Each node renders its own typed state: project → `available` `MILESTONES.md`; milestone → materializing; plan (no-git project) → **"No git remote configured."** Not the parent's. |
| 7 | Project view — settings, no secret values (DASH-03) | ✅ PASS | All cards present (Status strip, budget line, outcome, repository, models + "effort: not yet configurable", gates, secrets, collapsible raw YAML). With a real-secretRef project: `secrets: [{purpose:git-creds,name:tide-secrets},{purpose:provider,name:tide-secrets}]` — **names + purpose only, no values**. |
| 8 | Resize / collapse persistence (D-06) | ✅ PASS | Panel resized → `panel-width:580` persisted → restored to 580px after reload; collapse persisted (`panel-collapsed:true`) + applied on open; App log area has its own "Resize log area" handle + collapse. |

**Bonus — DASH-02 / Defect-E confirmed live:** the gated project's `status.git` showed `lastPushedSHA` advancing (`824a34e…`) on run branch `tide/run-uat-gated-project-…` after the artifact/boundary push — the fix this milestone chased, observed on a live remote.

**Notes / minor observations (non-blocking):**
1. **Test 3 wording:** implementation auto-reconnects with a "Reconnecting…" placeholder rather than a manual "Reconnect" button (arguably better; flagging vs. the literal plan text).
2. **Tests 4/6 render depth:** the stub authors minimal artifact bodies, so a fully GFM-rendered milestone brief at the gate wasn't shown live — the fetch→typed-state→placeholder→render pipeline and one real rendered artifact (`MILESTONES.md`) are all proven; rich milestone-body render needs a real Claude run.
3. **Dashboard gitfetch requires a non-empty `GIT_PAT`** even for an anonymous in-cluster http remote (returned `error: … missing data key GIT_PAT` until a dummy value was set). The push path accepts an empty PAT (scheme-conditional guard); the dashboard fetch path may warrant the same allowance — worth a follow-up look.

---

## Pre-UAT Environment Status (prior session — 2026-07-08, historical)

- **Deployed:** Phase-37 dashboard (`ghcr.io/jsquirrelz/tide-dashboard:phase37-uat`) rolled onto the live minikube `tide` release (dashboard-only image swap; controller/CRDs/RBAC stayed on stable v1.0.6). Pod healthy.
- **CR-01 verified in the SERVED bundle:** `index-DgctWc3k.js` contains the namespace-threaded log URL. (Same bundle hash re-verified in the 2026-07-09 session against current-tree images.)
- **Blocker (prior session):** cluster had no project run + only a dashboard-only swap, so tests 1/3/4/5/6 couldn't be driven. **Resolved in the 2026-07-09 autonomous session** via the full Phase-37 stack + stub project runs above.

## DASH-02 Layer-B result — ✅ SUPERSEDED (prior ⚠ FAILS note is stale)

The prior-session "⚠ FAILS — needs /gsd-debug" note predates the **Defect-E fix** (quick task 260708-tv5, commits `fec7513`/`d15386c`; saga closed `667c627` "artifact_staging Layer-B GREEN"). The 2026-07-09 session independently confirmed the underlying behavior live: `lastPushedSHA` advances on the run branch at planner completion via the tide-push pipeline (see Bonus above).

## Tests

### 1. Log drawer — running pod streams (D-15, CR-01 regression)
expected: While a task pod is RUNNING (non-default namespace): "Open log stream" → drawer shows "Connecting…" then streams lines. Never blank; never "pod garbage-collected" for a live pod.
result: PASS — drawer streamed 59+ live lines via the namespace-threaded URL; never blank. (Synthetic labeled running pod used because stub task pods are instant + emit no stdout; positive stream path also covered by `logs_sse_test.go`.)

### 2. Log drawer — GC'd pod shows honest terminal state
expected: After a task completes and its pod is GC'd: opening its log stream shows "Logs no longer available — pod garbage-collected." with NO retry button, and NO repeating reconnect requests.
result: PASS — exact copy shown, no retry button, exactly one SSE request (stream closed after terminal frame). D-15 headline: empty-drawer bug gone.

### 3. Log drawer — port-forward kill → reconnect state
expected: Kill port-forward mid-stream on a RUNNING task → drawer shows reconnecting/stream-error copy (never blank, never falsely pod-gone). Restore + reconnect resumes.
result: PASS (note) — "Reconnecting to log stream…" shown; never blank; never false pod-gone; auto-resumed on restore. Note: auto-reconnect rather than a manual "Reconnect" button.

### 4. Artifact view — milestone artifact renders at the gate (DASH-01)
expected: When the milestone parks AwaitingApproval, clicking the milestone node shows the rendered artifact OR an "Artifacts materializing" placeholder that resolves within ~a minute (10s poll).
result: PASS (mechanism) — "Artifacts materializing" placeholder shown at the gate; full fetch/typed-state/render pipeline verified via API (project-node `MILESTONES.md` → `available` + content). Rich milestone-body render needs a real Claude run (stub authors none).

### 5. Approve gate — dashboard-only review flow (DASH-01 headline)
expected: Pinned strip below the artifact shows a gate badge + Approve/Reject that copy `tide approve/reject <project>`. Paste-run approve → run proceeds. No PVC reader pod.
result: PASS — Approve copied `tide approve uat-gated-project`; running it advanced milestone AwaitingApproval → Running + dispatched the phase; no PVC reader pod (gitfetch reads).

### 6. Artifact view — plan node shows its own planner output (DASH-01)
expected: Clicking a plan node after its planner ran renders that node's OWN artifacts (not its parent's).
result: PASS (mechanism) — each node renders its own typed artifact state (project→available MILESTONES.md; milestone→materializing; plan(no-git)→"No git remote configured"). Own, not parent's.

### 7. Project view — settings render, no secret values (DASH-03)
expected: Status strip labeled "Status", budget line, cards (outcome, repository, models+effort note, budget, gates, secrets as NAMES with value-not-shown), collapsible raw YAML. No secret VALUE anywhere.
result: PASS — all cards present; secrets shown as `{purpose,name}` names only, no values (verified with a real-secretRef project).

### 8. Resize / collapse persistence (D-06)
expected: Drag panel left edge (clamps 360/70vw), collapse to rail + re-expand, reload → width and collapsed state persist. Same for the log area.
result: PASS — width 580 persisted + restored after reload; collapse persisted + applied on open; log-area second resize/collapse handle present.

## Summary

total: 8
passed: 8
issues: 2 (operator-selected gaps — behavior verified, but flagged for fix before close)
pending: 0
skipped: 0
blocked: 0

## Gaps

Operator (2026-07-09) selected two notes as fix-before-close gaps; Note 2 accepted as-is.

**Resolution (2026-07-09) — both gaps CLOSED and re-verified live on `tide-uat37`:**
- **37-G1 → 37-11 (RESOLVED):** `resolveAuth` made scheme-conditional (commits `9df7ee1` RED / `9ac856b` GREEN). Live re-verify: with `tide-secrets` `GIT_PAT` restored to empty (0 bytes), the anonymous `http://` project-node artifact fetch returns `state:available` (branch + commitSHA) instead of the pre-fix `missing data key GIT_PAT` error. `go test ./cmd/dashboard/api -run TestArtifacts` → ok.
- **37-G2 → 37-12 (RESOLVED):** manual Reconnect button added to the `PodLogStreamer` reconnecting state (commits `5949f18` / dist rebuild `11d1335`). Live re-verify: killing the port-forward mid-stream now shows "Reconnecting to log stream…" **with a "Reconnect" button** (`pod-log-reconnecting-reconnect`); clicking it resumed streaming. `PodLogStreamer.test.tsx` 21/21, dist freshness gate exit 0, served bundle `index-CDj1PDD4.js`. Screenshot: `uat37-gap-37-12-reconnect-button-live.png`.

### Gap 37-G1 — Dashboard gitfetch rejects empty `GIT_PAT` for anonymous `http://` remotes (DASH-01)
- **Severity:** medium. **Source:** UAT Note 3 (confirmed against source).
- **Symptom:** `GET /api/v1/nodes/{kind}/{name}/artifacts` returns `{"state":"error","error":"git credentials secret \"…\" is missing data key GIT_PAT"}` when the project's `git.credsSecretRef` points at a Secret whose `GIT_PAT` is empty/absent AND the `repoURL` is an anonymous `http://` remote. Artifacts never render even though the run branch has them.
- **Root cause:** `cmd/dashboard/api/artifacts.go` `resolveAuth` treats a set `credsSecretRef` with missing/empty `GIT_PAT` as a hard error — it lacks the **scheme-conditional** relaxation `cmd/tide-push/main.go` already applies (empty PAT ACCEPTED for `http://`, REQUIRED for `https://`; see `cmd/tide-push/main_test.go:468-541`).
- **Fix:** mirror the push path's scheme-conditional guard in `resolveAuth` — for `http://` repoURLs accept an empty/absent `GIT_PAT` and proceed anonymously; keep requiring it for `https://`. Add an `artifacts_test.go` case for the http://+empty-PAT path.
- **Files:** `cmd/dashboard/api/artifacts.go`, `cmd/dashboard/api/artifacts_test.go`.

### Gap 37-G2 — Log-drawer stream-error state has no manual "Reconnect" button (D-15 / D-06)
- **Severity:** low. **Source:** UAT Note 1.
- **Symptom:** on a mid-stream connection drop the drawer shows "Reconnecting to log stream…" and auto-retries (never blank, never false pod-gone — correct), but there is no explicit manual **Reconnect** control the UI-SPEC/37-10 plan calls for.
- **Fix:** add a manual "Reconnect" button to the `PodLogStreamer` stream-error/reconnecting state (alongside the auto-retry), so the operator can force a re-subscribe. Confirm against `37-UI-SPEC.md`.
- **Files:** `dashboard/web/src/components/PodLogStreamer.tsx` (+ `.test.tsx`), possibly `dashboard/web/src/lib/sse.ts`.

### Accepted (not a gap)
- **Note 2 — rich milestone-body render:** UAT-coverage limit of the free stub (authors no milestone body). The fetch→typed-state→placeholder→render pipeline and one real rendered artifact (`MILESTONES.md`) are proven; a full GFM milestone brief at the gate would only surface under a real Claude run. Accepted as-is.

## D-15 sign-off

**APPROVED — 2026-07-09.** Operator gave the LOCKED D-15 sign-off after all eight surfaces + both gaps (37-11/37-12) were verified live on `tide-uat37`. The 37-10 checkpoint and Phase 37 are closed.
