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

// verify_posture_sticky_test.go — Layer B kind integration spec for Plan
// 53-09 Task 1.
//
// Coverage: CFG-02 — the live half of ROADMAP SC2's "proven by an
// upgrade-path test": charts/tide/templates/verify-posture-configmap.yaml's
// lookup+resource-policy:keep marker makes a fresh install's verify-tier
// posture STICKY across upgrades, while an upgrade with no marker lineage
// (simulating a pre-existing pre-v1.0.9 install) stays OFF. `helm template`
// cannot exercise this: `lookup` is always empty without a live cluster
// (53-RESEARCH.md Finding 1), so this must be a real install/upgrade cycle.
//
// Isolation design (53-09-PLAN.md, RESEARCH Finding 7/9, Pitfall 5): this
// spec drives its OWN "tide-posture" release in its OWN "tide-posture-test"
// namespace, Serial-decorated, and never touches the shared suite's single
// "tide" release. Assertions are on RENDERED/APPLIED resources (Deployment
// args + the tide-verify-posture marker ConfigMap), never on pod readiness
// — the throwaway release's manager pod may crash-loop (its own webhook
// TLS cert has not necessarily been issued, and there is no reason to wait
// for it) and that is irrelevant to the posture contract.
//
// cluster-scoped-name collision check (read_first item 4, done at
// authoring time, not deferred to the executor): charts/tide/templates/
// tide.fullname (_helpers.tpl) resolves to the release name whenever the
// chart name "tide" is a substring of it — "tide-posture" qualifies — so
// every ClusterRole/ClusterRoleBinding this release creates is uniquely
// named "tide-posture-*", distinct from the shared "tide-*" set. No
// collision; the primary isolated-release approach applies as designed,
// the marker-deletion fallback against the shared release is not needed.
//
// A DIFFERENT, NOT-A-NAME collision was found and closed here: the chart's
// ValidatingWebhookConfiguration (validating-webhook-configuration.yaml) is
// unconditional (no values.yaml gate to disable it) and cluster-scoped with
// failurePolicy: Fail and no namespaceSelector — its Plan/Project/Wave
// CREATE/UPDATE rules match the ENTIRE cluster, not just this release's
// namespace. If left in place while this throwaway release's manager pod
// has no Ready endpoint, ANY Plan/Project/Wave mutation anywhere in the
// cluster (including background reconciliation the shared suite's "tide"
// release may still be driving from earlier specs) would be rejected with
// a "no endpoints available" admission error — a real DoS the plan's
// declared namespace/release isolation does not by itself prevent. This
// spec closes the gap itself: immediately after every helm install/upgrade
// it deletes ONLY this release's own uniquely-named
// tide-posture-validating-webhook-configuration object (never the shared
// release's), before doing anything else. Helm reapplies it on every
// subsequent install/upgrade (it is still part of the release manifest),
// so the delete is repeated after each helm invocation to keep the
// exposure window to a few hundred milliseconds instead of the whole test.

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	// postureReleaseName/postureNamespace are this spec's own throwaway
	// release + namespace — never the shared suite's "tide" release in
	// kindControllerNamespace ("tide-system").
	postureReleaseName = "tide-posture"
	postureNamespace   = "tide-posture-test"

	// postureDeploymentName mirrors tide.fullname (_helpers.tpl): the chart
	// name "tide" is a substring of the release name "tide-posture", so
	// tide.fullname resolves to the release name itself.
	postureDeploymentName = postureReleaseName + "-controller-manager"

	// postureWebhookConfigName is this release's own uniquely-named (never
	// the shared release's) ValidatingWebhookConfiguration — deleted after
	// every helm operation per the header comment's DoS-gap closure.
	postureWebhookConfigName = postureReleaseName + "-validating-webhook-configuration"

	// postureMarkerName/postureMarkerKey mirror
	// charts/tide/templates/verify-posture-configmap.yaml verbatim.
	postureMarkerName = "tide-verify-posture"
	postureMarkerKey  = "posture"

	// postureVerifyArgPrefix mirrors deployment.yaml's ARGS53 block: the
	// manager arg the tier's ON/OFF posture gates.
	postureVerifyArgPrefix = "--verify-levels-json="

	posturePollTimeout  = 30 * time.Second
	posturePollInterval = 2 * time.Second
)

