---
phase: 40-deprecate-v1alpha1-api
reviewed: 2026-07-12T02:26:07Z
depth: standard
files_reviewed: 73
files_reviewed_list:
  - .github/workflows/ci.yaml
  - Makefile
  - PROJECT
  - api/v1alpha3/groupversion_info.go
  - api/v1alpha3/import_types.go
  - api/v1alpha3/milestone_types.go
  - api/v1alpha3/phase_types.go
  - api/v1alpha3/plan_types.go
  - api/v1alpha3/project_types.go
  - api/v1alpha3/schema_test.go
  - api/v1alpha3/shared_types.go
  - api/v1alpha3/task_types.go
  - api/v1alpha3/wave_types.go
  - api/v1alpha3/zz_generated.deepcopy.go
  - charts/tide-crds/templates/milestone-crd.yaml
  - charts/tide-crds/templates/phase-crd.yaml
  - charts/tide-crds/templates/plan-crd.yaml
  - charts/tide-crds/templates/project-crd.yaml
  - charts/tide-crds/templates/task-crd.yaml
  - charts/tide-crds/templates/wave-crd.yaml
  - cmd/dashboard/api/informer_bridge.go
  - cmd/dashboard/api/settings.go
  - cmd/manager/main.go
  - cmd/manager/metrics_test.go
  - cmd/tide-eval/main.go
  - cmd/tide-import/main_test.go
  - cmd/tide-push/main.go
  - config/crd/bases/tideproject.k8s_milestones.yaml
  - config/crd/bases/tideproject.k8s_phases.yaml
  - config/crd/bases/tideproject.k8s_plans.yaml
  - config/crd/bases/tideproject.k8s_projects.yaml
  - config/crd/bases/tideproject.k8s_tasks.yaml
  - config/crd/bases/tideproject.k8s_waves.yaml
  - examples/projects/dogfood/02-codex-runtime-project.yaml
  - examples/projects/dogfood/run-2/RUNBOOK.md
  - examples/projects/small/README.md
  - internal/controller/dispatch_helpers_test.go
  - internal/controller/dispatch_helpers.go
  - internal/controller/milestone_controller.go
  - internal/controller/phase_controller.go
  - internal/controller/plan_controller.go
  - internal/controller/plan_wave_integration_test.go
  - internal/controller/project_boundary_push_test.go
  - internal/controller/project_controller_v2_guard_test.go
  - internal/controller/project_controller.go
  - internal/controller/project_pushresult_test.go
  - internal/controller/task_controller_extracted_test.go
  - internal/controller/task_controller.go
  - internal/credproxy/server.go
  - internal/dispatch/podjob/backend.go
  - internal/dispatch/podjob/jobspec_test.go
  - internal/dispatch/podjob/jobspec.go
  - internal/eval/doc.go
  - internal/eval/render_test.go
  - internal/webhook/v1alpha3/file_touch_utils.go
  - internal/webhook/v1alpha3/plan_webhook.go
  - internal/webhook/v1alpha3/project_webhook.go
  - internal/webhook/v1alpha3/strict_mode.go
  - internal/webhook/v1alpha3/wave_webhook.go
  - krew-plugins/tide.yaml
  - pkg/bundle/seed_test.go
  - pkg/dispatch/childcrd.go
  - pkg/dispatch/doc.go
  - pkg/dispatch/envelope_test.go
  - pkg/dispatch/envelope.go
  - test/e2e/dashboard_test.go
  - test/e2e/gate_flow_test.go
  - test/e2e/testdata/live-claude-project.yaml
  - test/integration/envtest/leak_blocked_test.go
  - test/integration/envtest/planner_dispatch_test.go
  - test/integration/envtest/reporter_materialize_test.go
  - test/integration/kind/baseref_crd_render_test.go
  - test/schema/dogfood_manifests_test.go
findings:
  critical: 0
  warning: 6
  info: 10
  total: 16
status: issues_found
---

# Phase 40: Code Review Report

**Reviewed:** 2026-07-12T02:26:07Z
**Depth:** standard
**Files Reviewed:** 73
**Status:** issues_found

## Summary

