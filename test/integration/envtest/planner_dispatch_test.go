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

package envtest_integration

// Phase 04.1 P1.2 — Layer A envtest asserting that a Milestone planner Job
// carries the full Phase 2 dispatch contract:
//   - PVC volume + subPath isolation ({project-uid}/workspace)
//   - credproxy native sidecar (init container with RestartPolicy=Always)
//   - subagent container with signed-token env (ANTHROPIC_API_KEY)
//   - envelope-writer init container
//   - two init containers total (envelope-writer + credproxy)
//   - Job name follows tide-milestone-<uid>-1 format
//   - JobKindPlanner label (role=planner, level=milestone)
//
// The spec exercises MilestoneReconciler directly (not the manager's cached
// reconciler) so field injection is explicit and the assertions run without
// waiting for a controller-runtime watch cycle.

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	tideprojectv1alpha3 "github.com/jsquirrelz/tide/api/v1alpha3"
	controller "github.com/jsquirrelz/tide/internal/controller"
	"github.com/jsquirrelz/tide/internal/dispatch/podjob"
	"github.com/jsquirrelz/tide/internal/pool"
	pkgdispatch "github.com/jsquirrelz/tide/pkg/dispatch"
)

var _ = Describe("Phase 04.1 P1.2 — planner dispatch contract (envtest)", Label("envtest", "phase4", "planner-dispatch"), func() {
	const pdProjectName = "pd-test-project-1"
	const pdMilestoneName = "pd-test-milestone-1"
	ctx := context.Background()

	BeforeEach(func() {
		proj := &tideprojectv1alpha3.Project{
			ObjectMeta: metav1.ObjectMeta{Name: pdProjectName, Namespace: "default"},
			Spec: tideprojectv1alpha3.ProjectSpec{SchemaRevision: "v1alpha3",
				TargetRepo: "https://github.com/example/pd-test.git",
				Subagent: tideprojectv1alpha3.SubagentConfig{
					Model: "claude-opus-4-7",
				},
				Git: &tideprojectv1alpha3.GitConfig{
					RepoURL:        "https://github.com/example/pd-test.git",
					CredsSecretRef: "pd-test-creds",
				},
				// cascade-13: credproxy is now gated on ProviderSecretRef. This spec exercises
				// the full dispatch contract INCLUDING the credproxy native sidecar, so the
				// Project must carry a provider secret ref (the $0/no-secret path is covered by
				// the jobspec unit tests' present/absent assertions).
				ProviderSecretRef: "pd-test-provider-secret",
			},
		}
		Expect(k8sClient.Create(ctx, proj)).To(Succeed())
		waitITCacheSync(pdProjectName, &tideprojectv1alpha3.Project{})
	})

	AfterEach(func() {
		ms := &tideprojectv1alpha3.Milestone{}
		if err := k8sClient.Get(ctx, types.NamespacedName{Name: pdMilestoneName, Namespace: "default"}, ms); err == nil {
			ms.Finalizers = nil
			_ = k8sClient.Update(ctx, ms)
			_ = k8sClient.Delete(ctx, ms)
		}
		proj := &tideprojectv1alpha3.Project{}
		if err := k8sClient.Get(ctx, types.NamespacedName{Name: pdProjectName, Namespace: "default"}, proj); err == nil {
			proj.Finalizers = nil
			_ = k8sClient.Update(ctx, proj)
			_ = k8sClient.Delete(ctx, proj)
		}
		var jobs batchv1.JobList
		_ = k8sClient.List(ctx, &jobs)
		for i := range jobs.Items {
			j := jobs.Items[i]
			_ = k8sClient.Delete(ctx, &j)
		}
	})

	Describe("Milestone planner Job has full dispatch contract", func() {
		It("creates a planner Job with PVC mount, credproxy sidecar, signed-token env, and correct labels", func() {
			ms := &tideprojectv1alpha3.Milestone{
				ObjectMeta: metav1.ObjectMeta{Name: pdMilestoneName, Namespace: "default"},
				Spec:       tideprojectv1alpha3.MilestoneSpec{ProjectRef: pdProjectName},
			}
			Expect(k8sClient.Create(ctx, ms)).To(Succeed())
			waitITCacheSync(pdMilestoneName, &tideprojectv1alpha3.Milestone{})

			// Drive reconciliation with an explicit reconciler instance
			// (not the manager's cached reconciler) so field injection is precise.
			r := &controller.MilestoneReconciler{
				Client: mgrClient,
				Scheme: k8sClient.Scheme(),
				Deps: controller.PlannerReconcilerDeps{
					Dispatcher:     &stubDispatcher{},
					EnvReader:      newMapEnvReader(),
					CredproxyImage: testCredproxyImage,
					SigningKey:     testSigningKey,
				},
				PlannerPool: pool.New(16, "planner"),
			}

			// Drive 5 reconcile passes to get past: finalizer-add → owner-ref → dispatch.
			for range 5 {
				_, _ = r.Reconcile(ctx, reconcile.Request{
					NamespacedName: types.NamespacedName{Name: pdMilestoneName, Namespace: "default"},
				})
			}

			// Fetch the Milestone UID — needed for deterministic Job name.
			var got tideprojectv1alpha3.Milestone
			Eventually(func() error {
				return mgrClient.Get(ctx, types.NamespacedName{Name: pdMilestoneName, Namespace: "default"}, &got)
			}, "5s", "100ms").Should(Succeed())

			expectedJobName := fmt.Sprintf("tide-milestone-%s-1", got.UID)

			// Wait for the planner Job to appear.
			var job batchv1.Job
			Eventually(func(g Gomega) {
				g.Expect(mgrClient.Get(ctx, types.NamespacedName{
					Name:      expectedJobName,
					Namespace: "default",
				}, &job)).To(Succeed())
			}, 15*time.Second, 100*time.Millisecond).Should(Succeed())

			// --- JobKindPlanner label assertions ---
			By("asserting role=planner label is set")
			Expect(job.Labels["tideproject.k8s/role"]).To(Equal("planner"),
				"planner Job must have role=planner label")

			By("asserting level=milestone label is set")
			Expect(job.Labels["tideproject.k8s/level"]).To(Equal("milestone"),
				"planner Job must have level=milestone label")

			By("asserting milestone-uid label is set")
			Expect(job.Labels["tideproject.k8s/milestone-uid"]).To(Equal(string(got.UID)),
				"planner Job must have tideproject.k8s/milestone-uid label")

			// --- Milestone Status assertion ---
			By("asserting Milestone.Status.Phase=Running after dispatch")
			Eventually(func(g Gomega) {
				var msAfter tideprojectv1alpha3.Milestone
				g.Expect(mgrClient.Get(ctx, types.NamespacedName{Name: pdMilestoneName, Namespace: "default"}, &msAfter)).To(Succeed())
				g.Expect(msAfter.Status.Phase).To(Equal("Running"))
			}, 15*time.Second, 100*time.Millisecond).Should(Succeed())

			spec := job.Spec.Template.Spec

			// --- Init container assertions (envelope-writer + credproxy) ---
			By("asserting exactly two init containers")
			Expect(spec.InitContainers).To(HaveLen(2),
				"planner Job must have exactly two init containers: envelope-writer + credproxy sidecar")

			By("asserting first init container is envelope-writer")
			Expect(spec.InitContainers[0].Name).To(Equal(podjob.ContainerNameEnvelopeWriter),
				"first init container must be envelope-writer")

			By("asserting second init container is tide-credproxy native sidecar")
			Expect(spec.InitContainers[1].Name).To(Equal(podjob.ContainerNameCredproxy),
				"second init container must be tide-credproxy")

			By("asserting credproxy sidecar has RestartPolicy=Always (native sidecar marker)")
			Expect(spec.InitContainers[1].RestartPolicy).NotTo(BeNil(),
				"credproxy sidecar RestartPolicy must not be nil")
			Expect(*spec.InitContainers[1].RestartPolicy).To(Equal(corev1.ContainerRestartPolicyAlways),
				"credproxy sidecar must have RestartPolicy=Always (K8s 1.33 native sidecar)")

			// --- Main subagent container ---
			By("asserting exactly one main container (subagent)")
			Expect(spec.Containers).To(HaveLen(1),
				"planner Job must have exactly one main container (subagent)")
			Expect(spec.Containers[0].Name).To(Equal(podjob.ContainerNameSubagent),
				"main container must be named 'subagent'")

			By("asserting subagent container has ANTHROPIC_API_KEY env (signed token)")
			var foundToken bool
			for _, e := range spec.Containers[0].Env {
				if e.Name == "ANTHROPIC_API_KEY" && e.Value != "" {
					foundToken = true
					break
				}
			}
			Expect(foundToken).To(BeTrue(),
				"subagent container must have ANTHROPIC_API_KEY env var set to signed token")

			// --- PVC subPath isolation ---
			By("asserting subagent container has PVC volume mount with subPath isolation")
			var foundPVCMount bool
			for _, vm := range spec.Containers[0].VolumeMounts {
				if vm.Name == podjob.VolumeProjectWorkspace && vm.SubPath != "" {
					// subPath must not start with "/" (relative path required by K8s)
					Expect(vm.SubPath).NotTo(HavePrefix("/"),
						"PVC subPath must be a relative path (no leading slash)")
					Expect(vm.SubPath).To(HaveSuffix("/workspace"),
						"PVC subPath must end with /workspace")
					foundPVCMount = true
					break
				}
			}
			Expect(foundPVCMount).To(BeTrue(),
				"subagent container must have a PVC volume mount with non-empty subPath")

			By("asserting envelope-writer init container also has PVC mount with subPath")
			var foundEnvWriterMount bool
			for _, vm := range spec.InitContainers[0].VolumeMounts {
				if vm.Name == podjob.VolumeProjectWorkspace && vm.SubPath != "" {
					foundEnvWriterMount = true
					break
				}
			}
			Expect(foundEnvWriterMount).To(BeTrue(),
				"envelope-writer init container must also have a PVC volume mount with subPath")

			// --- ActiveDeadlineSeconds (planner 600s floor + 60s grace) ---
			By("asserting ActiveDeadlineSeconds uses planner 600s floor")
			Expect(job.Spec.ActiveDeadlineSeconds).NotTo(BeNil(),
				"planner Job must have ActiveDeadlineSeconds set")
			// planner floor is 600s + 60s grace = 660s minimum.
			Expect(*job.Spec.ActiveDeadlineSeconds).To(BeNumerically(">=", 660),
				"planner Job ActiveDeadlineSeconds must be >= 660 (600s floor + 60s grace)")

			// --- credproxy sidecar image ---
			By("asserting credproxy sidecar uses the configured CredproxyImage")
			Expect(spec.InitContainers[1].Image).To(Equal(testCredproxyImage),
				"credproxy sidecar must use the testCredproxyImage")

			// --- subagent image ---
			By("asserting subagent uses the configured SubagentImage")
			Expect(spec.Containers[0].Image).To(Equal(testSubagentImage),
				"subagent container must use the testSubagentImage")
		})
	})
})

