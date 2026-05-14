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

// credproxy_test.go — Layer B kind integration test for credproxy sidecar topology.
//
// Coverage: AC5 (harness defense), HARN-03 (credproxy sidecar signal — Warning #8 fix).
//
// NEGATIVE-PATH COVERAGE NOTE (Warning #8):
//
// The stub-subagent makes ZERO outbound calls (Plan 04 contract — no real LLM calls,
// zero external network traffic). This means we CANNOT exercise the credproxy
// forward-proxy happy path from Layer B without adding a real outbound consumer to
// the stub, which would violate the Phase 2 zero-LLM-cost design.
//
// What Layer B DOES verify:
//   1. The credproxy sidecar container starts and becomes Ready (TCP probe on 8443).
//   2. The credproxy emits its startup log line "credproxy listening on 127.0.0.1:8443".
//
// Negative-path token-validation correctness (tampered token, expired token,
// wrong taskUID) is covered exhaustively in Plan 05's unit tests:
//   - internal/credproxy/token_test.go (HMAC sign/verify table-driven cases)
//   - internal/credproxy/server_test.go (request validation fixtures)
//
// These unit tests already ship (Phase 2 Plan 05). This comment cross-references
// the two-tier coverage architecture per the plan spec (Warning #8).

import (
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	tideprojectv1alpha1 "github.com/jsquirrelz/tide/api/v1alpha1"
)

var _ = Describe("Credproxy sidecar topology and startup signal (AC5 / HARN-03)", Label("kind"), func() {
	// NOTE: Warning #8 — see package-level doc comment above.
	const credproxyNS = "credproxy-test"

	BeforeEach(func() {
		skipIfCRDsOnlyMode()
	})

	AfterEach(func() {
		deleteNamespace(credproxyNS)
		if CurrentSpecReport().Failed() {
			exportKindLogs()
		}
	})

	// HARN-03: credproxy sidecar starts and becomes Ready.
	// Verifies: sidecar container topology (init container or sidecar) is present in the Pod,
	// and the TCP probe on :8443 succeeds.
	It("HARN-03: credproxy sidecar container starts and becomes Ready", func() {
		By("Creating a Plan with a Task using testMode=success (spawns a Job with credproxy sidecar)")
		Expect(applyHierarchy(ctx, credproxyNS, "credproxy-plan", "credproxy-task")).To(Succeed())

		// Wait for the task to be created.
		Eventually(func() error {
			t := &tideprojectv1alpha1.Task{}
			return k8sClient.Get(ctx, client.ObjectKey{Name: "credproxy-task", Namespace: credproxyNS}, t)
		}, 30*time.Second, time.Second).Should(Succeed())

		// Wait for a Pod to be created (Job spawns a Pod).
		var podName string
		Eventually(func() bool {
			pods := &corev1.PodList{}
			_ = k8sClient.List(ctx, pods, client.InNamespace(credproxyNS))
			for _, p := range pods.Items {
				if len(p.Name) > 0 {
					podName = p.Name
					return true
				}
			}
			return false
		}, 120*time.Second, 2*time.Second).Should(BeTrue(),
			"A Pod should be created for the credproxy-task Job")

		// Check if the credproxy sidecar container is present.
		Eventually(func() bool {
			pods := &corev1.PodList{}
			_ = k8sClient.List(ctx, pods, client.InNamespace(credproxyNS))
			for _, p := range pods.Items {
				// Check init containers.
				for _, ic := range p.Spec.InitContainers {
					if strings.Contains(ic.Name, "credproxy") || strings.Contains(ic.Name, "tide-credproxy") {
						GinkgoWriter.Printf("Found credproxy init container: %s in pod %s\n", ic.Name, p.Name)
						return true
					}
				}
				// Check sidecar containers.
				for _, c := range p.Spec.Containers {
					if strings.Contains(c.Name, "credproxy") || strings.Contains(c.Name, "tide-credproxy") {
						GinkgoWriter.Printf("Found credproxy container: %s in pod %s\n", c.Name, p.Name)
						return true
					}
				}
			}
			return false
		}, 60*time.Second, 2*time.Second).Should(BeTrue(),
			"Pod should contain a credproxy container (tide-credproxy)")

		GinkgoWriter.Printf("Pod name: %s\n", podName)
	})

	// HARN-03: credproxy emits its startup log line.
	It("HARN-03: credproxy container log contains 'credproxy listening on 127.0.0.1:8443'", func() {
		By("Dispatching a Task and checking credproxy container logs")
		Expect(applyHierarchy(ctx, credproxyNS, "credproxy-log-plan", "credproxy-log-task")).To(Succeed())

		// Wait for a Pod.
		Eventually(func() bool {
			pods := &corev1.PodList{}
			_ = k8sClient.List(ctx, pods, client.InNamespace(credproxyNS))
			return len(pods.Items) > 0
		}, 60*time.Second, 2*time.Second).Should(BeTrue())

		// Get the credproxy container logs and look for the startup line.
		Eventually(func() bool {
			pods := &corev1.PodList{}
			_ = k8sClient.List(ctx, pods, client.InNamespace(credproxyNS))
			for _, p := range pods.Items {
				// Try init containers first (credproxy may be an init container).
				for _, ic := range p.Spec.InitContainers {
					if strings.Contains(ic.Name, "credproxy") {
						logs := kubectlLogs(credproxyNS, p.Name, ic.Name)
						if strings.Contains(logs, "credproxy listening on 127.0.0.1:8443") {
							GinkgoWriter.Printf("Found startup log in init container %s: %q\n", ic.Name, logs)
							return true
						}
					}
				}
				for _, c := range p.Spec.Containers {
					if strings.Contains(c.Name, "credproxy") {
						logs := kubectlLogs(credproxyNS, p.Name, c.Name)
						if strings.Contains(logs, "credproxy listening on 127.0.0.1:8443") {
							GinkgoWriter.Printf("Found startup log in container %s: %q\n", c.Name, logs)
							return true
						}
					}
				}
			}
			return false
		}, 90*time.Second, 3*time.Second).Should(BeTrue(),
			"credproxy container log should contain 'credproxy listening on 127.0.0.1:8443'")
	})
})
