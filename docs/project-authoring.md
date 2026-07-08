# Authoring a TIDE Project

**Audience:** Operators writing their first `project.yaml`.

**Status:** v1.0; covers `Project.Spec` at the `tideproject.k8s/v1alpha1` schema lock
(Phase 1 D-A3; the API group + version are stable across the M0 → M_self bridge).

**Scope of this doc:**

- The `Project` CRD's `Project.Spec` field reference (the authoritative shape lives in
  [`api/v1alpha1/project_types.go`](../api/v1alpha1/project_types.go)).
- A three-sample walkthrough — **small ($0 stub)** → **medium (~$5 mini Claude)** →
  **large (~$25 acceptance)** — with the cost spectrum as the discriminator (per
  Phase 5 D-B1).
- Outcome-prompt authoring guidance: Variant A (over-prescriptive), Variant B
  (recommended), Variant C (under-specified) tradeoffs.
- Provider configuration (LLM credentials, per-level model selection) and how to
  point the controller at the right Secret.
- Forward links to [`docs/cli.md`](cli.md), [`docs/gates.md`](gates.md), and
  [`docs/troubleshooting.md`](troubleshooting.md) for next-step operator work.

A `Project` is the **outcome unit** that TIDE drives — the operator declares the
goal (`outcomePrompt`), the target repo (`targetRepo`), credentials
(`secretRefs`), and policy (`budget`, `gates`); TIDE dispatches a `MILESTONE.md`
planner, then phase planners, then plan planners, then task executors against the
DAG. Every level boundary is a Markdown artifact pushed to a per-run git branch.
See [`README.md`](../README.md) for the five-level paradigm and the two-DAG framing.

---

## Project.Spec field reference

Field shapes are sourced verbatim from [`api/v1alpha1/project_types.go`](../api/v1alpha1/project_types.go).
When a field comment in the type definition disagrees with this table, the type
file wins — file a doc-drift issue.

### Top-level `Project.Spec` fields

