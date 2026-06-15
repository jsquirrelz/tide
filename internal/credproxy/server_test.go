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
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// buildTestProxy constructs a Proxy wired to a fake upstream that captures
// the last request it received. The fake upstream always returns 200 OK.
func buildTestProxy(t *testing.T, signingKey []byte, taskUID string) (*Proxy, *http.Request) {
	t.Helper()
	var captured *http.Request
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Deep-copy the incoming request headers for assertion.
		clone := r.Clone(r.Context())
		captured = clone
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(upstream.Close)

	p := &Proxy{
		SigningKey:      signingKey,
		ExpectedTaskUID: taskUID,
		UpstreamBaseURL: upstream.URL,
		RealAPIKey:      "sk-real-key",
		ListenAddr:      "127.0.0.1:0",
	}
	_ = captured // captured is nil until first request
	return p, captured
}

// TestProxyHandler_RejectsBadBearerWith401 asserts that a request with a
// bogus Authorization header receives HTTP 401.
func TestProxyHandler_RejectsBadBearerWith401(t *testing.T) {
	key := []byte("12345678901234567890123456789012")
	p, _ := buildTestProxy(t, key, "task-uid-abc")

	h, err := p.Handler()
	if err != nil {
		t.Fatalf("Handler: %v", err)
	}
	srv := httptest.NewServer(h)
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/v1/messages", nil)
	req.Header.Set("Authorization", "Bearer bogus-not-a-real-token")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	bodyStr := string(body)
	if len(bodyStr) == 0 {
		t.Error("expected non-empty response body on 401")
	}
}

// TestProxyHandler_AcceptsValidBearerAndRewritesHeaders asserts that a request
// with a valid signed bearer token:
//   - Gets forwarded to the upstream (200 OK response)
//   - Has its Authorization header rewritten to Bearer <RealAPIKey>
//   - Has its x-api-key header rewritten to <RealAPIKey>
func TestProxyHandler_AcceptsValidBearerAndRewritesHeaders(t *testing.T) {
	key := []byte("12345678901234567890123456789012")
	const taskUID = "task-uid-abc"

	// Capture upstream headers.
	var capturedAuth, capturedXAPIKey string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedAuth = r.Header.Get("Authorization")
		capturedXAPIKey = r.Header.Get("X-Api-Key")
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	p := &Proxy{
		SigningKey:      key,
		ExpectedTaskUID: taskUID,
		UpstreamBaseURL: upstream.URL,
		RealAPIKey:      "sk-real-key",
		ListenAddr:      "127.0.0.1:0",
	}
	h, hErr := p.Handler()
	if hErr != nil {
		t.Fatalf("Handler: %v", hErr)
	}
	srv := httptest.NewServer(h)
	defer srv.Close()

	// Sign a valid token.
	token, err := Sign(key, taskUID, 10*time.Minute)
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}

	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/v1/messages", nil)
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if capturedAuth != "Bearer sk-real-key" {
		t.Errorf("upstream Authorization = %q, want %q", capturedAuth, "Bearer sk-real-key")
	}
	if capturedXAPIKey != "sk-real-key" {
		t.Errorf("upstream x-api-key = %q, want %q", capturedXAPIKey, "sk-real-key")
	}
}

// TestProxyHandler_FallsBackToXAPIKeyHeader asserts that a request omitting
// Authorization but sending x-api-key with a valid signed token is accepted.
func TestProxyHandler_FallsBackToXAPIKeyHeader(t *testing.T) {
	key := []byte("12345678901234567890123456789012")
	const taskUID = "task-uid-abc"

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	p := &Proxy{
		SigningKey:      key,
		ExpectedTaskUID: taskUID,
		UpstreamBaseURL: upstream.URL,
		RealAPIKey:      "sk-real-key",
		ListenAddr:      "127.0.0.1:0",
	}
	h, hErr := p.Handler()
	if hErr != nil {
		t.Fatalf("Handler: %v", hErr)
	}
	srv := httptest.NewServer(h)
	defer srv.Close()

	token, err := Sign(key, taskUID, 10*time.Minute)
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}

	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/v1/messages", nil)
	// No Authorization header — use x-api-key fallback.
	req.Header.Set("x-api-key", token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 (x-api-key fallback), got %d", resp.StatusCode)
	}
}

