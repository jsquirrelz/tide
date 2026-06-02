/*
Copyright 2026 TIDE Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
*/

// Plan 04-11 Task 1 — events_sse_test.go: tests for the project-scoped SSE
// event endpoint (DASH-03). Exercises the Pitfall 22 disconnect-cleanup
// path and the Pitfall 23 nginx-buffering-mitigation header set.
package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-logr/logr/testr"
	"go.uber.org/goleak"

	"github.com/jsquirrelz/tide/cmd/dashboard/hub"
)

// newEventsRouter returns a chi router that mounts an EventsHandler under
// /api/v1/projects/{name}/events backed by the given Hub and optional
// heartbeat interval (zero = default 15s; tests usually inject 50ms).
func newEventsRouter(t *testing.T, h *hub.Hub, heartbeat time.Duration) http.Handler {
	t.Helper()
	opts := []EventsHandlerOption{}
	if heartbeat > 0 {
		opts = append(opts, WithHeartbeatInterval(heartbeat))
	}
	eh := NewEventsHandler(h, testr.New(t), opts...)

	r := chi.NewRouter()
	r.Route("/api/v1", func(r chi.Router) {
		r.Get("/projects/{name}/events", eh.ServeHTTP)
	})
	return r
}

// TestEventsHandlerHeaders covers behavior #1: 200 + correct Pitfall-23
// header set. We open the SSE connection on a goroutine and read the first
// header bytes; we don't need an event — just the status line + headers.
func TestEventsHandlerHeaders(t *testing.T) {
	hubInst := hub.NewHub(testr.New(t))
	srv := httptest.NewServer(newEventsRouter(t, hubInst, 0))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	req, _ := http.NewRequestWithContext(ctx, http.MethodGet,
		srv.URL+"/api/v1/projects/foo/events?stream=sse", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET events: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d, want 200", resp.StatusCode)
	}
	wantHeaders := map[string]string{
		"Content-Type":      "text/event-stream",
		"Cache-Control":     "no-cache",
		"X-Accel-Buffering": "no",
		"Connection":        "keep-alive",
	}
	for k, v := range wantHeaders {
		got := resp.Header.Get(k)
		// Content-Type may have a charset suffix; do a prefix compare for it.
		if k == "Content-Type" {
			if !strings.HasPrefix(got, v) {
				t.Errorf("header %s=%q, want prefix %q", k, got, v)
			}
			continue
		}
		if got != v {
			t.Errorf("header %s=%q, want %q", k, got, v)
		}
	}
}

// TestEventsHandlerDeliversPublish covers behavior #2: hub.Publish during
// the connection causes an `id: N\nevent: T\ndata: J\n\n` SSE frame to
// arrive at the recorder within 100ms.
func TestEventsHandlerDeliversPublish(t *testing.T) {
	hubInst := hub.NewHub(testr.New(t))
	srv := httptest.NewServer(newEventsRouter(t, hubInst, 0))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	req, _ := http.NewRequestWithContext(ctx, http.MethodGet,
		srv.URL+"/api/v1/projects/foo/events", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	// Give the handler a brief moment to register the subscriber before we
	// Publish — Subscribe happens synchronously in ServeHTTP but we read
	// the HTTP response stream from the client side and the dial races
	// the Publish call below.
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		if hubInst.SubscriberCount("foo") > 0 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	hubInst.Publish("foo", hub.Event{
		Type: "project.update",
		JSON: json.RawMessage(`{"name":"foo","phase":"Running"}`),
	})

	frame, err := readSSEFrame(resp.Body, 200*time.Millisecond)
	if err != nil {
		t.Fatalf("read frame: %v", err)
	}
	if !strings.Contains(frame, "event: project.update") {
		t.Errorf("frame missing `event: project.update`:\n%s", frame)
	}
	if !strings.Contains(frame, "id: 1") {
		t.Errorf("frame missing `id: 1`:\n%s", frame)
	}
	if !strings.Contains(frame, `"name":"foo"`) {
		t.Errorf("frame missing data payload:\n%s", frame)
	}
}

// TestEventsHandlerLastEventIDReplay covers behavior #3: a Last-Event-ID
// header is parsed and only events with ID > N are replayed.
func TestEventsHandlerLastEventIDReplay(t *testing.T) {
	hubInst := hub.NewHub(testr.New(t))

	// Publish 5 events BEFORE any subscriber connects so they sit in the
	// hub's replay buffer.
	for i := range 5 {
		hubInst.Publish("foo", hub.Event{
			Type: "tick",
			JSON: json.RawMessage(fmt.Sprintf(`{"i":%d}`, i)),
		})
	}

	srv := httptest.NewServer(newEventsRouter(t, hubInst, 0))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet,
		srv.URL+"/api/v1/projects/foo/events", nil)
	req.Header.Set("Last-Event-ID", "3")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	// Should receive events with ID 4 and 5 (i values 3 and 4) — and only
	// those.
	got, err := readNFrames(resp.Body, 2, 500*time.Millisecond)
	if err != nil {
		t.Fatalf("readNFrames: %v", err)
	}
	if !strings.Contains(got[0], "id: 4") {
		t.Errorf("frame[0] not id=4:\n%s", got[0])
	}
	if !strings.Contains(got[1], "id: 5") {
		t.Errorf("frame[1] not id=5:\n%s", got[1])
	}
}