// decodeEnvelopeFromJob extracts and decodes the EnvelopeIn JSON carried by a
// dispatch Job's envelope-writer init container (the ENVELOPE_IN_B64 env var —
// see internal/dispatch/podjob/jobspec.go BuildJobSpec). This is the only place
// the RESOLVED Provider (vendor/model/params) is observable on the Job itself;
// the Job's labels only carry the dispatch level, not the resolved model.
func decodeEnvelopeFromJob(job *batchv1.Job) pkgdispatch.EnvelopeIn {
	var b64 string
	for _, ic := range job.Spec.Template.Spec.InitContainers {
		if ic.Name != podjob.ContainerNameEnvelopeWriter {
			continue
		}
		for _, e := range ic.Env {
			if e.Name == "ENVELOPE_IN_B64" {
				b64 = e.Value
			}
		}
	}
	ExpectWithOffset(1, b64).NotTo(BeEmpty(), "envelope-writer init container must carry ENVELOPE_IN_B64 on Job %s", job.Name)
	raw, err := base64.StdEncoding.DecodeString(b64)
	ExpectWithOffset(1, err).NotTo(HaveOccurred(), "decode ENVELOPE_IN_B64 on Job %s", job.Name)
	var envIn pkgdispatch.EnvelopeIn
	ExpectWithOffset(1, json.Unmarshal(raw, &envIn)).To(Succeed(), "unmarshal EnvelopeIn from Job %s", job.Name)
	return envIn
}

