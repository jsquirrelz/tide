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
	"context"
	"crypto/tls"
	"fmt"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
)

// RouteSpec is a (method, path-prefix) tuple for the per-Project credproxy
// allowlist extension (Phase 04.1 P4.2). This type is intentionally duplicated
// from api/v1alpha1.RouteSpec — the credproxy package MUST NOT import
// api/v1alpha1 (providerfirewall analyzer rule); JSON serialisation at the
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
