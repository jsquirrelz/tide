# Deferred Items — Phase 02

## Pre-existing test failures (out of scope for plan 02-10)

These failures were present in the baseline before plan 02-10 changes and are
unrelated to ProjectReconciler. Do not fix them in plan 02-10.

| Test | File | Discovered By | Status |
|------|------|---------------|--------|
| TestTaskReconciler_RateLimitGate_RequeuesWhenBucketExhausted | task_controller_test.go:409 | 02-10 executor | Pre-existing |
| TestTaskReconciler_RateLimitStormAbsorbed | task_controller_test.go:497 | 02-10 executor | Pre-existing |
| TestTaskReconciler_BudgetExceededHalts | task_controller_test.go:574 | 02-10 executor | Pre-existing |
| TestTaskReconciler_HaltsAtMaxAttempts | task_controller_test.go:766 | 02-10 executor | Pre-existing |

These tests belong to the TaskReconciler budget gate and rate-limit integration
paths. The failures exist on the wave-5 baseline (cf08db86) before any 02-10
changes. They should be addressed by the owning plans (02-09 or a follow-on plan).
