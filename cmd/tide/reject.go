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

func rejectRun(ctx context.Context, c client.Client, ns, projectName, reason string) error {
	return errors.New("rejectRun: not yet implemented (RED stub)")
}

func newRejectCmd() *cobra.Command {
	var reason string
	c := &cobra.Command{
		Use:   "reject <project>",
		Short: "Halt a Project with an optional reason",
		Long:  "tide reject writes the tideproject.k8s/reject annotation on the Project; reconcilers halt dispatch and leave resources in place for inspection. Defaults --reason to \"rejected by operator\".",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return errors.New("reject: not yet implemented (RED stub)")
		},
	}
	c.Flags().StringVar(&reason, "reason", "rejected by operator", "Reason recorded in the reject annotation")
	return c
}
