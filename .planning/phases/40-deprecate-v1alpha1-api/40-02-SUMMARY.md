---
phase: 40-deprecate-v1alpha1-api
plan: 02
subsystem: api
tags: [envelope-contract, api-versioning, dispatch, tdd]

# Dependency graph
requires:
  - phase: 40-deprecate-v1alpha1-api (plan 40-01, parallel wave)
    provides: api/v1alpha3 package (disjoint file set — no direct dependency, ran concurrently)
provides:
  - pkg/dispatch.APIVersionV1Alpha1 decoupled to "dispatch.tideproject.k8s/v1alpha1" (D-08)
  - old CRD-group string "tideproject.k8s/v1alpha1" provably rejected by ValidateAPIVersionKind
  - pkg/dispatch/doc.go rewritten with the kubeadm precedent; superseded v1beta1 hub/spoke plan removed
  - every envelope-context literal in the repo (production + test fixtures) migrated to the new group string
affects: [40-03 (owner-ref dual-accept removal / CRD-group cleanup), 40-04 (subagent.levels rename), migration docs]

# Tech tracking
tech-stack:
  added: []
  patterns: ["envelope contract group decoupled from CRD group (kubeadm-style own-subdomain group)"]

key-files:
  created: []
  modified:
    - pkg/dispatch/envelope.go
    - pkg/dispatch/doc.go
    - pkg/dispatch/envelope_test.go
    - cmd/tide-push/main.go
    - cmd/tide-eval/main.go
    - cmd/tide-import/main_test.go
    - pkg/bundle/seed_test.go
    - internal/eval/render_test.go
    - internal/dispatch/podjob/jobspec_test.go
    - test/integration/envtest/reporter_materialize_test.go
    - test/integration/envtest/leak_blocked_test.go
    - internal/controller/project_boundary_push_test.go
    - internal/controller/project_pushresult_test.go
    - internal/controller/plan_wave_integration_test.go

key-decisions:
  - "APIVersionV1Alpha1 value flips to dispatch.tideproject.k8s/v1alpha1; the constant NAME and version component (v1alpha1) are unchanged — pure group decoupling per D-08, no stability claim"
  - "cmd/tide-push/main.go keeps its independent hand-synced literal by design (documented no-pkg/dispatch-import); cmd/tide-eval/main.go now references pkgdispatch.APIVersionV1Alpha1 directly, closing the drift vector RESEARCH.md Pitfall 4 warned about"
  - "pushResultEnvelope / pushResultMirror fixtures (git-push-result JSON, Kind: PushResult) are envelope-context, not owner-ref-context — they mirror the same apiVersion discriminator cmd/tide-push emits, so they moved with the crank"
  - "test/e2e/{dashboard,gate_flow}_test.go inspected and found to carry only CRD-group YAML (apiVersion: tideproject.k8s/v1alpha1 for kind: Project/Milestone), not envelope literals — correctly out of this plan's scope (owned by 40-03/D-05); no edit made despite being listed in files_modified"

patterns-established:
  - "Envelope contract group lives at its own dispatch.tideproject.k8s subdomain, permanently decoupled from the CRD group tideproject.k8s (kubeadm precedent, documented in pkg/dispatch/doc.go)"

requirements-completed: [CRANK-02]

# Metrics
duration: 9min
completed: 2026-07-11
---

# Phase 40 Plan 02: Decouple Envelope Contract from CRD Group Summary

**Envelope contract `apiVersion` flipped from `tideproject.k8s/v1alpha1` to `dispatch.tideproject.k8s/v1alpha1` (D-08 kubeadm-style group decoupling), with the old CRD-group string now provably rejected and every envelope-context literal in the repo (13 files, ~20 sites) migrated in lockstep.**

## Performance

- **Duration:** ~9 min (RED commit 19:33:40 → final GREEN commit 19:42:27, local time -0400)
- **Started:** 2026-07-11T23:33:40Z (first commit)
- **Completed:** 2026-07-11T23:42:27Z (last commit)
- **Tasks:** 2 completed (Task 1 TDD: RED+GREEN; Task 2: single commit)
- **Files modified:** 14 (2 production, 12 test/fixture)

