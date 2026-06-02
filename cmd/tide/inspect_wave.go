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

// newInspectWaveCmd is registered in Task 1 to make `tide --help` honest;
// Task 2 fills in the tabwriter rendering against TaskList + label selectors.
//
// The positional argument is the Project name (the rendered column set is
// "tasks belonging to <project>, optionally filtered by wave"). The Plan/
// Project distinction in the verb name follows D-C3's "tide inspect-wave
// <plan>" surface — in v1.0 the canonical label vocabulary
// (tideproject.k8s/project) keys lookups on the Project name; a v1.x
// extension may switch to <plan> selectors if multi-plan-per-project
// emerges.
func newInspectWaveCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "inspect-wave <project>",
		Short: "List Tasks in a Project's wave with status/age/attempt/wave columns",
		Long: "tide inspect-wave renders the wave's task list in kubectl-aligned tabwriter columns " +
			"(NAME, STATUS, AGE, ATTEMPT, SCHEDULED-IN-WAVE). Filter by --wave N. Honours -o json.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runInspectWave(cmd, args)
		},
	}
	c.Flags().Int("wave", -1, "Filter to a specific wave index (default: show all waves)")
	return c
}
