//go:build kind_e2e

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

// kind_setup_test.go — Shared BeforeSuite / AfterSuite for the Phase 4 kind
// E2E suite (plan 04-14 Task 2). Vendored from test/integration/kind/suite_test.go's
// helper set (the Phase 02.2 harness) because those helpers live in package
// `kind_integration` and are not exported for cross-package consumption.
//
// Why a separate `kind_e2e` build tag (not the existing `e2e`):
//
//  1. The kubebuilder `e2e` suite (e2e_suite_test.go's TestE2E) uses
//     kustomize-driven `make deploy` against a kind cluster; this kind_e2e
//     suite uses helm-driven `helm install ./charts/tide` against a SECOND
//     kind cluster — two paradigms cannot share a single BeforeSuite without
//     fighting over the cluster lifecycle.
//  2. The `live_e2e` precedent (live_claude_test.go) already proves the
//     pattern: distinct build tag, distinct test entry-point, same `package e2e`.
//
// SKIP_KIND_TESTS=true short-circuits the suite (mirrors Phase 02.2 contract).
// kind + docker + helm must all be on PATH; if any is missing the suite
// Skips with a clear message (no Fail()).
//
// Cluster name: `tide-e2e-phase4` — DELIBERATELY distinct from `tide-test`
// (Phase 02.2 integration suite) and `tide-test-e2e` (kubebuilder e2e suite)
// so parallel CI runs don't collide.
package e2e

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	tideprojectv1alpha1 "github.com/jsquirrelz/tide/api/v1alpha1"
)

const (
	// kindE2EClusterName is the Phase 4 plan 04-14 cluster name. Distinct
	// from `tide-test` (test/integration/kind/) and `tide-test-e2e`
	// (test/e2e/ kubebuilder suite) to avoid collisions when multiple test
	// suites run in parallel CI.
	kindE2EClusterName = "tide-e2e-phase4"

	// kindE2EControllerNamespace is where helm installs tide (chart default
	// + our --create-namespace flag).
	kindE2EControllerNamespace = "tide-system"

	// kindE2EDashboardDeployment is the Helm chart's dashboard Deployment
	// name (see charts/tide/templates/dashboard-deployment.yaml).
	kindE2EDashboardDeployment = "tide-dashboard"

	// kindE2EDashboardService is the dashboard Service name (port 80 →
	// targetPort 8080).
	kindE2EDashboardService = "tide-dashboard"

	// kindE2ETestTimeout bounds BeforeSuite + spec execution.
	// Mirrors the 7m floor proven in Phase 02.2 (50s pre-helm setup + 300s
	// helm --timeout 5m + slack). Phase 4 adds binary builds (~30s for
	// tide-cli + dashboard image), so 10m gives generous headroom.
	kindE2ETestTimeout = 10 * time.Minute

	// kindE2EBinDir is where built CLI binaries land. The dashboard image
	// is built via `docker build` and loaded into kind directly.
	kindE2EBinDir = "bin"
)

var (
	// kindE2EClient is the controller-runtime client used by every spec in
	// the kind_e2e suite. Wired in BeforeSuite.
	kindE2EClient client.Client

	// kindE2ECtx + kindE2ECancel bound the suite. Cancelled in AfterSuite
	// so per-spec exec.CommandContext invocations exit promptly.
	kindE2ECtx    context.Context
	kindE2ECancel context.CancelFunc

	// kindE2EKubeconfigPath is the file written by `kind get kubeconfig`,
	// passed to every kubectl / helm invocation.
	kindE2EKubeconfigPath string

	// kindE2ETideCLI is the absolute path to the built tide CLI binary
	// (bin/tide). Resolved in BeforeSuite via `make tide-cli`.
	kindE2ETideCLI string
)

// TestKindE2E is the ginkgo entry point for the Phase 4 kind-harness E2E suite.
//
// SKIP_KIND_TESTS=true short-circuits — same gate as Phase 02.2's
// test/integration/kind/suite_test.go. Without docker or kind on PATH the
// BeforeSuite Skips (not Fails) so dev machines without container tooling
// pass cleanly.
func TestKindE2E(t *testing.T) {
	if os.Getenv("SKIP_KIND_TESTS") == "true" {
		t.Skip("SKIP_KIND_TESTS=true; skipping Phase 4 kind E2E suite")
	}
	RegisterFailHandler(Fail)
	RunSpecs(t, "Phase 4 Kind E2E Suite (plan 04-14)")
}

