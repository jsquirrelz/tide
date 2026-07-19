---
phase: 51-the-task-loop
plan: 04
subsystem: dispatch
tags: [job-spec, verifier, k8s-jobs, volume-mounts, env-injection, podjob]

# Dependency graph
requires:
  - phase: 51-the-task-loop (plan 02)
    provides: verifier image reads env.verify.commands + TIDE_GATE_COMMAND (the sole gate source, tools.py)
  - phase: 48-langgraph-image-credproxy-tls-spike
    provides: BuildOptions.ReadOnly (D-08) — RO /workspace mount + verifier-scratch /scratch emptyDir, no git-write/push creds
provides:
  - "JobKindVerifier JobKind = \"verifier\" — grep-distinct from executor/planner, with its own verifierCapsFloorSeconds=900 (shorter than executor's 1200s per TASK-04)"
  - "VerifierJobName(taskUID, attempt) — deterministic tide-verifier-<uid>-<attempt> name, collision-free against JobName's tide-task-<uid>-<attempt> form"
  - "BuildJobSpec(Kind=JobKindVerifier) is caller-ready end-to-end: deterministic name via VerifierJobName + role=verifier label + verify caps floor, no further jobspec.go changes needed by the dispatch-site caller"
  - "BuildOptions.GateCommand — conditionally stamps TIDE_GATE_COMMAND on the subagent container (mirrors PricingOverridesJSON/TraceParent shape)"
  - "The RW envelopes/<uid>/ subPath mount — resolves the jobspec.go :199-204 forward-note: under ReadOnly, a second VolumeMount of the same project-workspace volume is mounted read-write at /workspace/envelopes/<uid>, matching where the manager already reads out.json from"
affects: [51-06, 51-07]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Second VolumeMount of the same PVC volume Name at a nested MountPath/SubPath to punch a narrow read-write hole through an otherwise-ReadOnly parent mount, without adding a new Volume or widening the RO contract"
    - "Kind-discriminated Job name/label switch: each JobKind case sets jobName + role label together (JobKindPlanner, JobKindVerifier, default=JobKindExecutor) so a caller only needs to set opts.Kind"

key-files:
  created: []
  modified:
    - internal/dispatch/podjob/caps.go
    - internal/dispatch/podjob/names.go
    - internal/dispatch/podjob/jobspec.go
    - internal/dispatch/podjob/caps_test.go
    - internal/dispatch/podjob/names_test.go
    - internal/dispatch/podjob/jobspec_test.go
    - internal/dispatch/podjob/jobspec_readonly_test.go

key-decisions:
  - "verifierCapsFloorSeconds=900 (Claude's Discretion, no live-run data yet): shorter than executorCapsFloorSeconds's 1200s per TASK-04's must_have, sized for a gate-command subprocess run + one LLM judge pass (no code-authoring tool loop) — re-raise per the existing executor/planner floor-raising precedent if a live run hits DeadlineExceeded"
  - "TIDE_GATE_COMMAND injection is gated on GateCommand != \"\" only (not additionally gated on opts.ReadOnly/Kind) — matches the action text's literal instruction to mirror the unconditional-except-non-empty PricingOverridesJSON/TraceParent shape; Plan 06 is the only caller expected to ever set it"
  - "BuildJobSpec's Kind switch (Job name + labels) gained an explicit JobKindVerifier case rather than leaving it to fall into the executor default — without this, a Kind=JobKindVerifier caller would silently get the executor's tide-task- name and role=executor label, breaking the 'caller-ready' contract the plan's objective explicitly promises Plan 06"
  - "The RW envelopes/<uid>/ mount is gated on opts.ReadOnly only (not on Kind==JobKindVerifier) — matches the ReadOnly field's existing scope (a general read-only-dispatch variant, not verifier-specific), consistent with how the /scratch mount and ReadOnlyRootFilesystem are already gated purely on ReadOnly"

