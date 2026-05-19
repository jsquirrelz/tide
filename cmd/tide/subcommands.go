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
	"fmt"

	"github.com/spf13/cobra"
)

// registerSubcommands attaches every D-C3 verb (plus completion via cobra's
// built-in) onto root. Each *_cmd.go file exports a newXxxCmd() constructor
// returning a *cobra.Command; the constructor pattern (versus init()
// AddCommand on a package-level var) keeps tests isolated — each test builds
// a fresh tree via buildRootForTest.
func registerSubcommands(root *cobra.Command) {
	root.AddCommand(newApplyCmd())
	root.AddCommand(newWatchCmd())
	root.AddCommand(newInspectWaveCmd())
	root.AddCommand(newArtifactGetCmd())
	root.AddCommand(newDescribeBudgetCmd())

	// Stubs for plan 04-08. The stub set MUST appear in `tide --help`
	// so operators see the eventual verb surface; the RunE returns an
	// error citing 04-08 for honest implementation-state signaling.
	for _, stub := range plan0408Stubs() {
		root.AddCommand(stub)
	}
}

// plan0408Stubs returns one *cobra.Command per write-back verb landing in
// plan 04-08 (tail, approve, reject, cancel, resume). Each shares the same
// "not yet implemented — landing in plan 04-08" surface so the stub-test in
// cmd_test.go can assert error wording uniformly.
func plan0408Stubs() []*cobra.Command {
	defs := []struct {
		use   string
		short string
	}{
		{"tail <task>", "Stream pod logs for a Task (plan 04-08)"},
		{"approve <project>", "Clear a level- or wave-pause annotation (plan 04-08)"},
		{"reject <project>", "Halt a Project (plan 04-08)"},
		{"cancel <project>", "Destructively cancel a Project (plan 04-08)"},
		{"resume <project>", "Clear a reject annotation (plan 04-08)"},
	}
	out := make([]*cobra.Command, 0, len(defs))
	for _, d := range defs {
		d := d
		out = append(out, &cobra.Command{
			Use:   d.use,
			Short: d.short,
			RunE: func(cmd *cobra.Command, args []string) error {
				return fmt.Errorf("%q: not yet implemented — landing in plan 04-08", firstWord(d.use))
			},
		})
	}
	return out
}

// firstWord returns the leading word of a Use string (e.g. "tail <task>" ->
// "tail"). Cobra resolves the verb name from this leading token.
func firstWord(s string) string {
	for i, r := range s {
		if r == ' ' {
			return s[:i]
		}
	}
	return s
}
