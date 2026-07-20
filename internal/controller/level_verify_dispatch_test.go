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

// level_verify_dispatch_test.go — Phase 52 Plan 10 Task 2: envtest Ginkgo
// specs proving the three pre-Succeeded seams wired in phase_controller.go/
// milestone_controller.go/project_controller.go actually dispatch, consume,
// and escalate through the SHARED level_verify.go machinery (52-08) — the
// end-to-end proof that Plan 08's unit tests could only pin at the
// maybeRunLevelVerify function-call level. Mirrors task_verify_dispatch_test.go/
// plan_verify_dispatch_test.go's own envtest shape one/two levels up.
//
// internal/controller's sole Ginkgo entry point is TestControllers — run via
// `go test ./internal/controller/... -run TestControllers
// --ginkgo.focus='LevelVerify'`, never `go test -run LevelVerify` (which
// vacuously passes zero specs, 51-01-SUMMARY.md's documented finding).
package controller

import (
	"context"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	batchv1 "k8s.io/api/batch/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	tideprojectv1alpha3 "github.com/jsquirrelz/tide/api/v1alpha3"
	"github.com/jsquirrelz/tide/internal/budget"
	"github.com/jsquirrelz/tide/internal/dispatch/podjob"
	"github.com/jsquirrelz/tide/internal/gates"
	"github.com/jsquirrelz/tide/internal/owner"
	pkgdispatch "github.com/jsquirrelz/tide/pkg/dispatch"
)

// ---------- shared fixtures ----------

// makeVerifyLevelProject creates a Project carrying the given per-level
// VerificationDefaults (Phase 52 D-01 Project-scoped default map) plus an
// explicit Gates policy — the baseline fields mirror makeProjectForTask's
// shape, extended with Spec.Verification since Phase/MilestoneSpec
// themselves carry no authored-contract field (only Task/Plan.Spec.Verification
// do; Phase 52 D-01 resolves phase/milestone/project scope entirely from
// this map). Gates must be passed explicitly because EvaluatePolicy's
// zero-value default is "approve" for Milestone (unlike Phase's "auto") —
// scenario (f1) needs Milestone:"auto" so the D-07 boundary reaches the
// level-verify seam instead of parking at the pre-existing human gate.
func makeVerifyLevelProject(name string, v tideprojectv1alpha3.VerificationDefaults, g tideprojectv1alpha3.Gates) *tideprojectv1alpha3.Project {
	p := &tideprojectv1alpha3.Project{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
		Spec: tideprojectv1alpha3.ProjectSpec{
			SchemaRevision: "v1alpha3",
			TargetRepo:     "https://github.com/example/tide.git",
			Verification:   v,
			Gates:          g,
		},
	}
	Expect(k8sClient.Create(context.Background(), p)).To(Succeed())
	waitForCacheSync(name, "default", &tideprojectv1alpha3.Project{})
	return p
}

// cleanupLevelVerifyObjects deletes every Job in "default" plus each named
// Phase/Milestone/Plan/Project (clearing finalizers first) — mirrors
// boundary_push_test.go's cleanupBP shape, extended with Project cleanup
// since scenario (f)'s project-level spec reconciles a real Project.
func cleanupLevelVerifyObjects(names ...string) {
	ctx := context.Background()
	var jobs batchv1.JobList
	_ = k8sClient.List(ctx, &jobs, client.InNamespace("default"))
	for i := range jobs.Items {
		_ = k8sClient.Delete(ctx, &jobs.Items[i])
	}
	for _, n := range names {
		ph := &tideprojectv1alpha3.Phase{}
		if err := k8sClient.Get(ctx, types.NamespacedName{Name: n, Namespace: "default"}, ph); err == nil {
			ph.Finalizers = nil
			_ = k8sClient.Update(ctx, ph)
			_ = k8sClient.Delete(ctx, ph)
		}
		ms := &tideprojectv1alpha3.Milestone{}
		if err := k8sClient.Get(ctx, types.NamespacedName{Name: n, Namespace: "default"}, ms); err == nil {
			ms.Finalizers = nil
			_ = k8sClient.Update(ctx, ms)
			_ = k8sClient.Delete(ctx, ms)
		}
		pl := &tideprojectv1alpha3.Plan{}
		if err := k8sClient.Get(ctx, types.NamespacedName{Name: n, Namespace: "default"}, pl); err == nil {
			pl.Finalizers = nil
			_ = k8sClient.Update(ctx, pl)
			_ = k8sClient.Delete(ctx, pl)
		}
		proj := &tideprojectv1alpha3.Project{}
		if err := k8sClient.Get(ctx, types.NamespacedName{Name: n, Namespace: "default"}, proj); err == nil {
			proj.Finalizers = nil
			_ = k8sClient.Update(ctx, proj)
			_ = k8sClient.Delete(ctx, proj)
		}
	}
}

