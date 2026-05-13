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

	// kindTestTimeout bounds the BeforeSuite + spec execution context.
	// It MUST exceed:
	//   (a) pre-helm setup cost (~50s — kind connect + CRDs + cert-manager)
	//   (b) helm install --wait --timeout 5m budget (applyController()'s
	//       explicit --timeout)
	//   (c) waitForControllerReady fallback (~5s, no-op when helm --wait
	//       succeeds)
	//   (d) variance margin (60s — slow CI runners, image-pull stalls)
	//
	// 7m = 50s + 300s + 5s + 60s + slack. Going below ~6m25s shadow-kills
	// the helm subprocess before its own --timeout fires (see
	// .planning/phases/02.1-.../02.1-04-VERIFICATION.md §"Root-cause
	// analysis" for the failure mode this constant closes — Phase 02.2).
	kindTestTimeout = 7 * time.Minute

	// kindControllerNamespace is the namespace the tide-controller-manager
	// Deployment installs into (config/default Kustomize manifest target).
	// WR-09: split from kindNamespace (which is the *test fixture*
	// namespace) so a future config/default change doesn't silently break
	// the readiness check.
	kindControllerNamespace  = "tide-system"
	kindControllerDeployment = "tide-controller-manager"
)

var (
	k8sClient      client.Client
	ctx            context.Context
	cancel         context.CancelFunc
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

	By("Installing cert-manager (required by config/default webhook certs)")
	installCertManager()

	By("Applying TIDE controller Deployment via helm")
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
	cleanupKindCluster()
})

// cleanupKindCluster performs robust kind cluster cleanup with a docker-rm
// fallback for the zombie-container case (kind delete cluster fails when
// docker rm -f cannot kill a control-plane container that's stuck in a
// non-responsive state — see kind issue #1116 and moby/moby#51845 for the
// Docker 28→29 kill-event regression).
//
// Cleanup is best-effort; failure here does not fail the test suite — the
// next make test-int-kind-prep run will detect any residual state and
// attempt a fresh cluster create.
//
// Uses plain exec.Command() (no ctx) because the outer test ctx was
// cancelled at the top of AfterSuite — passing it to exec.CommandContext
// would immediately abort cleanup (Phase 02.2 RESEARCH Pitfall 7).
//
// Source of the failure mode:
// .planning/phases/02.1-.../02.1-04-VERIFICATION.md §"Failure Detail".
func cleanupKindCluster() {
	// Attempt 1: kind delete (happy path).
	cmd := exec.Command("kind", "delete", "cluster", "--name", kindClusterName)
	out, err := cmd.CombinedOutput()
	if err != nil {
		GinkgoWriter.Printf("Warning: kind delete cluster failed: %v\n%s\n", err, out)
	}

	// Verify no residual containers regardless of kind's exit code; kind
	// has been observed to exit 0 but leave containers in a stuck state
	// (Docker daemon kill-event race — moby/moby#51845). The label
	// `io.x-k8s.kind.cluster=<name>` is kind's documented internal
	// convention (kind pkg/cluster/internal/providers/docker) and survives
	// a future multi-node cluster.yaml change.
	listCmd := exec.Command("docker", "ps", "-aq",
		"--filter", fmt.Sprintf("label=io.x-k8s.kind.cluster=%s", kindClusterName))
	idsOut, listErr := listCmd.Output()
	if listErr != nil {
		GinkgoWriter.Printf("Warning: docker ps fallback failed: %v\n", listErr)
		return
	}
	ids := strings.Fields(strings.TrimSpace(string(idsOut)))
	if len(ids) == 0 {
		return // success: kind delete cleaned up properly
	}

	GinkgoWriter.Printf("Residual kind containers detected after delete: %v; force-removing\n", ids)
	rmArgs := append([]string{"rm", "-f", "-v"}, ids...)
	rmCmd := exec.Command("docker", rmArgs...)
	if rmOut, rmErr := rmCmd.CombinedOutput(); rmErr != nil {
		GinkgoWriter.Printf("Warning: docker rm -f fallback failed: %v\n%s\n", rmErr, rmOut)
	}
}

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

// certManagerVersion is the cert-manager release used in Layer B kind tests.
// Pinned per STACK.md spirit (versions documented; no @sha pin yet for the
// remote URL — see TODO). Override with TIDE_CERT_MANAGER_VERSION for
// local testing.
//
// v1.20.2 adds explicit K8s 1.33 support (matches kindest/node:v1.33.7
// pinned by cluster.yaml); chart-side usage of cert-manager.io/v1 Issuer +
// Certificate is unchanged (both chart Certificate resources specify
// issuerRef.kind: Issuer + group: cert-manager.io explicitly, so the
// v1.20 issuerRef-defaults revert is non-impacting). See
// cert-manager.io/docs/releases/release-notes/release-notes-1.20/ for
// the v1.20 changelog; risk surface for the bump is documented in
// .planning/phases/02.2-.../02.2-RESEARCH.md §"Pattern 4" (Phase 02.2).
const certManagerVersion = "v1.20.2"

