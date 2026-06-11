---
phase: 13-dispatch-image-resolution-provider-halt
plan: "02"
subsystem: billing-halt
tags: [billing, halt, credproxy, conditions, resume, tdd]
dependency_graph:
  requires: []
  provides:
    - ConditionBillingHalt + ReasonCreditBalanceTooLow constants (api/v1alpha1/shared_types.go)
    - checkBillingHalt gate predicate (internal/controller/billing_halt.go)
    - isBillingFailureReason classifier (internal/controller/billing_halt.go)
    - setBillingHaltIfNeeded condition writer (internal/controller/billing_halt.go)
    - credproxy isCreditExhaustion + billingHalted latch (internal/credproxy/server.go)
    - tide resume BillingHalt clear (cmd/tide/resume.go)
  affects:
    - plan 13-04 (wires billing halt into five reconciler dispatch gates)
tech_stack:
  added: []
  patterns:
    - "atomic.Bool for per-session fail-fast latch in credproxy"
    - "conditional status patch in resume.go (only when condition exists — backward-compat)"
key_files:
  created:
    - internal/controller/billing_halt.go
    - internal/controller/billing_halt_test.go
  modified:
    - api/v1alpha1/shared_types.go
    - internal/credproxy/server.go
    - internal/credproxy/server_test.go
    - cmd/tide/resume.go
    - cmd/tide/resume_test.go
decisions:
  - "Re-fetch project before status patch in resume.go to get fresh resourceVersion after annotation patch"
  - "Guard status patch on haltCond != nil (not unconditional) to avoid fake-client 'not found' on tests without WithStatusSubresource"
  - "billingHalted as atomic.Bool on Proxy (not a separate struct) — per-session latch, zero allocation"
metrics:
  duration: "~7 minutes"
  completed: "2026-06-11T17:53:00Z"
  tasks_completed: 3
  tasks_total: 3
  files_created: 2
  files_modified: 5
---

# Phase 13 Plan 02: BillingHalt Machinery Summary

BillingHalt HALT-01 machinery — condition vocabulary, controller helpers, credproxy per-session fail-fast latch, and `tide resume` recovery verb — all with RED-first TDD.

## Tasks Completed

| Task | Name | Commit | Files |
|------|------|--------|-------|
| 1 | BillingHalt condition vocabulary + shared helpers | 8cf8771 | api/v1alpha1/shared_types.go, internal/controller/billing_halt.go, internal/controller/billing_halt_test.go |
| 2 | credproxy billing-400 classifier + fail-fast latch | a592ffe | internal/credproxy/server.go, internal/credproxy/server_test.go |
| 3 | tide resume clears BillingHalt | 142e388 | cmd/tide/resume.go, cmd/tide/resume_test.go |

## What Was Built

**Task 1 — Condition vocabulary + controller helpers:**
- `api/v1alpha1/shared_types.go`: Phase 13 const block with `ConditionBillingHalt = "BillingHalt"` and `ReasonCreditBalanceTooLow = "CreditBalanceTooLow"`, following the ConditionBudgetExceeded comment density/naming pattern.
- `internal/controller/billing_halt.go` (new file — NOT dispatch_helpers.go, which plan 13-01 owns):
  - `isBillingFailureReason(reason string) bool` — pure string ops: `strings.HasPrefix(reason, "billing-halt:")` OR `strings.Contains(lower, "credit balance")`. No SDK import.
  - `checkBillingHalt(project *Project) bool` — nil-safe scan of Status.Conditions for BillingHalt=True.
  - `setBillingHaltIfNeeded(ctx, client, project, reason) error` — classifies via isBillingFailureReason, patches BillingHalt=True with Reason=CreditBalanceTooLow and a Message mentioning `tide resume`.
- 11 unit tests covering all behavior/boundary cases, all green.

**Task 2 — credproxy billing-400 classifier + fail-fast latch:**
- `isCreditExhaustion(body []byte) bool` — `strings.Contains(lower, "credit balance")`. Conservative substring per RESEARCH.
- `billingHalted atomic.Bool` on `Proxy` struct — per-session fail-fast flag.
- `rp.ModifyResponse` hook: reads body on 400 responses, restores body byte-identical (RESEARCH Pitfall 5), sets latch on billing hit, emits stdlib log line with "billing-halt" token.
- Handler latch check: after token validation, before allowlist check — if latched, respond HTTP 400 + `X-Tide-Billing-Halt: true` with Anthropic-shaped body (preserves "credit balance" substring for EnvelopeOut.Reason channel). Zero upstream calls after first billing 400.
- providerfirewall boundary intact: no api/v1alpha1 or controller-runtime imports in credproxy.
- 4 new test cases + all existing credproxy tests green.

