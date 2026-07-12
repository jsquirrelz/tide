/*
Copyright 2026 TIDE Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
*/

// Plan 04-11 Task 1 — informer_bridge_test.go: covers the per-kind event
// projection helpers (extractProjectKeyForProject, ...ForMilestone,
// ...ForPhase, ...ForPlan, ...ForTask, ...ForWave) and the high-level
// BridgeInformerToHub wiring that registers handlers across all 6 CRD
// kinds against a mock cache.
//
// We unit-test the projection helpers exhaustively (pure functions on
// CRDs) and smoke-test BridgeInformerToHub against a fakeCache that
// records every GetInformer + AddEventHandler call. The full informer
// → reconcile loop is exercised in plan 04-14's kind harness.

package api

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/go-logr/logr/testr"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	toolscache "k8s.io/client-go/tools/cache"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrlfake "sigs.k8s.io/controller-runtime/pkg/client/fake"

	tidev1alpha3 "github.com/jsquirrelz/tide/api/v1alpha3"
	"github.com/jsquirrelz/tide/cmd/dashboard/hub"
)

// TestExtractProjectKeyForProject — a Project's "owner project" is itself.
func TestExtractProjectKeyForProject(t *testing.T) {
	p := &tidev1alpha3.Project{
		ObjectMeta: metav1.ObjectMeta{Name: "alpha", Namespace: "default"},
	}
	c := ctrlfake.NewClientBuilder().WithScheme(testInformerScheme(t)).Build()
	got, err := resolveProjectKey(context.Background(), c, p)
	if err != nil {
		t.Fatalf("resolveProjectKey: %v", err)
	}
	if got != "alpha" {
		t.Errorf("project key = %q, want %q", got, "alpha")
	}
}

// TestExtractProjectKeyForMilestone — a Milestone's owner project comes
// from Spec.ProjectRef.
func TestExtractProjectKeyForMilestone(t *testing.T) {
	m := &tidev1alpha3.Milestone{
		ObjectMeta: metav1.ObjectMeta{Name: "m1", Namespace: "default"},
		Spec:       tidev1alpha3.MilestoneSpec{ProjectRef: "alpha"},
	}
	c := ctrlfake.NewClientBuilder().WithScheme(testInformerScheme(t)).Build()
	got, err := resolveProjectKey(context.Background(), c, m)
	if err != nil {
		t.Fatalf("resolveProjectKey: %v", err)
	}
	if got != "alpha" {
		t.Errorf("milestone owner project = %q, want %q", got, "alpha")
	}
}

// TestExtractProjectKeyForPhase — Phase → Milestone → Project.
func TestExtractProjectKeyForPhase(t *testing.T) {
	m := &tidev1alpha3.Milestone{
		ObjectMeta: metav1.ObjectMeta{Name: "m1", Namespace: "default"},
		Spec:       tidev1alpha3.MilestoneSpec{ProjectRef: "alpha"},
	}
	ph := &tidev1alpha3.Phase{
		ObjectMeta: metav1.ObjectMeta{Name: "p1", Namespace: "default"},
		Spec:       tidev1alpha3.PhaseSpec{MilestoneRef: "m1"},
	}
	c := ctrlfake.NewClientBuilder().WithScheme(testInformerScheme(t)).WithObjects(m).Build()
	got, err := resolveProjectKey(context.Background(), c, ph)
	if err != nil {
		t.Fatalf("resolveProjectKey: %v", err)
	}
	if got != "alpha" {
		t.Errorf("phase owner project = %q, want %q", got, "alpha")
	}
}

