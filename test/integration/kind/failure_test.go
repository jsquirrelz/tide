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

	"sigs.k8s.io/controller-runtime/pkg/client"

	tideprojectv1alpha2 "github.com/jsquirrelz/tide/api/v1alpha2"
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
		Expect(createProjectHierarchy(ctx, ns)).To(Succeed())

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
    # Phase 04.1 P1.4 (commit 416545c) removed the first-Project fallback.
    # Project name follows createProjectHierarchy convention: ns+"-project".
    tideproject.k8s/project: failure-test-project
    tideproject.k8s/wave-index: "0"
spec:
  planRef: fail-plan
  promptPath: "children/task-01.json"
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
    tideproject.k8s/project: failure-test-project
    tideproject.k8s/wave-index: "0"
spec:
  planRef: fail-plan
  promptPath: "children/task-02.json"
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
    tideproject.k8s/project: failure-test-project
    tideproject.k8s/wave-index: "1"
spec:
  planRef: fail-plan
  promptPath: "children/task-03.json"
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
				t := &tideprojectv1alpha2.Task{}
				return k8sClient.Get(ctx, client.ObjectKey{Name: name, Namespace: ns}, t)
			}, 30*time.Second, time.Second).Should(Succeed(), "Task %s should be created", name)
		}

		// Wait for β to fail.
		Eventually(func() string {
			t := &tideprojectv1alpha2.Task{}
			if err := k8sClient.Get(ctx, client.ObjectKey{Name: "beta-fail", Namespace: ns}, t); err != nil {
				return ""
			}
			return t.Status.Phase
		}, 240*time.Second, 3*time.Second).Should(Equal("Failed"),
			"beta-fail should reach Failed phase (testMode=fail-exit-1)")

		// α (independent sibling) should eventually complete.
		Eventually(func() string {
			t := &tideprojectv1alpha2.Task{}
			if err := k8sClient.Get(ctx, client.ObjectKey{Name: "alpha-fail", Namespace: ns}, t); err != nil {
				return ""
			}
			return t.Status.Phase
		}, 2*time.Minute, 3*time.Second).Should(Equal("Succeeded"),
			"alpha-fail (independent sibling) should succeed even though beta-fail failed")

		// γ (depends on β) should NEVER dispatch — verify it stays non-Running.
		Consistently(func() string {
			t := &tideprojectv1alpha2.Task{}
			if err := k8sClient.Get(ctx, client.ObjectKey{Name: "gamma-fail", Namespace: ns}, t); err != nil {
				return "notfound"
			}
			return t.Status.Phase
		}, 30*time.Second, 2*time.Second).ShouldNot(Equal("Running"),
			"gamma-fail (depends on failed beta) should never dispatch (phase should not be Running)")

		GinkgoWriter.Println("AC3: failure injection verified — sibling continues, dependent blocked")
	})
})

// createNamespace creates a namespace in the kind cluster and ensures the Task
// Job dependencies exist inside it.
//
// Every Task Job's PodSpec references tide-subagent, tide-projects, and
// tide-signing-key by name in its own namespace; without them Pod creation,
// scheduling, or credproxy startup fails. The chart templates these resources
// only in tide-system, so per-test namespaces get them via this helper at
// namespace-create time. See ensureSubagentSA in suite_test.go for the D-A4
// rationale.
//
// Phase 02.1 D-02 (02.1-BASELINE.md).
func createNamespace(ns string) {
	nsYAML := fmt.Sprintf(`
apiVersion: v1
kind: Namespace
metadata:
  name: %s
`, ns)
	_ = applyYAML(nsYAML)
	ensureSubagentSA(ns)
	ensureProjectsPVC(ns)
	ensureSigningKeySecret(ns)
	// Phase 09 plan 09-06: tide-reporter SA + Role + RoleBinding for the reader Job.
	ensureReporterSARBAC(ns)
	// Cascade-11: pre-bind WaitForFirstConsumer PVC for Pod-less fixtures (push-lease).
	pvcPrewarmPod(ns)
}

// ensureReporterSARBAC creates the tide-reporter ServiceAccount + Role +
// RoleBinding in the given namespace. The tide-reporter reader Job runs in the
// project namespace; without this SA the Job Pod cannot start.
//
// Mirrors the chart's reporter-rbac.yaml Role exactly:
//   - create+get+list on the five child CRD Kinds (list backs the idempotency
//     check in reporter.ChildrenAlreadyMaterialized)
//   - get on projects (resolveParent fetches the parent Project by name to
//     obtain its live UID for ownerRef — Project-level reporter only)
//
// (T-09-07 least-privilege mitigation). The chart's reporter-rbac.yaml only
// installs these into .Release.Namespace and .Values.projectNamespaces; per-test
// namespaces need the manual equivalent.
//
// Phase-09 origin defect (commit e451b90): the original helper omitted `list`
// on child kinds and omitted the `projects/get` rule, causing the reporter Job
// to exit 2 (RBAC denial on resolveParent) and no Milestone to be created —
// the root cause of the reporter_pod_test.go:196 materialization failure.
func ensureReporterSARBAC(ns string) {
	rbacYAML := fmt.Sprintf(`apiVersion: v1
kind: ServiceAccount
metadata:
  name: tide-reporter
  namespace: %s
automountServiceAccountToken: true
---
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: tide-reporter
  namespace: %s
rules:
  - apiGroups: ["tideproject.k8s"]
    resources: ["milestones", "phases", "plans", "tasks", "waves"]
    verbs: ["create", "get", "list"]
  - apiGroups: ["tideproject.k8s"]
    resources: ["projects"]
    verbs: ["get"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: tide-reporter
  namespace: %s
subjects:
  - kind: ServiceAccount
    name: tide-reporter
    namespace: %s
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: Role
  name: tide-reporter
`, ns, ns, ns, ns)
	_ = applyYAML(rbacYAML)
}
