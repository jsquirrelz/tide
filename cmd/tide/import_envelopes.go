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
	"fmt"
	"time"

	"github.com/spf13/cobra"
	"k8s.io/client-go/kubernetes"
)

// newImportEnvelopesCmd constructs the cobra command for `tide import-envelopes`.
// --dry-run runs offline (no cluster, no pods); the live path stages envelopes
// and the seed ConfigMap but does NOT apply the Project (D-05).
//
// Required RBAC in the target namespace (live mode only):
//   - pods: create, get, delete
//   - pods/exec: create   (loader pod stdin stream via SPDY — write direction)
//   - configmaps: create
func newImportEnvelopesCmd() *cobra.Command {
	var (
		namespace string
		dryRun    bool
		pvcName   string
		timeout   time.Duration
	)

	c := &cobra.Command{
		Use:   "import-envelopes <bundle>",
		Short: "Stage a bundle for re-adoption (--dry-run for offline preview)",
		Long: "tide import-envelopes stages a TIDE bundle (produced by export-envelopes)\n" +
			"into a target namespace so a subsequent `tide apply project.yaml` triggers\n" +
			"adoption of the already-executed envelopes (Phase 28, D-05).\n\n" +
			"Two modes:\n" +
			"  --dry-run  Validates the bundle offline — no cluster writes, no pods.\n" +
			"             Reports a per-level adopt/re-plan table (--output json for\n" +
			"             machine-readable output). Cycle in the DAG hard-rejects the\n" +
			"             entire import (D-09).\n\n" +
			"  (live)     Stages envelopes onto the target PVC via a loader pod\n" +
			"             (SPDY exec, pods/exec: create), creates the seed ConfigMap,\n" +
			"             and writes project.yaml to disk. Run `tide apply project.yaml`\n" +
			"             afterward to trigger adoption.\n\n" +
			"Example (dry-run):\n" +
			"  tide import-envelopes ./my-project.tgz --dry-run\n\n" +
			"Example (live):\n" +
			"  tide import-envelopes ./my-project.tgz --namespace prod",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			outputFormat, _ := cmd.Flags().GetString("output")
			return runImportEnvelopes(cmd, args, namespace, dryRun, timeout, pvcName, outputFormat)
		},
	}

	c.Flags().StringVarP(&namespace, "namespace", "n", "",
		"Target namespace for the seed ConfigMap and loader pod")
	c.Flags().BoolVar(&dryRun, "dry-run", false,
		"Validate bundle offline without cluster writes or pods")
	c.Flags().StringVar(&pvcName, "pvc", "tide-projects",
		"PersistentVolumeClaim name to stage envelopes onto")
	c.Flags().DurationVar(&timeout, "timeout", 5*time.Minute,
		"Maximum time for loader pod creation, streaming, and ConfigMap creation")

	return c
}

// runImportEnvelopes is the cobra RunE adapter for `tide import-envelopes`.
// In dry-run mode no K8s client is constructed (fully offline). In live mode
// it resolves the K8s client + clientset, applies the timeout context, and
// delegates to importEnvelopesRun.
func runImportEnvelopes(
	cmd *cobra.Command,
	args []string,
	namespace string,
	dryRun bool,
	timeout time.Duration,
	pvcName string,
	outputFormat string,
) error {
	bundlePath := args[0]

	if dryRun {
		// Offline — no K8s client needed (D-07).
		return importEnvelopesDryRun(bundlePath, outputFormat, cmd.OutOrStdout(), cmd.ErrOrStderr())
	}

	// Live mode — build clients.
	cfg, err := RESTConfig()
	if err != nil {
		return err
	}
	cs, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return fmt.Errorf("build kubernetes clientset: %w", err)
	}

	ctx, cancel := context.WithTimeout(cmd.Context(), timeout)
	defer cancel()

	return importEnvelopesRun(ctx, cs, bundlePath, namespace, pvcName, cmd.ErrOrStderr())
}
