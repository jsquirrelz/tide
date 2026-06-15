# Phase 18: Eval Harness - Discussion Log

> **Audit trail only.** Do not use as input to planning, research, or execution agents.
> Decisions are captured in CONTEXT.md — this log preserves the alternatives considered.

**Date:** 2026-06-15
**Phase:** 18-Eval Harness
**Areas discussed:** Token ratchet policy, `make test-unit` reality, Savings-fixture provenance, LLM-judge scope, Eval architecture split (goldie + count_tokens placement)

---

## Token ratchet policy (EVAL-06)

| Option | Description | Selected |
|--------|-------------|----------|
| Per-template no-growth snapshot | Commit each template's count as testdata; any growth hard-fails `make test`; deliberate reviewed update. Phase 18 freezes at v1.0.1 counts, Phase 19 ratchets down. | ✓ |
| Global % growth budget | Allow up to N% total growth across all five combined. Weaker per-template signal. | |
| Single repo-wide token ceiling | One absolute sum-of-all ceiling. Hides which template regressed. | |

**User's choice:** Per-template no-growth snapshot.
**Notes:** Follow-up surfaced the offline-tokenizer constraint — the offline ratchet must snapshot a deterministic proxy (bytes/runes/words), since `make test` is zero-network and Anthropic has no exact local tokenizer. Authoritative counts come from `make eval`.

---

## `make test-unit` reality (EVAL-03)

| Option | Description | Selected |
|--------|-------------|----------|
| Add `test-unit` target | Add a `test-unit` alias so the documented command literally exists. | |
| Retarget to `make test` + fix wording | Keep `make test` as the eval-test home; correct roadmap wording. | |
| Rename test→test-unit | Rename existing target; highest blast radius. | |
| Add `make eval` (free-text) | No `test-unit`; deterministic gate rides existing `make test`; the one new target is `make eval` for the online count_tokens preflight. | ✓ |

**User's choice:** "Add `make eval`" (free-text). Confirmed reading: no `test-unit` target; deterministic eval tests run under existing `make test`; `make eval` is the single new (online) target. Roadmap `make test-unit` references read as `make test`.
**Notes:** Reconciled in the follow-up architecture discussion (below).

---

## Savings-fixture provenance (EVAL-04)

| Option | Description | Selected |
|--------|-------------|----------|
| Synthetic Usage + edge cases | Hand-built `dispatch.Usage` structs; deterministic, free; parity is pure math. | |
| Capture one real dispatch | Run one real v1.0.1 dispatch, freeze its events.jsonl as canonical fixture. | ✓ |
| Both | Real anchor + synthetic edge cases. | |

**User's choice:** Capture one real dispatch.
**Notes:** No run-1 telemetry is committed in-repo → a one-time minimal real-dispatch capture path is needed (flagged for research). Fixture must populate all four token dimensions (input/output/cache-read/cache-creation) so the realized-savings math is exercised.

---

## LLM-judge scope

| Option | Description | Selected |
|--------|-------------|----------|
| Deterministic-only, judge deferred | Gate is purely deterministic protocol-compliance, zero-network; LLM judge stays EVAL-F1 deferred. | ✓ |
| Add optional non-gating judge now | Scaffold an optional manual judge this milestone. Extra scope + network/flake surface. | |

**User's choice:** Deterministic-only, judge deferred.

---

## Eval architecture split (goldie + count_tokens placement)

User proposed classifying goldie under "eval" (anticipating future LLM-response snapshots / LLM-as-judge) and treating count_tokens as a `bin/` script rather than a test.

**Resolution (approved):** Split by "does it touch the network/model," not by concept.
- goldie + ratchet + protocol-compliance + cost-parity = deterministic, zero-network → live in `internal/eval/` package (named "eval") but run in `make test` so the EVAL-06 regression gate fires automatically on every PR. goldie byte-diffing is the wrong tool for non-deterministic LLM output, so the future judge does NOT pull goldie online.
- count_tokens preflight = online tool, not a test → small command (`cmd/tide-eval/`) behind `//go:build eval`, invoked by `make eval`. Future LLM-as-judge joins this online surface.

**User's choice:** "Approved."

---

## Claude's Discretion

- Offline ratchet proxy unit (bytes vs runes vs words).
- Exact `make eval` command path/name (`cmd/tide-eval/` suggested).
- Canonical EnvelopeIn fixture shape for golden renders (must be deterministic).
- Cheapest real-dispatch capture mechanism for the events.jsonl fixture.

## Deferred Ideas

- LLM-as-judge / semantic-quality scoring — EVAL-F1, later milestone (joins `make eval`, not goldie).
- Template reorder / token trimming — Phase 19.
- `SharedContext` on `EnvelopeIn` — Phase 20.
- Per-level token accounting + cache-hit dashboard panel — Phase 21.
