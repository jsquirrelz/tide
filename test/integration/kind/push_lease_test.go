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

// push_lease_test.go — Layer B kind integration spec for plan 03-10.
//
// Coverage: ART-06 / D-B5 / D-B6 — per-Project push serialization, per-run
// branch, --force-with-lease against Status.Git.LastPushedSHA, PushLeaseFailed
// state machine, and bypass-push-lease annotation recovery (Pitfall 13).
//
// The spec verifies the ProjectReconciler's HANDLING of push Job lifecycle
// outcomes, not the actual `git push`. Real git pushes are not attempted —
// the remote URL is `https://example.invalid/...` and push Job outcomes
// are mocked by patching Job.Status directly. This isolates the test to the
// state-machine contract while keeping it cost-bounded and reliable.
//
// Each It uses its own namespace to keep test state independent; no shared
// fixture across the four scenarios.

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"

	tideprojectv1alpha1 "github.com/jsquirrelz/tide/api/v1alpha1"
)

var _ = Describe("Push lease semantics (ART-06 / D-B5 / D-B6)", Label("kind"), func() {
	const pushLeaseNS = "push-lease-test"

	BeforeEach(func() {
		skipIfCRDsOnlyMode()
		By("Ensure namespace-local SA + signing-key Secret (Phase 04.1 P12 Cascade 9 — same shape as Cascade 6)")
		createNamespace(pushLeaseNS)
	})

	AfterEach(func() {
		deleteNamespace(pushLeaseNS)
		if CurrentSpecReport().Failed() {
			exportKindLogs()
		}
	})

	// Test 1 — D-B6: first push omits the lease. Without a prior
	// Status.Git.LastPushedSHA, the push Job's --last-pushed-sha arg is empty.
	It("Test 1: first push Job has empty --last-pushed-sha arg (no prior lease)", func() {
		By("Apply push-lease fixture and wait for Project to exist")
		Expect(applyFile("testdata/push-lease-project.yaml")).To(Succeed())
		project := waitForPushProject("push-lease", pushLeaseNS, 30*time.Second)

		By("Patch Status.Phase=Complete to trigger push (bypasses planner lifecycle)")
		forcePushReady(project, "" /* no prior SHA */)

		By("Observe push Job tide-push-<project-uid> dispatched with empty --last-pushed-sha")
		jobName := fmt.Sprintf("tide-push-%s", project.UID)
		job := waitForPushJob(pushLeaseNS, jobName, 90*time.Second)
		args := pushJobArgs(job)
		Expect(args).To(ContainElement(HavePrefix("--last-pushed-sha=")),
			"first push Job must carry --last-pushed-sha= arg (D-B6)")
		Expect(args).To(ContainElement("--last-pushed-sha="),
			"first push Job's lease MUST be empty (no prior Status.Git.LastPushedSHA)")
		Expect(args).To(ContainElement(HavePrefix("--branch=tide/run-push-lease-")),
			"first push Job must carry --branch=tide/run-push-lease-<unix> (D-B6 refname format)")
	})

	// Test 2 — D-B6: subsequent push carries the lease. With a known prior SHA
	// recorded on Status.Git.LastPushedSHA, the push Job carries that value
	// in --last-pushed-sha (the lease the new push will check against).
	It("Test 2: subsequent push Job carries --last-pushed-sha=<recorded-SHA>", func() {
		const priorSHA = "deadbeef0123456789abcdef0123456789abcdef"

		By("Apply push-lease fixture")
		Expect(applyFile("testdata/push-lease-project.yaml")).To(Succeed())
		project := waitForPushProject("push-lease", pushLeaseNS, 30*time.Second)

		By("Patch Status.Git.LastPushedSHA = " + priorSHA + " and Phase=Complete")
		forcePushReady(project, priorSHA)

		By("Observe push Job dispatched with --last-pushed-sha=" + priorSHA)
		jobName := fmt.Sprintf("tide-push-%s", project.UID)
		job := waitForPushJob(pushLeaseNS, jobName, 90*time.Second)
		args := pushJobArgs(job)
		Expect(args).To(ContainElement("--last-pushed-sha="+priorSHA),
			"subsequent push must carry recorded LastPushedSHA as the lease (D-B6)")
	})

	// Test 3 — D-B6: stale-lease rejection. When the push Job fails, the
	// ProjectReconciler patches Status.Phase=PushLeaseFailed + increments
	// LeaseFailureCount (Plan 03-08 treats Job failure as lease rejection
	// per the plan's documented state transition).
	It("Test 3: push Job failure → Status.Phase=PushLeaseFailed + LeaseFailureCount++", func() {
		By("Apply push-lease fixture")
		Expect(applyFile("testdata/push-lease-project.yaml")).To(Succeed())
		project := waitForPushProject("push-lease", pushLeaseNS, 30*time.Second)

		By("Force Phase=Complete to trigger the first push Job")
		forcePushReady(project, "")

		By("Wait for the push Job to exist, then patch Job.Status to Failed")
		jobName := fmt.Sprintf("tide-push-%s", project.UID)
		waitForPushJob(pushLeaseNS, jobName, 90*time.Second)
		patchJobToFailed(pushLeaseNS, jobName)

		By("Eventually Project.Status.Phase=PushLeaseFailed + LeaseFailureCount==1")
		Eventually(func(g Gomega) {
			var p tideprojectv1alpha1.Project
			g.Expect(k8sClient.Get(ctx, client.ObjectKey{
				Name: "push-lease", Namespace: pushLeaseNS,
			}, &p)).To(Succeed())
			g.Expect(p.Status.Phase).To(Equal(tideprojectv1alpha1.PhasePushLeaseFailed),
				"Status.Phase must be PushLeaseFailed after push Job failure")
			g.Expect(p.Status.Git.LeaseFailureCount).To(BeNumerically(">=", int32(1)),
				"LeaseFailureCount must be incremented on push Job failure")
		}, 90*time.Second, 2*time.Second).Should(Succeed())
	})

	// Test 4 — D-B6: bypass annotation recovery. Annotating the Project with
	// tideproject.k8s/bypass-push-lease=true clears PushLeaseFailed and
	// transitions back to a running state for the next push attempt.
	It("Test 4: bypass-push-lease=true annotation clears PushLeaseFailed", func() {
		By("Set up Test 3's end state: PushLeaseFailed")
		Expect(applyFile("testdata/push-lease-project.yaml")).To(Succeed())
		project := waitForPushProject("push-lease", pushLeaseNS, 30*time.Second)
		forcePushReady(project, "")
		jobName := fmt.Sprintf("tide-push-%s", project.UID)
		waitForPushJob(pushLeaseNS, jobName, 90*time.Second)
		patchJobToFailed(pushLeaseNS, jobName)
		Eventually(func() string {
			var p tideprojectv1alpha1.Project
			if err := k8sClient.Get(ctx, client.ObjectKey{
				Name: "push-lease", Namespace: pushLeaseNS,
			}, &p); err != nil {
				return ""
			}
			return p.Status.Phase
		}, 90*time.Second, 2*time.Second).Should(Equal(tideprojectv1alpha1.PhasePushLeaseFailed))

		By("Annotate Project with tideproject.k8s/bypass-push-lease=true")
		annotateProjectBypass("push-lease", pushLeaseNS)

		By("Eventually Project.Status.Phase != PushLeaseFailed (annotation consumed, phase cleared)")
		Eventually(func() string {
			var p tideprojectv1alpha1.Project
			if err := k8sClient.Get(ctx, client.ObjectKey{
				Name: "push-lease", Namespace: pushLeaseNS,
			}, &p); err != nil {
				return ""
			}
			return p.Status.Phase
		}, 90*time.Second, 2*time.Second).ShouldNot(Equal(tideprojectv1alpha1.PhasePushLeaseFailed),
			"bypass-push-lease=true must clear the PushLeaseFailed phase")
	})
})