// TestProxyHandler_RejectsUnlistedRouteWith403 asserts that a request with a
// valid signed token but a route NOT in the allowlist (e.g. /v1/files) is
// rejected with 403 — defense-in-depth against a compromised subagent
// reaching arbitrary Anthropic API endpoints (CR-04).
func TestProxyHandler_RejectsUnlistedRouteWith403(t *testing.T) {
	key := []byte("12345678901234567890123456789012")
	const taskUID = "task-uid-abc"

	// Upstream should NOT be hit — set a flag to detect leaks.
	upstreamHit := false
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		upstreamHit = true
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	p := &Proxy{
		SigningKey:      key,
		ExpectedTaskUID: taskUID,
		UpstreamBaseURL: upstream.URL,
		RealAPIKey:      "sk-real-key",
		ListenAddr:      "127.0.0.1:0",
	}
	h, hErr := p.Handler()
	if hErr != nil {
		t.Fatalf("Handler: %v", hErr)
	}
	srv := httptest.NewServer(h)
	defer srv.Close()

	token, err := Sign(key, taskUID, 10*time.Minute)
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}

	// /v1/files is NOT in the allowlist.
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/v1/files", nil)
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403 for unlisted route, got %d", resp.StatusCode)
	}
	if upstreamHit {
		t.Error("upstream was hit despite 403 — allowlist failed open")
	}
}

// TestProxy_IsAllowedRoute_BaselineUnchanged asserts that the hardcoded
// baseline routes are always allowed, even when ExtraAllowedRoutes is empty.
func TestProxy_IsAllowedRoute_BaselineUnchanged(t *testing.T) {
	p := &Proxy{}
	if !p.isAllowedRoute("POST", "/v1/messages") {
		t.Error("baseline POST /v1/messages should be allowed")
	}
	if !p.isAllowedRoute("POST", "/v1/messages/count_tokens") {
		t.Error("baseline POST /v1/messages/count_tokens should be allowed")
	}
}

// TestProxy_IsAllowedRoute_ExtraAllowed asserts that ExtraAllowedRoutes
// extends the allowlist additively (Phase 04.1 P4.2).
func TestProxy_IsAllowedRoute_ExtraAllowed(t *testing.T) {
	p := &Proxy{ExtraAllowedRoutes: []RouteSpec{{Method: "POST", PathPrefix: "/v1/files"}}}
	if !p.isAllowedRoute("POST", "/v1/files") {
		t.Error("extra route /v1/files should be allowed")
	}
	if !p.isAllowedRoute("POST", "/v1/files/abc") {
		t.Error("path-prefix match /v1/files/abc should be allowed")
	}
	// Baseline still applies when extras are set.
	if !p.isAllowedRoute("POST", "/v1/messages") {
		t.Error("baseline /v1/messages should still be allowed with extra routes present")
	}
}

// TestProxy_IsAllowedRoute_RejectsUnlisted asserts that a method or path
// not in the baseline or ExtraAllowedRoutes is rejected.
func TestProxy_IsAllowedRoute_RejectsUnlisted(t *testing.T) {
	p := &Proxy{ExtraAllowedRoutes: []RouteSpec{{Method: "POST", PathPrefix: "/v1/files"}}}
	if p.isAllowedRoute("DELETE", "/v1/files") {
		t.Error("non-allowed method DELETE should be rejected")
	}
	if p.isAllowedRoute("POST", "/v1/admin/users") {
		t.Error("unlisted path /v1/admin should be rejected (also webhook-denied but proxy is defense-in-depth)")
	}
	if p.isAllowedRoute("GET", "/v1/messages") {
		t.Error("GET on POST-only baseline route should be rejected")
	}
}

