#!/usr/bin/env python3
"""tls_spike.py — Phase 48 D-06/D-07 EVAL-02 live credproxy-TLS spike.

Proves, live and binary pass/fail, whether SSL_CERT_FILE alone makes the real
ChatAnthropic construction path trust credproxy's freshly-minted self-signed
CA (D-06). Construction here is PLAIN — identical to the shipped
tide-langgraph-verifier skeleton's own ChatAnthropic call (D-07 REVISED: no
defensive client factory, no pre-flight probe, no subclass override, no
client-injection kwargs (none exist at the pinned langchain-anthropic==1.4.8
anyway, see 48-RESEARCH.md Pitfall A). The spike IS the measurement, not a
hardening exercise.

Mirrors cmd/tide-spike/main.go's discipline: flag/env-driven inputs,
fail-closed on missing credentials before any network attempt, and the
signed token value is NEVER logged or printed — only its presence is
reported.

Usage:

    make spike-langgraph-tls                 # driver: stands up credproxy,
                                               # mints a throwaway token, runs
                                               # this script inside the
                                               # tide-langgraph-verifier image
    python tls_spike.py --token=... [--proxy=https://127.0.0.1:8443] [--model=...]
                                               # direct invocation (advanced)

Required inputs (flag, falling back to env; flag wins if both are set):

    --token / TIDE_SIGNED_TOKEN   — HMAC signed token valid for the running
                                     credproxy (REQUIRED, no default — the
                                     script exits 1 before any import of
                                     langchain_anthropic or network access
                                     if this is missing)
    --proxy / TIDE_PROXY_ENDPOINT — credproxy base URL
                                     (default https://127.0.0.1:8443)
    --model / TIDE_SPIKE_MODEL    — model to dispatch against
                                     (default claude-sonnet-4-6, the same
                                     default cmd/tide-spike/main.go uses)

TLS trust itself flows PURELY from the container environment
(ANTHROPIC_BASE_URL, SSL_CERT_FILE) — exactly as the shipped skeleton reads
them from the Job pod spec (jobspec.go), never as a ChatAnthropic
constructor kwarg. --proxy/TIDE_PROXY_ENDPOINT only seeds ANTHROPIC_BASE_URL
via os.environ.setdefault() when the caller hasn't already set it directly;
the Makefile driver sets ANTHROPIC_BASE_URL via `docker run -e` (matching
production exactly), so that setdefault is a no-op in the normal `make
spike-langgraph-tls` path and only matters for a manual/direct invocation.

Verdict line (exactly one of these is printed; exit code follows):

    TLS-SPIKE: PASS                  (exit 0) — full success: the 1-token
                                        completion round-tripped through
                                        credproxy, trusting SSL_CERT_FILE
                                        alone.
    TLS-SPIKE: PASS-TLS-AUTH-FAIL     (exit 1) — the TLS handshake succeeded
                                        (an HTTP-level response was received
                                        from upstream) but the call itself
                                        failed (e.g. a bad/expired real key).
                                        This still answers EVAL-02's TLS
                                        question affirmatively: SSL_CERT_FILE
                                        alone was enough to trust credproxy.
    TLS-SPIKE: FAIL <error class>     (exit 1) — the thing being measured:
                                        no HTTP response was ever received.
                                        Classified by the underlying
                                        exception class (SSLCertVerificationError
                                        / ConnectError / other).
                                        D-06/D-07's genuine unknown resolved
                                        negative — escalate to the fix-shape
                                        decision (48-05-PLAN.md Task 2), do
                                        not improvise a fix here.

The live run + verdict recording in 48-TLS-SPIKE-VERDICT.md is a
checkpoint:human-verify step (Plan 05 Task 2) — this script only needs to
build/parse cleanly and fail closed without spending; the live invoke is
manual per the plan.
"""

from __future__ import annotations

import argparse
import os
import sys


DEFAULT_PROXY_ENDPOINT = "https://127.0.0.1:8443"
DEFAULT_MODEL = "claude-sonnet-4-6"  # same default cmd/tide-spike/main.go uses


def require_flag(name: str, value: str) -> str:
    """Exit 1 with a requireFlag-style stderr message if value is empty.

    Mirrors cmd/tide-spike/main.go's requireFlag: fail closed, no network
    attempted, before any credential is touched.
    """
    if not value:
        print(f"tls_spike: required flag/env --{name} not set", file=sys.stderr)
        sys.exit(1)
    return value


