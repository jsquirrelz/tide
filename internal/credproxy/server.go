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
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
)

// RouteSpec is a (method, path-prefix) tuple for the per-Project credproxy
// allowlist extension (Phase 04.1 P4.2). This type is intentionally duplicated
// from api/v1alpha3.RouteSpec — the credproxy package MUST NOT import
// api/v1alpha3 (providerfirewall analyzer rule); JSON serialisation at the
// TIDE_ALLOWED_ROUTES env-var boundary bridges the two types.
type RouteSpec struct {
	Method     string `json:"method"`
	PathPrefix string `json:"pathPrefix"`
}

// Proxy is the HTTPS reverse-proxy sidecar that validates HMAC-signed bearer
// tokens and injects the real ANTHROPIC_API_KEY on outbound requests.
//
// It runs as a Kubernetes native sidecar (D-C1) listening on 127.0.0.1:8443.
// The subagent container submits requests to this proxy using a short-lived
// signed token; the proxy verifies the token and replaces it with the real
// API key before forwarding to the upstream (api.anthropic.com or override).
//
// Token validation delegates to Verify (token.go); invalid tokens return
// HTTP 401 with a structured body (T-02-05-01 / T-02-05-06).
//
// Phase 13 D-04: billingHalted is an atomic flag set by ModifyResponse on the
// first credit-exhaustion 400. Once set, every subsequent valid signed request
// is short-circuited locally (HTTP 400 + X-Tide-Billing-Halt: true) without
// contacting the upstream. This prevents context ramps after a billing dry-out.
type Proxy struct {
	SigningKey        []byte // from Secret tide-signing-key (envFrom)
	ExpectedTaskUID   string // from env TIDE_TASK_UID
	UpstreamBaseURL   string // "https://api.anthropic.com"
	RealAPIKey        string // from envFrom secretRef <providerSecretRef>
	ListenAddr        string // "127.0.0.1:8443"
	CertFile, KeyFile string // /etc/tide/proxy/{cert.pem,key.pem}

	// ExtraAllowedRoutes carries operator-extended (method, path-prefix) tuples
	// populated from TIDE_ALLOWED_ROUTES env var (Phase 04.1 P4.2). The
	// hardcoded allowedRoutes baseline is ALWAYS checked first and cannot be
	// removed — operator additions are additive, never restrictive.
	ExtraAllowedRoutes []RouteSpec

	// TeeBodyDir, when non-empty, enables the FAIL-path body capture for the
	// CACHE-01 spike (Phase 20 D-02). Each forwarded /v1/messages request body
	// is written to a sequentially-numbered file (req-1.json, req-2.json, …)
	// inside TeeBodyDir so the two-pod request bodies can be diffed for prefix
	// divergence. Default empty = disabled (no behavior change to normal runs).
	//
	// Security invariants enforced by the tee:
	//   - Only the outbound request BODY is written; the real ANTHROPIC_API_KEY
	//     is a header injected by the Director and does not appear in the body.
	//   - ANTHROPIC_API_KEY and x-api-key headers are never captured.
	//   - Each tee file is capped at teeBodMaxSize bytes.
	//   - The caller (the spike) must create TeeBodyDir with 0700 permissions
	//     and clean it up after the spike exits.
	TeeBodyDir string

	// billingHalted is set to 1 by ModifyResponse on the first detected
	// credit-exhaustion 400 (D-04). Subsequent requests short-circuit before
	// reaching the upstream. Uses atomic.Bool for safe concurrent access.
	billingHalted atomic.Bool

	// teeMu guards teeSeq for atomic-increment-safe sequential numbering.
	teeMu  sync.Mutex
	teeSeq int
}

// isCreditExhaustion returns true if the upstream HTTP response body contains
// Anthropic's credit-exhaustion error signal.
//
// Classification: case-insensitive substring "credit balance" — conservative
// to survive Anthropic API message rewording (T-13-06 mitigate). Never
// exact-match the error string; Anthropic has reworded it before.
//
// Provider-firewall: this is the Anthropic-specific classification at the HTTP
// boundary. It lives in internal/credproxy (the legal home per the firewall
// analyzer rule). Do NOT import api/v1alpha3 or controller-runtime here.
func isCreditExhaustion(body []byte) bool {
	return strings.Contains(strings.ToLower(string(body)), "credit balance")
}

