---
phase: 47-self-hosted-phoenix-install-end-to-end-proof
plan: 03
subsystem: docs
tags: [phoenix, opentelemetry, otlp, helm, observability, install-docs]

# Dependency graph
requires:
  - phase: 47-01/47-02 (wave 1)
    provides: OTLP-headers wiring — otel.exporter.headersSecretRef values key, secretKeyRef env on manager + dashboard Deployments, ReporterOptions.OTLPHeaders forwarding to reporter Jobs, and the NOTES.txt tracing-dark nudge
provides:
  - docs/INSTALL.md "Enable tracing (Phoenix)" quickstart section (three-beat: install Phoenix, mint API key + wire headers Secret + helm upgrade tide, verify)
  - docs/observability.md "Self-hosted Phoenix" reference subsection (both storage strategies, auth posture, sizing, headers chain)
  - Freshly re-verified Phoenix chart pin (10.0.1 / appVersion 18.1.0) replacing the stale research-time pin (10.0.0 / 18.0.0)
  - Discharged deep-link placeholder ("install instructions land in Phase 47") with cross-references
affects: [47-04, 47-05]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Doc-authoring-time chart re-fetch: never trust a pin recorded more than a few days earlier for a near-daily-shipping external Helm chart — re-pull live and re-verify every fact the doc depends on, not just the version string"
    - "helm template render-before-document: rendering the exact quickstart values against the live chart pin before writing prose surfaces the real resource names (Service, Secret) instead of assuming them"

key-files:
  created: []
  modified:
    - docs/INSTALL.md
    - docs/observability.md

key-decisions:
  - "Chart pin updated to 10.0.1/appVersion 18.1.0 (moved from the plan's predicted 10.0.0/18.0.0 since research 2 days ago) after a live OCI pull + full re-verification of all three D-04 facts (appVersion floor, auth.enableAuth default, storage-mode exclusivity) — all three came back unchanged, so only the pin number and appVersion floor sentence needed updating, not the doc's argumentative structure"
  - "Real rendered Service name is tide-phoenix-svc (not the assumed tide-phoenix) — confirmed via a live helm template render of the exact quickstart values against the fresh pin; used consistently in every endpoint/port-forward example"
  - "Confirmed via live fetch of Arize's own authentication docs: default login is admin@localhost with the chart's PHOENIX_DEFAULT_ADMIN_INITIAL_PASSWORD Secret value (fallback literal admin), and the System-API-key flow is Settings -> API Keys -> Authorization: Bearer <token> — matches the plan's described flow exactly, cited in the doc"

requirements-completed: [PHX-01, PHX-02]

duration: 7min
completed: 2026-07-17
---

# Phase 47 Plan 03: Self-Hosted Phoenix Install Docs Summary

**Authored the PHX-01/PHX-02 documentation surface for a self-hosted Arize Phoenix install (chart pin 10.0.1/appVersion 18.1.0, freshly re-verified live) — an INSTALL.md three-beat quickstart mirroring the Prometheus precedent, and an observability.md reference subsection covering both storage paths, the auth-on posture, and the full OTLP-headers forwarding chain.**

## Performance

- **Duration:** ~7 min (task execution; excludes upstream context load)
- **Started:** 2026-07-17T13:32Z (worktree base reset)
- **Completed:** 2026-07-17T13:39Z
- **Tasks:** 2/2 completed
- **Files modified:** 2

## Accomplishments

