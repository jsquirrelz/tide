/*
Copyright 2026 TIDE Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.

Plan 04-08 — RED stub.
*/

package main

import (
	"context"
	"errors"
	"io"

	"github.com/spf13/cobra"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func cancelRun(ctx context.Context, c client.Client, ns, projectName string, force, dryRun bool, out, errOut io.Writer) error {
	return errors.New("cancelRun: not yet implemented (RED stub)")
}

func newCancelCmd() *cobra.Command {
	var force, dryRun bool
	c := &cobra.Command{
		Use:   "cancel <project>",
		Short: "Destructively cancel a Project (cascade delete)",
		Long:  "tide cancel deletes the Project CRD with foreground propagation; K8s GC cascades to Milestone/Phase/Plan/Task children via owner refs. Requires --force to confirm. Use --dry-run to preview the deletion scope.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return errors.New("cancel: not yet implemented (RED stub)")
		},
	}
	c.Flags().BoolVar(&force, "force", false, "Required confirmation for the destructive cascade delete")
	c.Flags().BoolVar(&dryRun, "dry-run", false, "Print what would be deleted without performing the delete")
	return c
}
