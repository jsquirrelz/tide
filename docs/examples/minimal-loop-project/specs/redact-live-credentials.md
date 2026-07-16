# Slice: archived logs contain no live credentials

**ID:** `redact-live-credentials`
**Status:** Active
**Owner:** Platform team
**Risk:** high

## Outcome

An operator searching any archive written after this slice closes can trust that AWS access keys and bearer tokens were replaced with `[REDACTED:<type>]` markers before upload.

## Context

A quarterly audit found two live AWS keys in archived logs (incident PLAT-412). Rotation cost a day of incident work, and the audit sampled only 5% of archives.

## Observable exit signals

- [ ] `evals/product/redaction-scenario.sh` passes: planted credentials in fixture logs never survive to the archive artifact.
- [ ] The nightly credential scan over real shipped archives reports zero findings for 14 consecutive days.

## Constraints

- Raw logs never leave the host boundary (PROJECT.md).
- Archive completion stays within the 2-hour window (PROJECT.md).

## Non-goals

- PII redaction.
- Streaming redaction.

## Risks and assumptions

- **Risk:** regex detectors miss encoded or line-wrapped tokens — **Mitigation/evaluation:** the fixture corpus includes base64 and line-wrapped variants; recall is measured in `detect-credentials` acceptance.
- **Assumption:** rotated logs are UTF-8 text — **Disproof signal:** the archiver encounters binary or non-UTF-8 rotated files in production.

## Tasks

- [`detect-credentials`](../tasks/detect-credentials.md) — detector set for AWS keys and bearer tokens (Locked).
- [`wire-archiver-redaction`](../tasks/wire-archiver-redaction.md) — invoke redaction between read and upload (Draft).

## Product evaluation

- [ ] `evals/product/redaction-scenario.sh` — evidence: eval exit code and the redacted fixture artifact retained by CI.
- [ ] Nightly archive credential scan — evidence: scan report metric in the run store.

Passing every Task is necessary but not sufficient: this slice closes only when its observable outcome is demonstrated.
