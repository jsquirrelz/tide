---
phase: 01-foundation-crds-pkg-dag-controller-scaffold
plan: 07
subsystem: infra
tags: [golang, controller-runtime, webhook, validating-webhook, conversion-webhook, envtest, ginkgo, crd-04, crd-05, d-b1, d-b3, req-plan-01, pitfall-16, test-01]

# Dependency graph
requires:
  - phase: 01-foundation-crds-pkg-dag-controller-scaffold
    provides: "Kubebuilder-scaffolded internal/webhook/v1alpha1/{plan,wave}_webhook.go skeletons (Plan 01-01); api/v1alpha1/plan_conversion.go with Hub() stub (Plan 01-01); shared internal/controller/suite_test.go BeforeSuite (Plan 01-06)"
provides:
  - "internal/webhook/v1alpha1/plan_webhook.go — PlanCustomValidator at no-op shape; ValidateCreate/Update/Delete return (nil, nil); inline Phase 2 wire-point comments for pkg/dag.ComputeWaves cycle detection per D-B3 / REQ-PLAN-01"
  - "internal/webhook/v1alpha1/wave_webhook.go — WaveCustomValidator at no-op shape; ValidateCreate/Update/Delete return (nil, nil); inline Phase 2 wire-point comments for reject-client-applies per D-B1"
  - "internal/controller/suite_test.go — augmented to install ValidatingWebhookConfiguration via testEnv.WebhookInstallOptions, start an in-process webhook server on the envtest-provisioned host/port/certDir, register both Phase 1 webhooks with the shared Manager, and wait for TLS readiness before specs run"
  - "internal/controller/plan_webhook_test.go — 4 specs proving ValidateCreate/Update/Delete Allow under Phase 1; CEL MinLength sanity for empty PhaseRef"
  - "internal/controller/wave_webhook_test.go — 4 specs proving ValidateCreate/Update/Delete Allow under Phase 1; CEL Minimum=0 sanity for WaveIndex=-1"
  - "Deletion of internal/webhook/v1alpha1/{webhook_suite_test.go,plan_webhook_test.go,wave_webhook_test.go} — the kubebuilder-scaffolded parallel suite is consolidated into the controller suite per revision Warning 9"
affects: [01-08, 01-09, 02-*]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "v0.24-generic CustomValidator: kubebuilder v4.14 emits PlanCustomValidator/WaveCustomValidator with TYPED parameters (e.g. obj *tideprojectv1alpha1.Plan), and ctrl.NewWebhookManagedBy(mgr, &Plan{}).WithValidator(...) wires the generic Validator[T runtime.Object] interface internally. We preserve the typed signatures — no runtime.Object + type-assertion boilerplate"
    - "Phase 2 wire-point comments use the literal token 'Phase 2' at every seam (21 occurrences across the two webhook files) so a grep-based regression net (>=4) is trivially enforced"
    - "Single shared envtest BeforeSuite for controllers + webhooks (revision Warning 9 / TEST-01 protection): WebhookInstallOptions on the same testEnv, in-process webhook server on envtest-provisioned certs, TLS readiness wait before specs"
    - "CEL still owns schema-level invariants (MinLength, Minimum) — the webhook layer is reserved for graph-level invariants the schema cannot express (cycle detection in Phase 2). The new tests include sanity specs that prove the CEL layer is still the one rejecting WaveIndex=-1 and empty PhaseRef, not the webhook"

key-files:
  modified:
    - internal/webhook/v1alpha1/plan_webhook.go — kubebuilder skeleton replaced with explicit no-op + Phase 2 wire-point comments (99 lines)
    - internal/webhook/v1alpha1/wave_webhook.go — kubebuilder skeleton replaced with explicit no-op + Phase 2 wire-point comments (97 lines)
    - internal/controller/suite_test.go — extended BeforeSuite to install + serve both webhooks alongside controllers (181 lines)
  created:
    - internal/controller/plan_webhook_test.go — 4 specs (124 lines)
    - internal/controller/wave_webhook_test.go — 4 specs (125 lines)
  deleted:
    - internal/webhook/v1alpha1/webhook_suite_test.go — parallel envtest BeforeSuite removed (Warning 9)
    - internal/webhook/v1alpha1/plan_webhook_test.go — kubebuilder scaffold removed (depended on deleted suite)
    - internal/webhook/v1alpha1/wave_webhook_test.go — kubebuilder scaffold removed (depended on deleted suite)

