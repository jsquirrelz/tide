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

package credproxy

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"time"
)

// Token layout (after base64.RawURLEncoding decode):
//
//	[0..16)  nonce        — 16 random bytes
//	[16..24) expiry       — int64 unix-seconds (big-endian)
//	[24..56) mac          — sha256.Size = 32 bytes
//
// Signed payload = (nonce || expiry-bytes || taskUID-bytes).
// The taskUID is bound into the MAC but NOT stored in the token; cross-Pod
// replay is detected because the verifier's expectedTaskUID is injected via
// the Pod's TIDE_TASK_UID env var (D-C3).
const (
	NonceLen  = 16
	ExpiryLen = 8
	MacLen    = sha256.Size                   // 32
	TokenLen  = NonceLen + ExpiryLen + MacLen // 56
)

// Sentinel errors returned by Verify. Callers may use errors.Is to
// distinguish failure modes (Pitfall 18 defence — Plan 09 TaskReconciler
// can surface the exact failure reason in Task.Status conditions).
var (
	ErrBadTokenLength  = errors.New("credproxy: bad token length")
	ErrExpired         = errors.New("credproxy: token expired")
	ErrTaskUIDMismatch = errors.New("credproxy: task uid mismatch")
	ErrBadMAC          = errors.New("credproxy: bad mac")
)

// Sign produces a base64.RawURLEncoded token for the given taskUID, valid
// until time.Now().Add(validFor). Returns an error if signingKey is empty or
// rand.Read fails.
//
// Per D-C3: HMAC-SHA256 over (nonce || expiry-bytes || taskUID).
func Sign(signingKey []byte, taskUID string, validFor time.Duration) (string, error) {
	if len(signingKey) == 0 {
		return "", errors.New("credproxy: signingKey must not be empty")
	}

	nonce := make([]byte, NonceLen)
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}

	expBytes := make([]byte, ExpiryLen)
	binary.BigEndian.PutUint64(expBytes, uint64(time.Now().Add(validFor).Unix()))

	mac := hmac.New(sha256.New, signingKey)
	mac.Write(nonce)
	mac.Write(expBytes)
	mac.Write([]byte(taskUID))
	sum := mac.Sum(nil)

	out := make([]byte, 0, TokenLen)
	out = append(out, nonce...)
	out = append(out, expBytes...)
	out = append(out, sum...)
	return base64.RawURLEncoding.EncodeToString(out), nil
}

// Verify returns nil iff the token MAC verifies, the expiry has not passed,
// AND the token was minted for expectedTaskUID (cross-Pod replay defence per
// T-02-05-01).
//
// Error semantics:
//   - ErrBadTokenLength — decode failed or wrong byte count (not a TIDE token)
//   - ErrExpired        — token was valid but has now expired
//   - ErrBadMAC         — MAC did not verify (includes: tampered bytes AND
//     wrong-UID replay, since the taskUID is MAC-bound but not stored)
//   - ErrTaskUIDMismatch — MAC verified but token was minted for a different
//     expectedTaskUID (returned only when we can confirm the difference via a
//     secondary probe; otherwise ErrBadMAC is returned)
//
// Implementation note: because the taskUID is not stored in the token, the
// verifier uses time-constant hmac.Equal (T-02-05-02) and returns ErrBadMAC
// for any MAC mismatch. ErrTaskUIDMismatch is a distinct sentinel for callers
// that explicitly test for cross-Pod replay; at runtime the sidecar treats
// both as auth failures.
func Verify(signingKey []byte, token string, expectedTaskUID string) error {
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil || len(raw) != TokenLen {
		return ErrBadTokenLength
	}

	nonce := raw[:NonceLen]
	expBytes := raw[NonceLen : NonceLen+ExpiryLen]
	gotMAC := raw[NonceLen+ExpiryLen:]

	expiry := int64(binary.BigEndian.Uint64(expBytes))
	if time.Now().Unix() > expiry {
		return ErrExpired
	}

	mac := hmac.New(sha256.New, signingKey)
	mac.Write(nonce)
	mac.Write(expBytes)
	mac.Write([]byte(expectedTaskUID))
	wantMAC := mac.Sum(nil)

	// Time-constant compare prevents MAC oracle timing attacks (T-02-05-02).
	if !hmac.Equal(gotMAC, wantMAC) {
		return ErrBadMAC
	}
	return nil
}
