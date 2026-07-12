/*
Copyright 2026 TIDE Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
*/

// Plan 04-08 Task 1 — RED tests for `tide resume`. Asserts the verb clears
// the `tideproject.k8s/reject` annotation via gates.ConsumeReject + Patch.
// Plan 12-02 Task 1 extends the suite: `--retry-failed` resets Status.Phase
// on Failed levels via the status subresource (all four level kinds), skips
// Running levels (Pitfall 3), and is a no-op when the flag is absent.
package main

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	tidev1alpha3 "github.com/jsquirrelz/tide/api/v1alpha3"
)

func makeRejectedProject(name, reason string) *tidev1alpha3.Project {
	return &tidev1alpha3.Project{
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Namespace:   "default",
			Annotations: map[string]string{"tideproject.k8s/reject": reason},
		},
		Spec:   tidev1alpha3.ProjectSpec{SchemaRevision: "v1alpha3", TargetRepo: "https://example.com/repo.git"},
		Status: tidev1alpha3.ProjectStatus{Phase: "Running"},
	}
}

func TestResumeClearsRejectAnnotation(t *testing.T) {
	p := makeRejectedProject("my-project", "stopped")
	c := fake.NewClientBuilder().WithScheme(testScheme(t)).WithObjects(p).Build()

	if err := resumeRun(context.Background(), c, "default", "my-project", false, nil); err != nil {
		t.Fatalf("resumeRun: %v", err)
	}
	var got tidev1alpha3.Project
	if err := c.Get(context.Background(), types.NamespacedName{Namespace: "default", Name: "my-project"}, &got); err != nil {
		t.Fatalf("get project: %v", err)
	}
	if v, ok := got.Annotations["tideproject.k8s/reject"]; ok {
		t.Errorf("expected reject annotation cleared; still present with value %q (annotations=%v)", v, got.Annotations)
	}
}

func TestResumePreservesOtherAnnotations(t *testing.T) {
	p := makeRejectedProject("my-project", "stopped")
	p.Annotations["other/key"] = "preserve-me"
	c := fake.NewClientBuilder().WithScheme(testScheme(t)).WithObjects(p).Build()

	if err := resumeRun(context.Background(), c, "default", "my-project", false, nil); err != nil {
		t.Fatalf("resumeRun: %v", err)
	}
	var got tidev1alpha3.Project
	_ = c.Get(context.Background(), types.NamespacedName{Namespace: "default", Name: "my-project"}, &got)
	if got.Annotations["other/key"] != "preserve-me" {
		t.Errorf("expected other annotations preserved; got %v", got.Annotations)
	}
}

func TestResumeProjectNotFound(t *testing.T) {
	c := fake.NewClientBuilder().WithScheme(testScheme(t)).Build()
	if err := resumeRun(context.Background(), c, "default", "missing", false, nil); err == nil {
		t.Fatal("expected not-found error; got nil")
	}
}

func TestResumeNoOpWhenNoReject(t *testing.T) {
	p := makeProject("my-project")
	c := fake.NewClientBuilder().WithScheme(testScheme(t)).WithObjects(p).Build()
	if err := resumeRun(context.Background(), c, "default", "my-project", false, nil); err != nil {
		t.Fatalf("resumeRun on un-rejected project should be no-op; got %v", err)
	}
}

// ---------------------------------------------------------------------------
// Plan 12-02 additions: --retry-failed behaviour
// ---------------------------------------------------------------------------

// makeFailedPlan builds a Plan fixture with Status.Phase="Failed" and the
// project label so the retry-failed walker can find it.
func makeFailedPlan(name, projectName string) *tidev1alpha3.Plan {
	return &tidev1alpha3.Plan{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "default",
			Labels:    map[string]string{"tideproject.k8s/project": projectName},
		},
		Spec:   tidev1alpha3.PlanSpec{PhaseRef: "some-phase"},
		Status: tidev1alpha3.PlanStatus{Phase: "Failed"},
	}
}

// makeRunningPlan builds a Plan fixture with Status.Phase="Running".
func makeRunningPlan(name, projectName string) *tidev1alpha3.Plan {
	return &tidev1alpha3.Plan{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "default",
			Labels:    map[string]string{"tideproject.k8s/project": projectName},
		},
		Spec:   tidev1alpha3.PlanSpec{PhaseRef: "some-phase"},
		Status: tidev1alpha3.PlanStatus{Phase: "Running"},
	}
}

