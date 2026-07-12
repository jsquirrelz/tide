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

// chaos_resume_test.go — Layer B kind integration spec for plan 03-10.
//
// Coverage: PERSIST-04 / TEST-04 / D-D1..D-D4 + algorithmic invariant (Pillar 5).
//
// Property under test: "resumption state = indegree map + completed-task set,
// nothing more" (README spec, §Failure handling at wave boundaries).
//
// Test fixture: testdata/chaos-resume-three-task.yaml — three Tasks with mixed
// pre-kill lifecycle states (α Succeeded, β + γ Running via wait-for-signal mode).
// The test kills the controller pod mid-wave, waits for leader handoff, then
// asserts the D-D4 four-pillar invariants hold across the kill — PLUS Pillar 5,
// the algorithmic invariant that pkg/dag.ComputeWaves re-derives the same wave
// structure post-restart (proof that no schedule was cached on disk).
//
// CONTEXT.md <specifics> #5 — chaos-resume is framed as a SINGLE load-bearing
// integration test (one Ginkgo It block), but the It block is split into 5
// named By("Pillar N: ...") subtests so a failure surfaces by pillar name
// (Phase 02.2 cascade lesson #4 — localized failure attribution cuts iterations).
//
// Wall budget: ≤ 90s per Phase 3 RESEARCH note (lease window ~15s + 30s buffer
// + 45s pre-kill + 30s post-release).
//
// Regenerating the Pillar 5 golden file: set GENERATE_GOLDEN=1 in the env
// before running the spec; the test writes the pre-kill ComputeWaves output
// to testdata/chaos-resume-waves.golden.json and skips the post-kill
// comparison. Use only when the fixture's Task DAG legitimately changes.

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	batchv1 "k8s.io/api/batch/v1"
	coordinationv1 "k8s.io/api/coordination/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	tideprojectv1alpha3 "github.com/jsquirrelz/tide/api/v1alpha3"
	"github.com/jsquirrelz/tide/pkg/dag"
)

const (
	// chaosResumeNS is the namespace defined by the fixture.
	chaosResumeNS = "chaos-resume-test"

	// chaosLeaseName is the controller-runtime leader-election Lease name
	// (matches cmd/manager/main.go:LeaderElectionID = "tide-controller-leader.tideproject.k8s").
	chaosLeaseName = "tide-controller-leader.tideproject.k8s"

	// chaosLeaseNamespace is the namespace controller-runtime defaults the
	// Lease into when LeaderElectionNamespace is unset (= the pod's own
	// namespace via /var/run/secrets/kubernetes.io/serviceaccount/namespace
	// in-cluster). The Helm chart deploys the manager into tide-system, so
	// the Lease lands there.
	chaosLeaseNamespace = kindControllerNamespace

	// chaosControllerSelector targets the controller-manager pod for kill.
	// Chart deployment.yaml template labels the Pod `control-plane=controller-manager`
	// (charts/tide/templates/deployment.yaml lines 17-19).
	chaosControllerSelector = "control-plane=controller-manager"

	// chaosWavesGoldenRel is the Pillar 5 algorithmic-invariant snapshot.
	chaosWavesGoldenRel = "testdata/chaos-resume-waves.golden.json"
)

// chaosTaskSnapshot captures the per-Task observation set used to assert the
// D-D4 pillars across a controller-pod kill. Snapshots are compared
// pre-kill vs. post-restart.
type chaosTaskSnapshot struct {
	TaskName    string
	TaskUID     string
	JobName     string
	JobUID      string
	Phase       string
	Attempt     int
	CompletedAt string // RFC3339; empty if !Succeeded
}

