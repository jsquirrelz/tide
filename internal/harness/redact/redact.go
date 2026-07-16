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

package redact

import (
	"io"
	"regexp"
)

// RedactingWriter wraps an io.Writer; bytes are redacted before forwarding.
// Maintains a tail-keep buffer to handle pattern matches across Write calls
// (Pitfall A defense — RESEARCH.md §"Pitfall A: Regex match straddling chunk
// boundary in streaming redactor").
//
// Write semantics: per the io.Writer contract, Write returns (len(p), nil) on
// success regardless of how many bytes are flushed to the underlying writer in
// this call — some bytes are held in the tail-keep buffer and are not flushed
// until the next Write or until Close is called.
//
// Close must be called once writing is complete to drain the remaining
// tail-keep buffer through a final redaction pass. If the underlying writer
// also implements io.Closer, its Close is propagated.
type RedactingWriter struct {
	dst      io.Writer
	tail     []byte // last maxPatternLen bytes from previous Write, unredacted
	patterns []*regexp.Regexp
}

// NewRedactingWriter returns a RedactingWriter that applies [SecretPatterns]
// before forwarding bytes to dst.
func NewRedactingWriter(dst io.Writer) *RedactingWriter {
	return &RedactingWriter{dst: dst, patterns: SecretPatterns}
}

// Write redacts p and forwards as many safe bytes as possible to the
// underlying writer. The last [maxPatternLen] bytes are held in a tail-keep
// buffer so that a secret pattern split across two Write calls is still
// detected on the second call.
//
// Returns (len(p), nil) on success; io.Writer contract is honoured even when
// fewer bytes are flushed downstream in this call.
func (w *RedactingWriter) Write(p []byte) (int, error) {
	// Concatenate tail + p into a working buffer.
	buf := make([]byte, 0, len(w.tail)+len(p))
	buf = append(buf, w.tail...)
	buf = append(buf, p...)

	// Apply all secret patterns in sequence (in-place replacement).
	for _, re := range w.patterns {
		buf = re.ReplaceAll(buf, []byte("[REDACTED]"))
	}

	// Hold back the last maxPatternLen bytes as the next tail-keep so any
	// pattern that straddles the boundary into the next Write is still caught.
	if len(buf) > maxPatternLen {
		flushUpTo := len(buf) - maxPatternLen
		if _, err := w.dst.Write(buf[:flushUpTo]); err != nil {
			return 0, err
		}
		w.tail = append(w.tail[:0], buf[flushUpTo:]...)
	} else {
		// Buffer is still within maxPatternLen — hold everything in tail.
		w.tail = append(w.tail[:0], buf...)
	}

	// Always return the number of bytes from the caller's input (io.Writer contract).
	return len(p), nil
}

// Close flushes the remaining tail-keep buffer through one final redaction
// pass and writes the result to the underlying writer. If the underlying
// writer also implements io.Closer, its Close is propagated.
func (w *RedactingWriter) Close() error {
	if len(w.tail) > 0 {
		buf := w.tail
		for _, re := range w.patterns {
			buf = re.ReplaceAll(buf, []byte("[REDACTED]"))
		}
		if _, err := w.dst.Write(buf); err != nil {
			return err
		}
		w.tail = w.tail[:0]
	}
	if c, ok := w.dst.(io.Closer); ok {
		return c.Close()
	}
	return nil
}

// String applies [SecretPatterns] to a single in-memory string and returns
// the redacted result. Unlike RedactingWriter (stream-oriented, tail-keep
// buffered across Write calls to defend against a pattern straddling a chunk
// boundary), String operates on a fully-materialized string in one pass —
// the correct shape for per-message redaction (MSG-02), where the entire
// message content is already available before it is ever emitted onto a
// span.
//
// D-09 (locked, non-negotiable): callers MUST call String BEFORE any
// truncation step. Truncating first can split a secret across the cut so
// the pattern no longer matches, leaving a partial credential visible in the
// emitted output — the same class of bug RedactingWriter's tail-keep buffer
// defends against for streaming chunk boundaries. See CONTEXT.md D-09 and
// RESEARCH.md Pitfall 4 ("Truncating before redacting").
func String(s string) string {
	b := []byte(s)
	for _, re := range SecretPatterns {
		b = re.ReplaceAll(b, []byte("[REDACTED]"))
	}
	return string(b)
}
