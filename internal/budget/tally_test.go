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

// Package budget unit tests for tally.go: RollUpUsage, MaybeResetWindow.
// Uses controller-runtime's fake client with WithStatusSubresource so Status
// patches actually persist.
package budget

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	tidev1alpha3 "github.com/jsquirrelz/tide/api/v1alpha3"
	pkgdispatch "github.com/jsquirrelz/tide/pkg/dispatch"
)

// newTallyFakeClient builds a fake client that honors Status subresource patches
// for Project objects.
func newTallyFakeClient(t *testing.T, objs ...client.Object) client.Client {
	t.Helper()
	s := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(s); err != nil {
		t.Fatalf("AddToScheme: %v", err)
	}
	if err := tidev1alpha3.AddToScheme(s); err != nil {
		t.Fatalf("AddToScheme tidev1alpha3: %v", err)
	}
	return fake.NewClientBuilder().
		WithScheme(s).
		WithObjects(objs...).
		WithStatusSubresource(&tidev1alpha3.Project{}).
		Build()
}

func makeProject(name string) *tidev1alpha3.Project {
	return &tidev1alpha3.Project{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "default",
		},
		Spec: tidev1alpha3.ProjectSpec{SchemaRevision: "v1alpha3",
			TargetRepo: "https://github.com/example/repo",
		},
	}
}

// TestRollUpUsage_AccumulatesAcrossCalls verifies that two successive RollUpUsage
// calls accumulate TokensSpent and CostSpentCents additively.
func TestRollUpUsage_AccumulatesAcrossCalls(t *testing.T) {
	p := makeProject("test-project")
	c := newTallyFakeClient(t, p)

	usage1 := pkgdispatch.Usage{InputTokens: 1000, OutputTokens: 500, EstimatedCostCents: 25}
	usage2 := pkgdispatch.Usage{InputTokens: 800, OutputTokens: 200, EstimatedCostCents: 15}

	if err := RollUpUsage(context.Background(), c, p, usage1); err != nil {
		t.Fatalf("first RollUpUsage: %v", err)
	}

	// Fetch updated project after first patch.
	updated := &tidev1alpha3.Project{}
	if err := c.Get(context.Background(), client.ObjectKeyFromObject(p), updated); err != nil {
		t.Fatalf("Get after first RollUpUsage: %v", err)
	}

	if err := RollUpUsage(context.Background(), c, updated, usage2); err != nil {
		t.Fatalf("second RollUpUsage: %v", err)
	}

	// Read final state.
	final := &tidev1alpha3.Project{}
	if err := c.Get(context.Background(), client.ObjectKeyFromObject(p), final); err != nil {
		t.Fatalf("Get after second RollUpUsage: %v", err)
	}

	wantTokens := int64(1000 + 500 + 800 + 200) // 2500
	wantCents := int64(25 + 15)                 // 40

	if final.Status.Budget.TokensSpent != wantTokens {
		t.Errorf("TokensSpent: got %d; want %d", final.Status.Budget.TokensSpent, wantTokens)
	}
	if final.Status.Budget.CostSpentCents != wantCents {
		t.Errorf("CostSpentCents: got %d; want %d", final.Status.Budget.CostSpentCents, wantCents)
	}
}

