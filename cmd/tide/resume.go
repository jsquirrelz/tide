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

	"github.com/spf13/cobra"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func resumeRun(ctx context.Context, c client.Client, ns, projectName string) error {
	return errors.New("resumeRun: not yet implemented (RED stub)")
}

func newResumeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "resume <project>",
		Short: "Clear a tideproject.k8s/reject annotation on a Project",
		Long:  "tide resume clears the tideproject.k8s/reject annotation via gates.ConsumeReject + client.Patch. The reconcilers re-enter the normal advance path.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return errors.New("resume: not yet implemented (RED stub)")
		},
	}
}
