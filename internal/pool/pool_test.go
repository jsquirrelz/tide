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

// Package pool unit tests for the chan struct{} semaphore + PreCharge
// helper. Uses controller-runtime's fake client to verify PreCharge consumes
// the right number of slots without an envtest cluster.
//
// Per POOL-01 (chan-based semaphore) and POOL-02 (PreCharge from live Jobs).
package pool

import (
	"context"
	"strings"
	"testing"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func newFakeClient(t *testing.T, objs ...client.Object) client.Client {
	t.Helper()
	s := runtime.NewScheme()
	if err := scheme.AddToScheme(s); err != nil {
		t.Fatalf("AddToScheme: %v", err)
	}
	return fake.NewClientBuilder().WithScheme(s).WithObjects(objs...).Build()
}

func TestPoolAcquireRelease(t *testing.T) {
	p := New(2, "test")
	ctx := context.Background()

	if err := p.Acquire(ctx); err != nil {
		t.Fatalf("first Acquire: %v", err)
	}
	if err := p.Acquire(ctx); err != nil {
		t.Fatalf("second Acquire: %v", err)
	}

	// Third Acquire on capacity-2 pool must block. Use a short-deadline ctx
	// to prove blocking semantics without actually deadlocking the test.
	blockedCtx, cancel := context.WithTimeout(ctx, 25*time.Millisecond)
	defer cancel()
	if err := p.Acquire(blockedCtx); err == nil {
		t.Fatalf("third Acquire should have blocked + timed out, got nil")
	}

	// Release one slot; next Acquire should succeed immediately.
	p.Release()
	if err := p.Acquire(ctx); err != nil {
		t.Fatalf("Acquire after Release: %v", err)
	}
	p.Release()
	p.Release()
}

func TestPoolAcquireCtxCancel(t *testing.T) {
	p := New(1, "test")
	ctx := context.Background()
	if err := p.Acquire(ctx); err != nil {
		t.Fatalf("first Acquire: %v", err)
	}

	// Pool is full; cancel before next Acquire returns and assert ctx.Err().
	cancelCtx, cancel := context.WithCancel(ctx)
	cancel()
	if err := p.Acquire(cancelCtx); err == nil {
		t.Fatalf("Acquire on full pool with cancelled ctx should error")
	} else if err != context.Canceled {
		t.Errorf("expected context.Canceled, got %v", err)
	}
	p.Release()
}

func TestPoolPreChargeFromZeroJobs(t *testing.T) {
	c := newFakeClient(t)
	p := New(4, "executor")
	if err := p.PreCharge(context.Background(), c, "tideproject.k8s/role=executor"); err != nil {
		t.Fatalf("PreCharge with no jobs: %v", err)
	}
	// Capacity unchanged: all 4 slots should still be acquirable.
	for i := 0; i < 4; i++ {
		if err := p.Acquire(context.Background()); err != nil {
			t.Fatalf("Acquire %d/4 after empty PreCharge: %v", i+1, err)
		}
	}
}

// makeActiveJob constructs a batchv1.Job with Status.Active=1 and the given
// label-selector key/value pair.
func makeActiveJob(name string, labels map[string]string) *batchv1.Job {
	return &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "default",
			Labels:    labels,
		},
		Status: batchv1.JobStatus{Active: 1},
	}
}

func TestPoolPreChargeFromLiveJobs(t *testing.T) {
	labels := map[string]string{"tideproject.k8s/role": "executor"}
	c := newFakeClient(t,
		makeActiveJob("j1", labels),
		makeActiveJob("j2", labels),
		makeActiveJob("j3", labels),
	)
	p := New(4, "executor")
	if err := p.PreCharge(context.Background(), c, "tideproject.k8s/role=executor"); err != nil {
		t.Fatalf("PreCharge with 3 live jobs: %v", err)
	}
	// 3 slots consumed → 1 slot remaining; one Acquire succeeds, next blocks.
	ctx := context.Background()
	if err := p.Acquire(ctx); err != nil {
		t.Fatalf("first Acquire after PreCharge: %v", err)
	}
	blockedCtx, cancel := context.WithTimeout(ctx, 25*time.Millisecond)
	defer cancel()
	if err := p.Acquire(blockedCtx); err == nil {
		t.Fatalf("second Acquire after PreCharge should block on full pool")
	}
	p.Release()
}

func TestPoolPreChargeOverflow(t *testing.T) {
	labels := map[string]string{"tideproject.k8s/role": "executor"}
	c := newFakeClient(t,
		makeActiveJob("j1", labels),
		makeActiveJob("j2", labels),
		makeActiveJob("j3", labels),
		makeActiveJob("j4", labels),
		makeActiveJob("j5", labels),
	)
	p := New(4, "executor")
	err := p.PreCharge(context.Background(), c, "tideproject.k8s/role=executor")
	if err == nil {
		t.Fatalf("PreCharge with 5 live jobs in capacity-4 pool should error")
	}
	if !strings.Contains(err.Error(), "executor") {
		t.Errorf("error %q should mention pool name 'executor'", err.Error())
	}
	if !strings.Contains(err.Error(), "capacity") {
		t.Errorf("error %q should mention capacity overflow", err.Error())
	}
}
