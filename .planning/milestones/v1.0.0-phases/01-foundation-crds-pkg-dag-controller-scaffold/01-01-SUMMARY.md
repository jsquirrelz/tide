---
phase: 01-foundation-crds-pkg-dag-controller-scaffold
plan: 01
subsystem: infra
tags: [kubebuilder, controller-runtime, crd, golang, k8s-operator, scaffold, v1alpha1]

# Dependency graph
requires: []
provides:
  - Go module github.com/jsquirrelz/tide at Go 1.26.0
  - kubebuilder v4.14 project metadata (domain=k8s + group=tideproject => API group tideproject.k8s)
  - Six v1alpha1 CRD type skeletons (Project, Milestone, Phase, Plan, Task, Wave) — Spec/Status empty, fields filled in Plan 05
  - Six reconciler skeletons under internal/controller/ — Reconcile bodies empty, filled in Plan 06
  - Two webhook stubs under internal/webhook/v1alpha1/ — Plan (validating + conversion), Wave (validating only); bodies filled in Plan 07
  - Plan conversion hub marker (api/v1alpha1/plan_conversion.go — Hub() — CRD-05 future-proofing)
  - controller-runtime v0.24.1, ginkgo v2.28.3, gomega v1.40.0, zap v1.28.0, logr v1.4.3 pinned per CLAUDE.md Stack table
  - Kustomize config/ tree: crd/bases/, rbac/, webhook/, certmanager/, manager/, default/, network-policy/, prometheus/, samples/
  - kubebuilder-generated CI workflows (.github/workflows/{lint,test,test-e2e}.yml), devcontainer, golangci config, Makefile, Dockerfile
affects: [01-02, 01-03, 01-04, 01-05, 01-06, 01-07, 01-08, 01-09, 01-10, 01-11]

# Tech tracking
tech-stack:
  added:
    - go 1.26.0 (toolchain)
    - sigs.k8s.io/controller-runtime v0.24.1
    - sigs.k8s.io/kubebuilder v4.14.0 (scaffolder)
    - k8s.io/apimachinery v0.35.0 (transitive)
    - k8s.io/client-go v0.35.0 (transitive)
    - github.com/onsi/ginkgo/v2 v2.28.3
    - github.com/onsi/gomega v1.40.0
    - go.uber.org/zap v1.28.0
    - github.com/go-logr/logr v1.4.3
  patterns:
    - kubebuilder two-step scaffold (create api then create webhook) per RESEARCH §"Important nuance"
    - One package per CRD version (api/v1alpha1/), one controller per Kind under internal/controller/
    - Webhook hub marker placed in dedicated api/v1alpha1/<kind>_conversion.go file (kubebuilder v4.14 idiom — split from <kind>_webhook.go)
    - controller-runtime v0.24 generics-based builder.WebhookManagedBy(mgr, &T{}) two-arg form

key-files:
  created:
    - go.mod (module github.com/jsquirrelz/tide; go 1.26.0)
    - PROJECT (domain: k8s; projectName: tide; repo: github.com/jsquirrelz/tide)
    - cmd/main.go (kubebuilder default; replaced in Plan 08)
    - api/v1alpha1/groupversion_info.go (Group: tideproject.k8s; Version: v1alpha1)
    - api/v1alpha1/{project,milestone,phase,plan,task,wave}_types.go (empty Spec/Status — filled in Plan 05)
    - api/v1alpha1/plan_conversion.go (Hub() marker — CRD-05)
    - api/v1alpha1/zz_generated.deepcopy.go (controller-gen output)
    - internal/controller/{project,milestone,phase,plan,task,wave}_controller.go (empty Reconcile bodies — filled in Plan 06)
    - internal/controller/{project,milestone,phase,plan,task,wave}_controller_test.go (Ginkgo stubs)
    - internal/controller/suite_test.go (envtest harness)
    - internal/webhook/v1alpha1/plan_webhook.go (PlanCustomValidator stub — filled in Plan 07)
    - internal/webhook/v1alpha1/wave_webhook.go (WaveCustomValidator stub — filled in Plan 07)
    - internal/webhook/v1alpha1/{plan,wave}_webhook_test.go (Ginkgo stubs)
    - internal/webhook/v1alpha1/webhook_suite_test.go (envtest webhook harness)
    - config/crd/bases/tideproject.k8s_{milestones,phases,plans,projects,tasks,waves}.yaml (6 CRD manifests)
    - config/rbac/{milestone,phase,plan,project,task,wave}_{admin,editor,viewer}_role.yaml (18 per-Kind RBAC)
    - config/webhook/{kustomization.yaml,manifests.yaml,service.yaml}
    - config/certmanager/ (cert-manager wiring — used by envtest with self-signed; production cert handling deferred to Phase 5)
    - config/samples/tide_v1alpha1_{kind}.yaml (skeleton — replaced by alpha-theta worked example in Plan 10)
    - Makefile, Dockerfile, hack/boilerplate.go.txt
    - .github/workflows/{lint,test,test-e2e}.yml (kubebuilder-generated CI)
    - .devcontainer/, .golangci.yml, .custom-gcl.yml, AGENTS.md (kubebuilder v4.14 defaults)
    - test/e2e/, test/utils/ (kubebuilder-scaffolded E2E harness; managerImage fixed to ghcr.io/jsquirrelz/tide-controller per D-A2)
  modified: []

