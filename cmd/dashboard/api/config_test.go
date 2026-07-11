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
// TELEM-03 / D-14): HTTP 200, application/json, body exactly
// {"telemetryEnabled":<bool>} — the single boolean the Telemetry view's
// banner derivation consumes.
func TestConfigHandlerGet(t *testing.T) {
	tests := []struct {
		name     string
		enabled  bool
		wantBody string
	}{
		{name: "telemetry enabled", enabled: true, wantBody: `{"telemetryEnabled":true}`},
		{name: "telemetry disabled", enabled: false, wantBody: `{"telemetryEnabled":false}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := &ConfigHandler{TelemetryEnabled: tt.enabled, Log: testr.New(t)}
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
