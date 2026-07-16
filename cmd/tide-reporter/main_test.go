/*
Copyright 2026 TIDE Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	tidev1alpha3 "github.com/jsquirrelz/tide/api/v1alpha3"
	pkgdispatch "github.com/jsquirrelz/tide/pkg/dispatch"
)

// buildScheme returns a *runtime.Scheme with TIDE types registered.
func buildScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	s := runtime.NewScheme()
	if err := tidev1alpha3.AddToScheme(s); err != nil {
		t.Fatalf("AddToScheme: %v", err)
	}
	return s
}

// buildFakeClient returns a fake client pre-populated with the given objects.
func buildFakeClient(t *testing.T, objs ...client.Object) client.Client {
	t.Helper()
	s := buildScheme(t)
	return fake.NewClientBuilder().WithScheme(s).WithObjects(objs...).Build()
}

// writeOutJSON writes an EnvelopeOut as out.json under workspace/envelopes/<taskUID>/out.json.
func writeOutJSON(t *testing.T, workspace, taskUID string, envOut pkgdispatch.EnvelopeOut) {
	t.Helper()
	dir := filepath.Join(workspace, "envelopes", taskUID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll %q: %v", dir, err)
	}
	data, err := json.Marshal(envOut)
	if err != nil {
		t.Fatalf("Marshal EnvelopeOut: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "out.json"), data, 0o644); err != nil {
		t.Fatalf("WriteFile out.json: %v", err)
	}
}

// phaseSpec returns a raw JSON-encoded PhaseSpec with the given milestoneRef.
func phaseSpec(t *testing.T, milestoneRef string) runtime.RawExtension {
	t.Helper()
	raw, err := json.Marshal(tidev1alpha3.PhaseSpec{MilestoneRef: milestoneRef})
	if err != nil {
		t.Fatalf("marshal PhaseSpec: %v", err)
	}
	return runtime.RawExtension{Raw: raw}
}

// Test 1: happy path — run() with a fake client and out.json containing N
// childCRDs creates N child CRs with same-namespace ownerRef and spec-parent-ref.
func TestRunHappyPath(t *testing.T) {
	workspace := t.TempDir()
	taskUID := "task-uid-happy"
	parentName := "parent-milestone"
	parentNS := "default"

	milestone := &tidev1alpha3.Milestone{
		ObjectMeta: metav1.ObjectMeta{
			Name:      parentName,
			Namespace: parentNS,
			UID:       types.UID("milestone-uid-happy"),
		},
	}

	envOut := pkgdispatch.EnvelopeOut{
		ChildCRDs: []pkgdispatch.ChildCRDSpec{
			{Kind: "Phase", Name: "phase-alpha", Spec: phaseSpec(t, parentName)},
			{Kind: "Phase", Name: "phase-beta", Spec: phaseSpec(t, parentName)},
		},
	}
	writeOutJSON(t, workspace, taskUID, envOut)

	c := buildFakeClient(t, milestone)
	cfg := reporterConfig{
		Workspace:       workspace,
		TaskUID:         taskUID,
		ParentName:      parentName,
		ParentNamespace: parentNS,
		ParentKind:      "Milestone",
	}

	var stderr bytes.Buffer
	code := runWithClient(context.Background(), cfg, nil, &stderr, c)
	if code != exitSuccess {
		t.Fatalf("runWithClient exit=%d, stderr=%q, want 0", code, stderr.String())
	}

	// Verify both Phase CRDs were created in the parent namespace.
	for _, name := range []string{"phase-alpha", "phase-beta"} {
		var ph tidev1alpha3.Phase
		if err := c.Get(context.Background(), client.ObjectKey{Namespace: parentNS, Name: name}, &ph); err != nil {
			t.Errorf("Get Phase %q: %v", name, err)
			continue
		}
		// ownerRef set with Controller=true pointing at the milestone.
		refs := ph.GetOwnerReferences()
		if len(refs) == 0 {
			t.Errorf("Phase %q has no owner refs", name)
			continue
		}
		var found bool
		for _, r := range refs {
			if r.Kind == "Milestone" && r.UID == milestone.UID {
				if r.Controller == nil || !*r.Controller {
					t.Errorf("Phase %q owner ref Controller not true", name)
				}
				found = true
			}
		}
		if !found {
			t.Errorf("Phase %q missing Milestone owner ref (uid=%s)", name, milestone.UID)
		}
		// spec-parent-ref set.
		if ph.Spec.MilestoneRef != parentName {
			t.Errorf("Phase %q Spec.MilestoneRef = %q, want %q", name, ph.Spec.MilestoneRef, parentName)
		}
	}
}

// Test 2: idempotent re-run — ChildrenAlreadyMaterialized short-circuits; no
// duplicate children created.
func TestRunIdempotent(t *testing.T) {
	workspace := t.TempDir()
	taskUID := "task-uid-idem"
	parentName := "parent-milestone-idem"
	parentNS := "default"

	milestone := &tidev1alpha3.Milestone{
		ObjectMeta: metav1.ObjectMeta{
			Name:      parentName,
			Namespace: parentNS,
			UID:       types.UID("milestone-uid-idem"),
		},
	}

	// Pre-create the phase (child already materialized).
	existingPhase := &tidev1alpha3.Phase{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pre-existing-phase-idem",
			Namespace: parentNS,
		},
		Spec: tidev1alpha3.PhaseSpec{MilestoneRef: parentName},
	}

	envOut := pkgdispatch.EnvelopeOut{
		ChildCRDs: []pkgdispatch.ChildCRDSpec{
			{Kind: "Phase", Name: "pre-existing-phase-idem", Spec: phaseSpec(t, parentName)},
		},
	}
	writeOutJSON(t, workspace, taskUID, envOut)

	c := buildFakeClient(t, milestone, existingPhase)
	cfg := reporterConfig{
		Workspace:       workspace,
		TaskUID:         taskUID,
		ParentName:      parentName,
		ParentNamespace: parentNS,
		ParentKind:      "Milestone",
	}

	var stderr bytes.Buffer
	code := runWithClient(context.Background(), cfg, nil, &stderr, c)
	if code != exitSuccess {
		t.Fatalf("second run exit=%d, stderr=%q, want 0 (idempotent)", code, stderr.String())
	}
}

// Test 3: missing out.json → non-zero exit with a clear error.
func TestRunMissingOutJSON(t *testing.T) {
	workspace := t.TempDir()
	// out.json deliberately NOT written.

	c := buildFakeClient(t, &tidev1alpha3.Milestone{
		ObjectMeta: metav1.ObjectMeta{Name: "m1", Namespace: "default"},
	})
	cfg := reporterConfig{
		Workspace:       workspace,
		TaskUID:         "task-no-file",
		ParentName:      "m1",
		ParentNamespace: "default",
		ParentKind:      "Milestone",
	}

	var stderr bytes.Buffer
	code := runWithClient(context.Background(), cfg, nil, &stderr, c)
	if code == exitSuccess {
		t.Fatal("expected non-zero exit for missing out.json, got 0")
	}
}

// Test 4: child declaring a disallowed Kind → non-zero exit; no children created.
func TestRunDisallowedKind(t *testing.T) {
	workspace := t.TempDir()
	taskUID := "task-disallowed"
	parentName := "parent-ms-disallowed"
	parentNS := "default"

	milestone := &tidev1alpha3.Milestone{
		ObjectMeta: metav1.ObjectMeta{
			Name:      parentName,
			Namespace: parentNS,
			UID:       types.UID("milestone-uid-disallowed"),
		},
	}

	envOut := pkgdispatch.EnvelopeOut{
		ChildCRDs: []pkgdispatch.ChildCRDSpec{
			{Kind: "Pod", Name: "evil-pod", Spec: runtime.RawExtension{Raw: []byte(`{}`)}},
		},
	}
	writeOutJSON(t, workspace, taskUID, envOut)

	c := buildFakeClient(t, milestone)
	cfg := reporterConfig{
		Workspace:       workspace,
		TaskUID:         taskUID,
		ParentName:      parentName,
		ParentNamespace: parentNS,
		ParentKind:      "Milestone",
	}

	var stderr bytes.Buffer
	code := runWithClient(context.Background(), cfg, nil, &stderr, c)
	if code == exitSuccess {
		t.Fatal("expected non-zero exit for disallowed Kind=Pod, got 0")
	}

	// Verify no Phase (or any TIDE CRD) was created as a side effect.
	var phases tidev1alpha3.PhaseList
	if err := c.List(context.Background(), &phases, client.InNamespace(parentNS)); err != nil {
		t.Fatalf("List phases: %v", err)
	}
	if len(phases.Items) != 0 {
		t.Errorf("unexpected %d Phase items after disallowed-Kind rejection", len(phases.Items))
	}
}

// Test 5: parent-by-name Get failure → non-zero exit; no children created.
func TestRunParentNotFound(t *testing.T) {
	workspace := t.TempDir()
	taskUID := "task-no-parent"

	envOut := pkgdispatch.EnvelopeOut{
		ChildCRDs: []pkgdispatch.ChildCRDSpec{
			{Kind: "Phase", Name: "orphan-phase", Spec: phaseSpec(t, "nonexistent-parent")},
		},
	}
	writeOutJSON(t, workspace, taskUID, envOut)

	// The client has NO milestone named "nonexistent-parent".
	c := buildFakeClient(t) // empty
	cfg := reporterConfig{
		Workspace:       workspace,
		TaskUID:         taskUID,
		ParentName:      "nonexistent-parent",
		ParentNamespace: "default",
		ParentKind:      "Milestone",
	}

	var stderr bytes.Buffer
	code := runWithClient(context.Background(), cfg, nil, &stderr, c)
	if code == exitSuccess {
		t.Fatal("expected non-zero exit for missing parent, got 0")
	}
}

// Test 6: missing required flags → non-zero exit (invariant violation).
func TestRunMissingFlags(t *testing.T) {
	workspace := t.TempDir()
	c := buildFakeClient(t)

	cases := []struct {
		name string
		cfg  reporterConfig
	}{
		{"missing task-uid", reporterConfig{
			Workspace: workspace, ParentName: "p", ParentNamespace: "ns", ParentKind: "Milestone",
		}},
		{"missing parent-name", reporterConfig{
			Workspace: workspace, TaskUID: "t", ParentNamespace: "ns", ParentKind: "Milestone",
		}},
		{"missing parent-namespace", reporterConfig{
			Workspace: workspace, TaskUID: "t", ParentName: "p", ParentKind: "Milestone",
		}},
		{"missing parent-kind", reporterConfig{
			Workspace: workspace, TaskUID: "t", ParentName: "p", ParentNamespace: "ns",
		}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var stderr bytes.Buffer
			code := runWithClient(context.Background(), tc.cfg, nil, &stderr, c)
			if code == exitSuccess {
				t.Errorf("%s: expected non-zero exit for missing required flag, got 0", tc.name)
			}
		})
	}
}

// Test 7: parseFlags accepts --traceparent and returns it verbatim on cfg
// (Phase 43 PROP-01/Pitfall 4 — the flag must exist in the same commit that
// BuildReporterJob starts emitting the --traceparent Arg).
func TestParseFlagsTraceparent(t *testing.T) {
	const traceParent = "00-0af7651916cd43dd8448eb211c80319c-b7ad6b7169203331-01"

	cfg, err := parseFlags([]string{
		"--traceparent=" + traceParent,
		"--project-uid=x",
		"--task-uid=t",
		"--parent-name=p",
		"--parent-namespace=ns",
		"--parent-kind=Milestone",
	})
	if err != nil {
		t.Fatalf("parseFlags: unexpected error: %v", err)
	}
	if cfg.TraceParent != traceParent {
		t.Errorf("cfg.TraceParent = %q, want %q", cfg.TraceParent, traceParent)
	}
}

// Test 8: parseFlags rejects an unknown flag — proves the crash-on-unknown
// contract survived the flag.ExitOnError → flag.ContinueOnError extraction
// (Pitfall 4: an unregistered Arg must still be a hard failure, not silently
// ignored).
func TestParseFlagsUnknownFlagErrors(t *testing.T) {
	_, err := parseFlags([]string{"--bogus=1"})
	if err == nil {
		t.Fatal("parseFlags: expected error for unknown flag --bogus, got nil")
	}
}
