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
// full TIDE wave lifecycle in a real kind cluster with stub-subagent Jobs.
//
// Prerequisites (handled by `make test-int-kind-prep`):
//   - Docker available
//   - kind v0.31+ available
//   - stub-subagent + credproxy images loaded via `kind load docker-image`
//
// Run via:
//
//	make test-int
//
// These tests are gated by the `kind` Ginkgo label and run sequentially
// (--procs=1) because they share a single kind cluster.
package kind_integration

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
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	tideprojectv1alpha1 "github.com/jsquirrelz/tide/api/v1alpha1"
)

const (
	kindClusterName = "tide-test"
	kindNamespace   = "tide-int-test"
	kindTestTimeout = 4 * time.Minute

	// kindControllerNamespace is the namespace the tide-controller-manager
	// Deployment installs into (config/default Kustomize manifest target).
	// WR-09: split from kindNamespace (which is the *test fixture*
	// namespace) so a future config/default change doesn't silently break
	// the readiness check.
	kindControllerNamespace  = "tide-system"
	kindControllerDeployment = "tide-controller-manager"
)

var (
	k8sClient    client.Client
	ctx          context.Context
	cancel       context.CancelFunc
	kubeconfigPath string
)

// TestIntegrationKind is the entry point for the Layer B kind suite.
func TestIntegrationKind(t *testing.T) {
	if os.Getenv("SKIP_KIND_TESTS") == "true" {
		t.Skip("SKIP_KIND_TESTS=true; skipping Layer B kind tests")
	}
	RegisterFailHandler(Fail)
	RunSpecs(t, "Integration Kind Suite")
}

var _ = BeforeSuite(func() {
	logf.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))
	ctx, cancel = context.WithTimeout(context.Background(), kindTestTimeout)

	By("Ensuring TIDE CRD types are registered in the scheme")
	Expect(tideprojectv1alpha1.AddToScheme(scheme.Scheme)).To(Succeed())

	By("Checking if kind is available")
	if _, err := exec.LookPath("kind"); err != nil {
		Skip("kind not found in PATH; skipping Layer B kind tests (install kind v0.31+)")
	}

	By("Checking if docker is available")
	if _, err := exec.LookPath("docker"); err != nil {
		Skip("docker not found in PATH; skipping Layer B kind tests (install Docker)")
	}

	By("Creating or reusing kind cluster " + kindClusterName)
	ensureKindCluster()

	By("Obtaining kubeconfig for kind cluster")
	kubeconfigPath = getKindKubeconfig()

	By("Building k8s client from kind kubeconfig")
	cfg, err := clientcmd.BuildConfigFromFlags("", kubeconfigPath)
	Expect(err).NotTo(HaveOccurred())
	k8sClient, err = client.New(cfg, client.Options{Scheme: scheme.Scheme})
	Expect(err).NotTo(HaveOccurred())

	By("Applying TIDE CRDs to kind cluster")
	applyCRDs()

	By("Applying TIDE controller Deployment via kustomize")
	applyController()

	By("Waiting for controller-manager Deployment to become ready")
	waitForControllerReady()

	GinkgoWriter.Println("Layer B kind suite ready; cluster: " + kindClusterName)
})

var _ = AfterSuite(func() {
	cancel()

	if os.Getenv("KEEP_KIND_CLUSTER") == "true" {
		GinkgoWriter.Println("KEEP_KIND_CLUSTER=true; keeping kind cluster for debug inspection")
		return
	}

	By("Deleting kind cluster " + kindClusterName)
	cmd := exec.Command("kind", "delete", "cluster", "--name", kindClusterName)
	output, err := cmd.CombinedOutput()
	if err != nil {
		GinkgoWriter.Printf("Warning: failed to delete kind cluster: %v\n%s\n", err, output)
	}
})

// ensureKindCluster creates the kind cluster if it does not already exist.
func ensureKindCluster() {
	// Check if cluster already exists.
	out, err := exec.Command("kind", "get", "clusters").CombinedOutput()
	if err == nil {
		for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
			if line == kindClusterName {
				GinkgoWriter.Println("Reusing existing kind cluster: " + kindClusterName)
				return
			}
		}
	}

	// cluster.yaml is in the same directory as this test file.
	clusterConfig := filepath.Join("cluster.yaml")
	cmd := exec.CommandContext(ctx, "kind", "create", "cluster",
		"--name", kindClusterName,
		"--config", clusterConfig,
		"--wait", "120s",
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		Fail(fmt.Sprintf("Failed to create kind cluster: %v\n%s", err, output))
	}
	GinkgoWriter.Printf("Created kind cluster %s\n%s\n", kindClusterName, output)
}

