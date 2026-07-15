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
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	tideprojectv1alpha3 "github.com/jsquirrelz/tide/api/v1alpha3"
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

// FlakeAttempts(1): retrying these Ordered specs is pure noise — the
// namespace persists across attempts, so recreating same-named fixtures
// collides with deferred deletes and the retry's failure text buries the
// real attempt-1 diagnostics (observed across three CI runs of PR #3).
var _ = Describe("Wave-parallel integration miss regression (Phase 34 INTEG-01..05)", Label("kind"), Ordered, FlakeAttempts(1), func() {

	BeforeAll(func() {
		skipIfCRDsOnlyMode()
		createNamespace(integrationMissNamespace)
		// Real clone / wave-integration / boundary-push pods run here — their
		// Jobs reference serviceAccountName tide-push, which the chart only
		// installs in tide-system (push-rbac.yaml cross-namespace caveat).
		ensurePushSARBAC(integrationMissNamespace)

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
		if CurrentSpecReport().Failed() {
			// Dump CRD + Job state BEFORE the namespace (and with it every
			// object) is deleted — the kind-logs artifact carries node/pod
			// logs only, and three CI iterations lost their diagnostics to
			// this ordering.
			dumpNamespaceState(integrationMissNamespace)
			exportKindLogs()
		}
		deleteNamespaceAndWait(integrationMissNamespace)
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
		Expect(createFixture(ctx, proj)).To(Succeed())
		// No deferred Delete: cleanup belongs to AfterAll's deleteNamespaceAndWait.
		// A function-level defer fires BEFORE any failure diagnostics run
		// (Ginkgo failure unwinds the It body first), cascading the whole
		// hierarchy away and leaving nothing to dump. DeferCleanup runs
		// after the attempt with the failure state still intact.
		DeferCleanup(func() {
			if CurrentSpecReport().Failed() {
				dumpNamespaceState(integrationMissNamespace)
			}
		})

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
		// wait-for-signal: the stub subagent creates no git branches, so the
		// tasks must NOT report success until the branch-writer fixture Job
		// has provisioned their tide/wt-<uid> branches (the real executor's
		// observable git effect) — otherwise the wave-integration Job
		// dispatches against branches that don't exist yet and burns its
		// bounded-retry budget on integration-incomplete misses.
		taskA := newStubTask(integrationMissNamespace, "sw-task-a", planName,
			withTaskProjectLabel(projName), withWaveIndex("0"), withPromptPath("children/task-01.json"),
			withTestMode("wait-for-signal"))
		taskB := newStubTask(integrationMissNamespace, "sw-task-b", planName,
			withTaskProjectLabel(projName), withWaveIndex("0"), withPromptPath("children/task-02.json"),
			withTestMode("wait-for-signal"))
		Expect(createFixture(ctx, taskA)).To(Succeed())
		Expect(createFixture(ctx, taskB)).To(Succeed())

		// The stub hierarchy bypasses every flow that stamps
		// Plan.Status.ValidationState="Validated" (planner-Job completion,
		// reporter follow-up, import). reconcileWaveMaterialization no-ops
		// until Validated, so the fixture stamps it — mirroring
		// import_controller.go's precedent for plans materialized without a
		// planner Job. Milestone/Phase need the analogous Status.Phase=
		// Running stamp (see stampMilestoneRunning/stampPhaseRunning) or the
		// Project never learns its child Plan Succeeded.
		By("Stamping Status.Phase=Running / ValidationState=Validated on the stub hierarchy")
		stampMilestoneRunning(msName)
		stampPhaseRunning(phName)
		stampPlanValidated(planName)

		By("Waiting for the workspace clone to complete (run branch + repo.git on the PVC)")
		runBranchName := waitForCloneComplete(projName)

		By("Provisioning task branches + release signals via the branch-writer Job")
		var freshProj tideprojectv1alpha3.Project
		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: projName, Namespace: integrationMissNamespace}, &freshProj)).To(Succeed())
		// Re-fetch task UIDs from the cluster: on a flake-attempt re-run the
		// createFixture calls hit AlreadyExists and leave the builder
		// objects' UIDs empty.
		var tA, tB tideprojectv1alpha3.Task
		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: "sw-task-a", Namespace: integrationMissNamespace}, &tA)).To(Succeed())
		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: "sw-task-b", Namespace: integrationMissNamespace}, &tB)).To(Succeed())
		provisionTaskBranchesAndSignal(projName+"-branch-writer", string(freshProj.UID), runBranchName,
			[]string{string(tA.UID), string(tB.UID)})

		By("Waiting for the Project to reach Complete")
		Eventually(func() error {
			var current tideprojectv1alpha3.Project
			if err := k8sClient.Get(ctx, client.ObjectKey{Name: projName, Namespace: integrationMissNamespace}, &current); err != nil {
				return err
			}
			if current.Status.Phase != "Complete" {
				return fmt.Errorf("Project %s: want Complete, got %q", projName, current.Status.Phase)
			}
			return nil
		}, 10*time.Minute, 5*time.Second).Should(Succeed())

		var final tideprojectv1alpha3.Project
		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: projName, Namespace: integrationMissNamespace}, &final)).To(Succeed())
		runBranch := final.Status.Git.BranchName
		Expect(runBranch).NotTo(BeEmpty(), "Project must have stamped a run branch")

		var taskAFresh, taskBFresh tideprojectv1alpha3.Task
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
		Expect(createFixture(ctx, proj)).To(Succeed())
		// See spec 1: no deferred Delete; failure dump via DeferCleanup.
		DeferCleanup(func() {
			if CurrentSpecReport().Failed() {
				dumpNamespaceState(integrationMissNamespace)
			}
		})

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
		// the observed 2-parallel-task final-wave shape. wait-for-signal for
		// the same reason as the single-wave spec: branches must exist before
		// tasks report success. All three branches + release files are
		// provisioned up-front — B/C's pods only dispatch after A succeeds
		// and find their release signal immediately.
		taskA := newStubTask(integrationMissNamespace, "fw-task-a", planName,
			withTaskProjectLabel(projName), withWaveIndex("0"), withPromptPath("children/task-01.json"),
			withTestMode("wait-for-signal"))
		taskB := newStubTask(integrationMissNamespace, "fw-task-b", planName,
			withTaskProjectLabel(projName), withWaveIndex("1"), withPromptPath("children/task-02.json"),
			withTaskDependsOn("fw-task-a"), withTestMode("wait-for-signal"))
		taskC := newStubTask(integrationMissNamespace, "fw-task-c", planName,
			withTaskProjectLabel(projName), withWaveIndex("1"), withPromptPath("children/task-03.json"),
			withTaskDependsOn("fw-task-a"), withTestMode("wait-for-signal"))
		Expect(createFixture(ctx, taskA)).To(Succeed())
		Expect(createFixture(ctx, taskB)).To(Succeed())
		Expect(createFixture(ctx, taskC)).To(Succeed())

		By("Stamping Status.Phase=Running / ValidationState=Validated on the stub hierarchy")
		stampMilestoneRunning(msName)
		stampPhaseRunning(phName)
		stampPlanValidated(planName)

		By("Waiting for the workspace clone to complete (run branch + repo.git on the PVC)")
		runBranchName := waitForCloneComplete(projName)

		By("Provisioning task branches + release signals via the branch-writer Job")
		var freshProj tideprojectv1alpha3.Project
		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: projName, Namespace: integrationMissNamespace}, &freshProj)).To(Succeed())
		// Re-fetch task UIDs (see spec 1: flake-attempt AlreadyExists leaves
		// the builder objects UID-less).
		var tA, tB, tC tideprojectv1alpha3.Task
		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: "fw-task-a", Namespace: integrationMissNamespace}, &tA)).To(Succeed())
		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: "fw-task-b", Namespace: integrationMissNamespace}, &tB)).To(Succeed())
		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: "fw-task-c", Namespace: integrationMissNamespace}, &tC)).To(Succeed())
		provisionTaskBranchesAndSignal(projName+"-branch-writer", string(freshProj.UID), runBranchName,
			[]string{string(tA.UID), string(tB.UID), string(tC.UID)})

		By("Waiting for the Project to reach Complete")
		Eventually(func() error {
			var current tideprojectv1alpha3.Project
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
		var final tideprojectv1alpha3.Project
		Eventually(func() error {
			if err := k8sClient.Get(ctx, client.ObjectKey{Name: projName, Namespace: integrationMissNamespace}, &final); err != nil {
				return err
			}
			c := meta.FindStatusCondition(final.Status.Conditions, tideprojectv1alpha3.ConditionBoundaryPushed)
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

		var taskAFresh, taskBFresh, taskCFresh tideprojectv1alpha3.Task
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

// stampPlanValidated patches Plan.Status.ValidationState="Validated" on the
// named stub Plan. Production stamps Validated from planner-Job completion
// (plan_controller.go handlePlannerJobCompletion), the reporter follow-up, or
// the import flow (import_controller.go) — a fixture-created Plan bypasses
// all three, and reconcileWaveMaterialization is a silent no-op until the
// stamp lands (its Step-1 gate). Retries on conflict: the PlanReconciler
// patches status concurrently (finalizer/owner-ref passes).
func stampPlanValidated(planName string) {
	GinkgoHelper()
	Eventually(func() error {
		var pl tideprojectv1alpha3.Plan
		if err := k8sClient.Get(ctx, client.ObjectKey{Name: planName, Namespace: integrationMissNamespace}, &pl); err != nil {
			return err
		}
		if pl.Status.ValidationState == "Validated" {
			return nil
		}
		patch := client.MergeFrom(pl.DeepCopy())
		pl.Status.ValidationState = "Validated"
		return k8sClient.Status().Patch(ctx, &pl, patch)
	}, 30*time.Second, time.Second).Should(Succeed(),
		"stub Plan %s must accept the ValidationState=Validated stamp", planName)
}

// stampMilestoneRunning and stampPhaseRunning mark the stub Milestone/Phase
// Status.Phase="Running", mirroring what a real planner dispatch sets
// (milestone_controller.go / phase_controller.go, both patch Status.Phase
// "Running" before returning). The stub hierarchy creates Milestone and Phase
// directly, bypassing planner dispatch entirely, so Status.Phase starts blank
// — and MilestoneReconciler/PhaseReconciler's idempotency guard ("skip
// dispatch when I already own >=1 child") only runs on that non-Running path,
// short-circuiting to a silent no-op the instant it sees the fixture's
// already-created child. Status.Phase never becomes "Running", so the
// ChildCount-gated succession logic in handleJobCompletion — which only runs
// on the Running branch — never fires: the Milestone/Phase never notice their
// child Succeeded, and the whole Project stalls at phase=Running forever.
func stampMilestoneRunning(name string) {
	GinkgoHelper()
	Eventually(func() error {
		var ms tideprojectv1alpha3.Milestone
		if err := k8sClient.Get(ctx, client.ObjectKey{Name: name, Namespace: integrationMissNamespace}, &ms); err != nil {
			return err
		}
		if ms.Status.Phase == tideprojectv1alpha3.PhaseRunning {
			return nil
		}
		patch := client.MergeFrom(ms.DeepCopy())
		ms.Status.Phase = tideprojectv1alpha3.PhaseRunning
		return k8sClient.Status().Patch(ctx, &ms, patch)
	}, 30*time.Second, time.Second).Should(Succeed(),
		"stub Milestone %s must accept the Status.Phase=Running stamp", name)
}

func stampPhaseRunning(name string) {
	GinkgoHelper()
	Eventually(func() error {
		var ph tideprojectv1alpha3.Phase
		if err := k8sClient.Get(ctx, client.ObjectKey{Name: name, Namespace: integrationMissNamespace}, &ph); err != nil {
			return err
		}
		if ph.Status.Phase == tideprojectv1alpha3.PhaseRunning {
			return nil
		}
		patch := client.MergeFrom(ph.DeepCopy())
		ph.Status.Phase = tideprojectv1alpha3.PhaseRunning
		return k8sClient.Status().Patch(ctx, &ph, patch)
	}, 30*time.Second, time.Second).Should(Succeed(),
		"stub Phase %s must accept the Status.Phase=Running stamp", name)
}

// waitForCloneComplete blocks until the Project's workspace clone finished
// (Status.Git.CloneComplete=true — set-on-success by the ProjectReconciler)
// and returns the stamped run branch name. The branch-writer Job needs
// repo.git + the run branch present on the PVC before it can fork task
// branches.
func waitForCloneComplete(projName string) string {
	GinkgoHelper()
	var branch string
	Eventually(func() error {
		var p tideprojectv1alpha3.Project
		if err := k8sClient.Get(ctx, client.ObjectKey{Name: projName, Namespace: integrationMissNamespace}, &p); err != nil {
			return err
		}
		if p.Status.Git.BranchName == "" {
			return fmt.Errorf("Project %s: run branch not yet stamped", projName)
		}
		if !p.Status.Git.CloneComplete {
			return fmt.Errorf("Project %s: clone not yet complete", projName)
		}
		branch = p.Status.Git.BranchName
		return nil
	}, 4*time.Minute, 2*time.Second).Should(Succeed(),
		"Project %s must stamp a run branch and finish its workspace clone", projName)
	return branch
}

// provisionTaskBranchesAndSignal runs a fixture Job (tide-push image —
// git-capable, chaos_resume release-writer + ancestry-assert Job pattern)
// that reproduces the real executor's observable git effects the stub
// subagent omits: for every task UID it forks tide/wt-<uid> from the run
// branch in a worktree at /workspace/worktrees/<uid> (the exact
// harness.EnsureWorktree layout) and adds one distinct commit
// (harness.CommitWorktree's effect), then touches the
// /workspace/envelopes/<uid>/release signal files so the wait-for-signal
// stub tasks report success — strictly AFTER their branches exist.
func provisionTaskBranchesAndSignal(jobName, projectUID, runBranch string, taskUIDs []string) {
	GinkgoHelper()

	// Branch-ownership split, decided per task off its IMMUTABLE dispatch
	// envelope (in.json, written by the controller before the pod starts):
	//   - envelope carries a "branch" key → the task won the BranchName race
	//     and the stub executor's git contract (ensureExecutorWorktree +
	//     commitExecutorWorktree) normally provisions tide/wt-<uid> — the
	//     writer MUST NOT race it (two `git worktree add`s on the same
	//     path). The writer polls up to 60s for the stub's branch ref: this
	//     Job runs after CloneComplete, so a stub still inside its bounded
	//     repo-wait exits it within one 2s poll — if the ref never appears
	//     the stub took its non-git fallback and the writer provisions.
	//   - no "branch" key (stub tasks dispatch within the same second the
	//     Project is created, usually beating the BranchName stamp) → the
	//     stub skips git entirely and the writer provisions immediately.
	// The "branch" key is omitempty, so key-presence == Branch was set.
	var sb strings.Builder
	// safe.directory is already system-level in the tide-push image; the
	// --global add is belt-and-braces and must never be fatal.
	sb.WriteString("set -e; export HOME=/tmp; git config --global --add safe.directory '*' || true; ")
	// Resolve the run branch FROM THE REPO, not from Project status: the two
	// must agree, but if they ever diverge (e.g. a re-stamped BranchName) the
	// repo ref is what the clone actually created — and the loud echo of
	// both values is the diagnostic for that divergence.
	fmt.Fprintf(&sb,
		"RB=$(git -C /workspace/repo.git for-each-ref --format='%%(refname:short)' 'refs/heads/tide/run-*' | head -1); "+
			"echo \"status-branch=%s repo-run-branch=$RB\"; "+
			"git -C /workspace/repo.git branch -a; "+
			"if [ -z \"$RB\" ]; then echo 'FATAL: no tide/run-* branch in repo.git'; exit 1; fi; ",
		runBranch)
	sb.WriteString("provision() { " +
		"git -C /workspace/repo.git worktree add /workspace/worktrees/$1 -b \"$2\" \"$RB\"; " +
		"echo \"stub task $1\" > /workspace/worktrees/$1/stub-task-$1.txt; " +
		"git -C /workspace/worktrees/$1 add -A; " +
		"git -C /workspace/worktrees/$1 -c user.name='TIDE Bot' -c user.email='tide-bot@tideproject.k8s' commit -m \"stub: task $1\"; }; ")
	for _, uid := range taskUIDs {
		branch := pkggit.TaskBranchName(uid)
		fmt.Fprintf(&sb,
			"if grep -q '\"branch\":' /workspace/envelopes/%[1]s/in.json; then "+
				"i=0; while [ $i -lt 30 ] && ! git -C /workspace/repo.git show-ref --verify --quiet 'refs/heads/%[2]s'; do i=$((i+1)); sleep 2; done; "+
				"if git -C /workspace/repo.git show-ref --verify --quiet 'refs/heads/%[2]s'; then "+
				"echo 'stub executor provisioned %[2]s'; else "+
				"echo 'stub fell back for task %[1]s; provisioning %[2]s'; provision %[1]s '%[2]s'; fi; "+
				"elif git -C /workspace/repo.git show-ref --verify --quiet 'refs/heads/%[2]s'; then "+
				"echo '%[2]s already provisioned (prior writer attempt); skipping'; else "+
				"provision %[1]s '%[2]s'; fi; ",
			uid, branch)
	}
	for _, uid := range taskUIDs {
		fmt.Fprintf(&sb, "mkdir -p /workspace/envelopes/%[1]s; touch /workspace/envelopes/%[1]s/release; ", uid)
	}
	sb.WriteString("echo 'branches + release signals provisioned'")

	ttl := int32(120)
	backoff := int32(2)
	// Worst case the ownership poll runs its full 60s per branch-key task
	// (stub fallback); 240s covers three tasks plus git work.
	deadline := int64(240)
	fsGroup := int64(1000)
	// uid 1000 — the SUBAGENT side of the two-uid PVC contract, same as
	// chaos_resume's release-writer. Run-5 CI evidence: as 65532 every git
	// step succeeded (repo.git is 65532-owned) but `touch .../release` hit
	// Permission denied — the envelope dirs are created subagent-side
	// (init container, uid 1000). The repo side is explicitly group-shared
	// for uid-1000 writers (tide-push makeWorkspaceGroupShared: chgrp 1000
	// + g+rwX + setgid at clone time), so 1000 satisfies BOTH sides.
	runAsUser := int64(1000)
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
				Name:    "branch-writer",
				Image:   integrationMissTidePushImage,
				Command: []string{"sh", "-c", sb.String()},
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
						SubPath:   fmt.Sprintf("%s/workspace", projectUID),
					},
				},
			},
		},
	}

	// Idempotent create: a writer Job may already exist under this deterministic
	// name (e.g. a prior BeforeEach in the same namespace). Delete it and recreate
	// rather than failing on AlreadyExists.
	if cErr := k8sClient.Create(ctx, job); cErr != nil {
		Expect(apierrors.IsAlreadyExists(cErr)).To(BeTrue(), "create branch-writer Job %s: %v", jobName, cErr)
		policy := metav1.DeletePropagationBackground
		Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, job, &client.DeleteOptions{PropagationPolicy: &policy}))).To(Succeed())
		Eventually(func() bool {
			var stale batchv1.Job
			return apierrors.IsNotFound(k8sClient.Get(ctx, client.ObjectKey{Name: jobName, Namespace: integrationMissNamespace}, &stale))
		}, time.Minute, 2*time.Second).Should(BeTrue(), "stale branch-writer Job must be gone before recreate")
		job.ResourceVersion = ""
		Expect(k8sClient.Create(ctx, job)).To(Succeed(), "recreate branch-writer Job %s", jobName)
	}

	Eventually(func() (bool, error) {
		var j batchv1.Job
		if err := k8sClient.Get(ctx, client.ObjectKey{Name: jobName, Namespace: integrationMissNamespace}, &j); err != nil {
			return false, err
		}
		if isJobSucceededShort(&j) {
			return true, nil
		}
		jobTerminallyFailed := false
		for _, c := range j.Status.Conditions {
			if c.Type == batchv1.JobFailed && c.Status == corev1.ConditionTrue {
				jobTerminallyFailed = true
			}
		}
		if jobTerminallyFailed {
			// Terminal failure — abort NOW with the pod logs in the failure
			// text (StopTrying, not a plain error: Gomega retries plain
			// errors until timeout and a later flake-attempt's message would
			// bury this one).
			logsCmd := exec.CommandContext(ctx, "kubectl", "--kubeconfig", kubeconfigPath,
				"logs", "job/"+jobName, "-n", integrationMissNamespace, "-c", "branch-writer", "--tail=50")
			logsOut, _ := logsCmd.CombinedOutput()
			return false, StopTrying(fmt.Sprintf("branch-writer Job %s terminally failed; logs (best-effort): %s", jobName, string(logsOut)))
		}
		return false, nil
	}, 5*time.Minute, 2*time.Second).Should(BeTrue(),
		"branch-writer Job must provision task branches + release signals")
}

