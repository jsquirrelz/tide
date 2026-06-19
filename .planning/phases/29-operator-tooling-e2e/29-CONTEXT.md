# Phase 29: Operator Tooling + E2E - Context

**Gathered:** 2026-06-19
**Status:** Ready for planning

<domain>
## Phase Boundary

Wrap the **already-built Phase 28 import core** in an operator-facing CLI and prove
it end-to-end. Two deliverables:

1. **`tide export-envelopes` / `tide import-envelopes` (with `--dry-run`)** — a stateless
   cobra CLI (D-C1) that exports a Project's planner envelopes to a portable bundle and
   stages a bundle into a target namespace so a subsequent `tide apply` of the Project
   adopts them (skipping every planner whose valid envelope exists).
2. **A kind integration test** proving zero-cost resumption: the `salvage-20260618`
   fixture round-trips through the real CLI, planners are skipped, execution proceeds,
   and no planning cost is re-paid.

**In scope:** the CLI verbs + bundle format + dry-run report + the E2E test.
**Out of scope (built in Phase 28, do not re-open):** the `ImportController` state machine,
the `cmd/tide-import` Job (copy + UID-rekey + v1alpha1→v1alpha2 conversion), the
`spec.importSource` surface, the `ImportComplete` park guards at the five dispatch sites,
and the seed-ConfigMap consumption contract. Phase 29 *produces* the bundle and seed
that Phase 28's machinery *consumes*; it does not change that machinery.

</domain>

<decisions>
## Implementation Decisions

### Export — mechanism & bundle (`tide export-envelopes`)
- **D-01:** Read envelope bytes off the per-namespace PVC by **reusing the
  `tide artifact-get` inspector-pod pattern** — a short-lived `busybox:1.36` pod mounts
  the PVC, tars the Project's envelopes subtree, and streams it to the CLI, which writes
  the bundle locally. No long-lived-pod dependency; symmetric with the existing read path.
- **D-02:** The bundle **mirrors the `salvage-20260618` fixture shape**: CR-tree YAMLs
  down to Plan level (`projects/milestones/phases/plans.yaml`), a **seed manifest**, the
  `pvc-envelopes/` envelope tree, and a human-readable `SEED-OUTLINE.md`. Default output
  is a single **`.tgz`**, with a `--dir` flag to emit the unpacked directory. The format
  must round-trip cleanly into `import-envelopes`.
- **D-03:** **Export emits the seed manifest** (full CR specs + the `name→oldUID` map),
  capturing live UIDs at the **only point they are reliably available** (querying the live
  cluster at export time). Import applies this verbatim — it never has to reconstruct the
  UID map. The fully-qualified-name keying convention from Phase 28 D-07 (object name +
  full parent chain) is the map key.
- **D-04:** Export writes a **per-envelope sha256** into the seed manifest. This closes
  the Phase 28 deferred integrity nicety while the tooling is being built; import/dry-run
  verify it as an additional gate beyond the `len(ChildCRDs)==ChildCount` completeness check.

### Import — staging flow (`tide import-envelopes`, live mode)
- **D-05:** `import-envelopes` is **stage-only**: it (1) loads the bundle's envelopes onto
  the target namespace's PVC, (2) creates the seed ConfigMap, and (3) surfaces the bundle's
  `project.yaml`. It does **not** apply the Project. The operator runs **`tide apply
  project.yaml`** separately — keeping one blessed Project-creation path and making the
  natural flow `dry-run → review → apply`. Phase 28's "fresh apply adopts" criterion still
  holds: the *Project apply* triggers adoption; staging just puts the bytes + ConfigMap in
  place first.
- **D-06:** Envelopes land on the PVC via a **loader pod** (the reverse of the export
  inspector pod): a short-lived pod mounts the PVC, the CLI streams the bundle tgz in, the
  pod unpacks it to the declared `pvcSubPath` at **old-UID paths**. `cmd/tide-import` then
  copies + rekeys those to the new-UID workspace paths (Phase 28 D-08 contract unchanged:
  `tide-import` reads only from the local PVC `pvcSubPath`, no cross-namespace reads).

### Dry-run — preview & report (`tide import-envelopes --dry-run`)
- **D-07:** Dry-run validates **locally on the unpacked bundle** — **no cluster writes, no
  pods**, works before the target cluster even exists. It **reuses the Phase 28 validation
  code** client-side: `ValidateAPIVersionKind`, the completeness check, the sha256 check
  (D-04), and `dag.ComputeWaves` for cycle detection.
- **D-08:** Output is a **per-level adopt/re-plan table** (`level | name | verdict | reason`)
  plus a summary count line, with **`--output json`** for machine consumption. The `reason`
  cites the failure class: schema mismatch / completeness failure / cycle / checksum mismatch.