// getKindKubeconfig writes the kind kubeconfig to a temp file and returns the path.
func getKindKubeconfig() string {
	out, err := exec.CommandContext(ctx, "kind", "get", "kubeconfig", "--name", kindClusterName).Output()
	Expect(err).NotTo(HaveOccurred(), "Failed to get kind kubeconfig")

	tmpFile, err := os.CreateTemp("", "tide-kind-kubeconfig-*.yaml")
	Expect(err).NotTo(HaveOccurred())
	_, err = tmpFile.Write(out)
	Expect(err).NotTo(HaveOccurred())
	Expect(tmpFile.Close()).To(Succeed())

	return tmpFile.Name()
}

// applyCRDs applies the TIDE CRDs from config/crd/bases/ to the kind cluster.
func applyCRDs() {
	crdDir := filepath.Join("..", "..", "..", "config", "crd", "bases")
	entries, err := os.ReadDir(crdDir)
	if err != nil {
		GinkgoWriter.Printf("Warning: CRD dir not found at %s: %v\n", crdDir, err)
		return
	}

	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".yaml") {
			continue
		}
		crdFile := filepath.Join(crdDir, entry.Name())
		cmd := exec.CommandContext(ctx, "kubectl", "--kubeconfig", kubeconfigPath,
			"apply", "-f", crdFile, "--timeout=30s")
		output, err := cmd.CombinedOutput()
		if err != nil {
			GinkgoWriter.Printf("Warning: CRD apply failed for %s: %v\n%s\n", entry.Name(), err, output)
		}
	}

	// Brief wait for CRDs to be established.
	time.Sleep(2 * time.Second)
}

// applyController applies the TIDE controller via kubectl kustomize.
// Falls back to skipping if config/default is not present or kustomize fails.
func applyController() {
	// Create the namespace first.
	cmd := exec.CommandContext(ctx, "kubectl", "--kubeconfig", kubeconfigPath,
		"create", "namespace", kindNamespace, "--dry-run=client", "-o", "yaml")
	nsYAML, err := cmd.Output()
	if err == nil {
		applyCmd := exec.CommandContext(ctx, "kubectl", "--kubeconfig", kubeconfigPath,
			"apply", "-f", "-")
		applyCmd.Stdin = strings.NewReader(string(nsYAML))
		_, _ = applyCmd.CombinedOutput()
	}

	configDefault := filepath.Join("..", "..", "..", "config", "default")
	if _, err := os.Stat(configDefault); os.IsNotExist(err) {
		GinkgoWriter.Println("config/default not found; skipping controller Deployment install")
		return
	}

	// Use kustomize if available.
	if _, lookErr := exec.LookPath("kustomize"); lookErr == nil {
		buildCmd := exec.CommandContext(ctx, "kustomize", "build", configDefault)
		manifest, buildErr := buildCmd.Output()
		if buildErr != nil {
			GinkgoWriter.Printf("kustomize build failed: %v\n", buildErr)
			return
		}
		applyCmd := exec.CommandContext(ctx, "kubectl", "--kubeconfig", kubeconfigPath,
			"apply", "-f", "-", "--timeout=60s")
		applyCmd.Stdin = strings.NewReader(string(manifest))
		out, applyErr := applyCmd.CombinedOutput()
		if applyErr != nil {
			GinkgoWriter.Printf("Warning: controller install failed (tests may still work with CRDs only): %v\n%s\n", applyErr, out)
		}
	} else {
		GinkgoWriter.Println("kustomize not found; skipping controller Deployment install")
	}
}

