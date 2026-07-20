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
	completePlanPlannerJobAttempt(ctx, plan, 1)
}

// completePlanPlannerJobAttempt mirrors completePlanPlannerJob, generalized
// to an arbitrary attempt number (Phase 52 D-04/D-06: a re-plan mints
// planner attempt 2, 3, ... via the SAME Iteration-derived Job-name formula
// reconcilePlannerDispatch itself uses).
func completePlanPlannerJobAttempt(ctx context.Context, plan *tideprojectv1alpha3.Plan, attempt int) {
	jobName := fmt.Sprintf("tide-plan-%s-%d", plan.UID, attempt)
	var job batchv1.Job
	ExpectWithOffset(1, k8sClient.Get(ctx, types.NamespacedName{Name: jobName, Namespace: "default"}, &job)).To(Succeed())
	jobPatch := client.MergeFrom(job.DeepCopy())
	completeJobStatus(&job)
	ExpectWithOffset(1, k8sClient.Status().Patch(ctx, &job, jobPatch)).To(Succeed())
	waitForJobTerminalCacheSync(ctx, jobName, "default")
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
	waitForJobTerminalCacheSync(ctx, jobName, "default")
}

// waitForJobTerminalCacheSync waits for the mgrClient cache to observe a
// terminal Job status (isJobTerminal). Required whenever a test patches a
// Job's status via the direct k8sClient immediately before a reconcile that
// reads the SAME Job via the reconciler's cached client (Client: mgrClient
// in newVerifyDispatchPlanReconciler/newVerifyDispatchTaskReconciler) —
// waitForCacheSync's plain existence check is not enough for a status-only
// mutation on an object that was already cached before the patch (Rule 1:
// this cache-sync race reproduced live under load, exhausting a re-planned
// attempt's verdict one reconcile early — see 52-09-SUMMARY.md deviations).
func waitForJobTerminalCacheSync(ctx context.Context, name, namespace string) {
	key := types.NamespacedName{Name: name, Namespace: namespace}
	EventuallyWithOffset(1, func() bool {
		var job batchv1.Job
		if err := mgrClient.Get(ctx, key, &job); err != nil {
			return false
		}
		return isJobTerminal(&job)
	}, 5*time.Second, 50*time.Millisecond).Should(BeTrue(),
		"timed out waiting for cache to observe terminal status on Job %s/%s", namespace, name)
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

// driveThroughPlanRepairCycle completes the plan-check verifier Job at
// attempt with verdict (expected REPAIRABLE and non-exhausting — the caller
// picks findings that keep the loop alive), and drives the Plan all the way
// through dispatchPlanRepair's delete-then-recreate reconciliation to
// attempt+1's own Verifying state with its plan-check verifier Job
// dispatched. Mirrors driveToPlanVerifying's own multi-reconcile shape
// (Phase 52 Plan 07), generalized to a re-plan cycle (Phase 52 Plan 09,
// D-04). Returns the fresh attempt's child Task names.
func driveThroughPlanRepairCycle(ctx context.Context, r *PlanReconciler, envReader *mapEnvReader, plan *tideprojectv1alpha3.Plan, name types.NamespacedName, projName string, attempt int, verdict *pkgdispatch.GateDecision, childCount int) []string {
	envReader.SetOut(string(plan.UID), pkgdispatch.EnvelopeOut{CompletedAt: time.Now(), Verdict: verdict})
	completePlanVerifierJob(ctx, plan, attempt)

	// This reconcile runs handlePlanVerifierCompletion -> repairOrHaltPlan ->
	// dispatchPlanRepair: stamps the D-04 findings annotation, deletes the
	// rejected attempt's child Tasks, bumps LoopStatus.Iteration, and clears
	// Phase off Verifying.
	_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: name})
	ExpectWithOffset(1, err).NotTo(HaveOccurred())

	nextAttempt := attempt + 1

	// dispatchPlanRepair's Delete calls land synchronously within the
	// reconcile above, but the .spec.planRef field indexer backing this List
	// can lag the primary object-store sync by a beat (52-07's identical
	// flake-fix rationale for makeVerifyChildTask, applied to deletion).
	EventuallyWithOffset(1, func() int {
		var taskList tideprojectv1alpha3.TaskList
		if lErr := mgrClient.List(context.Background(), &taskList,
			client.InNamespace(name.Namespace),
			client.MatchingFields{taskPlanRefIndexKey: name.Name},
		); lErr != nil {
			return -1
		}
		return len(taskList.Items)
	}, 5*time.Second, 50*time.Millisecond).Should(Equal(0),
		"dispatchPlanRepair must delete every rejected-attempt child Task before the fresh planner dispatch (RESEARCH Pitfall 3)")

	// reconcilePlannerDispatch's dispatch tail re-engages now the list is
	// empty (the tasks-exist early-return no longer fires) and creates the
	// fresh, findings-seeded planner attempt.
	_, err = r.Reconcile(ctx, reconcile.Request{NamespacedName: name})
	ExpectWithOffset(1, err).NotTo(HaveOccurred())

	plannerJobName := fmt.Sprintf("tide-plan-%s-%d", plan.UID, nextAttempt)
	var plannerJob batchv1.Job
	ExpectWithOffset(1, k8sClient.Get(ctx, types.NamespacedName{Name: plannerJobName, Namespace: "default"}, &plannerJob)).To(Succeed(),
		"the fresh, findings-seeded planner attempt must dispatch once the rejected attempt's children are gone")

	envReader.SetOut(string(plan.UID), pkgdispatch.EnvelopeOut{ExitCode: 0, ChildCount: childCount, CompletedAt: time.Now()})
	completePlanPlannerJobAttempt(ctx, plan, nextAttempt)

	// First post-completion reconcile: zero child Tasks exist yet for THIS
	// attempt, so the ChildCount gate requeues — this is also the reconcile
	// that clears replanFindingsAnnotation (D-04's consumption point).
	_, err = r.Reconcile(ctx, reconcile.Request{NamespacedName: name})
	ExpectWithOffset(1, err).NotTo(HaveOccurred())

	childNames := make([]string, 0, childCount)
	for i := range childCount {
		childName := fmt.Sprintf("%s-child-a%d-%d", name.Name, nextAttempt, i)
		makeVerifyChildTask(childName, name.Name, projName)
		childNames = append(childNames, childName)
	}

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

	// Second reconcile: the fresh attempt's children now exist and
	// ValidationState=="Validated" — transitions Running -> Verifying (D-03
	// applies identically to every planner attempt, not just the first).
	_, err = r.Reconcile(ctx, reconcile.Request{NamespacedName: name})
	ExpectWithOffset(1, err).NotTo(HaveOccurred())
	ExpectWithOffset(1, k8sClient.Get(ctx, name, plan)).To(Succeed())
	ExpectWithOffset(1, plan.Status.Phase).To(Equal(tideprojectv1alpha3.LevelPhaseVerifying),
		"the re-planned attempt's own children must also gate through Verifying (D-03 applies to every attempt)")

	// Third reconcile: dispatches the fresh attempt's own plan-check
	// verifier Job.
	_, err = r.Reconcile(ctx, reconcile.Request{NamespacedName: name})
	ExpectWithOffset(1, err).NotTo(HaveOccurred())

	verifierJobName := podjob.VerifierJobName("plan", string(plan.UID), nextAttempt)
	var verifierJob batchv1.Job
	ExpectWithOffset(1, k8sClient.Get(ctx, types.NamespacedName{Name: verifierJobName, Namespace: "default"}, &verifierJob)).To(Succeed(),
		"the re-planned attempt must dispatch its own plan-check verifier Job")

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

