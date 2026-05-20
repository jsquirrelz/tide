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

// cancel.go — `tide cancel <project> --force` cobra command + testable seam.
// Foreground cascade-deletes the Project CRD; K8s GC cascades to children
// (Milestone/Phase/Plan/Task) via owner refs. PVC cleanup runs via the
// existing finalizer (CTRL-05, Phase 1).
//
// Destructive surface — requires explicit --force to avoid accidental
// invocation. --dry-run prints the deletion scope (project + owner-ref'd
// children) without performing the Delete.

package main

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/spf13/cobra"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	tidev1alpha1 "github.com/jsquirrelz/tide/api/v1alpha1"
)

// cancelRun is the testable seam. Returns an error if --force is absent
// (gate), prints a banner + deletes with foreground propagation if --force is
// set, or enumerates children if --dry-run is also set.
func cancelRun(ctx context.Context, c client.Client, ns, projectName string, force, dryRun bool, out, errOut io.Writer) error {
	if !force {
		return errors.New(
			"tide: cancel is destructive — pass --force to confirm cascading delete of project, children, and PVC",
		)
	}

	var proj tidev1alpha1.Project
	if err := c.Get(ctx, types.NamespacedName{Namespace: ns, Name: projectName}, &proj); err != nil {
		if apierrors.IsNotFound(err) {
			return fmt.Errorf("tide: project %q not found in namespace %q", projectName, ns)
		}
		return fmt.Errorf("get project: %w", err)
	}

	if dryRun {
		return cancelDryRun(ctx, c, ns, projectName, &proj, out)
	}

	fmt.Fprintf(errOut, "Deleting project %s (foreground cascade)…\n", projectName)
	if err := c.Delete(ctx, &proj, client.PropagationPolicy(metav1.DeletePropagationForeground)); err != nil {
		return fmt.Errorf("delete project: %w", err)
	}
	// WR-02 fix: do not advertise PVC cleanup that the finalizer does not
	// perform. internal/controller/project_controller.go's finalizer callback
	// is a no-op log — the per-Project subPath on the shared `tide-projects`
	// PVC is NOT removed automatically. Operators must remove
	// `/workspaces/<project.UID>/workspace` manually or via an external
	// sweep Job.
	fmt.Fprintf(errOut, "Project %s deleted. Children cascade via owner refs.\n", projectName)
	fmt.Fprintf(errOut, "Note: the per-Project subPath on the shared tide-projects PVC is NOT cleaned automatically; remove %s/<project-uid>/workspace manually or via an external sweep Job.\n", "/workspaces")
	return nil
}

// cancelDryRun lists owner-ref'd / project-labelled children and prints the
// deletion scope without performing the Delete. Useful when an operator
// wants to verify the cascade target before committing.
func cancelDryRun(ctx context.Context, c client.Client, ns, projectName string, proj *tidev1alpha1.Project, out io.Writer) error {
	fmt.Fprintf(out, "tide cancel --dry-run\n")
	fmt.Fprintf(out, "  namespace: %s\n", ns)
	fmt.Fprintf(out, "  project: %s\n", projectName)
	fmt.Fprintf(out, "  would delete:\n")
	fmt.Fprintf(out, "    Project/%s\n", projectName)

	// Enumerate child kinds. Filter by the canonical project label, which is
	// the universal label-vocabulary stamped by reconcilers (per plan
	// 04-04/04-07 decisions). Owner-ref filtering would also work but the
	// label form is consistent with the rest of the CLI.
	{
		var list tidev1alpha1.MilestoneList
		if err := c.List(ctx, &list, client.InNamespace(ns)); err == nil {
			for i := range list.Items {
				m := &list.Items[i]
				if m.Labels["tideproject.k8s/project"] == projectName {
					fmt.Fprintf(out, "    Milestone/%s\n", m.Name)
				}
			}
		}
	}
	{
		var list tidev1alpha1.PhaseList
		if err := c.List(ctx, &list, client.InNamespace(ns)); err == nil {
			for i := range list.Items {
				p := &list.Items[i]
				if p.Labels["tideproject.k8s/project"] == projectName {
					fmt.Fprintf(out, "    Phase/%s\n", p.Name)
				}
			}
		}
	}
	{
		var list tidev1alpha1.PlanList
		if err := c.List(ctx, &list, client.InNamespace(ns)); err == nil {
			for i := range list.Items {
				p := &list.Items[i]
				if p.Labels["tideproject.k8s/project"] == projectName {
					fmt.Fprintf(out, "    Plan/%s\n", p.Name)
				}
			}
		}
	}
	{
		var list tidev1alpha1.TaskList
		if err := c.List(ctx, &list, client.InNamespace(ns)); err == nil {
			for i := range list.Items {
				tk := &list.Items[i]
				if tk.Labels["tideproject.k8s/project"] == projectName {
					fmt.Fprintf(out, "    Task/%s\n", tk.Name)
				}
			}
		}
	}
	fmt.Fprintf(out, "  PropagationPolicy: Foreground\n")
	// WR-02 fix: finalizer is a no-op for PVC sweep; advertise honestly.
	fmt.Fprintf(out, "  PVC cleanup: NOT automatic — operator must remove /workspaces/<project-uid>/workspace from the shared tide-projects PVC manually (or via an external sweep Job).\n")
	return nil
}

// newCancelCmd constructs the cobra command for `tide cancel`.
func newCancelCmd() *cobra.Command {
	var force, dryRun bool
	c := &cobra.Command{
		Use:   "cancel <project>",
		Short: "Destructively cancel a Project (cascade delete)",
		Long: "tide cancel deletes the Project CRD with foreground propagation; " +
			"K8s GC cascades to Milestone/Phase/Plan/Task children via owner refs. " +
			"Requires --force to confirm. Use --dry-run to preview the deletion scope.",
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
			return cancelRun(cmd.Context(), c, ns, args[0], force, dryRun, cmd.OutOrStdout(), cmd.ErrOrStderr())
		},
	}
	c.Flags().BoolVar(&force, "force", false, "Required confirmation for the destructive cascade delete")
	c.Flags().BoolVar(&dryRun, "dry-run", false, "Print what would be deleted without performing the delete")
	return c
}
