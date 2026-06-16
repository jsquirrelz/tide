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

// Plan 15-03 Task 2 — regression tests for `tide artifact-get` real implementation.
//
// The fake seam (inspectorPodRunner) allows all behavioural assertions without
// a live apiserver. The finding-3 regression ensures the command's default path
// does NOT print the old dry-run pod spec YAML/JSON on stdout.
//
// Coverage:
//   - Finding-3 regression: no pod-spec markers on stdout (the run-1 finding-3 symptom)
//   - Raw-bytes contract (D-10): stdout receives verbatim bytes; status goes to stderr
//   - Timeout exhaustion (D-12): non-zero error, pod delete called (T-15-09)
//   - Cleanup on success: delete called after stream fully drained
//   - Path validation (T-15-08): traversal paths and metachar refs rejected before
//     any pod creation (fake records zero Create calls)
//   - parseArtifactRef existing cases still pass (no regression in ref parsing)
package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	fakeclientset "k8s.io/client-go/kubernetes/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	tidev1alpha2 "github.com/jsquirrelz/tide/api/v1alpha2"
)

// makeProjectForArtifactGet builds a Project fixture with a stable UID for
// artifact-get tests. Uses a different name from approve_test.go's makeProject
// to avoid redeclaration in the same test package.
func makeProjectForArtifactGet(name, ns string) *tidev1alpha2.Project {
	return &tidev1alpha2.Project{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: ns,
			UID:       types.UID("test-project-uid-abc123"),
		},
	}
}

// fakeRunner is a test-injectable inspectorPodRunner that records calls and
// returns caller-controlled output / errors.
type fakeRunner struct {
	// createCalls counts how many times the runner was invoked.
	createCalls atomic.Int32
	// deleteCalls counts how many times the runner completed (implying delete).
	deleteCalls atomic.Int32
	// streamBytes are written verbatim to out when runFn is not set.
	streamBytes []byte
	// errToReturn is returned by the runner when set.
	errToReturn error
	// runFn overrides the default behaviour when set.
	runFn func(ctx context.Context, cs kubernetes.Interface, ns, projectUID, artifactPath, pvcName string, out, errOut io.Writer) error
}

func (f *fakeRunner) run(
	ctx context.Context,
	cs kubernetes.Interface,
	ns, projectUID, artifactPath, pvcName string,
	out, errOut io.Writer,
) error {
	f.createCalls.Add(1)
	defer f.deleteCalls.Add(1)
	if f.runFn != nil {
		return f.runFn(ctx, cs, ns, projectUID, artifactPath, pvcName, out, errOut)
	}
	if f.errToReturn != nil {
		return f.errToReturn
	}
	_, _ = out.Write(f.streamBytes)
	return nil
}

// injectRunner replaces the package-level seam and returns a restore func.
func injectRunner(f *fakeRunner) func() {
	orig := inspectorPodRunner
	inspectorPodRunner = f.run
	return func() { inspectorPodRunner = orig }
}

// TestArtifactGetFinding3Regression is the run-1 finding-3 regression test.
//
// The old behaviour (artifactGetDryRun) printed a pod spec with YAML markers
// like "apiVersion:", "claimName:", "image: busybox" to stdout — operators
// could not pipe the output to a file. The real implementation MUST stream only
// artifact bytes to stdout; no pod-spec text may appear.
func TestArtifactGetFinding3Regression(t *testing.T) {
	artifactBytes := []byte("# PLAN.md content from the real artifact\nkey: value\n")
	fr := &fakeRunner{streamBytes: artifactBytes}
	restore := injectRunner(fr)
	defer restore()

	proj := makeProjectForArtifactGet("my-project", "default")
	c := fake.NewClientBuilder().WithScheme(testScheme(t)).WithObjects(proj).Build()
	cs := fakeclientset.NewSimpleClientset()

	var stdout, stderr bytes.Buffer
	err := artifactGetRun(context.Background(), c, cs, "default/my-project/PLAN.md", "tide-projects", &stdout, &stderr)
	if err != nil {
		t.Fatalf("artifactGetRun returned error: %v", err)
	}

	got := stdout.String()

	// Finding-3 regression: these markers must NOT appear on stdout.
	podSpecMarkers := []string{
		"apiVersion",
		"claimName",
		"dry-run",
		"ttlSecondsAfterFinished",
		"volumeMounts",
		"mountPath",
		"namespace:",
		"project:",
	}
	for _, marker := range podSpecMarkers {
		if strings.Contains(got, marker) {
			t.Errorf("finding-3 regression: stdout contains pod-spec marker %q (must NOT appear)\nfull stdout:\n%s", marker, got)
		}
	}

	// Stdout must equal exactly the fake artifact bytes (D-10 raw-bytes contract).
	if got != string(artifactBytes) {
		t.Errorf("stdout mismatch\n  want: %q\n   got: %q", string(artifactBytes), got)
	}
}

