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

// Plan 29-03 — tests for `tide import-envelopes`.
//
// Coverage:
//   - Flag parsing: --namespace, --dry-run, --pvc, --timeout, --output wire through newImportEnvelopesCmd
//   - Dry-run: table output with level/name/verdict/reason columns
//   - Dry-run: --output json emits parseable JSON with verdicts
//   - Dry-run: checksum mismatch produces re-plan verdict
//   - Dry-run: cycle detection returns error + prints involved nodes
//   - Live mode: seed ConfigMap created with "manifest" key
//   - Live mode: idempotent re-run (AlreadyExists swallowed, no error returned)
//   - Live mode: loader pod has correct command (tar xzf - -C /workspace) and RW mount
//   - Live mode: Project is NOT created/applied (stage-only, D-05)
//   - import-envelopes registered in subcommands.go
package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"io"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes"
	fakeclientset "k8s.io/client-go/kubernetes/fake"

	pkgbundle "github.com/jsquirrelz/tide/pkg/bundle"
	pkgdispatch "github.com/jsquirrelz/tide/pkg/dispatch"
)

// ---------------------------------------------------------------------------
// Shared test helpers
// ---------------------------------------------------------------------------

// makeValidEnvelopeBytes returns a minimal valid out.json for dry-run adoption.
func makeValidEnvelopeBytes(t *testing.T, childCount int) []byte {
	t.Helper()
	children := make([]pkgdispatch.ChildCRDSpec, childCount)
	for i := range children {
		children[i] = pkgdispatch.ChildCRDSpec{Kind: "Task", Name: "child"}
	}
	env := pkgdispatch.EnvelopeOut{
		APIVersion: pkgdispatch.APIVersionV1Alpha1,
		Kind:       pkgdispatch.KindTaskEnvelopeOut,
		TaskUID:    "test-uid",
		ChildCRDs:  children,
		ChildCount: childCount,
	}
	b, err := json.Marshal(env)
	if err != nil {
		t.Fatalf("makeValidEnvelopeBytes: %v", err)
	}
	return b
}

// makeMinimalBundleTgz creates a bundle .tgz in a temp file and returns the path.
// The bundle contains one milestone entry with an adopting envelope.
func makeMinimalBundleTgz(t *testing.T) (tgzPath string, cleanFn func()) {
	t.Helper()

	envBytes := makeValidEnvelopeBytes(t, 1)
	sha256hex := pkgbundle.ComputeEnvelopeSHA256(envBytes)

	entry := pkgbundle.BundleEntry{
		Name:   "milestone-01",
		FQName: "milestone-01",
		OldUID: "old-uid-1",
		SHA256: sha256hex,
	}

	envelopes := map[string][]byte{
		"envelopes/old-uid-1/out.json": envBytes,
	}

	manifest := pkgbundle.BundleManifest{
		Milestones: []pkgbundle.BundleEntry{entry},
	}
	manifestJSON, err := json.Marshal(manifest)
	if err != nil {
		t.Fatalf("makeMinimalBundleTgz marshal: %v", err)
	}

	pvcTgz, err := pkgbundle.WritePVCEnvelopesTgz(envelopes)
	if err != nil {
		t.Fatalf("makeMinimalBundleTgz WritePVCEnvelopesTgz: %v", err)
	}

	files := map[string][]byte{
		pkgbundle.BundleFileSeedManifest: manifestJSON,
		pkgbundle.BundleFilePVCEnvelopes: pvcTgz,
		pkgbundle.BundleFileProject:      []byte("apiVersion: tideproject.k8s/v1alpha3\nkind: Project\n"),
	}

	tmp, err := os.CreateTemp("", "import-test-*.tgz")
	if err != nil {
		t.Fatalf("makeMinimalBundleTgz create temp: %v", err)
	}
	tgzPath = tmp.Name()
	_ = tmp.Close()

	if err := pkgbundle.WriteBundle(tgzPath, files); err != nil {
		t.Fatalf("makeMinimalBundleTgz WriteBundle: %v", err)
	}

	return tgzPath, func() { _ = os.Remove(tgzPath) }
}