var _ = BeforeSuite(func() {
	logf.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))
	kindE2ECtx, kindE2ECancel = context.WithTimeout(context.Background(), kindE2ETestTimeout)

	By("Ensuring TIDE CRD types are registered in the runtime scheme")
	Expect(tideprojectv1alpha1.AddToScheme(scheme.Scheme)).To(Succeed())

	By("Checking that kind, docker, and helm are available")
	for _, tool := range []string{"kind", "docker", "helm", "kubectl"} {
		if _, err := exec.LookPath(tool); err != nil {
			Skip(tool + " not found in PATH; skipping kind E2E suite")
		}
	}

	By("Creating or reusing kind cluster " + kindE2EClusterName)
	kindEnsureCluster()

	By("Obtaining kubeconfig for kind cluster")
	kindE2EKubeconfigPath = kindGetKubeconfig()

	By("Building k8s client from kind kubeconfig")
	cfg, err := clientcmd.BuildConfigFromFlags("", kindE2EKubeconfigPath)
	Expect(err).NotTo(HaveOccurred())
	kindE2EClient, err = client.New(cfg, client.Options{Scheme: scheme.Scheme})
	Expect(err).NotTo(HaveOccurred())

	By("Building the tide CLI binary (make tide-cli)")
	kindBuildCLI()

	By("Applying TIDE CRDs from config/crd/bases")
	kindApplyCRDs()

	By("Installing cert-manager")
	kindInstallCertManager()

	By("Building + loading controller + dashboard images into kind")
	kindBuildAndLoadImages()

	By("Applying TIDE chart via helm install (dashboard.enabled=true)")
	kindApplyChart()

	By("Waiting for controller-manager + dashboard Deployments to be Ready")
	kindWaitForDeployment("tide-controller-manager")
	kindWaitForDeployment(kindE2EDashboardDeployment)

	GinkgoWriter.Println("Phase 4 kind E2E suite ready; cluster: " + kindE2EClusterName)
})

var _ = AfterSuite(func() {
	if kindE2ECancel != nil {
		kindE2ECancel()
	}

	if os.Getenv("KEEP_KIND_CLUSTER") == "true" {
		GinkgoWriter.Println("KEEP_KIND_CLUSTER=true; keeping cluster for inspection")
		return
	}

	By("Deleting kind cluster " + kindE2EClusterName)
	kindCleanupCluster()
})

// kindEnsureCluster creates the kind cluster if it doesn't already exist.
// Mirrors test/integration/kind/suite_test.go:ensureKindCluster.
func kindEnsureCluster() {
	out, err := exec.Command("kind", "get", "clusters").CombinedOutput()
	if err == nil {
		for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
			if line == kindE2EClusterName {
				GinkgoWriter.Println("Reusing existing kind cluster: " + kindE2EClusterName)
				return
			}
		}
	}

	// No cluster.yaml override — default single-node kind config is fine for
	// the Phase 4 smoke surface (no multi-node Pod scheduling exercised).
	cmd := exec.CommandContext(kindE2ECtx, "kind", "create", "cluster",
		"--name", kindE2EClusterName,
		"--wait", "120s",
	)
	out, err = cmd.CombinedOutput()
	if err != nil {
		Fail(fmt.Sprintf("kind create cluster failed: %v\n%s", err, out))
	}
	GinkgoWriter.Printf("Created kind cluster %s\n%s\n", kindE2EClusterName, out)
}

// kindGetKubeconfig writes the kind kubeconfig to a temp file.
func kindGetKubeconfig() string {
	out, err := exec.CommandContext(kindE2ECtx, "kind", "get", "kubeconfig", "--name", kindE2EClusterName).Output()
	Expect(err).NotTo(HaveOccurred(), "failed to get kind kubeconfig")

	tmpFile, err := os.CreateTemp("", "tide-e2e-phase4-kubeconfig-*.yaml")
	Expect(err).NotTo(HaveOccurred())
	_, err = tmpFile.Write(out)
	Expect(err).NotTo(HaveOccurred())
	Expect(tmpFile.Close()).To(Succeed())

	return tmpFile.Name()
}

// kindBuildCLI runs `make tide-cli` from the repo root and resolves the
// resulting binary's absolute path. Stored in kindE2ETideCLI for use by
// gate_flow_test.go (which invokes `tide approve` + `tide tail`).
func kindBuildCLI() {
	repoRoot, err := kindRepoRoot()
	Expect(err).NotTo(HaveOccurred())

	cmd := exec.CommandContext(kindE2ECtx, "make", "tide-cli")
	cmd.Dir = repoRoot
	out, err := cmd.CombinedOutput()
	if err != nil {
		Fail(fmt.Sprintf("make tide-cli failed: %v\n%s", err, out))
	}

	kindE2ETideCLI = filepath.Join(repoRoot, kindE2EBinDir, "tide")
	if _, statErr := os.Stat(kindE2ETideCLI); os.IsNotExist(statErr) {
		Fail("make tide-cli did not produce bin/tide at " + kindE2ETideCLI)
	}
	GinkgoWriter.Println("tide CLI built at " + kindE2ETideCLI)
}