// TestProxyHandler_RejectsWrongMethodWith403 asserts that even an allowlisted
// path is rejected if the method doesn't match (e.g. GET /v1/messages).
func TestProxyHandler_RejectsWrongMethodWith403(t *testing.T) {
	key := []byte("12345678901234567890123456789012")
	const taskUID = "task-uid-abc"

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	p := &Proxy{
		SigningKey:      key,
		ExpectedTaskUID: taskUID,
		UpstreamBaseURL: upstream.URL,
		RealAPIKey:      "sk-real-key",
		ListenAddr:      "127.0.0.1:0",
	}
	h, hErr := p.Handler()
	if hErr != nil {
		t.Fatalf("Handler: %v", hErr)
	}
	srv := httptest.NewServer(h)
	defer srv.Close()

	token, err := Sign(key, taskUID, 10*time.Minute)
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}

	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/v1/messages", nil)
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403 for GET on POST-only route, got %d", resp.StatusCode)
	}
}

// ---------------------------------------------------------------------------
// Plan 13-02 Task 2 — billing-400 classifier + fail-fast latch
// ---------------------------------------------------------------------------

// TestIsCreditExhaustion_Classifies400Body asserts that the classifier
// correctly identifies Anthropic billing error bodies and rejects unrelated 400s.
func TestIsCreditExhaustion_Classifies400Body(t *testing.T) {
	billingBody := `{"type":"error","error":{"type":"invalid_request_error",` +
		`"message":"Your credit balance is too low to access the Anthropic API. Please go to Plans & Billing to upgrade or purchase credits."}}`
	if !isCreditExhaustion([]byte(billingBody)) {
		t.Error("expected isCreditExhaustion=true for credit balance body")
	}
	// Unrelated 400 must not match.
	if isCreditExhaustion([]byte(`{"type":"error","error":{"message":"invalid model"}}`)) {
		t.Error("expected isCreditExhaustion=false for non-billing 400 body")
	}
	// Empty body must not match.
	if isCreditExhaustion([]byte{}) {
		t.Error("expected isCreditExhaustion=false for empty body")
	}
}

// TestModifyResponse_BillingBodyPassesThroughUnchanged verifies that when the
// upstream returns HTTP 400 with a billing body, the client receives the full
// unmodified body (ModifyResponse must restore resp.Body after reading).
func TestModifyResponse_BillingBodyPassesThroughUnchanged(t *testing.T) {
	key := []byte("12345678901234567890123456789012")
	const taskUID = "task-uid-billing"
	billingBody := `{"type":"error","error":{"type":"invalid_request_error","message":"Your credit balance is too low"}}`

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(billingBody))
	}))
	defer upstream.Close()

	p := &Proxy{
		SigningKey:      key,
		ExpectedTaskUID: taskUID,
		UpstreamBaseURL: upstream.URL,
		RealAPIKey:      "sk-real-key",
		ListenAddr:      "127.0.0.1:0",
	}
	h, hErr := p.Handler()
	if hErr != nil {
		t.Fatalf("Handler: %v", hErr)
	}
	srv := httptest.NewServer(h)
	defer srv.Close()

	token, err := Sign(key, taskUID, 10*time.Minute)
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}

	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/v1/messages", nil)
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 pass-through; got %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if string(body) != billingBody {
		t.Errorf("body mismatch: got %q, want %q", string(body), billingBody)
	}
}

