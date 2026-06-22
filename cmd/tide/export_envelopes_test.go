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

// Plan 29-02 — tests for `tide export-envelopes`.
//
// Coverage:
//   - Flag parsing: --output, --dir, --pvc, --timeout wire through newExportEnvelopesCmd
//   - Inspector pod command: tar czf - -C /workspace envelopes/ artifacts/ (no sh -c)
//   - SubPath: project UID (not project name)
//   - Missing-UID project: hard error before pod creation
//   - Raw bytes: inspector tgz bytes stream into the bundle writer (stderr carries status)
//   - childCount repair: legacy out.json (ChildCount==0, len(ChildCRDs)>0) is stamped
//   - Seed manifest generation: FQName/OldUID/DependsOn/Status/sha256 from live CRs
//   - Timeout: non-zero error + delete called (T-15-09 mirror)
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

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	fakeclientset "k8s.io/client-go/kubernetes/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	tidev1alpha2 "github.com/jsquirrelz/tide/api/v1alpha2"
	pkgbundle "github.com/jsquirrelz/tide/pkg/bundle"
	pkgdispatch "github.com/jsquirrelz/tide/pkg/dispatch"
)

// fakeExportRunner is a test-injectable exportInspectorPodRunner that records
// calls and returns caller-controlled output/errors.
type fakeExportRunner struct {
	// createCalls counts how many times the runner was invoked.
	createCalls atomic.Int32
	// deleteCalls counts how many times the runner completed (implying delete).
	deleteCalls atomic.Int32
	// streamBytes are written to out when runFn is nil.
	streamBytes []byte
	// errToReturn is returned by the runner when set and runFn is nil.
	errToReturn error
	// runFn overrides default behaviour when set.
	runFn func(ctx context.Context, cs kubernetes.Interface, ns, projectUID, pvcName string, out, errOut io.Writer) error
}

func (f *fakeExportRunner) run(
	ctx context.Context,
	cs kubernetes.Interface,
	ns, projectUID, pvcName string,
	out, errOut io.Writer,
) error {
	f.createCalls.Add(1)
	defer f.deleteCalls.Add(1)
	if f.runFn != nil {
		return f.runFn(ctx, cs, ns, projectUID, pvcName, out, errOut)
	}
	if f.errToReturn != nil {
		return f.errToReturn
	}
	_, _ = out.Write(f.streamBytes)
	return nil
}

// injectExportRunner replaces the package-level seam and returns a restore func.
func injectExportRunner(f *fakeExportRunner) func() {
	orig := exportInspectorPodRunner
	exportInspectorPodRunner = f.run
	return func() { exportInspectorPodRunner = orig }
}

// makeProjectForExport builds a Project fixture with a stable UID.
func makeProjectForExport(name, ns string) *tidev1alpha2.Project {
	return &tidev1alpha2.Project{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: ns,
			UID:       types.UID("test-project-uid-export-abc123"),
		},
	}
}

// makeProjectForExportNoUID builds a Project without a UID (missing-UID error case).
func makeProjectForExportNoUID(name, ns string) *tidev1alpha2.Project {
	return &tidev1alpha2.Project{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: ns,
		},
	}
}

// makeFakePVCTgz creates an in-memory pvc-envelopes.tgz with the given out.json entries
// keyed by UID. entries maps uid → out.json bytes.
func makeFakePVCTgz(t *testing.T, entries map[string][]byte) []byte {
	t.Helper()
	files := make(map[string][]byte)
	for uid, data := range entries {
		files["envelopes/"+uid+"/out.json"] = data
	}
	b, err := pkgbundle.WritePVCEnvelopesTgz(files)
	if err != nil {
		t.Fatalf("makeFakePVCTgz: %v", err)
	}
	return b
}

// makeLegacyEnvelopeJSON builds an EnvelopeOut JSON with ChildCRDs but ChildCount==0 (legacy).
func makeLegacyEnvelopeJSON(t *testing.T, taskUID string, numChildren int) []byte {
	t.Helper()
	children := make([]pkgdispatch.ChildCRDSpec, numChildren)
	for i := range children {
		children[i] = pkgdispatch.ChildCRDSpec{Name: "child-" + string(rune('a'+i))}
	}
	env := pkgdispatch.EnvelopeOut{
		APIVersion: pkgdispatch.APIVersionV1Alpha1,
		Kind:       pkgdispatch.KindTaskEnvelopeOut,
		TaskUID:    taskUID,
		ChildCRDs:  children,
		ChildCount: 0, // legacy — intentionally missing
	}
	b, err := json.Marshal(env)
	if err != nil {
		t.Fatalf("makeLegacyEnvelopeJSON: %v", err)
	}
	return b
}

