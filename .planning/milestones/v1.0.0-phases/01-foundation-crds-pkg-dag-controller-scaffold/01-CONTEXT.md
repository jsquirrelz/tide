# Phase 1: Foundation — CRDs, pkg/dag, Controller Scaffold - Context

**Gathered:** 2026-05-12
**Status:** Ready for planning

<domain>
## Phase Boundary

Lay down the operator skeleton — six `v1alpha1` CRDs (`Project`, `Milestone`, `Phase`, `Plan`, `Task`, `Wave`) with Spec/Status separation, a pure-Go layered-Kahn library (`pkg/dag`) with cycle detection, six event-driven reconcilers registered on one `controller-runtime` Manager (each handling owner refs, finalizers, status conditions, with the subagent-dispatch path stubbed for Phase 2), two separately-sized `chan struct{}` parallelism semaphores wired from configuration, kubebuilder RBAC markers with no wildcards, conversion-webhook + validating-admission-webhook scaffolding (firing as no-ops until Phase 2 wires cycle detection), a custom `go-analyzer` enforcing the two-pool boundary, a minimal helmify-generated Helm chart pair (controller chart + CRD subchart), and hand-authored α…θ worked-example sample CRDs matching `pkg/dag`'s spec-fixture test.

Phase 1 does not dispatch subagents, does not push to git, does not start the dashboard, and does not ship the `tide` CLI. Phase 1 ships everything required for Phase 2 to add dispatch logic without rewriting the scaffold.

</domain>

<decisions>
## Implementation Decisions

### Module identity & artifact paths
- **D-A1:** Go module path is `github.com/jsquirrelz/tide`. Every internal import resolves under this prefix. Module name in `go.mod` matches.
- **D-A2:** Container images publish to `ghcr.io/jsquirrelz/` — the controller image is `ghcr.io/jsquirrelz/tide-controller`, dashboard image (Phase 4) will be `ghcr.io/jsquirrelz/tide-dashboard`, future tide-lint analyzer image (if shipped) follows the same pattern. GHCR is free for OSS and avoids Docker Hub's anonymous-pull rate limits.
- **D-A3:** Kubernetes API group / kubebuilder `--domain` is `tideproject.k8s` — a deliberately made-up string with a non-existent TLD (`.k8s` will never be a real TLD because of the Kubernetes trademark). Every CRD's `apiVersion` is `tideproject.k8s/v1alpha1`; every finalizer is `tideproject.k8s/<kind>-cleanup`; every label key and annotation is `tideproject.k8s/<key>`; every kubebuilder RBAC marker uses `groups=tideproject.k8s`. **Never use `tide.io`** (real registered domain not owned by this project) **or any placeholder** like `tide.local`, `example.com`, `my.domain`, etc. Collision risk is zero because the TLD cannot be registered; placeholder risk is zero because the value is deliberately chosen and committed to v1alpha1.

### Wave CRD shape
- **D-B1:** `WaveReconciler` is the sole producer of `Wave` objects. On `Plan` reaching ready state, the reconciler runs `pkg/dag.ComputeWaves` over the Plan's Tasks and creates one `Wave` per layer with owner-ref back to the Plan. Wave names are deterministic — `tide-wave-{plan-uid}-{index}` — so re-derivation on every reconcile is idempotent. **No human or other controller creates `Wave` objects; the admission webhook rejects any client-applied `Wave`.**
- **D-B2:** `Wave.Spec` carries only `planRef` (owning Plan) and `waveIndex` (integer layer position). Every other field — task member list, dispatch state, completion timestamps, failure reasons — lives in `Wave.Status`. This makes the "derived not declared" principle structurally enforced: if a future PR tries to add scheduling-relevant data to `Wave.Spec`, the schema review checklist flags it as a Pitfall 2 (cached schedule) reintroduction.
- **D-B3:** Cycle detection runs in a **validating admission webhook on `Plan`** (not on `Task`, not on `Wave`). The webhook computes wave structure via `pkg/dag.ComputeWaves` and rejects cyclic Plans before they reach etcd, with a structured error naming the involved Tasks. Phase 1 scaffolds the webhook endpoint as a no-op (always Allow); Phase 2 wires the actual rejection logic. CEL is used on Plan/Task/Wave for non-graph invariants (non-empty fields, format, range) — it cannot express transitive cycle detection over arbitrary task graphs.