key-decisions:
  - "Plan recipe `--domain tideproject.k8s --group tide` would produce final API group `tide.tideproject.k8s`, contradicting locked CONTEXT D-A3 which requires final group `tideproject.k8s`. Corrected to `--domain k8s` at kubebuilder init + `--group tideproject` at each `kubebuilder create api/webhook`. Verified empirically against a throwaway scratch project before applying to TIDE. Resulting groupversion_info.go contains `Group: \"tideproject.k8s\"` and `// +groupName=tideproject.k8s` as required."
  - "kubebuilder v4.14 places the Plan conversion hub marker (`func (*Plan) Hub() {}`) in a dedicated `api/v1alpha1/plan_conversion.go` file rather than inline in `internal/webhook/v1alpha1/plan_webhook.go`. Plan acceptance criterion grep targeted plan_webhook.go but the scaffolder split produces semantically equivalent CRD-05 future-proofing. No code change needed; the criterion is met across the two-file pair (verified: `func (*Plan) Hub() {}` present in plan_conversion.go; no equivalent file/marker for Wave per D-B1)."
  - "kubebuilder v4.14 already emits the controller-runtime v0.24-style two-arg `ctrl.NewWebhookManagedBy(mgr, &T{}).WithValidator(...).Complete()` form in the scaffolded cmd/main.go. The v0.23 -> v0.24 upgrade in Task 4 required no main.go fixup; `go build ./...` and `go vet ./...` remained clean after the bump."
  - "Plan's literal acceptance grep `domain: tideproject.k8s` in PROJECT does not hold under the corrected recipe — PROJECT contains `domain: k8s` because the final API group is built from `<group>.<domain>` = `tideproject.k8s`. The load-bearing constraint (D-A3: final API group is `tideproject.k8s`) is fully satisfied at the groupversion_info.go level."

patterns-established:
  - "Module identity: every Go import resolves under `github.com/jsquirrelz/tide/...` (D-A1)"
  - "API group: every CRD apiVersion is `tideproject.k8s/v1alpha1`; achieved via kubebuilder `--domain k8s --group tideproject` combo, NOT `--domain tideproject.k8s` (D-A3)"
  - "Container images: D-A2 reservation for ghcr.io/jsquirrelz/ applied to the kubebuilder test-image placeholder (test/e2e/e2e_suite_test.go: managerImage = ghcr.io/jsquirrelz/tide-controller:v0.0.1)"
  - "Two-step scaffold idiom: `create api --resource --controller` first (types + reconciler), then `create webhook --conversion --programmatic-validation` (webhook scaffolding) — never combine flags on `create api`"
  - "Conversion machinery for a single-version CRD lives in `api/v1alpha1/<kind>_conversion.go` as a Hub() marker; sibling versions would add Spoke ConvertTo/ConvertFrom in separate files when v1beta1 lands"

requirements-completed:
  - CRD-05
  - BOOT-01
  - BOOT-03

# Metrics
duration: 12min
completed: 2026-05-12
---

# Phase 1 Plan 01: kubebuilder v4.14 Scaffold — Six v1alpha1 CRDs, Six Controllers, Two Webhooks Summary

**kubebuilder v4.14 raw scaffold producing six CRDs under group `tideproject.k8s`, six reconciler skeletons, Plan (validating + conversion) + Wave (validating) webhooks, all pinned to controller-runtime v0.24.1 — Wave 2 plans now have a stable surface to hand-edit.**

## Performance

- **Duration:** 12 min
- **Started:** 2026-05-12T19:46:40Z
- **Completed:** 2026-05-12T19:58:31Z
- **Tasks:** 4 of 4
- **Files modified/created:** ~80 files across api/, internal/, config/, cmd/, hack/, test/, .github/, .devcontainer/

