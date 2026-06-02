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

// Command tide is the operator-facing CLI for TIDE Projects on Kubernetes.
//
// Per D-C1 the CLI is stateless — every invocation talks directly to the K8s
// API using the standard kubeconfig resolution chain ($KUBECONFIG ->
// ~/.kube/config -> in-cluster ServiceAccount). There is no local cache, no
// `tide login`, and no `kubectl tide`-only configuration file.
//
// Verbs (D-C3, final v1.0 set):
//
//	tide apply           — server-side apply wrapper around kubectl apply
//	tide watch           — long-running list+watch printing live status
//	tide tail            — pod-log stream (stub here; plan 04-08)
//	tide approve         — clear level- or wave-pause annotation (stub here; plan 04-08)
//	tide reject          — halt a Project (stub here; plan 04-08)
//	tide cancel          — destructive cascade (stub here; plan 04-08)
//	tide resume          — clear reject annotation (stub here; plan 04-08)
//	tide inspect-wave    — tabular Task render
//	tide artifact-get    — fetch PVC artifact via apiserver pod-exec proxy
//	tide describe-budget — render Project budget status
//
// Pitfall 25 mitigation: rootCmd.ExecuteContext receives a signal.NotifyContext
// wrapping context.Background, so Ctrl-C cancels cmd.Context() and long-running
// subcommands return within 1s.
//
// Pitfall 27 mitigation: rootCmd.Use is filepath.Base(os.Args[0]) so Krew-
// installed `kubectl-tide` and direct `tide` invocations both render correct
// help text.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/spf13/cobra"
)

// version is the CLI version string. Overridable at link time via:
//
//	go build -ldflags "-X main.version=v0.1.0" ./cmd/tide
//
// The default "dev" value is the in-tree development sentinel.
var version = "dev"

// rootCmd is the package-level cobra root. Subcommand files register
// themselves via rootCmd.AddCommand in their init() (D-C3 wiring).
var rootCmd = newRootCmd()

// newRootCmd constructs a fresh root command. Pulled into a constructor so
// tests can build isolated trees without touching the package-level rootCmd.
func newRootCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   filepath.Base(os.Args[0]),
		Short: "Manage TIDE Projects on Kubernetes",
		Long: "tide is the operator CLI for TIDE — Topologically-Indexed Dependency Execution. " +
			"Apply Projects, watch progress, inspect waves, fetch artifacts, and govern gates without leaving the terminal.",
		Version:       version,
		SilenceUsage:  true,
		SilenceErrors: false,
	}
	registerPersistentFlags(c)
	registerSubcommands(c)
	return c
}

// buildRootForTest is the test-only constructor that mirrors newRootCmd. It
// exists so tests can mutate flags + args on isolated command trees without
// interfering with each other or with the package-level rootCmd.
func buildRootForTest() *cobra.Command {
	return newRootCmd()
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := rootCmd.ExecuteContext(ctx); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