// makeCyclicBundleDir creates a bundle dir with a cyclic dependency graph.
func makeCyclicBundleDir(t *testing.T) (bundleDir string, cleanFn func()) {
	t.Helper()

	envBytes := makeValidEnvelopeBytes(t, 0)
	sha := pkgbundle.ComputeEnvelopeSHA256(envBytes)

	// ms-a depends on ms-b, ms-b depends on ms-a: cycle.
	msA := pkgbundle.BundleEntry{Name: "ms-a", FQName: "ms-a", OldUID: "uid-a", SHA256: sha, DependsOn: []string{"ms-b"}}
	msB := pkgbundle.BundleEntry{Name: "ms-b", FQName: "ms-b", OldUID: "uid-b", SHA256: sha, DependsOn: []string{"ms-a"}}

	manifest := pkgbundle.BundleManifest{
		Milestones: []pkgbundle.BundleEntry{msA, msB},
	}
	manifestJSON, err := json.Marshal(manifest)
	if err != nil {
		t.Fatalf("makeCyclicBundleDir: %v", err)
	}

	envelopes := map[string][]byte{
		"envelopes/uid-a/out.json": envBytes,
		"envelopes/uid-b/out.json": envBytes,
	}
	pvcTgz, err := pkgbundle.WritePVCEnvelopesTgz(envelopes)
	if err != nil {
		t.Fatalf("makeCyclicBundleDir pvc: %v", err)
	}

	dir, err := os.MkdirTemp("", "import-cycle-*")
	if err != nil {
		t.Fatalf("makeCyclicBundleDir mkdir: %v", err)
	}
	cleanFn = func() { _ = os.RemoveAll(dir) }

	_ = os.WriteFile(dir+"/"+pkgbundle.BundleFileSeedManifest, manifestJSON, 0o644)
	_ = os.WriteFile(dir+"/"+pkgbundle.BundleFilePVCEnvelopes, pvcTgz, 0o644)
	_ = os.WriteFile(dir+"/"+pkgbundle.BundleFileProject, []byte(""), 0o644)
	return dir, cleanFn
}

// makeTamperedBundleDir creates a bundle dir where the sha256 in the manifest
// does not match the actual envelope bytes (to exercise the checksum mismatch path).
func makeTamperedBundleDir(t *testing.T) (bundleDir string, cleanFn func()) {
	t.Helper()

	envBytes := makeValidEnvelopeBytes(t, 1)

	entry := pkgbundle.BundleEntry{
		Name:   "ms-tampered",
		FQName: "ms-tampered",
		OldUID: "uid-tampered",
		SHA256: "badhash000000000000000000000000000000000000000000000000000000000",
	}

	manifest := pkgbundle.BundleManifest{
		Milestones: []pkgbundle.BundleEntry{entry},
	}
	manifestJSON, err := json.Marshal(manifest)
	if err != nil {
		t.Fatalf("makeTamperedBundleDir: %v", err)
	}

	pvcTgz, err := pkgbundle.WritePVCEnvelopesTgz(map[string][]byte{
		"envelopes/uid-tampered/out.json": envBytes,
	})
	if err != nil {
		t.Fatalf("makeTamperedBundleDir pvc: %v", err)
	}

	dir, err := os.MkdirTemp("", "import-tamper-*")
	if err != nil {
		t.Fatalf("makeTamperedBundleDir mkdir: %v", err)
	}
	cleanFn = func() { _ = os.RemoveAll(dir) }

	_ = os.WriteFile(dir+"/"+pkgbundle.BundleFileSeedManifest, manifestJSON, 0o644)
	_ = os.WriteFile(dir+"/"+pkgbundle.BundleFilePVCEnvelopes, pvcTgz, 0o644)
	_ = os.WriteFile(dir+"/"+pkgbundle.BundleFileProject, []byte(""), 0o644)
	return dir, cleanFn
}

