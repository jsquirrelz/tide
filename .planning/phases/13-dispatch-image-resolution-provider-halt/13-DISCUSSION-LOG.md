# Phase 13: Dispatch Image Resolution + Provider Halt - Discussion Log

> **Audit trail only.** Do not use as input to planning, research, or execution agents.
> Decisions are captured in CONTEXT.md — this log preserves the alternatives considered.

**Date:** 2026-06-11
**Phase:** 13-Dispatch Image Resolution + Provider Halt
**Areas discussed:** Chart image default posture, Billing-400 detection point, Halt semantics + recovery

---

## Chart image default posture

| Option | Description | Selected |
|--------|-------------|----------|
| Drop the flag; claude default | Remove --subagent-image from deployment args; resolution falls to Levels.Image → Spec.Subagent.Image → subagent.defaults.image (claude, already shipped); stub opt-in for tests | ✓ |
| Keep flag, point it at claude | Same default outcome, keeps flag plumbing and two competing helm values | |
| Keep stub default | Unpinned Projects still silently get the stub | |

**User's choice:** Drop the flag; claude default (recommended option)

---

## Billing-400 detection point

| Option | Description | Selected |
|--------|-------------|----------|
| Both: credproxy fast-path + envelope classify | Proxy halts at FIRST 400 before siblings burn; reconciler envelope classification as backstop and cheap test layer | ✓ |
| Credproxy only | Single detection point; non-credproxy paths blind | |
| Envelope classification only | Simplest; each first-failure session burns its full context ramp | |

**User's choice:** Both (recommended option)

---

## Halt semantics + recovery

| Option | Description | Selected |
|--------|-------------|----------|
| Stop dispatch + fail-fast in-flight; manual resume | BillingHalt blocks all new dispatch; in-flight fail fast (no kill); levels park not Failed-cascade; clears via tide resume after refilling credits | ✓ |
| Stop new dispatch only, auto-probe recovery | Hands-off but spends probe calls against an empty balance, can flap | |
| Hard stop: kill in-flight Jobs | Cleanest cluster state; loses partial work for sessions that die in seconds anyway | |

**User's choice:** Stop dispatch + fail-fast in-flight; manual resume (recommended option)

---

## Claude's Discretion

- credproxy→controller reporting channel mechanics (within envelopes-as-artifacts rules)
- Condition type/reason naming (BillingHalt working name)
- Whether the billing dispatch hold shares a helper with Phase 12's holds
- Error-signature matching robustness (don't overfit one message string)
- Kind-harness stub wiring mechanism after the flag drop

## Deferred Ideas

- Provider-key budget on dashboard (COST-02)
- Auto-probe billing-halt recovery (rejected: never spend API calls testing an empty balance)