// allowedRoutes is the defense-in-depth allowlist of (method, path-prefix)
// tuples that the proxy forwards to the upstream (CR-04). Any request whose
// (method, path) is not in this set is rejected with 403, preventing a
// compromised subagent from reaching arbitrary Anthropic API endpoints
// (e.g. /v1/files/*, admin/billing surfaces) using the real org-level key.
//
// Keep this list narrow: only the SDK surface the subagent legitimately
// uses (currently the Messages API surface). Phase 3+ may expand this if
// the executor SDK genuinely requires more endpoints.
var allowedRoutes = []struct {
	method string
	prefix string
}{
	{http.MethodPost, "/v1/messages"},
	{http.MethodPost, "/v1/messages/count_tokens"},
}

// isAllowedRoute returns true iff (method, path) matches the hardcoded
// baseline allowlist or any ExtraAllowedRoutes entry on the Proxy.
//
// The baseline is checked first and is always enforced — operator
// ExtraAllowedRoutes are additive extensions, not replacements (P4.2).
func (p *Proxy) isAllowedRoute(method, path string) bool {
	// Hardcoded baseline (CR-04 — preserved verbatim, cannot be removed).
	for _, route := range allowedRoutes {
		if method != route.method {
			continue
		}
		if path == route.prefix || strings.HasPrefix(path, route.prefix+"/") {
			return true
		}
	}
	// Per-Project extensions injected via TIDE_ALLOWED_ROUTES (Phase 04.1 P4.2).
	for _, r := range p.ExtraAllowedRoutes {
		if method == r.Method && (path == r.PathPrefix || strings.HasPrefix(path, r.PathPrefix+"/")) {
			return true
		}
	}
	return false
}

// teeBodyMaxSize caps the number of request body bytes written to each tee
// file. Requests larger than this limit are truncated at the cap — the prefix
// is sufficient for diffing the per-pod system prompt divergence.
const teeBodyMaxSize = 1 << 20 // 1 MiB

// teeBody writes a copy of body to a sequentially-numbered file under
// p.TeeBodyDir (e.g. req-1.json, req-2.json). Called only when TeeBodyDir is
// non-empty. Write errors are logged but do not abort the request — the tee is
// diagnostic; the upstream path must not be disrupted by a tee failure.
//
// Security: body is the raw request body forwarded to the upstream; it does NOT
// contain the real ANTHROPIC_API_KEY (that is a header injected by the Director
// after the body is read). Signing tokens in the Authorization / x-api-key
// request headers are likewise not captured here.
func (p *Proxy) teeBody(body []byte) {
	if len(body) > teeBodyMaxSize {
		body = body[:teeBodyMaxSize]
	}
	p.teeMu.Lock()
	p.teeSeq++
	seq := p.teeSeq
	p.teeMu.Unlock()

	name := filepath.Join(p.TeeBodyDir, fmt.Sprintf("req-%d.json", seq))
	if err := os.WriteFile(name, body, 0600); err != nil {
		log.Printf("credproxy: tee-body-dir: failed to write %s: %v", name, err)
	}
}