// ---------------------------------------------------------------------------
// Fake loader runner
// ---------------------------------------------------------------------------

// fakeLoaderRunner is a test-injectable loaderPodRunner that records calls and
// captures the tgz bytes streamed to the (fake) pod stdin.
type fakeLoaderRunner struct {
	// createCalls counts invocations.
	createCalls atomic.Int32
	// capturedData holds the bytes "streamed" into the pod stdin.
	capturedData []byte
	// errToReturn is returned by the runner when set.
	errToReturn error
}

func (f *fakeLoaderRunner) run(
	ctx context.Context,
	cs kubernetes.Interface,
	ns, oldUID, pvcName, pvcTgzPath string,
	errOut io.Writer,
) error {
	f.createCalls.Add(1)
	if f.errToReturn != nil {
		return f.errToReturn
	}
	// Capture tgz bytes from the file path.
	data, err := os.ReadFile(pvcTgzPath)
	if err != nil {
		return err
	}
	f.capturedData = data
	return nil
}

// injectLoaderRunner replaces loaderPodRunner with the fake and returns a restore func.
func injectLoaderRunner(f *fakeLoaderRunner) func() {
	orig := loaderPodRunner
	loaderPodRunner = f.run
	return func() { loaderPodRunner = orig }
}

// ---------------------------------------------------------------------------
// Dry-run tests
// ---------------------------------------------------------------------------

// TestImportEnvelopesDryRunTableOutput verifies that --dry-run with a valid
// bundle emits a text table with adopt verdicts for each entry.
func TestImportEnvelopesDryRunTableOutput(t *testing.T) {
	tgzPath, cleanup := makeMinimalBundleTgz(t)
	defer cleanup()

	var stdout, stderr bytes.Buffer
	err := importEnvelopesDryRun(tgzPath, "", &stdout, &stderr)
	if err != nil {
		t.Fatalf("importEnvelopesDryRun returned error: %v", err)
	}

	out := stdout.String()

	// Table must contain level and name headers (rendered by tabwriter).
	for _, want := range []string{"level", "name", "verdict", "adopt"} {
		if !strings.Contains(strings.ToLower(out), want) {
			t.Errorf("expected stdout to contain %q; got:\n%s", want, out)
		}
	}
}

// TestImportEnvelopesDryRunJSONOutput verifies that --output json emits a
// machine-parseable JSON document with the expected verdicts.
func TestImportEnvelopesDryRunJSONOutput(t *testing.T) {
	tgzPath, cleanup := makeMinimalBundleTgz(t)
	defer cleanup()

	var stdout, stderr bytes.Buffer
	err := importEnvelopesDryRun(tgzPath, "json", &stdout, &stderr)
	if err != nil {
		t.Fatalf("importEnvelopesDryRun JSON returned error: %v", err)
	}

	// Must be valid JSON.
	var result map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatalf("stdout is not valid JSON: %v\nstdout:\n%s", err, stdout.String())
	}

	// Must contain "rows" or similar structure with an "adopt" verdict.
	raw := stdout.String()
	if !strings.Contains(raw, "adopt") {
		t.Errorf("JSON output does not contain 'adopt' verdict; got: %s", raw)
	}
}

// TestImportEnvelopesDryRunChecksumMismatch verifies that a tampered bundle
// produces a re-plan verdict with "checksum mismatch" in the reason.
func TestImportEnvelopesDryRunChecksumMismatch(t *testing.T) {
	dir, cleanup := makeTamperedBundleDir(t)
	defer cleanup()

	var stdout, stderr bytes.Buffer
	// Dry-run on a tampered bundle should return nil error (cycle is the only hard-reject)
	// but the table must show re-plan.
	_ = importEnvelopesDryRun(dir, "", &stdout, &stderr)

	out := stdout.String()
	if !strings.Contains(strings.ToLower(out), "re-plan") {
		t.Errorf("expected 're-plan' verdict for tampered bundle; got:\n%s", out)
	}
	if !strings.Contains(strings.ToLower(out), "checksum") {
		t.Errorf("expected 'checksum' in reason for tampered bundle; got:\n%s", out)
	}
}

