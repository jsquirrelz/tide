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

// integration_miss_test.go — Layer B kind integration regression specs for
// Phase 34 INTEG-05: the wave-parallel integration miss.
//
// Two distinct bug mechanisms are reproduced as SEPARATE specs (CONTEXT
// <specifics>: "treat as distinct mechanisms" — do not assume they are one
// bug):
//
//  1. Single-wave degenerate (cheapest RED for INTEG-01): a Plan with exactly
//     one Kahn wave (two tasks, no dependsOn). Pre-fix, the wave-boundary
//     loop in plan_controller.go ran `k < len(layers)-1`, so a single-wave
//     plan (len(layers)==1) iterated ZERO times — nothing ever integrated
//     either task branch into the run branch.
//  2. 2-parallel-task final wave (the observed production shape): task A
//     (wave 0, no deps), tasks B+C (wave 1/final, dependsOn A). Pre-fix, only
//     wave 0 integrates (the final-wave skip); B and C's branches never
//     merge. Also asserts `status.git.lastPushedSHA` is non-empty and
//     BoundaryPushed=True — reproducing the full observed contradiction
//     (Complete + BoundaryPushed=True + missing deliverable + empty
//     lastPushedSHA).
//
// Both specs reuse the hermetic git-http-server fixture stack from
// medium_http_test.go (anonymous in-cluster push, no LLM cost) in a
// DEDICATED namespace so they don't interfere with that spec's state.
// Branch-ancestry assertions run via an inline Job mounting the
// `tide-projects` PVC at subPath `<project.UID>/workspace` (the
// chaos_resume_test.go PVC-inspection Job pattern), execing `git
// merge-base --is-ancestor` against the tide-push image (git-capable,
// already built/loaded by `make test-int-kind-prep`).
//
// Every It description below contains the literal substring "integration
// miss" so `-ginkgo.focus='integration miss'` selects exactly these two specs.

import (
	"fmt"
	"os/exec"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	tideprojectv1alpha2 "github.com/jsquirrelz/tide/api/v1alpha2"
	pkggit "github.com/jsquirrelz/tide/pkg/git"
)

const (
	// integrationMissNamespace is the dedicated test namespace for the
	// INTEG-05 regression specs — isolated from medium-http-test and other
	// Layer B namespaces so its git-http-server fixture stack doesn't clash.
	integrationMissNamespace = "integration-miss-test"

	// integrationMissTidePushImage is the git-capable image the ancestry
	// assertion Job execs `git merge-base --is-ancestor` from. Built/loaded
	// by `make test-int-kind-prep` (images/tide-push/Dockerfile), already
	// present in the cluster for every other Layer B spec that exercises
	// push/wave-integration Jobs.
	integrationMissTidePushImage = "ghcr.io/jsquirrelz/tide-push:test"
)

// integrationMissTargetRepo is the in-cluster HTTP URL for this spec's demo
// remote, served by its own git-http-server Deployment (provisioned in
// BeforeAll below, mirroring medium_http_test.go).
const integrationMissTargetRepo = "http://git-http-server.integration-miss-test.svc.cluster.local/demo-remote.git"

