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
	"slices"
	"strings"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
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
	//   (d) per-spec budget for the full Layer B suite (~700s observed in
	//       Phase 04.1 Plan 12 iter-3 across 7-of-13 ran specs, projecting
	//       to ~900s for all 13)
	//   (e) variance margin (60s — slow CI runners, image-pull stalls)
	//
	// 15m = 50s + 300s + 5s + 850s + 60s + slack. Going below ~6m25s
	// shadow-kills the helm subprocess before its own --timeout fires (see
	// .planning/phases/02.1-.../02.1-04-VERIFICATION.md §"Root-cause
	// analysis" for the failure mode this constant closes — Phase 02.2).
	//
	// Iteration history:
	//   - Phase 02.2-12 baseline: 7m (assumed helm install ~50s + specs ~300s)
	//   - Phase 04.1-12 iter-2 bump → 12m: iter-1 observed helm --wait
	//     failing at 5m03s + 469s spec wall > 420s, cancelling suite ctx
	//   - Phase 04.1-12 iter-4 bump → 15m: iter-3 observed 817s inner wall
	//     (helm install ~365s + specs ~452s + skips ~0s) > 720s, causing
	//     6 of 13 specs to skip via skipIfCRDsOnlyMode when k8sClient.List
	//     returned ctx.DeadlineExceeded. Source:
	//     .planning/phases/04.1-pre-v1-audit-fixes-cross-phase-uat-closeout/04.1-12-SUMMARY.md
	//     §"Cascade 8: timing budget regression".
	//   - Phase 04.1-12 iter-5 bump → 18m: iter-4 observed 908s inner wall
	//     barely over 900s (15m), causing chaos_resume + caps_test to
	//     SKIP at the tail. push_lease specs (4 × ~103s = 415s) are the
	//     dominant budget consumer (now run unconditionally per the
	//     quick-task Cascade 9 recipe — see 04.1-12-SUMMARY.md Outstanding
	//     Follow-up #2). 18m gives headroom for unexpected first-run
	//     delays without re-tripping the cascade.
	kindTestTimeout = 18 * time.Minute

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
	if testing.Short() {
		t.Skip("skipping kind integration tests in short mode")
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

	// Phase 04.1 Plan 12 iter-3 (Cascade 4-b): mirror the tide-signing-key
	// Secret into the shared fixture namespace AFTER waitForControllerReady
	// rather than inside applyController(). applyController() returns early
	// on helm --wait failure (e.g. when chart resources take > 5m to become
	// Ready, even though the Deployment itself comes up); the prior in-helm
	// placement of ensureSigningKeySecret(kindNamespace) was skipped in that
	// path, causing all subsequent kind-namespace Tasks to fail with
	// `CreateContainerConfigError: secret "tide-signing-key" not found` on
	// the credproxy init container. Doing the mirror here, gated on
	// controllerSigningKeyData()'s availability (which the helper handles
	// internally with a GinkgoWriter warning + skip), survives the
	// helm-failed-but-Deployment-up case. Source:
	// .planning/phases/04.1-.../04.1-12-SUMMARY.md §"Cascade 4-b".
	By("Mirroring tide-signing-key into kindNamespace (helm-failure-resilient)")
	ensureSigningKeySecret(kindNamespace)

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
		if slices.Contains(strings.Split(strings.TrimSpace(string(out)), "\n"), kindClusterName) {
			GinkgoWriter.Println("Reusing existing kind cluster: " + kindClusterName)
			return
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
// tide-subagent ServiceAccount, and the --credproxy-image /
// --default-file-touch-mode Deployment args. Without them the manager
// Pod CrashLoopBackOffs on startup with
// `TIDE_SIGNING_KEY env var is required (HARN-03)` before any spec runs.
// Phase 13 D-01/D-02: the --subagent-image flag has been dropped from the
// chart; the stub is now an explicit opt-in via subagent.defaults.image.
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

	// Ensure namespace-local resources exist in the test-fixture namespace
	// (wave_test.go fixtures apply Tasks here). Every Task Job references
	// tide-subagent and tide-projects by name in its own namespace, while the
	// chart only templates them in tide-system / .Release.Namespace.
	ensureSubagentSA(kindNamespace)
	ensureProjectsPVC(kindNamespace)

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
	// Phase 13 D-01/D-02: subagent.defaults.image is set explicitly to the
	// kind-loaded stub image so the harness uses the stub. Without this set,
	// the chart default (real claude subagent) would be used — correct for
	// production but unavailable in the kind test cluster.
	// Use upgrade --install so KEEP_KIND_CLUSTER=true reruns update the live
	// release instead of continuing with a stale controller image. helm install
	// --replace only works for deleted releases; it fails for a currently
	// deployed release with "cannot re-use a name that is still in use".
	rolloutNonce := fmt.Sprintf("%d", time.Now().UnixNano())
	helmCmd := exec.CommandContext(ctx, "helm", helmControllerArgs(chartDir, rolloutNonce)...)
	start := time.Now()
	out, err := helmCmd.CombinedOutput()
	elapsed := time.Since(start)
	if err != nil {
		GinkgoWriter.Printf("helm upgrade --install failed after %s: %v\n%s\n", elapsed, err, out)
		// Capture pod-level diagnostics BEFORE Fail() triggers AfterSuite, which
		// deletes the kind cluster (destroying any chance of post-hoc `kind
		// export logs`). This disambiguates a genuine `--wait` timeout (manager
		// pod healthy but slow to schedule on a cold 2-core runner) from a real
		// pod failure (CrashLoop / webhook-cert wait / config). Emitted to
		// stdout so it survives in the captured `go test -v` output even after
		// the cluster is gone. Source: debug session nightly-int-flake-timeout.
		dumpControllerDiagnostics("helm upgrade --install failed")
		if os.Getenv("TIDE_REQUIRE_CONTROLLER") == "1" {
			Fail(fmt.Sprintf("helm upgrade --install failed (TIDE_REQUIRE_CONTROLLER=1): %v", err))
		}
		return
	}
	GinkgoWriter.Printf("helm upgrade --install completed in %s\n%s\n", elapsed, out)

	// NOTE: tide-signing-key mirroring into kindNamespace was MOVED to a
	// separate BeforeSuite step after waitForControllerReady() — Phase 04.1
	// Plan 12 iter-3 Cascade 4-b. The chart creates the source Secret in
	// .Release.Namespace (= tide-system) even when helm --wait subsequently
	// times out; keeping the mirror inside this function meant the
	// helm-failure-early-return path skipped it and broke every test that
	// runs in kindNamespace.
}

func helmControllerArgs(chartDir string, rolloutNonce string) []string {
	return []string{
		"upgrade", "--install", "tide", chartDir,
		"--create-namespace", "-n", kindControllerNamespace,
		"--kubeconfig", kubeconfigPath,
		"--set", "controllerManager.manager.image.repository=controller",
		"--set", "controllerManager.manager.image.tag=test",
		"--set", "controllerManager.manager.image.pullPolicy=IfNotPresent",
		"--set", "images.stubSubagent.tag=test",
		"--set", "images.stubSubagent.pullPolicy=IfNotPresent",
		// Phase 13 D-01/D-02: explicit stub opt-in. The --subagent-image flag
		// has been dropped from the chart; test installs must declare the stub
		// via subagent.defaults.image so CLAUDE_SUBAGENT_IMAGE points at the
		// kind-loaded stub image (ghcr.io/jsquirrelz/tide-stub-subagent:test).
		"--set", "subagent.defaults.image=ghcr.io/jsquirrelz/tide-stub-subagent:test",
		"--set", "images.credProxy.tag=test",
		"--set", "images.credProxy.pullPolicy=IfNotPresent",
		"--set", "images.tideReporter.tag=test",
		"--set", "images.tideReporter.pullPolicy=IfNotPresent",
		"--set-string", "controllerManager.manager.podAnnotations.tideproject\\.k8s/restart-nonce=" + rolloutNonce,
		// Override the chart's default accessModes [ReadWriteMany] to [ReadWriteOnce]
		// because kind's default rancher.io/local-path provisioner only supports
		// RWO/RWOPod. Single-node kind cluster doesn't need RWX semantics for these
		// tests. See .planning/phases/02.2-.../02.2-01-VERIFICATION.md §"Fix
		// landscape" Option A (chart-side override + test-side --set).
		"--set", "workspaces.pvc.accessModes={ReadWriteOnce}",
		// Disable the dashboard for the Layer B controller/CRD reconciliation
		// suite. `make test-int-kind-prep` builds + kind-loads only the four
		// controller-side images (controller, stub-subagent, credproxy, push) —
		// it does NOT build/load the dashboard image. The chart defaults
		// dashboard.enabled=true (charts/tide/values.yaml), so with the dashboard
		// enabled its pod can never pull ghcr.io/jsquirrelz/tide-dashboard on a
		// fresh CI kind node -> ImagePullBackOff, and helm `--wait` blocks on it
		// until the 5m deadline even though the manager Deployment is 1/1 Ready.
		// These 14 specs do not exercise the dashboard UI; the dashboard is
		// covered by the separate `make test-e2e-kind` target (which builds +
		// loads + installs it). Disabling it here removes the unpullable pod from
		// the release so `--wait` completes once the manager is Ready.
		// Source: debug session nightly-int-flake-timeout.
		"--set", "dashboard.enabled=false",
		"--wait", "--timeout", "5m",
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

// dumpControllerDiagnostics emits manager-pod-level state (deployment status,
// pods, recent events, container logs incl. previous-restart logs) for the
// tide-system namespace to stdout. It is called on the helm-install failure
// path BEFORE Fail() so the evidence survives AfterSuite's cluster teardown —
// the nightly workflow's post-failure `kind export logs` step runs only after
// the suite process exits, by which point the cluster is already deleted.
//
// Best-effort: every kubectl invocation is tolerant of errors (the cluster may
// be partially up). Plain exec.Command (no ctx) is intentional — when this is
// reached the suite ctx may already be on its way to cancellation.
func dumpControllerDiagnostics(reason string) {
	ns := kindControllerNamespace
	hdr := "=== CONTROLLER DIAGNOSTICS (" + reason + ") ns=" + ns + " ==="
	fmt.Fprintln(os.Stdout, hdr)
	GinkgoWriter.Println(hdr)

	run := func(label string, args ...string) {
		full := append([]string{"--kubeconfig", kubeconfigPath}, args...)
		out, err := exec.Command("kubectl", full...).CombinedOutput()
		block := "--- " + label + " ---\n" + string(out)
		if err != nil {
			block += "(kubectl error: " + err.Error() + ")\n"
		}
		fmt.Fprintln(os.Stdout, block)
		GinkgoWriter.Println(block)
	}

	run("deployment "+kindControllerDeployment, "get", "deployment",
		kindControllerDeployment, "-n", ns, "-o", "wide")
	run("describe deployment "+kindControllerDeployment, "describe", "deployment",
		kindControllerDeployment, "-n", ns)
	run("pods", "get", "pods", "-n", ns, "-o", "wide")
	run("describe pods", "describe", "pods", "-n", ns)
	run("events", "get", "events", "-n", ns,
		"--sort-by=.lastTimestamp")
	// Current + previous-restart logs for the manager (CrashLoop leaves the
	// failure only in --previous). --all-containers covers any sidecar.
	run("logs (current)", "logs", "-n", ns,
		"-l", "control-plane=controller-manager", "--all-containers=true",
		"--tail=200", "--prefix=true")
	run("logs (previous)", "logs", "-n", ns,
		"-l", "control-plane=controller-manager", "--all-containers=true",
		"--previous=true", "--tail=200", "--prefix=true")

	footer := "=== END CONTROLLER DIAGNOSTICS ==="
	fmt.Fprintln(os.Stdout, footer)
	GinkgoWriter.Println(footer)
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

// ensureProjectsPVC creates the tide-projects PersistentVolumeClaim in the
// given namespace. Every Task Job mounts this PVC by name in its own namespace
// (internal/controller/task_controller.go passes "tide-projects" to
// BuildJobSpec); without it, Pods stay Pending with
// `persistentvolumeclaim "tide-projects" not found`.
//
// The chart templates this PVC only in .Release.Namespace (= tide-system, see
// charts/tide/templates/projects-pvc.yaml), but Layer B applies Tasks into
// per-test namespaces. For kind, a namespace-local 1Gi RWO claim matches the
// helm test override for the single-node local-path provisioner.
func ensureProjectsPVC(ns string) {
	_ = applyYAML(projectsPVCYAML(ns))
}

func projectsPVCYAML(ns string) string {
	return fmt.Sprintf(`apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: tide-projects
  namespace: %s
spec:
  accessModes:
    - ReadWriteOnce
  resources:
    requests:
      storage: 1Gi
`, ns)
}

// pvcPrewarmPod schedules a one-shot pause Pod that mounts the namespace-local
// tide-projects PVC, waits for the PVC to reach ClaimBound, then deletes the
// Pod. This is a test-side compensator for kind's default
// rancher.io/local-path StorageClass, which has
// volumeBindingMode: WaitForFirstConsumer — a PVC stays Pending until the
// first Pod consuming it is scheduled.
//
// Pod-bearing Layer-B fixtures (chaos-resume, credproxy, output, up_stack)
// trigger binding naturally because the ProjectReconciler dispatches a
// tide-init Job within their first reconcile window. Pod-less fixtures
// (push-lease, which mocks Project.Status.Phase=Complete via direct status
// patch and never dispatches a Task) deadlock: the controller's PVC-Bound
// gate at internal/controller/project_controller.go:246 requeues forever.
// Pre-warming the PVC here unblocks both classes with a no-op for the first
// and a real bind for the second.
//
// Idempotency contract: if the PVC is already ClaimBound when this helper is
// invoked, it returns immediately without creating the Pod (cheap Get only).
// This keeps the helper a near-zero-cost no-op for the natural-binding
// fixtures that already pass.
//
// See .planning/debug/push-lease-pvc-pending.md for the full cascade-11
// root-cause analysis and the OPTION A vs OPTION B decision recap.
func pvcPrewarmPod(ns string) {
	// Step 1 — idempotency check: if PVC already Bound, skip the prewarm.
	existing := &corev1.PersistentVolumeClaim{}
	if err := k8sClient.Get(ctx, client.ObjectKey{
		Name:      "tide-projects",
		Namespace: ns,
	}, existing); err != nil {
		GinkgoWriter.Printf("pvcPrewarmPod: tide-projects PVC in namespace %q not yet visible (%v); proceeding to schedule prewarm Pod\n", ns, err)
	} else if existing.Status.Phase == corev1.ClaimBound {
		GinkgoWriter.Printf("pvcPrewarmPod: tide-projects PVC in namespace %q already Bound; skipping prewarm\n", ns)
		return
	}

	// Step 2 — create a pause Pod that mounts the PVC. The mere presence of
	// spec.volumes referencing the PVC is sufficient to trigger the
	// local-path provisioner; the container does not need to mount the
	// volume to cause the bind side-effect.
	podYAML := fmt.Sprintf(`apiVersion: v1
kind: Pod
metadata:
  name: tide-projects-prewarm
  namespace: %s
spec:
  restartPolicy: Never
  containers:
    - name: pause
      image: busybox:1.36
      command: ["sleep", "60"]
  volumes:
    - name: workspace
      persistentVolumeClaim:
        claimName: tide-projects
`, ns)
	_ = applyYAML(podYAML)

	// Step 3 — wait for the PVC to reach ClaimBound. 60s budget matches the
	// Risks: low estimate in push-lease-pvc-pending.md:181; 1s poll interval
	// matches existing Eventually precedents in this package.
	Eventually(func() corev1.PersistentVolumeClaimPhase {
		pvc := &corev1.PersistentVolumeClaim{}
		if err := k8sClient.Get(ctx, client.ObjectKey{
			Name:      "tide-projects",
			Namespace: ns,
		}, pvc); err != nil {
			return ""
		}
		return pvc.Status.Phase
	}, 60*time.Second, time.Second).Should(Equal(corev1.ClaimBound),
		"tide-projects PVC in namespace %q must reach Bound after prewarm Pod scheduled", ns)

	// Step 4 — best-effort cleanup. The pause Pod's sleep 60 ensures it
	// self-exits even if our explicit Delete is skipped or fails; AfterEach
	// namespace deletion GCs any orphans. Errors are logged but do NOT fail
	// the helper.
	prewarm := &corev1.Pod{}
	prewarm.Name = "tide-projects-prewarm"
	prewarm.Namespace = ns
	if err := k8sClient.Delete(ctx, prewarm); err != nil {
		GinkgoWriter.Printf("pvcPrewarmPod: best-effort delete of tide-projects-prewarm Pod in namespace %q returned %v (non-fatal; AfterEach will GC)\n", ns, err)
	}
}

// ensureSigningKeySecret mirrors the helm-created tide-signing-key Secret into
// a Task namespace. Every credproxy sidecar references this Secret by name via
// envFrom in its own Pod namespace; without the copy, Pods fail to start with
// `secret "tide-signing-key" not found`.
//
// In CRDs-only mode there is no helm-created source secret, so this helper
// degrades to a warning and leaves tests that need the controller to fail or
// skip according to their existing requirements.
func ensureSigningKeySecret(ns string) {
	keyData, err := controllerSigningKeyData()
	if err != nil {
		GinkgoWriter.Printf("Warning: could not mirror tide-signing-key into namespace %q: %v\n", ns, err)
		return
	}
	_ = applyYAML(signingKeySecretYAML(ns, keyData))
}

func controllerSigningKeyData() (string, error) {
	cmd := exec.CommandContext(ctx, "kubectl", "--kubeconfig", kubeconfigPath,
		"get", "secret", "tide-signing-key",
		"-n", kindControllerNamespace,
		"-o", "jsonpath={.data.TIDE_SIGNING_KEY}")
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	keyData := strings.TrimSpace(string(out))
	if keyData == "" {
		return "", fmt.Errorf("secret %s/tide-signing-key has empty data.TIDE_SIGNING_KEY", kindControllerNamespace)
	}
	return keyData, nil
}

func signingKeySecretYAML(ns, keyData string) string {
	return fmt.Sprintf(`apiVersion: v1
kind: Secret
metadata:
  name: tide-signing-key
  namespace: %s
type: Opaque
data:
  TIDE_SIGNING_KEY: %s
`, ns, keyData)
}

// applyHierarchy creates the full Namespace+Secret+Project+Milestone+Phase+Plan+Task
// hierarchy required by the task controller's resolveProject dispatch path.
//
// Purpose: the cascade-5 debug investigation (.planning/debug/credproxy-dispatch-guard.md
// §Reasoning Checkpoint) established that task_controller.go:217 (Step 3 of
// reconcileDispatch) calls resolveProject(ctx, task), which returns "no project found
// in namespace <ns>" when only a Plan+Task pair exists — exactly what the original
// credproxy_test.go inline fixture provided. This helper creates the FULL hierarchy
// mirroring test/integration/kind/testdata/three-task-wave.yaml field-for-field, closing
// the cascade-5 harness-bug (T-02.2-17 mitigation).
//
// The helper parameterizes: ns (Namespace + resource namespace), planName, taskName.
// It does NOT parameterize:
//   - Project name: derived deterministically as ns+"-project" (e.g., "credproxy-test-project")
//   - Milestone name: derived as ns+"-milestone"
//   - Phase name: derived as ns+"-phase"
//   - Budget: fixed at 100000 absoluteCapCents ($1000 — no budget cap during tests)
//   - Secret name: fixed at "tide-provider-secret" (matching three-task-wave.yaml)
//
// Signature is exactly 4-arg per T-02.2-20 acceptance (no struct-options, no builder
// pattern in v1). Future tests requiring richer parameterization extend the signature
// in a follow-up plan rather than copy-paste-mutating the helper.
//
// The helper calls createNamespace(ns) (which also creates namespace-local
// tide-subagent and tide-projects resources) as its first step; callers must
// NOT call createNamespace themselves before calling applyHierarchy to avoid
// double-create noise.
//
// API group: tideproject.k8s/v1alpha1 (per CLAUDE.md TIDE domain rule — never tide.io).
func applyHierarchy(ctx context.Context, ns, planName, taskName string) error {
	// Step 1: Create Namespace + namespace-local Task Job dependencies.
	createNamespace(ns)

	// Step 2: Derive deterministic names for the hierarchy parents.
	projectName := ns + "-project"
	milestoneName := ns + "-milestone"
	phaseName := ns + "-phase"

	// Step 3: Construct the full hierarchy as a single multi-doc YAML, mirroring
	// testdata/three-task-wave.yaml field-for-field (T-02.2-17 mitigation).
	// Secret + Project + Milestone + Phase + Plan + Task (Namespace already created above).
	hierarchyYAML := fmt.Sprintf(`apiVersion: v1
kind: Secret
metadata:
  name: tide-provider-secret
  namespace: %s
type: Opaque
data:
  ANTHROPIC_API_KEY: dGVzdC1hcGkta2V5LXN0dWItc3ViYWdlbnQtZG9lcy1ub3QtdXNlLWl0
---
apiVersion: tideproject.k8s/v1alpha1
kind: Project
metadata:
  name: %s
  namespace: %s
spec:
  targetRepo: "https://github.com/example/%s.git"
  providerSecretRef: "tide-provider-secret"
  budget:
    absoluteCapCents: 100000
---
apiVersion: tideproject.k8s/v1alpha1
kind: Milestone
metadata:
  name: %s
  namespace: %s
spec:
  projectRef: %s
---
apiVersion: tideproject.k8s/v1alpha1
kind: Phase
metadata:
  name: %s
  namespace: %s
spec:
  milestoneRef: %s
---
apiVersion: tideproject.k8s/v1alpha1
kind: Plan
metadata:
  name: %s
  namespace: %s
  labels:
    tideproject.k8s/project: %s
spec:
  phaseRef: %s
---
apiVersion: tideproject.k8s/v1alpha1
kind: Task
metadata:
  name: %s
  namespace: %s
  labels:
    tideproject.k8s/project: %s
    tideproject.k8s/wave-index: "0"
spec:
  planRef: %s
  filesTouched:
    - %s.go
  declaredOutputPaths:
    - %s.go
  promptPath: "children/task-01.json"
  dev:
    testMode: success
`,
		// Secret namespace
		ns,
		// Project: name, namespace, targetRepo ns
		projectName, ns, ns,
		// Milestone: name, namespace, projectRef
		milestoneName, ns, projectName,
		// Phase: name, namespace, milestoneRef
		phaseName, ns, milestoneName,
		// Plan: name, namespace, label project, phaseRef
		planName, ns, projectName, phaseName,
		// Task: name, namespace, label project, planRef, filesTouched, declaredOutputPaths
		taskName, ns, projectName, planName, taskName, taskName,
	)

	// Step 4: Apply the hierarchy via the existing applyYAML primitive.
	return applyYAML(hierarchyYAML)
}

// createProjectHierarchy creates the Namespace+Secret+Project+Milestone+Phase
// hierarchy required by the task controller's resolveProject dispatch path,
// WITHOUT a Plan or Task. The calling test supplies its own Plan and Task
// via a follow-up applyYAML call.
//
// Purpose: Plan 02.2-07's applyHierarchy authored the full
// Namespace+Secret+Project+Milestone+Phase+Plan+Task hierarchy and refactored
// credproxy_test.go to call it. Three other Layer B fixture files
// (caps_test.go, output_test.go, failure_test.go) have their OWN inline
// Plan+Task YAML and only need the parent-Project hierarchy — they cannot
// reuse applyHierarchy because applyHierarchy's Plan+Task would collide
// with their own. This helper provides the parent-only variant
// (T-02.2-24 mitigation: byte-identical Secret+Project+Milestone+Phase
// fields to applyHierarchy; only the Plan+Task documents are omitted).
//
// Signature: (ctx context.Context, ns string) error — mirrors applyHierarchy's
// signature shape but with only the namespace parameter (no planName/taskName
// because the caller supplies those).
//
// Like applyHierarchy, this helper calls createNamespace(ns) (which also
// creates namespace-local tide-subagent and tide-projects resources) as its
// first step; callers must NOT call createNamespace themselves before calling
// createProjectHierarchy to avoid double-create noise.
//
// API group: tideproject.k8s/v1alpha1 (per CLAUDE.md TIDE domain rule —
// never tide.io).
func createProjectHierarchy(ctx context.Context, ns string) error {
	// Step 1: Create Namespace + namespace-local Task Job dependencies.
	createNamespace(ns)

	// Step 2: Derive deterministic names for the hierarchy parents
	// (same shape as applyHierarchy).
	projectName := ns + "-project"
	milestoneName := ns + "-milestone"
	phaseName := ns + "-phase"

	// Step 3: Construct the parent hierarchy as a single multi-doc YAML,
	// mirroring applyHierarchy's Secret+Project+Milestone+Phase sections
	// field-for-field (T-02.2-24 byte-identical mitigation). Plan and Task
	// are intentionally omitted — the caller supplies those via its own
	// applyYAML call.
	hierarchyYAML := fmt.Sprintf(`apiVersion: v1
kind: Secret
metadata:
  name: tide-provider-secret
  namespace: %s
type: Opaque
data:
  ANTHROPIC_API_KEY: dGVzdC1hcGkta2V5LXN0dWItc3ViYWdlbnQtZG9lcy1ub3QtdXNlLWl0
---
apiVersion: tideproject.k8s/v1alpha1
kind: Project
metadata:
  name: %s
  namespace: %s
spec:
  targetRepo: "https://github.com/example/%s.git"
  providerSecretRef: "tide-provider-secret"
  budget:
    absoluteCapCents: 100000
---
apiVersion: tideproject.k8s/v1alpha1
kind: Milestone
metadata:
  name: %s
  namespace: %s
spec:
  projectRef: %s
---
apiVersion: tideproject.k8s/v1alpha1
kind: Phase
metadata:
  name: %s
  namespace: %s
spec:
  milestoneRef: %s
`,
		// Secret namespace
		ns,
		// Project: name, namespace, targetRepo ns
		projectName, ns, ns,
		// Milestone: name, namespace, projectRef
		milestoneName, ns, projectName,
		// Phase: name, namespace, milestoneRef
		phaseName, ns, milestoneName,
	)

	// Step 4: Apply the hierarchy via the existing applyYAML primitive.
	return applyYAML(hierarchyYAML)
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
	if os.Getenv("KEEP_KIND_NAMESPACES") == "true" {
		GinkgoWriter.Printf("KEEP_KIND_NAMESPACES=true; keeping namespace %s for debug inspection\n", ns)
		return
	}
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
