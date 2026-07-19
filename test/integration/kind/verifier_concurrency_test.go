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

// verifier_concurrency_test.go — Layer B kind integration spec for Plan 51-08
// Task 1.
//
// Coverage: ESC-04 (T-51-08 STRIDE DoS mitigation) — proves that concurrent
// role=verifier Job dispatch stays under the sized cap (the run-2b
// single-node-OOM guard), that a cap-hit dispatch DEFERS rather than leaking
// a slot, and that the in-flight count drains to zero as verifier Jobs
// complete — mirroring the shape of internal/controller/task_controller.go's
// verifierInFlightCount (dispatch_helpers.go:535) cap-before-acquire gate
// (defaultVerifierConcurrencyCap, task_controller.go:2008), but observed from
// a REAL kind cluster via real Job objects rather than envtest's fake client.
//
// Analogs (per 51-PATTERNS.md): configmap_planner_concurrency_test.go (the
// sized-cap-value precedent) + chaos_resume_test.go (real-Job-lifecycle Layer
// B spec shape — typed fixture builders, By("Pillar N: ...")-style staged
// assertions, AfterEach exportKindLogs-on-failure).
//
// Cap value: defaultVerifierConcurrencyCap (internal/controller/task_controller.go)
// is unexported, so it cannot be imported here — verifierConcurrencyCap below
// pins the same literal value (=2), per 51-06-SUMMARY.md's own "Decisions
// Made" note ("Claude's Discretion... Plan 08's kind test pins/re-tunes the
// cap value"). If a future plan changes the source constant, update this
// literal in the same commit.
//
// KNOWN GAP — internal.controller.TaskReconcilerDeps.VerifierImage (consumed
// by dispatchVerifier's BuildOptions.SubagentImage, task_controller.go:2106)
// is NOT wired anywhere in cmd/manager/main.go as of this commit (confirmed
// via `grep -n VerifierImage cmd/manager/main.go` returning zero hits inside
// the TaskReconciler's Deps literal — every sibling image field
// (CredproxyImage, ReporterImage, TidePushImage, ImportImage) IS wired from a
// flag/env var; VerifierImage alone is not). Until a flag/env var wires it
// (mirroring --credproxy-image's convention), every dispatchVerifier Job
// Create call in a real cluster resolves to an empty container image, which
// the API server rejects at admission ("spec.template.spec.containers[0]
// .image: Required value") — dispatchVerifier returns an error, the
// reconcile requeues, and checkVerifyingState's NotFound-retry path repeats
// the same failing Create indefinitely. Concretely: this spec's cap/drain
// assertions below will trivially pass (zero Jobs are ever actually
// persisted) but the final "no Task stranded in Verifying" assertion WILL
// correctly FAIL until the gap is closed — a deliberate fail-loud design
// (see that assertion's own comment). Closing the gap (a cmd/manager/main.go
// + likely a chart values change) is out of this plan's declared file scope
// (files_modified: this file only) and is called out as a prerequisite in
// this plan's own Task 2 human-verify runbook.
//
// KNOWN LIMITATION — the in-process budget.ReservationStore no-leak
// invariant (task_controller.go's dispatchVerifier Reserve/Release pair) is
// NOT independently re-provable from this Layer B spec: unlike executor/
// planner Jobs, a verifier Job's BuildOptions never sets EstimatedCostCents
// (confirmed via `grep -n EstimatedCostCents internal/controller/
// task_controller.go` — the executor/planner dispatch sites pass it,
// dispatchVerifier's podjob.BuildOptions literal does not), so verifier Jobs
// never carry the tideproject.k8s/estimated-cost label
// budget.RederiveReservations reads to reconstruct reservation state from
// outside the manager process. That in-process invariant (TotalReserved()==0
// after a cap-hit-then-complete cycle) is already proven by
// internal/controller/task_verify_dispatch_test.go's envtest cap-hit spec.
// This kind spec instead proves the externally-observable half of ESC-04:
// live Job-count-under-cap, no dispatch-slot leak, and drain-to-zero.
//
// Single-node OOM discipline (CLAUDE.md Operating Notes): this spec must run
// within the same single kind node as every other Layer B spec's executor
// dispatch — it deliberately dispatches only verifierConcurrencyTaskCount
// (3) contract-bearing Tasks (one over the cap), not a wide fan-out, keeping
// concurrent Job/Pod pressure bounded alongside the executor concurrency cap
// (PREFLIGHT-01, plannerConcurrency: 4) other specs in this suite exercise.

import (
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	batchv1 "k8s.io/api/batch/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	tideprojectv1alpha3 "github.com/jsquirrelz/tide/api/v1alpha3"
)

