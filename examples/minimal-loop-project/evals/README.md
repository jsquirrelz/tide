# Executable Evaluations

This directory contains executable closing signals, not a manually maintained verification checklist. Groups in use (only what this project needs):

```text
evals/
├── product/     # redaction-scenario.sh — planted-credential scenario
└── fixtures/    # credential corpus shared by evals and detector tests
```

Security and architecture invariants are enforced by the product scenario and the CI import-direction lint; a `system/` group is not yet needed.

## Run evidence contract

This list is canonical — Task contracts reference it rather than duplicating it. Every CI and task run must generate at least:

- Task and Spec identifiers, and the Task contract's locking commit (or blob hash).
- Commands and evaluator versions executed.
- Test and evaluation results.
- Changed-file manifest.
- Runtime, model, and prompt version.
- Cost, duration, and resource usage when applicable.
- Terminal reason and any bounded feedback passed to a new attempt.

## Integrity rules

- Do not change a system candidate and its evaluator in the same experiment.
- Deterministic security, compile, and test failures override probabilistic judge approval.
- Do not silently lower a threshold after seeing candidate results.
- Version evaluator changes and preserve the baseline needed to compare them.
- Keep detailed run evidence in CI artifacts, traces, or a run store rather than duplicating it here.
