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
	"strings"
	"testing"

	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	tidev1alpha3 "github.com/jsquirrelz/tide/api/v1alpha3"
	"github.com/jsquirrelz/tide/internal/otelinit"
	pkgdispatch "github.com/jsquirrelz/tide/pkg/dispatch"
	"github.com/jsquirrelz/tide/pkg/otelai"
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

// Test 8b: TestParseFlagsSkipMessageSpans — ADAPT-01/D-03 polarity: the
// bareword --skip-message-spans flag maps onto the PARSED struct field
// (Pitfall 3: a registered-but-never-copied flag silently no-ops); its
// absence must parse to false (D-03 absent-means-synthesize).
func TestParseFlagsSkipMessageSpans(t *testing.T) {
	t.Run("present", func(t *testing.T) {
		cfg, err := parseFlags([]string{
			"--skip-message-spans",
			"--project-uid=x",
			"--task-uid=t",
			"--parent-name=p",
			"--parent-namespace=ns",
			"--parent-kind=Milestone",
		})
		if err != nil {
			t.Fatalf("parseFlags: unexpected error: %v", err)
		}
		if !cfg.SkipMessageSpans {
			t.Errorf("cfg.SkipMessageSpans = false, want true when --skip-message-spans is present")
		}
	})

	t.Run("absent", func(t *testing.T) {
		cfg, err := parseFlags([]string{
			"--project-uid=x",
			"--task-uid=t",
			"--parent-name=p",
			"--parent-namespace=ns",
			"--parent-kind=Milestone",
		})
		if err != nil {
			t.Fatalf("parseFlags: unexpected error: %v", err)
		}
		if cfg.SkipMessageSpans {
			t.Errorf("cfg.SkipMessageSpans = true, want false when --skip-message-spans is absent (D-03)")
		}
	})
}

// ─── Phase 44 MSG-01/TRACE-03: trace-only mode + shutdown-on-every-path ────

// shutdownRecorder captures whether the newTracerProvider seam's returned
// ShutdownFunc was invoked and whether the context it was called with
// carried a deadline — the TRACE-03/D-12 bounded-flush discipline this test
// file exists to prove.
type shutdownRecorder struct {
	invoked     bool
	hadDeadline bool
}

func (r *shutdownRecorder) shutdown(ctx context.Context) error {
	r.invoked = true
	_, r.hadDeadline = ctx.Deadline()
	return nil
}

// installStubTracerProvider overrides the package-level newTracerProvider
// seam for the duration of the test with a stub wired to exp (synchronous
// sdktrace.WithSyncer — no flush needed for the span-content assertions in
// TestRunTraceOnly_EmitsSpans) and a shutdownRecorder standing in for
// otelinit's real ShutdownFunc. WithSyncer is used here despite TRACE-03
// being about the async batch path: this seam test proves Shutdown is
// INVOKED with a bounded ctx on every runWithClient exit path (the
// discipline); the SDK's own contract guarantees Shutdown drains the batch
// queue, and that batch-drain behavior is covered by 44-03's
// TestEmitSpans_BatchAggregateUnderCeiling ForceFlush/Shutdown path. Both
// the seam and the global otel provider are restored via t.Cleanup, mirror-
// ing setupSpanExporter's prev-provider restore discipline in
// internal/controller/span_emission_unit_test.go.
func installStubTracerProvider(t *testing.T, exp sdktrace.SpanExporter) *shutdownRecorder {
	t.Helper()
	rec := &shutdownRecorder{}
	prevSeam := newTracerProvider
	prevProvider := otel.GetTracerProvider()
	newTracerProvider = func(context.Context) (trace.TracerProvider, otelinit.ShutdownFunc, error) {
		tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exp))
		otel.SetTracerProvider(tp)
		return tp, rec.shutdown, nil
	}
	t.Cleanup(func() {
		newTracerProvider = prevSeam
		otel.SetTracerProvider(prevProvider)
	})
	return rec
}