// TestExtractProjectKeyForPlan — Plan → Phase → Milestone → Project.
func TestExtractProjectKeyForPlan(t *testing.T) {
	m := &tidev1alpha3.Milestone{
		ObjectMeta: metav1.ObjectMeta{Name: "m1", Namespace: "default"},
		Spec:       tidev1alpha3.MilestoneSpec{ProjectRef: "alpha"},
	}
	ph := &tidev1alpha3.Phase{
		ObjectMeta: metav1.ObjectMeta{Name: "p1", Namespace: "default"},
		Spec:       tidev1alpha3.PhaseSpec{MilestoneRef: "m1"},
	}
	pl := &tidev1alpha3.Plan{
		ObjectMeta: metav1.ObjectMeta{Name: "pl1", Namespace: "default"},
		Spec:       tidev1alpha3.PlanSpec{PhaseRef: "p1"},
	}
	c := ctrlfake.NewClientBuilder().WithScheme(testInformerScheme(t)).WithObjects(m, ph).Build()
	got, err := resolveProjectKey(context.Background(), c, pl)
	if err != nil {
		t.Fatalf("resolveProjectKey: %v", err)
	}
	if got != "alpha" {
		t.Errorf("plan owner project = %q, want %q", got, "alpha")
	}
}

// TestExtractProjectKeyForTask — Task → Plan → Phase → Milestone → Project.
func TestExtractProjectKeyForTask(t *testing.T) {
	m := &tidev1alpha3.Milestone{
		ObjectMeta: metav1.ObjectMeta{Name: "m1", Namespace: "default"},
		Spec:       tidev1alpha3.MilestoneSpec{ProjectRef: "alpha"},
	}
	ph := &tidev1alpha3.Phase{
		ObjectMeta: metav1.ObjectMeta{Name: "p1", Namespace: "default"},
		Spec:       tidev1alpha3.PhaseSpec{MilestoneRef: "m1"},
	}
	pl := &tidev1alpha3.Plan{
		ObjectMeta: metav1.ObjectMeta{Name: "pl1", Namespace: "default"},
		Spec:       tidev1alpha3.PlanSpec{PhaseRef: "p1"},
	}
	tk := &tidev1alpha3.Task{
		ObjectMeta: metav1.ObjectMeta{Name: "tk1", Namespace: "default"},
		Spec: tidev1alpha3.TaskSpec{
			PlanRef:             "pl1",
			FilesTouched:        []string{"a.go"},
			DeclaredOutputPaths: []string{"/workspace/a"},
		},
	}
	c := ctrlfake.NewClientBuilder().WithScheme(testInformerScheme(t)).WithObjects(m, ph, pl).Build()
	got, err := resolveProjectKey(context.Background(), c, tk)
	if err != nil {
		t.Fatalf("resolveProjectKey: %v", err)
	}
	if got != "alpha" {
		t.Errorf("task owner project = %q, want %q", got, "alpha")
	}
}

// TestExtractProjectKeyForWave — v1alpha3 Waves are global-scope and reference
// the owning Project directly via Spec.ProjectRef (resolved in one hop, no Plan
// walk).
func TestExtractProjectKeyForWave(t *testing.T) {
	wv := &tidev1alpha3.Wave{
		ObjectMeta: metav1.ObjectMeta{Name: "wv1", Namespace: "default"},
		Spec:       tidev1alpha3.WaveSpec{ProjectRef: "alpha", WaveIndex: 0},
	}
	c := ctrlfake.NewClientBuilder().WithScheme(testInformerScheme(t)).Build()
	got, err := resolveProjectKey(context.Background(), c, wv)
	if err != nil {
		t.Fatalf("resolveProjectKey: %v", err)
	}
	if got != "alpha" {
		t.Errorf("wave owner project = %q, want %q", got, "alpha")
	}
}

// TestInformerBridgeWiresAllKinds confirms BridgeInformerToHub calls
// AddEventHandler on the informer for ALL six TIDE kinds. We use a
// fakeCache that records GetInformer calls; assertion shape is:
//   - GetInformer was called for Project, Milestone, Phase, Plan, Task, Wave
//   - For each, AddEventHandler was called with a non-nil handler.
func TestInformerBridgeWiresAllKinds(t *testing.T) {
	fc := newFakeCache(testInformerScheme(t))
	c := ctrlfake.NewClientBuilder().WithScheme(testInformerScheme(t)).Build()
	h := hub.NewHub(testr.New(t))

	if err := BridgeInformerToHub(context.Background(), fc, c, h, testr.New(t)); err != nil {
		t.Fatalf("BridgeInformerToHub: %v", err)
	}

	expected := []string{
		"Project",
		"Milestone",
		"Phase",
		"Plan",
		"Task",
		"Wave",
	}
	for _, kind := range expected {
		if !fc.handlerRegisteredFor(kind) {
			t.Errorf("BridgeInformerToHub did not AddEventHandler for kind %q", kind)
		}
	}
}

