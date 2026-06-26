---
phase: 30-resumable-import-partial-tree-resume-adopt-complete-re-plan
reviewed: 2026-06-26T00:00:00Z
depth: standard
files_reviewed: 11
files_reviewed_list:
  - pkg/dispatch/envelope.go
  - pkg/dispatch/envelope_test.go
  - cmd/tide-import/main.go
  - cmd/tide/export_envelopes_run.go
  - cmd/tide/export_envelopes_test.go
  - internal/controller/project_controller.go
  - internal/controller/project_controller_test.go
  - internal/controller/import_controller_test.go
  - test/integration/kind/import_resume_test.go
  - test/integration/kind/suite_test.go
  - Makefile
findings:
  critical: 0
  warning: 4
  info: 5
  total: 9
status: issues_found
---

# Phase 30: Code Review Report

**Reviewed:** 2026-06-26
**Depth:** standard
**Files Reviewed:** 11
**Status:** issues_found

## Summary

Phase 30 fixes the v1.0.3 partial-tree-resume defect across three plans: (01) a shared
`pkgdispatch.IsEnvelopeComplete` single source of truth + export-time empty-Status gating,
(02) a post-ImportComplete project-planner adoption guard, (03) cross-tier teardown ordering
in the kind suite + Makefile budget bumps.

The core `IsEnvelopeComplete` contract is correct and well-tested. The export `seedStatusFor`
branch correctly distinguishes complete / incomplete / missing / corrupt cases (fail-closed).
The adoption guard is placed correctly (after the IMPORT-01 hold, before the pool acquire) and
the `ImportComplete=True` gating reasoning holds — it cannot fire mid-stream.

No BLOCKER-class defects found. The findings are: an adoption-guard ownership predicate that
trusts `Spec.ProjectRef` instead of an owner reference (cross-project name-collision exposure);
a fragile no-regression test discriminator that cannot distinguish "guard fired" from "dispatch
succeeded"; a partial bypass of the WR-02 malformed-envelope guard on the export path due to
pre-stamping order; and a few maintainability nits.

## Warnings

### WR-01: Adoption guard ownership predicate trusts `Spec.ProjectRef`, not owner reference

**Status: RESOLVED** (commit `8ba705b`)

**File:** `internal/controller/project_controller.go:1109-1120`
**Issue:** The adoption guard lists all Milestones in the namespace and matches on
`msList.Items[i].Spec.ProjectRef == project.Name`. Elsewhere in this same file the
authoritative "is this milestone mine" test uses an owner reference —
`countChildMilestones` (line 953-955) uses `metav1.IsControlledBy(&msList.Items[i], project)`.
`Spec.ProjectRef` is a free-form name string, not a UID-bound owner ref. In a namespace with two
Projects of the same name across recreate cycles, or a stray/leftover Milestone CR whose
`ProjectRef` happens to equal `project.Name` but which is owned by a different (deleted) Project,
the guard would suppress a legitimately-needed project-planner dispatch. The two predicates
should agree; the guard is the weaker of the two and is the one that *suppresses paid dispatch*,
so a false match is a silent "project never bootstraps its milestones" stall.
**Fix applied:** Switched to `metav1.IsControlledBy(&msList.Items[i], project)` (UID-bound),
matching `countChildMilestones`. Confirmed `reconcileCreatingCRs` calls `owner.EnsureOwnerRef`
on every materialized Milestone before `client.Create` (import_controller.go:405), so the owner
ref is present at guard time. Updated Test 1 to set a real controller owner ref; added Test 3
(WR-01 pinning): a foreign-owned Milestone with matching `ProjectRef` must NOT suppress dispatch.

### WR-02: No-regression test discriminator cannot distinguish guard-fired from dispatch-success

