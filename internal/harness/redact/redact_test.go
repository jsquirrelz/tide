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
	"bytes"
	"strings"
	"testing"
)

// assertRedaction is the shared assertion helper used by both the table-driven
// subtests and the top-level mirror functions below.
// For single-Write tests: all input is passed in one Write call followed by Close.
// For split-Write tests: callers invoke Write multiple times before calling Close.
func assertRedaction(t *testing.T, dst *bytes.Buffer, got, want string) {
	t.Helper()
	if got != want {
		t.Errorf("output mismatch\n  got:  %q\n  want: %q", got, want)
	}
}

// singleWriteRedact is a helper: writes input to a new RedactingWriter,
// calls Close, and returns the captured output.
func singleWriteRedact(t *testing.T, input string) string {
	t.Helper()
	var dst bytes.Buffer
	w := NewRedactingWriter(&dst)
	n, err := w.Write([]byte(input))
	if err != nil {
		t.Fatalf("Write error: %v", err)
	}
	if n != len(input) {
		t.Fatalf("Write returned %d, want %d", n, len(input))
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close error: %v", err)
	}
	return dst.String()
}

// TestRedactingWriter runs the full table of cases as subtests.
func TestRedactingWriter(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "PassthroughInnocuous",
			input: "the cat sat on the mat",
			want:  "the cat sat on the mat",
		},
		{
			name:  "RedactsAnthropicKey",
			input: "here is sk-ant-api03-aBcDeFgHiJkLmNoPqRsTuV the rest",
			want:  "here is [REDACTED] the rest",
		},
		{
			name:  "RedactsJWT",
			input: "token: eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0In0.SflKxwRJSMeKKF2QT4fwpMeJf36POk6yJV_adQssw5c end",
			want:  "token: [REDACTED] end",
		},
		{
			name:  "RedactsAWSKey",
			input: "key=AKIAIOSFODNN7EXAMPLE something",
			want:  "key=[REDACTED] something",
		},
		{
			name:  "RedactsGitHubPAT",
			input: "GITHUB_TOKEN=ghp_ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefgh12",
			want:  "GITHUB_TOKEN=[REDACTED]",
		},
		{
			name:  "RedactsSlackToken",
			input: "slack=xoxb-1234567890-abcdef done",
			want:  "slack=[REDACTED] done",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := singleWriteRedact(t, tc.input)
			assertRedaction(t, nil, got, tc.want)
		})
	}
}

// TestRedactingWriter_RedactsAnthropicKey is the top-level mirror for -run selection.
func TestRedactingWriter_RedactsAnthropicKey(t *testing.T) {
	// Key suffix must be >=20 chars to match sk-ant-api03-<20+> pattern.
	input := "here is sk-ant-api03-aBcDeFgHiJkLmNoPqRsTuV the rest"
	got := singleWriteRedact(t, input)
	assertRedaction(t, nil, got, "here is [REDACTED] the rest")
}

// TestRedactingWriter_RedactsJWT is the top-level mirror.
func TestRedactingWriter_RedactsJWT(t *testing.T) {
	input := "token: eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0In0.SflKxwRJSMeKKF2QT4fwpMeJf36POk6yJV_adQssw5c end"
	got := singleWriteRedact(t, input)
	assertRedaction(t, nil, got, "token: [REDACTED] end")
}

// TestRedactingWriter_RedactsAWSKey is the top-level mirror.
func TestRedactingWriter_RedactsAWSKey(t *testing.T) {
	input := "key=AKIAIOSFODNN7EXAMPLE something"
	got := singleWriteRedact(t, input)
	assertRedaction(t, nil, got, "key=[REDACTED] something")
}

// TestRedactingWriter_RedactsGitHubPAT is the top-level mirror.
func TestRedactingWriter_RedactsGitHubPAT(t *testing.T) {
	input := "GITHUB_TOKEN=ghp_ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefgh12"
	got := singleWriteRedact(t, input)
	assertRedaction(t, nil, got, "GITHUB_TOKEN=[REDACTED]")
}

// TestRedactingWriter_RedactsSlackToken is the top-level mirror.
func TestRedactingWriter_RedactsSlackToken(t *testing.T) {
	input := "slack=xoxb-1234567890-abcdef done"
	got := singleWriteRedact(t, input)
	assertRedaction(t, nil, got, "slack=[REDACTED] done")
}

// TestRedactingWriter_RedactsTokenSplitAcrossWrites is THE load-bearing test
// (Pitfall A defense). It splits an Anthropic API key across two Write calls —
// without the tail-keep buffer, the first half would be emitted unredacted.
func TestRedactingWriter_RedactsTokenSplitAcrossWrites(t *testing.T) {
	// Token = sk-ant-api03-aBcDeFgHiJkLmNoPqRsTuV (21 chars, matches {20,})
	// Split: "sk-ant-api03-aBcDeFgHi" | "JkLmNoPqRsTuV the rest"
	var dst bytes.Buffer
	w := NewRedactingWriter(&dst)

	first := "here is sk-ant-api03-aBcDeFgHi"
	second := "JkLmNoPqRsTuV the rest"

	n1, err := w.Write([]byte(first))
	if err != nil {
		t.Fatalf("Write#1 error: %v", err)
	}
	if n1 != len(first) {
		t.Fatalf("Write#1 returned %d, want %d", n1, len(first))
	}

	n2, err := w.Write([]byte(second))
	if err != nil {
		t.Fatalf("Write#2 error: %v", err)
	}
	if n2 != len(second) {
		t.Fatalf("Write#2 returned %d, want %d", n2, len(second))
	}

	if err := w.Close(); err != nil {
		t.Fatalf("Close error: %v", err)
	}

	got := dst.String()
	// The combined token "sk-ant-api03-aBcDeFgHiJkLmNoPqRs" must be redacted.
	if strings.Contains(got, "sk-ant-api03-") {
		t.Errorf("split-token NOT redacted: %q", got)
	}
	if !strings.Contains(got, "[REDACTED]") {
		t.Errorf("expected [REDACTED] in output, got: %q", got)
	}
	if !strings.Contains(got, "here is") || !strings.Contains(got, "the rest") {
		t.Errorf("surrounding text corrupted: %q", got)
	}
}

// TestRedactingWriter_DoesNotRedactInnocuousStrings is the top-level mirror.
func TestRedactingWriter_DoesNotRedactInnocuousStrings(t *testing.T) {
	input := "the cat sat on the mat"
	got := singleWriteRedact(t, input)
	assertRedaction(t, nil, got, "the cat sat on the mat")
}

// TestRedactingWriter_FlushesOnClose verifies that Close() drains the tail-keep
// buffer through a final redaction pass and writes the result to dst.
func TestRedactingWriter_FlushesOnClose(t *testing.T) {
	// Write a short string that stays entirely in the tail-keep buffer (len < maxPatternLen).
	// Without Close() the bytes would never be flushed.
	var dst bytes.Buffer
	w := NewRedactingWriter(&dst)
	input := "hello world"
	if _, err := w.Write([]byte(input)); err != nil {
		t.Fatalf("Write error: %v", err)
	}
	// Before Close, the tail-keep buffer may still hold some or all bytes.
	// After Close, all bytes must be in dst.
	if err := w.Close(); err != nil {
		t.Fatalf("Close error: %v", err)
	}
	if dst.String() != input {
		t.Errorf("after Close got %q, want %q", dst.String(), input)
	}
}
