---
name: phase-40-v1alpha-removal-semantic-rename
description: Remove v1alpha1 (eventually v1alpha2) API code entirely + subagent.levels semantic rename (STAGE-02) — breaking migration; candidate Phase 40
type: phase-seed
captured: 2026-07-09
source: operator (v1.0.7 close-out); full planning artifacts on operator's OTHER machine, never pushed to this origin
relates_to:
  - STAGE-02 (subagent.levels rename — REQUIREMENTS.md:73/96, PROJECT.md:27)
  - todo 2026-07-03-project-level-subagent-override-slot.md (the DECIDED rename + SchemaRevision/v1alpha3 pattern)
  - phase-41-refactoring-review (the non-breaking refactor track this migration precedes/unblocks)
---

# Phase 40 (candidate) — API-version code removal + semantic rename

> **✅ SUPERSEDED BY IMPORT (2026-07-11).** The authoritative Phase 40 planning artifacts landed via rebase from the operator's other machine: `.planning/phases/40-deprecate-v1alpha1-api/` (CONTEXT, RESEARCH, PATTERNS, VALIDATION, 7 plans, CRANK-01..07). Every scope item below maps into those plans — guard re-expression + owner-walk + scheme comment → 40-03; semantic rename → 40-04; removal → 40-05. This file remains as the capture record; close it when Phase 40 closes.

**⚠ The authoritative Phase 40 planning artifacts were authored on the operator's other machine and were NEVER pushed to this `origin`.** Confirmed absent here 2026-07-09 via an exhaustive search: all four worktrees, every local + remote branch (incl. `backup/sounding-pre-rebase`), the stash stack, the reflog, and dangling/orphaned commits — zero `40-*` files ever added, zero "Phase 40"/v1alpha3/semantic-rename references. This file is a **scope capture** so the phase is tracked in-repo; the real artifacts still need to be **imported from the operator's machine** (or Phase 40 re-planned fresh).

## Scope (operator intent, 2026-07-09)

By "deprecate v1alpha1/v1alpha2" the operator means **remove the code completely**, not just stop serving it — plus the `subagent.levels` **semantic rename**.

1. **Full removal of the old API-version code.** Today `api/v1alpha1` is already `served: false, storage: false` (serving-deprecation done), but the Go types, webhook, and dual-version scaffolding remain. Phase 40 deletes them. Note the two live couplings that make this a *migration*, not a delete:
   - The **SCHEMA-03 RequiresReinstall guard** (`checkSchemaRevisionGuard`) + `project_controller_v2_guard_test.go` — the guard's contract must survive or be re-expressed.
   - `task_controller.go`'s owner-walk matches `"tideproject.k8s/v1alpha1"` APIVersion — surviving in-cluster v1alpha1 objects must be handled (convert/reject) before the types can go.
   - `cmd/manager/main.go:303-311` scheme registration + its (currently stale) comment (see Phase 41 item #3) get resolved here.
2. **`subagent.levels` semantic rename (STAGE-02).** DECIDED-but-breaking: each key names the artifact being planned. Needs SchemaRevision/v1alpha3 treatment so old manifests still admit. This is the piece already logged as STAGE-02 / the 2026-07-03 override-slot todo — Phase 40 bundles it with the removal since both are the same breaking schema event.

## Sequencing vs. Phase 41

Phase 41 (refactoring) **explicitly defers** the v1alpha removal to Phase 40 ("api/v1alpha1 removal is a migration decision, not a refactor" — do-not-refactor list). So Phase 40 (breaking migration) is independent of Phase 41 and can lead: doing 40 first collapses the dual-version scaffolding that several Phase-41 items (#1 typed constants land on v1alpha2, #3 scheme comment) otherwise have to work around.

## Milestone placement — OPEN

STAGE-02 was scoped to "its own milestone" (breaking). v1.0.7 (paper cuts, phases 34–39) is nearly closed. So Phase 40 (+ 41) most likely seed a **new milestone** (continuing global phase numbering past 39), not a v1.0.7 addition. Decide at import/planning time.

## Next action
- Import the operator's Phase 40 artifacts from the other machine (preferred — preserves prior planning), OR re-plan fresh via GSD once the milestone placement is decided.
