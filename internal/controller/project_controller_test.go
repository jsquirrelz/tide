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

package controller

import (
	"context"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	tideprojectv1alpha2 "github.com/jsquirrelz/tide/api/v1alpha2"
)

// newTestProjectReconciler returns a ProjectReconciler wired to the shared
// envtest k8sClient with a stub Dispatcher so the seam body executes.
// stubDispatcher is defined in task_controller_test.go (same package).
func newTestProjectReconciler() *ProjectReconciler {
	return &ProjectReconciler{
		Client:                  k8sClient,
		Scheme:                  k8sClient.Scheme(),
		Dispatcher:              &stubDispatcher{},
		MaxConcurrentReconciles: 1,
	}
}

// makeTestBoundPVC creates a bound PVC named pvcName in namespace ns.
func makeTestBoundPVC(ctx context.Context, name, ns string) {
	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: ns,
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteMany},
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceStorage: resource.MustParse("1Gi"),
				},
			},
		},
	}
	Expect(k8sClient.Create(ctx, pvc)).To(Succeed())
	// Patch the PVC status to Bound so the reconciler proceeds.
	pvcPatch := pvc.DeepCopy()
	pvcPatch.Status.Phase = corev1.ClaimBound
	Expect(k8sClient.Status().Update(ctx, pvcPatch)).To(Succeed())
}