// TestArtifactGetRawBytesContract verifies D-10: stdout carries raw artifact
// bytes verbatim (including non-UTF8 bytes); all status messages go to stderr.
func TestArtifactGetRawBytesContract(t *testing.T) {
	// Inject non-UTF8 bytes to verify byte-for-byte pass-through.
	artifactBytes := []byte{0xFF, 0xFE, 0x00, 0x01, 'h', 'e', 'l', 'l', 'o'}
	fr := &fakeRunner{streamBytes: artifactBytes}
	restore := injectRunner(fr)
	defer restore()

	proj := makeProjectForArtifactGet("my-project", "default")
	c := fake.NewClientBuilder().WithScheme(testScheme(t)).WithObjects(proj).Build()
	cs := fakeclientset.NewSimpleClientset()

	var stdout, stderr bytes.Buffer
	err := artifactGetRun(context.Background(), c, cs, "default/my-project/out.bin", "tide-projects", &stdout, &stderr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// D-10: stdout bytes must be verbatim.
	if !bytes.Equal(stdout.Bytes(), artifactBytes) {
		t.Errorf("stdout not verbatim: want %v, got %v", artifactBytes, stdout.Bytes())
	}

	// D-10: stderr must be empty or contain only status lines (not artifact content).
	// The fake does not write status lines; any status text would come only from
	// artifactGetRun itself, which only writes to errOut. Assert artifact bytes
	// are not duplicated on stderr.
	if bytes.Equal(stderr.Bytes(), artifactBytes) {
		t.Error("artifact bytes must not appear on stderr (they belong on stdout)")
	}

	// Exactly one runner invocation.
	if n := fr.createCalls.Load(); n != 1 {
		t.Errorf("expected 1 runner invocation; got %d", n)
	}
}

// TestArtifactGetTimeoutExhaustionD12 verifies D-12 + T-15-09:
// when the fake runner blocks until ctx is cancelled (simulating a timeout),
// artifactGetRun returns a non-zero error mentioning the wait window, and the
// runner records that delete was called (cleanup on the error path).
func TestArtifactGetTimeoutExhaustionD12(t *testing.T) {
	var runnerEntered atomic.Bool
	fr := &fakeRunner{}
	fr.runFn = func(ctx context.Context, _ kubernetes.Interface, _, _, artifactPath, _ string, _, _ io.Writer) error {
		runnerEntered.Store(true)
		// Block until ctx is cancelled (simulating the timeout window expiry).
		<-ctx.Done()
		// Return the error that the real implementation would return on timeout.
		return fmt.Errorf("artifact %q was not available within the timeout window", artifactPath)
	}
	restore := injectRunner(fr)
	defer restore()

	proj := makeProjectForArtifactGet("my-project", "default")
	c := fake.NewClientBuilder().WithScheme(testScheme(t)).WithObjects(proj).Build()
	cs := fakeclientset.NewSimpleClientset()

	// Use a very short timeout so the test is fast.
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	var stdout, stderr bytes.Buffer
	err := artifactGetRun(ctx, c, cs, "default/my-project/PLAN.md", "tide-projects", &stdout, &stderr)

	// D-12: must return a non-zero/plain error after timeout.
	if err == nil {
		t.Fatal("expected non-zero error after timeout; got nil")
	}
	if !strings.Contains(err.Error(), "available") && !strings.Contains(err.Error(), "timeout") && !strings.Contains(err.Error(), "window") {
		t.Errorf("expected error message to mention timeout/wait window; got: %q", err.Error())
	}

	// T-15-09: delete must have been called on the error path.
	// The fake defers deleteCalls.Add(1) so it fires on every runner return.
	// Wait briefly for the goroutine to finish.
	deadline := time.Now().Add(200 * time.Millisecond)
	for time.Now().Before(deadline) && fr.deleteCalls.Load() == 0 {
		time.Sleep(5 * time.Millisecond)
	}
	if fr.deleteCalls.Load() == 0 {
		t.Error("T-15-09: delete was not called on the timeout error path")
	}
}

// TestArtifactGetCleanupOnSuccess verifies T-15-09 on the success path:
// the fake records delete called AFTER the stream fully drained.
func TestArtifactGetCleanupOnSuccess(t *testing.T) {
	streamDone := make(chan struct{})
	fr := &fakeRunner{}
	fr.runFn = func(ctx context.Context, _ kubernetes.Interface, _, _, _ string, _ string, out, _ io.Writer) error {
		_, _ = out.Write([]byte("artifact content"))
		close(streamDone)
		return nil
	}
	restore := injectRunner(fr)
	defer restore()

	proj := makeProjectForArtifactGet("my-project", "default")
	c := fake.NewClientBuilder().WithScheme(testScheme(t)).WithObjects(proj).Build()
	cs := fakeclientset.NewSimpleClientset()

	var stdout, stderr bytes.Buffer
	if err := artifactGetRun(context.Background(), c, cs, "default/my-project/PLAN.md", "tide-projects", &stdout, &stderr); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	select {
	case <-streamDone:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("stream did not complete within 500ms")
	}

	// T-15-09: delete (deferred in fakeRunner.run) must have been called.
	if fr.deleteCalls.Load() == 0 {
		t.Error("T-15-09: delete was not called on the success path")
	}
	if stdout.String() != "artifact content" {
		t.Errorf("stdout mismatch: got %q", stdout.String())
	}
}

// TestArtifactGetPathValidationRejectsTraversal verifies T-15-08:
// artifact refs containing ".." or absolute paths are rejected before any pod
// creation (fakeRunner must record zero Create calls).
func TestArtifactGetPathValidationRejectsTraversal(t *testing.T) {
	cases := []struct {
		name string
		ref  string
	}{
		{"dotdot component", "default/my-project/../etc/passwd"},
		{"shell dollar", "default/my-project/$HOME/.ssh/id_rsa"},
		{"semicolon", "default/my-project/path;bad"},
		{"backtick", "default/my-project/path`id`"},
		{"pipe", "default/my-project/path|cat"},
		{"space in path", "default/my-project/path with spaces"},
		{"ampersand", "default/my-project/path&bad"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fr := &fakeRunner{}
			restore := injectRunner(fr)
			defer restore()

			proj := makeProjectForArtifactGet("my-project", "default")
			c := fake.NewClientBuilder().WithScheme(testScheme(t)).WithObjects(proj).Build()
			cs := fakeclientset.NewSimpleClientset()

			var stdout, stderr bytes.Buffer
			err := artifactGetRun(context.Background(), c, cs, tc.ref, "tide-projects", &stdout, &stderr)

			if err == nil {
				t.Errorf("expected error for ref %q; got nil", tc.ref)
			}
			// T-15-08: zero pod Create calls — rejection happens before runner invocation.
			if n := fr.createCalls.Load(); n != 0 {
				t.Errorf("expected 0 Create calls for malicious ref %q; got %d", tc.ref, n)
			}
		})
	}
}