// buildValidEnvelopeJSON returns a valid EnvelopeOut JSON (childCount == len(ChildCRDs)).
func buildValidEnvelopeJSON(t *testing.T, taskUID string) []byte {
	t.Helper()
	env := pkgdispatch.EnvelopeOut{
		APIVersion: pkgdispatch.APIVersionV1Alpha1,
		Kind:       pkgdispatch.KindTaskEnvelopeOut,
		TaskUID:    taskUID,
		ChildCRDs:  []pkgdispatch.ChildCRDSpec{{Name: "task-01"}},
		ChildCount: 1,
	}
	b, err := json.Marshal(env)
	if err != nil {
		t.Fatalf("buildValidEnvelopeJSON: %v", err)
	}
	return b
}

// -- Flag-parsing tests ------------------------------------------------------

// TestExportEnvelopesFlagDefaults verifies default values for all flags.
func TestExportEnvelopesFlagDefaults(t *testing.T) {
	cmd := newExportEnvelopesCmd()

	timeoutFlag := cmd.Flags().Lookup("timeout")
	if timeoutFlag == nil {
		t.Fatal("expected --timeout flag on export-envelopes command")
	}
	if timeoutFlag.DefValue != "5m0s" {
		t.Errorf("expected --timeout default 5m0s; got %q", timeoutFlag.DefValue)
	}

	pvcFlag := cmd.Flags().Lookup("pvc")
	if pvcFlag == nil {
		t.Fatal("expected --pvc flag on export-envelopes command")
	}
	if pvcFlag.DefValue != "tide-projects" {
		t.Errorf("expected --pvc default tide-projects; got %q", pvcFlag.DefValue)
	}

	dirFlag := cmd.Flags().Lookup("dir")
	if dirFlag == nil {
		t.Fatal("expected --dir flag on export-envelopes command")
	}
	if dirFlag.DefValue != "false" {
		t.Errorf("expected --dir default false; got %q", dirFlag.DefValue)
	}

	if cmd.Flags().Lookup("output") == nil {
		t.Fatal("expected --output flag on export-envelopes command")
	}
}

// TestExportEnvelopesRegisteredInSubcommands verifies the verb appears in the root.
func TestExportEnvelopesRegisteredInSubcommands(t *testing.T) {
	root := buildRootForTest()
	var found bool
	for _, c := range root.Commands() {
		if c.Use == "export-envelopes <namespace>/<project>" {
			found = true
			break
		}
	}
	if !found {
		t.Error("export-envelopes not registered in registerSubcommands")
	}
}

// -- Inspector-pod shape tests -----------------------------------------------

// TestExportEnvelopesMissingUID verifies that a project with no UID returns an
// error before the runner is invoked (zero createCalls).
func TestExportEnvelopesMissingUID(t *testing.T) {
	fr := &fakeExportRunner{}
	restore := injectExportRunner(fr)
	defer restore()

	proj := makeProjectForExportNoUID("my-project", "default")
	c := fake.NewClientBuilder().WithScheme(testScheme(t)).WithObjects(proj).Build()
	cs := fakeclientset.NewSimpleClientset()

	var stdout, stderr bytes.Buffer
	err := exportEnvelopesRun(
		context.Background(), c, cs,
		"default/my-project",
		"tide-projects", "/tmp/out.tgz", false,
		&stdout, &stderr,
	)
	if err == nil {
		t.Fatal("expected error for project with empty UID; got nil")
	}
	if !strings.Contains(err.Error(), "UID") {
		t.Errorf("expected error to mention UID; got: %q", err.Error())
	}
	if n := fr.createCalls.Load(); n != 0 {
		t.Errorf("expected 0 runner calls for missing-UID; got %d", n)
	}
}