var _ = Describe("Chaos-resume: kill controller pod mid-wave (PERSIST-04 / TEST-04 / D-D4)", Label("kind"), func() {
	BeforeEach(func() {
		skipIfCRDsOnlyMode()
		// chaos-resume needs the controller deployment to be live for the
		// leader-handoff property to even be testable — skip otherwise.
		if !chaosControllerDeploymentLive() {
			Skip("chaos-resume requires a live controller deployment; CRDs-only mode skipped")
		}
	})

	AfterEach(func() {
		deleteNamespace(chaosResumeNS)
		if CurrentSpecReport().Failed() {
			exportKindLogs()
		}
	})

	// SINGLE It block per CONTEXT.md <specifics> #5 (single load-bearing claim)
	// — but split into 5 By("Pillar N: ...") subtests per W12 fix for localized
	// failure attribution.
	It("D-D4 four pillars + algorithmic invariant hold across controller kill", func() {
		By("Ensure namespace-local SA + signing-key Secret (Phase 04.1 P12 iter-4 Cascade 6)")
		// chaos-resume-test fixture YAML creates the Namespace/PVC/provider-Secret
		// inline but NOT the tide-subagent ServiceAccount or the helm-mirrored
		// tide-signing-key Secret. Without the SA, kubelet rejects Pod creation
		// with `serviceaccount "tide-subagent" not found`; without the signing
		// key, credproxy init container CreateContainerConfigError's. Both must
		// land in chaos-resume-test before applyFile so the Tasks actually run.
		// createNamespace() is idempotent (kubectl apply tolerates re-creates)
		// and bundles ensureSubagentSA + ensureProjectsPVC + ensureSigningKeySecret.
		createNamespace(chaosResumeNS)

		By("Apply fixture: 3-task mixed-state Plan (α=success, β/γ=wait-for-signal)")
		Expect(applyFile(filepath.Join("testdata", "chaos-resume-three-task.yaml"))).To(Succeed(),
			"chaos-resume fixture must apply cleanly")

		By("Wait for pre-kill lifecycle states: α=Succeeded, β=Running, γ=Running")
		// α resolves fastest (testMode=success); β starts as soon as Wave 0
		// dispatches; γ starts after α succeeds (depends on α).
		waitForChaosTaskPhase("alpha-chaos", "Succeeded", 4*time.Minute)
		waitForChaosTaskPhase("beta-chaos", "Running", 90*time.Second)
		waitForChaosTaskPhase("gamma-chaos", "Running", 4*time.Minute)

		By("Snapshot pre-kill state for the 3 Tasks")
		preKill := snapshotChaosTasks()
		Expect(preKill).To(HaveLen(3), "pre-kill snapshot must contain all 3 Tasks")
		Expect(preKill["alpha-chaos"].Phase).To(Equal("Succeeded"))
		Expect(preKill["beta-chaos"].Phase).To(Equal("Running"))
		Expect(preKill["gamma-chaos"].Phase).To(Equal("Running"))
		Expect(preKill["beta-chaos"].JobUID).NotTo(BeEmpty(), "beta-chaos must have a Job pre-kill")
		Expect(preKill["gamma-chaos"].JobUID).NotTo(BeEmpty(), "gamma-chaos must have a Job pre-kill")

		// Capture pre-kill Lease holder for handoff assertion.
		preKillHolder := readChaosLeaseHolder()
		Expect(preKillHolder).NotTo(BeEmpty(), "controller must hold the lease pre-kill")
		GinkgoWriter.Printf("Pre-kill lease holder: %s\n", preKillHolder)

		// Pillar 5 — Generate golden mode (escape hatch for legitimate DAG changes).
		if os.Getenv("GENERATE_GOLDEN") == "1" {
			By("GENERATE_GOLDEN=1: regenerating chaos-resume-waves.golden.json and exiting")
			writeChaosGolden(preKill)
			Skip("GENERATE_GOLDEN=1 — golden file regenerated; skipping post-kill comparison")
		}

		By("Kill controller pod (kubectl delete pod -l " + chaosControllerSelector + ")")
		killChaosControllerPod()

		By("Wait for leader handoff — new pod acquires the lease (max 60s, default lease=15s)")
		waitForChaosLeaseHandoff(preKillHolder, 60*time.Second)

		By("Snapshot post-restart state for the 3 Tasks")
		postKill := snapshotChaosTasks()
		Expect(postKill).To(HaveLen(3), "post-restart snapshot must contain all 3 Tasks")

		// Pillar 1: Job UID continuity for in-flight Tasks β and γ.
		By("Pillar 1: Job UID continuity for in-flight Tasks (β, γ unchanged across kill)")
		Expect(postKill["beta-chaos"].JobUID).To(Equal(preKill["beta-chaos"].JobUID),
			"Pillar 1: beta-chaos Job UID must be IDENTICAL pre/post-kill (no re-creation)")
		Expect(postKill["gamma-chaos"].JobUID).To(Equal(preKill["gamma-chaos"].JobUID),
			"Pillar 1: gamma-chaos Job UID must be IDENTICAL pre/post-kill (no re-creation)")

		// Pillar 2: Task.Status.Attempt unchanged across kill.
		By("Pillar 2: Task.Status.Attempt unchanged across kill (no spurious retry)")
		for _, name := range []string{"alpha-chaos", "beta-chaos", "gamma-chaos"} {
			Expect(postKill[name].Attempt).To(Equal(preKill[name].Attempt),
				"Pillar 2: %s Task.Status.Attempt must be unchanged pre/post-kill (was %d)",
				name, preKill[name].Attempt)
		}

		// Pillar 3: Completed-set preserved (α stays Succeeded with same CompletedAt).
		By("Pillar 3: Completed-set preserved (α stays Succeeded with same CompletedAt)")
		Expect(postKill["alpha-chaos"].Phase).To(Equal("Succeeded"),
			"Pillar 3: alpha-chaos must remain Succeeded post-restart")
		Expect(postKill["alpha-chaos"].CompletedAt).To(Equal(preKill["alpha-chaos"].CompletedAt),
			"Pillar 3: alpha-chaos.Status.CompletedAt must be unchanged across kill")

		// Pillar 5: Algorithmic invariant — ComputeWaves matches golden.
		// Executed BEFORE Pillar 4 because Pillar 4 mutates state (releases β/γ);
		// Pillar 5 is a pure read against the indegree map / Task DAG and the
		// invariant is precisely that the schedule is re-derived, not cached.
		By("Pillar 5: Algorithmic invariant — pkg/dag.ComputeWaves post-restart matches golden")
		assertChaosWavesGoldenMatch()

		// Pillar 4: Observed completion across kill — write release files, then
		// wait for β + γ to reach Succeeded and the Wave to follow.
		By("Pillar 4: Observed completion across kill — release β + γ, both reach Succeeded")
		writeChaosReleaseSignals(preKill["beta-chaos"], preKill["gamma-chaos"])

		// Eventually β + γ reach Succeeded post-release.
		waitForChaosTaskPhase("beta-chaos", "Succeeded", 4*time.Minute)
		waitForChaosTaskPhase("gamma-chaos", "Succeeded", 4*time.Minute)

		// Exactly 3 executor Jobs (α, β, γ) must succeed; non-task Jobs (init,
		// planners, release-writer) are excluded by the role=executor label filter
		// per .planning/debug/chaos-resume-cascade-10.md (Resolution, lines 282–294).
		Eventually(func() int {
			jobs := &batchv1.JobList{}
			_ = k8sClient.List(ctx, jobs, client.InNamespace(chaosResumeNS),
				client.MatchingLabels{"tideproject.k8s/role": "executor"})
			succeeded := 0
			for _, j := range jobs.Items {
				if isJobSucceededShort(&j) {
					succeeded++
				}
			}
			return succeeded
		}, 2*time.Minute, 3*time.Second).Should(Equal(3),
			"Pillar 4: exactly 3 Jobs must reach status.succeeded=1 post-release")

		GinkgoWriter.Println("chaos-resume: all 5 pillars (D-D4 + algorithmic invariant) verified across leader-handoff")
	})
})

