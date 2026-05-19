/*
Copyright 2026 TIDE Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.

Plan 04-08 — RED stub. GREEN body replaces this comment with the real
client.Patch-driven approve flow.
*/

package main

import (
	"context"
	"errors"
	"io"

	"github.com/spf13/cobra"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// approveRun is the testable seam for `tide approve`. GREEN fills the body.
func approveRun(ctx context.Context, c client.Client, ns, projectName, waveFlag string, out io.Writer) error {
	return errors.New("approveRun: not yet implemented (RED stub)")
}

// newApproveCmd is the cobra command constructor for `tide approve`.
func newApproveCmd() *cobra.Command {
	var waveFlag string
	c := &cobra.Command{
		Use:   "approve <project>",
		Short: "Approve the current AwaitingApproval level or a specific wave",
		Long:  "tide approve writes the canonical approve annotation (tideproject.k8s/approve-<level>=true) on the Project's current AwaitingApproval level. With --wave <plan>/<N>, writes tideproject.k8s/approve-wave-<N>=true on the named Plan instead.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return errors.New("approve: not yet implemented (RED stub)")
		},
	}
	c.Flags().StringVar(&waveFlag, "wave", "", "Approve a specific wave: <plan-name>/<integer>")
	return c
}
