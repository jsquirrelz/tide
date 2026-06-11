---
phase: 13-dispatch-image-resolution-provider-halt
reviewed: 2026-06-11T18:55:56Z
depth: standard
files_reviewed: 25
files_reviewed_list:
  - api/v1alpha1/shared_types.go
  - charts/tide/templates/deployment.yaml
  - charts/tide/values.yaml
  - cmd/manager/main.go
  - cmd/tide/resume.go
  - cmd/tide/resume_test.go
  - hack/helm/tide-values.yaml
  - hack/scripts/acceptance-v1.sh
  - internal/controller/billing_halt.go
  - internal/controller/billing_halt_test.go
  - internal/controller/billing_halt_regression_test.go
  - internal/controller/dispatch_helpers.go
  - internal/controller/dispatch_helpers_test.go
  - internal/controller/dispatch_image_test.go
  - internal/controller/milestone_controller.go
  - internal/controller/phase_controller.go
  - internal/controller/plan_controller.go
  - internal/controller/project_controller.go
  - internal/controller/task_controller.go
  - internal/credproxy/server.go
  - internal/credproxy/server_test.go
  - internal/dispatch/podjob/backend.go
  - test/integration/envtest/suite_test.go
  - test/integration/kind/suite_test.go
findings:
  critical: 1
  warning: 8
  info: 6
  total: 15
status: issues_found
---

# Phase 13: Code Review Report

**Reviewed:** 2026-06-11T18:55:56Z
**Depth:** standard
**Files Reviewed:** 25
**Status:** issues_found

## Narrative Findings (AI reviewer)

## Summary

Reviewed the Phase 13 implementation: resolveImage precedence chain wired at six dispatch sites with the main.go `--subagent-image` shim, the chart retemplating of `CLAUDE_SUBAGENT_IMAGE` from `subagent.defaults.image`, and the full BillingHalt stack (credproxy ModifyResponse classifier + atomic latch, reconciler envelope backstop at five completion sites, dispatch-entry holds at five levels, `tide resume` clearing).

**Verified clean against the requested attack surfaces:**