// writeTraceOnlyFixture writes a small (2-call) events.jsonl + in.json pair
// under workspace/envelopes/<taskUID> — reuses 44-03's fixture SHAPE
// (message_start/assistant/message_delta/message_stop cycles) without
// depending on internal/reporter/testdata's file paths.
func writeTraceOnlyFixture(t *testing.T, workspace, taskUID string) {
	t.Helper()
	dir := filepath.Join(workspace, "envelopes", taskUID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll %q: %v", dir, err)
	}
	const events = `{"type":"stream_event","event":{"type":"message_start","message":{"id":"msg_call1","model":"claude-sonnet-4-6","usage":{"input_tokens":10}}}}
{"type":"assistant","message":{"id":"msg_call1","model":"claude-sonnet-4-6","content":[{"type":"text","text":"hello"}]}}
{"type":"stream_event","event":{"type":"message_delta","usage":{"output_tokens":5}}}
{"type":"stream_event","event":{"type":"message_stop"}}
{"type":"stream_event","event":{"type":"message_start","message":{"id":"msg_call2","model":"claude-sonnet-4-6","usage":{"input_tokens":20}}}}
{"type":"assistant","message":{"id":"msg_call2","model":"claude-sonnet-4-6","content":[{"type":"text","text":"world"}]}}
{"type":"stream_event","event":{"type":"message_delta","usage":{"output_tokens":8}}}
{"type":"stream_event","event":{"type":"message_stop"}}
`
	if err := os.WriteFile(filepath.Join(dir, "events.jsonl"), []byte(events), 0o644); err != nil {
		t.Fatalf("WriteFile events.jsonl: %v", err)
	}
	const inJSON = `{"prompt":"do the thing"}`
	if err := os.WriteFile(filepath.Join(dir, "in.json"), []byte(inJSON), 0o644); err != nil {
		t.Fatalf("WriteFile in.json: %v", err)
	}
}

// Test 9: TestRunWithClient_ShutdownOnEveryExitPath — table-driven over 4
// distinct exit paths; asserts the newTracerProvider seam's ShutdownFunc was
// invoked with a bounded (deadline-bearing) context on EVERY path,
// regardless of the exit code returned.
func TestRunWithClient_ShutdownOnEveryExitPath(t *testing.T) {
	cases := []struct {
		name     string
		buildCfg func(t *testing.T) (reporterConfig, client.Client)
		wantExit int
	}{
		{
			name: "missing task-uid",
			buildCfg: func(t *testing.T) (reporterConfig, client.Client) {
				return reporterConfig{}, buildFakeClient(t)
			},
			wantExit: exitInvariant,
		},
		{
			name: "trace-only success",
			buildCfg: func(t *testing.T) (reporterConfig, client.Client) {
				workspace := t.TempDir()
				taskUID := "task-shutdown-trace"
				writeTraceOnlyFixture(t, workspace, taskUID)
				return reporterConfig{TraceOnly: true, TaskUID: taskUID, Workspace: workspace}, buildFakeClient(t)
			},
			wantExit: exitSuccess,
		},
		{
			name: "combined missing out.json",
			buildCfg: func(t *testing.T) (reporterConfig, client.Client) {
				workspace := t.TempDir()
				c := buildFakeClient(t, &tidev1alpha3.Milestone{
					ObjectMeta: metav1.ObjectMeta{Name: "m-shutdown", Namespace: "default"},
				})
				return reporterConfig{
					Workspace: workspace, TaskUID: "task-no-out", ParentName: "m-shutdown",
					ParentNamespace: "default", ParentKind: "Milestone",
				}, c
			},
			wantExit: exitInvariant,
		},
		{
			name: "combined happy path",
			buildCfg: func(t *testing.T) (reporterConfig, client.Client) {
				workspace := t.TempDir()
				taskUID := "task-shutdown-happy"
				parentName := "parent-shutdown-happy"
				milestone := &tidev1alpha3.Milestone{
					ObjectMeta: metav1.ObjectMeta{
						Name: parentName, Namespace: "default", UID: types.UID("uid-shutdown-happy"),
					},
				}
				envOut := pkgdispatch.EnvelopeOut{
					ChildCRDs: []pkgdispatch.ChildCRDSpec{
						{Kind: "Phase", Name: "phase-shutdown", Spec: phaseSpec(t, parentName)},
					},
				}
				writeOutJSON(t, workspace, taskUID, envOut)
				return reporterConfig{
					Workspace: workspace, TaskUID: taskUID, ParentName: parentName,
					ParentNamespace: "default", ParentKind: "Milestone",
				}, buildFakeClient(t, milestone)
			},
			wantExit: exitSuccess,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			exp := tracetest.NewInMemoryExporter()
			rec := installStubTracerProvider(t, exp)
			cfg, c := tc.buildCfg(t)

			var stderr bytes.Buffer
			code := runWithClient(context.Background(), cfg, nil, &stderr, c)
			if code != tc.wantExit {
				t.Fatalf("runWithClient exit=%d, want %d; stderr=%q", code, tc.wantExit, stderr.String())
			}
			if !rec.invoked {
				t.Errorf("otelShutdown was not invoked (TRACE-03 violated)")
			}
			if !rec.hadDeadline {
				t.Errorf("otelShutdown ctx carried no deadline (D-12 bound not applied)")
			}
		})
	}
}

