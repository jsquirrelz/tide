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

// caps_test.go — Layer B kind integration test for wall-clock cap enforcement.
//
// Coverage: AC5 (caps), HARN-02 (wall-clock cap kills subagent via SIGTERM).
// The stub-subagent testMode=hang causes the Job to sleep indefinitely.
// The Task spec sets caps.wallClockSeconds=10 (Job.activeDeadlineSeconds triggers
// at 70s with the 60s overage buffer). The test asserts the Pod exits with reason
// cap-hit OR the Job reaches Failed status.

import (
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	batchv1 "k8s.io/api/batch/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	tideprojectv1alpha2 "github.com/jsquirrelz/tide/api/v1alpha2"
)

var _ = Describe("Wall-clock cap enforcement (AC5 / HARN-02)", Label("kind"), func() {
	const capsNS = "caps-test"

	BeforeEach(func() {
		skipIfCRDsOnlyMode()
	})

	AfterEach(func() {
		deleteNamespace(capsNS)
		if CurrentSpecReport().Failed() {
			exportKindLogs()
		}
	})

	// AC5 / HARN-02: testMode=hang + caps.WallClockSeconds=10
	// The Job's activeDeadlineSeconds kills the Pod within ~70s.
	// Test budget: 80s (within the ~180s Layer B target).
	It("AC5: hang mode + wall-clock cap terminates subagent Job within cap window", func() {
		By("Creating a Plan with a hanging Task (caps.WallClockSeconds=10)")
		Expect(createProjectHierarchy(ctx, capsNS)).To(Succeed())

		capPlanYAML := fmt.Sprintf(`
apiVersion: tideproject.k8s/v1alpha2
kind: Plan
metadata:
  name: cap-plan
  namespace: %s
spec:
  phaseRef: cap-phase
---
apiVersion: tideproject.k8s/v1alpha2
kind: Task
metadata:
  name: hang-task
  namespace: %s
  labels:
    # Phase 04.1 P1.4 (commit 416545c) removed the first-Project fallback.
    # Project name follows createProjectHierarchy convention: ns+"-project".
    tideproject.k8s/project: caps-test-project
    tideproject.k8s/wave-index: "0"
spec:
  planRef: cap-plan
  promptPath: "children/task-01.json"
  filesTouched: ["hang.go"]
  declaredOutputPaths: ["hang.go"]
  caps:
    wallClockSeconds: 10
  dev:
    testMode: hang
`, capsNS, capsNS)

		Expect(applyYAML(capPlanYAML)).To(Succeed())

		// Wait for the task to be created.
		Eventually(func() error {
			t := &tideprojectv1alpha2.Task{}
			return k8sClient.Get(ctx, client.ObjectKey{Name: "hang-task", Namespace: capsNS}, t)
		}, 30*time.Second, time.Second).Should(Succeed())

		// Wait for a Job to be created (dispatched).
		Eventually(func() int {
			jobs := &batchv1.JobList{}
			_ = k8sClient.List(ctx, jobs, client.InNamespace(capsNS))
			return len(jobs.Items)
		}, 60*time.Second, 2*time.Second).Should(BeNumerically(">=", 1),
			"A Job should be created for the hang-task")

		// Eventually the Job should fail due to activeDeadlineSeconds.
		// activeDeadlineSeconds = wallClockSeconds + 60 = 70s.
		// Allow 80s total (conservative for CI).
		Eventually(func() bool {
			jobs := &batchv1.JobList{}
			_ = k8sClient.List(ctx, jobs, client.InNamespace(capsNS))
			for _, j := range jobs.Items {
				for _, cond := range j.Status.Conditions {
					if cond.Type == batchv1.JobFailed && cond.Status == "True" {
						return true
					}
				}
				// Also check for DeadlineExceeded
				if j.Status.Conditions != nil {
					for _, c := range j.Status.Conditions {
						if c.Reason == "DeadlineExceeded" {
							return true
						}
					}
				}
			}
			return false
		}, 90*time.Second, 3*time.Second).Should(BeTrue(),
			"Job should fail due to activeDeadlineSeconds (wall-clock cap)")

		// The Task should also reach Failed phase.
		Eventually(func() string {
			t := &tideprojectv1alpha2.Task{}
			if err := k8sClient.Get(ctx, client.ObjectKey{Name: "hang-task", Namespace: capsNS}, t); err != nil {
				return ""
			}
			return t.Status.Phase
		}, 30*time.Second, 2*time.Second).Should(Equal("Failed"),
			"Task should reach Failed phase after wall-clock cap")

		GinkgoWriter.Println("AC5: wall-clock cap enforcement verified — hang-task Job failed as expected")
	})
})