var _ = Describe("ProjectReconciler init + budget", Label("envtest", "phase2"), func() {
	ctx := context.Background()

	Describe("Init Job lifecycle", func() {
		It("TestProjectReconciler_CreatesInitJobOnFirstReconcile", func() {
			ns := "default"
			pvcName := "tide-projects-create-init"
			makeTestBoundPVC(ctx, pvcName, ns)
			DeferCleanup(func() {
				pvc := &corev1.PersistentVolumeClaim{}
				pvc.Name = pvcName
				pvc.Namespace = ns
				_ = k8sClient.Delete(ctx, pvc)
			})

			project := &tideprojectv1alpha2.Project{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-create-init-job",
					Namespace: ns,
				},
				Spec: tideprojectv1alpha2.ProjectSpec{SchemaRevision: "v1alpha2",
					TargetRepo: "https://github.com/example/repo.git",
				},
			}
			Expect(k8sClient.Create(ctx, project)).To(Succeed())
			DeferCleanup(func() { _ = k8sClient.Delete(ctx, project) })

			reconciler := newTestProjectReconciler()
			reconciler.SharedPVCName = pvcName

			name := types.NamespacedName{Name: project.Name, Namespace: project.Namespace}

			// Reconcile 1: add the finalizer. Regression (debug medium-http-completion-wedge):
			// the finalizer Update changes neither generation nor annotations, so the
			// For()-level predicate.Or(GenerationChangedPredicate, AnnotationChangedPredicate)
			// filters out the resulting Update event. The reconcile MUST self-requeue or the
			// Project parks at empty Status.Phase forever (never reaching the init-Job seam).
			res, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: name})
			Expect(err).NotTo(HaveOccurred())
			Expect(res.Requeue).To(BeTrue(), //nolint:staticcheck // SA1019: asserting the controller sets the legacy Requeue field after finalizer add (the fix under test).
				"finalizer-add reconcile must Requeue (predicate filters the finalizer Update event)")
			// Reconcile 2: execute seam body — should create the init Job.
			_, err = reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: name})
			Expect(err).NotTo(HaveOccurred())

			// Re-fetch project to get the UID.
			fetched := &tideprojectv1alpha2.Project{}
			Expect(k8sClient.Get(ctx, name, fetched)).To(Succeed())

			expectedJobName := "tide-init-" + string(fetched.UID)
			var job batchv1.Job
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{Name: expectedJobName, Namespace: ns}, &job)
			}, 10*time.Second, 250*time.Millisecond).Should(Succeed(), "init Job should be created")

			// Verify busybox mkdir command.
			Expect(job.Spec.Template.Spec.Containers).To(HaveLen(1))
			Expect(job.Spec.Template.Spec.Containers[0].Command).To(ContainElements(
				"sh", "-c",
			))
			// Command should contain mkdir -p
			var cmdJoined strings.Builder
			for _, c := range job.Spec.Template.Spec.Containers[0].Command {
				cmdJoined.WriteString(c + " ")
			}
			Expect(cmdJoined.String()).To(ContainSubstring("mkdir -p"))

			// Verify shared PVC subPath wiring (Blocker #2/#3).
			foundPVC := false
			for _, v := range job.Spec.Template.Spec.Volumes {
				if v.PersistentVolumeClaim != nil && v.PersistentVolumeClaim.ClaimName == pvcName {
					foundPVC = true
					break
				}
			}
			Expect(foundPVC).To(BeTrue(), "init Job should mount the shared PVC %s", pvcName)

			foundSubPath := false
			expectedSubPath := string(fetched.UID) + "/workspace"
			for _, vm := range job.Spec.Template.Spec.Containers[0].VolumeMounts {
				if vm.SubPath == expectedSubPath {
					foundSubPath = true
					break
				}
			}
			Expect(foundSubPath).To(BeTrue(),
				"init Job volumeMount should have SubPath=%s (Blocker #2/#3)", expectedSubPath)
		})

		It("TestProjectReconciler_InitJobIdempotent", func() {
			ns := "default"
			pvcName := "tide-projects-idempotent"
			makeTestBoundPVC(ctx, pvcName, ns)
			DeferCleanup(func() {
				pvc := &corev1.PersistentVolumeClaim{}
				pvc.Name = pvcName
				pvc.Namespace = ns
				_ = k8sClient.Delete(ctx, pvc)
			})

			project := &tideprojectv1alpha2.Project{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-init-idempotent",
					Namespace: ns,
				},
				Spec: tideprojectv1alpha2.ProjectSpec{SchemaRevision: "v1alpha2",
					TargetRepo: "https://github.com/example/repo.git",
				},
			}
			Expect(k8sClient.Create(ctx, project)).To(Succeed())
			DeferCleanup(func() { _ = k8sClient.Delete(ctx, project) })

			reconciler := newTestProjectReconciler()
			reconciler.SharedPVCName = pvcName
			name := types.NamespacedName{Name: project.Name, Namespace: project.Namespace}

			// Reconcile 1: add finalizer.
			_, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: name})
			Expect(err).NotTo(HaveOccurred())
			// Reconcile 2: creates init Job.
			_, err = reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: name})
			Expect(err).NotTo(HaveOccurred())
			// Reconcile 3: should be idempotent — AlreadyExists handled gracefully.
			_, err = reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: name})
			Expect(err).NotTo(HaveOccurred())

			// Re-fetch project to get UID.
			fetched := &tideprojectv1alpha2.Project{}
			Expect(k8sClient.Get(ctx, name, fetched)).To(Succeed())

			jobName := "tide-init-" + string(fetched.UID)
			var jobList batchv1.JobList
			Expect(k8sClient.List(ctx, &jobList, client.InNamespace(ns))).To(Succeed())
			count := 0
			for _, j := range jobList.Items {
				if j.Name == jobName {
					count++
				}
			}
			Expect(count).To(Equal(1), "exactly one init Job should exist after multiple reconciles")
		})

		It("TestProjectReconciler_OnInitJobSuccess_SetsPhaseInitialized", func() {
			ns := "default"
			pvcName := "tide-projects-success"
			makeTestBoundPVC(ctx, pvcName, ns)
			DeferCleanup(func() {
				pvc := &corev1.PersistentVolumeClaim{}
				pvc.Name = pvcName
				pvc.Namespace = ns
				_ = k8sClient.Delete(ctx, pvc)
			})

			project := &tideprojectv1alpha2.Project{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-init-success",
					Namespace: ns,
				},
				Spec: tideprojectv1alpha2.ProjectSpec{SchemaRevision: "v1alpha2",
					TargetRepo: "https://github.com/example/repo.git",
				},
			}
			Expect(k8sClient.Create(ctx, project)).To(Succeed())
			DeferCleanup(func() { _ = k8sClient.Delete(ctx, project) })

			reconciler := newTestProjectReconciler()
			reconciler.SharedPVCName = pvcName
			name := types.NamespacedName{Name: project.Name, Namespace: project.Namespace}

			// Reconcile 1: add finalizer.
			_, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: name})
			Expect(err).NotTo(HaveOccurred())

			fetched := &tideprojectv1alpha2.Project{}
			Expect(k8sClient.Get(ctx, name, fetched)).To(Succeed())

			// Pre-create a Succeeded init Job.
			completedJob := buildSucceededInitJob(fetched, pvcName)
			Expect(k8sClient.Create(ctx, completedJob)).To(Succeed())
			// Patch Job status to Succeeded.
			// K8s 1.36 requires: SuccessCriteriaMet=True before Complete=True,
			// and startTime is required for finished jobs.
			now := metav1.Now()
			jobPatch := completedJob.DeepCopy()
			jobPatch.Status = batchv1.JobStatus{
				Succeeded:      1,
				StartTime:      &now,
				CompletionTime: &now,
				Conditions: []batchv1.JobCondition{
					{
						Type:               batchv1.JobSuccessCriteriaMet,
						Status:             corev1.ConditionTrue,
						LastProbeTime:      now,
						LastTransitionTime: now,
						Reason:             "CompletionsReached",
					},
					{
						Type:               batchv1.JobComplete,
						Status:             corev1.ConditionTrue,
						LastProbeTime:      now,
						LastTransitionTime: now,
					},
				},
			}
			Expect(k8sClient.Status().Update(ctx, jobPatch)).To(Succeed())

			// Reconcile 2: seam body sees Succeeded Job and sets Initialized.
			_, err = reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: name})
			Expect(err).NotTo(HaveOccurred())

			Expect(k8sClient.Get(ctx, name, fetched)).To(Succeed())
			Expect(fetched.Status.Phase).To(Equal("Initialized"),
				"Phase should be Initialized after init Job success")
		})

		It("TestProjectReconciler_OnInitJobSuccess_DoesNotRevertPhaseFromComplete", func() {
			ns := "default"
			pvcName := "tide-projects-no-revert"
			makeTestBoundPVC(ctx, pvcName, ns)
			DeferCleanup(func() {
				pvc := &corev1.PersistentVolumeClaim{}
				pvc.Name = pvcName
				pvc.Namespace = ns
				_ = k8sClient.Delete(ctx, pvc)
			})

			project := &tideprojectv1alpha2.Project{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-no-revert-from-complete",
					Namespace: ns,
				},
				Spec: tideprojectv1alpha2.ProjectSpec{SchemaRevision: "v1alpha2",
					TargetRepo: "https://github.com/example/repo.git",
				},
			}
			Expect(k8sClient.Create(ctx, project)).To(Succeed())
			DeferCleanup(func() { _ = k8sClient.Delete(ctx, project) })

			reconciler := newTestProjectReconciler()
			reconciler.SharedPVCName = pvcName
			name := types.NamespacedName{Name: project.Name, Namespace: project.Namespace}

			// Reconcile 1: add finalizer.
			_, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: name})
			Expect(err).NotTo(HaveOccurred())

			fetched := &tideprojectv1alpha2.Project{}
			Expect(k8sClient.Get(ctx, name, fetched)).To(Succeed())

			// Pre-create a Succeeded init Job (mirror lines 243-271 of the canonical test).
			completedJob := buildSucceededInitJob(fetched, pvcName)
			Expect(k8sClient.Create(ctx, completedJob)).To(Succeed())
			now := metav1.Now()
			jobPatch := completedJob.DeepCopy()
			jobPatch.Status = batchv1.JobStatus{
				Succeeded:      1,
				StartTime:      &now,
				CompletionTime: &now,
				Conditions: []batchv1.JobCondition{
					{
						Type:               batchv1.JobSuccessCriteriaMet,
						Status:             corev1.ConditionTrue,
						LastProbeTime:      now,
						LastTransitionTime: now,
						Reason:             "CompletionsReached",
					},
					{
						Type:               batchv1.JobComplete,
						Status:             corev1.ConditionTrue,
						LastProbeTime:      now,
						LastTransitionTime: now,
					},
				},
			}
			Expect(k8sClient.Status().Update(ctx, jobPatch)).To(Succeed())

			// Reconcile 2: seam body observes Succeeded Job → Phase=Initialized.
			_, err = reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: name})
			Expect(err).NotTo(HaveOccurred())

			Expect(k8sClient.Get(ctx, name, fetched)).To(Succeed())
			Expect(fetched.Status.Phase).To(Equal("Initialized"),
				"Phase should be Initialized after init Job success")

			// Cascade 13 contract: manually advance Phase to Complete (simulating
			// the push_lease_test's forcePushReady helper). The init Job is STILL
			// Succeeded in the cluster — a subsequent Reconcile will re-enter
			// handleInitJobCompletion's isJobSucceeded branch.
			statusPatch := client.MergeFrom(fetched.DeepCopy())
			fetched.Status.Phase = tideprojectv1alpha2.PhaseComplete
			Expect(k8sClient.Status().Patch(ctx, fetched, statusPatch)).To(Succeed())

			// Sanity check the patch landed.
			Expect(k8sClient.Get(ctx, name, fetched)).To(Succeed())
			Expect(fetched.Status.Phase).To(Equal("Complete"),
				"pre-flight: Phase should be Complete after manual patch")

			// Reconcile 3: handleInitJobCompletion fires AGAIN (init Job still
			// Succeeded). Without the cascade-13 guard, this would re-stomp
			// Phase back to Initialized. With the guard, Phase stays at Complete.
			_, err = reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: name})
			Expect(err).NotTo(HaveOccurred())

			Expect(k8sClient.Get(ctx, name, fetched)).To(Succeed())
			Expect(fetched.Status.Phase).To(Equal("Complete"),
				"Cascade 13: Phase MUST NOT revert from Complete to Initialized — handleInitJobCompletion must be idempotent against forward-progressed Phase states")
		})

		It("TestProjectReconciler_OnInitJobFailure_SetsPhaseInitFailed", func() {
			ns := "default"
			pvcName := "tide-projects-initfail"
			makeTestBoundPVC(ctx, pvcName, ns)
			DeferCleanup(func() {
				pvc := &corev1.PersistentVolumeClaim{}
				pvc.Name = pvcName
				pvc.Namespace = ns
				_ = k8sClient.Delete(ctx, pvc)
			})

			project := &tideprojectv1alpha2.Project{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-init-fail",
					Namespace: ns,
				},
				Spec: tideprojectv1alpha2.ProjectSpec{SchemaRevision: "v1alpha2",
					TargetRepo: "https://github.com/example/repo.git",
				},
			}
			Expect(k8sClient.Create(ctx, project)).To(Succeed())
			DeferCleanup(func() { _ = k8sClient.Delete(ctx, project) })

			reconciler := newTestProjectReconciler()
			reconciler.SharedPVCName = pvcName
			name := types.NamespacedName{Name: project.Name, Namespace: project.Namespace}

			// Reconcile 1: add finalizer.
			_, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: name})
			Expect(err).NotTo(HaveOccurred())

			fetched := &tideprojectv1alpha2.Project{}
			Expect(k8sClient.Get(ctx, name, fetched)).To(Succeed())

			// Pre-create a Failed init Job.
			failedJob := buildFailedInitJob(fetched, pvcName)
			Expect(k8sClient.Create(ctx, failedJob)).To(Succeed())
			// Patch Job status to Failed.
			// K8s 1.36 requires: FailureTarget=True before Failed=True, and startTime.
			failNow := metav1.Now()
			failPatch := failedJob.DeepCopy()
			failPatch.Status = batchv1.JobStatus{
				Failed:    3,
				StartTime: &failNow,
				Conditions: []batchv1.JobCondition{
					{
						Type:               batchv1.JobFailureTarget,
						Status:             corev1.ConditionTrue,
						LastProbeTime:      failNow,
						LastTransitionTime: failNow,
						Reason:             "BackoffLimitExceeded",
					},
					{
						Type:               batchv1.JobFailed,
						Status:             corev1.ConditionTrue,
						LastProbeTime:      failNow,
						LastTransitionTime: failNow,
						Reason:             "BackoffLimitExceeded",
					},
				},
			}
			Expect(k8sClient.Status().Update(ctx, failPatch)).To(Succeed())

			// Reconcile 2: seam body sees Failed Job and sets InitFailed.
			_, err = reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: name})
			Expect(err).NotTo(HaveOccurred())

			Expect(k8sClient.Get(ctx, name, fetched)).To(Succeed())
			Expect(fetched.Status.Phase).To(Equal("InitFailed"),
				"Phase should be InitFailed after init Job failure")
		})
	})

	Describe("Budget gate", func() {
		It("TestProjectReconciler_BudgetCapExceeded_SetsBudgetExceeded", func() {
			ns := "default"
			pvcName := "tide-projects-budget-cap"
			makeTestBoundPVC(ctx, pvcName, ns)
			DeferCleanup(func() {
				pvc := &corev1.PersistentVolumeClaim{}
				pvc.Name = pvcName
				pvc.Namespace = ns
				_ = k8sClient.Delete(ctx, pvc)
			})

			project := &tideprojectv1alpha2.Project{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-budget-exceeded",
					Namespace: ns,
				},
				Spec: tideprojectv1alpha2.ProjectSpec{SchemaRevision: "v1alpha2",
					TargetRepo: "https://github.com/example/repo.git",
					Budget: tideprojectv1alpha2.BudgetConfig{
						AbsoluteCapCents: 100,
					},
				},
			}
			Expect(k8sClient.Create(ctx, project)).To(Succeed())
			DeferCleanup(func() { _ = k8sClient.Delete(ctx, project) })

			name := types.NamespacedName{Name: project.Name, Namespace: project.Namespace}
			fetched := &tideprojectv1alpha2.Project{}
			Expect(k8sClient.Get(ctx, name, fetched)).To(Succeed())
			// Patch status to exceed the cap.
			statusPatch := fetched.DeepCopy()
			statusPatch.Status.Budget.CostSpentCents = 200 // exceeds cap of 100
			Expect(k8sClient.Status().Update(ctx, statusPatch)).To(Succeed())

			reconciler := newTestProjectReconciler()
			reconciler.SharedPVCName = pvcName

			// Reconcile 1: add finalizer.
			_, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: name})
			Expect(err).NotTo(HaveOccurred())
			// Reconcile 2: seam body — budget cap should be detected and phase set.
			_, err = reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: name})
			Expect(err).NotTo(HaveOccurred())

			Expect(k8sClient.Get(ctx, name, fetched)).To(Succeed())
			Expect(fetched.Status.Phase).To(Equal("BudgetExceeded"),
				"Phase should be BudgetExceeded when cap is exceeded")
		})

		It("TestProjectReconciler_BypassAnnotation_ClearsBudgetExceeded", func() {
			ns := "default"
			pvcName := "tide-projects-bypass-oneshot"
			makeTestBoundPVC(ctx, pvcName, ns)
			DeferCleanup(func() {
				pvc := &corev1.PersistentVolumeClaim{}
				pvc.Name = pvcName
				pvc.Namespace = ns
				_ = k8sClient.Delete(ctx, pvc)
			})

			project := &tideprojectv1alpha2.Project{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-bypass-oneshot",
					Namespace: ns,
					Annotations: map[string]string{
						"tideproject.k8s/bypass-budget": "true",
					},
				},
				Spec: tideprojectv1alpha2.ProjectSpec{SchemaRevision: "v1alpha2",
					TargetRepo: "https://github.com/example/repo.git",
					Budget: tideprojectv1alpha2.BudgetConfig{
						AbsoluteCapCents: 100,
					},
				},
			}
			Expect(k8sClient.Create(ctx, project)).To(Succeed())
			DeferCleanup(func() { _ = k8sClient.Delete(ctx, project) })

			name := types.NamespacedName{Name: project.Name, Namespace: project.Namespace}
			fetched := &tideprojectv1alpha2.Project{}
			Expect(k8sClient.Get(ctx, name, fetched)).To(Succeed())

			// Patch status to BudgetExceeded with overspend.
			statusPatch := fetched.DeepCopy()
			statusPatch.Status.Phase = "BudgetExceeded"
			statusPatch.Status.Budget.CostSpentCents = 200
			Expect(k8sClient.Status().Update(ctx, statusPatch)).To(Succeed())

			// BYPASS-01: Simulate initialized project — set BranchName so bypass
			// targets PhaseRunning (not PhasePending).
			Expect(k8sClient.Get(ctx, name, fetched)).To(Succeed())
			branchPatch := client.MergeFrom(fetched.DeepCopy())
			fetched.Status.Git.BranchName = "tide/run-test-bypass-oneshot-1000000000"
			Expect(k8sClient.Status().Patch(ctx, fetched, branchPatch)).To(Succeed())

			reconciler := newTestProjectReconciler()
			reconciler.SharedPVCName = pvcName

			// Reconcile 1: add finalizer.
			_, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: name})
			Expect(err).NotTo(HaveOccurred())
			// Reconcile 2: bypass should clear BudgetExceeded.
			_, err = reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: name})
			Expect(err).NotTo(HaveOccurred())

			Expect(k8sClient.Get(ctx, name, fetched)).To(Succeed())
			Expect(fetched.Status.Phase).NotTo(Equal("BudgetExceeded"),
				"Phase should be cleared from BudgetExceeded when one-shot bypass annotation is present")
			// BYPASS-01: bypass of an initialized project must target PhaseRunning, not PhasePending.
			Expect(fetched.Status.Phase).To(Equal(tideprojectv1alpha2.PhaseRunning),
				"Phase must be Running (not Pending) after bypass clears BudgetExceeded on an initialized project")
			// One-shot bypass annotation should be consumed.
			Expect(fetched.Annotations).NotTo(HaveKey("tideproject.k8s/bypass-budget"),
				"one-shot bypass annotation should be consumed after bypass")
		})

		It("TestProjectReconciler_BypassUntilAnnotation_TTLHonored", func() {
			ns := "default"
			pvcName := "tide-projects-bypass-ttl"
			makeTestBoundPVC(ctx, pvcName, ns)
			DeferCleanup(func() {
				pvc := &corev1.PersistentVolumeClaim{}
				pvc.Name = pvcName
				pvc.Namespace = ns
				_ = k8sClient.Delete(ctx, pvc)
			})

			futureTime := time.Now().Add(time.Hour).Format(time.RFC3339)
			project := &tideprojectv1alpha2.Project{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-bypass-ttl",
					Namespace: ns,
					Annotations: map[string]string{
						"tideproject.k8s/bypass-budget-until": futureTime,
					},
				},
				Spec: tideprojectv1alpha2.ProjectSpec{SchemaRevision: "v1alpha2",
					TargetRepo: "https://github.com/example/repo.git",
					Budget: tideprojectv1alpha2.BudgetConfig{
						AbsoluteCapCents: 100,
					},
				},
			}
			Expect(k8sClient.Create(ctx, project)).To(Succeed())
			DeferCleanup(func() { _ = k8sClient.Delete(ctx, project) })

			name := types.NamespacedName{Name: project.Name, Namespace: project.Namespace}
			fetched := &tideprojectv1alpha2.Project{}
			Expect(k8sClient.Get(ctx, name, fetched)).To(Succeed())

			// Patch status to BudgetExceeded with overspend.
			statusPatch := fetched.DeepCopy()
			statusPatch.Status.Phase = "BudgetExceeded"
			statusPatch.Status.Budget.CostSpentCents = 200
			Expect(k8sClient.Status().Update(ctx, statusPatch)).To(Succeed())

			reconciler := newTestProjectReconciler()
			reconciler.SharedPVCName = pvcName

			// Reconcile 1: add finalizer.
			_, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: name})
			Expect(err).NotTo(HaveOccurred())
			// Reconcile 2: future TTL bypass should clear BudgetExceeded.
			_, err = reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: name})
			Expect(err).NotTo(HaveOccurred())

			Expect(k8sClient.Get(ctx, name, fetched)).To(Succeed())
			Expect(fetched.Status.Phase).NotTo(Equal("BudgetExceeded"),
				"future TTL bypass should clear BudgetExceeded")

			// Now update to past TTL — should re-enter BudgetExceeded on next reconcile.
			pastTime := time.Now().Add(-time.Hour).Format(time.RFC3339)
			Expect(k8sClient.Get(ctx, name, fetched)).To(Succeed())
			metaPatch := fetched.DeepCopy()
			metaPatch.Annotations["tideproject.k8s/bypass-budget-until"] = pastTime
			Expect(k8sClient.Update(ctx, metaPatch)).To(Succeed())

			// Reset status so reconciler can re-set BudgetExceeded.
			Expect(k8sClient.Get(ctx, name, fetched)).To(Succeed())
			resetPatch := fetched.DeepCopy()
			resetPatch.Status.Budget.CostSpentCents = 200
			resetPatch.Status.Phase = "Pending"
			Expect(k8sClient.Status().Update(ctx, resetPatch)).To(Succeed())

			_, err = reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: name})
			Expect(err).NotTo(HaveOccurred())

			Expect(k8sClient.Get(ctx, name, fetched)).To(Succeed())
			Expect(fetched.Status.Phase).To(Equal("BudgetExceeded"),
				"expired TTL bypass should not prevent BudgetExceeded phase from being re-set")
		})

		// BYPASS-04: raise-absolute-cap-alone resume sticks (D-04 acknowledged-spend baseline).
		// After a bypass, raising only the absolute cap (leaving rolling-window numerically
		// exceeded by already-incurred spend) should NOT immediately re-halt on the next reconcile.
		It("BYPASS-04: raise-absolute-cap-alone resume stays Running (no re-halt on old spend)", Label("envtest"), func() {
			ns := "default"
			pvcName := "tide-projects-bypass04-raise-abs"
			makeTestBoundPVC(ctx, pvcName, ns)
			DeferCleanup(func() {
				pvc := &corev1.PersistentVolumeClaim{}
				pvc.Name = pvcName
				pvc.Namespace = ns
				_ = k8sClient.Delete(ctx, pvc)
			})

			// Project: absoluteCap=100, rollingCap=100; spend=200 exceeds both.
			project := &tideprojectv1alpha2.Project{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-bypass04-raise-abs",
					Namespace: ns,
					Annotations: map[string]string{
						"tideproject.k8s/bypass-budget": "true",
					},
				},
				Spec: tideprojectv1alpha2.ProjectSpec{SchemaRevision: "v1alpha2",
					TargetRepo: "https://github.com/example/repo.git",
					Budget: tideprojectv1alpha2.BudgetConfig{
						AbsoluteCapCents:      100,
						RollingWindowCapCents: 100,
					},
				},
			}
			Expect(k8sClient.Create(ctx, project)).To(Succeed())
			DeferCleanup(func() { _ = k8sClient.Delete(ctx, project) })

			name := types.NamespacedName{Name: project.Name, Namespace: project.Namespace}
			fetched := &tideprojectv1alpha2.Project{}
			Expect(k8sClient.Get(ctx, name, fetched)).To(Succeed())

			// Set status: BudgetExceeded phase, spend=200, initialized project.
			statusPatch := client.MergeFrom(fetched.DeepCopy())
			fetched.Status.Phase = tideprojectv1alpha2.PhaseBudgetExceeded
			fetched.Status.Budget.CostSpentCents = 200
			fetched.Status.Git.BranchName = "tide/run-test-bypass04-raise-abs-1000000000"
			Expect(k8sClient.Status().Patch(ctx, fetched, statusPatch)).To(Succeed())

			reconciler := newTestProjectReconciler()
			reconciler.SharedPVCName = pvcName

			// Reconcile 1: add finalizer.
			_, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: name})
			Expect(err).NotTo(HaveOccurred())

			// Reconcile 2: bypass clears BudgetExceeded; D-04 sets BypassBaselineCents=200.
			_, err = reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: name})
			Expect(err).NotTo(HaveOccurred())

			Expect(k8sClient.Get(ctx, name, fetched)).To(Succeed())
			Expect(fetched.Status.Phase).To(Equal(tideprojectv1alpha2.PhaseRunning),
				"Phase should be Running after bypass")
			// Verify baseline was recorded.
			Expect(fetched.Status.Budget.BypassBaselineCents).To(BeNumerically("==", 200),
				"BypassBaselineCents must be set to CostSpentCents at bypass time")

			// Now raise ONLY the absolute cap to 300 (rolling=100 still below spend=200).
			// No new spend added. This simulates "operator raises absolute cap alone".
			Expect(k8sClient.Get(ctx, name, fetched)).To(Succeed())
			specPatch := client.MergeFrom(fetched.DeepCopy())
			fetched.Spec.Budget.AbsoluteCapCents = 300
			Expect(k8sClient.Patch(ctx, fetched, specPatch)).To(Succeed())

			// Reconcile 3: with baseline guard, re-halt must NOT fire (spend==baseline, no new spend).
			_, err = reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: name})
			Expect(err).NotTo(HaveOccurred())

			// Use Consistently to prove no re-halt across multiple reconciles.
			Consistently(func(g Gomega) {
				var refreshed tideprojectv1alpha2.Project
				g.Expect(k8sClient.Get(ctx, name, &refreshed)).To(Succeed())
				g.Expect(refreshed.Status.Phase).NotTo(Equal(tideprojectv1alpha2.PhaseBudgetExceeded),
					"Phase must NOT re-halt to BudgetExceeded when only absolute cap was raised and no new spend occurred")
			}, 2*time.Second, 200*time.Millisecond).Should(Succeed())
		})

		// BYPASS-04: re-halt fires on genuine new spend crossing rolling-window cap,
		// with Reason=RollingWindowCapReached (D-04 which-cap observability).
		It("BYPASS-04: re-halt on new rolling-window spend carries RollingWindowCapReached reason", Label("envtest"), func() {
			ns := "default"
			pvcName := "tide-projects-bypass04-rolling"
			makeTestBoundPVC(ctx, pvcName, ns)
			DeferCleanup(func() {
				pvc := &corev1.PersistentVolumeClaim{}
				pvc.Name = pvcName
				pvc.Namespace = ns
				_ = k8sClient.Delete(ctx, pvc)
			})

			// Project: absoluteCap=500 (high, won't be hit), rollingCap=100.
			project := &tideprojectv1alpha2.Project{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-bypass04-rolling",
					Namespace: ns,
					Annotations: map[string]string{
						"tideproject.k8s/bypass-budget": "true",
					},
				},
				Spec: tideprojectv1alpha2.ProjectSpec{SchemaRevision: "v1alpha2",
					TargetRepo: "https://github.com/example/repo.git",
					Budget: tideprojectv1alpha2.BudgetConfig{
						AbsoluteCapCents:      500,
						RollingWindowCapCents: 100,
					},
				},
			}
			Expect(k8sClient.Create(ctx, project)).To(Succeed())
			DeferCleanup(func() { _ = k8sClient.Delete(ctx, project) })

			name := types.NamespacedName{Name: project.Name, Namespace: project.Namespace}
			fetched := &tideprojectv1alpha2.Project{}
			Expect(k8sClient.Get(ctx, name, fetched)).To(Succeed())

			// Set status: BudgetExceeded, spend=150 (only rolling cap exceeded), initialized.
			statusPatch := client.MergeFrom(fetched.DeepCopy())
			fetched.Status.Phase = tideprojectv1alpha2.PhaseBudgetExceeded
			fetched.Status.Budget.CostSpentCents = 150
			fetched.Status.Git.BranchName = "tide/run-test-bypass04-rolling-1000000000"
			Expect(k8sClient.Status().Patch(ctx, fetched, statusPatch)).To(Succeed())

			reconciler := newTestProjectReconciler()
			reconciler.SharedPVCName = pvcName

			// Reconcile 1: add finalizer.
			_, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: name})
			Expect(err).NotTo(HaveOccurred())

			// Reconcile 2: bypass clears BudgetExceeded; D-04 sets BypassBaselineCents=150.
			_, err = reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: name})
			Expect(err).NotTo(HaveOccurred())

			Expect(k8sClient.Get(ctx, name, fetched)).To(Succeed())
			Expect(fetched.Status.Phase).To(Equal(tideprojectv1alpha2.PhaseRunning),
				"Phase should be Running after bypass")

			// Stamp NEW spend past baseline and rolling cap: 151 > baseline 150 AND > rolling 100.
			// AbsoluteCapCents=500, spend=151 → absolute NOT exceeded; rolling IS exceeded by new spend.
			stampBudgetSpend(ctx, project.Name, 151)

			// Reconcile 3: re-halt fires with RollingWindowCapReached reason.
			_, err = reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: name})
			Expect(err).NotTo(HaveOccurred())

			Eventually(func(g Gomega) {
				var refreshed tideprojectv1alpha2.Project
				g.Expect(k8sClient.Get(ctx, name, &refreshed)).To(Succeed())
				g.Expect(refreshed.Status.Phase).To(Equal(tideprojectv1alpha2.PhaseBudgetExceeded),
					"Phase must be BudgetExceeded after new spend crosses rolling-window cap")
				// Assert which-cap reason is RollingWindowCapReached.
				var cond *metav1.Condition
				for i := range refreshed.Status.Conditions {
					if refreshed.Status.Conditions[i].Type == tideprojectv1alpha2.ConditionBudgetExceeded {
						cond = &refreshed.Status.Conditions[i]
						break
					}
				}
				g.Expect(cond).NotTo(BeNil(), "ConditionBudgetExceeded must be set")
				g.Expect(cond.Reason).To(Equal("RollingWindowCapReached"),
					"Reason must be RollingWindowCapReached when rolling cap fires (not AbsoluteCapReached)")
			}, 5*time.Second, 200*time.Millisecond).Should(Succeed())
		})
	})

	Describe("Shared PVC guard", func() {
		It("TestProjectReconciler_NoSharedPVC_RequeuesAfter30s", func() {
			ns := "default"
			// Deliberately do NOT create the shared PVC for this test.

			project := &tideprojectv1alpha2.Project{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-no-pvc",
					Namespace: ns,
				},
				Spec: tideprojectv1alpha2.ProjectSpec{SchemaRevision: "v1alpha2",
					TargetRepo: "https://github.com/example/repo.git",
				},
			}
			Expect(k8sClient.Create(ctx, project)).To(Succeed())
			DeferCleanup(func() { _ = k8sClient.Delete(ctx, project) })

			reconciler := newTestProjectReconciler()
			// Use a name that cannot exist.
			reconciler.SharedPVCName = "tide-projects-nonexistent-12345"
			name := types.NamespacedName{Name: project.Name, Namespace: project.Namespace}

			// Reconcile 1: add finalizer.
			_, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: name})
			Expect(err).NotTo(HaveOccurred())

			// Reconcile 2: seam body — missing PVC should trigger RequeueAfter:30s.
			result, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: name})
			Expect(err).NotTo(HaveOccurred())
			Expect(result.RequeueAfter).To(Equal(30*time.Second),
				"should requeue after 30s when shared PVC is missing (Pitfall 1 — non-blocking)")

			// No init Job should have been created.
			fetched := &tideprojectv1alpha2.Project{}
			Expect(k8sClient.Get(ctx, name, fetched)).To(Succeed())
			var jobList batchv1.JobList
			Expect(k8sClient.List(ctx, &jobList, client.InNamespace(ns))).To(Succeed())
			for _, j := range jobList.Items {
				Expect(j.Name).NotTo(HavePrefix("tide-init-"+string(fetched.UID)),
					"no init Job should be created when shared PVC is missing")
			}
		})
	})
})

