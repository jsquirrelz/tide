# Phase 52 — Live-Proof Handoff (resume 52-11 Task 2)

**Paste the "PROMPT FOR NEXT SESSION" block below into a fresh session.** Everything it
needs is in this file or the repo. Context ran out mid-live-proof; the cluster is
left UP so you resume from the deployed state, not a rebuild.

---

## STATE AT HANDOFF (2026-07-20)

**Phase 52 code is COMPLETE + committed on `main`, all automated gates green.** 11 plans +
code-review fixes + one live-proof defect fix are landed. The only open item is the
operator-approved **billable live proof (52-11 Task 2)**, which is partially done.

### What the live proof already achieved (the high-value outcome)
It surfaced + fixed a real **SHIP-BLOCKER** the green suites and Phase 51 missed:

- **DEFECT-A — FIXED, committed `8e5f7a49`.** A Locked verification contract at
  Phase/Milestone/Project level (`maxIterations:0`) could never progress: the
  controller's full `Update()` round-trips the spec through Go where
  `maxIterations,omitempty` drops the `0`, so the apiserver saw `oldSelf.maxIterations=0`
  (present) vs `self` absent and the `VerificationSpec` CEL `self == oldSelf` immutability
  rule failed on every reconcile. Fix: dropped `omitempty` + added `+kubebuilder:default=0`
  on `VerificationSpec.MaxIterations` (`api/v1alpha3/task_types.go`) so stored and
  round-tripped forms match. Confirmed live. **The deployed manager pod runs the OLD
  binary and works only via the CRD `default:0`** — a resume must rebuild `controller:test`
  to ship DEFECT-A's Go change into the pod (see step 1).

### The blocker that stopped the proof (fixture-completeness, NOT a product defect)
Driving the full hierarchy to a billable verifier dispatch hit two fixture gaps:
1. The reporter Job needs a **per-namespace `tide-reporter` SA + Role/RoleBinding** (the
   chart only provisions it for chart-configured namespaces; created manually in
   `tide-lv-proof` already — see `kubectl get sa,role,rolebinding -n tide-lv-proof`).
2. A **directly-applied** hierarchy (Project→…→Task, adopted via ownerRefs) does NOT drive
   Plan→Phase→Milestone→Project succession the way a **planner-authored** tree does, so the
   Phase never reaches its pre-Succeeded verify seam. The Phase-51 Task proof never hit this
   because the Task ran independently without needing succession.

**Spend so far: ~1 cent** (budget cap 100000 cents). Only stub planners ran; NO verifier
has billed. The real-model verifier dispatch is the still-unreached step.

## DEPLOYED CLUSTER STATE (left UP)
- Cluster `tide-test`, kube context `kind-tide-test`. All 8 dev-head images loaded.
- Manager: `tide-system/tide-controller-manager`, helm release `tide`,
  `TIDE_VERIFIER_IMAGE=ghcr.io/jsquirrelz/tide-langgraph-verifier:test` patched in. cert-manager v1.20.2 installed. CRDs applied WITH DEFECT-A's `default:0`.
- Fixture ns `tide-lv-proof` (level-verify): Project `tide-lv-proof-project`
  (UID `271370b5-11a8-45be-8ad7-d783c096fb09`, branch
  `tide/run-tide-lv-proof-project-1784557199`, phase Running). Bare repo seeded at
  `/workspace/repo.git` on the PVC. Secrets `tide-signing-key` + `tide-provider-secret`
  (REAL key) present. Reporter SA/RBAC present. Task `Succeeded`; Plan/Phase stuck at no-phase.

## KEY FACTS (do not rediscover)
- **Verifier inherits the TASK-level model** (`ResolveProvider(project,"task")`), makes a
  REAL Anthropic call — use `claude-sonnet-4-6` (stub 404s, haiku spirals). Only the
  verifier bills; hierarchy succession is stub (free).
- **Level-verify provisions its OWN worktree** from the bare repo `/workspace/repo.git`
  (subPath `<projectUID>/workspace`) via the worktree-checkout init container, checking out
  `project.Status.Git.BranchName`. So SEED THE BARE REPO, not the worktree (unlike Phase 51's
  Task proof which hand-seeded `/workspace/worktrees/<uid>`).
