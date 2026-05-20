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
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	tideprojectv1alpha1 "github.com/jsquirrelz/tide/api/v1alpha1"
	controller "github.com/jsquirrelz/tide/internal/controller"
	"github.com/jsquirrelz/tide/internal/dispatch/podjob"
	"github.com/jsquirrelz/tide/internal/pool"
)

var _ = Describe("Phase 04.1 P1.2 — planner dispatch contract (envtest)", Label("envtest", "phase4", "planner-dispatch"), func() {
	const pdProjectName = "pd-test-project-1"
	const pdMilestoneName = "pd-test-milestone-1"
	ctx := context.Background()

	BeforeEach(func() {
		proj := &tideprojectv1alpha1.Project{
			ObjectMeta: metav1.ObjectMeta{Name: pdProjectName, Namespace: "default"},
			Spec: tideprojectv1alpha1.ProjectSpec{
				TargetRepo: "https://github.com/example/pd-test.git",
				Subagent: tideprojectv1alpha1.SubagentConfig{
					Model: "claude-opus-4-7",
				},
				Git: &tideprojectv1alpha1.GitConfig{
					RepoURL:        "https://github.com/example/pd-test.git",
					CredsSecretRef: "pd-test-creds",
				},
			},
		}
		Expect(k8sClient.Create(ctx, proj)).To(Succeed())
		waitITCacheSync(pdProjectName, &tideprojectv1alpha1.Project{})
	})

	AfterEach(func() {
		ms := &tideprojectv1alpha1.Milestone{}
		if err := k8sClient.Get(ctx, types.NamespacedName{Name: pdMilestoneName, Namespace: "default"}, ms); err == nil {
			ms.Finalizers = nil
			_ = k8sClient.Update(ctx, ms)
			_ = k8sClient.Delete(ctx, ms)
		}
		proj := &tideprojectv1alpha1.Project{}
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
			ms := &tideprojectv1alpha1.Milestone{
				ObjectMeta: metav1.ObjectMeta{Name: pdMilestoneName, Namespace: "default"},
				Spec:       tideprojectv1alpha1.MilestoneSpec{ProjectRef: pdProjectName},
			}
			Expect(k8sClient.Create(ctx, ms)).To(Succeed())
			waitITCacheSync(pdMilestoneName, &tideprojectv1alpha1.Milestone{})

			// Drive reconciliation with an explicit reconciler instance
			// (not the manager's cached reconciler) so field injection is precise.
			r := &controller.MilestoneReconciler{
				Client:         mgrClient,
				Scheme:         k8sClient.Scheme(),
				Dispatcher:     &stubDispatcher{},
				PlannerPool:    pool.New(16, "planner"),
				EnvReader:      newMapEnvReader(),
				SubagentImage:  testSubagentImage,
				CredproxyImage: testCredproxyImage,
				SigningKey:      testSigningKey,
			}

			// Drive 5 reconcile passes to get past: finalizer-add → owner-ref → dispatch.
			for i := 0; i < 5; i++ {
				_, _ = r.Reconcile(ctx, reconcile.Request{
					NamespacedName: types.NamespacedName{Name: pdMilestoneName, Namespace: "default"},
				})
			}

			// Fetch the Milestone UID — needed for deterministic Job name.
			var got tideprojectv1alpha1.Milestone
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
			}, 5*time.Second, 100*time.Millisecond).Should(Succeed())

			// --- JobKindPlanner label assertions ---
			By("asserting role=planner label is set")
			Expect(job.Labels["tideproject.k8s/role"]).To(Equal("planner"),
				"planner Job must have role=planner label")

			By("asserting level=milestone label is set")
			Expect(job.Labels["tideproject.k8s/level"]).To(Equal("milestone"),
				"planner Job must have level=milestone label")

			By("asserting milestone-uid label is set")
			Expect(job.Labels[fmt.Sprintf("tideproject.k8s/milestone-uid")]).To(Equal(string(got.UID)),
				"planner Job must have tideproject.k8s/milestone-uid label")

			// --- Milestone Status assertion ---
			By("asserting Milestone.Status.Phase=Running after dispatch")
			Eventually(func(g Gomega) {
				var msAfter tideprojectv1alpha1.Milestone
				g.Expect(mgrClient.Get(ctx, types.NamespacedName{Name: pdMilestoneName, Namespace: "default"}, &msAfter)).To(Succeed())
				g.Expect(msAfter.Status.Phase).To(Equal("Running"))
			}, 5*time.Second, 100*time.Millisecond).Should(Succeed())

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
