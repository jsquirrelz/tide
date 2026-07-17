/*
Copyright 2026 TIDE Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-logr/logr/testr"
)

// TestConfigHandlerGet locks the GET /api/v1/config wire contract (Phase 38
// TELEM-03 / D-14 + Phase 46 OBS-04 / D-10): HTTP 200, application/json,
// body exactly {"telemetryEnabled":<bool>,"phoenixBaseURL":<string>} — the
// telemetryEnabled boolean the Telemetry view's banner derivation consumes,
// and the phoenixBaseURL string the dashboard deep-link mount points
// consume. telemetryEnabled semantics are byte-identical to before this
// plan; phoenixBaseURL is a pure additive field.
func TestConfigHandlerGet(t *testing.T) {
	tests := []struct {
		name           string
		enabled        bool
		phoenixBaseURL string
		wantBody       string
	}{
		{name: "telemetry enabled, phoenix unset", enabled: true, phoenixBaseURL: "", wantBody: `{"telemetryEnabled":true,"phoenixBaseURL":""}`},
		{name: "telemetry disabled, phoenix unset", enabled: false, phoenixBaseURL: "", wantBody: `{"telemetryEnabled":false,"phoenixBaseURL":""}`},
		{name: "telemetry enabled, phoenix set", enabled: true, phoenixBaseURL: "http://phoenix.tide-system:6006", wantBody: `{"telemetryEnabled":true,"phoenixBaseURL":"http://phoenix.tide-system:6006"}`},
		{name: "telemetry disabled, phoenix set", enabled: false, phoenixBaseURL: "http://phoenix.tide-system:6006", wantBody: `{"telemetryEnabled":false,"phoenixBaseURL":"http://phoenix.tide-system:6006"}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := &ConfigHandler{TelemetryEnabled: tt.enabled, PhoenixBaseURL: tt.phoenixBaseURL, Log: testr.New(t)}
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/api/v1/config", nil)

			h.Get(rec, req)

			if rec.Code != http.StatusOK {
				t.Errorf("status: want %d, got %d", http.StatusOK, rec.Code)
			}
			if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
				t.Errorf("Content-Type: want application/json, got %q", ct)
			}
			if got := strings.TrimSpace(rec.Body.String()); got != tt.wantBody {
				t.Errorf("body: want %q, got %q", tt.wantBody, got)
			}
		})
	}
}