| Field                       | Type                  | Required | Default                       | What it controls                                                                                                                                                                                |
| --------------------------- | --------------------- | -------- | ----------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `targetRepo`                | `string` (URL)        | yes      | (none)                        | The URL of the repository TIDE plans + executes against. Must start with `http`, `https`, or `git@` (CEL-validated). Used by the in-cluster clone Job to populate the workspace PVC.            |
| `secretRefs.anthropicAPIKey` | `string` (Secret name) | optional | (none)                        | Name of the same-namespace `Secret` carrying `ANTHROPIC_API_KEY`. Read by the credproxy sidecar — the controller pod itself never sees the key (Phase 2 D-C1..C4).                              |
| `secretRefs.gitCredentials` | `string` (Secret name) | optional | (none)                        | Name of the same-namespace `Secret` carrying `GIT_PAT` for HTTPS+PAT push. Mounted as `envFrom` on the push Job pod; never read by the controller.                                              |
| `providerSecretRef`         | `string` (Secret name) | optional | (none)                        | Convenience alias for `secretRefs.anthropicAPIKey` (Phase 2+ credproxy contract). Set this if you prefer one Secret name per provider rather than the per-axis `secretRefs` block.              |
| `subagent.image`            | `string` (image ref)  | optional | Helm chart default            | Default subagent container image for all four levels. Picks the **vendor** (Claude, OpenAI stub, etc.) via the bundled `Subagent` impl. Example: `ghcr.io/jsquirrelz/tide-claude-subagent:v0.1.0`. |
| `subagent.model`            | `string`              | optional | Helm chart default            | Default model identifier passed to the vendor's CLI/SDK. Example: `claude-sonnet-4-6` (anthropic) or `o1-mini` (openai). Vendor + model are orthogonal axes (Phase 3 D-C2).                      |
| `subagent.levels.{milestone,phase,plan,task}.model` | `string` | optional | falls back to `subagent.model` | Per-level model override. Use a cheap model (e.g., `claude-haiku-4-5`) for the milestone planner and a stronger model (e.g., `claude-sonnet-4-6`) for plan/task levels.                          |
| `git.repoURL`               | `string` (URL)        | optional | (none)                        | The per-Project push target. CEL pattern requires `http(s)://`. Required for any Project whose lifecycle reaches push; optional for purely transient/test Projects (Phase 3 D-B6).              |
| `git.credsSecretRef`        | `string` (Secret name) | optional | (none)                        | Same-namespace Secret carrying `GIT_PAT`. Cross-namespace refs are NOT permitted in v1.0. Read only by the push Job (`tide-push` ServiceAccount), never by the controller.                       |
| `git.leaksConfigRef`        | `string` (ConfigMap)   | optional | embedded gitleaks defaults    | ConfigMap with gitleaks rule overrides per-Project. When empty, the push image's embedded ruleset applies.                                                                                       |
| `git.baseRef`               | `string` (ref)         | optional | remote default branch (`HEAD`) | The ref the per-run branch is created from — a branch, tag, full 40-hex SHA, or `refs/`-qualified path. Resolution order is `refs/`-verbatim → branch → tag → full SHA (NOT git revision syntax); `HEAD`, short SHAs, and `~`/`^` suffixes are rejected. Absent = the remote default branch. An unresolvable value halts the clone with `CloneFailed`/`BaseRefUnresolvable`; edit the field to re-attempt. See [Basing a run on a branch, tag, or SHA](#basing-a-run-on-a-branch-tag-or-sha). |
| `git.agentName`             | `string`              | optional | (none → compiled-in `TIDE Agent`) | Commit author/committer **name** stamped at all three TIDE commit sites — harness task commits, integrate merges, boundary pushes. Precedence: this field → chart `agent.name` → compiled-in default. Admission rejects angle brackets and newlines (`^[^<>\r\n]+$`, max 100 chars). |
| `git.agentEmail`            | `string`              | optional | (none → compiled-in `tide-agent@tideproject.k8s`) | Commit author/committer **email**, same three sites and precedence (→ chart `agent.email` → compiled-in default). Must be a bare `local@domain` shape — angle brackets, `@`-in-parts, and whitespace are rejected at admission (max 254 chars). |
| `budget.absoluteCapCents`   | `int64` (USD cents)   | yes (set a real value for production) | `0` = **cap DISABLED / unlimited spend** (NOT a hard stop) | Hard lifetime cap on LLM spend in USD cents. `2500` = $25. **The cap is only enforced when `> 0`** (`internal/budget/cap.go`: "zero cap = unlimited") — `0` means UNLIMITED, so a real Project with a real subagent and `absoluteCapCents: 0` can spend without bound. Always set a real cap in production. When a non-zero cap is exceeded, `Status.phase=BudgetExceeded` fires and dispatch halts (Phase 2 D-D2 + Phase 04.1 P4.1).                              |
| `budget.rollingWindowCapCents` | `int64` (USD cents) | optional | (no rolling window)           | Caps spending over the rolling window defined by `budget.rollingWindowDuration`. Window resets via `ProjectReconciler.handleBudgetGate` when `BudgetStatus.WindowStart + duration` elapses.       |
| `budget.rollingWindowDuration` | `metav1.Duration`  | optional | `24h`                         | Window length over which `rollingWindowCapCents` applies. Must be ≥ 1h (semantic check; controller-gen Pattern markers can't enforce on struct-typed fields). Set explicitly to override default. |
| `gates.milestone`           | `auto \| approve \| pause` | optional | `auto`                       | Per-level gate policy at the milestone boundary. `approve` halts until `tide approve` annotation lands; `pause` halts until `tide resume` (Phase 4 D-G1).                                          |
| `gates.phase`               | `auto \| approve \| pause` | optional | `auto`                       | Per-level gate policy at the phase boundary. Same semantics as `gates.milestone`.                                                                                                                |
| `gates.plan`                | `auto \| approve \| pause` | optional | `auto`                       | Per-level gate policy at the plan boundary. Use for plans that need extra human scrutiny.                                                                                                        |
| `gates.task`                | `auto \| approve \| pause` | optional | `auto`                       | Per-level gate policy at the task boundary. Rare — most operators use `auto` and gate at higher levels.                                                                                          |
| `gates.pauseBetweenWaves`   | `bool`                | optional | `false`                       | When `true`, the plan reconciler halts between consecutive waves until `tide resume` annotates the Project. Useful for human inspection of wave-N artifacts before dispatching wave-N+1.         |
| `providers[].name`          | `string` (enum)       | yes (within `providers`) | (none)               | Provider identifier. Only `anthropic` is supported in v1.0; the multi-vendor matrix is v1.x scope (the acceptance test authors the `internal/subagent/openai/` skeleton; it doesn't ship as a sample). |
| `providers[].requestsPerMinute` | `*int32`          | optional | (no cap)                      | Optional per-provider rate limit. Saturating this triggers backoff in the dispatcher; the eventual `Task.Status` reason is `RateLimitHit`.                                                       |
| `providers[].tokensPerMinute` | `*int32`            | optional | (no cap)                      | Optional per-provider TPM cap; same semantics as `requestsPerMinute`.                                                                                                                            |
| `providers[].allowedRoutes` | `[]RouteSpec` (`{method, pathPrefix}`) | optional | hardcoded `POST /v1/messages` + `POST /v1/messages/count_tokens` | Additive (not restrictive) extensions to the credproxy upstream allowlist. Use for newly-released LLM endpoints (Files API, prompt caching) without rebuilding credproxy.                       |
| `planAdmission.fileTouchMode` | `strict \| warn`    | optional | `warn`                        | How file-touch mismatches between a Plan's declared `<files>` and its executed diff are handled. `strict` rejects plans at admission; `warn` admits + annotates `Plan.Status` (Phase 2 D-E1..E4). |
| `maxAttemptsPerTask`        | `int32` (1..10)       | optional | `3` (Helm chart default)      | Number of dispatch attempts per Task before it's marked failed. Bounded by CEL to `[1, 10]` to prevent runaway retries.                                                                          |
| `modelSelection.{milestone,phase,plan,task}` | `string` | optional | falls back to `subagent.*`   | Legacy per-level model selection (kept for Phase 1 fixtures). Prefer `subagent.levels.*` in new Projects.                                                                                        |

> **`outcomePrompt` note.** The user-facing outcome prompt is conventionally
> embedded in a project-authoring annotation or a separate `ConfigMap` referenced
> by the planner Job's env in v1.0 — the `Project.Spec` itself does not currently
> carry an `outcomePrompt` field. The medium and large sample walkthroughs below
> show the recommended shape (annotation + ConfigMap). Field-on-spec promotion
> is a v1.x consideration.

### Status fields (read-only)

`Project.Status` is populated by the reconcilers; you never set these. The
operator-visible columns surfaced by `kubectl get project` are `Phase`,
`Status[Ready]`, and `Age`.

| Status field                        | What it means                                                                                                                  |
| ----------------------------------- | ------------------------------------------------------------------------------------------------------------------------------ |
| `phase`                             | High-level lifecycle label. One of: `Pending`, `Initialized`, `InitFailed`, `BudgetExceeded`, `Running`, `PushLeaseFailed`, `PushLeakBlocked`, `Complete`. |
| `conditions[]`                      | Standard K8s `metav1.Condition` array. Reconcilers set `Ready`, `Reconciling`, `Validated`, `WaveOrLevelPaused`, etc.          |
| `budget.tokensSpent`                | Cumulative tokens spent since `windowStart`. Reset by the rolling-window-reset logic (Phase 04.1 P4.1).                         |
| `budget.costSpentCents`             | Cumulative cost in USD cents since `windowStart`. Compared against `Spec.budget.{absoluteCapCents,rollingWindowCapCents}`.      |
| `budget.windowStart`                | Beginning of the current rolling budget window.                                                                                |
| `git.branchName`                    | Lifetime per-run branch (`tide/run-<project>-<unix-epoch>`). Fixed at Project creation; never changes for the lifetime.        |
| `git.baseSHA`                       | The commit SHA the run branch was created from — stamped on **every** run, including default-`HEAD` runs (no `baseRef` set). Reproducibility provenance: the ref can move after the run starts, so `baseSHA` pins the exact commit. Annotated tags stamp the peeled commit. |
| `git.lastPushedSHA`                 | Head SHA recorded on the last successful push; used as the `--force-with-lease` lease for the next push.                       |
| `git.leaseFailureCount`             | Consecutive push-lease rejections. Resets to 0 on success; trips `PhasePushLeaseFailed` when it exceeds the configured budget. |

---

## 3-sample walkthrough

TIDE ships three sample Projects under [`examples/projects/`](../examples/projects/),
discriminated by **cost**:

| Sample | Cost     | Purpose                                                                                       | API key needed? |
| ------ | -------- | --------------------------------------------------------------------------------------------- | --------------- |
| small  | $0       | Stub-subagent smoke test against any cluster. DIST-05 dry-run target.                          | No              |
| medium | ~$5      | Real Claude run against a local-only git remote scaffolded from `examples/tide-demo-fixture/`. | Yes (Anthropic) |
| large  | ~$25     | v1.0 acceptance test against THIS TIDE repo; authors the `internal/subagent/openai/` scaffold. | Yes (Anthropic) |

Phase 5 D-B1 frames cost as the discriminator: new operators care most about
"what will my first TIDE run charge me?" Once cost is settled, the three samples
shake out the dispatch path at progressively realistic settings.

### small — $0 stub-subagent

The small sample uses the **stub-subagent** (`ghcr.io/jsquirrelz/tide-stub-subagent`) —
it never calls a real LLM endpoint; the binary returns canned `EnvelopeOut`
payloads from in-image fixtures. This is the **DIST-05 dry-run target**: the
`make dry-run-v1` ritual on `v*-rc.*` tags applies this sample and times to
`Project.Status.phase == Complete`.

The fixture content matches the Phase 1 α…θ Task DAG (the
worked example from `pkg/dag`'s test fixtures, re-published as operator-visible
sample content under `examples/projects/small/`).

**File layout** (lands in Plan 05-11):

```text
examples/projects/small/
├── README.md            # apply-and-verify recipe
├── namespace.yaml       # tide-sample-small namespace
└── project.yaml         # Project CRD; budget cap 0 cents; subagent stub
```

**project.yaml shape:**

```yaml
apiVersion: tideproject.k8s/v1alpha1
kind: Project
metadata:
  name: small
  namespace: tide-sample-small
spec:
  # Stub-subagent runs against any URL — pattern validator just needs http(s).
  targetRepo: "https://example.invalid/no-real-clone-needed.git"

  subagent:
    image: ghcr.io/jsquirrelz/tide-stub-subagent:v1.0.0
    model: stub                            # ignored by stub image, present for shape

  budget:
    absoluteCapCents: 0                    # explicit zero — stub doesn't spend
    rollingWindowCapCents: 0
    rollingWindowDuration: 24h             # explicit; Phase 04.1 P4.1

  gates:
    milestone: auto
    phase: auto
    plan: auto
    task: auto

  maxAttemptsPerTask: 1                    # stub envelopes are deterministic
```

**Apply + verify:**

```bash
kubectl apply -f examples/projects/small/namespace.yaml
kubectl apply -f examples/projects/small/project.yaml

# Watch the Project advance: Pending → Initialized → Running → Complete.
kubectl get project -n tide-sample-small -w

# Status check.
kubectl wait --for=jsonpath='{.status.phase}'=Complete \
  project/small -n tide-sample-small --timeout=10m
```

Expected `Project.Status.phase` progression: `Pending` → `Initialized` →
`Running` → `Complete`. Total wall-clock < 2 minutes on a stock kind cluster.
No API key required, no external network calls.

### medium — ~$5 mini Claude (local-only git remote)

The medium sample uses a **real Claude image** (`tide-claude-subagent`) with
`claude-haiku-4-5` as the per-level model — bounded under $5 by the
`absoluteCapCents: 500` cap. The novel mechanic: the medium sample targets a
**local-only git remote** scaffolded from [`examples/tide-demo-fixture/`](../examples/tide-demo-fixture/)
(a tiny MIT-licensed Go scaffold: 1 README + 1 Go file + 1 unit test). No
external public repo dependency.

The local remote is bootstrapped by [`cmd/tide-demo-init`](../cmd/tide-demo-init/) —
a tiny Go binary published as `ghcr.io/jsquirrelz/tide-demo-init:v1.0.0`. The
binary uses `//go:embed` to bake the fixture content into the image, then on
startup runs `git init --bare` on a PVC subpath and seeds it with an initial
commit derived from the embedded fixture. The Project's `targetRepo` then
references the in-cluster `file:///demo-remote.git` URL on that PVC.

Phase 5 D-B3 + RESEARCH Topic 4 capture the design tradeoffs. Operators run
the medium sample fully offline once the `tide-demo-init` image is pulled.

**File layout** (lands in Plan 05-12 alongside `cmd/tide-demo-init`):

```text
examples/projects/medium/
├── README.md                       # apply order + cost expectations
├── namespace.yaml                  # tide-sample-medium namespace
├── demo-remote-pvc.yaml            # ReadWriteOnce PVC for the bare repo
├── demo-remote-init-job.yaml       # Job running ghcr.io/jsquirrelz/tide-demo-init
└── project.yaml                    # Project CRD; budget cap 500 cents
```

**project.yaml shape:**

```yaml
apiVersion: tideproject.k8s/v1alpha1
kind: Project
metadata:
  name: medium
  namespace: tide-sample-medium
spec:
  # The bare repo lives on demo-remote-pvc, mounted by the clone Job. The
  # path matches the subPath the demo-remote-init Job wrote to.
  targetRepo: "file:///demo-remote.git"

  secretRefs:
    anthropicAPIKey: tide-secrets        # Secret holding ANTHROPIC_API_KEY
    gitCredentials: tide-secrets         # file:// remote ignores GIT_PAT,
                                         # but the field must resolve.

  providerSecretRef: tide-secrets        # same Secret, credproxy contract

  git:
    repoURL: "file:///demo-remote.git"   # same shape; push round-trips locally
    credsSecretRef: tide-secrets

  subagent:
    image: ghcr.io/jsquirrelz/tide-claude-subagent:v1.0.0
    model: claude-haiku-4-5              # cheap, fast — bounds spend
    levels:
      milestone:
        model: claude-haiku-4-5
      phase:
        model: claude-haiku-4-5
      plan:
        model: claude-haiku-4-5
      task:
        model: claude-haiku-4-5

  budget:
    absoluteCapCents: 500                # $5 hard cap
    rollingWindowCapCents: 500
    rollingWindowDuration: 24h           # Phase 04.1 P4.1

  gates:
    milestone: auto
    phase: auto
    plan: auto
    task: auto
```

**Apply sequence — order matters:**

```bash
# 1. Create the namespace and the API key Secret. Replace <key> with your
#    Anthropic API key — never commit it to the repo.
kubectl apply -f examples/projects/medium/namespace.yaml
kubectl create secret generic tide-secrets \
  -n tide-sample-medium \
  --from-literal=ANTHROPIC_API_KEY=<key> \
  --from-literal=GIT_PAT=unused-for-file-remote

# 2. Create the bare-repo PVC.
kubectl apply -f examples/projects/medium/demo-remote-pvc.yaml

# 3. Run the demo-remote-init Job; this populates the bare repo from the
#    embedded fixture. Wait for completion before applying the Project.
kubectl apply -f examples/projects/medium/demo-remote-init-job.yaml
kubectl wait --for=condition=Complete \
  job/demo-remote-init -n tide-sample-medium --timeout=2m

# 4. Apply the Project. The controller's clone Job mounts demo-remote-pvc
#    and reads from file:///demo-remote.git on the shared subPath.
kubectl apply -f examples/projects/medium/project.yaml

# 5. Watch dispatch progress. Expect ~5-10 minutes of wall-clock on the
#    happy path with claude-haiku-4-5 at every level.
kubectl get project -n tide-sample-medium -w
```

Expected `Project.Status.budget.costSpentCents` at completion: ~200-450 cents
($2-$4.50) for a typical run. If the cap fires (`Status.phase=BudgetExceeded`),
inspect the controller logs and the per-level model selection — `claude-opus`
at every level will blow $5 quickly; the sample defaults to `claude-haiku-4-5`
on purpose.

### large — ~$25 acceptance test

The large sample IS the **v1 acceptance test** (Phase 5 D-A1 + D-B4). It targets
THIS TIDE repo, has a hard $25 cap (`absoluteCapCents: 2500`) with **no
bypass**, and drives TIDE to author the scaffold for `internal/subagent/openai/`
(mirroring the Phase 3 D-C1 layering pattern). Single Phase scope per D-A1 — the
acceptance signal is "full descent works once, repeatably, under cap," not
"full Milestone-level fan-out."

The maintainer ritual is [`make acceptance-v1`](../Makefile) (lands in Plan 05-15).
There is no CI integration for this sample — live LLM spend on every PR/nightly
is unjustified before OSS adoption proves there's demand. The cap halt (Phase 04.1
P4.1) is itself one of the acceptance signals — if cost climbs past $25, the
test FAILS by design (acceptance criteria #7 from D-A3).

**File layout** (lands in Plan 05-11):

```text
examples/projects/large/
├── README.md                       # maintainer ritual instructions
├── namespace.yaml                  # tide-sample-large namespace
└── project.yaml                    # Project CRD; budget cap 2500 cents
```

**project.yaml shape** (the `outcomePrompt` here is canonical Variant B from
RESEARCH §"Topic 5"; copied verbatim because the prompt phrasing is itself the
acceptance contract):

```yaml
apiVersion: tideproject.k8s/v1alpha1
kind: Project
metadata:
  name: large-project
  namespace: tide-sample-large
  # The outcomePrompt is carried as an annotation in v1.0 (field-on-spec
  # promotion is v1.x scope). The planner Job reads this annotation at
  # dispatch time and forwards it as the Subagent.Run() outcome.
  annotations:
    tideproject.k8s/outcome-prompt: |
      Author the scaffold for `internal/subagent/openai/` in this repository,
      mirroring the existing `internal/subagent/anthropic/` layout.

      Concrete deliverables (tight scope — target 5-7 tasks, ONE Plan, ONE Phase):

      1. `internal/subagent/openai/client.go` — defines `Client` struct +
         constructor. Match the shape of `internal/subagent/anthropic/client.go`.
         DO NOT call the real OpenAI API; the constructor takes an API key
         string but the methods are stubbed.

      2. `internal/subagent/openai/run.go` — defines
         `Subagent.Run(ctx, EnvelopeIn) (EnvelopeOut, error)` matching
         `pkg/dispatch.Subagent`. STUB implementation: return a canned
         `EnvelopeOut{Status: "success", Artifacts: []}` envelope. Add a TODO
         comment explaining real-API integration is v1.x scope.

      3. `internal/subagent/openai/Dockerfile` — multi-stage build mirroring
         `internal/subagent/anthropic/Dockerfile`. Final image must build clean.

      4. `internal/subagent/openai/run_test.go` — ONE unit test verifying the
         stub returns canned envelope and matches the `Subagent` interface.

      5. `internal/subagent/openai/doc.go` — package doc comment referencing
         Phase 3 D-C1 layering pattern.

      Constraints:
      - DO NOT modify any files outside `internal/subagent/openai/`.
      - DO NOT add the openai package to `cmd/manager`'s build (the contract
        is authoring the scaffold, not wiring it; wiring is v1.x scope).
      - Follow the existing repo conventions in CLAUDE.md (Apache-2.0 headers,
        logging discipline, error handling).

      Pass criterion: `go test ./internal/subagent/openai/...` is green;
      `docker build -f internal/subagent/openai/Dockerfile .` builds without
      error.
spec:
  targetRepo: "https://github.com/jsquirrelz/tide.git"

  secretRefs:
    anthropicAPIKey: tide-secrets
    gitCredentials: tide-secrets

  providerSecretRef: tide-secrets

  git:
    repoURL: "https://github.com/jsquirrelz/tide.git"
    credsSecretRef: tide-secrets

  subagent:
    image: ghcr.io/jsquirrelz/tide-claude-subagent:v1.0.0
    model: claude-sonnet-4-6              # planner needs sonnet to bound task count
    levels:
      milestone:
        model: claude-haiku-4-5           # N/A for Single Phase scope; cheap default
      phase:
        model: claude-sonnet-4-6
      plan:
        model: claude-sonnet-4-6
      task:
        model: claude-sonnet-4-6

  budget:
    absoluteCapCents: 2500                # $25 HARD cap — D-A2, no bypass
    rollingWindowCapCents: 2500
    rollingWindowDuration: 24h            # Phase 04.1 P4.1 — explicit

  gates:
    milestone: auto                       # No human gates — D-A1 self-contained
    phase: auto
    plan: auto
    task: auto
    pauseBetweenWaves: false

  planAdmission:
    fileTouchMode: strict                 # acceptance test wants tight scope; warn would mask drift

  maxAttemptsPerTask: 3
```

**Apply sequence** (maintainer-only, runs from `make acceptance-v1`):

```bash
# 0. Fresh kind cluster + helm install (the Make target wraps this).
kind create cluster --name tide-acceptance
helm install tide-crds ./charts/tide-crds -n tide-system --create-namespace
helm install tide ./charts/tide -n tide-system

# 1. Create the namespace + Secret holding ANTHROPIC_API_KEY + GIT_PAT.
kubectl apply -f examples/projects/large/namespace.yaml
kubectl create secret generic tide-secrets \
  -n tide-sample-large \
  --from-literal=ANTHROPIC_API_KEY=<your-real-key> \
  --from-literal=GIT_PAT=<your-github-pat>

# 2. Apply the Project. The clone Job pulls this repo to the workspace PVC;
#    the milestone planner reads the outcome-prompt annotation and dispatches.
kubectl apply -f examples/projects/large/project.yaml

# 3. Watch the seven D-A3 acceptance signals via hack/scripts/acceptance-verify.sh
#    (lands in Plan 05-15). Or watch directly:
kubectl get project -n tide-sample-large -w
kubectl logs -n tide-system deploy/tide-controller-manager -f
```

**The seven D-A3 acceptance signals** (`hack/scripts/acceptance-verify.sh`
exit-0 = all pass):

1. Per-run branch `tide/run-large-project-<unix-ts>` exists on the configured
   remote (Phase 3 D-B6).
2. Branch carries the 4 D-B2 commit-message shapes (`tide: plan ... authored`
   + `tide: plan ... executed`, `tide: phase ... authored`, `tide: milestone —
   N/A for Single Phase scope`, `tide: project complete`).
3. `Project.Status.phase == Complete`.
4. `kubectl logs ... --since=<run-start>` contains zero `ERROR` lines.
5. `kubectl get jobs -l tideproject.k8s/project-uid=<uid>` shows all Jobs at
   `status.succeeded=1` (no orphans).
6. `tide_secret_leak_blocked_total{project=large-project} == 0` (gitleaks
   passed).
7. `Project.Status.budget.costSpentCents < 2500` (under the cap).

Any one check failing = `make acceptance-v1` exits non-zero = BLOCKED; the
maintainer inspects evidence under `.acceptance-runs/<timestamp>/`.

---

## Outcome-prompt authoring guidance

The acceptance test exposed three outcome-prompt shapes. The phrasing matters
because the LLM at every level reads it (the milestone planner expands it into
`MILESTONE.md`; the phase planner ingests `MILESTONE.md` + the prompt; the plan
planner ingests both + the phase brief; etc.). Too vague and TIDE wanders; too
prescriptive and TIDE becomes a typing machine.

Phase 5 D-B4 + RESEARCH §"Topic 5" prototype three variants for the
`internal/subagent/openai/` authoring task.

### Variant A — over-prescriptive (avoid)

> Create `internal/subagent/openai/run.go` with the function
> `func (c *Client) Run(ctx context.Context, in EnvelopeIn) (EnvelopeOut, error) { return EnvelopeOut{Status: "success"}, nil }`.

**Why this is bad:** the operator has hand-authored the plan; TIDE's
contribution becomes mechanical typing. The acceptance signal degrades from
"did TIDE plan + execute correctly" to "did the file get created." Tasks
collapse into single-file edits with no DAG fan-out; dispatch coverage drops to
zero.

### Variant B — recommended

> Author the scaffold for `internal/subagent/openai/` mirroring the existing
> `internal/subagent/anthropic/` layout. Concrete deliverables (tight scope —
> target 5-7 tasks, ONE Plan, ONE Phase): 1. client.go ... 2. run.go ... [etc.]
> Constraints: DO NOT modify any files outside `internal/subagent/openai/`. Pass
> criterion: `go test ./internal/subagent/openai/...` is green.

**Why this works:** concrete file list bounds the task DAG (5-7 tasks is the
sweet spot for full descent without budget pressure); scope constraint
("DO NOT modify any files outside ...") prevents wander; pass criterion
(`go test` green) is machine-checkable. The shape mirrors a real PR — a senior
maintainer reviewing a new contributor's first task would write this prompt
verbatim.

The large sample's project.yaml embeds Variant B verbatim. The full text is
above in the large-sample walkthrough.

### Variant C — under-specified (avoid)

> Add OpenAI provider support to TIDE.

**Why this is bad:** TIDE will plan a multi-phase, multi-plan integration —
fan out 20+ tasks (real-API client + retry logic + e2e test + chart wiring +
docs + migration guide), hit the $25 cap, fail acceptance. The empirical
lesson from Phase 02.2's cascade-5 + cascade-8 sessions applies: vague prompts
blow budgets; cascade-debugging adds 10+ wasted iterations.

### Authoring checklist

When writing a new outcome prompt, check:

- [ ] **File list is concrete.** "Author X, Y, Z" beats "Add support for foo."
- [ ] **Scope constraint is explicit.** "DO NOT modify files outside `<dir>/`."
- [ ] **Pass criterion is machine-checkable.** `make test`, `go test ./...`,
      `docker build`, or a `kubectl wait --for=condition=...`.
- [ ] **Task count is bounded.** "Target N tasks, ONE Plan, ONE Phase" gives
      the planner an upper bound. Combined with the budget cap, this prevents
      runaway fan-out.
- [ ] **Constraints reference existing repo conventions.** "Follow the
      conventions in CLAUDE.md" + "Match the shape of `<existing-package>/`"
      anchors the LLM in real precedent.

---

## Provider configuration

### LLM credentials (Anthropic key)

Create a Kubernetes Secret in the same namespace as your Project. The credproxy
sidecar reads the `ANTHROPIC_API_KEY` data key directly; the controller pod
never reads it.

```bash
kubectl create secret generic tide-secrets \
  -n <project-namespace> \
  --from-literal=ANTHROPIC_API_KEY=sk-ant-api03-... \
  --from-literal=GIT_PAT=ghp_...
```

Reference the Secret name from `Project.Spec.secretRefs.anthropicAPIKey` and/or
`Project.Spec.providerSecretRef`. The two fields can point at the same Secret
(common) or different Secrets per axis.

> **Threat model note:** Cross-namespace Secret refs are NOT permitted in v1.0
> per the `T-304` mitigation. Each Project's Secrets live in its own namespace;
> the dedicated `tide-push` ServiceAccount has `secrets get` only on the
> push-Job namespace's Secrets. See [`docs/rbac.md`](rbac.md) for the per-Kind
> RBAC matrix.

### Per-level model selection

`Project.Spec.subagent.model` sets the default model for all four levels.
Override per-level via `Project.Spec.subagent.levels.{milestone,phase,plan,task}.model`.
Resolution chain at dispatch time:

```
Spec.Subagent.Levels.<level>.model  →  Spec.Subagent.Model  →  Helm-chart default
```

Common patterns:

| Goal                                  | Configuration                                                                                                  |
| ------------------------------------- | -------------------------------------------------------------------------------------------------------------- |
| Cheap end-to-end smoke                | `subagent.model: claude-haiku-4-5` (all levels use haiku via fallback)                                          |
| Cheap planners, strong executors      | `subagent.model: claude-haiku-4-5`; `subagent.levels.task.model: claude-sonnet-4-6`                              |
| Strong planners, cheap executors      | `subagent.model: claude-haiku-4-5`; `subagent.levels.{milestone,phase,plan}.model: claude-sonnet-4-6`            |
| Maximum quality (expensive)           | `subagent.model: claude-opus-4-1` (set per-level if you want haiku for milestone to save cost on level fan-out) |

Vendor + model are **orthogonal axes**: the `subagent.image` picks the vendor
(via the container image's bundled `Subagent` impl); `subagent.model` picks the
model identifier passed to that vendor's CLI/SDK. The v1.0 ship vendor is
Anthropic; the acceptance test authors the OpenAI scaffold but the v1.0
chart's default `subagent.image` stays Anthropic.

### Git host configuration

`Project.Spec.git.repoURL` is the per-Project push target. v1.0 ships HTTPS+PAT
authentication; SSH is deferred to v1.x per `ART-05` host-key caveats.

See [`docs/git-hosts.md`](git-hosts.md) for per-host PAT creation recipes
(GitHub, GitLab, Gitea, generic HTTPS). The flow is:

1. Generate a minimally-scoped PAT on the host.
2. Wire it into a same-namespace Secret as data key `GIT_PAT`.
3. Reference that Secret name from `Project.Spec.git.credsSecretRef`.

### Basing a run on a branch, tag, or SHA

By default TIDE bases the per-run branch on the remote's default branch — its
`HEAD`. Set `Project.Spec.git.baseRef` to base the run on a specific branch, tag,
or commit instead. This is the lever for driving a run off an **unmerged** hotfix
branch, a release tag, or a pinned commit without first merging it to the default
branch.

**Accepted forms** — resolution walks these in order, first match wins:

| Form | Example | Notes |
| ---- | ------- | ----- |
| Branch name | `release/1.2` | Resolved against `refs/heads/*`, then the fetched `refs/remotes/origin/*` — an unmerged branch that lives only on the remote resolves here. |
| Tag name | `v1.2.0` | Annotated tags are peeled to the commit they point at; the peeled commit SHA is what lands in `status.git.baseSHA`. |
| Full 40-hex commit SHA | `9f8e7d6c5b4a39281706f5e4d3c2b1a09f8e7d6c` | Must be **reachable from a branch or tag** — the bare clone fetches all heads + tags, and a SHA outside that set is unresolvable (no targeted-SHA fetch; see the limit below). |
| Fully qualified `refs/...` path | `refs/heads/release/1.2`, `refs/tags/v1.2.0` | Resolved **verbatim, before the chain above** — the explicit disambiguation escape hatch when a branch and a tag share a name. |

**Rejected forms** — `HEAD`, short (abbreviated) SHAs, and `~`/`^` suffix
expressions (e.g. `main~2`, `v1.2.0^`) are **not** accepted. Resolution is an
explicit ref chain, not git revision syntax — there are exactly the three short
forms above plus the `refs/`-qualified escape hatch. To say "use the default
branch," leave `baseRef` **absent** — that is the only encoding of `HEAD`; there
is no `baseRef: HEAD` sentinel.

**The reachable-SHA limit.** A commit SHA that no fetched branch or tag reaches
(a bare PR-head SHA, say) resolves to the same `BaseRefUnresolvable` halt, with a
message noting SHAs must be reachable from a branch or tag. Targeted-SHA fetch is
a documented non-goal for now — pin the branch or tag that carries the commit.

**When the ref is unresolvable.** An unresolvable `baseRef` halts the clone
before any subagent spend, surfacing a `CloneFailed` condition with reason
`BaseRefUnresolvable` and a message naming the ref:

```
unable to resolve '<ref>' to a commit SHA; fix spec.git.baseRef to re-attempt the clone
```

The halt is deliberate — the same bad ref is **never** hot-looped. Recovery is a
single edit: correct `spec.git.baseRef` (a typo costs one `kubectl edit`, not a
Project recreate). The corrected spec is a new generation, which clears the halt
and re-runs the clone automatically. There is no ls-remote preflight — the check
fires in the clone Job only, so failure always surfaces the same way.

**Edits after a successful clone are inert.** `baseRef` is read **once**, when the
lifetime run branch is created. Editing it after the clone has succeeded changes
nothing — the run branch already exists and is never re-based. The same is true
for **adopted or imported Projects**, whose run branch already exists on the
remote: `baseRef` has no effect there. If you need a different base, start a new
Project. (This is documented behavior, not an enforced immutability rule — there
is deliberately no CEL rule pinning the field, so the recovery edit above stays
possible.)

**Provenance on every run.** `status.git.baseSHA` records the resolved 40-hex
commit the run branch was created from — stamped on every run, **including**
default-`HEAD` runs with no `baseRef` set. Because a branch or tag can move after
a run starts, `baseSHA` is the durable record of exactly which commit this run
built on.

Example — base a run on an unmerged hotfix branch:

```yaml
spec:
  targetRepo: "https://github.com/acme/widgets.git"
  git:
    repoURL: "https://github.com/acme/widgets.git"
    credsSecretRef: tide-secrets
    baseRef: "hotfix/urgent-cve"   # unmerged branch — no merge-to-main first
```
### Commit author identity

Every TIDE commit — harness task commits, integrate merges, and boundary
pushes — is authored under a configurable agent identity. Set it per-Project via
`git.agentName` / `git.agentEmail`, or install-wide via the chart values
`agent.name` / `agent.email` (`--set agent.name=... --set agent.email=...`) so a
cluster runs one identity without per-Project config. Precedence is Project spec
→ chart value → compiled-in default `TIDE Agent <tide-agent@tideproject.k8s>`;
leaving all tiers empty uses the compiled default.

Choose a **real, routable email**. It costs nothing today, but when commit
signing lands (deferred out of v1.0.7) the committer email must match a verified
email on the machine account that holds the signing public key — so picking a
throwaway address now forces an identity change across existing history later.
Pointing `git.agentEmail` at a dedicated machine-account address from the start
avoids that churn.

### Budget bypass (emergency lever — `tide approve --bypass-budget`)

When a Project halts on `Status.phase=BudgetExceeded` and the maintainer needs
to unstick it (e.g., the budget was set too low for the realized work), the
**bypass annotation** clears the gate without rewriting `Spec.budget`:

```bash
tide approve <project> --bypass-budget
# Or equivalently:
kubectl annotate project <project> tideproject.k8s/bypass-budget=true
```

**Do not use bypass in the acceptance test.** The acceptance contract (Phase 5
D-A2) treats the cap halt as a signal — if cost climbs past $25, the test
fails by design.

---

## Next steps

Once your Project is authored and applied:

- **Drive your project:** [`docs/cli.md`](cli.md) — operator-facing CLI verbs
  (`tide apply`, `tide watch`, `tide approve`, `tide reject`, `tide resume`,
  `tide cancel`). Install from the GitHub Release tarball or
  `go install github.com/jsquirrelz/tide/cmd/tide@latest` (krew-index
  submission pending — see docs/cli.md §1).
- **Configure gates:** [`docs/gates.md`](gates.md) — per-level gate policy
  (auto / approve / pause), annotation handshake, `tide approve` mechanics.
- **Inspect via dashboard:** [`docs/dashboard.md`](dashboard.md) — port-forward
  setup, ingress reference, read-only dashboard surface (DASH-05).
- **Wire metrics + traces:** [`docs/observability.md`](observability.md) —
  Prometheus `ServiceMonitor` gating, OTel exporter config, OpenInference
  attribute conventions.
- **Pick a storage driver:** [`docs/rwx-drivers.md`](rwx-drivers.md) — RWX PVC
  driver matrix for multi-pod fan-out (clone Job + planner Job + executor
  Job share the same workspace PVC).
- **Troubleshoot:** [`docs/troubleshooting.md`](troubleshooting.md) — common
  failure modes (finalizer stuck, 401 invalid key, push lease conflict,
  gitleaks blocked, RWX missing, etc.) with copy-paste recipes.
- **RBAC reference:** [`docs/rbac.md`](rbac.md) — per-Kind verbs, per-namespace
  RoleBinding template (AUTH-02 catch-up), conversion-webhook no-op caveats.

For the load-bearing five-level paradigm + two-DAG framing + water/tide
vocabulary, see [`README.md`](../README.md). The spec is the source of truth
for what TIDE is; this doc is the operator on-ramp for how to drive it.
