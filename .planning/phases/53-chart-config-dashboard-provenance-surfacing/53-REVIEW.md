---
phase: 53-chart-config-dashboard-provenance-surfacing
reviewed: 2026-07-21T00:00:00Z
depth: standard
files_reviewed: 54
files_reviewed_list:
  - charts/tide/templates/deployment.yaml
  - charts/tide/templates/verify-posture-configmap.yaml
  - charts/tide/values.yaml
  - cmd/dashboard/api/artifacts_test.go
  - cmd/dashboard/api/artifacts.go
  - cmd/dashboard/api/plans_test.go
  - cmd/dashboard/api/plans.go
  - cmd/dashboard/api/projects_test.go
  - cmd/dashboard/api/projects.go
  - cmd/dashboard/api/tasks_test.go
  - cmd/dashboard/api/tasks.go
  - cmd/manager/main.go
  - cmd/stub-subagent/main.go
  - cmd/tide-langgraph-verifier/verifier/__main__.py
  - cmd/tide-langgraph-verifier/verifier/envelope.py
  - cmd/tide-langgraph-verifier/verifier/tests/test_findings_artifact.py
  - dashboard/web/src/App.tsx
  - dashboard/web/src/components/ArtifactViewer.test.tsx
  - dashboard/web/src/components/ConditionBadge.test.tsx
  - dashboard/web/src/components/ConditionBadge.tsx
  - dashboard/web/src/components/StatusBadge.test.tsx
  - dashboard/web/src/components/StatusBadge.tsx
  - dashboard/web/src/components/TaskDetailDrawer.test.tsx
  - dashboard/web/src/components/TaskDetailDrawer.tsx
  - dashboard/web/src/lib/api.ts
  - dashboard/web/src/lib/tasks.ts
  - hack/helm/augment-tide-chart.sh
  - hack/helm/tide-values.yaml
  - hack/helm/verify-posture-configmap.yaml
  - internal/controller/artifact_push_test.go
  - internal/controller/artifact_push.go
  - internal/controller/boundary_push_test.go
  - internal/controller/dispatch_helpers_loop_policy_test.go
  - internal/controller/dispatch_helpers.go
  - internal/controller/dispatch_image_test.go
  - internal/controller/level_verify_unit_test.go
  - internal/controller/level_verify.go
  - internal/controller/plan_controller_test.go
  - internal/controller/plan_controller.go
  - internal/controller/plan_verify_dispatch_test.go
  - internal/controller/task_controller_test.go
  - internal/controller/task_controller.go
  - internal/controller/task_dispatch_traceparent_test.go
  - internal/controller/task_findings_push_test.go
  - internal/controller/task_verify_dispatch_test.go
  - internal/controller/verification_enabled_unit_test.go
  - internal/controller/wave_controller_test.go
  - pkg/dispatch/verify_defaults_test.go
  - pkg/dispatch/verify_defaults.go
  - test/integration/kind/suite_test.go
  - test/integration/kind/verifier_concurrency_test.go
  - test/integration/kind/verify_chart_config_test.go
  - test/integration/kind/verify_posture_sticky_test.go
findings:
  critical: 1
  warning: 6
  info: 6
  total: 13
status: issues_found
---

# Phase 53: Code Review Report

**Reviewed:** 2026-07-21
**Depth:** standard
**Files Reviewed:** 54
**Status:** issues_found

## Summary

Reviewed the full Phase 53 diff (`3ab81163..HEAD`): the chart-first verify-tier config (fail-closed `ParseVerifyLevelDefaults`, `verificationEnabledForLevel` AND-gates at the four Verifying-entry chokepoints, sticky `tide-verify-posture` marker via helm `lookup`), the verdict-final findings-push trigger (`maybeTriggerTaskFindingsPush` + `taskFindingsStageable` + `ensureTaskEntries`), the verifier-side `findings.json` writer, and the dashboard provenance surface (loop-summary wire fields, VerifyHalted/Verifying vocabulary, findings disclosure).

