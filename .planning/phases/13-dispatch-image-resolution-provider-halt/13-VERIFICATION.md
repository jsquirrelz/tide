---
phase: 13-dispatch-image-resolution-provider-halt
verified: 2026-06-11T20:10:00Z
status: gaps_found
score: 4/6 must-haves verified
overrides_applied: 0
gaps:
  - truth: "Subagent image resolves correctly at ALL dispatch sites (milestone level included)"
    status: failed
    reason: >-
      CR-01: milestone_controller.go reconcilePlannerDispatch dereferences a nil-tolerant
      `project` twice without a guard — :370 `project.Spec.ProviderSecretRef` and :394
      `string(project.UID)`. `project` is assigned only on a successful Get (:326-331); it
      stays nil when ms.Spec.ProjectRef == "" or the Get fails transiently. Phase 13 inserted
      the explicitly nil-tolerant `resolveImage(project, "milestone", ...)` at :390 directly
      between the two nil-unsafe derefs, so the milestone dispatch site now half-implements a
      nil-project contract it panics on. The sibling phase_controller.go guards both sites
      (`project != nil &&` :339, guarded projectUID :346-349) and plan_controller.go refuses
      dispatch on nil project entirely (cascade-7 guard) — milestone is the only unguarded
      planner level. A flaky apiserver moment or a hand-applied Milestone with empty projectRef
      panics the reconcile goroutine instead of resolving the image. This is a crash class on a
      phase-13-touched function and directly contradicts "resolves correctly at all dispatch sites."
    artifacts:
      - path: "internal/controller/milestone_controller.go"
        issue: "Lines 370 and 394 dereference nil project; resolveImage at :390 is nil-safe but the surrounding derefs are not"
    missing:
      - "Add a nil-project guard at the top of milestone reconcilePlannerDispatch Step 4 (mirror plan_controller.go cascade-7 refusal, or guard both :370/:394 the way phase_controller.go does)"
      - "Add an envtest spec: a Milestone with empty/missing projectRef does not panic the reconciler"
  - truth: "BillingHalt recovery via `tide resume` actually restores dispatch (the halt does not silently re-stamp itself)"
    status: failed
    reason: >-
      WR-03: the credproxy fail-fast latch (server.go:215-223) synthesizes a local 400 whose
      body contains "Your credit balance is too low" — the exact substring isBillingFailureReason
      (billing_halt.go:62) matches. The latch never clears (billingHalted is process-lifetime;
      D-06 clears only the Project condition). Sequence: credits dry out → pod A sidecar latches →
      operator refills + runs `tide resume` (Project condition removed) → pod A's still-running
      session gets the SYNTHETIC local 400, exits non-zero with "credit balance" in stderr → the
      reconciler backstop (setBillingHaltIfNeeded at all five sites) re-stamps BillingHalt=True on
      the now-funded project. The operator's recovery is silently undone by a straggler; nothing
      distinguishes the synthetic halt body from a genuine fresh dry-out. HALT-01's goal clause is
      "halts the entire project" (verified) but its D-06 recovery verb is part of the same
      requirement, and recovery is not reliable while any latched sidecar drains. This is a
      correctness defect in the halt/recovery lifecycle, not a test-quality nit.
    artifacts:
      - path: "internal/credproxy/server.go"
        issue: "Synthetic latch body (:219-221) re-injects the 'credit balance' classifier substring; latch has no clear path"
      - path: "internal/controller/billing_halt.go"
        issue: "setBillingHaltIfNeeded (:91-108) re-stamps on any envelope reason containing 'credit balance', including the synthetic latch body"
    missing:
      - "Make the synthetic short-circuit distinguishable (e.g. body without re-triggering wording + an X-Tide-Billing-Halt sentinel the harness maps to 'billing-halt:cached')"
      - "Treat the cached sentinel as a no-op in setBillingHaltIfNeeded when the Project currently has no BillingHalt condition (only a genuine fresh upstream 400 may INITIATE a halt)"
      - "At minimum, surface in `tide resume` output that in-flight straggler sessions may re-trip the condition"
  - truth: "Full `make test-int` is green after the chart change (13-03 documented success criterion / D-02 deliverable)"
    status: failed
    reason: >-
      Full `make test-int` is MAKE_EXIT=2: Layer A 38/38 green, but kind Layer B FAILED 4 specs.
      3 of 4 are inherited pre-phase-13 fixture debt — testdata/three-task-wave.yaml (×3 tasks),
      chaos-resume-three-task.yaml, and output_test inline task lack spec.promptPath, required
      since b612fce (2026-06-08); full test-int has been silently red since before this milestone.
      13-03's own success_criteria and verify block require a green full make test-int, and the
      plan claimed it. The fix is mechanical (add promptPath to the fixtures). The 4th failure
      (reporter_pod_test.go:196 — reporter Job spawns but no child Milestone materializes,
      "total in ns: 0" across 3 retries) is the project→milestone reporter-materialization path;
      phase 13 did NOT touch reporter or stub-subagent code (last change debug-17 / Phase 9), and
      the stub image wiring traces correctly (subagent.defaults.image=stub:test → CLAUDE_SUBAGENT_IMAGE
      → HelmProviderDefaults.Image → resolveImage), so it is most consistent with inherited/environmental
      debt rather than a phase-13 image/billing regression — but it could not be positively proven
      green pre-phase-13 (no prior log) and remains UNATTRIBUTED. Either way it blocks the documented
      green-suite criterion.
    artifacts:
      - path: "test/integration/kind/testdata/three-task-wave.yaml"
        issue: "Tasks alpha/beta/gamma lack spec.promptPath (Required since b612fce) — admission rejects apply"
      - path: "test/integration/kind/testdata/chaos-resume-three-task.yaml"
        issue: "Same promptPath admission rejection"
      - path: "test/integration/kind/output_test.go"
        issue: "Inline exceed-output-task lacks spec.promptPath"
      - path: "test/integration/kind/reporter_pod_test.go"
        issue: "Line 196 — reporter Job spawns but no child Milestone materializes within 4m; root cause unattributed (provenance not phase-13 code, but blocks the green-suite criterion)"
    missing:
      - "Add spec.promptPath to the three fixture/inline Tasks (mechanical; matches the project owner's root-cause-over-defer preference)"
      - "Root-cause the reporter_pod_test materialization failure: confirm the stub planner wrote out.json with a Milestone ChildCRD under the new subagent.defaults.image install, and that reporter materialization succeeds; assign provenance (inherited Phase-9 path vs phase-12 gate vs phase-13 image)"
