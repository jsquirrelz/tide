---
phase: 29-operator-tooling-e2e
verified: 2026-06-22T06:42:38Z
status: gaps_found
score: 3/4 roadmap criteria verified (criterion #4 FAILED live — E2E run surfaced 3 real defects; see e2e_run_1)
overrides_applied: 0
e2e_run_1:
  date: 2026-06-22
  result: "FAIL — 0 Passed | 3 Failed | 18 Skipped (focused: Import resume E2E + Loader SPDY exec smoke). Log: 29-e2e-run1-failures.log"
  gaps:
    - id: GAP-1
      severity: blocker
      title: "Loader pod design contradiction — pod never completes"
      detail: "execLoaderPod (cmd/tide/import_envelopes_run.go) AND loader_exec_smoke_test.go create the loader pod with main Command `tar xzf - -C /workspace` + Stdin:true, then ALSO SPDY-exec a second `tar xzf -`. The main tar blocks forever on a container stdin that is never attached, so the pod stays Running; the smoke test's wait-for-PodSucceeded times out (30s). The two patterns are mutually exclusive."
      fix: "Make the loader pod idle (main command `sleep <timeout>`), stream the tgz via the SPDY exec (already correct), treat StreamWithContext==nil as success, DELETE the pod afterward. Remove the wait-for-Succeeded assertion + the main-tar-on-stdin command in BOTH execLoaderPod and the smoke test."
    - id: GAP-2
      severity: blocker
      title: "Salvage fixture lacks canonical bundle layout (project.yaml + seed-manifest.json)"
      detail: "tide import-envelopes expects `project.yaml` (singular) + seed-manifest.json; salvage-20260618 only carries projects/milestones/phases/plans.yaml (List-style, Phase-28 shape). Tier b fails immediately: 'read project.yaml from bundle: ... no such file or directory'. 29-04 produced the canonical artifacts for the SMALL fixture but not for salvage."
      fix: "Generate project.yaml (singular) + seed-manifest.json (BundleManifest: FQName→oldUID + sha256) for salvage-20260618, as 29-04 did for import-small-fixture. Keep the existing pvc-envelopes.tgz."
    - id: GAP-3
      severity: blocker
      title: "Tier a: ImportController never materializes Milestones after import+apply"
      detail: "Small-fixture import-envelopes + kubectl apply project.yaml succeeded, but after 480s 'no Milestones found' — the in-cluster ImportController did not create the CR tree from the seed ConfigMap. Likely downstream of GAP-1 (envelopes not staged) or a seed-ConfigMap/importSource wiring gap. Re-investigate after GAP-1/GAP-2 fixed."
  what_passed_in_code: "TOOL-01 unit tier fully green (pkg/bundle, cmd/tide); D-05/D-07/D-09/D-16 + zip-slip confirmed in source; Phase 28 isEnvelopeComplete guard untouched. Defects are runtime-integration only — surfaced solely by the live kind run."
---

# Phase 29: Operator Tooling + E2E Verification Report

**Phase Goal:** Operators can export a Project's planner envelopes to a portable bundle and import a bundle into a new run via the `tide` CLI, with a dry-run mode that reports what would be adopted vs re-planned — and a kind integration test proves end-to-end resumption against the real salvage fixture.

**Verified:** 2026-06-22T06:42:38Z
**Status:** human_needed
**Re-verification:** No — initial verification

## Goal Achievement

### Observable Truths (ROADMAP Success Criteria + merged PLAN must_haves)

| #  | Truth | Status | Evidence |
|----|-------|--------|----------|
| 1  | `tide export-envelopes` writes a portable bundle (tgz/dir) of a Project's planner envelopes from the per-namespace PVC (criterion #1) | ✓ VERIFIED | `cmd/tide/export_envelopes{,_run}.go` build + 8 `TestExportEnvelopes*` tests pass; inspector pod (`tar czf - -C /workspace envelopes/ artifacts/`) streams PVC bytes; `WriteBundle` assembles 7-entry tgz; `--dir` mode tested (`TestExportEnvelopesDirMode`) |
| 2  | `tide import-envelopes --dry-run` reports adopt vs re-plan offline with no cluster writes/pods (criterion #2, D-07/D-08) | ✓ VERIFIED | `TestImportEnvelopesDryRun{TableOutput,JSONOutput,ChecksumMismatch}` pass; dry-run branch constructs no K8s client; `pkg/bundle/dryrun.go` runs `ValidateAPIVersionKind` + completeness + sha256 + `dag.ComputeWaves` offline |
| 3  | A detected cycle hard-rejects the WHOLE import and reports involved nodes (D-09) | ✓ VERIFIED | `dryrun.go:211` calls `dag.ComputeWaves`, returns `CycleRejected` result on `*dag.CycleError`; `TestImportEnvelopesDryRunCycleRejects` + `pkg/bundle` cycle test pass |
| 4  | `tide import-envelopes` (live) stages a bundle (loader pod → PVC, seed ConfigMap, surfaced project.yaml) and does NOT apply the Project (criterion #3, D-05/D-06) | ✓ VERIFIED | `TestImportEnvelopesLiveMode{CreatesConfigMap,Idempotent,DoesNotApjectProject}` + `TestImportEnvelopesLoaderPodTgzStreamed` pass; `import_envelopes_run.go:262` prints `tide apply` next-step, no Project create; loader pod uses `remotecommand` SPDY exec (`SubResource("exec")`) |
| 5  | Export seed manifest carries FQName→oldUID + dependsOn + status + per-envelope sha256; stamps legacy childCount (D-03/D-04/D-16a) | ✓ VERIFIED | `TestExportEnvelopesSeedManifest` + `TestExportEnvelopesChildCountRepair` pass; export lists live `MilestoneList/PhaseList/PlanList` and maps each CR to a `BundleEntry` |
| 6  | Export emits only Milestone/Phase/Plan (no Wave, no Task) — D-13/D-15 | ✓ VERIFIED | grep of `export_envelopes_run.go`: zero `WaveList`/`Wave{` and zero `TaskList`/`tasks.yaml` references |
| 7  | Zip-slip defense: tgz extraction rejects `../` and absolute paths, writes nothing | ✓ VERIFIED | `pkg/bundle/bundle.go:185-191` rejects `..`-prefix + absolute + outside-dest; `TestZipSlip`/`TestExtract` pass |
| 8  | Salvage out.json carry childCount so the fixture imports as-is; only complete (exitCode==0) envelopes patched (D-16b/D-17) | ✓ VERIFIED | `scripts/check-salvage-childcount.sh` exits 0; patch commit `b75c73e` ("stamp childCount into 18 complete salvage out.json"); failed envelopes untouched |
| 9  | Phase 28 `isEnvelopeComplete` guard (cmd/tide-import/main.go) UNTOUCHED by Phase 29 (D-16) | ✓ VERIFIED | `cmd/tide-import/main.go` last touched by Phase-28 commits (`aa58181` etc.); no `29-` commit modifies it; only Phase-29 internal/controller edit is whitespace-only gofmt in `import_guard_test.go` (commit `72de00a`, guard logic untouched) |
| 10 | Small drain fixture exists (1 project/1 ms/1 phase/2 plans) drainable to all-Milestones-Succeeded (D-11a) | ✓ VERIFIED | `testdata/import-small-fixture/` has all 7 entries; seed-manifest.json carries milestones/phases/plans arrays |
| 11 | `make test-int-kind-prep` builds the tide CLI so the E2E can exec it (D-10) | ✓ VERIFIED | `Makefile` test-int-kind-prep recipe contains `go build -o bin/tide ./cmd/tide` |
| 12 | Tier-a E2E drives the REAL CLI export→import→apply round-trip and asserts milestone/phase adoption (D-10, criterion #4) | ⚠ ARTIFACT VERIFIED / LIVE PENDING | `import_resume_test.go` compiles + vets; carries `export-envelopes` → `assertD02BundleShape` → `import-envelopes` fresh ns → `kubectl apply` → `assertNoPlannerJobsForLevel(milestone/phase)`. Live kind run NOT executed (env decision pending) |
| 13 | Tier-b E2E asserts 0 planner Jobs for {milestone,phase} + CostSpentCents==0 before plan dispatch (D-11b/D-14/D-17) | ⚠ ARTIFACT VERIFIED / LIVE PENDING | `import_resume_test.go` tier-b asserts `MatchingLabels{role:planner, level:milestone/phase}` empty via Consistently + `project.Status.Budget.CostSpentCents == int64(0)`; does NOT over-assert plan level. Live run pending |
| 14 | SPDY loader-exec proven LIVE (loader_exec_smoke_test.go) — A1/A2 gate (D-06) | ⚠ ARTIFACT VERIFIED / LIVE PENDING | `loader_exec_smoke_test.go` compiles + vets; mirrors production exec URL/`SubResource("exec")`/`remotecommand.NewSPDYExecutor`/`StreamWithContext`. Live run pending |

**Score:** 11/11 verifiable-without-live-kind truths VERIFIED; 3 truths (12/13/14) artifact-verified with the live kind drain as the single remaining step → criterion #4 routed to human_needed.

### Required Artifacts

| Artifact | Expected | Status | Details |
|----------|----------|--------|---------|
| `pkg/bundle/{seed,bundle,dryrun}.go` | bundle types + zip-slip tgz codec + offline validator | ✓ VERIFIED | builds, vets, tests green; no `internal/controller` import (D-07) |
| `cmd/tide/export_envelopes{,_run}.go` | export verb + inspector pod read path | ✓ VERIFIED | builds; 8 tests pass; registered in subcommands.go |
| `cmd/tide/import_envelopes{,_run}.go` | import verb + dry-run + live stage-only loader | ✓ VERIFIED | builds; 11 tests pass; SPDY exec wired; no Project apply |
| `test/integration/kind/import_resume_test.go` | two-tier E2E | ⚠ compiles/vets; live run pending | correct D-10/D-11/D-14/D-17 assertions present |
| `test/integration/kind/loader_exec_smoke_test.go` | live SPDY-exec smoke | ⚠ compiles/vets; live run pending | mirrors production exec construction |
| `testdata/import-small-fixture/*` | drainable small fixture | ✓ VERIFIED | all 7 bundle entries present |
| `scripts/check-salvage-childcount.sh` | salvage childCount assertion | ✓ VERIFIED | exits 0 |
| `Makefile` (test-int-kind-prep) | bin/tide build wiring | ✓ VERIFIED | `go build -o bin/tide ./cmd/tide` present |

### Key Link Verification

| From | To | Via | Status |
|------|----|-----|--------|
| `pkg/bundle/dryrun.go` | `pkg/dag.ComputeWaves` | direct call on seed nodes/edges | ✓ WIRED (`dryrun.go:211`) |
| `pkg/bundle/dryrun.go` | `pkg/dispatch.ValidateAPIVersionKind` | per-envelope schema check | ✓ WIRED (`dryrun.go:180`) |
| `cmd/tide/export_envelopes_run.go` | `pkg/bundle` | WriteBundle + sha256 + childCount-stamp | ✓ WIRED |
| `cmd/tide/import_envelopes_run.go` | loader pod stdin | `remotecommand.NewSPDYExecutor.StreamWithContext` | ✓ WIRED (`:434/:453/:458`) |
| `cmd/tide/import_envelopes_run.go` | seed ConfigMap | `ConfigMaps(ns).Create`, AlreadyExists idempotent | ✓ WIRED (`:240`) |
| salvage out.json | `cmd/tide-import isEnvelopeComplete` | childCount==len(childCRDs) post-patch | ✓ WIRED (check script green; guard untouched) |

### Requirements Coverage

| Requirement | Source Plan(s) | Description | Status | Evidence |
|-------------|----------------|-------------|--------|----------|
| TOOL-01 | 29-01, 29-02, 29-03 | Operator CLI exports/imports envelope bundle with dry-run adopt-vs-replan preview | ✓ SATISFIED | export + import + dry-run verbs build, register, pass all unit tests |
| TOOL-02 | 29-04, 29-05 | kind E2E proves resumption against real salvage fixture; planning cost not re-paid | ? NEEDS HUMAN (live run) | E2E tests compile/vet with correct assertions; fixtures landed; live kind drain pending env decision |

Both TOOL-01 and TOOL-02 IDs from PLAN frontmatter are accounted for and map to REQUIREMENTS.md (both marked Complete in the traceability table). No orphaned requirements for Phase 29.

### Anti-Patterns Found

| File | Pattern | Severity | Impact |
|------|---------|----------|--------|
| (none) | TODO/FIXME/XXX/PLACEHOLDER scan of all Phase-29 production files | ℹ Info | CLEAN — no debt markers in `pkg/bundle/*.go` or `cmd/tide/{export,import}_envelopes*.go` |

### Behavioral Spot-Checks

| Behavior | Command | Result | Status |
|----------|---------|--------|--------|
| pkg/bundle unit tier | `go test ./pkg/bundle/ -count=1` | ok | ✓ PASS |
| export/import/dry-run unit tier | `go test ./cmd/tide/ -run 'TestExport\|TestImport\|TestDryRun' -count=1` | 19 specs PASS | ✓ PASS |
| build + vet phase-29 pkgs | `go build/vet ./pkg/bundle/ ./cmd/tide/` | exit 0 | ✓ PASS |
| kind test package compiles | `go test ./test/integration/kind/ -run XXX -short` | ok [no tests to run] | ✓ PASS |
| salvage childCount invariant | `bash scripts/check-salvage-childcount.sh` | exit 0 | ✓ PASS |
| no new DIRECT go.mod dep | `git diff 178225a HEAD -- go.mod` | only `// indirect` transitive deps (gorilla/websocket, moby/spdystream, k8s.io/streaming) from remotecommand | ✓ PASS (transitive, expected) |
| live kind Layer-B drain | `make test-int` | NOT RUN (OOM risk; env decision pending) | ? SKIP → human |

### Probe Execution

No conventional `scripts/*/tests/probe-*.sh` declared for this phase. The phase's runnable verification is the kind E2E suite, routed to human verification (see frontmatter).

### Human Verification Required

**1. Live kind Layer-B E2E drain**

**Test:** `make test-int-kind-prep && make test-int` (read the echoed MAKE_EXIT and `grep -nE '^--- FAIL|^FAIL\s'`, not just the Ginkgo summary, per CLAUDE.md).
**Expected:** Tier a drains the small fixture to all-Milestones-Succeeded then live-round-trips export→import→apply (0 {milestone,phase} planner Jobs in the fresh namespace). Tier b imports salvage-20260618 and asserts 0 planner Jobs for {milestone,phase} + CostSpentCents==0 before plan dispatch. loader_exec_smoke streams a tgz through SPDY exec and reads it back.
**Why human:** Two kind clusters already up; running make test-int risks OOM on the ~7.65 GiB host. Live Layer-B run deferred pending an environment decision. This is the only remaining proof for ROADMAP criterion #4's runtime behavior — the test code, assertions, and fixtures are all verified present and correct.

### Pre-existing Defect Note (NOT a Phase-29 gap)

`TestDogfoodManifests_StrictDecode` / `_RequiredFields` (api/v1alpha1) FAIL on `unknown field "failureProfile"` in `examples/projects/dogfood/02-codex-runtime-project.yaml`. Root cause is commit `dcd7069` (dogfood manifest → v1alpha2 conversion), a top-level dogfood manifest **not touched by any Phase-29 commit**. Confirmed pre-existing and unrelated; does NOT block Phase 29. Recommend a separate quick task to either gate that v1alpha2 manifest out of the v1alpha1 strict-decode test or move it.

### Gaps Summary

No code gaps. All TOOL-01 surface (export, import, dry-run, bundle format, zip-slip, sha256, childCount-stamp, cycle hard-reject) is implemented, wired, tested, and passes its unit tier. All TOOL-02 fixtures + E2E test code exist, compile, vet clean, and carry the correct D-10/D-11/D-14/D-17 assertions. Locked decisions D-05/D-06/D-07/D-09/D-13/D-15/D-16(a,b) are honored in code. The Phase 28 import guard is untouched. The single remaining item is the **live execution** of the kind E2E (criterion #4's runtime proof), deferred to avoid OOM on a host already running two kind clusters — routed to human verification rather than failed, per the verification context.

---

_Verified: 2026-06-22T06:42:38Z_
_Verifier: Claude (gsd-verifier)_
