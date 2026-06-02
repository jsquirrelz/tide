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
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strconv"
	"text/tabwriter"
	"time"

	"sigs.k8s.io/controller-runtime/pkg/client"

	tidev1alpha1 "github.com/jsquirrelz/tide/api/v1alpha1"
)

// canonical label keys per internal/controller/plan_controller.go:513-523.
// Do not invent alternate vocabularies — these are the contract WaveReconciler
// + TaskReconciler depend on for fast lookups.
const (
	labelProject   = "tideproject.k8s/project"
	labelWaveIndex = "tideproject.k8s/wave-index"
)

// taskRow is the JSON-shaped row emitted by inspect-wave -o json. JSON tag
// names are deliberately lowercased + minimal — these become a public CLI
// contract operators script against.
type taskRow struct {
	Name    string `json:"name"`
	Status  string `json:"status"`
	Age     string `json:"age"`
	Attempt int    `json:"attempt"`
	Wave    int    `json:"wave"`
}

// inspectWaveRun is the testable seam called by the cobra adapter and by
// tests directly. errOut defaults to os.Stderr from cobra's plumbing — see
// inspectWaveRunWithErr for the explicit stderr-capture form used in tests
// that need to assert the empty-wave message.
func inspectWaveRun(
	ctx context.Context, c client.Client, ns, projectName string, waveFilter int, format string, out io.Writer,
) error {
	return inspectWaveRunWithErr(ctx, c, ns, projectName, waveFilter, format, out, io.Discard)
}

// inspectWaveRunWithErr is the seam tests use when they want to assert the
// "No tasks in wave N..." stderr message. Cobra passes cmd.ErrOrStderr()
// from the adapter; the discard fallback above keeps the single-output
// signature for the JSON-only path.
func inspectWaveRunWithErr(
	ctx context.Context, c client.Client, ns, projectName string, waveFilter int, format string, out, errOut io.Writer,
) error {
	// List Tasks in the namespace; filter client-side by label vocabulary
	// to avoid coupling the test harness to API-server selector parsing.
	var list tidev1alpha1.TaskList
	if err := c.List(ctx, &list, client.InNamespace(ns)); err != nil {
		return fmt.Errorf("list tasks in %s: %w", ns, err)
	}

	rows := make([]taskRow, 0, len(list.Items))
	for i := range list.Items {
		tk := &list.Items[i]
		if tk.Labels[labelProject] != projectName {
			continue
		}
		waveStr := tk.Labels[labelWaveIndex]
		wave, err := strconv.Atoi(waveStr)
		if err != nil {
			// Tasks without a stamped wave label are skipped — they
			// will surface on the next plan-reconcile cycle when
			// stampTaskLabels runs.
			continue
		}
		if waveFilter >= 0 && wave != waveFilter {
			continue
		}
		rows = append(rows, taskRow{
			Name:    tk.Name,
			Status:  defaultStatus(tk.Status.Phase),
			Age:     ageString(tk.CreationTimestamp.Time),
			Attempt: tk.Status.Attempt,
			Wave:    wave,
		})
	}

	// Deterministic ordering: by wave then by name. Matches the kubectl
	// convention of stable, sort-on-status-then-name output.
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Wave != rows[j].Wave {
			return rows[i].Wave < rows[j].Wave
		}
		return rows[i].Name < rows[j].Name
	})

	if len(rows) == 0 {
		// Empty wave: a friendly stderr message, exit 0 (the wave
		// might not yet be stamped — not a hard error).
		if waveFilter >= 0 {
			fmt.Fprintf(errOut, "No tasks in wave %d for project %s.\n", waveFilter, projectName)
		} else {
			fmt.Fprintf(errOut, "No tasks for project %s.\n", projectName)
		}
		return nil
	}

	switch format {
	case "json":
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		return enc.Encode(rows)
	default: // "human" or any other → tabwriter
		tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
		fmt.Fprintln(tw, "NAME\tSTATUS\tAGE\tATTEMPT\tSCHEDULED-IN-WAVE")
		for _, r := range rows {
			fmt.Fprintf(tw, "%s\t%s\t%s\t%d\t%d\n", r.Name, r.Status, r.Age, r.Attempt, r.Wave)
		}
		return tw.Flush()
	}
}

// defaultStatus normalizes empty Status.Phase to "Pending" for display so the
// tabwriter column isn't empty for newly-created Tasks.
func defaultStatus(phase string) string {
	if phase == "" {
		return "Pending"
	}
	return phase
}

// ageString renders a wall-clock duration in kubectl's "Xd", "Xh", "Xm",
// "Xs" form (largest unit only). Mirrors the kubectl get pods AGE column.
func ageString(from time.Time) string {
	d := time.Since(from)
	switch {
	case d >= 24*time.Hour:
		return fmt.Sprintf("%dd", int(d/(24*time.Hour)))
	case d >= time.Hour:
		return fmt.Sprintf("%dh", int(d/time.Hour))
	case d >= time.Minute:
		return fmt.Sprintf("%dm", int(d/time.Minute))
	default:
		return fmt.Sprintf("%ds", int(d/time.Second))
	}
}
