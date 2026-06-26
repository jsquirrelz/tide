# Phase 30: Resumable Import — Partial-Tree Resume - Context

**Gathered:** 2026-06-25
**Status:** Ready for planning
**Source:** plan-phase fork resolution (4 design forks from run-2-FINDINGS.md resolved interactively)

<domain>
## Phase Boundary

**Delivers:** Import that resumes a *partially*-completed salvage tree — its primary use case —
via **adopt-complete + re-plan-incomplete**, driven by per-node envelope completeness.

Today the `ImportController` materializes every seed node at its salvaged status (e.g. `Running`)
while the `tide-import` completeness guard correctly *skips* incomplete envelopes
(`exitCode != 0 || len(childCRDs) != childCount`). The two halves don't reconcile: an incomplete
node is materialized `Running` with **no envelope behind it** → controller can't find the envelope,
can't materialize children → permanent stall + `tide-reporter` thrash loop. Run #2 evidence:
`{copied:257, converted:60, incomplete:40}` — 40 nodes became `Running`-with-no-envelope zombies,
`Task=0`, ~168 failed reporter pods, Project stuck `Running`.

**In scope:**
- Per-node branch in import materialization: complete envelope → adopt (current behavior);
  incomplete/missing envelope → materialize the node in a re-plannable (Pending) state.
- Tighten the Project-level adoption guard so the project planner cannot re-dispatch post-import.
- A new test tier that drives a *partial* import (mixed complete/incomplete) all the way to
  `Project=Complete` — coverage the existing Tier-b `import_resume_test.go` is missing.

