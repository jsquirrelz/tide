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

// plan_verify_dispatch_test.go — Phase 52 Plan 07 (PlanCheck): the plan-check
// loop's Verifying entry, the checkParentApproval hold, the
// dispatch/consume state machine, D-10's safety rails (concurrency cap,
// ReservationStore, fail-closed ClassifyVerdict), and the fail-closed
// exhaustion path — mirrors task_verify_dispatch_test.go's own shape one
// level up. internal/controller's sole Ginkgo entry point is
// TestControllers (Pitfall 5 from 52-RESEARCH.md/51-01-SUMMARY.md); run via
// `go test ./internal/controller/... -run TestControllers
// --ginkgo.focus='PlanCheck'`, never `go test -run PlanCheck` (which
// vacuously passes zero specs).
package controller

import (
	"context"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	batchv1 "k8s.io/api/batch/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	tideprojectv1alpha3 "github.com/jsquirrelz/tide/api/v1alpha3"
	"github.com/jsquirrelz/tide/internal/budget"
	"github.com/jsquirrelz/tide/internal/dispatch/podjob"
	"github.com/jsquirrelz/tide/internal/owner"
	pkgdispatch "github.com/jsquirrelz/tide/pkg/dispatch"
)

// newVerifyDispatchPlanReconciler mirrors task_verify_dispatch_test.go's
// newVerifyDispatchTaskReconciler for the Plan level — the fields
// dispatchPlanVerifier needs (VerifierImage, a FRESH ReservationStore + a
// positive ReserveEstimateCents so the D-10/Pitfall-6 no-leak assertions are
// meaningful and isolated per spec).
func newVerifyDispatchPlanReconciler(envReader podjob.EnvelopeReader) *PlanReconciler {
	return &PlanReconciler{
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

// makeVerifyPlan creates a Plan carrying the given VerificationSpec, stamped
// with the tideproject.k8s/project label (resolveProjectForPlan's label
// fast-path — mirrors makeVerifyTask's identical rationale one level up) and
// a dummy, deliberately-nonexistent PhaseRef (a NotFound Phase Get is
// tolerated throughout the Plan reconcile path). Waits for cache sync.
func makeVerifyPlan(name, projectName string, v tideprojectv1alpha3.VerificationSpec) *tideprojectv1alpha3.Plan {
	p := &tideprojectv1alpha3.Plan{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "default",
			Labels:    map[string]string{owner.LabelProject: projectName},
		},
		Spec: tideprojectv1alpha3.PlanSpec{
			PhaseRef:     name + "-no-such-phase",
			Verification: v,
		},
	}
	Expect(k8sClient.Create(context.Background(), p)).To(Succeed())
	waitForCacheSync(name, "default", &tideprojectv1alpha3.Plan{})
	return p
}

// makeVerifyChildTask creates a single child Task under planRef, stamped
// with the project label (mirrors makeVerifyTask one level down).
func makeVerifyChildTask(name, planRef, projectName string) *tideprojectv1alpha3.Task {
	t := &tideprojectv1alpha3.Task{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "default",
			Labels:    map[string]string{owner.LabelProject: projectName},
		},
		Spec: tideprojectv1alpha3.TaskSpec{
			PlanRef:             planRef,
			FilesTouched:        []string{"src/main.go"},
			DeclaredOutputPaths: []string{"artifacts/out.txt"},
			PromptPath:          "envelopes/test/children/" + name + ".json",
		},
	}
	Expect(k8sClient.Create(context.Background(), t)).To(Succeed())
	waitForCacheSync(name, "default", &tideprojectv1alpha3.Task{})
	return t
}

// dispatchPlanPlanner drives plan through its first two reconciles (finalizer
// add, then the real planner dispatch via BuildJobSpec — the SAME path
// production dispatch uses) and returns once the deterministic planner Job
// tide-plan-<uid>-1 exists and the cache reflects Phase=Running.
func dispatchPlanPlanner(ctx context.Context, r *PlanReconciler, name types.NamespacedName) {
	ExpectWithOffset(1, reconcileWithRetry(r.Reconcile, name, 4)).To(Succeed())
	var plan tideprojectv1alpha3.Plan
	ExpectWithOffset(1, k8sClient.Get(ctx, name, &plan)).To(Succeed())
	ExpectWithOffset(1, plan.Status.Phase).To(Equal(tideprojectv1alpha3.LevelPhaseRunning))
}