### Reconciler stub depth at end of Phase 1
- **D-C1:** Reconciler stubs are **Standard depth** — each of the six reconcilers (`ProjectReconciler`, `MilestoneReconciler`, `PhaseReconciler`, `PlanReconciler`, `WaveReconciler`, `TaskReconciler`) registers with the Manager, sets up the `Owns(&batchv1.Job{})` watch, ensures owner refs on create, runs idempotent finalizer cleanup with a bounded deadline on delete, and propagates status conditions (`Pending` / `Ready` / `Failed`). The **only** stubbed-out hole is the subagent dispatch path — Phase 2 fills exactly that. This keeps Pitfalls 21 (finalizer leaks) and 23 (wrong owner refs) prevented in the phase the requirements traceability table maps them to, instead of letting them bake in alongside Phase 2's densest-novel-territory dispatch work.
- **D-C2:** No `time.Sleep`, no blocking I/O, no LLM calls in any `Reconcile()` body in Phase 1. The custom analyzer (D-D1) is one half of the enforcement; manual review is the other.

### Task DAG declaration schema
- **D-F1:** `Task.Spec.dependsOn` is `[]string` — a list of sibling Task names within the same owning Plan. Cross-Plan dependencies do not exist in the spec; CEL validates that every string in `dependsOn` refers to a Task in the same Plan (cross-Plan refs rejected at admission). This keeps `pkg/dag`'s input shape trivial — string node IDs, no struct-key complications.
- **D-F2:** `Task.Spec.filesTouched` is `[]string`, **required and non-empty**. Every Task declares which paths under `/workspace/repo/` it intends to write. Phase 1 ships the schema field with CEL `MinItems: 1` validation; Phase 2 wires the file-touch ↔ `dependsOn` reconciliation in the Plan admission webhook (warning by default, strict-mode reject) — that's Pitfall 19 prevention.

### Configuration & distribution surface in Phase 1
- **D-E1:** Helm chart ships **in Phase 1**, not deferred to Phase 5. The release artifact is two charts: `charts/tide/` (controller-only — Deployment, RBAC, ServiceAccount, ConfigMap, values.yaml exposing `plannerConcurrency: 16`, `executorConcurrency: 4`, `maxConcurrentReconciles` per-Kind) and `charts/tide-crds/` (CRDs as a dedicated subchart for safe `helm upgrade`). kubebuilder's `config/` Kustomize output is the canonical source — the release pipeline runs `helmify` against `config/` to produce both charts. A `make helm` Makefile target invokes helmify locally.
- **D-E2:** Phase 5's Helm surface adds dashboard Deployment, `ServiceMonitor`, LICENSE headers, full docs, and external-operator dry-run validation. Phase 1's charts must remain Phase 5-compatible (helmify-driven) so Phase 5 just adds templates rather than restructuring.

### POOL-03 lint rule
- **D-D1:** **Working analyzer + CI gate ships in Phase 1.** Custom `golang.org/x/tools/go/analysis` Pass detects any `select` statement (or comparable construct) that waits on both `plannerPool` and `executorPool` channels in the same case set, lives in `tools/analyzers/crosspool/`, has `analysistest` fixtures under `testdata/`, and is invoked through `cmd/tide-lint` (a `unitchecker`-style entrypoint). `make lint` runs locally; `.github/workflows/ci.yaml` fails the PR on violation. Pitfall 6 (unified worker pool) cannot bake in during Phase 1 even though the pools are introduced in Phase 1.

### Sample CRDs / kubectl-apply fixtures
- **D-G1:** `config/samples/` contains a hand-authored worked-example set: one Project, one Milestone, one Phase, one Plan, and eight Tasks named `alpha` through `theta` whose `dependsOn` edges match the README spec's worked example exactly. Applied via `kubectl apply -k config/samples/` against the envtest cluster, the CRDs are accepted with CEL passing. The same task names and edges back the `pkg/dag` Kahn unit test fixture — so a reader greppingfor `alpha` finds the algorithm spec, the unit test, and the sample CRDs telling the same story.
- **D-G2:** Sample CRD YAML files are named `tide_v1alpha1_<kind>[_<name>].yaml` (kubebuilder default convention) and committed under `config/samples/`. A `kustomization.yaml` orders them so `kubectl apply -k` respects owner-ref dependencies (Project before Milestone before Phase before Plan before Tasks).

