---
phase: 37-dashboard-surfaces-artifact-view-project-view-log-drawer-sta
verified: 2026-07-15T06:16:41Z
status: passed
score: 4/4 must-haves verified
overrides_applied: 0
re_verification:
  previous_status: human_needed
  note: "Initial goal-backward verification found 4/4 criteria met with one env-gated human item: the Layer B kind run for DASH-02. Satisfied 2026-07-15: full `make test-int` on HEAD dcf2d16 (post-41 tree + kind namespace-race fix 260715-4jd) exited MAKE_EXIT=0 — envtest 56/56, kind 26/26 including artifact staging (log: ~/.claude/jobs/8b19abd1/tmp/test-int-fixed.log). DASH-02 flipped Complete in REQUIREMENTS.md with the reworded git-transport wording per the human_verification instruction."
human_verification:
  - test: "Run the Layer B kind suite for DASH-02 artifact staging end-to-end."
    expected: "`make test-int` (or `go test ./test/integration/kind/ -run TestArtifactStaging`) exits 0 with artifact_staging_test.go green: >=1 *.md under .tide/planning/<kind>/<name>/ on the run branch, byte-identical to the stub planner doc, no in.json/out.json staged, and Status.Git.LastPushedSHA advanced. Then flip DASH-02 -> Complete in REQUIREMENTS.md with the reworded git-transport wording."
    why_human: "Requires a kind cluster + Docker (unavailable in this verifier environment; the executor left the run env-gated in 37-09). The test is authored, substantive, and compiles clean (go vet passes); the underlying behavior was already observed live in the 37-10 UAT (lastPushedSHA advanced on the run branch, operator D-15 sign-off 2026-07-09), but the automated Layer B green run itself was not independently observed here."
---

# Phase 37: Dashboard Surfaces — Artifact View, Project View, Log-Drawer States Verification Report