// ---- helpers (file-local; avoid colliding with existing exported helpers) ----

// chaosControllerDeploymentLive returns true when the tide-controller-manager
// Deployment exists in the kind cluster. chaos-resume requires the controller
// to be live; CRDs-only mode skips.
func chaosControllerDeploymentLive() bool {
	cmd := exec.CommandContext(ctx, "kubectl", "--kubeconfig", kubeconfigPath,
		"get", "deployment", kindControllerDeployment,
		"-n", kindControllerNamespace, "--ignore-not-found=true",
		"--output=name")
	out, err := cmd.Output()
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(out)) != ""
}

// waitForChaosTaskPhase blocks until the named Task in chaosResumeNS reaches
// the given Status.Phase, or fails the spec on timeout.
func waitForChaosTaskPhase(name, want string, timeout time.Duration) {
	Eventually(func() string {
		t := &tideprojectv1alpha3.Task{}
		if err := k8sClient.Get(ctx, client.ObjectKey{Name: name, Namespace: chaosResumeNS}, t); err != nil {
			return ""
		}
		return t.Status.Phase
	}, timeout, 2*time.Second).Should(Equal(want),
		"Task %s should reach Status.Phase=%s within %s", name, want, timeout)
}

// snapshotChaosTasks reads the three chaos-resume Tasks and their owning Jobs
// (matched by label tideproject.k8s/task-uid=<task.UID>) and returns a
// keyed-by-name snapshot map. Tasks that lack a Job (e.g., α post-success may
// have its Job GC'd by TTLSecondsAfterFinished) are tolerated — JobUID stays
// empty but the Task fields are populated.
func snapshotChaosTasks() map[string]chaosTaskSnapshot {
	snap := make(map[string]chaosTaskSnapshot, 3)
	jobs := &batchv1.JobList{}
	if err := k8sClient.List(ctx, jobs, client.InNamespace(chaosResumeNS)); err != nil {
		Fail(fmt.Sprintf("snapshotChaosTasks: list jobs: %v", err))
	}
	for _, name := range []string{"alpha-chaos", "beta-chaos", "gamma-chaos"} {
		t := &tideprojectv1alpha3.Task{}
		if err := k8sClient.Get(ctx, client.ObjectKey{Name: name, Namespace: chaosResumeNS}, t); err != nil {
			Fail(fmt.Sprintf("snapshotChaosTasks: get Task %s: %v", name, err))
		}
		entry := chaosTaskSnapshot{
			TaskName: name,
			TaskUID:  string(t.UID),
			Phase:    t.Status.Phase,
			Attempt:  t.Status.Attempt,
		}
		if t.Status.CompletedAt != nil {
			entry.CompletedAt = t.Status.CompletedAt.UTC().Format(time.RFC3339Nano)
		}
		// Find the Job whose tideproject.k8s/task-uid label matches.
		for _, j := range jobs.Items {
			if j.Labels["tideproject.k8s/task-uid"] == string(t.UID) {
				entry.JobName = j.Name
				entry.JobUID = string(j.UID)
				break
			}
		}
		snap[name] = entry
	}
	return snap
}

