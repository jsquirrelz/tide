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

// level_verify_worktree_test.go — Layer B kind integration spec for Plan
// 52-11 Task 1.
//
// Coverage: ESC-01 — proves the level-verify worktree-checkout init container
// (52-05's pkg/git.AddReadOnlyWorktree + podjob.buildWorktreeCheckoutContainer)
// against a REAL PVC + bare repo on a kind cluster, the one behavior envtest
// is structurally blind to (52-RESEARCH.md Pitfall 2; 52-VALIDATION.md's
// Manual-Only Verifications row). Phase/Milestone/Project verify dispatches
// never run an executor at their own UID, so — unlike a Task verifier, which
// inherits a worktree an executor already provisioned — this second init
// container is the ONLY thing that gives a level-verify Job a real checkout
// to run its gate command against. envtest's fake client cannot observe a
// real PersistentVolumeClaim or exec a real `git worktree add`, so 52-05
// deferred this proof here by design.
//
// Deliberately non-billable: this spec builds a verifier-shaped Job directly
// via podjob.BuildJobSpec (bypassing the TaskReconciler/level_verify.go
// dispatch path entirely — no Project/Milestone/Phase/Plan/Task CRDs are
// created) and overrides the MAIN subagent container to a trivial,
// credential-free command. The assertion target is exclusively the
// worktree-checkout INIT container; the main container's own (nonexistent)
// verdict is out of scope, matching 52-05-SUMMARY's own "live PVC proof
// deferred to 52-11" handoff. opts.Project is left nil, which skips
// credproxy injection entirely (BuildJobSpec gates it on
// opts.Project != nil && opts.Project.Spec.ProviderSecretRef != "") — no
// ANTHROPIC_API_KEY, no provider secret, no credproxy sidecar anywhere in
// this spec's Job.
//
// Fixture shape (custom, not the createProjectHierarchy/newStubProject
// pipeline other kind specs use — that pipeline drives a REAL clone Job and
// a real Task dispatch, neither of which this spec needs or wants):
//  1. seedBareRepoWithOneCommit: a plain git-CLI setup Job (tide-push:test
//     image — already git-capable and already loaded by
//     `make test-int-kind-prep`, RESEARCH's cheaper reuse option) that
//     `git init --bare`s /workspace/repo.git directly on the shared PVC and
//     pushes one commit on the run branch — no HTTP git-server fixture stack
//     needed (unlike medium_http/integration_miss's remote-backed specs).
//  2. buildLevelVerifyWorktreeProofJob: podjob.BuildJobSpec with
//     Kind=JobKindVerifier, a synthetic (never-persisted) Phase ParentObj,
//     Level="phase", WorktreeCheckoutImage=the loaded tide-push:test image,
//     WorktreeBranch=the seeded run branch — the exact composition
//     internal/controller/level_verify.go's dispatchLevelVerify uses, minus
//     the credential-bearing fields.
//  3. waitForInitContainerSuccess polls Pod.Status.InitContainerStatuses
//     (NOT Job.Status — a Job's own status never reports individual init
//     container outcomes) for the worktree-checkout container's Terminated
//     state.
//  4. assertWorktreeCheckedOutDetachedAtTip runs a follow-up read-only Job
//     (the assertBranchesAreAncestors/chaos_resume PVC-inspection Job
//     pattern) that asserts /workspace/worktrees/<uid>/ HEAD equals the
//     seeded tip SHA and `git branch --show-current` is empty (detached —
//     AddReadOnlyWorktree mints no branch).
//
// Every custom Job in this file runs as uid 1000 / gid 1000 / fsGroup 1000 —
// byte-identical to buildWorktreeCheckoutContainer's own SecurityContext
// (internal/dispatch/podjob/jobspec.go) — so the bare repo this spec seeds
// and the worktree-checkout container that reads/writes it never hit a
// cross-uid PVC permission mismatch (the exact hazard
// internal_miss_test.go's branch-writer doc comment calls out).

import (
	"fmt"
	"os/exec"
	"regexp"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	tideprojectv1alpha3 "github.com/jsquirrelz/tide/api/v1alpha3"
	"github.com/jsquirrelz/tide/internal/dispatch/podjob"
)

