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

// up_stack_dispatch_test.go — Layer B kind integration spec for plan 03-10.
//
// Coverage: ART-03 / D-A1 / D-A2 — up-stack reconciler dispatch shape.
//
// Property under test: applying a Milestone CRD triggers the MilestoneReconciler
// to (a) set the owner ref to the parent Project (via internal/owner.EnsureOwnerRef),
// and (b) dispatch a planner Job named `tide-milestone-<milestone-uid>-1` with
// the level + role labels (D-A2 + D-B5 deterministic naming).
//
// What this spec verifies:
//   - Milestone owner ref correctly points back to its parent Project.
//   - The MilestoneReconciler creates a planner Job with the deterministic
//     name pattern `tide-milestone-<uid>-1`.
//   - The Job's labels include tideproject.k8s/milestone-uid + level=milestone
//     + role=planner.
//   - The Job's owner ref points back to the Milestone (Controller=true).
//
// What this spec does NOT verify (out-of-scope, separate plan):
//   - Phase ChildCRD materialization from EnvelopeOut.ChildCRDs. That requires
//     a planner-mode stub-subagent emitting a canned EnvelopeOut.ChildCRDs
//     block + a PodStatusEnvelopeReader read path tested end-to-end. The stub
//     in v1 of Phase 3 supports executor-mode test envelopes only (D-D3 added
//     wait-for-signal); a planner-mode addition is its own plan.
//   - The structural-shape contract above is the minimum-viable proof that the
//     up-stack reconciler bodies (plan 03-08) wire into real K8s.

import (
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	batchv1 "k8s.io/api/batch/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	tideprojectv1alpha2 "github.com/jsquirrelz/tide/api/v1alpha2"
)

var _ = Describe("Up-stack dispatch — Milestone planner Job (ART-03 / D-A1 / D-A2)", Label("kind"), func() {
	const upStackNS = "up-stack-test"

	BeforeEach(func() {
		skipIfCRDsOnlyMode()
	})

	AfterEach(func() {
		deleteNamespace(upStackNS)
		if CurrentSpecReport().Failed() {
			exportKindLogs()
		}
	})

	// Test 5: applying a Milestone triggers MilestoneReconciler to (a) set the
	// owner ref to its parent Project, and (b) dispatch a planner Job named
	// tide-milestone-<milestone-uid>-1 owned by the Milestone.
	It("applying Milestone CRD triggers planner Job dispatch with deterministic name + ownerReferences cascade", func() {
		By("Apply up-stack fixture (Project + Milestone, ownerReferences omitted)")
		Expect(applyFile("testdata/up-stack-project.yaml")).To(Succeed())

		By("Wait for the Project to exist (controller readiness signal)")
		var project tideprojectv1alpha2.Project
		Eventually(func() error {
			return k8sClient.Get(ctx, client.ObjectKey{
				Name: "up-stack-project", Namespace: upStackNS,
			}, &project)
		}, 60*time.Second, time.Second).Should(Succeed(),
			"Project up-stack-project must exist after fixture apply")

		By("Wait for MilestoneReconciler to set ownerReferences on the Milestone (cascade contract)")
		var milestone tideprojectv1alpha2.Milestone
		Eventually(func(g Gomega) {
			g.Expect(k8sClient.Get(ctx, client.ObjectKey{
				Name: "m1", Namespace: upStackNS,
			}, &milestone)).To(Succeed())
			// MilestoneReconciler step 4 sets the owner ref via
			// internal/owner.EnsureOwnerRef once it observes the parent Project.
			g.Expect(milestone.OwnerReferences).NotTo(BeEmpty(),
				"Milestone must have an ownerReference set by the controller")
			found := false
			for _, ref := range milestone.OwnerReferences {
				if ref.Kind == "Project" && ref.Name == "up-stack-project" {
					g.Expect(ref.UID).To(Equal(project.UID),
						"Milestone ownerReference UID must match Project.UID (CRD-02)")
					if ref.Controller != nil {
						g.Expect(*ref.Controller).To(BeTrue(),
							"Milestone ownerReference must set Controller=true")
					}
					found = true
				}
			}
			g.Expect(found).To(BeTrue(),
				"Milestone ownerReferences must include the parent Project")
		}, 90*time.Second, 2*time.Second).Should(Succeed())

		By("Observe planner Job tide-milestone-<milestone-uid>-1 dispatched with level + role labels")
		// Re-fetch Milestone to get the UID after the first reconcile.
		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: "m1", Namespace: upStackNS}, &milestone)).To(Succeed())
		Expect(milestone.UID).NotTo(BeEmpty(), "Milestone must have a UID")

		expectedJobName := fmt.Sprintf("tide-milestone-%s-1", milestone.UID)
		var plannerJob batchv1.Job
		Eventually(func() error {
			return k8sClient.Get(ctx, client.ObjectKey{
				Name: expectedJobName, Namespace: upStackNS,
			}, &plannerJob)
		}, 2*time.Minute, 2*time.Second).Should(Succeed(),
			"planner Job %s must be dispatched (deterministic D-B5 name)", expectedJobName)

		// D-A2 label contract.
		Expect(plannerJob.Labels).To(HaveKeyWithValue("tideproject.k8s/level", "milestone"),
			"planner Job must carry level=milestone label (D-A2)")
		Expect(plannerJob.Labels).To(HaveKeyWithValue("tideproject.k8s/role", "planner"),
			"planner Job must carry role=planner label (D-A2)")
		Expect(plannerJob.Labels).To(HaveKeyWithValue("tideproject.k8s/milestone-uid", string(milestone.UID)),
			"planner Job must carry milestone-uid=<milestone.UID> label")

		// Owner ref cascade: Job → Milestone (Controller=true).
		Expect(plannerJob.OwnerReferences).NotTo(BeEmpty(),
			"planner Job must have an ownerReference set by the controller")
		assertJobOwnedByMilestone(&plannerJob, &milestone)

		GinkgoWriter.Printf("up-stack dispatch verified: planner Job %s owned by Milestone %s (%s)\n",
			expectedJobName, milestone.Name, milestone.UID)
	})
})

// assertJobOwnedByMilestone verifies the Job's ownerReferences contain a
// Milestone entry whose UID matches and Controller=true (CRD-02 / Pitfall 23).
func assertJobOwnedByMilestone(job *batchv1.Job, ms *tideprojectv1alpha2.Milestone) {
	var ref *metav1.OwnerReference
	for i := range job.OwnerReferences {
		r := &job.OwnerReferences[i]
		if r.Kind == "Milestone" && r.Name == ms.Name {
			ref = r
			break
		}
	}
	Expect(ref).NotTo(BeNil(),
		"planner Job ownerReferences must include the Milestone parent")
	Expect(ref.UID).To(Equal(ms.UID),
		"planner Job ownerReference UID must match Milestone.UID")
	Expect(ref.Controller).NotTo(BeNil(),
		"planner Job ownerReference Controller flag must be set")
	Expect(*ref.Controller).To(BeTrue(),
		"planner Job ownerReference must set Controller=true (CRD-02)")
}
