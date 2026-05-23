# medium — ~$5 mini Claude (local-only git remote)

**Audience:** Operators trying TIDE with real LLM dispatch at ~$5 cost, with no
external repo dependency.

**Status:** v1.0; local-only `file://` git remote bootstrapped by
`cmd/tide-demo-init/` running as an in-cluster Job (Phase 5 D-B3 /
RESEARCH §"Topic 4 Option b" — D-B3 user decision: no external public
fixture repo for v1).

## Scope of this doc

- What this sample does (clone → plan → execute → push, all local)
- Why it's the middle of the cost spectrum (small $0 → medium $5 → large $25)
- The 7-step apply sequence (the most important section — order matters)
- What you'll observe + cleanup
- Pitfall 9 reminder (namespace prefix collisions)
- Schema-gap notes (file:// targetRepo, outcomePrompt annotation)

## What this does

Applying this sample drives TIDE to:

1. Clone the local-only bare repo at `file:///demo-remote.git` (populated by
   the `demo-remote-init` Job from `examples/tide-demo-fixture/`).
2. Plan a small, bounded outcome: add `FormattedNow() string` alongside the
   existing `Greeting(name)` function, with a unit test that round-trips
   through `time.Parse(time.RFC3339, ...)`.
3. Dispatch the resulting Task DAG via real Claude (`claude-haiku-4-5`,
   cost-disciplined per D-B1).
4. Push the resulting artifacts (PLAN.md + Task diffs) back to the same
   local bare repo on a per-run branch `tide/run-medium-project-<unix-ts>`.

Wall time: roughly **5–10 minutes** from final `kubectl apply` to
`Status=Complete`, dominated by Claude round-trips. The hard $5 cap
(`Project.Spec.budget.absoluteCapCents: 500`) bounds the worst case.

## Why local-only (D-B3)

The medium sample is the proof point that TIDE works end-to-end with real
Claude WITHOUT depending on a publicly-hosted git repo. Operators see the
full clone → plan → execute → push loop run entirely on their own machine:

- No GitHub / GitLab credentials needed for the targetRepo.
- No network round-trips for git ops (the `file://` transport is local).
- Repeatable in air-gapped clusters once the container images are
  pre-pulled.

Per D-B3 the user explicitly rejected hosting a `github.com/jsquirrelz/tide-demo-fixture`
fixture repo for v1 — the in-repo `examples/tide-demo-fixture/` + the
in-cluster bootstrap Job (`cmd/tide-demo-init/`) is the v1 mechanism.

## Prerequisites

- A Kubernetes cluster with TIDE installed (CRDs + controller chart).
  See [`docs/INSTALL.md`](../../../docs/INSTALL.md) for the full install
  recipe.
- `ANTHROPIC_API_KEY` exported in your shell (the secret-create step
  below reads it from the environment).
- A storage class that satisfies `ReadWriteOnce` for the small
  `demo-remote-pvc` (default for most clusters; see
  [`docs/rwx-drivers.md`](../../../docs/rwx-drivers.md) for matrix).
- The `ghcr.io/jsquirrelz/tide-demo-init:v1.0.0` image must be pullable
  by your cluster (or pre-loaded into kind via `kind load docker-image`).

## Setup (in order)

The order matters. The init Job must complete before the Project applies;
otherwise the controller's clone Job will race against the bare repo
not yet existing and fail with `CloneFailed`.

```bash
# 1. Namespace.
kubectl apply -f examples/projects/medium/namespace.yaml

# 2. Small auxiliary PVC for the local-only bare repo.
kubectl apply -f examples/projects/medium/demo-remote-pvc.yaml

# 3. Init Job — runs cmd/tide-demo-init/ to create the bare repo.
kubectl apply -f examples/projects/medium/demo-remote-init-job.yaml

# 4. Wait for the init Job to complete.
kubectl wait --for=condition=Complete job/demo-remote-init \
  -n tide-sample-medium --timeout=2m

# 5. Secret carrying ANTHROPIC_API_KEY (for the planner subagent) and
#    GIT_PAT (placeholder — file:// transport doesn't actually use it,
#    but the schema requires the field).
kubectl create secret generic tide-secrets \
  --from-literal=ANTHROPIC_API_KEY="$ANTHROPIC_API_KEY" \
  --from-literal=GIT_PAT="" \
  -n tide-sample-medium

# 6. Apply the Project CRD. TIDE picks it up and the controller starts
#    the clone Job (mounting the same demo-remote-pvc as the init Job),
#    then plans + dispatches the function-addition outcome.
kubectl apply -f examples/projects/medium/project.yaml

# 7. Wait for completion (bounded by ~15 min — well under the $5 cap's
#    natural duration). Watch via `kubectl get project -w` in another
#    terminal if you prefer.
kubectl wait --for=jsonpath='{.status.phase}'=Complete \
  project/medium-project -n tide-sample-medium --timeout=30m
```

## What you'll observe

- `kubectl get pods -n tide-sample-medium` shows the controller's clone
  Job pod transition Pending → Running → Completed (~30s).
- A planner Job pod follows: `kubectl get jobs -n tide-sample-medium`.
  The planner authors the phase brief + PLAN.md and pushes a boundary
  commit to `tide/run-medium-project-<unix-ts>` on the local-only bare
  repo.
- Per-Task executor Job pods run the function-addition tasks in waves
  (typically 1–2 waves for a 3–5 task plan).
- A push Job stages the final commit (`tide: project complete`) and
  pushes back to the same per-run branch.
- `kubectl get project medium-project -n tide-sample-medium -o jsonpath='{.status.budget.costSpentCents}'`
  shows the running spend (well under 500).

To inspect the artifacts TIDE wrote, exec into a Pod that mounts the same
PVC (or use `kubectl debug` against a temporary one) and `git log
tide/run-medium-project-*`. The cloned working tree under
`/workspace/repo.git` carries the planner's `PLAN.md` and Task diffs.

## Budget cap behavior

`Project.Spec.budget.absoluteCapCents: 500` ($5). If TIDE exceeds the cap
during the run, dispatch halts and `Project.Status.phase=BudgetExceeded`
fires (Phase 2 D-D2 + Phase 04.1 P4.1 rolling-window infra). The cap
firing is a LEGITIMATE outcome — it means the outcome prompt was too
ambitious for the budget; the recovery is to either tighten the prompt
or raise the cap.

`tide approve --bypass-budget` exists as a manual-recovery affordance.
For details and the full set of recovery paths see
[`docs/troubleshooting.md`](../../../docs/troubleshooting.md).

## Cleanup

```bash
kubectl delete namespace tide-sample-medium
```

The namespace deletion cascades to the Project, all child CRDs (Phase,
Plan, Task, Wave), the demo-remote-init Job, the demo-remote-pvc PVC,
and the tide-secrets Secret via owner-refs (where applicable) and
namespace-scoped lifecycle.

## Pitfall 9 reminder

`tide-sample-medium` is distinct from Phase 1's `tide-samples` (plural)
kubebuilder fixture namespace. The two paths serve different audiences
and never share resources. The `tide-sample-` prefix is the Phase 5
sample convention (`tide-sample-small`, `tide-sample-medium`,
`tide-sample-large`).

## Schema-gap notes (v1.0 → v1.x)

Two v1.0 schema gaps to be aware of (documented in
`.planning/phases/05-distribution-self-hosting-acceptance/deferred-items.md`
and carried forward from plan 05-11's large-sample experience):

- **`targetRepo: file:///demo-remote.git`** is the medium sample's
  contract value. The current CEL validator on `ProjectSpec.targetRepo`
  accepts only `http(s)` / `git@` prefixes; the small sample
  (`file:///tmp/no-such-repo`) is in the same boat and ships as-is. The
  v1.x schema work extends the allow-list to include the `file://`
  scheme. Until then, applying this Project CRD on a strict-CEL cluster
  may require an admission bypass (Helm chart's `controllerManager.validatingWebhook.enabled=false`
  for local dev).
- **`outcomePrompt`** is carried as the `tideproject.k8s/outcome-prompt`
  annotation. The planner subagent reads the annotation off the Project
  CRD. v1.x promotes this to a first-class `Project.Spec.OutcomePrompt`
  field; both surfaces will coexist for one minor version.

## Related

- [`examples/projects/README.md`](../README.md) — cost-spectrum overview of all 3 samples
- [`examples/projects/small/README.md`](../small/README.md) — $0 stub-subagent smoke test
- [`examples/projects/large/README.md`](../large/README.md) — $25 v1 acceptance ritual
- [`cmd/tide-demo-init/README.md`](../../../cmd/tide-demo-init/README.md) — the bootstrap binary
- [`examples/tide-demo-fixture/`](../../tide-demo-fixture/) — source-of-truth seed content (MIT-licensed)
- [`docs/project-authoring.md`](../../../docs/project-authoring.md) — Project.Spec field reference
- [Phase 5 CONTEXT.md](../../../.planning/phases/05-distribution-self-hosting-acceptance/05-CONTEXT.md) — D-B1..D-B3 sample decisions