// TestInformerBridgePublishesOnAdd confirms a synthetic OnAdd against the
// registered Project handler results in hub.Publish being called with the
// right project name + a JSON projection body.
func TestInformerBridgePublishesOnAdd(t *testing.T) {
	fc := newFakeCache(testInformerScheme(t))
	c := ctrlfake.NewClientBuilder().WithScheme(testInformerScheme(t)).Build()
	h := hub.NewHub(testr.New(t))

	if err := BridgeInformerToHub(context.Background(), fc, c, h, testr.New(t)); err != nil {
		t.Fatalf("BridgeInformerToHub: %v", err)
	}

	// Subscribe BEFORE firing the synthetic event so we capture it.
	sub := h.Subscribe("alpha", 0)
	defer h.Unsubscribe(sub)

	handler := fc.handlerForKind("Project")
	if handler == nil {
		t.Fatal("no Project handler registered")
	}
	handler.OnAdd(&tidev1alpha3.Project{
		ObjectMeta: metav1.ObjectMeta{Name: "alpha", Namespace: "default"},
		Status:     tidev1alpha3.ProjectStatus{Phase: "Running"},
	}, false)

	select {
	case ev := <-sub.Events():
		if ev.Type != "project.create" {
			t.Errorf("event Type = %q, want %q", ev.Type, "project.create")
		}
		var payload map[string]any
		if err := json.Unmarshal(ev.JSON, &payload); err != nil {
			t.Fatalf("unmarshal payload: %v", err)
		}
		if payload["name"] != "alpha" {
			t.Errorf("payload name = %v, want alpha", payload["name"])
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("hub.Publish was not called within 200ms of OnAdd")
	}
}

// TestInformerBridgePublishesMilestoneCreateWithProjectKey confirms that a
// Milestone OnAdd Publishes against the Milestone's parent Project key
// (resolved via Spec.ProjectRef).
func TestInformerBridgePublishesMilestoneCreateWithProjectKey(t *testing.T) {
	fc := newFakeCache(testInformerScheme(t))
	c := ctrlfake.NewClientBuilder().WithScheme(testInformerScheme(t)).Build()
	h := hub.NewHub(testr.New(t))
	if err := BridgeInformerToHub(context.Background(), fc, c, h, testr.New(t)); err != nil {
		t.Fatalf("BridgeInformerToHub: %v", err)
	}

	sub := h.Subscribe("alpha", 0)
	defer h.Unsubscribe(sub)

	handler := fc.handlerForKind("Milestone")
	if handler == nil {
		t.Fatal("no Milestone handler registered")
	}
	handler.OnAdd(&tidev1alpha3.Milestone{
		ObjectMeta: metav1.ObjectMeta{Name: "m1", Namespace: "default"},
		Spec:       tidev1alpha3.MilestoneSpec{ProjectRef: "alpha"},
		Status:     tidev1alpha3.MilestoneStatus{Phase: "Pending"},
	}, false)

	select {
	case ev := <-sub.Events():
		if ev.Type != "milestone.create" {
			t.Errorf("event Type = %q, want %q", ev.Type, "milestone.create")
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("hub.Publish was not called within 200ms of Milestone OnAdd")
	}
}

// TestInformerBridgePublishesOnStatusOnlyProjectUpdate pins the contract that
// status-only Project updates MUST publish through the informer bridge.
// The BudgetBlocked dashboard badge (BUDGET-02) depends on it: the controller
// issues a status-only MergeFrom patch when setBudgetBlockedIfNeeded fires;
// that patch produces an informer UpdateFunc call which must reach
// hub.Publish("alpha", "project.update") so PlanningDAGView can re-fetch
// ProjectDetail and render the badge.
//
// Today newKindHandler's UpdateFunc publishes unconditionally (no old/new diff
// filter). This test pins that contract so a future "skip no-op updates"
// optimization cannot silently filter status-only condition changes and kill
// the badge's liveness.
func TestInformerBridgePublishesOnStatusOnlyProjectUpdate(t *testing.T) {
	fc := newFakeCache(testInformerScheme(t))
	c := ctrlfake.NewClientBuilder().WithScheme(testInformerScheme(t)).Build()
	h := hub.NewHub(testr.New(t))

	if err := BridgeInformerToHub(context.Background(), fc, c, h, testr.New(t)); err != nil {
		t.Fatalf("BridgeInformerToHub: %v", err)
	}

	// Subscribe BEFORE firing the synthetic event so we capture it.
	sub := h.Subscribe("alpha", 0)
	defer h.Unsubscribe(sub)

	handler := fc.handlerForKind("Project")
	if handler == nil {
		t.Fatal("no Project handler registered")
	}

	// oldObj and newObj are identical Projects except:
	//   - newObj.Status.Conditions gains a True BudgetBlocked metav1.Condition
	//   - newObj.ResourceVersion is bumped ("1" → "2")
	// This mirrors the shape of a controller status-only MergeFrom patch.
	oldObj := &tidev1alpha3.Project{
		ObjectMeta: metav1.ObjectMeta{
			Name:            "alpha",
			Namespace:       "default",
			ResourceVersion: "1",
		},
		Status: tidev1alpha3.ProjectStatus{Phase: "Running"},
	}
	newObj := &tidev1alpha3.Project{
		ObjectMeta: metav1.ObjectMeta{
			Name:            "alpha",
			Namespace:       "default",
			ResourceVersion: "2",
		},
		Status: tidev1alpha3.ProjectStatus{
			Phase: "Running",
			Conditions: []metav1.Condition{
				{
					Type:               tidev1alpha3.ConditionBudgetBlocked,
					Status:             metav1.ConditionTrue,
					Reason:             "BudgetCapReached",
					Message:            "cap reached",
					LastTransitionTime: metav1.Now(),
				},
			},
		},
	}

	handler.OnUpdate(oldObj, newObj)

	select {
	case ev := <-sub.Events():
		if ev.Type != "project.update" {
			t.Errorf("event Type = %q, want %q", ev.Type, "project.update")
		}
		var payload map[string]any
		if err := json.Unmarshal(ev.JSON, &payload); err != nil {
			t.Fatalf("unmarshal payload: %v", err)
		}
		if payload["name"] != "alpha" {
			t.Errorf("payload name = %v, want alpha", payload["name"])
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("hub.Publish was not called within 200ms of OnUpdate (status-only Project update)")
	}
}

// TestInformerBridgeTaskUpdatePublishesBothTaskAndWavesSnapshot confirms that
// a Task OnUpdate fires BOTH a task.update event AND a waves.snapshot event
// on the project hub. The waves.snapshot reflects the fixture's wave grouping.
// This is the key invariant for the 15-06 CUTS-06 running-waves view.
func TestInformerBridgeTaskUpdatePublishesBothTaskAndWavesSnapshot(t *testing.T) {
	// Seed the owner chain needed by resolveProjectKey for a Task:
	// Task.Spec.PlanRef → Plan.Spec.PhaseRef → Phase.Spec.MilestoneRef → Milestone.Spec.ProjectRef
	ms := &tidev1alpha3.Milestone{
		ObjectMeta: metav1.ObjectMeta{Name: "ms1", Namespace: "default"},
		Spec:       tidev1alpha3.MilestoneSpec{ProjectRef: "proj1"},
	}
	ph := &tidev1alpha3.Phase{
		ObjectMeta: metav1.ObjectMeta{Name: "ph1", Namespace: "default"},
		Spec:       tidev1alpha3.PhaseSpec{MilestoneRef: "ms1"},
	}
	pl := &tidev1alpha3.Plan{
		ObjectMeta: metav1.ObjectMeta{Name: "plan-x", Namespace: "default"},
		Spec:       tidev1alpha3.PlanSpec{PhaseRef: "ph1"},
	}
	// The Task itself — with wave labels so computeRunningWaves finds it.
	tk := &tidev1alpha3.Task{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "task-run-1",
			Namespace: "default",
			Labels: map[string]string{
				labelProject:   "proj1",
				labelWaveIndex: "0",
			},
		},
		Spec: tidev1alpha3.TaskSpec{
			PlanRef:             "plan-x",
			FilesTouched:        []string{"a.go"},
			DeclaredOutputPaths: []string{"/workspace/a"},
		},
		Status: tidev1alpha3.TaskStatus{Phase: "Running"},
	}

	scheme := testInformerScheme(t)
	c := ctrlfake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(ms, ph, pl, tk).
		Build()
	fc := newFakeCache(scheme)
	h := hub.NewHub(testr.New(t))

	if err := BridgeInformerToHub(context.Background(), fc, c, h, testr.New(t)); err != nil {
		t.Fatalf("BridgeInformerToHub: %v", err)
	}

	sub := h.Subscribe("proj1", 0)
	defer h.Unsubscribe(sub)

	handler := fc.handlerForKind("Task")
	if handler == nil {
		t.Fatal("no Task handler registered")
	}
	// Fire a synthetic OnUpdate — same object as old/new is fine for this test.
	handler.OnUpdate(tk, tk)

	// Collect events; we expect task.update + waves.snapshot (order may vary
	// since map iteration is non-deterministic, but both must arrive).
	got := map[string]json.RawMessage{}
	deadline := time.NewTimer(300 * time.Millisecond)
	defer deadline.Stop()
	for len(got) < 2 {
		select {
		case ev := <-sub.Events():
			got[ev.Type] = ev.JSON
		case <-deadline.C:
			t.Fatalf("timed out waiting for events; received: %v", mapKeys(got))
		}
	}

	if _, ok := got["task.update"]; !ok {
		t.Error("expected task.update event; not received")
	}
	snapJSON, ok := got["waves.snapshot"]
	if !ok {
		t.Error("expected waves.snapshot event; not received")
	}

	// Verify the snapshot payload contains the expected wave.
	var snap WavesSnapshot
	if err := json.Unmarshal(snapJSON, &snap); err != nil {
		t.Fatalf("unmarshal waves.snapshot: %v", err)
	}
	if len(snap.Waves) != 1 {
		t.Fatalf("len(snap.Waves) = %d, want 1; snap=%+v", len(snap.Waves), snap)
	}
	if snap.Waves[0].PlanName != "plan-x" {
		t.Errorf("wave.PlanName = %q, want plan-x", snap.Waves[0].PlanName)
	}
	if snap.Waves[0].WaveIndex != 0 {
		t.Errorf("wave.WaveIndex = %d, want 0", snap.Waves[0].WaveIndex)
	}
}

// mapKeys returns the keys of a string-keyed map for test diagnostics.
func mapKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

// testInformerScheme returns a runtime.Scheme populated with all v1alpha3
// types + corev1 (needed by logs_sse_test.go for Pod registration). Same
// shape as the controller's scheme but bare so this test stays fast.
func testInformerScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	s := runtime.NewScheme()
	if err := tidev1alpha3.AddToScheme(s); err != nil {
		t.Fatalf("AddToScheme(v1alpha3): %v", err)
	}
	if err := corev1.AddToScheme(s); err != nil {
		t.Fatalf("AddToScheme(corev1): %v", err)
	}
	return s
}

// fakeCache implements the minimal cache.Informers surface BridgeInformerToHub
// needs: GetInformer returning a fakeInformer that records AddEventHandler
// calls. The remainder of the cache.Cache interface returns zero values —
// we never invoke those branches in this test.
type fakeCache struct {
	scheme    *runtime.Scheme
	mu        sync.Mutex
	informers map[string]*fakeInformer
}

func newFakeCache(scheme *runtime.Scheme) *fakeCache {
	return &fakeCache{
		scheme:    scheme,
		informers: make(map[string]*fakeInformer),
	}
}

func (f *fakeCache) handlerRegisteredFor(kind string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	inf, ok := f.informers[kind]
	if !ok {
		return false
	}
	return inf.handler != nil
}

func (f *fakeCache) handlerForKind(kind string) toolscache.ResourceEventHandler {
	f.mu.Lock()
	defer f.mu.Unlock()
	inf, ok := f.informers[kind]
	if !ok {
		return nil
	}
	return inf.handler
}

// GetInformer satisfies cache.Informers. We extract the kind name from the
// runtime object's GVK (registered via the scheme) and return our fake
// informer per kind.
func (f *fakeCache) GetInformer(_ context.Context, obj client.Object, _ ...cache.InformerGetOption) (cache.Informer, error) {
	gvks, _, err := f.scheme.ObjectKinds(obj)
	if err != nil {
		return nil, err
	}
	if len(gvks) == 0 {
		return nil, nil
	}
	kind := gvks[0].Kind
	f.mu.Lock()
	defer f.mu.Unlock()
	inf, ok := f.informers[kind]
	if !ok {
		inf = &fakeInformer{}
		f.informers[kind] = inf
	}
	return inf, nil
}

func (f *fakeCache) GetInformerForKind(_ context.Context, gvk schema.GroupVersionKind, _ ...cache.InformerGetOption) (cache.Informer, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	inf, ok := f.informers[gvk.Kind]
	if !ok {
		inf = &fakeInformer{}
		f.informers[gvk.Kind] = inf
	}
	return inf, nil
}

// RemoveInformer is a no-op for the bridge test (the bridge never calls
// it, but Cache requires it).
func (f *fakeCache) RemoveInformer(_ context.Context, _ client.Object) error {
	return nil
}

// Start is a no-op — production wiring runs the cache via the manager,
// not via the bridge.
func (f *fakeCache) Start(_ context.Context) error { return nil }

// WaitForCacheSync is a no-op — synchronously returns true.
func (f *fakeCache) WaitForCacheSync(_ context.Context) bool { return true }

// IndexField satisfies the FieldIndexer surface; never called by the
// bridge.
func (f *fakeCache) IndexField(_ context.Context, _ client.Object, _ string, _ client.IndexerFunc) error {
	return nil
}

// Read-side methods: the bridge never invokes them, but cache.Cache
// composes client.Reader so we stub them.
func (f *fakeCache) Get(_ context.Context, _ client.ObjectKey, _ client.Object, _ ...client.GetOption) error {
	return nil
}
func (f *fakeCache) List(_ context.Context, _ client.ObjectList, _ ...client.ListOption) error {
	return nil
}

// fakeInformer records the most recent handler added via AddEventHandler.
// The bridge only calls AddEventHandler; the other methods are stubs.
type fakeInformer struct {
	handler toolscache.ResourceEventHandler
}

func (f *fakeInformer) AddEventHandler(handler toolscache.ResourceEventHandler) (toolscache.ResourceEventHandlerRegistration, error) {
	f.handler = handler
	return nil, nil
}

func (f *fakeInformer) AddEventHandlerWithResyncPeriod(handler toolscache.ResourceEventHandler, _ time.Duration) (toolscache.ResourceEventHandlerRegistration, error) {
	f.handler = handler
	return nil, nil
}

func (f *fakeInformer) AddEventHandlerWithOptions(handler toolscache.ResourceEventHandler, _ toolscache.HandlerOptions) (toolscache.ResourceEventHandlerRegistration, error) {
	f.handler = handler
	return nil, nil
}

func (f *fakeInformer) RemoveEventHandler(_ toolscache.ResourceEventHandlerRegistration) error {
	return nil
}

func (f *fakeInformer) AddIndexers(_ toolscache.Indexers) error { return nil }

func (f *fakeInformer) HasSynced() bool { return true }

func (f *fakeInformer) HasSyncedChecker() toolscache.DoneChecker { return nil }

func (f *fakeInformer) IsStopped() bool { return false }