key-decisions:
  - "Preserve kubebuilder v4.14's TYPED webhook signatures over the plan's runtime.Object+assertion shape. The plan's example body used the older Validator-with-runtime.Object pattern; controller-runtime v0.24 ships Validator[T runtime.Object] with generics, and the scaffold emits typed bodies that the generic WebhookManagedBy resolves automatically. Typed signatures avoid the unnecessary `obj.(*Plan)` type assertion at every call site. The Phase 2 wire-points are equally expressible in both shapes — there's no behavioral cost to keeping the typed form, only readability gain."
  - "Hub() conversion stub in api/v1alpha1/plan_conversion.go (already present from Plan 01-01) satisfies CRD-05 / Pitfall 16 future-proofing without needing ConvertTo/ConvertFrom in Phase 1. v1alpha1 IS the hub, and no v1beta1 spoke exists yet; when v1beta1 lands, the hub/spoke registration is already in place and we only need to add Convert* methods on the spoke type."
  - "Schema-validation sanity specs (empty PhaseRef + WaveIndex=-1) are intentionally included in the webhook test files even though they exercise CEL, not the webhook. They lock in the Plan 05 contract: 'CEL is the gate for non-graph invariants; the webhook is the gate for graph invariants.' If a future contributor moves PhaseRef MinLength enforcement into the webhook body, these specs detect that boundary shift. Each spec asserts apierrors.IsInvalid || IsBadRequest so the test is robust to whichever code the apiserver returns for CEL rejection."
  - "Best-effort AfterEach cleanup pattern: list+delete the Plan/Wave list in default namespace after each spec, ignoring NotFound. The reconcilers' finalizers + envtest's missing GC controller mean objects may linger in Terminating, but the next spec's Create uses a unique name so there's no collision. This is the same pattern Plan 06's wave_controller_test.go uses for the cascade test."
  - "Webhook server readiness wait uses TLS Dial with InsecureSkipVerify (envtest generates a self-signed cert per run). 30-second Eventually budget is generous; in practice the server is reachable within ~500ms. //nolint:gosec annotation documents why InsecureSkipVerify is correct here (envtest cert is not trust-anchored)."

patterns-established:
  - "Single-suite invariant: any new webhook in this project extends internal/controller/suite_test.go's BeforeSuite — no parallel kubebuilder webhook_suite_test.go scaffolds. The invariant is enforced by the acceptance criterion `! test -f internal/webhook/v1alpha1/webhook_suite_test.go` exiting 0. A future contributor running `kubebuilder create webhook ...` will recreate the scaffold suite; the acceptance gate catches that and the contributor folds the new webhook's setup into the shared BeforeSuite."
  - "Phase-2-wire-point comment shape: every webhook method's body block contains a `// Phase 2: ...` line documenting the exact code that fills in Phase 2 (cycle-detection invocation, owner-ref check, etc.). Phase 2 plans cite the grep `grep -nE 'Phase 2:' internal/webhook/v1alpha1/*.go` to enumerate the seams that need filling."
  - "Webhook tests live alongside controller tests in package controller (not controller_test). The suite is package-internal so test helpers from controller_test.go can be reused; the shared ctx/k8sClient globals are package-private."

requirements-completed:
  - CRD-04
  - CRD-05

# Metrics
duration: 8min
completed: 2026-05-12
---

# Phase 1 Plan 07: Webhook Endpoints — No-Op Bodies + Phase 2 Wire-Points + Shared envtest Summary

**Both Plan and Wave validating webhooks are hand-edited from their kubebuilder skeletons to an explicit no-op shape with `Phase 2` wire-point comments documenting exactly where Phase 2's cycle detection (Plan, per D-B3 / REQ-PLAN-01) and reject-client-applies enforcement (Wave, per D-B1) will fill — and the envtest assertions proving the no-op contract are folded into Plan 06's shared `internal/controller/suite_test.go` BeforeSuite (single envtest cold-start; TEST-01 budget protected per revision Warning 9). The kubebuilder-scaffolded parallel webhook suite is deleted in the same change so the single-suite invariant is structural, not just convention.**