// waitForControllerReady waits up to 90s for the controller Deployment to
// have at least 1 ready replica. Skips silently if not deployed.
//
// WR-09: emits the CRDs-only fallback via stdout (Fprintln on GinkgoWriter
// plus an env-var-gated hard fail) so the CI signal is not invisible. Set
// TIDE_REQUIRE_CONTROLLER=1 to fail the suite if the Deployment is absent
// — opt-in so dev-machine runs that only want CRD-level coverage still
// pass.
func waitForControllerReady() {
	// If no Deployment was installed (CRDs-only mode), skip wait.
	checkCmd := exec.CommandContext(ctx, "kubectl", "--kubeconfig", kubeconfigPath,
		"get", "deployment", kindControllerDeployment,
		"-n", kindControllerNamespace, "--ignore-not-found=true",
		"--output=name")
	out, err := checkCmd.Output()
	if err != nil || strings.TrimSpace(string(out)) == "" {
		msg := fmt.Sprintf("%s Deployment not found in namespace %q; running in CRDs-only mode",
			kindControllerDeployment, kindControllerNamespace)
		// GinkgoWriter for the Gingko log, stdout for `go test -v` visibility.
		GinkgoWriter.Println(msg)
		fmt.Fprintln(os.Stdout, "WARN(kind suite): "+msg)
		if os.Getenv("TIDE_REQUIRE_CONTROLLER") == "1" {
			Fail(msg + " (TIDE_REQUIRE_CONTROLLER=1)")
		}
		return
	}

	// Wait up to 90s for the Deployment to be ready.
	deadline := time.Now().Add(90 * time.Second)
	for time.Now().Before(deadline) {
		ready, _ := isDeploymentReady()
		if ready {
			GinkgoWriter.Printf("%s is ready in namespace %q\n",
				kindControllerDeployment, kindControllerNamespace)
			return
		}
		time.Sleep(2 * time.Second)
	}
	GinkgoWriter.Printf("Warning: %s not ready in namespace %q after 90s; tests may fail\n",
		kindControllerDeployment, kindControllerNamespace)
}

func isDeploymentReady() (bool, error) {
	cmd := exec.CommandContext(ctx, "kubectl", "--kubeconfig", kubeconfigPath,
		"get", "deployment", kindControllerDeployment,
		"-n", kindControllerNamespace,
		"-o", "jsonpath={.status.readyReplicas}")
	out, err := cmd.Output()
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(string(out)) == "1", nil
}

// applyYAML applies a YAML string to the kind cluster via kubectl stdin.
func applyYAML(yaml string) error {
	cmd := exec.CommandContext(ctx, "kubectl", "--kubeconfig", kubeconfigPath,
		"apply", "-f", "-", "--timeout=30s")
	cmd.Stdin = strings.NewReader(yaml)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("kubectl apply failed: %w\n%s", err, out)
	}
	return nil
}

// applyFile applies a YAML file to the kind cluster.
func applyFile(path string) error {
	cmd := exec.CommandContext(ctx, "kubectl", "--kubeconfig", kubeconfigPath,
		"apply", "-f", path, "--timeout=30s")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("kubectl apply -f %s failed: %w\n%s", path, err, out)
	}
	return nil
}

// deleteNamespace deletes a namespace from the kind cluster.
func deleteNamespace(ns string) {
	cmd := exec.CommandContext(context.Background(), "kubectl", "--kubeconfig", kubeconfigPath,
		"delete", "namespace", ns, "--ignore-not-found=true", "--timeout=30s")
	_, _ = cmd.CombinedOutput()
}

// kubectlLogs returns the logs of a container in a pod.
func kubectlLogs(ns, podName, container string) string {
	cmd := exec.CommandContext(context.Background(), "kubectl", "--kubeconfig", kubeconfigPath,
		"logs", podName, "-n", ns, "-c", container, "--tail=50")
	out, err := cmd.Output()
	if err != nil {
		return fmt.Sprintf("(failed to get logs: %v)", err)
	}
	return string(out)
}

// exportKindLogs dumps kind cluster logs for CI debugging on failure.
func exportKindLogs() {
	logsDir := filepath.Join(os.TempDir(), "kind-logs-"+kindClusterName)
	cmd := exec.CommandContext(context.Background(), "kind", "export", "logs",
		"--name", kindClusterName, logsDir)
	out, err := cmd.CombinedOutput()
	if err != nil {
		GinkgoWriter.Printf("Warning: kind export logs failed: %v\n%s\n", err, out)
	} else {
		GinkgoWriter.Printf("Kind logs exported to: %s\n", logsDir)
	}
}

// Compile-time check that ctrl import is used (manager scheme access).
var _ = ctrl.Log
