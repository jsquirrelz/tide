/*
Copyright 2026 TIDE Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

// Plan 04-07 Task 2 — RED tests for `tide inspect-wave`. Uses
// controller-runtime/pkg/client/fake to populate a Task fixture matching the
// canonical label vocabulary (tideproject.k8s/project + .../wave-index per
// internal/controller/plan_controller.go:513-523) and asserts the renderer
// emits exactly the kubectl-style tabwriter columns.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	tidev1alpha3 "github.com/jsquirrelz/tide/api/v1alpha3"
)

func testScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	s := runtime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(s))
	utilruntime.Must(tidev1alpha3.AddToScheme(s))
	return s
}

// makeTask builds a Task fixture stamped with the canonical labels.
func makeTask(name, projectName, wave, phase string, attempt int, ageMin int) *tidev1alpha3.Task {
	created := metav1.NewTime(time.Now().Add(-time.Duration(ageMin) * time.Minute))
	return &tidev1alpha3.Task{
		ObjectMeta: metav1.ObjectMeta{
			Name:              name,
			Namespace:         "default",
			CreationTimestamp: created,
			Labels: map[string]string{
				"tideproject.k8s/project":    projectName,
				"tideproject.k8s/wave-index": wave,
			},
		},
		Spec: tidev1alpha3.TaskSpec{
			PlanRef:             "my-plan",
			FilesTouched:        []string{"a.go"},
			DeclaredOutputPaths: []string{"/workspace/out/a"},
		},
		Status: tidev1alpha3.TaskStatus{
			Phase:   phase,
			Attempt: attempt,
		},
	}
}

func newInspectWaveContext(t *testing.T, tasks ...*tidev1alpha3.Task) client.Client {
	t.Helper()
	objs := make([]client.Object, 0, len(tasks))
	for _, tk := range tasks {
		objs = append(objs, tk)
	}
	return fake.NewClientBuilder().WithScheme(testScheme(t)).WithObjects(objs...).Build()
}

func TestInspectWaveHumanRendersTabwriter(t *testing.T) {
	c := newInspectWaveContext(t,
		makeTask("alpha", "my-project", "0", "Running", 1, 2),
		makeTask("beta", "my-project", "0", "Pending", 0, 1),
	)
	var buf bytes.Buffer
	err := inspectWaveRun(context.Background(), c, "default", "my-project", -1, "human", &buf)
	if err != nil {
		t.Fatalf("inspectWaveRun: %v", err)
	}
	got := buf.String()
	for _, want := range []string{"NAME", "STATUS", "AGE", "ATTEMPT", "SCHEDULED-IN-WAVE", "alpha", "beta", "Running", "Pending"} {
		if !strings.Contains(got, want) {
			t.Errorf("expected %q in output; got:\n%s", want, got)
		}
	}
}

func TestInspectWaveFiltersByWave(t *testing.T) {
	c := newInspectWaveContext(t,
		makeTask("alpha", "my-project", "0", "Running", 1, 2),
		makeTask("zeta", "my-project", "1", "Pending", 0, 1),
	)
	var buf bytes.Buffer
	if err := inspectWaveRun(context.Background(), c, "default", "my-project", 0, "human", &buf); err != nil {
		t.Fatalf("inspectWaveRun: %v", err)
	}
	got := buf.String()
	if !strings.Contains(got, "alpha") {
		t.Errorf("expected wave-0 task 'alpha' in output:\n%s", got)
	}
	if strings.Contains(got, "zeta") {
		t.Errorf("expected wave-1 task 'zeta' filtered out:\n%s", got)
	}
}

func TestInspectWaveEmptyWaveFiltered(t *testing.T) {
	c := newInspectWaveContext(t,
		makeTask("alpha", "my-project", "0", "Running", 1, 2),
	)
	var buf bytes.Buffer
	var ebuf bytes.Buffer
	err := inspectWaveRunWithErr(context.Background(), c, "default", "my-project", 9, "human", &buf, &ebuf)
	if err != nil {
		t.Fatalf("inspectWaveRun: %v", err)
	}
	if !strings.Contains(ebuf.String(), "No tasks in wave 9 for project my-project") {
		t.Errorf("expected 'No tasks' message on stderr; got: stdout=%q stderr=%q", buf.String(), ebuf.String())
	}
}

func TestInspectWaveJSONOutput(t *testing.T) {
	c := newInspectWaveContext(t,
		makeTask("alpha", "my-project", "0", "Running", 1, 2),
	)
	var buf bytes.Buffer
	if err := inspectWaveRun(context.Background(), c, "default", "my-project", -1, "json", &buf); err != nil {
		t.Fatalf("inspectWaveRun: %v", err)
	}
	var rows []map[string]any
	if err := json.Unmarshal(buf.Bytes(), &rows); err != nil {
		t.Fatalf("expected JSON array; unmarshal failed: %v\nraw: %s", err, buf.String())
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 row; got %d (%v)", len(rows), rows)
	}
	if rows[0]["name"] != "alpha" {
		t.Errorf("row.name = %v; want alpha", rows[0]["name"])
	}
	if v, ok := rows[0]["wave"]; !ok || v == nil {
		t.Errorf("row missing wave field: %v", rows[0])
	}
}
