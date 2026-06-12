---
phase: 14
slug: budget-enforcement-pricing
status: draft
shadcn_initialized: false
preset: none
created: 2026-06-12
---

# Phase 14 — UI Design Contract (Gap Closure: BudgetBlocked on the Project Node)

> Visual and interaction contract for the **dashboard half of BUDGET-02** (14-VERIFICATION.md gap). The kubectl half is verified; this spec covers ONLY the missing surface: a `BudgetBlocked` condition on the Project CR must be visible on the dashboard project node. Per the gap analysis, the design generalizes to **blocking conditions on the project node** — Phase 13's `BillingHalt` condition (same pre-existing gap) rides on the same mechanism instead of a BudgetBlocked-only special case.

> **Scope guard:** This is NOT a phase redesign. Everything not listed under "Components in Scope" is untouched. The governing full-dashboard contract remains `.planning/milestones/v1.0.0-phases/04-gates-observability-dashboard-cli/04-UI-SPEC.md` (approved 2026-05-17) — all tokens, typography, spacing, and voice below are inherited from it verbatim, restated here only where this gap consumes them.

> **Locked upstream:** 14-CONTEXT.md D-04 — "BudgetBlocked condition on Project.Status.Conditions, mirroring Phase 13's BillingHalt shape (kubectl + dashboard visible, **no new chip states**)." Interpretation: the `StatusValue` enum (the `.status.phase` chip vocabulary in `StatusBadge.tsx`) gains NO new members. The blocked state is a **separate condition badge**, not a phase chip — conditions and phases are different CRD surfaces and stay typed apart. Phase 15's CUTS-05 chip remapping is unrelated and out of scope.

---

## Design System

| Property | Value |
|----------|-------|
| Tool | none (Tailwind v4 raw, no shadcn) — inherited, locked by 04-UI-SPEC |
| Preset | not applicable |
| Component library | none — bespoke primitives in `dashboard/web/src/components/` |
| Icon library | `lucide-react` (tree-shaken; only enumerated icons imported) |
| Font | System font stack; `--font-mono` for all badge text (inherited tokens in `src/index.css`) |

No shadcn gate applies: `components.json` is absent by approved prior decision (04-UI-SPEC "Rationale for no shadcn"), and AUTO mode forbids re-asking. Registry safety: not applicable.

---

## Spacing Scale

Inherited from 04-UI-SPEC (8-point scale, multiples of 4, arbitrary values forbidden by lint). Values this gap consumes:

| Token | Value | Usage here |
|-------|-------|------------|
| `space-1` | 4px | Badge icon-to-label gap (`gap-1`), badge vertical padding (`p-1`) |
| `space-2` | 8px | Badge horizontal padding (`px-2`), gap between StatusBadge and ConditionBadge in the node summary row (`gap-2`) |

Exceptions (inherited, unchanged): 14px badge icon square. No new exceptions introduced.

---

## Typography

Inherited from 04-UI-SPEC. Roles this gap consumes:

| Role | Size | Weight | Line Height | Family | Usage here |
|------|------|--------|-------------|--------|------------|
| Mono badge | 12px | 600 (semibold) | 1.4 | `--font-mono` | ConditionBadge label (identical to StatusBadge: 12px / 600 / 1.4 mono) |
| Label | 12px | 400 | 1.3 | sans | Node summary row text (unchanged) |

No new sizes or weights. Bold (700+), italics remain forbidden.

---

## Color

Inherited 60/30/10 tokens (dark default, light variant — both already defined in `src/index.css`). This gap introduces zero new color values.

| Role | Token | Dark | Light | Usage here |
|------|-------|------|-------|------------|
| Blocked family | `--color-status-blocked` | `#a371f7` | `#9333ea` | ConditionBadge foreground/tint for BOTH BudgetBlocked and BillingHalt; blocked-node border-left |
| Surface raised | `--color-surface-raised` | `#161B22` | `#F6F8FA` | Node body (unchanged) |