// kindApplyCRDs applies CRDs from config/crd/bases to the kind cluster.
func kindApplyCRDs() {
	repoRoot, err := kindRepoRoot()
	Expect(err).NotTo(HaveOccurred())

	crdDir := filepath.Join(repoRoot, "config", "crd", "bases")
	entries, err := os.ReadDir(crdDir)
	if err != nil {
		GinkgoWriter.Printf("Warning: CRD dir %s missing: %v\n", crdDir, err)
		return
	}

	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".yaml") {
			continue
		}
		path := filepath.Join(crdDir, entry.Name())
		cmd := exec.CommandContext(kindE2ECtx, "kubectl",
			"--kubeconfig", kindE2EKubeconfigPath,
			"apply", "-f", path, "--timeout=30s")
		out, applyErr := cmd.CombinedOutput()
		if applyErr != nil {
			GinkgoWriter.Printf("Warning: kubectl apply -f %s failed: %v\n%s\n",
				entry.Name(), applyErr, out)
		}
	}
	time.Sleep(2 * time.Second)
}

// kindInstallCertManager mirrors test/integration/kind/suite_test.go's
// installCertManager — required so the webhook server cert reconciles.
func kindInstallCertManager() {
	const version = "v1.20.2"
	url := fmt.Sprintf("https://github.com/cert-manager/cert-manager/releases/download/%s/cert-manager.yaml", version)

	GinkgoWriter.Printf("Installing cert-manager %s\n", version)
	applyCmd := exec.CommandContext(kindE2ECtx, "kubectl",
		"--kubeconfig", kindE2EKubeconfigPath,
		"apply", "-f", url, "--timeout=120s")
	out, err := applyCmd.CombinedOutput()
	if err != nil {
		GinkgoWriter.Printf("Warning: cert-manager apply failed: %v\n%s\n", err, out)
		return
	}

	for _, deploy := range []string{"cert-manager", "cert-manager-cainjector", "cert-manager-webhook"} {
		waitCmd := exec.CommandContext(kindE2ECtx, "kubectl",
			"--kubeconfig", kindE2EKubeconfigPath,
			"-n", "cert-manager",
			"rollout", "status", "deployment/"+deploy,
			"--timeout=120s")
		wOut, wErr := waitCmd.CombinedOutput()
		if wErr != nil {
			GinkgoWriter.Printf("Warning: cert-manager %s not ready: %v\n%s\n", deploy, wErr, wOut)
		}
	}
}

// kindBuildAndLoadImages builds the controller image (tag :test) + dashboard
// image (placeholder Dockerfile is greenfield in Phase 4; we tag the existing
// controller image as the dashboard since the dashboard binary embeds in the
// same multi-stage container in v1.x). Phase 5 splits these into distinct
// Dockerfiles + ghcr.io pushes. For now we satisfy the chart's image refs
// with two `kind load docker-image` invocations against the same digest.
//
// This is a known limitation captured in Phase 5 DIST-04 follow-up: ship
// separate dashboard Dockerfile + Docker Hub publish. For Phase 4 smoke
// purposes the chart needs SOMETHING to pull; tagging the existing manager
// image is the minimal viable shim.
func kindBuildAndLoadImages() {
	repoRoot, err := kindRepoRoot()
	Expect(err).NotTo(HaveOccurred())

	// Controller image — the existing Dockerfile builds /manager.
	buildCmd := exec.CommandContext(kindE2ECtx, "docker", "build",
		"-t", "controller:phase4-test",
		"-t", "ghcr.io/jsquirrelz/tide-dashboard:phase4-test",
		"-f", "Dockerfile", ".")
	buildCmd.Dir = repoRoot
	out, err := buildCmd.CombinedOutput()
	if err != nil {
		Fail(fmt.Sprintf("docker build failed: %v\n%s", err, out))
	}

	// Load both tags into kind.
	for _, img := range []string{"controller:phase4-test", "ghcr.io/jsquirrelz/tide-dashboard:phase4-test"} {
		loadCmd := exec.CommandContext(kindE2ECtx, "kind", "load", "docker-image", img, "--name", kindE2EClusterName)
		lOut, lErr := loadCmd.CombinedOutput()
		if lErr != nil {
			Fail(fmt.Sprintf("kind load docker-image %s failed: %v\n%s", img, lErr, lOut))
		}
	}
}

