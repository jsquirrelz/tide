---
phase: 15
slug: paper-cuts
status: planned
nyquist_compliant: true
wave_0_complete: false
created: 2026-06-12
---

# Phase 15 — Validation Strategy

> Per-phase validation contract for feedback sampling during execution.

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework** | go test + Ginkgo v2.28/Gomega (envtest), Vitest (dashboard/web) |
| **Config file** | Makefile (test targets), dashboard/web/vitest.config.ts |
| **Quick run command** | `go test ./internal/... ./cmd/...` (Go) / `npm test --prefix dashboard/web` (dashboard) |
| **Full suite command** | `make test` (unit+envtest); `make test-int` (kind Layer B, heavy — read MAKE_EXIT + grep '^--- FAIL', not just Ginkgo summary) |
| **Estimated runtime** | ~120s unit/envtest; kind suite minutes-scale (one heavy run at a time on the 7.65 GiB VM) |

---

## Sampling Rate

- **After every task commit:** Run the affected package's `go test ./internal/<pkg>/...` or `npm test --prefix dashboard/web -- --run <file>`
- **After every plan wave:** Run `make test` (full unit/envtest); `make test-int` only at phase-final gate
- **Before `/gsd:verify-work`:** Full suite must be green
- **Max feedback latency:** 180 seconds (unit/envtest layer)

---

## Per-Task Verification Map

*Run-1 regression symmetry: each CUTS requirement maps to a regression test reproducing the run-1 symptom.*

| Task ID | Plan | Wave | Requirement | Threat Ref | Secure Behavior | Test Type | Automated Command | File Exists | Status |
|---------|------|------|-------------|------------|-----------------|-----------|-------------------|-------------|--------|
| 15-01-T1 | 15-01 | 1 | CUTS-01 | T-15-01 | StampProjectLabel overwrites LLM-authored labels from authoritative parent | unit | `go test ./internal/owner/... ./internal/reporter/...` | ⬜ new (label_test.go, materialize_test.go extension) | ⬜ pending |
| 15-01-T2 | 15-01 | 1 | CUTS-01 | T-15-02, T-15-03 | Metadata-only one-shot backfill patch from OwnerRef chain | envtest | `go test ./internal/controller/... -run TestControllers -count=1` | ⬜ new specs | ⬜ pending |
| 15-01-T3 | 15-01 | 1 | CUTS-01 | — | Discovery stays label-filter-only (D-02) | unit | `go test ./cmd/tide/... -run Approve -count=1` | ⬜ new cases in approve_test.go | ⬜ pending |
| 15-02-T1 | 15-02 | 1 | CUTS-07 | T-15-05 | Webhook resolves real project mode; Pitfall B preserved | envtest | `go test ./internal/webhook/... ./internal/controller/... -run "Webhook\|PlanWebhook" -count=1` | partial (extend plan_webhook_test.go) | ⬜ pending |
| 15-02-T2 | 15-02 | 1 | CUTS-07 | T-15-04, T-15-07 | Strict mismatch parks before any dispatch; condition names tasks+path; park lifts on fix | envtest | `go test ./internal/controller/... -run TestControllers -count=1` | ⬜ new (file_touch_gate_test.go) | ⬜ pending |
| 15-02-T3 | 15-02 | 1 | CUTS-07 | — | Prompt rule prevents overlap at authoring time (D-07) | unit | `go test ./internal/subagent/common/... -count=1` | partial (extend prompt_templates_test.go) | ⬜ pending |
| 15-03-T1 | 15-03 | 1 | CUTS-04 | T-15-08, T-15-11 | Path validated; env-var delivery, no shell interpolation; fixed image | build/vet | `go build ./cmd/tide/... && go vet ./cmd/tide/...` | n/a | ⬜ pending |
| 15-03-T2 | 15-03 | 1 | CUTS-04 | T-15-09 | Pod deleted on every exit path; traversal refs rejected with zero Creates | unit (fake seam) | `go test ./cmd/tide/... -run ArtifactGet -count=1` | ⬜ new (artifact_get_run_test.go) | ⬜ pending |
| 15-04-T1 | 15-04 | 1 | CUTS-02 | T-15-12 | Clean-tree skip is operator-visible and asserted | unit | `go test ./cmd/tide-push/... -count=1` | ✅ main_test.go:996-1045 (audit + gap-close only) | ⬜ pending |
| 15-04-T2 | 15-04 | 1 | CUTS-03 | — | AwaitingApproval stays parked sans annotation (no flap) | envtest | `go test ./internal/controller/... -run TestControllers -count=1` | ⬜ audit-then-add in phase_gates_test.go | ⬜ pending |
| 15-05-T1 | 15-05 | 1 | CUTS-05 | T-15-14 | Coerce guards exhaustive-by-construction (derived from STATUS_TABLE) | typecheck+vitest | `cd dashboard/web && npx tsc --noEmit && npm run test -- --run` | n/a | ⬜ pending |
| 15-05-T2 | 15-05 | 1 | CUTS-05 | T-15-14 | Complete renders Complete-not-Pending (finding 9b symptom); unknown still → Pending | Vitest | `cd dashboard/web && npm run test -- --run` | partial (extend 3 spec files) | ⬜ pending |
| 15-06-T1 | 15-06 | 1 | CUTS-06 | T-15-16 | Cross-project Tasks excluded from aggregate; []-never-null | unit | `go test ./cmd/dashboard/api/... -run "Waves\|RunningWaves" -count=1` | ⬜ new (waves_test.go) | ⬜ pending |
| 15-06-T2 | 15-06 | 1 | CUTS-06 | T-15-19 | No new route; no CRD aggregate fields; read-only surface | unit + invariant | `go test ./cmd/dashboard/... -count=1 && make verify-no-aggregates` | partial (extend bridge/SSE tests) | ⬜ pending |
| 15-07-T1 | 15-07 | 2 | CUTS-06 | T-15-20, T-15-22 | Payload strings React-escaped; chip statuses coerced | typecheck+vitest | `cd dashboard/web && npx tsc --noEmit && npm run test -- --run` | ⬜ new (RunningWavesView.tsx) | ⬜ pending |
| 15-07-T2 | 15-07 | 2 | CUTS-06 | — | Old empty state gone; AppShell/Header untouched (Phase 16 seam) | typecheck+vitest | `cd dashboard/web && npx tsc --noEmit && npm run test -- --run` | n/a | ⬜ pending |
| 15-07-T3 | 15-07 | 2 | CUTS-06 | T-15-21 | Malformed snapshot ignored, last good state persists | Vitest | `cd dashboard/web && npm run test -- --run` | ⬜ new (RunningWavesView.test.tsx, App spec extension) | ⬜ pending |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*