var _ = Describe("Project Controller", func() {
	Context("When reconciling a resource", func() {
		ctx := context.Background()

		It("accepts a valid Project CRD apply", func() {
			project := &tideprojectv1alpha2.Project{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "valid-project",
					Namespace: "default",
				},
				Spec: tideprojectv1alpha2.ProjectSpec{SchemaRevision: "v1alpha2",
					TargetRepo: "https://github.com/example/repo.git",
				},
			}
			Expect(k8sClient.Create(ctx, project)).To(Succeed())

			By("Cleanup")
			Expect(k8sClient.Delete(ctx, project)).To(Succeed())
		})

		It("rejects a Project with an invalid targetRepo (CEL XValidation)", func() {
			invalid := &tideprojectv1alpha2.Project{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "invalid-project",
					Namespace: "default",
				},
				Spec: tideprojectv1alpha2.ProjectSpec{SchemaRevision: "v1alpha2",
					TargetRepo: "not-a-valid-url",
				},
			}
			err := k8sClient.Create(ctx, invalid)
			Expect(err).To(HaveOccurred())
			Expect(apierrors.IsInvalid(err)).To(BeTrue(),
				"expected CEL XValidation rejection, got: %v", err)
		})

		It("sets the finalizer on create (CTRL-05)", func() {
			project := &tideprojectv1alpha2.Project{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "finalizer-set",
					Namespace: "default",
				},
				Spec: tideprojectv1alpha2.ProjectSpec{SchemaRevision: "v1alpha2",
					TargetRepo: "https://github.com/example/repo.git",
				},
			}
			Expect(k8sClient.Create(ctx, project)).To(Succeed())

			reconciler := &ProjectReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			name := types.NamespacedName{Name: project.Name, Namespace: project.Namespace}

			// First reconcile: adds the finalizer and returns.
			_, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: name})
			Expect(err).NotTo(HaveOccurred())

			fetched := &tideprojectv1alpha2.Project{}
			Expect(k8sClient.Get(ctx, name, fetched)).To(Succeed())
			Expect(fetched.Finalizers).To(ContainElement("tideproject.k8s/project-cleanup"))

			By("Cleanup")
			Expect(k8sClient.Delete(ctx, fetched)).To(Succeed())
			// Drive cleanup so the finalizer is removed and GC proceeds.
			_, err = reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: name})
			Expect(err).NotTo(HaveOccurred())
		})

		It("removes finalizer on deletion (TestFinalizerLifecycle, Pitfall 21)", func() {
			project := &tideprojectv1alpha2.Project{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "finalizer-lifecycle",
					Namespace: "default",
				},
				Spec: tideprojectv1alpha2.ProjectSpec{SchemaRevision: "v1alpha2",
					TargetRepo: "https://github.com/example/repo.git",
				},
			}
			Expect(k8sClient.Create(ctx, project)).To(Succeed())

			reconciler := &ProjectReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}
			name := types.NamespacedName{Name: project.Name, Namespace: project.Namespace}

			// Add finalizer.
			_, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: name})
			Expect(err).NotTo(HaveOccurred())

			fetched := &tideprojectv1alpha2.Project{}
			Expect(k8sClient.Get(ctx, name, fetched)).To(Succeed())
			Expect(fetched.Finalizers).To(ContainElement("tideproject.k8s/project-cleanup"))

			// Issue a delete — object enters Terminating state because of the finalizer.
			Expect(k8sClient.Delete(ctx, fetched)).To(Succeed())

			Expect(k8sClient.Get(ctx, name, fetched)).To(Succeed())
			Expect(fetched.DeletionTimestamp.IsZero()).To(BeFalse(), "expected DeletionTimestamp set")

			// Drive cleanup — HandleDeletion runs the no-op callback and removes the finalizer.
			_, err = reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: name})
			Expect(err).NotTo(HaveOccurred())

			// The object should be GC'd within a short window.
			Eventually(func() bool {
				err := k8sClient.Get(ctx, name, &tideprojectv1alpha2.Project{})
				return apierrors.IsNotFound(err)
			}, 10*time.Second, 250*time.Millisecond).Should(BeTrue(),
				"expected Project to be garbage-collected after finalizer removal")
		})
	})
})

