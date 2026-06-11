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
// with GET /api/v1/query and GET /api/v1/query_range registered against the
// production PrometheusHandler, proving milestone exit-criteria #4 and #5
// end-to-end via httptest.Server.
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
// All three sub-tests issue real HTTP requests through a chi route tree
// wired to PrometheusHandler — the same handler RegisterRoutes installs;
// no handler function is called in isolation and no test-local
// reimplementation stands in for the production code path.
package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/go-logr/logr"
)

// telemetryProxyStatus is the minimal JSON shape decoded from all three proxy
// operating modes.  Unknown fields (e.g. the Prometheus "data" envelope) are
// silently dropped — the assertion target is only the top-level "status" key.
type telemetryProxyStatus struct {
	Status  string `json:"status"`
	Message string `json:"message,omitempty"`
}

// buildTelemetryProxyRouter creates a chi.Router with GET /api/v1/query and
// GET /api/v1/query_range wired to the production PrometheusHandler
// configured with the given upstream endpoint.  An empty endpoint activates
// the graceful-degradation (unavailable) path without touching any upstream
// at all.
func buildTelemetryProxyRouter(endpoint string) http.Handler {
	handler := &PrometheusHandler{
		Endpoint: endpoint,
		Log:      logr.Discard(),
	}
	r := chi.NewRouter()
	r.Route("/api/v1", func(r chi.Router) {
		r.Get("/query", handler.Query)
		r.Get("/query_range", handler.QueryRange)
	})
	return r
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

// TestPrometheusProxyDeadUpstream exercises the connection-refused path:
// the endpoint is configured but no server listens there.  The production
// handler must return HTTP 502 with status=="error" — never a hang and never
// the "unavailable" sentinel reserved for the unconfigured state.
func TestPrometheusProxyDeadUpstream(t *testing.T) {
	// Reserve a port by starting and immediately closing a listener, so the
	// URL points at a port with nothing listening.
	dead := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {}))
	deadURL := dead.URL
	dead.Close()

	router := buildTelemetryProxyRouter(deadURL)
	srv := httptest.NewServer(router)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/v1/query?query=up")
	if err != nil {
		t.Fatalf("GET /api/v1/query: %v", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusBadGateway {
		t.Fatalf("DEAD UPSTREAM: want HTTP 502, got %d", resp.StatusCode)
	}

	var body telemetryProxyStatus
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("DEAD UPSTREAM: decode response body: %v", err)
	}
	if body.Status != "error" {
		t.Errorf("DEAD UPSTREAM: body.status=%q, want %q", body.Status, "error")
	}
}