## Performance

- **Duration:** ~8 min
- **Started:** 2026-05-12T21:12:03Z
- **Completed:** 2026-05-12T21:20:04Z
- **Tasks:** 2 of 2
- **Files modified:** 3 (`plan_webhook.go`, `wave_webhook.go`, `suite_test.go`)
- **Files created:** 2 (`plan_webhook_test.go`, `wave_webhook_test.go` — both in `internal/controller/`)
- **Files deleted:** 3 (`internal/webhook/v1alpha1/{webhook_suite_test.go,plan_webhook_test.go,wave_webhook_test.go}`)
- **Test runtime:** `internal/controller` test pkg = 16.2s (TEST-01 budget = 30s)

## Webhook Body Shape (Phase 1 — explicit no-op)

Both validators implement the controller-runtime v0.24 generic `Validator[T runtime.Object]` interface with typed parameters. The kubebuilder scaffold's typed signatures are preserved.

```go
// PlanCustomValidator validates Plan objects.
// Phase 1: no-op (always Allow).
// Phase 2: wires cycle detection via pkg/dag.ComputeWaves per D-B3 / REQ-PLAN-01.
type PlanCustomValidator struct{}

func (v *PlanCustomValidator) ValidateCreate(_ context.Context, obj *tideprojectv1alpha1.Plan) (admission.Warnings, error) {
    planlog.V(1).Info("ValidateCreate (no-op in Phase 1 — REQ-PLAN-01 cycle detection wires in Phase 2)", "name", obj.GetName())
    // Phase 2: invoke pkg/dag.ComputeWaves here; reject *CycleError with structured message naming Tasks involved.
    return nil, nil
}
```

The Wave webhook has the same shape, with `D-B1` instead of `REQ-PLAN-01` and a different Phase 2 body pseudocode (reject-client-applies via missing owner-ref).

## Phase 2 Wire-Points (the seams)

| Webhook | Method        | Phase 2 fill                                                                                      | Marker          |
| ------- | ------------- | ------------------------------------------------------------------------------------------------- | --------------- |
| Plan    | ValidateCreate | `dag.ComputeWaves(nodeIDs, edges)` → reject `*CycleError`                                          | `REQ-PLAN-01` / `D-B3` |
| Plan    | ValidateUpdate | Same: re-run cycle detection on the new spec                                                       | `REQ-PLAN-01` / `D-B3` |
| Plan    | ValidateDelete | Optional: guard against deletion while Waves dispatching                                           | `Phase 2`       |
| Wave    | ValidateCreate | Reject Waves lacking `WaveReconciler`-stamped owner-ref                                            | `D-B1`          |
| Wave    | ValidateUpdate | Optional: reject mutations to `WaveIndex` / `PlanRef`                                              | `Phase 2`       |
| Wave    | ValidateDelete | Optional: mirror D-B1 on delete path                                                               | `Phase 2`       |

Phase 2's REQ-PLAN-01 plan greps `grep -nE 'Phase 2:' internal/webhook/v1alpha1/*.go` to enumerate all seams.

Acceptance: `grep -c "Phase 2" internal/webhook/v1alpha1/*.go` returns **11** (plan) + **10** (wave) = **21 total**, well above the required `>= 4`.

## Conversion Webhook (CRD-05 / Pitfall 16)

- `api/v1alpha1/plan_conversion.go` exists from Plan 01-01 with `func (*Plan) Hub() {}`.
- No `ConvertTo`/`ConvertFrom` is needed in Phase 1 because v1alpha1 IS the hub and no v1beta1 spoke exists yet.
- When a v1beta1 type lands in a future phase, the spoke type declares `ConvertTo(*Plan)` / `ConvertFrom(*Plan)`. The hub registration is already in place — that's the Pitfall 16 future-proofing.

