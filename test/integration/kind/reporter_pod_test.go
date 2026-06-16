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

// reporter_pod_test.go — Layer B kind integration spec for Phase 09 Plan 06.
//
// Coverage: REQ-09-01 (Manager spawns reader Job on dispatch-Job completion).
//
// Property under test: after a planner Job for a Project (the project-level
// dispatch site) completes, the Manager spawns a tide-reporter Job named
// "tide-reporter-<project-UID>" in the project namespace — discriminated from
// dispatch Jobs by the tideproject.k8s/role=reporter label.
//
// What this spec verifies (the four acceptance criteria from 09-06-PLAN.md):
//   1. The Manager spawns a reporter Job in the project namespace after the
//      planner Job completes.
//   2. The reporter Job carries the label tideproject.k8s/role=reporter.
//   3. The reporter Job's ServiceAccountName is "tide-reporter".
//   4. A child Milestone CR eventually appears in the project namespace
//      (reporter ran and materialized it via the K8s API).
//
// Why this test uses bare-project.yaml:
//   - The bare Project path exercises ALL FOUR planner-completion handlers
//     (Project → Milestone → Phase → Plan) in a single spec. The Project-level
//     reporter spawn (tide-reporter-<project-UID>) is the one this test watches.
//   - The stub subagent is used (CI path). The reporter binary reads the stub's
//     out.json which carries a Milestone ChildCRD (bare-project stub fixture).
//
// T-09-13 property verified: the reporter Job is NOT named like the dispatch Job
// ("tide-project-<uid>-1"). The label discriminates it from the dispatch Job so
// the dispatch-completion handler does not re-fire on the reporter Job's own
// completion (idempotent-AlreadyExists guard is the primary defence; label is
// belt-and-suspenders documented in T-09-13).
//
// This spec reuses the bare-project namespace to keep the cluster footprint small.
// It runs AFTER bare_project_test.go in the same suite; if the namespace already
// exists from a prior run (KEEP_KIND_CLUSTER=true), the BeforeEach's createNamespace
// and applyFile calls are idempotent.

import (
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	batchv1 "k8s.io/api/batch/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	tideprojectv1alpha2 "github.com/jsquirrelz/tide/api/v1alpha2"
)

const reporterPodNS = "reporter-pod-test"

