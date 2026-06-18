# Phase 28: Plan-Import Core - Discussion Log

> **Audit trail only.** Do not use as input to planning, research, or execution agents.
> Decisions are captured in CONTEXT.md — this log preserves the alternatives considered.

**Date:** 2026-06-18
**Phase:** 28-plan-import-core
**Areas discussed:** Import mechanism (A vs B), Operator trigger surface, v1alpha1→v1alpha2 conversion, Stable-identity & trust matching

---

## Import mechanism — Approach A vs B (the mandated design checkpoint)

| Option | Description | Selected |
|--------|-------------|----------|
| Approach B | One-shot ImportController + tide-import Job re-keys old-UID envelope trees to new UIDs; 5 reconcilers + reporter + path contract untouched | ✓ |
| Approach A | Name-based stable-key lookup; ReadOut grows a permanent by-name fallback path; needs a migration anyway (salvage has no by-name paths) | |
| Hybrid (B now + write-side later) | B for Phase 28, plus a deferred future capability where the planner writes by-name paths going forward | (write-side captured as deferred idea) |

**User's choice:** Approach B.
**Notes:** The fixture (`salvage-20260618`) has only UID-keyed paths, which collapses Approach A's "no migration" advantage — A would need a migration step functionally identical to B *plus* permanent dual-path complexity on the hot `ReadOut` path and cross-project collision risk. B confines all complexity to a one-shot phase that vanishes after `ImportComplete=True`. Research was split (STACK/FEATURES favored A; ARCHITECTURE favored B); the fixture reality decided it.

---

## Operator trigger surface

| Option | Description | Selected |
|--------|-------------|----------|
| Single Project + seed ConfigMap | Operator applies one Project with `spec.importSource = {seedConfigMapRef, pvcSubPath}`; ImportController materializes the CR tree and owns the old→new rekey table | ✓ |
| Operator applies full CR tree | Operator kubectl-applies the exported CR YAMLs directly; a separate signal triggers only the envelope-bridge Job | |

**User's choice:** Single Project + seed ConfigMap.
**Notes:** Matches success-criterion #1 and captures the old→new UID mapping atomically at creation. The direct-apply alternative was rejected because applied CRs get fresh UIDs and old UIDs are stripped on create, forcing fragile out-of-band capture of the mapping. The seed covers down to Plan level only (no `tasks.yaml`), so only Tasks materialize from envelopes.

---

## v1alpha1→v1alpha2 conversion

| Option | Description | Selected |
|--------|-------------|----------|
| Convert in tide-import Job, then strict-validate | One-shot Job upgrades child `Spec.Raw` v1alpha1→v1alpha2 during copy, validates against the v1alpha2 struct, fails import if unconvertible; steady-state materializer unchanged | ✓ |
| Convert in MaterializeChildCRDs (on read) | Conversion shim in the steady-state materializer — centralizes but permanently pollutes the hot reconcile path | |
| Validate-only, no conversion | Assume children are already v1alpha2; reject non-v1alpha2 → re-plan. Risks re-paying planning if R-06 is right | |

**User's choice:** Convert in tide-import Job, then strict-validate.
**Notes:** Preserves Approach B's "leave steady-state untouched" win. Flagged for the researcher: the salvage `in.json` claims `schemaRevision: v1alpha2` and SEED-OUTLINE says children are v1alpha2, but R-06 asserts v1alpha1 `Spec.Raw` — must be settled by diffing actual child bytes against the v1alpha2 struct to size the conversion (possibly a no-op).

---

## Stable-identity & trust matching

| Option | Description | Selected |
|--------|-------------|----------|
| FQ name (name + parent chain) + tree validation | Rekey key = object name + full parent chain; validate envelope children against the seed's declared tree (T-308 Kind allowlist) | ✓ |
| Bare object name + uniqueness assertion | Key on `metadata.name` alone; risks cross-object aliasing (dogfood reuses `plan-01-*`/`phase-01-*` across milestones) | |

| Option | Description | Selected |
|--------|-------------|----------|
| Operator-gated + same-ns PVC containment + child validation | 3 layers: spec.importSource (namespace RBAC) + read confined to importSource.pvcSubPath in this namespace's PVC + child Kind-allowlist/name-match/completeness | ✓ |
| Add per-envelope sha256 checksums | Above plus sha256 in the seed; more robust but pulls Phase-29 export tooling forward | (checksum deferred) |

**User's choice:** FQ-name keying + tree validation; operator-gated + same-namespace PVC containment + child validation.
**Notes:** Bare-name keying rejected because the dogfood tree reuses short names across milestones. Per-envelope checksums deferred to Phase 29 export/seed tooling — the completeness check + tree validation cover the immediate threat model.

---

## Claude's Discretion

- Exact `ImportSourceRef` field shape, seed ConfigMap schema, and condition-type naming (within D-02/D-03).
- Whether the rekey table lives in a ConfigMap vs `Project.Status` (CRD-status-only preferred; transient import state).

## Deferred Ideas

- **Hybrid write-side:** planner writes `envelopes/by-name/<fqName>/out.json` going forward so future salvages need no import Job — new capability, own phase/backlog.
- **Per-envelope sha256 checksums in the seed** — belongs with Phase 29 export/seed tooling.
- **Automatic export-on-halt** — existing deferred future requirement; convenience layer atop TOOL-01.