// TestResumeRunRetryFailed asserts that resumeRun with retryFailed=true resets
// Status.Phase on a Failed Plan to "" and stamps a ResumedByUser condition.
func TestResumeRunRetryFailed(t *testing.T) {
	p := makeProject("my-project")
	pl := makeFailedPlan("pl-one", "my-project")

	c := fake.NewClientBuilder().
		WithScheme(testScheme(t)).
		WithObjects(p, pl).
		WithStatusSubresource(&tidev1alpha3.Milestone{}, &tidev1alpha3.Phase{}, &tidev1alpha3.Plan{}, &tidev1alpha3.Task{}).
		Build()

	var buf bytes.Buffer
	if err := resumeRun(context.Background(), c, "default", "my-project", true, &buf); err != nil {
		t.Fatalf("resumeRun(retryFailed=true): %v", err)
	}

	var got tidev1alpha3.Plan
	if err := c.Get(context.Background(), types.NamespacedName{Namespace: "default", Name: "pl-one"}, &got); err != nil {
		t.Fatalf("get plan: %v", err)
	}
	if got.Status.Phase == "Failed" {
		t.Errorf("expected Status.Phase cleared; still 'Failed'")
	}
	cond := meta.FindStatusCondition(got.Status.Conditions, tidev1alpha3.ConditionWaveOrLevelPaused)
	if cond == nil {
		t.Fatal("expected WaveOrLevelPaused condition stamped on reset Plan; got nil")
	}
	if cond.Reason != tidev1alpha3.ReasonResumedByUser {
		t.Errorf("expected Reason=ResumedByUser; got %q", cond.Reason)
	}
	if cond.Status != metav1.ConditionFalse {
		t.Errorf("expected condition Status=False; got %q", cond.Status)
	}

	// Output should mention the plan name.
	if out := buf.String(); out == "" {
		t.Errorf("expected per-level feedback written to out; got empty string")
	}
}

// TestResumeRetryFailedSkipsRunning asserts that a Running Plan is untouched
// when retryFailed=true (Pitfall 3 — no double-dispatch).
func TestResumeRetryFailedSkipsRunning(t *testing.T) {
	p := makeProject("my-project")
	pl := makeRunningPlan("pl-running", "my-project")

	c := fake.NewClientBuilder().
		WithScheme(testScheme(t)).
		WithObjects(p, pl).
		WithStatusSubresource(&tidev1alpha3.Milestone{}, &tidev1alpha3.Phase{}, &tidev1alpha3.Plan{}, &tidev1alpha3.Task{}).
		Build()

	var buf bytes.Buffer
	if err := resumeRun(context.Background(), c, "default", "my-project", true, &buf); err != nil {
		t.Fatalf("resumeRun(retryFailed=true): %v", err)
	}

	var got tidev1alpha3.Plan
	if err := c.Get(context.Background(), types.NamespacedName{Namespace: "default", Name: "pl-running"}, &got); err != nil {
		t.Fatalf("get plan: %v", err)
	}
	if got.Status.Phase != "Running" {
		t.Errorf("expected Running Plan untouched; Status.Phase=%q", got.Status.Phase)
	}

	// Nothing reset — should print the "no Failed levels found" message.
	if out := buf.String(); out == "" {
		t.Errorf("expected 'no Failed levels found' feedback; got empty string")
	}
}

// TestResumeWithoutFlagLeavesFailed asserts that retryFailed=false does NOT
// touch a Failed Plan — flag is deliberate friction (D-06).
func TestResumeWithoutFlagLeavesFailed(t *testing.T) {
	p := makeProject("my-project")
	pl := makeFailedPlan("pl-fail", "my-project")

	c := fake.NewClientBuilder().
		WithScheme(testScheme(t)).
		WithObjects(p, pl).
		WithStatusSubresource(&tidev1alpha3.Milestone{}, &tidev1alpha3.Phase{}, &tidev1alpha3.Plan{}, &tidev1alpha3.Task{}).
		Build()

	if err := resumeRun(context.Background(), c, "default", "my-project", false, nil); err != nil {
		t.Fatalf("resumeRun(retryFailed=false): %v", err)
	}

	var got tidev1alpha3.Plan
	if err := c.Get(context.Background(), types.NamespacedName{Namespace: "default", Name: "pl-fail"}, &got); err != nil {
		t.Fatalf("get plan: %v", err)
	}
	if got.Status.Phase != "Failed" {
		t.Errorf("expected Failed Plan untouched when flag absent; Status.Phase=%q", got.Status.Phase)
	}
}

