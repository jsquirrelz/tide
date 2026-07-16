---
phase: 44
slug: llm-message-array-spans-d-o5-redaction-size-boundary
status: ready
nyquist_compliant: true
wave_0_complete: false
created: 2026-07-16
---

# Phase 44 — Validation Strategy

> Per-phase validation contract for feedback sampling during execution.
> Populated from 44-RESEARCH.md §"Validation Architecture" (plan-checker warning 1 closure).

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework** | Go stdlib `testing` (plain `go test` — matches existing `attrs_test.go`/`materialize_test.go`/`main_test.go` convention) |
| **Config file** | none — plain `go test ./...` |
| **Quick run command** | `go test ./pkg/otelai/... ./internal/reporter/... ./internal/harness/redact/... ./cmd/tide-reporter/...` |
| **Full suite command** | `make test` (unit tier), then `make test-int-fast` (Layer A envtest — D-06 spawn-gating touches reconcilers) |
| **Estimated runtime** | ~60 seconds (quick) / minutes (full) |

---

## Sampling Rate

- **After every task commit:** Run `go test ./pkg/otelai/... ./internal/reporter/... ./internal/harness/redact/... ./cmd/tide-reporter/...`
- **After every plan wave:** Run `make test` (full unit tier)
- **Before `/gsd:verify-work`:** Full suite green + `TestEmitSpans_BatchAggregateUnderCeiling` asserting real observed sizes (32 calls, fixture-modeled distribution) stay under `OTEL_BSP_MAX_EXPORT_BATCH_SIZE` × per-span-cap product
- **Max feedback latency:** ~60 seconds

---

## Per-Task Verification Map

| Task ID | Plan | Wave | Requirement | Threat Ref | Secure Behavior | Test Type | Automated Command | File Exists | Status |
|---------|------|------|-------------|------------|-----------------|-----------|-------------------|-------------|--------|
| 44-01 | 01 | 1 | MSG-02 | T-secrets | `redact.String` applies `SecretPatterns` non-streaming | unit | `go test ./internal/harness/redact/... -run TestString` | ❌ W0 | ⬜ pending |
| 44-01 | 01 | 1 | MSG-03 | — | Tool-use/thinking blocks emit spec-native keys, guard tests survive | unit | `go test ./pkg/otelai/... -run 'TestToolCall.*\|TestReasoning.*'` + `-run TestNoPayloadHelperOnPublicSurface` | ✅ guard / ❌ W0 new | ⬜ pending |
| 44-03 | 03 | 2 | MSG-01 | — | Reporter reconstructs conversation from `events.jsonl`; call-1 seeded from `in.json`/`PromptPath` | unit | `go test ./internal/reporter/... -run 'TestReconstructConversation(_SeedsPrompt)?'` | ❌ W0 | ⬜ pending |
| 44-03 | 03 | 2 | MSG-02 | T-secrets | Injected secret in fixture never appears in emitted span attributes | unit | `go test ./internal/reporter/... -run TestEmitSpans_Redacts` | ❌ W0 | ⬜ pending |
| 44-03 | 03 | 2 | MSG-03 | T-secrets | Redact-BEFORE-truncate: secret split across truncation cut still redacted (D-09) | unit | `go test ./internal/reporter/... -run TestEmitSpans_RedactsBeforeTruncate` | ❌ W0 | ⬜ pending |
| 44-03 | 03 | 2 | MSG-03 | — | Oversized message truncates head+tail with marker (D-08); `ArtifactPath` co-attribute present | unit | `go test ./internal/reporter/... -run TestEmitSpans_TruncatesOversizedMessage` | ❌ W0 | ⬜ pending |
| 44-03 | 03 | 2 | MSG-03 | — | Synthetic ~32-span Task-sized batch stays under ceiling per export RPC | integration | `go test ./internal/reporter/... -run TestEmitSpans_BatchAggregateUnderCeiling` | ❌ W0 | ⬜ pending |
| 44-03 | 03 | 2 | D-11 | — | Truncated/malformed `events.jsonl` still emits reconstructable conversation, stamped degraded | unit | `go test ./internal/reporter/... -run TestReconstructConversation_TolerantSkip` | ❌ W0 | ⬜ pending |
| 44-04 | 04 | 3 | TRACE-03 | — | Spans flush to fake OTLP collector before exit on EVERY exit path (success/generic/invariant) | integration | `go test ./cmd/tide-reporter/... -run 'TestRun.*Shutdown'` | ❌ W0 | ⬜ pending |
| 44-05 | 05 | 4 | MSG-01 | — | D-06-gated, per-attempt-idempotent Task trace-only spawn at both terminal call sites | envtest | `make test-int-fast` (task_traceonly_reporter envtest specs) | ❌ W0 | ⬜ pending |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*

---

## Wave 0 Requirements

- [ ] `internal/reporter/tracesynth_test.go` — MSG-01, MSG-03, D-11 (new file)
- [ ] `internal/harness/redact/redact_test.go` extension — MSG-02 `String()` helper
- [ ] `cmd/tide-reporter/main_test.go` extension — TRACE-03 shutdown-on-every-exit-path (needs fake OTLP collector test double, does not exist yet)
- [ ] `pkg/otelai/attrs_test.go` extension — D-03 tool-call/reasoning helpers (must not break `TestNoPayloadHelperOnPublicSurface`/`TestKeysUseSemconvModule`)
- [ ] Committed synthetic `events.jsonl` test fixture mirroring the real schema with a KNOWN injected pattern-matching fake secret — real dogfood fixtures must NOT be imported verbatim (provenance/size, and redaction assertions need a known secret)

*Note: tests are authored in-plan alongside the code they verify (plans 44-01..44-05), not as a separate Wave 0 pass.*

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| Phoenix renders message arrays inline for a real run | MSG-01 | Needs live Phoenix + real dispatch | Deferred to Phase 47 PROOF-01 live end-to-end proof |

---

## Validation Sign-Off

- [x] All tasks have `<automated>` verify or Wave 0 dependencies
- [x] Sampling continuity: no 3 consecutive tasks without automated verify
- [x] Wave 0 covers all MISSING references
- [x] No watch-mode flags
- [x] Feedback latency < 120s
- [x] `nyquist_compliant: true` set in frontmatter

**Approval:** approved 2026-07-16 (populated from RESEARCH.md Validation Architecture; verified by gsd-plan-checker Dimension 8 pass)