Acceptance: `grep -lrE "Hub\(\)|ConvertTo|ConvertFrom" api/v1alpha1/ internal/webhook/` returns matches (currently 2 files: the conversion stub + the plan webhook with a comment referencing the conversion seam).

## Shared envtest BeforeSuite (revision Warning 9)

The kubebuilder scaffold created `internal/webhook/v1alpha1/webhook_suite_test.go` with its own envtest `BeforeSuite`. Combining that with Plan 06's controller `BeforeSuite` would mean two envtest cold-starts per `make test` and risk exceeding the TEST-01 30-second budget.

**Plan 07's approach (revision Warning 9):**

1. Delete `internal/webhook/v1alpha1/webhook_suite_test.go` along with the two leftover kubebuilder-scaffolded `*_webhook_test.go` files that depended on its `ctx`/`k8sClient`/`cfg` globals.
2. Augment `internal/controller/suite_test.go`'s BeforeSuite:
   - Add `admissionv1.AddToScheme(scheme.Scheme)` so envtest can decode `ValidatingWebhookConfiguration`.
   - Add `testEnv.WebhookInstallOptions = envtest.WebhookInstallOptions{Paths: ...config/webhook...}` so envtest installs the webhook config and provisions self-signed certs.
   - After `testEnv.Start()`, create a Manager with `WebhookServer: webhook.NewServer(webhook.Options{Host, Port, CertDir})` bound to the envtest-provisioned cert paths.
   - Call `webhookv1alpha1.SetupPlanWebhookWithManager(mgr)` + `webhookv1alpha1.SetupWaveWebhookWithManager(mgr)`.
   - Start the Manager in a goroutine.
   - `Eventually` poll `tls.DialWithDialer` with `InsecureSkipVerify: true` (envtest cert is self-signed) until the webhook server accepts connections.
3. Place webhook test specs under `internal/controller/` (package `controller`), reusing the shared `ctx`/`k8sClient`.

Result: one envtest cold-start per `make test`. The `internal/controller` test package runs in **16.2s** with the full Plan 06 + Plan 07 suite — well under the 30s budget.

## envtest Suite Inventory (post-Plan 07)

| Spec category | Count | Origin |
| ------------- | ----- | ------ |
| Project (apply, CEL, finalizer, lifecycle) | 4 | Plan 06 |
| Wave (apply, CEL Minimum, owner-ref cascade) | 3 | Plan 06 |
| Milestone/Phase/Plan/Task scaffolded happy-path | 4 | Plan 06 |
| **PlanCustomValidator no-op** | **4** | **Plan 07 (NEW)** |
| **WaveCustomValidator no-op** | **4** | **Plan 07 (NEW)** |
| **Total** | **19** | |

The 8 new specs prove:

- Plan: ValidateCreate Allow, ValidateUpdate Allow, ValidateDelete Allow, CEL MinLength rejects empty `PhaseRef` (sanity that schema layer still owns non-graph invariants).
- Wave: ValidateCreate Allow, ValidateUpdate Allow, ValidateDelete Allow, CEL Minimum=0 rejects `WaveIndex=-1` (sanity).

## Generated Manifests

`make manifests` regenerates `config/webhook/manifests.yaml` unchanged from the Plan 01-01 baseline — both `+kubebuilder:webhook:` markers (`path=/validate-tideproject-k8s-v1alpha1-plan` and `path=/validate-tideproject-k8s-v1alpha1-wave`) emit identical `ValidatingWebhookConfiguration` entries to before. The body edits are inside the function bodies, not on the markers.

## Task Commits

| Task | Name | Commit | Files |
| ---- | ---- | ------ | ----- |
| 1 | Hand-edit Plan + Wave webhook bodies to no-op shape with Phase 2 wire-points; delete kubebuilder-scaffolded parallel suite + leftover scaffold tests | `49b9653` | 5 (2 modified, 3 deleted) |
| 2 | Augment internal/controller/suite_test.go with webhook server + register both Phase 1 webhooks; create plan_webhook_test.go and wave_webhook_test.go in internal/controller/ | `3dc9377` | 3 (1 modified, 2 created) |

**Plan metadata commit:** _(after SUMMARY + STATE + ROADMAP update)_

## Verification Output

