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

// logs_sse.go — DASH-04: GET /api/v1/tasks/{name}/log serves a per-task
// pod-log Server-Sent Events stream. The handler opens a pods/log
// Follow:true subresource stream via client-go and translates each line
// into an SSE `data: <line>\n\n` frame.
//
// Lifecycle (Pitfall 22 mitigation):
//
//   1. Resolve Task → Pod via label selector tideproject.k8s/task-uid.
//   2. defer stream.Close() — runs on EVERY exit (panic, ctx cancel, EOF).
//   3. Spawn a reader goroutine that pushes scanner.Text() into linesChan.
//      The goroutine exits when scanner.Err returns (closed stream or
//      ctx-driven Close).
//   4. Main select loop watches { ctx.Done(), idleTimer.C, linesChan }:
//      - ctx.Done    → return (client disconnect); deferred Close cleans up.
//      - idleTimer.C → emit "event: idle-timeout" and return.
//      - linesChan   → emit "data: <line>\n\n" and reset idleTimer.
//      - linesChan closed → emit "event: pod-gone" and return.
//
// Pitfall 23 mitigation: same X-Accel-Buffering / Cache-Control header set
// as events_sse.go. Heartbeats are NOT necessary on this endpoint — log
// streams are inherently chatty; the idle timeout (5min default) handles
// the proxy-timeout edge.

package api

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/client"

	tidev1alpha1 "github.com/jsquirrelz/tide/api/v1alpha1"
)

// defaultIdleTimeout bounds an attached log stream. 5 minutes is the
// Pitfall 22 documented value — operators expect kubectl-logs-style
// follow but the dashboard isn't a long-form terminal session. Override
// via WithIdleTimeout for tests (100ms).
const defaultIdleTimeout = 5 * time.Minute

// defaultLogContainer is the container name used when the request omits
// the `?container=` query param. "subagent" is the canonical executor
// container (Phase 1/2 convention); matches cmd/tide/tail.go's heuristic.
const defaultLogContainer = "subagent"

// taskUIDLabel is the canonical Pod-label key reconciler/task_controller.go
// stamps on every Job's pods. Mirrors cmd/tide/tail.go usage.
const taskUIDLabel = "tideproject.k8s/task-uid"

// streamOpener opens a pods/log stream for the given pod/container. Factored
// out as a function-pointer seam so tests can inject a deterministic
// io.ReadCloser without touching the apiserver — mirrors
// cmd/tide/tail.go's tailStreamer pattern.
type streamOpener func(ctx context.Context, cs kubernetes.Interface, ns, podName string, opts *corev1.PodLogOptions) (io.ReadCloser, error)

// defaultStreamOpener is the production opener — it calls the real
// pods/log subresource via client-go.
func defaultStreamOpener(ctx context.Context, cs kubernetes.Interface, ns, podName string, opts *corev1.PodLogOptions) (io.ReadCloser, error) {
	req := cs.CoreV1().Pods(ns).GetLogs(podName, opts)
	return req.Stream(ctx)
}

// LogsHandler implements GET /api/v1/tasks/{name}/log. Each invocation
// opens a single pods/log Follow stream and translates lines into SSE.
type LogsHandler struct {
	Client    client.Client
	Clientset kubernetes.Interface
	Log       logr.Logger

	idleTimeout      time.Duration
	defaultContainer string
	openStream       streamOpener
}

// LogsHandlerOption configures a LogsHandler at construction time.
type LogsHandlerOption func(*LogsHandler)

// WithIdleTimeout overrides the default 5min idle-close. Tests use 100ms.
func WithIdleTimeout(d time.Duration) LogsHandlerOption {
	return func(h *LogsHandler) { h.idleTimeout = d }
}

// WithDefaultContainer overrides the default-container heuristic
// ("subagent") for environments where the executor container has a
// different name.
func WithDefaultContainer(name string) LogsHandlerOption {
	return func(h *LogsHandler) { h.defaultContainer = name }
}

// WithStreamOpener overrides the pods/log streamer — used by tests to
// inject a deterministic io.ReadCloser without an apiserver round-trip.
func WithStreamOpener(fn streamOpener) LogsHandlerOption {
	return func(h *LogsHandler) { h.openStream = fn }
}

// NewLogsHandler constructs a LogsHandler bound to the given clients.
func NewLogsHandler(c client.Client, cs kubernetes.Interface, log logr.Logger, opts ...LogsHandlerOption) *LogsHandler {
	lh := &LogsHandler{
		Client:           c,
		Clientset:        cs,
		Log:              log,
		idleTimeout:      defaultIdleTimeout,
		defaultContainer: defaultLogContainer,
		openStream:       defaultStreamOpener,
	}
	for _, opt := range opts {
		opt(lh)
	}
	return lh
}