**File:** `internal/controller/project_controller_test.go:1326-1334`
**Issue:** Test 2 asserts the guard did NOT fire by checking
`guardFiredEarly := err == nil && result == (ctrl.Result{})`. But the *successful dispatch path*
also returns `ctrl.Result{}, nil` (project_controller.go:1210). The test passes today only
because the minimal reconciler has an empty `CredproxyImage`, so `r.Create(job)` errors at
project_controller.go:1189-1192 and the function returns a non-nil error. The assertion is
therefore coupled to an incidental failure of the dispatch path, not to a positive signal that
dispatch was *attempted*. If Job creation ever stops erroring in this setup (e.g. a future
default image, or fake-client leniency), the test would silently pass while actually exercising
the *success* path — masking a real guard regression.
**Fix:** Assert the positive signal instead of the absence of the guard return. The dispatch path
attempts to create `tide-project-<uid>-1`; the guard does not. Assert that the Job creation was
attempted (or that the function returned the specific dispatch error), e.g.:
```go
// Guard did not fire ⇒ dispatch path ran ⇒ either a job-create error surfaced
// or the planner Job exists. Both prove fall-through.
Expect(err).To(HaveOccurred()) // dispatch path attempted Create and it failed in minimal setup
```
or stamp a valid `CredproxyImage` and assert the `tide-project-<uid>-1` Job now exists.

### WR-03: WR-02 malformed-envelope guard is bypassed on the export path by pre-stamping

**Status: RESOLVED — option (a) chosen** (commit `72c0079`)

**File:** `cmd/tide/export_envelopes_run.go:299-303` (and `seedStatusFor` at 343-359)
**Issue:** `IsEnvelopeComplete`'s strict-equality guard (envelope.go:248-256) is documented as
the invariant that rejects a "ChildCRDs populated but ChildCount==0" malformed envelope (WR-02).
But on the export path, `processEnvelopesTgz` runs `pkgbundle.StampChildCount` on every
`out.json` BEFORE storing it into the `envelopes` map (line 299-303), and `StampChildCount`
repairs exactly that case (`ChildCount==0 && len(ChildCRDs)>0 → ChildCount = len(ChildCRDs)`).
So by the time `seedStatusFor` calls `IsEnvelopeComplete` on the map bytes, a genuinely-truncated
planner output that the WR-02 guard is meant to catch has already been "repaired" to look
complete — and the node will *adopt* its live status rather than being marked re-plannable.
The `tide-import` Job path does NOT pre-stamp, so the guard is meaningful there; the inconsistency
is the concern. A planner that crashed after emitting partial children (real truncation) is
indistinguishable on the export path from a legacy-but-complete envelope.

**Design investigation:** `tide-import` reads raw `out.json` from the old PVC mount
(`/old-workspace/envelopes/<uid>/out.json`) NOT from the bundle's `pvc-envelopes.tgz`.
StampChildCount repairs the bundle copy, but the old PVC still carries raw bytes. If the export
adopted a status on stamped bytes that tide-import rejects on raw bytes, the result is a Plan CR
with `Status=Succeeded` but no Tasks materialized in the new namespace (children never copied).
No legitimate exit-0-unstamped-complete envelopes appear in future exports — the 18 salvage
envelopes (plan 29-04, commit b75c73e) were patched directly on the PVC at that time.

**Fix applied (option a):** `processEnvelopesTgz` now evaluates `IsEnvelopeComplete` on raw
(pre-stamp) bytes and returns a `preStampComplete map[string]bool`. `seedStatusFor` uses the
pre-stamp verdict to match tide-import's completeness check. `StampChildCount` still runs so the
bundle's `pvc-envelopes.tgz` carries corrected bytes. Pinning test:
`TestSeedStatusFor_LegacyUnstampedEnvelopeIsRePlannableOnExport` verifies a legacy exit-0
unstamped envelope (ChildCount==0, len(ChildCRDs)==2) produces `Status=""` in the seed manifest.

### WR-04: `deleteNamespaceAndWait` blocks up to 3 min per namespace with no progress signal on hang

**File:** `test/integration/kind/suite_test.go:1006-1027`
**Issue:** Each long-spec `AfterEach` now calls `deleteNamespaceAndWait`, which blocks via
`Eventually(..., 3*time.Minute, 5*time.Second)` until the namespace is `NotFound`. Three tiers ×
up to two namespaces each = up to ~6 sequential 3-minute waits in the worst (stuck-finalizer)
case before the suite gives up — and the `kubectl delete --timeout=30s` is fire-and-forget
(error ignored), so a namespace wedged on a finalizer surfaces only as the eventual Gomega
timeout with no intermediate diagnostic. The Makefile `KIND_GO_TEST_TIMEOUT` was bumped to 40m
partly to absorb this. The wait is correct in intent (cross-tier contention fix) but the
worst-case cost is large and silent.
**Fix:** Log progress inside the poll (e.g. namespace `Status.Phase`/finalizers every Nth poll)
so a stuck teardown is diagnosable from the test log rather than only as a terminal timeout, and
consider a shorter per-namespace budget (e.g. 90s) given the comment says a healthy cluster
clears in <30s. This is test-infra robustness, not a product defect.

