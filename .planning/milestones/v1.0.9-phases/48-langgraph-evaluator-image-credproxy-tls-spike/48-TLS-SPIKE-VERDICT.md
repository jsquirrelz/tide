---
verdict: PASS
date: 2026-07-18
pins:
  langgraph: "1.2.9"
  langchain: "1.3.14"
  langchain-anthropic: "1.4.8"
  langchain-core: "1.4.9"
  anthropic: "0.117.0"
  pydantic: "2.13.4"
  httpx: "0.28.1"
---

# Phase 48 D-06/EVAL-02: Live Credproxy-TLS Spike Verdict

**Question:** Does `SSL_CERT_FILE` alone make the real `ChatAnthropic` construction
path (identical to the shipped `tide-langgraph-verifier` skeleton's own
construction — D-07 REVISED) trust credproxy's freshly-minted self-signed CA?

**How measured:** `make spike-langgraph-tls` — stands up the real
`internal/credproxy` binary (fresh self-signed CA, real key injection from
`~/.tide/anthropic.key`, hardcoded route allowlist, unchanged), mints a
throwaway HMAC signed token via `hack/minttoken`, and runs
`cmd/tide-langgraph-verifier/spike/tls_spike.py` inside the real
`tide-langgraph-verifier` image — one real `max_tokens=1` `ChatAnthropic.invoke()`.

## Evidence

Operator ran `make spike-langgraph-tls` live on 2026-07-18 (durable key at
`~/.tide/anthropic.key` confirmed present). Verbatim result:

```
tls_spike: token present: true
tls_spike: proxy endpoint: https://127.0.0.1:8443
tls_spike: model: claude-sonnet-4-6
tls_spike: making one real max_tokens=1 .invoke() call...
TLS-SPIKE: PASS
  SSL_CERT_FILE alone trusted credproxy's CA through the real ChatAnthropic path.
```

(No signed token or API key printed — the harness never logs either.)

## Implication

**PASS → EVAL-02 discharged.** `SSL_CERT_FILE` alone trusts credproxy's
freshly-minted self-signed CA through the real `ChatAnthropic` construction
path — the exact construction the shipped `tide-langgraph-verifier` skeleton
uses (D-07 REVISED). The genuine unknown flagged by `langchain#35843` /
`anthropic-sdk-python#923` resolved **positive** at the pinned versions: the
Anthropic SDK's custom httpx transport honors `SSL_CERT_FILE`. **Zero
defensive code** (no probe, no subclass) is needed in the shipped image, and
no D-07 fallback fork is opened. Phase 49 is unblocked.

The spike harness is retained (`make spike-langgraph-tls`) — re-run on any pin
bump to re-confirm this holds against a new transport version.

---
*Phase: 48-langgraph-evaluator-image-credproxy-tls-spike*
*Plan: 05*
