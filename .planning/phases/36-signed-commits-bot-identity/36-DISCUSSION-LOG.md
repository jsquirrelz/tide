# Phase 36: Signed Commits + Bot Identity - Discussion Log

> **Audit trail only.** Do not use as input to planning, research, or execution agents.
> Decisions are captured in CONTEXT.md — this log preserves the alternatives considered.

**Date:** 2026-07-03
**Phase:** 36-Signed Commits + Bot Identity
**Areas discussed:** Phase scope (is signing worth it), Identity configuration surface, Rename depth, Descope bookkeeping

---

## Area selection

All four proposed gray areas were selected: Harness key exposure (ASK-FIRST), Identity & signing config surface, Key-validation failure semantics, Verified-badge docs & UAT recipe. The scope discussion below mooted areas 1, 3, and 4.

## Phase scope — "Is this even worth it?"

While reviewing the harness key-exposure options (mount-with-documented-risk vs controller-sites-only vs restructure), the user stepped back and asked whether the signing feature was worth it at all. Assessment presented: SIGN-01 (identity) is unambiguously worth it; SIGN-02/03/04 (signing) is speculative — the branch-protection motivation is hypothetical, the cost (gpg-shim spike, key-exposure design, external UAT) is real, and deferral carries no compounding penalty.

| Option | Description | Selected |
|--------|-------------|----------|
| Identity only, defer signing (Recommended) | Phase shrinks to SIGN-01; SIGN-02/03/04 move to deferred backlog with spike findings noted | ✓ |
| Full signing as scoped | Keep SIGN-01..04, accept spike + documented-risk mount + manual GitHub UAT | |
| Defer the whole phase | Remove Phase 36 from v1.0.7 entirely | |

**User's choice:** Identity only, defer signing
**Notes:** This resolved the harness key-exposure question (no key → no exposure), key-validation semantics (moot), and Verified-badge docs/UAT (defer with signing). The key-exposure trade-off analysis is preserved in CONTEXT.md Deferred Ideas.

## Identity configuration surface

| Option | Description | Selected |
|--------|-------------|----------|
| Project spec + chart default (Recommended) | spec.git fields → chart values → compiled-in default; matches image-chain precedence | ✓ (with rename) |
| Chart values only | Cluster-wide env, no CRD change | |
| Project spec only | CRD field with compiled-in fallback, no chart default | |

**User's choice:** "Let's rename to agentName and agentEmail" — accepted the recommended precedence shape with the fields renamed from botName/botEmail to agentName/agentEmail.

## Rename depth

| Option | Description | Selected |
|--------|-------------|----------|
| Everything, incl. default identity | TIDE_AGENT_* env, agent.* chart values, default becomes "TIDE Agent <tide-agent@tideproject.k8s>" | ✓ |
| Config surface only, keep default | Agent-named config, bot-flavored compiled-in default (zero behavior change) | |
| CRD fields only | Only the new spec fields use agent naming | |

**User's choice:** Everything, incl. default identity
**Notes:** Accepted the one-time committer-identity change for unconfigured installs.

## Descope bookkeeping

| Option | Description | Selected |
|--------|-------------|----------|
| I update roadmap + requirements now | ROADMAP.md Phase 36 entry + REQUIREMENTS.md traceability amended this session | ✓ |
| CONTEXT.md only, docs later | User runs /gsd-phase edit 36 before planning | |

**User's choice:** Update roadmap + requirements in this session.

## Claude's Discretion

- Exact chart value nesting for agent identity (avoiding the `signingKey` HMAC collision)
- CEL validation on the new CRD fields
- Docs placement for the identity-config note

## Deferred Ideas

- GPG signing (SIGN-02/03/04) wholesale: gpg-shim vs plumbing spike, key-exposure options analysis (A/B/C), ProtonMail/go-crypto promotion, external Verified-badge UAT — full analysis preserved in CONTEXT.md `<deferred>`.
- The pending todo `2026-07-03-signed-commits-verified-badge.md` only half-resolves with this phase; retag/split its signing half.
