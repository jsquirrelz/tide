---
phase: 44
slug: llm-message-array-spans-d-o5-redaction-size-boundary
status: verified
threats_open: 0
asvs_level: 1
created: 2026-07-16
---

# Phase 44 — Security

> Per-phase security contract: threat register, accepted risks, and audit trail.

---

## Trust Boundaries

| Boundary | Description | Data Crossing |
|----------|-------------|---------------|
| repo/model content → span attribute values | Untrusted text (which may embed leaked secrets) becomes OTLP attribute values via Message.Content | Model-echoed repo content, potentially secret-bearing |
| raw events.jsonl (PVC, deliberately unredacted at source) → external OTLP collector | MSG-02's entire reason to exist — model-echoed repo secrets must never cross this boundary | Conversation payloads, tool-call args, reasoning text |
| adversarial/corrupted JSONL → reporter process | Untrusted file input parsed by the synthesizer | Subagent-writable JSONL stream |
| manager env → tenant-namespace Job env | The manager forwards its own OTLP endpoint value into reporter Job containers running in project namespaces | OTLP endpoint locator (host:port) |
| reporter process lifetime → OTLP collector | Spans buffered in the batch processor must survive process exit (or be lost silently) | Buffered trace spans |
| --traceparent Arg (manager-controlled) → span parenting | Malformed input must degrade, never crash | W3C traceparent header value |
| manager (cluster-scoped) → tenant-namespace Job creation | The manager creates trace-only reporter Jobs in project namespaces on Task completion | Job specs under the least-privilege tide-reporter SA |
| in.json promptPath (subagent-writable) → reporter file read | One-hop indirect read resolved from untrusted envelope content (registered post-plan via code review WR-04) | Workspace-relative file path |

---

## Threat Register

| Threat ID | Category | Component | Disposition | Mitigation | Status |
|-----------|----------|-----------|-------------|------------|--------|
| T-44-01 | Information Disclosure | redact.String / EmitSpans content pipeline | mitigate | `redact.String` iterates the single package-level `SecretPatterns` set (`internal/harness/redact/redact.go:118-124`, no pattern drift vs stdout path); applied to all four content paths in `boundMessages` — Content, ToolCall.ArgumentsJSON, reasoning Text, Signature (`internal/reporter/tracesynth.go:487-507`) before any attribute assignment. Tests: `TestString`, `TestEmitSpans_Redacts` (planted `sk-ant-api03-…` secret), `TestEmitSpans_SignatureRedactedTruncatedAndCounted` | closed |
| T-44-02 | Information Disclosure | truncation cut splitting a secret / otelai inlining contract | mitigate | `redactTruncate` sole wrapper enforces redact-BEFORE-truncate (`tracesynth.go:472-475`, D-09); straddle proof `TestEmitSpans_RedactsBeforeTruncate` (secret at byte 16370 straddling the 16384 head cut); D-O5 contract in `pkg/otelai/doc.go:72-100`; guards `TestNoPayloadHelperOnPublicSurface` + `TestKeysUseSemconvModule` intact | closed |
| T-44-03 | Denial of Service | ReconstructConversation on malformed/hostile JSONL | mitigate | `common.ReadLines` 16 MB per-line budget; D-11 tolerant-skip on unmarshal error (`tracesynth.go:362-369`); dangling-call flush + partial return on read error (WR-02, `tracesynth.go:426-443`). Tests: `TestReconstructConversation_TolerantSkip`, `_OversizedLineReturnsPartial`, `_MissingInJSON` | closed |
| T-44-04 | Denial of Service (tracing pipeline) | span payload volume / reporter OTLP export batches | mitigate | Triple guard: 32 KiB per-message cap + ONE SHARED 512 KiB input+output span budget (CR-01 form, `boundedSpanAttrs` `tracesynth.go:527-561`, larger side degrades to role-only first) + `OTEL_BSP_MAX_EXPORT_BATCH_SIZE="6"` literal on the reporter container (`reporter_jobspec.go:231`) bounding export RPCs under the 4 MiB gRPC ceiling. Tests: `TestEmitSpans_TruncatesOversizedMessage`, `_WholeSpanBudgetJointAcrossSides`, `_WholeSpanBudgetBothSidesOver`, `_BatchAggregateUnderCeiling`, `TestBuildReporterJob_OTLPEndpointEnv`/`_NoOTLPEndpointNoEnv` | closed |
| T-44-05 | Denial of Service (pipeline wedge) | otelShutdown against a dead collector | mitigate | D-12 bounded 5s context on every Shutdown call (`cmd/tide-reporter/main.go:180-188`); drop logged, never fails the run; `hadDeadline` asserted per exit path in `TestRunWithClient_ShutdownOnEveryExitPath` | closed |
| T-44-06 | Elevation of Privilege | trace-only reporter Job shape | mitigate | Trace-only Args exactly `{--trace-only, --workspace, --task-uid}` — zero parent-CR flags (`reporter_jobspec.go:195-200`); same least-privilege `tide-reporter` SA; trace-only branch returns before any K8s client build (`main.go:193-200`); none of the 19 phase commits touch `config/rbac/` or `charts/`. Test: `TestBuildReporterJob_TraceOnly` | closed |
| T-44-07 | Information Disclosure | OTLP endpoint value in Job spec | accept | Endpoint is a host:port locator, not a credential; identical value already visible in the manager's own pod env (`reporter_jobspec.go:230`). See Accepted Risks Log | closed |
| T-44-08 | Information Disclosure | ArtifactPath attribute value | mitigate | Attribute carries only the workspace-relative PVC path string `"envelopes/<uid>/events.jsonl"` (`tracesynth.go:626`, caller `main.go:348`), never file content; asserted in `TestEmitSpans_SpanShape` | closed |
| T-44-09 | Denial of Service | span loss on os.Exit | mitigate | TRACE-03: TracerProvider init + deferred Shutdown as `runWithClient`'s first action (`main.go:169-188`), one level below `main()`'s `os.Exit`; `TestRunWithClient_ShutdownOnEveryExitPath` covers all 4 exit paths asserting `invoked` + `hadDeadline` | closed |
| T-44-10 | Tampering | malformed --traceparent (manager-controlled) | mitigate | `otelai.ExtractRemoteParent` uses `propagation.TraceContext{}.Extract` — never panics (`pkg/otelai/tracecontext.go:93-96`); malformed input yields invalid SpanContext → unparented spans (bounded degradation). Test: `TestTraceContextExtractMalformedNoPanic` (4 malformed cases under recover() guard). Residual logging nuance in Documented Residuals | closed |
| T-44-11 | Repudiation/Integrity | Job retries re-emitting duplicate conversation spans | mitigate | D-10 exit-0 posture (trace-only path unconditionally `exitSuccess`, `synthesizeSpans` never touches exit codes) AND WR-05 `.spans-emitted` sentinel gate (checked before synth `main.go:321-325`, written after successful `EmitSpans` `main.go:353-355`). Test: `TestRunCombined_RetryDoesNotReemitSpans`. Residuals in Documented Residuals | closed |
| T-44-12 | Denial of Service | Job churn on plain clusters | mitigate | D-06 gate: `OTLPEndpoint == ""` early return before any API call (`task_controller.go:1063-1065`); envtest absence proof with `Consistently` (`task_traceonly_reporter_test.go:265,287`) | closed |
| T-44-13 | Denial of Service | reconcile-loop amplification via spawn errors | mitigate | Void-signature spawn helper: every failure logs and continues (`task_controller.go:1057,1075,1089`), no error return, no requeue — a broken spawn path cannot wedge Task terminal-state progression | closed |
| T-44-14 | Spoofing/Integrity | trace-only Job completion confused with dispatch Job | mitigate | `tideproject.k8s/role=reporter` label on Job and pod template (`reporter_jobspec.go:246-249,274,281`, T-09-13 precedent); envtest non-interference spec proves Task reaches `LevelPhaseSucceeded` unperturbed | closed |
| T-44-15 | Information Disclosure / Elevation of Privilege | promptPath resolution from subagent-writable in.json | mitigate | Registered post-plan via code review (WR-04, commit `7cb930f`): `os.OpenRoot` confines resolution to the workspace root — blocks `..`, absolute paths, and symlink escapes (`tracesynth.go:210-218`). Test: `TestSeedPrompt_RejectsPromptPathOutsideWorkspace` | closed |
| T-44-SC | Tampering | package installs | accept | Zero new packages this phase — all 19 phase-44 commits verified commit-by-commit to leave `go.mod`/`go.sum` untouched; deps pinned since Phase 42. See Accepted Risks Log | closed |

