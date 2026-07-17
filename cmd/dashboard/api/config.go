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

// config.go — GET /api/v1/config (Phase 38 TELEM-03 / D-14).
//
// Exposes the chart's prometheus.enabled toggle (transported into the
// dashboard Pod as the PROMETHEUS_ENABLED env, resolved by main.go) to the
// UI so the Telemetry view's banner can distinguish disabled-by-config from
// no-data. GET-only per the DASH-05 zero-mutation contract.
package api

import (
	"encoding/json"
	"net/http"

	"github.com/go-logr/logr"
)

// ConfigHandler serves the dashboard's read-only UI configuration surface.
// TelemetryEnabled is resolved once at process start (main.go's
// telemetryEnabledFromEnv) and injected here — the handler never reads env.
type ConfigHandler struct {
	// TelemetryEnabled mirrors the chart's prometheus.enabled toggle (D-14).
	TelemetryEnabled bool
	// PhoenixBaseURL mirrors the operator-set PHOENIX_BASE_URL env (Phase 46
	// OBS-04 / D-10, the telemetryEnabled precedent applied verbatim). Empty
	// string IS the no-link sentinel — the SPA's deep-link mount points
	// render nothing when this is "". The raw value is passed through
	// unmodified; trailing-slash normalization happens exactly once,
	// SPA-side in lib/phoenixLink.ts (D-11).
	PhoenixBaseURL string
	// Log is the structured logger for encode errors.
	Log logr.Logger
}

// configResponse is the GET /api/v1/config body. The key names
// "telemetryEnabled" and "phoenixBaseURL" are a locked wire contract:
// telemetryEnabled is consumed by TelemetryView.tsx's banner derivation;
// phoenixBaseURL is consumed by lib/phoenixLink.ts (Phase 46, additive-only).
type configResponse struct {
	TelemetryEnabled bool   `json:"telemetryEnabled"`
	PhoenixBaseURL   string `json:"phoenixBaseURL"`
}

// Get handles GET /api/v1/config — returns
// {"telemetryEnabled": <bool>, "phoenixBaseURL": <string>}.
func (h *ConfigHandler) Get(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	resp := configResponse{
		TelemetryEnabled: h.TelemetryEnabled,
		PhoenixBaseURL:   h.PhoenixBaseURL,
	}
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		h.Log.Error(err, "failed to encode config response")
	}
}