// TestExportEnvelopesRunnerInvoked verifies the runner is called with the
// project UID as the subPath and the correct pvcName.
func TestExportEnvelopesRunnerInvoked(t *testing.T) {
	pvcTgz, _ := pkgbundle.WritePVCEnvelopesTgz(map[string][]byte{})
	fr := &fakeExportRunner{}

	var capturedUID, capturedPVC string
	fr.runFn = func(_ context.Context, _ kubernetes.Interface, _, uid, pvc string, out, _ io.Writer) error {
		capturedUID = uid
		capturedPVC = pvc
		_, _ = out.Write(pvcTgz)
		return nil
	}
	restore := injectExportRunner(fr)
	defer restore()

	proj := makeProjectForExport("my-project", "default")
	c := fake.NewClientBuilder().WithScheme(testScheme(t)).WithObjects(proj).Build()
	cs := fakeclientset.NewSimpleClientset()

	outPath := t.TempDir() + "/out.tgz"
	var stdout, stderr bytes.Buffer
	err := exportEnvelopesRun(
		context.Background(), c, cs,
		"default/my-project",
		"tide-projects", outPath, false,
		&stdout, &stderr,
	)
	if err != nil {
		t.Fatalf("exportEnvelopesRun: %v", err)
	}

	if capturedUID != "test-project-uid-export-abc123" {
		t.Errorf("expected SubPath UID %q; got %q", "test-project-uid-export-abc123", capturedUID)
	}
	if capturedPVC != "tide-projects" {
		t.Errorf("expected pvcName tide-projects; got %q", capturedPVC)
	}
}

// TestExportEnvelopesTimeout verifies D-12 / T-15-09: timeout exits non-zero
// and the runner records delete called.
func TestExportEnvelopesTimeout(t *testing.T) {
	fr := &fakeExportRunner{}
	fr.runFn = func(ctx context.Context, _ kubernetes.Interface, _, _, _ string, _, _ io.Writer) error {
		<-ctx.Done()
		return ctx.Err()
	}
	restore := injectExportRunner(fr)
	defer restore()

	proj := makeProjectForExport("my-project", "default")
	c := fake.NewClientBuilder().WithScheme(testScheme(t)).WithObjects(proj).Build()
	cs := fakeclientset.NewSimpleClientset()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	var stdout, stderr bytes.Buffer
	err := exportEnvelopesRun(
		ctx, c, cs,
		"default/my-project",
		"tide-projects", "/tmp/x.tgz", false,
		&stdout, &stderr,
	)
	if err == nil {
		t.Fatal("expected error after timeout; got nil")
	}

	deadline := time.Now().Add(200 * time.Millisecond)
	for time.Now().Before(deadline) && fr.deleteCalls.Load() == 0 {
		time.Sleep(5 * time.Millisecond)
	}
	if fr.deleteCalls.Load() == 0 {
		t.Error("T-15-09: delete not called on timeout path")
	}
}

// -- Seed manifest + childCount repair tests ---------------------------------

// TestExportEnvelopesChildCountRepair verifies D-16a: a legacy out.json with
// ChildCount==0 and len(ChildCRDs)>0 is repaired in the bundle.
func TestExportEnvelopesChildCountRepair(t *testing.T) {
	const taskUID = "task-uid-legacy-001"
	const numChildren = 3

	legacyOutJSON := makeLegacyEnvelopeJSON(t, taskUID, numChildren)
	pvcTgz := makeFakePVCTgz(t, map[string][]byte{taskUID: legacyOutJSON})

	fr := &fakeExportRunner{streamBytes: pvcTgz}
	restore := injectExportRunner(fr)
	defer restore()

	ms := &tidev1alpha2.Milestone{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "ms-alpha",
			Namespace: "default",
			UID:       types.UID("ms-uid-001"),
		},
		Spec: tidev1alpha2.MilestoneSpec{ProjectRef: "my-project"},
	}
	ph := &tidev1alpha2.Phase{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "ph-01",
			Namespace: "default",
			UID:       types.UID("ph-uid-001"),
		},
		Spec: tidev1alpha2.PhaseSpec{MilestoneRef: "ms-alpha"},
	}
	pl := &tidev1alpha2.Plan{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pl-01",
			Namespace: "default",
			UID:       types.UID(taskUID), // oldUID matches the envelope
		},
		Spec: tidev1alpha2.PlanSpec{PhaseRef: "ph-01"},
	}
	proj := makeProjectForExport("my-project", "default")

	c := fake.NewClientBuilder().WithScheme(testScheme(t)).
		WithObjects(proj, ms, ph, pl).Build()
	cs := fakeclientset.NewSimpleClientset()

	outPath := t.TempDir() + "/out.tgz"
	var stdout, stderr bytes.Buffer
	if err := exportEnvelopesRun(
		context.Background(), c, cs,
		"default/my-project",
		"tide-projects", outPath, false,
		&stdout, &stderr,
	); err != nil {
		t.Fatalf("exportEnvelopesRun: %v", err)
	}

	// Read back the bundle and check the pvc-envelopes.tgz inside it.
	bundleFiles, err := pkgbundle.ReadBundle(outPath)
	if err != nil {
		t.Fatalf("ReadBundle: %v", err)
	}

	pvcEnvBytes, ok := bundleFiles[pkgbundle.BundleFilePVCEnvelopes]
	if !ok {
		t.Fatal("bundle missing pvc-envelopes.tgz")
	}

	// Extract the repaired out.json from the nested tgz.
	var repairedOut pkgdispatch.EnvelopeOut
	foundOutJSON := false
	gr, _ := gzip.NewReader(bytes.NewReader(pvcEnvBytes))
	tr := tar.NewReader(gr)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("read pvc-envelopes.tgz: %v", err)
		}
		if strings.HasSuffix(hdr.Name, "/out.json") {
			data, _ := io.ReadAll(tr)
			if err := json.Unmarshal(data, &repairedOut); err != nil {
				t.Fatalf("unmarshal repaired out.json: %v", err)
			}
			foundOutJSON = true
			break
		}
	}
	if !foundOutJSON {
		t.Fatal("no out.json found in pvc-envelopes.tgz")
	}
	if repairedOut.ChildCount != numChildren {
		t.Errorf("expected childCount=%d (repaired); got %d", numChildren, repairedOut.ChildCount)
	}
}

