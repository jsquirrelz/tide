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

// RED scaffold — assertions wired in plans 08-05 (git-http server) and 08-07 (CI nightly wiring).
//
// This file contains the Ginkgo spec skeleton for the hermetic medium-http kind
// integration test. All It blocks Skip immediately — they will be made GREEN in
// plans 08-05 (git-http server image + manifests) and 08-07 (CI nightly wiring).
//
// Coverage target (post-GREEN):
//   - SC-5: CI coverage for the medium/http transport path (hermetic stub-subagent
//     + real git-http server image, exercising go-git HTTP transport without LLM cost).
package kind_integration

import (
	"fmt"
	"os/exec"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	tideprojectv1alpha1 "github.com/jsquirrelz/tide/api/v1alpha1"
)

const (
	// mediumHTTPNamespace is the test namespace for the medium-http kind spec.
	// Isolated from kindNamespace (tide-int-test) so it doesn't interfere with
	// other Layer B specs.
	mediumHTTPNamespace = "medium-http-test"

	// mediumHTTPServiceName is the in-cluster ClusterIP Service name for the
	// git-http server Deployment (served at port 80).
	mediumHTTPServiceName = "git-http-server"

	// mediumHTTPTargetRepo is the in-cluster HTTP URL for the demo remote.
	// Accessible from Jobs in mediumHTTPNamespace via short DNS name.
	mediumHTTPTargetRepo = "http://git-http-server.medium-http-test.svc.cluster.local/demo-remote.git"
)