// dumpNamespaceState prints the live TIDE CRD + Job + Pod + Event state of
// the namespace into the Ginkgo timeline — the post-mortem the kind-logs
// artifact cannot provide once the namespace is deleted. Best-effort: every
// kubectl error is printed rather than failing the (already-failed) spec.
func dumpNamespaceState(ns string) {
	// Fresh bounded context — NOT the suite ctx: the dump most often runs
	// precisely when kindTestTimeout has expired, and run 6's dump produced
	// six "context deadline exceeded" lines instead of state.
	dumpCtx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	GinkgoWriter.Printf("\n===== namespace state dump: %s =====\n", ns)
	for _, args := range [][]string{
		{"get", "project,milestone,phase,plan,task,wave", "-n", ns, "-o", "wide"},
		{"get", "project", "-n", ns, "-o", "yaml"},
		{"get", "plan", "-n", ns, "-o", "yaml"},
		{"get", "jobs", "-n", ns, "-o", "wide"},
		{"get", "pods", "-n", ns, "-o", "wide"},
		{"get", "events", "-n", ns, "--sort-by=.lastTimestamp"},
	} {
		cmd := exec.CommandContext(dumpCtx, "kubectl", append([]string{"--kubeconfig", kubeconfigPath}, args...)...)
		out, err := cmd.CombinedOutput()
		GinkgoWriter.Printf("--- kubectl %s ---\n%s", strings.Join(args, " "), string(out))
		if err != nil {
			GinkgoWriter.Printf("(kubectl error: %v)\n", err)
		}
	}
	GinkgoWriter.Printf("===== end namespace state dump: %s =====\n\n", ns)
}

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

	// projectUID (not GinkgoRandomSeed, which is shared across every spec in
	// the run) keeps this Job name unique per spec — both specs in this
	// Ordered container share a namespace and ran the same seed, so a
	// seed-only name collided with spec 1's Job on spec 2's run (AlreadyExists).
	// Truncated to 8 chars: the full 36-char UID would push the Job name to
	// 60 chars, and the Job-generated Pod name (Job name + "-" + 5 random
	// chars) would then exceed the 63-char Pod DNS-label limit.
	jobName := fmt.Sprintf("integration-miss-assert-%.8s", projectUID)
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
