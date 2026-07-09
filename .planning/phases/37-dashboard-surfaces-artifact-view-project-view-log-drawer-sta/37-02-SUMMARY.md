---
phase: 37-dashboard-surfaces-artifact-view-project-view-log-drawer-sta
plan: 02
subsystem: infra
tags: [go, git, go-git, tide-push, artifacts, staging, gitleaks]

# Dependency graph
requires:
  - phase: 34-push-internals
    provides: "single-flight push, verify-in-push, lastPushedSHA, clean-tree skip"
  - phase: 36-agent-identity
    provides: "agentSignature() env-sourced TIDE agent author identity"
provides:
  - "--stage-envelopes flag on cmd/tide-push (CSV of <uid>:<destPrefix>)"
  - "parseStageEnvelopes: fail-closed parser with traversal containment"
  - "stageEnvelopeArtifacts: glob+copy+stage of planning *.md + children/*.json into .tide/planning/<destPrefix>/"
  - "Destination layout contract .tide/planning/<kind>/<name>/{*.md,children/*.json} for plans 37-06/37-07/37-09"
  - "NoErrAlreadyUpToDate treated as push success (idempotent cumulative restage)"
affects: [37-06, 37-07, 37-09, dashboard-artifact-view, DASH-02]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "src->dst envelope staging (UID-keyed PVC dir -> human-readable run-branch path)"
    - "Two-glob allowlist for D-04 exclusion (only *.md + children/*.json read; out.json/in.json never staged)"
    - "Fail-closed path validation: pattern + filepath.Clean containment under .tide/planning/"

key-files:
  created: []
  modified:
    - cmd/tide-push/main.go
    - cmd/tide-push/main_test.go

key-decisions:
  - "Store raw --stage-envelopes on pushConfig and parse inside runPush (not main) so the loud-failure path is reachable and testable through run() before any git op"
  - "Require >=1 *.md per envelope (planner-completed level always emits planning markdown); children/*.json optional"
  - "Treat go-git NoErrAlreadyUpToDate as push success in tide-push (mirrors pkg/git Fetch) so byte-identical cumulative restage is idempotent"
  - "Did NOT mark DASH-02 requirement complete — this is the write-half only; controller trigger (37-06) and e2e lock (37-09) still pending"

patterns-established:
  - "Envelope staging rides the existing commit/gitleaks-scan/force-with-lease/clean-tree pipeline unchanged — zero new commit sites"
  - "Typed envelope reason artifact-stage-failed + nonzero exit for every stage-envelopes failure path (D-03 loud failure)"

requirements-completed: []  # DASH-02 write-half implemented but requirement NOT complete (see decisions)

coverage:
  - id: D1
    description: "--stage-envelopes flag parses <uid>:<destPrefix> CSV with fail-closed validation (first-colon split, non-empty UID/destPrefix, pattern check, traversal containment under .tide/planning/)"
    requirement: "DASH-02"
    verification:
      - kind: unit
        ref: "cmd/tide-push/main_test.go#TestParseStageEnvelopes"
        status: pass
      - kind: integration
        ref: "cmd/tide-push/main_test.go#TestStageEnvelopesInvalidValueFailsLoud"
        status: pass
    human_judgment: false
  - id: D2
    description: "Envelope planning *.md + children/*.json stage to .tide/planning/<destPrefix>/ with byte fidelity and D-04 exclusion (out.json/in.json never staged), riding the existing commit/scan/push pipeline"
    requirement: "DASH-02"
    verification:
      - kind: integration
        ref: "cmd/tide-push/main_test.go#TestStageEnvelopesHappyPath"
        status: pass
      - kind: integration
        ref: "cmd/tide-push/main_test.go#TestStageEnvelopesByteFidelity"
        status: pass
    human_judgment: false
  - id: D3
    description: "All stage-envelopes failure paths are loud (missing dir / no *.md -> reason artifact-stage-failed, nonzero exit, nothing pushed) and cumulative restage is idempotent via the clean-tree skip"
    requirement: "DASH-02"
    verification:
      - kind: integration
        ref: "cmd/tide-push/main_test.go#TestStageEnvelopesMissingDirFailsLoud"
        status: pass
      - kind: integration
        ref: "cmd/tide-push/main_test.go#TestStageEnvelopesIdempotentRestage"
        status: pass
    human_judgment: false