// installCertManager installs cert-manager into the kind cluster so the
// `config/default` overlay's webhook certificate Issuer + Certificate can
// be reconciled. Without cert-manager, the manager Pod stays stuck on
// `MountVolume.SetUp failed: secret "webhook-server-cert" not found`.
func installCertManager() {
	version := os.Getenv("TIDE_CERT_MANAGER_VERSION")
	if version == "" {
		version = certManagerVersion
	}
	url := fmt.Sprintf("https://github.com/cert-manager/cert-manager/releases/download/%s/cert-manager.yaml", version)

	GinkgoWriter.Printf("Installing cert-manager %s from %s\n", version, url)
	applyCmd := exec.CommandContext(ctx, "kubectl", "--kubeconfig", kubeconfigPath,
		"apply", "-f", url, "--timeout=120s")
	out, err := applyCmd.CombinedOutput()
	if err != nil {
		GinkgoWriter.Printf("Warning: cert-manager apply failed: %v\n%s\n", err, out)
		return
	}

	// Wait for cert-manager Deployments to become Ready (webhook is the slowest).
	for _, deploy := range []string{"cert-manager", "cert-manager-cainjector", "cert-manager-webhook"} {
		waitCmd := exec.CommandContext(ctx, "kubectl", "--kubeconfig", kubeconfigPath,
			"-n", "cert-manager",
			"rollout", "status", "deployment/"+deploy,
			"--timeout=120s")
		out, err := waitCmd.CombinedOutput()
		if err != nil {
			GinkgoWriter.Printf("Warning: cert-manager %s not ready: %v\n%s\n", deploy, err, out)
			return
		}
	}
	GinkgoWriter.Println("cert-manager ready")
}