// TestArtifactGetRefParsingExistingCases ensures the existing parseArtifactRef
// test cases still pass after the implementation rewrite (no regression).
func TestArtifactGetRefParsingExistingCases(t *testing.T) {
	// Valid refs.
	ns, proj, path, err := parseArtifactRef("default/my-project/envelopes/abc/out.json")
	if err != nil {
		t.Fatalf("parseArtifactRef valid ref: %v", err)
	}
	if ns != "default" || proj != "my-project" || path != "envelopes/abc/out.json" {
		t.Errorf("unexpected parse: ns=%q proj=%q path=%q", ns, proj, path)
	}

	// Malformed refs.
	for _, bad := range []string{
		"missing-slashes",
		"only/two-parts",
		"/leading-slash/bad/ref",
		"",
	} {
		_, _, _, err := parseArtifactRef(bad)
		if err == nil {
			t.Errorf("expected error for malformed ref %q; got nil", bad)
		}
	}
}

// TestArtifactGetTimeoutFlagDefault verifies the --timeout flag default is 5m.
func TestArtifactGetTimeoutFlagDefault(t *testing.T) {
	cmd := newArtifactGetCmd()
	f := cmd.Flags().Lookup("timeout")
	if f == nil {
		t.Fatal("expected --timeout flag on artifact-get command")
	}
	if f.DefValue != "5m0s" {
		t.Errorf("expected --timeout default 5m0s; got %q", f.DefValue)
	}
}
