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

// bare_project_test.go — Layer B kind integration spec for Phase 07 Plan 02.
//
// Coverage: REQ-1, REQ-2, REQ-4, REQ-5, REQ-7a, REQ-7b.
//
// Property under test: applying a bare Project CRD (no pre-applied Milestone)
// self-bootstraps the full five-level cascade — Project dispatches a planner
// Job, a Milestone materializes (owner=Project), the Milestone dispatches a
// planner Job, a Phase materializes, the Phase dispatches a planner Job, a
// Plan materializes, Plan.Status.ValidationState is stamped "Validated",
// the Plan dispatches a planner Job, a Task materializes, a Wave materializes,
// the Task executor Job runs (stub → success), Task reaches Succeeded, and
// finally Project reaches Complete.
//
// IMPORTANT: This spec is intentionally RED until Plans 07-03 through 07-05
// implement production code. The verify step is `go build ./test/integration/kind/...`
// (compiles), not `go test` (passes). It will turn GREEN after 07-05 completes.
//
// Research notes (07-RESEARCH.md §"Critical Gap #2"):
//
//   - Project.Status.Phase reaches "Complete" as soon as Milestone.Status.Phase=="Succeeded".
//   - Milestone.Status.Phase reaches "Succeeded" immediately after ITS OWN planner Job
//     completes (milestone_controller.go:431 calls patchMilestoneSucceeded unconditionally
//     on Job done) — BEFORE Phase/Plan/Task are necessarily Succeeded.
//   - Therefore assertions 2 (Milestone.Succeeded) and 9 (Project.Complete) may resolve
//     BEFORE assertions 6-8 (Task materializes, Wave materializes, Task.Succeeded).
//   - Each assertion is independent; ordering reflects cascade materialization order, not
//     strict temporal dependency on the previous assertion completing before the next fires.

import (
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	tideprojectv1alpha3 "github.com/jsquirrelz/tide/api/v1alpha3"
)

const bareProjectNS = "bare-project-test"

