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
//   - real git-http server image, exercising go-git HTTP transport without LLM cost).
package kind_integration

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	tideprojectv1alpha3 "github.com/jsquirrelz/tide/api/v1alpha3"
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
            - containerPort: 8080
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
      targetPort: 8080
      protocol: TCP
`, ns, mediumHTTPServerImage, ns)
}

// mediumBaseRefSeedJobYAML returns a one-shot Job that seeds a NON-default
// branch (base-ref-target) on the demo remote so the baseRef e2e can base a run
// off a ref whose tip differs from the default branch.
//
// It reuses the git-http-server image (Alpine with a full `git` binary + shell,
// already loaded into the cluster by BeforeAll) and drives the exact anonymous
// smart-HTTP path the production clone/push Jobs use: clone over http://, branch
// from the default HEAD, add one distinguishing commit, push the new branch
// back. The default-branch HEAD (DEFAULT_TIP=) and the new branch tip
// (BASE_REF_TARGET_TIP=) are echoed to the Job pod log for the spec to harvest.
//
// The demo remote's default branch is `master` (go-git PlainInit picks it —
// cmd/tide-demo-init/main.go); the run branch created here therefore tips at a
// commit unreachable from master, proving a run really based off the non-default
// branch rather than silently falling back to HEAD.
func mediumBaseRefSeedJobYAML(ns string) string {
	script := strings.Join([]string{
		"set -eu",
		"export HOME=/tmp",
		"cd /tmp",
		"rm -rf work",
		"git clone " + mediumHTTPTargetRepo + " work",
		"cd work",
		"git config user.email seed@tide.local",
		"git config user.name tide-base-ref-seed",
		`echo "DEFAULT_TIP=$(git rev-parse HEAD)"`,
		"git checkout -b base-ref-target",
		`printf 'phase35 base-ref-target distinguishing content\n' > BASE_REF_TARGET.md`,
		"git add BASE_REF_TARGET.md",
		"git commit -m 'phase35: base-ref-target distinguishing commit'",
		"git push origin base-ref-target",
		`echo "BASE_REF_TARGET_TIP=$(git rev-parse HEAD)"`,
	}, "\n")
	return fmt.Sprintf(`apiVersion: batch/v1
kind: Job
metadata:
  name: base-ref-seed
  namespace: %s
  labels:
    app.kubernetes.io/component: base-ref-seed
spec:
  backoffLimit: 1
  template:
    metadata:
      labels:
        app.kubernetes.io/component: base-ref-seed
    spec:
      restartPolicy: Never
      containers:
        - name: base-ref-seed
          image: %s
          imagePullPolicy: IfNotPresent
          command: ["/bin/sh", "-c"]
          args:
            - %q
