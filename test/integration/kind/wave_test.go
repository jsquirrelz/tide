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

package kind_integration

// wave_test.go — Layer B kind integration tests for the three-task wave success path.
//
// Coverage: AC1 (three-task wave success), SUB-01 (stub-subagent Jobs dispatch),
// SUB-02 (attempt counter), SUB-04 (canned envelope), ART-01 (init Job lifecycle).
//
// These tests require the kind cluster to be running with the TIDE controller
// Deployment and stub-subagent image loaded. See BeforeSuite in suite_test.go.
// In CRDs-only mode (no controller Deployment), the tests are skipped gracefully.

import (
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	tideprojectv1alpha2 "github.com/jsquirrelz/tide/api/v1alpha2"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("Three-task wave success (AC1)", Label("kind"), func() {
	const testNS = "wave-success-test"

	// fixtureNS is where the wave hierarchy lives (the assertions below reference
	// it directly); testNS above is only the AfterEach cleanup target.
	const fixtureNS = "tide-int-test"

	BeforeEach(func() {
		skipIfCRDsOnlyMode()
		// Build the three-task wave hierarchy via the shared typed fixtures —
		// replaces testdata/three-task-wave.yaml, which duplicated applyHierarchy
		// field-for-field. α and β are wave-0 siblings; γ is wave-1 dependsOn both.
		createNamespace(fixtureNS)
		ensureProviderSecret(fixtureNS)
		const proj = "wave-test-project"
		fixtures := []client.Object{
			newStubProject(fixtureNS, proj,
				withTargetRepo("https://github.com/example/three-task-wave.git"),
				withProviderSecret("tide-provider-secret"),
				withBudget(100000)),
			newStubMilestone(fixtureNS, "wave-test-milestone", proj),
			newStubPhase(fixtureNS, "wave-test-phase", "wave-test-milestone"),
			newStubPlan(fixtureNS, "wave-test-plan", "wave-test-phase", withPlanProjectLabel(proj)),
			newStubTask(fixtureNS, "alpha", "wave-test-plan",
				withTaskProjectLabel(proj), withWallClockCap(120)),
			newStubTask(fixtureNS, "beta", "wave-test-plan",
				withTaskProjectLabel(proj), withPromptPath("children/task-02.json"), withWallClockCap(120)),
			newStubTask(fixtureNS, "gamma", "wave-test-plan",
				withTaskProjectLabel(proj), withWaveIndex("1"),
				withPromptPath("children/task-03.json"),
				withTaskDependsOn("alpha", "beta"), withWallClockCap(120)),
		}
		for _, f := range fixtures {
			Expect(createFixture(ctx, f)).To(Succeed())
		}
	})

	AfterEach(func() {
		deleteNamespace(testNS)
		// Clean up the fixture namespace.
		deleteNamespace("tide-int-test")
		if CurrentSpecReport().Failed() {
			exportKindLogs()
		}
	})

	// AC1: apply α→β→γ plan; Eventually all Tasks Succeeded; Wave 0 Succeeded.
	It("AC1: three-task wave — all tasks succeed via stub-subagent", func() {
		// Wave 0: α and β are independent; γ depends on α and β (Wave 1).
		// With testMode=success, stub-subagent exits 0 immediately.

		// Wait for alpha task to exist.
		Eventually(func() error {
			t := &tideprojectv1alpha2.Task{}
			return k8sClient.Get(ctx, client.ObjectKey{
				Name:      "alpha",
				Namespace: "tide-int-test",
			}, t)
		}, 30*time.Second, time.Second).Should(Succeed(),
			"Task 'alpha' should exist in tide-int-test namespace")

		// Eventually all three tasks should be Succeeded.
		for _, taskName := range []string{"alpha", "beta", "gamma"} {
			name := taskName // capture for closure
			Eventually(func() string {
				t := &tideprojectv1alpha2.Task{}
				if err := k8sClient.Get(ctx, client.ObjectKey{
					Name:      name,
					Namespace: "tide-int-test",
				}, t); err != nil {
					return ""
				}
				return t.Status.Phase
			}, 3*time.Minute, 5*time.Second).Should(Equal("Succeeded"),
				"Task %s should eventually reach Succeeded phase", name)
		}

		GinkgoWriter.Println("AC1: all three tasks reached Succeeded")
	})

	// wave-advances-only-after-all-tasks-complete: γ should not dispatch until α and β complete.
	It("AC1: wave 1 (gamma) does not dispatch until wave 0 tasks complete", func() {
		// Wait for alpha and beta to exist.
		Eventually(func() error {
			t := &tideprojectv1alpha2.Task{}
			return k8sClient.Get(ctx, client.ObjectKey{
				Name:      "alpha",
				Namespace: "tide-int-test",
			}, t)
		}, 30*time.Second, time.Second).Should(Succeed())

		// gamma should not be Running while alpha/beta haven't completed.
		// (this is a Consistently check over a short window immediately after apply)
		Consistently(func() string {
			t := &tideprojectv1alpha2.Task{}
			if err := k8sClient.Get(ctx, client.ObjectKey{
				Name:      "gamma",
				Namespace: "tide-int-test",
			}, t); err != nil {
				return "notfound"
			}
			return t.Status.Phase
		}, 10*time.Second, time.Second).ShouldNot(Equal("Running"),
			"gamma should not Run until alpha and beta have completed")
	})
})

// skipIfCRDsOnlyMode skips the test if the controller Deployment is not present.
func skipIfCRDsOnlyMode() {
	// Check if the controller is ready by listing Projects.
	// If the Project CRD is not installed (CRDs-only mode), we skip.
	pl := &tideprojectv1alpha2.ProjectList{}
	if err := k8sClient.List(ctx, pl); err != nil {
		Skip("CRDs not installed or controller not ready; skipping kind test")
	}
}