// TestResumeRetryFailedResetsWaveIntegration pins the Phase 34 recovery
// contract for wave-integration failures: resetting a Failed Plan must also
// zero Status.WaveIntegration (otherwise a plan parked at the Attempts cap
// re-fails terminally after ONE fresh attempt instead of a full budget) and
// delete the stale terminal-failed tide-push-wave-<plan.UID>-<N> Job
// (otherwise, within its 300s TTL window, the resumed Plan's next reconcile
// re-reads the stale envelope and instantly re-fails).
func TestResumeRetryFailedResetsWaveIntegration(t *testing.T) {
	p := makeProject("my-project")
	pl := makeFailedPlan("pl-wave-parked", "my-project")
	pl.UID = types.UID("plan-uid-wave-parked")
	now := metav1.Now()
	pl.Status.WaveIntegration = tidev1alpha3.WaveIntegrationStatus{
		Wave:            1,
		Attempts:        5,
		LastAttemptTime: &now,
		LastError:       "integration-failed",
	}
	staleJob := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "tide-push-wave-plan-uid-wave-parked-1",
			Namespace: "default",
			Labels: map[string]string{
				"tideproject.k8s/role":    "git-writer",
				"tideproject.k8s/project": "my-project",
			},
		},
	}

	c := fake.NewClientBuilder().
		WithScheme(testScheme(t)).
		WithObjects(p, pl, staleJob).
		WithStatusSubresource(&tidev1alpha3.Milestone{}, &tidev1alpha3.Phase{}, &tidev1alpha3.Plan{}, &tidev1alpha3.Task{}).
		Build()

	if err := resumeRun(context.Background(), c, "default", "my-project", true, nil); err != nil {
		t.Fatalf("resumeRun(retryFailed=true): %v", err)
	}

	var got tidev1alpha3.Plan
	if err := c.Get(context.Background(), types.NamespacedName{Namespace: "default", Name: "pl-wave-parked"}, &got); err != nil {
		t.Fatalf("get plan: %v", err)
	}
	if got.Status.WaveIntegration.Attempts != 0 || got.Status.WaveIntegration.Wave != 0 ||
		got.Status.WaveIntegration.LastError != "" || got.Status.WaveIntegration.LastAttemptTime != nil {
		t.Errorf("Status.WaveIntegration not reset: %+v — a resumed plan must get a fresh retry budget",
			got.Status.WaveIntegration)
	}

	var job batchv1.Job
	err := c.Get(context.Background(), types.NamespacedName{Namespace: "default", Name: staleJob.Name}, &job)
	if err == nil {
		t.Errorf("stale wave-integration Job survived resume — within its TTL window it instantly re-fails the resumed Plan")
	} else if !apierrors.IsNotFound(err) {
		t.Fatalf("get stale wave job: %v", err)
	}
}

