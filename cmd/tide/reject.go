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

// reject.go — `tide reject` cobra command + testable seam. Writes
// tideproject.k8s/reject=<reason> on the Project via client.MergeFrom +
// client.Patch. The reconciler-side reads land in plan 04-05 (CheckRejected /
// patch<Level>Failed). One-shot: `tide resume` clears the annotation.

package main

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	tidev1alpha3 "github.com/jsquirrelz/tide/api/v1alpha3"
	"github.com/jsquirrelz/tide/internal/gates"
)

// rejectRun is the testable seam. Fetches the Project, writes the reject
// annotation with the supplied reason, and Patches via MergeFrom.
//
// Empty reason defaults to "rejected by operator" so a bare `tide reject foo`
// still carries a meaningful value (D-G4: empty value is treated as
// no-rejection by gates.CheckRejected, so the default must be non-empty).
func rejectRun(ctx context.Context, c client.Client, ns, projectName, reason string) error {
	if reason == "" {
		reason = "rejected by operator"
	}

	var proj tidev1alpha3.Project
	if err := c.Get(ctx, types.NamespacedName{Namespace: ns, Name: projectName}, &proj); err != nil {
		if apierrors.IsNotFound(err) {
			return fmt.Errorf("tide: project %q not found in namespace %q", projectName, ns)
		}
		return fmt.Errorf("get project: %w", err)
	}

	patch := client.MergeFrom(proj.DeepCopy())
	anno := proj.GetAnnotations()
	if anno == nil {
		anno = map[string]string{}
	}
	anno[gates.AnnotationReject] = reason
	proj.SetAnnotations(anno)

	if err := c.Patch(ctx, &proj, patch); err != nil {
		return fmt.Errorf("patch project: %w", err)
	}
	return nil
}

// newRejectCmd constructs the cobra command for `tide reject`.
func newRejectCmd() *cobra.Command {
	var reason string
	c := &cobra.Command{
		Use:   "reject <project>",
		Short: "Halt a Project with an optional reason",
		Long: "tide reject writes the tideproject.k8s/reject annotation on the " +
			"Project; reconcilers halt dispatch and leave resources in place for " +
			"inspection. Defaults --reason to \"rejected by operator\".",
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
			return rejectRun(cmd.Context(), c, ns, args[0], reason)
		},
	}
	c.Flags().StringVar(&reason, "reason", "rejected by operator", "Reason recorded in the reject annotation")
	return c
}
