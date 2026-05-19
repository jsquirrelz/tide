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

// Command dashboard is the TIDE read-only dashboard backend (Phase 4 D-D2).
// router.go declares the chi route tree as a single function so cmd/dashboard
// main.go AND router_test.go construct the same surface from a Dependencies
// struct. DASH-05 requires that EVERY registered handler be HTTP GET — the
// route table is walked by TestZeroMutationRoutes to enforce this at build
// time.
package main

import (
	"errors"
	"io/fs"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-logr/logr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	dashboardapi "github.com/jsquirrelz/tide/cmd/dashboard/api"
	"github.com/jsquirrelz/tide/cmd/dashboard/hub"
)

// Dependencies is the injection surface for RegisterRoutes. cmd/dashboard
// main.go builds one with the controller-runtime client + the SSE hub +
// the embedded SPA filesystem; tests build one with a fake client + nil
// hub to assert route-table shape (zero-mutation guard).
type Dependencies struct {
	// Client is the controller-runtime client backed by the informer cache.
	// Per D-D2 the dashboard SA has read-only RBAC; this client is the
	// dashboard's source of truth for all reads.
	Client client.Client

	// Hub is the in-process Project-keyed SSE fan-out (Task 2).
	// Plan 04-11 wires the SSE handlers to this hub; plan 04-10 keeps the
	// field present so RegisterRoutes can pass it through and main.go can
	// thread the constructed Hub end-to-end.
	Hub *hub.Hub

	// Log is the structured logger; the chi middleware chain uses it for
	// access logging and panics flow through Recoverer for 500s.
	Log logr.Logger

	// SPAFS is the embedded React SPA bundle (cmd/dashboard/embed.Dist
	// rebased at the `dist` sub-tree so HTTP file paths read directly).
	// Tests pass an empty/fs.FS stub to verify the route table without
	// requiring the real embed.
	SPAFS fs.FS
}

// RegisterRoutes builds the dashboard's chi.Mux. DASH-05 invariant: EVERY
// route registered here MUST be HTTP GET — TestZeroMutationRoutes walks
// the resulting route tree at test time and fails the build on any
// non-GET method.
//
// Route table (Wave 5 inventory):
//
//   GET /healthz                              — process liveness
//   GET /readyz                               — process readiness
//   GET /api/v1/projects                      — list
//   GET /api/v1/projects/{name}               — single
//   GET /api/v1/projects/{name}/events        — SSE project events (plan 04-11)
//   GET /api/v1/tasks/{name}/log              — SSE pod-log (plan 04-11 Task 2)
//   GET /*                                    — SPA fallback (embed.FS)
//
// EventsHandler is registered only when deps.Hub is non-nil; tests pass
// nil to walk just the synchronous route shape.
func RegisterRoutes(deps Dependencies) chi.Router {
	r := chi.NewRouter()

	r.Use(middleware.RequestID)
	r.Use(middleware.Recoverer)
	// Logger is added last so it sees the assigned RequestID; chi's stock
	// Logger writes to log.Default(); the structured logr.Logger
	// in deps.Log is reserved for handlers that need per-request fields.
	r.Use(middleware.Logger)

	// Process-level liveness/readiness — exposed on the API port too so
	// `curl :8080/healthz` works without the operator knowing about the
	// internal manager port. main.go ALSO registers manager-level healthz
	// on :8081 gated by informer cache sync; the path here is the
	// stop-gap that doesn't require apiserver connectivity to return 200.
	r.Get("/healthz", processHealthzHandler)
	r.Get("/readyz", processReadyzHandler)

	// API surface — DASH-05: GET-only. Plan 04-11 added the SSE events
	// endpoint; pod-log SSE lands in plan 04-11 Task 2.
	ph := &dashboardapi.ProjectsHandler{
		Client: deps.Client,
		Log:    deps.Log,
	}
	var eh *dashboardapi.EventsHandler
	if deps.Hub != nil {
		eh = dashboardapi.NewEventsHandler(deps.Hub, deps.Log)
	}
	r.Route("/api/v1", func(r chi.Router) {
		r.Get("/projects", ph.List)
		r.Get("/projects/{name}", ph.Get)
		if eh != nil {
			r.Get("/projects/{name}/events", eh.ServeHTTP)
		}
		// Plan 04-11 Task 2 lands:
		//   r.Get("/tasks/{name}/log", logsSSEHandler.ServeHTTP)
	})

	// SPA fallback (D-X2 single-image deploy). Serves the embedded
	// `dist/` tree; on any path that doesn't resolve to a file, serves
	// `index.html` so React Router's client-side routes work on direct
	// navigation. nil SPAFS = 404 (tests inject nil to assert the route
	// is registered without exercising the file system).
	//
	// Registered via r.Get (NOT r.Handle) — DASH-05 / T-04-D5: r.Handle
	// registers for ALL methods including POST/PATCH/PUT/DELETE which
	// would mass-violate the zero-mutation guard. r.Get binds GET only;
	// any other method on the wildcard path falls through to chi's
	// default 405 MethodNotAllowed.
	if deps.SPAFS != nil {
		spaHandler := spaFallback(deps.SPAFS)
		r.Get("/*", spaHandler.ServeHTTP)
	} else {
		// Stub: register a Get handler so the route table walker still
		// sees a single `/*` GET entry (DASH-05 zero-mutation invariant
		// covers this code path too).
		r.Get("/*", func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "SPA assets not embedded", http.StatusNotFound)
		})
	}

	return r
}

