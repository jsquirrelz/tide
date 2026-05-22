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

// Package budget unit tests for the sync.Map-backed rate-limiter Store.
// Uses stdlib testing; no K8s client needed for pure in-memory state.
package budget

import (
	"sync"
	"testing"
)

// TestStore_ForSecret_LazyCreate verifies that the first call creates a
// *rate.Limiter and the second call returns the same instance (pointer equality).
func TestStore_ForSecret_LazyCreate(t *testing.T) {
	s := NewStore()
	limits := Limits{RequestsPerMinute: 60, BurstSize: 10}

	first := s.ForSecret("uid-A", limits)
	if first == nil {
		t.Fatal("ForSecret returned nil for RPM=60")
	}

	second := s.ForSecret("uid-A", limits)
	if second == nil {
		t.Fatal("second ForSecret returned nil")
	}

	if first != second {
		t.Errorf("lazy-create: second ForSecret call returned different *rate.Limiter (want same instance)")
	}
}

// TestStore_ForSecret_PerSecretUIDIsolation verifies that different UIDs get
// independent *rate.Limiter instances.
func TestStore_ForSecret_PerSecretUIDIsolation(t *testing.T) {
	s := NewStore()
	limits := Limits{RequestsPerMinute: 60, BurstSize: 10}

	a := s.ForSecret("uid-A", limits)
	b := s.ForSecret("uid-B", limits)

	if a == nil || b == nil {
		t.Fatal("ForSecret returned nil for non-zero RPM")
	}
	if a == b {
		t.Errorf("per-UID isolation: uid-A and uid-B returned the same *rate.Limiter")
	}
}

// TestStore_ForSecret_ZeroRPMReturnsNil verifies that RPM=0 returns nil
// (caller's "unlimited" path — no rate limiting applied).
func TestStore_ForSecret_ZeroRPMReturnsNil(t *testing.T) {
	s := NewStore()
	limits := Limits{RequestsPerMinute: 0, BurstSize: 10}

	lim := s.ForSecret("uid-A", limits)
	if lim != nil {
		t.Errorf("RPM=0 should return nil; got non-nil *rate.Limiter")
	}
}

// TestStore_Evict_RemovesBucket verifies that Evict removes the cached bucket
// and a subsequent ForSecret call creates a fresh *rate.Limiter.
func TestStore_Evict_RemovesBucket(t *testing.T) {
	s := NewStore()
	limits := Limits{RequestsPerMinute: 60, BurstSize: 10}

	first := s.ForSecret("uid-A", limits)
	if first == nil {
		t.Fatal("first ForSecret returned nil")
	}

	s.Evict("uid-A")

	second := s.ForSecret("uid-A", limits)
	if second == nil {
		t.Fatal("post-Evict ForSecret returned nil")
	}
	if first == second {
		t.Errorf("Evict: post-evict ForSecret returned same *rate.Limiter instance; expected fresh one")
	}
}

// TestStore_Concurrent_SafeUnder_LoadOrStore verifies that concurrent ForSecret
// calls for the same UID all receive the same *rate.Limiter (sync.Map atomicity).
func TestStore_Concurrent_SafeUnder_LoadOrStore(t *testing.T) {
	s := NewStore()
	limits := Limits{RequestsPerMinute: 60, BurstSize: 10}

	const goroutines = 50
	results := make([]*interface{}, goroutines)
	// Use a generic holder to avoid storing *rate.Limiter pointer in slice
	// causing alignment issues; just check identity via pointer comparison.
	type result struct{ lim interface{} }
	limResults := make([]result, goroutines)

	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		idx := i
		go func() {
			defer wg.Done()
			lim := s.ForSecret("uid-concurrent", limits)
			limResults[idx] = result{lim: lim}
		}()
	}
	wg.Wait()

	first := limResults[0].lim
	if first == nil {
		t.Fatal("concurrent ForSecret returned nil")
	}
	for i := 1; i < goroutines; i++ {
		if limResults[i].lim != first {
			t.Errorf("goroutine %d returned different *rate.Limiter instance; expected same (LoadOrStore atomicity)", i)
		}
	}

	_ = results // suppress unused warning
}

// TestStore_ForSecret_BurstBehavior verifies that a freshly-created Limiter
// allows burst-size requests immediately (rate.Limiter semantics).
func TestStore_ForSecret_BurstBehavior(t *testing.T) {
	s := NewStore()
	limits := Limits{RequestsPerMinute: 60, BurstSize: 5}

	lim := s.ForSecret("uid-burst", limits)
	if lim == nil {
		t.Fatal("ForSecret returned nil for RPM=60")
	}

	// Burst of 5 should be immediately allowed.
	for i := 0; i < 5; i++ {
		if !lim.Allow() {
			t.Errorf("burst request %d/5 should be allowed immediately; got denied", i+1)
		}
	}

	// 6th request should not be immediately allowed (burst exhausted).
	if lim.Allow() {
		t.Errorf("6th request (beyond burst=5) should not be immediately allowed")
	}
}
