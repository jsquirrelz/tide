---
phase: 40-deprecate-v1alpha1-api
verified: 2026-07-12T03:10:36Z
status: passed
score: 7/7 must-haves verified
overrides_applied: 0
re_verification:
  previous_status: none
  note: "Initial verification — no prior VERIFICATION.md existed"
---

# Phase 40: Deprecate v1alpha1 API (Full Version-Lifecycle Turn) Verification Report

**Phase Goal:** One full API-version crank: introduce v1alpha3 as the sole served+storage version for all 6 CRDs — carrying the folded `subagent.levels` semantic rename plus user-approved batchable schema fixes — then remove v1alpha1 AND v1alpha2 entirely (Go packages, CRD blocks, chart copies, scheme registrations, stale comments). Reinstall-only migration; SchemaRevision guard generalized as the permanent crank mechanism; owner-ref dual-accepts dropped; deep docs/samples sweep; envelope contract decoupled to `dispatch.tideproject.k8s/v1alpha1`.
**Verified:** 2026-07-12T03:10:36Z
**Status:** passed
**Re-verification:** No — initial verification

## Goal Achievement

The ROADMAP `success_criteria` array is empty; the phase contract is the 7 CRANK requirement IDs in REQUIREMENTS.md (minted at plan time per 40-CONTEXT.md Claude's Discretion). Each is treated as a non-negotiable observable truth. Plan-frontmatter must-have truths (24 across 7 plans) were verified as supporting evidence beneath each requirement.

### Observable Truths

| #  | Truth (CRANK requirement) | Status | Evidence |
| -- | ------------------------- | ------ | -------- |
| 1  | CRANK-01: `api/v1alpha3` is the copy-and-reshape of v1alpha2 — SchemaRevision enum `v1alpha3`, dead `ProjectSpec.ModelSelection` dropped (D-10), storage markers moved, CRDs + tide-crds chart regenerate reproducibly | ✓ VERIFIED | `api/` contains only `v1alpha3/` (11 files). `groupversion_info.go:30` GroupVersion `Version: "v1alpha3"`. `project_types.go:377-378` `Enum=v1alpha3` on `SchemaRevision`. `schema_test.go:42-43` negative assertion fails if `ModelSelection` exists. All 6 `config/crd/bases/*.yaml` carry exactly one `name: v1alpha3` block, one `storage: true`, one `served: true`. All 6 `charts/tide-crds/templates/*.yaml` carry one v1alpha3 block, zero legacy. |
| 2  | CRANK-02: Envelope contract decoupled to `dispatch.tideproject.k8s/v1alpha1` (D-08); old CRD-group string rejected under test; tide-push/tide-eval drift closed; doc.go v1beta1 plan erased | ✓ VERIFIED | `envelope.go:30` `const APIVersionV1Alpha1 = "dispatch.tideproject.k8s/v1alpha1"`. `envelope_test.go:403` rejects `"tideproject.k8s/v1alpha1"` → `UnknownAPIVersionError`. `tide-eval/main.go:83` references `pkgdispatch.APIVersionV1Alpha1` (no literal). `tide-push/main.go:244` hand-synced literal (by-design, no pkg import). `doc.go` has 0 `v1beta1` refs, kubeadm precedent documented at line 33. |
| 3  | CRANK-03: Every consumer runs on v1alpha3 (zero v1alpha2 imports); SchemaRevision guard generalized to a two-constant crank mechanism (D-04); owner-ref dual-accepts dropped (D-05) | ✓ VERIFIED | `git grep "api/v1alpha[12]" -- '*.go'` → 0 files. Schemes: `cmd/manager/main.go:309`, `cmd/dashboard/main.go:103`, `cmd/tide/root_flags.go:57` all `tidev1alpha3.AddToScheme` only (exactly one in manager — duplicate fixed). `internal/webhook/v1alpha3/` sole webhook pkg. Guard: `project_controller.go:2238-2239` two constants `expectedSchemaRevision="v1alpha3"`, `migrationGuideDocPath="docs/migration/v1alpha2-to-v1alpha3.md"`. Owner-refs: `task_controller.go:1260` + `backend.go:408` accept only `tidev1alpha3.GroupVersion.String()`; zero `||` dual-accepts repo-wide. |
| 4  | CRANK-04: `subagent.levels` semantics renamed per DECIDED mapping (D-02/D-11) via override-key mapping with dispatch identity unchanged; resolved model logged at all 5 dispatch sites | ✓ VERIFIED | `dispatch_helpers.go:159-173` `levelOverrideKey` maps project→milestone, milestone→phase, phase→plan, plan→plan, task→task (verbatim DECIDED table). Used only at `:191` (ResolveProvider) + `:327` (resolveImage), not for `Level`. `EnvelopeIn.Level` values remain original 5-valued strings (project/milestone/phase/plan/task) unchanged. `"resolved subagent dispatch"` Info log at all 5 sites: project_controller.go:1749, milestone_controller.go:498, phase_controller.go:457, plan_controller.go:492, task_controller.go:787. |
| 5  | CRANK-05: `api/v1alpha1` + `api/v1alpha2` deleted; 6 single-version CRD manifests; `verify-no-aggregates` hardened to version-agnostic fail-closed glob (D-12 mandatory); PROJECT metadata fixed; dogfood strict-decode relocated | ✓ VERIFIED | Both packages absent (`ls api/v1alpha1 api/v1alpha2` → No such file). 6 CRDs each one version block. `Makefile verify-no-aggregates` uses `ls api/v1alpha*/*_types.go` + `if [ -z "$FILES" ]; then ... exit 1` empty-glob guard. `PROJECT` all 6 resources `version: v1alpha3`. `test/schema/dogfood_manifests_test.go` relocated with `UnmarshalStrict` + `"tideproject.k8s/v1alpha3": true` allowlist. |
| 6  | CRANK-06: Deep docs/samples accuracy pass (D-06) — migration chapter + levels-remap table; INSTALL/gates/git-hosts/project-authoring/README on v1alpha3; 12 samples renamed + kustomization lockstep; SECURITY.md/rbac.md conversion-webhook staleness fixed | ✓ VERIFIED | `docs/migration/v1alpha2-to-v1alpha3.md` 185 lines (>80 min) with old-vs-new levels-remap table (lines 27-30) + fallback note. 12 `tide_v1alpha3_*.yaml` samples, 0 legacy; `kustomization.yaml` lists 18 v1alpha3 entries, 0 legacy. INSTALL/gates/git-hosts/README carry v1alpha3, 0 legacy; project-authoring's 3 "legacy" hits are all migration-doc-path citations. `SECURITY.md:40` + `docs/rbac.md:209-230` describe the retired conversion webhook (past tense, Phase 23). |
| 7  | CRANK-07: CI-wired `verify-no-legacy-api-refs` gate (zero legacy refs outside sanctioned set), provably alive via seeded-failure check, and full `make test-int` green on the final tree | ✓ VERIFIED | `Makefile verify-no-legacy-api-refs` (git grep -nIE tracked-text semantics, commit 95d3802). Wired in `.github/workflows/ci.yaml:66` alongside verify-no-aggregates:63. Seeded-failure test (this verifier): appended `v1alpha1` to a tracked file → `make: *** [verify-no-legacy-api-refs] Error 1`; reverted → `OK`. `make test-int` MAKE_EXIT=0, Layer A 56/56 SUCCESS, Layer B kind 26/26 SUCCESS, zero `^--- FAIL` (log line 5922/5468/5918). |

**Score:** 7/7 truths verified

### Required Artifacts

| Artifact | Expected | Status | Details |
| -------- | -------- | ------ | ------- |
| `api/v1alpha3/groupversion_info.go` | GroupVersion tideproject.k8s/v1alpha3 + AddToScheme | ✓ VERIFIED | `Version: "v1alpha3"` at :30 |
| `api/v1alpha3/project_types.go` | ProjectSpec, SchemaRevision Enum=v1alpha3, no ModelSelection | ✓ VERIFIED | `Enum=v1alpha3` :377; ModelSelection absent (negative test) |
| `api/v1alpha3/schema_test.go` | reflect-based structural assertions (Wave 0) | ✓ VERIFIED | 107 lines, 3 test funcs, substantive |
| `config/crd/bases/*.yaml` (6) | single v1alpha3 version block each | ✓ VERIFIED | 1 name/storage/served each; only legacy hit = migration-doc path in a description |
| `charts/tide-crds/templates/*.yaml` (6) | regenerated v1alpha3 CRDs | ✓ VERIFIED | 1 v1alpha3 block each; project-crd legacy hit = migration-doc path |
| `pkg/dispatch/envelope.go` | decoupled constant | ✓ VERIFIED | `dispatch.tideproject.k8s/v1alpha1` :30 |
| `pkg/dispatch/envelope_test.go` | rejection test for old group string | ✓ VERIFIED | `UnknownAPIVersionError` on old string :403-412 |
| `cmd/tide-push/main.go` | hand-synced literal | ✓ VERIFIED | :244 |
| `internal/webhook/v1alpha3/project_webhook.go` | v1alpha3 admission webhooks | ✓ VERIFIED | `package v1alpha3`, `versions=v1alpha3` :43 |
| `internal/controller/project_controller.go` | generalized guard | ✓ VERIFIED | `expectedSchemaRevision`/`migrationGuideDocPath` :2238-2239 |
| `cmd/manager/main.go` | single v1alpha3 scheme reg | ✓ VERIFIED | exactly 1 `tidev1alpha3.AddToScheme` :309 |
| `internal/controller/dispatch_helpers.go` | levelOverrideKey mapping | ✓ VERIFIED | :159-173, DECIDED table verbatim |
| `test/integration/envtest/planner_dispatch_test.go` | per-level resolution assertions | ✓ VERIFIED | present in 40-04 files_modified; test-int 56/56 green |
| `test/schema/dogfood_manifests_test.go` | relocated strict-decode test | ✓ VERIFIED | `UnmarshalStrict` + v1alpha3 allowlist |
| `Makefile` (verify-no-aggregates / verify-no-legacy-api-refs) | hardened + new gate | ✓ VERIFIED | version-agnostic glob + fail-closed; new gate present |
| `PROJECT` | kubebuilder metadata → v1alpha3 | ✓ VERIFIED | all 6 resources `version: v1alpha3` |
| `docs/migration/v1alpha2-to-v1alpha3.md` | migration chapter | ✓ VERIFIED | 185 lines, levels-remap table |
| `config/samples/tide_v1alpha3_project.yaml` | root sample w/ schemaRevision | ✓ VERIFIED | `schemaRevision: v1alpha3` + apiVersion v1alpha3 |
| `.github/workflows/ci.yaml` | CI invocation of new gate | ✓ VERIFIED | :66 `make verify-no-legacy-api-refs` |

### Key Link Verification

| From | To | Via | Status | Details |
| ---- | -- | --- | ------ | ------- |
| `api/v1alpha3/*_types.go` | `config/crd/bases/*.yaml` | make manifests | ✓ WIRED | all 6 CRDs `name: v1alpha3` |
| `config/crd/bases/` | `charts/tide-crds/templates/` | make helm-crds | ✓ WIRED | all 6 chart copies v1alpha3 |
| `envelope.go APIVersionV1Alpha1` | subagent/tide-push/tide-eval | constant reference | ✓ WIRED | tide-eval refs constant; tide-push hand-synced literal (by design) |
| `cmd/manager/main.go` | `internal/webhook/v1alpha3` | `webhookv1alpha3.Setup*` 3 sites | ✓ WIRED | manager :596/:601 + Project webhook |
| `project_controller.go checkSchemaRevisionGuard` | `docs/migration/v1alpha2-to-v1alpha3.md` | migrationGuideDocPath constant | ✓ WIRED | constant :2239; target file exists (185 lines) |
| `dispatch_helpers.go ResolveProvider/resolveImage` | `api/v1alpha3 LevelOverrides` | `levelOverrideKey` | ✓ WIRED | :191, :327 |
| `EnvelopeIn.Level` (unchanged) | template selection | 5-valued switch | ✓ WIRED | Level values byte-identical (project/milestone/phase/plan/task) |
| `Makefile verify-no-aggregates` | `api/v1alpha3/*_types.go` | version-agnostic glob + fail-closed | ✓ WIRED | `ls api/v1alpha*/*_types.go` + empty guard |
| `config/samples/kustomization.yaml` | `tide_v1alpha3_*.yaml` | resources list | ✓ WIRED | 18 v1alpha3 entries, 0 legacy |
| `.github/workflows/ci.yaml` | `Makefile verify-no-legacy-api-refs` | make invocation | ✓ WIRED | :66 next to verify-no-aggregates :63 |

### Behavioral Spot-Checks

| Behavior | Command | Result | Status |
| -------- | ------- | ------ | ------ |
| Legacy-refs gate fires on a seeded legacy string | append `v1alpha1` to tracked `docs/gates.md`; `make verify-no-legacy-api-refs` | `make: *** [verify-no-legacy-api-refs] Error 1`; file reverted, `git status` clean | ✓ PASS |
| Legacy-refs gate passes clean on final tree | `make verify-no-legacy-api-refs` | `OK: no legacy API-version references` | ✓ PASS |
| Full integration suite green (Layer A + Layer B) | `make test-int` (orchestrator, merged HEAD) | MAKE_EXIT=0; envtest 56/56, kind 26/26, zero `^--- FAIL` | ✓ PASS |
| Repo builds without legacy packages | `go build ./...` (orchestrator) | exit 0 | ✓ PASS |
| Unit tier green | `make test` (orchestrator) | exit 0, zero `^--- FAIL` | ✓ PASS |

### Requirements Coverage

| Requirement | Source Plan | Description | Status | Evidence |
| ----------- | ----------- | ----------- | ------ | -------- |
| CRANK-01 | 40-01 | api/v1alpha3 copy-reshape, ModelSelection drop, storage flip, reproducible regen | ✓ SATISFIED | Truth 1 |
| CRANK-02 | 40-02 | Envelope decoupled to dispatch group, old string rejected, drift closed, v1beta1 erased | ✓ SATISFIED | Truth 2 |
| CRANK-03 | 40-03 | All consumers on v1alpha3, two-constant guard, owner-refs single-accept | ✓ SATISFIED | Truth 3 |
| CRANK-04 | 40-04 | levels rename via override-key mapping, dispatch identity unchanged, 5-site logging | ✓ SATISFIED | Truth 4 |
| CRANK-05 | 40-05 | v1alpha1+v1alpha2 deleted, single-version CRDs, hardened verify-no-aggregates, PROJECT, relocated test | ✓ SATISFIED | Truth 5 |
| CRANK-06 | 40-06 | Migration chapter + remap table, docs on v1alpha3, samples + kustomization, SECURITY/rbac staleness | ✓ SATISFIED | Truth 6 |
| CRANK-07 | 40-07 | CI-wired legacy-refs gate, seeded-failure proof, make test-int green | ✓ SATISFIED | Truth 7 |

All 7 requirement IDs declared across the phase's plans are accounted for and satisfied. No ORPHANED requirements — REQUIREMENTS.md maps exactly CRANK-01..07 to Phase 40, all claimed by plans 40-01..07 respectively.

### Anti-Patterns Found

| File | Line | Pattern | Severity | Impact |
| ---- | ---- | ------- | -------- | ------ |
| — | — | Debt markers (TBD/FIXME/XXX) in phase source files | ℹ️ None found | Debt-marker BLOCKER gate clear |

Advisory findings from `40-REVIEW.md` (0 Critical / 6 Warning / 10 Info) — none blocks the phase goal:
- **WR-01** (`project_controller.go:265-300`): a redundant re-fetch wraps the generalized guard; if that Get errors, the guard/cycle-gate/wave-derivation are skipped (a fail-open wrapper around a fail-closed guard). Pre-existing Phase-23 shape; the guard itself fail-closes and meets D-04's two-constant contract. INFO/follow-up, not a goal-miss.
- **WR-02** (`envelope.go:61-63`): `EnvelopeIn.Level` public doc comment omits `"project"` from the documented level set. Doc-completeness gap for out-of-tree image authors; production dispatch of `Level="project"` works and is tested. Advisory.
- **WR-06** (`Makefile`): legacy-refs gate sanction filters are basename/line-scoped, theoretically bypassable. Gate is proven alive both directions; this is filter-hardening, not a functional defect.
- Remaining WR-03/04/05 and IN-01..10 are pre-existing or cosmetic (mojibake comments, dead params, stale doc comments) — advisory.

### End-State Legacy-Reference Audit

After excluding sanctioned dirs (`.planning/`, `migration/`, `audit/`, `salvage-*`, `superpowers/`) and sanctioned strings, 6 tracked references to `v1alpha1|v1alpha2` remain — all intentional and gate-sanctioned:
- `project_controller_v2_guard_test.go:138` — deliberate `SchemaRevision: "v1alpha2"` "wrong-value" case testing the guard rejects it (gate-excluded).
- `envelope_test.go:396/403/411/412` — rejection test proving the old `tideproject.k8s/v1alpha1` group string is refused (gate-excluded).
- `docs/superpowers/specs/2026-06-28-...md` — dated DEFERRED-VISION design doc referencing the then-current `api/v1alpha2` (gate-excluded via `superpowers/**`).

No production-code legacy leftovers. `git grep "api/v1alpha[12]" -- '*.go'` → 0 files.

### Human Verification Required

None. All plans are `autonomous` with no deferred `<verify><human-check>` blocks. The one item 40-07-SUMMARY deferred to verify-work — re-running kind Layer B `make test-int` once Docker Desktop recovered — has been executed by the orchestrator on the final merged HEAD (Layer B kind 26/26 SUCCESS, MAKE_EXIT=0). No visual/UX/external-service surface in this phase requires human eyes.

### Gaps Summary

No gaps. All 7 CRANK requirements are satisfied with codebase evidence at the existence, substantive, wiring, and data-flow levels. The full version-lifecycle turn is complete: `api/` holds only `v1alpha3`; all 6 CRDs (bases + chart) are single-version served+storage v1alpha3; every binary, webhook, dispatch site, CLI, and test compiles against v1alpha3 with zero legacy imports; the `subagent.levels` semantic rename is implemented via `levelOverrideKey` with dispatch identity byte-unchanged and resolved-model logging at all 5 sites; the SchemaRevision guard is a two-constant crank mechanism; owner-refs single-accept; docs/samples swept onto v1alpha3 with a full migration chapter; the envelope contract is decoupled to `dispatch.tideproject.k8s/v1alpha1`; and a CI-wired, seeded-failure-proven `verify-no-legacy-api-refs` gate locks the end state. `make test-int` is green (56/56 + 26/26) on the merged HEAD.

---

_Verified: 2026-07-12T03:10:36Z_
_Verifier: Claude (gsd-verifier)_
