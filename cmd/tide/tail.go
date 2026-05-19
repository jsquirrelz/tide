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
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// tailOptions carries the parsed flag set for `tide tail`.
type tailOptions struct {
	container  string
	tailLines  int64
	timestamps bool
}

// tailPodPicker resolves the active Pod + container for a Task. Exported as a
// function var so tests can inject a deterministic resolver without setting
// up real Pod fixtures behind a client-go fake clientset.
var tailPodPicker = func(ctx context.Context, k client.Client, ns, taskName string, opt tailOptions) (podName, container string, err error) {
	return "", "", errors.New("tailPodPicker: not yet implemented (RED stub)")
}

// tailStreamer opens the pod-log stream and copies until ctx is cancelled or
// EOF. Same function-var pattern as tailPodPicker so tests can drive without
// a live apiserver.
var tailStreamer = func(ctx context.Context, cs kubernetes.Interface, ns, pod, container string, opt tailOptions, out, errOut io.Writer) error {
	return errors.New("tailStreamer: not yet implemented (RED stub)")
}

// tailRun is the testable seam. The cobra adapter resolves clientsets, picks
// the pod/container via tailPodPicker, then hands off to tailStreamer.
func tailRun(ctx context.Context, k client.Client, cs kubernetes.Interface, ns, taskName string, opt tailOptions, out, errOut io.Writer) error {
	return errors.New("tailRun: not yet implemented (RED stub)")
}

func newTailCmd() *cobra.Command {
	var opt tailOptions
	c := &cobra.Command{
		Use:   "tail <task>",
		Short: "Stream pod logs for a Task",
		Long:  "tide tail opens a Follow:true log stream against the Task's currently-active Pod via the pods/log subresource. Ctrl-C cancels cleanly. Use --container to pick a specific container; default skips credproxy and init-* containers.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return errors.New("tail: not yet implemented (RED stub)")
		},
	}
	c.Flags().StringVarP(&opt.container, "container", "c", "", "Container name to stream from (default: first non-credproxy/non-init container)")
	c.Flags().Int64Var(&opt.tailLines, "tail", 100, "Number of recent lines to print before streaming")
	c.Flags().BoolVarP(&opt.timestamps, "timestamps", "t", true, "Include timestamps on log lines")
	_ = corev1.PodLogOptions{} // import anchor — used in the GREEN implementation
	return c
}