---

## Wave 0 Requirements

Existing infrastructure covers all phase requirements — envtest suites exist for all four controllers, cmd/tide has table-driven CLI tests with function-var fake seams (tail.go pattern), dashboard/web has Vitest + Testing Library. No new framework installs needed. New test FILES (label_test.go, file_touch_gate_test.go, artifact_get_run_test.go, waves_test.go, RunningWavesView.test.tsx) are created inside their owning plans' tasks — no separate Wave 0 plan required.

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| `tide artifact-get` against a live cluster streams real artifact bytes | CUTS-04 | End-to-end PVC + inspector-pod path needs a real kind cluster with a populated workspace PVC | Run a stub project to seed the PVC, then `tide artifact-get <ns>/<proj>/MILESTONE.md` and compare bytes; while an authoring session runs, confirm it waits rather than erroring or returning partial content |
| Dashboard running-waves view renders live | CUTS-06 | Visual SSE behavior on a live cluster | Open dashboard with ≥2 plans running; verify wave cards, live updates, click-through, and the All waves return button |

---

## Validation Sign-Off

- [x] All tasks have `<automated>` verify or Wave 0 dependencies
- [x] Sampling continuity: no 3 consecutive tasks without automated verify
- [x] Wave 0 covers all MISSING references (test files created in-plan)
- [x] No watch-mode flags (all Vitest commands use `--run`)
- [x] Feedback latency < 180s
- [x] `nyquist_compliant: true` set in frontmatter

**Approval:** pending
