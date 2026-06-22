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

// newExportEnvelopesCmd constructs the cobra command for `tide export-envelopes`.
//
// The command resolves the live Project UID, streams the per-namespace PVC
// envelope subtree (envelopes/ + artifacts/) via a short-lived busybox:1.36
// inspector pod, queries live Milestone/Phase/Plan CRs to build the seed manifest
// (FQName→oldUID + dependsOn + status + per-envelope sha256), repairs legacy
// childCount on out.json files (D-16a), and assembles the portable bundle
// (default .tgz; --dir for unpacked directory).
//
// Required RBAC in the target namespace:
//   - pods: create, get, delete
//   - pods/log: get          ← inspector pod log stream (read path)
//   - milestones, phases, plans: list, get  ← seed manifest generation
func newExportEnvelopesCmd() *cobra.Command {
	var timeout time.Duration
	var pvcName string
	var outputPath string
	var outputDir bool

	c := &cobra.Command{
		Use:   "export-envelopes <namespace>/<project>",
		Short: "Export project envelopes to a portable bundle",
		Long: "tide export-envelopes creates a short-lived busybox inspector pod that mounts\n" +
			"the per-project PVC and streams the envelope subtree (envelopes/ + artifacts/)\n" +
			"as a tgz. The CLI then queries live Milestone/Phase/Plan CRs to build the seed\n" +
			"manifest (FQName→oldUID + dependsOn + status + sha256), repairs any legacy\n" +
			"childCount fields, and assembles the portable bundle.\n\n" +
			"Default output is <project>.tgz. Use --dir to emit an unpacked directory.\n\n" +
			"Required RBAC in the target namespace: pods create/get/delete, pods/log get,\n" +
			"milestones/phases/plans list/get.\n\n" +
			"Example:\n" +
			"  tide export-envelopes my-ns/my-project --output my-project.tgz\n" +
			"  tide export-envelopes my-ns/my-project --dir --output ./bundle-dir",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runExportEnvelopes(cmd, args, timeout, pvcName, outputPath, outputDir)
		},
	}
	c.Flags().DurationVar(&timeout, "timeout", 5*time.Minute,
		"Maximum time to wait for the inspector pod and stream to complete")
	c.Flags().StringVar(&pvcName, "pvc", "tide-projects",
		"PersistentVolumeClaim name holding project envelopes")
	c.Flags().StringVar(&outputPath, "output", "",
		"Output bundle path (default: <project>.tgz, or <project>-bundle for --dir)")
	c.Flags().BoolVar(&outputDir, "dir", false,
		"Emit an unpacked directory instead of a .tgz bundle")
	return c
}

// runExportEnvelopes is the cobra RunE adapter for `tide export-envelopes`.
// Resolves the K8s client and clientset, derives the output path, applies the
// timeout context, and delegates to exportEnvelopesRun.
func runExportEnvelopes(
	cmd *cobra.Command,
	args []string,
	timeout time.Duration,
	pvcName, outputPath string,
	outputDir bool,
) error {
	k, err := K8sClient()
	if err != nil {
		return err
	}
	cfg, err := RESTConfig()
	if err != nil {
		return err
	}
	cs, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return fmt.Errorf("build kubernetes clientset: %w", err)
	}

	ref := args[0]
	// Derive the project name for the default output path.
	projName := ref
	if i := lastSlash(ref); i >= 0 {
		projName = ref[i+1:]
	}
	if outputPath == "" {
		if outputDir {
			outputPath = projName + "-bundle"
		} else {
			outputPath = projName + ".tgz"
		}
	}

	ctx, cancel := context.WithTimeout(cmd.Context(), timeout)
	defer cancel()

	return exportEnvelopesRun(
		ctx,
		k,
		cs,
		ref,
		pvcName,
		outputPath,
		outputDir,
		cmd.OutOrStdout(),
		cmd.ErrOrStderr(),
	)
}

// lastSlash returns the index of the last '/' in s, or -1.
func lastSlash(s string) int {
	for i := len(s) - 1; i >= 0; i-- {
		if s[i] == '/' {
			return i
		}
	}
	return -1
}