const (
	// verifierConcurrencyNS is the namespace this spec applies its fixtures into.
	verifierConcurrencyNS = "verifier-concurrency-test"

	// verifierConcurrencyCap pins internal/controller/task_controller.go's
	// unexported defaultVerifierConcurrencyCap (see header comment above).
	verifierConcurrencyCap = 2

	// verifierConcurrencyTaskCount deliberately exceeds verifierConcurrencyCap
	// by one, so at least one dispatch must observe a cap-hit deferral
	// (Pitfall 6 — the deferred requeue happens BEFORE any reservation or Job
	// create, per dispatchVerifier's own doc comment) rather than dispatching
	// immediately.
	verifierConcurrencyTaskCount = 3

	// verifierRoleLabel/verifierRoleValue mirror the label
	// dispatch_helpers.go's verifierInFlightCount selects on
	// (tideproject.k8s/role=verifier), stamped by dispatchVerifier at its
	// Job-create call site (task_controller.go:2125, mirroring the
	// git-writer Job-labeling convention).
	verifierRoleLabel = "tideproject.k8s/role"
	verifierRoleValue = "verifier"
)

var _ = Describe("Verifier concurrent-dispatch stays under the sized cap (ESC-04)", Label("kind"), func() {
	BeforeEach(func() {
		skipIfCRDsOnlyMode()
	})

	AfterEach(func() {
		deleteNamespace(verifierConcurrencyNS)
		if CurrentSpecReport().Failed() {
			exportKindLogs()
		}
	})

	It("concurrent role=verifier Job count never exceeds the sized cap and drains to zero", func() {
		ns := verifierConcurrencyNS
		projectName := ns + "-project"
		planName := ns + "-verify-plan"

		By("Creating the Project/Milestone/Phase hierarchy (parent-only; Plan+Tasks supplied below)")
		Expect(createProjectHierarchy(ctx, ns)).To(Succeed())
		Expect(createFixture(ctx, newStubPlan(ns, planName, ns+"-phase", withPlanProjectLabel(projectName)))).To(Succeed())

		By(fmt.Sprintf("Applying %d contract-bearing Tasks (locked verification contract; cap=%d)",
			verifierConcurrencyTaskCount, verifierConcurrencyCap))
		taskNames := make([]string, 0, verifierConcurrencyTaskCount)
		for i := range verifierConcurrencyTaskCount {
			name := fmt.Sprintf("verify-task-%d", i)
			taskNames = append(taskNames, name)
			task := newStubTask(ns, name, planName,
				withTaskProjectLabel(projectName),
				withPromptPath(fmt.Sprintf("children/task-%02d.json", i+1)),
				withTestMode("success"),
				// A locked, real (not decorative) gate command — every
				// contract-bearing Task authors a genuine pass-criterion even
				// though this spec's own assertions never inspect the verdict.
				withVerification("true", 1),
			)
			Expect(createFixture(ctx, task)).To(Succeed())
		}

		By("Waiting for every Task to reach executor-complete (Phase=Verifying) — proves dispatchVerifier was attempted for all N")
		// gateChecks stamps Status.Phase=Verifying BEFORE calling dispatchVerifier
		// (task_controller.go:1487, unconditionally — regardless of whether the
		// cap check inside dispatchVerifier immediately dispatches or defers),
		// so this wait is independent of the VerifierImage gap documented above.
		for _, name := range taskNames {
			n := name
			Eventually(func() string {
				t := &tideprojectv1alpha3.Task{}
				if err := k8sClient.Get(ctx, client.ObjectKey{Name: n, Namespace: ns}, t); err != nil {
					return ""
				}
				return t.Status.Phase
			}, 3*time.Minute, 3*time.Second).Should(Equal(tideprojectv1alpha3.LevelPhaseVerifying),
				"Task %s should reach Verifying (executor complete; verifier dispatch attempted, Phase 51 EXEC-04)", n)
		}

		By("Polling the concurrent role=verifier Job count over a window: must never exceed the sized cap")
		maxObserved := 0
		pollDeadline := time.Now().Add(90 * time.Second)
		for time.Now().Before(pollDeadline) {
			n, err := countNonTerminalVerifierJobs(ns, projectName)
			Expect(err).NotTo(HaveOccurred(), "listing role=verifier Jobs must not error mid-poll")
			if n > maxObserved {
				maxObserved = n
			}
			Expect(n).To(BeNumerically("<=", verifierConcurrencyCap),
				"concurrent role=verifier Jobs (%d) must never exceed the sized cap (%d) — ESC-04/T-51-08 OOM guard", n, verifierConcurrencyCap)
			time.Sleep(2 * time.Second)
		}
		GinkgoWriter.Printf("verifier concurrency: max observed in-flight = %d (cap = %d)\n", maxObserved, verifierConcurrencyCap)

		By("Waiting for concurrent verifier Job count to drain to zero (excess dispatch requeues rather than leaking a slot)")
		Eventually(func() (int, error) {
			return countNonTerminalVerifierJobs(ns, projectName)
		}, 5*time.Minute, 3*time.Second).Should(Equal(0),
			"concurrent role=verifier Job count must drain to zero as verifier Jobs complete (Pitfall 6 no-leak)")

		By("Confirming no Task remains stranded in Verifying once dispatch has drained")
		// A cap-hit-deferred Task retries dispatch every 10s via
		// checkVerifyingState's NotFound-retry path (task_controller.go) until
		// a slot opens; once the verifier Job count above has drained to zero,
		// every Task must have reached a REAL terminal outcome (Succeeded or
		// Failed — this spec is verdict-agnostic, see header comment: the stub
		// verifier envelope carries no Verdict, which fail-closes to Failed
		// per Plan 07's handleVerifierCompletion, and that is an expected,
		// acceptable outcome here). Wrapped in Eventually (not a bare Expect)
		// to absorb the same cached-client/direct-client completion-patch
		// race documented in 51-06-SUMMARY.md and 51-07-SUMMARY.md.
		//
		// NOTE: until the VerifierImage gap (header comment above) is closed,
		// this assertion is EXPECTED to fail — the cap/drain assertions above
		// pass trivially (zero verifier Jobs are ever actually persisted) but
		// dispatchVerifier's Job Create keeps erroring, so Tasks never leave
		// Verifying. That is the correct, fail-loud signal for the gap, not a
		// bug in this spec.
		for _, name := range taskNames {
			n := name
			Eventually(func() string {
				t := &tideprojectv1alpha3.Task{}
				if err := k8sClient.Get(ctx, client.ObjectKey{Name: n, Namespace: ns}, t); err != nil {
					return "get-error"
				}
				return t.Status.Phase
			}, 60*time.Second, 3*time.Second).ShouldNot(Equal(tideprojectv1alpha3.LevelPhaseVerifying),
				"Task %s must not remain stranded in Verifying once the verifier Job count has drained to zero", n)
		}

		GinkgoWriter.Println("ESC-04: verifier concurrent-dispatch stayed under the sized cap and drained to zero")
	})
})

