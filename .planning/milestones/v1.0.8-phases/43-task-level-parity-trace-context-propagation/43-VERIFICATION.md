---
phase: 43-task-level-parity-trace-context-propagation
verified: 2026-07-16T18:29:07Z
status: passed
score: 4/4 must-haves verified
overrides_applied: 0
re_verification:
  previous_status: none
  note: "Initial verification. A code-review pass (43-REVIEW.md) found CR-01 (Critical); fix commit af7447e was independently confirmed present and correct in this pass."
---

# Phase 43: Task-Level Parity + Trace-Context Propagation Verification Report

**Phase Goal:** Close the last dispatch-chain gap (the Task/executor level has zero span emission today) and thread a W3C `traceparent` one hop at a time from the manager into both the subagent Job and the reporter Job, so a single Project run composes into ONE connected, navigable trace tree instead of five disconnected roots — with each level's IDs durably anchored in its own CRD status for later reads.
**Verified:** 2026-07-16T18:29:07Z
**Status:** passed
**Re-verification:** No — initial verification

## Goal Achievement

### Observable Truths (ROADMAP Success Criteria)

| # | Truth | Status | Evidence |
| --- | ------- | ---------- | -------------- |
| 1 | **TRACE-01** — Manager emits a real AGENT dispatch span at Task Job completion, same call-site pattern as the other four levels | ✓ VERIFIED | `emitTaskSpanOnce` (`task_controller.go:953`) mirrors the four planner marker-gated sites; called from BOTH terminal branches — `1045` (EnvelopeReadFailed, `envReadOK=false`) and `1070` (post-read, `envReadOK=true`, before the OutputValidation/OutputPaths/standard divergence). Heavy envtest block `SpanEmission — Task level` (`span_emission_test.go:1177`, 5 specs) passed: succeeded span w/ `tide.role=executor`+`tide.invocation.level=task`+AGENT kind, failed-Job, degraded-envelope, output-validation-path, idempotency. |
| 2 | **TRACE-02** — One connected trace: deterministic TraceID from Project UID, every level parents under its immediate parent | ✓ VERIFIED | `synthesizePlannerSpan` (`span_emission.go:136`) derives TraceID via `otelai.TraceIDFromUID(project.UID)` for all levels and threads `parentSpanID` into `trace.NewSpanContext` (no custom IDGenerator; `grep IDGenerator`=0). All 5 levels resolve+thread their immediate parent's persisted span ID. Heavy envtest per-level specs assert `TraceID()==TraceIDFromUID(UID)`, `Parent.SpanID()==seeded parent hex`, `Parent.Remote()==true`, Project root has invalid parent — all passed. |
| 3 | **PROP-01** — W3C `traceparent` present as data in both subagent Job and reporter Job at dispatch time (both pod hops) | ✓ VERIFIED (documented Args-not-Env deviation for reporter) | Subagent: conditional `TRACEPARENT` env (`jobspec.go:414-418`). Reporter: conditional `--traceparent` Arg (`reporter_jobspec.go:145-148`) — Args not Env per RESEARCH Pitfall 3 (file is 100% Args-based; intent "injected as data" satisfied). Pattern-4 distinction honored: dispatch reads PARENT's field, reporter reads OWN field, at all 4 planner levels + Task dispatch hop. `tide-reporter` registers `--traceparent` flag (`main.go:95`). Heavy specs `dispatch_traceparent_test.go` (3) + `task_dispatch_traceparent_test.go` (1) assert full `00-<tid>-<sid>-01` strings + absent-parent omission — passed. |
| 4 | **PROP-02** — Each level's trace/span IDs persist in that level's `.status` field, durable across reconciler restarts | ✓ VERIFIED (documented flat-field vs `.status.trace` deviation, CONTEXT-sanctioned) | Flat `{Level}TraceSpanID string` on all 5 CRD Status structs + Task's net-new `TaskSpanEmittedUID` (`*_types.go`); CRD manifests regenerated (`config/crd/bases/*.yaml` expose all 6 JSON props). Each of the 5 completion handlers writes its own span ID in a SECOND, separately-retried `retry.RetryOnConflict` patch distinct from the marker stamp. Heavy envtest re-Get assertions (`Status.{Level}TraceSpanID == span.SpanID().String()`) pass per level. CONTEXT.md:35 grants explicit field-name/shape discretion; PROP-02's `.status.trace` wording satisfied by intent. |

