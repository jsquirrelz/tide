---
phase: 48-langgraph-evaluator-image-credproxy-tls-spike
plan: 05
subsystem: infra
tags: [credproxy, tls, spike, langchain-anthropic, credproxy-tls, checkpoint-resolved-pass]

# Dependency graph
requires:
  - phase: 48-04
    provides: ghcr.io/jsquirrelz/tide-langgraph-verifier:test image (docker-build-langgraph-verifier), the runtime the spike executes inside
provides:
  - cmd/tide-langgraph-verifier/spike/tls_spike.py — retained, re-runnable D-06/D-07 spike script (plain ChatAnthropic, binary verdict classification)
  - hack/minttoken/main.go — committed throwaway HMAC signed-token minting CLI (wraps internal/credproxy.Sign)
  - make spike-langgraph-tls (hack/scripts/spike-langgraph-tls.sh) — stands up real credproxy + mints token + runs the spike, guard-checked against ~/.tide/anthropic.key
  - 48-TLS-SPIKE-VERDICT.md — verdict: PASS (operator ran make spike-langgraph-tls live 2026-07-18; SSL_CERT_FILE alone trusted credproxy's CA through real ChatAnthropic — EVAL-02 discharged, no D-07 fallback opened)
affects: [49-loop-policy-gate-decision-schema]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Plain ChatAnthropic construction with api_key set explicitly to a throwaway token, base_url/TLS trust read purely from container env (ANTHROPIC_BASE_URL, SSL_CERT_FILE) via os.environ.setdefault — never a constructor kwarg — for D-07 REVISED fidelity with the shipped skeleton"
    - "Verdict classification by anthropic SDK exception hierarchy: APIStatusError = TLS succeeded (HTTP response received), APIConnectionError = TLS/connection failed (classify by exc.__cause__), unwrapped exception class name never includes secret material"
    - "Standalone spike container sharing credproxy's network namespace (--network container:<ctr>) to work around macOS Docker Desktop's host-loopback restriction — same pattern documented in RESEARCH.md's Networking constraint"

key-files:
  created:
    - cmd/tide-langgraph-verifier/spike/tls_spike.py
    - hack/minttoken/main.go
    - hack/scripts/spike-langgraph-tls.sh
    - .planning/phases/48-langgraph-evaluator-image-credproxy-tls-spike/48-TLS-SPIKE-VERDICT.md
  modified:
    - Makefile

key-decisions:
  - "ANTHROPIC_BASE_URL/SSL_CERT_FILE are read purely by ChatAnthropic's own env-resolution (from_env), never passed as constructor kwargs — the spike's --proxy/TIDE_PROXY_ENDPOINT input only seeds ANTHROPIC_BASE_URL via os.environ.setdefault(), a no-op once the Makefile driver sets it directly via docker run -e (matching how jobspec.go will set it in production)"
  - "hack/minttoken/main.go is committed (not a /tmp-only helper like the Phase-18 precedent) because CONTEXT.md locks the spike as a RETAINED, re-runnable artifact that must re-run on any of the 7 D-10 pin bumps"
  - "Verdict classification keys off the anthropic SDK's own exception hierarchy (APIStatusError vs APIConnectionError via _base_client.py's wrapping) rather than string-matching error messages — confirmed by direct source read that any non-AnthropicError exception during .send() is wrapped into APIConnectionError with the original httpx/ssl exception chained as __cause__"
  - "The driver script places the verifier container in the same network namespace as credproxy (--network container:<credproxy>) rather than trying host-loopback access, per RESEARCH.md's documented macOS Docker Desktop constraint"

patterns-established:
  - "Bash-script-backs-Makefile-target pattern (hack/scripts/*.sh + a one-line @bash Makefile target) reused a third time (test-verifier-readonly.sh, spike-langgraph-tls.sh) for anything beyond simple go run/docker build invocations"

requirements-completed: [EVAL-02]

# Metrics
duration: ~25min (Task 1 only — Task 2 is a checkpoint:human-verify gate, not yet run)
completed: 2026-07-18
---

# Phase 48 Plan 05: Credproxy-TLS Spike Harness (Task 1 of 2) Summary

**Built the retained D-06/D-07 TLS spike harness — a bare-python plain-`ChatAnthropic` script, a committed HMAC token-mint helper, and a `make spike-langgraph-tls` driver that stands up real credproxy and guard-checks `~/.tide/anthropic.key` before any spend — but the live measurement itself (Task 2) has NOT been run.**

## Status: 2/2 tasks complete — CHECKPOINT RESOLVED (PASS)

Task 1 (autonomous, the harness build) completed and committed. Task 2
(`checkpoint:human-verify`, `gate="blocking"`) was resolved by the operator
running `make spike-langgraph-tls` live on 2026-07-18 (durable key confirmed
present). Result: **`TLS-SPIKE: PASS`** — `SSL_CERT_FILE` alone trusted
credproxy's freshly-minted CA through the real `ChatAnthropic` construction
path. EVAL-02 is discharged; the D-07-REVISED bet (plain construction, spike
measures) held; no fallback fork opened. Verdict recorded in
`48-TLS-SPIKE-VERDICT.md` (`verdict: PASS`). Phase 49 is unblocked.

## Performance

- **Duration:** ~25 min (Task 1 only)
- **Completed:** 2026-07-18T19:04:56Z
- **Tasks:** 1/2 completed
- **Files modified:** 5

## Accomplishments
- `cmd/tide-langgraph-verifier/spike/tls_spike.py`: bare python spike script, flag/env-driven (`--token`/`TIDE_SIGNED_TOKEN` required, `--proxy`/`TIDE_PROXY_ENDPOINT` default `https://127.0.0.1:8443`, `--model`/`TIDE_SPIKE_MODEL` default `claude-sonnet-4-6`). Plain `ChatAnthropic(model=..., max_tokens=1, api_key=token)` construction — zero client-injection kwargs, zero pre-flight probe, zero subclass override (D-07 REVISED). Classifies the single real `.invoke("hi")` call into one of three verdict lines (`PASS` / `PASS-TLS-AUTH-FAIL` / `FAIL <error class>`) using the anthropic SDK's own exception hierarchy.
- `hack/minttoken/main.go`: ~40-line committed CLI wrapping `internal/credproxy.Sign` directly — verified end-to-end (throwaway test, not committed) that a minted token round-trips through `credproxy.Verify`.
- `hack/scripts/spike-langgraph-tls.sh` + `make spike-langgraph-tls`: guard-checks `~/.tide/anthropic.key` exists before touching Docker; builds the verifier/credproxy images if absent; stands up real credproxy with a throwaway signing key; mints a token; runs the spike inside `tide-langgraph-verifier:test` sharing credproxy's network namespace; tears the credproxy container down via `trap` on exit.
- `48-TLS-SPIKE-VERDICT.md`: `verdict: PENDING` frontmatter template with the 7 D-10 runtime pins recorded, awaiting Task 2's live evidence.

## Task Commits

1. **Task 1: Spike script + mint helper + make spike-langgraph-tls driver + verdict template** - `3880852` (feat)

Task 2 has no commit yet — it is the pending checkpoint.

## Files Created/Modified
- `cmd/tide-langgraph-verifier/spike/tls_spike.py` - the retained D-06/D-07 spike script
- `hack/minttoken/main.go` - throwaway signed-token minting CLI (wraps `internal/credproxy.Sign`)
- `hack/scripts/spike-langgraph-tls.sh` - the `make spike-langgraph-tls` driver (credproxy standup, token mint, spike run, teardown)
- `Makefile` - new `spike-langgraph-tls` target
- `.planning/phases/48-langgraph-evaluator-image-credproxy-tls-spike/48-TLS-SPIKE-VERDICT.md` - `PENDING` decision-artifact template (created, not yet filled in)

## Decisions Made
- `ANTHROPIC_BASE_URL`/`SSL_CERT_FILE` are read purely via `ChatAnthropic`'s own env-resolution — never passed as constructor kwargs — for exact construction-fidelity with the shipped skeleton (D-07 REVISED); the spike's own `--proxy` input only seeds `ANTHROPIC_BASE_URL` via `os.environ.setdefault()`, a no-op once the Makefile driver sets it directly.
- `hack/minttoken/main.go` is committed, not a `/tmp`-only helper, since the spike must remain re-runnable on any future pin bump (D-10).
- Verdict classification uses the anthropic SDK's actual exception hierarchy (`APIStatusError` vs `APIConnectionError`, confirmed by reading `_base_client.py`'s wrapping logic directly) rather than string-matching error messages, and unwraps `exc.__cause__` to name the underlying httpx/ssl exception class in the FAIL case.
- The driver places both containers in the same Docker network namespace (`--network container:<credproxy>`) per RESEARCH.md's documented macOS Docker Desktop host-loopback constraint.

