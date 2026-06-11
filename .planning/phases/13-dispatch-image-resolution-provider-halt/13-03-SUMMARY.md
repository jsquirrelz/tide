---
phase: 13-dispatch-image-resolution-provider-halt
plan: 03
subsystem: infra
tags: [helm, chart, deployment, subagent-image, test-harness, kind, acceptance]

requires:
  - phase: 13-dispatch-image-resolution-provider-halt
    plan: 01
    provides: "resolveImage chain in controllers + main.go shim (flag still accepted as override tier)"

provides:
  - "charts/tide/templates/deployment.yaml: --subagent-image flag removed; CLAUDE_SUBAGENT_IMAGE sourced from .Values.subagent.defaults.image with tag-defaulting"
  - "charts/tide/values.yaml: Phase 13 D-01 posture doc + updated stale images comment block"
  - "hack/helm/tide-values.yaml: identical comment/posture updates (mirror)"
  - "test/integration/kind/projects_pvc_test.go: two new chart contract tests + stub opt-in harness test"
  - "test/integration/kind/suite_test.go: explicit stub opt-in via --set subagent.defaults.image in helmControllerArgs"
  - "hack/scripts/acceptance-v1.sh: mode-conditional HELM_EXTRA_SUBAGENT_ARGS for small/$0 stub mode"

affects:
  - acceptance-v1
  - chart-release
  - 13-04

tech-stack:
  added: []
  patterns:
    - "Helm tag-defaulting: bare image ref (no `:tag` or `@digest`) gets `:<appVersion>` appended via `regexMatch + contains`; qualified refs pass through verbatim"
    - "Stub as explicit opt-in: test harnesses declare `--set subagent.defaults.image=stub:tag` rather than relying on an implicit startup flag"

key-files:
  created: []
  modified:
    - charts/tide/templates/deployment.yaml
    - charts/tide/values.yaml
    - hack/helm/tide-values.yaml
    - test/integration/kind/projects_pvc_test.go
    - test/integration/kind/suite_test.go
    - hack/scripts/acceptance-v1.sh
    - test/integration/kind/testdata/bare-project.yaml

key-decisions:
  - "D-01 (Phase 13): chart --subagent-image flag dropped as deliberate fixed-contract exception; CLAUDE_SUBAGENT_IMAGE env is now the sole default tier"
  - "D-02 (Phase 13): stub is explicit opt-in everywhere — harnesses declare subagent.defaults.image; production chart default is the real claude subagent"
  - "T-13-09/T-13-10: Helm-value image injection is at cluster-operator trust level; values.yaml documents digest-pinning guidance for production"

patterns-established:
  - "Helm deployment.yaml pattern for bare vs qualified image refs: use `regexMatch` + `contains '@'` to detect tag/digest suffix and default to appVersion"
  - "Test harness stub opt-in pattern: --set subagent.defaults.image=ghcr.io/jsquirrelz/tide-stub-subagent:test in helmControllerArgs"

requirements-completed: [DISPATCH-02]

duration: 55min
completed: 2026-06-11
---

# Phase 13 Plan 03: Chart --subagent-image Flag Drop + Stub Explicit Opt-In Summary

**Helm chart drops the v1.0.0 stub-forcing flag; CLAUDE_SUBAGENT_IMAGE flows from subagent.defaults.image with tag-defaulting; test harnesses and acceptance script opt into the stub explicitly (Phase 13 D-01/D-02)**

## Performance

- **Duration:** ~55 min
- **Started:** 2026-06-11T17:44:00Z
- **Completed:** 2026-06-11T18:37:01Z
- **Tasks:** 2
- **Files modified:** 7

## Accomplishments

- Dropped `--subagent-image=<stub>` from `charts/tide/templates/deployment.yaml` (line 30) — the line that silently forced the stub in every v1.0.0 production install
- Rewired `CLAUDE_SUBAGENT_IMAGE` to source from `.Values.subagent.defaults.image` with Helm-native tag-defaulting (bare ref → `:<appVersion>`; tag/digest-qualified → verbatim passthrough)
- Documented the Phase 13 D-01 resolution posture and stub opt-in instructions in both `values.yaml` and `hack/helm/tide-values.yaml`
- Added three chart contract tests to `projects_pvc_test.go` (TDD RED-then-GREEN): flag absence, env source, stub harness opt-in
- Updated `helmControllerArgs` in `suite_test.go` to explicitly pass `--set subagent.defaults.image=ghcr.io/jsquirrelz/tide-stub-subagent:test` (D-02 stub opt-in)
- Updated `acceptance-v1.sh` to conditionally pass `--set subagent.defaults.image=stub:${IMAGE_TAG}` for small/$0 mode only (real sample mode is correct out of the box)

