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

// billing_halt.go — BillingHalt condition helpers for HALT-01 (Phase 13).
//
// D-04: When a reconciler observes a billing-failure class Job envelope, it
// calls setBillingHaltIfNeeded to stamp BillingHalt=True on the Project. All
// five dispatch gates (milestone/phase/plan/project/task reconcilers) call
// checkBillingHalt before dispatching; if halted they park with a 30s requeue.
//
// D-05: No Job is killed; the running session exits non-zero on its own. The
// halt prevents NEW dispatch; in-flight sessions complete (or fail) naturally.
//
// D-06: Recovery is via `tide resume` (cmd/tide/resume.go), which calls
// meta.RemoveStatusCondition unconditionally. No auto-probe of provider balance.
//
// Provider-firewall note: isBillingFailureReason performs pure string ops with
// no SDK import. The Anthropic-specific classification at the HTTP boundary
// lives in internal/credproxy/server.go (isCreditExhaustion). This file
// classifies at the envelope/reason level and is legal in package controller.
package controller

import (
	"context"
	"strings"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	tideprojectv1alpha1 "github.com/jsquirrelz/tide/api/v1alpha1"
)

// isBillingFailureReason returns true if reason represents a provider
// credit-exhaustion failure that should trigger the BillingHalt gate.
//
// Classification rules (conservative substring — never exact-match Anthropic's
// wording, which may change between API versions):
//   - Case-insensitive substring "credit balance" in reason (covers the
//     EnvelopeOut.Reason channel: anthropic harness writes
//     "claude exit N: <stderr>" where Claude Code prints the 400 body message
//     to stderr, so "credit balance" appears on a billing dry-out).
//   - String has prefix "billing-halt:" (sentinel prefix for future structured
//     reporting, e.g. "billing-halt:credit-balance-too-low").
func isBillingFailureReason(reason string) bool {
	if strings.HasPrefix(reason, "billing-halt:") {
		return true
	}
	return strings.Contains(strings.ToLower(reason), "credit balance")
}

// checkBillingHalt returns true if the Project has a BillingHalt=True condition,
// indicating that all new dispatch should be parked until the operator refills
// credits and runs `tide resume`.
//
// Nil-safe: a nil project returns false (no halt — the reconciler handles the
// missing-project case separately).
func checkBillingHalt(project *tideprojectv1alpha1.Project) bool {
	if project == nil {
		return false
	}
	for _, c := range project.Status.Conditions {
		if c.Type == tideprojectv1alpha1.ConditionBillingHalt &&
			c.Status == metav1.ConditionTrue {
			return true
		}
	}
	return false
}

// setBillingHaltIfNeeded classifies reason via isBillingFailureReason; if it is
// a billing failure, stamps BillingHalt=True with Reason=CreditBalanceTooLow on
// project via the status subresource. The patch error is returned so callers
// can log it non-fatally (the halt is best-effort; the individual session has
// already exited non-zero).
//
// Nil project is a safe no-op (returns nil). Non-billing reasons are a no-op.
func setBillingHaltIfNeeded(ctx context.Context, c client.Client, project *tideprojectv1alpha1.Project, reason string) error {
	if project == nil {
		return nil
	}
	if !isBillingFailureReason(reason) {
		return nil
	}
	patch := client.MergeFrom(project.DeepCopy())
	meta.SetStatusCondition(&project.Status.Conditions, metav1.Condition{
		Type:   tideprojectv1alpha1.ConditionBillingHalt,
		Status: metav1.ConditionTrue,
		Reason: tideprojectv1alpha1.ReasonCreditBalanceTooLow,
		Message: "Provider billing 400: credit balance too low. New dispatch halted project-wide. " +
			"Run `tide resume` after refilling credits.",
		LastTransitionTime: metav1.Now(),
	})
	return c.Status().Patch(ctx, project, patch)
}
