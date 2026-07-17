---
status: partial
phase: 47-self-hosted-phoenix-install-end-to-end-proof
source: [47-VERIFICATION.md]
started: 2026-07-17T20:15:00Z
updated: 2026-07-17T20:15:00Z
---

## Current Test

[awaiting human testing]

## Tests

### 1. Review evidence against the PROOF-01 milestone acceptance bar
expected: Human confirms (a) the trace-tree PNG shows all five AGENT levels (project→milestone→phase→plan→task) with LLM spans under Task; (b) the query PNG shows a working DSL filter over the enrichment; (c) the deep-link PNG lands on the right trace. Then explicitly accepts PROOF-01 as met OR converts a shortfall to a named gap.
why_human: Milestone acceptance-bar sign-off; visual/UX judgment against the bar; the 47-05 Task 3 blocking human gate was auto-resolved via its named-gaps branch and never actually fired.
evidence: 47-evidence-trace-tree.png, 47-evidence-query.png, 47-evidence-deeplink.png, 47-evidence-llm-span-redacted.png, 47-EVIDENCE.md
result: [pending]

### 2. Judge the "redacted message arrays" clause
expected: Human decides whether pass-through content (with redaction proven via a 0-hit key-material search across all 392 spans) satisfies PROOF-01's "including redacted message arrays at the Task level" clause, or requests a supplementary capture from a run containing secret-bearing or over-cap content that visibly exercises the redaction/elision markers.
why_human: Interpretation of the milestone bar's "redacted" clause; cannot be resolved programmatically. This run's content had zero secret-pattern matches and its largest message attribute was 21,573 B (< 32 KiB elision cap), so unmarked pass-through is the boundary's correct output.
evidence: 47-evidence-llm-span-redacted.png, 47-EVIDENCE.md §4
result: [pending]

## Summary

total: 2
passed: 0
issues: 0
pending: 2
skipped: 0
blocked: 0

## Gaps