// completePlanPlannerJob marks the deterministic planner Job
// tide-plan-<uid>-1 JobComplete=True, mirroring completeExecutorJob's
// terminal-status shape one level up (completeJobStatus is shared, defined
// in task_verify_dispatch_test.go).
func completePlanPlannerJob(ctx context.Context, plan *tideprojectv1alpha3.Plan) {
	jobName := fmt.Sprintf("tide-plan-%s-1", plan.UID)
	var job batchv1.Job
	ExpectWithOffset(1, k8sClient.Get(ctx, types.NamespacedName{Name: jobName, Namespace: "default"}, &job)).To(Succeed())
	jobPatch := client.MergeFrom(job.DeepCopy())
	completeJobStatus(&job)
	ExpectWithOffset(1, k8sClient.Status().Patch(ctx, &job, jobPatch)).To(Succeed())
}

// completePlanVerifierJob marks the deterministic plan-check verifier Job
// tide-verifier-plan-<uid>-<attempt> JobComplete=True.
func completePlanVerifierJob(ctx context.Context, plan *tideprojectv1alpha3.Plan, attempt int) {
	jobName := podjob.VerifierJobName("plan", string(plan.UID), attempt)
	var job batchv1.Job
	ExpectWithOffset(1, k8sClient.Get(ctx, types.NamespacedName{Name: jobName, Namespace: "default"}, &job)).To(Succeed())
	jobPatch := client.MergeFrom(job.DeepCopy())
	completeJobStatus(&job)
	ExpectWithOffset(1, k8sClient.Status().Patch(ctx, &job, jobPatch)).To(Succeed())
}

// driveToPlanVerifying drives plan all the way to Phase=Verifying with a
// plan-check verifier Job dispatched, choosing childCount children (created
// via makeVerifyChildTask, named "<planName>-child-<i>"). Mirrors the REAL
// two-reconcile materialization shape: handlePlannerJobCompletion can only
// ever run ONCE per planner attempt (reconcilePlannerDispatch's
// tasks-exist early-return permanently blocks re-entry the moment any child
// Task becomes visible — see reconcileWaveMaterialization's own Phase 52
// D-03 comment) — so ValidationState is stamped and the ChildCount gate
// requeues on that FIRST reconcile (with zero Tasks existing yet), children
// are created only AFTER that, and the Verifying transition (now reachable
// via reconcileWaveMaterialization once ValidationState=="Validated") fires
// on a LATER reconcile. Returns the created child Task names.
func driveToPlanVerifying(ctx context.Context, r *PlanReconciler, envReader *mapEnvReader, plan *tideprojectv1alpha3.Plan, name types.NamespacedName, projName string, childCount int) []string {
	dispatchPlanPlanner(ctx, r, name)
	ExpectWithOffset(1, k8sClient.Get(ctx, name, plan)).To(Succeed())

	envReader.SetOut(string(plan.UID), pkgdispatch.EnvelopeOut{
		ExitCode: 0, ChildCount: childCount, CompletedAt: time.Now(),
	})
	completePlanPlannerJob(ctx, plan)

	// First post-completion reconcile: zero child Tasks exist yet, so
	// handlePlannerJobCompletion's ChildCount gate requeues (observed 0 <
	// expected) — this is the ONLY reconcile that ever runs
	// handlePlannerJobCompletion for this attempt, and it stamps
	// ValidationState="Validated" before the requeue.
	_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: name})
	ExpectWithOffset(1, err).NotTo(HaveOccurred())

	childNames := make([]string, 0, childCount)
	for i := range childCount {
		childName := fmt.Sprintf("%s-child-%d", name.Name, i)
		makeVerifyChildTask(childName, name.Name, projName)
		childNames = append(childNames, childName)
	}

	// waitForCacheSync (inside makeVerifyChildTask) only confirms the new
	// Task is visible via a plain Get-by-name — the reconcile path below
	// queries via the SEPARATE .spec.planRef field indexer
	// (taskPlanRefIndexKey), which can lag the primary object-store sync by
	// a beat. Wait for the indexed List itself to observe every child before
	// firing the next reconcile, or the transition below flakes.
	EventuallyWithOffset(1, func() int {
		var taskList tideprojectv1alpha3.TaskList
		if lErr := mgrClient.List(context.Background(), &taskList,
			client.InNamespace(name.Namespace),
			client.MatchingFields{taskPlanRefIndexKey: name.Name},
		); lErr != nil {
			return -1
		}
		return len(taskList.Items)
	}, 5*time.Second, 50*time.Millisecond).Should(Equal(childCount),
		"the .spec.planRef field indexer must observe every created child Task before the next reconcile")

	// Second reconcile: children now exist and ValidationState=="Validated"
	// — reconcilePlannerDispatch's tasks-exist early-return routes straight
	// to reconcileWaveMaterialization, whose Phase 52 D-03 check transitions
	// Running -> Verifying before any wave dispatch.
	_, err = r.Reconcile(ctx, reconcile.Request{NamespacedName: name})
	ExpectWithOffset(1, err).NotTo(HaveOccurred())
	ExpectWithOffset(1, k8sClient.Get(ctx, name, plan)).To(Succeed())
	ExpectWithOffset(1, plan.Status.Phase).To(Equal(tideprojectv1alpha3.LevelPhaseVerifying),
		"a Locked-contract Plan must enter Verifying after child materialization (D-03)")

	// Third reconcile: Reconcile()'s own Verifying routing dispatches the
	// plan-check verifier Job via checkPlanVerifyingState.
	_, err = r.Reconcile(ctx, reconcile.Request{NamespacedName: name})
	ExpectWithOffset(1, err).NotTo(HaveOccurred())

	return childNames
}