// applyController installs the TIDE controller via helm.
// Helm is the canonical install path (BOOT-04) AND it provides the
// chart-only resources (signing-key Secret, projects PVC, subagent SA,
// Deployment args) that kustomize omits. helm install --wait blocks until
// every chart resource is Ready, replacing the bespoke waitForControllerReady
// poll (which is kept as a fallback but expected to no-op).
//
// Phase 02.1 D-02 (02.1-BASELINE.md): the install path PIVOTS from
// `kustomize build config/default | kubectl apply -f -` to `helm install
// tide ./charts/tide --create-namespace -n tide-system ...`. The chart
// provides four pieces the kustomize overlay omits and the controller's
// Phase 2 wiring requires: tide-signing-key Secret, tide-projects PVC,
// tide-subagent ServiceAccount, and the --subagent-image / --credproxy-image
// / --default-file-touch-mode Deployment args. Without them the manager
// Pod CrashLoopBackOffs on startup with
// `TIDE_SIGNING_KEY env var is required (HARN-03)` before any spec runs.
//
// The WR-09 contract (D-03) is preserved: when helm is missing or the
// install fails, soft-skip with a GinkgoWriter warning by default;
// hard Fail only when TIDE_REQUIRE_CONTROLLER=1.
func applyController() {
	// Pre-create the test-fixture namespace (separate from tide-system which
	// helm creates via --create-namespace). Test fixtures applied to
	// kindNamespace later in test bodies would fail without this.
	cmd := exec.CommandContext(ctx, "kubectl", "--kubeconfig", kubeconfigPath,
		"create", "namespace", kindNamespace, "--dry-run=client", "-o", "yaml")
	nsYAML, err := cmd.Output()
	if err == nil {
		applyCmd := exec.CommandContext(ctx, "kubectl", "--kubeconfig", kubeconfigPath,
			"apply", "-f", "-")
		applyCmd.Stdin = strings.NewReader(string(nsYAML))
		_, _ = applyCmd.CombinedOutput()
	}

	// Ensure the tide-subagent ServiceAccount exists in the test-fixture
	// namespace (wave_test.go fixtures apply Tasks here; jobspec.go:43 sets
	// ServiceAccountName: tide-subagent on every Pod, so the SA must exist
	// in any namespace where Tasks run — chart only templates it in
	// tide-system / .Release.Namespace).
	ensureSubagentSA(kindNamespace)

	// Resolve helm: system dep per STACK.md (not project-local). No
	// bin/-fallback — helm is on $PATH or we soft-skip per WR-09.
	if _, lookErr := exec.LookPath("helm"); lookErr != nil {
		GinkgoWriter.Println("helm not in PATH; falling back to CRDs-only mode")
		return
	}

	// Resolve chart dir to an absolute path so helm respects it regardless
	// of the test process's working directory.
	chartDirRel := filepath.Join("..", "..", "..", "charts", "tide")
	chartDir, absErr := filepath.Abs(chartDirRel)
	if absErr != nil {
		GinkgoWriter.Printf("failed to resolve chart dir: %v\n", absErr)
		if os.Getenv("TIDE_REQUIRE_CONTROLLER") == "1" {
			Fail(fmt.Sprintf("failed to resolve chart dir (TIDE_REQUIRE_CONTROLLER=1): %v", absErr))
		}
		return
	}
	if _, statErr := os.Stat(chartDir); os.IsNotExist(statErr) {
		GinkgoWriter.Println("charts/tide not present; falling back to CRDs-only mode")
		return
	}

	// helm install --set values map the kind-loaded image tags + IfNotPresent
	// policy onto the chart's default values (which point at the production
	// ghcr.io/jsquirrelz/* registry with the chart-default tag). The manager
	// image repository overrides to plain "controller" because Makefile:133
	// builds + tags it as `controller:test`. images.stubSubagent + credProxy
	// keep their chart-default repositories — only .tag and .pullPolicy
	// need override.
	// --replace makes KEEP_KIND_CLUSTER=true reruns against an existing
	// `tide` release in `tide-system` idempotent — without it, helm errors
	// with "cannot re-use a name that is still in use" on second install.
	// Source: .planning/phases/02.1-.../02.1-02-SUMMARY.md §"Issues
	// Encountered" — Phase 02.2 collateral fix.
	helmCmd := exec.CommandContext(ctx, "helm",
		"install", "tide", chartDir,
		"--create-namespace", "-n", kindControllerNamespace,
		"--kubeconfig", kubeconfigPath,
		"--set", "controllerManager.manager.image.repository=controller",
		"--set", "controllerManager.manager.image.tag=test",
		"--set", "controllerManager.manager.image.pullPolicy=IfNotPresent",
		"--set", "images.stubSubagent.tag=test",
		"--set", "images.stubSubagent.pullPolicy=IfNotPresent",
		"--set", "images.credProxy.tag=test",
		"--set", "images.credProxy.pullPolicy=IfNotPresent",
		// Override the chart's default accessModes [ReadWriteMany] to [ReadWriteOnce]
		// because kind's default rancher.io/local-path provisioner only supports
		// RWO/RWOPod. Single-node kind cluster doesn't need RWX semantics for these
		// tests. See .planning/phases/02.2-.../02.2-01-VERIFICATION.md §"Fix
		// landscape" Option A (chart-side override + test-side --set).
		"--set", "workspaces.pvc.accessModes={ReadWriteOnce}",
		"--wait", "--replace", "--timeout", "5m",
	)
	start := time.Now()
	out, err := helmCmd.CombinedOutput()
	elapsed := time.Since(start)
	if err != nil {
		GinkgoWriter.Printf("helm install failed after %s: %v\n%s\n", elapsed, err, out)
		if os.Getenv("TIDE_REQUIRE_CONTROLLER") == "1" {
			Fail(fmt.Sprintf("helm install failed (TIDE_REQUIRE_CONTROLLER=1): %v", err))
		}
		return
	}
	GinkgoWriter.Printf("helm install completed in %s\n%s\n", elapsed, out)
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

// ensureSubagentSA creates the tide-subagent ServiceAccount in the given
// namespace. Every Task Job's PodSpec references this SA by name
// (internal/dispatch/podjob/jobspec.go:43 — ServiceAccountSubagent =
// "tide-subagent"); without it Pod creation fails with
// `serviceaccount "tide-subagent" not found`.
//
// The chart templates this SA only in .Release.Namespace (= tide-system,
// see charts/tide/templates/serviceaccount-subagent.yaml), but tests apply
// Tasks into per-test namespaces (tide-int-test, failure-test, caps-test,
// output-test, credproxy-test). This helper re-creates the SA in any
// namespace before Tasks are applied — wired into createNamespace() in
// failure_test.go so every test-fixture namespace gets it automatically.
//
// D-A4: no Role, no RoleBinding — subagent pods have zero K8s API verbs.
// The function intentionally does NOT accept any RBAC parameters
// (T-02.1-02-03 mitigation). Re-applying the SA is idempotent — kubectl
// apply tolerates "already exists" and the error from applyYAML is
// intentionally discarded (best-effort).
//
// Source of truth for the SA shape:
// charts/tide/templates/serviceaccount-subagent.yaml — Phase 02.1 D-02.
func ensureSubagentSA(ns string) {
	saYAML := fmt.Sprintf(`apiVersion: v1
kind: ServiceAccount
metadata:
  name: tide-subagent
  namespace: %s
automountServiceAccountToken: true
`, ns)
	_ = applyYAML(saYAML)
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
