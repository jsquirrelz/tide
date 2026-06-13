# Phase 17: Address tech debt — Plan label backfill + gate hardening - Research

**Researched:** 2026-06-12
**Domain:** Go + sigs.k8s.io/controller-runtime reconciler hardening; Ginkgo/envtest regression coverage
**Confidence:** HIGH (all claims verified against live code in this session via grep + read)

## Summary

This phase has no CONTEXT.md and the ROADMAP goal is "[To be planned]". Its scope is the v1.0.1 milestone audit's `tech_debt` list (`.planning/v1.0.1-MILESTONE-AUDIT.md` frontmatter). The phase title — "Plan label backfill + gate hardening" — names the two in-scope cores: the **Plan-level project-label backfill gap** (the audit's only *new* finding) and the **WR-* gate-semantics items** carried over from Phase 12.

I verified every file:line pointer the audit cites against the live tree. **All of them resolve to real code**, though several line numbers drifted by a few lines and two pointers (WR-10's `phase_controller.go:424`) point at a *related* site rather than the precise bug surface. Most consequentially, my verification surfaced that **WR-10 is a symmetric milestone+phase bug, not just a phase bug**, and that the plan controller's 12-05 fix is the exact template for both. It also confirmed **CR-01's parity-fix shape** by reading the divergent error handling in all three planner-completion handlers.

**Primary recommendation:** Scope this phase to **five reconciler/CLI edits, each with one envtest (or Go-unit) regression spec**, all of which mirror an already-shipped sibling pattern in this same codebase:

| # | Item | Fix shape | In/Defer |
|---|------|-----------|----------|
| 1 | **Plan label backfill** (headline) | Copy the milestone/phase backfill block into `PlanReconciler.Reconcile` | **IN** |
| 2 | **WR-10** reject-after-reporter-spawn | Move `CheckRejected` to top of `handleJobCompletion` in milestone + phase (mirror plan 12-05) | **IN** |
| 3 | **WR-06** D-07 guard over-blocks | Narrow `findFailedLevel` scope; `--wave` path already skips it (decide intended semantics) | **IN** |
| 4 | **WR-01** `checkParentApproval` fails open on NotFound | Bounded by informer lag; assess — likely **DEFER** (documented design choice) | **DEFER (lean)** |
| 5 | **CR-01** plan envelope-read → terminal Failed | Change to non-fatal requeue, mirror milestone/phase Pitfall-1 | **IN** |
| 6 | **15 WR-03** Project→Milestone reporter-edge label | One-line `*Project` special-case + test row | **IN (cheap)** |

Everything else in the audit's `tech_debt` block (13 misc WR/IN robustness notes, 16 UX-polish) → **DEFER** to the docs/audit hardening backlog, consistent with REQUIREMENTS.md "Out of Scope" line for the 27-item backlog.

## Architectural Responsibility Map

| Capability | Primary Tier | Secondary Tier | Rationale |
|------------|-------------|----------------|-----------|
| Project-label backfill on upgrade | Controller (PlanReconciler) | — | Self-healing label is a reconciler concern; mirrors milestone/phase backfill |
| Reject short-circuit ordering | Controller (Milestone/Phase reconcilers) | — | Gate enforcement lives in reconcilers, never the CLI |
| Approve-gate failed-level guard | CLI (`cmd/tide/approve.go`) | — | D-07 guard is a CLI UX gate; controller owns actual park/dispatch |
| Envelope-read error recovery | Controller (PlanReconciler) | — | Transient PVC/read error is a requeue concern, not terminal state |

## Standard Stack

No new dependencies. This phase is reconciler edits + tests inside the existing module.

### Core (already in tree — verified via go.mod + Makefile)
| Library | Version | Purpose | Why Standard |
|---------|---------|---------|--------------|
| sigs.k8s.io/controller-runtime | v0.24.x | Reconciler framework | TIDE's controller substrate [CITED: CLAUDE.md STACK] |
| Ginkgo / Gomega | v2.28 | envtest specs | Existing backfill/gate specs use it [VERIFIED: grep _test.go] |
| envtest | (controller-runtime bundled) | API-server test harness | Backfill regression tests run on it [VERIFIED: Makefile:85] |

### Package Legitimacy Audit

Not applicable — **zero external packages installed**. All work is edits to existing files in `github.com/jsquirrelz/tide` [VERIFIED: go.mod]. The Package Legitimacy Gate is satisfied vacuously.

## Verified Code-Path Inventory

Every audit pointer, checked against the live tree (the "Observe First" / grep-to-confirm rule):

| Audit claim | Cited loc | Live loc | Verdict |
|-------------|-----------|----------|---------|
| Phase backfill block to copy | `phase_controller.go:168-183` | `phase_controller.go:168-186` | ✅ EXISTS (label-guard 174, patch+err to 185) |
| Milestone backfill block (symmetry) | (implied) | `milestone_controller.go:178-194` | ✅ EXISTS — `resolveProjectNameForMilestone` (1 Get) |
| Plan owner-chain resolver already present | `plan_controller.go:1409-1417` | `plan_controller.go:1415-1425` (`resolveProjectName`); chain walk `resolveProjectForPlan:829` | ✅ EXISTS — fast-path + Plan→Phase→Milestone→Project walk |
| Plan reconcile LACKS backfill | (the gap) | `plan_controller.go:127-203` | ✅ CONFIRMED GAP — no label-absent block between step 4 (ownerref :154-173) and step 5 (dispatch :179) |
| WR-01 `checkParentApproval` fails open on NotFound | `dispatch_helpers.go:278-293` | `dispatch_helpers.go:304-329` (body); doc :287-303 | ✅ EXISTS — `client.IgnoreNotFound(err)` → returns `(false, nil)` on NotFound at :312/:318/:324 |
| WR-10 phase reject short-circuit after reporter spawn | `phase_controller.go:424` | reporter spawn `:483`; reject check `:510` | ⚠️ REFRAMED — `:424` is the *planner* Job Create (dispatch path, correctly gated at :336). True bug: reporter spawn `:483` precedes `CheckRejected` `:510` in `handleJobCompletion` |
| WR-10 plan instance fixed in 12-05 | (claim) | `plan_controller.go:466-472` "reject short-circuit FIRST" before reporter spawn `:507` | ✅ CONFIRMED FIXED in plan; ❌ NOT fixed in milestone (`:515` after `:556`) or phase (`:510` after `:483`) |
| WR-06 D-07 approve guard project-wide | `cmd/tide/approve.go:152-164` | `cmd/tide/approve.go:152-163` (guard); `findFailedLevel:194` scans ALL levels | ✅ EXISTS — guard in `approveLevel` |
| WR-06 `--wave` path skips the guard | (claim) | `approveRun:71-73` returns `approveWave` BEFORE `approveLevel`'s guard | ✅ CONFIRMED — `--wave` branch never calls `findFailedLevel` |
| CR-01 plan envelope-read → terminal Failed | "phase 03-08 origin" | `plan_controller.go:491-504` sets `Status.Phase="Failed"` on `ReadOut` err | ✅ CONFIRMED — vs milestone `:535-539` / phase `:468-473` non-fatal log+defer |
| 15 WR-03 Project→Milestone reporter-edge label | (claim) | `StampProjectLabel` `internal/owner/label.go:45`; `LabelProject` const `:33` | ✅ EXISTS — backfill (D-03) now heals the symptom; create-site special-case is the belt-and-suspenders |

## In-Scope Items — Detailed Fix Shapes & Acceptance

### Item 1 — Plan-level label backfill (HEADLINE) — IN

**What goes wrong:** Milestones backfill `tideproject.k8s/project` in-reconciler (`milestone_controller.go:178-194`), Phases do the same (`phase_controller.go:168-186`), and Tasks get `stampTaskLabels` (`plan_controller.go:1372`). But **Plans rely solely on create-site label inheritance** — `PlanReconciler.Reconcile` (`:127-203`) has NO label-absent backfill block. A pre-v1.0.1 Plan CR on an upgraded cluster stays unlabeled → invisible to `tide approve`'s `findAwaitingPlan` label selector and `tide resume --retry-failed`'s scans (which filter by `Labels["tideproject.k8s/project"] == projectName`, e.g. `approve.go:227-229`).

**Why it happens:** D-01/D-03 (CUTS-01, Phase 15) added backfill to milestone + phase reconcilers but not the plan reconciler. Upgrade-path-only: fresh installs get the label at create-site via `StampProjectLabel`.

**Fix shape (verified template exists):** Insert a backfill block into `PlanReconciler.Reconcile` between step 4 (owner-ref, ends `:173`) and step 5 (dispatcher, `:179`), mirroring `phase_controller.go:174-186`:
```go
// 4b. D-03 (CUTS-01): backfill tideproject.k8s/project on the Plan itself
// when absent. Heals pre-Phase-15 Plan CRs on upgraded clusters.
if plan.Labels[owner.LabelProject] == "" {
    if name, err := r.resolveProjectName(ctx, &plan); err == nil && name != "" {
        patch := client.MergeFrom(plan.DeepCopy())
        if plan.Labels == nil { plan.Labels = map[string]string{} }
        plan.Labels[owner.LabelProject] = name
        if err := r.Patch(ctx, &plan, patch); err != nil {
            return ctrl.Result{}, fmt.Errorf("backfill project label on plan %s: %w", plan.Name, err)
        }
    }
}
```
Note: `resolveProjectName` (`:1415`) already does fast-path + owner-chain walk and returns `ErrParentUnresolved` on miss — handle that as "skip silently" (the milestone/phase variants use a `""`-returning helper; the plan variant returns an error, so treat `err != nil` as skip). **Run BEFORE dispatch** so a parked-AwaitingApproval Plan self-heals on its first post-upgrade reconcile (same rationale as `phase_controller.go:172`).

**Acceptance (mirror `phase_controller_test.go:270-334`):** A new envtest spec `PlanReconciler — D-03 project-label backfill`:
1. Create Project + Milestone + Phase (with refs) + Plan **without** the `tideproject.k8s/project` label, with `Spec.PhaseRef` set so the Plan→Phase→Milestone→Project chain is traversable.
2. Reconcile (no Dispatcher → drives steps 1-5).
3. Assert `after.Labels["tideproject.k8s/project"] == projName`.
4. Idempotency: record `ResourceVersion`, reconcile again, assert unchanged.

### Item 2 — WR-10: reject short-circuit fires after reporter-Job spawn — IN

**What goes wrong:** In `PhaseReconciler.handleJobCompletion`, `spawnReporterIfNeeded` (`:483`) runs BEFORE the `gates.CheckRejected` short-circuit (`:510`). The **same ordering bug exists in milestone** (`spawnReporterIfNeeded:556` before `CheckRejected:515`). So when a rejected Project's planner Job completes, a NEW reporter Job is still spawned at the phase and milestone levels before the reject park fires — wasted dispatch, contradicts D-05 "rejected Project halts NEW dispatch."

**Why it happens:** The plan controller's 12-05 fix moved its reject check to the top of `handlePlannerJobCompletion` ("reject short-circuit FIRST", `:466-472`, before reporter spawn `:507`) but the milestone/phase handlers were not given the same treatment. (Note the dispatch-*entry* path IS correctly gated in all three — phase `:336`, milestone `:338`, plan `:313`. The bug is only in the *completion* handlers.)

**Fix shape:** Move the `if project != nil && gates.CheckRejected(project) { return r.patch<Level>Rejected(...) }` block to the **first statement after `project := r.resolveProject(...)`** in `MilestoneReconciler.handleJobCompletion` and `PhaseReconciler.handleJobCompletion`, ahead of `spawnReporterIfNeeded`. Verbatim copy of the plan template (`plan_controller.go:466-472`).

**Acceptance:** Two new envtest specs (milestone + phase): with a Project carrying the reject annotation (`gates.CheckRejected` true), drive `handleJobCompletion` and assert (a) the level is parked Rejected, and (b) **no `tide-reporter-<uid>` Job exists** in the namespace. The "no reporter Job created" assertion is the load-bearing one. No existing milestone/phase reject-after-spawn test was found (grep returned none), confirming this is net-new coverage.

### Item 3 — WR-06: D-07 approve guard over-blocks project-wide — IN (needs a semantics decision)

**What goes wrong:** `findFailedLevel` (`approve.go:194`) scans **all four level kinds across the whole project** and returns the first `Status.Phase=="Failed"`. `approveLevel` (`:155-163`) refuses ANY approval if any one level is Failed. This blocks an operator from approving an unrelated, healthy AwaitingApproval level just because some sibling somewhere failed — contradicting the strict failure profile (siblings independent; a failed task should not block approval of an unrelated parked level). Meanwhile the `--wave` path (`approveRun:71-73`) returns `approveWave` directly and **never calls the guard at all** — an inconsistency.

**Why it happens:** D-07 (Phase 12) intentionally made `tide approve` refuse to double as a spend-retry. The guard was scoped project-wide for simplicity; the `--wave` path was added later without re-applying it.

**Fix shape (decision point for the planner/discuss):** Two coherent options —
- **(A) Narrow the guard to the approval target:** only refuse if *the level being approved* (the one `findAwaiting*` would return) is itself Failed. Keeps D-07 intent, removes project-wide over-block. This is the recommended root-cause fix [per memory: fix-thoroughly-on-TIDE].
- **(B) Keep project-wide but also apply it to `--wave`:** consistency at the cost of the over-block. Weaker.

`[ASSUMED]` which semantics the operator wants — this is the one item with a genuine design fork. Recommend surfacing in discuss-phase. Default lean: **(A)**.

**Acceptance:** Go-unit test on `approveLevel` (these are plain table tests in `cmd/tide/approve_test.go`-style, fake client): assert that with one Failed Plan AND one healthy AwaitingApproval Phase, approving succeeds against the Phase (option A) rather than erroring. Plus a `--wave` row asserting chosen semantics.

### Item 4 — WR-01: `checkParentApproval` fails open on parent NotFound — DEFER (lean)

**What goes wrong:** `checkParentApproval` (`dispatch_helpers.go:304-329`) returns `(false, nil)` when the parent Get hits NotFound (`client.IgnoreNotFound` at `:312/:318/:324`) → the child proceeds to dispatch even though its parent might be parked (it just isn't visible yet).

**Why it's likely DEFER:** The doc-comment (`:293-295`) explicitly frames this as a **bounded, intentional choice**: "NotFound is transient informer lag; callers continue dispatch and the next reconcile re-checks." The window is one informer-sync cycle, and the parent-park is re-checked on the next reconcile. A genuinely-deleted parent means the child is an orphan that won't dispatch usefully anyway. The audit itself marks this "informer-lag-bounded; tracked in docs/audit backlog."

**Recommendation:** DEFER unless the planner wants a belt-and-suspenders requeue-on-NotFound (return `(false, requeueErr)` to retry rather than dispatch). If included, it's a one-line change + one spec asserting a child holds dispatch for one cycle when the parent Get transiently 404s. Low value; default DEFER.

### Item 5 — CR-01: plan envelope-read transient error → terminal Failed — IN

**What goes wrong:** `handlePlannerJobCompletion` (`plan_controller.go:491-504`) sets `Status.Phase="Failed"` + `ConditionFailed{Reason: EnvelopeReadFailed}` whenever `EnvReader.ReadOut` errors. A transient PVC/read error wedges the Plan terminally. The milestone (`:535-539`) and phase (`:468-473`) handlers treat the same error **non-fatally** (Pitfall 1: log + defer to children-based succession).

**Fix shape:** Replace the terminal-Failed branch with the milestone/phase pattern — log the read error non-fatally, set an `envReadOK=false` sentinel, and defer succession to the children-based fallback (`hasChildPlans`/`hasChildTasks`). Use `envReaderPresent`/`envReadOK` sentinels exactly as milestone does (`:530-545`) to distinguish "no reader" from "read error."

**Acceptance:** envtest spec: with an `EnvReader` stub that returns an error for the Plan's UID, drive `handlePlannerJobCompletion` and assert the Plan is **not** `Status.Phase=="Failed"` (it requeues / defers) — mirrors the milestone Pitfall-1 spec shape.

### Item 6 — 15 WR-03: Project→Milestone reporter-edge label stamp — IN (cheap)

**What goes wrong:** The create-site project-label stamp is dead for the Project→Milestone reporter edge (the reporter creating a Milestone has no project-name to stamp via the normal child path). The D-03 milestone backfill (`:178-194`) now heals the *symptom*, but the create-site remains a no-op for this one edge.

**Fix shape:** One-line `*Project` special-case at the reporter Milestone-create site so the label is stamped at creation (defense-in-depth alongside backfill) + a Project-parent test row. Audit explicitly recommends "one-line *Project special-case + Project-parent test row."

**Acceptance:** Extend the existing milestone backfill/stamp test with a Project-parent row asserting the label is present at create time (not only after backfill reconcile).

## Don't Hand-Roll

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|-------------|-----|
| Project-name resolution for Plan | New chain-walker | `resolveProjectName` / `resolveProjectForPlan` (`plan_controller.go:1415` / `:829`) | Already does fast-path + owner-chain walk + `ErrParentUnresolved` |
| Label constant | String literal `"tideproject.k8s/project"` | `owner.LabelProject` (`internal/owner/label.go:33`) | Avoids label-literal drift (itself a tracked WR item) |
| Create-site label stamp | Inline label map mutation | `owner.StampProjectLabel` (`label.go:45`) | Canonical, tested, idempotent |
| Idempotent label patch | Read-modify-write loop | `client.MergeFrom` + label-absent guard | The milestone/phase backfill already proves the idempotent pattern |

**Key insight:** Every in-scope fix has an already-shipped sibling in this same codebase. The phase is "make Plan-level + the two completion handlers consistent with the patterns milestone/phase/12-05 already established," not greenfield design.

## Common Pitfalls

### Pitfall 1: Plan resolver returns an error, not "" — handle the mismatch
**What goes wrong:** Milestone/phase backfill helpers return `""` on miss; the Plan's `resolveProjectName` returns `ErrParentUnresolved`. Copy-pasting the milestone block verbatim will compile-fail or mis-handle the orphan case.
**How to avoid:** Treat `err != nil || name == ""` as "skip backfill silently" in the Plan block.

### Pitfall 2: Backfill must run BEFORE dispatch, after owner-ref
**What goes wrong:** Placing the backfill after the dispatcher branch means a parked Plan never self-heals (the dispatch path returns first).
**How to avoid:** Insert between step 4 (owner-ref) and step 5 (dispatcher) — exactly where milestone/phase put it (`milestone:178`, `phase:168`). The owner-ref `Update` at `:169` may bump ResourceVersion; the backfill `Patch` is a separate write — fine, both are idempotent on second reconcile.

### Pitfall 3: WR-10 fix must NOT delete in-flight reporter Jobs
**What goes wrong:** Over-correcting by deleting already-spawned reporter Jobs on reject. D-05 is explicit: "in-flight Jobs drain (no Job deletion)."
**How to avoid:** The fix only *prevents a new* reporter spawn by reordering the check. It must not add Job deletion. Assert "no NEW reporter Job created when already rejected," not "reporter Job deleted."

### Pitfall 4: `make test-int` green ≠ ship-ready
**What goes wrong [CITED: CLAUDE.md "Verify Before Claiming"]:** Plain go-tests bundled in the integration package fail the package even when Ginkgo prints SUCCESS. A dropped assertion can ship green.
**How to avoid:** Read `MAKE_EXIT` AND grep `^--- FAIL|^FAIL\s` in the log, not just the Ginkgo summary line.

## Runtime State Inventory

This is a code-edit phase (reconciler logic + tests). The *purpose* of Item 1 is itself to heal stored runtime state, so the inventory is the subject, not a side-effect:

| Category | Items Found | Action Required |
|----------|-------------|------------------|
| Stored data | Pre-v1.0.1 **Plan CRs** on upgraded clusters missing `tideproject.k8s/project` label (the headline gap) | Item 1 backfill heals on next reconcile — no manual data migration; reconcile is the migration |
| Live service config | None — TIDE state lives in CRD `.status` only (no external DB per CLAUDE.md) | None |
| OS-registered state | None | None — no OS registrations involved |
| Secrets/env vars | None | None — no secret/env names change |
| Build artifacts | None — no package rename, no pyproject/egg-info; Go module path unchanged | `make build` / image rebuild on merge (standard) |

**Note:** Because the backfill runs in-reconciler, the "migration" is automatic on controller upgrade — no `kubectl patch` recipe needed. This is the whole point of the in-reconciler backfill pattern (vs. a one-shot migration Job).

## Validation Architecture

### Test Framework
| Property | Value |
|----------|-------|
| Framework | Ginkgo v2.28 + Gomega + envtest (controller-runtime bundled) [VERIFIED: Makefile, _test.go] |
| Config file | none — Ginkgo bootstrap in `internal/controller/suite_test.go` |
| Quick run command | `make test` (unit tier, `-short`, 120s timeout, envtest) [VERIFIED: Makefile:85-86] |
| Full suite command | `make test-int` (Layer A envtest + Layer B kind) [VERIFIED: Makefile:119] |

### Phase Requirements → Test Map
Requirement IDs for this phase are TBD (the planner will mint them — suggest `BACKFILL-01`, `GATE-05`/`GATE-06`, `ROBUST-01`). Behavior→test map:

| Behavior | Test Type | Automated Command | File |
|----------|-----------|-------------------|------|
| Plan backfill stamps label from owner chain + idempotent | unit (envtest) | `make test` (focus `PlanReconciler — D-03`) | `internal/controller/plan_controller_test.go` (new spec) |
| Reject before reporter spawn — no new reporter Job (milestone) | unit (envtest) | `make test` | `internal/controller/milestone_controller_test.go` (new) |
| Reject before reporter spawn — no new reporter Job (phase) | unit (envtest) | `make test` | `internal/controller/phase_controller_test.go` (new) |
| Approve guard narrowed — healthy level approvable despite unrelated Failed | unit (fake client) | `go test ./cmd/tide/...` | `cmd/tide/approve_test.go` (new row) |
| Plan envelope-read error → non-fatal (not terminal Failed) | unit (envtest) | `make test` | `internal/controller/plan_controller_test.go` (new) |
| Project→Milestone reporter edge stamps label at create | unit (envtest) | `make test` | `internal/controller/milestone_controller_test.go` (extend) |

### Sampling Rate
- **Per task commit:** `make test` (unit/envtest tier — covers every spec above; ~fast, 120s budget)
- **Per wave merge:** `make test-int-fast` (Layer A envtest, no Docker) then `make test-int` if a Layer B seam is touched (none expected — these are controller-internal edits)
- **Phase gate:** full `make test-int` green (read `MAKE_EXIT` + grep `FAIL`, per Pitfall 4) before `/gsd:verify-work`

### Wave 0 Gaps
- None for framework — backfill/reject/approve specs all have existing sibling patterns to mirror (`phase_controller_test.go:270`, `cmd/tide/approve_test.go`).
- New spec files NOT needed; all specs extend existing `*_test.go` files in `internal/controller/` and `cmd/tide/`.

## Security Domain

`security_enforcement` not set in config.json → treat as enabled. Applicability is **minimal** — this phase touches no auth, crypto, session, or input-validation surface.

| ASVS Category | Applies | Standard Control |
|---------------|---------|-----------------|
| V2 Authentication | no | — (no auth code touched) |
| V4 Access Control | marginal | The Plan-label backfill *restores* RBAC-relevant label-scoping for `tide approve`/`resume` discovery — a healed object becomes visible to the correct project scope. No new access surface. |
| V5 Input Validation | no | No new external input; `findFailedLevel`/`approve` read existing CRD status |
| V6 Cryptography | no | — |

| Pattern | STRIDE | Mitigation |
|---------|--------|------------|
| Backfill mis-resolves project (multi-Project namespace) | Spoofing/Tampering | `resolveProjectForPlan` already removed the `Items[0]` fallback that mis-routed in multi-Project namespaces (`plan_controller.go:1413`); backfill inherits that safety — **verify the chosen resolver is `resolveProjectName`, not a List-first variant** |
| Reject bypass via reporter spawn | Elevation (spend) | WR-10 fix closes the wasted-dispatch path; assert no reporter Job on reject |

## Assumptions Log

| # | Claim | Section | Risk if Wrong |
|---|-------|---------|---------------|
| A1 | WR-06 operator wants the *narrowed* guard (option A) over project-wide consistency (option B) | Item 3 | Wrong semantics ships; surfacing in discuss-phase mitigates. The fork is real — both are defensible. |
| A2 | WR-01 is intended/acceptable (DEFER) | Item 4 | If the informer-lag window is wider than assumed on a loaded cluster, a child could dispatch under a parked parent. Doc-comment frames it as bounded; low risk. |
| A3 | No Layer-B (kind) coverage needed — all fixes are controller-internal | Validation Architecture | If a fix interacts with the real reporter Job lifecycle in-cluster, an envtest-only gate could miss it. WR-10's "no Job created" assertion is envtest-checkable, mitigating. |

## Open Questions (RESOLVED)

1. **WR-06 semantics fork (A vs B).** RESOLVED: Option A (narrow guard) — only refuse approval when the approval target itself is Failed. See Item 3 / Assumption A1. Default lean confirmed downstream per fix-thoroughly preference.
2. **Should WR-01 be included as a hardening freebie?** RESOLVED: DEFER — informer-lag-bounded, documented design choice; tracked in the docs/audit hardening backlog. Not in this phase's scope.
3. **Requirement IDs.** RESOLVED: DEBT-01..04 minted (DEBT-01 = Item 1 backfill + WR-03 fold; DEBT-02 = WR-10 reject reorder; DEBT-03 = WR-06 guard narrow; DEBT-04 = CR-01 envelope-read non-fatal). REQUIREMENTS.md traceability updated.

## Environment Availability

| Dependency | Required By | Available | Version | Fallback |
|------------|------------|-----------|---------|----------|
| Go toolchain | build/test | assumed ✓ (user installs via brew) | 1.26.0 [VERIFIED: go.mod] | — |
| setup-envtest | `make test` | ✓ (Makefile target) | controller-runtime release-pinned | — |
| Docker + kind | `make test-int` Layer B only | host-dependent | — | `make test-int-fast` (Layer A envtest, no Docker) covers all this phase's specs |

No missing dependency blocks this phase — every in-scope spec runs on the envtest (Layer A) tier, which needs no Docker.

## State of the Art

No external state-of-the-art shift. This is internal consistency work. The relevant "old vs current" is internal:

| Old Approach | Current Approach | When Changed | Impact |
|--------------|------------------|--------------|--------|
| Milestone/phase reject checked after reporter spawn | Plan checks reject FIRST (12-05) | Phase 12 | WR-10 is bringing milestone/phase up to the plan's pattern |
| Milestone/phase create-site-only label | Milestone/phase in-reconciler backfill (D-03) | Phase 15 | Item 1 brings Plan up to the milestone/phase pattern |

## Sources

### Primary (HIGH confidence — verified this session)
- `internal/controller/plan_controller.go` (read :127-203, :455-514, :1400-1459) — confirmed backfill gap + resolver presence + 12-05 reject-first pattern + CR-01 terminal-Failed branch
- `internal/controller/milestone_controller.go` (read :170-228, :505-564) — backfill block + WR-10 ordering bug (reporter :556 before reject :515) + Pitfall-1 non-fatal pattern
- `internal/controller/phase_controller.go` (read :155-227, :330-444, :446-545) — backfill block + WR-10 (reporter :483 before reject :510) + dispatch-entry reject :336
- `internal/controller/dispatch_helpers.go` (read :265-329) — WR-01 fail-open-on-NotFound confirmed
- `cmd/tide/approve.go` (read :140-229, grep :71-421) — WR-06 guard + `--wave` skip confirmed
- `internal/controller/phase_controller_test.go:270-334`, `milestone_controller_test.go:436-488` — backfill test template
- `internal/owner/label.go:33,45` — `LabelProject` const + `StampProjectLabel`
- `Makefile:85-153`, `go.mod` — test tiers + Go 1.26 + module path
- `.planning/v1.0.1-MILESTONE-AUDIT.md` (frontmatter `tech_debt` + Cross-Phase Integration) — scope source
- `.planning/REQUIREMENTS.md` "Out of Scope" — confirms 27-item backlog is a separate milestone

### Secondary
- `CLAUDE.md` — STACK pins, Verify-Before-Claiming protocol, `make test-int` green-≠-ship gotcha
- Project memory: fix-thoroughly-on-TIDE (root-cause-first at forks), honor-auto-advance

## Metadata

**Confidence breakdown:**
- Standard stack: HIGH — no new deps; existing tier verified against go.mod/Makefile
- Architecture / fix shapes: HIGH — every fix has a read-confirmed sibling template in-tree; all audit pointers resolved (2 reframed, line drift noted)
- Pitfalls: HIGH — derived from the divergence I directly observed between the three completion handlers
- In/Defer recommendation: HIGH for IN items; MEDIUM for WR-06 semantics (genuine fork → A1) and WR-01 (judgment → A2)

**Research date:** 2026-06-12
**Valid until:** 2026-07-12 (stable — internal codebase consistency work; only invalidated by edits to the cited controllers before planning)
</content>
