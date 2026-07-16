# Executable Evaluations

This directory contains executable closing signals, not a manually maintained verification checklist.

Organize evaluations by what they protect, using only the groups the project actually needs:

```text
evals/
├── product/        # user-visible scenarios and outcome checks
├── security/       # authorization, secret, isolation, and abuse checks
├── architecture/   # dependency direction and boundary checks
└── system/         # versioned baseline/challenger benchmarks
```

## Evaluation contract

Every evaluation should define:

- A stable identifier and version.
- Reproducible inputs or fixtures.
- A documented command or entrypoint.
- A machine-observable pass/fail result.
- Evidence retained by CI or the run system.
- Ownership and the approval required to weaken or replace it.

Use exit code `0` for pass and non-zero for failure unless the surrounding framework defines a stronger result protocol. Emit structured result data when scores, confidence, or multiple assertions matter.

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
