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

// Package budget — see doc.go for package overview.
package budget

import (
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// Limits configures the token-bucket parameters for a provider credential Secret.
// Precedence for population (D-D3, highest first):
//
//  1. Secret annotations tideproject.k8s/requests-per-minute, tideproject.k8s/tokens-per-minute
//  2. Project.Spec.Providers[].RequestsPerMinute / TokensPerMinute
//  3. Helm-chart defaults (rateLimits.defaults.requestsPerMinute)
//
// Callers (TaskReconciler in Plan 09) compute the effective Limits before calling
// ForSecret; this package does not perform the precedence lookup.
type Limits struct {
	// RequestsPerMinute caps API calls per minute to the provider.
	// Zero means unlimited — ForSecret returns nil when RequestsPerMinute is 0.
	RequestsPerMinute int

	// BurstSize is the maximum burst token count allowed above the steady-state
	// rate. Defaults to 10 when not specified by the caller.
	BurstSize int
}

// Store is a sync.Map-backed in-process cache of per-Secret-UID rate limiters.
// It lives for the lifetime of the controller-manager process (D-D1 — in-memory
// only; no etcd persistence at sub-second granularity).
//
// Two Projects sharing the same provider Secret share a bucket because the
// Anthropic rate limit applies to the org API key, not the Project (D-D3).
type Store struct {
	// m maps secretUID (string) to *rate.Limiter.
	// sync.Map is safe for concurrent use without external locking.
	m sync.Map
}

// NewStore constructs an empty Store.
func NewStore() *Store {
	return &Store{}
}

// ForSecret returns (creates lazily, then caches) the rate.Limiter for secretUID.
//
// If limits.RequestsPerMinute is 0, returns nil — the caller treats nil as "no
// rate limit applies for this Secret."
//
// Concurrent calls for the same secretUID are safe: sync.Map.LoadOrStore
// guarantees that at most one Limiter is created per UID, and all racing
// goroutines receive the same *rate.Limiter.
func (s *Store) ForSecret(secretUID string, limits Limits) *rate.Limiter {
	if limits.RequestsPerMinute <= 0 {
		return nil
	}

	// Fast path: already stored.
	if v, ok := s.m.Load(secretUID); ok {
		return v.(*rate.Limiter) //nolint:forcetypeassert // only *rate.Limiter written to the map
	}

	// Slow path: create and atomically store.
	// rate.Every(interval) yields the token-refill rate; BurstSize is the
	// maximum tokens available in the bucket at once.
	interval := time.Minute / time.Duration(limits.RequestsPerMinute)
	lim := rate.NewLimiter(rate.Every(interval), limits.BurstSize)

	// LoadOrStore is atomic: if another goroutine stored first, we discard lim
	// and return the winner's value.
	actual, _ := s.m.LoadOrStore(secretUID, lim)
	return actual.(*rate.Limiter) //nolint:forcetypeassert // only *rate.Limiter written to the map
}

// Evict removes the cached bucket for secretUID. Called when the provider
// Secret is deleted or its UID changes (e.g. Secret re-creation). The next
// ForSecret call will lazily create a fresh Limiter.
func (s *Store) Evict(secretUID string) {
	s.m.Delete(secretUID)
}
