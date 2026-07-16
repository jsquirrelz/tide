# Task: invoke redaction between read and upload

**ID:** `wire-archiver-redaction`
**Spec:** [`redact-live-credentials`](../specs/redact-live-credentials.md)
**Contract state:** Draft
**Version:** `1`
**Supersedes:** none

This contract may be refined while `Draft`. It locks once [`detect-credentials`](detect-credentials.md) is accepted, since its acceptance signals depend on the detector's interface.

## Goal

The archiver pipes every rotated log through `redactlog` before upload, with no unredacted bytes reaching the network path.

## In scope

- Insert the redaction stage between read and upload in the archiver.
- Fail closed: if redaction errors, the file is not uploaded and the run reports the failure.

## Out of scope

- Detector patterns or thresholds (owned by `detect-credentials`).
- Retry or backoff changes to the upload path.

## Inputs and dependencies

- Accepted `detect-credentials` deliverables (`redaction/` package, `redactlog` CLI).

## Deliverables

- Archiver pipeline change, integration test, updated runbook note.

## Acceptance signals

- [ ] `evals/product/redaction-scenario.sh` exits 0 against the assembled pipeline.
- [ ] Integration test: a redaction error leaves the file unuploaded and the run marked failed.

## Constraints and prohibited changes

- Unredacted log bytes never reach a network socket (ARCHITECTURE.md security invariant).
- Do not weaken or delete an evaluator merely to make this Task pass.

## Evidence required from the run

- Everything in the run evidence contract in [`../evals/README.md`](../evals/README.md).

## Escalation

- **Fresh attempt:** integration-test failures within the archiver pipeline.
- **System escalation:** recurring harness timeouts dispatching archiver integration runs.
- **Human decision:** any change to the fail-closed behavior (availability versus leak-risk trade-off).