**Score:** 4/4 truths verified

### CR-01 Fix Verification (from 43-REVIEW.md — independently confirmed, not taken on faith)

| Item | Status | Evidence |
| ---- | ------ | -------- |
| `project != nil` guard added to marker-stamp gate | ✓ VERIFIED | Present in all three planner gates: `milestone_controller.go:574`, `phase_controller.go:526`, `plan_controller.go:570` — all now read `completedJob != nil && project != nil && ...SpanEmittedUID != string(completedJob.UID) && plannerSpanResolvable(...)`. Project's gate (`project_controller.go:1829`) correctly omits it (project there is the reconciled object, never nil). Task already had it (`task_controller.go:956`). |
| 3 regression envtest specs proving marker stays unstamped on resolution failure | ✓ VERIFIED | `span_emission_test.go:388/672/945` — one per level, seed an unresolvable parent ref, invoke the completion handler, assert `exp.GetSpans()` empty AND `Status.{Level}SpanEmittedUID` stays empty (so a later reconcile can still emit). All passed in the heavy tier. |

### Required Artifacts

| Artifact | Expected | Status | Details |
| -------- | ----------- | ------ | ------- |
| `internal/controller/span_emission.go` | parenting-aware synthesizer + `spanIDFromHexOrZero` + `traceparentForLevel` | ✓ VERIFIED | 248 lines; `trace.NewSpanContext` present, nil-project/TraceIDFromUID-error skip both implemented + doc-commented; both helpers exported & unit-tested. |
| `internal/controller/task_controller.go` | `emitTaskSpanOnce` + 2 call sites + dispatch `TraceParent` | ✓ VERIFIED | `emitTaskSpanOnce` def @953, calls @1045/@1070; dispatch `TraceParent: traceparentForLevel(project, parentPlanSpanHex)` @853 with parent-Plan Get @828-832. |
| `internal/dispatch/podjob/jobspec.go` | `BuildOptions.TraceParent` + conditional TRACEPARENT env | ✓ VERIFIED | Field @154-160; conditional env @414-418. |
| `internal/controller/reporter_jobspec.go` | `ReporterOptions.TraceParent` + conditional `--traceparent` Arg | ✓ VERIFIED | Field @81-86; conditional Arg @145-148; `grep Env:`=0 (Args-only preserved). |
| `cmd/tide-reporter/main.go` | `--traceparent` flag registration + `reporterConfig.TraceParent` | ✓ VERIFIED | `parseFlags` w/ `ContinueOnError` @85; flag @95; cfg field @67/@108. |
| `api/v1alpha3/*_types.go` (5) + `config/crd/bases/*.yaml` (5) | 6 additive status fields + regenerated manifests | ✓ VERIFIED | All 5 `{Level}TraceSpanID` + `TaskSpanEmittedUID` present in Go types; all 6 JSON props present in regenerated CRD manifests. |
| `internal/controller/dispatch_traceparent_test.go` | envtest proof of both hops, ≥80 lines | ✓ VERIFIED | 346 lines; 3 specs (dispatch present/absent + reporter hop) — all heavy, all passed. |
| `internal/controller/span_emission_test.go` | 5th "SpanEmission — Task level" Describe block | ✓ VERIFIED | Block @1177 (5 specs); + 3 CR-01 regression specs. |
| `internal/controller/task_dispatch_traceparent_test.go` | Task dispatch-hop envtest | ✓ VERIFIED | 167 lines; 1 heavy spec asserting full W3C TRACEPARENT — passed. |

### Key Link Verification