**Phase Goal:** The dashboard is a sufficient approve-gate review surface — operators read the planning artifacts a node produced, the project's outcome prompt and settings, and honest log-drawer states, without spinning up ad-hoc PVC reader pods.
**Verified:** 2026-07-15T06:16:41Z
**Status:** human_needed
**Re-verification:** No — initial verification (HEAD c09937f, PR #6 squash-merge `eeb96cf`).

## Goal Achievement

All four ROADMAP success criteria are met in the current tree at the code / wiring / behavior level, and were independently confirmed live in the phase's own 37-10 autonomous UAT (operator D-15 sign-off, 2026-07-09). One item — the automated Layer B kind-suite green run for DASH-02 — is env-gated and cannot be run in this verifier (no kind/Docker), so status is `human_needed` rather than `passed`. It is a confirmation/documentation item, not a goal gap.

### Observable Truths

| # | Truth (ROADMAP success criterion) | Status | Evidence |
| --- | --- | --- | --- |
| 1 | Clicking any Planning DAG node shows its artifacts, markdown-rendered (children JSON pretty-printed); gate-parked nodes render the artifact beside the approve action — approve needs no PVC reader pod (DASH-01) | ✓ VERIFIED | `ArtifactViewer.tsx` renders react-markdown (remarkGfm only, no rehype-raw) + `prettyJSON` `<pre>` + five R-04 states, no truncation (385 lines). `App.tsx:391-403,616-655` routes node clicks by kind → `NodeDetailPanel` mounts `ArtifactViewer`; `gateParked` (nodeStatus==="AwaitingApproval") drives `ApproveStrip` (copy-only `tide approve/reject`). Backend `artifacts.go` serves `.tide/planning/<kind>/<name>/` from the gitfetch Store. `ArtifactViewer` suite 9/9 green in isolation (x3). UAT Tests 4/5/6 PASS live (approve advanced the run, no PVC reader pod). |
| 2 | Planning artifacts durably readable by the dashboard without PVC access, full-fidelity/truncation-safe [reworded from ConfigMap wording per 37-CONTEXT D-01/D-03] (DASH-02) | ✓ VERIFIED (code+UAT); automated kind lock → human_verification | Write path: `tide-push --stage-envelopes` (`parseStageEnvelopes`, `stageEnvelopeArtifacts`, fail-closed traversal guard) → `.tide/planning/<destPrefix>/`; `triggerArtifactPush` wired at all 4 levels (milestone/phase/plan x2 + project) + boundary push carries cumulative map (R-05). Read path: `gitfetch.Store` + artifacts endpoint. **No artifact ConfigMap exists in code** (grep-confirmed) — the git-transport rework fully superseded the ConfigMap mechanism. `go test ./cmd/tide-push/ ./internal/controller/ -run TestArtifactPush` PASS (envtest). `artifact_staging_test.go` substantive (byte-fidelity, D-04 exclusion, LastPushedSHA), `go vet` clean. UAT observed `lastPushedSHA` advancing live. |
| 3 | Operator can read outcome prompt and project settings in a dashboard project view (no secret values) (DASH-03) | ✓ VERIFIED (with WARNING on baseRef) | `settings.go` projects outcome prompt / repo / models / budget / gates / secret NAMES + raw-spec YAML; handler holds no clientset (secret values structurally impossible). `ProjectSettingsPanel.tsx` renders outcome prompt in verbatim `pre-wrap` (never markdown), secrets suffixed "(name only — value not shown)", collapsible raw-spec. GET-only route. `ProjectSettingsPanel` 11/11 + `settings_test.go` green. UAT Test 7 PASS (names only). **WARNING:** `repo.baseRef` hardcoded `""` though `Spec.Git.BaseRef` exists — see WR below. |
| 4 | Log drawer always renders an explicit state (loading/streaming/pod-gone), never silently empty (DASH-04) | ✓ VERIFIED | `logs_sse.go` emits `pod-gone`/`error`/`idle-timeout`; `resolvePodName` serves terminated-but-present pods. `sse.ts` terminal-frame handler suppresses reconnect once; `useTaskLog(taskName, namespace?)` maps to states. `PodLogStreamer.tsx` renders explicit locked copy for every state (connecting/connected/offline/idle-closed/reconnecting[+Reconnect]/pod-gone[message-only, no retry]/stream-error[+Reconnect]) — parameterized no-empty-viewport guard. `PodLogStreamer` 21/21 + `sse.test.ts` 14/14 green. CR-01 namespace fix present end-to-end (App.tsx:747-750). UAT Tests 1/2/3 PASS live (empty-drawer bug gone — D-15 headline). |

**Score:** 4/4 truths verified

### Required Artifacts

| Artifact | Expected | Status | Details |
| --- | --- | --- | --- |
| `dashboard/web/src/components/ArtifactViewer.tsx` | Markdown+JSON render, 5 states, no truncation | ✓ VERIFIED | 385 lines; react-markdown+remarkGfm, `prettyJSON`, R-04 states; 9/9 tests |
| `dashboard/web/src/components/ApproveStrip.tsx` | Copy-only gate strip | ✓ VERIFIED | 60 lines; `ClipboardCopyAction` `tide approve/reject`, no mutation surface |
| `dashboard/web/src/components/NodeDetailPanel.tsx` | Right-panel shell, resize/collapse | ✓ VERIFIED | 290 lines; focus trap, `usePersistedSize`, collapse≠close |
| `dashboard/web/src/components/NodeClickContext.tsx` | (kind,name) routing | ✓ VERIFIED | 33 lines; kind-aware callback |
| `dashboard/web/src/components/ProjectSettingsPanel.tsx` | Settings cards + raw-spec | ✓ VERIFIED | 434 lines; pre-wrap prompt, names-only secrets, raw-spec disclosure |
| `dashboard/web/src/components/PodLogStreamer.tsx` | Four+ explicit states | ✓ VERIFIED | 335 lines; every state explicit copy; 21/21 tests |
| `dashboard/web/src/lib/sse.ts` | Terminal-frame state machine | ✓ VERIFIED | 523 lines; namespace-threaded log URL (CR-01) |
| `cmd/dashboard/api/artifacts.go` | Node artifacts endpoint (v1alpha3) | ✓ VERIFIED | 247 lines; gitfetch store, prefix filter, R-04 states, Gap 37-G1 scheme-conditional auth |
| `cmd/dashboard/api/settings.go` | Redacted settings endpoint | ✓ VERIFIED | 223 lines; no clientset, names-only, raw-spec YAML |
| `cmd/dashboard/gitfetch/{gitfetch,store}.go` | Shallow-clone Fetcher + LRU | ✓ VERIFIED | 211+105 lines; tests green |
| `cmd/tide-push/main.go` `--stage-envelopes` | Envelope staging write-half | ✓ VERIFIED | `parseStageEnvelopes`, `stageEnvelopeArtifacts`, `.tide/planning/`; tests green |
| `internal/controller/artifact_push.go` | Controller push trigger | ✓ VERIFIED | 254 lines; `triggerArtifactPush` wired at 4 levels + boundary; envtest green |
| `test/integration/kind/artifact_staging_test.go` | Layer B DASH-02 lock | ⚠ AUTHORED (run env-gated) | 455 lines; substantive assertions, `go vet` clean; kind run not executable here |

### Key Link Verification

| From | To | Via | Status | Details |
| --- | --- | --- | --- | --- |
| App.tsx click | ArtifactViewer | `onNodeClick(kind,name)` → NodeDetailPanel content | ✓ WIRED | App.tsx:391-403, 616-655 |
| ArtifactViewer | artifacts endpoint | `fetchNodeArtifacts` (api.ts) | ✓ WIRED | api.ts:257 → GET /nodes/{kind}/{name}/artifacts |
| artifacts endpoint | run branch | `gitfetch.Store.Artifacts` | ✓ WIRED | artifacts.go:170 |
| reporter/planner completion | run branch | `triggerArtifactPush` → tide-push `--stage-envelopes` | ✓ WIRED | 7 call sites across 4 controllers + boundary_push (R-05) |
| ProjectSettingsPanel | settings endpoint | `fetchProjectSettings` | ✓ WIRED | api.ts:319 → GET /projects/{name}/settings |
| PodLogStreamer | logs SSE | `useTaskLog(taskName, namespace)` | ✓ WIRED | sse.ts; namespace threaded (CR-01 fix) App.tsx:747-750 |
| ApproveStrip render | node gate state | `gateParked = nodeStatus==="AwaitingApproval"` | ✓ WIRED | App.tsx:639-651 |

### Data-Flow Trace (Level 4)

| Artifact | Data Variable | Source | Produces Real Data | Status |
| --- | --- | --- | --- | --- |
| ArtifactViewer | `data: NodeArtifacts` | `fetchNodeArtifacts` → artifacts.go → gitfetch clone of run branch | Yes — reads real `.tide/planning/` blobs (full content, byte-length) | ✓ FLOWING |
| ProjectSettingsPanel | `settings` | `fetchProjectSettings` → settings.go project whitelist projection | Yes — from Project CR spec/status | ✓ FLOWING (baseRef field hollow — see WR) |
| PodLogStreamer | `lines/state` | `useTaskLog` SSE ← logs_sse.go pod logs | Yes — real pod log stream + terminal frames | ✓ FLOWING |

### Behavioral Spot-Checks

| Behavior | Command | Result | Status |
| --- | --- | --- | --- |
| Dashboard + tide-push Go build | `go build ./cmd/dashboard/... ./cmd/tide-push/...` | exit 0 | ✓ PASS |
| Dashboard API + gitfetch + tide-push unit tests | `go test ./cmd/dashboard/api/... ./cmd/dashboard/gitfetch/... ./cmd/tide-push/...` | all ok | ✓ PASS |
| Controller artifact-push (envtest) | `go test ./internal/controller/ -run TestArtifactPush` | ok | ✓ PASS |
| Frontend DASH suites (sse/PodLogStreamer/ApproveStrip/ProjectSettingsPanel/NodeDetailPanel) | `npx vitest run` | 58/58 green | ✓ PASS |
| ArtifactViewer suite (isolation x3) | `npx vitest run ArtifactViewer.test.tsx` | 9/9 x3 | ✓ PASS |
| Integration test compiles | `go vet ./test/integration/kind/` | exit 0 | ✓ PASS |
| Layer B kind run (DASH-02 e2e) | `make test-int` | not runnable (no kind/Docker) | ? SKIP → human_verification |

Note: A combined `ArtifactViewer + node-panel-integration` run showed one transient JSON-tab failure under parallel fake-timer load; the ArtifactViewer suite is 9/9 green in isolation across 3 runs — matches the parallel-load flake documented in the 37-08 SUMMARY, not a defect. Local `node_modules` was stale (missing committed `react-markdown`/`remark-gfm`); `npm ci` restored them — both are correctly pinned in package.json + package-lock.json at HEAD.

### Requirements Coverage

| Requirement | Source Plan | Description | Status | Evidence |
| --- | --- | --- | --- | --- |
| DASH-01 | 37-03/04/05/07/08/11 | Planning-DAG node artifact view (markdown + JSON), gate-parked approve beside artifact | ✓ SATISFIED | Truth 1; ArtifactViewer/ApproveStrip/endpoint/gitfetch all verified; UAT Tests 4-6 |
| DASH-02 | 37-02/06/09 | Planning artifacts durably readable by dashboard without PVC access, full-fidelity [reworded from ConfigMap] | ✓ SATISFIED (code+UAT); automated kind lock pending human | Truth 2; write+read path verified, no ConfigMap in code, UAT live observation; **REQUIREMENTS.md still marks Pending + carries stale ConfigMap wording** |
| DASH-03 | 37-07/08 | Read outcome prompt + project settings, no secret values | ✓ SATISFIED (WARNING: baseRef) | Truth 3; settings endpoint + panel; UAT Test 7 |
| DASH-04 | 37-01/12 | Log drawer explicit loading/streaming/pod-gone, never silently empty | ✓ SATISFIED | Truth 4; sse.ts + PodLogStreamer + logs_sse.go; UAT Tests 1-3 |

No orphaned requirements: DASH-01..04 are all claimed by plans and REQUIREMENTS.md maps only these four to Phase 37.

### Anti-Patterns Found

| File | Line | Pattern | Severity | Impact |
| --- | --- | --- | --- | --- |
| (phase source files) | — | Debt markers (TBD/FIXME/XXX) | ℹ️ NONE | Clean — zero debt markers in any phase-modified file |
| `cmd/dashboard/api/settings.go` | 53-57, 161-164 | Hardcoded empty `repo.BaseRef` with stale "not yet landed" comment | ⚠️ WARNING | `Spec.Git.BaseRef` HAS existed since Phase 35 (project_types.go:242; used in project_controller.go:639, push_helpers.go:401). Curated Repository card shows "HEAD (default)" (ProjectSettingsPanel.tsx:299) even when a baseRef is set → misreports configured base ref. **Mitigated:** raw-spec YAML card marshals `p.Spec` (baseRef has `json:"baseRef,omitempty"`), so the true value IS readable there. Already flagged as REVIEW IN-02. Fix: `settings.Repo.BaseRef = p.Spec.Git.BaseRef` (guarded by existing `p.Spec.Git != nil`). Does not block DASH-03 ("operator can read settings" holds). |
| `charts/tide/templates/dashboard-rbac.yaml` | 61-66 | Cluster-wide `secrets: get` | ⚠️ WARNING (carried) | REVIEW WR-01 — read-only (get-only, `⊆{get,list,watch}` holds) but ClusterRole scope = cluster-wide secret-read blast radius. Handlers never return values. Advisory; recommend per-namespace Role or documented accepted-risk. |
| `cmd/dashboard/api/artifacts.go` / `gitfetch.go` | 170 / 108-152 | No timeout on ls-remote/clone | ⚠️ WARNING (carried) | REVIEW WR-02 — operator-supplied `repoURL`; a hung remote blocks the artifacts handler indefinitely. Recommend `context.WithTimeout(30s)`. |
| `cmd/dashboard/api/settings.go` | 127-139 | Nondeterministic cross-namespace first name-match | ⚠️ WARNING (carried) | REVIEW WR-03 — same-name Projects across namespaces → list-order-dependent settings served. Mirrors existing projects.go; higher-consequence surface. |
| `internal/controller/artifact_push.go` / `boundary_push.go` | shared Job name | Timing coupling | ℹ️ INFO (carried) | REVIEW IN-01 — artifact+boundary share `tide-push-<uid>`; mitigated (park-arm-only + cumulative map + requeue); no data-loss path proven. Suggest a regression test. |

None of the above blocks the phase goal. The four carried REVIEW findings (WR-01/02/03, IN-01) are advisory hardening items already documented in 37-REVIEW.md and open for human judgment.

### Wording Drift (recorded per task instruction)

- **DASH-02 was reworded from ConfigMaps to git-transport during phase discussion (37-CONTEXT D-01/D-03), but the rework was never propagated to the contract docs.** ROADMAP.md success criterion 2 and REQUIREMENTS.md line 50 still read "size-capped, owner-ref'd ConfigMaps … truncation markers … garbage-collects." The implemented mechanism is staged planning envelopes pushed via `tide-push --stage-envelopes` to `.tide/planning/<kind>/<name>/`, read back by the dashboard gitfetch store — **there is deliberately no artifact ConfigMap in code** (grep-confirmed). Verified against the reworded intent per instruction. Recommend updating ROADMAP.md criterion 2 and REQUIREMENTS.md DASH-02 to the git-transport wording.
- **REQUIREMENTS.md still marks DASH-02 as `[ ]` / "Pending"** (lines 50, 149) despite the write+read path being complete, unit/envtest-covered, and UAT-observed live. Documentation lag — flip to Complete once the Layer B kind run is observed green (the human_verification item).

### Human Verification Required

The phase already passed a live autonomous UAT (37-10) on isolated kind cluster `tide-uat37` — **8/8 surfaces verified with operator D-15 sign-off APPROVED 2026-07-09**, including the live approve-gate flow (no PVC reader pod), the GC'd-pod honest state, DASH-03 secret redaction, and the DASH-02 `lastPushedSHA`-advances observation. Both gaps surfaced there (37-G1 anonymous-http auth, 37-G2 reconnecting Reconnect button) were fixed and re-verified live. Those live behaviors are considered covered.

The one item this verifier could not independently confirm (no kind/Docker):

### 1. DASH-02 automated Layer B kind-suite green run

**Test:** On a kind-capable host, run `make test-int` (or `go test ./test/integration/kind/ -run TestArtifactStaging`).
**Expected:** Exit 0; `artifact_staging_test.go` green — ≥1 `*.md` under `.tide/planning/<kind>/<name>/` on the run branch, byte-identical to the stub planner doc, no `in.json`/`out.json` staged (D-04), `Status.Git.LastPushedSHA` advanced. Then flip DASH-02 → Complete in REQUIREMENTS.md with the reworded git-transport wording.
**Why human:** Needs a kind cluster + Docker, unavailable here; the executor left the run env-gated in 37-09. The test is authored, substantive, and compiles clean; the behavior was observed live in the 37-10 UAT — but the automated green run itself was not independently observed by this verifier.

### Gaps Summary

No goal gaps. All four ROADMAP success criteria are achieved in the current tree — artifacts exist, are substantive, are wired end-to-end, pass their unit/envtest/vitest suites, and were live-verified in the 37-10 UAT with operator D-15 sign-off. The phase goal (a self-sufficient approve-gate review surface — read artifacts, project settings, and honest log states without PVC reader pods) is met.

Status is `human_needed` (not `passed`) solely because one confirmation — the automated Layer B kind run for DASH-02 — requires live-cluster interaction this verifier cannot perform; per protocol it is surfaced rather than claimed. The `settings.go` baseRef misreport (WARNING, mitigated by the raw-spec card) and the DASH-02 doc-status/wording lag are recommended follow-ups but do not block goal achievement.

---

_Verified: 2026-07-15T06:16:41Z_
_Verifier: Claude (gsd-verifier)_