patterns-established:
  - "Nested read-write mount punched through a ReadOnly parent mount via subPath scoping on the same PVC volume — reusable for any future read-only dispatch variant that needs one narrow writable subtree"

requirements-completed: [TASK-04, ESC-04]

# Metrics
duration: 5min
completed: 2026-07-19
---

# Phase 51 Plan 04: Verifier Job-Spec Surface (JobKindVerifier + TIDE_GATE_COMMAND + RW envelopes/ Mount) Summary

**Built the caller-ready verifier Job-spec surface — a dedicated `JobKindVerifier` (900s floor, `role=verifier` label, `VerifierJobName`), `TIDE_GATE_COMMAND` env injection from a new `BuildOptions.GateCommand` field, and the RW `envelopes/<uid>/` subPath mount that resolves the prior `ReadOnly`-variant's out.json write-back gap — so Plan 06 only has to set `opts.Kind = JobKindVerifier` to dispatch.**

## Performance

- **Duration:** ~5 min (08:59 -> 09:04 local)
- **Started:** 2026-07-19T08:59:13-04:00
- **Completed:** 2026-07-19T09:03:50-04:00
- **Tasks:** 2/2 completed
- **Files modified:** 7 (4 source, 3 test)

## Accomplishments
- `JobKindVerifier JobKind = "verifier"` added to the enum, grep-distinct from `executor`/`planner`; `verifierCapsFloorSeconds = 900` documented with the same floor-raising discipline as the existing executor (1200s) and planner (1800s) floors, and a drift-guard test pins it strictly shorter than the executor floor
- `DefaultCaps`'s floor-selection logic became a `switch` covering all three Kinds (previously an `if kind == JobKindPlanner`)
- `VerifierJobName(taskUID, attempt)` returns `tide-verifier-<uid>-<attempt>`, proven collision-free against `JobName`'s `tide-task-<uid>-<attempt>` form for the same UID/attempt
- `BuildJobSpec`'s Job-name/label switch gained an explicit `case JobKindVerifier`, so a caller that sets `Kind=JobKindVerifier` (with `Task` populated, same as executor dispatch) gets the deterministic verifier name + `role=verifier` label + verify caps floor with zero further jobspec.go changes
- `BuildOptions.GateCommand string` conditionally stamps `TIDE_GATE_COMMAND` on the subagent container when non-empty, mirroring the existing `PricingOverridesJSON`/`TraceParent` conditional-append shape; carries ONLY the canonical single `gateCommand`, not the full pass-criteria list (that travels via the envelope's `VerifyContext.Commands`, Plan 06's job)
- Resolved the `jobspec.go` `:199-204` forward-note: under `ReadOnly`, the subagent now also mounts a second `VolumeMount` of the same `project-workspace` PVC volume, scoped via `subPath` to `envelopes/<uid>/` and mounted read-write at `/workspace/envelopes/<uid>` — `out.json` is now writable even though the primary `/workspace` mount stays `ReadOnly`, and the writable path matches exactly where `doc.go` says the manager already reads `out.json` from
- No git-write/push credential or write tool was added anywhere — `TestBuildJobSpec_Verifier_NoGitCredsInAnyContainer` (Phase 48) still passes unmodified against both `ReadOnly` states
- Full `internal/dispatch/podjob` suite: green, non-vacuous (`go test ./internal/dispatch/podjob/... -count=1` — package has no shared Ginkgo entry point, so `-run` filters genuinely execute the matching top-level funcs, confirmed via `-v` RUN/PASS line counts at each TDD gate)
- `make lint` clean; `go vet ./internal/dispatch/podjob/...` clean

## Task Commits

Each task was committed atomically (TDD RED/GREEN split per `tdd="true"`):