var _ = Describe("Verify-tier sticky posture across upgrades (CFG-02)", Label("kind"), Serial, func() {
	BeforeEach(func() {
		skipIfCRDsOnlyMode()
		// Best-effort pre-clean: a prior interrupted run (e.g. a killed
		// KEEP_KIND_CLUSTER=true dev iteration) may have left the release
		// installed, which would make a plain `helm install` below fail with
		// "cannot re-use a name that is still in use". Ignore all errors —
		// this is purely defensive.
		_, _ = runHelm("uninstall", postureReleaseName, "-n", postureNamespace, "--kubeconfig", kubeconfigPath)
		deletePostureWebhookConfig()
	})

	AfterEach(func() {
		_, _ = runHelm("uninstall", postureReleaseName, "-n", postureNamespace, "--kubeconfig", kubeconfigPath)
		deletePostureWebhookConfig()
		deleteNamespace(postureNamespace)
		if CurrentSpecReport().Failed() {
			exportKindLogs()
		}
	})

	It("stays ON across a same-release upgrade and turns OFF for an upgrade with no marker lineage", func() {
		chartDir := postureChartDir()

		By("Fresh-installing tide-posture: expect the sticky marker to be minted and --verify-levels-json to render")
		installArgs := append([]string{
			"install", postureReleaseName, chartDir,
			"--create-namespace", "-n", postureNamespace,
			"--kubeconfig", kubeconfigPath,
		}, postureImageSetArgs()...)
		out, err := runHelm(installArgs...)
		Expect(err).NotTo(HaveOccurred(), "helm install tide-posture: %s", out)
		deletePostureWebhookConfig()

		assertPostureMarkerEnabled("fresh install must mint the tide-verify-posture marker as enabled")
		assertPostureVerifyArg(true, "fresh install must render --verify-levels-json (IsInstall=true)")

		By("Upgrading the SAME release with no posture override: sticky lineage must keep the tier ON")
		upgradeArgs := append([]string{
			"upgrade", postureReleaseName, chartDir,
			"-n", postureNamespace,
			"--kubeconfig", kubeconfigPath,
		}, postureImageSetArgs()...)
		out, err = runHelm(upgradeArgs...)
		Expect(err).NotTo(HaveOccurred(), "helm upgrade tide-posture (marker present): %s", out)
		deletePostureWebhookConfig()

		assertPostureMarkerEnabled("an upgrade on a release with marker lineage must not disturb the marker")
		assertPostureVerifyArg(true, "an upgrade on a release with marker lineage must keep --verify-levels-json rendered (sticky)")

		By("Deleting the marker to simulate a release that predates it, then upgrading again")
		Expect(deletePostureMarkerConfigMap()).To(Succeed())
		out, err = runHelm(upgradeArgs...)
		Expect(err).NotTo(HaveOccurred(), "helm upgrade tide-posture (marker absent): %s", out)
		deletePostureWebhookConfig()

		assertPostureVerifyArg(false, "an upgrade with no marker lineage must render the tier OFF")
		assertPostureMarkerAbsent("an upgrade (IsInstall=false) must never mint new marker lineage")
	})
})

// postureChartDir resolves charts/tide the same way applyController() does,
// so this spec respects the test process's working directory independent of
// go test's package-relative invocation.
func postureChartDir() string {
	chartDirRel := filepath.Join("..", "..", "..", "charts", "tide")
	chartDir, err := filepath.Abs(chartDirRel)
	Expect(err).NotTo(HaveOccurred(), "resolve charts/tide dir")
	return chartDir
}