## Task Commits

Each task was committed atomically:

1. **TDD RED — chart contract tests** - `84483ba` (test)
2. **Task 1: Drop flag + rewire CLAUDE_SUBAGENT_IMAGE (GREEN)** - `0a126b4` (feat)
3. **Task 2: Explicit stub opt-in for Layer B + acceptance** - `f40b420` (feat)

## Files Created/Modified

- `charts/tide/templates/deployment.yaml` — Removed `--subagent-image=...stubSubagent...` arg (line 30); replaced `CLAUDE_SUBAGENT_IMAGE` env value template to use `subagent.defaults.image` with `regexMatch`-based tag-defaulting
- `charts/tide/values.yaml` — Updated stale images comment block (flag dropped, not injected); added Phase 13 D-01 posture doc at `subagent.defaults.image` (production default = real subagent; stub opt-in instructions; digest pinning note)
- `hack/helm/tide-values.yaml` — Identical comment/posture updates (mirrors values.yaml line-for-line in these regions)
- `test/integration/kind/projects_pvc_test.go` — Added `TestHelmDeploymentTemplateDropsSubagentImageFlag`, `TestHelmDeploymentTemplateSubagentImageEnvFromDefaults`, `TestHelmControllerArgsStubOptIn`
- `test/integration/kind/suite_test.go` — Added `--set subagent.defaults.image=ghcr.io/jsquirrelz/tide-stub-subagent:test` to `helmControllerArgs`; updated stale `--subagent-image` comment
- `hack/scripts/acceptance-v1.sh` — `HELM_EXTRA_SUBAGENT_ARGS` variable computed from `ACCEPTANCE_SAMPLE`; stub passed only for small mode
- `test/integration/kind/testdata/bare-project.yaml` — Updated stale comment referencing the dropped flag

## Decisions Made

- Helm tag-defaulting uses `regexMatch ":[^/]+$"` + `contains "@"` on the image value. This correctly detects `repo/image:tag` and `repo/image@sha256:...` as qualified while treating bare `repo/image` as needing `:<appVersion>` appended. The template uses `{{- $img := .Values.subagent.defaults.image }}` and a `{{- if ... }}` block to avoid nested-quote parse errors in YAML.
- Kept `images.stubSubagent.*` keys in values.yaml — they drive image build/load tooling (make test-int-kind-prep), they just no longer inject a flag into the Deployment args.
- `HELM_EXTRA_SUBAGENT_ARGS` uses `# shellcheck disable=SC2086` for intentional word-splitting of the empty-string case (empty = no extra args).

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] Fixed Helm template parse error in CLAUDE_SUBAGENT_IMAGE env**
- **Found during:** Task 1 (chart implementation)
- **Issue:** Initial implementation used escaped double-quotes inside an outer YAML double-quoted string: `value: "{{ if or (regexMatch \":[^/]+$\" ...) }}"` — Helm parser rejected with `unexpected "\\" in operand`
- **Fix:** Restructured to use `{{- $img := .Values.subagent.defaults.image }}` + `{{- if or ... }}` / `{{- else }}` block outside the YAML double-quoted value
- **Files modified:** `charts/tide/templates/deployment.yaml`
- **Verification:** `helm template tide charts/tide` exits 0; renders `ghcr.io/jsquirrelz/tide-claude-subagent:1.0.0` by default; `--set subagent.defaults.image=ghcr.io/jsquirrelz/tide-stub-subagent:test` renders that ref verbatim
- **Committed in:** `0a126b4` (Task 1 feat commit)

**2. [Rule 1 - Bug] Removed lingering `--subagent-image=` substring from comment in deployment.yaml**
- **Found during:** Task 1 verification (acceptance criteria grep check)
- **Issue:** A comment explaining the removal included the literal `--subagent-image=` string, causing the `grep -c 'subagent-image' deployment.yaml` acceptance criterion to return 1 instead of 0
- **Fix:** Reworded comment to `"The startup flag injecting the stub image has been removed"` — conveys the same meaning without the literal flag string
- **Files modified:** `charts/tide/templates/deployment.yaml`
- **Committed in:** `0a126b4` (Task 1 feat commit)

---

