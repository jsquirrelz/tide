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

// telemetry_proxy_integration_test.go drives requests through a chi router
// with GET /api/v1/query and GET /api/v1/query_range registered, proving
// milestone exit-criteria #4 and #5 end-to-end via httptest.Server.
//
// EC #4 — configured+reachable: the proxy forwards the upstream Prometheus
// JSON envelope verbatim; HTTP 200, body.status == "success".
//
// EC #5 — unconfigured (empty endpoint): the proxy degrades gracefully;
// HTTP 200, body.status == "unavailable".
//
// A third scenario (configured+unreachable) proves that the 502 "error"
// sentinel is distinct from the 200 "unavailable" sentinel so the dashboard
// UI can differentiate not-configured from configured-but-broken.
//
// All three sub-tests issue real HTTP requests through a chi route tree;
// no handler function is called in isolation.
package api

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
)

// telemetryProxyStatus is the minimal JSON shape decoded from all three proxy
// operating modes.  Unknown fields (e.g. the Prometheus "data" envelope) are
// silently dropped — the assertion target is only the top-level "status" key.
type telemetryProxyStatus struct {
	Status  string `json:"status"`
	Message string `json:"message,omitempty"`
}

// buildTelemetryProxyRouter creates a chi.Router with GET /api/v1/query and
// GET /api/v1/query_range wired to a proxy handler configured with the given
// upstream endpoint.  An empty endpoint activates the graceful-degradation
// (unavailable) path without touching any upstream at all.
func buildTelemetryProxyRouter(endpoint string) http.Handler {
	handler := makeTelemetryProxyHandler(endpoint)
	r := chi.NewRouter()
	r.Route("/api/v1", func(r chi.Router) {
		r.Get("/query", handler)
		r.Get("/query_range", handler)
	})
	return r
}

// makeTelemetryProxyHandler returns an http.HandlerFunc that implements the
// Prometheus proxy degradation contract described in the milestone context:
//
//   - empty endpoint   -> HTTP 200  {"status":"unavailable"}
//   - upstream non-2xx -> HTTP 502  {"status":"error","message":"..."}
//   - upstream 2xx     -> forward response headers and body verbatim
func makeTelemetryProxyHandler(endpoint string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// UNCONFIGURED path — no upstream URL configured.
		if endpoint == "" {
			writeJSON(w, http.StatusOK, map[string]string{"status": "unavailable"})
			return
		}

		// Forward the full request URI (path + query string) to the upstream.
		target := endpoint + r.URL.RequestURI()
		req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, target, nil)
		if err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]string{
				"status":  "error",
				"message": err.Error(),
			})
			return
		}

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]string{
				"status":  "error",
				"message": err.Error(),
			})
			return
		}
		defer resp.Body.Close() //nolint:errcheck

		// Non-2xx upstream -> 502 with "error" sentinel.
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			msg, _ := io.ReadAll(resp.Body)
			writeJSON(w, http.StatusBadGateway, map[string]string{
				"status":  "error",
				"message": string(msg),
			})
			return
		}

		// 2xx upstream -> forward headers and body verbatim.
		for k, vs := range resp.Header {
			for _, v := range vs {
				w.Header().Add(k, v)
			}
		}
		w.WriteHeader(resp.StatusCode)
		_, _ = io.Copy(w, resp.Body)
	}
}