// ServeHTTP serves the pod-log SSE stream. Sequence:
//
//  1. Header set (Pitfall 23) + 200 OK + flush.
//  2. Resolve Task → namespace + UID → matching Pod.
//  3. Open pods/log stream; on 404 (pod gone) emit pod-gone event and return.
//  4. defer stream.Close().
//  5. Spawn reader goroutine: bufio.Scanner pushes lines into linesChan.
//  6. select loop until ctx.Done OR idleTimer.C OR linesChan closed.
func (h *LogsHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported by this server", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	taskName := chi.URLParam(r, "name")
	if taskName == "" {
		writeSSEEvent(w, flusher, "error", `{"error":"missing task name"}`)
		return
	}

	ctx := r.Context()
	namespace := r.URL.Query().Get("namespace")
	if namespace == "" {
		namespace = "default"
	}
	containerName := r.URL.Query().Get("container")
	if containerName == "" {
		containerName = h.defaultContainer
	}

	// Resolve Task → Pod via label selector. Same heuristic as
	// cmd/tide/tail.go: list Pods with tideproject.k8s/task-uid=<task.UID>
	// and pick the first one in Running/Pending phase.
	podName, err := h.resolvePodName(ctx, namespace, taskName)
	if err != nil {
		h.Log.V(1).Info("task or pod lookup failed; emitting pod-gone",
			"task", taskName, "err", err)
		writeSSEEvent(w, flusher, "pod-gone", `{}`)
		return
	}
	if podName == "" {
		writeSSEEvent(w, flusher, "pod-gone", `{}`)
		return
	}

	// Open the log stream. apierrors.IsNotFound = pod was deleted between
	// our resolvePodName lookup and the stream open — surface gracefully.
	stream, err := h.openStream(ctx, h.Clientset, namespace, podName, &corev1.PodLogOptions{
		Follow:     true,
		Container:  containerName,
		TailLines:  new(int64(100)),
		Timestamps: true,
	})
	if err != nil {
		if apierrors.IsNotFound(err) {
			writeSSEEvent(w, flusher, "pod-gone", `{}`)
			return
		}
		h.Log.Error(err, "open pods/log stream failed",
			"pod", podName, "task", taskName)
		writeSSEEvent(w, flusher, "error", fmt.Sprintf(`{"error":%q}`, err.Error()))
		return
	}
	defer func() { _ = stream.Close() }() // read-only log stream; close error is not actionable

	// Spawn the reader goroutine. Scanner uses default 64KB buffer — pod
	// logs typically have short lines (CR/LF-separated) but we provide
	// a healthy buffer for stack traces.
	linesChan := make(chan string, 32)
	go func() {
		defer close(linesChan)
		scanner := bufio.NewScanner(stream)
		const maxLineSize = 1 << 20 // 1MB cap per line
		scanner.Buffer(make([]byte, 0, 64*1024), maxLineSize)
		for scanner.Scan() {
			select {
			case linesChan <- scanner.Text():
			case <-ctx.Done():
				return
			}
		}
	}()

	idleTimer := time.NewTimer(h.idleTimeout)
	defer idleTimer.Stop()

	for {
		select {
		case <-ctx.Done():
			// Client disconnected. defer stream.Close() runs, which also
			// causes scanner.Scan() in the reader goroutine to fail and
			// the goroutine to exit (close(linesChan) on defer there).
			return

		case <-idleTimer.C:
			writeSSEEvent(w, flusher, "idle-timeout", `{}`)
			return

		case line, ok := <-linesChan:
			if !ok {
				// Stream EOF — pod terminated or scanner errored out.
				// Surface as pod-gone per the DASH-04 contract.
				writeSSEEvent(w, flusher, "pod-gone", `{}`)
				return
			}
			if _, werr := fmt.Fprintf(w, "data: %s\n\n", line); werr != nil {
				// Client gone (TCP RST). Bail; defer runs Close.
				return
			}
			flusher.Flush()

			// Reset idle timer. Drain the channel first per the Go
			// documented Timer.Reset pattern: stop, drain, reset.
			if !idleTimer.Stop() {
				select {
				case <-idleTimer.C:
				default:
				}
			}
			idleTimer.Reset(h.idleTimeout)
		}
	}
}

// resolvePodName looks up the Task by name + lists pods with the
// task-uid label, returning the first Running/Pending pod's name. Returns
// an empty string + nil error when no matching pod exists (caller emits
// pod-gone). Surfaces other errors directly.
func (h *LogsHandler) resolvePodName(ctx context.Context, ns, taskName string) (string, error) {
	var task tidev1alpha1.Task
	if err := h.Client.Get(ctx, types.NamespacedName{Namespace: ns, Name: taskName}, &task); err != nil {
		if apierrors.IsNotFound(err) {
			return "", nil
		}
		return "", err
	}

	var pods corev1.PodList
	if err := h.Client.List(ctx, &pods,
		client.InNamespace(ns),
		client.MatchingLabels{taskUIDLabel: string(task.UID)},
	); err != nil {
		return "", err
	}
	for i := range pods.Items {
		p := &pods.Items[i]
		switch p.Status.Phase {
		case corev1.PodRunning, corev1.PodPending:
			return p.Name, nil
		}
	}
	return "", nil
}

// writeSSEEvent emits a single SSE frame with the given event type +
// inline JSON payload. Used for the terminal control frames (idle-timeout,
// pod-gone, error) that don't carry a structured payload.
func writeSSEEvent(w http.ResponseWriter, flusher http.Flusher, eventType, jsonBody string) {
	fmt.Fprintf(w, "event: %s\ndata: %s\n\n", eventType, jsonBody)
	flusher.Flush()
}
