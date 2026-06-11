# GSD Debug Knowledge Base

Resolved debug sessions. Used by `gsd-debugger` to surface known-pattern hypotheses at the start of new investigations.

---

## wave5-controller-suite-flakes — Dispatch tests flake in full Ginkgo suite due to wrong Project resolved
- **Date:** 2026-05-13
- **Error patterns:** RequeueAfter=0, task should not be dispatched, BudgetExceeded, RateLimitGate, RateLimitStorm, resolveProject, projectList.Items, namespace shared envtest, Ginkgo random order, tideproject.k8s/project label
- **Root cause:** TaskReconciler.resolveProject's namespace-wide List fallback returns projectList.Items[0] when the 'tideproject.k8s/project' label is absent. The test helper makeTask never stamped this label (PlanReconciler does it in production, but dispatch tests bypass PlanReconciler). When other suites create Projects in the shared 'default' namespace, the wrong Project resolves — bypassing the rate-limit gate (wrong Project has no ProviderSecretRef) and the BudgetExceeded gate (wrong Project's Phase is not BudgetExceeded). Latent since plan 02-09 but exposed when Wave-5 plans (02-10, 02-11) added more Projects to the shared namespace.
- **Fix:** makeTask now accepts a variadic projectName parameter and stamps the 'tideproject.k8s/project' label on the Task, matching PlanReconciler.stampTaskLabels in production. All 14 call sites updated. Test-only fix.
- **Files changed:** internal/controller/task_controller_test.go
---

## integration-envtest-9-failures — Phase 2 integration envtest suite: 9 of 18 specs fail
- **Date:** 2026-05-12
- **Error patterns:** no project found in namespace default, resolveProject, BudgetExceeded, init Job, tide-init, Phase=Initialized, SuccessCriteriaMet, completionTime, startTime, GenerationChangedPredicate, AnnotationChangedPredicate, PLAN-03 cycle recovery, recoverCycle, cycleRecover, integration envtest, kick reconcile
- **Root cause:** Three independent test-only bugs in test/integration/envtest/. (1) Indegree/SUB-02/SUB-03 specs created Plans + Tasks but no Project, so TaskReconciler.resolveProject returned "no project found in namespace default" on every reconcile (same shape as wave5-controller-suite-flakes). (2) ART-01 init Job + FAIL-04 budget specs relied on status-only patches to drive subsequent reconciles, but ProjectReconciler's watch uses predicate.Or(GenerationChangedPredicate, AnnotationChangedPredicate) — status updates don't bump metadata.Generation so the predicate filtered them out and the seam body never ran past finalizer-add. Unit tests sidestep this by calling Reconcile() directly. Plus K8s 1.33+ requires SuccessCriteriaMet=True + startTime + completionTime before Complete=True on Job.Status. Plus bypass test asserted a racy terminal phase when the one-shot bypass legitimately re-asserts BudgetExceeded on the next reconcile. (3) PLAN-03 grep test did a raw substring scan and matched the literal verification grep example documented inside plan_webhook.go's own doc comment.
- **Fix:** Test-only. indegree_test.go: BeforeEach creates a default Project + bound PVC, AfterEach cleans them, makeTaskWithWaveLabel stamps tideproject.k8s/project on every Task. budget_test.go: added kickProjectReconcile helper that bumps a benign tide-test/kick annotation; bypass assertion now checks annotation consumption (production one-shot signal). init_test.go: kickProjectReconcile after Project create and after Job status patch; Job status patch matches K8s 1.33+ contract; completion-test resolves Job by tide-init-{UID} deterministic name to avoid stale-job races. admission_test.go: PLAN-03 grep replaced with go/ast walk over identifier names so doc comments don't trip the scan.
- **Files changed:** test/integration/envtest/admission_test.go, test/integration/envtest/budget_test.go, test/integration/envtest/indegree_test.go, test/integration/envtest/init_test.go
---

---
**Closed at v1.0.0 milestone completion (2026-06-11).** The defect class this
session tracked was fixed and validated before ship: full `make test-int`
green (Layer A 36/36 + Layer B), nightly-integration green, live medium DoD
on minikube (Project=Complete, BoundaryPushed=True), and the v1.0.0-rc dry-run
gate green end-to-end.