*Status: open · closed*
*Disposition: mitigate (implementation required) · accept (documented risk) · transfer (third-party)*

---

## Accepted Risks Log

| Risk ID | Threat Ref | Rationale | Accepted By | Date |
|---------|------------|-----------|-------------|------|
| AR-44-01 | T-44-07 | OTLP endpoint value in tenant-namespace Job spec Env is a host:port locator, not a credential; the identical value is already visible in the manager's own pod env | gsd-secure-phase (plan-time disposition, 44-02-PLAN.md) | 2026-07-16 |
| AR-44-02 | T-44-SC | Supply chain: zero new packages in Phase 44 (verified commit-by-commit against go.mod/go.sum); deps pinned since Phase 42 | gsd-secure-phase (plan-time disposition, all plans) | 2026-07-16 |

*Accepted risks do not resurface in future audit runs.*

---

## Documented Residuals (non-blocking)

Carried from `44-REVIEW.md` — noted for future hardening phases, no open disposition required:

- **WR-05 sentinel timing:** `.spans-emitted` is written before the async export flush completes, so a pod kill in that window yields rare at-most-once span loss. The sentinel lives on the RW PVC mount — if IN-05's read-only-mount hardening lands later, it must except the sentinel path or ship deterministic span IDs first. A manual sentinel delete is required to legitimately re-emit spans.
- **IN-03 (info-tier, deliberately unfixed):** span name and `llm.model_name` come from the untrusted stream without redaction, bounded only by the 16 MB line cap.
- **T-44-10 logging nuance:** a malformed non-empty `--traceparent` degrades to unparented spans without a stderr notice (only the empty-value case logs). The security property — never panic, bounded degradation — is fully proven.

---

## Security Audit Trail

| Audit Date | Threats Total | Closed | Open | Run By |
|------------|---------------|--------|------|--------|
| 2026-07-16 | 16 | 16 | 0 | gsd-security-auditor (verify-mitigations mode; register authored at plan time; test suites re-run green: redact, otelai, reporter, tide-reporter) |

---

## Sign-Off

- [x] All threats have a disposition (mitigate / accept / transfer)
- [x] Accepted risks documented in Accepted Risks Log
- [x] `threats_open: 0` confirmed
- [x] `status: verified` set in frontmatter

**Approval:** verified 2026-07-16