const (
	// levelVerifyWorktreeNS is the dedicated test namespace for this spec.
	levelVerifyWorktreeNS = "level-verify-worktree-test"

	// levelVerifyWorktreeTidePushImage is the git-capable image both the
	// bare-repo seed Job and the worktree-checkout init container run —
	// already built/loaded by `make test-int-kind-prep` for every other
	// Layer B spec that dispatches push/clone/worktree Jobs.
	levelVerifyWorktreeTidePushImage = "ghcr.io/jsquirrelz/tide-push:test"

	// levelVerifyWorktreeMainImage is the verifier Job's MAIN subagent
	// container image. busybox:stable is already relied on unconditionally
	// by every kind-dispatched Job's envelope-writer init container
	// (internal/dispatch/podjob/jobspec.go), so no new image-load plumbing
	// is needed to run a trivial "echo; exit 0" in it.
	levelVerifyWorktreeMainImage = "busybox:stable"

	// levelVerifyWorktreeProjectUID is a synthetic (never a real Project CRD)
	// UID used purely as the PVC subPath prefix (<uid>/workspace) — this
	// spec drives podjob.BuildJobSpec directly and never creates a Project.
	levelVerifyWorktreeProjectUID = "52-11-synthetic-project-uid"

	// levelVerifyWorktreeParentUID is the synthetic Phase ParentObj's UID —
	// AddReadOnlyWorktree keys the checkout directory
	// (/workspace/worktrees/<uid>/) by this value.
	levelVerifyWorktreeParentUID = "52-11-synthetic-phase-uid"

	// levelVerifyWorktreeRunBranch is the run branch seeded with one commit
	// and later checked out (detached) by the worktree-checkout container.
	levelVerifyWorktreeRunBranch = "tide/run-52-11-worktree-proof"
)

// seedSHARegexp extracts the seeded tip commit SHA from the seed Job's log
// output ("SEED_SHA=<40-hex-char sha>").
var seedSHARegexp = regexp.MustCompile(`SEED_SHA=([0-9a-f]{40})`)

var _ = Describe("Level-verify worktree-checkout init container provisions a real worktree from a real PVC (ESC-01)", Label("kind"), func() {
	BeforeEach(func() {
		skipIfCRDsOnlyMode()
	})

	AfterEach(func() {
		deleteNamespace(levelVerifyWorktreeNS)
		if CurrentSpecReport().Failed() {
			exportKindLogs()
		}
	})

	It("provisions a detached worktree at the seeded run-branch tip with no ANTHROPIC key required", func() {
		ns := levelVerifyWorktreeNS

		By("Creating namespace + tide-projects PVC + tide-subagent SA (no Project/Task CRDs — this spec drives podjob.BuildJobSpec directly)")
		createNamespace(ns)

		By("Seeding a bare repo with one commit on the run branch via a setup Job")
		tipSHA := seedBareRepoWithOneCommit(ns, levelVerifyWorktreeProjectUID, levelVerifyWorktreeRunBranch)

		By("Building a verifier-shaped Job via podjob.BuildJobSpec (Kind=JobKindVerifier, no credproxy, no ANTHROPIC key)")
		job := buildLevelVerifyWorktreeProofJob(ns)
		Expect(k8sClient.Create(ctx, job)).To(Succeed(), "create level-verify worktree-proof Job")

		By("Waiting for the worktree-checkout init container to terminate with exit 0")
		waitForInitContainerSuccess(ns, job.Name, "worktree-checkout")

		By("Asserting /workspace/worktrees/<uid>/ HEAD equals the seeded tip SHA and is detached (no branch minted)")
		assertWorktreeCheckedOutDetachedAtTip(ns, levelVerifyWorktreeProjectUID, levelVerifyWorktreeParentUID, tipSHA)

		GinkgoWriter.Println("ESC-01: level-verify worktree-checkout init container proven on a real PVC, non-billable")
	})
})

// ---- helpers (file-local) ----

// levelVerifyWorktreeJobSecurityContext returns the uid 1000 / gid 1000 /
// non-root / no-priv-escalation / drop-ALL SecurityContext every custom Job
// container in this file runs with — byte-identical to
// buildWorktreeCheckoutContainer's own container SecurityContext
// (internal/dispatch/podjob/jobspec.go) so ownership never mismatches across
// the seed/checkout/assert Jobs that all touch the same PVC subtree.
func levelVerifyWorktreeJobSecurityContext() *corev1.SecurityContext {
	return &corev1.SecurityContext{
		RunAsUser:                new(int64(1000)),
		RunAsGroup:               new(int64(1000)),
		RunAsNonRoot:             new(true),
		AllowPrivilegeEscalation: new(false),
		Capabilities:             &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}},
	}
}