// newVerifyDispatchPhaseReconciler mirrors newVerifyDispatchPlanReconciler
// (plan_verify_dispatch_test.go) one level up.
func newVerifyDispatchPhaseReconciler(envReader podjob.EnvelopeReader) *PhaseReconciler {
	return &PhaseReconciler{
		Client: mgrClient,
		Scheme: k8sClient.Scheme(),
		Deps: PlannerReconcilerDeps{
			Dispatcher:     &stubDispatcher{},
			SigningKey:     testSigningKey,
			CredproxyImage: testCredproxyImage,
			EnvReader:      envReader,
			HelmProviderDefaults: ProviderDefaults{
				Image: testSubagentImage,
			},
			VerifierImage:        "tide-langgraph-verifier:test",
			Reservations:         budget.NewReservationStore(),
			ReserveEstimateCents: 500,
		},
		PlannerPool: newPlannerPoolForTest(),
	}
}

// newVerifyDispatchMilestoneReconciler mirrors the Phase constructor above,
// Milestone-scoped.
func newVerifyDispatchMilestoneReconciler(envReader podjob.EnvelopeReader) *MilestoneReconciler {
	return &MilestoneReconciler{
		Client: mgrClient,
		Scheme: k8sClient.Scheme(),
		Deps: PlannerReconcilerDeps{
			Dispatcher:     &stubDispatcher{},
			SigningKey:     testSigningKey,
			CredproxyImage: testCredproxyImage,
			EnvReader:      envReader,
			HelmProviderDefaults: ProviderDefaults{
				Image: testSubagentImage,
			},
			VerifierImage:        "tide-langgraph-verifier:test",
			Reservations:         budget.NewReservationStore(),
			ReserveEstimateCents: 500,
		},
		PlannerPool: newPlannerPoolForTest(),
	}
}

// newVerifyDispatchProjectReconciler builds a ProjectReconciler wired for
// checkProjectComplete's own dispatch fields — scenario (f)'s project spec
// calls checkProjectComplete DIRECTLY (not via the full r.Reconcile() init/
// clone/branch-name lifecycle, which is unrelated to what this spec proves)
// per the same unexported-method-call precedent level_verify_unit_test.go
// already established for maybeRunLevelVerify.
func newVerifyDispatchProjectReconciler(envReader podjob.EnvelopeReader) *ProjectReconciler {
	return &ProjectReconciler{
		Client: mgrClient,
		Scheme: k8sClient.Scheme(),
		Deps: PlannerReconcilerDeps{
			Dispatcher:     &stubDispatcher{},
			SigningKey:     testSigningKey,
			CredproxyImage: testCredproxyImage,
			EnvReader:      envReader,
			HelmProviderDefaults: ProviderDefaults{
				Image: testSubagentImage,
			},
			VerifierImage:        "tide-langgraph-verifier:test",
			Reservations:         budget.NewReservationStore(),
			ReserveEstimateCents: 500,
		},
	}
}

// makeVerifyMilestone creates a Milestone under projectName. Waits for cache sync.
func makeVerifyMilestone(name, projectName string) *tideprojectv1alpha3.Milestone {
	ms := &tideprojectv1alpha3.Milestone{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
		Spec:       tideprojectv1alpha3.MilestoneSpec{ProjectRef: projectName},
	}
	Expect(k8sClient.Create(context.Background(), ms)).To(Succeed())
	waitForCacheSync(name, "default", &tideprojectv1alpha3.Milestone{})
	return ms
}

// makeVerifyPhase creates a Phase under msName. Waits for cache sync.
func makeVerifyPhase(name, msName string) *tideprojectv1alpha3.Phase {
	ph := &tideprojectv1alpha3.Phase{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
		Spec:       tideprojectv1alpha3.PhaseSpec{MilestoneRef: msName},
	}
	Expect(k8sClient.Create(context.Background(), ph)).To(Succeed())
	waitForCacheSync(name, "default", &tideprojectv1alpha3.Phase{})
	return ph
}