Phase 40 is a full API version-lifecycle crank: `api/v1alpha3` introduced as the sole
served+storage version, `api/v1alpha1` + `api/v1alpha2` deleted, the D-02 `subagent.levels`
artifact-first semantic rename implemented via `levelOverrideKey`, the envelope contract
decoupled onto `dispatch.tideproject.k8s/v1alpha1` (D-08), the SchemaRevision fail-closed
guard generalized to a two-constant shape (D-04), and the `verify-no-legacy-api-refs`
regression gate added to Makefile + CI (CRANK-07).

Verification performed during review (Observe First):

- `make verify-no-legacy-api-refs` → PASS on HEAD; `make verify-no-aggregates` → PASS.
- `go build ./...` and `go vet` on api/controller/manager → clean.
- `go test` on `api/v1alpha3`, `pkg/dispatch`, `cmd/tide-import`, `test/schema`,
  `internal/webhook/v1alpha3` → all green.
- Cross-seam checks: the `levelOverrideKey` mapping (project→Milestone, milestone→Phase,
  phase/plan→Plan, task→Task) is applied consistently across `ResolveProvider`,
  `resolveImage`, and the `cmd/manager/env.go` `Models` map keys; dispatch identity (the
  5-valued level string on envelopes and Job labels) is untouched at all five dispatch
  sites; all four cmd binaries (`manager`, `dashboard`, `tide-reporter`, `tide`) register
  only `tidev1alpha3.AddToScheme`; config + chart CRDs are single-version v1alpha3 with
  served+storage; webhook paths (`/validate-tideproject-k8s-v1alpha3-{plan,project,wave}`)
  agree across markers, `config/webhook/manifests.yaml`, and the chart; the guard-test and
  drift-boundary-test sanctioned matches line up with the gate's filters.

No Critical findings. Six Warnings — the most substantive are a fail-open shape in the
generalized schema-revision guard path, a stale public envelope-contract doc that omits
the "project" dispatch level, and a dry-run-vs-import asymmetry for legacy-group
envelopes. Ten Info items. Findings carry confidence tags per the coverage-not-conservatism
instruction; pre-existing defects in reviewed files are flagged as such rather than dropped.

## Warnings

### WR-01: Version-crank guard block is fail-open behind a redundant re-fetch

