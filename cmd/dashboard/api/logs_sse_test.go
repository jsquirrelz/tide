/*
Copyright 2026 TIDE Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
*/

// Plan 04-11 Task 2 — logs_sse_test.go: tests for LogsHandler (DASH-04).
// Exercises the Pitfall 22 mitigations (defer stream.Close, ctx.Done
// watcher, 5-min idle timeout) and the Pitfall 23 nginx-buffering
// header set.
//
// Like cmd/tide/tail_test.go, we inject the streamer via a function
// variable seam (`logStreamOpener`) so the tests don't need a live
// apiserver. The actual pods/log Stream() call is exercised in plan
// 04-14's kind harness.

package api

import (
	"bytes"
	"context"
	"errors"
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
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	fakeclientset "k8s.io/client-go/kubernetes/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	tidev1alpha1 "github.com/jsquirrelz/tide/api/v1alpha1"
)

// fakeStream is a controllable io.ReadCloser backed by a bytes channel.
// Tests push bytes via Push() and close via Close(). Used by streamStub
// to deliver lines into the handler.
type fakeStream struct {
	buf    *bytes.Buffer
	closed atomic.Bool
	mu     sync.Mutex
	cond   *sync.Cond
	eof    bool
}

func newFakeStream() *fakeStream {
	fs := &fakeStream{buf: &bytes.Buffer{}}
	fs.cond = sync.NewCond(&fs.mu)
	return fs
}

func (f *fakeStream) Write(p []byte) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	n, err := f.buf.Write(p)
	f.cond.Broadcast()
	return n, err
}

func (f *fakeStream) Read(p []byte) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for f.buf.Len() == 0 && !f.eof && !f.closed.Load() {
		f.cond.Wait()
	}
	if f.closed.Load() && f.buf.Len() == 0 {
		return 0, io.EOF
	}
	if f.eof && f.buf.Len() == 0 {
		return 0, io.EOF
	}
	return f.buf.Read(p)
}

func (f *fakeStream) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closed.Store(true)
	f.cond.Broadcast()
	return nil
}

// newLogsRouter returns a chi router mounted with a LogsHandler that uses
// a deterministic stub streamer. The Pod has a single container named
// "subagent". Behaviour:
//
//   - taskExists controls whether the Task lookup returns a 404 or a Task
//     with a Pod-bearing UID.
//   - openStream returns the fakeStream + nil to simulate a live stream,
//     or returns nil + an error to simulate apierrors.IsNotFound (pod gone).
func newLogsRouter(t *testing.T, taskExists bool, idleTimeout time.Duration, openStream func(opts *corev1.PodLogOptions) (io.ReadCloser, error)) (*LogsHandler, *http.ServeMux, http.Handler) {
	t.Helper()
	scheme := testInformerScheme(t)
	builder := fake.NewClientBuilder().WithScheme(scheme)
	if taskExists {
		tk := &tidev1alpha1.Task{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "my-task",
				Namespace: "default",
				UID:       types.UID("task-uid-A"),
			},
			Spec: tidev1alpha1.TaskSpec{
				PlanRef:             "pl1",
				FilesTouched:        []string{"a.go"},
				DeclaredOutputPaths: []string{"/workspace/a"},
			},
			Status: tidev1alpha1.TaskStatus{Phase: "Running"},
		}
		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "my-task-pod",
				Namespace: "default",
				Labels:    map[string]string{"tideproject.k8s/task-uid": "task-uid-A"},
			},
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{
					{Name: "subagent"},
				},
			},
			Status: corev1.PodStatus{Phase: corev1.PodRunning},
		}
		builder = builder.WithObjects(tk, pod)
	}
	c := builder.Build()
	cs := fakeclientset.NewSimpleClientset()

	opts := []LogsHandlerOption{}
	if idleTimeout > 0 {
		opts = append(opts, WithIdleTimeout(idleTimeout))
	}
	if openStream != nil {
		opts = append(opts, WithStreamOpener(func(_ context.Context, _ kubernetes.Interface, _, _ string, podOpts *corev1.PodLogOptions) (io.ReadCloser, error) {
			return openStream(podOpts)
		}))
	}
	lh := NewLogsHandler(c, cs, testr.New(t), opts...)

	r := chi.NewRouter()
	r.Route("/api/v1", func(r chi.Router) {
		r.Get("/tasks/{name}/log", lh.ServeHTTP)
	})

	mux := http.NewServeMux()
	mux.Handle("/", r)
	return lh, mux, r
}