// TestBillingLatch_ShortCircuitsAfterFirst400 verifies that after the first
// credit-exhaustion 400, subsequent valid signed requests are short-circuited
// at the proxy (no upstream contact).
func TestBillingLatch_ShortCircuitsAfterFirst400(t *testing.T) {
	key := []byte("12345678901234567890123456789012")
	const taskUID = "task-uid-latch"
	billingBody := `{"type":"error","error":{"type":"invalid_request_error","message":"Your credit balance is too low"}}`

	upstreamHits := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		upstreamHits++
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(billingBody))
	}))
	defer upstream.Close()

	p := &Proxy{
		SigningKey:      key,
		ExpectedTaskUID: taskUID,
		UpstreamBaseURL: upstream.URL,
		RealAPIKey:      "sk-real-key",
		ListenAddr:      "127.0.0.1:0",
	}
	h, hErr := p.Handler()
	if hErr != nil {
		t.Fatalf("Handler: %v", hErr)
	}
	srv := httptest.NewServer(h)
	defer srv.Close()

	sendRequest := func() *http.Response {
		token, err := Sign(key, taskUID, 10*time.Minute)
		if err != nil {
			t.Fatalf("Sign: %v", err)
		}
		req, _ := http.NewRequest(http.MethodPost, srv.URL+"/v1/messages", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		return resp
	}

	// First request: hits upstream, gets billing 400, latch trips.
	resp1 := sendRequest()
	resp1.Body.Close()
	if resp1.StatusCode != http.StatusBadRequest {
		t.Fatalf("first request: expected 400; got %d", resp1.StatusCode)
	}

	// Second request: must NOT hit upstream (latch short-circuits).
	resp2 := sendRequest()
	defer resp2.Body.Close()

	if upstreamHits != 1 {
		t.Errorf("expected exactly 1 upstream hit; got %d", upstreamHits)
	}
	if resp2.StatusCode != http.StatusBadRequest {
		t.Fatalf("second request: expected 400 (local short-circuit); got %d", resp2.StatusCode)
	}
	if resp2.Header.Get("X-Tide-Billing-Halt") != "true" {
		t.Errorf("expected X-Tide-Billing-Halt: true header on short-circuited response; got %q",
			resp2.Header.Get("X-Tide-Billing-Halt"))
	}
}

// ---------------------------------------------------------------------------
// Plan 13-05 Task 3 — WR-03 defense-in-depth: synthetic latch body
// ---------------------------------------------------------------------------

// TestBillingLatch_SyntheticBodyDoesNotContainCreditBalance asserts that the
// fail-fast latch response body does NOT contain the "credit balance" substring
// (case-insensitive). The synthetic body must not feed the isBillingFailureReason
// classifier in the controller backstop — only the REAL first 400 body (whose
// text leads stderr and is captured in EnvelopeOut.Reason) is the billing evidence
// channel. Plan 13-05 Task 3 WR-03 defense-in-depth.
func TestBillingLatch_SyntheticBodyDoesNotContainCreditBalance(t *testing.T) {
	key := []byte("12345678901234567890123456789012")
	const taskUID = "task-uid-latch-body"
	billingBody := `{"type":"error","error":{"type":"invalid_request_error","message":"Your credit balance is too low"}}`

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(billingBody))
	}))
	defer upstream.Close()

	p := &Proxy{
		SigningKey:      key,
		ExpectedTaskUID: taskUID,
		UpstreamBaseURL: upstream.URL,
		RealAPIKey:      "sk-real-key",
		ListenAddr:      "127.0.0.1:0",
	}
	h, hErr := p.Handler()
	if hErr != nil {
		t.Fatalf("Handler: %v", hErr)
	}
	srv := httptest.NewServer(h)
	defer srv.Close()

	sendRequest := func() *http.Response {
		token, err := Sign(key, taskUID, 10*time.Minute)
		if err != nil {
			t.Fatalf("Sign: %v", err)
		}
		req, _ := http.NewRequest(http.MethodPost, srv.URL+"/v1/messages", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		return resp
	}

	// First request: hits upstream, latch trips on the real billing 400.
	resp1 := sendRequest()
	resp1.Body.Close()

	// Second request: served from the local latch short-circuit (synthetic body).
	resp2 := sendRequest()
	defer resp2.Body.Close()

	if resp2.Header.Get("X-Tide-Billing-Halt") != "true" {
		t.Fatalf("expected X-Tide-Billing-Halt: true on latch response; got %q", resp2.Header.Get("X-Tide-Billing-Halt"))
	}
	syntheticBody, _ := io.ReadAll(resp2.Body)
	// The synthetic body must NOT contain "credit balance" (case-insensitive) —
	// this is the manufactured-evidence channel that WR-03 closes.
	if strings.Contains(strings.ToLower(string(syntheticBody)), "credit balance") {
		t.Errorf("WR-03: synthetic latch body must NOT contain 'credit balance' substring; got body: %s", string(syntheticBody))
	}
}