// readChaosLeaseHolder returns the current Lease.Spec.HolderIdentity for the
// controller-runtime leader-election lease, or "" if absent.
func readChaosLeaseHolder() string {
	var lease coordinationv1.Lease
	if err := k8sClient.Get(ctx, types.NamespacedName{
		Name: chaosLeaseName, Namespace: chaosLeaseNamespace,
	}, &lease); err != nil {
		return ""
	}
	if lease.Spec.HolderIdentity == nil {
		return ""
	}
	return *lease.Spec.HolderIdentity
}

// killChaosControllerPod runs `kubectl delete pod -l control-plane=controller-manager
// -n tide-system` to force a leader handoff. Uses a fresh context (not the
// suite's ctx, which may carry a deadline) so the delete completes before any
// outer timeout cancels it.
func killChaosControllerPod() {
	cmd := exec.CommandContext(context.Background(), "kubectl", "--kubeconfig", kubeconfigPath,
		"delete", "pod", "-l", chaosControllerSelector,
		"-n", kindControllerNamespace, "--wait=false", "--timeout=30s")
	out, err := cmd.CombinedOutput()
	if err != nil {
		Fail(fmt.Sprintf("kubectl delete pod failed: %v\n%s", err, out))
	}
	GinkgoWriter.Printf("Killed controller pod(s):\n%s\n", out)
}

// waitForChaosLeaseHandoff blocks until the lease HolderIdentity changes from
// `previous`, or fails the spec on timeout. Lease defaults: 15s LeaseDuration,
// 10s RenewDeadline, 2s RetryPeriod (charts/tide/values.yaml +
// internal/config/env.go); empirical handoff window ~15-25s on kind.
func waitForChaosLeaseHandoff(previous string, timeout time.Duration) {
	Eventually(func() string {
		h := readChaosLeaseHolder()
		if h == "" {
			return previous // still empty / transient — keep waiting
		}
		return h
	}, timeout, time.Second).ShouldNot(Equal(previous),
		"lease HolderIdentity should change after controller-pod kill (previous=%s)", previous)
	GinkgoWriter.Printf("Post-kill lease holder: %s\n", readChaosLeaseHolder())
}

