---
phase: 42-trace-context-foundation-planner-level-span-emission
audited: 2026-07-16
auditor: gsd-security-auditor
asvs_level: 1
block_on: high
threats_total: 11
threats_closed: 11
threats_open: 0
verdict: SECURED
---

# Phase 42 Security Audit

Verifies every threat declared in the 42-01..42-05 PLAN.md `<threat_model>` blocks against the
implemented code on `main` (post-review fix pass — commits `c936762`/`4cc9f68`/`9b9b396`/`b4b15f2`/`9cae6bb`).
Audit stance: every mitigation assumed absent until proven present by direct code inspection.
The repo-root `SECURITY.md` (public vulnerability-disclosure policy) is a separate document and untouched.

**Important semantic note:** the review fix pass changed the emission ordering from
emit-then-mark ("exactly once") to **mark-then-emit (at-most-once)**. T-42-08 and T-42-11 were
verified against the CURRENT semantics, which are strictly stronger against re-emission loops:
duplicate spans are structurally impossible; a crash between stamp and emit loses that attempt's
span by design (span loss preferred over double-counted tokens/cost in Phoenix).

## Threat Verification

| Threat ID | Category | Disposition | Status | Evidence |
|-----------|----------|-------------|--------|----------|
| T-42-SC | Tampering (supply chain) | mitigate | CLOSED | `go.mod:6` exact pin `v0.1.1` (no floating version); `go.sum:20-21` carries both the module-zip `h1:` and `/go.mod` checksums (toolchain-enforced on every build); `go mod graph` shows the module's only edge is `go@1.25` — zero transitive deps confirmed live; blocking human-verify checkpoint executed and operator-approved, recorded in 42-01-SUMMARY.md ("Task 1 package-legitimacy checkpoint: operator reviewed proxy.golang.org version list, @latest origin metadata, and sum.golang.org checksum entry, and approved") |
| T-42-02 | Information Disclosure | accept | CLOSED | Accepted-risk basis verified: `pkg/otelai/attrs.go` imports only `semconv` (the pinned module) and `go.opentelemetry.io/otel/attribute` — pure string constants/functions, no I/O, no data flows in 42-01 scope |
| T-42-03 | Spoofing/Tampering | mitigate | CLOSED | `pkg/otelai/tracecontext.go:93-96` — `ExtractRemoteParent` delegates parsing entirely to `propagation.TraceContext{}.Extract` on a `MapCarrier`; no hand parsing, no control-flow decision from the value (ctx returned as-is). `TestTraceContextExtractMalformedNoPanic` present at `tracecontext_test.go:157` with panic-recovery guard (:160) and `IsValid()==false` assertion (:173) over 4 malformed-input classes |
| T-42-04 | Information Disclosure | accept | CLOSED | Accepted-risk basis verified: `tracecontext.go:53-56` derives TraceID from the UID hex; UIDs are non-secret object metadata visible to any principal with `get` RBAC; the OTLP collector sits inside the existing manager→OTLP trust boundary. No production caller exists yet (Option A — Phase 43 is the consumer) |
| T-42-05 | Tampering | accept | CLOSED | Accepted-risk basis verified: four `*SpanEmittedUID` fields defined under `+kubebuilder:subresource:status` types (`api/v1alpha3/milestone_types.go:78`, `phase_types.go`, `plan_types.go`, `project_types.go:529`) — writable only via status-subresource RBAC, same trust level as the adjacent `*RolledUpUID` markers; grep confirms field usage is confined to the four span-emission blocks, `span_emission.go` doc comments, and tests — zero non-telemetry control-flow reads. Worst-case tamper: marker forged → span suppressed; marker cleared → one extra span. Never a dispatch/control-flow effect |
| T-42-06 | Denial of Service | accept | CLOSED | Accepted-risk basis verified: each field is a single `+optional` string scalar holding a Job UID (36 bytes) — one bounded scalar per level object, no aggregates (PERSIST-02), far under etcd's 1.5 MiB limit. CRD manifests regenerated, not hand-edited (all 4 `config/crd/bases/*.yaml` + 4 `charts/tide-crds/templates/*-crd.yaml` carry the property) |
| T-42-07 | Information Disclosure | accept | CLOSED | Accepted-risk basis verified: `out.Reason` already flows to `setBillingHaltIfNeeded` (pre-existing, at `milestone_controller.go:666`, `phase_controller.go:597`, `plan_controller.go:666`, `project_controller.go:1957`, `task_controller.go:1059`) and to CRD condition messages — the span usage at `span_emission.go:154,156` adds no new exposure class; short failure codes, not LLM output; payload redaction is Phase 44 MSG-02 scope |
| T-42-08 | Denial of Service | mitigate | CLOSED | Current mark-then-emit semantics verified at both 42-04 levels: gate `completedJob != nil && marker != string(completedJob.UID) && plannerSpanResolvable(completedJob)` (`milestone_controller.go:569`, `phase_controller.go:510`); `plannerSpanResolvable` (`span_emission.go:76-82`) gates the stamp so a stamp is never wasted on an unemittable span; marker stamped durably with `MergeFromWithOptimisticLock` BEFORE emission; `synthesizePlannerSpan` runs only in the `else if stamped` branch (`milestone_controller.go:594-596`, `phase_controller.go:535-537`). Marker keyed by Job UID (WR-02) so a recreated attempt re-emits its own span. **Degrade path cannot re-emit:** on `RetryOnConflict` exhaustion the block logs and continues with NO emission (`milestone_controller.go:586-593`) — zero spans that reconcile; the unset marker retries the stamp on a later reconcile, still emitting at most once per successful stamp. Concurrent-reconcile race also cannot double-emit: the already-stamped short-circuit returns with `stamped==false` (the stamper emits). Proven: idempotency spec asserts span count == 1 after the second handler call (`span_emission_test.go:260,490`); degraded-envelope spec (`:311`) plus `TestSynthesizePlannerSpanDegradedEnvelope`; 13/13 SpanEmission envtest specs re-run green during this audit (2026-07-16) |
| T-42-09 | Tampering | accept | CLOSED | Accepted-risk basis verified: in `span_emission.go:140-160`, envelope ints (`Usage.*`, `ExitCode`) and `Reason` flow ONLY into span attributes / span status description; the two branches are on `envReadOK` (read success, not value) and `isJobFailed(completedJob)` (Job status, not envelope) — no control flow derives from envelope values |
| T-42-10 | Information Disclosure | accept | CLOSED | Identical class to T-42-07 at the last two levels: `plan_controller.go:580` and `project_controller.go:1851` pass the same `out` the handlers already consumed for conditions/billing-halt; no message payloads at planner levels; OTLP endpoint/credential surface unchanged from `internal/otelinit` |
| T-42-11 | Denial of Service | mitigate | CLOSED | Same mark-then-emit gate ported byte-identically to Plan (`plan_controller.go:554-582`) and Project (`project_controller.go:1825-1853`), UID-keyed, emission only in `else if stamped`, log-and-continue on patch failure with no emission. Project marker durability across halt/resume is structural: `PlannerSpanEmittedUID` lives directly on `ProjectStatus` (`project_types.go:529`) under the status subresource — persisted in etcd, survives manager restart/halt/resume like all CRD `.status`. Project idempotency spec additionally asserts the marker remains the Job UID after the second call (`span_emission_test.go:878-883`); Plan idempotency at `:700`. Import-source projects: span block deliberately outside the D-11/R-13 rollup-suppression branch; the `completedJob != nil` gate handles imports naturally |