**Why blocked-purple, not error-red or warning-amber:** the existing Status Vocabulary assigns `--color-status-blocked` to policy-halted states (`PushLeakBlocked`) vs. red for failures and amber for human-gate waits. BudgetBlocked and BillingHalt are exactly that class — dispatch halted by policy/account state, recoverable by operator action, nothing has *failed*. Both badges share the purple family and are disambiguated by icon + label (same pattern as the red family's `CircleX`/`LockKeyhole`/`Ban` split — never color-only, per the inherited color-blindness rule).

**Badge construction (verbatim StatusBadge formula — do not diverge):** foreground = `var(--color-status-blocked)` full saturation; background = `color-mix(in srgb, <color> 15%, transparent)`; border = `1px solid color-mix(in srgb, <color> 40%, transparent)`; border-radius 4px.

Accent reserved-for list: unchanged from 04-UI-SPEC (6 surfaces). The ConditionBadge does NOT use accent. Destructive reserved-for list: unchanged; BudgetBlocked is not destructive styling.

---

## Components in Scope

Exactly four touch points. Match surrounding file conventions exactly (doc-comment style, `data-testid` patterns, UI-SPEC § references in comments).

### C1. `<ConditionBadge>` — new primitive (`dashboard/web/src/components/ConditionBadge.tsx`)

A condition-vocabulary sibling of `<StatusBadge>` — same pill anatomy (14px lucide icon + 12px semibold mono label, tinted fill/border per the formula above), but keyed on **Project blocking-condition types**, not `StatusValue`. Renders nothing for unknown condition types (defensive against vocabulary drift, mirroring `coerce()`).

**Condition vocabulary table (verbatim — planner/executor use as the single source):**

| Condition Type | Color token | lucide Icon | Label | Animation | Native tooltip (`title`) | SR Description (`aria-label`) |
|----------------|-------------|-------------|-------|-----------|--------------------------|-------------------------------|
| `BudgetBlocked` | `--color-status-blocked` | `Wallet` | `Budget blocked` | none | The condition's `message` field verbatim (controller stamps e.g. "Cost spent 10100 cents (+ 220 reserved) exceeds cap 10000 cents; dispatch halted project-wide") | "Budget cap reached — dispatch halted. Raise spec.budget.absoluteCapCents or apply the bypass annotation to resume" |
| `BillingHalt` | `--color-status-blocked` | `CreditCard` | `Billing halted` | none | The condition's `message` field verbatim | "Provider credit balance too low — dispatch halted. Refill credits and run `tide resume`" |

**Props:**
```ts
export type ProjectBlockingCondition = {
  type: string;    // "BudgetBlocked" | "BillingHalt" (open string — unknown types render nothing)
  reason: string;  // e.g. "BudgetCapReached"
  message: string; // controller-stamped human message — surfaces as native title tooltip
  age: string;     // server-formatted relative time, e.g. "4m 12s" (same formatAge shape as taskCondition)
};
type Props = { condition: ProjectBlockingCondition; className?: string };
```

`role="status"`, `data-testid="condition-badge-<type>"`, `data-condition="<type>"`. Export the vocabulary table (e.g. `CONDITION_TABLE`) for test iteration, mirroring `STATUS_TABLE`'s re-export.

### C2. `<TideNodeShell>` extension — optional blocking-conditions slot

Add an optional prop `blockingConditions?: ProjectBlockingCondition[]` (default `[]`).

- **Badge placement:** in the existing summary row, immediately AFTER `<StatusBadge>`, before the summary text — `gap-2` separation (the row's existing gap). Each True blocking condition renders one `<ConditionBadge>`. Both badges `shrink-0`; summary text keeps `min-w-0 truncate` and absorbs the squeeze (operators see badges over counts when width is tight).
- **Border treatment:** when `blockingConditions.length > 0` AND the node is not in the failed family, apply `border-l-4 border-l-[var(--color-status-blocked)]` — the purple sibling of the existing destructive border-left, so blocked state reads at zoomed-out DAG scale where badges are illegible. Failed family takes precedence (destructive red wins if both apply). Expose `data-blocked="true|false"` alongside the existing `data-failed`.
- **Aria:** extend the node `aria-label` to `"<kind> <name>, status <status>, blocked: <Label1>[, <Label2>]"` only when blocked; unchanged otherwise.
- Zero conditions → identical render to today (regression: existing TideNodeShell/node tests pass unmodified).

### C3. `<ProjectNode>` extension

`ProjectNodeData` gains `blockingConditions: ProjectBlockingCondition[]`; pass through to `TideNodeShell`. `buildPlanningGraph` in `PlanningDAGView.tsx` maps `detail.blockingConditions ?? []` into `projectData`. No layout change: the node stays 360×92 min — the badge lives inside the existing summary row, and `minHeight` already tolerates row wrap if both conditions are simultaneously True (the composability case: budget cap AND billing halt).

### C4. Backend — expose Project blocking conditions (`cmd/dashboard/api/projects.go`)

Extend `projectSummary` with `BlockingConditions []projectCondition \`json:"blockingConditions"\`` populated in `summarize()`:

- **Whitelist filter:** only `Status.Conditions` entries with `Type ∈ {BudgetBlocked, BillingHalt}` AND `Status == True`. Bounded payload (≤ 2 entries) — consistent with the "stripped down to bound response size" doctrine in the file header.
- **Shape:** `{type, reason, message, age}` — mirrors `taskCondition` in `tasks.go` (same package; reuse `formatAge` for the relative-time string; add `message` since the tooltip needs it).
- **Empty contract:** pre-allocate so zero conditions serialize as `[]`, never `null` (existing D-UI-SPEC empty-array contract).
- Because `projectDetail` embeds `projectSummary`, both `GET /api/v1/projects` and `GET /api/v1/projects/{name}` carry the field — `fetchProject` → `ProjectDetail` (`dashboard/web/src/lib/api.ts` type mirrors get the same optional field, `blockingConditions: ProjectBlockingCondition[]`).
- Read-only GET only — `TestZeroMutationRoutes` invariant untouched.

**Live-update path (no new wiring):** `PlanningDAGView` already refetches the full `ProjectDetail` on any `kind=Project` SSE event; a `Status.Conditions` patch on the Project CR bumps resourceVersion, the informer bridge emits the event, and the debounced refetch picks up the new badge. **Verification item for the executor:** confirm with the run-1 regression flow (cap trips → badge appears without manual refresh); if the SSE projection filters status-only Project updates, that filter must admit them.

**Out of scope (do NOT build):** ConditionBadge in `<ProjectPicker>` rows or `<Header>`; blocking conditions on Milestone/Phase/Plan/Task nodes; any chip-state (`StatusValue`) additions (Phase 15 CUTS-05 owns chip remapping); budget spend/cap numeric display on the node (COST-02, deferred); in-dashboard unblock actions (read-only contract, D-D6).

---

## Copywriting Contract

Voice inherited: terse, declarative, operator-focused, em dashes, no exclamation points.

| Element | Copy |
|---------|------|
| Primary CTA | none — this gap adds no buttons (read-only surface; recovery commands live in CLI/kubectl) |
| BudgetBlocked badge label | `Budget blocked` |
| BillingHalt badge label | `Billing halted` |
| BudgetBlocked tooltip | condition `message` verbatim — never paraphrase the controller (e.g. `Cost spent 10100 cents (+ 220 reserved) exceeds cap 10000 cents; dispatch halted project-wide`) |
| BillingHalt tooltip | condition `message` verbatim |
| BudgetBlocked SR description | `Budget cap reached — dispatch halted. Raise spec.budget.absoluteCapCents or apply the bypass annotation to resume` |
| BillingHalt SR description | `Provider credit balance too low — dispatch halted. Refill credits and run `tide resume`` |
| Empty state | no badge rendered; node identical to pre-Phase-14 render (absence of the condition IS the empty state) |
| Error state | none new — payload field absent/empty degrades to no badge (frontend treats `blockingConditions` as optional, defaulting `[]`) |
| Destructive confirmation | none — no destructive actions in this gap |

---

## Validation Contract

| Surface | Test | Framework |
|---------|------|-----------|
| ConditionBadge | renders icon+label+tooltip per vocabulary table; unknown type renders nothing | Vitest (`ConditionBadge.test.tsx`, mirror `StatusBadge.test.tsx`) |
| TideNodeShell | `data-blocked` + purple border-left when conditions present; destructive precedence when also failed; zero-condition render unchanged | Vitest |
| ProjectNode/PlanningDAGView | `blockingConditions` from `ProjectDetail` reaches the badge | Vitest (extend existing PlanningDAGView/ProjectNode specs via `initialData`) |
| projects.go | `summarize()` whitelist filter, True-only, `[]`-not-null, message passthrough | Go (`projects_test.go`) |
| End-to-end (BUDGET-02 SC2) | cap trips → BudgetBlocked on Project CR → badge visible on dashboard project node via SSE refetch | envtest/manual per 14-VERIFICATION.md re-run |

---

## Registry Safety

| Registry | Blocks Used | Safety Gate |
|----------|-------------|-------------|
| shadcn official | none — not initialized | not applicable |
| Third-party registries | none | not applicable |

New lucide imports (`Wallet`, `CreditCard`) come from the already-pinned `lucide-react` dependency — no new packages, bundle gates unaffected (~2KB).

---

## Checker Sign-Off

- [ ] Dimension 1 Copywriting: PASS
- [ ] Dimension 2 Visuals: PASS
- [ ] Dimension 3 Color: PASS
- [ ] Dimension 4 Typography: PASS
- [ ] Dimension 5 Spacing: PASS
- [ ] Dimension 6 Registry Safety: PASS

**Approval:** pending

---

*UI Design Contract for: Phase 14 — Budget Enforcement + Pricing (BUDGET-02 dashboard gap closure)*
*Authored: 2026-06-12 via gsd-ui-researcher (AUTO mode — zero user questions; all decisions derived from 04-UI-SPEC + 14-CONTEXT.md D-04 + codebase inspection)*
*Inherits: 04-UI-SPEC.md tokens, typography, spacing, voice (approved 2026-05-17)*
