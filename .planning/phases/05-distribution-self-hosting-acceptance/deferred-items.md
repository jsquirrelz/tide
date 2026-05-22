# Phase 5 — Deferred Items

Out-of-scope discoveries logged during plan execution. Address in a follow-up plan.

- 2026-05-22 (plan 05-01) — `cmd/dashboard/api/plans.go` and `cmd/dashboard/api/tasks.go` are not gofmt-clean. Detected when `make fmt` ran during plan 05-01 verification. Out of scope for DIST-03 (these files already carry the Apache-2.0 header, so the verify-license gate is unaffected). Defer to a follow-up plan or a `/gsd:quick`.