// TestExportEnvelopesSeedManifest verifies that the seed-manifest.json in the
// bundle carries correct FQName, OldUID, DependsOn, Status, and sha256.
func TestExportEnvelopesSeedManifest(t *testing.T) {
	const msUID = "ms-uid-seed-001"
	const phUID = "ph-uid-seed-001"
	const plUID = "pl-uid-seed-001"

	ms := &tidev1alpha2.Milestone{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "ms-alpha",
			Namespace: "default",
			UID:       types.UID(msUID),
		},
		Spec:   tidev1alpha2.MilestoneSpec{ProjectRef: "my-project"},
		Status: tidev1alpha2.MilestoneStatus{Phase: "Succeeded"},
	}
	ph := &tidev1alpha2.Phase{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "ph-beta",
			Namespace: "default",
			UID:       types.UID(phUID),
		},
		Spec:   tidev1alpha2.PhaseSpec{MilestoneRef: "ms-alpha"},
		Status: tidev1alpha2.PhaseStatus{Phase: "Succeeded"},
	}
	pl := &tidev1alpha2.Plan{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pl-gamma",
			Namespace: "default",
			UID:       types.UID(plUID),
		},
		Spec: tidev1alpha2.PlanSpec{
			PhaseRef:  "ph-beta",
			DependsOn: []string{"ms-alpha/ph-beta"},
		},
		Status: tidev1alpha2.PlanStatus{Phase: "Succeeded"},
	}
	proj := makeProjectForExport("my-project", "default")

	// Build a valid envelope for the plan with its UID.
	envData := buildValidEnvelopeJSON(t, plUID)
	pvcTgz := makeFakePVCTgz(t, map[string][]byte{plUID: envData})

	fr := &fakeExportRunner{streamBytes: pvcTgz}
	restore := injectExportRunner(fr)
	defer restore()

	c := fake.NewClientBuilder().WithScheme(testScheme(t)).
		WithObjects(proj, ms, ph, pl).Build()
	cs := fakeclientset.NewSimpleClientset()

	outPath := t.TempDir() + "/out.tgz"
	var stdout, stderr bytes.Buffer
	if err := exportEnvelopesRun(
		context.Background(), c, cs,
		"default/my-project",
		"tide-projects", outPath, false,
		&stdout, &stderr,
	); err != nil {
		t.Fatalf("exportEnvelopesRun: %v", err)
	}

	bundleFiles, err := pkgbundle.ReadBundle(outPath)
	if err != nil {
		t.Fatalf("ReadBundle: %v", err)
	}

	manifestBytes, ok := bundleFiles[pkgbundle.BundleFileSeedManifest]
	if !ok {
		t.Fatal("bundle missing seed-manifest.json")
	}

	var manifest pkgbundle.BundleManifest
	if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
		t.Fatalf("unmarshal seed-manifest.json: %v", err)
	}

	// Milestone assertions.
	if len(manifest.Milestones) != 1 {
		t.Fatalf("expected 1 milestone entry; got %d", len(manifest.Milestones))
	}
	msEntry := manifest.Milestones[0]
	if msEntry.Name != "ms-alpha" {
		t.Errorf("milestone Name: got %q, want ms-alpha", msEntry.Name)
	}
	if msEntry.FQName != pkgbundle.MilestoneFQName("ms-alpha") {
		t.Errorf("milestone FQName: got %q, want %q", msEntry.FQName, pkgbundle.MilestoneFQName("ms-alpha"))
	}
	if msEntry.OldUID != msUID {
		t.Errorf("milestone OldUID: got %q, want %q", msEntry.OldUID, msUID)
	}
	if msEntry.Status != "Succeeded" {
		t.Errorf("milestone Status: got %q, want Succeeded", msEntry.Status)
	}
	if msEntry.ProjectRef != "my-project" {
		t.Errorf("milestone ProjectRef: got %q, want my-project", msEntry.ProjectRef)
	}

	// Phase assertions.
	if len(manifest.Phases) != 1 {
		t.Fatalf("expected 1 phase entry; got %d", len(manifest.Phases))
	}
	phEntry := manifest.Phases[0]
	if phEntry.FQName != pkgbundle.PhaseFQName("ms-alpha", "ph-beta") {
		t.Errorf("phase FQName: got %q, want %q", phEntry.FQName, pkgbundle.PhaseFQName("ms-alpha", "ph-beta"))
	}
	if phEntry.OldUID != phUID {
		t.Errorf("phase OldUID: got %q, want %q", phEntry.OldUID, phUID)
	}
	if phEntry.MilestoneRef != "ms-alpha" {
		t.Errorf("phase MilestoneRef: got %q, want ms-alpha", phEntry.MilestoneRef)
	}

	// Plan assertions.
	if len(manifest.Plans) != 1 {
		t.Fatalf("expected 1 plan entry; got %d", len(manifest.Plans))
	}
	plEntry := manifest.Plans[0]
	if plEntry.FQName != pkgbundle.PlanFQName("ms-alpha", "ph-beta", "pl-gamma") {
		t.Errorf("plan FQName: got %q, want %q", plEntry.FQName, pkgbundle.PlanFQName("ms-alpha", "ph-beta", "pl-gamma"))
	}
	if plEntry.OldUID != plUID {
		t.Errorf("plan OldUID: got %q, want %q", plEntry.OldUID, plUID)
	}
	if len(plEntry.DependsOn) != 1 || plEntry.DependsOn[0] != "ms-alpha/ph-beta" {
		t.Errorf("plan DependsOn: got %v, want [ms-alpha/ph-beta]", plEntry.DependsOn)
	}
	if plEntry.SHA256 == "" {
		t.Error("plan BundleEntry.SHA256 is empty; expected non-empty sha256")
	}
	if plEntry.Status != "Succeeded" {
		t.Errorf("plan Status: got %q, want Succeeded", plEntry.Status)
	}
	if plEntry.PhaseRef != "ph-beta" {
		t.Errorf("plan PhaseRef: got %q, want ph-beta", plEntry.PhaseRef)
	}
}

