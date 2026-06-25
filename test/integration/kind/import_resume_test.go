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

// import_resume_test.go — Phase 29 Plan 05 kind E2E test proving zero-cost resumption.
//
// Two-tier assertion bar (D-11):
//
// Tier a — small purpose-built fixture (test/integration/kind/testdata/import-small-fixture/):
//  1. Import the small fixture via the REAL tide CLI (exec.CommandContext on bin/tide).
//  2. Apply the fixture's project.yaml into a fresh namespace.
//  3. Eventually-poll until all Milestones in the namespace reach Succeeded (stub
//     subagents drive the full cascade: milestone planner → phase → plan → task).
//  4. LIVE export+import round-trip (D-10): tide export-envelopes of the completed
//     namespace → assert D-02-shaped bundle → import-envelopes into a FRESH namespace
//     → tide apply → assert milestone/phase adoption (0 planner Jobs).
//
// Tier b — full salvage-20260618 fixture (examples/projects/dogfood/salvage-20260618/):
//  1. Import the salvage bundle via the REAL tide CLI.
//  2. Apply the salvage project.yaml.
//  3. Assert zero planner Jobs for role=planner,level=milestone (adopted → no planner).
//  4. Assert zero planner Jobs for role=planner,level=phase (adopted → no planner).
//  5. Assert Project.Status.Budget.CostSpentCents == 0 (D-14: imported envelopes
//     must not re-pay planning cost) — sampled BEFORE plan-level planners dispatch
//     (the wave controller honors adoption at milestone/phase; plan planners legitimately
//     re-run per D-17 and are not asserted here).
//
// Gating (D-12): Label("kind","long") + testing.Short() skip + skipIfCRDsOnlyMode().
// Binary resolution (D-10/T-29-05-03): TIDE_BINARY env → exec.LookPath("tide") → Skip.
//
// Make sure `make test-int-kind-prep` (which builds bin/tide and loads images)
// has been run before invoking `make test-int`.
package kind_integration

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	batchv1 "k8s.io/api/batch/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	"sigs.k8s.io/controller-runtime/pkg/client"

	tideprojectv1alpha2 "github.com/jsquirrelz/tide/api/v1alpha2"
)

// importResumeNS is the namespace for tier-a spec (small fixture + round-trip).
const importResumeNS = "import-resume-test"

// importResumeRoundtripNS is the FRESH namespace for the export→import round-trip (tier-a step 4).
const importResumeRoundtripNS = "import-resume-roundtrip"

// importResumeSalvageNS is the namespace for tier-b spec (salvage fixture).
const importResumeSalvageNS = "import-resume-salvage"

// resolveTideBinary returns the path to the tide binary.
// Resolution order: TIDE_BINARY env → exec.LookPath("tide").
// Returns "" if not found (caller must Skip).
func resolveTideBinary() string {
	if v := os.Getenv("TIDE_BINARY"); v != "" {
		return v
	}
	p, _ := exec.LookPath("tide")
	return p
}