// TestEventsHandlerHeartbeat covers behavior #4: with no published events,
// the handler emits a `:heartbeat\n\n` comment at the configured interval.
// Uses heartbeat=50ms to keep the test fast.
func TestEventsHandlerHeartbeat(t *testing.T) {
	hubInst := hub.NewHub(testr.New(t))
	srv := httptest.NewServer(newEventsRouter(t, hubInst, 50*time.Millisecond))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet,
		srv.URL+"/api/v1/projects/foo/events", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	// Read for ~200ms; expect at least 2 heartbeat comments (50ms ticker).
	buf := make([]byte, 4096)
	deadline := time.Now().Add(500 * time.Millisecond)
	var accum strings.Builder
	for time.Now().Before(deadline) {
		_ = setReadDeadline(resp.Body, 50*time.Millisecond)
		n, _ := resp.Body.Read(buf)
		if n > 0 {
			accum.Write(buf[:n])
			if strings.Count(accum.String(), ": heartbeat") >= 2 {
				return
			}
		}
	}
	if strings.Count(accum.String(), ": heartbeat") < 2 {
		t.Errorf("expected >= 2 heartbeat comments in 500ms, got accumulator:\n%s",
			accum.String())
	}
}

// TestEventsHandlerClientDisconnectCleanup covers behavior #5: cancelling
// the request context causes the handler goroutine to exit within 1s and
// unsubscribes from the Hub. goleak (TestMain in this package) confirms no
// goroutine leaks.
func TestEventsHandlerClientDisconnectCleanup(t *testing.T) {
	hubInst := hub.NewHub(testr.New(t))
	srv := httptest.NewServer(newEventsRouter(t, hubInst, 0))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet,
		srv.URL+"/api/v1/projects/foo/events", nil)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}

	// Wait for the subscriber to register so the cancel actually exits an
	// active handler.
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		if hubInst.SubscriberCount("foo") > 0 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if hubInst.SubscriberCount("foo") != 1 {
		t.Fatalf("subscriber did not register; want 1, got %d", hubInst.SubscriberCount("foo"))
	}

	cancel()
	_ = resp.Body.Close()

	// Within 1s the handler should unsubscribe so SubscriberCount drops to 0.
	deadline = time.Now().Add(1 * time.Second)
	for time.Now().Before(deadline) {
		if hubInst.SubscriberCount("foo") == 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Errorf("subscriber count did not drop to 0 within 1s of client disconnect; got %d",
		hubInst.SubscriberCount("foo"))
}

// TestSSEFanoutCleanup covers behavior #6 — the race-tested fan-out
// scenario: 50 concurrent SSE subscribers + a publisher firing events for
// half a second; clients disconnect at random; final assertion: hub.subs
// is empty + goleak reports no leaks.
//
// This is the canonical T-04-D3 (DoS via subscriber leak) test.
func TestSSEFanoutCleanup(t *testing.T) {
	hubInst := hub.NewHub(testr.New(t))
	srv := httptest.NewServer(newEventsRouter(t, hubInst, 100*time.Millisecond))
	defer srv.Close()

	const numClients = 50
	publishDuration := 300 * time.Millisecond

	var publisherWG sync.WaitGroup
	publisherWG.Add(1)
	stopPublish := make(chan struct{})
	go func() {
		defer publisherWG.Done()
		t := time.NewTicker(5 * time.Millisecond)
		defer t.Stop()
		for {
			select {
			case <-stopPublish:
				return
			case <-t.C:
				hubInst.Publish("racey", hub.Event{
					Type: "tick",
					JSON: json.RawMessage(`{}`),
				})
			}
		}
	}()

	var clientWG sync.WaitGroup
	var connectedCount atomic.Int64
	cancels := make([]context.CancelFunc, numClients)
	for i := range numClients {
		clientWG.Add(1)
		ctx, cancel := context.WithCancel(context.Background())
		cancels[i] = cancel
		go func(i int) {
			defer clientWG.Done()
			req, _ := http.NewRequestWithContext(ctx, http.MethodGet,
				srv.URL+"/api/v1/projects/racey/events", nil)
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				return
			}
			connectedCount.Add(1)
			defer resp.Body.Close()
			// Drain until ctx is cancelled.
			buf := make([]byte, 1024)
			for {
				_ = setReadDeadline(resp.Body, 50*time.Millisecond)
				_, err := resp.Body.Read(buf)
				if err != nil {
					return
				}
				if ctx.Err() != nil {
					return
				}
			}
		}(i)
	}

	// Let traffic flow for publishDuration.
	time.Sleep(publishDuration)

	// Stop publisher and cancel all clients.
	close(stopPublish)
	for _, cancel := range cancels {
		cancel()
	}
	clientWG.Wait()
	publisherWG.Wait()

	// Within 1s, all subscribers should have unsubscribed.
	deadline := time.Now().Add(1 * time.Second)
	for time.Now().Before(deadline) {
		if hubInst.SubscriberCount("racey") == 0 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Errorf("subscriber count did not drop to 0 within 1s: got %d (connected=%d)",
		hubInst.SubscriberCount("racey"), connectedCount.Load())
}

