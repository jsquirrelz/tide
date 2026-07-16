# Project

**Status:** Active
**Owner:** Platform team
**Last reviewed:** 2026-07-16

## Active outcome

Application logs archived to long-term storage contain no live credentials, without slowing the nightly archive pipeline.

## Intended users

- Platform engineers searching archived logs during incidents.
- Security auditors reviewing archive storage.

## Observable success signals

- The nightly credential scan over shipped archives reports zero live-credential findings for 14 consecutive days.
- Archive completion stays within its current 2-hour window.

## Constraints and invariants

- Raw, unredacted logs never leave the host boundary; redaction runs before upload.
- No new external services; redaction runs inside the existing archiver deployment.

## Risk posture

Conservative: archives feed compliance-scoped storage. Prefer a missed pattern surfacing loudly in the eval over a clever detector that is hard to review.

## Authority and autonomy

| Decision | Authority |
| --- | --- |
| Execute an approved Task | automatic |
| Change a Spec | platform lead |
| Change accepted architecture | platform lead + security review |
| Change security or product evaluators | security team |
| Ship or merge | platform lead |

## Budget and stop conditions

- **Budget:** 3 engineer-weeks; agent spend ≤ $400/month.
- **Pause when:** a live credential is found in a shipped archive, or the archive window is exceeded twice in one week.
- **Cull when:** upstream logging adopts platform-wide secret masking, making archive-side redaction redundant.

## Non-goals

- PII redaction beyond credentials (separate initiative).
- Streaming or real-time redaction; the nightly batch path only.

## Active slices

- [`redact-live-credentials`](specs/redact-live-credentials.md) — archived logs contain no live AWS keys or bearer tokens.
