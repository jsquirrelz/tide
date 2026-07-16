# Accepted Architecture

**Last reviewed:** 2026-07-16

This document describes architecture that has been accepted. Proposals and speculative designs belong in review artifacts until approved.

## System context

The nightly archiver reads rotated application logs from host disk and uploads them to the `log-archive` bucket. This project inserts a redaction stage between read and upload.

## Boundaries and ownership

| Boundary | Owns | Does not own |
| --- | --- | --- |
| `redaction` | credential patterns, replacement rules | log file I/O, upload, scheduling |
| `archiver` | reading rotated logs, invoking redaction, uploading | pattern definitions |

## Dependency direction

```text
archiver (entrypoint) -> redaction (domain) -> detector interface -> pattern adapters
```

- `archiver` imports `redaction`; `redaction` must not import `archiver` or any storage client.

## Data ownership and persistence

| Data | Source of truth | Writer | Readers | Retention |
| --- | --- | --- | --- | --- |
| Raw rotated logs | host disk | application runtime | archiver | 7 days |
| Redacted archives | `log-archive` bucket | archiver | platform, security | 1 year |

## Security invariants

- Unredacted log bytes never reach a network socket; the archiver is the bucket's only writer, and it writes post-redaction output only.
- Enforced by `evals/product/redaction-scenario.sh` (planted-credential scenario) and the CI import-direction lint.

## Accepted components

| Component | Responsibility | Contract |
| --- | --- | --- |
| `redactlog` CLI | apply redaction rules to a log stream | stdin → stdout; non-zero exit on internal error |
| detector set v1 | AWS access key and bearer-token patterns | detector interface in `redaction/` |

## Architecture checks

- CI import-direction lint: `redaction` must not import `archiver` or storage clients.