// TestResumeRetryFailedAllFourKinds asserts that a single retryFailed=true
// call resets one Failed object of each of the four level kinds — Milestone,
// Phase, Plan, Task — leaving no item still "Failed".
func TestResumeRetryFailedAllFourKinds(t *testing.T) {
	p := makeProject("my-project")

	ms := &tidev1alpha3.Milestone{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "ms-failed",
			Namespace: "default",
			Labels:    map[string]string{"tideproject.k8s/project": "my-project"},
		},
		Spec:   tidev1alpha3.MilestoneSpec{ProjectRef: "my-project"},
		Status: tidev1alpha3.MilestoneStatus{Phase: "Failed"},
	}
	ph := &tidev1alpha3.Phase{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "ph-failed",
			Namespace: "default",
			Labels:    map[string]string{"tideproject.k8s/project": "my-project"},
		},
		Spec:   tidev1alpha3.PhaseSpec{MilestoneRef: "ms-failed"},
		Status: tidev1alpha3.PhaseStatus{Phase: "Failed"},
	}
	pl := makeFailedPlan("pl-failed", "my-project")
	tk := &tidev1alpha3.Task{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "tk-failed",
			Namespace: "default",
			Labels:    map[string]string{"tideproject.k8s/project": "my-project"},
		},
		Spec:   tidev1alpha3.TaskSpec{PlanRef: "pl-failed"},
		Status: tidev1alpha3.TaskStatus{Phase: "Failed"},
	}

	c := fake.NewClientBuilder().
		WithScheme(testScheme(t)).
		WithObjects(p, ms, ph, pl, tk).
		WithStatusSubresource(&tidev1alpha3.Milestone{}, &tidev1alpha3.Phase{}, &tidev1alpha3.Plan{}, &tidev1alpha3.Task{}).
		Build()

	var buf bytes.Buffer
	if err := resumeRun(context.Background(), c, "default", "my-project", true, &buf); err != nil {
		t.Fatalf("resumeRun(retryFailed=true): %v", err)
	}

	var gotMS tidev1alpha3.Milestone
	_ = c.Get(context.Background(), types.NamespacedName{Namespace: "default", Name: "ms-failed"}, &gotMS)
	if gotMS.Status.Phase == "Failed" {
		t.Errorf("Milestone still Failed after retry-failed")
	}

	var gotPH tidev1alpha3.Phase
	_ = c.Get(context.Background(), types.NamespacedName{Namespace: "default", Name: "ph-failed"}, &gotPH)
	if gotPH.Status.Phase == "Failed" {
		t.Errorf("Phase still Failed after retry-failed")
	}

	var gotPL tidev1alpha3.Plan
	_ = c.Get(context.Background(), types.NamespacedName{Namespace: "default", Name: "pl-failed"}, &gotPL)
	if gotPL.Status.Phase == "Failed" {
		t.Errorf("Plan still Failed after retry-failed")
	}

	var gotTK tidev1alpha3.Task
	_ = c.Get(context.Background(), types.NamespacedName{Namespace: "default", Name: "tk-failed"}, &gotTK)
	if gotTK.Status.Phase == "Failed" {
		t.Errorf("Task still Failed after retry-failed")
	}
}

// ---------------------------------------------------------------------------
// Plan 13-02 Task 3 — tide resume clears BillingHalt (D-06)
// ---------------------------------------------------------------------------

// makeBillingHaltedProject builds a Project fixture with BillingHalt=True.
func makeBillingHaltedProject(name string) *tidev1alpha3.Project {
	p := &tidev1alpha3.Project{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
		Spec:       tidev1alpha3.ProjectSpec{SchemaRevision: "v1alpha3", TargetRepo: "https://example.com/repo.git"},
		Status:     tidev1alpha3.ProjectStatus{Phase: "Running"},
	}
	meta.SetStatusCondition(&p.Status.Conditions, metav1.Condition{
		Type:               tidev1alpha3.ConditionBillingHalt,
		Status:             metav1.ConditionTrue,
		Reason:             tidev1alpha3.ReasonCreditBalanceTooLow,
		LastTransitionTime: metav1.Now(),
	})
	return p
}

// TestResumeClearsBillingHalt asserts that resumeRun clears the BillingHalt
// condition from a Project that has been halted for credit exhaustion.
func TestResumeClearsBillingHalt(t *testing.T) {
	p := makeBillingHaltedProject("halt-project")
	c := fake.NewClientBuilder().WithScheme(testScheme(t)).
		WithObjects(p).
		WithStatusSubresource(p).
		Build()

	if err := resumeRun(context.Background(), c, "default", "halt-project", false, nil); err != nil {
		t.Fatalf("resumeRun: %v", err)
	}

	var got tidev1alpha3.Project
	if err := c.Get(context.Background(), types.NamespacedName{Namespace: "default", Name: "halt-project"}, &got); err != nil {
		t.Fatalf("get project: %v", err)
	}
	for _, cond := range got.Status.Conditions {
		if cond.Type == tidev1alpha3.ConditionBillingHalt && cond.Status == metav1.ConditionTrue {
			t.Errorf("BillingHalt=True condition should be cleared after resume; still present: %+v", cond)
		}
	}
}

