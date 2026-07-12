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
	"encoding/json"
	"fmt"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	tideprojectv1alpha3 "github.com/jsquirrelz/tide/api/v1alpha3"
)

// BASE-02 (D-06/D-07) + BASE-03 (D-11): the ProjectReconciler classifies an
// unresolvable-baseRef clone failure into a generation-scoped halt (survives
// TTL GC, releases on spec edit), keeps delete-and-re-dispatch for every other
// clone-failure class, and stamps status.git.baseSHA from the clone success
// envelope in the CloneComplete patch (read-before-flip). Layer A behavioral
// lock beside the BYPASS-02 clone-idempotency suite.
var _ = Describe("BASE-02/BASE-03 baseRef classification + baseSHA stamp", Label("envtest"), func() {
	const pvcName = "tide-projects-baseref-halt"
	ctx := context.Background()

	nn := func(name string) types.NamespacedName {
		return types.NamespacedName{Name: name, Namespace: "default"}
	}

	BeforeEach(func() {
		ensurePVC(ctx, pvcName, "default")
	})

	AfterEach(func() {
		var jobs batchv1.JobList
		_ = k8sClient.List(ctx, &jobs, client.InNamespace("default"))
		for i := range jobs.Items {
			_ = k8sClient.Delete(ctx, &jobs.Items[i], client.PropagationPolicy(metav1.DeletePropagationBackground))
		}
		var pods corev1.PodList
		_ = k8sClient.List(ctx, &pods, client.InNamespace("default"))
		for i := range pods.Items {
			_ = k8sClient.Delete(ctx, &pods.Items[i], client.PropagationPolicy(metav1.DeletePropagationBackground))
		}
	})

	newReconciler := func() *ProjectReconciler {
		return &ProjectReconciler{
			Client: k8sClient,
			Scheme: k8sClient.Scheme(),
			Deps: PlannerReconcilerDeps{
				Dispatcher:    &stubDispatcher{},
				TidePushImage: "ghcr.io/jsquirrelz/tide-push:test",
			},
			MaxConcurrentReconciles: 1,
			SharedPVCName:           pvcName,
		}
	}

	// makeProject creates a Project with the given baseRef and cleans it up.
	makeProject := func(name, baseRef string) *tideprojectv1alpha3.Project {
		proj := &tideprojectv1alpha3.Project{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
			Spec: tideprojectv1alpha3.ProjectSpec{
				SchemaRevision: "v1alpha3",
				TargetRepo:     "https://github.com/example/test.git",
				Git: &tideprojectv1alpha3.GitConfig{
					RepoURL:        "https://github.com/example/test.git",
					CredsSecretRef: "test-creds",
					BaseRef:        baseRef,
				},
			},
		}
		Expect(k8sClient.Create(ctx, proj)).To(Succeed())
		waitForCacheSync(name, "default", &tideprojectv1alpha3.Project{})
		DeferCleanup(func() {
			p := &tideprojectv1alpha3.Project{}
			if err := k8sClient.Get(ctx, nn(name), p); err == nil {
				p.Finalizers = nil
				_ = k8sClient.Update(ctx, p)
				_ = k8sClient.Delete(ctx, p)
			}
		})
		return proj
	}

	reconcileN := func(r *ProjectReconciler, name string, n int) reconcile.Result {
		var last reconcile.Result
		for range n {
			res, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: nn(name)})
			if err != nil && !apierrors.IsConflict(err) {
				Expect(err).NotTo(HaveOccurred())
			}
			last = res
		}
		return last
	}

	// advanceToCloneJob drives reconciles, succeeding the init Job as it appears,
	// until the deterministic tide-clone-<uid> Job has been dispatched. Returns
	// the refreshed Project and the clone Job name.
	advanceToCloneJob := func(r *ProjectReconciler, name string) (tideprojectv1alpha3.Project, string) {
		var p tideprojectv1alpha3.Project
		for range 12 {
			if _, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: nn(name)}); err != nil && !apierrors.IsConflict(err) {
				Expect(err).NotTo(HaveOccurred())
			}
			if err := k8sClient.Get(ctx, nn(name), &p); err == nil {
				initJobName := fmt.Sprintf("tide-init-%s", p.UID)
				var j batchv1.Job
				if err := k8sClient.Get(ctx, nn(initJobName), &j); err == nil && !isJobSucceeded(&j) {
					_ = makeFakeJobTerminal(ctx, k8sClient, initJobName, "default", true)
				}
			}
		}
		Expect(k8sClient.Get(ctx, nn(name), &p)).To(Succeed())
		cloneJobName := fmt.Sprintf("tide-clone-%s", p.UID)
		Eventually(func() error {
			return k8sClient.Get(ctx, nn(cloneJobName), &batchv1.Job{})
		}, 5*time.Second, 200*time.Millisecond).Should(Succeed(),
			"clone Job should be dispatched")
		return p, cloneJobName
	}

	// fakeCloneJobPod creates a Pod labeled job-name=<cloneJobName> carrying the
	// marshaled envelope on ContainerStatuses[0].State.Terminated.Message —
	// exactly the surface readJobPushEnvelope parses. envtest runs no kubelet, so
	// the container status is fabricated directly.
	fakeCloneJobPod := func(cloneJobName string, env pushResultEnvelope, phase corev1.PodPhase) {
		raw, err := json.Marshal(env)
		Expect(err).NotTo(HaveOccurred())
		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      cloneJobName + "-pod",
				Namespace: "default",
				Labels:    map[string]string{"job-name": cloneJobName},
			},
			Spec: corev1.PodSpec{
				RestartPolicy: corev1.RestartPolicyNever,
				Containers: []corev1.Container{
					{Name: pushContainerName, Image: "ghcr.io/jsquirrelz/tide-push:test"},
				},
			},
		}
		Expect(k8sClient.Create(ctx, pod)).To(Succeed())
		statusPatch := client.MergeFrom(pod.DeepCopy())
		pod.Status.Phase = phase
		pod.Status.ContainerStatuses = []corev1.ContainerStatus{
			{
				Name: pushContainerName,
				State: corev1.ContainerState{
					Terminated: &corev1.ContainerStateTerminated{
						ExitCode: int32(env.ExitCode),
						Reason:   "Completed",
						Message:  string(raw),
					},
				},
			},
		}
		Expect(k8sClient.Status().Patch(ctx, pod, statusPatch)).To(Succeed())
	}

	// markCloneFailed transitions the clone Job to terminal-Failed. K8s status
	// validation: Failed=True requires FailureTarget=True first and forbids a
	// completionTime on a failed Job.
	markCloneFailed := func(cloneJobName string) {
		Eventually(func() error {
			var j batchv1.Job
			if e := k8sClient.Get(ctx, nn(cloneJobName), &j); e != nil {
				return e
			}
			now := metav1.Now()
			j.Status.StartTime = &now
			j.Status.Failed = 1
			j.Status.Conditions = []batchv1.JobCondition{
				{Type: batchv1.JobFailureTarget, Status: corev1.ConditionTrue, LastTransitionTime: now, Reason: "BackoffLimitExceeded"},
				{Type: batchv1.JobFailed, Status: corev1.ConditionTrue, LastTransitionTime: now, Reason: "BackoffLimitExceeded"},
			}
			return k8sClient.Status().Update(ctx, &j)
		}, 5*time.Second, 200*time.Millisecond).Should(Succeed())
	}

	// markCloneSucceeded transitions the clone Job to terminal-Succeeded with a
	// completionTime `completedAgo` in the past (startTime one minute before
	// that, satisfying startTime <= completionTime).
	markCloneSucceeded := func(cloneJobName string, completedAgo time.Duration) {
		Eventually(func() error {
			var j batchv1.Job
			if e := k8sClient.Get(ctx, nn(cloneJobName), &j); e != nil {
				return e
			}
			start := metav1.NewTime(time.Now().Add(-completedAgo - time.Minute))
			done := metav1.NewTime(time.Now().Add(-completedAgo))
			j.Status.StartTime = &start
			j.Status.CompletionTime = &done
			j.Status.Succeeded = 1
			j.Status.Conditions = []batchv1.JobCondition{
				{Type: batchv1.JobSuccessCriteriaMet, Status: corev1.ConditionTrue, LastTransitionTime: start},
				{Type: batchv1.JobComplete, Status: corev1.ConditionTrue, LastTransitionTime: done},
			}
			return k8sClient.Status().Update(ctx, &j)
		}, 5*time.Second, 200*time.Millisecond).Should(Succeed())
	}

	cloneFailedCond := func(name string) *metav1.Condition {
		var p tideprojectv1alpha3.Project
		Expect(k8sClient.Get(ctx, nn(name), &p)).To(Succeed())
		for i := range p.Status.Conditions {
			if p.Status.Conditions[i].Type == tideprojectv1alpha3.ConditionCloneFailed {
				return &p.Status.Conditions[i]
			}
		}
		return nil
	}

	Describe("Spec A: classify baseref-unresolvable → condition set, Job NOT deleted", func() {
		const projectName = "baseref-halt-spec-a"

		It("stamps CloneFailed=True/BaseRefUnresolvable with the ref-naming message and leaves the Job", Label("heavy"), func() {
			makeProject(projectName, "no-such-ref")
			r := newReconciler()
			p, cloneJobName := advanceToCloneJob(r, projectName)

			markCloneFailed(cloneJobName)
			fakeCloneJobPod(cloneJobName, pushResultEnvelope{
				Kind: "CloneResult", ProjectUID: string(p.UID), ExitCode: 2,
				Reason: "baseref-unresolvable", BaseRef: "no-such-ref",
			}, corev1.PodFailed)

			reconcileN(r, projectName, 3)

			cond := cloneFailedCond(projectName)
			Expect(cond).NotTo(BeNil(), "CloneFailed condition must be set")
			Expect(cond.Status).To(Equal(metav1.ConditionTrue))
			Expect(cond.Reason).To(Equal(tideprojectv1alpha3.ReasonBaseRefUnresolvable))
			Expect(cond.Message).To(ContainSubstring("unable to resolve 'no-such-ref' to a commit SHA"))

			var got tideprojectv1alpha3.Project
			Expect(k8sClient.Get(ctx, nn(projectName), &got)).To(Succeed())
			Expect(cond.ObservedGeneration).To(Equal(got.Generation),
				"halt must be scoped to the current generation (D-07)")

			// The Job is NOT deleted — the halt is the condition, not Job absence.
			Consistently(func() error {
				return k8sClient.Get(ctx, nn(cloneJobName), &batchv1.Job{})
			}, 1*time.Second, 100*time.Millisecond).Should(Succeed(),
				"the failed clone Job must NOT be deleted for the baseref-unresolvable class")
		})
	})

	Describe("Spec B: halt survives TTL GC (Pitfall 2) — no re-dispatch after Job deleted", func() {
		const projectName = "baseref-halt-spec-b"

		It("does NOT create a new clone Job after the failed one is GC'd, same generation", func() {
			makeProject(projectName, "no-such-ref")
			r := newReconciler()
			p, cloneJobName := advanceToCloneJob(r, projectName)

			markCloneFailed(cloneJobName)
			fakeCloneJobPod(cloneJobName, pushResultEnvelope{
				Kind: "CloneResult", ProjectUID: string(p.UID), ExitCode: 2,
				Reason: "baseref-unresolvable", BaseRef: "no-such-ref",
			}, corev1.PodFailed)
			reconcileN(r, projectName, 3)
			Expect(cloneFailedCond(projectName).Reason).To(Equal(tideprojectv1alpha3.ReasonBaseRefUnresolvable))

			// Simulate TTL GC: delete the failed Job (and its envelope pod).
			var j batchv1.Job
			Expect(k8sClient.Get(ctx, nn(cloneJobName), &j)).To(Succeed())
			Expect(k8sClient.Delete(ctx, &j, client.PropagationPolicy(metav1.DeletePropagationBackground))).To(Succeed())
			var pod corev1.Pod
			if err := k8sClient.Get(ctx, nn(cloneJobName+"-pod"), &pod); err == nil {
				_ = k8sClient.Delete(ctx, &pod)
			}
			Eventually(func() bool {
				return apierrors.IsNotFound(k8sClient.Get(ctx, nn(cloneJobName), &batchv1.Job{}))
			}, 5*time.Second, 200*time.Millisecond).Should(BeTrue(), "clone Job should be gone")

			reconcileN(r, projectName, 5)

			// The generation-scoped halt must prevent re-dispatch.
			Consistently(func() error {
				return k8sClient.Get(ctx, nn(cloneJobName), &batchv1.Job{})
			}, 1*time.Second, 100*time.Millisecond).Should(MatchError(ContainSubstring("not found")),
				"halt is the condition, not Job existence: no re-dispatch after TTL GC")
		})
	})

	Describe("Spec C: release on spec edit (D-07) — bumped generation re-dispatches", func() {
		const projectName = "baseref-halt-spec-c"

		It("dispatches a fresh clone Job once spec.git.baseRef changes (generation increments)", func() {
			makeProject(projectName, "no-such-ref")
			r := newReconciler()
			p, cloneJobName := advanceToCloneJob(r, projectName)

			markCloneFailed(cloneJobName)
			fakeCloneJobPod(cloneJobName, pushResultEnvelope{
				Kind: "CloneResult", ProjectUID: string(p.UID), ExitCode: 2,
				Reason: "baseref-unresolvable", BaseRef: "no-such-ref",
			}, corev1.PodFailed)
			reconcileN(r, projectName, 3)
			Expect(cloneFailedCond(projectName).Reason).To(Equal(tideprojectv1alpha3.ReasonBaseRefUnresolvable))

			// Simulate TTL GC of the halted Job, then fix the ref.
			var j batchv1.Job
			Expect(k8sClient.Get(ctx, nn(cloneJobName), &j)).To(Succeed())
			Expect(k8sClient.Delete(ctx, &j, client.PropagationPolicy(metav1.DeletePropagationBackground))).To(Succeed())
			var pod corev1.Pod
			if err := k8sClient.Get(ctx, nn(cloneJobName+"-pod"), &pod); err == nil {
				_ = k8sClient.Delete(ctx, &pod)
			}
			Eventually(func() bool {
				return apierrors.IsNotFound(k8sClient.Get(ctx, nn(cloneJobName), &batchv1.Job{}))
			}, 5*time.Second, 200*time.Millisecond).Should(BeTrue())

			var before tideprojectv1alpha3.Project
			Expect(k8sClient.Get(ctx, nn(projectName), &before)).To(Succeed())
			genBefore := before.Generation
			before.Spec.Git.BaseRef = "also-bad-but-different"
			Expect(k8sClient.Update(ctx, &before)).To(Succeed())

			Eventually(func(g Gomega) {
				var after tideprojectv1alpha3.Project
				g.Expect(k8sClient.Get(ctx, nn(projectName), &after)).To(Succeed())
				g.Expect(after.Generation).To(BeNumerically(">", genBefore),
					"spec edit must increment metadata.generation")
			}, 5*time.Second, 100*time.Millisecond).Should(Succeed())
			waitForCacheSync(projectName, "default", &tideprojectv1alpha3.Project{})

			reconcileN(r, projectName, 5)

			Eventually(func() error {
				return k8sClient.Get(ctx, nn(cloneJobName), &batchv1.Job{})
			}, 5*time.Second, 200*time.Millisecond).Should(Succeed(),
				"a new generation releases the halt and re-dispatches a fresh clone Job")
		})
	})

	Describe("Spec D: baseSHA stamp on success (D-11/BASE-03) + unpruned spec round-trip", func() {
		const projectName = "baseref-halt-spec-d"
		const resolvedSHA = "0123456789abcdef0123456789abcdef01234567"

		It("stamps status.git.baseSHA and CloneComplete in one patch, CloneFailed=False", func() {
			makeProject(projectName, "release-1.4")
			r := newReconciler()
			p, cloneJobName := advanceToCloneJob(r, projectName)

			// Apply-half of BASE-03's upgrade-path test: the field survives a real
			// API-server round-trip under the regenerated CRD (unpruned).
			var applied tideprojectv1alpha3.Project
			Expect(k8sClient.Get(ctx, nn(projectName), &applied)).To(Succeed())
			Expect(applied.Spec.Git.BaseRef).To(Equal("release-1.4"),
				"spec.git.baseRef must round-trip through the API server unpruned")

			markCloneSucceeded(cloneJobName, 0)
			fakeCloneJobPod(cloneJobName, pushResultEnvelope{
				Kind: "CloneResult", ProjectUID: string(p.UID), ExitCode: 0,
				Reason: "", BaseSHA: resolvedSHA, BaseRef: "release-1.4",
			}, corev1.PodSucceeded)

			reconcileN(r, projectName, 3)

			Eventually(func(g Gomega) {
				var got tideprojectv1alpha3.Project
				g.Expect(k8sClient.Get(ctx, nn(projectName), &got)).To(Succeed())
				g.Expect(got.Status.Git.CloneComplete).To(BeTrue())
				g.Expect(got.Status.Git.BaseSHA).To(Equal(resolvedSHA),
					"baseSHA must be stamped from the success envelope in the same patch")
			}, 5*time.Second, 100*time.Millisecond).Should(Succeed())

			cond := cloneFailedCond(projectName)
			if cond != nil {
				Expect(cond.Status).To(Equal(metav1.ConditionFalse),
					"a successful clone clears any prior CloneFailed halt")
			}
		})
	})

	// Spec E splits into two specs because a Job's startTime/completionTime are
	// immutable once set on an unsuspended Job — so a single Job cannot be
	// observed both fresh (requeue branch) and past-cutoff (flip branch). Each
	// spec fabricates its own Job and sets completionTime exactly once.
	Describe("Spec E: unreadable success envelope handling (read-before-flip, Pattern 2)", func() {
		It("requeues without flipping CloneComplete when the envelope is unreadable within the cutoff", func() {
			const projectName = "baseref-halt-spec-e1"
			makeProject(projectName, "main")
			r := newReconciler()
			_, cloneJobName := advanceToCloneJob(r, projectName)

			// Succeeded, FRESH completionTime, NO envelope pod.
			markCloneSucceeded(cloneJobName, 0)

			res, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: nn(projectName)})
			if err != nil && !apierrors.IsConflict(err) {
				Expect(err).NotTo(HaveOccurred())
			}
			Expect(res.RequeueAfter).To(BeNumerically(">", 0),
				"an unreadable success envelope within the cutoff must requeue, not flip")
			var mid tideprojectv1alpha3.Project
			Expect(k8sClient.Get(ctx, nn(projectName), &mid)).To(Succeed())
			Expect(mid.Status.Git.CloneComplete).To(BeFalse(),
				"CloneComplete must NOT flip while the envelope is still readable-pending")
		})

		It("flips CloneComplete with empty baseSHA once completion is older than the cutoff", func() {
			const projectName = "baseref-halt-spec-e2"
			makeProject(projectName, "main")
			r := newReconciler()
			_, cloneJobName := advanceToCloneJob(r, projectName)

			// Succeeded, completionTime backdated beyond the 60s cutoff, NO envelope.
			markCloneSucceeded(cloneJobName, 2*time.Minute)
			reconcileN(r, projectName, 3)

			Eventually(func(g Gomega) {
				var got tideprojectv1alpha3.Project
				g.Expect(k8sClient.Get(ctx, nn(projectName), &got)).To(Succeed())
				g.Expect(got.Status.Git.CloneComplete).To(BeTrue(),
					"past the cutoff, CloneComplete flips even without an envelope")
				g.Expect(got.Status.Git.BaseSHA).To(BeEmpty(),
					"degraded provenance: baseSHA stays empty when the envelope was never readable")
			}, 5*time.Second, 100*time.Millisecond).Should(Succeed())
		})
	})

	Describe("Spec F: other reasons keep delete-and-re-dispatch (non-goal preserved)", func() {
		const projectName = "baseref-halt-spec-f"

		It("deletes the Job and stamps CloneJobFailed (not BaseRefUnresolvable) for auth-failed", func() {
			makeProject(projectName, "main")
			r := newReconciler()
			p, cloneJobName := advanceToCloneJob(r, projectName)

			markCloneFailed(cloneJobName)
			fakeCloneJobPod(cloneJobName, pushResultEnvelope{
				Kind: "CloneResult", ProjectUID: string(p.UID), ExitCode: 12,
				Reason: "auth-failed", BaseRef: "main",
			}, corev1.PodFailed)

			var failed batchv1.Job
			Expect(k8sClient.Get(ctx, nn(cloneJobName), &failed)).To(Succeed())
			failedUID := failed.UID

			reconcileN(r, projectName, 5)

			Eventually(func(g Gomega) {
				cond := cloneFailedCond(projectName)
				g.Expect(cond).NotTo(BeNil())
				g.Expect(cond.Status).To(Equal(metav1.ConditionTrue))
				g.Expect(cond.Reason).To(Equal("CloneJobFailed"),
					"non-baseref clone failures keep the existing WR-03 reason, not the halt reason")
				g.Expect(strings.Contains(cond.Message, "unable to resolve")).To(BeFalse())

				// Delete-and-re-dispatch preserved: the failed Job is gone, or a
				// fresh one with a new UID replaced it.
				var cur batchv1.Job
				if err := k8sClient.Get(ctx, nn(cloneJobName), &cur); err == nil {
					g.Expect(cur.UID).NotTo(Equal(failedUID),
						"a re-dispatched clone Job must have a new UID (the failed one was deleted)")
				}
			}, 5*time.Second, 100*time.Millisecond).Should(Succeed())
		})
	})
})
