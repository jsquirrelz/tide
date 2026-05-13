package harness

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	pkgdispatch "github.com/jsquirrelz/tide/pkg/dispatch"
)

// Runtime is the HARN-06 seam: the in-pod interface that swappable backends
// plug into. Phase 2 uses the stub-subagent; Phase 3 swaps in Claude Code
// without any harness code changes.
//
// Execute runs the task described by in and streams output to stdout/stderr.
// It returns the resource usage accumulated during execution and an error if
// the runtime itself failed (network I/O, process error). Task-level failures
// (non-zero exit) are expressed via the returned error or by the harness
// translating them into EnvelopeOut.Result/Reason fields.
//
// The context's deadline should be set to caps.WallClockSeconds + grace (the
// harness derives this via context.WithTimeout in [Harness.Run]). Implementations
// MUST respect context cancellation.
type Runtime interface {
	Execute(ctx context.Context, in pkgdispatch.EnvelopeIn, stdout, stderr io.Writer) (pkgdispatch.Usage, error)
}

// Harness is the orchestrator that ties caps + Runtime + redact + outputs +
// envelope_io together for a single Task execution.
//
// Use [envelope_io.ReadEnvelopeIn] to populate Envelope from the PVC; call
// [Harness.Run] to execute; use [envelope_io.WriteEnvelopeOut] to persist the
// returned [pkgdispatch.EnvelopeOut].
type Harness struct {
	// Envelope is the fully-decoded input from /workspace/envelopes/{task-uid}/in.json.
	Envelope pkgdispatch.EnvelopeIn

	// Workspace is the absolute path to the per-Task workspace root on the
	// mounted PVC (typically "/workspace").
	Workspace string

	// Runtime is the pluggable execution backend (stub Phase 2; Claude Code
	// Phase 3). Must not be nil.
	Runtime Runtime

	// StdoutDest is where the runtime's stdout is forwarded. Callers should
	// wrap os.Stdout with a [redact.RedactingWriter] (Phase 3 production path).
	StdoutDest io.Writer

	// StderrDest is where the runtime's stderr is forwarded. Same convention
	// as StdoutDest.
	StderrDest io.Writer

	// StartedAt is the wall-clock time used as the baseline for output-path
	// validation (files modified before this time are pre-existing and ignored).
	// If zero, Run sets it to time.Now() before calling Execute.
	StartedAt time.Time
}

// Run orchestrates a single Task execution:
//
//  1. Sets StartedAt if not already set.
//  2. Derives a context.WithTimeout for the wall-clock cap (WallClockSeconds > 0).
//  3. Calls Runtime.Execute(capsCtx, h.Envelope, h.StdoutDest, h.StderrDest).
//  4. Translates context.DeadlineExceeded → cap-hit/wall-clock.
//  5. Propagates non-deadline runtime errors → error result.
//  6. Runs CheckCaps on the returned Usage.
//  7. Runs Validate on the workspace for output-path violations.
//  8. Returns a fully-populated EnvelopeOut.
func (h *Harness) Run(ctx context.Context) (pkgdispatch.EnvelopeOut, error) {
	if h.StartedAt.IsZero() {
		h.StartedAt = time.Now()
	}

	// Step 2: build a wall-clock-capped context if the cap is set.
	capsCtx := ctx
	var cancelCaps context.CancelFunc
	if h.Envelope.Caps.WallClockSeconds > 0 {
		capsCtx, cancelCaps = context.WithTimeout(
			ctx, time.Duration(h.Envelope.Caps.WallClockSeconds)*time.Second)
		defer cancelCaps()
	}

	// Step 3: run the runtime.
	usage, runErr := h.Runtime.Execute(capsCtx, h.Envelope, h.StdoutDest, h.StderrDest)

	// Step 4: wall-clock deadline exceeded.
	if runErr != nil && errors.Is(runErr, context.DeadlineExceeded) {
		return h.buildEnvelopeOut("cap-hit", "wall-clock", 1, pkgdispatch.Usage{}), nil
	}

	// Step 5: non-deadline runtime error.
	if runErr != nil {
		return h.buildEnvelopeOut("error", runErr.Error(), 1, usage), nil
	}

	// Step 6: iteration/token cap check.
	if capErr := CheckCaps(h.Envelope.Caps, usage); capErr != nil {
		var chErr *CapHitError
		if errors.As(capErr, &chErr) {
			return h.buildEnvelopeOut("cap-hit", chErr.Reason, 1, usage), nil
		}
		return h.buildEnvelopeOut("cap-hit", capErr.Error(), 1, usage), nil
	}

	// Step 7: output-path validation.
	violations, valErr := Validate(h.Workspace, h.StartedAt, h.Envelope.DeclaredOutputPaths)
	if valErr != nil {
		return h.buildEnvelopeOut("error", fmt.Sprintf("output-path validation error: %v", valErr), 1, usage), nil
	}
	if len(violations) > 0 {
		reason := strings.Join(violations, "; ")
		return h.buildEnvelopeOut("output-paths-violation", reason, 1, usage), nil
	}

	// Step 8: success.
	return h.buildEnvelopeOut("success", "", 0, usage), nil
}

// buildEnvelopeOut constructs a fully-populated [pkgdispatch.EnvelopeOut] from
// the common fields shared across all result branches.
func (h *Harness) buildEnvelopeOut(result, reason string, exitCode int, usage pkgdispatch.Usage) pkgdispatch.EnvelopeOut {
	return pkgdispatch.EnvelopeOut{
		APIVersion:  pkgdispatch.APIVersionV1Alpha1,
		Kind:        pkgdispatch.KindTaskEnvelopeOut,
		TaskUID:     h.Envelope.TaskUID,
		ExitCode:    exitCode,
		Result:      result,
		Reason:      reason,
		Usage:       usage,
		CompletedAt: time.Now(),
	}
}