## Accomplishments
- `pkg/dispatch.APIVersionV1Alpha1` now carries `dispatch.tideproject.k8s/v1alpha1` — a CRD-group version crank (v1alpha1→v1alpha2→v1alpha3...) can no longer collide with or bump the subagent-image envelope contract.
- `ValidateAPIVersionKind` fail-closed behavior pinned under test: the old CRD-group string `tideproject.k8s/v1alpha1` is rejected as an unknown apiVersion (new `TestValidateAPIVersionKind_RejectsOldCRDGroupString`).
- `pkg/dispatch/doc.go` rewritten: cites the kubeadm precedent (`kubeadm.k8s.io` — a K8s-shaped config-file API group that is not itself a served resource) and deletes the superseded "future breaks ride a v1beta1 hub/spoke bump" sentence that would have collided with the CRD-group crank.
- The two independent literal copies (`cmd/tide-push/main.go` hand-synced constant, `cmd/tide-eval/main.go` previously-hardcoded literal) both moved; `tide-eval` now references `pkgdispatch.APIVersionV1Alpha1` directly, closing the drift vector permanently.
- Every remaining inline envelope-context literal across test fixtures updated to match, discriminating correctly against owner-ref/CRD-group fixtures (left untouched, owned by plan 40-03/D-05).

## Task Commits

Each task was committed atomically:

1. **Task 1: Flip the constant, rewrite the contract doc, extend envelope tests** (TDD):
   - RED: `712964c` — `test(40-02): add failing test for decoupled envelope contract group (D-08)`
   - GREEN: `dfb8d58` — `feat(40-02): decouple envelope contract group from CRD group (D-08)`
2. **Task 2: Fix the literal copies and every inline envelope test fixture** - `b78f8ca` — `feat(40-02): close the envelope-literal drift vector across every carrier (D-08)`

**Plan metadata:** this SUMMARY.md commit (below)

## Files Created/Modified
- `pkg/dispatch/envelope.go` - `APIVersionV1Alpha1` constant value + doc comment
- `pkg/dispatch/doc.go` - versioning paragraph rewritten with kubeadm precedent, v1beta1 plan removed
- `pkg/dispatch/envelope_test.go` - `TestEnvelopeIn_Constants` updated; new `TestValidateAPIVersionKind_RejectsOldCRDGroupString`
- `cmd/tide-push/main.go` - `envelopeAPIVersion` literal flipped + hand-sync comment added
- `cmd/tide-eval/main.go` - literal replaced with `pkgdispatch.APIVersionV1Alpha1`
- `cmd/tide-import/main_test.go` - 2 envelope JSON fixture strings
- `pkg/bundle/seed_test.go` - 4 envelope JSON fixture strings
- `internal/eval/render_test.go` - `baseEnvelope.APIVersion` now references the constant
- `internal/dispatch/podjob/jobspec_test.go` - 2 envelope JSON fixture strings
- `test/integration/envtest/reporter_materialize_test.go` - 1 envelope JSON fixture string
- `test/integration/envtest/leak_blocked_test.go` - 2 `pushResultMirror` struct literals
- `internal/controller/project_boundary_push_test.go` - 6 `pushResultEnvelope` struct literals
- `internal/controller/project_pushresult_test.go` - 3 `pushResultEnvelope` struct literals
- `internal/controller/plan_wave_integration_test.go` - 1 envelope JSON fixture string (Rule 3 addition, see Deviations)

