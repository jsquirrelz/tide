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
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-logr/logr/testr"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	fakeclientset "k8s.io/client-go/kubernetes/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	tidev1alpha1 "github.com/jsquirrelz/tide/api/v1alpha2"
)

// newTasksHandler returns a TasksHandler with a fake controller-runtime
// client + an optional typed kubernetes.Interface for the pod-resolution
// happy-path test. Pass `cs == nil` to assert the graceful-podName="" path.
func newTasksHandler(t *testing.T, cs kubernetes.Interface, objs ...runtime.Object) (*TasksHandler, http.Handler) {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := tidev1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme(v1alpha1): %v", err)
	}
	builder := fake.NewClientBuilder().WithScheme(scheme)
	for _, o := range objs {
		builder = builder.WithRuntimeObjects(o)
	}
	c := builder.Build()
	h := &TasksHandler{Client: c, Clientset: cs, Log: testr.New(t)}

	r := chi.NewRouter()
	r.Route("/api/v1", func(r chi.Router) {
		r.Get("/tasks/{name}", h.Get)
	})
	return h, r
}

// newFullChain materializes a Project → Milestone → Phase → Plan → Task
// resolution chain (and a matching Wave that places the Task on waveIndex 1).
// Used by the happy-path test to exercise every rich-detail field.
func newFullChain() []runtime.Object {
	prj := &tidev1alpha1.Project{
		ObjectMeta: metav1.ObjectMeta{Name: "prj-1", Namespace: "default"},
		Spec: tidev1alpha1.ProjectSpec{
			TargetRepo: "https://example.com/repo.git",
			Budget:     tidev1alpha1.BudgetConfig{AbsoluteCapCents: 10000},
		},
		Status: tidev1alpha1.ProjectStatus{Phase: "Running"},
	}
	ms := &tidev1alpha1.Milestone{
		ObjectMeta: metav1.ObjectMeta{Name: "ms-1", Namespace: "default"},
		Spec:       tidev1alpha1.MilestoneSpec{ProjectRef: "prj-1"},
		Status:     tidev1alpha1.MilestoneStatus{Phase: "Running"},
	}
	ph := &tidev1alpha1.Phase{
		ObjectMeta: metav1.ObjectMeta{Name: "ph-1", Namespace: "default"},
		Spec:       tidev1alpha1.PhaseSpec{MilestoneRef: "ms-1"},
		Status:     tidev1alpha1.PhaseStatus{Phase: "Running"},
	}
	pl := &tidev1alpha1.Plan{
		ObjectMeta: metav1.ObjectMeta{Name: "pl-1", Namespace: "default"},
		Spec:       tidev1alpha1.PlanSpec{PhaseRef: "ph-1"},
		Status:     tidev1alpha1.PlanStatus{Phase: "Running"},
	}
	started := metav1.NewTime(time.Now().Add(-30 * time.Second))
	caps := &tidev1alpha1.Caps{Iterations: 5}
	tk := &tidev1alpha1.Task{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "task-007",
			Namespace: "default",
			UID:       types.UID("task-uid-007"),
		},
		Spec: tidev1alpha1.TaskSpec{
			PlanRef:             "pl-1",
			FilesTouched:        []string{"a.go"},
			DeclaredOutputPaths: []string{"/workspace/a"},
			Caps:                caps,
		},
		Status: tidev1alpha1.TaskStatus{
			Phase:     "Running",
			Attempt:   2,
			StartedAt: &started,
			Conditions: []metav1.Condition{
				{
					Type:               "Ready",
					Reason:             "PodRunning",
					Status:             metav1.ConditionTrue,
					LastTransitionTime: metav1.NewTime(time.Now().Add(-10 * time.Second)),
				},
			},
		},
	}
	w0 := &tidev1alpha1.Wave{
		ObjectMeta: metav1.ObjectMeta{Name: "pl-1-w0", Namespace: "default"},
		Spec:       tidev1alpha1.WaveSpec{PlanRef: "pl-1", WaveIndex: 0},
		Status:     tidev1alpha1.WaveStatus{TaskRefs: []string{"task-pre"}},
	}
	w1 := &tidev1alpha1.Wave{
		ObjectMeta: metav1.ObjectMeta{Name: "pl-1-w1", Namespace: "default"},
		Spec:       tidev1alpha1.WaveSpec{PlanRef: "pl-1", WaveIndex: 1},
		Status:     tidev1alpha1.WaveStatus{TaskRefs: []string{"task-007"}},
	}
	return []runtime.Object{prj, ms, ph, pl, tk, w0, w1}
}

