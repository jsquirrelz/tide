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

// events_sse.go — DASH-03: GET /api/v1/projects/{name}/events serves the
// project-scoped Server-Sent Events stream backed by the in-process Hub
// (cmd/dashboard/hub/pubsub.go).
//
// Architecture:
//
//   browser EventSource ──HTTP GET──> EventsHandler.ServeHTTP
//                                       │
//                                       ├─ hub.Subscribe(project, lastEventID)
//                                       │   │
//                                       │   └─ deferred Unsubscribe (Pitfall 22)
//                                       │
//                                       └─ select on { ctx.Done, ticker.C, sub.Events }
//                                              │
//                                              ├─ ctx.Done → return (client disconnected)
//                                              ├─ ticker.C → write ":heartbeat\n\n"  (Pitfall 23)
//                                              └─ Events  → write "id:N\nevent:T\ndata:J\n\n"
//
// Pitfall 22 mitigation (DoS via subscriber leak): defer hub.Unsubscribe(sub)
// runs on EVERY exit path; ctx.Done() fires when the client TCP connection
// drops; goroutine count stays bounded regardless of tab-close behavior.
//
// Pitfall 23 mitigation (nginx-ingress buffering): X-Accel-Buffering:no +
// 15s heartbeat comments prevent reverse-proxy buffering and idle-close.
//
// T-04-D5 (zero-mutation guard): handler is GET-only; TestZeroMutationRoutes
// in cmd/dashboard/router_test.go walks the chi route tree and fails the
// build on any non-GET registration.

package api

import (
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-logr/logr"

	"github.com/jsquirrelz/tide/cmd/dashboard/hub"
)

// defaultHeartbeatInterval is the cadence at which idle SSE connections
// emit `:heartbeat\n\n` comments. 15s is below the 60s nginx-ingress
// default proxy_read_timeout (Pitfall 23) — the heartbeat keeps the
// connection alive even when no project events are arriving.
const defaultHeartbeatInterval = 15 * time.Second

// EventsHandler implements GET /api/v1/projects/{name}/events. Each
// invocation opens a long-lived SSE connection that streams events
// published to the bound Hub for the requested project.
//
// The handler owns no goroutines other than ServeHTTP itself — fan-out
// is provided by the Hub. This keeps the goroutine count strictly
// proportional to the active SSE connection count (T-04-D3 mitigation).
type EventsHandler struct {
	Hub *hub.Hub
	Log logr.Logger

	// heartbeatInterval is private + injected via options so tests can
	// shrink it to 50ms without exposing the field on the public surface.
	heartbeatInterval time.Duration
}

// EventsHandlerOption configures an EventsHandler at construction time.
type EventsHandlerOption func(*EventsHandler)

// WithHeartbeatInterval overrides the default 15s heartbeat cadence.
// Used by tests to exercise the heartbeat path in <500ms without a fast
// clock.
func WithHeartbeatInterval(d time.Duration) EventsHandlerOption {
	return func(h *EventsHandler) {
		h.heartbeatInterval = d
	}
}

// NewEventsHandler constructs an EventsHandler bound to the given Hub.
// Default heartbeatInterval is 15s; override via WithHeartbeatInterval.
func NewEventsHandler(h *hub.Hub, log logr.Logger, opts ...EventsHandlerOption) *EventsHandler {
	eh := &EventsHandler{
		Hub:               h,
		Log:               log,
		heartbeatInterval: defaultHeartbeatInterval,
	}
	for _, opt := range opts {
		opt(eh)
	}
	return eh
}

// ServeHTTP serves the SSE stream for a project. Lifecycle:
//
//  1. Set Pitfall 23 headers + 200 OK + flush (proxies see headers
//     immediately even if no events arrive for a while).
//  2. Parse Last-Event-ID header → Hub.Subscribe(project, lastEventID).
//     Replay happens synchronously inside Subscribe; any buffered events
//     with ID > lastEventID land in sub.Events() before any new Publish.
//  3. defer Hub.Unsubscribe(sub) — Pitfall 22 mitigation. This MUST run
//     on every exit path (panic, ctx cancel, normal return).
//  4. Loop on select { ctx.Done(), ticker.C, sub.Events() } until the
//     client disconnects.
func (h *EventsHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		// Standard chi + net/http ResponseWriter implements Flusher; this
		// branch only fires under a weird custom middleware that wraps
		// the writer without preserving the Flusher contract.
		http.Error(w, "streaming unsupported by this server", http.StatusInternalServerError)
		return
	}

	// Pitfall 23 — disable buffering at every layer we can address.
	// X-Accel-Buffering targets nginx-ingress; Cache-Control:no-cache
	// stops CDNs / browser-side caches; Connection:keep-alive is the
	// standard SSE preamble.
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	projectName := chi.URLParam(r, "name")
	if projectName == "" {
		// Defensive: chi should never invoke us without `name` because the
		// route pattern requires it. Surface a clean event-stream-style
		// error and return — the connection is already 200 so we emit a
		// `event: error\ndata: ...\n\n` frame rather than rewriting headers.
		fmt.Fprint(w, "event: error\ndata: {\"error\":\"missing project name\"}\n\n")
		flusher.Flush()
		return
	}

	lastEventID := parseLastEventID(r.Header.Get("Last-Event-ID"))

	sub := h.Hub.Subscribe(projectName, lastEventID)
	defer h.Hub.Unsubscribe(sub)

	ticker := time.NewTicker(h.heartbeatInterval)
	defer ticker.Stop()

	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			// Client disconnected (browser tab close, TCP drop, ctx
			// cancellation). Deferred Unsubscribe + Hub.Unsubscribe's
			// channel-close cause the no-more-events path to land
			// without leaking the goroutine. Pitfall 22 closure.
			return

		case <-ticker.C:
			// SSE comment frame — invisible to the EventSource API but
			// keeps the underlying TCP connection from being declared
			// idle by intermediate proxies. RFC-compliant SSE comment
			// syntax: leading `:` + content + blank line.
			if _, err := fmt.Fprint(w, ": heartbeat\n\n"); err != nil {
				// Write error = client gone (TCP RST). Return; the
				// deferred Unsubscribe handles cleanup.
				return
			}
			flusher.Flush()

		case ev, ok := <-sub.Events():
			if !ok {
				// Hub.Unsubscribe closed our channel — exit cleanly.
				return
			}
			// SSE frame format: id + event + data + blank line.
			// The recv-side EventSource parses these per the WHATWG spec.
			if _, err := fmt.Fprintf(w, "id: %d\nevent: %s\ndata: %s\n\n",
				ev.ID, ev.Type, string(ev.JSON)); err != nil {
				// Write error = client gone (TCP RST). Return; deferred
				// Unsubscribe handles cleanup.
				return
			}
			flusher.Flush()
		}
	}
}

// parseLastEventID parses the `Last-Event-ID` HTTP header per the
// EventSource spec — an unsigned integer. Empty / malformed values
// return 0, which Hub.Subscribe interprets as "no replay".
func parseLastEventID(raw string) int64 {
	if raw == "" {
		return 0
	}
	n, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || n < 0 {
		return 0
	}
	return n
}