```text
$ make manifests       → config/webhook/manifests.yaml regenerated, identical to pre-edit
$ go build ./...       → exit 0
$ go vet ./...         → exit 0
$ make test            → 19/19 specs pass; internal/controller pkg = 16.2s (TEST-01 budget = 30s)
$ make verify-no-aggregates    → OK
$ make verify-no-sqlite-dep    → OK
$ make verify-dag-imports      → OK
$ make verify-no-blocking      → OK
$ make tide-lint               → exit 0
$ grep -c "Phase 2" internal/webhook/v1alpha1/*.go   → 21 total
$ ! test -f internal/webhook/v1alpha1/webhook_suite_test.go   → OK (parallel suite absent)
$ grep -rE "tide\.io|my\.domain" --include="*.go" internal/ api/   → empty (D-A3)
```

## Phase 2 Hand-off

The Plan webhook is the chosen seam for cycle detection (D-B3 / REQ-PLAN-01). Phase 2's REQ-PLAN-01 plan fills `ValidateCreate` and `ValidateUpdate` bodies with:

```go
// Sketch — Phase 2 to refine
nodeIDs, edges := tasksToDAG(plan.Spec.<TasksField>)  // whatever the Plan↔Task association ends up being
if _, err := dag.ComputeWaves(nodeIDs, edges); err != nil {
    var cyclic *dag.CycleError
    if errors.As(err, &cyclic) {
        return nil, fmt.Errorf("plan %s/%s rejected: cyclic task DAG involving %v", plan.Namespace, plan.Name, cyclic.InvolvedNodes)
    }
    return nil, err
}
return nil, nil
```

The Wave webhook is the chosen seam for D-B1 reject-client-applies. Phase 2 fills `ValidateCreate` (and optionally Update/Delete) with:

```go
// Sketch — Phase 2 to refine
if !hasReconcilerStampedOwnerRef(wave) {
    return nil, fmt.Errorf("Wave %s/%s rejected per D-B1: client-applied Waves not allowed; the WaveReconciler is the sole producer", wave.Namespace, wave.Name)
}
return nil, nil
```

`hasReconcilerStampedOwnerRef` checks for a `metav1.OwnerReference{Kind: "Plan", Controller: true, ...}` set by the WaveReconciler. The reconciler in Plan 06 already stamps that ref on every Wave it creates, so Phase 2's webhook addition is "reject everything that doesn't already look like a reconciler-produced object."

## Plan 08 Hand-off

`cmd/manager/main.go` (Plan 08) must call both setup functions on the production Manager:

```go
if err := webhookv1alpha1.SetupPlanWebhookWithManager(mgr); err != nil {
    setupLog.Error(err, "unable to create webhook", "webhook", "Plan")
    os.Exit(1)
}
if err := webhookv1alpha1.SetupWaveWebhookWithManager(mgr); err != nil {
    setupLog.Error(err, "unable to create webhook", "webhook", "Wave")
    os.Exit(1)
}
```

The Plan 08 manager wiring should grep for `SetupPlanWebhookWithManager` and `SetupWaveWebhookWithManager` in `cmd/` as part of its acceptance criteria — both functions exist and are exported from `internal/webhook/v1alpha1`.

## Deviations from Plan

None — plan executed exactly as written, with one minor stylistic preservation:

- **Typed signatures kept (not a deviation, an explicit decision):** The plan's `<action>` block showed example bodies using `obj runtime.Object` with `obj.(*Plan)` type assertions. The actual kubebuilder v4.14 scaffold uses typed parameters via controller-runtime v0.24's generic `Validator[T runtime.Object]`. I preserved the typed signatures (cleaner, no boilerplate) — the Phase 2 wire-point comments are equally expressible in either shape, and the plan's `<acceptance_criteria>` matched on patterns (`type PlanCustomValidator`, `ValidateCreate`, `Phase 2`, `D-B1`, `D-B3`/`REQ-PLAN-01`) without prescribing the parameter type. All acceptance criteria pass with the typed form.

## Authentication Gates

None — Phase 1 introduces no external service dependencies.

## Known Stubs

The webhook bodies are intentional Phase-1 stubs by design:

