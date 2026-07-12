/*
Copyright 2026 TIDE Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/go-logr/logr/testr"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	tidev1alpha3 "github.com/jsquirrelz/tide/api/v1alpha3"
)

// newPlansHandler returns a PlansHandler with a fake controller-runtime
// client populated with `objs`, plus a chi router mounted on
// /api/v1/plans/{name} so tests exercise the full URL-param path.
func newPlansHandler(t *testing.T, objs ...runtime.Object) (*PlansHandler, http.Handler) {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := tidev1alpha3.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme: %v", err)
	}
	builder := fake.NewClientBuilder().WithScheme(scheme)
	for _, o := range objs {
		builder = builder.WithRuntimeObjects(o)
	}
	c := builder.Build()
	h := &PlansHandler{Client: c, Log: testr.New(t)}

	r := chi.NewRouter()
	r.Route("/api/v1", func(r chi.Router) {
		r.Get("/plans/{name}", h.Get)
	})
	return h, r
}

// newPlanCRD is a minimal Plan factory.
func newPlanCRD(name, namespace, phaseRef, phase string) *tidev1alpha3.Plan {
	return &tidev1alpha3.Plan{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Spec:       tidev1alpha3.PlanSpec{PhaseRef: phaseRef},
		Status:     tidev1alpha3.PlanStatus{Phase: phase},
	}
}

// newTaskCRD is a minimal Task factory.
func newTaskCRD(name, namespace, planRef, phase string, dependsOn []string, attempt int) *tidev1alpha3.Task {
	return &tidev1alpha3.Task{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Spec: tidev1alpha3.TaskSpec{
			PlanRef:             planRef,
			DependsOn:           dependsOn,
			FilesTouched:        []string{"a.go"},
			DeclaredOutputPaths: []string{"/workspace/a"},
		},
		Status: tidev1alpha3.TaskStatus{Phase: phase, Attempt: attempt},
	}
}

// newWaveCRD is a minimal Wave factory. v1alpha3 Waves are global-scope:
// ProjectRef replaces the removed PlanRef. The Plan→Wave association is derived
// by the plans handler from Wave.Status.TaskRefs membership, not from the spec.
func newWaveCRD(name, namespace, projectRef string, waveIndex int, taskRefs []string) *tidev1alpha3.Wave {
	return &tidev1alpha3.Wave{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Spec:       tidev1alpha3.WaveSpec{ProjectRef: projectRef, WaveIndex: waveIndex},
		Status:     tidev1alpha3.WaveStatus{TaskRefs: taskRefs},
	}
}

// TestPlansHandlerHappyPath covers case 1: a Plan with 2 Tasks (one
// depends_on across a wave boundary) and 2 Waves materialized. The
// response carries 2 planTaskCards sorted by (waveIndex ASC, name ASC).
func TestPlansHandlerHappyPath(t *testing.T) {
	plan := newPlanCRD("p-auth", "default", "ph-1", "Running")
	tA := newTaskCRD("t-a", "default", "p-auth", "Succeeded", nil, 1)
	tB := newTaskCRD("t-b", "default", "p-auth", "Running", []string{"t-a"}, 1)
	w0 := newWaveCRD("p-auth-w0", "default", "p-auth", 0, []string{"t-a"})
	w1 := newWaveCRD("p-auth-w1", "default", "p-auth", 1, []string{"t-b"})

	_, router := newPlansHandler(t, plan, tA, tB, w0, w1)
	srv := httptest.NewServer(router)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/v1/plans/p-auth")
	if err != nil {
		t.Fatalf("GET /api/v1/plans/p-auth: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d want 200", resp.StatusCode)
	}

	var body planDetail
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Name != "p-auth" {
		t.Errorf("name=%q want p-auth", body.Name)
	}
	if body.Phase != "Running" {
		t.Errorf("phase=%q want Running", body.Phase)
	}
	if body.PhaseRef != "ph-1" {
		t.Errorf("phaseRef=%q want ph-1", body.PhaseRef)
	}
	if len(body.Tasks) != 2 {
		t.Fatalf("tasks len=%d want 2", len(body.Tasks))
	}
	// Sort: (waveIndex ASC, name ASC) — t-a (wave 0) before t-b (wave 1).
	if body.Tasks[0].Name != "t-a" || body.Tasks[0].WaveIndex != 0 {
		t.Errorf("tasks[0]=%+v want {Name:t-a WaveIndex:0}", body.Tasks[0])
	}
	if body.Tasks[1].Name != "t-b" || body.Tasks[1].WaveIndex != 1 {
		t.Errorf("tasks[1]=%+v want {Name:t-b WaveIndex:1}", body.Tasks[1])
	}
	// dependsOn carries across.
	if len(body.Tasks[1].DependsOn) != 1 || body.Tasks[1].DependsOn[0] != "t-a" {
		t.Errorf("tasks[1].DependsOn=%v want [t-a]", body.Tasks[1].DependsOn)
	}
	// activeDispatchWave: t-b is Running on wave 1, no Dispatching/Running on
	// wave 0. So ActiveDispatchWave == 1.
	if body.ActiveDispatchWave == nil || *body.ActiveDispatchWave != 1 {
		t.Errorf("activeDispatchWave=%v want *1", body.ActiveDispatchWave)
	}
}

// TestPlansHandlerNotFound covers case 2: 404 when the Plan doesn't exist.
func TestPlansHandlerNotFound(t *testing.T) {
	_, router := newPlansHandler(t)
	srv := httptest.NewServer(router)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/v1/plans/does-not-exist")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status=%d want 404", resp.StatusCode)
	}
	var body errorBody
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Error == "" {
		t.Errorf("expected non-empty error body, got %+v", body)
	}
}

// TestPlansHandlerTasksWithoutWaves covers case 3: Tasks present but no
// Waves materialized → waveIndex=0 for all, ActiveDispatchWave nil.
func TestPlansHandlerTasksWithoutWaves(t *testing.T) {
	plan := newPlanCRD("p-pre", "default", "ph-1", "Pending")
	t1 := newTaskCRD("t-pre-1", "default", "p-pre", "Pending", nil, 0)
	t2 := newTaskCRD("t-pre-2", "default", "p-pre", "Pending", nil, 0)

	_, router := newPlansHandler(t, plan, t1, t2)
	srv := httptest.NewServer(router)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/v1/plans/p-pre")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d want 200", resp.StatusCode)
	}
	var body planDetail
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Tasks) != 2 {
		t.Fatalf("tasks len=%d want 2", len(body.Tasks))
	}
	for _, c := range body.Tasks {
		if c.WaveIndex != 0 {
			t.Errorf("task %q waveIndex=%d want 0", c.Name, c.WaveIndex)
		}
	}
	if body.ActiveDispatchWave != nil {
		t.Errorf("activeDispatchWave=%v want nil", *body.ActiveDispatchWave)
	}
}

// TestPlansHandlerActiveDispatchWave covers case 4: one task with
// phase=Running on waveIndex=1 + one with phase=Succeeded on waveIndex=0.
// ActiveDispatchWave should point to the lowest wave with a Dispatching
// or Running task — here wave 1.
func TestPlansHandlerActiveDispatchWave(t *testing.T) {
	plan := newPlanCRD("p-act", "default", "ph-1", "Running")
	tDone := newTaskCRD("t-done", "default", "p-act", "Succeeded", nil, 1)
	tRun := newTaskCRD("t-run", "default", "p-act", "Running", []string{"t-done"}, 1)
	w0 := newWaveCRD("p-act-w0", "default", "p-act", 0, []string{"t-done"})
	w1 := newWaveCRD("p-act-w1", "default", "p-act", 1, []string{"t-run"})

	_, router := newPlansHandler(t, plan, tDone, tRun, w0, w1)
	srv := httptest.NewServer(router)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/v1/plans/p-act")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d want 200", resp.StatusCode)
	}
	var body planDetail
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.ActiveDispatchWave == nil {
		t.Fatalf("activeDispatchWave=nil want *1")
	}
	if *body.ActiveDispatchWave != 1 {
		t.Errorf("activeDispatchWave=%d want 1", *body.ActiveDispatchWave)
	}
}

// TestPlansHandlerPhaseDefaultsPending exercises the empty-phase fallback:
// a Task whose Status.Phase is "" should serialize as "Pending" in the
// response (UI-SPEC §Status Vocabulary default).
func TestPlansHandlerPhaseDefaultsPending(t *testing.T) {
	plan := newPlanCRD("p-empty", "default", "ph-1", "Pending")
	tEmpty := newTaskCRD("t-empty", "default", "p-empty", "", nil, 0)

	_, router := newPlansHandler(t, plan, tEmpty)
	srv := httptest.NewServer(router)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/v1/plans/p-empty")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d want 200", resp.StatusCode)
	}
	var body planDetail
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Tasks) != 1 || body.Tasks[0].Phase != "Pending" {
		t.Errorf("tasks[0].Phase=%q want Pending", body.Tasks[0].Phase)
	}
}