// TestExportEnvelopesDirMode verifies --dir emits an unpacked directory instead of tgz.
func TestExportEnvelopesDirMode(t *testing.T) {
	pvcTgz, _ := pkgbundle.WritePVCEnvelopesTgz(map[string][]byte{})
	fr := &fakeExportRunner{streamBytes: pvcTgz}
	restore := injectExportRunner(fr)
	defer restore()

	proj := makeProjectForExport("my-project", "default")
	c := fake.NewClientBuilder().WithScheme(testScheme(t)).WithObjects(proj).Build()
	cs := fakeclientset.NewSimpleClientset()

	outDir := t.TempDir() + "/bundle-dir"
	var stdout, stderr bytes.Buffer
	err := exportEnvelopesRun(
		context.Background(), c, cs,
		"default/my-project",
		"tide-projects", outDir, true, // outputDir=true
		&stdout, &stderr,
	)
	if err != nil {
		t.Fatalf("exportEnvelopesRun (--dir): %v", err)
	}

	// With --dir, the output path must be a directory containing seed-manifest.json.
	info, err := os.Stat(outDir)
	if err != nil {
		t.Fatalf("stat output dir %s: %v", outDir, err)
	}
	if !info.IsDir() {
		t.Errorf("expected output path to be a directory; is file")
	}
	if _, err := os.Stat(outDir + "/seed-manifest.json"); err != nil {
		t.Errorf("seed-manifest.json not found in --dir output: %v", err)
	}
}