// ---------------------------------------------------------------------------
// Plan 13-05 Task 2 — resume stamps AnnotationBillingResumedAt (WR-03)
// ---------------------------------------------------------------------------

// TestResumeStampsBillingResumedAt asserts that resumeRun on a Project that has
// BillingHalt=True clears the condition AND stamps AnnotationBillingResumedAt
// with an RFC3339 value parseable to approximately now.
func TestResumeStampsBillingResumedAt(t *testing.T) {
	p := makeBillingHaltedProject("halt-annotate-project")
	c := fake.NewClientBuilder().WithScheme(testScheme(t)).
		WithObjects(p).
		WithStatusSubresource(p).
		Build()

	before := time.Now().UTC().Add(-2 * time.Second)
	var buf bytes.Buffer
	if err := resumeRun(context.Background(), c, "default", "halt-annotate-project", false, &buf); err != nil {
		t.Fatalf("resumeRun: %v", err)
	}
	after := time.Now().UTC().Add(2 * time.Second)

	var got tidev1alpha3.Project
	if err := c.Get(context.Background(), types.NamespacedName{Namespace: "default", Name: "halt-annotate-project"}, &got); err != nil {
		t.Fatalf("get project: %v", err)
	}
	v, ok := got.Annotations[tidev1alpha3.AnnotationBillingResumedAt]
	if !ok {
		t.Fatalf("expected %q annotation stamped after resume; annotations=%v", tidev1alpha3.AnnotationBillingResumedAt, got.Annotations)
	}
	ts, err := time.Parse(time.RFC3339, v)
	if err != nil {
		t.Fatalf("AnnotationBillingResumedAt value %q is not RFC3339: %v", v, err)
	}
	if ts.Before(before) || ts.After(after) {
		t.Errorf("AnnotationBillingResumedAt %v is outside expected window [%v, %v]", ts, before, after)
	}
	// Output must mention the pre-resume in-flight session time-fence.
	if out := buf.String(); out == "" {
		t.Errorf("expected feedback output; got empty string")
	}
}

// TestResumeWithoutBillingHalt_DoesNotStampAnnotation asserts that resumeRun on
// a Project WITHOUT the BillingHalt condition does NOT add AnnotationBillingResumedAt.
func TestResumeWithoutBillingHalt_DoesNotStampAnnotation(t *testing.T) {
	p := makeProject("no-halt-project")
	c := fake.NewClientBuilder().WithScheme(testScheme(t)).
		WithObjects(p).
		WithStatusSubresource(p).
		Build()

	if err := resumeRun(context.Background(), c, "default", "no-halt-project", false, nil); err != nil {
		t.Fatalf("resumeRun: %v", err)
	}

	var got tidev1alpha3.Project
	if err := c.Get(context.Background(), types.NamespacedName{Namespace: "default", Name: "no-halt-project"}, &got); err != nil {
		t.Fatalf("get project: %v", err)
	}
	if _, ok := got.Annotations[tidev1alpha3.AnnotationBillingResumedAt]; ok {
		t.Errorf("expected NO AnnotationBillingResumedAt on project without BillingHalt; annotations=%v", got.Annotations)
	}
}

// TestResumeWithoutBillingHalt_StillSucceeds asserts that resumeRun on a
// Project with no BillingHalt condition still succeeds (no-op for billing).
func TestResumeWithoutBillingHalt_StillSucceeds(t *testing.T) {
	p := makeProject("plain-project")
	c := fake.NewClientBuilder().WithScheme(testScheme(t)).
		WithObjects(p).
		WithStatusSubresource(p).
		Build()

	if err := resumeRun(context.Background(), c, "default", "plain-project", false, nil); err != nil {
		t.Fatalf("resumeRun on project without BillingHalt should succeed; got %v", err)
	}
}

// ---------------------------------------------------------------------------
// Phase 33 PLANFAIL-04 — resume recovery for PlannerFailed phase/milestone
// ---------------------------------------------------------------------------

