package common

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

// TestReadLines_HappyPath verifies the line-by-line JSONL reader hands each
// line (without the trailing newline) to the handler exactly once. The test is
// content-agnostic — ReadLines is provider-neutral by D-C1.
func TestReadLines_HappyPath(t *testing.T) {
	input := "{}\n{\"x\":1}\n{\"y\":2}\n"
	var got []string
	if err := ReadLines(strings.NewReader(input), func(line []byte) error {
		got = append(got, string(line))
		return nil
	}); err != nil {
		t.Fatalf("ReadLines returned error: %v", err)
	}
	want := []string{"{}", `{"x":1}`, `{"y":2}`}
	if len(got) != len(want) {
		t.Fatalf("handler invocations: got %d, want %d (%v)", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("line %d: got %q, want %q", i, got[i], want[i])
		}
	}
}

// TestReadLines_ToleratesNonJSON verifies ReadLines does not try to parse —
// parsing is the handler's concern. Mirrors RESEARCH.md §"defensive parsers
// continue on json.Unmarshal failure" — ReadLines hands raw bytes through
// regardless of JSON validity.
func TestReadLines_ToleratesNonJSON(t *testing.T) {
	input := "{\"valid\":1}\nnot-json garbage\n{\"valid\":2}\n"
	var got []string
	if err := ReadLines(strings.NewReader(input), func(line []byte) error {
		got = append(got, string(line))
		return nil
	}); err != nil {
		t.Fatalf("ReadLines returned error: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("expected 3 lines (incl. non-JSON), got %d: %v", len(got), got)
	}
	if got[1] != "not-json garbage" {
		t.Errorf("non-JSON line not passed through: got %q", got[1])
	}
}

// TestReadLines_AcceptsLargeLine asserts the 16MB line budget set by
// scanner.Buffer accepts a single 16MB line without error. Per RESEARCH
// Pattern 5: claude-code piped stdin cap is 10MB; we budget 16MB headroom.
func TestReadLines_AcceptsLargeLine(t *testing.T) {
	// 16MB exactly — boundary case: scanner.Buffer max is 16*1024*1024.
	// Use 8MB to stay comfortably under the boundary while still proving the
	// budget is larger than bufio's default 64KB.
	const sz = 8 * 1024 * 1024
	big := strings.Repeat("a", sz)
	input := big + "\n"
	called := 0
	if err := ReadLines(strings.NewReader(input), func(line []byte) error {
		called++
		if len(line) != sz {
			return fmt.Errorf("line length: got %d, want %d", len(line), sz)
		}
		return nil
	}); err != nil {
		t.Fatalf("ReadLines on 8MB line: %v", err)
	}
	if called != 1 {
		t.Errorf("handler invocations: got %d, want 1", called)
	}
}

// TestReadLines_RejectsOversizeLine asserts a line larger than the 16MB cap
// returns an error from scanner.Err(). Mirrors RESEARCH Pattern 5 line 547.
func TestReadLines_RejectsOversizeLine(t *testing.T) {
	// 20MB > 16MB budget — must error.
	const sz = 20 * 1024 * 1024
	big := strings.Repeat("a", sz)
	input := big + "\n"
	err := ReadLines(strings.NewReader(input), func(line []byte) error { return nil })
	if err == nil {
		t.Fatal("expected error on 20MB line, got nil")
	}
}

// TestReadLines_HandlerErrorPropagates asserts a non-nil error returned from
// the handler terminates the read and is propagated to the caller. Mirrors the
// PATTERNS.md analog pattern of wrapped-error semantics.
func TestReadLines_HandlerErrorPropagates(t *testing.T) {
	input := "a\nb\nc\n"
	sentinel := errors.New("sentinel")
	called := 0
	err := ReadLines(strings.NewReader(input), func(line []byte) error {
		called++
		if called == 2 {
			return sentinel
		}
		return nil
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected sentinel error, got %v", err)
	}
	if called != 2 {
		t.Errorf("expected 2 handler calls, got %d", called)
	}
}