The core cross-checks the phase asked for mostly hold: the enablement gate fronts all four Verifying-*entry* seams (`handleJobCompletion` task_controller.go:1615, `handlePlannerJobCompletion` plan_controller.go:996, `reconcileWaveMaterialization` plan_controller.go:2262, `levelVerifyDecision` level_verify.go:154); the D-07 clamp cannot be reopened by the chart; the Go↔Python findings.json alignment invariant (`verdict_out is not None` ⇔ `LastEvaluation != nil` ⇔ file-on-disk) is pinned by tests on both sides, with a safe write ordering (findings.json before out.json before stub); the posture ConfigMap's `lookup` semantics were live-proven and the `and` short-circuit on an absent marker verified empirically; no secret/PII material rides the new wire fields (UIDs and controller-stamped messages only). Real defects remain, headed by a namespace-dropping fetch that breaks the new findings disclosure in every non-`default`-namespace install, wire fields that surface *authored* rather than *effective* loop policy (hiding or mis-rendering the exact chart-tier feature this phase ships), and an unbounded 5s requeue loop on VerifyHalted terminals.

## Critical Issues

### CR-01: Findings disclosure drops the task namespace — broken in every non-default-namespace install

**File:** `dashboard/web/src/components/TaskDetailDrawer.tsx:716`
**Confidence:** high
**Issue:** `FindingsContent` calls `fetchNodeArtifacts("task", task.name, task.projectName)` without the fourth `namespace` argument, even though `TaskDetailData.namespace` is populated (line 74) and rendered two sections above. The backend (`cmd/dashboard/api/artifacts.go:127-129`) defaults a missing `namespace` query param to `"default"`, so for any Project living outside the `default` namespace — the norm for TIDE, whose architecture is one Project per project-specific namespace — the artifacts GET 404s ("project not found"), `fetchNodeArtifacts` throws, and the disclosure permanently renders the "Couldn't fetch artifacts" error panel. The repo's own `dashboard/web/src/lib/tasks.ts:11-20` documents this exact footgun ("debug #14: omitting it let plans/tasks in a non-default namespace fail"), and the sibling consumer `ArtifactViewer.tsx:195` threads `namespace` correctly. `TaskDetailDrawer.test.tsx` mocks `fetchNodeArtifacts` and only asserts the first two arguments, so the omission is invisible to the suite.
**Fix:**
```tsx
const result = await fetchNodeArtifacts(
  "task",
  task.name,
  task.projectName,
  task.namespace,
);
```
(also add `task.namespace` to the `useCallback` dependency array, and extend the drawer test to assert the 4-arg call).

## Warnings

### WR-01: Loop-summary wire fields surface authored — not effective — MaxIterations, hiding/mis-rendering the chart-tier feature this phase ships

**File:** `cmd/dashboard/api/tasks.go:245`, `cmd/dashboard/api/plans.go:191`, `dashboard/web/src/App.tsx:714-718`, `dashboard/web/src/components/TaskDetailDrawer.tsx` (Iteration row)
**Confidence:** high
**Issue:** Both handlers emit `Spec.Verification.MaxIterations` raw. The effective value is `ResolveLoopPolicy(...)`'s output, which this same phase extends with the chart tier precisely for the authored-unset (`0`) case. Consequences:
1. **Task drawer:** a contract whose `maxIterations` is unset but chart-supplied (values default `task.maxIterations: 3`) renders `Iteration 2 of 0` — the `?? 0` fallback in the drawer's `MetaRow label="Iteration"` displays a denominator that is wrong whenever the chart tier (or the plan floor-to-1) is what actually governs the loop.
2. **Plan-check mirror never renders in common configs:** `plans.go` emits the trio with independent `omitempty`, and `App.tsx:714-718`'s eligibility is all-three-or-nothing. A plan whose contract comes from `Project.Spec.Verification.Plan` (project-scope authored default — `pl.Spec.Verification` is zero) or any plan-authored contract that leaves `maxIterations` unset (effective 1 via the compiled floor, or chart value) serializes no `verifyMaxIterations` key → the mirror silently never appears even after the loop ran and parked. The Go comment ("omits all three" only for a never-run loop) does not match the field-by-field omission behavior.
The dashboard process cannot resolve the chart tier (it never receives `--verify-levels-json`), so this is not fixable purely in the handlers.
**Fix:** persist the *effective* policy at loop engagement — e.g. stamp `policy.MaxIterations` into `LoopStatus` (a bounded scalar, LOOP-03-compatible) when the controller enters Verifying — and have both handlers read it from Status; alternatively emit `verifyMaxIterations` unconditionally inside the eligibility block (drop `omitempty`) and have the handlers walk at least the Plan→Project authored precedence via a shared resolver. At minimum, fix the App.tsx gate to not require `verifyMaxIterations`/`loopDecision` presence.

