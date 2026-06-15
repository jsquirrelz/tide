# Phase 19: Template Reorder + Token Minimization - Discussion Log

> **Audit trail only.** Do not use as input to planning, research, or execution agents.
> Decisions are captured in CONTEXT.md — this log preserves the alternatives considered.

**Date:** 2026-06-15
**Phase:** 19-Template Reorder + Token Minimization
**Areas discussed:** Inline TaskUID strategy, Trim risk appetite, Annotation mechanism, Shared-context slot timing

---

## Inline TaskUID strategy

### Q1 — De-volatilizing inline TaskUID paths

| Option | Description | Selected |
|--------|-------------|----------|
| Define paths once in suffix | Instruction body UID-free/abstract; single concrete path mapping in volatile suffix. Pure template edit, no harness change. | ✓ |
| CWD-relative convention | Harness sets pod CWD to task envelope dir; instructions use relative paths, no UID anywhere. Cleanest prefix but touches the in-pod harness. | |
| You decide | Defer to research/planning. | |

**User's choice:** Define paths once in suffix.
**Notes:** Chosen specifically to keep the change a pure template edit and avoid harness scope/risk.

### Q2 — Fate of Level/Role/Provider.* metadata lines

| Option | Description | Selected |
|--------|-------------|----------|
| Drop Level/Role, keep Provider in suffix | Remove redundant Level/Role; keep Provider+TaskUID. | |
| Keep all four in suffix | Move block verbatim, change nothing. | |
| Drop all metadata except TaskUID | Only TaskUID survives. | (resolved via Q3) |

**User's choice:** Other — asked "Explain how Provider.Vendor/Model can matter and if it's really worth keeping?"
**Notes:** Claude explained: not load-bearing in the printed body today (no template branches on it, the model knows its own identity, provenance lives in telemetry); the one real future use is provider-conditional `{{if}}` logic for the OpenAI backend — which keeps `.Provider` on the struct regardless of the printed line. Re-offered with that context (Q3).

### Q3 — What survives in the volatile suffix

| Option | Description | Selected |
|--------|-------------|----------|
| TaskUID only (Recommended) | Drop printed Level/Role + Provider.*; .Provider stays in struct; suffix = TaskUID + path mapping + {{.Prompt}}. | ✓ |
| TaskUID + Provider | Drop Level/Role, keep Provider printed as provenance insurance. | |
| Keep all four in suffix | Move verbatim, defer dropping to trim pass. | |

**User's choice:** TaskUID only.
**Notes:** Each removal gets a PROMPT-03 annotation; eval gate backstops structural integrity.

---

## Trim risk appetite

### Q1 — Trim posture

| Option | Description | Selected |
|--------|-------------|----------|
| Conservative (Recommended) | Pure redundancy + formatting slack only; every directive's semantics preserved. | ✓ |
| Moderate | Conservative + compress verbose explanatory prose into tighter imperative phrasing. | |
| Aggressive | Minimal imperative rewrite, drop rationale, lean on the gate. | |

**User's choice:** Conservative.
**Notes:** Surfaced that the Phase 18 gate is structural-only (no LLM-judge this milestone), so it cannot catch quality regressions from over-trimming — anchors the conservative choice given run #2 quality is the milestone goal.

### Q2 — Confidence check beyond the structural gate

| Option | Description | Selected |
|--------|-------------|----------|
| Diff review + make eval (Recommended) | Per-section commit + annotated diff review + `make eval` token confirmation. No live A/B; semantic judging deferred. | ✓ |
| Add a one-off live A/B | Manual before/after live dispatch on one template; subjective eyeball, costs spend. | |
| Structural gate only | Trust conservative posture + deterministic gate; skip token confirmation. | |

**User's choice:** Diff review + make eval.

---

## Annotation mechanism

### Q1 — Where PROMPT-03 "why-this-line" annotations live

| Option | Description | Selected |
|--------|-------------|----------|
| Inline {{/* */}} + commit msg (Recommended) | Zero-token inline comments (trim-marked so goldens unaffected) for surviving lines; removal rationale in per-section commit message. | ✓ |
| Sidecar ANNOTATIONS.md | Standalone rationale doc; can drift from templates. | |
| Both inline + sidecar | Inline for what stays + sidecar ledger for cascade proofs. | |

**User's choice:** Inline {{/* */}} + commit msg.
**Notes:** Flagged the trim-marker (`{{- -}}`) requirement to keep goldens byte-identical.

---

## Shared-context slot timing

### Q1 — Reserve the slot now or defer to Phase 20

| Option | Description | Selected |
|--------|-------------|----------|
| Reserve as zero-token marker (Recommended) | `{{- /* SharedContext slot — Phase 20 (CACHE-02/03) */ -}}` between instructions and suffix; satisfies PROMPT-01 order literally, clean Phase-20 insertion point. | ✓ |
| Defer entirely to Phase 20 | No marker in Phase 19; Phase 20 inserts the section. | |

**User's choice:** Reserve as zero-token marker.

---

## Claude's Discretion

- Exact compressed wording of the paradigm preamble (keep the load-bearing "read the spec" directive).
- Cross-template DRY via shared Go template partials vs self-contained files (maintainability only — no caching impact).
- Offline ratchet proxy unit (already fixed by Phase 18 D-01a; Phase 19 ratchets numbers down).
- Precise rendered form/layout of the volatile-suffix path mapping (must stay deterministic).

## Deferred Ideas

- `SharedContext` field + identical-per-wave population — Phase 20 (CACHE-02/03).
- Cross-pod prefix-cache verification spike — Phase 20 (CACHE-01).
- Per-level token accounting + cache-hit dashboard panel — Phase 21 (OBSV-01–03).
- LLM-as-judge / semantic-quality scoring — EVAL-F1, later milestone (would enable a safely-aggressive trim later).
