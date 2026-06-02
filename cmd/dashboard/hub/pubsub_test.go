/*
Copyright 2026 TIDE Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

package hub

import (
	"encoding/json"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-logr/logr/testr"
	"go.uber.org/goleak"
)

// newTestHub returns a Hub backed by the test logger.
func newTestHub(t *testing.T) *Hub {
	t.Helper()
	return NewHub(testr.New(t))
}

// TestMain wraps every test with goroutine-leak detection (RESEARCH §777
// hub MUST own zero goroutines; SSE handlers in plan 04-11 own goroutines
// but those are not in scope here). goleak.VerifyTestMain runs after the
// test binary's tests complete.
//
// Per the test framework requirement in Task 2 Test 7, this is the safety
// net for the entire package — any test that leaks a goroutine fails the
// suite.
func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}

// TestSubscribeReturnsValidSubscriber covers behavior #1: Subscribe yields
// a non-nil Subscriber with a non-nil receive channel.
func TestSubscribeReturnsValidSubscriber(t *testing.T) {
	h := newTestHub(t)
	sub := h.Subscribe("my-project", 0)
	if sub == nil {
		t.Fatal("Subscribe returned nil")
	}
	if sub.Events() == nil {
		t.Fatal("Subscribe returned subscriber with nil Events channel")
	}
	if sub.Project() != "my-project" {
		t.Errorf("Subscriber.Project() = %q, want %q", sub.Project(), "my-project")
	}
	h.Unsubscribe(sub)
}

// TestPublishDeliversEvent covers behavior #2: a Published event reaches
// the Subscriber's channel within 100ms.
func TestPublishDeliversEvent(t *testing.T) {
	h := newTestHub(t)
	sub := h.Subscribe("my-project", 0)
	defer h.Unsubscribe(sub)

	want := Event{ID: 1, Type: "project.update", JSON: json.RawMessage(`{"ok":true}`)}
	h.Publish("my-project", want)

	select {
	case got := <-sub.Events():
		if got.Type != want.Type {
			t.Errorf("Events received Type=%q, want %q", got.Type, want.Type)
		}
		if string(got.JSON) != string(want.JSON) {
			t.Errorf("Events received JSON=%s, want %s", got.JSON, want.JSON)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Publish did not deliver event within 100ms")
	}
}

// TestFanOutToTwoSubscribers covers behavior #3: two subscribers on the
// same project both receive the published event.
func TestFanOutToTwoSubscribers(t *testing.T) {
	h := newTestHub(t)
	subA := h.Subscribe("my-project", 0)
	subB := h.Subscribe("my-project", 0)
	defer h.Unsubscribe(subA)
	defer h.Unsubscribe(subB)

	h.Publish("my-project", Event{Type: "x", JSON: json.RawMessage(`{}`)})

	for i, sub := range []*Subscriber{subA, subB} {
		select {
		case ev := <-sub.Events():
			if ev.Type != "x" {
				t.Errorf("sub %d: got Type=%q, want %q", i, ev.Type, "x")
			}
		case <-time.After(100 * time.Millisecond):
			t.Fatalf("sub %d: no event within 100ms (expected fan-out)", i)
		}
	}
}

// TestKeyedIsolation covers behavior #4: subscribers only receive events
// for their own project key.
func TestKeyedIsolation(t *testing.T) {
	h := newTestHub(t)
	subA := h.Subscribe("project-A", 0)
	subB := h.Subscribe("project-B", 0)
	defer h.Unsubscribe(subA)
	defer h.Unsubscribe(subB)

	h.Publish("project-A", Event{Type: "for-A", JSON: json.RawMessage(`{}`)})

	// subA must receive the event.
	select {
	case ev := <-subA.Events():
		if ev.Type != "for-A" {
			t.Errorf("subA: got %q, want %q", ev.Type, "for-A")
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("subA: event lost")
	}

	// subB must NOT receive the project-A event.
	select {
	case ev := <-subB.Events():
		t.Fatalf("subB: leaked event from project-A: %+v", ev)
	case <-time.After(50 * time.Millisecond):
		// expected — no event arrives
	}
}

// TestBufferOverflowDropsOldest covers behavior #5: when a slow consumer
// fills the 64-event buffer, the hub drops the oldest queued events to
// make room — preserving "latest state" semantics (RESEARCH §777-780).
//
// We publish 70 events. The buffer holds at most 64. Drain the channel
// fully and assert: (a) we receive exactly subscriberBufferSize events,
// (b) the LAST event we receive is event #70 (newest preserved), (c) we
// do NOT see events 1..6 (oldest dropped).
func TestBufferOverflowDropsOldest(t *testing.T) {
	h := newTestHub(t)
	sub := h.Subscribe("my-project", 0)
	defer h.Unsubscribe(sub)

	const numPublished = 70
	for i := 1; i <= numPublished; i++ {
		h.Publish("my-project", Event{Type: "tick", JSON: json.RawMessage([]byte{})})
	}

	// Drain.
	var seen []int64
	timeout := time.After(500 * time.Millisecond)
DRAIN:
	for {
		select {
		case ev, ok := <-sub.Events():
			if !ok {
				break DRAIN
			}
			seen = append(seen, ev.ID)
		case <-timeout:
			break DRAIN
		default:
			break DRAIN
		}
	}

	if len(seen) != subscriberBufferSize {
		t.Errorf("buffer overflow: drained %d events, want %d (subscriberBufferSize)",
			len(seen), subscriberBufferSize)
	}

	// Newest preserved: last drained event should be event #numPublished.
	if len(seen) > 0 && seen[len(seen)-1] != int64(numPublished) {
		t.Errorf("drop-oldest: last drained event ID=%d, want %d (newest preserved)",
			seen[len(seen)-1], numPublished)
	}

	// Oldest dropped: event #1 should NOT be in the drained set.
	for _, id := range seen {
		if id == 1 {
			t.Errorf("drop-oldest: event #1 (oldest) still present in drained set; expected to be dropped")
		}
	}
}

// TestUnsubscribeClosesChannel covers behavior #6: Unsubscribe removes
// the subscriber + closes Events; subsequent Publish doesn't leak or
// panic.
func TestUnsubscribeClosesChannel(t *testing.T) {
	h := newTestHub(t)
	sub := h.Subscribe("my-project", 0)

	h.Unsubscribe(sub)

	// Receive on closed channel returns zero value immediately + ok=false.
	select {
	case _, ok := <-sub.Events():
		if ok {
			t.Errorf("Events channel should be closed after Unsubscribe; got ok=true")
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Events channel not closed within 100ms of Unsubscribe")
	}

	// Subsequent Publish must not panic (hub's internal map has no
	// reference to the closed channel anymore).
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("Publish after Unsubscribe panicked: %v", r)
		}
	}()
	h.Publish("my-project", Event{Type: "post-unsub", JSON: json.RawMessage(`{}`)})
}

// TestConcurrentSubscribePublishUnsubscribe covers behavior #7: under
// concurrent calls + go test -race, no data races, no panics, no
// goroutine leaks (TestMain's goleak verifies the leak property).
func TestConcurrentSubscribePublishUnsubscribe(t *testing.T) {
	h := newTestHub(t)

	const workers = 8
	const iterationsPerWorker = 200

	var wg sync.WaitGroup
	var publishedCount atomic.Int64

	// Publishers.
	for w := range workers {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for range iterationsPerWorker {
				h.Publish("p", Event{Type: "t", JSON: json.RawMessage(`{}`)})
				publishedCount.Add(1)
			}
		}(w)
	}

	// Subscribers.
	for w := range workers {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for range iterationsPerWorker {
				sub := h.Subscribe("p", 0)
				// Drain a few events non-blockingly.
				for range 5 {
					select {
					case <-sub.Events():
					default:
					}
				}
				h.Unsubscribe(sub)
			}
		}(w)
	}

	wg.Wait()

	if publishedCount.Load() != int64(workers*iterationsPerWorker) {
		t.Errorf("publishedCount = %d, want %d", publishedCount.Load(), workers*iterationsPerWorker)
	}
}

// TestLastEventIDReplay covers behavior #8: Subscribe with a non-zero
// lastEventID replays buffered events with ID > lastEventID (and only
// those) into the new subscriber's channel.
func TestLastEventIDReplay(t *testing.T) {
	h := newTestHub(t)

	// Publish 5 events (IDs 1..5 stamped by the hub).
	for range 5 {
		h.Publish("my-project", Event{Type: "x", JSON: json.RawMessage(`{}`)})
	}

	// New subscriber asks for replay after event 3.
	sub := h.Subscribe("my-project", 3)
	defer h.Unsubscribe(sub)

	// Should receive events 4 and 5 only.
	var got []int64
	timeout := time.After(200 * time.Millisecond)
LOOP:
	for {
		select {
		case ev := <-sub.Events():
			got = append(got, ev.ID)
			if len(got) == 2 {
				break LOOP
			}
		case <-timeout:
			break LOOP
		}
	}

	if len(got) != 2 || got[0] != 4 || got[1] != 5 {
		t.Errorf("Last-Event-ID replay: got IDs %v, want [4 5]", got)
	}
}

// TestReplayBufferTruncation confirms the per-project replay buffer is
// capped at replayBufferSize (100). After publishing replayBufferSize+50
// events, a new subscriber with lastEventID=0 sees at most
// subscriberBufferSize (64) events — limited by the destination channel's
// drop-oldest, not by an unbounded replay scan.
func TestReplayBufferTruncation(t *testing.T) {
	h := newTestHub(t)

	// Publish more than the replay buffer holds.
	const overflow = 50
	for range replayBufferSize + overflow {
		h.Publish("my-project", Event{Type: "x", JSON: json.RawMessage(`{}`)})
	}

	sub := h.Subscribe("my-project", 0)
	defer h.Unsubscribe(sub)

	// Drain.
	var got []int64
DRAIN:
	for {
		select {
		case ev, ok := <-sub.Events():
			if !ok {
				break DRAIN
			}
			got = append(got, ev.ID)
		default:
			break DRAIN
		}
	}

	// Subscriber channel is bounded; drained set should not exceed it.
	if len(got) > subscriberBufferSize {
		t.Errorf("drained %d events; subscriber channel buffer is %d", len(got), subscriberBufferSize)
	}

	// The newest event (ID = replayBufferSize+overflow) should be in the
	// drained set OR be one of the dropped-oldest casualties.
	// Either way, no event should have ID <= overflow (those are older
	// than the truncation cut-off).
	for _, id := range got {
		if id <= int64(overflow) {
			t.Errorf("replay returned event ID=%d which should have been truncated (cap=%d)",
				id, replayBufferSize)
		}
	}
}

// TestUnsubscribeRemovesFromInternalMap is a white-box assertion against
// the hub's internal state: after Unsubscribe the per-project subscriber
// slice is either empty (and removed from h.subs) or doesn't contain the
// unsubscribed Subscriber pointer.
func TestUnsubscribeRemovesFromInternalMap(t *testing.T) {
	h := newTestHub(t)
	sub := h.Subscribe("my-project", 0)

	h.mu.Lock()
	if len(h.subs["my-project"]) != 1 {
		h.mu.Unlock()
		t.Fatalf("expected 1 subscriber, got %d", len(h.subs["my-project"]))
	}
	h.mu.Unlock()

	h.Unsubscribe(sub)

	h.mu.Lock()
	defer h.mu.Unlock()
	if _, ok := h.subs["my-project"]; ok {
		t.Errorf("expected 'my-project' to be removed from h.subs after last Unsubscribe; still present with %d subs",
			len(h.subs["my-project"]))
	}
}

// TestSubscribeWithCallerStampedID confirms Publish honors a caller-set
// non-zero Event.ID instead of auto-stamping. Used when the informer
// adapter (plan 04-11) wants to derive IDs from CRD resourceVersion.
func TestSubscribeWithCallerStampedID(t *testing.T) {
	h := newTestHub(t)
	sub := h.Subscribe("my-project", 0)
	defer h.Unsubscribe(sub)

	h.Publish("my-project", Event{ID: 42, Type: "x", JSON: json.RawMessage(`{}`)})

	select {
	case got := <-sub.Events():
		if got.ID != 42 {
			t.Errorf("got ID=%d, want 42 (caller-stamped)", got.ID)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("event not delivered")
	}

	// Next auto-stamp should not collide with the caller ID.
	h.Publish("my-project", Event{Type: "x", JSON: json.RawMessage(`{}`)})
	select {
	case got := <-sub.Events():
		if got.ID <= 42 {
			t.Errorf("auto-stamped ID=%d collided with caller-stamped 42 (expected > 42)", got.ID)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("second event not delivered")
	}
}