| From | To | Via | Status | Details |
| ---- | --- | --- | ------ | ------- |
| `{milestone,phase,plan}_controller.go` dispatch-prep | parent CRD `.status.{Level}TraceSpanID` | `traceparentForLevel(project, parentHex)` → `BuildOptions.TraceParent` | ✓ WIRED | Milestone @465 (parent=Project field), Phase @441 (`parentMs.MilestoneTraceSpanID`), Plan @461 (`parentPh.PhaseTraceSpanID`), Task @853 (`parentPlan.PlanTraceSpanID`). Project dispatch @1739 correctly carries none (root). |
| `*_controller.go` completion handlers | `ReporterOptions.TraceParent` | own-span field (Pattern 4) | ✓ WIRED | Milestone @640, Phase @593 (via `spawnReporterIfNeeded` trailing `traceParent` param, `dispatch_helpers.go:102/121`), Plan @649, Project @1904 (inline literals) — every site reads the level's OWN `{Level}TraceSpanID`, never the parent's. |
| `task_controller.go handleJobCompletion` | `span_emission.go synthesizePlannerSpan` | `emitTaskSpanOnce` w/ `level="task"`, parent from Plan status | ✓ WIRED | @997 with `parentSpanID` from `parentPlan.Status.PlanTraceSpanID`. |
| `span_emission.go` | `pkg/otelai/tracecontext.go` | `TraceIDFromUID` + `FormatTraceparent` (first prod call sites) | ✓ WIRED | `TraceIDFromUID` @158/@242, `FormatTraceparent` @246. |

### Behavioral Spot-Checks / Probe Execution

| Check | Command | Result | Status |
| -------- | ------- | ------ | ------ |
| Build (relevant pkgs) | `go build ./internal/... ./api/... ./pkg/... ./cmd/tide-reporter/...` | exit 0 | ✓ PASS |
| Vet | `go vet ./internal/controller/... ./internal/dispatch/podjob/... ./cmd/tide-reporter/... ./api/...` | exit 0 | ✓ PASS |
| Unit-tier span tests | `go test ./internal/controller/ -run 'TestSynthesizePlannerSpan\|TestSpanIDFromHexOrZero\|TestTraceparentForLevel\|...'` | ok | ✓ PASS |
| Job-builder carrier tests | `go test ./internal/dispatch/podjob/ -run TestBuildJobSpec` + `TestBuildReporterJob` + `./cmd/tide-reporter/` | ok (all 3) | ✓ PASS |
| Heavy controller envtest tier | `KUBEBUILDER_ASSETS=… go test ./internal/controller/... -ginkgo.label-filter=heavy` | Ran 38 of 230, 0 failures, `ok 38.4s` | ✓ PASS |
| SpanEmission heavy focus | `… -ginkgo.focus=SpanEmission` | ok 12.8s | ✓ PASS |

Envtest was run in the verifier's own process (envtest binaries at `bin/k8s/1.33.0-darwin-amd64`; etcd/kube-apiserver present). The 38 heavy specs include all 5 `SpanEmission` blocks, the 3 CR-01 regression specs, `dispatch_traceparent_test.go` (3), and `task_dispatch_traceparent_test.go` (1) — SUMMARY PASS claims independently reproduced.

### Requirements Coverage

| Requirement | Source Plan(s) | Description | Status | Evidence |
| ----------- | ---------- | ----------- | ------ | -------- |
| TRACE-01 | 43-05 | Retroactive AGENT span at Task completion | ✓ SATISFIED | Truth 1 |
| TRACE-02 | 43-03, 43-05 | One deterministic-TraceID trace, correct parenting | ✓ SATISFIED | Truth 2 |
| PROP-01 | 43-02, 43-04, 43-05 | traceparent as data at both pod hops | ✓ SATISFIED | Truth 3 |
| PROP-02 | 43-01, 43-03, 43-05 | Per-level IDs persist in `.status` | ✓ SATISFIED | Truth 4 |

All 4 phase requirement IDs (TRACE-01, TRACE-02, PROP-01, PROP-02) accounted for across plan frontmatter; REQUIREMENTS.md maps exactly these 4 to Phase 43. No orphaned requirements.

