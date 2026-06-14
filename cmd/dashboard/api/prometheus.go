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

// prometheus.go — GET /api/v1/query and GET /api/v1/query_range
// (plan-02-promql-proxy-handlers).
//
// PrometheusHandler is a server-side PromQL proxy that forwards instant and
// range queries to the configured Prometheus endpoint. Three-shape degradation:
//   - Empty Endpoint: HTTP 200 {"status":"unavailable"} (graceful sentinel)
//   - Configured but unreachable: HTTP 502 {"status":"error","message":"..."}
//   - Configured and reachable: HTTP 200, Prometheus JSON envelope passed through
//
// DASH-05 zero-mutation contract: both handlers are HTTP GET only.
package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/go-logr/logr"
)

// proxyTimeout bounds individual upstream Prometheus requests. Prevents a
// hanging upstream from pinning a dashboard goroutine indefinitely (TELEM-06).
const proxyTimeout = 30 * time.Second

// proxyClient is the package-level HTTP client for upstream Prometheus calls.
// Using a dedicated client with an explicit Timeout lets us set a hard bound
// that survives browser disconnects — context propagation via the request
// handles the disconnect case; proxyTimeout is the last-resort bound (TELEM-06).
var proxyClient = &http.Client{Timeout: proxyTimeout}

// PrometheusHandler proxies PromQL queries to the configured Prometheus
// endpoint. Endpoint is the base URL (e.g. "http://prometheus:9090"); an
// empty value is valid — both handlers return the unavailable sentinel
// (HTTP 200, {"status":"unavailable"}).
type PrometheusHandler struct {
	// Endpoint is the Prometheus base URL. Empty is the graceful-degradation state.
	Endpoint string
	// Log is the structured logger for proxy errors.
	Log logr.Logger
}

// Query handles GET /api/v1/query — PromQL instant queries.
// Forwards query and time parameters to the upstream Prometheus /api/v1/query.
func (h *PrometheusHandler) Query(w http.ResponseWriter, r *http.Request) {
	h.proxy(w, r, "/api/v1/query")
}

// QueryRange handles GET /api/v1/query_range — PromQL range queries.
// Forwards query, start, end, and step parameters to the upstream endpoint.
func (h *PrometheusHandler) QueryRange(w http.ResponseWriter, r *http.Request) {
	h.proxy(w, r, "/api/v1/query_range")
}

// proxy forwards the incoming request to the upstream Prometheus path, passing
// all query parameters verbatim and re-emitting the upstream response.
func (h *PrometheusHandler) proxy(w http.ResponseWriter, r *http.Request, path string) {
	w.Header().Set("Content-Type", "application/json")

	// Graceful-degradation: no endpoint configured — return unavailable sentinel.
	if h.Endpoint == "" {
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "unavailable"})
		return
	}

	upstream, err := url.Parse(h.Endpoint)
	if err != nil {
		h.Log.Error(err, "invalid prometheus endpoint", "endpoint", h.Endpoint)
		w.WriteHeader(http.StatusBadGateway)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"status":  "error",
			"message": fmt.Sprintf("invalid upstream endpoint: %v", err),
		})
		return
	}
	upstream.Path = strings.TrimRight(upstream.Path, "/") + path
	upstream.RawQuery = r.URL.RawQuery

	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, upstream.String(), nil)
	if err != nil {
		h.Log.Error(err, "failed to build upstream request", "url", upstream.String())
		w.WriteHeader(http.StatusBadGateway)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"status":  "error",
			"message": fmt.Sprintf("failed to build request: %v", err),
		})
		return
	}
	resp, err := proxyClient.Do(req)
	if err != nil {
		h.Log.Error(err, "prometheus upstream unreachable", "url", upstream.String())
		w.WriteHeader(http.StatusBadGateway)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"status":  "error",
			"message": fmt.Sprintf("upstream unreachable: %v", err),
		})
		return
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		h.Log.Info("prometheus upstream non-2xx", "status", resp.StatusCode, "url", upstream.String())
		w.WriteHeader(http.StatusBadGateway)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"status":  "error",
			"message": fmt.Sprintf("upstream returned %d", resp.StatusCode),
		})
		return
	}

	// Pass the upstream content-type and body through verbatim.
	if ct := resp.Header.Get("Content-Type"); ct != "" {
		w.Header().Set("Content-Type", ct)
	}
	w.WriteHeader(http.StatusOK)
	_, _ = io.Copy(w, resp.Body)
}