// makeSucceededChildPlan creates a controller-owned child Plan under phParent
// with Status.Phase=Succeeded, and waits for the cache to observe both the
// create and the status patch — mirrors boundary_push_test.go's own
// makeSucceededChildPlan closure (unexported to that file's Describe block,
// so re-implemented here at package scope for reuse).
func makeSucceededChildPlan(name, phName string, phParent client.Object) {
	ctx := context.Background()
	pl := &tideprojectv1alpha3.Plan{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
		Spec:       tideprojectv1alpha3.PlanSpec{PhaseRef: phName},
	}
	t := true
	pl.OwnerReferences = []metav1.OwnerReference{{
		APIVersion:         tideprojectv1alpha3.GroupVersion.String(),
		Kind:               "Phase",
		Name:               phParent.GetName(),
		UID:                phParent.GetUID(),
		Controller:         &t,
		BlockOwnerDeletion: &t,
	}}
	Expect(k8sClient.Create(ctx, pl)).To(Succeed())
	waitForCacheSync(name, "default", &tideprojectv1alpha3.Plan{})
	var got tideprojectv1alpha3.Plan
	Expect(k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: "default"}, &got)).To(Succeed())
	patch := client.MergeFrom(got.DeepCopy())
	got.Status.Phase = tideprojectv1alpha3.LevelPhaseSucceeded
	Expect(k8sClient.Status().Patch(ctx, &got, patch)).To(Succeed())
	Eventually(func() string {
		var g tideprojectv1alpha3.Plan
		if err := k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: "default"}, &g); err != nil {
			return ""
		}
		return g.Status.Phase
	}, 5*time.Second, 50*time.Millisecond).Should(Equal(tideprojectv1alpha3.LevelPhaseSucceeded))
}

// makeSucceededChildPhaseUnder creates a controller-owned child Phase under
// msParent with Status.Phase=Succeeded — the Milestone-level analog of
// makeSucceededChildPlan above.
func makeSucceededChildPhaseUnder(name, msName string, msParent client.Object) {
	ctx := context.Background()
	ph := &tideprojectv1alpha3.Phase{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
		Spec:       tideprojectv1alpha3.PhaseSpec{MilestoneRef: msName},
	}
	t := true
	ph.OwnerReferences = []metav1.OwnerReference{{
		APIVersion:         tideprojectv1alpha3.GroupVersion.String(),
		Kind:               "Milestone",
		Name:               msParent.GetName(),
		UID:                msParent.GetUID(),
		Controller:         &t,
		BlockOwnerDeletion: &t,
	}}
	Expect(k8sClient.Create(ctx, ph)).To(Succeed())
	waitForCacheSync(name, "default", &tideprojectv1alpha3.Phase{})
	var got tideprojectv1alpha3.Phase
	Expect(k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: "default"}, &got)).To(Succeed())
	patch := client.MergeFrom(got.DeepCopy())
	got.Status.Phase = tideprojectv1alpha3.LevelPhaseSucceeded
	Expect(k8sClient.Status().Patch(ctx, &got, patch)).To(Succeed())
	Eventually(func() string {
		var g tideprojectv1alpha3.Phase
		if err := k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: "default"}, &g); err != nil {
			return ""
		}
		return g.Status.Phase
	}, 5*time.Second, 50*time.Millisecond).Should(Equal(tideprojectv1alpha3.LevelPhaseSucceeded))
}

// makeSucceededChildMilestoneUnder is the Project-level analog: a
// controller-owned child Milestone under projParent with Status.Phase=Succeeded.
func makeSucceededChildMilestoneUnder(name, projectName string, projParent client.Object) {
	ctx := context.Background()
	ms := &tideprojectv1alpha3.Milestone{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
		Spec:       tideprojectv1alpha3.MilestoneSpec{ProjectRef: projectName},
	}
	t := true
	ms.OwnerReferences = []metav1.OwnerReference{{
		APIVersion:         tideprojectv1alpha3.GroupVersion.String(),
		Kind:               "Project",
		Name:               projParent.GetName(),
		UID:                projParent.GetUID(),
		Controller:         &t,
		BlockOwnerDeletion: &t,
	}}
	Expect(k8sClient.Create(ctx, ms)).To(Succeed())
	waitForCacheSync(name, "default", &tideprojectv1alpha3.Milestone{})
	var got tideprojectv1alpha3.Milestone
	Expect(k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: "default"}, &got)).To(Succeed())
	patch := client.MergeFrom(got.DeepCopy())
	got.Status.Phase = tideprojectv1alpha3.LevelPhaseSucceeded
	Expect(k8sClient.Status().Patch(ctx, &got, patch)).To(Succeed())
	Eventually(func() string {
		var g tideprojectv1alpha3.Milestone
		if err := k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: "default"}, &g); err != nil {
			return ""
		}
		return g.Status.Phase
	}, 5*time.Second, 50*time.Millisecond).Should(Equal(tideprojectv1alpha3.LevelPhaseSucceeded))
}

