package dispatch

import "context"

// Subagent is the out-of-tree contract for TIDE subagent image runtimes
// (D-A1 / REQ-SUB-01). Out-of-tree image authors implement this interface to
// integrate a custom LLM backend into TIDE without modifying the orchestrator.
//
// In-repo reference implementations:
//   - cmd/stub-subagent — canned test responses, no LLM call (Plan 04).
//   - internal/harness  — cap enforcement + signed-token client + secret
//     redaction + output-path validation (Plan 06). The harness wraps a
//     concrete Subagent implementation selected at startup.
//
// The concrete Anthropic-backed implementation (Phase 3) satisfies this
// interface behind the provider firewall enforced by `make verify-dispatch-imports`.
type Subagent interface {
	// Run executes the task described by in and returns the result envelope.
	// The context deadline should be set to caps.wallClockSeconds + grace; the
	// harness also enforces the cap internally via SIGTERM (HARN-02).
	//
	// A non-nil error indicates a dispatch-level failure (network, I/O);
	// task-level failures (subagent exited non-zero) are expressed via
	// EnvelopeOut.ExitCode and EnvelopeOut.Reason.
	Run(ctx context.Context, in EnvelopeIn) (EnvelopeOut, error)
}