1. **Task 1: JobKindVerifier + verify caps floor + VerifierJobName**
   - RED: `10167330` (test) — verifier-branch cases added to `TestDefaultCaps`/`TestDefaultCaps_NilCapsDeadlineMatch` + `TestVerifierJobName`/`TestVerifierJobName_DistinctFromJobName`; confirmed failing (`undefined: JobKindVerifier` / `verifierCapsFloorSeconds` / `VerifierJobName` — genuine build-failure RED)
   - GREEN: `a0af5181` (feat) — `JobKindVerifier` const + `verifierCapsFloorSeconds` + `DefaultCaps` switch arm + `VerifierJobName` implemented; all acceptance-criteria greps + `go test -run 'Caps|JobName|Verifier'` pass
2. **Task 2: TIDE_GATE_COMMAND env injection + RW envelopes/ subPath mount (RO preserved)**
   - RED: `afaf0390` (test) — `buildVerifierTestOptions` fixture + name/label/caps-floor/env/mount tests added; confirmed failing (`opts.GateCommand undefined` — genuine build-failure RED)
   - GREEN: `3cae70f0` (feat) — `BuildOptions.GateCommand` + conditional `TIDE_GATE_COMMAND` append + RW `envelopes/<uid>/` subPath mount + `BuildJobSpec`'s `JobKindVerifier` name/label case implemented; includes the Rule 1 fix below

## Files Created/Modified
- `internal/dispatch/podjob/caps.go` - `JobKindVerifier` const, `verifierCapsFloorSeconds`, `DefaultCaps` switch arm + updated doc comment
- `internal/dispatch/podjob/names.go` - `VerifierJobName(taskUID, attempt) string`
- `internal/dispatch/podjob/jobspec.go` - `BuildOptions.GateCommand`, `TIDE_GATE_COMMAND` conditional env append, RW `envelopes/<uid>/` subPath `VolumeMount`, `BuildJobSpec`'s `case JobKindVerifier` in the name/label switch, `ReadOnly` field doc comment updated from forward-note to resolved-note
- `internal/dispatch/podjob/caps_test.go` - verifier-branch `TestDefaultCaps` cases + verifier floor/drift-guard assertions in `TestDefaultCaps_NilCapsDeadlineMatch`
- `internal/dispatch/podjob/names_test.go` - `TestVerifierJobName`, `TestVerifierJobName_DistinctFromJobName`
- `internal/dispatch/podjob/jobspec_test.go` - `buildVerifierTestOptions` fixture + 7 new tests (name/label/caps-floor wiring, `TIDE_GATE_COMMAND` present/absent, RW envelopes mount present/absent)
- `internal/dispatch/podjob/jobspec_readonly_test.go` - Rule 1 fix to `TestBuildJobSpec_Verifier_WorkspaceMountReadOnly` (see Deviations)