// levelVerifyWorktreePodSecurityContext returns the matching Pod-level
// SecurityContext (FSGroup + RunAsUser + RunAsGroup all 1000 — mirrors
// BuildJobSpec's own PodSpec.SecurityContext).
func levelVerifyWorktreePodSecurityContext() *corev1.PodSecurityContext {
	return &corev1.PodSecurityContext{
		FSGroup:    new(int64(1000)),
		RunAsUser:  new(int64(1000)),
		RunAsGroup: new(int64(1000)),
	}
}

// seedBareRepoWithOneCommit runs a setup Job that `git init --bare`s
// /workspace/repo.git directly on the shared PVC (subPath
// <projectUID>/workspace — the same subPath podjob.BuildJobSpec computes
// from opts.ProjectUID) and pushes one commit on runBranch via git's local
// filesystem transport (no network, no HTTP git-server fixture stack).
// Returns the seeded tip commit's SHA, parsed from the Job's own log output.
func seedBareRepoWithOneCommit(ns, projectUID, runBranch string) string {
	GinkgoHelper()

	const jobName = "level-verify-worktree-seed"
	subPath := projectUID + "/workspace"

	var sb strings.Builder
	sb.WriteString("set -e; export HOME=/tmp; git config --global --add safe.directory '*' || true; ")
	sb.WriteString("git init --quiet --bare /workspace/repo.git; ")
	sb.WriteString("mkdir -p /tmp/seed && cd /tmp/seed && git init --quiet; ")
	fmt.Fprintf(&sb, "git checkout --quiet -b %s; ", runBranch)
	sb.WriteString("echo 'level-verify worktree proof seed' > seed.txt && git add -A && ")
	sb.WriteString("git -c user.name='TIDE Test' -c user.email='tide-test@tideproject.k8s' commit --quiet -m 'seed: one commit on the run branch'; ")
	fmt.Fprintf(&sb, "git push --quiet /workspace/repo.git %s; ", runBranch)
	sb.WriteString("echo \"SEED_SHA=$(git rev-parse HEAD)\"")

	ttl := int32(120)
	backoff := int32(0)
	deadline := int64(120)

	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{Name: jobName, Namespace: ns},
		Spec: batchv1.JobSpec{
			BackoffLimit:            &backoff,
			TTLSecondsAfterFinished: &ttl,
			ActiveDeadlineSeconds:   &deadline,
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					RestartPolicy:      corev1.RestartPolicyNever,
					ServiceAccountName: podjob.ServiceAccountSubagent,
					SecurityContext:    levelVerifyWorktreePodSecurityContext(),
					Volumes: []corev1.Volume{
						{
							Name: podjob.VolumeProjectWorkspace,
							VolumeSource: corev1.VolumeSource{
								PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
									ClaimName: "tide-projects",
								},
							},
						},
					},
					Containers: []corev1.Container{
						{
							Name:            "seed",
							Image:           levelVerifyWorktreeTidePushImage,
							Command:         []string{"sh", "-c", sb.String()},
							SecurityContext: levelVerifyWorktreeJobSecurityContext(),
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      podjob.VolumeProjectWorkspace,
									MountPath: "/workspace",
									SubPath:   subPath,
								},
							},
						},
					},
				},
			},
		},
	}

	Expect(k8sClient.Create(ctx, job)).To(Succeed(), "create bare-repo seed Job")

	var seedSHA string
	Eventually(func() (bool, error) {
		var j batchv1.Job
		if err := k8sClient.Get(ctx, client.ObjectKey{Name: jobName, Namespace: ns}, &j); err != nil {
			return false, err
		}
		if isJobSucceededShort(&j) {
			logsCmd := exec.CommandContext(ctx, "kubectl", "--kubeconfig", kubeconfigPath,
				"logs", "job/"+jobName, "-n", ns, "-c", "seed", "--tail=20")
			out, lErr := logsCmd.CombinedOutput()
			if lErr != nil {
				return false, fmt.Errorf("reading seed Job logs: %w", lErr)
			}
			m := seedSHARegexp.FindStringSubmatch(string(out))
			if len(m) != 2 {
				return false, StopTrying(fmt.Sprintf("seed Job logs missing SEED_SHA=<sha> line: %s", string(out)))
			}
			seedSHA = m[1]
			return true, nil
		}
		for _, c := range j.Status.Conditions {
			if c.Type == batchv1.JobFailed && c.Status == corev1.ConditionTrue {
				logsCmd := exec.CommandContext(ctx, "kubectl", "--kubeconfig", kubeconfigPath,
					"logs", "job/"+jobName, "-n", ns, "-c", "seed", "--tail=50")
				logsOut, _ := logsCmd.CombinedOutput()
				return false, StopTrying(fmt.Sprintf("bare-repo seed Job failed; logs: %s", string(logsOut)))
			}
		}
		return false, nil
	}, 2*time.Minute, 2*time.Second).Should(BeTrue(), "bare-repo seed Job must complete and print SEED_SHA")

	Expect(seedSHA).NotTo(BeEmpty(), "seed Job must have printed a SEED_SHA")
	return seedSHA
}