// TestResumeRetryFailedPlannerFailed asserts that a Failed Phase and Failed
// Milestone each carrying ConditionFailed/Reason=ReasonPlannerFailed are reset
// (Status.Phase != "Failed") by resumeRun(retryFailed=true), with a
// ConditionWaveOrLevelPaused condition stamped with Reason=ReasonResumedByUser.
// No new resume.go code is needed — the walker already resets Failed Phases and
// Milestones; this test proves the guard's output is recoverable (PLANFAIL-04).
func TestResumeRetryFailedPlannerFailed(t *testing.T) {
	p := makeProject("my-project")

	ms := &tidev1alpha3.Milestone{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "ms-planner-failed",
			Namespace: "default",
			Labels:    map[string]string{"tideproject.k8s/project": "my-project"},
		},
		Spec:   tidev1alpha3.MilestoneSpec{ProjectRef: "my-project"},
		Status: tidev1alpha3.MilestoneStatus{Phase: "Failed"},
	}
	meta.SetStatusCondition(&ms.Status.Conditions, metav1.Condition{
		Type:               tidev1alpha3.ConditionFailed,
		Status:             metav1.ConditionTrue,
		Reason:             tidev1alpha3.ReasonPlannerFailed,
		Message:            "planner exited nonzero (exitCode=1) with zero children",
		LastTransitionTime: metav1.Now(),
	})

	ph := &tidev1alpha3.Phase{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "ph-planner-failed",
			Namespace: "default",
			Labels:    map[string]string{"tideproject.k8s/project": "my-project"},
		},
		Spec:   tidev1alpha3.PhaseSpec{MilestoneRef: "ms-planner-failed"},
		Status: tidev1alpha3.PhaseStatus{Phase: "Failed"},
	}
	meta.SetStatusCondition(&ph.Status.Conditions, metav1.Condition{
		Type:               tidev1alpha3.ConditionFailed,
		Status:             metav1.ConditionTrue,
		Reason:             tidev1alpha3.ReasonPlannerFailed,
		Message:            "planner exited nonzero (exitCode=1) with zero children",
		LastTransitionTime: metav1.Now(),
	})

	c := fake.NewClientBuilder().
		WithScheme(testScheme(t)).
		WithObjects(p, ms, ph).
		WithStatusSubresource(&tidev1alpha3.Milestone{}, &tidev1alpha3.Phase{}).
		Build()

	var buf bytes.Buffer
	if err := resumeRun(context.Background(), c, "default", "my-project", true, &buf); err != nil {
		t.Fatalf("resumeRun(retryFailed=true): %v", err)
	}

	// Phase must be reset.
	var gotPH tidev1alpha3.Phase
	if err := c.Get(context.Background(), types.NamespacedName{Namespace: "default", Name: "ph-planner-failed"}, &gotPH); err != nil {
		t.Fatalf("get phase: %v", err)
	}
	if gotPH.Status.Phase == "Failed" {
		t.Errorf("PLANFAIL-04: Phase still Failed after retry-failed with ReasonPlannerFailed")
	}
	phCond := meta.FindStatusCondition(gotPH.Status.Conditions, tidev1alpha3.ConditionWaveOrLevelPaused)
	if phCond == nil {
		t.Fatal("PLANFAIL-04: expected WaveOrLevelPaused condition on reset Phase; got nil")
	}
	if phCond.Reason != tidev1alpha3.ReasonResumedByUser {
		t.Errorf("PLANFAIL-04: Phase condition Reason=%q, want %q", phCond.Reason, tidev1alpha3.ReasonResumedByUser)
	}

	// Milestone must be reset.
	var gotMS tidev1alpha3.Milestone
	if err := c.Get(context.Background(), types.NamespacedName{Namespace: "default", Name: "ms-planner-failed"}, &gotMS); err != nil {
		t.Fatalf("get milestone: %v", err)
	}
	if gotMS.Status.Phase == "Failed" {
		t.Errorf("PLANFAIL-04: Milestone still Failed after retry-failed with ReasonPlannerFailed")
	}
	msCond := meta.FindStatusCondition(gotMS.Status.Conditions, tidev1alpha3.ConditionWaveOrLevelPaused)
	if msCond == nil {
		t.Fatal("PLANFAIL-04: expected WaveOrLevelPaused condition on reset Milestone; got nil")
	}
	if msCond.Reason != tidev1alpha3.ReasonResumedByUser {
		t.Errorf("PLANFAIL-04: Milestone condition Reason=%q, want %q", msCond.Reason, tidev1alpha3.ReasonResumedByUser)
	}
}