var _ = Describe("PlanCheck: plan-check loop dispatch/hold/rails/fail-closed (Phase 52 Plan 07)", Label("envtest", "phase52", "plan-check"), func() {
	ctx := context.Background()

	It("(a) a contracted Plan enters Verifying after materialization, dispatches a plan-check verifier Job, and holds child Task dispatch (D-03 pre-spend invariant)", func() {
		const projName = "pc-proj-hold"
		const planName = "pc-plan-hold"

		makeProjectForTask(projName)
		defer cleanupProject(projName)

		plan := makeVerifyPlan(planName, projName, tideprojectv1alpha3.VerificationSpec{
			Phase: "Locked", Version: 1, GateCommand: "make plan-check",
		})
		envReader := newMapEnvReader()
		r := newVerifyDispatchPlanReconciler(envReader)
		name := types.NamespacedName{Name: planName, Namespace: "default"}

		childNames := driveToPlanVerifying(ctx, r, envReader, plan, name, projName, 1)
		defer cleanupTask(childNames[0])

		verifierJobName := podjob.VerifierJobName("plan", string(plan.UID), 1)
		var verifierJob batchv1.Job
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: verifierJobName, Namespace: "default"}, &verifierJob)).To(Succeed())
		Expect(verifierJob.Labels["tideproject.k8s/role"]).To(Equal("verifier"))
		Expect(verifierJob.Labels["tideproject.k8s/level"]).To(Equal("plan"))
		Expect(verifierJob.Labels["tideproject.k8s/project"]).To(Equal(projName))

		// D-03's pre-spend invariant: the child Task must NOT dispatch while
		// the Plan is Verifying — checkParentApproval's new OR-clause holds it.
		taskR := newVerifyDispatchTaskReconciler(envReader)
		childKey := types.NamespacedName{Name: childNames[0], Namespace: "default"}
		_, tErr := reconcileWithRetryResult(taskR.Reconcile, childKey, 3)
		Expect(tErr).NotTo(HaveOccurred())

		var childTask tideprojectv1alpha3.Task
		Expect(k8sClient.Get(ctx, childKey, &childTask)).To(Succeed())
		executorJobName := podjob.JobName(childTask.UID, 1)
		var executorJob batchv1.Job
		err := k8sClient.Get(ctx, types.NamespacedName{Name: executorJobName, Namespace: "default"}, &executorJob)
		Expect(apierrors.IsNotFound(err)).To(BeTrue(), "no executor Job may exist for a child Task while the parent Plan is Verifying (D-03)")
	})

	It("(b) a Plan with no resolved verification contract keeps today's behavior: phase clears to \"\" with zero plan-check verifier Jobs (off-switch)", func() {
		const projName = "pc-proj-noconfig"
		const planName = "pc-plan-noconfig"

		makeProjectForTask(projName)
		defer cleanupProject(projName)

		plan := makeVerifyPlan(planName, projName, tideprojectv1alpha3.VerificationSpec{})

		envReader := newMapEnvReader()
		r := newVerifyDispatchPlanReconciler(envReader)
		name := types.NamespacedName{Name: planName, Namespace: "default"}

		dispatchPlanPlanner(ctx, r, name)
		Expect(k8sClient.Get(ctx, name, plan)).To(Succeed())

		envReader.SetOut(string(plan.UID), pkgdispatch.EnvelopeOut{
			ExitCode: 0, ChildCount: 0, CompletedAt: time.Now(),
		})
		completePlanPlannerJob(ctx, plan)

		_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: name})
		Expect(err).NotTo(HaveOccurred())

		Expect(k8sClient.Get(ctx, name, plan)).To(Succeed())
		Expect(plan.Status.Phase).To(Equal(""), "a contract-less Plan must keep the pre-Phase-52 clear-to-\"\" behavior")

		verifierJobName := podjob.VerifierJobName("plan", string(plan.UID), 1)
		var verifierJob batchv1.Job
		err = k8sClient.Get(ctx, types.NamespacedName{Name: verifierJobName, Namespace: "default"}, &verifierJob)
		Expect(apierrors.IsNotFound(err)).To(BeTrue(), "no plan-check verifier Job may be created for a contract-less Plan")
	})

	It("(c) defers plan-check dispatch at the ESC-04 concurrency cap without leaking a reservation (Pitfall 6)", func() {
		const projName = "pc-proj-caphit"
		const planName = "pc-plan-caphit"

		makeProjectForTask(projName)
		defer cleanupProject(projName)

		plan := makeVerifyPlan(planName, projName, tideprojectv1alpha3.VerificationSpec{
			Phase: "Locked", Version: 1, GateCommand: "make plan-check",
		})
		envReader := newMapEnvReader()
		r := newVerifyDispatchPlanReconciler(envReader)
		name := types.NamespacedName{Name: planName, Namespace: "default"}

		// Drive to Verifying WITHOUT dispatching yet: two reconciles
		// (mirrors driveToPlanVerifying's first two steps) so the cap can be
		// saturated BEFORE the dispatch-attempt reconcile.
		dispatchPlanPlanner(ctx, r, name)
		Expect(k8sClient.Get(ctx, name, plan)).To(Succeed())
		envReader.SetOut(string(plan.UID), pkgdispatch.EnvelopeOut{
			ExitCode: 0, ChildCount: 1, CompletedAt: time.Now(),
		})
		completePlanPlannerJob(ctx, plan)
		_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: name})
		Expect(err).NotTo(HaveOccurred())
		const childName = "pc-plan-caphit-child-0"
		makeVerifyChildTask(childName, planName, projName)
		defer cleanupTask(childName)
		// See driveToPlanVerifying's identical comment: the field indexer
		// backing this List can lag a plain Get-by-name cache sync.
		Eventually(func() int {
			var taskList tideprojectv1alpha3.TaskList
			if lErr := mgrClient.List(ctx, &taskList,
				client.InNamespace("default"),
				client.MatchingFields{taskPlanRefIndexKey: planName},
			); lErr != nil {
				return -1
			}
			return len(taskList.Items)
		}, 5*time.Second, 50*time.Millisecond).Should(Equal(1))
		_, err = r.Reconcile(ctx, reconcile.Request{NamespacedName: name})
		Expect(err).NotTo(HaveOccurred())
		Expect(k8sClient.Get(ctx, name, plan)).To(Succeed())
		Expect(plan.Status.Phase).To(Equal(tideprojectv1alpha3.LevelPhaseVerifying))

		// Saturate the ESC-04 cap with defaultVerifierConcurrencyCap dummy
		// non-terminal verifier Jobs for this project (role=verifier is
		// level-agnostic — verifierInFlightCount counts across every level).
		fillerJobs := make([]*batchv1.Job, 0, defaultVerifierConcurrencyCap)
		for i := range defaultVerifierConcurrencyCap {
			filler := makeVerifierJob(fmt.Sprintf("pc-verifier-filler-%d", i), "default", projName, false)
			Expect(k8sClient.Create(ctx, filler)).To(Succeed())
			fillerJobs = append(fillerJobs, filler)
		}
		defer func() {
			for _, j := range fillerJobs {
				_ = k8sClient.Delete(ctx, j)
			}
		}()
		Eventually(func() int {
			var jobs batchv1.JobList
			if lErr := mgrClient.List(ctx, &jobs, client.InNamespace("default"),
				client.MatchingLabels{"tideproject.k8s/role": "verifier", "tideproject.k8s/project": projName}); lErr != nil {
				return -1
			}
			return len(jobs.Items)
		}, 5*time.Second, 50*time.Millisecond).Should(Equal(defaultVerifierConcurrencyCap))

		result, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: name})
		Expect(err).NotTo(HaveOccurred())
		Expect(result.RequeueAfter).To(BeNumerically(">", 0), "a cap-hit dispatch must defer via requeue, not error")

		verifierJobName := podjob.VerifierJobName("plan", string(plan.UID), 1)
		var verifierJob batchv1.Job
		err = k8sClient.Get(ctx, types.NamespacedName{Name: verifierJobName, Namespace: "default"}, &verifierJob)
		Expect(apierrors.IsNotFound(err)).To(BeTrue(), "a cap-hit dispatch must not create the plan-check verifier Job")
		Expect(r.Deps.Reservations.TotalReserved()).To(Equal(int64(0)), "a cap-hit deferral must never reserve budget (Pitfall 6 — cap-before-reserve)")
	})

	It("(c2) reserves budget on plan-check dispatch and settles it on consumption (D-10)", func() {
		const projName = "pc-proj-reserve"
		const planName = "pc-plan-reserve"

		makeProjectForTask(projName)
		defer cleanupProject(projName)

		plan := makeVerifyPlan(planName, projName, tideprojectv1alpha3.VerificationSpec{
			Phase: "Locked", Version: 1, GateCommand: "make plan-check",
		})
		envReader := newMapEnvReader()
		r := newVerifyDispatchPlanReconciler(envReader)
		name := types.NamespacedName{Name: planName, Namespace: "default"}

		Expect(r.Deps.Reservations.TotalReserved()).To(Equal(int64(0)), "no reservation before the plan-check verifier ever dispatches")

		childNames := driveToPlanVerifying(ctx, r, envReader, plan, name, projName, 1)
		defer cleanupTask(childNames[0])

		Expect(r.Deps.Reservations.TotalReserved()).To(Equal(int64(500)), "dispatchPlanVerifier must reserve ReserveEstimateCents for plan.UID")

		envReader.SetOut(string(plan.UID), pkgdispatch.EnvelopeOut{
			CompletedAt: time.Now(),
			Verdict:     &pkgdispatch.GateDecision{Verdict: pkgdispatch.VerdictApproved, Summary: "clean"},
		})
		completePlanVerifierJob(ctx, plan, 1)

		_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: name})
		Expect(err).NotTo(HaveOccurred())
		Expect(r.Deps.Reservations.TotalReserved()).To(Equal(int64(0)), "settlePlanVerifierSpend must settle the reservation on consumption")
	})

	It("(d) a malformed/empty plan-check verdict envelope fails closed to the exhaustion path, never to an unblocked \"\" phase (D-10)", func() {
		const projName = "pc-proj-failclosed"
		const planName = "pc-plan-failclosed"

		makeProjectForTask(projName)
		defer cleanupProject(projName)

		plan := makeVerifyPlan(planName, projName, tideprojectv1alpha3.VerificationSpec{
			Phase: "Locked", Version: 1, GateCommand: "make plan-check", OnExhaustion: "escalate",
		})
		envReader := newMapEnvReader()
		r := newVerifyDispatchPlanReconciler(envReader)
		name := types.NamespacedName{Name: planName, Namespace: "default"}

		childNames := driveToPlanVerifying(ctx, r, envReader, plan, name, projName, 1)
		defer cleanupTask(childNames[0])

		// Malformed/empty envelope: a Verdict with an empty verdict field
		// classifies BLOCKED via ClassifyVerdict's own fail-closed default.
		envReader.SetOut(string(plan.UID), pkgdispatch.EnvelopeOut{
			CompletedAt: time.Now(),
			Verdict:     &pkgdispatch.GateDecision{},
		})
		completePlanVerifierJob(ctx, plan, 1)

		_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: name})
		Expect(err).NotTo(HaveOccurred())

		Expect(k8sClient.Get(ctx, name, plan)).To(Succeed())
		Expect(plan.Status.Phase).To(Equal(tideprojectv1alpha3.LevelPhaseVerifyHalted),
			"a malformed/fail-closed verdict must escalate, never leave the Plan unblocked at \"\"")
		Expect(plan.Status.Phase).NotTo(Equal(""), "fail-closed proof: the exhaustion path can never reach the unblocked \"\" state")

		var proj tideprojectv1alpha3.Project
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: projName, Namespace: "default"}, &proj)).To(Succeed())
		halted := false
		for _, c := range proj.Status.Conditions {
			if c.Type == tideprojectv1alpha3.ConditionVerifyHalt && c.Status == metav1.ConditionTrue {
				halted = true
			}
		}
		Expect(halted).To(BeTrue(), "escalate must freeze the project-wide ConditionVerifyHalt")
	})

	It("(e) an APPROVED plan-check verdict clears Verifying and unblocks child Task dispatch", func() {
		const projName = "pc-proj-approved"
		const planName = "pc-plan-approved"

		makeProjectForTask(projName)
		defer cleanupProject(projName)

		plan := makeVerifyPlan(planName, projName, tideprojectv1alpha3.VerificationSpec{
			Phase: "Locked", Version: 1, GateCommand: "make plan-check",
		})
		envReader := newMapEnvReader()
		r := newVerifyDispatchPlanReconciler(envReader)
		name := types.NamespacedName{Name: planName, Namespace: "default"}

		childNames := driveToPlanVerifying(ctx, r, envReader, plan, name, projName, 1)
		defer cleanupTask(childNames[0])

		envReader.SetOut(string(plan.UID), pkgdispatch.EnvelopeOut{
			CompletedAt: time.Now(),
			Verdict:     &pkgdispatch.GateDecision{Verdict: pkgdispatch.VerdictApproved, Summary: "plan looks correct"},
		})
		completePlanVerifierJob(ctx, plan, 1)

		_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: name})
		Expect(err).NotTo(HaveOccurred())

		Expect(k8sClient.Get(ctx, name, plan)).To(Succeed())
		Expect(plan.Status.Phase).To(Equal(""), "an APPROVED plan-check verdict must clear Verifying back to \"\"")
		Expect(plan.Status.LoopStatus.ExitReason).To(Equal(tideprojectv1alpha3.ExitApproved))

		// Child Task dispatch must now proceed: checkParentApproval no longer
		// holds it (Phase is neither AwaitingApproval nor Verifying).
		taskR := newVerifyDispatchTaskReconciler(envReader)
		childKey := types.NamespacedName{Name: childNames[0], Namespace: "default"}
		Expect(reconcileWithRetry(taskR.Reconcile, childKey, 4)).To(Succeed())

		var childTask tideprojectv1alpha3.Task
		Expect(k8sClient.Get(ctx, childKey, &childTask)).To(Succeed())
		executorJobName := podjob.JobName(childTask.UID, 1)
		var executorJob batchv1.Job
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: executorJobName, Namespace: "default"}, &executorJob)).To(Succeed(),
			"an executor Job must now exist for the child Task once the plan-check verdict is APPROVED")
	})
})
