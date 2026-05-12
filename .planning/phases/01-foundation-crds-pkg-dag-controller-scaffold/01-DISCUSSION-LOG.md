# Phase 1: Foundation — CRDs, pkg/dag, Controller Scaffold - Discussion Log

> **Audit trail only.** Do not use as input to planning, research, or execution agents.
> Decisions are captured in `01-CONTEXT.md` — this log preserves the alternatives considered.

**Date:** 2026-05-12
**Phase:** 01-foundation-crds-pkg-dag-controller-scaffold
**Areas discussed:** Module path & repo identity (A), Wave CRD shape (B), Reconciler stub depth (C), Task dependsOn schema (F), Helm-values plumbing in P1 (E), Sample CRD fixtures (G), POOL-03 analyzer scope (D)

---

## Area Selection

| Option | Description | Selected |
|--------|-------------|----------|
| Module path & repo identity (A) | Go module path, image registry | ✓ |
| Wave CRD shape (B) | Who creates Wave, Spec vs Status, cycle detection location | ✓ |
| Reconciler stub depth (C) | How complete the six stubs are at Phase 1 end | ✓ |
| POOL-03 analyzer scope (D) | Custom go-analyzer for cross-pool wait | ✓ (revisited after first pass) |
| Helm-values plumbing in P1 (E) | How concurrency knobs reach the process before P5 Helm chart | ✓ |
| Task dependsOn schema (F) | Schema shape for dependency declarations | ✓ |
| Sample CRD fixtures (G) | Shape of kubectl-apply-able samples | ✓ |

**Notes:** Seven of seven gray areas discussed. POOL-03 was initially deferred and then pulled back in.

---

## A. Module path & repo identity

### A1. Go module path

| Option | Description | Selected |
|--------|-------------|----------|
| `github.com/justinsearles/tide` | Personal namespace; easy to move later | |
| `github.com/tideproject/tide` (Recommended) | Project-namespaced GitHub org; signals OSS intent | |
| `tideproject.k8s/tide` | Vanity path via meta-redirect; strongest OSS branding, non-trivial setup | |
| (Other) `github.com/jsquirrelz/tide` | User's actual personal-namespace handle | ✓ |

**User's choice:** `github.com/jsquirrelz/tide`
**Notes:** User opted for their personal namespace handle rather than the recommended project-org namespace. Go module path is changeable later via `go.mod` redirect; chose simplicity over OSS-branding hint.

### A2. Container image / Helm chart repo path

| Option | Description | Selected |
|--------|-------------|----------|
| `ghcr.io/<same-as-module>/tide-controller` (Recommended) | GHCR mirrors module path; free for OSS | ✓ |
| `docker.io/<org>/tide-controller` | Docker Hub; familiar but rate-limited | |
| Decide later — placeholder | TBD until Phase 5 release | |

**User's choice:** `ghcr.io/jsquirrelz/tide-controller` (and dashboard image will follow same pattern)
**Notes:** GHCR mirrors module path → no separate registry account, no Docker Hub rate-limit risk.

---

## B. Wave CRD shape

### B1. Who creates Wave objects

| Option | Description | Selected |
|--------|-------------|----------|
| `WaveReconciler` materializes on Plan ready (Recommended) | Deterministic names, idempotent re-derivation | ✓ |
| `PlanReconciler` creates Waves directly | Simpler but breaks one-reconciler-per-Kind invariant | |
| Drop Wave as a CRD; use status fields on Task | Saves a Kind but rejected by research convergence #6 | |

**User's choice:** `WaveReconciler` materializes on Plan ready
**Notes:** Aligns with research Pattern 1 (one reconciler per Kind) and makes the dashboard's live Execution DAG rendering trivial.

### B2. What lives in `Wave.Spec`

| Option | Description | Selected |
|--------|-------------|----------|
| Bare minimum: `planRef` + `waveIndex` (Recommended) | Members + state in Status; "derived not declared" structurally enforced | ✓ |
| Spec includes task refs list | Easier kubectl inspection but invites Pitfall 2 (cached schedule) | |
| Empty Spec — observability-only Kind | Cleanest pure-derived but unusual K8s shape | |

**User's choice:** Bare minimum (`planRef` + `waveIndex`)
**Notes:** Spec is intent, Status is observation — re-derivation rewriting Spec on every reconcile would feel wrong and weaken the Pitfall 2 guarantee.

### B3. Where cycle detection blocks

| Option | Description | Selected |
|--------|-------------|----------|
| Validating admission webhook on Plan (Recommended) | Cycle never reaches etcd; matches REQ-PLAN-01 | ✓ |
| CEL rule on Plan | Cleaner but CEL can't express transitive cycle detection | |
| Defer to WaveReconciler at materialize time | Cycles reach etcd; failed Plans stuck visibly | |