var _ = Describe("Import resume E2E", Label("kind", "long"), func() {

	// -----------------------------------------------------------------------
	// Tier a: small fixture import → drain → LIVE export+import round-trip (D-10)
	// -----------------------------------------------------------------------
	Describe("Tier a: small fixture drain to all-Milestones-Succeeded + live round-trip", func() {

		BeforeEach(func() {
			skipIfCRDsOnlyMode()
			if testing.Short() {
				Skip("Skipping long import-resume (tier a) in short mode")
			}
			tideBin := resolveTideBinary()
			if tideBin == "" {
				Skip("tide binary not found; build with `make test-int-kind-prep` or set TIDE_BINARY")
			}

			createNamespace(importResumeNS)
			createNamespace(importResumeRoundtripNS)
			// GAP-8: the stub project planner re-runs in the import flow and its
			// credproxy sidecar mounts tide-provider-secret; create it (the fixture
			// project.yaml header documents the test as the secret's creator).
			ensureProviderSecret(importResumeNS)
			ensureProviderSecret(importResumeRoundtripNS)
		})

		AfterEach(func() {
			deleteNamespace(importResumeNS)
			deleteNamespace(importResumeRoundtripNS)
			if CurrentSpecReport().Failed() {
				exportKindLogs()
			}
		})

		It("imports small fixture, drains to Succeeded, then round-trips via live export+import (D-10/D-11a)", func() {
			tideBin := resolveTideBinary()

			// ----------------------------------------------------------------
			// Step 1: import the small fixture bundle into importResumeNS.
			// The fixture lives at testdata/import-small-fixture/ — it is a
			// pre-built directory (not a .tgz), so we pass the directory path.
			// ----------------------------------------------------------------
			smallFixtureDir := filepath.Join("testdata", "import-small-fixture")
			By("Importing small fixture via tide import-envelopes (real CLI)")
			importCmd := exec.CommandContext(ctx, tideBin,
				"--kubeconfig", kubeconfigPath,
				"import-envelopes", smallFixtureDir,
				"--namespace", importResumeNS,
			)
			importOut, err := importCmd.CombinedOutput()
			Expect(err).NotTo(HaveOccurred(),
				"tide import-envelopes of small fixture: %s", importOut)
			GinkgoWriter.Printf("import-envelopes (small fixture): %s\n", importOut)

			// ----------------------------------------------------------------
			// Step 2: apply the fixture's project.yaml with the namespace patch.
			// The project.yaml in the fixture omits namespace (commented out)
			// because the test owns the namespace assignment. Use kubectl apply
			// with --field-manager to stamp the target namespace.
			// ----------------------------------------------------------------
			By("Applying small-fixture project.yaml via kubectl apply")
			projectYAMLPath := filepath.Join(smallFixtureDir, "project.yaml")
			applyCmd := exec.CommandContext(ctx, "kubectl", "--kubeconfig", kubeconfigPath,
				"apply", "-f", projectYAMLPath,
				"-n", importResumeNS,
				"--timeout=30s",
			)
			applyOut, err := applyCmd.CombinedOutput()
			Expect(err).NotTo(HaveOccurred(),
				"kubectl apply -f project.yaml -n %s: %s", importResumeNS, applyOut)
			GinkgoWriter.Printf("apply project.yaml: %s\n", applyOut)

			// ----------------------------------------------------------------
			// Step 3: wait for all Milestones in importResumeNS to reach
			// Succeeded. Stub subagents drive the cascade. Generous 8m timeout
			// matches the existing Layer B long-spec budgets.
			// ----------------------------------------------------------------
			By("Waiting for all Milestones to reach Succeeded (stub subagents drive cascade)")
			Eventually(func() (bool, error) {
				var msList tideprojectv1alpha2.MilestoneList
				if err := k8sClient.List(ctx, &msList, client.InNamespace(importResumeNS)); err != nil {
					return false, err
				}
				if len(msList.Items) == 0 {
					return false, fmt.Errorf("no Milestones found in %s yet", importResumeNS)
				}
				for _, ms := range msList.Items {
					if ms.Status.Phase != "Succeeded" {
						return false, fmt.Errorf("Milestone %s is %q, not Succeeded", ms.Name, ms.Status.Phase)
					}
				}
				return true, nil
			}, 8*time.Minute, 5*time.Second).Should(BeTrue(),
				"all Milestones in %s must reach Succeeded", importResumeNS)

			GinkgoWriter.Printf("All Milestones in %s reached Succeeded\n", importResumeNS)

			// ----------------------------------------------------------------
			// Step 4 — LIVE export+import round-trip (D-10, criterion #4):
			//
			// (a) export-envelopes: tide export-envelopes <ns>/<project>
			//     --output <tmpdir>/exported.tgz
			// (b) assert exported bundle has D-02-shaped content
			// (c) import-envelopes into a FRESH namespace + kubectl apply
			// (d) assert milestone/phase adoption in the fresh namespace
			// ----------------------------------------------------------------
			By("Running LIVE export+import round-trip (D-10)")

			tmpDir := GinkgoT().TempDir()
			exportedBundle := filepath.Join(tmpDir, "exported.tgz")

			By("(4a) tide export-envelopes of the completed namespace")
			exportCmd := exec.CommandContext(ctx, tideBin,
				"--kubeconfig", kubeconfigPath,
				"export-envelopes", importResumeNS+"/import-small-test",
				"--output", exportedBundle,
			)
			exportOut, err := exportCmd.CombinedOutput()
			Expect(err).NotTo(HaveOccurred(),
				"tide export-envelopes: %s", exportOut)
			GinkgoWriter.Printf("export-envelopes: %s\n", exportOut)

			// (4b) assert the exported bundle is a D-02-shaped tgz.
			By("(4b) asserting exported bundle has D-02-shaped content")
			assertD02BundleShape(exportedBundle)

			// (4c) import the freshly-exported bundle into a FRESH namespace.
			By("(4c) tide import-envelopes of freshly-exported bundle into fresh namespace")
			importRTCmd := exec.CommandContext(ctx, tideBin,
				"--kubeconfig", kubeconfigPath,
				"import-envelopes", exportedBundle,
				"--namespace", importResumeRoundtripNS,
			)
			importRTOut, err := importRTCmd.CombinedOutput()
			Expect(err).NotTo(HaveOccurred(),
				"tide import-envelopes of exported bundle: %s", importRTOut)
			GinkgoWriter.Printf("import-envelopes (round-trip): %s\n", importRTOut)

			// The import-envelopes writes project.yaml to CWD. Register cleanup
			// BEFORE the apply so a failed apply doesn't leak the file into the repo.
			defer func() { _ = os.Remove("project.yaml") }()
			By("(4c) Applying the project.yaml written by import-envelopes to the round-trip namespace")
			applyRTCmd := exec.CommandContext(ctx, "kubectl", "--kubeconfig", kubeconfigPath,
				"apply", "-f", "project.yaml",
				"-n", importResumeRoundtripNS,
				"--timeout=30s",
			)
			applyRTOut, err := applyRTCmd.CombinedOutput()
			Expect(err).NotTo(HaveOccurred(),
				"kubectl apply round-trip project.yaml: %s", applyRTOut)
			GinkgoWriter.Printf("apply round-trip project.yaml: %s\n", applyRTOut)

			// (4d) assert adoption in the round-trip namespace. Gate on the
			// deterministic ImportComplete condition — once it fires, adoption is
			// durable and the milestone/phase levels will NEVER dispatch a planner,
			// so a Consistently window confirms a permanent property rather than
			// racing the controller's reconcile.
			By("(4d) Waiting for ImportComplete in round-trip namespace")
			waitForImportComplete(importResumeRoundtripNS)
			By("(4d) Asserting milestone/phase adoption in round-trip namespace (0 planner Jobs)")
			assertNoPlannerJobsForLevelConsistently(importResumeRoundtripNS, "milestone",
				15*time.Second, 2*time.Second, "adopted milestone planners must not dispatch Jobs")
			assertNoPlannerJobsForLevelConsistently(importResumeRoundtripNS, "phase",
				15*time.Second, 2*time.Second, "adopted phase planners must not dispatch Jobs")
		})
	})

	// -----------------------------------------------------------------------
	// Tier b: salvage-20260618 fixture adoption — zero {milestone,phase} planner
	// Jobs + $0 re-paid planning cost (D-11b, D-17)
	// -----------------------------------------------------------------------
	Describe("Tier b: salvage-20260618 adoption — 0 planner Jobs {milestone,phase} + $0 re-paid", func() {

		BeforeEach(func() {
			skipIfCRDsOnlyMode()
			if testing.Short() {
				Skip("Skipping long import-resume (tier b) in short mode")
			}
			tideBin := resolveTideBinary()
			if tideBin == "" {
				Skip("tide binary not found; build with `make test-int-kind-prep` or set TIDE_BINARY")
			}

			createNamespace(importResumeSalvageNS)
			// GAP-8: provider secret for the credproxy sidecar (see Tier a).
			ensureProviderSecret(importResumeSalvageNS)
		})

		AfterEach(func() {
			deleteNamespace(importResumeSalvageNS)
			if CurrentSpecReport().Failed() {
				exportKindLogs()
			}
		})

		It("imports salvage fixture, asserts 0 {milestone,phase} planner Jobs + $0 re-paid cost (D-11b/D-14/D-17)", func() {
			tideBin := resolveTideBinary()

			// ----------------------------------------------------------------
			// Step 1: import the salvage-20260618 bundle into importResumeSalvageNS.
			// The salvage fixture is a directory (no .tgz at top level — use the
			// pvc-envelopes.tgz inside; but the CLI accepts the directory root).
			// Per D-17: 18 envelopes are adopted (project/milestone/phase);
			// 42 plan planners legitimately re-run (failed envelopes, no adoption).
			// ----------------------------------------------------------------
			salvageFixtureDir := filepath.Join("..", "..", "..", "examples", "projects", "dogfood", "salvage-20260618")
			By("Importing salvage-20260618 fixture via tide import-envelopes (real CLI)")
			importCmd := exec.CommandContext(ctx, tideBin,
				"--kubeconfig", kubeconfigPath,
				"import-envelopes", salvageFixtureDir,
				"--namespace", importResumeSalvageNS,
			)
			importOut, err := importCmd.CombinedOutput()
			Expect(err).NotTo(HaveOccurred(),
				"tide import-envelopes of salvage fixture: %s", importOut)
			GinkgoWriter.Printf("import-envelopes (salvage): %s\n", importOut)

			// ----------------------------------------------------------------
			// Step 2: apply the salvage project.yaml into the test namespace.
			// Apply the canonical singular project.yaml (generated by
			// scripts/gen-salvage-seed). It is namespace-less, so -n targets
			// importResumeSalvageNS cleanly (the List-style projects.yaml carries
			// the original dogfood namespace and cannot be -n-overridden). The
			// Milestone/Phase/Plan CRs are NOT pre-applied here — the Phase 28
			// ImportController materializes the whole tree from the seed ConfigMap;
			// the Eventually below waits for that materialization.
			// ----------------------------------------------------------------
			By("Applying salvage project.yaml via kubectl apply")
			salvageProjectYAML := filepath.Join(salvageFixtureDir, "project.yaml")
			applyCmd := exec.CommandContext(ctx, "kubectl", "--kubeconfig", kubeconfigPath,
				"apply", "-f", salvageProjectYAML,
				"-n", importResumeSalvageNS,
				"--timeout=30s",
			)
			applyOut, err := applyCmd.CombinedOutput()
			Expect(err).NotTo(HaveOccurred(),
				"kubectl apply -f project.yaml -n %s: %s", importResumeSalvageNS, applyOut)
			GinkgoWriter.Printf("apply salvage project.yaml: %s\n", applyOut)

			// ----------------------------------------------------------------
			// Step 3 + Step 4 — assert 0 planner Jobs for {milestone,phase}.
			//
			// Window choice: we assert IMMEDIATELY after import+apply and wait
			// with Eventually for the controller to observe the seed ConfigMap
			// and begin reconciling. The adoption gate fires within the first
			// reconcile — the controller reads the seed, marks ImportComplete,
			// and the milestone/phase controllers see adoption and skip planner
			// dispatch. We poll for the Project to show ImportComplete (or
			// at least for Milestones to appear) to confirm the import was
			// processed, then assert 0 planner Jobs for those levels.
			//
			// D-17: we do NOT assert plan-level planners — those re-run.
			// ----------------------------------------------------------------
			By("Waiting for Project ImportComplete condition (import processed by controller)")
			// Gate on the deterministic adoption signal — not "Milestones exist".
			// A materialized Milestone does not yet mean adoption has fired; gating
			// on ImportComplete ensures the no-re-plan + $0 assertions below sample
			// AFTER adoption settles, not during a transient pre-adoption window.
			waitForImportComplete(importResumeSalvageNS)

			// ----------------------------------------------------------------
			// Assert $0 re-paid planning cost (D-14) — BEFORE plan-level planners
			// can dispatch. We read CostSpentCents immediately after milestone
			// adoption settles. The project controller skips budget rollup for
			// imported levels (project_controller.go:1253 ImportSource != nil
			// check), so CostSpentCents must remain 0 for the adopted levels.
			//
			// The window is: after milestone/phase import adoption fires (above
			// Eventually ensures at least 3 milestones are present) but before
			// any plan-level planner Job completes and reports usage. This is
			// deterministic: adoption fires synchronously in the first reconcile
			// of the ImportController state machine; plan planners are dispatched
			// only after the import Job runs (Job creation → pod scheduling →
			// pod running → planner completes), which takes at minimum tens of
			// seconds even on a fast cluster.
			// ----------------------------------------------------------------
			By("Asserting $0 re-paid planning cost (D-14) before plan-level planner dispatch")
			// Find the salvage project name from the applied YAML.
			// The salvage project is named "dogfood-codex-runtime".
			salvageProjectName := "dogfood-codex-runtime"
			var project tideprojectv1alpha2.Project
			Expect(k8sClient.Get(ctx, client.ObjectKey{
				Namespace: importResumeSalvageNS,
				Name:      salvageProjectName,
			}, &project)).To(Succeed(),
				"salvage Project %s must be readable in namespace %s", salvageProjectName, importResumeSalvageNS)
			Expect(project.Status.Budget.CostSpentCents).To(Equal(int64(0)),
				"imported envelopes must not re-pay planning cost (D-14): CostSpentCents should be 0 "+
					"before plan-level planner Jobs report usage")

			GinkgoWriter.Printf("CostSpentCents immediately after import: %d (expected 0)\n",
				project.Status.Budget.CostSpentCents)

			// ----------------------------------------------------------------
			// Step 3: assert 0 planner Jobs for role=planner,level=milestone.
			// Milestone envelopes (3) were adopted — no planner Job should be
			// dispatched. Use Consistently with a brief window to confirm the
			// controller is not racing to dispatch one.
			// ----------------------------------------------------------------
			By("Asserting 0 planner Jobs for role=planner,level=milestone (adopted levels)")
			assertNoPlannerJobsForLevelConsistently(importResumeSalvageNS, "milestone",
				15*time.Second, 2*time.Second,
				"adopted milestone planners must not dispatch Jobs in salvage namespace")

			// ----------------------------------------------------------------
			// Step 4: assert 0 planner Jobs for role=planner,level=phase.
			// Phase envelopes (14 of 15) were adopted — no planner Job for any
			// phase level. D-17 explicitly scopes to {milestone,phase} only.
			// ----------------------------------------------------------------
			By("Asserting 0 planner Jobs for role=planner,level=phase (adopted levels)")
			assertNoPlannerJobsForLevelConsistently(importResumeSalvageNS, "phase",
				15*time.Second, 2*time.Second,
				"adopted phase planners must not dispatch Jobs in salvage namespace")

			GinkgoWriter.Printf("Tier b assertions passed: 0 milestone/phase planner Jobs, "+
				"CostSpentCents=0 for salvage import in namespace %s\n", importResumeSalvageNS)
		})
	})
})