## Decisions Made
- Kept `cmd/tide-push/main.go`'s literal independent by design (documented no-`pkg/dispatch`-import boundary) rather than adding an import, per the plan's explicit instruction; added a comment stating the hand-sync obligation.
- Treated `pushResultEnvelope`/`pushResultMirror` (git-push-result JSON, `Kind: "PushResult"`) as envelope-context rather than CRD-group-context: they carry the identical `apiVersion` discriminator field `cmd/tide-push` emits via `envelopeAPIVersion`, so they must track the same value even though they are a distinct Go type from `EnvelopeIn`/`EnvelopeOut`.
- Confirmed `test/e2e/{dashboard,gate_flow}_test.go` (listed in the plan's `files_modified` frontmatter) contain only CRD-group YAML apiVersions (`kind: Project`, `kind: Milestone`) — no envelope literal exists in either file. No edit made; this is correctly out of scope for the envelope-decoupling task and belongs to the CRD-group cleanup owned by plan 40-03.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking] Fixed doc.go rewrite introducing a fresh "v1beta1" mention that tripped the plan's own acceptance grep**
- **Found during:** Task 1 (doc.go rewrite)
- **Issue:** My first rewrite of the versioning paragraph referenced "a future dispatch.tideproject.k8s/v1beta1" as an illustrative example of the new bump path. The plan's acceptance criterion `grep -n 'v1beta1' pkg/dispatch/doc.go` returns 0 hits — my own new sentence violated it.
- **Fix:** Rephrased the sentence to describe bumping "the dispatch group's OWN version component" without naming a specific future version string.
- **Files modified:** pkg/dispatch/doc.go
- **Verification:** `grep -n 'v1beta1' pkg/dispatch/doc.go` returns 0 hits; `grep -n 'kubeadm' pkg/dispatch/doc.go` returns 1 hit.
- **Committed in:** dfb8d58 (Task 1 GREEN commit)

**2. [Rule 3 - Blocking] Migrated an envelope-context literal in a file not listed in the plan's `files_modified` frontmatter**
- **Found during:** Task 2 (repo-wide literal sweep)
- **Issue:** `internal/controller/plan_wave_integration_test.go:772` carries an envelope-context literal (a `pushResultEnvelope`-shaped JSON string with `"kind":"PushResult"`, the same class as the explicitly-in-scope `project_boundary_push_test.go`/`project_pushresult_test.go` fixtures) but the file is absent from the plan's `files_modified` list. Leaving it unmigrated would fail the plan's own Task 2 acceptance criterion ("zero envelope-context hits" repo-wide).
- **Fix:** Updated the literal from `tideproject.k8s/v1alpha1` to `dispatch.tideproject.k8s/v1alpha1`, matching every other `PushResult`-shaped fixture in the same package.
- **Files modified:** internal/controller/plan_wave_integration_test.go
- **Verification:** repo-wide `grep -rn '"tideproject.k8s/v1alpha1"' --include='*.go' .` now returns only owner-ref-context lines, the two CRD-group dual-accept sites, the `api/v1alpha1` package's own tests, and this plan's own Task-1 rejection test (which intentionally uses the old string as its rejected input).
- **Committed in:** b78f8ca (Task 2 commit)

**3. [Housekeeping] Removed a stray build artifact**
- **Found during:** Task 2 (build verification with `-tags eval`)
- **Issue:** `go build -tags eval ./cmd/tide-eval/...` (run to verify the literal replacement compiles — `tide-eval` is behind a build tag and not covered by `go build ./...`) produced an untracked `tide-eval` binary in the repo root.
- **Fix:** Deleted the binary before staging; never committed.
- **Files modified:** none (untracked artifact removed, not part of any commit)

---

**Total deviations:** 3 (2 Rule-3 blocking fixes necessary to satisfy the plan's own acceptance criteria; 1 housekeeping cleanup of a build byproduct)
**Impact on plan:** No scope creep — both Rule-3 fixes are within the same envelope-literal-migration concern the plan already scoped, just catching a self-authored gap (a fresh doc mention) and a frontmatter omission (a file the plan's own verification grep would have caught anyway).

## Issues Encountered
- `go build ./...` initially failed on `cmd/tide-demo-init/main.go` (`pattern all:fixture: no matching files found`) — this is a pre-existing, unrelated build-time embed prerequisite (`make demo-fixture` runs `go generate` to materialize the gitignored `fixture/` directory before `vet`/`lint`/`test`). Ran `make demo-fixture` (a standard build prerequisite, not a plan-scoped fix) and the build was clean afterward. No plan files were touched to resolve this.
- Layer A envtest suite (`test/integration/envtest`, 55 specs) is not part of `make test` (env-gated per plan's action note) but was run manually as an extra verification pass since three files in that directory were modified — all 55 specs passed.

## User Setup Required
None - no external service configuration required.

## Next Phase Readiness
- The envelope contract is fully decoupled from the CRD group; plan 40-03 (owner-ref dual-accept removal, CRD-group cleanup) and the parallel plan 40-01 (`api/v1alpha3` introduction) are unaffected by this plan's file set (verified disjoint — no touches to `api/` version packages).
- `examples/projects/dogfood/salvage-20260618/` envelope JSON fixtures were deliberately left untouched per the plan's boundary notes — owned by plan 40-03's coherent regeneration alongside the CRD schemaRevision bump.
- No blockers for downstream plans in this phase.

## Self-Check: PASSED

- FOUND: pkg/dispatch/envelope.go
- FOUND: pkg/dispatch/doc.go
- FOUND: .planning/phases/40-deprecate-v1alpha1-api/40-02-SUMMARY.md
- FOUND: commit 712964c (RED)
- FOUND: commit dfb8d58 (GREEN, Task 1)
- FOUND: commit b78f8ca (Task 2)

---
*Phase: 40-deprecate-v1alpha1-api*
*Completed: 2026-07-11*
