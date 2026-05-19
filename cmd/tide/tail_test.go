/*
Copyright 2026 TIDE Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
*/

// Plan 04-08 Task 3 — RED tests for `tide tail`. Tests the container-resolution
// heuristic, the flag default surface, the friendly "no running pod" error,
// the EOF→stderr-message exit-0 surface, and the signal-aware cancellation
// pattern.
//
// The tests inject deterministic resolvers (tailPodPicker) and stream funcs
// (tailStreamer) so the assertions don't depend on a live apiserver. The
// real apiserver streaming is exercised in plan 04-14's kind harness.
package main

import (
	"bytes"
	"context"
	"io"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	fakeclientset "k8s.io/client-go/kubernetes/fake"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	tidev1alpha1 "github.com/jsquirrelz/tide/api/v1alpha1"
)

func makeTaskForTail(name, jobName string, phase string) *tidev1alpha1.Task {
	return &tidev1alpha1.Task{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "default",
			UID:       types.UID("test-uid"),
		},
		Spec: tidev1alpha1.TaskSpec{
			PlanRef:             "my-plan",
			FilesTouched:        []string{"a.go"},
			DeclaredOutputPaths: []string{"/workspace/out/a"},
		},
		Status: tidev1alpha1.TaskStatus{Phase: phase},
	}
}

func TestTailDefaultsContainerSkipsCredproxy(t *testing.T) {
	// Container resolution heuristic — given a Pod with credproxy + subagent
	// containers, the default picker MUST select "subagent" (or whichever
	// is the non-credproxy/non-init-* container).
	got := pickContainer([]corev1.Container{
		{Name: "credproxy"},
		{Name: "subagent"},
	}, "")
	if got != "subagent" {
		t.Errorf("expected default to skip credproxy and pick 'subagent'; got %q", got)
	}

	// Explicit --container flag wins.
	got = pickContainer([]corev1.Container{
		{Name: "credproxy"},
		{Name: "subagent"},
	}, "credproxy")
	if got != "credproxy" {
		t.Errorf("explicit --container should win; got %q", got)
	}

	// init-* containers are also skipped.
	got = pickContainer([]corev1.Container{
		{Name: "init-clone"},
		{Name: "subagent"},
	}, "")
	if got != "subagent" {
		t.Errorf("expected init-clone skipped; got %q", got)
	}
}

func TestTailTaskNotFoundError(t *testing.T) {
	c := fake.NewClientBuilder().WithScheme(testScheme(t)).Build()
	cs := fakeclientset.NewSimpleClientset()
	var stdout, stderr bytes.Buffer
	err := tailRun(context.Background(), c, cs, "default", "missing-task", tailOptions{}, &stdout, &stderr)
	if err == nil {
		t.Fatal("expected NotFound error for missing Task; got nil")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected 'not found' in error; got %q", err.Error())
	}
}

func TestTailTaskNoRunningPodFriendlyError(t *testing.T) {
	// Task exists but has no Pod with the canonical
	// tideproject.k8s/task-uid label.
	tk := makeTaskForTail("my-task", "tide-task-test-uid-0", "Pending")
	c := fake.NewClientBuilder().WithScheme(testScheme(t)).WithObjects(tk).Build()
	cs := fakeclientset.NewSimpleClientset()
	var stdout, stderr bytes.Buffer
	err := tailRun(context.Background(), c, cs, "default", "my-task", tailOptions{}, &stdout, &stderr)
	if err == nil {
		t.Fatal("expected 'no running pod' error; got nil")
	}
	if !strings.Contains(err.Error(), "no running pod") {
		t.Errorf("expected 'no running pod' in error; got %q", err.Error())
	}
}

func TestTailContextCancellationReturnsWithin1s(t *testing.T) {
	// Pattern: spawn a goroutine that blocks on a fake stream until ctx
	// cancels; assert RunE returns within 1500ms after cancel.
	//
	// This exercises the actual ctx-aware Streamer signature without a
	// live apiserver — we inject a stub Streamer that respects ctx.Done().
	origStreamer := tailStreamer
	defer func() { tailStreamer = origStreamer }()
	var streamerCalled atomic.Bool
	tailStreamer = func(ctx context.Context, cs kubernetes.Interface, ns, pod, container string, opt tailOptions, out, errOut io.Writer) error {
		streamerCalled.Store(true)
		<-ctx.Done()
		return ctx.Err()
	}

	origPicker := tailPodPicker
	defer func() { tailPodPicker = origPicker }()
	tailPodPicker = func(ctx context.Context, k ctrlclient.Client, ns, taskName string, opt tailOptions) (string, string, error) {
		return "fake-pod", "subagent", nil
	}

	tk := makeTaskForTail("my-task", "tide-task-test-uid-0", "Running")
	c := fake.NewClientBuilder().WithScheme(testScheme(t)).WithObjects(tk).Build()
	cs := fakeclientset.NewSimpleClientset()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		var stdout, stderr bytes.Buffer
		done <- tailRun(ctx, c, cs, "default", "my-task", tailOptions{}, &stdout, &stderr)
	}()

	// Wait for the streamer goroutine to be entered — proves tailRun
	// actually flowed through to the stream phase rather than returning
	// early with a stub error.
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) && !streamerCalled.Load() {
		time.Sleep(10 * time.Millisecond)
	}
	if !streamerCalled.Load() {
		t.Fatal("tailRun did not invoke tailStreamer; RED stub likely returned early")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(1500 * time.Millisecond):
		t.Fatal("tailRun did not return within 1.5s of ctx cancel")
	}
}

func TestTailDefaultFlagValues(t *testing.T) {
	// Sanity: the cobra command exposes --container/-c, --tail, --timestamps/-t
	// with the expected defaults.
	c := newTailCmd()
	if f := c.Flags().Lookup("container"); f == nil {
		t.Error("expected --container flag")
	}
	if f := c.Flags().Lookup("tail"); f == nil {
		t.Error("expected --tail flag")
	} else if f.DefValue != "100" {
		t.Errorf("expected --tail default 100; got %q", f.DefValue)
	}
	if f := c.Flags().Lookup("timestamps"); f == nil {
		t.Error("expected --timestamps flag")
	} else if f.DefValue != "true" {
		t.Errorf("expected --timestamps default true; got %q", f.DefValue)
	}
	// Short forms.
	if f := c.Flags().ShorthandLookup("c"); f == nil || f.Name != "container" {
		t.Errorf("expected -c → --container; got %+v", f)
	}
	if f := c.Flags().ShorthandLookup("t"); f == nil || f.Name != "timestamps" {
		t.Errorf("expected -t → --timestamps; got %+v", f)
	}
}
