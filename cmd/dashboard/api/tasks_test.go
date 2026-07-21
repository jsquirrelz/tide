/*
Copyright 2026 TIDE Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

package api

import (
	"bytes"
	"encoding/json"
	"io"
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

	tidev1alpha3 "github.com/jsquirrelz/tide/api/v1alpha3"
	"github.com/jsquirrelz/tide/pkg/otelai"
)

// newTasksHandler returns a TasksHandler with a fake controller-runtime
// client + an optional typed kubernetes.Interface for the pod-resolution
// happy-path test. Pass `cs == nil` to assert the graceful-podName="" path.
func newTasksHandler(t *testing.T, cs kubernetes.Interface, objs ...runtime.Object) (*TasksHandler, http.Handler) {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := tidev1alpha3.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme(v1alpha3): %v", err)
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
	prj := &tidev1alpha3.Project{
		ObjectMeta: metav1.ObjectMeta{Name: "prj-1", Namespace: "default"},
		Spec: tidev1alpha3.ProjectSpec{SchemaRevision: "v1alpha3",
			TargetRepo: "https://example.com/repo.git",
			Budget:     tidev1alpha3.BudgetConfig{AbsoluteCapCents: 10000},
		},
		Status: tidev1alpha3.ProjectStatus{Phase: "Running"},
	}
	ms := &tidev1alpha3.Milestone{
		ObjectMeta: metav1.ObjectMeta{Name: "ms-1", Namespace: "default"},
		Spec:       tidev1alpha3.MilestoneSpec{ProjectRef: "prj-1"},
		Status:     tidev1alpha3.MilestoneStatus{Phase: "Running"},
	}
	ph := &tidev1alpha3.Phase{
		ObjectMeta: metav1.ObjectMeta{Name: "ph-1", Namespace: "default"},
		Spec:       tidev1alpha3.PhaseSpec{MilestoneRef: "ms-1"},
		Status:     tidev1alpha3.PhaseStatus{Phase: "Running"},
	}
	pl := &tidev1alpha3.Plan{
		ObjectMeta: metav1.ObjectMeta{Name: "pl-1", Namespace: "default"},
		Spec:       tidev1alpha3.PlanSpec{PhaseRef: "ph-1"},
		Status:     tidev1alpha3.PlanStatus{Phase: "Running"},
	}
	started := metav1.NewTime(time.Now().Add(-30 * time.Second))
	caps := &tidev1alpha3.Caps{Iterations: 5}
	tk := &tidev1alpha3.Task{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "task-007",
			Namespace: "default",
			UID:       types.UID("task-uid-007"),
		},
		Spec: tidev1alpha3.TaskSpec{
			PlanRef:             "pl-1",
			FilesTouched:        []string{"a.go"},
			DeclaredOutputPaths: []string{"/workspace/a"},
			Caps:                caps,
		},
		Status: tidev1alpha3.TaskStatus{
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
	w0 := &tidev1alpha3.Wave{
		ObjectMeta: metav1.ObjectMeta{Name: "pl-1-w0", Namespace: "default"},
		Spec:       tidev1alpha3.WaveSpec{ProjectRef: "prj-1", WaveIndex: 0},
		Status:     tidev1alpha3.WaveStatus{TaskRefs: []string{"task-pre"}},
	}
	w1 := &tidev1alpha3.Wave{
		ObjectMeta: metav1.ObjectMeta{Name: "pl-1-w1", Namespace: "default"},
		Spec:       tidev1alpha3.WaveSpec{ProjectRef: "prj-1", WaveIndex: 1},
		Status:     tidev1alpha3.WaveStatus{TaskRefs: []string{"task-007"}},
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
	tk := &tidev1alpha3.Task{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "orphan",
			Namespace: "default",
			UID:       types.UID("orphan-uid"),
		},
		Spec: tidev1alpha3.TaskSpec{
			PlanRef:             "missing-plan",
			FilesTouched:        []string{"a.go"},
			DeclaredOutputPaths: []string{"/workspace/a"},
		},
		Status: tidev1alpha3.TaskStatus{Phase: "Pending"},
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
	// D-11: a broken resolution chain degrades trace identity to empty
	// strings too, never a 500.
	if body.TraceID != "" {
		t.Errorf("TraceID=%q want empty (chain break)", body.TraceID)
	}
	if body.TraceSpanID != "" {
		t.Errorf("TraceSpanID=%q want empty (chain break)", body.TraceSpanID)
	}
}

// TestTasksHandlerTraceIdentity covers Phase 46 OBS-04 / D-11: a full
// resolution chain whose Project carries a UID and whose Task carries a
// TaskTraceSpanID status yields both traceId (deterministic derivation
// from the Project's UID via otelai.TraceIDFromUID) and traceSpanId in the
// response — and re-fetching yields the identical traceId (same UID twice
// → same hex).
func TestTasksHandlerTraceIdentity(t *testing.T) {
	objs := newFullChain()
	prj, ok := objs[0].(*tidev1alpha3.Project)
	if !ok {
		t.Fatalf("objs[0] is not *Project")
	}
	prj.UID = types.UID("550e8400-e29b-41d4-a716-446655440000")
	tk, ok := objs[4].(*tidev1alpha3.Task)
	if !ok {
		t.Fatalf("objs[4] is not *Task")
	}
	tk.Status.TaskTraceSpanID = "00f067aa0ba902b7"

	_, router := newTasksHandler(t, nil, objs...)
	srv := httptest.NewServer(router)
	defer srv.Close()

	wantTraceID, err := otelai.TraceIDFromUID(string(prj.UID))
	if err != nil {
		t.Fatalf("TraceIDFromUID: %v", err)
	}

	fetch := func() taskDetail {
		t.Helper()
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
		return body
	}

	body := fetch()
	if body.TraceID != wantTraceID.String() {
		t.Errorf("TraceID=%q want %q", body.TraceID, wantTraceID.String())
	}
	if body.TraceSpanID != "00f067aa0ba902b7" {
		t.Errorf("TraceSpanID=%q want 00f067aa0ba902b7", body.TraceSpanID)
	}

	// Deterministic derivation: same UID resolved twice yields the same hex.
	body2 := fetch()
	if body2.TraceID != body.TraceID {
		t.Errorf("TraceID not deterministic: first=%q second=%q", body.TraceID, body2.TraceID)
	}
}

// TestTasksHandlerExitCode covers case 4: exitCode null vs set. Two
// sub-cases: nil → JSON `null`; set → JSON number.
func TestTasksHandlerExitCode(t *testing.T) {
	tNil := &tidev1alpha3.Task{
		ObjectMeta: metav1.ObjectMeta{
			Name: "t-nilcode", Namespace: "default", UID: types.UID("u-nilcode"),
		},
		Spec: tidev1alpha3.TaskSpec{
			PlanRef:             "missing",
			FilesTouched:        []string{"a.go"},
			DeclaredOutputPaths: []string{"/workspace/a"},
		},
		Status: tidev1alpha3.TaskStatus{Phase: "Running"},
	}
	zero := 0
	tZero := &tidev1alpha3.Task{
		ObjectMeta: metav1.ObjectMeta{
			Name: "t-zerocode", Namespace: "default", UID: types.UID("u-zerocode"),
		},
		Spec: tidev1alpha3.TaskSpec{
			PlanRef:             "missing",
			FilesTouched:        []string{"a.go"},
			DeclaredOutputPaths: []string{"/workspace/a"},
		},
		Status: tidev1alpha3.TaskStatus{Phase: "Succeeded", ExitCode: &zero},
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
	tFinished := &tidev1alpha3.Task{
		ObjectMeta: metav1.ObjectMeta{
			Name: "t-fin", Namespace: "default", UID: types.UID("u-fin"),
		},
		Spec: tidev1alpha3.TaskSpec{
			PlanRef:             "missing",
			FilesTouched:        []string{"a.go"},
			DeclaredOutputPaths: []string{"/workspace/a"},
		},
		Status: tidev1alpha3.TaskStatus{
			Phase:       "Succeeded",
			StartedAt:   &startedFinished,
			CompletedAt: &completed,
		},
	}

	startedRunning := metav1.NewTime(now.Add(-45 * time.Second))
	tRunning := &tidev1alpha3.Task{
		ObjectMeta: metav1.ObjectMeta{
			Name: "t-run", Namespace: "default", UID: types.UID("u-run"),
		},
		Spec: tidev1alpha3.TaskSpec{
			PlanRef:             "missing",
			FilesTouched:        []string{"a.go"},
			DeclaredOutputPaths: []string{"/workspace/a"},
		},
		Status: tidev1alpha3.TaskStatus{
			Phase:     "Running",
			StartedAt: &startedRunning,
		},
	}

	tNeither := &tidev1alpha3.Task{
		ObjectMeta: metav1.ObjectMeta{
			Name: "t-neither", Namespace: "default", UID: types.UID("u-neither"),
		},
		Spec: tidev1alpha3.TaskSpec{
			PlanRef:             "missing",
			FilesTouched:        []string{"a.go"},
			DeclaredOutputPaths: []string{"/workspace/a"},
		},
		Status: tidev1alpha3.TaskStatus{Phase: "Pending"},
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

// TestTasksHandlerLoopProvenance covers Phase 53 D-07/OBS-04: a Task with a
// Locked verification contract + a populated LoopStatus surfaces the full
// loop-provenance summary — hasVerification, loopIteration,
// verifyMaxIterations (from Spec.Verification.MaxIterations, NOT
// Caps.Iterations), the unchanged attemptMax (from Caps.Iterations),
// lastEvaluation's three sub-fields, and derived loopRunId/attemptId. Also
// pins LOOP-03 at the wire level: the raw JSON body never carries an
// iteration-history array or a key matching "history".
func TestTasksHandlerLoopProvenance(t *testing.T) {
	caps := &tidev1alpha3.Caps{Iterations: 5}
	tk := &tidev1alpha3.Task{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "task-loop",
			Namespace: "default",
			UID:       types.UID("task-loop-uid"),
		},
		Spec: tidev1alpha3.TaskSpec{
			PlanRef:             "missing",
			FilesTouched:        []string{"a.go"},
			DeclaredOutputPaths: []string{"/workspace/a"},
			Caps:                caps,
			Verification: tidev1alpha3.VerificationSpec{
				Phase:         "Locked",
				GateCommand:   "make test",
				MaxIterations: 2,
			},
		},
		Status: tidev1alpha3.TaskStatus{
			Phase:   "Repairing",
			Attempt: 1,
			LoopStatus: tidev1alpha3.LoopStatus{
				Iteration:  2,
				ExitReason: "verify_exhausted",
				LastEvaluation: &tidev1alpha3.EvaluationSummary{
					Decision:          "REPAIRABLE",
					FindingsCount:     3,
					HighSeverityCount: 1,
				},
			},
		},
	}
	_, router := newTasksHandler(t, nil, tk)
	srv := httptest.NewServer(router)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/v1/tasks/task-loop")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	rawBody, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}

	var body taskDetail
	if err := json.Unmarshal(rawBody, &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !body.HasVerification {
		t.Errorf("HasVerification=false want true")
	}
	if body.LoopIteration != 2 {
		t.Errorf("LoopIteration=%d want 2", body.LoopIteration)
	}
	if body.VerifyMaxIterations != 2 {
		t.Errorf("VerifyMaxIterations=%d want 2", body.VerifyMaxIterations)
	}
	if body.AttemptMax != 5 {
		t.Errorf("AttemptMax=%d want 5 (unchanged Caps.Iterations source)", body.AttemptMax)
	}
	if body.LoopExitReason != "verify_exhausted" {
		t.Errorf("LoopExitReason=%q want verify_exhausted", body.LoopExitReason)
	}
	if body.LastEvaluation == nil {
		t.Fatalf("LastEvaluation is nil, want populated")
	}
	if body.LastEvaluation.Decision != "REPAIRABLE" || body.LastEvaluation.FindingsCount != 3 || body.LastEvaluation.HighSeverityCount != 1 {
		t.Errorf("LastEvaluation=%+v", body.LastEvaluation)
	}
	if body.LoopRunID != "task-loop-uid" {
		t.Errorf("LoopRunID=%q want task-loop-uid", body.LoopRunID)
	}
	if body.AttemptID != "task-loop-uid-1" {
		t.Errorf("AttemptID=%q want task-loop-uid-1", body.AttemptID)
	}

	// LOOP-03 wire-level pin: no key matching "history" and no array of
	// iterations anywhere in the raw body.
	if bytes.Contains(bytes.ToLower(rawBody), []byte("history")) {
		t.Errorf("response body contains a \"history\" token, want none (LOOP-03): %s", rawBody)
	}
}

// TestTasksHandlerLoopProvenanceEffectiveMaxIterations covers WR-01 (Phase
// 53 code review): a Task whose authored maxIterations is unset (0 — the
// chart tier or compiled floor governs the loop) surfaces the
// CONTROLLER-STAMPED effective bound from
// Status.LoopStatus.EffectiveMaxIterations, never the raw authored 0 that
// rendered "Iteration 2 of 0" in the drawer. The authored value remains the
// fallback when the stamp is absent (pinned by
// TestTasksHandlerLoopProvenance above, whose fixture has no stamp).
func TestTasksHandlerLoopProvenanceEffectiveMaxIterations(t *testing.T) {
	tk := &tidev1alpha3.Task{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "task-effective",
			Namespace: "default",
			UID:       types.UID("task-effective-uid"),
		},
		Spec: tidev1alpha3.TaskSpec{
			PlanRef:             "missing",
			FilesTouched:        []string{"a.go"},
			DeclaredOutputPaths: []string{"/workspace/a"},
			Verification: tidev1alpha3.VerificationSpec{
				Phase:       "Locked",
				GateCommand: "make test",
				// MaxIterations deliberately unset (0): the chart tier supplies
				// the real bound, which the controller stamped below.
			},
		},
		Status: tidev1alpha3.TaskStatus{
			Phase:   "Verifying",
			Attempt: 2,
			LoopStatus: tidev1alpha3.LoopStatus{
				Iteration:              2,
				EffectiveMaxIterations: 3,
			},
		},
	}
	_, router := newTasksHandler(t, nil, tk)
	srv := httptest.NewServer(router)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/v1/tasks/task-effective")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	var body taskDetail
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.VerifyMaxIterations != 3 {
		t.Errorf("VerifyMaxIterations=%d want 3 (the stamped effective bound, not the authored 0)", body.VerifyMaxIterations)
	}
	if body.LoopIteration != 2 {
		t.Errorf("LoopIteration=%d want 2", body.LoopIteration)
	}
}

// TestTasksHandlerLoopProvenanceAbsentWithoutContract covers the omitempty +
// gating half of D-07: a Task with no verification contract (zero-value
// Spec.Verification, zero-value Status.LoopStatus) emits none of the new
// loop-provenance keys in the JSON body.
func TestTasksHandlerLoopProvenanceAbsentWithoutContract(t *testing.T) {
	tk := &tidev1alpha3.Task{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "task-nocontract",
			Namespace: "default",
			UID:       types.UID("task-nocontract-uid"),
		},
		Spec: tidev1alpha3.TaskSpec{
			PlanRef:             "missing",
			FilesTouched:        []string{"a.go"},
			DeclaredOutputPaths: []string{"/workspace/a"},
		},
		Status: tidev1alpha3.TaskStatus{Phase: "Running"},
	}
	_, router := newTasksHandler(t, nil, tk)
	srv := httptest.NewServer(router)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/v1/tasks/task-nocontract")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	rawBody, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}

	var m map[string]any
	if err := json.Unmarshal(rawBody, &m); err != nil {
		t.Fatalf("decode: %v", err)
	}
	for _, key := range []string{
		"hasVerification", "loopIteration", "verifyMaxIterations",
		"loopExitReason", "lastEvaluation", "loopRunId", "attemptId",
	} {
		if _, present := m[key]; present {
			t.Errorf("key %q present in body, want absent (omitempty on zero-value loop state): %s", key, rawBody)
		}
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
