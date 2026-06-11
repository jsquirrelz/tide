---
title: Codex --output-schema as schema-constrained child-CRD emission
trigger_condition: Codex runtime implementation starts, OR Phase 11's schema-constrained emit_child work unblocks (claude --bare)
planted_date: 2026-06-10
---

# Codex `--output-schema` as schema-constrained child-CRD emission

Codex CLI natively supports `--output-schema <file>`, constraining the final agent
response to a JSON Schema. That is exactly the airtight child-CRD emission fix that is
*blocked* on the Claude side (MCP `emit_child` waiting on `claude --bare` —
see memory/Phase 11 routing).

Implications when the trigger fires:

- The Codex runtime may leapfrog the Claude runtime on emission robustness from day one —
  wire `--output-schema` to the ChildCRDSpec JSON schema instead of reusing the
  json.Decoder + per-file-isolation parser path.
- Keep emission strategy **per-runtime**, not shared: each `Subagent` impl picks the most
  airtight mechanism its CLI offers (Codex: output-schema; Claude: parser+prompt until
  `--bare` lands, then MCP emit_child).
- If Codex emission proves materially more reliable, that's evidence for prioritizing the
  Claude `--bare`/MCP path rather than tolerating the parser.