// ---- helpers (push_lease_test.go-local) ----

// waitForPushProject blocks until the Project exists in K8s and returns it.
func waitForPushProject(name, ns string, timeout time.Duration) *tideprojectv1alpha1.Project {
	var p tideprojectv1alpha1.Project
	Eventually(func() error {
		return k8sClient.Get(ctx, client.ObjectKey{Name: name, Namespace: ns}, &p)
	}, timeout, time.Second).Should(Succeed(),
		"Project %s/%s must exist", ns, name)
	return &p
}

// forcePushReady forces the Project into a state where the ProjectReconciler
// will dispatch a push Job: Status.Git.BranchName seeded if empty,
// Status.Git.LastPushedSHA = lastPushedSHA, Status.Phase = PhaseComplete.
//
// Uses kubectl patch with type=merge for the /status subresource because the
// k8sClient.Status().Update path is racy when the controller is concurrently
// patching status.
func forcePushReady(p *tideprojectv1alpha1.Project, lastPushedSHA string) {
	// Seed BranchName via Status patch (matches the reconciler's expected
	// "tide/run-<name>-<unix>" format so the lease grep finds it).
	branch := fmt.Sprintf("tide/run-%s-%d", p.Name, time.Now().Unix())
	statusBody := map[string]any{
		"status": map[string]any{
			"phase": tideprojectv1alpha1.PhaseComplete,
			"git": map[string]any{
				"branchName":    branch,
				"lastPushedSHA": lastPushedSHA,
			},
		},
	}
	body, err := json.Marshal(statusBody)
	Expect(err).NotTo(HaveOccurred())
	cmd := exec.CommandContext(ctx, "kubectl", "--kubeconfig", kubeconfigPath,
		"patch", "project", p.Name, "-n", p.Namespace,
		"--subresource=status", "--type=merge",
		"--patch", string(body))
	out, err := cmd.CombinedOutput()
	Expect(err).NotTo(HaveOccurred(),
		"kubectl patch project status failed: %s", out)
}