### WR-02: `handleVerifyHaltedTerminal` requeues every 5s forever when the entry can never be carried

**File:** `internal/controller/task_controller.go:594-608` (with `maybeTriggerTaskFindingsPush` at 2686-2712 and `triggerArtifactPush` guards at `internal/controller/artifact_push.go:328-343`)
**Confidence:** high
**Issue:** Any `!carried` result converts the terminal short-circuit into `RequeueAfter: 5s`. `carried` can be *permanently* unreachable:
- a VerifyHalted Task with nil `LoopStatus.LastEvaluation` (the T-53-25 poison-guard shape — verifier crashed pre-verdict): `maybeTriggerTaskFindingsPush` returns `(false, nil)` on every call, forever;
- a Project with no `spec.git` / empty `TidePushImage` / no run branch: `triggerArtifactPush` silently returns nil, no Job is ever created, `carried` never flips — and in the empty-image case it emits an Info log line *every 5 seconds per halted Task* ("skipping artifact push: TidePushImage not configured").
The T-53-23 "once carried, steady state — no churn" comment only holds for the happy path; the terminal state persists until `tide resume`, so these loops run indefinitely. `TestTaskFindingsPush_PoisonGuard_NilEvaluationNeverTriggers` itself confirms `carried=false` for the poison-guard case but nothing pins the requeue behavior.
**Fix:** distinguish "retryable" from "never eligible": return no requeue when `!taskFindingsStageable(task)` or when the project is git-less/push-image-less (have `maybeTriggerTaskFindingsPush` return a tri-state or a `retryable bool`), and use the established 30s parked-plan cadence rather than 5s for the transient busy-race window.

### WR-03: Posture marker with missing `data.posture` bricks every subsequent `helm upgrade` (nil-pointer template error)

**File:** `charts/tide/templates/deployment.yaml:41` (source: `hack/helm/augment-tide-chart.sh:255`)
**Confidence:** high (failure mode empirically reproduced)
**Issue:** `{{- $verifyMarkerOn := and $verifyMarker (eq $verifyMarker.data.posture "enabled") }}` — Go templates' `and` short-circuits on a falsy (absent-marker/empty-map) first argument, so the normal paths are safe. But when the `tide-verify-posture` ConfigMap *exists* without a `data` map (e.g. an operator hand-recreates it via `kubectl create configmap tide-verify-posture`, or strips `data` with a patch — plausible given T-53-10 explicitly anticipates operators hand-editing this object), the render fails hard: `nil pointer evaluating interface {}.posture` (reproduced against a chart fixture). Every subsequent `helm upgrade` of the release then fails with an opaque template error until the CM is fixed or deleted.
**Fix:**
```
{{- $verifyMarkerOn := eq (dig "data" "posture" "" ($verifyMarker | default dict)) "enabled" }}
```

### WR-04: `subagent.verify.posture=disabled` is documented as a tier kill-switch but does not override authored Project CR entries

