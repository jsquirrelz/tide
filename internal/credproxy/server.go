package credproxy

import (
	"context"
	"crypto/tls"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
)

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
func (p *Proxy) Handler() http.Handler {
	upstream, _ := url.Parse(p.UpstreamBaseURL)
	rp := httputil.NewSingleHostReverseProxy(upstream)
	origDirector := rp.Director
	rp.Director = func(req *http.Request) {
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

		rp.ServeHTTP(w, r)
	})
}

// ListenAndServe starts the TLS server and blocks until ctx is cancelled, at
// which point it calls srv.Shutdown(ctx) for a graceful drain.
//
// CertFile and KeyFile must point at the cert + key written by MintSelfSignedCert.
func (p *Proxy) ListenAndServe(ctx context.Context) error {
	srv := &http.Server{
		Addr:      p.ListenAddr,
		Handler:   p.Handler(),
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