// ---- helpers ---------------------------------------------------------------

// assertD02BundleShape unpacks the given tgz bundle and asserts the D-02
// bundle shape: projects.yaml / milestones.yaml / phases.yaml / plans.yaml +
// seed-manifest.json + SEED-OUTLINE.md + pvc-envelopes.tgz.
//
// The assertion is non-exhaustive; it checks presence of required entries so
// the test proves the round-trip produces a well-formed bundle without
// re-asserting every byte of content.
func assertD02BundleShape(tgzPath string) {
	GinkgoHelper()
	f, err := os.Open(tgzPath)
	Expect(err).NotTo(HaveOccurred(), "open exported bundle %s", tgzPath)
	defer func() { _ = f.Close() }()

	gr, err := gzip.NewReader(f)
	Expect(err).NotTo(HaveOccurred(), "gzip.NewReader on exported bundle")
	defer func() { _ = gr.Close() }()

	tr := tar.NewReader(gr)

	required := map[string]bool{
		// GAP-15: the canonical D-02 bundle (pkg/bundle bundleEntryOrder, =
		// BundleFileProject) carries the SINGULAR project.yaml — the one Project to
		// re-apply. The plural projects.yaml the old assertion demanded is never
		// emitted by export-envelopes nor read by import-envelopes (it applies
		// project.yaml). Milestones/phases/plans stay plural (they are lists).
		"project.yaml":       false,
		"milestones.yaml":    false,
		"phases.yaml":        false,
		"plans.yaml":         false,
		"seed-manifest.json": false,
		"SEED-OUTLINE.md":    false,
		"pvc-envelopes.tgz":  false,
	}

	for {
		hdr, terr := tr.Next()
		if terr == io.EOF {
			break
		}
		Expect(terr).NotTo(HaveOccurred(), "read tgz entry from exported bundle")
		// Normalize: strip any leading "./" prefix.
		name := filepath.Clean(hdr.Name)
		// Only check top-level entries.
		if filepath.Dir(name) == "." {
			if _, ok := required[name]; ok {
				required[name] = true
			}
		}
	}

	for entry, found := range required {
		Expect(found).To(BeTrue(), "exported bundle must contain D-02 entry %q", entry)
	}
	GinkgoWriter.Printf("D-02 bundle shape verified: all required entries present in %s\n", tgzPath)
}

