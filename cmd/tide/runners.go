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

// Package main — runners.go holds the RunE bodies for the three Task-2
// subcommands (inspect-wave, artifact-get, describe-budget). Task 1 ships
// minimal placeholder implementations so the binary builds and `tide --help`
// lists every verb; Task 2 overwrites the bodies with the real renderers.

package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

// Task-1 placeholder. Task 2 replaces with real tabwriter rendering.
func runInspectWave(cmd *cobra.Command, args []string) error {
	return fmt.Errorf("inspect-wave: not yet implemented in Task 1 — Task 2 fills the renderer")
}

// Task-1 placeholder. Task 2 replaces with real apiserver pod-exec proxy.
func runArtifactGet(cmd *cobra.Command, args []string) error {
	return fmt.Errorf("artifact-get: not yet implemented in Task 1 — Task 2 fills the renderer")
}

// Task-1 placeholder. Task 2 replaces with real Status.Budget render.
func runDescribeBudget(cmd *cobra.Command, args []string) error {
	return fmt.Errorf("describe-budget: not yet implemented in Task 1 — Task 2 fills the renderer")
}