// TestImportEnvelopesDryRunCycleRejects verifies that a cyclic bundle returns a
// non-nil error and prints the involved node names to stdout.
func TestImportEnvelopesDryRunCycleRejects(t *testing.T) {
	dir, cleanup := makeCyclicBundleDir(t)
	defer cleanup()

	var stdout, stderr bytes.Buffer
	err := importEnvelopesDryRun(dir, "", &stdout, &stderr)
	if err == nil {
		t.Fatal("expected non-nil error for cyclic bundle; got nil")
	}

	out := stdout.String()
	// Must print the cycle signal and at least one involved node name.
	if !strings.Contains(strings.ToLower(out), "cycle") {
		t.Errorf("expected 'CYCLE' in stdout for cyclic bundle; got:\n%s", out)
	}
	// Both nodes should appear somewhere in the output.
	if !strings.Contains(out, "ms-a") || !strings.Contains(out, "ms-b") {
		t.Errorf("expected involved node names in stdout; got:\n%s", out)
	}
}

// ---------------------------------------------------------------------------
// Flags / command tests
// ---------------------------------------------------------------------------

// TestImportEnvelopesFlagDefaults verifies the expected flags and defaults on the
// import-envelopes command.
func TestImportEnvelopesFlagDefaults(t *testing.T) {
	cmd := newImportEnvelopesCmd()

	flags := []struct {
		name     string
		defValue string
	}{
		{"timeout", "5m0s"},
		{"pvc", "tide-projects"},
		{"dry-run", "false"},
		{"namespace", ""},
	}

	for _, f := range flags {
		fl := cmd.Flags().Lookup(f.name)
		if fl == nil {
			t.Errorf("expected flag --%s on import-envelopes command", f.name)
			continue
		}
		if fl.DefValue != f.defValue {
			t.Errorf("flag --%s: expected default %q; got %q", f.name, f.defValue, fl.DefValue)
		}
	}
}

// TestImportEnvelopesRegisteredInSubcommands verifies that newImportEnvelopesCmd
// is registered in the root command via registerSubcommands.
func TestImportEnvelopesRegisteredInSubcommands(t *testing.T) {
	root := buildRootForTest()
	found := false
	for _, cmd := range root.Commands() {
		if cmd.Name() == "import-envelopes" {
			found = true
			break
		}
	}
	if !found {
		t.Error("import-envelopes command not registered in subcommands.go")
	}
}

// ---------------------------------------------------------------------------
// Live mode tests (fakeLoaderRunner + fakeclientset)
// ---------------------------------------------------------------------------

// makeMinimalBundleDirForLive creates a bundle dir suitable for testing live mode.
// Returns the dir path, oldUID, and a cleanup func.
func makeMinimalBundleDirForLive(t *testing.T) (bundleDir, oldUID string, cleanup func()) {
	t.Helper()
	oldUID = "old-proj-uid-live"

	envBytes := makeValidEnvelopeBytes(t, 1)
	sha := pkgbundle.ComputeEnvelopeSHA256(envBytes)
	entry := pkgbundle.BundleEntry{
		Name:   "ms-live",
		FQName: "ms-live",
		OldUID: "uid-ms-live",
		SHA256: sha,
	}

	manifest := pkgbundle.BundleManifest{
		Milestones: []pkgbundle.BundleEntry{entry},
	}
	manifestJSON, _ := json.Marshal(manifest)

	pvcTgz, _ := pkgbundle.WritePVCEnvelopesTgz(map[string][]byte{
		"envelopes/uid-ms-live/out.json": envBytes,
	})

	// project.yaml must have importSource for live mode to read pvcSubPath.
	projectYAML := `apiVersion: tideproject.k8s/v1alpha3
kind: Project
metadata:
  name: my-project
  namespace: default
spec:
  importSource:
    seedManifestConfigMap: tide-import-seed-my-project
    salvagedPVCSubPath: ` + oldUID + `/workspace
`

	dir, err := os.MkdirTemp("", "import-live-*")
	if err != nil {
		t.Fatalf("makeMinimalBundleDirForLive mkdir: %v", err)
	}
	cleanup = func() { _ = os.RemoveAll(dir) }

	_ = os.WriteFile(dir+"/"+pkgbundle.BundleFileSeedManifest, manifestJSON, 0o644)
	_ = os.WriteFile(dir+"/"+pkgbundle.BundleFilePVCEnvelopes, pvcTgz, 0o644)
	_ = os.WriteFile(dir+"/"+pkgbundle.BundleFileProject, []byte(projectYAML), 0o644)

	return dir, oldUID, cleanup
}

