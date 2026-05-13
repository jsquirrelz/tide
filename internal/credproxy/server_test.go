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

	srv := httptest.NewServer(p.Handler())
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
	srv := httptest.NewServer(p.Handler())
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
	srv := httptest.NewServer(p.Handler())
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
