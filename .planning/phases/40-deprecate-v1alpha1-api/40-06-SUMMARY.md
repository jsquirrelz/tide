---
phase: 40-deprecate-v1alpha1-api
plan: 06
subsystem: docs
tags: [v1alpha3, migration-guide, kustomize, schemaRevision, subagent-levels]

# Dependency graph
requires:
  - phase: 40 (plan 40-03)
    provides: generalized SchemaRevision guard + migrationGuideDocPath constant citing docs/migration/v1alpha2-to-v1alpha3.md
provides:
  - docs/migration/v1alpha2-to-v1alpha3.md — the v1alpha2→v1alpha3 migration chapter (levels-remap table, modelSelection removal, envelope group note, reinstall recipe, fail-closed section, dogfood note)
  - every user-facing Project example (README, INSTALL, gates, git-hosts, project-authoring) on tideproject.k8s/v1alpha3 with schemaRevision: v1alpha3
  - config/samples/tide_v1alpha3_*.yaml (12 files, renamed + content-bumped from tide_v1alpha1_*) + updated kustomization.yaml
  - SECURITY.md / docs/rbac.md with the pre-Phase-23 conversion-webhook claims replaced by the real (retired) posture
affects: [40-07 (terminal grep/closeout), any future v1alpha4 crank's doc sweep]

# Tech tracking
tech-stack:
  added: []
  patterns: ["schemaRevision as the required first spec key on every copy-pasteable Project example", "migration chapter mirrors the prior chapter's section order (What changed and why → Version bump/Reinstall → Fail-Closed Safety Net → Dogfood Note)"]

key-files:
  created: [docs/migration/v1alpha2-to-v1alpha3.md]
  modified: [README.md, SECURITY.md, docs/INSTALL.md, docs/gates.md, docs/git-hosts.md, docs/project-authoring.md, docs/rbac.md, config/samples/kustomization.yaml, "config/samples/tide_v1alpha3_*.yaml (12, renamed from tide_v1alpha1_*)"]

key-decisions:
  - "Migration chapter's levels-remap table sources the DECIDED mapping from the folded todo (.planning/todos/pending/2026-07-03-project-level-subagent-override-slot.md) verbatim, with the fallback note about the old MILESTONE.md dispatch silently falling back to spec.subagent.model"
  - "docs/project-authoring.md field table: replaced the dead spec.modelSelection row (D-10) rather than marking it deprecated-but-present, since v1alpha3's Go type has no such field at all"
  - "docs/rbac.md and SECURITY.md conversion-webhook sections rewritten (not merely trimmed) to state plainly that conversion was retired in Phase 23 and point at docs/migration/ for the real reinstall-only mechanism"

requirements-completed: [CRANK-06]

# Metrics
duration: ~15min
completed: 2026-07-11
---

# Phase 40 Plan 06: Docs, Samples & Migration Guide Summary

**Authored the v1alpha2→v1alpha3 migration chapter with the levels-remap table, landed every user-facing Project example on v1alpha3 + schemaRevision, renamed+bumped all 12 config/samples, and fixed the stale pre-Phase-23 conversion-webhook claims in SECURITY.md/docs/rbac.md.**

## Performance

- **Duration:** ~15 min
- **Tasks:** 3 completed
- **Files modified:** 20 (1 created, 8 modified, 12 renamed+modified — counting each sample individually)

## Accomplishments

- `docs/migration/v1alpha2-to-v1alpha3.md` (185 lines) — mirrors the v1alpha1-to-v1alpha2 template's section order; contains the 4-row levels-remap table, the ModelSelection removal (D-10), the envelope group-decoupling note (D-08), the reinstall procedure (D-03), and the exact `RequiresReinstall` message shape from the 40-03-landed `checkSchemaRevisionGuard` / `migrationGuideDocPath` constant.
- Every copy-pasteable Project example in README.md, docs/INSTALL.md, docs/gates.md, docs/git-hosts.md, and docs/project-authoring.md (3 examples: small/medium/large) now reads `apiVersion: tideproject.k8s/v1alpha3` with `schemaRevision: v1alpha3` as the first spec key.
- docs/project-authoring.md: header re-locked to v1alpha3, api-link references repointed to `api/v1alpha3/project_types.go`, the dead `modelSelection` field row deleted, and the `subagent.levels` row rewritten to the artifact-first semantics (D-02) with a pointer to the new migration guide.
- All 12 `config/samples/tide_v1alpha1_*.yaml` files git-mv'd to `tide_v1alpha3_*`, apiVersion + header comments bumped, `schemaRevision: v1alpha3` added to the project sample only, and `kustomization.yaml`'s resources list + header comment repointed — verified with a live `kubectl kustomize config/samples/` build.
- SECURITY.md and docs/rbac.md: replaced the "conversion webhook (no-op for v1)" / "D-X7 no-op" claims with an accurate description — conversion webhooks were retired in Phase 23, TIDE ships single-version CRDs with reinstall-only migration (D-12).

## Task Commits

1. **Task 1: Author docs/migration/v1alpha2-to-v1alpha3.md** - `5f2d482` (docs)
2. **Task 2: Living docs bump — quickstarts, authoring guide, SECURITY, rbac** - `e0a9148` (docs)
3. **Task 3: Samples — rename all 12, bump contents, update kustomization** - `248140b` (docs)
4. **Fix-up: reword two prose lines to satisfy the phase-end version grep** - `80130cc` (docs)

_No plan-metadata commit — this is a worktree-mode executor; the orchestrator commits shared STATE/ROADMAP after merge._

## Files Created/Modified

- `docs/migration/v1alpha2-to-v1alpha3.md` - new migration chapter (levels-remap, modelSelection removal, envelope note, reinstall recipe, fail-closed section, dogfood note)
- `README.md` - real-Project example bumped to v1alpha3 + schemaRevision
- `docs/INSTALL.md` - "complete real-world Project" example bumped
- `docs/gates.md` - gates example bumped
- `docs/git-hosts.md` - K8s Secret Setup example bumped
- `docs/project-authoring.md` - header, api links, 3 sample walkthroughs, field-reference table (schemaRevision added, modelSelection removed, levels row rewritten), per-level-model-selection prose, RBAC forward-link description
- `SECURITY.md` - CRDs in-scope bullet rewritten (v1alpha3, conversion webhook retired)
- `docs/rbac.md` - scope-of-doc bullet + full "Conversion webhook" section rewritten to state retirement plainly
- `config/samples/kustomization.yaml` - resources list + header comment repointed to `tide_v1alpha3_*`
- `config/samples/tide_v1alpha3_{project,milestone,phase,plan,task_alpha,task_beta,task_delta,task_epsilon,task_eta,task_gamma,task_theta,task_zeta}.yaml` - renamed from `tide_v1alpha1_*`, apiVersion + header comments bumped; project sample additionally carries `schemaRevision: v1alpha3`

## Decisions Made

- Sourced the levels-remap table straight from the folded todo's DECIDED mapping (`.planning/todos/pending/2026-07-03-project-level-subagent-override-slot.md`) rather than re-deriving it, per the plan's explicit read_first instruction.
- Reworded two prose lines (docs/project-authoring.md "carrying forward a v1alpha2 Project's overrides" and docs/rbac.md "v1alpha1→v1alpha2, v1alpha2→v1alpha3") that used bare version tokens outside a "migration"-tagged line, tripping the phase-end verification grep (`grep -v migration | grep -v audit | grep -v superpowers` expects 0 hits). Reworded to prior-schema-revision prose without literal version strings — meaning preserved, verification now clean.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] Reworded two prose lines to pass the plan's own overall verification grep**
- **Found during:** Post-Task-3 verification pass (running the plan's `<verification>` block, not just the per-task automated gates)
- **Issue:** docs/project-authoring.md and docs/rbac.md each had one line using bare `v1alpha1`/`v1alpha2` tokens without the word "migration" on that same physical line — the plan's `grep -rn 'v1alpha1\|v1alpha2' docs/ README.md SECURITY.md | grep -v migration | grep -v audit | grep -v superpowers` verification step matches per-line, so these two non-excluded lines produced 2 hits instead of the required 0.
- **Fix:** Reworded both sentences to describe "prior schema revision" / "every schema crank since" without repeating literal version strings on the flagged line. No meaning lost.
- **Files modified:** docs/project-authoring.md, docs/rbac.md
- **Verification:** Re-ran the exact `<verification>` grep pipeline — 0 hits, exit 1 (no matches).
- **Committed in:** `80130cc`

---

**Total deviations:** 1 auto-fixed (1 bug-class wording fix)
**Impact on plan:** Purely editorial; no scope creep, no functional change. Fixes the plan's own stated overall-verification gate.

## Issues Encountered

None beyond the deviation above.

## User Setup Required

None - no external service configuration required.

## Next Phase Readiness

- 40-07 (terminal grep/closeout) can now assert zero `v1alpha1`/`v1alpha2` hits outside `docs/migration/`, `docs/audit/`, `docs/superpowers/`, and `.planning/` — this plan's own overall-verification grep already confirms that for the files it touched (`docs/`, `README.md`, `SECURITY.md`).
- `config/samples/` is fully renamed + kustomize-buildable against v1alpha3; no dependency on 40-04/40-05's Go-source or Makefile changes to apply cleanly.
- No blockers. Migration guide is in place at the exact path the 40-03-landed `migrationGuideDocPath` constant cites.

---
*Phase: 40-deprecate-v1alpha1-api*
*Completed: 2026-07-11*