## Accepted Risks Log

| Threat ID | Risk accepted | Rationale | Revisit trigger |
|-----------|---------------|-----------|-----------------|
| T-42-02 | Attribute constants ship as pure strings | No data flows in 42-01 scope; value flows threat-modeled in 42-04/42-05 | New helper that performs I/O in `pkg/otelai` |
| T-42-04 | Project UID visible to OTLP collector via deterministic TraceID | UIDs are non-secret metadata; collector inside existing manager→OTLP boundary | Collector moves outside the cluster trust boundary |
| T-42-05 | Status-subresource principals can suppress/duplicate a telemetry span by tampering `*SpanEmittedUID` | Same trust level as `*RolledUpUID`; no control-flow effect | Any future code reading these markers for non-telemetry decisions |
| T-42-06 | +1 bounded string scalar per level object in etcd | 36-byte Job UID, no aggregates (PERSIST-02) | Marker becomes a list/map |
| T-42-07 / T-42-10 | `tide.reason` / token-count / model attributes leave the process to the chart-configured OTLP endpoint | Same values already flow to CRD conditions; short failure codes, not LLM output | Phase 44 (message content on spans) — redaction is MSG-02 scope |
| T-42-09 | Untrusted envelope ints attached as span attributes without validation | Telemetry values only; envelope remains status optimization, never success authority | Any control flow added on envelope-derived attribute values |

## Unregistered Flags

None. No SUMMARY.md contains a `## Threat Flags` section, and no new attack surface appeared
outside the registered threats (42-01's `make demo-fixture` run materializes tracked-source-backed
build content only; 42-03's chart regeneration is generated output).

Informational residuals from 42-REVIEW.md (not threats, tracked there as open INFO items):
IN-04 notes the `stripGoComments` helper backing the `TestKeysUseSemconvModule` ATTR-03 guard
treats `//` inside string literals as a comment — a latent blind spot in a code-convention
tripwire, not a security control. IN-01/IN-02 describe span-LOSS paths (telemetry gaps), which
are consistent with the at-most-once contract and carry no re-emission or disclosure risk.

## Verification Method

- Supply chain: direct read of `go.mod`/`go.sum`; `go mod graph` for transitive closure; operator-approval record in 42-01-SUMMARY.md.
- Mitigations: full read of `span_emission.go` and all four controller insertion blocks; grep for all `SpanEmittedUID` usages repo-wide; full read of `tracecontext.go` + test-function inventory.
- Accepted risks: factual basis of each acceptance re-checked in code (imports, field markers, pre-existing `out.Reason` flows, branch conditions).
- Live proof: `go test ./pkg/otelai/` green; span-emission unit tests 9/9 green; SpanEmission envtest suite `Ran 13 of 217 Specs — SUCCESS! 13 Passed | 0 Failed` (run 2026-07-16 during this audit).
