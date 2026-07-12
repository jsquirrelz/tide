/*
Copyright 2026 TIDE Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/go-chi/chi/v5"
	"github.com/go-logr/logr/testr"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	tidev1alpha3 "github.com/jsquirrelz/tide/api/v1alpha3"
)

// TestTelemetryEnabledFromEnv covers the D-13/D-14 env resolution: the
// chart-passed PROMETHEUS_ENABLED is authoritative when it is a literal
// "true"/"false"; anything else (legacy env-less deploys included) falls
// back to PROM_ENDPOINT presence so old charts keep behaving sensibly.
func TestTelemetryEnabledFromEnv(t *testing.T) {
	tests := []struct {
		name         string
		promEnabled  *string // nil = unset
		promEndpoint *string // nil = unset
		want         bool
	}{
		{name: "explicit true", promEnabled: new("true"), want: true},
		{
			name:         "explicit false wins over set PROM_ENDPOINT",
			promEnabled:  new("false"),
			promEndpoint: new("http://prometheus:9090"),
			want:         false,
		},
		{
			name:         "unset falls back to PROM_ENDPOINT presence",
			promEndpoint: new("http://x"),
			want:         true,
		},
		{name: "unset with no PROM_ENDPOINT is disabled", want: false},
		{
			name:         "unrecognized value falls back to PROM_ENDPOINT presence",
			promEnabled:  new("yes"),
			promEndpoint: new("http://x"),
			want:         true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setOrUnsetEnv(t, "PROMETHEUS_ENABLED", tt.promEnabled)
			setOrUnsetEnv(t, "PROM_ENDPOINT", tt.promEndpoint)
			if got := telemetryEnabledFromEnv(); got != tt.want {
				t.Errorf("telemetryEnabledFromEnv() = %v, want %v", got, tt.want)
			}
		})
	}
}

// setOrUnsetEnv sets the env var when v is non-nil, otherwise guarantees it
// is unset. t.Setenv registers restoration either way (its cleanup restores
// the pre-test value even after os.Unsetenv).
func setOrUnsetEnv(t *testing.T, key string, v *string) {
	t.Helper()
	if v != nil {
		t.Setenv(key, *v)
		return
	}
	t.Setenv(key, "") // register cleanup restoring the original value
	if err := os.Unsetenv(key); err != nil {
		t.Fatalf("Unsetenv(%s): %v", key, err)
	}
}

// TestConfigRouteRegistered asserts the Phase 38 TELEM-03 route lands in the
// composed route table exactly once, as GET, and that
// Dependencies.TelemetryEnabled flows through RegisterRoutes into the
// response body.
func TestConfigRouteRegistered(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := tidev1alpha3.AddToScheme(scheme); err != nil {
		t.Fatalf("add scheme: %v", err)
	}
	spa := fstest.MapFS{
		"index.html": &fstest.MapFile{Data: []byte("<html></html>")},
	}
	r := RegisterRoutes(Dependencies{
		Client:           fake.NewClientBuilder().WithScheme(scheme).Build(),
		Log:              testr.New(t),
		SPAFS:            spa,
		TelemetryEnabled: true,
	})

	// Route-table shape: GET /api/v1/config appears exactly once.
	count := 0
	err := chi.Walk(r, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		if route == "/api/v1/config" {
			count++
			if method != http.MethodGet {
				t.Errorf("route /api/v1/config registered as %s, want GET (DASH-05)", method)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("chi.Walk: %v", err)
	}
	if count != 1 {
		t.Fatalf("GET /api/v1/config registered %d times, want exactly 1", count)
	}

	// Wire contract end-to-end: the injected boolean reaches the body.
	srv := httptest.NewServer(r)
	defer srv.Close()
	resp, err := http.Get(srv.URL + "/api/v1/config")
	if err != nil {
		t.Fatalf("GET /api/v1/config: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status: want 200, got %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if got := strings.TrimSpace(string(body)); got != `{"telemetryEnabled":true}` {
		t.Errorf("body: want %q, got %q", `{"telemetryEnabled":true}`, got)
	}
}
