# SECURITY.md — Phase 39: Pre-flight Tech-Debt Hardening

**Audit date:** 2026-06-29
**ASVS Level:** 1
**Block-on:** critical
**Disposition:** SECURED — 5/5 threats resolved (3 mitigate verified present, 2 accept validly accepted)

This phase carried two plan-time `<threat_model>` blocks (39-01, 39-02). Each declared threat
was verified against implemented code by its declared disposition. No blind vulnerability scan
was performed — verification only.

## Threat Verification

| Threat ID | Category | Disposition | Status | Evidence |
|-----------|----------|-------------|--------|----------|
| T-39-01 | Denial of Service | mitigate | CLOSED | Fallback corrected to `default 4` at the chart-augment SOURCE `hack/helm/augment-tide-chart.sh:90` AND the regenerated `charts/tide/templates/configmap.yaml:22` (`plannerConcurrency: {{ .Values.plannerConcurrency \| default 4 }}`). `helm template tide charts/tide` renders `plannerConcurrency: 4` (line 177 of render). No `default 16` on the plannerConcurrency line (the `default 16` on `maxConcurrentReconciles.task:30` is intentional and out of scope). Pinned by `test/integration/kind/configmap_planner_concurrency_test.go` (render contract) and `internal/config/config_default_test.go` (config.Load resolves PlannerConcurrency == 4). |
| T-39-02 | Tampering | accept | CLOSED | No new go.mod/go.sum or chart dependency. `git show --stat` across all 5 phase-39 code commits (5da4df6, 6b4c3ef, 6f20c29, db7abe8, 057047b) touches only configmap.yaml, augment script, two new test files, and project_controller.go — zero `go.mod`/`go.sum`/`Chart.yaml`/lock-file changes. Accepted risk validly recorded here. |
| T-39-03 | Tampering | mitigate | CLOSED | `internal/controller/project_controller.go:1382-1395`: `retry.RetryOnConflict(retry.DefaultRetry, ...)` re-fetches latest Project via `r.Get` (1384), idempotent short-circuit on `latest.Status.Budget.PlannerRolledUpUID == plannerJobName` (1387-1389), `client.MergeFromWithOptimisticLock{}` patch (1390-1392), and returns the error to requeue on exhaustion — `return ctrl.Result{}, fmt.Errorf("patch PlannerRolledUpUID: %w", mErr)` (1394). No best-effort last-write-wins window remains. Mirrors `milestone_controller.go:620-632`. The `retry` import is present at `project_controller.go:34`. |
| T-39-04 | Repudiation | mitigate | CLOSED | Durable `PlannerRolledUpUID` marker is the sole idempotency guard, stamped only after a successful `budget.RollUpUsage` (ordering preserved, project_controller.go:1371-1392). `internal/controller/project_rollup_idempotency_test.go` proves CostSpentCents accrues once (Test 1, line 136-148) then stays constant on a second post-TTL-GC reconcile (`Consistently`, line 163-169) with `ReporterImage=""` forcing `isFirstCompletion=true` on every call. |
| T-39-05 | Tampering | accept | CLOSED | Only new import is `k8s.io/client-go/util/retry` (project_controller.go:34) — already imported pre-phase in milestone_controller.go since Phase 31, a pre-existing dependency. No new module added (confirmed by the same zero-go.mod-change git evidence as T-39-02). Accepted risk validly recorded here. |

## Accepted Risks Log

- **T-39-02 (Tampering — test/build deps):** Phase 39 introduces no new go.mod/go.sum or chart
  dependency. Only stdlib `os/exec` (already used in the kind package) and existing test helpers.
  Verified by git stat across all phase-39 code commits.
- **T-39-05 (Tampering — go module deps):** The single new import,
  `k8s.io/client-go/util/retry`, is a pre-existing dependency already used by
  milestone_controller.go. No module graph change. Verified by git stat.

## Unregistered Flags

None. Both SUMMARY.md `## Threat Flags` / `## Threat Surface Scan` sections report no new network
endpoints, auth paths, file access patterns, or schema changes. The changes are confined to an
existing reconcile path (narrowing a race window) and a Helm template default-value correction.
No new attack surface appeared during implementation.

## Notes

- Implementation files were not modified by this audit (read-only).
- All three `mitigate` threats resolved to CLOSED with a grep-confirmed code location, not by
  documentation or structural inference.
- Both `accept` threats resolved to CLOSED with positive git evidence of zero dependency change,
  not merely the plan's pre-verified claim.
