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
	"github.com/spf13/cobra"
)

// newDescribeBudgetCmd is registered in Task 1; Task 2 fills in the
// human/json rendering against Project.Status.Budget.
func newDescribeBudgetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "describe-budget <project>",
		Short: "Show a Project's budget cap vs. running spend",
		Long: "tide describe-budget surfaces Project.Status.Budget (TokensSpent, CostSpentCents, WindowStart) " +
			"against Project.Spec.Budget.AbsoluteCapCents. Default output is human-readable; -o json emits a structured object.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDescribeBudget(cmd, args)
		},
	}
}