// processHealthzHandler is the API-port liveness probe. It does NOT gate on
// informer-cache sync — that's the manager-port `/healthz` (registered in
// main.go via mgr.AddHealthzCheck). This handler returns 200 the instant
// the HTTP server is bound, which is what `kubectl exec curl localhost:8080`
// from inside the dashboard Pod expects.
func processHealthzHandler(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}

// processReadyzHandler mirrors processHealthzHandler. The cache-sync-gated
// readiness check is on the manager port (:8081) so Helm's readinessProbe
// can target it; the API-port `/readyz` exists so the SPA can probe
// "is my backend up?" without needing manager-port access.
func processReadyzHandler(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}

// spaFallback serves static files from the embedded SPA bundle and falls
// back to `index.html` for any path that doesn't resolve to a real file
// (React Router client-side routing).
//
// Uses http.ServeFileFS (Go 1.22+) rather than http.FileServer because
// FileServer issues 301 redirects between `/index.html` ↔ `/`, which
// combined with a fallback-to-index strategy produces a redirect loop
// for any not-found path. ServeFileFS reads a specific named file with
// no redirect logic — exactly the SPA-fallback semantics we want.
//
// Security: fs.Stat + ServeFileFS read against the embed.FS root; `..`
// traversal is rejected by the fs.FS contract.
func spaFallback(spa fs.FS) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Normalize the URL path. chi's `/*` parameter has the leading
		// slash stripped; we want to read against the embed root.
		urlPath := strings.TrimPrefix(r.URL.Path, "/")
		if urlPath == "" {
			urlPath = "index.html"
		}
		// Attempt to stat the requested file; on miss OR on a directory
		// (the embed.FS may contain `assets/` etc.), serve index.html
		// — React Router does the rest client-side.
		info, err := fs.Stat(spa, urlPath)
		if err != nil {
			if !errors.Is(err, fs.ErrNotExist) {
				http.Error(w, "failed to stat asset", http.StatusInternalServerError)
				return
			}
			http.ServeFileFS(w, r, spa, "index.html")
			return
		}
		if info.IsDir() {
			http.ServeFileFS(w, r, spa, "index.html")
			return
		}
		http.ServeFileFS(w, r, spa, urlPath)
	})
}