### Claude's Discretion
- Webhook certificate strategy for Phase 1 (envtest-only context — likely kubebuilder's auto-generated dev certs are sufficient; cert-manager integration ships when the chart matures in Phase 5).
- Conversion-webhook scaffold shape — pick whatever kubebuilder v4.14 emits by default for a single-version CRD's hub/spoke registration; no need to add v1beta1 stubs until v1beta1 is real work.
- Finalizer name convention — pick a `tideproject.k8s/<kind>-cleanup` form and apply uniformly.
- Repo top-level layout details (`cmd/manager/main.go` vs `cmd/tide-controller/main.go` etc.) — follow kubebuilder v4.14 scaffold defaults except where the architecture doc's `Recommended Project Structure §` overrides.
- Status condition vocabulary — pick a small canonical set (`Pending`, `Ready`, `Reconciling`, `Failed`) and apply uniformly across all six CRDs; document in the package.
- Helm chart `Chart.yaml` `appVersion` / `version` initial values, image tag scheme (likely `v0.1.0-dev` pending first real release tag).
- `cmd/tide-lint`'s exact CLI surface beyond "runs the analyzer over the module."
- Unit-test framework choice within Phase 1 — Ginkgo v2 + Gomega is the kubebuilder default for the controller suite; `pkg/dag` may use stdlib `testing` with `t.Run` table tests since it has no async-poll requirements (Ginkgo is overkill there).
- Whether Phase 1's CI matrix includes a `kind v0.31` E2E run (recommended skip — `envtest` is enough for Phase 1's no-dispatch contract; `kind` E2E lands when Phase 2 actually creates Jobs).

</decisions>

<canonical_refs>
## Canonical References

**Downstream agents MUST read these before planning or implementing Phase 1.**

### Spec & paradigm
- `README.md` — TIDE paradigm spec: five-level hierarchy, two-DAG split, layered Kahn worked example (α…θ), failure semantics at wave boundaries, resumption state contract
- `CLAUDE.md` — Project instructions: spec is load-bearing, vocabulary conventions, two parallelism budgets, artifact-as-source-of-truth principle, full Technology Stack table (versions + alternatives + what-not-to-use)

### Project frame
- `.planning/PROJECT.md` — Vision, requirements (active + out-of-scope with reasons), constraints, 13 locked Key Decisions
- `.planning/REQUIREMENTS.md` — v1 requirements (82 mapped, 26 in Phase 1), traceability table by REQ-ID → Phase, coverage summary
- `.planning/ROADMAP.md` §"Phase 1: Foundation — CRDs, pkg/dag, Controller Scaffold" — Phase goal, dependencies, requirements list, five success criteria

