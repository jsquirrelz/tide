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

package dispatch

// TerminalReason is the machine-checkable exit classification for a single
// Execution-loop attempt (EXEC-02). The set is exactly:
//
//	completed | cap_exceeded | blocked | tool_failure | invalid_output
//
// — no other value is ever valid. The zero value ("") is an INVALID, unset
// sentinel: it is NEVER a silent default and MUST NEVER be read as
// "completed". Every executor exit path MUST set this field explicitly
// (D-02); consumers MUST treat an unset or unrecognized TerminalReason as NOT
// completed — fail-closed, mirroring [ClassifyVerdict]'s never-collapses-to-
// APPROVED discipline (verdict.go).
type TerminalReason string

const (
	// TerminalReasonCompleted reports only that the agent BELIEVES this
	// attempt is complete (EXEC-04) — it is NON-AUTHORITATIVE for Task
	// correctness. Correctness is exclusively the Task loop's call (the
	// Phase-51 verifier); no field or code path in the Execution loop may
	// treat this value as a stamp of Task correctness.
	TerminalReasonCompleted TerminalReason = "completed"

	// TerminalReasonCapExceeded means a wall-clock, token, or iteration cap
	// fired before the agent finished.
	TerminalReasonCapExceeded TerminalReason = "cap_exceeded"

	// TerminalReasonBlocked means a policy/output-path violation or a gate
	// block prevented the attempt from producing a change.
	TerminalReasonBlocked TerminalReason = "blocked"

	// TerminalReasonToolFailure means a tool subprocess or exec call failed
	// (e.g. worktree setup, commit, the subagent process itself).
	TerminalReasonToolFailure TerminalReason = "tool_failure"

	// TerminalReasonInvalidOutput means the agent's output (or the envelope
	// input it was given) was unparseable or schema-invalid.
	TerminalReasonInvalidOutput TerminalReason = "invalid_output"
)

// Valid reports whether t is exactly one of the 5 defined TerminalReason
// values. The zero value ("") and any unrecognized string both return false —
// there is no default-true case, so a forgetful caller cannot mistake an
// unset reason for a valid one.
func (t TerminalReason) Valid() bool {
	switch t {
	case TerminalReasonCompleted, TerminalReasonCapExceeded, TerminalReasonBlocked, TerminalReasonToolFailure, TerminalReasonInvalidOutput:
		return true
	default:
		return false
	}
}
