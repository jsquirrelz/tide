---
created: 2026-07-03T18:08:15.231Z
title: Rename subagent.levels semantics — each key names the artifact being planned
area: api
files:
  - internal/controller/project_controller.go:1213
  - internal/controller/dispatch_helpers.go:138
  - api/v1alpha2/project_types.go:165
---

## Problem

`Project.Spec.subagent.levels` has slots for `milestone`/`phase`/`plan`/`task`,
but the dispatch that authors MILESTONE.md is level `"project"` —
`project_controller.go:1213` calls `BuildPlannerEnvelope("project", ...)`,
which matches no case in `ResolveProvider`'s switch (`dispatch_helpers.go:138`),
so it silently falls back to `Spec.Subagent.Model`.

Operator impact (hit on the first external-repo run, 2026-07-03): operators reliably read
`levels.milestone` as "the model that authors the milestone," but the actual
semantics are "the model the Milestone CR uses to author its children (phase
briefs)." The intended per-artifact ladder (Fable→milestone, Opus→phase,
Sonnet→plan, Haiku→task) landed off-by-one, and the MILESTONE.md dispatch ran
on the fallback model with no error or event.

## Solution

**DECIDED (operator preference, 2026-07-03): rename the semantics so each
`levels.X` key means "level X is planned by this model"** — the reading
operators already have. Do NOT merely document the current
dispatching-CR semantics (rejected: the naming would stay permanently
counterintuitive) and do NOT bolt on `levels.project` (rejected: keeps the
off-by-one and adds a sixth confusing key).

Target mapping (spec key → dispatch surface it resolves):

| Key | Authors | Current dispatch level string |
| --- | --- | --- |
| `levels.milestone` | MILESTONE.md | `"project"` |
| `levels.phase` | phase briefs | `"milestone"` |
| `levels.plan` | PLAN.md **and** the task DAG | `"phase"` + `"plan"` (two dispatches, one key — both are "planning the plan's content") |
| `levels.task` | task execution (diffs) | `"task"` |

Design points to settle at planning time:

- Whether folding PLAN.md + task-DAG under one `levels.plan` key is right, or
  the task DAG deserves its own key — operator mental model treats them as
  one ("Sonnet for the Plan").
- Migration: this is a silent semantic remap of existing v1alpha2 fields —
  existing manifests would resolve differently after upgrade. Use the
  SchemaRevision discriminator pattern (or v1alpha3) so old manifests
  fail-close or convert explicitly rather than shifting models silently.
- Top-level `subagent.model` becomes pure fallback (no longer the only route
  to the MILESTONE.md dispatch).
- Also log the resolved model at dispatch (today it appears nowhere in pod
  spec, events, or conditions — only inside the PVC envelope).
