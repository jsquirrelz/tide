# TIDE Large Sample ($25 v1 acceptance test)

**Audience:** Maintainers running the $25 v1 acceptance ritual on a dev laptop.

**Status:** v1.0. This sample IS the BOOT-04 acceptance test — TIDE authors the
scaffold for `internal/subagent/openai/` mirroring the existing
`internal/subagent/anthropic/` layering (Phase 3 D-C1), commits artifacts to
a per-run branch, halts at `Complete` or `BudgetExceeded`.

**Scope of this doc:**

- What this sample does (acceptance ritual; NOT a CI gate)
- How to run via `make acceptance-v1` (NOT direct `kubectl apply`)
- The hard $25 cap (D-A2 — no bypass)
- The 7 D-A3 pass criteria
- Evidence capture under `.acceptance-runs/<ts>/`
- Why this is maintainer-only

## What this does

Applying this sample drives TIDE to author the scaffold for
`internal/subagent/openai/` in THIS TIDE repo, mirroring the existing
`internal/subagent/anthropic/` package layout (Phase 3 D-C1 layering pattern).
The Variant B outcome prompt (carried as the
`tideproject.k8s/outcome-prompt` annotation on the Project CRD) constrains
TIDE to a tight scope:

- ONE Phase brief
- ONE PLAN.md
- 5-7 Tasks
- Pass criterion: `go test ./internal/subagent/openai/...` is green and
  `docker build -f internal/subagent/openai/Dockerfile .` builds clean
- Constraint: DO NOT integrate against the real OpenAI API (stub the
  `Subagent.Run()` method — wiring is v1.x scope)

If the acceptance run succeeds, the maintainer has the option to actually
merge the authored skeleton — that's a bonus, not a Phase 5 deliverable.

## Run with

```bash
make acceptance-v1
```

That target (lands in plan 05-15) does the following:

1. Spins a fresh kind cluster (`kind create cluster --name tide-acceptance`).
2. Installs the TIDE charts (`helm install tide-crds ./charts/tide-crds` then
   `helm install tide ./charts/tide`).
3. Creates the `tide-secrets` Secret in the `tide-sample-large` namespace
   carrying `ANTHROPIC_API_KEY` (from your env) and `GIT_PAT` (a GitHub PAT
   with `repo:contents:write` scope).
4. Applies this Project CRD (`kubectl apply -f examples/projects/large/project.yaml`).
5. Watches `Project.Status.phase` and bounds the wait by a 4h ceiling.
6. Captures evidence under `.acceptance-runs/<timestamp>/`.
7. Runs the 7-check verifier (`hack/scripts/acceptance-verify.sh`).

**Prerequisites the make target enforces:**

- `ANTHROPIC_API_KEY` env var set (the target refuses to run otherwise — see
  [docs/INSTALL.md](../../../docs/INSTALL.md) for key setup).
- `GIT_PAT` env var set (GitHub Personal Access Token; same scope as above).
- `kind`, `helm`, `kubectl`, `docker` on PATH.
- Sufficient laptop resources (~8 GB free RAM during the run).

**DO NOT `kubectl apply` this file directly.** The make target sets up the
cluster, secrets, watch, and evidence capture — direct apply would race the
controller bring-up and miss the run-start timestamp the verifier needs.

## Hard $25 cap (D-A2 — no bypass)

`Project.Spec.budget.absoluteCapCents: 2500` ($25). If TIDE exceeds the cap
during the run, dispatch halts and `Project.Status.phase=BudgetExceeded`
fires (Phase 2 D-D2 + Phase 04.1 P4.1 rolling-window infra). The cap firing
**is itself one of the acceptance signals** — proves the budget gate works
in production.

`tide approve --bypass-budget` exists as a manual-recovery affordance, but it
is **NOT** used in the acceptance run. The contract is binary:

- Cap fires → acceptance test fails (BLOCKED) → maintainer inspects.
- Cap doesn't fire → acceptance test continues to other checks.

## 7 D-A3 pass criteria

The `hack/scripts/acceptance-verify.sh` script (lands in plan 05-15)
composes these 7 checks into the `make acceptance-v1` exit code. Pass = all
7 pass = exit 0; any check failing = exit non-zero + halt + maintainer
inspects.

