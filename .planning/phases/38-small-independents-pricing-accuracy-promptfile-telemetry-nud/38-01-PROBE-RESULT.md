# COST-03 Probe Result — 2026-07-11, operator's local minikube host (teed credproxy, host CLI)

D-05/D-06 evidence gate for the phase-38 pricing rows (D-07). The probe was run
2026-07-11 by the orchestrator with the operator's explicit permission
("You have permission to run the recipe above"), superseding the D-05/D-06
operator-only lock for this run. The operator updated `~/.tide/secrets` for the
real key; the dispatch went through a locally-built teed credproxy
(`--tee-body-dir`, 0700 mktemp dir per the internal/credproxy/server.go contract).

## Probe report (verbatim)

- Distinct `cache_control` values observed (verbatim, from
  `grep -ho '"cache_control":{[^}]*}' "$TEE_DIR"/req-*.json | sort -u`
  across req-1.json and req-2.json):

  ```
  "cache_control":{"type":"ephemeral"}
  ```

  That was the ONLY distinct shape. NO `"ttl":"1h"` appeared anywhere — unambiguous.
- `claude --version`: 2.1.207 (Claude Code)
- Model: claude-haiku-4-5
- Date: 2026-07-11
- Vehicle: HOST CLI via direct `claude -p --bare` invocation with the production
  flag set copied from internal/subagent/anthropic/subagent.go:285-294
  (`-p`, `--model`, `--output-format stream-json`, `--verbose`,
  `--include-partial-messages`, `--permission-mode acceptEdits`, `--add-dir`,
  `--bare`), dispatched through a locally-built teed credproxy
  (`--tee-body-dir`, 0700). This is the recipe's step-2 variant adapted to a
  direct CLI call because the tide-spike wrapper hardcodes
  `NODE_EXTRA_CA_CERTS=/etc/tide/proxy/ca.crt` (root-owned path, unavailable).
- Supplemental usage evidence from the response stream:
  `"usage":{"input_tokens":9,"cache_creation_input_tokens":9525,"cache_read_input_tokens":0,"output_tokens":40}`
  — a real cache write occurred under the ephemeral (no-ttl) shape.
- Hygiene: tee dir, cert dir, and probe output files were deleted after the
  verdict was read; the credproxy was stopped.

## Evidence-grade caveat

The probe exercised the HOST `claude` CLI (v2.1.207), NOT the subagent image's
pinned CLI, though with the identical production flag set TIDE dispatches with.
D-05's ideal ("the subagent image's `claude` CLI") was not met exactly; if the
image's pinned CLI version ever diverges in cache-TTL behavior, re-run the probe
via the recipe's step-3 (docker image) variant. D-08 keeps the flip a one-line
constant change by design.

## Verdict

All observed `cache_control` objects were `{"type":"ephemeral"}` with no `ttl`
key → 5-minute cache-write TTL → 1.25× cache-write pricing.

verdict: cacheWriteMultiplier = 125/100

Not mixed — every breakpoint requested the same 5m shape; no `mixed-ttl` flag.
Plan 38-06's `cacheWriteMultiplier` constant comment cites this artifact (D-08).