var _ = Describe("Wave-parallel integration miss regression (Phase 34 INTEG-01..05)", Label("kind"), Ordered, func() {

	BeforeAll(func() {
		skipIfCRDsOnlyMode()
		createNamespace(integrationMissNamespace)

		By("Loading tide-demo-init image into kind cluster")
		loadRequiredImage(mediumHTTPDemoInitImage)
		By("Loading tide-git-http-server image into kind cluster")
		loadRequiredImage(mediumHTTPServerImage)

		By("Creating demo-remote-pvc in " + integrationMissNamespace)
		Expect(applyYAML(mediumDemoRemotePVCYAML(integrationMissNamespace))).To(Succeed(),
			"demo-remote-pvc must be created in "+integrationMissNamespace)

		By("Bootstrapping the bare repo via demo-remote-init Job")
		Expect(applyYAML(mediumDemoRemoteInitJobYAML(integrationMissNamespace))).To(Succeed())
		Eventually(func() error {
			return kubectlWaitJobComplete(integrationMissNamespace, "demo-remote-init", 10*time.Second)
		}, 2*time.Minute, 5*time.Second).Should(Succeed(),
			"demo-remote-init Job must reach Complete within 2 minutes")

		By("Applying git-http-server Deployment + Service into " + integrationMissNamespace)
		Expect(applyYAML(mediumGitHTTPServerYAML(integrationMissNamespace))).To(Succeed())
		Eventually(func() error {
			return kubectlWaitDeploymentAvailable(integrationMissNamespace, mediumHTTPServiceName, 10*time.Second)
		}, 2*time.Minute, 5*time.Second).Should(Succeed(),
			"git-http-server Deployment must reach Available within 2 minutes")

		By("Creating tide-secrets Secret (anonymous http:// push — empty GIT_PAT) in " + integrationMissNamespace)
		tideSecretsYAML := fmt.Sprintf(`apiVersion: v1
kind: Secret
metadata:
  name: tide-secrets
  namespace: %s
type: Opaque
data:
  ANTHROPIC_API_KEY: dGVzdC1hcGkta2V5LXN0dWItc3ViYWdlbnQtZG9lcy1ub3QtdXNlLWl0
  GIT_PAT: ""
`, integrationMissNamespace)
		Expect(applyYAML(tideSecretsYAML)).To(Succeed())
	})

	AfterAll(func() {
		deleteNamespace(integrationMissNamespace)
		if CurrentSpecReport().Failed() {
			exportKindLogs()
		}
	})

	// -------------------------------------------------------------------
	// Spec 1: single-wave degenerate — cheapest RED repro of INTEG-01.
	// -------------------------------------------------------------------
	It("integration miss: single-wave degenerate plan integrates both task branches into the run branch", func() {
		projName := fmt.Sprintf("integ-miss-single-wave-%d", GinkgoRandomSeed())
		proj := newStubProject(integrationMissNamespace, projName,
			withTargetRepo(integrationMissTargetRepo),
			withProviderSecret("tide-secrets"),
			withGit(integrationMissTargetRepo, "tide-secrets"))
		By("Creating single-wave Project " + projName)
		Expect(k8sClient.Create(ctx, proj)).To(Succeed())
		defer func() { _ = k8sClient.Delete(ctx, proj) }()

		msName := projName + "-ms"
		phName := projName + "-ph"
		planName := projName + "-plan"
		for _, o := range []client.Object{
			newStubMilestone(integrationMissNamespace, msName, projName),
			newStubPhase(integrationMissNamespace, phName, msName),
			newStubPlan(integrationMissNamespace, planName, phName, withPlanProjectLabel(projName)),
		} {
			Expect(createFixture(ctx, o)).To(Succeed())
		}

		// Two tasks, NO dependsOn — a single Kahn wave. Both wave index 0.
		taskA := newStubTask(integrationMissNamespace, "sw-task-a", planName,
			withTaskProjectLabel(projName), withWaveIndex("0"), withPromptPath("children/task-01.json"))
		taskB := newStubTask(integrationMissNamespace, "sw-task-b", planName,
			withTaskProjectLabel(projName), withWaveIndex("0"), withPromptPath("children/task-02.json"))
		Expect(createFixture(ctx, taskA)).To(Succeed())
		Expect(createFixture(ctx, taskB)).To(Succeed())

		By("Waiting for the Project to reach Complete")
		Eventually(func() error {
			var current tideprojectv1alpha2.Project
			if err := k8sClient.Get(ctx, client.ObjectKey{Name: projName, Namespace: integrationMissNamespace}, &current); err != nil {
				return err
			}
			if current.Status.Phase != "Complete" {
				return fmt.Errorf("Project %s: want Complete, got %q", projName, current.Status.Phase)
			}
			return nil
		}, 10*time.Minute, 5*time.Second).Should(Succeed())

		var final tideprojectv1alpha2.Project
		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: projName, Namespace: integrationMissNamespace}, &final)).To(Succeed())
		runBranch := final.Status.Git.BranchName
		Expect(runBranch).NotTo(BeEmpty(), "Project must have stamped a run branch")

		var taskAFresh, taskBFresh tideprojectv1alpha2.Task
		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: "sw-task-a", Namespace: integrationMissNamespace}, &taskAFresh)).To(Succeed())
		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: "sw-task-b", Namespace: integrationMissNamespace}, &taskBFresh)).To(Succeed())

		branchA := pkggit.TaskBranchName(string(taskAFresh.UID))
		branchB := pkggit.TaskBranchName(string(taskBFresh.UID))

		By("Asserting both task branches are ancestors of the run branch (single-wave degenerate)")
		assertBranchesAreAncestors(string(final.UID), runBranch, []string{branchA, branchB})
	})

	// -------------------------------------------------------------------
	// Spec 2: 2-parallel-task final wave — the observed production shape.
	// -------------------------------------------------------------------
	It("integration miss: 2-parallel-task final wave integrates all three task branches and stamps lastPushedSHA", func() {
		projName := fmt.Sprintf("integ-miss-final-wave-%d", GinkgoRandomSeed())
		proj := newStubProject(integrationMissNamespace, projName,
			withTargetRepo(integrationMissTargetRepo),
			withProviderSecret("tide-secrets"),
			withGit(integrationMissTargetRepo, "tide-secrets"))
		By("Creating final-wave Project " + projName)
		Expect(k8sClient.Create(ctx, proj)).To(Succeed())
		defer func() { _ = k8sClient.Delete(ctx, proj) }()

		msName := projName + "-ms"
		phName := projName + "-ph"
		planName := projName + "-plan"
		for _, o := range []client.Object{
			newStubMilestone(integrationMissNamespace, msName, projName),
			newStubPhase(integrationMissNamespace, phName, msName),
			newStubPlan(integrationMissNamespace, planName, phName, withPlanProjectLabel(projName)),
		} {
			Expect(createFixture(ctx, o)).To(Succeed())
		}

		// task A: wave 0, no deps. tasks B + C: wave 1 (FINAL), dependsOn A —
		// the observed 2-parallel-task final-wave shape.
		taskA := newStubTask(integrationMissNamespace, "fw-task-a", planName,
			withTaskProjectLabel(projName), withWaveIndex("0"), withPromptPath("children/task-01.json"))
		taskB := newStubTask(integrationMissNamespace, "fw-task-b", planName,
			withTaskProjectLabel(projName), withWaveIndex("1"), withPromptPath("children/task-02.json"),
			withTaskDependsOn("fw-task-a"))
		taskC := newStubTask(integrationMissNamespace, "fw-task-c", planName,
			withTaskProjectLabel(projName), withWaveIndex("1"), withPromptPath("children/task-03.json"),
			withTaskDependsOn("fw-task-a"))
		Expect(createFixture(ctx, taskA)).To(Succeed())
		Expect(createFixture(ctx, taskB)).To(Succeed())
		Expect(createFixture(ctx, taskC)).To(Succeed())

		By("Waiting for the Project to reach Complete")
		Eventually(func() error {
			var current tideprojectv1alpha2.Project
			if err := k8sClient.Get(ctx, client.ObjectKey{Name: projName, Namespace: integrationMissNamespace}, &current); err != nil {
				return err
			}
			if current.Status.Phase != "Complete" {
				return fmt.Errorf("Project %s: want Complete, got %q", projName, current.Status.Phase)
			}
			return nil
		}, 10*time.Minute, 5*time.Second).Should(Succeed())

		// BoundaryPushed=True — wait for the boundary-push retry state
		// machine to converge (may take a few reconcile passes after Complete).
		By("Waiting for BoundaryPushed=True")
		var final tideprojectv1alpha2.Project
		Eventually(func() error {
			if err := k8sClient.Get(ctx, client.ObjectKey{Name: projName, Namespace: integrationMissNamespace}, &final); err != nil {
				return err
			}
			c := meta.FindStatusCondition(final.Status.Conditions, tideprojectv1alpha2.ConditionBoundaryPushed)
			if c == nil || c.Status != metav1.ConditionTrue {
				return fmt.Errorf("BoundaryPushed not yet True (condition: %+v)", c)
			}
			return nil
		}, 5*time.Minute, 5*time.Second).Should(Succeed())

		runBranch := final.Status.Git.BranchName
		Expect(runBranch).NotTo(BeEmpty())

		// The full observed contradiction this spec locks against: a Complete
		// Project with BoundaryPushed=True must NOT have an empty lastPushedSHA
		// (Pitfall 4 / D-14 — the stamp must be armed).
		Expect(final.Status.Git.LastPushedSHA).NotTo(BeEmpty(),
			"lastPushedSHA must be stamped once BoundaryPushed=True (D-14)")

		var taskAFresh, taskBFresh, taskCFresh tideprojectv1alpha2.Task
		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: "fw-task-a", Namespace: integrationMissNamespace}, &taskAFresh)).To(Succeed())
		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: "fw-task-b", Namespace: integrationMissNamespace}, &taskBFresh)).To(Succeed())
		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: "fw-task-c", Namespace: integrationMissNamespace}, &taskCFresh)).To(Succeed())

		branches := []string{
			pkggit.TaskBranchName(string(taskAFresh.UID)),
			pkggit.TaskBranchName(string(taskBFresh.UID)),
			pkggit.TaskBranchName(string(taskCFresh.UID)),
		}

		By("Asserting all three task branches (incl. the final wave) are ancestors of the run branch")
		assertBranchesAreAncestors(string(final.UID), runBranch, branches)
	})
})