`, ns, mediumHTTPServerImage, script)
}

// loadRequiredImage loads a REQUIRED Layer-B fixture image into the kind cluster.
// These images are private (unpublished) and the consuming pods set
// imagePullPolicy=IfNotPresent, so a missing image cannot be pulled — it is a
// hard error. Failing here with an actionable message is deliberate: a silent
// skip previously let the missing image surface downstream as a misleading
// 2-minute "Job never completes" timeout (the historical "medium_http flake").
// Build the images with `make test-int-kind-prep` before running the suite.
func loadRequiredImage(image string) {
	checkCmd := exec.CommandContext(ctx, "docker", "image", "inspect", image)
	if err := checkCmd.Run(); err != nil {
		Fail(fmt.Sprintf("required fixture image %s not found locally — run `make test-int-kind-prep` "+
			"before the kind suite (it is a private image; pods cannot pull it with IfNotPresent)", image))
	}
	loadCmd := exec.CommandContext(ctx, "kind", "load", "docker-image", image, "--name", kindClusterName)
	out, err := loadCmd.CombinedOutput()
	Expect(err).NotTo(HaveOccurred(), "kind load docker-image %s failed:\n%s", image, out)
	GinkgoWriter.Printf("Loaded image %s into kind cluster %s\n", image, kindClusterName)
}

var _ = Describe("Medium http transport", Label("kind"), Ordered, func() {

	BeforeAll(func() {
		skipIfCRDsOnlyMode()
		// createNamespace provisions the namespace + tide-subagent SA + tide-projects
		// PVC + tide-signing-key Secret + PVC prewarm (same helper used by other specs).
		createNamespace(mediumHTTPNamespace)
		// Spec 3 runs REAL clone / wave-integration / boundary-push pods —
		// their Jobs reference serviceAccountName tide-push, which the chart
		// only installs in tide-system (push-rbac.yaml cross-namespace caveat).
		ensurePushSARBAC(mediumHTTPNamespace)

		// Load the git-http-server and demo-init images into the kind cluster so
		// the Deployment and Job Pods can pull them with IfNotPresent.
		By("Loading tide-demo-init image into kind cluster")
		loadRequiredImage(mediumHTTPDemoInitImage)
		By("Loading tide-git-http-server image into kind cluster")
		loadRequiredImage(mediumHTTPServerImage)

		// Create the demo-remote-pvc (distinct from tide-projects, used by the
		// init Job and git-http server to share the bootstrapped bare repo).
		By("Creating demo-remote-pvc in " + mediumHTTPNamespace)
		Expect(applyYAML(mediumDemoRemotePVCYAML(mediumHTTPNamespace))).To(Succeed(),
			"demo-remote-pvc must be created in "+mediumHTTPNamespace)
	})

	AfterAll(func() {
		if CurrentSpecReport().Failed() {
			// Dump CRD/Job/Pod/Event state BEFORE deleting the namespace —
			// otherwise the failure diagnostics are unrecoverable (PR #3
			// CI iterations lost the spec-3 stall point to this ordering).
			dumpNamespaceState(mediumHTTPNamespace)
			exportKindLogs()
		}
		deleteNamespaceAndWait(mediumHTTPNamespace)
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

		// debug #15: verify ANONYMOUS PUSH is enabled. git-http-backend advertises
		// the receive-pack service over info/refs?service=git-receive-pack ONLY when
		// the served repo's config has http.receivepack=true (set by tide-demo-init
		// at seed time + self-healed by the server entrypoint). If it is unset,
		// git-http-backend returns 403 Forbidden and `wget -q` exits non-zero — which
		// is exactly the "authorization failed" boundary-push failure observed live.
		// This assertion fails CI if the receive-pack path regresses, instead of the
		// prior silent pass (Spec 3 only asserted Project=Complete, which does NOT
		// gate on push success — see debug #13b).
		By("Verifying git-http-backend advertises receive-pack for anonymous push (debug #15)")
		recvRefsURL := "http://git-http-server." + mediumHTTPNamespace + ".svc.cluster.local/demo-remote.git/info/refs?service=git-receive-pack"
		var recvOutput string
		Eventually(func() error {
			wgetCmd := exec.CommandContext(ctx, "kubectl", "--kubeconfig", kubeconfigPath,
				"run", "git-recv-smoke", "--rm", "-i",
				"--restart=Never",
				"--image=busybox:1.36",
				"--namespace="+mediumHTTPNamespace,
				"--", "wget", "-q", "-O-", recvRefsURL)
			out, err := wgetCmd.CombinedOutput()
			if err != nil {
				// Non-zero exit here is the regression signal: a 403 from
				// git-http-backend (receive-pack disabled) makes wget -q fail.
				return fmt.Errorf("wget git-receive-pack info/refs failed (anonymous push likely disabled — http.receivepack not set?): %w\n%s", err, out)
			}
			recvOutput = string(out)
			return nil
		}, 2*time.Minute, 10*time.Second).Should(Succeed(),
			"git-receive-pack info/refs endpoint must be reachable (anonymous push enabled)")

		Expect(recvOutput).To(ContainSubstring("git-receive-pack"),
			"git-http-backend info/refs response must advertise 'git-receive-pack' (anonymous push enabled via http.receivepack=true)")

		GinkgoWriter.Printf("medium-http-test: anonymous push verified (info/refs advertises git-receive-pack)\n")
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
		// Build the Project via the shared typed fixture builder (fixtures_test.go).
		// The builder defaults schemaRevision + $0 budget + stub subagent + auto
		// gates; this spec layers on the http:// targetRepo and git config that
		// exercise go-git's HTTP transport through the clone/push Jobs.
		proj := newStubProject(mediumHTTPNamespace, projName,
			withTargetRepo(mediumHTTPTargetRepo),
			withProviderSecret("tide-secrets"),
			withGit(mediumHTTPTargetRepo, "tide-secrets"))
		By("Creating medium Project (stub subagent, http:// targetRepo) in " + mediumHTTPNamespace)
		Expect(k8sClient.Create(ctx, proj)).To(Succeed(),
			"medium Project must be admitted (http:// targetRepo passes CEL rule)")

		// Dump the namespace on failure BEFORE any teardown cascades the
		// hierarchy away (a function-level defer would run ahead of this and
		// destroy the evidence — why previous failures left no diagnosable
		// state). Project cleanup itself belongs to AfterAll's
		// deleteNamespaceAndWait; this is the container's final spec.
		DeferCleanup(func() {
			if CurrentSpecReport().Failed() {
				dumpNamespaceState(mediumHTTPNamespace)
			}
		})

		// Wait for Project to reach Complete within 10 minutes. The periodic
		// GinkgoWriter progress line lands in the live timeline, so the
		// stall point survives even when a flake-retry or suite-teardown
		// skip swallows the Eventually's final failure text.
		By("Waiting for medium Project to reach Complete over http://")
		lastProgress := time.Now()
		Eventually(func() error {
			var current tideprojectv1alpha3.Project
			if err := k8sClient.Get(ctx, client.ObjectKey{
				Name:      projName,
				Namespace: mediumHTTPNamespace,
			}, &current); err != nil {
				return err
			}
			if current.Status.Phase != "Complete" {
				if time.Since(lastProgress) > 30*time.Second {
					lastProgress = time.Now()
					GinkgoWriter.Printf("medium-http progress: phase=%q branch=%q cloneComplete=%v lastCondition=%s children=[%s]\n",
						current.Status.Phase, current.Status.Git.BranchName,
						current.Status.Git.CloneComplete, mediumLastConditionMessage(current),
						mediumChildSummary())
				}
				return fmt.Errorf("medium Project %s: want Status.Phase=Complete, got %q (last condition: %s)",
					projName, current.Status.Phase, mediumLastConditionMessage(current))
			}
			return nil
		}, 10*time.Minute, 5*time.Second).Should(Succeed(),
			"medium Project must reach Status.Phase=Complete within 10 minutes (stub + http:// transport)")

		GinkgoWriter.Printf("medium-http-test: Project %s reached Complete\n", projName)
	})

	// -------------------------------------------------------------------------
	// Spec 4 (Phase 35 BASE-01 e2e): a Project with spec.git.baseRef set to a
	// non-default branch reaches cloneComplete with status.git.baseSHA stamped
	// to that branch's tip — the whole chain against a real cluster and real
	// git-http remote: chart-installed CRD → controller plumb-through → clone
	// Job → refs/remotes/origin resolution → termination-message envelope →
	// status stamp. envtest (plan 35-03) fabricates envelopes; only this Layer B
	// spec proves the real clone Job resolves a real non-default branch.
	//
	// Runs LAST (single heavy run at a time on a constrained VM — CLAUDE.md
	// recipe): Spec 3's Project has already reached Complete, the git-http
	// server + tide-secrets are up, and this spec is clone-stage-scoped (it does
	// NOT wait for Complete), so it adds only a short bounded run.
	//
	// Scope (binding): HAPPY path only. Halt/release mechanics for an
	// unresolvable ref are fully locked at Layer A (plan 35-03,
	// project_baseref_halt_test.go); a kind-level unresolvable case would double
	// suite runtime for no new coverage.
	It("stamps status.git.baseSHA from a non-default baseRef branch over http://", func() {
		skipIfCRDsOnlyMode()

		// Seed a non-default branch (base-ref-target) whose tip differs from the
		// default branch, and harvest both tips from the seed Job's pod log.
		By("Applying base-ref-seed Job into " + mediumHTTPNamespace)
		Expect(applyYAML(mediumBaseRefSeedJobYAML(mediumHTTPNamespace))).To(Succeed(),
			"base-ref-seed Job must be applied into "+mediumHTTPNamespace)

		By("Waiting for base-ref-seed Job to reach Complete")
		Eventually(func() error {
			cmd := exec.CommandContext(ctx, "kubectl", "--kubeconfig", kubeconfigPath,
				"wait", "--for=condition=Complete",
				"job/base-ref-seed", "-n", mediumHTTPNamespace, "--timeout=10s")
			out, err := cmd.CombinedOutput()
			if err != nil {
				return fmt.Errorf("base-ref-seed Job not yet Complete: %w\n%s", err, out)
			}
			return nil
		}, 2*time.Minute, 5*time.Second).Should(Succeed(),
			"base-ref-seed Job must reach Complete within 2 minutes")

		By("Harvesting DEFAULT_TIP and BASE_REF_TARGET_TIP from the seed Job log")
		logsCmd := exec.CommandContext(ctx, "kubectl", "--kubeconfig", kubeconfigPath,
			"logs", "job/base-ref-seed", "-n", mediumHTTPNamespace, "--tail=-1")
		logOut, err := logsCmd.CombinedOutput()
		Expect(err).NotTo(HaveOccurred(), "kubectl logs job/base-ref-seed failed:\n%s", logOut)
		defaultTip := grepGreppedTip(string(logOut), "DEFAULT_TIP=")
		targetTip := grepGreppedTip(string(logOut), "BASE_REF_TARGET_TIP=")
		Expect(targetTip).To(MatchRegexp(`^[0-9a-f]{40}$`),
			"seed Job must print a 40-hex BASE_REF_TARGET_TIP; got %q (log:\n%s)", targetTip, logOut)
		Expect(defaultTip).To(MatchRegexp(`^[0-9a-f]{40}$`),
			"seed Job must print a 40-hex DEFAULT_TIP; got %q (log:\n%s)", defaultTip, logOut)
		Expect(targetTip).NotTo(Equal(defaultTip),
			"base-ref-target tip must differ from the default branch tip (distinguishing commit)")
		GinkgoWriter.Printf("medium-http-test: base-ref-target tip=%s default tip=%s\n", targetTip, defaultTip)

		// tide-secrets (empty GIT_PAT for anonymous http) is required by the
		// clone Job. Spec 3 creates it too; apply is idempotent.
		By("Ensuring tide-secrets Secret exists in " + mediumHTTPNamespace)
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
			"tide-secrets Secret must exist in "+mediumHTTPNamespace)

		// Create a bare-shape Project basing its run off base-ref-target.
		projName := fmt.Sprintf("baseref-http-project-%d", GinkgoRandomSeed())
		proj := newStubProject(mediumHTTPNamespace, projName,
			withTargetRepo(mediumHTTPTargetRepo),
			withProviderSecret("tide-secrets"),
			withGit(mediumHTTPTargetRepo, "tide-secrets"),
			withBaseRef("base-ref-target"))
		By("Creating baseRef Project (baseRef=base-ref-target) in " + mediumHTTPNamespace)
		Expect(createFixture(ctx, proj)).To(Succeed(),
			"baseRef Project must be admitted (base-ref-target passes the charset Pattern)")

		// Bound the extra load: once the clone-stage assertion passes, delete the
		// Project so it does not keep reconciling the full hierarchy.
		DeferCleanup(func() {
			if CurrentSpecReport().Failed() {
				dumpNamespaceState(mediumHTTPNamespace)
			}
			_ = k8sClient.Delete(context.Background(), proj)
		})

		By("Waiting for baseRef Project clone to complete and stamp baseSHA")
		Eventually(func() error {
			var current tideprojectv1alpha3.Project
			if err := k8sClient.Get(ctx, client.ObjectKey{
				Name:      projName,
				Namespace: mediumHTTPNamespace,
			}, &current); err != nil {
				return err
			}
			if !current.Status.Git.CloneComplete {
				return fmt.Errorf("baseRef Project %s: cloneComplete not yet true (last condition: %s)",
					projName, mediumLastConditionMessage(current))
			}
			if current.Status.Git.BaseSHA == "" {
				return fmt.Errorf("baseRef Project %s: cloneComplete true but baseSHA not yet stamped", projName)
			}
			return nil
		}, 5*time.Minute, 5*time.Second).Should(Succeed(),
			"baseRef Project must reach cloneComplete with baseSHA stamped within 5 minutes")

		var final tideprojectv1alpha3.Project
		Expect(k8sClient.Get(ctx, client.ObjectKey{
			Name:      projName,
			Namespace: mediumHTTPNamespace,
		}, &final)).To(Succeed())
		Expect(final.Status.Git.BaseSHA).To(Equal(targetTip),
			"status.git.baseSHA must equal the base-ref-target branch tip (real resolution, not HEAD fallback)")
		Expect(final.Status.Git.BaseSHA).NotTo(Equal(defaultTip),
			"status.git.baseSHA must NOT equal the default branch tip (proves the run based off the non-default branch)")
		GinkgoWriter.Printf("medium-http-test: baseRef Project %s stamped baseSHA=%s\n", projName, final.Status.Git.BaseSHA)
	})
})

// grepGreppedTip returns the value following the first line beginning with
// prefix (e.g. "BASE_REF_TARGET_TIP=") in the given multi-line log, trimmed of
// surrounding whitespace. Returns "" when the prefix is absent.
func grepGreppedTip(log, prefix string) string {
	for line := range strings.SplitSeq(log, "\n") {
		line = strings.TrimSpace(line)
		if after, ok := strings.CutPrefix(line, prefix); ok {
			return strings.TrimSpace(after)
		}
	}
	return ""
}

// mediumChildSummary lists every child CRD in the medium namespace with its
// phase — the progress line's pointer to WHICH level of the hierarchy a
// stalled run is stuck at (spec 3 exercises all five levels; the Project's
// own conditions do not change while a mid-hierarchy level is wedged).
func mediumChildSummary() string {
	var parts []string
	var msList tideprojectv1alpha3.MilestoneList
	if err := k8sClient.List(ctx, &msList, client.InNamespace(mediumHTTPNamespace)); err == nil {
		for i := range msList.Items {
			parts = append(parts, fmt.Sprintf("ms/%s=%s", msList.Items[i].Name, msList.Items[i].Status.Phase))
		}
	}
	var phList tideprojectv1alpha3.PhaseList
	if err := k8sClient.List(ctx, &phList, client.InNamespace(mediumHTTPNamespace)); err == nil {
		for i := range phList.Items {
			parts = append(parts, fmt.Sprintf("ph/%s=%s", phList.Items[i].Name, phList.Items[i].Status.Phase))
		}
	}
	var plList tideprojectv1alpha3.PlanList
	if err := k8sClient.List(ctx, &plList, client.InNamespace(mediumHTTPNamespace)); err == nil {
		for i := range plList.Items {
			wi := plList.Items[i].Status.WaveIntegration
			parts = append(parts, fmt.Sprintf("plan/%s=%s(integ=%d,val=%s,wiWave=%d,wiAttempts=%d,wiErr=%q)",
				plList.Items[i].Name, plList.Items[i].Status.Phase,
				plList.Items[i].Status.IntegratedThroughWave, plList.Items[i].Status.ValidationState,
				wi.Wave, wi.Attempts, wi.LastError))
		}
	}
	var tList tideprojectv1alpha3.TaskList
	if err := k8sClient.List(ctx, &tList, client.InNamespace(mediumHTTPNamespace)); err == nil {
		for i := range tList.Items {
			parts = append(parts, fmt.Sprintf("task/%s=%s", tList.Items[i].Name, tList.Items[i].Status.Phase))
		}
	}
	var jList batchv1.JobList
	if err := k8sClient.List(ctx, &jList, client.InNamespace(mediumHTTPNamespace)); err == nil {
		for i := range jList.Items {
			state := "running"
			for _, c := range jList.Items[i].Status.Conditions {
				if c.Status == corev1.ConditionTrue &&
					(c.Type == batchv1.JobComplete || c.Type == batchv1.JobFailed) {
					state = string(c.Type)
				}
			}
			parts = append(parts, fmt.Sprintf("job/%s=%s(f=%d)", jList.Items[i].Name, state, jList.Items[i].Status.Failed))
		}
	}
	return strings.Join(parts, " ")
}

// mediumLastConditionMessage returns the last condition message for a Project,
// for diagnostic output in Eventually error messages.
func mediumLastConditionMessage(proj tideprojectv1alpha3.Project) string {
	conds := proj.Status.Conditions
	if len(conds) == 0 {
		return "(no conditions)"
	}
	last := conds[len(conds)-1]
	return fmt.Sprintf("type=%s reason=%s msg=%s", last.Type, last.Reason, strings.TrimSpace(last.Message))
}