// TestRollUpUsage_RetriesOnConflict verifies that a concurrent roll-up landing
// between RollUpUsage's read and patch does not get clobbered: the optimistic
// lock surfaces a Conflict, the retry re-fetches the newer tally, and BOTH
// completions' costs survive (WR-09 — last-write-wins silently dropped spend).
func TestRollUpUsage_RetriesOnConflict(t *testing.T) {
	p := makeProject("test-project-conflict")

	s := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(s); err != nil {
		t.Fatalf("AddToScheme: %v", err)
	}
	if err := tidev1alpha3.AddToScheme(s); err != nil {
		t.Fatalf("AddToScheme tidev1alpha3: %v", err)
	}

	var injected sync.Once
	c := fake.NewClientBuilder().
		WithScheme(s).
		WithObjects(p).
		WithStatusSubresource(&tidev1alpha3.Project{}).
		WithInterceptorFuncs(interceptor.Funcs{
			SubResourcePatch: func(ctx context.Context, cl client.Client, subResourceName string, obj client.Object, patch client.Patch, opts ...client.SubResourcePatchOption) error {
				// Before the first patch lands, slip in a concurrent completion
				// that bumps resourceVersion — forcing a Conflict and a retry.
				injected.Do(func() {
					other := &tidev1alpha3.Project{}
					if err := cl.Get(ctx, client.ObjectKeyFromObject(p), other); err != nil {
						t.Errorf("interceptor Get: %v", err)
						return
					}
					base := client.MergeFrom(other.DeepCopy())
					other.Status.Budget.TokensSpent += 70
					other.Status.Budget.CostSpentCents += 7
					if err := cl.Status().Patch(ctx, other, base); err != nil {
						t.Errorf("interceptor concurrent patch: %v", err)
					}
				})
				return cl.SubResource(subResourceName).Patch(ctx, obj, patch, opts...)
			},
		}).
		Build()

	usage := pkgdispatch.Usage{InputTokens: 100, OutputTokens: 50, EstimatedCostCents: 25}
	if err := RollUpUsage(context.Background(), c, p, usage); err != nil {
		t.Fatalf("RollUpUsage: %v", err)
	}

	final := &tidev1alpha3.Project{}
	if err := c.Get(context.Background(), client.ObjectKeyFromObject(p), final); err != nil {
		t.Fatalf("Get: %v", err)
	}

	// Both the injected concurrent roll-up (70 tokens / 7 cents) and this
	// call's usage (150 tokens / 25 cents) must survive.
	if got, want := final.Status.Budget.TokensSpent, int64(70+150); got != want {
		t.Errorf("TokensSpent: got %d; want %d (concurrent completion's tokens were clobbered)", got, want)
	}
	if got, want := final.Status.Budget.CostSpentCents, int64(7+25); got != want {
		t.Errorf("CostSpentCents: got %d; want %d (concurrent completion's cost was clobbered)", got, want)
	}
}

// TestRollUpUsage_SetsWindowStartOnFirstCall verifies that WindowStart is set on
// the first RollUpUsage call when it was previously nil.
func TestRollUpUsage_SetsWindowStartOnFirstCall(t *testing.T) {
	p := makeProject("test-project-ws")
	c := newTallyFakeClient(t, p)

	if p.Status.Budget.WindowStart != nil {
		t.Fatal("expected nil WindowStart before first call")
	}

	usage := pkgdispatch.Usage{InputTokens: 100, OutputTokens: 50, EstimatedCostCents: 5}
	if err := RollUpUsage(context.Background(), c, p, usage); err != nil {
		t.Fatalf("RollUpUsage: %v", err)
	}

	updated := &tidev1alpha3.Project{}
	if err := c.Get(context.Background(), client.ObjectKeyFromObject(p), updated); err != nil {
		t.Fatalf("Get: %v", err)
	}
	if updated.Status.Budget.WindowStart == nil {
		t.Errorf("WindowStart should be set after first RollUpUsage call; got nil")
	}
}

// TestRollUpUsage_PreservesExistingWindowStart verifies that a pre-set WindowStart
// is not overwritten by subsequent RollUpUsage calls.
func TestRollUpUsage_PreservesExistingWindowStart(t *testing.T) {
	p := makeProject("test-project-pws")

	// Set a WindowStart before creating the client.
	existingTime := metav1.Now()
	p.Status.Budget.WindowStart = &existingTime
	c := newTallyFakeClient(t, p)

	usage := pkgdispatch.Usage{InputTokens: 100, OutputTokens: 50, EstimatedCostCents: 5}
	if err := RollUpUsage(context.Background(), c, p, usage); err != nil {
		t.Fatalf("RollUpUsage: %v", err)
	}

	updated := &tidev1alpha3.Project{}
	if err := c.Get(context.Background(), client.ObjectKeyFromObject(p), updated); err != nil {
		t.Fatalf("Get: %v", err)
	}
	if updated.Status.Budget.WindowStart == nil {
		t.Errorf("WindowStart should be preserved; got nil")
	}
	// Compare with second-level truncation since metav1.Time is serialized to
	// JSON as RFC3339 (second precision) and the fake client round-trips through
	// JSON encoding, stripping sub-second precision and the monotonic clock reading.
	// WR-08: time.Second is the self-documenting form of the previous magic
	// `Truncate(1000000000)` (1e9 ns).
	if !updated.Status.Budget.WindowStart.Time.Truncate(time.Second).Equal(existingTime.Truncate(time.Second)) {
		t.Errorf("WindowStart changed: got %v; want %v", updated.Status.Budget.WindowStart, existingTime)
	}
}

