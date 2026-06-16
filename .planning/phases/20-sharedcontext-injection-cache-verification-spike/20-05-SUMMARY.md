# Plan 20-05 Summary — CACHE-01 decision + CACHE-05 + D-08 resolution

**Status:** Complete
**Requirements:** CACHE-01, CACHE-05
**Self-Check:** PASSED

## What was done

Ran the Plan-04 spike **live** on `kind-tide-dogfood` (real Anthropic API via credproxy,
`claude-sonnet-4-6`), investigated the result to root cause, recorded the CACHE-01 decision
in PROJECT.md, verified CACHE-05 provider-neutrality, and resolved the D-08 contingency as a
scoped follow-up. No `subagent.go` change, no chart change.

## CACHE-01 verdict (3 live runs + official docs + flag test)

- **Cross-pod caching fires** under `claude -p --bare` (dispatch B `cache_read=1307 > 0`),
  reproducibly. **The original blocker is REFUTED**: the per-pod `--add-dir`/CWD path is NOT
  in the request body — pod A and pod B bodies are byte-identical except a per-request-random
  `cch=<hex>` token in an `x-anthropic-billing-header` system block.
- **But caller content does NOT cross-pod cache-read.** B read only the CLI's ~1.1–1.3k-token
  tool/system scaffold; the ~11k-token user message (our prompt → SharedContext) is re-created
  every dispatch (`1307 + 10989 = 12296`).
- Official Claude Code caching docs confirm the cache is "scoped to one machine and directory"
  (system prompt embeds CWD/git/platform ahead of caller content). The documented fix flag
  `--exclude-dynamic-system-prompt-sections` was **tested live and did not recover** caller-content
  caching (B still `read=1061`/`create=12372`); the residual cap is the `cch` nonce, which has
  **no CLI suppression lever**.
- **Reframe:** SharedContext ships as **token-minimization**; cross-pod *cache* benefit on
  caller content is not achievable on the CLI path → deferred to a direct-SDK backend (CACHE-F1).

## CACHE-05 (provider-neutrality)

Grep across `envelope.go`, `childcrd.go`, `dispatch_helpers.go`, and the four planner templates
returned **zero** vendor-coupled SharedContext lines. SharedContext is a plain stable-prefix
string on the provider-agnostic `EnvelopeIn`. OpenAI/Codex live parity deferred to run-#2.
Per-provider floor table recorded in PROJECT.md (D-06 — never hardcode 1,024).

## D-08 resolution: scoped follow-up

No contained in-phase fix exists — the `--add-dir` normalization candidate is refuted, the
`--exclude-dynamic-system-prompt-sections` flag does not recover caching, and the `cch` nonce
is CLI-internal with no knob. Filed `.planning/todos/pending/cache-f1-direct-sdk-cross-pod-caching.md`
and annotated CACHE-F1 in REQUIREMENTS.md with the measured evidence. `subagent.go` untouched;
`charts/tide/values.yaml` untouched.

## Key files

- `.planning/PROJECT.md` — CACHE-01 decision row + full decision record + floor table
- `.planning/REQUIREMENTS.md` — CACHE-F1 annotated with the spike evidence
- `.planning/todos/pending/cache-f1-direct-sdk-cross-pod-caching.md` — scoped follow-up

## Verification

- `go build ./...` exits 0; full unit suite green (dispatch, controller, reporter, subagent, eval).
- No `charts/tide/values.yaml` change; no `subagent.go` change.
- PROJECT.md records the CACHE-01 decision (criterion 1) + CACHE-05 deferral (criterion 5).

## Deviation from plan

Task 1's `make eval` floor measurement was **not** run live: the spike conclusively shows
caller content does not cache cross-pod on the CLI path regardless of whether the prefix clears
the floor, making a live floor measurement moot for the cache decision. The floor analysis is
recorded from Phase 18/19 data and the per-provider table; precise per-template live measurement
folds into the CACHE-F1 follow-up. The live spike budget was instead spent (with maintainer
approval) on root-causing the cap and testing the `--exclude-dynamic-system-prompt-sections` fix.