// kubectlWaitJobComplete waits (once, non-blocking on this call — callers
// wrap in Eventually) for the named Job to reach condition=Complete.
func kubectlWaitJobComplete(ns, jobName string, timeout time.Duration) error {
	cmd := exec.CommandContext(ctx, "kubectl", "--kubeconfig", kubeconfigPath,
		"wait", "--for=condition=Complete",
		"job/"+jobName, "-n", ns, fmt.Sprintf("--timeout=%s", timeout))
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("job %s/%s not yet Complete: %w\n%s", ns, jobName, err, out)
	}
	return nil
}

// kubectlWaitDeploymentAvailable waits (once — callers wrap in Eventually)
// for the named Deployment to reach condition=Available.
func kubectlWaitDeploymentAvailable(ns, deployName string, timeout time.Duration) error {
	cmd := exec.CommandContext(ctx, "kubectl", "--kubeconfig", kubeconfigPath,
		"wait", "--for=condition=Available",
		"deployment/"+deployName, "-n", ns, fmt.Sprintf("--timeout=%s", timeout))
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("deployment %s/%s not yet Available: %w\n%s", ns, deployName, err, out)
	}
	return nil
}

// assertBranchesAreAncestors runs an inline Job (chaos_resume_test.go
// PVC-inspection pattern) mounting the tide-projects PVC at subPath
// <projectUID>/workspace, execing `git merge-base --is-ancestor <branch>
// <runBranch>` in the bare repo for every branch in taskBranches. Job
// success (exit 0) = every branch is an ancestor; failure = at least one
// miss (pre-fix: this is exactly the INTEG-01/02 miss this phase closes).
func assertBranchesAreAncestors(projectUID, runBranch string, taskBranches []string) {
	GinkgoHelper()

	jobName := fmt.Sprintf("integration-miss-assert-%d", GinkgoRandomSeed())
	subPath := fmt.Sprintf("%s/workspace", projectUID)

	var scriptBuilder strings.Builder
	scriptBuilder.WriteString("set -e; cd /workspace/repo.git; ")
	for _, br := range taskBranches {
		fmt.Fprintf(&scriptBuilder,
			"echo 'checking %s is ancestor of %s'; git merge-base --is-ancestor '%s' '%s' || { echo 'MISSING: %s not an ancestor of %s'; exit 1; }; ",
			br, runBranch, br, runBranch, br, runBranch)
	}
	scriptBuilder.WriteString("echo 'all branches are ancestors of the run branch'")
	script := scriptBuilder.String()

	ttl := int32(60)
	backoff := int32(1)
	deadline := int64(120)
	fsGroup := int64(1000)
	runAsUser := int64(65532)
	allowPrivEsc := false

	job := &batchv1.Job{}
	job.Name = jobName
	job.Namespace = integrationMissNamespace
	job.Spec.BackoffLimit = &backoff
	job.Spec.TTLSecondsAfterFinished = &ttl
	job.Spec.ActiveDeadlineSeconds = &deadline
	job.Spec.Template.Spec = corev1.PodSpec{
		RestartPolicy:      corev1.RestartPolicyNever,
		ServiceAccountName: "tide-subagent",
		SecurityContext: &corev1.PodSecurityContext{
			FSGroup: &fsGroup,
		},
		Volumes: []corev1.Volume{
			{
				Name: "project-workspace",
				VolumeSource: corev1.VolumeSource{
					PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
						ClaimName: "tide-projects",
					},
				},
			},
		},
		Containers: []corev1.Container{
			{
				Name:    "assert",
				Image:   integrationMissTidePushImage,
				Command: []string{"sh", "-c", script},
				SecurityContext: &corev1.SecurityContext{
					RunAsUser:                &runAsUser,
					AllowPrivilegeEscalation: &allowPrivEsc,
					Capabilities: &corev1.Capabilities{
						Drop: []corev1.Capability{"ALL"},
					},
				},
				VolumeMounts: []corev1.VolumeMount{
					{
						Name:      "project-workspace",
						MountPath: "/workspace",
						SubPath:   subPath,
					},
				},
			},
		},
	}

	Expect(k8sClient.Create(ctx, job)).To(Succeed(), "create ancestry-assert Job")

	Eventually(func() (bool, error) {
		var j batchv1.Job
		if err := k8sClient.Get(ctx, client.ObjectKey{Name: jobName, Namespace: integrationMissNamespace}, &j); err != nil {
			return false, err
		}
		if isJobSucceededShort(&j) {
			return true, nil
		}
		if j.Status.Failed > 0 {
			logsCmd := exec.CommandContext(ctx, "kubectl", "--kubeconfig", kubeconfigPath,
				"logs", "job/"+jobName, "-n", integrationMissNamespace, "-c", "assert", "--tail=50")
			logsOut, _ := logsCmd.CombinedOutput()
			return false, fmt.Errorf("ancestry-assert Job %s failed (branches missing from run branch %s); logs (best-effort): %s",
				jobName, runBranch, string(logsOut))
		}
		return false, nil
	}, 2*time.Minute, 2*time.Second).Should(BeTrue(),
		"all task branches must be ancestors of the run branch — a false result here means the wave-parallel integration miss (INTEG-01/02) regressed")
}
