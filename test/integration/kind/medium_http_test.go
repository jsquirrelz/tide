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

// Package kind_integration holds Layer B integration tests that exercise the
// full TIDE wave lifecycle in a real kind cluster.
//
// This file covers SC-5: hermetic medium-http kind integration test.
// The git-http-server (nginx + fcgiwrap + git-http-backend) serves a bare
// repo over in-cluster http://, exercising the pure-Go go-git HTTP transport
// path without any LLM cost (stub-subagent).
//
// Coverage target:
//   - SC-5: CI coverage for the medium/http transport path (hermetic stub-subagent
//     + real git-http server image, exercising go-git HTTP transport without LLM cost).
package kind_integration

import (
	"fmt"
	"os/exec"
	"strings"
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

	// mediumHTTPDemoInitImage is the init job image that bootstraps the bare repo.
	mediumHTTPDemoInitImage = "ghcr.io/jsquirrelz/tide-demo-init:1.0.0"

	// mediumHTTPServerImage is the git-http-backend server image.
	mediumHTTPServerImage = "ghcr.io/jsquirrelz/tide-git-http-server:1.0.0"
)

// mediumDemoRemotePVCYAML returns the demo-remote PVC YAML for the given namespace.
// The PVC uses ReadWriteOnce (required for kind's local-path provisioner, which does
// not support ReadWriteMany). Named demo-remote-pvc, distinct from the tide-projects
// PVC that createNamespace() provisions.
func mediumDemoRemotePVCYAML(ns string) string {
	return fmt.Sprintf(`apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: demo-remote-pvc
  namespace: %s
  labels:
    app.kubernetes.io/component: demo-remote
spec:
  accessModes: [ReadWriteOnce]
  resources:
    requests:
      storage: 100Mi
`, ns)
}

// mediumDemoRemoteInitJobYAML returns the init Job YAML for the given namespace.
// Mirrors examples/projects/medium/demo-remote-init-job.yaml but with the
// namespace overridden for the test namespace.
func mediumDemoRemoteInitJobYAML(ns string) string {
	return fmt.Sprintf(`apiVersion: batch/v1
kind: Job
metadata:
  name: demo-remote-init
  namespace: %s
  labels:
    app.kubernetes.io/component: demo-remote-init
spec:
  backoffLimit: 1
  template:
    metadata:
      labels:
        app.kubernetes.io/component: demo-remote-init
    spec:
      restartPolicy: Never
      containers:
        - name: tide-demo-init
          image: %s
          imagePullPolicy: IfNotPresent
          args:
            - --bootstrap-dir=/workspace/demo-remote.git
          volumeMounts:
            - name: demo-remote
              mountPath: /workspace
      volumes:
        - name: demo-remote
          persistentVolumeClaim:
            claimName: demo-remote-pvc
`, ns, mediumHTTPDemoInitImage)
}

// mediumGitHTTPServerYAML returns the Deployment + Service YAML for the git-http
// server in the given namespace. Mirrors git-http-server-deployment.yaml but with
// the namespace overridden.
func mediumGitHTTPServerYAML(ns string) string {
	return fmt.Sprintf(`apiVersion: apps/v1
kind: Deployment
metadata:
  name: git-http-server
  namespace: %s
  labels:
    app.kubernetes.io/component: git-http-server
spec:
  replicas: 1
  selector:
    matchLabels:
      app: git-http-server
  template:
    metadata:
      labels:
        app: git-http-server
    spec:
      containers:
        - name: git-http-server
          image: %s
          imagePullPolicy: IfNotPresent
          ports:
            - containerPort: 80
              protocol: TCP
          volumeMounts:
            - name: demo-remote
              mountPath: /srv/git
          securityContext:
            runAsUser: 1000
            runAsNonRoot: true
            readOnlyRootFilesystem: false
      volumes:
        - name: demo-remote
          persistentVolumeClaim:
            claimName: demo-remote-pvc
---
apiVersion: v1
kind: Service
metadata:
  name: git-http-server
  namespace: %s
  labels:
    app.kubernetes.io/component: git-http-server
spec:
  type: ClusterIP
  selector:
    app: git-http-server
  ports:
    - name: http
      port: 80
      targetPort: 80
      protocol: TCP
`, ns, mediumHTTPServerImage, ns)
}

// loadImageIfNeeded loads a Docker image into the kind cluster if it exists locally.
// If the image is not present locally, it is skipped (the image must have been
// built by make test-int-kind-prep or equivalent before the suite runs).
func loadImageIfNeeded(image string) {
	// Check if the image exists locally.
	checkCmd := exec.CommandContext(ctx, "docker", "image", "inspect", image)
	if err := checkCmd.Run(); err != nil {
		GinkgoWriter.Printf("Image %s not found locally — skipping kind load (image must be pre-built)\n", image)
		return
	}
	// Load the image into kind.
	loadCmd := exec.CommandContext(ctx, "kind", "load", "docker-image", image, "--name", kindClusterName)
	out, err := loadCmd.CombinedOutput()
	if err != nil {
		GinkgoWriter.Printf("Warning: kind load docker-image %s failed: %v\n%s\n", image, err, out)
	} else {
		GinkgoWriter.Printf("Loaded image %s into kind cluster %s\n", image, kindClusterName)
	}
}