// driveToPhaseVerifying dispatches ph's planner Job, completes it with
// ChildCount:1, creates a single Succeeded child Plan, then reconciles until
// the boundary is detected. With a resolved Project-level Phase verification
// contract this lands the Phase in Verifying with a dispatched verifier Job
// (mirrors boundary_push_test.go's Test 2 shape, extended for the level-verify
// seam). Returns the up-to-date Phase.
func driveToPhaseVerifying(ctx context.Context, r *PhaseReconciler, envReader *mapEnvReader, phaseName, msName, projName string) *tideprojectv1alpha3.Phase {
	name := types.NamespacedName{Name: phaseName, Namespace: "default"}
	ExpectWithOffset(1, reconcileWithRetry(r.Reconcile, name, 5)).To(Succeed())

	var got tideprojectv1alpha3.Phase
	ExpectWithOffset(1, mgrClient.Get(ctx, name, &got)).To(Succeed())
	envReader.SetOut(string(got.UID), pkgdispatch.EnvelopeOut{
		TaskUID:    string(got.UID),
		ExitCode:   0,
		ChildCount: 1,
	})
	plannerJobName := fmt.Sprintf("tide-phase-%s-1", got.UID)
	ExpectWithOffset(1, makeFakeJobTerminal(ctx, mgrClient, plannerJobName, "default", true)).To(Succeed())

	makeSucceededChildPlan(phaseName+"-child", phaseName, &got)

	ExpectWithOffset(1, reconcileWithRetry(r.Reconcile, name, 3)).To(Succeed())

	var ph tideprojectv1alpha3.Phase
	ExpectWithOffset(1, k8sClient.Get(ctx, name, &ph)).To(Succeed())
	return &ph
}

// driveToMilestoneVerifying is driveToPhaseVerifying's Milestone-level
// analog — dispatches ms's planner Job, completes it with ChildCount:1,
// creates a single Succeeded child Phase, then reconciles until the boundary
// is detected.
func driveToMilestoneVerifying(ctx context.Context, r *MilestoneReconciler, envReader *mapEnvReader, msName, projName string) *tideprojectv1alpha3.Milestone {
	name := types.NamespacedName{Name: msName, Namespace: "default"}
	ExpectWithOffset(1, reconcileWithRetry(r.Reconcile, name, 5)).To(Succeed())

	var got tideprojectv1alpha3.Milestone
	ExpectWithOffset(1, mgrClient.Get(ctx, name, &got)).To(Succeed())
	envReader.SetOut(string(got.UID), pkgdispatch.EnvelopeOut{
		TaskUID:    string(got.UID),
		ExitCode:   0,
		ChildCount: 1,
	})
	plannerJobName := fmt.Sprintf("tide-milestone-%s-1", got.UID)
	ExpectWithOffset(1, makeFakeJobTerminal(ctx, mgrClient, plannerJobName, "default", true)).To(Succeed())

	makeSucceededChildPhaseUnder(msName+"-child", msName, &got)

	ExpectWithOffset(1, reconcileWithRetry(r.Reconcile, name, 3)).To(Succeed())

	var ms tideprojectv1alpha3.Milestone
	ExpectWithOffset(1, k8sClient.Get(ctx, name, &ms)).To(Succeed())
	return &ms
}

// completeLevelVerifierJob marks the deterministic level-verify Job
// tide-verifier-<level>-<uid>-<attempt> JobComplete=True.
func completeLevelVerifierJob(ctx context.Context, level, uid string, attempt int) {
	jobName := podjob.VerifierJobName(level, uid, attempt)
	var job batchv1.Job
	ExpectWithOffset(1, k8sClient.Get(ctx, types.NamespacedName{Name: jobName, Namespace: "default"}, &job)).To(Succeed())
	jobPatch := client.MergeFrom(job.DeepCopy())
	completeJobStatus(&job)
	ExpectWithOffset(1, k8sClient.Status().Patch(ctx, &job, jobPatch)).To(Succeed())
}