def parse_args(argv: list[str] | None = None) -> argparse.Namespace:
    parser = argparse.ArgumentParser(description="Phase 48 D-06 live credproxy-TLS spike.")
    parser.add_argument(
        "--proxy",
        default=os.environ.get("TIDE_PROXY_ENDPOINT", DEFAULT_PROXY_ENDPOINT),
        help="credproxy base URL (e.g. https://127.0.0.1:8443)",
    )
    parser.add_argument(
        "--token",
        default=os.environ.get("TIDE_SIGNED_TOKEN", ""),
        help="HMAC signed token for credproxy",
    )
    parser.add_argument(
        "--model",
        default=os.environ.get("TIDE_SPIKE_MODEL", DEFAULT_MODEL),
        help="model to dispatch against",
    )
    return parser.parse_args(argv)


def classify_and_report(exc: Exception) -> int:
    """Print the single TLS-SPIKE verdict line for a failed .invoke() and
    return the process exit code.

    NEVER includes token/key material — only the exception class name and
    its (secret-free) string representation. Anthropic SDK exceptions never
    carry the Authorization/x-api-key header value in their message or repr
    (confirmed: internal/credproxy/token.go's signed token travels only in
    request headers, never the body or a raised exception's fields).
    """
    # Deferred import: anthropic is only needed here to classify, keeping the
    # fail-closed require_flag() path import-light.
    import anthropic

    if isinstance(exc, anthropic.APIStatusError):
        # The TLS handshake necessarily succeeded to receive an HTTP-level
        # response at all — this answers EVAL-02's TLS question
        # affirmatively even though the call itself failed (e.g. a
        # bad/expired real key at ~/.tide/anthropic.key).
        print(f"TLS-SPIKE: PASS-TLS-AUTH-FAIL (status={exc.status_code})")
        print(f"  error class: {type(exc).__name__}")
        print("  TLS handshake succeeded (an HTTP response was received from upstream);")
        print("  the failure is at the API/auth layer, not the TLS trust layer.")
        return 1

    if isinstance(exc, anthropic.APIConnectionError):
        # No HTTP response was ever received — the thing being measured.
        # The original httpx/ssl exception is chained as __cause__.
        cause = exc.__cause__ if exc.__cause__ is not None else exc
        error_class = type(cause).__name__
        print(f"TLS-SPIKE: FAIL {error_class}")
        print(f"  error class: {error_class}")
        print("  no HTTP response was received from credproxy — the TLS trust")
        print("  chain (or connection) did not hold with SSL_CERT_FILE alone.")
        return 1

    # Unclassified exception type — still fail closed, still no secrets.
    print(f"TLS-SPIKE: FAIL {type(exc).__name__}")
    print(f"  error class: {type(exc).__name__} (unclassified — not a recognized")
    print("  anthropic.APIStatusError/APIConnectionError subclass)")
    return 1


def main(argv: list[str] | None = None) -> int:
    args = parse_args(argv)

    token = require_flag("token", args.token)
    proxy = require_flag("proxy", args.proxy)
    model = require_flag("model", args.model)

    # Report credential presence only — NEVER the token value itself
    # (mirrors cmd/tide-spike/main.go's "token present: true" discipline).
    print("tls_spike: token present: true")
    print(f"tls_spike: proxy endpoint: {proxy}")
    print(f"tls_spike: model: {model}")
    print()

    # base_url/trust flow PURELY from the container env (ANTHROPIC_BASE_URL,
    # SSL_CERT_FILE) — see module docstring. setdefault() is a no-op when the
    # Makefile driver already set ANTHROPIC_BASE_URL directly via `docker run
    # -e` (production parity); it only seeds the value for a manual/direct
    # invocation of this script.
    os.environ.setdefault("ANTHROPIC_BASE_URL", proxy)

    # Deferred import so --help / fail-closed paths never need
    # langchain_anthropic importable (keeps the argparse/require_flag
    # self-check dependency-light).
    from langchain_anthropic import ChatAnthropic

    # PLAIN construction (D-07 REVISED): no client-injection kwargs
    # (RESEARCH Pitfall A — none exist at this pin anyway), no pre-flight
    # probe, no subclass override. api_key is the one explicit override
    # (the throwaway signed token); everything else — base_url, TLS trust —
    # is identical to what the shipped skeleton's own construction does.
    llm = ChatAnthropic(model=model, max_tokens=1, api_key=token)

    print("tls_spike: making one real max_tokens=1 .invoke() call...")
    try:
        llm.invoke("hi")
    except Exception as exc:  # noqa: BLE001 - classified by type below, never swallowed
        return classify_and_report(exc)

    print("TLS-SPIKE: PASS")
    print("  SSL_CERT_FILE alone trusted credproxy's CA through the real ChatAnthropic path.")
    return 0


if __name__ == "__main__":
    sys.exit(main())
