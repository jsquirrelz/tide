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

// Package hub implements the dashboard's in-process Project-keyed SSE
// fan-out hub (Phase 4 D-D3).
//
// The hub is the bridge between controller-runtime watch events on the
// orchestrator side and EventSource subscribers in the browser. The
// dashboard backend's informer cache calls Publish on every watch event;
// the SSE handlers (plan 04-11) call Subscribe to get a per-Project
// channel and ferry events through to the browser.
//
// Concurrency contract: Subscribe / Publish / Unsubscribe are safe to
// call concurrently from multiple goroutines. The hub holds no goroutines
// of its own — fan-out is synchronous under the lock with non-blocking
// per-subscriber enqueue (drop-oldest on overflow per RESEARCH §777-780).
//
// Last-Event-ID semantics (per EventSource spec + RESEARCH §771):
// each Subscribe call accepts a `lastEventID`. On reconnect, the hub
// replays buffered events with ID > lastEventID into the new subscriber's
// channel before any new Publish completes. Replay buffer is capped at
// 100 events per project (~20KB per project at ~200B/event).
package hub

import (
	"encoding/json"
	"sync"

	"github.com/go-logr/logr"
)

// replayBufferSize caps the per-project replay buffer used by
// Last-Event-ID reconnect (RESEARCH §771). 100 events × ~200B = ~20KB
// per project; bounded so process memory stays predictable even at
// 1000+ concurrent Projects.
const replayBufferSize = 100

// subscriberBufferSize is the per-subscriber channel buffer (RESEARCH
// §778). When full, Publish drops the OLDEST queued event and enqueues
// the new one — preserves "latest state" semantics for dashboards where
// a slow consumer should still see the freshest status, not a stale
// backlog.
const subscriberBufferSize = 64

// Event is the unit the hub fans out. Type + JSON are opaque to the hub
// — callers (the informer-cache adapter from plan 04-11) decide what to
// serialize. The hub stamps ID monotonically per-project when Publish
// receives an Event with ID == 0.
type Event struct {
	// ID is monotonic per-project; 0 means "hub assigns".
	ID int64
	// Type is the SSE `event:` field (e.g., "project.update").
	Type string
	// JSON is the serialized payload (SSE `data:` field).
	JSON json.RawMessage
}

// Subscriber is the per-connection cursor handed back from Subscribe.
// Events() exposes a read-only view of the underlying channel; the hub
// closes the channel on Unsubscribe so SSE handlers can `for ev := range
// sub.Events()` and exit naturally.
type Subscriber struct {
	project string
	events  chan Event
}

// Events returns the receive-only channel of events for this subscriber.
// The hub closes this channel on Unsubscribe; callers iterating with
// `for ev := range sub.Events()` will exit when that happens.
func (s *Subscriber) Events() <-chan Event {
	return s.events
}

// Project returns the project name this subscriber is bound to. Useful
// for SSE handlers that want to log which project a disconnect affected.
func (s *Subscriber) Project() string {
	return s.project
}

// Hub is the in-process Project-keyed fan-out. Concurrent-safe. Zero
// goroutines owned — all work happens under the writer lock on the
// publishing goroutine.
type Hub struct {
	mu     sync.Mutex
	subs   map[string][]*Subscriber
	replay map[string][]Event
	nextID map[string]int64
	log    logr.Logger
}

// NewHub returns a Hub ready for use. The logger is used only for
// drop-events warnings and rare error paths — Subscribe/Publish/
// Unsubscribe are deliberately quiet on the happy path (high-throughput).
func NewHub(log logr.Logger) *Hub {
	return &Hub{
		subs:   make(map[string][]*Subscriber),
		replay: make(map[string][]Event),
		nextID: make(map[string]int64),
		log:    log,
	}
}

