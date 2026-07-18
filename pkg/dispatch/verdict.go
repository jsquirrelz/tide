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

import "encoding/json"

// Verdict is the terminal classification of a verifier's gate_decision
// (EVAL-03). The set is exactly APPROVED | REPAIRABLE | BLOCKED — no other
// value is ever produced by [ClassifyVerdict].
type Verdict string

const (
	// VerdictApproved means the verifier judged the candidate change correct
	// and complete. Never reached by an unparseable or ambiguous
	// gate_decision — [ClassifyVerdict] only returns this for an exact,
	// well-formed "APPROVED" match (D-04).
	VerdictApproved Verdict = "APPROVED"

	// VerdictRepairable means the verifier found deviations that a fresh
	// repair attempt can address. This is the signal that drives the Task
	// loop's core iteration (LoopPolicy.MaxIterations, Phase 51).
	VerdictRepairable Verdict = "REPAIRABLE"

	// VerdictBlocked is the escalation terminal and the fail-closed default.
	// Every empty, missing-verdict-field, or malformed gate_decision
	// classifies here — never to VerdictApproved (D-04 / the 2026-07-03
	// silent-Complete incident this milestone exists to prevent).
	VerdictBlocked Verdict = "BLOCKED"
)

// Finding is a single deviation the verifier reported. Fields are FREE
// STRINGS this phase (coverage-not-conservatism: every deviation is tagged,
// and policy — not the finder — decides what blocks; typed enums are
// deferred to Phase 51 EVAL-04 per RESEARCH Open Question 2).
//
// Severity currently has no enforced vocabulary, but the high-severity token
// "blocker" is the value [pkg/dispatch.TerminationStub]'s HighSeverityCount
// keys off (Phase 51 owns retuning this rubric).
type Finding struct {
	// Dimension names the axis of correctness this finding concerns (e.g.
	// "security", "correctness", "test-coverage"). Free string.
	Dimension string `json:"dimension,omitempty"`

	// Severity is the finding's severity (e.g. "blocker", "advisory"). Free
	// string. The literal "blocker" is the currently-expected high-severity
	// token TerminationStub.HighSeverityCount counts.
	Severity string `json:"severity,omitempty"`

	// Confidence is the verifier's confidence in this finding (e.g. "high",
	// "medium", "low"). Free string.
	Confidence string `json:"confidence,omitempty"`

	// Evidence is the concrete observation backing this finding (e.g. a file
	// path, a log excerpt, a failing test name).
	Evidence string `json:"evidence,omitempty"`

	// SuggestedFix is the verifier's proposed remediation, consumed by a
	// repair attempt's evidence packet (Phase 51 TASK-02).
	SuggestedFix string `json:"suggestedFix,omitempty"`
}

// GateDecision is the wire-format verdict document a verifier writes to
// out.json (EVAL-03). It round-trips through the file-envelope seam between
// the K8s controller and an out-of-tree evaluator image — it is
// intentionally NOT a CRD type (D-01: putting this in api/v1alpha3 would drag
// CRD-machinery imports into the dispatch seam).
type GateDecision struct {
	// Verdict is the terminal classification. Always one of VerdictApproved,
	// VerdictRepairable, or VerdictBlocked — see [ClassifyVerdict] for the
	// fail-closed parsing discipline that guarantees this.
	Verdict Verdict `json:"verdict"`

	// Summary is a human-readable one-line rollup of the verdict.
	Summary string `json:"summary,omitempty"`

	// Findings is the list of deviations backing a REPAIRABLE or BLOCKED
	// verdict. Empty for a clean APPROVED verdict.
	Findings []Finding `json:"findings,omitempty"`
}

// ClassifyVerdict parses raw as a gate_decision JSON document and returns its
// terminal [Verdict], fail-closed by construction (D-04): empty input,
// malformed JSON, and a missing/unrecognized verdict field all return
// [VerdictBlocked] — never [VerdictApproved]. The bare Verdict return type
// (no accompanying error value) makes "unknown" inexpressible as anything but
// BLOCKED — a caller cannot forget to map an error to the safe terminal,
// because there is no error to forget (RESEARCH Pitfall 2).
func ClassifyVerdict(raw json.RawMessage) Verdict {
	if len(raw) == 0 {
		return VerdictBlocked // empty JSON
	}
	var parsed struct {
		Verdict string `json:"verdict"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return VerdictBlocked // malformed
	}
	switch Verdict(parsed.Verdict) {
	case VerdictApproved, VerdictRepairable, VerdictBlocked:
		return Verdict(parsed.Verdict)
	default:
		return VerdictBlocked // missing/unrecognized verdict field
	}
}
