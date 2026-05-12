// Package dispatch is reserved for Phase 2's Subagent interface (REQ-SUB-01).
//
// Phase 1 lands only this placeholder so reconciler structs can declare a
// `Dispatcher dispatch.Dispatcher` field that is nil in Phase 1 and injected
// in Phase 2. This avoids a Phase 2 refactor when the real interface lands —
// the package path and the field name are committed surface from day one.
//
// Per .planning/phases/01-foundation-crds-pkg-dag-controller-scaffold/01-CONTEXT.md
// "Integration Points": "Future integration seams declared (but not wired)
// in Phase 1."
//
// The Phase 2 contract (per 01-RESEARCH.md and PROJECT.md) is roughly:
//
//	type Dispatcher interface {
//	    Run(ctx context.Context, in EnvelopeIn) (EnvelopeOut, error)
//	}
//
// with the EnvelopeIn / EnvelopeOut types capturing the artifact PVC mount,
// the subagent-runtime selector (claude-code, openai, …), and the result
// envelope path. Phase 1 deliberately does not define those types — locking
// the contract before Phase 2's research lands would invite churn.
package dispatch

// Dispatcher is intentionally empty in Phase 1. Phase 2 (REQ-SUB-01) replaces
// this with the Subagent.Run(ctx, EnvelopeIn) (EnvelopeOut, error) contract.
//
// Reconciler structs that will eventually call a Dispatcher MAY declare a
// `Dispatcher dispatch.Dispatcher` field in Phase 1; the field will be nil
// until Phase 2's main.go wiring injects a concrete impl.
type Dispatcher interface{}
