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

// Package pool implements TIDE's two separately-sized parallelism budgets
// (planner pool, executor pool) as chan struct{} semaphores with a PreCharge
// helper that consumes slots equal to live Jobs at Manager startup.
//
// Per POOL-01: chan-based semaphore with Acquire/Release.
// Per POOL-02: PreCharge from live Job count via client.List.
//
// Pitfall 6 prevention (unified worker pool) is enforced separately by the
// crosspool analyzer in tools/analyzers/crosspool/. Phase 1 constructs both
// pools and calls PreCharge at Manager startup; Phase 2 is the first to call
// Acquire/Release from within the WaveReconciler dispatch path.
package pool

import (
	"context"
	"fmt"

	batchv1 "k8s.io/api/batch/v1"
	"k8s.io/apimachinery/pkg/labels"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Pool is a capacity-bounded semaphore using chan struct{} for Acquire/Release
// signalling. Pool instances are constructed once at Manager startup
// (cmd/manager/main.go) and passed by pointer into reconciler structs as
// the plannerPool / executorPool fields the crosspool analyzer keys on.
type Pool struct {
	sem  chan struct{}
	name string // "planner" | "executor" — used in error messages and logs/metrics
}

// New constructs a Pool with the given capacity and human-readable name.
// The name is used in PreCharge overflow error messages and (eventually) in
// Prometheus metric labels.
func New(capacity int, name string) *Pool {
	return &Pool{
		sem:  make(chan struct{}, capacity),
		name: name,
	}
}

// Acquire blocks until a slot is available or ctx is cancelled.
// Returns ctx.Err() if the context is cancelled or times out before a slot
// becomes free.
func (p *Pool) Acquire(ctx context.Context) error {
	select {
	case p.sem <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Release frees one slot. Must only be called after a successful Acquire —
// over-releasing panics by way of receiving from an empty channel (stdlib
// chan semantics: receive on empty buffered channel blocks; if drained by
// over-release the next legitimate Release will block forever instead).
//
// Calling Release after a failed Acquire (ctx cancellation) is a bug.
func (p *Pool) Release() {
	<-p.sem
}

// PreCharge inspects the cluster for Jobs matching labelSelector and consumes
// one Pool slot per Job with Status.Active > 0. This is called once per Pool
// at Manager startup so a leader-election failover doesn't accidentally
// double the in-flight Job count past the configured concurrency budget.
//
// Returns a descriptive error if more live Jobs are found than the Pool's
// capacity. The reconciler should treat that as a configuration error
// (capacity reduced below the running fleet) and refuse to start.
func (p *Pool) PreCharge(ctx context.Context, c client.Client, labelSelector string) error {
	sel, err := labels.Parse(labelSelector)
	if err != nil {
		return fmt.Errorf("pool %s: parse label selector %q: %w", p.name, labelSelector, err)
	}
	var jobs batchv1.JobList
	if err := c.List(ctx, &jobs, &client.ListOptions{LabelSelector: sel}); err != nil {
		return fmt.Errorf("pool %s: list jobs matching %q: %w", p.name, labelSelector, err)
	}
	consumed := 0
	for _, j := range jobs.Items {
		if j.Status.Active <= 0 {
			continue
		}
		select {
		case p.sem <- struct{}{}:
			consumed++
		default:
			return fmt.Errorf("pool %s capacity exceeded by pre-charge: live jobs=%d capacity=%d (matched selector %q, consumed %d slots before overflow)",
				p.name, countActive(jobs.Items), cap(p.sem), labelSelector, consumed)
		}
	}
	return nil
}

// countActive returns the number of Jobs in the slice with Status.Active > 0.
// Helper for diagnostic messages — not on the Acquire/Release hot path.
func countActive(items []batchv1.Job) int {
	n := 0
	for _, j := range items {
		if j.Status.Active > 0 {
			n++
		}
	}
	return n
}