// waitForImportComplete blocks until the (single) Project in the namespace
// reports ImportComplete=True — the deterministic adoption signal. Once it
// fires, adoption is durable: the wave controller permanently suppresses
// planner dispatch for the imported milestone/phase levels. Gating the
// no-re-plan assertions on this (rather than on "Milestones exist" or a fixed
// time window) removes the race where a planner could dispatch before adoption
// settles or after a too-short observation window closes.
func waitForImportComplete(ns string) {
	GinkgoHelper()
	Eventually(func(g Gomega) {
		var pl tideprojectv1alpha2.ProjectList
		g.Expect(k8sClient.List(ctx, &pl, client.InNamespace(ns))).To(Succeed())
		g.Expect(pl.Items).NotTo(BeEmpty(), "no Project yet in %s", ns)
		g.Expect(meta.IsStatusConditionTrue(
			pl.Items[0].Status.Conditions, tideprojectv1alpha2.ConditionImportComplete,
		)).To(BeTrue(), "Project %s/%s not yet ImportComplete", ns, pl.Items[0].Name)
	}, 3*time.Minute, 5*time.Second).Should(Succeed())
}

// assertNoPlannerJobsForLevelConsistently asserts (via Consistently) that zero
// planner Jobs exist in the namespace for the given level over the given window.
// Used for tier-b where we confirm adoption is stable (not just transiently 0).
func assertNoPlannerJobsForLevelConsistently(ns, level string, duration, interval time.Duration, msg string) {
	GinkgoHelper()
	Consistently(func(g Gomega) {
		jobs := &batchv1.JobList{}
		g.Expect(k8sClient.List(ctx, jobs,
			client.InNamespace(ns),
			client.MatchingLabels{
				"tideproject.k8s/role":  "planner",
				"tideproject.k8s/level": level,
			},
		)).To(Succeed())
		g.Expect(jobs.Items).To(BeEmpty(), msg)
	}, duration, interval).Should(Succeed())
}