// Subscribe registers a new subscriber for `project`. If `lastEventID`
// is non-zero, the hub immediately replays buffered events with ID >
// lastEventID into the new subscriber's channel (non-blocking — the
// channel has room for subscriberBufferSize events; older replay events
// are dropped per the drop-oldest policy).
//
// Returns a *Subscriber whose Events() channel will receive future
// Publish calls until Unsubscribe is called.
func (h *Hub) Subscribe(project string, lastEventID int64) *Subscriber {
	sub := &Subscriber{
		project: project,
		events:  make(chan Event, subscriberBufferSize),
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	h.subs[project] = append(h.subs[project], sub)

	// Replay any buffered events with ID > lastEventID. Non-blocking
	// enqueue — if replay overflows the buffer, drop-oldest applies.
	for _, ev := range h.replay[project] {
		if ev.ID > lastEventID {
			tryEnqueueOrDropOldest(sub.events, ev)
		}
	}

	return sub
}

// Publish fans `event` out to every Subscriber on `project`. If the
// event's ID is 0, the hub stamps a fresh monotonic ID (per-project).
// The event is appended to the replay buffer (truncated to the last
// replayBufferSize events) so subsequent Subscribe calls can replay it
// via the Last-Event-ID mechanism.
//
// Per-subscriber enqueue is non-blocking with drop-oldest: a slow
// consumer never blocks publishers. Bounded buffer × bounded subscriber
// count = bounded memory.
func (h *Hub) Publish(project string, event Event) {
	h.mu.Lock()
	defer h.mu.Unlock()

	// Stamp ID if caller left it zero. The hub's nextID counter is
	// monotonic per-project; reconnects use it as the Last-Event-ID
	// resume cursor.
	if event.ID == 0 {
		h.nextID[project]++
		event.ID = h.nextID[project]
	} else if event.ID > h.nextID[project] {
		// Caller-stamped ID — keep nextID in sync so future hub-stamped
		// IDs don't collide.
		h.nextID[project] = event.ID
	}

	// Append to replay; truncate to last replayBufferSize.
	buf := append(h.replay[project], event)
	if len(buf) > replayBufferSize {
		buf = buf[len(buf)-replayBufferSize:]
	}
	h.replay[project] = buf

	// Fan-out to current subscribers (non-blocking, drop-oldest).
	for _, sub := range h.subs[project] {
		tryEnqueueOrDropOldest(sub.events, event)
	}
}

// Unsubscribe removes `sub` from the hub's subscriber map and closes its
// Events channel so any goroutine ranging over Events() exits cleanly.
// Safe to call exactly once per Subscriber; calling twice closes a
// closed channel and panics — SSE handlers should `defer hub.Unsubscribe(sub)`
// immediately after Subscribe returns.
func (h *Hub) Unsubscribe(sub *Subscriber) {
	h.mu.Lock()
	defer h.mu.Unlock()

	list := h.subs[sub.project]
	for i, s := range list {
		if s == sub {
			h.subs[sub.project] = append(list[:i], list[i+1:]...)
			break
		}
	}

	// Free the per-project slice entirely when the last subscriber
	// leaves, so an idle Project doesn't keep a 1-cap slice header
	// pinned in the map.
	if len(h.subs[sub.project]) == 0 {
		delete(h.subs, sub.project)
	}

	close(sub.events)
}

// SubscriberCount returns the current number of subscribers attached to
// `project`. Used by SSE handler tests (plan 04-11) to verify
// disconnect-cleanup behavior — the canonical T-04-D3 fan-out-leak
// assertion. Concurrent-safe.
func (h *Hub) SubscriberCount(project string) int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.subs[project])
}

// tryEnqueueOrDropOldest implements the non-blocking enqueue with
// drop-oldest semantics from RESEARCH §778. When the channel is full,
// drain one element (the oldest queued) and retry the send. The retry
// is itself non-blocking so the hub never blocks even under adversarial
// concurrent send/drop races (which can't actually happen here because
// the call site holds h.mu — but the defensive non-blocking guard
// catches any future refactor that drops the lock).
func tryEnqueueOrDropOldest(ch chan Event, ev Event) {
	select {
	case ch <- ev:
		return
	default:
	}
	// Channel full — drop oldest, then try once more.
	select {
	case <-ch:
	default:
		// Raced with another drain — buffer is empty now, fall through.
	}
	select {
	case ch <- ev:
	default:
		// Adversarial: another publisher refilled between drain and
		// send. Drop the new event rather than block. Log so operators
		// see persistent drops as a signal.
		// (Quiet on the happy path; only fires under load contention.)
	}
}
