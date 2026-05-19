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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	toolscache "k8s.io/client-go/tools/cache"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrlfake "sigs.k8s.io/controller-runtime/pkg/client/fake"

	tidev1alpha1 "github.com/jsquirrelz/tide/api/v1alpha1"
	"github.com/jsquirrelz/tide/cmd/dashboard/hub"
)

// TestExtractProjectKeyForProject — a Project's "owner project" is itself.
func TestExtractProjectKeyForProject(t *testing.T) {
	p := &tidev1alpha1.Project{
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
	m := &tidev1alpha1.Milestone{
		ObjectMeta: metav1.ObjectMeta{Name: "m1", Namespace: "default"},
		Spec:       tidev1alpha1.MilestoneSpec{ProjectRef: "alpha"},
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
	m := &tidev1alpha1.Milestone{
		ObjectMeta: metav1.ObjectMeta{Name: "m1", Namespace: "default"},
		Spec:       tidev1alpha1.MilestoneSpec{ProjectRef: "alpha"},
	}
	ph := &tidev1alpha1.Phase{
		ObjectMeta: metav1.ObjectMeta{Name: "p1", Namespace: "default"},
		Spec:       tidev1alpha1.PhaseSpec{MilestoneRef: "m1"},
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
	m := &tidev1alpha1.Milestone{
		ObjectMeta: metav1.ObjectMeta{Name: "m1", Namespace: "default"},
		Spec:       tidev1alpha1.MilestoneSpec{ProjectRef: "alpha"},
	}
	ph := &tidev1alpha1.Phase{
		ObjectMeta: metav1.ObjectMeta{Name: "p1", Namespace: "default"},
		Spec:       tidev1alpha1.PhaseSpec{MilestoneRef: "m1"},
	}
	pl := &tidev1alpha1.Plan{
		ObjectMeta: metav1.ObjectMeta{Name: "pl1", Namespace: "default"},
		Spec:       tidev1alpha1.PlanSpec{PhaseRef: "p1"},
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
	m := &tidev1alpha1.Milestone{
		ObjectMeta: metav1.ObjectMeta{Name: "m1", Namespace: "default"},
		Spec:       tidev1alpha1.MilestoneSpec{ProjectRef: "alpha"},
	}
	ph := &tidev1alpha1.Phase{
		ObjectMeta: metav1.ObjectMeta{Name: "p1", Namespace: "default"},
		Spec:       tidev1alpha1.PhaseSpec{MilestoneRef: "m1"},
	}
	pl := &tidev1alpha1.Plan{
		ObjectMeta: metav1.ObjectMeta{Name: "pl1", Namespace: "default"},
		Spec:       tidev1alpha1.PlanSpec{PhaseRef: "p1"},
	}
	tk := &tidev1alpha1.Task{
		ObjectMeta: metav1.ObjectMeta{Name: "tk1", Namespace: "default"},
		Spec: tidev1alpha1.TaskSpec{
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

// TestExtractProjectKeyForWave — Wave → Plan → Phase → Milestone → Project.
func TestExtractProjectKeyForWave(t *testing.T) {
	m := &tidev1alpha1.Milestone{
		ObjectMeta: metav1.ObjectMeta{Name: "m1", Namespace: "default"},
		Spec:       tidev1alpha1.MilestoneSpec{ProjectRef: "alpha"},
	}
	ph := &tidev1alpha1.Phase{
		ObjectMeta: metav1.ObjectMeta{Name: "p1", Namespace: "default"},
		Spec:       tidev1alpha1.PhaseSpec{MilestoneRef: "m1"},
	}
	pl := &tidev1alpha1.Plan{
		ObjectMeta: metav1.ObjectMeta{Name: "pl1", Namespace: "default"},
		Spec:       tidev1alpha1.PlanSpec{PhaseRef: "p1"},
	}
	wv := &tidev1alpha1.Wave{
		ObjectMeta: metav1.ObjectMeta{Name: "wv1", Namespace: "default"},
		Spec:       tidev1alpha1.WaveSpec{PlanRef: "pl1", WaveIndex: 0},
	}
	c := ctrlfake.NewClientBuilder().WithScheme(testInformerScheme(t)).WithObjects(m, ph, pl).Build()
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
	handler.OnAdd(&tidev1alpha1.Project{
		ObjectMeta: metav1.ObjectMeta{Name: "alpha", Namespace: "default"},
		Status:     tidev1alpha1.ProjectStatus{Phase: "Running"},
	}, false)

	select {
	case ev := <-sub.Events():
		if ev.Type != "project.create" {
			t.Errorf("event Type = %q, want %q", ev.Type, "project.create")
		}
		var payload map[string]interface{}
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
	handler.OnAdd(&tidev1alpha1.Milestone{
		ObjectMeta: metav1.ObjectMeta{Name: "m1", Namespace: "default"},
		Spec:       tidev1alpha1.MilestoneSpec{ProjectRef: "alpha"},
		Status:     tidev1alpha1.MilestoneStatus{Phase: "Pending"},
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

// testInformerScheme returns a runtime.Scheme populated with all v1alpha1
// types — same shape as the controller's scheme but bare so this test
// stays fast.
func testInformerScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	s := runtime.NewScheme()
	if err := tidev1alpha1.AddToScheme(s); err != nil {
		t.Fatalf("AddToScheme: %v", err)
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