**File:** `internal/controller/project_controller.go:265-300`
**Severity:** Warning · **Confidence:** High (behavior verified by reading both fetch sites)
**Issue:** Step 4a re-Gets the same `req.NamespacedName` into `v2project` — the identical
GVK the step-1 fetch already returned (pre-crank this was a *different* version's Get, so
the graceful-skip made sense). Post-generalization: (a) the second cache Get is pure
redundancy on every Project reconcile; (b) if that Get errors for any reason, the
`checkSchemaRevisionGuard` fail-closed guard, the DEPS-03 global cycle gate, AND
`deriveGlobalWaves` are all silently skipped, and reconcile proceeds into
`reconcileProjectPhase2(ctx, &project)` — a fail-*open* wrapper around a guard whose whole
point is to fail closed. The comment ("If the Get fails (e.g., a race with deletion), the
guards are skipped gracefully") justifies a condition that no longer exists.
**Fix:** Drop the re-fetch; run the guards on the step-1 object and propagate errors:
```go
// 4a. Version-crank migration guards (SCHEMA-03 + DEPS-03, generalized by D-04).
if blocked, gErr := r.checkSchemaRevisionGuard(ctx, &project); blocked {
    return ctrl.Result{}, gErr
}
depNodes, depEdges, asmTasks, asmErr := r.assembleProjectDepGraph(ctx, &project)
if asmErr != nil {
    return ctrl.Result{}, fmt.Errorf("assemble dep graph for project %s: %w", project.Name, asmErr)
}
// ... cycle gate + deriveGlobalWaves on &project ...
```
(Also retires the misleading `v2project` name.)

### WR-02: Public envelope contract omits the "project" dispatch level

**File:** `pkg/dispatch/envelope.go:61-63`
**Severity:** Warning · **Confidence:** High (production dispatches Level="project" at
`internal/controller/project_controller.go:1719`; in-repo template selection handles it at
`internal/subagent/common/prompt_templates.go:39`)
**Issue:** `EnvelopeIn.Level`'s doc comment — the *public* contract for out-of-tree
subagent-image authors per `pkg/dispatch/doc.go` — documents the domain as
`"milestone" | "phase" | "plan" | "task"`, omitting `"project"`. This phase explicitly
documented the 5-valued set in `internal/dispatch/podjob/jobspec.go:95-103` ("'project'
predates this phase … documented here rather than left out-of-spec") but left the public
surface stale. An external image implementing the documented 4-value switch will
mis-handle every project-level planner dispatch (wrong/missing template selection).
**Fix:**
```go
// Level is the hierarchy level this dispatch operates at:
// "project" | "milestone" | "phase" | "plan" | "task".
// "project" is the Project-CR planner dispatch that authors MILESTONE.md.
```

### WR-03: Legacy-group envelopes — dry-run rejects what tide-import accepts (and import doesn't normalize apiVersion)

**File:** `cmd/tide-import/main.go` (rewrite path; taskUID rewritten, apiVersion not) and
`pkg/bundle/dryrun.go:187`
**Severity:** Warning · **Confidence:** Medium-High (code paths confirmed; operational
impact depends on importing pre-crank salvage bundles)
**Issue:** Two validators for the same artifact now disagree about pre-Phase-40 envelopes
(which carry the old group string `tideproject.k8s/v1alpha1`, deliberately preserved in
the salvage archives the CRANK-07 gate excludes):
- `pkg/bundle/dryrun.go` calls `ValidateAPIVersionKind` against the new constant → a
  legacy bundle now **fails** dry-run with `UnknownAPIVersionError`.
- `cmd/tide-import` never checks apiVersion (only `IsEnvelopeComplete`) → the same
  envelopes **import fine**, and are copied to the new-UID tree with the legacy
  `apiVersion` intact (only `taskUID` is rewritten), leaving mixed-group envelope trees on
  the new PVC that any future apiVersion-validating reader will reject.
**Fix:** During tide-import's atomic `out.json` rewrite, normalize `apiVersion` to
`pkgdispatch.APIVersionV1Alpha1` alongside the taskUID rewrite (it already round-trips the
document); or, if legacy bundles are meant to be refused, add the same
`ValidateAPIVersionKind` check to tide-import so the two surfaces agree. Either way the
dry-run and the Job must give the same answer for the same bundle.

### WR-04: Settings endpoint never serves `spec.git.baseRef` — guard comment is now false

**File:** `cmd/dashboard/api/settings.go:50-58, 160-163`
**Severity:** Warning (pre-existing since Phase 37, retained through this phase's touch of
the file) · **Confidence:** High
**Issue:** `repoSettings.BaseRef` is declared and serialized but never populated; the
comment says "Spec.Git.BaseRef not present in schema at 37-07 execution time … Wire it to
Spec.Git.BaseRef once that field exists." The field exists (Phase 35;
`api/v1alpha3/project_types.go:242`) and survived the v1alpha3 reshape. The settings panel
therefore always renders the HEAD-default label even when an operator pinned a base ref —
incorrect data served to the UI, with the TODO's stated precondition satisfied.
**Fix:** In `writeSettings`, inside the existing `if p.Spec.Git != nil` block:
```go
settings.Repo.BaseRef = p.Spec.Git.BaseRef
```
and delete the stale "not present in schema" comments.

### WR-05: plan-crd chart template carries a mangled conversion-webhook namespace on a single-version CRD

**File:** `charts/tide-crds/templates/plan-crd.yaml:11-22`
**Severity:** Warning (pre-existing — present at the diff base; survives this phase's
`make helm` regeneration, so the regeneration deliverable reproduces it) · **Confidence:**
High for the defect; Medium for near-term impact (dormant while single-version)
**Issue:** Only the Plan CRD template declares `conversion: strategy: Webhook`, and its
`clientConfig.service.namespace` is corrupted by the helmify/augment pass:
```yaml
namespace: '{{ .Release.Namespace }}s{{ .Release.Namespace }}y{{ .Release.Namespace
  }}s{{ .Release.Namespace }}t{{ .Release.Namespace }}e{{ .Release.Namespace
  }}m{{ .Release.Namespace }}'
```
This renders `<ns>s<ns>y<ns>s<ns>t<ns>e<ns>m<ns>` — the letters of "system" interleaved
with the release namespace — pointing at a namespace that cannot exist. It is inert today
only because a single-version CRD never invokes conversion (and the manager registers no
`/convert` handler for these types). The moment a future crank adds a second version, Plan
conversion breaks with a maximally confusing service-resolution error. It is also
inconsistent: the other five CRD templates carry no conversion stanza at all.
**Fix:** Fix the augment source in `hack/helm/` so the stanza is either removed (match the
other five CRDs; nothing serves `/convert`) or renders
`namespace: '{{ .Release.Namespace }}'` — then `make helm` and commit the regenerated
chart (CI's reproducibility gate requires the fix land in the generator, not the output).

### WR-06: CRANK-07 gate sanction filters are basename/line-scoped — quietly bypassable

**File:** `Makefile:616-638` (`verify-no-legacy-api-refs`)
**Severity:** Warning · **Confidence:** High (filter behavior verified by reading the
pipeline; gate verified green on HEAD)
**Issue:** Three robustness gaps in the new regression gate:
1. The sanctioned-file filters match by **basename**, not path:
   `envelope_test\.go:[0-9]*:.*"tideproject\.k8s/v1alpha1"` and
   `project_controller_v2_guard_test\.go:[0-9]*:.*SchemaRevision: "v1alpha2"` whitelist
   those patterns in *any* file with that name anywhere in the tree — a new
   `foo/envelope_test.go` can reintroduce the legacy group string unnoticed, contradicting
   the comment "NOT filtered anywhere else in the tree."
2. Filtering is **line-based**: `grep -v "dispatch\.tideproject\.k8s/v1alpha1"` drops the
   whole line, so a line containing both the sanctioned dispatch-group string and a bare
   legacy token passes.
3. The directory exclusions `**/migration/**`, `**/audit/**`, `**/superpowers/**` match
   those directory names **anywhere**, not just under `docs/` as the comment states — a
   future `internal/migration/` Go package would be silently exempt from the gate.
**Fix:** Anchor the sanctioned files by path (`pkg/dispatch/envelope_test\.go:` /
`internal/controller/project_controller_v2_guard_test\.go:`), and scope the doc
exclusions to `docs/migration/**`, `docs/audit/**`, `docs/superpowers/**`. The line-based
smuggling gap is inherent to grep filtering — acceptable residual risk once 1 and 2 are
anchored, but worth a comment acknowledging it.

## Info

### IN-01: `verify-no-aggregates` "gate misconfigured" branch is unreachable under `set -e`

**File:** `Makefile:563-577`
**Confidence:** High (verified empirically: `bash -ec 'FILES=$(ls no-match*/ 2>/dev/null); …'`
exits at the assignment; the `-z` message never prints)
**Issue:** The D-12 hardening (`if [ -z "$$FILES" ]; then echo "gate misconfigured"…`) can
never print: with `.SHELLFLAGS = -ec`, the failed `ls` command substitution aborts the
recipe (stderr suppressed) before the emptiness check. The gate still fails closed on a
dead glob — the intended safety holds — but silently, with zero diagnostic output in CI.
**Fix:** `FILES=$$(ls api/v1alpha*/*_types.go 2>/dev/null || true);` keeps the friendly
message reachable.

### IN-02: `checkSchemaRevisionGuard` doc comment describes a 3-value return; the signature has 2

**File:** `internal/controller/project_controller.go:2249-2259`
**Confidence:** High
**Issue:** "Returns (true, ctrl.Result{}, reconcile.TerminalError(...))" / "Returns
(false, ctrl.Result{}, nil)" — the function returns `(blocked bool, err error)`. Stale
from the pre-generalization shape.
**Fix:** Update the comment to the two-value contract.

### IN-03: UTF-8 mojibake in dispatch_helpers.go comments

**File:** `internal/controller/dispatch_helpers.go:17-36, 221-230` (13 occurrences)
**Confidence:** High (pre-existing — the base had 18 occurrences; this phase removed 5 and
kept 13)
**Issue:** Em-dashes/arrows render as `â` (e.g. "dispatch_helpers.go consolidates the
three planner dispatch helpers … â each reconciler is ~80-150 LOC"). Comment-only, but
this is the file carrying the phase's flagship `levelOverrideKey` documentation.
**Fix:** Re-encode the affected comments as UTF-8 em-dashes/arrows.

### IN-04: informer_bridge.go header comment contradicts the codebase

**File:** `cmd/dashboard/api/informer_bridge.go:30-31, 186-189`
**Confidence:** High
**Issue:** "no OwnerRefs are stamped today — internal/controller/* uses
Spec.{…}Ref" — controllers stamp owner refs at every level via `owner.EnsureOwnerRef`
(e.g. `milestone_controller.go:172`). The Spec-ref resolution chain is still a fine
design choice; the justification is stale and was carried through this phase's edit.
**Fix:** Reword to "resolution follows Spec refs (owner-ref walking is avoided here by
design)".

### IN-05: `hasTopLevelKey` matches keys at any indentation, contradicting its name and doc

**File:** `test/schema/dogfood_manifests_test.go:100-111`
**Confidence:** High
**Issue:** The doc says "top-level key (i.e., at the start of a line with no leading
spaces)" but the implementation `bytes.TrimLeft(line, " \t")` + `HasPrefix` matches the
key at *any* indentation. For the inline-secret check this is conservatively safe (it
catches nested `data:`/`stringData:` too), but it will false-positive on any legitimate
nested `data:` map in a future dogfood doc, and the name misleads maintainers.
**Fix:** Rename to `hasKeyAtAnyIndent` (and fix the doc), or drop the `TrimLeft` to match
the documented behavior — pick whichever contract the secret gate actually wants.

### IN-06: PROJECT scaffold omits the Project validating webhook

**File:** `PROJECT:12-20`
**Confidence:** Medium (scaffolding metadata only; no runtime effect)
**Issue:** The regenerated PROJECT declares `webhooks: validation: true` for Plan and Wave
but not for Project, although a Project validating webhook exists and is registered
(`internal/webhook/v1alpha3/project_webhook.go`, `cmd/manager/main.go:605`). Future
`kubebuilder` scaffolding operations will treat Project as webhook-less.
**Fix:** Add the `webhooks: {validation: true, webhookVersion: v1}` block to the Project
resource entry.

### IN-07: Envelope-group crank fail-closes older pinned subagent images (documented, but sharp)

**File:** `pkg/dispatch/envelope.go:30` (contract change), `docs/migration/v1alpha2-to-v1alpha3.md:55-61`
**Confidence:** High for mechanism; informational because the break is a locked decision
(D-08) with a drift-boundary test
**Issue:** Any subagent image compiled before this phase validates `in.json` against the
old group string; a `Project.Spec.Subagent.Image` (or per-level image) pinning such an
image now fails every dispatch in-pod with `UnknownAPIVersionError`. The chart-default
image rolls with the release, and the migration doc covers rebuilds — but the failure
surfaces as a per-task pod error, not at admission.
**Fix (optional):** None required for correctness. A release-notes callout naming the
exact in-pod error string would shorten operator diagnosis.

### IN-08: `BuildPlannerEnvelope` `attempt` parameter is dead

**File:** `internal/controller/dispatch_helpers.go:277`
**Confidence:** High (pre-existing)
**Issue:** `attempt int` is accepted at all five call sites but never used in the function
body (EnvelopeIn carries no attempt field). Dead parameter on a shared helper.
**Fix:** Remove the parameter or wire it into the envelope if per-attempt context is
intended.

### IN-09: Hand-rolled `strings.Contains` in test helper

**File:** `internal/controller/dispatch_helpers_test.go:396-408`
**Confidence:** High
**Issue:** `contains`/`containsHelper` reimplement `strings.Contains` "to avoid importing
strings" — the import costs nothing and the hand-rolled version's
`len(s) > 0 && containsHelper(...)` guard is redundant with its own bounds.
**Fix:** `import "strings"` and use `strings.Contains`.

### IN-10: Default-tier consequence of the D-02 remap + stale line citation in the dogfood sample

**File:** `examples/projects/dogfood/02-codex-runtime-project.yaml:16, 238-247`
**Confidence:** Medium
**Issue:** Two small operator-facing accuracy notes: (a) the comment cites
`dispatch_helpers.go:179, ResolveProvider` — `ResolveProvider` now starts at `:190`
(line-number citations in long-lived sample comments rot; the symbol name alone is
durable); (b) one consequence of the decided remap worth a conscious eye: with unchanged
chart model defaults, the Milestone-CR dispatch (authors phase briefs) now resolves the
`phase` default tier (sonnet) where it previously resolved the `milestone` tier (opus) —
correct under artifact-first semantics, but it silently changes which model authors phase
briefs on default installs. The sample's own per-level overrides are self-consistent.
**Fix:** Drop the `:179` line number; optionally note the default-tier shift in the
migration doc if chart defaults were not re-tuned alongside the rename.

---

_Reviewed: 2026-07-12T02:26:07Z_
_Reviewer: Claude (gsd-code-reviewer)_
_Depth: standard_
