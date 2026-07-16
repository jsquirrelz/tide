# Phase 44: LLM Message-Array Spans + D-O5 Redaction/Size Boundary - Discussion Log

> **Audit trail only.** Do not use as input to planning, research, or execution agents.
> Decisions are captured in CONTEXT.md — this log preserves the alternatives considered.

**Date:** 2026-07-16
**Phase:** 44-LLM Message-Array Spans + D-O5 Redaction/Size Boundary
**Areas discussed:** Span granularity, Level coverage, Truncation semantics, Failure posture

---

## Span granularity

### Q1 — What does one LLM span represent?

| Option | Description | Selected |
|--------|-------------|----------|
| Per API call (Recommended) | One LLM span per message_start..message_stop cycle (~32 for the big fixture); mirrors native openinference-instrumentation-langchain output (runtime-neutrality); cost: input context repeats across calls | ✓ |
| Per Task session | One span, full transcript once; compact but doesn't mirror native instrumentation, per-call cost lost | |
| Per assistant turn, delta input | Per-turn spans with only-new-messages input; bounded but deviates from OpenInference semantics | |

**User's choice:** Per API call

### Q2 — Non-text content (tool_use / tool_result / thinking) mapping

| Option | Description | Selected |
|--------|-------------|----------|
| Spec encoding if module has it (Recommended) | Research checks v0.1.1 module for message.tool_calls keys; spec-native if present, stringified fallback; thinking excluded unless module names it | ✓ |
| Stringify everything into content | All blocks as readable text; simple but not structurally queryable, bloats faster | |
| Text only, tools skipped | Smallest payload; loses the majority of agentic Task activity | |

**User's choice:** Spec encoding if module has it

### Q3 — Token counts on per-call LLM spans

| Option | Description | Selected |
|--------|-------------|----------|
| Per-call counts + rollup check (Recommended) | Each LLM span carries that call's usage (D-08 pre-sum); research verifies Phoenix aggregation for double-count risk vs parent AGENT totals | ✓ |
| AGENT span totals only | No double-count risk but per-call spans render $0.00 | |

**User's choice:** Per-call counts + rollup check

### Q4 — Timing fallback when events carry no timestamps

| Option | Description | Selected |
|--------|-------------|----------|
| Real if present, else proportional (Recommended) | Research verifies schema; fallback divides Job window proportionally, marked-synthetic | |
| Zero-duration at ordinal offsets | Honest but degenerate waterfalls | |
| You decide | Planner/researcher picks; marked-synthetic floor | ✓ |

**User's choice:** You decide

---

## Level coverage

### Q1 — Task-only or all levels?

| Option | Description | Selected |
|--------|-------------|----------|
| Task-only, seam-ready (Recommended) | Roadmap letter; tracesynth level-agnostic by construction; planner adoption deferred | |
| All five levels now | Planner materialization runs also synthesize; richest Phoenix picture; wider blast radius | ✓ |

**User's choice:** All five levels now — deliberate extension past MSG-01's letter; formal gate stays bound to Task.

### Q2 — Failure parity for message spans

| Option | Description | Selected |
|--------|-------------|----------|
| Failure parity (Recommended) | Failed Jobs at every level spawn trace-only reporter; tolerant of truncated files; degraded marker | ✓ |
| Success-only | Simpler but skips the highest-value debugging case | |

**User's choice:** Failure parity

### Q3 — Skip trace-only spawns when no OTLP endpoint?

| Option | Description | Selected |
|--------|-------------|----------|
| Skip spawn when unset (Recommended) | Manager checks the endpoint config it forwards; zero churn on plain clusters | ✓ |
| Always spawn | Uniform codepaths at the cost of an extra pod per Task everywhere | |

**User's choice:** Skip spawn when unset

### Q4 — Planner success path: ride materialization run or separate spawn?

| Option | Description | Selected |
|--------|-------------|----------|
| Ride the existing run (Recommended) | One reporter Job does both; trace-only spawns only where no materialization run exists | ✓ |
| Separate trace-only spawn everywhere | Clean isolation; second pod per planner completion | |

**User's choice:** Ride the existing run

---

## Truncation semantics

**Stated up front (not asked):** redaction runs BEFORE truncation — truncating first can split a secret so patterns no longer match. Unchallenged; locked.

### Q1 — What survives above the byte threshold?

| Option | Description | Selected |
|--------|-------------|----------|
| Head-keep (Recommended) | First N bytes + marker; simplest, matches vendor norm; tail (errors) gets cut | |
| Head+tail, middle elided | Both ends kept; conversations carry signal at both ends; two constants | ✓ |
| Marker-only above threshold | Size+pointer only; harshest | |

**User's choice:** Head+tail, middle elided

### Q2 — Whole-span budget backing the per-message cap?

| Option | Description | Selected |
|--------|-------------|----------|
| Both caps (Recommended) | Secondary total-span budget; no single span can sink a batch | |
| Per-message cap only | One constant + documented math | |
| Other | User asked: "Why aren't messages being sent more often and being aggregated with a trace_id?" → answered (aggregation by trace_id already is the design; 4 MB pressure is per-span; live emission blocked by black-box runtime, redaction boundary, stateless reconcilers). User then: "I think we might be modeling this wrong. Can we defer this to research?" | ✓ |

**User's choice:** Deferred to research, model included — research validates the size-bounding model itself against real fixtures, with license to overturn the per-call full-context assumption; constants from fixture data; findings that ripple into granularity surface before planning locks.

---

## Failure posture

### Q1 — Exit status on synth failure

| Option | Description | Selected |
|--------|-------------|----------|
| Best-effort exit 0 everywhere (Recommended) | Observability never gates; no new exit class; no duplicate-span retries; cost: pipeline breakage visible only in logs | ✓ |
| Trace-only runs fail visibly | Non-zero + retries; needs Phoenix dedupe verification first | |

**User's choice:** Best-effort exit 0 everywhere

### Q2 — Parse strictness

| Option | Description | Selected |
|--------|-------------|----------|
| Tolerant-skip with marker (Recommended) | Skip bad lines, emit what reconstructs, degraded marker; matches ParseStream posture | ✓ |
| Strict all-or-nothing | No partial traces, but forfeits failed-Job debugging traces | |

**User's choice:** Tolerant-skip with marker

### Q3 — Flush discipline bound

| Option | Description | Selected |
|--------|-------------|----------|
| Bounded flush timeout (Recommended) | Shutdown under a context deadline (seconds); hung collector can't wedge pipeline | ✓ |
| Block until exported | Maximal delivery; wedges pods on collector outage | |
| You decide | Planner picks | |

**User's choice:** Bounded flush timeout

---

## Claude's Discretion

- Span timing shape when events carry no timestamps (marked-synthetic floor)
- Trace-only mode invocation surface (flag vs env var)
- Non-streaming redaction helper placement (`redact.String` suggested)
- Guard-test update mechanics (`attrs.go` unchanged is the research prior)
- LLM span naming; flush-timeout constant
- RBAC/chart deltas for the wider reporter role

## Deferred Ideas

None — the one boundary extension (all-level coverage) was folded into scope as D-02. Four keyword-matched todos carried forward as reviewed-not-folded with Phase 42's dispositions.