// TestMaybeResetWindow_TableDriven covers the window-reset cases:
//   - no rolling cap configured        → no-op
//   - rolling cap set but WindowStart nil → no-op (first roll-up sets it)
//   - window not yet elapsed           → no-op
//   - window elapsed, default duration → reset
//   - window elapsed, custom duration  → reset
//
// Uses the existing newTallyFakeClient + makeProject helpers (Phase 04.1 P4.1).
func TestMaybeResetWindow_TableDriven(t *testing.T) {
	baseTime := time.Date(2026, 5, 20, 12, 0, 0, 0, time.UTC)
	baseTimeMeta := metav1.NewTime(baseTime)

	cases := []struct {
		name            string
		rollingCapCents int64
		windowDuration  *metav1.Duration
		windowStart     *metav1.Time
		costSpent       int64
		now             time.Time
		wantReset       bool
		wantCostSpent   int64 // expected CostSpentCents AFTER MaybeResetWindow
	}{
		{
			name:            "no rolling cap → no-op",
			rollingCapCents: 0,
			windowStart:     &baseTimeMeta,
			costSpent:       50,
			now:             baseTime.Add(48 * time.Hour),
			wantReset:       false,
			wantCostSpent:   50,
		},
		{
			name:            "rolling cap set but WindowStart nil → no-op",
			rollingCapCents: 100,
			windowStart:     nil,
			costSpent:       50,
			now:             baseTime,
			wantReset:       false,
			wantCostSpent:   50,
		},
		{
			name:            "window not elapsed (default 24h) → no-op",
			rollingCapCents: 100,
			windowStart:     &baseTimeMeta,
			costSpent:       50,
			now:             baseTime.Add(23 * time.Hour),
			wantReset:       false,
			wantCostSpent:   50,
		},
		{
			name:            "window elapsed (default 24h) → reset",
			rollingCapCents: 100,
			windowStart:     &baseTimeMeta,
			costSpent:       95,
			now:             baseTime.Add(25 * time.Hour),
			wantReset:       true,
			wantCostSpent:   0,
		},
		{
			name:            "window elapsed (custom 1h duration) → reset",
			rollingCapCents: 100,
			windowDuration:  &metav1.Duration{Duration: 1 * time.Hour},
			windowStart:     &baseTimeMeta,
			costSpent:       95,
			now:             baseTime.Add(2 * time.Hour),
			wantReset:       true,
			wantCostSpent:   0,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := makeProject(fmt.Sprintf("test-%s", strings.ReplaceAll(tc.name, " ", "-")))
			p.Spec.Budget.RollingWindowCapCents = tc.rollingCapCents
			p.Spec.Budget.RollingWindowDuration = tc.windowDuration
			p.Status.Budget.WindowStart = tc.windowStart
			p.Status.Budget.CostSpentCents = tc.costSpent

			c := newTallyFakeClient(t, p)
			ctx := context.Background()

			reset, err := MaybeResetWindow(ctx, c, p, tc.now)
			if err != nil {
				t.Fatalf("MaybeResetWindow: %v", err)
			}
			if reset != tc.wantReset {
				t.Errorf("reset = %v; want %v", reset, tc.wantReset)
			}

			var updated tidev1alpha3.Project
			if err := c.Get(ctx, client.ObjectKey{Namespace: p.Namespace, Name: p.Name}, &updated); err != nil {
				t.Fatalf("get project: %v", err)
			}
			if updated.Status.Budget.CostSpentCents != tc.wantCostSpent {
				t.Errorf("CostSpentCents = %d; want %d", updated.Status.Budget.CostSpentCents, tc.wantCostSpent)
			}
		})
	}
}