**Task 3 — tide resume clears BillingHalt:**
- `resumeRun` extended: after annotation patch, re-fetches project (fresh resourceVersion), then calls `meta.RemoveStatusCondition(&proj.Status.Conditions, tidev1alpha1.ConditionBillingHalt)` + `c.Status().Patch` when condition exists.
- Guard: status patch only executed when `haltCond != nil` — backward-compatible with existing tests that don't register Project as status subresource.
- Emits "cleared BillingHalt (billing recovery)" to out when condition was True.
- No auto-probe logic (D-06: operator chose recovery by running resume).
- 2 new test cases + all 11 existing resume tests still green.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] Guard BillingHalt status patch on condition existence**
- **Found during:** Task 3 GREEN
- **Issue:** Unconditional `c.Status().Patch` failed with "not found" in existing tests that build fake clients without `WithStatusSubresource` for Project. The fake client treats status subresource as optional.
- **Fix:** Only execute the status patch when `meta.FindStatusCondition` returns non-nil for BillingHalt. This preserves correctness (no-op when not set), satisfies the new test (uses `WithStatusSubresource`), and is backward-compatible with existing tests.
- **Files modified:** cmd/tide/resume.go
- **Commit:** 142e388

**2. [Rule 1 - Bug] Re-fetch project before status patch**
- **Found during:** Task 3 GREEN (same fix iteration)
- **Issue:** After the annotation `c.Patch`, the project's resourceVersion is stale for a subsequent `c.Status().Patch`. Added `c.Get` re-fetch between the two patches.
- **Files modified:** cmd/tide/resume.go
- **Commit:** 142e388

## Test Coverage

All tests are pure Go unit tests (no envtest/Ginkgo). The envtest suite in `internal/controller` requires `etcd` which is not present in this environment — this is pre-existing and unrelated to plan 13-02 changes.

| Package | New Tests | Existing Tests | Status |
|---------|-----------|----------------|--------|
| internal/controller | 11 | all pass | GREEN |
| internal/credproxy | 4 | all pass | GREEN |
| cmd/tide | 2 | all pass | GREEN |

## Acceptance Criteria

- `grep -c 'ConditionBillingHalt = "BillingHalt"' api/v1alpha1/shared_types.go` = 1 (PASS)
- `grep -c 'func checkBillingHalt\|func isBillingFailureReason\|func setBillingHaltIfNeeded' internal/controller/billing_halt.go` = 3 (PASS)
- `grep -n 'PhaseBillingHalt' api/v1alpha1/` = nothing (PASS — Conditions only)
- `grep -c 'func isCreditExhaustion' internal/credproxy/server.go` = 1 (PASS)
- `grep -c 'ModifyResponse' internal/credproxy/server.go` >= 1 (= 4, PASS)
- `grep -n 'api/v1alpha1\|controller-runtime' internal/credproxy/server.go` = only comments (PASS — firewall intact)
- `grep -c 'RemoveStatusCondition' cmd/tide/resume.go` = 1 (PASS)
- `grep -in 'probe\|test request\|ping' cmd/tide/resume.go` = no code (PASS — comment only)
- `go vet ./internal/credproxy/` exits 0 (PASS)
- All targeted unit tests exit 0 (PASS)

## Threat Flags

None — no new trust boundaries introduced. All implementations stay within boundaries declared in the plan's threat model.

## Self-Check: PASSED

Files exist:
- /internal/controller/billing_halt.go: FOUND
- /internal/controller/billing_halt_test.go: FOUND
- /api/v1alpha1/shared_types.go (modified): FOUND

Commits exist:
- 8cf8771: FOUND (feat(13-02): BillingHalt condition vocabulary + shared helpers)
- a592ffe: FOUND (feat(13-02): credproxy billing-400 classifier + fail-fast latch)
- 142e388: FOUND (feat(13-02): tide resume clears BillingHalt condition (D-06))