var _ = Describe("bare Project self-bootstraps full cascade to Project=Complete (REQ-1..5 + REQ-7a/b)", Label("kind"), func() {

	BeforeEach(func() {
		skipIfCRDsOnlyMode()
		// createNamespace creates the namespace FIRST, then provisions the
		// per-namespace SA + tide-projects PVC + signing-key Secret and prewarms
		// the WaitForFirstConsumer PVC — in the correct order (the same all-in-one
		// helper push_lease_test.go uses). The previous code called the individual
		// helpers BEFORE the namespace existed (the fixture's Namespace doc applies
		// too late, at applyFile below), so each helper silently no-op'd against a
		// nonexistent namespace and the PVC never bound — the BeforeEach timed out
		// before the spec body ran.
		createNamespace(bareProjectNS)
		Expect(applyFile("testdata/bare-project.yaml")).To(Succeed())
	})

	AfterEach(func() {
		deleteNamespace(bareProjectNS)
		if CurrentSpecReport().Failed() {
			exportKindLogs()
		}
	})

	It("bare Project self-bootstraps full cascade to Project=Complete (REQ-1..5 + REQ-7a/b)", Label("kind"), func() {
		// filterByOwner returns the subset of objects from the list whose
		// ownerReferences contain an entry with the given ownerName.
		filterByOwner := func(items []metav1.ObjectMeta, ownerName string) []metav1.ObjectMeta {
			var matched []metav1.ObjectMeta
			for _, obj := range items {
				for _, ref := range obj.OwnerReferences {
					if ref.Name == ownerName {
						matched = append(matched, obj)
						break
					}
				}
			}
			return matched
		}

		// ------------------------------------------------------------------
		// Assertion 1 — Milestone materializes in bareProjectNS (timeout 3m)
		// REQ-1: Project dispatches a planner Job; REQ-2: Milestone materializes.
		// ------------------------------------------------------------------
		By("Wait for Milestone to materialize (owner=bare-project)")
		var milestoneName string
		Eventually(func() error {
			var list tideprojectv1alpha3.MilestoneList
			if err := k8sClient.List(ctx, &list, client.InNamespace(bareProjectNS)); err != nil {
				return err
			}
			// Collect the ObjectMeta slice so filterByOwner can inspect ownerReferences.
			var metas []metav1.ObjectMeta
			for _, ms := range list.Items {
				metas = append(metas, ms.ObjectMeta)
			}
			owned := filterByOwner(metas, "bare-project")
			if len(owned) == 0 {
				return fmt.Errorf("no Milestone owned by bare-project found yet (total in ns: %d)", len(list.Items))
			}
			milestoneName = owned[0].Name
			return nil
		}, 3*time.Minute, 2*time.Second).Should(Succeed(),
			"Milestone owned by Project bare-project must materialize within 3 minutes (REQ-2)")

		GinkgoWriter.Printf("bare-project-test: Milestone materialized: %s\n", milestoneName)

		// ------------------------------------------------------------------
		// Assertion 2 — Milestone.Status.Phase reaches Succeeded (timeout 4m)
		// Research note: this fires BEFORE Phase/Plan/Task exist — that is expected.
		// ------------------------------------------------------------------
		By("Wait for Milestone to reach Succeeded")
		Eventually(func() error {
			var ms tideprojectv1alpha3.Milestone
			if err := k8sClient.Get(ctx, client.ObjectKey{
				Name:      milestoneName,
				Namespace: bareProjectNS,
			}, &ms); err != nil {
				return err
			}
			if ms.Status.Phase != "Succeeded" {
				return fmt.Errorf("Milestone %s: want Status.Phase=Succeeded, got %q", milestoneName, ms.Status.Phase)
			}
			return nil
		}, 4*time.Minute, 2*time.Second).Should(Succeed(),
			"Milestone must reach Status.Phase=Succeeded within 4 minutes (milestone planner Job done)")

		GinkgoWriter.Printf("bare-project-test: Milestone %s reached Succeeded\n", milestoneName)

		// ------------------------------------------------------------------
		// Assertion 3 — Phase materializes owned by the Milestone (timeout 5m)
		// REQ-5: Phase in the full Milestone→Phase→Plan→Task tree materializes.
		// ------------------------------------------------------------------
		By("Wait for Phase to materialize (owner=milestone)")
		var phaseName string
		Eventually(func() error {
			var list tideprojectv1alpha3.PhaseList
			if err := k8sClient.List(ctx, &list, client.InNamespace(bareProjectNS)); err != nil {
				return err
			}
			var metas []metav1.ObjectMeta
			for _, ph := range list.Items {
				metas = append(metas, ph.ObjectMeta)
			}
			owned := filterByOwner(metas, milestoneName)
			if len(owned) == 0 {
				return fmt.Errorf("no Phase owned by Milestone %s found yet (total in ns: %d)", milestoneName, len(list.Items))
			}
			phaseName = owned[0].Name
			return nil
		}, 5*time.Minute, 2*time.Second).Should(Succeed(),
			"Phase owned by Milestone must materialize within 5 minutes (REQ-5)")

		GinkgoWriter.Printf("bare-project-test: Phase materialized: %s\n", phaseName)

		// ------------------------------------------------------------------
		// Assertion 4 — Plan materializes owned by the Phase (timeout 6m)
		// REQ-5: Plan in the full tree materializes.
		// ------------------------------------------------------------------
		By("Wait for Plan to materialize (owner=phase)")
		var planName string
		Eventually(func() error {
			var list tideprojectv1alpha3.PlanList
			if err := k8sClient.List(ctx, &list, client.InNamespace(bareProjectNS)); err != nil {
				return err
			}
			var metas []metav1.ObjectMeta
			for _, pl := range list.Items {
				metas = append(metas, pl.ObjectMeta)
			}
			owned := filterByOwner(metas, phaseName)
			if len(owned) == 0 {
				return fmt.Errorf("no Plan owned by Phase %s found yet (total in ns: %d)", phaseName, len(list.Items))
			}
			planName = owned[0].Name
			return nil
		}, 6*time.Minute, 2*time.Second).Should(Succeed(),
			"Plan owned by Phase must materialize within 6 minutes (REQ-5)")

		GinkgoWriter.Printf("bare-project-test: Plan materialized: %s\n", planName)

		// ------------------------------------------------------------------
		// Assertion 5 — Plan.Status.ValidationState == "Validated" (timeout 7m)
		// REQ-7a: ValidationState must be stamped after planner Job completion.
		// ------------------------------------------------------------------
		By("Wait for Plan.Status.ValidationState to reach Validated (REQ-7a)")
		Eventually(func() error {
			var pl tideprojectv1alpha3.Plan
			if err := k8sClient.Get(ctx, client.ObjectKey{
				Name:      planName,
				Namespace: bareProjectNS,
			}, &pl); err != nil {
				return err
			}
			if pl.Status.ValidationState != "Validated" {
				return fmt.Errorf("Plan %s: want Status.ValidationState=Validated, got %q", planName, pl.Status.ValidationState)
			}
			return nil
		}, 7*time.Minute, 2*time.Second).Should(Succeed(),
			"Plan.Status.ValidationState must reach Validated within 7 minutes (REQ-7a)")

		GinkgoWriter.Printf("bare-project-test: Plan %s ValidationState=Validated\n", planName)

		// ------------------------------------------------------------------
		// Assertion 6 — Task materializes owned by the Plan (timeout 7m)
		// REQ-5, REQ-7b: Task in the full tree materializes.
		// ------------------------------------------------------------------
		By("Wait for Task to materialize (owner=plan)")
		var taskName string
		Eventually(func() error {
			var list tideprojectv1alpha3.TaskList
			if err := k8sClient.List(ctx, &list, client.InNamespace(bareProjectNS)); err != nil {
				return err
			}
			var metas []metav1.ObjectMeta
			for _, t := range list.Items {
				metas = append(metas, t.ObjectMeta)
			}
			owned := filterByOwner(metas, planName)
			if len(owned) == 0 {
				return fmt.Errorf("no Task owned by Plan %s found yet (total in ns: %d)", planName, len(list.Items))
			}
			taskName = owned[0].Name
			return nil
		}, 7*time.Minute, 2*time.Second).Should(Succeed(),
			"Task owned by Plan must materialize within 7 minutes (REQ-5, REQ-7b)")

		GinkgoWriter.Printf("bare-project-test: Task materialized: %s\n", taskName)

		// ------------------------------------------------------------------
		// Assertion 7 — Wave materializes in bareProjectNS (timeout 8m)
		// REQ-7a: reconcileWaveMaterialization ran (gated on ValidationState=Validated).
		// ------------------------------------------------------------------
		By("Wait for Wave to materialize in bare-project-test namespace (REQ-7a: ValidationState unblocked wave derivation)")
		Eventually(func() error {
			var list tideprojectv1alpha3.WaveList
			if err := k8sClient.List(ctx, &list, client.InNamespace(bareProjectNS)); err != nil {
				return err
			}
			if len(list.Items) == 0 {
				return fmt.Errorf("no Wave in namespace %s yet", bareProjectNS)
			}
			return nil
		}, 8*time.Minute, 2*time.Second).Should(Succeed(),
			"Wave must materialize in bare-project-test within 8 minutes (REQ-7a: ValidationState unblocked reconcileWaveMaterialization)")

		GinkgoWriter.Printf("bare-project-test: Wave materialized in namespace %s\n", bareProjectNS)

		// ------------------------------------------------------------------
		// Assertion 8 — Task.Status.Phase reaches Succeeded (timeout 9m)
		// REQ-7b: executor Job ran to completion (stub success mode).
		// ------------------------------------------------------------------
		By("Wait for Task to reach Succeeded (REQ-7b: executor Job ran)")
		Eventually(func() error {
			var t tideprojectv1alpha3.Task
			if err := k8sClient.Get(ctx, client.ObjectKey{
				Name:      taskName,
				Namespace: bareProjectNS,
			}, &t); err != nil {
				return err
			}
			if t.Status.Phase != "Succeeded" {
				return fmt.Errorf("Task %s: want Status.Phase=Succeeded, got %q", taskName, t.Status.Phase)
			}
			return nil
		}, 9*time.Minute, 2*time.Second).Should(Succeed(),
			"Task must reach Status.Phase=Succeeded within 9 minutes (REQ-7b: executor Job completed)")

		GinkgoWriter.Printf("bare-project-test: Task %s reached Succeeded\n", taskName)

		// ------------------------------------------------------------------
		// Assertion 9 — Project.Status.Phase reaches Complete (timeout 10m)
		// REQ-4: Project transitions Running→Complete when all Milestones Succeeded.
		// Research note: may have fired earlier (after assertion 2) due to
		// Milestone=Succeeded triggering BoundaryDetected immediately. Asserting
		// here regardless ensures the terminal state is reached.
		// ------------------------------------------------------------------
		By("Wait for Project bare-project to reach Complete (REQ-4)")
		Eventually(func() error {
			var proj tideprojectv1alpha3.Project
			if err := k8sClient.Get(ctx, client.ObjectKey{
				Name:      "bare-project",
				Namespace: bareProjectNS,
			}, &proj); err != nil {
				return err
			}
			if proj.Status.Phase != "Complete" {
				return fmt.Errorf("Project bare-project: want Status.Phase=Complete, got %q", proj.Status.Phase)
			}
			return nil
		}, 10*time.Minute, 2*time.Second).Should(Succeed(),
			"Project bare-project must reach Status.Phase=Complete within 10 minutes (REQ-4: all owned Milestones Succeeded)")

		GinkgoWriter.Printf("bare-project-test: Project bare-project reached Complete\n")
	})
})
