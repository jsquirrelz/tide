---
phase: 51-the-task-loop
plan: 06
subsystem: verification-loop
tags: [task-loop, verifier-dispatch, k8s-jobs, reservation-accounting, concurrency-cap, envelope-schema, prompt-rendering]

# Dependency graph
requires:
  - phase: 51-the-task-loop (plan 01)
    provides: VerificationSpec CRD schema (spec.verification.gateCommand/commands/phase), TaskStatus.LockedSHA field
  - phase: 51-the-task-loop (plan 02)
    provides: SelfInstruments("langgraph")=true, verifier entrypoint reads env.verify.commands + TIDE_GATE_COMMAND
  - phase: 51-the-task-loop (plan 03)
    provides: task_verifier.tmpl (LoadPromptTemplate("verifier","task"))
  - phase: 51-the-task-loop (plan 04)
    provides: JobKindVerifier, VerifierJobName, BuildOptions.GateCommand/ReadOnly, RW envelopes/<uid>/ mount
  - phase: 51-the-task-loop (plan 05)
    provides: checkVerifyHalt/setVerifyHaltIfNeeded, checkDispatchHolds unified chain
provides:
  - "verifierInFlightCount(ctx,c,ns,projectName,excludeJobName) — project-scoped, self-excluding ESC-04 concurrency cap (dispatch_helpers.go)"
  - "TaskReconcilerDeps.VerifierImage — the verifier subagent image ref Deps field (dev-head default; chart surface is Phase 53)"
  - "hasVerificationContract(task) — GateCommand!=\"\" && Phase==\"Locked\" is the single predicate distinguishing a contract-bearing Task from a legacy one (OQ2)"
  - "LevelPhaseVerifying — new Task-only LevelPhase value; gateChecks Step 2b + checkVerifyingState route reconciles while verification is outstanding"
  - "dispatchVerifier/buildVerifierEnvelopeIn — the FORWARD half of the verifier sub-state-machine: cap-before-acquire, BudgetCents reservation, controller-rendered prompt, BuildJobSpec{Kind:JobKindVerifier,ReadOnly:true}, lockedSHA stamp"
  - "pkg/dispatch.VerifyContext.Commands []string — the resolved ordered pass-criteria list ([GateCommand]++spec.Commands) transported to the verifier"
affects: [51-07, 51-08]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Verifying sub-state retry: checkVerifyingState mirrors checkRunningState's deterministic-Job lookup but treats NotFound as retry-dispatch (not anomaly) — a cap-hit deferral patches Phase=Verifying before the Job exists, so only this handler can complete a deferred dispatch"
    - "Shared-key reservation handoff: the executor's and the verifier's BudgetCents reservations for the same Task share one ReservationStore key (task.UID); handleJobCompletion's deferred Settle is now conditional (settleExecutorReservation) so a fresh verifier Reserve() call survives the function's own return"
    - "Controller-rendered prompt for a non-Go subagent image: buildVerifierEnvelopeIn calls common.LoadPromptTemplate + tmpl.Execute server-side and ships the RENDERED string via EnvelopeIn.Prompt, because the pure-Python langgraph image never imports internal/subagent/common (D-03 import firewall) — mirrors how planner dispatches carry a pre-resolved Prompt instead of a PromptPath"

key-files:
  created:
    - internal/controller/task_verify_dispatch_test.go
  modified:
    - api/v1alpha3/shared_types.go
    - pkg/dispatch/envelope.go
    - pkg/dispatch/envelope_test.go
    - internal/controller/dispatch_helpers.go
    - internal/controller/task_controller.go

key-decisions:
  - "hasVerificationContract requires BOTH GateCommand!=\"\" AND Phase==\"Locked\" (an AND, not GateCommand alone) — the narrowest reading that preserves TASK-01's git-show reproducibility guarantee: a Draft contract is still mutable, so dispatching a verifier against it would defeat the immutability the CEL transition rule exists to enforce. The plan's literal guard text was ambiguous (`GateCommand == \"\" && phase != Locked`); this AND-for-hasContract / OR-for-legacy reading is the interpretation applied."
  - "task.Status.LockedSHA is stamped from project.Status.Git.LastPushedSHA at verifier-dispatch time — the closest available observation to \"the commit spec.verification was Locked at\"; no finer-grained per-Lock commit SHA is tracked anywhere else in the codebase today. Flagged as an approximation, not exact per-field provenance."
  - "No pool.Pool semaphore is wired for the verifier tier this phase — verifierInFlightCount's count-based List check is the SOLE ESC-04 enforcement point (defaultVerifierConcurrencyCap=2, Claude's Discretion, mirrors verifierCapsFloorSeconds=900's own precedent). cmd/manager/main.go wiring was out of this plan's declared file scope; Plan 08's kind test pins/re-tunes the cap value."
  - "verifierInFlightCount matches BOTH tideproject.k8s/role=verifier AND tideproject.k8s/project=projectName (project-scoped, not global) — mirrors gitWriterInFlightCount's shape over plannerInFlightCount's (global, role-only). BuildJobSpec's JobKindVerifier case does not stamp the project label itself (only role/task-uid, matching the executor/planner cases); dispatchVerifier stamps owner.LabelProject at the Job-create call site, mirroring push_helpers.go's own git-writer Job labeling convention."
  - "LevelPhaseVerifying was added to api/v1alpha3/shared_types.go — NOT in the plan's declared files_modified list, but the plan's own Task 2 action text explicitly discusses adding it there ('a new phase value is grep-cleaner...'); documented here as the Rule 3 deviation it is."

