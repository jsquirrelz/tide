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
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	tideprojectv1alpha3 "github.com/jsquirrelz/tide/api/v1alpha3"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("Three-task wave success (AC1)", Label("kind"), func() {
	const (
		testNS    = "wave-success-test"
		fixtureNS = "tide-int-test"
		proj      = "wave-test-project"
	)

	// taskPhase reads a Task's status phase ("" if not found yet).
	taskPhase := func(name string) string {
		t := &tideprojectv1alpha3.Task{}
		if err := k8sClient.Get(ctx, client.ObjectKey{Name: name, Namespace: fixtureNS}, t); err != nil {
			return ""
		}
		return t.Status.Phase
	}

	// waveTask builds a wave Task via the shared typed fixture builder.
	waveTask := func(name, prompt, waveIdx, mode string, capSec int32, deps ...string) *tideprojectv1alpha3.Task {
		opts := []taskOpt{
			withTaskProjectLabel(proj),
			withWaveIndex(waveIdx),
			withPromptPath(prompt),
			withTestMode(mode),
			withWallClockCap(capSec),
		}
		if len(deps) > 0 {
			opts = append(opts, withTaskDependsOn(deps...))
		}
		return newStubTask(fixtureNS, name, "wave-test-plan", opts...)
	}

	BeforeEach(func() {
		skipIfCRDsOnlyMode()
		// Build the common parents (ns + provider secret + Project→Milestone→
		// Phase→Plan) via the shared typed fixtures. Each It supplies its own
		// Tasks because the two specs need different wave-0 task modes.
		createNamespace(fixtureNS)
		ensureProviderSecret(fixtureNS)
		parents := []client.Object{
			newStubProject(fixtureNS, proj,
				withTargetRepo("https://github.com/example/three-task-wave.git"),
				withProviderSecret("tide-provider-secret"),
				withBudget(100000)),
			newStubMilestone(fixtureNS, "wave-test-milestone", proj),
			newStubPhase(fixtureNS, "wave-test-phase", "wave-test-milestone"),
			newStubPlan(fixtureNS, "wave-test-plan", "wave-test-phase", withPlanProjectLabel(proj)),
		}
		for _, o := range parents {
			Expect(createFixture(ctx, o)).To(Succeed())
		}
	})

	AfterEach(func() {
		deleteNamespaceAndWait(testNS)
		deleteNamespaceAndWait(fixtureNS)
		if CurrentSpecReport().Failed() {
			exportKindLogs()
		}
	})

	// AC1: α,β (wave 0) succeed; γ (wave 1, dependsOn both) then succeeds. This
	// also proves the wave gate OPENS — γ runs once its dependencies complete.
	It("AC1: three-task wave — all tasks succeed via stub-subagent", func() {
		for _, t := range []*tideprojectv1alpha3.Task{
			waveTask("alpha", "children/task-01.json", "0", "success", 120),
			waveTask("beta", "children/task-02.json", "0", "success", 120),
			waveTask("gamma", "children/task-03.json", "1", "success", 120, "alpha", "beta"),
		} {
			Expect(createFixture(ctx, t)).To(Succeed())
		}

		for _, name := range []string{"alpha", "beta", "gamma"} {
			n := name // capture for closure
			Eventually(func() string { return taskPhase(n) }, 3*time.Minute, 5*time.Second).
				Should(Equal("Succeeded"), "Task %s should eventually reach Succeeded phase", n)
		}
		GinkgoWriter.Println("AC1: all three tasks reached Succeeded")
	})

	// AC1: the wave gate HOLDS. α,β are wait-for-signal so they dispatch and BLOCK
	// (never Succeed without a release), keeping γ's dependsOn permanently
	// unsatisfied. γ must therefore stay un-dispatched the entire time wave 0 is
	// open — a deterministic gate, with no reliance on incidental wave-0
	// completion timing (the prior version's Consistently-over-a-race-window).
	It("AC1: wave 1 (gamma) does not dispatch until wave 0 tasks complete", func() {
		// capSec 300 keeps the blocked wave-0 tasks alive well past the assertion
		// window so the wall-clock cap can't fail them mid-test (mirrors chaos_resume).
		for _, t := range []*tideprojectv1alpha3.Task{
			waveTask("alpha", "children/task-01.json", "0", "wait-for-signal", 300),
			waveTask("beta", "children/task-02.json", "0", "wait-for-signal", 300),
			waveTask("gamma", "children/task-03.json", "1", "success", 300, "alpha", "beta"),
		} {
			Expect(createFixture(ctx, t)).To(Succeed())
		}

		// α and β must dispatch and reach Running (blocked on their release signal).
		for _, name := range []string{"alpha", "beta"} {
			n := name
			Eventually(func() string { return taskPhase(n) }, 90*time.Second, 2*time.Second).
				Should(Equal("Running"), "wave-0 task %s should dispatch and block (wait-for-signal)", n)
		}

		// With α,β blocked (never Succeeded), γ's dependencies can never be
		// satisfied, so γ must never dispatch — for as long as we care to check.
		Consistently(func() string { return taskPhase("gamma") }, 20*time.Second, 2*time.Second).
			ShouldNot(Equal("Running"), "gamma must not dispatch until wave 0 completes")
	})
})

// skipIfCRDsOnlyMode skips the test if the controller Deployment is not present.
func skipIfCRDsOnlyMode() {
	// Check if the controller is ready by listing Projects.
	// If the Project CRD is not installed (CRDs-only mode), we skip.
	pl := &tideprojectv1alpha3.ProjectList{}
	if err := k8sClient.List(ctx, pl); err != nil {
		// Suite-ctx expiry is NOT CRDs-only mode: once kindTestTimeout
		// elapses every List fails, and converting that into per-spec
		// Skips turned exhausted-budget runs into silent greens (PR #3
		// run 6: 12 specs — including one that had just spent 10 minutes
		// failing — recorded Skipped, suite SUCCESS). A blown budget must
		// be a red build, never a quiet one.
		if ctx.Err() != nil {
			Fail(fmt.Sprintf(
				"suite context expired (kindTestTimeout %s) — failing instead of skipping so an exhausted budget cannot masquerade as green: %v",
				kindTestTimeout, err))
		}
		Skip("CRDs not installed or controller not ready; skipping kind test")
	}
}
