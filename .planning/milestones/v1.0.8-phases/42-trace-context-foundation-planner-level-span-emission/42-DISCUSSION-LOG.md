# Phase 42: Trace-Context Foundation + Planner-Level Span Emission - Discussion Log

> **Audit trail only.** Do not use as input to planning, research, or execution agents.
> Decisions are captured in CONTEXT.md — this log preserves the alternatives considered.

**Date:** 2026-07-15
**Phase:** 42-Trace-Context Foundation + Planner-Level Span Emission
**Areas discussed:** Failure-path span coverage, ATTR-03 custom-key policy, Attribute value semantics

---

## Todo Cross-Reference

| Option | Description | Selected |
|--------|-------------|----------|
| Fold none (Recommended) | All four matched on generic keywords only (score 0.6) — GPG signing, dispatch-gate ordering, caching unrelated to span emission | ✓ |
| Fold W-2 gate todos | Project FailureHalt gap + Task Import ordering — same controllers, different concern | |
| Fold GPG signing | SIGN-02/03/04 Verified-badge work | |
| Fold CACHE-F1 | Direct-SDK cross-pod caching backend | |

**User's choice:** Fold none

---

## Gray Area Selection

Offered: Mid-milestone trace shape, Failure-path span coverage, ATTR-03 custom-key policy, Attribute value semantics.
**Selected:** Failure-path span coverage, ATTR-03 custom-key policy, Attribute value semantics (Mid-milestone trace shape left to Claude's discretion).

---

## Failure-path span coverage

**Q1: Emit AGENT spans for failed planner-Job completions, or only succeeded?**

| Option | Description | Selected |
|--------|-------------|----------|
| Succeeded + failed (Recommended) | Failed dispatches are what an operator debugs in Phoenix; status Error; data already at the call site | ✓ |
| Succeeded only | Literal reading of success criteria; smaller surface | |
| You decide | | |

**Q2: Span per Job attempt, or only final completion on retries?**

| Option | Description | Selected |
|--------|-------------|----------|
| Span per Job attempt (Recommended) | Real per-attempt timestamps; retries visible; aligns with state-transition-edge idempotency guard | ✓ |
| Final attempt only | Minimal traces but hides failed attempts, needs suppression state | |
| You decide | | |

**Q3: How is failure detail carried on the span?**

| Option | Description | Selected |
|--------|-------------|----------|
| Status + reason attrs (Recommended) | Status Error + Reason description, plus envelope ExitCode/Reason as queryable attributes | ✓ |
| Span status only | Minimal, not queryable in filter DSL | |
| You decide | | |

**Q4: Emit span when envelope unreadable (envReadOK=false)?**

| Option | Description | Selected |
|--------|-------------|----------|
| Emit degraded span (Recommended) | Always emit at completion edge; absent usage attrs + degradation marker; research pins non-envelope model source | ✓ |
| Skip span when unreadable | ATTR-01 literally airtight but observability holes on misbehaving runs | |
| You decide | | |

---

## ATTR-03 custom-key policy

**Q1: How should ATTR-03 treat TIDE-custom keys with no official-module counterpart?**

| Option | Description | Selected |
|--------|-------------|----------|
| Module + tide.* customs (Recommended) | Spec keys from module; customs renamed into explicit tide.* namespace; rename free at zero call sites | ✓ |
| Module + keep customs as-is | Documented exceptions; gen_ai.* squat persists | |
| Strictly module keys only | Loses artifact-path reference + level discriminator | |
| You decide | | |

**Q2: Pin policy for the pre-1.0 openinference-semantic-conventions module (v0.1.1)?**

| Option | Description | Selected |
|--------|-------------|----------|
| Exact pin + drift test (Recommended) | Guard test asserting module constants equal expected wire values | |
| Exact pin only | go.mod freeze; bumps reviewed via PR diff | ✓ |
| Follow minor (v0.1.x) | Anthropic-SDK convention, but unsafe on v0.x | |
| You decide | | |

**Notes:** User explicitly declined the recommended drift-guard test — do not add one.

---

## Attribute value semantics

**Q1: Where should llm.provider (and llm.system) values come from?**

| Option | Description | Selected |
|--------|-------------|----------|
| Derive from dispatch data (Recommended) | Plumb the provider the manager already knows per dispatch; replaces hardcoded constant; runtime-neutrality | ✓ |
| Hardcode 'anthropic' for now | Matches single-backend reality; second hardcoded site to hunt later | |
| You decide | | |

**Q2: If research confirms prompt_details.cache_* are subsets OF llm.token_count.prompt (not disjoint as TIDE encodes), re-map the split or additive-only?**

| Option | Description | Selected |
|--------|-------------|----------|
| Spec-exact, fix split (Recommended) | Re-map at emission layer per Phoenix's documented formula; free at zero call sites; correct cost math | ✓ |
| Additive only | Smallest diff; risks Phoenix under-reporting prompt tokens/cost | |
| You decide | | |

---

## Claude's Discretion

- Mid-milestone trace shape: deterministic Project-UID TraceID grouping in 42 vs independent roots until Phase 43 (area offered, not selected).
- Span/agent.name naming (`tide.dispatch.<level>` docstring convention as prior); exact model-ID form; how module constants surface in pkg/otelai.
- Which bucket `agent.role`/`agent.name` fall into under D-05 — research fact, apply the policy.

## Deferred Ideas

None — discussion stayed within phase scope. Four keyword-matched todos reviewed and not folded (see CONTEXT.md Deferred section).