**User's choice:** Validating admission webhook on Plan
**Notes:** Phase 1 ships the webhook endpoint scaffold firing as no-op; Phase 2 wires the actual `pkg/dag.ComputeWaves` rejection logic.

---

## C. Reconciler stub depth

### C1. How complete are the six reconciler stubs at end of Phase 1

**First pass:**

| Option | Description | Selected |
|--------|-------------|----------|
| Standard: register + owner-ref + finalizer + status conditions, only dispatch stubbed (Recommended) | CTRL-05 + CRD-02 land in their mapped phase | |
| Minimal: register + return `Result{}` | Smallest scope but Pitfalls 21 + 23 leak to Phase 2 | ✓ (initially) |
| Maximal: everything except `Subagent.Run()` call | Pulls dispatch interface design into Phase 1 — risky | |

**User's first choice:** Minimal

**Conflict flagged:** Minimal pushed CTRL-05 + CRD-02 + Phase 1 success criterion #5 (kubectl delete cascade demo) out of Phase 1. User asked to resolve.

### C1-resolve. Resolution of Minimal-vs-requirements conflict

| Option | Description | Selected |
|--------|-------------|----------|
| Keep Minimal; move CTRL-05 + CRD-02 to Phase 2 | Roadmap success criterion narrows; Phase 2 grows | |
| Take Standard instead — keep CTRL-05 + CRD-02 in Phase 1, only dispatch missing (Recommended) | Matches REQUIREMENTS as written | ✓ |
| Hybrid: minimal stubs but ship owner-ref + finalizer helpers in `pkg/k8shelpers` | Compromise; helpers tested but reconcilers don't yet call them | |

**User's revised choice:** Standard
**Notes:** Standard depth lands owner refs + finalizers + status conditions in Phase 1 alongside the CRDs they govern, preventing Pitfalls 21 + 23 in the phase the traceability table maps them to. Phase 1 success criterion #5 verifies as-written.

---

## F. Task `dependsOn` schema

### F1. How tasks declare dependencies

| Option | Description | Selected |
|--------|-------------|----------|
| By name within same Plan: `dependsOn: ["alpha", "beta"]` (Recommended) | Simplest pkg/dag input; cross-Plan deps don't exist | ✓ |
| By `LocalObjectReference`: `dependsOn: [{name: alpha}]` | K8s-idiomatic, extensible, but more verbose | |
| By label selector | Powerful but invites runtime resolution bugs | |

**User's choice:** By name within same Plan (`[]string`)
**Notes:** CEL validates strings refer to sibling Tasks; cross-Plan refs rejected at admission.

### F2. `filesTouched` location

| Option | Description | Selected |
|--------|-------------|----------|
| `Task.Spec.filesTouched: []string`, required, non-empty (Recommended) | Phase 2 file-touch reconciliation; Pitfall 19 prevention | ✓ |
| Same, but optional | Weaker Pitfall 19 prevention | |
| Defer to Phase 2 | Save P1 scope but add a schema bump | |

**User's choice:** Required non-empty
**Notes:** CEL `MinItems: 1` in Phase 1; reconciliation against declared `dependsOn` lands in Phase 2's Plan admission webhook.

---

## E. Helm-values plumbing in Phase 1

### E1. Configuration source for concurrency knobs

**First pass:**

| Option | Description | Selected |
|--------|-------------|----------|
| Typed config struct loaded from `--config FILE` (Recommended) | ComponentConfig pattern; P5 Helm renders YAML into ConfigMap | |
| Plain flags on manager binary | Simpler P1, longer Deployment template | |
| Env vars | K8s ergonomic but flat; per-Kind MCR awkward | |
| Stub minimal Helm chart now | Pulls Helm into Phase 1; less rework at Phase 5 | ✓ |

**User's choice:** Stub minimal Helm chart in Phase 1

### E1-followup. Helm chart shape in Phase 1

| Option | Description | Selected |
|--------|-------------|----------|
| helmify-from-Kustomize, controller-only chart (Recommended) | kubebuilder `config/` canonical; matches REQ-DIST-02 | |
| Hand-written chart, controller-only | Skip helmify; introduce in Phase 5 | |
| Same as recommended but include CRD subchart from day one | Two charts: `charts/tide/` + `charts/tide-crds/` | ✓ |

**User's choice:** helmify-from-Kustomize + CRD subchart from day one (two charts)
**Notes:** Pulls REQ-DIST-01's dedicated CRD subchart pattern forward. Phase 5 adds dashboard, ServiceMonitor, docs — but the chart skeleton lives from Phase 1.

---

## G. Sample CRD fixtures

### G1. Sample CRD shape

| Option | Description | Selected |
|--------|-------------|----------|
| Hand-authored worked-example matching pkg/dag spec fixture (α…θ) (Recommended) | Same names across algorithm test, integration test, samples | ✓ |
| kubebuilder-default minimal samples | Smallest but no narrative coherence | |
| Two sample sets: minimal smoke + worked-example | More files but separated concerns | |