# Metrics
duration: 25min
completed: 2026-07-08
status: complete
---

# Phase 37 Plan 02: tide-push --stage-envelopes Summary

**tide-push gains a src->dst `--stage-envelopes` flag that copies each envelope's planning `*.md` + `children/*.json` into human-readable `.tide/planning/<kind>/<name>/` on the run branch, riding the existing commit/gitleaks/force-with-lease pipeline with byte fidelity, D-04 exclusion, loud failure, and idempotent restage.**

## Performance

- **Duration:** ~25 min
- **Started:** 2026-07-08T04:47:00Z (approx)
- **Completed:** 2026-07-08T09:09:15Z (commit e6a913c)
- **Tasks:** 2
- **Files modified:** 2

## Accomplishments
- `--stage-envelopes` flag + `EnvelopeStage{UID,DestPrefix}` type + `parseStageEnvelopes` with fail-closed validation (first-colon split, pattern `^[a-z0-9]([a-zA-Z0-9._/-]*[a-zA-Z0-9])?$`, and `filepath.Clean` containment under `.tide/planning/` — traversal rejected two ways, T-37-02-01).
- `stageEnvelopeArtifacts` globs `envelopes/<uid>/*.md` + `envelopes/<uid>/children/*.json` ONLY and copies into `.tide/planning/<destPrefix>/` (children/ preserved); `out.json`/`in.json` excluded by construction (D-04).
- Every failure path is loud: missing envelope dir, zero `*.md`, or any copy/stage error → `writePushEnvelope(..., "artifact-stage-failed")` + stderr + nonzero exit, nothing pushed (D-03).
- Byte fidelity holds through a >1 MiB `*.md` fixture — no truncation/size-cap anywhere in the pipeline (D-03).
- Cumulative restage is idempotent: byte-identical copies leave the tree clean → the existing clean-tree skip pushes HEAD, and `NoErrAlreadyUpToDate` is now treated as success so the no-op re-push exits 0 with the commit count unchanged.

## Task Commits

Each task was committed atomically (TDD RED→GREEN followed in-process; committed once per task to keep the tree buildable):

1. **Task 1: --stage-envelopes flag, parsing, and validation** - `407ccc7` (feat)
2. **Task 2: envelope staging step — glob, copy, stage, loud failure, idempotence** - `e6a913c` (feat)

## Files Created/Modified
- `cmd/tide-push/main.go` — `StageEnvelopes` field on pushConfig; `EnvelopeStage` type; `destPrefixPattern`; `parseStageEnvelopes`; `--stage-envelopes` flag + doc; early parse in runPush (before any git op); integration-only shortcut now skips only when no envelopes queued; `stageEnvelopeArtifacts` helper; `NoErrAlreadyUpToDate` → push success.
- `cmd/tide-push/main_test.go` — `TestParseStageEnvelopes` (happy/empty/10 rejections incl. traversal), `TestStageEnvelopesInvalidValueFailsLoud`, `TestStageEnvelopesHappyPath` (full-listing D-04 exclusion), `TestStageEnvelopesByteFidelity` (>1 MiB), `TestStageEnvelopesMissingDirFailsLoud`, `TestStageEnvelopesIdempotentRestage`; helpers `writeEnvelopeFile`, `treePathsUnder`, `treeFileBytes`, `commitCount`.