### Research synthesis (Phase 1 is the densest pitfall window)
- `.planning/research/SUMMARY.md` — Convergence table (16 hard contracts), divergence resolutions (Wave-as-Kind, CEL+webhook split, v1alpha1+conversion-webhook), Phase 1 in build-order rationale
- `.planning/research/STACK.md` — Pinned versions: Go 1.26, controller-runtime v0.24.x, kubebuilder v4.14.0, Kubernetes 1.33+ minimum, Ginkgo v2.28, zap v1.28, kind v0.31, helmify
- `.planning/research/ARCHITECTURE.md` — Component responsibilities, recommended project structure, Patterns 1-4 (one reconciler per Kind, owner-ref cascade, two-DAG one-algorithm, two parallelism budgets), Anti-Patterns 1-3 + 5 + 7 (relevant to Phase 1's scope boundary)
- `.planning/research/PITFALLS.md` — Eight Phase-1-mapped pitfalls: P1 (long-running reconcile), P3 (DAG unification), P4 (status-as-truth), P6 (unified worker pool), P15 (RBAC scope creep), P16 (breaking CRD changes), P21 (finalizer leaks), P23 (wrong owner refs). Each has explicit prevention + verification.
- `.planning/research/FEATURES.md` — TS-6, TS-7, TS-9, TS-18, D-1, D-2, D-3, D-10, D-11, D-12 (Phase 1-relevant feature classifications)

### External references (read on demand; don't pre-load)
- Kubebuilder Book — https://book.kubebuilder.io — Writing Tests (Ginkgo+envtest), Configuring EnvTest, Finalizers, CRD Versioning, Webhooks
- controller-runtime v0.24 docs — Manager, Reconciler, Owns, leader election, `MaxConcurrentReconciles`
- Kubernetes CRD validation rules (CEL) — `x-kubernetes-validations` markers, scope (single-object, no cross-resource)
- `golang.org/x/tools/go/analysis` — Pass framework, `analysistest` fixtures (POOL-03 analyzer foundation)

</canonical_refs>

<code_context>
## Existing Code Insights

### Reusable Assets
- **None yet.** Repo contains only `README.md` (spec) and `CLAUDE.md` (project instructions). Phase 1 is the first phase to land Go code; everything starts from scratch via `kubebuilder init` + `kubebuilder create api`.

### Established Patterns
- **Spec/research convergence dictates structure.** Architecture's "Recommended Project Structure" (`.planning/research/ARCHITECTURE.md` §"Recommended Project Structure") is the baseline — `cmd/`, `api/v1alpha1/`, `internal/controller/`, `pkg/dag/`, `pkg/dispatch/` (Phase 2), `pkg/git/` (Phase 3), `pkg/otelai/` (Phase 4), `tools/analyzers/`, `config/` (kustomize), `charts/tide/`, `charts/tide-crds/`.
- **Vocabulary discipline applies to identifiers and log lines** — Rising tide, Slack tide, Tidal lock, Tidepool, TIDE pod. Reconciler names use Kind names (`ProjectReconciler`, `WaveReconciler`, etc.). Helm chart names are `tide` and `tide-crds`. CLAUDE.md is the authoritative source for the metaphor.

### Integration Points
- **No external runtime dependencies in Phase 1** — Phase 1 is self-contained: kubebuilder scaffold + envtest + unit tests + analyzer + Helm chart generation via helmify. No LLM provider, no git remote, no real cluster (kind comes online in Phase 2).
- **Future integration seams declared (but not wired) in Phase 1:** the dispatch interface lives in `pkg/dispatch` package, the `Subagent` interface signature is reserved (Phase 2 designs and implements), reconcilers reserve the call site behind a `Dispatcher` field on each Reconciler struct (nil in Phase 1, injected in Phase 2). This avoids a Phase 2 refactor when the interface lands.

</code_context>

<specifics>
## Specific Ideas

- **α…θ worked example as the spine.** The `pkg/dag` unit-test fixture, the `config/samples/` CRDs, and the controller integration test all use the same eight task names (`alpha`, `beta`, `gamma`, `delta`, `epsilon`, `zeta`, `eta`, `theta`) with the same edge set, producing the same wave structure (`W1: {α,β,γ,ζ} → W2: {δ,η} → W3: {ε,θ}`). A reader greppingfor `alpha` finds the algorithm spec (README), the algorithm test (`pkg/dag/kahn_test.go`), and the K8s integration story (`config/samples/`) all reinforcing each other.
- **Pitfall prevention belongs in the phase the traceability table names.** When the user's first instinct was to ship "Minimal" reconciler stubs, the explicit collision with Phase 1 success criterion #5 surfaced — and the user reverted to "Standard" depth. The principle to carry forward: gray-area decisions that push a mapped requirement into a later phase deserve a flag, not silent acceptance.
- **Helm chart pulled forward to Phase 1 against the recommended "config-struct-only" path.** User's reasoning: getting `helmify`-from-Kustomize working in Phase 1 means Phase 5 adds templates rather than discovers structural surprises. CRD subchart pulled forward for the same reason (REQ-DIST-01's "dedicated subchart for safe upgrades" pattern is structurally easier to scaffold once than to migrate into).
- **POOL-03 analyzer is real engineering, but Pitfall 6 is named Serious and bakes in at controller scaffold time.** Working analyzer + CI gate is "+1 day of focused work" by the user's estimate; the alternative was deferring detection until Phase 2 — but by then the dispatch logic is already touching both pools and any unification mistake would have to be unwound. Phase 1 is the right home.

</specifics>

<deferred>
## Deferred Ideas

- **Dashboard chart template, `ServiceMonitor`, LICENSE headers, full external-operator docs** — Phase 5 (DIST-01..05). Phase 1 charts are deliberately scoped to controller + CRDs only.
- **Webhook actual cycle detection** — Phase 2 (REQ-PLAN-01). Phase 1 ships the webhook endpoint scaffold firing as no-op.
- **File-touch ↔ `dependsOn` reconciliation** — Phase 2 (REQ-PLAN-02). Phase 1 ships the `filesTouched` schema field with CEL `MinItems: 1`; reconciliation logic against declared edges lands with the Plan admission webhook in Phase 2.
- **Real `Subagent` interface design** — Phase 2 (REQ-SUB-01). Phase 1 reserves the package path (`pkg/dispatch`) and a `Dispatcher` field on Reconciler structs but writes no interface signature yet.
- **kind E2E test tier** — Phase 2 (REQ-TEST-02). Phase 1's tests run via envtest (no kubelet, no real Jobs) — sufficient because Phase 1 doesn't create Jobs.
- **`tide` CLI** — Phase 4 (REQ-CLI-01..04). Phase 1's `cmd/` contains the manager binary and `cmd/tide-lint`; no end-user CLI yet.
- **Per-level model selection field on Project CRD** — Phase 2 or 4. Schema field is reserved in `Project.Spec` (research divergence #10 resolved to expose it) but Phase 1 just declares it with default; consumption is downstream.
- **Conversion webhook actually doing conversion** — beyond v1; Phase 1 only scaffolds the hub/spoke registration so adding `v1beta1` later is non-breaking (REQ-CRD-05, Pitfall 16 mitigation).

</deferred>

---

*Phase: 01-foundation-crds-pkg-dag-controller-scaffold*
*Context gathered: 2026-05-12*