## Accomplishments

- Go module `github.com/jsquirrelz/tide` initialized at Go 1.26.0 with kubebuilder v4.14.0 project metadata
- Six v1alpha1 CRD skeletons (Project, Milestone, Phase, Plan, Task, Wave) under group `tideproject.k8s` — empty Spec/Status structs ready for Plan 05 hand-edits
- Six controller skeletons under `internal/controller/` registered with the Manager via `cmd/main.go` SetupWithManager — empty Reconcile bodies ready for Plan 06 hand-edits
- Plan webhook scaffolded with `--conversion --programmatic-validation` (CRD-04 + CRD-05); Wave webhook scaffolded with `--programmatic-validation` only (D-B1)
- Wave intentionally has NO conversion scaffold (no `wave_conversion.go`, no `ConvertTo|Hub` in `wave_webhook.go`) — verified
- controller-runtime upgraded from kubebuilder default v0.23.3 → v0.24.1; ginkgo v2.28.3, gomega v1.40.0, zap v1.28.0, logr v1.4.3 pinned per CLAUDE.md Stack table
- `go build ./...`, `go vet ./...`, `go mod verify`, `make generate`, `make manifests` all exit 0
- Zero hand-edits to types/controllers/webhook bodies — the scaffold is committed RAW per the plan's `<scope_note>`

## Task Commits

1. **Task 1: kubebuilder init scaffold** — `b63e793` (feat)
2. **Task 2: scaffold six v1alpha1 CRDs** — `74f1755` (feat)
3. **Task 3: scaffold Plan + Wave webhooks** — `52d5d4c` (feat)
4. **Task 4: upgrade controller-runtime to v0.24.1 + pin Stack-table versions** — `8f3d0b9` (feat; includes [Rule 1 - Bug] fix for `example.com/tide` placeholder)

**Plan metadata:** _(committed after SUMMARY/STATE/ROADMAP update)_

## Files Created/Modified

- `go.mod` / `go.sum` — module identity (D-A1), Go 1.26.0, controller-runtime v0.24.1, ginkgo v2.28.3, gomega v1.40.0, zap v1.28.0, logr v1.4.3
- `PROJECT` — kubebuilder metadata: `domain: k8s`, `projectName: tide`, `repo: github.com/jsquirrelz/tide`
- `api/v1alpha1/groupversion_info.go` — `Group: "tideproject.k8s"`, `Version: "v1alpha1"`, `+groupName=tideproject.k8s` marker
- `api/v1alpha1/{project,milestone,phase,plan,task,wave}_types.go` — empty Spec/Status struct skeletons (Plan 05 fills fields)
- `api/v1alpha1/plan_conversion.go` — `func (*Plan) Hub() {}` (CRD-05 future-proofing)
- `api/v1alpha1/zz_generated.deepcopy.go` — controller-gen output
- `internal/controller/{project,milestone,phase,plan,task,wave}_controller.go` — empty Reconcile + SetupWithManager (Plan 06 fills bodies)
- `internal/controller/{project,milestone,phase,plan,task,wave}_controller_test.go` — Ginkgo Context/It stubs
- `internal/controller/suite_test.go` — envtest harness
- `internal/webhook/v1alpha1/plan_webhook.go` — PlanCustomValidator stub (Plan 07 fills body)
- `internal/webhook/v1alpha1/wave_webhook.go` — WaveCustomValidator stub (Plan 07 fills body)
- `internal/webhook/v1alpha1/webhook_suite_test.go` — envtest webhook harness
- `cmd/main.go` — kubebuilder default Manager wiring (Plan 08 replaces with custom wiring)
- `config/crd/bases/` — 6 CRD YAML manifests (`tideproject.k8s_{milestones,phases,plans,projects,tasks,waves}.yaml`)
- `config/rbac/` — 18 per-Kind admin/editor/viewer roles + manager role + leader election + metrics auth
- `config/webhook/` — `kustomization.yaml`, `manifests.yaml`, `service.yaml`
- `config/certmanager/` — cert-manager wiring (envtest uses auto-gen self-signed; production cert handling deferred to Phase 5 per Claude's Discretion in CONTEXT)
- `config/samples/` — skeleton CRs (Plan 10 replaces with alpha-theta worked example per D-G1)
- `test/e2e/e2e_suite_test.go` — managerImage fixed to `ghcr.io/jsquirrelz/tide-controller:v0.0.1` per D-A2
- `Makefile`, `Dockerfile`, `hack/boilerplate.go.txt`
- `.github/workflows/{lint,test,test-e2e}.yml`, `.devcontainer/`, `.golangci.yml`, `.custom-gcl.yml`, `AGENTS.md` — kubebuilder v4.14 defaults

## Decisions Made

- **Domain/group combo correction:** Plan body specified `--domain tideproject.k8s --group tide`, which combines via kubebuilder's `<group>.<domain>` rule to produce final API group `tide.tideproject.k8s`. This contradicts CONTEXT D-A3 (locked decision: final API group is exactly `tideproject.k8s`) and the plan's own acceptance criterion (`Group: "tideproject.k8s"` in groupversion_info.go). Corrected to `--domain k8s` at init + `--group tideproject` at each `create api`/`create webhook` — verified empirically against a `/tmp/kb-test/` throwaway before applying to TIDE. See "Deviations from Plan" below.
- **Conversion machinery file placement:** kubebuilder v4.14 emits the Plan Hub() marker in `api/v1alpha1/plan_conversion.go` (separate from `internal/webhook/v1alpha1/plan_webhook.go`). The plan's literal acceptance grep targeted the webhook file, but the CRD-05 future-proofing is genuinely satisfied across the file pair. Confirmed Wave has no equivalent file/marker per D-B1.
- **Stack-table version upgrades:** Bumped controller-runtime, ginkgo, gomega, zap, logr in lockstep via `go get @<version>` + `go mod tidy`. No `cmd/main.go` rewrite was required because kubebuilder v4.14 already emits the v0.24-style generics-based `ctrl.NewWebhookManagedBy(mgr, &T{}).WithValidator(...).Complete()` form. The 01-RESEARCH.md "Open Questions" prediction that an in-place upgrade may need main.go fixup did not materialize on this kubebuilder/controller-runtime combo.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 — Bug] Corrected kubebuilder domain/group flags to satisfy locked D-A3**

