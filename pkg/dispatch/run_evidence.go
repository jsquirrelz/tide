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

// ChangedFile is one entry in [RunEvidence]'s bounded changed-file manifest —
// a git `--name-status` pair (path + status letter), never a diff.
type ChangedFile struct {
	// Path is the repo-relative path of the changed file.
	Path string `json:"path"`

	// Status is the git name-status letter: A (added), M (modified),
	// D (deleted), R (renamed), etc.
	Status string `json:"status"`
}

// RunEvidence is the Phase-50 run-evidence contract (EXEC-03) — a single
// structured block mapping the canonical evals/README.md "Run evidence
// contract" list 1:1.
//
// RunEvidence is REFERENCED, NOT RE-DERIVED: it holds ONLY the net-new
// evidence fields. The rest of the canonical contract list is already
// carried on the enclosing [EnvelopeOut] and MUST be read from there, never
// duplicated here:
//
//   - Task ID                    -> EnvelopeOut.TaskUID
//   - cost / duration / tokens   -> EnvelopeOut.Usage + EnvelopeOut.CompletedAt
//   - iteration count            -> EnvelopeOut.Usage.Iterations
//   - head commit                -> EnvelopeOut.Git.HeadSHA
//   - bounded feedback           -> EnvelopeOut.Result
//   - terminal reason            -> EnvelopeOut.TerminalReason
//
// Every field here is bounded by construction; writers MUST call [RunEvidence.Bounded]
// before marshalling a RunEvidence built from untrusted or pathological input
// (T-50-01 — DoS via an unbounded manifest/command list blowing the 4KB
// TerminationStub budget or the reporter's OTLP export batch).
type RunEvidence struct {
	// SpecID is the Task-contract spec ID this attempt targets. Defined now;
	// populated by Phase 51's locked Task contract.
	SpecID string `json:"specID,omitempty"`

	// LockingCommit is the Task-contract-locking commit SHA. Defined now;
	// populated by Phase 51.
	LockingCommit string `json:"lockingCommit,omitempty"`

	// Commands is the list of command lines executed during this attempt,
	// bounded by [MaxRunEvidenceCommands] / [MaxRunEvidenceCommandBytes].
	Commands []string `json:"commands,omitempty"`

	// EvaluatorVersions is the list of evaluator version strings that
	// assessed this attempt. Defined now; populated by Phase 51.
	EvaluatorVersions []string `json:"evaluatorVersions,omitempty"`

	// ChangedFiles is a bounded git `--name-status` manifest — path + status
	// only, never a diff. Bounded by [MaxRunEvidenceChangedFiles] /
	// [MaxRunEvidencePathBytes].
	ChangedFiles []ChangedFile `json:"changedFiles,omitempty"`

	// ChangedFileTotal is the pre-truncation count of changed files, even
	// when ChangedFiles itself was truncated by [RunEvidence.Bounded].
	ChangedFileTotal int `json:"changedFileTotal,omitempty"`

	// Model is the model identifier this attempt ran under, echoed from
	// EnvelopeIn.Provider.Model — never re-derived.
	Model string `json:"model,omitempty"`

	// PromptVersion is the compiled-in prompt-template version this attempt
	// rendered.
	PromptVersion string `json:"promptVersion,omitempty"`

	// RuntimeVersion is the executor runtime (CLI) version this attempt ran
	// under.
	RuntimeVersion string `json:"runtimeVersion,omitempty"`
}

// Bounds for RunEvidence fields (T-50-01 mitigation). Writers MUST call
// [RunEvidence.Bounded] before marshalling RunEvidence data whose size is not
// already known to be small (e.g. a git-produced changed-file manifest or
// command list from a pathological worktree).
const (
	// MaxRunEvidenceChangedFiles is the maximum number of ChangedFiles
	// entries retained by Bounded().
	MaxRunEvidenceChangedFiles = 100

	// MaxRunEvidencePathBytes is the maximum byte length of a single
	// ChangedFile.Path retained by Bounded().
	MaxRunEvidencePathBytes = 256

	// MaxRunEvidenceCommands is the maximum number of Commands entries
	// retained by Bounded().
	MaxRunEvidenceCommands = 10

	// MaxRunEvidenceCommandBytes is the maximum byte length of a single
	// Commands entry retained by Bounded().
	MaxRunEvidenceCommandBytes = 256

	// MaxRunEvidenceVersionBytes is the maximum byte length of Model,
	// PromptVersion, RuntimeVersion, SpecID, and LockingCommit retained by
	// Bounded().
	MaxRunEvidenceVersionBytes = 64
)

// Bounded returns a copy of e with every field truncated to its bounds
// const:
//   - ChangedFiles is truncated to at most [MaxRunEvidenceChangedFiles]
//     entries, and each entry's Path is truncated to at most
//     [MaxRunEvidencePathBytes].
//   - ChangedFileTotal is set to the pre-truncation len(e.ChangedFiles) when
//     it was zero or less than that length — never re-derived from the
//     (possibly truncated) output.
//   - Commands is truncated to at most [MaxRunEvidenceCommands] entries,
//     each truncated to at most [MaxRunEvidenceCommandBytes].
//   - Model, PromptVersion, RuntimeVersion, SpecID, and LockingCommit are
//     each truncated to at most [MaxRunEvidenceVersionBytes].
//
// This is the sole security control against an unbounded manifest or command
// list blowing the envelope or its bounded <4KB termination-message summary
// (T-50-01). Writers call this before marshalling any RunEvidence built from
// data whose size is not already known to be small.
func (e RunEvidence) Bounded() RunEvidence {
	out := e

	originalChangedFiles := len(e.ChangedFiles)
	if out.ChangedFileTotal == 0 || out.ChangedFileTotal < originalChangedFiles {
		out.ChangedFileTotal = originalChangedFiles
	}

	changedFiles := e.ChangedFiles
	if len(changedFiles) > MaxRunEvidenceChangedFiles {
		changedFiles = changedFiles[:MaxRunEvidenceChangedFiles]
	}
	boundedFiles := make([]ChangedFile, len(changedFiles))
	for i, f := range changedFiles {
		f.Path = truncateRunEvidenceString(f.Path, MaxRunEvidencePathBytes)
		boundedFiles[i] = f
	}
	out.ChangedFiles = boundedFiles

	commands := e.Commands
	if len(commands) > MaxRunEvidenceCommands {
		commands = commands[:MaxRunEvidenceCommands]
	}
	boundedCommands := make([]string, len(commands))
	for i, c := range commands {
		boundedCommands[i] = truncateRunEvidenceString(c, MaxRunEvidenceCommandBytes)
	}
	out.Commands = boundedCommands

	out.Model = truncateRunEvidenceString(out.Model, MaxRunEvidenceVersionBytes)
	out.PromptVersion = truncateRunEvidenceString(out.PromptVersion, MaxRunEvidenceVersionBytes)
	out.RuntimeVersion = truncateRunEvidenceString(out.RuntimeVersion, MaxRunEvidenceVersionBytes)
	out.SpecID = truncateRunEvidenceString(out.SpecID, MaxRunEvidenceVersionBytes)
	out.LockingCommit = truncateRunEvidenceString(out.LockingCommit, MaxRunEvidenceVersionBytes)

	return out
}

// truncateRunEvidenceString truncates s to at most n bytes. RunEvidence
// string fields are diagnostic-only and never re-parsed, so a hard byte cut
// (not UTF-8-boundary-aware) is sufficient and keeps the bound exact.
func truncateRunEvidenceString(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
