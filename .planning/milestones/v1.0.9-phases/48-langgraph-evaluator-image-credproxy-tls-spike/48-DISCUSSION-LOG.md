# Phase 48: LangGraph Evaluator Image + Credproxy-TLS Spike - Discussion Log

> **Audit trail only.** Do not use as input to planning, research, or execution agents.
> Decisions are captured in CONTEXT.md — this log preserves the alternatives considered.

**Date:** 2026-07-18
**Phase:** 48-langgraph-evaluator-image-credproxy-tls-spike
**Areas discussed:** Minimal-image scope depth, TLS spike fidelity + API, Client-construction posture, Read-only proof fidelity, Jobspec scope boundary

---

## Minimal-image scope depth

| Option | Description | Selected |
|--------|-------------|----------|
| Bare seam shell | Decode in.json → git-read + one read-only bash gate command → one ChatAnthropic call → trivial EnvelopeOut (no findings). Proves seam + TLS + read-only, zero premature schema. | ✓ |
| Placeholder verdict | Same shell but emit a stub gate_decision:"APPROVED" to exercise the Phase-49 output shape early. | |

**User's choice:** Bare seam shell
**Notes:** Keeps the EVAL-01/02-only boundary crisp; Phase 49 owns the verdict schema.

---

## TLS spike fidelity + API

| Option | Description | Selected |
|--------|-------------|----------|
| Standalone + real API | docker-run beside a locally-minted credproxy CA; one real max_tokens=1 ChatAnthropic.invoke() to the real Anthropic API. Full trust chain, fast iteration. | ✓ |
| Standalone + stub upstream | Same, but credproxy forwards to a fake /v1/messages — proves CA handshake without spend/real key. | |
| Full kind PodJob + real API | Real evaluator pod with live credproxy native sidecar (exact prod surface). Highest fidelity, heaviest (kind OOM risk), slowest. | |

**User's choice:** Standalone + real API
**Notes:** The trust chain under test is identical to production; kind-sidecar surface adds only dispatch-integration fidelity (already proven for Node/Go) and can be promoted to a gate later.

---

## Client-construction posture

| Option | Description | Selected |
|--------|-------------|----------|
| Ship defensive factory | Spike measures the plain-SSL_CERT_FILE path; shipped image builds anthropic.Anthropic(http_client=httpx.Client(verify=ssl_context(cafile=...))) → ChatAnthropic(client=...), robust either way. | ✓ |
| Optimistic, patch if it fails | Ship plain ChatAnthropic; wire the http_client= fallback only if the spike fails. | |

**User's choice:** Ship defensive factory
**Notes:** Spike still measures the alone-path for a durable regression signal; matches research's "plan slack for the fallback" (langchain#35843, anthropic-sdk-python#923).

---

## Read-only proof fidelity

| Option | Description | Selected |
|--------|-------------|----------|
| Both static + behavioral | Unit-assert the jobspec (ReadOnly:true mount, no GIT_PAT, ReadOnlyRootFilesystem) AND a lightweight read-only-bind-mount container test attempting commit/push → EROFS/missing-credential. | ✓ |
| Behavioral only | Just the adversarial fixture test. | |
| Static jobspec only | Just the config assertions. | |

**User's choice:** Both static + behavioral
**Notes:** Defense in depth across three enforcement layers; PITFALLS Pitfall 4 point 5 explicitly wants the behavioral proof; behavioral test stays CI-friendly (docker --read-only, no kind).

---

## Jobspec scope boundary (surfaced by the "both" RO-proof choice)

| Option | Description | Selected |
|--------|-------------|----------|
| Add RO jobspec variant now | Phase 48 adds a read-only verifier pod-spec path in jobspec.go — unit-tested and asserted, NOT yet dispatched. Dispatch integration stays Phase 51. | ✓ |
| Image + spike only; defer jobspec to 51 | Ship only the image + standalone spike + behavioral container test; move jobspec variant + static assertions to Phase 51. | |

**User's choice:** Add RO jobspec variant now
**Notes:** Makes EVAL-01's "enforced structurally" real in-repo and gives the static assertion a concrete target, while the Subagent interface + envelope contract stay literally unchanged.

---

## Claude's Discretion

- LangGraph graph internals (`create_react_agent` vs. minimal `StateGraph`), the exact trivial gate command, multi-stage Dockerfile layout, and `pytest`-vs-plain-`python` spike entrypoint.

## Deferred Ideas

- New `"langgraph"` vendor sentinel + `SelfInstruments` registration + `EVALUATOR`-kind span → Phase 51 (OBS-03).
- `VerifyContext` envelope field + `gate_decision` verdict schema + findings persistence → Phase 49.
- `role="verifier"` orchestrator-side Go prompt templates + coverage-not-conservatism → Phase 51 (EVAL-04).
- `TaskReconciler` verifier dispatch + concurrency-gate accounting + `LoopPolicy.BudgetCents` → Phase 51.
- Kind-cluster full-fidelity TLS/dispatch gate — optional promotion of the standalone spike (Phase 51).
- `cache_control` / prompt-caching middleware + `Provider.Params` passthrough (CACHE-F1) — future authoring-migration win.
