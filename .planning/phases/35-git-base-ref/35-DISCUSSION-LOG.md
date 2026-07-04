# Phase 35: Git Base Ref - Discussion Log

> **Audit trail only.** Do not use as input to planning, research, or execution agents.
> Decisions are captured in CONTEXT.md — this log preserves the alternatives considered.

**Date:** 2026-07-03
**Phase:** 35-Git Base Ref
**Areas discussed:** Accepted ref forms, Failure timing & surface, Recovery & mutability, baseSHA stamping

---

## Todo Cross-Reference

| Option | Description | Selected |
|--------|-------------|----------|
| Fold it | File pointers + solution sketch become CONTEXT.md input | ✓ |
| Reviewed, don't fold | Note as reviewed; rely on REQUIREMENTS.md alone | |

**User's choice:** "Safe to fold in if it doesn't contain private company or PII data."
**Notes:** Verified before folding — the todo names no company, repo, or person (external repo deliberately unnamed per the run-details-stay-private convention). Folded.

---

## Accepted ref forms

| Option | Description | Selected |
|--------|-------------|----------|
| Explicit chain | refs/heads → refs/tags (peel) → full 40-hex SHA; predictable, documentable; matches todo sketch + PITFALLS security guidance | ✓ |
| go-git ResolveRevision | gitrevisions-liberal (HEAD, short SHAs, ~/^ suffixes); least code, fuzzier contract | |
| Chain + short SHAs | Explicit chain plus unambiguous 7+-hex short SHAs | |

**User's choice:** Explicit chain (recommended)

| Option | Description | Selected |
|--------|-------------|----------|
| Accept qualified | refs/-prefixed values resolve verbatim before the chain — disambiguation escape hatch | ✓ |
| Short names only | Reject refs/ forms with a targeted error message | |

**User's choice:** Accept qualified (recommended)

| Option | Description | Selected |
|--------|-------------|----------|
| Unresolvable, documented | Unreachable SHA = same typed condition; docs note the reachability limit | ✓ |
| Attempt targeted fetch | Try fetching the SHA directly (host-dependent, uploadpack.allowAnySHA1InWant) | |

**User's choice:** Unresolvable, documented (recommended)

---

## Failure timing & surface

| Option | Description | Selected |
|--------|-------------|----------|
| Clone Job only | Single resolution site (EnsureRunBranch); controller classifies Job result; no manager→git egress | ✓ |
| Controller preflight + clone Job | ls-remote at first reconcile for instant branch/tag feedback; can't preflight SHAs; two sites drift | |

**User's choice:** Clone Job only (recommended)
**Notes:** Transport not asked — code-grounded decision: clone mode adopts the existing push-mode envelope/termination-log contract (clone mode writes no envelope today, `main.go:257`); new `baseref-unresolvable` reason; Argo CD-style message wording.

---

## Recovery & mutability

| Option | Description | Selected |
|--------|-------------|----------|
| Edit spec, re-attempt | Generation-gated: spec edit clears the condition and re-runs the clone; typo costs one kubectl edit | ✓ |
| Terminal — recreate Project | Condition permanent; delete/recreate to fix | |

**User's choice:** Edit spec, re-attempt (recommended)
**Notes:** Consequence made explicit during discussion: no CEL immutability rule (would block the recovery edit; P10 adoption hazard).

| Option | Description | Selected |
|--------|-------------|----------|
| Docs + observable signal | Field docs + event/condition on post-clone edit; needs as-used-ref stamp for comparison | |
| Docs only | CRD field comment + operator docs; zero extra controller logic | ✓ |

**User's choice:** Docs only — user chose the simpler option over the recommendation.

---

## baseSHA stamping

| Option | Description | Selected |
|--------|-------------|----------|
| Always stamp | Every run (incl. default-HEAD) records the SHA it branched from; Argo CD status.sync.revision pattern | ✓ |
| Only when baseRef set | baseSHA strictly means "resolved baseRef" | |

**User's choice:** Always stamp (recommended)

---

## Session Logistics (parallel discuss agents)

| Option | Description | Selected |
|--------|-------------|----------|
| Local branches, you merge | Each agent commits to its own worktree branch; operator merges locally; all agents skip STATE.md | ✓ |
| Draft PRs | Push + PR per phase | |
| Disable guard, serialize | Turn off bgIsolation and run sessions one at a time | |

**User's choice:** Local branches, you merge (recommended)
**Notes:** Raised because the background-session isolation guard blocked in-place writes and the user disclosed parallel discuss sessions for every phase. STATE.md is the only shared-write file — all agents skip its update; one `state.record-session` after merging trues it up.

## Claude's Discretion

- CEL safe-charset validation shape/strictness (must guard absence)
- Exit-code assignment for `baseref-unresolvable` in the tide-push taxonomy
- Exact condition type/reason identifiers; docs placement for accepted forms
- baseSHA stamping timing (natural spot: same status patch as CloneComplete)

## Deferred Ideas

- Targeted SHA fetch for unreachable commits (host-dependent) — revisit only on operator demand
- Observable signal on post-clone baseRef edits — explicitly declined (docs-only chosen)
- `tide apply --base-ref` CLI flag — not in BASE-01..03; CRD-only this phase
