// Package common houses provider-agnostic primitives shared across every
// subagent backend (Anthropic Claude Code today; OpenAI/Google/xAI/opencode
// tomorrow). Per D-C1 the contract is: anthropic-specific code lives in
// internal/subagent/anthropic/; provider-neutral plumbing (JSONL line
// reading, compiled-in prompt templates) lives here. The package must NOT
// import the CRD types package (CRD-agnostic by design) and must NOT mention
// any vendor by name — that asymmetry is what lets a future
// internal/subagent/openai/ reuse this code unchanged.
package common

import (
	"bufio"
	"io"
)

// streamReaderMaxLineBytes is the per-line ceiling enforced by bufio.Scanner
// in [ReadLines]. Claude Code's piped stdin cap is 10MB (RESEARCH Pattern 5,
// lines 545-569); 16MB gives headroom for verbose stream-json events that
// inline large diffs or assistant text. The initial buffer (1MB) is the
// starting allocation — bufio.Scanner grows it geometrically up to this max.
const streamReaderMaxLineBytes = 16 * 1024 * 1024

// streamReaderInitialBufBytes is the initial buffer scanner.Buffer starts with.
// Picking 1MB avoids many small reallocations on typical stream-json line
// sizes (usually a few KB) while keeping the cold-start RSS small.
const streamReaderInitialBufBytes = 1024 * 1024

// ReadLines reads newline-delimited byte chunks from r and invokes handler
// once per line. Each handler call receives the line as raw bytes WITHOUT the
// trailing newline. Parsing (JSON or otherwise) is the handler's concern —
// ReadLines is content-agnostic by design so anthropic, openai, google, xai,
// and opencode subagent implementations can share the same line plumbing.
//
// If handler returns a non-nil error, ReadLines stops reading and returns
// that error wrapped (via direct return, not %w — caller controls wrapping).
// If the underlying scanner returns an error (oversize line, I/O failure),
// ReadLines returns it. A nil error means EOF was reached cleanly.
//
// Per-line size budget: [streamReaderMaxLineBytes] (16MB). Lines longer than
// the budget return bufio.ErrTooLong via scanner.Err().
func ReadLines(r io.Reader, handler func(line []byte) error) error {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, streamReaderInitialBufBytes), streamReaderMaxLineBytes)
	for scanner.Scan() {
		if err := handler(scanner.Bytes()); err != nil {
			return err
		}
	}
	return scanner.Err()
}