## Deviations from Plan

None - plan executed exactly as written for Task 1. No Rule 1-4 triggers encountered (no bugs found, no missing critical functionality, no blocking issues, no architectural changes needed). Task 2 was intentionally NOT executed per the operator's explicit objective (live, billable, requires human presence) — this is the plan's own designed checkpoint gate, not a deviation.

## Issues Encountered

None. All Task 1 acceptance criteria were verified without spending: `go build`/`go vet`/`golangci-lint` clean on `hack/minttoken`; a throwaway (uncommitted) round-trip test confirmed a minted token passes `credproxy.Verify`; `tls_spike.py` fails closed with no `TIDE_SIGNED_TOKEN` (verified: exits 1, no network attempted); the make target's guard-check was verified against a fake `$HOME` with no `~/.tide/anthropic.key` present (exits 1, no Docker/network activity); source-assertion greps confirm zero `client=`/`http_client=`/`anthropic_client=`/`create_default_context` occurrences and no secret-value echoes.

## User Setup Required

**Human action required to complete this plan.** Task 2 (`checkpoint:human-verify`, `gate="blocking"`) needs the operator to run the live spike:

1. Confirm the durable key exists: `test -f ~/.tide/anthropic.key && echo present`
2. Run the live spike: `make spike-langgraph-tls` (costs ~fractions of a cent)
3. Read the single `TLS-SPIKE: ...` verdict line printed to stdout
4. Record the outcome in `.planning/phases/48-langgraph-evaluator-image-credproxy-tls-spike/48-TLS-SPIKE-VERDICT.md`: flip `verdict:` to `PASS` or `FAIL`, paste the verdict line + error class (never the token/key) into Evidence, date it
5. On FAIL: do not improvise a fix — the measured error class routes to a fix-shape decision per D-07 REVISED (see 48-05-PLAN.md Task 2's `<how-to-verify>` for the full protocol)

See the CHECKPOINT block returned alongside this summary for the complete resume protocol.

## Next Phase Readiness

- Phase 48 success criterion 2 (the live pass/fail TLS spike) is **not yet discharged** — the harness exists and is verified fail-closed/offline, but the actual measurement is pending human execution.
- Phase 49 is gated on `48-TLS-SPIKE-VERDICT.md` no longer reading `PENDING` (per this plan's own `<success_criteria>`) — do not begin Phase 49 planning until Task 2 resolves.
- No blockers for Task 2 itself: both images (`tide-langgraph-verifier:test`, to be built or already present from 48-04; `tide-credproxy:test`, built on first `make spike-langgraph-tls` run) and the driver script are ready.

## Self-Check: PASSED

- FOUND: cmd/tide-langgraph-verifier/spike/tls_spike.py
- FOUND: hack/minttoken/main.go
- FOUND: hack/scripts/spike-langgraph-tls.sh
- FOUND: .planning/phases/48-langgraph-evaluator-image-credproxy-tls-spike/48-TLS-SPIKE-VERDICT.md
- FOUND: commit 3880852 (Task 1)

---
*Phase: 48-langgraph-evaluator-image-credproxy-tls-spike*
*Completed: 2026-07-18 (Task 1 of 2 — checkpoint pending)*