**Out of scope:**
- The OpenAI backend and dogfood run #2 re-attempt (this phase *unblocks* run #2 but does not run it).
- Re-architecting envelope completeness signaling (the `isEnvelopeComplete` signal is already correct
  and is the driver for the new branch — reuse it, don't redesign it).
- Changing the import platform/adoption gate semantics for *fully*-complete trees (current behavior
  is correct for that case and stays).

</domain>

<decisions>
## Implementation Decisions

### Fork 2 (LOCKED) — Re-plannable mechanism: materialize incomplete node as Pending; parent re-authors
When import encounters an incomplete or missing envelope for a seed node, **materialize the CR but
reset it to a fresh/Pending state** (no envelope copied) so its parent — or the node itself —
re-plans it against current `main`.
- **Chosen over** "omit the node entirely; parent re-plans the whole subtree."
- **Why:** preserves node identity/UID so a *completed* node that adopted a `dependsOn` edge to this
  node stays valid. Run #2 had exactly this shape (complete nodes depending on incomplete ones).
  Omitting would break those adopted `dependsOn` edges and discard deeper adopted descendants.
- **Rejected:** omit-and-parent-re-plans — cleaner consistency with current `main` but breaks adopted
  dependents and throws away salvageable completed descendants.

### Fork 3 (LOCKED) — Project-level adoption: tighten guard to child/status-based
`project_controller.go:1037-1052`'s idempotency guard is **Job-presence-based**, so the project
planner can still dispatch after `ImportComplete` (run #2 observed a `tide-project` planner Job fire).
**Tighten** project-level adoption to gate on materialized children / `ImportComplete` state rather
than Job presence, so there are no redundant project planner re-dispatches post-import.
- **Note the existing comment's reasoning** (lines 1037-1043): Job-presence was chosen over a
  count-based guard specifically because N child Milestones materialize incrementally and a count
  guard would abort mid-stream. The fix must NOT regress that N>1-milestone case — gate on the
  *import/adoption* signal (`ImportComplete`), not on a child *count*.

### Fork 1 (RESEARCH) — Exact re-plannable status semantics
What exact status makes a node re-plannable **without** tripping (a) the import platform/adoption gate
or (b) the Job-presence idempotency guard? Resolve in research against the live CRD status machines
(`*_controller.go` materialization paths + the import adoption gate). The chosen status must round-trip:
materialize → parent/self re-plans → children materialize → cascade resumes.

### Fork 4 (REQUIRED) — New partial-tree-to-completion test tier
Add a test that drives a **partial** import (mixed complete/incomplete envelopes, ideally the run #2
`salvage-20260618` bundle or a trimmed analog) all the way to `Project=Complete`. The existing
Tier-b `import_resume_test.go` asserts only the adoption *gate* (zero re-paid planner Jobs,
`ImportComplete=True`) and never drives the partial tree to execution — which is why the zombie stall
shipped green. The new tier asserts the **end-to-end outcome**, not just the mechanism.

### Dependency consistency (resolved by Fork 2 choice)
Because the node is materialized (identity preserved) rather than omitted, an adopted completed node
that `dependsOn` a re-planned node keeps a valid edge. Research must still confirm: when the
re-planned node regenerates children, do the **global indegree** / dependent edges stay consistent
for the already-adopted dependents? (Global Execution DAG from v1.0.2 Spring Tide is the substrate.)

### Claude's Discretion
- Exact field/status value and where the branch lives (ImportController materialization vs a helper).
- Test fixture shape (reuse run #2 salvage bundle vs a minimal hand-authored mixed bundle).
- Whether the project-guard tightening is a new condition or a refactor of the existing guard block.

</decisions>

<canonical_refs>
## Canonical References

**Downstream agents MUST read these before planning or implementing.**

### The defect + fix shape (authoritative)
- `.planning/dogfood/run-2-FINDINGS.md` — root cause, fix shape, the 4 design forks, run #2 evidence.

### Import materialization (where the per-node branch lands)
- `internal/controller/import_controller.go` — `reconcileCreatingCRs` materializes each seed node as
  a CR with its salvaged status; this is where complete-vs-incomplete branching must happen.
- `cmd/tide-import/main.go` — completeness guard (`isEnvelopeComplete`, ~line 219/315): `exitCode==0`
  AND `len(childCRDs)==childCount`; emits `{copied,converted,incomplete}` report. The signal that
  drives the new branch — reuse, don't redesign.

### Project-level adoption guard (Fork 3)
- `internal/controller/project_controller.go:1037-1052` — the Job-presence idempotency guard to
  tighten; read the surrounding comment (lines 1037-1043) for the N>1-milestone constraint that must
  not regress.

### Test coverage gap (Fork 4)
- `test/integration/kind/import_resume_test.go` — existing Tier-b test that asserts only the adoption
  gate; the new partial-tree-to-completion tier extends or parallels it.

### Execution-DAG substrate (dependency consistency)
- v1.0.2 Spring Tide global Execution DAG (Phases 22-26) — `internal/controller/depgraph.go` and the
  global indegree model are the substrate; re-planned-node children must keep global indegree
  consistent for adopted dependents.

</canonical_refs>

<specifics>
## Specific Ideas

- The completeness signal already exists (`isEnvelopeComplete` in `cmd/tide-import/main.go`) and the
  import report already counts `incomplete` — the fix wires an existing-but-unused signal into the
  materialization branch, it does not invent a new one.
- Run #2 reproduction asset: `salvage-20260618` bundle (60 complete / 40 incomplete) on the
  `kind-tide-dogfood` cluster, plus `examples/projects/dogfood/run-2/seed-manifest.trimmed.json`.
- Carried-in infra (reusable for validation): `kind-tide-dogfood` cluster (v1.0.4 chart, in-cluster
  git mirror, `tide-dogfood-codex` ns with `tide-import` SA). Note the medium per-ns template lacked
  the `tide-import` SA — adding it to the template is a candidate sub-task.

</specifics>

<deferred>
## Deferred Ideas

- Dogfood run #2 re-attempt (import partial salvage → execute to completion → review diffs) — happens
  AFTER this phase lands; bounded by the $50 metered cap. Out of this phase's scope.
- OpenAI backend — separate milestone (vNext), gated on this resumability fix.

</deferred>

---

*Phase: 30-resumable-import-partial-tree-resume-adopt-complete-re-plan*
*Context gathered: 2026-06-25 via plan-phase fork resolution*
