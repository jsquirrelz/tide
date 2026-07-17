---
phase: 46
slug: observability-enrichment-dashboard-deep-link
status: draft
nyquist_compliant: false
wave_0_complete: false
created: 2026-07-17
---

# Phase 46 — Validation Strategy

> Per-phase validation contract for feedback sampling during execution.

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework** | Go `testing` (table-driven) for Go surfaces; Vitest for `dashboard/web` |
| **Config file** | `dashboard/web/vitest.config.ts` (existing); Go needs none |
| **Quick run command** | `go test ./pkg/otelai/... ./internal/controller/... ./internal/reporter/... -run <TestName> -v` · `cd dashboard/web && npx vitest run <file>` |
| **Full suite command** | `make test` (Go unit tier) + `cd dashboard/web && npm test` |
| **Estimated runtime** | ~60–120 seconds (Go unit) + ~30 seconds (Vitest) |

---

## Sampling Rate

- **After every task commit:** Run the scoped `go test` / `npx vitest run` for the touched package
- **After every plan wave:** Run `make test` + `cd dashboard/web && npm test`
- **Before `/gsd:verify-work`:** Full suite green, plus a `helm template charts/tide` render confirming `tracesSamplerArg: "1.0"` and (with `phoenix.baseURL` set) `PHOENIX_BASE_URL` present
- **Max feedback latency:** ~120 seconds

---

## Per-Task Verification Map

| Task ID | Plan | Wave | Requirement | Threat Ref | Secure Behavior | Test Type | Automated Command | File Exists | Status |
|---------|------|------|-------------|------------|-----------------|-----------|-------------------|-------------|--------|
| *(filled by planner from PLAN.md tasks)* | | | OBS-01..04 | T-46-01..03 | see RESEARCH.md §Security Domain | unit / static | see RESEARCH.md §Validation Architecture req map | ❌ W0 | ⬜ pending |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*

---

## Wave 0 Requirements

- [ ] `pkg/otelai/attrs_test.go` — extend with `TestSessionID`, `TestMetadata` (JSON-string encoding + `attribute.STRING` type), `TestTags` (`attribute.STRINGSLICE` type — Pitfall 4 regression guard)
- [ ] `internal/controller/span_emission_test.go` — extend: `level=="task"` omits `TokenCount`; the four planner levels retain it (Pitfall 2 regression guard)
- [ ] `internal/reporter/tracesynth_test.go` — extend `EmitSpans` tests: session/metadata/tags CLI-sourced values land on emitted LLM spans
- [ ] `dashboard/web/src/lib/__tests__/phoenixLink.test.ts` — new file: `phoenixTraceURL`/`phoenixSpanURL` shape + trailing-slash normalization
- [ ] `dashboard/web/src/components/__tests__/node-panel-integration.test.tsx` — extend: link render / hide-on-empty-config cases
- [ ] `TaskDetailDrawer` link coverage (new/extended test) — required per RESEARCH.md Pitfall 1 correction (Task nodes render in TaskDetailDrawer, not NodeDetailPanel)

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| Deep link lands on the correct Phoenix trace in a live Phoenix | OBS-04 | Needs a running Phoenix instance (Phase 47's live-proof environment) | Covered by Phase 47 PROOF-01 run; this phase verifies URL shape by unit test only |

---

## Validation Sign-Off

- [ ] All tasks have `<automated>` verify or Wave 0 dependencies
- [ ] Sampling continuity: no 3 consecutive tasks without automated verify
- [ ] Wave 0 covers all MISSING references
- [ ] No watch-mode flags
- [ ] Feedback latency < 120s
- [ ] `nyquist_compliant: true` set in frontmatter

**Approval:** pending