// buildLevelVerifyWorktreeProofJob builds a verifier-shaped Job via
// podjob.BuildJobSpec — the exact composition
// internal/controller/level_verify.go's dispatchLevelVerify uses (Kind=
// JobKindVerifier, ParentObj/Level driving the verifier Job name + labels,
// WorktreeCheckoutImage+WorktreeBranch composing the second init container)
// — minus every credential-bearing field. opts.Project is left nil, which
// BuildJobSpec's own credproxyEnabled gate (opts.Project != nil &&
// opts.Project.Spec.ProviderSecretRef != "") reads as "skip credproxy
// entirely": no ANTHROPIC_API_KEY, no provider secret, no sidecar. The MAIN
// subagent container is then overridden to a trivial, credential-free
// command — this spec's assertion target is exclusively the
// worktree-checkout INIT container, never the (nonexistent) verifier
// verdict.
func buildLevelVerifyWorktreeProofJob(ns string) *batchv1.Job {
	// A synthetic, never-persisted Phase — only its UID/Namespace are read
	// by BuildJobSpec (ParentObj.GetUID()/GetNamespace()). Using a real CRD
	// type (rather than an arbitrary corev1 object) keeps Level="phase"
	// thematically honest without requiring an actual Phase to exist.
	parentObj := &tideprojectv1alpha3.Phase{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "synthetic-phase",
			Namespace: ns,
			UID:       types.UID(levelVerifyWorktreeParentUID),
		},
	}

	job := podjob.BuildJobSpec(podjob.BuildOptions{
		Kind:                  podjob.JobKindVerifier,
		ParentObj:             parentObj,
		Level:                 "phase",
		Attempt:               1,
		EnvelopeInJSON:        []byte("{}"),
		SubagentImage:         levelVerifyWorktreeMainImage,
		PVCName:               "tide-projects",
		ProjectUID:            levelVerifyWorktreeProjectUID,
		ReadOnly:              true,
		WorktreeCheckoutImage: levelVerifyWorktreeTidePushImage,
		WorktreeBranch:        levelVerifyWorktreeRunBranch,
	})

	for i := range job.Spec.Template.Spec.Containers {
		if job.Spec.Template.Spec.Containers[i].Name != podjob.ContainerNameSubagent {
			continue
		}
		job.Spec.Template.Spec.Containers[i].Command = []string{"sh", "-c"}
		job.Spec.Template.Spec.Containers[i].Args = []string{"echo main-container-stub-ok; exit 0"}
	}

	return job
}

// waitForInitContainerSuccess polls the Job's Pod (selector job-name=<jobName>
// — the label the Job controller itself stamps, mirrors
// internal/controller/git_writer.go's own List) for containerName's
// InitContainerStatuses entry reaching Terminated with exit code 0. A Job's
// own Status never reports individual init container outcomes, so this must
// read the Pod directly.
func waitForInitContainerSuccess(ns, jobName, containerName string) {
	GinkgoHelper()

	Eventually(func() (bool, error) {
		var pods corev1.PodList
		if err := k8sClient.List(ctx, &pods, client.InNamespace(ns), client.MatchingLabels{"job-name": jobName}); err != nil {
			return false, err
		}
		if len(pods.Items) == 0 {
			return false, nil
		}
		pod := pods.Items[0]
		for i := range pod.Status.InitContainerStatuses {
			ics := pod.Status.InitContainerStatuses[i]
			if ics.Name != containerName {
				continue
			}
			if ics.State.Terminated == nil {
				return false, nil
			}
			if ics.State.Terminated.ExitCode == 0 {
				return true, nil
			}
			logsCmd := exec.CommandContext(ctx, "kubectl", "--kubeconfig", kubeconfigPath,
				"logs", pod.Name, "-n", ns, "-c", containerName, "--tail=80")
			logsOut, _ := logsCmd.CombinedOutput()
			return false, StopTrying(fmt.Sprintf(
				"%s init container exited %d (pod %s); logs: %s",
				containerName, ics.State.Terminated.ExitCode, pod.Name, string(logsOut)))
		}
		return false, nil
	}, 3*time.Minute, 3*time.Second).Should(BeTrue(),
		"%s init container must terminate with exit 0 on a real PVC", containerName)
}