**File:** `charts/tide/values.yaml:285-293`, `charts/tide/templates/verify-posture-configmap.yaml:20-24`, vs `internal/controller/dispatch_helpers.go:463-471`
**Confidence:** high (behavior), medium (whether behavior or docs should change)
**Issue:** The values doc says posture `"disabled"` "forces it OFF" / "forces the whole tier OFF regardless". Mechanically, `disabled` only suppresses the `--verify-levels-json` arg; `verificationEnabledForLevel` still returns true for any level with an authored `Project.Spec.Verification.<level>` entry ("authored always means ON"). An operator flipping `posture=disabled` to stop verifier spend will still get verifier dispatches (real LLM spend) for every Project carrying authored verification defaults — the exact spend-gate expectation T-53-14 addresses. The two contracts shipped in the same phase contradict each other.
**Fix:** either (a) amend the values.yaml/ConfigMap comments to state that authored Project-scope entries always outrank the chart posture, including `disabled`; or (b) if a hard kill-switch is intended, plumb the posture itself into the manager (e.g. a `--verify-posture=disabled` flag consulted before the authored tier). Recommend (a) plus a follow-up decision on (b).

### WR-05: A stageable Task whose envelope dir lacks findings.json permanently poisons every push (boundary pushes included) — no self-heal for image-skew or the tolerated write failure

**File:** `internal/controller/artifact_push.go:126-136` / `204-213`, `internal/controller/boundary_push.go:175-188`, `cmd/tide-push/main.go:1242-1252`, `cmd/tide-langgraph-verifier/verifier/__main__.py:227-231`
**Confidence:** medium-high
**Issue:** `taskFindingsStageable` is a Status-only proxy for findings.json presence. When the file is missing while `LastEvaluation` is recorded, tide-push hard-fails the ENTIRE cumulative push — and because `triggerBoundaryPush` also carries the staging map, *boundary pushes* (real level-boundary commits) fail too, halting the project's run-branch progression with no self-heal (the verifier Job is complete; findings.json will never be re-written). Reachable via: (a) a `tide-langgraph-verifier` image predating 53-11 driven by a Phase-53 controller — the chart lets operators pin `images.tideLanggraphVerifier.tag` independently, and any pre-existing cluster with verdict-final Tasks from Phase 51/52 runs is poisoned on controller upgrade with no migration guard; (b) the deliberately-unsoftened `write_findings` OSError swallow (`__main__.py:230-231`, pinned by `test_findings_write_oserror_never_masks_the_relay`) — a transient one-file write failure freezes the project's whole push pipeline until manual PVC surgery. The `taskFindingsStageable` doc itself narrates this fragility but the shipped state keeps a single-file single-point-of-poison with cross-level blast radius.
**Fix:** in tide-push, degrade a task entry whose dir exists but lacks findings.json to the same loud-skip path as an absent dir (the file's absence is diagnosable from the Status-vs-disk divergence; failing every *other* level's artifacts is disproportionate), or version-gate task-entry staging on evidence the writer ran (e.g. a `findingsWritten` marker in the stub relayed to Status). If fail-closed is retained deliberately, document the operator recovery path.

### WR-06: `subagent.verify.posture` accepts any string — typos silently fail OPEN to `auto`

**File:** `charts/tide/templates/deployment.yaml:39-49` (source: `hack/helm/augment-tide-chart.sh:253-267`)
**Confidence:** medium
**Issue:** The posture branch treats every value other than exact `"enabled"`/`"disabled"` as `auto`. A typo (`disable`, `Disabled`, `off`, boolean `true` stringified as `"true"`) silently resolves to `auto`, which on a fresh install (or any install with marker lineage) turns the spend-bearing verifier tier ON — the opposite of the operator's intent for the `disabled` family of typos. This is inconsistent with the fail-closed doctrine applied one layer down (`ParseVerifyLevelDefaults` rejects unknown level keys/values loudly at startup, T-53-03).
**Fix:** validate in-template:
```
{{- if not (has $verifyPosture (list "auto" "enabled" "disabled")) }}
{{- fail (printf "subagent.verify.posture must be auto|enabled|disabled, got %q" $verifyPosture) }}
{{- end }}
```

## Info

