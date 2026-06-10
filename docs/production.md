# Production checklist — before you point TIDE at a repo that matters

TIDE runs real LLM dispatch that **spends money** and **pushes commits to your git
remote**. Read this before your first run against a non-throwaway repository. For field
definitions see [docs/project-authoring.md](project-authoring.md); for install +
namespace bootstrap see [docs/INSTALL.md](INSTALL.md).

> **First real run: use a sandbox.** Point your first real-Claude run at a non-critical
> repo (a fork, a scratch repo) with a small `absoluteCapCents`, watch it in the dashboard,
> and inspect the pushed `tide/run-*` branch before trusting it against anything important.

## What TIDE does to your repo (the safety contract)

- **It pushes to a dedicated run branch, never your default branch.** Each run authors on
  `tide/run-<project>-<unixtime>`. TIDE does **not** commit to or merge `main`/`master`.
- **The push uses `--force-with-lease` scoped to that run branch only** — it is lease-guarded
  (won't clobber a remote that moved under it) and never force-pushes any other ref.
- **The run branch is yours to review and merge.** TIDE opens no PR and merges nothing; the
  branch is the deliverable. Review the diff, then merge (or discard) by hand.
- **The PAT should be push-scoped to the target repo only.** See [docs/git-hosts.md](git-hosts.md)
  for minimal fine-grained token scopes per host (GitHub / GitLab / Gitea).
- **Secret-scan on push.** The push Job runs `gitleaks` over the diff before pushing; override
  rules per-Project via `git.leaksConfigRef` (see project-authoring.md).
- **Transport:** HTTPS + PAT only at v1.0. SSH is deferred to v1.x; `file://` is rejected by
  CEL validation.

## Budget safety (real money)

- **Set a real `budget.absoluteCapCents`.** It is a hard circuit-breaker: when cumulative
  spend exceeds it, `Status.phase=BudgetExceeded` and dispatch halts.
- **`absoluteCapCents: 0` means UNLIMITED, not "off".** The cap is only enforced when `> 0`
  (`internal/budget/cap.go`: "zero cap = unlimited"). Never ship a production Project with
  `0` — a real subagent will spend without bound. The `0` you see in the small sample is safe
  only because the stub-subagent makes no API calls.
- **`budget.rollingWindowCapCents` + `rollingWindowDuration`** add a window-relative cap
  (default window 24h) on top of the lifetime cap — useful for capping daily burn.
- **Cost scales with fan-out and model choice.** Planning fans out wide (one planner per
  milestone/phase/plan); execution fans out per task. Use cheap models for mechanical levels
  and stronger models only where they pay off — set `subagent.levels.{milestone,phase,plan,task}.model`
  (e.g. Sonnet planners, Haiku task execution). Start with a small cap and raise it once you've
  seen a real run's actual spend (`Status.budget.costSpentCents`).

## Cluster sizing

A real run is not just the controller — each level dispatches Jobs (planner → reporter →
clone → subagent executors → push), and a wave runs its tasks **concurrently**. Plan for:

- **Controller + dashboard + cert-manager** steady-state (small).
- **Per-run Job pods**: one planner per level, one reporter per level, a clone Job, N executor
  pods per wave (N = wave width), and a push Job — each pod carries the subagent + a credproxy
  sidecar. Peak pod count tracks your widest wave.
- **A `ReadWriteMany` workspace PVC** — concurrent wave tasks + clone/push mount it at once.
  RWO only works for serialized single-mount kind/minikube testing. See
  [docs/rwx-drivers.md](rwx-drivers.md).

The bundled samples run a 3-task toy on ~4 vCPU / 6 GB (kind/minikube). A real multi-wave
project needs headroom proportional to wave width — size the node pool for `controller +
dashboard + (widest wave × executor pod)` concurrently, and provision an RWX StorageClass.

## Per-task wall-clock caps (long tasks)

Caps-unset executor Jobs get a wall-clock floor (480s + 60s grace = 540s `activeDeadlineSeconds`).
A task whose real-LLM tool loop runs longer is killed with `DeadlineExceeded` before it writes
its result — surfacing as `EnvelopeReadFailed` / a failed task. For long-running real tasks set
an explicit per-Task `caps.WallClockSeconds` (and budget) above the expected runtime.

## Human gates for a first real run

Set per-level gate policy (`gates.{milestone,phase,plan,task}` = `auto | approve | pause`).
For a first production run, gate at least the **milestone** boundary (`approve`) so you review
the plan before it spends and pushes — drive it with `tide approve` (see [docs/gates.md](gates.md)
and [docs/cli.md](cli.md)). Loosen gates once you trust the pipeline.

## Known limitations at v1.0

- **Git transport:** HTTPS + PAT only. SSH deferred to v1.x; `file://` unsupported (CEL-rejected).
- **Provider:** the `Subagent` interface is pluggable, but v1.0 ships one concrete impl
  (Claude-backed). No OpenAI/other concrete subagent in-tree yet.
- **Workspace storage:** concurrency requires an RWX StorageClass.
- **One namespace per Project** is the supported multi-tenant pattern; cross-namespace Secret
  refs are not permitted.

## Pre-flight checklist

- [ ] cert-manager installed; `tide-crds` then `tide` charts installed ([INSTALL.md](INSTALL.md)).
- [ ] Project namespace bootstrapped (PVC + SAs + RBAC + signing key) — [INSTALL.md §Bootstrapping a Project namespace](INSTALL.md#bootstrapping-a-project-namespace).
- [ ] RWX StorageClass available for the `tide-projects` PVC.
- [ ] `ANTHROPIC_API_KEY` Secret in the Project namespace, referenced by `providerSecretRef`.
- [ ] Push-scoped `GIT_PAT` Secret, referenced by `git.credsSecretRef`.
- [ ] `subagent.image` = the claude-subagent image (or rely on the chart's claude fallback — **not** the stub) and per-level models set.
- [ ] `budget.absoluteCapCents` set to a real, non-zero value.
- [ ] `targetRepo` is a sandbox/non-critical repo for the first run.
- [ ] Milestone gate set to `approve` for the first run; dashboard open to watch.

## See also

- [docs/project-authoring.md](project-authoring.md) — every `Project.Spec` field + the medium/large worked examples.
- [docs/gates.md](gates.md) · [docs/cli.md](cli.md) — gate policy + `tide approve/resume/cancel`.
- [docs/git-hosts.md](git-hosts.md) — per-host PAT scopes + the run-branch smoke recipe.
- [docs/observability.md](observability.md) — Prometheus + OTel for watching a real run.
- [docs/troubleshooting.md](troubleshooting.md) — symptom/cause/recipe table.