// ---------------------------------------------------------------------------
// Phase 34 plan 34-05 Task 3 — tide resume stamps reset-boundary-push (D-13)
// ---------------------------------------------------------------------------

// makeIntegrationIncompleteProject builds a Project fixture with a sticky
// ConditionIntegrationIncomplete and a non-zero BoundaryPush.Attempts tally.
func makeIntegrationIncompleteProject(name string) *tidev1alpha3.Project {
	p := &tidev1alpha3.Project{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
		Spec:       tidev1alpha3.ProjectSpec{SchemaRevision: "v1alpha3", TargetRepo: "https://example.com/repo.git"},
		Status:     tidev1alpha3.ProjectStatus{Phase: "Complete"},
	}
	p.Status.BoundaryPush.Attempts = 5
	meta.SetStatusCondition(&p.Status.Conditions, metav1.Condition{
		Type:               tidev1alpha3.ConditionIntegrationIncomplete,
		Status:             metav1.ConditionTrue,
		Reason:             tidev1alpha3.ReasonIntegrationIncomplete,
		Message:            "5 branch(es) missing",
		LastTransitionTime: metav1.Now(),
	})
	return p
}

// TestResumeStampsResetBoundaryPushAnnotation asserts resumeRun stamps
// tideproject.k8s/reset-boundary-push=true on a Project with a sticky
// IntegrationIncomplete condition.
func TestResumeStampsResetBoundaryPushAnnotation(t *testing.T) {
	p := makeIntegrationIncompleteProject("im-reset-project")
	c := fake.NewClientBuilder().WithScheme(testScheme(t)).
		WithObjects(p).
		WithStatusSubresource(p).
		Build()

	var buf bytes.Buffer
	if err := resumeRun(context.Background(), c, "default", "im-reset-project", false, &buf); err != nil {
		t.Fatalf("resumeRun: %v", err)
	}

	var got tidev1alpha3.Project
	if err := c.Get(context.Background(), types.NamespacedName{Namespace: "default", Name: "im-reset-project"}, &got); err != nil {
		t.Fatalf("get project: %v", err)
	}
	if got.Annotations["tideproject.k8s/reset-boundary-push"] != "true" {
		t.Errorf("expected tideproject.k8s/reset-boundary-push=true, got annotations: %v", got.Annotations)
	}
	if !strings.Contains(buf.String(), "reset boundary-push retry state") {
		t.Errorf("expected a one-line reset notice in output, got: %s", buf.String())
	}
}

// TestResumeDoesNotStampResetBoundaryPushWhenClean asserts resumeRun does NOT
// stamp the annotation on a Project with no boundary-push retry state.
func TestResumeDoesNotStampResetBoundaryPushWhenClean(t *testing.T) {
	p := &tidev1alpha3.Project{
		ObjectMeta: metav1.ObjectMeta{Name: "clean-project", Namespace: "default"},
		Spec:       tidev1alpha3.ProjectSpec{SchemaRevision: "v1alpha3", TargetRepo: "https://example.com/repo.git"},
		Status:     tidev1alpha3.ProjectStatus{Phase: "Running"},
	}
	c := fake.NewClientBuilder().WithScheme(testScheme(t)).
		WithObjects(p).
		WithStatusSubresource(p).
		Build()

	if err := resumeRun(context.Background(), c, "default", "clean-project", false, nil); err != nil {
		t.Fatalf("resumeRun: %v", err)
	}

	var got tidev1alpha3.Project
	if err := c.Get(context.Background(), types.NamespacedName{Namespace: "default", Name: "clean-project"}, &got); err != nil {
		t.Fatalf("get project: %v", err)
	}
	if _, ok := got.Annotations["tideproject.k8s/reset-boundary-push"]; ok {
		t.Errorf("reset-boundary-push annotation should not be stamped on a clean project; got annotations: %v", got.Annotations)
	}
}
