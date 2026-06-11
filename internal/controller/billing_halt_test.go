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

// Plan 13-02 Task 1 — RED tests for BillingHalt condition vocabulary + shared helpers.
// Tests: isBillingFailureReason, checkBillingHalt, setBillingHaltIfNeeded.
package controller

import (
	"context"
	"testing"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	tideprojectv1alpha1 "github.com/jsquirrelz/tide/api/v1alpha1"
)

// ---------- isBillingFailureReason ----------

func TestIsBillingFailureReason_CreditBalanceSubstring(t *testing.T) {
	reason := "claude exit 1: API Error: 400 Your credit balance is too low to access the Anthropic API."
	if !isBillingFailureReason(reason) {
		t.Errorf("expected isBillingFailureReason=true for %q", reason)
	}
}

func TestIsBillingFailureReason_BillingHaltPrefix(t *testing.T) {
	reason := "billing-halt:credit-balance-too-low"
	if !isBillingFailureReason(reason) {
		t.Errorf("expected isBillingFailureReason=true for billing-halt: prefix %q", reason)
	}
}

func TestIsBillingFailureReason_CaseInsensitive(t *testing.T) {
	reason := "something Credit Balance something"
	if !isBillingFailureReason(reason) {
		t.Errorf("expected isBillingFailureReason=true for case-insensitive match %q", reason)
	}
}

func TestIsBillingFailureReason_ForcedFailure_False(t *testing.T) {
	if isBillingFailureReason("forced-failure") {
		t.Error("expected isBillingFailureReason=false for forced-failure")
	}
}

func TestIsBillingFailureReason_CapHit_False(t *testing.T) {
	if isBillingFailureReason("cap-hit") {
		t.Error("expected isBillingFailureReason=false for cap-hit")
	}
}

func TestIsBillingFailureReason_InvalidModel_False(t *testing.T) {
	if isBillingFailureReason("claude exit 1: invalid model") {
		t.Error("expected isBillingFailureReason=false for invalid model reason")
	}
}

func TestIsBillingFailureReason_EmptyString_False(t *testing.T) {
	if isBillingFailureReason("") {
		t.Error("expected isBillingFailureReason=false for empty string")
	}
}

// ---------- checkBillingHalt ----------

func TestCheckBillingHalt_TrueWhenConditionPresent(t *testing.T) {
	project := &tideprojectv1alpha1.Project{}
	meta.SetStatusCondition(&project.Status.Conditions, metav1.Condition{
		Type:               tideprojectv1alpha1.ConditionBillingHalt,
		Status:             metav1.ConditionTrue,
		Reason:             tideprojectv1alpha1.ReasonCreditBalanceTooLow,
		LastTransitionTime: metav1.Now(),
	})
	if !checkBillingHalt(project) {
		t.Error("expected checkBillingHalt=true when BillingHalt=True condition present")
	}
}

func TestCheckBillingHalt_FalseWhenConditionAbsent(t *testing.T) {
	project := &tideprojectv1alpha1.Project{}
	if checkBillingHalt(project) {
		t.Error("expected checkBillingHalt=false when no conditions")
	}
}

func TestCheckBillingHalt_FalseWhenConditionFalse(t *testing.T) {
	project := &tideprojectv1alpha1.Project{}
	meta.SetStatusCondition(&project.Status.Conditions, metav1.Condition{
		Type:               tideprojectv1alpha1.ConditionBillingHalt,
		Status:             metav1.ConditionFalse,
		Reason:             "cleared",
		LastTransitionTime: metav1.Now(),
	})
	if checkBillingHalt(project) {
		t.Error("expected checkBillingHalt=false when BillingHalt=False")
	}
}

func TestCheckBillingHalt_FalseForNilProject(t *testing.T) {
	if checkBillingHalt(nil) {
		t.Error("expected checkBillingHalt=false for nil project")
	}
}

// ---------- setBillingHaltIfNeeded ----------

func TestSetBillingHaltIfNeeded_SetsCondition(t *testing.T) {
	s := fakeSchemeWithAll(t)
	project := &tideprojectv1alpha1.Project{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-project",
			Namespace: "default",
		},
		Spec: tideprojectv1alpha1.ProjectSpec{TargetRepo: "https://example.com/repo.git"},
	}
	c := fake.NewClientBuilder().WithScheme(s).
		WithObjects(project).
		WithStatusSubresource(project).
		Build()

	reason := "claude exit 1: API Error: 400 Your credit balance is too low to access the Anthropic API."
	if err := setBillingHaltIfNeeded(context.Background(), c, project, reason); err != nil {
		t.Fatalf("setBillingHaltIfNeeded: %v", err)
	}

	var got tideprojectv1alpha1.Project
	if err := c.Get(context.Background(), types.NamespacedName{Namespace: "default", Name: "my-project"}, &got); err != nil {
		t.Fatalf("get project: %v", err)
	}

	cond := meta.FindStatusCondition(got.Status.Conditions, tideprojectv1alpha1.ConditionBillingHalt)
	if cond == nil {
		t.Fatal("expected BillingHalt condition to be set; got nil")
	}
	if cond.Status != metav1.ConditionTrue {
		t.Errorf("expected BillingHalt=True; got %q", cond.Status)
	}
	if cond.Reason != tideprojectv1alpha1.ReasonCreditBalanceTooLow {
		t.Errorf("expected Reason=%q; got %q", tideprojectv1alpha1.ReasonCreditBalanceTooLow, cond.Reason)
	}
	if len(cond.Message) == 0 {
		t.Error("expected non-empty condition Message")
	}
	// Message must mention tide resume
	if !containsStr(cond.Message, "tide resume") {
		t.Errorf("expected condition Message to mention 'tide resume'; got %q", cond.Message)
	}
}

func TestSetBillingHaltIfNeeded_NonBillingReason_NoOp(t *testing.T) {
	s := fakeSchemeWithAll(t)
	project := &tideprojectv1alpha1.Project{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-project",
			Namespace: "default",
		},
		Spec: tideprojectv1alpha1.ProjectSpec{TargetRepo: "https://example.com/repo.git"},
	}
	c := fake.NewClientBuilder().WithScheme(s).
		WithObjects(project).
		WithStatusSubresource(project).
		Build()

	if err := setBillingHaltIfNeeded(context.Background(), c, project, "forced-failure"); err != nil {
		t.Fatalf("setBillingHaltIfNeeded: %v", err)
	}

	var got tideprojectv1alpha1.Project
	if err := c.Get(context.Background(), types.NamespacedName{Namespace: "default", Name: "my-project"}, &got); err != nil {
		t.Fatalf("get project: %v", err)
	}
	cond := meta.FindStatusCondition(got.Status.Conditions, tideprojectv1alpha1.ConditionBillingHalt)
	if cond != nil {
		t.Errorf("expected no BillingHalt condition for non-billing reason; got %+v", cond)
	}
}

func TestSetBillingHaltIfNeeded_NilProject_NoOp(t *testing.T) {
	s := fakeSchemeWithAll(t)
	c := fake.NewClientBuilder().WithScheme(s).Build()
	// Must not panic
	if err := setBillingHaltIfNeeded(context.Background(), c, nil, "claude exit 1: credit balance"); err != nil {
		t.Fatalf("expected nil error for nil project; got %v", err)
	}
}

// containsStr is a helper so billing_halt_test.go doesn't need to import strings.
func containsStr(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 ||
		func() bool {
			for i := 0; i <= len(s)-len(sub); i++ {
				if s[i:i+len(sub)] == sub {
					return true
				}
			}
			return false
		}())
}