// ---------------------------------------------------------------------------
// Plan 20-04 Task 1 — CACHE-01 FAIL-path body tee (Phase 20 D-02)
// ---------------------------------------------------------------------------

// TestTeeBodyDir_WritesRequestBodyToFile asserts that when TeeBodyDir is set,
// each /v1/messages request body is written to a sequentially-numbered file
// under that directory, and the request still reaches the upstream (body is
// restored for forwarding).
func TestTeeBodyDir_WritesRequestBodyToFile(t *testing.T) {
	key := []byte("12345678901234567890123456789012")
	const taskUID = "task-uid-tee"

	var upstreamBody []byte
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	teeDir := t.TempDir()

	p := &Proxy{
		SigningKey:      key,
		ExpectedTaskUID: taskUID,
		UpstreamBaseURL: upstream.URL,
		RealAPIKey:      "sk-real-key",
		ListenAddr:      "127.0.0.1:0",
		TeeBodyDir:      teeDir,
	}
	h, err := p.Handler()
	if err != nil {
		t.Fatalf("Handler: %v", err)
	}
	srv := httptest.NewServer(h)
	defer srv.Close()

	token, err := Sign(key, taskUID, 10*time.Minute)
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}

	const body = `{"model":"claude-sonnet-4-6","messages":[{"role":"user","content":"hello"}]}`

	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/v1/messages",
		strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200; got %d", resp.StatusCode)
	}

	// The tee file req-1.json must exist and contain the request body.
	teeFile := filepath.Join(teeDir, "req-1.json")
	teed, err := os.ReadFile(teeFile)
	if err != nil {
		t.Fatalf("tee file %s not found: %v", teeFile, err)
	}
	if string(teed) != body {
		t.Errorf("tee file contents = %q; want %q", string(teed), body)
	}

	// The upstream must also have received the full body (restore path works).
	if string(upstreamBody) != body {
		t.Errorf("upstream body = %q; want %q", string(upstreamBody), body)
	}

	// T-20-04-01: tee file must NOT contain API key or Authorization header.
	// The key is a header, not body — this asserts it doesn't leak into body.
	if strings.Contains(strings.ToLower(string(teed)), "x-api-key") ||
		strings.Contains(strings.ToLower(string(teed)), "authorization") ||
		strings.Contains(strings.ToLower(string(teed)), "sk-real-key") {
		t.Errorf("tee file must not contain API key or auth header; got: %s", string(teed))
	}
}