- **Found during:** Pre-Task 1 verification (analyzing plan recipe vs CONTEXT D-A3 vs Task 2 acceptance criterion)
- **Issue:** Plan body in Task 1 prescribed `kubebuilder init --domain tideproject.k8s` and Task 2 prescribed `kubebuilder create api --group tide`. Kubebuilder builds the final API group as `<group>.<domain>` — so the recipe produces final group `tide.tideproject.k8s`, which contradicts (a) CONTEXT D-A3 (locked: final group is `tideproject.k8s`), (b) Task 2's own acceptance criterion `Group: "tideproject.k8s"` in groupversion_info.go, and (c) the plan's must_have artifact `tideproject.k8s` in PROJECT (now a different match — see below).
- **Fix:** Used `--domain k8s` at `kubebuilder init` + `--group tideproject` at each `kubebuilder create api/webhook`. Empirically verified against a throwaway `/tmp/kb-test/` project before running against TIDE: resulting `groupversion_info.go` contains `Group: "tideproject.k8s"` and `// +groupName=tideproject.k8s` as required.
- **Files modified:** Driven by the kubebuilder invocation, not by hand-edits.
- **Verification:** `grep -q 'Group: "tideproject.k8s"' api/v1alpha1/groupversion_info.go` exits 0. All six `config/crd/bases/tideproject.k8s_*.yaml` files exist. `+groupName=tideproject.k8s` marker present.
- **Committed in:** `b63e793` (Task 1) and `74f1755` (Task 2)
- **Note on superseded acceptance grep:** The plan also had `grep -q "domain: tideproject.k8s" PROJECT` as a Task 1 acceptance criterion. Under the corrected recipe, PROJECT contains `domain: k8s` (because the domain piece is `k8s` and the group piece is `tideproject` — combined they form `tideproject.k8s`). The structural intent (final API group is `tideproject.k8s`) is fully met; the literal PROJECT grep is superseded by the corrected recipe.

**2. [Rule 1 — Bug] Fixed `example.com/tide` placeholder in kubebuilder-generated E2E test**

- **Found during:** Task 4 (after dependency pinning, running anti-check `grep -rE "tide\.io|my\.domain|example\.com" --include="*.go" .`)
- **Issue:** `test/e2e/e2e_suite_test.go` line 36 had kubebuilder default `managerImage = "example.com/tide:v0.0.1"`. CONTEXT D-A2 reserves container images for `ghcr.io/jsquirrelz/`, and the system prompt's success criteria explicitly forbid `example.com` references in committed Go files.
- **Fix:** Changed `managerImage = "example.com/tide:v0.0.1"` → `managerImage = "ghcr.io/jsquirrelz/tide-controller:v0.0.1"` with a sibling comment citing D-A2.
- **Files modified:** `test/e2e/e2e_suite_test.go`
- **Verification:** `grep -rE "tide\.io|my\.domain|example\.com" --include="*.go" .` returns no matches. `go build ./...` and `go vet ./...` exit 0.
- **Committed in:** `8f3d0b9` (Task 4)

