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

// output_test.go — Layer B kind integration test for output-path validation.
//
// Coverage: AC5 (output validation), HARN-05 (harness output path enforcement).
// The stub-subagent testMode=exceed-output-paths causes the stub to write files
// outside the declared output paths. Plan 09's handleJobCompletion flags this
// via outputs.Validate after reading the EnvelopeOut.
//
// The test asserts the Task reaches Failed phase with an appropriate condition.

import (
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"sigs.k8s.io/controller-runtime/pkg/client"

	tideprojectv1alpha1 "github.com/jsquirrelz/tide/api/v1alpha1"
)

var _ = Describe("Output path violation detection (AC5 / HARN-05)", Label("kind"), func() {
	const outputNS = "output-test"

	BeforeEach(func() {
		skipIfCRDsOnlyMode()
	})

	AfterEach(func() {
		deleteNamespace(outputNS)
		if CurrentSpecReport().Failed() {
			exportKindLogs()
		}
	})

	// AC5 / HARN-05: testMode=exceed-output-paths causes the stub to write
	// outside declared paths; the harness validator flags the violation.
	It("AC5: exceed-output-paths mode causes Task to fail with output violation", func() {
		By("Creating a Plan with a Task set to exceed output paths")
		Expect(createProjectHierarchy(ctx, outputNS)).To(Succeed())

		outputPlanYAML := fmt.Sprintf(`
apiVersion: tideproject.k8s/v1alpha1
kind: Plan
metadata:
  name: output-plan
  namespace: %s
spec:
  phaseRef: output-phase
---
apiVersion: tideproject.k8s/v1alpha1
kind: Task
metadata:
  name: exceed-output-task
  namespace: %s
  labels:
    # Phase 04.1 P1.4 (commit 416545c) removed the first-Project fallback in
    # resolveProject; the label is now required for Task→Project resolution
    # because PlanReconciler-stamped owner-refs race with the Task reconciler.
    # The project name follows the createProjectHierarchy convention: ns+"-project".
    tideproject.k8s/project: output-test-project
    tideproject.k8s/wave-index: "0"
spec:
  planRef: output-plan
  promptPath: "children/task-01.json"
  filesTouched: ["declared.go"]
  declaredOutputPaths: ["declared.go"]
  dev:
    testMode: exceed-output-paths
`, outputNS, outputNS)

		Expect(applyYAML(outputPlanYAML)).To(Succeed())

		// Wait for task to be created.
		Eventually(func() error {
			t := &tideprojectv1alpha1.Task{}
			return k8sClient.Get(ctx, client.ObjectKey{Name: "exceed-output-task", Namespace: outputNS}, t)
		}, 30*time.Second, time.Second).Should(Succeed())

		// The Task should reach Failed phase (output-path violation detected by harness).
		// Allow 2 minutes for Job to complete + controller to detect the violation.
		Eventually(func() string {
			t := &tideprojectv1alpha1.Task{}
			if err := k8sClient.Get(ctx, client.ObjectKey{Name: "exceed-output-task", Namespace: outputNS}, t); err != nil {
				return ""
			}
			return t.Status.Phase
		}, 2*time.Minute, 3*time.Second).Should(Equal("Failed"),
			"Task should reach Failed phase when output paths are violated")

		GinkgoWriter.Println("AC5: output path violation detected — task reached Failed as expected")
	})
})
