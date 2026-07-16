# Task: `<bounded implementation result>`

**ID:** `<task-id>`
**Spec:** [`<slice-id>`](../specs/SLICE.template.md)
**Contract state:** Draft
**Version:** `1`
**Supersedes:** `<task-id or none>`

This contract may be refined while `Draft`. Once locked for dispatch, do not rewrite it to match an implementation; create a new version with `supersedes` instead.

## Goal

`<One implementation result this Task must produce.>`

## In scope

- `<Required behavior, file surface, or interface>`

## Out of scope

- `<Tempting adjacent work this Task must not absorb>`

## Inputs and dependencies

- `<Accepted architecture, prior Task, API, fixture, or decision>`

## Deliverables

- `<Code, test, migration, documentation, or artifact>`

## Acceptance signals

- [ ] `<Executable test or evaluator command and expected result>`
- [ ] `<Observable contract assertion>`

## Constraints and prohibited changes

- `<Architecture or security invariant that must remain true>`
- Do not weaken or delete an evaluator merely to make this Task pass.

## Evidence required from the run

- Commands and evaluator versions executed.
- Test/evaluation results and changed-file manifest.
- Runtime, model, prompt version, duration, cost, and terminal reason when available.

## Escalation

- **Fresh attempt:** `<Which failures may return to the Task loop?>`
- **System escalation:** `<What recurring cross-Task pattern would justify changing the harness, prompt, model, or evaluator?>`
- **Human decision:** `<Which ambiguity, risk, or architecture change requires approval?>`