// kindApplyChart helm-installs the chart with the Phase 4 dashboard enabled
// and the test image tags loaded above.
func kindApplyChart() {
	repoRoot, err := kindRepoRoot()
	Expect(err).NotTo(HaveOccurred())

	chartDir := filepath.Join(repoRoot, "charts", "tide")
	if _, statErr := os.Stat(chartDir); os.IsNotExist(statErr) {
		Fail("charts/tide not present at " + chartDir)
	}

	helmCmd := exec.CommandContext(kindE2ECtx, "helm",
		"upgrade", "--install", "tide", chartDir,
		"--create-namespace", "-n", kindE2EControllerNamespace,
		"--kubeconfig", kindE2EKubeconfigPath,
		"--set", "controllerManager.manager.image.repository=controller",
		"--set", "controllerManager.manager.image.tag=phase4-test",
		"--set", "controllerManager.manager.image.pullPolicy=IfNotPresent",
		"--set", "dashboard.enabled=true",
		"--set", "dashboard.image.tag=phase4-test",
		"--set", "dashboard.image.pullPolicy=IfNotPresent",
		// kind's default local-path provisioner only supports RWO.
		"--set", "workspaces.pvc.accessModes={ReadWriteOnce}",
		"--wait", "--timeout", "5m",
	)
	start := time.Now()
	out, err := helmCmd.CombinedOutput()
	elapsed := time.Since(start)
	if err != nil {
		Fail(fmt.Sprintf("helm upgrade --install failed after %s: %v\n%s", elapsed, err, out))
	}
	GinkgoWriter.Printf("helm install completed in %s\n", elapsed)
}

// kindWaitForDeployment waits up to 120s for `deploy/<name>` in
// kindE2EControllerNamespace to be Available.
func kindWaitForDeployment(name string) {
	cmd := exec.CommandContext(kindE2ECtx, "kubectl",
		"--kubeconfig", kindE2EKubeconfigPath,
		"-n", kindE2EControllerNamespace,
		"rollout", "status", "deployment/"+name,
		"--timeout=120s")
	out, err := cmd.CombinedOutput()
	if err != nil {
		Fail(fmt.Sprintf("rollout status %s failed: %v\n%s", name, err, out))
	}
}

// kindCleanupCluster mirrors Phase 02.2's cleanupKindCluster — best-effort
// kind delete + docker rm fallback for zombie containers (moby/moby#51845).
func kindCleanupCluster() {
	cmd := exec.Command("kind", "delete", "cluster", "--name", kindE2EClusterName)
	out, err := cmd.CombinedOutput()
	if err != nil {
		GinkgoWriter.Printf("Warning: kind delete failed: %v\n%s\n", err, out)
	}

	listCmd := exec.Command("docker", "ps", "-aq",
		"--filter", fmt.Sprintf("label=io.x-k8s.kind.cluster=%s", kindE2EClusterName))
	idsOut, listErr := listCmd.Output()
	if listErr != nil {
		return
	}
	ids := strings.Fields(strings.TrimSpace(string(idsOut)))
	if len(ids) == 0 {
		return
	}
	rmArgs := append([]string{"rm", "-f", "-v"}, ids...)
	if _, rmErr := exec.Command("docker", rmArgs...).CombinedOutput(); rmErr != nil {
		GinkgoWriter.Printf("Warning: docker rm -f fallback failed: %v\n", rmErr)
	}
}

// kindRepoRoot returns the absolute path of the repo root (4 levels up from
// test/e2e/). Used to locate the Makefile + charts/tide + config/crd dirs
// regardless of the test process's cwd (Ginkgo can run from anywhere).
func kindRepoRoot() (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	// test/e2e/ → repo root is two levels up.
	return filepath.Clean(filepath.Join(cwd, "..", "..")), nil
}

// kindApplyYAML applies a YAML string to the cluster via kubectl stdin.
func kindApplyYAML(yaml string) error {
	cmd := exec.CommandContext(kindE2ECtx, "kubectl",
		"--kubeconfig", kindE2EKubeconfigPath,
		"apply", "-f", "-", "--timeout=30s")
	cmd.Stdin = strings.NewReader(yaml)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("kubectl apply failed: %w\n%s", err, out)
	}
	return nil
}

// kindDeleteNamespace removes a test fixture namespace (best-effort).
func kindDeleteNamespace(ns string) {
	if os.Getenv("KEEP_KIND_NAMESPACES") == "true" {
		return
	}
	cmd := exec.CommandContext(context.Background(), "kubectl",
		"--kubeconfig", kindE2EKubeconfigPath,
		"delete", "namespace", ns,
		"--ignore-not-found=true", "--timeout=30s")
	_, _ = cmd.CombinedOutput()
}

// kindRunCLI invokes the built tide CLI binary, capturing stdout + stderr +
// exit code. The kubeconfig is wired via KUBECONFIG env so the CLI's
// client-go config loader picks it up.
func kindRunCLI(ctx context.Context, args ...string) (string, string, int, error) {
	cmd := exec.CommandContext(ctx, kindE2ETideCLI, args...)
	cmd.Env = append(os.Environ(), "KUBECONFIG="+kindE2EKubeconfigPath)
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			return stdout.String(), stderr.String(), -1, err
		}
	}
	return stdout.String(), stderr.String(), exitCode, nil
}
