package harness

import (
	"errors"
	"testing"

	pkgdispatch "github.com/jsquirrelz/tide/pkg/dispatch"
)

// TestCheckCaps runs the full table of cap-enforcement cases as subtests.
func TestCheckCaps(t *testing.T) {
	tests := []struct {
		name        string
		caps        pkgdispatch.Caps
		usage       pkgdispatch.Usage
		wantNil     bool
		wantReason  string
	}{
		{
			name: "AllUnder",
			caps: pkgdispatch.Caps{
				WallClockSeconds: 30,
				Iterations:       10,
				InputTokens:      1000,
				OutputTokens:     1000,
			},
			usage: pkgdispatch.Usage{
				InputTokens:  500,
				OutputTokens: 500,
				Iterations:   5,
			},
			wantNil: true,
		},
		{
			name:       "IterationsExceeded",
			caps:       pkgdispatch.Caps{Iterations: 10},
			usage:      pkgdispatch.Usage{Iterations: 11},
			wantNil:    false,
			wantReason: "iterations",
		},
		{
			name:       "InputTokensExceeded",
			caps:       pkgdispatch.Caps{InputTokens: 1000},
			usage:      pkgdispatch.Usage{InputTokens: 1001},
			wantNil:    false,
			wantReason: "input-tokens",
		},
		{
			name:       "OutputTokensExceeded",
			caps:       pkgdispatch.Caps{OutputTokens: 1000},
			usage:      pkgdispatch.Usage{OutputTokens: 1001},
			wantNil:    false,
			wantReason: "output-tokens",
		},
		{
			name:    "ZeroCapMeansUnconstrained",
			caps:    pkgdispatch.Caps{},
			usage:   pkgdispatch.Usage{Iterations: 999, InputTokens: 999999, OutputTokens: 999999},
			wantNil: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := CheckCaps(tc.caps, tc.usage)
			if tc.wantNil {
				if err != nil {
					t.Errorf("expected nil error, got: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("expected error with reason %q, got nil", tc.wantReason)
			}
			var chErr *CapHitError
			if !errors.As(err, &chErr) {
				t.Fatalf("expected *CapHitError, got %T: %v", err, err)
			}
			if chErr.Reason != tc.wantReason {
				t.Errorf("reason mismatch: got %q, want %q", chErr.Reason, tc.wantReason)
			}
		})
	}
}

// Top-level mirror functions for individual -run selection.

func TestCheckCaps_AllUnder_ReturnsNil(t *testing.T) {
	caps := pkgdispatch.Caps{WallClockSeconds: 30, Iterations: 10, InputTokens: 1000, OutputTokens: 1000}
	usage := pkgdispatch.Usage{InputTokens: 500, OutputTokens: 500, Iterations: 5}
	if err := CheckCaps(caps, usage); err != nil {
		t.Errorf("expected nil, got: %v", err)
	}
}

func TestCheckCaps_IterationsExceeded(t *testing.T) {
	caps := pkgdispatch.Caps{Iterations: 10}
	usage := pkgdispatch.Usage{Iterations: 11}
	err := CheckCaps(caps, usage)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	var chErr *CapHitError
	if !errors.As(err, &chErr) {
		t.Fatalf("expected *CapHitError, got %T", err)
	}
	if chErr.Reason != "iterations" {
		t.Errorf("reason mismatch: got %q, want %q", chErr.Reason, "iterations")
	}
}

func TestCheckCaps_InputTokensExceeded(t *testing.T) {
	caps := pkgdispatch.Caps{InputTokens: 1000}
	usage := pkgdispatch.Usage{InputTokens: 1001}
	err := CheckCaps(caps, usage)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	var chErr *CapHitError
	if !errors.As(err, &chErr) {
		t.Fatalf("expected *CapHitError, got %T", err)
	}
	if chErr.Reason != "input-tokens" {
		t.Errorf("reason mismatch: got %q, want %q", chErr.Reason, "input-tokens")
	}
}

func TestCheckCaps_OutputTokensExceeded(t *testing.T) {
	caps := pkgdispatch.Caps{OutputTokens: 1000}
	usage := pkgdispatch.Usage{OutputTokens: 1001}
	err := CheckCaps(caps, usage)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	var chErr *CapHitError
	if !errors.As(err, &chErr) {
		t.Fatalf("expected *CapHitError, got %T", err)
	}
	if chErr.Reason != "output-tokens" {
		t.Errorf("reason mismatch: got %q, want %q", chErr.Reason, "output-tokens")
	}
}

func TestCheckCaps_ZeroCapMeansUnconstrained(t *testing.T) {
	caps := pkgdispatch.Caps{}
	usage := pkgdispatch.Usage{Iterations: 999, InputTokens: 999999, OutputTokens: 999999}
	if err := CheckCaps(caps, usage); err != nil {
		t.Errorf("expected nil for zero caps, got: %v", err)
	}
}
