# Task: credential detector set for AWS keys and bearer tokens

**ID:** `detect-credentials`
**Spec:** [`redact-live-credentials`](../specs/redact-live-credentials.md)
**Contract state:** Locked
**Version:** `1`
**Supersedes:** none

This contract is `Locked`: the commit that flipped this state fixes its text, and runs reference that commit. To change it, create a successor with `Supersedes: detect-credentials`.

## Goal

A `redaction/` package exposing detector set v1 that finds AWS access keys and bearer tokens in UTF-8 text, with recall measured on the fixture corpus.

## In scope

- Detectors for AWS access key IDs, AWS secret access keys, and `Authorization: Bearer` tokens.
- Replacement with `[REDACTED:<type>]`, preserving line structure.
- Fixture corpus under `evals/fixtures/credentials/`, including base64 and line-wrapped variants.

## Out of scope

- Wiring into the archiver (`wire-archiver-redaction` owns that).
- PII patterns; entropy-based generic secret detection.

## Inputs and dependencies

- Detector interface in `redaction/` (ARCHITECTURE.md accepted component).

## Deliverables

- `redaction/` package with detector set v1, unit tests, and the fixture corpus.

## Acceptance signals

- [ ] `go test ./redaction/...` exits 0.
- [ ] Recall ≥ 0.98 and precision ≥ 0.95 on the fixture corpus, reported by the package's corpus test (`go test ./redaction/ -run TestCorpusRecall -v`).

## Constraints and prohibited changes

- `redaction` must not import `archiver` or any storage client (dependency direction).
- Do not weaken or delete an evaluator merely to make this Task pass.

## Evidence required from the run

- Everything in the run evidence contract in [`../evals/README.md`](../evals/README.md).
- The recall/precision report over the fixture corpus.

## Escalation

- **Fresh attempt:** unit-test failures or recall below threshold on the fixture corpus.
- **System escalation:** repeated attempts that try to edit fixtures or thresholds instead of the detector.
- **Human decision:** any credential type beyond the three in scope (that is a Spec change).