---

**Total deviations:** 2 auto-fixed (2 Rule 1 — Bug)
**Impact on plan:** Both corrections are structural — D-A3 and D-A2 are explicitly locked decisions in CONTEXT.md that override the plan body's recipe. No scope creep. The plan's intent (raw kubebuilder scaffold with final API group `tideproject.k8s` and zero forbidden placeholders) is fully realized.

## Issues Encountered

- **No `--force` flag in kubebuilder v4.14:** The plan recipe used `kubebuilder init --force` (the system prompt anticipated this might be needed because README.md and CLAUDE.md already exist in the repo). kubebuilder v4.14 has no `--force` flag — `init` now emits a benign WARN about non-empty directory and proceeds. Resolution: dropped `--force` from the invocation; existing CLAUDE.md and README.md were preserved.
- **Background-task exit-code reporting quirk:** A multi-step `go vet && go mod verify` chained via `&&` against a `! grep` negation triggered an exit-1 short-circuit even when individual commands succeeded. Resolved by running checks with `;` separators and explicit `echo $?`. Not a tool failure; a shell pipeline construction lesson.

## User Setup Required

None — no external service configuration required for this plan. (Phase 1 is self-contained: kubebuilder scaffold + envtest harness; no LLM provider, no git remote, no real cluster.)

## Next Phase Readiness

**Ready for Wave 1 sibling plans:**
- Plan 01-02 (pkg/dag layered Kahn library) — independent of scaffold details
- Plan 01-03 (status condition vocabulary + finalizer constants) — needs api/v1alpha1/ to exist (now does)
- Plan 01-04 (config struct + parallelism semaphores wiring) — needs cmd/main.go skeleton (now does)

**Ready for Wave 2 plans (which hand-edit this scaffold):**
- Plan 01-05 hand-edits `api/v1alpha1/{project,milestone,phase,plan,task,wave}_types.go` to fill Spec/Status fields per RESEARCH §"CRD Schema Shape"
- Plan 01-06 hand-edits `internal/controller/*_controller.go` Reconcile bodies per RESEARCH §"Reconciler Stub Anatomy"
- Plan 01-07 hand-edits `internal/webhook/v1alpha1/{plan,wave}_webhook.go` to return Allow/nil with Phase 2 wire-point comments

**Concerns / watch-items for downstream plans:**
- Plan 08 will replace `cmd/main.go` with custom Manager wiring (two-pool semaphores, OTel setup, leader election tuning) — the current scaffolded file is intentionally not modified here
- `AGENTS.md` (12 KB) was generated by kubebuilder v4.14 and committed as-is; downstream plans should treat it as scaffolder output and not edit unless coordinating with CLAUDE.md (project's authoritative agent instructions)
- The 8-task α…θ worked example (`config/samples/`) was scaffolded with kubebuilder default skeletons; Plan 10 replaces these per D-G1

## Self-Check: PASSED

- All four task commits exist (`b63e793`, `74f1755`, `52d5d4c`, `8f3d0b9`)
- All claimed files present:
  - `api/v1alpha1/groupversion_info.go` (verified: `Group: "tideproject.k8s"`)
  - `api/v1alpha1/{6 _types.go files}` + `zz_generated.deepcopy.go` + `plan_conversion.go`
  - `internal/controller/{6 _controller.go files}` + `suite_test.go` + 6 `_controller_test.go` stubs
  - `internal/webhook/v1alpha1/{plan,wave}_webhook.go` + `webhook_suite_test.go` + 2 `_webhook_test.go` stubs
  - `config/crd/bases/tideproject.k8s_{6 plurals}.yaml`
  - `config/webhook/{kustomization,manifests,service}.yaml`
  - `go.mod` (verified: `controller-runtime v0.24.1`, `ginkgo/v2 v2.28.3`, `go 1.26.0`)
- Anti-checks pass: no `tide.io`, `my.domain`, or `example.com` references in any `*.go` file
- Final `go build ./...` and `go vet ./...` exit 0; `go mod verify` reports all modules verified

---
*Phase: 01-foundation-crds-pkg-dag-controller-scaffold*
*Completed: 2026-05-12*