// waitForPushJob blocks until the named Job exists in the namespace.
func waitForPushJob(ns, jobName string, timeout time.Duration) *batchv1.Job {
	var job batchv1.Job
	Eventually(func() error {
		return k8sClient.Get(ctx, client.ObjectKey{Name: jobName, Namespace: ns}, &job)
	}, timeout, time.Second).Should(Succeed(),
		"push Job %s/%s must be dispatched", ns, jobName)
	return &job
}

// pushJobArgs returns the args of the first container in the push Job.
func pushJobArgs(job *batchv1.Job) []string {
	if len(job.Spec.Template.Spec.Containers) == 0 {
		return nil
	}
	return job.Spec.Template.Spec.Containers[0].Args
}

// patchJobToFailed forces the named Job into a Failed terminal state by
// patching its Status with a JobFailed=True condition. Used to mock push
// Job lease rejection without actually running tide-push against a real
// remote.
func patchJobToFailed(ns, jobName string) {
	patch := map[string]any{
		"status": map[string]any{
			"failed": 1,
			"conditions": []map[string]any{
				// K8s 1.31+ requires FailureTarget=True before Failed=True can be set on Job status.
				// See .planning/debug/push-lease-pvc-pending.md cascade-12 footnote.
				{
					"type":               string(batchv1.JobFailureTarget),
					"status":             string(corev1.ConditionTrue),
					"reason":             "LeaseRejected",
					"message":            "mocked: --force-with-lease detected divergence",
					"lastTransitionTime": time.Now().UTC().Format(time.RFC3339),
					"lastProbeTime":      time.Now().UTC().Format(time.RFC3339),
				},
				{
					"type":               string(batchv1.JobFailed),
					"status":             string(corev1.ConditionTrue),
					"reason":             "LeaseRejected",
					"message":            "mocked: --force-with-lease detected divergence",
					"lastTransitionTime": time.Now().UTC().Format(time.RFC3339),
					"lastProbeTime":      time.Now().UTC().Format(time.RFC3339),
				},
			},
		},
	}
	body, err := json.Marshal(patch)
	Expect(err).NotTo(HaveOccurred())
	cmd := exec.CommandContext(ctx, "kubectl", "--kubeconfig", kubeconfigPath,
		"patch", "job", jobName, "-n", ns,
		"--subresource=status", "--type=merge",
		"--patch", string(body))
	out, err := cmd.CombinedOutput()
	if err != nil {
		// Some kube-apiserver versions fail on `kubectl patch job --subresource=status`
		// for batch/v1; fall back to a non-subresource patch (kind v0.31 + k8s 1.33
		// supports the subresource path, but be defensive).
		if !strings.Contains(string(out), "the server could not find the requested resource") {
			Fail(fmt.Sprintf("patch Job status failed: %v\n%s", err, out))
		}
		cmd = exec.CommandContext(ctx, "kubectl", "--kubeconfig", kubeconfigPath,
			"patch", "job", jobName, "-n", ns,
			"--type=merge", "--patch", string(body))
		out, err = cmd.CombinedOutput()
		Expect(err).NotTo(HaveOccurred(),
			"fallback patch Job status (no subresource) failed: %s", out)
	}
}

// annotateProjectBypass adds the tideproject.k8s/bypass-push-lease=true
// annotation on the named Project, mirroring `kubectl annotate`.
func annotateProjectBypass(name, ns string) {
	cmd := exec.CommandContext(ctx, "kubectl", "--kubeconfig", kubeconfigPath,
		"annotate", "project", name, "-n", ns,
		"tideproject.k8s/bypass-push-lease=true", "--overwrite")
	out, err := cmd.CombinedOutput()
	Expect(err).NotTo(HaveOccurred(),
		"kubectl annotate failed: %s", out)
}

// (compile-time use to keep imports tidy if all-args helpers shift)
var _ = context.Background
var _ = apierrors.IsNotFound