// replanScoreThirteen is a REPAIRABLE verdict scoring severityScore(3,1)=13
// (1 blocker + 2 advisory findings) — the RePlan family's shared "first
// pass" fixture, reused across specs that need a stable non-zero score to
// compare a later verdict against.
func replanScoreThirteen(summary string) *pkgdispatch.GateDecision {
	return &pkgdispatch.GateDecision{
		Verdict: pkgdispatch.VerdictRepairable,
		Summary: summary,
		Findings: []pkgdispatch.Finding{
			{Dimension: "dependency-correctness", Severity: "blocker", Evidence: "child depends on itself"},
			{Dimension: "file-touch", Severity: "advisory", Evidence: "declared path never referenced"},
			{Dimension: "goal-alignment", Severity: "advisory", Evidence: "scope drift from phase brief"},
		},
	}
}

var _ = Describe("RePlan: findings-seeded re-plan, one-shot at defaults, severity-weighted stall (Phase 52 Plan 09, D-04/D-05/D-06)", Label("envtest", "phase52", "replan"), func() {
	ctx := context.Background()

	It("(a) exactly one re-plan at default maxIterations:1: a REPAIRABLE verdict re-dispatches a findings-seeded planner attempt 2, and a second REPAIRABLE verdict exhausts — no third planner Job (D-04)", func() {
		const projName = "rp-proj-onereplan"
		const planName = "rp-plan-onereplan"

		makeProjectForTask(projName)
		defer cleanupProject(projName)

		plan := makeVerifyPlan(planName, projName, tideprojectv1alpha3.VerificationSpec{
			Phase: "Locked", Version: 1, GateCommand: "make plan-check", OnExhaustion: "escalate",
		})
		envReader := newMapEnvReader()
		r := newVerifyDispatchPlanReconciler(envReader)
		name := types.NamespacedName{Name: planName, Namespace: "default"}

		childNames1 := driveToPlanVerifying(ctx, r, envReader, plan, name, projName, 1)
		defer cleanupTask(childNames1[0])

		verdict1 := replanScoreThirteen("dependency ordering looks wrong")
		childNames2 := driveThroughPlanRepairCycle(ctx, r, envReader, plan, name, projName, 1, verdict1, 1)
		defer cleanupTask(childNames2[0])

		Expect(k8sClient.Get(ctx, name, plan)).To(Succeed())
		Expect(plan.Status.LoopStatus.Iteration).To(Equal(int32(1)), "D-06: the quality-re-plan counter increments exactly once")
		Expect(plan.Annotations["tideproject.k8s/replan-findings"]).To(BeEmpty(),
			"the D-04 findings annotation must clear once the fresh attempt materializes (consumption point)")

		plannerJob1Name := fmt.Sprintf("tide-plan-%s-1", plan.UID)
		plannerJob2Name := fmt.Sprintf("tide-plan-%s-2", plan.UID)
		var plannerJob2 batchv1.Job
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: plannerJob2Name, Namespace: "default"}, &plannerJob2)).To(Succeed(),
			"a second planner Job (tide-plan-<uid>-2) must dispatch once the rejected attempt's children are gone")
		Expect(plannerJob2.Name).NotTo(Equal(plannerJob1Name), "the re-plan attempt must NOT collide with the rejected attempt's Job name")

		envIn2 := decodeEnvelopeIn(&plannerJob2)
		Expect(envIn2.RepairFindings).To(HaveLen(3), "the fresh planner attempt must be seeded with the D-04 findings block")
		Expect(envIn2.RepairFindings[0].Severity).To(Equal("blocker"))

		// Second REPAIRABLE verdict on the re-planned attempt: default
		// maxIterations:1 means Iteration(1) >= MaxIterations(1) —
		// exhausted regardless of score (the MaxIterations boundary, not
		// the stall check).
		envReader.SetOut(string(plan.UID), pkgdispatch.EnvelopeOut{
			CompletedAt: time.Now(),
			Verdict: &pkgdispatch.GateDecision{
				Verdict:  pkgdispatch.VerdictRepairable,
				Summary:  "still not right",
				Findings: []pkgdispatch.Finding{{Dimension: "goal-alignment", Severity: "advisory", Evidence: "still drifting"}},
			},
		})
		completePlanVerifierJob(ctx, plan, 2)

		_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: name})
		Expect(err).NotTo(HaveOccurred())

		Expect(k8sClient.Get(ctx, name, plan)).To(Succeed())
		Expect(plan.Status.Phase).To(Equal(tideprojectv1alpha3.LevelPhaseVerifyHalted),
			"a second REPAIRABLE verdict must exhaust (default maxIterations:1 allows exactly one re-plan), never dispatch a third planner attempt")
		Expect(plan.Status.LoopStatus.ExitReason).To(Equal(tideprojectv1alpha3.ExitEscalated))

		plannerJob3Name := fmt.Sprintf("tide-plan-%s-3", plan.UID)
		err = k8sClient.Get(ctx, types.NamespacedName{Name: plannerJob3Name, Namespace: "default"}, &batchv1.Job{})
		Expect(apierrors.IsNotFound(err)).To(BeTrue(), "exactly ONE re-plan at default maxIterations:1 — no third planner Job may ever dispatch")
	})

	It("(b) the rejected attempt's child Task is deleted outright, never merely un-dispatched — no executor Job ever exists for it (T-52-27 stale-attempt invariant)", func() {
		const projName = "rp-proj-staleinvariant"
		const planName = "rp-plan-staleinvariant"

		makeProjectForTask(projName)
		defer cleanupProject(projName)

		plan := makeVerifyPlan(planName, projName, tideprojectv1alpha3.VerificationSpec{
			Phase: "Locked", Version: 1, GateCommand: "make plan-check",
		})
		envReader := newMapEnvReader()
		r := newVerifyDispatchPlanReconciler(envReader)
		name := types.NamespacedName{Name: planName, Namespace: "default"}

		childNames := driveToPlanVerifying(ctx, r, envReader, plan, name, projName, 1)
		rejectedChildKey := types.NamespacedName{Name: childNames[0], Namespace: "default"}

		var rejectedChild tideprojectv1alpha3.Task
		Expect(k8sClient.Get(ctx, rejectedChildKey, &rejectedChild)).To(Succeed())
		rejectedUID := rejectedChild.UID

		envReader.SetOut(string(plan.UID), pkgdispatch.EnvelopeOut{
			CompletedAt: time.Now(),
			Verdict:     replanScoreThirteen("dependency ordering looks wrong"),
		})
		completePlanVerifierJob(ctx, plan, 1)

		_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: name})
		Expect(err).NotTo(HaveOccurred())

		// The rejected attempt's child Task object is gone entirely —
		// there is nothing left for a Task reconcile to ever dispatch.
		err = k8sClient.Get(ctx, rejectedChildKey, &tideprojectv1alpha3.Task{})
		Expect(apierrors.IsNotFound(err)).To(BeTrue(), "the rejected attempt's child Task must be deleted, not merely left un-dispatched")

		executorJobName := podjob.JobName(rejectedUID, 1)
		var executorJob batchv1.Job
		err = k8sClient.Get(ctx, types.NamespacedName{Name: executorJobName, Namespace: "default"}, &executorJob)
		Expect(apierrors.IsNotFound(err)).To(BeTrue(), "no executor Job may ever exist for the rejected attempt's deleted child Task (T-52-27)")
	})

	It("(c) severity-weighted stall detection halts early at maxIterations:2 without consuming the remaining iteration (D-05)", func() {
		const projName = "rp-proj-stall"
		const planName = "rp-plan-stall"

		makeProjectForTask(projName)
		defer cleanupProject(projName)

		plan := makeVerifyPlan(planName, projName, tideprojectv1alpha3.VerificationSpec{
			Phase: "Locked", Version: 1, GateCommand: "make plan-check", MaxIterations: 2, OnExhaustion: "escalate",
		})
		envReader := newMapEnvReader()
		r := newVerifyDispatchPlanReconciler(envReader)
		name := types.NamespacedName{Name: planName, Namespace: "default"}

		childNames1 := driveToPlanVerifying(ctx, r, envReader, plan, name, projName, 1)
		defer cleanupTask(childNames1[0])

		scoreThirteen := replanScoreThirteen("first pass: dependency ordering looks wrong")
		childNames2 := driveThroughPlanRepairCycle(ctx, r, envReader, plan, name, projName, 1, scoreThirteen, 1)
		defer cleanupTask(childNames2[0])

		Expect(k8sClient.Get(ctx, name, plan)).To(Succeed())
		Expect(plan.Status.LoopStatus.Iteration).To(Equal(int32(1)), "one re-plan consumed so far; one remains under maxIterations:2")

		// SAME score (13) on the re-planned attempt — non-improving.
		envReader.SetOut(string(plan.UID), pkgdispatch.EnvelopeOut{CompletedAt: time.Now(), Verdict: scoreThirteen})
		completePlanVerifierJob(ctx, plan, 2)

		_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: name})
		Expect(err).NotTo(HaveOccurred())

		Expect(k8sClient.Get(ctx, name, plan)).To(Succeed())
		Expect(plan.Status.Phase).To(Equal(tideprojectv1alpha3.LevelPhaseVerifyHalted),
			"a non-improving re-plan must halt at the stall check, even though maxIterations:2 would allow another attempt")
		Expect(plan.Status.LoopStatus.Iteration).To(Equal(int32(1)), "the stall check must fire BEFORE consuming the remaining iteration")

		// NO third planner Job — the stall check pre-empted the
		// MaxIterations boundary, which alone would have allowed one more
		// re-plan (Pitfall 3's warning sign inverted into a positive
		// assertion: the second Job exists, per driveThroughPlanRepairCycle
		// above, but the third never does).
		plannerJob3Name := fmt.Sprintf("tide-plan-%s-3", plan.UID)
		err = k8sClient.Get(ctx, types.NamespacedName{Name: plannerJob3Name, Namespace: "default"}, &batchv1.Job{})
		Expect(apierrors.IsNotFound(err)).To(BeTrue(), "a stalled re-plan must never dispatch a further planner attempt")
	})

	It("(c-control) an improving re-plan score proceeds and consumes the remaining iteration at maxIterations:2 (D-05 control)", func() {
		const projName = "rp-proj-improve"
		const planName = "rp-plan-improve"

		makeProjectForTask(projName)
		defer cleanupProject(projName)

		plan := makeVerifyPlan(planName, projName, tideprojectv1alpha3.VerificationSpec{
			Phase: "Locked", Version: 1, GateCommand: "make plan-check", MaxIterations: 2, OnExhaustion: "escalate",
		})
		envReader := newMapEnvReader()
		r := newVerifyDispatchPlanReconciler(envReader)
		name := types.NamespacedName{Name: planName, Namespace: "default"}

		childNames1 := driveToPlanVerifying(ctx, r, envReader, plan, name, projName, 1)
		defer cleanupTask(childNames1[0])

		scoreThirteen := replanScoreThirteen("first pass: dependency ordering looks wrong")
		childNames2 := driveThroughPlanRepairCycle(ctx, r, envReader, plan, name, projName, 1, scoreThirteen, 1)
		defer cleanupTask(childNames2[0])

		// Improving score: 5 (zero blockers, 5 advisories) < 13.
		scoreFive := &pkgdispatch.GateDecision{
			Verdict: pkgdispatch.VerdictRepairable,
			Summary: "second pass: only minor findings remain",
			Findings: []pkgdispatch.Finding{
				{Dimension: "file-touch", Severity: "advisory", Evidence: "a"},
				{Dimension: "file-touch", Severity: "advisory", Evidence: "b"},
				{Dimension: "goal-alignment", Severity: "advisory", Evidence: "c"},
				{Dimension: "goal-alignment", Severity: "advisory", Evidence: "d"},
				{Dimension: "goal-alignment", Severity: "advisory", Evidence: "e"},
			},
		}
		childNames3 := driveThroughPlanRepairCycle(ctx, r, envReader, plan, name, projName, 2, scoreFive, 1)
		defer cleanupTask(childNames3[0])

		Expect(k8sClient.Get(ctx, name, plan)).To(Succeed())
		Expect(plan.Status.Phase).To(Equal(tideprojectv1alpha3.LevelPhaseVerifying),
			"an improving re-plan must proceed and consume the remaining iteration — a third planner attempt dispatches")
		Expect(plan.Status.LoopStatus.Iteration).To(Equal(int32(2)), "the second (final, maxIterations:2) re-plan consumed the remaining iteration")

		plannerJob3Name := fmt.Sprintf("tide-plan-%s-3", plan.UID)
		var plannerJob3 batchv1.Job
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: plannerJob3Name, Namespace: "default"}, &plannerJob3)).To(Succeed(),
			"an improving re-plan must consume the remaining iteration and dispatch a third planner attempt")
	})
})