// D-02 (folded todo 2026-07-03-project-level-subagent-override-slot.md): each
// Levels.X override key now means "level X is planned by this model" — the
// mapping implemented by levelOverrideKey inside ResolveProvider/resolveImage
// (internal/controller/dispatch_helpers.go). Dispatch identity (the level
// string carried in the envelope and stamped as the tideproject.k8s/level Job
// label) stays the 5-valued "project"|"milestone"|"phase"|"plan"|"task" set —
// only which Levels.<key> slot each dispatch level RESOLVES against shifts.
//
// This spec drives all five dispatch surfaces of ONE Project/Milestone/Phase/
// Plan/Task chain (each override slot set to a distinct model string) so each
// assertion below unambiguously identifies which Levels.<key> slot the
// corresponding dispatch level resolved against.
var _ = Describe("Phase 40-04 D-02 — subagent.levels override-key mapping resolves per dispatch surface (envtest)", Label("envtest", "phase40", "levels-rename"), func() {
	const lrProjectName = "lr-test-project-1"
	const lrMilestoneName = "lr-test-milestone-1"
	const lrPhaseName = "lr-test-phase-1"
	const lrPlanName = "lr-test-plan-1"
	const lrTaskName = "lr-test-task-1"
	ctx := context.Background()

	AfterEach(func() {
		cleanupGateFlowFixture(lrProjectName, lrPlanName, lrMilestoneName, lrPhaseName)
		var jobs batchv1.JobList
		_ = k8sClient.List(ctx, &jobs)
		for i := range jobs.Items {
			j := jobs.Items[i]
			_ = k8sClient.Delete(ctx, &j)
		}
	})

	It("resolves each dispatch level's model via the D-02 override-key mapping and leaves dispatch identity (envelope Level + Job level label) unchanged", func() {
		makeBoundPVC(ctx, "tide-projects", "default")

		// The Project carries FOUR distinct per-level models — one per Levels.X
		// override slot — so each dispatch surface's resolved Provider.Model
		// unambiguously identifies which slot it read from.
		proj := &tideprojectv1alpha3.Project{
			ObjectMeta: metav1.ObjectMeta{Name: lrProjectName, Namespace: "default"},
			Spec: tideprojectv1alpha3.ProjectSpec{SchemaRevision: "v1alpha3",
				TargetRepo: "https://github.com/example/levels-rename.git",
				Git: &tideprojectv1alpha3.GitConfig{
					RepoURL:        "https://github.com/example/levels-rename.git",
					CredsSecretRef: "lr-test-creds",
				},
				Subagent: tideprojectv1alpha3.SubagentConfig{
					Levels: tideprojectv1alpha3.LevelOverrides{
						Milestone: &tideprojectv1alpha3.LevelConfig{Model: "model-milestone-md"},
						Phase:     &tideprojectv1alpha3.LevelConfig{Model: "model-phase-briefs"},
						Plan:      &tideprojectv1alpha3.LevelConfig{Model: "model-plan-content"},
						Task:      &tideprojectv1alpha3.LevelConfig{Model: "model-task-exec"},
					},
				},
			},
		}
		Expect(k8sClient.Create(ctx, proj)).To(Succeed())
		waitITCacheSync(lrProjectName, &tideprojectv1alpha3.Project{})

		// Bypass init-Job/PVC ceremony (mirrors boundary_push_test.go's makeProjectBP
		// helper): stamping BranchName lets reconcilePhase3Lifecycle — and its Step 0b
		// project-level planner dispatch — run without a real init Pod completing.
		var gotProj tideprojectv1alpha3.Project
		Expect(mgrClient.Get(ctx, types.NamespacedName{Name: lrProjectName, Namespace: "default"}, &gotProj)).To(Succeed())
		projPatch := client.MergeFrom(gotProj.DeepCopy())
		gotProj.Status.Git.BranchName = "tide/run-" + lrProjectName + "-1700000000"
		Expect(k8sClient.Status().Patch(ctx, &gotProj, projPatch)).To(Succeed())

		// The suite-registered (background) ProjectReconciler carries no SigningKey
		// (boundary_push_test.go: "Milestone/Project deliberately stay push-incapable")
		// so it never reaches project-level planner dispatch — an explicit instance
		// is required for Test 1, mirroring this file's existing Milestone pattern.
		rProject := &controller.ProjectReconciler{
			Client: mgrClient,
			Scheme: k8sClient.Scheme(),
			Deps: controller.PlannerReconcilerDeps{
				Dispatcher:           &stubDispatcher{},
				SigningKey:           testSigningKey,
				CredproxyImage:       testCredproxyImage,
				HelmProviderDefaults: controller.ProviderDefaults{Image: testSubagentImage},
			},
		}

		// --- Test 1: Project-CR planner dispatch (authors MILESTONE.md, dispatch
		// level "project") resolves Levels.Milestone's model — the D-02 rename
		// target. Pre-fix this silently falls back to Spec.Subagent.Model ("").
		projectJobName := fmt.Sprintf("tide-project-%s-1", gotProj.UID)
		var projectJob batchv1.Job
		Eventually(func(g Gomega) {
			_, _ = rProject.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: lrProjectName, Namespace: "default"},
			})
			g.Expect(mgrClient.Get(ctx, types.NamespacedName{Name: projectJobName, Namespace: "default"}, &projectJob)).To(Succeed())
		}, 15*time.Second, 100*time.Millisecond).Should(Succeed())

		projectEnv := decodeEnvelopeFromJob(&projectJob)
		Expect(projectEnv.Provider.Model).To(Equal("model-milestone-md"),
			"project-level dispatch (authors MILESTONE.md) must resolve Levels.Milestone's model (D-02)")
		Expect(projectEnv.Level).To(Equal("project"), `dispatch identity: envelope Level must stay "project"`)
		Expect(projectJob.Labels["tideproject.k8s/level"]).To(Equal("project"), `dispatch identity: Job level label must stay "project"`)

		// --- Test 2: Milestone-CR dispatch (authors phase briefs, dispatch level
		// "milestone") resolves Levels.Phase's model.
		ms := &tideprojectv1alpha3.Milestone{
			ObjectMeta: metav1.ObjectMeta{Name: lrMilestoneName, Namespace: "default"},
			Spec:       tideprojectv1alpha3.MilestoneSpec{ProjectRef: lrProjectName},
		}
		Expect(k8sClient.Create(ctx, ms)).To(Succeed())
		waitITCacheSync(lrMilestoneName, &tideprojectv1alpha3.Milestone{})

		var gotMs tideprojectv1alpha3.Milestone
		Eventually(func() error {
			return mgrClient.Get(ctx, types.NamespacedName{Name: lrMilestoneName, Namespace: "default"}, &gotMs)
		}, "5s", "100ms").Should(Succeed())

		rMilestone := &controller.MilestoneReconciler{
			Client: mgrClient,
			Scheme: k8sClient.Scheme(),
			Deps: controller.PlannerReconcilerDeps{
				Dispatcher:           &stubDispatcher{},
				EnvReader:            newMapEnvReader(),
				CredproxyImage:       testCredproxyImage,
				SigningKey:           testSigningKey,
				HelmProviderDefaults: controller.ProviderDefaults{Image: testSubagentImage},
			},
			PlannerPool: pool.New(16, "planner"),
		}
		msJobName := fmt.Sprintf("tide-milestone-%s-1", gotMs.UID)
		var msJob batchv1.Job
		Eventually(func(g Gomega) {
			_, _ = rMilestone.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: lrMilestoneName, Namespace: "default"},
			})
			g.Expect(mgrClient.Get(ctx, types.NamespacedName{Name: msJobName, Namespace: "default"}, &msJob)).To(Succeed())
		}, 15*time.Second, 100*time.Millisecond).Should(Succeed())

		msEnv := decodeEnvelopeFromJob(&msJob)
		Expect(msEnv.Provider.Model).To(Equal("model-phase-briefs"),
			"milestone-level dispatch (authors phase briefs) must resolve Levels.Phase's model (D-02)")
		Expect(msEnv.Level).To(Equal("milestone"), `dispatch identity: envelope Level must stay "milestone"`)
		Expect(msJob.Labels["tideproject.k8s/level"]).To(Equal("milestone"), `dispatch identity: Job level label must stay "milestone"`)

		// --- Test 3: Phase-CR dispatch (authors PLAN.md, dispatch level "phase")
		// resolves Levels.Plan's model.
		ph := &tideprojectv1alpha3.Phase{
			ObjectMeta: metav1.ObjectMeta{Name: lrPhaseName, Namespace: "default"},
			Spec:       tideprojectv1alpha3.PhaseSpec{MilestoneRef: lrMilestoneName},
		}
		Expect(k8sClient.Create(ctx, ph)).To(Succeed())
		waitITCacheSync(lrPhaseName, &tideprojectv1alpha3.Phase{})

		var gotPh tideprojectv1alpha3.Phase
		Eventually(func() error {
			return mgrClient.Get(ctx, types.NamespacedName{Name: lrPhaseName, Namespace: "default"}, &gotPh)
		}, "5s", "100ms").Should(Succeed())

		rPhase := newPhaseReconcilerForGateIT()
		rPhase.Deps.HelmProviderDefaults = controller.ProviderDefaults{Image: testSubagentImage}
		phJobName := fmt.Sprintf("tide-phase-%s-1", gotPh.UID)
		var phJob batchv1.Job
		Eventually(func(g Gomega) {
			_, _ = rPhase.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: lrPhaseName, Namespace: "default"},
			})
			g.Expect(mgrClient.Get(ctx, types.NamespacedName{Name: phJobName, Namespace: "default"}, &phJob)).To(Succeed())
		}, 15*time.Second, 100*time.Millisecond).Should(Succeed())

		phEnv := decodeEnvelopeFromJob(&phJob)
		Expect(phEnv.Provider.Model).To(Equal("model-plan-content"),
			"phase-level dispatch (authors PLAN.md) must resolve Levels.Plan's model (D-02)")
		Expect(phEnv.Level).To(Equal("phase"), `dispatch identity: envelope Level must stay "phase"`)
		Expect(phJob.Labels["tideproject.k8s/level"]).To(Equal("phase"), `dispatch identity: Job level label must stay "phase"`)

		// --- Test 4: Plan-CR dispatch (authors the task DAG, dispatch level "plan")
		// ALSO resolves Levels.Plan's model — the D-11 collapse: same override slot
		// as the phase-level dispatch above (one key, two planning dispatches).
		plan := &tideprojectv1alpha3.Plan{
			ObjectMeta: metav1.ObjectMeta{Name: lrPlanName, Namespace: "default"},
			Spec:       tideprojectv1alpha3.PlanSpec{PhaseRef: lrPhaseName},
		}
		Expect(k8sClient.Create(ctx, plan)).To(Succeed())
		waitITCacheSync(lrPlanName, &tideprojectv1alpha3.Plan{})

		var gotPlan tideprojectv1alpha3.Plan
		Eventually(func() error {
			return mgrClient.Get(ctx, types.NamespacedName{Name: lrPlanName, Namespace: "default"}, &gotPlan)
		}, "5s", "100ms").Should(Succeed())

		rPlan := &controller.PlanReconciler{
			Client: mgrClient,
			Scheme: k8sClient.Scheme(),
			Deps: controller.PlannerReconcilerDeps{
				Dispatcher:           &stubDispatcher{},
				CredproxyImage:       testCredproxyImage,
				SigningKey:           testSigningKey,
				HelmProviderDefaults: controller.ProviderDefaults{Image: testSubagentImage},
			},
			PlannerPool: pool.New(16, "planner"),
		}
		planJobName := fmt.Sprintf("tide-plan-%s-1", gotPlan.UID)
		var planJob batchv1.Job
		Eventually(func(g Gomega) {
			_, _ = rPlan.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: lrPlanName, Namespace: "default"},
			})
			g.Expect(mgrClient.Get(ctx, types.NamespacedName{Name: planJobName, Namespace: "default"}, &planJob)).To(Succeed())
		}, 15*time.Second, 100*time.Millisecond).Should(Succeed())

		planEnv := decodeEnvelopeFromJob(&planJob)
		Expect(planEnv.Provider.Model).To(Equal("model-plan-content"),
			"plan-level dispatch (authors the task DAG) must resolve Levels.Plan's model — D-11 collapse, same slot as the phase-level dispatch")
		Expect(planEnv.Level).To(Equal("plan"), `dispatch identity: envelope Level must stay "plan"`)
		Expect(planJob.Labels["tideproject.k8s/level"]).To(Equal("plan"), `dispatch identity: Job level label must stay "plan"`)

		// --- Test 5 + Test 6 (task half): task executor dispatch (dispatch level
		// "task") resolves Levels.Task's model — unaffected by the rename (was
		// never off-by-one). Driven by the suite-registered background
		// TaskReconciler (SigningKey wired in suite_test.go); no explicit instance
		// needed since the resolved model comes from the Project spec, not
		// reconciler field injection.
		task := makeGateITTask(lrTaskName, lrPlanName, lrProjectName, nil)

		taskJobName := podjob.JobName(task.UID, 1)
		var taskJob batchv1.Job
		Eventually(func(g Gomega) {
			g.Expect(mgrClient.Get(ctx, types.NamespacedName{Name: taskJobName, Namespace: "default"}, &taskJob)).To(Succeed())
		}, 15*time.Second, 100*time.Millisecond).Should(Succeed())

		taskEnv := decodeEnvelopeFromJob(&taskJob)
		Expect(taskEnv.Provider.Model).To(Equal("model-task-exec"),
			"task executor dispatch must resolve Levels.Task's model (unchanged mapping — was never off-by-one)")
		// Executor Jobs carry no tideproject.k8s/level label (only planner Jobs do —
		// jobspec.go's JobKindExecutor branch never sets one), so the envelope Level
		// field is the correct place to pin dispatch identity for this surface.
		Expect(taskEnv.Level).To(Equal("task"), `dispatch identity: envelope Level must stay "task"`)
	})
})