// TestTasksHandlerHappyPath covers case 1: Task + Plan + Phase + Milestone
// + Project chain + Wave with TaskRefs → all rich fields populated
// including waveIndex, projectName, planName, envelopePath. Uses
// k8s.io/client-go/kubernetes/fake for the Pod-list path.
func TestTasksHandlerHappyPath(t *testing.T) {
	objs := newFullChain()

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "task-007-pod",
			Namespace: "default",
			Labels:    map[string]string{"tideproject.k8s/task-uid": "task-uid-007"},
		},
		Status: corev1.PodStatus{Phase: corev1.PodRunning},
	}
	cs := fakeclientset.NewSimpleClientset(pod)

	_, router := newTasksHandler(t, cs, objs...)
	srv := httptest.NewServer(router)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/v1/tasks/task-007")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d want 200", resp.StatusCode)
	}
	var body taskDetail
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Name != "task-007" {
		t.Errorf("Name=%q want task-007", body.Name)
	}
	if body.Status != "Running" {
		t.Errorf("Status=%q want Running", body.Status)
	}
	if body.Attempt != 2 {
		t.Errorf("Attempt=%d want 2", body.Attempt)
	}
	if body.AttemptMax != 5 {
		t.Errorf("AttemptMax=%d want 5", body.AttemptMax)
	}
	if body.WaveIndex != 1 {
		t.Errorf("WaveIndex=%d want 1", body.WaveIndex)
	}
	if body.PodName != "task-007-pod" {
		t.Errorf("PodName=%q want task-007-pod", body.PodName)
	}
	if body.EnvelopePath != "/workspace/envelopes/task-uid-007/result.json" {
		t.Errorf("EnvelopePath=%q want /workspace/envelopes/task-uid-007/result.json", body.EnvelopePath)
	}
	if body.PlanName != "pl-1" {
		t.Errorf("PlanName=%q want pl-1", body.PlanName)
	}
	if body.ProjectName != "prj-1" {
		t.Errorf("ProjectName=%q want prj-1", body.ProjectName)
	}
	if body.ScheduledAt == "" {
		t.Errorf("ScheduledAt is empty; expected RFC3339")
	}
	if body.ElapsedText == "" {
		t.Errorf("ElapsedText is empty; expected 'running for ...'")
	}
	if len(body.Conditions) != 1 {
		t.Fatalf("Conditions len=%d want 1", len(body.Conditions))
	}
	if body.Conditions[0].Type != "Ready" || body.Conditions[0].Reason != "PodRunning" {
		t.Errorf("Conditions[0]=%+v", body.Conditions[0])
	}
	if body.Conditions[0].Age == "" {
		t.Errorf("Conditions[0].Age is empty")
	}
	if body.ExitCode != nil {
		t.Errorf("ExitCode=%v want nil", body.ExitCode)
	}
}

// TestTasksHandlerNotFound covers case 2: Task not found → 404.
func TestTasksHandlerNotFound(t *testing.T) {
	_, router := newTasksHandler(t, nil)
	srv := httptest.NewServer(router)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/v1/tasks/missing")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status=%d want 404", resp.StatusCode)
	}
}

// TestTasksHandlerResolutionChainBreak covers case 3: Task with PlanRef
// pointing to a missing Plan → projectName="", planName="" — 200 with
// graceful degradation (NOT 500).
func TestTasksHandlerResolutionChainBreak(t *testing.T) {
	tk := &tidev1alpha1.Task{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "orphan",
			Namespace: "default",
			UID:       types.UID("orphan-uid"),
		},
		Spec: tidev1alpha1.TaskSpec{
			PlanRef:             "missing-plan",
			FilesTouched:        []string{"a.go"},
			DeclaredOutputPaths: []string{"/workspace/a"},
		},
		Status: tidev1alpha1.TaskStatus{Phase: "Pending"},
	}
	_, router := newTasksHandler(t, nil, tk)
	srv := httptest.NewServer(router)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/v1/tasks/orphan")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status=%d want 200 (graceful)", resp.StatusCode)
	}
	var body taskDetail
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.PlanName != "" {
		t.Errorf("PlanName=%q want empty (chain break)", body.PlanName)
	}
	if body.ProjectName != "" {
		t.Errorf("ProjectName=%q want empty (chain break)", body.ProjectName)
	}
	// AttemptMax defaults to 1 (no Caps set).
	if body.AttemptMax != 1 {
		t.Errorf("AttemptMax=%d want 1 (no Caps)", body.AttemptMax)
	}
}