// postureImageSetArgs pins the same kind-loaded image tags
// helmControllerArgs uses for the shared "tide" release, so the throwaway
// "tide-posture" release resolves to the identical schedulable images
// (though this spec never waits on pod readiness). Duplicated rather than
// factored out of helmControllerArgs because this plan's declared file
// scope is this test file only (mirrors verifier_concurrency_test.go's
// precedent of pinning a duplicated literal with a comment rather than
// reaching into suite_test.go).
func postureImageSetArgs() []string {
	return []string{
		"--set", "controllerManager.manager.image.repository=controller",
		"--set", "controllerManager.manager.image.tag=test",
		"--set", "controllerManager.manager.image.pullPolicy=IfNotPresent",
		"--set", "images.stubSubagent.tag=test",
		"--set", "images.stubSubagent.pullPolicy=IfNotPresent",
		"--set", "subagent.defaults.image=ghcr.io/jsquirrelz/tide-stub-subagent:test",
		"--set", "images.credProxy.tag=test",
		"--set", "images.credProxy.pullPolicy=IfNotPresent",
		"--set", "images.tideReporter.tag=test",
		"--set", "images.tideReporter.pullPolicy=IfNotPresent",
		"--set", "images.tideImport.tag=test",
		"--set", "images.tideImport.pullPolicy=IfNotPresent",
		"--set", "images.tideLanggraphVerifier.tag=test",
		"--set", "images.tideLanggraphVerifier.pullPolicy=IfNotPresent",
		"--set", "images.tidePush.tag=test",
		"--set", "images.tidePush.pullPolicy=IfNotPresent",
		"--set", "workspaces.pvc.accessModes={ReadWriteOnce}",
		"--set", "dashboard.enabled=false",
	}
}

// runHelm runs a helm subcommand and returns its combined output for
// failure-message context.
func runHelm(args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "helm", args...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// deletePostureWebhookConfig deletes ONLY tide-posture's own uniquely-named
// ValidatingWebhookConfiguration — see the file header comment for why this
// runs immediately after every helm install/upgrade. Never targets the
// shared "tide" release's webhook config (different, non-prefixed name).
func deletePostureWebhookConfig() {
	cmd := exec.CommandContext(ctx, "kubectl", "--kubeconfig", kubeconfigPath,
		"delete", "validatingwebhookconfiguration", postureWebhookConfigName,
		"--ignore-not-found=true", "--timeout=15s")
	_, _ = cmd.CombinedOutput()
}

// deletePostureMarkerConfigMap deletes the tide-verify-posture ConfigMap so
// the next helm upgrade simulates a release that predates the marker
// template (RESEARCH Finding 7's deterministic pre-existing-install trick).
func deletePostureMarkerConfigMap() error {
	cm := &corev1.ConfigMap{}
	cm.Name = postureMarkerName
	cm.Namespace = postureNamespace
	if err := k8sClient.Delete(ctx, cm); err != nil && !apierrors.IsNotFound(err) {
		return err
	}
	return nil
}

// assertPostureMarkerEnabled asserts the tide-verify-posture ConfigMap
// exists with data.posture == "enabled".
func assertPostureMarkerEnabled(why string) {
	GinkgoHelper()
	Eventually(func() (string, error) {
		cm := &corev1.ConfigMap{}
		if err := k8sClient.Get(ctx, client.ObjectKey{Name: postureMarkerName, Namespace: postureNamespace}, cm); err != nil {
			return "", err
		}
		return cm.Data[postureMarkerKey], nil
	}, posturePollTimeout, posturePollInterval).Should(Equal("enabled"), why)
}

// assertPostureMarkerAbsent asserts the tide-verify-posture ConfigMap does
// NOT exist (an upgrade must never mint lineage the release never had).
func assertPostureMarkerAbsent(why string) {
	GinkgoHelper()
	cm := &corev1.ConfigMap{}
	err := k8sClient.Get(ctx, client.ObjectKey{Name: postureMarkerName, Namespace: postureNamespace}, cm)
	Expect(apierrors.IsNotFound(err)).To(BeTrue(), fmt.Sprintf("%s (got err=%v)", why, err))
}

// assertPostureVerifyArg asserts whether the manager Deployment's "manager"
// container Args contains a --verify-levels-json= entry.
func assertPostureVerifyArg(wantPresent bool, why string) {
	GinkgoHelper()
	Eventually(func() (bool, error) {
		dep := &appsv1.Deployment{}
		if err := k8sClient.Get(ctx, client.ObjectKey{Name: postureDeploymentName, Namespace: postureNamespace}, dep); err != nil {
			return false, err
		}
		var args []string
		for _, c := range dep.Spec.Template.Spec.Containers {
			if c.Name == "manager" {
				args = c.Args
				break
			}
		}
		for _, a := range args {
			if strings.HasPrefix(a, postureVerifyArgPrefix) {
				return true, nil
			}
		}
		return false, nil
	}, posturePollTimeout, posturePollInterval).Should(Equal(wantPresent), why)
}
