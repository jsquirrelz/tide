// Package harness is the in-pod orchestrator that wraps every Subagent runtime
// (stub in Phase 2, Claude Code in Phase 3) and enforces the three security
// and correctness guarantees that make per-Task execution safe:
//
//  1. Cap enforcement (HARN-02): wall-clock via context.WithTimeout; iteration
//     and token caps via [CheckCaps] post-Execute. Prevents Pitfall 8 (runaway
//     agent loops burning budget).
//
//  2. Secret-pattern redaction (HARN-04): [redact.RedactingWriter] wraps
//     subagent stdout/stderr with a tail-keep buffer that catches tokens split
//     across Write calls (Pitfall A). Prevents Pitfall 18 (API keys/JWTs in
//     Pod logs).
//
//  3. Output-path validation (HARN-05): [Validate] post-Execute asserts every
//     file written after runStart resolves under a declared output path via
//     filepath.EvalSymlinks + filepath.Rel. Prevents Pitfall 7 (subagent
//     context bleed — artifact writes outside the declared scope).
//
// Role + level flags (HARN-01): [Harness.Run] passes the full [EnvelopeIn] —
// including Role and Level — to the [Runtime]; prompt/tool-allowance selection
// is the Runtime impl's responsibility (stub in Phase 2, Claude Code in Phase 3
// per HARN-06 second half).
//
// HARN-06 seam: the [Runtime] interface is the swap point. Phase 2's
// stub-subagent satisfies the contract for integration tests; Phase 3's Claude
// Code adapter plugs in behind the same interface with zero harness code
// changes.
//
// Phase 2 packaging note: the harness package compiles and ships its full API +
// tests in Phase 2, but [Harness.Run] is NOT yet the container entrypoint for
// Task Pods (Plan 04's stub-subagent binary runs directly). The harness becomes
// the wrapping binary in Phase 3. [Validate] IS called from the controller side
// in Plan 09's handleJobCompletion (Warning #5 fix — wires HARN-05 into the
// dispatch chain without requiring the harness binary as the Pod entrypoint).
//
// Public counterpart: [pkg/dispatch.Subagent] is the orchestrator-facing
// interface; [Runtime] here is the in-pod analog that carries Caps + Usage
// instead of the full EnvelopeIn/Out round-trip.
package harness
