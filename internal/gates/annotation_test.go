/*
Copyright 2026 TIDE Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

// Package gates unit tests for annotation.go: CheckApprove / CheckWaveApprove
// / CheckRejected / RejectedReason / ConsumeApprove / ConsumeWaveApprove /
// ConsumeReject. Pure-func tests modeled on internal/budget/cap_test.go.
// Grep-test asserts the three annotation key constants are exported.
package gates

import (
	"maps"
	"os"
	"reflect"
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	tideprojectv1alpha1 "github.com/jsquirrelz/tide/api/v1alpha2"
)

// withAnnotations is a small helper that fabricates a Milestone / Project /
// Plan with the supplied annotations. The CRD type matters for the
// `client.Object` interface (GetAnnotations); the underlying tests are
// agnostic to which CRD kind is used (the helpers read annotations off the
// metav1.ObjectMeta uniformly).
func mkMilestone(ann map[string]string) *tideprojectv1alpha1.Milestone {
	return &tideprojectv1alpha1.Milestone{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "m1",
			Namespace:   "default",
			Annotations: ann,
		},
	}
}

func mkPlan(ann map[string]string) *tideprojectv1alpha1.Plan {
	return &tideprojectv1alpha1.Plan{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "p1",
			Namespace:   "default",
			Annotations: ann,
		},
	}
}

func mkProject(ann map[string]string) *tideprojectv1alpha1.Project {
	return &tideprojectv1alpha1.Project{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "proj1",
			Namespace:   "default",
			Annotations: ann,
		},
	}
}

// ---- CheckApprove ----

// Test 1: presence + "true" value -> approval (strict).
// Test 2: empty annotations -> false.
// Test 3: value != "true" -> false (strict).
func TestCheckApprove(t *testing.T) {
	cases := []struct {
		name  string
		ann   map[string]string
		level string
		want  bool
	}{
		{"approve-milestone=true", map[string]string{"tideproject.k8s/approve-milestone": "true"}, "milestone", true},
		{"empty annotations", nil, "milestone", false},
		{"approve-milestone=false (strict)", map[string]string{"tideproject.k8s/approve-milestone": "false"}, "milestone", false},
		{"approve-milestone=TRUE (case-sensitive: strict)", map[string]string{"tideproject.k8s/approve-milestone": "TRUE"}, "milestone", false},
		{"approve-phase=true on milestone level -> false (level isolation)", map[string]string{"tideproject.k8s/approve-phase": "true"}, "milestone", false},
		{"approve-task=true", map[string]string{"tideproject.k8s/approve-task": "true"}, "task", true},
		{"approve-plan=true", map[string]string{"tideproject.k8s/approve-plan": "true"}, "plan", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := CheckApprove(mkMilestone(tc.ann), tc.level)
			if got != tc.want {
				t.Errorf("CheckApprove(ann=%v, level=%q) = %v; want %v", tc.ann, tc.level, got, tc.want)
			}
		})
	}
}

// ---- CheckWaveApprove ----

// Test 4: approve-wave-N annotation approves wave N only (level isolation by
// integer index — T-04-G3 mitigation).
func TestCheckWaveApprove(t *testing.T) {
	plan := mkPlan(map[string]string{"tideproject.k8s/approve-wave-3": "true"})
	if !CheckWaveApprove(plan, 3) {
		t.Errorf("CheckWaveApprove(approve-wave-3=true, 3) = false; want true")
	}
	if CheckWaveApprove(plan, 4) {
		t.Errorf("CheckWaveApprove(approve-wave-3=true, 4) = true; want false (different wave)")
	}
	if CheckWaveApprove(plan, 0) {
		t.Errorf("CheckWaveApprove(approve-wave-3=true, 0) = true; want false (different wave)")
	}
	// Empty annotations -> false.
	if CheckWaveApprove(mkPlan(nil), 3) {
		t.Errorf("CheckWaveApprove(nil annotations, 3) = true; want false")
	}
	// Value != "true" -> false (strict).
	if CheckWaveApprove(mkPlan(map[string]string{"tideproject.k8s/approve-wave-3": "yes"}), 3) {
		t.Errorf("CheckWaveApprove(approve-wave-3=yes, 3) = true; want false (strict)")
	}
}

// ---- CheckRejected + RejectedReason ----

// Test 5: presence of tideproject.k8s/reject (any non-empty value) is rejection.
func TestCheckRejectedAndReason(t *testing.T) {
	project := mkProject(map[string]string{"tideproject.k8s/reject": "test reason"})
	if !CheckRejected(project) {
		t.Errorf("CheckRejected(reject='test reason') = false; want true")
	}
	if got := RejectedReason(project); got != "test reason" {
		t.Errorf("RejectedReason = %q; want %q", got, "test reason")
	}

	// Empty annotations: false + empty reason.
	empty := mkProject(nil)
	if CheckRejected(empty) {
		t.Errorf("CheckRejected(nil) = true; want false")
	}
	if got := RejectedReason(empty); got != "" {
		t.Errorf("RejectedReason(nil) = %q; want empty", got)
	}

	// Reject with empty value -> NOT rejected (D-G4: reason is required
	// for the annotation to count; otherwise an empty kubectl-annotate
	// could accidentally halt the project).
	emptyVal := mkProject(map[string]string{"tideproject.k8s/reject": ""})
	if CheckRejected(emptyVal) {
		t.Errorf("CheckRejected(reject='') = true; want false (empty value is not rejection)")
	}
}

// ---- ConsumeApprove / ConsumeWaveApprove / ConsumeReject — purity tests ----

// Test 6: ConsumeApprove returns NEW map with approve-level key removed,
// original Annotations map NOT mutated. Mirrors budget.ConsumeBypass
// purity contract.
func TestConsumeApprovePurity(t *testing.T) {
	originalAnnotations := map[string]string{
		"tideproject.k8s/approve-milestone": "true",
		"some-other-key":                    "preserved",
	}
	originalCopy := map[string]string{}
	maps.Copy(originalCopy, originalAnnotations)

	ms := mkMilestone(originalAnnotations)
	got := ConsumeApprove(ms, "milestone")

	// Returned map: approve key removed, other keys preserved.
	if _, ok := got["tideproject.k8s/approve-milestone"]; ok {
		t.Errorf("ConsumeApprove returned map still contains approve-milestone key")
	}
	if got["some-other-key"] != "preserved" {
		t.Errorf("ConsumeApprove dropped unrelated annotation; got %v", got)
	}

	// Purity: original annotations map unmodified.
	if !reflect.DeepEqual(ms.Annotations, originalCopy) {
		t.Errorf("ConsumeApprove mutated original Annotations\n  got:  %v\n  want: %v", ms.Annotations, originalCopy)
	}
}

// Test for ConsumeWaveApprove.
func TestConsumeWaveApprovePurity(t *testing.T) {
	originalAnnotations := map[string]string{
		"tideproject.k8s/approve-wave-2": "true",
		"keep-me":                        "yes",
	}
	originalCopy := map[string]string{}
	maps.Copy(originalCopy, originalAnnotations)

	plan := mkPlan(originalAnnotations)
	got := ConsumeWaveApprove(plan, 2)

	if _, ok := got["tideproject.k8s/approve-wave-2"]; ok {
		t.Errorf("ConsumeWaveApprove returned map still contains approve-wave-2")
	}
	if got["keep-me"] != "yes" {
		t.Errorf("ConsumeWaveApprove dropped unrelated annotation; got %v", got)
	}
	if !reflect.DeepEqual(plan.Annotations, originalCopy) {
		t.Errorf("ConsumeWaveApprove mutated original Annotations")
	}
}

// Test 7: ConsumeReject removes the reject annotation; preserves others; pure.
func TestConsumeRejectPurity(t *testing.T) {
	originalAnnotations := map[string]string{
		"tideproject.k8s/reject": "operator halt",
		"label-style":            "stays",
	}
	originalCopy := map[string]string{}
	maps.Copy(originalCopy, originalAnnotations)

	project := mkProject(originalAnnotations)
	got := ConsumeReject(project)

	if _, ok := got["tideproject.k8s/reject"]; ok {
		t.Errorf("ConsumeReject returned map still contains reject key")
	}
	if got["label-style"] != "stays" {
		t.Errorf("ConsumeReject dropped unrelated annotation; got %v", got)
	}
	if !reflect.DeepEqual(project.Annotations, originalCopy) {
		t.Errorf("ConsumeReject mutated original Annotations")
	}
}

// Consume on object with no matching annotation returns a copy of the
// existing map (or an empty map for nil annotations).
func TestConsumeHandlesNilAndMissing(t *testing.T) {
	// nil annotations -> empty non-nil map.
	got := ConsumeApprove(mkMilestone(nil), "milestone")
	if got == nil {
		t.Errorf("ConsumeApprove(nil annotations, ...) returned nil; want empty map")
	}
	if len(got) != 0 {
		t.Errorf("ConsumeApprove(nil annotations, ...) returned non-empty: %v", got)
	}
	// Missing key in non-nil map: returns a copy preserving all keys.
	ms := mkMilestone(map[string]string{"other": "x"})
	got = ConsumeApprove(ms, "milestone")
	if got["other"] != "x" {
		t.Errorf("ConsumeApprove copy missing 'other' key; got %v", got)
	}
}

// ---- Test 8: exported annotation key constants are grep-discoverable. ----

func TestAnnotationConstantsExported(t *testing.T) {
	if AnnotationApprovePrefix != "tideproject.k8s/approve-" {
		t.Errorf("AnnotationApprovePrefix = %q; want %q", AnnotationApprovePrefix, "tideproject.k8s/approve-")
	}
	if AnnotationApproveWavePrefix != "tideproject.k8s/approve-wave-" {
		t.Errorf("AnnotationApproveWavePrefix = %q; want %q", AnnotationApproveWavePrefix, "tideproject.k8s/approve-wave-")
	}
	if AnnotationReject != "tideproject.k8s/reject" {
		t.Errorf("AnnotationReject = %q; want %q", AnnotationReject, "tideproject.k8s/reject")
	}
}

// Grep-assert the three key prefixes appear as literal strings in
// annotation.go (mirrors the validation block in PLAN: grep -c
// "tideproject.k8s/approve" annotation.go >= 2 and "tideproject.k8s/reject"
// >= 1).
func TestAnnotationKeysSourceGrep(t *testing.T) {
	src, err := os.ReadFile("annotation.go")
	if err != nil {
		t.Fatalf("read annotation.go: %v", err)
	}
	body := string(src)
	if !strings.Contains(body, "tideproject.k8s/approve-") {
		t.Errorf("annotation.go missing literal %q", "tideproject.k8s/approve-")
	}
	if !strings.Contains(body, "tideproject.k8s/approve-wave-") {
		t.Errorf("annotation.go missing literal %q", "tideproject.k8s/approve-wave-")
	}
	if !strings.Contains(body, "tideproject.k8s/reject") {
		t.Errorf("annotation.go missing literal %q", "tideproject.k8s/reject")
	}
}