| # | Check                                                                                   | Source                          |
| - | --------------------------------------------------------------------------------------- | ------------------------------- |
| 1 | Per-run branch `tide/run-large-project-<unix-ts>` exists on configured remote           | Phase 3 D-B6 / D-A3 #1          |
| 2 | Per-run branch has commits matching 3 of 4 D-B2 shapes (milestone N/A; Single Phase)    | Phase 3 D-B2 / D-A3 #2          |
| 3 | `Project.Status.phase == Complete`                                                      | D-A3 #3                         |
| 4 | `kubectl logs ... --since=<run-start>` contains zero `ERROR` level lines                | D-A3 #4                         |
| 5 | `kubectl get jobs -l tideproject.k8s/project-uid=<uid>` shows all `status.succeeded=1`  | D-A3 #5 (no orphan Jobs)        |
| 6 | `tide_secret_leak_blocked_total{project=large-project} == 0` (gitleaks passed)          | Phase 4 D-W1 / D-A3 #6          |
| 7 | `Project.Status.budget.costSpentCents < 2500` (under the $25 cap)                       | D-A3 #7                         |

Note on check 2: D-A1 Single Phase scope means there is no Milestone-level
commit shape — the run produces phase, plan, task, and project-complete
shapes (3 of 4). The verifier accepts this.

## Evidence

After every run (pass or fail), `.acceptance-runs/<timestamp>/` captures:

- `controller.log` — `kubectl logs -n tide-system deploy/tide-controller-manager`
- `crds.yaml` — `kubectl get project,phase,plan,task,wave -A -o yaml`
- `run-branch.log` — `git log tide/run-large-project-* --oneline`
- `dashboard.png` — screenshot via Chrome DevTools MCP (per Phase 04.1 P14
  precedent)

Maintainer attaches the transcript and verifier output to the v1.0 release
notes. Acceptance evidence is reusable on every subsequent release — the
ritual runs against each `v1.x` candidate.

## Why maintainer-only (D-A4)

Per D-A4: live LLM cost per PR or nightly is unjustified before OSS adoption
proves there's demand. The maintainer ritual is the v1 stance; CI-gated live
acceptance is a post-v1 decision when (a) demand exists, (b) someone is
funding the API cost, and (c) the run is stable enough to gate releases.

The `make dry-run-v1` target (plan 05-14) provides the CI-runnable proof —
$0 stub-subagent, exercises the install flow end-to-end against the small
sample. The two targets prove different properties; don't conflate them.

## Troubleshooting

**`make acceptance-v1` refuses to start, complaining about
`ANTHROPIC_API_KEY`** — export the env var or source a `.env` file. The
make target's env-gate is a guardrail per Phase 04.1 P2.4 (no LLM run
without explicit operator opt-in).

**Project halts at `BudgetExceeded`** — D-A2 hard cap fired. This is a
LEGITIMATE acceptance signal — TIDE is supposed to halt at $25. Inspect
`Project.Status.budget.costSpentCents` against `absoluteCapCents` to confirm
the gate fired, then inspect `controller.log` for what TIDE was doing when
the cap hit. If TIDE was stuck in a loop, that's a bug; if it ran out of
budget on a legitimately-too-large task, the outcome prompt may need
tightening (Variant C is the next iteration).

**Project halts at `PushLeaseFailed`** — concurrent push race on `main`-adjacent
refs (Phase 3 D-B6). Recovery:

```bash
kubectl annotate project large-project tideproject.k8s/bypass-push-lease=true -n tide-sample-large
```

**Project halts at `PushLeakBlocked`** — gitleaks found a secret in the
artifacts TIDE was about to push. Inspect the diff
(`git log tide/run-large-project-* -p`) and either drop the leaked secret
artifact or rotate the leaked credential.

For the full troubleshooting table, see
[docs/troubleshooting.md](../../../docs/troubleshooting.md) (lands in plan 05-08).

## Related

- [examples/projects/README.md](../README.md) — cost-spectrum overview of all 3 samples
- [examples/projects/small/README.md](../small/README.md) — the $0 smoke test
- [docs/project-authoring.md](../../../docs/project-authoring.md) — Project.Spec field reference
- [Phase 5 CONTEXT.md](../../../.planning/phases/05-distribution-self-hosting-acceptance/05-CONTEXT.md) — D-A1..D-A4 acceptance test decisions