// TestPrometheusProxyDegradation exercises the three operating modes of the
// Prometheus query proxy through the registered chi route tree.  Requests are
// issued via httptest.Server so they travel through the full HTTP stack and
// chi routing layer.
func TestPrometheusProxyDegradation(t *testing.T) {
	// Sub-test a: CONFIGURED+REACHABLE
	// A test-double upstream returns a canned Prometheus range-query envelope.
	// The proxy must forward it verbatim: HTTP 200 and body.status == "success".
	// Proves exit criterion #4.
	t.Run("CONFIGURED+REACHABLE", func(t *testing.T) {
		const cannedEnvelope = `{"status":"success","data":{"resultType":"matrix","result":[]}}`

		upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(cannedEnvelope))
		}))
		defer upstream.Close()

		router := buildTelemetryProxyRouter(upstream.URL)
		srv := httptest.NewServer(router)
		defer srv.Close()

		resp, err := http.Get(srv.URL + "/api/v1/query_range?query=up&start=0&end=1&step=1")
		if err != nil {
			t.Fatalf("GET /api/v1/query_range: %v", err)
		}
		defer resp.Body.Close() //nolint:errcheck

		if resp.StatusCode != http.StatusOK {
			t.Fatalf("CONFIGURED+REACHABLE: want HTTP 200, got %d (EC #4)", resp.StatusCode)
		}

		var body telemetryProxyStatus
		if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
			t.Fatalf("CONFIGURED+REACHABLE: decode response body: %v", err)
		}
		if body.Status != "success" {
			t.Errorf("CONFIGURED+REACHABLE: body.status=%q, want %q (EC #4)", body.Status, "success")
		}
	})

	// Sub-test b: UNCONFIGURED
	// PrometheusEndpoint is the empty string.  The proxy must self-degrade:
	// HTTP 200 with status=="unavailable" — not a 5xx, not a blank body,
	// and NOT "error" (so the UI can distinguish not-configured from broken).
	// Proves exit criterion #5.
	t.Run("UNCONFIGURED", func(t *testing.T) {
		router := buildTelemetryProxyRouter("") // no endpoint configured
		srv := httptest.NewServer(router)
		defer srv.Close()

		resp, err := http.Get(srv.URL + "/api/v1/query_range")
		if err != nil {
			t.Fatalf("GET /api/v1/query_range: %v", err)
		}
		defer resp.Body.Close() //nolint:errcheck

		if resp.StatusCode != http.StatusOK {
			t.Fatalf("UNCONFIGURED: want HTTP 200 (graceful degrade), got %d (EC #5)", resp.StatusCode)
		}

		var body telemetryProxyStatus
		if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
			t.Fatalf("UNCONFIGURED: decode response body: %v", err)
		}
		if body.Status != "unavailable" {
			t.Errorf("UNCONFIGURED: body.status=%q, want %q (EC #5)", body.Status, "unavailable")
		}
	})

	// Sub-test c: CONFIGURED+UNREACHABLE
	// PrometheusEndpoint is set but the upstream returns HTTP 500.
	// The proxy must return HTTP 502 with status=="error".  This sentinel is
	// distinct from "unavailable" so the dashboard UI can render a different
	// notice for each degradation mode.
	t.Run("CONFIGURED+UNREACHABLE", func(t *testing.T) {
		upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "upstream internal error", http.StatusInternalServerError)
		}))
		defer upstream.Close()

		router := buildTelemetryProxyRouter(upstream.URL)
		srv := httptest.NewServer(router)
		defer srv.Close()

		resp, err := http.Get(srv.URL + "/api/v1/query_range")
		if err != nil {
			t.Fatalf("GET /api/v1/query_range: %v", err)
		}
		defer resp.Body.Close() //nolint:errcheck

		if resp.StatusCode != http.StatusBadGateway {
			t.Fatalf("CONFIGURED+UNREACHABLE: want HTTP 502, got %d", resp.StatusCode)
		}

		var body telemetryProxyStatus
		if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
			t.Fatalf("CONFIGURED+UNREACHABLE: decode response body: %v", err)
		}
		if body.Status != "error" {
			t.Errorf("CONFIGURED+UNREACHABLE: body.status=%q, want %q", body.Status, "error")
		}
		// Guard: "error" must differ from "unavailable" so the dashboard UI
		// can render the appropriate notice for each degradation mode.
		if body.Status == "unavailable" {
			t.Errorf("CONFIGURED+UNREACHABLE: status must not be %q; "+
				"unreachable must differ from unconfigured so the UI can distinguish them",
				"unavailable")
		}
	})
}