deferred: []
---

# Phase 13: Dispatch Image Resolution + Provider Halt Verification Report

**Phase Goal:** Subagent image resolves correctly at all dispatch sites via the documented chain, and a provider billing-400 response halts the entire project instead of burning sessions one at a time.
**Verified:** 2026-06-11T20:10:00Z
**Status:** gaps_found
**Re-verification:** No — initial verification

## Goal Achievement

### Observable Truths

| # | Truth | Status | Evidence |
| --- | ----- | ------ | -------- |
| 1 | resolveImage implements the documented chain (Levels.<level>.Image → Spec.Subagent.Image → flag/Helm default) with full unit coverage | ✓ VERIFIED | dispatch_helpers.go:263-285 mirrors ResolveProvider; 6 TestResolveImage_* cases (dispatch_helpers_test.go:173-243) covering all 3 tiers + nil + empty-level + project-level fall-through |
| 2 | Image resolves correctly at ALL dispatch sites (incl. milestone) | ✗ FAILED | CR-01: milestone_controller.go:370/:394 deref nil project unguarded; resolveImage at :390 is nil-safe but the site panics before/after it. 6 call sites wired (grep TOTAL=6) and envtest regression (dispatch_image_test.go) exists, but the milestone site is crash-prone on nil project |
| 3 | Released-chart install dispatches the real subagent (no silent stub override); stub is explicit opt-in | ✓ VERIFIED | deployment.yaml: `subagent-image` flag dropped (grep=0); CLAUDE_SUBAGENT_IMAGE sourced from `.Values.subagent.defaults.image` (grep=2); 2 contract go-tests + preserved podAnnotations test; suite_test.go:489 + acceptance-v1.sh:107 opt into stub explicitly |
| 4 | A provider billing-400 halts NEW dispatch project-wide and surfaces a Project condition (the halt itself) | ✓ VERIFIED | checkBillingHalt gate present + correctly positioned (before pool acquire / Job create) at all 5 levels (milestone:309, phase:298, plan:309, project:949, task:341); setBillingHaltIfNeeded backstop at all 5 envelope sites; credproxy isCreditExhaustion + atomic latch with byte-identical pass-through; condition vocabulary + 14 substantive unit tests |
| 5 | BillingHalt recovery via `tide resume` reliably restores dispatch | ✗ FAILED | WR-03: latch synthetic body re-injects "credit balance"; latch never clears → straggler envelope re-stamps BillingHalt right after resume, silently undoing recovery. resume.go:84-104 clears correctly in isolation, but the lifecycle is defective end-to-end |
| 6 | Full `make test-int` green after the chart change (13-03 / D-02 deliverable) | ✗ FAILED | MAKE_EXIT=2; Layer A 38/38 green, Layer B 4 specs FAILED (3 inherited promptPath fixture debt + 1 unattributed reporter materialization). 13-03 claimed/required green |