// Test 10: TestRunTraceOnly_EmitsSpans — a trace-only run against a fixture
// workspace emits at least one span whose TraceID matches the injected
// traceparent's TraceID, proving ExtractRemoteParent-based parenting.
func TestRunTraceOnly_EmitsSpans(t *testing.T) {
	exp := tracetest.NewInMemoryExporter()
	installStubTracerProvider(t, exp)

	workspace := t.TempDir()
	taskUID := "task-trace-only-emits"
	writeTraceOnlyFixture(t, workspace, taskUID)

	traceID, err := trace.TraceIDFromHex("0af7651916cd43dd8448eb211c80319c")
	if err != nil {
		t.Fatalf("TraceIDFromHex: %v", err)
	}
	spanID, err := trace.SpanIDFromHex("b7ad6b7169203331")
	if err != nil {
		t.Fatalf("SpanIDFromHex: %v", err)
	}
	traceParent := otelai.FormatTraceparent(traceID, spanID, true)

	cfg := reporterConfig{
		TraceOnly: true, TaskUID: taskUID, Workspace: workspace, TraceParent: traceParent,
	}

	var stderr bytes.Buffer
	code := runWithClient(context.Background(), cfg, nil, &stderr, nil)
	if code != exitSuccess {
		t.Fatalf("runWithClient exit=%d, want exitSuccess; stderr=%q", code, stderr.String())
	}

	spans := exp.GetSpans()
	if len(spans) == 0 {
		t.Fatal("got 0 spans, want at least 1")
	}
	for i, span := range spans {
		if span.SpanContext.TraceID() != traceID {
			t.Errorf("span[%d].TraceID = %s, want %s (parenting via --traceparent)", i, span.SpanContext.TraceID(), traceID)
		}
	}
}

// Test 10b: TestRunTraceOnly_SkipsSynthesisWhenFlagSet — ADAPT-01/D-05: the
// inverse of TestRunTraceOnly_EmitsSpans. Against the SAME fixture that
// normally yields spans, SkipMessageSpans: true must synthesize zero spans,
// write no .spans-emitted sentinel, exit 0, and log the skip line on stderr.
func TestRunTraceOnly_SkipsSynthesisWhenFlagSet(t *testing.T) {
	exp := tracetest.NewInMemoryExporter()
	installStubTracerProvider(t, exp)

	workspace := t.TempDir()
	taskUID := "task-trace-only-skips"
	writeTraceOnlyFixture(t, workspace, taskUID) // would normally yield 2 spans

	cfg := reporterConfig{
		TraceOnly: true, TaskUID: taskUID, Workspace: workspace, SkipMessageSpans: true,
	}

	var stderr bytes.Buffer
	code := runWithClient(context.Background(), cfg, nil, &stderr, nil)
	if code != exitSuccess {
		t.Fatalf("runWithClient exit=%d, want exitSuccess; stderr=%q", code, stderr.String())
	}

	if got := len(exp.GetSpans()); got != 0 {
		t.Errorf("got %d spans, want 0 (synthesis must be skipped when SkipMessageSpans is set)", got)
	}

	sentinelPath := filepath.Join(workspace, "envelopes", taskUID, ".spans-emitted")
	if _, err := os.Stat(sentinelPath); !os.IsNotExist(err) {
		t.Errorf("os.Stat(sentinel) err = %v, want a not-exist error (D-05: skipped run writes no sentinel)", err)
	}

	if !strings.Contains(stderr.String(), "skip") {
		t.Errorf("stderr = %q, want a skip log line", stderr.String())
	}
}

