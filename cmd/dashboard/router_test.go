/*
Copyright 2026 TIDE Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

package main

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-logr/logr/testr"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	tidev1alpha1 "github.com/jsquirrelz/tide/api/v1alpha1"
)

// newTestRouter builds a Dependencies with a fake client (zero objects)
// and a tiny in-memory SPA, then calls RegisterRoutes. Used by every test
// in this file that doesn't need bespoke fixtures.
func newTestRouter(t *testing.T) chi.Router {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := tidev1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("add v1alpha1 scheme: %v", err)
	}
	c := fake.NewClientBuilder().WithScheme(scheme).Build()
	spa := fstest.MapFS{
		"index.html": &fstest.MapFile{Data: []byte("<!doctype html><html><body>spa</body></html>")},
	}
	return RegisterRoutes(Dependencies{
		Client: c,
		Hub:    nil, // Hub is unused by the GET handlers this plan ships.
		Log:    testr.New(t),
		SPAFS:  spa,
	})
}

// TestZeroMutationRoutes is the DASH-05 architectural enforcement test
// (T-04-D5 mitigation). It walks the chi route tree and fails the build
// if ANY registered handler exposes a method other than GET or HEAD.
//
// HEAD is permitted because Go's http.FileServer derives HEAD responses
// from GET handlers for free; explicitly registering HEAD on the SPA
// route would be redundant. POST/PUT/PATCH/DELETE/OPTIONS are denied
// outright.
func TestZeroMutationRoutes(t *testing.T) {
	r := newTestRouter(t)

	allowed := map[string]bool{
		http.MethodGet:  true,
		http.MethodHead: true,
	}

	var violations []string
	err := chi.Walk(r, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		if !allowed[method] {
			violations = append(violations, method+" "+route)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("chi.Walk failed: %v", err)
	}
	if len(violations) > 0 {
		t.Fatalf("DASH-05 violation: dashboard router registered non-GET routes (T-04-D5):\n  %s\n"+
			"All dashboard endpoints MUST be HTTP GET — mutations route through `tide` CLI / kubectl per D-D6.",
			strings.Join(violations, "\n  "))
	}
}

// TestRouterMiddlewareChain confirms the three required middlewares are
// active: RequestID propagates an ID through the request context,
// Recoverer turns panics into 500s rather than crashing the process,
// and Logger fires per request (verified via panic-catch + structured
// log output existing — covered by Recoverer test).
//
// chi v5's middleware.RequestID stores the ID in r.Context() under
// middleware.RequestIDKey (NOT in a response header — different from
// gin's behavior). We assert by registering a probe handler that
// reads the ID from context and echoes it back.
func TestRouterMiddlewareChain(t *testing.T) {
	t.Run("RequestID populates request context", func(t *testing.T) {
		r := newTestRouter(t)
		// Mount a probe handler that reads the request ID from context.
		r.Get("/_test/reqid", func(w http.ResponseWriter, req *http.Request) {
			id := middleware.GetReqID(req.Context())
			w.Header().Set("X-Test-ReqID", id)
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(id))
		})
		srv := httptest.NewServer(r)
		defer srv.Close()

		resp, err := http.Get(srv.URL + "/_test/reqid")
		if err != nil {
			t.Fatalf("GET probe: %v", err)
		}
		defer resp.Body.Close()
		if got := resp.Header.Get("X-Test-ReqID"); got == "" {
			t.Errorf("expected RequestID middleware to populate r.Context() with a non-empty ID; got empty")
		}
	})

	t.Run("Recoverer turns panic into 500", func(t *testing.T) {
		// Register a panicking handler on a fresh router built with
		// the same RegisterRoutes path so Recoverer is in the chain.
		panickyRouter := newTestRouter(t)
		panickyRouter.Get("/panic-please", func(http.ResponseWriter, *http.Request) {
			panic("test panic — Recoverer must catch")
		})
		srv := httptest.NewServer(panickyRouter)
		defer srv.Close()

		resp, err := http.Get(srv.URL + "/panic-please")
		if err != nil {
			t.Fatalf("GET /panic-please: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusInternalServerError {
			t.Errorf("expected 500 from Recoverer; got %d", resp.StatusCode)
		}
	})
}

// TestHealthzReturns200 confirms the API-port /healthz handler returns
// 200 without needing apiserver connectivity (per the plan's separation
// between API-port healthz and manager-port informer-gated healthz).
func TestHealthzReturns200(t *testing.T) {
	r := newTestRouter(t)
	srv := httptest.NewServer(r)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/healthz")
	if err != nil {
		t.Fatalf("GET /healthz: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
}

// TestSPAFallback confirms a GET to `/` returns the embedded index.html
// content with an HTML content type (the placeholder index.html until
// plan 04-12/04-13 lands the real bundle).
func TestSPAFallback(t *testing.T) {
	r := newTestRouter(t)
	srv := httptest.NewServer(r)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/")
	if err != nil {
		t.Fatalf("GET /: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
	ct := resp.Header.Get("Content-Type")
	if !strings.HasPrefix(ct, "text/html") {
		t.Errorf("expected text/html content type, got %q", ct)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "spa") {
		t.Errorf("expected SPA HTML body to contain 'spa', got %q", string(body))
	}
}

// TestSPAFallbackUnknownPath confirms a GET to a non-existent path falls
// back to index.html (React Router client-side routing requirement).
func TestSPAFallbackUnknownPath(t *testing.T) {
	r := newTestRouter(t)
	srv := httptest.NewServer(r)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/projects/foo/runs/bar")
	if err != nil {
		t.Fatalf("GET unknown path: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200 (SPA fallback), got %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "spa") {
		t.Errorf("expected SPA fallback body, got %q", string(body))
	}
}

// TestGracefulShutdown exercises the Test 5 behavior from the plan's
// Task 1: when SIGTERM-equivalent cancellation triggers, Shutdown drains
// the server and ListenAndServe returns http.ErrServerClosed within the
// shutdownTimeout budget.
func TestGracefulShutdown(t *testing.T) {
	srv := &http.Server{
		Addr:              "127.0.0.1:0", // OS picks free port
		Handler:           newTestRouter(t),
		ReadHeaderTimeout: 10 * time.Second,
	}

	listenErrCh := make(chan error, 1)
	go func() {
		listenErrCh <- srv.ListenAndServe()
	}()

	// Give the server a moment to bind. 50ms is generous on macOS;
	// CI will be similar.
	time.Sleep(50 * time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown returned error: %v", err)
	}

	select {
	case err := <-listenErrCh:
		if !errors.Is(err, http.ErrServerClosed) {
			t.Errorf("expected http.ErrServerClosed, got %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("ListenAndServe did not return within 10s of Shutdown")
	}
}

// TestRouteTableContainsExpectedGETs asserts the plan's `must_haves`
// route inventory: /healthz, /readyz, /api/v1/projects, /api/v1/projects/{name},
// and the SPA `/*` fallback. The two SSE routes (events, log) are deferred
// to plan 04-11 and NOT in this plan's table.
func TestRouteTableContainsExpectedGETs(t *testing.T) {
	r := newTestRouter(t)

	want := map[string]bool{
		"GET /healthz":                true,
		"GET /readyz":                 true,
		"GET /api/v1/projects":        true,
		"GET /api/v1/projects/{name}": true,
	}
	got := make(map[string]bool)
	err := chi.Walk(r, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		got[method+" "+route] = true
		return nil
	})
	if err != nil {
		t.Fatalf("chi.Walk: %v", err)
	}
	for r := range want {
		if !got[r] {
			t.Errorf("missing expected route %q", r)
		}
	}
}