var _ = Describe("Medium http transport", Label("kind"), Ordered, func() {

	BeforeAll(func() {
		// Create the test namespace with per-namespace resources (SA, PVC, signing-key).
		// createNamespace provisions tide-subagent SA + tide-projects PVC + signing-key
		// Secret and pre-warms the WaitForFirstConsumer PVC (same helper used by
		// bare_project_test.go and failure_test.go).
		createNamespace(mediumHTTPNamespace)
	})

	AfterAll(func() {
		deleteNamespace(mediumHTTPNamespace)
		if CurrentSpecReport().Failed() {
			exportKindLogs()
		}
	})

	// -------------------------------------------------------------------------
	// Spec 1: demo-remote-init Job bootstraps the bare repo on demo-remote-pvc.
	// The init Job (cmd/tide-demo-init) pushes over file:// internally to the PVC.
	// After completion, the PVC contains demo-remote.git and can be served by
	// the git-http server Deployment.
	// -------------------------------------------------------------------------
	It("initializes the git-http server via demo-remote-init Job", func() {
		Skip("git-http-server image not yet built — pending 08-05")

		// Apply the demo-remote-init Job into the test namespace.
		By("Applying demo-remote-init Job into " + mediumHTTPNamespace)
		cmd := exec.CommandContext(ctx, "kubectl", "--kubeconfig", kubeconfigPath,
			"apply", "-f", "../../examples/projects/medium/demo-remote-init-job.yaml",
			"-n", mediumHTTPNamespace, "--timeout=30s")
		out, err := cmd.CombinedOutput()
		Expect(err).NotTo(HaveOccurred(),
			fmt.Sprintf("kubectl apply demo-remote-init-job.yaml failed: %s", out))

		// Wait for Job to reach Complete condition within 2 minutes.
		By("Waiting for demo-remote-init Job to reach Complete")
		Eventually(func() error {
			cmd := exec.CommandContext(ctx, "kubectl", "--kubeconfig", kubeconfigPath,
				"wait", "--for=condition=Complete",
				"job/demo-remote-init", "-n", mediumHTTPNamespace, "--timeout=10s")
			_, err := cmd.CombinedOutput()
			return err
		}, 2*time.Minute, 5*time.Second).Should(Succeed(),
			"demo-remote-init Job must reach Complete within 2 minutes")

		GinkgoWriter.Printf("medium-http-test: demo-remote-init Job Complete in %s\n", mediumHTTPNamespace)
	})

	// -------------------------------------------------------------------------
	// Spec 2: git-http server Deployment reaches Available.
	// The Deployment serves the bare repo (populated by Spec 1) over HTTP via
	// git-http-backend CGI + nginx + fcgiwrap.
	// -------------------------------------------------------------------------
	It("git-http server Deployment is Available", func() {
		Skip("git-http-server image not yet built — pending 08-05")

		// Apply the git-http server Deployment + ClusterIP Service.
		By("Applying git-http-server-deployment.yaml into " + mediumHTTPNamespace)
		cmd := exec.CommandContext(ctx, "kubectl", "--kubeconfig", kubeconfigPath,
			"apply", "-f", "../../examples/projects/medium/git-http-server-deployment.yaml",
			"-n", mediumHTTPNamespace, "--timeout=30s")
		out, err := cmd.CombinedOutput()
		Expect(err).NotTo(HaveOccurred(),
			fmt.Sprintf("kubectl apply git-http-server-deployment.yaml failed: %s", out))

		// Wait for Deployment Available condition within 2 minutes.
		By("Waiting for git-http-server Deployment to reach Available")
		Eventually(func() error {
			cmd := exec.CommandContext(ctx, "kubectl", "--kubeconfig", kubeconfigPath,
				"wait", "--for=condition=Available",
				"deployment/"+mediumHTTPServiceName, "-n", mediumHTTPNamespace, "--timeout=10s")
			_, err := cmd.CombinedOutput()
			return err
		}, 2*time.Minute, 5*time.Second).Should(Succeed(),
			"git-http-server Deployment must reach Available within 2 minutes")

		GinkgoWriter.Printf("medium-http-test: git-http-server Deployment Available in %s\n", mediumHTTPNamespace)
	})

	// -------------------------------------------------------------------------
	// Spec 3: medium Project with stub-subagent reaches Complete over http://.
	// This exercises the go-git HTTP transport (pure-Go) end-to-end:
	//   - clone Job (cmd/tide-push --mode=clone) clones from git-http-server
	//   - stub-subagent returns canned envelopes (no LLM cost)
	//   - push Job (cmd/tide-push --mode=push) pushes back to git-http-server
	// -------------------------------------------------------------------------
	It("medium Project with stub-subagent reaches Complete over http://", func() {
		Skip("pending 08-05 + 08-07 wiring")

		// Create a medium-style Project with the stub subagent and http:// targetRepo.
		// The stub-subagent ignores targetRepo but the clone/push Jobs use it for
		// the real go-git HTTP transport path (exercising SC-5).
		projName := fmt.Sprintf("medium-http-project-%d", GinkgoRandomSeed())
		proj := &tideprojectv1alpha1.Project{
			ObjectMeta: metav1.ObjectMeta{
				Name:      projName,
				Namespace: mediumHTTPNamespace,
			},
			Spec: tideprojectv1alpha1.ProjectSpec{
				TargetRepo:        mediumHTTPTargetRepo,
				ProviderSecretRef: "tide-secrets",
				Budget: tideprojectv1alpha1.BudgetConfig{
					AbsoluteCapCents: 0,
				},
				Subagent: tideprojectv1alpha1.SubagentConfig{
					Model: "stub",
				},
				Gates: tideprojectv1alpha1.Gates{
					Milestone:         "auto",
					Phase:             "auto",
					Plan:              "auto",
					Task:              "auto",
					PauseBetweenWaves: false,
				},
			},
		}
		Expect(k8sClient.Create(ctx, proj)).To(Succeed(),
			"medium Project must be admitted (http:// targetRepo passes CEL rule)")

		defer func() {
			_ = k8sClient.Delete(ctx, proj)
		}()

		// Wait for Project to reach Complete within 10 minutes.
		By("Waiting for medium Project to reach Complete over http://")
		Eventually(func() error {
			var current tideprojectv1alpha1.Project
			if err := k8sClient.Get(ctx, client.ObjectKey{
				Name:      projName,
				Namespace: mediumHTTPNamespace,
			}, &current); err != nil {
				return err
			}
			if current.Status.Phase != "Complete" {
				return fmt.Errorf("medium Project %s: want Status.Phase=Complete, got %q",
					projName, current.Status.Phase)
			}
			return nil
		}, 10*time.Minute, 5*time.Second).Should(Succeed(),
			"medium Project must reach Status.Phase=Complete within 10 minutes (stub + http:// transport)")

		GinkgoWriter.Printf("medium-http-test: Project %s reached Complete\n", projName)
	})
})
