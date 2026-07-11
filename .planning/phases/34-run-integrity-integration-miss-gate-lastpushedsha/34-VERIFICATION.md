---
phase: 34-run-integrity-integration-miss-gate-lastpushedsha
verified: 2026-07-11T17:16:00Z
status: passed
score: 5/5 must-haves verified
behavior_unverified: 0
overrides_applied: 0
---

# Phase 34: Run-Integrity Integration-Miss Gate + lastPushedSHA — Verification Report

**Phase Goal:** A pushed run branch provably contains every Succeeded task's work — the wave-parallel integration step cannot silently drop a merge, boundary push is gated on completeness verified from git, and a run can no longer stamp Complete while a declared deliverable is missing from the branch.

**Verified:** 2026-07-11T17:16:00Z
**Status:** passed
**Re-verification:** No — initial verification

## Context note

Phase 34 executed 6/6 in a cloud sandbox that had no Docker/kind (per the 34-01 and 34-06 SUMMARYs, the RED and GREEN kind runs were deferred). Verification here is against the **current tree** (main's tip, with Phases 35–38 merged on top). Every Phase 34 deliverable was found still present and correctly wired, and later phases build on them. The Layer A envtest, unit, and data-plane tiers were run locally (they cover the INTEG state transitions); the Layer B kind suite is env-gated on this host by policy and its execution evidence is cited from CI (see SC-5).

## Goal Achievement

### Observable Truths (= ROADMAP Success Criteria, the contract)

| # | Truth | Status | Evidence |
|---|-------|--------|----------|
| 1 | Every Succeeded task's branch is merged into the run branch, incl. the final Kahn wave (the `plan_controller.go:1192` last-wave skip is closed) | ✓ VERIFIED | Loop is now `for k := range layers` (plan_controller.go:1358) iterating **every** wave incl. the last, with an INTEG-01 comment (:1351-1357) citing the closed `k < len(layers)-1` skip; per-wave dispatch in `reconcileWaveBoundary`. Behavioral: `TestPlanReconcilerSingleWaveDispatchesIntegrationJob` PASS, `TestPlanReconcilerFinalWaveIntegratesAndGatesSucceeded` PASS (envtest) |
| 2 | Tasks run in parallel; run-branch merges are serialized + idempotent (cumulative Succeeded set; safe re-merge) | ✓ VERIFIED | `succeededTaskBranches` (git_writer.go:58) computes the cumulative set inside every trigger; `gitWriterInFlightCount` (git_writer.go:104) + `errGitWriterBusy` (boundary_push.go:37) single-flight gate; kernel `unix.Flock` (tide-push main.go:735); `MergeConflictError` + defensive/failure-path `merge --abort` (integrate.go:99,117). Behavioral: `TestPlanReconcilerWaveDispatchGatedByGitWriterBusy` PASS; pkg/git idempotent-re-merge + self-heal PASS; `TestRunPushHoldsFlockAcrossIntegrateVerifyPush` + `TestStageEnvelopesIdempotentRestage` PASS |
| 3 | Boundary push fires only when `merge-base --is-ancestor` confirms every Succeeded branch is integrated (recomputed from git, never cached); a miss raises a sticky integration-incomplete condition | ✓ VERIFIED | `verifyIntegrationComplete` (tide-push main.go:959) runs `git merge-base --is-ancestor <br> <runBranch>` per expected branch before push → `exitIntegrationMiss=14`; controller parks on `ConditionIntegrationIncomplete` (project_controller.go, 8 refs) at cap and immediately on `pushEnvelopeReasonMergeConflict` (:1090). Behavioral: `TestVerifyIntegrationCompleteDetectsMiss/PassesWhenMerged/EmptyDiff/TruncationAtEnvelopeWrite` PASS; 26 controller condition/conflict/boundary-push specs PASS |
| 4 | After a successful boundary push, `status.git.lastPushedSHA` shows the push envelope's HeadSHA | ✓ VERIFIED | `project.Status.Git.LastPushedSHA = env.HeadSHA` (project_controller.go:1025) set in the SAME MergeFrom patch that clears LeaseFailureCount, immediately before `BoundaryPushed=True` (:1030). Behavioral: 5 `LastPushedSHA` Ginkgo specs PASS incl. "advances from the SUCCEEDED pod even when a failed-attempt pod sorts first" |
| 5 | A kind-suite regression test reproduces the 2-parallel-task final-wave miss and locks the fix | ✓ VERIFIED | `test/integration/kind/integration_miss_test.go` (799 lines, package `kind_integration`, git-tracked, `go vet` clean) — spec "integration miss: 2-parallel-task final wave integrates all three task branches and stamps lastPushedSHA" asserts all 3 branches are ancestors (`assertBranchesAreAncestors` via `merge-base --is-ancestor` Job) + `LastPushedSHA` non-empty. Wired into `kind-sensitive.yml` (`make test-int-kind`) and `nightly-integration.yml`. Execution evidence: CI Layer B green on main's last two PRs (#8: 26/26 specs; #9: 26m27s) — and the spec's own last commit is PR #8 "make the Layer B kind suite deterministically green". Not run locally (env-gated by policy) |

**Score:** 5/5 truths verified (0 present, behavior-unverified)

### Required Artifacts

| Artifact | Expected | Status | Details |
|----------|----------|--------|---------|
| `internal/controller/plan_controller.go` | Full-range wave loop + bounded retry + conflict→Plan Failed | ✓ VERIFIED | `for k := range layers` :1358; `maxWaveIntegrationAttempts=5` :67; conflict single-shot :1187; `DeletePropagationBackground` :1231 |
| `internal/controller/boundary_push.go` | Cumulative set inside trigger + D-02 gate + `errGitWriterBusy` | ✓ VERIFIED | `succeededTaskBranches` :155, gate :137, sentinel :37; `taskItems` param gone (only a removal comment remains :284) |
| `internal/controller/git_writer.go` | 3 shared helpers | ✓ VERIFIED | `succeededTaskBranches` :58, `gitWriterInFlightCount` :104, `readJobPushEnvelope` :152 |
| `internal/controller/project_controller.go` | SHA stamp + condition arms + mid-run observation + reset annotation | ✓ VERIFIED | `LastPushedSHA = env.HeadSHA` :1025; `dispatchIfMissing` :823/:966; `pushEnvelopeReasonMergeConflict` arm :1090; `ConditionIntegrationIncomplete` ×8 |
| `cmd/tide-push/main.go` | flock + verify gate + exit 14/15 + wave-success envelope | ✓ VERIFIED | `unix.Flock` :735; `verifyIntegrationComplete` :959; exit codes :255-256; conflict/miss envelope :763/:789 |
| `pkg/git/integrate.go` | `MergeConflictError` + merge-abort hygiene | ✓ VERIFIED | type :139; defensive-start + failure-path abort :99/:117 |
| `cmd/tide/resume.go` + `internal/gates/annotation.go` | D-13 reset via annotation | ✓ VERIFIED | `gates.AnnotationResetBoundaryPush` (annotation.go:62); stamped in resume.go:96; consumed by controller |
| API vocabulary + `WaveIntegrationStatus` (both versions) + CRD | Parity + regenerated schema | ✓ VERIFIED | 7 const refs in each `shared_types.go`; `WaveIntegrationStatus struct` ×1 in each `plan_types.go`; `waveIntegration` ×2 in CRD YAML (both served versions) |
| `internal/metrics/registry.go` | `tide_integration_outcomes_total` | ✓ VERIFIED | present ×1; metric test PASS |
| `test/integration/kind/integration_miss_test.go` | GREEN regression lock | ✓ VERIFIED | 799 lines, both "integration miss" specs, unweakened assertions, CI-green |

### Key Link Verification

| From | To | Via | Status |
|------|-----|-----|--------|
| plan_controller.go | git_writer.go | `gitWriterInFlightCount` gate + `readJobPushEnvelope` classification | ✓ WIRED (:1139, :1185) |
| boundary_push.go | git_writer.go | `succeededTaskBranches` cumulative set inside shared trigger | ✓ WIRED (:155) |
| project_controller.go | git_writer.go | success/failure arms read envelope; dispatch uses cumulative set + gate | ✓ WIRED (:1025 stamp, succeededTaskBranches dispatch) |
| cmd/tide-push/main.go | pkg/git/integrate.go | `errors.As(&MergeConflictError)` classifies integrate failure | ✓ WIRED (:763) |
| cmd/tide-push/main.go | project_controller.go | envelope JSON tags (`missingBranches`/`conflictBranch`) match `pushResultEnvelope` | ✓ WIRED (cross-binary contract; both sides carry identical tags) |
| cmd/tide/resume.go | project_controller.go | `reset-boundary-push` annotation set by CLI, consumed once by controller | ✓ WIRED (gates.AnnotationResetBoundaryPush) |
| integration_miss_test.go | plan_controller.go | spec GREEN depends on full-range loop landing | ✓ WIRED (CI Layer B green incl. this spec) |

### Behavioral Spot-Checks

| Behavior | Command | Result | Status |
|----------|---------|--------|--------|
| Data plane: verify gate, flock, conflict, idempotent restage | `go test ./pkg/git/ ./cmd/tide-push/ -count=1` | both `ok` | ✓ PASS |
| INTEG-01 wave loop + gate + conflict park | `go test ./internal/controller/ -run 'TestPlanReconciler...'` | 5/5 PASS | ✓ PASS |
| INTEG-04 SHA stamp | `-ginkgo.focus='LastPushedSHA'` | Ran 5, SUCCESS! 5 Passed 0 Failed | ✓ PASS |
| INTEG-03 sticky condition / conflict / boundary-push | `-ginkgo.focus='ntegration.incomplete\|conflict\|boundary.push'` | Ran 26, SUCCESS! 26 Passed 0 Failed | ✓ PASS |
| D-13 reset annotation | `go test ./cmd/tide/ -run Resume` | `ok` | ✓ PASS |
| API parity + metric | `go test ./api/... ./internal/metrics/` | all `ok` | ✓ PASS |
| Build (all INTEG packages) | `go build ./internal/controller/ ./cmd/... ./pkg/git/ ./api/...` | BUILD_EXIT=0 | ✓ PASS |

### Probe Execution

| Probe | Command | Result | Status |
|-------|---------|--------|--------|
| Layer B kind suite (SC-5) | `make test-int-kind` | Env-gated on this host by policy; not run locally | ? SKIP (CI evidence cited: PRs #8 26/26, #9 26m27s green) |

### Requirements Coverage

| Requirement | Source Plan(s) | Description | Status | Evidence |
|-------------|----------------|-------------|--------|----------|
| INTEG-01 | 34-04, 34-06 | Final-Kahn-wave / single-wave integration (close :1192 skip) | ✓ SATISFIED | Full-range loop + two passing envtest specs |
| INTEG-02 | 34-02, 34-03, 34-04, 34-06 | Serialized, idempotent merges; cumulative set; flock | ✓ SATISFIED | Gate + flock + idempotent-remerge tests |
| INTEG-03 | 34-02, 34-03, 34-05, 34-06 | Push gated on `merge-base --is-ancestor`; sticky miss condition | ✓ SATISFIED | verify gate + 26 condition specs |
| INTEG-04 | 34-02, 34-05, 34-06 | `lastPushedSHA` = envelope HeadSHA | ✓ SATISFIED | :1025 + 5 SHA specs |
| INTEG-05 | 34-01, 34-06 | Kind regression repro locks the fix | ✓ SATISFIED | Spec exists + wired + CI green (#8/#9) |

No orphaned requirements — all 5 IDs mapped to Phase 34 in REQUIREMENTS.md are claimed by plans and satisfied.

### Anti-Patterns Found

None. No `TBD`/`FIXME`/`XXX` debt markers in any Phase 34 production file (plan_controller.go, boundary_push.go, git_writer.go, project_controller.go, tide-push/main.go, integrate.go, resume.go). No stub returns feeding user-visible output.

### Gaps Summary

None. Every ROADMAP success criterion is observably satisfied in the current tree: the last-wave skip is structurally closed (`for k := range layers`), merges are serialized behind a single-flight gate + kernel flock and are idempotent, the push is gated on a git-recomputed `merge-base --is-ancestor` verify with a sticky `IntegrationIncomplete` condition on a miss, `lastPushedSHA` is stamped from the envelope HeadSHA in the same patch as `BoundaryPushed=True`, and the 2-parallel-task final-wave kind regression spec exists, is unweakened, and is wired into the CI Layer B tier (green on PRs #8 and #9). Local Layer A/unit/data-plane tiers covering these transitions all pass. Phase 34's own sandbox could not run kind (34-01/34-06 deferred), but the deliverables landed and later phases (35–38) build on them intact.

---

_Verified: 2026-07-11T17:16:00Z_
_Verifier: Claude (gsd-verifier)_