// Handler returns an http.Handler that validates incoming bearer tokens and
// forwards approved requests to the upstream with the real API key injected.
//
// Token extraction order (per D-C1 subagent SDK header behaviour):
//  1. Authorization header (strip "Bearer " prefix)
//  2. x-api-key header (Anthropic SDK alternate form)
//
// On Verify failure: 401 with body "unauthorized: <reason>".
// On Verify success: Authorization + x-api-key rewritten to RealAPIKey;
// Host set to upstream.Host; all other headers (including anthropic-version)
// pass through unchanged.
func (p *Proxy) Handler() (http.Handler, error) {
	upstream, err := url.Parse(p.UpstreamBaseURL)
	if err != nil {
		return nil, fmt.Errorf("credproxy: invalid upstream URL %q: %w", p.UpstreamBaseURL, err)
	}
	if upstream == nil || upstream.Host == "" {
		return nil, fmt.Errorf("credproxy: invalid upstream URL %q: missing host", p.UpstreamBaseURL)
	}
	rp := httputil.NewSingleHostReverseProxy(upstream)
	//nolint:staticcheck // SA1019: rp.Director is load-bearing credproxy security semantics (header rewrite firewall);
	// migrating to Rewrite is out of scope for lint hygiene and would alter the security-critical request path.
	origDirector := rp.Director
	rp.Director = func(req *http.Request) { //nolint:staticcheck // SA1019: see above — credproxy Director firewall
		origDirector(req)
		req.Host = upstream.Host
		// Replace signed-token with real key in both header forms that the
		// Anthropic SDK may send (D-C1 bearer-token contract).
		req.Header.Set("Authorization", "Bearer "+p.RealAPIKey)
		req.Header.Set("x-api-key", p.RealAPIKey)
		// anthropic-version and other headers pass through untouched.
	}

	// Phase 13 D-04: ModifyResponse inspects upstream 400 responses for the
	// Anthropic billing signal. On detection, it sets the billingHalted latch so
	// subsequent requests short-circuit without contacting the upstream.
	// The body is passed through byte-identical to the subagent (RESEARCH Pitfall 5:
	// always restore resp.Body after io.ReadAll, even on error paths).
	rp.ModifyResponse = func(resp *http.Response) error {
		if resp.StatusCode != http.StatusBadRequest {
			return nil
		}
		body, err := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		// Always restore the body before returning, even on read error.
		if err != nil {
			resp.Body = io.NopCloser(bytes.NewReader(nil))
			return nil
		}
		resp.Body = io.NopCloser(bytes.NewReader(body))
		if isCreditExhaustion(body) {
			p.billingHalted.Store(true)
			// stdlib log — credproxy must NOT import controller-runtime (providerfirewall boundary).
			log.Printf("billing-halt: Anthropic credit-exhaustion 400 detected at credproxy (status=%d)", resp.StatusCode)
		}
		return nil
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Extract bearer token: try Authorization Bearer first, then x-api-key.
		token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if token == "" {
			token = r.Header.Get("x-api-key")
		}

		if err := Verify(p.SigningKey, token, p.ExpectedTaskUID); err != nil {
			http.Error(w, "unauthorized: "+err.Error(), http.StatusUnauthorized)
			return
		}

		// CR-04: Defense-in-depth — even a valid token may only reach the
		// narrow SDK surface the subagent legitimately uses. Reject any
		// (method, path) outside the allowlist (baseline + extensions) with 403.
		if !p.isAllowedRoute(r.Method, r.URL.Path) {
			http.Error(w, "forbidden: route not allowed", http.StatusForbidden)
			return
		}

		// CACHE-01 FAIL-path body tee (Phase 20 D-02). When TeeBodyDir is set,
		// snapshot the outbound request body before forwarding so the two-pod
		// prefix divergence can be diffed. The body is read, teed, then restored
		// on r.Body so the reverse-proxy Director can forward it unmodified.
		// This branch only runs for /v1/messages (the spike target route).
		if p.TeeBodyDir != "" && r.URL.Path == "/v1/messages" && r.Body != nil {
			raw, readErr := io.ReadAll(io.LimitReader(r.Body, teeBodyMaxSize+1))
			_ = r.Body.Close()
			if readErr == nil {
				p.teeBody(raw)
			} else {
				log.Printf("credproxy: tee-body-dir: failed to read request body: %v", readErr)
			}
			// Restore body so the reverse-proxy can forward it normally.
			r.Body = io.NopCloser(bytes.NewReader(raw))
			r.ContentLength = int64(len(raw))
		}

		// Phase 13 D-04: fail-fast latch. After the first credit-exhaustion 400,
		// short-circuit every subsequent request locally without contacting the
		// upstream.
		//
		// WR-03 (Plan 13-05): the body is DELIBERATELY non-matching for
		// isBillingFailureReason (no "credit balance" substring). The reconciler
		// backstop's billing evidence is the REAL first 400 (which set this latch
		// and whose message leads the subagent's stderr — EnvelopeOut.Reason is
		// built from the head of stderr, so the real message survives). Post-resume
		// staleness is fenced by AnnotationBillingResumedAt (billing_halt.go). A
		// subagent container that restarts inside an already-latched pod sees this
		// synthetic body as its first stderr error; without the body rewording its
		// Reason would also carry the classifier substring, enabling WR-03 re-stamp
		// even for a container that never touched the real Anthropic API.
		if p.billingHalted.Load() {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("X-Tide-Billing-Halt", "true")
			w.WriteHeader(http.StatusBadRequest)
			_, _ = fmt.Fprint(w, `{"type":"error","error":{"type":"invalid_request_error",`+
				`"message":"TIDE billing halt is active (cached at credproxy); upstream not contacted. `+
				`Run tide resume after refilling credits."}}`)
			return
		}

		rp.ServeHTTP(w, r)
	}), nil
}

// ListenAndServe starts the TLS server and blocks until ctx is cancelled, at
// which point it calls srv.Shutdown(ctx) for a graceful drain.
//
// CertFile and KeyFile must point at the cert + key written by MintSelfSignedCert.
func (p *Proxy) ListenAndServe(ctx context.Context) error {
	handler, err := p.Handler()
	if err != nil {
		return err
	}
	srv := &http.Server{
		Addr:      p.ListenAddr,
		Handler:   handler,
		TLSConfig: &tls.Config{MinVersion: tls.VersionTLS12},
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.ListenAndServeTLS(p.CertFile, p.KeyFile)
	}()

	select {
	case <-ctx.Done():
		return srv.Shutdown(context.Background())
	case err := <-errCh:
		return err
	}
}