// TestTeeBodyDir_SequentialNumbering asserts that two sequential requests
// produce req-1.json and req-2.json (not overwritten).
func TestTeeBodyDir_SequentialNumbering(t *testing.T) {
	key := []byte("12345678901234567890123456789012")
	const taskUID = "task-uid-tee-seq"

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	teeDir := t.TempDir()

	p := &Proxy{
		SigningKey:      key,
		ExpectedTaskUID: taskUID,
		UpstreamBaseURL: upstream.URL,
		RealAPIKey:      "sk-real-key",
		ListenAddr:      "127.0.0.1:0",
		TeeBodyDir:      teeDir,
	}
	h, err := p.Handler()
	if err != nil {
		t.Fatalf("Handler: %v", err)
	}
	srv := httptest.NewServer(h)
	defer srv.Close()

	send := func(body string) {
		t.Helper()
		token, err := Sign(key, taskUID, 10*time.Minute)
		if err != nil {
			t.Fatalf("Sign: %v", err)
		}
		req, _ := http.NewRequest(http.MethodPost, srv.URL+"/v1/messages",
			strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+token)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("request: %v", err)
		}
		resp.Body.Close() //nolint:errcheck
	}

	send(`{"dispatch":"A"}`)
	send(`{"dispatch":"B"}`)

	b1, err1 := os.ReadFile(filepath.Join(teeDir, "req-1.json"))
	b2, err2 := os.ReadFile(filepath.Join(teeDir, "req-2.json"))
	if err1 != nil || err2 != nil {
		t.Fatalf("expected req-1.json and req-2.json; err1=%v err2=%v", err1, err2)
	}
	if string(b1) != `{"dispatch":"A"}` {
		t.Errorf("req-1.json = %q; want dispatch A", string(b1))
	}
	if string(b2) != `{"dispatch":"B"}` {
		t.Errorf("req-2.json = %q; want dispatch B", string(b2))
	}
}

// TestTeeBodyDir_DisabledByDefault asserts that when TeeBodyDir is empty,
// no tee files are written and the request path is unchanged.
func TestTeeBodyDir_DisabledByDefault(t *testing.T) {
	key := []byte("12345678901234567890123456789012")
	const taskUID = "task-uid-tee-off"

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	// TeeBodyDir intentionally NOT set.
	p := &Proxy{
		SigningKey:      key,
		ExpectedTaskUID: taskUID,
		UpstreamBaseURL: upstream.URL,
		RealAPIKey:      "sk-real-key",
		ListenAddr:      "127.0.0.1:0",
	}
	h, err := p.Handler()
	if err != nil {
		t.Fatalf("Handler: %v", err)
	}
	srv := httptest.NewServer(h)
	defer srv.Close()

	token, err := Sign(key, taskUID, 10*time.Minute)
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/v1/messages",
		strings.NewReader(`{"model":"test"}`))
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()
	// Just confirm the request succeeded without panicking or failing.
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 on default (no-tee) path; got %d", resp.StatusCode)
	}
}

// TestNonBilling400_DoesNotLatch verifies that a non-billing 400 upstream
// response does not trip the latch — subsequent requests still reach upstream.
func TestNonBilling400_DoesNotLatch(t *testing.T) {
	key := []byte("12345678901234567890123456789012")
	const taskUID = "task-uid-nolatch"
	nonBillingBody := `{"type":"error","error":{"type":"invalid_request_error","message":"invalid model"}}`

	upstreamHits := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		upstreamHits++
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(nonBillingBody))
	}))
	defer upstream.Close()

	p := &Proxy{
		SigningKey:      key,
		ExpectedTaskUID: taskUID,
		UpstreamBaseURL: upstream.URL,
		RealAPIKey:      "sk-real-key",
		ListenAddr:      "127.0.0.1:0",
	}
	h, hErr := p.Handler()
	if hErr != nil {
		t.Fatalf("Handler: %v", hErr)
	}
	srv := httptest.NewServer(h)
	defer srv.Close()

	sendRequest := func() *http.Response {
		token, err := Sign(key, taskUID, 10*time.Minute)
		if err != nil {
			t.Fatalf("Sign: %v", err)
		}
		req, _ := http.NewRequest(http.MethodPost, srv.URL+"/v1/messages", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		return resp
	}

	r1 := sendRequest()
	r1.Body.Close()
	r2 := sendRequest()
	r2.Body.Close()

	if upstreamHits != 2 {
		t.Errorf("expected 2 upstream hits (no latch on non-billing 400); got %d", upstreamHits)
	}
}