// writeChaosReleaseSignals writes the per-Task release files under the shared
// PVC so the wait-for-signal stub-subagent harness observes them and emits
// the canned success envelope (cmd/stub-subagent/main.go:dispatchWaitForSignal).
//
// Path layout: per Phase 02.2 D-G2 PVC subPath, the controller's init Job
// MkdirAll's /workspace/envelopes when initializing each Project's slice
// at subPath=<project.UID>/workspace. The Task Job pod mounts the same PVC
// with subPath=<project.UID>/workspace + a per-Task envelopes/{task-uid}/
// directory created by Phase 2 PodStatusEnvelopeReader infrastructure.
//
// We touch the release signal via `kubectl exec` into a tide-projects-mounted
// busybox Pod we spin up just for this purpose. The signal path inside the
// Pod's /workspace is /workspace/envelopes/{task-uid}/release.
func writeChaosReleaseSignals(beta, gamma chaosTaskSnapshot) {
	// Resolve the Project UID — the PVC subPath for chaos-resume.
	var proj tideprojectv1alpha3.Project
	Expect(k8sClient.Get(ctx, client.ObjectKey{
		Name: "chaos-resume-project", Namespace: chaosResumeNS,
	}, &proj)).To(Succeed(), "chaos-resume-project must exist for release-signal write")
	projectUID := string(proj.UID)

	writerJobName := "chaos-resume-release-writer"
	subPath := fmt.Sprintf("%s/workspace", projectUID)

	// Inline busybox Job that touches the two release signal files and exits.
	// MkdirAll first because stub-subagent only creates envelopes/{task-uid}/
	// on first run AFTER its own out.json write — pre-release the parent dir
	// may not exist if the manager-side reader is still PodStatus-driven.
	signalScript := fmt.Sprintf(
		"set -e; mkdir -p /workspace/envelopes/%s /workspace/envelopes/%s; "+
			"touch /workspace/envelopes/%s/release /workspace/envelopes/%s/release; "+
			"ls -la /workspace/envelopes/%s/release /workspace/envelopes/%s/release",
		beta.TaskUID, gamma.TaskUID,
		beta.TaskUID, gamma.TaskUID,
		beta.TaskUID, gamma.TaskUID,
	)

	ttl := int32(60)
	backoff := int32(2)
	deadline := int64(60)
	fsGroup := int64(1000)
	runAsUser := int64(1000)
	allowPrivEsc := false

	writerJob := &batchv1.Job{}
	writerJob.Name = writerJobName
	writerJob.Namespace = chaosResumeNS
	writerJob.Spec.BackoffLimit = &backoff
	writerJob.Spec.TTLSecondsAfterFinished = &ttl
	writerJob.Spec.ActiveDeadlineSeconds = &deadline
	writerJob.Spec.Template.Spec = corev1.PodSpec{
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
				Name:    "release",
				Image:   "busybox:1.36",
				Command: []string{"sh", "-c", signalScript},
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

	// Best-effort create. If a stale writer Job exists (rerun without cleanup),
	// delete + recreate.
	if err := k8sClient.Create(ctx, writerJob); err != nil {
		Fail(fmt.Sprintf("create release-writer Job: %v", err))
	}

	// Wait for the writer Job to succeed; on timeout, dump its Pod logs.
	Eventually(func() bool {
		var j batchv1.Job
		if err := k8sClient.Get(ctx, client.ObjectKey{Name: writerJobName, Namespace: chaosResumeNS}, &j); err != nil {
			return false
		}
		return isJobSucceededShort(&j)
	}, 2*time.Minute, 2*time.Second).Should(BeTrue(),
		"release-writer Job must succeed (writes /workspace/envelopes/{β,γ}/release)")
	GinkgoWriter.Printf("Release signals written for β.UID=%s γ.UID=%s\n", beta.TaskUID, gamma.TaskUID)
}

// assertChaosWavesGoldenMatch is Pillar 5 — the algorithmic invariant. Reads
// the three Tasks live, projects them into pkg/dag.ComputeWaves's
// (nodes, edges) input via Task.Name + Task.Spec.DependsOn, runs ComputeWaves,
// and compares the result to the committed golden file. Equality on the wave
// structure (same node sets per wave layer) proves "schedule is re-derived,
// not cached on disk" (PERSIST-03).
func assertChaosWavesGoldenMatch() {
	var tl tideprojectv1alpha3.TaskList
	Expect(k8sClient.List(ctx, &tl, client.InNamespace(chaosResumeNS))).To(Succeed(),
		"Pillar 5: list Tasks for ComputeWaves input")

	// Project Tasks into (nodes, edges). Only consider tasks belonging to
	// the chaos-resume Plan to avoid false positives if other Tasks ever
	// land in the namespace.
	nodes := make([]dag.NodeID, 0, len(tl.Items))
	edges := make([]dag.Edge, 0, len(tl.Items))
	for _, t := range tl.Items {
		if t.Spec.PlanRef != "chaos-resume-plan" {
			continue
		}
		nodes = append(nodes, t.Name)
		for _, dep := range t.Spec.DependsOn {
			edges = append(edges, dag.Edge{From: dep, To: t.Name})
		}
	}
	Expect(nodes).To(HaveLen(3), "Pillar 5: must observe exactly 3 chaos-resume Tasks")

	waves, err := dag.ComputeWaves(nodes, edges)
	Expect(err).NotTo(HaveOccurred(), "Pillar 5: ComputeWaves must not error against live Tasks")

	// Normalize wave contents — ComputeWaves already lex-sorts within each
	// wave, but we sort defensively in case its contract evolves.
	for _, w := range waves {
		sort.Strings(w)
	}

	// Read + parse golden file.
	goldenPath := filepath.Join(chaosWavesGoldenRel)
	goldenData, gErr := os.ReadFile(goldenPath)
	Expect(gErr).NotTo(HaveOccurred(),
		"Pillar 5: golden file %s must be readable", goldenPath)
	var golden [][]string
	Expect(json.Unmarshal(goldenData, &golden)).To(Succeed(),
		"Pillar 5: golden file must parse as [][]string")

	Expect(waves).To(Equal(golden),
		"Pillar 5: ComputeWaves output must match golden snapshot (no cached schedule). "+
			"got=%v want=%v", waves, golden)
}

// writeChaosGolden serializes the pre-kill wave structure to the golden file.
// Only invoked when GENERATE_GOLDEN=1 — escape hatch for legitimate fixture
// changes.
func writeChaosGolden(_ map[string]chaosTaskSnapshot) {
	var tl tideprojectv1alpha3.TaskList
	Expect(k8sClient.List(ctx, &tl, client.InNamespace(chaosResumeNS))).To(Succeed())
	nodes := make([]dag.NodeID, 0, len(tl.Items))
	edges := make([]dag.Edge, 0, len(tl.Items))
	for _, t := range tl.Items {
		if t.Spec.PlanRef != "chaos-resume-plan" {
			continue
		}
		nodes = append(nodes, t.Name)
		for _, dep := range t.Spec.DependsOn {
			edges = append(edges, dag.Edge{From: dep, To: t.Name})
		}
	}
	waves, err := dag.ComputeWaves(nodes, edges)
	Expect(err).NotTo(HaveOccurred())
	data, err := json.MarshalIndent(waves, "", "  ")
	Expect(err).NotTo(HaveOccurred())
	Expect(os.WriteFile(chaosWavesGoldenRel, data, 0o644)).To(Succeed())
	GinkgoWriter.Printf("Regenerated golden file: %s\n%s\n", chaosWavesGoldenRel, data)
}

// isJobSucceededShort returns true if the Job has a JobComplete condition
// set to True. Local copy (name-clash-free with the controller package's
// helper) to keep this file self-contained.
func isJobSucceededShort(j *batchv1.Job) bool {
	for _, c := range j.Status.Conditions {
		if c.Type == batchv1.JobComplete && c.Status == "True" {
			return true
		}
	}
	return false
}