### IN-01: Stale "SURFACED CONTRADICTION" comment — the findings.json writer now exists

**File:** `internal/controller/artifact_push.go:111-125`
**Issue:** The `taskFindingsStageable` doc block still states "as of this plan, nothing in the verifier's write path ... ever writes a findings.json file" and cites `__main__.py:211-225`. Plan 53-11 landed `write_findings` (`__main__.py:227-231`, `envelope.py:212-233`), so the comment is now false and will misdirect the next reader (the residual risk is only the WR-05 skew/OSError cases).
**Fix:** rewrite the paragraph to reference the landed writer and the remaining divergence cases.

### IN-02: In-flight Verifying loops are not stopped by flipping the chart posture off; empty-VerifierImage park becomes permanent

**File:** `internal/controller/task_controller.go:802`, `internal/controller/plan_controller.go:202-204`
**Issue:** The D-04 gate fronts Verifying *entry* only. `checkVerifyingState` (Task) and `checkPlanVerifyingState` (Plan) re-dispatch verifiers ungated, so a `helm upgrade` to `posture=disabled` lets in-flight loops keep spending until their own exit (bounded by MaxIterations — acceptable transition semantics, but undocumented). Sharper edge: a Task parked in Verifying by the empty-`VerifierImage` benign-skip (task_controller.go:2237-2241) whose install then disables the tier stays parked forever with no exit arm.
**Fix:** document the in-flight-completes semantic in values.yaml; consider routing a Verifying task whose level is now disabled to the no-contract Succeeded path or an operator-visible condition.

### IN-03: Plan-check mirror is fetched once per node selection — never refreshed on SSE events

**File:** `dashboard/web/src/App.tsx:289-307`
**Issue:** `planCheckDetail` refetches only when `selectedNode`/`selectedNamespace` change; the SSE refresh triggers that re-drive `useTasks` do not re-run this effect, so an open plan panel shows a stale iteration/decision while the loop advances.
**Fix:** include the SSE refresh tick (the same trigger `useTasks` consumes) in the effect's dependency chain.

### IN-04: FindingsContent renders `files[0]` unfiltered

**File:** `dashboard/web/src/components/TaskDetailDrawer.tsx` (`FindingsContent`, tail of file)
**Issue:** The available-state branch takes the first file under `.tide/planning/task/<name>/` blind. Today only findings.json is staged for task-kind, but any future second staged file (ordering is server-side path order) could displace it silently.
**Fix:** `const findingsFile = (data.files ?? []).find((f) => f.name === "findings.json") ?? (data.files ?? [])[0];`

### IN-05: `verify_chart_config_test.go` hard-requires a `helm` binary in a plain go-test

**File:** `test/integration/kind/verify_chart_config_test.go:87-145`
**Issue:** `TestHelmDeploymentTemplateVerifyPostureInstallVsUpgrade` shells out to `helm` with no `exec.LookPath` skip-guard; on a dev machine without helm the *plain go-test* tier of the kind package fails (the same tier CLAUDE.md warns fails `make test-int` even when Ginkgo is green). CI pins helm, so this is environmental only.
**Fix:** `if _, err := exec.LookPath("helm"); err != nil { t.Skip("helm not on PATH") }` — or accept as a deliberate CI-contract test and document it.

### IN-06: Repeated no-op push Jobs re-created after TTL GC for long-lived VerifyHalted tasks

**File:** `internal/controller/task_controller.go:594-608`, `internal/controller/push_helpers.go:283` (TTL 300s)
**Issue:** After the carrying push Job is TTL-GC'd, any later reconcile of a still-VerifyHalted Task Get→NotFound→creates a fresh push Job (full clone + clean-tree skip) that stages nothing new. Consistent with the pre-existing parked-plan re-trigger pattern (37-06 Pitfall 8), so noted for awareness rather than as a defect; fixing WR-02's eligibility tri-state would also bound this.

---

_Reviewed: 2026-07-21_
_Reviewer: Claude (gsd-code-reviewer)_
_Depth: standard_
