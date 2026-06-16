/*
Copyright 2026 TIDE Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

// Plan 04-07 Task 2 — RED tests for `tide describe-budget` and
// `tide artifact-get`. Asserts the human/json render shapes against an
// in-memory Project fixture, and asserts artifact-get's <ns>/<project>/<path>
// parser surfaces useful errors.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	tidev1alpha1 "github.com/jsquirrelz/tide/api/v1alpha2"
)

func projectFixture(name string, capCents, spendCents int64, tokens int64) *tidev1alpha1.Project {
	return &tidev1alpha1.Project{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
		Spec: tidev1alpha1.ProjectSpec{
			TargetRepo: "https://example.com/repo.git",
			Budget:     tidev1alpha1.BudgetConfig{AbsoluteCapCents: capCents},
		},
		Status: tidev1alpha1.ProjectStatus{
			Phase: "Running",
			Budget: tidev1alpha1.BudgetStatus{
				CostSpentCents: spendCents,
				TokensSpent:    tokens,
			},
		},
	}
}

func newBudgetContext(t *testing.T, p *tidev1alpha1.Project) client.Client {
	t.Helper()
	return fake.NewClientBuilder().WithScheme(testScheme(t)).WithObjects(p).Build()
}

func TestDescribeBudgetHumanOutput(t *testing.T) {
	c := newBudgetContext(t, projectFixture("my-project", 5000, 1234, 1_500_000))
	var buf bytes.Buffer
	if err := describeBudgetRun(context.Background(), c, "default", "my-project", "human", &buf); err != nil {
		t.Fatalf("describeBudgetRun: %v", err)
	}
	got := buf.String()
	for _, want := range []string{
		"Project: my-project",
		"Absolute cap",
		"Current spend",
		"Tokens spent",
		"within budget",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("expected %q in output:\n%s", want, got)
		}
	}
}

func TestDescribeBudgetOverCap(t *testing.T) {
	c := newBudgetContext(t, projectFixture("my-project", 1000, 1500, 0))
	var buf bytes.Buffer
	if err := describeBudgetRun(context.Background(), c, "default", "my-project", "human", &buf); err != nil {
		t.Fatalf("describeBudgetRun: %v", err)
	}
	got := buf.String()
	if !strings.Contains(got, "OVER BUDGET") {
		t.Errorf("expected 'OVER BUDGET' marker when spend > cap; got:\n%s", got)
	}
}

func TestDescribeBudgetJSONOutput(t *testing.T) {
	c := newBudgetContext(t, projectFixture("my-project", 5000, 1234, 2000))
	var buf bytes.Buffer
	if err := describeBudgetRun(context.Background(), c, "default", "my-project", "json", &buf); err != nil {
		t.Fatalf("describeBudgetRun: %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(buf.Bytes(), &payload); err != nil {
		t.Fatalf("expected JSON object; got %q (err: %v)", buf.String(), err)
	}
	for _, key := range []string{"capCents", "currentSpendCents", "tokensSpent", "withinBudget"} {
		if _, ok := payload[key]; !ok {
			t.Errorf("missing JSON key %q in payload: %v", key, payload)
		}
	}
}

func TestArtifactGetRefParsingMalformed(t *testing.T) {
	for _, bad := range []string{
		"missing-slashes",
		"only/two-parts",
		"/leading-slash/bad/ref",
		"",
	} {
		ns, proj, path, err := parseArtifactRef(bad)
		if err == nil {
			t.Errorf("expected error for ref %q; got ns=%q proj=%q path=%q", bad, ns, proj, path)
		}
	}
}

func TestArtifactGetRefParsingValid(t *testing.T) {
	ns, proj, path, err := parseArtifactRef("default/my-project/envelopes/abc/out.json")
	if err != nil {
		t.Fatalf("parseArtifactRef: %v", err)
	}
	if ns != "default" || proj != "my-project" || path != "envelopes/abc/out.json" {
		t.Errorf("unexpected parse: ns=%q proj=%q path=%q", ns, proj, path)
	}
}

// TestArtifactGetDryRunPrintsPodSpec was removed in plan 15-03: the dry-run
// stub (artifactGetDryRun) is gone; the real inspector-pod path replaces it.
// Finding-3 regression coverage lives in artifact_get_run_test.go.