// TestLogsHandlerHeaders covers behavior #1: 200 + the Pitfall-23 header
// set, identical to events_sse.
func TestLogsHandlerHeaders(t *testing.T) {
	fs := newFakeStream()
	_, _, router := newLogsRouter(t, true, 0, func(_ *corev1.PodLogOptions) (io.ReadCloser, error) {
		return fs, nil
	})
	srv := httptest.NewServer(router)
	defer srv.Close()
	defer fs.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet,
		srv.URL+"/api/v1/tasks/my-task/log?stream=sse", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET: %v", err)
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

// TestLogsHandlerDeliversLines covers behavior #2: when the pods/log stream
// produces "line one\nline two\n", the SSE recorder receives two
// `data: line one\n\n` and `data: line two\n\n` frames.
func TestLogsHandlerDeliversLines(t *testing.T) {
	fs := newFakeStream()
	_, _, router := newLogsRouter(t, true, 0, func(_ *corev1.PodLogOptions) (io.ReadCloser, error) {
		return fs, nil
	})
	srv := httptest.NewServer(router)
	defer srv.Close()
	defer fs.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet,
		srv.URL+"/api/v1/tasks/my-task/log", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	// Push two lines into the stream.
	_, _ = fs.Write([]byte("line one\nline two\n"))

	got, err := readNFrames(resp.Body, 2, 500*time.Millisecond)
	if err != nil {
		t.Fatalf("readNFrames: %v", err)
	}

	if !strings.Contains(got[0], "data: line one") {
		t.Errorf("frame[0] = %q, want 'data: line one'", got[0])
	}
	if !strings.Contains(got[1], "data: line two") {
		t.Errorf("frame[1] = %q, want 'data: line two'", got[1])
	}
}

// TestLogsHandlerIdleTimeout covers behavior #3 + #4: with idleTimeout=100ms
// and no lines after the first burst, the handler emits `event:
// idle-timeout` and returns. Stream is closed via defer.
func TestLogsHandlerIdleTimeout(t *testing.T) {
	fs := newFakeStream()
	streamClosed := atomic.Bool{}
	wrap := &closeTrackingStream{rc: fs, onClose: func() { streamClosed.Store(true) }}

	_, _, router := newLogsRouter(t, true, 100*time.Millisecond, func(_ *corev1.PodLogOptions) (io.ReadCloser, error) {
		return wrap, nil
	})
	srv := httptest.NewServer(router)
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet,
		srv.URL+"/api/v1/tasks/my-task/log", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	// Don't push any lines — let the idle timer fire.
	body, err := readSSEFrame(resp.Body, 500*time.Millisecond)
	if err != nil {
		t.Fatalf("readSSEFrame: %v", err)
	}
	if !strings.Contains(body, "event: idle-timeout") {
		t.Errorf("expected idle-timeout event in body:\n%s", body)
	}

	// Within 300ms the stream Close should have been called.
	deadline := time.Now().Add(300 * time.Millisecond)
	for time.Now().Before(deadline) {
		if streamClosed.Load() {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !streamClosed.Load() {
		t.Error("stream Close not called within 300ms of idle-timeout")
	}
}

// TestLogsHandlerClientDisconnectCleanup covers behavior #5: cancelling the
// request context closes the stream within 1s.
func TestLogsHandlerClientDisconnectCleanup(t *testing.T) {
	fs := newFakeStream()
	streamClosed := atomic.Bool{}
	wrap := &closeTrackingStream{rc: fs, onClose: func() { streamClosed.Store(true) }}

	_, _, router := newLogsRouter(t, true, 5*time.Second, func(_ *corev1.PodLogOptions) (io.ReadCloser, error) {
		return wrap, nil
	})
	srv := httptest.NewServer(router)
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet,
		srv.URL+"/api/v1/tasks/my-task/log", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}

	// Wait for the stream to be opened by sending a line and waiting for
	// the client to see the frame.
	_, _ = fs.Write([]byte("hello\n"))
	_, _ = readSSEFrame(resp.Body, 500*time.Millisecond)

	cancel()
	_ = resp.Body.Close()

	// Within 1s, the stream Close should have been called.
	deadline := time.Now().Add(1 * time.Second)
	for time.Now().Before(deadline) {
		if streamClosed.Load() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Error("stream Close not called within 1s of client cancel")
}

// TestLogsHandlerPodGone covers behavior #6: when the stream opener
// returns apierrors.IsNotFound, the handler emits a `pod-gone` event and
// returns gracefully.
func TestLogsHandlerPodGone(t *testing.T) {
	_, _, router := newLogsRouter(t, true, 0, func(_ *corev1.PodLogOptions) (io.ReadCloser, error) {
		return nil, apierrors.NewNotFound(schema.GroupResource{Group: "", Resource: "pods"}, "my-task-pod")
	})
	srv := httptest.NewServer(router)
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet,
		srv.URL+"/api/v1/tasks/my-task/log", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	body, err := readSSEFrame(resp.Body, 500*time.Millisecond)
	if err != nil {
		t.Fatalf("readSSEFrame: %v", err)
	}
	if !strings.Contains(body, "event: pod-gone") {
		t.Errorf("expected pod-gone event:\n%s", body)
	}
}

// TestLogsHandlerContainerQueryParam covers behavior #7: ?container=foo
// is threaded into PodLogOptions.Container.
func TestLogsHandlerContainerQueryParam(t *testing.T) {
	var sawContainer string
	var mu sync.Mutex
	fs := newFakeStream()
	defer fs.Close()

	_, _, router := newLogsRouter(t, true, 5*time.Second, func(opts *corev1.PodLogOptions) (io.ReadCloser, error) {
		mu.Lock()
		sawContainer = opts.Container
		mu.Unlock()
		return fs, nil
	})
	srv := httptest.NewServer(router)
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet,
		srv.URL+"/api/v1/tasks/my-task/log?container=alt-c", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	// Push something so the read loop spins and the opener fires.
	_, _ = fs.Write([]byte("ping\n"))
	_, _ = readSSEFrame(resp.Body, 500*time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	if sawContainer != "alt-c" {
		t.Errorf("Container query param: got %q, want %q", sawContainer, "alt-c")
	}
}

// TestLogsHandlerContainerDefault covers behavior #7b: without ?container=,
// the default container is "subagent" (matches the Phase 1/2 stub
// container name).
func TestLogsHandlerContainerDefault(t *testing.T) {
	var sawContainer string
	var mu sync.Mutex
	fs := newFakeStream()
	defer fs.Close()

	_, _, router := newLogsRouter(t, true, 5*time.Second, func(opts *corev1.PodLogOptions) (io.ReadCloser, error) {
		mu.Lock()
		sawContainer = opts.Container
		mu.Unlock()
		return fs, nil
	})
	srv := httptest.NewServer(router)
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet,
		srv.URL+"/api/v1/tasks/my-task/log", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	_, _ = fs.Write([]byte("ping\n"))
	_, _ = readSSEFrame(resp.Body, 500*time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	if sawContainer != "subagent" {
		t.Errorf("default container: got %q, want %q", sawContainer, "subagent")
	}
}

// TestLogsHandlerTaskNotFound covers the missing-Task path: the handler
// emits a `pod-gone` event and returns.
func TestLogsHandlerTaskNotFound(t *testing.T) {
	_, _, router := newLogsRouter(t, false, 0, nil)
	srv := httptest.NewServer(router)
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet,
		srv.URL+"/api/v1/tasks/missing/log", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	body, err := readSSEFrame(resp.Body, 500*time.Millisecond)
	if err != nil {
		t.Fatalf("readSSEFrame: %v", err)
	}
	if !strings.Contains(body, "event: pod-gone") {
		t.Errorf("expected pod-gone event when Task missing:\n%s", body)
	}
}

// closeTrackingStream wraps an io.ReadCloser with an onClose callback.
// Lets tests assert that defer stream.Close() actually ran.
type closeTrackingStream struct {
	rc      io.ReadCloser
	onClose func()
	closed  atomic.Bool
}

func (c *closeTrackingStream) Read(p []byte) (int, error) {
	return c.rc.Read(p)
}

func (c *closeTrackingStream) Close() error {
	if c.closed.CompareAndSwap(false, true) && c.onClose != nil {
		c.onClose()
	}
	return c.rc.Close()
}

// Silence unused-import linter for errors when we reference it in
// the conditional pod-gone path.
var _ = errors.New