// countVerifierJobsFor counts Jobs carrying the deterministic
// tide-verifier-<level>-<uid>- name prefix — used by scenario (c)'s
// convergence-guard assertion (no second verifier Job after approve).
func countVerifierJobsFor(ctx context.Context, level, uid string) int {
	var jobs batchv1.JobList
	ExpectWithOffset(1, k8sClient.List(ctx, &jobs, client.InNamespace("default"),
		client.MatchingLabels{"tideproject.k8s/role": "verifier", fmt.Sprintf("tideproject.k8s/%s-uid", level): uid})).To(Succeed())
	return len(jobs.Items)
}

var _ = Describe("LevelVerify: Phase/Milestone/Project pre-Succeeded verify dispatch (Phase 52 Plan 10)", Label("envtest", "phase52", "level-verify"), func() {
	ctx := context.Background()

	It("(a) a Phase whose children all Succeeded, with a Project-level phase contract, enters Verifying and dispatches a verifier Job without stamping Succeeded", func() {
		const projName, msName, phaseName = "lv-proj-a", "lv-ms-a", "lv-ph-a"
		makeVerifyLevelProject(projName, tideprojectv1alpha3.VerificationDefaults{
			Phase: &tideprojectv1alpha3.VerificationSpec{
				Phase:       "Locked",
				Version:     1,
				GateCommand: "make test-phase-gate",
			},
		}, tideprojectv1alpha3.Gates{})
		defer cleanupLevelVerifyObjects(projName, msName, phaseName, phaseName+"-child")

		makeVerifyMilestone(msName, projName)
		makeVerifyPhase(phaseName, msName)

		envReader := newMapEnvReader()
		r := newVerifyDispatchPhaseReconciler(envReader)
		ph := driveToPhaseVerifying(ctx, r, envReader, phaseName, msName, projName)

		Expect(ph.Status.Phase).To(Equal(tideprojectv1alpha3.LevelPhaseVerifying),
			"a contract-bearing Phase must enter Verifying, never Succeeded, once its children all Succeeded (D-07)")

		verifierJobName := podjob.VerifierJobName("phase", string(ph.UID), 1)
		var verifierJob batchv1.Job
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: verifierJobName, Namespace: "default"}, &verifierJob)).To(Succeed())
		Expect(verifierJob.Labels["tideproject.k8s/role"]).To(Equal("verifier"))
		Expect(verifierJob.Labels["tideproject.k8s/level"]).To(Equal("phase"))
		Expect(verifierJob.Labels[owner.LabelProject]).To(Equal(projName))

		Expect(ph.Status.Phase).NotTo(Equal(tideprojectv1alpha3.LevelPhaseSucceeded),
			"Succeeded must not be stamped while the level verifier Job is still running")
	})

	It("(b) an APPROVED verdict clears Verifying to Succeeded with LoopStatus.ExitReason=approved", func() {
		const projName, msName, phaseName = "lv-proj-b", "lv-ms-b", "lv-ph-b"
		makeVerifyLevelProject(projName, tideprojectv1alpha3.VerificationDefaults{
			Phase: &tideprojectv1alpha3.VerificationSpec{
				Phase:       "Locked",
				Version:     1,
				GateCommand: "make test-phase-gate",
			},
		}, tideprojectv1alpha3.Gates{})
		defer cleanupLevelVerifyObjects(projName, msName, phaseName, phaseName+"-child")

		makeVerifyMilestone(msName, projName)
		makeVerifyPhase(phaseName, msName)

		envReader := newMapEnvReader()
		r := newVerifyDispatchPhaseReconciler(envReader)
		ph := driveToPhaseVerifying(ctx, r, envReader, phaseName, msName, projName)
		Expect(ph.Status.Phase).To(Equal(tideprojectv1alpha3.LevelPhaseVerifying))

		envReader.SetOut(string(ph.UID), pkgdispatch.EnvelopeOut{
			CompletedAt: time.Now(),
			Verdict:     &pkgdispatch.GateDecision{Verdict: pkgdispatch.VerdictApproved, Summary: "phase outcome verified"},
		})
		completeLevelVerifierJob(ctx, "phase", string(ph.UID), 1)

		name := types.NamespacedName{Name: phaseName, Namespace: "default"}
		_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: name})
		Expect(err).NotTo(HaveOccurred())

		Expect(k8sClient.Get(ctx, name, ph)).To(Succeed())
		Expect(ph.Status.Phase).To(Equal(tideprojectv1alpha3.LevelPhaseSucceeded),
			"an APPROVED level-verify verdict must clear Verifying to Succeeded")
		Expect(ph.Status.LoopStatus.ExitReason).To(Equal(tideprojectv1alpha3.ExitApproved))
	})

	It("(c) a REPAIRABLE verdict parks AwaitingApproval (project ConditionVerifyHalt NOT set); approve proceeds to Succeeded with NO second verifier Job", func() {
		const projName, msName, phaseName = "lv-proj-c", "lv-ms-c", "lv-ph-c"
		makeVerifyLevelProject(projName, tideprojectv1alpha3.VerificationDefaults{
			Phase: &tideprojectv1alpha3.VerificationSpec{
				Phase:       "Locked",
				Version:     1,
				GateCommand: "make test-phase-gate",
			},
		}, tideprojectv1alpha3.Gates{})
		defer cleanupLevelVerifyObjects(projName, msName, phaseName, phaseName+"-child")

		makeVerifyMilestone(msName, projName)
		makeVerifyPhase(phaseName, msName)

		envReader := newMapEnvReader()
		r := newVerifyDispatchPhaseReconciler(envReader)
		ph := driveToPhaseVerifying(ctx, r, envReader, phaseName, msName, projName)
		Expect(ph.Status.Phase).To(Equal(tideprojectv1alpha3.LevelPhaseVerifying))

		envReader.SetOut(string(ph.UID), pkgdispatch.EnvelopeOut{
			CompletedAt: time.Now(),
			Verdict: &pkgdispatch.GateDecision{
				Verdict:  pkgdispatch.VerdictRepairable,
				Summary:  "phase outcome falls short but looks fixable",
				Findings: []pkgdispatch.Finding{{Dimension: "correctness", Severity: "advisory"}},
			},
		})
		completeLevelVerifierJob(ctx, "phase", string(ph.UID), 1)

		name := types.NamespacedName{Name: phaseName, Namespace: "default"}
		_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: name})
		Expect(err).NotTo(HaveOccurred())

		Expect(k8sClient.Get(ctx, name, ph)).To(Succeed())
		Expect(ph.Status.Phase).To(Equal(tideprojectv1alpha3.LevelPhaseAwaitingApproval),
			"SC2's default onExhaustion=requireApproval must park a REPAIRABLE phase-level verdict, not repair it (no repair branch at this level)")

		var proj tideprojectv1alpha3.Project
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: projName, Namespace: "default"}, &proj)).To(Succeed())
		Expect(meta.IsStatusConditionTrue(proj.Status.Conditions, tideprojectv1alpha3.ConditionVerifyHalt)).To(BeFalse(),
			"a requireApproval park must NOT freeze the project via ConditionVerifyHalt — that is the escalate leg's job")

		jobCountBeforeApprove := countVerifierJobsFor(ctx, "phase", string(ph.UID))

		var current tideprojectv1alpha3.Phase
		Expect(k8sClient.Get(ctx, name, &current)).To(Succeed())
		patch := client.MergeFrom(current.DeepCopy())
		if current.Annotations == nil {
			current.Annotations = map[string]string{}
		}
		current.Annotations[gates.AnnotationApprovePrefix+"phase"] = "true"
		Expect(k8sClient.Patch(ctx, &current, patch)).To(Succeed())

		Eventually(func() bool {
			var p tideprojectv1alpha3.Phase
			if err := mgrClient.Get(ctx, name, &p); err != nil {
				return false
			}
			return gates.CheckApprove(&p, "phase")
		}, 5*time.Second, 50*time.Millisecond).Should(BeTrue())

		Eventually(func(g Gomega) {
			_, rErr := r.Reconcile(ctx, reconcile.Request{NamespacedName: name})
			g.Expect(rErr).NotTo(HaveOccurred())
			g.Expect(k8sClient.Get(ctx, name, ph)).To(Succeed())
			g.Expect(ph.Status.Phase).To(Equal(tideprojectv1alpha3.LevelPhaseSucceeded))
		}, 5*time.Second, 50*time.Millisecond).Should(Succeed(),
			"an approved verify-exhausted Phase must proceed to Succeeded — the post-approval convergence guard")

		Expect(countVerifierJobsFor(ctx, "phase", string(ph.UID))).To(Equal(jobCountBeforeApprove),
			"the convergence guard (ExitReason already set) must prevent a second verifier Job on the approve-driven re-entry")
	})

	It("(d) authored onExhaustion:escalate + a BLOCKED verdict halts VerifyHalted and stamps project-wide ConditionVerifyHalt", func() {
		const projName, msName, phaseName = "lv-proj-d", "lv-ms-d", "lv-ph-d"
		makeVerifyLevelProject(projName, tideprojectv1alpha3.VerificationDefaults{
			Phase: &tideprojectv1alpha3.VerificationSpec{
				Phase:        "Locked",
				Version:      1,
				GateCommand:  "make test-phase-gate",
				OnExhaustion: "escalate",
			},
		}, tideprojectv1alpha3.Gates{})
		defer cleanupLevelVerifyObjects(projName, msName, phaseName, phaseName+"-child")

		makeVerifyMilestone(msName, projName)
		makeVerifyPhase(phaseName, msName)

		envReader := newMapEnvReader()
		r := newVerifyDispatchPhaseReconciler(envReader)
		ph := driveToPhaseVerifying(ctx, r, envReader, phaseName, msName, projName)
		Expect(ph.Status.Phase).To(Equal(tideprojectv1alpha3.LevelPhaseVerifying))

		envReader.SetOut(string(ph.UID), pkgdispatch.EnvelopeOut{
			CompletedAt: time.Now(),
			Verdict: &pkgdispatch.GateDecision{
				Verdict: pkgdispatch.VerdictBlocked,
				Summary: "phase outcome unverifiable",
			},
		})
		completeLevelVerifierJob(ctx, "phase", string(ph.UID), 1)

		name := types.NamespacedName{Name: phaseName, Namespace: "default"}
		_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: name})
		Expect(err).NotTo(HaveOccurred())

		Expect(k8sClient.Get(ctx, name, ph)).To(Succeed())
		Expect(ph.Status.Phase).To(Equal(tideprojectv1alpha3.LevelPhaseVerifyHalted))
		Expect(ph.Status.LoopStatus.ExitReason).To(Equal(tideprojectv1alpha3.ExitEscalated))

		var proj tideprojectv1alpha3.Project
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: projName, Namespace: "default"}, &proj)).To(Succeed())
		Expect(meta.IsStatusConditionTrue(proj.Status.Conditions, tideprojectv1alpha3.ConditionVerifyHalt)).To(BeTrue(),
			"onExhaustion:escalate must freeze the project-wide ConditionVerifyHalt (D-08)")
	})

	It("(e) a Phase with no resolved verification contract anywhere keeps today's exact behavior: Succeeded with zero verifier Jobs (off-switch pin)", func() {
		const projName, msName, phaseName = "lv-proj-e", "lv-ms-e", "lv-ph-e"
		makeVerifyLevelProject(projName, tideprojectv1alpha3.VerificationDefaults{}, tideprojectv1alpha3.Gates{})
		defer cleanupLevelVerifyObjects(projName, msName, phaseName, phaseName+"-child")

		makeVerifyMilestone(msName, projName)
		makeVerifyPhase(phaseName, msName)

		envReader := newMapEnvReader()
		r := newVerifyDispatchPhaseReconciler(envReader)
		ph := driveToPhaseVerifying(ctx, r, envReader, phaseName, msName, projName)

		Expect(ph.Status.Phase).To(Equal(tideprojectv1alpha3.LevelPhaseSucceeded),
			"a contract-less Phase must keep the pre-Phase-52 exact behavior: succeed directly, no Verifying transition observed")

		verifierJobName := podjob.VerifierJobName("phase", string(ph.UID), 1)
		var verifierJob batchv1.Job
		err := k8sClient.Get(ctx, types.NamespacedName{Name: verifierJobName, Namespace: "default"}, &verifierJob)
		Expect(apierrors.IsNotFound(err)).To(BeTrue(), "no level verifier Job may be created for a contract-less Phase")
	})

	It("(f1) the same machinery serves Milestone: contract -> Verifying -> APPROVED -> Succeeded, verifier Job carries role=verifier + project label", func() {
		const projName, msName = "lv-proj-f1", "lv-ms-f1"
		makeVerifyLevelProject(projName, tideprojectv1alpha3.VerificationDefaults{
			Milestone: &tideprojectv1alpha3.VerificationSpec{
				Phase:       "Locked",
				Version:     1,
				GateCommand: "make test-milestone-gate",
			},
		}, tideprojectv1alpha3.Gates{Milestone: "auto"})
		defer cleanupLevelVerifyObjects(projName, msName, msName+"-child")

		makeVerifyMilestone(msName, projName)

		envReader := newMapEnvReader()
		r := newVerifyDispatchMilestoneReconciler(envReader)
		ms := driveToMilestoneVerifying(ctx, r, envReader, msName, projName)

		Expect(ms.Status.Phase).To(Equal(tideprojectv1alpha3.LevelPhaseVerifying),
			"a contract-bearing Milestone must enter Verifying once its children all Succeeded (D-07, same machinery one level up)")

		verifierJobName := podjob.VerifierJobName("milestone", string(ms.UID), 1)
		var verifierJob batchv1.Job
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: verifierJobName, Namespace: "default"}, &verifierJob)).To(Succeed())
		Expect(verifierJob.Labels["tideproject.k8s/role"]).To(Equal("verifier"))
		Expect(verifierJob.Labels["tideproject.k8s/level"]).To(Equal("milestone"))
		Expect(verifierJob.Labels[owner.LabelProject]).To(Equal(projName))

		envReader.SetOut(string(ms.UID), pkgdispatch.EnvelopeOut{
			CompletedAt: time.Now(),
			Verdict:     &pkgdispatch.GateDecision{Verdict: pkgdispatch.VerdictApproved, Summary: "milestone outcome verified"},
		})
		completeLevelVerifierJob(ctx, "milestone", string(ms.UID), 1)

		name := types.NamespacedName{Name: msName, Namespace: "default"}
		_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: name})
		Expect(err).NotTo(HaveOccurred())

		Expect(k8sClient.Get(ctx, name, ms)).To(Succeed())
		Expect(ms.Status.Phase).To(Equal(tideprojectv1alpha3.LevelPhaseSucceeded))
		Expect(ms.Status.LoopStatus.ExitReason).To(Equal(tideprojectv1alpha3.ExitApproved))
	})

	It("(f2) the same machinery serves Project: contract -> Verifying -> APPROVED -> Complete, verifier Job carries role=verifier + project label", func() {
		const projName, childMsName = "lv-proj-f2", "lv-proj-f2-child-ms"
		project := makeVerifyLevelProject(projName, tideprojectv1alpha3.VerificationDefaults{
			Project: &tideprojectv1alpha3.VerificationSpec{
				Phase:       "Locked",
				Version:     1,
				GateCommand: "make test-project-gate",
			},
		}, tideprojectv1alpha3.Gates{})
		defer cleanupLevelVerifyObjects(projName, childMsName)

		makeSucceededChildMilestoneUnder(childMsName, projName, project)

		envReader := newMapEnvReader()
		r := newVerifyDispatchProjectReconciler(envReader)

		handled, res, complete, err := r.checkProjectComplete(ctx, project)
		Expect(err).NotTo(HaveOccurred())
		Expect(handled).To(BeTrue(), "an active, not-yet-concluded level-verify contract must claim the reconcile")
		Expect(complete).To(BeFalse())
		_ = res

		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: projName, Namespace: "default"}, project)).To(Succeed())
		Expect(project.Status.Phase).To(Equal(tideprojectv1alpha3.LevelPhaseVerifying),
			"a contract-bearing Project must enter Verifying once its children all Succeeded (D-07, same machinery two levels up)")

		verifierJobName := podjob.VerifierJobName("project", string(project.UID), 1)
		var verifierJob batchv1.Job
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: verifierJobName, Namespace: "default"}, &verifierJob)).To(Succeed())
		Expect(verifierJob.Labels["tideproject.k8s/role"]).To(Equal("verifier"))
		Expect(verifierJob.Labels["tideproject.k8s/level"]).To(Equal("project"))
		Expect(verifierJob.Labels[owner.LabelProject]).To(Equal(projName))

		envReader.SetOut(string(project.UID), pkgdispatch.EnvelopeOut{
			CompletedAt: time.Now(),
			Verdict:     &pkgdispatch.GateDecision{Verdict: pkgdispatch.VerdictApproved, Summary: "project outcome verified"},
		})
		completeLevelVerifierJob(ctx, "project", string(project.UID), 1)
		// The APPROVED fall-through reads the verifier Job's terminal status via
		// the cached client; wait for the completion patch to propagate to the
		// cache before asserting, or checkProjectComplete can race it and stay in
		// Verifying (the 52-09 waitForJobTerminalCacheSync idiom for this exact
		// status-only-mutation-on-an-already-cached-object race).
		waitForJobTerminalCacheSync(ctx, verifierJobName, "default")

		handled2, _, complete2, err2 := r.checkProjectComplete(ctx, project)
		Expect(err2).NotTo(HaveOccurred())
		Expect(handled2).To(BeFalse(), "an APPROVED verdict must fall through to the Complete patch in the SAME call")
		Expect(complete2).To(BeTrue())
		Expect(project.Status.Phase).To(Equal(tideprojectv1alpha3.PhaseComplete))
	})
})