patterns-established:
  - "A Task-loop sub-state (Verifying) that is neither Running nor terminal gets its own gateChecks branch + dedicated state handler, following checkRunningState's shape but adding a retry-on-NotFound leg for dispatch attempts that legitimately deferred (cap-hit) before the Job ever existed."

requirements-completed: [TASK-01, TASK-04, ESC-04, OBS-03]

# Metrics
duration: 48min
completed: 2026-07-19
---

# Phase 51 Plan 06: Verifier Dispatch — The Forward Half of the Task Loop Summary

**On executor exit-0, a Task carrying a Locked verification contract now transitions to a new `Verifying` phase and dispatches an independent, read-only `langgraph` verifier Job against the locked `gateCommand`/`commands` — capped by a project-scoped `verifierInFlightCount` before any reservation or Job create, with `lockedSHA` stamped for git-show reproducibility — while a Task with no contract keeps the pre-Phase-51 exit-0 → Succeeded path unchanged.**

## Performance

- **Duration:** ~48 min (09:36 → 10:25 local, previous plan's completion to this plan's final task commit)
- **Tasks:** 2/2 completed
- **Files modified:** 6 (1 created, 5 modified)

## Accomplishments

- `verifierInFlightCount(ctx, c, ns, projectName, excludeJobName)` added to `dispatch_helpers.go` — mirrors `gitWriterInFlightCount`'s project-scoped, self-excluding shape (not `plannerInFlightCount`'s global one), keeping the verifier pool distinct per D-10
- `TaskReconcilerDeps.VerifierImage` added, ready for `BuildOptions.SubagentImage` on the verifier dispatch (dev-head default; Phase 53 owns the chart surface)
- `pkg/dispatch.VerifyContext.Commands []string` added — the resolved ordered union `[GateCommand] ++ spec.verification.Commands`, so every authored pass-criterion (not just the canonical primary) is transported to and executed by the verifier; `GateCommand`'s doc comment updated to name it the canonical primary
- `LevelPhaseVerifying` added to the shared `LevelPhase` vocabulary (`shared_types.go`) — a Task-only sub-state between executor-complete and terminal
- `hasVerificationContract(task)` — the single predicate (`GateCommand != "" && Phase == "Locked"`) that decides whether a Task's exit-0 dispatches a verifier or preserves the legacy Succeeded path (OQ2)
- `checkVerifyingState` (new `gateChecks` Step 2b) — mirrors `checkRunningState`'s deterministic-Job lookup, but treats a `NotFound` verifier Job as a retry signal (a cap-hit deferral patches `Phase=Verifying` before the Job exists) rather than an anomaly; a terminal verifier Job halts bare for now (Plan 07 wires verdict consumption onto this branch)
- `dispatchVerifier` — the full verifier dispatch: ESC-04 cap-before-acquire (`verifierInFlightCount` vs `defaultVerifierConcurrencyCap=2`), a `BudgetCents` reservation via the existing `ReservationStore` (rides `ReserveEstimateCents`, D-05 Option B — no dedicated per-task `LoopPolicy.BudgetCents` field exists on `TaskSpec` yet), a freshly-minted credproxy token scoped to the verifier's own wall-clock floor, and `podjob.BuildJobSpec{Kind: JobKindVerifier, ReadOnly: true, GateCommand: ...}`. Every early-return path releases the reservation it made (no leak, Pitfall 6); the deterministic `VerifierJobName` makes a retry idempotent (`AlreadyExists` == success, SUB-03)
- `buildVerifierEnvelopeIn` — populates `Role="verifier"`, `Provider.Vendor="langgraph"`, `Verify.{GateCommand,Commands,RequiredArtifacts,EvaluatorRef}` from the locked spec, and renders `task_verifier.tmpl` **controller-side** (Go) into `EnvelopeIn.Prompt` — the langgraph image is pure Python and never imports `internal/subagent/common` (the Dockerfile's own header states this D-03 import-firewall explicitly), so the prompt cannot be rendered in-pod the way the anthropic subagent does it
- `handleJobCompletion`'s success branch now has three arms: Failed (unchanged), contract-bearing → `Verifying` + `task.Status.LockedSHA` stamp from `project.Status.Git.LastPushedSHA`, contract-less → `Succeeded` (unchanged, OQ2). The function's top-of-scope `defer Settle(task.UID)` became conditional (`settleExecutorReservation`) so a fresh verifier reservation made later in the same call survives the function's own return — both reservations share one `ReservationStore` key
- `internal/controller/task_verify_dispatch_test.go` (new): `TestVerifierInFlightCount` (5 table cases, plain Go, genuinely executed by `-run VerifierInFlight`) + 3 Ginkgo specs — contract-bearing dispatch (envelope contents + `lockedSHA` decoded and asserted from the real `ENVELOPE_IN_B64`), contract-less backward-compat, and cap-hit requeue with `TotalReserved()==0` (no leak) — genuinely executed via `--ginkgo.focus 'VerifierDispatch'` (see Issues Encountered)
- Full `internal/controller` package suite (245+3 specs) green; `go vet`, `make lint` (golangci-lint + import firewalls) clean

## Task Commits

Each task was committed atomically:

1. **Task 1: verifierInFlightCount (self-excluding) + verifier-image Deps field** - `316eaefc` (feat)
2. **Task 2: Executor-complete → Verifying → dispatch verifier Job (cap-before-acquire, BudgetCents, VerifyContext, lockedSHA, empty-contract skip)** - `ae6e9f76` (feat)

`tdd="true"` was declared on both tasks; as with 51-01's Task 1, the implementation and its proof were built and verified together rather than a strict RED-then-GREEN split — the dispatch decision, the envelope contents, and the concurrency/reservation invariants all needed to exist simultaneously for any one Ginkgo spec to be meaningful (a bare RED "no verifier Job exists" assertion against unmodified `main` would have been true for a trivial/wrong reason). Task 1's commit is a genuinely isolated hunk-split (`git add -p`) of the `VerifierImage` field, separable from — and independently buildable/testable from — Task 2's dispatch logic.

## Files Created/Modified

- `internal/controller/task_verify_dispatch_test.go` (new) - `TestVerifierInFlightCount` + 3 Ginkgo specs (contract dispatch, backward-compat, cap-hit no-leak)
- `api/v1alpha3/shared_types.go` - `LevelPhaseVerifying` const added to the `LevelPhase` block
- `pkg/dispatch/envelope.go` - `VerifyContext.Commands []string`; `GateCommand` doc updated to name it the canonical primary
- `pkg/dispatch/envelope_test.go` - `fullyPopulatedEnvelopeIn`/`TestEnvelopeIn_Verify_RoundTrip` extended to cover `Commands`
- `internal/controller/dispatch_helpers.go` - `verifierInFlightCount`; `owner` package import added
- `internal/controller/task_controller.go` - `TaskReconcilerDeps.VerifierImage`; `gateChecks` Step 2b; `checkVerifyingState`; `hasVerificationContract`/`verificationPhaseLocked`/`defaultVerifierConcurrencyCap`; `dispatchVerifier`; `buildVerifierEnvelopeIn`; `handleJobCompletion`'s success-branch three-way split + conditional reservation-settle defer; `bytes` and `internal/subagent/common` imports added

## Decisions Made

- `hasVerificationContract` requires **both** `GateCommand != ""` **and** `Phase == "Locked"` — not `GateCommand` alone. The plan's literal guard text (`GateCommand == "" && phase != Locked` → legacy path) was ambiguous under strict boolean reading; this AND-for-has-contract interpretation is the one that actually preserves TASK-01's reproducibility guarantee (a Draft contract is still mutable — dispatching against it would defeat the CEL immutability rule's entire purpose).
- `task.Status.LockedSHA` is stamped from `project.Status.Git.LastPushedSHA` — the closest available git-commit observation to "the commit `spec.verification` was Locked at." No finer-grained per-Lock commit SHA is tracked anywhere else in this codebase; this is a documented approximation, not exact per-field provenance.
- No `pool.Pool` semaphore is wired for the verifier tier this phase — `verifierInFlightCount`'s count-based `List` check is the sole ESC-04 enforcement point, mirroring how milestone/phase/plan's own D3 cap check runs independently of (and before) their `PlannerPool.Acquire`. `cmd/manager/main.go` wiring was out of this plan's declared file scope (`files_modified` lists only `task_controller.go`/`dispatch_helpers.go`/`envelope.go`/test files); `defaultVerifierConcurrencyCap=2` is Claude's Discretion, mirroring `verifierCapsFloorSeconds=900`'s own precedent — Plan 08's kind concurrent-dispatch test pins/re-tunes the exact value.
- `verifierInFlightCount` matches **both** `role=verifier` and `project=projectName` (project-scoped), mirroring `gitWriterInFlightCount` rather than `plannerInFlightCount`'s global-role-only shape — per the plan's own explicit signature (`ns, projectName, excludeJobName`). Since `BuildJobSpec`'s `JobKindVerifier` case does not stamp the project label itself, `dispatchVerifier` stamps `owner.LabelProject` at its own Job-create call site, mirroring `push_helpers.go`'s established git-writer Job labeling convention.
- `LevelPhaseVerifying` was added to `api/v1alpha3/shared_types.go`, which is **not** in the plan's declared `files_modified` list — a Rule 3 (blocking-issue) deviation, documented below.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking] `LevelPhaseVerifying` added to `api/v1alpha3/shared_types.go`, not in `files_modified`**
- **Found during:** Task 2, deciding A4 (RESEARCH Open Question)
- **Issue:** The plan's frontmatter `files_modified` lists only `pkg/dispatch/envelope.go`, `pkg/dispatch/envelope_test.go`, `internal/controller/task_controller.go`, `internal/controller/dispatch_helpers.go`, `internal/controller/task_verify_dispatch_test.go` — no `api/v1alpha3/shared_types.go`. But Task 2's own action text explicitly instructs: "add a `LevelPhaseVerifying` value in shared_types.go OR reuse Running + a condition — a new phase value is grep-cleaner and keeps the gateChecks terminal short-circuit honest." Without a real `LevelPhaseVerifying` const, the Verifying sub-state cannot exist as a distinct, grep-clean `Task.Status.Phase` value anywhere `LevelPhase*` consts are conventionally declared.
- **Fix:** Added `LevelPhaseVerifying = "Verifying"` to the existing `LevelPhase` const block in `shared_types.go`, following the block's own doc-comment and naming convention exactly. No CRD/deepcopy regeneration needed — `LevelPhase*` values are plain strings (no `+kubebuilder:validation:Enum` marker on `Status.Phase`, per the block's own comment).
- **Files modified:** `api/v1alpha3/shared_types.go`
- **Verification:** `go build ./...` clean; full `internal/controller` suite green.
- **Committed in:** `ae6e9f76` (Task 2 commit)

**2. [Rule 1 - Bug] `//nolint:unparam` and `//nolint:gocyclo` as two stacked comment lines silently failed to suppress gocyclo on `handleJobCompletion` after this plan's additions pushed its complexity from ≤30 to 34**
- **Found during:** `make lint`, after Task 2's implementation was complete
- **Issue:** `handleJobCompletion` already carried a pre-existing two-line stacked `//nolint:unparam` / `//nolint:gocyclo` directive pair (matching the file's own established precedent for this "flat state machine of mutually-exclusive completion arms" shape, shared by the sibling milestone/phase/plan/project controllers). This plan's three-way success-branch split plus the trailing verifier-dispatch call pushed the function's cyclomatic complexity to 34 (> the 30 threshold) — and, empirically, the two-line stacked directive form did NOT suppress the resulting `gocyclo` finding on this specific `golangci-lint` build (`v2.11.4-custom-gcl-...`), even though a synthetic reproduction of the identical two-line-stacked-nolint-above-func shape suppressed cleanly, and 11 of 12 other package-wide gocyclo-over-30 functions (each using a SINGLE `//nolint:gocyclo` line) were suppressed correctly in the same run. Root cause not fully isolated (a `nolintlint`-adjacent quirk specific to two consecutive distinct `//nolint:X` directive lines in this custom-gcl build is the leading hypothesis, but not conclusively proven).
- **Fix:** Merged the two directives onto one line — `//nolint:unparam,gocyclo // <combined reason>` — the standard comma-separated multi-linter nolint syntax. Confirmed via `--enable-only gocyclo,unparam` that this alone resolves the issue.
- **Files modified:** `internal/controller/task_controller.go` (comment-only change, no logic touched)
- **Verification:** `make lint` clean (0 issues); `go test ./internal/controller/... -count=1` unaffected (comment-only change).
- **Committed in:** `ae6e9f76` (Task 2 commit)

---

**Total deviations:** 2 auto-fixed (1 Rule 3 — a necessary schema-adjacent file outside the declared scope; 1 Rule 1 — a genuine lint-suppression bug this plan's own complexity growth surfaced).
**Impact on plan:** No scope creep — both fixes are narrowly required for the plan's own stated Verifying sub-state to exist and for `make lint`'s own verification requirement to pass.

## Issues Encountered

**The plan's literal `<verify>`/`<verification>` commands (`go test ./internal/controller/... -run 'VerifyDispatch|VerifierDispatch' -count=1` / `...-run 'VerifierInFlight'...`) are partially vacuous — the systemic finding documented in 51-01-SUMMARY.md and 51-03-SUMMARY.md recurs here.** `internal/controller`'s sole top-level Ginkgo entry point is `TestControllers`; a `-run` regexp that doesn't match that literal name runs zero specs regardless of whether matching `Describe`/`It` text exists, exiting 0 vacuously (confirmed empirically: `go test ./internal/controller/... -run 'VerifyDispatch|VerifierDispatch' -count=1` prints `[no tests to run]` in <1s). `TestVerifierInFlightCount` (Task 1, a plain `testing.T` function with no shared Ginkgo suite dependency) genuinely executes under `-run VerifierInFlight` — confirmed via `-v` RUN/PASS lines. The three Task-2 Ginkgo specs were verified genuinely via `go test ./internal/controller/... -run TestControllers -v -args --ginkgo.focus="VerifierDispatch"`, which reports `Will run 3 of 245 Specs` and `3 Passed | 0 Failed`. The full unfiltered suite (`go test ./internal/controller/... -count=1`, 245 + 3 = 248 specs) was also run and is green (~122s).

**A cache-sync race surfaced while writing the cap-hit test.** The first attempt at the cap-hit spec created `defaultVerifierConcurrencyCap` filler verifier Jobs via the direct `k8sClient` and immediately triggered the completion reconcile — `dispatchVerifier`'s cap check reads via the reconciler's cached client (`mgrClient`), and without an explicit `Eventually`-based cache-sync wait for the filler Jobs, the check raced the informer cache and under-counted (flaky false-negative: the verifier dispatched instead of deferring). Fixed by adding an `Eventually` poll on `mgrClient.List(...)` before triggering the completion reconcile, matching the existing `waitForCacheSync`/`stampBillingHalt` cache-sync-wait convention already established in this test package.

## User Setup Required

None - no external service configuration required. `TaskReconcilerDeps.VerifierImage` is not yet wired in `cmd/manager/main.go` (out of this plan's declared scope) — production dispatch will use an empty `SubagentImage` until a future plan (Phase 53 chart surface, or an earlier `main.go` wiring pass) sets it; this has zero effect on Plan 06/07/08's envtest-driven proof, which never pulls a real image.

## Next Phase Readiness

- The `Verifying` sub-state, `dispatchVerifier`, `buildVerifierEnvelopeIn`, and `checkVerifyingState`'s NotFound/still-running branches are all live and ready for Plan 07 to extend: Plan 07's own interfaces section explicitly calls this "the Verifying sub-state added in Plan 06" and wires its verdict-consumption logic onto `checkVerifyingState`'s terminal branch (currently a bare `return taskGateResult{shouldHalt: true, result: ctrl.Result{}}, nil` placeholder — search for the comment "BACKWARD half... is Plan 07" to find the exact insertion point).
- `handleJobCompletion`'s conditional `settleExecutorReservation` flag and the shared-key reservation handoff pattern are ready for Plan 07's own `Settle`/`Release` calls at verifier completion (the verifier's reservation, made in `dispatchVerifier`, is still outstanding under `task.UID` until Plan 07 settles or releases it — currently nothing does, by design, since Plan 06 doesn't consume the verdict yet).
- `pkg/dispatch.VerifyContext.Commands` is populated and ready for the verifier entrypoint's existing `_run_commands_out_of_band`/`_assemble_verdict` (Plan 02) to consume — no further Python-side plumbing is needed.
- No blockers. `defaultVerifierConcurrencyCap=2` is unvalidated against a live run — flagged for Plan 08's kind concurrent-dispatch test to pin/re-tune, same as the executor/planner/verifier caps-floor precedent.

---
*Phase: 51-the-task-loop*
*Completed: 2026-07-19*

## Self-Check: PASSED

All 6 created/modified files confirmed present on disk; both task commits (`316eaefc`, `ae6e9f76`) confirmed in `git log`.