## Decisions Made
- **Raw-string-on-config + parse-in-runPush.** The plan sketched `StageEnvelopes []EnvelopeStage` populated in `main()`, but Task 1 Test 3 ("rejected value exits with `artifact-stage-failed` before any git op") must be reachable through the testable `run()` entry point, and `main()` is not unit-testable. Storing the raw CSV on `pushConfig` and parsing at the top of `runPush` satisfies the test, guarantees "before any git op," and lets format-validation and staging failures share the one typed reason.
- **Require at least one `*.md` per envelope.** The plan states a planner-completed level MUST have at least one `*.md`; the empty-`*.md` set is a loud failure. `children/*.json` is optional (leaf levels may have none).
- **DASH-02 left Pending.** This plan is the write-half only. Per the plan, 37-06 adds the controller trigger and 37-09 the e2e lock. Marking DASH-02 complete now would be a false claim, so REQUIREMENTS.md is unchanged.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] Idempotent clean-tree re-push failed with "already up-to-date"**
- **Found during:** Task 2 (`TestStageEnvelopesIdempotentRestage`)
- **Issue:** The plan's success criterion "cumulative restaging is idempotent via the existing clean-tree skip" was not actually achievable: `pkggit.Push` does NOT swallow go-git's `NoErrAlreadyUpToDate` (unlike `pkggit.Fetch`, which does). On the second byte-identical push the tree is clean and HEAD already exists on the remote at the leased SHA, so the force-with-lease push is a no-op that go-git reports as `NoErrAlreadyUpToDate` → `classifyPushError` mapped it to `push-failed` (exit 1). Idempotence was broken.
- **Fix:** In `runPush`, after `pkggit.Push` returns an error, check `errors.Is(err, gogit.NoErrAlreadyUpToDate)` and treat it as success (write success envelope, exit 0). Mirrors the existing `pkg/git/fetch.go` precedent that swallows the same sentinel. Kept within frontmatter file scope (`cmd/tide-push/main.go`) — the wrapped `%w` error preserves the sentinel chain — rather than editing shared `pkg/git/push.go`.
- **Files modified:** cmd/tide-push/main.go
- **Verification:** `TestStageEnvelopesIdempotentRestage` passes (second push takes clean-tree path, commit count unchanged); full `go test ./cmd/tide-push/` green including all pre-existing push tests.
- **Committed in:** e6a913c (Task 2 commit)

---

**Total deviations:** 1 auto-fixed (1 bug)
**Impact on plan:** The fix is necessary to satisfy the plan's own idempotence success criterion; it is a single-caller behavior correction with a direct sibling-function precedent and no scope creep. Zero new push/commit code paths were added — staging reuses the existing commit, gitleaks scan, lease, and clean-tree machinery.

## Issues Encountered
- The pattern `^[a-z0-9]([a-zA-Z0-9._/-]*[a-zA-Z0-9])?$` alone permits interior traversal like `milestone/../../etc` (interior `.` and `/` are legal for real prefixes). Handled as designed by the second `filepath.Clean` containment gate — verified by `TestParseStageEnvelopes/rejects_destPrefix_nested_traversal`.

## Threat Flags

None — no new security-relevant surface beyond the plan's threat model. destPrefix→worktree-path traversal (T-37-02-01) is mitigated by the two-gate validation (pattern + containment), unit-tested; secret-shaped strings in artifacts remain covered by the existing gitleaks `ScanDiff` because staging rides the same single commit (T-37-02-02).

## Self-Check

- `cmd/tide-push/main.go` — FOUND (modified)
- `cmd/tide-push/main_test.go` — FOUND (modified)
- Commit `407ccc7` — FOUND
- Commit `e6a913c` — FOUND
- `go build ./...` — OK
- `go vet ./cmd/tide-push/` — OK
- `gofmt -l` — clean
- `go test ./cmd/tide-push/` — PASS (full package, incl. all pre-existing tests)
- Acceptance greps: `grep -c 'stage-envelopes'`=18 (>=2), `grep -c 'artifact-stage-failed'`=12 (>=1)

## Self-Check: PASSED

## Next Phase Readiness
- Destination layout contract `.tide/planning/<kind>/<name>/{*.md,children/*.json}` is live and ready for 37-06 (`PushOptions.StageEnvelopes` + controller trigger) and 37-09 (e2e kind-suite lock).
- DASH-02 remains Pending until the controller trigger (37-06) and end-to-end truth (37-09) land.

---
*Phase: 37-dashboard-surfaces-artifact-view-project-view-log-drawer-sta*
*Completed: 2026-07-08*
