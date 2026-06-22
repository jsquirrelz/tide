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

	// Plan 04-08 — real write-back verbs (approve / reject / cancel / resume / tail).
	root.AddCommand(newApproveCmd())
	root.AddCommand(newRejectCmd())
	root.AddCommand(newCancelCmd())
	root.AddCommand(newResumeCmd())
	root.AddCommand(newTailCmd())

	// Plan 29-02/29-03 — envelope portability verbs.
	root.AddCommand(newExportEnvelopesCmd())
	root.AddCommand(newImportEnvelopesCmd())
}