**User's choice:** Hand-authored α…θ worked example
**Notes:** `config/samples/tide_v1alpha1_task_alpha.yaml` etc. — naming aligns with kubebuilder convention; `kustomization.yaml` orders by owner-ref dependency.

---

## D. POOL-03 custom go-analyzer scope

### D1. What ships in Phase 1

| Option | Description | Selected |
|--------|-------------|----------|
| Working analyzer + CI gate (Recommended given Pitfall 6 bakes in here) | Full detector, `tools/analyzers/crosspool/`, Makefile + GitHub Action | ✓ |
| Placeholder analyzer + CONTRIBUTING.md note; full impl later | Skeleton Pass + TODO; weaker prevention | |
| Documentation-only; analyzer ships in Phase 2 | Lowest P1 scope; relies on PR review | |
| Working analyzer, advisory-only (CI warning, not fail) | Middle ground; defeats prevention guarantee | |

**User's choice:** Working analyzer + CI gate
**Notes:** ~1 day of focused work absorbed into Phase 1. Pitfall 6 (unified worker pool) cannot bake in even though pools are introduced in Phase 1.

---

## H. K8s API group / kubebuilder `--domain` (added post-planning, pre-execution)

This area was not surfaced during the initial discuss-phase pass — the planner inherited an assumed default of `tide.io` from the research artifacts (which had been generated before the domain question was raised). Caught at execute-phase boot when reviewing Plan 01's `kubebuilder init --domain tide.io` invocation.

### H1. The placeholder/registered-domain problem

The user flagged that `tide.io` is a real registered domain not owned by this project — collision risk + false provenance for an OSS posture. Also rejected placeholders (`tide.local`, `example.com`, `my.domain`, etc.) on principle: "make a concrete decision and document it; no TBD."

### H2. Candidate domain

**First pass** — Claude proposed `tide.justinsearl.es` (subdomain of user's owned email domain) or registering a project domain (`tide.run`, `tideproject.dev`, etc.).

**User counter-proposal:** `k8s.tide.ai`. Claude flagged `tide.ai` is `.ai` ccTLD (~$60-90/yr) and noted couldn't verify ownership from session context.

**User decision:** `tide.ai` is taken and not owned. Reverted to the "made-up string" approach Claude had mentioned (K8s validates only DNS-1123 syntax; doesn't require domain ownership).

### H3. Made-up string selection

| Candidate | Description | Selected |
|--------|-------------|----------|
| `tide.tide` | Project-name as both subdomain and TLD; self-namespacing; can't be registered (`.tide` will never be a real TLD) | |
| `tideproject.k8s` | `.k8s` will never be a real TLD (Kubernetes trademark blocks any application); more descriptive than `tide.tide`; slightly longer in every CRD/finalizer/label | ✓ |
| `tide.local` | mDNS-conventional but collides with other `.local` operators in shared clusters — bad for OSS | |

**User's choice:** `tideproject.k8s`
**Notes:** Locked as D-A3 in CONTEXT.md. Rewrite landed across 15 files (103 occurrences of `tide.io` → `tideproject.k8s`) — CRDs, finalizers, label keys, RBAC markers, kubebuilder `--domain` flag, sample YAMLs. No code yet exists, so the rewrite is pre-execution; v1alpha1 schema commits with `tideproject.k8s` as its API group from day one.

---

## Claude's Discretion

The following items the user explicitly deferred to Claude's judgment during planning/execution:

- Webhook certificate strategy in Phase 1 (envtest-only context — kubebuilder dev defaults likely fine)
- Conversion-webhook scaffold internals (kubebuilder v4.14 default emission)
- Finalizer name convention (`tideproject.k8s/<kind>-cleanup` pattern, applied uniformly)
- Repo top-level layout details beyond what the architecture doc specifies
- Status condition vocabulary across the six CRDs (small canonical set)
- `Chart.yaml` `appVersion` / `version` initial values, image tag scheme
- `cmd/tide-lint`'s exact CLI surface beyond "runs the analyzer over the module"
- Unit-test framework split between Ginkgo (controller suite) and stdlib testing (`pkg/dag`)
- Whether Phase 1 CI matrix includes a `kind` E2E run (recommended skip — envtest sufficient)

## Deferred Ideas

Items captured during discussion as belonging to later phases:

- Dashboard Helm chart template, `ServiceMonitor`, LICENSE headers, full external-operator docs → Phase 5
- Webhook actual cycle detection wiring → Phase 2
- File-touch ↔ `dependsOn` reconciliation logic → Phase 2
- Real `Subagent` interface design → Phase 2
- `kind` E2E test tier → Phase 2 (when Jobs are actually created)
- `tide` CLI → Phase 4
- Per-level model selection field consumption → Phase 2 or 4
- Conversion webhook actually doing conversion → beyond v1
