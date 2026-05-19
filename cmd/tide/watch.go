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
	"io"
	"time"

	"github.com/spf13/cobra"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	tidev1alpha1 "github.com/jsquirrelz/tide/api/v1alpha1"
)

// pollInterval is the live-status refresh cadence for `tide watch`. The watch
// loop honours ctx cancellation between ticks so Ctrl-C terminates within
// pollInterval (~1s).
//
// Set conservatively for v1.0 — a future revision can swap to a real K8s
// Watch (watch.Interface) for push semantics; the poll form here is
// simpler, RBAC-equivalent, and identical-feel to the operator.
const pollInterval = 1 * time.Second

// newWatchCmd implements `tide watch <project>` — long-running list+watch on
// the Project + child CRDs. Prints a one-line status update each tick.
func newWatchCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "watch <project>",
		Short: "Watch a TIDE Project's live status",
		Long:  "tide watch streams a one-line status update per refresh tick (default 1s). Honours --namespace from kubeconfig; Ctrl-C exits cleanly.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runWatch(cmd.Context(), args[0], cmd.OutOrStdout())
		},
	}
	return c
}

// runWatch is the testable seam. Polls the Project status at pollInterval
// until ctx is cancelled.
func runWatch(ctx context.Context, name string, out io.Writer) error {
	k, err := K8sClient()
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

	var lastLine string
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	// Emit one initial line immediately so the operator sees activity
	// without waiting a full tick.
	if line := readAndRender(ctx, k, ns, name); line != "" {
		fmt.Fprintln(out, line)
		lastLine = line
	}

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			line := readAndRender(ctx, k, ns, name)
			if line != "" && line != lastLine {
				fmt.Fprintln(out, line)
				lastLine = line
			}
		}
	}
}

// readAndRender fetches the Project and formats a single status line. Returns
// "" on transient errors so the loop continues without spamming the operator.
func readAndRender(ctx context.Context, k client.Client, ns, name string) string {
	var p tidev1alpha1.Project
	if err := k.Get(ctx, types.NamespacedName{Namespace: ns, Name: name}, &p); err != nil {
		if apierrors.IsNotFound(err) {
			return fmt.Sprintf("project/%s/%s: not found", ns, name)
		}
		return ""
	}
	phase := p.Status.Phase
	if phase == "" {
		phase = "Pending"
	}

	// Count active Milestones (child CRDs) to match the dashboard-grammar
	// "live status" expectation from CONTEXT.md "tide watch should render
	// the live state as it would appear in the dashboard's left pane".
	activeMs := 0
	var msList tidev1alpha1.MilestoneList
	if err := k.List(ctx, &msList, client.InNamespace(ns)); err == nil {
		for i := range msList.Items {
			m := &msList.Items[i]
			if metav1.IsControlledBy(m, &p) {
				switch m.Status.Phase {
				case "Running", "Pending", "AwaitingApproval", "":
					activeMs++
				}
			}
		}
	}

	return fmt.Sprintf("%s/%s phase=%s active_milestones=%d", ns, name, phase, activeMs)
}
