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

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"

	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	tidev1alpha1 "github.com/jsquirrelz/tide/api/v1alpha2"
)

// budgetPayload is the -o json shape for `tide describe-budget`. Becomes a
// public CLI contract — script-friendly keys, lowerCamelCase per JSON norms.
type budgetPayload struct {
	Project           string  `json:"project"`
	CapCents          int64   `json:"capCents"`
	CurrentSpendCents int64   `json:"currentSpendCents"`
	TokensSpent       int64   `json:"tokensSpent"`
	WindowStart       string  `json:"windowStart,omitempty"`
	WithinBudget      bool    `json:"withinBudget"`
	UtilizationPct    float64 `json:"utilizationPct"`
}

// describeBudgetRun is the testable seam — fetches Project + renders.
//
// Output formats follow D-C4: "human" (default) is a 6-line block matching
// the dashboard's left-pane budget-row grammar; "json" emits budgetPayload.
//
// T-04-C3 mitigation: this renderer reads ONLY Status.Budget + Spec.Budget
// fields. It never touches Spec.SecretRefs, ProviderSecretRef, or any
// kubeconfig-derived token; the threat-model assertion is by-construction.
func describeBudgetRun(ctx context.Context, c client.Client, ns, name, format string, out io.Writer) error {
	var p tidev1alpha1.Project
	if err := c.Get(ctx, types.NamespacedName{Namespace: ns, Name: name}, &p); err != nil {
		return fmt.Errorf("get project %s/%s: %w", ns, name, err)
	}

	cap := p.Spec.Budget.AbsoluteCapCents
	spend := p.Status.Budget.CostSpentCents
	tokens := p.Status.Budget.TokensSpent

	// "Within budget" = spend <= cap. Zero cap means "uncapped" — always
	// within budget; render util as 0% to avoid divide-by-zero.
	within := true
	if cap > 0 {
		within = spend <= cap
	}
	util := 0.0
	if cap > 0 {
		util = (float64(spend) / float64(cap)) * 100.0
	}

	windowStart := ""
	if p.Status.Budget.WindowStart != nil {
		windowStart = p.Status.Budget.WindowStart.UTC().Format("2006-01-02T15:04:05Z")
	}

	if format == "json" {
		payload := budgetPayload{
			Project:           name,
			CapCents:          cap,
			CurrentSpendCents: spend,
			TokensSpent:       tokens,
			WindowStart:       windowStart,
			WithinBudget:      within,
			UtilizationPct:    util,
		}
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		return enc.Encode(payload)
	}

	// Human form — six readable lines plus a status marker. Uses USD-cents
	// rendering since BudgetConfig.AbsoluteCapCents is the canonical unit
	// per api/v1alpha1/project_types.go.
	status := "within budget"
	if !within {
		status = "OVER BUDGET"
	}

	fmt.Fprintf(out, "Project: %s\n", name)
	fmt.Fprintf(out, "Absolute cap:    $%.2f (%d cents)\n", float64(cap)/100.0, cap)
	fmt.Fprintf(out, "Current spend:   $%.2f (%d cents)\n", float64(spend)/100.0, spend)
	fmt.Fprintf(out, "Tokens spent:    %d\n", tokens)
	if windowStart != "" {
		fmt.Fprintf(out, "Window start:    %s\n", windowStart)
	}
	fmt.Fprintf(out, "Utilization:     %.1f%%\n", util)
	fmt.Fprintf(out, "Status:          %s\n", status)
	return nil
}
