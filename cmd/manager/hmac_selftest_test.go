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

package main

import (
	"strings"
	"testing"
)

// TestHmacSelfTest_RoundTripsValidKey verifies the round-trip succeeds for
// a 32-byte key — the minimum-length contract enforced by
// decodeSigningKeyFromEnv. Plain-bytes input (no double-decoding) is the
// state the manager binary sees at runtime per the env-decode comment.
func TestHmacSelfTest_RoundTripsValidKey(t *testing.T) {
	key := []byte("0123456789abcdef0123456789abcdef") // 32 bytes
	if err := hmacSelfTest(key); err != nil {
		t.Fatalf("hmacSelfTest: expected nil for valid 32-byte key, got %v", err)
	}
}

// TestHmacSelfTest_RoundTripsLongKey verifies the typical Helm-rendered
// 64-byte key — the production shape — also round-trips.
func TestHmacSelfTest_RoundTripsLongKey(t *testing.T) {
	key := []byte(strings.Repeat("ab", 32)) // 64 bytes
	if err := hmacSelfTest(key); err != nil {
		t.Fatalf("hmacSelfTest: expected nil for 64-byte key, got %v", err)
	}
}

// TestHmacSelfTest_RejectsEmptyKey verifies an empty key fails the Sign
// step (credproxy.Sign returns an explicit empty-key error). This is the
// regression-guard for the env-decode bug: if env decoding silently
// produced a zero-length byte slice, the self-test would catch it here
// before the manager entered Start.
func TestHmacSelfTest_RejectsEmptyKey(t *testing.T) {
	err := hmacSelfTest(nil)
	if err == nil {
		t.Fatal("hmacSelfTest: expected error for nil key, got nil")
	}
	if !strings.Contains(err.Error(), "Sign failed") {
		t.Errorf("expected Sign-failed error wrapping; got: %v", err)
	}
}