// TestTasksHandlerExitCode covers case 4: exitCode null vs set. Two
// sub-cases: nil → JSON `null`; set → JSON number.
func TestTasksHandlerExitCode(t *testing.T) {
	tNil := &tidev1alpha1.Task{
		ObjectMeta: metav1.ObjectMeta{
			Name: "t-nilcode", Namespace: "default", UID: types.UID("u-nilcode"),
		},
		Spec: tidev1alpha1.TaskSpec{
			PlanRef:             "missing",
			FilesTouched:        []string{"a.go"},
			DeclaredOutputPaths: []string{"/workspace/a"},
		},
		Status: tidev1alpha1.TaskStatus{Phase: "Running"},
	}
	zero := 0
	tZero := &tidev1alpha1.Task{
		ObjectMeta: metav1.ObjectMeta{
			Name: "t-zerocode", Namespace: "default", UID: types.UID("u-zerocode"),
		},
		Spec: tidev1alpha1.TaskSpec{
			PlanRef:             "missing",
			FilesTouched:        []string{"a.go"},
			DeclaredOutputPaths: []string{"/workspace/a"},
		},
		Status: tidev1alpha1.TaskStatus{Phase: "Succeeded", ExitCode: &zero},
	}

	_, router := newTasksHandler(t, nil, tNil, tZero)
	srv := httptest.NewServer(router)
	defer srv.Close()

	// First: nil ExitCode → JSON null.
	respNil, err := http.Get(srv.URL + "/api/v1/tasks/t-nilcode")
	if err != nil {
		t.Fatalf("GET nilcode: %v", err)
	}
	defer respNil.Body.Close()
	var bNil map[string]any
	if err := json.NewDecoder(respNil.Body).Decode(&bNil); err != nil {
		t.Fatalf("decode nilcode: %v", err)
	}
	// JSON null → Go any => nil.
	if v, ok := bNil["exitCode"]; !ok {
		t.Errorf("exitCode key missing in body")
	} else if v != nil {
		t.Errorf("exitCode=%v (type %T) want nil", v, v)
	}

	// Second: ExitCode == 0 → JSON number 0.
	respZero, err := http.Get(srv.URL + "/api/v1/tasks/t-zerocode")
	if err != nil {
		t.Fatalf("GET zerocode: %v", err)
	}
	defer respZero.Body.Close()
	var bZero map[string]any
	if err := json.NewDecoder(respZero.Body).Decode(&bZero); err != nil {
		t.Fatalf("decode zerocode: %v", err)
	}
	v, ok := bZero["exitCode"]
	if !ok {
		t.Fatalf("exitCode key missing in zerocode body")
	}
	// JSON number → float64.
	if f, ok := v.(float64); !ok || f != 0 {
		t.Errorf("exitCode=%v (type %T) want 0", v, v)
	}
}

// TestTasksHandlerElapsedText covers case 5: three elapsedText shapes.
//   - StartedAt+CompletedAt → "finished in …"
//   - StartedAt only        → "running for …"
//   - neither               → ""
func TestTasksHandlerElapsedText(t *testing.T) {
	now := time.Now()
	startedFinished := metav1.NewTime(now.Add(-90 * time.Second))
	completed := metav1.NewTime(now.Add(-5 * time.Second))
	tFinished := &tidev1alpha1.Task{
		ObjectMeta: metav1.ObjectMeta{
			Name: "t-fin", Namespace: "default", UID: types.UID("u-fin"),
		},
		Spec: tidev1alpha1.TaskSpec{
			PlanRef:             "missing",
			FilesTouched:        []string{"a.go"},
			DeclaredOutputPaths: []string{"/workspace/a"},
		},
		Status: tidev1alpha1.TaskStatus{
			Phase:       "Succeeded",
			StartedAt:   &startedFinished,
			CompletedAt: &completed,
		},
	}

	startedRunning := metav1.NewTime(now.Add(-45 * time.Second))
	tRunning := &tidev1alpha1.Task{
		ObjectMeta: metav1.ObjectMeta{
			Name: "t-run", Namespace: "default", UID: types.UID("u-run"),
		},
		Spec: tidev1alpha1.TaskSpec{
			PlanRef:             "missing",
			FilesTouched:        []string{"a.go"},
			DeclaredOutputPaths: []string{"/workspace/a"},
		},
		Status: tidev1alpha1.TaskStatus{
			Phase:     "Running",
			StartedAt: &startedRunning,
		},
	}

	tNeither := &tidev1alpha1.Task{
		ObjectMeta: metav1.ObjectMeta{
			Name: "t-neither", Namespace: "default", UID: types.UID("u-neither"),
		},
		Spec: tidev1alpha1.TaskSpec{
			PlanRef:             "missing",
			FilesTouched:        []string{"a.go"},
			DeclaredOutputPaths: []string{"/workspace/a"},
		},
		Status: tidev1alpha1.TaskStatus{Phase: "Pending"},
	}

	_, router := newTasksHandler(t, nil, tFinished, tRunning, tNeither)
	srv := httptest.NewServer(router)
	defer srv.Close()

	for _, tc := range []struct {
		name        string
		path        string
		wantSubstr  string
		expectEmpty bool
	}{
		{"finished", "/api/v1/tasks/t-fin", "finished in", false},
		{"running", "/api/v1/tasks/t-run", "running for", false},
		{"neither", "/api/v1/tasks/t-neither", "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resp, err := http.Get(srv.URL + tc.path)
			if err != nil {
				t.Fatalf("GET: %v", err)
			}
			defer resp.Body.Close()
			var body taskDetail
			if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
				t.Fatalf("decode: %v", err)
			}
			if tc.expectEmpty {
				if body.ElapsedText != "" {
					t.Errorf("ElapsedText=%q want empty", body.ElapsedText)
				}
				return
			}
			if body.ElapsedText == "" || !contains(body.ElapsedText, tc.wantSubstr) {
				t.Errorf("ElapsedText=%q want substring %q", body.ElapsedText, tc.wantSubstr)
			}
		})
	}
}

// contains is a tiny strings.Contains shim — avoids importing strings just
// for one helper to satisfy goimports's "minimal imports" preference in the
// tests file.
func contains(s, substr string) bool {
	if substr == "" {
		return true
	}
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