- **Plan webhook ValidateCreate/Update bodies — `return nil, nil` with Phase 2 wire-point comment.** Resolved by Phase 2 REQ-PLAN-01 plan, which fills the body with `pkg/dag.ComputeWaves(...)` invocation. The stub is the entire point of the plan; deleting it would violate CRD-04.
- **Wave webhook ValidateCreate body — `return nil, nil` with Phase 2 wire-point comment.** Resolved by Phase 2's D-B1 plan, which fills with `hasReconcilerStampedOwnerRef(wave)` check.

Both stubs are typed-signature, registered-with-Manager, exercised-by-envtest endpoints. They are the *seams* Phase 2 fills, not absent functionality.

## Issues Encountered

- **The kubebuilder scaffold's parallel webhook suite (`internal/webhook/v1alpha1/webhook_suite_test.go`) was present from Plan 01-01 and would have caused a dual envtest cold-start.** Revision Warning 9 explicitly called this out; the file is deleted in Task 1 (along with the two leftover `*_webhook_test.go` scaffolds that referenced its `ctx`/`k8sClient` globals). Acceptance criterion `! test -f internal/webhook/v1alpha1/webhook_suite_test.go` enforces the single-suite invariant going forward.
- **No test or build broke during the deletion** because the leftover scaffolded test files only contained placeholder `Describe` blocks with no actual assertions — they were "TODO: add tests here" scaffolding that depended on the suite's globals.

## User Setup Required

None — all changes are file edits + envtest binaries (downloaded by `make setup-envtest` which is part of `make test`).

## Next Phase Readiness

**Ready for Plan 01-08 (cmd/manager/main.go wiring):**