### Documented Deviations (both intent-satisfying, not gaps)

1. **PROP-01 reporter uses `--traceparent` Arg, not env** — reporter_jobspec.go is 100% Args-based (`grep Env:`=0); RESEARCH Pitfall 3 scopes the literal env mechanism to `jobspec.go`. The requirement's intent ("injected as data … runtime-neutral contract at both pod hops") is fully met; the flag is registered in `tide-reporter` in the same commit (crash-loop-proof). Documented in 43-02-PLAN/SUMMARY.
2. **PROP-02 uses flat `{Level}TraceSpanID` fields, not nested `.status.trace`** — 43-CONTEXT.md:35 grants explicit field-name/shape discretion, preferring house-style flat fields matching the existing `{Level}SpanEmittedUID` markers. Durable, small, re-readable carrier intent satisfied.

*Optional:* if you want either deviation formally recorded, add matching `overrides:` entries to this file's frontmatter — but neither is required, as both achieve the requirement's intent and were sanctioned at planning time.

### Anti-Patterns Found

| File | Line | Pattern | Severity | Impact |
| ---- | ---- | ------- | -------- | ------ |
| (none) | — | No TODO/FIXME/XXX/HACK/PLACEHOLDER in any modified source file | — | Clean |
| `task_controller.go` | 1023-1024 | `//nolint:unparam` + `//nolint:gocyclo` on `handleJobCompletion` | ℹ️ Info | Justified inline with commit 9cae6bb precedent (flat mutually-exclusive completion arms); not a debt marker. |

### Non-Blocking Observations (from 43-REVIEW.md — informational, do not affect status)

- **WR-01** (post-emission `{Level}TraceSpanID` persist failure is unretried/unsignalled): documented, accepted graceful-degradation tradeoff — a transient API failure AFTER the span already exported degrades that descendant's parent-linkage to an unnested span, never a crash or duplicate. Not a phase-goal blocker; suggested follow-up is an operator-facing metric/condition (dashboard/observability territory, Phase 46).
- **WR-02** (Task AGENT span status reflects Job outcome, not the Task's final controller-side verdict): intentional per D-07 (unit of instrumentation = Job attempt) and consistent with all four planner levels. A documentation nuance, not a bug.
- **IN-01** (redundant parent fetch on Phase/Plan dispatch+completion) and **IN-02** (`--traceparent` parsed but unconsumed until Phase 44): both intentional/forward-compat; no action this phase.

### Scope Boundaries (correctly deferred, not gaps)

- Task's reporter Job + reporter-hop traceparent → Phase 44 (MSG-01). Task has no reporter Job this phase; only its dispatch hop is in scope and is wired.
- Reporter-side consumption of `--traceparent` (TracerProvider) → Phase 44 (TRACE-03).
- End-to-end connected trace visible/queryable in self-hosted Phoenix → Phase 47 (PROOF-01, the milestone acceptance bar).

### Human Verification Required

None. This phase is backend trace-context plumbing with no UI, real-time, or external-service surface; every success criterion is code-level and fully proven by the heavy envtest tier, which the verifier re-ran and reproduced (38/38 heavy specs, 0 failures). Phoenix end-to-end visibility is out of scope (PROOF-01/Phase 47).

### Gaps Summary

No gaps. All four ROADMAP success criteria are observably true in the codebase and proven by a re-run of the heavy envtest tier. The CR-01 Critical from code review is independently confirmed fixed (guards present in all three planner gates + 3 passing regression specs). The two documented deviations (reporter Args-not-Env; flat fields vs `.status.trace`) are intent-satisfying and were sanctioned at planning time. The five-level trace tree (Project → Milestone → Phase → Plan → Task) shares one deterministic TraceID with correct parent linkage, each level persists its own span ID, and both dispatch pod hops carry the W3C traceparent as data.

---

_Verified: 2026-07-16T18:29:07Z_
_Verifier: Claude (gsd-verifier)_