- Live-re-fetched the Phoenix Helm chart pin at doc-authoring time per the ROADMAP-mandated D-03 requirement: found it had moved from the plan's predicted `10.0.0`/appVersion `18.0.0` to `10.0.1`/appVersion `18.1.0` since research two days prior. Pulled the new chart and re-verified all three D-04 posture facts (appVersion ≥ 14.2.0 floor, `auth.enableAuth=true` default, storage-mode exclusivity `fail` guard) — all confirmed unchanged, so only the pin number and the deep-link floor sentence needed a text update, not the doc's structure.
- Rendered the exact quickstart values (`persistence.enabled=true`, `postgresql.enabled=false`, `service.type=ClusterIP`, `ingress.enabled=false`) against the fresh pin via `helm template` — confirmed the render succeeds (proves the value combination clears `phoenix.validatePersistence`), the real Service name (`tide-phoenix-svc`), and the `phoenix-secret` Secret shape (including the `PHOENIX_DEFAULT_ADMIN_INITIAL_PASSWORD` key used in the doc's login step).
- Added `docs/INSTALL.md`'s `### Enable tracing (Phoenix)` section as a sibling directly after "Enable telemetry (Prometheus)": install Phoenix (pinned, SQLite-on-PVC), mint a System API key and wire the `tide-otlp-headers` Secret + `otel.exporter.endpoint`/`headersSecretRef` via `helm upgrade tide`, verify via port-forward + login + the NOTES.txt warning disappearing. Includes the bare-`host:port` scheme-prefix warning callout and the sampler-already-at-1.0 reassurance.
- Added `docs/observability.md`'s `### Self-hosted Phoenix` subsection under `## Tracing`: both storage strategies (SQLite-on-PVC quickstart with the chart's own `maxSpansQueueSize` sizing heuristic; bundled-Postgres durable default with the D-07 honesty line naming which path this milestone's live proof actually exercised), the auth-on posture with the weak-default call-outs (`admin`, `PHOENIX_POSTGRES_PASSWORD: "postgres"`), and the exposure framing for the dev-only auth-off override. Extended the env-var table with the `OTEL_EXPORTER_OTLP_HEADERS` row and described the full manager -> dashboard -> reporter-Job forwarding chain, including the RBAC-implies-access honesty note.
- Replaced the `phoenix.baseURL` deep-link section's "install instructions land in Phase 47" placeholder with cross-references to the new subsection and INSTALL.md's step, and updated the `>= 14.2.0` floor sentence to cite the freshly re-verified pin's appVersion.

## Task Commits

Each task was committed atomically:

1. **Task 1: Fresh pin re-check + D-04 re-verification + INSTALL.md "Enable tracing (Phoenix)" section** - `cadb827` (docs)
2. **Task 2: observability.md "Self-hosted Phoenix" reference subsection + env-table row + deep-link placeholder replacement** - `68ba5d4` (docs)

_Note: no code changes in this plan — Wave 1 (47-01/47-02) already shipped the OTLP-headers chart wiring this plan's docs describe._

## Files Created/Modified

- `docs/INSTALL.md` - New `### Enable tracing (Phoenix)` section (three-beat: install/wire/verify) with the pinned `10.0.1` chart, real `tide-phoenix-svc` Service name, and the `tide-otlp-headers` Secret creation flow
- `docs/observability.md` - New `### Self-hosted Phoenix` subsection, extended env-var table (`OTEL_EXPORTER_OTLP_HEADERS` row), and the deep-link section's placeholder replaced with cross-references + updated appVersion-floor sentence

## Decisions Made

- **Chart pin: `10.0.1` / appVersion `18.1.0`** (re-verified live 2026-07-17, superseding the plan interfaces block's predicted `10.0.0`/`18.0.0`) — the pre-flight re-fetch is not bureaucracy; the chart genuinely moved in the two days since research. All three D-04 facts (appVersion floor, auth default, storage exclusivity) came back identical, so the divergence was purely the version string plus the deep-link floor sentence's citation.
- **Real Service name `tide-phoenix-svc`**, not the assumed `tide-phoenix` — verified via live `helm template` render before writing any endpoint example, per Task 1's binding pre-flight instruction.
- **Login credentials confirmed via a live fetch of Arize's own authentication docs**: default admin login is `admin@localhost` (email) + the chart's `PHOENIX_DEFAULT_ADMIN_INITIAL_PASSWORD` Secret value (weak fallback `admin`), and the System-API-key flow is exactly `Settings -> API Keys` -> `Authorization: Bearer <token>` — used verbatim in both docs.
- Locked discretionary values from the plan frontmatter used consistently: namespace `phoenix`, Helm release `tide-phoenix`, `persistence.size=2Gi`, `database.defaultRetentionPolicyDays=7`, headers Secret name `tide-otlp-headers` in `tide-system`.

## Deviations from Plan

None — plan executed exactly as written, with the expected chart-pin re-verification producing an update to the pin number (as the plan's pre-flight step anticipated could happen) rather than any structural doc change, since none of the three D-04 posture facts diverged.

**One noted gate-scope observation (not a deviation, not fixed):** Task 1's automated `<verify>` command greps the *entire* `docs/INSTALL.md` file for `endpoint=https?://` and expects zero matches. A pre-existing, out-of-scope line at `docs/INSTALL.md:195` (`--set prometheus.endpoint=http://prometheus-operated.monitoring:9090`, part of the pre-existing Prometheus section, unmodified by this plan) matches that pattern — it is a Prometheus HTTP base-URL for the dashboard's PromQL proxy, not an OTLP exporter target, and is legitimately scheme-prefixed. Verified via `git show HEAD:docs/INSTALL.md` that this line predates this plan's changes. Confirmed the actual acceptance intent — zero scheme-prefixed *OTLP* endpoints — holds: `awk` isolating just the new `### Enable tracing (Phoenix)` section shows zero matches. Per the deviation-rules scope boundary ("only auto-fix issues DIRECTLY caused by the current task's changes"), this pre-existing, unrelated line was left untouched rather than edited to satisfy an over-broad whole-file grep.

## Issues Encountered

None beyond the expected chart-pin drift (handled per Task 1's binding pre-flight instructions) and the grep-gate scope note above.

## User Setup Required

None - no external service configuration required. (The docs this plan authored describe manual steps an *operator* would run when following the recipe; no setup is required to land this plan itself.)

## Next Phase Readiness

- The recipe this plan documents is proven by execution in Plans 47-04/47-05 (D-11) — a wrong step here fails the live proof. Both docs are internally consistent (INSTALL.md <-> observability.md cross-reference each other; NOTES.txt's "Enable tracing" pointer text matches INSTALL.md's heading verbatim) and `make helm-assert`'s 9-permutation render gate stayed green (docs-only change, sanity-confirmed no chart drift).
- No blockers. Plans 47-04/47-05 should re-verify the chart pin again if more than a few days elapse before the live proof runs (Phoenix ships near-daily) — the pin recorded here (`10.0.1`/`18.1.0`, fetched 2026-07-17) is fresh as of this plan's authoring but not guaranteed fresh at proof time.

---
*Phase: 47-self-hosted-phoenix-install-end-to-end-proof*
*Completed: 2026-07-17*

## Self-Check: PASSED

- FOUND: docs/INSTALL.md
- FOUND: docs/observability.md
- FOUND: .planning/phases/47-self-hosted-phoenix-install-end-to-end-proof/47-03-SUMMARY.md
- FOUND commit: cadb827 (Task 1)
- FOUND commit: 68ba5d4 (Task 2)
- FOUND commit: 488fa69 (SUMMARY.md)