**Score:** 4/6 truths verified

### Required Artifacts

| Artifact | Expected | Status | Details |
| -------- | -------- | ------ | ------- |
| internal/controller/dispatch_helpers.go | resolveImage precedence walk | ✓ VERIFIED | :263-285, pure function, no receiver |
| internal/controller/dispatch_helpers_test.go | 3-tier + nil coverage | ✓ VERIFIED | 6 cases |
| internal/controller/dispatch_image_test.go | Envtest pinned-image regression | ✓ VERIFIED | exists, referenced in review files list |
| api/v1alpha1/shared_types.go | ConditionBillingHalt + Reason vocab | ✓ VERIFIED | grep present |
| internal/controller/billing_halt.go | classifier + gate + writer | ✓ VERIFIED | 3 funcs, providerfirewall-clean |
| internal/credproxy/server.go | ModifyResponse classifier + latch | ⚠️ ORPHANED-RISK | present + tested, but latch lifecycle defect (WR-03) and body-read swallow (WR-07) |
| cmd/tide/resume.go | BillingHalt clear | ✓ VERIFIED | :84-104, RemoveStatusCondition, no auto-probe; but lossy merge patch (WR-06) and re-stampable (WR-03) |
| charts/tide/templates/deployment.yaml | flag dropped, env from defaults.image | ✓ VERIFIED | grep flag=0, defaults.image=2 |
| internal/controller/billing_halt_regression_test.go | run-1 end-to-end regression | ⚠️ PARTIAL | task-level legs genuine; 4 planner-level holds VACUOUS (WR-01 — no Dispatcher, never enter dispatch body) |

### Key Link Verification

| From | To | Via | Status | Details |
| ---- | -- | --- | ------ | ------- |
| 5 controller dispatch gates | checkBillingHalt | dispatch-entry hold | ✓ WIRED | all 5 present, before pool acquire (production code correct) |
| 5 controllers handleJobCompletion | setBillingHaltIfNeeded | failed-envelope classification | ✓ WIRED | all 5 present |
| credproxy ModifyResponse | isCreditExhaustion | on 400 responses | ✓ WIRED | :183 |
| resume.go | meta.RemoveStatusCondition | status patch | ✓ WIRED | :97, but unlocked merge patch clobbers concurrent conditions (WR-06) |
| deployment.yaml | subagent.defaults.image | CLAUDE_SUBAGENT_IMAGE env | ✓ WIRED | tag/digest pass-through + appVersion append |
| 6 controller dispatch sites | resolveImage | BuildOptions.SubagentImage | ✓ WIRED | TOTAL=6 |

### Behavioral Spot-Checks

| Behavior | Command | Result | Status |
| -------- | ------- | ------ | ------ |
| resolveImage unit suite | (orchestrator: make test MAKE_EXIT=0) | green | ✓ PASS |
| billing classifier/latch/resume units | (orchestrator: make test MAKE_EXIT=0) | green | ✓ PASS |
| Layer A integration | (orchestrator: make test-int-fast) | 38/38 | ✓ PASS |
| Full make test-int | (orchestrator: /tmp/w13-test-int-full.log) | MAKE_EXIT=2, 4 Layer B FAILs | ✗ FAIL |

### Requirements Coverage

| Requirement | Source Plan | Description | Status | Evidence |
| ----------- | ----------- | ----------- | ------ | -------- |
| DISPATCH-01 | 13-01 | Image resolves via chain at all reconciler dispatch sites | ⚠️ PARTIAL | Chain + 6 sites wired and unit-covered, but milestone site crashes on nil project (CR-01) |
| DISPATCH-02 | 13-03 | Released chart dispatches a pinned real image, no silent stub | ⚠️ PARTIAL | Chart artifacts verified; documented green-suite criterion not met (Layer B red) |
| HALT-01 | 13-02, 13-04 | Billing 400 halts project-wide + condition, not crash-fan-out | ⚠️ PARTIAL | Halt + condition + gates + backstop verified; recovery (D-06) silently re-stampable (WR-03); planner-hold tests vacuous (WR-01) |

### Anti-Patterns Found