- Red gate `test -f VERIFIED.md` (absent in seed) → non-APPROVED → escalation/park. Green
  gate `test -f proof.go` (present in seed) → APPROVED.
- Fixtures: `.planning/phases/52-…/live-proof-level-verify.yaml`,
  `live-proof-bare-repo-seed.yaml`. Phase-51 reference fixtures + the 51-08 runbook are in
  `.planning/phases/51-the-task-loop/`.
- Deploy recipe (if the cluster is gone): `make test-int-kind-prep` (creates cluster + builds/loads
  images — NOTE: background bash gets reaped in this harness, run image loads FOREGROUND) →
  `kubectl apply -f config/crd/bases/` → cert-manager v1.20.2 → `helm upgrade --install`
  with the `helmControllerArgs` `--set` overrides (see `test/integration/kind/suite_test.go`
  `helmControllerArgs`, incl `dashboard.enabled=false`) → `kubectl set env deploy/tide-controller-manager -n tide-system -c manager TIDE_VERIFIER_IMAGE=…verifier:test` → rollout.

---

## PROMPT FOR NEXT SESSION

```
Resume Phase 52's operator-approved billable live proof (52-11 Task 2). The operator
already approved billable spend. Read
.planning/phases/52-per-level-looppolicy-parameterization/52-11-HANDOFF.md and
52-11-SUMMARY.md first — the kind-test cluster is left UP with dev-head + real-key
secrets deployed and DEFECT-A (a CEL ship-blocker) already fixed+committed (8e5f7a49).

Do this, observing runtime state at each step (kubectl + manager logs), and surface+root-fix
any defect you find (the 51-08 precedent — this gate exists to catch what green suites miss):

1. Ship DEFECT-A's Go change into the running pod: rebuild controller:test
   (`make docker-build-test` or the target the chart uses), `kind load docker-image
   controller:test --name tide-test`, rollout-restart deploy/tide-controller-manager.
2. Get the hierarchy to drive Plan→Phase succession so the Phase reaches its pre-Succeeded
   verify seam. The directly-applied fixture doesn't do this — investigate why the Plan
   won't succeed (check the stub project-planner's authored tree, the reporter
   materialization, and the plan/phase controllers' succession for an adopted child). Likely
   cleaner path: apply ONLY a Project (with Project.Spec.Verification.Phase Locked) and let
   the stub planners author a minimal succeeding tree, ensuring each project namespace has a
   tide-reporter SA/RBAC. Whatever the path, keep succession stub-driven (free) until the
   verifier dispatches.
3. Level-verify proof (phase level): once the Phase reaches Verifying, observe
   tide-verifier-phase-<uid>-1 — its worktree-checkout init container checks out the seeded
   run branch, runs the gate; a red gate (test -f VERIFIED.md absent) → non-APPROVED →
   AwaitingApproval park (onExhaustion: requireApproval, maxIterations:0). Then `tide approve`
   → Succeeded with exactly one verifier Job. Then re-seed with VERIFIED.md present (or use a
   green gate) for the APPROVED→Succeeded path.
4. Plan-check proof: a separate fixture Project with Project.Spec.Verification.Plan Locked +
   a real gate command + a deliberately weak first Plan attempt → observe Verifying →
   tide-verifier-plan-<uid>-1 → REPAIRABLE → child-Task deletion → tide-plan-<uid>-2 planner
   Job carrying the findings block → APPROVED or resolved escalation.
5. Confirm ESC-04 rails live: kubectl get jobs -l tideproject.k8s/role=verifier counts stay
   ≤ the concurrency cap (2) throughout.
6. Record kubectl/manager-log proof artifacts + approximate spend in 52-11-SUMMARY.md, flip
   52-HUMAN-UAT.md to resolved, run gsd-sdk query phase.complete 52, commit, and present.

Budget cap is 100000 cents; spend so far ~1 cent. If you find another shipped defect like
DEFECT-A, fix it at the root and commit before continuing. If the cluster is gone, re-deploy
per the recipe in 52-11-HANDOFF.md.
```