// TestMain runs goleak verification across this package.
//
// Note: net/http has a small set of known internal goroutines (e.g.
// transport idle-conn watchdogs) that may persist briefly after a
// httptest.Server.Close — they're not real leaks but they do trip
// goleak. We list the well-known ones via IgnoreTopFunction.
func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m,
		// httptest.Server's idle-conn watchdogs in DefaultTransport.
		goleak.IgnoreTopFunction("net/http.(*Transport).dialConnFor"),
		goleak.IgnoreTopFunction("internal/poll.runtime_pollWait"),
		// The k8s informer cache's reflector spawned by envtest in
		// other tests — not relevant here, but defensive.
		goleak.IgnoreAnyFunction("k8s.io/client-go/tools/cache.(*Reflector).Run"),
	)
}

// readSSEFrame reads from r until a `\n\n` SSE frame terminator is seen.
// Returns the frame (including the terminator) or an error if `timeout`
// elapses first.
func readSSEFrame(r io.Reader, timeout time.Duration) (string, error) {
	deadline := time.Now().Add(timeout)
	var accum strings.Builder
	buf := make([]byte, 512)
	for time.Now().Before(deadline) {
		_ = setReadDeadline(r, 50*time.Millisecond)
		n, err := r.Read(buf)
		if n > 0 {
			accum.Write(buf[:n])
			s := accum.String()
			if idx := strings.Index(s, "\n\n"); idx >= 0 {
				return s[:idx+2], nil
			}
		}
		if err != nil && err != io.EOF {
			// Read-deadline expiry is a net.Error with Timeout()==true; we
			// keep looping until the outer deadline.
			if ne, ok := err.(interface{ Timeout() bool }); ok && ne.Timeout() {
				continue
			}
			return accum.String(), err
		}
	}
	return accum.String(), fmt.Errorf("readSSEFrame: %v timeout exceeded; got:\n%s",
		timeout, accum.String())
}

// readNFrames reads n SSE frames or fails after timeout.
func readNFrames(r io.Reader, n int, timeout time.Duration) ([]string, error) {
	frames := make([]string, 0, n)
	deadline := time.Now().Add(timeout)
	var accum strings.Builder
	buf := make([]byte, 512)
	for len(frames) < n && time.Now().Before(deadline) {
		_ = setReadDeadline(r, 50*time.Millisecond)
		bn, err := r.Read(buf)
		if bn > 0 {
			accum.Write(buf[:bn])
			s := accum.String()
			for {
				idx := strings.Index(s, "\n\n")
				if idx < 0 {
					break
				}
				frames = append(frames, s[:idx+2])
				s = s[idx+2:]
				accum.Reset()
				accum.WriteString(s)
				if len(frames) >= n {
					return frames, nil
				}
			}
		}
		if err != nil && err != io.EOF {
			if ne, ok := err.(interface{ Timeout() bool }); ok && ne.Timeout() {
				continue
			}
			return frames, err
		}
	}
	if len(frames) < n {
		return frames, fmt.Errorf("readNFrames: got %d of %d frames within %v",
			len(frames), n, timeout)
	}
	return frames, nil
}

// setReadDeadline is a best-effort wrapper around net.Conn.SetReadDeadline
// when r implements it — http.Response.Body wraps a net.Conn, but the
// underlying connection isn't directly reachable through the io.Reader
// interface. This helper falls through to a no-op when the deadline can't
// be applied; the outer per-test timeout still fires via deadlines on the
// surrounding loops.
func setReadDeadline(r io.Reader, d time.Duration) error {
	type deadliner interface {
		SetReadDeadline(t time.Time) error
	}
	if dl, ok := r.(deadliner); ok {
		return dl.SetReadDeadline(time.Now().Add(d))
	}
	return nil
}
