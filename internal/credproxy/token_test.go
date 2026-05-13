package credproxy

import (
	"encoding/base64"
	"errors"
	"testing"
	"time"
)

// TestSign_RoundTrip verifies Sign produces a valid base64 token that decodes
// to the expected 56-byte layout.
func TestSign_RoundTrip(t *testing.T) {
	key := []byte("12345678901234567890123456789012") // 32 bytes
	token, err := Sign(key, "task-uid-abc", 10*time.Minute)
	if err != nil {
		t.Fatalf("Sign: unexpected error: %v", err)
	}
	if token == "" {
		t.Fatal("Sign: returned empty token")
	}
	// base64.RawURLEncoding: ceil(56 * 4 / 3) = 75 characters (no padding)
	if len(token) != 75 {
		t.Fatalf("Sign: expected 75-char token, got %d chars: %q", len(token), token)
	}
}

// TestSign_ErrorOnEmptyKey ensures Sign returns an error when signingKey is empty.
func TestSign_ErrorOnEmptyKey(t *testing.T) {
	_, err := Sign(nil, "task-uid-abc", 10*time.Minute)
	if err == nil {
		t.Fatal("Sign: expected error for empty key, got nil")
	}
}

// verifyTests is the table of Verify test cases.
// Note on ErrTaskUIDMismatch: since the taskUID is MAC-bound but not stored in
// the token, a wrong-UID verify produces the same MAC failure as a tampered
// token — both return ErrBadMAC. ErrTaskUIDMismatch is kept as a sentinel for
// future token formats that embed the UID (or for callers that construct
// test-only scenarios via wrapping). In this plan the wrong-UID path also
// returns ErrBadMAC (see Verify docstring).
var verifyTests = []struct {
	name       string
	setup      func(key []byte) string // returns token
	uid        string
	wantErr    error
	wantNilErr bool
}{
	{
		name: "ValidToken",
		setup: func(key []byte) string {
			tok, err := Sign(key, "task-uid-abc", 10*time.Minute)
			if err != nil {
				panic(err)
			}
			return tok
		},
		uid:        "task-uid-abc",
		wantNilErr: true,
	},
	{
		name: "TaskUIDMismatch",
		setup: func(key []byte) string {
			tok, err := Sign(key, "task-uid-abc", 10*time.Minute)
			if err != nil {
				panic(err)
			}
			return tok
		},
		// Wrong UID → MAC mismatch → ErrBadMAC (taskUID is MAC-bound, not stored)
		uid:     "task-uid-xyz",
		wantErr: ErrBadMAC,
	},
	{
		name: "TamperedMAC",
		setup: func(key []byte) string {
			tok, err := Sign(key, "task-uid-abc", 10*time.Minute)
			if err != nil {
				panic(err)
			}
			// Decode, tamper the last byte (MAC region), re-encode.
			// Flipping raw bytes ensures the base64 remains valid.
			raw, _ := base64.RawURLEncoding.DecodeString(tok)
			raw[len(raw)-1] ^= 0xFF
			return base64.RawURLEncoding.EncodeToString(raw)
		},
		uid:     "task-uid-abc",
		wantErr: ErrBadMAC,
	},
	{
		name: "Expired",
		setup: func(key []byte) string {
			// Sign with negative validFor so the token is already expired
			tok, err := Sign(key, "task-uid-abc", -1*time.Hour)
			if err != nil {
				panic(err)
			}
			return tok
		},
		uid:     "task-uid-abc",
		wantErr: ErrExpired,
	},
	{
		name: "BadTokenLength",
		setup: func(key []byte) string {
			return "not-base64-not-a-token"
		},
		uid:     "task-uid-abc",
		wantErr: ErrBadTokenLength,
	},
}

func TestVerify(t *testing.T) {
	key := []byte("12345678901234567890123456789012") // 32 bytes
	for _, tc := range verifyTests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			tok := tc.setup(key)
			err := Verify(key, tok, tc.uid)
			if tc.wantNilErr {
				if err != nil {
					t.Fatalf("Verify: expected nil, got %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("Verify: expected error %v, got nil", tc.wantErr)
			}
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("Verify: expected errors.Is(%v), got %v", tc.wantErr, err)
			}
		})
	}
}

// Mirror functions so -run TestVerify_ValidToken selects directly.

func TestVerify_ValidToken(t *testing.T) {
	key := []byte("12345678901234567890123456789012")
	tok, _ := Sign(key, "task-uid-abc", 10*time.Minute)
	if err := Verify(key, tok, "task-uid-abc"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestVerify_TaskUIDMismatch confirms that a token signed for task-uid-abc
// fails verification when presented with task-uid-xyz. Since the taskUID is
// MAC-bound but not stored in the 56-byte token, the verifier cannot
// distinguish UID mismatch from a tampered token — both return ErrBadMAC.
// ErrTaskUIDMismatch is reserved as a sentinel for future use.
func TestVerify_TaskUIDMismatch(t *testing.T) {
	key := []byte("12345678901234567890123456789012")
	tok, _ := Sign(key, "task-uid-abc", 10*time.Minute)
	err := Verify(key, tok, "task-uid-xyz")
	if !errors.Is(err, ErrBadMAC) {
		t.Fatalf("expected ErrBadMAC (wrong UID → MAC mismatch), got %v", err)
	}
}

func TestVerify_TamperedMAC(t *testing.T) {
	key := []byte("12345678901234567890123456789012")
	tok, _ := Sign(key, "task-uid-abc", 10*time.Minute)
	// Decode, tamper the last MAC byte, re-encode so the base64 remains valid.
	raw, _ := base64.RawURLEncoding.DecodeString(tok)
	raw[len(raw)-1] ^= 0xFF
	tampered := base64.RawURLEncoding.EncodeToString(raw)
	err := Verify(key, tampered, "task-uid-abc")
	if !errors.Is(err, ErrBadMAC) {
		t.Fatalf("expected ErrBadMAC, got %v", err)
	}
}

func TestVerify_Expired(t *testing.T) {
	key := []byte("12345678901234567890123456789012")
	tok, _ := Sign(key, "task-uid-abc", -1*time.Hour)
	err := Verify(key, tok, "task-uid-abc")
	if !errors.Is(err, ErrExpired) {
		t.Fatalf("expected ErrExpired, got %v", err)
	}
}

func TestVerify_BadTokenLength(t *testing.T) {
	key := []byte("12345678901234567890123456789012")
	err := Verify(key, "not-base64-not-a-token", "task-uid-abc")
	if !errors.Is(err, ErrBadTokenLength) {
		t.Fatalf("expected ErrBadTokenLength, got %v", err)
	}
}
