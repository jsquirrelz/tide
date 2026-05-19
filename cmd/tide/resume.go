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

// resume.go — `tide resume` cobra command + testable seam. Clears the
// tideproject.k8s/reject annotation via gates.ConsumeReject + client.Patch.
// Mirrors the reconciler's annotation-consume pattern in plan 04-05
// (e.g. milestone_controller.go:291-296) — ConsumeReject returns a NEW map,
// caller patches once.

package main

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	tidev1alpha1 "github.com/jsquirrelz/tide/api/v1alpha1"
	"github.com/jsquirrelz/tide/internal/gates"
)

// resumeRun is the testable seam. Fetches the Project, calls
// gates.ConsumeReject to get a new annotation map with the reject key
// removed, then Patches via MergeFrom.
//
// No-op if the reject annotation isn't present — ConsumeReject returns a map
// equal to the original (minus a key that didn't exist). The Patch still
// happens (it's a no-op at the apiserver level) so the operator gets
// consistent behavior whether or not the annotation was actually set.
func resumeRun(ctx context.Context, c client.Client, ns, projectName string) error {
	var proj tidev1alpha1.Project
	if err := c.Get(ctx, types.NamespacedName{Namespace: ns, Name: projectName}, &proj); err != nil {
		if apierrors.IsNotFound(err) {
			return fmt.Errorf("tide: project %q not found in namespace %q", projectName, ns)
		}
		return fmt.Errorf("get project: %w", err)
	}

	patch := client.MergeFrom(proj.DeepCopy())
	proj.SetAnnotations(gates.ConsumeReject(&proj))
	if err := c.Patch(ctx, &proj, patch); err != nil {
		return fmt.Errorf("patch project: %w", err)
	}
	return nil
}

// newResumeCmd constructs the cobra command for `tide resume`.
func newResumeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "resume <project>",
		Short: "Clear a tideproject.k8s/reject annotation on a Project",
		Long: "tide resume clears the tideproject.k8s/reject annotation via " +
			"gates.ConsumeReject + client.Patch. The reconcilers re-enter the " +
			"normal advance path.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
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
			return resumeRun(cmd.Context(), c, ns, args[0])
		},
	}
}
