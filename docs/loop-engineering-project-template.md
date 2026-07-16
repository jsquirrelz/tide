# Minimal Loop-Engineering Project Template

**Audience:** Teams starting an agent-assisted software project or introducing agents into an existing repository.

**Purpose:** Provide the smallest useful set of durable planning artifacts while keeping execution evidence in CI, traces, and run artifacts.

The copyable files live in [`docs/templates/minimal-loop-project/`](templates/minimal-loop-project/). A filled-in example — a log-redaction pipeline with one slice, two contracts, and a runnable eval — lives in [`examples/minimal-loop-project/`](../examples/minimal-loop-project/).

**Relationship to TIDE:** this template is standalone methodology, not TIDE's planning hierarchy. TIDE's five levels (Milestone → Phase → Plan → Task → Wave — see [`concepts.md`](concepts.md)) describe how the orchestrator decomposes and dispatches work; the five surfaces below describe what any repository should persist while agents work on it. In particular, a Task file here is an immutable implementation contract — not a TIDE `Task` CRD, which is a DAG node derived from a plan and dispatched in waves. The pack copies into any repository, TIDE-driven or not.

## Principle

Do not create one planning hierarchy for every AI-engineering loop. The [oversight, system, product, task, and execution loops](https://arize.com/blog/what-is-a-loop-in-ai-engineering-anyway/) describe processes operating at different feedback scopes; they are not five new document trees.

The five loops, by feedback scope:

- **Execution** — one bounded attempt: edit, run, read tool/compile/test feedback, repeat.
- **Task** — attempts against one locked contract: dispatch, judge evidence, retry or supersede.
- **System** — the machinery shared across Tasks: prompts, models, tools, harnesses, evaluators.
- **Product** — whether accepted Tasks add up to outcomes users can observe.
- **Oversight** — whether the product remains worth its budget, risk, and opportunity cost.

Use five durable repository surfaces:

```text
PROJECT.md                 # active outcome, authority, constraints, and non-goals
ARCHITECTURE.md            # accepted architecture, boundaries, and invariants
specs/<slice>.md           # product slice and observable exit signals
tasks/<task>.md            # one bounded task contract, immutable after dispatch
evals/                     # executable product, security, and system evaluations
```

Add `decisions/` only when the first consequential or difficult-to-reverse architecture decision needs an ADR. `ARCHITECTURE.md` says what is accepted now; an ADR records why a consequential choice was made and what it displaced.

## Artifact responsibilities

### `PROJECT.md`

Defines the current outcome and the authority under which work runs: intended users, success signals, constraints, budget, risk posture, autonomy, and non-goals. Change it deliberately when oversight changes the project—not as a side effect of implementation.

### `ARCHITECTURE.md`

Contains accepted architecture only: boundaries, dependency direction, data ownership, security invariants, and approved components. Agents may propose changes, but a proposal does not become accepted architecture until its evaluation and approval are complete.

### `specs/`

Each file defines one product slice in observable terms. A slice states the user or operator outcome, exit signals, constraints, risks, and the Tasks intended to realize it. Correct Tasks that do not produce the slice outcome are a product-loop failure.

### `tasks/`

Each file is a bounded implementation contract. It can be refined while in `draft`, but becomes immutable when locked for dispatch. If a locked contract is wrong, create a replacement with `supersedes` rather than silently weakening the original.

Task status and run transcripts do not belong in the contract. CI or the execution system owns them.

### `evals/`

Contains executable closing signals: tests, security checks, architecture checks, product scenarios, and system benchmarks. Prefer programs and fixtures over manually maintained verification checklists.

Candidate code must not weaken its own evaluator unnoticed. Changes to security gates, architecture checks, or benchmark definitions require heightened review; do not change a system candidate and the evaluator judging it in the same experiment.

## Failure routing

A failure moves outward only as far as its evidence requires:

| Signal | Loop | Response |
| --- | --- | --- |
| Tool, compile, or test feedback during one attempt | Execution | Inspect, edit, and rerun within the bounded attempt |
| A locked Task still fails after a fresh attempt | Task | Restart from the same contract with a compact evidence packet |
| The same failure pattern recurs across Tasks or runs | System | Change and evaluate prompts, models, tools, harnesses, or evaluators |
| Tasks pass their contracts but the slice misses the user outcome | Product | Revisit the slice, prioritization, or product assumption |
| The product is no longer worth its budget, risk, or opportunity cost | Oversight | Reallocate, reduce autonomy, pause, or cull the work |

Retry count alone does not prove a system defect. A difficult individual Task stays in the Task loop; move outward when evidence shows a recurring system pattern.

## Contract lifecycle

```text
draft Task
  -> review its scope and executable exit signals
  -> lock the contract by committing it
  -> dispatch using that exact commit
  -> generate evidence
  -> accept, retry, escalate, or supersede
```

A Task contract is in exactly one state:

| State | Meaning | Transitions to |
| --- | --- | --- |
| `Draft` | Refinable; not dispatchable | `Locked` |
| `Locked` | Immutable; dispatch references the locking commit | `Superseded` |
| `Superseded` | Replaced by a newer contract that names it in `Supersedes` | terminal |

A slice moves `Draft → Active → Closed`, or to `Culled` when oversight withdraws it. `Active` means at least one of its contracts is locked or dispatched; `Closed` requires the slice's observable outcome demonstrated, not merely every Task accepted.

Locking is a commit: the commit that flips `Contract state` to `Locked` fixes the contract's exact text, and git content-addresses it — `git show <sha>:tasks/<task>.md` reproduces precisely what was dispatched. No separate content hash is needed; runs record the locking commit (or the file's blob hash) in their evidence. Every run should identify the exact Project, architecture, Spec, Task, evaluator, prompt/runtime, and model versions it used.

## Evidence contract

The canonical list of fields every CI and task run must produce lives in the pack at [`evals/README.md`](templates/minimal-loop-project/evals/README.md), so it travels with the copied template and Task contracts can reference it without duplicating it.

Keep detailed evidence in CI artifacts, trace storage, or a run store. Commit evidence only when it is itself a release or compliance artifact. Avoid a manually curated `evidence/` hierarchy that can drift from what actually ran.

## Architecture change gate

Material boundary changes follow this path:

```text
proposal -> evaluation -> oversight approval -> ADR -> ARCHITECTURE.md update
```

This prevents an implementation agent from silently expanding the platform, weakening security invariants, or turning speculative machinery into accepted architecture.

## Starting a project

1. Copy [`docs/templates/minimal-loop-project/`](templates/minimal-loop-project/) to the repository root.
2. Fill in `PROJECT.md` before authoring slices.
3. Record the minimum accepted architecture and invariants in `ARCHITECTURE.md`.
4. Create the first product slice from `specs/SLICE.template.md`.
5. Split it into bounded contracts using `tasks/TASK.template.md`.
6. Implement the slice's closing signals under `evals/` before increasing autonomy.
7. Configure CI to retain generated evidence and require the relevant evaluators.

The [example project](../examples/minimal-loop-project/) shows each artifact filled in for one small, real slice — use it to calibrate how much detail a field needs.

Start with supervised outer loops. Increase autonomy separately for execution, task, product, and system work only after their closing signals have proven reliable.