- **Halt-bypass audit:** all five LLM-dispatching sites carry the hold, positioned before pool acquire and Job create — milestone (`milestone_controller.go:309`), phase (`phase_controller.go:298`), plan (`plan_controller.go:309`), project (`project_controller.go:949`), task (`task_controller.go:341`). The Wave reconciler is observational (no Job creation), and the non-LLM Job spawn sites (boundary push, wave-integration push, reporter) are ungated by design — they burn no provider credits. No bypass found on the production reconcile paths (but see WR-08 for the fixture-only `PodJobBackend.Run` and WR-01 for test coverage that doesn't prove the planner-level gates).
- **Latch race:** `Proxy.billingHalted` is `atomic.Bool`; concurrent requests in flight at the first 400 each reach the upstream and get the genuine 400, then the latch short-circuits all later requests. `Store(true)` is idempotent; the latch check sits after Verify and the route allowlist so unauthenticated callers cannot probe halt state. No race defect.
- **Chart tag-defaulting regex:** `regexMatch ":[^/]+$"` correctly distinguishes `localhost:5000/img` (registry port, no tag → gets `:appVersion` appended) from `repo:tag` and `repo@sha256:...` (pass through verbatim). The regex logic is correct; the empty-value edge is not (WR-04).
- **Image injection surface:** `spec.subagent.image` / `levels.<level>.image` only select the container image of a Job that runs in the project namespace with the per-task signed token; the real API key stays in the credproxy sidecar behind the route allowlist. Acceptable for v1's trust model; see IN-05 for the missing CRD-level format validation.

**What is not clean:** one crash-class nil dereference on the milestone dispatch path, four vacuous regression tests that do not actually exercise the new planner-level holds, a project-wide-halt false-positive/DoS class in the substring classifier, a resume/re-halt loop between the never-unlatching sidecar and the envelope backstop, and the unimplemented empty-image config-error contract.

## Critical Issues

### CR-01: Nil-Project dereference crashes MilestoneReconciler dispatch path

**File:** `internal/controller/milestone_controller.go:370` (and `:394`)
**Issue:** In `reconcilePlannerDispatch`, Step 4 resolves `project` best-effort — it stays `nil` when `ms.Spec.ProjectRef == ""` or when the `Get` fails transiently (`milestone_controller.go:325-331`). The code then dereferences it unguarded twice:

- `:370` — `if project.Spec.ProviderSecretRef != ""` (panics on nil)
- `:394` — `ProjectUID: string(project.UID)` (panics on nil)

A flaky apiserver moment or a hand-applied Milestone with an empty `projectRef` panics the reconcile goroutine. The sibling `PhaseReconciler` guards both sites (`phase_controller.go:339` `project != nil &&`, `:346-349` guarded `projectUID`), and `PlanReconciler` refuses dispatch entirely on nil project (cascade-7 guard, `plan_controller.go:332-345`) — the Milestone controller is the only unguarded one. Git blame shows the deref predates Phase 13 (2026-05-20), but this phase inserted the explicitly nil-tolerant `resolveImage(project, ...)` at `:390` directly between the two nil-unsafe derefs, so the code now half-implements a nil-project contract it cannot survive.
**Fix:**
```go
// Step 4: Resolve project; refuse dispatch when unresolvable (mirrors
// plan_controller.go cascade-7 guard — BuildJobSpec drops the provider
// Secret on nil Project and the planner pod CrashLoopBackOffs anyway).
if project == nil {
    if ms.Spec.ProjectRef == "" {
        logger.Info("refusing milestone-planner dispatch: spec.projectRef is empty")
        return ctrl.Result{}, nil
    }
    return ctrl.Result{RequeueAfter: 1 * time.Second}, nil
}
```
(then the existing `:370`/`:394` derefs are safe; alternatively guard both sites the way `phase_controller.go` does).

## Warnings

### WR-01: BillingHalt planner-level hold regression tests are vacuous — they pass with the gate deleted

**File:** `internal/controller/billing_halt_regression_test.go:702-755` (used by specs at `:115-318`)
**Issue:** `newBHMilestoneReconciler`, `newBHPhaseReconciler`, `newBHPlanReconciler`, and `newBHProjectReconciler` do not set `Dispatcher`. Every one of those reconcilers gates its entire dispatch body on `r.Dispatcher != nil` (`milestone_controller.go:175`, `phase_controller.go:165`, `plan_controller.go:167`, `project_controller.go:234`), so the four "no planner Job created while BillingHalt=True" specs never reach `checkBillingHalt` at all — no Job would be created with or without the hold. The helper comment ("Uses testSigningKey so the dispatch guard passes") describes the ProjectReconciler-only `len(SigningKey) > 0` guard and misses the `Dispatcher != nil` outer gate. Deleting all four planner-level `checkBillingHalt` calls leaves this suite green; only the task-level legs (which use `newTaskReconciler` with a wired Dispatcher and assert the 30s requeue) genuinely cover HALT-01. This is false confidence on the exact regression this phase exists to prevent.
**Fix:** Wire `Dispatcher: &stubDispatcher{}` (plus `PlannerPool`/`EnvReader` as the dispatch path requires) into all four `newBH*Reconciler` helpers, and strengthen the assertions to prove the gate fired — e.g. assert `result.RequeueAfter == 30*time.Second` as the task-level Leg 2 does. Add a control spec (clear the halt, re-reconcile, assert a Job IS created) so the test fails if the reconciler never enters the dispatch path.

### WR-02: Free-text substring billing classifier lets any failing subagent halt the whole project

**File:** `internal/controller/billing_halt.go:58-63` (consumed at `task_controller.go:831`, `milestone_controller.go:514`, `phase_controller.go:443`, `plan_controller.go:527`, `project_controller.go:1122`)
**Issue:** `isBillingFailureReason` matches the case-insensitive substring `"credit balance"` anywhere in `EnvelopeOut.Reason`. That channel is built from the subagent's stderr ("claude exit N: <stderr>") — content fully controlled by an LLM-driven process executing arbitrary repository code. Any failing task whose stderr happens to contain the phrase — a project that legitimately implements billing features, a test fixture quoting the Anthropic error, log output echoing a string literal, or a deliberately adversarial print — stamps `BillingHalt=True` and parks **all new dispatch project-wide** until an operator manually runs `tide resume`. Blast radius is asymmetric to the credproxy-side classifier (`isCreditExhaustion`), whose latch is confined to one pod: a single task's text output escalates to a project-wide availability halt. Conservative-substring matching for API rewording resilience (T-13-06) is reasonable at the HTTP boundary; it is too loose at the envelope boundary.
**Fix:** Require corroborating structure before halting on the substring channel, e.g. (a) have the anthropic harness emit the structured sentinel the code already reserves — `billing-halt:credit-balance-too-low` — when it observes the `X-Tide-Billing-Halt` response header or a 400 status from the proxy, and (b) narrow the free-text fallback to the API-error shape (`strings.Contains(lower, "credit balance") && strings.Contains(lower, "400")` at minimum, ideally anchored on the "API Error: 400" prefix the harness writes). Keep the prefix match as the primary trigger.

### WR-03: Credproxy latch never unlatches — a surviving latched session re-stamps BillingHalt right after `tide resume`

**File:** `internal/credproxy/server.go:215-223` interacting with `internal/controller/billing_halt.go:91-108` and `cmd/tide/resume.go:90-104`
**Issue:** `billingHalted` is latched for the lifetime of the sidecar process and there is no clear path (D-06 clears only the Project condition). The short-circuit body is deliberately crafted to contain "credit balance" so the stderr→envelope channel still classifies. Sequence: credits dry out → pod A's sidecar latches → operator refills credits and runs `tide resume` (Project condition removed) → pod A's still-running session gets the *synthetic* local 400, exits non-zero with "credit balance" in stderr → the reconciler backstop classifies it and re-stamps `BillingHalt=True` on the now-funded project. The operator's resume is silently undone by a straggler; nothing distinguishes the synthetic halt body from a genuine fresh dry-out, so the project stays parked until a second (or Nth) resume after all latched pods drain. D-05 says in-flight sessions fail naturally — but their failure evidence is manufactured by the latch, not by the provider.
**Fix:** Make the synthetic short-circuit distinguishable and excluded from re-stamping: e.g. body message "TIDE billing halt active (cached)…" without re-triggering wording, plus have the harness translate the `X-Tide-Billing-Halt: true` header into the structured `billing-halt:cached` sentinel which `setBillingHaltIfNeeded` treats as no-op when the Project currently has no BillingHalt condition (i.e. only the genuine upstream 400 may *initiate* a halt). At minimum, document in the `tide resume` output that in-flight sessions from the halted period may re-trip the condition.

### WR-04: Chart renders a garbage image ref (`":<appVersion>"`) when `subagent.defaults.image` is empty, and crashes rendering when the block is overridden away

**File:** `charts/tide/templates/deployment.yaml:59-65` (mirrored in `hack/helm/tide-values.yaml`)
**Issue:** `{{- $img := .Values.subagent.defaults.image }}` with `--set subagent.defaults.image=""` takes the bare-ref branch and renders `CLAUDE_SUBAGENT_IMAGE: ":1.0.0"` — a syntactically invalid image ref that is nonetheless non-empty, so the manager's `envOrDefault` fallback never fires and every dispatched Job fails pod creation with `InvalidImageName` at runtime, far from the misconfiguration. Worse, a user values file that replaces the `subagent:` block without `defaults.image` (e.g. only `subagent.levels`) passes `nil` to `regexMatch`, failing the render with a cryptic sprig type error instead of a named message. The phase brief's own contract is "empty image is a config error" — the chart should enforce that at render time, which is the cheapest place.
**Fix:**
```yaml
{{- $img := required "subagent.defaults.image must be a non-empty image ref (Phase 13 D-01)" .Values.subagent.defaults.image }}
```

### WR-05: resolveImage's documented empty-image contract is unimplemented at all six dispatch sites

**File:** `internal/controller/dispatch_helpers.go:254-285` (call sites: `milestone_controller.go:390`, `phase_controller.go:359`, `plan_controller.go:391`, `project_controller.go:1004`, `task_controller.go:641`, `task_controller.go:1063`)
**Issue:** The doc comment states "An empty return means no image was configured; callers must surface this as a config error rather than dispatching a Job with an empty image field" — and `ProviderDefaults.Image` carries the same contract ("caller is responsible for surfacing this at Job creation time"). No caller checks. An empty resolved image flows into `podjob.BuildJobSpec` and surfaces only as the apiserver rejecting the Job create (`spec.template.spec.containers[].image: Required value`), which the reconcilers return as a generic error → exponential-backoff requeue loop with no status condition and no Event. In production the main.go `envOrDefault` chain makes "" unlikely, but the chart edge in WR-04 produces an equally undiagnosable invalid ref, and direct/test wiring with zero-value `ProviderDefaults` hits it immediately. A contract that exists only in a comment is a bug factory.
**Fix:** At each dispatch site (or centrally in `BuildJobSpec`'s callers), guard:
```go
img := resolveImage(project, "milestone", r.HelmProviderDefaults)
if img == "" {
    return r.patchMilestoneFailed(ctx, ms, "SubagentImageUnresolved",
        "no subagent image configured (set spec.subagent.image or Helm subagent.defaults.image)")
}
```
(or a parked condition + requeue if config-fix-without-recreate is preferred).

### WR-06: `tide resume` clears BillingHalt with an unlocked merge patch that replaces the whole conditions array

**File:** `cmd/tide/resume.go:90-104`
**Issue:** The clear path re-Gets the Project, calls `meta.RemoveStatusCondition`, and patches with `client.MergeFrom` (no optimistic lock). A JSON merge patch on `status.conditions` replaces the entire array, so any condition written by a reconciler between the CLI's Get and Patch (e.g. `Succeeded`, `BoundaryPushed`, `AuthoringPlanner`, or a fresh `BillingHalt` from a straggler envelope) is silently clobbered. Reconcilers get away with the same pattern because the per-object workqueue serializes them; `tide resume` runs externally and races by construction.
**Fix:** Use `client.MergeFromWithOptions(proj.DeepCopy(), client.MergeFromWithOptimisticLock{})` and retry on conflict (`retry.RetryOnConflict` with re-Get), so a concurrent condition write produces a 409 + retry instead of a lost update.

### WR-07: credproxy ModifyResponse swallows body-read errors and delivers a corrupted empty 400

**File:** `internal/credproxy/server.go:175-181`
**Issue:** On a 400 response, if `io.ReadAll(resp.Body)` fails partway, the code closes the body, replaces it with an **empty** reader, and returns `nil` — the subagent receives a 400 with a zero-length body where the upstream sent content, and the error is never surfaced (no log, no 502). The comment cites "always restore resp.Body … even on error paths," but restoring an empty body is not restoration; it silently corrupts the response and also defeats the classifier (a truncated billing body never latches). `httputil.ReverseProxy` already has the correct channel for this: returning the error from `ModifyResponse` invokes the ErrorHandler (502), which is honest about the failure.
**Fix:**
```go
body, err := io.ReadAll(resp.Body)
resp.Body.Close()
if err != nil {
    return fmt.Errorf("credproxy: read upstream 400 body: %w", err)
}
resp.Body = io.NopCloser(bytes.NewReader(body))
```

### WR-08: Plan controller fails terminally on a transient envelope-read error — asymmetric with milestone/phase and it skips the billing backstop

**File:** `internal/controller/plan_controller.go:468-484`
**Issue:** `handlePlannerJobCompletion` marks the Plan `Status.Phase=Failed` (`EnvelopeReadFailed`) on any `EnvReader.ReadOut` error. The milestone and phase controllers treat the identical condition as transient (log + fall back to children-based succession / requeue — `milestone_controller.go:471-481`, `phase_controller.go:411-419`), because termination-message propagation lags Job-terminal observation. Two consequences for this phase specifically: (a) a planner that died of credit exhaustion whose envelope is momentarily unreadable goes terminal `Failed` *without* the billing backstop ever classifying `out.Reason` — the new `setBillingHaltIfNeeded` call at `:526-530` is unreachable on this path, so the halt depends on some other level catching a later failure; (b) `Failed` is permanent (terminal short-circuit at `:261`) and recoverable only via `tide resume --retry-failed`, contradicting the D-05 park-not-fail posture for billing-class outages. Pre-existing asymmetry, but the backstop wiring this phase added made it consequential.
**Fix:** Mirror the milestone/phase pattern: on read error, log non-fatally and requeue (5s) instead of patching `Failed`, falling back to children-based succession; reserve `EnvelopeReadFailed` terminality for a bounded retry count if livelock is a concern.

## Info

### IN-01: Stale "until 13-03" shim comments in main.go — the chart change already landed in this phase

**File:** `cmd/manager/main.go:151-156, 202-207`
**Issue:** The `--subagent-image` flag help and the shim comment both say the chart is "still passing --subagent-image until plan 13-03 drops the arg." The arg was dropped in this very phase (`charts/tide/templates/deployment.yaml` no longer renders it), so the text now describes a state that no longer exists and will mislead the next reader about who sets the flag (answer: nobody, in chart-driven installs).
**Fix:** Reword to "retained as an operator escape hatch / test override; the chart no longer passes it (13-03)."

### IN-02: Dead `SubagentImage` fields retained on five reconcilers

**File:** `internal/controller/milestone_controller.go:83`, `phase_controller.go:75`, `plan_controller.go:80`, `project_controller.go:181`, `task_controller.go:90` (still populated in `cmd/manager/main.go:376, 406, 430, 453, 491`)
**Issue:** All five are documented as "dead since Phase 13 … ignored at dispatch" yet remain declared and wired. Dead-but-wired config fields invite the next contributor to read or "fix" them.
**Fix:** Remove the fields and their main.go/test assignments in a follow-up sweep once the legacy test wiring is migrated to `HelmProviderDefaults`.

### IN-03: Hand-rolled substring scan instead of `strings.Contains`

**File:** `internal/controller/billing_halt_test.go:203-214`
**Issue:** `containsStr` reimplements `strings.Contains` with a nested closure "so billing_halt_test.go doesn't need to import strings" — importing `strings` is strictly simpler and removes ~10 lines of logic that itself needs review.
**Fix:** `import "strings"` and use `strings.Contains`.

### IN-04: `buildTestProxy`'s second return value is always nil

**File:** `internal/credproxy/server_test.go:29-49`
**Issue:** `captured` is assigned inside the upstream handler closure but returned by value at construction time — it is always nil for callers (every call site discards it with `_`). Dead helper output; the one test that needs captured headers builds its own upstream instead.
**Fix:** Drop the second return value (and the `_ = captured` line).

### IN-05: No CRD-level validation on `spec.subagent.image` / `levels.<level>.image`

**File:** `internal/controller/dispatch_helpers.go:263-285` (consuming the unvalidated CRD fields)
**Issue:** The image fields accept any string; whitespace, `--`-prefixed, or otherwise malformed values surface only as Job-create rejections or `InvalidImageName` pod failures in the project namespace. Also worth documenting explicitly: anyone with Project update RBAC selects the container image that receives the workspace PVC and the per-dispatch signed token (the real key stays behind the credproxy allowlist) — by design, but it should be a stated trust boundary.
**Fix:** Add a CEL `x-kubernetes-validations` pattern (e.g. reject whitespace and require an OCI-ref-shaped string) per the project's CEL-over-webhook convention, and note the RBAC implication in the Project CRD field docs.

### IN-06: `PodJobBackend.Run` bypasses the billing-halt gate and ships the raw signing key as the bearer token

**File:** `internal/dispatch/podjob/backend.go:216-329` (`:277` `SignedToken: string(b.SigningKey)`)
**Issue:** `Run` is documented fixture-only (production reconcilers call `BuildJobSpec` directly), but it is wired as the live `Dispatcher` on every reconciler. It performs no `checkBillingHalt`, sets `SecretUID: string(project.UID)` (wrong UID), and places the raw HMAC signing key into the Job env as the signed token — any future caller that "just uses the Dispatcher interface" inherits a halt bypass and a key-disclosure path into pod specs. The inline image-precedence walk (`:262-271`) does correctly mirror `resolveImage`.
**Fix:** Either mint a real token + add the halt check in `Run`, or make the fixture-only contract structural (panic/error in `Run` unless an explicit `AllowFixtureRun` field is set).

---

_Reviewed: 2026-06-11T18:55:56Z_
_Reviewer: Claude (gsd-code-reviewer)_
_Depth: standard_
