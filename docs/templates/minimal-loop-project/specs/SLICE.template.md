# Slice: `<observable product outcome>`

**ID:** `<slice-id>`
**Status:** Draft
**Owner:** `<human or accountable team>`
**Risk:** `<low / medium / high>`

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

- **Command/scenario:** `<path under evals/ or CI command>`
- **Evidence:** `<artifact, trace, metric, or user observation retained by the run>`

Passing every Task is necessary but not sufficient: this slice closes only when its observable outcome is demonstrated.