// Test 11: TestRunTraceOnly_MissingEventsStillExitsZero — D-10: an empty
// workspace (no events.jsonl at all) still exits 0, emits zero spans, and
// logs an error line.
func TestRunTraceOnly_MissingEventsStillExitsZero(t *testing.T) {
	exp := tracetest.NewInMemoryExporter()
	installStubTracerProvider(t, exp)

	workspace := t.TempDir() // deliberately empty — no envelopes dir
	cfg := reporterConfig{TraceOnly: true, TaskUID: "task-trace-only-missing", Workspace: workspace}

	var stderr bytes.Buffer
	code := runWithClient(context.Background(), cfg, nil, &stderr, nil)
	if code != exitSuccess {
		t.Fatalf("runWithClient exit=%d, want exitSuccess (D-10); stderr=%q", code, stderr.String())
	}
	if len(exp.GetSpans()) != 0 {
		t.Errorf("got %d spans, want 0 (no events.jsonl present)", len(exp.GetSpans()))
	}
	if stderr.Len() == 0 {
		t.Error("expected an error line on stderr for missing events.jsonl, got none")
	}
}

// TestRunTraceOnly_PartialEventsStillEmitsSpans — D-11: an events.jsonl whose
// tail line exceeds the 16 MB per-line budget (bufio.ErrTooLong; the scanner
// cannot resume) still emits the calls reconstructed before the bad line —
// partial telemetry over none — with the run exiting 0 (D-10) and stderr
// citing the partial recovery.
func TestRunTraceOnly_PartialEventsStillEmitsSpans(t *testing.T) {
	exp := tracetest.NewInMemoryExporter()
	installStubTracerProvider(t, exp)

	workspace := t.TempDir()
	taskUID := "task-trace-only-partial"
	writeTraceOnlyFixture(t, workspace, taskUID) // 2 complete calls
	eventsPath := filepath.Join(workspace, "envelopes", taskUID, "events.jsonl")
	f, err := os.OpenFile(eventsPath, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatalf("OpenFile: %v", err)
	}
	if _, err := f.WriteString(strings.Repeat("x", 17*1024*1024) + "\n"); err != nil {
		t.Fatalf("append oversized line: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	cfg := reporterConfig{TraceOnly: true, TaskUID: taskUID, Workspace: workspace}

	var stderr bytes.Buffer
	code := runWithClient(context.Background(), cfg, nil, &stderr, nil)
	if code != exitSuccess {
		t.Fatalf("runWithClient exit=%d, want exitSuccess (D-10); stderr=%q", code, stderr.String())
	}
	if got := len(exp.GetSpans()); got != 2 {
		t.Errorf("got %d spans, want 2 (partial conversation emitted despite read error)", got)
	}
	if !strings.Contains(stderr.String(), "partial") {
		t.Errorf("stderr = %q, want a partial-recovery log line", stderr.String())
	}
}

// TestRunCombined_RetryDoesNotReemitSpans — the combined shape's
// BackoffLimit=2 retry loop: a failed planner Job (events.jsonl present,
// out.json missing → exit 2 AFTER synth) re-runs the reporter pod. The
// .spans-emitted sentinel written after the first successful EmitSpans must
// make the second attempt skip synthesis — otherwise every retry re-emits
// the full conversation and multi-counts llm.token_count.* costs in Phoenix.
func TestRunCombined_RetryDoesNotReemitSpans(t *testing.T) {
	exp := tracetest.NewInMemoryExporter()
	installStubTracerProvider(t, exp)

	workspace := t.TempDir()
	taskUID := "task-combined-retry"
	writeTraceOnlyFixture(t, workspace, taskUID) // events.jsonl present; out.json deliberately absent

	c := buildFakeClient(t, &tidev1alpha3.Milestone{
		ObjectMeta: metav1.ObjectMeta{Name: "m-retry", Namespace: "default"},
	})
	cfg := reporterConfig{
		Workspace: workspace, TaskUID: taskUID, ParentName: "m-retry",
		ParentNamespace: "default", ParentKind: "Milestone",
	}

	var stderr1 bytes.Buffer
	code := runWithClient(context.Background(), cfg, nil, &stderr1, c)
	if code != exitInvariant {
		t.Fatalf("first attempt exit=%d, want exitInvariant (missing out.json); stderr=%q", code, stderr1.String())
	}
	firstAttemptSpans := len(exp.GetSpans())
	if firstAttemptSpans != 2 {
		t.Fatalf("first attempt emitted %d spans, want 2", firstAttemptSpans)
	}
	sentinel := filepath.Join(workspace, "envelopes", taskUID, ".spans-emitted")
	if _, err := os.Stat(sentinel); err != nil {
		t.Fatalf("sentinel %q missing after successful emission: %v", sentinel, err)
	}

	// Simulated Job retry: same PVC state, fresh pod.
	var stderr2 bytes.Buffer
	code = runWithClient(context.Background(), cfg, nil, &stderr2, c)
	if code != exitInvariant {
		t.Fatalf("retry exit=%d, want exitInvariant; stderr=%q", code, stderr2.String())
	}
	if got := len(exp.GetSpans()); got != firstAttemptSpans {
		t.Errorf("retry re-emitted spans: total = %d, want still %d", got, firstAttemptSpans)
	}
	if !strings.Contains(stderr2.String(), "idempotent skip") {
		t.Errorf("retry stderr = %q, want an idempotent-skip log line", stderr2.String())
	}
}

// Test 12: TestRunCombined_SynthFailureDoesNotChangeExit — D-10: a combined
// (materialization) run with out.json present but events.jsonl ABSENT
// synthesizes nothing, logs the failure, and still exits exactly like
// TestRunHappyPath — materialization's outcome is authoritative.
func TestRunCombined_SynthFailureDoesNotChangeExit(t *testing.T) {
	exp := tracetest.NewInMemoryExporter()
	installStubTracerProvider(t, exp)

	workspace := t.TempDir()
	taskUID := "task-combined-no-events"
	parentName := "parent-combined-no-events"
	parentNS := "default"

	milestone := &tidev1alpha3.Milestone{
		ObjectMeta: metav1.ObjectMeta{
			Name: parentName, Namespace: parentNS, UID: types.UID("uid-combined-no-events"),
		},
	}
	envOut := pkgdispatch.EnvelopeOut{
		ChildCRDs: []pkgdispatch.ChildCRDSpec{
			{Kind: "Phase", Name: "phase-combined-no-events", Spec: phaseSpec(t, parentName)},
		},
	}
	writeOutJSON(t, workspace, taskUID, envOut) // out.json present; events.jsonl absent

	c := buildFakeClient(t, milestone)
	cfg := reporterConfig{
		Workspace: workspace, TaskUID: taskUID, ParentName: parentName,
		ParentNamespace: parentNS, ParentKind: "Milestone",
	}

	var stderr bytes.Buffer
	code := runWithClient(context.Background(), cfg, nil, &stderr, c)
	if code != exitSuccess {
		t.Fatalf("runWithClient exit=%d, want exitSuccess (synth failure must not change materialization outcome); stderr=%q",
			code, stderr.String())
	}

	var ph tidev1alpha3.Phase
	if err := c.Get(context.Background(), client.ObjectKey{Namespace: parentNS, Name: "phase-combined-no-events"}, &ph); err != nil {
		t.Errorf("Get Phase: %v (materialization should succeed despite missing events.jsonl)", err)
	}
}