var _ = Describe("Medium http transport", Label("kind"), Ordered, func() {

	BeforeAll(func() {
		skipIfCRDsOnlyMode()
		// createNamespace provisions the namespace + tide-subagent SA + tide-projects
		// PVC + tide-signing-key Secret + PVC prewarm (same helper used by other specs).
		createNamespace(mediumHTTPNamespace)

		// Load the git-http-server and demo-init images into the kind cluster so
		// the Deployment and Job Pods can pull them with IfNotPresent.
		By("Loading tide-demo-init image into kind cluster")
		loadImageIfNeeded(mediumHTTPDemoInitImage)
		By("Loading tide-git-http-server image into kind cluster")
		loadImageIfNeeded(mediumHTTPServerImage)

		// Create the demo-remote-pvc (distinct from tide-projects, used by the
		// init Job and git-http server to share the bootstrapped bare repo).
		By("Creating demo-remote-pvc in " + mediumHTTPNamespace)
		Expect(applyYAML(mediumDemoRemotePVCYAML(mediumHTTPNamespace))).To(Succeed(),
			"demo-remote-pvc must be created in "+mediumHTTPNamespace)
	})

	AfterAll(func() {
		deleteNamespace(mediumHTTPNamespace)
		if CurrentSpecReport().Failed() {
			exportKindLogs()
		}
	})

	// -------------------------------------------------------------------------
	// Spec 1: demo-remote-init Job bootstraps the bare repo on demo-remote-pvc.
	// The init Job (cmd/tide-demo-init) pushes the embedded fixture over the
	// file:// transport to the PVC. After completion, the PVC contains
	// demo-remote.git and can be served by the git-http server Deployment.
	// -------------------------------------------------------------------------
	It("initializes the git-http server via demo-remote-init Job", func() {
		// Apply the demo-remote-init Job into the test namespace.
		By("Applying demo-remote-init Job into " + mediumHTTPNamespace)
		Expect(applyYAML(mediumDemoRemoteInitJobYAML(mediumHTTPNamespace))).To(Succeed(),
			"demo-remote-init Job must be applied into "+mediumHTTPNamespace)

		// Wait for Job to reach Complete condition within 2 minutes.
		By("Waiting for demo-remote-init Job to reach Complete")
		Eventually(func() error {
			cmd := exec.CommandContext(ctx, "kubectl", "--kubeconfig", kubeconfigPath,
				"wait", "--for=condition=Complete",
				"job/demo-remote-init", "-n", mediumHTTPNamespace, "--timeout=10s")
			out, err := cmd.CombinedOutput()
			if err != nil {
				return fmt.Errorf("job not yet Complete: %w\n%s", err, out)
			}
			return nil
		}, 2*time.Minute, 5*time.Second).Should(Succeed(),
			"demo-remote-init Job must reach Complete within 2 minutes")

		GinkgoWriter.Printf("medium-http-test: demo-remote-init Job Complete in %s\n", mediumHTTPNamespace)
	})

	// -------------------------------------------------------------------------
	// Spec 2: git-http server Deployment reaches Available and serves git protocol.
	// The Deployment serves the bare repo (populated by Spec 1) over HTTP via
	// git-http-backend CGI + nginx + fcgiwrap.
	//
	// RESEARCH Pitfall 1 validation: verify git-http-backend is correctly serving
	// smart HTTP (info/refs?service=git-upload-pack endpoint returns expected output).
	// -------------------------------------------------------------------------
	It("git-http server Deployment is Available", func() {
		// Apply the git-http server Deployment + ClusterIP Service.
		By("Applying git-http-server Deployment + Service into " + mediumHTTPNamespace)
		Expect(applyYAML(mediumGitHTTPServerYAML(mediumHTTPNamespace))).To(Succeed(),
			"git-http-server Deployment+Service must be applied into "+mediumHTTPNamespace)

		// Wait for Deployment Available condition within 2 minutes.
		By("Waiting for git-http-server Deployment to reach Available")
		Eventually(func() error {
			cmd := exec.CommandContext(ctx, "kubectl", "--kubeconfig", kubeconfigPath,
				"wait", "--for=condition=Available",
				"deployment/"+mediumHTTPServiceName, "-n", mediumHTTPNamespace, "--timeout=10s")
			out, err := cmd.CombinedOutput()
			if err != nil {
				return fmt.Errorf("deployment not yet Available: %w\n%s", err, out)
			}
			return nil
		}, 2*time.Minute, 5*time.Second).Should(Succeed(),
			"git-http-server Deployment must reach Available within 2 minutes")

		GinkgoWriter.Printf("medium-http-test: git-http-server Deployment Available in %s\n", mediumHTTPNamespace)

		// Verify git-http-backend serves the smart HTTP protocol by hitting the
		// info/refs?service=git-upload-pack endpoint from a transient busybox pod.
		// The response must contain "git-upload-pack" (RESEARCH Pitfall 1 validation).
		By("Verifying git-http-backend serves git smart HTTP protocol")
		infoRefsURL := "http://git-http-server." + mediumHTTPNamespace + ".svc.cluster.local/demo-remote.git/info/refs?service=git-upload-pack"
		var smokeOutput string
		Eventually(func() error {
			// kubectl run a transient busybox pod to wget the info/refs endpoint.
			// --rm --restart=Never makes it single-shot; -i captures stdout.
			wgetCmd := exec.CommandContext(ctx, "kubectl", "--kubeconfig", kubeconfigPath,
				"run", "git-smoke", "--rm", "-i",
				"--restart=Never",
				"--image=busybox:1.36",
				"--namespace="+mediumHTTPNamespace,
				"--", "wget", "-q", "-O-", infoRefsURL)
			out, err := wgetCmd.CombinedOutput()
			if err != nil {
				return fmt.Errorf("wget git smart HTTP failed: %w\n%s", err, out)
			}
			smokeOutput = string(out)
			return nil
		}, 2*time.Minute, 10*time.Second).Should(Succeed(),
			"git smart HTTP info/refs endpoint must be reachable from within cluster")

		Expect(smokeOutput).To(ContainSubstring("git-upload-pack"),
			"git-http-backend info/refs response must contain 'git-upload-pack' (smart HTTP protocol)")

		GinkgoWriter.Printf("medium-http-test: git smart HTTP verified (info/refs response contains git-upload-pack)\n")
	})

	// -------------------------------------------------------------------------
	// Spec 3: medium Project with stub-subagent reaches Complete over http://.
	// This exercises the go-git HTTP transport (pure-Go) end-to-end:
	//   - clone Job (cmd/tide-push --mode=clone) clones from git-http-server
	//   - stub-subagent returns canned envelopes (no LLM cost)
	//   - push Job (cmd/tide-push --mode=push) pushes back to git-http-server
	// -------------------------------------------------------------------------
	It("medium Project with stub-subagent reaches Complete over http://", func() {
		skipIfCRDsOnlyMode()

		// Create the tide-secrets Secret with empty GIT_PAT for anonymous
		// in-cluster http:// push (scheme-conditional guard in tide-push accepts
		// empty PAT for non-https schemes per Phase 8 Plan 08-05 Task 3).
		By("Creating tide-secrets Secret in " + mediumHTTPNamespace)
		tideSecretsYAML := fmt.Sprintf(`apiVersion: v1
kind: Secret
metadata:
  name: tide-secrets
  namespace: %s
type: Opaque
data:
  ANTHROPIC_API_KEY: dGVzdC1hcGkta2V5LXN0dWItc3ViYWdlbnQtZG9lcy1ub3QtdXNlLWl0
  GIT_PAT: ""
`, mediumHTTPNamespace)
		Expect(applyYAML(tideSecretsYAML)).To(Succeed(),
			"tide-secrets Secret must be created in "+mediumHTTPNamespace)

		// Create a medium-style Project with the stub subagent and http:// targetRepo.
		// The stub-subagent ignores the LLM calls but the clone/push Jobs use
		// targetRepo for the real go-git HTTP transport path (exercising SC-5).
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
				Git: &tideprojectv1alpha1.GitConfig{
					RepoURL:        mediumHTTPTargetRepo,
					CredsSecretRef: "tide-secrets",
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
				return fmt.Errorf("medium Project %s: want Status.Phase=Complete, got %q (last condition: %s)",
					projName, current.Status.Phase, mediumLastConditionMessage(current))
			}
			return nil
		}, 10*time.Minute, 5*time.Second).Should(Succeed(),
			"medium Project must reach Status.Phase=Complete within 10 minutes (stub + http:// transport)")

		GinkgoWriter.Printf("medium-http-test: Project %s reached Complete\n", projName)
	})
})

// mediumLastConditionMessage returns the last condition message for a Project,
// for diagnostic output in Eventually error messages.
func mediumLastConditionMessage(proj tideprojectv1alpha1.Project) string {
	conds := proj.Status.Conditions
	if len(conds) == 0 {
		return "(no conditions)"
	}
	last := conds[len(conds)-1]
	return fmt.Sprintf("type=%s reason=%s msg=%s", last.Type, last.Reason, strings.TrimSpace(last.Message))
}
