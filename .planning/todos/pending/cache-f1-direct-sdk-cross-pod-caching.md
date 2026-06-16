---
status: pending
created: 2026-06-15
area: caching
source: Phase 20 CACHE-01 spike (D-08 scoped follow-up)
relates_to: CACHE-F1
---

# Realize cross-pod cache benefit via a direct-SDK subagent backend (CACHE-F1)

**Why (Phase 20 CACHE-01 spike evidence):** Cross-pod prefix caching *fires* under
`claude -p --bare`, but caller-controlled content (the `-p` prompt, hence
`SharedContext`) does **not** realize a cross-pod cache benefit. Across 3 live runs on
`kind-tide-dogfood`, dispatch B read only the CLI's ~1.1–1.3k-token tool/system scaffold
and re-created the ~11k-token user message every time. The request bodies are
byte-identical across pods **except a per-request-random `cch=<hex>` token** the CLI
front-loads into an `x-anthropic-billing-header` system block ahead of caller content.
The documented `--exclude-dynamic-system-prompt-sections` flag was tested live and did
**not** recover it (the residual cap is the `cch` nonce, which has no CLI suppression
lever). The per-pod `--add-dir`/CWD divergence is **refuted** — it is not in the body.

**The fix (out of scope for the CLI path):** a direct-SDK subagent backend that sets the
system prompt explicitly (no per-request nonce, no dynamic sections) and places
`cache_control` breakpoints on the shared stable prefix — so wave-sibling dispatches share
a cacheable prefix across pods. This is CACHE-F1 (provider-controlled caching), already a
tracked Future Requirement; this todo records the concrete motivation + measured evidence.

**Next steps when picked up:**
1. Prototype a direct-SDK backend (behind the existing `Subagent` interface) that owns the
   request body: static system prompt + `cache_control` on the SharedContext prefix.
2. Re-run the Phase 20 spike harness shape against it; confirm dispatch B `cache_read`
   covers the shared prefix (not just the scaffold).
3. Quantify the realized per-wave savings via the Phase 18 eval harness (`estimatedCostCents`,
   cache-write premium subtracted).
4. Keep it provider-neutral (CACHE-05): the same backend interface should map to OpenAI
   automatic prefix caching / Gemini explicit `CachedContent` / Bedrock `cachePoint` in the
   run-#2 multi-provider milestone.

**Stays true regardless:** SharedContext shipped in Phase 20 (Plans 01–03) and delivers
**token-minimization** (curated summaries < verbatim dumps) on every provider today; this
follow-up is only about the *cache* payoff.