**Total deviations:** 2 auto-fixed (both Rule 1 bugs, found during implementation)
**Impact on plan:** Both fixes were in-flight during the same commit; no extra commits required.

## Test Verification

### Plain go-tests (cluster-free contract tests) — all PASSED

```
--- PASS: TestHelmControllerArgsUpgradeInstallReusesExistingRelease
--- PASS: TestHelmControllerArgsForcesManagerRollout
--- PASS: TestHelmControllerArgsStubOptIn
--- PASS: TestHelmDeploymentTemplateRendersManagerPodAnnotations
--- PASS: TestHelmDeploymentTemplateDropsSubagentImageFlag
--- PASS: TestHelmDeploymentTemplateSubagentImageEnvFromDefaults
--- PASS: TestThreeTaskWaveFixtureIncludesProjectsPVC
--- PASS: TestProjectsPVCYAMLBuildsNamespaceLocalRWOClaim
--- PASS: TestSigningKeySecretYAMLBuildsNamespaceLocalSecret
ok  github.com/jsquirrelz/tide/test/integration/kind  0.59s
```

### Helm render assertions (manual, all PASSED)

- `grep -c 'subagent-image' charts/tide/templates/deployment.yaml` → 0
- `grep -c 'subagent.defaults.image' charts/tide/templates/deployment.yaml` → 2
- `helm template tide charts/tide` → `CLAUDE_SUBAGENT_IMAGE: "ghcr.io/jsquirrelz/tide-claude-subagent:1.0.0"` (real subagent default)
- `helm template tide charts/tide --set subagent.defaults.image=ghcr.io/jsquirrelz/tide-stub-subagent:test` → `CLAUDE_SUBAGENT_IMAGE: "ghcr.io/jsquirrelz/tide-stub-subagent:test"` (stub verbatim)

### make test-int — pre-existing failures block green run

`make test-int` exits non-zero due to pre-existing failures in `test/integration/envtest/` (Layer A) that were present at the wave 1 base commit (cdde58d) before this plan's changes:

**Layer A (envtest) — 6 pre-existing failures (NOT caused by this plan's changes):**
- `planner_dispatch_test.go:146` — Phase 04.1 planner dispatch
- `boundary_push_test.go:272,326` — boundary push (phase/plan levels)
- `gates_test.go:285` — gate approve flow
- `indegree_test.go:112` — FAIL-01 indegree blocks dispatch
- `gates_test.go:178` — GATE-04 descent hold

These failures are in `test/integration/envtest/` (out of scope for plan 13-03). The 13-04 parallel executor is responsible for `internal/controller/` fixes that resolve these.

**Layer B (kind) — 2 pre-existing Ginkgo spec failures (NOT caused by this plan's changes):**
- `credproxy_test.go:75` — `spec.promptPath: Required value` in fixture (CEL CRD validation; fixture predates the required field addition)
- `medium_http_test.go:433` — `go test -timeout=20m` expired waiting for Project to reach Complete over HTTP (environment-constrained run)

All 9 plain go-tests in `test/integration/kind/` (the files this plan owns) passed. The kind cluster installed cleanly with the new chart (`subagent.defaults.image` opt-in rendered correctly; helm upgrade --install completed in 19s).

## Deferred Issues

Pre-existing failures documented above; logged here for 13-04 visibility:
- `test/integration/envtest/indegree_test.go:112` — FAIL-01 indegree recomputed per reconcile
- `test/integration/envtest/gates_test.go:178` — GATE-04 descent hold
- `test/integration/kind/credproxy_test.go:75` — spec.promptPath Required CRD validation missing from fixture

## Threat Surface Scan

No new network endpoints, auth paths, file access patterns, or schema changes introduced. The `subagent.defaults.image` Helm value replaces the `images.claudeSubagent.*` path that was already at the same trust level. T-13-09, T-13-10, T-13-11 threats are as documented in the plan's `<threat_model>`.

## Known Stubs

None — no stubs introduced. The chart default `ghcr.io/jsquirrelz/tide-claude-subagent` is a real published image. The stub opt-in is explicit in test contexts.

## Next Phase Readiness

- Chart ships the real subagent by default; production installs no longer silently force stub
- Test harnesses have been updated for D-02 explicit stub opt-in
- 13-04 (controller gate/halt fixes) will resolve the pre-existing envtest failures; after that merge, `make test-int` should be green

---
*Phase: 13-dispatch-image-resolution-provider-halt*
*Completed: 2026-06-11*
