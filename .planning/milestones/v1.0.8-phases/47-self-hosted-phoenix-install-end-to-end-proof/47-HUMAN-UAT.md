---
status: passed
phase: 47-self-hosted-phoenix-install-end-to-end-proof
source: [47-VERIFICATION.md]
started: 2026-07-17T20:15:00Z
updated: 2026-07-17T22:20:26Z
signed_off_by: human
signed_off_at: 2026-07-17T22:20:26Z
---

## Current Test

[complete — both items signed off by human at milestone close]

## Tests

### 1. Review evidence against the PROOF-01 milestone acceptance bar
expected: Human confirms (a) the trace-tree PNG shows all five AGENT levels (project→milestone→phase→plan→task) with LLM spans under Task; (b) the query PNG shows a working DSL filter over the enrichment; (c) the deep-link PNG lands on the right trace. Then explicitly accepts PROOF-01 as met OR converts a shortfall to a named gap.
why_human: Milestone acceptance-bar sign-off; visual/UX judgment against the bar; the 47-05 Task 3 blocking human gate was auto-resolved via its named-gaps branch and never actually fired.
evidence: 47-evidence-trace-tree.png, 47-evidence-query.png, 47-evidence-deeplink.png, 47-evidence-llm-span-redacted.png, 47-EVIDENCE.md
result: passed
notes: >
  Human reviewed all four evidence PNGs + 47-EVIDENCE.md at v1.0.8 close (2026-07-17).
  Confirmed (a) trace-tree shows all five AGENT levels project→milestone→phase→plan→task
  correctly parented, with LLM message spans nested under tide.dispatch.task; (b) the DSL
  filter `metadata['level'] == 'phase'` returns a live filtered span population; (c) the
  dashboard deep link resolves to trace e9124906f6ee4aeba650a6fdd93b86fd with the plan
  AGENT span 4d7f2bda9aee8d57 selected. PROOF-01 ACCEPTED as met. The two run-surfaced
  defects visible in the captures (Defect #1 status-flap / empty-artifacts panel; Defect #2
  partial ~1/3 enrichment coverage) are acknowledged as closed in code via Gap #3 (47-08)
  and CR-01 (reporter-spawn idempotency, 3 envtest specs) respectively — envtest-verified,
  not re-proven in a fresh paid live run, and accepted on that basis.

### 2. Judge the "redacted message arrays" clause
expected: Human decides whether pass-through content (with redaction proven via a 0-hit key-material search across all 392 spans) satisfies PROOF-01's "including redacted message arrays at the Task level" clause, or requests a supplementary capture from a run containing secret-bearing or over-cap content that visibly exercises the redaction/elision markers.
why_human: Interpretation of the milestone bar's "redacted" clause; cannot be resolved programmatically. This run's content had zero secret-pattern matches and its largest message attribute was 21,573 B (< 32 KiB elision cap), so unmarked pass-through is the boundary's correct output.
evidence: 47-evidence-llm-span-redacted.png, 47-EVIDENCE.md §4
result: passed
notes: >
  Human ACCEPTS pass-through content as satisfying the "redacted message arrays" clause.
  Basis: the redaction pass (redact.String at the Phase 44 D-09 chokepoint) runs
  unconditionally on the path and is unit-tested; this run simply had nothing to mask
  (0 secret-pattern matches) and no over-cap message (largest 21,573 B < 32 KiB), so
  intact pass-through is the boundary's correct output — proven safe by a 0-hit
  key-material search across all 392 spans (the real key never reached Phoenix).
  Declined to require a supplementary secret-bearing / over-cap capture.

## Summary

total: 2
passed: 2
issues: 0
pending: 0
skipped: 0
blocked: 0

## Gaps

None. Both human-judgment items signed off at v1.0.8 milestone close (2026-07-17).