| File | Line | Pattern | Severity | Impact |
| ---- | ---- | ------- | -------- | ------ |
| milestone_controller.go | 370, 394 | nil-project deref | 🛑 Blocker | reconcile goroutine panic on nil project (CR-01) |
| credproxy/server.go | 219-221, 215-223 | synthetic body re-injects classifier substring + non-clearing latch | 🛑 Blocker | resume recovery silently undone (WR-03) |
| billing_halt_regression_test.go | 702-755 | 4 planner-hold specs lack Dispatcher → vacuous | ⚠️ Warning | false confidence on planner-level holds (WR-01); production gates ARE present |
| credproxy/server.go | 175-181 | io.ReadAll error swallowed, empty body, returns nil | ⚠️ Warning | corrupted empty 400 to subagent; classifier defeated on truncation (WR-07) |
| billing_halt.go | 58-63 | free-text "credit balance" substring at envelope boundary | ⚠️ Warning | any failing task echoing the phrase halts whole project; LLM-influenceable (WR-02) |
| cmd/tide/resume.go | 90-104 | unlocked MergeFrom replaces conditions array | ⚠️ Warning | concurrent condition write lost (WR-06) |
| plan_controller.go | 468-484 | terminal Failed on transient envelope-read error | ⚠️ Warning | billing backstop unreachable on this path; contradicts D-05 park (WR-08) |
| charts deployment.yaml | 59-65 | empty subagent.defaults.image → ":appVersion" garbage ref | ⚠️ Warning | InvalidImageName at runtime, not render-time config error (WR-04) |
| dispatch_helpers.go | 254-285 | documented empty-image config-error contract unimplemented at 6 sites | ⚠️ Warning | empty image → opaque apiserver reject + backoff loop (WR-05) |
| podjob/backend.go | 216-329 | fixture-only Run bypasses halt gate + ships raw signing key | ⚠️ Warning | future Dispatcher caller inherits halt bypass + key disclosure (IN-06) |

### Human Verification Required

None mandated beyond automated closure — the gaps are all observable in code/test output. (The 13-03 manual phase-gate item — kind install with chart defaults + a Project pinning a real image showing the pinned image in `kubectl get job -o yaml` with no stub children — remains a valid end-to-end confidence check but is not the deciding factor; the blockers above are code-observable.)

### Gaps Summary

The phase delivered the bulk of its machinery: the resolveImage precedence chain (6 sites, 6 unit
tests, envtest regression), the chart flag-drop with documented posture, and the full BillingHalt
stack (classifier, latch, 5 gates, 5 backstops, resume-clear) all exist and are substantively
wired. The HALT-01 halt-side and DISPATCH-02 chart artifacts pass.

Three gaps block the phase goal:

1. **CR-01 (DISPATCH-01)** — the milestone dispatch site, one of the "all dispatch sites" the goal
   names, panics on a nil project. resolveImage is nil-safe but the surrounding :370/:394 derefs are
   not, and phase 13 wired the nil-tolerant call between them. Mechanical guard fix (mirror
   phase/plan controllers).

2. **WR-03 (HALT-01)** — the halt fires, but recovery is unreliable: the non-clearing credproxy latch
   re-injects the "credit balance" substring, so a straggler session re-stamps BillingHalt
   immediately after `tide resume`. The goal's recovery half (D-06) does not hold end-to-end. Root
   cause: the synthetic local 400 is indistinguishable from a genuine fresh dry-out.

3. **Full `make test-int` red (DISPATCH-02 / 13-03 criterion)** — 3 inherited promptPath fixture
   failures (mechanical fix; matches the owner's root-cause-over-defer preference) plus 1 unattributed
   reporter-materialization failure that needs provenance assignment. 13-03 explicitly required a green
   full suite and claimed it.

WR-01 (vacuous planner-hold tests) is a WARNING, not a blocker: the production gates ARE present and
correctly positioned (verified by grep + reading all five sites); only their planner-level test
coverage is hollow. WR-02/04/05/06/07/08 and IN-06 are tracked warnings — none alone defeats the goal,
but several (WR-02 free-text classifier blast radius, WR-08 plan terminal-fail bypassing the backstop,
IN-06 fixture Dispatcher halt bypass) are robustness risks worth closing alongside the blockers.

None of the gaps are addressed in later milestone phases (14 Budget/Pricing, 15 Paper Cuts, 16
Telemetry) — no deferral applies.

---

_Verified: 2026-06-11T20:10:00Z_
_Verifier: Claude (gsd-verifier)_