// buildSucceededInitJob builds a pre-created init Job for testing — Spec only,
// caller patches Status separately since envtest requires separate status updates.
func buildSucceededInitJob(project *tideprojectv1alpha2.Project, _ string) *batchv1.Job {
	backoffLimit := int32(2)
	ttl := int32(300)
	return &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "tide-init-" + string(project.UID),
			Namespace: project.Namespace,
		},
		Spec: batchv1.JobSpec{
			BackoffLimit:            &backoffLimit,
			TTLSecondsAfterFinished: &ttl,
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					RestartPolicy: corev1.RestartPolicyNever,
					Containers: []corev1.Container{
						{
							Name:    "init",
							Image:   "busybox:1.36",
							Command: []string{"sh", "-c", "mkdir -p /workspace/repo /workspace/artifacts /workspace/envelopes"},
						},
					},
				},
			},
		},
	}
}

// buildFailedInitJob builds a pre-created init Job for testing — Spec only.
func buildFailedInitJob(project *tideprojectv1alpha2.Project, _ string) *batchv1.Job {
	backoffLimit := int32(2)
	ttl := int32(300)
	return &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "tide-init-" + string(project.UID),
			Namespace: project.Namespace,
		},
		Spec: batchv1.JobSpec{
			BackoffLimit:            &backoffLimit,
			TTLSecondsAfterFinished: &ttl,
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					RestartPolicy: corev1.RestartPolicyNever,
					Containers: []corev1.Container{
						{
							Name:    "init",
							Image:   "busybox:1.36",
							Command: []string{"sh", "-c", "exit 1"},
						},
					},
				},
			},
		},
	}
}

// Ensure ctrl is used to avoid unused import errors.
var _ ctrl.Result
