/*
Copyright 2026 TIDE Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package harness

import (
	"fmt"

	pkgdispatch "github.com/jsquirrelz/tide/pkg/dispatch"
)

// CapHitError is returned by [CheckCaps] when any usage field exceeds the
// corresponding cap limit (HARN-02). The Reason field carries a structured
// code identifying which cap was hit.
//
// Wall-clock caps are NOT checked in CheckCaps — they are enforced via
// context.WithTimeout in [Harness.Run] and reported as Reason="wall-clock"
// when the runtime returns a wrapped context.DeadlineExceeded.
//
// Supported Reason values:
//   - "iterations"    — usage.Iterations > caps.Iterations
//   - "input-tokens"  — usage.InputTokens > caps.InputTokens
//   - "output-tokens" — usage.OutputTokens > caps.OutputTokens
type CapHitError struct {
	// Reason is the structured cap that was exceeded.
	Reason string
}

// Error implements the error interface.
func (e *CapHitError) Error() string {
	return fmt.Sprintf("cap hit: %s", e.Reason)
}

// CheckCaps returns a [*CapHitError] if any usage field exceeds the
// corresponding cap limit; returns nil if all usage is within caps.
//
// Zero-valued cap fields are treated as "unconstrained" — a cap of 0 means
// no limit. This matches the JSON omitempty semantics on [pkgdispatch.Caps]:
// an envelope with no caps section should not artificially limit the runtime.
//
// Wall-clock enforcement is the responsibility of the context passed to
// [Harness.Run] — it is not performed here.
func CheckCaps(caps pkgdispatch.Caps, usage pkgdispatch.Usage) error {
	if caps.Iterations > 0 && usage.Iterations > caps.Iterations {
		return &CapHitError{Reason: "iterations"}
	}
	if caps.InputTokens > 0 && usage.InputTokens > caps.InputTokens {
		return &CapHitError{Reason: "input-tokens"}
	}
	if caps.OutputTokens > 0 && usage.OutputTokens > caps.OutputTokens {
		return &CapHitError{Reason: "output-tokens"}
	}
	return nil
}