// assertWorktreeCheckedOutDetachedAtTip runs a follow-up read-only Job (the
// assertBranchesAreAncestors/chaos_resume PVC-inspection Job pattern) that
// asserts /workspace/worktrees/<uid>/ (the exact directory
// pkg/git.AddReadOnlyWorktree provisions, keyed by the verifier Job's
// ParentObj UID) has HEAD equal to the seeded tip SHA and
// `git branch --show-current` empty (detached — AddReadOnlyWorktree's
// `git worktree add --detach` mints no branch).
func assertWorktreeCheckedOutDetachedAtTip(ns, projectUID, uid, expectedSHA string) {
	GinkgoHelper()

	const jobName = "level-verify-worktree-assert"
	subPath := projectUID + "/workspace"

	script := fmt.Sprintf(`set -e
WT=/workspace/worktrees/%s
if [ ! -d "$WT" ]; then echo "MISSING: worktree dir $WT does not exist"; exit 1; fi
cd "$WT"
HEAD_SHA=$(git rev-parse HEAD)
BRANCH=$(git branch --show-current)
echo "HEAD_SHA=$HEAD_SHA BRANCH=[$BRANCH]"
if [ "$HEAD_SHA" != "%s" ]; then
  echo "MISMATCH: worktree HEAD $HEAD_SHA != seeded tip %s"
  exit 1
fi
if [ -n "$BRANCH" ]; then
  echo "NOT DETACHED: worktree is on branch '$BRANCH', want detached (empty)"
  exit 1
fi
echo "worktree checked out detached at the seeded tip -- assertions passed"
`, uid, expectedSHA, expectedSHA)

	ttl := int32(60)
	backoff := int32(0)
	deadline := int64(90)

	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{Name: jobName, Namespace: ns},
		Spec: batchv1.JobSpec{
			BackoffLimit:            &backoff,
			TTLSecondsAfterFinished: &ttl,
			ActiveDeadlineSeconds:   &deadline,
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					RestartPolicy:      corev1.RestartPolicyNever,
					ServiceAccountName: podjob.ServiceAccountSubagent,
					SecurityContext:    levelVerifyWorktreePodSecurityContext(),
					Volumes: []corev1.Volume{
						{
							Name: podjob.VolumeProjectWorkspace,
							VolumeSource: corev1.VolumeSource{
								PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
									ClaimName: "tide-projects",
								},
							},
						},
					},
					Containers: []corev1.Container{
						{
							Name:            "assert",
							Image:           levelVerifyWorktreeTidePushImage,
							Command:         []string{"sh", "-c", script},
							SecurityContext: levelVerifyWorktreeJobSecurityContext(),
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      podjob.VolumeProjectWorkspace,
									MountPath: "/workspace",
									SubPath:   subPath,
								},
							},
						},
					},
				},
			},
		},
	}

	Expect(k8sClient.Create(ctx, job)).To(Succeed(), "create worktree-assert Job")

	Eventually(func() (bool, error) {
		var j batchv1.Job
		if err := k8sClient.Get(ctx, client.ObjectKey{Name: jobName, Namespace: ns}, &j); err != nil {
			return false, err
		}
		if isJobSucceededShort(&j) {
			return true, nil
		}
		for _, c := range j.Status.Conditions {
			if c.Type == batchv1.JobFailed && c.Status == corev1.ConditionTrue {
				logsCmd := exec.CommandContext(ctx, "kubectl", "--kubeconfig", kubeconfigPath,
					"logs", "job/"+jobName, "-n", ns, "-c", "assert", "--tail=50")
				logsOut, _ := logsCmd.CombinedOutput()
				return false, StopTrying(fmt.Sprintf("worktree-assert Job failed; logs: %s", string(logsOut)))
			}
		}
		return false, nil
	}, 2*time.Minute, 2*time.Second).Should(BeTrue(),
		"worktree-checkout must produce a detached checkout at the seeded run-branch tip")
}