## Info

### IN-01: `seedStatusFor` adopts non-terminal live status (e.g. "Running") for complete-envelope nodes

**File:** `cmd/tide/export_envelopes_run.go:343-359`
**Issue:** When the envelope is complete, the function returns `liveStatus` verbatim — which the
test fixtures show can be `"Running"` (export_envelopes_test.go:631, 708). The import controller
then stamps `Status.Phase="Running"` + `ValidationState="Validated"` (import_controller.go:518-533)
on adoption. This is intentional per the GAP-12 comment (wave materialization re-derives
succession from observed children, so a "Running" adopted plan is not a zombie), but it is a
subtle invariant: adoption preserves whatever non-terminal phase the source cluster happened to
have at export time. Worth a one-line comment in `seedStatusFor` cross-referencing GAP-12 so a
reader does not "fix" it to only adopt terminal statuses (which would break the re-derive path).
**Fix:** Add a comment noting that a non-terminal `liveStatus` is deliberately preserved and
re-armed via `ValidationState=Validated` downstream (GAP-12, import_controller.go:518-533).

### IN-02: Local-variable naming `ns_` breaks Go idiom

**File:** `test/integration/kind/suite_test.go:1022`
**Issue:** `var ns_ corev1.Namespace` uses a trailing-underscore name to avoid shadowing the `ns`
parameter. Go convention prefers a distinct meaningful name (`fetched`, `got`, `nsObj`) over
underscore-suffixing. Minor style nit in test code.
**Fix:** Rename to `var nsObj corev1.Namespace` (or `fetched`).

### IN-03: `reconcileProjectPlannerDispatch` now carries two near-identical `ImportSource != nil` arms

**File:** `internal/controller/project_controller.go:1080-1086` (IMPORT-01 hold) and `1105-1126` (adoption guard)
**Issue:** Two consecutive guard blocks both open with
`if project.Spec.ImportSource != nil { ... FindStatusCondition(ConditionImportComplete) ... }`.
The first parks until ImportComplete=True; the second skips dispatch once it is. They are
logically distinct but textually adjacent and duplicative, raising the function's cyclomatic
complexity over the lint threshold (hence the new `//nolint:gocyclo`). Functionally correct, but
the two arms could be merged into a single `if ImportSource != nil` block with an inner
`switch`/`if-else` on the ImportComplete condition, removing one List/FindStatusCondition pair
and the nolint pressure.
**Fix:** Optional refactor — fold the IMPORT-01 hold and the adoption guard into one
`ImportSource != nil` block to reduce duplication and complexity.

### IN-04: Adoption-guard List error is silently swallowed (intended, but undocumented severity)

**File:** `internal/controller/project_controller.go:1110, 1122-1124`
**Issue:** `if listErr := r.List(...); listErr == nil { ... }` — a List error causes the guard to
do nothing and fall through to dispatch. The comment explains the fall-through is intentional
(an empty/failed import may still need a bootstrap planner). That is a defensible fail-open
choice for a *guard that suppresses dispatch*, but it means a transient apiserver List error
during a legitimate adopted-import reconcile could let a paid project-planner Job slip through
(the exact cost the guard exists to prevent). Because the planner dispatch also flips
Status.Phase=Running and is idempotency-guarded by Job presence on subsequent reconciles, the
blast radius is one spurious Job, not a loop.
**Fix:** Consider returning `ctrl.Result{RequeueAfter: ...}, nil` on List error instead of
falling through, so a transient List failure retries rather than risking a paid dispatch. If the
fall-through is the deliberate choice, the cost ("one paid Job on transient List error") is worth
noting in the comment.

### IN-05: `makeLegacyEnvelopeJSON` child-name generation breaks past 26 children

**File:** `cmd/tide/export_envelopes_test.go:135`
**Issue:** `Name: "child-" + string(rune('a'+i))` produces non-letter runes for `i >= 26` and
collides/garbles names. Harmless in current tests (`numChildren == 3`), but a latent footgun if
a future test raises the count. Test-only.
**Fix:** Use `fmt.Sprintf("child-%d", i)`.

---

_Reviewed: 2026-06-26_
_Reviewer: Claude (gsd-code-reviewer)_
_Depth: standard_
