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

// newArtifactGetCmd constructs the cobra command for `tide artifact-get`.
// --timeout (default 5m) bounds the create/wait/stream window; status messages
// go to stderr; stdout carries raw artifact bytes (pipeable).
//
// Required RBAC in the target namespace:
//   - pods: create, get, delete
//   - pods/log: get
func newArtifactGetCmd() *cobra.Command {
	var timeout time.Duration
	var pvcName string
	c := &cobra.Command{
		Use:   "artifact-get <namespace>/<project>/<path>",
		Short: "Fetch a PVC artifact via an inspector pod",
		Long: "tide artifact-get creates a short-lived busybox inspector pod that mounts\n" +
			"the per-project PVC and streams the artifact bytes to stdout. Ref form:\n" +
			"<namespace>/<project>/<relative-path>.\n\n" +
			"The command waits for the artifact to exist and stabilize before streaming\n" +
			"(D-11: race-free readiness wait — guards against half-written files).\n" +
			"After --timeout is exhausted the command exits non-zero.\n\n" +
			"Required RBAC in the target namespace: pods create/get/delete, pods/log get.\n\n" +
			"Example:\n" +
			"  tide artifact-get my-ns/my-project/PLAN.md > plan.md",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runArtifactGet(cmd, args, timeout, pvcName)
		},
	}
	c.Flags().DurationVar(&timeout, "timeout", 5*time.Minute,
		"Maximum time to wait for the artifact to be available and stream to complete")
	c.Flags().StringVar(&pvcName, "pvc", "tide-projects",
		"PersistentVolumeClaim name holding project artifacts")
	return c
}

// runArtifactGet is the cobra RunE adapter for `tide artifact-get`.
// Resolves the K8s client and clientset, applies the timeout context, and
// delegates to artifactGetRun.
func runArtifactGet(cmd *cobra.Command, args []string, timeout time.Duration, pvcName string) error {
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

	ctx, cancel := context.WithTimeout(cmd.Context(), timeout)
	defer cancel()

	return artifactGetRun(
		ctx,
		k,
		cs,
		args[0],
		pvcName,
		cmd.OutOrStdout(),
		cmd.ErrOrStderr(),
	)
}
