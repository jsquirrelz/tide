---
verdict: PENDING
date:
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

_(Paste the single `TLS-SPIKE: ...` verdict line + error class here. NEVER the
signed token or the real API key.)_

## Implication

_(PASS → EVAL-02 discharged, zero defensive code needed in the shipped image;
D-06/D-07's genuine unknown resolved positive.)_

_(FAIL → the measured error class routes to a human fix-shape decision per
D-07 REVISED — the minimal fix is designed against the MEASURED error, not
improvised defensively. Record the fork surfaced and the decision made.)_

---
*Phase: 48-langgraph-evaluator-image-credproxy-tls-spike*
*Plan: 05*
