---
status: fixing
trigger: "GH Action failures on tag v1.0.0-rc.2 (fabd0c0): Tests 27312281364, dry-run 27312281348, Lint 27312281333 all failed; release (helmify-verify) succeeded. Goal: fix on main, re-tag v1.0.0-rc.3, confirm Tests+dry-run+Lint green."
created: 2026-06-10
updated: 2026-06-10
---

## Symptoms

- **Tests (27312281364)**: `make test-int-fast` Layer A envtest — 14/36 specs FAIL. 13 share one root: `Task ... spec.promptPath: Invalid value: "" ... at least 1 chars long` (422). 1 (boundary_push Phase spec) is separate.
- **Lint (27312281333)**: golangci-lint — 25-26 real offenses (gocyclo 5, lll 10, modernize 4, goconst 1-2, unused 3, unconvert 1, unparam 1).
- **dry-run (27312281348)**: docker CLI fix (fabd0c0) WORKED. NEW failure: unauthenticated `git clone https://github.com/jsquirrelz/tide` inside the DinD container (private repo) → "could not read Username".
- Same Tests/Lint/ci failures on main pushes. nightly-integration green June 5–10 is MISLEADING: main was last pushed June 3; all June 8–10 work was batch-pushed June 10, so nightlies ran against pre-breakage main.

## Current Focus

hypothesis: CONFIRMED (three independent root causes, see Resolution)
next_action: verify `make test` + `make test-int-fast` green locally, then commit per-fix, push main, tag v1.0.0-rc.3

## Evidence

- 2026-06-10: 13 envtest failures all 422 promptPath — commit 1f8fc86 (June 8) made Task.spec.promptPath required (MinLength=1); envtest fixtures (admission ×8, gates, indegree, parent_unresolved) never set it.
- 2026-06-10: boundary_push Phase spec fails locally too (35/36 after fixture fix, run1 log /tmp/rc3-envtest-run1.log): commit 5c5c3e5 (09-08 Defect B) gates succession on out.ChildCount; test stubs EnvelopeOut without ChildCount → "boundary push skipped: planner authored no Plan children (leaf)" → patchPhaseSucceeded without push Job.
- 2026-06-10: `make lint` locally reproduces 25 offenses → fixed (see files_changed); `make lint` now "0 issues".
- 2026-06-10: dry-run clone: workflow checkout is shallow+detached at tag; git refuses local clone from shallow repo → fetch-depth: 0 + pinned detach checkout; CLONE_PIN_OK simulated locally.

## Eliminated

- hypothesis: dry-run still failing on missing docker CLI — REFUTED: kind cluster creates fine post-fabd0c0.
- hypothesis: Tests failures are product regressions in controllers — REFUTED for 13/14 (fixture-vs-CRD-contract drift); the 14th is also a test-fixture contract drift (ChildCount), not product (live medium DoD proved boundary push works).

## Resolution

root_cause: |
  (1) Tests/ci: 1f8fc86 added required promptPath (MinLength=1) to Task CRD; envtest fixtures never updated → 422 on every direct Task create. Plus boundary_push Phase spec stubbed EnvelopeOut without ChildCount after 5c5c3e5 made succession ChildCount-gated → leaf path skips boundary push.
  (2) Lint: June 8–10 commits accumulated 25 offenses; Lint workflow never ran on them until the batch push.
  (3) dry-run: private repo; container cloned https://github.com unauthenticated. Also latent: shallow/detached runner checkout can't be local-cloned.
fix: |
  (1) PromptPath stamped in all 11 envtest TaskSpec fixtures; boundary_push Phase spec sets ChildCount: 1.
  (2) All lint offenses fixed: deleted dead code (childKindAllowlist, childrenAlreadyMaterialized, waveIntegJobCompletionTime), slices.Contains ×3, any ×1, unconvert, unparam (_ ctx), envelopesDirName const, lll wraps in cmd/*, extracted spawnReporterIfNeeded (milestone 34→<30, phase 32→<30) + reconcileWaveBoundary (plan 36→<30), gocyclo added to _test.go exclusions.
  (3) dry-run-v1.sh mounts REPO_ROOT ro at /host-repo, DRY_RUN_REPO_URL defaults to /host-repo, safe.directory + pinned detach checkout; dry-run.yaml checkout fetch-depth: 0.
verification: make lint = "0 issues" (observed). make test + make test-int-fast pending (run2).
files_changed: test/integration/envtest/{admission,gates,indegree,parent_unresolved,boundary_push,reporter_materialize}_test.go, internal/controller/{dispatch_helpers,milestone_controller,phase_controller,plan_controller,task_controller,plan_wave_integration_test}.go, internal/harness/{envelope_io,outputs}.go, internal/subagent/anthropic/{subagent.go,subagent_test.go}, cmd/{claude-subagent,tide-push,tide-reporter}/main.go, cmd/manager/wave_dispatcher_wiring_test.go, .golangci.yml, hack/scripts/dry-run-v1.sh, .github/workflows/dry-run.yaml