## Decisions Made
- `verifierCapsFloorSeconds=900` picked at Claude's Discretion per the plan's explicit allowance — no live-run data exists yet to calibrate it precisely; documented with the same re-raise-if-DeadlineExceeded discipline the executor/planner floors already follow.
- `TIDE_GATE_COMMAND` injection is gated on `GateCommand != ""` alone, not additionally on `Kind`/`ReadOnly` — the action text explicitly said to mirror the unconditional-except-non-empty `PricingOverridesJSON`/`TraceParent` shape, and only Plan 06 is expected to ever populate the field.
- Added an explicit `case JobKindVerifier` to `BuildJobSpec`'s Job-name/label switch (not itemized verbatim in Task 2's action text, but required by the plan's objective: "Plan 06 is the caller... Build it caller-ready (the new BuildOptions field, the new kind/name/mount)"). Without this, a `Kind=JobKindVerifier` dispatch would silently fall into the executor `default:` branch and get the wrong name (`tide-task-...`) and `role=executor` label — breaking ESC-04's `role=verifier` selector contract before Plan 06 even runs.
- The RW `envelopes/<uid>/` mount is gated on `opts.ReadOnly` alone, matching how `/scratch` and `ReadOnlyRootFilesystem` are already scoped — keeps the mount tied to "is this dispatch structurally read-only", not to the `Kind` enum, so any future `ReadOnly` variant besides the verifier automatically gets a writable envelope path too.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] `TestBuildJobSpec_Verifier_WorkspaceMountReadOnly` (Phase 48) broken by the new RW envelopes mount sharing the same Volume Name**
- **Found during:** Task 2, after implementing the GREEN RW envelopes mount and running the full `Verifier|ReadOnly|JobSpec` filter
- **Issue:** The pre-existing test iterated `subagent.VolumeMounts` matching on `vm.Name == VolumeProjectWorkspace` alone to find "the" workspace mount and assert `ReadOnly==true`. Adding the new RW envelopes/`<uid>` mount (same `Name: VolumeProjectWorkspace`, different `MountPath`) gave the loop two matches; it landed on the RW one last and failed asserting `ReadOnly==true` against a mount that is read-write by design — a direct, mechanical consequence of this task's own change, not a pre-existing unrelated failure.
- **Fix:** Scoped the test's match condition to `vm.Name == VolumeProjectWorkspace && vm.MountPath == "/workspace"`, disambiguating the primary mount from the new nested one, with a comment pointing at the new `TestBuildJobSpec_Verifier_RWEnvelopesSubPathMount` test that covers the second mount.
- **Files modified:** `internal/dispatch/podjob/jobspec_readonly_test.go`
- **Verification:** `go test ./internal/dispatch/podjob/... -count=1` green (all 60+ top-level tests, including this one and the full readonly suite).
- **Committed in:** `3cae70f0` (Task 2 GREEN commit)

---

**Total deviations:** 1 auto-fixed (Rule 1 — a genuine test-assumption break directly caused by this task's own mount-topology change)
**Impact on plan:** The fix was necessary for the plan's own acceptance criterion (`go test ./internal/dispatch/podjob/... -run 'Verifier|ReadOnly|JobSpec' -count=1` exits 0, genuinely) to hold. No scope creep — the fix touches only the one pre-existing assertion this task's change directly invalidated.

## Issues Encountered

None beyond the deviation above. Confirmed the run was genuinely non-vacuous at every gate: each RED commit failed with a real `undefined: X` compile error (not a filter matching zero tests), and each GREEN's `-v` output showed the expected RUN/PASS lines for every named test, consistent with 51-01/51-02's documented finding that `internal/dispatch/podjob` (unlike `internal/controller`) has no shared Ginkgo entry point, so `go test -run <name>` filters here are always real.

## User Setup Required

None - no external service configuration required.

## Next Phase Readiness
- `JobKindVerifier` + `VerifierJobName` + `BuildOptions.GateCommand` + the RW `envelopes/<uid>/` mount are all live and caller-ready for Plan 06's dispatch site (`task_controller.go`'s verifier dispatch/consume sub-state-machine) — Plan 06 sets `opts.Kind = JobKindVerifier`, `opts.ReadOnly = true`, `opts.GateCommand = task.Spec.Verification.GateCommand`, and calls `BuildJobSpec`; no further `jobspec.go`/`caps.go`/`names.go` changes are needed for the Job-spec layer itself.
- `verifierInFlightCount` (Plan 06, per PATTERNS.md) can select on `tideproject.k8s/role=verifier` today.
- The RW envelopes mount's writable path (`/workspace/envelopes/<uid>`) is exactly where the manager's `FilesystemEnvelopeReader` already reads `out.json` from (per `doc.go`) — Plan 06 needs no new envelope-reader wiring, only to dispatch the Job.
- No blockers. `verifierCapsFloorSeconds`'s exact value (900) is unvalidated against a live run — flagged for re-tuning if a real verifier dispatch hits `DeadlineExceeded`, same as the executor/planner floors' own history.

---
*Phase: 51-the-task-loop*
*Completed: 2026-07-19*
