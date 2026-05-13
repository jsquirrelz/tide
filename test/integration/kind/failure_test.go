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

// failure_test.go — Layer B kind integration tests for failure injection and
// dependent task blocking.
//
// Coverage: AC3 (failed task siblings continue), FAIL-01 (failed task dependent
// never dispatches), FAIL-02 (sibling wave tasks are independent).

import (
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	tideprojectv1alpha1 "github.com/jsquirrelz/tide/api/v1alpha1"
)

var _ = Describe("Failure injection and dependent task blocking (AC3)", Label("kind"), func() {
	const failNS = "failure-test"

	BeforeEach(func() {
		skipIfCRDsOnlyMode()
	})

	AfterEach(func() {
		deleteNamespace(failNS)
		if CurrentSpecReport().Failed() {
			exportKindLogs()
		}
	})

	// AC3 / FAIL-01: β (middle task) has testMode=fail-exit-1; α (independent)
	// completes; γ (depends on β) never dispatches.
	It("AC3: failed task β does not block independent sibling α; dependent γ never dispatches", func() {
		By("Creating a plan with 3 tasks: α (independent), β (fail), γ (depends on β)")
		ns := failNS
		createNamespace(ns)

		planYAML := fmt.Sprintf(`
apiVersion: tideproject.k8s/v1alpha1
kind: Plan
metadata:
  name: fail-plan
  namespace: %s
spec:
  phaseRef: fail-phase
---
apiVersion: tideproject.k8s/v1alpha1
kind: Task
metadata:
  name: alpha-fail
  namespace: %s
  labels:
    tideproject.k8s/wave-index: "0"
spec:
  planRef: fail-plan
  filesTouched: ["alpha-fail.go"]
  declaredOutputPaths: ["alpha-fail.go"]
  dev:
    testMode: success
---
apiVersion: tideproject.k8s/v1alpha1
kind: Task
metadata:
  name: beta-fail
  namespace: %s
  labels:
    tideproject.k8s/wave-index: "0"
spec:
  planRef: fail-plan
  filesTouched: ["beta-fail.go"]
  declaredOutputPaths: ["beta-fail.go"]
  dev:
    testMode: fail-exit-1
---
apiVersion: tideproject.k8s/v1alpha1
kind: Task
metadata:
  name: gamma-fail
  namespace: %s
  labels:
    tideproject.k8s/wave-index: "1"
spec:
  planRef: fail-plan
  dependsOn: ["beta-fail"]
  filesTouched: ["gamma-fail.go"]
  declaredOutputPaths: ["gamma-fail.go"]
  dev:
    testMode: success
`, ns, ns, ns, ns)

		Expect(applyYAML(planYAML)).To(Succeed())

		// Wait for tasks to be created.
		for _, taskName := range []string{"alpha-fail", "beta-fail", "gamma-fail"} {
			name := taskName
			Eventually(func() error {
				t := &tideprojectv1alpha1.Task{}
				return k8sClient.Get(ctx, client.ObjectKey{Name: name, Namespace: ns}, t)
			}, 30*time.Second, time.Second).Should(Succeed(), "Task %s should be created", name)
		}

		// Wait for β to fail.
		Eventually(func() string {
			t := &tideprojectv1alpha1.Task{}
			if err := k8sClient.Get(ctx, client.ObjectKey{Name: "beta-fail", Namespace: ns}, t); err != nil {
				return ""
			}
			return t.Status.Phase
		}, 2*time.Minute, 3*time.Second).Should(Equal("Failed"),
			"beta-fail should reach Failed phase (testMode=fail-exit-1)")

		// α (independent sibling) should eventually complete.
		Eventually(func() string {
			t := &tideprojectv1alpha1.Task{}
			if err := k8sClient.Get(ctx, client.ObjectKey{Name: "alpha-fail", Namespace: ns}, t); err != nil {
				return ""
			}
			return t.Status.Phase
		}, 2*time.Minute, 3*time.Second).Should(Equal("Succeeded"),
			"alpha-fail (independent sibling) should succeed even though beta-fail failed")

		// γ (depends on β) should NEVER dispatch — verify it stays non-Running.
		Consistently(func() string {
			t := &tideprojectv1alpha1.Task{}
			if err := k8sClient.Get(ctx, client.ObjectKey{Name: "gamma-fail", Namespace: ns}, t); err != nil {
				return "notfound"
			}
			return t.Status.Phase
		}, 30*time.Second, 2*time.Second).ShouldNot(Equal("Running"),
			"gamma-fail (depends on failed beta) should never dispatch (phase should not be Running)")

		GinkgoWriter.Println("AC3: failure injection verified — sibling continues, dependent blocked")
	})
})

// createNamespace creates a namespace in the kind cluster.
func createNamespace(ns string) {
	nsYAML := fmt.Sprintf(`
apiVersion: v1
kind: Namespace
metadata:
  name: %s
`, ns)
	_ = applyYAML(nsYAML)
}

// makeKindTask creates a Task in the kind cluster.
func makeKindTask(ns, name, planRef string, dependsOn []string, testMode string) *tideprojectv1alpha1.Task {
	task := &tideprojectv1alpha1.Task{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: ns,
		},
		Spec: tideprojectv1alpha1.TaskSpec{
			PlanRef:             planRef,
			DependsOn:           dependsOn,
			FilesTouched:        []string{name + ".go"},
			DeclaredOutputPaths: []string{name + ".go"},
			Dev:                 tideprojectv1alpha1.TaskDev{TestMode: testMode},
		},
	}
	Expect(k8sClient.Create(ctx, task)).To(Succeed())
	return task
}