var _ = Describe("Manager spawns tide-reporter Job after planner completion (REQ-09-01)", Label("kind"), func() {

	BeforeEach(func() {
		skipIfCRDsOnlyMode()
		createNamespace(reporterPodNS)
		// Apply the same bare-project fixture used by bare_project_test.go with
		// the test's namespace substituted. We use a dynamically-generated YAML
		// here to avoid adding another testdata file; the fixture is minimal.
		projectYAML := fmt.Sprintf(`apiVersion: v1
kind: Secret
metadata:
  name: tide-provider-secret
  namespace: %s
type: Opaque
data:
  ANTHROPIC_API_KEY: dGVzdC1hcGkta2V5LXN0dWItc3ViYWdlbnQtZG9lcy1ub3QtdXNlLWl0
---
apiVersion: tideproject.k8s/v1alpha1
kind: Project
metadata:
  name: reporter-test-project
  namespace: %s
spec:
  targetRepo: "https://git.example.internal/stub/reporter-test.git"
  providerSecretRef: "tide-provider-secret"
  budget:
    absoluteCapCents: 0
  subagent:
    model: stub
  gates:
    milestone: auto
    phase: auto
    plan: auto
    task: auto
    pauseBetweenWaves: false
`, reporterPodNS, reporterPodNS)
		Expect(applyYAML(projectYAML)).To(Succeed())
	})

	AfterEach(func() {
		deleteNamespace(reporterPodNS)
		if CurrentSpecReport().Failed() {
			exportKindLogs()
		}
	})

	It("Manager spawns a tide-reporter Job in the project namespace after planner-Job completion", func() {
		// ------------------------------------------------------------------
		// Step 1: Obtain the Project's UID so we can compute the expected
		// reporter Job name: "tide-reporter-<project-UID>"
		// ------------------------------------------------------------------
		By("Wait for Project to exist and obtain its UID")
		var project tideprojectv1alpha2.Project
		Eventually(func() error {
			return k8sClient.Get(ctx, client.ObjectKey{
				Name:      "reporter-test-project",
				Namespace: reporterPodNS,
			}, &project)
		}, 60*time.Second, time.Second).Should(Succeed(),
			"Project reporter-test-project must exist in "+reporterPodNS)

		projectUID := string(project.UID)
		expectedJobName := fmt.Sprintf("tide-reporter-%s", projectUID)

		GinkgoWriter.Printf("reporter-pod-test: project UID=%s; expecting reporter Job %s\n",
			projectUID, expectedJobName)

		// ------------------------------------------------------------------
		// Step 2: Wait for the Manager to spawn the reporter Job.
		// The planner Job must complete first (stub runs fast). Then
		// handleProjectJobCompletion spawns "tide-reporter-<projectUID>".
		// Timeout: 3m — planner Job dispatch + stub execution + reconcile loop.
		// ------------------------------------------------------------------
		By("Wait for tide-reporter Job to be spawned in project namespace")
		var reporterJob batchv1.Job
		Eventually(func() error {
			return k8sClient.Get(ctx, client.ObjectKey{
				Name:      expectedJobName,
				Namespace: reporterPodNS,
			}, &reporterJob)
		}, 3*time.Minute, 2*time.Second).Should(Succeed(),
			fmt.Sprintf("tide-reporter Job %q must be spawned in namespace %s within 3 minutes (REQ-09-01)", expectedJobName, reporterPodNS))

		GinkgoWriter.Printf("reporter-pod-test: reporter Job %s spawned\n", expectedJobName)

		// ------------------------------------------------------------------
		// Step 3: Assert the reporter Job carries the correct role label
		// (T-09-13 discrimination gate).
		// ------------------------------------------------------------------
		By("Assert reporter Job carries tideproject.k8s/role=reporter label")
		Expect(reporterJob.Labels).To(HaveKeyWithValue(
			"tideproject.k8s/role", "reporter"),
			"reporter Job must carry tideproject.k8s/role=reporter (T-09-13 dispatch-completion discrimination)")

		// ------------------------------------------------------------------
		// Step 4: Assert the reporter Pod's ServiceAccountName is tide-reporter
		// (T-09-14 privilege escalation mitigation — reporter runs as least-
		// privilege SA, not the Manager's ClusterRole).
		// ------------------------------------------------------------------
		By("Assert reporter Job pod ServiceAccountName is tide-reporter")
		Expect(reporterJob.Spec.Template.Spec.ServiceAccountName).To(Equal("tide-reporter"),
			"reporter Job pod must run as tide-reporter SA (T-09-14 least-privilege mitigation)")

		// ------------------------------------------------------------------
		// Step 5: Wait for a child Milestone to appear in the project namespace.
		// The reporter reads out.json from the PVC and materializes it via the
		// K8s API. Timeout: 4m — reporter Job Pod start + execution.
		// This assertion proves the end-to-end: Manager-spawns-reader-Job →
		// child-CR-appears-via-watch.
		// ------------------------------------------------------------------
		By("Wait for child Milestone to materialize (reporter Job created it)")
		Eventually(func() error {
			var milestones tideprojectv1alpha2.MilestoneList
			if err := k8sClient.List(ctx, &milestones, client.InNamespace(reporterPodNS)); err != nil {
				return err
			}
			for _, ms := range milestones.Items {
				for _, ref := range ms.OwnerReferences {
					if string(ref.UID) == projectUID {
						GinkgoWriter.Printf("reporter-pod-test: Milestone %s materialized (owned by project %s)\n",
							ms.Name, projectUID)
						return nil
					}
				}
			}
			return fmt.Errorf("no Milestone owned by project UID=%s found yet (total in ns: %d)",
				projectUID, len(milestones.Items))
		}, 4*time.Minute, 2*time.Second).Should(Succeed(),
			"Milestone owned by project must materialize within 4 minutes (reporter Job created it via K8s API)")
	})
})
