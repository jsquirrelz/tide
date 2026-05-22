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
