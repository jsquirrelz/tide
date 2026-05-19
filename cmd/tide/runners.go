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

// Package main — runners.go holds the cobra-side wiring (RunE bodies) for
// the three Task-2 subcommands. The pure-Go renderers live in
// inspect_wave_run.go, describe_budget_run.go, and artifact_get_run.go so
// the in-memory client.Client fixtures from controller-runtime/pkg/client/fake
// drive them directly without cobra plumbing.

package main

import (
	"github.com/spf13/cobra"
)

// runInspectWave is the cobra RunE adapter for `tide inspect-wave`.
//
// Adapter contract: resolve the namespace + K8s client, parse the --wave
// flag, delegate to inspectWaveRun.
func runInspectWave(cmd *cobra.Command, args []string) error {
	project := args[0]
	wave, _ := cmd.Flags().GetInt("wave")
	c, err := K8sClient()
	if err != nil {
		return err
	}
	ns, err := resolveNamespace()
	if err != nil {
		return err
	}
	if ns == "" {
		ns = "default"
	}
	return inspectWaveRunWithErr(cmd.Context(), c, ns, project, wave, outputFormat, cmd.OutOrStdout(), cmd.ErrOrStderr())
}

// runDescribeBudget is the cobra RunE adapter for `tide describe-budget`.
func runDescribeBudget(cmd *cobra.Command, args []string) error {
	project := args[0]
	c, err := K8sClient()
	if err != nil {
		return err
	}
	ns, err := resolveNamespace()
	if err != nil {
		return err
	}
	if ns == "" {
		ns = "default"
	}
	return describeBudgetRun(cmd.Context(), c, ns, project, outputFormat, cmd.OutOrStdout())
}

// runArtifactGet is the cobra RunE adapter for `tide artifact-get`. v1.0
// implementation is dry-run-only — the real apiserver pod-exec proxy lands
// alongside the kind harness work in plan 04-14. Dry-run is exercised by
// tests and documents the pod spec that WOULD be created.
func runArtifactGet(cmd *cobra.Command, args []string) error {
	return artifactGetDryRun(args[0], cmd.OutOrStdout())
}