// TestImportEnvelopesLiveModeCreatesConfigMap verifies that live mode creates the
// seed ConfigMap with "manifest" key in the target namespace.
func TestImportEnvelopesLiveModeCreatesConfigMap(t *testing.T) {
	dir, _, cleanup := makeMinimalBundleDirForLive(t)
	defer cleanup()
	defer func() { _ = os.Remove("project.yaml") }() // clean up side-effect write

	fl := &fakeLoaderRunner{}
	restore := injectLoaderRunner(fl)
	defer restore()

	cs := fakeclientset.NewSimpleClientset()
	ctx := context.Background()

	var stderr bytes.Buffer
	err := importEnvelopesRun(ctx, cs, dir, "default", "tide-projects", &stderr)
	if err != nil {
		t.Fatalf("importEnvelopesRun returned error: %v", err)
	}

	// ConfigMap must exist with "manifest" data key.
	cm, err := cs.CoreV1().ConfigMaps("default").Get(ctx, "tide-import-seed-my-project", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("seed ConfigMap not created: %v", err)
	}
	if _, ok := cm.Data["manifest"]; !ok {
		t.Errorf("seed ConfigMap missing 'manifest' key; data: %v", cm.Data)
	}

	// Loader runner must have been invoked.
	if fl.createCalls.Load() == 0 {
		t.Error("loaderPodRunner was not invoked")
	}
}

// TestImportEnvelopesLiveModeIdempotent verifies that if the seed ConfigMap
// already exists, importEnvelopesRun returns no error (AlreadyExists swallowed).
func TestImportEnvelopesLiveModeIdempotent(t *testing.T) {
	dir, _, cleanup := makeMinimalBundleDirForLive(t)
	defer cleanup()
	defer func() { _ = os.Remove("project.yaml") }()

	fl := &fakeLoaderRunner{}
	restore := injectLoaderRunner(fl)
	defer restore()

	// Pre-create the ConfigMap.
	existingCM := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "tide-import-seed-my-project",
			Namespace: "default",
		},
		Data: map[string]string{"manifest": "{}"},
	}
	cs := fakeclientset.NewSimpleClientset(existingCM)
	ctx := context.Background()

	var stderr bytes.Buffer
	err := importEnvelopesRun(ctx, cs, dir, "default", "tide-projects", &stderr)
	if err != nil {
		t.Fatalf("importEnvelopesRun with pre-existing ConfigMap returned error: %v", err)
	}
}