- **D-09:** A detected **cycle hard-rejects the whole import** — dry-run reports the cycle
  edges and marks the entire import as would-fail, never per-level partial adoption. Mirrors
  Phase 28 D-10 (cycle ⇒ `ImportFailed`, no partial CRs); cycles are bugs, never adopted.

### E2E test (TOOL-02)
- **D-10:** The test **drives the real CLI** — `tide export-envelopes` then
  `tide import-envelopes` — so the bundle round-trips through the actual commands,
  exercising TOOL-01 and TOOL-02 together end-to-end.
- **D-11:** **Two-tier assertion bar** (best rigor-per-cost, honors ROADMAP criterion #4):
  - A **small purpose-built fixture** is imported and driven **all the way to
    all-Milestones-`Succeeded`** with stub subagents — proving `import → execute → Succeeded`
    end-to-end including cross-level / cross-milestone transitions.
  - The **full `salvage-20260618` fixture** is imported and asserted for **adoption +
    zero planner Jobs dispatched for imported levels + $0 re-paid planning cost** (budget
    rollup suppressed per Phase 28 D-11) — proving the real salvage data adopts and scales,
    without requiring the flaky full 42-plan drain to Succeeded on a single kind node.
- **D-12:** Gate the heavy paths behind the existing kind-suite guard convention
  (`testing.Short()` skip + long-test tag) so the full-fixture and drain-to-Succeeded
  assertions don't bloat the default `make test-int` wall-clock.

### Invariants carried forward (locked — do not re-litigate)
- **D-13:** Wave CRs are **never** exported or imported — always re-derived (Phase 28 D-09).
- **D-14:** Budget rollup stays **suppressed for imported envelopes** (Phase 28 D-11) — the
  prior run already paid the planning cost; the E2E test asserts $0 re-paid as the headline.
- **D-15:** Seed covers **down to Plan only** (Phase 28 D-04); Tasks materialize from
  plan-level envelope children via the unchanged reporter/materializer path.

### Claude's Discretion
- Exact cobra flag names, the seed-manifest on-disk schema (beyond "CR specs + name→oldUID +
  sha256"), inspector/loader pod RBAC verbs, and the small E2E fixture's concrete shape are
  authoring decisions for the planner, within D-01…D-12.
- Whether export/import share a single `pkg/`-level bundle reader/writer (recommended for
  round-trip symmetry) vs. per-command code is a planner structuring choice.

</decisions>

<canonical_refs>
## Canonical References

**Downstream agents MUST read these before planning or implementing.**

### Phase 28 import core (what Phase 29 wraps — read first)
- `.planning/phases/28-plan-import-core/28-CONTEXT.md` — the import-mechanism decisions
  (D-01…D-12 there): Approach B, seed ConfigMap + `spec.importSource`, UID rekey on
  fully-qualified name, schema conversion in the Job, trust boundaries, cycle-before-create.
- `internal/controller/import_controller.go` — the `ImportController` state machine the
  seed ConfigMap drives.
- `cmd/tide-import/main.go` — the in-pod copy + rekey + atomic `out.json` rewrite the
  loader-staged envelopes feed.
- `api/v1alpha2/import_types.go` — `ImportSourceRef` (`{seedConfigMapRef, pvcSubPath}`) the
  bundle's `project.yaml` must populate; `api/v1alpha2/shared_types.go:265`
  `ConditionImportComplete`.

### CLI conventions & the reusable PVC-access analog
- `cmd/tide/main.go` — stateless cobra CLI contract (D-C1: kubeconfig chain, no local state),
  the D-C3 verb set, Krew/`kubectl-tide` naming, Ctrl-C context.
- `cmd/tide/subcommands.go` — `registerSubcommands` + the `newXxxCmd()` constructor pattern
  (new `export-envelopes` / `import-envelopes` verbs register here).
- `cmd/tide/artifact_get.go` + `cmd/tide/artifact_get_run.go` — **the inspector-pod pattern
  to reuse/mirror** (`busybox:1.36`, `inspectorPodRunner` func-var seam, readiness wait,
  RBAC: pods create/get/delete + pods/log get). Export reuses this; import inverts it (loader).

### Validation + DAG (reused client-side by dry-run)
- `pkg/dispatch/envelope.go:407` `ValidateAPIVersionKind` + `:24` `APIVersionV1Alpha1` —
  envelope version/kind validation.
- `pkg/dag/kahn.go:46` `ComputeWaves` — cycle detection + wave derivation; dry-run calls it
  on the imported graph (D-07/D-09).

### Salvage fixture (the acceptance target)
- `examples/projects/dogfood/salvage-20260618/SEED-OUTLINE.md` — 3 milestones / 15 phases /
  42 plans; the human-readable tree.
- `examples/projects/dogfood/salvage-20260618/{projects,milestones,phases,plans}.yaml` +
  `pvc-envelopes.tgz` — **the bundle shape D-02 must reproduce** (down to Plan; no tasks.yaml).

### Spec invariants
- `README.md` — §"Failure handling at wave boundaries"; waves derived-not-declared; cycles
  are bugs (refuse, don't recover); resumption = indegree map + completed-set.

### Requirements / roadmap
- `.planning/REQUIREMENTS.md` — TOOL-01, TOOL-02 (Phase 29).
- `.planning/ROADMAP.md` §"Phase 29: Operator Tooling + E2E" — goal + 4 success criteria
  (criterion #4 = "all Milestones reach Succeeded", satisfied by D-11's small-fixture tier).

### Background research (the v1.0.3 basis)
- `.planning/research/ARCHITECTURE.md` — Approach B detailed design.
- `.planning/research/FEATURES.md` — Phase 2 import-core feature set + prior-art convergence.
- `.planning/research/PITFALLS.md` / `.planning/research/SUMMARY.md` / `.planning/research/STACK.md`
  — R-01…R-13 pitfalls (incl. R-02 UID aliasing, R-05 partial-plan, R-12 cycle bypass,
  R-13 rollup suppression); zero new `go.mod` entries expected.

</canonical_refs>

<code_context>
## Existing Code Insights

### Reusable Assets
- **`cmd/tide/artifact_get_run.go` inspector-pod machinery** (`inspectorPodRunner` func-var,
  busybox mount + readiness wait): the export read path (D-01) and the import loader pod
  (D-06) both build on this shape — one streams out, one streams in.
- **`cmd/tide/subcommands.go` constructor pattern** (`newXxxCmd()` + `buildRootForTest`):
  the two new verbs slot in here; test isolation already established.
- **Phase 28 validation surface** (`ValidateAPIVersionKind`, completeness check,
  `dag.ComputeWaves`): dry-run (D-07) reuses these client-side rather than reimplementing.
- **`cmd/tide-import/main.go`** (copy + rekey + convert): unchanged; the loader-staged
  old-UID envelopes are exactly its input contract.

### Established Patterns
- **Stateless CLI over kubeconfig** (D-C1): both verbs resolve `K8sClient()`/`RESTConfig()`
  the same way `artifact-get` does; no new auth/config surface.
- **`AlreadyExists`-is-success idempotency** (Phase 28): re-running `import-envelopes` /
  re-applying the Project must be a no-op, not a duplicate-create error.
- **Existing kind integration suite** (`test/integration/kind/*_test.go`, `suite_test.go`,
  `testdata/`): the new E2E test follows this harness + the `testing.Short()` gating
  convention (D-12). Note known environmental flakes (medium_http, Layer B) — the full-drain
  tier (D-11) is the new flake surface the two-tier design deliberately bounds.

### Integration Points
- New `cmd/tide/export_envelopes.go` + `cmd/tide/import_envelopes.go` (+ `_test.go`),
  registered in `subcommands.go`.
- New kind test file under `test/integration/kind/` driving the CLI round-trip against
  both the small fixture and `salvage-20260618`.
- A small purpose-built E2E fixture (new `examples/` or `testdata/` tree) for the
  drain-to-Succeeded tier (D-11).

</code_context>

<specifics>
## Specific Ideas

- The concrete bar is the **real `salvage-20260618` fixture** (~$90 of dogfood-run-#2
  planning that budget-halted with zero execution). "Adopt envelopes, skip planners, pay $0
  planning" is the headline the E2E test must assert on the real data.
- The bundle format is intentionally **identical to the existing fixture** so `salvage-20260618`
  itself doubles as a golden round-trip fixture — `export` of a freshly-imported run should
  reproduce an equivalent bundle.

</specifics>

<deferred>
## Deferred Ideas

- **Automatic export-on-halt** (snapshot envelopes to a durable bundle when a budget/failure
  halt fires) — already a deferred future requirement; a convenience layer atop TOOL-01, not
  Phase 29.
- **Hybrid by-name write-side envelope paths** (planner also writes `by-name/<fqName>/out.json`
  so future salvages need no import Job) — a new capability, its own backlog item (surfaced in
  Phase 28).
- **Partial / incremental re-planning** (re-author only the changed sub-tree) — out of scope;
  Phase 29 targets full-tree adoption.

### Reviewed Todos (not folded)
- `cache-f1-direct-sdk-cross-pod-caching` (todo match score 0.6) — **not folded.** It's the
  carried-from-Ebb-Tide CACHE-F1 direct-SDK backend, explicitly orthogonal to plan-import and
  listed as a deferred future requirement. Out of scope for Phase 29.

</deferred>

---

*Phase: 29-operator-tooling-e2e*
*Context gathered: 2026-06-19*