// ---- helpers (file-local; avoid colliding with existing exported helpers) ----

// withVerification returns a taskOpt that stamps a locked, contract-bearing
// VerificationSpec (Phase 51 TASK-01) — the single predicate
// hasVerificationContract(task) checks (GateCommand != "" && Phase ==
// "Locked", task_controller.go) to route a Task's executor-complete through
// the Verifying sub-state instead of the legacy direct-to-Succeeded path.
func withVerification(gateCommand string, maxIterations int32) taskOpt {
	return func(t *tideprojectv1alpha3.Task) {
		t.Spec.Verification = tideprojectv1alpha3.VerificationSpec{
			Phase:         "Locked",
			GateCommand:   gateCommand,
			MaxIterations: maxIterations,
			OnExhaustion:  "requireApproval",
		}
	}
}

// countNonTerminalVerifierJobs lists role=verifier Jobs scoped to (ns,
// projectName) via the suite's k8sClient and counts those that are NOT
// terminal (JobComplete or JobFailed condition True) — a Layer B,
// external-client observation mirroring
// internal/controller/dispatch_helpers.go's verifierInFlightCount (same
// project-scoped label match: tideproject.k8s/role=verifier +
// tideproject.k8s/project=<projectName>), but read directly from the API
// server rather than the reconciler's own cached client. This is
// deliberately a ground-truth read, not a copy of the reconciler's cap-gate
// logic — the spec's assertion is that the REAL Job population observed
// externally never exceeds the cap, independent of what the controller's own
// (possibly cache-lagged) view of itself believes.
func countNonTerminalVerifierJobs(ns, projectName string) (int, error) {
	var jobs batchv1.JobList
	if err := k8sClient.List(ctx, &jobs,
		client.InNamespace(ns),
		client.MatchingLabels{
			verifierRoleLabel: verifierRoleValue,
			labelProject:      projectName,
		},
	); err != nil {
		return 0, err
	}
	n := 0
	for i := range jobs.Items {
		// A Job on its way out (DeletionTimestamp set) must not hold a cap
		// slot — mirrors plannerInFlightCount/verifierInFlightCount's own
		// exclusion (dispatch_helpers.go).
		if jobs.Items[i].DeletionTimestamp != nil {
			continue
		}
		if !isJobTerminalVerifier(&jobs.Items[i]) {
			n++
		}
	}
	return n, nil
}

// isJobTerminalVerifier mirrors internal/controller/task_controller.go's
// unexported isJobTerminal (JobComplete or JobFailed condition True). A
// file-local copy — same same-package precedent as chaos_resume_test.go's
// isJobSucceededShort — to keep this file self-contained without importing
// internal/controller into the kind_integration test package.
func isJobTerminalVerifier(job *batchv1.Job) bool {
	for _, c := range job.Status.Conditions {
		if c.Status == "True" && (c.Type == batchv1.JobComplete || c.Type == batchv1.JobFailed) {
			return true
		}
	}
	return false
}