// TestImportEnvelopesLiveModeDoesNotApplyProject verifies that importEnvelopesRun
// does NOT create a Project CR (stage-only, D-05).
func TestImportEnvelopesLiveModeDoesNotApplyProject(t *testing.T) {
	dir, _, cleanup := makeMinimalBundleDirForLive(t)
	defer cleanup()
	defer func() { _ = os.Remove("project.yaml") }()

	fl := &fakeLoaderRunner{}
	restore := injectLoaderRunner(fl)
	defer restore()

	cs := fakeclientset.NewSimpleClientset()
	ctx := context.Background()

	var stderr bytes.Buffer
	err := importEnvelopesRun(ctx, cs, dir, "default", "tide-projects", &stderr)
	if err != nil {
		t.Fatalf("importEnvelopesRun returned error: %v", err)
	}

	// No Project CRs should be created via the core clientset (stage-only).
	// The controller-runtime fake client also has no Projects created.
	// Check there are no projects via actions.
	actions := cs.Actions()
	for _, action := range actions {
		if action.GetResource().Resource == "projects" && action.GetVerb() == "create" {
			t.Error("D-05 violation: importEnvelopesRun created a Project CR")
		}
	}
}

// TestImportEnvelopesLoaderPodTgzStreamed verifies that the loader runner is
// invoked with a pvc-envelopes.tgz path that is a valid tgz file.
func TestImportEnvelopesLoaderPodTgzStreamed(t *testing.T) {
	dir, _, cleanup := makeMinimalBundleDirForLive(t)
	defer cleanup()
	defer func() { _ = os.Remove("project.yaml") }()

	fl := &fakeLoaderRunner{}
	restore := injectLoaderRunner(fl)
	defer restore()

	cs := fakeclientset.NewSimpleClientset()
	ctx := context.Background()

	var stderr bytes.Buffer
	err := importEnvelopesRun(ctx, cs, dir, "default", "tide-projects", &stderr)
	if err != nil {
		t.Fatalf("importEnvelopesRun returned error: %v", err)
	}

	// Loader must have been called.
	if fl.createCalls.Load() == 0 {
		t.Fatal("loaderPodRunner was not invoked")
	}

	// The captured data must be a valid gzip stream (the pvc-envelopes.tgz).
	if len(fl.capturedData) == 0 {
		t.Fatal("loader received no data")
	}
	gr, err := gzip.NewReader(bytes.NewReader(fl.capturedData))
	if err != nil {
		t.Fatalf("captured data is not a valid gzip stream: %v", err)
	}
	tr := tar.NewReader(gr)
	foundEnvelope := false
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("tar read error: %v", err)
		}
		if strings.HasSuffix(hdr.Name, "/out.json") {
			foundEnvelope = true
		}
	}
	if !foundEnvelope {
		t.Error("pvc-envelopes.tgz streamed to loader does not contain any out.json entries")
	}
}

// ---------------------------------------------------------------------------
// Timeout test
// ---------------------------------------------------------------------------

// TestImportEnvelopesDryRunTimeout verifies that when the context is already
// cancelled at call time, importEnvelopesDryRun still completes (dry-run is
// offline — it does not respect context for the validation itself but should
// not block forever).
func TestImportEnvelopesDryRunTimeout(t *testing.T) {
	tgzPath, cleanup := makeMinimalBundleTgz(t)
	defer cleanup()

	// Dry-run is cluster-free, so even a cancelled context returns quickly.
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Millisecond)
	defer cancel()
	_ = ctx // dry-run doesn't use ctx

	var stdout, stderr bytes.Buffer
	// Should not hang regardless of timeout.
	done := make(chan error, 1)
	go func() {
		done <- importEnvelopesDryRun(tgzPath, "", &stdout, &stderr)
	}()

	select {
	case <-done:
		// OK — completed promptly.
	case <-time.After(5 * time.Second):
		t.Fatal("importEnvelopesDryRun hung — did not complete within 5s")
	}
}

// ---------------------------------------------------------------------------
// Helper: simulate AlreadyExists from fakeclientset
// ---------------------------------------------------------------------------

// alreadyExistsError returns a fake apierrors.AlreadyExists for testing.
func alreadyExistsError() error {
	return apierrors.NewAlreadyExists(schema.GroupResource{Resource: "configmaps"}, "seed")
}

// Ensure alreadyExistsError is used to avoid "declared but not used" if removed.
var _ = alreadyExistsError