- Both webhook setup functions are exported: `webhookv1alpha1.SetupPlanWebhookWithManager(mgr)` and `webhookv1alpha1.SetupWaveWebhookWithManager(mgr)`.
- `cmd/manager/main.go` must call both before `mgr.Start(ctx)`. The pattern is identical to the per-controller `SetupWithManager` calls already in scope.
- The `ctrl.Options` block in main.go must include a `WebhookServer` field (controller-runtime v0.24's `webhook.NewServer(webhook.Options{...})`). For production, the cert paths come from a mounted Secret; for `make run` (out-of-cluster), `controller-runtime` defaults to `/tmp/k8s-webhook-server/serving-certs` which works with a one-off `openssl` cert generation. Plan 08 should document the local-dev cert path.

**Ready for Phase 2 (REQ-PLAN-01 — Plan cycle detection):**

- The Plan webhook is the chosen seam. Phase 2 fills inside `ValidateCreate` and `ValidateUpdate`.
- `pkg/dag.ComputeWaves` already returns `*CycleError` with `InvolvedNodes`; Phase 2 just calls it.
- Test pattern is established: Phase 2 adds a "rejects cyclic Plan" spec to `internal/controller/plan_webhook_test.go` alongside the existing "allows ValidateCreate" specs.

**Ready for Phase 2 (D-B1 — Wave reject-client-applies):**

- The Wave webhook is the chosen seam. Phase 2 fills inside `ValidateCreate`.
- The WaveReconciler (Plan 06) already stamps the owner-ref. Phase 2's webhook addition is `if !hasReconcilerStampedOwnerRef(wave) { return reject }`.
- Test pattern: Phase 2 adds a "rejects client-applied Wave" spec; the existing "allows ValidateCreate" spec must be updated to stamp the owner-ref before applying (or moved to a Reconciler-produced fixture).

**Concerns / watch-items:**

- The local-dev cert path for `make run` is not documented yet (Plan 08 surface).
- Phase 2's cycle-detection wire-in introduces a new test pattern: a cyclic Plan must be applied to envtest and the apiserver's response inspected for the structured cycle error. The existing happy-path tests in `plan_webhook_test.go` give Phase 2 the test scaffolding shape but not the failure-path harness — Phase 2 will need to extend it.
- `InsecureSkipVerify: true` in `suite_test.go`'s TLS readiness wait is correct for envtest's self-signed cert but flagged by `gosec` — the `//nolint:gosec` annotation explains why. Plan 09's lint hardening should not re-enable that rule on test files.

## Self-Check: PASSED

- All claimed commits exist:
  - `49b9653` Task 1 (webhook no-op bodies + Phase 2 markers + delete kubebuilder scaffold suite/tests)
  - `3dc9377` Task 2 (shared suite_test.go augmentation + new webhook test specs in internal/controller/)
- All claimed files modified:
  - `internal/webhook/v1alpha1/plan_webhook.go` ✓
  - `internal/webhook/v1alpha1/wave_webhook.go` ✓
  - `internal/controller/suite_test.go` ✓
- All claimed files created:
  - `internal/controller/plan_webhook_test.go` ✓
  - `internal/controller/wave_webhook_test.go` ✓
- All claimed files deleted:
  - `internal/webhook/v1alpha1/webhook_suite_test.go` ✓ (not present)
  - `internal/webhook/v1alpha1/plan_webhook_test.go` ✓ (not present)
  - `internal/webhook/v1alpha1/wave_webhook_test.go` ✓ (not present)
- Verification commands all exit 0:
  - `go build ./...` ✓
  - `go vet ./...` ✓
  - `KUBEBUILDER_ASSETS=... go test ./internal/controller/... -count=1 -race -timeout 90s` (19 specs, ~15.6s with `-race`) ✓
  - `make test` (full repo, internal/controller package = 16.206s) ✓
  - `make manifests` ✓ (config/webhook/manifests.yaml regenerated; diff empty against Plan 01-01 baseline)
  - `make verify-no-aggregates` ✓
  - `make verify-no-sqlite-dep` ✓
  - `make verify-dag-imports` ✓
  - `make verify-no-blocking` ✓
  - `make tide-lint` ✓
- Acceptance criteria checks all pass:
  - `test -f internal/webhook/v1alpha1/plan_webhook.go` ✓
  - `test -f internal/webhook/v1alpha1/wave_webhook.go` ✓
  - `grep -q "type PlanCustomValidator" internal/webhook/v1alpha1/plan_webhook.go` ✓
  - `grep -q "type WaveCustomValidator" internal/webhook/v1alpha1/wave_webhook.go` ✓
  - `grep -q "ValidateCreate" internal/webhook/v1alpha1/plan_webhook.go` ✓
  - `grep -q "ValidateCreate" internal/webhook/v1alpha1/wave_webhook.go` ✓
  - `grep -c "Phase 2" internal/webhook/v1alpha1/*.go` returns 11 + 10 = 21 ✓ (>= 4)
  - `grep -q "D-B1" internal/webhook/v1alpha1/wave_webhook.go` ✓
  - `grep -qE "D-B3|REQ-PLAN-01" internal/webhook/v1alpha1/plan_webhook.go` ✓
  - `grep -lrE "Hub\(\)|ConvertTo|ConvertFrom" api/v1alpha1/ internal/webhook/` returns 2 matches ✓
  - `grep -c "+kubebuilder:webhook:" internal/webhook/v1alpha1/*.go` returns 1+1=2 ✓ (>= 2)
  - `! test -f internal/webhook/v1alpha1/webhook_suite_test.go` ✓ (single-suite invariant enforced)
  - `test -f internal/controller/plan_webhook_test.go` ✓
  - `test -f internal/controller/wave_webhook_test.go` ✓
  - `grep -q "SetupPlanWebhookWithManager" internal/controller/suite_test.go` ✓
  - `grep -q "SetupWaveWebhookWithManager" internal/controller/suite_test.go` ✓
  - `grep -q "WebhookInstallOptions" internal/controller/suite_test.go` ✓
  - `grep -q "allows ValidateCreate" internal/controller/plan_webhook_test.go` ✓
  - `grep -q "Phase 1 no-op" internal/controller/wave_webhook_test.go` ✓
  - `grep -q "WaveIndex.*-1" internal/controller/wave_webhook_test.go` ✓
- Anti-checks pass:
  - `grep -rE "tide\.io|my\.domain" --include="*.go" internal/ api/` returns empty (D-A3) ✓

---
*Phase: 01-foundation-crds-pkg-dag-controller-scaffold*
*Completed: 2026-05-12*
