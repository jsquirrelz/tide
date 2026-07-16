# Slice: `<observable product outcome>`

**ID:** `<slice-id>`
**Status:** Draft — one of `Draft`, `Active`, `Closed`, `Culled`
**Owner:** `<human or accountable team>`
**Risk:** `<low / medium / high>`

`Active` means at least one contract is locked or dispatched. `Closed` requires the observable outcome below demonstrated, not merely every Task accepted. `Culled` means oversight withdrew the slice.

## Outcome

`<What becomes true for a user or operator when this slice succeeds?>`

## Context

`<Why this outcome matters now and what evidence motivated it.>`

## Observable exit signals

- [ ] `<Externally observable behavior or executable product scenario>`
- [ ] `<Operational, security, or quality signal>`

## Constraints

- `<Constraint inherited from PROJECT.md or ARCHITECTURE.md>`

## Non-goals

- `<Adjacent behavior deliberately excluded from this slice>`

## Risks and assumptions

- **Risk:** `<risk>` — **Mitigation/evaluation:** `<how it will be tested>`
- **Assumption:** `<assumption>` — **Disproof signal:** `<what would invalidate it>`

## Tasks

- [`<task-id>`](../tasks/TASK.template.md) — `<bounded contribution to this outcome>`

## Product evaluation

- [ ] `<path under evals/ or CI command>` — evidence: `<artifact, trace, metric, or user observation retained by the run>`
- [ ] `<additional scenario when the outcome needs more than one closing signal>`

Passing every Task is necessary but not sufficient: this slice closes only when its observable outcome is demonstrated.
